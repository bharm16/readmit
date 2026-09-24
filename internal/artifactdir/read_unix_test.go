//go:build unix

package artifactdir_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// A pipe, socket or device is neither a file nor a directory, and a reader
// that tells it apart from a link says so in its own sentence; one that does
// not reports it as a link.
func TestReadRefusesASpecialFileInTheReadersOwnSentence(t *testing.T) {
	dir := caseFolder(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	errLink, errSpecial := errors.New("link"), errors.New("special")
	layout := artifactdir.Layout{AllowFile: func(string) bool { return true }, MaxFiles: 1, MaxFileBytes: 1, MaxBytes: 1, Refusals: artifactdir.Refusals{Link: errLink, Special: errSpecial}}
	if _, err := artifactdir.Read(dir, layout); err != errSpecial {
		t.Fatalf("a pipe was refused as %v", err)
	}
	layout.Refusals.Special = nil
	if _, err := artifactdir.Read(dir, layout); err != errLink {
		t.Fatalf("a pipe was refused as %v without a sentence of its own", err)
	}
	if err := os.Remove(filepath.Join(dir, "pipe")); err != nil {
		t.Fatal(err)
	}
}
