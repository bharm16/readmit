package desktop_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/operation"
)

// The workspace below holds two cases. The first carries a booking, its
// accepted acknowledgement, a second booking, a rejected acknowledgement, and
// one occurrence nothing can decode; the second carries one booking alone. Each
// has an index of the patient identifier and the acknowledgement code beside
// it, which is what a grid reads.
const (
	gridBooking  = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	gridRebooked = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S13|CTL-3|P|2.5.1\rPID|1||MRN-2^^^READMIT^MR||ROE^RICHARD\r"
	gridAccepted = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|CTL-2|P|2.5.1\rMSA|AA|CTL-1\r"
	gridRejected = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120101||ACK^S13|CTL-4|P|2.5.1\rMSA|AE|CTL-3\r"
	gridGarbage  = "NOT-HL7-AT-ALL"

	patientField = "PID[1]-3[1]"
)

// framed, writeCase and the other generic fixture verbs live in harness_test.go.

func indexedAt() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) }

// writeIndex builds and writes one index beside the case it describes. It goes
// through the same writer the command line uses, so artifactpath decides where
// an index may be written.
func writeIndex(t *testing.T, root, name string, opened *bundle.Bundle, until *time.Time) string {
	t.Helper()
	policy := index.Policy{
		Fields:      []string{patientField, grid.AckCodeSelector},
		Retention:   index.RetainValues,
		RetainUntil: until,
	}
	document, err := index.Build(context.Background(), opened, policy, indexedAt())
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	if _, err := index.Write(filepath.Join(root, name), document); err != nil {
		t.Fatalf("write index: %v", err)
	}
	return name
}

// gridWorkspace is the two-case workspace above, with the filters file the app
// keeps this viewer's saved filters in.
func gridWorkspace(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	filters := filepath.Join(state, "filters.json")
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filters, filepath.Join(state, "session.json"), filepath.Join(filepath.Dir(filepath.Join(state, "session.json")), "drafts.json"))

	incident := writeCase(t, root, "incident",
		framed(gridBooking)+framed(gridAccepted)+framed(gridRebooked)+framed(gridRejected)+framed(gridGarbage))
	writeIndex(t, root, "incident.index.json", incident, nil)
	followup := writeCase(t, root, "followup", framed(gridBooking))
	writeIndex(t, root, "followup.index.json", followup, nil)
	return app, root, filters
}

func acknowledgements() grid.Filter {
	return grid.Filter{Name: "acknowledgements", Kinds: []bundle.EventKind{bundle.Acknowledgement}}
}

func saveFilter(t *testing.T, app *desktop.App, filter grid.Filter) {
	t.Helper()
	if result := app.SaveFilter(filter); result.State != desktop.Completed || result.Selected != filter.Name {
		t.Fatalf("the filter was not saved and selected: %+v", result)
	}
}

func openGrid(t *testing.T, app *desktop.App, root, name string, offset, limit int) desktop.GridResult {
	t.Helper()
	return app.OpenGrid(root, name, name+".index.json", offset, limit)
}

// fingerprint is every file of a directory and its exact bytes, so "a refused
// index never changes evidence" is checked against the bytes.
func fingerprint(t *testing.T, root string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		files[filepath.ToSlash(relative)] = hex.EncodeToString(sum[:])
		return nil
	}); err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	return files
}

// The grid renders a window of one case and always says how many occurrences
// the selected filter removed from it. A view that hides records without saying
// how many is the interface equivalent of a false negative.
func TestTheGridRendersAWindowAndNamesHowManyRecordsItExcluded(t *testing.T) {
	app, root, _ := gridWorkspace(t)

	unfiltered := openGrid(t, app, root, "incident", 0, grid.MaxRows)
	if unfiltered.State != desktop.Completed || unfiltered.Grid == nil {
		t.Fatalf("the grid did not open: %+v", unfiltered)
	}
	if unfiltered.Grid.Total != 5 || unfiltered.Grid.Matched != 5 || unfiltered.Grid.Excluded != 0 {
		t.Fatalf("an unfiltered grid excluded something: %+v", unfiltered.Grid)
	}
	if unfiltered.Grid.Filter != "" || unfiltered.Grid.Undecodable != 1 || len(unfiltered.Grid.Rows) != 5 {
		t.Fatalf("the unfiltered grid did not report the case as it is: %+v", unfiltered.Grid)
	}
	if unfiltered.Grid.Identity == "" || unfiltered.Grid.Case != "incident" || unfiltered.Grid.Index != "incident.index.json" {
		t.Fatalf("the grid does not name the evidence it read: %+v", unfiltered.Grid)
	}
	first := unfiltered.Grid.Rows[0]
	if first.ID != "s0001-e000001" || first.Kind != bundle.Message || first.Size == 0 || !first.Decoded {
		t.Fatalf("a row does not locate its occurrence: %+v", first)
	}
	if last := unfiltered.Grid.Rows[4]; last.Decoded {
		t.Fatalf("the occurrence the case could not decode was reported as decoded: %+v", last)
	}

	saveFilter(t, app, acknowledgements())
	filtered := openGrid(t, app, root, "incident", 0, grid.MaxRows)
	if filtered.State != desktop.Completed || filtered.Grid == nil {
		t.Fatalf("the filtered grid did not open: %+v", filtered)
	}
	if filtered.Grid.Matched != 2 || filtered.Grid.Excluded != 3 || filtered.Grid.Total != 5 {
		t.Fatalf("the filtered grid did not name what it excluded: %+v", filtered.Grid)
	}
	if filtered.Grid.Filter != "acknowledgements" {
		t.Fatalf("the grid does not name the filter it applied: %+v", filtered.Grid)
	}

	// A window over the same filter reports the same exclusion: how many
	// records are hidden never depends on how many are being drawn.
	window := openGrid(t, app, root, "incident", 1, 1)
	if window.State != desktop.Completed || len(window.Grid.Rows) != 1 || window.Grid.Rows[0].ID != "s0001-e000004" {
		t.Fatalf("the second window did not render the second match: %+v", window.Grid)
	}
	if window.Grid.Excluded != 3 || window.Grid.Matched != 2 {
		t.Fatalf("a window changed what the filter excluded: %+v", window.Grid)
	}

	// A filter that keeps nothing is empty with the reason, and still carries
	// the counts, because how many records it removed is the answer.
	saveFilter(t, app, grid.Filter{Name: "another source", Sources: []string{"s0009"}})
	nothing := openGrid(t, app, root, "incident", 0, grid.MaxRows)
	if nothing.State != desktop.Empty || nothing.Grid == nil || nothing.Reason == "" {
		t.Fatalf("a filter that kept nothing was not reported: %+v", nothing)
	}
	if nothing.Grid.Excluded != 5 || len(nothing.Grid.Rows) != 0 {
		t.Fatalf("a filter that kept nothing did not say what it removed: %+v", nothing.Grid)
	}

	// Past the last match is an empty window with its own reason, not a claim
	// that the filter matched nothing.
	saveFilter(t, app, acknowledgements())
	past := openGrid(t, app, root, "incident", 9, 5)
	if past.State != desktop.Empty || past.Grid == nil || past.Grid.Matched != 2 || len(past.Grid.Rows) != 0 {
		t.Fatalf("a window past the last match: %+v", past)
	}
	if past.Reason == nothing.Reason {
		t.Fatal("a window past the last match reads like a filter that matched nothing")
	}
}

