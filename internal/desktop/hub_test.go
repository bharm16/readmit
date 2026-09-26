package desktop_test

import (
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
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hubprotocol"
)

type hubTestAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newHubTestAuthority(t *testing.T, cn string) *hubTestAuthority {
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
	return &hubTestAuthority{
		cert: parsed,
		key:  k,
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}),
	}
}

func (a *hubTestAuthority) issue(t *testing.T, cn string, server bool) (certPEM, keyPEM []byte) {
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

func createDesktopKeyProvider(t *testing.T, keyBytes []byte) string {
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

func issueDesktopTestToken(t *testing.T, key *rsa.PrivateKey, kid, iss, sub, aud, clientID string, scopes []string, iat, exp int64) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "typ": "at+jwt", "kid": kid}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{
		"iss":       iss,
		"sub":       sub,
		"aud":       aud,
		"client_id": clientID,
		"jti":       "test-jti-5678",
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

func TestDesktopHubJourney(t *testing.T) {
	ca := newHubTestAuthority(t, "customer-hub-ca")
	serverCert, serverKey := ca.issue(t, "localhost", true)
	clientCert, clientKey := ca.issue(t, "doctor@hospital.org", false)

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

	artifactPayload := []byte("readmit-evidence-hub-artifact-content-bytes")
	artifactDigest := hex.EncodeToString(func() []byte {
		h := sha256.Sum256(artifactPayload)
		return h[:]
	}())

	hubMux := http.NewServeMux()
	hubMux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	hubMux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	hubMux.HandleFunc("/v1/projects/cardio-study/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := hubprotocol.LifecycleHistory{
			Schema: hubprotocol.LifecycleHistorySchema,
			Head:   10,
			Events: []hubprotocol.LifecycleEvent{{
				Schema:  hubprotocol.LifecycleEventSchema,
				Project: "cardio-study",
				Actor:   "lead@hospital.org",
				At:      "2026-09-21T10:00:00Z",
				Command: hubprotocol.LifecycleCommand{
					Kind:     "record",
					Resource: "evidence",
					Artifact: artifactDigest,
					Reason:   "baseline trial observation",
				},
			}},
			Warning: "Hospital Trial Hub Warning",
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, resp)
	})
	hubMux.HandleFunc("/v1/projects/cardio-study/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		digest := strings.TrimPrefix(r.URL.Path, "/v1/projects/cardio-study/artifacts/")
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
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			h := sha256.Sum256(body)
			if hex.EncodeToString(h[:]) != digest {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// A support export is served the same way, so downloading one can be
	// driven through a planted link below.
	hubMux.HandleFunc("/v2/projects/cardio-study/exports/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != "GET" || strings.TrimPrefix(r.URL.Path, "/v2/projects/cardio-study/exports/") != artifactDigest {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(artifactPayload)
	})

	server := httptest.NewUnstartedServer(hubMux)
	server.TLS = tlsConfig
	server.StartTLS()
	defer server.Close()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	nowSec := now.Unix()
	testToken := issueDesktopTestToken(t, rsaKey, "key-1", "https://idp.hospital.org", "doctor@hospital.org", "hub-aud", "desktop-app", []string{"evidence.read", "evidence.write"}, nowSec, nowSec+1800)

	idpMux := http.NewServeMux()
	idpMux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"access_token": testToken,
			"token_type":   "Bearer",
			"expires_in":   1800,
		}
		w.Header().Set("Content-Type", "application/json")
		json.MarshalWrite(w, resp)
	})
	idpServer := httptest.NewServer(idpMux)
	defer idpServer.Close()

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	certPath := filepath.Join(dir, "client.pem")
	os.WriteFile(caPath, ca.pem, 0600)
	os.WriteFile(certPath, clientCert, 0600)
	keyProviderPath := createDesktopKeyProvider(t, clientKey)

	cfgJSON := fmt.Sprintf(`{
		"schema": "readmit-hub-client/v1",
		"hub": %q,
		"ca": %q,
		"certificate": %q,
		"key": {
			"command": %q,
			"arguments": []
		},
		"idp": {
			"issuer": "https://idp.hospital.org",
			"client_id": "desktop-app",
			"audience": "hub-aud",
			"authorize_endpoint": "https://idp.hospital.org/auth",
			"token_endpoint": %q,
			"scopes": ["evidence.read", "evidence.write"]
		},
		"projects": ["cardio-study"]
	}`, server.URL, caPath, certPath, keyProviderPath, idpServer.URL+"/oauth/token")

	cfgPath := filepath.Join(dir, "hub-client.json")
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	app := desktop.New(&chooser{folder: dir}, desktop.ShellDocuments{Folder: dir})

	// 1. Initial status: offline/local
	status0 := app.HubStatus()
	if status0.State != desktop.Empty || status0.Connected {
		t.Fatalf("expected empty/offline status initially, got %+v", status0)
	}

	// 2. Select config
	selectRes := app.SelectHubConfig(cfgPath)
	if selectRes.State != desktop.Completed || selectRes.ConfigPath != cfgPath {
		t.Fatalf("SelectHubConfig failed: %+v", selectRes)
	}

	// 3. Diagnose hub prerequisites
	diagRes := app.DiagnoseHub()
	if diagRes.State != desktop.Completed || !diagRes.Passed {
		t.Fatalf("DiagnoseHub failed: %+v", diagRes)
	}

	// 4. Connect to hub
	connRes := app.ConnectHub()
	if connRes.State != desktop.Completed || !connRes.Connected {
		t.Fatalf("ConnectHub failed: %+v", connRes)
	}
	if connRes.Authenticated {
		t.Fatal("expected Authenticated=false before signing in")
	}

	// 5. Start auth flow
	authUrlRes := app.StartHubAuth()
	if authUrlRes.State != desktop.Completed || authUrlRes.AuthURL == "" || authUrlRes.Port <= 0 {
		t.Fatalf("StartHubAuth failed: %+v", authUrlRes)
	}

	// Complete auth via loopback callback
	go func() {
		// Wait 50ms for listener to be active
		time.Sleep(50 * time.Millisecond)
		// Extract state from authURL
		authURL := authUrlRes.AuthURL
		var stateVal string
		for _, part := range strings.Split(authURL, "&") {
			if strings.HasPrefix(part, "state=") {
				stateVal = strings.TrimPrefix(part, "state=")
			}
		}
		http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?code=auth-code-test&state=%s", authUrlRes.Port, stateVal))
	}()

	authFinishRes := app.CompleteHubAuth("", "")
	if authFinishRes.State != desktop.Completed || !authFinishRes.Authenticated {
		t.Fatalf("CompleteHubAuth failed: %+v", authFinishRes)
	}
	if authFinishRes.Subject != "doctor@hospital.org" {
		t.Fatalf("unexpected subject: %s", authFinishRes.Subject)
	}
	if len(authFinishRes.Projects) != 1 || !authFinishRes.Projects[0].Authorized {
		t.Fatalf("expected authorized project cardio-study, got %+v", authFinishRes.Projects)
	}

	// 6. List project artifacts
	artRes := app.ListHubProjectArtifacts("cardio-study")
	if artRes.State != desktop.Completed || len(artRes.Artifacts) != 1 {
		t.Fatalf("ListHubProjectArtifacts failed: %+v", artRes)
	}
	if artRes.Artifacts[0].Digest != artifactDigest {
		t.Fatalf("unexpected artifact digest: %s", artRes.Artifacts[0].Digest)
	}

	// 7. Download artifact
	downloadDest := filepath.Join(dir, "downloaded-evidence.bin")
	dlRes := app.DownloadHubArtifact(desktop.HubDownloadRequest{
		Project:         "cardio-study",
		Digest:          artifactDigest,
		DestinationPath: downloadDest,
	})
	if dlRes.State != desktop.Completed || dlRes.TransferState != "completed" {
		t.Fatalf("DownloadHubArtifact failed: %+v", dlRes)
	}
	if !strings.Contains(dlRes.Warning, "local custody") {
		t.Fatalf("expected custody warning in download result, got: %s", dlRes.Warning)
	}
	content, err := os.ReadFile(downloadDest)
	if err != nil || string(content) != string(artifactPayload) {
		t.Fatalf("downloaded content mismatch")
	}
	// A download of an artifact or of a support export writes over its
	// destination by renaming a new file onto it, so a symbolic link planted
	// there is replaced and the file it led to keeps its bytes (#347).
	// Windows may not let this account make one.
	outsideFile := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outsideFile, []byte("synthetic file outside the destination"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, download := range map[string]func(desktop.HubDownloadRequest) desktop.HubTransferResult{
		"DownloadHubArtifact": app.DownloadHubArtifact,
		"DownloadHubExport":   app.DownloadHubExport,
	} {
		linkedDest := filepath.Join(dir, name+"-linked.bin")
		if err := os.Symlink(outsideFile, linkedDest); err != nil {
			if runtime.GOOS == "windows" {
				break
			}
			t.Fatal(err)
		}
		if linked := download(desktop.HubDownloadRequest{Project: "cardio-study", Digest: artifactDigest, DestinationPath: linkedDest}); linked.State != desktop.Completed {
			t.Fatalf("%s over a link: %+v", name, linked)
		}
		if outside, err := os.ReadFile(outsideFile); err != nil || string(outside) != "synthetic file outside the destination" {
			t.Fatalf("%s through a link changed the file it led to", name)
		}
		if entry, err := os.Lstat(linkedDest); err != nil || !entry.Mode().IsRegular() || string(mustRead(t, linkedDest)) != string(artifactPayload) {
			t.Fatalf("%s left %v at its destination, want what it downloaded", name, entry)
		}
	}

	// 8. Upload artifact (admit author requirement)
	uploadSrc := filepath.Join(dir, "to-upload.bin")
	if err := os.WriteFile(uploadSrc, []byte("test upload bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	// Upload requires author admission (writes: true)
	// Without operation license admission, admitAuthor will check operationGuard
	// Let's test upload result
	upRes := app.UploadHubArtifact(desktop.HubUploadRequest{
		Project:    "cardio-study",
		SourcePath: uploadSrc,
	})
	// Check if admitted or permission denied depending on operation policy
	if upRes.State == desktop.Completed {
		if upRes.TransferState != "completed" {
			t.Fatalf("UploadHubArtifact unexpected transfer state: %+v", upRes)
		}
	} else if upRes.State != desktop.PermissionDenied {
		t.Fatalf("expected Completed or PermissionDenied for Upload, got %+v", upRes)
	}

	// 9. Disconnect
	discRes := app.DisconnectHub()
	if discRes.State != desktop.Completed || discRes.Connected {
		t.Fatalf("DisconnectHub failed: %+v", discRes)
	}
	if !strings.Contains(discRes.CustodyWarning, "local custody") {
		t.Fatalf("expected custody warning on disconnect, got: %s", discRes.CustodyWarning)
	}

	// 10. Status after disconnect
	statusFinal := app.HubStatus()
	if statusFinal.Connected || statusFinal.Authenticated {
		t.Fatalf("expected offline after disconnect: %+v", statusFinal)
	}
}
