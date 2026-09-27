package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testisolation"
)

// This fixture implements the public HTTPS fixture protocol with its own state
// machine. It calls no production allocator, transition, postcondition or reader.
// The independent MLLP target above owns its ordinary appointment CSV export.
type flowContractFixture struct {
	beforeSnapshot func(context.Context)
	afterEffect    func(string)
	snapshotReads  atomic.Int32
	afterCreate    func()
	denyCleanup    bool
	mu             sync.Mutex
	target         *target
	mode           string
	mutation       string
	lease          testisolation.Lease
	resource       *testisolation.Resource
	version        int
	requests       int
	events         []string
	violated       bool
}

func (f *flowContractFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/export" && r.Method == http.MethodGet {
		f.snapshotReads.Add(1)
		f.mu.Lock()
		hook := f.beforeSnapshot
		f.mu.Unlock()
		if hook != nil {
			hook(r.Context())
		}
		f.target.mu.Lock()
		raw, err := os.ReadFile(f.target.file)
		f.target.mu.Unlock()
		if err != nil {
			http.Error(w, "snapshot unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Age", "0")
		_, _ = w.Write(raw)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	action := strings.TrimPrefix(r.URL.Path, "/fixture/v1/")
	role := "setup"
	if r.Method == http.MethodGet {
		role = "read"
	} else if action == "delete" || action == "lease-release" {
		role = "cleanup"
	}
	if r.Header.Get("Authorization") != "Bearer contract-"+role {
		http.Error(w, "wrong role", 403)
		return
	}
	if f.denyCleanup && (action == "delete" || action == "lease-release") {
		http.Error(w, "cleanup permission denied", 403)
		return
	}
	var request testisolation.Request
	var scope testisolation.Scope
	if r.Method == http.MethodGet {
		if json.Unmarshal([]byte(r.URL.Query().Get("scope")), &scope) != nil {
			http.Error(w, "bad scope", 400)
			return
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 128<<10))
		if err != nil || json.Unmarshal(raw, &request) != nil || request.Schema != testisolation.ProtocolSchema || request.Action != action {
			http.Error(w, "bad request", 400)
			return
		}
		scope = request.Scope
	}
	if scope.Project != "lab" || scope.Environment != "test" || scope.Revision != "1" || scope.AdapterRevision != "1" || scope.Tenant != "contract-tenant" || scope.Namespace != "contract-data" || scope.Owner == "" || scope.LeaseKey == "" {
		http.Error(w, "wrong deployment", 409)
		return
	}
	respond := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		raw, err := json.Marshal(v)
		if err != nil {
			http.Error(w, "cannot encode", 500)
			return
		}
		_, _ = w.Write(raw)
	}
	switch action {
	case "capabilities":
		respond(testisolation.Capabilities{Schema: testisolation.ProtocolSchema, Scope: scope, Protocol: "typed-fixture-v1", LeaseMode: "exclusive-no-expiry", VersionGuards: true, Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name", "status"}}}})
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
			http.Error(w, "tenant already leased", 409)
			return
		}
		f.version++
		f.lease = testisolation.Lease{Key: scope.LeaseKey, Owner: scope.Owner, Version: strconv.Itoa(f.version)}
	default:
		if request.Lease != f.lease || f.lease.Owner != scope.Owner || f.lease.Key != scope.LeaseKey {
			http.Error(w, "invalid fence", 409)
			return
		}
		switch action {
		case "create":
			if f.resource != nil || request.Resource == nil || request.Resource.Kind != "patient" || request.Resource.Template != "patient" {
				http.Error(w, "invalid patient", 409)
				return
			}
			f.version++
			row := *request.Resource
			row.Owner, row.Version = scope.Owner, strconv.Itoa(f.version)
			f.resource = &row
			// Provisioning the synthetic patient starts its empty appointment
			// inventory. The test caller does not manually reset or stitch data.
			f.target.reset(f.mode)
			if f.afterCreate != nil {
				f.afterCreate()
			}
		case "delete":
			if f.resource == nil || request.Resource == nil || !reflect.DeepEqual(*request.Resource, *f.resource) {
				http.Error(w, "changed owned patient", 409)
				return
			}
			f.resource = nil
			f.target.reset(f.mode)
		case "lease-release":
			if f.resource != nil {
				http.Error(w, "patient remains", 409)
				return
			}
			f.lease = testisolation.Lease{}
		default:
			http.Error(w, "unsupported action", 400)
			return
		}
	}
	f.events = append(f.events, action)
	if f.afterEffect != nil {
		f.afterEffect(action)
	}
	respond(testisolation.Reply{Schema: testisolation.ProtocolSchema, Scope: scope, Lease: f.lease, Resource: f.resource, Outcome: "applied"})
}

