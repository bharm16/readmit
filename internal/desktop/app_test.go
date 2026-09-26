package desktop_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/project"
)

// Independently authored identities for the frozen readmit-synth-v1 reference
// vector, quoted from docs/synth-v1-vector.md. They were calculated from the
// literal fixtures with Python, never by running readmit's generator.
var sampleIdentities = map[string]string{
	"regression":   "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",
	"cancellation": "96077b34226faa19325f01f3fbb0d728de88644c22ba8428659c883703d3f438",
	"invalid":      "ab6d014aa0fc9e2ed9cba7160e73bba17b8753f6a7ca5c3a8618f3ec8ba095a9",
}

// chooser, activatedApp, newApp, sample and the other generic verbs these tests
// share live in harness_test.go.

func TestSampleWorkspaceIsTheFrozenSyntheticFamilyAndOpensItsCases(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	result := sample(t, app)

	kinds := make(map[string]desktop.Artifact)
	for _, artifact := range result.Workspace.Artifacts {
		kinds[artifact.Name] = artifact
	}
	if len(kinds) != 5 {
		t.Fatalf("unexpected sample workspace listing: %+v", result.Workspace.Artifacts)
	}
	for name := range sampleIdentities {
		artifact, ok := kinds[name]
		if !ok || artifact.Kind != desktop.CaseArtifact || artifact.Schema != "readmit-case/v1" || artifact.Provenance != "generated" {
			t.Fatalf("sample case %q was not listed as generated case evidence: %+v", name, artifact)
		}
	}
	// The practice endpoint a test names and the index the grid reads are a
	// configuration and a derived document, not case bundles. The listing
	// names what each declares — the environment and the index that the
	// applicable pickers offer — rather than hiding them or implying the shell
	// opens them as evidence.
	for name, want := range map[string]desktop.Kind{
		guide.TargetName: desktop.TargetArtifact,
		guide.IndexName:  desktop.IndexArtifact,
	} {
		if entry := kinds[name]; entry.Kind != want {
			t.Fatalf("%s was not reported as %s: %+v", name, want, entry)
		}
	}
	// The sample is a workspace rather than a family: a directory carrying the
	// family completion record is retained evidence nothing may be written
	// inside, and the guided sample is saved and run inside this one.
	if _, err := os.Lstat(filepath.Join(result.Workspace.Root, "family.json")); !os.IsNotExist(err) {
		t.Fatal("the sample workspace carries a family completion record and cannot be written in")
	}

	for name, identity := range sampleIdentities {
		opened := app.OpenCase(result.Workspace.Root, name)
		if opened.State != desktop.Completed || opened.Case == nil {
			t.Fatalf("open %q: %+v", name, opened)
		}
		if opened.Case.Identity != identity {
			t.Fatalf("case %q identity %s does not match the frozen vector %s", name, opened.Case.Identity, identity)
		}
		if opened.Case.Schema != "readmit-case/v1" || opened.Case.Provenance != "generated" || opened.Case.Sources != 1 {
			t.Fatalf("case %q lost its declared contract: %+v", name, opened.Case)
		}
	}
	if regression := app.OpenCase(result.Workspace.Root, "regression"); regression.Case.Messages != 2 || regression.Case.Occurrences != 2 || regression.Case.Acknowledgements != 0 || regression.Case.Unparsed != 0 {
		t.Fatalf("regression case counts changed: %+v", regression.Case)
	}
}

func TestOpenedCaseCarriesNoMessageContent(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sample(t, app).Workspace.Root
	opened := app.OpenCase(root, "regression")
	encoded, err := json.Marshal(opened)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"MSH", "SCH", "PID", "SYNTH-", "READMIT"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("the typed case result exposed message content %q: %s", leaked, encoded)
		}
	}
}

