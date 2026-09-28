package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/correlate"
)

// CorrelationReviewRequest selects an immutable history explicitly. Mapping
// binds a derived view to that selection; a mismatch invalidates it. Previous
// empty means the original machine mapping, never a hidden latest revision.
//
// A review of a project names its link rules and its review as the project's
// objects instead: LinkRules is the exact link rule version, and the review
// is the one the project holds of exactly this case under exactly those
// rules. Review is the revision of it the window opened, which a decision
// must still find current; left empty, the window saw none. Each decision is
// one new revision of that review, submitted once under IntentID. Link
// narrows the view and its history to one relationship.
type CorrelationReviewRequest struct {
	Context     RequestContext     `json:"context,omitzero"`
	RulesSHA256 string             `json:"rules_sha256"`
	Offset      int                `json:"offset"`
	Workspace   string             `json:"workspace"`
	Case        string             `json:"case"`
	Identity    string             `json:"identity"`
	Rules       string             `json:"rules"`
	LinkRules   *ItemRef           `json:"link_rules,omitzero"`
	Review      *ItemRef           `json:"review,omitzero"`
	Link        string             `json:"link,omitzero"`
	IntentID    string             `json:"intent_id,omitzero"`
	Previous    string             `json:"previous"`
	Mapping     string             `json:"mapping"`
	ShowValues  bool               `json:"show_values"`
	Decision    correlate.Decision `json:"decision,omitzero"`
	Output      string             `json:"output"`
}

// CorrelationReviewResult answers one review read or decision. Review is the
// exact review revision the view reflects, and Saved the revision a decision
// published. Problems names what a refused decision lacks at its field: a
// decision with no reviewer to record it under is refused with a problem at
// ReviewerField.
type CorrelationReviewResult struct {
	State    State                   `json:"state"`
	Reason   string                  `json:"reason,omitzero"`
	Problems []FieldProblem          `json:"problems,omitzero"`
	Context  RequestContext          `json:"context"`
	View     *correlate.ReviewedView `json:"view,omitzero"`
	Review   *ItemRef                `json:"review,omitzero"`
	Saved    *ItemRef                `json:"saved,omitzero"`
	Output   string                  `json:"output,omitzero"`
}

