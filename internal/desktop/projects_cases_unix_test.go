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

// A remembered project whose folder this account cannot read stays in the
// projects list, unreadable, with the reason, and is listed again as it was
// once the folder can be read.
func TestAnUnreadableProjectStaysListedWithItsReason(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	app, _, context := casesProject(t)
	if err := os.Chmod(context.Project, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(context.Project, 0o700) })
	listed := app.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem})
	if listed.Page == nil || len(listed.Page.Items) != 1 {
		t.Fatalf("projects: %+v", listed)
	}
	if item := listed.Page.Items[0]; item.Ref.ID != context.ProjectID || item.Availability != desktop.ItemUnreadable || item.Reason == "" || item.Name != "Scheduling QA" {
		t.Fatalf("an unreadable project: %+v", item)
	}
	if err := os.Chmod(context.Project, 0o700); err != nil {
		t.Fatal(err)
	}
	if again := app.ListCatalog(desktop.CatalogQuery{Kind: desktop.ProjectItem}); again.Page == nil || len(again.Page.Items) != 1 || again.Page.Items[0].Availability != desktop.ItemAvailable {
		t.Fatalf("the project did not come back: %+v", again)
	}
}
