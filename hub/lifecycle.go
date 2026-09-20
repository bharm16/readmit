package hub

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const maxLifecycle = 1024

// LifecycleCommand is a new contract; review commands and approved releases keep
// their existing meaning. Parents name immutable revision event IDs, not paths.
type LifecycleCommand struct {
	Schema   string   `json:"schema"`
	ID       string   `json:"id"`
	Expected int      `json:"expected"`
	Kind     string   `json:"kind"`
	Resource string   `json:"resource"`
	Artifact string   `json:"artifact"`
	Parents  []string `json:"parents"`
	Subject  string   `json:"subject"`
	Until    string   `json:"until"`
	Reason   string   `json:"reason"`
}
type LifecycleEvent struct {
	Schema     string           `json:"schema"`
	Project    string           `json:"project"`
	Sequence   int              `json:"sequence"`
	Issuer     string           `json:"issuer"`
	Actor      string           `json:"actor"`
	At         string           `json:"at"`
	ReviewHead int              `json:"review_head"`
	Command    LifecycleCommand `json:"command"`
}

func decodeLifecycle(data []byte) (LifecycleCommand, error) {
	var c LifecycleCommand
	if len(data) > 8192 || requireExactMembers(data, "schema", "id", "expected", "kind", "resource", "artifact", "parents", "subject", "until", "reason") != nil || json.Unmarshal(data, &c, json.RejectUnknownMembers(true)) != nil {
		return c, errAccess
	}
	if c.Schema != "readmit-hub-lifecycle-command/v1" || !validProject(c.ID) || c.Expected < 0 || c.Expected >= maxLifecycle || !reviewText(c.Reason, 2048) || strings.TrimSpace(c.Reason) == "" {
		return c, errAccess
	}
	switch c.Kind {
	case "revision", "resolve":
		if !validProject(c.Resource) || !validDigest(c.Artifact) || c.Subject != "" || c.Until != "" || len(c.Parents) > 64 || (c.Kind == "revision" && len(c.Parents) > 1) || (c.Kind == "resolve" && len(c.Parents) < 2) {
			return c, errAccess
		}
	case "remove-user", "retention", "retire", "audit-export":
		if c.Resource != "" || len(c.Parents) != 0 {
			return c, errAccess
		}
		switch c.Kind {
		case "remove-user":
			if c.Subject == "" || !reviewText(c.Subject, 256) || c.Artifact != "" || c.Until != "" {
				return c, errAccess
			}
		case "retention":
			if !validDigest(c.Artifact) || c.Subject != "" {
				return c, errAccess
			}
			if _, e := time.Parse(time.RFC3339Nano, c.Until); e != nil {
				return c, errAccess
			}
		case "retire":
			if !validDigest(c.Artifact) || c.Subject != "" || c.Until != "" {
				return c, errAccess
			}
		case "audit-export":
			if c.Artifact != "" || c.Subject != "" || c.Until != "" {
				return c, errAccess
			}
		}
	default:
		return c, errAccess
	}
	previous := ""
	for _, p := range c.Parents {
		if !validProject(p) || p <= previous {
			return c, errAccess
		}
		previous = p
	}
	return c, nil
}
func (s *Store) lifecycleEvents(ctx context.Context, project string) ([]LifecycleEvent, error) {
	return lifecycleLog.read(ctx, s.db, project)
}
func revisionTips(events []LifecycleEvent) map[string][]string {
	tips := map[string][]string{}
	for _, e := range events {
		c := e.Command
		if c.Kind != "revision" && c.Kind != "resolve" {
			continue
		}
		p := tips[c.Resource]
		p = slices.DeleteFunc(p, func(id string) bool { return slices.Contains(c.Parents, id) })
		p = append(p, c.ID)
		slices.Sort(p)
		tips[c.Resource] = p
	}
	return tips
}
func validateLifecycle(c LifecycleCommand, events []LifecycleEvent, load func(string) ([]byte, error), at time.Time) error {
	if c.Artifact != "" {
		for _, event := range events {
			if event.Command.Kind == "retire" && event.Command.Artifact == c.Artifact {
				return ErrConflict
			}
		}
		if _, e := load(c.Artifact); e != nil {
			return e
		}
	}
	switch c.Kind {
	case "remove-user", "audit-export":
		return nil
	case "retention", "retire":
		var until time.Time
		for _, e := range events {
			if e.Command.Artifact == c.Artifact {
				if e.Command.Kind == "retire" {
					return ErrConflict
				}
				if e.Command.Kind == "retention" {
					until, _ = time.Parse(time.RFC3339Nano, e.Command.Until)
				}
			}
		}
		if c.Kind == "retire" {
			if until.IsZero() || at.Before(until) {
				return ErrConflict
			}
		} else {
			next, _ := time.Parse(time.RFC3339Nano, c.Until)
			if next.Before(until) {
				return ErrConflict
			}
		}
		return nil
	}
	tips := revisionTips(events)[c.Resource]
	if c.Kind == "revision" && len(tips) >= 64 && (len(c.Parents) == 0 || !slices.Contains(tips, c.Parents[0])) {
		return ErrLimit
	}
	if c.Kind == "resolve" && !slices.Equal(c.Parents, tips) {
		return ErrConflict
	}
	if len(c.Parents) == 0 && len(tips) > 0 {
		return ErrConflict
	}
	for _, parent := range c.Parents {
		found := false
		for _, event := range events {
			p := event.Command
			if p.ID == parent && p.Resource == c.Resource && (p.Kind == "revision" || p.Kind == "resolve") {
				found = true
			}
		}
		if !found {
			return ErrConflict
		}
	}
	return nil
}
func (s *Store) lifecycleRequest(w http.ResponseWriter, r *http.Request, a *Access, project string) {
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method refused", 405)
		return
	}
	action := "evidence.read"
	var c LifecycleCommand
	if r.Method == "POST" {
		data, e := io.ReadAll(io.LimitReader(r.Body, 8193))
		if e != nil {
			http.Error(w, "incomplete command", 400)
			return
		}
		c, e = decodeLifecycle(data)
		if e != nil {
			http.Error(w, "invalid command", 400)
			return
		}
		action = "evidence.write"
		if c.Kind != "revision" && c.Kind != "resolve" {
			action = "admin"
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, release, ok := s.authorizeWrite(a, r, w, project, action,
		r.Method == "POST" && c.Kind != "audit-export",
		func(p Principal) (bool, string) {
			if r.Method == "POST" && (p.Kind != "oidc" || p.Role == "runner" || !reviewText(p.Issuer, 2048)) {
				return false, "access refused"
			}
			return true, ""
		})
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
		sendReview(w, 200, struct {
			Schema  string              `json:"schema"`
			Head    int                 `json:"head"`
			Events  []LifecycleEvent    `json:"events"`
			Tips    map[string][]string `json:"tips"`
			Warning string              `json:"warning"`
		}{"readmit-hub-lifecycle-history/v1", len(events), events, revisionTips(events), "Downloaded copies remain under local custody and cannot be revoked."})
		return
	}
	if c.Kind == "audit-export" {
		if _, e := s.authorize(a, r, project, "export"); e != nil {
			http.Error(w, "export refused", 403)
			return
		}
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
	if e = validateLifecycle(c, events, func(d string) ([]byte, error) {
		if !s.linked(r.Context(), project, d) {
			return nil, ErrMissing
		}
		return s.Get(r.Context(), d)
	}, now); e != nil {
		http.Error(w, "lifecycle conflict", 409)
		return
	}
	event := LifecycleEvent{"readmit-hub-lifecycle-event/v1", project, len(events) + 1, p.Issuer, p.Subject, now.Format(time.RFC3339Nano), 0, c}
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
	for _, event := range events {
		if event.Command.Kind == "remove-user" && event.Command.Subject == p.Subject && event.Issuer == p.Issuer {
			return Principal{}, errAccess
		}
	}
	return p, nil
}
func (s *Store) retired(ctx context.Context, project, digest string) (bool, error) {
	events, e := s.lifecycleEvents(ctx, project)
	if e != nil {
		return false, e
	}
	for _, event := range events {
		if event.Command.Kind == "retire" && event.Command.Artifact == digest {
			return true, nil
		}
	}
	return false, nil
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
	sendReview(w, status, struct {
		Schema     string           `json:"schema"`
		Project    string           `json:"project"`
		Lifecycle  []LifecycleEvent `json:"lifecycle"`
		ReviewHead int              `json:"review_head"`
		Reviews    []ReviewEvent    `json:"reviews"`
		Warning    string           `json:"warning"`
	}{auditReviewSchema(reviews), event.Project, prefix, len(reviews), reviews, "Downloaded copies remain under local custody and cannot be revoked."})
}
