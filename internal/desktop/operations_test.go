package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testlicense"
	"path/filepath"
	"testing"
)

func TestUnactivatedDesktopRetainsSampleReadsButRefusesAuthoring(t *testing.T) {
	app := desktop.New(&chooser{folder: t.TempDir()}, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
	sample := app.CreateSampleWorkspace()
	if sample.State != desktop.Completed {
		t.Fatal(sample)
	}
	if result := app.StartDurableRun("absent", "absent"); result.State != desktop.PermissionDenied {
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
