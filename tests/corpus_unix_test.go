//go:build !windows

package tests

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCorpusScanAcknowledgesAnInterruptWithoutWritingABenchmark drives the
// signal path the command actually installs. Windows has no interrupt to
// deliver to a child process, so the portable half of this contract is the
// context cancellation covered in internal/importer.
func TestCorpusScanAcknowledgesAnInterruptWithoutWritingABenchmark(t *testing.T) {
	// A corpus large enough that the scan is still running well after the
	// interrupt is sent, and small enough for the command budget.
	stream, _ := generateCorpus(t, "interrupted.mllp", "150000", framedCorpus...)
	report := filepath.Join(t.TempDir(), "benchmark.json")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := append([]string{"corpus", "scan", stream, "--report", report}, framedCorpus...)
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	err := cmd.Wait()
	if ctx.Err() != nil {
		t.Fatal("the interrupted scan never exited")
	}
	if err == nil {
		t.Fatalf("an interrupted scan exited zero:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "scan cancelled; no benchmark was written") {
		t.Fatalf("diagnostic %q does not acknowledge the cancellation", stderr.String())
	}
	// The cancellation is observable: the scan reports the state it stopped in
	// and the counts it had reached, rather than reporting nothing or reporting
	// a complete answer.
	if summaryField(t, stdout.String(), "State") != "cancelled" {
		t.Fatalf("state %q, want cancelled", summaryField(t, stdout.String(), "State"))
	}
	records := summaryField(t, stdout.String(), "Records")
	if records == "0" || records == "150000" {
		t.Errorf("a cancelled scan reported %s of 150000 records", records)
	}
	// It counted part of a stream, so it knows nothing about the rest of it.
	// Reporting those counts as within the case bundle bounds would report the
	// unread remainder as a pass.
	if bounds := summaryField(t, stdout.String(), "Case bounds"); !strings.Contains(bounds, "not evaluated") {
		t.Errorf("case bounds %q, want a cancelled scan to evaluate none", bounds)
	}
	// Nothing half written is left behind: a partial scan measured part of a
	// stream, and a benchmark of part of a stream is a number nothing stands
	// behind.
	if _, err := os.Lstat(report); !os.IsNotExist(err) {
		t.Error("a cancelled scan left a benchmark behind")
	}
}
