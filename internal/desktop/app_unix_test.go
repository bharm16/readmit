//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// unreadable removes every permission from path for the duration of one test.
// A privileged account ignores the mode bits, so the test skips instead of
// silently asserting nothing.
func unreadable(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0700) })
}

func TestOpenWorkspaceSeparatesPermissionFromFailure(t *testing.T) {
	root := t.TempDir()
	unreadable(t, root)
	result := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json")).OpenWorkspace(root)
	if result.State != desktop.PermissionDenied || result.Workspace != nil {
		t.Fatalf("an unreadable folder was not reported as permission denied: %+v", result)
	}
	if result.Reason == "" {
		t.Fatal("permission denial gave the shell nothing to show")
	}
}

func TestSampleWorkspaceSeparatesPermissionFromFailure(t *testing.T) {
	parent := t.TempDir()
	app := desktop.New(&chooser{folder: parent}, filepath.Join(t.TempDir(), "recent.json"))
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0700) })
	result := app.CreateSampleWorkspace()
	if result.State != desktop.PermissionDenied || result.Workspace != nil {
		t.Fatalf("a read-only folder was not reported as permission denied: %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(parent, desktop.SampleName)); !os.IsNotExist(err) {
		t.Fatal("a refused sample workspace left output behind")
	}
}

func TestRecentWorkspacesSeparatesPermissionFromFailure(t *testing.T) {
	directory := t.TempDir()
	store := filepath.Join(directory, "recent.json")
	if err := os.WriteFile(store, []byte(`{"schema":"readmit-desktop-recent/v1","roots":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	unreadable(t, store)
	result := desktop.New(&chooser{}, store).RecentWorkspaces()
	if result.State != desktop.PermissionDenied || len(result.Roots) != 0 {
		t.Fatalf("an unreadable recent list was not reported as permission denied: %+v", result)
	}
}

func TestWorkspaceListingRefusesToFollowSymbolicLinks(t *testing.T) {
	parent := t.TempDir()
	app := desktop.New(&chooser{folder: parent}, filepath.Join(t.TempDir(), "recent.json"))
	root := sample(t, app).Workspace.Root
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Join(root, "regression"), alias); err != nil {
		t.Fatal(err)
	}
	listing := app.OpenWorkspace(root)
	if listing.State != desktop.Completed {
		t.Fatalf("listing: %+v", listing)
	}
	var linked desktop.Artifact
	for _, artifact := range listing.Workspace.Artifacts {
		if artifact.Name == "alias" {
			linked = artifact
		}
	}
	if linked.Kind != desktop.UnsupportedArtifact || linked.Schema != "" {
		t.Fatalf("a symbolic link was listed as evidence: %+v", linked)
	}
	if opened := app.OpenCase(root, "alias"); opened.State != desktop.Failed || opened.Case != nil {
		t.Fatalf("a symbolic link was opened as a case: %+v", opened)
	}
	// The workspace root itself is still refused when it is reached by a link.
	workspaceAlias := filepath.Join(parent, "workspace-alias")
	if err := os.Symlink(root, workspaceAlias); err != nil {
		t.Fatal(err)
	}
	if result := app.OpenWorkspace(workspaceAlias); result.State != desktop.Failed {
		t.Fatalf("a symbolic link was opened as a workspace: %+v", result)
	}
}
