package hubclient_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hubclient"
)

type testAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestAuthority(t *testing.T, cn string) *testAuthority {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &testAuthority{
		cert: parsed,
		key:  k,
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}),
	}
}

func (a *testAuthority) issue(t *testing.T, cn string, server bool) (certPEM, keyPEM []byte) {
	t.Helper()
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.ExtKeyUsageClientAuth
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		usage = x509.ExtKeyUsageServerAuth
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
		tmpl.DNSNames = []string{"localhost", "hub.example.com"}
	}
	tmpl.ExtKeyUsage = []x509.ExtKeyUsage{usage}

	raw, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &leafKey.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	encKey, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encKey})
}

// createKeyProvider creates a test executable script that returns the key bytes.
func createKeyProvider(t *testing.T, keyBytes []byte) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "key-provider.sh")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyFile, keyBytes, 0600); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\ncat %q\n", keyFile)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

// TestConfigValidation covers configuration parsing, strict JSON decoding, and validation rules.
func TestConfigValidation(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	providerPath := filepath.Join(dir, "provider.sh")
	os.WriteFile(caPath, []byte("CA"), 0600)
	os.WriteFile(certPath, []byte("CERT"), 0600)
	os.WriteFile(providerPath, []byte("#!/bin/sh\n"), 0755)

	validJSON := fmt.Sprintf(`{
		"schema": "readmit-hub-client/v1",
		"hub": "https://hub.example.com:8443",
		"ca": %q,
		"certificate": %q,
		"key": {
			"command": %q,
			"arguments": ["--arg1"]
		},
		"idp": {
			"issuer": "https://idp.example.com",
			"client_id": "readmit-desktop",
			"audience": "readmit-hub",
			"authorize_endpoint": "https://idp.example.com/oauth/authorize",
			"token_endpoint": "https://idp.example.com/oauth/token",
			"scopes": ["evidence.read", "evidence.write"]
		},
		"projects": ["alpha-1", "beta-2"]
	}`, caPath, certPath, providerPath)

	cfg, err := hubclient.DecodeConfig([]byte(validJSON))
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if cfg.Hub != "https://hub.example.com:8443" {
		t.Fatalf("unexpected hub: %s", cfg.Hub)
	}
	if len(cfg.Projects) != 2 || cfg.Projects[0] != "alpha-1" {
		t.Fatalf("unexpected projects: %v", cfg.Projects)
	}

	// Schema mismatch
	badSchema := strings.Replace(validJSON, "readmit-hub-client/v1", "readmit-hub-client/v2", 1)
	if _, err := hubclient.DecodeConfig([]byte(badSchema)); err == nil {
		t.Fatal("expected error on unsupported schema")
	}

	// Unknown member rejected
	unknownMember := strings.Replace(validJSON, `"projects"`, `"extra": 123, "projects"`, 1)
	if _, err := hubclient.DecodeConfig([]byte(unknownMember)); err == nil {
		t.Fatal("expected error on unknown member")
	}

	// Missing member
	missingHub := strings.Replace(validJSON, `"hub": "https://hub.example.com:8443",`, "", 1)
	if _, err := hubclient.DecodeConfig([]byte(missingHub)); err == nil {
		t.Fatal("expected error on missing hub member")
	}

	// Invalid Hub URL
	badHubURL := strings.Replace(validJSON, "https://hub.example.com:8443", "http://hub.example.com", 1)
	if _, err := hubclient.DecodeConfig([]byte(badHubURL)); err == nil {
		t.Fatal("expected error on http hub URL")
	}

	// Relative CA path
	relCA := strings.Replace(validJSON, fmt.Sprintf("%q", caPath), `"./ca.pem"`, 1)
	if _, err := hubclient.DecodeConfig([]byte(relCA)); err == nil {
		t.Fatal("expected error on relative CA path")
	}

	// Invalid scope
	badScope := strings.Replace(validJSON, `"evidence.read"`, `"unsupported.scope"`, 1)
	if _, err := hubclient.DecodeConfig([]byte(badScope)); err == nil {
		t.Fatal("expected error on unsupported scope")
	}

	// Invalid project identifier
	badProject := strings.Replace(validJSON, `"alpha-1"`, `"alpha_1_invalid!"`, 1)
	if _, err := hubclient.DecodeConfig([]byte(badProject)); err == nil {
		t.Fatal("expected error on invalid project name")
	}

	// Duplicate project
	dupProject := strings.Replace(validJSON, `"beta-2"`, `"alpha-1"`, 1)
	if _, err := hubclient.DecodeConfig([]byte(dupProject)); err == nil {
		t.Fatal("expected error on duplicate project")
	}

	// Test ReadConfig from file
	cfgFile := filepath.Join(dir, "client.json")
	if err := os.WriteFile(cfgFile, []byte(validJSON), 0600); err != nil {
		t.Fatal(err)
	}
	fromFile, err := hubclient.ReadConfig(cfgFile)
	if err != nil {
		t.Fatalf("ReadConfig failed: %v", err)
	}
	if fromFile.Hub != cfg.Hub {
		t.Fatalf("ReadConfig returned different hub: %s vs %s", fromFile.Hub, cfg.Hub)
	}
}

