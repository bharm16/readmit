package hub_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
)

func assertConnectedWireVisibility(t *testing.T, report *runqueue.ConnectedReport, reveal bool) {
	t.Helper()
	if report == nil || len(report.Jobs) != 1 || report.Jobs[0].Flow == nil || len(report.Jobs[0].Flow.Phases) != 2 || report.Jobs[0].Flow.Phases[0].Wire == nil {
		t.Fatal("actual connected wire report was not retained")
	}
	wire := report.Jobs[0].Flow.Phases[0].Wire
	if wire.Verdict != "pass" || wire.Passed != 1 || len(wire.Results) != 1 || wire.Results[0].Outcome != "passed" {
		t.Fatal("hiding original text changed the actual wire oracle")
	}
	for _, field := range []*assertion.FieldValue{wire.Results[0].Observed.Field, wire.Results[0].Observed.Compared} {
		if field == nil || field.State != "present" {
			t.Fatal("hiding text changed the source field state")
		}
		if reveal && (field.Text == nil || *field.Text != "RUNNER-BOOK") || !reveal && field.Text != nil {
			t.Fatal("connected field text bypassed the deliberate reveal boundary")
		}
	}
}

func TestConnectedNamedDesktopSuiteUsesActualRunnerAndSameRetainedOracle(t *testing.T) {
	f := newConnectedHubFixture(t)
	h, config, coreRequest, coreAuthority := provisionConnectedSuite(t, f)
	core, err := executeCustomerConnectedSuite(t, config, coreRequest, coreAuthority)
	if err != nil || core.ExitCode() != 0 || len(core.Jobs) != 1 || core.Jobs[0].Flow == nil {
		t.Fatal("core customer runner did not establish the literal one-Encounter oracle", err)
	}
	app := desktop.New(nil, desktop.ShellDocuments{Folder: t.TempDir()})
	if selected := app.SelectOperationPolicy(testlicense.New(t)); selected.State != desktop.Completed {
		t.Fatal(selected.Reason)
	}
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Connected customer QA", Location: t.TempDir()})
	if created.State != desktop.Completed {
		t.Fatal(created.Reason)
	}
	root := created.Context.Project
	if err := os.CopyFS(root, os.DirFS(h.Root)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(coreRequest.Path)
	if err != nil {
		t.Fatal(err)
	}
	document, err := suite.DecodeConnected(raw)
	if err != nil {
		t.Fatal(err)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: created.Context, Kind: desktop.SuiteItem, IntentID: "connected-suite", Draft: desktop.ItemDraft{Name: "Connected customer regression", Suite: &desktop.SuiteDraft{Connected: &desktop.ConnectedSuiteDraft{Document: document}}}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("named connected suite did not publish: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: created.Context, Ref: *saved.Saved})
	if opened.State != desktop.Completed || opened.Suite == nil || opened.Draft == nil || opened.Draft.Suite == nil || opened.Draft.Suite.Connected == nil {
		t.Fatal("named connected suite did not reopen", opened.Reason)
	}
	// The customer's administrator approves the exact named saved bytes.
	// This separate installation is neither a passing-run approval nor a UI
	// callback that fabricates admission.
	approvedSuite := filepath.Join(root, ".readmit-compiled-suite-approved.json")
	if err := os.WriteFile(approvedSuite, []byte(opened.Suite.Document), 0600); err != nil {
		t.Fatal(err)
	}
	review, err := suite.ReviewConnectedPromotion(approvedSuite, "qa", coreRequest.Revision)
	if err != nil {
		t.Fatal(err)
	}
	promotionPath := filepath.Join(root, "facade-promotion.json")
	promotion, err := suite.ApproveConnectedPromotion(approvedSuite, "qa", coreRequest.Revision, review.Identity(), "Operator", "Exact saved Desktop revision", promotionPath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := suite.PrepareConnected(suite.ConnectedRequest{Path: approvedSuite, Environment: "qa", Output: filepath.Join(t.TempDir(), "prepared")})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := prepared.Capabilities.Identity()
	if err != nil {
		t.Fatal(err)
	}
	connectedlab.WriteJSON(t, filepath.Join(root, "facade-authority.json"), customerrunner.ConnectedAuthority{Schema: customerrunner.ConnectedAuthoritySchema, Actor: "runner", Generation: "1", Input: prepared.Identity, Promotion: promotion.Identity(), Capabilities: capability,
		Operations: prepared.Operations(), IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour), MaxSeconds: 120, MaxOccurrences: 10})
	connectedlab.WriteJSON(t, filepath.Join(root, "runner.json"), config)
	target := desktop.SuiteRunTarget{Context: created.Context, Suite: *saved.Saved}
	options := &desktop.ConnectedSuiteRunOptions{RunnerConfig: "runner.json", Authority: "facade-authority.json", Promotion: "facade-promotion.json", PromotionIdentity: promotion.Identity(), Revision: coreRequest.Revision, Instance: "desktop-occurrence"}
	beforeCalls, beforeWrites := f.requests.Load(), h.Lab.Creates.Load()
	// Schedule preparation uses the same named suite and a named runner.
	// It installs typed dispatch data and authorizes no scheduled send.
	runner := app.SaveItem(desktop.SaveItemRequest{Context: created.Context, Kind: desktop.RunnerItem, IntentID: "named-runner", Draft: desktop.ItemDraft{Name: "Customer QA runner", Runner: &desktop.RunnerDraft{
		Hub: config.Hub, Project: config.Project, Environment: config.Environment, Root: config.Root, CA: config.CA, Certificate: config.Certificate,
		Key: desktop.RunnerReferenceInput{Command: config.Key.Command, Arguments: config.Key.Arguments}, Token: desktop.RunnerReferenceInput{Command: config.Token.Command, Arguments: config.Token.Arguments}, UpdateKey: config.UpdateKey, UpdateEngine: config.UpdateEngine}}})
	if runner.Outcome != desktop.SavedOutcome || runner.Saved == nil {
		t.Fatal("named customer runner did not save", runner.Reason)
	}
	scheduleOptions := *options
	scheduleOptions.RunnerConfig, scheduleOptions.Instance = "", ""
	scheduled := app.PrepareSchedule(desktop.SchedulePrepareRequest{Context: created.Context, Draft: desktop.ScheduleDraft{Name: "Connected nightly", Suite: *saved.Saved, Environment: "qa", Runner: runner.Saved.ID,
		Connected: &scheduleOptions, Repeat: "daily", Days: []string{}, At: "06:00", Zone: "UTC", WindowMinutes: 30}})
	if scheduled.State != desktop.Completed || scheduled.Review == nil || len(scheduled.Review.Tests) != 1 || f.requests.Load() != beforeCalls || h.Lab.Creates.Load() != beforeWrites {
		t.Fatalf("connected schedule was not a passive exact review: %+v", scheduled)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".readmit", "scheduled"))
	if err != nil || len(entries) != 1 {
		t.Fatal("normal schedule review did not retain one managed descriptor", err)
	}
	dispatch, err := suite.ReadConnectedDispatch(filepath.Join(root, ".readmit", "scheduled", entries[0].Name(), "dispatch.json"))
	if err != nil || dispatch.Input != prepared.Identity || dispatch.PromotionIdentity != promotion.Identity() || dispatch.Authority != filepath.Join(root, "facade-authority.json") {
		t.Fatal("scheduled descriptor did not retain exact installed authority and input pins", err)
	}
	reviewed := app.PrepareAction(desktop.PrepareActionRequest{Context: created.Context, Action: desktop.RunSuiteAction, Items: []desktop.ItemRef{target.Suite}, Run: &desktop.RunActionOptions{Environment: "qa", Connected: options}})
	if reviewed.State != desktop.Completed || reviewed.Review == nil || reviewed.Review.Run == nil || reviewed.Review.Run.Connected == nil || !reviewed.Review.Ready || h.Lab.Creates.Load() != beforeWrites || f.requests.Load() != beforeCalls {
		t.Fatalf("passive named review: %+v", reviewed)
	}
	output := reviewed.Review.Destination.Output
	executed := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: created.Context, Token: reviewed.Review.Token, IntentID: "desktop-suite-click"})
	if executed.State != desktop.Completed || executed.Run == nil || executed.ConnectedReport == nil || executed.ConnectedReport.ExitCode() != 0 || executed.ConnectedReport.Executed != 1 || h.Lab.Creates.Load() != 2 || f.requests.Load() <= beforeCalls {
		if executed.ConnectedReport != nil {
			t.Logf("actual queue: %+v", *executed.ConnectedReport)
			for _, job := range executed.ConnectedReport.Jobs {
				if job.Flow != nil {
					t.Logf("actual child: %+v", *job.Flow)
				}
			}
		}
		retained, retainErr := os.MkdirTemp("", "readmit-ig15-facade-failure-")
		if retainErr == nil {
			retainErr = os.CopyFS(filepath.Join(retained, "project"), os.DirFS(root))
			t.Logf("synthetic facade failure retained at %s (copy error %v)", retained, retainErr)
		}
		t.Logf("target creates=%d prior=%d; hub calls=%d prior=%d", h.Lab.Creates.Load(), beforeWrites, f.requests.Load(), beforeCalls)
		t.Fatalf("actual enrolled facade execution: %+v", executed)
	}
	actual := executed.ConnectedReport.Jobs[0].Flow
	assertConnectedWireVisibility(t, executed.ConnectedReport, false)
	if actual == nil || len(actual.Phases) != 2 || len(core.Jobs[0].Flow.Phases) != 2 {
		t.Fatal("facade and core changed the retained semantic oracle")
	}
	for i, phase := range actual.Phases {
		wantChecks := 2
		if i == 0 {
			wantChecks = 3
		}
		if !reflect.DeepEqual(phase.Checks, core.Jobs[0].Flow.Phases[i].Checks) || len(phase.Checks) != wantChecks || phase.Checks[0].Outcome != "passed" || phase.Checks[1].Outcome != "passed" {
			t.Fatal("facade and core changed literal downstream count or start checks")
		}
	}
	// Reusing the same dispatch identity cannot create another actual send.
	retry := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: created.Context, Token: reviewed.Review.Token, IntentID: "desktop-repeat-click"})
	if retry.Outcome != desktop.ActionRefused || h.Lab.Creates.Load() != 2 {
		t.Fatal("facade retried an already retained occurrence", retry)
	}
	legacyOptions := *options
	legacyOptions.Instance = "desktop-legacy-projection"
	preflight := app.PreflightRun(desktop.RunPreflightRequest{Suite: &target, Environment: "qa", Output: "legacy-projection", Connected: &legacyOptions})
	if preflight.State != desktop.Completed || preflight.Preflight == nil || !preflight.Preflight.Admission.Admitted {
		t.Fatal("legacy suite facade could not prepare the exact installed connected scope", preflight)
	}
	started := app.StartSuiteRun(desktop.SuiteRunRequest{Item: &target, Environment: "qa", Output: "legacy-projection", Expected: preflight.Preflight.Identity, Connected: &legacyOptions})
	if started.State != desktop.Completed || started.ConnectedReport == nil || started.ConnectedReport.ExitCode() != 0 || h.Lab.Creates.Load() != 2 {
		t.Fatal("legacy suite facade did not use the actual customer runner", started)
	}
	assertConnectedWireVisibility(t, started.ConnectedReport, false)
	h.Lab.Server().Close()
	f.server.Close()
	if err := os.Remove(config.Key.Arguments[0]); err != nil {
		t.Fatal(err)
	}
	retained, err := suite.OpenConnectedExecution(t.Context(), filepath.Join(root, output))
	if err != nil || retained.Report.ExitCode() != 0 {
		t.Fatal("facade evidence could not reopen offline", err)
	}
	assertConnectedWireVisibility(t, &retained.Report, true)
	if _, err := json.Marshal(retained.Report); err != nil {
		t.Fatal(err)
	}
	openedRun := app.OpenRun(desktop.RunRequest{Context: created.Context, Run: *executed.Run})
	if openedRun.State != desktop.Completed || openedRun.Run == nil || openedRun.Run.ConnectedReport == nil || openedRun.Run.Result != desktop.RunPassed || len(openedRun.Run.Jobs) != 1 {
		t.Fatal("normal run page could not inspect actual linked proof offline", openedRun.Reason)
	}
	assertConnectedWireVisibility(t, openedRun.Run.ConnectedReport, false)
	revealed := app.OpenRun(desktop.RunRequest{Context: created.Context, Run: *executed.Run, Reveal: true})
	if revealed.State != desktop.Completed || revealed.Run == nil {
		t.Fatal("explicit reveal could not read the actual sealed original report", revealed)
	}
	assertConnectedWireVisibility(t, revealed.Run.ConnectedReport, true)
	after, err := suite.OpenConnectedExecution(t.Context(), filepath.Join(root, output))
	if err != nil || after.Identity != retained.Identity {
		t.Fatal("facade hiding or reveal changed the sealed private evidence", err)
	}
}
