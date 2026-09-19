package assertion_test

import (
	"encoding/json/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/testrunner"
)

const fixtureRoot = "../../testdata/fixtures/"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureRoot + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// mutate applies one textual edit to the positive fixture and fails the test if
// the edit did not land, so a refusal is never proven against unchanged bytes.
func mutate(t *testing.T, old, replacement string) []byte {
	t.Helper()
	source := string(fixture(t, "assertion-set.json"))
	if !strings.Contains(source, old) {
		t.Fatalf("the fixture does not contain %q", old)
	}
	return []byte(strings.Replace(source, old, replacement, 1))
}

// edit decodes the positive fixture loosely, lets the test reshape it, and
// encodes it again, for refusals a single textual edit cannot express.
func edit(t *testing.T, reshape func(document map[string]any)) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(fixture(t, "assertion-set.json"), &document); err != nil {
		t.Fatalf("decode fixture loosely: %v", err)
	}
	reshape(document)
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode edited fixture: %v", err)
	}
	return data
}

func assertions(t *testing.T, document map[string]any) []any {
	t.Helper()
	list, ok := document["assertions"].([]any)
	if !ok || len(list) == 0 {
		t.Fatal("the fixture holds no assertions")
	}
	return list
}

func first(t *testing.T, document map[string]any) map[string]any {
	t.Helper()
	assertion, ok := assertions(t, document)[0].(map[string]any)
	if !ok {
		t.Fatal("the fixture's first assertion is not an object")
	}
	return assertion
}

func TestDecodeReadsTheFixtureAsWritten(t *testing.T) {
	set, err := assertion.Decode(fixture(t, "assertion-set.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if set.Schema != assertion.Schema {
		t.Fatalf("decoded schema %q", set.Schema)
	}
	named := make(map[assertion.Operator]bool, len(set.Assertions))
	for _, declared := range set.Assertions {
		named[declared.Operator] = true
	}
	// Every operator this contract carries is exercised by the fixture, so a
	// new operator cannot be added without evidence that it reads. The set is
	// taken from the tables the fuzz target restates independently of the
	// reader, not from the reader's own.
	for operator := range subjects {
		if !named[operator] {
			t.Errorf("the fixture declares no %q assertion", operator)
		}
	}
	if len(named) != len(subjects) {
		t.Fatalf("the fixture names %d operators of %d", len(named), len(subjects))
	}
}

func TestDecodeRefusesTheRefusedFixtureByName(t *testing.T) {
	if _, err := assertion.Decode(fixture(t, "assertion-set-refused.json")); err == nil {
		t.Fatal("accepted an operator paired with a subject it does not read")
	}
}

// Each edit is one documented refusal. A set that decodes has already been
// bounded and type-checked, so nothing below can reach an evaluation.
func TestDecodeRefusesEveryContractViolation(t *testing.T) {
	for _, refusal := range []struct{ name, old, replacement string }{
		{"a contract version this release does not read", "readmit-assertion-set/v1", "readmit-assertion-set/v2"},
		{"an unknown top-level member", `"name":`, `"command": "rm -rf /", "name":`},
		{"an unknown assertion member", `"id": "ack-accepted"`, `"id": "ack-accepted", "script": "sh"`},
		{"an unknown subject member", `"selector": "MSA-1"}}`, `"selector": "MSA-1", "regex": "."}}`},
		{"an unknown expectation member", `{"field": {"state": "present", "text": "AA"}}`, `{"field": {"state": "present", "text": "AA"}, "count": 1}`},
		{"a duplicate member", `"name": "Synthetic`, `"name": "duplicate", "name": "Synthetic`},
		{"an operator this contract does not carry", `"operator": "field_equals"`, `"operator": "shell"`},
		{"an operator paired with the wrong subject", `"operator": "record_count"`, `"operator": "field_equals"`},
		{"an operator paired with the wrong expectation", `{"count": 2}`, `{"holds": true}`},
		{"a subject naming two members", `{"collection": {"scope": "after"}}`, `{"collection": {"scope": "after"}, "each": {"scope": "after", "quantifier": "any"}}`},
		{"an expectation naming two members", `{"count": 2}`, `{"count": 2, "holds": true}`},
		{"a null expectation member", `{"count": 2}`, `{"count": null}`},
		{"an omitted condition", `"when": null,`, ``},
		{"a null subject", `"subject": {"collection": {"scope": "after"}},`, `"subject": null,`},
		{"a duplicate assertion id", `"id": "ack-not-rejected"`, `"id": "ack-accepted"`},
		{"an assertion id outside the grammar", `"id": "ack-accepted"`, `"id": "ACK Accepted"`},
		{"a selector outside the shared grammar", `"selector": "MSA-1"`, `"selector": "MSA-0"`},
		{"a selector with a wildcard", `"selector": "MSA-1"`, `"selector": "MSA-*"`},
		{"a message key outside the occurrence grammar", `"message": "s0001-e000001", "selector": "MSA-1"`, `"message": "../../etc/passwd", "selector": "MSA-1"`},
		{"a scope this contract does not name", `"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"`, `"scope": "downstream", "message": "s0001-e000001", "selector": "MSA-1"`},
		{"a collection scope this contract does not name", `{"collection": {"scope": "after"}}`, `{"collection": {"scope": "during"}}`},
		{"a quantifier this contract does not name", `"quantifier": "every"`, `"quantifier": "most"`},
		{"a transition from an observation to itself", `{"transition": {"from": "before", "to": "after"}}`, `{"transition": {"from": "after", "to": "after"}}`},
		{"a state this contract does not name", `{"state": "null"}`, `{"state": "unknown"}`},
		{"text carried by a value that is not present", `{"state": "null"}`, `{"state": "null", "text": "\"\""}`},
		{"a present value carrying no text", `{"field": {"state": "present", "text": "AA"}}`, `{"field": {"state": "present"}}`},
		{"a negative count", `{"count": 2}`, `{"count": -1}`},
		{"a count past the record limit", `{"count": 2}`, `{"count": 1048577}`},
		{"a range whose minimum is above its maximum", `{"min": "50", "max": "150"}`, `{"min": "150", "max": "50"}`},
		{"a range bound that is not a decimal literal", `{"min": "50", "max": "150"}`, `{"min": "fifty", "max": "150"}`},
		{"a range bound in exponent notation", `{"min": "50", "max": "150"}`, `{"min": "5e1", "max": "150"}`},
		{"a decimal literal past its byte bound", `{"min": "50", "max": "150"}`, `{"min": "50", "max": "1234567890123456789012345678901234"}`},
		{"a range bound written as a JSON number", `{"min": "50", "max": "150"}`, `{"min": 50, "max": 150}`},
		{"a negative tolerance", `{"value": "72", "tolerance": "0.5"}`, `{"value": "72", "tolerance": "-0.5"}`},
		{"a window whose start is after its end", `{"from": "20260103100000+0000", "to": "20260103120000+0000"}`, `{"from": "20260103120000+0000", "to": "20260103100000+0000"}`},
		{"a window bound carrying no offset", `"from": "20260103100000+0000"`, `"from": "20260103100000"`},
		{"a window bound that is not a timestamp", `"from": "20260103100000+0000"`, `"from": "yesterday+0000"`},
		{"an impossible window bound", `"from": "20260103100000+0000"`, `"from": "20261303100000+0000"`},
		{"a pattern this engine does not compile", `"^MSG[0-9]{5}$"`, `"^MSG([0-9]{5}$"`},
		{"an empty pattern", `"^MSG[0-9]{5}$"`, `""`},
		{"a pattern carrying a control character", `"^MSG[0-9]{5}$"`, "\"^MSG\\u0007\""},
		{"an empty key list", `["appt-9001", "appt-9002"]`, `[]`},
		{"a duplicate declared key", `["appt-9001", "appt-9002"]`, `["appt-9001", "appt-9001"]`},
		{"a key carrying a control character", `["appt-9001", "appt-9002"]`, "[\"appt-9001\", \"appt\\u00099002\"]"},
		{"an empty set name", `"Synthetic reschedule expectations"`, `""`},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			if _, err := assertion.Decode(mutate(t, refusal.old, refusal.replacement)); err == nil {
				t.Fatalf("accepted %s", refusal.name)
			}
		})
	}
}

