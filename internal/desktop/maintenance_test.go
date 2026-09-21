package desktop_test

import (
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
		t.Fatalf("dismissed picker: %+v", cancelled)
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
	dest := t.TempDir()
	c := &chooser{folder: dest}
	app := newApp(t, c)
	for _, kind := range []string{"backup-destination", "restore-destination", "backup-source", "upgrade-candidate", "archive-destination"} {
		result := app.ChooseMaintenancePath(kind)
		if result.State != desktop.Completed || result.Path != dest || result.Kind != kind {
			t.Fatalf("%s: %+v", kind, result)
		}
	}
	unknown := app.ChooseMaintenancePath("network-update")
	if unknown.State != desktop.Failed {
		t.Fatalf("unknown kind: %+v", unknown)
	}
}
