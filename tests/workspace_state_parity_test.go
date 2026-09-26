package tests

// The window's project settings, saved filters, editable project document and
// guided-sample capture against the command line. Each is the same shared
// operation or reader the command runs, so the window writes the bytes the
// command writes and reads what the command reads. A change the project or
// the fixtures refuse is refused by both in the same words, and a document a
// later release wrote is refused by both and replaced by neither.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/project"
)

// stateApp is an unlicensed window whose local shell state lives in a known
// folder, so a test can place a document there that a later release wrote.
func stateApp(folder, state string) *desktop.App {
	return desktop.New(chosenFolder(folder), desktop.ShellDocuments{Folder: state})
}

// A settings Save in the window writes the project document the command
// writes for the same edit, byte for byte. A draft either refuses is left
// unwritten by both, and a document a later release wrote is refused by both
// and replaced by neither.
func TestProjectSettingsEditedInTheWindowMatchTheCommandLine(t *testing.T) {
	command, window := newProject(t), newProject(t)
	if !bytes.Equal(mustRead(t, filepath.Join(command, project.DocumentName)), mustRead(t, filepath.Join(window, project.DocumentName))) {
		t.Fatal("two projects created alike start from different documents")
	}
	if _, stderr, err := run(t, "project", "settings", command, "--title", "Epic scheduling handover", "--owner", "integration-team",
		"--interface-version", "siu-2.5.1-v2", "--default-interface-version", "siu-2.5.1-v2"); err != nil || stderr != "" {
		t.Fatalf("project settings: %v %s", err, stderr)
	}
	app := desktopApp(t, t.TempDir())
	opened := app.OpenNamedProject(window)
	if opened.State != desktop.Completed || opened.Project == nil {
		t.Fatalf("open: %+v", opened)
	}
	save := func(intent string, draft desktop.ProjectDraft) desktop.SaveItemResult {
		current := app.OpenItem(desktop.ItemRequest{Context: opened.Context, Ref: opened.Project.Ref})
		base := ""
		if current.Item != nil {
			base = current.Item.Ref.Revision
		}
		return app.SaveItem(desktop.SaveItemRequest{Context: opened.Context, Kind: desktop.ProjectItem, Item: opened.Project.Ref.ID,
			BaseRevision: base, IntentID: intent, Draft: desktop.ItemDraft{Project: &draft}})
	}
	kept := []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "siu-2.5.1-v1"}, {ID: "siu-2.5.1-v2", Name: "siu-2.5.1-v2", Default: true}}
	stored := save("settings", desktop.ProjectDraft{Name: "Epic scheduling handover", Owner: "integration-team",
		Revisions: []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "siu-2.5.1-v1"}, {Name: "siu-2.5.1-v2", Default: true}}})
	if stored.State != desktop.Completed || stored.Projection == nil || !reflect.DeepEqual(stored.Projection.Project.Revisions, kept) {
		t.Fatalf("the window's settings Save: %+v", stored)
	}
	edited := mustRead(t, filepath.Join(command, project.DocumentName))
	if !bytes.Equal(edited, mustRead(t, filepath.Join(window, project.DocumentName))) {
		t.Fatalf("the window wrote another document than the command for the same edit:\n%s\n%s", edited, mustRead(t, filepath.Join(window, project.DocumentName)))
	}

	for name, refusal := range map[string]struct {
		draft desktop.ProjectDraft
		flags []string
	}{
		"an empty title":           {desktop.ProjectDraft{Name: "", Revisions: kept}, []string{"--title", ""}},
		"an empty further version": {desktop.ProjectDraft{Name: "Epic scheduling handover", Revisions: append(kept[:2:2], desktop.RevisionDraft{Name: ""})}, []string{"--interface-version", ""}},
	} {
		t.Run(name, func(t *testing.T) {
			if result := save("refused-"+strings.ReplaceAll(name, " ", "-"), refusal.draft); result.Outcome != desktop.InvalidOutcome || len(result.Problems) == 0 {
				t.Fatalf("the window: %+v", result)
			}
			if _, stderr, err := run(t, append([]string{"project", "settings", command}, refusal.flags...)...); err == nil || stderr == "" {
				t.Fatal("the command accepted the refused edit")
			}
			for _, root := range []string{command, window} {
				if !bytes.Equal(mustRead(t, filepath.Join(root, project.DocumentName)), edited) {
					t.Fatal("a refused settings edit changed the project document")
				}
			}
		})
	}

	// A document a later release wrote is refused by both and replaced by
	// neither.
	later := []byte(`{"schema":"readmit-project/v99","settings":{"title":"Written later"}}` + "\n")
	for _, root := range []string{command, window} {
		if err := os.WriteFile(filepath.Join(root, project.DocumentName), later, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if result := save("later", desktop.ProjectDraft{Name: "Epic scheduling handover", Revisions: kept}); result.State != desktop.Failed ||
		result.Reason != "the project document was written by a version this release cannot read" {
		t.Fatalf("the window over a later document: %+v", result)
	}
	if _, stderr, err := run(t, "project", "settings", command, "--title", "Epic scheduling handover"); err == nil || stderr == "" {
		t.Fatal("the command changed a document a later release wrote")
	}
	for _, root := range []string{command, window} {
		if !bytes.Equal(mustRead(t, filepath.Join(root, project.DocumentName)), later) {
			t.Fatal("a document a later release wrote was replaced")
		}
	}
}

// The window reads a project's notes from the editable document `readmit
// project show` prints: each note with its text, about the case it names or
// about the project, and the overview names each revision's parent as the
// document records it.
func TestTheWindowReadsTheEditableDocumentProjectShowPrints(t *testing.T) {
	original, derived := redactedRevision(t)
	root := newProject(t)
	copyInto(t, root, "booking", original)
	copyInto(t, root, "booking-redacted", derived)
	for _, args := range [][]string{
		{"project", "add", root, "booking", "--title", "Duplicate appointment after reschedule"},
		{"project", "revise", root, "booking-redacted", "--parent", "booking"},
		{"project", "note", root, "handover", "--title", "Handover checklist", "--body", "Confirm the reschedule once.\nThen close the case."},
		{"project", "note", root, "why", "--subject", "booking", "--title", "Why it doubled"},
	} {
		if _, stderr, err := run(t, args...); err != nil || stderr != "" {
			t.Fatalf("%v: %v %s", args, err, stderr)
		}
	}
	app := desktopApp(t, t.TempDir())
	context := desktop.RequestContext{Project: root}
	unbound := app.ListNotes(desktop.NotesRequest{Context: context})
	cases := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if unbound.State != desktop.Completed || cases.Page == nil || len(cases.Page.Items) != 1 {
		t.Fatalf("the window's read of the project: %+v %+v", unbound, cases)
	}
	about := app.ListNotes(desktop.NotesRequest{Context: context, Case: &cases.Page.Items[0].Ref})
	recorded, err := project.ReadRevisions(root)
	if err != nil {
		t.Fatal(err)
	}
	shown, stderr, err := run(t, "project", "show", root)
	if err != nil || stderr != "" {
		t.Fatalf("project show: %v %s", err, stderr)
	}
	read := append(slices.Clone(unbound.Notes), about.Notes...)
	if len(read) != len(recorded.Notes) {
		t.Fatalf("the window read %d notes, the document records %d", len(read), len(recorded.Notes))
	}
	for _, note := range read {
		at := slices.IndexFunc(recorded.Notes, func(held project.Note) bool { return held.Name == note.ID })
		if at < 0 || recorded.Notes[at].Title != note.Name || recorded.Notes[at].Body != note.Content || (recorded.Notes[at].Subject != "") != (note.Case != nil) {
			t.Fatalf("the window read another note than the one recorded: %+v", note)
		}
		if !strings.Contains(shown, fmt.Sprintf("    title: %s\n", note.Name)) {
			t.Errorf("project show does not print the note the window read, %q:\n%s", note.Name, shown)
		}
	}
	if overview := app.OpenProjectOverview(root); overview.Overview == nil || len(overview.Overview.Revisions) != 1 ||
		overview.Overview.Revisions[0].Parent != recorded.Revisions[0].Operation.Parent {
		t.Fatalf("the overview does not list the revision the document records: %+v", overview)
	}

	// A document a later release wrote is refused by both and left as written.
	later := []byte(`{"schema":"readmit-revisions/v99","notes":[],"revisions":[]}` + "\n")
	if err := os.WriteFile(filepath.Join(root, project.RevisionsDocumentName), later, 0o600); err != nil {
		t.Fatal(err)
	}
	if refused := app.ListNotes(desktop.NotesRequest{Context: context}); refused.State != desktop.Failed || len(refused.Notes) != 0 ||
		refused.Reason != "the editable project document was written by a version this release cannot read" {
		t.Fatalf("the window over a later editable document: %+v", refused)
	}
	if _, stderr, err := run(t, "project", "show", root); err == nil || stderr == "" {
		t.Fatal("project show read a document a later release wrote")
	}
	if !bytes.Equal(mustRead(t, filepath.Join(root, project.RevisionsDocumentName)), later) {
		t.Fatal("a later editable document was replaced")
	}
}

// The saved filters the window lists and selects between draw exactly the
// occurrences `readmit index search` finds for the same question over the
// same index, no filter draws every occurrence, and a saved-filter document a
// later release wrote is refused by every filter operation and never
// replaced.
func TestSavedFiltersSelectExactlyWhatTheCommandLineIndexSearchFinds(t *testing.T) {
	workspace := t.TempDir()
	evidence := filepath.Join(workspace, "feed")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/listen-s12.hl7", "../testdata/fixtures/listen-s13.hl7", "--output", evidence); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	written := filepath.Join(workspace, "feed.index.json")
	if _, stderr, err := run(t, "index", "build", evidence, "--output", written, "--field", "MSH-9", "--retain", "values", "--retain-until", "indefinite"); err != nil || stderr != "" {
		t.Fatalf("index build: %v %s", err, stderr)
	}
	state := t.TempDir()
	app := stateApp(workspace, state)
	questions := map[string]string{"reschedules": "S13", "bookings": "S12"}
	for _, name := range []string{"reschedules", "bookings"} {
		saved := app.SaveFilter(grid.Filter{Name: name, Fields: []grid.FieldPredicate{{Selector: "MSH[1]-9[1]", Match: index.Contains, Term: questions[name]}}})
		if saved.State != desktop.Completed || saved.Selected != name {
			t.Fatalf("saving %s: %+v", name, saved)
		}
	}
	listed := app.Filters()
	if listed.State != desktop.Completed || listed.Selected != "bookings" || len(listed.Filters) != 2 ||
		listed.Filters[0].Name != "reschedules" || listed.Filters[1].Name != "bookings" {
		t.Fatalf("the saved filters listed: %+v", listed)
	}
	rendered := func() ([]string, *desktop.Grid) {
		result := app.OpenGrid(workspace, "feed", "feed.index.json", 0, 50)
		if result.State != desktop.Completed || result.Grid == nil {
			t.Fatalf("the grid did not open: %+v", result)
		}
		ids := []string{}
		for _, row := range result.Grid.Rows {
			ids = append(ids, row.ID)
		}
		return ids, result.Grid
	}
	for name, term := range questions {
		if selected := app.SelectFilter(name); selected.State != desktop.Completed || selected.Selected != name {
			t.Fatalf("selecting %s: %+v", name, selected)
		}
		stdout, stderr, err := run(t, "index", "search", evidence, written, "--field", "MSH-9", "--contains", term)
		if err != nil || stderr != "" {
			t.Fatalf("index search: %v %s", err, stderr)
		}
		ids, drawn := rendered()
		if found := reportedOccurrences(stdout); len(found) != 1 || !reflect.DeepEqual(ids, found) || drawn.Filter != name || drawn.Excluded != 1 {
			t.Fatalf("%s drew %v of %+v; the command line found %v", name, ids, drawn, found)
		}
	}
	if none := app.SelectFilter(""); none.State != desktop.Completed || none.Selected != "" {
		t.Fatalf("selecting no filter: %+v", none)
	}
	if ids, drawn := rendered(); !reflect.DeepEqual(ids, []string{"s0001-e000001", "s0002-e000001"}) || drawn.Excluded != 0 {
		t.Fatalf("no filter drew %v of %+v", ids, drawn)
	}
	if absent := app.SelectFilter("absent"); absent.State != desktop.Failed || absent.Selected != "" || absent.Reason != "that filter is not one this viewer has saved" {
		t.Fatalf("selecting a filter nobody saved: %+v", absent)
	}

	later := []byte(`{"schema":"readmit-filters/v2","filters":[],"selected":""}` + "\n")
	if err := os.WriteFile(filepath.Join(state, "filters.json"), later, 0o600); err != nil {
		t.Fatal(err)
	}
	const unreadable = "the saved filters were written by a version this release cannot read"
	for name, result := range map[string]desktop.FiltersResult{
		"list":   app.Filters(),
		"save":   app.SaveFilter(grid.Filter{Name: "later", Kinds: []bundle.EventKind{bundle.Message}}),
		"select": app.SelectFilter(""),
	} {
		if result.State != desktop.Failed || result.Reason != unreadable || len(result.Filters) != 0 {
			t.Errorf("%s over a later saved-filter document: %+v", name, result)
		}
	}
	if !bytes.Equal(mustRead(t, filepath.Join(state, "filters.json")), later) {
		t.Fatal("a saved-filter document a later release wrote was replaced")
	}
}

// The window's guided sample imports the frozen receiver fixtures through the
// operation `readmit sample capture` runs, without activation. Each bundle is
// exactly that operation's output at the instant it recorded as its import
// time, identity included, so the two differ in that instant and in nothing
// else; and both refuse bytes that are not the frozen fixtures in the same
// words, writing nothing.
func TestTheWindowsSampleCaptureIsTheCommandLinesCase(t *testing.T) {
	fixtures := t.TempDir()
	for _, name := range []string{"listen-s12.hl7", "listen-s13.hl7"} {
		if err := os.WriteFile(filepath.Join(fixtures, name), readFixture(t, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace := t.TempDir()
	stdout, err := rawOperation(t, "sample", "capture", "--fixtures", fixtures, "--output", filepath.Join(workspace, "command"))
	if err != nil {
		t.Fatalf("sample capture: %v %s", err, stdout)
	}
	app := stateApp(fixtures, t.TempDir())
	captured := app.CaptureSample(desktop.SampleCaptureRequest{Workspace: workspace, Output: "window"})
	if captured.State != desktop.Completed || captured.Case == nil {
		t.Fatalf("the window's sample capture: %+v", captured)
	}
	for _, want := range []string{
		"Schema: " + captured.Case.Schema, "Provenance: " + captured.Case.Provenance,
		fmt.Sprintf("Sources: %d", captured.Case.Sources), fmt.Sprintf("Occurrences: %d", captured.Case.Occurrences),
		fmt.Sprintf("Messages: %d", captured.Case.Messages), fmt.Sprintf("ACKs: %d", captured.Case.Acknowledgements),
	} {
		if !strings.Contains(stdout, want+"\n") {
			t.Errorf("the command reports another case than the window, missing %q:\n%s", want, stdout)
		}
	}

	instants := map[string]string{}
	for _, entry := range []string{"command", "window"} {
		written, err := bundle.Open(filepath.Join(workspace, entry))
		if err != nil {
			t.Fatal(err)
		}
		at := *written.Manifest.Provenance.ImportedAt
		again := filepath.Join(t.TempDir(), "again")
		if _, err := operation.CaptureSample(fixtures, again, at); err != nil {
			t.Fatal(err)
		}
		if a, b := treeOf(t, filepath.Join(workspace, entry)), treeOf(t, again); !reflect.DeepEqual(a, b) {
			t.Fatalf("the %s's case is not the shared operation's case at the instant it recorded", entry)
		}
		instants[entry] = at.Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	command, window := treeOf(t, filepath.Join(workspace, "command")), treeOf(t, filepath.Join(workspace, "window"))
	delete(command, "identity.sha256")
	delete(window, "identity.sha256")
	if len(command) != len(window) {
		t.Fatalf("the two cases hold different files: %d and %d", len(command), len(window))
	}
	for name, data := range command {
		if !bytes.Equal(bytes.ReplaceAll(data, []byte(instants["command"]), []byte(instants["window"])), window[name]) {
			t.Errorf("%s differs between the command's case and the window's beyond the import instant", name)
		}
	}
	if !strings.Contains(stdout, "Bundle: "+strings.TrimSpace(string(mustRead(t, filepath.Join(workspace, "command", "identity.sha256"))))+"\n") {
		t.Fatalf("the command reported another identity than it wrote:\n%s", stdout)
	}

	altered := t.TempDir()
	if err := os.WriteFile(filepath.Join(altered, "listen-s12.hl7"), []byte("MSH|^~\\&|ALTERED\r"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(altered, "listen-s13.hl7"), mustRead(t, filepath.Join(fixtures, "listen-s13.hl7")), 0o600); err != nil {
		t.Fatal(err)
	}
	before := treeOf(t, workspace)
	refusedWindow := stateApp(altered, t.TempDir()).CaptureSample(desktop.SampleCaptureRequest{Workspace: workspace, Output: "altered"})
	refusedCommand, err := rawOperation(t, "sample", "capture", "--fixtures", altered, "--output", filepath.Join(workspace, "altered-command"))
	if refusedWindow.State != desktop.Failed || err == nil || !strings.Contains(refusedCommand, refusedWindow.Reason) ||
		refusedWindow.Reason != operation.ErrSampleFixtureChanged.Error() {
		t.Fatalf("altered fixtures: the window %+v; the command %v %s", refusedWindow, err, refusedCommand)
	}
	if after := treeOf(t, workspace); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused sample capture wrote into the workspace")
	}
}
