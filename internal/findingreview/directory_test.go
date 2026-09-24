package findingreview_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
)

// A review is recorded as one new directory holding the record and its
// rendering and nothing else. It is never written over an existing one, and
// never inside either document it was made over: not in the case, and not in
// the diagnosis report directory, however either is reached.
func TestAReviewIsRecordedAsANewDirectoryOutsideBothItsInputs(t *testing.T) {
	casePath, source := openCase(t, frame(booking, acknowledgement))
	report, _ := diagnosed(t, casePath)
	reportDirectory := filepath.Join(t.TempDir(), "diagnosis")
	identity, err := diagnose.WriteReport(reportDirectory, report)
	if err != nil {
		t.Fatal(err)
	}
	record := review(t, report, identity, source, findingreview.Decision{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "keep it"})
	document, err := findingreview.JSON(record)
	if err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "review")
	if err := findingreview.Write(destination, record, casePath, reportDirectory); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"review.json": document, "review.md": findingreview.Markdown(record)} {
		if got, err := os.ReadFile(filepath.Join(destination, name)); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s is not the record's own bytes: %v", name, err)
		}
	}
	if entries, err := os.ReadDir(destination); err != nil || len(entries) != 2 {
		t.Fatalf("a review directory holds something besides its two members: %v %v", entries, err)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("the review directory is not private: %v %v", info.Mode(), err)
		}
	}
	again := findingreview.Write(destination, record, casePath, reportDirectory)
	if again == nil || !strings.Contains(again.Error(), "destination must be new") || strings.Contains(again.Error(), destination) {
		t.Fatalf("an existing review directory was not refused by name alone: %v", again)
	}

	inputs := map[string]string{"the case": casePath, "the diagnosis": reportDirectory}
	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "diagnosis-link")
		if err := os.Symlink(reportDirectory, link); err != nil {
			t.Fatal(err)
		}
		inputs["the diagnosis through a link"] = link
	}
	for name, input := range inputs {
		inside := filepath.Join(input, "review")
		err := findingreview.Write(inside, record, casePath, reportDirectory)
		if err == nil || !strings.Contains(err.Error(), "output must be outside the immutable input case") {
			t.Fatalf("a review inside %s was not refused: %v", name, err)
		}
		if _, statErr := os.Lstat(inside); !os.IsNotExist(statErr) {
			t.Fatalf("a refused review left a directory inside %s", name)
		}
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if err := findingreview.Write(filepath.Join(t.TempDir(), "review"), record, missing, reportDirectory); err == nil || err.Error() != "cannot inspect the reviewed case directory" {
		t.Fatalf("a review of a case that is not there was not refused: %v", err)
	}
	if err := findingreview.Write(filepath.Join(t.TempDir(), "review"), record, casePath, missing); err == nil || err.Error() != "cannot inspect the diagnosis report directory" {
		t.Fatalf("a review of a diagnosis that is not there was not refused: %v", err)
	}
}