func TestSampleWorkspaceRefusesAnExistingDestination(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	sample(t, app)
	again := app.CreateSampleWorkspace()
	if again.State != desktop.Failed || again.Reason == "" || again.Workspace != nil {
		t.Fatalf("a second sample overwrote or silently reused existing evidence: %+v", again)
	}
	// Recovery: the facade is usable again and a different folder still works.
	app = newApp(t, &chooser{folder: t.TempDir()})
	if result := app.CreateSampleWorkspace(); result.State != desktop.Completed {
		t.Fatalf("the facade did not recover after a refused destination: %+v", result)
	}
}

func TestOpenWorkspaceReportsEmptyMissingAndNonDirectoryFolders(t *testing.T) {
	app := newApp(t, &chooser{})
	empty := t.TempDir()
	result := app.OpenWorkspace(empty)
	if result.State != desktop.Empty || result.Workspace == nil || len(result.Workspace.Artifacts) != 0 {
		t.Fatalf("an empty folder was not reported as empty: %+v", result)
	}

	missing := app.OpenWorkspace(filepath.Join(empty, "absent"))
	if missing.State != desktop.Failed || missing.Workspace != nil {
		t.Fatalf("a missing folder was not reported as failed: %+v", missing)
	}

	file := filepath.Join(empty, "evidence.hl7")
	if err := os.WriteFile(file, []byte("MSH|^~\\&|APP\r"), 0600); err != nil {
		t.Fatal(err)
	}
	if notDirectory := app.OpenWorkspace(file); notDirectory.State != desktop.Failed {
		t.Fatalf("a file was accepted as a workspace: %+v", notDirectory)
	}
}

func TestWorkspaceListingDeclaresAndNeverVerifies(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sample(t, app).Workspace.Root
	if err := os.WriteFile(filepath.Join(root, "regression", "identity.sha256"), []byte("0000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	listing := app.OpenWorkspace(root)
	if listing.State != desktop.Completed {
		t.Fatalf("listing a folder holding damaged evidence failed: %+v", listing)
	}
	// Opening is the verification step, and it must refuse the damaged case.
	opened := app.OpenCase(root, "regression")
	if opened.State != desktop.Failed || opened.Case != nil {
		t.Fatalf("damaged evidence was opened as a verified case: %+v", opened)
	}
}

func TestOpenCaseRefusesNamesOutsideTheWorkspace(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sample(t, app).Workspace.Root
	for _, name := range []string{"", ".", "..", "../readmit-sample/regression", "regression/payloads", filepath.Join(root, "regression"), "absent"} {
		if result := app.OpenCase(root, name); result.State != desktop.Failed || result.Case != nil {
			t.Fatalf("OpenCase accepted %q: %+v", name, result)
		}
	}
	if result := app.OpenCase(filepath.Join(parent, "absent"), "regression"); result.State != desktop.Failed {
		t.Fatalf("OpenCase accepted a workspace that does not exist: %+v", result)
	}
}

func TestSelectWorkspaceReportsDismissedAndFailedDialogs(t *testing.T) {
	dismissed := newApp(t, &chooser{folder: ""})
	if result := dismissed.SelectWorkspace(); result.State != desktop.Cancelled || result.Workspace != nil {
		t.Fatalf("a dismissed folder dialog was not reported as cancelled: %+v", result)
	}

	broken := &chooser{err: errors.New("dialog unavailable")}
	app := activatedApp(t, broken, t.TempDir())
	result := app.SelectWorkspace()
	if result.State != desktop.Failed || result.Workspace != nil {
		t.Fatalf("a failed folder dialog was not reported as failed: %+v", result)
	}
	if strings.Contains(result.Reason, "dialog unavailable") {
		t.Fatalf("the facade echoed a host diagnostic to the shell: %q", result.Reason)
	}
	if len(broken.titles) != 1 || broken.titles[0] == "" {
		t.Fatalf("the folder dialog was not given a title: %+v", broken.titles)
	}
}

func TestCancelStopsTheRunningOperationAndTheFacadeRecovers(t *testing.T) {
	parent := t.TempDir()
	preparation := newApp(t, &chooser{folder: parent})
	root := sample(t, preparation).Workspace.Root

	cancelling := &chooser{folder: root}
	app := activatedApp(t, cancelling, t.TempDir())
	cancelling.before = func() { app.Cancel("") }
	if result := app.SelectWorkspace(); result.State != desktop.Cancelled || result.Workspace != nil {
		t.Fatalf("a cancelled open reported a workspace: %+v", result)
	}
	// Recovery: cancelling one operation does not disable the next.
	if result := app.OpenWorkspace(root); result.State != desktop.Completed {
		t.Fatalf("the facade did not recover after cancellation: %+v", result)
	}
}

func TestOnlyOneOperationRunsAtATime(t *testing.T) {
	parent := t.TempDir()
	preparation := newApp(t, &chooser{folder: parent})
	root := sample(t, preparation).Workspace.Root

	reentrant := &chooser{folder: root}
	app := activatedApp(t, reentrant, t.TempDir())
	var concurrent desktop.WorkspaceResult
	reentrant.before = func() { concurrent = app.OpenWorkspace(root) }
	if result := app.SelectWorkspace(); result.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", result)
	}
	if concurrent.State != desktop.Busy || concurrent.Workspace != nil {
		t.Fatalf("a second operation ran while the first held the facade: %+v", concurrent)
	}
	// Recovery: the slot is released once the first operation finishes.
	if result := app.OpenWorkspace(root); result.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", result)
	}
}

