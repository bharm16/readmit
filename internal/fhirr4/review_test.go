package fhirr4_test

import (
	"bytes"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
)

func TestFHIRGoverningBundleUncertaintyBlocksChildrenAndEmptyProjections(t *testing.T) {
	source := dataset.Digest([]byte("source"))
	binding := dataset.Binding{Run: "run-1", Phase: "after", Source: source, Namespace: "patients"}
	p := fhirr4.Projection{Schema: fhirr4.ProjectionSchema, ResourceType: "Patient", MaxRows: 10, MaxValues: 10, Columns: []fhirr4.Column{{Name: "id", Selector: path("id"), Required: true}}}
	for _, raw := range []string{
		`{"resourceType":"Bundle","type":"collection","implicitRules":"urn:owned:unknown-rules"}`,
		`{"resourceType":"Bundle","type":"collection","implicitRules":"urn:owned:unknown-rules","entry":[{"resource":{"resourceType":"Patient","id":"p1"}}]}`,
		`{"resourceType":"Bundle","type":"collection","entry":[{"modifierExtension":[{"url":"urn:owned:unknown","valueBoolean":true}],"resource":{"resourceType":"Patient","id":"p1"}}]}`,
		`{"resourceType":"Bundle","type":"collection","entry":[{"resource":{"resourceType":"Bundle","type":"collection","implicitRules":"urn:owned:inner-rules","entry":[{"resource":{"resourceType":"Patient","id":"p1"}}]}}]}`,
	} {
		d, err := fhirr4.Decode(t.Context(), []byte(raw), contextR4())
		if err != nil {
			t.Fatal(err)
		}
		got, err := d.Project(t.Context(), binding, p)
		if err != nil || got.Status == "complete" {
			t.Fatalf("governing uncertainty produced usable evidence: %+v %v", got, err)
		}
		for _, r := range d.MatchingResources("Patient", nil) {
			if d.Select(t.Context(), r.Occurrence, path("id")).State != "unsupported" {
				t.Fatal("child ignored governing uncertainty")
			}
		}
	}
	// Exhaust diagnostics with unsupported metadata, without inserting invalid JSON.
	var body strings.Builder
	body.WriteString(`{"resourceType":"Bundle","type":"collection"`)
	for i := 0; i < fhirr4.MaxFindings+1; i++ {
		key, _ := json.Marshal("unknown" + strings.Repeat("x", i%30) + string(rune(0x1000+i)))
		body.WriteByte(',')
		body.Write(key)
		body.WriteString(`:"opaque"`)
	}
	body.WriteByte('}')
	d, err := fhirr4.Decode(t.Context(), []byte(body.String()), contextR4())
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Project(t.Context(), binding, p)
	if err != nil || got.Status == "complete" {
		t.Fatal("finding exhaustion became passing absence", got, err)
	}
	control, err := fhirr4.Decode(t.Context(), []byte(`{"resourceType":"Bundle","type":"collection"}`), contextR4())
	if err != nil {
		t.Fatal(err)
	}
	empty, err := control.Project(t.Context(), binding, p)
	if err != nil || empty.Status != "complete" || len(empty.Rows) != 0 {
		t.Fatal("valid empty control failed", empty, err)
	}
}
func TestFHIRDatesRequireUnsignedASCIIDigitsBeforeCalendarArithmetic(t *testing.T) {
	for _, value := range []string{"+001", "2026-+1", "2026-01-+1", "-001", "2026-01-01T+1:00:00Z", "2026-01-01T12:00:00++1:00"} {
		field := "birthDate"
		if strings.Contains(value, "T") {
			field = "deceasedDateTime"
		}
		raw := []byte(`{"resourceType":"Patient","` + field + `":"` + value + `"}`)
		d, err := fhirr4.Decode(t.Context(), raw, contextR4())
		if err != nil {
			t.Fatal(err)
		}
		if d.Select(t.Context(), "r000001", path(field)).State != "invalid" {
			t.Fatalf("signed/non-date lexeme passed: %s", value)
		}
	}
	for _, value := range []string{"0001", "2026-01", "2026-01-01"} {
		d, err := fhirr4.Decode(t.Context(), []byte(`{"resourceType":"Patient","birthDate":"`+value+`"}`), contextR4())
		if err != nil || d.Select(t.Context(), "r000001", path("birthDate")).State != "present" {
			t.Fatal("valid unsigned date failed", value, err)
		}
	}
}
func TestFHIRCapabilityResourceTypesUseFixedR4Vocabulary(t *testing.T) {
	base := golden(t, "capability.json").Raw()
	for _, typ := range []string{"MadeUpResource", "ActorDefinition", "patient"} {
		raw := bytes.Replace(base, []byte(`"type":"Patient"`), []byte(`"type":"`+typ+`"`), 1)
		d, err := fhirr4.Decode(t.Context(), raw, contextR4())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Capabilities("r000001"); err == nil {
			t.Fatal("unrecognized R4 resource claim accepted", typ)
		}
	}
	// Medication is valid R4 although it has no qualified domain projection here.
	raw := bytes.Replace(base, []byte(`"type":"Patient"`), []byte(`"type":"Medication"`), 1)
	d, err := fhirr4.Decode(t.Context(), raw, contextR4())
	if err != nil {
		t.Fatal(err)
	}
	claims, err := d.Capabilities("r000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims.REST) != 1 || len(claims.REST[0].Resources) == 0 || claims.REST[0].Resources[0].Type != "Medication" {
		t.Fatal("valid R4 type outside projection subset refused", claims)
	}
}
