package tests

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
)

// The diagnosis panel's grouping, reopening, review preview, decisions editor
// and configuration editor, held to the commands that do the same work: the
// window reaches `readmit diagnose groups`, `readmit diagnose` and `readmit
// diagnose review` through the same engine and the same readers, so what the
// window shows and refuses is what they report and refuse over the same files.

// panelWorkspace captures the acknowledged booking and two captures of one
// reschedule whose booking the capture never held, and copies in the case the
// native window retained in September (testdata/acceptance/native-109), all as
// entries of one workspace both entry points read. It returns the verified
// acknowledged case's identity.
func panelWorkspace(t *testing.T) (workspace, identity string) {
	t.Helper()
	workspace = t.TempDir()
	for _, capture := range []struct{ fixture, entry string }{
		{"diagnose-acknowledged.mllp", "acked"},
		{"diagnose-reschedule.hl7", "monday"},
		{"diagnose-reschedule.hl7", "tuesday"},
	} {
		if _, stderr, err := run(t, "capture", "../testdata/fixtures/"+capture.fixture, "--output", filepath.Join(workspace, capture.entry)); err != nil || stderr != "" {
			t.Fatalf("capture %s: %v %s", capture.entry, err, stderr)
		}
	}
	copyTree(t, "../testdata/acceptance/native-109/regression", filepath.Join(workspace, "retained"))
	opened, err := bundle.Open(filepath.Join(workspace, "acked"))
	if err != nil {
		t.Fatal(err)
	}
	return workspace, opened.Identity
}

// placeRetained places one retained document into the workspace byte for
// byte, as a person copies a colleague's file or a shipped example.
func placeRetained(t *testing.T, from, to string) {
	t.Helper()
	if err := os.WriteFile(to, mustRead(t, from), 0o600); err != nil {
		t.Fatal(err)
	}
}

// refusedByCommandLine holds one command to a refusal: it fails, prints nothing on
// standard output, and says exactly the sentence the window said.
func refusedByCommandLine(t *testing.T, sentence string, args ...string) {
	t.Helper()
	stdout, stderr, err := run(t, args...)
	if err == nil || stdout != "" || stderr != "readmit: "+sentence+"\n" {
		t.Fatalf("readmit %s: want the window's refusal %q, got %v %q %q", strings.Join(args, " "), sentence, err, stdout, stderr)
	}
}

