package desktop_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

func TestBackupVerifyRestoreReopenThroughTheFacade(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sampleProject(t, app)
	backupDir := filepath.Join(t.TempDir(), "project.backup")
	created := app.CreateProjectBackup(desktop.BackupCreateRequest{Project: root, Destination: backupDir})
	if created.State != desktop.Completed || created.Report == nil || !created.Report.Complete {
		t.Fatalf("create: %+v", created)
	}
	if len(created.Report.Evidence) == 0 {
		t.Fatal("backup report listed no canonical evidence")
	}
	if len(created.Report.Mutable) == 0 {
		t.Fatal("backup report listed no mutable project documents")
	}
	verified := app.VerifyProjectBackup(backupDir)
	if verified.State != desktop.Completed || verified.Report == nil || !verified.Report.Complete {
		t.Fatalf("verify: %+v", verified)
	}
	restoredPath := filepath.Join(t.TempDir(), "recovered")
	restored := app.RestoreProjectBackup(desktop.BackupRestoreRequest{Backup: backupDir, Destination: restoredPath})
	if restored.State != desktop.Completed || restored.Report == nil || !restored.Report.Complete {
		t.Fatalf("restore: %+v", restored)
	}
	opened := app.OpenProjectOverview(restored.Report.Root)
	if opened.State != desktop.Completed && opened.State != desktop.Empty {
		t.Fatalf("reopen restored project: %+v", opened)
	}
	if opened.Overview == nil || opened.Overview.Title == "" {
		t.Fatalf("restored overview missing: %+v", opened)
	}
}

func TestBackupRefusesDangerousAndMissingDestinations(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sampleProject(t, app)
	inside := app.CreateProjectBackup(desktop.BackupCreateRequest{Project: root, Destination: filepath.Join(root, "inside-backup")})
	if inside.State != desktop.Failed {
		t.Fatalf("backup inside project must fail: %+v", inside)
	}
	empty := app.CreateProjectBackup(desktop.BackupCreateRequest{Project: root, Destination: ""})
	if empty.State != desktop.Failed {
		t.Fatalf("empty destination must fail: %+v", empty)
	}
	missing := app.VerifyProjectBackup(filepath.Join(t.TempDir(), "absent"))
	if missing.State != desktop.Failed {
		t.Fatalf("missing backup must fail: %+v", missing)
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.backup")
	if err := os.Mkdir(corrupt, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, "backup.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := app.VerifyProjectBackup(corrupt)
	if bad.State != desktop.Failed {
		t.Fatalf("corrupt backup must fail: %+v", bad)
	}
	usable := filepath.Join(t.TempDir(), "still-usable")
	if err := os.Mkdir(usable, 0700); err != nil {
		t.Fatal(err)
	}
	overwrite := app.RestoreProjectBackup(desktop.BackupRestoreRequest{
		Backup:      corrupt,
		Destination: usable,
	})
	if overwrite.State != desktop.Failed {
		t.Fatalf("corrupt restore must fail: %+v", overwrite)
	}
	if entries, err := os.ReadDir(usable); err != nil || len(entries) != 0 {
		t.Fatalf("failed restore must not replace usable destination: %v %v", entries, err)
	}
}

func TestCancelledMaintenancePathAndStaleDelete(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sampleProject(t, app)
	cancelled := newApp(t, &chooser{}).ChooseMaintenancePath("backup-destination")
	if cancelled.State != desktop.Cancelled {
		t.Fatalf("dismissed save dialog: %+v", cancelled)
	}
	preview := app.PreviewProjectRetirement(root)
	if preview.State != desktop.Completed || preview.Preview == nil || preview.Preview.Selection == "" {
		t.Fatalf("preview: %+v", preview)
	}
	if !strings.Contains(preview.Preview.NotErasure, "not forensic") {
		t.Fatalf("preview must refuse secure-erasure claims: %+v", preview.Preview)
	}
	noConfirm := app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{
		Project:     root,
		Destination: filepath.Join(t.TempDir(), "archive-a"),
		Selection:   preview.Preview.Selection,
		Delete:      true,
		Confirm:     false,
	})
	if noConfirm.State != desktop.Failed {
		t.Fatalf("delete without confirm: %+v", noConfirm)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("source must remain when delete is not confirmed")
	}
	// Stale selection: change a mutable document after preview.
	opened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	doc := opened.Document
	doc.Settings.Title = doc.Settings.Title + " changed"
	if err := project.WriteDocument(root, doc); err != nil {
		t.Fatal(err)
	}
	stale := app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{
		Project:     root,
		Destination: filepath.Join(t.TempDir(), "archive-b"),
		Selection:   preview.Preview.Selection,
		Delete:      true,
		Confirm:     true,
	})
	if stale.State != desktop.Failed || !strings.Contains(stale.Reason, "changed since") {
		t.Fatalf("stale delete: %+v", stale)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("stale delete must retain the source")
	}
	fresh := app.PreviewProjectRetirement(root)
	if fresh.State != desktop.Completed {
		t.Fatalf("fresh preview: %+v", fresh)
	}
	archived := app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{
		Project:     root,
		Destination: filepath.Join(t.TempDir(), "archive-c"),
		Selection:   fresh.Preview.Selection,
		Delete:      false,
	})
	if archived.State != desktop.Completed || archived.Report == nil || !archived.Report.Complete {
		t.Fatalf("archive: %+v", archived)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("archive must keep the source")
	}
}

