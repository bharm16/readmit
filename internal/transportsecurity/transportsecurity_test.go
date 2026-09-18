package transportsecurity_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/transportsecurity"
)

// pair generates one self-signed certificate and its key in PEM. Both are
// test-only material created in this process and written nowhere.
func pair(t *testing.T) (certificate, key []byte) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "readmit-test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, IsCA: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalECPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded})
}

// TestConfigurationsApplyOneFloorAndAlwaysVerify is the whole rule: both
// directions floor at TLS 1.2, permit TLS 1.3 by leaving the ceiling to the
// library, and never skip verification.
func TestConfigurationsApplyOneFloorAndAlwaysVerify(t *testing.T) {
	certificate, key := pair(t)
	client, err := transportsecurity.ClientConfig("collector.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	server, err := transportsecurity.ServerConfig(certificate, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, config := range map[string]*tls.Config{"client": client, "server": server} {
		if config.MinVersion != tls.VersionTLS12 {
			t.Fatalf("the %s configuration does not floor at TLS 1.2: %x", name, config.MinVersion)
		}
		if config.MaxVersion != 0 {
			t.Fatalf("the %s configuration caps the version rather than permitting TLS 1.3", name)
		}
		if config.InsecureSkipVerify {
			t.Fatalf("the %s configuration skips verification", name)
		}
	}
	if client.ServerName != "collector.invalid" {
		t.Fatalf("the client configuration verifies against %q", client.ServerName)
	}
	if server.ClientAuth != tls.NoClientCert {
		t.Fatal("a listener asked for a client certificate it was not configured to verify")
	}
}

// TestClientConfigRefusesAnUnverifiableConfiguration keeps every refusal
// explicit. A missing name and an authority file holding no certificate would
// each produce a session readmit could not honestly describe.
func TestClientConfigRefusesAnUnverifiableConfiguration(t *testing.T) {
	if _, err := transportsecurity.ClientConfig("", nil); err == nil {
		t.Fatal("a connection with no verified server name was accepted")
	}
	if _, err := transportsecurity.ClientConfig("collector.invalid", []byte("not a certificate")); err == nil {
		t.Fatal("an authority file holding no certificate was accepted")
	}
}

// TestServerConfigRefusesAnUnusableListener names each way a listener cannot be
// stood up: no material, a certificate and key that are not a pair, an
// authority file with nothing in it, and mutual TLS with no issuing authority.
func TestServerConfigRefusesAnUnusableListener(t *testing.T) {
	certificate, key := pair(t)
	_, other := pair(t)
	for name, attempt := range map[string]func() error{
		"no certificate": func() error {
			_, err := transportsecurity.ServerConfig(nil, key, nil)
			return err
		},
		"no key": func() error {
			_, err := transportsecurity.ServerConfig(certificate, nil, nil)
			return err
		},
		"mismatched pair": func() error {
			_, err := transportsecurity.ServerConfig(certificate, other, nil)
			return err
		},
		"empty client authority": func() error {
			_, err := transportsecurity.ServerConfig(certificate, key, []byte("not a certificate"))
			return err
		},
	} {
		if err := attempt(); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

// TestADeclaredClientAuthorityRequiresAndVerifies states what declaring one
// means: the client certificate is required and verified against exactly that
// authority. There is no mode where a missing certificate reads as verified.
func TestADeclaredClientAuthorityRequiresAndVerifies(t *testing.T) {
	certificate, key := pair(t)
	authority, _ := pair(t)
	config, err := transportsecurity.ServerConfig(certificate, key, authority)
	if err != nil {
		t.Fatal(err)
	}
	if config.ClientAuth != tls.RequireAndVerifyClientCert || config.ClientCAs == nil {
		t.Fatalf("a declared client authority did not require and verify a client: %v", config.ClientAuth)
	}
}

// TestServerConfigRefusesADisclosedKeyMessage keeps the parser's own diagnostic
// out of readmit's: a message about key material is replaced, never wrapped.
func TestServerConfigRefusesADisclosedKeyMessage(t *testing.T) {
	certificate, _ := pair(t)
	_, err := transportsecurity.ServerConfig(certificate, []byte("-----BEGIN EC PRIVATE KEY-----\nsecret\n-----END EC PRIVATE KEY-----\n"), nil)
	if err == nil {
		t.Fatal("an unusable private key was accepted")
	}
	if got := err.Error(); got != "the configured certificate and the private key its reference names are not a pair" {
		t.Fatalf("the refusal repeated the parser's own message: %q", got)
	}
}
