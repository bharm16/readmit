package desktop_test

import (
	"encoding/json/v2"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestExplicitFHIRObservationRetainsTypedZeroAndReopensWithoutRequests(t *testing.T) {
	app, context := namedProject(t)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/fhir+json")
		if r.URL.Path == "/fhir/metadata" {
			_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","status":"active","date":"2026-09-29","kind":"instance","fhirVersion":"4.0.1","format":["json"],"rest":[{"mode":"server","resource":[{"type":"Appointment","interaction":[{"code":"search-type"}],"searchParam":[{"name":"identifier","type":"token"}]}]}]}`))
			return
		}
		if r.URL.Path != "/fhir/Appointment" || r.URL.Query().Get("identifier") != "urn:lab:appointment|qa" {
			t.Errorf("unexpected search: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset","link":[{"relation":"self","url":"` + serverBaseFromRequest(r) + r.URL.String() + `"}],"entry":[{"fullUrl":"` + serverBaseFromRequest(r) + `/fhir/Appointment/qa","search":{"mode":"match"},"resource":{"resourceType":"Appointment","id":"qa","status":"booked","priority":0,"participant":[{"status":"accepted","actor":{"reference":"Patient/qa"}}]}}]}`))
	}))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	env := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: desktop.ItemDraft{Name: "FHIR QA", FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: server.URL + "/fhir", ServerName: "example.com", CAFile: ca, Authentication: "none", Classification: replay.Nonproduction}, SendPolicy: &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}}, IntentID: "collection-env"})
	review := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CheckFHIRCapabilitiesAction, Items: []desktop.ItemRef{env}})
	if review.Review == nil || !review.Review.Ready {
		t.Fatalf("capability review: %+v", review)
	}
	if result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Review.Token, IntentID: "collection-capabilities"}); result.State != desktop.Completed {
		t.Fatalf("capabilities: %+v", result)
	}
	vocabulary := app.Shell().Shell.Vocabulary.Connected
	setup := vocabulary.Observation
	search := vocabulary.Search
	search.Boundary = "reference-fhir-store"
	setup.Environment = env.ID
	search.Criteria = []desktop.FHIRCriterion{{Parameter: "identifier", Type: "token", System: "urn:lab:appointment", Value: "qa"}}
	search.Fields = []desktop.FHIRFieldProjection{{Name: "identity", Field: "resource-identity", Key: true, Required: true}, {Name: "status", Field: "status", Required: true}, {Name: "priority", Field: "priority"}}
	setup.FHIR = &search
	setup.BusinessKeys = []desktop.BusinessKeyMapping{{Field: "identity", Variable: "business-id"}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: "Appointment state", Observation: &desktop.ObservationDraft{Connected: &setup}}, IntentID: "collection-source"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	collect := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*saved.Saved}})
	if collect.Review == nil || !collect.Review.Ready {
		t.Fatalf("collect review: %+v", collect)
	}
	if requests.Load() != 1 {
		t.Fatal("saving or reviewing the observation queried its source")
	}
	result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: collect.Review.Token, IntentID: "collect-typed"})
	if result.State != desktop.Completed || result.Collected == nil || result.TypedCollection == nil || len(result.TypedCollection.Rows) != 1 {
		t.Fatalf("typed collection: %+v row=%+v typed=%+v requests=%d", result, result.Collected, result.TypedCollection, requests.Load())
	}
	zero := result.TypedCollection.Rows[0].Values[2]
	if zero.State != "present" || zero.Type != "decimal" || zero.Text != "0" {
		t.Fatalf("legitimate zero was lost: %+v", zero)
	}
	if !strings.Contains(result.TypedCollection.Meaning, "snapshot") || !strings.Contains(result.TypedCollection.Meaning, "horizon") {
		t.Fatal("a standalone snapshot was presented as a completed test window")
	}
	history := app.ObservationHistory(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if history.State != desktop.Completed || len(history.Collections) != 1 {
		t.Fatalf("history: %+v inspection=%+v", history, app.InspectCompletion(desktop.CompletionRequest{Context: context, Ref: *saved.Saved, Entry: result.Collected.Entry}))
	}
	inspection := app.InspectCompletion(desktop.CompletionRequest{Context: context, Ref: *saved.Saved, Entry: result.Collected.Entry})
	if inspection.State != desktop.Completed || inspection.Completion.Typed == nil || inspection.Completion.Typed.Rows[0].Values[2].Text != "0" {
		t.Fatalf("offline typed reopening: %+v", inspection)
	}
	if requests.Load() != 3 {
		t.Fatalf("offline history or inspection reached the server: %d", requests.Load())
	}
	detail := app.OpenItem(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if detail.Item == nil || detail.Item.Summary.Observation.LatestCollection == nil || detail.Item.Summary.Observation.LatestStatus != "complete" {
		t.Fatalf("dated observation summary: %+v", detail)
	}
	checked := false
	for _, row := range app.ListConnections(context).Rows {
		if row.Ref == "observation:"+saved.Saved.ID {
			checked = row.State == desktop.ConnectionChecked && row.CheckedAt != nil && row.Detail.Outcome == "complete"
		}
	}
	if !checked || requests.Load() != 3 {
		t.Fatal("offline FHIR inventory lost the actual dated collection")
	}
}

func serverBaseFromRequest(r *http.Request) string { return "https://" + r.Host }

func connectedDatasetDraft(t *testing.T, app *desktop.App, context desktop.RequestContext, legacy *desktop.ObservationDraft, sample string) *desktop.ObservationDraft {
	t.Helper()
	fields := app.ObservationFields(desktop.ObservationFieldsRequest{Context: context, Source: &legacy.Source, SchemaSample: sample, Typed: true})
	if fields.State != desktop.Completed || fields.Projection == nil {
		t.Fatalf("typed fields: %+v", fields)
	}
	projection := *fields.Projection
	key := "appointment"
	if legacy.Source.HTTP != nil {
		key = "id"
		continuation := slices.IndexFunc(fields.Continuations, func(choice desktop.ObservationFieldChoice) bool { return choice.ID == "next" })
		if continuation < 0 {
			t.Fatalf("the sample offers no document-relative next-page choice: %+v", fields)
		}
		projection.Continuation = slices.Clone(fields.Continuations[continuation].Locator)
	}
	fieldLocator := func(id string) importer.Locator {
		index := slices.IndexFunc(fields.Choices, func(choice desktop.ObservationFieldChoice) bool { return choice.ID == id })
		if index < 0 {
			t.Fatalf("the actual picker offers no record-relative field %q: %+v", id, fields.Choices)
		}
		return slices.Clone(fields.Choices[index].Locator)
	}
	legacy.Source.Extraction.RecordKey = fieldLocator(key)
	projection.Columns = []dataset.Column{{Name: "identity", Type: "text", Locator: fieldLocator(key), Key: true, Required: true}, {Name: "status", Type: "text", Locator: fieldLocator("status"), Required: true}}
	setup := app.Shell().Shell.Vocabulary.Connected.Observation
	setup.Projection = &projection
	setup.BusinessKeys = []desktop.BusinessKeyMapping{{Field: "identity", Variable: "business-id"}}
	legacy.Connected = &setup
	return legacy
}

func TestTypedFileCollectAnchorsProjectPathsAndPreservesDeclaredSourceOffline(t *testing.T) {
	app, context := namedProject(t)
	writeDocument(t, context.Project, "export.csv", "appointment,status\nqa,booked\n")
	draft := connectedDatasetDraft(t, app, context, observationDraft(t, "appointments"), "")
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: "Relative export", Observation: draft}, IntentID: "typed-relative"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	raw, err := os.ReadFile(filepath.Join(context.Project, memberEntry(t, context.Project, *saved.Saved, "source")))
	if err != nil {
		t.Fatal(err)
	}
	source, err := observesource.DecodeSource(raw)
	if err != nil || source.File.Path != "export.csv" {
		t.Fatalf("declared relative source: %+v %v", source.File, err)
	}
	result := collect(t, app, context, *saved.Saved, "typed-relative-collect")
	if result.State != desktop.Completed || result.TypedCollection == nil || len(result.TypedCollection.Rows) != 1 || result.TypedCollection.Rows[0].Values[1].Text != "booked" {
		t.Fatalf("relative source collection: %+v", result)
	}
	snapshot, err := observesource.OpenDataset(t.Context(), filepath.Join(context.Project, result.Collected.Entry, "acquisition"))
	if err != nil || snapshot.Document().Binding.Source != source.Identity() || string(snapshot.Document().Acquisition.SourceConfiguration) != string(raw) {
		t.Fatalf("retained declaration changed during acquisition: %v", err)
	}
	if err := os.Remove(filepath.Join(context.Project, "export.csv")); err != nil {
		t.Fatal(err)
	}
	inspection := app.InspectCompletion(desktop.CompletionRequest{Context: context, Ref: *saved.Saved, Entry: result.Collected.Entry})
	if inspection.State != desktop.Completed || inspection.Completion.Typed == nil || inspection.Completion.Typed.Rows[0].Values[1].Text != "booked" {
		t.Fatalf("offline relative source: %+v", inspection)
	}
}

func TestTypedHTTPCollectUsesSavedCredentialArgumentsAndRelativeCA(t *testing.T) {
	app, context := namedProject(t)
	const value = "synthetic-typed-source-credential"
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != value {
			t.Error("the source did not resolve its named credential")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"appointments":[{"id":"qa","status":"booked"}]}`))
	}))
	defer server.Close()
	writeDocument(t, context.Project, "ca.pem", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
	writeDocument(t, context.Project, "schema.json", `{"appointments":[{"id":"qa","status":"booked"}],"next":""}`)
	locator := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(locator, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	address := strings.TrimPrefix(server.URL, "https://")
	registered := app.SaveCredential(desktop.CredentialSaveRequest{Context: context, Name: "typed-api", Purpose: secret.SourceEndpoint, Store: secret.CustomerManaged, Address: address, Command: "/bin/cat", Arguments: []string{locator}})
	if registered.State != desktop.Completed {
		t.Fatalf("credential registration: %+v", registered)
	}
	draft := observationOf(t, strings.Replace(httpObservationSource, "https://api.example/appointments", server.URL+"/appointments", 1))
	draft.Source.HTTP.CAFile, draft.Source.HTTP.ServerName = "ca.pem", "example.com"
	draft.Credential = "typed-api"
	draft = connectedDatasetDraft(t, app, context, draft, "schema.json")
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: "Typed API", Observation: draft}, IntentID: "typed-api-source"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	answer, _ := json.Marshal(opened)
	if opened.Draft == nil || len(opened.Draft.Observation.Source.HTTP.Credential.Arguments) != 0 || strings.Contains(string(answer), locator) {
		t.Fatal("the editor disclosed runtime locator arguments")
	}
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: desktop.ItemDraft{Name: "Approved lab", Environment: targetDraft(address), SendPolicy: &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}}, IntentID: "typed-api-policy"})
	review := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CollectObservationAction, Items: []desktop.ItemRef{*saved.Saved}, Destination: &environment})
	if review.Review == nil || !review.Review.Ready || requests.Load() != 0 {
		t.Fatalf("passive review: %+v calls=%d", review, requests.Load())
	}
	result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Review.Token, IntentID: "typed-api-collect"})
	if result.State != desktop.Completed || result.TypedCollection == nil || len(result.TypedCollection.Rows) != 1 || result.TypedCollection.Rows[0].Values[1].Text != "booked" || requests.Load() != 1 {
		t.Fatalf("saved runtime source: %+v calls=%d", result, requests.Load())
	}
	answer, _ = json.Marshal(result)
	if strings.Contains(string(answer), value) || strings.Contains(string(answer), locator) {
		t.Fatal("the collection disclosed credential material")
	}
	server.Close()
	if err := os.Remove(locator); err != nil {
		t.Fatal(err)
	}
	inspection := app.InspectCompletion(desktop.CompletionRequest{Context: context, Ref: *saved.Saved, Entry: result.Collected.Entry})
	if inspection.State != desktop.Completed || inspection.Completion.Typed == nil || inspection.Completion.Typed.Rows[0].Values[1].Text != "booked" || requests.Load() != 1 {
		t.Fatalf("offline credential source: %+v calls=%d", inspection, requests.Load())
	}
}