// The filter selection is what a person set up. It survives moving to another
// case, and it survives the window being closed and opened again, because it is
// stored rather than held in the interface.
func TestTheSelectedFilterSurvivesNavigatingToAnotherCaseAndReopeningTheShell(t *testing.T) {
	app, root, filters := gridWorkspace(t)
	saveFilter(t, app, acknowledgements())

	incident := openGrid(t, app, root, "incident", 0, grid.MaxRows)
	if incident.Grid == nil || incident.Grid.Filter != "acknowledgements" || incident.Grid.Matched != 2 {
		t.Fatalf("the selected filter was not applied to the first case: %+v", incident.Grid)
	}
	followup := openGrid(t, app, root, "followup", 0, grid.MaxRows)
	if followup.State != desktop.Empty || followup.Grid == nil {
		t.Fatalf("navigating to the second case did not keep the filter: %+v", followup)
	}
	if followup.Grid.Filter != "acknowledgements" || followup.Grid.Excluded != 1 || followup.Grid.Total != 1 {
		t.Fatalf("the filter did not follow the person to the next case: %+v", followup.Grid)
	}
	if back := openGrid(t, app, root, "incident", 0, grid.MaxRows); back.Grid.Filter != "acknowledgements" {
		t.Fatalf("navigating back lost the selection: %+v", back.Grid)
	}

	// A new window over the same viewer state reads the same selection back.
	reopened := desktop.New(&chooser{}, filepath.Join(t.TempDir(), "recent.json"), filters, filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
	listed := reopened.Filters()
	if listed.State != desktop.Completed || listed.Selected != "acknowledgements" || len(listed.Filters) != 1 {
		t.Fatalf("reopening the shell lost the saved filters: %+v", listed)
	}
	if again := openGrid(t, reopened, root, "incident", 0, grid.MaxRows); again.Grid.Matched != 2 {
		t.Fatalf("a reopened shell did not apply the selected filter: %+v", again.Grid)
	}

	// Selecting nothing shows the whole case again and excludes nothing.
	if cleared := reopened.SelectFilter(""); cleared.State != desktop.Completed || cleared.Selected != "" {
		t.Fatalf("the selection was not cleared: %+v", cleared)
	}
	whole := openGrid(t, reopened, root, "incident", 0, grid.MaxRows)
	if whole.Grid.Matched != 5 || whole.Grid.Excluded != 0 || whole.Grid.Filter != "" {
		t.Fatalf("clearing the selection did not show the whole case: %+v", whole.Grid)
	}
}

// A viewer starts with no saved filters, and that is a complete state rather
// than a failure. A name nothing saved is refused rather than silently ignored.
func TestFiltersReportAnEmptyViewerAndRefuseASelectionNothingSaved(t *testing.T) {
	app, _, _ := gridWorkspace(t)

	listed := app.Filters()
	if listed.State != desktop.Empty || len(listed.Filters) != 0 || listed.Selected != "" || listed.Reason == "" {
		t.Fatalf("a viewer who has saved nothing: %+v", listed)
	}
	if missing := app.SelectFilter("nothing saved under this name"); missing.State != desktop.Failed {
		t.Fatalf("a selection naming no saved filter was accepted: %+v", missing)
	}

	saveFilter(t, app, acknowledgements())
	replaced := acknowledgements()
	replaced.Kinds = []bundle.EventKind{bundle.Message}
	saveFilter(t, app, replaced)
	if stored := app.Filters(); len(stored.Filters) != 1 || len(stored.Filters[0].Kinds) != 1 || stored.Filters[0].Kinds[0] != bundle.Message {
		t.Fatalf("saving under an existing name did not replace exactly that filter: %+v", stored.Filters)
	}

	for name, filter := range map[string]grid.Filter{
		"an unnamed filter":           {},
		"a non-canonical selector":    {Name: "n", Fields: []grid.FieldPredicate{{Selector: "PID-3", Match: index.Contains, Term: "MRN"}}},
		"a predicate asking for both": {Name: "n", Fields: []grid.FieldPredicate{{Selector: patientField, Match: index.Contains, Term: "MRN", State: hl7.Present}}},
		"an unknown occurrence type":  {Name: "n", Kinds: []bundle.EventKind{"acknowledgement"}},
	} {
		if refused := app.SaveFilter(filter); refused.State != desktop.Failed || refused.Reason == "" {
			t.Fatalf("%s was saved: %+v", name, refused)
		}
	}
	if stored := app.Filters(); len(stored.Filters) != 1 {
		t.Fatalf("a refused filter reached the saved document: %+v", stored.Filters)
	}
}

// A saved-filter document this release cannot read is reported and left exactly
// as written: it is the person's file, nothing was derived from it, and nothing
// here migrates or repairs one.
func TestAnUnreadableSavedFilterDocumentIsReportedAndNeverReplaced(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown version": `{"schema":"readmit-filters/v2","filters":[],"selected":""}`,
		"unknown member":  `{"schema":"readmit-filters/v1","filters":[],"selected":"","sorted":true}`,
		"not JSON":        `{`,
	} {
		state := t.TempDir()
		filters := filepath.Join(state, "filters.json")
		if err := os.WriteFile(filters, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filters, filepath.Join(state, "session.json"), filepath.Join(filepath.Dir(filepath.Join(state, "session.json")), "drafts.json"))
		if listed := app.Filters(); listed.State != desktop.Failed || len(listed.Filters) != 0 || listed.Reason == "" {
			t.Fatalf("%s was read: %+v", name, listed)
		}
		if saved := app.SaveFilter(acknowledgements()); saved.State != desktop.Failed {
			t.Fatalf("a filter was saved into %s: %+v", name, saved)
		}
		if selected := app.SelectFilter(""); selected.State != desktop.Failed {
			t.Fatalf("a selection was stored into %s: %+v", name, selected)
		}
		data, err := os.ReadFile(filters)
		if err != nil || string(data) != contents {
			t.Fatalf("the shell overwrote %s: %q", name, data)
		}
	}
}

