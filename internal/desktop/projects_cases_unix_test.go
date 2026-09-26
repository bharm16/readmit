//go:build !windows

package desktop_test

import (
	"os"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// Removing a registered case records the removal and then unregisters it. A
// project document that cannot be replaced leaves the case registered and
// listed, the refusal says so, and removing it again succeeds.
func TestARemovalThatCannotUnregisterLeavesTheCaseListedAndRemovable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	app, _, context := casesProject(t)
	item := caseAt(t, app, context, "regression")
	if err := os.Chmod(context.Project, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(context.Project, 0o700) })
	refused := app.RemoveCaseFromProject(desktop.ItemRequest{Context: context, Ref: item.Ref})
	if refused.State != desktop.PermissionDenied || refused.Reason == "" {
		t.Fatalf("remove: %+v", refused)
	}
	opened, err := project.Open(context.Project)
	if err != nil || !slices.ContainsFunc(opened.Document.Cases, func(c project.Case) bool { return c.Name == "regression" }) {
		t.Fatalf("the case was unregistered: %+v %v", opened, err)
	}
	if !listsCaseAt(t, app, context, "regression") {
		t.Fatal("a case the project still registers is hidden")
	}
	if err := os.Chmod(context.Project, 0o700); err != nil {
		t.Fatal(err)
	}
	if again := app.RemoveCaseFromProject(desktop.ItemRequest{Context: context, Ref: item.Ref}); again.State != desktop.Completed {
		t.Fatalf("removing it again: %+v", again)
	}
	if listsCaseAt(t, app, context, "regression") {
		t.Fatal("the removed case is still listed")
	}
}
