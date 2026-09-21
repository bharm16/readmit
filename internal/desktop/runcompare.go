package desktop

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/runcompare"
)

type RunComparisonRequest struct {
	Workspace string   `json:"workspace"`
	Baseline  string   `json:"baseline"`
	Current   string   `json:"current"`
	Approval  string   `json:"approval"`
	Repeats   []string `json:"repeats"`
}
type RunComparisonResult struct {
	State      State                  `json:"state"`
	Reason     string                 `json:"reason,omitzero"`
	Comparison *runcompare.Comparison `json:"comparison,omitzero"`
}

func (r *RunComparisonResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// CompareRuns reads only named workspace entries. Cancellation discards the
// view, never evidence; recovery is a fresh verification of the selected runs.
func (a *App) CompareRuns(request RunComparisonRequest) RunComparisonResult {
	return runNamed[RunComparisonResult, *RunComparisonResult](a, "run-comparison", true, false, func(ctx context.Context) RunComparisonResult {
		return a.compareRuns(ctx, request)
	})
}

func (a *App) compareRuns(ctx context.Context, request RunComparisonRequest) RunComparisonResult {
	failure := func() RunComparisonResult {
		return RunComparisonResult{State: Failed, Reason: "comparison requires verified workspace results or durable runs, an optional valid approval, and distinct complete repeat samples"}
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return RunComparisonResult{State: declined.state, Reason: declined.reason}
	}
	if len(request.Repeats) > runcompare.MaxRepeats {
		return failure()
	}
	input := runcompare.Input{}
	var err error
	input.Baseline, err = artifactpath.Child(root, request.Baseline)
	if err != nil {
		return failure()
	}
	input.Current, err = artifactpath.Child(root, request.Current)
	if err != nil {
		return failure()
	}
	for _, name := range request.Repeats {
		p, e := artifactpath.Child(root, name)
		if e != nil {
			return failure()
		}
		input.Repeats = append(input.Repeats, p)
	}
	if request.Approval != "" {
		if artifactpath.EntryName(request.Approval) != nil {
			return failure()
		}
		input.Approval = filepath.Join(root, request.Approval)
	}
	report, err := runcompare.Compare(ctx, input)
	if errors.Is(err, context.Canceled) {
		return RunComparisonResult{State: Cancelled, Reason: "comparison cancelled; retained evidence is unchanged"}
	}
	if err != nil {
		return failure()
	}
	return RunComparisonResult{State: Completed, Comparison: &report}
}
