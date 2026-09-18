//go:build !windows

package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/backup"
)

// A backup stores regular files. A symbolic link in the project is refused
// rather than followed — following one copies bytes from outside the project
// into evidence — and rather than skipped, which would produce a backup quietly
// missing part of what it was pointed at.
func TestBackupRefusesLinkedEntriesOfTheProject(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("not this project\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, link := range map[string]string{
		"escaping link": outside,
		"source alias":  "project.json",
		"case alias":    "regression",
	} {
		t.Run(name, func(t *testing.T) {
			opened := newProject(t)
			registerCase(t, opened, "regression", 1)
			if err := os.Symlink(link, filepath.Join(opened.Root, "alias")); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(t.TempDir(), "backup")
			if _, err := backup.Create(context.Background(), opened.Root, destination); err == nil {
				t.Fatal("a backup stored a linked entry of the project")
			}
		})
	}
}

// A project directory that is itself a symbolic link is refused, exactly as
// every other strict artifact reader refuses one.
func TestBackupRefusesALinkedProjectRoot(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(opened.Root, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Create(context.Background(), alias, filepath.Join(t.TempDir(), "backup")); err == nil {
		t.Error("a backup was taken through a linked project root")
	}
	_, stored := create(t, opened.Root)
	linked := filepath.Join(t.TempDir(), "linked-backup")
	if err := os.Symlink(stored, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Verify(linked); err == nil {
		t.Error("a backup was verified through a linked root")
	}
	if _, err := backup.Restore(context.Background(), linked, filepath.Join(t.TempDir(), "restored"), builtAt()); err == nil {
		t.Error("a project was restored through a linked backup root")
	}
}

// A link smuggled into the files a backup stores is refused when the backup is
// read, so a restore never follows one out of the directory it was given.
func TestVerifyRefusesALinkedStoredFile(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	_, stored := create(t, opened.Root)
	if err := os.Symlink("../"+backup.DocumentName, filepath.Join(stored, backup.FilesDirectory, "alias.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Verify(stored); err == nil {
		t.Fatal("a backup holding a linked file verified")
	}
	target := filepath.Join(t.TempDir(), "restored")
	if _, err := backup.Restore(context.Background(), stored, target, builtAt()); err == nil {
		t.Error("a backup holding a linked file restored")
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused restore created a destination")
	}
}

// A destination named through a link is resolved before it is used, so what is
// written is the physical directory the link leads to and the report names that
// path rather than the one that was typed.
func TestRestoreWritesThePathTheReservationReturned(t *testing.T) {
	opened := newProject(t)
	registerCase(t, opened, "regression", 1)
	_, stored := create(t, opened.Root)

	physical := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	report, err := backup.Restore(context.Background(), stored, filepath.Join(alias, "restored"), builtAt())
	if err != nil {
		t.Fatalf("restore through a linked parent: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(physical, "restored"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Root != resolved {
		t.Errorf("restore reported %q, wrote %q", report.Root, resolved)
	}
	if _, err := os.Stat(filepath.Join(resolved, "regression", "identity.sha256")); err != nil {
		t.Errorf("the restored project is not at the resolved path: %v", err)
	}
}
