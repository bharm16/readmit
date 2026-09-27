package desktop_test

// Storage's tasks that write or delete are reviewed actions. Each one binds
// the exact backup, project and folder its review showed, reads them again at
// the final click, writes only somewhere new or deletes only what it names,
// and leaves the project a person had open exactly as it was when it fails.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/lifecycle"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/upgrade"
)

func knownProjects(t *testing.T, app *desktop.App) []desktop.CatalogItem {
	t.Helper()
	listed := app.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem})
	if listed.Page == nil {
		t.Fatalf("projects: %+v", listed)
	}
	return listed.Page.Items
}

func storageRequest(context desktop.RequestContext, action desktop.ActionID, options desktop.StorageActionOptions) desktop.PrepareActionRequest {
	return desktop.PrepareActionRequest{Context: context, Action: action, Items: []desktop.ItemRef{}, Storage: &options}
}

func executed(app *desktop.App, context desktop.RequestContext, review *desktop.ActionReview, click string) desktop.ReviewedActionResult {
	return app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: click})
}

// projectTree is every file of a project and its digest, so "unchanged" is
// checked against bytes.
func projectTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		relative, _ := filepath.Rel(root, path)
		files[relative] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for name, digest := range a {
		if b[name] != digest {
			return false
		}
	}
	return true
}

func TestRestoreCreatesASeparateProjectWithItsOwnIdentityAndOpensIt(t *testing.T) {
	app, location, _ := storageApp(t)
	long := strings.Repeat("x", 200)
	for _, lane := range []struct {
		name, title string
		moved       bool
	}{
		{"ordinary", "Scheduling investigation", false},
		{"long title", long, false},
		{"moved and renamed backup", "Order interface", true},
	} {
		t.Run(lane.name, func(t *testing.T) {
			opened := storageProject(t, app, lane.title)
			original := projectTree(t, opened.Context.Project)
			made := backedUp(t, app, opened)
			id := made.ID
			if lane.moved {
				// Moved into the backup folder under another name, the backup
				// is no longer the recorded one; it is still the same backup.
				renamed := filepath.Join(location, "renamed-by-a-person")
				if err := os.Rename(made.Folder, renamed); err != nil {
					t.Fatal(err)
				}
				id = ""
				for _, row := range app.ListBackups().Backups {
					if row.Folder == resolved(t, renamed) && row.Availability == desktop.ItemAvailable {
						id = row.ID
					}
				}
				if id == "" {
					t.Fatalf("the moved backup is not listed: %+v", app.ListBackups())
				}
			}
			review := prepared(t, app, storageRequest(opened.Context, desktop.StorageRestoreBackupAction, desktop.StorageActionOptions{Backup: id}))
			want := lane.title + " restored"
			if lane.title == long {
				want = strings.Repeat("x", 191) + " restored"
			}
			if review.Consent != desktop.RestoreConsent || review.Storage == nil || review.Storage.Name != want ||
				review.Storage.Consequence != "Creates a separate project; the current project stays unchanged." ||
				review.Storage.Contents == nil || !review.Storage.Contents.Complete || review.Storage.Location == "" {
				t.Fatalf("review: %+v %+v", review, review.Storage)
			}
			if utf8.RuneCountInString(review.Storage.Name) > 200 {
				t.Fatalf("the default name breaks the title rule: %d", utf8.RuneCountInString(review.Storage.Name))
			}
			restored := executed(app, opened.Context, review, "click-restore-"+strings.ReplaceAll(lane.name, " ", "-"))
			if restored.State != desktop.Completed || restored.Outcome != desktop.ActionCompleted || restored.Storage == nil || restored.Storage.Project == nil {
				t.Fatalf("restore: %+v", restored)
			}
			reopened := restored.Storage.Project
			if reopened.State != desktop.Completed || !reopened.Recorded || reopened.Project == nil || reopened.Project.Name != want {
				t.Fatalf("the restored project did not open: %+v", reopened)
			}
			if reopened.Context.ProjectID == opened.Context.ProjectID || reopened.Context.Project == opened.Context.Project {
				t.Fatalf("the restored project shares the original's identity or folder: %+v %+v", reopened.Context, opened.Context)
			}
			if !sameFiles(original, projectTree(t, opened.Context.Project)) {
				t.Fatal("restoring changed the original project")
			}
			// Both projects stay among the projects this viewer opened.
			known := map[string]bool{}
			for _, item := range knownProjects(t, app) {
				known[item.Ref.ID] = true
			}
			if !known[opened.Context.ProjectID] || !known[reopened.Context.ProjectID] {
				t.Fatalf("recents lost a project: %v", known)
			}
			if overview := app.OpenProjectOverview(reopened.Context.Project); overview.Overview == nil || len(overview.Overview.Cases) == 0 {
				t.Fatalf("the restored project's evidence: %+v", overview)
			}
		})
	}
}

