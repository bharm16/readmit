//go:build !windows

package desktop_test

import (
	"os"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

// A project folder this account cannot write is a different answer from a
// change the project refused, and the window says which. The probe runs only
// after a write has already failed, and it touches nothing.
func TestProjectWritesSeparatePermissionFromFailure(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	writeCase(t, root, "incident-4821", framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|1|P|2.5.1\r"))
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })

	result := app.RegisterCase(root, "incident-4821", desktop.CaseRegistration{Title: "Duplicate appointment"})
	if result.State != desktop.PermissionDenied || result.Overview != nil {
		t.Fatalf("an unwritable project was not reported as permission denied: %+v", result)
	}
	title := "Epic scheduling interface, 2026"
	refused := app.UpdateProjectSettings(root, desktop.SettingsChange{Title: &title})
	if refused.State != desktop.PermissionDenied {
		t.Fatalf("a settings write into an unwritable project was not permission denied: %+v", refused)
	}
}
