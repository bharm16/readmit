package desktop_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/sharing"
	"github.com/bharm16/readmit/internal/testlicense"
)

// hubAuthFixture is one signed-in desktop session against a running TLS hub
// stub: the configured application, the directory holding its state files and
// the host dialog it answers through.
type hubAuthFixture struct {
	app    *desktop.App
	dir    string
	dialog *chooser
}

// newAuthenticatedHubApp starts mux behind a mutual-TLS server, writes a
// readmit-hub-client/v1 configuration trusting it, and drives the PKCE
// loopback to a signed-in session for subject. Every desktop hub journey test
// shares this bootstrap so a session change cannot fix one test and stale
// another.
func newAuthenticatedHubApp(t *testing.T, mux *http.ServeMux, subject string, scopes []string) hubAuthFixture {
	t.Helper()
	tok := signedHubAccessToken(t, subject, scopes)
	fixture := newConnectedHubApp(t, mux, subject, scopes, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{"access_token": tok, "token_type": "Bearer", "expires_in": 1800})
	})
	app := fixture.app
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
		_, _ = http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?code=auth-code-collab&state=%s", auth.Port, stateVal))
	}()
	if res := app.CompleteHubAuth("", ""); res.State != desktop.Completed || !res.Authenticated {
		t.Fatalf("CompleteHubAuth: %+v", res)
	}
	return fixture
}

// signedHubAccessToken is the RFC 9068 access token the test IdP issues for
// subject, valid for half an hour.
func signedHubAccessToken(t *testing.T, subject string, scopes []string) string {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	return issueDesktopTestToken(t, rsaKey, "key-1", "https://idp.hospital.org", subject, "hub-aud", "desktop-app",
		scopes, now.Unix(), now.Unix()+1800)
}

