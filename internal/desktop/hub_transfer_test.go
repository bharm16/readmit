package desktop_test

// The customer hub's transfers and searches the hub panel offers (#311): a
// configuration chosen through the host's dialog, evidence published under an
// activated author admission, an approved support export downloaded, and the
// team's history and notifications searched through the v2 routes. Each is
// driven through the facade against a mutual-TLS hub stub that records what
// reached it, so a refusal the window reaches itself is shown to have sent
// nothing and a refusal the hub gives is shown to have been asked once and
// never retried. The hub's own enforcement of the same routes is tested
// through its real handler in the hub module.

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/sharing"
)

// hubRequests records the requests of the routes under test that reached the
// hub stub, in the order they arrived.
type hubRequests struct {
	mu   sync.Mutex
	seen []hubRequest
}

type hubRequest struct {
	method, path string
	body         []byte
}

func (h *hubRequests) record(r *http.Request) []byte {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, hubRequest{r.Method, r.URL.Path, body})
	return body
}

func (h *hubRequests) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.seen)
}

func (h *hubRequests) last() hubRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.seen) == 0 {
		return hubRequest{}
	}
	return h.seen[len(h.seen)-1]
}

// hubIdentity is the customer identity provider's token endpoint: it answers
// every code with a newly signed access token for its subject, holding the
// scopes and lifetime last set, as an IdP issues whatever its policy says now.
type hubIdentity struct {
	t        *testing.T
	key      *rsa.PrivateKey
	subject  string
	mu       sync.Mutex
	scopes   []string
	lifetime time.Duration
}

func newHubIdentity(t *testing.T, subject string, scopes []string) *hubIdentity {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &hubIdentity{t: t, key: key, subject: subject, scopes: scopes, lifetime: 30 * time.Minute}
}

func (i *hubIdentity) issue(scopes []string, lifetime time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.scopes, i.lifetime = scopes, lifetime
}

func (i *hubIdentity) token(w http.ResponseWriter, _ *http.Request) {
	i.mu.Lock()
	scopes, lifetime := i.scopes, i.lifetime
	i.mu.Unlock()
	now := time.Now().Unix()
	token := issueDesktopTestToken(i.t, i.key, "key-1", "https://idp.hospital.org", i.subject, "hub-aud", "desktop-app",
		scopes, now, now+int64(lifetime/time.Second))
	w.Header().Set("Content-Type", "application/json")
	_ = json.MarshalWrite(w, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(lifetime / time.Second)})
}

// signInWithCode completes a sign-in the window started with the code the
// identity provider returned to the browser, and returns what the window then
// shows.
func signInWithCode(t *testing.T, app *desktop.App) desktop.HubResult {
	t.Helper()
	flow := app.StartHubAuth()
	authorization, err := url.Parse(flow.AuthURL)
	if flow.State != desktop.Completed || err != nil {
		t.Fatalf("start sign-in: %+v", flow)
	}
	result := app.CompleteHubAuth("code-from-the-browser", authorization.Query().Get("state"))
	if result.State != desktop.Completed || !result.Authenticated {
		t.Fatalf("sign-in: %+v", result)
	}
	return result
}

// untilExpired waits until the session a sign-in reported has expired.
func untilExpired(t *testing.T, signedIn desktop.HubResult) {
	t.Helper()
	expires, err := time.Parse(time.RFC3339, signedIn.ExpiresAt)
	if err != nil {
		t.Fatalf("the sign-in reports no expiry: %+v", signedIn)
	}
	time.Sleep(time.Until(expires) + 100*time.Millisecond)
}

func healthy(mux *http.ServeMux) {
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
}

