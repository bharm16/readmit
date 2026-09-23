package receiver_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// practiceWorkspace writes the guided sample's workspace and saves its
// regression test into it, as the window does, and answers the workspace and
// the saved spec's name.
func practiceWorkspace(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "sample")
	inputs := bundle.GeneratorInputs{Seed: 0, BaseTime: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), GeneratorVersion: synth.GeneratorVersion, ProfileVersion: synth.ProfileVersion}
	if _, err := synth.Write(root, inputs); err != nil {
		t.Fatal(err)
	}
	if err := guide.PrepareWorkspace(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	source, err := bundle.Open(filepath.Join(root, guide.CaseName))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := testauthor.NewDraft(guide.CaseName, source.Identity)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &one}}},
	} {
		if draft, err = draft.Answer(answer); err != nil {
			t.Fatalf("%s: %v", answer.Stage, err)
		}
	}
	saved, err := testauthor.Save(root, source, draft, "reschedule-test.json")
	if err != nil {
		t.Fatal(err)
	}
	return root, saved.Output
}

// A report trial and a practice run of the guided sample also send to this
// fixture in their own process, each under a five-second message timeout that
// the target it retains records (#350). A durable fixture flushes its ledger
// twice per message before the ACK, so flushes stalled by three seconds alone
// run out that window, as the durable control of
// TestInProcessLedgerAnswersWithoutWaitingForTheDisk shows at a smaller scale.
// Both fixtures install their ledger without flushing it, so both operations
// still complete with the verdicts an unstalled disk gives them.
func TestReportAndPracticeRunDoNotWaitForStalledLedgerFlushes(t *testing.T) {
	restore := receiver.DelayLedgerSyncForTest(3 * time.Second)
	defer restore()
	t.Run("report", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "packet")
		packet, err := report.Create(t.Context(), report.Scenario, dir)
		if err != nil {
			t.Fatal(err)
		}
		// Open re-derives the identity from every retained file and holds each
		// retained target to the fixture's timeouts.
		if opened, err := report.Open(dir); err != nil || opened.Identity != packet.Identity {
			t.Fatalf("the packet did not reopen as written: %v", err)
		}
		baseline, err := testrunner.Open(filepath.Join(dir, "baseline"))
		if err != nil {
			t.Fatal(err)
		}
		fixed, err := testrunner.Open(filepath.Join(dir, "post-fix"))
		if err != nil {
			t.Fatal(err)
		}
		if baseline.Result.Status != testrunner.AssertionFailure || len(baseline.FinalObservation.Records) != 2 || fixed.Result.Status != testrunner.Pass || len(fixed.FinalObservation.Records) != 1 {
			t.Fatalf("stalled flushes changed the report's verdicts: %s, %s", baseline.Result.Status, fixed.Result.Status)
		}
	})
	t.Run("practice-run", func(t *testing.T) {
		root, spec := practiceWorkspace(t)
		for _, trial := range []struct {
			output    string
			corrected bool
			status    testrunner.Status
		}{{"baseline-run", false, testrunner.AssertionFailure}, {"post-fix-run", true, testrunner.Pass}} {
			artifact, err := guide.Run(t.Context(), root, spec, trial.output, trial.corrected)
			if err != nil {
				t.Fatal(err)
			}
			if artifact.Result.Status != trial.status {
				t.Fatalf("stalled flushes changed the %s verdict to %s", trial.output, artifact.Result.Status)
			}
			if reopened, err := testrunner.Open(filepath.Join(root, trial.output, "result")); err != nil || reopened.Identity != artifact.Identity {
				t.Fatalf("the %s result did not reopen as written: %v", trial.output, err)
			}
		}
	})
}
