package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
