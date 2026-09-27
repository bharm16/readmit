package desktop_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/index"
)

// Search settings name no file: the first save writes the case's index, a
// later save replaces only that index, verified as this case's own, and an
// index of another case under the expected name is left untouched while the
// new one takes a free name beside it.
func TestSaveSearchSettingsChoosesItsOwnDestinationAndReplacesOnlyThisCasesIndex(t *testing.T) {
	app, root, _, opened := messagesWorkspace(t)
	app = activatedApp(t, &chooser{}, t.TempDir())
	if described := app.DescribeSearchSettings(root, "incident", opened.Identity); described.State != desktop.Empty || described.Settings != nil {
		t.Fatalf("a case with no index: %+v", described)
	}
	followup, err := bundle.Open(filepath.Join(root, "followup"))
	if err != nil {
		t.Fatal(err)
	}
	writeIndex(t, root, "incident.index.json", followup, nil)
	foreign := mustReadFile(t, filepath.Join(root, "incident.index.json"))

	request := desktop.SearchSettingsRequest{Workspace: root, Case: "incident", Identity: opened.Identity,
		Fields: []string{"PID-3"}, Retention: index.RetainStates, RetainUntil: "indefinite"}
	first := app.SaveSearchSettings(request)
	if first.State != desktop.Completed || first.Index == nil || first.Index.IndexName != "incident.index-2.json" {
		t.Fatalf("first save: %+v", first)
	}
	if !reflect.DeepEqual(mustReadFile(t, filepath.Join(root, "incident.index.json")), foreign) {
		t.Fatal("another case's index was replaced")
	}
	described := app.DescribeSearchSettings(root, "incident", opened.Identity)
	if described.State != desktop.Completed || !reflect.DeepEqual(described.Settings.Fields, []string{"PID[1]-3[1]"}) ||
		described.Settings.Retention != index.RetainStates || described.Settings.RetainUntil != nil || described.Settings.Expired {
		t.Fatalf("describe: %+v", described)
	}

	request.Fields, request.Retention, request.RetainUntil = []string{"PID-3", grid.AckCodeSelector}, index.RetainValues, "2099-01-01T00:00:00Z"
	second := app.SaveSearchSettings(request)
	if second.State != desktop.Completed || second.Index.IndexName != "incident.index-2.json" || len(second.Index.Fields) != 2 {
		t.Fatalf("second save: %+v", second)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 4 {
		t.Fatalf("saving settings again left %d entries", len(entries))
	}
	// The saved index is reused by the reader for the questions it retains.
	read := readMessages(t, app, root, opened, grid.Query{AckCodes: []string{"AA"}})
	if read.State != desktop.Completed || !reflect.DeepEqual(rowIDs(read.Rows), []string{"s0001-e000002"}) {
		t.Fatalf("reading through the saved index: %+v", read)
	}
	if refused := app.SaveSearchSettings(desktop.SearchSettingsRequest{Workspace: root, Case: "incident", Identity: opened.Identity, Fields: []string{"PID-3"}, Retention: index.RetainStates}); refused.State != desktop.Failed {
		t.Fatalf("an unstated retention end was accepted: %+v", refused)
	}
}
