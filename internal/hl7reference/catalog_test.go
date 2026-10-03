package hl7reference_test

import (
	"github.com/bharm16/readmit/internal/hl7reference"
	"os"
	"strings"
	"testing"
)

// Owned source-shaped record; these expectations are independently authored.
const catalog = `{"schema":"readmit-hl7-reference/v1","edition":"2.5.1","sources":[{"role":"standard","file":"owned-chapter.pdf","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","publisher":"Owned fixture"}],"coverage":{"segments":1,"fields":1,"definitions":2,"missing":[]},"records":[{"key":"segment/ZAA","kind":"segment","segment":"ZAA","field":0,"name":"Owned segment","datatype":{"state":"not_applicable","value":""},"optionality":{"state":"not_applicable","value":""},"length":{"state":"not_applicable","value":""},"conformance_length":{"state":"not_applicable","value":""},"repetition":{"state":"not_applicable","value":""},"item":{"state":"not_applicable","value":""},"table":{"state":"not_applicable","value":""},"section":{"state":"specified","value":"3.9.1"},"definition":"Owned segment definition.","source":"owned-chapter.pdf"},{"key":"field/ZAA/1","kind":"field","segment":"ZAA","field":1,"name":"Owned appointment","datatype":{"state":"specified","value":"ST"},"optionality":{"state":"specified","value":"C"},"length":{"state":"specified","value":"1..16"},"conformance_length":{"state":"not_specified","value":""},"repetition":{"state":"specified","value":"Y/5"},"item":{"state":"specified","value":"00009"},"table":{"state":"not_specified","value":""},"section":{"state":"specified","value":"3.9.1.1"},"definition":"Required when the owned condition is met.","source":"owned-chapter.pdf"}]}`

func TestExactEditionLookupKeepsPrintedAttributesAndCoverage(t *testing.T) {
	c, err := hl7reference.Decode([]byte(catalog))
	if err != nil {
		t.Fatal(err)
	}
	got := c.Lookup("2.5.1", "ZAA", 1)
	if got.Status != "available" || got.Record == nil || got.Record.Item.Value != "00009" || got.Record.Repetition.Value != "Y/5" || got.Record.ConformanceLength.State != "not_specified" || got.Record.Section.Value != "3.9.1.1" {
		t.Fatalf("wrong reference: %+v", got)
	}
	if c.Lookup("2.5", "ZAA", 1).Status != "unsupported_edition" {
		t.Fatal("borrowed another edition")
	}
	if c.Lookup("2.5.1", "ZBB", 1).Status != "not_available" {
		t.Fatal("invented a local field")
	}
	if c.Lookup("2.5.1", "ZAA", 0).Record.Definition != "Owned segment definition." {
		t.Fatal("segment inherited a field definition")
	}
}

func TestReferenceCatalogRefusesContradictoryCoverageAndUnboundedNotices(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(catalog, `"fields":1`, `"fields":2`, 1),
		strings.Replace(catalog, `"missing":[]`, `"missing":["`+strings.Repeat("x", 1000)+`"]`, 1),
		strings.Replace(catalog, `"state":"not_specified","value":""`, `"state":"not_specified","value":"guessed"`, 1),
	} {
		if _, err := hl7reference.Decode([]byte(raw)); err == nil {
			t.Fatal("accepted contradictory or unbounded reference")
		}
	}
}

func TestOfficialV251CatalogAttributes(t *testing.T) {
	path := os.Getenv("READMIT_HL7_REFERENCE_CATALOG")
	if path == "" {
		t.Skip("requires explicitly selected local controlled-source extraction")
	}
	c, err := hl7reference.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	// Independently checked against the pinned edition's chapter attribute tables.
	for _, want := range []struct {
		segment                                                         string
		field                                                           int
		name, datatype, usage, length, item, section, repetition, table string
	}{
		{"MSH", 9, "Message Type", "MSG", "R", "15", "00009", "2.15.9.9", "", ""},
		{"PV1", 7, "Attending Doctor", "XCN", "O", "250", "00137", "3.4.3.7", "Y", "0010"},
		{"SCH", 11, "Appointment Timing Quantity", "TQ", "B", "200", "00884", "10.6.2.11", "Y", ""},
	} {
		got := c.Lookup("2.5.1", want.segment, want.field)
		if got.Record == nil {
			t.Fatalf("no record %s-%d", want.segment, want.field)
		}
		r := got.Record
		if r.Name != want.name || r.Datatype.Value != want.datatype || r.Optionality.Value != want.usage || r.Length.Value != want.length || r.Item.Value != want.item || r.Section.Value != want.section || r.ConformanceLength.State != "not_specified" || r.Repetition.Value != want.repetition || r.Table.Value != want.table || r.Definition == "" {
			t.Fatalf("independent attribute mismatch %s-%d: %+v", want.segment, want.field, r)
		}
	}
	if c.Summary().Coverage.Segments != 151 || c.Summary().Coverage.Fields != 2106 {
		t.Fatalf("partial catalog: %+v", c.Summary().Coverage)
	}
}

func TestOlderReferenceSchemasRejectFutureMembersEvenWhenEmptyOrNull(t *testing.T) {
	for _, tc := range []struct{ schema, key, value, target string }{
		{hl7reference.Schema, "container", `""`, "record"}, {hl7reference.Schema, "position", "0", "record"},
		{hl7reference.Schema, "table_id", `""`, "record"}, {hl7reference.SchemaV2, "uses", "[]", "record"},
		{hl7reference.SchemaV3, "structure_id", `""`, "record"}, {hl7reference.SchemaV3, "sequence", "[]", "record"},
		{hl7reference.SchemaV3, "message_code", "null", "record"}, {hl7reference.SchemaV2, "tables", "0", "coverage"},
		{hl7reference.Schema, "datatypes", "null", "coverage"}, {hl7reference.SchemaV3, "messages", "0", "coverage"},
	} {
		t.Run(tc.schema+"/"+tc.key+"/"+tc.value, func(t *testing.T) {
			raw := strings.Replace(catalog, hl7reference.Schema, tc.schema, 1)
			if tc.target == "record" {
				raw = strings.Replace(raw, `"key":"segment/ZAA"`, `"`+tc.key+`":`+tc.value+`,"key":"segment/ZAA"`, 1)
			} else {
				raw = strings.Replace(raw, `"coverage":{`, `"coverage":{"`+tc.key+`":`+tc.value+`,`, 1)
			}
			if _, err := hl7reference.Decode([]byte(raw)); err == nil {
				t.Fatal("version-ineligible member was accepted by zero value")
			}
		})
	}
}
