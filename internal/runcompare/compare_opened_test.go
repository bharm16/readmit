package runcompare_test

import (
	"context"
	"testing"

	"github.com/bharm16/readmit/internal/drift"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The projection over opened evidence is pure: these tests fabricate opened
// results in memory instead of running durable jobs against a loopback peer.
// A fabricated result carries no run, so nothing is observed; the specs,
// pins, targets and assertion outcomes are the comparison's whole input.
func openedSpec() *testrunner.Spec {
	return &testrunner.Spec{
		Schema: testrunner.SpecSchema, Name: "synthetic",
		Input:      testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}},
		Target:     "target.json",
		Setup:      testrunner.Setup{InitialState: testrunner.OperatorDeclared, ResetInstructions: "reset synthetic peer"},
		Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1"}},
	}
}

func openedTarget() *replay.TargetRecord {
	return &replay.TargetRecord{Address: "127.0.0.1:2575", Transport: "plain", TestEndpoint: true, ConnectTimeout: "2s", MessageTimeout: "2s", MaxACKBytes: 4096}
}

func openedPin() *runresult.Pin {
	document := engine.Current(testrunner.SpecSchema)
	return &runresult.Pin{Digest: "fabricated-pin", Document: &document}
}

func openedResult(id string, state durablerun.State, status testrunner.Status, spec *testrunner.Spec, assertions []testrunner.AssertionResult) *runresult.Result {
	return &runresult.Result{
		Path:      "fabricated-" + id,
		Durable:   true,
		Lifecycle: durablerun.Summary{State: state, StopReason: state, ResultIdentity: id},
		Pin:       openedPin(),
		Artifact: &testrunner.Artifact{
			Identity: id,
			Result: testrunner.Result{
				Status: status, InputBundleIdentity: "case-1",
				Target: openedTarget(), ReceiverMode: observation.Defective,
				Assertions: assertions,
			},
			Spec: spec,
			Run:  &replay.Run{Manifest: replay.Manifest{}},
		},
		Spec:       spec,
		Assertions: assertions,
	}
}

