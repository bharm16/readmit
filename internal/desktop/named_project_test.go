package desktop_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// A project is created from a name alone: no interface revision is asked for
// or invented, its folder is the application's, and the folder new projects
// are kept in is asked for once and remembered.
func TestANewProjectNeedsOnlyAName(t *testing.T) {
	location := t.TempDir()
	dialogs := &chooser{folder: location}
	app := newApp(t, dialogs)
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA"})
	if created.State != desktop.Completed || created.Project == nil || created.Project.Name != "Scheduling QA" {
		t.Fatalf("create: %+v", created)
	}
	folder := created.Project.Summary.Project.Folder
	if filepath.Dir(folder) != resolved(t, location) || created.Context.Project != folder || created.Context.ProjectID != created.Project.Ref.ID {
		t.Fatalf("the project was not created in the chosen folder: %+v", created)
	}
	opened, err := project.Open(folder)
	if err != nil || opened.Document.Schema != project.SchemaV2 || len(opened.Document.InterfaceVersions) != 0 || opened.Document.Settings.DefaultInterfaceVersion != "" {
		t.Fatalf("the new project declares a revision nobody gave it: %+v %v", opened, err)
	}
	if created.Project.Summary.Project.Cases != 0 || len(created.Project.Summary.Project.InterfaceVersions) != 0 {
		t.Fatalf("summary: %+v", created.Project.Summary.Project)
	}
	// The second project is created in the remembered folder without a
	// dialog, and an overview reads the empty project as it is.
	dialogs.folder = ""
	second := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Order interface"})
	if second.State != desktop.Completed || len(dialogs.titles) != 1 {
		t.Fatalf("the remembered folder was asked for again: %+v %v", second, dialogs.titles)
	}
	if overview := app.OpenProjectOverview(folder); overview.State != desktop.Empty || overview.Overview == nil || len(overview.Overview.InterfaceVersions) != 0 {
		t.Fatalf("overview of an empty v2 project: %+v", overview)
	}
	if remembered := app.ProjectLocation(); remembered.State != desktop.Completed || remembered.Location != location {
		t.Fatalf("location: %+v", remembered)
	}
	// A remembered folder that is gone is asked for again, never created.
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o700); err != nil {
		t.Fatal(err)
	}
	dialogs.folder = gone
	if chosen := app.ChooseProjectLocation(); chosen.State != desktop.Completed {
		t.Fatalf("choose: %+v", chosen)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	dialogs.folder = ""
	if refused := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Third"}); refused.State != desktop.Cancelled {
		t.Fatalf("a missing projects folder was not asked for again: %+v", refused)
	}
	if _, err := os.Lstat(gone); !os.IsNotExist(err) {
		t.Fatal("a remembered projects folder that was gone was created")
	}
}

// Two projects may share a name; renaming one changes its title alone, and
// moving its folder keeps its identity, its cases, its tests and its history
// — without a byte of evidence rewritten.
func TestAProjectIsRenamedAndMovedWithoutRewritingEvidence(t *testing.T) {
	location := t.TempDir()
	app := newApp(t, &chooser{folder: location})
	first := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA"})
	second := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA"})
	if first.State != desktop.Completed || second.State != desktop.Completed {
		t.Fatalf("%+v %+v", first, second)
	}
	if first.Project.Ref.ID == second.Project.Ref.ID || first.Context.Project == second.Context.Project {
		t.Fatalf("two projects of one name share an identity or a folder: %+v %+v", first.Project, second.Project)
	}
	projects := app.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem})
	if projects.Page == nil || projects.Page.Total != 2 {
		t.Fatalf("projects: %+v", projects)
	}

	root := first.Context.Project
	writeCase(t, root, "incident", framed(fixture(t, "listen-s12.hl7")))
	writeDocument(t, root, "lab.json", replayTarget("127.0.0.1:2575", "nonproduction"))
	evidence := bytesUnder(t, filepath.Join(root, "incident"))
	context := first.Context
	cases := listed(t, app, root, desktop.CaseItem)
	environments := listed(t, app, root, desktop.EnvironmentItem)
	incident, lab := cases["@incident"], environments["lab-replay"]
	if incident.Ref.ID == "" || lab.Ref.ID == "" {
		t.Fatalf("%+v %+v", cases, environments)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: lab.Ref.ID,
		Draft: desktop.ItemDraft{Environment: targetDraft("127.0.0.1:2576")}, IntentID: "edit-lab"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}

	renamed := app.RenameItem(desktop.RenameRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}, Name: "Scheduling regression"})
	if renamed.State != desktop.Completed || renamed.Item.Name != "Scheduling regression" {
		t.Fatalf("rename: %+v", renamed)
	}

	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	for _, known := range app.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem}).Page.Items {
		if known.Ref.ID == context.ProjectID && (known.Availability != desktop.ItemMissing || known.Capabilities[0] != desktop.LocateAction) {
			t.Fatalf("a moved project is not a visible missing row: %+v", known)
		}
	}
	// A window that still asks about the old folder is refused, and one that
	// asks about the new folder under the old identity is answered.
	if stale := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem}); stale.State == desktop.Completed {
		t.Fatalf("the old folder answered: %+v", stale)
	}
	// Locating the project in a folder that holds another project is
	// refused; locating it where it now is finds it under its identity.
	if wrong := app.LocateItem(desktop.LocateRequest{Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}, Folder: second.Context.Project}); wrong.State != desktop.Failed {
		t.Fatalf("a project was located in another project's folder: %+v", wrong)
	}
	found := app.LocateItem(desktop.LocateRequest{Ref: desktop.ItemRef{Kind: desktop.ProjectItem, ID: context.ProjectID}, Folder: moved})
	if found.State != desktop.Completed || found.Item.Ref.ID != context.ProjectID || found.Item.Availability != desktop.ItemAvailable {
		t.Fatalf("locate the moved project: %+v", found)
	}
	reopened := app.OpenNamedProject(moved)
	if reopened.State != desktop.Completed || reopened.Project.Ref.ID != context.ProjectID || reopened.Project.Name != "Scheduling regression" ||
		reopened.Project.Availability != desktop.ItemAvailable {
		t.Fatalf("reopen: %+v", reopened)
	}
	after := desktop.RequestContext{Project: reopened.Context.Project, ProjectID: context.ProjectID}
	casesAfter := app.ListCatalog(desktop.CatalogQuery{Context: after, Kind: desktop.CaseItem})
	if casesAfter.Page == nil || len(casesAfter.Page.Items) != 1 || casesAfter.Page.Items[0].Ref != incident.Ref || casesAfter.Page.Items[0].Availability != desktop.ItemAvailable {
		t.Fatalf("the case after the move: %+v", casesAfter)
	}
	environmentsAfter := app.ListCatalog(desktop.CatalogQuery{Context: after, Kind: desktop.EnvironmentItem})
	if environmentsAfter.Page == nil || len(environmentsAfter.Page.Items) != 1 || environmentsAfter.Page.Items[0].Ref.Revision != "1" ||
		environmentsAfter.Page.Items[0].Summary.Environment.Address != "127.0.0.1:2576" {
		t.Fatalf("the saved environment and its history after the move: %+v", environmentsAfter)
	}
	if now := bytesUnder(t, filepath.Join(moved, "incident")); !maps.EqualFunc(now, evidence, bytes.Equal) {
		t.Fatal("renaming or moving the project rewrote evidence")
	}
	if original := read(t, filepath.Join(moved, "lab.json")); !strings.Contains(original, "127.0.0.1:2575") {
		t.Fatal("saving the environment rewrote the file it was discovered in")
	}
	// The other project of the same name is untouched by all of it.
	if other := app.OpenNamedProject(second.Context.Project); other.State != desktop.Completed || other.Project.Name != "Scheduling QA" {
		t.Fatalf("the other project: %+v", other)
	}
	// A folder that now holds a different project is refused under the old
	// identity.
	if foreign := app.ListCatalog(desktop.CatalogQuery{Context: desktop.RequestContext{Project: second.Context.Project, ProjectID: context.ProjectID}, Kind: desktop.CaseItem}); foreign.State != desktop.Failed {
		t.Fatalf("another project's folder answered: %+v", foreign)
	}
}

