package desktop_test

import (
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
)

// A public fixture-protocol target independently enforces exclusive leases,
// ownership and version guards. Counts witness setup and cleanup per trial.
type minimizeFixture struct {
	mu               sync.Mutex
	project          string
	lease            testisolation.Lease
	resource         *testisolation.Resource
	version          int
	created, deleted int
	denyCleanup      bool
}

func (f *minimizeFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := strings.TrimPrefix(r.URL.Path, "/fixture/v1/")
	role := "setup"
	if r.Method == "GET" {
		role = "read"
	} else if name == "delete" || name == "lease-release" {
		role = "cleanup"
	}
	if r.Header.Get("Authorization") != "Bearer connected-"+role {
		http.Error(w, "wrong role", 403)
		return
	}
	var request testisolation.Request
	var scope testisolation.Scope
	if r.Method == "GET" {
		if json.Unmarshal([]byte(r.URL.Query().Get("scope")), &scope) != nil {
			http.Error(w, "bad scope", 400)
			return
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 128<<10))
		if err != nil || json.Unmarshal(raw, &request) != nil || request.Schema != testisolation.ProtocolSchema || request.Action != name {
			http.Error(w, "bad request", 400)
			return
		}
		scope = request.Scope
	}
	if scope.Project != f.project || scope.Environment != "qa" || scope.Revision != "1" || scope.Tenant != "synthetic" || scope.Namespace != "minimize" || scope.Owner == "" {
		http.Error(w, "wrong deployment", 409)
		return
	}
	respond := func(v any) {
		raw, _ := json.Marshal(v)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
	switch name {
	case "capabilities":
		respond(testisolation.Capabilities{Schema: testisolation.ProtocolSchema, Scope: scope, Protocol: "typed-fixture-v1", LeaseMode: "exclusive-no-expiry", VersionGuards: true, Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}})
		return
	case "state":
		resources := []testisolation.Resource{}
		if f.resource != nil {
			resources = append(resources, *f.resource)
		}
		respond(testisolation.Snapshot{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: f.lease, Resources: resources})
		return
	case "lease-acquire":
		if f.lease.Owner != "" {
			http.Error(w, "already leased", 409)
			return
		}
		f.version++
		f.lease = testisolation.Lease{Key: scope.LeaseKey, Owner: scope.Owner, Version: strconv.Itoa(f.version)}
	default:
		if request.Lease != f.lease || f.lease.Owner != scope.Owner {
			http.Error(w, "wrong fence", 409)
			return
		}
		switch name {
		case "create":
			if f.resource != nil || request.Resource == nil {
				http.Error(w, "not empty", 409)
				return
			}
			f.version++
			resource := *request.Resource
			resource.Owner, resource.Version = scope.Owner, strconv.Itoa(f.version)
			f.resource = &resource
			f.created++
		case "delete":
			if f.denyCleanup {
				http.Error(w, "cleanup denied", 403)
				return
			}
			if f.resource == nil || request.Resource == nil || !reflect.DeepEqual(*f.resource, *request.Resource) {
				http.Error(w, "changed resource", 409)
				return
			}
			f.resource = nil
			f.deleted++
		case "lease-release":
			if f.resource != nil {
				http.Error(w, "resource remains", 409)
				return
			}
			f.lease = testisolation.Lease{}
		default:
			http.Error(w, "unsupported", 400)
			return
		}
	}
	respond(testisolation.Reply{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: f.lease, Resource: f.resource, Outcome: "applied"})
}

