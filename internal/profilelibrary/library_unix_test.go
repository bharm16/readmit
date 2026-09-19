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
