package suite_test

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

func write(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T, address string) (string, suite.Document) {
	t.Helper()
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	raw, e := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if e != nil {
		t.Fatal(e)
	}
	_, e = bundle.Write(filepath.Join(dir, "case-one"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if e != nil {
		t.Fatal(e)
	}
	target := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096}
	write(t, filepath.Join(dir, "east.json"), target)
	value := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "booking", Input: testrunner.Input{Case: "unbound", Messages: []string{"s0001-e000001"}}, Target: "unbound", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture deliberately"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "ack", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}}}
	write(t, filepath.Join(dir, "booking.json"), spec)
	doc, e := suite.Decode([]byte(document))
	if e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(dir, "suite.json"), doc)
	return dir, doc
}
func TestPrepareBindsRowsAndEnvironmentWithoutChangingTemplates(t *testing.T) {
	dir, doc := fixture(t, "127.0.0.1:1")
	before, _ := os.ReadFile(filepath.Join(dir, "booking.json"))
	value := "AE"
	doc.Tables[0].Rows = append(doc.Tables[0].Rows, suite.Row{ID: "rejected", Case: "case-one", Expected: map[string]testrunner.Value{"ack": {Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}})
	setup := doc.Tests[0]
	setup.ID = "setup"
	doc.Tests = append([]suite.Test{setup}, doc.Tests...)
	doc.Tests[1].After = []string{"setup"}
	write(t, filepath.Join(dir, "suite.json"), doc)
	out := filepath.Join(dir, "compiled")
	p, e := suite.Prepare(filepath.Join(dir, "suite.json"), "east", out)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Queue.Jobs) != 4 || len(p.Queue.Jobs[2].After) != 2 || p.Queue.Jobs[2].After[1] != "setup-rejected" {
		t.Fatalf("%+v", p.Queue)
	}
	spec, e := testrunner.ReadSpec(filepath.Join(out, "booking-rejected.json"))
	if e != nil {
		t.Fatal(e)
	}
	if spec.Target != filepath.Join(dir, "east.json") || spec.Input.Case != filepath.Join(dir, "case-one") || *spec.Assertions[0].Expected.Field.Text != "AE" {
		t.Fatalf("%+v", spec)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "booking.json"))
	if string(before) != string(after) {
		t.Fatal("template mutated")
	}
	if _, e = suite.Prepare(filepath.Join(dir, "suite.json"), "east", out); e == nil {
		t.Fatal("overwrote compiled suite")
	}
}

func TestPreparationRefusalsWriteNothingAndDoNotDropAnExpectation(t *testing.T) {
	for _, kind := range []string{"unknown-environment", "unknown-assertion", "wrong-value", "observation", "sequence", "missing-case", "template-member", "duplicate-row", "production-version"} {
		t.Run(kind, func(t *testing.T) {
			dir, doc := fixture(t, "127.0.0.1:1")
			env := "east"
			switch kind {
			case "unknown-environment":
				env = "absent"
			case "unknown-assertion":
				n := 1
				doc.Tables[0].Rows[0].Expected = map[string]testrunner.Value{"absent": {Count: &n}}
			case "wrong-value":
				n := 1
				doc.Tables[0].Rows[0].Expected = map[string]testrunner.Value{"ack": {Count: &n}}
			case "observation":
				doc.Environments[0].Bindings[0].Observation = "ledger.json"
			case "sequence":
				doc.Tests[0].Sequence = []string{"s0001-e000002"}
			case "missing-case":
				doc.Tables[0].Rows[0].Case = "missing"
			case "template-member":
				raw, _ := os.ReadFile(filepath.Join(dir, "booking.json"))
				raw = append([]byte(`{"unknown":1,`), raw[1:]...)
				if e := os.WriteFile(filepath.Join(dir, "booking.json"), raw, 0600); e != nil {
					t.Fatal(e)
				}
			case "duplicate-row":
				doc.Tables[0].Rows = append(doc.Tables[0].Rows, doc.Tables[0].Rows[0])
			case "production-version":
				doc.Schema = "readmit-suite/v2"
			}
			write(t, filepath.Join(dir, "suite.json"), doc)
			out := filepath.Join(dir, "refused")
			if _, e := suite.Prepare(filepath.Join(dir, "suite.json"), env, out); e == nil {
				t.Fatal("accepted invalid suite")
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatal("refusal left partial configuration")
			}
		})
	}
}