func connectedMinimizeProject(t *testing.T) (*desktop.App, desktop.RequestContext, desktop.ItemRef, *reductionReceiver, *minimizeFixture) {
	t.Helper()
	receiver := newReductionReceiver(t)
	app, context, run, environment := minimizeProject(t, receiver)
	root := context.Project
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	// Exercise a numeric ID actually minted by CreateNamedProject. Each
	// project is ordinary facade output; no identity is edited or aliased.
	for attempts := 0; document.Project.ID[0] > '9'; attempts++ {
		if attempts == 64 {
			t.Fatal("the project allocator produced no numeric ID")
		}
		app, context, run, environment = minimizeProject(t, receiver)
		root = context.Project
		store, err = catalog.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		document, _, err = store.Read()
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		retained, err := os.MkdirTemp("", "readmit-desktop-reduction-failure-")
		if err != nil {
			t.Logf("cannot retain failed reduction: %v", err)
			return
		}
		if err = os.CopyFS(filepath.Join(retained, "evidence"), os.DirFS(root)); err != nil {
			t.Logf("partial failed reduction retained at %s: %v", retained, err)
			return
		}
		t.Logf("failed desktop reduction retained at %s", retained)
	})
	project := document.Project.ID
	fixture := &minimizeFixture{project: project}
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	provider := filepath.Join(root, "minimize-credential.sh")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf 'connected-%s' \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	role := func(name string, operation sendpolicy.Operation) testisolation.Credential {
		return testisolation.Credential{Endpoint: "fixture-" + name, Reference: networkaction.Credential{Endpoint: server.Listener.Addr().String(), Purpose: operation, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: provider, Arguments: []string{name}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "fixture", Revision: "1", Project: project, Environment: "qa", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "synthetic", Namespace: "minimize", URL: server.URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), Read: role("read", sendpolicy.ObservationRead), Setup: role("setup", sendpolicy.SetupAction), Cleanup: role("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}, TimeoutMS: 30000}}}
	writeFixtureJSON(t, root, "fixture-registry.json", registry)
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: environment})
	if opened.Draft == nil {
		t.Fatal(opened)
	}
	draft := *opened.Draft
	draft.Environment.Schema = replay.TargetSchemaV3
	draft.SendPolicy = &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}
	draft.Isolation = &desktop.EnvironmentIsolation{Name: "Own synthetic patient", RegistryFile: filepath.Join(root, "fixture-registry.json"), Adapter: "fixture", Mode: "isolated-tenant", Resources: []desktop.IsolationResource{{ID: "patient", Name: "Patient", Kind: "patient", Template: "patient", Ownership: "create", DependsOn: []string{}, Attributes: map[string]string{"name": "Synthetic"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "business", Value: "same"}}}}, Manual: []desktop.IsolationManual{{ID: "reset", Name: "Reset listener", Instructions: draft.ResetPlan.Actions[0].Instructions}, {ID: "test-setup", Name: "Test setup", Instructions: "Restart the listener before running this."}}}
	// The explicitly initiated isolation editor replaces check-only setup;
	// its contract retains both original manual clauses unchanged.
	draft.ResetPlan = nil
	draft.Links.ResetName, draft.Links.ActionNames = "", nil
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: environment.ID, BaseRevision: opened.Ref.Revision, Draft: draft, IntentID: "isolation"})
	if saved.Saved == nil {
		t.Fatalf("saved isolation: %+v", saved)
	}
	environment = *saved.Saved
	// Pin only application-published members, retaining the original failed
	// run. The customer's suite author operates at these shared engine APIs.
	document, _, err = store.Read()
	if err != nil {
		t.Fatal(err)
	}
	record := document.Items[document.Find(environment.ID)]
	paths := map[string]string{}
	for _, member := range record.Current().Members {
		paths[member.Role] = filepath.Join(root, member.Path)
	}
	var target replay.Target
	readFixtureDocument(t, paths["target"], &target)
	rp, err := replay.Prepare(filepath.Join(root, "incident"), target, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, targetPort, _ := net.SplitHostPort(target.Address)
	_, fixturePort, _ := net.SplitHostPort(server.Listener.Addr().String())
	n, _ := strconv.Atoi(targetPort)
	fn, _ := strconv.Atoi(fixturePort)
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: project, Environment: "qa", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "receiver", Operation: sendpolicy.V2Stimulus, Port: n, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}}
	for _, c := range []testisolation.Credential{registry.Adapters[0].Read, registry.Adapters[0].Setup, registry.Adapters[0].Cleanup} {
		policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: c.Endpoint, Operation: c.Reference.Purpose, Port: fn, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
	}
	policyRaw, _ := json.Marshal(policy)
	writeFixtureJSON(t, root, "connected-policy.json", policy)
	extraction := &observesource.Extraction{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8, CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 1}, RecordKey: importer.Locator{"key"}}
	writeDocument(t, root, "application.csv", "key\n")
	source := observesource.Source{Schema: observesource.SchemaV1, Observes: observewindow.Source{Kind: observesource.FileExport, Identity: "independent-export", Scope: "appointments"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "1h"}, Extraction: extraction, File: &observesource.File{Path: filepath.Join(root, "application.csv"), MaxBytes: 65536}}
	if err := observesource.WriteSource(filepath.Join(root, "application-source.json"), source); err != nil {
		t.Fatal(err)
	}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "appointments", Format: "csv", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, CSV: extraction.CSV}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"key"}, Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 20, MaxBytes: 65536, TimeoutMS: 30000}}
	files := map[string][]byte{}
	ref := func(id, schema, file string, value any) connectedtest.Reference {
		raw, _ := json.Marshal(value, json.Deterministic(true))
		files[file] = raw
		return connectedtest.Reference{Project: project, ID: id, Schema: schema, File: file, SHA256: dataset.Digest(raw)}
	}
	interval := observeinterval.Definition{Schema: observeinterval.Schema, Source: source.Identity(), Namespace: "appointments", Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 25, SampleMS: 10, MaxGapMS: 30000, MaxSamples: 100, MaxRecords: 20, MaxBytes: 65536}
	projectionRef := ref("projection", dataset.ProjectionSchema, "projection.json", projection)
	intervalRef := ref("interval", observeinterval.Schema, "interval.json", interval)
	ds := connectedtest.Dataset{ID: "after", Kind: "typed-rows", Namespace: "appointments", Phase: "after", Source: source.Identity(), Projection: &projectionRef, Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: 25, MaxRecords: 20, MaxBytes: 65536, Policy: &intervalRef}}
	zero := 0
	checks := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{{Name: "after", Namespace: "appointments", Phase: "after", Source: source.Identity(), ProjectionIdentity: projection.Identity()}}, Assertions: []assertion.DatasetAssertion{{ID: "empty-export", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &zero}}}
	contract := testisolation.Contract{Schema: testisolation.ContractSchema, Project: project, Environment: "qa", Revision: "1", Adapter: "fixture", Tenant: "synthetic", Namespace: "minimize", Mode: "isolated-tenant", Concurrency: "exclusive-target-lease", Resources: []testisolation.Requirement{}, Manual: []testisolation.ManualStep{}}
	for _, resource := range draft.Isolation.Resources {
		contract.Resources = append(contract.Resources, testisolation.Requirement{ID: resource.ID, Kind: resource.Kind, Template: resource.Template, Ownership: resource.Ownership, LogicalID: resource.LogicalID, Version: resource.Version, DependsOn: resource.DependsOn, Attributes: resource.Attributes, Identifiers: resource.Identifiers})
	}
	for _, manual := range draft.Isolation.Manual {
		contract.Manual = append(contract.Manual, testisolation.ManualStep{ID: manual.ID, Instructions: manual.Instructions})
	}
	flow := connectedtest.FlowTest{Schema: connectedtest.FlowTestSchema, Project: project, ID: "reschedule", Revision: "1", Environment: connectedtest.Environment{Project: project, ID: "qa", Revision: "1", Name: target.Name, Classification: "nonproduction", Endpoint: "receiver", TargetIdentity: rp.Target().Identity(), AddressPolicyIdentity: dataset.Digest(policyRaw), TLS: connectedtest.TLS{Mode: "plain"}, TargetRevision: connectedtest.TargetRevision{Provenance: "operator-declared", Value: "synthetic-defect"}}, Isolation: ref("isolation", testisolation.ContractSchema, "isolation.json", contract), Boundary: "application-state", Limits: connectedtest.Limits{MaxSteps: 16, MaxBytes: 1 << 20, DeadlineMS: 600000}}
	input, err := bundle.Open(filepath.Join(root, "incident"))
	if err != nil {
		t.Fatal(err)
	}
	wire := assertion.Set{Schema: assertion.Schema, Name: "Saved ACK checks", Assertions: []assertion.Assertion{}}
	for i, message := range rdSequence {
		raw, _ := input.Raw(message)
		filename := fmt.Sprintf("input%d.hl7", i)
		files[filename] = raw
		flow.Steps = append(flow.Steps, connectedtest.Step{ID: fmt.Sprintf("step%d", i), Endpoint: "receiver", After: []string{}, BusinessKeys: []connectedtest.BusinessKey{}, V2: &connectedtest.V2Stimulus{Input: connectedtest.Reference{Project: project, ID: fmt.Sprintf("input%d", i), Schema: "hl7", File: filename, SHA256: dataset.Digest(raw)}, Occurrence: message, Assignments: []connectedtest.Assignment{}}})
		text := "AA"
		wire.Assertions = append(wire.Assertions, assertion.Assertion{ID: []string{"booking-accepted", rdAssertion, "update-accepted"}[i], Operator: assertion.FieldEquals, Subject: assertion.Subject{Field: &assertion.FieldRef{Scope: assertion.ObservedMessages, Message: message, Selector: "MSA-1"}}, Expected: assertion.Expected{Field: &assertion.FieldValue{State: "present", Text: &text}}})
	}
	flow.Phases = []connectedtest.FlowPhase{{ID: "trial", Steps: []string{"step0", "step1", "step2"}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{ds}, Checks: ref("checks", assertion.DatasetSchema, "checks.json", checks), Wire: &connectedtest.WireChecks{Set: ref("wire", assertion.Schema, "wire.json", wire), Observed: "transport-acks"}}}
	raw, _ := json.Marshal(flow)
	plan, err := connectedtest.CompileFlow(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, "connected-plan")
	if err = plan.Write(t.Context(), planPath); err != nil {
		t.Fatal(err)
	}
	grant := connectedrun.Grant{Path: "unprovisioned.json", Actor: "desktop", Generation: "selected"}
	writeFixtureJSON(t, root, "connected-config.json", connectedrun.FlowConfig{Schema: connectedrun.FlowConfigSchema, Isolation: connectedrun.IsolationSelection{Registry: filepath.Join(root, "fixture-registry.json"), Policy: "connected-policy.json", Read: grant, Setup: grant, Cleanup: grant}, Phases: map[string]connectedrun.ConfigV2{"trial": {Schema: connectedrun.ConfigSchemaV2, Definition: connectedrun.Config{Schema: connectedrun.ConfigSchema, Case: "incident", Target: paths["target"], Policy: "connected-policy.json", Send: grant, Sources: map[string]connectedrun.SourceSelection{"after": {Path: "application-source.json"}}}, Barriers: map[string]connectedrun.SourceSelection{}}}})
	review, err := expectation.ReviewConnected(planPath)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.ApproveConnected(planPath, "", review.Identity(), "Fixture owner", "Approve exact synthetic expectations", filepath.Join(root, "connected-release.json"))
	if err != nil {
		t.Fatal(err)
	}
	suiteDocument := suite.ConnectedDocument{Schema: suite.ConnectedSchema, ID: "connected-reproducer", Owner: "Fixture owner", Tags: []string{}, Parallelism: 1, Tests: []suite.ConnectedTest{{ID: review.ID, Revision: review.Revision, Definition: review.Definition, Release: "connected-release.json", ReleaseIdentity: release.Identity(), After: []string{}, State: "enabled"}}, Environments: []suite.ConnectedEnvironment{{ID: "qa", Bindings: []suite.ConnectedBinding{{Test: review.ID, Plan: "connected-plan", PlanIdentity: plan.Identity(), Config: "connected-config.json"}}}}}
	prepared, err := connectedrun.PrepareFlow(planPath, filepath.Join(root, "connected-config.json"), "fixture-review")
	if err != nil {
		t.Fatalf("the fixture's selected lifecycle does not prepare: %v", err)
	}
	if _, err = prepared.InputIdentity(); err != nil {
		t.Fatalf("fixture input identity: %v", err)
	}
	if _, err = prepared.Capabilities(); err != nil {
		t.Fatalf("fixture capabilities: %v", err)
	}
	savedSuite := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.SuiteItem, IntentID: "connected-suite", Draft: desktop.ItemDraft{Name: "Approved connected reproducer", Suite: &desktop.SuiteDraft{Connected: &desktop.ConnectedSuiteDraft{Document: suiteDocument}}}})
	if savedSuite.Saved == nil {
		t.Fatalf("save connected suite: %+v", savedSuite)
	}
	return app, context, run, receiver, fixture
}
func readFixtureDocument(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err, path)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatal(err, path)
	}
}

