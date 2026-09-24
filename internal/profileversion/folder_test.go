package profileversion_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/profileversion"
)

// place writes named fixtures into a new folder, each under the name given.
func place(t *testing.T, documents map[string][]byte) string {
	t.Helper()
	folder := t.TempDir()
	for name, data := range documents {
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// TestVerifyFolderRefusesAVersionSealedOverOtherRules is sealed-version
// immutability at the folder a profile is saved into: the same rules under the
// sealed version pass, changed rules under that version are refused by name,
// and the changed rules under a new version pass. Everything in the folder
// that is not a seal of this version is passed over.
func TestVerifyFolderRefusesAVersionSealedOverOtherRules(t *testing.T) {
	folder := place(t, map[string][]byte{
		"profile-version.json": fixture(t, "profile-version.json"),
		"local-profile.json":   fixture(t, "local-profile.json"),
		"profile-pack.json":    fixture(t, "profile-pack.json"),
		"other-version.json":   mutate(t, "profile-version.json", `"version": "1"`, `"version": "7"`),
		"oversized.json":       make([]byte, profileversion.MaxVersionBytes+1),
	})
	if err := os.Mkdir(filepath.Join(folder, "nested.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	sealed := profile(t)
	if err := profileversion.VerifyFolder(folder, sealed); err != nil {
		t.Fatalf("the sealed rules under their own version: %v", err)
	}

	changed := profile(t)
	changed.Segments[0].Description = "A description the seal was not computed over."
	err := profileversion.VerifyFolder(folder, changed)
	if err == nil || err.Error() != "profile version 1 is already sealed with different content; increment version to save changes" {
		t.Fatalf("changed rules under the sealed version: %v", err)
	}

	changed.Identity.Version = "2"
	if err := profileversion.VerifyFolder(folder, changed); err != nil {
		t.Fatalf("changed rules under a new version: %v", err)
	}

	// A seal under a name that is not *.json is not one of the folder's seals.
	elsewhere := place(t, map[string][]byte{"profile-version.seal": fixture(t, "profile-version.json")})
	changed.Identity.Version = "1"
	if err := profileversion.VerifyFolder(elsewhere, changed); err != nil {
		t.Fatalf("a seal not named *.json was read: %v", err)
	}
}

// TestVerifyFolderRefusesAFolderItCannotList is the live defect: a check that
// could not look has not found the version unsealed, so a folder that is not
// there, or is not a folder, is refused rather than passed.
func TestVerifyFolderRefusesAFolderItCannotList(t *testing.T) {
	folder := place(t, map[string][]byte{"profile-version.json": fixture(t, "profile-version.json")})
	for _, path := range []string{filepath.Join(folder, "absent"), filepath.Join(folder, "profile-version.json")} {
		err := profileversion.VerifyFolder(path, profile(t))
		if err == nil || !strings.Contains(err.Error(), "whether version 1 is already sealed cannot be checked") {
			t.Errorf("%s: %v", path, err)
		}
	}
}