func TestQuotaMigrationAndUpgradeHandoff(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sampleProject(t, app)
	quota := app.InspectProjectQuota(root)
	if quota.State != desktop.Completed || quota.Quota == nil || quota.Quota.Explain == "" {
		t.Fatalf("quota: %+v", quota)
	}
	if !strings.Contains(quota.Quota.Explain, "disposable") {
		t.Fatalf("quota must explain disposable indexes: %+v", quota.Quota)
	}
	set := app.SetProjectQuota(desktop.ProjectQuotaChange{Project: root, MaxBytes: 50_000_000, MaxFiles: 10_000})
	if set.State != desktop.Completed || set.Quota == nil || !set.Quota.Declared || !set.Quota.Within {
		t.Fatalf("set quota: %+v", set)
	}
	migration := app.PreviewProjectMigration(root)
	if migration.State != desktop.Completed || migration.Plan == nil || !migration.Plan.Compatible {
		t.Fatalf("migration: %+v", migration)
	}
	if !strings.Contains(migration.Guidance, "no silent rewrite") {
		t.Fatalf("migration guidance: %+v", migration)
	}
	check := app.CheckStagedUpgrade(desktop.UpgradeCheckRequest{Candidate: "", Projects: []string{root}})
	if check.State != desktop.Failed {
		t.Fatalf("empty candidate: %+v", check)
	}
	nothing := app.CheckStagedUpgrade(desktop.UpgradeCheckRequest{Candidate: t.TempDir()})
	if nothing.State != desktop.Failed || !strings.Contains(nothing.Reason, "at least one") {
		t.Fatalf("review of nothing: %+v", nothing)
	}
	unapproved := app.PrepareStagedUpgrade(desktop.UpgradePrepareRequest{
		Project:     root,
		Candidate:   t.TempDir(),
		Destination: filepath.Join(t.TempDir(), "rollback"),
		Approve:     false,
	})
	if unapproved.State != desktop.Failed || !strings.Contains(unapproved.Reason, "approval") {
		t.Fatalf("prepare without approve: %+v", unapproved)
	}
}

func TestChooseMaintenancePathKinds(t *testing.T) {
	existing := t.TempDir()
	named := filepath.Join(t.TempDir(), "new-folder")
	c := &chooser{folder: existing, destination: named}
	app := newApp(t, c)
	for kind, want := range map[string]string{
		"backup-destination": named, "restore-destination": named, "archive-destination": named,
		"backup-source": existing, "upgrade-candidate": existing,
	} {
		result := app.ChooseMaintenancePath(kind)
		if result.State != desktop.Completed || result.Path != want || result.Kind != kind {
			t.Fatalf("%s: %+v", kind, result)
		}
	}
	unknown := app.ChooseMaintenancePath("network-update")
	if unknown.State != desktop.Failed {
		t.Fatalf("unknown kind: %+v", unknown)
	}
}