// A configuration is chosen through the host's folder dialog: the operator's
// hub-client.json is found in the chosen folder, or hub.json where the file
// has that name, and it is what the next window restores. A folder holding
// neither, or a configuration that does not validate, is refused and keeps
// what was selected; a dismissed dialog changes nothing. Choosing reaches no
// hub.
func TestChoosingAHubConfigurationFindsTheOperatorsFileAndRefusesAFolderWithoutOne(t *testing.T) {
	endpoint := newCountingEndpoint(t, false)
	hub := "https://" + endpoint.address
	operator := t.TempDir()
	chosen := writeHubClientConfig(t, operator, hub)
	// hub-client.json is the operator's file even beside a hub.json.
	writeDocument(t, operator, "hub.json", `{"schema":"something-else/v1"}`)
	named := t.TempDir()
	writeHubClientConfig(t, named, hub)
	if err := os.Rename(filepath.Join(named, "hub-client.json"), filepath.Join(named, "hub.json")); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	plain := t.TempDir()
	writeHubClientConfig(t, plain, "http://"+endpoint.address)

	selection := filepath.Join(t.TempDir(), "operations.json")
	app := freshApp(t, &queueChooser{folders: []string{operator, empty, plain, named}}, selection)
	selected := func(want string) {
		t.Helper()
		if status := app.HubStatus(); status.ConfigPath != want || status.Connected || status.Authenticated {
			t.Fatalf("the hub panel shows %+v, want %s selected and offline", status, want)
		}
	}

	if result := app.ChooseHubConfig(); result.State != desktop.Completed || result.ConfigPath != chosen || result.HubURL != hub || result.Connected {
		t.Fatalf("choosing the operator's folder: %+v", result)
	}
	if result := app.ChooseHubConfig(); result.State != desktop.Failed || result.Reason != "the chosen folder does not contain hub-client.json" {
		t.Fatalf("choosing a folder without a configuration: %+v", result)
	}
	selected(chosen)
	if result := app.ChooseHubConfig(); result.State != desktop.Failed || !strings.Contains(result.Reason, "https") {
		t.Fatalf("choosing a configuration that does not validate: %+v", result)
	}
	selected(chosen)
	renamed := filepath.Join(named, "hub.json")
	if result := app.ChooseHubConfig(); result.State != desktop.Completed || result.ConfigPath != renamed {
		t.Fatalf("choosing a folder whose configuration is hub.json: %+v", result)
	}
	if result := app.ChooseHubConfig(); result.State != desktop.Cancelled {
		t.Fatalf("a dismissed dialog: %+v", result)
	}
	selected(renamed)

	if status := freshApp(t, &queueChooser{}, selection).HubStatus(); status.ConfigPath != renamed || status.Connected {
		t.Fatalf("the next window restored %+v, want the configuration last chosen", status)
	}
	if n := endpoint.accepted.Load(); n != 0 {
		t.Fatalf("choosing a hub configuration reached the hub %d times", n)
	}
}

