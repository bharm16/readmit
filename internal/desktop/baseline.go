package desktop

import (
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/operation"
)

// BaselineRequest names workspace entries only. Review is the explicit local
// decision; the engine re-reads both files before accepting it.
type BaselineRequest struct {
	Release    bool     `json:"release"`
	ReleaseID  string   `json:"release_id"`
	Profiles   []string `json:"profiles"`
	Workspace  string   `json:"workspace"`
	Spec       string   `json:"spec"`
	Previous   string   `json:"previous"`
	ShowValues bool     `json:"show_values"`
	Review     string   `json:"review"`
	Approver   string   `json:"approver"`
	Rationale  string   `json:"rationale"`
	Output     string   `json:"output"`
}
type BaselineResult struct {
	State             State                `json:"state"`
	Reason            string               `json:"reason,omitzero"`
	Comparison        *baseline.Comparison `json:"comparison,omitzero"`
	PreviousApprover  string               `json:"previous_approver,omitzero"`
	PreviousRationale string               `json:"previous_rationale,omitzero"`
	Output            string               `json:"output,omitzero"`
}

// ReviewBaseline and ApproveBaseline share the same bounded local reader.
// They hold the operation slot and finish once admitted; Cancel cannot interrupt
// the short exclusive file write, and no operation sends or resumes a run.
func (a *App) ReviewBaseline(request BaselineRequest) BaselineResult {
	if request.Release {
		return a.expectation(request, false, false)
	}
	return a.baseline(request, false)
}
func (a *App) ApproveBaseline(request BaselineRequest) BaselineResult {
	if err := a.admitAuthor(); err != nil {
		return BaselineResult{State: PermissionDenied, Reason: err.Error()}
	}
	if request.Release {
		return a.expectation(request, true, false)
	}
	return a.baseline(request, true)
}

// OpenBaseline inspects a retained revision even when its authored spec is gone.
func (a *App) OpenBaseline(request BaselineRequest) BaselineResult {
	if request.Release {
		return a.expectation(request, false, true)
	}
	release, ok := a.claim()
	if !ok {
		return BaselineResult{State: Busy, Reason: busyRefusal.reason}
	}
	defer release()
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return BaselineResult{State: declined.state, Reason: declined.reason}
	}
	if artifactpath.EntryName(request.Previous) != nil {
		return BaselineResult{State: Failed, Reason: "a baseline must be one regular entry of the open workspace"}
	}
	revision, err := baseline.Read(filepath.Join(root, request.Previous))
	if err != nil {
		return BaselineResult{State: Failed, Reason: err.Error()}
	}
	report, err := baseline.Inspect(revision, request.ShowValues)
	if err != nil {
		return BaselineResult{State: Failed, Reason: err.Error()}
	}
	return BaselineResult{State: Completed, Comparison: &report, PreviousApprover: revision.Approver, PreviousRationale: revision.Rationale}
}

func (a *App) baseline(request BaselineRequest, approve bool) BaselineResult {
	release, ok := a.claim()
	if !ok {
		return BaselineResult{State: Busy, Reason: busyRefusal.reason}
	}
	defer release()
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return BaselineResult{State: declined.state, Reason: declined.reason}
	}
	failure := func(reason string) BaselineResult { return BaselineResult{State: Failed, Reason: reason} }
	if artifactpath.EntryName(request.Spec) != nil {
		return failure("a specification must be one regular entry of the open workspace")
	}
	previousPath := ""
	if request.Previous != "" {
		if artifactpath.EntryName(request.Previous) != nil {
			return failure("a previous baseline must be one regular entry of the open workspace")
		}
		previousPath = filepath.Join(root, request.Previous)
	}
	outputPath := ""
	if approve {
		if artifactpath.EntryName(request.Output) != nil {
			return failure("a baseline is written to one new entry of the open workspace")
		}
		outputPath = filepath.Join(root, request.Output)
	}
	op := operation.BaselineRequest{Spec: filepath.Join(root, request.Spec), Previous: previousPath, Output: outputPath, ShowValues: request.ShowValues, Review: request.Review, Approver: request.Approver, Rationale: request.Rationale}
	var outcome operation.BaselineResult
	var err error
	if approve {
		outcome, err = operation.ApproveBaseline(op)
	} else {
		outcome, err = operation.ReviewBaseline(op)
	}
	if err != nil {
		return failure(err.Error())
	}
	result := BaselineResult{State: Completed, Comparison: &outcome.Comparison}
	if outcome.Previous != nil {
		result.PreviousApprover = outcome.Previous.Approver
		result.PreviousRationale = outcome.Previous.Rationale
	}
	if approve {
		result.Output = request.Output
	}
	return result
}
