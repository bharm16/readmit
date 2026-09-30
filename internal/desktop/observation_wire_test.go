package desktop_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/observesource"
)

func TestObservationLogicalWireRoundTripUsesTheSelectedAdapterWithoutWeakeningArtifactReaders(t *testing.T) {
	app, ctx := namedProject(t)
	newDraft := app.OpenItemDraft(desktop.ItemRequest{Context: ctx, Ref: desktop.ItemRef{Kind: desktop.ObservationItem}})
	if newDraft.Draft == nil {
		t.Fatal(newDraft)
	}
	// Wails serializes the Go-returned incomplete draft and then decodes the
	// edited arguments before a facade method is invoked. A direct typed stub
	// cannot exercise the source's null-as-present version boundary.
	raw, err := json.Marshal(newDraft.Draft)
	if err != nil {
		t.Fatal(err)
	}
	var reopened desktop.ItemDraft
	if err := json.Unmarshal(raw, &reopened); err != nil {
		t.Fatalf("new draft cannot return through Wails: %v", err)
	}
	if reopened.Observation.Source.Schema != observesource.SchemaV1 || reopened.Observation.Source.Capture != nil {
		t.Fatal("logical roundtrip changed legacy source semantics")
	}
	v := app.Shell().Shell.Vocabulary.Connected
	setup := v.Observation
	search := v.Search
	search.Boundary = "reference-fhir-store"
	setup.FHIR = &search
	fhir := desktop.ItemDraft{Observation: &desktop.ObservationDraft{Source: newDraft.Draft.Observation.Source, Window: newDraft.Draft.Observation.Window, Connected: &setup}}
	raw, err = json.Marshal(fhir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"capture"`) || strings.Contains(string(raw), `"window"`) {
		t.Fatal("FHIR wire carries an irrelevant frozen source/window")
	}
	if err := json.Unmarshal(raw, &reopened); err != nil || reopened.Observation.Connected.FHIR == nil {
		t.Fatalf("FHIR logical roundtrip: %v", err)
	}
	request := desktop.ObservationFieldsRequest{Context: ctx, Resource: "Appointment"}
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded desktop.ObservationFieldsRequest
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Source != nil {
		t.Fatalf("FHIR picker arguments enter the legacy source reader: %v", err)
	}
	if answer := app.ObservationFields(decoded); answer.State != desktop.Completed || len(answer.Search) == 0 {
		t.Fatalf("actual decoded picker: %+v", answer)
	}
	// The artifact reader must still reject a v1 document with v2 capture,
	// even null. Only the logical draft adapter knows these are UI placeholders.
	artifactRaw, err := json.Marshal(newDraft.Draft.Observation.Source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observesource.DecodeSource(artifactRaw); err == nil {
		t.Fatal("the frozen artifact source reader was weakened")
	}
	legacyRequest := desktop.ObservationFieldsRequest{Context: ctx, Source: &newDraft.Draft.Observation.Source, Typed: true}
	raw, err = json.Marshal(legacyRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Source == nil || decoded.Source.Schema != observesource.SchemaV1 || !decoded.Typed {
		t.Fatalf("legacy source picker roundtrip: %v", err)
	}
}
