package desktop_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/testlicense"
)

// messagesWorkspace is one case of the grid fixture with no index beside it, a
// second case, and the viewer's state folder.
func messagesWorkspace(t *testing.T) (*desktop.App, string, string, *bundle.Bundle) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
	incident := writeInputs(t, root, "incident", []bundle.Input{{
		Path:    "fixture",
		Data:    []byte(framed(gridBooking) + framed(gridAccepted) + framed(gridRebooked) + framed(gridRejected) + framed(gridGarbage)),
		Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
		Observations: map[int]bundle.Observation{
			1: {Direction: bundle.Outbound, ObservedAt: minute(10)},
			2: {Direction: bundle.Inbound, ObservedAt: minute(11)},
			3: {Direction: bundle.Outbound, ObservedAt: minute(5)},
			4: {Direction: bundle.Inbound},
		},
	}})
	writeCase(t, root, "followup", framed(gridBooking))
	return app, root, state, incident
}

func minute(m int) *time.Time {
	instant := time.Date(2026, 1, 1, 12, m, 0, 0, time.UTC)
	return &instant
}

func readMessages(t *testing.T, app *desktop.App, root string, opened *bundle.Bundle, query grid.Query) desktop.MessagesResult {
	t.Helper()
	return app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "incident", Identity: opened.Identity, Query: query})
}

func rowIDs(rows []desktop.MessageRow) []string {
	found := []string{}
	for _, row := range rows {
		found = append(found, row.ID)
	}
	return found
}

func TestReadMessagesListsActualTypesKindsAndFacetsWithoutAnIndex(t *testing.T) {
	t.Parallel()
	app, root, state, opened := messagesWorkspace(t)
	evidence, viewer := bytesUnder(t, root), bytesUnder(t, state)

	result := readMessages(t, app, root, opened, grid.Query{})
	if result.State != desktop.Completed || result.Total != 5 || result.Matched != 5 || result.Undecodable != 1 ||
		!result.Complete || result.Scanned != 5 || len(result.Rows) != 5 {
		t.Fatalf("an unindexed case was not read: %+v", result)
	}
	want := []struct {
		kind             bundle.EventKind
		code, trigger    string
		direction        bundle.Direction
		sequence         int
		decoded, hasTime bool
	}{
		{bundle.Message, "SIU", "S12", bundle.Outbound, 1, true, true},
		{bundle.Acknowledgement, "ACK", "S12", bundle.Inbound, 2, true, true},
		{bundle.Message, "SIU", "S13", bundle.Outbound, 3, true, true},
		{bundle.Acknowledgement, "ACK", "S13", bundle.Inbound, 4, true, false},
		{bundle.Unparsed, "", "", bundle.Unknown, 5, false, false},
	}
	for i, row := range result.Rows {
		expected := want[i]
		if row.Kind != expected.kind || row.MessageCode != expected.code || row.TriggerEvent != expected.trigger ||
			row.Direction != expected.direction || row.Sequence != expected.sequence || row.Decoded != expected.decoded ||
			(row.ObservedAt != nil) != expected.hasTime || row.SourceID != "s0001" || row.Size == 0 {
			t.Fatalf("row %d is %+v", i, row)
		}
	}
	facets := desktop.MessageFacets{
		Types: []grid.MessageType{
			{Kind: bundle.Message, Code: "SIU", Trigger: "S12"},
			{Kind: bundle.Acknowledgement},
			{Kind: bundle.Message, Code: "SIU", Trigger: "S13"},
			{Kind: bundle.Unparsed},
		},
		Sources:  []desktop.SourceFacet{{ID: "s0001"}},
		AckCodes: []string{"AA", "AE"},
	}
	if !reflect.DeepEqual(result.Facets, facets) {
		t.Fatalf("facets %+v, want %+v", result.Facets, facets)
	}
	if !reflect.DeepEqual(bytesUnder(t, root), evidence) || !reflect.DeepEqual(bytesUnder(t, state), viewer) {
		t.Fatal("reading messages wrote a file")
	}
}

