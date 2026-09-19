package assertion_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

// Independently authored synthetic messages. The acknowledgement and the
// update are what the interface produced; the request is what the run sent.
// PID-6 is an explicit HL7 null, PID-7 is empty and PID-8 is omitted, which is
// the distinction every field operator here is held to.
const (
	observedACK = "MSH|^~\\&|READMIT|FIXTURE|SCHED|TEST|20260103110001+0000||ACK^S12|READMITACK000001|P|2.5.1\r" +
		"MSA|AA|MSG00001\r"
	observedUpdate = "MSH|^~\\&|SCHED|FIXTURE|READMIT|TEST|20260103110500+0000||SIU^S12|MSG00002|P|2.5.1\r" +
		"EVN|S12|20260103110000+0000\r" +
		"PID|1||MRN-0137^^^READMIT^MR||DOE^JANE|\"\"|\r" +
		"OBX|1|NM|WEIGHT^Weight||72.5|kg\r"
	sentRequest = "MSH|^~\\&|SCHED|FIXTURE|READMIT|TEST|20260103110000+0000||SIU^S12|MSG00001|P|2.5.1\r" +
		"SCH|PLACER-9001|FILLER-9001\r"
)

func message(t *testing.T, raw string) assertion.Message {
	t.Helper()
	document, err := hl7.Parse([]byte(raw), hl7.Options{Format: hl7.Raw, Terminator: hl7.CR})
	if err != nil {
		t.Fatalf("parse synthetic message: %v", err)
	}
	return assertion.Message{Document: document}
}

func evidence(t *testing.T) assertion.Evidence {
	t.Helper()
	return assertion.Evidence{
		Observed: map[string]assertion.Message{
			"s0001-e000001": message(t, observedACK),
			"s0001-e000002": message(t, observedUpdate),
		},
		Input:  map[string]assertion.Message{"s0001-e000001": message(t, sentRequest)},
		Before: assertion.Collection{Complete: true},
		After:  assertion.Collection{Complete: true, Keys: []string{"appt-9001", "appt-9002"}},
	}
}

func decode(t *testing.T, document string) assertion.Set {
	t.Helper()
	set, err := assertion.Decode([]byte(document))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return set
}

// single builds a one-assertion set, so a test states one operator, one
// subject and one expectation and nothing else.
func single(t *testing.T, operator, subject, expected string) assertion.Set {
	t.Helper()
	return decode(t, `{"schema":"readmit-assertion-set/v1","name":"one assertion","assertions":[`+
		`{"id":"only","operator":"`+operator+`","subject":`+subject+`,"when":null,"expected":`+expected+`}]}`)
}

func observedField(message, selector string) string {
	return `{"field":{"scope":"observed","message":"` + message + `","selector":"` + selector + `"}}`
}

func report(t *testing.T, set assertion.Set, evidence assertion.Evidence) assertion.Report {
	t.Helper()
	produced, err := set.Evaluate(t.Context(), evidence)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return produced
}

func only(t *testing.T, set assertion.Set, evidence assertion.Evidence) assertion.Result {
	t.Helper()
	produced := report(t, set, evidence)
	if len(produced.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(produced.Results))
	}
	return produced.Results[0]
}

func TestTheCommittedSetPassesAgainstTheEvidenceItDescribes(t *testing.T) {
	set := decode(t, string(fixture(t, "assertion-set.json")))
	produced := report(t, set, evidence(t))
	if produced.Verdict != assertion.VerdictPass {
		t.Fatalf("verdict %q with %d failed and %d undecided", produced.Verdict, produced.Failed, produced.Undecided)
	}
	// The one assertion whose condition does not hold is skipped, and a
	// skipped assertion is reported as one rather than counted as a pass.
	if produced.Skipped != 1 || produced.Passed != len(set.Assertions)-1 {
		t.Fatalf("%d passed and %d skipped of %d assertions", produced.Passed, produced.Skipped, len(set.Assertions))
	}
	for _, result := range produced.Results {
		if result.ID == "rejected-run-names-its-error" && result.Outcome != assertion.OutcomeSkipped {
			t.Fatalf("the conditional assertion is %q", result.Outcome)
		}
	}
}

