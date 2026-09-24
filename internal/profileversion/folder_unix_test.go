//go:build !windows

package profileversion_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/profileversion"
)

// TestVerifyFolderRefusesAFolderThisAccountCannotRead is the defect the check
// had: a folder this account may write into and not list passed, so changed
// rules could be saved beside the seal of their version. It is refused now,
// and once the folder can be listed again the seal refuses the changed rules.
func TestVerifyFolderRefusesAFolderThisAccountCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the superuser lists a folder whatever its mode")
	}
	folder := place(t, map[string][]byte{"profile-version.json": fixture(t, "profile-version.json")})
	changed := profile(t)
	changed.Segments[0].Description = "A description the seal was not computed over."
	if err := os.Chmod(folder, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(folder, 0o700) })
	err := profileversion.VerifyFolder(folder, changed)
	if err == nil || !strings.Contains(err.Error(), "cannot read the folder the profile is saved into") {
		t.Fatalf("an unlistable folder: %v", err)
	}
	if err := os.Chmod(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := profileversion.VerifyFolder(folder, changed); err == nil {
		t.Fatal("the seal did not refuse the changed rules")
	}
}

// TestVerifyFolderNeverReadsThroughALink passes over a symbolic link to a
// seal, so only the seals somebody put in the folder are held against a save.
func TestVerifyFolderNeverReadsThroughALink(t *testing.T) {
	outside := place(t, map[string][]byte{"profile-version.json": fixture(t, "profile-version.json")})
	folder := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "profile-version.json"), filepath.Join(folder, "linked.json")); err != nil {
		t.Skipf("this filesystem creates no symbolic link: %v", err)
	}
	changed := profile(t)
	changed.Segments[0].Description = "A description the seal was not computed over."
	if err := profileversion.VerifyFolder(folder, changed); err != nil {
		t.Fatalf("a linked seal was read: %v", err)
	}
}

// TestVerifyFolderPassesOverAMemberItCannotRead holds the check to what it
// promises for a member this account cannot read: it is passed over, as any
// other entry that is not a readable seal is, while the folder itself can be
// listed. Only a folder that cannot be listed is refused.
func TestVerifyFolderPassesOverAMemberItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the superuser reads a file whatever its mode")
	}
	folder := place(t, map[string][]byte{"profile-version.json": fixture(t, "profile-version.json")})
	if err := os.Chmod(filepath.Join(folder, "profile-version.json"), 0); err != nil {
		t.Fatal(err)
	}
	changed := profile(t)
	changed.Segments[0].Description = "A description the seal was not computed over."
	if err := profileversion.VerifyFolder(folder, changed); err != nil {
		t.Fatalf("an unreadable member was not passed over: %v", err)
	}
}
