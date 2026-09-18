package desktop

import (
	"encoding/json/v2"
	"path/filepath"
	"testing"
)

func FuzzRecent(f *testing.F) {
	f.Add([]byte(`{"schema":"readmit-desktop-recent/v1","roots":["/evidence/case"]}`))
	f.Add([]byte(`{"schema":"readmit-desktop-recent/v2","roots":[]}`))
	f.Add([]byte(`{"schema":"readmit-desktop-recent/v1","roots":["relative"],"extra":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxRecentBytes {
			return
		}
		roots, err := decodeRecent(data)
		if err != nil {
			if len(err.Error()) > 256 {
				t.Fatal("unbounded diagnostic")
			}
			return
		}
		if len(roots) > MaxRecentWorkspaces {
			t.Fatal("accepted an unbounded recent workspace list")
		}
		seen := make(map[string]bool, len(roots))
		for _, root := range roots {
			if !filepath.IsAbs(root) || len(root) > maxRootBytes || seen[root] {
				t.Fatal("accepted a relative, oversized, or duplicated workspace root")
			}
			seen[root] = true
		}
		// Whatever the decoder accepts must round-trip through the writer's
		// encoding, so a list the shell stores is a list it can read back.
		encoded, err := json.Marshal(recentList{Schema: RecentSchema, Roots: roots}, json.Deterministic(true))
		if err != nil {
			t.Fatal("accepted a list the shell cannot store")
		}
		if _, err := decodeRecent(encoded); err != nil {
			t.Fatalf("stored list cannot be read back: %v", err)
		}
	})
}
