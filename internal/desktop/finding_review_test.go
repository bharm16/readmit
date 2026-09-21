package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/findingreview"
)

// reviewableWorkspace is the diagnosed fixture: the acknowledged-booking case
// beside the diagnosis the window just ran over it, with the identity of both.
func reviewableWorkspace(t *testing.T) (*desktop.App, string, string, string) {
	t.Helper()
	app, root, identity := diagnosisWorkspace(t)
	ran := diagnosed(t, app, desktop.DiagnosisRequest{
		Workspace: root, Case: "acked", Identity: identity, Builtin: "siu", Output: "diagnosis-out",
	})
	if ran.Diagnosis.Total < 2 {
		t.Fatalf("the fixture no longer produces the findings this review decides: %+v", ran.Diagnosis)
	}
	return app, root, identity, ran.Diagnosis.ReportSHA256
}

func findingReviewRequest(root, identity, reportSHA256 string) desktop.FindingReviewRequest {
	return desktop.FindingReviewRequest{
		Workspace: root, Case: "acked", Identity: identity,
		Report: "diagnosis-out", ReportSHA256: reportSHA256,
		Decisions: []findingreview.Decision{
			{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "the scheduler must keep rejecting this booking"},
			{Finding: "f000002", Verdict: findingreview.Suppressed, Scope: findingreview.ScopeCase, Rationale: "the error detail is the vendor's wording"},
		},
	}
}

// Reviewing joins the diagnosis and the typed decisions and reports every
// verdict, basis and promotion — writing nothing. Only a confirmation
// promotes, and a suppression recorded at case scope covers what it covers by
// scope rather than by a second decision.
func TestReviewingFindingsJoinsJudgmentToTheDiagnosisAndWritesNothing(t *testing.T) {
	app, root, identity, reportSHA256 := reviewableWorkspace(t)
	before := bytesUnder(t, root)
	result := app.ReviewFindings(findingReviewRequest(root, identity, reportSHA256))
	if result.State != desktop.Completed || result.Review == nil {
		t.Fatalf("the review did not complete: %+v", result)
	}
	record := result.Review.Record
	if record.Schema != findingreview.Schema || record.Diagnosis.Report != reportSHA256 || record.Statement == "" {
		t.Fatalf("the record does not name what it was made over: %+v", record)
	}
	byFinding := map[string]findingreview.Status{}
	for _, status := range record.Findings {
		byFinding[status.Finding] = status
	}
	confirmed := byFinding["f000001"]
	if confirmed.Verdict != findingreview.Confirmed || confirmed.Basis != findingreview.BasisDecision ||
		confirmed.Promotion == nil || len(confirmed.Promotion.Expectations) == 0 {
		t.Fatalf("a confirmed acknowledgement finding promoted nothing: %+v", confirmed)
	}
	suppressed := byFinding["f000002"]
	if suppressed.Verdict != findingreview.Suppressed || suppressed.Scope != findingreview.ScopeCase || suppressed.Promotion != nil {
		t.Fatalf("a suppressed finding was not reported as suppressed: %+v", suppressed)
	}
	// Reviewing decided nothing on disk: not the case, not the report, and no
	// new entry anywhere in the workspace.
	if after := bytesUnder(t, root); len(after) != len(before) {
		t.Fatalf("a read-only review changed the workspace: %d entries became %d", len(before), len(after))
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"SECRET-FREE-TEXT", "SECRET-ERR-TEXT", root} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("the review disclosed %q", private)
		}
	}
}

// A judgment binds to exactly what was displayed: a report that changed since,
// a decision about a finding nobody produced, and text a person could not have
// typed are each refused with the reason, and nothing is written.
func TestReviewingFindingsRefusesWhatWouldMisattributeAJudgment(t *testing.T) {
	app, root, identity, reportSHA256 := reviewableWorkspace(t)
	for name, change := range map[string]func(*desktop.FindingReviewRequest){
		"a report that changed since it was displayed": func(r *desktop.FindingReviewRequest) {
			r.ReportSHA256 = strings.Repeat("0", 64)
		},
		"evidence that changed since it was displayed": func(r *desktop.FindingReviewRequest) {
			r.Identity = strings.Repeat("0", 64)
		},
		"a decision about a finding nobody produced": func(r *desktop.FindingReviewRequest) {
			r.Decisions[0].Finding = "f000099"
		},
		"a verdict this release does not record": func(r *desktop.FindingReviewRequest) {
			r.Decisions[0].Verdict = "accepted"
		},
		"a rationale carrying a control character": func(r *desktop.FindingReviewRequest) {
			r.Decisions[0].Rationale = "a\x00b"
		},
		"a suppression scope on a confirmation": func(r *desktop.FindingReviewRequest) {
			r.Decisions[0].Scope = findingreview.ScopeCase
		},
		"no rationale at all": func(r *desktop.FindingReviewRequest) {
			r.Decisions[0].Rationale = ""
		},
	} {
		request := findingReviewRequest(root, identity, reportSHA256)
		change(&request)
		if got := app.ReviewFindings(request); got.State != desktop.Failed || got.Review != nil {
			t.Fatalf("%s was reviewed anyway: %+v", name, got)
		}
	}
	// The stale-report refusal names the remedy.
	stale := findingReviewRequest(root, identity, strings.Repeat("0", 64))
	if got := app.ReviewFindings(stale); !strings.Contains(got.Reason, "reopen the report before reviewing") {
		t.Fatalf("a stale report was not refused by name: %+v", got)
	}
}

