//go:build !windows

package secret

import (
	"os"
	"path/filepath"
	"testing"
)

// A named path is resolved, so naming a symbolic link checks what it points at:
// that is the path the person asked about. An entry found beneath a named
// directory is never followed, so walking a tree cannot leave it, and the link
// is counted as not read rather than silently ignored.
func TestCollectResolvesNamedPathsAndNeverFollowsEntriesBeneathThem(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("outside the tree"), 0600); err != nil {
		t.Fatal(err)
	}
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, "inside.json"), []byte("inside the tree"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree, "escape.json")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	files, skipped, err := Collect([]string{tree})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(files) != 1 || string(files[tree+"/inside.json"]) != "inside the tree" {
		t.Fatalf("collect read %v", files)
	}
	if _, followed := files[tree+"/escape.json"]; followed {
		t.Fatal("collect followed a symbolic link out of the tree it was given")
	}
	if skipped != 1 {
		t.Fatalf("collect reported %d entries not read, want 1", skipped)
	}

	// The same link named directly is the path the caller asked about, and its
	// target is read under the name the caller used.
	named := filepath.Join(tree, "escape.json")
	files, skipped, err = Collect([]string{named})
	if err != nil || skipped != 0 {
		t.Fatalf("collect of a named link: %v, %d not read", err, skipped)
	}
	if string(files[named]) != "outside the tree" {
		t.Fatalf("a named link was not resolved: %v", files)
	}
}
