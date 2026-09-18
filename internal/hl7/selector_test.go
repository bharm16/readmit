package hl7_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
)

func TestSelectorAddressesOccurrencesRepetitionsComponentsAndStates(t *testing.T) {
	raw := []byte("MSH|^~\\&|APP|||||||P|2.5.1\rPID|1||one^^^A&OID&ISO~two^^^B||\"\"|\rPID|2||three^^^C\rZPD|a\\Z^&~|inside\\b^after&end\r")
	doc, err := hl7.Parse(raw, hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, text string
		state      hl7.State
	}{
		{"PID-3", "one^^^A&OID&ISO", hl7.Present}, {"PID-3[2].1", "two", hl7.Present},
		{"PID-3.4.2", "OID", hl7.Present}, {"PID[2]-3.4", "C", hl7.Present},
		{"PID-3.2", "", hl7.Empty}, {"PID-5.2.3", `""`, hl7.Null},
		{"PID-6.2", "", hl7.Empty}, {"PID-7", "", hl7.Omitted},
		{"PID[3]-3", "", hl7.Omitted}, {"PID-3[3]", "", hl7.Omitted},
		{"PID-3.5", "", hl7.Omitted}, {"ZPD-1.2.2", "end", hl7.Present},
		{"ZPD-1.1", `a\Z^&~|inside\b`, hl7.Present},
		{"MSH-1", "|", hl7.Present}, {"MSH-2", `^~\&`, hl7.Present}, {"MSH-2.1", "", hl7.Omitted},
	} {
		t.Run(tc.path, func(t *testing.T) {
			s, err := hl7.ParseSelector(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			value, err := doc.Select(0, s)
			if err != nil {
				t.Fatal(err)
			}
			if value.State != tc.state || string(doc.Bytes(value.Span)) != tc.text {
				t.Fatalf("got %+v %q", value, doc.Bytes(value.Span))
			}
			canonical, err := hl7.ParseSelector(s.String())
			if err != nil || canonical != s {
				t.Fatal("canonical selector changed")
			}
		})
	}
	if !bytes.Equal(doc.Serialize(), raw) {
		t.Fatal("selectors changed evidence")
	}
}

func TestSelectorRejectsInvalidPathsWithoutReflectingData(t *testing.T) {
	for _, path := range []string{"", "SECRET", "PID-0", "PID[0]-3", "PID-3[0]", "PID-3.0", "PID-3.1.0", "PID-3..2", "PID-3.1.2.3", "PID[*]-3", "PID-03", "pid-3", "PID-999999999999999999999999", strings.Repeat("X", 100)} {
		if _, err := hl7.ParseSelector(path); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("invalid selector accepted or disclosed")
		}
	}
	doc, err := hl7.Parse([]byte("MSH|^~\\&|APP\r"), hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := hl7.ParseSelector("PID-3")
	if _, err := doc.Select(-1, s); err == nil {
		t.Fatal("negative index accepted")
	}
	if _, err := doc.Select(1, s); err == nil {
		t.Fatal("missing message accepted")
	}
	if _, err := doc.Select(0, hl7.Selector{}); err == nil {
		t.Fatal("zero selector accepted")
	}
}

func TestDecodeEscapesWithoutChangingEvidence(t *testing.T) {
	delim := hl7.Delimiters{Field: '*', Component: '$', Repetition: '%', Escape: '!', Subcomponent: '?'}
	got, err := hl7.Decode([]byte("a!F!b!S!c!R!d!T!e!E!f!Xff00!"), delim)
	if err != nil || !bytes.Equal(got, []byte{'a', '*', 'b', '$', 'c', '%', 'd', '?', 'e', '!', 'f', 255, 0}) {
		t.Fatalf("got %q %v", got, err)
	}
	for _, raw := range []string{"!ZSECRET!", "!X0!", "!Xgg!", "!X!", "!F", "!!"} {
		if _, err := hl7.Decode([]byte(raw), delim); err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatal("invalid escape accepted or disclosed")
		}
	}
	raw := []byte("literal\\F\\")
	got, err = hl7.Decode(raw, hl7.Delimiters{})
	if err != nil || !bytes.Equal(raw, got) {
		t.Fatal("undeclared escape decoded")
	}
	got[0] = 'x'
	if raw[0] != 'l' {
		t.Fatal("decode shared input bytes")
	}
}

func FuzzSelector(f *testing.F) {
	for _, s := range []string{"PID-3.4.1", "PID[2]-3[2]", "MSH-1", "ZPD-1.1.1", "bad"} {
		f.Add(s)
	}
	doc, err := hl7.Parse([]byte("MSH|^~\\&|APP\rPID|1||one^^^A~two^^^B\r"), hl7.Options{})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, path string) {
		s, err := hl7.ParseSelector(path)
		if err != nil {
			return
		}
		v, err := doc.Select(0, s)
		if err != nil {
			t.Fatal(err)
		}
		if v.Span.Start < 0 || v.Span.End < v.Span.Start || v.Span.End > len(doc.Serialize()) {
			t.Fatal("selector escaped document")
		}
	})
}
