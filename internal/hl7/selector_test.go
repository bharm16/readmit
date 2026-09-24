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
			rebuilt, err := hl7.NewSelector(s.Parts())
			if err != nil || rebuilt != s {
				t.Fatal("selector built from its own parts changed")
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

func TestSelectorPartsAreACopyAndBuildTheSameSelector(t *testing.T) {
	s, err := hl7.ParseSelector("PID[2]-3[4].5.6")
	if err != nil {
		t.Fatal(err)
	}
	parts := s.Parts()
	if parts != (hl7.Parts{Segment: "PID", Occurrence: 2, Field: 3, Repetition: 4, Component: 5, Subcomponent: 6}) {
		t.Fatalf("parts %+v", parts)
	}
	parts.Occurrence = 7
	if s.String() != "PID[2]-3[4].5.6" {
		t.Fatal("changing the parts changed the selector")
	}
	moved, err := hl7.NewSelector(parts)
	if err != nil || moved.String() != "PID[7]-3[4].5.6" {
		t.Fatalf("got %q %v", moved, err)
	}
	defaults, err := hl7.ParseSelector("MSA-1")
	if err != nil || defaults.Parts() != (hl7.Parts{Segment: "MSA", Occurrence: 1, Field: 1, Repetition: 1}) {
		t.Fatalf("defaulted parts %+v", defaults.Parts())
	}
	if (hl7.Selector{}).Parts() != (hl7.Parts{}) {
		t.Fatal("the zero selector has positions")
	}
}

func TestNewSelectorRefusesWhatTheGrammarRefuses(t *testing.T) {
	valid := hl7.Parts{Segment: "PID", Occurrence: 1, Field: 3, Repetition: 1}
	for name, reshape := range map[string]func(*hl7.Parts){
		"empty segment":                  func(p *hl7.Parts) { p.Segment = "" },
		"lowercase segment":              func(p *hl7.Parts) { p.Segment = "pid" },
		"long segment":                   func(p *hl7.Parts) { p.Segment = "SECRET" },
		"digit first":                    func(p *hl7.Parts) { p.Segment = "1ID" },
		"zero occurrence":                func(p *hl7.Parts) { p.Occurrence = 0 },
		"zero field":                     func(p *hl7.Parts) { p.Field = 0 },
		"zero repetition":                func(p *hl7.Parts) { p.Repetition = 0 },
		"negative component":             func(p *hl7.Parts) { p.Component = -1 },
		"negative subcomponent":          func(p *hl7.Parts) { p.Component, p.Subcomponent = 1, -1 },
		"subcomponent without component": func(p *hl7.Parts) { p.Subcomponent = 1 },
		"position past the syntax limit": func(p *hl7.Parts) { p.Field = 200001 },
	} {
		t.Run(name, func(t *testing.T) {
			parts := valid
			reshape(&parts)
			if _, err := hl7.NewSelector(parts); err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("invalid parts accepted or disclosed")
			}
		})
	}
	s, err := hl7.NewSelector(hl7.Parts{Segment: "ZZ9", Occurrence: 200000, Field: 200000, Repetition: 200000, Component: 200000, Subcomponent: 200000})
	if err != nil {
		t.Fatal(err)
	}
	if parsed, err := hl7.ParseSelector(s.String()); err != nil || parsed != s {
		t.Fatal("the largest selector does not parse back from its canonical form")
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
