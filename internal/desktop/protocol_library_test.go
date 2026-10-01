package desktop_test

import (
	"bytes"
	stdcontext "context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/scenariogen"
)

func TestFHIRScenarioBoundJSONRoundTripKeepsOnlyGenuinelyEmptyV2Carriers(t *testing.T) {
	app, context := namedProject(t)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}, Protocol: "fhir-r4"})
	draft := *opened.Draft
	draft.Name = "Bound FHIR scenario"
	draft.Scenario.FHIR.Steps = []desktop.FHIRScenarioStep{{ID: "patient", After: "0s", SourceKind: "resource", Document: `{"resourceType":"Patient","id":"synthetic"}`}}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip desktop.ItemDraft
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	// The JSON value null and explicit empty JS arrays carry no v2 clause.
	roundTrip.Scenario.Plan.Rows = []scenariogen.Row{}
	roundTrip.Scenario.Plan.Variants = []scenariogen.Variant{}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: roundTrip, IntentID: "bound-fhir-save"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("the bound empty carrier prevented an authored FHIR save: %+v", saved)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Draft.Scenario.FHIR.Steps, draft.Scenario.FHIR.Steps) {
		t.Fatalf("the authored FHIR clauses changed on round trip: %+v", reopened)
	}
	before := entries(t, context.Project)
	roundTrip.Scenario.Plan.Template = jsontext.Value(`{"retained_v2_clause":true}`)
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: roundTrip, IntentID: "bound-fhir-real-v2-clause"})
	if refused.Outcome == desktop.SavedOutcome || !reflect.DeepEqual(before, entries(t, context.Project)) ||
		!reflect.DeepEqual(roundTrip.Scenario.FHIR.Steps, draft.Scenario.FHIR.Steps) {
		t.Fatalf("a real v2 clause was discarded to save FHIR: %+v", refused)
	}
}

func TestLibrarySavesGenerationForEverySupportedV2Family(t *testing.T) {
	for _, tc := range []struct {
		family   string
		messages int
	}{{"ADT", 14}, {"SIU", 9}, {"ORM", 6}, {"ORU", 6}} {
		t.Run(tc.family, func(t *testing.T) {
			app, context := namedProject(t)
			family := strings.ToLower(tc.family)
			writeDocument(t, context.Project, "pack.json", string(casegenFixture(t, "owned/pack.json")))
			packs := app.MetadataPacks(context)
			if packs.State != desktop.Completed || len(packs.Packs) != 1 {
				t.Fatalf("pack: %+v", packs)
			}
			profile, err := localprofile.Decode(casegenFixture(t, "owned/profile-"+family+".json"))
			if err != nil {
				t.Fatal(err)
			}
			published := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, IntentID: "profile",
				Draft: desktop.ItemDraft{Name: tc.family + " fixture contract", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &packs.Packs[0].Item.Ref}}})
			if published.Outcome != desktop.SavedOutcome {
				t.Fatalf("profile save: %+v", published)
			}
			var request casegen.Request
			if err := json.Unmarshal(casegenFixture(t, "request-"+family+".json"), &request); err != nil {
				t.Fatal(err)
			}
			plan := scenariogen.Plan{Schema: scenariogen.Schema, GeneratorVersion: scenariogen.Version, Seed: request.Seed, Template: request.Scenario,
				Rows:     []scenariogen.Row{{ID: "plain", PatientName: "SYNTHETIC", Notes: request.Rows[0].Notes, Encoding: "utf-8"}},
				Variants: []scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}}}
			settings := &desktop.CaseGenerationSettings{Wire: request.Wire, Bindings: request.Bindings, Variants: []casegen.Variant{}}
			saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, IntentID: "scenario",
				Draft: desktop.ItemDraft{Name: tc.family + " lifecycle", Scenario: &desktop.ScenarioDraft{Plan: plan, Profile: published.Saved, Generation: settings}}})
			if saved.Outcome != desktop.SavedOutcome {
				t.Fatalf("scenario save: %+v", saved)
			}
			reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
			if reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Draft.Scenario.Generation, settings) {
				t.Fatalf("reopen: %+v", reopened)
			}
			if tc.family == "ORM" || tc.family == "ORU" {
				if reopened.Draft.Scenario.Template.Schema != "readmit-order-scenario/v1" || len(reopened.Draft.Scenario.Orders) != 1 || reopened.Draft.Scenario.Results == nil {
					t.Fatalf("order/result clauses were dropped from the initiated editor: %+v", reopened.Draft.Scenario)
				}
			}
			generated := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: *saved.Saved, IntentID: "actual-bytes"})
			if generated.State != desktop.Completed || generated.Family != tc.family || len(generated.Cases) != 1 ||
				generated.Cases[0].Messages != tc.messages || len(generated.Cases[0].Phases) != tc.messages || !generated.Cases[0].Evaluated || generated.Cases[0].Verdict == "fail" {
				t.Fatalf("saved %s settings did not generate actual mapped bytes under the shared evaluator: %+v", tc.family, generated)
			}
			openedCase := app.OpenCase(context.Project, generated.Cases[0].Entry)
			if openedCase.State != desktop.Completed || openedCase.Case == nil || openedCase.Case.Occurrences != tc.messages {
				t.Fatalf("generated case cannot be read: %+v", openedCase)
			}
		})
	}
}

