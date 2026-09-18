package hl7_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMalformedOrAmbiguousInputHasBoundedDiagnostics(t *testing.T) {
	header := "MSH|^~\\&|SYNTHETIC\r"
	for name, raw := range map[string]string{
		"empty": "", "garbage": "SECRET-PATIENT", "short MSH": "MSH|^~\r",
		"duplicate delimiters": "MSH|^^\\&|\r", "unknown encoding": "MSH|abc|\r",
		"invalid delimiter": "MSH ^~\\& \r", "quote delimiter": "MSH|^~\\\"|\r",
		"truncated frame": "\x0b" + header, "missing frame CR": "\x0b" + header + "\x1c",
		"invalid frame CR":       "\x0b" + header + "\x1c\n",
		"trailing frame garbage": "\x0b" + header + "\x1c\rSECRET-PATIENT",
		"empty frame":            "\x0b\x1c\r", "nested frame": "\x0b\x0b" + header + "\x1c\r",
		"two raw messages":        header + header,
		"two messages in a frame": "\x0b" + header + header + "\x1c\r",
		"mixed endings":           header + "PID|1\n",
		"missing final ending":    header + "PID|1",
		"bad segment ID":          header + "piD|1\r",
		"wrong separator":         header + "PID*1\r",
		"control bytes":           header + "PID|SECRET-PATIENT\x00\r",
		"unterminated escape":     header + "ZPD|SECRET-PATIENT\\F\r",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := hl7.Parse([]byte(raw), hl7.Options{})
			if err == nil {
				t.Fatal("accepted malformed or ambiguous input")
			}
			if len(err.Error()) > 256 || strings.Contains(err.Error(), "SECRET-PATIENT") {
				t.Fatalf("unsafe diagnostic: %s", err)
			}
		})
	}
	for _, opts := range []hl7.Options{{Format: "secret"}, {Terminator: "secret"}, {Format: hl7.MLLP}, {Terminator: hl7.LF}} {
		if _, err := hl7.Parse([]byte(header), opts); err == nil {
			t.Errorf("accepted invalid declaration %+v", opts)
		}
	}
}

func TestRawMessagePreservesEvidenceAndFieldStates(t *testing.T) {
	raw := fixture(t, "adt-cr.hl7")
	doc, err := hl7.Parse(raw, hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Format != hl7.Raw || len(doc.Messages) != 1 || doc.Messages[0].Terminator != hl7.CR {
		t.Fatalf("unexpected input description: %+v", doc)
	}
	msg := doc.Messages[0]
	if len(msg.Segments) != 5 {
		t.Fatalf("got %d segments, want 5", len(msg.Segments))
	}
	pid := msg.Segments[2]
	for n, want := range map[int]hl7.State{2: hl7.Empty, 5: hl7.Present, 6: hl7.Null, 7: hl7.Empty, 8: hl7.Omitted} {
		if got := pid.Field(n).State; got != want {
			t.Errorf("PID-%d state = %s, want %s", n, got, want)
		}
	}
	if got := string(doc.Bytes(pid.Field(3).Span)); got != "SYNTH-001^^^READMIT^MR~ALT-001^^^READMIT^PI" {
		t.Errorf("PID-3 = %q", got)
	}
	if got := string(doc.Bytes(msg.Segments[0].Field(1).Span)); got != "|" {
		t.Errorf("MSH-1 = %q", got)
	}
	if got := string(doc.Bytes(msg.Segments[0].Field(2).Span)); got != `^~\&` {
		t.Errorf("MSH-2 = %q", got)
	}
	if !bytes.Equal(doc.Serialize(), raw) {
		t.Fatal("untouched evidence did not round-trip")
	}
	// Neither caller-owned input nor returned byte views can mutate evidence.
	raw[0] = 'X'
	copy := doc.Serialize()
	copy[0] = 'Y'
	view := doc.Bytes(pid.Field(3).Span)
	view[0] = 'Z'
	if !bytes.Equal(doc.Serialize(), fixture(t, "adt-cr.hl7")) {
		t.Fatal("evidence was mutable through a byte slice")
	}
}

func TestFormatsRoundTripWithoutSplittingOrMerging(t *testing.T) {
	for _, tc := range []struct {
		file       string
		format     hl7.Format
		terminator hl7.Terminator
		messages   int
	}{
		{"adt-cr.hl7", hl7.Raw, hl7.CR, 1},
		{"siu-lf.hl7", hl7.Raw, hl7.LF, 1},
		{"ack-crlf.hl7", hl7.Raw, hl7.CRLF, 1},
		{"custom-delimiters.hl7", hl7.Raw, hl7.CR, 1},
		{"two-messages.mllp", hl7.MLLP, hl7.CR, 2},
		{"non-utf8.hl7", hl7.Raw, hl7.CR, 1},
		{"reduced-delimiters.hl7", hl7.Raw, hl7.CR, 1},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw := fixture(t, tc.file)
			for _, options := range []hl7.Options{{}, {Format: tc.format, Terminator: tc.terminator}} {
				doc, err := hl7.Parse(raw, options)
				if err != nil {
					t.Fatal(err)
				}
				if doc.Format != tc.format || len(doc.Messages) != tc.messages || doc.Messages[0].Terminator != tc.terminator {
					t.Fatalf("unexpected input description: %+v", doc)
				}
				if !bytes.Equal(doc.Serialize(), raw) {
					t.Fatal("round-trip changed evidence")
				}
			}
		})
	}
}

