package reduce_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/reduce"
)

var assertionName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// FuzzReductionPlanDocument exercises the plan reader: the version read before
// the strict decode, the presence-then-strict pass that refuses unknown members
// in both the plan and the signature nested inside it, and every bound.
//
// The ticket's hardest promise is stated here as a property no bytes may break:
// **no document can declare a timeout, a cancellation, an execution error, an
// uncertain delivery or a pass as the failure to reduce towards**. A plan that
// decodes at all names exactly one class of failure, names at least one
// assertion of it, declares a grouping this release performs, carries
// correlation rules only where they mean something, and holds a budget and a
// confirmation count inside their bounds.
func FuzzReductionPlanDocument(f *testing.F) {
	identity, digest := strings.Repeat("a", 64), strings.Repeat("b", 64)
	complete := `{"schema":"readmit-reduction-plan/v1","case":"` + identity +
		`","grouping":"group-by-correlation/v1","rules":"` + digest +
		`","signature":{"state":"assertion_failed","assertions":["reschedule-accepted"]},` +
		`"trials":64,"confirmations":2}`
	for _, seed := range []string{
		complete,
		strings.Replace(complete, "plan/v1", "plan/v2", 1),
		strings.Replace(complete, `"group-by-correlation/v1","rules":"`+digest+`"`, `"group-per-occurrence/v1"`, 1),
		strings.Replace(complete, `"assertion_failed"`, `"timed_out"`, 1),
		strings.Replace(complete, `"assertion_failed"`, `"passed"`, 1),
		strings.Replace(complete, `["reschedule-accepted"]`, `[]`, 1),
		strings.Replace(complete, `"trials":64`, `"trials":0`, 1),
		strings.Replace(complete, `"confirmations":2`, `"confirmations":99`, 1),
		`{"schema":"readmit-reduction-plan/v1"}`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		plan, err := reduce.DecodePlan(data)
		if err != nil {
			return
		}
		if plan.Schema != reduce.PlanSchema {
			t.Fatalf("accepted the contract %q", plan.Schema)
		}
		if len(plan.Case) != 64 {
			t.Fatalf("accepted a plan bound to %q", plan.Case)
		}
		if plan.Signature.State != durablerun.AssertionFailed {
			t.Fatalf("accepted %q as the failure to reduce towards", plan.Signature.State)
		}
		if len(plan.Signature.Assertions) == 0 || len(plan.Signature.Assertions) > 256 {
			t.Fatalf("accepted a signature naming %d assertions", len(plan.Signature.Assertions))
		}
		seen := make(map[string]bool, len(plan.Signature.Assertions))
		for _, id := range plan.Signature.Assertions {
			if !assertionName.MatchString(id) || seen[id] {
				t.Fatalf("accepted the assertion identifier %q", id)
			}
			seen[id] = true
		}
		switch plan.Grouping {
		case reduce.GroupPerOccurrence:
			if plan.Rules != "" {
				t.Fatalf("accepted correlation rules on a grouping that relates nothing: %q", plan.Rules)
			}
		case reduce.GroupByCorrelation:
			if len(plan.Rules) != 64 {
				t.Fatalf("accepted a correlation grouping bound to %q", plan.Rules)
			}
		default:
			t.Fatalf("accepted the grouping %q", plan.Grouping)
		}
		if plan.Trials < 1 || plan.Trials > reduce.MaxTrials {
			t.Fatalf("accepted a budget of %d trials", plan.Trials)
		}
		if plan.Confirmations < 1 || plan.Confirmations > reduce.MaxConfirmations {
			t.Fatalf("accepted %d confirmations", plan.Confirmations)
		}
	})
}