func TestConcurrentCallersNeverShareAnOperation(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sample(t, app).Workspace.Root
	var wait sync.WaitGroup
	states := make([]desktop.State, 8)
	for i := range states {
		wait.Add(1)
		go func() {
			defer wait.Done()
			states[i] = app.OpenWorkspace(root).State
		}()
	}
	wait.Wait()
	for i, state := range states {
		if state != desktop.Completed && state != desktop.Busy {
			t.Fatalf("concurrent open %d reported %q", i, state)
		}
	}
}

func TestDefaultShellDocumentsStayInsideTheUserConfigurationDirectory(t *testing.T) {
	documents, err := desktop.DefaultShellDocuments()
	if err != nil {
		t.Skipf("this host has no user configuration directory: %v", err)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(config, "readmit"); documents.Folder != want {
		t.Fatalf("the shell document store is at %q, outside %q", documents.Folder, want)
	}
}

func TestSampleWorkspaceRefusesRetainedOutputWithoutReusingIt(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	// An interrupted attempt retains incomplete output rather than deleting it.
	// The next attempt must refuse that folder, leave it exactly as it is, and
	// say how to continue. It is never completed, reused, or overwritten.
	retained := filepath.Join(parent, desktop.SampleName)
	if err := os.Mkdir(retained, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(retained, "manifest.json"), []byte(`{"schema":"readmit-case/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	result := app.CreateSampleWorkspace()
	if result.State != desktop.Failed || result.Workspace != nil {
		t.Fatalf("retained output was reused as a sample workspace: %+v", result)
	}
	if !strings.Contains(result.Reason, "choose a different folder") {
		t.Fatalf("the refusal does not say how to continue: %q", result.Reason)
	}
	entries, err := os.ReadDir(retained)
	if err != nil || len(entries) != 1 || entries[0].Name() != "manifest.json" {
		t.Fatalf("the refused attempt changed retained output: %v %v", entries, err)
	}
	if recovered := newApp(t, &chooser{folder: t.TempDir()}).CreateSampleWorkspace(); recovered.State != desktop.Completed {
		t.Fatalf("no recovery was possible after retained output: %+v", recovered)
	}
}

func TestOpenCaseHoldsTheSameOperationSlot(t *testing.T) {
	parent := t.TempDir()
	preparation := newApp(t, &chooser{folder: parent})
	root := sample(t, preparation).Workspace.Root

	reentrant := &chooser{folder: root}
	app := activatedApp(t, reentrant, t.TempDir())
	var concurrent desktop.CaseResult
	reentrant.before = func() { concurrent = app.OpenCase(root, "regression") }
	if result := app.SelectWorkspace(); result.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", result)
	}
	if concurrent.State != desktop.Busy || concurrent.Case != nil {
		t.Fatalf("a case was verified while another operation held the facade: %+v", concurrent)
	}
	// Recovery: the slot is released and verification works immediately after.
	if opened := app.OpenCase(root, "regression"); opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", opened)
	}
}

func TestOpenProjectReportsTheRecordedDocument(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	result := newApp(t, &chooser{}).OpenProject(root)
	if result.State != desktop.Completed || result.Project == nil {
		t.Fatalf("a project document was not opened: %+v", result)
	}
	if result.Project.Settings.Title != "Epic scheduling interface" || len(result.Project.InterfaceVersions) != 1 {
		t.Fatalf("the project settings did not reach the shell: %+v", result.Project)
	}
	entry := result.Project.Cases[0]
	if entry.Identity != sampleIdentities["regression"] {
		t.Fatalf("the shell reports identity %s for a case the project recorded as %s", entry.Identity, sampleIdentities["regression"])
	}
	if entry.Status != project.StatusInvestigating || entry.Owner != "scheduling-team" || entry.Title == "" {
		t.Fatalf("the managed metadata did not reach the shell: %+v", entry)
	}
	if !reflect.DeepEqual(entry.Tags, []string{"duplicate", "scheduling"}) || !reflect.DeepEqual(entry.Incidents, []string{"INC-4821"}) {
		t.Fatalf("tags or linked incidents did not reach the shell: %+v", entry)
	}
}

func TestOpenProjectSeparatesAnEmptyProjectFromAFailure(t *testing.T) {
	empty := newApp(t, &chooser{}).OpenProject(writeProject(t, t.TempDir(), ""))
	if empty.State != desktop.Empty || empty.Project == nil {
		t.Fatalf("a project with no registered case was not reported as empty: %+v", empty)
	}
	// A later version bumps the contract because it carries members this
	// release has never seen. That is the shape the reader must still report as
	// a version it cannot read, rather than as an invalid document.
	unsupported := t.TempDir()
	later := `{"schema":"readmit-project/v3","settings":{"title":"t"},"interface_versions":["a"],"cases":[],"suites":[]}`
	if err := os.WriteFile(filepath.Join(unsupported, "project.json"), []byte(later), 0600); err != nil {
		t.Fatal(err)
	}
	reasons := make(map[string]string)
	for name, folder := range map[string]string{
		"no document":      t.TempDir(),
		"absent folder":    filepath.Join(t.TempDir(), "absent"),
		"unknown contract": unsupported,
	} {
		result := newApp(t, &chooser{}).OpenProject(folder)
		if result.State != desktop.Failed || result.Project != nil {
			t.Fatalf("%s was not reported as a failure: %+v", name, result)
		}
		if result.Reason == "" {
			t.Fatalf("%s gave the shell nothing to show", name)
		}
		reasons[name] = result.Reason
	}
	// A document this release cannot read is not the same as no document, and
	// the shell is told which one it found rather than the two being conflated.
	if reasons["unknown contract"] == reasons["no document"] {
		t.Fatalf("an unsupported contract version was reported as a missing project: %q", reasons["unknown contract"])
	}
}

// A project document is listed as what it declares, exactly as a case bundle
// directory is. It is never presented as evidence, and a document this release
// cannot read is reported rather than hidden.
func TestWorkspaceListsAProjectDocumentByItsDeclaredContract(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	listed := newApp(t, &chooser{}).OpenWorkspace(root)
	if listed.State != desktop.Completed || listed.Workspace == nil || len(listed.Workspace.Artifacts) != 1 {
		t.Fatalf("the project folder was not listed: %+v", listed)
	}
	artifact := listed.Workspace.Artifacts[0]
	if artifact.Kind != desktop.ProjectArtifact || artifact.Schema != project.Schema || artifact.Reason != "" {
		t.Fatalf("the project document was not listed by its declared contract: %+v", artifact)
	}

	later := `{"schema":"readmit-project/v3","settings":{"title":"t"},"interface_versions":["a"],"cases":[],"suites":[]}`
	if err := os.WriteFile(filepath.Join(root, "project.json"), []byte(later), 0600); err != nil {
		t.Fatal(err)
	}
	unsupported := newApp(t, &chooser{}).OpenWorkspace(root).Workspace.Artifacts[0]
	if unsupported.Kind != desktop.UnsupportedArtifact || unsupported.Reason == "" || unsupported.Schema != "" {
		t.Fatalf("an unreadable project document was not reported as unsupported: %+v", unsupported)
	}
	// The listing says which kind of unreadable it found, as opening does.
	if err := os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"schema":`), 0600); err != nil {
		t.Fatal(err)
	}
	damaged := newApp(t, &chooser{}).OpenWorkspace(root).Workspace.Artifacts[0]
	if damaged.Reason == unsupported.Reason {
		t.Fatalf("a damaged document and a later version were reported the same way: %q", damaged.Reason)
	}
}

func TestOpenProjectHoldsTheSameOperationSlot(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	reentrant := &chooser{folder: root}
	app := activatedApp(t, reentrant, t.TempDir())
	var concurrent desktop.ProjectResult
	reentrant.before = func() { concurrent = app.OpenProject(root) }
	if result := app.SelectWorkspace(); result.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", result)
	}
	if concurrent.State != desktop.Busy || concurrent.Project != nil {
		t.Fatalf("a project was opened while another operation held the facade: %+v", concurrent)
	}
	if opened := app.OpenProject(root); opened.State != desktop.Completed || opened.Project == nil {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", opened)
	}
}

