package hl7reference_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/hl7reference"
)

func TestReferenceHoverResolvesIndependentEntityAndKeepsFieldContext(t *testing.T) {
	c := compositionCatalog(t)
	p, answer, children, count, total, err := c.AttributePreview("2.5.1", "field/PV1/7", "datatype", 0, 1, "")
	if err != nil || p.Code != "XCN" || p.Context.Key != "field/PV1/7" || p.TargetKey != "datatype/XCN" || answer.Record.Kind != "datatype" || len(children) != 1 || count != 2 || total != 2 {
		t.Fatalf("wrong independent preview: %+v %+v %+v %d %d %v", p, answer, children, count, total, err)
	}
	if p.Definition == p.Context.Definition {
		t.Fatal("datatype preview borrowed field definition")
	}
	p, answer, _, _, _, err = c.AttributePreview("2.5.1", "component/XCN/1", "datatype", 0, 5, "")
	if err != nil || p.Context == nil || p.Code != "ST" || p.Definition != "" || answer.Record != nil || answer.Status != "not_available" {
		t.Fatalf("missing datatype borrowed component prose: %+v %+v %v", p, answer, err)
	}
	p, answer, _, _, _, err = c.AttributePreview("2.4", "field/PV1/7", "datatype", 0, 5, "")
	if err != nil || p.Context != nil || answer.Record != nil || answer.Status != "unsupported_edition" {
		t.Fatal("hover borrowed a nearby edition")
	}
}

func TestReferenceHoverAvailabilityAndBoundsAreNotInferred(t *testing.T) {
	c := compositionCatalog(t)
	p, _, _, _, _, err := c.AttributePreview("2.5.1", "field/PV1/7", "length", 0, 5, "")
	if err != nil || p.Kind != "attribute" || p.Title != "Field length not specified" || p.Code != "" || p.TargetKey != "" {
		t.Fatalf("invented a field limit: %+v %v", p, err)
	}
	p, _, _, _, _, err = c.AttributePreview("2.5.1", "field/PV1/7", "repetition", 0, 5, "")
	if err != nil || p.Code != "Y" || p.Title != "May repeat" {
		t.Fatalf("lost printed repetition: %+v %v", p, err)
	}
	p, _, _, _, _, err = c.AttributePreview("2.5.1", "component/XCN/1", "item", 0, 5, "")
	if err != nil || p.Title != "Data element not applicable" || p.TargetKey != "" {
		t.Fatal("component inherited a field item")
	}
	for _, bounds := range []struct {
		offset, limit int
		query         string
	}{{-1, 5, ""}, {0, 101, ""}, {0, 5, strings.Repeat("x", 129)}} {
		if _, _, _, _, _, err := c.AttributePreview("2.5.1", "field/PV1/7", "length", bounds.offset, bounds.limit, bounds.query); err == nil {
			t.Fatal("attribute preview bypassed request bounds")
		}
	}
	if _, _, _, _, _, err := c.AttributePreview("2.5.1", "field/PV1/7", "unknown", 0, 5, ""); err == nil {
		t.Fatal("unknown attribute accepted")
	}
}

func TestReferenceHoverDatatypeMaximumStaysSeparateFromFieldLength(t *testing.T) {
	var d map[string]any
	if err := json.Unmarshal([]byte(catalog), &d); err != nil {
		t.Fatal(err)
	}
	records := d["records"].([]any)
	field := records[1].(map[string]any)
	field["datatype"] = map[string]any{"state": "specified", "value": "ZC"}
	field["length"] = map[string]any{"state": "specified", "value": "7"}
	var datatype map[string]any
	raw, _ := json.Marshal(field)
	if err := json.Unmarshal(raw, &datatype); err != nil {
		t.Fatal(err)
	}
	datatype["key"], datatype["kind"], datatype["container"] = "datatype/ZC", "datatype", "ZC"
	datatype["segment"], datatype["field"] = "", 0
	datatype["item"], datatype["repetition"] = map[string]any{"state": "not_applicable", "value": ""}, map[string]any{"state": "not_applicable", "value": ""}
	datatype["name"] = "Owned datatype"
	datatype["definition"] = "Component table preamble\nDefinition: Owned first line.\nOwned continuation.\nMaximum Length: 20\nNote: Owned later context."
	d["schema"] = hl7reference.SchemaV2
	d["records"] = append(records, datatype)
	coverage := d["coverage"].(map[string]any)
	coverage["datatypes"], coverage["definitions"] = 1, 3
	raw, _ = json.Marshal(d)
	c, err := hl7reference.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, _, _, _, _, err := c.AttributePreview("2.5.1", "field/ZAA/1", "datatype", 0, 5, "")
	if err != nil || p.Definition != "Owned first line. Owned continuation." || p.MaximumLength != "20" || p.Context.Length.Value != "7" {
		t.Fatalf("mixed source constraints: %+v %v", p, err)
	}
	for _, heading := range []string{"2.9.51.1 Identifier (ST)", "2A.1.1 Identifier (ST)"} {
		for _, intro := range []string{"", "Owned datatype overview.", "Definition: Owned datatype overview."} {
			datatype["definition"] = strings.TrimSpace(intro + "\n" + heading + "\nDefinition: Owned child definition.\nMaximum Length: 99")
			raw, _ = json.Marshal(d)
			c, err = hl7reference.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			p, _, _, _, _, err = c.AttributePreview("2.5.1", "field/ZAA/1", "datatype", 0, 5, "")
			want := "Owned datatype overview."
			if intro == "" {
				want = ""
			}
			if err != nil || p.Definition != want || p.MaximumLength != "" {
				t.Fatalf("borrowed child introduction or maximum: %+v %v", p, err)
			}
		}
	}
}
