//go:build !windows

package profilelibrary_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestALibraryMemberIsNeverReadThroughALink keeps a library to the directory
// somebody assembled: a symbolic link named like a pack is refused rather than
// followed, so nothing outside the directory is read as a member of it.
func TestALibraryMemberIsNeverReadThroughALink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	data, err := os.ReadFile(fixtureRoot + "profile-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		t.Fatal(err)
	}
	root := directory(t, "profile-pack-adt.json")
	if err := os.Symlink(outside, filepath.Join(root, "linked.json")); err != nil {
		t.Skipf("this filesystem creates no symbolic link: %v", err)
	}
	refuses(t, root, "regular profile pack documents named *.json and nothing else")
}

// TestAnUnreadableMemberRefusesALibraryAndIsPassedOverByDiscovery holds the
// two ways of reading packs from a place to their own rules for a member this
// account cannot read: a library is refused whole, and discovery passes over
// it, answering with the readable pinned pack beside it and with nothing when
// the unreadable member is the only candidate.
func TestAnUnreadableMemberRefusesALibraryAndIsPassedOverByDiscovery(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the superuser reads a file whatever its mode")
	}
	root := directory(t, "profile-pack.json", "profile-pack-adt.json")
	if err := os.Chmod(filepath.Join(root, "profile-pack-adt.json"), 0); err != nil {
		t.Fatal(err)
	}
	refuses(t, root, "cannot read a profile library member")
	pinned(t, root, siuPin, true)

	alone := directory(t, "profile-pack.json")
	if err := os.Chmod(filepath.Join(alone, "profile-pack.json"), 0); err != nil {
		t.Fatal(err)
	}
	pinned(t, alone, siuPin, false)
}

// TestDiscoveryNeverReadsThroughALink passes over a symbolic link named like
// the pinned pack, so a folder offers only the documents somebody put in it.
func TestDiscoveryNeverReadsThroughALink(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	data, err := os.ReadFile(fixtureRoot + "profile-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0o600); err != nil {
		t.Fatal(err)
	}
	root := directory(t, "profile-pack-adt.json")
	if err := os.Symlink(outside, filepath.Join(root, "linked.json")); err != nil {
		t.Skipf("this filesystem creates no symbolic link: %v", err)
	}
	pinned(t, root, siuPin, false)
}
