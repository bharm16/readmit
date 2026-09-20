package desktop

import (
	"context"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/operation"
)

// CorrelationReviewRequest selects an immutable history explicitly. Mapping
// binds a derived view to that selection; a mismatch invalidates it. Previous
// empty means the original machine mapping, never a hidden latest revision.
type CorrelationReviewRequest struct {
	RulesSHA256 string             `json:"rules_sha256"`
	Offset      int                `json:"offset"`
	Workspace   string             `json:"workspace"`
	Case        string             `json:"case"`
	Identity    string             `json:"identity"`
	Rules       string             `json:"rules"`
	Previous    string             `json:"previous"`
	Mapping     string             `json:"mapping"`
	ShowValues  bool               `json:"show_values"`
	Decision    correlate.Decision `json:"decision"`
	Output      string             `json:"output"`
}
type CorrelationReviewResult struct {
	State  State                   `json:"state"`
	Reason string                  `json:"reason,omitzero"`
	View   *correlate.ReviewedView `json:"view,omitzero"`
	Output string                  `json:"output,omitzero"`
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
// and never leaves an acknowledged write half-applied.
func (a *App) DecideCorrelation(request CorrelationReviewRequest) CorrelationReviewResult {
	return a.correlationReview(request, true)
}

func (a *App) correlationReview(request CorrelationReviewRequest, write bool) CorrelationReviewResult {
	return run(a, false, write, func(context.Context) CorrelationReviewResult {
		return a.reviewCorrelation(request, write)
	})
}

func (a *App) reviewCorrelation(request CorrelationReviewRequest, write bool) CorrelationReviewResult {
	failure := func(reason string) CorrelationReviewResult {
		return CorrelationReviewResult{State: Failed, Reason: reason}
	}
	if request.Offset < 0 {
		return failure("a correlation review window cannot begin before its first item")
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return CorrelationReviewResult{State: declined.state, Reason: declined.reason}
	}
	casePath, err := artifactpath.Child(root, request.Case)
	if err != nil {
		return failure("a case must be one directory entry of the open workspace")
	}
	opened, err := operation.OpenVerifiedCase(casePath, request.Identity)
	if err != nil {
		return failure(err.Error())
	}
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
		r, err = correlate.Decide(opened, *report, prior, request.Mapping, request.Decision)
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
