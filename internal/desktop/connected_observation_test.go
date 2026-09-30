package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
)

func TestFHIRObservationPublishesSourceProjectionAndFullHorizonTogether(t *testing.T) {
	app, context := namedProject(t)
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: desktop.ItemDraft{Name: "FHIR QA", FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: "https://qa.invalid/fhir", ServerName: "qa.invalid", Authentication: "none", Classification: replay.Unclassified}}, IntentID: "typed-env"})
	vocabulary := app.Shell().Shell.Vocabulary.Connected
	setup := vocabulary.Observation
	search := vocabulary.Search
	search.Boundary = "reference-fhir-store"
	setup.Environment = environment.ID
	search.Criteria = []desktop.FHIRCriterion{{Parameter: "identifier", Type: "token", System: "urn:lab:appointment", Value: "{business-id}"}}
	search.Fields = []desktop.FHIRFieldProjection{{Name: "identity", Field: "resource-identity", Key: true, Required: true}, {Name: "status", Field: "status", Required: true}, {Name: "minutes", Field: "minutesDuration"}}
	setup.BusinessKeys = []desktop.BusinessKeyMapping{{Field: "identity", Variable: "business-id"}}
	setup.FHIR = &search
	observation := &desktop.ObservationDraft{Connected: &setup}
	draft := desktop.ItemDraft{Name: "Appointment state", Observation: observation}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: draft, IntentID: "typed-observation"})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("whole save: %+v", saved)
	}
	sourceRaw, err := os.ReadFile(filepath.Join(context.Project, memberEntry(t, context.Project, *saved.Saved, "source")))
	if err != nil {
		t.Fatal(err)
	}
	source, err := fhirobserve.Decode(sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Columns) != 3 || source.Columns[1].Type != "code" || source.Columns[1].CodeSystem != "http://hl7.org/fhir/appointmentstatus" || source.Columns[2].Type != "decimal" {
		t.Fatalf("shared typed projection: %+v", source.Columns)
	}
	intervalRaw, err := os.ReadFile(filepath.Join(context.Project, memberEntry(t, context.Project, *saved.Saved, "interval")))
	if err != nil {
		t.Fatal(err)
	}
	interval, err := observeinterval.Decode(intervalRaw)
	if err != nil || interval.Source != source.Identity() || interval.HorizonMS != 30000 || interval.Barrier != nil {
		t.Fatalf("shared full-horizon policy: %+v %v", interval, err)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.Observation.Connected == nil || len(opened.Draft.Observation.Connected.FHIR.Fields) != 3 {
		t.Fatalf("reopened full draft: %+v", opened)
	}
	search.Fields[2].Field = "unsupported.path()"
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: saved.Saved.ID, BaseRevision: saved.Saved.Revision, Draft: draft, IntentID: "unsupported-mapping"})
	if refused.Outcome != desktop.InvalidOutcome {
		t.Fatalf("unsupported mapping: %+v", refused)
	}
	current := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if current.State != desktop.Completed || current.Ref.Revision != saved.Saved.Revision || len(current.Draft.Observation.Connected.FHIR.Fields) != 3 {
		t.Fatal("a mapping refusal changed the published observation")
	}
}

func TestTypedObservationPickerDefaultsRespectEverySourceBudget(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "export.csv", "appointment,status\nqa,booked\n")
	writeDocument(t, context.Project, "schema.json", `{"appointments":[{"id":"qa","status":"booked"}],"next":""}`)
	file := observationDraft(t, "appointments")
	http := observationOf(t, httpObservationSource)
	database := observationOf(t, databaseObservationSource)
	database.Source.Database.Limits = &observesource.DatabaseLimits{Timeout: "2s", MaxRows: 20, MaxBytes: 32768}
	for _, test := range []struct {
		name, sample, key string
		draft             *desktop.ObservationDraft
		limits            dataset.Limits
	}{
		{"file", "", "appointment", file, dataset.Limits{MaxRows: 128, MaxBytes: 65536, TimeoutMS: 30000}},
		{"http", "schema.json", "id", http, dataset.Limits{MaxRows: 128, MaxBytes: 65536, TimeoutMS: 5000}},
		{"database", "export.csv", "appointment", database, dataset.Limits{MaxRows: 20, MaxBytes: 32768, TimeoutMS: 2000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields := app.ObservationFields(desktop.ObservationFieldsRequest{Context: context, Source: &test.draft.Source, SchemaSample: test.sample})
			if fields.State != desktop.Completed || fields.Projection == nil || fields.Projection.Limits != test.limits {
				t.Fatalf("declared budget defaults: %+v", fields)
			}
			projection := *fields.Projection
			projection.Columns = []dataset.Column{{Name: "identity", Type: "text", Locator: importer.Locator{test.key}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}
			if test.name == "http" {
				projection.Continuation = importer.Locator{"next"}
			}
			if err := observesource.ValidateDatasetProjection(test.draft.Source, projection); err != nil {
				t.Fatalf("picker initialized an uncollectable declaration: %v", err)
			}
		})
	}
}

func TestTypedObservationRefusesIncompatibleSourceAndProjectionWithoutPublishing(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "export.csv", "appointment,status\nqa,booked\n")
	draft := connectedDatasetDraft(t, app, context, observationDraft(t, "appointments"), "")
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: "Typed export", Observation: draft}, IntentID: "typed-compatible"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("initial save: %+v", saved)
	}
	draft.Source.Extraction = &observesource.Extraction{Envelope: "json", Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: importer.Locator{"appointments"}}, RecordKey: importer.Locator{"id"}}
	draft.Source.File.Path = "export.json"
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: saved.Saved.ID, BaseRevision: saved.Saved.Revision, Draft: desktop.ItemDraft{Name: "Typed export", Observation: draft}, IntentID: "typed-incompatible"})
	if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) != 1 || refused.Problems[0].Field != "observation.connected.projection" {
		t.Fatalf("mismatched whole object: %+v", refused)
	}
	current := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if current.State != desktop.Completed || current.Ref.Revision != saved.Saved.Revision || current.Draft.Observation.Source.Extraction.Envelope != "csv" || current.Draft.Observation.Connected.Projection.Format != "csv" || len(current.Draft.Observation.Connected.Projection.Columns) != 2 {
		t.Fatalf("incompatible save changed the published source or fields: %+v", current)
	}
	if draft.Source.Extraction.Envelope != "json" || draft.Connected.Projection.Format != "csv" {
		t.Fatal("refusal mutated the caller's whole draft")
	}
}
