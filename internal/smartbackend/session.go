package smartbackend

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

type token struct {
	refreshAt           time.Time
	value               []byte
	expires             time.Time
	authority, consumer networkaction.Actor
}

func (token) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, secret.Mask) }
func (token) MarshalJSON() ([]byte, error) { return nil, failure(ServerUnsupported) }

func (Session) MarshalJSONTo(*jsontext.Encoder) error { return failure(ServerUnsupported) }

type flight struct {
	done chan struct{}
	err  error
}

// Session owns one bounded cache for one immutable client/scopes/key generation.
// Disconnect and invalidation are terminal; a changed declaration needs a new
// preparation and independently admitted session, never resurrection of a grant.
type Session struct{ *sessionState }
type sessionState struct {
	client    *Client
	authority networkaction.Authority
	resolve   sendpolicy.Resolver
	mu        sync.Mutex
	cached    token
	pending   *flight
	admitted  networkaction.Actor
	stopped   bool
	cancel    context.CancelFunc
	life      context.Context
	now       func() time.Time
}

func (c *Client) Session(authority networkaction.Authority, resolve sendpolicy.Resolver) *Session {
	life, cancel := context.WithCancel(context.Background())
	return &Session{sessionState: &sessionState{client: c, authority: authority, resolve: resolve, cancel: cancel, life: life, now: time.Now}}
}
func (s *Session) Identity() string          { return s.client.identity }
func (s Session) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "SMART session (private)") }
func (Session) MarshalJSON() ([]byte, error) { return nil, failure(ServerUnsupported) }
func (s *Session) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.cached = token{}
	s.cancel()
}
func (s *Session) Invalidate() { s.Disconnect() }
func (s *Session) check(ctx context.Context, t networkaction.RuntimeTarget) (networkaction.Actor, error) {
	if s.authority == nil || t.Check == nil || t.Check(ctx) != nil || ctx.Err() != nil {
		return networkaction.Actor{}, failure(AuthorityChanged)
	}
	a, e := s.authority.Check(ctx, s.client.token.Binding())
	if e != nil || !networkaction.CurrentActor(a) {
		return networkaction.Actor{}, failure(AuthorityChanged)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return networkaction.Actor{}, failure(Disconnected)
	}
	if s.admitted != (networkaction.Actor{}) && s.admitted != a {
		s.cached = token{}
		s.stopped = true
		s.cancel()
		return networkaction.Actor{}, failure(AuthorityChanged)
	}
	s.admitted = a
	return a, nil
}
func (s *Session) Material(ctx context.Context, t networkaction.RuntimeTarget) (networkaction.RuntimeMaterial, error) {
	if err := s.client.requestPermission(t); err != nil {
		return networkaction.RuntimeMaterial{}, err
	}
	for {
		actor, err := s.check(ctx, t)
		if err != nil {
			return networkaction.RuntimeMaterial{}, err
		}
		s.mu.Lock()
		if s.cached.authority == actor && s.cached.consumer == t.Actor && s.cached.refreshAt.After(s.now()) {
			value := append([]byte(nil), s.cached.value...)
			s.mu.Unlock()
			if _, err := s.check(ctx, t); err != nil {
				return networkaction.RuntimeMaterial{}, err
			}
			return networkaction.BearerMaterial(value).WhileAuthorized(func(ctx context.Context) error { _, err := s.check(ctx, t); return err }), nil
		}
		if active := s.pending; active != nil {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return networkaction.RuntimeMaterial{}, failure(AuthUnavailable)
			case <-s.life.Done():
				return networkaction.RuntimeMaterial{}, failure(Disconnected)
			case <-active.done:
				if active.err != nil {
					return networkaction.RuntimeMaterial{}, active.err
				}
				continue
			}
		}
		active := &flight{done: make(chan struct{})}
		s.pending = active
		s.cached = token{}
		s.mu.Unlock()
		value, err := s.fetch(ctx, t, actor)
		s.mu.Lock()
		if s.stopped {
			err = failure(Disconnected)
		}
		if err == nil {
			s.cached = value
		}
		active.err = err
		s.pending = nil
		close(active.done)
		s.mu.Unlock()
		if err != nil {
			return networkaction.RuntimeMaterial{}, err
		}
		if !value.expires.After(s.now()) {
			return networkaction.RuntimeMaterial{}, failure(AuthUnavailable)
		}
		if _, err := s.check(ctx, t); err != nil {
			return networkaction.RuntimeMaterial{}, err
		}
		return networkaction.BearerMaterial(value.value).WhileAuthorized(func(ctx context.Context) error { _, err := s.check(ctx, t); return err }), nil
	}
}
func (s *Session) fetch(ctx context.Context, t networkaction.RuntimeTarget, actor networkaction.Actor) (token, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.life, cancel)
	defer stop()
	now := s.now()
	if now.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) || now.After(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)) {
		return token{}, failure(ClockInvalid)
	}
	check := func(ctx context.Context) error { _, err := s.check(ctx, t); return err }
	sig := &signer{client: s.client, now: now, check: check}
	response, _, err := s.client.token.Execute(ctx, s.authority, s.resolve, sig)
	if err != nil {
		if sig.failure != nil {
			return token{}, sig.failure
		}
		if check(ctx) != nil {
			return token{}, failure(AuthorityChanged)
		}
		return token{}, failure(AuthUnavailable)
	}
	if date := response.Header("Date"); date != "" {
		at, e := http.ParseTime(date)
		if e != nil || at.Sub(now) > 2*time.Minute || now.Sub(at) > 2*time.Minute {
			return token{}, failure(ClockInvalid)
		}
	}
	if response.Status == 400 || response.Status == 401 || response.Status == 403 {
		return token{}, failure(GrantInsufficient)
	}
	if response.Status != 200 {
		return token{}, failure(AuthUnavailable)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
		RefreshToken string `json:"refresh_token"`
	}
	if json.Unmarshal(response.Body.Expose(), &payload) != nil || payload.AccessToken == "" || len(payload.AccessToken) > 32<<10 || !strings.EqualFold(payload.TokenType, "bearer") || payload.ExpiresIn < 1 || payload.ExpiresIn > 300 || payload.RefreshToken != "" {
		return token{}, failure(ServerUnsupported)
	}
	for _, b := range []byte(payload.AccessToken) {
		if b < 33 || b > 126 {
			return token{}, failure(ServerUnsupported)
		}
	}
	granted, err := parseScopes(strings.Fields(payload.Scope))
	if err != nil || !covers(granted, s.client.permissions) {
		return token{}, failure(GrantInsufficient)
	}
	// A broader returned token must not quietly exceed the configured boundary.
	if !covers(s.client.permissions, granted) {
		return token{}, failure(GrantInsufficient)
	}
	if check(ctx) != nil {
		return token{}, failure(AuthorityChanged)
	}
	lifetime := time.Duration(payload.ExpiresIn) * time.Second
	margin := min(30*time.Second, lifetime/5)
	return token{value: []byte(payload.AccessToken), expires: now.Add(lifetime), refreshAt: now.Add(lifetime - margin), authority: actor, consumer: t.Actor}, nil
}
func (c *Client) requestPermission(t networkaction.RuntimeTarget) error {
	if t.Binding.Plan != c.config.Token.Plan || t.Binding.Project != c.config.Token.Project || t.Binding.Environment != c.config.Token.Environment || t.Binding.Revision != c.config.Token.Revision {
		return failure(AuthorityChanged)
	}
	if t.Contract == networkaction.RuntimeHTTPSchemaV2 {
		return c.requestPermissionV2(t)
	}
	base, _ := url.Parse(c.config.FHIRBase)
	u, err := url.Parse(t.URL)
	if err != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, base.Path+"/") || strings.Contains(u.Path, "//") || strings.Contains(u.Path, "/../") || strings.Contains(u.Path, "/./") {
		return failure(GrantInsufficient)
	}
	path := strings.TrimPrefix(u.Path, base.Path+"/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || !fhirr4.KnownResourceType(parts[0]) {
		return failure(ServerUnsupported)
	}
	permission := ""
	switch t.Method {
	case "GET":
		if t.Operation != sendpolicy.FHIRSearch && t.Operation != sendpolicy.ObservationRead && t.Operation != sendpolicy.FHIRMetadata {
			return failure(GrantInsufficient)
		}
		if len(parts) == 1 {
			permission = "s"
		} else if len(parts) == 2 && validFHIRID(parts[1]) {
			permission = "r"
		} else if len(parts) == 4 && parts[2] == "_history" && validFHIRID(parts[1]) && validFHIRID(parts[3]) {
			permission = "r"
		}
	case "POST":
		if len(parts) == 1 {
			permission = "c"
		}
	case "PUT", "PATCH":
		if len(parts) == 2 && validFHIRID(parts[1]) {
			permission = "u"
		}
	case "DELETE":
		if len(parts) == 2 && validFHIRID(parts[1]) {
			permission = "d"
		}
	}
	if permission == "" || !strings.Contains(c.permissions[parts[0]]+c.permissions["*"], permission) {
		return failure(GrantInsufficient)
	}
	if t.Method != "GET" && (c.config.Role != "setup" || (t.Operation != sendpolicy.FHIRAction && t.Operation != sendpolicy.SetupAction)) {
		return failure(GrantInsufficient)
	}
	return nil
}

