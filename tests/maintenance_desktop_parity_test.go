package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/backup"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/upgrade"
)

// refusedAlike holds one refusal the window reports to the refusal the
// command line prints for the same request: the same sentence on standard
// error, a non-zero exit status, and whatever plan the command printed before
// it — which is the plan the window answered, or nothing when it answered none.
func refusedAlike(t *testing.T, what string, window desktop.UpgradeResult, stdout, stderr string, err error) {
	t.Helper()
	if window.State != desktop.Failed || err == nil || stderr != "readmit: "+window.Reason+"\n" {
		t.Fatalf("%s: the window answered %q (%s), the command line %v %q", what, window.Reason, window.State, err, stderr)
	}
	plan := ""
	if window.View != nil && window.View.Plan != nil {
		encoded, encodeErr := upgrade.Encode(*window.View.Plan)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		plan = string(encoded)
	}
	if !strings.HasPrefix(stdout, plan) || plan == "" && stdout != "" {
		t.Fatalf("%s: the command line printed a plan the window did not answer:\nwindow: %s\ncommand: %s", what, plan, stdout)
	}
}

// The upgrade screen's check is `readmit upgrade check`: over one staged
// candidate and the project it reviews, the plan the window answers is the
// document the command prints, byte for byte, and each refusal is the
// command's refusal in its words — a development preview, a package that was
// not staged whole, a file the manifest does not record, and a manifest of a
// later version. The evidence reviewed is left exactly as it was, and the
// check needs no license term.
func TestTheWindowsUpgradeCheckIsTheCommandLinesPlan(t *testing.T) {
	root := indexedProject(t)
	before := treeDigests(t, root)
	app := unlicensedDesktopApp(t, t.TempDir())

	candidate := stagedCandidate(t, true, "9.9.9")
	window := app.CheckStagedUpgrade(desktop.UpgradeCheckRequest{Candidate: candidate, Projects: []string{root}})
	if window.State != desktop.Completed || window.View == nil || window.View.Plan == nil || window.View.Plan.State != upgrade.Ready {
		t.Fatalf("the window checked a staged candidate as %+v", window)
	}
	plan, err := upgrade.Encode(*window.View.Plan)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "upgrade", "check", "--candidate", candidate, "--project", root)
	if err != nil || stderr != "" || stdout != string(plan) {
		t.Fatalf("readmit upgrade check printed another plan: %v %s\nwindow: %s\ncommand: %s", err, stderr, plan, stdout)
	}

	preview := stagedCandidate(t, false, "9.9.9")
	unsigned := app.CheckStagedUpgrade(desktop.UpgradeCheckRequest{Candidate: preview, Projects: []string{root}})
	if unsigned.Reason != upgrade.ErrUnsigned.Error() {
		t.Fatalf("a development preview: %+v", unsigned)
	}
	stdout, stderr, err = run(t, "upgrade", "check", "--candidate", preview, "--project", root)
	refusedAlike(t, "a development preview", unsigned, stdout, stderr, err)

	altered := stagedCandidate(t, true, "9.9.9")
	if err := os.WriteFile(filepath.Join(altered, "readmit-desktop_9.9.9_"+runtime.GOARCH+".pkg"), []byte("a download that stopped partway"), 0o600); err != nil {
		t.Fatal(err)
	}
	partial := app.CheckStagedUpgrade(desktop.UpgradeCheckRequest{Candidate: altered, Projects: []string{root}})
	if partial.Reason != upgrade.ErrNotStaged.Error() || partial.View == nil || partial.View.Plan.Staged[0].State != upgrade.Altered {
		t.Fatalf("a package not staged whole: %+v", partial)
	}
	stdout, stderr, err = run(t, "upgrade", "check", "--candidate", altered, "--project", root)
	refusedAlike(t, "a package not staged whole", partial, stdout, stderr, err)

	unrecorded := stagedCandidate(t, true, "9.9.9")
	if err := os.WriteFile(filepath.Join(unrecorded, "readmit-desktop_9.9.8_"+runtime.GOARCH+".pkg"), []byte("an older package"), 0o600); err != nil {
		t.Fatal(err)
	}
	beside := app.CheckStagedUpgrade(desktop.UpgradeCheckRequest{Candidate: unrecorded, Projects: []string{root}})
	if beside.View != nil {
		t.Fatalf("a candidate holding an unrecorded file was read as a candidate: %+v", beside)
	}
	stdout, stderr, err = run(t, "upgrade", "check", "--candidate", unrecorded, "--project", root)
	refusedAlike(t, "a file the manifest does not record", beside, stdout, stderr, err)

	later := stagedCandidate(t, true, "9.9.9")
	manifest := filepath.Join(later, upgrade.CandidateDocumentName)
	document, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(strings.Replace(string(document), upgrade.CandidateSchema, "readmit-desktop-package/v2", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	newer := app.CheckStagedUpgrade(desktop.UpgradeCheckRequest{Candidate: later, Projects: []string{root}})
	stdout, stderr, err = run(t, "upgrade", "check", "--candidate", later, "--project", root)
	refusedAlike(t, "a later manifest version", newer, stdout, stderr, err)

	sameTree(t, "project", before, treeDigests(t, root))
}

// The rollback archive the upgrade screen prepares is the archive `readmit
// upgrade prepare` takes: after the same approval, over the same candidate and
// project, both archives hold the same backup document and completion marker
// byte for byte, `readmit backup verify` verifies the window's with the
// identity the project recorded, and both say installing the development
// preview is still refused. Nothing is written without the approval, for a
// candidate not staged whole, inside the project or over an existing folder,
// and each of those refusals is the command's, in its words. The project is
// left exactly as it was, and no license term is needed.
func TestTheWindowsRollbackArchiveIsTheOneUpgradePrepareTakes(t *testing.T) {
	root := indexedProject(t)
	before := treeDigests(t, root)
	app := unlicensedDesktopApp(t, t.TempDir())
	candidate := stagedCandidate(t, false, "9.9.9")
	archives := t.TempDir()
	windowArchive := filepath.Join(archives, "window-rollback")

	unapproved := app.PrepareStagedUpgrade(desktop.UpgradePrepareRequest{Project: root, Candidate: candidate, Destination: windowArchive})
	if unapproved.State != desktop.Failed || !strings.Contains(unapproved.Reason, "administrator approval") {
		t.Fatalf("a rollback archive without approval: %+v", unapproved)
	}
	if _, err := os.Lstat(windowArchive); !os.IsNotExist(err) {
		t.Fatal("an unapproved preparation wrote its archive anyway")
	}

	window := app.PrepareStagedUpgrade(desktop.UpgradePrepareRequest{Project: root, Candidate: candidate, Destination: windowArchive, Approve: true})
	stillRefused := "Installing this candidate is still refused: " + upgrade.ErrUnsigned.Error()
	if window.State != desktop.Completed || window.Report == nil || !window.Report.Complete || window.Reason != "Rollback point taken. "+stillRefused {
		t.Fatalf("the window prepared %+v", window)
	}
	plan, err := upgrade.Encode(*window.View.Plan)
	if err != nil {
		t.Fatal(err)
	}
	commandArchive := filepath.Join(archives, "command-rollback")
	stdout, stderr, err := run(t, "upgrade", "prepare", root, "--candidate", candidate, "--output", commandArchive, "--approve")
	if err != nil || stderr != "" || !strings.HasPrefix(stdout, string(plan)) || !strings.HasSuffix(stdout, stillRefused+"\n") {
		t.Fatalf("readmit upgrade prepare: %v %s\n%s", err, stderr, stdout)
	}
	for _, name := range []string{backup.DocumentName, backup.MarkerName} {
		fromWindow, windowErr := os.ReadFile(filepath.Join(windowArchive, name))
		fromCommand, commandErr := os.ReadFile(filepath.Join(commandArchive, name))
		if windowErr != nil || commandErr != nil || string(fromWindow) != string(fromCommand) {
			t.Fatalf("the window's %s is not the command line's: %v %v", name, windowErr, commandErr)
		}
	}
	if !reflect.DeepEqual(treeDigests(t, windowArchive), treeDigests(t, commandArchive)) {
		t.Fatal("the window's rollback archive holds other files than the command line's")
	}
	stdout, stderr, err = run(t, "backup", "verify", windowArchive)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Complete: yes") ||
		!strings.Contains(stdout, "regression kind=case evidence=verified identity="+frozenRegressionIdentity) {
		t.Fatalf("readmit backup verify read the window's archive as: %v %s\n%s", err, stderr, stdout)
	}

	absent := stagedCandidate(t, true, "9.9.9")
	if err := os.Remove(filepath.Join(absent, "readmit-desktop_9.9.9_"+runtime.GOARCH+".pkg")); err != nil {
		t.Fatal(err)
	}
	for _, refused := range []struct {
		what, candidate, destination string
	}{
		{"a candidate not staged whole", absent, filepath.Join(archives, "not-staged")},
		{"an archive inside the project", candidate, filepath.Join(root, "rollback")},
		{"an archive over an existing folder", candidate, windowArchive},
	} {
		existed := treeDigests(t, archives)
		answer := app.PrepareStagedUpgrade(desktop.UpgradePrepareRequest{Project: root, Candidate: refused.candidate, Destination: refused.destination, Approve: true})
		stdout, stderr, err := run(t, "upgrade", "prepare", root, "--candidate", refused.candidate, "--output", refused.destination, "--approve")
		refusedAlike(t, refused.what, answer, stdout, stderr, err)
		if answer.Report != nil || !reflect.DeepEqual(existed, treeDigests(t, archives)) {
			t.Fatalf("%s wrote an archive: %+v", refused.what, answer)
		}
		sameTree(t, "project", before, treeDigests(t, root))
	}
}

// The recovery copies the maintenance screen lists are the copies `readmit
// project recover` selects by digest, and recovering one through the window
// leaves the project as the command line's recovery of the same copy leaves an
// identical project: the same documents, the same copies beside them, and the
// settings `readmit project show` reads. Every refusal is the command's, in its
// words, and changes nothing.
func TestTheWindowsRecoveryCopiesAreTheOnesProjectRecoverRestores(t *testing.T) {
	windowRoot, commandRoot := newProject(t), newProject(t)
	for _, root := range []string{windowRoot, commandRoot} {
		if _, stderr, err := run(t, "project", "settings", root, "--title", "Renamed by mistake"); err != nil {
			t.Fatalf("project settings: %v %s", err, stderr)
		}
	}
	app := unlicensedDesktopApp(t, t.TempDir())
	listed := app.ListProjectRecoveryCopies(windowRoot)
	if listed.State != desktop.Completed || len(listed.Copies) != 1 || listed.Copies[0].Document != project.DocumentName || listed.Copies[0].State != "readable" {
		t.Fatalf("the window listed %+v", listed)
	}
	digest := listed.Copies[0].Digest
	if _, err := os.Lstat(filepath.Join(commandRoot, project.DocumentName+".recovery-"+digest)); err != nil {
		t.Fatalf("the window listed a copy the command line cannot select: %v", err)
	}

	if recovered := app.RecoverProjectDocument(desktop.ProjectRecoverRequest{Project: windowRoot, Document: project.DocumentName, Digest: digest}); recovered.State != desktop.Completed {
		t.Fatalf("the window recovered %+v", recovered)
	}
	stdout, stderr, err := run(t, "project", "recover", commandRoot, "--document", project.DocumentName, "--digest", digest)
	if err != nil || stderr != "" || stdout != "Document recovered; previous bytes retained.\n" {
		t.Fatalf("readmit project recover: %v %s %s", err, stdout, stderr)
	}
	if !reflect.DeepEqual(treeDigests(t, windowRoot), treeDigests(t, commandRoot)) {
		t.Fatal("the window's recovery left another project than the command line's")
	}
	shown, _, err := run(t, "project", "show", windowRoot)
	if err != nil || !strings.HasPrefix(shown, "Project: Epic scheduling interface\n") {
		t.Fatalf("readmit project show read the recovered project as: %v\n%s", err, shown)
	}

	for _, root := range []string{windowRoot, commandRoot} {
		if err := os.WriteFile(filepath.Join(root, project.DocumentName+".recovery-"+digest), []byte("damaged"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	missing := strings.Repeat("0", 64)
	for _, refused := range []struct{ what, document, digest string }{
		{"a damaged copy", project.DocumentName, digest},
		{"a copy that is not there", project.DocumentName, missing},
		{"a digest in capitals", project.DocumentName, strings.ToUpper(digest)},
		{"a document that keeps no copies", "notes.json", digest},
	} {
		before := treeDigests(t, windowRoot)
		answer := app.RecoverProjectDocument(desktop.ProjectRecoverRequest{Project: windowRoot, Document: refused.document, Digest: refused.digest})
		_, stderr, err := run(t, "project", "recover", commandRoot, "--document", refused.document, "--digest", refused.digest)
		if answer.State != desktop.Failed || err == nil || stderr != "readmit: "+answer.Reason+"\n" {
			t.Fatalf("%s: the window answered %+v, the command line %v %q", refused.what, answer, err, stderr)
		}
		if !reflect.DeepEqual(before, treeDigests(t, windowRoot)) || !reflect.DeepEqual(before, treeDigests(t, commandRoot)) {
			t.Fatalf("%s changed a project", refused.what)
		}
	}
}

// The archive-and-migrate and storage sections are `readmit project
// migration-preview`, `quota`, `archive` and `delete`. Over two identical
// projects, the window's migration plan is the document the command prints,
// the quota it declares is the declaration the command writes and its refusal
// of one the project cannot fit is the command's, and its archive and its
// delete leave what the command's leave: the same backup document and marker,
// and the project unlinked. A delete whose preview went stale deletes nothing.
// The projects register the same frozen case and hold no index, whose build
// time would tell them apart.
func TestTheWindowsArchiveDeleteMigrationAndQuotaAreTheCommandLines(t *testing.T) {
	windowRoot, commandRoot := newProject(t), newProject(t)
	for _, root := range []string{windowRoot, commandRoot} {
		registerFrozenCase(t, root, "regression")
	}
	app := desktopApp(t, t.TempDir())

	migration := app.PreviewProjectMigration(windowRoot)
	if migration.State != desktop.Completed || migration.Plan == nil {
		t.Fatalf("the window previewed %+v", migration)
	}
	plan, err := json.Marshal(*migration.Plan, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "project", "migration-preview", commandRoot)
	if err != nil || stderr != "" || stdout != string(plan)+"\n" {
		t.Fatalf("readmit project migration-preview printed another plan: %v %s\nwindow: %s\ncommand: %s", err, stderr, plan, stdout)
	}

	tooSmall := app.SetProjectQuota(desktop.ProjectQuotaChange{Project: windowRoot, MaxBytes: 1000, MaxFiles: 5000})
	_, stderr, err = run(t, "project", "quota", commandRoot, "--max-bytes", "1000", "--max-files", "5000")
	if tooSmall.State != desktop.Failed || err == nil || stderr != "readmit: "+tooSmall.Reason+"\n" {
		t.Fatalf("a quota the project cannot fit: the window answered %+v, the command line %v %q", tooSmall, err, stderr)
	}
	declared := app.SetProjectQuota(desktop.ProjectQuotaChange{Project: windowRoot, MaxBytes: 500_000_000, MaxFiles: 5000})
	if _, stderr, err := run(t, "project", "quota", commandRoot, "--max-bytes", "500000000", "--max-files", "5000"); declared.State != desktop.Completed || err != nil {
		t.Fatalf("declaring a quota: the window answered %+v, the command line %v %s", declared, err, stderr)
	}
	if !reflect.DeepEqual(treeDigests(t, windowRoot), treeDigests(t, commandRoot)) {
		t.Fatal("the window declared another quota than the command line")
	}

	archives := t.TempDir()
	preview := app.PreviewProjectRetirement(windowRoot)
	if preview.State != desktop.Completed || preview.Preview == nil {
		t.Fatalf("the window previewed the retirement as %+v", preview)
	}
	windowArchive, commandArchive := filepath.Join(archives, "window-archive"), filepath.Join(archives, "command-archive")
	archived := app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{Project: windowRoot, Destination: windowArchive, Selection: preview.Preview.Selection})
	if _, stderr, err := run(t, "project", "archive", commandRoot, "--output", commandArchive); archived.State != desktop.Completed || err != nil {
		t.Fatalf("archiving: the window answered %+v, the command line %v %s", archived, err, stderr)
	}
	if !reflect.DeepEqual(treeDigests(t, windowArchive), treeDigests(t, commandArchive)) {
		t.Fatal("the window's archive holds another backup than the command line's")
	}
	existing := app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{Project: windowRoot, Destination: windowArchive, Selection: preview.Preview.Selection})
	_, stderr, err = run(t, "project", "archive", commandRoot, "--output", commandArchive)
	if existing.State != desktop.Failed || err == nil || stderr != "readmit: "+existing.Reason+"\n" {
		t.Fatalf("an archive over an existing folder: the window answered %+v, the command line %v %q", existing, err, stderr)
	}

	for _, root := range []string{windowRoot, commandRoot} {
		if err := os.WriteFile(filepath.Join(root, "handover-notes.txt"), []byte("written after the preview\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stale := app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{
		Project: windowRoot, Destination: filepath.Join(archives, "stale"), Selection: preview.Preview.Selection, Delete: true, Confirm: true,
	})
	if stale.State != desktop.Failed || stale.Reason != "the project changed since the retirement preview; nothing was deleted" {
		t.Fatalf("a stale delete: %+v", stale)
	}
	if _, err := os.Lstat(filepath.Join(archives, "stale")); !os.IsNotExist(err) || !reflect.DeepEqual(treeDigests(t, windowRoot), treeDigests(t, commandRoot)) {
		t.Fatal("a stale delete wrote an archive or changed the project")
	}

	fresh := app.PreviewProjectRetirement(windowRoot)
	windowDeleted, commandDeleted := filepath.Join(archives, "window-deleted"), filepath.Join(archives, "command-deleted")
	deleted := app.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{
		Project: windowRoot, Destination: windowDeleted, Selection: fresh.Preview.Selection, Delete: true, Confirm: true,
	})
	stdout, stderr, err = run(t, "project", "delete", commandRoot, "--output", commandDeleted, "--confirm-delete")
	if deleted.State != desktop.Completed || err != nil || !strings.HasSuffix(stdout, deleted.Reason+"\n") {
		t.Fatalf("deleting: the window answered %+v, the command line %v %s %s", deleted, err, stdout, stderr)
	}
	for _, root := range []string{windowRoot, commandRoot} {
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatalf("a delete left the project in place: %v", err)
		}
	}
	if !reflect.DeepEqual(treeDigests(t, windowDeleted), treeDigests(t, commandDeleted)) {
		t.Fatal("the window's delete kept another recovery archive than the command line's")
	}
}
