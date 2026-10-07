package desktop_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestMessageGridKeepsExpandedSegmentsInSourceOrder(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "whole-message", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPID|1\rPV1|1|O|CLINIC^ROOM^BED^HOSP\rPID|2\rRGS|1|A\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{
		Workspace: root, Case: "whole-message", Identity: b.Identity, Occurrence: "s0001-e000001",
		Path: "PV1[1]-2", ByteOffset: -1, RawOffset: -1,
		Grid: &desktop.InspectionGridRequest{Expanded: []string{"MSH[1]", "PV1[1]"}, FollowSelection: true},
	})
	if got.State != desktop.Completed || got.Inspection == nil || got.Inspection.Grid == nil {
		t.Fatal(got)
	}
	grid := got.Inspection.Grid
	want := []string{"MSH[1]", "MSH[1]-9", "PID[1]", "PV1[1]", "PV1[1]-2", "PID[2]", "RGS[1]"}
	at := 0
	for _, row := range grid.Rows {
		if at < len(want) && row.Node.Path == want[at] {
			at++
		}
		if row.Value != "" || row.Raw != "" {
			t.Fatal("an unrevealed grid disclosed a source value")
		}
	}
	if at != len(want) {
		t.Fatalf("whole-message order lost %s: %+v", want[at], grid.Rows)
	}
	if grid.Mode != "message" || len(grid.Rows) > desktop.InspectorNodeWindow {
		t.Fatalf("grid is not a bounded message projection: %+v", grid)
	}
}

func TestMessageGridPagesLargeBranchesWithoutLosingParentContext(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "paged-grid", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rZAA|"+strings.Repeat("OWNED^", 6000)+"LAST|SIBLING\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{
		Workspace: root, Case: "paged-grid", Identity: b.Identity, Occurrence: "s0001-e000001",
		Path: "ZAA[1]-1[1].6001", ByteOffset: -1, RawOffset: -1,
		Grid: &desktop.InspectionGridRequest{FollowSelection: true},
	})
	if got.State != desktop.Completed || got.Inspection == nil || got.Inspection.Grid == nil {
		t.Fatal(got)
	}
	grid := got.Inspection.Grid
	if grid.Offset < 6000 || grid.RowCount < 6001 || len(grid.Rows) > desktop.InspectorNodeWindow {
		t.Fatalf("large branch did not produce a bounded late window: %+v", grid)
	}
	if len(got.Inspection.Parents) < 2 {
		t.Fatal("the off-page parent context was lost")
	}
	found := false
	for _, parent := range got.Inspection.Parents {
		if parent.Node.Path == "ZAA[1]-1" {
			found = true
		}
		if parent.Raw != "" || parent.Value != "" {
			t.Fatal("unrevealed parent context disclosed values")
		}
	}
	if !found {
		t.Fatal("selected component has no containing field")
	}
}

func TestMessageGridDisplayPathsKeepRepeatedPositionsUnambiguous(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "grid-paths", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPID|1||ONE~TWO\rPV1|1|O|CLINIC^ROOM^BED^HOSP\rPID|2||OTHER\r"))
	for _, sample := range []struct{ path, display string }{
		{"PID[1]-3[1]", "PID[1]-3[1]"},
		{"PID[1]-3[2]", "PID[1]-3[2]"},
		{"PV1[1]-3[1].4", "PV1-3.4"},
	} {
		got := app.InspectOccurrence(desktop.InspectRequest{
			Workspace: root, Case: "grid-paths", Identity: b.Identity, Occurrence: "s0001-e000001",
			Path: sample.path, ByteOffset: -1, RawOffset: -1,
			Grid: &desktop.InspectionGridRequest{FollowSelection: true},
		})
		if got.State != desktop.Completed || got.Inspection == nil {
			t.Fatal(got)
		}
		if got.Inspection.DisplayPath != sample.display || got.Inspection.Selected.Path != sample.path {
			t.Fatalf("display or original identity changed: %+v", got.Inspection)
		}
		for _, row := range got.Inspection.Grid.Rows {
			if row.Node.Path == sample.path && row.DisplayPath != sample.display {
				t.Fatalf("grid and Details disagree: %+v", row)
			}
		}
	}
}

func TestMessageGridPlacesAnOmittedSelectionInsideItsComposite(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	catalog := ownedParserReference(t, root)
	b := writeCase(t, root, "composite-grid", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPV1|1|O|CLINIC^ROOM^BED^HOSP||||123^NAME\rPID|1\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{
		Workspace: root, Case: "composite-grid", Identity: b.Identity, Occurrence: "s0001-e000001",
		Path: "PV1[1]-3[1].4.3", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: catalog,
		Grid: &desktop.InspectionGridRequest{FollowSelection: true},
	})
	if got.State != desktop.Completed || got.Inspection == nil || got.Inspection.Grid == nil {
		t.Fatal(got)
	}
	rows := got.Inspection.Grid.Rows
	at := -1
	for i, row := range rows {
		if row.Node.Path == "PV1[1]-3[1].4.3" {
			at = i
			if row.Depth != 3 || row.GridParent != "PV1[1]-3[1].4" || row.Node.State != "omitted" || row.Node.Start != row.Node.End || row.Raw != "" || row.Value != "" {
				t.Fatalf("omitted selection has false hierarchy or source bytes: %+v", row)
			}
		}
	}
	if at < 1 || rows[at-1].Node.Path != "PV1[1]-3[1].4.1" {
		t.Fatalf("omitted selection is not beside its transmitted sibling: %+v", rows)
	}
	if got.Inspection.RawWindow.Selected != "" {
		t.Fatal("an omitted field acquired an original-byte highlight")
	}
}

