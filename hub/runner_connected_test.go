package hub_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

type connectedHubFixture struct {
	t          *testing.T
	config     hub.Config
	store      *hub.Store
	access     *hub.Access
	accessPath string
	policy     hub.AccessPolicy
	grants     runnerprotocol.ConnectedPolicy
	grantPath  string
	host       hub.Config
	client     *http.Client
	server     *httptest.Server
	token      string
	clock      *hubClock
	request    runnerprotocol.ConnectedRequest
	requests   atomic.Int64
	wire       atomic.Pointer[connectedHubWire]
}

// Fault injection belongs to the authenticated HTTP response boundary. The
// underlying customer hub still authorizes and durably handles every request.
type connectedHubWire struct {
	serve func(http.ResponseWriter, *http.Request, http.Handler)
}

func newConnectedHubFixture(t *testing.T) *connectedHubFixture {
	t.Helper()
	f := &connectedHubFixture{t: t, config: integrationConfig(t), clock: newHubClock()}
	cert := certificates(t, &f.host)
	f.access, f.policy, _, f.accessPath = accessFixture(t)
	f.token = "rh_" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("q", 32)))
	f.policy.Tokens = []hub.ScopedToken{{Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(f.token))), Subject: "runner", Project: "alpha", Actions: []string{"enrollment", "execution"}, Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Certificate: fmt.Sprintf("%x", sha256.Sum256(cert.Certificate[0])), Kind: "runner"}}
	writePolicy(t, f.accessPath, f.policy)
	f.request = runnerprotocol.ConnectedRequest{Schema: runnerprotocol.ConnectedRequestSchema, Environment: "lab", Instance: "worker-one", Job: "slot-one", Engine: "dev", Input: strings.Repeat("a", 64), Resources: []string{strings.Repeat("b", 64)},
		Capabilities: runnerprotocol.Capabilities{Schema: runnerprotocol.CapabilitiesSchema, Engine: "dev", Pins: []runnerprotocol.CapabilityPin{{Kind: "contract", ID: "readmit-connected-test", Version: "v1"}}}}
	identity, err := f.request.Capabilities.Identity()
	if err != nil {
		t.Fatal(err)
	}
	f.grants = runnerprotocol.ConnectedPolicy{Schema: runnerprotocol.ConnectedPolicySchema, Runners: []runnerprotocol.ConnectedGrant{{Project: "alpha", Subject: "runner", Environment: "lab", Engine: "dev", Capability: identity, MaxSeconds: 30, MaxJobs: 2}}}
	f.grantPath = filepath.Join(t.TempDir(), "runners.json")
	f.writeGrants()
	reset(t, testDatabase(t, f.config))
	f.store = open(t, f.config)
	if err := f.store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(f.host.ClientCA)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid fixture CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}}
	f.client = &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(transport.CloseIdleConnections)
	f.start()
	f.clock.advance(runnerHold)
	t.Cleanup(func() { f.server.Close() })
	return f
}

func (f *connectedHubFixture) writeGrants() {
	f.t.Helper()
	b, err := json.Marshal(f.grants)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(f.grantPath+".connected", b, 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *connectedHubFixture) start() {
	f.t.Helper()
	runner := f.store.RunnerHandlerWithClockForTest(f.access, f.grantPath, f.clock.now)
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if wire := f.wire.Load(); wire != nil {
			wire.serve(w, r, runner)
			return
		}
		runner.ServeHTTP(w, r)
	}))
	var err error
	f.server.TLS, err = f.host.TLS()
	if err != nil {
		f.t.Fatal(err)
	}
	f.server.StartTLS()
}

// runnerConfig provisions the private configuration and synthetic external
// credential-store references the production client reads. Only filenames
// enter the config; private-key bytes and scoped tokens remain in the provider.
func (f *connectedHubFixture) runnerConfig() customerrunner.Config {
	f.t.Helper()
	dir := f.t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		f.t.Fatal(err)
	}
	cert := f.client.Transport.(*http.Transport).TLSClientConfig.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		f.t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"client.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}),
		"key.pem":    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
		"token":      []byte(f.token),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			f.t.Fatal(err)
		}
	}
	return customerrunner.Config{Schema: "readmit-runner/v1", Hub: f.server.URL, Project: "alpha", Environment: "lab", Root: dir, CA: f.host.ClientCA, Certificate: filepath.Join(dir, "client.pem"),
		Key: customerrunner.Reference{Command: "/bin/cat", Arguments: []string{filepath.Join(dir, "key.pem")}}, Token: customerrunner.Reference{Command: "/bin/cat", Arguments: []string{filepath.Join(dir, "token")}},
		UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "next"}
}

