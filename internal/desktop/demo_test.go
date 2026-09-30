package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/testrunner"
)

func openDemo(t *testing.T, app *desktop.App) desktop.ProjectOpenResult {
	t.Helper()
	opened := app.OpenDemoProject()
	if opened.State != desktop.Completed || opened.Project == nil {
		t.Fatalf("OpenDemoProject: %+v", opened)
	}
	return opened
}

func demoOf(t *testing.T, app *desktop.App, context desktop.RequestContext) desktop.DemoProgress {
	t.Helper()
	progress := app.DemoProgress(context)
	if progress.State != desktop.Completed || progress.Demo == nil {
		t.Fatalf("DemoProgress: %+v", progress)
	}
	return *progress.Demo
}

// done names the steps a progress reports done.
func done(progress desktop.DemoProgress) []string {
	finished := []string{}
	for _, step := range progress.Steps {
		if step.Done {
			finished = append(finished, string(step.ID))
		}
	}
	return finished
}

// The demo is a named project of the frozen synthetic sample, created in the
// application's own folder and recorded there. Opening it again opens the
// same project and writes nothing into it, and a folder at its place that is
// not the demo project is refused and left exactly as it is.
func TestTheDemoProjectIsCreatedInManagedStorageAndNeverOverwritten(t *testing.T) {
	state := t.TempDir()
	app := activatedApp(t, &chooser{}, state)
	opened := openDemo(t, app)
	root := opened.Context.Project
	if root != filepath.Join(resolved(t, state), "demo", "readmit-sample") || !opened.Recorded || opened.Context.ProjectID == "" || opened.Project.Name != desktop.DemoTitle {
		t.Fatalf("the demo project: %+v", opened)
	}
	before := bytesUnder(t, root)
	again := openDemo(t, app)
	if again.Context != opened.Context || !reflect.DeepEqual(bytesUnder(t, root), before) {
		t.Fatalf("opening the demo again changed it: %+v", again)
	}
	if reopened := openDemo(t, activatedApp(t, &chooser{}, state)); reopened.Context != opened.Context {
		t.Fatalf("a new window opened another demo: %+v", reopened)
	}

	occupied := t.TempDir()
	place := filepath.Join(occupied, "demo", "readmit-sample")
	if err := os.MkdirAll(place, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, place, "notes.txt", "mine\n")
	held := bytesUnder(t, place)
	refused := activatedApp(t, &chooser{}, occupied).OpenDemoProject()
	if refused.State != desktop.Failed || refused.Project != nil || !reflect.DeepEqual(bytesUnder(t, place), held) {
		t.Fatalf("a folder at the demo's place: %+v", refused)
	}
}

// The demo is finite synthetic work against the loopback practice receiver,
// under the frozen-sample exemption: a window with no activation creates,
// records and opens it, reads its progress and runs its practice.
func TestTheDemoStaysLoopbackAndNeedsNoActivation(t *testing.T) {
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()})
	opened := openDemo(t, app)
	if !opened.Recorded {
		t.Fatalf("the unactivated demo was not recorded: %+v", opened)
	}
	progress := demoOf(t, app, opened.Context)
	if progress.Target != guide.TargetName || progress.Identity != guide.CaseIdentity || guide.Target().Address != "127.0.0.1:2575" || !guide.Target().TestEndpoint {
		t.Fatalf("the demo's endpoint: %+v %+v", progress, guide.Target())
	}
	spec := authorGuidedTest(t, app, opened.Context.Project, "reschedule-test.json", 1)
	run := app.RunPractice(desktop.PracticeRequest{Workspace: opened.Context.Project, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run"})
	if run.State != desktop.Completed || run.Practice.Status != testrunner.AssertionFailure {
		t.Fatalf("an unactivated practice run: %+v", run)
	}
}

// Every step the project can show is done only once the result it names is
// retained, and the three steps that are reads are never done here: the
// window marks them from the successful result of the call they name.
func TestDemoStepsCompleteOnlyFromActualResults(t *testing.T) {
	app := activatedApp(t, &chooser{}, t.TempDir())
	opened := openDemo(t, app)
	root := opened.Context.Project
	fresh := demoOf(t, app, opened.Context)
	var ids, sessions []string
	for _, step := range fresh.Steps {
		ids = append(ids, string(step.ID))
		if step.Evidence == desktop.DemoSessionEvidence {
			sessions = append(sessions, string(step.ID))
		}
	}
	if !reflect.DeepEqual(ids, []string{"open-messages", "create-test", "run-defective", "view-failed-check", "run-fixed", "compare"}) ||
		!reflect.DeepEqual(sessions, []string{"open-messages", "view-failed-check", "compare"}) || len(done(fresh)) != 0 {
		t.Fatalf("a fresh demo: %+v", fresh)
	}
	if opened := app.OpenCase(root, guide.CaseName); opened.State != desktop.Completed {
		t.Fatal(opened)
	}
	if after := demoOf(t, app, opened.Context); len(done(after)) != 0 {
		t.Fatalf("opening the messages completed %v", done(after))
	}
	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)
	if authored := demoOf(t, app, opened.Context); !reflect.DeepEqual(done(authored), []string{"create-test"}) || authored.Spec != spec {
		t.Fatalf("the saved test: %+v", authored)
	}
	cancelled := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: "somewhere-else", Output: "refused-run"})
	if cancelled.State != desktop.Failed {
		t.Fatal(cancelled)
	}
	if baseline := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run"}); baseline.State != desktop.Completed {
		t.Fatal(baseline)
	}
	defective := demoOf(t, app, opened.Context)
	if !reflect.DeepEqual(done(defective), []string{"create-test", "run-defective"}) || defective.Steps[2].Entry != "baseline-run" || defective.Steps[2].Status != string(testrunner.AssertionFailure) {
		t.Fatalf("after the defective run: %+v", defective)
	}
	if fixed := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: guide.StepPostFix, Output: "post-fix-run"}); fixed.State != desktop.Completed {
		t.Fatal(fixed)
	}
	if all := demoOf(t, app, opened.Context); !reflect.DeepEqual(done(all), []string{"create-test", "run-defective", "run-fixed"}) {
		t.Fatalf("after both runs: %v", done(all))
	}
	named, context := namedProject(t)
	if other := named.DemoProgress(context); other.State != desktop.Empty || other.Demo != nil {
		t.Fatalf("a project that is not the demo: %+v", other)
	}
}

