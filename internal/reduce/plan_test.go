package reduce_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/reduce"
)

const caseIdentity = "7d3cd0690000000000000000000000000000000000000000000000000000abcd"
const rulesIdentity = "f60d58880000000000000000000000000000000000000000000000000000abcd"

// document is the one well-formed plan every refusal below is a single change
// away from, so each case proves the member it names and nothing else.
func document(members ...string) string {
	declared := map[string]string{
		"schema":        `"schema":"readmit-reduction-plan/v1"`,
		"case":          `"case":"` + caseIdentity + `"`,
		"grouping":      `"grouping":"group-per-occurrence/v1"`,
		"signature":     `"signature":{"state":"assertion_failed","assertions":["reschedule-accepted"]}`,
		"trials":        `"trials":64`,
		"confirmations": `"confirmations":2`,
	}
	for _, replacement := range members {
		name, value, _ := strings.Cut(replacement, "=")
		if value == "" {
			delete(declared, name)
			continue
		}
		declared[name] = value
	}
	parts := make([]string, 0, len(declared))
	for _, name := range []string{"schema", "case", "grouping", "rules", "signature", "trials", "confirmations", "extra"} {
		if value, ok := declared[name]; ok {
			parts = append(parts, value)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// TestTheControlPlanIsValid keeps every refusal below meaningful.
func TestTheControlPlanIsValid(t *testing.T) {
	plan, err := reduce.DecodePlan([]byte(document()))
	if err != nil {
		t.Fatalf("the control plan must decode, otherwise the refusals prove nothing: %v", err)
	}
	if plan.Trials != 64 || plan.Confirmations != 2 || plan.Grouping != reduce.GroupPerOccurrence {
		t.Fatalf("a decoded plan is what it declared: %+v", plan)
	}
	if plan.Signature.State != durablerun.AssertionFailed {
		t.Fatalf("a decoded signature is what it declared: %+v", plan.Signature)
	}
	grouped, err := reduce.DecodePlan([]byte(document(
		`grouping="grouping":"group-by-correlation/v1"`, `rules="rules":"`+rulesIdentity+`"`)))
	if err != nil || grouped.Rules != rulesIdentity {
		t.Fatalf("grouping by correlation names the rules whose relations form the groups: %+v %v", grouped, err)
	}
}

// TestNoTimeoutCancellationOrErrorCanBeDeclaredAsTheFailureToPreserve is the
// ticket's own rule inside the contract rather than beside it. A reduction is
// about an assertion that failed; a run that stopped for any other reason
// establishes nothing about an expectation, so it cannot be named as the thing
// to reduce towards.
func TestNoTimeoutCancellationOrErrorCanBeDeclaredAsTheFailureToPreserve(t *testing.T) {
	for _, state := range []durablerun.State{
		durablerun.Passed, durablerun.TimedOut, durablerun.Cancelled, durablerun.Interrupted,
		durablerun.ExecutionError, durablerun.DeliveryUncertain, durablerun.Ready, durablerun.Running, "",
	} {
		t.Run(string(state)+"_", func(t *testing.T) {
			declared := `signature="signature":{"state":"` + string(state) + `","assertions":["reschedule-accepted"]}`
			if _, err := reduce.DecodePlan([]byte(document(declared))); err == nil {
				t.Fatalf("a plan declared %q as the failure to preserve", state)
			}
		})
	}
}

// TestAPlanReaderRefusesWhatItCannotMean is the contract boundary: unknown
// versions, unknown members, omitted members and every bound.
func TestAPlanReaderRefusesWhatItCannotMean(t *testing.T) {
	for name, plan := range map[string]string{
		"a later contract version":                   document(`schema="schema":"readmit-reduction-plan/v2"`),
		"no contract version":                        document("schema="),
		"a member this contract has not":             document(`extra="reset":"reset.json"`),
		"an omitted case":                            document("case="),
		"an omitted grouping":                        document("grouping="),
		"an omitted signature":                       document("signature="),
		"an omitted budget":                          document("trials="),
		"an omitted confirmation count":              document("confirmations="),
		"a case that is not an identity":             document(`case="case":"incident-4821"`),
		"a grouping nobody reviewed":                 document(`grouping="grouping":"group-by-patient/v1"`),
		"rules a per-occurrence grouping cannot use": document(`rules="rules":"` + rulesIdentity + `"`),
		"a correlation grouping naming no rules":     document(`grouping="grouping":"group-by-correlation/v1"`),
		"a correlation grouping naming a name":       document(`grouping="grouping":"group-by-correlation/v1"`, `rules="rules":"interface.rules.json"`),
		"no trials at all":                           document(`trials="trials":0`),
		"more trials than this release spends":       document(`trials="trials":513`),
		"no confirmation at all":                     document(`confirmations="confirmations":0`),
		"more confirmations than it holds":           document(`confirmations="confirmations":9`),
		"a signature naming no assertion":            document(`signature="signature":{"state":"assertion_failed","assertions":[]}`),
		"a signature naming one twice":               document(`signature="signature":{"state":"assertion_failed","assertions":["a","a"]}`),
		"a signature naming an unreadable id":        document(`signature="signature":{"state":"assertion_failed","assertions":["Reschedule Accepted"]}`),
		"a signature with a member of its own":       document(`signature="signature":{"state":"assertion_failed","assertions":["a"],"tolerance":2}`),
		"a signature declaring no state":             document(`signature="signature":{"assertions":["a"]}`),
		"a signature declaring no assertions":        document(`signature="signature":{"state":"assertion_failed"}`),
		"nothing at all":                             "",
		"something that is not a document":           "[]",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := reduce.DecodePlan([]byte(plan)); err == nil {
				t.Fatal("a reduction plan accepted a document it cannot mean")
			}
		})
	}
	oversized := `{"schema":"readmit-reduction-plan/v1","case":"` + strings.Repeat("a", reduce.MaxPlanBytes) + `"}`
	if _, err := reduce.DecodePlan([]byte(oversized)); err == nil {
		t.Fatal("a reduction plan past its size limit was read rather than refused")
	}
}