// The shell shows a project's own metadata. No message content reaches it.
func TestOpenedProjectCarriesNoMessageContent(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	encoded, err := json.Marshal(newApp(t, &chooser{}).OpenProject(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"MSH", "SCH", "PID", "SYNTH-", "READMIT"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("the typed project result exposed message content %q: %s", leaked, encoded)
		}
	}
}

// sampleProject creates a project directory holding a project document and a
// copy of the frozen regression case. The sample family itself is retained
// synthetic evidence, so a project is never created inside it; a bundle
// identity covers relative paths and contents only, so the copy keeps the
// identity the project document records.
func sampleProject(t *testing.T, app *desktop.App) string {
	t.Helper()
	family := sample(t, app).Workspace.Root
	root := writeProject(t, t.TempDir(), registeredRegression)
	if err := os.CopyFS(filepath.Join(root, "regression"), os.DirFS(filepath.Join(family, "regression"))); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestListNotesSeparatesAnEmptyDocumentFromAFailure(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	empty := newApp(t, &chooser{}).ListNotes(desktop.NotesRequest{Context: desktop.RequestContext{Project: root}})
	if empty.State != desktop.Empty || empty.Notes == nil || len(empty.Notes) != 0 {
		t.Fatalf("a project that has recorded nothing was not reported as empty: %+v", empty)
	}
	if _, err := os.Lstat(filepath.Join(root, project.RevisionsDocumentName)); err == nil {
		t.Fatal("reading a project wrote an editable document into it")
	}

	later := `{"schema":"readmit-revisions/v2","notes":[],"revisions":[],"suites":[]}`
	unsupported := writeProject(t, t.TempDir(), registeredRegression)
	if err := os.WriteFile(filepath.Join(unsupported, project.RevisionsDocumentName), []byte(later), 0600); err != nil {
		t.Fatal(err)
	}
	damaged := writeProject(t, t.TempDir(), registeredRegression)
	if err := os.WriteFile(filepath.Join(damaged, project.RevisionsDocumentName), []byte(`{"schema":`), 0600); err != nil {
		t.Fatal(err)
	}
	reasons := make(map[string]string)
	for name, folder := range map[string]string{
		"unknown contract": unsupported,
		"damaged document": damaged,
		"no project":       t.TempDir(),
	} {
		result := newApp(t, &chooser{}).ListNotes(desktop.NotesRequest{Context: desktop.RequestContext{Project: folder}})
		if result.State != desktop.Failed || len(result.Notes) != 0 {
			t.Fatalf("%s was not reported as a failure: %+v", name, result)
		}
		if result.Reason == "" {
			t.Fatalf("%s gave the shell nothing to show", name)
		}
		reasons[name] = result.Reason
	}
	if reasons["unknown contract"] == reasons["damaged document"] {
		t.Fatalf("a version this release cannot read was reported as a damaged one: %q", reasons["unknown contract"])
	}
	// A document this release cannot read is left exactly as it was written.
	kept, err := os.ReadFile(filepath.Join(unsupported, project.RevisionsDocumentName))
	if err != nil || string(kept) != later {
		t.Fatalf("an unreadable editable document was rewritten: %v %s", err, kept)
	}
}

// A note is working text stored in the project's own editable document,
// through the operation `readmit project note` stores it with. No byte of any
// retained artifact is touched, and nothing the project recorded is rewritten.
func TestSaveNoteEditsWorkingTextAndNeverTouchesEvidence(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root := sampleProject(t, app)
	evidence := bytesUnder(t, filepath.Join(root, "regression"))
	document, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	context := desktop.RequestContext{Project: root}
	regression := caseAt(t, app, context, "regression")

	saved := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "note-1",
		Note: desktop.NoteInput{Name: "Working theory", Content: "The second S13 keeps the original filler identifier.", Case: &regression.Ref}})
	if saved.State != desktop.Completed || saved.Saved == nil || len(saved.Notes) != 1 {
		t.Fatalf("a note was not stored: %+v", saved)
	}
	stored, err := project.ReadRevisions(root)
	if err != nil || len(stored.Notes) != 1 || stored.Notes[0].Name != saved.Saved.ID || stored.Notes[0].Subject != "regression" ||
		stored.Notes[0].Title != "Working theory" {
		t.Fatalf("the stored note is not the one that was written: %+v %v", stored, err)
	}
	replaced := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "note-2",
		Note: desktop.NoteInput{ID: saved.Saved.ID, Name: "Confirmed", Content: "Reproduced against the fixed receiver.", Case: &regression.Ref}})
	if replaced.State != desktop.Completed || len(replaced.Notes) != 1 || replaced.Notes[0].Name != "Confirmed" {
		t.Fatalf("replacing a note did not replace exactly that note: %+v", replaced)
	}
	if reopened, err := project.ReadRevisions(root); err != nil || len(reopened.Notes) != 1 {
		t.Fatalf("the stored note did not read back: %+v", reopened)
	}

	for name, note := range map[string]desktop.NoteInput{
		"no name":                             {Content: "text"},
		"a body carrying a control character": {Name: "Escaped", Content: "one\ttwo"},
		"a note that is no longer held":       {ID: "gone", Name: "Gone"},
	} {
		result := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "refused-" + strings.ReplaceAll(name, " ", "-"), Note: note})
		if result.State != desktop.Failed || result.Saved != nil || result.Reason == "" {
			t.Fatalf("%s was stored: %+v", name, result)
		}
	}

	// No edit above reached the evidence the project organizes, and none of
	// them rewrote what the project itself recorded.
	if !reflect.DeepEqual(bytesUnder(t, filepath.Join(root, "regression")), evidence) {
		t.Fatal("editing a note rewrote retained case evidence")
	}
	after, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil || !reflect.DeepEqual(after, document) {
		t.Fatal("editing a note rewrote the project document")
	}
	final, err := project.ReadRevisions(root)
	if err != nil || len(final.Notes) != 1 || final.Notes[0].Title != "Confirmed" {
		t.Fatalf("a refused edit changed the stored document: %+v %v", final, err)
	}
}

