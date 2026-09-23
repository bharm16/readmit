package hub_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/testlicense"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The subprocess represents the customer's credential store, with synthetic keys only.
func TestRunnerProvider(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--runner-fixture" && i+1 < len(os.Args) {
			b, e := os.ReadFile(os.Args[i+1])
			if e != nil {
				os.Exit(1)
			}
			os.Stdout.Write(b)
			os.Exit(0)
		}
	}
}
func TestRunnerProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--runner-process" && i+2 < len(os.Args) {
			c, err := customerrunner.ReadConfig(os.Args[i+1])
			if err != nil {
				os.Exit(2)
			}
			job, err := customerrunner.ReadJob(os.Args[i+2])
			if err != nil {
				os.Exit(2)
			}
			_, err = customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, job)
			if err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
	}
}

// runnerHold is the documented ten seconds a new hub handler refuses every
// lease for.
const runnerHold = 10 * time.Second

// hubClock is the hub's clock in a test that drives a real runner client: the
// host's clock plus an offset the test moves. The client refuses a lease that
// grants more than ten seconds by the host's clock, so the hub's clock must not
// run ahead of the host's. It starts one hold behind, so a handler built on it
// starts inside its hold; advancing it by the hold ends the hold without
// waiting and leaves the hub on the host's clock for every lease after.
type hubClock struct{ offset atomic.Int64 }

func newHubClock() *hubClock {
	c := &hubClock{}
	c.offset.Store(int64(-runnerHold))
	return c
}
func (c *hubClock) now() time.Time          { return time.Now().Add(time.Duration(c.offset.Load())) }
func (c *hubClock) advance(d time.Duration) { c.offset.Add(int64(d)) }

// TestRunnerHoldIssuesNoLeaseBeforeItEnds proves on the hub's own clock, to
// the nanosecond and without waiting, the ordering the hold exists for: a new
// handler issues no lease until ten seconds after it starts, then issues one;
// and a handler restarted at the instant its predecessor issued a lease, which
// it never saw, refuses every other runner until that lease has expired.
func TestRunnerHoldIssuesNoLeaseBeforeItEnds(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	store := open(t, c)
	if e := store.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	a, policy, _, accessPath := accessFixture(t)
	token := "rh_" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("h", 32)))
	policy.Tokens = []hub.ScopedToken{{Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(token))), Subject: "runner", Project: "alpha", Actions: []string{"enrollment", "execution"}, Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Certificate: fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic-client-cert"))), Kind: "runner"}}
	writePolicy(t, accessPath, policy)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	grants := filepath.Join(dir, "runners.json")
	raw, _ := json.Marshal(runnerprotocol.Policy{Schema: "readmit-runner-policy/v1", Runners: []runnerprotocol.Grant{{Project: "alpha", Subject: "runner", Environment: "lab", Engine: "dev", Spec: "readmit-test/v1", Profile: "readmit-siu-v1", MaxSeconds: 300, MaxJobs: 2}}})
	if e := os.WriteFile(grants, raw, 0600); e != nil {
		t.Fatal(e)
	}
	// Each request is answered in this goroutine, so the clock needs no lock.
	started := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := started
	clock := func() time.Time { return now }
	lease := func(handler http.Handler, instance string) (int, runnerprotocol.Lease) {
		t.Helper()
		body, _ := json.Marshal(runnerprotocol.Request{Schema: "readmit-runner-request/v1", Environment: "lab", Instance: instance, Job: "job", Engine: "dev", Spec: "readmit-test/v1", Profile: "readmit-siu-v1"})
		r := request(token)
		r.Method = "POST"
		r.URL.Path = "/v1/projects/alpha/runner"
		r.Body = io.NopCloser(bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			return w.Code, runnerprotocol.Lease{}
		}
		granted, e := runnerprotocol.DecodeLease(w.Body.Bytes())
		if e != nil {
			t.Fatal(e)
		}
		return w.Code, granted
	}
	first := store.RunnerHandlerWithClockForTest(a, grants, clock)
	for _, at := range []time.Time{started, started.Add(runnerHold - time.Nanosecond)} {
		now = at
		if code, _ := lease(first, "first"); code != http.StatusConflict {
			t.Fatalf("lease %v after the handler started: status %d, want %d", at.Sub(started), code, http.StatusConflict)
		}
	}
	now = started.Add(runnerHold)
	code, granted := lease(first, "first")
	if code != http.StatusOK || !granted.Expires.Equal(now.Add(10*time.Second)) {
		t.Fatalf("lease as the hold ends: status %d, %+v", code, granted)
	}
	restarted := store.RunnerHandlerWithClockForTest(a, grants, clock)
	now = granted.Expires.Add(-time.Nanosecond)
	if code, _ := lease(restarted, "second"); code != http.StatusConflict {
		t.Fatalf("restarted hub leased while its predecessor's lease was current: status %d", code)
	}
	now = granted.Expires
	if code, _ := lease(restarted, "second"); code != http.StatusOK {
		t.Fatalf("restarted hub once the predecessor's lease expired: status %d", code)
	}
}

