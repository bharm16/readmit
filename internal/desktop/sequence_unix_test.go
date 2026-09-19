//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// A rules document this account cannot read is a different answer from one the
// reader refused, and the window says which: there is nothing to fix in the
// document, and the remedy is not to write it again.
func TestASequenceSeparatesPermissionFromRefusal(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses file permissions")
	}
	if err := os.Chmod(filepath.Join(root, seqRulesEntry), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, seqRulesEntry), 0o600) })

	result := app.OpenSequence(sequenceRequest(root, identity, seqRulesEntry))
	if result.State != desktop.PermissionDenied || result.Sequence != nil {
		t.Fatalf("a rules document this account cannot read was not reported as a permission: %+v", result)
	}
	if result.Reason == "" || strings.Contains(result.Reason, root) {
		t.Fatalf("the refusal says nothing, or repeats a path: %q", result.Reason)
	}

	// Recovery: the slot was released and the same case is still readable
	// without the rules it could not open.
	if recovered := laidOut(t, app, sequenceRequest(root, identity, "")); recovered.Total != 6 {
		t.Fatalf("a refused rules document left the case unreadable: %+v", recovered)
	}
}
