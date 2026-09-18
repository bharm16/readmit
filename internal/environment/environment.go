// Package environment configures and diagnoses one named test environment over
// the explicitly selected target configuration replay already reads.
//
// A diagnosis opens one connection, completes TLS when the configuration
// declares it, reports what it found and closes. It never sends an HL7 payload:
// reaching an endpoint and verifying its certificate are evidence about the
// transport, never evidence that an application accepted, processed or stored
// anything. A confirmation window that expires with nothing received is the
// ordinary result for a receiver waiting to be sent to, and is not a negative
// application result.
//
// The classification a configuration records is displayed here, never enforced.
// It is what an operator wrote down: labelling an endpoint nonproduction is not
// proof that the address is safe to send to, and deciding what a classification
// permits belongs outside this package.
package environment

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

const (
	// maxPEMBytes bounds each certificate file a configuration names, matching
	// the bound replay applies to the CA file of the same configuration.
	maxPEMBytes = 1 << 20
	// confirmBytes bounds the one read a diagnosis performs. Anything the
	// endpoint sends unprompted is reported and discarded, never interpreted.
	confirmBytes = 512
)

// Outcome is what one diagnosis established. The set is closed, and every
// member other than Reachable names the reason reaching the endpoint failed.
// There is no member for an unknown result: an outcome readmit cannot name is
// reported as a network error, never as a reachable endpoint.
type Outcome string

const (
	Reachable        Outcome = "reachable"
	UnsolicitedBytes Outcome = "unsolicited_bytes"

	ConnectionRefused Outcome = "connection_refused"
	Timeout           Outcome = "timeout"
	Cancelled         Outcome = "cancelled"
	Disconnected      Outcome = "disconnected"
	NetworkError      Outcome = "network_error"

	CertificateExpired        Outcome = "tls_certificate_expired"
	UntrustedAuthority        Outcome = "tls_untrusted_authority"
	HostnameMismatch          Outcome = "tls_hostname_mismatch"
	ClientCertificateRejected Outcome = "tls_client_certificate_rejected"
	HandshakeFailed           Outcome = "tls_handshake_failed"
)

// Report is what one diagnosis found. It names the environment it described, so
// a verdict is never separated from the environment it was produced against.
type Report struct {
	Environment replay.Environment
	// Peer is the address the connection actually reached. It is read from the
	// established connection rather than from a separate name lookup, so it is
	// the destination that was used and not one that might be resolved again.
	Peer    string
	Outcome Outcome
	// Phase is where the outcome was established: dial, tls or confirm.
	Phase string
	// TLS is present only when a handshake completed. readmit does not describe
	// a session it did not establish.
	TLS *TLSStatus
	// Unverified holds the certificates the endpoint presented when
	// verification failed. They are reported so a certificate failure can be
	// diagnosed, and they are kept apart from TLS because nothing established
	// that they identify the endpoint.
	Unverified []Certificate
	// Unsolicited counts the bytes the endpoint sent without being asked.
	Unsolicited int
}

// TLSStatus is the session that was actually negotiated.
type TLSStatus struct {
	Version     string
	CipherSuite string
	ServerName  string
	// ClientCertificateRequested reports that the endpoint asked for a client
	// certificate; ClientCertificatePresented reports whether this
	// configuration had one to offer. Together they are the evidence behind a
	// rejected-certificate outcome, so a verdict never stands on its own.
	ClientCertificateRequested bool
	ClientCertificatePresented bool
	Chain                      []Certificate
}

// Certificate is one certificate the endpoint presented. Expiry is reported as
// the certificate states it; readmit does not act on how near it is.
type Certificate struct {
	Subject   string
	Issuer    string
	NotBefore time.Time
	NotAfter  time.Time
}

