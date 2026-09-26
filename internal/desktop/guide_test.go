package desktop_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// guided is the sample workspace, created the way the window creates it.
func guided(t *testing.T) (*desktop.App, string) {
	t.Helper()
	app := newApp(t, &chooser{folder: t.TempDir()})
	return app, sample(t, app).Workspace.Root
}

// authorGuidedTest walks the authoring stages the window walks and saves the
// spec, so what the practice run below executes is a test this facade authored
// rather than one the test wrote by hand.
func authorGuidedTest(t *testing.T, app *desktop.App, root, output string, appointments int) string {
	t.Helper()
	opened := app.OpenCase(root, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	request := desktop.TestRequest{Workspace: root, Case: guide.CaseName, Identity: opened.Case.Identity}
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &appointments},
		}},
	} {
		request.Answer = answer
		authored := app.AuthorTest(request)
		if authored.State != desktop.Completed && authored.State != desktop.Empty {
			t.Fatalf("%s: %+v", answer.Stage, authored)
		}
		if authored.Test == nil {
			t.Fatalf("%s reported no draft: %+v", answer.Stage, authored)
		}
		request.Draft, request.Answer = authored.Test.Draft, testauthor.Answer{}
	}
	request.Output = output
	saved := app.SaveTest(request)
	if saved.State != desktop.Completed || saved.Test == nil || saved.Test.Output != output {
		t.Fatalf("save the authored test: %+v", saved)
	}
	return output
}

func guideOf(t *testing.T, app *desktop.App, root string) desktop.GuideResult {
	t.Helper()
	result := app.Guide(root)
	if result.Guide == nil {
		t.Fatalf("the guided sample reported no steps: %+v", result)
	}
	return result
}

// The window keeps no tutorial state, so the guided sample is read back out of
// the folder every time it is asked for. A folder nobody has started reports the
// path with nothing done rather than a failure.
func TestTheGuidedSampleIsReadBackFromTheOpenWorkspace(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	plain := t.TempDir()
	untouched := guideOf(t, app, plain)
	if untouched.State != desktop.Empty || untouched.Reason == "" || untouched.Guide.Next != guide.StepSample {
		t.Fatalf("an unrelated folder reported progress: %+v", untouched)
	}

	root := sample(t, app).Workspace.Root
	created := guideOf(t, app, root)
	if created.State != desktop.Completed || created.Guide.Next != guide.StepTest {
		t.Fatalf("the sample workspace did not complete its own step: %+v", created)
	}
	if created.Guide.Case != guide.CaseName || created.Guide.Identity == "" {
		t.Fatalf("the guided sample did not name the evidence it is bound to: %+v", created.Guide)
	}

	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)
	if authored := guideOf(t, app, root); authored.Guide.Spec != spec || authored.Guide.Next != guide.StepBaseline {
		t.Fatalf("the saved test did not complete the authoring step: %+v", authored.Guide)
	}
	if missing := app.Guide(filepath.Join(root, "absent")); missing.State != desktop.Failed || missing.Guide != nil {
		t.Fatalf("a folder that is not there reported a guided sample: %+v", missing)
	}
}

