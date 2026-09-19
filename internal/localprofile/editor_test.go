package localprofile_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
)

func opened(t *testing.T) *localprofile.Editor {
	t.Helper()
	editor, err := localprofile.Open(decoded(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return editor
}

// The editor writes the document a reader reads: the fixture opened and
// written again is the fixture, byte for byte, so nothing is normalized,
// dropped or invented on the way through.
func TestTheFixtureSurvivesBeingOpenedAndWritten(t *testing.T) {
	encoded, err := opened(t).Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != string(fixture(t, "local-profile.json")) {
		t.Fatalf("the profile was rewritten:\n%s", encoded)
	}
}

// Every rule kind the required delivery names is edited here through the
// public interface, and the result is a document the reader accepts.
func TestEveryRuleKindIsEditedThroughTheInterface(t *testing.T) {
	base := localprofile.Base{
		Pack:       profilepack.Identity{ID: "fixture-siu", Version: "1"},
		HL7Version: "2.5.1",
		Family:     "ADT",
	}
	if _, err := localprofile.NewEditor(localprofile.Identity{ID: "1-site", Version: "1"}, base); err == nil {
		t.Fatal("began a profile whose identity could never be written")
	}
	editor, err := localprofile.NewEditor(localprofile.Identity{ID: "site-adt", Version: "1"}, base)
	if err != nil {
		t.Fatalf("new editor: %v", err)
	}
	// A profile with no segment is not a document yet, and Encode says so
	// rather than writing one that constrains nothing.
	if _, err := editor.Encode(); err == nil {
		t.Fatal("wrote a profile that constrains no segment")
	}
	for _, step := range []struct {
		name string
		do   func() error
	}{
		{"a local code table", func() error {
			return editor.SetTerminology(localprofile.TerminologySet{
				ID:      "site-consent",
				Binding: localprofile.BindingRequired,
				Codes:   []localprofile.Code{{Code: "Y", Display: "Consented"}, {Code: "N"}},
			})
		}},
		{"an assigning authority", func() error {
			return editor.SetAuthority(localprofile.Authority{
				ID: "site-mrn", Namespace: "SITE",
				UniversalID: "2.16.840.1.113883.3.72", UniversalIDType: "ISO",
			})
		}},
		{"a date rule", func() error {
			return editor.SetDate(localprofile.DateHandling{
				ID: "admit-instant", Precision: localprofile.PrecisionSecond, TimeZone: localprofile.TimeZoneRequired,
			})
		}},
		{"a site-defined Z-segment", func() error {
			return editor.SetSegment(localprofile.Segment{
				ID:          "ZCN",
				Description: "The consent extension this site adds.",
				Cardinality: &localprofile.Cardinality{Min: 0, Max: localprofile.Unbounded},
				Fields:      []localprofile.Field{{Position: 1, Name: "Consent set id", Usage: localprofile.UsageRequired, Type: "SI"}},
			})
		}},
		{"a cardinality", func() error {
			return editor.SetField("ZCN", localprofile.Field{
				Position: 1, Name: "Consent set id", Usage: localprofile.UsageRequired,
				Cardinality: &localprofile.Cardinality{Min: 1, Max: "1"}, Type: "SI",
			})
		}},
		{"a conditional requirement bound to a code table", func() error {
			return editor.SetField("ZCN", localprofile.Field{
				Position: 2, Name: "Consent given", Usage: localprofile.UsageConditional,
				Condition: &localprofile.Condition{
					Segment: "ZCN", Position: 1, Operator: localprofile.ConditionValueIn, Values: []string{"1"},
				},
				Type: "ID", Terminology: "site-consent",
			})
		}},
		{"an identifier bound to an authority", func() error {
			return editor.SetField("ZCN", localprofile.Field{
				Position: 3, Name: "Consent record number", Usage: localprofile.UsageRequiredOrEmpty,
				Type: "CX", Authority: "site-mrn",
			})
		}},
		{"a date-carrying field bound to a date rule", func() error {
			return editor.SetField("ZCN", localprofile.Field{
				Position: 4, Name: "Consent recorded at", Usage: localprofile.UsageOptional,
				Type: "DTM", Date: "admit-instant",
			})
		}},
		{"a field this interface does not support", func() error {
			return editor.SetField("ZCN", localprofile.Field{
				Position: 5, Name: "Retired consent flag", Usage: localprofile.UsageNotSupported,
			})
		}},
		{"a standard segment constrained beside the local one", func() error {
			return editor.SetSegment(localprofile.Segment{
				ID:     "PID",
				Fields: []localprofile.Field{{Position: 3, Usage: localprofile.UsageRequired, Cardinality: &localprofile.Cardinality{Min: 1, Max: localprofile.Unbounded}, Type: "CX", Authority: "site-mrn"}},
			})
		}},
	} {
		if err := step.do(); err != nil {
			t.Fatalf("editing %s: %v", step.name, err)
		}
	}
	encoded, encodeErr := editor.Encode()
	if encodeErr != nil {
		t.Fatalf("encode: %v", encodeErr)
	}
	written, err := localprofile.Decode(encoded)
	if err != nil {
		t.Fatalf("the editor wrote a document its own reader refuses: %v", err)
	}
	if len(written.Segments) != 2 || written.Segments[0].ID != "PID" || written.Segments[1].ID != "ZCN" {
		t.Fatalf("segments %+v", written.Segments)
	}
	if len(written.Segments[1].Fields) != 5 {
		t.Fatalf("the Z-segment carries %d field rules", len(written.Segments[1].Fields))
	}
	// Order is the canonical one whatever order the edits arrived in, so the
	// same profile is the same bytes.
	for i, position := range []int{1, 2, 3, 4, 5} {
		if written.Segments[1].Fields[i].Position != position {
			t.Fatalf("field %d is at position %d", i, written.Segments[1].Fields[i].Position)
		}
	}
}

// A refused edit is an edit that did not happen. Every refusal below leaves
// the profile exactly as it was, so an editor is never part-way through one.
func TestARefusedEditChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(*localprofile.Editor) error
	}{
		{"a segment id the parser would never produce", func(e *localprofile.Editor) error {
			return e.SetSegment(localprofile.Segment{ID: "zpd", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageOptional}}})
		}},
		{"a required field that may be absent", func(e *localprofile.Editor) error {
			return e.SetField("ZPD", localprofile.Field{
				Position: 1, Usage: localprofile.UsageRequired,
				Cardinality: &localprofile.Cardinality{Min: 0, Max: "1"}, Type: "SI",
			})
		}},
		{"a conditional field with no condition", func(e *localprofile.Editor) error {
			return e.SetField("ZPD", localprofile.Field{Position: 7, Usage: localprofile.UsageConditional})
		}},
		{"a code table bound to a field that carries no code", func(e *localprofile.Editor) error {
			return e.SetField("ZPD", localprofile.Field{
				Position: 7, Usage: localprofile.UsageOptional, Type: "NM", Terminology: "local-visit-reason",
			})
		}},
		{"a reference to a set the profile does not declare", func(e *localprofile.Editor) error {
			return e.SetField("ZPD", localprofile.Field{
				Position: 7, Usage: localprofile.UsageOptional, Type: "IS", Terminology: "codes-that-do-not-exist",
			})
		}},
		{"a code table a field still binds to", func(e *localprofile.Editor) error {
			return e.RemoveTerminology("local-visit-reason")
		}},
		{"an authority a field still names", func(e *localprofile.Editor) error {
			return e.RemoveAuthority("local-mrn-authority")
		}},
		{"a date rule a field still names", func(e *localprofile.Editor) error {
			return e.RemoveDate("appointment-instant")
		}},
		{"a field rule of a segment this profile does not constrain", func(e *localprofile.Editor) error {
			return e.SetField("OBX", localprofile.Field{Position: 5, Usage: localprofile.UsageOptional})
		}},
		{"a field rule that is not there", func(e *localprofile.Editor) error {
			return e.RemoveField("ZPD", 99)
		}},
		{"a segment that is not there", func(e *localprofile.Editor) error {
			return e.RemoveSegment("OBX")
		}},
		{"a declared set that is not there", func(e *localprofile.Editor) error {
			return e.RemoveDate("no-such-rule")
		}},
		{"a date rule that requires an offset on a value with no time of day", func(e *localprofile.Editor) error {
			return e.SetDate(localprofile.DateHandling{
				ID: "appointment-instant", Precision: localprofile.PrecisionDay, TimeZone: localprofile.TimeZoneRequired,
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			editor := opened(t)
			before, err := editor.Encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if err := tc.do(editor); err == nil {
				t.Fatal("the edit was accepted")
			}
			after, err := editor.Encode()
			if err != nil {
				t.Fatalf("encode after a refused edit: %v", err)
			}
			if string(before) != string(after) {
				t.Fatalf("a refused edit changed the profile:\n%s", after)
			}
			if editor.Changed() {
				t.Fatal("a refused edit is reported as a change")
			}
		})
	}
}