// A v1 project reads exactly as written, is refused a change that needs the
// v2 model, and becomes v2 only when a person migrates it.
func TestAVersionOneProjectIsMigratedOnlyOnPurpose(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	writeProject(t, root, "")
	before := read(t, filepath.Join(root, "project.json"))
	if opened := app.OpenNamedProject(root); opened.State != desktop.Completed || opened.Project.Summary.Project.Schema != project.Schema {
		t.Fatalf("open: %+v", opened)
	}
	if read(t, filepath.Join(root, "project.json")) != before {
		t.Fatal("opening a v1 project rewrote it")
	}
	migrated := app.MigrateProjectDocument(root)
	if migrated.State == desktop.Failed || migrated.Overview == nil {
		t.Fatalf("migrate: %+v", migrated)
	}
	if after := read(t, filepath.Join(root, "project.json")); !strings.Contains(after, `"readmit-project/v2"`) ||
		strings.Replace(after, "v2", "v1", 1) != before {
		t.Fatalf("migration changed more than the contract:\n%s\n%s", before, after)
	}
	if again := app.MigrateProjectDocument(root); again.State != desktop.Failed {
		t.Fatalf("a v2 project was migrated again: %+v", again)
	}
}

// A viewer without an author seat opens and reads a project whose catalog was
// never recorded, and nothing is written into it; recording, renaming and
// saving are what need the seat.
func TestAProjectIsReadWithoutAnAuthorSeatAndWrittenOnlyWithOne(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, "")
	writeDocument(t, root, "lab.json", replayTarget("127.0.0.1:2575", "nonproduction"))
	reader := unlicensedApp(t)
	opened := reader.OpenNamedProject(root)
	if opened.State != desktop.Completed || opened.Recorded || opened.Project.Ref.ID != "" || opened.Context.ProjectID != "" {
		t.Fatalf("a read-only open: %+v", opened)
	}
	environments := listed(t, reader, root, desktop.EnvironmentItem)
	lab := environments["lab-replay"]
	if lab.Availability != desktop.ItemAvailable || slices.Contains(lab.Capabilities, desktop.SaveAction) || slices.Contains(lab.Capabilities, desktop.ReplaySendAction) {
		t.Fatalf("a readable object offered what the viewer may not do: %+v", lab)
	}
	if renamed := reader.RenameItem(desktop.RenameRequest{Context: opened.Context, Ref: lab.Ref, Name: "Lab"}); renamed.State != desktop.PermissionDenied {
		t.Fatalf("a rename without an author seat: %+v", renamed)
	}
	if _, err := os.Lstat(filepath.Join(root, ".readmit")); !os.IsNotExist(err) {
		t.Fatal("a viewer without an author seat wrote the project's catalog")
	}
	// An author opening it records it, under the identity the reader listed.
	author := workspaceApp(t)
	recorded := author.OpenNamedProject(root)
	if recorded.State != desktop.Completed || !recorded.Recorded || recorded.Project.Ref.ID == "" {
		t.Fatalf("an author's open: %+v", recorded)
	}
	if again := listed(t, author, root, desktop.EnvironmentItem)["lab-replay"]; again.Ref != lab.Ref {
		t.Fatalf("recording changed an object's identity: %+v %+v", again.Ref, lab.Ref)
	}
}
