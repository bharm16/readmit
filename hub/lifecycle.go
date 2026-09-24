package hub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

func (s *Store) lifecycleEvents(ctx context.Context, project string) ([]LifecycleEvent, error) {
	return lifecycleLog.read(ctx, s.db, project)
}

// lifecycleAdmission is the lifecycle route's one declaration of how a
// request maps into action vocabulary: a read reports history, a revision or
// resolve writes, every other command is administrative, and an audit-export
// additionally holds the export grant its content stands for. An audit-export
// is an export of record rather than new authored work, so it never admits
// an operation.
func lifecycleAdmission(method, kind string) teamAdmission {
	adm := teamAdmission{action: "evidence.read"}
	if method != "POST" {
		return adm
	}
	adm.writes = kind != "audit-export"
	if kind == "revision" || kind == "resolve" {
		adm.action = "evidence.write"
	} else {
		adm.action = "admin"
	}
	if kind == "audit-export" {
		adm.further = []furtherGrant{{action: "export", refusal: "export refused"}}
	}
	return adm
}

// lifecycleIdentity is the identity rule the lifecycle route declares: a
// command is issued by a human the policy identifies, whose issuer the
// backup bound can retain.
func lifecycleIdentity(method string) identityRule {
	return func(p Principal) (bool, string) {
		if method == "POST" && (p.Kind != "oidc" || p.Role == "runner" || !reviewText(p.Issuer, 2048)) {
			return false, "access refused"
		}
		return true, ""
	}
}

func (s *Store) lifecycleRequest(w http.ResponseWriter, r *http.Request, a *Access, project string) {
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method refused", 405)
		return
	}
	var c LifecycleCommand
	if r.Method == "POST" {
		data, e := io.ReadAll(io.LimitReader(r.Body, hubprotocol.MaxCommandBytes+1))
		if e != nil {
			http.Error(w, "incomplete command", 400)
			return
		}
		c, e = hubprotocol.DecodeLifecycleCommand(data)
		if e != nil {
			http.Error(w, "invalid command", 400)
			return
		}
	}
	adm := lifecycleAdmission(r.Method, c.Kind)
	adm.accept = lifecycleIdentity(r.Method)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, release, ok := s.authorizeWrite(a, r, w, project, adm)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	events, e := s.lifecycleEvents(r.Context(), project)
	if e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	if r.Method == "GET" {
		sendReview(w, 200, hubprotocol.LifecycleHistory{Schema: hubprotocol.LifecycleHistorySchema, Head: len(events), Events: events, Tips: hubprotocol.DeriveLifecycle(events).Tips(), Warning: hubprotocol.CustodyWarning})
		return
	}
	total, e := lifecycleLog.total(r.Context(), s.db)
	if e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	existing, replay, e := lifecycleLog.admit(events, total, c, p.Subject, p.Issuer)
	if errors.Is(e, errLogIDConflict) {
		http.Error(w, "command id conflict", 409)
		return
	}
	if errors.Is(e, errLogHead) {
		http.Error(w, "head conflict or limit", 409)
		return
	}
	if replay {
		s.sendLifecycle(w, r, 200, existing, events)
		return
	}
	if c.Kind == "remove-user" {
		policy, e := a.policy()
		if e != nil || c.Subject == p.Subject || policy.role(c.Subject, project) == "" || policy.role(c.Subject, project) == "owner" {
			http.Error(w, "removal refused", 403)
			return
		}
	}
	now := time.Now().UTC()
	if e = hubprotocol.DeriveLifecycle(events).Validate(c, func(d string) ([]byte, error) {
		if !s.linked(r.Context(), project, d) {
			return nil, ErrMissing
		}
		return s.Get(r.Context(), d)
	}, now); e != nil {
		http.Error(w, "lifecycle conflict", 409)
		return
	}
	event := LifecycleEvent{Schema: hubprotocol.LifecycleEventSchema, Project: project, Sequence: len(events) + 1, Issuer: p.Issuer, Actor: p.Subject, At: now.Format(time.RFC3339Nano), Command: c}
	if c.Kind == "audit-export" {
		reviews, e := s.reviewEvents(r.Context(), project)
		if e != nil {
			http.Error(w, "audit unavailable", 503)
			return
		}
		event.ReviewHead = len(reviews)
	}
	if e := lifecycleLog.commit(r.Context(), s.db, project, event); e != nil {
		http.Error(w, "commit unavailable; retry same id", 503)
		return
	}
	s.sendLifecycle(w, r, 201, event, append(events, event))
}

// authorize combines the reloaded access policy with durable project removals.
// Policy reinstallation cannot accidentally resurrect a removed principal.
func (s *Store) authorize(a *Access, r *http.Request, project, action string) (Principal, error) {
	p, e := a.Authorize(r, project, action)
	if e != nil {
		return p, e
	}
	events, e := s.lifecycleEvents(r.Context(), project)
	if e != nil {
		return Principal{}, errAccess
	}
	if hubprotocol.DeriveLifecycle(events).Removed(p.Issuer, p.Subject) {
		return Principal{}, errAccess
	}
	return p, nil
}
func (s *Store) retired(ctx context.Context, project, digest string) (bool, error) {
	events, e := s.lifecycleEvents(ctx, project)
	if e != nil {
		return false, e
	}
	return hubprotocol.DeriveLifecycle(events).Retired(digest), nil
}
func (s *Store) sendLifecycle(w http.ResponseWriter, r *http.Request, status int, event LifecycleEvent, events []LifecycleEvent) {
	if event.Command.Kind != "audit-export" {
		sendReview(w, status, event)
		return
	}
	reviews, e := s.reviewEvents(r.Context(), event.Project)
	if e != nil {
		http.Error(w, "audit unavailable; retry same id", 503)
		return
	}
	// Retry reproduces both committed history prefixes.
	if event.ReviewHead > len(reviews) {
		http.Error(w, "audit unavailable", 503)
		return
	}
	reviews = reviews[:event.ReviewHead]
	prefix := events[:event.Sequence]
	w.Header().Set("Content-Disposition", `attachment; filename="audit.json"`)
	sendReview(w, status, hubprotocol.AuditExport{Schema: hubprotocol.DeriveReviews(reviews).AuditSchema(), Project: event.Project, Lifecycle: prefix, ReviewHead: len(reviews), Reviews: reviews, Warning: hubprotocol.CustodyWarning})
}
