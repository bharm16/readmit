package desktop_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/project"
)

const sampleImportHL7 = "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||ADT^A01|MSG001|P|2.5.1\rPID|||12345||DOE^JOHN|||M\r"

func validDesktopPlan() importer.Plan {
	return importer.Plan{
		Schema:     importer.PlanSchema,
		Framing:    importer.RawFraming,
		Terminator: hl7.CR,
		Encoding:   importer.UTF8,
		Direction:  bundle.Inbound,
		Members:    []string{".hl7"},
	}
}

// mllpPlan is validDesktopPlan declaring MLLP-framed US-ASCII .mllp members,
// the way the larger imports below are written.
func mllpPlan() importer.Plan {
	plan := validDesktopPlan()
	plan.Framing, plan.Encoding, plan.Members = importer.MLLPFraming, importer.USASCII, []string{".mllp"}
	return plan
}

// mllpSource writes one MLLP source of repeated bookings. Importing it writes
// one synced payload file per occurrence, so its size sets how long the case
// write an interruption test lands in takes.
func mllpSource(t *testing.T, occurrences int) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "interface.mllp")
	if err := os.WriteFile(source, []byte(strings.Repeat(framed(gridBooking), occurrences)), 0600); err != nil {
		t.Fatal(err)
	}
	return source
}

// importProject is a project folder that registers no case yet.
func importProject(t *testing.T) string {
	t.Helper()
	folder := writeProject(t, t.TempDir(), "")
	if err := project.WriteRevisions(folder, project.Revisions{Schema: project.RevisionsSchema, Revisions: []project.Revision{}, Notes: []project.Note{}}); err != nil {
		t.Fatal(err)
	}
	return folder
}

// previewedImport previews one MLLP source as an import of the project and
// answers the import that would register it as a case named name, under
// intent.
func previewedImport(t *testing.T, app *desktop.App, folder, name, source, intent string) desktop.ImportCaseRequest {
	t.Helper()
	plan := mllpPlan()
	context := desktop.RequestContext{Project: folder}
	request := desktop.ImportRequest{Context: context, Mode: "plan", Plan: &plan, Files: []string{source}}
	preview := app.PreviewImport(request)
	if preview.State != desktop.Completed {
		t.Fatalf("preview %s: %+v", name, preview)
	}
	return desktop.ImportCaseRequest{Context: context, Name: name, Source: request, PreviewToken: preview.PreviewToken, IntentID: intent}
}

// incomingPayloads counts the payloads the project's import area holds, in
// every case being written there.
func incomingPayloads(folder string) int {
	written, _ := filepath.Glob(filepath.Join(folder, ".readmit", "incoming", "*", "case", "payloads", "*"))
	return len(written)
}

func validDesktopRecipe() importer.Recipe {
	return importer.Recipe{
		Schema:   importer.RecipeSchema,
		Name:     "test-csv",
		Revision: 1,
		Envelope: importer.CSVEnvelope,
		Encoding: importer.UTF8,
		Members:  []string{".csv"},
		CSV: &importer.CSVDialect{
			Delimiter:       ",",
			RecordSeparator: importer.LFSeparator,
			Header:          importer.HeaderPresent,
			Fields:          4,
		},
		Payload: importer.PayloadMapping{
			Operator:   importer.VerbatimPayload,
			Locator:    importer.Locator{"payload"},
			Framing:    importer.RawFraming,
			Terminator: hl7.CR,
		},
		ObservedAt: importer.TimeMapping{
			Operator: importer.RFC3339Time,
			Locator:  importer.Locator{"time"},
		},
		Source: importer.LabelMapping{
			Operator: importer.DeclaredLabel,
			Declared: "test-system",
		},
		Direction: importer.DirectionMapping{
			Operator: importer.DeclaredDirection,
			Declared: bundle.Inbound,
		},
		Channel: importer.LabelMapping{
			Operator: importer.DeclaredLabel,
			Declared: "adt",
		},
	}
}