// Deciding persists both documents exactly as `readmit diagnose review` writes
// them: the decisions as one new flat entry the command line can consume, and
// the review directory holding review.json and review.md, never overwriting.
func TestDecidingFindingsPersistsTheDecisionsAndTheReviewWithoutOverwriting(t *testing.T) {
	app, root, identity, reportSHA256 := reviewableWorkspace(t)
	request := findingReviewRequest(root, identity, reportSHA256)
	request.Output = "review-out"
	request.DecisionsOutput = "verdicts.json"
	result := app.DecideFindings(request)
	if result.State != desktop.Completed || result.Review == nil ||
		result.Output != "review-out" || result.DecisionsOutput != "verdicts.json" {
		t.Fatalf("the decision was not persisted: %+v", result)
	}
	// The decisions entry is the same contract the command line reads, bound to
	// the exact report it was decided against.
	decisionsData, err := os.ReadFile(filepath.Join(root, "verdicts.json"))
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := findingreview.ParseDecisions(decisionsData)
	if err != nil || decisions.Report != reportSHA256 || len(decisions.Decisions) != 2 {
		t.Fatalf("the persisted decisions are not the document that was decided: %+v %v", decisions, err)
	}
	// The review record names the decisions by the digest of those exact bytes.
	reviewData, err := os.ReadFile(filepath.Join(root, "review-out", "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record findingreview.Record
	if err := json.Unmarshal(reviewData, &record, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if record.Decisions != sha256Of(decisionsData) || record.Diagnosis.Report != reportSHA256 {
		t.Fatalf("the retained review is not bound to the retained decisions: %+v", record)
	}
	if _, err := os.Lstat(filepath.Join(root, "review-out", "review.md")); err != nil {
		t.Fatal("the review directory holds no markdown rendering")
	}
	// Deciding again refuses both existing destinations and retains them.
	if got := app.DecideFindings(request); got.State != desktop.Failed {
		t.Fatalf("an existing decisions entry was overwritten: %+v", got)
	}
	retained, err := os.ReadFile(filepath.Join(root, "verdicts.json"))
	if err != nil || string(retained) != string(decisionsData) {
		t.Fatal("a refused decision changed the retained decisions")
	}
	fresh := request
	fresh.DecisionsOutput = "verdicts-2.json"
	if got := app.DecideFindings(fresh); got.State != desktop.Failed || !strings.Contains(got.Reason, "destination must be new") {
		t.Fatalf("an existing review directory was not refused by name: %+v", got)
	}
	// A decision needs both destinations named as entries of the workspace.
	unnamed := request
	unnamed.DecisionsOutput = ""
	if got := app.DecideFindings(unnamed); got.State != desktop.Failed ||
		!strings.Contains(got.Reason, "one new entry of the open workspace") {
		t.Fatalf("a decision with no decisions destination was accepted: %+v", got)
	}
}

// Deciding is authoring, so it is admitted the way `readmit diagnose review`
// is: a shell with no operation policy selected reviews freely and persists
// nothing.
func TestDecidingFindingsRequiresAdmissionAndReviewingDoesNot(t *testing.T) {
	licensed, root, identity, reportSHA256 := reviewableWorkspace(t)
	_ = licensed
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	request := findingReviewRequest(root, identity, reportSHA256)
	if got := app.ReviewFindings(request); got.State != desktop.Completed {
		t.Fatalf("an unadmitted shell could not review what is already there: %+v", got)
	}
	request.Output = "review-out"
	request.DecisionsOutput = "verdicts.json"
	if got := app.DecideFindings(request); got.State != desktop.PermissionDenied {
		t.Fatalf("an unadmitted decision was persisted: %+v", got)
	}
	for _, name := range []string{"review-out", "verdicts.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("an unadmitted decision wrote %s", name)
		}
	}
}
