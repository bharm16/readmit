package environment_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/environment"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
)

// Every certificate and key in this package is generated here, for one test
// process, under a temporary directory the test removes. None is committed, and
// nothing below reaches a host outside loopback.
const testOnlyName = "lab.example.invalid"

type authority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

type material struct {
	certificate []byte
	key         []byte
}

func newAuthority(t *testing.T) *authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial(t),
		Subject:               pkix.Name{CommonName: "readmit test-only authority"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &authority{certificate: certificate, key: key, pem: encode(t, "CERTIFICATE", der)}
}

// issue signs one leaf certificate. The identity, the validity window and the
// usage are explicit, so a test that needs an expired or misnamed certificate
// generates exactly that one on purpose.
func (a *authority) issue(t *testing.T, name string, notBefore, notAfter time.Time, usage x509.ExtKeyUsage) material {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial(t),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     []string{name},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.certificate, key.Public(), a.key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return material{certificate: encode(t, "CERTIFICATE", der), key: encode(t, "EC PRIVATE KEY", encoded)}
}

func (m material) pair(t *testing.T) tls.Certificate {
	t.Helper()
	pair, err := tls.X509KeyPair(m.certificate, m.key)
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func encode(t *testing.T, kind string, der []byte) []byte {
	t.Helper()
	data := pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der})
	if data == nil {
		t.Fatalf("cannot encode %s", kind)
	}
	return data
}

func serial(t *testing.T) *big.Int {
	t.Helper()
	number, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 96))
	if err != nil {
		t.Fatal(err)
	}
	return number
}

func write(t *testing.T, directory, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// endpoint starts one loopback listener on port 0 and closes it when the test
// ends. Nothing here reaches a host outside loopback, and no listener outlives
// the test that started it.
func endpoint(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				serve(connection)
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
	})
	return listener.Addr().String()
}

// drain reads until the peer closes, counting every byte it was sent.
func drain(received *atomic.Int64) func(net.Conn) {
	return func(connection net.Conn) {
		buffer := make([]byte, 1024)
		for {
			n, err := connection.Read(buffer)
			received.Add(int64(n))
			if err != nil {
				return
			}
		}
	}
}

// secured completes a TLS handshake and then drains. A handshake that fails is
// the endpoint's answer, and the connection is closed without a reply.
func secured(config *tls.Config, received *atomic.Int64) func(net.Conn) {
	return func(connection net.Conn) {
		server := tls.Server(connection, config)
		if err := server.Handshake(); err != nil {
			return
		}
		drain(received)(server)
	}
}

// silent keeps the connection open and answers nothing at all, so a client
// waiting for a server hello reaches its own connect timeout rather than
// seeing the endpoint close.
func silent(connection net.Conn) {
	buffer := make([]byte, 512)
	for {
		if _, err := connection.Read(buffer); err != nil {
			return
		}
	}
}

func plainTarget(address string) replay.Target {
	return replay.Target{
		Schema: replay.TargetSchemaV3, TestEndpoint: true, Name: "lab-siu",
		Classification: replay.Nonproduction, Address: address, Transport: "plain",
		ConnectTimeout: "2s", MessageTimeout: "200ms", MaxACKBytes: 4096,
	}
}

func tlsTarget(address, caFile string) replay.Target {
	config := plainTarget(address)
	config.Transport = "tls"
	config.CAFile = caFile
	config.ServerName = testOnlyName
	return config
}

func diagnose(t *testing.T, config replay.Target) environment.Report {
	t.Helper()
	report, err := environment.Diagnose(t.Context(), config)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	return report
}

// A connectivity diagnosis proves the endpoint is reachable and sends nothing.
func TestDiagnoseReachesAPlainEndpointWithoutSendingAnything(t *testing.T) {
	received := &atomic.Int64{}
	address := endpoint(t, drain(received))
	report := diagnose(t, plainTarget(address))
	if report.Outcome != environment.Reachable || report.Phase != "confirm" {
		t.Fatalf("outcome %q phase %q", report.Outcome, report.Phase)
	}
	if report.Peer != address {
		t.Fatalf("the diagnosis reported peer %q, want the address it actually reached %q", report.Peer, address)
	}
	if report.Name != "lab-siu" || report.Classification != replay.Nonproduction {
		t.Fatalf("the named environment and its classification were not reported: %+v", report)
	}
	if report.TLS != nil {
		t.Fatalf("a plain endpoint reported TLS status: %+v", report.TLS)
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("the diagnosis sent %d bytes to the endpoint; it must send none", got)
	}
}

