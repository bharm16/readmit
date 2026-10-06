package desktop_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
)

func connectedHandoff(output string) desktop.CIHandoffRequest {
	return desktop.CIHandoffRequest{Integration: "posix", Binary: "/opt/readmit/readmit", OperationPolicy: "/etc/readmit/operation-policy.json",
		SuiteFile: "/srv/readmit/suite.json", Environment: "qa", RunDirectory: "/var/lib/readmit-ci/run-1", Output: output,
		Connected: &desktop.CIConnectedStep{RunnerConfig: "/etc/readmit/runner.json", Authority: "/etc/readmit/connected-authority.json", Promotion: "/srv/readmit/promotion.json", PromotionIdentity: strings.Repeat("a", 64), Revision: "fixture build 7", Instance: "dispatch-1"}}
}

func TestConnectedCompletionErrorRemainsIncompleteInCoverageAndRunDetail(t *testing.T) {
	parallelLifecycleTest(t)
	h, document := desktopConnectedFixture(t)
	path, output := filepath.Join(h.Root, "suite.json"), filepath.Join(h.Root, "completion-error")
	connectedlab.WriteJSON(t, path, document)
	report, err := suite.RunConnected(t.Context(), suite.ConnectedRequest{Path: path, Environment: "qa", Output: output, Instance: "completion-error", Execute: func(ctx context.Context, p *connectedrun.PreparedFlow, destination string) (connectedrun.FlowResult, error) {
		for name, binding := range p.Bindings() {
			connectedlab.WriteJSON(t, filepath.Join(h.Root, connectedlab.GrantFile(name)), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
		}
		actual, err := connectedrun.ExecuteFlow(ctx, p, destination, testisolation.Confirmation{})
		if err != nil {
			return actual, err
		}
		return actual, errors.New("late completion write failed")
	}})
	if err != nil || report.ExitCode() != 2 || !report.Jobs[0].ExecutionError || report.Jobs[0].Flow == nil || report.Jobs[0].Flow.State != "complete" || report.Jobs[0].Flow.Verdict != "pass" || h.Lab.Creates.Load() != 1 {
		t.Fatal("a readable passing child erased its completion-write error", report, err)
	}
	execution, err := suite.OpenConnectedExecution(t.Context(), output)
	if err != nil || execution.Report.ExitCode() != 2 || !execution.Report.Jobs[0].ExecutionError {
		t.Fatal("passive suite inspection lost the completion-write error", err)
	}
	job := document.Tests[0]
	coverage := suite.ConnectedCoverageDocument{Schema: suite.ConnectedCoverageSchema, SuiteSHA256: execution.Preparation.Suite, Specifications: []suite.ConnectedCoverageSpecification{{Job: job.ID, Plan: execution.Queue.Jobs[0].PlanIdentity, Definition: job.Definition, Release: job.ReleaseIdentity}}, Requirements: []suite.Requirement{{ID: "one-appointment", Jobs: []string{job.ID}}}, Exclusions: []suite.Exclusion{}}
	coveragePath := filepath.Join(h.Root, "coverage.json")
	connectedlab.WriteJSON(t, coveragePath, coverage)
	h.Lab.Server().Close()
	assessment, err := suite.AssessConnectedCoverage(t.Context(), output, coveragePath, time.Now())
	if err != nil || assessment.Passed != 0 || len(assessment.Jobs) != 1 || assessment.Jobs[0].Eligible {
		t.Fatal("completion-write error qualified as successful coverage", assessment, err)
	}
	app := workspaceApp(t)
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Completion error", Location: t.TempDir()})
	if created.State != desktop.Completed {
		t.Fatal(created.Reason)
	}
	if err := os.CopyFS(created.Context.Project, os.DirFS(h.Root)); err != nil {
		t.Fatal(err)
	}
	run := listed(t, app, created.Context.Project, desktop.RunItem)["@completion-error"]
	opened := app.OpenRun(desktop.RunRequest{Context: created.Context, Run: run.Ref, Job: job.ID})
	if opened.State != desktop.Completed || opened.Run == nil || opened.Run.Result != desktop.RunIncomplete || len(opened.Run.Jobs) != 1 || opened.Run.Jobs[0].Result != desktop.RunIncomplete {
		t.Fatal("the actual saved job detail called an errored execution passed", opened)
	}
}

func TestConnectedReviewFixHostedGatedHandoffsUseYAMLSpaces(t *testing.T) {
	app := workspaceApp(t)
	for _, connected := range []bool{false, true} {
		for integration, indentation := range map[string]int{"github": 10, "azure": 6} {
			request := connectedHandoff(filepath.Join(t.TempDir(), "handoff.yml"))
			request.Integration = integration
			request.CoverageFile = "/srv/readmit/coverage.json"
			request.Gate = &desktop.CIGateStep{Releases: "/srv/readmit/releases.json", Promotion: "/srv/readmit/promotion.json", PromotionIdentity: strings.Repeat("a", 64), Revision: "fixture build 7", Baseline: "/var/lib/readmit-ci/baseline", Policy: "/srv/readmit/gate-policy.json", PolicyIdentity: strings.Repeat("b", 64), SnapshotDirectory: "/var/lib/readmit-ci/gate"}
			if !connected {
				request.Connected = nil
			}
			result := app.SaveCIHandoff(request)
			if result.State != desktop.Completed {
				t.Fatal(result.Reason)
			}
			commands := 0
			for _, line := range strings.Split(result.Document, "\n") {
				command := strings.TrimLeft(line, " \t")
				if !strings.HasPrefix(command, `"$READMIT_BIN"`) {
					continue
				}
				commands++
				if line[:len(line)-len(command)] != strings.Repeat(" ", indentation) {
					t.Errorf("%s connected=%t has invalid YAML command indentation: %q", integration, connected, line)
				}
			}
			if commands != 2 {
				t.Fatal("hosted workflow did not contain exactly its suite and gate commands")
			}
		}
	}
}

func TestConnectedGateFacadeReverifiesActualSnapshotOffline(t *testing.T) {
	parallelLifecycleTest(t)
	h, document := desktopConnectedFixture(t)
	path := filepath.Join(h.Root, "suite.json")
	connectedlab.WriteJSON(t, path, document)
	review, err := suite.ReviewConnectedPromotion(path, "qa", "independent-lab")
	if err != nil {
		t.Fatal(err)
	}
	promotionPath := filepath.Join(h.Root, "promotion.json")
	promotion, err := suite.ApproveConnectedPromotion(path, "qa", "independent-lab", review.Identity(), "Operator", "Exact synthetic QA binding", promotionPath)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{filepath.Join(h.Root, "baseline"), filepath.Join(h.Root, "current")}
	for i, output := range outputs {
		report, err := suite.RunConnected(t.Context(), suite.ConnectedRequest{Path: path, Environment: "qa", Output: output, Instance: "gate-" + string(rune('a'+i)), Promotion: promotionPath, PromotionIdentity: promotion.Identity(), Revision: "independent-lab",
			Execute: func(ctx context.Context, prepared *connectedrun.PreparedFlow, destination string) (connectedrun.FlowResult, error) {
				for name, binding := range prepared.Bindings() {
					connectedlab.WriteJSON(t, filepath.Join(h.Root, connectedlab.GrantFile(name)), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
				}
				return connectedrun.ExecuteFlow(ctx, prepared, destination, testisolation.Confirmation{})
			}})
		if err != nil || report.ExitCode() != 0 {
			t.Fatal("literal one-appointment baseline/current did not pass", err)
		}
	}
	baseline, err := suite.OpenConnectedExecution(t.Context(), outputs[0])
	if err != nil {
		t.Fatal(err)
	}
	current, err := suite.OpenConnectedExecution(t.Context(), outputs[1])
	if err != nil {
		t.Fatal(err)
	}
	job := current.Document.Tests[0]
	coverage := suite.ConnectedCoverageDocument{Schema: suite.ConnectedCoverageSchema, SuiteSHA256: current.Preparation.Suite, Specifications: []suite.ConnectedCoverageSpecification{{Job: job.ID, Plan: current.Queue.Jobs[0].PlanIdentity, Definition: job.Definition, Release: job.ReleaseIdentity}}, Requirements: []suite.Requirement{{ID: "one-appointment", Jobs: []string{"booking"}}}, Exclusions: []suite.Exclusion{}}
	policy := suite.ConnectedGatePolicy{Schema: suite.ConnectedGatePolicySchema, Environment: "qa", Revision: "independent-lab", Engine: current.Preparation.Capabilities.Engine, Promotion: promotion.Identity(), Input: current.Preparation.Input, Baseline: baseline.Identity, Coverage: coverage, MaxBytes: 64 << 20, RetainUntil: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Approver: "Reviewer", Rationale: "Reviewed actual one-appointment oracle"}
	policyPath, snapshot := filepath.Join(h.Root, "gate-policy.json"), filepath.Join(h.Root, "gate")
	connectedlab.WriteJSON(t, policyPath, policy)
	expected := suite.RetainConnectedGate(t.Context(), outputs[1], outputs[0], policyPath, policy.Identity(), snapshot, time.Now())
	if expected.ExitCode != 0 {
		t.Fatal("actual core gate did not retain a passing comparison", expected)
	}
	app := workspaceApp(t)
	inspected := app.InspectGatePolicy(policyPath)
	if inspected.State != desktop.Completed || inspected.Identity != policy.Identity() || inspected.Specifications != 1 {
		t.Fatal("facade did not read the actual v2 gate policy", inspected)
	}
	h.Lab.Server().Close()
	if err := os.Remove(promotionPath); err != nil {
		t.Fatal(err)
	}
	imported := app.InspectCIResults(snapshot)
	if imported.State != desktop.Completed || imported.CI != nil || imported.Gate == nil || imported.Gate.Schema != suite.ConnectedGateSchema || imported.Gate.State != expected.State || imported.Gate.ExitCode != expected.ExitCode || imported.Warning == "" {
		t.Fatal("facade did not read the actual standalone saved gate summary offline", imported)
	}
	verified := app.VerifyCIGate(snapshot, policy.Identity())
	if verified.State != desktop.Completed || verified.Gate == nil || !reflect.DeepEqual(*verified.Gate, suite.VerifyConnectedGate(t.Context(), snapshot, policy.Identity(), time.Now())) {
		t.Fatal("facade did not verify actual linked gate proof offline", verified)
	}
	if wrong := app.VerifyCIGate(snapshot, strings.Repeat("b", 64)); wrong.State != desktop.Failed || wrong.Gate == nil || wrong.Gate.ExitCode == 0 {
		t.Fatal("wrong policy identity became a passing gate", wrong)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "current", "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if altered := app.VerifyCIGate(snapshot, policy.Identity()); altered.State != desktop.Failed || altered.Gate == nil || altered.Gate.ExitCode == 0 {
		t.Fatal("tampered actual proof passed passive facade verification", altered)
	}
	if altered := app.InspectCIResults(snapshot); altered.State != desktop.Failed || altered.Gate != nil {
		t.Fatal("tampered saved summary passed passive facade inspection", altered)
	}
}

func desktopConnectedFixture(t *testing.T) (*connectedlab.Harness, suite.ConnectedDocument) {
	t.Helper()
	h := connectedlab.New(t, "")
	rows := h.Observe("book", "appointments", "after", "Appointment", "identifier=urn%3Areadmit-lab%3Aappointment%7CDESKTOP-1", "reference-fhir-store", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"))
	datasets := []connectedtest.Dataset{rows}
	h.Compile(connectedtest.FlowTest{ID: "booking", Steps: []connectedtest.Step{h.FHIRStep("create", "POST", "Appointment", `{"resourceType":"Appointment","identifier":[{"system":"urn:readmit-lab:appointment","value":"DESKTOP-1"}],"status":"booked","participant":[{"status":"accepted"}]}`, connectedtest.FHIRHeaders{}, nil)}, Phases: []connectedtest.FlowPhase{{ID: "book", Steps: []string{"create"}, Datasets: datasets, Checks: h.Checks("book", datasets, connectedlab.RowCount("one", "appointments", 1))}}})
	review, err := expectation.ReviewConnected(h.PlanPath)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.ApproveConnected(h.PlanPath, "", review.Identity(), "Reviewer", "Independent appointment count", filepath.Join(h.Root, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc := suite.ConnectedDocument{Schema: suite.ConnectedSchema, ID: "connected-regression", Owner: "interop", Tags: []string{}, Parallelism: 1,
		Tests:        []suite.ConnectedTest{{ID: "booking", Revision: "1", Definition: review.Definition, Release: "release.json", ReleaseIdentity: release.Identity(), After: []string{}, State: "enabled"}},
		Environments: []suite.ConnectedEnvironment{{ID: "qa", Bindings: []suite.ConnectedBinding{{Test: "booking", Plan: "flow-plan", PlanIdentity: h.Plan.Identity(), Config: "flow-config.json"}}}}}
	return h, doc
}

func TestConnectedSuiteSavesAndReopensThroughTheNamedCatalog(t *testing.T) {
	h, doc := desktopConnectedFixture(t)
	app := workspaceApp(t)
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Connected QA", Location: t.TempDir()})
	if created.State != desktop.Completed {
		t.Fatal(created.Reason)
	}
	if err := os.CopyFS(created.Context.Project, os.DirFS(h.Root)); err != nil {
		t.Fatal(err)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: created.Context, Kind: desktop.SuiteItem, IntentID: "connected-suite-save", Draft: desktop.ItemDraft{Name: "Connected regression", Suite: &desktop.SuiteDraft{Connected: &desktop.ConnectedSuiteDraft{Document: doc}}}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil || saved.Saved.Revision != "1" {
		t.Fatalf("normal logical save did not publish a connected suite: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: created.Context, Ref: *saved.Saved})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.Suite == nil || opened.Draft.Suite.Connected == nil || opened.Suite == nil || !opened.Suite.Runnable || !reflect.DeepEqual(opened.Draft.Suite.Connected.Document, doc) {
		t.Fatalf("saved connected version did not reopen whole: %+v", opened)
	}
	if _, err := suite.DecodeConnected([]byte(opened.Suite.Document)); err != nil {
		t.Fatal("catalog did not retain the core suite language", err)
	}
	if _, err := suite.Decode([]byte(opened.Suite.Document)); err == nil {
		t.Fatal("connected suite was silently converted into a legacy suite")
	}
	target := desktop.SuiteRunTarget{Context: created.Context, Suite: *saved.Saved}
	preflight := app.PreflightRun(desktop.RunPreflightRequest{Suite: &target, Environment: "qa"})
	if preflight.State != desktop.Completed || preflight.Preflight == nil || preflight.Preflight.Schema != suite.ConnectedSchema || preflight.Preflight.Connected == nil || len(preflight.Preflight.Connected.Jobs) != 1 || preflight.Preflight.Connected.Jobs[0].PlanIdentity != h.Plan.Identity() || preflight.Preflight.Admission.Admitted {
		t.Fatalf("named connected suite did not reach passive core preparation: %+v", preflight)
	}
	refused := app.StartSuiteRun(desktop.SuiteRunRequest{Item: &target, Environment: "qa", Output: "missing-authority-run", Expected: preflight.Preflight.Identity})
	if refused.State != desktop.Failed || !strings.Contains(refused.Reason, "runner") {
		t.Fatal("connected suite ran without separately installed authority", refused)
	}
	if _, err := os.Lstat(filepath.Join(created.Context.Project, "missing-authority-run")); !os.IsNotExist(err) {
		t.Fatal("missing authority created an execution artifact")
	}
	if h.Lab.Creates.Load() != 0 || h.Lab.Tokens.Load() != 0 {
		t.Fatal("logical save or reopening contacted a target or credential provider")
	}
}

func TestConnectedCIHandoffUsesOneActualSuiteCommandAndStableDispatch(t *testing.T) {
	app := workspaceApp(t)
	for _, integration := range []string{"posix", "github", "azure"} {
		request := connectedHandoff(filepath.Join(t.TempDir(), "handoff"))
		request.Integration = integration
		result := app.SaveCIHandoff(request)
		if result.State != desktop.Completed {
			t.Fatal(result.Reason)
		}
		workflow := result.Document[strings.Index(result.Document, "# --- reviewed workflow"):]
		for _, flag := range []string{`--runner-config "$RUNNER_CONFIG"`, `--authority "$RUNNER_AUTHORITY"`, `--promotion "$PROMOTION_FILE"`, `--promotion-identity "$PROMOTION_IDENTITY"`, `--revision "$TARGET_REVISION"`, `--instance "$DISPATCH_ID"`} {
			if !strings.Contains(workflow, flag) {
				t.Errorf("%s omitted %s", integration, flag)
			}
		}
		if strings.Count(workflow, "suite ci") != 1 || strings.Contains(workflow, "--releases") || strings.Contains(workflow, "--requirements") {
			t.Fatal("connected handoff split execution or borrowed legacy release/coverage contracts")
		}
		for _, value := range []string{request.Connected.RunnerConfig, request.Connected.Authority, request.Connected.Promotion, request.Connected.PromotionIdentity, request.Connected.Revision, request.Connected.Instance} {
			if strings.Contains(workflow, value) {
				t.Fatal("connected workflow embedded a customer configuration value")
			}
		}
	}
}

func TestConnectedCIHandoffRefusesIncompleteGateBeforeWriting(t *testing.T) {
	app := workspaceApp(t)
	request := connectedHandoff(filepath.Join(t.TempDir(), "handoff"))
	request.Gate = &desktop.CIGateStep{}
	result := app.SaveCIHandoff(request)
	if result.State != desktop.Failed || !strings.Contains(result.Reason, "baseline") {
		t.Fatal("incomplete gate was presented as connected assessment", result)
	}
	if _, err := os.Lstat(request.Output); !os.IsNotExist(err) {
		t.Fatal("unsupported connected gate wrote a misleading workflow")
	}
}

func TestConnectedCIHandoffRetainsTheCoreChangeGateAndOptionalCoverage(t *testing.T) {
	app := workspaceApp(t)
	request := connectedHandoff(filepath.Join(t.TempDir(), "handoff"))
	request.CoverageFile = "/srv/readmit/connected-coverage.json"
	request.Gate = &desktop.CIGateStep{Baseline: "/var/lib/readmit-ci/baseline", Policy: "/srv/readmit/connected-gate-policy.json", PolicyIdentity: strings.Repeat("b", 64), SnapshotDirectory: "/var/lib/readmit-ci/gate"}
	result := app.SaveCIHandoff(request)
	if result.State != desktop.Completed {
		t.Fatal(result.Reason)
	}
	workflow := result.Document[strings.Index(result.Document, "# --- reviewed workflow"):]
	if !strings.Contains(workflow, `--requirements "$COVERAGE_FILE"`) || !strings.Contains(workflow, `suite gate "$RUN_DIRECTORY/execution"`) || strings.Contains(workflow, "--releases") || !strings.Contains(workflow, `exit "$execution"`) {
		t.Fatal("connected generated workflow lost its actual coverage/gate path or suite exit")
	}
}