func TestMessageGridShowsReferenceOmissionsWithoutChangingEmptyOrNull(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	catalog := ownedParserReference(t, root)
	b := writeCase(t, root, "grid-states", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPV1|1|\"\"|CLINIC|\rPID|1\r"))
	request := desktop.InspectRequest{
		Workspace: root, Case: "grid-states", Identity: b.Identity, Occurrence: "s0001-e000001",
		Path: "PV1[1]", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: catalog,
		Grid: &desktop.InspectionGridRequest{FollowSelection: true},
	}
	for _, show := range []bool{false, true} {
		request.Grid.ShowOmitted = show
		got := app.InspectOccurrence(request)
		if got.State != desktop.Completed || got.Inspection == nil {
			t.Fatal(got)
		}
		states := map[string]string{}
		for _, row := range got.Inspection.Grid.Rows {
			states[row.Node.Path] = string(row.Node.State)
			if row.Node.State == "omitted" && (row.Raw != "" || row.Value != "" || row.Node.Start != row.Node.End) {
				t.Fatalf("schema omission acquired bytes: %+v", row)
			}
		}
		if states["PV1[1]-2"] != "null" || states["PV1[1]-4"] != "empty" {
			t.Fatalf("source states were rewritten: %+v", states)
		}
		if (states["PV1[1]-7"] == "omitted") != show {
			t.Fatalf("show omitted = %v did not control the absent reference field: %+v", show, states)
		}
	}
}

func TestMessageGridMaskCoversOtherExpandedSegmentsAndParentContext(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "masked-grid", framed("MSH|^~\\&|SENDER|FACILITY|RECV|FACILITY|20260101||ADT^A01|owned|P|2.5.1\rPID|1||OWNED-ID||OWNED-NAME^GIVEN\rPV1|1|O|OWNED-LOCATION^ROOM\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "masked-grid", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PID[1]-5[1].1", ByteOffset: -1, RawOffset: -1, Reveal: true, MaskPHI: true, Grid: &desktop.InspectionGridRequest{Expanded: []string{"PID[1]", "PV1[1]"}, FollowSelection: true}})
	if got.State != desktop.Completed || got.Inspection == nil {
		t.Fatal(got)
	}
	data, err := json.Marshal(got.Inspection)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"OWNED-ID", "OWNED-NAME", "GIVEN", "OWNED-LOCATION", "ROOM"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("message projection disclosed %s", secret)
		}
	}
	if !strings.Contains(string(data), "SENDER") || len(got.Inspection.Parents) == 0 {
		t.Fatal("mask lost structure or parent context")
	}
}

func TestMessageGridRejectsUnboundedAndMalformedExpansions(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "bounded-grid", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPV1|1\r"))
	for _, grid := range []desktop.InspectionGridRequest{{Offset: -1}, {Expanded: []string{"invalid/path"}}, {Expanded: make([]string, 129)}} {
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "bounded-grid", Identity: b.Identity, Occurrence: "s0001-e000001", ByteOffset: -1, RawOffset: -1, Reveal: true, Grid: &grid})
		if got.State != desktop.Failed || got.Inspection != nil {
			t.Fatalf("invalid grid returned evidence: %+v", got)
		}
	}
}

func TestMessageGridFillsABoundedViewportWindow(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "viewport-grid", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\r"+strings.Repeat("NTE|1||SYNTHETIC\r", 350)))
	for _, limit := range []int{0, 180, 1000} {
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "viewport-grid", Identity: b.Identity, Occurrence: "s0001-e000001", ByteOffset: -1, RawOffset: -1, Grid: &desktop.InspectionGridRequest{Limit: limit}})
		want := limit
		if want == 0 {
			want = desktop.InspectorNodeWindow
		}
		if want > 351 {
			want = 351
		}
		if got.State != desktop.Completed || got.Inspection == nil || got.Inspection.Grid == nil || len(got.Inspection.Grid.Rows) != want || got.Inspection.Grid.RowCount != 351 {
			t.Fatalf("limit %d did not fill its bounded viewport: %+v", limit, got)
		}
	}
	for _, limit := range []int{-1, 1001} {
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "viewport-grid", Identity: b.Identity, Occurrence: "s0001-e000001", ByteOffset: -1, RawOffset: -1, Grid: &desktop.InspectionGridRequest{Limit: limit}})
		if got.State != desktop.Failed || got.Inspection != nil {
			t.Fatalf("unbounded limit %d accepted", limit)
		}
	}
}
