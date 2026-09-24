package hubclient_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hubclient"
)

// memory is a Memory the test reads back: what it recalls, and every path it
// was asked to remember.
type memory struct {
	recall     string
	unreadable bool
	refuse     bool
	kept       []string
}

func (m *memory) Recall() (string, error) {
	if m.unreadable {
		return "", errors.New("unreadable")
	}
	return m.recall, nil
}

func (m *memory) Remember(path string) error {
	if m.refuse {
		return errors.New("full disk")
	}
	m.kept = append(m.kept, path)
	return nil
}

// connectionHub is a hub over mutual TLS answering its health probes, an IdP
// token endpoint issuing whatever token the test sets, and a configuration
// file naming both. It counts every request that reaches the hub.
type connectionHub struct {
	config   string
	key      *rsa.PrivateKey
	token    atomic.Value
	requests atomic.Int64
}

func newConnectionHub(t *testing.T) *connectionHub {
	t.Helper()
	hub := &connectionHub{}
	ca := newTestAuthority(t, "connection-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, "ana", false)
	pair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.pem)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("code") == "refused" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{"access_token": hub.token.Load(), "token_type": "Bearer", "expires_in": 60})
	}))
	t.Cleanup(idp.Close)
	hub.key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	hub.issue(t, time.Now().Unix()+1800)

	dir := t.TempDir()
	caPath, certPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "client.pem")
	if os.WriteFile(caPath, ca.pem, 0o600) != nil || os.WriteFile(certPath, clientCert, 0o600) != nil {
		t.Fatal("cannot write the client identity")
	}
	cfg := hubclient.Config{
		Schema: hubclient.Schema, Hub: server.URL, CA: caPath, Certificate: certPath,
		Key: hubclient.KeyReference{Command: createKeyProvider(t, clientKey), Arguments: []string{}},
		IdP: hubclient.IdPConfig{Issuer: "https://idp.example.com", ClientID: "desktop", Audience: "hub",
			AuthorizeEndpoint: "https://idp.example.com/authorize", TokenEndpoint: idp.URL + "/token", Scopes: []string{"evidence.read"}},
		Projects: []string{"alpha"},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	hub.config = filepath.Join(dir, "hub-client.json")
	if err := os.WriteFile(hub.config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return hub
}

// issue makes the IdP answer the next code with a token for ana expiring at exp.
func (h *connectionHub) issue(t *testing.T, exp int64) {
	t.Helper()
	h.token.Store(issueTestToken(t, h.key, "key-1", "https://idp.example.com", "ana", "hub", "desktop", []string{"evidence.read"}, exp-1800, exp))
}

// returnToListener is the browser arriving at the sign-in listener.
func returnToListener(port int, query string) {
	if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?%s", port, query)); err == nil {
		resp.Body.Close()
	}
}

func listenerClosed(t *testing.T, port int) {
	t.Helper()
	if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second); err == nil {
		conn.Close()
		t.Errorf("the sign-in listener on port %d is still open", port)
	}
}