func TestDecodeRefusesEveryStructuralViolation(t *testing.T) {
	for _, refusal := range []struct {
		name    string
		reshape func(document map[string]any)
	}{
		{"an empty assertion list", func(document map[string]any) { document["assertions"] = []any{} }},
		{"more assertions than the contract bounds", func(document map[string]any) {
			declared := first(t, document)
			list := make([]any, 0, assertion.MaxAssertions+1)
			for i := 0; i <= assertion.MaxAssertions; i++ {
				copied := make(map[string]any, len(declared))
				for member, value := range declared {
					copied[member] = value
				}
				copied["id"] = "a" + strconv.Itoa(i)
				list = append(list, copied)
			}
			document["assertions"] = list
		}},
		{"an assertions member that is not a list", func(document map[string]any) { document["assertions"] = "none" }},
		{"an omitted name", func(document map[string]any) { delete(document, "name") }},
		{"an omitted schema", func(document map[string]any) { delete(document, "schema") }},
		{"an assertion that is not an object", func(document map[string]any) { document["assertions"] = []any{"field_equals"} }},
		{"an omitted expectation", func(document map[string]any) { delete(first(t, document), "expected") }},
		{"an expectation naming no member", func(document map[string]any) { first(t, document)["expected"] = map[string]any{} }},
		{"a condition naming no field", func(document map[string]any) {
			first(t, document)["when"] = map[string]any{"equals": map[string]any{"state": "present", "text": "AA"}}
		}},
		{"a set name that is only whitespace", func(document map[string]any) { document["name"] = "   " }},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			if _, err := assertion.Decode(edit(t, refusal.reshape)); err == nil {
				t.Fatalf("accepted %s", refusal.name)
			}
		})
	}
}

func TestDecodeRefusesAnOversizedDocument(t *testing.T) {
	oversized := append(fixture(t, "assertion-set.json"), make([]byte, assertion.MaxSetBytes)...)
	if _, err := assertion.Decode(oversized); err == nil {
		t.Fatal("accepted a document past the size limit")
	}
}

// The two contracts stay apart: neither reader accepts the other's document,
// so readmit-test/v1 gains no member and no new meaning from this delivery.
func TestTheTestSpecContractIsUnchangedByThisOne(t *testing.T) {
	if _, err := assertion.Decode(fixture(t, "test-reschedule.json")); err == nil {
		t.Fatal("the assertion reader accepted a readmit-test/v1 spec")
	}
	if _, err := testrunner.DecodeSpec(fixture(t, "assertion-set.json")); err == nil {
		t.Fatal("the readmit-test/v1 reader accepted an assertion set")
	}
}
