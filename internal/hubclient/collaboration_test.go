package hubclient_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hubclient"
)

func TestCollaborationReviewLifecycleAndConflicts(t *testing.T) {
	ctx := context.Background()
	ca := newTestAuthority(t, "test-ca")
	serverCert, serverKey := ca.issue(t, "hub.example.com", true)
	clientCert, clientKey := ca.issue(t, "desktop-client", false)

	serverTLSCert, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(ca.pem)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}

	evidence := strings.Repeat("a", 64)
	release := strings.Repeat("b", 64)
	var reviewHead int
	var lifecycleHead int
	tips := map[string][]string{}
	posted := map[string]hubclient.ReviewEvent{}
	lifecyclePosted := map[string]hubclient.LifecycleEvent{}

	exportPayload := []byte("authorized-support-export")
	exportDigest := hex.EncodeToString(sha256Digest(exportPayload))
	deniedDigest := strings.Repeat("c", 64)

	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/v2/projects/icu-audit/history", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		events := make([]hubclient.ReviewEvent, 0, len(posted))
		for _, e := range posted {
			events = append(events, e)
		}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			var q hubclient.ReviewQuery
			if json.Unmarshal(body, &q) != nil || q.Schema != "readmit-hub-review-query/v1" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			filtered := []hubclient.ReviewEvent{}
			for _, e := range events {
				if e.Sequence > q.After && (q.Evidence == "" || e.Command.Evidence == q.Evidence) && strings.Contains(strings.ToLower(e.Command.Text), strings.ToLower(q.Text)) {
					filtered = append(filtered, e)
				}
			}
			events = filtered
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, hubclient.ReviewHistory{
			Schema: "readmit-hub-review-history/v2",
			Head:   reviewHead,
			Events: events,
		})
	})
	hubMux.HandleFunc("/v2/projects/icu-audit/notifications", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		events := []hubclient.ReviewEvent{}
		for _, e := range posted {
			if e.Command.Recipient == "reviewer@hospital.org" {
				events = append(events, e)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, hubclient.ReviewHistory{
			Schema: "readmit-hub-review-history/v2",
			Head:   reviewHead,
			Events: events,
		})
	})
	hubMux.HandleFunc("/v2/projects/icu-audit/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var cmd hubclient.ReviewCommand
		if json.Unmarshal(body, &cmd) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if existing, ok := posted[cmd.ID]; ok {
			same, _ := json.Marshal(existing.Command)
			got, _ := json.Marshal(cmd)
			if string(same) != string(got) {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.MarshalWrite(w, existing)
			return
		}
		if cmd.Expected != reviewHead {
			w.WriteHeader(http.StatusConflict)
			return
		}
		reviewHead++
		event := hubclient.ReviewEvent{
			Schema:   "readmit-hub-review-event/v1",
			Project:  "icu-audit",
			Sequence: reviewHead,
			Issuer:   "https://idp.example.com",
			Actor:    "physician@hospital.org",
			At:       time.Now().UTC().Format(time.RFC3339Nano),
			Command:  cmd,
		}
		posted[cmd.ID] = event
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, event)
	})
	hubMux.HandleFunc("/v2/projects/icu-audit/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			copied := map[string][]string{}
			for k, v := range tips {
				copied[k] = append([]string{}, v...)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.MarshalWrite(w, hubclient.LifecycleHistory{
				Schema:  "readmit-hub-lifecycle-history/v1",
				Head:    lifecycleHead,
				Events:  nil,
				Tips:    copied,
				Warning: "Downloaded copies remain under local custody and cannot be revoked.",
			})
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var cmd hubclient.LifecycleCommand
		if json.Unmarshal(body, &cmd) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if existing, ok := lifecyclePosted[cmd.ID]; ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.MarshalWrite(w, existing)
			return
		}
		if cmd.Expected != lifecycleHead {
			w.WriteHeader(http.StatusConflict)
			return
		}
		lifecycleHead++
		event := hubclient.LifecycleEvent{
			Schema:   "readmit-hub-lifecycle-event/v1",
			Project:  "icu-audit",
			Sequence: lifecycleHead,
			Issuer:   "https://idp.example.com",
			Actor:    "physician@hospital.org",
			At:       time.Now().UTC().Format(time.RFC3339Nano),
			Command:  cmd,
		}
		lifecyclePosted[cmd.ID] = event
		switch cmd.Kind {
		case "revision":
			current := tips[cmd.Resource]
			current = append([]string{}, current...)
			for _, p := range cmd.Parents {
				filtered := current[:0]
				for _, id := range current {
					if id != p {
						filtered = append(filtered, id)
					}
				}
				current = filtered
			}
			current = append(current, cmd.ID)
			tips[cmd.Resource] = current
		case "resolve":
			tips[cmd.Resource] = []string{cmd.ID}
		case "audit-export":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", `attachment; filename="audit.json"`)
			w.WriteHeader(http.StatusCreated)
			_ = json.MarshalWrite(w, hubclient.AuditExport{
				Schema:     "readmit-hub-audit/v1",
				Project:    "icu-audit",
				Lifecycle:  []hubclient.LifecycleEvent{event},
				ReviewHead: reviewHead,
				Reviews:    nil,
				Warning:    "Downloaded copies remain under local custody and cannot be revoked.",
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, event)
	})
	hubMux.HandleFunc("/v2/projects/icu-audit/exports/"+exportDigest, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Readmit-Custody-Warning", "Downloaded copies remain under local custody and cannot be revoked.")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(exportPayload)
	})
	hubMux.HandleFunc("/v2/projects/icu-audit/exports/"+deniedDigest, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	server := httptest.NewUnstartedServer(hubMux)
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	_ = os.WriteFile(caPath, ca.pem, 0600)
	_ = os.WriteFile(certPath, clientCert, 0600)
	keyProviderPath := createKeyProvider(t, clientKey)

	cfg := hubclient.Config{
		Schema:      hubclient.Schema,
		Hub:         server.URL,
		CA:          caPath,
		Certificate: certPath,
		Key:         hubclient.KeyReference{Command: keyProviderPath},
		IdP: hubclient.IdPConfig{
			Issuer:            "https://idp.example.com",
			ClientID:          "client-1",
			Audience:          "hub-aud",
			AuthorizeEndpoint: "https://idp.example.com/auth",
			TokenEndpoint:     "https://idp.example.com/token",
			Scopes:            []string{"evidence.read", "evidence.write", "approval", "admin", "export"},
		},
		Projects: []string{"icu-audit"},
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tok := issueTestToken(t, rsaKey, "key-1", cfg.IdP.Issuer, "physician@hospital.org", cfg.IdP.Audience, cfg.IdP.ClientID,
		[]string{"evidence.read", "evidence.write", "approval", "admin", "export"}, now.Unix(), now.Unix()+1800)
	session, err := hubclient.ValidateAccessToken(tok, cfg.IdP, now)
	if err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(ctx, cfg, session)
	if err != nil {
		t.Fatal(err)
	}

	comment := hubclient.ReviewCommand{
		Schema: "readmit-hub-review-command/v1", ID: "comment-1", Expected: 0, Kind: "comment",
		Evidence: evidence, Text: "Investigate synthetic mismatch", Recipient: "reviewer@hospital.org",
	}
	event, replay, err := client.PostReview(ctx, "icu-audit", comment)
	if err != nil || replay || event.Actor != "physician@hospital.org" || event.Sequence != 1 {
		t.Fatalf("PostReview comment: event=%+v replay=%v err=%v", event, replay, err)
	}
	_, replay, err = client.PostReview(ctx, "icu-audit", comment)
	if err != nil || !replay {
		t.Fatalf("safe retry should replay: replay=%v err=%v", replay, err)
	}
	stale := comment
	stale.ID = "comment-2"
	stale.Expected = 0
	if _, _, err := client.PostReview(ctx, "icu-audit", stale); !errors.Is(err, hubclient.ErrConflict) {
		t.Fatalf("stale head should conflict: %v", err)
	}

	request := hubclient.ReviewCommand{
		Schema: "readmit-hub-review-command/v1", ID: "review-1", Expected: 1, Kind: "review-request",
		Evidence: evidence, Recipient: "reviewer@hospital.org", Text: "Please review release", Release: release,
	}
	if _, _, err := client.PostReview(ctx, "icu-audit", request); err != nil {
		t.Fatalf("review-request: %v", err)
	}

	history, err := client.ListHistory(ctx, "icu-audit")
	if err != nil || history.Head != 2 || len(history.Events) != 2 {
		t.Fatalf("ListHistory: %+v err=%v", history, err)
	}
	filtered, err := client.SearchHistory(ctx, "icu-audit", hubclient.ReviewQuery{
		Schema: "readmit-hub-review-query/v1", After: 0, Text: "release", Evidence: evidence,
	})
	if err != nil || len(filtered.Events) != 1 || filtered.Events[0].Command.ID != "review-1" {
		t.Fatalf("SearchHistory: %+v err=%v", filtered, err)
	}
	notes, err := client.ListNotifications(ctx, "icu-audit")
	if err != nil || len(notes.Events) != 2 {
		t.Fatalf("ListNotifications: %+v err=%v", notes, err)
	}

	life, err := client.GetLifecycle(ctx, "icu-audit")
	if err != nil || life.Head != 0 || !strings.Contains(life.Warning, "local custody") {
		t.Fatalf("GetLifecycle: %+v err=%v", life, err)
	}
	rev1 := hubclient.LifecycleCommand{
		Schema: "readmit-hub-lifecycle-command/v1", ID: "edit-one", Expected: 0, Kind: "revision",
		Resource: "case-one", Artifact: evidence, Parents: []string{}, Reason: "offline branch a",
	}
	if _, err := client.PostLifecycle(ctx, "icu-audit", rev1); err != nil {
		t.Fatalf("revision one: %v", err)
	}
	life, err = client.GetLifecycle(ctx, "icu-audit")
	if err != nil {
		t.Fatal(err)
	}
	branchA := hubclient.LifecycleCommand{
		Schema: "readmit-hub-lifecycle-command/v1", ID: "edit-branch-a", Expected: life.Head, Kind: "revision",
		Resource: "case-one", Artifact: release, Parents: []string{"edit-one"}, Reason: "concurrent offline edit a",
	}
	if _, err := client.PostLifecycle(ctx, "icu-audit", branchA); err != nil {
		t.Fatalf("branch revision a: %v", err)
	}
	life, err = client.GetLifecycle(ctx, "icu-audit")
	if err != nil {
		t.Fatal(err)
	}
	branchB := hubclient.LifecycleCommand{
		Schema: "readmit-hub-lifecycle-command/v1", ID: "edit-branch-b", Expected: life.Head, Kind: "revision",
		Resource: "case-one", Artifact: evidence, Parents: []string{"edit-one"}, Reason: "concurrent offline edit b",
	}
	if _, err := client.PostLifecycle(ctx, "icu-audit", branchB); err != nil {
		t.Fatalf("branch revision b: %v", err)
	}
	life, err = client.GetLifecycle(ctx, "icu-audit")
	if err != nil || len(life.Tips["case-one"]) < 2 {
		t.Fatalf("expected concurrent tips: %+v err=%v", life, err)
	}
	resolve := hubclient.LifecycleCommand{
		Schema: "readmit-hub-lifecycle-command/v1", ID: "resolve-1", Expected: life.Head, Kind: "resolve",
		Resource: "case-one", Artifact: evidence, Parents: append([]string{}, life.Tips["case-one"]...), Reason: "keep both as new revision",
	}
	if _, err := client.PostLifecycle(ctx, "icu-audit", resolve); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	removal := hubclient.LifecycleCommand{
		Schema: "readmit-hub-lifecycle-command/v1", ID: "remove-1", Expected: lifecycleHead, Kind: "remove-user",
		Subject: "analyst@hospital.org", Reason: "role removed mid-request",
	}
	if _, err := client.PostLifecycle(ctx, "icu-audit", removal); err != nil {
		t.Fatalf("remove-user: %v", err)
	}
	auditCmd := hubclient.LifecycleCommand{
		Schema: "readmit-hub-lifecycle-command/v1", ID: "audit-1", Expected: lifecycleHead, Kind: "audit-export",
		Reason: "export decision history",
	}
	audit, err := client.PostLifecycle(ctx, "icu-audit", auditCmd)
	if err != nil || audit.Audit == nil || !strings.Contains(audit.Audit.Warning, "local custody") {
		t.Fatalf("audit-export: %+v err=%v", audit, err)
	}

	dest := filepath.Join(dir, "export.bin")
	transfer, err := client.DownloadExport(ctx, "icu-audit", exportDigest, dest)
	if err != nil || transfer.State != "completed" || !strings.Contains(transfer.Warning, "local custody") {
		t.Fatalf("DownloadExport: %+v err=%v", transfer, err)
	}

	roTok := issueTestToken(t, rsaKey, "key-1", cfg.IdP.Issuer, "viewer@hospital.org", cfg.IdP.Audience, cfg.IdP.ClientID,
		[]string{"evidence.read"}, now.Unix(), now.Unix()+1800)
	roSession, err := hubclient.ValidateAccessToken(roTok, cfg.IdP, now)
	if err != nil {
		t.Fatal(err)
	}
	roClient, err := hubclient.New(ctx, cfg, roSession)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := roClient.DownloadExport(ctx, "icu-audit", deniedDigest, filepath.Join(dir, "denied.bin")); !errors.Is(err, hubclient.ErrAccessDenied) {
		t.Fatalf("unauthorized export should fail on backend: %v", err)
	}
}

func sha256Digest(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// TestCollaborationSupportCommandContract pins the sharing workflow's wire
// shape: the support kinds ride the v2 review command, and the client reports
// a hub shape refusal as an explicit error rather than a bare status.
func TestCollaborationSupportCommandContract(t *testing.T) {
	ctx := context.Background()
	ca := newTestAuthority(t, "test-ca")
	serverCert, serverKey := ca.issue(t, "hub.example.com", true)
	clientCert, clientKey := ca.issue(t, "desktop-client", false)

	serverTLSCert, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(ca.pem)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}

	var postedSchema string
	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/v2/projects/icu-audit/reviews", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8192))
		var cmd hubclient.ReviewCommand
		if json.Unmarshal(body, &cmd) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		postedSchema = cmd.Schema
		if cmd.Schema != "readmit-hub-review-command/v2" || cmd.Kind != "support-policy" || cmd.Text != "support" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.MarshalWrite(w, hubclient.ReviewEvent{
			Schema: "readmit-hub-review-event/v2", Project: "icu-audit", Sequence: 1,
			Issuer: "https://idp.example.com", Actor: "physician@hospital.org",
			At: time.Now().UTC().Format(time.RFC3339Nano), Command: cmd,
		})
	})

	server := httptest.NewUnstartedServer(hubMux)
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	_ = os.WriteFile(caPath, ca.pem, 0600)
	_ = os.WriteFile(certPath, clientCert, 0600)
	keyProviderPath := createKeyProvider(t, clientKey)

	cfg := hubclient.Config{
		Schema:      hubclient.Schema,
		Hub:         server.URL,
		CA:          caPath,
		Certificate: certPath,
		Key:         hubclient.KeyReference{Command: keyProviderPath},
		IdP: hubclient.IdPConfig{
			Issuer:            "https://idp.example.com",
			ClientID:          "client-1",
			Audience:          "hub-aud",
			AuthorizeEndpoint: "https://idp.example.com/auth",
			TokenEndpoint:     "https://idp.example.com/token",
			Scopes:            []string{"evidence.read", "evidence.write", "approval", "admin", "export"},
		},
		Projects: []string{"icu-audit"},
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tok := issueTestToken(t, rsaKey, "key-1", cfg.IdP.Issuer, "physician@hospital.org", cfg.IdP.Audience, cfg.IdP.ClientID,
		cfg.IdP.Scopes, now.Unix(), now.Unix()+1800)
	session, err := hubclient.ValidateAccessToken(tok, cfg.IdP, now)
	if err != nil {
		t.Fatal(err)
	}
	client, err := hubclient.New(ctx, cfg, session)
	if err != nil {
		t.Fatal(err)
	}

	policy := hubclient.ReviewCommand{
		Schema: "readmit-hub-review-command/v2", ID: "policy-1", Expected: 0, Kind: "support-policy",
		Evidence: strings.Repeat("a", 64), Text: "support",
	}
	event, replay, err := client.PostReview(ctx, "icu-audit", policy)
	if err != nil || replay || event.Schema != "readmit-hub-review-event/v2" || event.Actor != "physician@hospital.org" {
		t.Fatalf("support-policy post: event=%+v replay=%v err=%v", event, replay, err)
	}
	if postedSchema != "readmit-hub-review-command/v2" {
		t.Fatalf("support kinds must ride the v2 command schema, hub saw %q", postedSchema)
	}

	reshaped := policy
	reshaped.ID = "policy-2"
	reshaped.Text = "not the support text"
	if _, _, err := client.PostReview(ctx, "icu-audit", reshaped); err == nil || !strings.Contains(err.Error(), "shape rejected") {
		t.Fatalf("hub shape refusal should be reported explicitly: %v", err)
	}
}
