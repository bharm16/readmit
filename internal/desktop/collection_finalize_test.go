package desktop_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
)

// collectedMessage is one whole synthetic message, segments ended by the
// terminator a test declares.
func collectedMessage(terminator string) string {
	return "MSH|^~\\&|SEND|FAC|RECV|FAC|20260101120000||SIU^S12|MSG001|P|2.5.1" + terminator +
		"SCH|1||||||||||||||||||||||||BOOKED" + terminator
}

// exportSource declares the local export folder root as a directory source.
func exportSource(root string) evidencesource.Source {
	return evidencesource.Source{
		Schema: evidencesource.Schema, Name: "exports", Kind: evidencesource.Directory,
		Scope: "appointments", Root: root,
		Quota: evidencesource.Quota{MaxEntries: 8, MaxEntryBytes: 1 << 20, MaxTotalBytes: 8 << 20},
		Retry: evidencesource.Retry{Attempts: 1, Backoff: "1ms"},
	}
}

// collectThroughWindow registers a directory source over the named entries
// and collects it the way the capture screen does, into "collected" with the
// receipt "collection.json" beside it.
func collectThroughWindow(t *testing.T, app *desktop.App, root string, plan importer.Plan, entries map[string]string) desktop.SourceCollectionResult {
	t.Helper()
	export := filepath.Join(root, "export")
	if err := os.Mkdir(export, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range entries {
		writeDocument(t, export, name, content)
	}
	if saved := app.SaveSourceRegistration(desktop.SourceRegistrationRequest{Workspace: root, SourceFile: "source.json", Source: exportSource(export)}); saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	return app.CollectSource(desktop.SourceWorkRequest{Workspace: root, SourceFile: "source.json", Plan: &plan,
		OutputName: "collected", ReceiptName: "collection.json"})
}

// collectStaged leaves in folder what an earlier collection of folder/export
// left there: the staged folder "staged" and its receipt "staged.json".
func collectStaged(t *testing.T, folder string) {
	t.Helper()
	options, err := operation.SourceOptions(validDesktopPlan(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := operation.SourceCollect(context.Background(), exportSource(filepath.Join(folder, "export")), filepath.Join(folder, "staged"), filepath.Join(folder, "staged.json"), options); err != nil {
		t.Fatal(err)
	}
}

func collectionPlan(framing importer.Framing, terminator hl7.Terminator, members ...string) importer.Plan {
	return importer.Plan{Schema: importer.PlanSchema, Framing: framing, Terminator: terminator,
		Encoding: importer.UTF8, Direction: bundle.Inbound, Members: members}
}

// A folder collected under MLLP framing is finalized under the framing it was
// collected under, not refused for contradicting a plan nobody declared.
func TestFinalizingAnMLLPCollectionImportsItUnderItsOwnPlan(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	plan := collectionPlan(importer.MLLPFraming, hl7.CR, ".mllp")
	framed := "\x0b" + collectedMessage("\r") + "\x1c\r"
	if collected := collectThroughWindow(t, app, root, plan, map[string]string{"one.mllp": framed}); collected.State != desktop.Completed {
		t.Fatalf("collect: %+v", collected)
	}
	result := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{Workspace: root, Folder: "collected", CollectionReceipt: "collection.json", OutputName: "imported.case"})
	if result.State != desktop.Completed || result.Case == nil || result.Case.Messages != 1 || result.Case.Unparsed != 0 {
		t.Fatalf("finalizing an MLLP collection: %+v", result)
	}
}

// A folder collected with LF segment terminators is finalized under that
// terminator, so its message parses rather than being quarantined.
func TestFinalizingAnLFCollectionImportsItWithoutQuarantine(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	plan := collectionPlan(importer.RawFraming, hl7.LF, ".hl7")
	if collected := collectThroughWindow(t, app, root, plan, map[string]string{"one.hl7": collectedMessage("\n")}); collected.State != desktop.Completed {
		t.Fatalf("collect: %+v", collected)
	}
	result := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{Workspace: root, Folder: "collected", CollectionReceipt: "collection.json", OutputName: "imported.case"})
	if result.State != desktop.Completed || result.Case == nil || result.Case.Messages != 1 || result.Case.Unparsed != 0 {
		t.Fatalf("finalizing an LF collection: %+v", result)
	}
}

// A collection that did not complete staged part of its scope. Finalizing it
// is refused, and no case is written that would read as the whole source.
func TestFinalizingAnIncompleteCollectionIsRefused(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	plan := collectionPlan(importer.RawFraming, hl7.CR, ".hl7")
	// The second entry's framing contradicts the declared raw framing, so it is
	// not collected and the collection fails with the first entry staged.
	collected := collectThroughWindow(t, app, root, plan, map[string]string{
		"one.hl7": collectedMessage("\r"), "two.hl7": "\x0b" + collectedMessage("\r") + "\x1c\r",
	})
	if collected.State != desktop.Failed || collected.Collection == nil || collected.Collection.Collected != 1 || collected.OutputPath != "collected" {
		t.Fatalf("collect: %+v", collected)
	}
	result := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{Workspace: root, Folder: "collected", CollectionReceipt: "collection.json", OutputName: "imported.case"})
	if result.State != desktop.Failed || result.Reason != evidencesource.ErrNotImportable.Error() || result.Case != nil {
		t.Fatalf("finalizing an incomplete collection: %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(root, "imported.case")); !os.IsNotExist(err) {
		t.Fatal("finalizing an incomplete collection wrote a case")
	}
}

// A folder collected under members the finalize step's former default did not
// name is finalized under the members it was collected under.
func TestFinalizingACollectionOfOtherMembersImportsThem(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	plan := collectionPlan(importer.RawFraming, hl7.CR, ".txt")
	if collected := collectThroughWindow(t, app, root, plan, map[string]string{"one.txt": collectedMessage("\r")}); collected.State != desktop.Completed {
		t.Fatalf("collect: %+v", collected)
	}
	result := app.FinalizeCaptureImport(desktop.FinalizeCaptureRequest{Workspace: root, Folder: "collected", CollectionReceipt: "collection.json", OutputName: "imported.case"})
	if result.State != desktop.Completed || result.Case == nil || result.Case.Messages != 1 {
		t.Fatalf("finalizing a collection of .txt members: %+v", result)
	}
}
