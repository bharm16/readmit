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
	result := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json")).OpenWorkspace(root)
	if result.State != desktop.PermissionDenied || result.Workspace != nil {
		t.Fatalf("an unreadable folder was not reported as permission denied: %+v", result)
	}
	if result.Reason == "" {
		t.Fatal("permission denial gave the shell nothing to show")
	}
}

func TestSampleWorkspaceSeparatesPermissionFromFailure(t *testing.T) {
	parent := t.TempDir()
	app := desktop.New(&chooser{folder: parent}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
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
	result := desktop.New(&chooser{}, store, filepath.Join(filepath.Dir(store), "filters.json"), filepath.Join(filepath.Dir(store), "session.json"), filepath.Join(filepath.Dir(filepath.Join(filepath.Dir(store), "session.json")), "drafts.json")).RecentWorkspaces()
	if result.State != desktop.PermissionDenied || len(result.Roots) != 0 {
		t.Fatalf("an unreadable recent list was not reported as permission denied: %+v", result)
	}
}

func TestWorkspaceListingRefusesToFollowSymbolicLinks(t *testing.T) {
	parent := t.TempDir()
	app := desktop.New(&chooser{folder: parent}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
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
	if opened := app.OpenCase(root, "alias"); opened.State != desktop.Failed || opened.Case != nil ||
		opened.Reason != caseEntry[0] {
		t.Fatalf("a symbolic link was opened as a case: %+v", opened)
	}
	// The workspace root itself is still refused when it is reached by a link.
	workspaceAlias := filepath.Join(parent, "workspace-alias")
	if err := os.Symlink(root, workspaceAlias); err != nil {
		t.Fatal(err)
	}
	if result := app.OpenWorkspace(workspaceAlias); result.State != desktop.Failed || result.Workspace != nil ||
		result.Reason != notAWorkspace[0] {
		t.Fatalf("a symbolic link was opened as a workspace: %+v", result)
	}
}

func TestOpenProjectSeparatesPermissionFromFailure(t *testing.T) {
	// An unreadable folder, and a readable folder holding a document this
	// account cannot read, are each permission rather than a missing project.
	for name, deny := range map[string]func(*testing.T, string){
		"folder":   func(t *testing.T, root string) { unreadable(t, root) },
		"document": func(t *testing.T, root string) { unreadable(t, filepath.Join(root, "project.json")) },
	} {
		root := t.TempDir()
		writeProject(t, root, "")
		deny(t, root)
		result := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json")).OpenProject(root)
		if result.State != desktop.PermissionDenied || result.Project != nil {
			t.Fatalf("an unreadable %s was not reported as permission denied: %+v", name, result)
		}
		if result.Reason == "" {
			t.Fatalf("permission denial on the %s gave the shell nothing to show", name)
		}
	}
}

func TestSearchSeparatesPermissionFromFailure(t *testing.T) {
	root := t.TempDir()
	unreadable(t, root)
	result := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json")).Search(root, "regression")
	if result.State != desktop.PermissionDenied || len(result.Matches) != 0 {
		t.Fatalf("an unreadable folder was not reported as permission denied: %+v", result)
	}
	if result.Reason == "" {
		t.Fatal("permission denial gave the shell nothing to show")
	}
}

// A saved-filter document this account cannot read is permission, not a
// document this release cannot read: the two have different remedies, and the
// window says which one it is.
func TestSavedFiltersSeparatePermissionFromAnUnreadableDocument(t *testing.T) {
	state := t.TempDir()
	store := filepath.Join(state, "filters.json")
	if err := os.WriteFile(store, []byte(`{"schema":"readmit-filters/v1","filters":[],"selected":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	unreadable(t, store)
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), store, filepath.Join(state, "session.json"), filepath.Join(filepath.Dir(filepath.Join(state, "session.json")), "drafts.json"))
	if result := app.Filters(); result.State != desktop.PermissionDenied || len(result.Filters) != 0 {
		t.Fatalf("an unreadable saved-filter document was not reported as permission denied: %+v", result)
	}
	// A grid never quietly shows an unfiltered view when the selection cannot
	// be read: it reports the same refusal.
	root := t.TempDir()
	if result := app.OpenGrid(root, "case", "case.index.json", 0, 10); result.State == desktop.Completed {
		t.Fatalf("a grid was rendered while the selection could not be read: %+v", result)
	}
}