func TestFHIRProfileVersionCannotStandForTwoDifferentSavedDocuments(t *testing.T) {
	app, context := namedProject(t)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProfileItem}, Protocol: "fhir-r4"})
	draft := *opened.Draft
	draft.Name = "Pinned site profile"
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: draft, IntentID: "site-profile"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("first profile: %+v", saved)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	copy := *reopened.Draft
	copy.Name = "Conflicting copy"
	copy.Profile.FHIR.ResourceType = "Observation"
	before := entries(t, context.Project)
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: copy, IntentID: "conflicting-profile-copy"})
	if refused.Outcome == desktop.SavedOutcome || !reflect.DeepEqual(before, entries(t, context.Project)) {
		t.Fatalf("one FHIR profile version stood for two meanings: %+v", refused)
	}
}

func TestFHIRFieldCheckGroupKeepsTypedProjectionAndBlocksUnknownClauses(t *testing.T) {
	app, context, _, entry, identity := importR4(t)
	read := app.ReadMessages(desktop.MessagesRequest{Workspace: context.Project, Case: entry, Identity: identity})
	inspected := app.InspectOccurrence(desktop.InspectRequest{Workspace: context.Project, Case: entry, Identity: identity,
		Occurrence: read.Rows[1].ID, Path: "identifier[].value", Reveal: true, ByteOffset: -1, RawOffset: -1})
	preset := inspected.Inspection.FHIR.Selected.Preset
	draft := desktop.ItemDraft{Name: "Repeated patient identifiers", CheckGroup: &desktop.CheckGroupDraft{
		FHIR: &desktop.FHIRCheckGroup{Schema: desktop.FHIRCheckGroupSchema, Name: "Repeated patient identifiers", Projections: []fhirr4.Projection{preset.Projection},
			Set: assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{preset.Binding}, Assertions: []assertion.DatasetAssertion{preset.Assertion}}}}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: draft, IntentID: "fhir-check-group"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save typed FHIR group: %+v", saved)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Draft.CheckGroup.FHIR, draft.CheckGroup.FHIR) {
		t.Fatalf("the reader's typed check lost a binding or projection: %+v", reopened)
	}
	document := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *reopened.Draft})
	if document.State != desktop.Completed || document.Schema != desktop.FHIRCheckGroupSchema {
		t.Fatalf("FHIR check document: %+v", document)
	}
	unsupported := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *reopened.Draft,
		Document: strings.Replace(document.Document, "sequence-equals", "remote-equals", 1)})
	if unsupported.State != desktop.Completed || len(unsupported.Draft.CheckGroup.Unsupported) != 1 ||
		unsupported.Draft.CheckGroup.Unsupported[0].Operator != "remote-equals" {
		t.Fatalf("the unsupported clause was discarded: %+v", unsupported)
	}
	before := entries(t, context.Project)
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CheckGroupItem, Item: saved.Saved.ID, BaseRevision: saved.Saved.Revision,
		Draft: *unsupported.Draft, IntentID: "fhir-check-unsupported"})
	if refused.Outcome == desktop.SavedOutcome || !reflect.DeepEqual(before, entries(t, context.Project)) {
		t.Fatalf("unsupported clauses were saved lossily: %+v", refused)
	}
}

