package desktop_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/hl7"
)

// diagnosisWorkspace holds the acknowledged-booking fixture as one case of an
// open workspace: a booking whose acknowledgement carries an application error
// with free text, so a default diagnosis produces findings and the free text
// is exactly what must never cross the facade.
func diagnosisWorkspace(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	root := t.TempDir()
	app := workspaceApp(t)
	wire, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "diagnose-acknowledged.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	written := writeInputs(t, root, "acked", []bundle.Input{{
		Path:    "SYNTHETIC-FIXTURE",
		Data:    wire,
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
	}})
	return app, root, written.Identity
}

// diagnosed runs one diagnosis and fails the test if the facade refused it.
func diagnosed(t *testing.T, app *desktop.App, request desktop.DiagnosisRequest) desktop.DiagnosisResult {
	t.Helper()
	result := app.RunDiagnosis(request)
	if result.State != desktop.Completed || result.Diagnosis == nil {
		t.Fatalf("the facade refused a diagnosis it supports: %+v", result)
	}
	return result
}

// The whole delivery in one diagnosis: the report the engine produced, written
// whole into one new directory exactly as the command line writes it, and
// reported back windowed with the identity a review must name.
func TestRunDiagnosisWritesTheReportTheCommandLineWritesAndReportsItsIdentity(t *testing.T) {
	app, root, identity := diagnosisWorkspace(t)
	result := diagnosed(t, app, desktop.DiagnosisRequest{
		Workspace: root, Case: "acked", Identity: identity, Builtin: "siu", Output: "diagnosis-out",
	})
	if result.Output != "diagnosis-out" || result.Diagnosis.ReportSHA256 == "" {
		t.Fatalf("the diagnosis does not name what it wrote: %+v", result)
	}
	// The retained report is byte-identical to the engine's own rendering: the
	// facade reimplements nothing.
	report, err := diagnose.Run(filepath.Join(root, "acked"), diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	want, err := diagnose.JSON(report)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(root, "diagnosis-out", "report.json"))
	if err != nil || string(written) != string(want) {
		t.Fatalf("the retained report is not the engine's own bytes: %v", err)
	}
	if markdown, err := os.ReadFile(filepath.Join(root, "diagnosis-out", "report.md")); err != nil || string(markdown) != string(diagnose.Markdown(report)) {
		t.Fatalf("the retained markdown is not the engine's own rendering: %v", err)
	}
	if result.Diagnosis.ReportSHA256 != sha256Of(written) {
		t.Fatalf("the reported identity is not the digest of the retained report: %s", result.Diagnosis.ReportSHA256)
	}
	if result.Diagnosis.Total != len(report.Findings) || len(result.Diagnosis.Findings) != len(report.Findings) {
		t.Fatalf("the window does not hold the engine's findings: %+v", result.Diagnosis)
	}
	if result.Diagnosis.Schema != diagnose.Schema || result.Diagnosis.Scope == "" {
		t.Fatalf("the diagnosis does not carry the engine's contract and scope: %+v", result.Diagnosis)
	}
	// The fixture's acknowledgement carries free text. None of it crosses.
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"SECRET-FREE-TEXT", "SECRET-ERR-TEXT", "DIAGNOSE-ACKED", root} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("the diagnosis disclosed %q", private)
		}
	}
}

