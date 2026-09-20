//go:build !windows

package desktop_test

import (
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"os"
	"path/filepath"
	"testing"
)

func TestCorrelationReviewPrivateModesAndRefusedSymlinks(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	req := desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry, Output: "private-review"}
	initial := app.OpenCorrelationReview(req)
	req.Mapping = initial.View.Mapping
	req.Decision = correlate.Decision{Action: "reject", Link: initial.View.Links[0].ID, Actor: "analyst", Reason: "sensitive local text"}
	saved := app.DecideCorrelation(req)
	if saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	for _, entry := range []string{"", "machine.json", "decisions.json", "identity.sha256"} {
		info, err := os.Stat(filepath.Join(root, req.Output, entry))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("review accessible by other users: %s", entry)
		}
	}
	// No saved directory or source can be used as an output again.
	if got := app.DecideCorrelation(req); got.State != desktop.Failed {
		t.Fatal("overwrote retained review")
	}
	if err := os.Symlink(filepath.Join(root, req.Output), filepath.Join(root, "review-alias")); err != nil {
		t.Fatal(err)
	}
	req.Previous = "review-alias"
	req.Mapping = ""
	if got := app.OpenCorrelationReview(req); got.State != desktop.Failed {
		t.Fatal("followed review symlink")
	}
	req.Previous = "private-review"
	reopened := app.OpenCorrelationReview(req)
	if reopened.State != desktop.Completed || reopened.View.Mapping != saved.View.Mapping {
		t.Fatal("failed writes changed review")
	}
}
