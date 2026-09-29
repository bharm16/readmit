package desktop_test

// A run's own page, a check group decided against a run, and two runs
// compared (#555), each read from what the runs retained and none of them
// sending, resuming or changing anything.

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The retained runs of the native acceptance project, as the saved test they
// executed: the baseline whose ledger check failed beside acknowledged
// messages, and the post-fix run that passed.
func acceptanceRuns(t *testing.T) (*desktop.App, desktop.RequestContext, desktop.ItemRef, map[string]desktop.CatalogItem) {
	t.Helper()
	app, context, retained := runsProject(t)
	test := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "retained", Draft: desktop.ItemDraft{TestDocument: retained}})
	return app, context, test, listed(t, app, context.Project, desktop.RunItem)
}

// A downstream check that failed is Failed with what it expected and what
// was observed, even though every message was acknowledged; the run is
// listed as the saved test version it executed.
func TestARunPageShowsAFailedDownstreamCheckBesideAcknowledgedMessages(t *testing.T) {
	app, context, test, runs := acceptanceRuns(t)
	baseline := runs["@baseline"]
	summary := baseline.Summary.Run
	if baseline.Name != "Native acceptance reschedule" || summary.Kind != desktop.TestRunKind || summary.Result != desktop.RunFailed ||
		summary.Test == nil || summary.Test.ID != test.ID || summary.Version != "1" || summary.DeliveryUncertain {
		t.Fatalf("the baseline in history: %+v %+v", baseline, summary)
	}
	if passed := runs["@post-fix"].Summary.Run; passed.Result != desktop.RunPassed {
		t.Fatalf("the post-fix run in history: %+v", passed)
	}

	opened := app.OpenRun(desktop.RunRequest{Context: context, Run: baseline.Ref})
	detail := opened.Run
	if opened.State != desktop.Completed || detail == nil || len(detail.Checks) != 1 || len(detail.Messages) == 0 {
		t.Fatalf("the baseline's page: %+v", opened)
	}
	check := detail.Checks[0]
	if check.Result != desktop.CheckFailed || check.Check.Operator != testauthor.LedgerCount || check.Check.Count == nil || *check.Check.Count != 1 ||
		check.Observed == nil || check.Observed.Count == nil || *check.Observed.Count != 2 || check.Unavailable != "" || check.Hidden {
		t.Fatalf("the failed ledger check: %+v", check)
	}
	for _, message := range detail.Messages {
		if message.Delivery != desktop.DeliveryAcknowledged || message.ACKCode != "AA" {
			t.Fatalf("a message of the baseline: %+v", message)
		}
	}
	if detail.Details.Boundary != testrunner.LedgerBoundary || detail.Details.FinalRecords == nil || *detail.Details.FinalRecords != 2 ||
		detail.Details.InitialRecords == nil || *detail.Details.InitialRecords != 0 || detail.Details.Status != string(testrunner.AssertionFailure) {
		t.Fatalf("the baseline's details: %+v", detail.Details)
	}
}

// Two runs compare earlier to later by when each started, whichever is
// chosen first: the check that failed and then passed improved, and each
// part of the configuration is compared on its own.
func TestComparingTwoRunsAlignsTheirChecksAndComparesConfigurationApart(t *testing.T) {
	app, context, _, runs := acceptanceRuns(t)
	compared := app.CompareRunItems(desktop.RunComparisonItemsRequest{Context: context, Runs: []desktop.ItemRef{runs["@post-fix"].Ref, runs["@baseline"].Ref}})
	view := compared.Comparison
	if compared.State != desktop.Completed || view == nil || view.Earlier.Run.ID != runs["@baseline"].Ref.ID || view.Later.Run.ID != runs["@post-fix"].Ref.ID ||
		view.Earlier.Result != desktop.RunFailed || view.Later.Result != desktop.RunPassed || len(view.Repeats) != 0 {
		t.Fatalf("the comparison: %+v", compared)
	}
	if len(view.Checks) != 1 || view.Checks[0].Change != desktop.CheckImproved || view.Checks[0].Earlier != "failed" || view.Checks[0].Later != "passed" ||
		view.Checks[0].EarlierObserved == nil || *view.Checks[0].EarlierObserved != 2 || view.Checks[0].LaterObserved == nil || *view.Checks[0].LaterObserved != 1 {
		t.Fatalf("the aligned checks: %+v", view.Checks)
	}
	parts := map[string]bool{}
	for _, part := range view.Configuration {
		parts[part.Part] = true
	}
	if !parts["input"] || !parts["target"] || !parts["environment"] || !parts["rule"] {
		t.Fatalf("the configuration parts: %+v", view.Configuration)
	}

	for name, request := range map[string][]desktop.ItemRef{
		"one run twice": {runs["@baseline"].Ref, runs["@baseline"].Ref},
		"one run":       {runs["@baseline"].Ref},
	} {
		if refused := app.CompareRunItems(desktop.RunComparisonItemsRequest{Context: context, Runs: request}); refused.State != desktop.Failed || refused.Comparison != nil {
			t.Fatalf("%s: %+v", name, refused)
		}
	}
}

// A check group is decided against a run's evidence as its own labelled
// analysis; the run's result is not changed. A group asking about records
// the run's observation did not collect is refused with what is missing.
func TestAnalyzeRunDecidesACheckGroupWithoutChangingTheRun(t *testing.T) {
	app, context, _, runs := acceptanceRuns(t)
	group := publishCheckGroup(t, context.Project, "", "", "group-1", reviewedSet)
	analyzed := app.AnalyzeRun(desktop.RunAnalysisRequest{Context: context, Run: runs["@post-fix"].Ref, Checks: group})
	if analyzed.State != desktop.Completed || analyzed.Explanation == nil || analyzed.Explanation.Verdict != "pass" || analyzed.Group != "Reschedule accepted" ||
		analyzed.Version != "1" || len(analyzed.Missing) != 0 {
		t.Fatalf("the analysis: %+v", analyzed)
	}
	if after := listed(t, app, context.Project, desktop.RunItem)["@post-fix"].Summary.Run; after.Result != desktop.RunPassed {
		t.Fatalf("the analysis changed the run: %+v", after)
	}
	records := `{"schema": "readmit-assertion-set/v1", "name": "Records after", "assertions": [
  {"id": "one-after", "operator": "record_count", "subject": {"collection": {"scope": "after"}}, "when": null, "expected": {"count": 1}}]}`
	needs := publishCheckGroup(t, context.Project, "", "", "group-2", records)
	missing := app.AnalyzeRun(desktop.RunAnalysisRequest{Context: context, Run: runs["@post-fix"].Ref, Checks: needs})
	if missing.State != desktop.Failed || len(missing.Missing) != 1 || !strings.Contains(missing.Missing[0], "observation") || missing.Explanation != nil {
		t.Fatalf("a group reading records nobody collected: %+v", missing)
	}
}
