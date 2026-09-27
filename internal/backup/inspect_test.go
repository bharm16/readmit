package backup_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/backup"
)

// Inspecting a backup reads its marker, the seal over its manifest and the
// manifest itself, and nothing it stores: a listing of backups stays cheap
// however large each one is, and a later full Verify is the explicit recheck.
func TestInspectReadsTheSealedManifestAndNotTheStoredFiles(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "case-a", 1)
	_, stored := create(t, opened.Root)

	document, identity, err := backup.Inspect(stored)
	if err != nil || len(identity) != 64 || len(document.Files) == 0 || !document.Complete() {
		t.Fatalf("inspect: %v %q %+v", err, identity, document)
	}
	marker, err := os.ReadFile(filepath.Join(stored, backup.MarkerName))
	if err != nil || string(marker) != identity+"\n" {
		t.Fatalf("the identity is not the completion marker's digest: %q %v", marker, err)
	}
	// A stored file damaged after the backup was sealed is only found by a
	// full verification, which reads every byte.
	damaged := filepath.Join(stored, backup.FilesDirectory, document.Files[0].Path)
	if err := os.WriteFile(damaged, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, again, err := backup.Inspect(stored); err != nil || again != identity {
		t.Fatalf("inspect read the stored files: %v %q", err, again)
	}
	if _, err := backup.Verify(stored); err == nil {
		t.Fatal("full verification accepted a damaged stored file")
	}

	if err := os.Remove(filepath.Join(stored, backup.MarkerName)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := backup.Inspect(stored); !errors.Is(err, backup.ErrIncomplete) {
		t.Fatalf("an unsealed backup: %v", err)
	}
	if _, _, err := backup.Inspect(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing backup was inspected")
	}
}

// Removing a backup removes exactly what its manifest names, and only while
// it is still the backup that was reviewed: a file the manifest does not
// record, or a backup sealed over different contents, refuses the removal
// before anything is removed.
func TestRemoveDeletesOnlyWhatTheManifestNames(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "case-a", 1)
	_, stored := create(t, opened.Root)
	_, identity, err := backup.Inspect(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.Remove(stored, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("a backup was removed under an identity it does not carry")
	}
	stray := filepath.Join(stored, backup.FilesDirectory, "unrecorded.txt")
	if err := os.WriteFile(stray, []byte("a person's own notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := tree(t, stored)
	if err := backup.Remove(stored, identity); err == nil {
		t.Fatal("a backup holding an unrecorded file was removed")
	}
	if !sameTree(before, tree(t, stored)) {
		t.Fatal("a refused removal removed something")
	}
	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}
	if err := backup.Remove(stored, identity); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Lstat(stored); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the backup folder is still there: %v", err)
	}
	if _, err := os.Stat(opened.Root); err != nil {
		t.Fatalf("removing a backup touched its source: %v", err)
	}
}
