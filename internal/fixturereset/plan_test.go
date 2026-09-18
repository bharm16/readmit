package fixturereset

import (
	"strings"
	"testing"
)

const confirmAction = `{"id":"stop-listener","operator":"operator_confirms","authority":"none","instructions":"Stop the prior listener."}`

func planWith(actions ...string) string {
	return `{"schema":"readmit-reset-plan/v1","environment":"lab-siu","actions":[` + strings.Join(actions, ",") + `]}`
}

func TestDecodePlanReadsReviewedActionsAsWritten(t *testing.T) {
	document := planWith(confirmAction,
		`{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"The fresh listener exports an empty ledger.","observation":"observation.json"}`,
		`{"id":"endpoint-quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"The fixture accepts connections again."}`)
	plan, err := DecodePlan([]byte(document))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if plan.Schema != PlanSchema || plan.Environment != "lab-siu" || len(plan.Actions) != 3 {
		t.Fatalf("decoded %+v", plan)
	}
	for _, action := range plan.Actions {
		if reviewed[action.Operator] != action.Authority {
			t.Fatalf("action %q ran under %q but its operator requires %q", action.ID, action.Authority, reviewed[action.Operator])
		}
	}
	if !plan.RequiresConnection() {
		t.Fatal("a plan holding a connect-authority action must report that it needs a connection")
	}
	onlyLocal, err := DecodePlan([]byte(planWith(confirmAction)))
	if err != nil || onlyLocal.RequiresConnection() {
		t.Fatalf("a plan with no connect-authority action must need no connection: %+v %v", onlyLocal, err)
	}
}

// TestDecodePlanRefusesAnythingItCannotRunAsReviewed is the ticket's first hard
// rule read as a reader test: no document, however it is spelled, may name
// something this release did not review, and no member may carry code.
func TestDecodePlanRefusesAnythingItCannotRunAsReviewed(t *testing.T) {
	for name, document := range map[string]string{
		"unknown operator":       planWith(`{"id":"wipe","operator":"run_shell","authority":"none","instructions":"x"}`),
		"empty operator":         planWith(`{"id":"wipe","operator":"","authority":"none","instructions":"x"}`),
		"unreviewed authority":   planWith(`{"id":"wipe","operator":"operator_confirms","authority":"execute_program","instructions":"x"}`),
		"wider authority":        planWith(`{"id":"wipe","operator":"operator_confirms","authority":"connect_approved_target","instructions":"x"}`),
		"narrower authority":     planWith(`{"id":"wipe","operator":"endpoint_quiet","authority":"none","instructions":"x"}`),
		"command member":         planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x","command":"rm -rf /"}`),
		"script member":          planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x","script":"psql -c 'truncate appointments'"}`),
		"argv member":            planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x","argv":["sh","-c","true"]}`),
		"shell member":           planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x","shell":"/bin/sh"}`),
		"exec member":            planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x","exec":true}`),
		"env member":             planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none","instructions":"x","env":{"PATH":"/tmp"}}`),
		"plan level command":     `{"schema":"readmit-reset-plan/v1","environment":"lab-siu","command":"sh -c true","actions":[` + confirmAction + `]}`,
		"missing operator":       planWith(`{"id":"wipe","authority":"none","instructions":"x"}`),
		"missing authority":      planWith(`{"id":"wipe","operator":"operator_confirms","instructions":"x"}`),
		"missing instructions":   planWith(`{"id":"wipe","operator":"operator_confirms","authority":"none"}`),
		"later contract":         `{"schema":"readmit-reset-plan/v2","environment":"lab-siu","actions":[` + confirmAction + `]}`,
		"no schema":              `{"environment":"lab-siu","actions":[` + confirmAction + `]}`,
		"no actions":             `{"schema":"readmit-reset-plan/v1","environment":"lab-siu","actions":[]}`,
		"no environment":         `{"schema":"readmit-reset-plan/v1","environment":"","actions":[` + confirmAction + `]}`,
		"duplicate id":           planWith(confirmAction, confirmAction),
		"uppercase id":           planWith(`{"id":"Stop","operator":"operator_confirms","authority":"none","instructions":"x"}`),
		"id starting with digit": planWith(`{"id":"1stop","operator":"operator_confirms","authority":"none","instructions":"x"}`),
		"observation on a confirm action": planWith(
			`{"id":"stop","operator":"operator_confirms","authority":"none","instructions":"x","observation":"observation.json"}`),
		"observation on a connect action": planWith(
			`{"id":"quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"x","observation":"observation.json"}`),
		"read action without a file": planWith(
			`{"id":"ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x"}`),
		"observation escaping the plan directory": planWith(
			`{"id":"ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"../../etc/passwd"}`),
		"absolute observation": planWith(
			`{"id":"ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"/etc/passwd"}`),
		// A document somebody imported must not be able to drive the terminal
		// it is displayed on, so prose carries no control character beyond tab
		// and newline. The JSON below is well formed; its decoded text is not.
		"terminal escape in prose": planWith(
			"{\"id\":\"stop\",\"operator\":\"operator_confirms\",\"authority\":\"none\",\"instructions\":\"x\\u001b]0;owned\\u0007\"}"),
		"empty instructions": planWith(`{"id":"stop","operator":"operator_confirms","authority":"none","instructions":""}`),
	} {
		if _, err := DecodePlan([]byte(document)); err == nil {
			t.Errorf("%s: accepted a plan this release cannot run as reviewed", name)
		}
	}
}

