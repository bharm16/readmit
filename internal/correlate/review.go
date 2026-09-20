package correlate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
)

const ReviewSchema = "readmit-correlation-review/v1"
const MaxDecisions = 1000
const MaxReviewBytes = 2 << 20

// Decision is a local declaration, not authenticated identity. Accept/reject
// name an existing machine or manual link. Add names exactly two occurrences;
// it never upgrades a collision into observed evidence.
type Decision struct {
	Action string `json:"action"`
	Link   string `json:"link"`
	From   string `json:"from"`
	To     string `json:"to"`
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

func (d *Decision) UnmarshalJSON(data []byte) error {
	var required struct {
		Action *string `json:"action"`
		Link   *string `json:"link"`
		From   *string `json:"from"`
		To     *string `json:"to"`
		Actor  *string `json:"actor"`
		Reason *string `json:"reason"`
	}
	if json.Unmarshal(data, &required) != nil || required.Action == nil || required.Link == nil || required.From == nil || required.To == nil || required.Actor == nil || required.Reason == nil {
		return errors.New("a correlation decision requires action, link, from, to, actor and reason")
	}
	type plain Decision
	var decoded plain
	if json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)) != nil {
		return errors.New("invalid correlation decision JSON")
	}
	*d = Decision(decoded)
	return nil
}

// ReviewRevision retains the full ordered human history separately from the
// untouched machine report. Parent commits to the preceding history prefix.
type ReviewRevision struct {
	Schema    string     `json:"schema"`
	Machine   string     `json:"machine"`
	Parent    string     `json:"parent"`
	Decisions []Decision `json:"decisions"`
}

type ReviewedLink struct {
	ID               string      `json:"id"`
	Linkage          string      `json:"linkage"`
	Rule             string      `json:"rule"`
	Occurrences      []Reference `json:"occurrences"`
	Status           string      `json:"status"`
	TotalOccurrences int         `json:"total_occurrences"`
}

// ReviewedCollision retains the original finding and its complete membership count.
type ReviewedCollision struct {
	Finding          Collision `json:"finding"`
	TotalOccurrences int       `json:"total_occurrences"`
}

// ReviewedView is derived only. Mapping binds the machine finding, verified
// case/rules and complete human history; consumers must require that exact
// identity when reusing any result derived under this mapping.
type ReviewedView struct {
	Offset          int                 `json:"offset"`
	TotalLinks      int                 `json:"total_links"`
	TotalCollisions int                 `json:"total_collisions"`
	TotalDecisions  int                 `json:"total_decisions"`
	Mapping         string              `json:"mapping"`
	Machine         string              `json:"machine"`
	Links           []ReviewedLink      `json:"links"`
	Collisions      []ReviewedCollision `json:"collisions"`
	History         []Decision          `json:"history"`
	ValuesShown     bool                `json:"values_shown"`
	Boundary        string              `json:"boundary"`
}

