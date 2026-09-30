package fhirr4_test

import (
	"context"
	"testing"

	"github.com/bharm16/readmit/internal/fhirr4"
)

func TestProjectionPickersPreserveEverySelectedRepeatedIdentifier(t *testing.T) {
	var choice *fhirr4.ProjectionField
	for _, field := range fhirr4.ProjectionFields("Patient") {
		if field.ID == "identifier[].value" {
			copy := field
			choice = &copy
		}
	}
	if choice == nil || !choice.Repeated {
		t.Fatal("all identifier values are not offered as a repeated projection")
	}
	document, err := fhirr4.Decode(context.Background(), []byte(`{"resourceType":"Patient","id":"qa-patient","identifier":[{"system":"urn:one","value":"first"},{"system":"urn:two","value":"second"}]}`), fhirr4.Context{Version: "4.0.1", Base: "https://qa.invalid/fhir", MediaType: "application/fhir+json"})
	if err != nil {
		t.Fatal(err)
	}
	selected := document.Select(context.Background(), document.Resources()[0].Occurrence, choice.Selector)
	if selected.State != "multiple" || len(selected.Readings) != 2 || selected.Readings[0].Value.Text != "first" || selected.Readings[1].Value.Text != "second" {
		t.Fatalf("projected identifiers: %+v", selected)
	}
}