func acceptedAssertion(status string) testrunner.AssertionResult {
	return testrunner.AssertionResult{
		Assertion: testrunner.Assertion{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1"},
		Status:    status,
	}
}

func TestCompareOpenedProjectsViewsStabilityAndMatches(t *testing.T) {
	spec := openedSpec()
	baseline := openedResult("baseline-1", durablerun.Passed, testrunner.Pass, spec, []testrunner.AssertionResult{acceptedAssertion(testrunner.Passed)})
	current := openedResult("current-1", durablerun.AssertionFailed, testrunner.AssertionFailure, spec, []testrunner.AssertionResult{acceptedAssertion(testrunner.Failed)})
	got, err := runcompare.CompareOpened(context.Background(), runcompare.OpenedInput{Baseline: baseline, Current: current})
	if err != nil {
		t.Fatal(err)
	}
	if got.Baseline.RunState != "passed" || got.Baseline.Status != "pass" || got.Baseline.Identity != "baseline-1" {
		t.Fatalf("baseline view is not the opened execution: %+v", got.Baseline)
	}
	if got.Current.RunState != "assertion_failed" || got.Current.Status != "assertion_failure" || got.Current.Identity != "current-1" {
		t.Fatalf("current view is not the opened execution: %+v", got.Current)
	}
	if state, ok := got.Baseline.DurableState(); !ok || state != durablerun.Passed {
		t.Fatalf("baseline typed state is %v, %t", state, ok)
	}
	if status, ok := got.Current.ResultStatus(); !ok || status != testrunner.AssertionFailure {
		t.Fatalf("current typed verdict is %v, %t", status, ok)
	}
	if got.Baseline.Planned != 1 || got.Baseline.Unobserved != 1 || len(got.Baseline.Gaps) != 1 {
		t.Fatalf("unobserved selection is not a stated gap: %+v", got.Baseline)
	}
	if got.Specification != "unchanged" {
		t.Fatalf("identical specs read %q", got.Specification)
	}
	if got.Drift.Attribution.Outcome != drift.NoDeclaredChange {
		t.Fatalf("identical evidence drifted: %+v", got.Drift.Attribution)
	}
	if len(got.Assertions) != 1 || got.Assertions[0].Definition != "unchanged" || got.Assertions[0].Behavior != "changed" {
		t.Fatalf("unchanged definition with moved outcome is not compared: %+v", got.Assertions)
	}
	if got.Stability.State != "possible_flakiness" || len(got.Stability.FlakyAssertions) != 1 || got.Stability.FlakyAssertions[0] != "accepted" || got.Stability.Passes != 1 || got.Stability.Failures != 1 {
		t.Fatalf("moved verdict is not a flakiness signal: %+v", got.Stability)
	}
}

func TestCompareOpenedComparesChangedAndUnevaluatedDefinitions(t *testing.T) {
	changed := acceptedAssertion(testrunner.Passed)
	changed.Assertion.Expected = testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present}}
	pending := testrunner.AssertionResult{
		Assertion: testrunner.Assertion{ID: "pending", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1"},
		Status:    testrunner.NotEvaluated,
	}
	baseline := openedResult("baseline-1", durablerun.Passed, testrunner.Pass, openedSpec(), []testrunner.AssertionResult{
		acceptedAssertion(testrunner.Passed),
		{Assertion: testrunner.Assertion{ID: "removed-only", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1"}, Status: testrunner.Passed},
		pending,
	})
	spec := openedSpec()
	spec.Name = "renamed"
	current := openedResult("current-1", durablerun.Passed, testrunner.Pass, spec, []testrunner.AssertionResult{
		changed,
		{Assertion: testrunner.Assertion{ID: "added-only", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1"}, Status: testrunner.Passed},
		pending,
	})
	got, err := runcompare.CompareOpened(context.Background(), runcompare.OpenedInput{Baseline: baseline, Current: current})
	if err != nil {
		t.Fatal(err)
	}
	if got.Specification != "changed" {
		t.Fatalf("renamed spec read %q", got.Specification)
	}
	rows := map[string]runcompare.AssertionComparison{}
	for _, row := range got.Assertions {
		rows[row.ID] = row
	}
	if rows["accepted"].Definition != "changed" || rows["accepted"].Behavior != "not_compared" {
		t.Errorf("redefined assertion is not held uncompared: %+v", rows["accepted"])
	}
	if rows["removed-only"].Definition != "removed" || rows["removed-only"].Current != "excluded" {
		t.Errorf("removed assertion is not stated: %+v", rows["removed-only"])
	}
	if rows["added-only"].Definition != "added" || rows["added-only"].Baseline != "excluded" {
		t.Errorf("added assertion is not stated: %+v", rows["added-only"])
	}
	if rows["pending"].Definition != "unchanged" || rows["pending"].Behavior != "not_compared" {
		t.Errorf("unevaluated assertion is not held uncompared: %+v", rows["pending"])
	}
	if got.Current.Unevaluated != 1 {
		t.Errorf("unevaluated assertions counted %d", got.Current.Unevaluated)
	}
	if got.Stability.State != "unresolved" {
		t.Errorf("runs under different specs are %q, not unresolved", got.Stability.State)
	}
}

func TestCompareOpenedReadsTypedStatesAndRefusesAbsences(t *testing.T) {
	direct := openedResult("direct-1", durablerun.Passed, testrunner.Pass, openedSpec(), nil)
	direct.Durable = false
	direct.Lifecycle = durablerun.Summary{}
	got, err := runcompare.CompareOpened(context.Background(), runcompare.OpenedInput{Baseline: direct, Current: direct})
	if err != nil {
		t.Fatal(err)
	}
	if got.Baseline.RunState != runresult.NoRunState {
		t.Fatalf("direct result run state is %q", got.Baseline.RunState)
	}
	if _, ok := got.Baseline.DurableState(); ok {
		t.Fatal("direct result reports a durable state")
	}
	if status, ok := got.Baseline.ResultStatus(); !ok || status != testrunner.Pass {
		t.Fatalf("direct result typed verdict is %v, %t", status, ok)
	}

	unfinished := openedResult("unfinished-1", durablerun.Running, testrunner.Pass, openedSpec(), nil)
	unfinished.Artifact = nil
	unfinished.Lifecycle.ResultIdentity = ""
	got, err = runcompare.CompareOpened(context.Background(), runcompare.OpenedInput{Baseline: unfinished, Current: unfinished})
	if err != nil {
		t.Fatal(err)
	}
	if got.Baseline.Status != "unknown" || got.Baseline.Identity != "" {
		t.Fatalf("unfinished run is not stated as unknown: %+v", got.Baseline)
	}
	if _, ok := got.Baseline.ResultStatus(); ok {
		t.Fatal("unfinished run reports a verdict")
	}
	if state, ok := got.Baseline.DurableState(); !ok || state != durablerun.Running {
		t.Fatalf("unfinished run typed state is %v, %t", state, ok)
	}
	if got.Stability.State != "insufficient_history" || got.Stability.Incomplete != 1 {
		t.Fatalf("unfinished history is not counted incomplete: %+v", got.Stability)
	}

	if _, err := runcompare.CompareOpened(context.Background(), runcompare.OpenedInput{Baseline: direct}); err == nil {
		t.Fatal("missing current execution compared")
	}
	repeats := make([]*runresult.Result, runcompare.MaxRepeats+1)
	if _, err := runcompare.CompareOpened(context.Background(), runcompare.OpenedInput{Baseline: direct, Current: direct, Repeats: repeats}); err == nil {
		t.Fatal("unbounded history compared")
	}
}