// Grouping in the window is `readmit diagnose groups` over the same cases:
// the grouped report encodes to the report.json the command writes, the
// recurring reschedule is one group of two members, a window of the groups
// keeps the whole count, and both refuse a duplicated case and a configuration
// of another contract version in one sentence. Grouping writes nothing, and no
// case changes.
func TestTheWindowGroupsTheCasesReadmitDiagnoseGroupsGroups(t *testing.T) {
	workspace, _ := panelWorkspace(t)
	cases := []string{"acked", "monday", "tuesday", "retained"}
	paths := make([]string, len(cases))
	for i, name := range cases {
		paths[i] = filepath.Join(workspace, name)
	}
	cliGroups := filepath.Join(t.TempDir(), "groups")
	if _, stderr, err := run(t, append(append([]string{"diagnose", "groups"}, paths...), "--output", cliGroups)...); err != nil || stderr != "" {
		t.Fatalf("diagnose groups: %v %s", err, stderr)
	}
	before := treeOf(t, workspace)

	app := desktopApp(t, workspace)
	grouped := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: workspace, Cases: cases, Builtin: "siu"})
	if grouped.State != desktop.Completed || grouped.Groups == nil || grouped.Total != len(grouped.Groups.Groups) {
		t.Fatalf("the window did not group the cases: %+v", grouped)
	}
	encoded, err := diagnose.GroupsJSON(*grouped.Groups)
	if err != nil {
		t.Fatal(err)
	}
	if cli := mustRead(t, filepath.Join(cliGroups, "report.json")); !bytes.Equal(encoded, cli) {
		t.Fatalf("the window grouped %s and the command line %s", encoded, cli)
	}
	// The two reschedules are one shape: one group holds both, each as its
	// own member, and every case's complete diagnosis is kept.
	if len(grouped.Groups.Cases) != len(cases) {
		t.Fatalf("the grouping does not keep every case's diagnosis: %d", len(grouped.Groups.Cases))
	}
	recurring := 0
	for _, group := range grouped.Groups.Groups {
		if group.RuleID == diagnose.BookingNotObserved {
			recurring++
			if len(group.Members) != 2 || group.Members[0].CaseIdentity == group.Members[1].CaseIdentity {
				t.Fatalf("the recurring reschedule is not one group of both captures: %+v", group)
			}
		}
	}
	if recurring != 1 {
		t.Fatalf("the recurring reschedule is %d groups", recurring)
	}
	// A later window of the groups is the rest of the same grouping.
	rest := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: workspace, Cases: cases, Builtin: "siu", Offset: 1})
	if rest.State != desktop.Completed || rest.Total != grouped.Total || !reflect.DeepEqual(rest.Groups.Groups, grouped.Groups.Groups[1:]) {
		t.Fatalf("a later window is not the rest of the grouping: %+v", rest)
	}

	// A case named twice, and a configuration of another contract version,
	// are refused by both in one sentence.
	duplicated := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: workspace, Cases: []string{"acked", "acked"}, Builtin: "siu"})
	if duplicated.State != desktop.Failed || duplicated.Groups != nil {
		t.Fatalf("the window grouped one case twice: %+v", duplicated)
	}
	refusedByCommandLine(t, duplicated.Reason, "diagnose", "groups", paths[0], paths[0], "--output", filepath.Join(t.TempDir(), "duplicate"))
	later := writeDocument(t, t.TempDir(), "later-config.json", `{"schema":"readmit-diagnose-config/v2","profile":"readmit-siu-v1","ruleset":"readmit-siu-diagnosis/v1","rules":["ack.msa-outcome"],"namespaces":[]}`)
	placeRetained(t, later, filepath.Join(workspace, "later-config.json"))
	before["later-config.json"] = mustRead(t, later)
	unread := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: workspace, Cases: cases, Config: "later-config.json"})
	if unread.State != desktop.Failed || unread.Groups != nil || unread.Reason != "unsupported diagnosis configuration schema" {
		t.Fatalf("the window grouped under a configuration it cannot read: %+v", unread)
	}
	refusedByCommandLine(t, unread.Reason, append(append([]string{"diagnose", "groups"}, paths...), "--config", later, "--output", filepath.Join(t.TempDir(), "later"))...)

	if after := treeOf(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatal("grouping in the window changed the workspace")
	}
}

