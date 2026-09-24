package hub

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

func (s *Store) reviewEvents(ctx context.Context, project string) ([]ReviewEvent, error) {
	return reviewLog.read(ctx, s.db, project)
}
func (s *Store) linked(ctx context.Context, project, digest string) bool {
	exists, e := s.linkedProjectArtifact(ctx, project, digest)
	return e == nil && exists
}
func sendReview(w http.ResponseWriter, status int, v any) {
	data, e := json.Marshal(v)
	if e != nil {
		http.Error(w, "review unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// humanReviewer is the identity rule the review route family declares: a
// review is written by a human the policy identifies, never a machine
// principal, and an issuer the backup bound cannot retain is refused here
// rather than mid-commit.
func humanReviewer(route string) identityRule {
	return func(p Principal) (bool, string) {
		if route == "reviews" && (p.Kind != "oidc" || p.Role == "runner" || len(p.Issuer) > 2048) {
			return false, "human identity required"
		}
		return true, ""
	}
}

// reviewAdmission is the review route family's one declaration of how a
// request maps into action vocabulary. History and notifications read; a
// review writes; and the command kinds escalate — a support policy needs
// admin, an approval needs approval, and a comment rides the commenter's
// approval scope when the policy grants one. The comment escalation asks the
// policy alone: removal is enforced on the final action inside the write
// sequence, so a removed principal is refused either way.
func reviewAdmission(route string, c ReviewCommand, a *Access, r *http.Request, project string) teamAdmission {
	action := "evidence.read"
	if route == "reviews" {
		action = "evidence.write"
		if c.Kind == "support-policy" {
			action = "admin"
		}
		if c.Kind == "approval" || hubprotocol.IsSupportApproval(c) {
			action = "approval"
		}
		// Reviewers can comment with their existing approval scope, but cannot assign.
		if c.Kind == "comment" {
			if _, e := a.Authorize(r, project, "approval"); e == nil {
				action = "approval"
			}
		}
	}
	// Searching history or notifications posts a query and reads; only a
	// review command writes, so only it is admitted as authoring.
	return teamAdmission{action: action, writes: route == "reviews" && r.Method != "GET", accept: humanReviewer(route)}
}

func (s *Store) reviewRequest(w http.ResponseWriter, r *http.Request, a *Access, project, route string, v2 bool) {
	supportRecorded := false
	if v2 && route == "reviews" {
		defer func() {
			if supportRecorded {
				log.Print(`{"schema":"readmit-sharing-security-event/v1","action":"team-reviewed"}`)
			} else {
				log.Print(`{"schema":"readmit-sharing-security-event/v1","action":"team-review-refused"}`)
			}
		}()
	}

	var c ReviewCommand
	if route == "reviews" {
		if r.Method != "POST" {
			http.Error(w, "method refused", 405)
			return
		}
		data, e := io.ReadAll(io.LimitReader(r.Body, hubprotocol.MaxCommandBytes+1))
		if e != nil {
			http.Error(w, "incomplete review", 400)
			return
		}
		c, e = hubprotocol.DecodeReviewCommand(data)
		if e != nil {
			http.Error(w, "invalid review", 400)
			return
		}
		if hubprotocol.IsSupport(c) && !v2 {
			http.Error(w, "review version unavailable", 400)
			return
		}
	} else if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method refused", 405)
		return
	}
	adm := reviewAdmission(route, c, a, r, project)
	s.mu.Lock()
	defer s.mu.Unlock()
	principal, release, ok := s.authorizeWrite(a, r, w, project, adm)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	events, e := s.reviewEvents(r.Context(), project)
	if e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	reviews := hubprotocol.DeriveReviews(events)
	if !v2 && reviews.HasSupport() {
		http.Error(w, "history requires v2", 409)
		return
	}
	if route == "reviews" && hubprotocol.IsSupport(c) && !reviews.Current(c) {
		http.Error(w, "sharing policy changed", 409)
		return
	}
	if route != "reviews" {
		var query hubprotocol.ReviewQuery
		if r.Method == "POST" {
			data, e := io.ReadAll(io.LimitReader(r.Body, hubprotocol.MaxQueryBytes+1))
			if e == nil {
				query, e = hubprotocol.DecodeReviewQuery(data)
			}
			if e != nil {
				http.Error(w, "invalid search", 400)
				return
			}
		}
		filtered := []ReviewEvent{}
		for _, event := range events {
			if event.Sequence <= query.After || (query.Evidence != "" && event.Command.Evidence != query.Evidence) || (route == "notifications" && (event.Command.Recipient != principal.Subject || event.Issuer != principal.Issuer)) || !strings.Contains(strings.ToLower(event.Command.Text), strings.ToLower(query.Text)) {
				continue
			}
			filtered = append(filtered, event)
		}
		sendReview(w, 200, hubprotocol.ReviewHistory{Schema: reviews.HistorySchema(v2), Head: len(events), Events: filtered})
		return
	}
	total, e := reviewLog.total(r.Context(), s.db)
	if e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	existing, replay, e := reviewLog.admit(events, total, c, principal.Subject, principal.Issuer)
	if errors.Is(e, errLogIDConflict) {
		http.Error(w, "review id conflict", 409)
		return
	}
	if errors.Is(e, errLogHead) {
		http.Error(w, "review head conflict or limit", 409)
		return
	}
	if replay {
		supportRecorded = true
		sendReview(w, 200, existing)
		return
	}
	policy, e := a.policy()
	if e != nil {
		http.Error(w, "access refused", 403)
		return
	}
	if c.Recipient != "" {
		if hubprotocol.IsSupport(c) {
			lifecycle, e := s.lifecycleEvents(r.Context(), project)
			if e != nil {
				http.Error(w, "recipient unavailable", 403)
				return
			}
			if hubprotocol.DeriveLifecycle(lifecycle).Removed(principal.Issuer, c.Recipient) {
				http.Error(w, "recipient refused", 403)
				return
			}
		}
		role := policy.role(c.Recipient, project)
		if role == "" || role == "runner" || ((c.Kind == "review-request" || hubprotocol.IsSupportRequest(c)) && (!roleAllows(role, "approval") || c.Recipient == principal.Subject)) {
			http.Error(w, "recipient refused", 403)
			return
		}
	}
	if e = hubSentinel(reviews.Validate(c, principal.Subject, principal.Issuer, func(d string) ([]byte, error) {
		if hubprotocol.IsSupport(c) {
			return s.supportArtifact(r.Context(), project, d)
		}
		if !s.linked(r.Context(), project, d) {
			return nil, ErrMissing
		}
		return s.Get(r.Context(), d)
	})); e != nil {
		status := 409
		if errors.Is(e, ErrMissing) {
			status = 404
		}
		if errors.Is(e, errAccess) {
			status = 403
		}
		http.Error(w, "review refused", status)
		return
	}
	eventSchema := hubprotocol.ReviewEventV1
	if hubprotocol.IsSupport(c) {
		eventSchema = hubprotocol.ReviewEventV2
	}
	event := ReviewEvent{Schema: eventSchema, Project: project, Sequence: len(events) + 1, Issuer: principal.Issuer, Actor: principal.Subject, At: time.Now().UTC().Format(time.RFC3339Nano), Command: c}
	if e := reviewLog.commit(r.Context(), s.db, project, event); e != nil {
		http.Error(w, "review commit unavailable; retry same id", 503)
		return
	}
	supportRecorded = true
	sendReview(w, 201, event)
}
