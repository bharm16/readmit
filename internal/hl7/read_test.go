package hl7_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
)

func parse(t *testing.T, raw string) *hl7.Document {
	t.Helper()
	doc, err := hl7.Parse([]byte(raw), hl7.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// header is an MSH declaring msh18 as its MSH-18 exactly as written, the
// field present even when it is empty.
func header(msh18 string) string {
	return "MSH|^~\\&|APP" + strings.Repeat("|", 15) + msh18 + "\r"
}

func read(t *testing.T, doc *hl7.Document, path string, policy hl7.CharacterSetPolicy) hl7.Reading {
	t.Helper()
	s, err := hl7.ParseSelector(path)
	if err != nil {
		t.Fatal(err)
	}
	reading, err := doc.Read(0, s, policy)
	if err != nil {
		t.Fatal(err)
	}
	return reading
}

func TestReadResolvesTheStandardEscapesWithoutChangingEvidence(t *testing.T) {
	raw := "MSH*$%!?*APP\rZPD*a!F!b!S!c!R!d!T!e!E!f!Xff00!*!X41!\r"
	doc := parse(t, raw)
	for _, policy := range []hl7.CharacterSetPolicy{hl7.IgnoreMSH18, hl7.EnforceMSH18} {
		reading := read(t, doc, "ZPD-1", policy)
		if reading.State != hl7.Present || !bytes.Equal(reading.Decoded, []byte{'a', '*', 'b', '$', 'c', '%', 'd', '?', 'e', '!', 'f', 255, 0}) || reading.Literal {
			t.Fatalf("got %+v", reading)
		}
		if text, ok := read(t, doc, "ZPD-2", policy).Text(); !ok || text != "A" {
			t.Fatalf("got %q %v", text, ok)
		}
	}
	reading := read(t, doc, "ZPD-1", hl7.IgnoreMSH18)
	reading.Decoded[0] = 'x'
	if !bytes.Equal(doc.Serialize(), []byte(raw)) || read(t, doc, "ZPD-1", hl7.IgnoreMSH18).Decoded[0] != 'a' {
		t.Fatal("a reading shared the evidence bytes")
	}
	// Without a declared escape character there is nothing to decode.
	literal := read(t, parse(t, "MSH|^~|APP\rZPD|a\\F\\b\r"), "ZPD-1", hl7.IgnoreMSH18)
	if text, ok := literal.Text(); !ok || text != `a\F\b` {
		t.Fatalf("got %q %v", text, ok)
	}
}

func TestReadNamesOneReasonAndNeverAValue(t *testing.T) {
	for name, tc := range map[string]struct {
		value   string
		reason  hl7.Reason
		decoded []byte
	}{
		"local escape":       {`\ZSECRET\`, hl7.UnsupportedEscape, nil},
		"formatting escape":  {`\.br\`, hl7.UnsupportedEscape, nil},
		"odd hexadecimal":    {`\X0\`, hl7.UnsupportedEscape, nil},
		"bad hexadecimal":    {`\Xgg\`, hl7.UnsupportedEscape, nil},
		"empty hexadecimal":  {`\X\`, hl7.UnsupportedEscape, nil},
		"empty escape":       {`\\`, hl7.UnsupportedEscape, nil},
		"escaped invalid":    {`SECRET\XFF\`, hl7.InvalidUTF8, []byte("SECRET\xff")},
		"raw invalid":        {"SECRET\xc3", hl7.InvalidUTF8, []byte("SECRET\xc3")},
		"surrogate encoding": {"\xed\xa0\x80", hl7.InvalidUTF8, []byte("\xed\xa0\x80")},
	} {
		t.Run(name, func(t *testing.T) {
			doc := parse(t, header("UNICODE UTF-8")+"ZPD|"+tc.value+"\r")
			for _, policy := range []hl7.CharacterSetPolicy{hl7.IgnoreMSH18, hl7.EnforceMSH18} {
				reading := read(t, doc, "ZPD-1", policy)
				if reading.State != hl7.Present || reading.Reason != tc.reason || !bytes.Equal(reading.Decoded, tc.decoded) {
					t.Fatalf("got %+v", reading)
				}
				if text, ok := reading.Text(); ok || text != "" || strings.Contains(string(reading.Reason), "SECRET") {
					t.Fatal("a value that is not text read as text or disclosed")
				}
				if string(doc.Bytes(reading.Span)) != tc.value {
					t.Fatal("the reading lost the original span")
				}
			}
		})
	}
}

func TestEnforcingMSH18ReadsOnlyTheDeclaredCharacterSet(t *testing.T) {
	const accented = "José"
	for name, tc := range map[string]struct {
		declaration string
		set         hl7.CharacterSet
		readable    bool
		accented    hl7.Reason // under EnforceMSH18
		plain       hl7.Reason // under EnforceMSH18
	}{
		"omitted":             {"MSH|^~\\&|APP\r", hl7.ASCII, true, hl7.UndeclaredCharacterSet, ""},
		"empty":               {header(""), hl7.ASCII, true, hl7.UndeclaredCharacterSet, ""},
		"ASCII":               {header("ASCII"), hl7.ASCII, true, hl7.UndeclaredCharacterSet, ""},
		"UTF-8":               {header("UNICODE UTF-8"), hl7.UTF8, true, "", ""},
		"another set":         {header("8859/1"), "", false, hl7.UndeclaredCharacterSet, hl7.UndeclaredCharacterSet},
		"explicit null":       {header(`""`), "", false, hl7.UndeclaredCharacterSet, hl7.UndeclaredCharacterSet},
		"escaped declaration": {header(`UNICODE\X20\UTF-8`), "", false, hl7.UndeclaredCharacterSet, hl7.UndeclaredCharacterSet},
		"two repetitions":     {header("UNICODE UTF-8~ASCII"), "", false, hl7.UndeclaredCharacterSet, hl7.UndeclaredCharacterSet},
		"lowercase":           {header("unicode utf-8"), "", false, hl7.UndeclaredCharacterSet, hl7.UndeclaredCharacterSet},
	} {
		t.Run(name, func(t *testing.T) {
			doc := parse(t, tc.declaration+"PID|1||"+accented+"|PLAIN\r")
			if set, readable := doc.CharacterSet(0); set != tc.set || readable != tc.readable {
				t.Fatalf("declared %q %v", set, readable)
			}
			if reading := read(t, doc, "PID-3", hl7.EnforceMSH18); reading.Reason != tc.accented || !bytes.Equal(reading.Decoded, []byte(accented)) {
				t.Fatalf("accented %+v", reading)
			}
			if reading := read(t, doc, "PID-4", hl7.EnforceMSH18); reading.Reason != tc.plain {
				t.Fatalf("plain %+v", reading)
			}
			// Ignoring the declaration reads any valid UTF-8, whatever it says.
			for _, path := range []string{"PID-3", "PID-4"} {
				if _, ok := read(t, doc, path, hl7.IgnoreMSH18).Text(); !ok {
					t.Fatalf("%s is not text when MSH-18 is ignored", path)
				}
			}
		})
	}
	invalid := parse(t, header("UNICODE UTF-8")+"PID|1||\xff\r")
	if reading := read(t, invalid, "PID-3", hl7.EnforceMSH18); reading.Reason != hl7.InvalidUTF8 {
		t.Fatalf("declared UTF-8 holding invalid bytes read %+v", reading)
	}
	if _, readable := invalid.CharacterSet(1); readable {
		t.Fatal("a message the document does not hold declared a character set")
	}
}

func TestMSH1AndMSH2ReadLiterallyUnderEitherPolicy(t *testing.T) {
	for _, tc := range []struct{ raw, separator, encoding string }{
		{"MSH|^~\\&|APP\r", "|", `^~\&`},
		{"MSH|^~\\&#|APP\r", "|", `^~\&#`},
		{"MSH*$%!?*APP\r", "*", "$%!?"},
		{"MSH|^~|APP\r", "|", "^~"},
	} {
		doc := parse(t, tc.raw)
		for _, policy := range []hl7.CharacterSetPolicy{hl7.IgnoreMSH18, hl7.EnforceMSH18} {
			for path, want := range map[string]string{"MSH-1": tc.separator, "MSH-2": tc.encoding} {
				reading := read(t, doc, path, policy)
				if text, ok := reading.Text(); !ok || text != want || !reading.Literal {
					t.Fatalf("%s of %q read %+v", path, tc.raw, reading)
				}
			}
			for _, path := range []string{"MSH-2.1", "MSH-1[2]", "MSH[2]-2"} {
				if reading := read(t, doc, path, policy); reading.State != hl7.Omitted || reading.Literal || reading.Decoded != nil {
					t.Fatalf("%s read %+v", path, reading)
				}
			}
		}
	}
	// Every other position is decoded, including one that holds the same bytes.
	if reading := read(t, parse(t, "MSH|^~\\&|APP\rZPD|a\\E\\b\r"), "ZPD-1", hl7.IgnoreMSH18); reading.Literal || string(reading.Decoded) != `a\b` {
		t.Fatalf("got %+v", reading)
	}
}

func TestEmptyNullAndOmittedValuesAreStatesNotText(t *testing.T) {
	doc := parse(t, header("8859/1")+"PID|1||\"\"||\\ZSECRET\\^\r")
	for path, state := range map[string]hl7.State{
		"PID-3": hl7.Null, "PID-4": hl7.Empty, "PID-9": hl7.Omitted, "PID-5.2": hl7.Empty,
		"PID-5.3": hl7.Omitted, "PID[2]-1": hl7.Omitted, "PID-3[2]": hl7.Omitted, "ZZZ-1": hl7.Omitted,
	} {
		for _, policy := range []hl7.CharacterSetPolicy{hl7.IgnoreMSH18, hl7.EnforceMSH18} {
			reading := read(t, doc, path, policy)
			if reading.State != state || reading.Reason != "" || reading.Decoded != nil || reading.Literal {
				t.Fatalf("%s read %+v", path, reading)
			}
			if _, ok := reading.Text(); ok {
				t.Fatalf("%s read as text", path)
			}
		}
	}
}

func TestReadRefusesAnUnsetPolicyAndPositionsOutsideTheDocument(t *testing.T) {
	doc := parse(t, "MSH|^~\\&|APP\rPID|1||one\r")
	s, _ := hl7.ParseSelector("PID-3")
	if _, err := doc.Read(0, s, 0); err == nil {
		t.Fatal("a read without a character-set policy was accepted")
	}
	if _, err := doc.Read(1, s, hl7.IgnoreMSH18); err == nil {
		t.Fatal("a missing message was read")
	}
	if _, err := doc.Read(0, hl7.Selector{}, hl7.IgnoreMSH18); err == nil {
		t.Fatal("the zero selector was read")
	}
}

func TestReadNodeReadsANavigatedPositionByTheSameRule(t *testing.T) {
	doc := parse(t, "MSH|^~\\&|APP\rPID|1||a\\F\\b~c^\\ZSECRET\\||José\r")
	node := func(path string) hl7.Node {
		t.Helper()
		selected, _, err := doc.Navigate(0, path)
		if err != nil {
			t.Fatal(err)
		}
		return selected
	}
	// A whole field reads with its repetition separators as written.
	whole, err := doc.ReadNode(0, node("PID[1]-3"), hl7.IgnoreMSH18)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := whole.Text(); ok || whole.Reason != hl7.UnsupportedEscape {
		t.Fatalf("whole field read %q %v", text, ok)
	}
	for path, want := range map[string]string{"PID[1]-3[1]": "a|b", "PID[1]-3[2].1": "c", "MSH[1]-2": `^~\&`, "MSH[1]-2[1]": `^~\&`, "MSH[1]-1": "|"} {
		reading, err := doc.ReadNode(0, node(path), hl7.EnforceMSH18)
		if text, ok := reading.Text(); err != nil || !ok || text != want {
			t.Fatalf("%s read %+v %v", path, reading, err)
		}
		selector, _ := hl7.ParseSelector(path)
		if selected, _ := doc.Read(0, selector, hl7.EnforceMSH18); selected.Literal != reading.Literal || !bytes.Equal(selected.Decoded, reading.Decoded) {
			t.Fatalf("%s read differently by selector", path)
		}
	}
	if reading, err := doc.ReadNode(0, node("PID[1]-5"), hl7.EnforceMSH18); err != nil || reading.Reason != hl7.UndeclaredCharacterSet {
		t.Fatalf("undeclared bytes read %+v %v", reading, err)
	}
	if reading, err := doc.ReadNode(0, node("PID[1]-9"), hl7.EnforceMSH18); err != nil || reading.State != hl7.Omitted {
		t.Fatalf("omitted field read %+v %v", reading, err)
	}
	for name, n := range map[string]hl7.Node{
		"message":   node(""),
		"segment":   node("PID[1]"),
		"outside":   {Kind: "field", Segment: "PID", Field: 3, State: hl7.Present, Start: 0, End: 10000},
		"backwards": {Kind: "field", Segment: "PID", Field: 3, State: hl7.Present, Start: 20, End: 10},
	} {
		if _, err := doc.ReadNode(0, n, hl7.IgnoreMSH18); err == nil {
			t.Fatalf("%s node was read as text", name)
		}
	}
	if _, err := doc.ReadNode(0, node("PID[1]-3"), 0); err == nil {
		t.Fatal("a node read without a character-set policy was accepted")
	}
	if _, err := doc.ReadNode(1, node("PID[1]-3"), hl7.IgnoreMSH18); err == nil {
		t.Fatal("a node of a missing message was read")
	}
}
