package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A suite or specification entry is read within its bound: one at the bound
// is read whole, and one a byte past it, a link or a folder is refused.
func TestReadBoundedEntryHoldsAnEntryToItsBound(t *testing.T) {
	dir := t.TempDir()
	at, past := filepath.Join(dir, "at.json"), filepath.Join(dir, "past.json")
	if err := os.WriteFile(at, bytes.Repeat([]byte("x"), 16), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(past, bytes.Repeat([]byte("x"), 17), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := readBoundedEntry(at, 16); err != nil || len(data) != 16 {
		t.Fatalf("an entry at its bound was read as %d bytes, %v", len(data), err)
	}
	if data, err := readBoundedEntry(past, 16); err == nil {
		t.Fatalf("an entry past its bound was read as %d bytes", len(data))
	}
	if _, err := readBoundedEntry(dir, 16); err == nil {
		t.Fatal("a folder was read as an entry")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(at, link); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	if _, err := readBoundedEntry(link, 16); err == nil {
		t.Fatal("a link was read as an entry")
	}
}
