//go:build !windows

package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/reproducer"
)

// A workspace this account cannot write is a different answer from a plan or a
// destination the engine refused, and the window says which.
func TestBuildingAReproducerSeparatesPermissionFromFailure(t *testing.T) {
	app, root, identity := reproducerWorkspace(t)
	selected := edit(t, app, root, identity, reproducer.Plan{},
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID})
	if os.Geteuid() == 0 {
		t.Skip("a privileged account bypasses directory permissions")
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0700) })
	build := request(root, identity, selected.Reproducer.Plan, reproducer.Step{})
	build.Output = "incident-reproducer"
	result := app.BuildReproducer(build)
	if result.State != desktop.PermissionDenied || result.Reproducer != nil {
		t.Fatalf("an unwritable workspace was not reported as permission denied: %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(root, "incident-reproducer")); !os.IsNotExist(err) {
		t.Fatal("a refused build left a folder behind")
	}
}
