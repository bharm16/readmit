package assertion_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
)

// subjects and expectations restate, independently of the reader, which
// subject each operator reads and which expectation it takes. A set that
// decoded must satisfy both tables, so the reader cannot quietly widen a
// pairing without this test disagreeing.
var subjects = map[assertion.Operator]string{
	assertion.FieldEquals: "field", assertion.FieldNotEquals: "field", assertion.FieldState: "field",
	assertion.TextMatches: "field", assertion.NumericRange: "field", assertion.NumericTolerance: "field",
	assertion.DateWindow: "field", assertion.ValuesEqual: "pair", assertion.RecordCount: "collection",
	assertion.RecordsUnique: "collection", assertion.RecordsContain: "collection",
	assertion.RecordsOrdered: "collection", assertion.RecordMultiplicity: "collection",
	assertion.RecordsAbsent: "collection", assertion.RecordKeyMatches: "each",
	assertion.RecordsChanged: "transition",
}

var expectations = map[assertion.Operator]string{
	assertion.FieldEquals: "field", assertion.FieldNotEquals: "field", assertion.FieldState: "state",
	assertion.TextMatches: "pattern", assertion.NumericRange: "range", assertion.NumericTolerance: "tolerance",
	assertion.DateWindow: "window", assertion.ValuesEqual: "holds", assertion.RecordCount: "count",
	assertion.RecordsUnique: "holds", assertion.RecordsContain: "keys", assertion.RecordsOrdered: "keys",
	assertion.RecordMultiplicity: "multiplicity", assertion.RecordsAbsent: "holds",
	assertion.RecordKeyMatches: "pattern", assertion.RecordsChanged: "change",
}

func subjectMembers(subject assertion.Subject) []string {
	var named []string
	for member, present := range map[string]bool{
		"field": subject.Field != nil, "pair": subject.Pair != nil, "collection": subject.Collection != nil,
		"each": subject.Each != nil, "transition": subject.Transition != nil,
	} {
		if present {
			named = append(named, member)
		}
	}
	return named
}

func expectedMembers(expected assertion.Expected) []string {
	var named []string
	for member, present := range map[string]bool{
		"field": expected.Field != nil, "state": expected.State != nil, "pattern": expected.Pattern != nil,
		"range": expected.Range != nil, "tolerance": expected.Tolerance != nil, "window": expected.Window != nil,
		"holds": expected.Holds != nil, "count": expected.Count != nil, "keys": expected.Keys != nil,
		"multiplicity": expected.Multiplicity != nil, "change": expected.Change != nil,
	} {
		if present {
			named = append(named, member)
		}
	}
	return named
}

