package desktop_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/operation"
)

const readerR4Bundle = "\n{\"resourceType\":\"Bundle\",\"type\":\"collection\",\"entry\":[{\"fullUrl\":\"urn:uuid:patient\",\"resource\":{\"resourceType\":\"Patient\",\"id\":\"17\",\"meta\":{\"versionId\":\"3\"},\"identifier\":[{\"system\":\"urn:mrn\",\"value\":\"REPEATED-PRIVATE\"},{\"system\":\"urn:mrn\",\"value\":\"REPEATED-PRIVATE\"}],\"text\":{\"status\":\"generated\",\"div\":\"<div xmlns='http://www.w3.org/1999/xhtml'><script src='https://evil.test/key'></script></div>\"}}},{\"fullUrl\":\"urn:uuid:observation\",\"resource\":{\"resourceType\":\"Observation\",\"status\":\"final\",\"code\":{\"text\":\"Synthetic result\"},\"subject\":{\"reference\":\"urn:uuid:patient\"},\"valueString\":\"CHOICE-PRIVATE\"}},{\"fullUrl\":\"urn:uuid:external\",\"resource\":{\"resourceType\":\"Observation\",\"status\":\"final\",\"code\":{\"text\":\"External\"},\"subject\":{\"reference\":\"https://evil.test/Patient/18\"}}}]}\n"

