package desktop_test

// The hub panel's operator-only mode (#312): an operator-only hub's artifact
// store read and stored through the facade, over the same hubclient transport
// the team mode uses, against a mutual-TLS stub of the store that records what
// reached it. A refusal the window reaches itself is shown to have sent
// nothing and opened no dialog it did not need; a refusal the hub gives is
// shown to have been asked once and never retried. The hub's own handler for
// the same routes is driven through this facade in the hub module
// (TestPostgresOperatorOnlyHubReadsAndStoresThroughTheApplication).

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hubclient"
)

// operatorStub is an operator-only hub's artifact store over mutual TLS: the
// two health probes and GET and PUT /v1/artifacts/{digest}, keeping one
// object per digest as the hub does.
type operatorStub struct {
	server   *httptest.Server
	requests *hubRequests
	config   string
	mu       sync.Mutex
	stored   map[string][]byte
	// status, when set, answers every artifact request with that status.
	status atomic.Int32
	// served, when set, is what a read answers in place of the stored bytes.
	served atomic.Pointer[[]byte]
	// hangUp ends each artifact request's connection without an answer.
	hangUp atomic.Bool
	// witness reads the privacy status as each artifact request arrives.
	witness *witness
}

func newOperatorStub(t *testing.T) *operatorStub {
	t.Helper()
	stub := &operatorStub{requests: &hubRequests{}, stored: map[string][]byte{}, witness: &witness{}}
	mux := http.NewServeMux()
	healthy(mux)
	mux.HandleFunc("/v1/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		stub.witness.observe()
		body := stub.requests.record(r)
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "an operator-only hub has no sign-in", http.StatusBadRequest)
			return
		}
		if stub.hangUp.Load() {
			if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
				conn.Close()
			}
			return
		}
		if status := stub.status.Load(); status != 0 {
			w.WriteHeader(int(status))
			return
		}
		digest := path.Base(r.URL.Path)
		stub.mu.Lock()
		defer stub.mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			sum := sha256.Sum256(body)
			if hex.EncodeToString(sum[:]) != digest {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			stub.stored[digest] = body
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			data, ok := stub.stored[digest]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if served := stub.served.Load(); served != nil {
				data = *served
			}
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	ca := newHubTestAuthority(t, "operator-hub-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, "hub-operator", false)
	serverPair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.pem)
	stub.server = httptest.NewUnstartedServer(mux)
	stub.server.TLS = &tls.Config{Certificates: []tls.Certificate{serverPair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	stub.server.StartTLS()
	t.Cleanup(stub.server.Close)

	dir := t.TempDir()
	caPath, certPath, keyPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "operator.pem"), filepath.Join(dir, "operator-key.pem")
	for file, data := range map[string][]byte{caPath: ca.pem, certPath: clientCert, keyPath: clientKey} {
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stub.config = filepath.Join(dir, "hub-operator.json")
	writeDocument(t, dir, "hub-operator.json", fmt.Sprintf(`{"schema":"readmit-hub-operator-client/v1","hub":%q,"ca":%q,"certificate":%q,"key":{"command":"/bin/cat","arguments":[%q]}}`,
		stub.server.URL, caPath, certPath, keyPath))
	return stub
}

func (s *operatorStub) sent(t *testing.T, want int) {
	t.Helper()
	if got := s.requests.count(); got != want {
		t.Fatalf("the hub received %d artifact requests, want %d", got, want)
	}
}

// connectOperator chooses the stub's configuration in the file dialog and
// connects, as the panel's operator-only mode does.
func connectOperator(t *testing.T, app *desktop.App, dialog *chooser, stub *operatorStub) {
	t.Helper()
	dialog.files = []string{stub.config}
	if result := app.ChooseOperatorHubConfig(); result.State != desktop.Completed {
		t.Fatalf("choose: %+v", result)
	}
	if result := app.ConnectOperatorHub(); result.State != desktop.Completed || !result.Connected {
		t.Fatalf("connect: %+v", result)
	}
}

// hubDisclosureDetail is what the privacy status's hub row says now.
func hubDisclosureDetail(app *desktop.App) string {
	for _, state := range app.DisclosureStatus().States {
		if state.ID == "hub" {
			return state.Detail
		}
	}
	return ""
}

// absent holds that a refused read left nothing behind.
func absent(t *testing.T, destination string) {
	t.Helper()
	for _, left := range []string{destination, destination + ".incomplete"} {
		if _, err := os.Lstat(left); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("a refused read left %s behind", filepath.Base(left))
		}
	}
}

// Choosing an operator-only hub's configuration reads that one file and
// reaches no hub. A dismissed dialog, a team hub's configuration and a choice
// of several files are each refused and keep the selection; the selection is
// this window's alone, so the next window starts with none, and the team
// mode's selection is untouched.
func TestChoosingAnOperatorOnlyHubConfigurationReachesNoHubAndIsNotRemembered(t *testing.T) {
	stub := newOperatorStub(t)
	team := writeHubClientConfig(t, t.TempDir(), stub.server.URL)
	dialog := &chooser{}
	state := t.TempDir()
	window := func() *desktop.App {
		return desktop.New(dialog, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
			filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	}
	app := window()

	if result := app.ConnectOperatorHub(); result.State != desktop.Failed || result.Reason != "no operator-only hub configuration selected" {
		t.Fatalf("connecting before choosing: %+v", result)
	}
	dialog.files = nil
	if result := app.ChooseOperatorHubConfig(); result.State != desktop.Cancelled {
		t.Fatalf("a dismissed dialog: %+v", result)
	}
	dialog.files = []string{team}
	if result := app.ChooseOperatorHubConfig(); result.State != desktop.Failed || result.Reason != "not an operator-only hub configuration this release reads" {
		t.Fatalf("a team hub's configuration: %+v", result)
	}
	dialog.files = []string{stub.config, team}
	if result := app.ChooseOperatorHubConfig(); result.State != desktop.Failed || result.Reason != "choose one operator-only hub configuration file" {
		t.Fatalf("two files: %+v", result)
	}
	if result := app.ConnectOperatorHub(); result.State != desktop.Failed || result.Reason != "no operator-only hub configuration selected" {
		t.Fatalf("refused choices selected something: %+v", result)
	}
	dialog.files = []string{stub.config}
	if result := app.ChooseOperatorHubConfig(); result.State != desktop.Completed || result.ConfigPath != stub.config || result.HubURL != stub.server.URL || result.Connected {
		t.Fatalf("choosing the operator's configuration: %+v", result)
	}
	for i, title := range dialog.titles {
		if title != "Choose the operator-only hub configuration" || dialog.opened[i] != "files" {
			t.Fatalf("the choice opened the %s dialog %q", dialog.opened[i], title)
		}
	}
	if status := app.HubStatus(); status.State != desktop.Empty || status.ConfigPath != "" {
		t.Fatalf("the team mode's selection changed: %+v", status)
	}
	if hub := hubDisclosure(t, app); hub != "offline" {
		t.Fatalf("a chosen, unconnected operator-only hub reads %s", hub)
	}
	stub.sent(t, 0)

	next := window()
	if result := next.ConnectOperatorHub(); result.State != desktop.Failed || result.Reason != "no operator-only hub configuration selected" {
		t.Fatalf("the next window restored an operator-only selection: %+v", result)
	}
	if entries := entriesOf(t, state); len(entries) != 0 {
		t.Fatalf("choosing an operator-only hub wrote %v into the window's state", entries)
	}
}

// Connected with the client certificate alone, the window stores the exact
// bytes of the chosen file under their digest and reads them back by that
// digest into a new owner-only file named in the save dialog, with the custody
// notice. A digest that is not whole, a dismissed save dialog and a name that
// is taken or lies inside evidence are refused before anything is asked, as
// is a file larger than an artifact may be; bytes that do not hash to their
// digest and a digest the hub does not hold write nothing; and once
// disconnected nothing can be sent. The privacy status reads the hub active
// while each request reaches it.
func TestAnOperatorOnlyHubStoresAndReadsTheExactBytesByDigest(t *testing.T) {
	stub := newOperatorStub(t)
	dialog := &chooser{}
	app := newApp(t, dialog)
	stub.witness.app.Store(app)
	connectOperator(t, app, dialog, stub)
	if hub, detail := hubDisclosure(t, app), hubDisclosureDetail(app); hub != "connected" || !strings.Contains(detail, "operator-only hub") {
		t.Fatalf("a connected operator-only hub reads %s: %s", hub, detail)
	}

	evidence := []byte("MSH|^~\\&|SYNTHETIC\rPID|1||TEST-312\r\x00\xff")
	sum := sha256.Sum256(evidence)
	digest := hex.EncodeToString(sum[:])
	source := filepath.Join(t.TempDir(), "probe.bin")
	if err := os.WriteFile(source, evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	dialog.files = []string{source}
	var stored desktop.HubTransferResult
	stub.witness.during(t, "an operator-only store", "hub", func() { stored = app.StoreOperatorHubArtifact() })
	if stored.State != desktop.Completed || stored.TransferState != "completed" || stored.Digest != digest || stored.Size != int64(len(evidence)) || stored.Path != resolvedSource {
		t.Fatalf("the store: %+v", stored)
	}
	stub.sent(t, 1)
	if request := stub.requests.last(); request.method != http.MethodPut || request.path != "/v1/artifacts/"+digest || string(request.body) != string(evidence) {
		t.Fatalf("the store reached %s %s", request.method, request.path)
	}
	if dialog.titles[len(dialog.titles)-1] != "Choose the file to store in the operator-only hub" || dialog.opened[len(dialog.opened)-1] != "files" {
		t.Fatalf("the store opened %v", dialog.titles)
	}

	folder := t.TempDir()
	resolvedFolder, err := filepath.EvalSymlinks(folder)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(folder, "received.bin")
	dialog.destination = destination
	var read desktop.HubTransferResult
	stub.witness.during(t, "an operator-only read", "hub", func() { read = app.ReadOperatorHubArtifact(digest) })
	if read.State != desktop.Completed || read.TransferState != "completed" || read.Digest != digest || read.Size != int64(len(evidence)) ||
		read.Path != filepath.Join(resolvedFolder, "received.bin") || read.Warning != "Downloaded copies remain under local custody and cannot be revoked." {
		t.Fatalf("the read: %+v", read)
	}
	if got := mustRead(t, destination); string(got) != string(evidence) {
		t.Fatalf("the read wrote %q, want the stored bytes", got)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("the read file is not owner-only: %v %v", info.Mode(), err)
		}
	}
	stub.sent(t, 2)
	if request := stub.requests.last(); request.method != http.MethodGet || request.path != "/v1/artifacts/"+digest {
		t.Fatalf("the read reached %s %s", request.method, request.path)
	}
	if dialog.titles[len(dialog.titles)-1] != "Name the file to save the artifact as" || dialog.opened[len(dialog.opened)-1] != "save" {
		t.Fatalf("the read opened %v", dialog.titles)
	}

	// Refused before anything is asked of the hub: a digest that is not whole
	// opens no dialog, and a dismissed dialog or a name already taken sends
	// nothing and leaves the file there as it was.
	opened := len(dialog.opened)
	for _, address := range []string{"", digest[:63], strings.ToUpper(digest), digest + "a"} {
		if result := app.ReadOperatorHubArtifact(address); result.State != desktop.Failed || result.Reason != hubclient.ErrInvalidDigest.Error() {
			t.Fatalf("a read of %q: %+v", address, result)
		}
	}
	if len(dialog.opened) != opened {
		t.Fatal("a read of an address that is not a digest opened a dialog")
	}
	dialog.destination = ""
	if result := app.ReadOperatorHubArtifact(digest); result.State != desktop.Cancelled || result.Reason != "no file was named" {
		t.Fatalf("a dismissed save dialog: %+v", result)
	}
	dialog.destination = destination
	if err := os.WriteFile(destination, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := app.ReadOperatorHubArtifact(digest); result.State != desktop.Failed || result.Reason != "a file is already there; name a new file for the artifact" {
		t.Fatalf("a read into a name that is taken: %+v", result)
	}
	if got := mustRead(t, destination); string(got) != "kept" {
		t.Fatalf("a refused read replaced the file with %q", got)
	}
	evidenceFolder := filepath.Join(folder, "case")
	if err := os.Mkdir(evidenceFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, evidenceFolder, "identity.sha256", strings.Repeat("0", 64)+"\n")
	inEvidence := filepath.Join(evidenceFolder, "received.bin")
	dialog.destination = inEvidence
	if result := app.ReadOperatorHubArtifact(digest); result.State != desktop.Failed || !strings.Contains(result.Reason, "outside the immutable input case") {
		t.Fatalf("a read into evidence: %+v", result)
	}
	absent(t, inEvidence)
	stub.sent(t, 2)

	// Bytes that do not hash to the digest, and a digest the hub does not
	// hold, are each asked once and write nothing.
	substituted := []byte("synthetic bytes under another digest")
	stub.served.Store(&substituted)
	mismatched := filepath.Join(folder, "mismatched.bin")
	dialog.destination = mismatched
	if result := app.ReadOperatorHubArtifact(digest); result.State != desktop.Failed || result.Reason != hubclient.ErrIntegrity.Error() {
		t.Fatalf("a read whose bytes do not match: %+v", result)
	}
	absent(t, mismatched)
	stub.sent(t, 3)
	stub.served.Store(nil)
	unknown := strings.Repeat("0", 64)
	missing := filepath.Join(folder, "missing.bin")
	dialog.destination = missing
	if result := app.ReadOperatorHubArtifact(unknown); result.State != desktop.Failed || result.Reason != hubclient.ErrArtifactAbsent.Error() {
		t.Fatalf("a digest the hub does not hold: %+v", result)
	}
	absent(t, missing)
	stub.sent(t, 4)

	// A dismissed file dialog and a choice of several files send nothing.
	dialog.files = nil
	if result := app.StoreOperatorHubArtifact(); result.State != desktop.Cancelled {
		t.Fatalf("a dismissed file dialog: %+v", result)
	}
	dialog.files = []string{source, source}
	if result := app.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.Reason != "choose one file to store" {
		t.Fatalf("two files: %+v", result)
	}
	// A file larger than an artifact may be is refused before it is sent.
	oversized := filepath.Join(t.TempDir(), "oversized.bin")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(hubclient.MaxArtifactBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	dialog.files = []string{oversized}
	if result := app.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.Reason != "input exceeds size limit" {
		t.Fatalf("an oversized file: %+v", result)
	}
	stub.sent(t, 4)

	// Disconnected, the custody notice stays and nothing can be sent.
	if result := app.DisconnectOperatorHub(); result.State != desktop.Completed || result.Connected ||
		result.CustodyWarning != "Downloaded copies remain under local custody and cannot be revoked." || result.ConfigPath != stub.config {
		t.Fatalf("disconnect: %+v", result)
	}
	if hub := hubDisclosure(t, app); hub != "offline" {
		t.Fatalf("a disconnected operator-only hub reads %s", hub)
	}
	opened = len(dialog.opened)
	if result := app.ReadOperatorHubArtifact(digest); result.State != desktop.Failed || result.Reason != "not connected to the operator-only hub" {
		t.Fatalf("a read after disconnecting: %+v", result)
	}
	if result := app.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.Reason != "not connected to the operator-only hub" {
		t.Fatalf("a store after disconnecting: %+v", result)
	}
	if len(dialog.opened) != opened {
		t.Fatal("a disconnected window opened a dialog")
	}
	stub.sent(t, 4)
}

// Storing is authoring. A window with no activated license is refused at its
// own admission before any dialog opens or anything is sent, though it still
// connects and reads. With a license, the hub's own admission and every other
// answer it can give is reported once and never retried: a certificate its
// operation policy binds to no author, or a hub that has served team mode, is
// permission denied; a store route it does not offer, bytes it finds do not
// match their digest and a capacity it would exceed each fail; an answer that
// never arrived leaves the store uncertain rather than completed; and a hub
// that cannot be reached received nothing.
func TestAnOperatorOnlyStoreIsAdmittedTwiceAndEachRefusalIsReportedOnce(t *testing.T) {
	stub := newOperatorStub(t)
	evidence := []byte("synthetic operator-only evidence\n")
	source := filepath.Join(t.TempDir(), "evidence.txt")
	if err := os.WriteFile(source, evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(evidence)
	digest := hex.EncodeToString(sum[:])

	unlicensedDialog := &chooser{}
	state := t.TempDir()
	unlicensed := desktop.New(unlicensedDialog, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
		filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	connectOperator(t, unlicensed, unlicensedDialog, stub)
	opened := len(unlicensedDialog.opened)
	unlicensedDialog.files = []string{source}
	if result := unlicensed.StoreOperatorHubArtifact(); result.State != desktop.PermissionDenied || result.Reason == "" || strings.Contains(result.Reason, "hub") {
		t.Fatalf("a store without an activated license: %+v", result)
	}
	if len(unlicensedDialog.opened) != opened {
		t.Fatal("a store refused at admission opened the file dialog")
	}
	stub.sent(t, 0)

	dialog := &chooser{}
	app := newApp(t, dialog)
	connectOperator(t, app, dialog, stub)
	dialog.files = []string{source}
	if result := app.StoreOperatorHubArtifact(); result.State != desktop.Completed || result.Digest != digest {
		t.Fatalf("a licensed store: %+v", result)
	}
	stub.sent(t, 1)
	// Reading is not authoring: the unlicensed window reads what was stored.
	unlicensedDialog.destination = filepath.Join(t.TempDir(), "read.txt")
	if result := unlicensed.ReadOperatorHubArtifact(digest); result.State != desktop.Completed || string(mustRead(t, unlicensedDialog.destination)) != string(evidence) {
		t.Fatalf("an unlicensed read: %+v", result)
	}
	stub.sent(t, 2)

	sent := 2
	for _, refusal := range []struct {
		status        int
		state         desktop.State
		transferState string
		reason        error
	}{
		{http.StatusForbidden, desktop.PermissionDenied, "permission_denied", hubclient.ErrOperatorStoreRefused},
		{http.StatusNotFound, desktop.Failed, "failed", hubclient.ErrNoOperatorStore},
		{http.StatusUnprocessableEntity, desktop.Failed, "failed", hubclient.ErrStoreMismatch},
		{http.StatusRequestEntityTooLarge, desktop.Failed, "failed", hubclient.ErrOverCapacity},
		{http.StatusServiceUnavailable, desktop.Failed, "failed", hubclient.ErrStoreUnavailable},
	} {
		stub.status.Store(int32(refusal.status))
		result := app.StoreOperatorHubArtifact()
		if result.State != refusal.state || result.TransferState != refusal.transferState || result.Reason != refusal.reason.Error() || result.Digest != "" {
			t.Errorf("a store the hub answered %d: %+v", refusal.status, result)
		}
		sent++
		stub.sent(t, sent)
	}
	// A read the hub refuses because it has served team mode is permission
	// denied, and writes nothing.
	stub.status.Store(http.StatusForbidden)
	refused := filepath.Join(t.TempDir(), "refused.txt")
	dialog.destination = refused
	if result := app.ReadOperatorHubArtifact(digest); result.State != desktop.PermissionDenied || result.Reason != hubclient.ErrOperatorAccessRefused.Error() {
		t.Fatalf("a read the hub refused: %+v", result)
	}
	absent(t, refused)
	sent++
	stub.sent(t, sent)
	stub.status.Store(0)

	stub.hangUp.Store(true)
	if result := app.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.TransferState != "uncertain" || result.Reason != hubclient.ErrStoreUncertain.Error() {
		t.Fatalf("a store whose answer never arrived: %+v", result)
	}
	sent++
	stub.sent(t, sent)
	stub.hangUp.Store(false)

	stub.server.Close()
	if result := app.StoreOperatorHubArtifact(); result.State != desktop.Failed || result.Reason != hubclient.ErrHubUnreachable.Error() {
		t.Fatalf("a store to a stopped hub: %+v", result)
	}
	stub.sent(t, sent)
}
