//go:build !windows

package tests

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// An evaluator who interrupts the walkthrough keeps what was complete and
// nothing that pretends to be. The interrupt reaches the shell and the readmit
// command it is running, as a terminal's Ctrl-C would.
func TestSyntheticWalkthroughInterruptedMidRunClaimsNoCompletion(t *testing.T) {
	parent := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := walkthroughCommand(t, ctx, parent, "../testdata/fixtures")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// The sealed packet is the longest step. `report` writes spec.json before
	// it starts its fixture trials, so once that file exists the command is
	// running with its interrupt handling in place and its seal still ahead.
	workspace := filepath.Join(parent, walkthroughWorkspace)
	packet := filepath.Join(workspace, "packet")
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(packet, "spec.json")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("report step never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	if err == nil || strings.Contains(stdout.String(), "Walkthrough complete") || strings.Contains(stdout.String(), "packet complete") {
		t.Fatalf("an interrupted walkthrough reported completion: %v\n%s", err, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(workspace, "family", "family.json")); err != nil {
		t.Fatalf("work complete before the interrupt was not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packet, "identity.sha256")); !os.IsNotExist(err) {
		t.Fatalf("an interrupted packet carries a completion marker: %v", err)
	}
	if out, _, err := run(t, "report", "verify", packet); err == nil || strings.Contains(out, "Packet verified") {
		t.Fatalf("an interrupted packet verified: %v %s", err, out)
	}
	// Recovery is a new workspace, never a resumed one.
	if stdout, stderr, err := runWalkthrough(t, t.TempDir(), "../testdata/fixtures"); err != nil || !strings.Contains(stdout, "Walkthrough complete") {
		t.Fatalf("a fresh walkthrough after an interrupt did not complete: %v %s", err, stderr)
	}
}
