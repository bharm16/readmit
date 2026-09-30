package textmerge_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/textmerge"
)

func merged(t *testing.T, base, yours, current string) []textmerge.Hunk {
	t.Helper()
	hunks, ok := textmerge.Merge(textmerge.Lines([]byte(base)), textmerge.Lines([]byte(yours)), textmerge.Lines([]byte(current)))
	if !ok {
		t.Fatal("texts this short were not compared")
	}
	return hunks
}

func agreed(hunks []textmerge.Hunk) (string, int) {
	text, conflicts := "", 0
	for _, hunk := range hunks {
		if hunk.Conflict {
			conflicts++
		}
		text += strings.Join(hunk.Lines, "")
	}
	return text, conflicts
}

// Changes to different parts combine; the same change is taken once; changes
// that differ in one part are a conflict carrying all three versions.
func TestAMergeCombinesSeparateChangesAndLeavesDifferingOnesToAPerson(t *testing.T) {
	if text, conflicts := agreed(merged(t, "a\nb\nc\n", "A\nb\nc\n", "a\nb\nC\n")); text != "A\nb\nC\n" || conflicts != 0 {
		t.Fatalf("separate changes: %q %d", text, conflicts)
	}
	if text, conflicts := agreed(merged(t, "a\nb\n", "a\nB\n", "a\nB\n")); text != "a\nB\n" || conflicts != 0 {
		t.Fatalf("the same change: %q %d", text, conflicts)
	}
	hunks := merged(t, "a\nb\nc\n", "a\nB1\nc\n", "a\nB2\nc\n")
	if len(hunks) != 3 || !hunks[1].Conflict || hunks[1].Base[0] != "b\n" || hunks[1].Yours[0] != "B1\n" || hunks[1].Current[0] != "B2\n" {
		t.Fatalf("a conflict: %+v", hunks)
	}
	if textmerge.Lines([]byte{0xff, 0xfe}) != nil || textmerge.Lines([]byte("a\x00b")) != nil {
		t.Fatal("bytes that are not text were split into lines")
	}
}