// A regression in the interface changes the verdict, and the report names
// which assertions disagreed rather than only that something did.
func TestARegressionFailsExactlyTheAssertionsItBreaks(t *testing.T) {
	set := decode(t, string(fixture(t, "assertion-set.json")))
	broken := evidence(t)
	broken.Observed["s0001-e000001"] = message(t, strings.Replace(observedACK, "MSA|AA|", "MSA|AR|", 1))
	produced := report(t, set, broken)
	if produced.Verdict != assertion.VerdictFail {
		t.Fatalf("verdict %q", produced.Verdict)
	}
	outcomes := make(map[string]assertion.Outcome, len(produced.Results))
	for _, result := range produced.Results {
		outcomes[result.ID] = result.Outcome
	}
	for id, want := range map[string]assertion.Outcome{
		"ack-accepted":                 assertion.OutcomeFailed,
		"ack-not-rejected":             assertion.OutcomeFailed,
		"accepted-run-is-a-test-run":   assertion.OutcomeSkipped,
		"rejected-run-names-its-error": assertion.OutcomeFailed,
		"two-appointments":             assertion.OutcomePassed,
	} {
		if outcomes[id] != want {
			t.Errorf("%s is %q, expected %q", id, outcomes[id], want)
		}
	}
}

// Four states are four answers, and the shape operators decide on all four.
func TestTheShapeOperatorsSeparatePresentEmptyNullAndOmitted(t *testing.T) {
	for selector, state := range map[string]string{
		"PID-5.1": "present",
		"PID-6":   "null",
		"PID-7":   "empty",
		"PID-8":   "omitted",
	} {
		for _, declared := range []string{"present", "empty", "null", "omitted"} {
			set := single(t, "field_state", observedField("s0001-e000002", selector), `{"state":"`+declared+`"}`)
			result := only(t, set, evidence(t))
			want := assertion.OutcomeFailed
			if declared == state {
				want = assertion.OutcomePassed
			}
			if result.Outcome != want {
				t.Errorf("%s declared %s is %q, expected %q", selector, declared, result.Outcome, want)
			}
			if result.Observed.Field == nil || string(result.Observed.Field.State) != state {
				t.Errorf("%s reported %+v", selector, result.Observed.Field)
			}
		}
	}
}

