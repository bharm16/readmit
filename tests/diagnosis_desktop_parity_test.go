package tests

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/findingreview"
)

// diagnosisParityWorkspace initializes a project and captures the
// acknowledged booking fixture as one entry of it, both through the command
// line, so the window works over what the command line wrote.
func diagnosisParityWorkspace(t *testing.T) (workspace, casePath string) {
	t.Helper()
	workspace = newProject(t)
	casePath = filepath.Join(workspace, "acked-case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/diagnose-acknowledged.mllp", "--output", casePath); err != nil || stderr != "" {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	return workspace, casePath
}

// catalogCaseRef is the catalog object the project discovers at one entry.
func catalogCaseRef(t *testing.T, app *desktop.App, project desktop.RequestContext, entry string) desktop.ItemRef {
	t.Helper()
	listed := app.ListCatalog(desktop.CatalogQuery{Context: project, Kind: desktop.CaseItem})
	if listed.Page == nil {
		t.Fatalf("cases: %+v", listed)
	}
	for _, item := range listed.Page.Items {
		if item.Summary.Case != nil && item.Summary.Case.Entry == entry {
			return item.Ref
		}
	}
	t.Fatalf("no case at %s: %+v", entry, listed.Page.Items)
	return desktop.ItemRef{}
}

// singleEntry is the one new entry of root with the prefix, such as the
// analysis or the finding review the window just saved.
func singleEntry(t *testing.T, root, prefix string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	found := []string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			found = append(found, entry.Name())
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d entries with prefix %q: %v", len(found), prefix, found)
	}
	return found[0]
}

// statusesBytes is the findings array both entry points' reviews carry,
// encoded the same way so the window's and the command line's compare.
func statusesBytes(t *testing.T, statuses []findingreview.Status) []byte {
	t.Helper()
	raw, err := json.Marshal(statuses, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The desktop shell and the command line write and reopen report directories
// through one diagnose module and record reviews through one findingreview
// module, so how each directory is laid out, bounded and named is tested
// there. What remains between the two entry points is the files one leaves for
// the other: the analysis the window's AnalyzeCase writes is the report
// directory `readmit diagnose` writes, the review it saves is the decisions
// document `readmit diagnose review` reads, and the record that review makes
// over them is the window's own, finding for finding.
func TestDesktopDiagnosisAndReviewWriteTheCommandLineBytes(t *testing.T) {
	workspace, casePath := diagnosisParityWorkspace(t)
	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "acked-case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("the case was not verified: %+v", opened)
	}
	project := desktop.RequestContext{Project: workspace}
	ran := app.AnalyzeCase(desktop.AnalyzeRequest{
		Context: project, Case: catalogCaseRef(t, app, project, "acked-case"), Identity: opened.Case.Identity,
		Profile: desktop.AnalysisProfileRef{Builtin: "siu"}, IntentID: "press-1",
	})
	if ran.State != desktop.Completed || ran.Analysis == nil {
		t.Fatalf("the window did not run the diagnosis: %+v", ran)
	}
	analysisDir := filepath.Join(workspace, singleEntry(t, workspace, "analysis-"))
	cliDiagnosis := filepath.Join(t.TempDir(), "diagnosis")
	if _, stderr, err := run(t, "diagnose", casePath, "--output", cliDiagnosis); err != nil || stderr != "" {
		t.Fatalf("diagnose: %v %s", err, stderr)
	}
	for _, name := range []string{"report.json", "report.md"} {
		cli := mustRead(t, filepath.Join(cliDiagnosis, name))
		shell := mustRead(t, filepath.Join(analysisDir, name))
		if !bytes.Equal(cli, shell) {
			t.Fatalf("%s differs between the desktop shell and the command line", name)
		}
	}
	decisions := []findingreview.Decision{
		{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "the scheduler must keep rejecting this booking"},
		{Finding: "f000002", Verdict: findingreview.Suppressed, Scope: findingreview.ScopeCase, Rationale: "the error detail is the vendor's wording"},
	}
	draft := desktop.FindingReviewDraft{Analysis: ran.Analysis.Ref, ReportSHA256: ran.Analysis.ReportSHA256, Decisions: decisions}
	before := treeOf(t, workspace)
	previewed := app.PreviewFindingReview(desktop.DraftRequest{Context: project, Kind: desktop.FindingReviewItem, Draft: desktop.ItemDraft{FindingReview: &draft}})
	if previewed.State != desktop.Completed || len(previewed.Problems) != 0 || len(previewed.Statuses) == 0 {
		t.Fatalf("the window did not preview the review: %+v", previewed)
	}
	if after := treeOf(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatal("a preview wrote into the workspace")
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: project, Kind: desktop.FindingReviewItem, IntentID: "review-1",
		Draft: desktop.ItemDraft{FindingReview: &draft}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("the window did not record the decisions: %+v", saved)
	}
	decisionsPath := filepath.Join(workspace, singleEntry(t, workspace, "finding-review-"))
	cliReview := filepath.Join(t.TempDir(), "review")
	if _, stderr, err := run(t, "diagnose", "review", analysisDir,
		"--case", casePath, "--decisions", decisionsPath, "--output", cliReview); err != nil || stderr != "" {
		t.Fatalf("diagnose review: %v %s", err, stderr)
	}
	var record findingreview.Record
	if err := json.Unmarshal(mustRead(t, filepath.Join(cliReview, "review.json")), &record); err != nil {
		t.Fatal(err)
	}
	if record.Decisions != sha256Hex(mustRead(t, decisionsPath)) {
		t.Fatal("the command line recorded other decisions than the window saved")
	}
	history := app.FindingReviewHistory(desktop.ItemRequest{Context: project, Ref: ran.Analysis.Ref})
	if history.State != desktop.Completed || history.Review == nil || history.Review.ID != saved.Saved.ID {
		t.Fatalf("the window does not read the review it saved: %+v", history)
	}
	if shell, cli := statusesBytes(t, history.Statuses), statusesBytes(t, record.Findings); !bytes.Equal(shell, cli) {
		t.Fatalf("the window read %s and the command line recorded %s", shell, cli)
	}
}
