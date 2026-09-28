package connectedlab

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

// Harness authors v5 flows against the lab as a customer would: explicit
// servers, reviewed requests, observations and independently written checks,
// a scoped policy, an isolation contract and separately provisioned grants.
type Harness struct {
	T                    testing.TB
	Root                 string
	Lab                  *FHIRLab
	Engine               *Engine
	Files                map[string][]byte
	Occurrences          map[string]string
	PlanPath, ConfigPath string
	Plan                 *connectedtest.FlowPlan
	Validation           *connectedrun.ValidationSelection
	HorizonMS            int64
	Authored             []byte
	messages             map[string]string
	observations         map[string]fhirobserve.Observation
	policy               sendpolicy.ScopedPolicy
	targetIdentity       string
	smart                *smartbackend.JWK
	smartProvider        string
}

// Message is one original v2 message a flow step sends, by step ID.
type Message struct{ Step, Raw string }

// New starts the lab, engine and fixture adapter and prepares a case holding
// the messages. Root is where every authored and retained file is written; an
// empty root is a fresh temporary directory.
func New(t testing.TB, root string, messages ...Message) *Harness {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	lab := StartFHIRLab(t)
	h := &Harness{T: t, Root: root, Lab: lab, Engine: StartEngine(t, lab), Files: map[string][]byte{}, Occurrences: map[string]string{}, messages: map[string]string{}, observations: map[string]fhirobserve.Observation{}, HorizonMS: 150}
	fixture := StartFixture(t, func() { lab.Reset(); lab.Seed() })
	wire := []byte{}
	for _, m := range messages {
		wire = append(append(append(wire, 11), []byte(m.Raw)...), 28, 13)
		h.messages[m.Step] = m.Raw
	}
	if len(messages) == 0 {
		wire = append(append([]byte{11}, []byte("MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000000||ADT^A08|UNUSED|P|2.5.1\rPID|1||UNUSED\r")...), 28, 13)
	}
	now := time.Now()
	casePath := filepath.Join(root, "case")
	source, err := bundle.Write(casePath, []bundle.Input{{Path: "independent.mllp", Data: wire}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now})
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range messages {
		h.Occurrences[m.Step] = source.Events[i].ID
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, Name: "Lab", Classification: replay.Nonproduction, TestEndpoint: true, Address: h.Engine.Listener.Addr().String(), Transport: "plain", ApprovedTransport: true, ConnectTimeout: "1s", MessageTimeout: "2s", MaxACKBytes: 4096}
	WriteJSON(t, filepath.Join(root, "target.json"), target)
	rp, err := replay.Prepare(casePath, target, replay.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h.targetIdentity = rp.Target().Identity()
	h.policy = sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1"}
	h.allow("receiver", target.Address, sendpolicy.V2Stimulus)
	// The operator's credential provider for the fixture adapter's three roles.
	provider := filepath.Join(root, "fixture-credential.sh")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf 'lab-%s' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	address := fixture.Server().Listener.Addr().String()
	credential := func(role string, operation sendpolicy.Operation) testisolation.Credential {
		endpoint := "fixture-" + role
		h.allow(endpoint, address, operation)
		return testisolation.Credential{Endpoint: endpoint, Reference: networkaction.Credential{Endpoint: address, Purpose: operation, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: provider, Arguments: []string{role}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "lab-fixture", Revision: "1", Project: "lab", Environment: "test", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "lab-tenant", Namespace: "lab-data", URL: fixture.Server().URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.Server().Certificate().Raw}), Read: credential("read", sendpolicy.ObservationRead), Setup: credential("setup", sendpolicy.SetupAction), Cleanup: credential("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}}, TimeoutMS: 30000}}}
	WriteJSON(t, filepath.Join(root, "registry.json"), registry)
	if err := os.WriteFile(filepath.Join(root, "lab-ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: lab.Server().Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	h.allowFHIR("lab", sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch, sendpolicy.FHIRAction, sendpolicy.SetupAction)
	return h
}

func (h *Harness) allow(endpoint, address string, operation sendpolicy.Operation) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		h.T.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	rule := sendpolicy.ScopeRule{Endpoint: endpoint, Operation: operation, Port: n, Destinations: []string{host + "/32"}, Selection: "single-address"}
	for _, existing := range h.policy.Rules {
		if existing.Endpoint == rule.Endpoint && existing.Operation == rule.Operation {
			return
		}
	}
	h.policy.Rules = append(h.policy.Rules, rule)
}
func (h *Harness) allowFHIR(endpoint string, operations ...sendpolicy.Operation) {
	for _, op := range operations {
		h.allow(endpoint, h.Lab.Server().Listener.Addr().String(), op)
	}
}
func (h *Harness) Ref(id, schema, name string, raw []byte) connectedtest.Reference {
	h.Files[name] = raw
	return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: name, SHA256: dataset.Digest(raw)}
}
func (h *Harness) RefJSON(id, schema, name string, value any) connectedtest.Reference {
	raw, err := json.Marshal(value)
	if err != nil {
		h.T.Fatal(err)
	}
	return h.Ref(id, schema, name, raw)
}

// Select builds a FHIR selector; "field#2" selects one repetition.
func Select(steps ...string) *fhirr4.Selector {
	s := &fhirr4.Selector{}
	for _, step := range steps {
		name, index, indexed := strings.Cut(step, "#")
		st := fhirr4.Step{Field: name}
		if indexed {
			i, _ := strconv.Atoi(index)
			st.Index = &i
		}
		s.Steps = append(s.Steps, st)
	}
	return s
}
func FieldColumn(name, typ, system string, key, required bool, steps ...string) fhirobserve.Column {
	return fhirobserve.Column{Name: name, Type: typ, CodeSystem: system, Key: key, Required: required, Value: fhirobserve.Value{Kind: "field", Selector: Select(steps...)}}
}
func ReferenceColumn(name string, steps ...string) fhirobserve.Column {
	return fhirobserve.Column{Name: name, Type: "text", Required: true, Value: fhirobserve.Value{Kind: "reference", Selector: Select(steps...)}}
}
func IdentityColumn() fhirobserve.Column {
	return fhirobserve.Column{Name: "identity", Type: "text", Required: true, Value: fhirobserve.Value{Kind: "identity"}}
}

// Observe authors one reviewed search and its full-horizon interval.
func (h *Harness) Observe(phase, id, when, resource, query, boundary string, columns ...fhirobserve.Column) connectedtest.Dataset {
	o := fhirobserve.Observation{Schema: fhirobserve.Schema, ID: id, Server: "lab", Resource: resource, Query: query, Boundary: boundary, Columns: columns, MaxRows: 20, MaxValues: 1000, Budget: fhirrest.Budget{Pages: 4, Rows: 100, Bytes: 256 << 10, TimeoutMS: 60000}, Retry: fhirrest.Retry{MaxAttempts: 1}}
	h.observations[o.Identity()] = o
	h.allowFHIR(id, sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch)
	name := phase + "-" + id + "-" + when
	ref := h.RefJSON(name, fhirobserve.Schema, name+".json", o)
	ds := connectedtest.Dataset{ID: id, Kind: "fhir-resources", Namespace: "fhir-" + id, Phase: when, Source: o.Identity(), Projection: &ref, Completion: connectedtest.Completion{Kind: "full-horizon", HorizonMS: h.HorizonMS, MaxRecords: 50, MaxBytes: 512 << 10}}
	interval := observeinterval.Definition{Schema: observeinterval.Schema, Source: o.Identity(), Namespace: ds.Namespace, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: h.HorizonMS, SampleMS: 25, MaxGapMS: 30000, MaxSamples: 400, MaxRecords: 50, MaxBytes: 512 << 10}
	policy := h.RefJSON(name+"-window", observeinterval.Schema, name+"-window.json", interval)
	ds.Completion.Policy = &policy
	return ds
}

// Checks pins an independently authored typed assertion set for datasets.
func (h *Harness) Checks(phase string, datasets []connectedtest.Dataset, assertions ...assertion.DatasetAssertion) connectedtest.Reference {
	set := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{}, Assertions: assertions}
	for _, ds := range datasets {
		set.Bindings = append(set.Bindings, assertion.DatasetBinding{Name: ds.ID, Namespace: ds.Namespace, Phase: ds.Phase, Source: ds.Source, ProjectionIdentity: h.observations[ds.Source].ProjectionIdentity()})
	}
	return h.RefJSON(phase+"-checks", assertion.DatasetSchema, phase+"-checks.json", set)
}
func RowCount(id, ds string, n int) assertion.DatasetAssertion {
	return assertion.DatasetAssertion{ID: id, Operator: "row-count", Subject: assertion.RowSelection{Dataset: ds}, Count: &n}
}
func Unique(id, ds string) assertion.DatasetAssertion {
	return assertion.DatasetAssertion{ID: id, Operator: "unique-keys", Subject: assertion.RowSelection{Dataset: ds}}
}
func Equals(id, ds, column string, v dataset.Value) assertion.DatasetAssertion {
	return assertion.DatasetAssertion{ID: id, Operator: "value-equals", Subject: assertion.RowSelection{Dataset: ds}, Column: column, Expected: &v}
}
func Instant(id, ds, column, text string) assertion.DatasetAssertion {
	v := dataset.Value{State: "present", Type: "datetime", Text: text, Precision: "second", Timezone: "+00:00"}
	return assertion.DatasetAssertion{ID: id, Operator: "instant-equals", Subject: assertion.RowSelection{Dataset: ds}, Column: column, Expected: &v}
}
func Related(id, ds, column, other, otherColumn string) assertion.DatasetAssertion {
	return assertion.DatasetAssertion{ID: id, Operator: "value-related", Subject: assertion.RowSelection{Dataset: ds}, Column: column, Other: &assertion.RowSelection{Dataset: other}, OtherColumn: otherColumn}
}
func Code(text, system string) dataset.Value {
	return dataset.Value{State: "present", Type: "code", Text: text, CodeSystem: system}
}

func (h *Harness) V2Step(id string, after ...string) connectedtest.Step {
	raw := h.messages[id]
	return connectedtest.Step{ID: id, Endpoint: "receiver", After: after, BusinessKeys: []connectedtest.BusinessKey{}, V2: &connectedtest.V2Stimulus{Input: h.Ref(id+"-input", "hl7", id+".hl7", []byte(raw)), Occurrence: h.Occurrences[id], Assignments: []connectedtest.Assignment{}}}
}
func (h *Harness) FHIRStep(id, method, path, body string, headers connectedtest.FHIRHeaders, bind []connectedtest.ResponseBinding, after ...string) connectedtest.Step {
	in := &connectedtest.FHIRInteraction{Server: "lab", Method: method, Path: path, Headers: headers, Bind: bind, Budget: fhirrest.Budget{Pages: 4, Rows: 100, Bytes: 1 << 20, TimeoutMS: 60000}, Retry: fhirrest.Retry{MaxAttempts: 1}}
	if body != "" {
		schema := "fhir-r4-json"
		if method == "PATCH" {
			schema = "json-patch"
		}
		ref := h.Ref(id+"-body", schema, id+"-body.json", []byte(body))
		in.Body = &ref
	}
	if bind == nil {
		in.Bind = []connectedtest.ResponseBinding{}
	}
	return connectedtest.Step{ID: id, Endpoint: "lab", After: after, BusinessKeys: []connectedtest.BusinessKey{}, Interaction: in}
}
func BindID(variable, from, scope string) connectedtest.ResponseBinding {
	return connectedtest.ResponseBinding{Variable: variable, From: from, Multiplicity: "exactly-one", Scope: scope}
}

// Compile pins the flow, writes the policy it names and the runtime selection.
func (h *Harness) Compile(flow connectedtest.FlowTest) {
	h.T.Helper()
	if err := h.TryCompile(flow); err != nil {
		h.T.Fatal(err)
	}
}
func (h *Harness) TryCompile(flow connectedtest.FlowTest) error {
	h.T.Helper()
	WriteJSON(h.T, filepath.Join(h.Root, "policy.json"), h.policy)
	policyRaw, err := os.ReadFile(filepath.Join(h.Root, "policy.json"))
	if err != nil {
		h.T.Fatal(err)
	}
	contract := testisolation.Contract{Schema: testisolation.ContractSchema, Project: "lab", Environment: "test", Revision: "1", Adapter: "lab-fixture", Tenant: "lab-tenant", Namespace: "lab-data", Mode: "isolated-tenant", Concurrency: "exclusive-target-lease", Resources: []testisolation.Requirement{{ID: "patient", Kind: "patient", Template: "patient", Ownership: "create", DependsOn: []string{}, Attributes: map[string]string{"name": "Synthetic lab patient"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "patient-business", Value: "LAB"}}}}, Manual: []testisolation.ManualStep{}}
	if flow.Schema == "" {
		flow.Schema = connectedtest.FHIRFlowTestSchema
	}
	flow.Project, flow.Revision = "lab", "1"
	if flow.ID == "" {
		flow.ID = "fhir-lifecycle"
	}
	if flow.Boundary == "" {
		flow.Boundary = "application-state"
	}
	flow.Environment = connectedtest.Environment{Project: "lab", ID: "test", Revision: "1", Name: "Lab", Classification: "nonproduction", Endpoint: "receiver", TargetIdentity: h.targetIdentity, AddressPolicyIdentity: dataset.Digest(policyRaw), TLS: connectedtest.TLS{Mode: "plain"}, TargetRevision: connectedtest.TargetRevision{Provenance: "operator-declared", Value: "independent-lab"}}
	flow.Isolation = h.RefJSON("isolation", testisolation.ContractSchema, "isolation.json", contract)
	if flow.Servers == nil {
		flow.Servers = []connectedtest.FHIRServer{{ID: "lab", Base: h.Lab.Base(), Capability: h.Ref("lab-capability", "fhir-r4-json", "lab-capability.json", []byte(Capability))}}
	}
	flow.Limits = connectedtest.Limits{MaxSteps: 32, MaxBytes: 16 << 20, DeadlineMS: 600000}
	if flow.Variables == nil {
		flow.Variables = []connectedtest.Variable{}
	}
	if flow.Profiles == nil {
		flow.Profiles = []connectedtest.Reference{}
	}
	raw, err := json.Marshal(flow)
	if err != nil {
		h.T.Fatal(err)
	}
	h.Authored = raw
	plan, err := connectedtest.CompileFlow(raw, h.Files, connectedtest.Generation{Seed: 7, BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		return err
	}
	h.Plan = plan
	h.PlanPath = filepath.Join(h.Root, "flow-plan")
	_ = os.RemoveAll(h.PlanPath)
	if err = plan.Write(h.T.Context(), h.PlanPath); err != nil {
		h.T.Fatal(err)
	}
	h.ConfigPath = filepath.Join(h.Root, "flow-config.json")
	h.writeConfig(flow)
	return nil
}

// EnableSMART registers a client key with the lab and selects SMART Backend
// Services for both roles: an observer client for reads and a setup client
// for writes, each with its own scopes and separately provisioned token grant.
func (h *Harness) EnableSMART() {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		h.T.Fatal(err)
	}
	h.Lab.RequireSMART(&key.PublicKey)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPath := filepath.Join(h.Root, "smart-key.pem")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		h.T.Fatal(err)
	}
	// The operator's key provider prints the registered private key; it uses
	// shell builtins only, as the provider runs without a search path.
	h.smartProvider = filepath.Join(h.Root, "smart-key.sh")
	script := "#!/bin/sh\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done < '" + keyPath + "'\n"
	if err := os.WriteFile(h.smartProvider, []byte(script), 0o700); err != nil {
		h.T.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	h.smart = &smartbackend.JWK{Kty: "EC", Kid: "lab-key", Alg: "ES384", Use: "sig", Crv: "P-384", X: enc(key.X.FillBytes(make([]byte, 48))), Y: enc(key.Y.FillBytes(make([]byte, 48)))}
	h.allow("smart-token", h.Lab.Server().Listener.Addr().String(), sendpolicy.SMARTToken)
}
func (h *Harness) smartClient(role, name string, permissions string) string {
	scopes := []string{}
	for _, resource := range []string{"Appointment", "Encounter", "Patient", "Practitioner", "Location", "ServiceRequest", "Observation", "DiagnosticReport"} {
		scopes = append(scopes, "system/"+resource+"."+permissions)
	}
	token := h.Lab.Server().URL + "/token"
	config := smartbackend.Config{Schema: smartbackend.ConfigSchema, FHIRBase: h.Lab.Base(), TokenEndpoint: token, ClientID: "lab-client", Audience: token, Algorithm: "ES384", Role: role, Scopes: scopes, Key: smartbackend.KeyReference{Kid: h.smart.Kid, Generation: "1", Locator: networkaction.Provider{Command: h.smartProvider, Arguments: []string{}}, JWKS: smartbackend.JWKS{Keys: []smartbackend.JWK{*h.smart}}}, Token: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: h.Plan.Identity(), Source: networkaction.Digest([]byte("lab-smart-" + role)), Project: "lab", Environment: "test", Revision: "1", Endpoint: "smart-token", Classification: "nonproduction", Operation: sendpolicy.SMARTToken, Method: "POST", URL: token, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: h.Lab.Server().Certificate().Raw}), TimeoutMS: 60000, MaxBytes: 65536}}
	WriteJSON(h.T, filepath.Join(h.Root, name), config)
	return name
}

