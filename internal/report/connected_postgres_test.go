package report_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// labPostgres is a disposable real PostgreSQL cluster serving TLS on loopback,
// holding one database, "application", in which a least-privilege observer
// may read the view "observed" and nothing else. It needs READMIT_POSTGRES_BIN
// naming a PostgreSQL installation's bin folder, as the hub journeys do.
type labPostgres struct {
	Address string
	// CA is the PEM authority that signed the server certificate for
	// example.com.
	CA   []byte
	bin  string
	root string
	port string
}

// startPostgres creates and starts the cluster until the test ends. Without
// READMIT_POSTGRES_BIN a developer run skips; continuous integration fails.
func startPostgres(t testing.TB) *labPostgres {
	t.Helper()
	bin := os.Getenv("READMIT_POSTGRES_BIN")
	if bin == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("READMIT_POSTGRES_BIN must name PostgreSQL's bin folder in CI")
		}
		t.Skip("set READMIT_POSTGRES_BIN to a PostgreSQL bin folder to run the real database lab")
	}
	root, err := os.MkdirTemp("", "readmit-lab-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	p := &labPostgres{bin: bin, root: root}
	data := filepath.Join(root, "data")
	p.run(t, "initdb", "-D", data, "--auth-local=trust", "--auth-host=reject", "-U", "lab_owner", "--no-locale", "--encoding=UTF8")
	certificate, key, ca := serverCertificate(t)
	p.CA = ca
	for name, content := range map[string][]byte{"server.crt": certificate, "server.key": key, "pg_hba.conf": []byte("local all all trust\nhostssl application observer 127.0.0.1/32 trust\nhost all all 127.0.0.1/32 reject\n")} {
		if err := os.WriteFile(filepath.Join(data, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p.Address = listener.Addr().String()
	_, p.port, _ = net.SplitHostPort(p.Address)
	listener.Close()
	p.run(t, "pg_ctl", "-D", data, "-l", filepath.Join(root, "server.log"), "-w", "-t", "20", "-o", "-h 127.0.0.1 -p "+p.port+" -k "+root+" -c ssl=on", "start")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, filepath.Join(bin, "pg_ctl"), "-D", data, "-m", "immediate", "-w", "stop").Run()
	})
	p.run(t, "psql", "-h", root, "-p", p.port, "-U", "lab_owner", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", "CREATE ROLE observer LOGIN")
	p.run(t, "createdb", "-h", root, "-p", p.port, "-U", "lab_owner", "application")
	return p
}

// Exec runs statements as the database owner.
func (p *labPostgres) Exec(t testing.TB, statements string) {
	t.Helper()
	p.run(t, "psql", "-h", p.root, "-p", p.port, "-U", "lab_owner", "-d", "application", "-v", "ON_ERROR_STOP=1", "-c", statements)
}

func (p *labPostgres) run(t testing.TB, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, filepath.Join(p.bin, name), args...).CombinedOutput(); err != nil {
		t.Fatalf("lab PostgreSQL %s failed: %v\n%s", name, err, out)
	}
}

// serverCertificate is a fresh authority and a server certificate for
// example.com signed by it.
func serverCertificate(t testing.TB) (certificate, key, ca []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Readmit lab database authority"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "example.com"}, DNSNames: []string{"example.com"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}