// A value operator decides on a present value it can read and on nothing
// else. Undecided is the third answer: it is not a pass, and it is not the
// claim that the value is outside the range, the window or the pattern.
func TestAValueOperatorIsUndecidedOnEverythingItCannotRead(t *testing.T) {
	long := "MSH|^~\\&|SCHED|FIXTURE|READMIT|TEST|20260103110500+0000||SIU^S12|MSG00003|P|2.5.1\r" +
		"OBX|1|ST|NOTE||" + strings.Repeat("a", assertion.MaxMatchBytes+1) + "|\r"
	undated := "MSH|^~\\&|SCHED|FIXTURE|READMIT|TEST|20260103110500+0000||SIU^S12|MSG00004|P|2.5.1\r" +
		"EVN|S12|20260103110000\r"
	material := evidence(t)
	material.Observed["s0001-e000003"] = message(t, long)
	material.Observed["s0001-e000004"] = message(t, undated)
	for _, undecidable := range []struct{ name, operator, subject, expected string }{
		{"an omitted field has no number to place", "numeric_range", observedField("s0001-e000002", "PID-8"), `{"range":{"min":"0","max":"1"}}`},
		{"an explicit null has no number to place", "numeric_range", observedField("s0001-e000002", "PID-6"), `{"range":{"min":"0","max":"1"}}`},
		{"an empty field has no number to place", "numeric_range", observedField("s0001-e000002", "PID-7"), `{"range":{"min":"0","max":"1"}}`},
		{"text that is not a number", "numeric_range", observedField("s0001-e000002", "PID-5.1"), `{"range":{"min":"0","max":"1"}}`},
		{"text that is not a number, within a tolerance", "numeric_tolerance", observedField("s0001-e000002", "PID-5.1"), `{"tolerance":{"value":"1","tolerance":"1"}}`},
		{"an omitted field has no text to match", "text_matches", observedField("s0001-e000002", "PID-8"), `{"pattern":"^.*$"}`},
		{"text past the match bound cannot be decided on a prefix", "text_matches", observedField("s0001-e000003", "OBX-5"), `{"pattern":"^a+$"}`},
		{"a timestamp carrying no offset names no instant", "date_window", observedField("s0001-e000004", "EVN-2"), `{"window":{"from":"20260103100000+0000","to":"20260103120000+0000"}}`},
		{"text that is not a timestamp", "date_window", observedField("s0001-e000002", "PID-5.1"), `{"window":{"from":"20260103100000+0000","to":"20260103120000+0000"}}`},
	} {
		t.Run(undecidable.name, func(t *testing.T) {
			set := single(t, undecidable.operator, undecidable.subject, undecidable.expected)
			produced := report(t, set, material)
			if produced.Results[0].Outcome != assertion.OutcomeUndecided {
				t.Fatalf("outcome %q", produced.Results[0].Outcome)
			}
			if produced.Verdict != assertion.VerdictUndecided {
				t.Fatalf("an undecided assertion produced the verdict %q", produced.Verdict)
			}
		})
	}
}

func TestTheValueOperatorsDecideOnAReadableValue(t *testing.T) {
	for _, decided := range []struct {
		name, operator, subject, expected string
		want                              assertion.Outcome
	}{
		{"a weight inside the range", "numeric_range", observedField("s0001-e000002", "OBX-5"), `{"range":{"min":"50","max":"150"}}`, assertion.OutcomePassed},
		{"a weight outside the range", "numeric_range", observedField("s0001-e000002", "OBX-5"), `{"range":{"min":"80","max":"150"}}`, assertion.OutcomeFailed},
		{"a bound is inclusive", "numeric_range", observedField("s0001-e000002", "OBX-5"), `{"range":{"min":"72.5","max":"72.5"}}`, assertion.OutcomePassed},
		{"a decimal compared exactly rather than as a float", "numeric_tolerance", observedField("s0001-e000002", "OBX-5"), `{"tolerance":{"value":"72.4","tolerance":"0.1"}}`, assertion.OutcomePassed},
		{"a decimal one step outside its tolerance", "numeric_tolerance", observedField("s0001-e000002", "OBX-5"), `{"tolerance":{"value":"72.4","tolerance":"0.09"}}`, assertion.OutcomeFailed},
		{"an instant inside the window", "date_window", observedField("s0001-e000002", "EVN-2"), `{"window":{"from":"20260103100000+0000","to":"20260103120000+0000"}}`, assertion.OutcomePassed},
		{"an instant on the window's edge", "date_window", observedField("s0001-e000002", "EVN-2"), `{"window":{"from":"20260103110000+0000","to":"20260103110000+0000"}}`, assertion.OutcomePassed},
		{"an instant outside the window", "date_window", observedField("s0001-e000002", "EVN-2"), `{"window":{"from":"20260103120000+0000","to":"20260103130000+0000"}}`, assertion.OutcomeFailed},
		{"the same instant written in another offset", "date_window", observedField("s0001-e000002", "EVN-2"), `{"window":{"from":"20260103060000-0500","to":"20260103060000-0500"}}`, assertion.OutcomePassed},
		{"a zero offset written with a minus sign is the same instant", "date_window", observedField("s0001-e000002", "EVN-2"), `{"window":{"from":"20260103110000-0000","to":"20260103110000-0000"}}`, assertion.OutcomePassed},
		{"a decimal compared past the sixth place", "numeric_tolerance", observedField("s0001-e000002", "OBX-5"), `{"tolerance":{"value":"72.4999999","tolerance":"0.0000001"}}`, assertion.OutcomePassed},
		{"a decimal one step past the sixth place", "numeric_tolerance", observedField("s0001-e000002", "OBX-5"), `{"tolerance":{"value":"72.4999998","tolerance":"0.0000001"}}`, assertion.OutcomeFailed},
		{"text the pattern matches", "text_matches", observedField("s0001-e000001", "MSA-2"), `{"pattern":"^MSG[0-9]{5}$"}`, assertion.OutcomePassed},
		{"text the pattern does not match", "text_matches", observedField("s0001-e000001", "MSA-2"), `{"pattern":"^ACK[0-9]{5}$"}`, assertion.OutcomeFailed},
	} {
		t.Run(decided.name, func(t *testing.T) {
			result := only(t, single(t, decided.operator, decided.subject, decided.expected), evidence(t))
			if result.Outcome != decided.want {
				t.Fatalf("outcome %q, expected %q", result.Outcome, decided.want)
			}
		})
	}
}

