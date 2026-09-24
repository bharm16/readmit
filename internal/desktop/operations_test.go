package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testlicense"
	"os"
	"path/filepath"
	"testing"
	"time"
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
// operation clock is retained. A cancellation that arrives while it waits is
// the person's cancellation, not a refused license, so it answers cancelled —
// never permission denied — without waiting for admission to give up, which
// takes about a second of retries, and nothing was admitted to be settled.
func TestACancellationDuringExecutionAdmissionIsCancelledNotDenied(t *testing.T) {
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	policy := testlicense.New(t)
	if result := app.SelectOperationPolicy(policy); result.State != desktop.Completed {
		t.Fatal(result)
	}
	// A retained clock update keeps admission retrying rather than deciding.
	retained := filepath.Join(filepath.Dir(policy), "clock.json.incomplete")
	if err := os.WriteFile(retained, nil, 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, operation := range []struct {
		name  string
		start func() desktop.State
	}{
		{"", func() desktop.State { return app.DiagnoseSource(desktop.SourceWorkRequest{Workspace: root}).State }},
		{"collect", func() desktop.State { return app.CollectSource(desktop.SourceWorkRequest{Workspace: root}).State }},
		{"capture", func() desktop.State { return app.StartCapture(desktop.CaptureRequest{Workspace: root}).State }},
		{"durable-run", func() desktop.State { return app.StartDurableRun(desktop.DurableRunRequest{Workspace: root}).State }},
		{"durable-run", func() desktop.State { return app.StartSuiteRun(desktop.SuiteRunRequest{Workspace: root}).State }},
		{"reduction", func() desktop.State { return app.StartReduction(desktop.ReductionRequest{Workspace: root}).State }},
		{"import", func() desktop.State { return app.CommitImport(desktop.ImportCommitRequest{Workspace: root}).State }},
		{"corpus", func() desktop.State { return app.GenerateCorpus(desktop.CorpusGenerateRequest{Folder: root}).State }},
		{"", func() desktop.State { return app.BuildIndex(desktop.BuildIndexRequest{Workspace: root}).State }},
	} {
		answered, holding := startHolding(t, app, root, bareState, operation.start)
		if !holding {
			t.Fatalf("%q answered %s before it could be cancelled", operation.name, <-answered)
		}
		requested := time.Now()
		app.Cancel(operation.name)
		if state := <-answered; state != desktop.Cancelled {
			t.Errorf("cancelling %q while its admission waited answered %s", operation.name, state)
		}
		if waited := time.Since(requested); waited > 500*time.Millisecond {
			t.Errorf("cancelling %q waited %s for admission to give up", operation.name, waited)
		}
	}
	// Once the retained update is resolved, admission decides again.
	if err := os.Remove(retained); err != nil {
		t.Fatal(err)
	}
	if result := app.DiagnoseSource(desktop.SourceWorkRequest{Workspace: root}); result.State == desktop.PermissionDenied || result.State == desktop.Cancelled {
		t.Fatalf("admission did not decide once the retained update was gone: %+v", result)
	}
}