// FuzzAssertionSetDocument exercises the reader and the evaluator together:
// the strict decode that refuses unknown members, the nested
// presence-then-strict decoders, the closed operator table, every bound an
// accepted set is held to, and the evaluation of whatever survives.
//
// No input may panic, and no bytes at all may produce a set that pairs an
// operator with a subject or an expectation it does not take, that reports a
// pass while something failed, was undecided or was only skipped, that
// answers a collection question from an observation that did not complete, or
// that answers differently the second time it is asked. That is the contract
// stated as a property: if the reader cannot be talked into any of them, an
// unreadable value, an incomplete observation and an unmet condition can
// never acquire a passing verdict.
func FuzzAssertionSetDocument(f *testing.F) {
	positive, err := os.ReadFile(fixtureRoot + "assertion-set.json")
	if err != nil {
		f.Fatalf("read fixture: %v", err)
	}
	negative, err := os.ReadFile(fixtureRoot + "assertion-set-refused.json")
	if err != nil {
		f.Fatalf("read fixture: %v", err)
	}
	source := string(positive)
	for _, seed := range []string{
		source,
		string(negative),
		strings.Replace(source, "readmit-assertion-set/v1", "readmit-assertion-set/v2", 1),
		strings.Replace(source, `"operator": "field_equals"`, `"operator": "record_count"`, 1),
		strings.Replace(source, `"when": null,`, `"when": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "MSH-10"}, "equals": {"state": "omitted"}},`, 1),
		strings.Replace(source, `"selector": "MSA-1"`, `"selector": "MSA-1[2].3.4"`, 1),
		strings.Replace(source, `{"count": 2}`, `{"count": 5001}`, 1),
		strings.Replace(source, `"^MSG[0-9]{5}$"`, `"(((a{99}){99}){99})"`, 1),
		strings.Replace(source, `"assertions": [`, `"exec": "sh -c true", "assertions": [`, 1),
		`{"schema":"readmit-assertion-set/v1","name":"n","assertions":[]}`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		set, err := assertion.Decode(data)
		if err != nil {
			return
		}
		if set.Schema != assertion.Schema {
			t.Fatalf("accepted the schema %q", set.Schema)
		}
		if len(set.Assertions) == 0 || len(set.Assertions) > assertion.MaxAssertions {
			t.Fatalf("accepted %d assertions", len(set.Assertions))
		}
		ids := make(map[string]bool, len(set.Assertions))
		collections := make(map[string]bool, len(set.Assertions))
		for _, declared := range set.Assertions {
			if declared.ID == "" || ids[declared.ID] {
				t.Fatalf("accepted the assertion id %q", declared.ID)
			}
			ids[declared.ID] = true
			subject, expectation := subjectMembers(declared.Subject), expectedMembers(declared.Expected)
			if len(subject) != 1 || len(expectation) != 1 {
				t.Fatalf("accepted %q with %v and %v", declared.Operator, subject, expectation)
			}
			if subjects[declared.Operator] != subject[0] || expectations[declared.Operator] != expectation[0] {
				t.Fatalf("accepted %q reading %q and expecting %q", declared.Operator, subject[0], expectation[0])
			}
			collections[declared.ID] = subject[0] != "field" && subject[0] != "pair"
		}

		material := evidence(t)
		report, err := set.Evaluate(t.Context(), material)
		if err != nil {
			if reflect.DeepEqual(report, assertion.Report{}) {
				checkRefusals(t, set, material)
				return
			}
			t.Fatalf("an execution error produced a report: %v", err)
		}
		if report.Passed+report.Failed+report.Undecided+report.Skipped != len(report.Results) {
			t.Fatalf("counts %+v do not account for %d results", report, len(report.Results))
		}
		passing := report.Verdict == assertion.VerdictPass
		if passing != (report.Failed == 0 && report.Undecided == 0 && report.Passed > 0) {
			t.Fatalf("verdict %q with %+v", report.Verdict, report)
		}
		// Asking the same question of the same evidence answers the same way.
		again, err := set.Evaluate(t.Context(), material)
		if err != nil || !reflect.DeepEqual(report, again) {
			t.Fatalf("a second evaluation answered differently: %v", err)
		}

		// A collection question asked of an observation that did not complete
		// is never answered, however the set is written.
		material.Before.Complete, material.After.Complete = false, false
		incomplete, err := set.Evaluate(t.Context(), material)
		if err == nil {
			for _, result := range incomplete.Results {
				if collections[result.ID] && result.Outcome != assertion.OutcomeSkipped {
					t.Fatalf("%q answered %q from an observation that did not complete", result.ID, result.Outcome)
				}
			}
		} else if !reflect.DeepEqual(incomplete, assertion.Report{}) {
			t.Fatal("an execution error produced a report")
		}
		checkRefusals(t, set, material)
	})
}

// checkRefusals holds every accepted set to the two refusals that do not
// depend on what it says: a cancelled evaluation produces no verdict, and a
// set that did not come through the reader does not evaluate at all.
func checkRefusals(t *testing.T, set assertion.Set, material assertion.Evidence) {
	t.Helper()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if report, err := set.Evaluate(cancelled, material); err == nil || !reflect.DeepEqual(report, assertion.Report{}) {
		t.Fatal("a cancelled evaluation produced a verdict")
	}
	assembled := assertion.Set{Schema: set.Schema, Name: set.Name, Assertions: set.Assertions}
	if _, err := assembled.Evaluate(t.Context(), material); err == nil {
		t.Fatal("a set assembled in Go evaluated")
	}
}