func TestLibraryOffersAllFourExecutableScenarioFamilies(t *testing.T) {
	app := workspaceApp(t)
	families := map[string]bool{}
	for _, template := range app.Shell().Shell.Vocabulary.Scenarios.Templates {
		families[template.Family] = true
	}
	for _, family := range []string{"ADT", "SIU", "ORM", "ORU"} {
		if !families[family] {
			t.Fatalf("the saved scenario editor offers no %s template", family)
		}
	}
}

func TestFHIRProfilePinsAndOriginSurviveSaveReopenAndPackageImport(t *testing.T) {
	app, context := namedProject(t)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProfileItem}, Protocol: "fhir-r4"})
	if opened.State != desktop.Completed || opened.Draft.Profile == nil || opened.Draft.Profile.FHIR == nil {
		t.Fatalf("new FHIR profile: %+v", opened)
	}
	draft := *opened.Draft
	draft.Name = "Site patient requirements"
	draft.Profile.Origin = profileOrigin(t)
	pin := fhirvalidator.Canonical{URL: "https://example.invalid/StructureDefinition/patient", Version: "1.0.0", SHA256: strings.Repeat("a", 64),
		Package: fhirvalidator.PackageRef{ID: "site.patient", Version: "1.0.0"}}
	draft.Profile.FHIR.Profiles = []fhirvalidator.Canonical{pin}
	draft.Profile.FHIR.Packages = []fhirvalidator.PackageRef{pin.Package}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: draft, IntentID: "fhir-profile"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save FHIR profile: %+v", saved)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Draft.Profile.FHIR.Profiles, []fhirvalidator.Canonical{pin}) ||
		!reflect.DeepEqual(reopened.Draft.Profile.Origin, draft.Profile.Origin) {
		t.Fatalf("the exact FHIR pins or attribution changed: %+v", reopened)
	}
	resolved := app.ResolveProfileDraft(desktop.DraftRequest{Context: context, Kind: desktop.ProfileItem, Draft: *reopened.Draft})
	if resolved.State != desktop.Completed || resolved.FHIR == nil || resolved.FHIR.State != "worker-missing" {
		t.Fatalf("passive resolution claimed validation without a worker: %+v", resolved)
	}
	exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *saved.Saved, Destination: context.Project + "/patient-package.json"})
	if exported.State != desktop.Completed || exported.Schema != desktop.FHIRProfilePackageSchema {
		t.Fatalf("export: %+v", exported)
	}
	imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: exported.Path})
	if imported.State != desktop.Completed || !reflect.DeepEqual(imported.Draft.Profile.FHIR, reopened.Draft.Profile.FHIR) ||
		!reflect.DeepEqual(imported.Draft.Profile.Origin, draft.Profile.Origin) {
		t.Fatalf("import did not retain all pins and origin: %+v", imported)
	}
	changed := *reopened.Draft
	changed.Profile.FHIR.Identity.Version = "2"
	changed.Profile.FHIR.Requirements.Terminology = "not-requested"
	next := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: saved.Saved.ID, BaseRevision: saved.Saved.Revision,
		Draft: changed, IntentID: "fhir-profile-2"})
	if next.Outcome != desktop.SavedOutcome {
		t.Fatalf("publish new version: %+v", next)
	}
	old := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if old.State != desktop.Completed || old.Draft.Profile.FHIR.Identity.Version != "1" || old.Draft.Profile.FHIR.Requirements.Terminology != "required" {
		t.Fatalf("publishing mutated old version: %+v", old)
	}
}

