package desktop_test

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
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

// chooser stands in for the host's native folder dialog. before runs while the
// dialog is notionally open, so a test can cancel or reenter deterministically.
type chooser struct {
	folder string
	err    error
	before func()
	titles []string
}

func (c *chooser) ChooseFolder(title string) (string, error) {
	c.titles = append(c.titles, title)
	if c.before != nil {
		c.before()
	}
	return c.folder, c.err
}

func newApp(t *testing.T, c *chooser) *desktop.App {
	t.Helper()
	return desktop.New(c, filepath.Join(t.TempDir(), "recent.json"))
}

// sample creates the sample workspace through the public facade and returns it.
func sample(t *testing.T, app *desktop.App) desktop.WorkspaceResult {
	t.Helper()
	result := app.CreateSampleWorkspace()
	if result.State != desktop.Completed || result.Workspace == nil {
		t.Fatalf("sample workspace: %+v", result)
	}
	if filepath.Base(result.Workspace.Root) != "readmit-sample" {
		t.Fatalf("sample workspace was not created in the chosen folder: %s", result.Workspace.Root)
	}
	return result
}

func TestSampleWorkspaceIsTheFrozenSyntheticFamilyAndOpensItsCases(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	result := sample(t, app)

	kinds := make(map[string]desktop.Artifact)
	for _, artifact := range result.Workspace.Artifacts {
		kinds[artifact.Name] = artifact
	}
	if len(kinds) != 4 {
		t.Fatalf("unexpected sample workspace listing: %+v", result.Workspace.Artifacts)
	}
	for name := range sampleIdentities {
		artifact, ok := kinds[name]
		if !ok || artifact.Kind != desktop.CaseArtifact || artifact.Schema != "readmit-case/v1" || artifact.Provenance != "generated" {
			t.Fatalf("sample case %q was not listed as generated case evidence: %+v", name, artifact)
		}
	}
	// The family completion record is not a case bundle. The listing says so
	// rather than hiding it or implying the shell understands it.
	if family := kinds["family.json"]; family.Kind != desktop.UnsupportedArtifact || family.Reason == "" {
		t.Fatalf("family record was not reported as unsupported: %+v", family)
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
	app := desktop.New(broken, filepath.Join(t.TempDir(), "recent.json"))
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
	app := desktop.New(cancelling, filepath.Join(t.TempDir(), "recent.json"))
	cancelling.before = app.Cancel
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
	app := desktop.New(reentrant, filepath.Join(t.TempDir(), "recent.json"))
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

func TestRecentWorkspacesRecordOpensMostRecentFirstWithinItsBound(t *testing.T) {
	store := filepath.Join(t.TempDir(), "recent.json")
	app := desktop.New(&chooser{}, store)
	if result := app.RecentWorkspaces(); result.State != desktop.Empty || len(result.Roots) != 0 {
		t.Fatalf("a shell that has opened nothing reported recent workspaces: %+v", result)
	}

	var opened []string
	for range desktop.MaxRecentWorkspaces + 3 {
		root := t.TempDir()
		if opening := app.OpenWorkspace(root); opening.State != desktop.Empty {
			t.Fatalf("open %s: %+v", root, opening)
		}
		opened = append(opened, resolved(t, root))
	}
	recent := app.RecentWorkspaces()
	if recent.State != desktop.Completed || len(recent.Roots) != desktop.MaxRecentWorkspaces {
		t.Fatalf("recent workspaces are unbounded or missing: %+v", recent)
	}
	want := opened[len(opened)-desktop.MaxRecentWorkspaces:]
	for i := range want {
		if recent.Roots[i] != want[len(want)-1-i] {
			t.Fatalf("recent workspaces are not most-recent-first: %v want reverse of %v", recent.Roots, want)
		}
	}

	// Reopening an already-recorded workspace moves it to the front without
	// duplicating it, and a fresh shell reads the same list back from disk.
	if reopening := app.OpenWorkspace(want[0]); reopening.State != desktop.Empty {
		t.Fatalf("reopen: %+v", reopening)
	}
	reopened := desktop.New(&chooser{}, store).RecentWorkspaces()
	if len(reopened.Roots) != desktop.MaxRecentWorkspaces || reopened.Roots[0] != recent.Roots[desktop.MaxRecentWorkspaces-1] {
		t.Fatalf("reopening did not move the workspace to the front: %+v", reopened.Roots)
	}
	if duplicates := len(reopened.Roots) - len(unique(reopened.Roots)); duplicates != 0 {
		t.Fatalf("recent workspaces contain %d duplicates: %v", duplicates, reopened.Roots)
	}
}

func TestRecentStoreRefusesUnknownVersionsAndMembers(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown version": `{"schema":"readmit-desktop-recent/v2","roots":["/tmp"]}`,
		"unknown member":  `{"schema":"readmit-desktop-recent/v1","roots":["/tmp"],"last_opened":"2026-01-01"}`,
		"relative root":   `{"schema":"readmit-desktop-recent/v1","roots":["relative"]}`,
		"not JSON":        "{",
	} {
		store := filepath.Join(t.TempDir(), "recent.json")
		if err := os.WriteFile(store, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		app := desktop.New(&chooser{}, store)
		if result := app.RecentWorkspaces(); result.State != desktop.Failed || len(result.Roots) != 0 {
			t.Fatalf("the recent store accepted an %s: %+v", name, result)
		}
		// An unreadable store is never silently replaced: opening a workspace
		// still succeeds, and the stored bytes are left for the person to fix.
		if opened := app.OpenWorkspace(t.TempDir()); opened.State != desktop.Empty {
			t.Fatalf("an unreadable recent store blocked opening a workspace: %+v", opened)
		}
		data, err := os.ReadFile(store)
		if err != nil || string(data) != contents {
			t.Fatalf("the shell overwrote an unreadable recent store: %q", data)
		}
	}
}

func TestRecentStoreIsWrittenCompletelyAndPrivately(t *testing.T) {
	store := filepath.Join(t.TempDir(), "state", "recent.json")
	app := desktop.New(&chooser{}, store)
	root := t.TempDir()
	if result := app.OpenWorkspace(root); result.State != desktop.Empty {
		t.Fatalf("open: %+v", result)
	}
	data, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Schema string   `json:"schema"`
		Roots  []string `json:"roots"`
	}
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != desktop.RecentSchema || !reflect.DeepEqual(decoded.Roots, []string{resolved(t, root)}) {
		t.Fatalf("unexpected recent store contents: %s", data)
	}
	entries, err := os.ReadDir(filepath.Dir(store))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "recent.json" {
		t.Fatalf("the recent store left partial files behind: %v", entries)
	}
}

func TestDefaultRecentPathStaysInsideTheUserConfigurationDirectory(t *testing.T) {
	path, err := desktop.DefaultRecentPath()
	if err != nil {
		t.Skipf("this host has no user configuration directory: %v", err)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(config, "readmit", "recent.json"); path != want {
		t.Fatalf("recent store path %q is outside %q", path, want)
	}
}

func resolved(t *testing.T, root string) string {
	t.Helper()
	target, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	var distinct []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			distinct = append(distinct, value)
		}
	}
	return distinct
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
	app := desktop.New(reentrant, filepath.Join(t.TempDir(), "recent.json"))
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

// writeProject puts a project document into folder and returns the folder. The
// document is authored here, not produced by the facade, so these tests fail if
// the facade ever rewrites what a project recorded.
func writeProject(t *testing.T, folder string, cases string) string {
	t.Helper()
	document := `{"schema":"readmit-project/v1","settings":{"title":"Epic scheduling interface",` +
		`"default_owner":"integration-team","default_interface_version":"siu-2.5.1-v1"},` +
		`"interface_versions":["siu-2.5.1-v1"],"cases":[` + cases + "]}\n"
	if err := os.WriteFile(filepath.Join(folder, "project.json"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return folder
}

// registeredRegression is one case entry naming the frozen reference identity.
const registeredRegression = `{"name":"regression","identity":"7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",` +
	`"schema":"readmit-case/v1","provenance":"generated","interface_version":"siu-2.5.1-v1",` +
	`"title":"Duplicate appointment after reschedule","status":"investigating","owner":"scheduling-team",` +
	`"tags":["duplicate","scheduling"],"incidents":["INC-4821"]}`

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
	for name, folder := range map[string]string{
		"no document":      t.TempDir(),
		"absent folder":    filepath.Join(t.TempDir(), "absent"),
		"unknown contract": "",
	} {
		if name == "unknown contract" {
			folder = t.TempDir()
			if err := os.WriteFile(filepath.Join(folder, "project.json"), []byte(`{"schema":"readmit-project/v2"}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		result := newApp(t, &chooser{}).OpenProject(folder)
		if result.State != desktop.Failed || result.Project != nil {
			t.Fatalf("%s was not reported as a failure: %+v", name, result)
		}
		if result.Reason == "" {
			t.Fatalf("%s gave the shell nothing to show", name)
		}
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

	if err := os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"schema":"readmit-project/v2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	unsupported := newApp(t, &chooser{}).OpenWorkspace(root).Workspace.Artifacts[0]
	if unsupported.Kind != desktop.UnsupportedArtifact || unsupported.Reason == "" || unsupported.Schema != "" {
		t.Fatalf("an unreadable project document was not reported as unsupported: %+v", unsupported)
	}
}

func TestOpenProjectHoldsTheSameOperationSlot(t *testing.T) {
	root := writeProject(t, t.TempDir(), registeredRegression)
	reentrant := &chooser{folder: root}
	app := desktop.New(reentrant, filepath.Join(t.TempDir(), "recent.json"))
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
