package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
)

// diagnosisParityWorkspace captures the acknowledged booking fixture as one
// entry of a workspace both entry points read.
func diagnosisParityWorkspace(t *testing.T) (workspace, casePath string) {
	t.Helper()
	workspace = t.TempDir()
	casePath = filepath.Join(workspace, "acked-case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/diagnose-acknowledged.mllp", "--output", casePath); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	return workspace, casePath
}

// The desktop shell and the command line are two entry points into one
// diagnosis engine, and one review of it. The window runs a diagnosis over a
// workspace entry and records the analyst's decisions; `readmit diagnose` and
// `readmit diagnose review` do the same work over the same evidence. Neither
// reimplements the other, so every retained document — report, decisions and
// review — must be byte-identical across the two, down to the identities each
// one binds.
func TestDesktopDiagnosisAndReviewWriteTheCommandLineBytes(t *testing.T) {
	workspace, casePath := diagnosisParityWorkspace(t)
	cliDiagnosis := filepath.Join(t.TempDir(), "reports")
	if _, stderr, err := run(t, "diagnose", casePath, "--output", cliDiagnosis); err != nil || stderr != "" {
		t.Fatalf("diagnose: %v %s", err, stderr)
	}

	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "acked-case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the case was not verified: %+v", opened)
	}
	ran := app.RunDiagnosis(desktop.DiagnosisRequest{
		Workspace: workspace, Case: "acked-case", Identity: opened.Case.Identity,
		Builtin: "siu", Output: "desktop-diagnosis",
	})
	if ran.State != desktop.Completed || ran.Diagnosis == nil {
		t.Fatalf("the window did not run the diagnosis: %+v", ran)
	}
	for _, name := range []string{"report.json", "report.md"} {
		cli := mustRead(t, filepath.Join(cliDiagnosis, name))
		shell := mustRead(t, filepath.Join(workspace, "desktop-diagnosis", name))
		if !bytes.Equal(cli, shell) {
			t.Fatalf("%s differs between the desktop shell and the command line", name)
		}
	}
	// The identity the window displayed is the identity of the bytes both wrote,
	// which is the identity a review must name.
	sum := sha256.Sum256(mustRead(t, filepath.Join(cliDiagnosis, "report.json")))
	if ran.Diagnosis.ReportSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("the window reported an identity the report does not have: %s", ran.Diagnosis.ReportSHA256)
	}

	// The decisions the window persists are the document the command line reads,
	// and the review each writes over them is the same record.
	decided := app.DecideFindings(desktop.FindingReviewRequest{
		Workspace: workspace, Case: "acked-case", Identity: opened.Case.Identity,
		Report: "desktop-diagnosis", ReportSHA256: ran.Diagnosis.ReportSHA256,
		Decisions: []findingreview.Decision{
			{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "the scheduler must keep rejecting this booking"},
			{Finding: "f000002", Verdict: findingreview.Suppressed, Scope: findingreview.ScopeCase, Rationale: "the error detail is the vendor's wording"},
		},
		Output: "desktop-review", DecisionsOutput: "desktop-decisions.json",
	})
	if decided.State != desktop.Completed || decided.Review == nil {
		t.Fatalf("the window did not record the decisions: %+v", decided)
	}
	cliReview := filepath.Join(t.TempDir(), "review")
	if _, stderr, err := run(t, "diagnose", "review", filepath.Join(workspace, "desktop-diagnosis"),
		"--case", casePath, "--decisions", filepath.Join(workspace, "desktop-decisions.json"),
		"--output", cliReview); err != nil || stderr != "" {
		t.Fatalf("diagnose review: %v %s", err, stderr)
	}
	for _, name := range []string{"review.json", "review.md"} {
		cli := mustRead(t, filepath.Join(cliReview, name))
		shell := mustRead(t, filepath.Join(workspace, "desktop-review", name))
		if !bytes.Equal(cli, shell) {
			t.Fatalf("%s differs between the desktop shell and the command line", name)
		}
	}

	// A destination that exists is refused by both entry points, never
	// overwritten, with the same fixed sentence.
	stdout, stderr, err := run(t, "diagnose", casePath, "--output", cliDiagnosis)
	if err == nil || stdout != "" || !strings.Contains(stderr, "destination must be new") {
		t.Fatalf("the command line overwrote a retained diagnosis: %v %s %s", err, stdout, stderr)
	}
	again := app.RunDiagnosis(desktop.DiagnosisRequest{
		Workspace: workspace, Case: "acked-case", Identity: opened.Case.Identity,
		Builtin: "siu", Output: "desktop-diagnosis",
	})
	if again.State != desktop.Failed || !strings.Contains(again.Reason, "destination must be new") {
		t.Fatalf("the window overwrote a retained diagnosis: %+v", again)
	}
}