func writeFixtureJSON(t *testing.T, root, entry string, value any) {
	t.Helper()
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, entry, string(raw))
}

func TestApprovedConnectedLifecycleMinimizesTheFailedRunThroughTheProductionFacade(t *testing.T) {
	parallelLifecycleTest(t)
	app, context, run, receiver, fixture := connectedMinimizeProject(t)
	before := receiver.connections()
	setup := app.MinimizeSetup(desktop.RunRequest{Context: context, Run: run})
	if setup.Setup == nil || len(setup.Setup.Connected) != 1 || !setup.Setup.Connected[0].Eligible {
		t.Fatalf("connected choice: %+v", setup)
	}
	if receiver.connections() != before {
		t.Fatal("passive setup sent")
	}
	options := desktop.MinimizeOptions{Checks: []string{rdAssertion}, Grouping: reduce.GroupPerOccurrence, Trials: 16, Confirmations: 1, Connected: &setup.Setup.Connected[0].Options}
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.MinimizeFailureAction, Items: []desktop.ItemRef{run}, Minimize: &options})
	if !review.Ready || review.Minimize.Connected == nil || len(review.Minimize.Connected.Checks.Assertions) != 1 || review.Isolation == nil {
		t.Fatalf("connected review: %+v", review)
	}
	confirmed := []string{}
	for _, action := range review.Reset.Actions {
		confirmed = append(confirmed, action.ID)
	}
	result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "connected-reduction", Decisions: desktop.ReviewDecisions{Confirmed: confirmed}})
	if result.Minimize == nil || result.Minimize.Outcome != reduce.OutcomeReduced || result.Minimize.Variant == nil || result.Minimize.Minimality != reduce.GroupOneMinimal {
		t.Fatalf("connected result: %+v", result)
	}
	retained := []string{}
	for _, message := range result.Minimize.Retained {
		retained = append(retained, message.ID)
	}
	if !slices.Equal(retained, rdSequence[:2]) {
		t.Fatalf("lost same defect: %v", retained)
	}
	fixture.mu.Lock()
	created, deleted, held := fixture.created, fixture.deleted, fixture.lease.Owner
	fixture.mu.Unlock()
	if created != len(result.Minimize.Trials) || deleted != created || held != "" {
		t.Fatalf("each trial needs fresh setup and cleanup: %d/%d/%s for %d trials", created, deleted, held, len(result.Minimize.Trials))
	}
	if receiver.connections()-before != len(result.Minimize.Trials) {
		t.Fatal("a connected trial was replayed or skipped")
	}
	for _, trial := range result.Minimize.Trials {
		if trial.Reset != "confirmed" {
			t.Fatalf("unconfirmed trial: %+v", trial)
		}
	}
	variant := listed(t, app, context.Project, desktop.VariantItem)["Rescheduling incident minimized"]
	if variant.Ref.ID != result.Minimize.Variant.ID || variant.Summary.Variant == nil || variant.Summary.Variant.Parent == nil {
		t.Fatalf("derived variant lineage: %+v", variant)
	}
}