func TestRestoreRefusesAChangedBackupAsStale(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	made := backedUp(t, app, opened)
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageRestoreBackupAction, desktop.StorageActionOptions{Backup: made.ID}))
	before, err := os.ReadDir(review.Storage.Location)
	if err != nil {
		t.Fatal(err)
	}
	// The backup is damaged after it was reviewed.
	document, _, err := backup.Inspect(made.Folder)
	if err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(made.Folder, backup.FilesDirectory, document.Files[0].Path)
	if err := os.WriteFile(stored, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := executed(app, opened.Context, review, "click-stale-restore")
	if stale.Outcome != desktop.ActionStale || stale.Storage != nil {
		t.Fatalf("a changed backup was restored: %+v", stale)
	}
	if stale.Refreshed != nil && stale.Refreshed.Ready {
		t.Fatalf("the refreshed review of a damaged backup is ready: %+v", stale.Refreshed)
	}
	after, err := os.ReadDir(review.Storage.Location)
	if err != nil || len(after) != len(before) {
		t.Fatalf("a refused restore wrote into the destination: %v %v", before, after)
	}
	// A backup that is missing is refused before anything is written.
	if err := os.RemoveAll(made.Folder); err != nil {
		t.Fatal(err)
	}
	missing := app.PrepareAction(storageRequest(opened.Context, desktop.StorageRestoreBackupAction, desktop.StorageActionOptions{Backup: made.ID}))
	if missing.Review == nil || missing.Review.Ready || missing.Review.Token != "" || missing.Review.Refusal == "" {
		t.Fatalf("a missing backup: %+v", missing)
	}
}

func TestDeleteBackupRemovesOnlyWhatItsManifestNames(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	original := projectTree(t, opened.Context.Project)
	made := backedUp(t, app, opened)
	kept := backedUp(t, app, opened)
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageDeleteBackupAction, desktop.StorageActionOptions{Backup: made.ID}))
	if review.Consent != desktop.DeleteConsent || review.Storage.Consequence != "Deletes this backup; the source project remains." ||
		review.Storage.Backup == nil || review.Storage.Backup.CreatedAt == nil || review.Storage.Location == "" {
		t.Fatalf("review: %+v", review.Storage)
	}
	// A file somebody put inside the backup is not the backup's to delete.
	stray := filepath.Join(made.Folder, "notes.txt")
	if err := os.WriteFile(stray, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	refused := executed(app, opened.Context, review, "click-delete-stray")
	if refused.State == desktop.Completed {
		t.Fatalf("a backup holding an unrecorded file was deleted: %+v", refused)
	}
	if _, err := os.Stat(filepath.Join(made.Folder, backup.MarkerName)); err != nil {
		t.Fatal("a refused deletion removed part of the backup")
	}
	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}
	again := prepared(t, app, storageRequest(opened.Context, desktop.StorageDeleteBackupAction, desktop.StorageActionOptions{Backup: made.ID}))
	deleted := executed(app, opened.Context, again, "click-delete")
	if deleted.State != desktop.Completed || deleted.Outcome != desktop.ActionCompleted {
		t.Fatalf("delete: %+v", deleted)
	}
	if _, err := os.Lstat(made.Folder); !os.IsNotExist(err) {
		t.Fatal("the backup is still there")
	}
	if !sameFiles(original, projectTree(t, opened.Context.Project)) {
		t.Fatal("deleting a backup changed its source project")
	}
	listed := app.ListBackups()
	if len(listed.Backups) != 1 || listed.Backups[0].ID != kept.ID || listed.Backups[0].Availability != desktop.ItemAvailable {
		t.Fatalf("after delete: %+v", listed)
	}
}

