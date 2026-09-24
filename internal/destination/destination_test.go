package destination_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Every certificate and key here is generated for one test process and none is
// committed. Nothing below reaches a host outside loopback: a configured name is
// a reserved one only a fake resolver answers.
const configuredName = "lab.example.invalid"

var (
	approved = &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8"}}
	remote   = &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"198.51.100.0/24"}}
)

// answering resolves the configured name to the given addresses and counts how
// often it was asked.
func answering(asked *atomic.Int64, addresses ...string) sendpolicy.Resolver {
	return func(_ context.Context, host string) ([]netip.Addr, error) {
		asked.Add(1)
		if host != configuredName {
			return nil, errors.New("only the configured name resolves in these tests")
		}
		var found []netip.Addr
		for _, address := range addresses {
			found = append(found, netip.MustParseAddr(address))
		}
		return found, nil
	}
}

// listen starts one loopback listener, counts what it accepts, hands each
// connection to serve, and closes when the test ends.
func listen(t *testing.T, serve func(net.Conn)) (string, *atomic.Int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := &atomic.Int64{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
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
	return listener.Addr().String(), accepted
}

// hold keeps a connection open and answers nothing until the peer closes it.
func hold(connection net.Conn) {
	buffer := make([]byte, 512)
	for {
		if _, err := connection.Read(buffer); err != nil {
			return
		}
	}
}

// hangUp reads the client's first TLS record in full and closes without
// answering, so the client sees the connection end rather than a reset.
func hangUp(connection net.Conn) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(connection, header); err != nil {
		return
	}
	_, _ = io.ReadFull(connection, make([]byte, int(header[3])<<8|int(header[4])))
}

