package hub_test

// Several authenticated people, each in their own application window, work one
// project on a real customer hub: the team handler served over mutual TLS with
// PostgreSQL metadata, identities signed by an identity provider the hub
// trusts, and every window driving the same typed facade its screens call. No
// window disables anything here and no response is stubbed, because a disabled
// control is not the boundary: the hub's roles, compare-and-swap history,
// approval chains, revocation and session expiry are. A counter in front of
// the hub records what each person's window actually sent, so a refusal the
// window reached locally is shown to have sent nothing and a refusal the hub
// gave is shown to have been asked exactly once.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/sharing"
	"github.com/bharm16/readmit/internal/testlicense"
)

// sentBy counts the requests that reached the hub per signed-in subject. The
// subject is read from the bearer token's claims only to attribute the count;
// the hub itself verifies every token.
type sentBy struct {
	mu     sync.Mutex
	counts map[string]int
}

func (s *sentBy) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject := "unauthenticated"
		if parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "."); len(parts) == 3 {
			var claims struct {
				Subject string `json:"sub"`
			}
			if raw, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil && json.Unmarshal(raw, &claims) == nil {
				subject = claims.Subject
			}
		}
		s.mu.Lock()
		s.counts[subject]++
		s.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (s *sentBy) of(subject string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[subject]
}

func (s *sentBy) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, count := range s.counts {
		total += count
	}
	return total
}

