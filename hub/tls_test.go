package hub_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/hub"
)

func certificates(t *testing.T, c *hub.Config) tls.Certificate {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "readmit synthetic test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c.ClientCA = filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(c.ClientCA, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	makeLeaf := func(id int64, usage x509.ExtKeyUsage) ([]byte, []byte) {
		leaf := &x509.Certificate{SerialNumber: big.NewInt(id), Subject: pkix.Name{CommonName: "synthetic test identity"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
		bytes, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		pk, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bytes}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})
	}
	cert, pk := makeLeaf(2, x509.ExtKeyUsageServerAuth)
	c.Certificate = filepath.Join(dir, "server.pem")
	c.Key = filepath.Join(dir, "server-key.pem")
	if err = os.WriteFile(c.Certificate, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.Key, pk, 0600); err != nil {
		t.Fatal(err)
	}
	cert, pk = makeLeaf(3, x509.ExtKeyUsageClientAuth)
	client, err := tls.X509KeyPair(cert, pk)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestTransportRequiresTrustedClientAndTLS13(t *testing.T) {
	var c hub.Config
	cert := certificates(t, &c)
	tc, err := c.TLS()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	server.TLS = tc
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	data, _ := os.ReadFile(c.Certificate)
	roots.AppendCertsFromPEM(data)
	ca, _ := os.ReadFile(c.ClientCA)
	roots.AppendCertsFromPEM(ca)
	for _, test := range []struct {
		name  string
		certs []tls.Certificate
		max   uint16
		pass  bool
	}{{"trusted", []tls.Certificate{cert}, tls.VersionTLS13, true}, {"absent", nil, tls.VersionTLS13, false}, {"obsolete TLS", []tls.Certificate{cert}, tls.VersionTLS12, false}} {
		t.Run(test.name, func(t *testing.T) {
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: test.certs, MaxVersion: test.max}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Second}
			r, err := client.Get(server.URL)
			if r != nil {
				r.Body.Close()
			}
			if (err == nil) != test.pass {
				t.Fatalf("pass=%v error=%v", test.pass, err)
			}
		})
	}
}
