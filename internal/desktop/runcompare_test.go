package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
)

func TestExecutionComparisonRetainsFailureAndSeparatesDrift(t *testing.T) {
	app, root := guided(t)
	spec := authorGuidedTest(t, app, root, "comparison-test.json", 1)
	for _, trial := range []struct{ name, step string }{{"broken", guide.StepBaseline}, {"fixed", guide.StepPostFix}} {
		got := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: trial.step, Output: trial.name})
		if got.State != desktop.Completed {
			t.Fatal(got)
		}
		if err := os.Rename(filepath.Join(root, trial.name, "result"), filepath.Join(root, trial.name+"-result")); err != nil {
			t.Fatal(err)
		}
	}
	req := desktop.RunComparisonRequest{Workspace: root, Baseline: "broken-result", Current: "fixed-result"}
	got := app.CompareRuns(req)
	if got.State != desktop.Completed || got.Comparison == nil {
		t.Fatal(got)
	}
	c := got.Comparison
	if c.Baseline.Status != "assertion_failure" || c.Current.Status != "pass" || len(c.Assertions) != 1 || c.Assertions[0].Behavior != "changed" {
		t.Fatalf("failure vanished: %+v", c)
	}
	if c.Drift.Left.Target.Revision != "unknown" || c.Drift.Drift[1].Outcome != "changed" {
		t.Fatalf("configuration conflated with behavior: %+v", c.Drift)
	}
	if c.Baseline.Excluded != "unknown: the original case is not reopened" || c.Approval != "not_selected" {
		t.Fatal(c)
	}
	req.Repeats = []string{"broken-result"}
	if duplicate := app.CompareRuns(req); duplicate.State != desktop.Failed {
		t.Fatal("duplicate evidence counted as repeat")
	}
	req.Repeats = nil
	req.Baseline = "../broken"
	if outside := app.CompareRuns(req); outside.State != desktop.Failed {
		t.Fatal("escaped workspace")
	}
	req.Baseline = "broken-result"
	if err := os.WriteFile(filepath.Join(root, "broken-result", "result.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	refused := app.CompareRuns(req)
	raw, _ := json.Marshal(refused)
	if refused.State != desktop.Failed || refused.Comparison != nil || strings.Contains(string(raw), root) {
		t.Fatalf("invalid evidence/privacy: %s", raw)
	}
}

func TestRunComparisonSharesBusySlotAndRecovers(t *testing.T) {
	chooser := &chooser{}
	app := newApp(t, chooser)
	chooser.before = func() {
		if got := app.CompareRuns(desktop.RunComparisonRequest{}); got.State != desktop.Busy || got.Comparison != nil {
			t.Fatalf("busy: %+v", got)
		}
	}
	app.SelectWorkspace()
	if got := app.CompareRuns(desktop.RunComparisonRequest{}); got.State == desktop.Busy {
		t.Fatal("busy slot leaked")
	}
}
