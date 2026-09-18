package fixturereset

import (
	"path/filepath"
	"testing"
)

// FuzzResetPlanDocument exercises the reset-plan reader: the strict decode that
// refuses unknown members, the nested action decoder's presence and
// unknown-member passes, and every bound an accepted action is held to.
//
// No input may panic, and no bytes at all may produce a plan naming an operator
// this release did not review or declaring an authority other than the one that
// operator requires. That is the ticket's first hard rule stated as a property:
// if the reader cannot be talked into either, then no imported document can
// reach anything a reviewer did not read.
func FuzzResetPlanDocument(f *testing.F) {
	for _, seed := range []string{
		planWith(confirmAction),
		planWith(`{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"observation.json"}`),
		planWith(`{"id":"endpoint-quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"x"}`),
		planWith(`{"id":"wipe","operator":"run_shell","authority":"none","instructions":"rm -rf /"}`),
		planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x","command":"sh -c true"}`),
		planWith(`{"id":"wipe","operator":"operator_confirms","authority":"connect_approved_target","instructions":"x"}`),
		planWith(`{"id":"ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"../../etc/passwd"}`),
		`{"schema":"readmit-reset-plan/v2","environment":"lab-siu","actions":[` + confirmAction + `]}`,
		`{"schema":"readmit-reset-plan/v1","environment":"lab-siu","actions":[]}`,
		`{"schema":"readmit-reset-plan/v1","environment":"lab-siu"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		plan, err := DecodePlan(data)
		if err != nil {
			return
		}
		if plan.Schema != PlanSchema || len(plan.Actions) == 0 || len(plan.Actions) > maxActions {
			t.Fatalf("accepted %+v", plan)
		}
		if err := environmentName(plan.Environment); err != nil {
			t.Fatalf("accepted an environment name the configuration could never match: %v", err)
		}
		for _, action := range plan.Actions {
			required, reviewedOperator := reviewed[action.Operator]
			if !reviewedOperator {
				t.Fatalf("accepted the unreviewed operator %q", action.Operator)
			}
			if action.Authority != required {
				t.Fatalf("accepted %q under %q, but it requires %q", action.Operator, action.Authority, required)
			}
			if (action.Observation != "") != (action.Operator == ObservationEmpty) {
				t.Fatalf("accepted %q with observation %q; only a read-authority action names a file", action.Operator, action.Observation)
			}
			if action.Observation != "" && !filepath.IsLocal(action.Observation) {
				t.Fatalf("accepted the observation %q, which leaves the plan's own directory", action.Observation)
			}
			if action.Authority == ConnectApprovedTarget && !plan.RequiresConnection() {
				t.Fatal("a plan holding a connect-authority action must report that it needs a connection")
			}
		}
	})
}
