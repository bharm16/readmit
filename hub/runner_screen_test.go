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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/testlicense"
)

// silentChooser stands in for the host dialog; the runner screen needs none.
type silentChooser struct{}

func (silentChooser) ChooseFolder(string) (string, error) { return "", nil }
func (silentChooser) ChooseFiles(string, string, string) ([]string, error) {
	return nil, nil
}

// screenApp is the window under test: an activated application shell whose
// local state files are fresh.
func screenApp(t *testing.T) *desktop.App {
	t.Helper()
	state := t.TempDir()
	app := desktop.New(silentChooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	if result := app.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
		t.Fatal(result)
	}
	return app
}

// asInput converts a reference the runner host reads into the structured form
// the window's configuration form holds: the same absolute program and
// arguments, never a value.
func asInput(reference customerrunner.Reference) desktop.RunnerReferenceInput {
	return desktop.RunnerReferenceInput{Command: reference.Command, Arguments: reference.Arguments}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestRunnerScreenPreparesConfiguresAndExecutesThroughExistingContracts drives
// the application-facing runner workflow over a real hub admission: the window
// generates every document through its structured forms, enrolls, executes one
// pinned job through the unchanged runner path, refuses duplicates and
// stale pins, reads recovery offline, and a job the window prepared executes
// with an equivalent verdict through the unchanged CLI.
func TestRunnerScreenPreparesConfiguresAndExecutesThroughExistingContracts(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	cliBinary := journeyExecutable(t, "READMIT_ACCEPTANCE_BINARY", "..", "readmit")
	cert := certificates(t, &c)
	a, policy, adminKey, accessPath := accessFixture(t)
	token := "rh_" + base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("r", 32)))
	policy.Tokens = []hub.ScopedToken{{Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(token))), Subject: "runner", Project: "alpha", Actions: []string{"enrollment", "execution"}, Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Certificate: fmt.Sprintf("%x", sha256.Sum256(cert.Certificate[0])), Kind: "runner"}}
	writePolicy(t, accessPath, policy)
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	screen := screenApp(t)

	// The hub's private runner policy is generated, not hand-authored: the
	// window writes the grant for this project and environment, and the hub
	// reads the generated file through its own strict reader.
	grantPath := filepath.Join(dir, "runners.generated.json")
	if result := screen.SaveRunnerGrant(desktop.RunnerGrantRequest{
		Project: "alpha", Subject: "runner", Environment: "lab",
		Engine: engine.Version(), MaxSeconds: 30, MaxJobs: 2, Output: grantPath,
	}); result.State != desktop.Completed {
		t.Fatalf("grant: %+v", result)
	}
	if _, err := runnerprotocol.DecodePolicy(mustRead(t, grantPath)); err != nil {
		t.Fatalf("generated grant is not a valid policy document: %v", err)
	}

	// The fixture receiver answers every connection with a passing ACK and
	// counts every connection after the first, so the test can prove the
	// completed job was never attempted again before the CLI's own run.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	attempts := make(chan struct{}, 4)
	go func() {
		first := true
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(15 * time.Second))
			if !first {
				attempts <- struct{}{}
			}
			first = false
			reader, _ := mllp.NewReader(conn, 1<<20)
			if _, err := reader.ReadFrame(); err == nil {
				fmt.Fprint(conn, "\x0bMSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r\x1c\r")
			}
			conn.Close()
		}
	}()
	spec := runnerSpec(t, dir, listener.Addr().String())

	// Credential references stay references: the provider program is the test
	// binary's own fixture responder, exactly as the CLI documentation's
	// customer-secret-reader stands in for a real store.
	executable, _ := os.Executable()
	provider := func(name, value string) customerrunner.Reference {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		return customerrunner.Reference{Command: executable, Arguments: []string{"-test.run=^TestRunnerProvider$", "--", "--runner-fixture", path}}
	}
	clientCert := filepath.Join(dir, "client.pem")
	if err := os.WriteFile(clientCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "key.pem")
	key, _ := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "runs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}

	store := open(t, c)
	if e := store.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	// The store's administration writes admit authors through an installed
	// operation policy; this fixture binds the administrator identity whose
	// certificate the removal command below presents.
	authorPolicyPath := filepath.Join(dir, "hub-operation-policy.json")
	authorPolicy := hub.OperationPolicy{Schema: "readmit-hub-operation-policy/v1", Policy: testlicense.New(t), Bindings: []hub.OperationBinding{{
		Issuer: "https://idp.example", Subject: "owner",
		Certificate: fmt.Sprintf("%x", sha256.Sum256(cert.Certificate[0])), Author: "test-author", Device: "test-device",
	}}}
	authorPolicyRaw, err := json.Marshal(authorPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authorPolicyPath, authorPolicyRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetOperationPolicy(authorPolicyPath); err != nil {
		t.Fatal(err)
	}
	clock := newHubClock()
	server := httptest.NewUnstartedServer(store.RunnerHandlerWithClockForTest(a, grantPath, clock.now))
	server.TLS, _ = c.TLS()
	server.StartTLS()
	defer server.Close()

	// The window generates the configuration from its structured form and the
	// hub-side grant from its own; the runner host's own reader accepts both.
	configPath := filepath.Join(dir, "runner.json")
	if result := screen.SaveRunnerConfig(desktop.RunnerConfigRequest{
		Hub: server.URL, Project: "alpha", Environment: "lab",
		Root: root, CA: c.ClientCA, Certificate: clientCert,
		Key: asInput(provider("key-fixture", string(mustRead(t, keyPath)))), Token: asInput(provider("token-fixture", token)),
		UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "v-next",
		Output: configPath,
	}); result.State != desktop.Completed {
		t.Fatalf("config: %+v", result)
	}
	if _, err := customerrunner.ReadConfig(configPath); err != nil {
		t.Fatalf("the runner host refuses the generated configuration: %v", err)
	}
	if inspected := screen.ReadRunnerConfig(configPath); inspected.State != desktop.Completed || inspected.Config == nil || inspected.Health == nil || inspected.Health.State != "idle" {
		t.Fatalf("inspect before enrollment: %+v", inspected)
	}

	// The window prepares the job document and the pin, and refuses a changed
	// spec before admission is even asked.
	jobPath := filepath.Join(dir, "screen-job.json")
	if result := screen.SaveRunnerJob(desktop.RunnerJobRequest{ID: "screen-001", Spec: spec, Output: jobPath}); result.State != desktop.Completed {
		t.Fatalf("job: %+v", result)
	}
	preview := screen.InspectRunnerJob(configPath, jobPath)
	if preview.State != desktop.Completed || preview.InputIdentity == "" || preview.Environment != "lab" {
		t.Fatalf("preview: %+v", preview)
	}
	originalSpec := mustRead(t, spec)
	if err := os.WriteFile(spec, append(originalSpec, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if after := screen.InspectRunnerJob(configPath, jobPath); after.State != desktop.Completed || after.InputIdentity == preview.InputIdentity {
		t.Fatal("a changed spec kept the same prepared identity")
	}
	if err := os.WriteFile(spec, originalSpec, 0600); err != nil {
		t.Fatal(err)
	}

	// Enrollment is refused while the fresh handler waits out a predecessor's
	// grants; once the cooldown ends the pinned job is admitted. Each execution
	// below releases its lease when it completes, so no step waits for one to
	// lapse; that wait is proven once, against the shipped hub, by
	// TestPublicRunnerAndHubProcessesExecuteAndRefuseReuse.
	if result := screen.EnrollRunner(configPath); result.State == desktop.Completed {
		t.Fatal("enrollment bypassed the restart cooldown")
	}
	clock.advance(runnerHold)

	execution := screen.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: configPath, JobPath: jobPath, Expected: preview.InputIdentity})
	if execution.State != desktop.Completed || execution.Summary == nil || execution.Summary.State != "passed" {
		t.Fatalf("execution: %+v", execution)
	}
	select {
	case <-attempts:
		t.Fatal("a completed execution was attempted again")
	default:
	}

	// A retained id is never replayed, whatever the panel asks.
	duplicate := screen.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: configPath, JobPath: jobPath})
	if duplicate.State != desktop.Failed || duplicate.Summary != nil {
		t.Fatalf("duplicate submission: %+v", duplicate)
	}

	// The executed job's id is occupied from now on. The window's preflight
	// names it rather than a pin, by the rule the runner's service skips it
	// by, and the command line's runner refuses the same document over the
	// same configuration, as the window's execution did.
	if retained, err := customerrunner.Retained(root, "screen-001"); err != nil || !retained {
		t.Fatalf("the runner does not hold the executed id: %v %v", retained, err)
	}
	if occupied := screen.InspectRunnerJob(configPath, jobPath); occupied.State != desktop.Failed || occupied.InputIdentity != "" ||
		!strings.Contains(occupied.Reason, "job id screen-001 is already retained in this runner's root") {
		t.Fatalf("occupied preflight: %+v", occupied)
	}
	again := exec.Command(cliBinary, "--operation-policy", testlicense.New(t), "runner", "execute", jobPath, "--config", configPath, "--send")
	if output, err := again.CombinedOutput(); err == nil || string(output) != "readmit: "+customerrunner.ErrRefused.Error()+"\n" {
		t.Fatalf("the command line's runner ran an occupied job id: %v %s", err, output)
	}
	select {
	case <-attempts:
		t.Fatal("an occupied job id was sent again")
	default:
	}

	// The panel reads recovery and the retained state offline, exactly as the
	// CLI does, and it does not resend.
	recovery := screen.ReadRunnerRecovery(configPath, "screen-001")
	if recovery.State != desktop.Completed || recovery.Acknowledged != 1 || recovery.Uncertain != 0 {
		t.Fatalf("recovery: %+v", recovery)
	}
	// A configuration bound to another environment is refused on its face: the
	// prepared inputs bind one environment and the runner is configured for
	// another, before the hub is ever asked.
	otherConfig := filepath.Join(dir, "runner-other.json")
	if result := screen.SaveRunnerConfig(desktop.RunnerConfigRequest{
		Hub: server.URL, Project: "alpha", Environment: "stage",
		Root: root, CA: c.ClientCA, Certificate: clientCert,
		Key: asInput(provider("key-fixture", string(mustRead(t, keyPath)))), Token: asInput(provider("token-fixture", token)),
		UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "v-next",
		Output: otherConfig,
	}); result.State != desktop.Completed {
		t.Fatalf("other config: %+v", result)
	}
	if denied := screen.InspectRunnerJob(otherConfig, jobPath); denied.State != desktop.Failed || !strings.Contains(denied.Reason, "bind environment lab") {
		t.Fatalf("environment denial: %+v", denied)
	}

	// A job the window prepared executes with an equivalent verdict through
	// the unchanged command line: fresh job id, same generated configuration.
	cliJob := filepath.Join(dir, "cli-job.json")
	if result := screen.SaveRunnerJob(desktop.RunnerJobRequest{ID: "cli-001", Spec: spec, Output: cliJob}); result.State != desktop.Completed {
		t.Fatalf("cli job: %+v", result)
	}
	command := exec.Command(cliBinary, "--operation-policy", testlicense.New(t), "runner", "execute", cliJob, "--config", configPath, "--send")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI runner execute: %v %s", err, output)
	}
	if _, err := customerrunner.Health(root); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	retained := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			retained[entry.Name()] = true
		}
	}
	if !retained["screen-001"] || !retained["cli-001"] {
		t.Fatalf("retained jobs: %v", retained)
	}

	// A grant for a different engine is the hub's version refusal, shown with
	// its own reason; restoring the generated grant re-admits.
	revised := filepath.Join(dir, "runners.revised.json")
	if result := screen.SaveRunnerGrant(desktop.RunnerGrantRequest{
		Policy: grantPath, Project: "alpha", Subject: "runner", Environment: "lab",
		Engine: "other-approved-build", MaxSeconds: 30, MaxJobs: 2, Output: revised,
	}); result.State != desktop.Completed {
		t.Fatalf("revised grant: %+v", result)
	}
	if err := os.Rename(revised, grantPath); err != nil {
		t.Fatal(err)
	}
	if mismatch := screen.EnrollRunner(configPath); mismatch.State != desktop.PermissionDenied || !strings.Contains(mismatch.Reason, "version or environment refused") {
		t.Fatalf("version mismatch: %+v", mismatch)
	}
	original := filepath.Join(dir, "runners.original.json")
	if result := screen.SaveRunnerGrant(desktop.RunnerGrantRequest{
		Project: "alpha", Subject: "runner", Environment: "lab",
		Engine: engine.Version(), MaxSeconds: 30, MaxJobs: 2, Output: original,
	}); result.State != desktop.Completed {
		t.Fatalf("restored grant: %+v", result)
	}
	if err := os.Rename(original, grantPath); err != nil {
		t.Fatal(err)
	}
	enrollment := screen.EnrollRunner(configPath)
	if enrollment.State != desktop.Completed || enrollment.MaxSeconds != 30 || enrollment.MaxJobs != 2 || enrollment.ExpiresAt == "" {
		t.Fatalf("enrollment after restore: %+v", enrollment)
	}
	// The probe's own lease holds the environment; a different instance is
	// refused with the hub's reasoned answer while it is current, and claims
	// nothing. This is the duplicate-admission rule the panel displays rather
	// than hides.
	heldJob := filepath.Join(dir, "held-job.json")
	if result := screen.SaveRunnerJob(desktop.RunnerJobRequest{ID: "screen-002", Spec: spec, Output: heldJob}); result.State != desktop.Completed {
		t.Fatalf("held job: %+v", result)
	}
	if blocked := screen.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: configPath, JobPath: heldJob}); blocked.State != desktop.Failed || !strings.Contains(blocked.Reason, "environment leased or recovering") {
		t.Fatalf("probe lease not enforced: %+v", blocked)
	}
	if _, err := os.Stat(filepath.Join(root, "screen-002")); !os.IsNotExist(err) {
		t.Fatal("job refused by a current lease claimed work", err)
	}

	// The project administration authority (#262's lifecycle log) is the
	// enforcement behind every admission: recording a remove-user command for
	// the runner's own subject — through the same server, by an authorized
	// administrator identity — must stop the runner at its next admission,
	// with the hub's own refusal displayed, whatever any client believes.
	adminJWT := signed(t, adminKey, claims("owner"), accessHeader)
	removeCommand := fmt.Sprintf(`{"schema":"readmit-hub-lifecycle-command/v1","id":"remove-runner","expected":0,"kind":"remove-user","resource":"","artifact":"","parents":[],"subject":%q,"until":"","reason":"offboarding the runner identity"}`, "runner")
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(mustRead(t, c.ClientCA)) {
		t.Fatal("hub client CA unreadable")
	}
	adminClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: caPool, MinVersion: tls.VersionTLS13},
	}}
	removal, err := http.NewRequestWithContext(context.Background(), "POST", server.URL+"/v2/projects/alpha/lifecycle", strings.NewReader(removeCommand))
	if err != nil {
		t.Fatal(err)
	}
	removal.Header.Set("Authorization", "Bearer "+adminJWT)
	removal.Header.Set("Content-Type", "application/json")
	removalResponse, err := adminClient.Do(removal)
	if err != nil {
		t.Fatal(err)
	}
	removalBody, _ := io.ReadAll(io.LimitReader(removalResponse.Body, 4096))
	removalResponse.Body.Close()
	if removalResponse.StatusCode != http.StatusCreated {
		t.Fatalf("remove-user command: %d %s", removalResponse.StatusCode, removalBody)
	}
	// The probe's lease may still be current; the hub decides access before
	// any lease, so the refusal is the removal's, not the lease's.
	if denied := screen.EnrollRunner(configPath); denied.State != desktop.PermissionDenied || !strings.Contains(denied.Reason, "access refused") {
		t.Fatalf("removed runner admission: %+v", denied)
	}

	// Disconnection changes nothing about what is retained and offers no
	// resend: enrollment fails, and the panel reads current state offline.
	server.Close()
	if after := screen.EnrollRunner(configPath); after.State != desktop.Failed {
		t.Fatalf("enrollment after disconnect: %+v", after)
	}
	if inspected := screen.ReadRunnerConfig(configPath); inspected.State != desktop.Completed || inspected.Health == nil || inspected.Health.Jobs != 2 {
		t.Fatalf("inspect after disconnect: %+v", inspected)
	}
}