// One relationship operator covers a cross-message relationship and a mapping
// expectation, because both are one field reference related to another.
func TestARelationshipRelatesTwoScopesOrTwoMessages(t *testing.T) {
	mapped := `{"pair":{"left":{"scope":"input","message":"s0001-e000001","selector":"MSH-10"},` +
		`"right":{"scope":"observed","message":"s0001-e000001","selector":"MSA-2"}}}`
	if result := only(t, single(t, "values_equal", mapped, `{"holds":true}`), evidence(t)); result.Outcome != assertion.OutcomePassed {
		t.Fatalf("a control id carried through is %q", result.Outcome)
	}
	broken := evidence(t)
	broken.Observed["s0001-e000001"] = message(t, strings.Replace(observedACK, "MSA|AA|MSG00001", "MSA|AA|MSG09999", 1))
	result := only(t, single(t, "values_equal", mapped, `{"holds":true}`), broken)
	if result.Outcome != assertion.OutcomeFailed {
		t.Fatalf("a control id the interface replaced is %q", result.Outcome)
	}
	if result.Observed.Field == nil || result.Observed.Compared == nil {
		t.Fatal("a relationship reports both sides it read")
	}
	// Two omitted fields are equally omitted: the relationship reads the
	// four-state value, not only present text.
	absent := `{"pair":{"left":{"scope":"observed","message":"s0001-e000002","selector":"PID-8"},` +
		`"right":{"scope":"observed","message":"s0001-e000002","selector":"PID-9"}}}`
	if result := only(t, single(t, "values_equal", absent, `{"holds":true}`), evidence(t)); result.Outcome != assertion.OutcomePassed {
		t.Fatalf("two omitted fields are %q", result.Outcome)
	}
}