func (f *connectedHubFixture) restart() {
	f.t.Helper()
	f.server.Close()
	if err := f.store.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.store = open(f.t, f.config)
	f.start()
	f.clock.advance(runnerHold)
}

func (f *connectedHubFixture) call(path string, v any) (int, []byte) {
	return f.callAt(http.MethodPost, "/v2/projects/alpha/runner"+path, v)
}

func (f *connectedHubFixture) callAt(method, path string, v any) (int, []byte) {
	f.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		f.t.Fatal(err)
	}
	r, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(b))
	if err != nil {
		f.t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("Content-Type", "application/json")
	response, err := f.client.Do(r)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return response.StatusCode, body
}

func (f *connectedHubFixture) admit(request runnerprotocol.ConnectedRequest) runnerprotocol.ConnectedLease {
	f.t.Helper()
	status, body := f.call("", request)
	if status != http.StatusOK {
		f.t.Fatalf("dispatch not admitted: status=%d body=%s", status, body)
	}
	lease, err := runnerprotocol.DecodeConnectedLease(body)
	if err != nil {
		f.t.Fatal(err)
	}
	return lease
}

func (f *connectedHubFixture) command(request runnerprotocol.ConnectedRequest, lease runnerprotocol.ConnectedLease) runnerprotocol.ConnectedCommand {
	return runnerprotocol.ConnectedCommand{Schema: runnerprotocol.ConnectedCommandSchema, Request: request, Generation: lease.Generation}
}

