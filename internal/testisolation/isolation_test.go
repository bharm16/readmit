package testisolation_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
	isolation "github.com/bharm16/readmit/internal/testisolation"
)

// Independent target: ordinary HTTPS JSON records and a mutex-protected store.
// It has no Readmit production transition, allocator, evaluator or lease helper.
type fixtureTarget struct {
	holdRelease                                    bool
	failRelease                                    bool
	revision                                       string
	mu                                             sync.Mutex
	resources                                      map[string]map[string]any
	leases                                         map[string]map[string]any
	version                                        int
	effects                                        []string
	failCreate, failDelete, wrongPost, unsupported bool
	requests                                       int
}

func newTarget() *fixtureTarget {
	return &fixtureTarget{revision: "1", resources: map[string]map[string]any{}, leases: map[string]map[string]any{}}
}
func (f *fixtureTarget) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	action := strings.TrimPrefix(r.URL.Path, "/fixture/v1/")
	expected := "setup-token"
	if r.Method == "GET" {
		expected = "observer-token"
	} else if action == "delete" || action == "lease-release" {
		expected = "cleanup-token"
	}
	if r.Header.Get("Authorization") != "Bearer "+expected {
		http.Error(w, "denied", http.StatusForbidden)
		return
	}
	var request map[string]any
	var scope map[string]any
	if r.Method == "GET" {
		_ = json.Unmarshal([]byte(r.URL.Query().Get("scope")), &scope)
	} else {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if json.Unmarshal(raw, &request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		scope = request["scope"].(map[string]any)
	}
	if scope["tenant"] != "tenant" || scope["project"] != "lab" || scope["environment"] != "test" {
		http.Error(w, "wrong scope", 409)
		return
	}
	if r.Method != "GET" && scope["revision"] != f.revision {
		http.Error(w, "changed deployment", 409)
		return
	}
	scope["revision"] = f.revision
	base := map[string]any{"schema": "readmit-fixture-adapter/v1", "scope": scope}
	leaseKey := scope["lease_key"].(string)
	namespace := scope["namespace"].(string)
	owner := scope["owner"].(string)
	lease := f.leases[leaseKey]
	if lease == nil {
		lease = map[string]any{"key": "", "owner": "", "version": ""}
	}
	write := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := json.Marshal(v)
		_, _ = w.Write(raw)
	}
	switch action {
	case "capabilities":
		base["protocol"] = "typed-fixture-v1"
		base["lease_mode"] = "exclusive-no-expiry"
		base["version_guards"] = true
		templates := []map[string]any{{"id": "patient", "kind": "patient", "attributes": []string{"name"}}, {"id": "appointment", "kind": "appointment", "attributes": []string{"status"}}}
		if f.unsupported {
			templates = templates[:1]
		}
		base["templates"] = templates
		write(base)
		return
	case "state":
		rows := []map[string]any{}
		for key, row := range f.resources {
			if strings.HasPrefix(key, namespace+"/") {
				rows = append(rows, row)
			}
		}
		base["resources"] = rows
		base["lease"] = lease
		write(base)
		return
	case "lease-acquire":
		if lease["owner"] != "" {
			http.Error(w, "busy", 409)
			return
		}
		f.version++
		lease = map[string]any{"key": leaseKey, "owner": owner, "version": strconv.Itoa(f.version)}
		f.leases[leaseKey] = lease
	default:
		asked := request["lease"].(map[string]any)
		if asked["owner"] != owner || !bytes.Equal(encode(asked), encode(lease)) {
			http.Error(w, "stale lease", 409)
			return
		}
		if action == "lease-release" {
			if !f.holdRelease {
				delete(f.leases, leaseKey)
			}
			lease = map[string]any{"key": "", "owner": "", "version": ""}
			if f.failRelease {
				connection, _, _ := w.(http.Hijacker).Hijack()
				_ = connection.Close()
				return
			}
			break
		}
		resource := request["resource"].(map[string]any)
		key := namespace + "/" + resource["kind"].(string) + "/" + resource["id"].(string)
		old := f.resources[key]
		switch action {
		case "create":
			for _, value := range resource["references"].([]any) {
				ref := value.(map[string]any)
				if f.resources[namespace+"/"+ref["kind"].(string)+"/"+ref["id"].(string)] == nil {
					http.Error(w, "missing prerequisite", 409)
					return
				}
			}
			if old != nil {
				http.Error(w, "collision", 409)
				return
			}
			f.version++
			resource["version"] = strconv.Itoa(f.version)
			resource["owner"] = owner
			if f.wrongPost {
				resource["attributes"] = map[string]any{"status": "wrong"}
			}
			f.resources[key] = resource
		case "claim":
			if old == nil || old["version"] != resource["version"] || old["owner"] != "" {
				http.Error(w, "stale claim", 409)
				return
			}
			f.version++
			resource["version"] = strconv.Itoa(f.version)
			resource["owner"] = owner
			f.resources[key] = resource
		case "delete":
			if f.failDelete {
				http.Error(w, "cleanup unavailable", 503)
				return
			}
			if old == nil || old["owner"] != owner || old["version"] != resource["version"] {
				http.Error(w, "stale resource", 409)
				return
			}
			delete(f.resources, key)
		default:
			http.Error(w, "unknown", 400)
			return
		}
		f.effects = append(f.effects, action+":"+resource["kind"].(string))
		if action == "create" && f.failCreate {
			connection, _, _ := w.(http.Hijacker).Hijack()
			_ = connection.Close()
			return
		}
		base["resource"] = resource
	}
	base["outcome"] = "applied"
	base["lease"] = lease
	write(base)
}
func encode(v any) []byte {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		panic(err)
	}
	return b
}
func TestFixtureCredentialProvider(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--fixture-credential" {
		return
	}
	fmt.Print(os.Args[len(os.Args)-1] + "-token")
	os.Exit(0)
}