func stateOf(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

// A restored connection reads what memory keeps and the configuration it
// names and nothing else: it is selected and offline, and a selection that
// cannot be restored is kept with why (#363).
func TestARestoredConnectionIsSelectedAndOffline(t *testing.T) {
	hub := newConnectionHub(t)
	invalid := filepath.Join(t.TempDir(), "hub-client.json")
	if err := os.WriteFile(invalid, []byte(`{"schema":"readmit-hub-client/v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		memory   memory
		path     string
		refusal  string
		selected bool
	}{
		"nothing remembered":                       {memory{}, "", "", false},
		"an unreadable memory":                     {memory{unreadable: true}, "", "cannot be read", false},
		"a configuration no longer there":          {memory{recall: filepath.Join(t.TempDir(), "gone.json")}, "gone.json", "no longer there", false},
		"a configuration that no longer validates": {memory{recall: invalid}, invalid, "no longer validates", false},
		"a configuration that validates":           {memory{recall: hub.config}, hub.config, "", true},
	} {
		t.Run(name, func(t *testing.T) {
			status := hubclient.RestoreConnection(&c.memory).Status()
			if (status.Config != nil) != c.selected || !strings.HasSuffix(status.ConfigPath, c.path) || (c.path == "" && status.ConfigPath != "") ||
				(c.refusal == "") != (status.Refusal == "") || !strings.Contains(status.Refusal, c.refusal) || status.Client != nil || status.Session != nil {
				t.Fatalf("restored %+v", status)
			}
		})
	}
	if sent := hub.requests.Load(); sent != 0 {
		t.Fatalf("restoring reached the hub %d times", sent)
	}
	refused := hubclient.RestoreConnection(&memory{unreadable: true})
	if _, err := refused.Select(hub.config); err != nil {
		t.Fatal(err)
	}
	if status := refused.Status(); status.Refusal != "" || status.Config == nil || status.ConfigPath != hub.config {
		t.Fatalf("selecting again kept the restore refusal: %+v", status)
	}
}

// A selection is remembered before it is made: one memory cannot keep is
// refused and changes nothing (#384).
func TestASelectionIsRememberedBeforeItIsMade(t *testing.T) {
	hub := newConnectionHub(t)
	kept := &memory{}
	connection := hubclient.NewConnection(kept)
	if _, err := connection.Select("relative/hub-client.json"); err == nil || !strings.Contains(err.Error(), "cleaned absolute") {
		t.Fatalf("a relative path: %v", err)
	}
	if _, err := connection.Select(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("a missing configuration selected")
	}
	if len(kept.kept) != 0 {
		t.Fatalf("a refused selection was remembered: %v", kept.kept)
	}
	cfg, err := connection.Select(hub.config)
	if err != nil || cfg.Projects[0] != "alpha" || len(kept.kept) != 1 || kept.kept[0] != hub.config {
		t.Fatalf("select: %+v %v, remembered %v", cfg, err, kept.kept)
	}
	kept.refuse = true
	other := filepath.Join(t.TempDir(), "other.json")
	data, _ := os.ReadFile(hub.config)
	if err := os.WriteFile(other, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Select(other); !errors.Is(err, hubclient.ErrNotRemembered) {
		t.Fatalf("a selection memory could not keep: %v", err)
	}
	if status := connection.Status(); status.ConfigPath != hub.config {
		t.Fatalf("a refused selection changed the selection: %+v", status)
	}
	if _, err := hubclient.NewConnection(nil).Select(hub.config); err != nil {
		t.Fatalf("a connection that remembers nothing: %v", err)
	}
}

// However a sign-in ends, its listener is closed and nothing of it is left to
// complete (#332); signing in keeps the connection and renews its client, and
// selecting again, disconnecting or expiring ends the session.
func TestSigningInAndOutFollowsTheConnectionsSessionRules(t *testing.T) {
	ctx := context.Background()
	hub := newConnectionHub(t)
	connection := hubclient.NewConnection(nil)
	if _, _, err := connection.SignedIn(); !errors.Is(err, hubclient.ErrNotConnected) {
		t.Fatalf("nothing connected: %v", err)
	}
	if err := connection.Connect(ctx); !errors.Is(err, hubclient.ErrNotSelected) {
		t.Fatalf("connecting with nothing selected: %v", err)
	}
	if _, _, err := connection.StartSignIn(); !errors.Is(err, hubclient.ErrNotSelected) {
		t.Fatalf("signing in with nothing selected: %v", err)
	}
	if err := connection.CompleteSignIn(ctx, "", "", time.Second); !errors.Is(err, hubclient.ErrNotSelected) {
		t.Fatalf("completing with nothing selected: %v", err)
	}
	if _, err := connection.Select(hub.config); err != nil {
		t.Fatal(err)
	}
	if err := connection.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	connected := connection.Status().Client
	if connected == nil {
		t.Fatal("connecting kept no client")
	}
	if _, _, err := connection.SignedIn(); !errors.Is(err, hubclient.ErrSignInRequired) {
		t.Fatalf("connected, not signed in: %v", err)
	}
	if err := connection.CompleteSignIn(ctx, "", "", time.Second); !errors.Is(err, hubclient.ErrNoSignIn) {
		t.Fatalf("completing with no sign-in pending: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, c := range map[string]struct {
		want   error
		finish func(authURL string, port int) error
	}{
		"a forged state": {hubclient.ErrStateMismatch, func(string, int) error {
			return connection.CompleteSignIn(ctx, "typed", "forged", time.Second)
		}},
		"a browser that never returns": {hubclient.ErrSignInTimedOut, func(string, int) error {
			return connection.CompleteSignIn(ctx, "", "", 50*time.Millisecond)
		}},
		"an IdP refusing the sign-in": {nil, func(authURL string, port int) error {
			go returnToListener(port, "error=access_denied&state="+stateOf(t, authURL))
			return connection.CompleteSignIn(ctx, "", "", 5*time.Second)
		}},
		"an IdP refusing the code": {nil, func(authURL string, port int) error {
			go returnToListener(port, "code=refused&state="+stateOf(t, authURL))
			return connection.CompleteSignIn(ctx, "", "", 5*time.Second)
		}},
		"a cancelled wait": {context.Canceled, func(string, int) error {
			return connection.CompleteSignIn(cancelled, "", "", 5*time.Second)
		}},
		"an ended sign-in": {hubclient.ErrNoSignIn, func(string, int) error {
			connection.EndSignIn()
			return connection.CompleteSignIn(ctx, "", "", time.Second)
		}},
		"a sign-in started again": {hubclient.ErrNoSignIn, func(_ string, port int) error {
			_, again, err := connection.StartSignIn()
			if err != nil || again == port {
				return fmt.Errorf("a new sign-in reused the listener: %d %v", again, err)
			}
			listenerClosed(t, port)
			connection.EndSignIn()
			return connection.CompleteSignIn(ctx, "", "", time.Second)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			authURL, port, err := connection.StartSignIn()
			if err != nil || port <= 0 {
				t.Fatalf("start: %d %v", port, err)
			}
			err = c.finish(authURL, port)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("the sign-in ended with %v, want %v", err, c.want)
			}
			listenerClosed(t, port)
			if again := connection.CompleteSignIn(ctx, "", "", 50*time.Millisecond); !errors.Is(again, hubclient.ErrNoSignIn) {
				t.Fatalf("the ended attempt completed again: %v", again)
			}
			if status := connection.Status(); status.Client != connected || status.Session != nil {
				t.Fatalf("a failed sign-in changed the connection: %+v", status)
			}
		})
	}

	authURL, port, err := connection.StartSignIn()
	if err != nil {
		t.Fatal(err)
	}
	go returnToListener(port, "code=accepted&state="+stateOf(t, authURL))
	if err := connection.CompleteSignIn(ctx, "", "", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	listenerClosed(t, port)
	client, session, err := connection.SignedIn()
	if err != nil || session.Subject != "ana" || client == connected {
		t.Fatalf("signed in: %v %v, renewed client %v", session, err, client != connected)
	}

	// Disconnecting ends a pending sign-in and the session, and keeps the selection.
	_, pending, _ := connection.StartSignIn()
	connection.Disconnect()
	listenerClosed(t, pending)
	if status := connection.Status(); status.Client != nil || status.Session != nil || status.Config == nil {
		t.Fatalf("disconnected: %+v", status)
	}
	if _, _, err := connection.SignedIn(); !errors.Is(err, hubclient.ErrNotConnected) {
		t.Fatalf("disconnected: %v", err)
	}

	// Selecting again ends the session that belonged to the earlier selection.
	if err := connection.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	authURL, port, _ = connection.StartSignIn()
	go returnToListener(port, "code=accepted&state="+stateOf(t, authURL))
	if err := connection.CompleteSignIn(ctx, "", "", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Select(hub.config); err != nil {
		t.Fatal(err)
	}
	if status := connection.Status(); status.Client != nil || status.Session != nil {
		t.Fatalf("a new selection kept the old session: %+v", status)
	}

	// A session that expires is kept for the status to show, and refused.
	if err := connection.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	hub.issue(t, time.Now().Unix()+2)
	authURL, port, _ = connection.StartSignIn()
	go returnToListener(port, "code=accepted&state="+stateOf(t, authURL))
	if err := connection.CompleteSignIn(ctx, "", "", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, _, err := connection.SignedIn(); err == nil; _, _, err = connection.SignedIn() {
		if time.Now().After(deadline) {
			t.Fatal("the session never expired")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, _, err := connection.SignedIn(); !errors.Is(err, hubclient.ErrSignInRequired) {
		t.Fatalf("an expired session: %v", err)
	}
	if status := connection.Status(); status.Session == nil || !status.Session.IsExpired(time.Now()) || status.Client == nil {
		t.Fatalf("an expired session is not shown: %+v", status)
	}
}
