//go:build linux || darwin

package observation_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observation"
)

func TestReadRejectsFIFOWithoutWaitingForAWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observation.pipe")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := observation.Read(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accepted FIFO as an observation")
		}
	case <-time.After(time.Second):
		// Release an incorrectly blocked open before failing the regression.
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0600)
		if err == nil {
			defer file.Close()
		}
		t.Fatal("observation read blocked on a FIFO without a writer")
	}
}