func TestTypedExportPickersAcquireJSONXMLAndTextUsingReturnedRecordLocators(t *testing.T) {
	for _, test := range []struct {
		format, body, key, status string
		extraction                *observesource.Extraction
	}{
		{"json", `{"appointments":[{"id":"qa","nested":{"status.code":"booked"},"optional":null,"enabled":false}]}`, "id", "nested.status.code", &observesource.Extraction{Envelope: "json", Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{"appointments"}}, RecordKey: importer.Locator{"id"}}},
		{"xml", `<rows><row><id>qa</id><nested><status>booked</status></nested></row></rows>`, "id", "nested.status", &observesource.Extraction{Envelope: "xml", Encoding: importer.UTF8, XML: &importer.DocumentDialect{RecordPath: []string{"rows", "row"}}, RecordKey: importer.Locator{"id"}}},
		{"text", "qa|booked\n", "1", "2", &observesource.Extraction{Envelope: "text", Encoding: importer.UTF8, Text: &importer.TextDialect{FieldSeparator: "|", RecordSeparator: importer.LFSeparator, Fields: 2}, RecordKey: importer.Locator{"1"}}},
	} {
		t.Run(test.format, func(t *testing.T) {
			app, context := namedProject(t)
			name := "export." + test.format
			writeDocument(t, context.Project, name, test.body)
			draft := observationDraft(t, "appointments")
			draft.Source.Extraction, draft.Source.File.Path = test.extraction, name
			fields := app.ObservationFields(desktop.ObservationFieldsRequest{Context: context, Source: &draft.Source, Typed: true})
			if fields.State != desktop.Completed || fields.Projection == nil {
				t.Fatalf("actual typed %s picker: %+v", test.format, fields)
			}
			choice := func(id string) importer.Locator {
				index := slices.IndexFunc(fields.Choices, func(choice desktop.ObservationFieldChoice) bool { return choice.ID == id })
				if index < 0 {
					t.Fatalf("the %s picker offers no record field %q: %+v", test.format, id, fields.Choices)
				}
				return slices.Clone(fields.Choices[index].Locator)
			}
			draft.Source.Extraction.RecordKey = choice(test.key)
			projection := *fields.Projection
			projection.Columns = []dataset.Column{{Name: "identity", Type: "text", Locator: choice(test.key), Key: true, Required: true}, {Name: "status", Type: "text", Locator: choice(test.status), Required: true}}
			if test.format == "json" {
				projection.Columns = append(projection.Columns, dataset.Column{Name: "optional", Type: "text", Locator: choice("optional")}, dataset.Column{Name: "enabled", Type: "boolean", Locator: choice("enabled")})
			}
			setup := app.Shell().Shell.Vocabulary.Connected.Observation
			setup.Projection, setup.BusinessKeys = &projection, []desktop.BusinessKeyMapping{{Field: "identity", Variable: "business-id"}}
			draft.Connected = &setup
			saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Name: "Typed export", Observation: draft}, IntentID: "picked-export"})
			if saved.Outcome != desktop.SavedOutcome {
				t.Fatalf("actual picked export save: %+v", saved)
			}
			result := collect(t, app, context, *saved.Saved, "picked-export-collect")
			if result.State != desktop.Completed || result.TypedCollection == nil || len(result.TypedCollection.Rows) != 1 || result.TypedCollection.Rows[0].Values[1].Text != "booked" {
				t.Fatalf("picked record locators are not executable: %+v", result)
			}
			if test.format == "json" && (result.TypedCollection.Rows[0].Values[2].State != "null" || result.TypedCollection.Rows[0].Values[3].State != "present" || result.TypedCollection.Rows[0].Values[3].Text != "false") {
				t.Fatal("Go-selected null or false was lost")
			}
		})
	}
}