// Grouping several cases is the same evaluation in both entry points: the
// window's grouped report encodes to exactly the report.json the command line
// retains over the same cases and the same configuration.
func TestDesktopGroupingEncodesTheCommandLineGroupsReport(t *testing.T) {
	workspace, casePath := diagnosisParityWorkspace(t)
	second := filepath.Join(workspace, "resched-case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/diagnose-reschedule.hl7", "--output", second); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	cliGroups := filepath.Join(t.TempDir(), "groups")
	if _, stderr, err := run(t, "diagnose", "groups", casePath, second, "--output", cliGroups); err != nil || stderr != "" {
		t.Fatalf("diagnose groups: %v %s", err, stderr)
	}

	app := desktopApp(t, workspace)
	grouped := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{
		Workspace: workspace, Cases: []string{"acked-case", "resched-case"}, Builtin: "siu",
	})
	if grouped.State != desktop.Completed || grouped.Groups == nil {
		t.Fatalf("the window did not group the diagnoses: %+v", grouped)
	}
	if grouped.Total != len(grouped.Groups.Groups) || grouped.Total == 0 {
		t.Fatalf("the window windowed away part of a small grouping: %+v", grouped)
	}
	encoded, err := diagnose.GroupsJSON(*grouped.Groups)
	if err != nil {
		t.Fatal(err)
	}
	if cli := mustRead(t, filepath.Join(cliGroups, "report.json")); !bytes.Equal(encoded, cli) {
		t.Fatalf("the window grouped %s and the command line %s", encoded, cli)
	}
}

// A configuration neither entry point can read is refused by both for the same
// reason, and no report directory appears anywhere.
func TestDesktopDiagnosisRefusesTheConfigurationTheCommandLineRefuses(t *testing.T) {
	workspace, casePath := diagnosisParityWorkspace(t)
	writeDocument(t, workspace, "refused-config.json", `{"schema":"readmit-diagnose-config/v1","secret":"x"}`)
	stdout, stderr, err := run(t, "diagnose", casePath,
		"--config", filepath.Join(workspace, "refused-config.json"),
		"--output", filepath.Join(t.TempDir(), "new"))
	if err == nil || stdout != "" || stderr == "" {
		t.Fatalf("the command line diagnosed under an unreadable configuration: %s", stdout)
	}

	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "acked-case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the case was not verified: %+v", opened)
	}
	refused := app.RunDiagnosis(desktop.DiagnosisRequest{
		Workspace: workspace, Case: "acked-case", Identity: opened.Case.Identity,
		Config: "refused-config.json", Output: "never-written",
	})
	if refused.State != desktop.Failed || refused.Diagnosis != nil {
		t.Fatalf("the window diagnosed under an unreadable configuration: %+v", refused)
	}
	if refused.Reason == "" || !strings.Contains(stderr, refused.Reason) {
		t.Fatalf("the two entry points refuse differently: window %q, command line %q", refused.Reason, stderr)
	}
	if _, err := os.Lstat(filepath.Join(workspace, "never-written")); !os.IsNotExist(err) {
		t.Fatal("a refused diagnosis left a report directory behind")
	}
}
