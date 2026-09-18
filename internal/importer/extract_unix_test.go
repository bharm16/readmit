//go:build !windows

package importer_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/importer"
)

// A folder import reads the folder the person named and nothing a link inside
// it points at, so evidence outside the declared container can never be drawn
// into a case by a name that looks like a member.
func TestFolderMembersReachedThroughALinkAreRefused(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "secret.hl7", message("OUT"))
	folder := t.TempDir()
	write(t, folder, "real.hl7", message("ONE"))
	if err := os.Symlink(filepath.Join(outside, "secret.hl7"), filepath.Join(folder, "link.hl7")); err != nil {
		t.Skipf("this filesystem does not support symbolic links: %v", err)
	}
	_, err := importer.Extract(t.Context(), plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), nil, []string{folder}, nil)
	if !errors.Is(err, importer.ErrUnsafeEntry) {
		t.Fatalf("a linked folder member was not refused: %v", err)
	}
}

func TestDeclaredContainersReachedThroughALinkedRootAreRefused(t *testing.T) {
	folder := t.TempDir()
	write(t, folder, "real.hl7", message("ONE"))
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(folder, alias); err != nil {
		t.Skipf("this filesystem does not support symbolic links: %v", err)
	}
	if _, err := importer.Extract(t.Context(), plan(t, importer.RawFraming, "", "cr", importer.UTF8, "unknown"), nil, []string{alias}, nil); err == nil {
		t.Fatal("a folder named through a symbolic link was imported")
	}
}
