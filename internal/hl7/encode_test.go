package hl7_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

func TestTimeEncodesUTCWholeSecondsAndAnExplicitOffset(t *testing.T) {
	utc := time.Date(2026, 9, 20, 3, 4, 5, 0, time.UTC)
	if got := hl7.Time(utc); got != "20260920030405+0000" {
		t.Fatalf("Time = %q", got)
	}
	zone := time.FixedZone("TEST", 5*3600+30*60)
	if got := hl7.Time(time.Date(2026, 9, 20, 3, 4, 5, 0, zone)); got != "20260919213405+0000" {
		t.Fatalf("Time did not coerce to UTC: %q", got)
	}
	// The layout itself stays available for the one writer whose contract is
	// a declared offset rather than UTC.
	if got := time.Date(2026, 9, 20, 3, 4, 5, 0, zone).Format(hl7.TimestampLayout); got != "20260920030405+0530" {
		t.Fatalf("TimestampLayout = %q", got)
	}
}

func TestEncodeJoinsFieldsTerminatesSegmentsAndPreservesDeclaredEmpties(t *testing.T) {
	segments := [][]string{
		{"MSH", "^~\\&", "READMIT", "SYNTHETIC", "RECEIVER", "READMIT", "20260920030405+0000", "", "SIU^S12", "SYNTH-000001", "T", "2.5.1"},
		{"SCH", "P^READMIT", "F^READMIT", "", "", "", "CHECKUP"},
		{"PID", "1", "", "P1^^^READMIT", "", "SYNTHETIC^PATIENT"},
	}
	want := "MSH|^~\\&|READMIT|SYNTHETIC|RECEIVER|READMIT|20260920030405+0000||SIU^S12|SYNTH-000001|T|2.5.1\r" +
		"SCH|P^READMIT|F^READMIT||||CHECKUP\r" +
		"PID|1||P1^^^READMIT||SYNTHETIC^PATIENT\r"
	if got := hl7.Encode(segments); !bytes.Equal(got, []byte(want)) {
		t.Fatalf("Encode = %q, want %q", got, want)
	}
	if got := hl7.Encode(nil); len(got) != 0 {
		t.Fatalf("Encode(nil) = %q", got)
	}
	framed := mllp.Frame(hl7.Encode(segments))
	if framed[0] != 0x0b || framed[len(framed)-2] != 0x1c || framed[len(framed)-1] != '\r' {
		t.Fatalf("Frame = %v", framed)
	}
}
