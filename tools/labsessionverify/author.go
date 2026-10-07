package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/smartbackend"
	"github.com/bharm16/readmit/internal/testisolation"
)

func grantFile(name string) string { return strings.ReplaceAll(name, ":", "-") + "-grant.json" }
func grant(name string) connectedrun.Grant {
	return connectedrun.Grant{Path: grantFile(name), Actor: "operator", Generation: "1"}
}
func (d *driver) author(a *sessionAdapter, patient map[string]any) error {
	input := filepath.Join(d.root, "inputs")
	if err := os.Mkdir(input, 0700); err != nil {
		return err
	}
	files := map[string][]byte{}
	ref := func(id, schema, name string, value any) connectedtest.Reference {
		raw, ok := value.([]byte)
		if !ok {
			raw, _ = json.Marshal(value)
		}
		files[name] = raw
		return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: name, SHA256: dataset.Digest(raw)}
	}
	rawMessages := [][]byte{}
	wire := []byte{}
	for _, name := range []string{"siu-s12.hl7", "siu-s13.hl7"} {
		raw, e := d.stimulus(name)
		if e != nil {
			return e
		}
		rawMessages = append(rawMessages, raw)
		wire = append(wire, 11)
		wire = append(wire, raw...)
		wire = append(wire, 28, 13)
	}
	now := time.Now()
	source, err := bundle.Write(filepath.Join(d.root, "case"), []bundle.Input{{Path: "session-input.mllp", Data: wire}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		return err
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, Name: "owned-oie-session", Classification: replay.Nonproduction, TestEndpoint: true, Address: net.JoinHostPort(d.connection.MLLP.Host, strconv.Itoa(d.connection.MLLP.Port)), Transport: "plain", ApprovedTransport: true, ConnectTimeout: "10s", MessageTimeout: "30s", MaxACKBytes: 65536}
	if err = writeJSON(filepath.Join(d.root, "target.json"), target); err != nil {
		return err
	}
	rp, err := replay.Prepare(filepath.Join(d.root, "case"), target, replay.Options{})
	if err != nil {
		return err
	}
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1"}
	allow := func(endpoint, address string, op sendpolicy.Operation) {
		host, port, _ := net.SplitHostPort(address)
		n, _ := strconv.Atoi(port)
		policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: endpoint, Operation: op, Port: n, Destinations: []string{host + "/32"}, Selection: "single-address"})
	}
	allow("receiver", target.Address, sendpolicy.V2Stimulus)
	fixtureAddress := a.server.Listener.Addr().String()
	for role, token := range a.tokens {
		if err = os.WriteFile(filepath.Join(d.root, "fixture-"+role+"-token"), []byte(token), 0600); err != nil {
			return err
		}
	}
	cred := func(role string, op sendpolicy.Operation) testisolation.Credential {
		allow("fixture-"+role, fixtureAddress, op)
		return testisolation.Credential{Endpoint: "fixture-" + role, Reference: networkaction.Credential{Endpoint: fixtureAddress, Purpose: op, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: "/bin/cat", Arguments: []string{filepath.Join(d.root, "fixture-"+role+"-token")}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "session", Revision: "1", Project: "lab", Environment: "test", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: d.session.Generation, Namespace: "session", URL: a.server.URL, ServerName: "example.com", Authorities: a.ca(), Read: cred("read", sendpolicy.ObservationRead), Setup: cred("setup", sendpolicy.SetupAction), Cleanup: cred("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{}}}, TimeoutMS: 30000}}}
	if err = writeJSON(filepath.Join(d.root, "registry.json"), registry); err != nil {
		return err
	}
	u, _ := url.Parse(d.connection.FHIR.Base)
	for _, endpoint := range []string{"lab", "appointments"} {
		allow(endpoint, u.Host, sendpolicy.FHIRMetadata)
		allow(endpoint, u.Host, sendpolicy.FHIRSearch)
	}
	allow("smart-token", u.Host, sendpolicy.SMARTToken)
	if err = writeJSON(filepath.Join(d.root, "policy.json"), policy); err != nil {
		return err
	}
	policyRaw, err := os.ReadFile(filepath.Join(d.root, "policy.json"))
	if err != nil {
		return err
	}
	contract := testisolation.Contract{Schema: testisolation.ContractSchema, Project: "lab", Environment: "test", Revision: "1", Adapter: "session", Tenant: d.session.Generation, Namespace: "session", Mode: "isolated-tenant", Concurrency: "exclusive-target-lease", Resources: []testisolation.Requirement{{ID: "patient", Kind: "patient", Template: "patient", Ownership: "select", LogicalID: a.resource.ID, Version: a.resource.Version, DependsOn: []string{}, Attributes: map[string]string{}, Identifiers: []testisolation.Identifier{}}}, Manual: []testisolation.ManualStep{}}
	capability, err := os.ReadFile(filepath.Join(d.state, "session-evidence", d.session.Generation, "capability.json"))
	if err != nil {
		return err
	}
	flow := connectedtest.FlowTest{Schema: connectedtest.FHIRFlowTestSchema, Project: "lab", ID: "siu-product", Revision: "1", Boundary: "application-state", Environment: connectedtest.Environment{Project: "lab", ID: "test", Revision: "1", Name: target.Name, Classification: "nonproduction", Endpoint: "receiver", TargetIdentity: rp.Target().Identity(), AddressPolicyIdentity: dataset.Digest(policyRaw), TLS: connectedtest.TLS{Mode: "plain"}, TargetRevision: connectedtest.TargetRevision{Value: d.session.Generation + "-" + d.session.Mode, Provenance: "operator-declared"}}, Isolation: ref("isolation", testisolation.ContractSchema, "isolation.json", contract), Servers: []connectedtest.FHIRServer{{ID: "lab", Base: d.connection.FHIR.Base, Capability: ref("capability", "fhir-r4-json", "capability.json", capability)}}, Variables: []connectedtest.Variable{}, Profiles: []connectedtest.Reference{}, Limits: connectedtest.Limits{MaxSteps: 8, MaxBytes: 16 << 20, DeadlineMS: 600000}}
	index := 0
	for i, id := range []string{"book", "move"} {
		occurrence := source.Events[i].ID
		step := connectedtest.Step{ID: id, Endpoint: "receiver", BusinessKeys: []connectedtest.BusinessKey{}, V2: &connectedtest.V2Stimulus{Input: ref(id+"-input", "hl7", id+".hl7", rawMessages[i]), Occurrence: occurrence, Assignments: []connectedtest.Assignment{}}}
		if i > 0 {
			step.After = []string{"book"}
		}
		flow.Steps = append(flow.Steps, step)
		observation := fhirobserve.Observation{Schema: fhirobserve.Schema, ID: "appointments", Server: "lab", Resource: "Appointment", Query: "_tag=" + url.QueryEscape("urn:readmit:independent-lab|"+d.session.Generation) + "&identifier=" + url.QueryEscape("urn:readmit:lab:appointment|lab-appointment-01"), Boundary: "reference-fhir-store", MaxRows: 20, MaxValues: 100, Budget: fhirrest.Budget{Pages: 4, Rows: 100, Bytes: 4 << 20, TimeoutMS: 30000}, Retry: fhirrest.Retry{MaxAttempts: 1}, Columns: []fhirobserve.Column{{Name: "key", Type: "text", Key: true, Required: true, Value: fhirobserve.Value{Kind: "field", Selector: &fhirr4.Selector{Steps: []fhirr4.Step{{Field: "identifier", Index: &index}, {Field: "value"}}}}}, {Name: "start", Type: "datetime", Required: true, Value: fhirobserve.Value{Kind: "field", Selector: &fhirr4.Selector{Steps: []fhirr4.Step{{Field: "start"}}}}}}}
		projection := ref(id+"-observation", fhirobserve.Schema, id+"-observation.json", observation)
		interval := observeinterval.Definition{Schema: observeinterval.Schema, Source: observation.Identity(), Namespace: "appointments", Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 30000, SampleMS: 2000, MaxGapMS: 60000, MaxSamples: 100, MaxRecords: 100, MaxBytes: 8 << 20}
		window := ref(id+"-window", observeinterval.Schema, id+"-window.json", interval)
		ds := connectedtest.Dataset{ID: "appointments", Kind: "fhir-resources", Namespace: "appointments", Phase: "after", Source: observation.Identity(), Projection: &projection, Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: 30000, MaxRecords: 100, MaxBytes: 8 << 20, Policy: &window}}
		expected := []string{"2030-01-02T09:30:00Z", "2030-01-02T10:00:00Z"}[i]
		one := 1
		set := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{{Name: "appointments", Namespace: ds.Namespace, Phase: "after", Source: ds.Source, ProjectionIdentity: observation.ProjectionIdentity()}}, Assertions: []assertion.DatasetAssertion{{ID: "one", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "appointments"}, Count: &one}, {ID: "unique", Operator: "unique-keys", Subject: assertion.RowSelection{Dataset: "appointments"}}, {ID: "start", Operator: "instant-equals", Subject: assertion.RowSelection{Dataset: "appointments"}, Column: "start", Expected: &dataset.Value{State: "present", Type: "datetime", Text: expected, Precision: "second", Timezone: "+00:00"}}}}
		checks := ref(id+"-checks", assertion.DatasetSchema, id+"-checks.json", set)
		ack := ref(id+"-ack", assertion.Schema, id+"-ack.json", []byte(fmt.Sprintf(`{"schema":"readmit-assertion-set/v1","name":"Positive ACK","assertions":[{"id":"accepted","operator":"field_equals","subject":{"field":{"scope":"observed","message":%q,"selector":"MSA-1"}},"when":null,"expected":{"field":{"state":"present","text":"AA"}}}]}`, occurrence)))
		phase := connectedtest.FlowPhase{ID: id, Steps: []string{id}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{ds}, Checks: checks, Wire: &connectedtest.WireChecks{Set: ack, Observed: "transport-acks"}}
		if i > 0 {
			phase.After = []connectedtest.PhaseDependency{{Phase: "book", Requires: "pass"}}
		}
		flow.Phases = append(flow.Phases, phase)
	}
	for name, raw := range files {
		if err = os.WriteFile(filepath.Join(input, name), raw, 0600); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(input, "test.json"), flow)
}
func (d *driver) configure(a *sessionAdapter) error {
	plan, err := connectedtest.OpenFlowPlan(filepath.Join(d.root, "plan"))
	if err != nil {
		return err
	}
	// The provider resolves the operator's existing key only at runtime. Config
	// and retained results carry the reference and public JWK, never key bytes.
	provider := networkaction.Provider{Command: "/bin/cat", Arguments: []string{d.connection.FHIR.PrivateKey}}
	smart := smartbackend.Config{Schema: smartbackend.ConfigSchema, FHIRBase: d.connection.FHIR.Base, TokenEndpoint: d.connection.FHIR.Token, ClientID: d.connection.FHIR.Client, Audience: d.connection.FHIR.Token, Algorithm: "RS384", Role: "observer", Scopes: d.connection.FHIR.Scopes, Key: smartbackend.KeyReference{Kid: "observer", Generation: "1", Locator: provider, JWKS: smartbackend.JWKS{Keys: []smartbackend.JWK{d.jwk()}}}, Token: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: plan.Identity(), Source: networkaction.Digest([]byte("session-observer")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "smart-token", Classification: "nonproduction", Operation: sendpolicy.SMARTToken, Method: "POST", URL: d.connection.FHIR.Token, ServerName: d.connection.FHIR.ServerName, Authorities: d.ca, TimeoutMS: 30000, MaxBytes: 65536}}
	if err = writeJSON(filepath.Join(d.root, "smart.json"), smart); err != nil {
		return err
	}
	token := grant("token:lab:observation")
	server := connectedrun.FHIRServerSelection{Authorities: d.connection.FHIR.Authorities, ServerName: d.connection.FHIR.ServerName, Observation: connectedrun.FHIRAuthorization{Mode: "smart", Client: "smart.json", Token: &token}, Action: connectedrun.FHIRAuthorization{Mode: "none"}}
	config := connectedrun.FHIRFlowConfig{Schema: connectedrun.FHIRFlowConfigSchema, Policy: "policy.json", Servers: map[string]connectedrun.FHIRServerSelection{"lab": server}, Phases: map[string]connectedrun.FHIRPhaseSelection{}, Isolation: connectedrun.IsolationSelection{Registry: "registry.json", Policy: "policy.json", Read: grant("isolation:read"), Setup: grant("isolation:setup"), Cleanup: grant("isolation:cleanup")}, Seed: 7}
	for _, id := range []string{"book", "move"} {
		send := grant(id + ":stimulus")
		config.Phases[id] = connectedrun.FHIRPhaseSelection{Case: "case", Target: "target.json", Send: &send, Sources: map[string]connectedrun.SourceSelection{}, Barriers: map[string]connectedrun.SourceSelection{}, Grants: map[string]connectedrun.Grant{"dataset:appointments": grant(id + ":dataset:appointments")}}
	}
	return writeJSON(filepath.Join(d.root, "config.json"), config)
}