// newConnectedHubApp is that bootstrap up to the sign-in: the application is
// connected to mux over mutual TLS and signed in to nothing, and its
// configured IdP token endpoint is answered by token.
func newConnectedHubApp(t *testing.T, mux *http.ServeMux, subject string, scopes []string, token http.HandlerFunc) hubAuthFixture {
	t.Helper()
	ca := newHubTestAuthority(t, "customer-hub-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, subject, false)

	serverPair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(ca.pem)
	server := httptest.NewUnstartedServer(mux)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	idpMux := http.NewServeMux()
	idpMux.HandleFunc("/oauth/token", token)
	idpServer := httptest.NewServer(idpMux)
	t.Cleanup(idpServer.Close)

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	_ = os.WriteFile(caPath, ca.pem, 0600)
	_ = os.WriteFile(certPath, clientCert, 0600)
	keyProvider := createDesktopKeyProvider(t, clientKey)
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
	_ = os.WriteFile(cfgPath, []byte(cfgJSON), 0600)

	dialog := &chooser{folder: dir}
	app := desktop.New(dialog, filepath.Join(dir, "recent.json"), filepath.Join(dir, "filters.json"),
		filepath.Join(dir, "session.json"), filepath.Join(dir, "drafts.json"))
	if res := app.SelectOperationPolicy(testlicense.New(t)); res.State != desktop.Completed {
		t.Fatalf("SelectOperationPolicy: %+v", res)
	}
	if res := app.SelectHubConfig(cfgPath); res.State != desktop.Completed {
		t.Fatalf("SelectHubConfig: %+v", res)
	}
	if res := app.ConnectHub(); res.State != desktop.Completed || !res.Connected {
		t.Fatalf("ConnectHub: %+v", res)
	}
	return hubAuthFixture{app: app, dir: dir, dialog: dialog}
}

func TestDesktopHubCollaborationConflictAndAdmin(t *testing.T) {
	evidence := strings.Repeat("a", 64)
	var reviewHead int
	var lifeHead int
	var postedSchema string
	tips := map[string][]string{}

	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/v1/projects/cardio-study/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{
			"schema": "readmit-hub-lifecycle-history/v1", "head": lifeHead, "events": []any{},
			"tips": tips, "warning": "Downloaded copies remain under local custody and cannot be revoked.",
		})
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{
			"schema": "readmit-hub-review-history/v2", "head": reviewHead,
			"events": []map[string]any{{
				"schema": "readmit-hub-review-event/v1", "project": "cardio-study", "sequence": 1,
				"issuer": "https://idp.hospital.org", "actor": "doctor@hospital.org", "at": "2026-09-21T12:00:00Z",
				"command": map[string]any{
					"schema": "readmit-hub-review-command/v1", "id": "assign-1", "expected": 0, "kind": "assignment",
					"evidence": evidence, "parent": "", "recipient": "reviewer@hospital.org",
					"text": "Assigned for review", "release": "",
				},
			}},
		})
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/notifications", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{"schema": "readmit-hub-review-history/v2", "head": reviewHead, "events": []any{}})
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/reviews", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var cmd map[string]any
		if json.Unmarshal(body, &cmd) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		expected := int(cmd["expected"].(float64))
		if expected != reviewHead {
			w.WriteHeader(http.StatusConflict)
			return
		}
		reviewHead++
		postedSchema, _ = cmd["schema"].(string)
		eventSchema := "readmit-hub-review-event/v1"
		if strings.HasPrefix(postedSchema, "readmit-hub-review-command/v2") {
			eventSchema = "readmit-hub-review-event/v2"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, map[string]any{
			"schema": eventSchema, "project": "cardio-study", "sequence": reviewHead,
			"issuer": "https://idp.hospital.org", "actor": "doctor@hospital.org", "at": time.Now().UTC().Format(time.RFC3339Nano),
			"command": cmd,
		})
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.MarshalWrite(w, map[string]any{
				"schema": "readmit-hub-lifecycle-history/v1", "head": lifeHead, "events": []any{},
				"tips": tips, "warning": "Downloaded copies remain under local custody and cannot be revoked.",
			})
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var cmd map[string]any
		if json.Unmarshal(body, &cmd) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		expected := int(cmd["expected"].(float64))
		if expected != lifeHead {
			w.WriteHeader(http.StatusConflict)
			return
		}
		lifeHead++
		id, _ := cmd["id"].(string)
		kind, _ := cmd["kind"].(string)
		resource, _ := cmd["resource"].(string)
		if kind == "revision" {
			tips[resource] = append(tips[resource], id)
		}
		if kind == "resolve" {
			tips[resource] = []string{id}
		}
		if kind == "audit-export" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.MarshalWrite(w, map[string]any{
				"schema": "readmit-hub-audit/v1", "project": "cardio-study",
				"lifecycle": []any{}, "review_head": reviewHead, "reviews": []any{},
				"warning": "Downloaded copies remain under local custody and cannot be revoked.",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, map[string]any{
			"schema": "readmit-hub-lifecycle-event/v1", "project": "cardio-study", "sequence": lifeHead,
			"issuer": "https://idp.hospital.org", "actor": "doctor@hospital.org", "at": time.Now().UTC().Format(time.RFC3339Nano),
			"command": cmd,
		})
	})

	fx := newAuthenticatedHubApp(t, hubMux, "doctor@hospital.org",
		[]string{"evidence.read", "evidence.write", "approval", "admin", "export"})
	app := fx.app
	dir := fx.dir

	history := app.ListHubReviews("cardio-study")
	if history.State != desktop.Completed || len(history.Events) != 1 || history.Events[0].Actor != "doctor@hospital.org" {
		t.Fatalf("ListHubReviews: %+v", history)
	}
	if !strings.Contains(history.Warning, "local custody") {
		t.Fatalf("expected custody warning: %+v", history)
	}

	posted := app.PostHubReview(desktop.HubReviewCommandRequest{
		Project: "cardio-study", ID: "comment-1", Expected: 0, Kind: "comment",
		Evidence: evidence, Recipient: "reviewer@hospital.org", Text: "Evidence-linked comment",
	})
	if posted.State != desktop.Completed || len(posted.Events) == 0 || posted.Events[0].Actor != "doctor@hospital.org" {
		t.Fatalf("PostHubReview: %+v", posted)
	}
	if postedSchema != "readmit-hub-review-command/v1" {
		t.Fatalf("comment should ride the v1 command schema, got %q", postedSchema)
	}
	stale := app.PostHubReview(desktop.HubReviewCommandRequest{
		Project: "cardio-study", ID: "comment-2", Expected: 0, Kind: "comment",
		Evidence: evidence, Recipient: "reviewer@hospital.org", Text: "stale head",
	})
	if stale.State != desktop.Failed || !strings.Contains(stale.Reason, "conflict") {
		t.Fatalf("stale review head should fail: %+v", stale)
	}

	// The support kinds are the #260 sharing workflow and must ride the v2
	// command contract the hub's sharing routes require; the panel's dedicated
	// privacy-review UI for them lands with #260.
	support := app.PostHubReview(desktop.HubReviewCommandRequest{
		Project: "cardio-study", ID: "support-1", Expected: reviewHead, Kind: "support-policy",
		Evidence: evidence, Text: "support",
	})
	if support.State != desktop.Completed || len(support.Events) == 0 {
		t.Fatalf("PostHubReview support-policy: %+v", support)
	}
	if postedSchema != "readmit-hub-review-command/v2" {
		t.Fatalf("support-policy should ride the v2 command schema, got %q", postedSchema)
	}
	if support.Events[0].Schema != "readmit-hub-review-event/v2" {
		t.Fatalf("support event should carry the v2 event schema: %+v", support.Events[0])
	}
	unknown := app.PostHubReview(desktop.HubReviewCommandRequest{
		Project: "cardio-study", ID: "grant-1", Expected: reviewHead, Kind: "grant-everything",
		Evidence: evidence, Text: "unsupported kind",
	})
	if unknown.State != desktop.Failed || !strings.Contains(unknown.Reason, "unsupported review kind") {
		t.Fatalf("unknown review kind should be refused before the hub: %+v", unknown)
	}

	life := app.ListHubLifecycle("cardio-study")
	if life.State != desktop.Completed || !strings.Contains(life.Warning, "local custody") {
		t.Fatalf("ListHubLifecycle: %+v", life)
	}
	rev := app.PostHubLifecycle(desktop.HubLifecycleCommandRequest{
		Project: "cardio-study", ID: "edit-one", Expected: 0, Kind: "revision",
		Resource: "case-one", Artifact: evidence, Parents: []string{}, Reason: "offline draft reconnect",
	})
	if rev.State != desktop.Completed || rev.Event == nil || rev.Event.Actor != "doctor@hospital.org" {
		t.Fatalf("PostHubLifecycle revision: %+v", rev)
	}
	life = app.ListHubLifecycle("cardio-study")
	if life.State != desktop.Completed || len(life.Tips["case-one"]) != 1 {
		t.Fatalf("tips after revision: %+v", life)
	}
	summary := desktop.FormatHubConflictSummary("case-one", life.Tips["case-one"])
	if !strings.Contains(summary, "unresolved") {
		t.Fatalf("conflict summary: %s", summary)
	}

	removed := app.PostHubLifecycle(desktop.HubLifecycleCommandRequest{
		Project: "cardio-study", ID: "remove-1", Expected: lifeHead, Kind: "remove-user",
		Subject: "analyst@hospital.org", Reason: "role removed mid-request",
	})
	if removed.State != desktop.Completed {
		t.Fatalf("remove-user: %+v", removed)
	}
	audit := app.PostHubLifecycle(desktop.HubLifecycleCommandRequest{
		Project: "cardio-study", ID: "audit-1", Expected: lifeHead, Kind: "audit-export",
		Reason: "export decision history",
	})
	if audit.State != desktop.Completed || audit.Audit == nil || !strings.Contains(audit.Audit.Warning, "local custody") {
		t.Fatalf("audit-export: %+v", audit)
	}

	custody := app.ExplainHubCustody()
	if custody.State != desktop.Completed || !strings.Contains(custody.Reason, "cannot be revoked") {
		t.Fatalf("ExplainHubCustody: %+v", custody)
	}

	workspace := filepath.Join(dir, "workspace")
	_ = os.MkdirAll(workspace, 0700)
	drafts := app.SaveHubOfflineDraft(desktop.HubOfflineDraftRequest{
		Workspace: workspace, Project: "cardio-study", Resource: "case-one",
		ParentTips: []string{"edit-one"}, LocalPath: filepath.Join(workspace, "offline.bin"),
		ExpectedHead: lifeHead, Note: "retained offline branch",
	})
	if drafts.State != desktop.Completed || len(drafts.Drafts) != 1 || drafts.Drafts[0].Kind != "hub-revision" {
		t.Fatalf("SaveHubOfflineDraft: %+v", drafts)
	}
}

