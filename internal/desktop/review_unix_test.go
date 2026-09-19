//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// A plan this account cannot read is a different answer from one the reader
// refused, and the window says which: there is nothing to fix in the document,
// and the remedy is not to write it again.
func TestAPreviewSeparatesPermissionFromRefusal(t *testing.T) {
	app, root, identity := transformWorkspace(t, `{"operator":"shift-dates/v1","shift":"24h"}`)
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses file permissions")
	}
	plan := filepath.Join(root, "plan.json")
	if err := os.Chmod(plan, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(plan, 0o600) })

	result := app.PreviewTransformation(transformRequest(root, identity))
	if result.State != desktop.PermissionDenied || result.Transformation != nil {
		t.Fatalf("a plan this account cannot read was not reported as a permission: %+v", result)
	}
	if result.Reason == "" || strings.Contains(result.Reason, root) {
		t.Fatalf("the refusal says nothing, or repeats a path: %q", result.Reason)
	}

	// Recovery: the slot was released and the same plan is previewed once the
	// document can be read again.
	if err := os.Chmod(plan, 0o600); err != nil {
		t.Fatal(err)
	}
	if recovered := app.PreviewTransformation(transformRequest(root, identity)); recovered.State != desktop.Completed {
		t.Fatalf("a refused document left the facade unable to preview: %+v", recovered)
	}
}
