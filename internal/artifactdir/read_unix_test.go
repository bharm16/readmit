//go:build unix

package artifactdir_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// A pipe at a document's name is refused from what the name holds, before an
// open could block on it.
func TestDocumentReadRefusesAPipeBeforeOpeningIt(t *testing.T) {
	folder := t.TempDir()
	pipe := filepath.Join(folder, "doc.json")
	if err := syscall.Mkfifo(pipe, 0600); err != nil {
		t.Fatal(err)
	}
	document := exampleDocument()
	for _, links := range []artifactdir.Links{artifactdir.RefuseLinks, artifactdir.FollowLinks} {
		document.Links = links
		if _, err := document.Read(pipe); err != errDocumentIrregular {
			t.Fatalf("a pipe was reported as %v", err)
		}
	}
}

// A regular file this account may not open is refused in the document's own
// sentence, with the filesystem's refusal behind it.
func TestDocumentReadRefusesAFileItCannotOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("an administrator opens every file")
	}
	_, path := documentFolder(t, "doc.json", []byte("closed\n"))
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := exampleDocument().Read(path); !errors.Is(err, errDocumentOpen) || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("an unopenable document was reported as %v", err)
	}
}

// A previous copy the folder will not take refuses the replacement, which
// leaves the document as it was.
func TestReplaceRefusesAPreviousCopyItCannotCreate(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("an administrator writes into every folder")
	}
	folder, path := documentFolder(t, "doc.json", []byte("old\n"))
	if err := os.Chmod(folder, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(folder, 0700) })
	keeping := exampleDocument()
	keeping.Previous = &artifactdir.Previous{Irregular: errPreviousIrregular, Damaged: errPreviousDamaged, Create: errPreviousCreate, Write: errPreviousWrite}
	if err := keeping.Replace(path, []byte("new\n")); !errors.Is(err, errPreviousCreate) || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("an uncreatable copy was reported as %v", err)
	}
	if string(readBack(t, path)) != "old\n" || !slices.Equal(names(t, folder), []string{"doc.json"}) {
		t.Fatalf("a refused replacement changed the folder: %v", names(t, folder))
	}
}

// An owner-only document refuses a file anyone but its owner may read, and
// reads one only its owner may.
func TestDocumentReadRefusesAFileOthersMayReadWhenOwnerOnly(t *testing.T) {
	_, path := documentFolder(t, "policy.json", []byte("private\n"))
	document := exampleDocument()
	document.OwnerOnly = true
	if data, err := document.Read(path); err != nil || string(data) != "private\n" {
		t.Fatalf("an owner-only file was read as %q, %v", data, err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := document.Read(path); err != errDocumentIrregular {
		t.Fatalf("a file its group may read was reported as %v", err)
	}
}