// The whole point of the guided sample: the same authored test fails against the
// fixture's defect and passes once that defect is corrected, and both runs are
// evidence on disk rather than a claim the window makes.
func TestAPracticeRunFailsAgainstTheDefectAndPassesOnceItIsCorrected(t *testing.T) {
	app, root := guided(t)
	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)

	baseline := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run"})
	if baseline.State != desktop.Completed || baseline.Practice == nil {
		t.Fatalf("the baseline practice run: %+v", baseline)
	}
	if baseline.Practice.Status != testrunner.AssertionFailure {
		t.Fatalf("the test did not fail against the defect: %s", baseline.Practice.Status)
	}
	// The failure is reported as the expectation that was not met, and never as
	// the value that was read: displaying evidence is what the inspector is for.
	failed := false
	for _, evaluated := range baseline.Practice.Assertions {
		if evaluated.ID == "" || evaluated.Operator == "" || evaluated.Status == "" {
			t.Fatalf("an expectation was reported without naming itself: %+v", evaluated)
		}
		if evaluated.ID == "one-appointment" && evaluated.Status == "failed" {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("the failing run did not name the expectation it failed: %+v", baseline.Practice.Assertions)
	}
	// What a practice run rebinds is stated rather than implied.
	if len(baseline.Practice.ChangedBindings) != 3 {
		t.Fatalf("the practice run did not state what it rebound: %+v", baseline.Practice.ChangedBindings)
	}

	fixed := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepPostFix, Output: "post-fix-run"})
	if fixed.State != desktop.Completed || fixed.Practice == nil || fixed.Practice.Status != testrunner.Pass {
		t.Fatalf("the same spec did not pass against the corrected fixture: %+v", fixed)
	}
	if fixed.Practice.SpecIdentity != baseline.Practice.SpecIdentity {
		t.Fatal("the two trials executed different specs")
	}
	if complete := guideOf(t, app, root); complete.Guide.Next != "" {
		t.Fatalf("the guided sample did not complete: %+v", complete.Guide)
	}
	// Reopening the folder in a new window reports the same thing, because the
	// window remembered none of it.
	reopened := newApp(t, &chooser{folder: root})
	if again := guideOf(t, reopened, root); again.Guide.Next != "" || again.Guide.Spec != spec {
		t.Fatalf("a new window disagreed about the same folder: %+v", again.Guide)
	}
}

// Every refusal names one state, changes nothing, and releases the slot.
func TestAPracticeRunRefusesWhatItCannotExecute(t *testing.T) {
	app, root := guided(t)
	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)

	for _, refused := range []struct {
		name    string
		request desktop.PracticeRequest
		state   desktop.State
	}{
		{"a trial the guided sample does not declare",
			desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: "reintroduced", Output: "run"}, desktop.Failed},
		{"no trial at all",
			desktop.PracticeRequest{Workspace: root, Spec: spec, Output: "run"}, desktop.Failed},
		{"a workspace that is not a folder",
			desktop.PracticeRequest{Workspace: filepath.Join(root, spec), Spec: spec, Trial: guide.StepBaseline, Output: "run"}, desktop.Failed},
		{"an entry that is not a test spec",
			desktop.PracticeRequest{Workspace: root, Spec: guide.TargetName, Trial: guide.StepBaseline, Output: "run"}, desktop.Failed},
		{"an output that is not one new entry",
			desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "runs/one"}, desktop.Failed},
		{"an output that already holds evidence",
			desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: guide.CaseName}, desktop.Failed},
	} {
		t.Run(refused.name, func(t *testing.T) {
			result := app.RunPractice(refused.request)
			if result.State != refused.state || result.Practice != nil || result.Reason == "" {
				t.Fatalf("the practice run reported %+v", result)
			}
		})
	}
	// Recovery: every refusal above released the slot and changed nothing.
	if progress := guideOf(t, app, root); progress.Guide.Next != guide.StepBaseline {
		t.Fatalf("a refused practice run changed the workspace: %+v", progress.Guide)
	}
	recovered := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run"})
	if recovered.State != desktop.Completed {
		t.Fatalf("a refused practice run left the facade unusable: %+v", recovered)
	}
}

// Both operations take the one operation slot, so neither races another and
// neither is started while something else is running.
func TestTheGuidedSampleTakesTheSameOperationSlot(t *testing.T) {
	app, root := guided(t)
	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)

	reentrant := &chooser{folder: root}
	second := desktop.New(reentrant, desktop.ShellDocuments{Folder: t.TempDir()})
	var progress desktop.GuideResult
	var practice desktop.PracticeResult
	reentrant.before = func() {
		progress = second.Guide(root)
		practice = second.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run"})
	}
	if opened := second.SelectWorkspace(); opened.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", opened)
	}
	if progress.State != desktop.Busy || progress.Guide != nil {
		t.Fatalf("the guided sample was read while another operation held the facade: %+v", progress)
	}
	if practice.State != desktop.Busy || practice.Practice != nil {
		t.Fatalf("a practice run started while another operation held the facade: %+v", practice)
	}
	if recovered := second.Guide(root); recovered.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", recovered)
	}
}