func TestCustomerRunnerActualTLSExecutionRevocationAndRecovery(t *testing.T) {
	var hc hub.Config
	cert := certificates(t, &hc)
	a, policy, _, accessPath := accessFixture(t)
	token := "rh_" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	policy.Tokens = []hub.ScopedToken{{Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(token))), Subject: "runner", Project: "alpha", Actions: []string{"enrollment", "execution"}, Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Certificate: fmt.Sprintf("%x", sha256.Sum256(cert.Certificate[0])), Kind: "runner"}}
	writePolicy(t, accessPath, policy)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	runnerPolicy := filepath.Join(dir, "runners.json")
	grants := runnerprotocol.Policy{Schema: "readmit-runner-policy/v1", Runners: []runnerprotocol.Grant{{Project: "alpha", Subject: "runner", Environment: "lab", Engine: "dev", Spec: "readmit-test/v1", Profile: "readmit-siu-v1", MaxSeconds: 30, MaxJobs: 2}}}
	raw, _ := json.Marshal(grants)
	os.WriteFile(runnerPolicy, raw, 0600)
	storeConfig := integrationConfig(t)
	db := testDatabase(t, storeConfig)
	reset(t, db)
	store := open(t, storeConfig)
	if e := store.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	clock := newHubClock()
	server := httptest.NewUnstartedServer(store.RunnerHandlerWithClockForTest(a, runnerPolicy, clock.now))
	server.TLS, _ = hc.TLS()
	server.StartTLS()
	defer server.Close()
	clientCert := filepath.Join(dir, "client.pem")
	os.WriteFile(clientCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600)
	keyPath := filepath.Join(dir, "key.pem")
	key, _ := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600)
	tokenPath := filepath.Join(dir, "token")
	os.WriteFile(tokenPath, []byte(token), 0600)
	executable, _ := os.Executable()
	ref := func(path string) customerrunner.Reference {
		return customerrunner.Reference{Command: executable, Arguments: []string{"-test.run=^TestRunnerProvider$", "--", "--runner-fixture", path}}
	}
	root := filepath.Join(dir, "runs")
	os.Mkdir(root, 0700)
	c := customerrunner.Config{Schema: "readmit-runner/v1", Hub: server.URL, Project: "alpha", Environment: "lab", Root: root, CA: hc.ClientCA, Certificate: clientCert, Key: ref(keyPath), Token: ref(tokenPath), UpdateEngine: "v-next", UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
	configPath := filepath.Join(dir, "runner.json")
	raw, _ = json.Marshal(c)
	os.WriteFile(configPath, raw, 0600)
	if _, err := customerrunner.ReadConfig(configPath); err != nil {
		t.Fatal(err)
	}
	if _, err := customerrunner.Enroll(context.Background(), c); err == nil {
		t.Fatal("restart cooldown not enforced")
	}
	clock.advance(runnerHold)
	// A wrong approved-environment binding refuses before any local job exists.
	wrong := c
	wrong.Environment = "other"
	if _, err := customerrunner.Enroll(context.Background(), wrong); err == nil {
		t.Fatal("wrong environment enrolled")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan struct{})
	crashReceived := make(chan struct{})
	outageReceived := make(chan struct{})
	go func() {
		for i := 0; i < 4; i++ {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(15 * time.Second))
			reader, _ := mllp.NewReader(conn, 1<<20)
			if _, e := reader.ReadFrame(); e != nil {
				conn.Close()
				return
			}
			if i == 0 {
				fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
				conn.Close()
			} else {
				if i == 1 {
					close(received)
				} else if i == 2 {
					close(crashReceived)
				} else {
					close(outageReceived)
				}
				io.Copy(io.Discard, conn)
				conn.Close()
			}
		}
	}()
	spec := runnerSpec(t, dir, listener.Addr().String())
	job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "first", Spec: spec}
	prepared, e := durablerun.Prepare(spec)
	if e != nil {
		t.Fatal(e)
	}
	identity, e := prepared.InputIdentity()
	if e != nil {
		t.Fatal(e)
	}
	// A changed schedule pin refuses before the local fixture sees a frame.
	if _, e := customerrunner.RunPinned(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, job, strings.Repeat("0", 64)); e == nil {
		t.Fatal("changed pin executed")
	}
	now := time.Now().UTC()
	due := now.Truncate(time.Minute)
	schedules := hub.SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: "serial-skip-missed", Schedules: []hub.Schedule{{ID: "scheduled", Zone: "UTC", At: due.Format("15:04"), WindowSeconds: 3600, Runner: configPath, Spec: spec, Input: identity}}}
	journal := filepath.Join(dir, "schedule-journal")
	if e := hub.InitializeSchedules(journal, schedules, due.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	scheduler, e := hub.OpenScheduler(journal, schedules, hub.ExecuteScheduledRun, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = scheduler.Tick(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), now); e != nil {
		t.Fatal(e)
	}
	history := scheduler.History()
	scheduler.Close()
	if len(history.Records) != 1 || history.Records[0].State != "passed" {
		t.Fatalf("scheduled headless verdict %+v", history)
	}
	job.ID = history.Records[0].ID
	if _, e := customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, job); e == nil {
		t.Fatal("completed ID replayed")
	}
	grants.Runners[0].MaxJobs = 1
	raw, _ = json.Marshal(grants)
	os.WriteFile(runnerPolicy, raw, 0600)
	next := job
	next.ID = "quota-refused"
	if _, e := customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, next); e == nil {
		t.Fatal("retained job quota ignored")
	}
	grants.Runners[0].MaxJobs = 2
	raw, _ = json.Marshal(grants)
	os.WriteFile(runnerPolicy, raw, 0600)
	job.ID = "second"
	result := make(chan durablerun.Summary, 1)
	errs := make(chan error, 1)
	go func() {
		summary, e := customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, job)
		result <- summary
		errs <- e
	}()
	select {
	case <-received:
	case e := <-errs:
		t.Fatalf("runner failed before send: %v", e)
	case <-time.After(10 * time.Second):
		t.Fatal("runner did not send")
	}
	health, e := customerrunner.Health(root)
	if e != nil || health.State != "lease_current" {
		t.Fatalf("health %+v %v", health, e)
	}
	if _, e := customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, customerrunner.Job{Schema: job.Schema, ID: "concurrent", Spec: spec}); e == nil {
		t.Fatal("concurrent root accepted")
	}
	if _, e := customerrunner.Enroll(context.Background(), c); e == nil {
		t.Fatal("second runner leased environment")
	}
	// Atomic policy replacement revokes the next renewal; active delivery is uncertain.
	enrolledTokens := policy.Tokens
	policy.Tokens = []hub.ScopedToken{}
	writePolicy(t, accessPath, policy)
	select {
	case summary := <-result:
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
		if summary.State != durablerun.DeliveryUncertain || summary.StopReason != durablerun.Cancelled {
			t.Fatalf("revocation %+v", summary)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("revoked runner failed to stop")
	}
	recovered, e := durablerun.Recover(filepath.Join(root, "second", "run"))
	if e != nil || !recovered.Run.DeliveryUncertain || recovered.SafeToRepeat {
		t.Fatalf("recovery %+v %v", recovered, e)
	}
	if _, e := customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, job); e == nil {
		t.Fatal("revoked/repeated job accepted")
	}
	// Killing a real runner process leaves its durable claim and never replays it.
	policy.Tokens = enrolledTokens
	writePolicy(t, accessPath, policy)
	grants.Runners[0].MaxJobs = 4
	raw, _ = json.Marshal(grants)
	os.WriteFile(runnerPolicy, raw, 0600)
	time.Sleep(10 * time.Second)
	job.ID = "crashed"
	jobPath := filepath.Join(dir, "crash-job.json")
	raw, _ = json.Marshal(job)
	os.WriteFile(jobPath, raw, 0600)
	child := exec.Command(executable, "-test.run=^TestRunnerProcess$", "--", "--runner-process", configPath, jobPath)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Process.Kill() })
	select {
	case <-crashReceived:
	case <-time.After(12 * time.Second):
		child.Process.Kill()
		child.Wait()
		t.Fatal("child runner did not send")
	}
	child.Process.Kill()
	child.Wait()
	state, e := customerrunner.Health(root)
	if e != nil || state.State == "idle" {
		t.Fatal("crash claim disappeared", state, e)
	}
	retained, e := durablerun.Recover(filepath.Join(root, "crashed", "run"))
	if e != nil || retained.SafeToRepeat || !retained.Run.DeliveryUncertain {
		t.Fatalf("crash recovery %+v %v", retained, e)
	}
	if _, e := customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, job); e == nil {
		t.Fatal("crash auto replay")
	}
	// Simulate the documented operator action only after the child is confirmed dead.
	os.Remove(filepath.Join(root, ".active", "lease.json"))
	os.Remove(filepath.Join(root, ".active", "lease.next"))
	os.Remove(filepath.Join(root, ".active"))
	time.Sleep(10 * time.Second)
	job.ID = "outage"
	go func() {
		summary, e := customerrunner.Run(customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t))), c, job)
		result <- summary
		errs <- e
	}()
	select {
	case <-outageReceived:
	case e := <-errs:
		t.Fatal("outage run refused", e)
	case <-time.After(12 * time.Second):
		t.Fatal("outage run did not send")
	}
	server.Close()
	select {
	case summary := <-result:
		if e := <-errs; e != nil || summary.State != durablerun.DeliveryUncertain {
			t.Fatalf("outage %+v %v", summary, e)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("hub outage did not stop execution")
	}
	restarted := httptest.NewUnstartedServer(store.RunnerHandler(a, runnerPolicy))
	restarted.TLS, _ = hc.TLS()
	restarted.StartTLS()
	defer restarted.Close()
	fresh := c
	fresh.Hub = restarted.URL
	if _, e := customerrunner.Enroll(context.Background(), fresh); e == nil {
		t.Fatal("hub restart bypassed predecessor lease cooldown")
	}
	// Recovery and status remain readable without either policy or credentials.
	os.Remove(accessPath)
	os.Remove(tokenPath)
	if _, e := customerrunner.Health(root); e != nil {
		t.Fatal(e)
	}
}
func runnerSpec(t *testing.T, dir, address string) string {
	t.Helper()
	raw, err := os.ReadFile("../testdata/fixtures/listen-s12.hl7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = bundle.Write(filepath.Join(dir, "case"), []bundle.Input{{Data: raw, Options: hl7.Options{Format: hl7.Raw}}}, bundle.Provenance{Mode: bundle.Generated, Generator: &bundle.GeneratorInputs{BaseTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GeneratorVersion: "fixture", ProfileVersion: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	target := replay.Target{Schema: replay.TargetSchemaV3, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "1s", MessageTimeout: "30s", MaxACKBytes: 4096, Name: "lab", Classification: replay.Nonproduction}
	val := "AA"
	spec := testrunner.Spec{Schema: testrunner.SpecSchema, Name: "ACK", Input: testrunner.Input{Case: "case", Messages: []string{"s0001-e000001"}}, Target: "target.json", Setup: testrunner.Setup{InitialState: "operator-declared", ResetInstructions: "reset fixture"}, Observation: testrunner.Observation{Boundary: testrunner.ACKBoundary}, Assertions: []testrunner.Assertion{{ID: "accepted", Operator: "ack_field_equals", Message: "s0001-e000001", Selector: "MSA-1", Expected: testrunner.Value{Field: &testrunner.FieldValue{State: hl7.Present, Text: &val}}}}}
	for name, v := range map[string]any{"target.json": target, "spec.json": spec} {
		b, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "spec.json")
}
