package desktop_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"encoding/pem"
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
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testisolation"
)

// This target knows only ordinary MLLP framing, MSH and an atomic CSV export.
// It uses no readmit parser, sender, generator, evaluator or receipt writer.
// An AA witnesses transport acceptance; it makes no business-workflow claim.
type scenarioByteWitness struct {
	listener net.Listener
	file     string
	mu       sync.Mutex
	frames   [][]byte
	controls []string
	errors   []string
	done     sync.WaitGroup
}

func startScenarioByteWitness(t *testing.T, root string) *scenarioByteWitness {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	w := &scenarioByteWitness{listener: l, file: filepath.Join(root, "received.csv")}
	if err = os.WriteFile(w.file, []byte("control\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w.done.Add(1)
	go func() {
		defer w.done.Done()
		for {
			connection, err := l.Accept()
			if err != nil {
				return
			}
			w.done.Add(1)
			go func() {
				defer w.done.Done()
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(time.Minute))
				reader := bufio.NewReader(connection)
				for {
					frame, err := reader.ReadBytes(28)
					if err != nil {
						return
					}
					terminator, err := reader.ReadByte()
					if err != nil {
						return
					}
					if len(frame) < 2 || frame[0] != 11 || terminator != 13 {
						w.mu.Lock()
						w.errors = append(w.errors, "invalid MLLP envelope")
						w.mu.Unlock()
						return
					}
					header := strings.Split(strings.Split(string(frame[1:len(frame)-1]), "\r")[0], "|")
					if len(header) < 12 || header[0] != "MSH" {
						w.mu.Lock()
						w.errors = append(w.errors, "missing MSH")
						w.mu.Unlock()
						return
					}
					control := header[9]
					w.mu.Lock()
					w.frames = append(w.frames, append(bytes.Clone(frame), terminator))
					w.controls = append(w.controls, control)
					csv := "control\n" + strings.Join(w.controls, "\n") + "\n"
					// A downstream system publishes a whole export, so acquisition
					// can never read a half-written row while stimulus is running.
					temporary := w.file + ".new"
					err = os.WriteFile(temporary, []byte(csv), 0600)
					if err == nil {
						err = os.Rename(temporary, w.file)
					}
					if err != nil {
						w.errors = append(w.errors, "cannot publish received-byte witness")
					}
					w.mu.Unlock()
					if err != nil {
						return
					}
					ack := "MSH|^~\\&|BYTE-WITNESS|SYNTHETIC|READMIT|SYNTHETIC|20260101120000||ACK|WITNESS-ACK|T|2.5.1\rMSA|AA|" + control + "\r"
					_, err = connection.Write(append(append([]byte{11}, []byte(ack)...), 28, 13))
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = l.Close(); w.done.Wait() })
	return w
}

type scenarioConnectedCase struct {
	family, scenario string
	steps, events    []string
	orders, results  []string
}

// Events and step identities are independently stated from the authored
// synthetic lifecycles. Saved business times, wire parameters and real seed
// remain inputs; only generation-variant delays govern connected execution.
func TestSavedScenarioCasesReachConnectedExecutionForEveryV2Family(t *testing.T) {
	for _, tc := range []scenarioConnectedCase{
		{family: "ADT", scenario: "adt-visit-lifecycle", steps: []string{"register", "admit", "transfer", "update", "cancel-a-discharge-that-never-happened", "discharge", "cancel-an-admission-already-discharged", "cancel-the-discharge", "admit-the-second-visit", "merge-a-patient-into-itself", "merge-the-duplicate-identity", "merge-an-identity-already-merged-away", "update-the-visit-of-a-merged-identity", "merge-into-an-identity-already-merged-away"}, events: []string{"ADT^A04", "ADT^A01", "ADT^A02", "ADT^A08", "ADT^A13", "ADT^A03", "ADT^A11", "ADT^A13", "ADT^A01", "ADT^A40", "ADT^A40", "ADT^A40", "ADT^A08", "ADT^A40"}},
		{family: "SIU", scenario: "siu-appointment-lifecycle", steps: []string{"cancel-before-anything-was-booked", "book", "book-the-same-appointment-again", "reschedule", "modify", "cancel", "cancel-the-cancellation", "reschedule-after-cancellation", "record-a-no-show-after-cancellation"}, events: []string{"SIU^S15", "SIU^S12", "SIU^S12", "SIU^S13", "SIU^S14", "SIU^S15", "SIU^S15", "SIU^S13", "SIU^S26"}},
		{family: "ORM", scenario: "orm-lifecycle", steps: []string{"step-1", "step-2", "step-3", "step-4", "step-5", "step-6"}, events: []string{"ORM^O01", "ORM^O01", "ORM^O01", "ORM^O01", "ORM^O01", "ORM^O01"}, orders: []string{"CA", "NW", "XO", "CA", "XO", "NW"}},
		{family: "ORU", scenario: "oru-lifecycle", steps: []string{"step-1", "step-2", "step-3", "step-4", "step-5", "step-6"}, events: []string{"ORU^R01", "ORU^R01", "ORU^R01", "ORU^R01", "ORU^R01", "ORU^R01"}, results: []string{"C", "P", "P", "F", "P", "C"}},
	} {
		t.Run(tc.family, func(t *testing.T) { savedScenarioConnectedCase(t, tc) })
	}
}

func savedScenarioConnectedCase(t *testing.T, tc scenarioConnectedCase) {
	t.Helper()
	app, context := namedProject(t)
	root := context.Project
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		retained, err := os.MkdirTemp("", "readmit-saved-scenario-connected-failure-")
		if err != nil {
			t.Log(err)
			return
		}
		if err = os.CopyFS(filepath.Join(retained, "evidence"), os.DirFS(root)); err != nil {
			t.Log(err)
		}
		t.Logf("failed saved-scenario fixture retained at %s", retained)
	})
	writeDocument(t, root, "pack.json", string(casegenFixture(t, "owned/pack.json")))
	packs := app.MetadataPacks(context)
	if packs.State != desktop.Completed || len(packs.Packs) != 1 {
		t.Fatalf("saved pack: %+v", packs)
	}
	profile, err := localprofile.Decode(casegenFixture(t, "owned/profile-"+strings.ToLower(tc.family)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	published := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, IntentID: "connected-profile", Draft: desktop.ItemDraft{Name: tc.family + " local requirements", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &packs.Packs[0].Item.Ref}}})
	if published.Saved == nil {
		t.Fatalf("save profile: %+v", published)
	}
	var request casegen.Request
	if err = json.Unmarshal(casegenFixture(t, "request-"+strings.ToLower(tc.family)+".json"), &request); err != nil {
		t.Fatal(err)
	}
	const seed = 8675309
	scenario := scenariogen.Plan{Schema: scenariogen.Schema, GeneratorVersion: scenariogen.Version, Seed: seed, Template: request.Scenario, Rows: []scenariogen.Row{{ID: "plain", PatientName: "SYNTHETIC", Notes: request.Rows[0].Notes, Encoding: "utf-8"}}, Variants: []scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}}}
	settings := &desktop.CaseGenerationSettings{Wire: request.Wire, Bindings: request.Bindings, Variants: []casegen.Variant{}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, IntentID: "connected-scenario", Draft: desktop.ItemDraft{Name: tc.family + " saved lifecycle", Scenario: &desktop.ScenarioDraft{Plan: scenario, Profile: published.Saved, Generation: settings}}})
	if saved.Saved == nil {
		t.Fatalf("save scenario: %+v", saved)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if reopened.Draft == nil || reopened.Draft.Scenario.Plan.Seed != seed || !reflect.DeepEqual(reopened.Draft.Scenario.Generation, settings) {
		t.Fatalf("saved generation inputs changed: %+v", reopened)
	}
	generated := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: *saved.Saved, IntentID: "connected-original-bytes"})
	if generated.State != desktop.Completed || len(generated.Cases) != 1 || generated.Family != tc.family || generated.Seed != seed || generated.BaseTime != "2026-01-01T12:00:00Z" {
		t.Fatalf("actual saved generation: %+v", generated)
	}
	c := generated.Cases[0]
	if c.Messages != len(tc.steps) || len(c.Phases) != len(tc.steps) {
		t.Fatalf("saved lifecycle count: %+v", c)
	}
	recordRaw, err := os.ReadFile(filepath.Join(root, generated.Record))
	if err != nil {
		t.Fatal(err)
	}
	record, err := casegen.ReadRecord(recordRaw)
	if err != nil {
		t.Fatal(err)
	}
	if record.Ancestry.Source == nil || record.Ancestry.Source.Item != saved.Saved.ID || record.Ancestry.Source.Revision != saved.Saved.Revision || record.Ancestry.Seed != seed {
		t.Fatalf("generation lost saved revision provenance: %+v", record.Ancestry)
	}
	input, err := bundle.Open(filepath.Join(root, c.Entry))
	if err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	project := document.Project.ID
	var profilePath string
	for _, member := range document.Items[document.Find(published.Saved.ID)].Current().Members {
		if member.Role == "profile" {
			profilePath = filepath.Join(root, member.Path)
		}
	}
	profileBytes, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	requestBytes, err := json.Marshal(record.Request, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	direct, err := casegen.Generate(t.Context(), requestBytes, profileBytes, casegenFixture(t, "owned/pack.json"), casegen.Options{Source: &casegen.Source{Item: saved.Saved.ID, Revision: saved.Saved.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	directDirectory := filepath.Join(root, "direct-generation")
	if err = os.Mkdir(directDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	written, err := direct.Write(t.Context(), directDirectory, casegen.Placement{Record: "record.json", Entry: func(casegen.Case) string { return "baseline" }})
	if err != nil {
		t.Fatal(err)
	}
	original, err := bundle.Open(filepath.Join(directDirectory, written.Cases[0].Entry))
	if err != nil || original.Identity != input.Identity {
		t.Fatal("the saved facade case differs from the shared generator's original bundle", err)
	}
	witness := startScenarioByteWitness(t, root)
	fixture := &minimizeFixture{project: project}
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	provider := filepath.Join(root, "scenario-fixture-credential.sh")
	if err = os.WriteFile(provider, []byte("#!/bin/sh\nprintf 'connected-%s' \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: project, Environment: "qa", Revision: "1", Rules: []sendpolicy.ScopeRule{}}
	allow := func(name, address string, operation sendpolicy.Operation) {
		_, port, _ := net.SplitHostPort(address)
		number, _ := strconv.Atoi(port)
		policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: name, Operation: operation, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
	}
	allow("receiver", witness.listener.Addr().String(), sendpolicy.V2Stimulus)
	role := func(name string, operation sendpolicy.Operation) testisolation.Credential {
		allow("fixture-"+name, server.Listener.Addr().String(), operation)
		return testisolation.Credential{Endpoint: "fixture-" + name, Reference: networkaction.Credential{Endpoint: server.Listener.Addr().String(), Purpose: operation, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: provider, Arguments: []string{name}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "fixture", Revision: "1", Project: project, Environment: "qa", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "synthetic", Namespace: "minimize", URL: server.URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), Read: role("read", sendpolicy.ObservationRead), Setup: role("setup", sendpolicy.SetupAction), Cleanup: role("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}, TimeoutMS: 30000}}}
	writeFixtureJSON(t, root, "scenario-registry.json", registry)
	writeFixtureJSON(t, root, "scenario-network-policy.json", policy)
	policyRaw, err := os.ReadFile(filepath.Join(root, "scenario-network-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, Name: "byte-witness", Classification: replay.Nonproduction, TestEndpoint: true, Address: witness.listener.Addr().String(), Transport: "plain", ApprovedTransport: true, ConnectTimeout: "30s", MessageTimeout: "30s", MaxACKBytes: 4096}
	writeFixtureJSON(t, root, "scenario-target.json", target)
	rp, err := replay.Prepare(filepath.Join(root, c.Entry), target, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Preparation reads the target identity; execution below uses the complete
	// unchanged scheduled connected plan and every declared phase delay.
	extraction := &observesource.Extraction{Envelope: importer.CSVEnvelope, Encoding: importer.UTF8, CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 1}, RecordKey: importer.Locator{"control"}}
	source := observesource.Source{Schema: observesource.SchemaV1, Observes: observewindow.Source{Kind: observesource.FileExport, Identity: "independent-byte-witness", Scope: "received"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "1h"}, Extraction: extraction, File: &observesource.File{Path: witness.file, MaxBytes: 65536}}
	if err = observesource.WriteSource(filepath.Join(root, "scenario-witness-source.json"), source); err != nil {
		t.Fatal(err)
	}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "received", Format: "csv", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, CSV: extraction.CSV}, Columns: []dataset.Column{{Name: "control", Type: "text", Locator: importer.Locator{"control"}, Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 32, MaxBytes: 65536, TimeoutMS: 30000}}
	files := map[string][]byte{}
	ref := func(id, schema, file string, raw []byte) connectedtest.Reference {
		files[file] = raw
		return connectedtest.Reference{Project: project, ID: id, Schema: schema, File: file, SHA256: dataset.Digest(raw)}
	}
	refJSON := func(id, schema, file string, value any) connectedtest.Reference {
		raw, err := json.Marshal(value, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		return ref(id, schema, file, raw)
	}
	contract := testisolation.Contract{Schema: testisolation.ContractSchema, Project: project, Environment: "qa", Revision: "1", Adapter: "fixture", Tenant: "synthetic", Namespace: "minimize", Mode: "isolated-tenant", Concurrency: "exclusive-target-lease", Resources: []testisolation.Requirement{{ID: "patient", Kind: "patient", Template: "patient", Ownership: "create", DependsOn: []string{}, Attributes: map[string]string{"name": "Synthetic"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "business", Value: "scenario-fixture"}}}}, Manual: []testisolation.ManualStep{}}
	flow := connectedtest.FlowTest{Schema: connectedtest.ScheduledFlowTestSchema, Project: project, ID: "saved-" + strings.ToLower(tc.family), Revision: "1", Environment: connectedtest.Environment{Project: project, ID: "qa", Revision: "1", Name: target.Name, Classification: "nonproduction", Endpoint: "receiver", TargetIdentity: rp.Target().Identity(), AddressPolicyIdentity: dataset.Digest(policyRaw), TLS: connectedtest.TLS{Mode: "plain"}, TargetRevision: connectedtest.TargetRevision{Provenance: "operator-declared", Value: "independent-synthetic-target"}}, Isolation: refJSON("isolation", testisolation.ContractSchema, "scenario-isolation.json", contract), Boundary: "engine-output", Limits: connectedtest.Limits{MaxSteps: 32, MaxBytes: 4 << 20, DeadlineMS: 600000}, Schedule: &connectedtest.Schedule{Generation: ref("saved-generation", casegen.RecordSchema, "saved-generation.json", recordRaw), Row: "plain", Variant: "baseline"}}
	projectionRef := refJSON("projection", dataset.ProjectionSchema, "scenario-projection.json", projection)
	interval := observeinterval.Definition{Schema: observeinterval.Schema, Source: source.Identity(), Namespace: "received", Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 25, SampleMS: 10, MaxGapMS: 30000, MaxSamples: 100, MaxRecords: 32, MaxBytes: 65536}
	intervalRef := refJSON("interval", observeinterval.Schema, "scenario-interval.json", interval)
	grant := func(name string) connectedrun.Grant {
		return connectedrun.Grant{Path: name + "-grant.json", Actor: "scenario-runner", Generation: "fresh"}
	}
	config := connectedrun.FlowConfig{Schema: connectedrun.FlowConfigSchema, Isolation: connectedrun.IsolationSelection{Registry: "scenario-registry.json", Policy: "scenario-network-policy.json", Read: grant("isolation-read"), Setup: grant("isolation-setup"), Cleanup: grant("isolation-cleanup")}, Phases: map[string]connectedrun.ConfigV2{}}
	controls := []string{}
	for i, step := range tc.steps {
		occurrence := record.Cases[0].Occurrences[i]
		if occurrence.Ordinal != i+1 || occurrence.Step != step || record.Cases[0].Phases[i].ID != step || !slices.Equal(record.Cases[0].Phases[i].Occurrences, []int{i + 1}) {
			t.Fatalf("saved step mapping was rewritten: %+v", occurrence)
		}
		// MSH-10 is independently derived from the documented generator rule,
		// using literal version/scenario/row/step and the saved seed.
		sum := sha256.Sum256([]byte("readmit-case-generator-v1\x008675309\x00" + tc.scenario + "\x001\x00plain\x00" + step))
		control := "C" + strings.ToUpper(hex.EncodeToString(sum[:])[:16])
		controls = append(controls, control)
		if occurrence.ControlID != control {
			t.Fatalf("step %s has an unverified generated control ID", step)
		}
		raw, err := os.ReadFile(filepath.Join(root, c.Entry, filepath.FromSlash(occurrence.Payload)))
		if err != nil {
			t.Fatal(err)
		}
		body, err := input.Raw(occurrence.CaseEvent)
		if err != nil || !bytes.Equal(raw, body) {
			t.Fatal("mapped generation bytes differ from the actual saved case", err)
		}
		stepRef := ref("message-"+strconv.Itoa(i+1), "hl7", "scenario-message-"+strconv.Itoa(i+1)+".hl7", raw)
		flow.Steps = append(flow.Steps, connectedtest.Step{ID: step, Endpoint: "receiver", After: []string{}, BusinessKeys: []connectedtest.BusinessKey{}, V2: &connectedtest.V2Stimulus{Input: stepRef, Occurrence: occurrence.CaseEvent, Assignments: []connectedtest.Assignment{}}})
		ds := connectedtest.Dataset{ID: "after", Kind: "typed-rows", Namespace: "received", Phase: "after", Source: source.Identity(), Projection: &projectionRef, Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: 25, MaxRecords: 32, MaxBytes: 65536, Policy: &intervalRef}}
		count := i + 1
		checks := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{{Name: "after", Namespace: "received", Phase: "after", Source: source.Identity(), ProjectionIdentity: projection.Identity()}}, Assertions: []assertion.DatasetAssertion{{ID: "received-count", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &count}}}
		aa := "AA"
		wire := assertion.Set{Schema: assertion.Schema, Name: "Transport receipt", Assertions: []assertion.Assertion{{ID: "received", Operator: assertion.FieldEquals, Subject: assertion.Subject{Field: &assertion.FieldRef{Scope: assertion.ObservedMessages, Message: occurrence.CaseEvent, Selector: "MSA-1"}}, Expected: assertion.Expected{Field: &assertion.FieldValue{State: "present", Text: &aa}}}}}
		phase := connectedtest.FlowPhase{ID: step, Steps: []string{step}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{ds}, Checks: refJSON("checks-"+strconv.Itoa(i+1), assertion.DatasetSchema, "scenario-checks-"+strconv.Itoa(i+1)+".json", checks), Wire: &connectedtest.WireChecks{Set: refJSON("wire-"+strconv.Itoa(i+1), assertion.Schema, "scenario-wire-"+strconv.Itoa(i+1)+".json", wire), Observed: "transport-acks"}}
		if i > 0 {
			phase.After = []connectedtest.PhaseDependency{{Phase: tc.steps[i-1], Requires: "pass"}}
		}
		flow.Phases = append(flow.Phases, phase)
		config.Phases[step] = connectedrun.ConfigV2{Schema: connectedrun.ConfigSchemaV2, Definition: connectedrun.Config{Schema: connectedrun.ConfigSchema, Case: c.Entry, Target: "scenario-target.json", Policy: "scenario-network-policy.json", Send: grant(step), Sources: map[string]connectedrun.SourceSelection{"after": {Path: "scenario-witness-source.json"}}}, Barriers: map[string]connectedrun.SourceSelection{}}
	}
	raw, err := json.Marshal(flow, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := connectedtest.CompileFlow(raw, files, connectedtest.Generation{Seed: seed, BaseTime: "2026-01-01T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, "scenario-connected-plan")
	if err = plan.Write(t.Context(), planPath); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "scenario-connected-config.json")
	writeFixtureJSON(t, root, "scenario-connected-config.json", config)
	instance := "saved-" + strings.ToLower(tc.family) + "-occurrence"
	prepared, err := connectedrun.PrepareFlow(planPath, configPath, instance)
	if err != nil {
		t.Fatal(err)
	}
	for name, binding := range prepared.Bindings() {
		file := strings.ReplaceAll(name, ":", "-") + "-grant.json"
		writeFixtureJSON(t, root, file, networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "scenario-runner", Generation: "fresh", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
	// The stimulus binding's role suffix is not part of the authored step ID.
	for _, step := range tc.steps {
		writeFixtureJSON(t, root, step+"-grant.json", networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "scenario-runner", Generation: "fresh", Binding: prepared.Bindings()[step+":stimulus"], IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
	witness.mu.Lock()
	before := len(witness.frames)
	witness.mu.Unlock()
	if before != 0 {
		t.Fatal("compilation/preparation sent generated bytes")
	}
	output := filepath.Join(root, "scenario-connected-result")
	result, err := connectedrun.ExecuteFlow(t.Context(), prepared, output, testisolation.Confirmation{Plan: prepared.IsolationIdentity(), Instance: instance})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "complete" || result.Verdict != assertion.VerdictPass || result.Setup != "ready" || result.Cleanup != "complete" || len(result.Phases) != len(tc.steps) {
		t.Fatalf("saved case did not execute completely: %+v", result)
	}
	witness.mu.Lock()
	frames := slices.Clone(witness.frames)
	observedControls := slices.Clone(witness.controls)
	errors := slices.Clone(witness.errors)
	witness.mu.Unlock()
	if len(errors) != 0 || len(frames) != len(tc.steps) || !slices.Equal(observedControls, controls) {
		t.Fatalf("independent witness: %d frames, controls %v, errors %v", len(frames), observedControls, errors)
	}
	for i, frame := range frames {
		o := record.Cases[0].Occurrences[i]
		body, err := input.Raw(o.CaseEvent)
		if err != nil {
			t.Fatal(err)
		}
		originalBody, err := original.Raw(o.CaseEvent)
		if err != nil || !bytes.Equal(originalBody, body) {
			t.Fatal("original generator/facade bundle bytes differ", err)
		}
		want := append(append([]byte{11}, body...), 28, 13)
		if !bytes.Equal(frame, want) {
			t.Fatalf("step %s did not send original generated bytes", tc.steps[i])
		}
		header := strings.Split(strings.Split(string(frame[1:len(frame)-2]), "\r")[0], "|")
		if len(header) < 12 || !strings.HasPrefix(header[8], tc.events[i]+"^") || header[9] != controls[i] || header[10] != "T" || header[11] != "2.5.1" {
			t.Fatalf("independent expected event/identity/version: %v", header)
		}
		for _, segment := range strings.Split(string(frame[1:len(frame)-2]), "\r") {
			fields := strings.Split(segment, "|")
			if tc.family == "ORM" && fields[0] == "ORC" && (len(fields) <= 1 || fields[1] != tc.orders[i]) {
				t.Fatalf("order lifecycle action changed at %s: %v", tc.steps[i], fields)
			}
			if tc.family == "ORU" && fields[0] == "OBR" && (len(fields) <= 25 || fields[25] != tc.results[i]) {
				t.Fatalf("result lifecycle status changed at %s: %v", tc.steps[i], fields)
			}
		}
		phase := result.Phases[i]
		if phase.ID != tc.steps[i] || phase.State != "complete" || phase.Verdict != assertion.VerdictPass || len(phase.Steps) != 1 || phase.Steps[0].Step != tc.steps[i] || phase.Steps[0].Outcome != "complete" {
			t.Fatalf("retained mapping changed: %+v", phase)
		}
		transport, err := connectedtransport.OpenEvidence(filepath.Join(output, "phases", tc.steps[i], "transport"))
		if err != nil || !slices.Equal(transport.Schedule, []time.Duration{0}) {
			t.Fatalf("original baseline schedule not retained: %+v %v", transport.Schedule, err)
		}
	}
	fixture.mu.Lock()
	created, deleted, held := fixture.created, fixture.deleted, fixture.lease.Owner
	fixture.mu.Unlock()
	if created != 1 || deleted != 1 || held != "" {
		t.Fatalf("lifecycle isolation was not owned/cleaned: %d/%d/%s", created, deleted, held)
	}
	reopenedResult, err := connectedrun.OpenFlow(t.Context(), output)
	if err != nil || !reflect.DeepEqual(reopenedResult, result) {
		t.Fatal("saved connected result does not reopen to its original evidence", err)
	}
	witness.mu.Lock()
	after := len(witness.frames)
	witness.mu.Unlock()
	if after != len(tc.steps) {
		t.Fatal("offline reopen resent generated bytes")
	}
}
