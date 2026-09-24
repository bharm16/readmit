package hl7_test

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7"
)

// Every message below is synthetic; no value refers to anything real.
const siu = "MSH|^~\\&|APP|SITE|RECEIVER|LAB|20260101120000||SIU^S12|MSG-001|P|2.5.1\r" +
	"PID|1||MRN-001^^^AUTH||NAME^GIVEN\r" +
	"NTE|1||remark\r" +
	"OBX|1|ST|||value\r"

func replace(t *testing.T, path, value string) hl7.Edit {
	t.Helper()
	s, err := hl7.ParseSelector(path)
	if err != nil {
		t.Fatal(err)
	}
	return hl7.Edit{Selector: s, Value: []byte(value)}
}

func remove(t *testing.T, segment string, occurrence int) hl7.Edit {
	t.Helper()
	s, err := hl7.NewSelector(hl7.Parts{Segment: segment, Occurrence: occurrence, Field: 1, Repetition: 1})
	if err != nil {
		t.Fatal(err)
	}
	return hl7.Edit{Selector: s, RemoveSegment: true}
}

func rewrite(t *testing.T, raw string, edits ...hl7.Edit) (hl7.Rewritten, error) {
	t.Helper()
	return parse(t, raw).Rewrite(0, edits, hl7.StandardDelimiters)
}

// A rewrite writes new bytes beside the source, reports where each edit
// landed in byte order whatever order the edits were given in, and reads the
// result back.
func TestRewriteSplicesEditsAndReportsWhereEachLanded(t *testing.T) {
	doc := parse(t, siu)
	edits := []hl7.Edit{replace(t, "PID-5", ""), replace(t, "MSH-10", "READMIT000001"), replace(t, "PID-3.1", "SURROGATE")}
	result, err := doc.Rewrite(0, edits, hl7.StandardDelimiters)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer("MSG-001", "READMIT000001", "MRN-001", "SURROGATE", "NAME^GIVEN", "").Replace(siu)
	if string(result.Bytes) != want || !bytes.Equal(doc.Serialize(), []byte(siu)) {
		t.Fatalf("got %q; the source must stay %q", result.Bytes, siu)
	}
	if !bytes.Equal(result.Document.Serialize(), result.Bytes) {
		t.Fatal("the read-back document is not the rewritten bytes")
	}
	order := []int{1, 2, 0}
	if len(result.Placed) != len(edits) {
		t.Fatalf("placed %+v", result.Placed)
	}
	for i, placed := range result.Placed {
		if placed.Edit != order[i] || placed.State != hl7.Present ||
			string(result.Bytes[placed.Result.Start:placed.Result.End]) != string(edits[placed.Edit].Value) ||
			string(doc.Bytes(placed.Original)) != map[int]string{0: "NAME^GIVEN", 1: "MSG-001", 2: "MRN-001"}[placed.Edit] {
			t.Fatalf("placement %d: %+v", i, placed)
		}
	}
	// A position's state before the edit is reported, and clearing an explicit
	// null leaves an empty position rather than the null.
	result, err = rewrite(t, "MSH|^~\\&|APP\rPID|1|\"\"||\r", replace(t, "PID-2", ""), replace(t, "PID-3", "NEW"))
	if err != nil || string(result.Bytes) != "MSH|^~\\&|APP\rPID|1||NEW|\r" ||
		result.Placed[0].State != hl7.Null || result.Placed[1].State != hl7.Empty || result.Placed[1].Result != (hl7.Span{Start: 20, End: 23}) {
		t.Fatalf("%q %+v %v", result.Bytes, result.Placed, err)
	}
	// Framing is preserved and the result is read back the way the source was.
	framed := "\x0b" + siu + "\x1c\r"
	result, err = rewrite(t, framed, replace(t, "MSH-10", "READMIT000001"))
	if err != nil || result.Document.Format != hl7.MLLP || string(result.Bytes) != strings.Replace(framed, "MSG-001", "READMIT000001", 1) {
		t.Fatalf("%q %v", result.Bytes, err)
	}
	// No edit is a rewrite too: the same bytes, read back.
	result, err = rewrite(t, siu)
	if err != nil || string(result.Bytes) != siu || len(result.Placed) != 0 {
		t.Fatalf("%q %+v %v", result.Bytes, result.Placed, err)
	}
}