func TestFlowContractCredentialProvider(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--flow-contract-credential" {
		return
	}
	fmt.Print("contract-" + os.Args[len(os.Args)-1])
	os.Exit(0)
}

type flowContractHarness struct {
	root, planPath, configPath string
	fixture                    *flowContractFixture
	plan                       *connectedtest.FlowPlan
	server                     *httptest.Server
}

func newFlowContractHarness(t *testing.T, mutation ...string) *flowContractHarness {
	t.Helper()
	return newFlowContractHarnessWithSource(t, false, mutation...)
}

func newFlowContractHarnessForRecovery(t *testing.T, mutation ...string) *flowContractHarness {
	t.Helper()
	return newFlowContractHarnessWithSource(t, true, mutation...)
}
func newFlowContractHarnessWithSource(t *testing.T, coherentHTTP bool, mutation ...string) *flowContractHarness {
	t.Helper()
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	root := t.TempDir()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		retained, err := os.MkdirTemp("", "readmit-flow-failure-")
		if err != nil {
			t.Logf("cannot preserve failed synthetic lifecycle: %v", err)
			return
		}
		if err := os.CopyFS(filepath.Join(retained, "evidence"), os.DirFS(root)); err != nil {
			t.Logf("partial failed lifecycle evidence at %s: %v", retained, err)
			return
		}
		t.Logf("failed synthetic lifecycle preserved at %s", retained)
	})
	target := startTarget(t, root)
	fixture := &flowContractFixture{target: target, mode: "fixed"}
	if len(mutation) > 0 {
		fixture.mutation = mutation[0]
	}
	target.setOutput(func(_ int, raw string) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if fixture.lease.Owner == "" || fixture.resource == nil {
			fixture.violated = true
		}
		event := "booking"
		if strings.Contains(raw, "SIU^S13") {
			event = "reschedule"
		}
		fixture.events = append(fixture.events, event)
		if event == "booking" && fixture.mutation != "" && fixture.resource != nil {
			// The actual stimulus changes target-owned state before its ACK.
			// The client must independently observe and validate this transition.
			fixture.version++
			fixture.resource.Version = strconv.Itoa(fixture.version)
			fixture.resource.Attributes["status"] = "booked"
			switch fixture.mutation {
			case "foreign-owner":
				fixture.resource.Owner = strings.Repeat("f", 64)
			case "undeclared-name":
				fixture.resource.Attributes["name"] = "Unexpected replacement"
			}
		}
	})
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(server.Close)
	intervalPrepared(t, root, target)
	old, err := connectedtest.OpenPlan(filepath.Join(root, "interval-plan"))
	if err != nil {
		t.Fatal(err)
	}
	var policy sendpolicy.ScopedPolicy
	flowContractRead(t, filepath.Join(root, "policy.json"), &policy)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	credential := func(role string, operation sendpolicy.Operation) testisolation.Credential {
		endpoint := "fixture-" + role
		policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: endpoint, Operation: operation, Port: portNumber, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
		return testisolation.Credential{Endpoint: endpoint, Reference: networkaction.Credential{Endpoint: server.Listener.Addr().String(), Purpose: operation, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: executable, Arguments: []string{"-test.run=^TestFlowContractCredentialProvider$", "--", "--flow-contract-credential", role}}}}
	}
	registry := testisolation.Registry{Schema: testisolation.RegistrySchema, Adapters: []testisolation.Registration{{ID: "contract-fixture", Revision: "1", Project: "lab", Environment: "test", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "contract-tenant", Namespace: "contract-data", URL: server.URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), Read: credential("read", sendpolicy.ObservationRead), Setup: credential("setup", sendpolicy.SetupAction), Cleanup: credential("cleanup", sendpolicy.SetupAction), Templates: []testisolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name", "status"}}}, TimeoutMS: 2000}}}
	var snapshotSource observesource.Source
	if coherentHTTP {
		flowContractRead(t, filepath.Join(root, "source.json"), &snapshotSource)
		ca := filepath.Join(root, "snapshot-ca.pem")
		if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
			t.Fatal(err)
		}
		snapshotSource.Observes.Kind = observesource.HTTPAPI
		snapshotSource.File = nil
		snapshotSource.HTTP = &observesource.HTTP{URL: server.URL + "/export", Classification: "nonproduction", CAFile: ca, ServerName: "example.com", Timeout: "2s", MaxBytes: 65536, Retry: observesource.Retry{Attempts: 0, Delay: "5ms"}}
		if err := observesource.WriteSource(filepath.Join(root, "source.json"), snapshotSource); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"before", "after"} {
			policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: id, Operation: sendpolicy.ObservationRead, Port: portNumber, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
		}
	}
	write(t, filepath.Join(root, "registry.json"), registry)
	write(t, filepath.Join(root, "policy.json"), policy)
	policyRaw, err := os.ReadFile(filepath.Join(root, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	base := old.Document().Test
	if coherentHTTP {
		for i := range base.Datasets {
			base.Datasets[i].Source = snapshotSource.Identity()
		}
	}
	base.Environment.AddressPolicyIdentity = dataset.Digest(policyRaw)
	files := map[string][]byte{}
	oldFiles := old.Files()
	for _, step := range base.Steps {
		files[step.V2.Input.File] = oldFiles["dependencies/"+step.V2.Input.SHA256]
	}
	ref := func(id, schema, name string, value any) connectedtest.Reference {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = raw
		return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: name, SHA256: dataset.Digest(raw)}
	}
	contract := testisolation.Contract{Schema: testisolation.ContractSchema, Project: "lab", Environment: "test", Revision: "1", Adapter: "contract-fixture", Tenant: "contract-tenant", Namespace: "contract-data", Mode: "isolated-tenant", Concurrency: "exclusive-target-lease", Resources: []testisolation.Requirement{{ID: "patient", Kind: "patient", Template: "patient", Ownership: "create", DependsOn: []string{}, Attributes: map[string]string{"name": "Synthetic contract patient"}, Identifiers: []testisolation.Identifier{{Scope: "patient", Namespace: "patient-business", Value: "SAME"}}}}, Manual: []testisolation.ManualStep{}}
	if fixture.mutation != "" {
		contract.Resources[0].Attributes["status"] = "awaiting"
	}
	flow := connectedtest.FlowTest{Schema: connectedtest.FlowTestSchema, Project: "lab", ID: "whole-lifecycle", Revision: "1", Environment: base.Environment, Isolation: ref("isolation", testisolation.ContractSchema, "isolation.json", contract), Boundary: "application-state", Steps: base.Steps, Limits: base.Limits}
	flow.Limits.DeadlineMS = 30000
	var config connectedrun.ConfigV2
	flowContractRead(t, filepath.Join(root, "interval-execution.json"), &config)
	phaseConfigs := map[string]connectedrun.ConfigV2{}
	for index, name := range []string{"booking", "reschedule"} {
		definitions := slices.Clone(base.Datasets)
		for i := range definitions {
			ds := &definitions[i]
			files[ds.Projection.File] = oldFiles["dependencies/"+ds.Projection.SHA256]
			if coherentHTTP {
				projection, err := dataset.DecodeProjection(files[ds.Projection.File])
				if err != nil {
					t.Fatal(err)
				}
				// HTTP authorization and TLS are part of acquisition. The file
				// helper's 300ms budget expires before those complete under race.
				projection.Limits.TimeoutMS = 2000
				pinned := ref(ds.Projection.ID, dataset.ProjectionSchema, ds.Projection.File, projection)
				ds.Projection = &pinned
			}
			var interval observeinterval.Definition
			if err := json.Unmarshal(oldFiles["dependencies/"+ds.Completion.Policy.SHA256], &interval); err != nil {
				t.Fatal(err)
			}
			interval.Source = ds.Source
			interval.HorizonMS, interval.SampleMS, interval.MaxGapMS = 120, 20, 1000
			if coherentHTTP {
				// These positive lifecycle fixtures qualify state/authority,
				// not scheduling latency. A healthy admitted two-second HTTP read must
				// fit coverage within the unchanged finite parent deadline.
				// Timing-negative tests author their tighter gap explicitly.
				interval.MaxGapMS = flow.Limits.DeadlineMS
			}
			interval.MaxSamples = 100
			intervalRef := ref(name+"-"+ds.ID+"-window", observeinterval.Schema, name+"-"+ds.ID+"-window.json", interval)
			ds.Completion.HorizonMS, ds.Completion.Policy = interval.HorizonMS, &intervalRef
		}
		beforeCount, afterCount := index, 1
		status, start := "booked", "2026-01-01T12:00"
		if index == 1 {
			status, start = "moved", "2026-01-02T12:00"
		}
		checks := assertion.DatasetSetDocument{Schema: assertion.DatasetSchema, Bindings: []assertion.DatasetBinding{}, Assertions: []assertion.DatasetAssertion{
			{ID: "before-count", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "before"}, Count: &beforeCount},
			{ID: "after-count", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: &afterCount},
			{ID: "one-key", Operator: "unique-keys", Subject: assertion.RowSelection{Dataset: "after"}},
			{ID: "status", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Expected: &dataset.Value{State: "present", Type: "text", Text: status}},
			{ID: "start", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "after"}, Column: "start", Expected: &dataset.Value{State: "present", Type: "datetime", Text: start, Precision: "minute", Timezone: "absent"}},
		}}
		for _, ds := range definitions {
			checks.Bindings = append(checks.Bindings, assertion.DatasetBinding{Name: ds.ID, Namespace: ds.Namespace, Phase: ds.Phase, Source: ds.Source, ProjectionIdentity: ds.Projection.SHA256})
		}
		phase := connectedtest.FlowPhase{ID: name, Steps: []string{base.Steps[index].ID}, Datasets: definitions, Checks: ref(name+"-checks", assertion.DatasetSchema, name+"-checks.json", checks)}
		if index == 1 {
			phase.After = []connectedtest.PhaseDependency{{Phase: "booking", Requires: "pass"}}
		} else if fixture.mutation != "" {
			phase.IsolationChanges = []connectedtest.IsolationChange{{Alias: "patient", Attributes: map[string]string{"name": "Synthetic contract patient", "status": "booked"}}}
		}
		flow.Phases = append(flow.Phases, phase)
		childConfig := config
		childConfig.Definition.Send.Path = name + "-send-grant.json"
		if coherentHTTP {
			childConfig.Definition.Sources = maps.Clone(config.Definition.Sources)
			for id, selection := range childConfig.Definition.Sources {
				selection.Grant = &connectedrun.Grant{Path: name + "-" + id + "-source-grant.json", Actor: "runner", Generation: "1"}
				selection.CredentialGeneration = "1"
				childConfig.Definition.Sources[id] = selection
			}
		}
		phaseConfigs[name] = childConfig
	}
	raw, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := connectedtest.CompileFlow(raw, files, old.Document().Generation)
	if err != nil {
		t.Fatal(err)
	}
	h := &flowContractHarness{root: root, planPath: filepath.Join(root, "flow-plan"), configPath: filepath.Join(root, "flow-config.json"), fixture: fixture, plan: plan, server: server}
	if err := plan.Write(t.Context(), h.planPath); err != nil {
		t.Fatal(err)
	}
	grant := func(role string) connectedrun.Grant {
		return connectedrun.Grant{Path: role + "-isolation-grant.json", Actor: "runner", Generation: "1"}
	}
	selection := connectedrun.IsolationSelection{Registry: "registry.json", Policy: "policy.json", Read: grant("read"), Setup: grant("setup"), Cleanup: grant("cleanup")}
	if fixture.mutation != "" {
		read, cleanup := grant("transition-read"), grant("transition-cleanup")
		selection.TransitionRead, selection.TransitionCleanup = &read, &cleanup
	}
	write(t, h.configPath, connectedrun.FlowConfig{Schema: connectedrun.FlowConfigSchema, Phases: phaseConfigs, Isolation: selection, Seed: 7})
	return h
}