// TestDiagnosePrerequisites tests actionable prerequisite diagnostics against live and failing endpoints.
func TestDiagnosePrerequisites(t *testing.T) {
	ctx := context.Background()
	ca := newTestAuthority(t, "customer-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, "user@customer", false)

	serverPair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(ca.pem)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}

	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	serverMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewUnstartedServer(serverMux)
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	os.WriteFile(caPath, ca.pem, 0600)
	os.WriteFile(certPath, clientCert, 0600)
	keyProviderPath := createKeyProvider(t, clientKey)

	cfg := hubclient.Config{
		Schema:      hubclient.Schema,
		Hub:         server.URL,
		CA:          caPath,
		Certificate: certPath,
		Key: hubclient.KeyReference{
			Command:   keyProviderPath,
			Arguments: nil,
		},
		IdP: hubclient.IdPConfig{
			Issuer:            "https://idp.customer.example",
			ClientID:          "client-1",
			Audience:          "hub-aud",
			AuthorizeEndpoint: "https://idp.customer.example/auth",
			TokenEndpoint:     "https://idp.customer.example/token",
			Scopes:            []string{"evidence.read"},
		},
		Projects: []string{"proj-1"},
	}

	// 1. All prerequisites pass
	diag := hubclient.DiagnosePrerequisites(ctx, cfg)
	if !diag.Passed {
		for _, c := range diag.Checks {
			if !c.Passed {
				t.Logf("check failed: %s: %s (%s)", c.Name, c.Message, c.Detail)
			}
		}
		t.Fatalf("expected all prerequisites to pass")
	}

	// 2. Client certificate / key mismatch
	otherCA := newTestAuthority(t, "other-ca")
	_, mismatchKey := otherCA.issue(t, "other-user", false)
	mismatchProvider := createKeyProvider(t, mismatchKey)

	badKeyCfg := cfg
	badKeyCfg.Key.Command = mismatchProvider
	diagMismatch := hubclient.DiagnosePrerequisites(ctx, badKeyCfg)
	if diagMismatch.Passed {
		t.Fatal("expected diagnostic failure on key mismatch")
	}
	foundKeyCheck := false
	for _, c := range diagMismatch.Checks {
		if c.Name == "key_pair_match" && !c.Passed {
			foundKeyCheck = true
		}
	}
	if !foundKeyCheck {
		t.Fatal("expected key_pair_match check to fail")
	}

	// 3. Unreadable CA file
	badCACfg := cfg
	badCACfg.CA = filepath.Join(dir, "nonexistent-ca.pem")
	diagNoCA := hubclient.DiagnosePrerequisites(ctx, badCACfg)
	if diagNoCA.Passed {
		t.Fatal("expected diagnostic failure on nonexistent CA")
	}

	// 4. Server probe failure (live/ready returns 500)
	failMux := http.NewServeMux()
	failMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	failMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	failServer := httptest.NewUnstartedServer(failMux)
	failServer.TLS = tlsConfig
	failServer.StartTLS()
	defer failServer.Close()

	failHubCfg := cfg
	failHubCfg.Hub = failServer.URL
	diagFailServer := hubclient.DiagnosePrerequisites(ctx, failHubCfg)
	if diagFailServer.Passed {
		t.Fatal("expected diagnostic failure on failing health probe")
	}
}

