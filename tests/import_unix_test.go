//go:build !windows

package tests

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
)

// Writing the case is the longest step of an import, one synced payload file
// per occurrence, so an interrupt that arrives while it runs stops it there
// rather than after the last payload: the command exits non-zero, the case has
// no completion marker and is short of payloads, no receipt is written, every
// reader refuses it, and a fresh destination imports the same bytes whole.
func TestInterruptingAnImportWhileItWritesStopsBeforeTheLastPayload(t *testing.T) {
	const occurrences = 600
	dir := t.TempDir()
	frame := "\x0b" + importFixture("INTERRUPT") + "\x1c\r"
	source := filepath.Join(dir, "interface.mllp")
	if err := os.WriteFile(source, []byte(strings.Repeat(frame, occurrences)), 0600); err != nil {
		t.Fatal(err)
	}
	declared := []string{"--file", source, "--framing", "mllp", "--terminator", "cr", "--encoding", "us-ascii", "--direction", "inbound"}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	partial := ""
	for attempt := 0; attempt < 3 && partial == ""; attempt++ {
		output := filepath.Join(dir, fmt.Sprintf("interrupted-%d", attempt))
		receipt := output + "-receipt.json"
		command := testCommand(ctx, t, append([]string{"import", "--output", output, "--receipt", receipt}, declared...)...)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		var waitErr error
		go func() { waitErr = command.Wait(); close(done) }()
		t.Cleanup(func() { _ = command.Process.Kill(); <-done })
		tick := time.NewTicker(100 * time.Microsecond)
	waitForPayloads:
		for {
			if written, _ := os.ReadDir(filepath.Join(output, "payloads")); len(written) > 0 {
				break
			}
			select {
			case <-done:
				break waitForPayloads
			case <-ctx.Done():
				tick.Stop()
				t.Fatal("the import never began writing its case")
			case <-tick.C:
			}
		}
		tick.Stop()
		signalErr := command.Process.Signal(syscall.SIGINT)
		<-done
		// An import that finished before the interrupt reached it is verified
		// and another destination tried; it is never called partial.
		if _, err := os.Stat(filepath.Join(output, "identity.sha256")); err == nil {
			if _, err := bundle.Open(output); err != nil {
				t.Fatalf("an import that completed does not verify: %v", err)
			}
			continue
		}
		if signalErr != nil {
			t.Fatal(signalErr)
		}
		if waitErr == nil {
			t.Fatalf("an interrupted import exited successfully: %s", stdout.String())
		}
		payloads, err := os.ReadDir(filepath.Join(output, "payloads"))
		if err != nil || len(payloads) >= occurrences {
			t.Fatalf("the interrupt did not stop the case write: %d of %d payloads, %v", len(payloads), occurrences, err)
		}
		if _, err := os.Stat(receipt); !os.IsNotExist(err) {
			t.Fatalf("an interrupted import wrote a receipt: %v", err)
		}
		partial = output
	}
	if partial == "" {
		t.Fatal("three imports completed before the interrupt reached them; no interrupted-write coverage obtained")
	}
	if _, err := bundle.Open(partial); err == nil {
		t.Fatal("an interrupted case verified")
	}
	if _, _, err := run(t, "timeline", partial); err == nil {
		t.Fatal("the CLI accepted an interrupted case")
	}
	fresh := filepath.Join(dir, "fresh")
	if _, stderr, err := run(t, append([]string{"import", "--output", fresh, "--receipt", fresh + "-receipt.json"}, declared...)...); err != nil {
		t.Fatalf("a fresh import did not complete: %v %s", err, stderr)
	}
	if b, err := bundle.Open(fresh); err != nil || len(b.Events) != occurrences {
		t.Fatalf("a fresh import is not the whole source: %v", err)
	}
}
