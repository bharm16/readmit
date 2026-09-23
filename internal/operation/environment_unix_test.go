//go:build !windows

package operation_test

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
)

// Opening a target for editing peeks at the contract the file declares before
// the target reader reads it. The peek is held to the same rule as that
// reader: a FIFO, a link to one and a file past the target bound are refused
// promptly, never opened and read to their end.
func TestEditingATargetRefusesAFIFOALinkAndAnOversizedFileWithoutReadingThem(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(dir, "large.json")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, path := range []string{fifo, link, large} {
		for name, call := range map[string]func(string) error{
			"open": func(path string) error { _, err := operation.OpenOrNewTarget(path); return err },
			"save": func(path string) error {
				_, err := operation.SaveTarget(path, replay.Target{Name: "lab"})
				return err
			},
		} {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			allocated := memory.TotalAlloc
			answered := make(chan error, 1)
			go func() { answered <- call(path) }()
			select {
			case err := <-answered:
				if err == nil {
					t.Errorf("%s accepted %s", name, filepath.Base(path))
				}
				runtime.ReadMemStats(&memory)
				if grown := memory.TotalAlloc - allocated; grown > 64<<20 {
					t.Errorf("%s allocated %d MiB reading %s", name, grown>>20, filepath.Base(path))
				}
			case <-time.After(10 * time.Second):
				if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
					writer.Close()
				}
				t.Errorf("%s blocked on %s instead of refusing it", name, filepath.Base(path))
				<-answered
			}
		}
	}
}