// A removed segment goes with its terminator; framing stays, and two adjacent
// removed segments only touch.
func TestRewriteRemovesASegmentWithItsTerminator(t *testing.T) {
	result, err := rewrite(t, siu, remove(t, "NTE", 1), remove(t, "OBX", 1))
	if err != nil || string(result.Bytes) != strings.Split(siu, "NTE")[0] {
		t.Fatalf("%q %v", result.Bytes, err)
	}
	if placed := result.Placed[0]; placed.State != hl7.Present || placed.Result.Start != placed.Result.End {
		t.Fatalf("%+v", placed)
	}
	framed := "\x0b" + siu + "\x1c\r"
	result, err = rewrite(t, framed, remove(t, "OBX", 1))
	if err != nil || string(result.Bytes) != strings.Split(framed, "OBX")[0]+"\x1c\r" {
		t.Fatalf("%q %v", result.Bytes, err)
	}
	if _, err := rewrite(t, siu, hl7.Edit{Selector: remove(t, "NTE", 1).Selector, Value: []byte("x"), RemoveSegment: true}); err == nil {
		t.Fatal("a segment removal wrote a value")
	}
}

// One position, named twice. A field with one component, and every position
// below an empty or explicit-null ancestor, resolve to the ancestor's own
// span, so different selectors can name one place to write.
func TestRewriteRefusesTheSamePositionTwice(t *testing.T) {
	for name, test := range map[string]struct {
		raw   string
		paths []string
	}{
		"one selector spelled twice":         {siu, []string{"PID-3.1", "PID[1]-3[1].1"}},
		"a field with one component":         {"MSH|^~\\&|APP\rPID|1||MRN\r", []string{"PID-3", "PID-3.1"}},
		"an empty field and its component":   {"MSH|^~\\&|APP\rPID|1||\r", []string{"PID-3", "PID-3.1"}},
		"an empty field's two components":    {"MSH|^~\\&|APP\rPID|1||\r", []string{"PID-3.4.1", "PID-3.1"}},
		"an explicit null and its component": {"MSH|^~\\&|APP\rPID|1||\"\"\r", []string{"PID-3.1", "PID-3.2"}},
	} {
		for _, reversed := range []bool{false, true} {
			first, second := replace(t, test.paths[0], "A"), replace(t, test.paths[1], "B")
			if reversed {
				first, second = second, first
			}
			if _, err := rewrite(t, test.raw, first, second); !errors.Is(err, hl7.ErrOverlappingEdits) {
				t.Errorf("%s (reversed %v): %v", name, reversed, err)
			}
		}
	}
}