func (r *CorrelationReviewResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// OpenCorrelationReview reconstructs the reviewed links, leaving original
// sequence findings and evidence intact. A caller reusing derived results must
// send their Mapping; a stale identity returns no view at all.
func (a *App) OpenCorrelationReview(request CorrelationReviewRequest) CorrelationReviewResult {
	return a.correlationReview(request, false)
}

// DecideCorrelation re-verifies all inputs and persists one decision in a new
// immutable revision. Like opening a sequence, this bounded local operation
// holds the shared slot and finishes once admitted; cancellation sends nothing
// and never leaves an acknowledged write half-applied. The facade stamps when
// the decision was recorded. The decision is recorded under the reviewer it
// names, or else the configured local Reviewer; with neither it is refused
// with a problem at ReviewerField and nothing is written. A reviewer is a
// local declaration, never an authenticated identity.
func (a *App) DecideCorrelation(request CorrelationReviewRequest) CorrelationReviewResult {
	return a.correlationReview(request, true)
}

func (a *App) correlationReview(request CorrelationReviewRequest, write bool) CorrelationReviewResult {
	return run(a, false, write, func(ctx context.Context) CorrelationReviewResult {
		var result CorrelationReviewResult
		if request.LinkRules != nil || request.Workspace == "" && request.Context.Project != "" {
			result = a.reviewNamed(ctx, request, write)
		} else {
			result = a.reviewCorrelation(request, write)
		}
		result.Context = request.Context
		return result
	})
}

// ReviewerField is the field a relationship decision's reviewer is asked for
// at, when it named none and no local Reviewer is configured.
const ReviewerField = "decision.actor"

// stamped is a decision as it is recorded: at the facade's own time, under
// the reviewer the window named, or else the configured local Reviewer. The
// account's own name is never put in its place: with no reviewer, nothing is
// recorded and the answer asks for one.
func (a *App) stamped(decision correlate.Decision) (correlate.Decision, *CorrelationReviewResult) {
	decision.At = a.now().UTC()
	if strings.TrimSpace(decision.Actor) == "" {
		decision.Actor = a.savedReviewer()
	}
	problem := ""
	switch {
	case decision.Actor == "":
		problem = "name the reviewer this decision is recorded under"
	case !printable(decision.Actor, maxReviewerBytes) || strings.TrimSpace(decision.Actor) == "":
		problem = "a reviewer name is at most 200 printable characters"
	default:
		return decision, nil
	}
	return decision, &CorrelationReviewResult{State: Failed, Reason: problem, Problems: []FieldProblem{{Field: ReviewerField, Problem: problem}}}
}

func (a *App) reviewCorrelation(request CorrelationReviewRequest, write bool) CorrelationReviewResult {
	failure := func(reason string) CorrelationReviewResult {
		return CorrelationReviewResult{State: Failed, Reason: reason}
	}
	if request.Offset < 0 {
		return failure("a correlation review window cannot begin before its first item")
	}
	if request.Review != nil {
		return failure("a named relationship review is opened with the link rules it was made under")
	}
	root, opened, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return CorrelationReviewResult{State: declined.state, Reason: declined.reason}
	}
	casePath := artifactpath.JoinReference(root, request.Case)
	report, declined := correlated(root, casePath, request.Rules)
	if report == nil {
		if declined.reason != "" {
			return CorrelationReviewResult{State: declined.state, Reason: declined.reason}
		}
		return failure("correlation review requires explicit rules")
	}
	if request.RulesSHA256 != "" && request.RulesSHA256 != report.RulesSHA256 {
		return failure("the displayed rules changed; reopen the sequence before reviewing")
	}
	var prior *correlate.ReviewRevision
	if request.Previous != "" {
		path, err := artifactpath.Child(root, request.Previous)
		if err != nil {
			return failure("a review must be one directory entry of the open workspace")
		}
		r, err := correlate.ReadReview(path, opened, *report)
		if err != nil {
			return failure(err.Error())
		}
		prior = &r
	}
	r, view, err := correlate.Review(opened, *report, prior, request.ShowValues)
	if err != nil {
		return failure(err.Error())
	}
	if request.Mapping != "" && request.Mapping != view.Mapping {
		return failure("correlation mapping changed; discard dependent results and review again")
	}
	result := CorrelationReviewResult{State: Completed, View: &view}
	if write {
		if artifactpath.EntryName(request.Output) != nil {
			return failure("a review is written to one new directory entry of the open workspace")
		}
		decision, refused := a.stamped(request.Decision)
		if refused != nil {
			return *refused
		}
		r, err = correlate.Decide(opened, *report, prior, request.Mapping, decision)
		if err != nil {
			return failure(err.Error())
		}
		if err = correlate.SaveReview(filepath.Join(root, request.Output), opened, *report, r); err != nil {
			return failure(err.Error())
		}
		_, view, err = correlate.Review(opened, *report, &r, request.ShowValues)
		if err != nil {
			return failure(err.Error())
		}
		result.View = &view
		result.Output = request.Output
	}
	windowCorrelationReview(result.View, request.Offset)
	return result
}