func TestReadMessagesAppliesATransientQueryAndWritesNothing(t *testing.T) {
	t.Parallel()
	app, root, state, opened := messagesWorkspace(t)
	evidence, viewer := bytesUnder(t, root), bytesUnder(t, state)

	for name, expected := range map[string]struct {
		query grid.Query
		rows  []string
	}{
		"field contains": {grid.Query{Fields: []grid.FieldPredicate{{Selector: patientField, Match: index.Contains, Term: "MRN-2"}}}, []string{"s0001-e000003"}},
		"field omitted":  {grid.Query{Fields: []grid.FieldPredicate{{Selector: patientField, Match: index.State, State: hl7.Omitted}}}, []string{"s0001-e000002", "s0001-e000004"}},
		"ack code":       {grid.Query{AckCodes: []string{"AE"}}, []string{"s0001-e000004"}},
		"type is not":    {grid.Query{NotTypes: []grid.MessageType{{Kind: bundle.Acknowledgement}, {Kind: bundle.Unparsed}}}, []string{"s0001-e000001", "s0001-e000003"}},
		"direction":      {grid.Query{Directions: []bundle.Direction{bundle.Inbound}}, []string{"s0001-e000002", "s0001-e000004"}},
		"content":        {grid.Query{Search: &grid.TextSearch{Scope: grid.SearchContent, Text: "ROE^RICHARD"}}, []string{"s0001-e000003"}},
		"metadata":       {grid.Query{Search: &grid.TextSearch{Scope: grid.SearchMetadata, Text: "CTL-2"}}, []string{"s0001-e000002"}},
		"filtered empty": {grid.Query{Sources: []string{"s0009"}}, []string{}},
	} {
		result := readMessages(t, app, root, opened, expected.query)
		if result.State != desktop.Completed || result.Total != 5 || !reflect.DeepEqual(rowIDs(result.Rows), expected.rows) ||
			result.Matched != len(expected.rows) {
			t.Fatalf("%s: %+v", name, result)
		}
		if len(result.Facets.Types) != 4 {
			t.Fatalf("%s: a filtered read lost the case's facets: %+v", name, result.Facets)
		}
	}
	if !reflect.DeepEqual(bytesUnder(t, root), evidence) || !reflect.DeepEqual(bytesUnder(t, state), viewer) {
		t.Fatal("applying a query wrote a file")
	}

	refused := readMessages(t, app, root, opened, grid.Query{Directions: []bundle.Direction{"sideways"}})
	if refused.State != desktop.Failed || !strings.Contains(refused.Reason, "direction") {
		t.Fatalf("an unsupported query was not refused with its reason: %+v", refused)
	}
	changed := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "incident", Identity: strings.Repeat("0", 64)})
	if changed.State != desktop.Failed || len(changed.Rows) != 0 {
		t.Fatalf("a changed identity was read: %+v", changed)
	}
}

func TestReadMessagesSortsByTimeWithUnknownLastAndPagesWithinTheBound(t *testing.T) {
	t.Parallel()
	app, root, _, opened := messagesWorkspace(t)
	sorted := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "incident", Identity: opened.Identity, Sort: grid.TimeDescending})
	if got := rowIDs(sorted.Rows); !reflect.DeepEqual(got, []string{"s0001-e000002", "s0001-e000001", "s0001-e000003", "s0001-e000004", "s0001-e000005"}) {
		t.Fatalf("descending order %v", got)
	}
	window := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "incident", Identity: opened.Identity, Offset: 1, Limit: 2})
	if got := rowIDs(window.Rows); window.State != desktop.Completed || window.Matched != 5 || !reflect.DeepEqual(got, []string{"s0001-e000002", "s0001-e000003"}) {
		t.Fatalf("window %v %+v", got, window)
	}
	for _, bad := range []desktop.MessagesRequest{{Offset: -1}, {Limit: grid.MaxRows + 1}, {Sort: "causal"}} {
		bad.Workspace, bad.Case, bad.Identity = root, "incident", opened.Identity
		if refused := app.ReadMessages(bad); refused.State != desktop.Failed {
			t.Fatalf("request %+v was accepted", bad)
		}
	}
}

