package observesource

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observewindow"
)

// Keep the three independently retained local lab runs readable. Their
// manifests bind the files, and the production readers still refuse any
// corrupted completion or typed database result.
func TestCommittedPostgreSQLQualificationEvidence(t *testing.T) {
	for _, major := range []string{"16", "17", "18"} {
		t.Run(major, func(t *testing.T) {
			root := filepath.Join("..", "..", "testdata", "lab-evidence", "postgresql-"+major+"-linux-arm64")
			manifest, err := os.ReadFile(filepath.Join(root, "sha256sums.txt"))
			if err != nil {
				t.Fatal("missing lab evidence manifest")
			}
			lines := strings.Split(strings.TrimSpace(string(manifest)), "\n")
			if len(lines) != 94 {
				t.Fatalf("lab evidence has %d files, want 94", len(lines))
			}
			seen := map[string]bool{}
			for _, line := range lines {
				parts := strings.SplitN(line, "  ", 2)
				if len(parts) != 2 || len(parts[0]) != 64 || seen[parts[1]] || parts[1] == "" ||
					filepath.IsAbs(parts[1]) || filepath.Clean(parts[1]) != parts[1] || strings.HasPrefix(parts[1], "..") {
					t.Fatal("invalid lab evidence manifest entry")
				}
				seen[parts[1]] = true
				raw, err := os.ReadFile(filepath.Join(root, parts[1]))
				if err != nil {
					t.Fatal("missing lab evidence member")
				}
				sum := sha256.Sum256(raw)
				if hex.EncodeToString(sum[:]) != parts[0] {
					t.Fatal("lab evidence member changed")
				}
				if strings.HasSuffix(parts[1], "/completion.json") {
					if _, err := observewindow.DecodeCompletion(raw); err != nil {
						t.Fatal("retained completion is unreadable")
					}
				}
				if strings.HasSuffix(parts[1], "/database.json") {
					if _, err := DecodeDatabaseRead(raw); err != nil {
						t.Fatal("retained typed database result is unreadable")
					}
				}
			}
			for _, required := range []string{"qualification.md", "server-version.txt", "cli-public-read/completion.json",
				"populated/completion.json", "empty/completion.json", "lost-connection/completion.json", "connection-recovered/completion.json"} {
				if !seen[required] {
					t.Fatalf("lab evidence lacks %s", required)
				}
			}
		})
	}
}
