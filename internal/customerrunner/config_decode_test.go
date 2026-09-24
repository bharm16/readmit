package customerrunner_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/testlicense"
)

// decodeFixture is a structurally complete configuration whose runner root may
// name a directory on another host: DecodeConfig validates bytes, not volumes.
func decodeFixture(root string) customerrunner.Config {
	return customerrunner.Config{
		Schema: "readmit-runner/v1", Hub: "https://hub.example:8443", Project: "alpha",
		Environment: "lab", Root: root, CA: "/etc/readmit-runner/ca.pem",
		Certificate: "/etc/readmit-runner/client.pem",
		Key:         customerrunner.Reference{Command: "/bin/cat", Arguments: []string{"/keys/runner-key"}},
		Token:       customerrunner.Reference{Command: "/bin/cat", Arguments: []string{"/keys/runner-token"}},
		UpdateKey:   base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "next",
	}
}

func TestDecodeConfigValidatesBytesWithoutTheRunnerRoot(t *testing.T) {
	// The root does not exist here: an application on another host can still
	// read and display the configuration the administrator installed.
	raw, err := json.Marshal(decodeFixture(filepath.Join(t.TempDir(), "absent-root")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := customerrunner.DecodeConfig(raw); err != nil {
		t.Fatalf("structural read refused: %v", err)
	}
}

func TestDecodeConfigRefusesUnknownMissingAndInvalidMembers(t *testing.T) {
	good, err := json.Marshal(decodeFixture("/var/lib/readmit-runner/runs"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if json.Unmarshal(good, &m) != nil {
		t.Fatal("fixture")
	}
	delete(m, "token")
	missing, _ := json.Marshal(m)
	m["token"] = nil
	nullToken, _ := json.Marshal(m)
	badKey := decodeFixture("/var/lib/readmit-runner/runs")
	badKey.UpdateKey = base64.StdEncoding.EncodeToString(make([]byte, 16))
	badKeyRaw, _ := json.Marshal(badKey)
	badHub := decodeFixture("/var/lib/readmit-runner/runs")
	badHub.Hub = "http://hub.example:8443"
	badHubRaw, _ := json.Marshal(badHub)
	badProject := decodeFixture("/var/lib/readmit-runner/runs")
	badProject.Project = "Alpha"
	badProjectRaw, _ := json.Marshal(badProject)
	schema := decodeFixture("/var/lib/readmit-runner/runs")
	schema.Schema = "readmit-runner/v2"
	schemaRaw, _ := json.Marshal(schema)
	for name, raw := range map[string][]byte{
		"missing member": missing,
		"null member":    nullToken,
		"short key":      badKeyRaw,
		"plain hub":      badHubRaw,
		"project case":   badProjectRaw,
		"wrong schema":   schemaRaw,
		"unknown member": []byte(`{"schema":"readmit-runner/v1","hub":"https://hub.example:8443","project":"alpha","environment":"lab","root":"/var/lib/readmit-runner/runs","ca":"/ca.pem","certificate":"/c.pem","key":{"command":"/bin/cat","arguments":[]},"token":{"command":"/bin/cat","arguments":[]},"update_key":"` + base64.StdEncoding.EncodeToString(make([]byte, 32)) + `","update_engine":"next","extra":1}`),
	} {
		if _, err := customerrunner.DecodeConfig(raw); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestValidateJobAppliesTheDocumentRules(t *testing.T) {
	if err := customerrunner.ValidateJob(customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "nightly-001", Spec: "/srv/spec.json"}); err != nil {
		t.Fatalf("valid job refused: %v", err)
	}
	bad := []customerrunner.Job{
		{Schema: "readmit-runner-job/v2", ID: "nightly-001", Spec: "/srv/spec.json"},
		{Schema: "readmit-runner-job/v1", ID: "Nightly", Spec: "/srv/spec.json"},
		{Schema: "readmit-runner-job/v1", ID: "nightly-001", Spec: "srv/spec.json"},
		{Schema: "readmit-runner-job/v1", ID: "", Spec: "/srv/spec.json"},
	}
	for _, job := range bad {
		if err := customerrunner.ValidateJob(job); err == nil {
			t.Fatalf("accepted %+v", job)
		}
	}
}

// admissionTLS answers the enrollment request with the given status and body,
// over the TLS 1.3 mutual-authentication path the real runner admission takes.
func admissionTLS(t *testing.T, status int, body string) (hubURL, caPath string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	leaf, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	caPath = filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf}), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{leaf}, PrivateKey: k}},
		MinVersion:   tls.VersionTLS13,
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server.URL, caPath
}

// credentials writes one private file and returns the reference the bounded
// absolute-program reader resolves it through, exactly as other credentials.
func credentials(t *testing.T, name, value string) customerrunner.Reference {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return customerrunner.Reference{Command: "/bin/cat", Arguments: []string{path}}
}

// runnerClient writes a self-signed runner client certificate to a private
// folder and returns its path and the private key that pairs with it.
func runnerClient(t *testing.T) (certPath string, clientKey []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	clientTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "runner"},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientRaw, err := x509.CreateCertificate(rand.Reader, clientTmpl, clientTmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certDir := t.TempDir()
	os.Chmod(certDir, 0700)
	certPath = filepath.Join(certDir, "client.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientRaw}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func TestEnrollReportsTheHubRefusalReason(t *testing.T) {
	certPath, clientKey := runnerClient(t)

	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{403, "version or environment refused", "version or environment refused"},
		{409, "environment leased or recovering", "environment leased or recovering"},
		{503, "capacity refused", "capacity refused"},
	} {
		hubURL, caPath := admissionTLS(t, tc.status, tc.body)
		root := t.TempDir()
		os.Chmod(root, 0700)
		c := customerrunner.Config{
			Schema: "readmit-runner/v1", Hub: hubURL, Project: "alpha", Environment: "lab",
			Root: root, CA: caPath, Certificate: certPath,
			Key:       credentials(t, "key.pem", string(clientKey)),
			Token:     credentials(t, "token", "rh_token"),
			UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "next",
		}
		_, err := customerrunner.Enroll(context.Background(), c)
		if err == nil || !errors.Is(err, customerrunner.ErrRefused) {
			t.Fatalf("enroll error: %v", err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("reason %q does not name the hub refusal %q", err.Error(), tc.want)
		}
	}
}

// The runner's key and token commands are programs the operator declared, and
// each run of them reports itself to an observer the caller's context carries:
// the release that ends an admitted run as well as the enrollment that began
// it, although the release is never cancelled with the run.
func TestRunReportsEveryKeyAndTokenCommandItRuns(t *testing.T) {
	certPath, clientKey := runnerClient(t)
	lease := `{"schema":"readmit-runner-lease/v1","expires_at":"` + time.Now().Add(9*time.Second).UTC().Format(time.RFC3339Nano) +
		`","max_seconds":60,"max_jobs":1}`
	hubURL, caPath := admissionTLS(t, http.StatusOK, lease)
	root := t.TempDir()
	os.Chmod(root, 0700)
	c := customerrunner.Config{
		Schema: "readmit-runner/v1", Hub: hubURL, Project: "alpha", Environment: "lab",
		Root: root, CA: caPath, Certificate: certPath,
		Key:       credentials(t, "key.pem", string(clientKey)),
		Token:     credentials(t, "token", "rh_token"),
		UpdateKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), UpdateEngine: "next",
	}
	var running, started, ended atomic.Int64
	guarded := customerrunner.WithOperationGuard(context.Background(), operationguard.New(testlicense.New(t)))
	ctx := secret.ObserveDeclaredPrograms(guarded, func() func() {
		running.Add(1)
		started.Add(1)
		return func() {
			running.Add(-1)
			ended.Add(1)
		}
	})
	// The job names a specification that is not there, so the admitted run is
	// refused before it executes anything and releases its admission at once.
	job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "nightly-001", Spec: filepath.Join(t.TempDir(), "absent.json")}
	if _, err := customerrunner.Run(ctx, c, job); !errors.Is(err, customerrunner.ErrRefused) {
		t.Fatalf("run: %v", err)
	}
	// Enrolling and releasing each read the key and the token once.
	if started.Load() != 4 || ended.Load() != 4 || running.Load() != 0 {
		t.Fatalf("the key and token commands were reported %d started, %d ended, %d running; want both read for the enrollment and the release",
			started.Load(), ended.Load(), running.Load())
	}
}
