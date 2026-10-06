package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7reference"
)

// Independently authored composition fixture, with no normative prose.
func ownedParserReference(t *testing.T, root string) string {
	t.Helper()
	specified := func(value string) hl7reference.Attribute {
		return hl7reference.Attribute{State: "specified", Value: value}
	}
	absent := hl7reference.Attribute{State: "not_specified"}
	row := func(key, kind, name, datatype string) hl7reference.Record {
		dt := specified(datatype)
		if datatype == "" {
			dt = hl7reference.Attribute{State: "not_applicable"}
		}
		fieldOnly := hl7reference.Attribute{State: "not_applicable"}
		if kind == "field" {
			fieldOnly = absent
		}
		return hl7reference.Record{Key: key, Kind: kind, Name: name, Datatype: dt, Optionality: absent, Length: absent, ConformanceLength: absent, Repetition: fieldOnly, Item: fieldOnly, Table: absent, Section: absent, Source: "owned-parser.txt"}
	}
	records := []hl7reference.Record{}
	for _, field := range []struct {
		position       int
		name, datatype string
	}{{2, "Patient Class", "IS"}, {3, "Assigned Patient Location", "PL"}, {7, "Attending Doctor", "XCN"}} {
		r := row("field/PV1/"+string(rune('0'+field.position)), "field", field.name, field.datatype)
		r.Segment = "PV1"
		r.Field = field.position
		records = append(records, r)
	}
	for _, dt := range []string{"IS", "PL", "HD", "ST", "ID"} {
		r := row("datatype/"+dt, "datatype", dt, "")
		r.Container = dt
		records = append(records, r)
	}
	for _, child := range []struct {
		container      string
		position       int
		name, datatype string
	}{{"PL", 1, "Point of Care", "ST"}, {"PL", 2, "Room", "ST"}, {"PL", 3, "Bed", "ST"}, {"PL", 4, "Facility", "HD"}, {"PL", 5, "Location Status", "IS"}, {"PL", 6, "Person Location Type", "IS"}, {"PL", 7, "Building", "ST"}, {"PL", 8, "Floor", "ST"}, {"PL", 9, "Location Description", "ST"}, {"HD", 1, "Namespace ID", "IS"}, {"HD", 2, "Universal ID", "ST"}, {"HD", 3, "Universal ID Type", "ID"}} {
		r := row("component/"+child.container+"/"+string(rune('0'+child.position)), "component", child.name, child.datatype)
		r.Container = child.container
		r.Position = child.position
		records = append(records, r)
	}
	raw, err := json.Marshal(map[string]any{"schema": hl7reference.SchemaV2, "edition": "2.5.1", "sources": []hl7reference.Source{{Role: "standard", File: "owned-parser.txt", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Publisher: "Owned fixture"}}, "coverage": hl7reference.Coverage{Fields: 3, Datatypes: 5, Components: 12, Missing: []string{}}, "records": records})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "parser-reference.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = hl7reference.Read(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectorPrimitiveFieldDoesNotInventAComponent(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	catalog := ownedParserReference(t, root)
	b := writeCase(t, root, "primitive", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPV1|1|O|CLINIC^ROOM^BED^HOSP|||123^NAME\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "primitive", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PV1[1]-2", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: catalog})
	if got.State != desktop.Completed || got.Inspection.Grid == nil {
		t.Fatal(got)
	}
	for _, row := range got.Inspection.Grid.Rows {
		if row.Node.Path == "PV1[1]-2[1].1" {
			t.Fatalf("primitive grew a duplicate component: %+v", row)
		}
	}
}

func TestInspectorNestedOmissionsStayBesideTheirParentAndSiblings(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	catalog := ownedParserReference(t, root)
	b := writeCase(t, root, "nested", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPV1|1|O|CLINIC^ROOM^BED^HOSP||||123^NAME\r"))
	for _, path := range []string{"PV1[1]-3[1].4", "PV1[1]-3[1].4.3"} {
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "nested", Identity: b.Identity, Occurrence: "s0001-e000001", Path: path, ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: catalog})
		if got.State != desktop.Completed || got.Inspection.Grid == nil {
			t.Fatal(got)
		}
		rows := got.Inspection.Grid.Rows
		at := map[string]int{}
		for i, row := range rows {
			at[row.Node.Path] = i
		}
		for _, expected := range []string{"PV1[1]-3[1].5", "PV1[1]-3[1].9", "PV1[1]-3[1].4.2", "PV1[1]-3[1].4.3"} {
			i, ok := at[expected]
			if !ok {
				t.Fatalf("%s lost reference sibling %s: %+v", path, expected, rows)
			}
			row := rows[i]
			if row.Node.State != "omitted" || row.Raw != "" || row.Value != "" || row.Node.Start != row.Node.End {
				t.Fatalf("omitted node acquired evidence: %+v", row)
			}
		}
		want := []string{"PV1[1]-3[1].4", "PV1[1]-3[1].4.1", "PV1[1]-3[1].4.2", "PV1[1]-3[1].4.3", "PV1[1]-3[1].5", "PV1[1]-3[1].9", "PV1[1]-4", "PV1[1]-7"}
		last := -1
		for _, expected := range want {
			i, ok := at[expected]
			if !ok || i <= last {
				t.Fatalf("%s is outside its logical branch after %s: %+v", expected, path, rows)
			}
			last = i
		}
		if rows[at["PV1[1]-3[1].4.3"]].Depth != 3 {
			t.Fatal("selected omitted subcomponent has component depth")
		}
	}
}