func TestConnectedHubLostWorkerHoldsResourcesAndNeverResends(t *testing.T) {
	f := newConnectedHubFixture(t)
	lease := f.admit(f.request)
	var witnessed atomic.Int64
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { witnessed.Add(1); w.WriteHeader(204) }))
	defer downstream.Close()
	effect := f.command(f.request, lease)
	effect.Step = "effect-1"
	if status, body := f.call("/attempt", effect); status != http.StatusNoContent {
		t.Fatalf("effect not admitted: %d %s", status, body)
	}
	response, err := downstream.Client().Post(downstream.URL, "application/json", bytes.NewBufferString(`{"synthetic":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	// The customer worker disappeared before retaining a settled response.
	f.restart()
	other := f.request
	other.Instance = "worker-two"
	other.Job = "slot-two"
	for _, request := range []runnerprotocol.ConnectedRequest{f.request, other} {
		if status, _ := f.call("", request); status != http.StatusConflict {
			t.Fatalf("lost worker dispatch or resource recycled: %d", status)
		}
	}
	if status, _ := f.call("/attempt", effect); status != http.StatusConflict {
		t.Fatalf("effect replayed after restart: %d", status)
	}
	f.clock.advance(time.Minute)
	if status, _ := f.call("", other); status != http.StatusConflict {
		t.Fatalf("expired uncertainty recycled: %d", status)
	}
	settle := f.command(f.request, lease)
	settle.Outcome = "settled"
	if status, _ := f.call("/settle", settle); status != http.StatusConflict {
		t.Fatalf("stale old fence released restarted uncertainty: %d", status)
	}
	settle.Outcome = "uncertain"
	if status, _ := f.call("/settle", settle); status != http.StatusNoContent {
		t.Fatalf("uncertainty could not be retained: %d", status)
	}
	if status, _ := f.call("", other); status != http.StatusConflict || witnessed.Load() != 1 {
		t.Fatalf("uncertainty released or stimulus repeated: %d witnessed=%d", status, witnessed.Load())
	}
	settle.Outcome = "settled"
	if status, _ := f.call("/settle", settle); status != http.StatusConflict {
		t.Fatalf("explicit uncertain settlement was upgraded to permission: %d", status)
	}
}

func TestConnectedHubSettlesOnlyTheMatchingFenceAndAdmitsIndependentJobs(t *testing.T) {
	f := newConnectedHubFixture(t)
	first := f.admit(f.request)
	independent := f.request
	independent.Job = "slot-independent"
	independent.Instance = "worker-two"
	independent.Resources = []string{strings.Repeat("c", 64)}
	second := f.admit(independent)
	if second.Generation <= first.Generation {
		t.Fatal("fence generation did not advance")
	}
	third := f.request
	third.Job = "slot-third"
	third.Resources = []string{strings.Repeat("d", 64)}
	if status, _ := f.call("", third); status != http.StatusServiceUnavailable {
		t.Fatalf("concurrent job cap exceeded: %d", status)
	}
	wrong := f.command(f.request, first)
	wrong.Generation = second.Generation
	wrong.Outcome = "settled"
	if status, _ := f.call("/settle", wrong); status != http.StatusConflict {
		t.Fatalf("another dispatch fence settled ownership: %d", status)
	}
	collision := f.request
	collision.Job = "slot-collision"
	if status, _ := f.call("", collision); status != http.StatusConflict {
		t.Fatalf("shared target reached concurrent admission: %d", status)
	}
	settle := f.command(f.request, first)
	settle.Outcome = "settled"
	if status, body := f.call("/settle", settle); status != http.StatusNoContent {
		t.Fatalf("matching terminal fence refused: %d %s", status, body)
	}
	newOwner := f.admit(collision)
	if newOwner.Generation <= second.Generation {
		t.Fatal("settled resource reused an old fence generation")
	}
	// A repeated settlement of the old fence cannot release the new owner.
	if status, _ := f.call("/settle", settle); status != http.StatusNoContent {
		t.Fatalf("idempotent terminal settlement refused: %d", status)
	}
	other := f.request
	other.Job = "slot-other"
	if status, _ := f.call("", other); status != http.StatusConflict {
		t.Fatalf("stale settlement released a newer resource fence: %d", status)
	}
	f.restart()
	if status, _ := f.call("", f.request); status != http.StatusConflict {
		t.Fatalf("settled dispatch replayed after restart: %d", status)
	}
}

func TestConnectedHubRefusesPinChangesRevocationAndRepeatedEffects(t *testing.T) {
	f := newConnectedHubFixture(t)
	wrong := f.request
	wrong.Engine = "wrong-engine"
	wrong.Capabilities.Engine = wrong.Engine
	if status, _ := f.call("", wrong); status != http.StatusForbidden {
		t.Fatalf("wrong engine admitted: %d", status)
	}
	wrong = f.request
	wrong.Capabilities.Pins = []runnerprotocol.CapabilityPin{{Kind: "contract", ID: "readmit-connected-test", Version: "v2"}}
	if status, _ := f.call("", wrong); status != http.StatusForbidden {
		t.Fatalf("wrong connected contract admitted: %d", status)
	}
	for _, kind := range []string{"profile", "terminology", "validator", "validator-worker"} {
		wrong = f.request
		wrong.Capabilities.Pins = []runnerprotocol.CapabilityPin{{Kind: kind, ID: "required", Version: "wrong", SHA256: strings.Repeat("e", 64)}}
		if status, _ := f.call("", wrong); status != http.StatusForbidden {
			t.Fatalf("wrong %s pin admitted: %d", kind, status)
		}
	}
	lease := f.admit(f.request)
	changed := f.command(f.request, lease)
	changed.Request.Input = strings.Repeat("c", 64)
	if status, _ := f.call("/renew", changed); status != http.StatusConflict {
		t.Fatalf("changed input renewed an admitted occurrence: %d", status)
	}
	effect := f.command(f.request, lease)
	effect.Step = "effect-1"
	if status, _ := f.call("/attempt", effect); status != http.StatusNoContent {
		t.Fatalf("first effect refused: %d", status)
	}
	if status, _ := f.call("/attempt", effect); status != http.StatusConflict {
		t.Fatalf("effect attempted twice: %d", status)
	}
	f.grants.Runners = []runnerprotocol.ConnectedGrant{}
	f.writeGrants()
	effect.Step = "effect-2"
	if status, _ := f.call("/attempt", effect); status != http.StatusForbidden {
		t.Fatalf("revoked grant admitted an effect: %d", status)
	}
	identity, _ := f.request.Capabilities.Identity()
	f.grants.Runners = []runnerprotocol.ConnectedGrant{{Project: "alpha", Subject: "runner", Environment: "lab", Engine: "dev", Capability: identity, MaxSeconds: 30, MaxJobs: 2}}
	f.writeGrants()
	f.policy.Tokens = []hub.ScopedToken{}
	writePolicy(t, f.accessPath, f.policy)
	if status, _ := f.call("/attempt", effect); status != http.StatusForbidden {
		t.Fatalf("revoked runner principal admitted an effect: %d", status)
	}
}

func TestConnectedHubExpiredAuthorityCannotBeReassigned(t *testing.T) {
	f := newConnectedHubFixture(t)
	lease := f.admit(f.request)
	f.clock.advance(11 * time.Second)
	command := f.command(f.request, lease)
	for _, action := range []string{"/renew", "/attempt"} {
		command.Step = ""
		if action == "/attempt" {
			command.Step = "effect-1"
		}
		if status, _ := f.call(action, command); status != http.StatusConflict {
			t.Fatalf("expired authority accepted %s: %d", action, status)
		}
	}
	other := f.request
	other.Job, other.Instance = "slot-new", "worker-new"
	late := f.command(f.request, lease)
	late.Outcome = "settled"
	if status, _ := f.call("/settle", late); status != http.StatusConflict {
		t.Fatalf("expired old fence settled uncertainty: %d", status)
	}
	if status, _ := f.call("", other); status != http.StatusConflict {
		t.Fatalf("expired target resource reassigned: %d", status)
	}
	f.restart()
	if status, _ := f.call("/settle", late); status != http.StatusConflict {
		t.Fatalf("restarted expired old fence settled uncertainty: %d", status)
	}
	if status, _ := f.call("", other); status != http.StatusConflict {
		t.Fatalf("expired ownership lost on restart: %d", status)
	}
}

func TestConnectedHubInterruptedStateWriteRefusesEveryNewEffect(t *testing.T) {
	f := newConnectedHubFixture(t)
	lease := f.admit(f.request)
	if err := os.WriteFile(filepath.Join(f.config.Root, ".runner-connected-next"), []byte(`{"interrupted":`), 0600); err != nil {
		t.Fatal(err)
	}
	command := f.command(f.request, lease)
	command.Step = "effect-1"
	if status, _ := f.call("/attempt", command); status != http.StatusServiceUnavailable {
		t.Fatalf("interrupted durable write admitted an effect: %d", status)
	}
	f.restart()
	other := f.request
	other.Job, other.Instance = "slot-new", "worker-new"
	if status, _ := f.call("", other); status != http.StatusServiceUnavailable {
		t.Fatalf("restarted hub ignored the interrupted write: %d", status)
	}
}

func TestConnectedHubPolicyTransitionWaitsForLegacyLeaseAndPreservesLegacyReader(t *testing.T) {
	f := newConnectedHubFixture(t)
	if err := os.Remove(f.grantPath + ".connected"); err != nil {
		t.Fatal(err)
	}
	policy := runnerprotocol.Policy{Schema: "readmit-runner-policy/v1", Runners: []runnerprotocol.Grant{{Project: "alpha", Subject: "runner", Environment: "lab", Engine: "dev", Spec: "readmit-test/v1", Profile: "readmit-siu-v1", MaxSeconds: 30, MaxJobs: 2}}}
	b, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.grantPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	legacy := runnerprotocol.Request{Schema: "readmit-runner-request/v1", Environment: "lab", Instance: "legacy-worker", Job: "legacy-slot", Engine: "dev", Spec: "readmit-test/v1", Profile: "readmit-siu-v1"}
	status, body := f.callAt(http.MethodPost, "/v1/projects/alpha/runner", legacy)
	if status != http.StatusOK {
		t.Fatalf("unchanged legacy environment refused: %d %s", status, body)
	}
	if _, err := runnerprotocol.DecodeLease(body); err != nil {
		t.Fatal("legacy lease format changed", err)
	}
	// Installing v2 authority cannot overlap the outstanding legacy lease.
	f.writeGrants()
	if status, _ := f.call("", f.request); status != http.StatusConflict {
		t.Fatalf("connected dispatch overlapped a live legacy lease: %d", status)
	}
	if status, body := f.callAt(http.MethodPost, "/v1/projects/alpha/runner", legacy); status != http.StatusConflict || !strings.Contains(string(body), "connected runner v2") {
		t.Fatalf("reserved legacy environment continued admission: %d %s", status, body)
	}
	if status, _ := f.callAt(http.MethodDelete, "/v1/projects/alpha/runner", legacy); status != http.StatusNoContent {
		t.Fatalf("matching legacy lease could not complete: %d", status)
	}
	f.admit(f.request)
	// Removing the v2 grant cannot reopen a legacy path around durable ownership.
	if err := os.Remove(f.grantPath + ".connected"); err != nil {
		t.Fatal(err)
	}
	if status, _ := f.callAt(http.MethodPost, "/v1/projects/alpha/runner", legacy); status != http.StatusConflict {
		t.Fatalf("grant withdrawal discarded connected ownership: %d", status)
	}
}

func TestConnectedHubBackupRefusesToDiscardDispatchAndInterruptedClaims(t *testing.T) {
	f := newConnectedHubFixture(t)
	lease := f.admit(f.request)
	command := f.command(f.request, lease)
	command.Step = "effect-1"
	if status, _ := f.call("/attempt", command); status != http.StatusNoContent {
		t.Fatalf("effect refused: %d", status)
	}
	destination := filepath.Join(t.TempDir(), "backup")
	if err := f.store.Backup(context.Background(), destination); !errors.Is(err, hub.ErrSchedule) {
		t.Fatalf("backup omitted connected dispatch claims: %v", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused backup created an incomplete destination: %v", err)
	}
	// Even a staged first claim, without a complete state document, prevents a
	// backup from silently treating a lost dispatch as an empty runner history.
	if err := os.Remove(filepath.Join(f.config.Root, "runner-connected.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.config.Root, ".runner-connected-next"), []byte(`{"interrupted":`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Backup(context.Background(), destination); !errors.Is(err, hub.ErrSchedule) {
		t.Fatalf("backup omitted interrupted first dispatch: %v", err)
	}
}

func TestConnectedCustomerClientUsesRealAdmissionBeforeEveryEffect(t *testing.T) {
	f := newConnectedHubFixture(t)
	c := f.runnerConfig()
	path := filepath.Join(c.Root, "runner.json")
	b, err := json.Marshal(c)
	if err != nil || os.WriteFile(path, b, 0600) != nil {
		t.Fatal("cannot provision synthetic runner configuration")
	}
	before := f.requests.Load()
	c, err = customerrunner.ReadConfig(path)
	if err != nil || f.requests.Load() != before {
		t.Fatalf("offline config read contacted authority: %v requests=%d", err, f.requests.Load()-before)
	}
	client, err := customerrunner.EnrollConnected(context.Background(), c, f.request)
	if err != nil {
		t.Fatal(err)
	}
	lease := client.Lease()
	if lease.Generation == 0 || lease.MaxJobs != 2 || lease.MaxSeconds != 30 {
		t.Fatalf("unexpected capability lease: %+v", lease)
	}
	var witnessed atomic.Int64
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { witnessed.Add(1); w.WriteHeader(204) }))
	defer downstream.Close()
	effect := func(step string) error {
		if err := client.Attempt(context.Background(), step); err != nil {
			return err
		}
		response, err := downstream.Client().Post(downstream.URL, "application/json", bytes.NewBufferString(`{"synthetic":"one"}`))
		if err == nil {
			response.Body.Close()
		}
		return err
	}
	if err := effect("effect-1"); err != nil {
		t.Fatal(err)
	}
	f.grants.Runners = []runnerprotocol.ConnectedGrant{}
	f.writeGrants()
	if err := effect("effect-2"); !errors.Is(err, customerrunner.ErrHubRefused) || witnessed.Load() != 1 {
		t.Fatalf("revoked grant did not stop the next target effect: %v witnessed=%d", err, witnessed.Load())
	}
	if _, err := customerrunner.EnrollConnected(context.Background(), c, f.request); err == nil {
		t.Fatal("revoked grant admitted a fresh client")
	}
}

func TestConnectedCustomerClientPreservesShortBudgetAndTerminalFence(t *testing.T) {
	f := newConnectedHubFixture(t)
	f.grants.Runners[0].MaxSeconds = 5
	f.writeGrants()
	c := f.runnerConfig()
	request := f.request
	request.Capabilities.Pins = slices.Clone(request.Capabilities.Pins)
	request.Resources = slices.Clone(request.Resources)
	client, err := customerrunner.EnrollConnected(context.Background(), c, request)
	if err != nil {
		t.Fatal("valid five-second job authority refused", err)
	}
	initial := client.Lease()
	if initial.MaxSeconds != 5 {
		t.Fatalf("short duration was widened: %+v", initial)
	}
	// Neither mutation of caller-owned metadata nor a higher installed cap can
	// change the exact request or the original dispatch's authority.
	request.Capabilities.Pins[0].Version = "v99"
	request.Resources[0] = strings.Repeat("f", 64)
	c.Key.Arguments[0] = "/never-selected"
	c.Token.Arguments[0] = "/never-selected"
	f.grants.Runners[0].MaxSeconds = 300
	f.grants.Runners[0].MaxJobs = 10
	f.writeGrants()
	if err := client.Check(context.Background()); err != nil {
		t.Fatal("caller mutation changed live agreement", err)
	}
	current := client.Lease()
	if current.Generation != initial.Generation || current.MaxSeconds != 5 || current.MaxJobs != 2 || current.Expires.After(initial.Expires) {
		t.Fatalf("existing job authority widened: before=%+v after=%+v", initial, current)
	}
	if err := client.Attempt(context.Background(), "effect-1"); err != nil {
		t.Fatal(err)
	}
	if err := client.Settle(context.Background(), false); err != nil {
		t.Fatal("known terminal state could not settle its fence", err)
	}
	before := f.requests.Load()
	if client.Check(context.Background()) == nil || client.Attempt(context.Background(), "effect-2") == nil || client.Settle(context.Background(), false) == nil || f.requests.Load() != before {
		t.Fatal("a terminal client recycled dispatch authority")
	}
	// Inspect the original occurrence as received, rather than the mutated
	// caller value: a fresh client cannot reuse a terminal dispatch ID.
	if _, err := customerrunner.EnrollConnected(context.Background(), f.runnerConfig(), f.request); err == nil {
		t.Fatal("terminal dispatch was admitted again")
	}
}

func TestConnectedCustomerClientRejectsChangedFenceCapsAndInvalidExpiry(t *testing.T) {
	for name, change := range map[string]func(*runnerprotocol.ConnectedLease){
		"fence":    func(l *runnerprotocol.ConnectedLease) { l.Generation++ },
		"duration": func(l *runnerprotocol.ConnectedLease) { l.MaxSeconds++ },
		"jobs":     func(l *runnerprotocol.ConnectedLease) { l.MaxJobs++ },
		"expired":  func(l *runnerprotocol.ConnectedLease) { l.Expires = time.Now().UTC().Add(-time.Second) },
		"too long": func(l *runnerprotocol.ConnectedLease) { l.Expires = time.Now().UTC().Add(time.Hour) },
		"v1":       func(l *runnerprotocol.ConnectedLease) { l.Schema = "readmit-runner-lease/v1" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newConnectedHubFixture(t)
			client, err := customerrunner.EnrollConnected(context.Background(), f.runnerConfig(), f.request)
			if err != nil {
				t.Fatal(err)
			}
			initial := client.Lease()
			f.wire.Store(&connectedHubWire{serve: func(w http.ResponseWriter, r *http.Request, next http.Handler) {
				if !strings.HasSuffix(r.URL.Path, "/renew") {
					next.ServeHTTP(w, r)
					return
				}
				recorded := httptest.NewRecorder()
				next.ServeHTTP(recorded, r)
				if recorded.Code != http.StatusOK {
					t.Error("real hub could not renew before wire fault")
					w.WriteHeader(recorded.Code)
					return
				}
				lease, err := runnerprotocol.DecodeConnectedLease(recorded.Body.Bytes())
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				change(&lease)
				b, _ := json.Marshal(lease)
				w.Header().Set("Content-Type", "application/json")
				w.Write(b)
			}})
			if err := client.Check(context.Background()); !errors.Is(err, customerrunner.ErrRefused) || client.Lease() != initial {
				t.Fatalf("changed authority accepted: %v lease=%+v", err, client.Lease())
			}
			before := f.requests.Load()
			if client.Attempt(context.Background(), "effect-1") == nil || client.Settle(context.Background(), false) == nil || f.requests.Load() != before {
				t.Fatal("invalid authority silently re-enrolled or released uncertainty")
			}
			f.wire.Store(nil)
			if err := client.Settle(context.Background(), true); err != nil {
				t.Fatal("invalid authority could not retain uncertain completion", err)
			}
			other := f.request
			other.Job, other.Instance = "other-slot", "other-worker"
			if status, _ := f.call("", other); status != http.StatusConflict {
				t.Fatalf("invalid authority released an unresolved resource: %d", status)
			}
		})
	}
}

func TestConnectedCustomerClientRetainsAmbiguousAttemptWithoutRetryOrSecretDiagnostic(t *testing.T) {
	f := newConnectedHubFixture(t)
	client, err := customerrunner.EnrollConnected(context.Background(), f.runnerConfig(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	const diagnostic = "synthetic-sensitive-provider-diagnostic"
	f.wire.Store(&connectedHubWire{serve: func(w http.ResponseWriter, r *http.Request, next http.Handler) {
		if !strings.HasSuffix(r.URL.Path, "/attempt") {
			next.ServeHTTP(w, r)
			return
		}
		recorded := httptest.NewRecorder()
		next.ServeHTTP(recorded, r)
		if recorded.Code != http.StatusNoContent {
			t.Error("real hub did not retain the attempted effect")
		}
		// The authority retained the attempt but its response was lost or
		// replaced by an intermediary's diagnostic. The caller cannot know it
		// is safe to repeat the step, and must never echo the response text.
		http.Error(w, diagnostic, http.StatusServiceUnavailable)
	}})
	before := f.requests.Load()
	err = client.Attempt(context.Background(), "effect-1")
	if !errors.Is(err, customerrunner.ErrHubRefused) || strings.Contains(err.Error(), diagnostic) || strings.Contains(err.Error(), f.token) || f.requests.Load()-before != 2 {
		t.Fatalf("ambiguous attempt retried or exposed a diagnostic: %v requests=%d", err, f.requests.Load()-before)
	}
	f.wire.Store(nil)
	before = f.requests.Load()
	if client.Attempt(context.Background(), "effect-1") == nil || client.Attempt(context.Background(), "effect-2") == nil || client.Check(context.Background()) == nil || f.requests.Load() != before {
		t.Fatal("ambiguous attempt silently reacquired effect authority")
	}
	if err := client.Settle(context.Background(), true); err != nil {
		t.Fatal("uncertain attempt was not retained", err)
	}
	other := f.request
	other.Job, other.Instance = "other-slot", "other-worker"
	if status, _ := f.call("", other); status != http.StatusConflict {
		t.Fatalf("uncertain attempt released its resource: %d", status)
	}
	badConfig := f.runnerConfig()
	badConfig.Token = customerrunner.Reference{Command: "/bin/sh", Arguments: []string{"-c", "printf synthetic-sensitive-provider-diagnostic >&2; exit 1"}}
	errRequest := f.request
	errRequest.Job = "unresolved-provider"
	if _, err := customerrunner.EnrollConnected(context.Background(), badConfig, errRequest); !errors.Is(err, customerrunner.ErrRefused) || strings.Contains(err.Error(), diagnostic) {
		t.Fatalf("credential provider diagnostic exposed: %v", err)
	}
}

func TestConnectedCustomerClientStopsAtExpiryAndCannotSilentlyReenroll(t *testing.T) {
	f := newConnectedHubFixture(t)
	client, err := customerrunner.EnrollConnected(context.Background(), f.runnerConfig(), f.request)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(11 * time.Second)
	if err := client.Attempt(context.Background(), "effect-1"); !errors.Is(err, customerrunner.ErrHubRefused) {
		t.Fatalf("expired authority admitted an effect: %v", err)
	}
	before := f.requests.Load()
	if client.Attempt(context.Background(), "effect-2") == nil || client.Check(context.Background()) == nil || f.requests.Load() != before {
		t.Fatal("expired authority retried admission")
	}
	f.restart()
	if _, err := customerrunner.EnrollConnected(context.Background(), f.runnerConfig(), f.request); !errors.Is(err, customerrunner.ErrHubRefused) {
		t.Fatalf("hub restart forgot expired ownership: %v", err)
	}
}

func TestConnectedCustomerClientRefusesWrongEnvironmentAndEngineBeforeRPC(t *testing.T) {
	f := newConnectedHubFixture(t)
	c := f.runnerConfig()
	before := f.requests.Load()
	for _, request := range []runnerprotocol.ConnectedRequest{
		{Schema: runnerprotocol.ConnectedRequestSchema},
		func() runnerprotocol.ConnectedRequest { r := f.request; r.Environment = "other"; return r }(),
		func() runnerprotocol.ConnectedRequest {
			r := f.request
			r.Engine, r.Capabilities.Engine = "wrong", "wrong"
			return r
		}(),
	} {
		if _, err := customerrunner.EnrollConnected(context.Background(), c, request); !errors.Is(err, customerrunner.ErrRefused) {
			t.Fatalf("invalid local admission metadata accepted: %v", err)
		}
	}
	if f.requests.Load() != before {
		t.Fatal("invalid environment or engine contacted admission")
	}
}
