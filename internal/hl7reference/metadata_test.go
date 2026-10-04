package hl7reference_test

import (
	"github.com/bharm16/readmit/internal/hl7reference"
	"testing"
)

func TestTableIdentifiersRequireTheirOwnCatalogVersionAndSource(t *testing.T) {
	d := ownedProvenance(t)
	field := d["records"].([]any)[0].(map[string]any)
	table := map[string]any{}
	for key, value := range field {
		table[key] = value
	}
	table["key"] = "table/0099"
	table["kind"] = "table"
	table["segment"] = ""
	table["field"] = 0
	table["table_id"] = "0099"
	table["table_kind"] = "hl7"
	table["definition"] = ""
	table["definition_origin"] = map[string]any{"kind": "not_available"}
	table["content_state"] = "available"
	table["table_metadata"] = map[string]any{"code_system_oid": "2.16.840.1.113883.18.9999", "code_system_version": "1.0.0", "origin": map[string]any{"kind": "normative", "source": "standard", "locator": "owned-table"}}
	d["records"] = append(d["records"].([]any), table)
	d["coverage"].(map[string]any)["tables"] = 1
	if _, err := hl7reference.Decode(provenanceBytes(t, d)); err == nil {
		t.Fatal("v5 accepted later table identifiers")
	}
	d["schema"] = "readmit-hl7-reference/v6"
	catalog, err := hl7reference.Decode(provenanceBytes(t, d))
	if err != nil {
		t.Fatal(err)
	}
	got, _, _, err := catalog.Entity("2.5.1", "table/0099", 0, 100)
	if err != nil || got.Record.TableMetadata.CodeSystemOID != "2.16.840.1.113883.18.9999" {
		t.Fatalf("sourced metadata: %+v %v", got, err)
	}
}
