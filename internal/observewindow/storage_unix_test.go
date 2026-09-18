//go:build linux || darwin

package observewindow_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observewindow"
)

// A window or completion that came from a pipe is not a document an operator
// selected, and refusing it must not wait for a writer that may never arrive.
func TestObservationReadersRejectFIFOWithoutWaitingForAWriter(t *testing.T) {
	for name, read := range map[string]func(string) error{
		"window":     func(path string) error { _, err := observewindow.ReadWindow(path); return err },
		"completion": func(path string) error { _, err := observewindow.ReadCompletion(path); return err },
	} {
		path := filepath.Join(t.TempDir(), name+".pipe")
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- read(path) }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("accepted a FIFO as an observation %s", name)
			}
		case <-time.After(time.Second):
			// Release an incorrectly blocked open before failing.
			if file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0600); err == nil {
				file.Close()
			}
			t.Fatalf("reading an observation %s blocked on a FIFO without a writer", name)
		}
	}
}

// A destination that resolves into retained evidence through a link is the
// same destination, and is refused the same way.
func TestCompletionIsNotWrittenThroughALinkIntoEvidence(t *testing.T) {
	directory := t.TempDir()
	record, err := observewindow.DecodeCompletion([]byte(authoredCompletion(t)))
	if err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(directory, "run")
	if err := os.Mkdir(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte("evidence\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(directory, "alias")
	if err := os.Symlink(evidence, alias); err != nil {
		t.Fatal(err)
	}
	if err := observewindow.WriteCompletion(filepath.Join(alias, "completion.json"), record); err == nil {
		t.Fatal("a completion was written into retained evidence through a link")
	}
	// A symlink already occupying the destination is not a new destination.
	occupied := filepath.Join(directory, "completion.json")
	if err := os.Symlink(filepath.Join(directory, "elsewhere.json"), occupied); err != nil {
		t.Fatal(err)
	}
	if err := observewindow.WriteCompletion(occupied, record); err == nil {
		t.Fatal("a completion was written through a dangling symlink")
	}
	if _, err := os.Lstat(filepath.Join(directory, "elsewhere.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused write created a file through a link: %v", err)
	}
}
