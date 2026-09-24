package hub_test

// An operator reads and stores artifacts on a real operator-only hub from the
// application (#312): the hub's own handler served over mutual TLS with
// PostgreSQL metadata, and the window driving the same typed facade its
// operator-only mode calls. Nothing is stubbed: the hub's operation policy
// admits or refuses the store for the client certificate, and a counter in
// front of the hub records what the window sent, so a refusal the window
// reached itself is shown to have sent nothing and one the hub gave to have
// been asked once. What the window stores is what the hub's own reader
// returns, and what the window reads is what the hub stored.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/testlicense"
)

// operatorDialogs answers the host's file and save dialogs with what the test
// sets before each act, as the person's choice.
type operatorDialogs struct {
	files       []string
	destination string
}

func (d *operatorDialogs) ChooseFolder(string) (string, error) { return "", nil }
func (d *operatorDialogs) ChooseFiles(string, string, string) ([]string, error) {
	return d.files, nil
}
func (d *operatorDialogs) ChooseDestination(string) (string, error) { return d.destination, nil }

func TestPostgresOperatorOnlyHubReadsAndStoresThroughTheApplication(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	cert := certificates(t, &c)
	store := open(t, c)
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certificateDigest := fmt.Sprintf("%x", sha256.Sum256(cert.Certificate[0]))
	// The hub's operator binds authors in its operation policy: first to
	// another certificate only, then to the one the window presents.
	policy := func(certificates ...string) {
		t.Helper()
		operation := hub.OperationPolicy{Schema: "readmit-hub-operation-policy/v1", Policy: testlicense.New(t), Bindings: []hub.OperationBinding{}}
		for _, bound := range certificates {
			operation.Bindings = append(operation.Bindings, hub.OperationBinding{Issuer: "mutual-tls", Subject: bound, Certificate: bound, Author: "test-author", Device: "test-device"})
		}
		path := filepath.Join(t.TempDir(), "hub-operation.json")
		if raw, err := json.Marshal(operation); err != nil || os.WriteFile(path, raw, 0o600) != nil {
			t.Fatal("hub operation policy", err)
		}
		if err := store.SetOperationPolicy(path); err != nil {
			t.Fatal(err)
		}
	}
	policy(fmt.Sprintf("%x", sha256.Sum256([]byte("another operator's certificate"))))

	// The operator serves the hub operator-only; the served handler can be
	// swapped to its team service, as the operator restarts it.
	var served atomic.Value
	served.Store(store.Handler())
	var sent atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/live" && r.URL.Path != "/health/ready" {
			sent.Add(1)
		}
		served.Load().(http.Handler).ServeHTTP(w, r)
	}))
	server.TLS, _ = c.TLS()
	server.StartTLS()
	defer server.Close()
	asked := func(want int64) {
		t.Helper()
		if got := sent.Load(); got != want {
			t.Fatalf("the hub was asked %d times, want %d", got, want)
		}
	}

	clientPEM, keyPEM := filepath.Join(dir, "operator.pem"), filepath.Join(dir, "operator-key.pem")
	privateKey, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		clientPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}),
		keyPEM:    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(dir, "hub-operator.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"schema":"readmit-hub-operator-client/v1","hub":%q,"ca":%q,"certificate":%q,"key":{"command":"/bin/cat","arguments":[%q]}}`,
		server.URL, c.ClientCA, clientPEM, keyPEM), 0o600); err != nil {
		t.Fatal(err)
	}

	// Each window is a separate installation: the operator's, with a license
	// activated, and one without.
	window := func(licensed bool) (*desktop.App, *operatorDialogs) {
		t.Helper()
		dialogs := &operatorDialogs{files: []string{config}}
		state := t.TempDir()
		app := desktop.New(dialogs, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
			filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
		if licensed {
			if result := app.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
				t.Fatal(result)
			}
		}
		if result := app.ChooseOperatorHubConfig(); result.State != desktop.Completed {
			t.Fatalf("choose: %+v", result)
		}
		if result := app.ConnectOperatorHub(); result.State != desktop.Completed || !result.Connected {
			t.Fatalf("connect: %+v", result)
		}
		return app, dialogs
	}
	operator, dialogs := window(true)
	unlicensed, unlicensedDialogs := window(false)
	asked(0)

	evidence := []byte("MSH|^~\\&|SYNTHETIC\rPID|1||TEST-312\r\x00\xff")
	sum := sha256.Sum256(evidence)
	digest := hex.EncodeToString(sum[:])
	source := filepath.Join(t.TempDir(), "evidence.bin")
	if err := os.WriteFile(source, evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	dialogs.files, unlicensedDialogs.files = []string{source}, []string{source}
	absentOnHub := func() {
		t.Helper()
		if _, err := store.Get(ctx, digest); !errors.Is(err, hub.ErrMissing) {
			t.Fatalf("the hub holds the refused store: %v", err)
		}
	}

	// A window with no license is refused by its own admission; the hub's
	// operation policy refuses a certificate it binds to no author.
	if result := unlicensed.StoreOperatorHubArtifact(); result.State != desktop.PermissionDenied {
		t.Fatalf("an unlicensed store: %+v", result)
	}
	asked(0)
	if result := operator.StoreOperatorHubArtifact(); result.State != desktop.PermissionDenied || result.Reason != hubclient.ErrOperatorStoreRefused.Error() {
		t.Fatalf("a store the hub's operation policy does not admit: %+v", result)
	}
	asked(1)
	absentOnHub()

	// Bound, the store is the hub's own: its reader returns exactly the
	// file's bytes, and storing them again is safe.
	policy(certificateDigest)
	for range 2 {
		if result := operator.StoreOperatorHubArtifact(); result.State != desktop.Completed || result.Digest != digest || result.Size != int64(len(evidence)) {
			t.Fatalf("the admitted store: %+v", result)
		}
	}
	asked(3)
	if kept, err := store.Get(ctx, digest); err != nil || !bytes.Equal(kept, evidence) {
		t.Fatalf("the hub's own reader returns %q, %v", kept, err)
	}

	// Reading is not authoring: both windows read the stored bytes back into
	// a new file, with the custody notice.
	for name, reader := range map[string]struct {
		app     *desktop.App
		dialogs *operatorDialogs
	}{"operator": {operator, dialogs}, "unlicensed": {unlicensed, unlicensedDialogs}} {
		reader.dialogs.destination = filepath.Join(t.TempDir(), "received.bin")
		result := reader.app.ReadOperatorHubArtifact(digest)
		if result.State != desktop.Completed || result.Digest != digest || result.Warning != "Downloaded copies remain under local custody and cannot be revoked." {
			t.Fatalf("the %s window's read: %+v", name, result)
		}
		if got, err := os.ReadFile(reader.dialogs.destination); err != nil || !bytes.Equal(got, evidence) {
			t.Fatalf("the %s window wrote %q, %v", name, got, err)
		}
	}
	asked(5)
	nothingWritten := func(path string) {
		t.Helper()
		for _, left := range []string{path, path + ".incomplete"} {
			if _, err := os.Lstat(left); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a refused read left %s behind", filepath.Base(left))
			}
		}
	}
	read := func(address string) desktop.HubTransferResult {
		t.Helper()
		dialogs.destination = filepath.Join(t.TempDir(), "read.bin")
		result := operator.ReadOperatorHubArtifact(address)
		if result.State != desktop.Completed {
			nothingWritten(dialogs.destination)
		}
		return result
	}

	// A digest the hub does not hold, and a stored copy damaged on the hub's
	// disk, which the hub refuses to serve and storing again cannot replace.
	if result := read(hex.EncodeToString(make([]byte, 32))); result.State != desktop.Failed || result.Reason != hubclient.ErrArtifactAbsent.Error() {
		t.Fatalf("a digest the hub does not hold: %+v", result)
	}
	if err := os.WriteFile(filepath.Join(c.Root, digest), []byte("damaged on the hub's disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := read(digest); result.State != desktop.Failed || result.Reason != hubclient.ErrArtifactUnavailable.Error() {
		t.Fatalf("a damaged stored copy: %+v", result)
	}
	if result := operator.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.Reason != hubclient.ErrStoreMismatch.Error() {
		t.Fatalf("storing over a damaged copy: %+v", result)
	}
	asked(8)

	// Restarted as a team hub, the service offers no operator-only store;
	// once it has served team mode, serving it operator-only again keeps the
	// store closed. Neither stores anything.
	access, _, key, _ := accessFixture(t)
	team := store.TeamHandler(access)
	served.Store(team)
	other := []byte("synthetic evidence stored while the hub serves team mode")
	if err := os.WriteFile(source, other, 0o600); err != nil {
		t.Fatal(err)
	}
	otherSum := sha256.Sum256(other)
	otherDigest := hex.EncodeToString(otherSum[:])
	if result := operator.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.Reason != hubclient.ErrNoOperatorStore.Error() {
		t.Fatalf("a store to a team hub: %+v", result)
	}
	if result := read(digest); result.State != desktop.Failed || result.Reason != hubclient.ErrArtifactAbsent.Error() {
		t.Fatalf("a read from a team hub: %+v", result)
	}
	marking := request(signed(t, key, claims("viewer"), accessHeader))
	marking.URL.Path = "/v1/projects/alpha/artifacts/" + digest
	team.ServeHTTP(httptest.NewRecorder(), marking)
	served.Store(store.Handler())
	if result := operator.StoreOperatorHubArtifact(); result.State != desktop.PermissionDenied || result.Reason != hubclient.ErrOperatorStoreRefused.Error() {
		t.Fatalf("a store to an operator-only hub that has served team mode: %+v", result)
	}
	if result := read(digest); result.State != desktop.PermissionDenied || result.Reason != hubclient.ErrOperatorAccessRefused.Error() {
		t.Fatalf("a read from an operator-only hub that has served team mode: %+v", result)
	}
	asked(12)
	if _, err := store.Get(ctx, otherDigest); !errors.Is(err, hub.ErrMissing) {
		t.Fatalf("a refused store reached the hub's store: %v", err)
	}

	// A stopped hub received nothing; a disconnected window sends nothing.
	server.Close()
	if result := read(digest); result.State != desktop.Failed || result.Reason != hubclient.ErrHubUnreachable.Error() {
		t.Fatalf("a read from a stopped hub: %+v", result)
	}
	if result := operator.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.Reason != hubclient.ErrHubUnreachable.Error() {
		t.Fatalf("a store to a stopped hub: %+v", result)
	}
	if result := operator.DisconnectOperatorHub(); result.State != desktop.Completed || result.Connected || result.CustodyWarning == "" {
		t.Fatalf("disconnect: %+v", result)
	}
	if result := read(digest); result.State != desktop.Failed || result.Reason != "not connected to the operator-only hub" {
		t.Fatalf("a read after disconnecting: %+v", result)
	}
	asked(12)
}