func TestChooseImportSources(t *testing.T) {
	c := &chooser{
		folder: "/tmp/test-folder",
		files:  []string{"/tmp/file1.hl7", "/tmp/file2.hl7"},
	}
	app := newApp(t, c)

	// Choose folder
	resFolder := app.ChooseImportSources("folder")
	if resFolder.State != desktop.Completed || resFolder.Kind != "folder" || len(resFolder.Paths) != 1 || resFolder.Paths[0] != "/tmp/test-folder" {
		t.Fatalf("unexpected folder result: %+v", resFolder)
	}

	// Choose files
	resFiles := app.ChooseImportSources("files")
	if resFiles.State != desktop.Completed || resFiles.Kind != "files" || len(resFiles.Paths) != 2 {
		t.Fatalf("unexpected files result: %+v", resFiles)
	}

	// Choose archive
	c.files = []string{"/tmp/archive.zip"}
	resArchive := app.ChooseImportSources("archive")
	if resArchive.State != desktop.Completed || resArchive.Kind != "archive" || len(resArchive.Paths) != 1 {
		t.Fatalf("unexpected archive result: %+v", resArchive)
	}

	// Dismissed dialog
	c.files = nil
	resDismissed := app.ChooseImportSources("files")
	if resDismissed.State != desktop.Cancelled {
		t.Fatalf("expected cancelled state for dismissed dialog, got: %+v", resDismissed)
	}
}

func TestPreviewImportAcrossModes(t *testing.T) {
	tempDir := t.TempDir()
	msgFile := filepath.Join(tempDir, "msg.hl7")
	if err := os.WriteFile(msgFile, []byte(sampleImportHL7), 0600); err != nil {
		t.Fatal(err)
	}
	csvFile := filepath.Join(tempDir, "data.csv")
	csvData := "time,payload,source,channel\n2026-01-01T12:00:00Z,\"" + sampleImportHL7 + "\",sys,ch\n"
	if err := os.WriteFile(csvFile, []byte(csvData), 0600); err != nil {
		t.Fatal(err)
	}
	engineFile := filepath.Join(tempDir, "engine.dat")
	if err := os.WriteFile(engineFile, []byte(sampleImportHL7), 0600); err != nil {
		t.Fatal(err)
	}

	app := workspaceApp(t)

	// 1. Plan preview
	plan := validDesktopPlan()
	resPlan := app.PreviewImport(desktop.ImportRequest{
		Workspace: tempDir,
		Mode:      "plan",
		Files:     []string{msgFile},
		Plan:      &plan,
	})
	if resPlan.State != desktop.Completed || resPlan.PlanPreview == nil || resPlan.PlanPreview.Totals.Sources != 1 {
		t.Fatalf("unexpected plan preview result: %+v", resPlan)
	}

	// 2. Recipe preview
	recipe := validDesktopRecipe()
	resRecipe := app.PreviewImport(desktop.ImportRequest{
		Workspace: tempDir,
		Mode:      "recipe",
		Files:     []string{csvFile},
		Recipe:    &recipe,
	})
	if resRecipe.State != desktop.Completed || resRecipe.RecipePreview == nil || resRecipe.RecipePreview.Totals.Sources != 1 {
		t.Fatalf("unexpected recipe preview result: %+v", resRecipe)
	}

	// 3. Engine preview
	enginePlan := engineexport.Plan{
		Schema:     engineexport.Schema,
		Engine:     "mirth",
		Version:    "4.5.2",
		Format:     "raw",
		Terminator: "cr",
	}
	resEngine := app.PreviewImport(desktop.ImportRequest{
		Workspace:  tempDir,
		Mode:       "engine",
		Files:      []string{engineFile},
		EnginePlan: &enginePlan,
	})
	if resEngine.State != desktop.Completed || resEngine.EnginePreview == nil || resEngine.EnginePreview.Qualification != "unqualified" {
		t.Fatalf("unexpected engine preview result: %+v", resEngine)
	}
}