// receiverFixtures copies the two frozen receiver fixtures into a new folder,
// byte for byte, as a person copies them out of a release archive.
func receiverFixtures(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	for _, name := range []string{"listen-s12.hl7", "listen-s13.hl7"} {
		writeDocument(t, folder, name, fixture(t, name))
	}
	return folder
}

// The guided sample imports the frozen receiver fixtures as one imported case
// without activation, through the shared operation the command line runs, and
// verifies what it wrote through the reader every case is opened with. What
// it reports is that verified case, and the folder the fixtures came from is
// left exactly as it was.
func TestTheGuidedSampleImportsTheFrozenReceiverFixtures(t *testing.T) {
	fixtures := receiverFixtures(t)
	root := t.TempDir()
	host := &chooser{folder: fixtures}
	unlicensed := desktop.New(host, desktop.ShellDocuments{Folder: t.TempDir()})
	before := bytesUnder(t, fixtures)
	result := unlicensed.CaptureSample(desktop.SampleCaptureRequest{Workspace: root, Output: "receiver-sample"})
	if result.State != desktop.Completed || result.Case == nil {
		t.Fatalf("the sample capture: %+v", result)
	}
	if got := result.Case; got.Name != "receiver-sample" || got.Provenance != "imported" || got.Schema != "readmit-case/v1" ||
		got.Sources != 2 || got.Occurrences != 2 || got.Messages != 2 || got.Acknowledgements != 0 || got.Unparsed != 0 {
		t.Fatalf("the sample case is not the booking and its reschedule: %+v", got)
	}
	if opened := unlicensed.OpenCase(root, "receiver-sample"); opened.State != desktop.Completed || opened.Case.Identity != result.Case.Identity {
		t.Fatalf("the reported case is not the one the reader verifies: %+v", opened)
	}
	if !reflect.DeepEqual(host.titles, []string{"Choose the folder holding the frozen receiver fixtures"}) {
		t.Fatalf("the fixtures folder was chosen through %v", host.titles)
	}
	if after := bytesUnder(t, fixtures); !reflect.DeepEqual(before, after) {
		t.Fatal("importing the fixtures changed the folder they were read from")
	}
}

// Every refusal leaves the workspace as it was: a dismissed dialog, bytes
// other than the frozen fixtures, a fixture that is not there, a name that is
// not one new entry, and an entry that already exists. The fixture refusals
// are the command's own words.
func TestTheSampleCaptureRefusesAndWritesNothing(t *testing.T) {
	changed := receiverFixtures(t)
	writeDocument(t, changed, "listen-s12.hl7", "MSH|^~\\&|CHANGED\r")
	absent := t.TempDir()
	for name, test := range map[string]struct {
		folder, output string
		state          desktop.State
		reason         string
	}{
		"dismissed dialog": {"", "receiver-sample", desktop.Cancelled, "no folder was chosen"},
		"changed bytes":    {changed, "receiver-sample", desktop.Failed, "sample requires the unchanged frozen synthetic fixtures"},
		"missing fixtures": {absent, "receiver-sample", desktop.Failed, "frozen sample fixture unavailable"},
		"a path":           {receiverFixtures(t), "nested/receiver-sample", desktop.Failed, "the sample case needs one new folder name in the open workspace, never a path"},
		"no name":          {receiverFixtures(t), "", desktop.Failed, "the sample case needs one new folder name in the open workspace, never a path"},
		"existing entry":   {receiverFixtures(t), "taken", desktop.Failed, "cannot create bundle; destination must be new and parent readable and writable"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeDocument(t, root, "taken", "someone else's file")
			before := bytesUnder(t, root)
			app := newApp(t, &chooser{folder: test.folder})
			result := app.CaptureSample(desktop.SampleCaptureRequest{Workspace: root, Output: test.output})
			if result.State != test.state || result.Reason != test.reason || result.Case != nil {
				t.Fatalf("got %+v, want %s %q", result, test.state, test.reason)
			}
			if after := bytesUnder(t, root); !reflect.DeepEqual(before, after) {
				t.Fatalf("a refused sample capture changed the workspace: %v", after)
			}
		})
	}
}