// TestDecodePlanKeepsMultiStepProseReadable is the positive control for the
// prose bound: a person writing several steps for themselves uses tabs and
// newlines, and only the characters that drive a terminal are refused.
func TestDecodePlanKeepsMultiStepProseReadable(t *testing.T) {
	document := planWith("{\"id\":\"stop\",\"operator\":\"operator_confirms\",\"authority\":\"none\",\"instructions\":\"1. Stop the listener.\\n\\t2. Wait for it to exit.\"}")
	plan, err := DecodePlan([]byte(document))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(plan.Actions[0].Instructions, "\n\t2. Wait") {
		t.Fatalf("prose was not read as written: %q", plan.Actions[0].Instructions)
	}
}

func TestDecodePlanRefusesDocumentsPastItsBounds(t *testing.T) {
	actions := make([]string, 0, maxActions+1)
	for i := 0; i <= maxActions; i++ {
		actions = append(actions, `{"id":"stop-`+string(rune('a'+i%26))+string(rune('a'+i/26))+`","operator":"operator_confirms","authority":"none","instructions":"x"}`)
	}
	if _, err := DecodePlan([]byte(planWith(actions...))); err == nil {
		t.Error("accepted more actions than one plan may declare")
	}
	oversize := planWith(`{"id":"stop","operator":"operator_confirms","authority":"none","instructions":"` + strings.Repeat("x", MaxPlanBytes) + `"}`)
	if _, err := DecodePlan([]byte(oversize)); err == nil {
		t.Error("accepted a plan past its size limit")
	}
	longProse := planWith(`{"id":"stop","operator":"operator_confirms","authority":"none","instructions":"` + strings.Repeat("x", maxInstructions+1) + `"}`)
	if _, err := DecodePlan([]byte(longProse)); err == nil {
		t.Error("accepted instructions past their length limit")
	}
}

// TestReviewedTableIsTheClosedSet keeps the review itself honest: the operators
// a document may name and the authorities they run under are exactly what this
// test spells out, so adding either without a reviewer reading this file fails.
func TestReviewedTableIsTheClosedSet(t *testing.T) {
	expected := map[Operator]Authority{
		OperatorConfirms: NoAuthority,
		ObservationEmpty: ReadDeclaredFile,
		EndpointQuiet:    ConnectApprovedTarget,
	}
	if len(reviewed) != len(expected) {
		t.Fatalf("the reviewed set holds %d operators and this test reviewed %d", len(reviewed), len(expected))
	}
	for operator, authority := range expected {
		if reviewed[operator] != authority {
			t.Errorf("%s requires %q, reviewed as %q", operator, reviewed[operator], authority)
		}
	}
}
