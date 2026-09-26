package suite_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// One refusal table at the expansion. Each row mutates the fixture in one way
// the expansion must refuse, and the test holds both faces of that expansion
// to the same refusal: the preview a window shows and the configuration
// directory preparation writes, which is left behind by nothing. The
// evidence-send-order row is the former live gap — a suite whose declared
// send order the selected evidence does not support was once shown by a
// preview that preparation then refused.
func TestTheExpansionRefusesTheSameDeclarationsForPreviewAndPreparation(t *testing.T) {
	for _, kind := range []struct {
		name   string
		mutate func(t *testing.T, dir string, doc suite.Document) (environment, references string)
	}{
		{"unknown-environment", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			return "absent", filepath.Join(dir, "releases.json")
		}},
		{"unknown-assertion", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			n := 1
			doc.Tables[0].Rows[0].Expected = map[string]testrunner.Value{"absent": {Count: &n}}
			return "east", filepath.Join(dir, "releases.json")
		}},
		{"wrong-value", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			n := 1
			doc.Tables[0].Rows[0].Expected = map[string]testrunner.Value{"ack": {Count: &n}}
			return "east", filepath.Join(dir, "releases.json")
		}},
		{"observation", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			doc.Environments[0].Bindings[0].Observation = "ledger.json"
			return "east", filepath.Join(dir, "releases.json")
		}},
		{"sequence", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			doc.Tests[0].Sequence = []string{"s0001-e000002"}
			return "east", filepath.Join(dir, "releases.json")
		}},
		{"missing-case", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			doc.Tables[0].Rows[0].Case = "missing"
			return "east", ""
		}},
		{"template-member", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			raw, e := os.ReadFile(filepath.Join(dir, "booking.json"))
			if e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, "booking.json"), append([]byte(`{"unknown":1,`), raw[1:]...), 0600); e != nil {
				t.Fatal(e)
			}
			return "east", ""
		}},
		{"changed-release", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			raw, e := os.ReadFile(filepath.Join(dir, "booking.json"))
			if e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(dir, "booking.json"), []byte(`{"unknown":1,`+string(raw)[1:]), 0600); e != nil {
				t.Fatal(e)
			}
			return "east", filepath.Join(dir, "releases.json")
		}},
		{"released-row-expectation", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			value := "AE"
			doc.Tables[0].Rows[0].Expected = map[string]testrunner.Value{"ack": {Field: &testrunner.FieldValue{State: hl7.Present, Text: &value}}}
			return "east", filepath.Join(dir, "releases.json")
		}},
		{"evidence-send-order", func(t *testing.T, dir string, doc suite.Document) (string, string) {
			raw, e := os.ReadFile("../../testdata/fixtures/listen-s12.hl7")
			if e != nil {
				t.Fatal(e)
			}
			_, e = bundle.Write(filepath.Join(dir, "two-messages"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}, {Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
			if e != nil {
				t.Fatal(e)
			}
			reversed := []string{"s0002-e000001", "s0001-e000001"}
			doc.Tests[0].Sequence = reversed
			doc.Tables[0].Rows[0].Case = "two-messages"
			return "east", ""
		}},
	} {
		t.Run(kind.name, func(t *testing.T) {
			dir, doc, _ := releasedFixture(t)
			environment, references := kind.mutate(t, dir, doc)
			write(t, filepath.Join(dir, "suite.json"), doc)
			if _, e := suite.Preview(dir, doc, environment, references); e == nil {
				t.Fatal("preview accepted an expansion preparation refuses")
			}
			out := filepath.Join(dir, "refused")
			if _, e := suite.Prepare(suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: environment, Output: out, References: references}); e == nil {
				t.Fatal("preparation accepted an invalid expansion")
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatal("refusal left partial configuration")
			}
		})
	}
}

// The one request is the whole caller-facing choice. Its optional pins are
// refused when incomplete or mutually exclusive — before any suite is read —
// and every valid combination passes validation; the released-pinned run
// combination is exercised for real.
func TestSuiteRequestPinCombinations(t *testing.T) {
	valid := []suite.Request{
		{Path: "suite.json", Environment: "east", Output: "out"},
		{Path: "suite.json", Environment: "east", Output: "out", References: "releases.json"},
		{Path: "suite.json", Environment: "east", Output: "out", Identity: "identity"},
		{Path: "suite.json", Environment: "east", Output: "out", References: "releases.json", Identity: "identity"},
		{Path: "suite.json", Environment: "east", Output: "out", References: "releases.json", Promotion: "promotion.json", PromotionIdentity: "identity", Revision: "fixture-v1"},
	}
	for _, request := range valid {
		if err := request.Validate(); err != nil {
			t.Fatalf("%+v: %v", request, err)
		}
	}
	invalid := []suite.Request{
		{Environment: "east", Output: "out"},
		{Path: "suite.json", Output: "out"},
		{Path: "suite.json", Environment: "east"},
		{Path: "suite.json", Environment: "east", Output: "out", PromotionIdentity: "identity"},
		{Path: "suite.json", Environment: "east", Output: "out", Revision: "fixture-v1"},
		{Path: "suite.json", Environment: "east", Output: "out", Promotion: "promotion.json"},
		{Path: "suite.json", Environment: "east", Output: "out", Promotion: "promotion.json", PromotionIdentity: "identity"},
		{Path: "suite.json", Environment: "east", Output: "out", Promotion: "promotion.json", Revision: "fixture-v1"},
		{Path: "suite.json", Environment: "east", Output: "out", Promotion: "promotion.json", PromotionIdentity: "identity", Revision: "fixture-v1"},
		{Path: "suite.json", Environment: "east", Output: "out", References: "releases.json", Promotion: "promotion.json", PromotionIdentity: "identity", Revision: "fixture-v1", Identity: "identity"},
	}
	for _, request := range invalid {
		if err := request.Validate(); err == nil {
			t.Fatalf("accepted %+v", request)
		}
		if _, err := suite.Prepare(request); err == nil {
			t.Fatalf("prepared %+v", request)
		}
	}
	// A promotion is executed, never prepared: the approved suite compiles
	// under Run with the promotion members, and no preparation of one exists.
	if _, err := suite.Prepare(valid[4]); err == nil {
		t.Fatal("prepared a promoted suite")
	}
	// The released-pinned combination runs for real, through the one Run.
	dir, doc, _ := releasedFixture(t)
	raw, e := os.ReadFile(filepath.Join(dir, "suite.json"))
	if e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(dir, "suite.json"), doc)
	out := filepath.Join(dir, "out")
	report, e := suite.Run(t.Context(), suite.Request{Path: filepath.Join(dir, "suite.json"), Environment: "east", Output: out,
		References: filepath.Join(dir, "releases.json"), Identity: suite.Identity(raw)})
	if e != nil || report.Schema == "" || len(report.Jobs) != 1 {
		t.Fatalf("the released pinned run did not run: %+v %v", report, e)
	}
}