func TestDeleteSourceUsesTheRecordedArchiveAndReportsARemainder(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	// Without an archive nothing can be deleted.
	unarchived := app.PrepareAction(storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
	if unarchived.Review == nil || unarchived.Review.Ready || unarchived.Review.Refusal == "" {
		t.Fatalf("delete before any archive: %+v", unarchived)
	}
	archive := prepared(t, app, storageRequest(opened.Context, desktop.StorageArchiveCopyAction, desktop.StorageActionOptions{}))
	if archive.Consent != desktop.CopyConsent || archive.Storage.Files == 0 || archive.Storage.Bytes == 0 {
		t.Fatalf("archive review: %+v", archive.Storage)
	}
	archived := executed(app, opened.Context, archive, "click-archive")
	if archived.State != desktop.Completed || archived.Storage == nil || archived.Storage.Backup == nil ||
		archived.Storage.Backup.Reason != desktop.BackupReasonArchive {
		t.Fatalf("archive: %+v", archived)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("archiving removed the source")
	}
	// A source changed after its archive is not deleted against it.
	edited, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	edited.Document.Settings.DefaultOwner = "changed-after-archive"
	if err := edited.Save(edited.Document); err != nil {
		t.Fatal(err)
	}
	changed := app.PrepareAction(storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
	if changed.Review == nil || changed.Review.Ready {
		t.Fatalf("delete of a source changed after its archive: %+v", changed)
	}
	archived = executed(app, opened.Context, prepared(t, app, storageRequest(opened.Context, desktop.StorageArchiveCopyAction, desktop.StorageActionOptions{})), "click-archive-again")
	if archived.State != desktop.Completed {
		t.Fatalf("archive again: %+v", archived)
	}
	backupsBefore := len(app.ListBackups().Backups)
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
	if review.Consent != desktop.DeleteConsent || review.Storage.Backup == nil || review.Storage.Backup.ID != archived.Storage.Backup.ID ||
		review.Storage.Consequence != "Archives the selected source, then deletes it from this computer; this is not secure erasure." ||
		review.Storage.Contents == nil {
		t.Fatalf("delete review: %+v", review.Storage)
	}
	neighbour := filepath.Join(filepath.Dir(root), "unrelated.txt")
	if err := os.WriteFile(neighbour, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		// The source's inner folder refuses the unlink of what it holds, so
		// the removal stops part way.
		inner := filepath.Join(root, "regression")
		if err := os.Chmod(inner, 0o500); err != nil {
			t.Fatal(err)
		}
		partial := executed(app, opened.Context, review, "click-delete-source")
		if partial.State != desktop.Failed || partial.Storage == nil || partial.Storage.Source != lifecycle.RemovalIncomplete ||
			partial.Storage.Remainder != root+".retiring" {
			t.Fatalf("a partial deletion: %+v", partial)
		}
		t.Cleanup(func() { os.Chmod(filepath.Join(root+".retiring", "regression"), 0o700) })
		retry := app.PrepareAction(storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
		if retry.Review != nil && retry.Review.Ready {
			t.Fatalf("a retry over a remainder is ready: %+v", retry)
		}
		if len(app.ListBackups().Backups) != backupsBefore {
			t.Fatal("a retry took a second archive")
		}
		if err := os.Chmod(filepath.Join(root+".retiring", "regression"), 0o700); err != nil {
			t.Fatal(err)
		}
		return
	}
	deleted := executed(app, opened.Context, review, "click-delete-source")
	if deleted.State != desktop.Completed || deleted.Storage.Source != lifecycle.Deleted {
		t.Fatalf("delete source: %+v", deleted)
	}
	if _, err := os.Stat(neighbour); err != nil {
		t.Fatal("deleting a source removed a file beside it")
	}
}

func TestDeleteSourceDeletesAgainstItsVerifiedArchive(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	archived := executed(app, opened.Context, prepared(t, app, storageRequest(opened.Context, desktop.StorageArchiveCopyAction, desktop.StorageActionOptions{})), "click-archive")
	if archived.State != desktop.Completed {
		t.Fatalf("archive: %+v", archived)
	}
	neighbour := filepath.Join(filepath.Dir(root), "unrelated.txt")
	if err := os.WriteFile(neighbour, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageDeleteSourceAction, desktop.StorageActionOptions{}))
	deleted := executed(app, opened.Context, review, "click-delete-source")
	if deleted.State != desktop.Completed || deleted.Storage.Source != lifecycle.Deleted || deleted.Storage.Remainder != "" {
		t.Fatalf("delete source: %+v", deleted)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("the source is still there")
	}
	if _, err := os.Stat(neighbour); err != nil {
		t.Fatal("deleting a source removed a file beside it")
	}
	if _, err := backup.Verify(archived.Storage.Backup.Folder); err != nil {
		t.Fatalf("the archive is not kept whole: %v", err)
	}
}

func TestMoveKeepsIdentitySealsAndPriorLocationOnFailure(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	original := projectTree(t, root)
	if unchosen := app.PrepareAction(storageRequest(opened.Context, desktop.StorageMoveProjectAction, desktop.StorageActionOptions{})); unchosen.State == desktop.Completed {
		t.Fatalf("a move with no destination: %+v", unchosen)
	}
	destination := t.TempDir()
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageMoveProjectAction, desktop.StorageActionOptions{Location: destination}))
	if review.Consent != desktop.CopyConsent || review.Storage.Files == 0 {
		t.Fatalf("move review: %+v", review.Storage)
	}
	// A project changed after its review is not moved, and nothing is left
	// that could be taken for it.
	edited, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	edited.Document.Settings.DefaultOwner = "edited"
	if err := edited.Save(edited.Document); err != nil {
		t.Fatal(err)
	}
	if stale := executed(app, opened.Context, review, "click-move-stale"); stale.Outcome != desktop.ActionStale {
		t.Fatalf("a changed project moved: %+v", stale)
	}
	if entries, _ := os.ReadDir(destination); len(entries) != 0 {
		t.Fatalf("a refused move wrote into the destination: %v", entries)
	}
	original = projectTree(t, root)
	review = prepared(t, app, storageRequest(opened.Context, desktop.StorageMoveProjectAction, desktop.StorageActionOptions{Location: destination}))
	moved := executed(app, opened.Context, review, "click-move")
	if moved.State != desktop.Completed || moved.Storage == nil || moved.Storage.Project == nil || moved.Storage.Project.State != desktop.Completed {
		t.Fatalf("move: %+v", moved)
	}
	now := moved.Storage.Project.Context
	if now.ProjectID != opened.Context.ProjectID || filepath.Dir(now.Project) != resolved(t, destination) {
		t.Fatalf("the moved project: %+v", now)
	}
	// Every sealed file came across byte for byte, and the source is kept.
	if !sameFiles(original, projectTree(t, now.Project)) || !sameFiles(original, projectTree(t, root)) {
		t.Fatal("the move changed a byte of the project or of its copy")
	}
	if overview := app.OpenProjectOverview(now.Project); overview.Overview == nil || len(overview.Overview.Cases) == 0 || overview.Overview.Cases[0].Evidence != "verified" {
		t.Fatalf("the moved evidence does not verify: %+v", overview)
	}
	// This viewer now opens the project from its new folder.
	for _, item := range knownProjects(t, app) {
		if item.Ref.ID == now.ProjectID && item.Summary.Project.Folder != now.Project {
			t.Fatalf("recents still name the previous folder: %+v", item.Summary.Project)
		}
	}
	// A destination this account cannot write in is refused, and the project
	// stays where it was.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := t.TempDir()
		if err := os.Chmod(locked, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o700) })
		refused := app.PrepareAction(storageRequest(moved.Storage.Project.Context, desktop.StorageMoveProjectAction, desktop.StorageActionOptions{Location: locked}))
		if refused.State == desktop.Completed {
			t.Fatalf("a move into an unwritable folder: %+v", refused)
		}
	}
}

