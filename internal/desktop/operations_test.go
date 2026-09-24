package desktop_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestUnactivatedDesktopRetainsSampleReadsButRefusesAuthoring(t *testing.T) {
	app := desktop.New(&chooser{folder: t.TempDir()}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
	sample := app.CreateSampleWorkspace()
	if sample.State != desktop.Completed {
		t.Fatal(sample)
	}
	if result := app.StartDurableRun(desktop.DurableRunRequest{Workspace: "absent", Spec: "absent", Output: "absent"}); result.State != desktop.PermissionDenied {
		t.Fatal(result)
	}
	if result := app.SaveNote("absent", project.Note{}); result.State != desktop.PermissionDenied {
		t.Fatal(result)
	}
	if result := app.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
		t.Fatal(result)
	}
	if result := app.SaveNote("absent", project.Note{}); result.State == desktop.PermissionDenied {
		t.Fatal("valid trial did not admit authoring", result)
	}
	if result := app.ReleaseOperations(); result.State != desktop.Completed {
		t.Fatal(result)
	}
	if result := app.SaveNote("absent", project.Note{}); result.State != desktop.PermissionDenied {
		t.Fatal(result)
	}
	if result := app.OpenWorkspace(sample.Workspace.Root); result.State != desktop.Completed {
		t.Fatal(result)
	}
}

func TestDesktopOperationSelectionSurvivesRestartWithoutReactivation(t *testing.T) {
	dir := t.TempDir()
	selection := filepath.Join(dir, "operations.json")
	app := desktop.NewWithOperationSelection(&chooser{}, filepath.Join(dir, "recent"), filepath.Join(dir, "filters"), filepath.Join(dir, "session"), filepath.Join(dir, "drafts"), selection)
	if result := app.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
		t.Fatal(result)
	}
	next := desktop.NewWithOperationSelection(&chooser{}, filepath.Join(dir, "recent"), filepath.Join(dir, "filters"), filepath.Join(dir, "session"), filepath.Join(dir, "drafts"), selection)
	if result := next.OperationStatus(); result.State != desktop.Completed || result.Clock == nil || result.Clock.Released {
		t.Fatal(result)
	}
	if result := next.ActivateOperations(); result.State != desktop.Failed {
		t.Fatal("existing activation reset", result)
	}
	if result := next.ReleaseOperations(); result.State != desktop.Completed {
		t.Fatal(result)
	}
	if result := app.SaveNote("absent", project.Note{}); result.State != desktop.PermissionDenied {
		t.Fatal("another window cached released authority", result)
	}
}

func TestCorrelationWritesRequireActivationButSequenceReadsDoNot(t *testing.T) {
	app := desktop.New(&chooser{}, "", "", "", "")
	if got := app.DecideCorrelation(desktop.CorrelationReviewRequest{}); got.State != desktop.PermissionDenied {
		t.Fatal(got)
	}
	if got := app.OpenCorrelationReview(desktop.CorrelationReviewRequest{}); got.State == desktop.PermissionDenied {
		t.Fatal("read gated", got)
	}
	if got := app.OpenSequence(desktop.SequenceRequest{}); got.State == desktop.PermissionDenied {
		t.Fatal("sequence analysis gated", got)
	}
}