func TestSuiteOutputCannotEnterRetainedEvidence(t *testing.T) {
	dir, _ := fixture(t, "127.0.0.1:1")
	if _, e := suite.Prepare(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "case-one", "nested")); e == nil {
		t.Fatal("wrote into evidence")
	}
}

func TestSuiteSequenceMustMatchEvidenceOrderNotOnlyTemplateOrder(t *testing.T) {
	dir, doc := fixture(t, "127.0.0.1:1")
	raw, e := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
	if e != nil {
		t.Fatal(e)
	}
	_, e = bundle.Write(filepath.Join(dir, "two-messages"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}, {Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if e != nil {
		t.Fatal(e)
	}
	spec, e := testrunner.ReadSpec(filepath.Join(dir, "booking.json"))
	if e != nil {
		t.Fatal(e)
	}
	spec.Input.Messages = []string{"s0002-e000001", "s0001-e000001"}
	doc.Tests[0].Sequence = spec.Input.Messages
	doc.Tables[0].Rows[0].Case = "two-messages"
	write(t, filepath.Join(dir, "booking.json"), spec)
	write(t, filepath.Join(dir, "suite.json"), doc)
	if _, e = suite.Prepare(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "out")); e == nil {
		t.Fatal("claimed reversed source order was executable")
	}
}

func TestSuiteBindsLedgerObservationWithoutEditingExpectations(t *testing.T) {
	dir, doc := fixture(t, "127.0.0.1:1")
	spec, e := testrunner.ReadSpec(filepath.Join(dir, "booking.json"))
	if e != nil {
		t.Fatal(e)
	}
	n := 1
	spec.Setup.InitialState = "empty-ledger"
	spec.Observation = testrunner.Observation{Boundary: testrunner.LedgerBoundary, Path: "unbound.json"}
	spec.Assertions = []testrunner.Assertion{{ID: "count", Operator: "ledger_count", Expected: testrunner.Value{Count: &n}}}
	write(t, filepath.Join(dir, "booking.json"), spec)
	if _, e = suite.Prepare(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "missing")); e == nil {
		t.Fatal("ledger binding omitted")
	}
	doc.Environments[0].Bindings[0].Observation = "east-observation.json"
	write(t, filepath.Join(dir, "suite.json"), doc)
	prepared, e := suite.Prepare(filepath.Join(dir, "suite.json"), "east", filepath.Join(dir, "out"))
	if e != nil {
		t.Fatal(e)
	}
	generated, e := testrunner.ReadSpec(filepath.Join(prepared.Directory, "booking-one.json"))
	if e != nil || generated.Observation.Path != filepath.Join(dir, "east-observation.json") || *generated.Assertions[0].Expected.Count != 1 {
		t.Fatalf("%+v %v", generated, e)
	}
}

// A pinned run compiles only the suite document it was pinned to, checked on
// the bytes it then compiles, so nothing can change between the check and the
// read: a document rewritten since its identity was taken, or no identity at
// all, is refused before the output exists, and the unchanged document runs.
func TestAPinnedSuiteRunRefusesADocumentThatChangedSinceItsIdentity(t *testing.T) {
	dir, doc := fixture(t, "127.0.0.1:1")
	path := filepath.Join(dir, "suite.json")
	original, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	identity := suite.Identity(original)
	doc.Parallelism++
	write(t, path, doc)
	out := filepath.Join(dir, "out")
	for name, pinned := range map[string]string{"a rewritten suite": identity, "no identity": ""} {
		if _, e := suite.RunPinned(t.Context(), path, "east", out, "", pinned); !errors.Is(e, suite.ErrChanged) {
			t.Fatalf("%s was run: %v", name, e)
		}
		if _, e := os.Lstat(out); !os.IsNotExist(e) {
			t.Fatalf("%s created its output", name)
		}
	}
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	report, e := suite.RunPinned(t.Context(), path, "east", out, "", identity)
	if e != nil || report.Schema == "" || len(report.Jobs) != 1 {
		t.Fatalf("the pinned suite did not run: %+v %v", report, e)
	}
	retained, e := os.ReadFile(filepath.Join(out, "suite.json"))
	if e != nil || string(retained) != string(original) {
		t.Fatalf("the suite retained other bytes than it was pinned to: %v", e)
	}
}
