//go:build !windows

package replay_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/replay"
)

func TestConfigurationAndCARejectFIFOWithoutOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := replay.ReadTarget(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("target FIFO blocked")
	}
	config := target("127.0.0.1:2575")
	config.Transport = "tls"
	config.CAFile = path
	source := caseAt(t, request("FIFO"))
	go func() { _, err := replay.Prepare(source, config, replay.Options{}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("CA FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("CA FIFO blocked")
	}
	// A symlink to a FIFO must receive the same preliminary file-type check.
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	go func() { _, err := replay.ReadTarget(alias); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO alias accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO alias blocked")
	}
}