// A test saved through the catalog, as the Tests screen saves one, completes
// the demo's test step: the catalog saves the revision as an entry of the
// project, the guide prefers the catalog's current revision, and the
// practice run executes it by the entry the progress reports. A later
// revision replaces the earlier one as the demo's test.
func TestADemoTestSavedThroughTheCatalogIsFoundByTheGuide(t *testing.T) {
	app := activatedApp(t, &chooser{}, t.TempDir())
	opened := openDemo(t, app)
	root := opened.Context.Project
	fresh := demoOf(t, app, opened.Context)
	draft := fresh.Test
	if draft.Case.Identity != guide.CaseIdentity || draft.Target != guide.TargetName || fresh.Steps[2].Output != "defective-run" || fresh.Steps[4].Output != "fixed-run" || fresh.CaseRef == nil {
		t.Fatalf("the supplied test and run outputs: %+v", fresh)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: opened.Context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &draft}, IntentID: "demo-test"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("saving the demo test: %+v", saved)
	}
	progress := demoOf(t, app, opened.Context)
	if !reflect.DeepEqual(done(progress), []string{"create-test"}) || !strings.HasPrefix(progress.Spec, "test-") || progress.Steps[1].Ref == nil || progress.Steps[1].Ref.ID != saved.Saved.ID {
		t.Fatalf("the catalog-saved test was not found: %+v", progress)
	}
	draft.Reset = "Restart the practice receiver from an empty ledger before each run."
	revised := app.SaveItem(desktop.SaveItemRequest{Context: opened.Context, Kind: desktop.TestItem, Item: saved.Saved.ID,
		BaseRevision: "1", Draft: desktop.ItemDraft{Test: &draft}, IntentID: "revise-2"})
	if revised.Outcome != desktop.SavedOutcome {
		t.Fatalf("revising the demo test: %+v", revised)
	}
	first := progress.Spec
	progress = demoOf(t, app, opened.Context)
	if progress.Spec == first || !reflect.DeepEqual(done(progress), []string{"create-test"}) {
		t.Fatalf("the revised test: %+v", progress)
	}
	current, err := testrunner.ReadSpec(filepath.Join(root, progress.Spec))
	if err != nil || current.Setup.ResetInstructions != draft.Reset {
		t.Fatalf("the guide read an earlier revision: %+v %v", current.Setup, err)
	}
	baseline := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: progress.Spec, Trial: guide.StepBaseline, Output: progress.Steps[2].Output})
	if baseline.State != desktop.Completed || baseline.Practice.Status != testrunner.AssertionFailure {
		t.Fatalf("the catalog-saved test's practice run: %+v", baseline)
	}
	after := demoOf(t, app, opened.Context)
	if !reflect.DeepEqual(done(after), []string{"create-test", "run-defective"}) || after.Steps[2].Ref == nil || after.Steps[2].Ref.Kind != desktop.RunItem {
		t.Fatalf("the run of the catalog-saved test: %+v", after.Steps)
	}
}