// helper to build signed RFC 9068 test token
func issueTestToken(t *testing.T, key *rsa.PrivateKey, kid, iss, sub, aud, clientID string, scopes []string, iat, exp int64) string {
	t.Helper()
	header := map[string]string{
		"alg": "RS256",
		"typ": "at+jwt",
		"kid": kid,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{
		"iss":       iss,
		"sub":       sub,
		"aud":       aud,
		"client_id": clientID,
		"jti":       "test-jti-1234",
		"iat":       iat,
		"exp":       exp,
		"scope":     strings.Join(scopes, " "),
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}

	h64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	c64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := h64 + "." + c64

	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatal(err)
	}
	s64 := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + s64
}

// TestPKCEAndAuthFlow verifies PKCE parameters and local loopback callback handling.
func TestPKCEAndAuthFlow(t *testing.T) {
	v, c, s, err := hubclient.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}
	if len(v) < 40 || len(c) < 40 || len(s) < 20 {
		t.Fatalf("unexpected PKCE string lengths: verifier=%d, challenge=%d, state=%d", len(v), len(c), len(s))
	}

	// Verify challenge is base64url(SHA256(verifier))
	h := sha256.Sum256([]byte(v))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	if c != expectedChallenge {
		t.Fatalf("challenge mismatch: expected %s, got %s", expectedChallenge, c)
	}

	cfg := hubclient.Config{
		Schema:      hubclient.Schema,
		Hub:         "https://hub.customer.example",
		CA:          "/tmp/ca.pem",
		Certificate: "/tmp/cert.pem",
		Key:         hubclient.KeyReference{Command: "/tmp/key.sh"},
		IdP: hubclient.IdPConfig{
			Issuer:            "https://idp.example.com",
			ClientID:          "readmit",
			Audience:          "hub",
			AuthorizeEndpoint: "https://idp.example.com/oauth/authorize",
			TokenEndpoint:     "https://idp.example.com/oauth/token",
			Scopes:            []string{"evidence.read", "evidence.write"},
		},
		Projects: []string{"proj-1"},
	}

	// 1. Simulate callback with state mismatch
	flowBadState, err := hubclient.StartAuthFlow(cfg)
	if err != nil {
		t.Fatalf("StartAuthFlow failed: %v", err)
	}
	defer flowBadState.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	badResp, err := client.Get(fmt.Sprintf("%s?code=abc&state=wrong-state", flowBadState.RedirectURI))
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 on bad state, got %d", badResp.StatusCode)
	}
	ctxErr, cancelErr := context.WithTimeout(context.Background(), time.Second)
	defer cancelErr()
	if _, err := flowBadState.WaitForCallback(ctxErr); err == nil {
		t.Fatal("expected WaitForCallback to return state mismatch error")
	}

	// 2. Simulate callback with IdP error
	flowErr, err := hubclient.StartAuthFlow(cfg)
	if err != nil {
		t.Fatalf("StartAuthFlow failed: %v", err)
	}
	defer flowErr.Close()

	errResp, err := client.Get(fmt.Sprintf("%s?error=access_denied", flowErr.RedirectURI))
	if err != nil {
		t.Fatal(err)
	}
	errResp.Body.Close()
	if errResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 on idp error, got %d", errResp.StatusCode)
	}
	ctxIdp, cancelIdp := context.WithTimeout(context.Background(), time.Second)
	defer cancelIdp()
	if _, err := flowErr.WaitForCallback(ctxIdp); err == nil {
		t.Fatal("expected WaitForCallback to return IdP error")
	}

	// 3. Now valid callback
	flowValid, err := hubclient.StartAuthFlow(cfg)
	if err != nil {
		t.Fatalf("StartAuthFlow failed: %v", err)
	}
	defer flowValid.Close()

	validResp, err := client.Get(fmt.Sprintf("%s?code=valid-auth-code&state=%s", flowValid.RedirectURI, flowValid.State))
	if err != nil {
		t.Fatal(err)
	}
	validResp.Body.Close()
	if validResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on valid callback, got %d", validResp.StatusCode)
	}

	ctxValid, cancelValid := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelValid()
	code, err := flowValid.WaitForCallback(ctxValid)
	if err != nil {
		t.Fatalf("WaitForCallback failed: %v", err)
	}
	if code != "valid-auth-code" {
		t.Fatalf("expected code 'valid-auth-code', got %q", code)
	}
}