func reviewBytes(v any) []byte        { data, _ := json.Marshal(v, json.Deterministic(true)); return data }
func reviewDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Identity commits to all history and its machine input. Parent is derived
// from the preceding prefix and validated separately, keeping validation linear.
func (r ReviewRevision) Identity() string {
	return reviewDigest(reviewBytes(struct {
		Schema    string     `json:"schema"`
		Machine   string     `json:"machine"`
		Decisions []Decision `json:"decisions"`
	}{r.Schema, r.Machine, r.Decisions}))
}
func reviewText(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

func newRevision(report Report) ReviewRevision {
	return ReviewRevision{Schema: ReviewSchema, Machine: reviewDigest(reviewBytes(report)), Decisions: []Decision{}}
}

// Review applies a retained history to a report produced by Run over opened.
// Callers supply the verified bundle and freshly computed machine report, never
// a report received from an interface or an unverified document.
func Review(opened *bundle.Bundle, report Report, prior *ReviewRevision, showValues bool) (ReviewRevision, ReviewedView, error) {
	r := newRevision(report)
	if prior != nil {
		r = *prior
	}
	view := ReviewedView{Mapping: r.Identity(), Machine: r.Machine, Links: []ReviewedLink{}, Collisions: []ReviewedCollision{}, History: []Decision{}, ValuesShown: showValues, Boundary: "Human decisions are local analyst assertions, not authenticated identity or observed causality. Rejected findings remain in the original machine report. CLI and transformation operations do not consume this reviewed mapping."}
	fail := func() (ReviewRevision, ReviewedView, error) {
		return ReviewRevision{}, ReviewedView{}, errors.New("correlation review is incompatible, stale or invalid")
	}
	if opened == nil || report.Schema != ReportSchema || report.CaseIdentity != opened.Identity || r.Schema != ReviewSchema || r.Machine != newRevision(report).Machine || len(r.Decisions) > MaxDecisions || len(reviewBytes(r)) > MaxReviewBytes {
		return fail()
	}
	// Every revision has one more decision than its parent; the parent is the
	// exact previous prefix, so a missing/replaced history cannot silently pass.
	if len(r.Decisions) == 0 {
		if r.Parent != "" {
			return fail()
		}
	} else {
		prefix := newRevision(report)
		prefix.Decisions = r.Decisions[:len(r.Decisions)-1]
		if r.Parent != prefix.Identity() {
			return fail()
		}
	}
	for _, finding := range report.Collisions {
		view.Collisions = append(view.Collisions, ReviewedCollision{Finding: finding, TotalOccurrences: len(finding.Occurrences)})
	}
	for _, link := range report.Links {
		view.Links = append(view.Links, ReviewedLink{ID: link.ID, Linkage: string(link.Linkage), Rule: link.Rule, Occurrences: link.Occurrences, Status: "unreviewed"})
	}
	occurrences := map[string]Reference{}
	for _, event := range opened.Events {
		if event.Kind != bundle.Unparsed {
			occurrences[event.ID] = Reference{Occurrence: event.ID, SourceID: event.SourceID, Kind: string(event.Kind)}
		}
	}
	for i, d := range r.Decisions {
		if !reviewText(d.Actor, 256) || !reviewText(d.Reason, 1024) {
			return fail()
		}
		switch d.Action {
		case "accept", "reject":
			if d.From != "" || d.To != "" {
				return fail()
			}
			at := slices.IndexFunc(view.Links, func(link ReviewedLink) bool { return link.ID == d.Link })
			if at < 0 {
				return fail()
			}
			status := "accepted"
			if d.Action == "reject" {
				status = "rejected"
			}
			if view.Links[at].Status == status {
				return fail()
			}
			if status == "accepted" && view.Links[at].Linkage == "manual" && manualPairExists(view.Links, view.Links[at].Occurrences[0].Occurrence, view.Links[at].Occurrences[1].Occurrence) {
				return fail()
			}
			view.Links[at].Status = status
		case "add":
			from, ok := occurrences[d.From]
			to, exists := occurrences[d.To]
			if d.Link != "" || !ok || !exists || d.From == d.To {
				return fail()
			}
			if manualPairExists(view.Links, d.From, d.To) {
				return fail()
			}

			view.Links = append(view.Links, ReviewedLink{ID: fmt.Sprintf("manual-%06d", i+1), Linkage: "manual", Occurrences: []Reference{from, to}, Status: "accepted"})
		default:
			return fail()
		}
		if !showValues {
			d.Actor = ""
			d.Reason = ""
		}
		view.History = append(view.History, d)
	}
	return r, view, nil
}

// Decide refuses a stale derived-view identity before recording any new history.
func Decide(opened *bundle.Bundle, report Report, prior *ReviewRevision, mapping string, d Decision) (ReviewRevision, error) {
	r, view, err := Review(opened, report, prior, false)
	if err != nil {
		return ReviewRevision{}, err
	}
	if mapping == "" || mapping != view.Mapping {
		return ReviewRevision{}, errors.New("correlation mapping changed; discard dependent results and review again")
	}
	parent := r.Identity()
	r.Decisions = append(slices.Clone(r.Decisions), d)
	r.Parent = parent
	r, _, err = Review(opened, report, &r, false)
	return r, err
}

func DecodeReview(data []byte) (ReviewRevision, error) {
	var r ReviewRevision
	var required struct {
		Schema    *string     `json:"schema"`
		Machine   *string     `json:"machine"`
		Parent    *string     `json:"parent"`
		Decisions *[]Decision `json:"decisions"`
	}
	if len(data) > MaxReviewBytes || json.Unmarshal(data, &required) != nil || required.Schema == nil || required.Machine == nil || required.Parent == nil || required.Decisions == nil || json.Unmarshal(data, &r, json.RejectUnknownMembers(true)) != nil || r.Schema != ReviewSchema || len(r.Decisions) > MaxDecisions {
		return ReviewRevision{}, errors.New("invalid correlation review JSON")
	}
	return r, nil
}

// Rejecting a manual pair permits a later replacement; reaccepting the old one
// cannot create two simultaneously active decisions about that same pair.
func manualPairExists(links []ReviewedLink, from, to string) bool {
	for _, link := range links {
		if link.Linkage != "manual" || link.Status == "rejected" {
			continue
		}
		a, b := link.Occurrences[0].Occurrence, link.Occurrences[1].Occurrence
		if (a == from && b == to) || (a == to && b == from) {
			return true
		}
	}
	return false
}