// GrantFile is where the runner grant for one named binding is provisioned.
func GrantFile(name string) string { return strings.ReplaceAll(name, ":", "-") + "-grant.json" }

func (h *Harness) writeConfig(flow connectedtest.FlowTest) {
	grant := func(name string) connectedrun.Grant {
		return connectedrun.Grant{Path: GrantFile(name), Actor: "runner", Generation: "1"}
	}
	server := connectedrun.FHIRServerSelection{Authorities: "lab-ca.pem", ServerName: "example.com", Observation: connectedrun.FHIRAuthorization{Mode: "none"}, Action: connectedrun.FHIRAuthorization{Mode: "none"}}
	if h.smart != nil {
		observation, action := grant("token:lab:observation"), grant("token:lab:action")
		server.Observation = connectedrun.FHIRAuthorization{Mode: "smart", Client: h.smartClient("observer", "smart-observer.json", "rs"), Token: &observation}
		server.Action = connectedrun.FHIRAuthorization{Mode: "smart", Client: h.smartClient("setup", "smart-setup.json", "cruds"), Token: &action}
	}
	config := connectedrun.FHIRFlowConfig{Schema: connectedrun.FHIRFlowConfigSchema, Policy: "policy.json", Servers: map[string]connectedrun.FHIRServerSelection{"lab": server}, Phases: map[string]connectedrun.FHIRPhaseSelection{}, Validation: h.Validation, Isolation: connectedrun.IsolationSelection{Registry: "registry.json", Policy: "policy.json", Read: grant("isolation:read"), Setup: grant("isolation:setup"), Cleanup: grant("isolation:cleanup")}, Seed: 7}
	steps := map[string]connectedtest.Step{}
	for _, s := range flow.Steps {
		steps[s.ID] = s
	}
	for _, phase := range flow.Phases {
		selection := connectedrun.FHIRPhaseSelection{Sources: map[string]connectedrun.SourceSelection{}, Barriers: map[string]connectedrun.SourceSelection{}, Grants: map[string]connectedrun.Grant{}}
		servers := false
		for _, id := range phase.Steps {
			if steps[id].V2 != nil {
				send := grant(phase.ID + ":stimulus")
				selection.Case, selection.Target, selection.Send = "case", "target.json", &send
				continue
			}
			selection.Grants["step:"+id] = grant(phase.ID + ":step:" + id)
			servers = true
		}
		if servers {
			selection.Grants["preflight:lab"] = grant(phase.ID + ":preflight:lab")
		}
		for _, ds := range phase.Datasets {
			selection.Grants["dataset:"+ds.ID] = grant(phase.ID + ":dataset:" + ds.ID)
		}
		config.Phases[phase.ID] = selection
	}
	WriteJSON(h.T, h.ConfigPath, config)
}

