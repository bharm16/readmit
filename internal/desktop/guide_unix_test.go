//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
)

// A workspace this account cannot write is a different answer from a request the
// engine refused, and the window says which. Nothing is sent either way: the run
// is refused before a socket is bound.
func TestAPracticeRunSeparatesPermissionFromFailure(t *testing.T) {
	app, root := guided(t)
	spec := authorGuidedTest(t, app, root, "reschedule-test.json", 1)
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0700) })

	result := app.RunPractice(desktop.PracticeRequest{
		Workspace: root, Spec: spec, Trial: guide.StepBaseline, Output: "baseline-run",
	})
	if result.State != desktop.PermissionDenied || result.Practice != nil {
		t.Fatalf("an unwritable workspace was not reported as permission denied: %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(root, "baseline-run")); !os.IsNotExist(err) {
		t.Fatal("a refused practice run left a folder behind")
	}
}
