package hl7reference_test

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/hl7reference"
	"os"
	"testing"
)

func ownedProvenance(t *testing.T) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(catalog), &d); err != nil {
		t.Fatal(err)
	}
	d["schema"] = "readmit-hl7-reference/v5"
	sources := d["sources"].([]any)
	sources = append(sources, map[string]any{"role": "schemas", "file": "owned-schema.zip", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "publisher": "Owned schema fixture"})
	d["sources"] = sources
	for _, value := range d["records"].([]any) {
		r := value.(map[string]any)
		r["name_origin"] = map[string]any{"kind": "normative", "source": "standard", "locator": "owned-chapter.pdf"}
		r["definition_origin"] = map[string]any{"kind": "normative", "source": "standard", "locator": "owned-chapter.pdf"}
		for _, key := range []string{"datatype", "optionality", "length", "conformance_length", "repetition", "item", "table", "section"} {
			a := r[key].(map[string]any)
			o := map[string]any{"kind": "normative", "source": "standard", "locator": "owned-chapter.pdf"}
			if a["state"] == "not_applicable" {
				o = map[string]any{"kind": "not_applicable"}
			}
			a["origin"] = o
		}
	}
	field := d["records"].([]any)[1].(map[string]any)
	field["datatype"].(map[string]any)["origin"] = map[string]any{"kind": "schema", "source": "schemas", "locator": "fields.xsd#ZAA.1.Type"}
	return d
}
func provenanceBytes(t *testing.T, d map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestReferenceV5AttributesHaveIndependentOriginsAndLegacyCatalogsStayUnknown(t *testing.T) {
	d := ownedProvenance(t)
	c, err := hl7reference.Decode(provenanceBytes(t, d))
	if err != nil {
		t.Fatal(err)
	}
	row := c.Lookup("2.5.1", "ZAA", 1).Record
	if row.Datatype.Origin.Kind != "schema" || row.Datatype.Origin.Source != "schemas" || row.Length.Origin.Kind != "normative" || row.Source != "owned-chapter.pdf" {
		t.Fatalf("attribute fallback borrowed chapter provenance: %+v", row)
	}
	row.Datatype.Origin.Source = "changed"
	if c.Lookup("2.5.1", "ZAA", 1).Record.Datatype.Origin.Source != "schemas" {
		t.Fatal("answer mutation changed immutable reference origin")
	}
	old, err := hl7reference.Decode([]byte(catalog))
	if err != nil || old.Lookup("2.5.1", "ZAA", 1).Record.Datatype.Origin.Kind != "" {
		t.Fatal("legacy chapter context invented attribute origin")
	}
}
func TestReferenceV5OriginMembersAreStrictAcrossVersionsIncludingNull(t *testing.T) {
	for _, member := range []string{"name_origin", "definition_origin", "datatype.origin"} {
		d := ownedProvenance(t)
		d["schema"] = "readmit-hl7-reference/v4"
		for _, value := range d["records"].([]any) {
			r := value.(map[string]any)
			delete(r, "name_origin")
			delete(r, "definition_origin")
			for _, key := range []string{"datatype", "optionality", "length", "conformance_length", "repetition", "item", "table", "section"} {
				delete(r[key].(map[string]any), "origin")
			}
		}
		r := d["records"].([]any)[1].(map[string]any)
		if member == "datatype.origin" {
			r["datatype"].(map[string]any)["origin"] = nil
		} else {
			r[member] = nil
		}
		if _, err := hl7reference.Decode(provenanceBytes(t, d)); err == nil {
			t.Fatal("old version accepted future null origin member", member)
		}
	}
	d := ownedProvenance(t)
	r := d["records"].([]any)[1].(map[string]any)
	r["datatype"].(map[string]any)["origin"].(map[string]any)["source"] = "unlisted"
	if _, err := hl7reference.Decode(provenanceBytes(t, d)); err == nil {
		t.Fatal("unverified source role became origin")
	}
}

func TestOfficialV5ReferenceCatalogRetainsOriginsBeyondLegacyByteBound(t *testing.T) {
	path := os.Getenv("READMIT_HL7_REFERENCE_V5_LATER_CATALOG")
	if path == "" {
		t.Skip("requires explicitly selected controlled source v5 catalog")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= hl7reference.LegacyMaxBytes || len(raw) > hl7reference.MaxBytes {
		t.Fatal("independent supplied source cohort did not exercise the v5 admission bound")
	}
	c, err := hl7reference.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	row := c.Lookup("2.8.2", "MSH", 10).Record
	if row == nil || row.Length.Value != "1..199" || row.ConformanceLength.Value != "=" || row.Length.Origin.Kind != "normative" || row.Length.Origin.Source != "standard" || row.Datatype.Origin.Kind != "normative" {
		t.Fatalf("source notation/origins changed: %+v", row)
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	d["schema"] = "readmit-hl7-reference/v4"
	if _, err := hl7reference.Decode(provenanceBytes(t, d)); err == nil {
		t.Fatal("v5 size/origin meaning was accepted under legacy schema")
	}
}
