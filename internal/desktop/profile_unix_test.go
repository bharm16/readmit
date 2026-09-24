//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// TestSaveProfileRefusesAWorkspaceItCannotCheckForSeals is the immutability
// check's live defect: in a workspace this account may write into and not
// list, the check for a seal of the same version passed silently, so changed
// rules were saved under a version already sealed beside them. Saving is
// refused now, and nothing is written.
func TestSaveProfileRefusesAWorkspaceItCannotCheckForSeals(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the superuser lists a folder whatever its mode")
	}
	app := workspaceApp(t)
	root := t.TempDir()
	writeDocument(t, root, "profile-version.json", fixture(t, "profile-version.json"))
	changed := strings.Replace(fixture(t, "local-profile.json"),
		"The scheduling segment, constrained beyond what the pinned pack labels.", "A description version 1 was not sealed over.", 1)
	if err := os.Chmod(root, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })
	saved := app.SaveProfile(desktop.ProfileSaveRequest{Workspace: root, Document: changed, Output: "changed.json", SealOutput: "changed-seal.json"})
	if saved.State != desktop.Failed || !strings.Contains(saved.Reason, "whether version 1 is already sealed cannot be checked") {
		t.Fatalf("saving into a workspace that cannot be listed: %+v", saved)
	}
	for _, name := range []string{"changed.json", "changed-seal.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was written: %v", name, err)
		}
	}

	// Listed again, the seal beside it refuses the changed rules by name.
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	saved = app.SaveProfile(desktop.ProfileSaveRequest{Workspace: root, Document: changed, Output: "changed.json"})
	if saved.State != desktop.Failed || saved.Reason != "profile version 1 is already sealed with different content; increment version to save changes" {
		t.Fatalf("saving changed rules beside their version's seal: %+v", saved)
	}
}
