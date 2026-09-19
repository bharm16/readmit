package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopBaselineReviewStaleApprovalAndRecovery(t *testing.T) {
	app, root, _ := authoringWorkspace(t)
	spec := `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`
	if err := os.WriteFile(filepath.Join(root, "test.json"), []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	request := desktop.BaselineRequest{Workspace: root, Spec: "test.json", Approver: "reviewer", Rationale: "synthetic", Output: "baseline.json"}
	if got := app.ApproveBaseline(request); got.State != desktop.Failed {
		t.Fatal("unreviewed accepted")
	}
	review := app.ReviewBaseline(request)
	if review.State != desktop.Completed {
		t.Fatal(review)
	}
	for _, c := range review.Comparison.Changes {
		if c.After != "" {
			t.Fatal("value crossed default privacy boundary")
		}
	}
	request.Review = review.Comparison.Identity
	if got := app.ApproveBaseline(request); got.State != desktop.Completed || got.Output != "baseline.json" {
		t.Fatal(got)
	}
	if got := app.ApproveBaseline(request); got.State != desktop.Failed {
		t.Fatal("existing revision replaced")
	}
	request.Previous = "baseline.json"
	request.Output = "next.json"
	if got := app.ApproveBaseline(request); got.State != desktop.Failed {
		t.Fatal("stale parent accepted")
	}
	request.ShowValues = true
	next := app.ReviewBaseline(request)
	if next.State != desktop.Completed || next.PreviousApprover != "reviewer" || next.PreviousRationale != "synthetic" {
		t.Fatal(next)
	}
	request.Review = next.Comparison.Identity
	if got := app.ApproveBaseline(request); got.State != desktop.Completed {
		t.Fatal(got)
	}
	request.Spec = "../test.json"
	if got := app.ReviewBaseline(request); got.State != desktop.Failed {
		t.Fatal("workspace escaped")
	}
}

func TestBaselineSharesDesktopBusySlotAndRecovers(t *testing.T) {
	chooser := &chooser{}
	app := newApp(t, chooser)
	chooser.before = func() {
		for _, result := range []desktop.BaselineResult{app.ReviewBaseline(desktop.BaselineRequest{}), app.ApproveBaseline(desktop.BaselineRequest{}), app.OpenBaseline(desktop.BaselineRequest{})} {
			if result.State != desktop.Busy || result.Comparison != nil {
				t.Fatalf("did not respect shared busy slot: %+v", result)
			}
		}
	}
	app.SelectWorkspace()
	if result := app.ReviewBaseline(desktop.BaselineRequest{}); result.State == desktop.Busy {
		t.Fatal("busy slot leaked")
	}
}
