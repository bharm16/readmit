package desktop_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// storageProject is a project holding the frozen regression case under a
// title, opened once so its catalog records its identity: the project a
// person backs up from Storage.
func storageProject(t *testing.T, app *desktop.App, title string) desktop.ProjectOpenResult {
	t.Helper()
	// The sample family is written through a window of its own, so the
	// folder the window under test chose keeps only what it wrote there.
	root := sampleProject(t, newApp(t, &chooser{folder: t.TempDir()}))
	opened, err := project.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	opened.Document.Settings.Title = title
	if err := opened.Save(opened.Document); err != nil {
		t.Fatal(err)
	}
	result := app.OpenNamedProject(root)
	if result.State != desktop.Completed || !result.Recorded {
		t.Fatalf("open project: %+v", result)
	}
	return result
}

// storageApp is an app whose backups are kept in a folder it chose.
func storageApp(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	state, location := t.TempDir(), t.TempDir()
	app := activatedApp(t, &chooser{folder: location}, state)
	if chosen := app.ChooseBackupLocation(); chosen.State != desktop.Completed || chosen.Location != location {
		t.Fatalf("choose location: %+v", chosen)
	}
	return app, location, state
}

func backedUp(t *testing.T, app *desktop.App, opened desktop.ProjectOpenResult) desktop.StorageBackup {
	t.Helper()
	created := app.BackupProject(desktop.StorageBackupRequest{Context: opened.Context})
	if created.State != desktop.Completed || created.Backup == nil || created.Backup.Availability != desktop.ItemAvailable {
		t.Fatalf("backup: %+v", created)
	}
	return *created.Backup
}

