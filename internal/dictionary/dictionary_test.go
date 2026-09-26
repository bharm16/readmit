package dictionary_test

import (
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
)

// TestDeclaredReadsTheVersionAndFamilyOnce pins the one declared-version
// reader: MSH-12.1 and MSH-9.1 of the first repetition, as the bytes are
// written, through the shared selector. A componentised declaration such as
// 2.5.1^USA and a repeated MSH-12 are read at their first component, so
// every caller that asks is answered identically.
func TestDeclaredReadsTheVersionAndFamilyOnce(t *testing.T) {
	for _, tc := range []struct {
		name         string
		message      string
		version      string
		family       string
		messageIndex int
	}{
		{"plain", "MSH|^~\\&|A|B|C|D|20260101000000||SIU^S12|X|P|2.5.1\rSCH|1\r", "2.5.1", "SIU", 0},
		{"componentised", "MSH|^~\\&|A|B|C|D|20260101000000||SIU^S12^SIU_S12|X|P|2.5.1^USA\rSCH|1\r", "2.5.1", "SIU", 0},
		{"repeated", "MSH|^~\\&|A|B|C|D|20260101000000||SIU^S12^SIU_S12|X|P|2.5.1^USA~2.4\rSCH|1\r", "2.5.1", "SIU", 0},
		{"repeated without components", "MSH|^~\\&|A|B|C|D|20260101000000||SIU^S12|X|P|2.5.1~2.9\rSCH|1\r", "2.5.1", "SIU", 0},
		{"other version first", "MSH|^~\\&|A|B|C|D|20260101000000||SIU^S12|X|P|2.9^ZZZ~2.5.1\rSCH|1\r", "2.9", "SIU", 0},
		{"omitted header fields", "MSH|^~\\&|A|B|C|D|20260101000000\rSCH|1\r", "", "", 0},
		{"absent message", "MSH|^~\\&|A|B|C|D|20260101000000||SIU^S12|X|P|2.5.1\rSCH|1\r", "", "", 1},
	} {
		doc, err := hl7.Parse([]byte(tc.message), hl7.Options{})
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		if got := dictionary.Declared(doc, tc.messageIndex); got != (dictionary.Combination{Version: tc.version, Family: tc.family}) {
			t.Fatalf("%s: declared %+v", tc.name, got)
		}
		// Consulting the declaration never touches the evidence bytes.
		if string(doc.Serialize()) != tc.message {
			t.Fatalf("%s: the declaration read changed the bytes", tc.name)
		}
	}
	if got := dictionary.Declared(nil, 0); got != (dictionary.Combination{}) {
		t.Fatalf("no document declared %+v", got)
	}
}

// TestLoadIsParsedOnceAndAnswersPositions pins the label module's surface:
// one parse shared by every caller, applicability decided by the module, and
// label, status answers per position without any caller walking the raw table.
func TestLoadIsParsedOnceAndAnswersPositions(t *testing.T) {
	named, err := dictionary.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	again, err := dictionary.Load()
	if err != nil || again != named {
		t.Fatal("every caller must be answered from the one parse")
	}
	if named.Contract != "readmit-field-labels/v1" || named.HL7Version != "2.5.1" {
		t.Fatalf("the bundled contract changed: %q %q", named.Contract, named.HL7Version)
	}
	if dictionary.Provenance == "" || dictionary.Provenance[:5] != "nHapi" {
		t.Fatalf("the recorded provenance is missing: %q", dictionary.Provenance)
	}
	if !named.Applies(dictionary.Combination{Version: "2.5.1", Family: "SIU"}) || named.Applies(dictionary.Combination{Version: "2.6", Family: "SIU"}) {
		t.Fatal("applicability is decided on the declared version")
	}
	if named.Applies(dictionary.Combination{}) {
		t.Fatal("a message that declares nothing is not labelled")
	}
	if got := named.Label("PID", 3); got != "Patient Identifier List" {
		t.Fatalf("PID-3: %q", got)
	}
	if got := named.Label("PID", 999); got != "" || named.Label("ZZZ", 1) != "" {
		t.Fatal("an unlabelled position must answer an empty name")
	}
	if !named.LabelsSegment("PID") || named.LabelsSegment("ZZZ") {
		t.Fatal("segment applicability is wrong")
	}
	if named.LastPosition("ZZZ") != 0 {
		t.Fatal("an unlabelled segment has no last position")
	}
	if named.LastPosition("PID") < 3 {
		t.Fatal("the last labelled position must cover the labels it carries")
	}
	seen := 0
	named.Positions(func(segment string, position int, name string) {
		if segment == "" || position < 1 || name == "" || named.Label(segment, position) != name {
			t.Fatalf("inconsistent position %s-%d", segment, position)
		}
		seen++
	})
	if seen == 0 {
		t.Fatal("no labelled position was visited")
	}
}

// TestAtAnswersTheStatusVocabulary pins the position answers the desktop
// inspector reports, so a selection is described the same way wherever it is
// shown.
func TestAtAnswersTheStatusVocabulary(t *testing.T) {
	named, err := dictionary.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tc := range []struct {
		name     string
		position dictionary.Position
		status   string
		label    string
	}{
		{"whole message", dictionary.Position{Kind: "message"}, dictionary.StatusFieldLabelsOnly, ""},
		{"labelled segment", dictionary.Position{Kind: "segment", Segment: "PID"}, dictionary.StatusFieldLabelsOnly, ""},
		{"unlabelled segment", dictionary.Position{Kind: "segment", Segment: "ZZZ"}, dictionary.StatusUnsupportedSegment, ""},
		{"labelled field", dictionary.Position{Kind: "field", Segment: "PID", Field: 3}, dictionary.StatusLabeledField, "Patient Identifier List"},
		{"component of a labelled field", dictionary.Position{Kind: "component", Segment: "PID", Field: 3}, dictionary.StatusLabeledField, "Patient Identifier List"},
		{"unlabelled position", dictionary.Position{Kind: "field", Segment: "PID", Field: 999}, dictionary.StatusUnlabeledPosition, ""},
		{"unsupported segment field", dictionary.Position{Kind: "field", Segment: "ZZZ", Field: 1}, dictionary.StatusUnsupportedSegment, ""},
	} {
		if got := named.At(tc.position); got.Status != tc.status || got.Label != tc.label {
			t.Fatalf("%s: %+v", tc.name, got)
		}
	}
	if !reflect.DeepEqual(named.At(dictionary.Position{Kind: "field", Segment: "PID", Field: 3}), dictionary.Answer{Status: dictionary.StatusLabeledField, Label: "Patient Identifier List"}) {
		t.Fatal("the answer carries the status and the label and nothing else")
	}
}