// Admission is the first thing a sending, listening, collecting or writing
// operation does, and it can wait: it retries while another update of the
// operation clock or of the runner admission record is retained. A
// cancellation that arrives while it waits is the person's cancellation, not
// a refused license, so it answers cancelled — never permission denied —
// without waiting for admission to give up, which takes about a second of
// retries, and nothing was admitted to be settled. That holds for every
// operation whose declared profile takes an execution, every other
// interruptible one that admits the author, and a local write, each cancelled
// under the name its panel cancels while its first admission waits; and for
// every execution cancelled after its author admission was decided, while the
// execution admission itself waits — where a connectivity check, a fixture
// reset and an observation collection used to answer permission denied.
func TestACancellationDuringExecutionAdmissionIsCancelledNotDenied(t *testing.T) {
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	policy := testlicense.New(t)
	if result := app.SelectOperationPolicy(policy); result.State != desktop.Completed {
		t.Fatal(result)
	}
	root := t.TempDir()
	// A send and a reexecution refuse a request that is not authorized
	// before they ask for admission; every other operation asks first.
	requests := map[string]any{
		"SendReplay":                desktop.ReplaySendRequest{Approved: true, Expected: "previewed", Replay: desktop.ReplayRequest{Workspace: root, Output: "run"}},
		"ReexecuteReviewedEvidence": desktop.ReexecutionSendRequest{Workspace: root, Authorize: true, Expected: "previewed"},
	}
	type operation struct {
		method, name string
		start        func() desktop.State
	}
	declared := func(admits func(operationguard.Profile) bool) []operation {
		var operations []operation
		for method, profile := range desktop.DeclaredProfilesForTest() {
			if !profile.Interruptible || !admits(profile) {
				continue
			}
			call := reflect.ValueOf(app).MethodByName(method)
			arguments := make([]reflect.Value, call.Type().NumIn())
			for i := range arguments {
				arguments[i] = reflect.New(call.Type().In(i)).Elem()
			}
			if request, ok := requests[method]; ok {
				arguments[0] = reflect.ValueOf(request)
			}
			operations = append(operations, operation{method, profile.Name, func() desktop.State {
				state, _ := stateOf(call.Call(arguments)[0].Interface())
				return state
			}})
		}
		return operations
	}
	// A retained clock update keeps every admission retrying; a retained
	// admission record update keeps only the execution admission retrying, so
	// the author admission an execution also declares is decided first. A
	// runner's job admission waits only once its configuration is read.
	anyAdmission := declared(func(profile operationguard.Profile) bool {
		return profile.Author || profile.Execution != operationguard.NoExecution
	})
	anyAdmission = append(anyAdmission, operation{"BuildIndex", "", func() desktop.State { return app.BuildIndex(desktop.BuildIndexRequest{Workspace: root}).State }})
	executionAdmission := declared(func(profile operationguard.Profile) bool { return profile.Execution == operationguard.Execute })
	if len(executionAdmission) < 12 {
		t.Fatalf("implausibly few declared executions: %d", len(executionAdmission))
	}
	clock := filepath.Join(filepath.Dir(policy), "clock.json")
	for update, operations := range map[string][]operation{"clock.json.incomplete": anyAdmission, "admissions.json.incomplete": executionAdmission} {
		retained := filepath.Join(filepath.Dir(policy), update)
		if err := os.WriteFile(retained, nil, 0600); err != nil {
			t.Fatal(err)
		}
		for _, operation := range operations {
			before, err := os.Stat(clock)
			if err != nil {
				t.Fatal(err)
			}
			answered, holding := startHolding(t, app, root, bareState, operation.start)
			if !holding {
				t.Fatalf("%s answered %s before it could be cancelled", operation.method, <-answered)
			}
			if update == "admissions.json.incomplete" {
				// Every admission replaces the clock once it is decided, and a
				// waiting execution admission replaces it again on each retry:
				// a second replacement is the execution admission waiting,
				// after any author admission the operation declares.
				awaitReplaced(t, clock, before, 2)
			}
			requested := time.Now()
			app.Cancel(operation.name)
			if state := <-answered; state != desktop.Cancelled {
				t.Errorf("cancelling %s (%q) while %s was retained answered %s", operation.method, operation.name, update, state)
			}
			if waited := time.Since(requested); waited > 500*time.Millisecond {
				t.Errorf("cancelling %s waited %s for admission to give up", operation.method, waited)
			}
		}
		// Once the retained update is resolved, admission decides again.
		if err := os.Remove(retained); err != nil {
			t.Fatal(err)
		}
		if result := app.DiagnoseSource(desktop.SourceWorkRequest{Workspace: root}); result.State == desktop.PermissionDenied || result.State == desktop.Cancelled {
			t.Fatalf("admission did not decide once %s was gone: %+v", update, result)
		}
	}
}

// awaitReplaced waits until the file at path has been replaced at least times
// since it was as before, each atomic rewrite being a new file.
func awaitReplaced(t *testing.T, path string, before os.FileInfo, times int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for replaced := 0; replaced < times; {
		current, err := os.Stat(path)
		if err == nil && !os.SameFile(before, current) {
			replaced++
			before = current
			continue
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s was replaced %d times, want %d", filepath.Base(path), replaced, times)
		}
		time.Sleep(time.Millisecond)
	}
}