// TestDesktopHubSupportSharingJourney drives the #260 sharing journey from the
// hub panel above the privacy screens: the sharing policy is announced by its
// exact bytes, a published value-free summary is requested for team review by
// its support.json member, and the asked reviewer approves the request naming
// the same bytes — with the refusals the journey promises before anything is
// sent and the privacy panels' local approval inputs untouched.
func TestDesktopHubSupportSharingJourney(t *testing.T) {
	policy := sharing.Policy{Schema: sharing.PolicySchema, Support: true,
		Destinations: []string{"customer-hub-download", "local-file"}, MaxBytes: 65536}
	policyBytes, err := json.Marshal(policy, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	policyDigest := sharing.Digest(policyBytes)
	summary := sharing.Summary{Schema: sharing.Schema, SourceKind: "retained-packet",
		SourceIdentity: strings.Repeat("b", 64), InputCommitment: strings.Repeat("c", 64),
		SpecIdentity: strings.Repeat("d", 64), PolicyIdentity: policyDigest,
		Outcome: "pass", ExternalEquivalence: "declined", Scope: sharing.Scope}
	summaryBytes, err := json.Marshal(summary, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	policyEntry := filepath.Join(workspace, "sharing-policy.json")
	if err := os.WriteFile(policyEntry, policyBytes, 0600); err != nil {
		t.Fatal(err)
	}
	bundleEntry := filepath.Join(workspace, "support-bundle")
	if err := os.MkdirAll(filepath.Join(bundleEntry, "x"), 0700); err != nil {
		t.Fatal(err)
	}
	summaryFile := filepath.Join(bundleEntry, "support.json")
	if err := os.WriteFile(summaryFile, summaryBytes, 0600); err != nil {
		t.Fatal(err)
	}
	emptyEntry := filepath.Join(workspace, "empty-bundle")
	if err := os.MkdirAll(emptyEntry, 0700); err != nil {
		t.Fatal(err)
	}
	otherEntry := filepath.Join(workspace, "not-a-policy.json")
	if err := os.WriteFile(otherEntry, []byte(`{"schema":"something-else/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}

	var reviewHead int
	var uploaded map[string][]byte = map[string][]byte{}
	var events []map[string]any = []map[string]any{}
	var policyCmdID string
	var requestCmdID string

	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/v1/projects/cardio-study/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		digest := filepath.Base(r.URL.Path)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		uploaded[digest] = body
		w.WriteHeader(http.StatusCreated)
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{
			"schema": "readmit-hub-review-history/v2", "head": reviewHead, "events": events,
		})
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/reviews", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var cmd map[string]any
		if json.Unmarshal(body, &cmd) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if int(cmd["expected"].(float64)) != reviewHead || cmd["text"] != "support" || cmd["schema"] != "readmit-hub-review-command/v2" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		kind, _ := cmd["kind"].(string)
		evidence, _ := cmd["evidence"].(string)
		switch kind {
		case "support-policy":
			if cmd["release"] != "" || cmd["parent"] != "" || cmd["recipient"] != "" || string(uploaded[evidence]) != string(policyBytes) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			policyCmdID, _ = cmd["id"].(string)
		case "support-request":
			if cmd["release"] != policyDigest || cmd["parent"] != policyCmdID ||
				cmd["recipient"] == "" || string(uploaded[evidence]) != string(summaryBytes) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requestCmdID, _ = cmd["id"].(string)
		case "support-approval":
			if cmd["release"] != policyDigest || cmd["parent"] == "" ||
				cmd["parent"] != requestCmdID || cmd["recipient"] != "" ||
				evidence != sharing.Digest(summaryBytes) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reviewHead++
		event := map[string]any{
			"schema": "readmit-hub-review-event/v2", "project": "cardio-study", "sequence": reviewHead,
			"issuer": "https://idp.hospital.org", "actor": "author@hospital.org", "at": time.Now().UTC().Format(time.RFC3339Nano),
			"command": cmd,
		}
		events = append(events, event)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, event)
	})

	app := newAuthenticatedHubApp(t, hubMux, "author@hospital.org",
		[]string{"evidence.read", "evidence.write", "approval", "export"}).app

	// Refusals before anything is sent.
	unknown := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "sharing-policy.json",
		Kind: "grant-everything", ID: "sup-0",
	})
	if unknown.State != desktop.Failed || !strings.Contains(unknown.Reason, "support-policy, support-request or support-approval") {
		t.Fatalf("unknown sharing kind should be refused locally: %+v", unknown)
	}
	unannounced := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "support-bundle",
		Kind: "support-request", ID: "sup-1", Recipient: "reviewer@hospital.org",
	})
	if unannounced.State != desktop.Failed || !strings.Contains(unannounced.Reason, "no sharing policy in force") {
		t.Fatalf("a request before any policy in force should be refused: %+v", unannounced)
	}
	unrequested := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "support-bundle",
		Kind: "support-approval", ID: "sup-2",
	})
	if unrequested.State != desktop.Failed || !strings.Contains(unrequested.Reason, "no support request names this exact summary") {
		t.Fatalf("approval without a matching request should be refused: %+v", unrequested)
	}
	notPolicy := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "not-a-policy.json",
		Kind: "support-policy", ID: "sup-3",
	})
	if notPolicy.State != desktop.Failed || !strings.Contains(notPolicy.Reason, "not a sharing policy") {
		t.Fatalf("a non-policy entry should be refused before the hub: %+v", notPolicy)
	}
	noSummary := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "empty-bundle",
		Kind: "support-request", ID: "sup-4", Recipient: "reviewer@hospital.org",
	})
	if noSummary.State != desktop.Failed || !strings.Contains(noSummary.Reason, "publish it locally") {
		t.Fatalf("a bundle without support.json should be refused: %+v", noSummary)
	}

	announce := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "sharing-policy.json",
		Kind: "support-policy", ID: "sup-policy-1",
	})
	if announce.State != desktop.Completed || len(announce.Events) != 1 || announce.Events[0].Actor != "author@hospital.org" {
		t.Fatalf("support-policy: %+v", announce)
	}
	if string(uploaded[policyDigest]) != string(policyBytes) {
		t.Fatalf("the hub must hold the exact policy bytes")
	}

	request := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "support-bundle",
		Kind: "support-request", ID: "sup-request-1", Recipient: "reviewer@hospital.org",
	})
	if request.State != desktop.Completed || len(request.Events) != 1 {
		t.Fatalf("support-request: %+v", request)
	}
	if string(uploaded[sharing.Digest(summaryBytes)]) != string(summaryBytes) {
		t.Fatalf("the hub must hold the exact summary bytes")
	}

	approval := app.PostHubSupportReview(desktop.HubSupportReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "support-bundle",
		Kind: "support-approval", ID: "sup-approval-1",
	})
	if approval.State != desktop.Completed || len(approval.Events) != 1 {
		t.Fatalf("support-approval: %+v", approval)
	}
	if approval.Events[0].Parent != "sup-request-1" {
		t.Fatalf("approval should chain to the content-matched request: %+v", approval.Events[0])
	}
}

// TestDesktopHubReleaseReviewJourney drives the expectation-release journey
// from the suite panel's release surface: the released expectation is verified
// locally, uploaded, requested for review and approved by the exact content —
// with the refusals the journey promises before anything is sent.
func TestDesktopHubReleaseReviewJourney(t *testing.T) {
	// The release the journey posts, exactly as the expectation panel writes it.
	spec := []byte(`{"schema":"readmit-test/v1","name":"synthetic","input":{"case":"case","messages":["s0001-e000001"]},"target":"target.json","setup":{"initial_state":"operator-declared","reset_instructions":"Reset fixture"},"observation":{"boundary":"ack-contract"},"assertions":[{"id":"ack","operator":"ack_field_equals","message":"s0001-e000001","selector":"MSA-1","expected":{"field":{"state":"present","text":"AA"}}}]}`)
	review, err := expectation.Review("booking", spec, []profileversion.Version{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.Approve("booking", spec, []profileversion.Version{}, nil, review.Identity, "Local approver label", "synthetic rationale")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := release.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	releaseDigest := hex.EncodeToString(sum[:])

	workspace := t.TempDir()
	releaseEntry := filepath.Join(workspace, "booking-release.json")
	if err := os.WriteFile(releaseEntry, raw, 0600); err != nil {
		t.Fatal(err)
	}
	otherEntry := filepath.Join(workspace, "not-a-release.json")
	if err := os.WriteFile(otherEntry, []byte(`{"schema":"something-else/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}

	var reviewHead int
	var uploaded map[string][]byte = map[string][]byte{}
	var requestedID, requestedRelease, requestedEvidence, requestedRecipient string
	approveRefusal := "no review request names this exact release content"

	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	hubMux.HandleFunc("/v1/projects/cardio-study/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		digest := filepath.Base(r.URL.Path)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		uploaded[digest] = body
		w.WriteHeader(http.StatusCreated)
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/history", func(w http.ResponseWriter, r *http.Request) {
		events := []any{}
		if requestedID != "" {
			events = append(events, map[string]any{
				"schema": "readmit-hub-review-event/v1", "project": "cardio-study", "sequence": 1,
				"issuer": "https://idp.hospital.org", "actor": "author@hospital.org", "at": "2026-09-21T12:00:00Z",
				"command": map[string]any{
					"schema": "readmit-hub-review-command/v1", "id": requestedID, "expected": 0, "kind": "review-request",
					"evidence": requestedEvidence, "parent": "", "recipient": requestedRecipient,
					"text": "Review exact release", "release": requestedRelease,
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{
			"schema": "readmit-hub-review-history/v2", "head": reviewHead, "events": events,
		})
	})
	hubMux.HandleFunc("/v2/projects/cardio-study/reviews", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var cmd map[string]any
		if json.Unmarshal(body, &cmd) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if int(cmd["expected"].(float64)) != reviewHead {
			w.WriteHeader(http.StatusConflict)
			return
		}
		kind, _ := cmd["kind"].(string)
		if kind == "review-request" {
			if cmd["schema"] != "readmit-hub-review-command/v1" ||
				cmd["recipient"] == "" || cmd["parent"] != "" ||
				cmd["release"] != releaseDigest || cmd["evidence"] != releaseDigest {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if uploaded[releaseDigest] == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			requestedID, _ = cmd["id"].(string)
			requestedRelease, _ = cmd["release"].(string)
			requestedEvidence, _ = cmd["evidence"].(string)
			requestedRecipient, _ = cmd["recipient"].(string)
		}
		if kind == "approval" {
			if cmd["schema"] != "readmit-hub-review-command/v1" ||
				cmd["recipient"] != "" || cmd["parent"] == "" ||
				cmd["parent"] != requestedID || cmd["release"] != requestedRelease ||
				cmd["evidence"] != requestedEvidence {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		reviewHead++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, map[string]any{
			"schema": "readmit-hub-review-event/v1", "project": "cardio-study", "sequence": reviewHead,
			"issuer": "https://idp.hospital.org", "actor": "author@hospital.org", "at": time.Now().UTC().Format(time.RFC3339Nano),
			"command": cmd,
		})
	})

	app := newAuthenticatedHubApp(t, hubMux, "author@hospital.org",
		[]string{"evidence.read", "evidence.write", "approval"}).app

	// Refusals before anything is sent: an unknown kind, an entry that is not a
	// released expectation, and an approval with no outstanding request.
	unknown := app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "booking-release.json",
		Kind: "grant-everything", ID: "rel-0", Text: "unsupported kind",
	})
	if unknown.State != desktop.Failed || !strings.Contains(unknown.Reason, "review-request or approval") {
		t.Fatalf("unknown journey kind should be refused locally: %+v", unknown)
	}
	notRelease := app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "not-a-release.json",
		Kind: "review-request", ID: "rel-1", Recipient: "reviewer@hospital.org", Text: "not a release",
	})
	if notRelease.State != desktop.Failed || !strings.Contains(notRelease.Reason, "not a released expectation") {
		t.Fatalf("a non-release entry should be refused before the hub: %+v", notRelease)
	}
	unrequested := app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "booking-release.json",
		Kind: "approval", ID: "rel-2", Text: "approving without a request",
	})
	if unrequested.State != desktop.Failed || !strings.Contains(unrequested.Reason, approveRefusal) {
		t.Fatalf("approval without a matching request should be refused: %+v", unrequested)
	}
	unaddressed := app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "booking-release.json",
		Kind: "review-request", ID: "rel-3", Text: "nobody asked to review",
	})
	if unaddressed.State != desktop.Failed || !strings.Contains(unaddressed.Reason, "names the subject") {
		t.Fatalf("a review request without a recipient should be refused: %+v", unaddressed)
	}

	request := app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "booking-release.json",
		Kind: "review-request", ID: "rel-request-1", Recipient: "reviewer@hospital.org",
		Text: "Review the exact released expectations",
	})
	if request.State != desktop.Completed || len(request.Events) != 1 {
		t.Fatalf("PostHubReleaseReview request: %+v", request)
	}
	if request.Events[0].Actor != "author@hospital.org" || request.Events[0].CommandID != "rel-request-1" {
		t.Fatalf("request event should carry the session actor: %+v", request.Events[0])
	}
	if string(uploaded[releaseDigest]) != string(raw) {
		t.Fatalf("the hub must hold the exact reviewed bytes")
	}

	approval := app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{
		Project: "cardio-study", Workspace: workspace, Entry: "booking-release.json",
		Kind: "approval", ID: "rel-approval-1",
		Text: "Approved against the reviewed impact",
	})
	if approval.State != desktop.Completed || len(approval.Events) != 1 {
		t.Fatalf("PostHubReleaseReview approval: %+v", approval)
	}
	if approval.Events[0].Parent != "rel-request-1" {
		t.Fatalf("approval should chain to the content-matched request: %+v", approval.Events[0])
	}
}

