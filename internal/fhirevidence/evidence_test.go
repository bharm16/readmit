package fhirevidence_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
)

func TestRetainedR4EvidencePreservesExactBytesAndRepeatedResourceIdentity(t *testing.T) {
	raw := []byte("{\n \"resourceType\":\"Bundle\",\"type\":\"collection\",\"entry\":[{\"fullUrl\":\"urn:uuid:one\",\"resource\":{\"resourceType\":\"Patient\",\"id\":\"same\",\"identifier\":[{\"system\":\"urn:mrn\",\"value\":\"17\"},{\"system\":\"urn:mrn\",\"value\":\"17\"}]}},{\"fullUrl\":\"urn:uuid:two\",\"resource\":{\"resourceType\":\"Patient\",\"id\":\"same\"}}]}\n")
	path := filepath.Join(t.TempDir(), "evidence")
	declaration := fhirevidence.Declaration{SourceKind: "bundle", Context: fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json"}}
	written, err := fhirevidence.Create(context.Background(), path, declaration, raw, fhirevidence.Provenance{Mode: "imported"})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := fhirevidence.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened.Raw(), raw) || opened.Identity != written.Identity {
		t.Fatal("readback changed retained bytes or identity")
	}
	resources := opened.Document.Resources()
	if len(resources) != 3 || resources[1].Occurrence == resources[2].Occurrence || len(resources[1].Identifiers) != 2 {
		t.Fatalf("resource multiplicity lost: %#v", resources)
	}
	if _, err := fhirevidence.Create(context.Background(), filepath.Join(t.TempDir(), "other"), fhirevidence.Declaration{SourceKind: "resource", Context: declaration.Context}, raw, fhirevidence.Provenance{Mode: "imported"}); err == nil {
		t.Fatal("Bundle silently accepted as individual resource")
	}
	if err := os.WriteFile(filepath.Join(path, "source.json"), append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fhirevidence.Open(context.Background(), path); err == nil {
		t.Fatal("modified original bytes accepted")
	}
}

func TestRetainedR4RequestIncludesReviewedMethodAndEmptyBody(t *testing.T) {
	declaration := fhirevidence.Declaration{SourceKind: "request", Context: fhirr4.Context{Version: fhirr4.Version, Base: "https://example.test/fhir", MediaType: "application/fhir+json"}, Request: &fhirevidence.RequestDeclaration{Method: "GET", URL: "https://example.test/fhir/Patient/17"}}
	first, err := fhirevidence.Create(context.Background(), filepath.Join(t.TempDir(), "first"), declaration, nil, fhirevidence.Provenance{Mode: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	declaration.Request.URL = "https://example.test/fhir/Patient/18"
	second, err := fhirevidence.Create(context.Background(), filepath.Join(t.TempDir(), "second"), declaration, nil, fhirevidence.Provenance{Mode: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity == second.Identity || first.Document != nil || len(first.Raw()) != 0 {
		t.Fatal("request identity or empty-body state lost")
	}
}