func TestTheCollectionOperatorsReadOneCompletedObservation(t *testing.T) {
	material := evidence(t)
	material.After = assertion.Collection{Complete: true, Keys: []string{"appt-9001", "appt-9002", "appt-9001"}}
	for _, decided := range []struct {
		name, operator, expected string
		want                     assertion.Outcome
	}{
		{"three records were observed", "record_count", `{"count":3}`, assertion.OutcomePassed},
		{"two were not", "record_count", `{"count":2}`, assertion.OutcomeFailed},
		{"a repeated key is not unique", "records_unique", `{"holds":true}`, assertion.OutcomeFailed},
		{"and that is stated as such", "records_unique", `{"holds":false}`, assertion.OutcomePassed},
		{"both declared keys were observed", "records_contain", `{"keys":["appt-9001","appt-9002"]}`, assertion.OutcomePassed},
		{"a key that was never observed", "records_contain", `{"keys":["appt-9003"]}`, assertion.OutcomeFailed},
		{"the declared order holds", "records_ordered", `{"keys":["appt-9001","appt-9002"]}`, assertion.OutcomePassed},
		{"the reverse order holds too, because a key repeats", "records_ordered", `{"keys":["appt-9002","appt-9001"]}`, assertion.OutcomePassed},
		{"an order ending in a key never observed", "records_ordered", `{"keys":["appt-9002","appt-9003"]}`, assertion.OutcomeFailed},
		{"one key produced two records", "record_multiplicity", `{"multiplicity":{"key":"appt-9001","count":2}}`, assertion.OutcomePassed},
		{"a key that produced none", "record_multiplicity", `{"multiplicity":{"key":"appt-9003","count":0}}`, assertion.OutcomePassed},
		{"records were observed, so none is not absent", "records_absent", `{"holds":true}`, assertion.OutcomeFailed},
	} {
		t.Run(decided.name, func(t *testing.T) {
			result := only(t, single(t, decided.operator, `{"collection":{"scope":"after"}}`, decided.expected), material)
			if result.Outcome != decided.want {
				t.Fatalf("outcome %q, expected %q", result.Outcome, decided.want)
			}
			if result.Observed.Records == nil || *result.Observed.Records != 3 {
				t.Fatalf("reported records %v", result.Observed.Records)
			}
		})
	}
}

// An observed empty state is evidence, and it is the only kind of emptiness
// that is.
func TestAnObservedEmptyStateSupportsAnAbsenceAssertion(t *testing.T) {
	material := evidence(t)
	material.After = assertion.Collection{Complete: true}
	result := only(t, single(t, "records_absent", `{"collection":{"scope":"after"}}`, `{"holds":true}`), material)
	if result.Outcome != assertion.OutcomePassed {
		t.Fatalf("an observed empty source is %q", result.Outcome)
	}
}

// The one rule: failed collection never becomes a passing absence assertion.
func TestAnIncompleteObservationIsAnExecutionErrorAndNeverACountOfZero(t *testing.T) {
	material := evidence(t)
	material.After = assertion.Collection{Complete: false}
	for _, operator := range []struct{ name, expected string }{
		{"records_absent", `{"holds":true}`},
		{"record_count", `{"count":0}`},
		{"records_unique", `{"holds":true}`},
	} {
		set := single(t, operator.name, `{"collection":{"scope":"after"}}`, operator.expected)
		produced, err := set.Evaluate(t.Context(), material)
		if class(err) != assertion.ErrorIncompleteObservation {
			t.Fatalf("%s produced %v", operator.name, err)
		}
		if !reflect.DeepEqual(produced, assertion.Report{}) {
			t.Fatalf("%s produced a report against evidence it could not stand behind", operator.name)
		}
	}
}

func TestAnObservationPastTheRecordLimitIsAPrefixRatherThanTheSource(t *testing.T) {
	material := evidence(t)
	// One past what readmit-observation-window/v1 lets a window hold in scope.
	material.After = assertion.Collection{Complete: true, Keys: make([]string, 1<<20+1)}
	set := single(t, "record_count", `{"collection":{"scope":"after"}}`, `{"count":1048576}`)
	if _, err := set.Evaluate(t.Context(), material); class(err) != assertion.ErrorRecordLimit {
		t.Fatalf("produced %v", err)
	}
}

func TestAQuantifierOverNoRecordQuantifiesOverNothing(t *testing.T) {
	material := evidence(t)
	for _, quantifier := range []string{"every", "any", "none"} {
		set := single(t, "record_key_matches", `{"each":{"scope":"before","quantifier":"`+quantifier+`"}}`, `{"pattern":"^appt-"}`)
		result := only(t, set, material)
		if result.Outcome != assertion.OutcomeUndecided {
			t.Fatalf("%s over an empty observation is %q", quantifier, result.Outcome)
		}
		if result.Observed.Matched != nil {
			t.Fatalf("%s reported a match count over no record", quantifier)
		}
	}
}