// reviewNamed reads or decides the relationship review a project holds of
// one case under one exact link rule version. A review made for any other
// case or rules is never applied; a decision publishes one new revision of
// the review, or nothing.
func (a *App) reviewNamed(ctx context.Context, request CorrelationReviewRequest, write bool) CorrelationReviewResult {
	result := CorrelationReviewResult{}
	failure := func(reason string) CorrelationReviewResult {
		result.refuse(Failed, reason)
		result.View = nil
		return result
	}
	if request.Offset < 0 {
		return failure("a correlation review window cannot begin before its first item")
	}
	if request.Rules != "" || request.Previous != "" || request.Output != "" {
		return failure("a named relationship review is read and saved in its project, never through workspace documents")
	}
	loaded, declined := a.loadCatalog(ctx, request.Context, write)
	if loaded == nil {
		return failure(declined.reason)
	}
	root, opened, declined := openedCase(loaded.root, request.Case, request.Identity)
	if root == "" {
		return failure(declined.reason)
	}
	// With no link rules, the review is of the case's recorded links.
	report := correlate.Recorded(opened)
	if request.LinkRules != nil {
		rules, _, _, err := loaded.linkRulesAt(*request.LinkRules)
		if err != nil {
			return failure(err.Error())
		}
		if report, err = correlate.Run(artifactpath.JoinReference(root, request.Case), rules); err != nil {
			return failure(err.Error())
		}
	}
	if request.RulesSHA256 != "" && request.RulesSHA256 != report.RulesSHA256 {
		return failure("the displayed rules changed; reopen the timeline before reviewing")
	}
	prior, bound := loaded.boundReview(report)
	if request.Review != nil {
		held, exact, err := loaded.reviewAt(*request.Review)
		if err != nil {
			return failure(err.Error())
		}
		if _, _, err := correlate.Review(opened, report, &held, false); err != nil {
			return failure("this relationship review was made for other link rules or another case; it is not applied")
		}
		if !write {
			prior, bound = &held, &exact
		}
	}
	if write {
		// The same click arriving again is answered with the revision it
		// already published, whatever was published since.
		for _, intent := range loaded.document.Intents {
			if intent.ID != request.IntentID || request.IntentID == "" {
				continue
			}
			if intent.Digest != decisionDigest(request) {
				return failure("this submission was already used for a different decision; nothing was saved")
			}
			replayed := ItemRef{Kind: LinkReviewItem, ID: intent.Item, Revision: revisionLabel(intent.Revision)}
			held, _, err := loaded.reviewAt(replayed)
			if err != nil {
				return failure(err.Error())
			}
			if _, view, err := correlate.Review(opened, report, &held, request.ShowValues); err == nil {
				if request.Link != "" {
					narrowReview(&view, request.Link)
				}
				windowCorrelationReview(&view, request.Offset)
				result.State, result.View, result.Review, result.Saved = Completed, &view, &replayed, &replayed
				return result
			}
			return failure("this relationship review was made for other link rules or another case; it is not applied")
		}
		if !sameRevision(request.Review, bound) {
			return failure("the relationship review changed since it was opened; nothing was saved")
		}
	}
	r, view, err := correlate.Review(opened, report, prior, request.ShowValues)
	if err != nil {
		return failure(err.Error())
	}
	if request.Mapping != "" && request.Mapping != view.Mapping {
		return failure("correlation mapping changed; discard dependent results and review again")
	}
	result.State, result.Review = Completed, bound
	if write && request.Decision.Action != correlate.AddDecision && !slices.ContainsFunc(view.Links, func(link correlate.ReviewedLink) bool { return link.ID == request.Decision.Link }) {
		return failure(unreviewableLink)
	}
	if write {
		if request.IntentID == "" {
			return failure("a decision is submitted once, under the identity of its submission")
		}
		decision, refused := a.stamped(request.Decision)
		if refused != nil {
			return *refused
		}
		if r, err = correlate.Decide(opened, report, prior, request.Mapping, decision); err != nil {
			return failure(err.Error())
		}
		saved, reason := a.publishReview(loaded, request, bound, r)
		if saved == nil {
			return failure(reason)
		}
		if _, view, err = correlate.Review(opened, report, &r, request.ShowValues); err != nil {
			return failure(err.Error())
		}
		result.Saved, result.Review = saved, saved
	}
	if request.Link != "" {
		narrowReview(&view, request.Link)
	}
	result.View = &view
	windowCorrelationReview(result.View, request.Offset)
	return result
}

// unreviewableLink refuses a decision about a relationship that is not a
// link: an acknowledgement or identifier that matched more than one
// occurrence stays as the evidence recorded it and is never accepted or
// rejected.
const unreviewableLink = "only a link is accepted, rejected or undone; an ambiguous relationship stays as the evidence recorded it, and Add link records the pair you mean"

// sameRevision reports whether the review revision a window opened is still
// the current one: none when there is none.
func sameRevision(opened, current *ItemRef) bool {
	if opened == nil || current == nil {
		return opened == nil && current == nil
	}
	return opened.Kind == current.Kind && opened.ID == current.ID && opened.Revision == current.Revision
}