// The recovery copies a project retained are listed through the reader
// RecoverProjectDocument restores them with, and one is recovered: the
// project reads its earlier settings again and the document it replaced is
// listed as another copy. A damaged copy is listed as damaged and refused in
// the words the command line refuses it with, changing nothing, and a folder
// that is no project lists nothing.
func TestRecoveryCopiesAreListedAndRecoveredThroughTheFacade(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sampleProject(t, app)
	if listed := app.ListProjectRecoveryCopies(root); listed.State != desktop.Empty || len(listed.Copies) != 0 {
		t.Fatalf("a project nothing replaced: %+v", listed)
	}
	original, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	title := opened.Document.Settings.Title
	renamed := opened.Document
	renamed.Settings.Title = "Renamed by mistake"
	if err := project.WriteDocument(root, renamed); err != nil {
		t.Fatal(err)
	}
	listed := app.ListProjectRecoveryCopies(root)
	sum := sha256.Sum256(original)
	want := desktop.ProjectRecoveryCopy{Document: project.DocumentName, Digest: hex.EncodeToString(sum[:]), Size: int64(len(original)), State: "readable"}
	if listed.State != desktop.Completed || len(listed.Copies) != 1 || listed.Copies[0] != want {
		t.Fatalf("listed %+v, want %+v", listed, want)
	}

	recovered := app.RecoverProjectDocument(desktop.ProjectRecoverRequest{Project: root, Document: project.DocumentName, Digest: want.Digest})
	if recovered.State != desktop.Completed {
		t.Fatalf("recover: %+v", recovered)
	}
	if overview := app.OpenProjectOverview(root); overview.Overview == nil || overview.Overview.Title != title {
		t.Fatalf("the recovered project reads %+v, want the title %q", overview, title)
	}
	listed = app.ListProjectRecoveryCopies(root)
	if listed.State != desktop.Completed || len(listed.Copies) != 2 {
		t.Fatalf("after recovery: %+v", listed)
	}
	for _, retained := range listed.Copies {
		if retained.Current != (retained.Digest == want.Digest) || retained.State != "readable" {
			t.Fatalf("after recovery the copies read %+v", listed.Copies)
		}
	}

	if err := os.WriteFile(filepath.Join(root, project.DocumentName+".recovery-"+want.Digest), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	listed = app.ListProjectRecoveryCopies(root)
	damaged := false
	for _, retained := range listed.Copies {
		damaged = damaged || retained.Digest == want.Digest && retained.State == "damaged" && !retained.Current
	}
	if !damaged {
		t.Fatalf("a damaged copy was not listed as damaged: %+v", listed)
	}
	refused := app.RecoverProjectDocument(desktop.ProjectRecoverRequest{Project: root, Document: project.DocumentName, Digest: want.Digest})
	if refused.State != desktop.Failed || refused.Reason != "recovery copy is damaged" {
		t.Fatalf("recovering a damaged copy: %+v", refused)
	}
	if after, err := os.ReadFile(filepath.Join(root, project.DocumentName)); err != nil || string(after) != string(current) {
		t.Fatal("a refused recovery changed the current document")
	}
	if none := app.ListProjectRecoveryCopies(t.TempDir()); none.State != desktop.Failed || len(none.Copies) != 0 {
		t.Fatalf("a folder that is no project: %+v", none)
	}
}

// A backup report lists a recovery copy with the mutable project documents
// because the document store names it one; a file that only resembles a
// copy's name is not a recovery copy, and is not listed as one.
func TestBackupReportListsRecoveryCopiesTheStoreNames(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sampleProject(t, app)
	original, err := os.ReadFile(filepath.Join(root, project.DocumentName))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	renamed := opened.Document
	renamed.Settings.Title = "Renamed"
	if err := project.WriteDocument(root, renamed); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	recoveryCopy := project.DocumentName + ".recovery-" + hex.EncodeToString(sum[:])
	resembling := project.DocumentName + ".recovery-not-a-digest"
	if err := os.WriteFile(filepath.Join(root, resembling), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := app.CreateProjectBackup(desktop.BackupCreateRequest{Project: root, Destination: filepath.Join(t.TempDir(), "project.backup")})
	if created.State != desktop.Completed || created.Report == nil {
		t.Fatalf("create: %+v", created)
	}
	listed := func(entries []desktop.BackupInventoryEntry, name string) bool {
		for _, entry := range entries {
			if entry.Path == name {
				return true
			}
		}
		return false
	}
	if !listed(created.Report.Mutable, recoveryCopy) || listed(created.Report.Other, recoveryCopy) {
		t.Fatalf("the recovery copy was not listed as a mutable project document: %+v", created.Report)
	}
	if listed(created.Report.Mutable, resembling) {
		t.Fatalf("a file resembling a recovery copy was listed as one: %+v", created.Report)
	}
}
