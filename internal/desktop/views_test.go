package desktop_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

func viewNames(result desktop.ViewsResult) []string {
	names := []string{}
	for _, view := range result.Views {
		names = append(names, view.Name)
	}
	return names
}

// A view is saved only explicitly, for one project, and reopening it gives
// back the exact criteria it was saved with — an exclusive end time, an
// omitted field state and a null one included. Renaming and removing a view
// change only the saved query: the evidence is never touched.
func TestSavedViewsAreProjectLocalExactAndNeverTouchEvidence(t *testing.T) {
	app, root, state, opened := messagesWorkspace(t)
	other := t.TempDir()
	evidence := bytesUnder(t, root)
	if listed := app.ListViews(root); listed.State != desktop.Empty || len(listed.Views) != 0 {
		t.Fatalf("a project with no view: %+v", listed)
	}
	query := grid.Query{
		NotTypes:      []grid.MessageType{{Kind: bundle.Unparsed}},
		ObservedFrom:  minute(5),
		ObservedUntil: minute(11),
		Fields: []grid.FieldPredicate{
			{Selector: patientField, Match: index.State, State: hl7.Present},
			{Selector: "PID[1]-7[1]", Match: index.State, State: hl7.Omitted},
		},
	}
	applied := readMessages(t, app, root, opened, query)
	if saved := bytesUnder(t, state); len(saved) != 0 {
		t.Fatalf("applying a query stored it: %v", saved)
	}
	saved := app.SaveView(root, "Booked before 12:11", query)
	if saved.State != desktop.Completed || !reflect.DeepEqual(viewNames(saved), []string{"Booked before 12:11"}) {
		t.Fatalf("save view: %+v", saved)
	}
	if elsewhere := app.ListViews(other); elsewhere.State != desktop.Empty {
		t.Fatalf("a view leaked into another project: %+v", elsewhere)
	}
	reopened := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state}).ListViews(root)
	if reopened.State != desktop.Completed || len(reopened.Views) != 1 {
		t.Fatalf("reopening the views: %+v", reopened)
	}
	again := readMessages(t, app, root, opened, reopened.Views[0].Query)
	if !reflect.DeepEqual(rowIDs(again.Rows), rowIDs(applied.Rows)) || again.Matched != applied.Matched ||
		!reflect.DeepEqual(rowIDs(applied.Rows), []string{"s0001-e000001", "s0001-e000003"}) {
		t.Fatalf("a reopened view matched %v, the applied query %v", rowIDs(again.Rows), rowIDs(applied.Rows))
	}

	if refused := app.SaveView(root, strings.Repeat("n", 201), query); refused.State != desktop.Failed || len(refused.Views) != 1 {
		t.Fatalf("a long name: %+v", refused)
	}
	if refused := app.SaveView(root, "bad", grid.Query{Directions: []bundle.Direction{"up"}}); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "direction") {
		t.Fatalf("an invalid query: %+v", refused)
	}
	app.SaveView(root, "Second", grid.Query{})
	if clash := app.RenameView(root, "Second", "Booked before 12:11"); clash.State != desktop.Failed || len(clash.Views) != 2 {
		t.Fatalf("a rename onto another view: %+v", clash)
	}
	renamed := app.RenameView(root, "Second", "All")
	if renamed.State != desktop.Completed || !reflect.DeepEqual(viewNames(renamed), []string{"Booked before 12:11", "All"}) {
		t.Fatalf("rename: %+v", renamed)
	}
	if absent := app.RenameView(root, "missing", "x"); absent.State != desktop.Failed {
		t.Fatalf("renaming a view nobody saved: %+v", absent)
	}
	removed := app.RemoveView(root, "Booked before 12:11")
	if removed.State != desktop.Completed || !reflect.DeepEqual(viewNames(removed), []string{"All"}) {
		t.Fatalf("remove: %+v", removed)
	}
	if last := app.RemoveView(root, "All"); last.State != desktop.Empty {
		t.Fatalf("removing the last view: %+v", last)
	}
	if !reflect.DeepEqual(bytesUnder(t, root), evidence) {
		t.Fatal("saving, renaming or removing a view changed the project folder")
	}
	// The saved filters a v1 release wrote are still read beside the views.
	if filters := app.SaveFilter(grid.Filter{Name: "acks", Kinds: []bundle.EventKind{bundle.Acknowledgement}}); filters.State != desktop.Completed {
		t.Fatalf("a saved filter beside views: %+v", filters)
	}
	if views := app.SaveView(root, "kept", grid.Query{}); views.State != desktop.Completed || len(app.Filters().Filters) != 1 {
		t.Fatalf("a view and a filter did not coexist: %+v", views)
	}
}