// publishReview publishes one decision's revision of a review. A submission
// is recognized by what it asked for, so the same click arriving again is
// answered with the revision it already published.
func (a *App) publishReview(loaded *loadedCatalog, request CorrelationReviewRequest, bound *ItemRef, r correlate.ReviewRevision) (*ItemRef, string) {
	draft := catalog.Draft{Kind: string(LinkReviewItem), Intent: request.IntentID,
		Members: []catalog.Staged{{Role: string(LinkReviewItem), File: linkReviewFile, Data: correlate.EncodeReview(r)}}}
	if bound != nil {
		draft.ItemID, draft.Base = bound.ID, bound.Revision
	}
	draft.Digest = decisionDigest(request)
	saved, err := loaded.store.Save(draft, verifierFor(LinkReviewItem), catalog.Options{Now: a.now, Fault: a.saveFault})
	var conflict *catalog.Conflict
	switch {
	case errors.As(err, &conflict):
		return nil, "the relationship review changed since it was opened; nothing was saved"
	case errors.Is(err, catalog.ErrIntentReused):
		return nil, "this submission was already used for a different decision; nothing was saved"
	case errors.Is(err, catalog.ErrTooManyPending):
		return nil, "this project holds as many interrupted saves as it keeps; retry or discard one first. Nothing was saved"
	case err != nil:
		return nil, "the decision was not saved; the previous review is still current"
	}
	return &ItemRef{Kind: LinkReviewItem, ID: saved.Item.ID, Revision: revisionLabel(saved.Revision)}, ""
}

// decisionDigest is the digest of everything one decision's submission asks
// for: the review revision it began from, the mapping it saw and the
// decision itself. The time it is recorded at is the facade's, so a retry of
// the same click is the same submission.
func decisionDigest(request CorrelationReviewRequest) string {
	opened := ItemRef{}
	if request.Review != nil {
		opened = *request.Review
	}
	decision := request.Decision
	digest := sha256.New()
	rules := ItemRef{ID: "recorded"}
	if request.LinkRules != nil {
		rules = *request.LinkRules
	}
	for _, part := range []string{string(LinkReviewItem), request.Case, request.Identity, rules.ID, rules.Revision,
		opened.ID, opened.Revision, request.Mapping, string(decision.Action), decision.Link, decision.From, decision.To,
		strings.TrimSpace(decision.Actor), decision.Reason} {
		digest.Write([]byte(part + "\x00"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// narrowReview keeps one relationship of a reviewed view and the decisions
// about it: the ones naming it, and the one that added it.
func narrowReview(view *correlate.ReviewedView, link string) {
	view.Links = slices.DeleteFunc(view.Links, func(reviewed correlate.ReviewedLink) bool { return reviewed.ID != link })
	history := []correlate.Decision{}
	for i, decision := range view.History {
		if decision.Link == link || decision.Action == "add" && fmt.Sprintf("manual-%06d", i+1) == link {
			history = append(history, decision)
		}
	}
	view.History = history
}

// Windows bound both item counts and membership lists. Counts remain complete.
func windowCorrelationReview(view *correlate.ReviewedView, offset int) {
	view.Offset = offset
	view.TotalLinks = len(view.Links)
	view.TotalCollisions = len(view.Collisions)
	view.TotalDecisions = len(view.History)
	view.Links = reviewWindow(view.Links, offset)
	view.Collisions = reviewWindow(view.Collisions, offset)
	view.History = reviewWindow(view.History, offset)
	for i := range view.Links {
		view.Links[i].TotalOccurrences = len(view.Links[i].Occurrences)
		view.Links[i].Occurrences = view.Links[i].Occurrences[:min(len(view.Links[i].Occurrences), MaxRelatedOccurrences)]
	}
	for i := range view.Collisions {
		refs := view.Collisions[i].Finding.Occurrences
		view.Collisions[i].Finding.Occurrences = refs[:min(len(refs), MaxRelatedOccurrences)]
	}
}
func reviewWindow[T any](items []T, offset int) []T {
	start := min(offset, len(items))
	return items[start : start+min(MaxSequenceEvents, len(items)-start)]
}