// The schedule policy the window reopens names the identity the hub binds its
// journal to: over the revision the window saved, the hub's own reader and
// one-time initialization record the identity the window shows, and a policy
// a later release wrote is refused by both.
func TestTheWindowReopensAScheduleRevisionWithTheIdentityTheHubJournals(t *testing.T) {
	screen := screenApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")
	entry := desktop.ScheduleEntryInput{
		ID: "nightly", Zone: "America/Chicago", At: "02:30", WindowSeconds: 600,
		Runner: filepath.Join(dir, "runner.json"), Spec: filepath.Join(dir, "spec.json"),
		Input: strings.Repeat("b", 64), Route: "https://alerts.example/", Approved: true,
	}
	saved := screen.SaveSchedulePolicy(desktop.SchedulePolicyRequest{Output: path, Entries: []desktop.ScheduleEntryInput{entry}})
	if saved.State != desktop.Completed {
		t.Fatalf("save: %+v", saved)
	}
	raw := mustRead(t, path)
	policy, err := hub.DecodeSchedules(raw)
	if err != nil {
		t.Fatalf("the hub refuses the window's revision: %v", err)
	}
	journal := filepath.Join(dir, "journal")
	if err := hub.InitializeSchedules(journal, policy, time.Now()); err != nil {
		t.Fatal(err)
	}
	var history struct {
		Policy string `json:"policy_sha256"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(journal, "history.json")), &history); err != nil || len(history.Policy) != 64 {
		t.Fatalf("the hub's journal: %+v %v", history, err)
	}
	opened := screen.OpenSchedulePolicy(path)
	if opened.State != desktop.Completed || opened.Identity != history.Policy || opened.Identity != saved.Identity ||
		len(opened.Entries) != 1 || opened.Entries[0].Entry != entry {
		t.Fatalf("the window reopened the revision as %+v; the hub journals %s", opened, history.Policy)
	}

	later := filepath.Join(dir, "schedules-v2.json")
	if err := os.WriteFile(later, bytes.Replace(raw, []byte("readmit-hub-schedules/v1"), []byte("readmit-hub-schedules/v2"), 1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.DecodeSchedules(mustRead(t, later)); err == nil {
		t.Fatal("the hub read a policy a later release wrote")
	}
	if refused := screen.OpenSchedulePolicy(later); refused.State != desktop.Failed || refused.Identity != "" ||
		refused.Reason != "the schedule policy could not be read through its own strict reader" {
		t.Fatalf("the window reopened a policy a later release wrote: %+v", refused)
	}
}