func TestAQuantifierDecidesOverTheRecordsItWasGiven(t *testing.T) {
	material := evidence(t)
	material.After = assertion.Collection{Complete: true, Keys: []string{"appt-9001", "order-4"}}
	for _, decided := range []struct {
		quantifier string
		want       assertion.Outcome
	}{
		{"every", assertion.OutcomeFailed},
		{"any", assertion.OutcomePassed},
		{"none", assertion.OutcomeFailed},
	} {
		set := single(t, "record_key_matches", `{"each":{"scope":"after","quantifier":"`+decided.quantifier+`"}}`, `{"pattern":"^appt-[0-9]{4}$"}`)
		result := only(t, set, material)
		if result.Outcome != decided.want {
			t.Fatalf("%s is %q, expected %q", decided.quantifier, result.Outcome, decided.want)
		}
		if result.Observed.Matched == nil || *result.Observed.Matched != 1 {
			t.Fatalf("%s reported matches %v", decided.quantifier, result.Observed.Matched)
		}
	}
}

func TestATransitionComparesTwoObservationsAsSetsOfKeys(t *testing.T) {
	material := evidence(t)
	material.Before = assertion.Collection{Complete: true, Keys: []string{"appt-9001", "appt-9000"}}
	material.After = assertion.Collection{Complete: true, Keys: []string{"appt-9001", "appt-9002", "appt-9002"}}
	set := single(t, "records_changed", `{"transition":{"from":"before","to":"after"}}`, `{"change":{"added":1,"removed":1}}`)
	result := only(t, set, material)
	if result.Outcome != assertion.OutcomePassed {
		t.Fatalf("outcome %q", result.Outcome)
	}
	if result.Observed.Added == nil || *result.Observed.Added != 1 || result.Observed.Removed == nil || *result.Observed.Removed != 1 {
		t.Fatalf("reported added %v removed %v", result.Observed.Added, result.Observed.Removed)
	}
	// A transition cannot be decided from an observation that did not
	// complete, on either side.
	material.Before.Complete = false
	if _, err := set.Evaluate(t.Context(), material); class(err) != assertion.ErrorIncompleteObservation {
		t.Fatalf("produced %v", err)
	}
}

// A document may hold several messages. A field reference reads the one its
// message's index names, and an index the document does not hold is absent
// evidence rather than an omitted value.
func TestAMessageIndexAddressesOneFrameOfADocument(t *testing.T) {
	rejected := strings.Replace(observedACK, "MSA|AA|MSG00001", "MSA|AE|MSG00002", 1)
	framed := append(mllp.Frame([]byte(observedACK)), mllp.Frame([]byte(rejected))...)
	document, err := hl7.Parse(framed, hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR})
	if err != nil || len(document.Messages) != 2 {
		t.Fatalf("parse two frames: %v", err)
	}
	material := evidence(t)
	for index, code := range []string{"AA", "AE"} {
		material.Observed["s0001-e000001"] = assertion.Message{Document: document, Index: index}
		set := single(t, "field_equals", observedField("s0001-e000001", "MSA-1"), `{"field":{"state":"present","text":"`+code+`"}}`)
		if result := only(t, set, material); result.Outcome != assertion.OutcomePassed {
			t.Fatalf("message %d reading %q is %q", index, code, result.Outcome)
		}
	}
	material.Observed["s0001-e000001"] = assertion.Message{Document: document, Index: len(document.Messages)}
	set := single(t, "field_state", observedField("s0001-e000001", "MSA-1"), `{"state":"omitted"}`)
	if _, err := set.Evaluate(t.Context(), material); class(err) != assertion.ErrorUnknownMessage {
		t.Fatalf("produced %v", err)
	}
}

