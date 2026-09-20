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
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/expectation"
)

const maxReviews = 1024

type ReviewCommand struct {
	Schema    string `json:"schema"`
	ID        string `json:"id"`
	Expected  int    `json:"expected"`
	Kind      string `json:"kind"`
	Evidence  string `json:"evidence"`
	Parent    string `json:"parent"`
	Recipient string `json:"recipient"`
	Text      string `json:"text"`
	Release   string `json:"release"`
}

// ReviewEvent binds a command to the authenticated actor and server sequence.
// Neither the local release's approver label nor a client header supplies identity.
type ReviewEvent struct {
	Schema   string        `json:"schema"`
	Project  string        `json:"project"`
	Sequence int           `json:"sequence"`
	Issuer   string        `json:"issuer"`
	Actor    string        `json:"actor"`
	At       string        `json:"at"`
	Command  ReviewCommand `json:"command"`
}

func reviewText(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func decodeReviewCommand(data []byte) (ReviewCommand, error) {
	var c ReviewCommand
	if len(data) > 8192 || requireExactMembers(data, "schema", "id", "expected", "kind", "evidence", "parent", "recipient", "text", "release") != nil || json.Unmarshal(data, &c, json.RejectUnknownMembers(true)) != nil {
		return c, errAccess
	}
	if (c.Schema != "readmit-hub-review-command/v1" && !supportCommand(c)) || !validProject(c.ID) || c.Expected < 0 || c.Expected >= maxReviews || !validDigest(c.Evidence) || (c.Parent != "" && !validProject(c.Parent)) || !reviewText(c.Recipient, 256) || !reviewText(c.Text, 2048) || strings.TrimSpace(c.Text) == "" {
		return c, errAccess
	}
	if supportCommand(c) {
		return c, validateSupportShape(c)
	}
	switch c.Kind {
	case "comment":
		if c.Release != "" {
			return c, errAccess
		}
	case "assignment":
		if c.Recipient == "" || c.Release != "" || c.Parent != "" {
			return c, errAccess
		}
	case "review-request":
		if c.Recipient == "" || !validDigest(c.Release) || c.Parent != "" {
			return c, errAccess
		}
	case "approval":
		if c.Recipient != "" || !validDigest(c.Release) || c.Parent == "" {
			return c, errAccess
		}
	default:
		return c, errAccess
	}
	return c, nil
}
func (s *Store) reviewEvents(ctx context.Context, project string) ([]ReviewEvent, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT document FROM readmit_hub_reviews WHERE project=$1 ORDER BY sequence`, project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	events := []ReviewEvent{}
	for rows.Next() {
		var data string
		if e = rows.Scan(&data); e != nil {
			return nil, e
		}
		var event ReviewEvent
		if json.Unmarshal([]byte(data), &event, json.RejectUnknownMembers(true)) != nil {
			return nil, ErrIntegrity
		}
		events = append(events, event)
		if len(events) > maxReviews {
			return nil, ErrLimit
		}
	}
	return events, rows.Err()
}
func (s *Store) linked(ctx context.Context, project, digest string) bool {
	var exists bool
	return s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM readmit_hub_project_artifacts WHERE project=$1 AND digest=$2)`, project, digest).Scan(&exists) == nil && exists
}
func loadRelease(load func(string) ([]byte, error), digest string) (expectation.Release, error) {
	data, e := load(digest)
	if e != nil {
		return expectation.Release{}, e
	}
	return expectation.Decode(data)
}