func flowContractRead(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

func (h *flowContractHarness) prepare(t *testing.T, instance string) *connectedrun.PreparedFlow {
	t.Helper()
	p, err := connectedrun.PrepareFlow(h.planPath, h.configPath, instance)
	if err != nil {
		t.Fatal(err)
	}
	for name, binding := range p.Bindings() {
		parts := strings.SplitN(name, ":", 2)
		path := parts[0] + "-send-grant.json"
		if parts[0] == "isolation" {
			path = parts[1] + "-isolation-grant.json"
		} else if strings.HasPrefix(parts[1], "dataset:") {
			path = parts[0] + "-" + strings.TrimPrefix(parts[1], "dataset:") + "-source-grant.json"
		}
		write(t, filepath.Join(h.root, path), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
	return p
}

func TestFlowLifecycleContractPreparationHasNoTargetEffects(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	_ = h.prepare(t, "first-run")
	h.fixture.mu.Lock()
	defer h.fixture.mu.Unlock()
	if h.fixture.requests != 0 || h.fixture.snapshotReads.Load() != 0 || h.fixture.target.received.Load() != 0 {
		t.Fatalf("preparation performed network effects: reads=%d sends=%d", h.fixture.requests, h.fixture.target.received.Load())
	}
}

func TestFlowLifecycleContractUnchangedOracleDetectsDefectFixAndReintroduction(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	originalPlan := h.plan.Identity()
	originalFiles := h.plan.Files()
	var retained []string
	for index, mode := range []string{"defective", "fixed", "defective"} {
		instance := fmt.Sprintf("contract-run-%d", index)
		p := h.prepare(t, instance)
		h.fixture.mu.Lock()
		h.fixture.mode = mode // Only the independently implemented target changes.
		h.fixture.events = nil
		h.fixture.mu.Unlock()
		output := filepath.Join(h.root, instance)
		result, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{})
		if err != nil {
			t.Fatal(mode, err)
		}
		want := assertion.VerdictFail
		if mode == "fixed" {
			want = assertion.VerdictPass
		}
		if result.Plan != originalPlan || result.State != "complete" || result.Verdict != want || result.Setup != "ready" || result.Cleanup != "complete" || result.Boundary != "application-state" {
			t.Fatalf("%s: full lifecycle = %+v", mode, result)
		}
		if len(result.Phases) != 2 || result.Phases[0].ID != "booking" || result.Phases[0].Verdict != assertion.VerdictPass || result.Phases[1].ID != "reschedule" || result.Phases[1].Verdict != want {
			t.Fatalf("%s: phase checks lost intermediate state: %+v", mode, result.Phases)
		}
		for _, phase := range result.Phases {
			if phase.State != "complete" || len(phase.Checks) != 5 || len(phase.Steps) != 1 {
				t.Fatalf("incomplete phase denominator: %+v", phase)
			}
			run, err := replay.Open(filepath.Join(output, "phases", phase.ID, "transport", "run"))
			if err != nil {
				t.Fatal(err)
			}
			attempted := 0
			for _, event := range run.Events {
				if event.Outcome == replay.NotAttempted {
					continue
				}
				attempted++
				if event.Delivery != "acknowledged" || event.ACK.Code != "AA" || event.ACK.Correlation != "matched" {
					t.Fatalf("target did not ACK the same valid input in %s mode: %+v", mode, event)
				}
			}
			if attempted != 1 {
				t.Fatalf("phase sent %d inputs, want one", attempted)
			}
		}
		h.fixture.mu.Lock()
		if h.fixture.violated || h.fixture.resource != nil || h.fixture.lease.Owner != "" || !slices.Equal(h.fixture.events, []string{"lease-acquire", "create", "booking", "reschedule", "delete", "lease-release"}) {
			t.Errorf("unsafe lifecycle: violated=%v resource=%+v lease=%+v events=%v", h.fixture.violated, h.fixture.resource, h.fixture.lease, h.fixture.events)
		}
		h.fixture.mu.Unlock()
		if got := h.fixture.target.received.Load(); got != int32((index+1)*2) {
			t.Fatalf("unexpected actual target input count: %d", got)
		}
		reopenedPlan, err := connectedtest.OpenFlowPlan(h.planPath)
		if err != nil || reopenedPlan.Identity() != originalPlan || !reflect.DeepEqual(reopenedPlan.Files(), originalFiles) {
			t.Fatalf("approved oracle was rewritten: %v", err)
		}
		reopened, err := connectedrun.OpenFlow(t.Context(), output)
		if err != nil || !reflect.DeepEqual(reopened, result) {
			t.Fatalf("retained lifecycle differs: %v", err)
		}
		retained = append(retained, output)
	}
	// Historical reads must need neither the live target nor the selected
	// source/configuration/plan directories and must never send again.
	h.server.Close()
	for _, name := range []string{"target.json", "registry.json", "policy.json", "source.json", "flow-config.json", "export.csv"} {
		if err := os.Remove(filepath.Join(h.root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(h.planPath, h.planPath+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	for index, output := range retained {
		want := assertion.VerdictFail
		if index == 1 {
			want = assertion.VerdictPass
		}
		result, err := connectedrun.OpenFlow(context.Background(), output)
		if err != nil || result.Verdict != want || result.Cleanup != "complete" {
			t.Fatalf("offline historical result changed: %+v %v", result, err)
		}
	}
	if h.fixture.target.received.Load() != 6 {
		t.Fatal("offline reopening resent inputs")
	}
}

func TestFlowLifecycleContractOnlyApprovedOwnedStateCanAdvance(t *testing.T) {
	for _, mutation := range []string{"approved-status", "foreign-owner", "undeclared-name"} {
		t.Run(mutation, func(t *testing.T) {
			h := newFlowContractHarnessWithSource(t, true, mutation)
			p := h.prepare(t, "state-transition")
			output := filepath.Join(h.root, "transition-result")
			result, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{})
			if err != nil {
				t.Fatal("transition must retain its outcome", err)
			}
			if len(result.Phases) != 2 || len(result.Phases[0].Checks) != 5 {
				t.Fatalf("booking evidence was not evaluated: %+v", result)
			}
			for _, check := range result.Phases[0].Checks {
				if check.Outcome != assertion.OutcomePassed {
					for _, phase := range []string{"booking", "reschedule"} {
						if child, readErr := connectedrun.Open(t.Context(), filepath.Join(output, "phases", phase)); readErr == nil {
							t.Logf("phase %s retained summary: %+v", phase, child)
						}
						if interval, readErr := observeinterval.Open(t.Context(), filepath.Join(output, "phases", phase, "intervals", "after")); readErr == nil {
							t.Logf("phase %s retained interval: %+v", phase, interval)
						}
					}
					t.Fatalf("booking check did not pass independently of isolation: %+v; lifecycle=%+v", check, result)
				}
			}
			h.fixture.mu.Lock()
			defer h.fixture.mu.Unlock()
			if mutation == "approved-status" {
				if result.State != "complete" || result.Verdict != assertion.VerdictPass || result.Cleanup != "complete" || result.Phases[1].State != "complete" {
					t.Fatalf("approved target transition prevented subsequent work: %+v", result)
				}
				if h.fixture.target.received.Load() != 2 || h.fixture.resource != nil || h.fixture.lease.Owner != "" || !slices.Equal(h.fixture.events, []string{"lease-acquire", "create", "booking", "reschedule", "delete", "lease-release"}) {
					t.Fatalf("approved cleanup did not use observed current version: resource=%+v lease=%+v events=%v", h.fixture.resource, h.fixture.lease, h.fixture.events)
				}
			} else {
				if result.State == "complete" || result.Verdict == assertion.VerdictPass || result.Cleanup == "complete" || result.Phases[1].State == "complete" {
					t.Fatalf("unapproved target transition accepted: %+v", result)
				}
				if h.fixture.target.received.Load() != 1 || h.fixture.resource == nil || h.fixture.lease.Owner == "" || !slices.Equal(h.fixture.events, []string{"lease-acquire", "create", "booking"}) {
					t.Fatalf("unapproved transition caused a send or destructive cleanup: resource=%+v lease=%+v events=%v", h.fixture.resource, h.fixture.lease, h.fixture.events)
				}
			}
			reopened, err := connectedrun.OpenFlow(t.Context(), output)
			if err != nil || !reflect.DeepEqual(reopened, result) {
				t.Fatalf("offline transition outcome changed: %+v %v", reopened, err)
			}
		})
	}
}
