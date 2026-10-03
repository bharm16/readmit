package hl7reference_test

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/hl7reference"
	"os"
	"strings"
	"testing"
)

func compositionCatalog(t *testing.T) *hl7reference.Catalog {
	t.Helper()
	a := func(v string) hl7reference.Attribute { return hl7reference.Attribute{State: "specified", Value: v} }
	absent := hl7reference.Attribute{State: "not_specified"}
	na := hl7reference.Attribute{State: "not_applicable"}
	row := func(key, kind, container string, pos int, name, datatype, section string) hl7reference.Record {
		return hl7reference.Record{Key: key, Kind: kind, Container: container, Position: pos, Name: name, Datatype: a(datatype), Optionality: absent, Length: absent, ConformanceLength: absent, Repetition: na, Item: na, Table: absent, Section: a(section), Definition: "Owned definition for " + name, Source: "owned-chapter.pdf"}
	}
	field := row("field/PV1/7", "field", "", 0, "Attending Doctor", "XCN", "3.4.3.7")
	field.Segment = "PV1"
	field.Field = 7
	field.Item = a("00137")
	field.Repetition = a("Y")
	xcn := row("datatype/XCN", "datatype", "XCN", 0, "Extended composite ID and name", "", "2.A.86")
	xcn.Datatype = na
	fn := row("datatype/FN", "datatype", "FN", 0, "Family name", "", "2.A.30")
	fn.Datatype = na
	records := []hl7reference.Record{field, xcn, fn, row("component/XCN/1", "component", "XCN", 1, "ID Number", "ST", "2.A.86.1"), row("component/XCN/2", "component", "XCN", 2, "Family Name", "FN", "2.A.86.2"), row("component/FN/1", "component", "FN", 1, "Surname", "ST", "2.A.30.1")}
	raw, err := json.Marshal(map[string]any{"schema": hl7reference.SchemaV2, "edition": "2.5.1", "sources": []hl7reference.Source{{Role: "standard", File: "owned-chapter.pdf", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Publisher: "Owned fixture"}}, "coverage": hl7reference.Coverage{Fields: 1, Datatypes: 2, Components: 3, Definitions: 6, Missing: []string{}}, "records": records})
	if err != nil {
		t.Fatal(err)
	}
	c, err := hl7reference.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestComponentDefinitionsFollowSourcedDatatypeCompositionWithoutParentAttributes(t *testing.T) {
	c := compositionCatalog(t)
	for _, want := range []struct{ path, name, section string }{{"PV1[2]-7[3].1", "ID Number", "2.A.86.1"}, {"PV1[2]-7[3].2", "Family Name", "2.A.86.2"}, {"PV1[2]-7[3].2.1", "Surname", "2.A.30.1"}} {
		selector, err := hl7.ParseSelector(want.path)
		if err != nil {
			t.Fatal(err)
		}
		got := c.LookupValue("2.5.1", selector.Parts())
		if got.Record == nil || got.Record.Name != want.name || got.Record.Section.Value != want.section || got.Record.Item.State != "not_applicable" || got.Record.Repetition.State != "not_applicable" || got.ParentField == nil || got.ParentField.Item.Value != "00137" {
			t.Fatalf("wrong distinct reference %s: %+v", want.path, got)
		}
	}
}

func TestUnknownDescendantsAndVariableTypesNeverBorrowAnotherDefinition(t *testing.T) {
	c := compositionCatalog(t)
	selector, _ := hl7.ParseSelector("PV1[2]-7[3].2.99")
	answer := c.LookupValue("2.5.1", selector.Parts())
	if answer.Record != nil || answer.Status != "not_available" || answer.DatatypeKey != "" {
		t.Fatalf("unknown subcomponent borrowed parent: %+v", answer)
	}
	variable, err := hl7reference.Decode([]byte(strings.Replace(catalog, `"datatype":{"state":"specified","value":"ST"}`, `"datatype":{"state":"specified","value":"varies"}`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := hl7.ParseSelector("ZAA[1]-1[1].1")
	got := variable.LookupValue("2.5.1", path.Parts())
	if got.Record != nil || got.TypeResolution != "unresolved" || got.DeclaredDatatype != "varies" || got.ResolvedDatatype != "" {
		t.Fatalf("guessed variable type: %+v", got)
	}
}

func TestDatatypeDrilldownIsBoundedAndKeepsIndependentEntityIdentity(t *testing.T) {
	c := compositionCatalog(t)
	got, children, total, err := c.Entity("2.5.1", "datatype/XCN", 1, 1)
	if err != nil || got.Record == nil || got.Record.Kind != "datatype" || total != 2 || len(children) != 1 || children[0].Name != "Family Name" || children[0].Position != 2 {
		t.Fatalf("wrong datatype window: %+v %+v %d %v", got, children, total, err)
	}
	if _, _, _, err = c.Entity("2.5.1", "datatype/XCN", 0, 101); err == nil {
		t.Fatal("unbounded datatype window")
	}
}

func TestOfficialV251CompositionAttributes(t *testing.T) {
	path := os.Getenv("READMIT_HL7_COMPOSITION_CATALOG")
	if path == "" {
		t.Skip("requires the explicitly selected controlled-source composition catalog")
	}
	c, err := hl7reference.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ path, name, datatype, section string }{
		{"MSH[1]-9[1].2", "Trigger Event", "ID", "2.A.44.2"},
		{"PV1[2]-7[3].1", "ID Number", "ST", "2.A.86.1"},
		{"PV1[2]-7[3].2", "Family Name", "FN", "2.A.86.2"},
		{"PV1[2]-7[3].2.1", "Surname", "ST", "2.A.30.1"},
	} {
		selector, _ := hl7.ParseSelector(want.path)
		got := c.LookupValue("2.5.1", selector.Parts())
		if got.Record == nil || got.Record.Name != want.name || got.Record.Datatype.Value != want.datatype || got.Record.Section.Value != want.section || got.Record.Item.State != "not_applicable" || got.Record.Repetition.State != "not_applicable" || got.Record.Definition == "" {
			t.Fatalf("independent composition mismatch %s: %+v", want.path, got)
		}
	}
	if c.Summary().Coverage.Datatypes != 89 || c.Summary().Coverage.Components != 431 {
		t.Fatalf("missing composition coverage: %+v", c.Summary().Coverage)
	}
}