// Execute is the shared protected HTTP adapter consumed by FHIR protocol work.
// It retries at most one GET after 401, with a new admission and new token. A
// mutating request, failed transport or any other status is never retried.
func (s *Session) Execute(ctx context.Context, plan *networkaction.RuntimeHTTPPlan, authority networkaction.Authority, resolve sendpolicy.Resolver) (networkaction.HTTPResponse, []networkaction.RuntimeReceipt, error) {
	if plan == nil || plan.Declaration().Authorization != s.Identity() {
		return networkaction.HTTPResponse{}, nil, failure(AuthorityChanged)
	}
	receipts := []networkaction.RuntimeReceipt{}
	for attempt := 0; attempt < 2; attempt++ {
		response, receipt, err := plan.Execute(ctx, authority, resolve, s)
		receipts = append(receipts, receipt)
		if err != nil {
			var refusal networkaction.CredentialRefusal
			if errors.As(err, &refusal) {
				return networkaction.HTTPResponse{}, receipts, failure(Code(refusal.Code))
			}
			return networkaction.HTTPResponse{}, receipts, failure(AuthUnavailable)
		}
		if response.Status == 403 {
			return response, receipts, failure(GrantInsufficient)
		}
		if response.Status != 401 {
			return response, receipts, nil
		}
		if attempt == 1 || plan.Declaration().HTTP.Method != "GET" {
			return response, receipts, failure(GrantInsufficient)
		}
		s.mu.Lock()
		s.cached = token{}
		s.mu.Unlock()
	}
	return networkaction.HTTPResponse{}, receipts, failure(AuthUnavailable)
}

