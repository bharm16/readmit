//go:build unix

package fixturereset

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/durablerun"
)

// TestAReadAuthorityActionCannotFollowALinkOutOfThePlanDirectory holds the
// narrow authority to what it says. The declared name is checked lexically by
// the reader, and the file is opened inside the plan's own directory, so a link
// somebody left there does not widen what one action may read.
func TestAReadAuthorityActionCannotFollowALinkOutOfThePlanDirectory(t *testing.T) {
	outside := t.TempDir()
	elsewhere := filepath.Join(outside, "elsewhere.json")
	if err := os.WriteFile(elsewhere, []byte(emptySnapshot), 0600); err != nil {
		t.Fatal(err)
	}
	directory := planDirectory(t, nil)
	if err := os.Symlink(elsewhere, filepath.Join(directory, "observation.json")); err != nil {
		t.Skipf("this filesystem does not support symbolic links: %v", err)
	}
	document := planWith(`{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"observation.json"}`)
	result, _ := Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(document), PlanDirectory: directory,
	}, noResolution)
	if result.State != durablerun.ExecutionError || result.Outcome != Unconfirmed || result.Reason != ObservationUnreadable {
		t.Fatalf("a link out of the plan directory must leave the reset unconfirmed, got %+v", result)
	}
}
