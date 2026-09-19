package transform_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/transform"
)

// FuzzTransformPlanDocument exercises the plan reader: the version read before
// the strict decode, the presence-then-strict pass that refuses unknown
// members, and every member each typed operator declares.
//
// No input may panic, and no bytes at all may produce a plan that names an
// operator outside the closed set, carries a member another operator owns, or
// declares an entry, a position, a rule or a shift this release does not
// perform. That is the ticket's promise stated as a property: a transformation
// step cannot be talked into becoming a script, and a plan cannot be talked out
// of naming the evidence and the declarations it preserves.
var entryName = regexp.MustCompile(`^t[0-9]{6}$`)

func FuzzTransformPlanDocument(f *testing.F) {
	identity, digest := strings.Repeat("a", 64), strings.Repeat("b", 64)
	complete := `{"schema":"readmit-transform-plan/v1","case":"` + identity + `","rules":"` + digest +
		`","profile":{"id":"fixture-siu","version":"1"},"steps":[` +
		`{"operator":"rebase-identifiers/v1","rule":"patient"},` +
		`{"operator":"shift-dates/v1","shift":"24h"},` +
		`{"operator":"reorder-occurrence/v1","entry":"t000002","position":1},` +
		`{"operator":"duplicate-occurrence/v1","entry":"t000001"},` +
		`{"operator":"drop-occurrence/v1","entry":"t000003"}]}`
	for _, seed := range []string{
		complete,
		strings.Replace(complete, "plan/v1", "plan/v2", 1),
		strings.Replace(complete, `"shift":"24h"`, `"shift":"90ms"`, 1),
		strings.Replace(complete, `"entry":"t000001"`, `"entry":"s0001-e000001"`, 1),
		strings.Replace(complete, `"profile":{"id":"fixture-siu","version":"1"},`, "", 1),
		`{"schema":"readmit-transform-plan/v1","case":"` + identity + `","rules":"` + digest + `","steps":[]}`,
		`{"schema":"readmit-transform-plan/v1"}`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	operators := []string{
		transform.DropOccurrence, transform.DuplicateOccurrence, transform.ReorderOccurrence,
		transform.RebaseIdentifiers, transform.ShiftDates,
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		plan, err := transform.DecodePlan(data)
		if err != nil {
			return
		}
		if plan.Schema != transform.PlanSchema {
			t.Fatalf("accepted the contract %q", plan.Schema)
		}
		if len(plan.Case) != 64 || len(plan.Rules) != 64 {
			t.Fatalf("accepted a plan bound to %q and %q", plan.Case, plan.Rules)
		}
		if len(plan.Steps) > transform.MaxSteps {
			t.Fatalf("accepted %d steps", len(plan.Steps))
		}
		for _, step := range plan.Steps {
			if !slices.Contains(operators, step.Operator) {
				t.Fatalf("accepted the operator %q", step.Operator)
			}
			if step.Operator == transform.ShiftDates {
				if _, err := transform.ParseShift(step.Shift); err != nil {
					t.Fatalf("accepted the shift %q", step.Shift)
				}
				if step.Entry != "" || step.Position != 0 || step.Rule != "" {
					t.Fatalf("accepted a shift carrying another operator's members: %+v", step)
				}
				continue
			}
			if step.Shift != "" {
				t.Fatalf("accepted a shift on the operator %q", step.Operator)
			}
			if step.Operator == transform.RebaseIdentifiers {
				if step.Rule == "" || step.Entry != "" || step.Position != 0 {
					t.Fatalf("accepted a rename carrying another operator's members: %+v", step)
				}
				continue
			}
			if step.Rule != "" || !entryName.MatchString(step.Entry) {
				t.Fatalf("accepted a sequence step carrying another operator's members: %+v", step)
			}
			reordering := step.Operator == transform.ReorderOccurrence
			if reordering != (step.Position != 0) {
				t.Fatalf("accepted a position only a reorder declares: %+v", step)
			}
			if reordering && step.Position > transform.MaxEntries {
				t.Fatalf("accepted the position %d", step.Position)
			}
		}
	})
}