func TestOptionalEncodingCharactersCanBeOmitted(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		escape byte
	}{
		{"MSH|^~|APP|||||||P|2.5.1\rZPD|literal\\without escape~two\r", 0},
		{"MSH|^~\\|APP|||||||P|2.5.1\rZPD|literal\\F\\pipe~two\r", '\\'},
	} {
		doc, err := hl7.Parse([]byte(tc.raw), hl7.Options{})
		if err != nil {
			t.Fatal(err)
		}
		message := doc.Messages[0]
		if message.Delimiters.Escape != tc.escape || message.Delimiters.Subcomponent != 0 || len(message.Segments[1].Field(1).Repetitions) != 2 {
			t.Fatalf("omitted encoding characters mishandled: %+v", message)
		}
		if !bytes.Equal(doc.Serialize(), []byte(tc.raw)) {
			t.Fatal("changed reduced delimiter declaration")
		}
	}
}

func TestRepetitionsRespectEscapesAndTrailingEmpties(t *testing.T) {
	doc, err := hl7.Parse(fixture(t, "custom-delimiters.hl7"), hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	pid := doc.Messages[0].Segments[1]
	if got := len(pid.Field(3).Repetitions); got != 2 {
		t.Fatalf("PID-3 repetitions = %d, want 2", got)
	}
	zpd := doc.Messages[0].Segments[2]
	if len(zpd.Fields) != 5 || len(zpd.Field(1).Repetitions) != 1 {
		t.Fatalf("escape content was split: %+v", zpd)
	}
	if got := string(doc.Bytes(zpd.Field(1).Span)); got != "local!Zraw*inside%escape!" {
		t.Fatalf("local escape = %q", got)
	}
	raw := []byte("MSH|^~\\&#|APP|||||||P|2.8\rZXX\rZPD|one~\"\"~~\r")
	doc, err = hl7.Parse(raw, hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Messages[0].Delimiters.Truncation != '#' || doc.Messages[0].Segments[1].Field(1).State != hl7.Omitted {
		t.Fatal("optional truncation character or omitted segment fields lost")
	}
	reps := doc.Messages[0].Segments[2].Field(1).Repetitions
	if len(reps) != 4 || reps[1].State != hl7.Null || reps[2].State != hl7.Empty || reps[3].State != hl7.Empty {
		t.Fatalf("repetition states = %+v", reps)
	}
	if !bytes.Equal(doc.Serialize(), raw) {
		t.Fatal("round-trip changed evidence")
	}
}

func TestResourceLimits(t *testing.T) {
	if _, err := hl7.Parse(make([]byte, hl7.MaxInputBytes+1), hl7.Options{}); err == nil {
		t.Fatal("accepted oversized input")
	}
	for _, payload := range []string{strings.Repeat("|", 200001), strings.Repeat("~", 200001)} {
		if _, err := hl7.Parse([]byte("MSH|^~\\&|\rZPD|"+payload+"\r"), hl7.Options{}); err == nil {
			t.Fatal("accepted excessive syntax nodes")
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, name := range []string{"adt-cr.hl7", "siu-lf.hl7", "ack-crlf.hl7", "custom-delimiters.hl7", "two-messages.mllp", "non-utf8.hl7", "reduced-delimiters.hl7"} {
		raw, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		doc, err := hl7.Parse(raw, hl7.Options{})
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if !bytes.Equal(doc.Serialize(), raw) {
			t.Fatal("accepted input changed on serialization")
		}
		for _, message := range doc.Messages {
			for _, segment := range message.Segments {
				for _, field := range segment.Fields {
					if field.Span.Start < segment.Span.Start || field.Span.End > segment.Span.End || field.Span.Start > field.Span.End {
						t.Fatal("field outside segment")
					}
				}
			}
		}
	})
}
