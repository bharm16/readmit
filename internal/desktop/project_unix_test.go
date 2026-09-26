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
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("open: %+v", opened)
	}
	item := caseAt(t, app, opened.Context, "incident-4821")
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })

	result := saveCase(app, opened.Context, item, "register", desktop.CaseDraft{Name: "Duplicate appointment", Status: "open", InterfaceRevision: "siu-2.5.1-v1"})
	if result.State != desktop.PermissionDenied || result.Saved != nil {
		t.Fatalf("an unwritable project was not reported as permission denied: %+v", result)
	}
	settings := saveProject(app, opened.Context, "retitle", desktop.ProjectDraft{Name: "Epic scheduling interface, 2026",
		Revisions: []desktop.RevisionDraft{{ID: "siu-2.5.1-v1", Name: "siu-2.5.1-v1", Default: true}, {ID: "siu-2.5.1-v2", Name: "siu-2.5.1-v2"}}})
	if settings.State != desktop.PermissionDenied {
		t.Fatalf("a settings write into an unwritable project was not permission denied: %+v", settings)
	}
}

// A project folder this account cannot list has its recovery copies refused
// as a permission the account lacks rather than as a failure, so the window
// says what to do about it.
func TestRecoveryCopiesSeparatePermissionFromFailure(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	root := sampleProject(t, app)
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	if err := os.Chmod(root, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o700) })
	listed := app.ListProjectRecoveryCopies(root)
	if listed.State != desktop.PermissionDenied || len(listed.Copies) != 0 {
		t.Fatalf("an unlistable project folder: %+v", listed)
	}
}