// TestTokenValidationAndExchange tests RFC 9068 validation and code exchange.
func TestTokenValidationAndExchange(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	idpCfg := hubclient.IdPConfig{
		Issuer:   "https://idp.example.com",
		ClientID: "client-readmit",
		Audience: "customer-hub",
		Scopes:   []string{"evidence.read", "evidence.write"},
	}

	now := time.Now()
	nowSec := now.Unix()

	// 1. Valid token
	tok := issueTestToken(t, rsaKey, "key-1", idpCfg.Issuer, "user@hospital.org", idpCfg.Audience, idpCfg.ClientID, []string{"evidence.read", "evidence.write"}, nowSec, nowSec+1800)
	session, err := hubclient.ValidateAccessToken(tok, idpCfg, now)
	if err != nil {
		t.Fatalf("ValidateAccessToken failed: %v", err)
	}
	if session.Subject != "user@hospital.org" {
		t.Fatalf("unexpected subject: %s", session.Subject)
	}
	if !session.Allows("evidence.read") || !session.Allows("evidence.write") {
		t.Fatalf("expected allowed scopes")
	}
	if session.Allows("ownership") {
		t.Fatalf("did not expect ownership scope")
	}
	if session.IsExpired(now.Add(2*time.Hour)) != true {
		t.Fatalf("expected session to expire")
	}
	if !strings.HasPrefix(session.BearerHeader(), "Bearer ") {
		t.Fatalf("unexpected bearer header: %s", session.BearerHeader())
	}
	// Verify String masks token
	if strings.Contains(session.String(), tok) {
		t.Fatalf("Session.String leaked access token")
	}

	// 2. Expired token
	expiredTok := issueTestToken(t, rsaKey, "key-1", idpCfg.Issuer, "user@hospital.org", idpCfg.Audience, idpCfg.ClientID, []string{"evidence.read"}, nowSec-3600, nowSec-10)
	if _, err := hubclient.ValidateAccessToken(expiredTok, idpCfg, now); err == nil {
		t.Fatal("expected expired token to fail validation")
	}

	// 3. Issuer mismatch
	badIssTok := issueTestToken(t, rsaKey, "key-1", "https://evil.idp.com", "user@hospital.org", idpCfg.Audience, idpCfg.ClientID, []string{"evidence.read"}, nowSec, nowSec+1800)
	if _, err := hubclient.ValidateAccessToken(badIssTok, idpCfg, now); err == nil {
		t.Fatal("expected issuer mismatch to fail validation")
	}

	// 4. ClientID mismatch
	badClientTok := issueTestToken(t, rsaKey, "key-1", idpCfg.Issuer, "user@hospital.org", idpCfg.Audience, "other-client", []string{"evidence.read"}, nowSec, nowSec+1800)
	if _, err := hubclient.ValidateAccessToken(badClientTok, idpCfg, now); err == nil {
		t.Fatal("expected client mismatch to fail validation")
	}

	// 5. Test ExchangeCode with test IdP server
	idpMux := http.NewServeMux()
	idpMux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.FormValue("grant_type") != "authorization_code" || r.FormValue("code") != "auth-code-123" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := map[string]any{
			"access_token": tok,
			"token_type":   "Bearer",
			"expires_in":   1800,
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, resp)
	})
	idpServer := httptest.NewServer(idpMux)
	defer idpServer.Close()

	cfg := hubclient.Config{
		Schema: hubclient.Schema,
		IdP: hubclient.IdPConfig{
			Issuer:        idpCfg.Issuer,
			ClientID:      idpCfg.ClientID,
			Audience:      idpCfg.Audience,
			TokenEndpoint: idpServer.URL + "/oauth/token",
			Scopes:        idpCfg.Scopes,
		},
	}

	exchangedSession, err := hubclient.ExchangeCode(context.Background(), cfg, "auth-code-123", "verifier-xyz", "http://127.0.0.1:8000/callback")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if exchangedSession.Subject != "user@hospital.org" {
		t.Fatalf("unexpected exchanged session subject: %s", exchangedSession.Subject)
	}
}