func TestR4UnrevealedViewsWithholdUnrecognizedResourceTypeValues(t *testing.T) {
	const sentinel = "PatientSyntheticPrivateSentinel"
	raw := []byte("\n{\"resourceType\":\"Bundle\",\"type\":\"collection\",\"entry\":[{\"resource\":{\"resourceType\":\"Patient\",\"active\":true}},{\"resource\":{\"resourceType\":\"" + sentinel + "\"}}]}\n")
	app := workspaceApp(t)
	root := importProject(t)
	requestContext := desktop.RequestContext{Project: root}
	input := filepath.Join(t.TempDir(), "synthetic.json")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	source := desktop.ImportRequest{Context: requestContext, Mode: "fhir-r4", Files: []string{input}, FHIR: &fhirevidence.Declaration{SourceKind: "bundle", Context: fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json"}}}
	assertHidden := func(name string, result any) {
		t.Helper()
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), sentinel) {
			t.Errorf("%s exposed an unknown source resourceType before reveal", name)
		}
	}
	preview := app.PreviewImport(source)
	if preview.State != desktop.Completed || len(preview.Rows) != 3 || preview.Rows[2].Type != "Unsupported R4 resource" {
		t.Fatalf("unsupported R4 preview: %+v", preview)
	}
	assertHidden("import preview", preview)
	previewInspection := app.InspectImportPreview(desktop.ImportInspectRequest{Context: requestContext, Source: source, PreviewToken: preview.PreviewToken, Row: 2, ByteOffset: -1})
	if previewInspection.Inspection == nil || previewInspection.Inspection.FHIR == nil {
		t.Fatalf("unsupported preview inspection: %+v", previewInspection)
	}
	assertHidden("import preview inspection", previewInspection)
	imported := app.ImportCase(desktop.ImportCaseRequest{Context: requestContext, Name: "Synthetic unsupported resources", Source: source, PreviewToken: preview.PreviewToken, IntentID: "r4-private-type-import"})
	if imported.State != desktop.Completed || imported.Case == nil {
		t.Fatalf("unsupported R4 import: %+v", imported)
	}
	assertHidden("import publication", imported)
	named := app.OpenItem(desktop.ItemRequest{Context: requestContext, Ref: *imported.Case})
	if named.Item == nil || named.Item.Summary.Case == nil {
		t.Fatalf("unsupported named case: %+v", named)
	}
	if named.Item.Summary.Case.Occurrences != nil {
		t.Fatal("FHIR resource counts must not be presented as retained HL7 occurrences")
	}
	assertHidden("named case labels", named)
	entry := named.Item.Summary.Case.Entry
	opened := app.OpenCase(root, entry)
	if opened.Case == nil || opened.Case.Schema != fhirevidence.Schema || opened.Case.Resources != 3 {
		t.Fatalf("unsupported case census: %+v", opened)
	}
	assertHidden("opened case labels", opened)
	identity := opened.Case.Identity
	resources := app.ReadMessages(desktop.MessagesRequest{Workspace: root, Case: entry, Identity: identity})
	if len(resources.Rows) != 3 || resources.Rows[2].ResourceType != "Unsupported R4 resource" || resources.Rows[2].Decoded {
		t.Fatalf("unsupported resource rows: %+v", resources)
	}
	assertHidden("resource rows and facets", resources)
	request := desktop.InspectRequest{Workspace: root, Case: entry, Identity: identity, Occurrence: "r000003", ByteOffset: -1, RawOffset: -1}
	hidden := app.InspectOccurrence(request)
	if hidden.Inspection == nil || hidden.Inspection.FHIR == nil {
		t.Fatalf("unsupported resource inspector: %+v", hidden)
	}
	assertHidden("resource inspector and picker", hidden)
	plan := desktop.FHIRVariantPlan{Parent: identity, Steps: []desktop.FHIRVariantEdit{{Occurrence: "r000002", Selector: fhirr4.Selector{Steps: []fhirr4.Step{{Field: "active"}}}, Operator: "set", Value: &dataset.Value{State: "present", Type: "boolean", Text: "false"}}}}
	variantRequest := desktop.VariantRequest{Context: requestContext, Draft: desktop.VariantDraft{Source: *imported.Case, FHIR: &plan}}
	variant := app.ResolveVariant(variantRequest)
	if variant.State != desktop.Completed || variant.Variant == nil || variant.Variant.FHIR == nil {
		t.Fatalf("unsupported retained variant: %+v", variant)
	}
	assertHidden("variant retained resources and picker", variant)
	if variant.Variant.FHIR.Resources[2].Type != "Unsupported R4 resource" || variant.Variant.FHIR.Resources[2].State == "parsed" {
		t.Error("unsupported type was presented as a supported resource")
	}
	request.Reveal = true
	shown := app.InspectOccurrence(request)
	if shown.Inspection == nil || shown.Inspection.FHIR.Resources[2].Type != sentinel || !strings.Contains(shown.Inspection.Raw, sentinel) {
		t.Fatal("explicit reveal lost the original unsupported type")
	}
	variantRequest.Reveal = true
	shownVariant := app.ResolveVariant(variantRequest)
	if shownVariant.Variant == nil || shownVariant.Variant.FHIR.Resources[2].Type != sentinel {
		t.Fatal("explicit variant reveal lost the original unsupported type")
	}
	evidence, err := fhirevidence.Open(context.Background(), filepath.Join(root, entry))
	if err != nil || !bytes.Equal(evidence.Raw(), raw) || evidence.Manifest.Schema != fhirevidence.Schema || evidence.Manifest.Declaration.Context.Version != fhirr4.Version {
		t.Fatal("privacy presentation changed retained bytes or declared schema/version")
	}
}

