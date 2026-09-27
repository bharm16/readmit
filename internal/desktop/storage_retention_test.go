package desktop_test

// An archive or a deletion names the retention its source declares and the
// related work that refers to it. A protected transfer package within its
// declared retention blocks Delete source, with no override, as discarding
// the package would be refused; a search index's retention is shown and
// never blocks. One case of a project is archived and deleted the way the
// whole project is: against its own verified archive, and nothing else.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/protect"
)

func archivedProject(t *testing.T, app *desktop.App, opened desktop.ProjectOpenResult, items ...desktop.ItemRef) desktop.ReviewedActionResult {
	t.Helper()
	request := storageRequest(opened.Context, desktop.StorageArchiveCopyAction, desktop.StorageActionOptions{})
	if items != nil {
		request.Items = items
	}
	archived := executed(app, opened.Context, prepared(t, app, request), "click-archive-"+time.Now().Format("150405.000000000"))
	if archived.State != desktop.Completed || archived.Storage == nil || archived.Storage.Backup == nil {
		t.Fatalf("archive: %+v", archived)
	}
	return archived
}

func TestDeleteSourceIsRefusedWhileATransferPackageIsWithinItsRetention(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	if saved := app.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: root, Entry: "protection.json", Name: "lab-evidence", Storage: "os-volume-encryption",
		Command: keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", ""), Arguments: []string{"find-generic-password", "-w", "-s", "readmit-lab-key"},
		MaxAge: "720h", Retain: "1h",
	}); saved.State != desktop.Completed {
		t.Fatalf("control: %+v", saved)
	}
	writeDocument(t, root, "evidence.txt", "synthetic retained evidence bytes")
	if packed := app.PackProtectedPackage(desktop.ProtectionPackRequest{Workspace: root, Entry: "protection.json", Control: "lab-evidence",
		Sources: []string{"evidence.txt"}, Output: "transfer"}); packed.State != desktop.Completed {
		t.Fatalf("pack: %+v", packed)
	}
	descriptor, _, err := protect.ReadPackage(filepath.Join(root, "transfer"))
	if err != nil {
		t.Fatal(err)
	}
	until := descriptor.RetainUntil.UTC().Format(time.RFC3339)
	// The archive names the retention as scope.
	archive := prepared(t, app, storageRequest(opened.Context, desktop.StorageArchiveCopyAction, desktop.StorageActionOptions{}))
	held := func(review *desktop.StorageReview) *desktop.RetentionHold {
		for i, hold := range review.Retention {
			if hold.Kind == desktop.TransferPackageRetention && hold.Entry == "transfer" {
				return &review.Retention[i]
			}
		}
		return nil
	}
	if hold := held(archive.Storage); hold == nil || hold.State != protect.WithinRetention || hold.Until != until || !hold.Blocks {
		t.Fatalf("the archive review's retention: %+v", archive.Storage.Retention)
	}
	archived := executed(app, opened.Context, archive, "click-archive")
	if archived.State != desktop.Completed {
		t.Fatalf("archive: %+v", archived)
	}
	original := projectTree(t, root)
	refused := app.PrepareAction(storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
	if refused.Review == nil || refused.Review.Ready || refused.Review.Token != "" ||
		refused.Review.Refusal != "transfer is declared retained until "+until+"; nothing is deleted" || held(refused.Review.Storage) == nil {
		t.Fatalf("a delete within a package's retention: %+v", refused.Review)
	}
	if !sameFiles(original, projectTree(t, root)) {
		t.Fatal("a refused deletion changed the source")
	}
	// Once the declared retention has passed, the package no longer holds
	// the project, and the deletion goes ahead against the verified archive.
	desktop.SetClockForTest(app, func() time.Time { return descriptor.RetainUntil.Add(time.Minute) })
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
	if hold := held(review.Storage); hold == nil || hold.State != protect.PastRetention || hold.Blocks {
		t.Fatalf("a retention that passed: %+v", review.Storage.Retention)
	}
	if deleted := executed(app, opened.Context, review, "click-delete-source"); deleted.State != desktop.Completed || deleted.Storage.Source != lifecycle.Deleted {
		t.Fatalf("delete source: %+v", deleted)
	}
}

func TestDeleteSourceNamesIndexRetentionWithoutBlocking(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	until := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	if built := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "regression", Output: "regression.index.json",
		Fields: []string{"MSH-9"}, Retention: index.RetainDigests, RetainUntil: until.Format(time.RFC3339)}); built.State != desktop.Completed {
		t.Fatalf("build: %+v", built)
	}
	archivedProject(t, app, opened)
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
	want := desktop.RetentionHold{Kind: desktop.SearchIndexRetention, Entry: "regression.index.json", Case: "regression",
		State: protect.WithinRetention, Until: until.Format(time.RFC3339)}
	if !slices.Contains(review.Storage.Retention, want) {
		t.Fatalf("the index's retention is not named: %+v", review.Storage.Retention)
	}
	// Related work is what the project holds, by kind.
	if !slices.ContainsFunc(review.Storage.Related, func(related desktop.RelatedWork) bool {
		return related.Kind == desktop.CaseItem && related.Count == 1
	}) {
		t.Fatalf("related work: %+v", review.Storage.Related)
	}
	if deleted := executed(app, opened.Context, review, "click-delete-source"); deleted.State != desktop.Completed || deleted.Storage.Source != lifecycle.Deleted {
		t.Fatalf("delete source: %+v", deleted)
	}
}

