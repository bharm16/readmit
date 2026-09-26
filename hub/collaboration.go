package hub

import (
	"encoding/json/v2"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

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
	adm.serialized = true
	s.mu.Lock()
	defer s.mu.Unlock()
	principal, proj, release, ok := s.authorizeWrite(a, r, w, project, adm)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	projects := s.projects()
	reviews, e := projects.reviews(r.Context(), proj, v2)
	if errors.Is(e, errSupportNeedsV2) {
		http.Error(w, "history requires v2", 409)
		return
	}
	if e != nil {
		http.Error(w, "metadata unavailable", 503)
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
		sendReview(w, 200, reviews.history(principal, route == "notifications", query, v2))
		return
	}
	event, replayed, e := projects.recordReview(r.Context(), proj, reviews, principal, c, a.roles(project))
	switch {
	case errors.Is(e, errPolicyChanged):
		http.Error(w, "sharing policy changed", 409)
	case errors.Is(e, errLogIDConflict):
		http.Error(w, "review id conflict", 409)
	case errors.Is(e, errLogHead):
		http.Error(w, "review head conflict or limit", 409)
	case errors.Is(e, errPolicyUnavailable):
		http.Error(w, "access refused", 403)
	case errors.Is(e, errRecipient):
		http.Error(w, "recipient refused", 403)
	case errors.Is(e, errReviewRefused):
		status := 409
		if errors.Is(e, ErrMissing) {
			status = 404
		}
		if errors.Is(e, errAccess) {
			status = 403
		}
		http.Error(w, "review refused", status)
	case errors.Is(e, errCommitUnavailable):
		http.Error(w, "review commit unavailable; retry same id", 503)
	case e != nil:
		http.Error(w, "metadata unavailable", 503)
	case replayed:
		supportRecorded = true
		sendReview(w, 200, event)
	default:
		supportRecorded = true
		sendReview(w, 201, event)
	}
}
