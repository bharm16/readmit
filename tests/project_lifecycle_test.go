package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectLifecyclePublicWorkflow(t *testing.T) {
	root := indexedProject(t)
	if stdout, stderr, err := run(t, "project", "migration-preview", root); err != nil || stderr != "" || !strings.Contains(stdout, `"compatible":true`) || !strings.Contains(stdout, "rebuild-on-restore") {
		t.Fatalf("preview: %s %s %v", stdout, stderr, err)
	}
	if _, stderr, err := run(t, "project", "quota", root, "--max-bytes", "10000000", "--max-files", "1000"); err != nil {
		t.Fatalf("quota: %s %v", stderr, err)
	}
	copies, err := filepath.Glob(filepath.Join(root, "project.json.recovery-*"))
	if err != nil || len(copies) == 0 {
		t.Fatal("no recovery copies")
	}
	archive := filepath.Join(t.TempDir(), "archive")
	if _, _, err := run(t, "project", "delete", root, "--output", archive); err == nil {
		t.Fatal("deletion did not require explicit flag")
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatal("unconfirmed deletion wrote archive")
	}
	if _, stderr, err := run(t, "project", "delete", root, "--output", archive, "--confirm-delete"); err != nil {
		t.Fatalf("delete: %s %v", stderr, err)
	}
	restored := filepath.Join(t.TempDir(), "restored")
	if _, stderr, err := run(t, "backup", "restore", archive, "--output", restored); err != nil {
		t.Fatalf("restore: %s %v", stderr, err)
	}
	if _, err := os.Stat(filepath.Join(restored, backupIndexName)); err != nil {
		t.Fatal("index not rebuilt")
	}
	if stdout, stderr, err := run(t, "project", "show", restored); err != nil || !strings.Contains(stdout, frozenRegressionIdentity) {
		t.Fatalf("restored evidence: %s %s %v", stdout, stderr, err)
	}
	if _, stderr, err := run(t, "project", "recover", restored, "--document", "project.json", "--digest", strings.TrimPrefix(filepath.Base(copies[0]), "project.json.recovery-")); err != nil {
		t.Fatalf("recover: %s %v", stderr, err)
	}
}