// Two edits meeting anywhere are refused: overlapping bytes, and an empty
// position at the start, inside or at the end of the bytes another edit
// replaces, in either order. Neighbouring positions are independent.
func TestRewriteRefusesOverlappingEdits(t *testing.T) {
	for name, test := range map[string]struct {
		raw   string
		edits func() []hl7.Edit
	}{
		"a field and its component": {siu, func() []hl7.Edit { return []hl7.Edit{replace(t, "PID-3", "A"), replace(t, "PID-3.4", "B")} }},
		"an empty first component":  {"MSH|^~\\&|APP\rPID|1||^X\r", func() []hl7.Edit { return []hl7.Edit{replace(t, "PID-3", "A"), replace(t, "PID-3.1", "B")} }},
		"an empty inner component":  {"MSH|^~\\&|APP\rPID|1||X^^Y\r", func() []hl7.Edit { return []hl7.Edit{replace(t, "PID-3", "A"), replace(t, "PID-3.2", "B")} }},
		"an empty last component":   {"MSH|^~\\&|APP\rPID|1||X^\r", func() []hl7.Edit { return []hl7.Edit{replace(t, "PID-3", "A"), replace(t, "PID-3.2", "B")} }},
		"a field of a removed segment": {siu, func() []hl7.Edit {
			return []hl7.Edit{remove(t, "NTE", 1), replace(t, "NTE-3", "B")}
		}},
		"one segment removed twice": {siu, func() []hl7.Edit { return []hl7.Edit{remove(t, "NTE", 1), remove(t, "NTE", 1)} }},
	} {
		for _, reversed := range []bool{false, true} {
			edits := test.edits()
			if reversed {
				edits[0], edits[1] = edits[1], edits[0]
			}
			if _, err := rewrite(t, test.raw, edits...); !errors.Is(err, hl7.ErrOverlappingEdits) {
				t.Errorf("%s (reversed %v): %v", name, reversed, err)
			}
		}
	}
	for name, edits := range map[string][]hl7.Edit{
		"two components":            {replace(t, "PID-3.1", "A"), replace(t, "PID-3.4", "B")},
		"two fields":                {replace(t, "PID-3", "A"), replace(t, "PID-5", "B")},
		"adjacent removed segments": {remove(t, "NTE", 1), remove(t, "OBX", 1)},
		"a field beside a removal":  {replace(t, "PID-5", ""), remove(t, "NTE", 1)},
	} {
		if _, err := rewrite(t, siu, edits...); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Two empty neighbours are two positions.
	if _, err := rewrite(t, "MSH|^~\\&|APP\rPID|1||^\r", replace(t, "PID-3.1", "A"), replace(t, "PID-3.2", "B")); err != nil {
		t.Fatal(err)
	}
}

// MSH-1 and MSH-2 declare the delimiters every other position is split on, so
// no edit restates them, not even with their own bytes, and MSH is never
// removed. Any part of them is an omitted position.
func TestRewriteNeverRewritesTheDelimiterDeclarations(t *testing.T) {
	for name, edit := range map[string]hl7.Edit{
		"MSH-1":             replace(t, "MSH-1", "|"),
		"MSH-2":             replace(t, "MSH-2", "^~\\&"),
		"MSH-2, rewritten":  replace(t, "MSH-2", "^~\\#"),
		"removing MSH":      remove(t, "MSH", 1),
		"MSH-1, under read": replace(t, "MSH[1]-1[1]", "*"),
	} {
		if _, err := rewrite(t, siu, edit); !errors.Is(err, hl7.ErrDelimiterDeclaration) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := rewrite(t, siu, replace(t, "MSH-2.1", "#")); !errors.Is(err, hl7.ErrOmittedPosition) {
		t.Fatalf("a part of MSH-2: %v", err)
	}
}

// A position the message does not declare has no bytes to replace, and a
// segment it does not hold has none to remove.
func TestRewriteRefusesAnOmittedPosition(t *testing.T) {
	for name, edit := range map[string]hl7.Edit{
		"a field past the last":           replace(t, "PID-99", "A"),
		"a component past the last":       replace(t, "PID-3.9", "A"),
		"a repetition not declared":       replace(t, "PID-3[2]", "A"),
		"a segment not held":              replace(t, "ZPD-1", "A"),
		"an occurrence not held":          replace(t, "PID[2]-1", "A"),
		"removing a segment not held":     remove(t, "ZPD", 1),
		"removing an occurrence not held": remove(t, "NTE", 2),
	} {
		if _, err := rewrite(t, siu, edit); !errors.Is(err, hl7.ErrOmittedPosition) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Under StandardDelimiters a message declaring anything but |^~\& is refused
// before any edit is read: custom separators, two or three encoding
// characters, or a truncation character. Under DeclaredDelimiters the same
// message is rewritten by its own declaration.
func TestRewriteUnderStandardDelimitersRefusesOtherDeclarations(t *testing.T) {
	for _, name := range []string{"custom-delimiters.hl7", "reduced-delimiters.hl7"} {
		raw, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			t.Fatal(err)
		}
		doc := parse(t, string(raw))
		edit := []hl7.Edit{replace(t, "MSH-10", "READMIT000001")}
		if _, err := doc.Rewrite(0, edit, hl7.StandardDelimiters); !errors.Is(err, hl7.ErrUnstandardDelimiters) {
			t.Fatalf("%s: %v", name, err)
		}
		result, err := doc.Rewrite(0, edit, hl7.DeclaredDelimiters)
		if err != nil || !bytes.Contains(result.Bytes, []byte("READMIT000001")) {
			t.Fatalf("%s: %q %v", name, result.Bytes, err)
		}
	}
	for _, raw := range []string{"MSH|^~\\&#|APP\rPID|1\r", "MSH|^~\\|APP\rPID|1\r"} {
		if _, err := rewrite(t, raw, replace(t, "PID-1", "2")); !errors.Is(err, hl7.ErrUnstandardDelimiters) {
			t.Fatalf("%q: %v", raw, err)
		}
	}
	if !parse(t, siu).Messages[0].Delimiters.Standard() {
		t.Fatal("the standard declaration was not recognized")
	}
}

// The result is held to the 16 MiB any input is held to.
func TestRewriteRefusesAResultPastTheInputLimit(t *testing.T) {
	value := strings.Repeat("A", hl7.MaxInputBytes-len(siu)+len("MRN-001"))
	if _, err := rewrite(t, siu, replace(t, "PID-3.1", value)); err != nil {
		t.Fatalf("a result at the limit: %v", err)
	}
	if _, err := rewrite(t, siu, replace(t, "PID-3.1", value+"A")); !errors.Is(err, hl7.ErrRewriteTooLarge) {
		t.Fatalf("a result past the limit: %v", err)
	}
}

// What cannot be read back the way the source was read is refused rather than
// returned: a value that ends its segment, a control byte, a terminator the
// source did not use, or a removal that leaves no message.
func TestRewriteRefusesAResultThatDoesNotReadBack(t *testing.T) {
	for name, edit := range map[string]hl7.Edit{
		"a segment terminator": replace(t, "PID-5", "A\rB"),
		"a control byte":       replace(t, "PID-5", "A\x01B"),
		"a line feed":          replace(t, "PID-5", "A\nB"),
		"an open escape":       replace(t, "PID-5", "A\\B"),
	} {
		if _, err := rewrite(t, siu, edit); !errors.Is(err, hl7.ErrUnreadableRewrite) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The source's declared terminator binds the read-back too.
	doc, err := hl7.Parse([]byte(strings.ReplaceAll(siu, "\r", "\n")), hl7.Options{Terminator: hl7.LF})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Rewrite(0, []hl7.Edit{replace(t, "PID-5", "A\r")}, hl7.StandardDelimiters); !errors.Is(err, hl7.ErrUnreadableRewrite) {
		t.Fatalf("a mixed terminator: %v", err)
	}
}

func TestRewriteRefusesAnUnsetPolicyAndAMessageOutsideTheDocument(t *testing.T) {
	doc := parse(t, siu)
	if _, err := doc.Rewrite(0, nil, 0); err == nil {
		t.Fatal("an unset policy was accepted")
	}
	for _, index := range []int{-1, 1} {
		if _, err := doc.Rewrite(index, nil, hl7.DeclaredDelimiters); err == nil {
			t.Fatalf("message %d was rewritten", index)
		}
	}
	if _, err := doc.Rewrite(0, []hl7.Edit{{Value: []byte("A")}}, hl7.StandardDelimiters); err == nil {
		t.Fatal("the zero selector was rewritten")
	}
	if _, err := doc.Rewrite(0, []hl7.Edit{{RemoveSegment: true}}, hl7.StandardDelimiters); err == nil {
		t.Fatal("the zero selector's segment was removed")
	}
}

// FuzzRewrite applies two arbitrary edits to an arbitrary message. Whatever it
// accepts reads back, is bounded, leaves the source unchanged, holds every
// edit's bytes where it says they landed and every other byte where the
// source had it; whatever it refuses, it refuses by name.
func FuzzRewrite(f *testing.F) {
	for _, name := range []string{"adt-cr.hl7", "siu-lf.hl7", "custom-delimiters.hl7", "two-messages.mllp", "reduced-delimiters.hl7"} {
		raw, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw, "PID-3.1", []byte("A"), "PID-5", []byte(""), false)
	}
	f.Add([]byte(siu), "PID-3", []byte("A"), "PID-3.1", []byte("B"), false)
	f.Add([]byte("MSH|^~\\&|APP\rPID|1||\r"), "PID-3", []byte("A"), "PID-3.1", []byte("B"), false)
	f.Add([]byte(siu), "NTE-3", []byte("A"), "NTE-1", []byte{}, true)
	f.Add([]byte(siu), "MSH-2", []byte("#"), "PID-5", []byte("A\rB"), false)
	f.Fuzz(func(t *testing.T, raw []byte, first string, firstValue []byte, second string, secondValue []byte, removal bool) {
		doc, err := hl7.Parse(raw, hl7.Options{})
		if err != nil {
			return
		}
		a, errA := hl7.ParseSelector(first)
		b, errB := hl7.ParseSelector(second)
		if errA != nil || errB != nil {
			return
		}
		edits := []hl7.Edit{{Selector: a, Value: firstValue}, {Selector: b, Value: secondValue, RemoveSegment: removal}}
		if removal {
			edits[1].Value = nil
		}
		for _, policy := range []hl7.DelimiterPolicy{hl7.StandardDelimiters, hl7.DeclaredDelimiters} {
			result, err := doc.Rewrite(0, edits, policy)
			if !bytes.Equal(doc.Serialize(), raw) {
				t.Fatal("the source changed")
			}
			if err != nil {
				named := []error{hl7.ErrUnstandardDelimiters, hl7.ErrDelimiterDeclaration, hl7.ErrOmittedPosition, hl7.ErrOverlappingEdits, hl7.ErrRewriteTooLarge, hl7.ErrUnreadableRewrite}
				if !slices.ContainsFunc(named, func(refusal error) bool { return errors.Is(err, refusal) }) {
					t.Fatalf("an unnamed refusal: %v", err)
				}
				continue
			}
			if len(result.Bytes) > hl7.MaxInputBytes || !bytes.Equal(result.Document.Serialize(), result.Bytes) || len(result.Placed) != len(edits) {
				t.Fatal("an accepted rewrite is unbounded or does not read back")
			}
			source, position, at := raw, 0, 0
			for i, placed := range result.Placed {
				if i > 0 && placed.Original.Start < result.Placed[i-1].Original.End {
					t.Fatal("accepted edits overlap")
				}
				gap := source[position:placed.Original.Start]
				if !bytes.Equal(result.Bytes[at:at+len(gap)], gap) || at+len(gap) != placed.Result.Start {
					t.Fatal("a byte outside every edit moved or changed")
				}
				if !bytes.Equal(result.Bytes[placed.Result.Start:placed.Result.End], edits[placed.Edit].Value) {
					t.Fatal("an edit's bytes are not where it landed")
				}
				position, at = placed.Original.End, placed.Result.End
			}
			if !bytes.Equal(result.Bytes[at:], source[position:]) {
				t.Fatal("the tail of the source changed")
			}
		}
	})
}