// closed is a loopback port nothing listens on, so a dial to it is refused.
func closed(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

// named is the configured address a name gives the endpoint at address.
func named(address string) string {
	_, port, _ := net.SplitHostPort(address)
	return net.JoinHostPort(configuredName, port)
}

func admitted(t *testing.T, request destination.Request) destination.Route {
	t.Helper()
	decision, err := destination.Decide(t.Context(), request)
	route, ok := decision.Route()
	if err != nil || !ok {
		t.Fatalf("expected the destination to be admitted: %v %+v", err, decision.Decision)
	}
	return route
}

// TestDecideAdmitsEachPurposeByItsOwnRule is the one admission table. A send and
// an observation are explicit and need an allowed decision; a reset's check
// requests no send and needs nothing about the destination to refuse; a check
// reports the decision and is admitted whatever it says, to the configured
// address.
func TestDecideAdmitsEachPurposeByItsOwnRule(t *testing.T) {
	for name, expected := range map[string]struct {
		purpose        destination.Purpose
		address        string
		classification string
		policy         *sendpolicy.Policy
		admitted       bool
		explicit       bool
		reason         sendpolicy.Reason
	}{
		"a send to a loopback address":              {destination.Send, "127.0.0.1:2575", "", nil, true, true, sendpolicy.LoopbackDestination},
		"a send to a remote address with no policy": {destination.Send, "203.0.113.9:2575", "nonproduction", nil, false, true, sendpolicy.PolicyRequired},
		"a send to production":                      {destination.Send, "127.0.0.1:2575", "production", approved, false, true, sendpolicy.ProductionClassification},
		"an observation of an approved address":     {destination.Observe, "127.0.0.9:443", "nonproduction", approved, true, true, sendpolicy.Approved},
		"an observation of an unapproved address":   {destination.Observe, "203.0.113.9:443", "nonproduction", approved, false, true, sendpolicy.UnapprovedDestination},
		"a reset's check of a loopback address":     {destination.ResetCheck, "127.0.0.1:2575", "nonproduction", nil, true, false, sendpolicy.SendNotExplicit},
		"a reset's check of an approved address":    {destination.ResetCheck, "198.51.100.7:2575", "nonproduction", remote, true, false, sendpolicy.SendNotExplicit},
		"a reset's check of an unrecorded class":    {destination.ResetCheck, "198.51.100.7:2575", "unclassified", remote, false, false, sendpolicy.UnrecordedClassification},
		"a check nothing approves":                  {destination.Check, "203.0.113.9:2575", "nonproduction", nil, true, false, sendpolicy.PolicyRequired},
		"a check of production":                     {destination.Check, "127.0.0.1:2575", "production", nil, true, false, sendpolicy.ProductionClassification},
	} {
		decision, err := destination.Decide(t.Context(), destination.Request{
			Purpose: expected.purpose, Address: expected.address, Classification: expected.classification,
			Policy: expected.policy, Budget: time.Second,
		})
		route, ok := decision.Route()
		if err != nil || ok != expected.admitted || decision.Reason != expected.reason || decision.ExplicitSend != expected.explicit {
			t.Errorf("%s: admitted %v with %+v (%v); expected admitted %v, reason %q, explicit %v", name, ok, decision.Decision, err, expected.admitted, expected.reason, expected.explicit)
		}
		if !ok && route != (destination.Route{}) {
			t.Errorf("%s: a refused destination handed back a route to %q", name, route.Address())
		}
		if ok && route.Address() != expected.address {
			t.Errorf("%s: admitted to %q, expected %q", name, route.Address(), expected.address)
		}
	}
}

// TestANameResolvingToSeveralAddressesIsNeverConnectedTo holds for every
// purpose that connects on the decision's authority. Which address a
// connection would reach is not established, so none is reached. A check still
// reaches the configured address, because its decision is only reported.
func TestANameResolvingToSeveralAddressesIsNeverConnectedTo(t *testing.T) {
	for _, purpose := range []destination.Purpose{destination.Send, destination.Observe, destination.ResetCheck, destination.Check} {
		asked := &atomic.Int64{}
		decision, err := destination.Decide(t.Context(), destination.Request{
			Purpose: purpose, Address: configuredName + ":2575", Classification: "nonproduction",
			Policy: approved, Budget: time.Second, Resolve: answering(asked, "127.0.0.7", "127.0.0.8"),
		})
		route, ok := decision.Route()
		if err != nil || decision.Reason != sendpolicy.AmbiguousDestination || len(decision.ResolvedAddresses) != 2 {
			t.Fatalf("purpose %d: expected an ambiguous destination, got %+v (%v)", purpose, decision.Decision, err)
		}
		if purpose == destination.Check {
			if !ok || route.Address() != configuredName+":2575" {
				t.Errorf("a check reaches the configured address, got %q (%v)", route.Address(), ok)
			}
			continue
		}
		if ok {
			t.Errorf("purpose %d: admitted to %q", purpose, route.Address())
		}
	}
}

// TestADecisionNobodyRetainedOpensNothing retains the decision before it is
// used, refusals included, and a failure to retain it stops the caller before
// any connection exists.
func TestADecisionNobodyRetainedOpensNothing(t *testing.T) {
	address, accepted := listen(t, hold)
	failed := errors.New("cannot retain the decision")
	var retained []sendpolicy.Decision
	decision, err := destination.Decide(t.Context(), destination.Request{
		Purpose: destination.Send, Address: address, Budget: time.Second,
		Record: func(d sendpolicy.Decision) error { retained = append(retained, d); return failed },
	})
	if !errors.Is(err, failed) {
		t.Fatalf("the recorder's failure was not returned: %v", err)
	}
	if _, ok := decision.Route(); ok {
		t.Fatal("a decision that was not retained admitted a connection")
	}
	refused, err := destination.Decide(t.Context(), destination.Request{
		Purpose: destination.Send, Address: "203.0.113.9:2575", Classification: "nonproduction", Budget: time.Second,
		Record: func(d sendpolicy.Decision) error { retained = append(retained, d); return nil },
	})
	if err != nil || len(retained) != 2 || retained[0].Reason != sendpolicy.LoopbackDestination || retained[1].Reason != refused.Reason {
		t.Fatalf("every decision is retained, a refusal included: %+v (%v)", retained, err)
	}
	if accepted.Load() != 0 {
		t.Fatal("a connection was opened")
	}
}

// TestAnAdmittedRouteReachesOnlyTheAddressItsDecisionChecked is the pinned dial.
// The name resolves once, inside the decision, and every connection the route
// opens — whether this package completes TLS or a driver does and names an
// address of its own — reaches the checked address. A recorder that rewrites
// the decision it retained cannot change where the route leads.
func TestAnAdmittedRouteReachesOnlyTheAddressItsDecisionChecked(t *testing.T) {
	address, accepted := listen(t, hold)
	asked := &atomic.Int64{}
	for _, purpose := range []destination.Purpose{destination.Send, destination.Observe, destination.ResetCheck} {
		route := admitted(t, destination.Request{
			Purpose: purpose, Address: named(address), Classification: "nonproduction", Policy: approved,
			Budget: 2 * time.Second, Resolve: answering(asked, "127.0.0.1"),
			Record: func(d sendpolicy.Decision) error { d.ResolvedAddresses[0] = "203.0.113.9"; return nil },
		})
		if route.Address() != address {
			t.Fatalf("purpose %d: the route leads to %q, expected the checked %q", purpose, route.Address(), address)
		}
		connection, err := route.Open(t.Context(), nil)
		if err != nil {
			t.Fatalf("purpose %d: %v", purpose, err)
		}
		if connection.RemoteAddr().String() != address {
			t.Errorf("purpose %d: reached %s", purpose, connection.RemoteAddr())
		}
		connection.Close()
		driven, err := route.DialContext(t.Context(), "tcp", "203.0.113.9:5432")
		if err != nil {
			t.Fatalf("purpose %d: a driver's own address widened the decision: %v", purpose, err)
		}
		if driven.RemoteAddr().String() != address {
			t.Errorf("purpose %d: a driver's dial reached %s", purpose, driven.RemoteAddr())
		}
		driven.Close()
	}
	if asked.Load() != 3 {
		t.Fatalf("the name was resolved %d times for three decisions; nothing resolves it again", asked.Load())
	}
	deadline := time.Now().Add(2 * time.Second)
	for accepted.Load() != 6 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if accepted.Load() != 6 {
		t.Fatalf("the checked address accepted %d of six connections", accepted.Load())
	}
}

// TestResolvingSpendsTheBudgetOfTheConnectionItAuthorizes shares one connect
// budget between the lookup and the connection that follows at once on the
// decision's authority. A lookup that used the whole budget leaves a send and a
// reset's check nothing to connect with; an observation reads many times over
// a window, so each of its connections keeps the whole bound.
func TestResolvingSpendsTheBudgetOfTheConnectionItAuthorizes(t *testing.T) {
	address, _ := listen(t, hold)
	slow := func(ctx context.Context, _ string) ([]netip.Addr, error) {
		<-ctx.Done()
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	for purpose, spent := range map[destination.Purpose]bool{destination.Send: true, destination.ResetCheck: true, destination.Observe: false} {
		route := admitted(t, destination.Request{
			Purpose: purpose, Address: named(address), Classification: "nonproduction", Policy: approved,
			Budget: 200 * time.Millisecond, Resolve: slow,
		})
		connection, err := route.Open(t.Context(), nil)
		var failure *destination.Failure
		switch {
		case spent && (!errors.As(err, &failure) || failure.Phase != destination.Dial || failure.Kind != destination.Timeout):
			t.Errorf("purpose %d: the lookup spent the budget, yet the connection reported %v", purpose, err)
		case !spent && err != nil:
			t.Errorf("purpose %d: an observation's connection keeps its whole bound: %v", purpose, err)
		}
		if connection != nil {
			connection.Close()
		}
	}
}

type authority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

func newAuthority(t *testing.T) *authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial(t), Subject: pkix.Name{CommonName: "readmit test-only authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &authority{certificate: certificate, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue signs one leaf for name, valid from notBefore until notAfter.
func (a *authority) issue(t *testing.T, name string, notBefore, notAfter time.Time, usage x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: serial(t), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.certificate, key.Public(), a.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func (a *authority) valid(t *testing.T, name string, usage x509.ExtKeyUsage) tls.Certificate {
	return a.issue(t, name, time.Now().Add(-time.Minute), time.Now().Add(time.Hour), usage)
}

func serial(t *testing.T) *big.Int {
	t.Helper()
	number, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 96))
	if err != nil {
		t.Fatal(err)
	}
	return number
}

// serving completes a TLS handshake under config and then holds the connection.
func serving(config *tls.Config) func(net.Conn) {
	return func(connection net.Conn) {
		server := tls.Server(connection, config)
		if err := server.Handshake(); err != nil {
			return
		}
		hold(server)
	}
}

// TestOpenVerifiesTheConfiguredNameOnThePinnedAddress dials the address the
// decision checked and verifies the certificate against the configured name —
// the declared server name, or the configured host when none is declared —
// never against the address it dialled.
func TestOpenVerifiesTheConfiguredNameOnThePinnedAddress(t *testing.T) {
	ca := newAuthority(t)
	address, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{ca.valid(t, configuredName, x509.ExtKeyUsageServerAuth)}}))
	route := admitted(t, destination.Request{
		Purpose: destination.Send, Address: named(address), Classification: "nonproduction", Policy: approved,
		Budget: 2 * time.Second, Resolve: answering(&atomic.Int64{}, "127.0.0.1"),
	})
	connection, err := route.Open(t.Context(), &destination.Security{Authorities: ca.pem})
	if err != nil {
		t.Fatalf("the configured host was not the name verified: %v", err)
	}
	state, secured := connection.TLS()
	connection.Close()
	if !secured || state.ServerName != configuredName || state.Version < tls.VersionTLS12 {
		t.Fatalf("negotiated %+v", state)
	}
	_, err = route.Open(t.Context(), &destination.Security{ServerName: "other.example.invalid", Authorities: ca.pem})
	var failure *destination.Failure
	if !errors.As(err, &failure) || failure.Kind != destination.HostnameMismatch {
		t.Fatalf("a declared server name is the one verified: %v", err)
	}
	config, err := route.ClientConfig(destination.Security{Authorities: ca.pem})
	if err != nil || config.ServerName != configuredName || config.MinVersion != tls.VersionTLS12 || config.InsecureSkipVerify {
		t.Fatalf("a driver's configuration verifies the configured host under the one TLS rule: %+v %v", config, err)
	}
	literal := admitted(t, destination.Request{Purpose: destination.Check, Address: "[::1]:2575", Budget: time.Second})
	if config, err := literal.ClientConfig(destination.Security{}); err != nil || config.ServerName != "::1" {
		t.Fatalf("an address with no declared name verifies its own host: %+v %v", config, err)
	}
}

