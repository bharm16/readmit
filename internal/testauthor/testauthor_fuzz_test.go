package testauthor_test

import (
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// FuzzTestDraftDocument exercises the draft reader and the generator together.
//
// No input may panic, and no bytes at all may produce a draft that generates a
// document `readmit test` refuses, that pairs an outcome boundary with an
// initial state the contract does not pair it with, that carries an
// observation source the ACK boundary does not read, or that reads back as a
// different draft than the one that was accepted. That is the whole claim of
// this delivery — a saved test is a valid spec — stated as a property, so it
// cannot be lost by widening the reader.
func FuzzTestDraftDocument(f *testing.F) {
	identity := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	authored, err := testauthor.NewDraft("incident", identity)
	if err != nil {
		f.Fatal(err)
	}
	count := 1
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001"}},
		{Stage: testauthor.StageTarget, Target: "test-target.json"},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "test-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Restart the listener from an empty ledger."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &count},
			{ID: "booking-ack", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: present("AA")},
		}},
	} {
		if authored, err = authored.Answer(answer); err != nil {
			f.Fatal(err)
		}
	}
	seed, err := json.Marshal(authored)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(seed))
	f.Add(`{"schema":"readmit-test-draft/v1","case":{"entry":"incident","identity":"` + identity + `"},"name":"","messages":[],"target":"","boundary":"","observation":"","reset":"","expectations":[]}`)
	f.Add(`{"schema":"readmit-test-draft/v2"}`)
	f.Add(`{`)

	f.Fuzz(func(t *testing.T, document string) {
		draft, err := testauthor.DecodeDraft([]byte(document))
		if err != nil {
			return
		}
		encoded, err := json.Marshal(draft)
		if err != nil {
			t.Fatalf("an accepted draft could not be written back: %v", err)
		}
		again, err := testauthor.DecodeDraft(encoded)
		if err != nil || !reflect.DeepEqual(draft, again) {
			t.Fatalf("a draft did not read back as itself: %v", err)
		}
		if draft.Boundary == testrunner.ACKBoundary && draft.Observation != "" {
			t.Fatal("the ACK boundary accepted an observation source")
		}
		data, err := testauthor.Generate(draft)
		if err != nil {
			return
		}
		spec, err := testrunner.DecodeSpec(data)
		if err != nil {
			t.Fatalf("a generated document is not a spec this release reads: %v", err)
		}
		if spec.Setup.InitialState != testauthor.Setup(spec.Observation.Boundary) {
			t.Fatalf("boundary %q was paired with the initial state %q", spec.Observation.Boundary, spec.Setup.InitialState)
		}
	})
}
