package desktop

import (
	"context"
	"errors"
	"testing"

	"github.com/bharm16/readmit/internal/redact"
)

// A send ends in exactly one state, and only a matched assessment completes.
// A changed or unavailable phase is refused in the assessment's own reason, as
// the command line refuses it, a cancellation is its own state and says
// whether anything could have been sent, and every state that retained a job
// still carries it.
func TestAReexecutionCompletesOnlyWhenItsAssessmentMatched(t *testing.T) {
	assessed := func(criteria string) redact.ReexecutionAssessment {
		return redact.ReexecutionAssessment{Criteria: criteria, ExternalEquivalence: "declined", Reason: "assessed " + criteria}
	}
	job := func(criteria string) *ReexecutionOutcome {
		return &ReexecutionOutcome{Job: "reexecution-001", Assessment: assessed(criteria)}
	}
	refused := errors.New("reexecution inputs changed after preview; prepare and authorize again")
	for name, check := range map[string]struct {
		cancelled bool
		outcome   *ReexecutionOutcome
		err       error
		state     State
		reason    string
		retained  bool
	}{
		"matched":                            {false, job("matched"), nil, Completed, "", true},
		"changed":                            {false, job("changed"), nil, Failed, "assessed changed", true},
		"unavailable or unstable":            {false, job("unavailable-or-unstable"), nil, Failed, "assessed unavailable-or-unstable", true},
		"refused before any job":             {false, nil, refused, Failed, refused.Error(), false},
		"refused after the job was retained": {false, job("unavailable-or-unstable"), refused, Failed, refused.Error(), true},
		"cancelled before any job":           {true, nil, context.Canceled, Cancelled, "the reexecution was cancelled before anything was sent; no job was created", false},
		"cancelled with a retained job": {true, job("unavailable-or-unstable"), nil, Cancelled,
			"the reexecution was cancelled; the job retains what happened, and whatever may already have been delivered is never resent — reconcile it at the target before a separately authorized new attempt", true},
	} {
		assessment := assessed("unavailable-or-unstable")
		if check.outcome != nil {
			assessment = check.outcome.Assessment
		}
		got := answerReexecution(check.cancelled, assessment, check.outcome, check.err)
		if got.State != check.state || got.Reason != check.reason || (got.Outcome != nil) != check.retained {
			t.Errorf("%s: %+v, want %s %q with a retained job %v", name, got, check.state, check.reason, check.retained)
		}
	}
}
