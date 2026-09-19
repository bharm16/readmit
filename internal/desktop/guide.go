package desktop

import (
	"errors"

	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/testrunner"
)

// PracticeRequest is one practice run of the guided sample: the open workspace,
// the saved spec to execute, which fixture behaviour to execute it against, and
// the new entry the run is written into.
//
// Trial is one of the two run steps the guided sample declares. It selects the
// fixture's behaviour and nothing else: both trials send the same bytes of the
// same spec at the same built-in receiver, so what separates them is the defect,
// not the test.
type PracticeRequest struct {
	Workspace string `json:"workspace"`
	Spec      string `json:"spec"`
	Trial     string `json:"trial"`
	Output    string `json:"output"`
}

// PracticeAssertion is one expectation of the executed spec and what the run
// decided about it. It carries the identifier and the operator the spec
// declared and the outcome the runner's reader re-derived, and never the value
// that was observed: reading a value out of evidence is what the inspector is
// for, and a guided run is not a second way to display one.
type PracticeAssertion struct {
	ID       string `json:"id"`
	Operator string `json:"operator"`
	Status   string `json:"status"`
}

// Practice is what one practice run produced. Status is the runner's verdict for
// the whole spec; Identity and SpecIdentity are the retained result and the
// exact spec bytes it was decided from, so the entry this names can be matched
// to the spec in the workspace by something other than its name.
//
// ChangedBindings names every member of the saved spec a practice run rebinds
// onto its own directory. The list is stated rather than implied, because a run
// that silently repointed a test would be a run nobody could trust.
type Practice struct {
	Output          string              `json:"output"`
	Trial           string              `json:"trial"`
	Status          testrunner.Status   `json:"status"`
	Identity        string              `json:"identity"`
	SpecIdentity    string              `json:"spec_identity"`
	Assertions      []PracticeAssertion `json:"assertions"`
	ChangedBindings []string            `json:"changed_bindings"`
}

// PracticeResult separates facade success from the run's verdict, the way a
// durable run does: a completed operation reports a failing test, because a test
// that failed is a run that worked.
type PracticeResult struct {
	State    State     `json:"state"`
	Reason   string    `json:"reason,omitzero"`
	Practice *Practice `json:"practice,omitzero"`
}

// GuideResult carries one state. Guide is present whenever the folder could be
// read, including when nothing has been done in it: a guided sample nobody has
// started is a state somebody is in on the way through it, not a failure.
type GuideResult struct {
	State  State           `json:"state"`
	Reason string          `json:"reason,omitzero"`
	Guide  *guide.Progress `json:"guide,omitzero"`
}

// changedBindings is what a practice run rebinds onto its own directory: the
// endpoint, because the practice receiver chooses its own loopback port; the
// observation document, because the practice receiver writes its own; and the
// reference back to the case, because the executed copy of the spec sits one
// directory below the one the saved spec sits in. Everything a verdict depends
// on — the selected occurrences, the boundary and every assertion — is the
// saved spec's own and is never touched.
var changedBindings = []string{"target", "observation.path", "input.case"}

func (r refusal) guide() GuideResult { return GuideResult{State: r.state, Reason: r.reason} }

func (r refusal) practice() PracticeResult {
	return PracticeResult{State: r.state, Reason: r.reason}
}

// Guide reports the guided sample over the open workspace: the steps, what the
// folder shows about each of them, and the step to perform next.
//
// It is read back from the folder on every call. The shell keeps no tutorial
// state and writes no progress document, so a person who closes the window and
// reopens the folder — or who did a step from the command line — is told exactly
// what that folder really holds. It reads evidence and writes nothing.
func (a *App) Guide(workspace string) GuideResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.guide()
	}
	defer release()
	root, declined := resolveFolder(workspace)
	if root == "" {
		return declined.guide()
	}
	progress, err := guide.Read(root)
	if err != nil {
		return probeReadFailure(root).guide()
	}
	if progress.Next == guide.StepSample {
		return GuideResult{State: Empty, Reason: "this folder is not a sample workspace yet", Guide: &progress}
	}
	return GuideResult{State: Completed, Guide: &progress}
}

// RunPractice executes a saved regression test against the built-in practice
// receiver and writes the run into one new entry of the open workspace.
//
// This is the only operation in the window that sends: two synthetic messages
// over a loopback port the practice receiver binds in this process. No other
// host is reachable from it, the spec in the workspace is not rewritten, and the
// evidence the run retains is an ordinary result the command line reads.
// Cancel stops future sends; whatever was already written is retained where it
// was written and is reported as cancelled rather than as a verdict.
func (a *App) RunPractice(request PracticeRequest) PracticeResult {
	ctx, release, claimed := a.begin()
	if !claimed {
		return busyRefusal.practice()
	}
	defer release()
	if request.Trial != guide.StepBaseline && request.Trial != guide.StepPostFix {
		return PracticeResult{State: Failed, Reason: "a practice run is one of the two run steps the guided sample declares"}
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return declined.practice()
	}
	artifact, err := guide.Run(ctx, root, request.Spec, request.Output, request.Trial == guide.StepPostFix)
	switch {
	case errors.Is(err, guide.ErrCancelled):
		return PracticeResult{State: Cancelled, Reason: "the practice run was cancelled; whatever it retained is kept in the workspace"}
	case errors.Is(err, guide.ErrCannotWrite):
		// A folder this account cannot write is separated from the rest only
		// after the write has already failed, and the probe never touches the
		// destination.
		return probeWriteFailure(root,
			"this account cannot write into the open workspace",
			"the practice run could not be created in the open workspace").practice()
	case err != nil:
		return PracticeResult{State: Failed, Reason: err.Error()}
	}
	result := artifact.Result
	assertions := make([]PracticeAssertion, 0, len(result.Assertions))
	for _, evaluated := range result.Assertions {
		assertions = append(assertions, PracticeAssertion{
			ID: evaluated.Assertion.ID, Operator: evaluated.Assertion.Operator, Status: evaluated.Status,
		})
	}
	return PracticeResult{State: Completed, Practice: &Practice{
		Output: request.Output, Trial: request.Trial, Status: result.Status,
		Identity: artifact.Identity, SpecIdentity: result.SpecIdentity,
		Assertions: assertions, ChangedBindings: changedBindings,
	}}
}