// An editable project document is listed as what it declares, exactly as the
// project document beside it is.
func TestWorkspaceListsTheEditableProjectDocumentByItsDeclaredContract(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	if err := project.WriteRevisions(root, project.Revisions{Schema: project.RevisionsSchema, Notes: []project.Note{{Name: "triage", Title: "Working theory"}}}); err != nil {
		t.Fatal(err)
	}
	listed := newApp(t, &chooser{}).OpenWorkspace(root)
	if listed.State != desktop.Completed || listed.Workspace == nil || len(listed.Workspace.Artifacts) != 2 {
		t.Fatalf("the project folder was not listed: %+v", listed)
	}
	kinds := make(map[string]desktop.Artifact)
	for _, artifact := range listed.Workspace.Artifacts {
		kinds[artifact.Name] = artifact
	}
	editable := kinds[project.RevisionsDocumentName]
	if editable.Kind != desktop.RevisionsArtifact || editable.Schema != project.RevisionsSchema || editable.Reason != "" {
		t.Fatalf("the editable document was not listed by its declared contract: %+v", editable)
	}

	later := `{"schema":"readmit-revisions/v2","notes":[],"revisions":[],"suites":[]}`
	if err := os.WriteFile(filepath.Join(root, project.RevisionsDocumentName), []byte(later), 0600); err != nil {
		t.Fatal(err)
	}
	unsupported := newApp(t, &chooser{}).OpenWorkspace(root)
	for _, artifact := range unsupported.Workspace.Artifacts {
		if artifact.Name != project.RevisionsDocumentName {
			continue
		}
		if artifact.Kind != desktop.UnsupportedArtifact || artifact.Reason == "" || artifact.Schema != "" {
			t.Fatalf("an unreadable editable document was not reported as unsupported: %+v", artifact)
		}
	}
}

