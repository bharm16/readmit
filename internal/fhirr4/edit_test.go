package fhirr4_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
)

func TestReviewedR4PrimitiveEditsPreserveUnchangedBytesAndPrimitiveCompanions(t *testing.T) {
	raw := []byte("{\n  \"resourceType\": \"Patient\", \"id\":\"17\",\"active\":true,\"_active\":{\"id\":\"metadata\"},\"identifier\":[{\"system\":\"urn:mrn\",\"value\":\"one\"},{\"system\":\"urn:mrn\",\"value\":\"one\"}]\n}\n")
	ctx := context.Background()
	doc, err := fhirr4.Decode(ctx, raw, fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json"})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := doc.EditPrimitive(ctx, "r000001", fhirr4.Selector{Steps: []fhirr4.Step{{Field: "active"}}}, "set", &dataset.Value{State: "present", Type: "boolean", Text: "false"})
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(raw, []byte("\"active\":true"), []byte("\"active\":false"), 1)
	if !bytes.Equal(changed.Raw(), want) {
		t.Fatalf("unrelated bytes changed: %s", changed.Raw())
	}
	if _, err := changed.EditPrimitive(ctx, "r000001", fhirr4.Selector{Steps: []fhirr4.Step{{Field: "active"}}}, "remove", nil); err == nil {
		t.Fatal("removal silently discarded primitive metadata")
	}
	first := 0
	changed, err = changed.EditPrimitive(ctx, "r000001", fhirr4.Selector{Steps: []fhirr4.Step{{Field: "identifier", Index: &first}, {Field: "value"}}}, "set", &dataset.Value{State: "present", Type: "text", Text: "two"})
	if err != nil {
		t.Fatal(err)
	}
	read := changed.Select(ctx, "r000001", fhirr4.Selector{Steps: []fhirr4.Step{{Field: "identifier", Each: true}, {Field: "value"}}})
	if len(read.Readings) != 2 || read.Readings[0].Value.Text != "two" || read.Readings[1].Value.Text != "one" {
		t.Fatal("repeated edit changed another identifier")
	}
}