// ForgetToken permits an explicitly bounded protocol-level retry after a known
// unauthorized response. It never changes declarations or revives a session.
func (s *Session) ForgetToken() { s.mu.Lock(); defer s.mu.Unlock(); s.cached = token{} }
func (c *Client) requestPermissionV2(t networkaction.RuntimeTarget) error {
	h := t.Headers
	r, e := fhirrequest.Parse(c.config.FHIRBase, t.Method, t.URL, t.ContentType, t.Body, fhirrequest.Headers{IfMatch: h.IfMatch, IfNoneMatch: h.IfNoneMatch, IfModifiedSince: h.IfModifiedSince, IfNoneExist: h.IfNoneExist, Prefer: h.Prefer})
	if t.Page != nil {
		if t.Method != "GET" || len(t.Body) > 0 || !networkaction.ValidDigest(t.Page.FromResponseSHA256) {
			return failure(ServerUnsupported)
		}
		r, e = fhirrequest.Page(c.config.FHIRBase, t.Page.Resource, t.URL)
	}
	if e != nil {
		return failure(ServerUnsupported)
	}
	if t.Method == "GET" {
		if t.Operation != sendpolicy.FHIRSearch && t.Operation != sendpolicy.FHIRMetadata && t.Operation != sendpolicy.ObservationRead {
			return failure(GrantInsufficient)
		}
	} else if t.Operation != sendpolicy.FHIRAction && t.Operation != sendpolicy.SetupAction {
		return failure(GrantInsufficient)
	}
	for _, p := range r.Permissions {
		if p.Interaction != "r" && p.Interaction != "s" && c.config.Role != "setup" {
			return failure(GrantInsufficient)
		}
		if !strings.Contains(c.permissions[p.Resource]+c.permissions["*"], p.Interaction) {
			return failure(GrantInsufficient)
		}
	}
	return nil
}