func importR4(t *testing.T) (*desktop.App, desktop.RequestContext, desktop.ItemRef, string, string) {
	t.Helper()
	app := workspaceApp(t)
	root := importProject(t)
	requestContext := desktop.RequestContext{Project: root}
	input := filepath.Join(t.TempDir(), "synthetic.json")
	if err := os.WriteFile(input, []byte(readerR4Bundle), 0600); err != nil {
		t.Fatal(err)
	}
	source := desktop.ImportRequest{Context: requestContext, Mode: "fhir-r4", Files: []string{input}, FHIR: &fhirevidence.Declaration{SourceKind: "bundle", Context: fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json"}}}
	preview := app.PreviewImport(source)
	if preview.State != desktop.Completed || preview.RowTotal != 4 || len(preview.Rows) != 4 || preview.Rows[1].Type != "Patient" {
		t.Fatalf("R4 preview: %+v", preview)
	}
	result := app.ImportCase(desktop.ImportCaseRequest{Context: requestContext, Name: "Synthetic R4 Bundle", Source: source, PreviewToken: preview.PreviewToken, IntentID: "r4-import"})
	if result.State != desktop.Completed || result.Case == nil {
		t.Fatalf("R4 import: %+v", result)
	}
	opened := app.OpenItem(desktop.ItemRequest{Context: requestContext, Ref: *result.Case})
	if opened.Item == nil || opened.Item.Summary.Case == nil || opened.Item.Summary.Case.Evidence != operation.EvidenceVerified {
		t.Fatalf("R4 named case: %+v", opened)
	}
	entry := opened.Item.Summary.Case.Entry
	facts, _, err := operation.VerifiedCase(root, entry)
	if err != nil {
		t.Fatal(err)
	}
	return app, requestContext, *result.Case, entry, facts.Identity
}

func TestR4CaseDoesNotOfferLegacyReplayOrLossyV2Views(t *testing.T) {
	app, requestContext, ref, entry, identity := importR4(t)
	opened := app.OpenItem(desktop.ItemRequest{Context: requestContext, Ref: ref})
	if opened.Item == nil || slices.Contains(opened.Item.Capabilities, desktop.ReplaySendAction) || slices.Contains(opened.Item.Capabilities, desktop.DeriveReviewAction) {
		t.Fatal("R4 resource evidence offered an unsupported legacy send or v2 redaction")
	}
	fields := app.MessageFields(desktop.MessageFieldsRequest{Workspace: requestContext.Project, Case: entry, Identity: identity})
	if fields.State != desktop.Failed || !strings.Contains(fields.Reason, "R4") {
		t.Fatalf("a v2-only field view did not refuse the R4 protocol explicitly: %+v", fields)
	}
}

func TestR4VariantIsReviewedAtomicDerivedEvidenceAndKeepsItsSource(t *testing.T) {
	app, requestContext, ref, entry, identity := importR4(t)
	plan := desktop.FHIRVariantPlan{Parent: identity, Steps: []desktop.FHIRVariantEdit{{Occurrence: "r000003", Selector: fhirr4.Selector{Steps: []fhirr4.Step{{Field: "valueString"}}}, Operator: "set", Value: &dataset.Value{State: "present", Type: "text", Text: "Reviewed result"}}}}
	draft := desktop.VariantDraft{Source: ref, FHIR: &plan}
	preview := app.ResolveVariant(desktop.VariantRequest{Context: requestContext, Draft: draft, Reveal: true})
	if preview.State != desktop.Completed || preview.Variant == nil || preview.Variant.FHIR == nil || len(preview.Variant.FHIR.Changes) != 1 || preview.Variant.FHIR.Changes[0].Before.Readings[0].Value.Text != "CHOICE-PRIVATE" {
		t.Fatalf("typed variant preview: %+v", preview)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: requestContext, Kind: desktop.VariantItem, Draft: desktop.ItemDraft{Name: "Reviewed R4 result", Variant: &draft}, IntentID: "r4-variant"})
	if saved.State != desktop.Completed || saved.Saved == nil || saved.Projection == nil || saved.Projection.Variant.FHIR == nil {
		t.Fatalf("typed variant Save: %+v", saved)
	}
	opened := app.OpenItem(desktop.ItemRequest{Context: requestContext, Ref: *saved.Saved})
	if opened.Item == nil || opened.Item.Summary.Variant == nil || opened.Item.Summary.Variant.Parent == nil || opened.Item.Summary.Variant.Parent.ID != ref.ID {
		t.Fatalf("R4 variant lineage: %+v", opened)
	}
	derived, err := fhirevidence.Open(context.Background(), filepath.Join(requestContext.Project, opened.Item.Summary.Variant.Entry))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(readerR4Bundle, "CHOICE-PRIVATE", "Reviewed result", 1)
	if derived.Manifest.Provenance.Parent != identity || derived.Manifest.Provenance.Mode != "derived" || !bytes.Equal(derived.Raw(), []byte(want)) {
		t.Fatal("derived revision changed undeclared bytes or lineage")
	}
	original, err := fhirevidence.Open(context.Background(), filepath.Join(requestContext.Project, entry))
	if err != nil || !bytes.Equal(original.Raw(), []byte(readerR4Bundle)) {
		t.Fatal("variant changed original evidence")
	}
	plan.Parent = strings.Repeat("a", 64)
	stale := app.ResolveVariant(desktop.VariantRequest{Context: requestContext, Draft: desktop.VariantDraft{Source: ref, FHIR: &plan}})
	if stale.State != desktop.Failed {
		t.Fatal("stale R4 plan applied to another retained source")
	}
}

func TestR4ImportAndReaderUseRetainedOriginalEvidenceWithHiddenTypedFields(t *testing.T) {
	app, requestContext, _, entry, identity := importR4(t)
	opened := app.OpenCase(requestContext.Project, entry)
	if opened.State != desktop.Completed || opened.Case == nil || opened.Case.Occurrences != 4 || opened.Case.Schema != fhirevidence.Schema {
		t.Fatalf("R4 open: %+v", opened)
	}
	read := app.ReadMessages(desktop.MessagesRequest{Workspace: requestContext.Project, Case: entry, Identity: identity})
	if read.State != desktop.Completed || len(read.Rows) != 4 || read.Rows[1].ResourceType != "Patient" || read.Rows[2].Protocol != "fhir-r4" {
		t.Fatalf("R4 resources: %+v", read)
	}
	inspection := desktop.InspectRequest{Workspace: requestContext.Project, Case: entry, Identity: identity, Occurrence: read.Rows[1].ID, Path: "identifier[].value", ByteOffset: -1, RawOffset: -1}
	hidden := app.InspectOccurrence(inspection)
	if hidden.Inspection == nil || hidden.Inspection.FHIR == nil || hidden.Inspection.FHIR.Selected == nil || hidden.Inspection.FHIR.Selected.Selection.State != "multiple" {
		t.Fatalf("hidden R4 field: %+v", hidden)
	}
	encoded, _ := json.Marshal(hidden)
	if strings.Contains(string(encoded), "REPEATED-PRIVATE") || strings.Contains(string(encoded), "https://evil.test") || strings.Contains(string(encoded), "<script") {
		t.Fatal("hidden reader exposed source values")
	}
	inspection.Reveal = true
	shown := app.InspectOccurrence(inspection)
	if shown.Inspection == nil || len(shown.Inspection.FHIR.Selected.Selection.Readings) != 2 || shown.Inspection.FHIR.Resources[1].VersionID != "3" || len(shown.Inspection.FHIR.Resources[1].Identifiers) != 2 {
		t.Fatalf("R4 fidelity: %+v", shown)
	}
	if shown.Inspection.FHIR.Selected.Field.Selector.Steps[0].Field != "identifier" || shown.Inspection.FHIR.Selected.Preset == nil || shown.Inspection.FHIR.Selected.Preset.Assertion.Operator != "sequence-equals" {
		t.Fatal("selection fabricated a v2 selector or lost repeated typed check")
	}
	inspection.Path = "identifier[1].value"
	second := app.InspectOccurrence(inspection)
	if second.Inspection == nil || second.Inspection.FHIR.Selected == nil || len(second.Inspection.FHIR.Selected.Selection.Readings) != 1 || second.Inspection.FHIR.Selected.Field.Repeated {
		t.Fatalf("a concrete repeated field was not selectable: %+v", second)
	}
	inspection.Occurrence, inspection.Path = read.Rows[2].ID, "valueString"
	observation := app.InspectOccurrence(inspection)
	if observation.Inspection == nil || observation.Inspection.FHIR.Selected.Selection.Readings[0].Value.Text != "CHOICE-PRIVATE" || len(observation.Inspection.FHIR.References) != 1 || observation.Inspection.FHIR.References[0].Resolution.Occurrences[0] != read.Rows[1].ID {
		t.Fatalf("choice/local reference: %+v", observation)
	}
	inspection.Occurrence, inspection.Path = read.Rows[3].ID, ""
	external := app.InspectOccurrence(inspection)
	if external.Inspection == nil || external.Inspection.FHIR.References[0].Resolution.State != "external" {
		t.Fatalf("external reference: %+v", external)
	}
	evidence, err := fhirevidence.Open(context.Background(), filepath.Join(requestContext.Project, entry))
	if err != nil || !bytes.Equal(evidence.Raw(), []byte(readerR4Bundle)) {
		t.Fatal("import changed original R4 bytes")
	}
}
