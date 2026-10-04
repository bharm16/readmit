package diff_test

import (
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"path/filepath"
	"testing"
	"time"
)

func TestExplicitOccurrencePairDoesNotInferIdentityAlignmentOrIgnoreFields(t *testing.T) {
	path := sourceCase(t, "diff-before.mllp")
	report, err := diff.CompareSelected(diff.Input{Path: path}, diff.Input{Path: path}, "s0001-e000002", "s0001-e000001", diff.Options{ShowValues: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Pairs) != 1 || report.Alignment != "explicit-occurrences" || report.Pairs[0].Left.Occurrence != "s0001-e000002" || report.Pairs[0].Right.Occurrence != "s0001-e000001" || len(report.Ignore) != 0 {
		t.Fatalf("explicit pair: %+v", report)
	}
	if _, err = diff.CompareSelected(diff.Input{Path: path}, diff.Input{Path: path}, "absent", "s0001-e000001", diff.Options{}); err == nil {
		t.Fatal("unknown source occurrence was guessed")
	}
}

func TestExplicitOccurrencePairKeepsEmptyNullOmittedRepeatsAndRawBytes(t *testing.T) {
	base := "MSH|^~\\&|S|F|R|F|20261003120000||ADT^A01|UNCHANGED|P|2.5.1\r"
	path := filepath.Join(t.TempDir(), "case")
	_, err := bundle.Write(path, []bundle.Input{{Data: append(mllp.Frame([]byte(base+"ZTX||\"\"|one~two|\rZRB|\xff\r")), mllp.Frame([]byte(base+"ZTX|\"\"||one~three\rZRB|\xfe\r"))...)}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{Seed: 17, BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "selected-v1", ProfileVersion: "states-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := diff.CompareSelected(diff.Input{Path: path}, diff.Input{Path: path}, "s0001-e000001", "s0001-e000002", diff.Options{ShowValues: true})
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string][2]hl7.State{"ZTX[1]-1[1]": {hl7.Empty, hl7.Null}, "ZTX[1]-2[1]": {hl7.Null, hl7.Empty}, "ZTX[1]-4[1]": {hl7.Empty, hl7.Omitted}}
	repeated, raw := false, false
	for _, field := range report.Pairs[0].Fields {
		if states, ok := wanted[field.Selector]; ok {
			if field.Left.State != states[0] || field.Right.State != states[1] {
				t.Fatalf("%s changed states: %+v", field.Selector, field)
			}
			delete(wanted, field.Selector)
		}
		if field.Selector == "ZTX[1]-3[2]" {
			repeated = field.Left.Display != nil && field.Right.Display != nil && *field.Left.Display != *field.Right.Display
		}
		if field.Selector == "ZRB[1]-1[1]" {
			raw = field.Left.Display != nil && field.Right.Display != nil && *field.Left.Display != *field.Right.Display
		}
	}
	if len(wanted) != 0 || !repeated || !raw {
		t.Fatalf("lost field semantics: remaining=%v repeated=%t raw=%t fields=%+v", wanted, repeated, raw, report.Pairs[0].Fields)
	}
}