// Clear saved searches removes every view of this project and nothing else:
// another project's views are kept, and no evidence or index is touched.
func TestClearViewsRemovesOnlyThisProjectsSavedQueries(t *testing.T) {
	app, root, state, _ := messagesWorkspace(t)
	other := t.TempDir()
	query := grid.Query{AckCodes: []string{"AA"}}
	for _, name := range []string{"Accepted", "Accepted again"} {
		if saved := app.SaveView(root, name, query); saved.State != desktop.Completed {
			t.Fatalf("save %s: %+v", name, saved)
		}
	}
	if saved := app.SaveView(other, "Elsewhere", query); saved.State != desktop.Completed {
		t.Fatalf("save elsewhere: %+v", saved)
	}
	evidence := bytesUnder(t, root)
	cleared := app.ClearViews(root)
	if cleared.State != desktop.Empty || len(cleared.Views) != 0 {
		t.Fatalf("clear: %+v", cleared)
	}
	if listed := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state}).ListViews(root); listed.State != desktop.Empty {
		t.Fatalf("cleared views came back: %+v", listed)
	}
	if kept := app.ListViews(other); !reflect.DeepEqual(viewNames(kept), []string{"Elsewhere"}) {
		t.Fatalf("another project's views were cleared: %+v", kept)
	}
	if !reflect.DeepEqual(bytesUnder(t, root), evidence) {
		t.Fatal("clearing the views changed the project")
	}
	if again := app.ClearViews(root); again.State != desktop.Empty {
		t.Fatalf("clearing a project with no view: %+v", again)
	}
}

// A project's views are saved under the stable identity the application
// gave the project, so they follow it when its folder moves. Views saved
// under its folder before it had one are read there, and the next change
// moves them under the identity.
func TestSavedViewsFollowTheProjectsIdentityAndMoveFromItsFolder(t *testing.T) {
	dialogs := &chooser{folder: t.TempDir()}
	state := t.TempDir()
	app := activatedApp(t, dialogs, state)
	root := sample(t, app).Workspace.Root
	writeDocument(t, root, "project.json", `{"schema":"readmit-project/v2","settings":{"title":"Scheduling QA"},"interface_versions":[],"cases":[]}`+"\n")
	if saved := app.SaveView(root, "Before", grid.Query{}); saved.State != desktop.Completed {
		t.Fatalf("a view of a folder with no identity yet: %+v", saved)
	}
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed || !opened.Recorded || opened.Context.ProjectID == "" {
		t.Fatalf("open: %+v", opened)
	}
	if listed := app.ListViews(root); !reflect.DeepEqual(viewNames(listed), []string{"Before"}) {
		t.Fatalf("the folder's views under the project's identity: %+v", listed)
	}
	if saved := app.SaveView(root, "After", grid.Query{}); !reflect.DeepEqual(viewNames(saved), []string{"Before", "After"}) {
		t.Fatalf("save: %+v", saved)
	}
	var stored strings.Builder
	for _, data := range bytesUnder(t, state) {
		stored.Write(data)
	}
	if !strings.Contains(stored.String(), `"project":"`+opened.Context.ProjectID+`"`) || strings.Count(stored.String(), `"project":`) != 1 {
		t.Fatalf("the views were not moved under the project's identity: %s", stored.String())
	}
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if listed := app.ListViews(moved); !reflect.DeepEqual(viewNames(listed), []string{"Before", "After"}) {
		t.Fatalf("a moved project lost its views: %+v", listed)
	}
}
