package fixturereset

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func confirmedResult(t *testing.T) Result {
	t.Helper()
	directory := planDirectory(t, map[string]string{"observation.json": emptySnapshot})
	return Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(planWith(confirmAction)), PlanDirectory: directory,
		Confirmed: []string{"stop-listener"},
	}, noResolution)
}

func TestWriteOutcomeCreatesOneOwnerOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reset.json")
	if err := ReserveOutcome(path); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := WriteOutcome(path, confirmedResult(t)); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Errorf("retained outcome has mode %v", info.Mode().Perm())
	}
}

// TestWriteOutcomeRefusesAnExistingDestination keeps one reset attempt to one
// retained document. Rerunning after performing the manual step needs a new
// outcome file, so the attempt that stopped is still there to read.
func TestWriteOutcomeRefusesAnExistingDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reset.json")
	result := confirmedResult(t)
	if err := WriteOutcome(path, result); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := WriteOutcome(path, result); err == nil {
		t.Fatal("a second reset overwrote the first one's retained outcome")
	}
}

// TestWriteOutcomeRefusesRetainedEvidence routes the one writer this package
// owns through the single path policy: a reset outcome never lands inside a
// retained case, run or result directory.
func TestWriteOutcomeRefusesRetainedEvidence(t *testing.T) {
	evidence := t.TempDir()
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(evidence, "reset.json")
	if err := ReserveOutcome(path); err == nil {
		t.Error("reserved a reset outcome inside retained evidence")
	}
	if err := WriteOutcome(path, confirmedResult(t)); err == nil {
		t.Error("wrote a reset outcome inside retained evidence")
	}
}