func TestFHIRScenarioDraftIsSavedWholeWithGoAllocatedSeedAndBaseTime(t *testing.T) {
	app, context := namedProject(t)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}, Protocol: "fhir-r4"})
	if opened.State != desktop.Completed || opened.Draft.Scenario == nil || opened.Draft.Scenario.FHIR == nil {
		t.Fatalf("new FHIR scenario: %+v", opened)
	}
	draft := *opened.Draft
	draft.Name = "Synthetic resource and conditional request"
	template := draft.Scenario.FHIR
	if template.BaseTime == "" || template.Seed > 1<<53-1 {
		t.Fatalf("Go did not initialize the reproducible inputs: %+v", template)
	}
	raw := " {\n\"resourceType\":\"Patient\",\"id\":\"synthetic-a\",\"identifier\":[{\"system\":\"urn:synthetic\",\"value\":\"A\"}]}\n"
	template.Steps = []desktop.FHIRScenarioStep{{ID: "patient", After: "0s", SourceKind: "resource", Document: raw},
		{ID: "read-patient", After: "1s", SourceKind: "request", Document: "", Request: &desktop.FHIRScenarioRequest{
			Base: "https://example.invalid/fhir", Method: "GET", URL: "https://example.invalid/fhir/Patient/synthetic-a"}}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: draft, IntentID: "fhir-scenario"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save FHIR scenario: %+v", saved)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Draft.Scenario.FHIR.Steps, template.Steps) ||
		reopened.Draft.Scenario.FHIR.Seed != template.Seed || reopened.Draft.Scenario.FHIR.BaseTime != template.BaseTime {
		t.Fatalf("saved FHIR template changed: %+v", reopened)
	}
	document := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: *reopened.Draft})
	if document.State != desktop.Completed || document.Schema != desktop.FHIRScenarioSchema || !bytes.Contains([]byte(document.Document), []byte("synthetic-a")) {
		t.Fatalf("the same initiated editor cannot read the retained template: %+v", document)
	}
}