// Reopening a retained report in the window reads the report.json `readmit
// diagnose` wrote with the reader `readmit diagnose review` applies: the same
// identity, the same findings, and the case it was run over. A report of
// another case opens as what it is, and reviewing it over this case is
// refused by both. Both entry points reopen a report directory through
// diagnose.OpenReport, whose refusals are tested there.
func TestTheWindowReopensTheReportReadmitDiagnoseWrote(t *testing.T) {
	workspace, identity := panelWorkspace(t)
	for _, diagnosis := range []struct{ entry, output string }{{"acked", "acked-diagnosis"}, {"monday", "monday-diagnosis"}} {
		if _, stderr, err := run(t, "diagnose", filepath.Join(workspace, diagnosis.entry), "--output", filepath.Join(workspace, diagnosis.output)); err != nil || stderr != "" {
			t.Fatalf("diagnose %s: %v %s", diagnosis.entry, err, stderr)
		}
	}
	reportData := mustRead(t, filepath.Join(workspace, "acked-diagnosis", "report.json"))
	report, err := diagnose.ParseReport(reportData)
	if err != nil {
		t.Fatal(err)
	}
	app := desktopApp(t, workspace)
	opened := app.OpenCase(workspace, "acked")
	if opened.State != desktop.Completed || opened.Case == nil || opened.Case.Identity != identity {
		t.Fatalf("the case was not verified: %+v", opened)
	}
	before := treeOf(t, workspace)

	reopened := app.OpenDiagnosisReport(workspace, "acked-diagnosis", 0)
	if reopened.State != desktop.Completed || reopened.Diagnosis == nil {
		t.Fatalf("the window did not reopen the report: %+v", reopened)
	}
	shown := reopened.Diagnosis
	if shown.ReportSHA256 != sha256Hex(reportData) || shown.CaseIdentity != identity || shown.CaseIdentity != report.CaseIdentity {
		t.Fatalf("the window named another report or case: %s %s", shown.ReportSHA256, shown.CaseIdentity)
	}
	if shown.Total != len(report.Findings) || !reflect.DeepEqual(shown.Findings, report.Findings) ||
		!reflect.DeepEqual(shown.Rules, report.Rules) || shown.Scope != report.Scope || shown.Profile != report.Profile {
		t.Fatalf("the window reopened other findings than the command line wrote: %+v", shown)
	}

	// Decisions typed about it are reviewed by the window exactly as the
	// command line reviews them over the file.
	decisions := decisionsFor(t, sha256Hex(reportData))
	previewed := app.ReviewFindings(desktop.FindingReviewRequest{
		Workspace: workspace, Case: "acked", Identity: identity, Report: "acked-diagnosis",
		ReportSHA256: shown.ReportSHA256, Decisions: decisions.Decisions,
	})
	if previewed.State != desktop.Completed || previewed.Review == nil {
		t.Fatalf("the reopened report could not be reviewed: %+v", previewed)
	}

	// Another case's report opens as what it is — run over other evidence —
	// and reviewing it over this case is refused by both in one sentence.
	other := app.OpenDiagnosisReport(workspace, "monday-diagnosis", 0)
	if other.State != desktop.Completed || other.Diagnosis == nil || other.Diagnosis.CaseIdentity == identity {
		t.Fatalf("another case's report was not named as another case's: %+v", other)
	}
	otherDecisions := decisionsFor(t, other.Diagnosis.ReportSHA256)
	foreign := app.ReviewFindings(desktop.FindingReviewRequest{
		Workspace: workspace, Case: "acked", Identity: identity, Report: "monday-diagnosis",
		ReportSHA256: other.Diagnosis.ReportSHA256, Decisions: otherDecisions.Decisions[:1],
	})
	if foreign.State != desktop.Failed || foreign.Review != nil ||
		foreign.Reason != "this diagnosis was run over different evidence; open the case the report names" {
		t.Fatalf("the window reviewed another case's report over this case: %+v", foreign)
	}
	otherFile := writeDocument(t, t.TempDir(), "other-decisions.json", string(encodeDecisions(t, findingreview.Decisions{
		Schema: findingreview.DecisionsSchema, Report: other.Diagnosis.ReportSHA256, Decisions: otherDecisions.Decisions[:1],
	})))
	refusedByCommandLine(t, foreign.Reason, "diagnose", "review", filepath.Join(workspace, "monday-diagnosis"),
		"--case", filepath.Join(workspace, "acked"), "--decisions", otherFile, "--output", filepath.Join(t.TempDir(), "review"))

	if after := treeOf(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatal("reopening and previewing in the window changed the workspace")
	}
}

// decisionsFor is one person's decisions about the acknowledged booking's two
// findings — or, for a report with one finding, about that one — bound to the
// report they were typed against.
func decisionsFor(t *testing.T, report string) findingreview.Decisions {
	t.Helper()
	return findingreview.Decisions{Schema: findingreview.DecisionsSchema, Report: report, Decisions: []findingreview.Decision{
		{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "The scheduler must keep rejecting this booking."},
		{Finding: "f000002", Verdict: findingreview.Suppressed, Scope: findingreview.ScopeCase, Rationale: "The error detail is the vendor's wording."},
	}}
}

