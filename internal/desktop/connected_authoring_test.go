package desktop_test

import (
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
)

// connectedAuthoring is a project whose saved objects a connected test is
// authored from: a v2 environment reaching an independent v2-to-FHIR engine,
// a FHIR environment reaching the FHIR application it writes, both with the
// operator's typed isolation; FHIR observations of appointments and
// encounters; a case of SIU booking and reschedule messages; and FHIR
// evidence of an appointment create request. Everything is synthetic.
type connectedAuthoring struct {
	app                  *desktop.App
	context              desktop.RequestContext
	root                 string
	lab                  *connectedlab.FHIRLab
	engine               *connectedlab.Engine
	fixture              *httptest.Server
	v2, fhir             desktop.ItemRef
	appointments         desktop.ItemRef
	encounters           desktop.ItemRef
	messages, request    desktop.CatalogItem
	messageIdentity      string
	requestIdentity      string
	bookAt, rescheduleAt string
	requestResource      string
	provider, registryAt string
}

// labCapability declares the lab's Appointment conditional create and update.
var labCapability = strings.Replace(connectedlab.Capability, `{"type":"Appointment","versioning":"versioned-update",`, `{"type":"Appointment","versioning":"versioned-update","conditionalCreate":true,"conditionalUpdate":true,`, 1)

const bookMessage = "MSH|^~\\&|SCHED|LAB|ENGINE|LAB|20260301090000||SIU^S12|BOOK-1|P|2.5.1\rSCH|APT-1||||||||||2026-03-02T09:00:00Z\r"
const rescheduleMessage = "MSH|^~\\&|SCHED|LAB|ENGINE|LAB|20260301091500||SIU^S13|MOVE-1|P|2.5.1\rSCH|APT-1||||||||||2026-03-02T09:30:00Z\r"