// Help answers from the bundled articles while another operation holds the
// slot: it takes none, and it never substitutes one article for another.
func TestHelpReadsBundledArticlesWithoutTheSlot(t *testing.T) {
	app := workspaceApp(t)
	release, held := desktop.HoldSlotForTest(app, "")
	if !held {
		t.Fatal("the slot could not be held")
	}
	defer release()
	topics := app.HelpTopics()
	if topics.State != desktop.Completed || len(topics.Topics) != 5 || topics.Topics[0].Title != "Import messages" {
		t.Fatalf("HelpTopics: %+v", topics)
	}
	article := app.HelpArticle("import-messages")
	if article.State != desktop.Completed || article.Article.Action == nil || article.Article.Action.ID != "start-import" {
		t.Fatalf("HelpArticle: %+v", article)
	}
	if missing := app.HelpArticle("nothing-here"); missing.State != desktop.Failed || missing.Article != nil || missing.Reason != "this help article is not in this version" {
		t.Fatalf("a missing article: %+v", missing)
	}
	if found := app.SearchHelp("timestamps"); found.State != desktop.Completed || len(found.Matches) == 0 {
		t.Fatalf("SearchHelp: %+v", found)
	}
	if none := app.SearchHelp("zzzz"); none.State != desktop.Empty || none.Matches == nil || len(none.Matches) != 0 {
		t.Fatalf("an unmatched search: %+v", none)
	}
	if empty := app.SearchHelp(""); empty.State != desktop.Failed {
		t.Fatalf("an empty search: %+v", empty)
	}
}

// Diagnostics names this build, its platform and every named operation with
// the admission it takes, and nothing else: no path, project or value.
func TestDiagnosticsNamesTheBuildAndOperationsAndNoPaths(t *testing.T) {
	state := t.TempDir()
	app := activatedApp(t, &chooser{}, state)
	result := app.Diagnostics()
	if result.State != desktop.Completed || result.Diagnostics == nil {
		t.Fatalf("Diagnostics: %+v", result)
	}
	diagnostics := *result.Diagnostics
	if diagnostics.Version != engine.Version() || diagnostics.GoVersion == "" || diagnostics.OS == "" || diagnostics.Arch == "" {
		t.Fatalf("the build: %+v", diagnostics)
	}
	declared := desktop.DeclaredProfilesForTest()
	if len(diagnostics.Operations) != len(declared) {
		t.Fatalf("%d operations for %d declared", len(diagnostics.Operations), len(declared))
	}
	for _, operation := range diagnostics.Operations {
		profile := declared[operation.Method]
		if operation.Name != profile.Name || operation.Interruptible != profile.Interruptible || len(operation.Prerequisites) != len(profile.Prerequisites()) {
			t.Errorf("%s: %+v, declared %+v", operation.Method, operation, profile)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{state, home, string(filepath.Separator), "\\"} {
		if path != "" && strings.Contains(string(encoded), path) {
			t.Errorf("Diagnostics names a path (%q): %s", path, encoded)
		}
	}
}

// The demo's practice runs are runs of the project: Runs lists them, the run
// page shows the defective receiver's failed check and the fixed receiver's
// passed one, and the two are compared as any two runs of a test are.
func TestTheDemoRunsOpenAndCompareAsOrdinaryRuns(t *testing.T) {
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()})
	opened := openDemo(t, app)
	root := opened.Context.Project
	draft := demoOf(t, app, opened.Context).Test
	if saved := app.SaveItem(desktop.SaveItemRequest{Context: opened.Context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &draft}, IntentID: "demo-test"}); saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("the supplied test: %+v", saved)
	}
	progress := demoOf(t, app, opened.Context)
	for _, trial := range []struct {
		step   int
		trial  string
		result desktop.RunCheckResult
	}{{2, guide.StepBaseline, "failed"}, {4, guide.StepPostFix, "passed"}} {
		ran := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: progress.Spec, Trial: trial.trial, Output: progress.Steps[trial.step].Output})
		if ran.State != desktop.Completed {
			t.Fatalf("the %s run: %+v", trial.trial, ran)
		}
		progress = demoOf(t, app, opened.Context)
		ref := progress.Steps[trial.step].Ref
		if ref == nil || ref.Kind != desktop.RunItem {
			t.Fatalf("the %s run is not a run of the project: %+v", trial.trial, progress.Steps[trial.step])
		}
		detail := app.OpenRun(desktop.RunRequest{Context: opened.Context, Run: *ref})
		if detail.State != desktop.Completed || len(detail.Run.Checks) != 1 || detail.Run.Checks[0].Result != trial.result {
			t.Fatalf("the %s run page: %+v", trial.trial, *detail.Run)
		}
	}
	compared := app.CompareRunItems(desktop.RunComparisonItemsRequest{Context: opened.Context, Runs: []desktop.ItemRef{*progress.Steps[2].Ref, *progress.Steps[4].Ref}})
	if compared.State != desktop.Completed || compared.Comparison == nil {
		t.Fatalf("comparing the demo's runs: %+v", compared)
	}
}