// TestDesktopHubCollaborationRefusesWithoutSession pins the refusal the docs
// promise: no collaboration decision is recorded without an authenticated hub
// session, whatever the window's buttons offered.
func TestDesktopHubCollaborationRefusesWithoutSession(t *testing.T) {
	dir := t.TempDir()
	app := desktop.New(&chooser{folder: dir}, filepath.Join(dir, "recent.json"), filepath.Join(dir, "filters.json"),
		filepath.Join(dir, "session.json"), filepath.Join(dir, "drafts.json"))
	if res := app.SelectOperationPolicy(testlicense.New(t)); res.State != desktop.Completed {
		t.Fatalf("SelectOperationPolicy: %+v", res)
	}

	disconnected := app.PostHubReview(desktop.HubReviewCommandRequest{
		Project: "cardio-study", ID: "comment-1", Kind: "comment",
		Evidence: strings.Repeat("a", 64), Text: "no hub configured",
	})
	if disconnected.State != desktop.Failed || !strings.Contains(disconnected.Reason, "not connected") {
		t.Fatalf("review without a hub should fail: %+v", disconnected)
	}
	unknown := app.PostHubReview(desktop.HubReviewCommandRequest{
		Project: "cardio-study", ID: "grant-1", Kind: "grant-everything",
		Evidence: strings.Repeat("a", 64), Text: "unsupported kind",
	})
	if unknown.State != desktop.Failed || !strings.Contains(unknown.Reason, "unsupported review kind") {
		t.Fatalf("unknown kind should be refused without a hub too: %+v", unknown)
	}
}
