package tests

// A database observation reads through a credential bound to observation and
// to its one endpoint; a reset credential is a separate reference and never
// reaches that read path. The window's observation screen opens, validates and
// collects through the same source reader the command line uses, so it refuses
// a reset reference with the same reason, before any collector runs and
// without retaining a completion.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestTheWindowRefusesAResetCredentialOnTheObservationReadPath(t *testing.T) {
	directory, _, _ := collectDocuments(t, strings.Replace(databaseSourceDocument, "database-observation", "database-reset", 1))
	if err := os.WriteFile(filepath.Join(directory, "window.json"), []byte(strings.Replace(collectWindowDocument, "file-export", "database-query", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	window := desktopApp(t, directory)
	const refusal = "reset credentials are refused"
	if result := window.OpenObservationSource(directory, "source.json"); result.State == desktop.Completed || !strings.Contains(result.Reason, refusal) {
		t.Fatalf("the window opened a source that presents a reset credential: %+v", result)
	}
	if result := window.ValidateObservationSource(directory, "source.json"); result.State == desktop.Completed || !strings.Contains(result.Reason, refusal) {
		t.Fatalf("the window validated a source that presents a reset credential: %+v", result)
	}
	collected := window.CollectObservation(desktop.ObservationCollectFacadeRequest{Workspace: directory, SourceFile: "source.json", WindowFile: "window.json",
		OutputFile: "completion.json", SnapshotDir: "snapshot", Authorize: true})
	if collected.State == desktop.Completed || !strings.Contains(collected.Reason, refusal) {
		t.Fatalf("the window collected through a reset credential: %+v", collected)
	}
	for _, name := range []string{"completion.json", "snapshot"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("a refused collection retained %s", name)
		}
	}
}