func TestInspectorMessageIdentityAndSegmentNavigationDoNotDependOnSelection(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "identity", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned-a|P|2.5.1\rPV1|1|O|CLINIC^ROOM^BED^HOSP\r"))
	for _, path := range []string{"PV1[1]-3[1].4.3", "MSH[1]", ""} {
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "identity", Identity: b.Identity, Occurrence: "s0001-e000001", Path: path, ByteOffset: -1, RawOffset: -1, Reveal: true})
		if got.State != desktop.Completed {
			t.Fatal(got)
		}
		encoded, err := json.Marshal(got.Inspection)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err = json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if wire["control_id"] != "owned-a" {
			t.Fatalf("direct %s did not carry its message identity", path)
		}
		segments, ok := wire["segments"].([]any)
		if !ok || len(segments) != 2 {
			t.Fatalf("direct %s lost message segments", path)
		}
		if segments[1].(map[string]any)["node"].(map[string]any)["path"] != "PV1[1]" {
			t.Fatal("segment occurrence identity changed")
		}
	}
	hidden := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "identity", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PV1[1]-3", ByteOffset: -1, RawOffset: -1})
	encoded, _ := json.Marshal(hidden.Inspection)
	var wire map[string]any
	_ = json.Unmarshal(encoded, &wire)
	if _, ok := wire["control_id"]; ok {
		t.Fatal("unrevealed inspection disclosed message identity")
	}
}

func TestInspectorDirectOmittedDescendantsKeepAncestorsWithoutReferenceMetadata(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "omitted-branch", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rPV1|1|O|CLINIC^ROOM^BED^HOSP||||123^NAME\r"))
	for _, path := range []string{"PV1[1]-3[1].10.3", "PV1[1]-8[1].4.3", "PV1[1]-3[2].4.3", "PV1[1]-3[3].4.3"} {
		got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "omitted-branch", Identity: b.Identity, Occurrence: "s0001-e000001", Path: path, ByteOffset: -1, RawOffset: -1, Reveal: true})
		if got.State != desktop.Completed || got.Inspection.Grid == nil {
			t.Fatal(got)
		}
		found := false
		parentFound := false
		for _, row := range got.Inspection.Grid.Rows {
			if row.Node.Path == got.Inspection.Selected.Parent {
				parentFound = true
			}
			if row.Node.Path == path {
				found = true
				if row.Depth != 3 || row.Node.State != "omitted" || row.Raw != "" || row.Node.Start != row.Node.End {
					t.Fatalf("omitted selection fabricated hierarchy or evidence: %+v", row)
				}
			}
		}
		if !found || !parentFound {
			t.Fatalf("selected omission lost its parent: %+v", got.Inspection.Grid)
		}
	}
}

func TestInspectorLargeCompositeKeepsItsBoundedLastPage(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	b := writeCase(t, root, "large-composite", framed("MSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned|P|2.5.1\rZAA|"+strings.Repeat("OWNED^", 6000)+"LAST|SIBLING\r"))
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "large-composite", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "ZAA[1]-1[1]", NodeOffset: 6000, ByteOffset: -1, RawOffset: -1})
	if got.State != desktop.Completed || got.Inspection.ChildCount != 6001 || got.Inspection.Grid == nil {
		t.Fatal(got)
	}
	rows := got.Inspection.Grid.Rows
	if len(rows) != 5 {
		t.Fatalf("large branch did not retain a bounded last page: %d", len(rows))
	}
	if rows[3].Node.Path != "ZAA[1]-1[1].6001" || rows[3].Node.State != "present" || rows[4].Node.Path != "ZAA[1]-2" {
		t.Fatalf("last component or sibling field lost: %+v", rows)
	}
}
