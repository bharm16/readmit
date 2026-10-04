package hl7reference_test

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/hl7reference"
	"os"
	"testing"
)

func tableCatalog(t *testing.T) *hl7reference.Catalog {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(catalog), &d); err != nil {
		t.Fatal(err)
	}
	na := hl7reference.Attribute{State: "not_applicable"}
	spec := func(v string) hl7reference.Attribute { return hl7reference.Attribute{State: "specified", Value: v} }
	row := func(key, kind, name, section string) hl7reference.Record {
		return hl7reference.Record{Key: key, Kind: kind, Name: name, Datatype: na, Optionality: na, Length: na, ConformanceLength: na, Repetition: na, Item: na, Table: na, Section: spec(section), Definition: "Owned source definition.", Source: "owned-chapter.pdf", ContentState: "available"}
	}
	table := row("table/0003", "table", "Event type", "2.17.2")
	table.TableID = "0003"
	table.TableKind = "hl7"
	element := row("element/00009", "element", "Message Type", "2.15.9.9")
	element.ItemID = "00009"
	element.Uses = []string{"field/ZAA/1"}
	s12 := row(hl7reference.CodeKey("0003", "S12"), "code", "S12", "2.17.2")
	s12.TableID = "0003"
	s12.Code = "S12"
	s12.Definition = "Owned new appointment notice."
	s13 := row(hl7reference.CodeKey("0003", "S13"), "code", "S13", "2.17.2")
	s13.TableID = "0003"
	s13.Code = "S13"
	s13.Definition = "Owned appointment rescheduling notice."
	d["schema"] = hl7reference.SchemaV3
	// Read original independently authored records through JSON, then append
	// genuine catalog inputs; no lookup reply is fabricated.
	var old []hl7reference.Record
	body, _ := json.Marshal(d["records"])
	if err := json.Unmarshal(body, &old); err != nil {
		t.Fatal(err)
	}
	d["records"] = append(old, table, element, s12, s13)
	d["coverage"] = hl7reference.Coverage{Segments: 1, Fields: 1, Tables: 1, Elements: 1, Codes: 2, Definitions: 6, Missing: []string{}}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	c, err := hl7reference.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTableCodesSearchIsBoundedEditionSpecificAndRetainsLeadingZeros(t *testing.T) {
	c := tableCatalog(t)
	answer, children, matched, total, err := c.EntitySearch("2.5.1", "table/0003", 0, 1, "resched")
	if err != nil || answer.Record == nil || answer.Record.TableID != "0003" || len(children) != 1 || children[0].Code != "S13" || matched != 1 || total != 2 {
		t.Fatalf("wrong bounded code search: %+v %+v %d %d %v", answer, children, matched, total, err)
	}
	element, _, _, err := c.Entity("2.5.1", "element/00009", 0, 1)
	if err != nil || element.Record == nil || element.Record.ItemID != "00009" || element.Record.Section.Value != "2.15.9.9" {
		t.Fatalf("wrong exact element: %+v %v", element, err)
	}
	wrong, _, _, _, err := c.EntitySearch("2.5", "table/0003", 0, 1, "")
	if err != nil || wrong.Record != nil || wrong.Status != "unsupported_edition" {
		t.Fatal("borrowed another edition's codes")
	}
}

func TestOfficialV251TablesAndDataElements(t *testing.T) {
	path := os.Getenv("READMIT_HL7_TABLE_CATALOG")
	if path == "" {
		t.Skip("requires the explicitly selected controlled-source table catalog")
	}
	c, err := hl7reference.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	table, rows, matched, total, err := c.EntitySearch("2.5.1", "table/0003", 0, 100, "S13")
	if err != nil || table.Record == nil || table.Record.Section.Value != "2.17.2" || table.Record.TableKind != "hl7" || matched != 1 || total != 288 || len(rows) != 1 || rows[0].Code != "S13" || rows[0].Definition != "SIU/ACK - Notification of appointment rescheduling" {
		t.Fatalf("independent event table mismatch: %+v %+v %d %d %v", table, rows, matched, total, err)
	}
	element, _, _, err := c.Entity("2.5.1", "element/00009", 0, 100)
	if err != nil || element.Record == nil || element.Record.Name != "Message Type" || element.Record.ItemID != "00009" || element.Record.Section.Value != "2.15.9.9" || element.Record.Datatype.Value != "MSG" || element.Record.Definition == "" {
		t.Fatalf("independent element mismatch: %+v %v", element, err)
	}
}