// TestOpenNamesEveryTransportAndCertificateFailure is the one classification
// suite. Every way opening a connection can fail has its own name and phase,
// and a certificate readmit refused is carried so it can be diagnosed.
func TestOpenNamesEveryTransportAndCertificateFailure(t *testing.T) {
	ca, other := newAuthority(t), newAuthority(t)
	pool := x509.NewCertPool()
	pool.AddCert(ca.certificate)
	for name, expected := range map[string]struct {
		address    func(*testing.T) string
		security   *destination.Security
		cancelled  bool
		phase      destination.Phase
		kind       destination.Kind
		unverified bool
	}{
		"a refused connection": {address: closed, phase: destination.Dial, kind: destination.ConnectionRefused},
		"a cancelled connection": {address: func(t *testing.T) string { a, _ := listen(t, hold); return a },
			cancelled: true, phase: destination.Dial, kind: destination.Cancelled},
		"an expired certificate": {address: func(t *testing.T) string {
			a, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{ca.issue(t, configuredName, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour), x509.ExtKeyUsageServerAuth)}}))
			return a
		}, security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.CertificateExpired, unverified: true},
		"a certificate from an authority that is not configured": {address: func(t *testing.T) string {
			a, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{other.valid(t, configuredName, x509.ExtKeyUsageServerAuth)}}))
			return a
		}, security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.UntrustedAuthority, unverified: true},
		"a certificate issued to another name": {address: func(t *testing.T) string {
			a, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{ca.valid(t, "other.example.invalid", x509.ExtKeyUsageServerAuth)}}))
			return a
		}, security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.HostnameMismatch, unverified: true},
		"a certificate for another purpose": {address: func(t *testing.T) string {
			a, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{ca.valid(t, configuredName, x509.ExtKeyUsageClientAuth)}}))
			return a
		}, security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.CertificateUnverified, unverified: true},
		"a client certificate a TLS 1.2 endpoint requires": {address: func(t *testing.T) string {
			a, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{ca.valid(t, configuredName, x509.ExtKeyUsageServerAuth)},
				MaxVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}))
			return a
		}, security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.ClientCertificateRejected},
		"an endpoint that refuses every version readmit offers": {address: func(t *testing.T) string {
			a, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{ca.valid(t, configuredName, x509.ExtKeyUsageServerAuth)},
				MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11}))
			return a
		}, security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.HandshakeRefused},
		"an endpoint that hangs up during the handshake": {address: func(t *testing.T) string {
			a, _ := listen(t, hangUp)
			return a
		}, security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.Disconnected},
		"an endpoint that never completes the handshake": {address: func(t *testing.T) string { a, _ := listen(t, hold); return a },
			security: &destination.Security{Authorities: ca.pem}, phase: destination.Handshake, kind: destination.Timeout},
	} {
		address := expected.address(t)
		route := admitted(t, destination.Request{
			Purpose: destination.Send, Address: named(address), Classification: "nonproduction", Policy: approved,
			Budget: 300 * time.Millisecond, Resolve: answering(&atomic.Int64{}, "127.0.0.1"),
		})
		ctx, cancel := context.WithCancel(t.Context())
		if expected.cancelled {
			cancel()
		}
		connection, err := route.Open(ctx, expected.security)
		cancel()
		var failure *destination.Failure
		if !errors.As(err, &failure) || connection != nil {
			t.Errorf("%s: expected a named failure, got %v", name, err)
			continue
		}
		if failure.Phase != expected.phase || failure.Kind != expected.kind {
			t.Errorf("%s: named %s in phase %s, expected %s in phase %s", name, failure.Kind, failure.Phase, expected.kind, expected.phase)
		}
		if (len(failure.Unverified) > 0) != expected.unverified || failure.Kind.Verification() != expected.unverified {
			t.Errorf("%s: carried %d unverified certificates; verification refusal %v", name, len(failure.Unverified), failure.Kind.Verification())
		}
		// A handshake failed on a connection that reached the checked address;
		// a dial that failed reached nothing.
		if reached := failure.Phase == destination.Handshake; (failure.Peer == address) != reached || !reached && failure.Peer != "" {
			t.Errorf("%s: reported reaching %q", name, failure.Peer)
		}
	}
}

