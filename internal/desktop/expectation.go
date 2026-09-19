package desktop

import (
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/testrunner"
	"path/filepath"
)

// expectation uses the baseline panel's operation and privacy boundary. A
// release approval re-reads the candidate, profiles and predecessor together.
func (a *App) expectation(request BaselineRequest, approve, inspect bool) BaselineResult {
	release, ok := a.claim()
	if !ok {
		return BaselineResult{State: Busy, Reason: busyRefusal.reason}
	}
	defer release()
	failure := func(reason string) BaselineResult { return BaselineResult{State: Failed, Reason: reason} }
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return BaselineResult{State: declined.state, Reason: declined.reason}
	}
	var previous *expectation.Release
	if request.Previous != "" {
		if artifactpath.EntryName(request.Previous) != nil {
			return failure("release must be one regular workspace entry")
		}
		r, e := expectation.Read(filepath.Join(root, request.Previous))
		if e != nil {
			return failure(e.Error())
		}
		previous = &r
	}
	if inspect {
		if previous == nil {
			return failure("select a retained release")
		}
		report, e := expectation.Inspect(*previous, request.ShowValues)
		if e != nil {
			return failure(e.Error())
		}
		return BaselineResult{State: Completed, Comparison: &report, PreviousApprover: previous.Baseline.Approver, PreviousRationale: previous.Baseline.Rationale}
	}
	if artifactpath.EntryName(request.Spec) != nil {
		return failure("specification must be one regular workspace entry")
	}
	raw, e := baseline.ReadBytes(filepath.Join(root, request.Spec), testrunner.MaxSpecBytes)
	if e != nil {
		return failure(e.Error())
	}
	paths := []string{}
	for _, name := range request.Profiles {
		if artifactpath.EntryName(name) != nil {
			return failure("profile must be one regular workspace entry")
		}
		paths = append(paths, filepath.Join(root, name))
	}
	pins, e := expectation.ReadProfiles(paths)
	if e != nil {
		return failure(e.Error())
	}
	comparison, e := expectation.Review(request.ReleaseID, raw, pins, previous, request.ShowValues)
	if e != nil {
		return failure(e.Error())
	}
	view := comparison.Baseline
	view.Schema = comparison.Schema
	view.Identity = comparison.Identity
	view.Parent = comparison.Parent
	view.Changes = append(view.Changes, comparison.Profiles...)
	result := BaselineResult{State: Completed, Comparison: &view}
	if previous != nil {
		result.PreviousApprover = previous.Baseline.Approver
		result.PreviousRationale = previous.Baseline.Rationale
	}
	if approve {
		if artifactpath.EntryName(request.Output) != nil {
			return failure("release requires a new workspace entry")
		}
		r, e := expectation.Approve(request.ReleaseID, raw, pins, previous, request.Review, request.Approver, request.Rationale)
		if e != nil {
			return failure(e.Error())
		}
		if e = expectation.Save(filepath.Join(root, request.Output), r); e != nil {
			return failure(e.Error())
		}
		result.Output = request.Output
	}
	return result
}