func TestSavedFHIRScenarioGeneratesActualResourceAndRequestEvidenceOnce(t *testing.T) {
	app, context := namedProject(t)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}, Protocol: "fhir-r4"})
	draft := *opened.Draft
	draft.Name = "Synthetic FHIR patient"
	raw := " {\n\"resourceType\":\"Patient\",\"id\":\"synthetic-a\",\"active\":true}\n"
	draft.Scenario.FHIR.Steps = []desktop.FHIRScenarioStep{{ID: "patient", After: "0s", SourceKind: "resource", Document: raw},
		{ID: "read-patient", After: "1s", SourceKind: "request", Request: &desktop.FHIRScenarioRequest{
			Base: "https://example.invalid/fhir", Method: "GET", URL: "https://example.invalid/fhir/Patient/synthetic-a"}}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: draft, IntentID: "fhir-scenario-bytes"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	// The bound window carries empty arrays explicitly when generation uses
	// saved settings. They contain no v2 clause and must stay an empty input.
	settings := desktop.CaseGenerationSettings{Variants: []casegen.Variant{}, Bindings: casegen.Bindings{Patients: []casegen.PatientBinding{},
		Visits: []casegen.VisitBinding{}, Appointments: []casegen.AppointmentBinding{}, Resources: []casegen.ResourceBinding{}, Orders: []casegen.OrderBinding{}, Edits: []casegen.Edit{}}}
	generated := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: *saved.Saved, Settings: settings, IntentID: "fhir-cases"})
	if generated.State != desktop.Completed || len(generated.Cases) != 2 || generated.Cases[0].Evaluated || generated.Cases[0].Verdict != "" ||
		generated.Cases[0].Phases[0].ID != "patient" || generated.Cases[1].Phases[0].ID != "read-patient" {
		t.Fatalf("saved FHIR templates did not become actual usable evidence: %+v", generated)
	}
	resource, err := fhirevidence.Open(stdcontext.Background(), filepath.Join(context.Project, generated.Cases[0].Entry))
	if err != nil || resource.Document == nil || string(resource.Raw()) != raw || resource.Manifest.Provenance.Mode != "generated" {
		t.Fatalf("generated resource bytes or provenance changed: %v %+v", err, resource)
	}
	request, err := fhirevidence.Open(stdcontext.Background(), filepath.Join(context.Project, generated.Cases[1].Entry))
	if err != nil || request.Request == nil || request.Request.Kind != "read" || request.Manifest.Declaration.Request.Method != "GET" || len(request.Raw()) != 0 {
		t.Fatalf("generated request lost its real HTTP semantics: %v %+v", err, request)
	}
	before := entries(t, context.Project)
	again := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: *saved.Saved, IntentID: "fhir-cases-again"})
	if again.State != desktop.Completed || !again.Replayed || again.ContentIdentity != generated.ContentIdentity || !reflect.DeepEqual(again.Cases, generated.Cases) ||
		!reflect.DeepEqual(before, entries(t, context.Project)) {
		t.Fatalf("a repeat wrote or changed saved cases: %+v", again)
	}
	settings.Wire.Delimiters = "|^~\\&"
	refused := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: *saved.Saved, Settings: settings, IntentID: "fhir-with-v2-clause"})
	if refused.State != desktop.Failed || !reflect.DeepEqual(before, entries(t, context.Project)) {
		t.Fatalf("FHIR generation discarded an authored v2 clause: %+v", refused)
	}
	items := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if items.State != desktop.Completed || len(items.Page.Items) != 2 {
		t.Fatalf("generated evidence is not available as named cases: %+v", items)
	}
	for _, item := range items.Page.Items {
		if item.Availability != desktop.ItemAvailable || item.Summary.Case.Provenance != "synthetic" {
			t.Fatalf("generated evidence lost its scope: %+v", item)
		}
	}
}

func TestSavedScenarioSettingsReachTheRealCaseEncoder(t *testing.T) {
	app, context, scenarioRef, profile, settings := generationProject(t,
		[]scenariogen.Row{{ID: "plain", PatientName: "DOE", Notes: []string{}, Encoding: "utf-8"}},
		[]scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}})
	// The chosen local profile is published whole first, so the scenario can
	// retain the exact saved revision instead of a moving file path.
	openedProfile := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: profile})
	if openedProfile.State != desktop.Completed {
		t.Fatalf("open profile: %+v", openedProfile)
	}
	openedProfile.Draft.Profile.Profile.Identity.Version = "2"
	published := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Item: profile.ID, Draft: *openedProfile.Draft, IntentID: "profile-publish"})
	if published.Outcome != desktop.SavedOutcome {
		t.Fatalf("publish profile: %+v", published)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: scenarioRef})
	opened.Draft.Scenario.Profile = published.Saved
	opened.Draft.Scenario.Generation = &settings
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem,
		Item: scenarioRef.ID, BaseRevision: scenarioRef.Revision, Draft: *opened.Draft, IntentID: "scenario-settings"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save settings: %+v", saved)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if reopened.State != desktop.Completed || !reflect.DeepEqual(reopened.Draft.Scenario.Generation, &settings) ||
		!reflect.DeepEqual(reopened.Draft.Scenario.Profile, published.Saved) {
		t.Fatalf("the saved wire form and business bindings changed on reopen: %+v", reopened.Draft)
	}
	generated := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context,
		Scenario: *saved.Saved, IntentID: "saved-cases"})
	if generated.State != desktop.Completed || len(generated.Cases) != 1 || generated.Cases[0].Verdict != "pass" ||
		generated.Cases[0].Messages != 3 || len(generated.Cases[0].Phases) != 3 || generated.GeneratorVersion != "readmit-case-generator-v1" {
		t.Fatalf("the saved scenario did not reach the real encoder: %+v", generated)
	}
}
