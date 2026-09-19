package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This fixture independently populates both documented maximum catalogues with
// their widest accepted scalar fields. It exercises the writer's serializer and
// actual strict file reader, without creating 65,536 payload files.
func TestBackupV2MaximumCataloguesFitAndReadBack(t *testing.T) {
	team := true
	manifest := backupManifest{Schema: "readmit-hub-backup/v2", MetadataVersion: 3, Team: &team, Artifacts: make([]backupEntry, 65536), Projects: make([]projectLink, 65536)}
	project := strings.Repeat("z", 64)
	for i := range manifest.Artifacts {
		digest := fmt.Sprintf("%064x", i)
		manifest.Artifacts[i] = backupEntry{Digest: digest, Size: 67108864, RetainedAt: "9999-12-31T23:59:59.999999999Z"}
		manifest.Projects[i] = projectLink{Project: project, Digest: digest}
	}
	data, err := encodeBackupManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// The old 16MiB ceiling cannot encode the combined supported catalogues.
	if len(data) <= 16<<20 || len(data) > 32<<20 {
		t.Fatalf("unexpected maximum fixture length %d", len(data))
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	got, err := readBackup(root)
	if err != nil {
		t.Fatal("writer produced unreadable maximum manifest", err)
	}
	if len(got.Artifacts) != 65536 || len(got.Projects) != 65536 || got.Projects[65535].Project != project || got.Artifacts[65535].Size != 67108864 {
		t.Fatal("maximum catalogue was truncated")
	}
	t.Logf("maximum combined catalogue is %d bytes", len(data))
}
