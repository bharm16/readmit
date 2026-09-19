package desktop

import (
	"errors"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
)

// DurableRunResult separates facade success from the run's execution state.
// A completed read can report an interrupted or assertion-failed run.
type DurableRunResult struct {
	State  State               `json:"state"`
	Reason string              `json:"reason,omitzero"`
	Run    *durablerun.Summary `json:"run,omitzero"`
}

// StartDurableRun sends once with an explicit operator action, retaining a new
// journal directory. Cancel stops future sends; in-flight effects remain visible.
func (a *App) StartDurableRun(spec, output string) DurableRunResult {
	ctx, release, claimed := a.begin()
	if !claimed {
		return DurableRunResult{State: Busy, Reason: busyRefusal.reason}
	}
	defer release()
	result, err := durablerun.Start(ctx, spec, output)
	if err != nil {
		if result.Schema != "" {
			return DurableRunResult{State: Failed, Run: &result, Reason: "journal persistence failed; recover retained output before any new execution"}
		}
		return DurableRunResult{State: Failed, Reason: "the durable run could not finish; recover the retained output to inspect partial evidence"}
	}
	return DurableRunResult{State: Completed, Run: &result}
}

// OpenDurableRun is read-only recovery and never acquires send authority.
func (a *App) OpenDurableRun(path string) DurableRunResult {
	release, claimed := a.claim()
	if !claimed {
		return DurableRunResult{State: Busy, Reason: busyRefusal.reason}
	}
	defer release()
	result, err := durablerun.Open(path)
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		return DurableRunResult{State: Failed, Reason: "the durable run was evaluated by a version this release cannot read; its evidence has not been changed"}
	}
	if err != nil {
		return DurableRunResult{State: Failed, Reason: "the durable run could not be verified; partial evidence has not been changed"}
	}
	return DurableRunResult{State: Completed, Run: &result}
}
