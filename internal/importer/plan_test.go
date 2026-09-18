package importer_test

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

func TestDecodePlanRequiresEveryDeclaration(t *testing.T) {
	complete := `{"schema":"readmit-import-plan/v1","framing":"mllp","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".mllp"]}`
	plan, err := importer.DecodePlan([]byte(complete))
	if err != nil {
		t.Fatalf("complete plan: %v", err)
	}
	if plan.Framing != importer.MLLPFraming || plan.Terminator != "cr" || plan.Encoding != importer.UTF8 || plan.Direction != "inbound" {
		t.Fatalf("plan lost a declaration: %+v", plan)
	}
	for name, document := range map[string]string{
		"missing framing":        `{"schema":"readmit-import-plan/v1","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[]}`,
		"missing terminator":     `{"schema":"readmit-import-plan/v1","framing":"raw","encoding":"utf-8","direction":"inbound","members":[]}`,
		"missing encoding":       `{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","direction":"inbound","members":[]}`,
		"missing direction":      `{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","members":[]}`,
		"missing members":        `{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"inbound"}`,
		"unknown member":         `{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[],"split":"auto"}`,
		"automatic framing":      `{"schema":"readmit-import-plan/v1","framing":"auto","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[]}`,
		"automatic end":          `{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"auto","encoding":"utf-8","direction":"inbound","members":[]}`,
		"boundary without batch": `{"schema":"readmit-import-plan/v1","framing":"raw","batch_boundary":"segment-start","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[]}`,
		"batch without boundary": `{"schema":"readmit-import-plan/v1","framing":"batch","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[]}`,
	} {
		if _, err := importer.DecodePlan([]byte(document)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestDecodePlanReportsAnUnsupportedVersionDistinctly(t *testing.T) {
	_, err := importer.DecodePlan([]byte(`{"schema":"readmit-import-plan/v2","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[]}`))
	if !errors.Is(err, importer.ErrUnsupportedVersion) {
		t.Fatalf("a later contract version was not reported as unsupported: %v", err)
	}
}

func TestDecodePlanDiagnosticsNeverRepeatTheDocument(t *testing.T) {
	_, err := importer.DecodePlan([]byte(`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"sideways","members":[".mrn-1234567"]}`))
	if err == nil {
		t.Fatal("an undeclared direction was accepted")
	}
	if strings.Contains(err.Error(), "sideways") || strings.Contains(err.Error(), "1234567") {
		t.Fatalf("diagnostic repeated the declared value: %v", err)
	}
}

// The two documents this package writes are compared against expectations
// authored here from the documented format, never against output the encoder
// produced. A change in either shape is a new contract version, so this test
// failing is a decision to make rather than a golden file to refresh.
func TestDocumentsMatchTheirIndependentlyAuthoredShape(t *testing.T) {
	declared := importer.Plan{
		Schema: importer.PlanSchema, Framing: importer.BatchFraming, BatchBoundary: importer.SegmentStart,
		Terminator: hl7.CR, Encoding: importer.UTF8, Direction: bundle.Inbound, Members: []string{".hl7"},
	}
	containers := []importer.Container{{
		Kind: importer.FolderContainer, Path: "/evidence/exports", Size: 0, SHA256: "",
		Members: []importer.Member{
			{Name: "a.hl7", Size: 4, SHA256: "aa", State: importer.Included, Records: []importer.Record{{SourceID: "s0001", Offset: 0, Size: 4, Occurrences: 1}}},
			{Name: "notes.txt", Size: 2, SHA256: "bb", State: importer.Excluded, Reason: importer.ReasonSuffix, Records: []importer.Record{}},
		},
	}}
	totals := importer.Totals{Containers: 1, Members: 2, Excluded: 1, Sources: 1, Occurrences: 1}
	planJSON := `{"schema":"readmit-import-plan/v1","framing":"batch","batch_boundary":"segment-start","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".hl7"]}`
	containersJSON := `[{"kind":"folder","path":"/evidence/exports","size":0,"sha256":"","members":[` +
		`{"name":"a.hl7","size":4,"sha256":"aa","state":"included","records":[{"source_id":"s0001","offset":0,"size":4,"occurrences":1}]},` +
		`{"name":"notes.txt","size":2,"sha256":"bb","state":"excluded","reason":"` + importer.ReasonSuffix + `","records":[]}]}]`
	totalsJSON := `{"containers":1,"members":2,"excluded":1,"sources":1,"occurrences":1}`

	preview, err := importer.EncodePreview(importer.Preview{Schema: importer.PreviewSchema, Plan: declared, Containers: containers, Totals: totals})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":"readmit-import-preview/v1","plan":` + planJSON + `,"containers":` + containersJSON + `,"totals":` + totalsJSON + "}\n"
	if string(preview) != want {
		t.Errorf("preview document:\n got %s\nwant %s", preview, want)
	}

	receipt, err := importer.EncodeReceipt(importer.Receipt{
		Schema: importer.ReceiptSchema, Plan: declared,
		ImportedAt:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Case:        importer.CaseRef{Identity: strings.Repeat("a", 64), Schema: "readmit-case/v1", Provenance: "imported"},
		Containers:  containers,
		Quarantined: []importer.Quarantined{{SourceID: "s0001", EventID: "s0001-e000001", Reason: "invalid input at byte 0: expected an MSH header"}},
		Totals:      totals,
	})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"schema":"readmit-import-receipt/v1","plan":` + planJSON + `,"imported_at":"2026-01-02T03:04:05Z","case":` +
		`{"identity":"` + strings.Repeat("a", 64) + `","schema":"readmit-case/v1","provenance":"imported"},"containers":` + containersJSON +
		`,"quarantined":[{"source_id":"s0001","event_id":"s0001-e000001","reason":"invalid input at byte 0: expected an MSH header"}],"totals":` + totalsJSON + "}\n"
	if string(receipt) != want {
		t.Errorf("receipt document:\n got %s\nwant %s", receipt, want)
	}
}

func FuzzImportPlan(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-import-plan/v1","framing":"mllp","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[".mllp"]}`))
	f.Add([]byte(`{"schema":"readmit-import-plan/v1","framing":"batch","batch_boundary":"hl7-batch","terminator":"lf","encoding":"unknown","direction":"unknown","members":[]}`))
	f.Add([]byte(`{"schema":"readmit-import-plan/v2","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"outbound","members":[]}`))
	f.Add([]byte(`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"auto","encoding":"utf-8","direction":"inbound","members":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		plan, err := importer.DecodePlan(data)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if plan.Framing == importer.BatchFraming != (plan.BatchBoundary != "") {
			t.Fatalf("a boundary and batch framing were accepted apart: %+v", plan)
		}
		if plan.Terminator == "auto" || plan.Terminator == "" || plan.Members == nil {
			t.Fatalf("an undeclared terminator or member list was accepted: %+v", plan)
		}
		// Whatever the reader accepts must survive being written back out, so
		// a plan recorded in a receipt reads as the plan that ran.
		encoded, err := json.Marshal(plan, json.Deterministic(true))
		if err != nil {
			t.Fatal("accepted a plan that cannot be recorded")
		}
		again, err := importer.DecodePlan(encoded)
		if err != nil {
			t.Fatalf("a recorded plan did not read back: %v", err)
		}
		if again.Schema != plan.Schema || again.Framing != plan.Framing || again.BatchBoundary != plan.BatchBoundary ||
			again.Terminator != plan.Terminator || again.Encoding != plan.Encoding || again.Direction != plan.Direction ||
			!slices.Equal(again.Members, plan.Members) {
			t.Fatal("a recorded plan read back as a different plan")
		}
	})
}