// Absent evidence is not an absent value: a set naming a message the caller
// did not supply is an execution error rather than an omitted field.
func TestEvidenceTheSetNamesAndTheCallerDidNotSupplyIsAnExecutionError(t *testing.T) {
	material := evidence(t)
	delete(material.Observed, "s0001-e000002")
	set := single(t, "field_state", observedField("s0001-e000002", "PID-8"), `{"state":"omitted"}`)
	if _, err := set.Evaluate(t.Context(), material); class(err) != assertion.ErrorUnknownMessage {
		t.Fatalf("produced %v", err)
	}
	// The same selector against the same message that was supplied does
	// decide, so the error is about the evidence and not the selector.
	if result := only(t, set, evidence(t)); result.Outcome != assertion.OutcomePassed {
		t.Fatalf("outcome %q", result.Outcome)
	}
}

func TestAValueTheParserCannotDecodeIsAnExecutionError(t *testing.T) {
	material := evidence(t)
	material.Observed["s0001-e000001"] = message(t, strings.Replace(observedACK, "MSG00001", "MSG\\H\\00001", 1))
	set := single(t, "field_equals", observedField("s0001-e000001", "MSA-2"), `{"field":{"state":"present","text":"MSG00001"}}`)
	if _, err := set.Evaluate(t.Context(), material); class(err) != assertion.ErrorUnreadableValue {
		t.Fatalf("produced %v", err)
	}
}

// Cancelling produces no verdict, and evaluating the same set against the
// same evidence afterwards produces exactly the report the cancelled run
// would have. Recovery is re-evaluation; nothing is retained to resume.
func TestACancelledEvaluationProducesNoVerdictAndRecoversByRunningAgain(t *testing.T) {
	set := decode(t, string(fixture(t, "assertion-set.json")))
	material := evidence(t)
	before := report(t, set, material)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	produced, err := set.Evaluate(cancelled, material)
	if class(err) != assertion.ErrorCancelled {
		t.Fatalf("produced %v", err)
	}
	if !reflect.DeepEqual(produced, assertion.Report{}) {
		t.Fatal("a cancelled evaluation produced a verdict")
	}
	after := report(t, set, material)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("re-evaluation after a cancellation produced a different report")
	}
}

// A set assembled in Go rather than read refuses to evaluate, so the reader's
// bounds and type checks cannot be bypassed by construction.
func TestASetThatDidNotComeThroughTheReaderRefusesToEvaluate(t *testing.T) {
	assembled := assertion.Set{Schema: assertion.Schema, Name: "assembled", Assertions: []assertion.Assertion{{
		ID: "only", Operator: assertion.RecordCount,
		Subject: assertion.Subject{Collection: &assertion.RecordRef{Scope: assertion.AfterRecords}},
	}}}
	if _, err := assembled.Evaluate(t.Context(), evidence(t)); class(err) != assertion.ErrorNotDecoded {
		t.Fatalf("produced %v", err)
	}
}

// A set every assertion of which was skipped asserted nothing, and nothing is
// not a pass.
func TestASetThatOnlySkippedAssertsNothing(t *testing.T) {
	set := decode(t, `{"schema":"readmit-assertion-set/v1","name":"conditional only","assertions":[{`+
		`"id":"only","operator":"field_state","subject":`+observedField("s0001-e000001", "ERR-3")+`,`+
		`"when":{"field":{"scope":"observed","message":"s0001-e000001","selector":"MSA-1"},`+
		`"equals":{"state":"present","text":"AR"}},"expected":{"state":"present"}}]}`)
	produced := report(t, set, evidence(t))
	if produced.Verdict != assertion.VerdictUndecided || produced.Skipped != 1 || produced.Passed != 0 {
		t.Fatalf("verdict %q with %d skipped and %d passed", produced.Verdict, produced.Skipped, produced.Passed)
	}
}

func class(err error) string {
	var failure *assertion.Error
	if errors.As(err, &failure) {
		return failure.Class
	}
	return ""
}