func newConnectedAuthoring(t *testing.T) *connectedAuthoring {
	t.Helper()
	app, context := namedProject(t)
	f := &connectedAuthoring{app: app, context: context, root: context.Project}
	f.lab = connectedlab.StartFHIRLab(t)
	f.lab.CapabilityOverride.Store(labCapability)
	f.lab.SetDownstream("encounter")
	f.engine = connectedlab.StartEngine(t, f.lab)
	adapter := &minimizeFixture{project: context.ProjectID}
	f.fixture = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Provisioning the tenant's synthetic patient starts the lab's
		// application from a clean store, as the operator's adapter does.
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/create") {
			f.lab.Reset()
		}
		adapter.serve(w, r)
	}))
	t.Cleanup(f.fixture.Close)
	folder := t.TempDir()
	f.provider = filepath.Join(folder, "fixture-credential.sh")
	if err := os.WriteFile(f.provider, []byte("#!/bin/sh\nprintf 'connected-%s' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	address := f.fixture.Listener.Addr().String()
	role := func(name string, operation sendpolicy.Operation) testisolation.Credential {
		return testisolation.Credential{Endpoint: "fixture-" + name, Reference: networkaction.Credential{Endpoint: address, Purpose: operation, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: f.provider, Arguments: []string{name}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "lab-fixture", Revision: "1", Project: context.ProjectID, Environment: "qa", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "synthetic", Namespace: "minimize", URL: f.fixture.URL, ServerName: "example.com",
		Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.fixture.Certificate().Raw}), Read: role("read", sendpolicy.ObservationRead), Setup: role("setup", sendpolicy.SetupAction), Cleanup: role("cleanup", sendpolicy.SetupAction),
		Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}, TimeoutMS: 30000}}}
	f.registryAt = filepath.Join(folder, "registry.json")
	raw, _ := json.Marshal(registry)
	if err := os.WriteFile(f.registryAt, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	isolation := func() *desktop.EnvironmentIsolation {
		return &desktop.EnvironmentIsolation{Name: "Lab tenant", RegistryFile: f.registryAt, Adapter: "lab-fixture", Mode: "isolated-tenant", Manual: []desktop.IsolationManual{},
			Resources: []desktop.IsolationResource{{Name: "Patient", Kind: "patient", Template: "patient", Ownership: "create", DependsOn: []string{}, Attributes: map[string]string{"name": "Synthetic lab patient"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "patient-business", Value: "LAB"}}}}}
	}
	approved := &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}
	ca := filepath.Join(folder, "lab-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.lab.Server().Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	f.fhir = saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "fhir-environment", Draft: desktop.ItemDraft{Name: "Scheduling FHIR", SendPolicy: approved, Isolation: isolation(),
		FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: f.lab.Base(), ServerName: "example.com", CAFile: ca, Classification: replay.Nonproduction, Authentication: "none"}}})
	target := replay.Target{Schema: replay.TargetSchemaV3, Name: "Scheduling engine", Classification: replay.Nonproduction, TestEndpoint: true, Address: f.engine.Listener.Addr().String(), Transport: "plain", ApprovedTransport: true, ConnectTimeout: "30s", MessageTimeout: "60s", MaxACKBytes: 4096}
	f.v2 = saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "v2-environment", Draft: desktop.ItemDraft{Name: "Scheduling engine", Environment: &target, SendPolicy: approved, Isolation: isolation()}})
	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CheckFHIRCapabilitiesAction, Items: []desktop.ItemRef{f.fhir}})
	if prepared.Review == nil || !prepared.Review.Ready {
		t.Fatalf("capability review: %+v", prepared)
	}
	if checked := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "lab-capabilities"}); checked.State != desktop.Completed {
		t.Fatalf("capability check: %+v", checked)
	}
	completion := observeinterval.Definition{Schema: observeinterval.Schema, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 150, SampleMS: 25, MaxGapMS: 30000, MaxSamples: 400, MaxRecords: 50, MaxBytes: 512 << 10}
	observe := func(name, resource string, fields ...desktop.FHIRFieldProjection) desktop.ItemRef {
		saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, IntentID: "observe-" + resource, Draft: desktop.ItemDraft{Name: name, Observation: &desktop.ObservationDraft{Connected: &desktop.ConnectedObservation{
			Environment: f.fhir.ID, Namespace: strings.ToLower(resource) + "s", Phase: "both", Baseline: "before-run", BusinessKeys: []desktop.BusinessKeyMapping{{Field: "key", Variable: "appointment-key"}}, Completion: completion,
			FHIR: &desktop.FHIRSearchDraft{Resource: resource, Boundary: "reference-fhir-store", Criteria: []desktop.FHIRCriterion{{Parameter: "identifier", Type: "token", System: connectedlab.AppointmentSystem, Value: "APT-1"}}, Fields: fields, Budget: fhirrest.Budget{Pages: 4, Rows: 50, Bytes: 256 << 10, TimeoutMS: 60000}}}}}})
		if saved.Outcome != desktop.SavedOutcome {
			t.Fatalf("save %s observation: %+v", resource, saved)
		}
		return *saved.Saved
	}
	f.appointments = observe("Appointments", "Appointment", desktop.FHIRFieldProjection{Name: "key", Field: "identifier[0].value", Key: true, Required: true}, desktop.FHIRFieldProjection{Name: "identity", Field: "resource-identity", Required: true}, desktop.FHIRFieldProjection{Name: "start", Field: "start"}, desktop.FHIRFieldProjection{Name: "status", Field: "status"})
	f.encounters = observe("Encounters", "Encounter", desktop.FHIRFieldProjection{Name: "key", Field: "identifier[0].value", Key: true, Required: true}, desktop.FHIRFieldProjection{Name: "appointment", Field: "appointment[0].reference"}, desktop.FHIRFieldProjection{Name: "status", Field: "status"})

	now := time.Now()
	wire := []byte{}
	for _, m := range []string{bookMessage, rescheduleMessage} {
		wire = append(append(append(wire, 11), m...), 28, 13)
	}
	written, err := bundle.Write(filepath.Join(f.root, "reschedule-messages"), []bundle.Input{{Path: "scheduling.mllp", Data: wire, Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	f.messageIdentity, f.bookAt, f.rescheduleAt = written.Identity, written.Events[0].ID, written.Events[1].ID
	if _, err := operation.RegisterCase(f.root, "reschedule-messages", operation.CaseRegistration{Title: "Reschedule messages"}); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"resourceType":"Appointment","identifier":[{"system":"` + connectedlab.AppointmentSystem + `","value":"APT-1"}],"status":"booked","start":"2026-03-02T09:00:00Z","participant":[{"status":"accepted"}]}`)
	evidence, err := fhirevidence.Create(t.Context(), filepath.Join(f.root, "appointment-request"), fhirevidence.Declaration{SourceKind: "request", Context: fhirr4.Context{Version: fhirr4.Version, MediaType: "application/fhir+json", Base: "https://ehr.example/fhir"},
		Request: &fhirevidence.RequestDeclaration{Method: "POST", URL: "https://ehr.example/fhir/Appointment", Headers: fhirrequest.Headers{IfNoneExist: "identifier=" + connectedlab.AppointmentSystem + "|APT-1"}}}, body, fhirevidence.Provenance{Mode: "imported", ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	f.requestIdentity = evidence.Identity
	f.requestResource = evidence.Document.Resources()[0].Occurrence
	if _, err := operation.RegisterCase(f.root, "appointment-request", operation.CaseRegistration{Title: "Appointment request"}); err != nil {
		t.Fatal(err)
	}
	cases := listed(t, app, f.root, desktop.CaseItem)
	f.messages, f.request = cases["Reschedule messages"], cases["Appointment request"]
	if f.messages.Ref.ID == "" || f.request.Ref.ID == "" {
		t.Fatalf("cases were not registered: %+v", cases)
	}
	return f
}

func key(text string) dataset.Value { return dataset.Value{State: "present", Type: "text", Text: text} }

func (f *connectedAuthoring) where() []assertion.RowFilter {
	return []assertion.RowFilter{{Column: "key", Equals: key("APT-1")}}
}

// reschedule is the external v2 reschedule test: the booking and reschedule
// messages, then one appointment, at the new start, still booked.
func (f *connectedAuthoring) reschedule() desktop.ConnectedTestDraft {
	one := 1
	start := dataset.Value{State: "present", Type: "datetime", Text: "2026-03-02T09:30:00Z", Precision: "second", Timezone: "+00:00"}
	booked := dataset.Value{State: "present", Type: "code", Text: "booked", CodeSystem: "http://hl7.org/fhir/appointmentstatus"}
	source := func(occurrence string) desktop.ConnectedSource {
		return desktop.ConnectedSource{Case: f.messages.Ref, Identity: f.messageIdentity, Occurrence: occurrence}
	}
	return desktop.ConnectedTestDraft{Schema: desktop.ConnectedTestSchema, Boundary: desktop.ApplicationBoundary, Server: f.fhir.ID, Generation: connectedtest.Generation{Seed: 7, BaseTime: "2026-03-01T00:00:00Z"}, Variables: []desktop.ConnectedVariable{},
		Steps: []desktop.ConnectedStep{{ID: "step-1", After: []string{}, Source: source(f.bookAt), V2: &desktop.ConnectedV2{}}, {ID: "step-2", After: []string{"step-1"}, Source: source(f.rescheduleAt), V2: &desktop.ConnectedV2{}}},
		Phases: []desktop.ConnectedPhase{{ID: "reschedule", Name: "Reschedule", Steps: []string{"step-1", "step-2"}, After: []connectedtest.PhaseDependency{},
			Observations: []desktop.ConnectedPhaseObservation{{Dataset: "appointments", Observation: f.appointments, When: "after"}},
			Checks: []desktop.ConnectedCheck{
				{Name: "One appointment", Check: assertion.DatasetAssertion{ID: "one-appointment", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "appointments", Where: f.where()}, Count: &one}},
				{Name: "Moved start", Check: assertion.DatasetAssertion{ID: "moved-start", Operator: "instant-equals", Subject: assertion.RowSelection{Dataset: "appointments", Where: f.where()}, Column: "start", Expected: &start}},
				{Name: "Still booked", Check: assertion.DatasetAssertion{ID: "still-booked", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "appointments", Where: f.where()}, Column: "status", Expected: &booked}},
			},
			Responses: []desktop.ConnectedResponseCheck{}, Validations: []desktop.ConnectedValidationCheck{}, Acks: []desktop.ConnectedAckCheck{{ID: "rescheduled-accepted", Name: "Reschedule accepted", Step: "step-2", Code: "AA"}}}}}
}

// booking is the FHIR test: a conditional create of the requested
// appointment, then a version-aware update of exactly that appointment, each
// checking the application's appointment and the encounter it keeps related.
func (f *connectedAuthoring) booking() desktop.ConnectedTestDraft {
	one := 1
	booked := dataset.Value{State: "present", Type: "code", Text: "booked", CodeSystem: "http://hl7.org/fhir/appointmentstatus"}
	planned := dataset.Value{State: "present", Type: "code", Text: "planned", CodeSystem: "http://hl7.org/fhir/encounter-status"}
	source := desktop.ConnectedSource{Case: f.request.Ref, Identity: f.requestIdentity, Occurrence: f.requestResource}
	observations := []desktop.ConnectedPhaseObservation{{Dataset: "appointments", Observation: f.appointments, When: "after"}, {Dataset: "encounters", Observation: f.encounters, When: "after"}}
	checks := func(prefix string) []desktop.ConnectedCheck {
		return []desktop.ConnectedCheck{
			{Name: "One appointment", Check: assertion.DatasetAssertion{ID: prefix + "-one", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "appointments", Where: f.where()}, Count: &one}},
			{Name: "Booked", Check: assertion.DatasetAssertion{ID: prefix + "-booked", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "appointments", Where: f.where()}, Column: "status", Expected: &booked}},
			{Name: "Encounter of the appointment", Check: assertion.DatasetAssertion{ID: prefix + "-related", Operator: "value-related", Subject: assertion.RowSelection{Dataset: "encounters", Where: f.where()}, Column: "appointment", Other: &assertion.RowSelection{Dataset: "appointments", Where: f.where()}, OtherColumn: "identity"}},
			{Name: "Planned encounter", Check: assertion.DatasetAssertion{ID: prefix + "-planned", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "encounters", Where: f.where()}, Column: "status", Expected: &planned, When: &assertion.DatasetCondition{Subject: assertion.RowSelection{Dataset: "appointments", Where: f.where()}, Column: "status", Equals: booked}}},
		}
	}
	return desktop.ConnectedTestDraft{Schema: desktop.ConnectedTestSchema, Boundary: desktop.ApplicationBoundary, Generation: connectedtest.Generation{Seed: 11, BaseTime: "2026-03-01T00:00:00Z"},
		Variables: []desktop.ConnectedVariable{{ID: "appointment-id", Kind: "response"}, {ID: "appointment-version", Kind: "response"}},
		Steps: []desktop.ConnectedStep{
			{ID: "create", After: []string{}, Source: source, FHIR: &desktop.ConnectedFHIR{Method: "POST", Resource: "Appointment", Target: desktop.ConnectedFHIRTarget{Kind: "type"}, IfNone: &desktop.ConnectedIdentifier{System: connectedlab.AppointmentSystem, Value: "APT-1"},
				Bind: []connectedtest.ResponseBinding{{Variable: "appointment-id", From: "logical-id", Multiplicity: "exactly-one", Scope: "lifecycle"}, {Variable: "appointment-version", From: "version-id", Multiplicity: "exactly-one", Scope: "lifecycle"}}}},
			{ID: "update", After: []string{"create"}, Source: source, FHIR: &desktop.ConnectedFHIR{Method: "PUT", Resource: "Appointment", Target: desktop.ConnectedFHIRTarget{Kind: "instance", Variable: "appointment-id"}, IfMatch: "appointment-version", Prefer: "return=representation", Bind: []connectedtest.ResponseBinding{}}},
		},
		Phases: []desktop.ConnectedPhase{
			{ID: "create", Name: "Create", Steps: []string{"create"}, After: []connectedtest.PhaseDependency{}, Observations: observations, Checks: checks("created"),
				Responses: []desktop.ConnectedResponseCheck{{Name: "Created", Check: connectedtest.ResponseCheck{ID: "created", Step: "create", Outcome: "succeeded"}}}, Validations: []desktop.ConnectedValidationCheck{}, Acks: []desktop.ConnectedAckCheck{}},
			{ID: "update", Name: "Update", Steps: []string{"update"}, After: []connectedtest.PhaseDependency{{Phase: "create", Requires: "pass"}}, When: &connectedtest.PhaseCondition{Phase: "create", Check: "response:created", Outcome: "passed"}, Observations: observations, Checks: checks("updated"),
				Responses: []desktop.ConnectedResponseCheck{{Name: "Updated", Check: connectedtest.ResponseCheck{ID: "updated", Step: "update", Outcome: "succeeded"}}}, Validations: []desktop.ConnectedValidationCheck{}, Acks: []desktop.ConnectedAckCheck{}},
		}}
}

func (f *connectedAuthoring) save(t *testing.T, name, intent string, d desktop.ConnectedTestDraft, environment string) desktop.ItemRef {
	t.Helper()
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, IntentID: intent, Draft: desktop.ItemDraft{Name: name, ConnectedTest: &d, TestLinks: &desktop.TestLinks{Environment: environment, Reset: desktop.ResetFromEnvironment}}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save %s: %+v", name, saved)
	}
	return *saved.Saved
}

func TestConnectedTestsAuthoredFromV2AndFHIREvidenceSaveOnceAndReopenWhole(t *testing.T) {
	f := newConnectedAuthoring(t)
	creates := f.lab.Creates.Load()
	reschedule := f.reschedule()
	ref := f.save(t, "Reschedule keeps one appointment", "reschedule-create", reschedule, f.v2.ID)
	again := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, IntentID: "reschedule-create", Draft: desktop.ItemDraft{Name: "Reschedule keeps one appointment", ConnectedTest: &reschedule, TestLinks: &desktop.TestLinks{Environment: f.v2.ID, Reset: desktop.ResetFromEnvironment}}})
	if again.Outcome != desktop.SavedOutcome || !again.Replayed || *again.Saved != ref {
		t.Fatalf("a repeated Create test published again: %+v", again)
	}
	booking := f.booking()
	bookingRef := f.save(t, "Booking keeps its encounter", "booking-create", booking, f.fhir.ID)
	for _, saved := range []struct {
		ref   desktop.ItemRef
		draft desktop.ConnectedTestDraft
	}{{ref, reschedule}, {bookingRef, booking}} {
		opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: saved.ref})
		if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.ConnectedTest == nil || opened.Draft.Test != nil || len(opened.Problems) != 0 {
			t.Fatalf("reopen: %+v", opened)
		}
		if !reflect.DeepEqual(*opened.Draft.ConnectedTest, saved.draft) {
			t.Fatalf("reopening dropped a field, condition or expected state:\nsaved  %+v\nopened %+v", saved.draft, *opened.Draft.ConnectedTest)
		}
		if opened.Test == nil || opened.Test.Connected == nil || len(opened.Test.Connected.Pinned) == 0 || len(opened.Test.Connected.Environments) != 2 {
			t.Fatalf("connected context: %+v", opened.Test)
		}
	}
	listing := listed(t, f.app, f.root, desktop.TestItem)
	for _, expected := range []struct {
		name       string
		ref        desktop.ItemRef
		source     desktop.ItemRef
		draft      desktop.ConnectedTestDraft
		assertions int
	}{
		{"Reschedule keeps one appointment", ref, f.messages.Ref, reschedule, 4},
		{"Booking keeps its encounter", bookingRef, f.request.Ref, booking, 10},
	} {
		row := listing[expected.name]
		summary := row.Summary.Test
		if row.Ref != expected.ref || summary == nil || summary.Boundary != desktop.ApplicationBoundary || summary.Assertions != expected.assertions || summary.Entry == "" || summary.SourceCase == nil || summary.SourceCase.Kind != expected.source.Kind || summary.SourceCase.ID != expected.source.ID {
			t.Fatalf("listed connected test %s: ref=%+v summary=%+v expected_source=%+v assertions=%d", expected.name, row.Ref, summary, expected.source, expected.assertions)
		}
		data, err := os.ReadFile(filepath.Join(f.root, summary.Entry))
		var retained desktop.ConnectedTestDraft
		if err != nil || json.Unmarshal(data, &retained, json.RejectUnknownMembers(true)) != nil || !reflect.DeepEqual(retained, expected.draft) {
			t.Fatalf("listed connected entry does not identify its exact saved draft: %s (%v)", summary.Entry, err)
		}
	}
	if f.lab.Creates.Load() != creates || f.lab.Tokens.Load() != 0 {
		t.Fatal("authoring, saving or reopening a connected test reached the application")
	}
}

// approve records one reviewed approval of a saved suite version: its
// baseline, or with options its promotion to one environment.
func (f *connectedAuthoring) approve(t *testing.T, action desktop.ActionID, ref desktop.ItemRef, intent string, options *desktop.SuiteApprovalOptions) desktop.ReviewedActionResult {
	t.Helper()
	prepared := f.app.PrepareAction(desktop.PrepareActionRequest{Context: f.context, Action: action, Items: []desktop.ItemRef{ref}, SuiteApproval: options})
	if prepared.Review == nil || !prepared.Review.Ready || prepared.Review.SuiteApproval == nil {
		t.Fatalf("approval review: %+v", prepared)
	}
	return f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: prepared.Review.Token, IntentID: intent, Decisions: desktop.ReviewDecisions{Rationale: "Independently reviewed synthetic expectations"}})
}

// copyEnvironment saves another named environment exactly like one.
func (f *connectedAuthoring) copyEnvironment(t *testing.T, ref desktop.ItemRef, name, intent string) desktop.ItemRef {
	t.Helper()
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: ref})
	if opened.Draft == nil {
		t.Fatalf("open environment: %+v", opened)
	}
	draft := *opened.Draft
	draft.Name = name
	return saveEnvironment(t, f.app, f.context, desktop.SaveItemRequest{IntentID: intent, Draft: draft})
}

func TestConnectedTestsJoinOneSuiteWithTheirApprovedDefinitionsInEveryEnvironmentAndRunInTheConnectedRunner(t *testing.T) {
	parallelLifecycleTest(t)
	f := newConnectedAuthoring(t)
	reschedule := f.save(t, "Reschedule keeps one appointment", "reschedule-create", f.reschedule(), f.v2.ID)
	booking := f.save(t, "Booking keeps its encounter", "booking-create", f.booking(), f.fhir.ID)

	// A second environment binds other named environments of the same lab:
	// promotion changes the binding, never the approved expectations.
	stagingV2 := f.copyEnvironment(t, f.v2, "Staging engine", "staging-v2")
	stagingFHIR := f.copyEnvironment(t, f.fhir, "Staging FHIR", "staging-fhir")
	prepared := f.app.PrepareAction(desktop.PrepareActionRequest{Context: f.context, Action: desktop.CheckFHIRCapabilitiesAction, Items: []desktop.ItemRef{stagingFHIR}})
	if prepared.Review == nil || f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: prepared.Review.Token, IntentID: "staging-capabilities"}).State != desktop.Completed {
		t.Fatalf("staging capability check: %+v", prepared)
	}
	draft := desktop.SuiteDraft{Tags: []string{}, Concurrency: 1,
		Tests: []desktop.SuiteTestDraft{
			{ID: "reschedule", Test: reschedule, Parameter: "engine", After: []string{}, Sequence: []string{}, Tags: []string{}},
			{ID: "booking", Test: booking, Parameter: "application", After: []string{"reschedule"}, Sequence: []string{}, Tags: []string{}},
		},
		Datasets: []desktop.SuiteDataset{},
		Environments: []desktop.SuiteEnvironment{
			{ID: "qa", Name: "QA", Bindings: []desktop.SuiteBinding{{Parameter: "engine", Target: f.v2, Server: &f.fhir}, {Parameter: "application", Target: f.fhir}}},
			{ID: "staging", Name: "Staging", Bindings: []desktop.SuiteBinding{{Parameter: "engine", Target: stagingV2, Server: &stagingFHIR}, {Parameter: "application", Target: stagingFHIR}}},
		},
		Requirements: []desktop.SuiteRequirement{{ID: "scheduling-records", Name: "Scheduling keeps its records", Tests: []string{"reschedule", "booking"}}},
		Exclusions:   []desktop.SuiteExclusion{}}
	// Saving publishes the definition alone, with each binding's pinned
	// environment version kept, and places nothing for the runner.
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.SuiteItem, IntentID: "suite-save", Draft: desktop.ItemDraft{Name: "Scheduling regression", Suite: &draft}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save suite: %+v", saved)
	}
	if _, err := os.Stat(filepath.Join(f.root, ".readmit", "connected")); !os.IsNotExist(err) {
		t.Fatalf("saving a suite placed connected files: %v", err)
	}
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: *saved.Saved})
	if opened.Draft == nil || opened.Draft.Suite == nil || len(opened.Draft.Suite.Tests) != 2 || opened.Suite == nil || opened.Suite.Runnable {
		t.Fatalf("an unapproved suite version reopened as runnable: %+v", opened)
	}
	if pinned := opened.Draft.Suite.Environments[0].Bindings[0]; pinned.Target.Revision == "" || pinned.Server == nil || pinned.Server.Revision == "" {
		t.Fatalf("a binding lost its pinned environment version: %+v", pinned)
	}
	// Approving the version's baseline is the one approval of its tests'
	// expectations; it makes the version runnable.
	if approved := f.approve(t, desktop.ApproveSuiteBaselineAction, *saved.Saved, "suite-baseline", nil); approved.State != desktop.Completed || approved.SuiteApproval == nil {
		t.Fatalf("baseline: %+v", approved)
	}
	opened = f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: *saved.Saved})
	if opened.Suite == nil || !opened.Suite.Runnable || opened.Suite.Document == "" {
		t.Fatalf("the approved version is not runnable: %+v", opened.Suite)
	}
	var document suite.ConnectedDocument
	if err := json.Unmarshal([]byte(opened.Suite.Document), &document); err != nil || len(document.Environments) != 2 || len(document.Tests) != 2 {
		t.Fatalf("approved document: %+v %v", document, err)
	}
	for i, test := range document.Tests {
		qa, staging := document.Environments[0].Bindings[i], document.Environments[1].Bindings[i]
		if qa.PlanIdentity == staging.PlanIdentity {
			t.Fatal("two environments compiled one lifecycle; their bindings are lost")
		}
		for _, binding := range []string{qa.Plan, staging.Plan} {
			review, err := expectationReview(filepath.Join(f.root, binding))
			if err != nil || review.ID != test.ID || review.Revision != test.Revision || review.Definition != test.Definition {
				t.Fatalf("%s: an environment changed the approved definition: %+v %v", test.ID, review, err)
			}
		}
	}
	// Promoting the baselined version to Staging approves the binding only.
	if promoted := f.approve(t, desktop.ApprovePromotionAction, *saved.Saved, "suite-staging", &desktop.SuiteApprovalOptions{Environment: "staging", Revision: "lab-build-1"}); promoted.State != desktop.Completed || promoted.SuiteApproval == nil || !promoted.SuiteApproval.Current {
		t.Fatalf("staging approval: %+v", promoted)
	}
	preflight := f.app.PreflightRun(desktop.RunPreflightRequest{Suite: &desktop.SuiteRunTarget{Context: f.context, Suite: *saved.Saved}, Environment: "staging"})
	if preflight.State != desktop.Completed || preflight.Preflight == nil || preflight.Preflight.Connected == nil || len(preflight.Preflight.Connected.Jobs) != 2 || preflight.Preflight.Admission.Admitted {
		t.Fatalf("the saved suite did not reach the connected runner's preparation: %+v", preflight)
	}
	if f.lab.Creates.Load() != 0 {
		t.Fatal("saving, approving or preparing the suite reached the application")
	}

	// The connected runner executes exactly the saved suite version.
	run := func(instance string) runqueueReport {
		raw, _ := json.Marshal(document, json.Deterministic(true))
		placed := filepath.Join(f.root, ".readmit-compiled-suite-"+instance+".json")
		if err := os.WriteFile(placed, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(placed)
		report, err := suiteRunConnected(t, placed, "qa", filepath.Join(f.root, instance), instance, func(name string, binding networkaction.Binding) {
			for _, environment := range document.Environments {
				for _, b := range environment.Bindings {
					grant := filepath.Join(f.root, filepath.Dir(b.Config), "grants", strings.ReplaceAll(name, ":", "-")+".json")
					connectedlab.WriteJSON(t, grant, networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
				}
			}
		})
		if err != nil {
			t.Fatalf("connected runner: %v", err)
		}
		return report
	}
	passed := run("fixed-engine")
	if passed.code != 0 {
		t.Fatalf("the correct engine and application failed the saved suite: %+v", passed)
	}
	// Coverage of the saved version is assessed over its retained connected
	// execution with the plans and approvals that execution pinned.
	coverage := f.app.SuiteCoverage(desktop.SuiteCoverageRequest{Context: f.context, Suite: *saved.Saved, Previous: []desktop.ItemRef{}})
	if coverage.State != desktop.Completed || coverage.Denominator != 1 || coverage.Passed != 1 || len(coverage.Jobs) != 2 || coverage.Jobs[0].Test != "reschedule" || !coverage.Jobs[0].Eligible {
		t.Fatalf("connected coverage: %+v", coverage)
	}
	// Suggest checks reads the completed run of exactly this definition: a
	// count and each field of the record its business key selects, never a
	// server-assigned identity as a value; nothing is added until accepted.
	listed(t, f.app, f.root, desktop.RunItem)
	runs := f.app.SuggestConnectedChecks(desktop.ConnectedSuggestRequest{Context: f.context, Test: reschedule})
	if runs.State != desktop.Completed || len(runs.Runs) != 1 || len(runs.Proposals) != 0 {
		t.Fatalf("eligible runs: %+v", runs)
	}
	suggested := f.app.SuggestConnectedChecks(desktop.ConnectedSuggestRequest{Context: f.context, Test: reschedule, Run: &runs.Runs[0].Run})
	byName := map[string]desktop.ConnectedProposal{}
	for _, proposal := range suggested.Proposals {
		byName[proposal.Check.Name] = proposal
	}
	if count := byName["Records of APT-1"]; count.Check.Check.Count == nil || *count.Check.Check.Count != 1 || count.Check.Check.Subject.Where[0].Equals.Text != "APT-1" {
		t.Fatalf("record count proposal: %+v", suggested.Proposals)
	}
	if start := byName["start of APT-1"]; start.Reason != "" || start.Check.Check.Operator != "instant-equals" || start.Check.Check.Expected == nil {
		t.Fatalf("start proposal: %+v", start)
	}
	if identity := byName["identity of APT-1"]; identity.Reason == "" || identity.Check.Check.Expected != nil {
		t.Fatalf("a server-assigned identity was proposed as an expected value: %+v", identity)
	}
	f.engine.SetMode("defective")
	failed := run("defective-engine")
	// The duplicate is a failed check; the booking that depends on the
	// reschedule passing is skipped and stays in the denominator.
	if failed.code == 0 || failed.verdicts["test-"+reschedule.ID] != assertion.VerdictFail || len(failed.verdicts) != 1 {
		t.Fatalf("a reschedule that duplicates the appointment passed: %+v", failed)
	}
}

func TestConnectedSuiteRunsEachDatasetRowWithItsOverridesAndKeepsItsPins(t *testing.T) {
	parallelLifecycleTest(t)
	f := newConnectedAuthoring(t)
	reschedule := f.save(t, "Reschedule keeps one appointment", "reschedule-create", f.reschedule(), f.v2.ID)
	booking := f.save(t, "Booking keeps its encounter", "booking-create", f.booking(), f.fhir.ID)
	late := dataset.Value{State: "present", Type: "datetime", Text: "2026-03-02T09:45:00Z", Precision: "second", Timezone: "+00:00"}
	draft := func(overrides map[string]dataset.Value) desktop.SuiteDraft {
		return desktop.SuiteDraft{Tags: []string{}, Concurrency: 1,
			Tests: []desktop.SuiteTestDraft{
				{ID: "reschedule", Test: reschedule, Dataset: "schedules", Parameter: "engine", After: []string{}, Sequence: []string{}, Tags: []string{}},
				{ID: "booking", Test: booking, Parameter: "application", After: []string{"reschedule"}, Sequence: []string{}, Tags: []string{}},
			},
			Datasets: []desktop.SuiteDataset{{ID: "schedules", Name: "Schedules", Rows: []desktop.SuiteDataRow{
				{ID: "on-time", Case: f.messages.Ref},
				{ID: "late", Case: f.messages.Ref, ConnectedExpected: overrides},
			}}},
			Environments: []desktop.SuiteEnvironment{{ID: "qa", Name: "QA", Bindings: []desktop.SuiteBinding{{Parameter: "engine", Target: f.v2, Server: &f.fhir}, {Parameter: "application", Target: f.fhir}}}},
			Requirements: []desktop.SuiteRequirement{}, Exclusions: []desktop.SuiteExclusion{}}
	}
	unknown := draft(map[string]dataset.Value{"reschedule/no-such-check": late})
	refused := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.SuiteItem, IntentID: "suite-unknown", Draft: desktop.ItemDraft{Name: "Schedules", Suite: &unknown}})
	if refused.Outcome != desktop.InvalidOutcome || !hasProblem(refused.Problems, "suite.datasets.0.rows.1", "no check reschedule/no-such-check") {
		t.Fatalf("a row overrode a check the test does not have: %+v", refused)
	}
	parameterized := draft(map[string]dataset.Value{"reschedule/moved-start": late})
	saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.SuiteItem, IntentID: "suite-save", Draft: desktop.ItemDraft{Name: "Schedules", Suite: &parameterized}})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save parameterized suite: %+v", saved)
	}

	// A baseline that is not recorded leaves nothing it placed behind.
	connected := filepath.Join(f.root, ".readmit", "connected")
	desktop.SetSaveFaultForTest(f.app, func(string) error { return errors.New("injected publication failure") })
	if failed := f.approve(t, desktop.ApproveSuiteBaselineAction, *saved.Saved, "baseline-fails", nil); failed.State == desktop.Completed {
		t.Fatalf("a baseline was recorded through a failed publication: %+v", failed)
	}
	desktop.SetSaveFaultForTest(f.app, nil)
	if _, err := os.Stat(connected); !os.IsNotExist(err) {
		t.Fatalf("a baseline that was not recorded left its files: %v", err)
	}
	if approved := f.approve(t, desktop.ApproveSuiteBaselineAction, *saved.Saved, "baseline", nil); approved.State != desktop.Completed {
		t.Fatalf("baseline: %+v", approved)
	}
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: *saved.Saved})
	var document suite.ConnectedDocument
	if opened.Suite == nil || json.Unmarshal([]byte(opened.Suite.Document), &document) != nil || len(document.Tests) != 3 {
		t.Fatalf("one job per row and test: %+v", opened.Suite)
	}
	onTime, lateJob, bookingJob := "test-"+reschedule.ID+"-on-time", "test-"+reschedule.ID+"-late", "test-"+booking.ID
	if document.Tests[0].ID != onTime || document.Tests[1].ID != lateJob || document.Tests[2].ID != bookingJob || document.Tests[0].Definition == document.Tests[1].Definition ||
		!reflect.DeepEqual(document.Tests[2].After, []string{onTime, lateJob}) {
		t.Fatalf("rows and dependencies: %+v", document.Tests)
	}

	// Each row runs with its own expected values: the engine moves the
	// appointment to 09:30, which the late row does not expect.
	raw, _ := json.Marshal(document, json.Deterministic(true))
	placed := filepath.Join(f.root, ".readmit-compiled-suite-rows.json")
	if err := os.WriteFile(placed, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(placed)
	report, err := suiteRunConnected(t, placed, "qa", filepath.Join(f.root, "rows"), "rows", func(name string, binding networkaction.Binding) {
		for _, b := range document.Environments[0].Bindings {
			grant := filepath.Join(f.root, filepath.Dir(b.Config), "grants", strings.ReplaceAll(name, ":", "-")+".json")
			connectedlab.WriteJSON(t, grant, networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
		}
	})
	if err != nil || report.verdicts[onTime] != assertion.VerdictPass || report.verdicts[lateJob] != assertion.VerdictFail {
		t.Fatalf("rows ran without their own expectations: %+v %v", report, err)
	}

	// The suite keeps the environment versions it pinned: a later version
	// of an environment is chosen again, never followed.
	environment := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: f.v2})
	changed := *environment.Draft
	timeout := *changed.Environment
	timeout.MessageTimeout = "45s"
	changed.Environment = &timeout
	saveEnvironment(t, f.app, f.context, desktop.SaveItemRequest{IntentID: "v2-later", Item: f.v2.ID, BaseRevision: f.v2.Revision, Draft: changed})
	opened = f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: *saved.Saved})
	if pinned := opened.Draft.Suite.Environments[0].Bindings[0].Target; pinned.Revision != f.v2.Revision {
		t.Fatalf("the suite followed a later environment version: %+v", pinned)
	}
	again := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.SuiteItem, Item: saved.Saved.ID, BaseRevision: saved.Saved.Revision, IntentID: "suite-again", Draft: *opened.Draft})
	if again.Outcome != desktop.InvalidOutcome || !hasProblem(again.Problems, "suite.environments.0.bindings.0", "this suite pins version "+f.v2.Revision) {
		t.Fatalf("a suite silently followed a changed environment: %+v", again)
	}
}

func TestAConnectedLifecycleOpensInTheEditorWithItsUnsupportedChecksKeptExactly(t *testing.T) {
	f := newConnectedAuthoring(t)
	// A check that selects a record by its position in an earlier output is
	// one the editor does not author; written by hand, it is kept as written.
	text := `{ "id": "first-output", "operator": "row-count", "subject": {"dataset": "appointments", "row": "r000001", "where": []}, "count": 1 }`
	d := f.reschedule()
	d.Phases[0].Unsupported = []desktop.ConnectedClause{{Kind: "check", ID: "first-output", Name: "First output", Reason: "it selects a record by its position", Text: text}}
	ref := f.save(t, "Reschedule as written", "written", d, f.v2.ID)
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: ref})
	if opened.Draft == nil || opened.Draft.ConnectedTest == nil || len(opened.Draft.ConnectedTest.Phases[0].Unsupported) != 1 || opened.Draft.ConnectedTest.Phases[0].Unsupported[0].Text != text {
		t.Fatalf("an unsupported check did not survive save byte for byte: %+v", opened.Draft)
	}

	// The same test's sealed readmit-connected-test/v5 lifecycle opens in
	// the editor whole: inputs from the project's case, observations at
	// their saved versions, every typed check, and the unsupported one kept.
	folder := filepath.Join(t.TempDir(), "plan")
	if err := desktop.WriteConnectedLifecycleForTest(f.app, f.context, d, f.v2.ID, folder); err != nil {
		t.Fatal(err)
	}
	importing := activatedApp(t, &chooser{files: []string{filepath.Join(folder, "test.json")}}, t.TempDir())
	imported := importing.ImportTestDraft(f.context)
	if imported.State != desktop.Completed || !imported.New || imported.Draft == nil || imported.Draft.ConnectedTest == nil {
		t.Fatalf("import lifecycle: %+v", imported)
	}
	got := *imported.Draft.ConnectedTest
	if !reflect.DeepEqual(got.Steps, d.Steps) || got.Server != f.fhir.ID || got.Generation != d.Generation || !reflect.DeepEqual(got.Phases[0].Observations, d.Phases[0].Observations) {
		t.Fatalf("the lifecycle's inputs or observations were not read back:\n%+v\n%+v", got, d)
	}
	for i, check := range d.Phases[0].Checks {
		if !reflect.DeepEqual(got.Phases[0].Checks[i].Check, check.Check) {
			t.Fatalf("check %d changed on import: %+v", i, got.Phases[0].Checks[i])
		}
	}
	if len(got.Phases[0].Acks) != 1 || got.Phases[0].Acks[0].Step != "step-2" || got.Phases[0].Acks[0].Code != "AA" {
		t.Fatalf("acknowledgement check: %+v", got.Phases[0].Acks)
	}
	kept := got.Phases[0].Unsupported
	var written, read assertion.DatasetAssertion
	if len(kept) != 1 || kept[0].Kind != "check" || kept[0].ID != "first-output" || kept[0].Reason == "" ||
		json.Unmarshal([]byte(text), &written) != nil || json.Unmarshal([]byte(kept[0].Text), &read) != nil || !reflect.DeepEqual(written, read) {
		t.Fatalf("the unsupported check was not kept: %+v", kept)
	}
	if len(imported.Problems) != 1 || imported.Problems[0].Field != "test.environment" {
		t.Fatalf("import problems: %+v", imported.Problems)
	}
	resaved := f.save(t, "Reschedule imported", "imported", got, f.v2.ID)
	reopened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: resaved})
	if reopened.Draft == nil || reopened.Draft.ConnectedTest.Phases[0].Unsupported[0].Text != kept[0].Text {
		t.Fatalf("the imported clause changed through save: %+v", reopened.Draft)
	}
	// A FHIR lifecycle's conditional create and version-aware update read
	// back as the same typed requests from the same evidence.
	booking := f.booking()
	bookingPlan := filepath.Join(t.TempDir(), "plan")
	if err := desktop.WriteConnectedLifecycleForTest(f.app, f.context, booking, f.fhir.ID, bookingPlan); err != nil {
		t.Fatal(err)
	}
	importing = activatedApp(t, &chooser{files: []string{filepath.Join(bookingPlan, "flow.json")}}, t.TempDir())
	imported = importing.ImportTestDraft(f.context)
	if imported.Draft == nil || imported.Draft.ConnectedTest == nil || !reflect.DeepEqual(imported.Draft.ConnectedTest.Steps, booking.Steps) || !reflect.DeepEqual(imported.Draft.ConnectedTest.Variables, booking.Variables) ||
		!reflect.DeepEqual(imported.Draft.ConnectedTest.Phases[1].When, booking.Phases[1].When) || len(imported.Draft.ConnectedTest.Phases[1].Checks) != 4 || len(imported.Draft.ConnectedTest.Phases[1].Unsupported) != 0 {
		t.Fatalf("the FHIR lifecycle was not read back whole: %+v", imported)
	}
	if f.lab.Creates.Load() != 0 {
		t.Fatal("importing a lifecycle reached the application")
	}
}

func TestConnectedTestRefusalsKeepTheDraftAndNeverReachTheApplication(t *testing.T) {
	f := newConnectedAuthoring(t)
	ref := f.save(t, "Reschedule keeps one appointment", "reschedule-create", f.reschedule(), f.v2.ID)

	// A stale base is refused and publishes nothing.
	edited := f.reschedule()
	edited.Phases[0].Name = "Reschedule twice"
	first := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, Item: ref.ID, BaseRevision: ref.Revision, IntentID: "edit-one", Draft: desktop.ItemDraft{Name: "Reschedule keeps one appointment", ConnectedTest: &edited, TestLinks: &desktop.TestLinks{Environment: f.v2.ID, Reset: desktop.ResetFromEnvironment}}})
	stale := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, Item: ref.ID, BaseRevision: ref.Revision, IntentID: "edit-two", Draft: desktop.ItemDraft{Name: "Reschedule keeps one appointment", ConnectedTest: &edited, TestLinks: &desktop.TestLinks{Environment: f.v2.ID, Reset: desktop.ResetFromEnvironment}}})
	if first.Outcome != desktop.SavedOutcome || stale.Outcome != desktop.ConflictOutcome {
		t.Fatalf("stale base: %+v %+v", first, stale)
	}
	current := *first.Saved

	// A validation check needs the FHIR environment's local validator.
	booking := f.booking()
	booking.Phases[0].Validations = []desktop.ConnectedValidationCheck{{ID: "created-valid", Name: "Created resource is valid", Step: "create"}}
	noValidator := f.app.ValidateDraft(desktop.DraftRequest{Context: f.context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Name: "Booking", ConnectedTest: &booking, TestLinks: &desktop.TestLinks{Environment: f.fhir.ID}}})
	if !hasProblem(noValidator.Problems, "connected.server", "validator") {
		t.Fatalf("a validation check without a validator was accepted: %+v", noValidator.Problems)
	}

	// SMART authorization needs its registered signing key reference.
	smart := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: f.fhir})
	authorized := *smart.Draft
	connection := *authorized.FHIR
	connection.Authentication, connection.ClientID, connection.TokenEndpoint, connection.Algorithm, connection.Scopes, connection.KeyReference, connection.KeyID, connection.PublicKeysFile = "smart", "lab-client", f.lab.Server().URL+"/token", "ES384", []string{"system/Appointment.rs"}, "unregistered-key", "lab-key", filepath.Join(t.TempDir(), "keys.json")
	authorized.FHIR, authorized.Name = &connection, "SMART FHIR"
	smartRef := saveEnvironment(t, f.app, f.context, desktop.SaveItemRequest{IntentID: "smart-environment", Draft: authorized})
	// Bound to it, the test's observations of the other server's records are
	// stale until the person chooses observations of this one.
	moved := f.app.ValidateDraft(desktop.DraftRequest{Context: f.context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Name: "Booking", ConnectedTest: ptr(f.booking()), TestLinks: &desktop.TestLinks{Environment: smartRef.ID}}})
	if !hasProblem(moved.Problems, "connected.phases.0.observations.0", "reads records of Scheduling FHIR") || !hasProblem(moved.Problems, "connected.phases.0.checks.0", "another FHIR server") {
		t.Fatalf("observations of another server's records were not invalidated: %+v", moved.Problems)
	}
	observeSMART := func(ref desktop.ItemRef, intent string) desktop.ItemRef {
		opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: ref})
		copied := *opened.Draft
		setup := *copied.Observation.Connected
		setup.Environment = smartRef.ID
		copied.Name, copied.Observation = "SMART "+copied.Name, &desktop.ObservationDraft{Connected: &setup}
		saved := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ObservationItem, IntentID: intent, Draft: copied})
		if saved.Outcome != desktop.SavedOutcome {
			t.Fatalf("save SMART observation: %+v", saved)
		}
		return *saved.Saved
	}
	smartBooking := f.booking()
	observations := []desktop.ConnectedPhaseObservation{{Dataset: "appointments", Observation: observeSMART(f.appointments, "smart-appointments"), When: "after"}, {Dataset: "encounters", Observation: observeSMART(f.encounters, "smart-encounters"), When: "after"}}
	for p := range smartBooking.Phases {
		smartBooking.Phases[p].Observations = observations
	}
	noAuth := f.app.ValidateDraft(desktop.DraftRequest{Context: f.context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Name: "Booking", ConnectedTest: &smartBooking, TestLinks: &desktop.TestLinks{Environment: smartRef.ID}}})
	if !hasProblem(noAuth.Problems, "connected.server", "signing key reference") {
		t.Fatalf("SMART without its key reference was accepted: %+v", noAuth.Problems)
	}
	// The observation's projection changes: the checks that read it are
	// invalidated where they are, the draft keeps every check, and a save is
	// refused until the person resolves them.
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: f.appointments})
	narrowed := *opened.Draft
	setup := *narrowed.Observation.Connected
	search := *setup.FHIR
	search.Fields = search.Fields[:2]
	setup.FHIR = &search
	narrowed.Observation = &desktop.ObservationDraft{Connected: &setup}
	if changed := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.ObservationItem, Item: f.appointments.ID, BaseRevision: f.appointments.Revision, IntentID: "narrow-appointments", Draft: narrowed}); changed.Outcome != desktop.SavedOutcome {
		t.Fatalf("narrow observation: %+v", changed)
	}
	reopened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: current})
	if reopened.Draft == nil || len(reopened.Draft.ConnectedTest.Phases[0].Checks) != 3 || !hasProblem(reopened.Problems, "connected.phases.0.checks.1", "changed") || !hasProblem(reopened.Problems, "connected.phases.0.observations.0", "changed") {
		t.Fatalf("an incompatible observation change was not shown at the checks it invalidates: %+v", reopened.Problems)
	}
	refused := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.TestItem, Item: current.ID, BaseRevision: current.Revision, IntentID: "save-invalidated", Draft: *reopened.Draft})
	if refused.Outcome != desktop.InvalidOutcome || !hasProblem(refused.Problems, "connected.phases.0.checks.1", "changed") {
		t.Fatalf("a save with invalidated checks was not refused: %+v", refused)
	}
	repinned := *reopened.Draft.ConnectedTest
	repinned.Phases = append([]desktop.ConnectedPhase{}, repinned.Phases...)
	phase := repinned.Phases[0]
	phase.Observations = append([]desktop.ConnectedPhaseObservation{}, phase.Observations...)
	phase.Observations[0].Observation.Revision = "2"
	repinned.Phases[0] = phase
	unbound := f.app.ValidateDraft(desktop.DraftRequest{Context: f.context, Kind: desktop.TestItem, Item: current.ID, Draft: desktop.ItemDraft{Name: reopened.Draft.Name, ConnectedTest: &repinned, TestLinks: reopened.Draft.TestLinks}})
	if !hasProblem(unbound.Problems, "connected.phases.0.checks.1", "no longer projects the field start") || hasProblem(unbound.Problems, "connected.phases.0.checks.0", "") {
		t.Fatalf("using the observation's current version did not leave exactly the unbound check: %+v", unbound.Problems)
	}

	if f.lab.Creates.Load() != 0 || f.lab.Tokens.Load() != 0 || f.lab.Writes.Load() != 0 {
		t.Fatal("a refused or validated draft reached the application")
	}
}

func hasProblem(problems []desktop.FieldProblem, field, contains string) bool {
	for _, p := range problems {
		if p.Field == field && strings.Contains(p.Problem, contains) {
			return true
		}
	}
	return false
}

func ptr[T any](value T) *T { return &value }

func TestConnectedTestSourcesReadVerifiedEvidenceAsTypedInputs(t *testing.T) {
	f := newConnectedAuthoring(t)
	messages := f.app.ConnectedTestSources(desktop.ConnectedSourcesRequest{Context: f.context, Case: f.messages.Ref})
	if messages.State != desktop.Completed || messages.Protocol != "v2" || messages.Identity != f.messageIdentity || len(messages.Sources) != 2 ||
		messages.Sources[0].Occurrence != f.bookAt || messages.Sources[0].Label != "SIU^S12" || !messages.Sources[1].Sendable || messages.Sources[1].FHIR != nil {
		t.Fatalf("v2 evidence as inputs: %+v", messages)
	}
	request := f.app.ConnectedTestSources(desktop.ConnectedSourcesRequest{Context: f.context, Case: f.request.Ref})
	if request.State != desktop.Completed || request.Protocol != "fhir" || request.Identity != f.requestIdentity || len(request.Sources) != 1 {
		t.Fatalf("FHIR evidence as inputs: %+v", request)
	}
	declared := request.Sources[0].FHIR
	if declared == nil || declared.Method != "POST" || declared.Resource != "Appointment" || declared.Target.Kind != "type" || declared.IfNone == nil || declared.IfNone.System != connectedlab.AppointmentSystem || declared.IfNone.Value != "APT-1" {
		t.Fatalf("the declared conditional create was not prefilled as typed choices: %+v", declared)
	}
	// Starting a test from FHIR evidence starts a connected draft of its
	// requests; nothing is saved or sent.
	started := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: f.request.Ref, Messages: []string{}}})
	if started.State != desktop.Completed || started.Draft == nil || started.Draft.ConnectedTest == nil || len(started.Draft.ConnectedTest.Steps) != 1 || started.Draft.ConnectedTest.Steps[0].FHIR.IfNone == nil || started.Test == nil || started.Test.Connected == nil {
		t.Fatalf("a test started from FHIR evidence: %+v", started)
	}
	if f.lab.Creates.Load() != 0 {
		t.Fatal("reading evidence as inputs reached the application")
	}
}