// An index is derived and disposable, and a grid never rests on one the
// evidence no longer supports. Every refusal here leaves the case exactly as it
// was: the remedy is to build the index again.
func TestTheGridRefusesAnIndexThatIsStaleDamagedExpiredOrNotAnIndex(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	before := fingerprint(t, filepath.Join(root, "incident"))

	// An index built from other evidence describes a case that is not this one.
	if stale := app.OpenGrid(root, "incident", "followup.index.json", 0, 10); stale.State != desktop.Failed || stale.Grid != nil {
		t.Fatalf("an index of other evidence answered for this case: %+v", stale)
	}

	// An index altered after it was written stands behind nothing.
	damagedRoot := t.TempDir()
	opened := writeCase(t, damagedRoot, "incident", framed(gridBooking)+framed(gridAccepted))
	writeIndex(t, damagedRoot, "incident.index.json", opened, nil)
	damaged := filepath.Join(damagedRoot, "incident.index.json")
	data, err := os.ReadFile(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(damaged, []byte(strings.Replace(string(data), `"sequence":1`, `"sequence":2`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if broken := openGrid(t, app, damagedRoot, "incident", 0, 10); broken.State != desktop.Failed || broken.Grid != nil {
		t.Fatalf("a damaged index answered: %+v", broken)
	}

	// Past the retention its own policy declared, an index serves nobody.
	expiredRoot := t.TempDir()
	ended := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	expired := writeCase(t, expiredRoot, "incident", framed(gridBooking)+framed(gridAccepted))
	writeIndex(t, expiredRoot, "incident.index.json", expired, &ended)
	if over := openGrid(t, app, expiredRoot, "incident", 0, 10); over.State != desktop.Failed || over.Reason == "" {
		t.Fatalf("an index past its retention answered: %+v", over)
	}

	// Something that is not an index at all is refused rather than guessed at.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not an index"), 0600); err != nil {
		t.Fatal(err)
	}
	if other := app.OpenGrid(root, "incident", "notes.txt", 0, 10); other.State != desktop.Failed {
		t.Fatalf("an entry that is not an index was read as one: %+v", other)
	}
	if missing := app.OpenGrid(root, "incident", "absent.index.json", 0, 10); missing.State != desktop.Failed {
		t.Fatalf("a missing index was read: %+v", missing)
	}

	// A case that is not complete, unmodified evidence is refused before any
	// index is consulted.
	if unverified := app.OpenGrid(root, "notes.txt", "incident.index.json", 0, 10); unverified.State != desktop.Failed {
		t.Fatalf("something that is not a case was verified: %+v", unverified)
	}
	if after := fingerprint(t, filepath.Join(root, "incident")); !maps.Equal(before, after) {
		t.Fatal("a refused index changed the evidence beside it")
	}
}

// A filter the index cannot answer is refused by name rather than answered from
// something narrower, and the refusal repeats neither the field nor the term.
func TestTheGridRefusesAFilterThisIndexCannotAnswer(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	saveFilter(t, app, grid.Filter{Name: "another field",
		Fields: []grid.FieldPredicate{{Selector: "PID[1]-5[1]", Match: index.Contains, Term: "DOE"}}})

	refused := openGrid(t, app, root, "incident", 0, grid.MaxRows)
	if refused.State != desktop.Failed || refused.Grid != nil || refused.Reason == "" {
		t.Fatalf("a question the index cannot answer was answered: %+v", refused)
	}
	for _, leaked := range []string{"PID", "DOE", "another field"} {
		if strings.Contains(refused.Reason, leaked) {
			t.Fatalf("the refusal repeated %q: %s", leaked, refused.Reason)
		}
	}
}

// The grid names an entry of the open workspace and a bounded window. Nothing
// outside the folder is reachable, and no window is unbounded.
func TestTheGridRefusesEntriesOutsideTheWorkspaceAndUnboundedWindows(t *testing.T) {
	app, root, _ := gridWorkspace(t)

	for name, entry := range map[string][2]string{
		"a parent directory": {"..", "incident.index.json"},
		"a nested path":      {"incident/manifest.json", "incident.index.json"},
		"an absolute path":   {root, "incident.index.json"},
		"an index outside":   {"incident", filepath.Join(root, "incident.index.json")},
		"an index above":     {"incident", "../incident.index.json"},
	} {
		if refused := app.OpenGrid(root, entry[0], entry[1], 0, 10); refused.State != desktop.Failed {
			t.Fatalf("%s was opened as a grid: %+v", name, refused)
		}
	}
	for name, window := range map[string][2]int{
		"no rows":                     {0, 0},
		"negative rows":               {0, -1},
		"unbounded rows":              {0, grid.MaxRows + 1},
		"before the first occurrence": {-1, 10},
	} {
		if refused := openGrid(t, app, root, "incident", window[0], window[1]); refused.State != desktop.Failed {
			t.Fatalf("a window with %s was rendered: %+v", name, refused)
		}
	}
	if missing := openGrid(t, app, filepath.Join(root, "absent"), "incident", 0, 10); missing.State != desktop.Failed {
		t.Fatalf("a folder that is not a workspace was opened: %+v", missing)
	}
}

// A grid displays evidence, so what crosses this boundary is positions, types
// and recorded times. No message byte, field value or original source path is
// among them.
func TestAGridCarriesNoMessageContent(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	saveFilter(t, app, grid.Filter{Name: "one patient",
		Fields: []grid.FieldPredicate{{Selector: patientField, Match: index.Contains, Term: "MRN-1"}}})

	result := openGrid(t, app, root, "incident", 0, grid.MaxRows)
	if result.State != desktop.Completed || result.Grid.Matched != 1 {
		t.Fatalf("the value filter did not answer: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"MSH", "MRN-", "DOE", "JANE", "SIU", "fixture", "CTL-"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("the grid exposed message content %q: %s", leaked, encoded)
		}
	}
}

// The grid verifies a case, so it holds the one operation slot for as long as
// it runs, and the slot is released whatever the outcome.
func TestTheGridHoldsTheSameOperationSlot(t *testing.T) {
	app, root, filters := gridWorkspace(t)

	reentrant := &chooser{folder: root}
	second := desktop.New(reentrant, filepath.Join(t.TempDir(), "recent.json"), filters, filepath.Join(t.TempDir(), "session.json"), filepath.Join(filepath.Dir(filepath.Join(t.TempDir(), "session.json")), "drafts.json"))
	var concurrent desktop.GridResult
	var saved desktop.FiltersResult
	reentrant.before = func() {
		concurrent = second.OpenGrid(root, "incident", "incident.index.json", 0, 10)
		saved = second.SaveFilter(acknowledgements())
	}
	if opened := second.SelectWorkspace(); opened.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", opened)
	}
	if concurrent.State != desktop.Busy || concurrent.Grid != nil {
		t.Fatalf("a grid ran while another operation held the facade: %+v", concurrent)
	}
	if saved.State != desktop.Busy || len(saved.Filters) != 0 {
		t.Fatalf("a filter was saved while another operation held the facade: %+v", saved)
	}
	if recovered := openGrid(t, app, root, "incident", 0, 10); recovered.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", recovered)
	}
}

// A grid runs to completion under the case reader's own limits once it starts,
// so Cancel does not interrupt it and cannot retract what it read. Cancelling
// when nothing is running does nothing, the slot is released either way, and
// the next window still opens.
func TestCancelNeverInterruptsAGridAndTheFacadeStaysUsable(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	app.Cancel("")

	var cancelling sync.WaitGroup
	cancelling.Add(1)
	go func() {
		defer cancelling.Done()
		for range 32 {
			app.Cancel("")
		}
	}()
	result := openGrid(t, app, root, "incident", 0, 10)
	cancelling.Wait()
	if result.State != desktop.Completed || result.Grid == nil {
		t.Fatalf("cancelling turned a window that runs to completion into %+v", result)
	}
	if again := openGrid(t, app, root, "incident", 0, 10); again.State != desktop.Completed {
		t.Fatalf("the facade did not stay usable after being cancelled: %+v", again)
	}
	if saved := app.SaveFilter(acknowledgements()); saved.State != desktop.Completed {
		t.Fatalf("a filter could not be saved after being cancelled: %+v", saved)
	}
}

func TestFailedFilterWriteKeepsTheAppliedSelection(t *testing.T) {
	for _, action := range []string{"select", "save", "replace"} {
		t.Run(action, func(t *testing.T) {
			app, root, filters := gridWorkspace(t)
			saveFilter(t, app, acknowledgements())
			before := app.Filters()
			if err := os.WriteFile(filters+".incomplete", []byte("interrupted"), 0600); err != nil {
				t.Fatal(err)
			}
			var result desktop.FiltersResult
			switch action {
			case "select":
				result = app.SelectFilter("")
			case "save":
				result = app.SaveFilter(grid.Filter{Name: "messages", Kinds: []bundle.EventKind{bundle.Message}})
			case "replace":
				result = app.SaveFilter(grid.Filter{Name: "acknowledgements", Kinds: []bundle.EventKind{bundle.Message}})
			}
			if result.State != desktop.Failed {
				t.Fatalf("write unexpectedly succeeded: %+v", result)
			}
			want, _ := json.Marshal(before.Filters)
			got, _ := json.Marshal(result.Filters)
			if result.Selected != before.Selected || string(got) != string(want) {
				t.Fatalf("failed write changed displayed state: %+v", result)
			}
			window := openGrid(t, app, root, "incident", 0, grid.MaxRows)
			if window.Grid == nil || window.Grid.Filter != result.Selected || window.Grid.Matched != 2 {
				t.Fatalf("display and applied filter disagree: %+v", window)
			}
		})
	}
}

func TestOversizedFiltersKeepThePersistedSelection(t *testing.T) {
	app, _, _ := gridWorkspace(t)
	saveFilter(t, app, acknowledgements())
	// Each filter is valid alone; together they exceed the document bound.
	for n := 0; n < grid.MaxFilters; n++ {
		before := app.Filters()
		filter := grid.Filter{Name: fmt.Sprintf("large-%d", n)}
		for source := 0; source < grid.MaxSources; source++ {
			filter.Sources = append(filter.Sources, fmt.Sprintf("%03d", source)+strings.Repeat("x", grid.MaxNameBytes-3))
		}
		result := app.SaveFilter(filter)
		if result.State == desktop.Failed {
			if !strings.Contains(result.Reason, "bounded document") {
				t.Fatalf("wrong refusal: %+v", result)
			}
			want, _ := json.Marshal(before.Filters)
			got, _ := json.Marshal(result.Filters)
			if result.Selected != before.Selected || string(want) != string(got) {
				t.Fatalf("encoding failure replaced persisted state: %+v", result)
			}
			return
		}
	}
	t.Fatal("fixture did not exceed the document bound")
}

// A faster directory traversal must still verify the current bytes on every
// page, including when the payload directory has been replaced since last use.
func TestNavigationReverifiesReplacedPayloads(t *testing.T) {
	for _, change := range []string{"bytes", "directory", "directory-symlink", "payload-symlink", "nested-directory", "extra-file", "missing-file"} {
		t.Run(change, func(t *testing.T) {
			app, root, _ := gridWorkspace(t)
			if got := openGrid(t, app, root, "incident", 0, 2); got.State != desktop.Completed {
				t.Fatalf("initial navigation: %+v", got)
			}
			payloads := filepath.Join(root, "incident", "payloads")
			payload := filepath.Join(payloads, "s0001-e000001.bin")
			switch change {
			case "directory", "directory-symlink":
				moved := filepath.Join(root, "previous-payloads")
				if err := os.Rename(payloads, moved); err != nil {
					t.Fatal(err)
				}
				if change == "directory-symlink" {
					if err := os.Symlink(moved, payloads); err != nil {
						if runtime.GOOS == "windows" {
							t.Skipf("symlinks unavailable: %v", err)
						}
						t.Fatal(err)
					}
				} else if err := os.Mkdir(payloads, 0700); err != nil {
					t.Fatal(err)
				}
			case "payload-symlink":
				moved := filepath.Join(root, "previous-payload.bin")
				if err := os.Rename(payload, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, payload); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("symlinks unavailable: %v", err)
					}
					t.Fatal(err)
				}
			case "nested-directory":
				if err := os.Mkdir(filepath.Join(payloads, "unexpected"), 0700); err != nil {
					t.Fatal(err)
				}
			case "extra-file":
				if err := os.WriteFile(filepath.Join(payloads, "extra.bin"), []byte("untracked"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-file":
				if err := os.Remove(payload); err != nil {
					t.Fatal(err)
				}
			case "bytes":
				if err := os.WriteFile(payload, []byte(framed(gridRebooked)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := openGrid(t, app, root, "incident", 2, 2); got.State != desktop.Failed || got.Grid != nil {
				t.Fatalf("navigation reused or followed replaced evidence: %+v", got)
			}
		})
	}
}

// Why a window or a description refuses an index.
const (
	builtFromOtherEvidence = "the index was built from different evidence than this case; build it again from this case"
	retentionEnded         = "the retention declared for this index has ended; build it again from this case, or delete it"
	notAnIndex             = "that entry is not an index this release reads; build one from this case"
	laterVersion           = "the index was written under a contract version this release cannot read; build it again from this case"
	alteredIndex           = "the index no longer matches what was written for it; the evidence is unchanged, build the index again"
)

// reopened is the case of that name as the shared reader verifies it now.
func reopened(t *testing.T, root, name string) *bundle.Bundle {
	t.Helper()
	opened, err := bundle.Open(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

// rewrite replaces one file's bytes as another program would, in place.
func rewrite(t *testing.T, path string, change func(string) string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := change(string(data))
	if changed == string(data) {
		t.Fatalf("the change left %s as it was", path)
	}
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
}

// alteredAfterSealing is an index still well formed in every member whose
// recorded build time is no longer the one its digest was taken over.
func alteredAfterSealing(document string) string {
	return strings.Replace(document, `"built_at":"2026-09-18T12:00:00Z"`, `"built_at":"2026-09-18T12:00:01Z"`, 1)
}

// A window and what it says about its index come from one read of the case:
// the details a grid carries are exactly the details DescribeIndex reports of
// the same index, in every state an index can be in, while the grid keeps its
// own state and reason. The window shows those details beside the rows, so a
// page needs no second verification of the case to show them.
func TestAWindowDescribesTheIndexItsOwnReadChecked(t *testing.T) {
	for _, tc := range []struct {
		name    string
		index   func(t *testing.T, app *desktop.App, root, filters string) string
		state   desktop.State
		reason  string
		details func(*desktop.IndexDetails) bool
	}{
		{"applicable", func(*testing.T, *desktop.App, string, string) string { return "incident.index.json" },
			desktop.Completed, "", func(d *desktop.IndexDetails) bool { return d != nil && d.Applicable }},
		{"built from other evidence", func(*testing.T, *desktop.App, string, string) string { return "followup.index.json" },
			desktop.Failed, builtFromOtherEvidence, func(d *desktop.IndexDetails) bool { return d != nil && d.Stale && !d.Applicable }},
		{"retention ended", func(t *testing.T, _ *desktop.App, root, _ string) string {
			ended := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
			return writeIndex(t, root, "ended.index.json", reopened(t, root, "incident"), &ended)
		}, desktop.Failed, retentionEnded, func(d *desktop.IndexDetails) bool { return d != nil && d.Expired && !d.Applicable }},
		{"altered after it was written", func(t *testing.T, _ *desktop.App, root, _ string) string {
			name := writeIndex(t, root, "altered.index.json", reopened(t, root, "incident"), nil)
			rewrite(t, filepath.Join(root, name), alteredAfterSealing)
			return name
		}, desktop.Failed, alteredIndex, func(d *desktop.IndexDetails) bool { return d != nil && d.Damaged }},
		{"declares the contract but is not one", func(t *testing.T, _ *desktop.App, root, _ string) string {
			name := writeIndex(t, root, "extended.index.json", reopened(t, root, "incident"), nil)
			rewrite(t, filepath.Join(root, name), func(s string) string { return strings.Replace(s, "{", `{"unexpected":true,`, 1) })
			return name
		}, desktop.Failed, alteredIndex, func(d *desktop.IndexDetails) bool { return d != nil && d.Damaged }},
		{"a later contract version", func(t *testing.T, _ *desktop.App, root, _ string) string {
			writeDocument(t, root, "later.index.json", `{"schema":"readmit-index/v999"}`)
			return "later.index.json"
		}, desktop.Failed, laterVersion, func(d *desktop.IndexDetails) bool { return d != nil && d.Unsupported }},
		{"not an index", func(t *testing.T, _ *desktop.App, root, _ string) string {
			writeDocument(t, root, "notes.txt", "not an index")
			return "notes.txt"
		}, desktop.Failed, notAnIndex, func(d *desktop.IndexDetails) bool { return d == nil }},
		{"a filter it cannot answer", func(t *testing.T, app *desktop.App, root, _ string) string {
			document, err := index.Build(context.Background(), reopened(t, root, "incident"),
				index.Policy{Fields: []string{patientField}, Retention: index.RetainStates}, indexedAt())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := index.Write(filepath.Join(root, "states.index.json"), document); err != nil {
				t.Fatal(err)
			}
			saveFilter(t, app, grid.Filter{Name: "one patient",
				Fields: []grid.FieldPredicate{{Selector: patientField, Match: index.Equals, Term: "MRN-1"}}})
			return "states.index.json"
		}, desktop.Failed, "this index retains no values", func(d *desktop.IndexDetails) bool { return d != nil && d.Applicable }},
		{"saved filters it cannot read", func(t *testing.T, _ *desktop.App, _, filters string) string {
			if err := os.WriteFile(filters, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
			return "incident.index.json"
		}, desktop.Failed, "the saved filters cannot be read", func(d *desktop.IndexDetails) bool { return d != nil && d.Applicable }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, root, filters := gridWorkspace(t)
			name := tc.index(t, app, root, filters)
			got := app.OpenGrid(root, "incident", name, 0, 10)
			if got.State != tc.state || (got.Grid != nil) != (tc.state == desktop.Completed) ||
				(tc.reason == "") != (got.Reason == "") || !strings.Contains(got.Reason, tc.reason) {
				t.Fatalf("the grid answered %s %q, want %s %q", got.State, got.Reason, tc.state, tc.reason)
			}
			if !tc.details(got.Index) {
				t.Fatalf("the grid described its index as %+v", got.Index)
			}
			if described := app.DescribeIndex(root, "incident", name); !reflect.DeepEqual(got.Index, described.Index) {
				t.Fatalf("the grid described its index as %+v, DescribeIndex as %+v", got.Index, described.Index)
			}
		})
	}
}

// Nothing one read is kept for the next. A case or an index changed between
// two reads — two pages, or the description a verified case opens with and
// the first page after it — is refused by the second, and what that read found
// is what the window is told about the index.
func TestNavigationRefusesACaseOrIndexChangedBetweenTwoPages(t *testing.T) {
	firstReads := map[string]func(t *testing.T, app *desktop.App, root string) *desktop.IndexDetails{
		"after a page": func(t *testing.T, app *desktop.App, root string) *desktop.IndexDetails {
			if page := openGrid(t, app, root, "incident", 0, 2); page.State == desktop.Completed {
				return page.Index
			}
			return nil
		},
		"after the description a case opens with": func(_ *testing.T, app *desktop.App, root string) *desktop.IndexDetails {
			if described := app.DescribeIndex(root, "incident", ""); described.State == desktop.Completed {
				return described.Index
			}
			return nil
		},
	}
	for _, tc := range []struct {
		name    string
		change  func(t *testing.T, root string)
		reason  string
		details func(*desktop.IndexDetails) bool
	}{
		{"the case replaced by other evidence", func(t *testing.T, root string) {
			if err := os.RemoveAll(filepath.Join(root, "incident")); err != nil {
				t.Fatal(err)
			}
			writeCase(t, root, "incident", framed(gridBooking)+framed(gridAccepted))
		}, builtFromOtherEvidence, func(d *desktop.IndexDetails) bool { return d != nil && d.Stale && !d.Applicable }},
		{"the case's occurrence records rewritten", func(t *testing.T, root string) {
			rewrite(t, filepath.Join(root, "incident", "events.jsonl"), func(s string) string { return s + "\n" })
		}, operation.ErrCaseUnverified.Error(), func(d *desktop.IndexDetails) bool { return d == nil }},
		{"the index altered", func(t *testing.T, root string) {
			rewrite(t, filepath.Join(root, "incident.index.json"), alteredAfterSealing)
		}, alteredIndex, func(d *desktop.IndexDetails) bool { return d != nil && d.Damaged }},
		{"the index replaced by one of other evidence", func(t *testing.T, root string) {
			other, err := os.ReadFile(filepath.Join(root, "followup.index.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "incident.index.json"), other, 0600); err != nil {
				t.Fatal(err)
			}
		}, builtFromOtherEvidence, func(d *desktop.IndexDetails) bool { return d != nil && d.Stale && !d.Applicable }},
		{"the index replaced by one whose retention has ended", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "incident.index.json")); err != nil {
				t.Fatal(err)
			}
			ended := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
			writeIndex(t, root, "incident.index.json", reopened(t, root, "incident"), &ended)
		}, retentionEnded, func(d *desktop.IndexDetails) bool { return d != nil && d.Expired && !d.Applicable }},
	} {
		for first, read := range firstReads {
			t.Run(tc.name+" "+first, func(t *testing.T) {
				app, root, _ := gridWorkspace(t)
				if details := read(t, app, root); details == nil || !details.Applicable {
					t.Fatalf("the first read described the index as %+v", details)
				}
				tc.change(t, root)
				second := openGrid(t, app, root, "incident", 2, 2)
				if second.State != desktop.Failed || second.Grid != nil || second.Reason != tc.reason {
					t.Fatalf("the second read answered %s %q with %+v, want the refusal %q", second.State, second.Reason, second.Grid, tc.reason)
				}
				if !tc.details(second.Index) {
					t.Fatalf("the second read described its index as %+v", second.Index)
				}
			})
		}
	}
}

func TestBuildIndexSuccessAndRebuild(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)

	incident := writeCase(t, root, "incident", framed(gridBooking)+framed(gridAccepted))

	// 1. Build new index with values retention
	req := desktop.BuildIndexRequest{
		Workspace:   root,
		Case:        "incident",
		Identity:    incident.Identity,
		Output:      "incident.index.json",
		Fields:      []string{patientField, grid.AckCodeSelector},
		Retention:   "values",
		RetainUntil: "indefinite",
	}
	built := app.BuildIndex(req)
	if built.State != desktop.Completed || built.Index == nil {
		t.Fatalf("build index failed: %+v", built)
	}
	if built.Index.IndexName != "incident.index.json" || !built.Index.Applicable {
		t.Fatalf("built index mismatch: %+v", built.Index)
	}
	if built.Index.Retention != "values" || built.Index.RetentionState != "active" {
		t.Fatalf("built index retention mismatch: %+v", built.Index)
	}

	// 2. Describe index
	desc := app.DescribeIndex(root, "incident", "")
	if desc.State != desktop.Completed || desc.Index == nil || !desc.Index.Applicable {
		t.Fatalf("describe index auto-select failed: %+v", desc)
	}
	if desc.Index.IndexName != "incident.index.json" {
		t.Fatalf("expected incident.index.json, got %q", desc.Index.IndexName)
	}

	// 3. Rebuild without replace returns failure
	dupe := app.BuildIndex(req)
	if dupe.State != desktop.Failed || !strings.Contains(dupe.Reason, "already exists") {
		t.Fatalf("expected already exists refusal, got %+v", dupe)
	}

	// 4. Rebuild with replace succeeds
	req.Replace = true
	req.Retention = "states"
	rebuilt := app.BuildIndex(req)
	if rebuilt.State != desktop.Completed || rebuilt.Index == nil || rebuilt.Index.Retention != "states" {
		t.Fatalf("rebuild index with replace failed: %+v", rebuilt)
	}

	// 5. Open grid with the rebuilt index
	g := app.OpenGrid(root, "incident", "incident.index.json", 0, 10)
	if g.State != desktop.Completed || g.Grid == nil {
		t.Fatalf("open grid with rebuilt index failed: %+v", g)
	}
}

// A person who starts rebuilding an index and cancels has changed their mind,
// not asked to lose the index the grid was reading. The index being replaced is
// removed only once its replacement is built, so a cancelled rebuild leaves it
// byte for byte, and the grid still opens through it.
func TestCancellingAReplacingRebuildKeepsTheIndexItReplaces(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	// Enough occurrences that the rebuild is still verifying and building
	// when a cancellation that follows the busy answer arrives.
	opened := writeCase(t, root, "incident", strings.Repeat(framed(gridBooking), 500))
	name := writeIndex(t, root, "incident.index.json", opened, nil)
	before, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	rebuild := desktop.BuildIndexRequest{Workspace: root, Case: "incident", Output: name,
		Fields: []string{patientField}, Retention: "states", RetainUntil: "indefinite", Replace: true}
	answered, holding := startHolding(t, app, root, bareState, func() desktop.State { return app.BuildIndex(rebuild).State })
	if !holding {
		t.Fatalf("the rebuild answered %s before it could be cancelled", <-answered)
	}
	app.Cancel("")
	if state := <-answered; state != desktop.Cancelled {
		t.Fatalf("a cancelled rebuild answered %s", state)
	}
	after, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || string(after) != string(before) {
		t.Fatalf("a cancelled rebuild did not leave the index it was replacing: %v", err)
	}
	if got := app.OpenGrid(root, "incident", name, 0, 10); got.State != desktop.Completed {
		t.Fatalf("the grid no longer opens after a cancelled rebuild: %+v", got)
	}
}

func TestBuildIndexValidationAndNegativePaths(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)

	writeCase(t, root, "incident", framed(gridBooking))

	// No fields
	if res := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "incident", Output: "test.index.json", Retention: "states"}); res.State != desktop.Failed {
		t.Errorf("expected failure for empty fields: %+v", res)
	}

	// Invalid selector
	if res := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "incident", Output: "test.index.json", Fields: []string{"NOT A SELECTOR"}, Retention: "states"}); res.State != desktop.Failed {
		t.Errorf("expected failure for invalid selector: %+v", res)
	}

	// Missing retention
	if res := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "incident", Output: "test.index.json", Fields: []string{patientField}}); res.State != desktop.Failed {
		t.Errorf("expected failure for missing retention: %+v", res)
	}

	// Invalid expiry
	if res := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "incident", Output: "test.index.json", Fields: []string{patientField}, Retention: "states", RetainUntil: "not-a-date"}); res.State != desktop.Failed {
		t.Errorf("expected failure for invalid expiry: %+v", res)
	}

	// Output inside evidence
	if res := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "incident", Output: "incident/leak.index.json", Fields: []string{patientField}, Retention: "states"}); res.State != desktop.Failed {
		t.Errorf("expected failure for destination inside case: %+v", res)
	}

	// Case does not exist
	if res := app.BuildIndex(desktop.BuildIndexRequest{Workspace: root, Case: "nonexistent", Output: "test.index.json", Fields: []string{patientField}, Retention: "states"}); res.State != desktop.Failed {
		t.Errorf("expected failure for nonexistent case: %+v", res)
	}
}