func TestArchiveAndDeleteOneCaseAgainstItsVerifiedArchive(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	var regression desktop.CatalogItem
	for _, item := range listed(t, app, root, desktop.CaseItem) {
		regression = item
	}
	ref := regression.Ref
	writeDocument(t, root, "unrelated.txt", "kept")
	caseRequest := func(action desktop.ActionID) desktop.PrepareActionRequest {
		request := storageRequest(opened.Context, action, desktop.StorageActionOptions{})
		request.Items = []desktop.ItemRef{ref}
		return request
	}
	// Without an archive of the case, it is not deleted; an archive of the
	// whole project does not stand for one of the case.
	archivedProject(t, app, opened)
	unarchived := app.PrepareAction(caseRequest(desktop.StorageDeleteSourceAction))
	if unarchived.Review == nil || unarchived.Review.Ready || unarchived.Review.Refusal != "this case has no archive copy yet; archive it first, and it is deleted only against that verified copy" {
		t.Fatalf("a delete of a case with no archive: %+v", unarchived.Review)
	}
	review := prepared(t, app, caseRequest(desktop.StorageArchiveCopyAction))
	if len(review.Items) != 1 || review.Items[0].Ref.ID != ref.ID || review.Storage.Files == 0 || review.Storage.Project != "Scheduling investigation" ||
		review.Storage.Consequence != "Copies the selected case into a new verified archive; the case is kept." {
		t.Fatalf("the case archive review: %+v %+v", review.Items, review.Storage)
	}
	before := projectTree(t, root)
	archived := executed(app, opened.Context, review, "click-archive-case")
	row := archived.Storage.Backup
	if archived.State != desktop.Completed || row == nil || row.Case != ref.ID || row.Reason != desktop.BackupReasonArchive || row.ProjectID != opened.Context.ProjectID {
		t.Fatalf("the case archive: %+v %+v", archived, row)
	}
	if !sameFiles(before, projectTree(t, root)) {
		t.Fatal("archiving a case changed the project")
	}
	document, _, err := backup.Inspect(row.Folder)
	if err != nil || len(document.Evidence) != 1 || document.Evidence[0].Name != "regression" || document.Evidence[0].State != backup.Verified {
		t.Fatalf("the archive does not hold the case as verified evidence: %+v %v", document, err)
	}
	// Deleted against that archive, the case's folder goes, the project no
	// longer registers or lists it, and nothing else of the project changes.
	deletion := prepared(t, app, caseRequest(desktop.StorageDeleteSourceAction))
	if deletion.Storage.Backup == nil || deletion.Storage.Backup.ID != row.ID || len(deletion.Items) != 1 {
		t.Fatalf("the case deletion review: %+v", deletion.Storage)
	}
	deleted := executed(app, opened.Context, deletion, "click-delete-case")
	if deleted.State != desktop.Completed || deleted.Storage.Source != lifecycle.Deleted {
		t.Fatalf("delete the case: %+v", deleted)
	}
	if _, err := os.Lstat(filepath.Join(root, "regression")); !os.IsNotExist(err) {
		t.Fatal("the case folder is still there")
	}
	if _, err := os.Lstat(lifecycle.CaseRemainder(root, "regression")); !os.IsNotExist(err) {
		t.Fatal("the case deletion left a remainder")
	}
	if cases := listed(t, app, root, desktop.CaseItem); len(cases) != 0 {
		t.Fatalf("the deleted case is still listed: %+v", cases)
	}
	if reopened, err := project.Open(root); err != nil || len(reopened.Document.Cases) != 0 {
		t.Fatalf("the project still registers the case: %+v %v", reopened, err)
	}
	if kept := mustRead(t, filepath.Join(root, "unrelated.txt")); string(kept) != "kept" {
		t.Fatal("deleting a case changed a file beside it")
	}
	if _, err := backup.Verify(row.Folder); err != nil {
		t.Fatalf("the case archive is not kept whole: %v", err)
	}
	// Restoring the case archive is an ordinary restore into a new project
	// holding the case.
	restore := prepared(t, app, storageRequest(opened.Context, desktop.StorageRestoreBackupAction, desktop.StorageActionOptions{Backup: row.ID}))
	restored := executed(app, opened.Context, restore, "click-restore-case")
	if restored.State != desktop.Completed || restored.Storage.Project == nil {
		t.Fatalf("restore the case archive: %+v", restored)
	}
	if overview := app.OpenProjectOverview(restored.Storage.Project.Context.Project); overview.Overview == nil || len(overview.Overview.Cases) != 1 ||
		overview.Overview.Cases[0].Evidence != "verified" {
		t.Fatalf("the restored case: %+v", overview)
	}
}