// A feed of more MLLP frames than one parse admits is previewed frame by
// frame, as the import writes it: every booking is a message row, and none of
// the feed is reported unparsed.
func TestAPreviewOfALargeMLLPFeedListsEveryFrameAsTheImportWritesIt(t *testing.T) {
	app := workspaceApp(t)
	plan := mllpPlan()
	preview := app.PreviewImport(desktop.ImportRequest{Workspace: t.TempDir(), Mode: "plan", Files: []string{mllpSource(t, bundle.MaxEvents)}, Plan: &plan})
	if preview.State != desktop.Completed || preview.RowTotal != bundle.MaxEvents || preview.Problems.Unparsed != 0 {
		t.Fatalf("the preview of %d frames: %s %q, %d rows, %d unparsed", bundle.MaxEvents, preview.State, preview.Reason, preview.RowTotal, preview.Problems.Unparsed)
	}
	if len(preview.Rows) == 0 || preview.Rows[0].Kind != "message" {
		t.Fatalf("the first preview row is %+v", preview.Rows)
	}
}

// An engine export the window cannot read is refused without naming it, in
// the words `readmit import engine` refuses it with, when previewing and when
// committing alike.
func TestAnUnreadableEngineExportIsRefusedWithoutItsPath(t *testing.T) {
	workspace := t.TempDir()
	app := workspaceApp(t)
	enginePlan := engineexport.Plan{Schema: engineexport.Schema, Engine: "mirth", Version: "4.5.2", Format: "raw", Terminator: "cr"}
	missing := filepath.Join(workspace, "patient-named-export.dat")
	preview := app.PreviewImport(desktop.ImportRequest{Workspace: workspace, Mode: "engine", Files: []string{missing}, EnginePlan: &enginePlan})
	if preview.State != desktop.Failed || preview.Reason != "input must be a readable regular file" || strings.Contains(preview.Reason, "patient-named") {
		t.Errorf("the preview refused an unreadable export as %s %q", preview.State, preview.Reason)
	}
	if entries, err := os.ReadDir(workspace); err != nil || len(entries) != 0 {
		t.Fatalf("a refused engine import wrote into the workspace: %v %v", entries, err)
	}
}

// Writing an imported case is one synced payload file after another, and it is
// the longest step of an import. A person who cancels while it runs is answered
// at once, not when the last payload is written: the write stops between
// payloads and the case is retained incomplete, where every reader refuses it.
// Nothing incomplete is registered in the project, no receipt claims it, the
// destination is never reused, and importing again into a new destination
// completes.
func TestCancellingAnImportWhileItWritesRetainsARefusedIncompleteCase(t *testing.T) {
	folder := importProject(t)
	source := mllpSource(t, 300)
	app := workspaceApp(t)
	listed := entries(t, folder)

	request := previewedImport(t, app, folder, "Interrupted", source, "import-interrupted")
	answered := make(chan desktop.ImportCaseResult, 1)
	go func() { answered <- app.ImportCase(request) }()
	deadline := time.Now().Add(30 * time.Second)
	for incomingPayloads(folder) == 0 {
		select {
		case early := <-answered:
			t.Fatalf("the import ended before it wrote: %+v", early)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the import never began writing")
		}
		time.Sleep(time.Millisecond)
	}
	app.Cancel("import")
	cancelled := <-answered
	if cancelled.State != desktop.Cancelled || cancelled.Case != nil {
		t.Fatalf("an import cancelled while it wrote answered %+v", cancelled)
	}
	// What it wrote stays in the project's own area, where no reader lists
	// it: the project gains no entry and registers nothing.
	if after := entries(t, folder); !slices.Equal(after, listed) {
		t.Fatalf("a cancelled import added project entries: %v, was %v", after, listed)
	}
	if opened := app.OpenProject(folder); opened.State != desktop.Empty || opened.Project == nil || len(opened.Project.Cases) != 0 {
		t.Fatalf("the project registered a cancelled import: %+v", opened)
	}
	// The same click again writes the case whole and registers it once.
	again := app.ImportCase(request)
	if again.State != desktop.Completed || again.Case == nil {
		t.Fatalf("importing again after the cancel did not complete: %+v", again)
	}
	opened := app.OpenProject(folder)
	if opened.Project == nil || len(opened.Project.Cases) != 1 {
		t.Fatalf("registered after the retry: %+v", opened)
	}
	if verified := app.OpenCase(folder, opened.Project.Cases[0].Name); verified.State != desktop.Completed || verified.Case.Occurrences != 300 {
		t.Fatalf("the case the retry registered: %+v", verified)
	}
}

