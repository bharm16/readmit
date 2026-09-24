package tests

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
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

// The desktop shell and the command line write and reopen report directories
// through one diagnose module and record reviews through one findingreview
// module, so how each directory is laid out, bounded and named is tested
// there. What remains between the two entry points is the files one leaves for
// the other: the report directory and the decisions document the window
// retains are what `readmit diagnose review` reads, and the review it records
// over them is the window's own, byte for byte.
func TestDesktopDiagnosisAndReviewWriteTheCommandLineBytes(t *testing.T) {
	workspace, casePath := diagnosisParityWorkspace(t)
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
}
