package receiver_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

// authority is a certificate authority generated for one test. Every key here
// is created in the test process, is never written to the repository, and
// exists only for the duration of the test that made it.
type authority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

func newAuthority(t *testing.T, name string) *authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, IsCA: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &authority{certificate: parsed, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})}
}

// issue returns one leaf certificate and its private key, both in PEM. The key
// is test-only material generated here; readmit itself never writes one.
func (a *authority) issue(t *testing.T, name string, server bool) (certificate, key []byte) {
	t.Helper()
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.ExtKeyUsageClientAuth
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	if server {
		usage = x509.ExtKeyUsageServerAuth
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	template.ExtKeyUsage = []x509.ExtKeyUsage{usage}
	raw, err := x509.CreateCertificate(rand.Reader, template, a.certificate, &leafKey.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encodedKey})
}

// securedCapture starts a capture behind the one TLS rule, with the listener
// wrapped exactly the way the command wraps it.
func securedCapture(t *testing.T, issuer *authority, clientAuthority []byte) *collecting {
	t.Helper()
	certificate, key := issuer.issue(t, "readmit-collector", true)
	config, err := transportsecurity.ServerConfig(certificate, key, clientAuthority)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return serving(t, receiver.CollectorConfig{MaxMessages: 1, MaxConnections: 2, IdleTimeout: 2 * time.Second, TLS: true, ClientCertificate: len(clientAuthority) > 0},
		tls.NewListener(listener, config))
}

// secureDial reaches the capture under the same client rule every other
// readmit path uses.
func secureDial(t *testing.T, address string, roots []byte, certificates ...tls.Certificate) (*tls.Conn, error) {
	t.Helper()
	config, err := transportsecurity.ClientConfig("127.0.0.1", roots)
	if err != nil {
		t.Fatal(err)
	}
	config.Certificates = certificates
	connection, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	secured := tls.Client(connection, config)
	return secured, secured.Handshake()
}

func keyPair(t *testing.T, certificate, key []byte) tls.Certificate {
	t.Helper()
	pair, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

// TestCollectorCapturesOverVerifiedMutualTLS is the positive mTLS contract: a
// client the declared authority issued is served, its frame is captured, and
// the negotiated session is at or above the floor every readmit path applies.
func TestCollectorCapturesOverVerifiedMutualTLS(t *testing.T) {
	issuer := newAuthority(t, "readmit-test-server-ca")
	clients := newAuthority(t, "readmit-test-client-ca")
	certificate, key := clients.issue(t, "readmit-test-client", false)
	h := securedCapture(t, issuer, clients.pem)
	secured, err := secureDial(t, h.address, issuer.pem, keyPair(t, certificate, key))
	if err != nil {
		t.Fatalf("a verified client was refused: %v", err)
	}
	if secured.ConnectionState().Version < tls.VersionTLS12 {
		t.Fatalf("a session below the declared floor was negotiated: %x", secured.ConnectionState().Version)
	}
	reader, _ := mllp.NewReader(secured, 1<<20)
	answer, err := exchange(secured, reader, "MUTUAL-001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "MUTUAL-001") {
		t.Fatalf("the acknowledgement did not echo the sender's control ID: %q", answer)
	}
	secured.Close()
	record := h.finished(t).Collection
	if record == nil || len(record.Received) != 1 || record.Received[0].ControlID != "MUTUAL-001" {
		t.Fatalf("a mutually authenticated frame was not captured: %+v", record)
	}
}

// TestCollectorRefusesAClientCertificateFromAnotherAuthority is the negative
// mTLS contract. A certificate the declared authority did not issue is refused
// at the handshake, nothing is captured, and no acknowledgement is invented.
func TestCollectorRefusesAClientCertificateFromAnotherAuthority(t *testing.T) {
	issuer := newAuthority(t, "readmit-test-server-ca")
	clients := newAuthority(t, "readmit-test-client-ca")
	stranger := newAuthority(t, "readmit-test-other-ca")
	certificate, key := stranger.issue(t, "readmit-test-outsider", false)
	h := securedCapture(t, issuer, clients.pem)
	secured, err := secureDial(t, h.address, issuer.pem, keyPair(t, certificate, key))
	if err == nil {
		// TLS 1.3 completes the client's handshake before the endpoint has
		// judged the certificate, so the refusal arrives on the next read.
		_, err = secured.Read(make([]byte, 1))
	}
	if err == nil {
		t.Fatal("a client certificate from an undeclared authority was accepted")
	}
	secured.Close()
	h.cancel()
	record := h.finished(t).Collection
	if record == nil || len(record.Received) != 0 || len(record.Sessions) != 0 {
		t.Fatalf("a refused client left captured evidence: %+v", record)
	}
}

// TestCollectorRefusesAMissingClientCertificateWhenRequired keeps "no
// certificate" from reading as "verified": an absent credential is a refusal,
// never an anonymous session that is captured anyway.
func TestCollectorRefusesAMissingClientCertificateWhenRequired(t *testing.T) {
	issuer := newAuthority(t, "readmit-test-server-ca")
	clients := newAuthority(t, "readmit-test-client-ca")
	h := securedCapture(t, issuer, clients.pem)
	secured, err := secureDial(t, h.address, issuer.pem)
	if err == nil {
		_, err = secured.Read(make([]byte, 1))
	}
	if err == nil {
		t.Fatal("a client presenting no certificate was accepted where one is required")
	}
	secured.Close()
	h.cancel()
	if record := h.finished(t).Collection; record == nil || len(record.Received) != 0 {
		t.Fatalf("a refused client left captured evidence: %+v", record)
	}
}

// TestClientRefusesAnUntrustedListenerAuthority states the other direction of
// the same rule: verification is always on, so a listener whose authority the
// client does not trust is refused rather than captured against.
func TestClientRefusesAnUntrustedListenerAuthority(t *testing.T) {
	issuer := newAuthority(t, "readmit-test-server-ca")
	stranger := newAuthority(t, "readmit-test-other-ca")
	h := securedCapture(t, issuer, nil)
	if _, err := secureDial(t, h.address, stranger.pem); err == nil {
		t.Fatal("a listener presenting an untrusted authority was accepted")
	}
	h.cancel()
	if record := h.finished(t).Collection; record == nil || len(record.Received) != 0 {
		t.Fatalf("a refused handshake left captured evidence: %+v", record)
	}
}

// TestCollectorRefusesATLSVersionBelowTheFloor proves the floor is enforced by
// the listener, not only declared: a client capped below TLS 1.2 cannot capture.
func TestCollectorRefusesATLSVersionBelowTheFloor(t *testing.T) {
	issuer := newAuthority(t, "readmit-test-server-ca")
	h := securedCapture(t, issuer, nil)
	config, err := transportsecurity.ClientConfig("127.0.0.1", issuer.pem)
	if err != nil {
		t.Fatal(err)
	}
	config.MinVersion, config.MaxVersion = tls.VersionTLS10, tls.VersionTLS11
	connection, err := net.DialTimeout("tcp", h.address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := tls.Client(connection, config).Handshake(); err == nil {
		t.Fatal("a session below TLS 1.2 was negotiated")
	}
	h.cancel()
	if record := h.finished(t).Collection; record == nil || len(record.Received) != 0 {
		t.Fatalf("a refused handshake left captured evidence: %+v", record)
	}
}