// TestAClientCertificateRejectedAfterTheHandshakeIsNamed follows TLS 1.3, where
// the endpoint judges the client certificate after the client's handshake has
// completed, so the rejection arrives on the open connection. The connection
// knows the endpoint asked, and whether it had a certificate to present.
func TestAClientCertificateRejectedAfterTheHandshakeIsNamed(t *testing.T) {
	ca := newAuthority(t)
	pool := x509.NewCertPool()
	pool.AddCert(ca.certificate)
	address, _ := listen(t, serving(&tls.Config{Certificates: []tls.Certificate{ca.valid(t, configuredName, x509.ExtKeyUsageServerAuth)},
		MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}))
	route := admitted(t, destination.Request{
		Purpose: destination.Check, Address: address, Budget: 2 * time.Second,
	})
	connection, err := route.Open(t.Context(), &destination.Security{ServerName: configuredName, Authorities: ca.pem})
	if err != nil {
		t.Fatalf("a TLS 1.3 client completes its handshake before it is judged: %v", err)
	}
	defer connection.Close()
	if !connection.ClientCertificateRequested() || connection.ClientCertificatePresented() {
		t.Fatal("the endpoint asked for a certificate this connection did not have")
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = connection.Read(make([]byte, 1))
	if kind := connection.Classify(t.Context(), err); kind != destination.ClientCertificateRejected {
		t.Fatalf("the rejection was named %s (%v)", kind, err)
	}
	if kind := destination.Classify(t.Context(), err); kind != destination.HandshakeRefused {
		t.Fatalf("without knowing a certificate was asked for, the refusal is the endpoint's, named %s", kind)
	}

	presented := ca.valid(t, "readmit-test-only-client", x509.ExtKeyUsageClientAuth)
	accepted, err := route.Open(t.Context(), &destination.Security{ServerName: configuredName, Authorities: ca.pem, Certificate: &presented})
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	if !accepted.ClientCertificateRequested() || !accepted.ClientCertificatePresented() {
		t.Fatal("the configured client certificate was not presented")
	}
}

// TestReadAuthoritiesAppliesOneBoundAndOneRefusal reads the explicitly
// configured authority file every connection to a configuration verifies
// against, or nothing when none is configured.
func TestReadAuthoritiesAppliesOneBoundAndOneRefusal(t *testing.T) {
	directory := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if data, err := destination.ReadAuthorities(""); data != nil || err != nil {
		t.Fatalf("no configured authority is the platform roots: %q %v", data, err)
	}
	ca := newAuthority(t)
	if data, err := destination.ReadAuthorities(write("ca.pem", ca.pem)); err != nil || string(data) != string(ca.pem) {
		t.Fatalf("the configured authority was not read: %v", err)
	}
	for name, path := range map[string]string{
		"an absent file":             filepath.Join(directory, "absent.pem"),
		"a directory":                directory,
		"a file with no certificate": write("empty.pem", []byte("not a certificate\n")),
		"a file past the bound":      write("large.pem", append(ca.pem, make([]byte, 1<<20)...)),
	} {
		if _, err := destination.ReadAuthorities(path); err == nil {
			t.Errorf("%s was accepted as a certificate authority", name)
		}
	}
}