// A project holds a bounded number of notes. One past the bound is refused
// rather than stored, and the document already there is left as it was.
func TestSaveNoteRefusesMoreNotesThanThisReleaseStores(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	full := project.Revisions{Schema: project.RevisionsSchema}
	for i := range project.MaxNotes {
		full.Notes = append(full.Notes, project.Note{Name: fmt.Sprintf("note-%03d", i), Title: "Draft"})
	}
	if err := project.WriteRevisions(root, full); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(root, project.RevisionsDocumentName))
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(t, &chooser{})
	context := desktop.RequestContext{Project: root}
	if result := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "one-too-many", Note: desktop.NoteInput{Name: "Draft"}}); result.State != desktop.Failed || result.Reason == "" {
		t.Fatalf("a note past the bound was stored: %+v", result)
	}
	after, err := os.ReadFile(filepath.Join(root, project.RevisionsDocumentName))
	if err != nil || !reflect.DeepEqual(after, stored) {
		t.Fatal("a refused note rewrote the document that was already there")
	}
	// Replacing a note that is already held stays within the bound.
	if result := app.SaveNoteItem(desktop.NoteSaveRequest{Context: context, IntentID: "replace", Note: desktop.NoteInput{ID: "note-000", Name: "Confirmed"}}); result.State != desktop.Completed {
		t.Fatalf("replacing a note was refused at the bound: %+v", result)
	}
}

func TestSaveNoteHoldsTheSameOperationSlot(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	reentrant := &chooser{folder: root}
	app := activatedApp(t, reentrant, t.TempDir())
	var concurrent desktop.NotesResult
	note := desktop.NoteSaveRequest{Context: desktop.RequestContext{Project: root}, IntentID: "triage", Note: desktop.NoteInput{Name: "Working theory"}}
	reentrant.before = func() { concurrent = app.SaveNoteItem(note) }
	if result := app.SelectWorkspace(); result.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", result)
	}
	if concurrent.State != desktop.Busy || concurrent.Saved != nil {
		t.Fatalf("a note was written while another operation held the facade: %+v", concurrent)
	}
	if _, err := os.Lstat(filepath.Join(root, project.RevisionsDocumentName)); err == nil {
		t.Fatal("a refused operation wrote an editable document anyway")
	}
	if saved := app.SaveNoteItem(note); saved.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", saved)
	}
}
