package desktop

import (
	"testing"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A run's one result ranks its lifecycle above its verdict: a journal that
// never recorded its end is interrupted and a stopped run incomplete whatever
// their checks decided, and an unsettled delivery beside a passing verdict
// is never shown as passed.
func TestARunsResultRanksLifecycleAboveItsVerdict(t *testing.T) {
	result := func(status testrunner.Status) *testrunner.Artifact {
		return &testrunner.Artifact{Result: testrunner.Result{Status: status}}
	}
	for name, test := range map[string]struct {
		opened *runresult.Result
		want   RunResult
	}{
		"a pass":                      {&runresult.Result{Artifact: result(testrunner.Pass)}, RunPassed},
		"an assertion failure":        {&runresult.Result{Artifact: result(testrunner.AssertionFailure)}, RunFailed},
		"an execution error":          {&runresult.Result{Artifact: result(testrunner.ExecutionError)}, RunErrored},
		"a durable pass":              {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.Passed, ResultIdentity: "r"}, Artifact: result(testrunner.Pass)}, RunPassed},
		"an interrupted journal":      {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.Interrupted}}, RunInterrupted},
		"a recovered pass":            {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.Passed, Recovered: true, ResultIdentity: "r"}, Artifact: result(testrunner.Pass)}, RunInterrupted},
		"a running journal":           {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.Running}}, RunInterrupted},
		"an incomplete journal":       {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.Passed, JournalIncomplete: true, ResultIdentity: "r"}, Artifact: result(testrunner.Pass)}, RunIncomplete},
		"a stopped run":               {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.Cancelled}}, RunIncomplete},
		"a timed out run":             {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.TimedOut}}, RunIncomplete},
		"an uncertain pass":           {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.Passed, DeliveryUncertain: true, ResultIdentity: "r"}, Artifact: result(testrunner.Pass)}, RunIncomplete},
		"an uncertain failure":        {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.AssertionFailed, DeliveryUncertain: true, ResultIdentity: "r"}, Artifact: result(testrunner.AssertionFailure)}, RunIncomplete},
		"a stopped error":             {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.ExecutionError, StopReason: durablerun.Cancelled, ResultIdentity: "r"}, Artifact: result(testrunner.ExecutionError)}, RunIncomplete},
		"a result without a verdict":  {&runresult.Result{}, RunIncomplete},
		"a delivery-uncertain ending": {&runresult.Result{Durable: true, Lifecycle: durablerun.Summary{State: durablerun.DeliveryUncertain, ResultIdentity: "r"}, Artifact: result(testrunner.Pass)}, RunIncomplete},
	} {
		if got := testRunResult(test.opened); got != test.want {
			t.Errorf("%s: %s, want %s", name, got, test.want)
		}
	}
	if rank(RunInterrupted) >= rank(RunPassed) || rank(RunBlocked) >= rank(RunErrored) || rank(RunErrored) >= rank(RunFailed) {
		t.Fatal("the results are not ranked by priority")
	}
}

// A check whose definition changed is a changed check, never a regression
// or an improvement, and a check either run did not evaluate is not
// compared.
func TestAChangedCheckDefinitionIsNeverARegression(t *testing.T) {
	for name, test := range map[string]struct {
		row  runcompare.AssertionComparison
		want CheckChange
	}{
		"failed then passed":         {runcompare.AssertionComparison{Definition: "unchanged", Baseline: "failed", Current: "passed"}, CheckImproved},
		"passed then failed":         {runcompare.AssertionComparison{Definition: "unchanged", Baseline: "passed", Current: "failed"}, CheckRegressed},
		"passed with a new value":    {runcompare.AssertionComparison{Definition: "unchanged", Baseline: "failed", Current: "failed", Behavior: "changed"}, CheckObservedChanged},
		"the same":                   {runcompare.AssertionComparison{Definition: "unchanged", Baseline: "passed", Current: "passed", Behavior: "unchanged"}, CheckUnchanged},
		"a changed definition":       {runcompare.AssertionComparison{Definition: "changed", Baseline: "passed", Current: "failed"}, CheckDefinitionChange},
		"a changed definition fixed": {runcompare.AssertionComparison{Definition: "changed", Baseline: "failed", Current: "passed"}, CheckDefinitionChange},
		"added":                      {runcompare.AssertionComparison{Definition: "added", Baseline: "excluded", Current: "passed"}, CheckAdded},
		"removed":                    {runcompare.AssertionComparison{Definition: "removed", Baseline: "failed", Current: "excluded"}, CheckRemoved},
		"not evaluated":              {runcompare.AssertionComparison{Definition: "unchanged", Baseline: "not_evaluated", Current: "passed"}, CheckNotCompared},
		"unknown":                    {runcompare.AssertionComparison{Definition: "unknown", Baseline: "unknown", Current: "passed"}, CheckNotCompared},
	} {
		if got := checkChange(test.row); got != test.want {
			t.Errorf("%s: %s, want %s", name, got, test.want)
		}
	}
}

// A check with no observed value reads as unavailable with its reason,
// never as zero; a count of zero stays a count.
func TestAMissingObservationIsUnavailableNeverZero(t *testing.T) {
	zero := 0
	declared := testrunner.Assertion{ID: "none", Operator: "ledger_count", Expected: testrunner.Value{Count: &zero}}
	artifact := &testrunner.Artifact{Result: testrunner.Result{Status: testrunner.AssertionFailure}}
	if reason := unavailable(declared, artifact, map[string]bool{}); reason == "" {
		t.Fatal("a missing observation has no reason")
	}
	check := RunCheck{Check: expectationOf(declared), Observed: &testrunner.Value{Count: &zero}}
	hide(&check, false)
	if check.Observed == nil || check.Observed.Count == nil || *check.Observed.Count != 0 || check.Hidden {
		t.Fatalf("an observed zero: %+v", check)
	}
}
