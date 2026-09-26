package desktop_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/testlicense"
)

// runnerGateFixture is an authenticated window over a mutual-TLS hub stub
// that serves the project administration log (#262's lifecycle surface) and a
// runner admission endpoint the window's OIDC session could never satisfy —
// the hub admits runners through certificate-bound rh_ tokens, not session
// tokens — so a test can see exactly whether the administration gate let a
// runner lifecycle action through to the hub.
type runnerGateFixture struct {
	app           *desktop.App
	serverURL     string
	caPath        string
	runnerCert    string
	runnerKey     string
	runnerToken   string
	admissions    func() int64
	setRemoved    func(subject string)
	setUnreadable func()
	// observe, once set, runs as each request reaches the hub, before the hub
	// answers it.
	observe atomic.Pointer[func()]
}

func newRunnerGateFixture(t *testing.T, subject string, scopes []string) *runnerGateFixture {
	t.Helper()
	ca := newHubTestAuthority(t, "customer-hub-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	runnerCertPEM, runnerKeyPEM := ca.issue(t, "runner-host", false)

	fixture := &runnerGateFixture{}
	var admissions atomic.Int64
	var removedSubject atomic.Value
	var unreadable atomic.Bool
	removedSubject.Store("")
	fixture.admissions = func() int64 { return admissions.Load() }
	fixture.setRemoved = func(who string) { removedSubject.Store(who) }
	fixture.setUnreadable = func() { unreadable.Store(true) }

	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /v1/projects/cardio-study/runner", func(w http.ResponseWriter, r *http.Request) {
		admissions.Add(1)
		http.Error(w, "access refused", http.StatusForbidden)
	})
	mux.HandleFunc("GET /v2/projects/cardio-study/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		if unreadable.Load() {
			http.Error(w, "metadata unavailable", http.StatusServiceUnavailable)
			return
		}
		history := hubprotocol.LifecycleHistory{Schema: hubprotocol.LifecycleHistorySchema, Events: []hubprotocol.LifecycleEvent{}, Tips: map[string][]string{}, Warning: hubprotocol.CustodyWarning}
		if removed, ok := removedSubject.Load().(string); ok && removed != "" {
			history.Events = append(history.Events, hubprotocol.LifecycleEvent{
				Schema: hubprotocol.LifecycleEventSchema, Project: "cardio-study", Sequence: 1,
				Issuer: "https://idp.hospital.org", Actor: "an-admin", At: "2026-09-01T00:00:00Z",
				Command: hubprotocol.LifecycleCommand{Schema: hubprotocol.LifecycleCommandSchema, ID: "remove-1",
					Kind: "remove-user", Parents: []string{}, Subject: removed, Reason: "offboarding"},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, history)
	})

	serverPair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(ca.pem)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if observe := fixture.observe.Load(); observe != nil {
			(*observe)()
		}
		mux.ServeHTTP(w, r)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	fixture.serverURL = server.URL

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	accessToken := issueDesktopTestToken(t, rsaKey, "key-1", "https://idp.hospital.org", subject, "hub-aud", "desktop-app",
		scopes, now.Unix(), now.Unix()+1800)
	idpMux := http.NewServeMux()
	idpMux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{"access_token": accessToken, "token_type": "Bearer", "expires_in": 1800})
	})
	idpServer := httptest.NewServer(idpMux)
	t.Cleanup(idpServer.Close)

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	if err := os.WriteFile(caPath, ca.pem, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, runnerCertPEM, 0600); err != nil {
		t.Fatal(err)
	}
	keyProvider := createDesktopKeyProvider(t, runnerKeyPEM)
	scopesJSON, _ := json.Marshal(scopes)
	cfgPath := filepath.Join(dir, "hub-client.json")
	cfgJSON := fmt.Sprintf(`{
		"schema": "readmit-hub-client/v1",
		"hub": %q,
		"ca": %q,
		"certificate": %q,
		"key": {"command": %q, "arguments": []},
		"idp": {
			"issuer": "https://idp.hospital.org",
			"client_id": "desktop-app",
			"audience": "hub-aud",
			"authorize_endpoint": "https://idp.hospital.org/oauth/authorize",
			"token_endpoint": %q,
			"scopes": %s
		},
		"projects": ["cardio-study"]
	}`, server.URL, caPath, certPath, keyProvider, idpServer.URL+"/oauth/token", scopesJSON)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	app := desktop.New(&chooser{folder: dir}, desktop.ShellDocuments{Folder: dir})
	if res := app.SelectOperationPolicy(testlicense.New(t)); res.State != desktop.Completed {
		t.Fatalf("SelectOperationPolicy: %+v", res)
	}
	if res := app.SelectHubConfig(cfgPath); res.State != desktop.Completed {
		t.Fatalf("SelectHubConfig: %+v", res)
	}
	if res := app.ConnectHub(); res.State != desktop.Completed || !res.Connected {
		t.Fatalf("ConnectHub: %+v", res)
	}
	auth := app.StartHubAuth()
	if auth.State != desktop.Completed || auth.AuthURL == "" {
		t.Fatalf("StartHubAuth: %+v", auth)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		var stateVal string
		for _, part := range strings.Split(auth.AuthURL, "&") {
			if strings.HasPrefix(part, "state=") {
				stateVal = strings.TrimPrefix(part, "state=")
			}
		}
		_, _ = http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?code=auth-code-runner&state=%s", auth.Port, stateVal))
	}()
	if res := app.CompleteHubAuth("", ""); res.State != desktop.Completed || !res.Authenticated {
		t.Fatalf("CompleteHubAuth: %+v", res)
	}

	tokenPath := filepath.Join(dir, "runner-token")
	if err := os.WriteFile(tokenPath, []byte("rh_runner-gate-token"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture.app = app
	fixture.caPath = caPath
	fixture.runnerCert = certPath
	fixture.runnerKey = keyProvider
	fixture.runnerToken = tokenPath
	return fixture
}

// gateRunnerConfig writes the runner configuration the gate tests execute
// against: real credential references, the gate hub's URL, the administered
// project, and a private root on this machine.
func (f *runnerGateFixture) gateRunnerConfig(t *testing.T) string {
	t.Helper()
	request := runnerConfigInput(t, mustPrivateRoot(t))
	request.Hub = f.serverURL
	request.Project = "cardio-study"
	request.CA = f.caPath
	request.Certificate = f.runnerCert
	request.Key = desktop.RunnerReferenceInput{Command: f.runnerKey, Arguments: []string{}}
	request.Token = desktop.RunnerReferenceInput{Command: "/bin/cat", Arguments: []string{f.runnerToken}}
	request.Output = filepath.Join(t.TempDir(), "runner.json")
	if saved := f.app.SaveRunnerConfig(request); saved.State != desktop.Completed {
		t.Fatalf("config: %+v", saved)
	}
	return request.Output
}

func mustPrivateRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestRunnerGatePassesAdmissibleWorkThroughToTheHub proves the gate is not a
// renderer substitution: an admissible principal's enrollment reaches the hub
// admission endpoint, and the hub's own refusal — not the gate's — is what
// the panel displays.
func TestRunnerGatePassesAdmissibleWorkThroughToTheHub(t *testing.T) {
	fixture := newRunnerGateFixture(t, "scheduling-lead", []string{"evidence.read", "enrollment", "execution"})
	configPath := fixture.gateRunnerConfig(t)
	result := fixture.app.EnrollRunner(configPath)
	if result.State != desktop.PermissionDenied || !strings.Contains(result.Reason, "access refused") {
		t.Fatalf("admissible work must reach the hub: %+v", result)
	}
	if fixture.admissions() != 1 {
		t.Fatalf("admissions seen: %d", fixture.admissions())
	}
}

// TestRunnerGateRefusesARemovedPrincipal covers the removed-user denial path:
// once the administration log records the signed-in principal's removal, both
// runner lifecycle operations refuse in the window and no admission is asked.
func TestRunnerGateRefusesARemovedPrincipal(t *testing.T) {
	fixture := newRunnerGateFixture(t, "scheduling-lead", []string{"evidence.read", "enrollment", "execution"})
	configPath := fixture.gateRunnerConfig(t)
	fixture.setRemoved("scheduling-lead")
	enrollment := fixture.app.EnrollRunner(configPath)
	if enrollment.State != desktop.PermissionDenied || !strings.Contains(enrollment.Reason, "removed from this project's administration log") {
		t.Fatalf("removed principal enrollment: %+v", enrollment)
	}
	jobPath := filepath.Join(t.TempDir(), "job.json")
	if saved := fixture.app.SaveRunnerJob(desktop.RunnerJobRequest{ID: "nightly-001", Spec: "/srv/readmit/spec.json", Output: jobPath}); saved.State != desktop.Completed {
		t.Fatalf("job: %+v", saved)
	}
	execution := fixture.app.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: configPath, JobPath: jobPath})
	if execution.State != desktop.PermissionDenied || !strings.Contains(execution.Reason, "removed from this project's administration log") {
		t.Fatalf("removed principal execution: %+v", execution)
	}
	if fixture.admissions() != 0 {
		t.Fatalf("a removed principal's work reached the hub: %d", fixture.admissions())
	}
}

