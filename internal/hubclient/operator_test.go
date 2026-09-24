package hubclient_test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/hubclient"
)

// operatorDocument is a complete readmit-hub-operator-client/v1 document with
// one member replaced or removed, for the strictness cases below.
func operatorDocument(members map[string]string) string {
	document := map[string]string{
		"schema":      `"readmit-hub-operator-client/v1"`,
		"hub":         `"https://hub.hospital.org:8443"`,
		"ca":          `"/etc/readmit/ca.pem"`,
		"certificate": `"/etc/readmit/operator.pem"`,
		"key":         `{"command":"/usr/bin/security","arguments":["find-generic-password","-w"]}`,
	}
	for name, value := range members {
		if value == "" {
			delete(document, name)
			continue
		}
		document[name] = value
	}
	parts := make([]string, 0, len(document))
	for _, name := range []string{"schema", "hub", "ca", "certificate", "key", "idp", "projects"} {
		if value, ok := document[name]; ok {
			parts = append(parts, fmt.Sprintf("%q:%s", name, value))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// An operator-only hub's configuration is read strictly: exactly its five
// members, the key reference exactly its command and arguments, and the same
// endpoint and client-identity rules a team configuration is held to. A team
// hub's configuration is not one, and neither is anything carrying a member
// an operator-only hub does not have.
func TestOperatorConfigurationIsReadStrictly(t *testing.T) {
	config, err := hubclient.DecodeOperatorConfig([]byte(operatorDocument(nil)))
	if err != nil {
		t.Fatalf("a complete configuration: %v", err)
	}
	if config.Hub != "https://hub.hospital.org:8443" || config.CA != "/etc/readmit/ca.pem" || config.Certificate != "/etc/readmit/operator.pem" ||
		config.Key.Command != "/usr/bin/security" || strings.Join(config.Key.Arguments, " ") != "find-generic-password -w" {
		t.Fatalf("decoded %+v", config)
	}
	team := `{"schema":"readmit-hub-client/v1","hub":"https://hub.hospital.org:8443","ca":"/etc/readmit/ca.pem","certificate":"/etc/readmit/operator.pem",` +
		`"key":{"command":"/usr/bin/security","arguments":[]},"idp":{"issuer":"https://idp.hospital.org","client_id":"c","audience":"a","authorize_endpoint":"https://idp.hospital.org/a","token_endpoint":"https://idp.hospital.org/t","scopes":["evidence.read"]},"projects":["alpha"]}`
	if _, err := hubclient.DecodeOperatorConfig([]byte(team)); !errors.Is(err, hubclient.ErrUnsupportedOperatorVersion) {
		t.Fatalf("a team hub's configuration read as operator-only: %v", err)
	}
	for name, document := range map[string]string{
		"no schema":                operatorDocument(map[string]string{"schema": ""}),
		"null schema":              operatorDocument(map[string]string{"schema": "null"}),
		"a later version":          operatorDocument(map[string]string{"schema": `"readmit-hub-operator-client/v2"`}),
		"no hub":                   operatorDocument(map[string]string{"hub": ""}),
		"null certificate":         operatorDocument(map[string]string{"certificate": "null"}),
		"no key":                   operatorDocument(map[string]string{"key": ""}),
		"an identity provider":     operatorDocument(map[string]string{"idp": `{}`}),
		"projects":                 operatorDocument(map[string]string{"projects": `["alpha"]`}),
		"a key with a value":       operatorDocument(map[string]string{"key": `{"command":"/usr/bin/security","arguments":[],"value":"x"}`}),
		"a key without arguments":  operatorDocument(map[string]string{"key": `{"command":"/usr/bin/security"}`}),
		"null key arguments":       operatorDocument(map[string]string{"key": `{"command":"/usr/bin/security","arguments":null}`}),
		"a key command on PATH":    operatorDocument(map[string]string{"key": `{"command":"security","arguments":[]}`}),
		"a cleartext hub":          operatorDocument(map[string]string{"hub": `"http://hub.hospital.org:8443"`}),
		"a hub with a path":        operatorDocument(map[string]string{"hub": `"https://hub.hospital.org/v1"`}),
		"a hub with a user":        operatorDocument(map[string]string{"hub": `"https://operator@hub.hospital.org"`}),
		"a relative CA":            operatorDocument(map[string]string{"ca": `"ca.pem"`}),
		"an uncleaned certificate": operatorDocument(map[string]string{"certificate": `"/etc/readmit/../operator.pem"`}),
		"a duplicate member":       strings.Replace(operatorDocument(nil), `"hub":`, `"hub":"https://other.hospital.org","hub":`, 1),
		"not an object":            `["readmit-hub-operator-client/v1"]`,
		"oversized":                operatorDocument(map[string]string{"ca": `"/` + strings.Repeat("a", hubclient.MaxConfigBytes) + `"`}),
	} {
		if _, err := hubclient.DecodeOperatorConfig([]byte(document)); err == nil {
			t.Errorf("%s: read as an operator-only configuration", name)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "hub-operator.json")
	if err := os.WriteFile(path, []byte(operatorDocument(nil)), 0o600); err != nil {
		t.Fatal(err)
	}
	if read, err := hubclient.ReadOperatorConfig(path); err != nil || read.Hub != config.Hub {
		t.Fatalf("reading the file: %+v %v", read, err)
	}
	for name, path := range map[string]string{"a folder": dir, "a missing file": filepath.Join(dir, "absent.json")} {
		if _, err := hubclient.ReadOperatorConfig(path); !errors.Is(err, hubclient.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// operatorHub is a mutual-TLS stub of an operator-only hub's artifact store
// that counts what reaches it and answers each route as the test sets it.
type operatorHub struct {
	server   *httptest.Server
	config   hubclient.OperatorConfig
	requests atomic.Int64
	mu       sync.Mutex
	stored   map[string][]byte
	// status, when set, answers every artifact request with that status.
	status atomic.Int32
	// served, when set, is what a read answers in place of the stored bytes.
	served atomic.Pointer[[]byte]
	// hangUp ends the connection without an answer.
	hangUp atomic.Bool
	// oversized answers a read with one byte more than an artifact may hold.
	oversized atomic.Bool
	last      atomic.Pointer[http.Request]
}

func newOperatorHub(t *testing.T) *operatorHub {
	t.Helper()
	ca := newTestAuthority(t, "operator-hub-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, "hub-operator", false)
	serverPair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.pem)
	hub := &operatorHub{stored: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		hub.requests.Add(1)
		hub.last.Store(r)
		body, _ := io.ReadAll(r.Body)
		if hub.hangUp.Load() {
			conn, _, err := http.NewResponseController(w).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		if status := hub.status.Load(); status != 0 {
			w.WriteHeader(int(status))
			return
		}
		digest := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
		hub.mu.Lock()
		defer hub.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			sum := sha256.Sum256(body)
			if hex.EncodeToString(sum[:]) != digest {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			hub.stored[digest] = body
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			data, ok := hub.stored[digest]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if served := hub.served.Load(); served != nil {
				data = *served
			}
			if hub.oversized.Load() {
				data = make([]byte, hubclient.MaxArtifactBytes+1)
			}
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	hub.server = httptest.NewUnstartedServer(mux)
	hub.server.TLS = &tls.Config{Certificates: []tls.Certificate{serverPair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	hub.server.StartTLS()
	t.Cleanup(hub.server.Close)

	dir := t.TempDir()
	caPath, certPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "operator.pem")
	if os.WriteFile(caPath, ca.pem, 0o600) != nil || os.WriteFile(certPath, clientCert, 0o600) != nil {
		t.Fatal("writing the client identity")
	}
	hub.config = hubclient.OperatorConfig{Schema: hubclient.OperatorSchema, Hub: hub.server.URL, CA: caPath, Certificate: certPath,
		Key: hubclient.KeyReference{Command: createKeyProvider(t, clientKey), Arguments: []string{}}}
	return hub
}

// The operator-only client stores bytes under their digest and reads back
// exactly those bytes, over the hub's /v1/artifacts store with the client
// certificate alone: no bearer token and no project. Each answer the hub can
// give is reported as what it means for that store, a read whose bytes do not
// hash to their digest returns nothing, and an address that is not a whole
// digest reaches nothing.
func TestOperatorClientStoresAndReadsByDigestAndReportsEachAnswer(t *testing.T) {
	ctx := context.Background()
	hub := newOperatorHub(t)
	client, err := hubclient.NewOperator(ctx, hub.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckHealth(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
	payload := []byte("MSH|^~\\&|SYNTHETIC\rPID|1||TEST-312\r\x00\xff")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	stored, err := client.StoreArtifact(ctx, payload)
	if err != nil || stored != digest {
		t.Fatalf("store: %q %v", stored, err)
	}
	request := hub.last.Load()
	if request.Method != http.MethodPut || request.URL.Path != "/v1/artifacts/"+digest || request.Header.Get("Authorization") != "" ||
		request.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("the store reached %s %s with %v", request.Method, request.URL.Path, request.Header)
	}
	if again, err := client.StoreArtifact(ctx, payload); err != nil || again != digest {
		t.Fatalf("storing the same bytes again: %q %v", again, err)
	}
	read, err := client.ReadArtifact(ctx, digest)
	if err != nil || string(read) != string(payload) {
		t.Fatalf("read: %q %v", read, err)
	}
	if request := hub.last.Load(); request.Method != http.MethodGet || request.URL.Path != "/v1/artifacts/"+digest || request.Header.Get("Authorization") != "" {
		t.Fatalf("the read reached %s %s", request.Method, request.URL.Path)
	}

	// Bytes that do not hash to the digest asked for are never returned.
	substituted := []byte("synthetic bytes under another digest")
	hub.served.Store(&substituted)
	if read, err := client.ReadArtifact(ctx, digest); !errors.Is(err, hubclient.ErrIntegrity) || read != nil {
		t.Fatalf("a substituted answer: %q %v", read, err)
	}
	hub.served.Store(nil)

	asked := hub.requests.Load()
	for _, address := range []string{"", "abc", strings.ToUpper(digest), digest + "0", "../" + digest[3:]} {
		if _, err := client.ReadArtifact(ctx, address); !errors.Is(err, hubclient.ErrInvalidDigest) {
			t.Errorf("read of %q: %v", address, err)
		}
	}
	if got := hub.requests.Load(); got != asked {
		t.Fatalf("addresses that are not a digest reached the hub %d times", got-asked)
	}

	for status, want := range map[int][2]error{
		http.StatusForbidden:             {hubclient.ErrOperatorAccessRefused, hubclient.ErrOperatorStoreRefused},
		http.StatusNotFound:              {hubclient.ErrArtifactAbsent, hubclient.ErrNoOperatorStore},
		http.StatusServiceUnavailable:    {hubclient.ErrArtifactUnavailable, hubclient.ErrStoreUnavailable},
		http.StatusUnprocessableEntity:   {nil, hubclient.ErrStoreMismatch},
		http.StatusRequestEntityTooLarge: {nil, hubclient.ErrOverCapacity},
	} {
		hub.status.Store(int32(status))
		if want[0] != nil {
			if _, err := client.ReadArtifact(ctx, digest); !errors.Is(err, want[0]) {
				t.Errorf("a read answered %d: %v", status, err)
			}
		}
		if _, err := client.StoreArtifact(ctx, payload); !errors.Is(err, want[1]) {
			t.Errorf("a store answered %d: %v", status, err)
		}
	}
	hub.status.Store(http.StatusTeapot)
	if _, err := client.StoreArtifact(ctx, payload); err == nil || !strings.Contains(err.Error(), "418") {
		t.Errorf("an unexpected answer: %v", err)
	}
	hub.status.Store(0)

	// An answer larger than an artifact may be is not kept, and bytes over
	// the bound are never sent.
	hub.oversized.Store(true)
	if read, err := client.ReadArtifact(ctx, digest); err == nil || read != nil || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("an oversized answer: %d bytes, %v", len(read), err)
	}
	hub.oversized.Store(false)
	asked = hub.requests.Load()
	if _, err := client.StoreArtifact(ctx, make([]byte, hubclient.MaxArtifactBytes+1)); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Errorf("an oversized store: %v", err)
	}
	if got := hub.requests.Load(); got != asked {
		t.Fatal("an oversized store reached the hub")
	}

	// A hub that hangs up before answering leaves a store uncertain and a
	// read unanswered; one that cannot be reached received nothing.
	hub.hangUp.Store(true)
	if _, err := client.StoreArtifact(ctx, payload); !errors.Is(err, hubclient.ErrStoreUncertain) {
		t.Errorf("a store the hub never answered: %v", err)
	}
	if _, err := client.ReadArtifact(ctx, digest); !errors.Is(err, hubclient.ErrReadUnanswered) {
		t.Errorf("a read the hub never answered: %v", err)
	}
	hub.hangUp.Store(false)
	hub.server.Close()
	if _, err := client.StoreArtifact(ctx, payload); !errors.Is(err, hubclient.ErrHubUnreachable) {
		t.Errorf("a store to a stopped hub: %v", err)
	}
	if _, err := client.ReadArtifact(ctx, digest); !errors.Is(err, hubclient.ErrHubUnreachable) {
		t.Errorf("a read from a stopped hub: %v", err)
	}
}

// A hub whose certificate the configured CA does not verify ends the
// handshake before anything is sent, so a store to it is not uncertain.
func TestOperatorClientSendsNothingToAHubWhoseCertificateDoesNotVerify(t *testing.T) {
	ctx := context.Background()
	hub := newOperatorHub(t)
	other := newTestAuthority(t, "another-hub-ca")
	caPath := filepath.Join(t.TempDir(), "other-ca.pem")
	if err := os.WriteFile(caPath, other.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	config := hub.config
	config.CA = caPath
	client, err := hubclient.NewOperator(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.StoreArtifact(ctx, []byte("synthetic")); !errors.Is(err, hubclient.ErrHubUnreachable) {
		t.Fatalf("a store to a hub whose certificate does not verify: %v", err)
	}
	if _, err := client.ReadArtifact(ctx, strings.Repeat("a", 64)); !errors.Is(err, hubclient.ErrHubUnreachable) {
		t.Fatalf("a read from a hub whose certificate does not verify: %v", err)
	}
	if hub.requests.Load() != 0 {
		t.Fatal("a hub whose certificate does not verify was asked")
	}
}

// Creating the client resolves the configured identity the way a team client
// does, and refuses a configuration that does not validate before reading any
// file or running the key's program.
func TestOperatorClientRefusesAnInvalidConfigurationAndAMissingIdentity(t *testing.T) {
	ctx := context.Background()
	hub := newOperatorHub(t)
	invalid := hub.config
	invalid.Schema = hubclient.Schema
	if _, err := hubclient.NewOperator(ctx, invalid); !errors.Is(err, hubclient.ErrUnsupportedOperatorVersion) {
		t.Fatalf("a configuration of the wrong contract: %v", err)
	}
	missing := hub.config
	missing.Certificate = filepath.Join(t.TempDir(), "absent.pem")
	if _, err := hubclient.NewOperator(ctx, missing); err == nil || !strings.Contains(err.Error(), "cannot read client certificate") {
		t.Fatalf("a missing client certificate: %v", err)
	}
	if hub.requests.Load() != 0 {
		t.Fatal("a refused configuration reached the hub")
	}
}