func TestPostgresTwoDesktopUsersEnforceRolesConflictsRevocationAndExpiry(t *testing.T) {
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	cert := certificates(t, &c)
	access, grants, key, accessPath := accessFixture(t)
	store := open(t, c)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Every person who writes presents the one client certificate these
	// windows share; the hub binds each author to it. The viewer is a free
	// read-only reviewer and holds no author binding at all.
	dir := t.TempDir()
	certificateDigest := fmt.Sprintf("%x", sha256.Sum256(cert.Certificate[0]))
	operation := hub.OperationPolicy{Schema: "readmit-hub-operation-policy/v1", Policy: testlicense.New(t), Bindings: []hub.OperationBinding{}}
	for _, subject := range []string{"owner", "admin", "analyst", "reviewer"} {
		operation.Bindings = append(operation.Bindings, hub.OperationBinding{Issuer: "https://idp.example", Subject: subject, Certificate: certificateDigest, Author: "test-author", Device: "test-device"})
	}
	operationPath := filepath.Join(dir, "hub-operation.json")
	if raw, err := json.Marshal(operation); err != nil || os.WriteFile(operationPath, raw, 0o600) != nil {
		t.Fatal("hub operation policy", err)
	}
	if err := store.SetOperationPolicy(operationPath); err != nil {
		t.Fatal(err)
	}
	sent := &sentBy{counts: map[string]int{}}
	server := httptest.NewUnstartedServer(sent.wrap(store.TeamHandler(access)))
	server.TLS, _ = c.TLS()
	server.StartTLS()
	defer server.Close()

	// The identity provider answers an authorization code with the signed
	// access token of the person it names.
	var codes sync.Map
	var exchanges atomic.Int64
	issue := func(code, subject string, lifetime time.Duration) {
		now := time.Now().Unix()
		token := claims(subject)
		token["iat"], token["exp"] = now-1, now+int64(lifetime/time.Second)
		codes.Store(code, signed(t, key, token, accessHeader))
	}
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = r.ParseForm()
		token, ok := codes.Load(r.PostForm.Get("code"))
		if !ok {
			http.Error(w, "unknown code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 300})
	}))
	defer idp.Close()

	clientPEM := filepath.Join(dir, "client.pem")
	keyPEM := filepath.Join(dir, "client-key.pem")
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
	config := filepath.Join(dir, "hub-client.json")
	if err := os.WriteFile(config, fmt.Appendf(nil, `{"schema":"readmit-hub-client/v1","hub":%q,"ca":%q,"certificate":%q,`+
		`"key":{"command":"/bin/cat","arguments":[%q]},`+
		`"idp":{"issuer":"https://idp.example","client_id":"readmit-client","audience":"https://hub.example","authorize_endpoint":"https://idp.example/authorize","token_endpoint":%q,"scopes":["evidence.read","evidence.write","approval","export","admin"]},`+
		`"projects":["alpha"]}`, server.URL, c.ClientCA, clientPEM, keyPEM, idp.URL+"/token"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Each person's window keeps its own local state, exactly as two
	// installations do.
	window := func(state string) *desktop.App {
		t.Helper()
		app := desktop.New(silentChooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"),
			filepath.Join(state, "session.json"), filepath.Join(state, "drafts.json"))
		if result := app.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
			t.Fatal(result)
		}
		if result := app.SelectHubConfig(config); result.State != desktop.Completed {
			t.Fatalf("select hub: %+v", result)
		}
		return app
	}
	signIn := func(app *desktop.App, subject string, lifetime time.Duration) desktop.HubResult {
		t.Helper()
		if result := app.ConnectHub(); result.State != desktop.Completed {
			t.Fatalf("connect as %s: %+v", subject, result)
		}
		flow := app.StartHubAuth()
		authorization, err := url.Parse(flow.AuthURL)
		if flow.State != desktop.Completed || err != nil {
			t.Fatalf("start sign-in as %s: %+v", subject, flow)
		}
		code := fmt.Sprintf("code-%s-%d", subject, time.Now().UnixNano())
		issue(code, subject, lifetime)
		return app.CompleteHubAuth(code, authorization.Query().Get("state"))
	}
	person := map[string]*desktop.App{}
	for _, subject := range []string{"analyst", "reviewer", "admin", "viewer"} {
		person[subject] = window(t.TempDir())
		if result := signIn(person[subject], subject, 5*time.Minute); result.State != desktop.Completed || !result.Authenticated || result.Subject != subject ||
			len(result.Projects) != 1 || !result.Projects[0].Authorized {
			t.Fatalf("sign in as %s: %+v", subject, result)
		}
	}
	upload := func(subject, name string, data []byte) desktop.HubTransferResult {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return person[subject].UploadHubArtifact(desktop.HubUploadRequest{Project: "alpha", SourcePath: path})
	}
	head := func(subject string) int {
		t.Helper()
		history := person[subject].ListHubReviews("alpha")
		if history.State != desktop.Completed {
			t.Fatalf("%s reads the history: %+v", subject, history)
		}
		return history.Head
	}

	// Roles are enforced by the hub behind every action a window offers.
	evidence := upload("analyst", "evidence.bin", []byte("synthetic team evidence"))
	if evidence.State != desktop.Completed {
		t.Fatalf("the analyst could not link evidence: %+v", evidence)
	}
	before := sent.of("viewer")
	if result := upload("viewer", "viewer.bin", []byte("a viewer's upload")); result.State != desktop.PermissionDenied {
		t.Fatalf("the hub accepted a viewer's upload: %+v", result)
	}
	if result := person["viewer"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "viewer-note", Expected: head("viewer"), Kind: "comment", Evidence: evidence.Digest, Text: "a viewer's comment"}); result.State != desktop.PermissionDenied {
		t.Fatalf("the hub accepted a viewer's comment: %+v", result)
	}
	if got := sent.of("viewer") - before; got != 3 {
		t.Fatalf("the viewer's two refused writes and one read reached the hub %d times, want each exactly once", got)
	}

	// Two people posting against the same head: the first is recorded, the
	// second is refused as a conflict rather than silently ordered, and each
	// recorded actor is the one the hub authenticated.
	shared := head("analyst")
	if result := person["analyst"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "analyst-note", Expected: shared, Kind: "comment", Evidence: evidence.Digest, Text: "the reschedule duplicates the booking"}); result.State != desktop.Completed {
		t.Fatalf("the first post: %+v", result)
	}
	stale := person["reviewer"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "reviewer-note", Expected: shared, Kind: "comment", Evidence: evidence.Digest, Text: "confirmed on the lab fixture"})
	if stale.State != desktop.Failed || !strings.Contains(stale.Reason, "conflict") {
		t.Fatalf("a post against a stale head was not refused as a conflict: %+v", stale)
	}
	if reused := person["reviewer"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "analyst-note", Expected: head("reviewer"), Kind: "comment", Evidence: evidence.Digest, Text: "the reschedule duplicates the booking"}); reused.State != desktop.Failed {
		t.Fatalf("one person replayed another's command id: %+v", reused)
	}
	if renewed := person["reviewer"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "reviewer-note", Expected: head("reviewer"), Kind: "comment", Evidence: evidence.Digest, Text: "confirmed on the lab fixture"}); renewed.State != desktop.Completed {
		t.Fatalf("the renewed post: %+v", renewed)
	}
	actors := []string{}
	for _, event := range person["viewer"].ListHubReviews("alpha").Events {
		actors = append(actors, event.Actor)
	}
	if strings.Join(actors, ",") != "analyst,reviewer" {
		t.Fatalf("the recorded actors are %v, want the two authenticated people in order", actors)
	}
	// Searching reads: the free read-only viewer, who holds no author
	// binding, searches the history and their notifications.
	query := desktop.HubReviewQueryRequest{Project: "alpha", Text: "reschedule", Evidence: evidence.Digest}
	if found := person["viewer"].SearchHubReviews(query); found.State != desktop.Completed || len(found.Events) != 1 || found.Events[0].Actor != "analyst" {
		t.Fatalf("a read-only viewer could not search the history: %+v", found)
	}
	for name, result := range map[string]desktop.HubReviewsResult{
		"list notifications":   person["viewer"].ListHubNotifications("alpha"),
		"search notifications": person["viewer"].SearchHubNotifications(query),
	} {
		if result.State != desktop.Completed {
			t.Fatalf("a read-only viewer could not %s: %+v", name, result)
		}
	}

	// A support export is served only under the exact approval chain, to a
	// role that may export, and a later policy withdraws the approval.
	policy := []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["customer-hub-download"],"max_bytes":4096}`)
	policyUpload := upload("admin", "policy.json", policy)
	summary := sharing.Summary{Schema: sharing.Schema, SourceKind: "retained-packet", SourceIdentity: strings.Repeat("1", 64), InputCommitment: strings.Repeat("2", 64),
		SpecIdentity: strings.Repeat("3", 64), PolicyIdentity: policyUpload.Digest, Outcome: "assertion_failure", ExternalEquivalence: "declined", Scope: sharing.Scope}
	summaryBytes, err := json.Marshal(summary, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	summaryUpload := upload("analyst", "support.json", summaryBytes)
	if policyUpload.State != desktop.Completed || summaryUpload.State != desktop.Completed {
		t.Fatalf("support inputs: %+v %+v", policyUpload, summaryUpload)
	}
	policyCommand := desktop.HubReviewCommandRequest{Project: "alpha", ID: "policy", Kind: "support-policy", Evidence: policyUpload.Digest, Text: "support"}
	policyCommand.Expected = head("analyst")
	if result := person["analyst"].PostHubReview(policyCommand); result.State != desktop.PermissionDenied {
		t.Fatalf("an analyst set the sharing policy: %+v", result)
	}
	if result := person["admin"].PostHubReview(policyCommand); result.State != desktop.Completed {
		t.Fatalf("the administrator's sharing policy: %+v", result)
	}
	if result := person["analyst"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "request", Expected: head("analyst"), Kind: "support-request",
		Evidence: summaryUpload.Digest, Release: policyUpload.Digest, Parent: "policy", Recipient: "reviewer", Text: "support"}); result.State != desktop.Completed {
		t.Fatalf("the support request: %+v", result)
	}
	approval := desktop.HubReviewCommandRequest{Project: "alpha", ID: "approve", Kind: "support-approval", Evidence: summaryUpload.Digest, Release: policyUpload.Digest, Parent: "request", Text: "support"}
	approval.Expected = head("analyst")
	if result := person["analyst"].PostHubReview(approval); result.State != desktop.PermissionDenied {
		t.Fatalf("the requester approved their own request: %+v", result)
	}
	if result := person["reviewer"].PostHubReview(approval); result.State != desktop.Completed {
		t.Fatalf("the asked reviewer's approval: %+v", result)
	}
	export := func(subject string) desktop.HubTransferResult {
		return person[subject].DownloadHubExport(desktop.HubDownloadRequest{Project: "alpha", Digest: summaryUpload.Digest, DestinationPath: filepath.Join(t.TempDir(), "support.json")})
	}
	if result := export("viewer"); result.State != desktop.PermissionDenied {
		t.Fatalf("a viewer downloaded the support export: %+v", result)
	}
	downloaded := export("analyst")
	if data, err := os.ReadFile(downloaded.Path); downloaded.State != desktop.Completed || err != nil || !bytes.Equal(data, summaryBytes) {
		t.Fatalf("the approved export: %+v %v", downloaded, err)
	}
	newer := upload("admin", "policy-2.json", []byte(`{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["customer-hub-download"],"max_bytes":8192}`))
	if result := person["admin"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "policy-2", Expected: head("admin"), Kind: "support-policy", Evidence: newer.Digest, Text: "support"}); result.State != desktop.Completed {
		t.Fatalf("the newer sharing policy: %+v", result)
	}
	if result := export("analyst"); result.State != desktop.PermissionDenied {
		t.Fatalf("an approval under a superseded policy still authorized the export: %+v", result)
	}
	approval.Expected = head("reviewer")
	if result := person["reviewer"].PostHubReview(approval); result.State == desktop.Completed {
		t.Fatalf("a stale approval was recorded again under the newer policy: %+v", result)
	}

	// Removing a grant takes effect on the next request of a window that is
	// still signed in, and a refused request is sent once, never retried.
	grants.Grants = grants.Grants[:0:0]
	for _, grant := range []hub.ProjectGrant{{Project: "alpha", Subject: "owner", Role: "owner"}, {Project: "alpha", Subject: "admin", Role: "admin"},
		{Project: "alpha", Subject: "analyst", Role: "analyst"}, {Project: "alpha", Subject: "viewer", Role: "viewer"}} {
		grants.Grants = append(grants.Grants, grant)
	}
	writePolicy(t, accessPath, grants)
	before = sent.of("reviewer")
	if result := person["reviewer"].PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "after-revocation", Expected: head("analyst"), Kind: "comment", Evidence: evidence.Digest, Text: "still here"}); result.State != desktop.PermissionDenied {
		t.Fatalf("a revoked reviewer posted: %+v", result)
	}
	if got := sent.of("reviewer") - before; got != 1 {
		t.Fatalf("the refused post reached the hub %d times, want exactly once", got)
	}
	if status := person["reviewer"].HubStatus(); len(status.Projects) != 1 || status.Projects[0].Authorized {
		t.Fatalf("a revoked reviewer's window still shows the project authorized: %+v", status)
	}

	// An administrator's removal of a person is durable in the hub's own log,
	// whatever the grant file still says.
	lifecycle := person["admin"].ListHubLifecycle("alpha")
	if lifecycle.State != desktop.Completed {
		t.Fatalf("lifecycle: %+v", lifecycle)
	}
	if result := person["admin"].PostHubLifecycle(desktop.HubLifecycleCommandRequest{Project: "alpha", ID: "remove-viewer", Expected: lifecycle.Head, Kind: "remove-user", Subject: "viewer", Reason: "left the team"}); result.State != desktop.Completed {
		t.Fatalf("remove the viewer: %+v", result)
	}
	if result := person["viewer"].ListHubReviews("alpha"); result.State != desktop.PermissionDenied {
		t.Fatalf("a removed viewer still reads the project: %+v", result)
	}

	// An expired session is refused by the window itself: nothing is sent, and
	// nothing refreshes or retries it. Signing in again is a deliberate act.
	short := window(t.TempDir())
	if result := signIn(short, "analyst", 2*time.Second); result.State != desktop.Completed || !result.Authenticated {
		t.Fatalf("short sign-in: %+v", result)
	}
	time.Sleep(2500 * time.Millisecond)
	before, exchanged := sent.total(), exchanges.Load()
	if result := short.PostHubReview(desktop.HubReviewCommandRequest{Project: "alpha", ID: "expired-note", Expected: 0, Kind: "comment", Evidence: evidence.Digest, Text: "after expiry"}); result.State != desktop.PermissionDenied {
		t.Fatalf("an expired session posted: %+v", result)
	}
	if result := short.ListHubReviews("alpha"); result.State != desktop.PermissionDenied {
		t.Fatalf("an expired session read: %+v", result)
	}
	if status := short.HubStatus(); status.Authenticated {
		t.Fatalf("an expired session is shown as signed in: %+v", status)
	}
	if got := sent.total() - before; got != 0 || exchanges.Load() != exchanged {
		t.Fatalf("an expired session sent %d requests to the hub and %d to the identity provider", got, exchanges.Load()-exchanged)
	}
	if result := signIn(short, "analyst", 5*time.Minute); result.State != desktop.Completed || !result.Authenticated {
		t.Fatalf("a deliberate new sign-in: %+v", result)
	}

	// Offline work a person retained before their removal comes back with
	// their window, and it regains no authority. The window restarts the way
	// a new process does, with its license selection restored and no hub
	// connection: reconciling needs an explicit connection, then a new
	// sign-in, and the hub still refuses the removed person.
	analystState := t.TempDir()
	restart := func() *desktop.App {
		return desktop.NewWithOperationSelection(silentChooser{}, filepath.Join(analystState, "recent.json"), filepath.Join(analystState, "filters.json"),
			filepath.Join(analystState, "session.json"), filepath.Join(analystState, "drafts.json"), filepath.Join(analystState, "operations.json"))
	}
	offline := restart()
	if result := offline.SelectOperationPolicy(testlicense.New(t)); result.State != desktop.Completed {
		t.Fatal(result)
	}
	if result := offline.SelectHubConfig(config); result.State != desktop.Completed {
		t.Fatal(result)
	}
	signIn(offline, "analyst", 5*time.Minute)
	tips := offline.ListHubLifecycle("alpha")
	draft := offline.SaveHubOfflineDraft(desktop.HubOfflineDraftRequest{Workspace: analystState, Project: "alpha", Resource: "evidence",
		ParentTips: []string{}, LocalPath: filepath.Join(analystState, "evidence.bin"), ExpectedHead: tips.Head, Note: "revised offline"})
	if draft.State != desktop.Completed {
		t.Fatalf("retain the offline revision: %+v", draft)
	}
	if result := person["admin"].PostHubLifecycle(desktop.HubLifecycleCommandRequest{Project: "alpha", ID: "remove-analyst", Expected: person["admin"].ListHubLifecycle("alpha").Head, Kind: "remove-user", Subject: "analyst", Reason: "rotated off the project"}); result.State != desktop.Completed {
		t.Fatalf("remove the analyst: %+v", result)
	}
	restarted := restart()
	if status := restarted.OperationStatus(); !status.Selected {
		t.Fatalf("the restarted window lost its license selection: %+v", status)
	}
	if retained := restarted.EditorDrafts(); retained.State != desktop.Completed || len(retained.Drafts) != 1 || retained.Drafts[0].Kind != "hub-revision" {
		t.Fatalf("the offline revision did not come back with the window: %+v", retained)
	}
	reconcile := desktop.HubLifecycleCommandRequest{Project: "alpha", ID: "offline-revision", Expected: tips.Head, Kind: "revision", Resource: "evidence", Artifact: evidence.Digest, Parents: []string{}, Reason: "revised offline"}
	before = sent.total()
	if result := restarted.ReconcileHubOfflineDraft(reconcile); result.State != desktop.Failed || !strings.Contains(result.Reason, "not connected") {
		t.Fatalf("a restored draft reconciled without a connection: %+v", result)
	}
	if got := sent.total() - before; got != 0 {
		t.Fatalf("restoring the window and reconciling unconnected sent %d requests", got)
	}
	if result := restarted.SelectHubConfig(config); result.State != desktop.Completed {
		t.Fatal(result)
	}
	if result := restarted.ConnectHub(); result.State != desktop.Completed {
		t.Fatal(result)
	}
	connected := sent.total()
	if result := restarted.ReconcileHubOfflineDraft(reconcile); result.State != desktop.PermissionDenied || !strings.Contains(result.Reason, "sign-in required") {
		t.Fatalf("a restored draft reconciled without a sign-in: %+v", result)
	}
	if got := sent.total() - connected; got != 0 {
		t.Fatalf("the unsigned reconciliation sent %d requests", got)
	}
	signIn(restarted, "analyst", 5*time.Minute)
	before = sent.of("analyst")
	if result := restarted.ReconcileHubOfflineDraft(reconcile); result.State != desktop.PermissionDenied {
		t.Fatalf("a removed person's retained offline work was accepted: %+v", result)
	}
	if got := sent.of("analyst") - before; got != 1 {
		t.Fatalf("the refused reconciliation reached the hub %d times, want exactly once", got)
	}
	if retained := restarted.EditorDrafts(); len(retained.Drafts) != 1 {
		t.Fatalf("a refused reconciliation discarded the retained work: %+v", retained)
	}
}