// storageCandidate is what an administrator staged on this machine: a package
// and the readmit-desktop-package/v1 manifest beside it.
func storageCandidate(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "staged")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "readmit-desktop_9.9.9_" + runtime.GOARCH + ".pkg"
	payload := []byte("the bytes of a package this test never installs")
	if err := os.WriteFile(filepath.Join(directory, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	document := `{"schema":"` + upgrade.CandidateSchema + `","version":"9.9.9","os":"` + runtime.GOOS + `","arch":"` + runtime.GOARCH +
		`","signed_for_distribution":false,"packages":[{"name":"` + name + `","format":"pkg","sha256":"` + hex.EncodeToString(sum[:]) + `"}]}`
	if err := os.WriteFile(filepath.Join(directory, upgrade.CandidateDocumentName), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestPrepareUpdateRecordsTheCandidateAndInstallsNothing(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	original := projectTree(t, opened.Context.Project)
	candidate := storageCandidate(t)
	staged := projectTree(t, candidate)
	review := prepared(t, app, storageRequest(opened.Context, desktop.StoragePrepareUpdateAction, desktop.StorageActionOptions{Candidate: candidate}))
	if review.Consent != desktop.PrepareConsent || review.Storage.Upgrade == nil || review.Storage.Upgrade.Plan.Candidate != "9.9.9" ||
		review.Storage.Consequence != "Takes a verified rollback copy of this project and records this staged candidate as prepared. Nothing is installed or run." {
		t.Fatalf("prepare review: %+v", review.Storage)
	}
	prepared := executed(app, opened.Context, review, "click-prepare")
	if prepared.State != desktop.Completed || prepared.Storage == nil || prepared.Storage.Backup == nil || prepared.Storage.Candidate == nil {
		t.Fatalf("prepare: %+v", prepared)
	}
	row, candidateRecord := prepared.Storage.Backup, prepared.Storage.Candidate
	if row.Reason != desktop.BackupReasonRollback || row.Availability != desktop.ItemAvailable ||
		candidateRecord.Version != "9.9.9" || candidateRecord.OS != runtime.GOOS || candidateRecord.Arch != runtime.GOARCH ||
		candidateRecord.Path != resolved(t, candidate) || len(candidateRecord.PlanDigest) != 64 {
		t.Fatalf("the prepared rollback copy: %+v %+v", row, candidateRecord)
	}
	listed := app.ListBackups()
	if len(listed.Backups) != 1 || listed.Backups[0].Candidate == nil || *listed.Backups[0].Candidate != *candidateRecord {
		t.Fatalf("the rollback copy is not listed with its candidate: %+v", listed)
	}
	if !sameFiles(staged, projectTree(t, candidate)) || !sameFiles(original, projectTree(t, opened.Context.Project)) {
		t.Fatal("preparing an update changed the candidate or the project")
	}
	// A candidate whose package changed is not prepared, and nothing is
	// recorded.
	if err := os.WriteFile(filepath.Join(candidate, "readmit-desktop_9.9.9_"+runtime.GOARCH+".pkg"), []byte("altered"), 0o600); err != nil {
		t.Fatal(err)
	}
	altered := app.PrepareAction(storageRequest(opened.Context, desktop.StoragePrepareUpdateAction, desktop.StorageActionOptions{Candidate: candidate}))
	if altered.Review == nil || altered.Review.Ready || altered.Review.Refusal == "" {
		t.Fatalf("an altered candidate: %+v", altered)
	}
	if len(app.ListBackups().Backups) != 1 {
		t.Fatal("a refused preparation wrote a rollback copy")
	}
}

func TestRestoreCopyRecoversInPlaceAndKeepsTheReplacedDocument(t *testing.T) {
	app, _, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	copies := app.ListProjectRecoveryCopies(root)
	var earlier *desktop.ProjectRecoveryCopy
	for i, retained := range copies.Copies {
		if retained.Document == project.DocumentName && !retained.Current && retained.State == "readable" {
			earlier = &copies.Copies[i]
		}
	}
	if earlier == nil {
		t.Fatalf("no earlier copy: %+v", copies)
	}
	review := prepared(t, app, storageRequest(opened.Context, desktop.StorageRestoreCopyAction, desktop.StorageActionOptions{Document: earlier.Document, Digest: earlier.Digest}))
	if review.Consent != desktop.RestoreConsent || review.Storage.Copy == nil || review.Storage.Copy.Digest != earlier.Digest {
		t.Fatalf("review: %+v", review.Storage)
	}
	before := len(copies.Copies)
	recovered := executed(app, opened.Context, review, "click-restore-copy")
	if recovered.State != desktop.Completed {
		t.Fatalf("restore copy: %+v", recovered)
	}
	after := app.ListProjectRecoveryCopies(root)
	if len(after.Copies) != before+1 {
		t.Fatalf("the replaced document was not kept as a copy: %+v", after)
	}
	if reopened, err := project.Open(root); err != nil || reopened.Document.Settings.Title == "Scheduling investigation" {
		t.Fatalf("the earlier title did not come back: %+v %v", reopened, err)
	}
	// The project keeps its identity: recovery copies never touch the catalog.
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if document, present, err := store.Read(); err != nil || !present || document.Project.ID != opened.Context.ProjectID {
		t.Fatalf("the catalog: %+v %v", document, err)
	}
}
