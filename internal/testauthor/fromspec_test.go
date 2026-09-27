package testauthor_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A saved spec reopens as exactly the draft it was generated from: every
// stage, every expectation in order with every member it declares — each of
// the four field states, a count of zero, an empty ledger and records with
// every identifier authority. A member the draft cannot hold is kept out of
// the draft and named, never shortened.
func TestFromSpecIsTheInverseOfGenerate(t *testing.T) {
	_, source := workspace(t)
	zero := 0
	records := []observation.Record{
		{RecordID: "r000001", PatientID: observation.Identifier{Value: "P2", Namespace: "HOSP", UniversalID: "1.2.3", UniversalIDType: "ISO"},
			PlacerID: observation.Identifier{Value: "PL2", Namespace: "", UniversalID: "", UniversalIDType: ""},
			FillerID: observation.Identifier{Value: "F2", Namespace: "SCHED", UniversalID: "2.16.840", UniversalIDType: "ISO"}, AppointmentStart: "202601020900"},
		{RecordID: "r000002", PatientID: observation.Identifier{Value: "P1", Namespace: "HOSP", UniversalID: "", UniversalIDType: ""},
			PlacerID: observation.Identifier{Value: "PL1", Namespace: "ORD", UniversalID: "", UniversalIDType: ""},
			FillerID: observation.Identifier{Value: "F1", Namespace: "SCHED", UniversalID: "", UniversalIDType: ""}, AppointmentStart: "202601020800"},
	}
	empty := []observation.Record{}
	ledger := draft(t, source,
		testauthor.Answer{Stage: testauthor.StageName, Name: "Reschedule keeps one appointment"},
		testauthor.Answer{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		testauthor.Answer{Stage: testauthor.StageTarget, Target: "test-target.json"},
		testauthor.Answer{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		testauthor.Answer{Stage: testauthor.StageObservation, Observation: "test-observation.json"},
		testauthor.Answer{Stage: testauthor.StageReset, Reset: "Start from an empty ledger."},
		testauthor.Answer{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "none-yet", Operator: testauthor.LedgerCount, Count: &zero},
			{ID: "exact", Operator: testauthor.LedgerEquals, Records: &records},
			{ID: "nothing", Operator: testauthor.LedgerEquals, Records: &empty},
			{ID: "code", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: present("AA")},
			{ID: "empty", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-3", Field: &testrunner.FieldValue{State: hl7.Empty}},
			{ID: "null", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000002", Selector: "ERR-1", Field: &testrunner.FieldValue{State: hl7.Null}},
			{ID: "absent", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000002", Selector: "ERR-3", Field: &testrunner.FieldValue{State: hl7.Omitted}},
		}},
	)
	ack := draft(t, source,
		testauthor.Answer{Stage: testauthor.StageName, Name: "ACK only"},
		testauthor.Answer{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001"}},
		testauthor.Answer{Stage: testauthor.StageTarget, Target: "test-target.json"},
		testauthor.Answer{Stage: testauthor.StageBoundary, Boundary: testrunner.ACKBoundary},
		testauthor.Answer{Stage: testauthor.StageReset, Reset: "Declare the starting state."},
		testauthor.Answer{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "code", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: present("AA")},
		}},
	)
	for _, authored := range []testauthor.Draft{ledger, ack} {
		data, err := testauthor.Generate(authored)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := testrunner.DecodeSpec(data)
		if err != nil {
			t.Fatal(err)
		}
		reopened, clauses, err := testauthor.FromSpec(spec, source.Identity)
		if err != nil || len(clauses) != 0 {
			t.Fatalf("%s: %v %v", authored.Name, clauses, err)
		}
		if !reflect.DeepEqual(reopened, authored) {
			t.Fatalf("%s reopened differently:\n%+v\nwant\n%+v", authored.Name, reopened, authored)
		}
		again, err := testauthor.Generate(reopened)
		if err != nil || string(again) != string(data) {
			t.Fatalf("%s did not generate its own bytes again: %v", authored.Name, err)
		}
		if problems := testauthor.Problems("", source, reopened); len(problems) != 0 {
			t.Fatalf("%s: a generated draft has problems: %+v", authored.Name, problems)
		}
	}

	// Members a draft cannot hold are named and left out, and the rest of
	// the draft is kept.
	data, err := testauthor.Generate(ack)
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := testrunner.DecodeSpec(data)
	spec.Input.Case, spec.Target, spec.Setup.ResetInstructions = "../elsewhere/incident", "targets/lab.json", "Stop the listener.\nStart it again."
	reopened, clauses, err := testauthor.FromSpec(spec, source.Identity)
	if err != nil {
		t.Fatal(err)
	}
	stages := []string{}
	for _, clause := range clauses {
		stages = append(stages, clause.Stage)
	}
	if !slices.Equal(stages, []string{testauthor.StageCase, testauthor.StageTarget, testauthor.StageReset}) ||
		reopened.Case.Entry != "" || reopened.Target != "" || reopened.Reset != "" || len(reopened.Expectations) != 1 || reopened.Name != "ACK only" {
		t.Fatalf("unrepresentable members: %+v %+v", clauses, reopened)
	}

	// ExportTo writes exactly the bytes and refuses to replace a file.
	destination := filepath.Join(t.TempDir(), "exported.json")
	if _, err := testauthor.ExportTo(data, destination); err != nil {
		t.Fatal(err)
	}
	if written, _ := os.ReadFile(destination); string(written) != string(data) {
		t.Fatal("the exported bytes differ")
	}
	if _, err := testauthor.ExportTo(data, destination); err == nil {
		t.Fatal("an existing file was replaced")
	}
}

// Every problem of a whole draft is named at once, each at its stage, and an
// expectation's at its position.
func TestProblemsNamesEveryStageOfAWholeDraft(t *testing.T) {
	root, source := workspace(t)
	blank, err := testauthor.NewDraft("incident", source.Identity)
	if err != nil {
		t.Fatal(err)
	}
	stages := func(problems []testauthor.Problem) []string {
		out := []string{}
		for _, problem := range problems {
			out = append(out, problem.Stage)
		}
		return out
	}
	if got := stages(testauthor.Problems(root, source, blank)); !slices.Equal(got, []string{"name", "messages", "target", "boundary", "reset", "expectations"}) {
		t.Fatalf("a blank draft: %v", got)
	}
	bad := blank
	bad.Name, bad.Messages, bad.Target, bad.Boundary, bad.Reset = "ok", []string{"s0001-e000003"}, "missing.json", testrunner.LedgerBoundary, "r"
	bad.Expectations = []testauthor.Expectation{
		{ID: "fine", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000003", Selector: "MSA-1", Field: present("AA")},
		{ID: "wrong", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000009", Selector: "MSA-1", Field: present("AA")},
	}
	problems := testauthor.Problems(root, source, bad)
	if got := stages(problems); !slices.Equal(got, []string{"messages", "target", "observation", "expectations"}) || problems[3].Index != 1 {
		t.Fatalf("a wrong draft: %+v", problems)
	}
}
