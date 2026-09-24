package hl7_test

import (
	"errors"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
)

// A date shift moves exactly MSH-7 and both appointment endpoints of every SCH
// occurrence and SCH-11 repetition, whole seconds with or without an offset,
// and leaves empty, null and omitted endpoints as they are.
func TestShiftTimestampsMovesMSH7AndEveryAppointmentEndpoint(t *testing.T) {
	doc := parse(t, "MSH|^~\\&|APP|SITE|RECEIVER|LAB|20260101120000||SIU^S12|MSG-001|P|2.5.1\r"+
		"SCH|A|B|||||||||^^^20260102120000+0000^20260102123000+0000~^^^\"\"^\r"+
		"PID|1||MRN^^^AUTH||||19800210000000\r"+
		"SCH|C|D|||||||||^^^20261231233000\r")
	edits, err := doc.ShiftTimestamps(0, 90*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{
		{"MSH[1]-7[1]", "20260101133000"},
		{"SCH[1]-11[1].4", "20260102133000+0000"},
		{"SCH[1]-11[1].5", "20260102140000+0000"},
		{"SCH[2]-11[1].4", "20270101010000"},
	}
	if len(edits) != len(want) {
		t.Fatalf("got %d edits", len(edits))
	}
	for i, edit := range edits {
		if edit.Selector.String() != want[i][0] || string(edit.Value) != want[i][1] || edit.RemoveSegment {
			t.Fatalf("edit %d: %s %q", i, edit.Selector, edit.Value)
		}
	}
	// The edits are an ordinary rewrite; nothing else in the message moves.
	result, err := doc.Rewrite(0, edits, hl7.DeclaredDelimiters)
	if err != nil || string(result.Bytes) != "MSH|^~\\&|APP|SITE|RECEIVER|LAB|20260101133000||SIU^S12|MSG-001|P|2.5.1\r"+
		"SCH|A|B|||||||||^^^20260102133000+0000^20260102140000+0000~^^^\"\"^\r"+
		"PID|1||MRN^^^AUTH||||19800210000000\r"+
		"SCH|C|D|||||||||^^^20270101010000\r" {
		t.Fatalf("%q %v", result.Bytes, err)
	}
	// A message declaring none of the positions moves nothing.
	if edits, err := parse(t, "MSH|^~\\&|APP\rPID|1\r").ShiftTimestamps(0, time.Hour); err != nil || len(edits) != 0 {
		t.Fatalf("%v %v", edits, err)
	}
}

// Anything the shift cannot move and read back as the same kind of timestamp
// refuses the whole shift, by name.
func TestShiftTimestampsRefusesATimestampItCannotMove(t *testing.T) {
	for value, refusal := range map[string]error{
		"202601011200":          hl7.ErrShiftTimestamp,
		"20260101120000.5":      hl7.ErrShiftTimestamp,
		"20260101120000+00":     hl7.ErrShiftTimestamp,
		"20260231120000":        hl7.ErrShiftTimestamp,
		"2026010112000A":        hl7.ErrShiftTimestamp,
		"99991231230000":        hl7.ErrShiftYear,
		"99991231230000+0000":   hl7.ErrShiftYear,
		"20260101120000 +0000":  hl7.ErrShiftTimestamp,
		"20260101120000+0000^X": hl7.ErrShiftTimestamp,
	} {
		doc := parse(t, "MSH|^~\\&|APP|SITE|RECEIVER|LAB|20260101120000||SIU^S12\rSCH|A|B|||||||||^^^"+value+"\r")
		if _, err := doc.ShiftTimestamps(0, 2*time.Hour); !errors.Is(err, refusal) {
			t.Errorf("%q: %v", value, err)
		}
	}
	if _, err := parse(t, "MSH|^~\\&|APP||||00010101010000\r").ShiftTimestamps(0, -2*time.Hour); !errors.Is(err, hl7.ErrShiftYear) {
		t.Fatalf("before year 1: %v", err)
	}
	for _, index := range []int{-1, 1} {
		if _, err := parse(t, "MSH|^~\\&|APP\r").ShiftTimestamps(index, time.Hour); err == nil {
			t.Fatalf("message %d was shifted", index)
		}
	}
}

// A shift is a nonzero whole-second duration within ten 365-day years.
func TestParseShiftHoldsADateShiftToWholeSecondsWithinTenYears(t *testing.T) {
	for value, want := range map[string]time.Duration{"24h": 24 * time.Hour, "-2h": -2 * time.Hour, "1s": time.Second, "87600h": 87600 * time.Hour, "-87600h": -87600 * time.Hour} {
		if got, err := hl7.ParseShift(value); err != nil || got != want {
			t.Errorf("%q: %v %v", value, got, err)
		}
	}
	for _, value := range []string{"", "0s", "0", "1.5s", "1ms", "87600h1s", "-87600h1s", "one day", "24"} {
		if _, err := hl7.ParseShift(value); !errors.Is(err, hl7.ErrShiftDuration) {
			t.Errorf("%q: %v", value, err)
		}
	}
}
