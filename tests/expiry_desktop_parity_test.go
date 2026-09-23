package tests

// An expired term stops new paid work and nothing else, and it stops the same
// work in both entry points. The window and the command line are held to one
// expired, activated term at once: authoring new evidence, checking a target
// and changing a quota are refused by both, while reading, diagnosing, exporting and importing a
// reviewed profile package, verifying, backing up, restoring, recovering,
// archiving and taking an upgrade's rollback point stay available in both,
// because each of those only reads, copies or preserves what already exists. A disabled
// control is not the boundary, so every window operation is called directly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestAnExpiredTermGatesTheSameOperationsInTheWindowAsOnTheCommandLine(t *testing.T) {
	root := indexedProject(t)
	// A quota change under the active term leaves the recovery copy that
	// recovery restores later.
	if _, stderr, err := run(t, "project", "quota", root, "--max-bytes", "10000000", "--max-files", "1000"); err != nil {
		t.Fatalf("quota: %v %s", err, stderr)
	}
	copies, err := filepath.Glob(filepath.Join(root, "project.json.recovery-*"))
	if err != nil || len(copies) == 0 {
		t.Fatal("no recovery copy was retained")
	}
	digest := strings.TrimPrefix(filepath.Base(copies[0]), "project.json.recovery-")
	candidate := stagedCandidate(t, false, "9.9.9")

	expired := issuedTrial(t, time.Now().UTC().Truncate(time.Second).Add(-31*24*time.Hour))
	cli := func(args ...string) (string, error) {
		t.Helper()
		return rawOperation(t, append([]string{"--operation-policy", expired}, args...)...)
	}
	window := unlicensedDesktopApp(t, t.TempDir())
	if result := window.SelectOperationPolicy(expired); result.State != desktop.Completed {
		t.Fatalf("select the expired term: %+v", result)
	}
	if status := window.OperationStatus(); status.Term != "expired" {
		t.Fatalf("the window does not report the term as expired: %+v", status)
	}

	// New work is refused by both, and neither writes what it was asked for.
	cliIndex := filepath.Join(root, "cli.index.json")
	if out, err := cli("index", "build", filepath.Join(root, "regression"), "--output", cliIndex,
		"--field", "PID-3", "--retain", "values", "--retain-until", "indefinite"); err == nil || !strings.Contains(out, "expired") {
		t.Fatalf("the command line built an index under an expired term: %v %s", err, out)
	}
	built := window.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "regression", Output: "window.index.json",
		Fields: []string{"PID-3"}, Retention: "values", RetainUntil: "indefinite"})
	if built.State != desktop.PermissionDenied || !strings.Contains(built.Reason, "expired") {
		t.Fatalf("the window built an index under an expired term: %+v", built)
	}
	for _, refused := range []string{cliIndex, filepath.Join(root, "window.index.json")} {
		if _, err := os.Lstat(refused); !os.IsNotExist(err) {
			t.Fatalf("a refused build wrote %s", filepath.Base(refused))
		}
	}
	// A connectivity check reaches a destination, so it is execution, refused
	// by both before anything is reached.
	lab := namedTarget(t, t.TempDir(), "lab-mllp", "nonproduction", "127.0.0.1:9")
	if out, err := cli("target", "check", "--target", lab); err == nil || !strings.Contains(out, "expired") {
		t.Fatalf("the command line checked a target under an expired term: %v %s", err, out)
	}
	if result := window.CheckTarget(desktop.TargetCheckRequest{Workspace: filepath.Dir(lab), TargetFile: filepath.Base(lab)}); result.State != desktop.PermissionDenied || !strings.Contains(result.Reason, "expired") {
		t.Fatalf("the window checked a target under an expired term: %+v", result)
	}
	// Setting a quota is a change, so it is refused by both too.
	if out, err := cli("project", "quota", root, "--max-bytes", "20000000"); err == nil || !strings.Contains(out, "expired") {
		t.Fatalf("the command line changed a quota under an expired term: %v %s", err, out)
	}
	if result := window.SetProjectQuota(desktop.ProjectQuotaChange{Project: root, MaxBytes: 20000000}); result.State != desktop.PermissionDenied {
		t.Fatalf("the window changed a quota under an expired term: %+v", result)
	}

	// Retained evidence stays readable in both.
	if out, err := cli("project", "show", root); err != nil || !strings.Contains(out, frozenRegressionIdentity) {
		t.Fatalf("the command line gated a read: %v %s", err, out)
	}
	if result := window.OpenProjectOverview(root); result.State != desktop.Completed {
		t.Fatalf("the window gated the project overview: %+v", result)
	}
	if result := window.OpenCase(root, "regression"); result.State != desktop.Completed || result.Case.Identity != frozenRegressionIdentity {
		t.Fatalf("the window gated case verification: %+v", result)
	}
	if result := window.DescribeIndex(root, "regression", backupIndexName); result.State != desktop.Completed {
		t.Fatalf("the window gated the retained index: %+v", result)
	}

	// Diagnosing retained evidence and exporting or importing a reviewed
	// profile package read or copy what exists, so both entry points perform
	// them under the expired term.
	evidence, _ := diagnosisParityWorkspace(t)
	if out, err := cli("diagnose", filepath.Join(evidence, "acked-case"), "--output", filepath.Join(t.TempDir(), "reports")); err != nil {
		t.Fatalf("the command line refused a diagnosis under an expired term: %v %s", err, out)
	}
	opened := window.OpenCase(evidence, "acked-case")
	if opened.State != desktop.Completed {
		t.Fatalf("open the diagnosed case: %+v", opened)
	}
	if result := window.RunDiagnosis(desktop.DiagnosisRequest{Workspace: evidence, Case: "acked-case", Identity: opened.Case.Identity, Builtin: "siu", Output: "window-diagnosis"}); result.State != desktop.Completed {
		t.Fatalf("the window refused a diagnosis under an expired term: %+v", result)
	}
	profiles := t.TempDir()
	for entry, fixture := range map[string]string{"profile.json": "local-profile.json", "pack.json": "profile-pack.json", "version.json": "profile-version.json", "origin.json": "profile-origin.json"} {
		writeDocument(t, profiles, entry, string(mustRead(t, filepath.Join("..", "testdata", "fixtures", fixture))))
	}
	cliPackage := filepath.Join(t.TempDir(), "package.json")
	if out, err := cli("profile", "export", filepath.Join(profiles, "profile.json"), "--pack", filepath.Join(profiles, "pack.json"), "--version", filepath.Join(profiles, "version.json"),
		"--origin", filepath.Join(profiles, "origin.json"), "--output", cliPackage, "--reviewed"); err != nil {
		t.Fatalf("the command line refused a profile export under an expired term: %v %s", err, out)
	}
	if out, err := cli("profile", "import", cliPackage, "--output", filepath.Join(t.TempDir(), "imported")); err != nil {
		t.Fatalf("the command line refused a profile import under an expired term: %v %s", err, out)
	}
	if result := window.ExportProfilePackage(desktop.ProfilePackageExportRequest{Workspace: profiles, Profile: "profile.json", Pack: "pack.json", Version: "version.json",
		Origin: "origin.json", Output: "package.json", Reviewed: true}); result.State != desktop.Completed {
		t.Fatalf("the window refused a profile export under an expired term: %+v", result)
	}
	if result := window.ImportProfilePackage(desktop.ProfilePackageImportRequest{Workspace: profiles, Package: "package.json", Output: "imported"}); result.State != desktop.Completed {
		t.Fatalf("the window refused a profile import under an expired term: %+v", result)
	}

	// Preserving what exists is ungated in both: each entry point backs the
	// project up, verifies and restores that backup, recovers a document in the
	// restored copy, archives the source, takes an upgrade's rollback point and
	// deletes a second restored copy behind its own recovery archive.
	scratch := t.TempDir()
	at := func(name string) string { return filepath.Join(scratch, name) }
	for _, args := range [][]string{
		{"backup", "create", root, "--output", at("cli-backup")},
		{"backup", "verify", at("cli-backup")},
		{"backup", "restore", at("cli-backup"), "--output", at("cli-restored")},
		{"project", "recover", at("cli-restored"), "--document", "project.json", "--digest", digest},
		{"project", "archive", root, "--output", at("cli-archive")},
		{"upgrade", "prepare", root, "--candidate", candidate, "--output", at("cli-rollback"), "--approve"},
		{"backup", "restore", at("cli-backup"), "--output", at("cli-doomed")},
		{"project", "delete", at("cli-doomed"), "--output", at("cli-retired"), "--confirm-delete"},
	} {
		if out, err := cli(args...); err != nil {
			t.Fatalf("the command line refused %s %s under an expired term: %v %s", args[0], args[1], err, out)
		}
	}
	// The steps run in order: restore and recovery read what the backup wrote.
	steps := []struct {
		name string
		call func() desktop.State
	}{
		{"backup", func() desktop.State {
			return window.CreateProjectBackup(desktop.BackupCreateRequest{Project: root, Destination: at("window-backup")}).State
		}},
		{"verify", func() desktop.State { return window.VerifyProjectBackup(at("window-backup")).State }},
		{"restore", func() desktop.State {
			return window.RestoreProjectBackup(desktop.BackupRestoreRequest{Backup: at("window-backup"), Destination: at("window-restored")}).State
		}},
		{"recover", func() desktop.State {
			return window.RecoverProjectDocument(desktop.ProjectRecoverRequest{Project: at("window-restored"), Document: "project.json", Digest: digest}).State
		}},
		{"archive", func() desktop.State {
			preview := window.PreviewProjectRetirement(root)
			if preview.State != desktop.Completed || preview.Preview == nil {
				return preview.State
			}
			return window.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{Project: root, Destination: at("window-archive"), Selection: preview.Preview.Selection}).State
		}},
		{"rollback point", func() desktop.State {
			return window.PrepareStagedUpgrade(desktop.UpgradePrepareRequest{Project: root, Candidate: candidate, Destination: at("window-rollback"), Approve: true}).State
		}},
		{"delete", func() desktop.State {
			if restored := window.RestoreProjectBackup(desktop.BackupRestoreRequest{Backup: at("window-backup"), Destination: at("window-doomed")}); restored.State != desktop.Completed {
				return restored.State
			}
			preview := window.PreviewProjectRetirement(at("window-doomed"))
			if preview.State != desktop.Completed || preview.Preview == nil {
				return preview.State
			}
			return window.ArchiveOrDeleteProject(desktop.ProjectArchiveRequest{Project: at("window-doomed"), Destination: at("window-retired"),
				Selection: preview.Preview.Selection, Delete: true, Confirm: true}).State
		}},
	}
	for _, step := range steps {
		if state := step.call(); state != desktop.Completed {
			t.Errorf("the window refused the %s the command line performs under the same expired term: %s", step.name, state)
		}
	}
}