func validateReview(c ReviewCommand, actor, issuer string, events []ReviewEvent, load func(string) ([]byte, error)) error {
	if supportCommand(c) {
		return validateSupport(c, actor, issuer, events, load)
	}
	if _, e := load(c.Evidence); e != nil {
		return e
	}
	var parent *ReviewEvent
	for i := range events {
		if events[i].Command.ID == c.Parent {
			parent = &events[i]
		}
	}
	if c.Parent != "" && (parent == nil || parent.Command.Evidence != c.Evidence) {
		return ErrMissing
	}
	if c.Kind != "review-request" && c.Kind != "approval" {
		return nil
	}
	release, e := loadRelease(load, c.Release)
	if e != nil {
		return e
	}
	if c.Kind == "review-request" {
		return nil
	}
	if parent.Command.Kind != "review-request" || parent.Command.Release != c.Release || parent.Command.Recipient != actor || parent.Issuer != issuer || parent.Actor == actor {
		return errAccess
	}
	var previous *expectation.Release
	for _, event := range events {
		if event.Command.Kind != "approval" {
			continue
		}
		if event.Command.Parent == c.Parent {
			return ErrConflict
		}
		prior, e := loadRelease(load, event.Command.Release)
		if e != nil {
			return e
		}
		if prior.ID == release.ID {
			previous = &prior
		}
	}
	if previous == nil {
		if release.Parent != "" || release.Baseline.Revision != 1 {
			return ErrConflict
		}
	} else {
		if release.Parent != previous.Identity() || release.Baseline.Revision != previous.Baseline.Revision+1 {
			return ErrConflict
		}
		// Recompute both predecessor commitments and all profile continuity rules.
		spec, e := json.Marshal(release.Baseline.Spec)
		if e != nil {
			return e
		}
		review, e := expectation.Review(release.ID, spec, release.Profiles, previous, false)
		if e != nil || review.Identity != release.Review {
			return ErrConflict
		}
	}
	return nil
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
func (s *Store) reviewRequest(w http.ResponseWriter, r *http.Request, a *Access, project, route string) {
	v2 := strings.HasPrefix(r.URL.Path, "/v2/")
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

	action := "evidence.read"
	if route == "reviews" {
		action = "evidence.write"
	}
	var c ReviewCommand
	if route == "reviews" {
		if r.Method != "POST" {
			http.Error(w, "method refused", 405)
			return
		}
		data, e := io.ReadAll(io.LimitReader(r.Body, 8193))
		if e != nil {
			http.Error(w, "incomplete review", 400)
			return
		}
		c, e = decodeReviewCommand(data)
		if e != nil {
			http.Error(w, "invalid review", 400)
			return
		}
		if supportCommand(c) && !v2 {
			http.Error(w, "review version unavailable", 400)
			return
		}
		if c.Kind == "support-policy" {
			action = "admin"
		}
		if c.Kind == "approval" || supportApproval(c) {
			action = "approval"
		}
		// Reviewers can comment with their existing approval scope, but cannot assign.
		if c.Kind == "comment" {
			if _, e := s.authorize(a, r, project, "approval"); e == nil {
				action = "approval"
			}
		}
	} else if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method refused", 405)
		return
	}
	principal, e := s.authorize(a, r, project, action)
	if e != nil {
		http.Error(w, "access refused", 403)
		return
	}
	if route == "reviews" && (principal.Kind != "oidc" || principal.Role == "runner" || len(principal.Issuer) > 2048) {
		http.Error(w, "human identity required", 403)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method != "GET" {
		release, err := s.admitAuthor(r, principal)
		if err != nil {
			http.Error(w, "operation admission refused", 403)
			return
		}
		defer release()
	}
	// Reauthorize after waiting for the write/backup lock so queued requests do not
	// retain a grant that was revoked while another operation held the lock.
	principal, e = s.authorize(a, r, project, action)
	if e != nil {
		http.Error(w, "access refused", 403)
		return
	}
	events, e := s.reviewEvents(r.Context(), project)
	if e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	if !v2 && hasSupportEvents(events) {
		http.Error(w, "history requires v2", 409)
		return
	}
	if route == "reviews" && supportCommand(c) && !supportCurrent(c, events) {
		http.Error(w, "sharing policy changed", 409)
		return
	}
	if route != "reviews" {
		var query struct {
			Schema   string `json:"schema"`
			After    int    `json:"after"`
			Text     string `json:"text"`
			Evidence string `json:"evidence"`
		}
		if r.Method == "POST" {
			data, e := io.ReadAll(io.LimitReader(r.Body, 2049))
			if e != nil || len(data) > 2048 || requireExactMembers(data, "schema", "after", "text", "evidence") != nil || json.Unmarshal(data, &query, json.RejectUnknownMembers(true)) != nil || query.Schema != "readmit-hub-review-query/v1" || query.After < 0 || !reviewText(query.Text, 256) || (query.Evidence != "" && !validDigest(query.Evidence)) {
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
		sendReview(w, 200, struct {
			Schema string        `json:"schema"`
			Head   int           `json:"head"`
			Events []ReviewEvent `json:"events"`
		}{reviewHistorySchema(v2), len(events), filtered})
		return
	}
	for _, event := range events {
		if event.Command.ID == c.ID {
			if event.Command == c && event.Actor == principal.Subject && event.Issuer == principal.Issuer {
				supportRecorded = true
				sendReview(w, 200, event)
			} else {
				http.Error(w, "review id conflict", 409)
			}
			return
		}
	}
	var total int
	if e := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM readmit_hub_reviews`).Scan(&total); e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	if c.Expected != len(events) || total >= maxReviews {
		http.Error(w, "review head conflict or limit", 409)
		return
	}
	policy, e := a.policy()
	if e != nil {
		http.Error(w, "access refused", 403)
		return
	}
	if c.Recipient != "" {
		if supportCommand(c) {
			lifecycle, e := s.lifecycleEvents(r.Context(), project)
			if e != nil {
				http.Error(w, "recipient unavailable", 403)
				return
			}
			for _, event := range lifecycle {
				if event.Command.Kind == "remove-user" && event.Command.Subject == c.Recipient && event.Issuer == principal.Issuer {
					http.Error(w, "recipient refused", 403)
					return
				}
			}
		}
		role := policy.role(c.Recipient, project)
		if role == "" || role == "runner" || ((c.Kind == "review-request" || supportRequest(c)) && (!roleAllows(role, "approval") || c.Recipient == principal.Subject)) {
			http.Error(w, "recipient refused", 403)
			return
		}
	}
	if e = validateReview(c, principal.Subject, principal.Issuer, events, func(d string) ([]byte, error) {
		if supportCommand(c) {
			return s.supportArtifact(r.Context(), project, d)
		}
		if !s.linked(r.Context(), project, d) {
			return nil, ErrMissing
		}
		return s.Get(r.Context(), d)
	}); e != nil {
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
	eventSchema := "readmit-hub-review-event/v1"
	if supportCommand(c) {
		eventSchema = "readmit-hub-review-event/v2"
	}
	event := ReviewEvent{eventSchema, project, len(events) + 1, principal.Issuer, principal.Subject, time.Now().UTC().Format(time.RFC3339Nano), c}
	data, e := json.Marshal(event)
	if e != nil {
		http.Error(w, "review unavailable", 503)
		return
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		http.Error(w, "metadata unavailable", 503)
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(r.Context(), `INSERT INTO readmit_hub_reviews(project,sequence,id,document) VALUES($1,$2,$3,$4)`, project, event.Sequence, c.ID, string(data)); e == nil {
		_, e = tx.ExecContext(r.Context(), `UPDATE readmit_hub_schema SET team_enabled=true WHERE singleton`)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		http.Error(w, "review commit unavailable; retry same id", 503)
		return
	}
	supportRecorded = true
	sendReview(w, 201, event)
}