// The environment the killed import child reads.
const (
	importCrashChild   = "READMIT_DESKTOP_IMPORT_CRASH_CHILD"
	importCrashState   = "READMIT_DESKTOP_IMPORT_CRASH_STATE"
	importCrashProject = "READMIT_DESKTOP_IMPORT_CRASH_PROJECT"
	importCrashSource  = "READMIT_DESKTOP_IMPORT_CRASH_SOURCE"
)

// The process is killed while it is importing into a project, one import after
// another, so the kill lands inside a case write or a registration. What the
// reopened shell finds is only ever evidence and a project it can trust: the
// project document reads whole, every case it registers verifies as the case it
// registered — so a case whose write was cut short is registered nowhere — and
// importing again into a new destination completes.
func TestAKilledImportLeavesNoRegisteredOrAcceptedPartialCase(t *testing.T) {
	if os.Getenv(importCrashChild) == "1" {
		importCrashLoop(t)
		return
	}
	folder := importProject(t)
	source := mllpSource(t, 300)
	state := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), importCrashChild+"=1", importCrashState+"="+state, importCrashProject+"="+folder, importCrashSource+"="+source)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill() })
	// Killed once one import has registered and the next is writing its case.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if opened, err := project.Open(folder); err == nil && len(opened.Document.Cases) > 0 && incomingPayloads(folder) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the child never began a second import")
		}
		time.Sleep(time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()

	app := workspaceApp(t)
	opened := app.OpenProject(folder)
	if (opened.State != desktop.Completed && opened.State != desktop.Empty) || opened.Project == nil {
		t.Fatalf("a kill during an import left a project that does not read: %+v", opened)
	}
	registered := map[string]bool{}
	for _, entry := range opened.Project.Cases {
		registered[entry.Name] = true
		if verified := app.OpenCase(folder, entry.Name); verified.State != desktop.Completed || verified.Case.Identity != entry.Identity {
			t.Fatalf("the project registers %s, which does not verify as registered: %+v", entry.Name, verified)
		}
	}
	if len(registered) == 0 {
		t.Fatalf("the import that finished before the kill is not registered: %+v", opened.Project.Cases)
	}
	// A case the kill cut short was being written in the project's own area,
	// never at an entry the project lists.
	for _, entry := range opened.Project.Cases {
		if strings.HasPrefix(entry.Name, ".") {
			t.Fatalf("the project registers a case from its own area: %+v", entry)
		}
	}

	again := app.ImportCase(previewedImport(t, app, folder, "After the kill", mllpSource(t, 3), "after-the-kill"))
	// A kill during registration retains the interrupted project write, which
	// the next registration reports rather than reuses; otherwise it registers.
	if _, err := os.Stat(filepath.Join(folder, "project.json.incomplete")); err == nil {
		if again.State == desktop.Completed {
			t.Fatalf("a registration reused an interrupted project write: %+v", again)
		}
	} else if again.State != desktop.Completed || again.Case == nil {
		t.Fatalf("importing again after the kill did not register: %+v", again)
	}
}

// importCrashLoop imports the same source into one new case after another,
// registering each, until it is killed.
func importCrashLoop(t *testing.T) {
	state := os.Getenv(importCrashState)
	app := activatedApp(t, &chooser{}, state)
	folder := os.Getenv(importCrashProject)
	for i := 1; ; i++ {
		result := app.ImportCase(previewedImport(t, app, folder, fmt.Sprintf("Import %d", i), os.Getenv(importCrashSource), fmt.Sprintf("import-%d", i)))
		if result.State != desktop.Completed || result.Case == nil {
			t.Fatalf("child import %d: %+v", i, result)
		}
	}
}
