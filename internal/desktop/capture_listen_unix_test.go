//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/evidencesource"
	"github.com/bharm16/readmit/internal/operation"
)

// A declared document this account cannot read is refused in the words the
// command line's reader refuses it with, and opens nothing. The policy reader
// carries the cause, so the window says the account was denied; the source
// reader names only its own sentence, so the window says it failed.
func TestAnUnreadableDeclaredDocumentIsRefusedAsTheCommandLineRefusesIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the superuser reads a file whatever its permissions")
	}
	app := workspaceApp(t)
	root := t.TempDir()
	policy := filepath.Join(root, "policy.json")
	source := filepath.Join(root, "source.json")
	for _, path := range []string{policy, source} {
		if err := os.WriteFile(path, []byte(facadeAnyPolicy), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	_, policyErr := operation.ReceiverPolicyRead(policy)
	if opened := app.ReadReceiverPolicy(root, "policy.json"); policyErr == nil || opened.State != desktop.PermissionDenied || opened.Reason != policyErr.Error() || opened.Policy != nil {
		t.Errorf("an unreadable policy reopened as %+v, the reader refused with %v", opened, policyErr)
	}
	_, sourceErr := evidencesource.ReadSource(source)
	if opened := app.ReadSourceRegistration(root, "source.json"); sourceErr == nil || opened.State != desktop.Failed || opened.Reason != sourceErr.Error() || opened.Source != nil {
		t.Errorf("an unreadable registration reopened as %+v, the reader refused with %v", opened, sourceErr)
	}
}