// No index, a foreign index, an expired one and a corrupt one all leave the
// case readable with the same complete answer, and none is chosen by name.
// Search settings are offered only for the case's own index that has expired
// or cannot answer the applied query.
func TestReadMessagesReadsTheSameAnswerWhateverIndexLiesBesideTheCase(t *testing.T) {
	t.Parallel()
	query := grid.Query{Fields: []grid.FieldPredicate{{Selector: patientField, Match: index.Equals, Term: "MRN-1^^^READMIT^MR"}}}
	ended := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	for name, arrangement := range map[string]struct {
		arrange        func(t *testing.T, root string, opened *bundle.Bundle)
		asked, unasked desktop.SearchIndexState
	}{
		"no index": {func(*testing.T, string, *bundle.Bundle) {}, desktop.SearchIndexFine, desktop.SearchIndexFine},
		"own index": {func(t *testing.T, root string, opened *bundle.Bundle) {
			writeIndex(t, root, "incident.index.json", opened, nil)
		}, desktop.SearchIndexFine, desktop.SearchIndexFine},
		"own index retaining too little": {func(t *testing.T, root string, opened *bundle.Bundle) {
			document, err := index.Build(context.Background(), opened, index.Policy{Fields: []string{grid.AckCodeSelector}, Retention: index.RetainStates}, indexedAt())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := index.Write(filepath.Join(root, "incident.index.json"), document); err != nil {
				t.Fatal(err)
			}
		}, desktop.SearchIndexInsufficient, desktop.SearchIndexFine},
		"foreign index": {func(t *testing.T, root string, _ *bundle.Bundle) {
			followup, err := bundle.Open(filepath.Join(root, "followup"))
			if err != nil {
				t.Fatal(err)
			}
			writeIndex(t, root, "incident.index.json", followup, nil)
		}, desktop.SearchIndexFine, desktop.SearchIndexFine},
		"expired index": {func(t *testing.T, root string, opened *bundle.Bundle) {
			writeIndex(t, root, "incident.index.json", opened, &ended)
		}, desktop.SearchIndexExpired, desktop.SearchIndexExpired},
		"corrupt index": {func(t *testing.T, root string, opened *bundle.Bundle) {
			writeIndex(t, root, "incident.index.json", opened, nil)
			path := filepath.Join(root, "incident.index.json")
			data, _ := os.ReadFile(path)
			if err := os.WriteFile(path, data[:len(data)/2], 0o600); err != nil {
				t.Fatal(err)
			}
		}, desktop.SearchIndexFine, desktop.SearchIndexFine},
	} {
		app, root, state, opened := messagesWorkspace(t)
		arrangement.arrange(t, root, opened)
		evidence, viewer := bytesUnder(t, root), bytesUnder(t, state)
		result := readMessages(t, app, root, opened, query)
		if result.State != desktop.Completed || !result.Complete || result.Total != 5 ||
			!reflect.DeepEqual(rowIDs(result.Rows), []string{"s0001-e000001"}) || result.SearchIndex != arrangement.asked {
			t.Fatalf("%s: %+v", name, result)
		}
		if unasked := readMessages(t, app, root, opened, grid.Query{}); unasked.SearchIndex != arrangement.unasked {
			t.Fatalf("%s: a query that asks the index nothing offers %q", name, unasked.SearchIndex)
		}
		if !reflect.DeepEqual(bytesUnder(t, root), evidence) || !reflect.DeepEqual(bytesUnder(t, state), viewer) {
			t.Fatalf("%s: reading messages wrote, replaced or extended a file", name)
		}
	}
}

func TestReadMessagesReadsTenThousandOccurrencesInBoundedWindows(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()})
	var wire strings.Builder
	for i := range bundle.MaxEvents {
		fmt.Fprintf(&wire, "\x0bMSH|^~\\&|A|B|C|D|20260101120000||ADT^A%02d|CTL-%d|P|2.5.1\rPID|1||MRN-%d\r\x1c\r", i%10, i, i)
	}
	opened := writeCase(t, root, "large", wire.String())
	result := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "large", Identity: opened.Identity,
		Query: grid.Query{Search: &grid.TextSearch{Scope: grid.SearchContent, Text: "|CTL-9999|"}}})
	if result.State != desktop.Completed || result.Total != bundle.MaxEvents || result.Matched != 1 || !result.Complete ||
		result.Scanned != bundle.MaxEvents || len(result.Facets.Types) != 10 {
		t.Fatalf("a large case: %+v", result)
	}
	first := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "large", Identity: opened.Identity})
	if len(first.Rows) != grid.MaxRows || first.Matched != bundle.MaxEvents {
		t.Fatalf("the default window of a large case held %d rows of %d", len(first.Rows), first.Matched)
	}
}

func TestReadMessagesReportsACaseThatHoldsNoMessagesAsEmpty(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()})
	started := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	snapshot := observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: strings.Repeat("c", 32),
		Mode: observation.Fixed, Processed: []observation.Occurrence{}, Consistent: true, Records: []observation.Record{}}
	opened, err := bundle.WriteRecorded(filepath.Join(root, "quiet"), nil, started, snapshot)
	if err != nil {
		t.Fatalf("an empty recorded case: %v", err)
	}
	result := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: "quiet", Identity: opened.Identity})
	if result.State != desktop.Empty || result.Total != 0 || len(result.Rows) != 0 || result.Reason == "" {
		t.Fatalf("an empty case: %+v", result)
	}
}

// A read the window's one operation slot turns away still answers empty
// lists, as every other refusal of a message window does, so the window never
// reads rows or facets that are not there.
func TestABusyMessageWindowAnswersEmptyLists(t *testing.T) {
	t.Parallel()
	app := windowWith(t, testlicense.New(t))
	var answer desktop.MessagesResult
	desktop.RunUnderProfileForTest(app, "CheckTarget", func(context.Context) {
		answer = app.ReadMessages(desktop.MessagesRequest{})
	})
	if answer.State != desktop.Busy {
		t.Fatalf("a read while another operation runs answered %s %q", answer.State, answer.Reason)
	}
	if answer.Rows == nil || answer.Facets.Types == nil || answer.Facets.Sources == nil || answer.Facets.AckCodes == nil {
		t.Fatalf("a busy message window answered nil lists: %+v", answer)
	}
}
