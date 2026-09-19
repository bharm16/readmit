package tests

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// guidedTest answers every authoring stage through the facade and saves the
// spec, which is how a person answers them in the window.
func guidedTest(t *testing.T, app *desktop.App, root, output string) {
	t.Helper()
	opened := app.OpenCase(root, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	one := 1
	request := desktop.TestRequest{Workspace: root, Case: guide.CaseName, Identity: opened.Case.Identity}
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &one},
		}},
	} {
		request.Answer = answer
		authored := app.AuthorTest(request)
		if authored.Test == nil {
			t.Fatalf("%s: %+v", answer.Stage, authored)
		}
		request.Draft, request.Answer = authored.Test.Draft, testauthor.Answer{}
	}
	request.Output = output
	if saved := app.SaveTest(request); saved.State != desktop.Completed {
		t.Fatalf("save the authored test: %+v", saved)
	}
}

// The delivery this ticket exists for: an engineer completes the sample without
// typing a command anywhere. Every step below is one facade call — the same
// calls the window's buttons make — and what they leave behind is the real case
// and the real spec, read afterwards by the command line rather than by a second
// reader the window keeps for itself.
func TestTheGuidedSampleIsCompletedThroughTheWindowAlone(t *testing.T) {
	parent := t.TempDir()
	app := desktopApp(t, parent)

	created := app.CreateSampleWorkspace()
	if created.State != desktop.Completed || created.Workspace == nil {
		t.Fatalf("create the sample workspace: %+v", created)
	}
	root := created.Workspace.Root

	guidedTest(t, app, root, "reschedule-test.json")

	baseline := app.RunPractice(desktop.PracticeRequest{
		Workspace: root, Spec: "reschedule-test.json", Trial: guide.StepBaseline, Output: "baseline-run",
	})
	if baseline.State != desktop.Completed || baseline.Practice == nil || baseline.Practice.Status != testrunner.AssertionFailure {
		t.Fatalf("the test did not fail against the fixture's defect: %+v", baseline)
	}
	fixed := app.RunPractice(desktop.PracticeRequest{
		Workspace: root, Spec: "reschedule-test.json", Trial: guide.StepPostFix, Output: "post-fix-run",
	})
	if fixed.State != desktop.Completed || fixed.Practice == nil || fixed.Practice.Status != testrunner.Pass {
		t.Fatalf("the same test did not pass against the corrected fixture: %+v", fixed)
	}

	// Closing the window and opening the folder again reports the same thing,
	// because nothing about where somebody is was ever written down.
	reopened := desktopApp(t, parent)
	progress := reopened.Guide(root)
	if progress.State != desktop.Completed || progress.Guide == nil || !progress.Guide.Complete() {
		t.Fatalf("a reopened folder did not report the completed guided sample: %+v", progress)
	}
	if progress.Guide.Spec != "reschedule-test.json" || progress.Guide.Case != guide.CaseName {
		t.Fatalf("the reopened guided sample named other work: %+v", progress.Guide)
	}

	// The workspace holds the work and nothing else. There is no progress
	// document, no tutorial state and no file the window keeps for itself.
	listed, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed))
	for _, entry := range listed {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	expected := []string{
		"baseline-run", "cancellation", "invalid", guide.TargetName, guide.IndexName,
		"post-fix-run", guide.CaseName, "reschedule-test.json",
	}
	slices.Sort(expected)
	if !slices.Equal(names, expected) {
		t.Fatalf("the completed workspace holds %v rather than %v", names, expected)
	}

	// The spec is a document `readmit test` prepares unchanged: what the window
	// saved is a regression test, not a shape only the window understands.
	stdout, stderr, err := run(t, "test", filepath.Join(root, "reschedule-test.json"))
	if err != nil || stderr != "" {
		t.Fatalf("readmit test: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Local validation: no connection opened, no verdict or result artifact produced",
		"Observation boundary: appointment-ledger",
		"Messages: 2",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("the command line disagrees about the saved spec (%q):\n%s", want, stdout)
		}
	}

	// Each practice run really sent over a socket: the case the practice
	// receiver recorded is ordinary evidence the command line verifies.
	for _, entry := range []string{"baseline-run", "post-fix-run"} {
		stdout, stderr, err := run(t, "timeline", filepath.Join(root, entry, "receiver"))
		if err != nil || stderr != "" {
			t.Fatalf("timeline %s: %v %s", entry, err, stderr)
		}
		if !strings.Contains(stdout, "Messages: 2") {
			t.Fatalf("the practice receiver of %s did not record the two sent messages:\n%s", entry, stdout)
		}
	}

	// And each retained result is read back by the reader the command line uses,
	// which re-derives the verdict from the run and observation evidence.
	for entry, want := range map[string]testrunner.Status{
		"baseline-run": testrunner.AssertionFailure,
		"post-fix-run": testrunner.Pass,
	} {
		artifact, err := testrunner.Open(filepath.Join(root, entry, "result"))
		if err != nil {
			t.Fatalf("reopen %s: %v", entry, err)
		}
		if artifact.Result.Status != want {
			t.Fatalf("%s reopened as %s rather than %s", entry, artifact.Result.Status, want)
		}
		if artifact.Result.InputBundleIdentity != frozenRegressionIdentity {
			t.Fatalf("%s was not decided over the frozen sample evidence: %s", entry, artifact.Result.InputBundleIdentity)
		}
	}
}
