package desktop_test

import (
	"github.com/bharm16/readmit/internal/desktop"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopReleasedExpectationReviewAndStaleProfile(t *testing.T) {
	app, root, _ := authoringWorkspace(t)
	spec := `{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`
	if e := os.WriteFile(filepath.Join(root, "test.json"), []byte(spec), 0600); e != nil {
		t.Fatal(e)
	}
	request := desktop.BaselineRequest{Workspace: root, Spec: "test.json", Release: true, ReleaseID: "booking", Approver: "reviewer", Rationale: "fixture", Output: "release.json"}
	if got := app.ApproveBaseline(request); got.State != desktop.Failed {
		t.Fatal("unreviewed release")
	}
	review := app.ReviewBaseline(request)
	if review.State != desktop.Completed {
		t.Fatal(review)
	}
	request.Review = review.Comparison.Identity
	profile, e := os.ReadFile("../../testdata/fixtures/local-profile.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "profile.json"), profile, 0600); e != nil {
		t.Fatal(e)
	}
	request.Profiles = []string{"profile.json"}
	if got := app.ApproveBaseline(request); got.State != desktop.Failed {
		t.Fatal("stale profile selection accepted")
	}
	review = app.ReviewBaseline(request)
	if review.State != desktop.Completed {
		t.Fatal(review)
	}
	request.Review = review.Comparison.Identity
	if got := app.ApproveBaseline(request); got.State != desktop.Completed {
		t.Fatal(got)
	}
	if e = os.Remove(filepath.Join(root, "test.json")); e != nil {
		t.Fatal(e)
	}
	request.Previous = "release.json"
	if got := app.OpenBaseline(request); got.State != desktop.Completed || got.PreviousApprover != "reviewer" {
		t.Fatal(got)
	}
	request.Previous = "../release.json"
	if got := app.OpenBaseline(request); got.State != desktop.Failed {
		t.Fatal("path escape")
	}
}