// Reverting is this surface's cancel: nothing was written anywhere, so
// abandoning the changes restores exactly what was opened.
func TestRevertReturnsToTheProfileThatWasOpened(t *testing.T) {
	editor := opened(t)
	before, err := editor.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if editor.Changed() {
		t.Fatal("an untouched profile is reported as changed")
	}
	if err := editor.SetField("ZPD", localprofile.Field{
		Position: 1, Name: "Something else entirely", Usage: localprofile.UsageOptional,
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := editor.RemoveSegment("SCH"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !editor.Changed() {
		t.Fatal("an edited profile is not reported as changed")
	}
	editor.Revert()
	if editor.Changed() {
		t.Fatal("a reverted profile is still reported as changed")
	}
	after, err := editor.Encode()
	if err != nil {
		t.Fatalf("encode after revert: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("revert did not restore the profile:\n%s", after)
	}
}

// A segment that constrains nothing is not a rule, so the last field rule of a
// segment is not removed on its own: the refusal says to remove the segment,
// and removing the segment is what actually happens.
func TestTheLastFieldRuleOfASegmentIsNotRemovedOnItsOwn(t *testing.T) {
	editor := opened(t)
	for _, position := range []int{1, 2} {
		if err := editor.RemoveField("SCH", position); err != nil {
			t.Fatalf("remove SCH-%d: %v", position, err)
		}
	}
	err := editor.RemoveField("SCH", 11)
	if err == nil {
		t.Fatal("removed the only position a segment constrains")
	}
	if !strings.Contains(err.Error(), "SCH") || !strings.Contains(err.Error(), "remove the segment") {
		t.Fatalf("the refusal does not say what to do instead: %v", err)
	}
	if err := editor.RemoveSegment("SCH"); err != nil {
		t.Fatalf("remove SCH: %v", err)
	}
	written, err := editor.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	profile, err := localprofile.Decode(written)
	if err != nil {
		t.Fatalf("decode what the editor wrote: %v", err)
	}
	if len(profile.Segments) != 1 || profile.Segments[0].ID != "ZPD" {
		t.Fatalf("segments %+v", profile.Segments)
	}
}

// Open refuses a profile a reader would have refused, so an editor never
// begins from a document that could not have been read.
func TestOpenRefusesAProfileTheReaderWouldRefuse(t *testing.T) {
	profile := decoded(t)
	profile.Segments[1].Fields[4].Type = "ST"
	if _, err := localprofile.Open(profile); err == nil {
		t.Fatal("opened a profile the reader refuses")
	}
}

// What the editor hands out is a copy. A caller that keeps a profile and
// changes it cannot reach back into the editor through a shared slice.
func TestTheProfileHandedOutIsACopy(t *testing.T) {
	editor := opened(t)
	held := editor.Profile()
	held.Segments[0].Fields[0].Name = "Rewritten from outside"
	held.Terminology[0].Codes[0].Code = "REWRITTEN"
	held.Segments[1].Fields[2].Condition.Values[0] = "REWRITTEN"
	again := editor.Profile()
	if again.Segments[0].Fields[0].Name == "Rewritten from outside" ||
		again.Terminology[0].Codes[0].Code == "REWRITTEN" ||
		again.Segments[1].Fields[2].Condition.Values[0] == "REWRITTEN" {
		t.Fatal("a caller reached into the editor through a shared slice")
	}
}