func TestConnectedMinimizationStopsForCleanupUncertaintyOrAChangedFailure(t *testing.T) {
	parallelLifecycleTest(t)
	for _, failure := range []string{"cleanup", "signature"} {
		t.Run(failure, func(t *testing.T) {
			app, context, run, receiver, fixture := connectedMinimizeProject(t)
			setup := app.MinimizeSetup(desktop.RunRequest{Context: context, Run: run})
			if setup.Setup == nil || len(setup.Setup.Connected) != 1 || !setup.Setup.Connected[0].Eligible {
				t.Fatalf("connected choice: %+v", setup)
			}
			options := desktop.MinimizeOptions{Checks: []string{rdAssertion}, Grouping: reduce.GroupPerOccurrence, Trials: 16, Confirmations: 1, Connected: &setup.Setup.Connected[0].Options}
			review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.MinimizeFailureAction, Items: []desktop.ItemRef{run}, Minimize: &options})
			if !review.Ready {
				t.Fatalf("connected review: %+v", review)
			}
			if failure == "cleanup" {
				fixture.mu.Lock()
				fixture.denyCleanup = true
				fixture.mu.Unlock()
			} else {
				writeDocument(t, context.Project, "application.csv", "key\nUNEXPECTED\n")
			}
			before := receiver.connections()
			confirmed := []string{}
			for _, action := range review.Reset.Actions {
				confirmed = append(confirmed, action.ID)
			}
			result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "uncertain-reduction", Decisions: desktop.ReviewDecisions{Confirmed: confirmed}})
			want := reduce.CleanupUnresolved
			if failure == "signature" {
				want = reduce.FailureSignatureChanged
			}
			if result.Minimize == nil || result.Minimize.Outcome != reduce.OutcomeUndecided || result.Minimize.Reason != want || result.Minimize.Minimality != reduce.NoClaim || result.Minimize.Variant != nil || len(result.Minimize.Trials) != 1 {
				t.Fatalf("unresolved reduction became a reproducer: %+v", result)
			}
			if receiver.connections()-before != 1 || len(listed(t, app, context.Project, desktop.VariantItem)) != 0 {
				t.Fatal("an unresolved trial was repeated or published")
			}
			if failure == "cleanup" && result.Outcome != desktop.ActionUncertain {
				t.Fatalf("cleanup uncertainty omitted: %+v", result)
			}
			if _, err := connectedrun.OpenFlow(t.Context(), filepath.Join(context.Project, result.Minimize.Output, "t0001")); err != nil {
				t.Fatal("original failed trial was not retained", err)
			}
		})
	}
}
