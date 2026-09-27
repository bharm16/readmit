package fhirrequest_test

import (
	"github.com/bharm16/readmit/internal/fhirrequest"
	"testing"
)

func TestFHIRRequestConditionalAndBundlePermissions(t *testing.T) {
	r, err := fhirrequest.Parse("https://example.test/fhir", "PUT", "https://example.test/fhir/Patient?identifier=lab%7C123", "application/fhir+json", []byte(`{"resourceType":"Patient","active":true}`), fhirrequest.Headers{})
	if err != nil || r.Kind != "conditional-update" || len(r.Permissions) != 1 || r.Permissions[0].Interaction != "u" {
		t.Fatal(err, r)
	}
	r, err = fhirrequest.Parse("https://example.test/fhir", "POST", "https://example.test/fhir", "application/fhir+json", []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","active":true}},{"request":{"method":"GET","url":"Patient/p"}}]}`), fhirrequest.Headers{})
	if err != nil || r.Kind != "transaction" || len(r.Entries) != 2 || len(r.Permissions) != 2 {
		t.Fatal(err, r)
	}
	if _, err = fhirrequest.Parse("https://example.test/fhir", "GET", "https://example.test/fhir/Patient/$everything", "", nil, fhirrequest.Headers{}); err == nil {
		t.Fatal("arbitrary operation accepted")
	}
}

func TestFHIRRequestPatchRequiresExplicitPathEvenForRoot(t *testing.T) {
	if _, err := fhirrequest.Parse("https://example.test/fhir", "PATCH", "https://example.test/fhir/Patient/p", "application/json-patch+json", []byte(`[{"op":"replace","value":{"resourceType":"Patient","id":"p"}}]`), fhirrequest.Headers{}); err == nil {
		t.Fatal("missing path treated as root replacement")
	}
}

func TestFHIRRequestMediaTypeHonorsOnlyFixedR4Version(t *testing.T) {
	if got := fhirrequest.MediaType("application/fhir+json; fhirVersion=4.0; charset=UTF-8"); got != "application/fhir+json" {
		t.Fatal("fixed R4 media parameter refused", got)
	}
	if got := fhirrequest.MediaType("application/fhir+json; fhirVersion=5.0"); got != "" {
		t.Fatal("other FHIR version accepted")
	}
}
