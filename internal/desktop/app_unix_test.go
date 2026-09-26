//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"reflect"
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
	result := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()}).OpenWorkspace(root)
	if result.State != desktop.PermissionDenied || result.Workspace != nil {
		t.Fatalf("an unreadable folder was not reported as permission denied: %+v", result)
	}
	if result.Reason == "" {
		t.Fatal("permission denial gave the shell nothing to show")
	}
}

func TestSampleWorkspaceSeparatesPermissionFromFailure(t *testing.T) {
	parent := t.TempDir()
	app := desktop.New(&chooser{folder: parent}, desktop.ShellDocuments{Folder: t.TempDir()})
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
	result := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: filepath.Dir(store)}).RecentWorkspaces()
	if result.State != desktop.PermissionDenied || len(result.Roots) != 0 {
		t.Fatalf("an unreadable recent list was not reported as permission denied: %+v", result)
	}
}

func TestWorkspaceListingRefusesToFollowSymbolicLinks(t *testing.T) {
	parent := t.TempDir()
	app := desktop.New(&chooser{folder: parent}, desktop.ShellDocuments{Folder: t.TempDir()})
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
		result := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()}).OpenProject(root)
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
	result := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()}).Search(root, "regression")
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
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
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

// A list this account cannot replace is a different answer from one this
// release cannot read, and forgetting says which. The list stays as it was.
func TestForgettingSeparatesAnUnwritableListFromAFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	directory := t.TempDir()
	store := filepath.Join(directory, "recent.json")
	app := activatedApp(t, &chooser{}, filepath.Dir(store))
	root := t.TempDir()
	if result := app.OpenWorkspace(root); result.State != desktop.Empty {
		t.Fatalf("open: %+v", result)
	}
	before, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(directory, 0700) })
	result := app.ForgetWorkspace(resolved(t, root))
	if result.State != desktop.PermissionDenied || result.Reason != "this account cannot write the recent workspace list" || !reflect.DeepEqual(result.Roots, []string{resolved(t, root)}) {
		t.Fatalf("an unwritable recent list: %+v", result)
	}
	if after, err := os.ReadFile(store); err != nil || string(after) != string(before) {
		t.Fatalf("a refused forget changed the list: %q", after)
	}
}