// A diagnosis binds to the identity the window displayed, runs under exactly
// one chosen configuration, and never overwrites a retained report.
func TestRunDiagnosisRefusesStaleIdentityImplicitConfigurationAndOverwrite(t *testing.T) {
	app, root, identity := diagnosisWorkspace(t)
	request := desktop.DiagnosisRequest{Workspace: root, Case: "acked", Identity: identity, Builtin: "siu", Output: "out"}

	stale := request
	stale.Identity = strings.Repeat("0", 64)
	if got := app.RunDiagnosis(stale); got.State != desktop.Failed || got.Diagnosis != nil {
		t.Fatalf("evidence that changed since it was displayed was diagnosed anyway: %+v", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "out")); !os.IsNotExist(err) {
		t.Fatal("a refused diagnosis left a report behind")
	}
	implicit := request
	implicit.Builtin = ""
	if got := app.RunDiagnosis(implicit); got.State != desktop.Failed ||
		!strings.Contains(got.Reason, "one named configuration entry or one built-in selection") {
		t.Fatalf("a diagnosis ran under a configuration nobody selected: %+v", got)
	}
	unknown := request
	unknown.Builtin = "everything"
	if got := app.RunDiagnosis(unknown); got.State != desktop.Failed {
		t.Fatalf("an unknown built-in selection was accepted: %+v", got)
	}
	first := diagnosed(t, app, request)
	original, err := os.ReadFile(filepath.Join(root, "out", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := app.RunDiagnosis(request); got.State != desktop.Failed || !strings.Contains(got.Reason, "destination must be new") {
		t.Fatalf("an existing report directory was not refused by name: %+v", got)
	}
	retained, err := os.ReadFile(filepath.Join(root, "out", "report.json"))
	if err != nil || string(retained) != string(original) {
		t.Fatal("a refused diagnosis changed the retained report")
	}
	if first.Diagnosis.ReportSHA256 != sha256Of(original) {
		t.Fatal("the first diagnosis no longer names the retained report")
	}
}

// A diagnosis can run under an authored configuration entry of the workspace,
// read through the same strict parser the command line applies, and a
// configuration the parser refuses runs nothing.
func TestRunDiagnosisReadsAnAuthoredConfigurationThroughItsOwnParser(t *testing.T) {
	app, root, identity := diagnosisWorkspace(t)
	writeDocument(t, root, "diagnose.config.json",
		`{"schema":"readmit-diagnose-config/v1","profile":"readmit-siu-v1","ruleset":"readmit-siu-diagnosis/v1","rules":["ack.msa-outcome","ack.err-outcome"],"namespaces":[]}`)
	result := diagnosed(t, app, desktop.DiagnosisRequest{
		Workspace: root, Case: "acked", Identity: identity, Config: "diagnose.config.json", Output: "configured-out",
	})
	if result.Diagnosis.Config != "diagnose.config.json" || len(result.Diagnosis.Rules) != 2 {
		t.Fatalf("the diagnosis did not run under the authored configuration: %+v", result.Diagnosis)
	}
	writeDocument(t, root, "broken.config.json", `{"schema":"readmit-diagnose-config/v1","profile":"p!","ruleset":"r","rules":["x"],"namespaces":[]}`)
	refused := app.RunDiagnosis(desktop.DiagnosisRequest{
		Workspace: root, Case: "acked", Identity: identity, Config: "broken.config.json", Output: "refused-out",
	})
	if refused.State != desktop.Failed {
		t.Fatalf("an unreadable configuration ran a diagnosis: %+v", refused)
	}
	if _, err := os.Lstat(filepath.Join(root, "refused-out")); !os.IsNotExist(err) {
		t.Fatal("a refused configuration left a report behind")
	}
}

// Opening a retained report reads it with the same strict reader a review
// uses: the same identity, the same findings, and a report.json declaring any
// other contract is refused rather than displayed as a diagnosis.
func TestOpeningARetainedDiagnosisReportsTheIdentityAReviewMustName(t *testing.T) {
	app, root, identity := diagnosisWorkspace(t)
	ran := diagnosed(t, app, desktop.DiagnosisRequest{
		Workspace: root, Case: "acked", Identity: identity, Builtin: "siu", Output: "diagnosis-out",
	})
	opened := app.OpenDiagnosisReport(root, "diagnosis-out", 0)
	if opened.State != desktop.Completed || opened.Diagnosis == nil {
		t.Fatalf("a retained report did not open: %+v", opened)
	}
	if opened.Diagnosis.ReportSHA256 != ran.Diagnosis.ReportSHA256 || opened.Diagnosis.Total != ran.Diagnosis.Total {
		t.Fatalf("reopening reported a different diagnosis: %+v", opened.Diagnosis)
	}
	// Both name the case the report was run over, so the window can tell a
	// report of this case from a retained report of another one.
	if ran.Diagnosis.CaseIdentity != identity || opened.Diagnosis.CaseIdentity != identity {
		t.Fatalf("the diagnosis does not name the case it was run over: %s %s", ran.Diagnosis.CaseIdentity, opened.Diagnosis.CaseIdentity)
	}
	// A window past the last finding is empty, with every count retained.
	past := app.OpenDiagnosisReport(root, "diagnosis-out", opened.Diagnosis.Total)
	if past.State != desktop.Empty || past.Diagnosis == nil || past.Diagnosis.Total != opened.Diagnosis.Total {
		t.Fatalf("a window past the last finding was not empty with its counts: %+v", past)
	}
	if got := app.OpenDiagnosisReport(root, "diagnosis-out", -1); got.State != desktop.Failed {
		t.Fatalf("a window before the first finding was accepted: %+v", got)
	}
	// Some other report.json is not a diagnosis, and a case is not a report.
	if err := os.MkdirAll(filepath.Join(root, "other-report"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, filepath.Join("other-report", "report.json"), `{"schema":"readmit-other/v1"}`)
	if got := app.OpenDiagnosisReport(root, "other-report", 0); got.State != desktop.Failed || got.Diagnosis != nil {
		t.Fatalf("another contract's report.json was displayed as a diagnosis: %+v", got)
	}
	if got := app.OpenDiagnosisReport(root, "acked", 0); got.State != desktop.Failed {
		t.Fatalf("a case bundle was displayed as a diagnosis: %+v", got)
	}
}

// Grouping re-evaluates the selected cases under one configuration, exactly as
// the command line does, and windows the groups without hiding any case.
func TestGroupDiagnosesGroupsEqualSignaturesAcrossSelectedCases(t *testing.T) {
	app, root, _ := diagnosisWorkspace(t)
	wire, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "diagnose-acknowledged.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	writeInputs(t, root, "acked-again", []bundle.Input{{
		Path:    "SYNTHETIC-FIXTURE-TWO",
		Data:    wire,
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
	}})
	result := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{
		Workspace: root, Cases: []string{"acked", "acked-again"}, Builtin: "siu",
	})
	if result.State != desktop.Completed || result.Groups == nil {
		t.Fatalf("two diagnosable cases were not grouped: %+v", result)
	}
	if len(result.Groups.Cases) != 2 || result.Total == 0 || result.Total != len(result.Groups.Groups) {
		t.Fatalf("the grouping does not carry both complete diagnoses and its own counts: %+v", result)
	}
	for _, report := range result.Groups.Cases {
		entry := result.CaseEntries[report.CaseIdentity]
		if entry != "acked" && entry != "acked-again" {
			t.Fatalf("the evaluated case lost its workspace entry: %q", entry)
		}
	}
	for _, group := range result.Groups.Groups {
		if len(group.Members) != 2 {
			t.Fatalf("the same fixture twice did not group by signature: %+v", group)
		}
	}
	// The engine's own bounds and this facade's own naming refusals hold.
	if got := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: root, Cases: []string{}, Builtin: "siu"}); got.State != desktop.Failed {
		t.Fatalf("a grouping of no cases was accepted: %+v", got)
	}
	if got := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: root, Cases: []string{filepath.Join("..", "acked")}, Builtin: "siu"}); got.State != desktop.Failed ||
		!strings.Contains(got.Reason, "one directory entry of the open workspace") {
		t.Fatalf("a case named outside the workspace was accepted: %+v", got)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"SECRET-FREE-TEXT", "SECRET-ERR-TEXT", root} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("the grouping disclosed %q", private)
		}
	}
}

