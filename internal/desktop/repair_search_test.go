package desktop_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/index"
)

// Repairing search rebuilds only the case's own index, under exactly the
// retention it declared, and never an index whose retention has ended or
// whose declarations cannot be read.
func TestRepairSearchKeepsRetentionAndRefusesExpired(t *testing.T) {
	app := newApp(t, &chooser{folder: t.TempDir()})
	opened := storageProject(t, app, "Scheduling investigation")
	root := opened.Context.Project
	var regression desktop.ItemRef
	for _, item := range listed(t, app, root, desktop.CaseItem) {
		regression = item.Ref
	}
	request := desktop.RepairSearchRequest{Context: opened.Context, Case: regression}
	if none := app.RepairSearch(request); none.State != desktop.Empty {
		t.Fatalf("a case with no index: %+v", none)
	}
	until := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	built := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "regression", Output: "regression.index.json",
		Fields: []string{"MSH-9", "PID-3"}, Retention: index.RetainDigests, RetainUntil: until.Format(time.RFC3339)})
	if built.State != desktop.Completed {
		t.Fatalf("build: %+v", built)
	}
	path := filepath.Join(root, "regression.index.json")
	// An index made stale by an edit to what it restates is repaired under
	// the retention it declared, at the name it had.
	document, err := index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Records[0].Size++
	data, err := index.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if stale := app.DescribeIndex(root, "regression", "regression.index.json"); stale.State != desktop.Failed || stale.Index == nil || !stale.Index.Stale {
		t.Fatalf("the edited index is not stale: %+v", stale)
	}
	repaired := app.RepairSearch(request)
	if repaired.State != desktop.Completed || repaired.Index == nil || repaired.Index.IndexName != "regression.index.json" ||
		!slices.Equal(repaired.Index.Fields, built.Index.Fields) || repaired.Index.Retention != index.RetainDigests ||
		repaired.Index.RetainUntil == nil || !repaired.Index.RetainUntil.Equal(until) || !repaired.Index.Applicable {
		t.Fatalf("repair: %+v %+v", repaired, repaired.Index)
	}
	if healthy := app.DescribeIndex(root, "regression", "regression.index.json"); healthy.State != desktop.Completed || healthy.Index.Stale {
		t.Fatalf("after repair: %+v", healthy)
	}
	if entries, _ := os.ReadDir(root); slices.ContainsFunc(entries, func(entry os.DirEntry) bool { return entry.Name()[0] == '.' && entry.Name() != ".readmit" }) {
		t.Fatalf("a repair left a file behind: %v", entries)
	}
	// An index whose retention has ended is not rebuilt.
	document, err = index.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ended := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	document.Policy.RetainUntil = &ended
	if data, err = index.Encode(document); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, path)
	if expired := app.RepairSearch(request); expired.State != desktop.Failed || expired.Reason == "" {
		t.Fatalf("an expired index was repaired: %+v", expired)
	}
	if after := mustRead(t, path); string(after) != string(before) {
		t.Fatal("a refused repair changed the index")
	}
	// An index that cannot be read is not rebuilt under guessed choices.
	if err := os.WriteFile(path, []byte(`{"schema":"readmit-index/v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if damaged := app.RepairSearch(request); damaged.State != desktop.Failed || damaged.Reason == "" {
		t.Fatalf("a damaged index was repaired: %+v", damaged)
	}
}
