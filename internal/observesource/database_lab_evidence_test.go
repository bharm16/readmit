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

// Keep local PostgreSQL proof, hosted discovery, and the final pinned main-run
// evidence readable. Each tree binds its exact bytes and the production readers'
// decisions; the two hosted runs remain distinct.
func TestCommittedDatabaseQualificationEvidence(t *testing.T) {
	for _, cell := range []string{
		"postgresql-16-linux-arm64", "postgresql-17-linux-arm64", "postgresql-18-linux-arm64",
		"postgresql-16-linux-amd64", "postgresql-17-linux-amd64", "postgresql-18-linux-amd64",
		"sqlserver-2019-linux-amd64", "sqlserver-2022-linux-amd64", "sqlserver-2025-linux-amd64",
	} {
		t.Run(cell, func(t *testing.T) {
			root := filepath.Join("..", "..", "testdata", "lab-evidence", cell)
			checkLabEvidence(t, root)
		})
	}
	mainRoot := filepath.Join("..", "..", "testdata", "lab-evidence", "pinned-main-36029817324")
	provenance, err := os.ReadFile(filepath.Join(mainRoot, "PROVENANCE.md"))
	if err != nil || !strings.Contains(string(provenance), "36029817324") ||
		!strings.Contains(string(provenance), "cea49f96669a18759ba323c38a915886d62430b6") {
		t.Fatal("missing exact pinned main-run provenance")
	}
	for _, cell := range []struct {
		name, image, patch string
	}{
		{"postgresql-16-linux-amd64", "postgres@sha256:a3b7f434b2dc57ce85a67e171163eb8ab1a1ebcb39d27484661f26b1dfbe30d6", "16.15"},
		{"postgresql-17-linux-amd64", "postgres@sha256:d74eeac9a635390a49bc21bd49fccd973de707e2a53a76ac49b552b8712ec46f", "17.11"},
		{"postgresql-18-linux-amd64", "postgres@sha256:5a5a84b19854a9ffaa54082c166ff4ec27473a361e496e5ea167f298f2da9722", "18.6"},
		{"sqlserver-2019-linux-amd64", "mcr.microsoft.com/mssql/server@sha256:ef0b8db33970ecd01bed49c3a84a1d083c435a9891718df619298b67b352e74a", "15.0.4490.9"},
		{"sqlserver-2022-linux-amd64", "mcr.microsoft.com/mssql/server@sha256:4402d880dd4c34bfa7d8705e56a86cd6c88da80a1f6bbbe741f999e76264a090", "16.0.4295.3"},
		{"sqlserver-2025-linux-amd64", "mcr.microsoft.com/mssql/server@sha256:2b5b581621126574f3d1f75e78d3eebe8d05aedb59ad0cfdf9aa42cb0634d726", "17.0.5005.3"},
	} {
		t.Run("pinned-main/"+cell.name, func(t *testing.T) {
			root := filepath.Join(mainRoot, cell.name)
			checkLabEvidence(t, root)
			manifest, err := os.ReadFile(filepath.Join(root, "sha256sums.txt"))
			if err != nil {
				t.Fatal("missing pinned main-run manifest")
			}
			manifestDigest := sha256.Sum256(manifest)
			if !strings.Contains(string(provenance), hex.EncodeToString(manifestDigest[:])) {
				t.Fatal("pinned main-run provenance disagrees with manifest")
			}
			qualification, err := os.ReadFile(filepath.Join(root, "qualification.md"))
			if err != nil {
				t.Fatal("missing pinned main-run qualification")
			}
			serverVersion, err := os.ReadFile(filepath.Join(root, "server-version.txt"))
			if err != nil {
				t.Fatal("missing pinned main-run server version")
			}
			server := strings.Join(strings.Fields(string(serverVersion)), " ")
			if !strings.Contains(string(qualification), "- Image: `"+cell.image+"`") ||
				!strings.Contains(string(qualification), "- Repo digests: `"+cell.image+"`") ||
				!strings.Contains(string(qualification), "- Image platform: linux/amd64") ||
				!strings.Contains(string(qualification), "- Server version: `"+server+"`") ||
				!strings.Contains(server, cell.patch) {
				t.Fatal("pinned main-run image or server identity disagrees with tested cell")
			}
		})
	}
}

func checkLabEvidence(t *testing.T, root string) {
	t.Helper()
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
}