func TestDescribeIndexStaleExpiredDamagedUnsupported(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))

	incident := writeCase(t, root, "incident", framed(gridBooking))
	other := writeCase(t, root, "other", framed(gridRebooked))

	// Missing index
	if res := app.DescribeIndex(root, "incident", ""); res.State != desktop.Empty {
		t.Errorf("expected Empty for missing index, got %+v", res)
	}

	// Stale index (built for other case)
	writeIndex(t, root, "other.index.json", other, nil)
	if res := app.DescribeIndex(root, "incident", "other.index.json"); res.State != desktop.Failed || res.Index == nil || !res.Index.Stale {
		t.Errorf("expected Stale refusal, got %+v", res)
	}

	// Expired index
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	writeIndex(t, root, "expired.index.json", incident, &past)
	if res := app.DescribeIndex(root, "incident", "expired.index.json"); res.State != desktop.Failed || res.Index == nil || !res.Index.Expired {
		t.Errorf("expected Expired refusal, got %+v", res)
	}

	// Damaged index
	writeIndex(t, root, "damaged.index.json", incident, nil)
	damagedPath := filepath.Join(root, "damaged.index.json")
	data, err := os.ReadFile(damagedPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(damagedPath, []byte(strings.Replace(string(data), `"sequence":1`, `"sequence":2`, 1)), 0600)
	if res := app.DescribeIndex(root, "incident", "damaged.index.json"); res.State != desktop.Failed || res.Index == nil || !res.Index.Damaged {
		t.Errorf("expected Damaged refusal, got %+v", res)
	}

	// Unsupported version
	unsupportedPath := filepath.Join(root, "unsupported.index.json")
	_ = os.WriteFile(unsupportedPath, []byte(`{"schema":"readmit-index/v999"}`), 0600)
	if res := app.DescribeIndex(root, "incident", "unsupported.index.json"); res.State != desktop.Failed || res.Index == nil || !res.Index.Unsupported {
		t.Errorf("expected Unsupported refusal, got %+v", res)
	}
}

func TestRefusedQueryClarity(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	filters := filepath.Join(state, "filters.json")
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filters, filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))

	incident := writeCase(t, root, "incident", framed(gridBooking))

	// Build a states index
	policy := index.Policy{
		Fields:    []string{patientField},
		Retention: index.RetainStates,
	}
	doc, err := index.Build(context.Background(), incident, policy, indexedAt())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Write(filepath.Join(root, "incident.index.json"), doc); err != nil {
		t.Fatal(err)
	}

	// Save filter with equals on values
	saveFilter(t, app, grid.Filter{
		Name: "patient-filter",
		Fields: []grid.FieldPredicate{
			{Selector: patientField, Match: "equals", Term: "MRN-1"},
		},
	})

	// Open grid with states index and value filter should refuse with specific clarity
	got := app.OpenGrid(root, "incident", "incident.index.json", 0, 10)
	if got.State != desktop.Failed || !strings.Contains(got.Reason, "this index retains no values") {
		t.Fatalf("expected 'this index retains no values' clarity, got %+v", got)
	}
}
