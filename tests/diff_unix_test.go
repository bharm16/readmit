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

func TestDiffRejectsFIFOManifestBeforeOpening(t *testing.T) {
	pipe := filepath.Join(t.TempDir(), "SECRET-FIFO")
	if err := syscall.Mkfifo(pipe, 0600); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []bool{false, true} {
		dir := t.TempDir()
		manifest := filepath.Join(dir, "manifest.json")
		if alias {
			if err := os.Symlink(pipe, manifest); err != nil {
				t.Fatal(err)
			}
		} else if err := syscall.Mkfifo(manifest, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := testCommand(ctx, t, "diff", dir, "../testdata/fixtures/listen-s12.hl7")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		blocked := ctx.Err() != nil
		cancel()
		if blocked {
			t.Fatal("diff blocked opening a FIFO manifest")
		}
		if err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), "cannot read diff artifact manifest") || strings.Contains(stderr.String(), "SECRET") || strings.Contains(stderr.String(), dir) {
			t.Fatalf("FIFO was accepted or diagnostics were not private: %v %q %q", err, stdout.String(), stderr.String())
		}
	}
}