// encodeDecisions is a decisions document as a person's editor might hold it:
// indented, members in the contract's order.
func encodeDecisions(t *testing.T, decisions findingreview.Decisions) []byte {
	t.Helper()
	data, err := json.Marshal(decisions, jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A preview in the window is the record `readmit diagnose review` writes over
// the same decisions, and it writes nothing. A report that changed after it
// was displayed, and decisions recorded against another report, are refused by
// diagnose.OpenReport and findingreview.Review, which both entry points call.
func TestTheWindowPreviewsTheReviewReadmitDiagnoseReviewRecords(t *testing.T) {
	workspace, identity := panelWorkspace(t)
	if _, stderr, err := run(t, "diagnose", filepath.Join(workspace, "acked"), "--output", filepath.Join(workspace, "acked-diagnosis")); err != nil || stderr != "" {
		t.Fatalf("diagnose: %v %s", err, stderr)
	}
	reportPath := filepath.Join(workspace, "acked-diagnosis", "report.json")
	reportIdentity := sha256Hex(mustRead(t, reportPath))
	decisions := decisionsFor(t, reportIdentity)
	app := desktopApp(t, workspace)
	if opened := app.OpenCase(workspace, "acked"); opened.State != desktop.Completed {
		t.Fatalf("the case was not verified: %+v", opened)
	}
	request := desktop.FindingReviewRequest{
		Workspace: workspace, Case: "acked", Identity: identity, Report: "acked-diagnosis",
		ReportSHA256: reportIdentity, Decisions: decisions.Decisions,
	}
	before := treeOf(t, workspace)
	previewed := app.ReviewFindings(request)
	if previewed.State != desktop.Completed || previewed.Review == nil || previewed.Output != "" || previewed.DecisionsOutput != "" {
		t.Fatalf("the window did not preview the review: %+v", previewed)
	}
	if after := treeOf(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatal("a preview wrote into the workspace")
	}
	// The same decisions saved as the window's decisions document are what the
	// command line reviews, and its record is the preview's, byte for byte.
	saved := app.SaveFindingDecisions(desktop.RuleDocumentSaveRequest{
		Workspace: workspace, Document: string(encodeDecisions(t, decisions)), Output: "decisions.json",
	})
	if saved.State != desktop.Completed {
		t.Fatalf("the window did not save the decisions: %+v", saved)
	}
	cliReview := filepath.Join(t.TempDir(), "review")
	if _, stderr, err := run(t, "diagnose", "review", filepath.Join(workspace, "acked-diagnosis"),
		"--case", filepath.Join(workspace, "acked"), "--decisions", filepath.Join(workspace, "decisions.json"), "--output", cliReview); err != nil || stderr != "" {
		t.Fatalf("diagnose review: %v %s", err, stderr)
	}
	record, err := findingreview.JSON(previewed.Review.Record)
	if err != nil {
		t.Fatal(err)
	}
	if cli := mustRead(t, filepath.Join(cliReview, "review.json")); !bytes.Equal(record, cli) {
		t.Fatalf("the window previewed %s and the command line recorded %s", record, cli)
	}
}

// The decisions editor opens a retained decisions document with the reader
// `readmit diagnose review` applies — the shipped example accepted and its
// refused sibling refused in the reader's sentence — and saves one canonical
// new entry the command line reviews, named by the digest of its bytes. The
// shipped example names a placeholder report, so over a real diagnosis it is a
// stale decision the command line refuses.
func TestTheWindowOpensAndSavesTheDecisionsReadmitDiagnoseReviewReads(t *testing.T) {
	workspace, identity := panelWorkspace(t)
	if _, stderr, err := run(t, "diagnose", filepath.Join(workspace, "acked"), "--output", filepath.Join(workspace, "acked-diagnosis")); err != nil || stderr != "" {
		t.Fatalf("diagnose: %v %s", err, stderr)
	}
	reportIdentity := sha256Hex(mustRead(t, filepath.Join(workspace, "acked-diagnosis", "report.json")))
	placeRetained(t, "../testdata/fixtures/finding-decisions.json", filepath.Join(workspace, "shipped-decisions.json"))
	placeRetained(t, "../testdata/fixtures/finding-decisions-refused.json", filepath.Join(workspace, "refused-decisions.json"))
	app := desktopApp(t, workspace)

	shipped := mustRead(t, filepath.Join(workspace, "shipped-decisions.json"))
	expected, err := findingreview.ParseDecisions(shipped)
	if err != nil {
		t.Fatal(err)
	}
	opened := app.OpenFindingDecisions(workspace, "shipped-decisions.json")
	if opened.State != desktop.Completed || opened.Decisions == nil || !reflect.DeepEqual(*opened.Decisions, expected) ||
		opened.SHA256 != sha256Hex(shipped) {
		t.Fatalf("the window opened other decisions than the reader reads: %+v", opened)
	}
	if opened.Decisions.Report == reportIdentity {
		t.Fatal("the shipped example names a real report")
	}
	refusedByCommandLine(t, "these decisions were recorded against a different diagnosis report; finding identifiers name other findings there",
		"diagnose", "review", filepath.Join(workspace, "acked-diagnosis"), "--case", filepath.Join(workspace, "acked"),
		"--decisions", filepath.Join(workspace, "shipped-decisions.json"), "--output", filepath.Join(t.TempDir(), "review"))

	refused := app.OpenFindingDecisions(workspace, "refused-decisions.json")
	if refused.State != desktop.Failed || refused.Decisions != nil || refused.Reason == "" {
		t.Fatalf("the window opened decisions the reader refuses: %+v", refused)
	}
	refusedByCommandLine(t, refused.Reason, "diagnose", "review", filepath.Join(workspace, "acked-diagnosis"), "--case", filepath.Join(workspace, "acked"),
		"--decisions", filepath.Join(workspace, "refused-decisions.json"), "--output", filepath.Join(t.TempDir(), "review"))

	// The editor's document, bound to the report on screen, is saved as one
	// canonical new entry: the digest reported is the file's, it reopens as
	// itself, and the command line reviews it into the record the window's
	// own review of those decisions is.
	decisions := decisionsFor(t, reportIdentity)
	saved := app.SaveFindingDecisions(desktop.RuleDocumentSaveRequest{
		Workspace: workspace, Document: string(encodeDecisions(t, decisions)), Output: "my-decisions.json",
	})
	written := mustRead(t, filepath.Join(workspace, "my-decisions.json"))
	if saved.State != desktop.Completed || saved.SHA256 != sha256Hex(written) || saved.Document != string(written) ||
		saved.Decisions == nil || !reflect.DeepEqual(*saved.Decisions, decisions) {
		t.Fatalf("the window saved other decisions than it reported: %+v", saved)
	}
	if again := app.OpenFindingDecisions(workspace, "my-decisions.json"); again.SHA256 != saved.SHA256 || !reflect.DeepEqual(again.Decisions, saved.Decisions) {
		t.Fatalf("the saved decisions do not reopen as themselves: %+v", again)
	}
	cliReview := filepath.Join(t.TempDir(), "review")
	if _, stderr, err := run(t, "diagnose", "review", filepath.Join(workspace, "acked-diagnosis"), "--case", filepath.Join(workspace, "acked"),
		"--decisions", filepath.Join(workspace, "my-decisions.json"), "--output", cliReview); err != nil || stderr != "" {
		t.Fatalf("diagnose review: %v %s", err, stderr)
	}
	previewed := app.ReviewFindings(desktop.FindingReviewRequest{
		Workspace: workspace, Case: "acked", Identity: identity, Report: "acked-diagnosis",
		ReportSHA256: reportIdentity, Decisions: saved.Decisions.Decisions,
	})
	if previewed.State != desktop.Completed || previewed.Review == nil || previewed.Review.Record.Decisions != saved.SHA256 {
		t.Fatalf("the window's review does not name the saved decisions: %+v", previewed)
	}
	record, err := findingreview.JSON(previewed.Review.Record)
	if err != nil {
		t.Fatal(err)
	}
	if cli := mustRead(t, filepath.Join(cliReview, "review.json")); !bytes.Equal(record, cli) {
		t.Fatalf("the window reviewed %s and the command line %s", record, cli)
	}
	// Saving never overwrites: an entry already there is refused, unchanged.
	again := app.SaveFindingDecisions(desktop.RuleDocumentSaveRequest{Workspace: workspace, Document: string(written), Output: "shipped-decisions.json"})
	if again.State != desktop.Failed || !bytes.Equal(mustRead(t, filepath.Join(workspace, "shipped-decisions.json")), shipped) {
		t.Fatalf("a save overwrote a retained decisions document: %+v", again)
	}
}

// The configuration editor opens a retained diagnose configuration with the
// reader `readmit diagnose --config` applies, named by the digest of its
// bytes. One naming a rule this release does not define is valid data: the
// window's diagnosis under it is the command line's, byte for byte, with the
// rule reported as not evaluated. One of another contract version is refused
// by open, by a diagnosis and by the command line in one sentence.
func TestTheWindowOpensTheConfigurationReadmitDiagnoseRuns(t *testing.T) {
	workspace, identity := panelWorkspace(t)
	const colleague = `{
  "schema": "readmit-diagnose-config/v1",
  "profile": "readmit-siu-v1",
  "ruleset": "readmit-siu-diagnosis/v1",
  "rules": ["ack.msa-outcome", "siu.retired-rule"],
  "namespaces": [{"key": "READMIT", "namespace": "READMIT", "universal_id": "", "universal_id_type": ""}]
}
`
	writeDocument(t, workspace, "colleague-config.json", colleague)
	writeDocument(t, workspace, "later-config.json", `{"schema":"readmit-diagnose-config/v2","profile":"readmit-siu-v1","ruleset":"readmit-siu-diagnosis/v1","rules":["ack.msa-outcome"],"namespaces":[]}`)
	expected, err := diagnose.ParseConfig([]byte(colleague))
	if err != nil {
		t.Fatal(err)
	}
	app := desktopApp(t, workspace)
	opened := app.OpenDiagnoseConfig(workspace, "colleague-config.json")
	if opened.State != desktop.Completed || opened.Config == nil || !reflect.DeepEqual(*opened.Config, expected) ||
		opened.SHA256 != sha256Hex([]byte(colleague)) {
		t.Fatalf("the window opened another configuration than the reader reads: %+v", opened)
	}

	// A diagnosis under it is the command line's, and the retired rule is
	// reported as the one thing not evaluated.
	cliDiagnosis := filepath.Join(t.TempDir(), "diagnosis")
	if _, stderr, err := run(t, "diagnose", filepath.Join(workspace, "acked"), "--config", filepath.Join(workspace, "colleague-config.json"), "--output", cliDiagnosis); err != nil || stderr != "" {
		t.Fatalf("diagnose --config: %v %s", err, stderr)
	}
	if verified := app.OpenCase(workspace, "acked"); verified.State != desktop.Completed {
		t.Fatalf("the case was not verified: %+v", verified)
	}
	ran := app.RunDiagnosis(desktop.DiagnosisRequest{Workspace: workspace, Case: "acked", Identity: identity, Config: "colleague-config.json", Output: "colleague-diagnosis"})
	if ran.State != desktop.Completed || ran.Diagnosis == nil {
		t.Fatalf("the window did not diagnose under the configuration: %+v", ran)
	}
	for _, name := range []string{"report.json", "report.md"} {
		if !bytes.Equal(mustRead(t, filepath.Join(cliDiagnosis, name)), mustRead(t, filepath.Join(workspace, "colleague-diagnosis", name))) {
			t.Fatalf("%s differs between the window and the command line", name)
		}
	}
	if len(ran.Diagnosis.Unsupported) != 1 || ran.Diagnosis.Unsupported[0].Code != "unsupported_rule" ||
		ran.Diagnosis.Unsupported[0].Detail != "A configured rule is unsupported: siu.retired-rule" {
		t.Fatalf("the retired rule is not reported as not evaluated: %+v", ran.Diagnosis.Unsupported)
	}
	if !reflect.DeepEqual(ran.Diagnosis.Rules, []string{"ack.msa-outcome"}) || ran.Diagnosis.Total != 1 {
		t.Fatalf("the diagnosis evaluated other rules than the configuration's supported one: %+v", ran.Diagnosis)
	}

	// Saved again from the editor's document, it is a new entry the command
	// line diagnoses under identically.
	saved := app.SaveDiagnoseConfig(desktop.RuleDocumentSaveRequest{Workspace: workspace, Document: opened.Document, Output: "extended-config.json"})
	if saved.State != desktop.Completed || saved.SHA256 != sha256Hex(mustRead(t, filepath.Join(workspace, "extended-config.json"))) {
		t.Fatalf("the window did not save the configuration it opened: %+v", saved)
	}
	resaved := filepath.Join(t.TempDir(), "diagnosis")
	if _, stderr, err := run(t, "diagnose", filepath.Join(workspace, "acked"), "--config", filepath.Join(workspace, "extended-config.json"), "--output", resaved); err != nil || stderr != "" {
		t.Fatalf("diagnose --config: %v %s", err, stderr)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(resaved, "report.json")), mustRead(t, filepath.Join(cliDiagnosis, "report.json"))) {
		t.Fatal("the saved configuration diagnoses differently from the one it was opened from")
	}

	// Another contract version is refused by the editor, by a diagnosis and
	// by the command line in one sentence, and no report is written.
	later := app.OpenDiagnoseConfig(workspace, "later-config.json")
	if later.State != desktop.Failed || later.Config != nil || later.Reason != "unsupported diagnosis configuration schema" {
		t.Fatalf("the window opened a configuration of another contract version: %+v", later)
	}
	refusedRun := app.RunDiagnosis(desktop.DiagnosisRequest{Workspace: workspace, Case: "acked", Identity: identity, Config: "later-config.json", Output: "never-written"})
	if refusedRun.State != desktop.Failed || refusedRun.Reason != later.Reason {
		t.Fatalf("the window diagnosed under a configuration it cannot read: %+v", refusedRun)
	}
	refusedByCommandLine(t, later.Reason, "diagnose", filepath.Join(workspace, "acked"), "--config", filepath.Join(workspace, "later-config.json"), "--output", filepath.Join(t.TempDir(), "never"))
	if _, err := os.Lstat(filepath.Join(workspace, "never-written")); !os.IsNotExist(err) {
		t.Fatal("a refused diagnosis left a report directory behind")
	}
}