// Diagnose reaches the configured endpoint once and reports what it found.
//
// An error means the configuration itself cannot be used: a CA file that cannot
// be read, a client certificate that does not pair with the key its credential
// reference names, timeouts that are not durations. Failing to reach the
// endpoint is not an error; it is the named Outcome of a Report.
func Diagnose(ctx context.Context, target replay.Target) (Report, error) {
	report := Report{Environment: target.Environment()}
	connect, connectErr := time.ParseDuration(target.ConnectTimeout)
	window, windowErr := time.ParseDuration(target.MessageTimeout)
	if connectErr != nil || windowErr != nil || connect <= 0 || window <= 0 {
		return Report{}, errors.New("the configuration does not declare usable connect and message timeouts")
	}
	config, offered, err := clientConfig(ctx, target)
	if err != nil {
		return Report{}, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, connect)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", target.Address)
	if err != nil {
		report.Outcome, report.Phase = classify(err, dialCtx, false), "dial"
		return report, nil
	}
	defer connection.Close()
	// Cancellation interrupts blocked network input the way a replay's does.
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	report.Peer = connection.RemoteAddr().String()
	confirm := connection
	if config != nil {
		// The connect timeout covers dialling and TLS setup together, exactly
		// as it does for a replay to the same endpoint.
		secured := tls.Client(connection, config)
		if err := secured.HandshakeContext(dialCtx); err != nil {
			report.Outcome, report.Phase = classify(err, dialCtx, offered.requested), "tls"
			report.Unverified = unverified(err)
			return report, nil
		}
		report.TLS = status(secured.ConnectionState(), offered)
		confirm = secured
	}
	report.Outcome, report.Phase, report.Unsolicited = confirmQuiet(ctx, confirm, window, offered.requested)
	return report, nil
}

// clientConfig builds the TLS client configuration a target declares, or nil
// for a plain transport. Certificate verification is always on and there is no
// insecure mode: a diagnosis that skipped verification would report a trust it
// never established. An explicitly configured CA replaces the system roots.
func clientConfig(ctx context.Context, target replay.Target) (*tls.Config, *offer, error) {
	if target.Transport != "tls" {
		return nil, &offer{}, nil
	}
	// The contract's owner reads the CA member, so a diagnosis applies the same
	// bound and the same refusal a replay to this endpoint applies.
	authorities, err := replay.LoadCA(target)
	if err != nil {
		return nil, nil, err
	}
	// The minimum version, the always-on verification and the explicit
	// authority rule live in one place, so a diagnosis cannot honour settings
	// the send path of the same configuration ignores.
	config, err := transportsecurity.ClientConfig(replay.VerifiedServerName(target), authorities)
	if err != nil {
		return nil, nil, err
	}
	presented := &offer{}
	if target.ClientCertificate != "" {
		pair, err := clientCertificate(ctx, target)
		if err != nil {
			return nil, nil, err
		}
		presented.certificate = &pair
	}
	// The endpoint's request for a client certificate is observed rather than
	// assumed: this callback runs only when one was asked for, on the
	// handshake's own goroutine and before it completes. It is the difference
	// between "the endpoint refused us" and "the endpoint wanted a certificate
	// this configuration has not got".
	config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		presented.requested = true
		if presented.certificate == nil {
			return &tls.Certificate{}, nil
		}
		return presented.certificate, nil
	}
	return config, presented, nil
}

// offer is what this configuration had to present and what the endpoint asked
// for. It is written during the handshake and read after it, on one goroutine.
type offer struct {
	certificate *tls.Certificate
	requested   bool
}

// clientCertificate pairs the certificate chain a configuration names with the
// private key its credential reference reads back. The certificate is
// configuration and is shared as written; the key is a credential, resolved
// from its declared store for this one connection and never written anywhere.
func clientCertificate(ctx context.Context, target replay.Target) (tls.Certificate, error) {
	chain, err := readBounded(target.ClientCertificate, maxPEMBytes)
	if err != nil {
		return tls.Certificate{}, errors.New("cannot read the configured client certificate")
	}
	reference, err := replay.BindCredential(target)
	if err != nil {
		return tls.Certificate{}, err
	}
	if reference.Name == "" {
		return tls.Certificate{}, errors.New("a client certificate requires a credential reference naming its private key")
	}
	value, err := secret.Resolve(ctx, reference)
	if err != nil {
		return tls.Certificate{}, errors.New("the private key the client certificate's reference names did not resolve from its declared store")
	}
	// The parser's own message is replaced rather than wrapped. It describes
	// key material, and readmit does not repeat anything a credential says.
	pair, err := tls.X509KeyPair(chain, value.Expose())
	if err != nil {
		return tls.Certificate{}, errors.New("the configured client certificate and the private key its reference names are not a pair")
	}
	return pair, nil
}

