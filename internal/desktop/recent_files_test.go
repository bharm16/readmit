package desktop_test

// The recent workspace list is read through the shell document store's one
// rule — a bounded regular file, never a link — exactly as the other six
// documents are, and a list this release cannot read is reported and never
// replaced.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestAnOversizedOrLinkedRecentListIsReportedAndNeverReplaced(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(t *testing.T, path string)
	}{
		{"oversized", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(`{"schema":"readmit-desktop-recent/v1","roots":["/evidence"],`+strings.Repeat("\"x\"", 40000)+"}"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a link", func(t *testing.T, path string) {
			outside := filepath.Join(t.TempDir(), "elsewhere.json")
			if err := os.WriteFile(outside, []byte(`{"schema":"readmit-desktop-recent/v1","roots":["/outside"]}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			state := t.TempDir()
			path := filepath.Join(state, "recent.json")
			c.plant(t, path)
			before, beforeErr := os.Lstat(path)

			app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
			if listed := app.RecentWorkspaces(); listed.State != desktop.Failed || listed.Reason == "" ||
				strings.Contains(listed.Reason, "version this release cannot read") {
				t.Fatalf("an unreadable recent list was read or misreported: %+v", listed)
			}

			// It is reported, never replaced: forgetting refuses too, and the
			// list is left exactly as it was.
			if forgotten := app.ForgetWorkspace("/evidence"); forgotten.State != desktop.Failed {
				t.Fatalf("an unreadable recent list was replaced by a forget: %+v", forgotten)
			}
			after, afterErr := os.Lstat(path)
			if beforeErr != nil || afterErr != nil || !os.SameFile(before, after) {
				t.Fatalf("the unreadable recent list was replaced: %v then %v", beforeErr, afterErr)
			}
		})
	}
}
