package desktop_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
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

// registeredImport imports one MLLP source into a new case of a project and
// registers it there.
func registeredImport(folder, name, source string) desktop.ImportCommitRequest {
	plan := mllpPlan()
	return desktop.ImportCommitRequest{Workspace: folder, Project: folder, Mode: "plan", OutputName: name,
		Files: []string{source}, Plan: &plan, RegisterInProject: true, CaseTitle: "Interface capture", CaseVersion: "siu-2.5.1-v1"}
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

func TestStagePastedContent(t *testing.T) {
	tempDir := t.TempDir()
	app := workspaceApp(t)

	req := desktop.PastedSourceRequest{
		Workspace: tempDir,
		Name:      "pasted-msg.hl7",
		Content:   sampleImportHL7,
		Encoding:  "utf-8",
	}

	res := app.StagePastedContent(req)
	if res.State != desktop.Completed {
		t.Fatalf("StagePastedContent failed: %+v", res)
	}
	if res.Size != len(sampleImportHL7) || res.Name != "pasted-msg.hl7" {
		t.Fatalf("unexpected size or name: %+v", res)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("staged file does not exist at %s: %v", res.Path, err)
	}

	// Staging again with the same name fails
	resDuplicate := app.StagePastedContent(req)
	if resDuplicate.State != desktop.Failed {
		t.Fatalf("expected duplicate name failure, got: %+v", resDuplicate)
	}

	// Traversal name fails
	reqTraversal := req
	reqTraversal.Name = "../bad.hl7"
	resTraversal := app.StagePastedContent(reqTraversal)
	if resTraversal.State != desktop.Failed {
		t.Fatalf("expected traversal failure, got: %+v", resTraversal)
	}
}

// Pasted content is staged only into a folder the window opened, and never
// into retained evidence: a paste aimed at a sealed case or at a folder that
// does not exist is refused before anything is created, so the case keeps
// exactly the entries it was sealed with and still verifies.
func TestStagingPastedContentCreatesNothingInEvidenceOrAnywhereNew(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	writeCase(t, root, "case", framed(string(listenFrame(t))))
	sealed := func() []string {
		entries, err := os.ReadDir(filepath.Join(root, "case"))
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return names
	}
	before := sealed()
	for _, target := range []string{filepath.Join(root, "case"), filepath.Join(root, "absent", "project")} {
		result := app.StagePastedContent(desktop.PastedSourceRequest{Workspace: root, Project: target, Name: "pasted.hl7", Content: sampleImportHL7})
		if result.State == desktop.Completed {
			t.Fatalf("pasted content was staged into %s: %+v", target, result)
		}
	}
	if after := sealed(); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("a refused paste changed the sealed case: %v, was %v", after, before)
	}
	if _, err := os.Lstat(filepath.Join(root, "absent")); !os.IsNotExist(err) {
		t.Fatal("a refused paste created the folder it was aimed at")
	}
	if opened := app.OpenCase(root, "case"); opened.State != desktop.Completed {
		t.Fatalf("the case no longer verifies after a refused paste: %+v", opened)
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

func TestCommitImportAndProjectRegistration(t *testing.T) {
	tempDir := t.TempDir()
	msgFile := filepath.Join(tempDir, "msg.hl7")
	if err := os.WriteFile(msgFile, []byte(sampleImportHL7), 0600); err != nil {
		t.Fatal(err)
	}

	// Initialize a project in tempDir
	projDoc := project.Document{
		Schema: project.Schema,
		Settings: project.Settings{
			Title:                   "Test Project",
			DefaultInterfaceVersion: "v1",
		},
		InterfaceVersions: []string{"v1"},
		Cases:             []project.Case{},
	}
	if err := project.WriteDocument(tempDir, projDoc); err != nil {
		t.Fatal(err)
	}
	revisionsDoc := project.Revisions{
		Schema:    project.RevisionsSchema,
		Revisions: []project.Revision{},
		Notes:     []project.Note{},
	}
	if err := project.WriteRevisions(tempDir, revisionsDoc); err != nil {
		t.Fatal(err)
	}

	app := workspaceApp(t)
	plan := validDesktopPlan()

	// Commit import with project registration
	commitReq := desktop.ImportCommitRequest{
		Workspace:         tempDir,
		Project:           tempDir,
		Mode:              "plan",
		OutputName:        "imported-case-1",
		ReceiptName:       "imported-case-1-receipt.json",
		Files:             []string{msgFile},
		Plan:              &plan,
		RegisterInProject: true,
		CaseTitle:         "First Imported Case",
		CaseVersion:       "v1",
	}

	res := app.CommitImport(commitReq)
	if res.State != desktop.Completed {
		t.Fatalf("CommitImport failed: %+v", res)
	}
	if res.Case == nil || res.Case.Name != "imported-case-1" || res.Case.Identity == "" {
		t.Fatalf("unexpected case result: %+v", res.Case)
	}
	if !res.Registered || res.Project == nil || len(res.Project.Cases) != 1 {
		t.Fatalf("case was not registered in project: registered=%v, project=%+v", res.Registered, res.Project)
	}
	if res.Project.Cases[0].Title != "First Imported Case" || res.Project.Cases[0].Identity != res.Case.Identity {
		t.Fatalf("registered case mismatch: %+v", res.Project.Cases[0])
	}

	// Verify the case can be opened through facade
	caseRes := app.OpenCase(tempDir, "imported-case-1")
	if caseRes.State != desktop.Completed || caseRes.Case.Identity != res.Case.Identity {
		t.Fatalf("OpenCase failed on committed case: %+v", caseRes)
	}

	// Committing to the same output name should be refused
	dupRes := app.CommitImport(commitReq)
	if dupRes.State != desktop.Failed {
		t.Fatalf("expected failure committing duplicate output name, got: %+v", dupRes)
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
	commit := app.CommitImport(desktop.ImportCommitRequest{Workspace: workspace, Mode: "engine", OutputName: "engine-case", Files: []string{missing}, EnginePlan: &enginePlan})
	for name, answered := range map[string]struct {
		state  desktop.State
		reason string
	}{"preview": {preview.State, preview.Reason}, "commit": {commit.State, commit.Reason}} {
		if answered.state != desktop.Failed || answered.reason != "input must be a readable regular file" || strings.Contains(answered.reason, "patient-named") {
			t.Errorf("the %s refused an unreadable export as %+v", name, answered)
		}
	}
	if entries, err := os.ReadDir(workspace); err != nil || len(entries) != 0 {
		t.Fatalf("a refused engine import wrote into the workspace: %v %v", entries, err)
	}
}

// The capture screen's import and the import screen's commit are one flow:
// the same destinations, the same receipt name when none is given, the same
// case description and the same registration, over the same staged folder.
func TestFinalizingACaptureAndCommittingAnImportAreOneFlow(t *testing.T) {
	workspace := t.TempDir()
	projDoc := project.Document{
		Schema:            project.Schema,
		Settings:          project.Settings{Title: "Test Project", DefaultInterfaceVersion: "v1"},
		InterfaceVersions: []string{"v1"},
		Cases:             []project.Case{},
	}
	if err := project.WriteDocument(workspace, projDoc); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteRevisions(workspace, project.Revisions{Schema: project.RevisionsSchema, Revisions: []project.Revision{}, Notes: []project.Note{}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "export"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "export", "one.hl7"), []byte(sampleImportHL7), 0600); err != nil {
		t.Fatal(err)
	}
	collectStaged(t, workspace)
	staged := filepath.Join(workspace, "staged")
	app := workspaceApp(t)
	plan := operation.DefaultImportPlan()
	finalized := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{
		Workspace: workspace, Project: workspace, Folder: "staged", CollectionReceipt: "staged.json", OutputName: "finalized",
		RegisterInProject: true, CaseVersion: "v1",
	})
	committed := app.CommitImport(desktop.ImportCommitRequest{
		Workspace: workspace, Project: workspace, Mode: "plan", Plan: &plan, Folders: []string{staged}, OutputName: "committed",
		RegisterInProject: true, CaseVersion: "v1",
	})
	for name, result := range map[string]desktop.ImportCommitResult{"finalized": finalized, "committed": committed} {
		if result.State != desktop.Completed || !result.Registered || result.Case == nil || result.Case.Name != name ||
			filepath.Base(result.ReceiptPath) != name+"-receipt.json" {
			t.Fatalf("%s: %+v", name, result)
		}
		if _, err := os.Stat(result.ReceiptPath); err != nil {
			t.Fatalf("%s wrote no receipt where it said: %v", name, err)
		}
		opened := app.OpenCase(workspace, name)
		if opened.State != desktop.Completed || !reflect.DeepEqual(opened.Case, result.Case) {
			t.Fatalf("%s described its case as %+v; opening it reads %+v", name, result.Case, opened.Case)
		}
	}
	if last := committed.Project; last == nil || len(last.Cases) != 2 || last.Cases[0].Title != "finalized" || last.Cases[1].Title != "committed" {
		t.Fatalf("registered: %+v", last)
	}

	// Both refuse a destination the same way.
	again := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{Workspace: workspace, Folder: "staged", CollectionReceipt: "staged.json", OutputName: "committed"})
	if again.State != desktop.Failed || again.Reason != operation.ErrImportCaseExists.Error() {
		t.Fatalf("finalizing into a taken case: %+v", again)
	}
}

func TestCommitImportHonorsBusySlot(t *testing.T) {
	app := workspaceApp(t)
	plan := validDesktopPlan()

	// Claim slot externally or run concurrent preview
	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		app.PreviewImport(desktop.ImportRequest{
			Workspace: "/nonexistent",
			Mode:      "plan",
			Plan:      &plan,
		})
	}()

	// Since operations claim the single slot, test slot contention via a held dialog
	c := &chooser{
		before: func() {
			close(started)
			<-done
		},
	}
	activeApp := newApp(t, c)

	go func() {
		activeApp.ChooseImportSources("folder")
	}()

	<-started
	// While ChooseImportSources is running and holding the slot:
	busyRes := activeApp.CommitImport(desktop.ImportCommitRequest{
		Workspace:  "/tmp",
		Mode:       "plan",
		OutputName: "case-x",
		Plan:       &plan,
	})
	close(done)

	if busyRes.State != desktop.Busy {
		t.Fatalf("expected busy state while another operation runs, got: %+v", busyRes)
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

	answered := make(chan desktop.ImportCommitResult, 1)
	go func() { answered <- app.CommitImport(registeredImport(folder, "interrupted", source)) }()
	awaitEntries(t, filepath.Join(folder, "interrupted", "payloads"), 1, answered)
	app.Cancel("import")
	cancelled := <-answered
	if cancelled.State != desktop.Cancelled || cancelled.Case != nil || cancelled.Registered {
		t.Fatalf("an import cancelled while it wrote answered %+v", cancelled)
	}
	if _, err := os.Lstat(filepath.Join(folder, "interrupted", "identity.sha256")); !os.IsNotExist(err) {
		t.Fatalf("a cancelled import wrote its completion marker: %v", err)
	}
	if opened := app.OpenCase(folder, "interrupted"); opened.State == desktop.Completed {
		t.Fatalf("the reader accepted an import that was cancelled while it wrote: %+v", opened)
	}
	if _, err := os.Lstat(filepath.Join(folder, "interrupted-receipt.json")); !os.IsNotExist(err) {
		t.Fatalf("a receipt claims a cancelled import: %v", err)
	}
	if opened := app.OpenProject(folder); opened.State != desktop.Empty || opened.Project == nil || len(opened.Project.Cases) != 0 {
		t.Fatalf("the project registered a cancelled import: %+v", opened)
	}
	if reused := app.CommitImport(registeredImport(folder, "interrupted", source)); reused.State != desktop.Failed {
		t.Fatalf("a cancelled import's destination was reused: %+v", reused)
	}
	again := app.CommitImport(registeredImport(folder, "imported", mllpSource(t, 3)))
	if again.State != desktop.Completed || !again.Registered || again.Case == nil || again.Case.Occurrences != 3 {
		t.Fatalf("importing again into a new destination did not complete: %+v", again)
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
	// Killed once one import has finished and the next is writing its case.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if written, _ := os.ReadDir(filepath.Join(folder, "import-2", "payloads")); len(written) > 0 {
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
	if !registered["import-1"] {
		t.Fatalf("the import that finished before the kill is not registered: %+v", opened.Project.Cases)
	}
	// The case the kill cut short carries no completion marker, and nothing
	// accepts it; one the write outran is simply complete.
	if _, err := os.Lstat(filepath.Join(folder, "import-2", "identity.sha256")); os.IsNotExist(err) {
		if verified := app.OpenCase(folder, "import-2"); verified.State == desktop.Completed || registered["import-2"] {
			t.Fatalf("a case the kill cut short was accepted: %+v", verified)
		}
	}

	again := app.CommitImport(registeredImport(folder, "after-the-kill", mllpSource(t, 3)))
	if again.State != desktop.Completed || again.Case == nil {
		t.Fatalf("importing again after the kill did not complete: %+v", again)
	}
	// A kill during registration retains the interrupted project write, which
	// the next registration reports rather than reuses; otherwise it registers.
	if _, err := os.Stat(filepath.Join(folder, "project.json.incomplete")); err == nil {
		if again.Registered {
			t.Fatalf("a registration reused an interrupted project write: %+v", again)
		}
	} else if !again.Registered {
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
		result := app.CommitImport(registeredImport(folder, fmt.Sprintf("import-%d", i), os.Getenv(importCrashSource)))
		if result.State != desktop.Completed || !result.Registered {
			t.Fatalf("child import %d: %+v", i, result)
		}
	}
}