// confirmQuiet waits for the endpoint to prove the connection survived without
// being sent anything. A window that expires with nothing received is the
// ordinary result, because a receiver waits to be sent to; it is reported as
// reachable and is never an application answer. Bytes arriving unprompted, a
// closed connection and a TLS alert are each named instead. TLS 1.3 completes
// the client's handshake before the endpoint has judged the client certificate,
// so a rejected one is reported here rather than at the handshake.
func confirmQuiet(ctx context.Context, connection net.Conn, window time.Duration, requested bool) (Outcome, string, int) {
	deadline := time.Now().Add(window)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := connection.SetReadDeadline(deadline); err != nil {
		return NetworkError, "confirm", 0
	}
	n, err := connection.Read(make([]byte, confirmBytes))
	if n > 0 {
		return UnsolicitedBytes, "confirm", n
	}
	var expired net.Error
	if err == nil || errors.As(err, &expired) && expired.Timeout() && ctx.Err() == nil {
		return Reachable, "confirm", 0
	}
	outcome := classify(err, ctx, requested)
	if peerAlert(err) {
		return outcome, "tls", 0
	}
	return outcome, "confirm", 0
}

// classify names one transport or certificate failure. Cancellation and expiry
// of the caller's own context are decided first, so a deadline that interrupted
// the work is never reported as the endpoint's answer.
func classify(err error, ctx context.Context, requested bool) Outcome {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return Cancelled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return Timeout
	}
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var authority x509.UnknownAuthorityError
	switch {
	case errors.As(err, &hostname):
		return HostnameMismatch
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return CertificateExpired
	case errors.As(err, &authority):
		return UntrustedAuthority
	}
	if peerAlert(err) {
		if requested {
			return ClientCertificateRejected
		}
		return HandshakeFailed
	}
	var expired net.Error
	switch {
	case errors.As(err, &expired) && expired.Timeout():
		return Timeout
	case replay.RefusedConnection(err):
		return ConnectionRefused
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed):
		return Disconnected
	}
	var verification *tls.CertificateVerificationError
	if errors.As(err, &verification) {
		return HandshakeFailed
	}
	return NetworkError
}

// peerAlert reports that the endpoint refused the connection at the TLS layer.
// crypto/tls surfaces an alert the peer sent as a net.OpError whose operation
// is "remote error" and keeps its own alert type unexported, so the operation
// identifies one without matching the text of an error message. An alert this
// does not recognise falls through to an ordinary network error, never to a
// reachable endpoint.
func peerAlert(err error) bool {
	var operation *net.OpError
	return errors.As(err, &operation) && operation.Op == "remote error"
}

// unverified reports the certificates an endpoint presented when verification
// refused them. crypto/tls carries them on the verification error, so a
// certificate failure can be diagnosed from the same connection that failed.
func unverified(err error) []Certificate {
	var verification *tls.CertificateVerificationError
	if !errors.As(err, &verification) {
		return nil
	}
	return describe(verification.UnverifiedCertificates)
}

func describe(certificates []*x509.Certificate) []Certificate {
	var described []Certificate
	for _, certificate := range certificates {
		described = append(described, Certificate{
			Subject:   certificate.Subject.String(),
			Issuer:    certificate.Issuer.String(),
			NotBefore: certificate.NotBefore.UTC(),
			NotAfter:  certificate.NotAfter.UTC(),
		})
	}
	return described
}

func status(state tls.ConnectionState, presented *offer) *TLSStatus {
	reported := &TLSStatus{
		Version:                    tls.VersionName(state.Version),
		CipherSuite:                tls.CipherSuiteName(state.CipherSuite),
		ServerName:                 state.ServerName,
		ClientCertificateRequested: presented.requested,
		ClientCertificatePresented: presented.requested && presented.certificate != nil,
		Chain:                      describe(state.PeerCertificates),
	}
	return reported
}

// readBounded reads one bounded regular file the configuration named. It
// re-checks the opened file rather than trusting the earlier stat, so a path
// that changed underneath is refused.
func readBounded(path string, limit int) ([]byte, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, errors.New("cannot resolve a configured file")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("a configured file must be a regular file")
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, errors.New("cannot open a configured file")
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("a configured file must be a regular file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > limit {
		return nil, errors.New("a configured file cannot be read within its size limit")
	}
	return data, nil
}
