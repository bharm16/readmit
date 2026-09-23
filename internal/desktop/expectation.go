package desktop

import (
	"context"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/operation"
	"path/filepath"
)

// expectation uses the baseline panel's operation and privacy boundary. A
// release approval re-reads the candidate, profiles and predecessor together.
func (a *App) expectation(request BaselineRequest, approve, inspect bool) BaselineResult {
	return run(a, false, false, func(context.Context) BaselineResult {
		return a.applyExpectation(request, approve, inspect)
	})
}

func (a *App) applyExpectation(request BaselineRequest, approve, inspect bool) BaselineResult {
	failure := func(reason string) BaselineResult { return BaselineResult{State: Failed, Reason: reason} }
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return BaselineResult{State: declined.state, Reason: declined.reason}
	}
	previousPath := ""
	if request.Previous != "" {
		if artifactpath.EntryName(request.Previous) != nil {
			return failure("release must be one regular workspace entry")
		}
		previousPath = filepath.Join(root, request.Previous)
	}
	if inspect {
		if previousPath == "" {
			return failure("select a retained release")
		}
		previous, e := expectation.Read(previousPath)
		if e != nil {
			return failure(e.Error())
		}
		report, e := expectation.Inspect(previous, request.ShowValues)
		if e != nil {
			return failure(e.Error())
		}
		return BaselineResult{State: Completed, Comparison: &report, PreviousApprover: previous.Baseline.Approver, PreviousRationale: previous.Baseline.Rationale, ReleaseID: previous.ID}
	}
	if artifactpath.EntryName(request.Spec) != nil {
		return failure("specification must be one regular workspace entry")
	}
	paths := []string{}
	for _, name := range request.Profiles {
		if artifactpath.EntryName(name) != nil {
			return failure("profile must be one regular workspace entry")
		}
		paths = append(paths, filepath.Join(root, name))
	}
	outputPath := ""
	if approve {
		if artifactpath.EntryName(request.Output) != nil {
			return failure("release requires a new workspace entry")
		}
		outputPath = filepath.Join(root, request.Output)
	}
	op := operation.ExpectationRequest{ID: request.ReleaseID, Spec: filepath.Join(root, request.Spec), Previous: previousPath, Output: outputPath, Profiles: paths, ShowValues: request.ShowValues, Review: request.Review, Approver: request.Approver, Rationale: request.Rationale}
	var outcome operation.ExpectationResult
	var e error
	if approve {
		outcome, e = operation.ApproveExpectation(op)
	} else {
		outcome, e = operation.ReviewExpectation(op)
	}
	if e != nil {
		return failure(e.Error())
	}
	comparison := outcome.Comparison
	view := comparison.Baseline
	view.Schema = comparison.Schema
	view.Identity = comparison.Identity
	view.Parent = comparison.Parent
	view.Changes = append(view.Changes, comparison.Profiles...)
	result := BaselineResult{State: Completed, Comparison: &view}
	if outcome.Previous != nil {
		result.PreviousApprover = outcome.Previous.Baseline.Approver
		result.PreviousRationale = outcome.Previous.Baseline.Rationale
	}
	if approve {
		result.Output = request.Output
	}
	return result
}