func TestBackupLocationIsRememberedAndProbed(t *testing.T) {
	state, location := t.TempDir(), t.TempDir()
	dialogs := &chooser{folder: location}
	app := activatedApp(t, dialogs, state)
	if unset := app.BackupLocation(); unset.State != desktop.Empty || unset.Reason == "" {
		t.Fatalf("a location nobody chose: %+v", unset)
	}
	opened := storageProject(t, app, "Scheduling investigation")
	// With no location the backup asks for one and writes nothing; no dialog
	// opens on its own.
	asked := len(dialogs.titles)
	if refused := app.BackupProject(desktop.StorageBackupRequest{Context: opened.Context}); refused.State != desktop.Empty || refused.Reason == "" || len(dialogs.titles) != asked {
		t.Fatalf("a backup without a location: %+v %v", refused, dialogs.titles)
	}
	if listed := app.ListBackups(); listed.State != desktop.Empty || len(listed.Backups) != 0 {
		t.Fatalf("backups before any: %+v", listed)
	}
	if chosen := app.ChooseBackupLocation(); chosen.State != desktop.Completed || chosen.Location != location {
		t.Fatalf("choose: %+v", chosen)
	}
	// A dismissed dialog keeps what was chosen before.
	dialogs.folder = ""
	if dismissed := app.ChooseBackupLocation(); dismissed.State != desktop.Cancelled {
		t.Fatalf("dismissed: %+v", dismissed)
	}
	// Another window over the same shell state remembers the location, and
	// backups are kept in it.
	again := activatedApp(t, &chooser{}, state)
	if remembered := again.BackupLocation(); remembered.State != desktop.Completed || remembered.Location != location {
		t.Fatalf("remembered: %+v", remembered)
	}
	made := backedUp(t, again, opened)
	if filepath.Dir(made.Folder) != resolved(t, location) {
		t.Fatalf("the backup was not kept in the chosen folder: %+v", made)
	}
	// A location that is gone is not offered, and is not created.
	if err := os.RemoveAll(location); err != nil {
		t.Fatal(err)
	}
	if gone := again.BackupLocation(); gone.State != desktop.Empty || gone.Reason == "" {
		t.Fatalf("a location that is gone: %+v", gone)
	}
	if refused := again.BackupProject(desktop.StorageBackupRequest{Context: opened.Context}); refused.State != desktop.Empty {
		t.Fatalf("a backup into a location that is gone: %+v", refused)
	}
	if _, err := os.Lstat(location); err == nil {
		t.Fatal("a location that was gone was created")
	}
	// A storage document this release cannot read is reported and left as
	// it was written, never replaced by a choice.
	written := filepath.Join(state, "storage.json")
	if err := os.WriteFile(written, []byte(`{"schema":"readmit-desktop-storage/v2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if unreadable := again.BackupLocation(); unreadable.State != desktop.Failed {
		t.Fatalf("an unreadable storage document: %+v", unreadable)
	}
	if unreadable := again.ListBackups(); unreadable.State != desktop.Failed {
		t.Fatalf("backups from an unreadable storage document: %+v", unreadable)
	}
	dialogs.folder = t.TempDir()
	if refused := app.ChooseBackupLocation(); refused.State != desktop.Failed {
		t.Fatalf("a choice over an unreadable storage document: %+v", refused)
	}
	if kept := mustRead(t, written); string(kept) != `{"schema":"readmit-desktop-storage/v2"}` {
		t.Fatalf("the unreadable document was replaced: %s", kept)
	}
}

func TestBackupsListNamesEveryBackupWithItsProblem(t *testing.T) {
	app, location, _ := storageApp(t)
	times := []time.Time{time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC), time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC), time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)}
	next := 0
	desktop.SetClockForTest(app, func() time.Time { return times[min(next, len(times)-1)] })
	opened := storageProject(t, app, "Scheduling investigation")
	made := make([]desktop.StorageBackup, 0, len(times))
	for range times {
		made = append(made, backedUp(t, app, opened))
		next++
	}
	if listed := app.ListBackups(); listed.State != desktop.Completed || len(listed.Backups) != len(times) ||
		listed.Backups[0].ID != made[3].ID || listed.Backups[3].ID != made[0].ID {
		t.Fatalf("backups are not newest first: %+v", listed)
	}
	for _, row := range made {
		if row.Project != "Scheduling investigation" || row.ProjectID != opened.Context.ProjectID || row.Size <= 0 ||
			row.CreatedAt == nil || row.Reason != desktop.BackupReasonBackup {
			t.Fatalf("row: %+v", row)
		}
	}
	// Missing: the folder is gone.
	if err := os.RemoveAll(made[0].Folder); err != nil {
		t.Fatal(err)
	}
	// Damaged: the seal no longer matches the manifest.
	if err := os.WriteFile(filepath.Join(made[1].Folder, backup.DocumentName), []byte(`{"schema":"readmit-backup/v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Incomplete: the completion marker is gone.
	if err := os.Remove(filepath.Join(made[2].Folder, backup.MarkerName)); err != nil {
		t.Fatal(err)
	}
	// Symbolic link: a recorded backup replaced by a link to a real one.
	elsewhere := filepath.Join(t.TempDir(), "real")
	if err := os.Rename(made[3].Folder, elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, made[3].Folder); err != nil {
		t.Fatal(err)
	}
	// An unrecorded backup someone copied in is listed with no creation date.
	copied := filepath.Join(location, "copied-in")
	if err := os.CopyFS(copied, os.DirFS(elsewhere)); err != nil {
		t.Fatal(err)
	}
	// An unrelated folder is no backup and is not listed.
	if err := os.Mkdir(filepath.Join(location, "holiday photos"), 0o700); err != nil {
		t.Fatal(err)
	}
	listed := app.ListBackups()
	if listed.State != desktop.Completed || len(listed.Backups) != 5 {
		t.Fatalf("every backup is not a row: %+v", listed)
	}
	want := map[string]desktop.Availability{
		made[0].ID: desktop.ItemMissing, made[1].ID: desktop.ItemUnreadable,
		made[2].ID: desktop.BackupIncomplete, made[3].ID: desktop.ItemUnreadable,
	}
	for _, row := range listed.Backups {
		if row.Folder == resolved(t, copied) {
			if row.Availability != desktop.ItemAvailable || row.CreatedAt != nil || row.Project != "Scheduling investigation" || row.Reason != "" {
				t.Fatalf("an unrecorded backup: %+v", row)
			}
			continue
		}
		if availability, known := want[row.ID]; !known || row.Availability != availability || row.Problem == "" {
			t.Fatalf("row %+v, want %s", row, availability)
		}
	}
	// The unrecorded one, having no creation date, sorts after every dated row.
	if last := listed.Backups[len(listed.Backups)-1]; last.CreatedAt != nil {
		t.Fatalf("an undated backup sorted before a dated one: %+v", listed.Backups)
	}
	// Explicit verification reads every byte and names what it found.
	if verified := app.InspectBackup(made[1].ID); verified.State != desktop.Failed || verified.Reason == "" {
		t.Fatalf("verify a damaged backup: %+v", verified)
	}
	for _, row := range listed.Backups {
		if row.Availability == desktop.ItemAvailable {
			if verified := app.InspectBackup(row.ID); verified.State != desktop.Completed || verified.Report == nil || !verified.Report.Complete {
				t.Fatalf("verify: %+v", verified)
			}
		}
	}
	if unknown := app.InspectBackup("not-a-backup"); unknown.State != desktop.Failed {
		t.Fatalf("an unknown backup: %+v", unknown)
	}
}

func TestABackupNeverOverwritesAndACancelledOneIsIncomplete(t *testing.T) {
	app, location, _ := storageApp(t)
	opened := storageProject(t, app, "Scheduling investigation")
	first := backedUp(t, app, opened)
	before := mustRead(t, filepath.Join(first.Folder, backup.MarkerName))
	second := backedUp(t, app, opened)
	if second.Folder == first.Folder || second.ID == first.ID {
		t.Fatalf("a second backup reused the first one's folder: %+v %+v", first, second)
	}
	if after := mustRead(t, filepath.Join(first.Folder, backup.MarkerName)); string(after) != string(before) {
		t.Fatal("a second backup changed the first")
	}
	// A cancelled backup keeps what it wrote, is never recorded, and lists
	// as incomplete.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := desktop.BackupProjectWithinForTest(app, cancelled, desktop.StorageBackupRequest{Context: opened.Context})
	if stopped.State != desktop.Cancelled {
		t.Fatalf("a cancelled backup: %+v", stopped)
	}
	listed := app.ListBackups()
	complete := 0
	for _, row := range listed.Backups {
		switch row.Availability {
		case desktop.ItemAvailable:
			complete++
		case desktop.BackupIncomplete:
			if row.CreatedAt != nil || row.Reason != "" {
				t.Fatalf("a cancelled backup was recorded: %+v", row)
			}
		default:
			t.Fatalf("row: %+v", row)
		}
	}
	if complete != 2 {
		t.Fatalf("complete backups: %+v", listed.Backups)
	}
	entries, err := os.ReadDir(location)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Contains(names, filepath.Base(first.Folder)) || !slices.Contains(names, filepath.Base(second.Folder)) {
		t.Fatalf("location: %v", names)
	}
	// A backup's folder is shown, and a backup nobody listed is not.
	var shown string
	desktop.SetRevealForTest(app, func(path string) error { shown = path; return nil })
	if revealed := app.RevealBackup(first.ID); revealed.State != desktop.Completed || shown != first.Folder {
		t.Fatalf("reveal: %+v %q", revealed, shown)
	}
	if refused := app.RevealBackup("unknown"); refused.State != desktop.Failed {
		t.Fatalf("reveal unknown: %+v", refused)
	}
}
