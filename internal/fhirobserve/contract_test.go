package fhirobserve

import (
	"testing"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
)

func observation() Observation {
	selector := func(fields ...string) *fhirr4.Selector {
		s := &fhirr4.Selector{}
		for _, f := range fields {
			s.Steps = append(s.Steps, fhirr4.Step{Field: f})
		}
		return s
	}
	return Observation{Schema: Schema, ID: "appointments", Server: "lab", Resource: "Appointment", Query: "identifier=urn%3Alab%7C{appointment-key}", Boundary: "reference-fhir-store", MaxRows: 10, MaxValues: 100, Budget: fhirrest.Budget{Pages: 2, Rows: 10, Bytes: 1 << 20, TimeoutMS: 1000}, Retry: fhirrest.Retry{MaxAttempts: 1}, Columns: []Column{
		{Name: "status", Type: "code", CodeSystem: "http://hl7.org/fhir/appointmentstatus", Key: true, Required: true, Value: Value{Kind: "field", Selector: selector("status")}},
		{Name: "patient", Type: "text", Value: Value{Kind: "reference", Selector: selector("participant", "actor", "reference")}},
		{Name: "identity", Type: "text", Value: Value{Kind: "identity"}},
	}}
}

func TestFHIRObservationContractRefusesPartialViewsAndUntypedColumns(t *testing.T) {
	if err := observation().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Observation){
		"summary view":        func(o *Observation) { o.Query = "_summary=true" },
		"elements view":       func(o *Observation) { o.Query = "_elements=status" },
		"no key":              func(o *Observation) { o.Columns[0].Key = false },
		"undeclared boundary": func(o *Observation) { o.Boundary = "ehr" },
		"code without system": func(o *Observation) { o.Columns[0].CodeSystem = "" },
		"typed reference":     func(o *Observation) { o.Columns[1].Type = "code" },
		"identity selector":   func(o *Observation) { o.Columns[2].Value.Selector = o.Columns[0].Value.Selector },
		"identity only":       func(o *Observation) { o.Columns = o.Columns[2:]; o.Columns[0].Key = true },
	} {
		o := observation()
		change(&o)
		if o.Validate() == nil {
			t.Error(name, "accepted")
		}
	}
}

func TestFHIRObservationURLResolvesOnlyCompiledValues(t *testing.T) {
	o := observation()
	got, err := o.URL("https://lab.test/fhir", map[string]string{"appointment-key": "A 1"})
	if err != nil || got != "https://lab.test/fhir/Appointment?identifier=urn%3Alab%7CA+1" {
		t.Fatal(got, err)
	}
	if _, err := o.URL("https://lab.test/fhir", map[string]string{}); err == nil {
		t.Fatal("an unresolved placeholder produced an address")
	}
	if _, err := o.URL("https://lab.test/fhir", map[string]string{"appointment-key": "{other}"}); err == nil {
		t.Fatal("a value introduced a new placeholder")
	}
}

// A reference is logical identity only when it is unambiguous under the one
// declared base; anything else is unsupported, never guessed or fetched.
func TestFHIRObservationReferencesAndTypesAreNeverGuessed(t *testing.T) {
	base := "https://lab.test/fhir"
	text := func(v string) dataset.Value { return dataset.Value{State: "present", Type: "text", Text: v} }
	for in, want := range map[string]string{"Patient/p1": "Patient/p1", base + "/Patient/p1": "Patient/p1", "Patient/p1/_history/3": "Patient/p1"} {
		if got := reference(text(in), base); got.State != "present" || got.Text != want {
			t.Error(in, got)
		}
	}
	for _, in := range []string{"#contained", "urn:uuid:0f7c8a5e-7d6b-4b1a-9c1e-2b8f7a6d5c4e", "https://other.test/fhir/Patient/p1", "Patient"} {
		if got := reference(text(in), base); got.State != "unsupported" {
			t.Error(in, got)
		}
	}
	status := observation().Columns[0]
	if v, ok := typed(status, dataset.Value{State: "present", Type: "code", Text: "booked"}); !ok || v.CodeSystem != status.CodeSystem {
		t.Fatal("a code did not take its declared binding system", v)
	}
	if _, ok := typed(status, dataset.Value{State: "present", Type: "text", Text: "booked"}); ok {
		t.Fatal("a text value was converted to a declared code")
	}
	if _, ok := typed(status, dataset.Value{State: "present", Type: "code", Text: "booked", CodeSystem: "urn:other"}); ok {
		t.Fatal("a code from another system was accepted")
	}
}