// TestRunnerGateRefusesAGrantedScopeTheActionNeeds covers the role denial
// path: the access policy's roles decide which identities hold enrollment and
// execution, and the window session's granted scopes are the authority the
// gate consults — never a renderer assumption.
func TestRunnerGateRefusesAGrantedScopeTheActionNeeds(t *testing.T) {
	fixture := newRunnerGateFixture(t, "reviewer-only", []string{"evidence.read"})
	configPath := fixture.gateRunnerConfig(t)
	enrollment := fixture.app.EnrollRunner(configPath)
	if enrollment.State != desktop.PermissionDenied || !strings.Contains(enrollment.Reason, "do not include enrollment") {
		t.Fatalf("scope-denied enrollment: %+v", enrollment)
	}
	jobPath := filepath.Join(t.TempDir(), "job.json")
	if saved := fixture.app.SaveRunnerJob(desktop.RunnerJobRequest{ID: "nightly-001", Spec: "/srv/readmit/spec.json", Output: jobPath}); saved.State != desktop.Completed {
		t.Fatalf("job: %+v", saved)
	}
	execution := fixture.app.ExecuteRunnerJob(desktop.RunnerExecuteRequest{ConfigPath: configPath, JobPath: jobPath})
	if execution.State != desktop.PermissionDenied || !strings.Contains(execution.Reason, "do not include execution") {
		t.Fatalf("scope-denied execution: %+v", execution)
	}
	if fixture.admissions() != 0 {
		t.Fatalf("scope-denied work reached the hub: %d", fixture.admissions())
	}
}

// TestRunnerGateFailsClosedWhenTheAdministrationLogIsUnreadable covers the
// unknown-authority path: a session that cannot establish the project's
// administration state refuses new runner work instead of assuming a pass.
func TestRunnerGateFailsClosedWhenTheAdministrationLogIsUnreadable(t *testing.T) {
	fixture := newRunnerGateFixture(t, "scheduling-lead", []string{"evidence.read", "enrollment", "execution"})
	configPath := fixture.gateRunnerConfig(t)
	fixture.setUnreadable()
	result := fixture.app.EnrollRunner(configPath)
	if result.State != desktop.PermissionDenied || !strings.Contains(result.Reason, "administration log could not be read") {
		t.Fatalf("unreadable administration log: %+v", result)
	}
	if fixture.admissions() != 0 {
		t.Fatalf("work passed an unreadable authority: %d", fixture.admissions())
	}
}
