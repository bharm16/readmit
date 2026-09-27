package fhirr4_test

import (
	"github.com/bharm16/readmit/internal/fhirr4"
	"testing"
)

func TestFHIRKnownResourceTypeUsesFixedR4Vocabulary(t *testing.T) {
	for _, name := range []string{"Patient", "Medication", "CapabilityStatement", "DomainResource"} {
		if !fhirr4.KnownResourceType(name) {
			t.Fatal("fixed R4 type missing", name)
		}
	}
	for _, name := range []string{"MadeUpResource", "patient", "MedicationUsage", "*", ""} {
		if fhirr4.KnownResourceType(name) {
			t.Fatal("non-R4 type accepted", name)
		}
	}
}
