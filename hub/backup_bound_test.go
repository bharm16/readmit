package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hubprotocol"
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

func TestBackupV4MaximumCataloguesAndLifecycleReadBack(t *testing.T) {
	team := true
	m := backupManifest{Schema: "readmit-hub-backup/v4", MetadataVersion: 5, Team: &team, Artifacts: make([]backupEntry, 65536), Projects: make([]projectLink, 65536), Reviews: []ReviewEvent{}, Lifecycle: make([]LifecycleEvent, hubprotocol.MaxLifecycle)}
	project := strings.Repeat("z", 64)
	for i := range m.Artifacts {
		d := fmt.Sprintf("%064x", i)
		m.Artifacts[i] = backupEntry{d, 67108864, "9999-12-31T23:59:59.999999999Z"}
		m.Projects[i] = projectLink{project, d}
	}
	for i := range m.Lifecycle {
		m.Lifecycle[i] = LifecycleEvent{Schema: "readmit-hub-lifecycle-event/v1", Project: project, Sequence: i + 1, Issuer: "https://example.invalid/" + strings.Repeat("x", 2024), Actor: strings.Repeat("a", 256), At: "9999-12-31T23:59:59.999999999Z", Command: LifecycleCommand{Schema: "readmit-hub-lifecycle-command/v1", ID: fmt.Sprintf("%064d", i), Expected: i, Kind: "remove-user", Parents: []string{}, Subject: strings.Repeat("b", 256), Reason: strings.Repeat("\"", 2048)}}
	}

	// Both independent event catalogues can reach their supported maxima together.
	emptyDigest := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	m.Artifacts[65535] = backupEntry{emptyDigest, 0, "9999-12-31T23:59:59.999999999Z"}
	m.Projects[65535] = projectLink{project, emptyDigest}
	m.Reviews = make([]ReviewEvent, hubprotocol.MaxReviews)
	for i := range m.Reviews {
		m.Reviews[i] = ReviewEvent{Schema: "readmit-hub-review-event/v1", Project: project, Sequence: i + 1, Issuer: "https://example.invalid/" + strings.Repeat("x", 2024), Actor: strings.Repeat("a", 256), At: "9999-12-31T23:59:59.999999999Z", Command: ReviewCommand{Schema: "readmit-hub-review-command/v1", ID: fmt.Sprintf("%064d", i), Expected: i, Kind: "comment", Evidence: emptyDigest, Recipient: strings.Repeat("c", 256), Text: strings.Repeat("\"", 2048)}}
	}
	data, e := encodeBackupManifest(m)
	if e != nil {
		t.Fatal(e)
	}
	if len(data) > 128<<20 {
		t.Fatal("maximum writer exceeds reader bound")
	}
	dir := t.TempDir()
	if e = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, emptyDigest), nil, 0600); e != nil {
		t.Fatal(e)
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	got, e := readBackup(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Reviews) != hubprotocol.MaxReviews || len(got.Lifecycle) != hubprotocol.MaxLifecycle || len(got.Artifacts) != 65536 || len(got.Projects) != 65536 {
		t.Fatal("maximum backup truncated")
	}
}