// TLS status is reported in full, including the expiry of the certificate the
// endpoint presented. The expiry is information; nothing here acts on it.
func TestDiagnoseReportsTLSStatusAndCertificateExpiry(t *testing.T) {
	directory := t.TempDir()
	ca := newAuthority(t)
	expiry := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	server := ca.issue(t, testOnlyName, time.Now().Add(-time.Minute), expiry, x509.ExtKeyUsageServerAuth)
	received := &atomic.Int64{}
	address := endpoint(t, secured(&tls.Config{Certificates: []tls.Certificate{server.pair(t)}}, received))
	report := diagnose(t, tlsTarget(address, write(t, directory, "ca.pem", ca.pem)))
	if report.Outcome != environment.Reachable {
		t.Fatalf("outcome %q phase %q", report.Outcome, report.Phase)
	}
	if report.TLS == nil {
		t.Fatal("a verified TLS endpoint reported no TLS status")
	}
	if report.TLS.Version != "TLS 1.3" && report.TLS.Version != "TLS 1.2" {
		t.Fatalf("negotiated version %q", report.TLS.Version)
	}
	if report.TLS.CipherSuite == "" || report.TLS.ServerName != testOnlyName {
		t.Fatalf("TLS status %+v", report.TLS)
	}
	if report.TLS.ClientCertificatePresented {
		t.Fatal("a configuration with no client certificate reported presenting one")
	}
	if len(report.TLS.Chain) == 0 {
		t.Fatal("the presented certificate chain was not reported")
	}
	leaf := report.TLS.Chain[0]
	if !leaf.NotAfter.Equal(expiry) {
		t.Fatalf("reported expiry %s, want %s", leaf.NotAfter, expiry)
	}
	if leaf.Subject == "" || leaf.Issuer == "" {
		t.Fatalf("certificate %+v", leaf)
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("the diagnosis sent %d bytes over TLS; it must send none", got)
	}
}

// Every way reaching a configured endpoint can fail is refused with its own
// name. Unknown is never reported as reachable.
func TestDiagnoseNamesEveryTransportAndCertificateFailure(t *testing.T) {
	directory := t.TempDir()
	ca := newAuthority(t)
	other := newAuthority(t)
	caFile := write(t, directory, "ca.pem", ca.pem)
	valid := func() (time.Time, time.Time) { return time.Now().Add(-time.Minute), time.Now().Add(time.Hour) }

	for name, build := range map[string]func(*testing.T) (replay.Target, environment.Outcome, string){
		"an expired certificate": func(t *testing.T) (replay.Target, environment.Outcome, string) {
			server := ca.issue(t, testOnlyName, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), x509.ExtKeyUsageServerAuth)
			address := endpoint(t, secured(&tls.Config{Certificates: []tls.Certificate{server.pair(t)}}, &atomic.Int64{}))
			return tlsTarget(address, caFile), environment.CertificateExpired, "tls"
		},
		"a certificate from an authority that is not configured": func(t *testing.T) (replay.Target, environment.Outcome, string) {
			from, until := valid()
			server := other.issue(t, testOnlyName, from, until, x509.ExtKeyUsageServerAuth)
			address := endpoint(t, secured(&tls.Config{Certificates: []tls.Certificate{server.pair(t)}}, &atomic.Int64{}))
			return tlsTarget(address, caFile), environment.UntrustedAuthority, "tls"
		},
		"a certificate issued to another name": func(t *testing.T) (replay.Target, environment.Outcome, string) {
			from, until := valid()
			server := ca.issue(t, "other.example.invalid", from, until, x509.ExtKeyUsageServerAuth)
			address := endpoint(t, secured(&tls.Config{Certificates: []tls.Certificate{server.pair(t)}}, &atomic.Int64{}))
			return tlsTarget(address, caFile), environment.HostnameMismatch, "tls"
		},
		"a client certificate the endpoint requires and the configuration has not got": func(t *testing.T) (replay.Target, environment.Outcome, string) {
			from, until := valid()
			server := ca.issue(t, testOnlyName, from, until, x509.ExtKeyUsageServerAuth)
			pool := x509.NewCertPool()
			pool.AddCert(ca.certificate)
			address := endpoint(t, secured(&tls.Config{
				Certificates: []tls.Certificate{server.pair(t)},
				ClientAuth:   tls.RequireAndVerifyClientCert,
				ClientCAs:    pool,
			}, &atomic.Int64{}))
			return tlsTarget(address, caFile), environment.ClientCertificateRejected, "tls"
		},
		"a refused connection": func(t *testing.T) (replay.Target, environment.Outcome, string) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			return plainTarget(address), environment.ConnectionRefused, "dial"
		},
		"an endpoint that accepts and never completes TLS": func(t *testing.T) (replay.Target, environment.Outcome, string) {
			address := endpoint(t, silent)
			config := tlsTarget(address, caFile)
			config.ConnectTimeout = "200ms"
			return config, environment.Timeout, "tls"
		},
	} {
		config, want, phase := build(t)
		report := diagnose(t, config)
		if report.Outcome != want {
			t.Errorf("%s was reported as %q, want %q", name, report.Outcome, want)
		}
		if report.Phase != phase {
			t.Errorf("%s was reported in phase %q, want %q", name, report.Phase, phase)
		}
	}
}