func TestRetainedDiagnosisGroupsOpenThroughTheirOwnReader(t *testing.T) {
	app, root, _ := diagnosisWorkspace(t)
	grouped, err := diagnose.GroupCases(context.Background(), []string{filepath.Join(root, "acked")}, diagnose.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	data, err := diagnose.GroupsJSON(grouped)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "groups-out"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "groups-out", "report.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	listed := app.OpenWorkspace(root)
	if listed.Workspace == nil {
		t.Fatalf("workspace did not open: %+v", listed)
	}
	for _, artifact := range listed.Workspace.Artifacts {
		if artifact.Name == "groups-out" && artifact.Kind != desktop.DiagnosisGroupsArtifact {
			t.Fatalf("grouping was listed as %q", artifact.Kind)
		}
	}
	opened := app.OpenDiagnosisGroupsReport(root, "groups-out", 0)
	if opened.State != desktop.Completed || opened.Groups == nil || opened.Total != len(grouped.Groups) {
		t.Fatalf("retained grouping did not open: %+v", opened)
	}
	if len(opened.CaseEntries) != 0 {
		t.Fatalf("a saved report invented workspace labels: %+v", opened.CaseEntries)
	}
	if got := app.OpenDiagnosisReport(root, "groups-out", 0); got.State != desktop.Failed || got.Diagnosis != nil {
		t.Fatalf("grouping opened as one diagnosis: %+v", got)
	}
	if got := app.OpenDiagnosisGroupsReport(root, "groups-out", -1); got.State != desktop.Failed || got.Groups != nil {
		t.Fatalf("negative grouping offset was accepted: %+v", got)
	}
	if got := app.OpenDiagnosisGroupsReport(root, "acked", 0); got.State != desktop.Failed || got.Groups != nil {
		t.Fatalf("case bundle opened as a grouping: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(root, "groups-out", "report.json"), []byte(`{"schema":"readmit-diagnosis-groups/v1","unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := app.OpenDiagnosisGroupsReport(root, "groups-out", 0); got.State != desktop.Failed || got.Groups != nil {
		t.Fatalf("an unknown member opened as a grouping: %+v", got)
	}
}

// A grouping reads several cases and is interruptible: cancelled before its
// first case is evaluated, it answers cancelled — not failed, and with no
// groups — and grouping again groups.
func TestACancelledGroupingGroupsNothingAndGroupingAgainGroups(t *testing.T) {
	_, root, _ := diagnosisWorkspace(t)
	wire, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "diagnose-acknowledged.mllp"))
	if err != nil {
		t.Fatal(err)
	}
	writeInputs(t, root, "acked-again", []bundle.Input{{
		Path:    "SYNTHETIC-FIXTURE-TWO",
		Data:    wire,
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
	}})
	request := desktop.GroupDiagnosesRequest{Workspace: root, Cases: []string{"acked", "acked-again"}, Builtin: "siu"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := desktop.GroupDiagnosesWithinForTest(ctx, request)
	if cancelled.State != desktop.Cancelled || cancelled.Groups != nil || cancelled.Reason == "" || cancelled.Total != 0 {
		t.Fatalf("a cancelled grouping was not reported as cancelled: %+v", cancelled)
	}
	grouped := desktop.GroupDiagnosesWithinForTest(context.Background(), request)
	if grouped.State != desktop.Completed || grouped.Groups == nil || len(grouped.Groups.Cases) != 2 {
		t.Fatalf("grouping again did not group: %+v", grouped)
	}
}