type approval struct {
	binding networkaction.Binding
	actor   networkaction.Actor
	revoked *bool
}

func (a approval) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if ctx.Err() != nil || *a.revoked || b != a.binding {
		return networkaction.Actor{}, context.Canceled
	}
	return a.actor, nil
}

type harness struct {
	p                      *isolation.Prepared
	a                      isolation.Authorities
	root, registry, policy string
	target                 *fixtureTarget
	contract               isolation.Contract
	options                isolation.Options
	revoked                *bool
}

func fixture(t *testing.T, mode string) *harness {
	t.Helper()
	// Only re-executed credential helpers read this race setting.
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	f := newTarget()
	server := httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	root := t.TempDir()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	n, _ := strconv.Atoi(port)
	ip := netip.MustParseAddr(host)
	bits := 128
	if ip.Is4() {
		bits = 32
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	role := func(name, endpoint string, op sendpolicy.Operation) isolation.Credential {
		return isolation.Credential{Endpoint: endpoint, Reference: networkaction.Credential{Endpoint: server.Listener.Addr().String(), Purpose: op, Generation: "1", Header: "Authorization", Prefix: "Bearer ", Locator: networkaction.Provider{Command: executable, Arguments: []string{"-test.run=^TestFixtureCredentialProvider$", "--", "--fixture-credential", name}}}}
	}
	registry := isolation.Registry{Schema: isolation.RegistrySchema, Adapters: []isolation.Registration{{ID: "fixture", Revision: "1", Project: "lab", Environment: "test", EnvironmentRevision: "1", Classification: "nonproduction", Tenant: "tenant", Namespace: "synthetic", URL: server.URL, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), Read: role("observer", "observe", sendpolicy.ObservationRead), Setup: role("setup", "setup", sendpolicy.SetupAction), Cleanup: role("cleanup", "cleanup", sendpolicy.SetupAction), Templates: []isolation.Template{{ID: "patient", Kind: "patient", Attributes: []string{"name"}}, {ID: "appointment", Kind: "appointment", Attributes: []string{"status"}}}, TimeoutMS: 2000}}}
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1"}
	for _, v := range []struct {
		id string
		op sendpolicy.Operation
	}{{"observe", sendpolicy.ObservationRead}, {"setup", sendpolicy.SetupAction}, {"cleanup", sendpolicy.SetupAction}} {
		policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: v.id, Operation: v.op, Port: n, Destinations: []string{netip.PrefixFrom(ip, bits).String()}, Selection: "single-address"})
	}
	contract := isolation.Contract{Schema: isolation.ContractSchema, Project: "lab", Environment: "test", Revision: "1", Adapter: "fixture", Tenant: "tenant", Namespace: "synthetic", Mode: mode, Concurrency: "exclusive-target-lease", Resources: []isolation.Requirement{{ID: "patient", Kind: "patient", Template: "patient", Ownership: "create", Attributes: map[string]string{"name": "fictional"}, DependsOn: []string{}, Identifiers: []isolation.Identifier{{Scope: "patient", Namespace: "patient-business", Value: "authored-duplicate"}}}, {ID: "booking", Kind: "appointment", Template: "appointment", Ownership: "create", Attributes: map[string]string{"status": "booked"}, DependsOn: []string{"patient"}, Identifiers: []isolation.Identifier{{Scope: "appointment", Namespace: "appointment-business", Value: "authored-duplicate"}}}}, Manual: []isolation.ManualStep{}}
	h := &harness{root: root, registry: filepath.Join(root, "registry.json"), policy: filepath.Join(root, "policy.json"), target: f, contract: contract, options: isolation.Options{ParentPlan: networkaction.Digest([]byte("plan")), Instance: "run-one", Seed: 7}, revoked: new(bool)}
	for path, value := range map[string]any{h.registry: registry, h.policy: policy} {
		if err := os.WriteFile(path, encode(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h.prepare(t)
	return h
}
func (h *harness) prepare(t *testing.T) {
	t.Helper()
	var err error
	h.p, err = isolation.Prepare(encode(h.contract), h.registry, h.policy, h.options)
	if err != nil {
		t.Fatal(err)
	}
	grant := func(phase string) networkaction.Authority {
		return approval{binding: h.p.Review(phase).Binding, actor: networkaction.Actor{Kind: "runner", ID: "runner", Generation: "1", EvidenceIdentity: networkaction.Digest([]byte(phase)), Expires: time.Now().Add(time.Hour)}, revoked: h.revoked}
	}
	h.a = isolation.Authorities{Read: grant("read"), Setup: grant("setup"), Cleanup: grant("cleanup")}
}
func (h *harness) preflight(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(h.root, name)
	if _, err := h.p.Preflight(t.Context(), h.a, path); err != nil {
		t.Fatal(err)
	}
	if _, err := isolation.Open(path); err != nil {
		t.Fatalf("preflight readback: %v", err)
	}
	return path
}
func (h *harness) start(t *testing.T, name string) (*isolation.Session, string, error) {
	t.Helper()
	preflight := h.preflight(t, name+"-preflight")
	path := filepath.Join(h.root, name)
	s, err := isolation.Start(t.Context(), h.p, h.a, preflight, path, isolation.Confirmation{})
	return s, path, err
}
func TestIsolationActuallyProvisionsVerifiesAndCleansExactOwnedResources(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	if h.target.requests != 0 {
		t.Fatal("preparation connected")
	}
	p2, err := isolation.Prepare(encode(h.contract), h.registry, h.policy, h.options)
	if err != nil || p2.Identity() != h.p.Identity() {
		t.Fatal("allocation was not deterministic")
	}
	allocations := h.p.Allocations()
	if allocations[0].ID == allocations[1].ID || allocations[0].Identifiers[0].Value != allocations[1].Identifiers[0].Value {
		t.Fatal("logical IDs collapsed or authored duplicate changed")
	}
	s, path, err := h.start(t, "run")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.Ready() || len(h.target.resources) != 2 {
		t.Fatal("setup did not produce actual ready state")
	}
	if err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.target.resources) != 0 || len(h.target.leases) != 0 {
		t.Fatal("cleanup left owned state")
	}
	got, err := isolation.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Setup != "ready" || got.Cleanup != "complete" {
		t.Fatal(got)
	}
	if strings.Join(h.target.effects, ",") != "create:patient,create:appointment,delete:appointment,delete:patient" {
		t.Fatal("unsafe dependency order", h.target.effects)
	}
}
func TestIsolationCrashReconcilesActualEffectsWithoutRetry(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	h.target.failCreate = true
	s, path, err := h.start(t, "crash")
	if err == nil {
		t.Fatal("lost response passed")
	}
	s.Close()
	effects := len(h.target.effects)
	inspection, err := isolation.Inspect(path)
	if err != nil || inspection.Setup != "uncertain" {
		t.Fatalf("%+v %v", inspection, err)
	}
	receipt, err := isolation.Reconcile(t.Context(), h.p, h.a, path, filepath.Join(h.root, "reconciled"))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Setup != "reconciled-not-ready" || len(receipt.Resources) != 1 || len(h.target.effects) != effects {
		t.Fatal("reconciliation retried or invented setup")
	}
}

func TestIsolationResidualStateAndParallelTenantRunCannotPass(t *testing.T) {
	h := fixture(t, "isolated-tenant")
	h.target.resources["synthetic/appointment/stale"] = map[string]any{"alias": "stale", "kind": "appointment", "id": "stale", "version": "1", "owner": "", "template": "appointment", "attributes": map[string]string{"status": "booked"}, "identifiers": []any{}, "references": []any{}}
	s, _, err := h.start(t, "residual")
	if err == nil || s.Ready() {
		t.Fatal("residual appointment satisfied setup")
	}
	if err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(h.target.resources) != 1 {
		t.Fatal("residual unowned resource was deleted")
	}
	h.target.mu.Lock()
	delete(h.target.resources, "synthetic/appointment/stale")
	h.target.mu.Unlock()
	first, _, err := h.start(t, "first")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	next := *h
	next.options.Instance = "run-two"
	next.prepare(t)
	second, _, err := next.start(t, "second")
	if second != nil {
		defer second.Close()
	}
	if err == nil || second != nil && second.Ready() {
		t.Fatal("parallel tenant run acquired shared state")
	}
	if err := first.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestIsolationRequiresFreshManualConfirmationAndScopedCredentials(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	h.contract.Manual = []isolation.ManualStep{{ID: "notifications", Instructions: "Confirm external notifications are disabled for this execution"}}
	h.prepare(t)
	preflight := h.preflight(t, "preflight")
	for _, confirmation := range []isolation.Confirmation{{}, {Plan: h.p.Identity(), Instance: "old-run", Steps: []string{"notifications"}}} {
		if s, err := isolation.Start(t.Context(), h.p, h.a, preflight, filepath.Join(t.TempDir(), "out"), confirmation); err == nil || s != nil {
			t.Fatal("missing current manual confirmation accepted")
		}
	}
	wrong := h.a
	wrong.Setup = wrong.Read
	if s, err := isolation.Start(t.Context(), h.p, wrong, preflight, filepath.Join(t.TempDir(), "wrong"), isolation.Confirmation{Plan: h.p.Identity(), Instance: h.options.Instance, Steps: []string{"notifications"}}); err == nil || s != nil {
		t.Fatal("observer acquired setup authority")
	}
	s, err := isolation.Start(t.Context(), h.p, h.a, preflight, filepath.Join(h.root, "confirmed"), isolation.Confirmation{Plan: h.p.Identity(), Instance: h.options.Instance, Steps: []string{"notifications"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Result().Manual) != 1 || s.Result().Manual[0].Provenance != "operator-declared-current-execution" {
		t.Fatal("manual claim became verified reset")
	}
	if err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestIsolationPoisonedImportsUnsupportedCapabilityAndChangedRegistryRefuse(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	poisoned := bytes.Replace(encode(h.contract), []byte(`"schema":`), []byte(`"command":"delete-everything","schema":`), 1)
	if _, err := isolation.Prepare(poisoned, h.registry, h.policy, h.options); err == nil {
		t.Fatal("imported executable hook accepted")
	}
	h.target.unsupported = true
	if _, err := h.p.Preflight(t.Context(), h.a, filepath.Join(h.root, "unsupported")); err == nil {
		t.Fatal("unsupported prerequisite passed")
	}
	h.target.unsupported = false
	preflight := h.preflight(t, "supported")
	var registry isolation.Registry
	raw, _ := os.ReadFile(h.registry)
	_ = json.Unmarshal(raw, &registry)
	registry.Adapters[0].Read.Reference = registry.Adapters[0].Setup.Reference
	if err := os.WriteFile(h.registry, encode(registry), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := isolation.Start(t.Context(), h.p, h.a, preflight, filepath.Join(h.root, "changed"), isolation.Confirmation{}); err == nil {
		t.Fatal("changed credential registration retained old authority")
	}
	if _, err := isolation.Prepare(encode(h.contract), h.registry, h.policy, h.options); err == nil {
		t.Fatal("write credential registered for observer")
	}
}
func TestIsolationChangedResourceOrFailedCleanupNeverOverwritesOutcome(t *testing.T) {
	for _, failure := range []string{"version", "server", "grant", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			h := fixture(t, "reserved-namespace")
			s, path, err := h.start(t, "run")
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			switch failure {
			case "version":
				h.target.mu.Lock()
				for _, r := range h.target.resources {
					r["version"] = "changed"
				}
				h.target.mu.Unlock()
			case "server":
				h.target.mu.Lock()
				h.target.failDelete = true
				h.target.mu.Unlock()
			case "grant":
				*h.revoked = true
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := s.Cleanup(ctx); err == nil {
				t.Fatal("unsafe cleanup passed")
			}
			if s.Ready() || s.Result().Cleanup == "complete" {
				t.Fatal("cleanup failure lost")
			}
			s.Close()
			inspected, err := isolation.Inspect(path)
			if err != nil {
				t.Fatal(err)
			}
			if inspected.Complete {
				t.Fatal("partial cleanup retained success")
			}
			h.target.mu.Lock()
			defer h.target.mu.Unlock()
			if len(h.target.resources) != 2 {
				t.Fatal("failed cleanup deleted resources")
			}
		})
	}
}
func TestIsolationExplicitRecoveryCleanupUsesNewObservedVersionsAndAuthority(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	h.target.failCreate = true
	s, path, err := h.start(t, "crash")
	if err == nil {
		t.Fatal("uncertain setup passed")
	}
	s.Close()
	reconciled := filepath.Join(h.root, "reconciled")
	if _, err := isolation.Reconcile(t.Context(), h.p, h.a, path, reconciled); err != nil {
		t.Fatal(err)
	}
	if _, err := isolation.CleanupReconciled(t.Context(), h.p, h.a, reconciled, filepath.Join(h.root, "stale-consent")); err == nil {
		t.Fatal("old review authorized recovered cleanup")
	}
	grant := func(phase string) networkaction.Authority {
		review, err := isolation.RecoveryReview(h.p, reconciled, phase)
		if err != nil {
			t.Fatal(err)
		}
		return approval{binding: review.Binding, actor: networkaction.Actor{Kind: "runner", ID: "runner", Generation: "new", EvidenceIdentity: networkaction.Digest([]byte("fresh-" + phase)), Expires: time.Now().Add(time.Hour)}, revoked: h.revoked}
	}
	authority := isolation.Authorities{Read: grant("read"), Cleanup: grant("cleanup")}
	output := filepath.Join(h.root, "cleanup")
	result, err := isolation.CleanupReconciled(t.Context(), h.p, authority, reconciled, output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Setup != "recovered-no-setup" || result.Cleanup != "complete" || len(result.Manual) != 0 {
		t.Fatal("recovery restored setup consent")
	}
	if _, err := isolation.Open(output); err != nil {
		t.Fatalf("recovery readback: %v", err)
	}
	h.target.mu.Lock()
	defer h.target.mu.Unlock()
	if len(h.target.resources) != 0 || len(h.target.leases) != 0 || strings.Join(h.target.effects, ",") != "create:patient,delete:patient" {
		t.Fatal("recovery retried provisioning or failed cleanup", h.target.effects)
	}
}

func TestIsolationRecordedBaselineSelectionAndClaimAreExact(t *testing.T) {
	for _, ownership := range []string{"select", "claim"} {
		t.Run(ownership, func(t *testing.T) {
			h := fixture(t, "isolated-tenant")
			h.contract.Mode = "recorded-baseline"
			h.contract.Resources = h.contract.Resources[:1]
			h.contract.Resources[0].Ownership = ownership
			h.contract.Resources[0].LogicalID = "baseline-patient"
			h.contract.Resources[0].Version = "old"
			resource := isolation.Resource{Alias: "patient", Kind: "patient", ID: "baseline-patient", Version: "old", Template: "patient", Attributes: map[string]string{"name": "fictional"}, Identifiers: h.contract.Resources[0].Identifiers, References: []isolation.Reference{}}
			h.contract.Baseline = isolation.SnapshotIdentity(isolation.Snapshot{Scope: h.p.Scope(), Resources: []isolation.Resource{resource}})
			h.prepare(t)
			var row map[string]any
			_ = json.Unmarshal(encode(resource), &row)
			h.target.resources["synthetic/patient/baseline-patient"] = row
			s, _, err := h.start(t, "baseline")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if !s.Ready() {
				t.Fatal("verified baseline not ready")
			}
			if err := s.Cleanup(t.Context()); err != nil {
				t.Fatal(err)
			}
			h.target.mu.Lock()
			defer h.target.mu.Unlock()
			want := 0
			if ownership == "select" {
				want = 1
			}
			if len(h.target.resources) != want {
				t.Fatal("cleanup changed unclaimed reference or left claimed resource")
			}
		})
	}
	h := fixture(t, "isolated-tenant")
	h.contract.Mode = "recorded-baseline"
	h.contract.Baseline = networkaction.Digest([]byte("different-baseline"))
	h.prepare(t)
	s, _, err := h.start(t, "mismatch")
	if err == nil || s.Ready() {
		t.Fatal("wrong recorded baseline passed")
	}
	if err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestIsolationWrongPostconditionRemainsUncertain(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	h.target.wrongPost = true
	s, path, err := h.start(t, "wrong")
	if err == nil || s.Ready() {
		t.Fatal("successful HTTP response hid wrong actual state")
	}
	s.Close()
	result, err := isolation.Inspect(path)
	if err != nil || result.Setup != "uncertain" {
		t.Fatalf("lost bad postcondition: %+v %v", result, err)
	}
	if _, err := isolation.Reconcile(t.Context(), h.p, h.a, path, filepath.Join(h.root, "reconcile-wrong")); err == nil {
		t.Fatal("wrongly provisioned state became eligible for automatic cleanup")
	}
}

func TestIsolationReaderRejectsResealedEffectsAndSummaries(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	s, path, err := h.start(t, "source")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		name, _ := filepath.Rel(path, p)
		raw, e := os.ReadFile(p)
		files[filepath.ToSlash(name)] = raw
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"summary-version", "claimed-ready-without-effect", "lost-manual-provenance", "valid-other-resource-network-action", "orphan-intent", "changed-observed-journal", "wrong-checkpoint-binding"} {
		t.Run(variant, func(t *testing.T) {
			changed := map[string][]byte{}
			for name, raw := range files {
				changed[name] = bytes.Clone(raw)
			}
			var result isolation.Result
			_ = json.Unmarshal(changed["result.json"], &result)
			switch variant {
			case "orphan-intent":
				changed["entry-0099-intent.json"] = bytes.Clone(changed["entry-0002-intent.json"])
			case "changed-observed-journal":
				var entry isolation.Entry
				_ = json.Unmarshal(changed["entry-0002-observed.json"], &entry)
				entry.State = "uncertain"
				changed["entry-0002-observed.json"] = encode(entry)
			case "wrong-checkpoint-binding":
				var setup isolation.Result
				_ = json.Unmarshal(changed["setup.json"], &setup)
				setup.Plan = networkaction.Digest([]byte("another-plan"))
				changed["setup.json"] = encode(setup)
			case "summary-version":
				result.Resources[0].Version = "invented"
				var setup isolation.Result
				_ = json.Unmarshal(changed["setup.json"], &setup)
				setup.Resources[0].Version = "invented"
				changed["setup.json"] = encode(setup)
			case "claimed-ready-without-effect":
				result.Entries = result.Entries[:1]
			case "valid-other-resource-network-action":
				source := result.Entries[2].Network + "/"
				destination := result.Entries[1].Network + "/"
				for name, raw := range files {
					if strings.HasPrefix(name, source) {
						changed[destination+strings.TrimPrefix(name, source)] = bytes.Clone(raw)
					}
				}
			case "lost-manual-provenance":
				result.Manual = []isolation.ManualClaim{{ID: "invented", Provenance: "verified-database-reset"}}
			}
			changed["result.json"] = encode(result)
			changed["identity.sha256"] = []byte(artifactdir.Identity(isolation.ResultSchema, changed) + "\n")
			out := filepath.Join(t.TempDir(), "copy")
			for name, raw := range changed {
				dest := filepath.Join(out, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if variant == "valid-other-resource-network-action" {
				if _, err := networkaction.OpenHTTP(filepath.Join(out, result.Entries[1].Network)); err != nil {
					t.Fatalf("replacement was not independently valid: %v", err)
				}
			}
			if _, err := isolation.Open(out); err == nil {
				t.Fatal("resealed unsupported effect claim passed")
			}
		})
	}
}

func TestIsolationChangedTargetRevisionDoesNotUseStalePreflight(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	preflight := h.preflight(t, "before-redeployment")
	h.target.mu.Lock()
	h.target.revision = "2"
	h.target.mu.Unlock()
	s, err := isolation.Start(t.Context(), h.p, h.a, preflight, filepath.Join(h.root, "stale"), isolation.Confirmation{})
	if s != nil {
		defer s.Close()
	}
	if err == nil {
		t.Fatal("changed target accepted old reviewed revision")
	}
	h.target.mu.Lock()
	defer h.target.mu.Unlock()
	if len(h.target.effects) != 0 || len(h.target.resources) != 0 {
		t.Fatal("stale target preflight provisioned resources")
	}
}

func TestIsolationRechecksActualRunnerGrantBeforeCleanupEffects(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	paths := map[string]string{}
	grant := func(phase string) networkaction.Authority {
		path := filepath.Join(h.root, phase+"-grant.json")
		paths[phase] = path
		doc := networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: h.p.Review(phase).Binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)}
		if err := os.WriteFile(path, encode(doc), 0600); err != nil {
			t.Fatal(err)
		}
		return networkaction.FileAuthority{Path: path, Actor: "runner", Generation: "1"}
	}
	h.a = isolation.Authorities{Read: grant("read"), Setup: grant("setup"), Cleanup: grant("cleanup")}
	s, path, err := h.start(t, "run")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths["cleanup"])
	if err != nil {
		t.Fatal(err)
	}
	var changed networkaction.RunnerGrant
	_ = json.Unmarshal(raw, &changed)
	changed.Generation = "2"
	if err := os.WriteFile(paths["cleanup"], encode(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(t.Context()); err == nil {
		t.Fatal("rotated runner grant permitted cleanup")
	}
	s.Close()
	report, err := isolation.Inspect(path)
	if err != nil || report.Cleanup == "complete" {
		t.Fatalf("lost changed grant outcome: %+v %v", report, err)
	}
	h.target.mu.Lock()
	defer h.target.mu.Unlock()
	if len(h.target.resources) != 2 || strings.Join(h.target.effects, ",") != "create:patient,create:appointment" {
		t.Fatal("revoked cleanup reached target mutation")
	}
}

func TestIsolationLostReleaseResponseReconcilesObservedAbsenceWithoutRetry(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	s, path, err := h.start(t, "run")
	if err != nil {
		t.Fatal(err)
	}
	h.target.mu.Lock()
	h.target.failRelease = true
	h.target.mu.Unlock()
	if err := s.Cleanup(t.Context()); err == nil {
		t.Fatal("lost release response passed")
	}
	if s.Result().Setup != "ready" || s.Result().Cleanup != "uncertain" {
		t.Fatal("cleanup uncertainty overwrote known setup")
	}
	s.Close()
	out := filepath.Join(h.root, "reconcile-released")
	result, err := isolation.Reconcile(t.Context(), h.p, h.a, path, out)
	if err != nil {
		t.Fatal(err)
	}
	if result.Setup != "reconciled-not-ready" || result.Cleanup != "observed-absent" || result.Lease != (isolation.Lease{}) || len(result.Resources) != 0 || len(result.Manual) != 0 {
		t.Fatal("absence became restored setup or fabricated effects", result)
	}
	if _, err := isolation.Open(out); err != nil {
		t.Fatal(err)
	}
	if _, err := isolation.RecoveryReview(h.p, out, "cleanup"); err == nil {
		t.Fatal("already absent state offered repeat cleanup")
	}
	h.target.mu.Lock()
	defer h.target.mu.Unlock()
	if len(h.target.resources) != 0 || len(h.target.leases) != 0 || strings.Join(h.target.effects, ",") != "create:patient,create:appointment,delete:appointment,delete:patient" {
		t.Fatal("reconciliation retried an effect")
	}
}

func TestIsolationLeaseReleaseRequiresIndependentReadback(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	s, path, err := h.start(t, "held-release")
	if err != nil {
		t.Fatal(err)
	}
	h.target.mu.Lock()
	h.target.holdRelease = true
	h.target.mu.Unlock()
	if err := s.Cleanup(t.Context()); err == nil {
		t.Fatal("lying release response passed without actual lease removal")
	}
	if s.Result().Complete || s.Result().Cleanup != "uncertain" {
		t.Fatal("retained completed cleanup while target lease remained")
	}
	s.Close()
	if result, err := isolation.Inspect(path); err != nil || result.Complete || result.Cleanup != "uncertain" {
		t.Fatalf("lost lease failure: %+v %v", result, err)
	}
	h.target.mu.Lock()
	defer h.target.mu.Unlock()
	if len(h.target.leases) != 1 || len(h.target.resources) != 0 {
		t.Fatal("negative control did not retain only the lease")
	}
}

func TestIsolationRecoveryReviewNeverMixesValidReplacementVersions(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	h.target.failCreate = true
	s, path, err := h.start(t, "crash")
	if err == nil {
		t.Fatal("lost create response passed")
	}
	s.Close()
	a, b := filepath.Join(h.root, "version-a"), filepath.Join(h.root, "version-b")
	if _, err := isolation.Reconcile(t.Context(), h.p, h.a, path, a); err != nil {
		t.Fatal(err)
	}
	h.target.mu.Lock()
	for _, resource := range h.target.resources {
		resource["version"] = "new-version"
	}
	h.target.mu.Unlock()
	if _, err := isolation.Reconcile(t.Context(), h.p, h.a, path, b); err != nil {
		t.Fatal(err)
	}
	first, err := isolation.RecoveryReview(h.p, a, "cleanup")
	if err != nil {
		t.Fatal(err)
	}
	second, err := isolation.RecoveryReview(h.p, b, "cleanup")
	if err != nil {
		t.Fatal(err)
	}
	if first.Binding.Configuration == second.Binding.Configuration || first.Effects[0].Operation == second.Effects[0].Operation {
		t.Fatal("replacement control did not change versions and binding")
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		temporary := filepath.Join(h.root, "swapping")
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if err := os.Rename(a, temporary); err != nil {
				done <- err
				return
			}
			if err := os.Rename(b, a); err != nil {
				_ = os.Rename(temporary, a)
				done <- err
				return
			}
			if err := os.Rename(temporary, b); err != nil {
				done <- err
				return
			}
		}
	}()
	successes := 0
	for i := 0; i < 100; i++ {
		review, err := isolation.RecoveryReview(h.p, a, "cleanup")
		if err != nil {
			continue
		}
		successes++
		if !bytes.Equal(encode(review), encode(first)) && !bytes.Equal(encode(review), encode(second)) {
			close(stop)
			<-done
			t.Fatal("effects from one valid revision acquired another revision's authority")
		}
	}
	close(stop)
	if err := <-done; err != nil {
		t.Logf("filesystem stopped concurrent directory replacement: %v", err)
	}
	if successes == 0 {
		t.Fatal("no valid snapshot read during replacement")
	}
}

func TestIsolationAbruptCrashAfterReleaseReplyRetainsCleanupUncertainty(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	s, path, err := h.start(t, "release-crash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	complete := s.Result()
	last := len(complete.Entries)
	if complete.Entries[last-1].Action != "lease-release" {
		t.Fatal("fixture does not end at release")
	}
	// Remove only records not durable at the simulated abrupt crash. There is
	// no orderly Close checkpoint and no recorded release postcondition.
	for _, name := range []string{"identity.sha256", "result.json", fmt.Sprintf("entry-%04d-observed.json", last)} {
		if err := os.Remove(filepath.Join(path, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(path, "interrupted.json")); !os.IsNotExist(err) {
		t.Fatal("fixture unexpectedly contains orderly-close checkpoint")
	}
	actions, err := os.ReadDir(filepath.Join(path, "actions"))
	if err != nil {
		t.Fatal(err)
	}
	finalRead := actions[len(actions)-1].Name()
	if "actions/"+finalRead <= complete.Entries[last-1].Network {
		t.Fatal("fixture lacks final observer read")
	}
	if err := os.RemoveAll(filepath.Join(path, "actions", finalRead)); err != nil {
		t.Fatal(err)
	}
	report, err := isolation.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete || report.Cleanup != "uncertain" || report.Setup != "ready" {
		t.Fatalf("release attempt lost or overwrote setup facts: %+v", report)
	}
}