// TestClientOperations tests mTLS communication, health checks, lifecycle probing, download and upload.
func TestClientOperations(t *testing.T) {
	ctx := context.Background()
	ca := newTestAuthority(t, "hub-client-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, "physician@hospital.org", false)

	serverPair, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(ca.pem)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}

	artifactPayload := []byte("readmit-test-evidence-payload-data-contents")
	artifactDigest := hex.EncodeToString(sha256New(artifactPayload))

	uploadedMap := make(map[string][]byte)

	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	hubMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	hubMux.HandleFunc("/v1/projects/icu-audit/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := map[string]any{
			"schema": "readmit-hub-lifecycle-history/v1",
			"head":   42,
			"events": []map[string]any{
				{
					"schema":  "readmit-hub-lifecycle-event/v1",
					"project": "icu-audit",
					"actor":   "auditor@hospital.org",
					"at":      "2026-09-21T12:00:00Z",
					"command": map[string]any{
						"kind":     "record",
						"resource": "evidence",
						"artifact": artifactDigest,
						"reason":   "baseline evidence collection",
					},
				},
			},
			"warning": "Customer test warning",
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, resp)
	})
	hubMux.HandleFunc("/v1/projects/denied-project/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	hubMux.HandleFunc("/v1/projects/missing-project/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	hubMux.HandleFunc("/v1/projects/icu-audit/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		digest := strings.TrimPrefix(r.URL.Path, "/v1/projects/icu-audit/artifacts/")
		if r.Method == "GET" {
			if digest == artifactDigest {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Readmit-Custody-Warning", "Downloaded copies remain under local custody and cannot be revoked.")
				w.WriteHeader(http.StatusOK)
				w.Write(artifactPayload)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == "PUT" {
			data, err := os.ReadFile(r.URL.Path) // just checking read from body
			_ = data
			bodyBytes, err := readBounded(r.Body, hubclient.MaxArtifactBytes)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			h := sha256.Sum256(bodyBytes)
			actualDigest := hex.EncodeToString(h[:])
			if actualDigest != digest {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			uploadedMap[digest] = bodyBytes
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	server := httptest.NewUnstartedServer(hubMux)
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	os.WriteFile(caPath, ca.pem, 0600)
	os.WriteFile(certPath, clientCert, 0600)
	keyProviderPath := createKeyProvider(t, clientKey)

	cfg := hubclient.Config{
		Schema:      hubclient.Schema,
		Hub:         server.URL,
		CA:          caPath,
		Certificate: certPath,
		Key: hubclient.KeyReference{
			Command:   keyProviderPath,
			Arguments: nil,
		},
		IdP: hubclient.IdPConfig{
			Issuer:            "https://idp.example.com",
			ClientID:          "client-1",
			Audience:          "hub-aud",
			AuthorizeEndpoint: "https://idp.example.com/auth",
			TokenEndpoint:     "https://idp.example.com/token",
			Scopes:            []string{"evidence.read", "evidence.write"},
		},
		Projects: []string{"icu-audit"},
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	nowSec := now.Unix()
	tok := issueTestToken(t, rsaKey, "key-1", cfg.IdP.Issuer, "physician@hospital.org", cfg.IdP.Audience, cfg.IdP.ClientID, []string{"evidence.read", "evidence.write"}, nowSec, nowSec+1800)
	session, err := hubclient.ValidateAccessToken(tok, cfg.IdP, now)
	if err != nil {
		t.Fatal(err)
	}

	client, err := hubclient.New(ctx, cfg, session)
	if err != nil {
		t.Fatalf("hubclient.New failed: %v", err)
	}

	// 1. Health check
	if err := client.CheckHealth(ctx); err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}

	// 2. Probe authorized project
	status, err := client.ProbeProject(ctx, "icu-audit")
	if err != nil {
		t.Fatalf("ProbeProject failed: %v", err)
	}
	if !status.Authorized {
		t.Fatalf("expected project icu-audit to be authorized: %s", status.Reason)
	}
	if len(status.Artifacts) != 1 || status.Artifacts[0].Digest != artifactDigest {
		t.Fatalf("unexpected artifacts in status: %v", status.Artifacts)
	}
	if status.Head != 42 {
		t.Fatalf("expected head 42, got %d", status.Head)
	}

	// 3. Probe denied project
	deniedStatus, err := client.ProbeProject(ctx, "denied-project")
	if err != nil {
		t.Fatalf("ProbeProject returned unexpected error: %v", err)
	}
	if deniedStatus.Authorized {
		t.Fatal("expected denied project to have Authorized=false")
	}
	if !strings.Contains(deniedStatus.Reason, "denied") {
		t.Fatalf("expected reason to mention denied: %s", deniedStatus.Reason)
	}

	// 4. Download artifact successfully
	destPath := filepath.Join(dir, "downloaded-evidence.bin")
	transfer, err := client.DownloadArtifact(ctx, "icu-audit", artifactDigest, destPath)
	if err != nil {
		t.Fatalf("DownloadArtifact failed: %v", err)
	}
	if transfer.State != "completed" || transfer.Size != int64(len(artifactPayload)) {
		t.Fatalf("unexpected transfer result: %+v", transfer)
	}
	if !strings.Contains(transfer.Warning, "local custody") {
		t.Fatalf("expected custody warning, got: %s", transfer.Warning)
	}
	downloadedBytes, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("cannot read downloaded file: %v", err)
	}
	if string(downloadedBytes) != string(artifactPayload) {
		t.Fatalf("payload content mismatch")
	}

	// Verify permissions are 0600
	destInfo, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if destInfo.Mode().Perm() != 0600 {
		t.Fatalf("expected file mode 0600, got %o", destInfo.Mode().Perm())
	}

	// 5. Download artifact with corrupt digest (simulate integrity error)
	wrongDigest := strings.Repeat("0", 64)
	_, err = client.DownloadArtifact(ctx, "icu-audit", wrongDigest, filepath.Join(dir, "wrong.bin"))
	if err == nil {
		t.Fatal("expected error on nonexistent/corrupt digest")
	}

	// 6. Upload artifact successfully
	uploadSrc := filepath.Join(dir, "to-upload.bin")
	uploadPayload := []byte("upload-artifact-test-payload-12345")
	if err := os.WriteFile(uploadSrc, uploadPayload, 0600); err != nil {
		t.Fatal(err)
	}
	upTransfer, err := client.UploadArtifact(ctx, "icu-audit", uploadSrc)
	if err != nil {
		t.Fatalf("UploadArtifact failed: %v", err)
	}
	if upTransfer.State != "completed" {
		t.Fatalf("unexpected upload state: %s", upTransfer.State)
	}
	if string(uploadedMap[upTransfer.Digest]) != string(uploadPayload) {
		t.Fatalf("uploaded bytes on server do not match local payload")
	}

	// 7. Upload without write permission
	readOnlyTok := issueTestToken(t, rsaKey, "key-1", cfg.IdP.Issuer, "viewer@hospital.org", cfg.IdP.Audience, cfg.IdP.ClientID, []string{"evidence.read"}, nowSec, nowSec+1800)
	roSession, err := hubclient.ValidateAccessToken(readOnlyTok, cfg.IdP, now)
	if err != nil {
		t.Fatal(err)
	}
	roClient, err := hubclient.New(ctx, cfg, roSession)
	if err != nil {
		t.Fatal(err)
	}
	_, err = roClient.UploadArtifact(ctx, "icu-audit", uploadSrc)
	if !errors.Is(err, hubclient.ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied for read-only session upload, got %v", err)
	}
}

func sha256New(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, max+1))
}