// Publishing evidence is authoring: the window admits the author against the
// activated license before anything is sent, needs a signed-in session whose
// token grants evidence.write, and the hub stores exactly the bytes of the
// chosen file under their digest. A role the hub refuses and bytes it finds
// do not match their digest are each asked once and reported, never retried.
func TestPublishingEvidenceNeedsAuthorAdmissionAndASessionAndStoresTheExactBytes(t *testing.T) {
	puts := &hubRequests{}
	var storedMu sync.Mutex
	stored := map[string][]byte{}
	var refuse atomic.Int32
	mux := http.NewServeMux()
	healthy(mux)
	mux.HandleFunc("/v1/projects/cardio-study/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		body := puts.record(r)
		if r.Method != http.MethodPut || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if status := refuse.Load(); status != 0 {
			w.WriteHeader(int(status))
			return
		}
		digest := path.Base(r.URL.Path)
		if sharing.Digest(body) != digest {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		storedMu.Lock()
		stored[digest] = body
		storedMu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})
	writer := []string{"evidence.read", "evidence.write"}
	identity := newHubIdentity(t, "author@hospital.org", writer)
	fixture := newConnectedHubApp(t, mux, "author@hospital.org", writer, identity.token)
	app := fixture.app

	evidence := []byte("synthetic reschedule evidence for the team\n")
	source := filepath.Join(t.TempDir(), "reschedule-evidence.txt")
	if err := os.WriteFile(source, evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	publish := func(window *desktop.App) desktop.HubTransferResult {
		return window.UploadHubArtifact(desktop.HubUploadRequest{Project: "cardio-study", SourcePath: source})
	}
	sent := func(want int) {
		t.Helper()
		if got := puts.count(); got != want {
			t.Fatalf("the hub received %d uploads, want %d", got, want)
		}
	}

	// Connected but not signed in, the window refuses and sends nothing.
	if result := publish(app); result.State != desktop.PermissionDenied || !strings.Contains(result.Reason, "sign-in required") {
		t.Fatalf("an upload before sign-in: %+v", result)
	}
	sent(0)

	// Signed in, the exact bytes are stored under their digest.
	signInWithCode(t, app)
	digest := sharing.Digest(evidence)
	result := publish(app)
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != desktop.Completed || result.TransferState != "completed" || result.Digest != digest ||
		result.Size != int64(len(evidence)) || result.Path != resolved {
		t.Fatalf("the upload: %+v", result)
	}
	sent(1)
	if request := puts.last(); request.method != http.MethodPut || request.path != "/v1/projects/cardio-study/artifacts/"+digest {
		t.Fatalf("the upload reached %s %s", request.method, request.path)
	}
	storedMu.Lock()
	kept := string(stored[digest])
	storedMu.Unlock()
	if kept != string(evidence) {
		t.Fatalf("the hub stored %q, want the chosen file's bytes", kept)
	}

	// A role the hub refuses is permission denied, asked once.
	refuse.Store(http.StatusForbidden)
	if result := publish(app); result.State != desktop.PermissionDenied || result.Reason != "hub access refused; insufficient permissions or role revoked" {
		t.Fatalf("an upload the hub's roles refuse: %+v", result)
	}
	sent(2)
	// Bytes the hub finds do not match their digest fail, asked once.
	refuse.Store(http.StatusUnprocessableEntity)
	if result := publish(app); result.State != desktop.Failed || !strings.Contains(result.Reason, "digest mismatch") {
		t.Fatalf("an upload whose digest the hub refuses: %+v", result)
	}
	sent(3)
	refuse.Store(0)

	// A token that grants no evidence.write is refused by the window.
	identity.issue([]string{"evidence.read"}, 30*time.Minute)
	signInWithCode(t, app)
	if result := publish(app); result.State != desktop.PermissionDenied {
		t.Fatalf("an upload under a session without evidence.write: %+v", result)
	}
	sent(3)

	// An expired session is refused by the window; nothing renews it.
	identity.issue(writer, 2*time.Second)
	untilExpired(t, signInWithCode(t, app))
	if result := publish(app); result.State != desktop.PermissionDenied || !strings.Contains(result.Reason, "session expired") {
		t.Fatalf("an upload after the session expired: %+v", result)
	}
	sent(3)

	// A window with no activated license is refused at admission, signed in
	// or not, before anything is sent.
	identity.issue(writer, 30*time.Minute)
	state := t.TempDir()
	unlicensed := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
		filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
	if result := unlicensed.SelectHubConfig(filepath.Join(fixture.dir, "hub-client.json")); result.State != desktop.Completed {
		t.Fatalf("select: %+v", result)
	}
	if result := unlicensed.ConnectHub(); result.State != desktop.Completed {
		t.Fatalf("connect: %+v", result)
	}
	signInWithCode(t, unlicensed)
	if result := publish(unlicensed); result.State != desktop.PermissionDenied || result.Reason == "" || strings.Contains(result.Reason, "sign-in") {
		t.Fatalf("an upload without an activated license: %+v", result)
	}
	sent(3)

	// Disconnected, the window has no hub to send to.
	app.DisconnectHub()
	if result := publish(app); result.State != desktop.Failed || result.Reason != "not connected to customer hub" {
		t.Fatalf("an upload after disconnecting: %+v", result)
	}
	sent(3)
}

// A support export is downloaded only as the exact bytes the approval chain
// named: the digest the hub serves under is verified before anything is
// written, the file is written whole with the custody notice, and a digest
// the hub refuses or bytes that do not match it leave nothing at the
// destination.
func TestDownloadingAnApprovedSupportExportWritesOnlyTheVerifiedBytes(t *testing.T) {
	policy := []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["customer-hub-download"],"max_bytes":4096}`)
	summary, err := json.Marshal(sharing.Summary{Schema: sharing.Schema, SourceKind: "retained-packet",
		SourceIdentity: strings.Repeat("1", 64), InputCommitment: strings.Repeat("2", 64), SpecIdentity: strings.Repeat("3", 64),
		PolicyIdentity: sharing.Digest(policy), Outcome: "assertion_failure", ExternalEquivalence: "declined", Scope: sharing.Scope},
		json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	approved := sharing.Digest(summary)
	// The hub answers this digest with the approved summary's bytes, which do
	// not hash to it.
	substituted := strings.Repeat("f", 64)
	exports := &hubRequests{}
	mux := http.NewServeMux()
	healthy(mux)
	mux.HandleFunc("/v2/projects/cardio-study/exports/", func(w http.ResponseWriter, r *http.Request) {
		exports.record(r)
		switch path.Base(r.URL.Path) {
		case approved, substituted:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(summary)
		default:
			http.Error(w, "exact reviewed support unavailable", http.StatusForbidden)
		}
	})
	app := newAuthenticatedHubApp(t, mux, "analyst@hospital.org", []string{"evidence.read", "export"}).app
	folder := t.TempDir()
	resolvedFolder, err := filepath.EvalSymlinks(folder)
	if err != nil {
		t.Fatal(err)
	}
	download := func(digest, destination string) desktop.HubTransferResult {
		return app.DownloadHubExport(desktop.HubDownloadRequest{Project: "cardio-study", Digest: digest, DestinationPath: destination})
	}
	absent := func(destination string) {
		t.Helper()
		for _, left := range []string{destination, destination + ".incomplete"} {
			if _, err := os.Lstat(left); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a refused download left %s behind", left)
			}
		}
	}
	sent := func(want int) {
		t.Helper()
		if got := exports.count(); got != want {
			t.Fatalf("the hub received %d export requests, want %d", got, want)
		}
	}

	destination := filepath.Join(folder, "support.json")
	result := download(approved, destination)
	if result.State != desktop.Completed || result.TransferState != "completed" || result.Digest != approved ||
		result.Size != int64(len(summary)) || result.Path != filepath.Join(resolvedFolder, "support.json") ||
		result.Warning != "Downloaded copies remain under local custody and cannot be revoked." {
		t.Fatalf("the approved export: %+v", result)
	}
	if got := mustRead(t, destination); string(got) != string(summary) {
		t.Fatalf("the downloaded export is %q, want the approved summary's bytes", got)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("the downloaded export is not owner-only: %v %v", info.Mode(), err)
		}
	}
	sent(1)
	if request := exports.last(); request.method != http.MethodGet || request.path != "/v2/projects/cardio-study/exports/"+approved {
		t.Fatalf("the export was asked of %s %s", request.method, request.path)
	}

	// A digest the approval chain does not name is refused by the hub.
	unnamed := filepath.Join(folder, "unnamed.json")
	if result := download(sharing.Digest(policy), unnamed); result.State != desktop.PermissionDenied || result.TransferState != "permission_denied" {
		t.Fatalf("an export the approval chain does not name: %+v", result)
	}
	absent(unnamed)
	sent(2)
	// Bytes that do not match the digest asked for are never written.
	mismatched := filepath.Join(folder, "mismatched.json")
	if result := download(substituted, mismatched); result.State != desktop.Failed || !strings.Contains(result.Reason, "digest mismatch") {
		t.Fatalf("an export whose bytes do not match its digest: %+v", result)
	}
	absent(mismatched)
	sent(3)

	// A digest that is not one, or a destination whose folder is missing, is
	// refused before anything is asked.
	if result := download("not-a-digest", filepath.Join(folder, "named.json")); result.State != desktop.Failed || result.Reason != "invalid artifact digest" {
		t.Fatalf("an export named by something other than a digest: %+v", result)
	}
	if result := download(approved, filepath.Join(folder, "missing", "support.json")); result.State != desktop.Failed || !strings.Contains(result.Reason, "invalid destination path") {
		t.Fatalf("an export to a missing folder: %+v", result)
	}
	sent(3)

	app.DisconnectHub()
	if result := download(approved, filepath.Join(folder, "after.json")); result.State != desktop.Failed || result.Reason != "not connected to customer hub" {
		t.Fatalf("an export after disconnecting: %+v", result)
	}
	sent(3)
}

// The notification list and the history and notification searches read the
// v2 routes with exactly the query the person typed, and never the v1 ones.
// A query the query contract cannot carry is refused before anything is
// sent; a refusal the hub gives is asked once.
func TestHistoryAndNotificationSearchesCarryThePersonsQueryToTheV2Routes(t *testing.T) {
	evidence := strings.Repeat("a", 64)
	events := []hubprotocol.ReviewEvent{
		{Schema: hubprotocol.ReviewEventV1, Project: "cardio-study", Sequence: 2, Issuer: "https://idp.hospital.org",
			Actor: "reviewer@hospital.org", At: "2026-09-23T10:00:00Z",
			Command: hubprotocol.ReviewCommand{Schema: hubprotocol.ReviewCommandV1, ID: "reviewer-confirms", Expected: 1, Kind: "comment",
				Evidence: evidence, Recipient: "analyst@hospital.org", Text: "Confirmed the reschedule on the lab fixture"}},
	}
	reads := &hubRequests{}
	legacy := &hubRequests{}
	var refuse atomic.Int32
	answer := func(w http.ResponseWriter, r *http.Request) {
		reads.record(r)
		if status := refuse.Load(); status != 0 {
			w.WriteHeader(int(status))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, hubprotocol.ReviewHistory{Schema: hubprotocol.ReviewHistoryV2, Head: 2, Events: events})
	}
	mux := http.NewServeMux()
	healthy(mux)
	mux.HandleFunc("/v2/projects/cardio-study/history", answer)
	mux.HandleFunc("/v2/projects/cardio-study/notifications", answer)
	for _, route := range []string{"/v1/projects/cardio-study/history", "/v1/projects/cardio-study/notifications"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			legacy.record(r)
			w.WriteHeader(http.StatusConflict)
		})
	}
	app := newAuthenticatedHubApp(t, mux, "analyst@hospital.org", []string{"evidence.read"}).app
	sent := func(want int) {
		t.Helper()
		if got := reads.count(); got != want {
			t.Fatalf("the hub received %d reads, want %d", got, want)
		}
	}
	found := func(name string, result desktop.HubReviewsResult) {
		t.Helper()
		if result.State != desktop.Completed || result.Project != "cardio-study" || result.Head != 2 || len(result.Events) != 1 ||
			result.Warning != "Downloaded copies remain under local custody and cannot be revoked." {
			t.Fatalf("%s: %+v", name, result)
		}
		if got := result.Events[0]; got.Sequence != 2 || got.Actor != "reviewer@hospital.org" || got.Kind != "comment" || got.Evidence != evidence ||
			got.Recipient != "analyst@hospital.org" || got.Text != "Confirmed the reschedule on the lab fixture" || got.CommandID != "reviewer-confirms" {
			t.Fatalf("%s shows %+v, want the event the hub recorded", name, got)
		}
	}

	found("the notification list", app.ListHubNotifications("cardio-study"))
	if request := reads.last(); request.method != http.MethodGet || request.path != "/v2/projects/cardio-study/notifications" || len(request.body) != 0 {
		t.Fatalf("the notification list asked %s %s %q", request.method, request.path, request.body)
	}
	query := desktop.HubReviewQueryRequest{Project: "cardio-study", After: 1, Text: "reschedule", Evidence: evidence}
	wire := `{"schema":"readmit-hub-review-query/v1","after":1,"text":"reschedule","evidence":"` + evidence + `"}`
	for _, search := range []struct {
		name  string
		route string
		run   func(desktop.HubReviewQueryRequest) desktop.HubReviewsResult
	}{
		{"the history search", "/v2/projects/cardio-study/history", app.SearchHubReviews},
		{"the notification search", "/v2/projects/cardio-study/notifications", app.SearchHubNotifications},
	} {
		found(search.name, search.run(query))
		if request := reads.last(); request.method != http.MethodPost || request.path != search.route || string(request.body) != wire {
			t.Fatalf("%s asked %s %s %s, want POST %s %s", search.name, request.method, request.path, request.body, search.route, wire)
		}

		// A query the contract cannot carry is refused before anything is sent.
		before := reads.count()
		for problem, refused := range map[string]desktop.HubReviewQueryRequest{
			"a negative sequence":        {Project: "cardio-study", After: -1},
			"text beyond 256 bytes":      {Project: "cardio-study", Text: strings.Repeat("r", 257)},
			"text holding a NUL":         {Project: "cardio-study", Text: "re\x00schedule"},
			"text that is not UTF-8":     {Project: "cardio-study", Text: "re\xffschedule"},
			"a partial digest":           {Project: "cardio-study", Evidence: evidence[:63]},
			"an uppercase digest":        {Project: "cardio-study", Evidence: strings.ToUpper(evidence)},
			"evidence that is no digest": {Project: "cardio-study", Evidence: "the reschedule message"},
		} {
			if result := search.run(refused); result.State != desktop.Failed || result.Reason == "" || result.Project != "cardio-study" {
				t.Fatalf("%s with %s: %+v", search.name, problem, result)
			}
		}
		sent(before)
	}

	// A refusal the hub gives is shown for what it is, asked once each.
	before := reads.count()
	refuse.Store(http.StatusForbidden)
	for name, result := range map[string]desktop.HubReviewsResult{
		"the notification list":   app.ListHubNotifications("cardio-study"),
		"the history search":      app.SearchHubReviews(query),
		"the notification search": app.SearchHubNotifications(query),
	} {
		if result.State != desktop.PermissionDenied || result.Reason != "hub access refused; insufficient permissions or role revoked" {
			t.Fatalf("%s the hub refused: %+v", name, result)
		}
	}
	sent(before + 3)
	refuse.Store(0)

	// Disconnected, nothing is asked.
	app.DisconnectHub()
	for name, result := range map[string]desktop.HubReviewsResult{
		"the notification list":   app.ListHubNotifications("cardio-study"),
		"the history search":      app.SearchHubReviews(query),
		"the notification search": app.SearchHubNotifications(query),
	} {
		if result.State != desktop.Failed || result.Reason != "not connected to customer hub" {
			t.Fatalf("%s after disconnecting: %+v", name, result)
		}
	}
	sent(before + 3)
	if n := legacy.count(); n != 0 {
		t.Fatalf("the window asked the v1 review routes %d times", n)
	}
}