// A cancelled diagnosis reports cancellation, never reachability.
func TestDiagnoseReportsCancellationRatherThanReachability(t *testing.T) {
	address := endpoint(t, drain(&atomic.Int64{}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	report, err := environment.Diagnose(ctx, plainTarget(address))
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if report.Outcome != environment.Cancelled {
		t.Fatalf("a cancelled diagnosis reported %q", report.Outcome)
	}
}

// Bytes the endpoint sends without being asked are reported as their own
// outcome. Nothing consumes them, and reachability is not claimed.
func TestDiagnoseReportsUnsolicitedBytes(t *testing.T) {
	address := endpoint(t, func(connection net.Conn) {
		connection.Write([]byte("MSH|^~\\&|"))
		drain(&atomic.Int64{})(connection)
	})
	report := diagnose(t, plainTarget(address))
	if report.Outcome != environment.UnsolicitedBytes || report.Unsolicited == 0 {
		t.Fatalf("unsolicited bytes were reported as %q (%d bytes)", report.Outcome, report.Unsolicited)
	}
}

// A configuration whose classification nobody recorded is reported as
// unclassified. It is never reported as a nonproduction one.
func TestDiagnoseReportsAnAbsentClassificationAsUnclassified(t *testing.T) {
	address := endpoint(t, drain(&atomic.Int64{}))
	config := replay.Target{
		Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain",
		ConnectTimeout: "2s", MessageTimeout: "200ms", MaxACKBytes: 4096,
	}
	report := diagnose(t, config)
	if report.Classification != replay.Unclassified || report.Name != "" {
		t.Fatalf("classification %q for the environment named %q", report.Classification, report.Name)
	}
}

// A configuration readmit cannot read is refused before anything is dialled.
func TestDiagnoseRefusesAConfigurationItCannotUse(t *testing.T) {
	directory := t.TempDir()
	config := tlsTarget("127.0.0.1:1", filepath.Join(directory, "absent.pem"))
	if _, err := environment.Diagnose(t.Context(), config); err == nil {
		t.Fatal("a CA file that is not there was accepted")
	}
	config = tlsTarget("127.0.0.1:1", write(t, directory, "empty.pem", []byte("not a certificate\n")))
	if _, err := environment.Diagnose(t.Context(), config); err == nil {
		t.Fatal("a CA file holding no certificate was accepted")
	}
}

// The stand-in credential store these tests point readmit at is this test
// binary, re-run with keyProvider set. It emits the bytes of the file its
// locator argument names. Every key it emits is generated by the test that
// registered it: no credential and no key is committed in this repository.
const keyProvider = "READMIT_TEST_KEY_PROVIDER"

func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv(keyProvider); ok && len(os.Args) == 2 {
		data, err := os.ReadFile(os.Args[1])
		if err != nil {
			os.Exit(4)
		}
		os.Stdout.Write(data)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// register writes a secret reference document naming this test binary as the
// store that reads the private key back, scoped to one endpoint address.
func register(t *testing.T, directory, address, key string) string {
	t.Helper()
	program, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(directory, "secrets.json")
	document := secret.Document{Schema: secret.Schema, References: []secret.Reference{{
		Name: "lab-client-key", Store: secret.CustomerManaged, Purpose: secret.MLLPEndpoint,
		Address: address, Command: program, Arguments: []string{key}, Generation: 1,
		RotatedAt: time.Now().UTC().Truncate(time.Second),
	}}}
	if err := secret.WriteStore(store, document); err != nil {
		t.Fatal(err)
	}
	return store
}

// A client certificate is configuration; its private key is a credential read
// from the store its reference names, for this one connection only.
func TestDiagnosePresentsTheClientCertificateItsCredentialNames(t *testing.T) {
	t.Setenv(keyProvider, "1")
	directory := t.TempDir()
	ca := newAuthority(t)
	from, until := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	server := ca.issue(t, testOnlyName, from, until, x509.ExtKeyUsageServerAuth)
	client := ca.issue(t, "readmit-test-only-client", from, until, x509.ExtKeyUsageClientAuth)
	pool := x509.NewCertPool()
	pool.AddCert(ca.certificate)
	received := &atomic.Int64{}
	address := endpoint(t, secured(&tls.Config{
		Certificates: []tls.Certificate{server.pair(t)},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}, received))

	config := tlsTarget(address, write(t, directory, "ca.pem", ca.pem))
	config.ClientCertificate = write(t, directory, "client.pem", client.certificate)
	config.Credential = replay.Credential{
		SecretsFile: register(t, directory, address, write(t, t.TempDir(), "client-key.pem", client.key)),
		Reference:   "lab-client-key",
	}
	report := diagnose(t, config)
	if report.Outcome != environment.Reachable {
		t.Fatalf("outcome %q phase %q", report.Outcome, report.Phase)
	}
	if report.TLS == nil || !report.TLS.ClientCertificateRequested || !report.TLS.ClientCertificatePresented {
		t.Fatalf("TLS status %+v", report.TLS)
	}
	if got := received.Load(); got != 0 {
		t.Fatalf("the diagnosis sent %d bytes; it must send none", got)
	}

	// A certificate whose private key is not the one its reference reads back
	// is refused by name, before anything is dialled.
	other := ca.issue(t, "readmit-test-only-other", from, until, x509.ExtKeyUsageClientAuth)
	mismatched := config
	mismatched.ClientCertificate = write(t, directory, "other.pem", other.certificate)
	if _, err := environment.Diagnose(t.Context(), mismatched); err == nil {
		t.Fatal("a client certificate that does not pair with its key was accepted")
	}
}
