package desktop_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7reference"
)

func ownedCompositionReference(t *testing.T, root string) string {
	t.Helper()
	a := func(v string) hl7reference.Attribute { return hl7reference.Attribute{State: "specified", Value: v} }
	na := hl7reference.Attribute{State: "not_applicable"}
	absent := hl7reference.Attribute{State: "not_specified"}
	row := func(key, kind, container string, position int, name, dt, section string) hl7reference.Record {
		datatype := na
		if dt != "" {
			datatype = a(dt)
		}
		return hl7reference.Record{Key: key, Kind: kind, Container: container, Position: position, Name: name, Datatype: datatype, Optionality: absent, Length: absent, ConformanceLength: absent, Repetition: na, Item: na, Table: absent, Section: a(section), Definition: "Owned reference for " + name + ".", Source: "owned-datatypes.pdf"}
	}
	field := row("field/PV1/7", "field", "", 0, "Attending Doctor", "XCN", "3.4.3.7")
	field.Segment = "PV1"
	field.Field = 7
	field.Item = a("00137")
	field.Repetition = a("Y")
	records := []hl7reference.Record{field, row("datatype/XCN", "datatype", "XCN", 0, "Extended composite identifier", "", "2.A.86"), row("datatype/FN", "datatype", "FN", 0, "Family name", "", "2.A.30"), row("component/XCN/1", "component", "XCN", 1, "ID Number", "ST", "2.A.86.1"), row("component/XCN/2", "component", "XCN", 2, "Family Name", "FN", "2.A.86.2"), row("component/FN/1", "component", "FN", 1, "Surname", "ST", "2.A.30.1")}
	raw, err := json.Marshal(map[string]any{"schema": hl7reference.SchemaV2, "edition": "2.5.1", "sources": []hl7reference.Source{{Role: "standard", File: "owned-datatypes.pdf", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Publisher: "Owned fixture"}}, "coverage": hl7reference.Coverage{Fields: 1, Datatypes: 2, Components: 3, Definitions: 6, Missing: []string{}}, "records": records})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "owned-composition.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectorDatatypeCompositionKeepsRepeatedCustomDelimiterSpans(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	catalog := ownedCompositionReference(t, root)
	message := "MSH*%~\\&*A*B*C*D*20260101**ADT%A01*id*P*2.5.1\rPV1*******123%DOE&FAMILY\rPV1*******456%ROE&SURNAME~789%SMITH&OTHER\r"
	b := writeCase(t, root, "composite", framed(message))
	got := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "composite", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PV1[2]-7[2].2.1", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: catalog})
	if got.State != desktop.Completed || got.Inspection == nil {
		t.Fatalf("read: %+v", got)
	}
	v := got.Inspection
	if v.Raw != "SMITH" || v.Decoded != "SMITH" || v.RawWindow.Selected != "SMITH" || v.Reference.Record == nil || v.Reference.Record.Name != "Surname" || v.Metadata.Label != "Surname" || v.Reference.Record.Section.Value != "2.A.30.1" || v.Reference.Record.Item.State != "not_applicable" || v.Reference.Record.Repetition.State != "not_applicable" || v.Reference.ParentField.Item.Value != "00137" {
		t.Fatalf("wrong repeated composition: %+v", v)
	}
	if len(v.ReferenceValues) != 1 || v.ReferenceValues[0].Key != "component/FN/1" || v.ReferenceValues[0].Encoded != "SMITH" || v.ReferenceValues[0].Decoded != "SMITH" || v.ReferenceValues[0].Node.Path != "PV1[2]-7[2].2.1" {
		t.Fatalf("datatype preview lost exact source occurrence: %+v", v.ReferenceValues)
	}
	hidden := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "composite", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PV1[2]-7[2].2.1", ByteOffset: -1, RawOffset: -1, ReferenceCatalog: catalog})
	if len(hidden.Inspection.ReferenceValues) != 1 || hidden.Inspection.ReferenceValues[0].Encoded != "" || hidden.Inspection.ReferenceValues[0].Decoded != "" {
		t.Fatalf("datatype preview disclosed hidden values: %+v", hidden)
	}
	multiple := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "composite", Identity: b.Identity, Occurrence: "s0001-e000001", Path: "PV1[2]-7", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: catalog})
	if len(multiple.Inspection.ReferenceValues) != 0 || multiple.Inspection.ReferenceValuesNotice != "Choose a field repetition to show component values." {
		t.Fatalf("silently chose a repeated value: %+v", multiple)
	}
	// Reduced encoding characters do not acquire invented optional delimiters.
	reduced := writeCase(t, root, "reduced-composite", framed("MSH*%~*A*B*C*D*20260101**ADT%A01*id*P*2.5.1\rPV1*******123%OWNED\r"))
	omitted := app.InspectOccurrence(desktop.InspectRequest{Workspace: root, Case: "reduced-composite", Identity: reduced.Identity, Occurrence: "s0001-e000001", Path: "PV1[1]-7[1].2.2", ByteOffset: -1, RawOffset: -1, Reveal: true, ReferenceCatalog: catalog})
	if omitted.State != desktop.Completed || omitted.Inspection.Selected.State != "omitted" || omitted.Inspection.Raw != "" || omitted.Inspection.Reference.Record != nil {
		t.Fatalf("invented omitted descendant: %+v", omitted)
	}
}

func TestHL7ReferenceDrilldownPinsCatalogAndBoundsChildren(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	catalog := ownedCompositionReference(t, root)
	summary := app.ReadReferenceCatalog(catalog)
	if summary.State != desktop.Completed {
		t.Fatalf("summary: %+v", summary)
	}
	request := desktop.HL7ReferenceRequest{Catalog: catalog, Identity: summary.Reference.Identity, Edition: "2.5.1", Key: "datatype/XCN", Offset: 1, Limit: 1}
	got := app.LookupHL7Reference(request)
	if got.State != desktop.Completed || got.Reference.Record.Container != "XCN" || len(got.Children) != 1 || got.Children[0].Name != "Family Name" || got.ChildCount != 2 {
		t.Fatalf("bounded lookup: %+v", got)
	}
	raw, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog, append(raw, byte(10)), 0600); err != nil {
		t.Fatal(err)
	}
	if current := app.ReadReferenceCatalog(catalog); current.State != desktop.Completed || current.Reference.Identity == request.Identity {
		t.Fatalf("changed valid catalog did not receive a new identity: %+v", current)
	}
	if changed := app.LookupHL7Reference(request); changed.State == desktop.Completed || changed.Reference != nil {
		t.Fatalf("changed catalog accepted: %+v", changed)
	}
}
