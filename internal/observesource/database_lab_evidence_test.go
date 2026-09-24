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

// Keep local PostgreSQL proof and the six-cell hosted discovery run readable.
// Discovery evidence is not a claim that the final digest-pinned main workflow
// passed; it binds these exact bytes and the production readers' decisions.
func TestCommittedDatabaseQualificationEvidence(t *testing.T) {
	for _, cell := range []string{
		"postgresql-16-linux-arm64", "postgresql-17-linux-arm64", "postgresql-18-linux-arm64",
		"postgresql-16-linux-amd64", "postgresql-17-linux-amd64", "postgresql-18-linux-amd64",
		"sqlserver-2019-linux-amd64", "sqlserver-2022-linux-amd64", "sqlserver-2025-linux-amd64",
	} {
		t.Run(cell, func(t *testing.T) {
			root := filepath.Join("..", "..", "testdata", "lab-evidence", cell)
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
			if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				if !entry.Type().IsRegular() {
					t.Fatal("lab evidence member is not a regular file")
				}
				name, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				if name != "sha256sums.txt" && !seen[filepath.ToSlash(name)] {
					t.Fatal("unlisted lab evidence member")
				}
				return nil
			}); err != nil {
				t.Fatal("cannot inspect complete lab evidence tree")
			}
		})
	}
}