// Prepare provisions the exact runner grants the prepared flow names.
func (h *Harness) Prepare(instance string) *connectedrun.PreparedFlow {
	h.T.Helper()
	p, err := connectedrun.PrepareFlow(h.PlanPath, h.ConfigPath, instance)
	if err != nil {
		h.T.Fatal(err)
	}
	for name, binding := range p.Bindings() {
		WriteJSON(h.T, filepath.Join(h.Root, GrantFile(name)), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
	return p
}

// Run executes one instance and checks it reopens offline unchanged.
func (h *Harness) Run(instance string) (connectedrun.FlowResult, string) {
	h.T.Helper()
	p := h.Prepare(instance)
	output := filepath.Join(h.Root, instance)
	result, err := connectedrun.ExecuteFlow(h.T.Context(), p, output, testisolation.Confirmation{Plan: p.IsolationIdentity(), Instance: instance})
	if err != nil {
		h.T.Fatal(err)
	}
	reopened, err := connectedrun.OpenFlow(h.T.Context(), output)
	if err != nil {
		h.T.Fatal("retained v5 result does not reopen offline:", err)
	}
	if reopened.Verdict != result.Verdict || reopened.State != result.State {
		h.T.Fatal("offline reopen differs", reopened.Verdict, result.Verdict)
	}
	return result, output
}

func PhaseResult(r connectedrun.FlowResult, id string) connectedrun.FlowPhaseResult {
	for _, p := range r.Phases {
		if p.ID == id {
			return p
		}
	}
	return connectedrun.FlowPhaseResult{}
}
func CheckOutcome(p connectedrun.FlowPhaseResult, id string) assertion.Outcome {
	for _, c := range p.Checks {
		if c.ID == id {
			return c.Outcome
		}
	}
	return ""
}
func WriteJSON(t testing.TB, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
func ReadJSON(t testing.TB, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}
