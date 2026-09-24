package destination

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

// maxAuthorityBytes bounds a configured certificate authority file.
const maxAuthorityBytes = 1 << 20

// ReadAuthorities returns the bytes of the explicitly configured certificate
// authority file path names, or nothing when it names none and the platform
// roots apply. A send and a diagnosis of the same target configuration read its
// CA member here, so both apply one bound and one refusal to it.
func ReadAuthorities(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, errors.New("cannot read configured CA certificates")
	}
	data, err := authorityFile.Read(resolved)
	if err != nil {
		return nil, errors.New("cannot read configured CA certificates")
	}
	if !x509.NewCertPool().AppendCertsFromPEM(data) {
		return nil, errors.New("configured CA file contains no certificates")
	}
	return data, nil
}

// authorityFile is how a configured certificate authority file is read,
// through the shared document store, which checks the opened descriptor again
// because the path can change underneath between the check and the open.
var authorityFile = artifactdir.Document{
	MaxBytes: maxAuthorityBytes,
	Links:    artifactdir.FollowLinks,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("input must be a regular file"),
		Read:      errors.New("input cannot be read within size limit"),
	},
}

// Open dials the route's address and, when security is present, completes TLS,
// within the route's budget. A connection it could not open is a *Failure that
// names where and why. Any other error is a TLS configuration the route cannot
// use, reported before anything is dialled.
func (r Route) Open(ctx context.Context, security *Security) (*Connection, error) {
	offered := &offer{}
	var config *tls.Config
	if security != nil {
		built, err := r.ClientConfig(*security)
		if err != nil {
			return nil, err
		}
		// The endpoint's request for a client certificate is observed rather
		// than assumed: this runs only when one was asked for, on the
		// handshake's own goroutine and before it completes. It is the
		// difference between "the endpoint refused us" and "the endpoint
		// wanted a certificate this configuration has not got".
		offered.hasCertificate = security.Certificate != nil
		present := built.GetClientCertificate
		built.GetClientCertificate = func(request *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			offered.requested = true
			return present(request)
		}
		config = built
	}
	dialCtx, cancel := context.WithTimeout(ctx, r.budget)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", r.address)
	if err != nil {
		return nil, &Failure{Phase: Dial, Kind: classify(dialCtx, err, false)}
	}
	if config == nil {
		return &Connection{Conn: connection, offer: offered}, nil
	}
	secured := tls.Client(connection, config)
	if err := secured.HandshakeContext(dialCtx); err != nil {
		_ = secured.Close()
		failure := &Failure{Phase: Handshake, Kind: classify(dialCtx, err, offered.requested), Peer: connection.RemoteAddr().String()}
		var verification *tls.CertificateVerificationError
		if errors.As(err, &verification) {
			failure.Unverified = verification.UnverifiedCertificates
		}
		return nil, failure
	}
	return &Connection{Conn: secured, offer: offered}, nil
}

// offer is what a connection had to present and what the endpoint asked for.
// It is written during the handshake and read after it, on one goroutine.
type offer struct {
	hasCertificate bool
	requested      bool
}

// Connection is one open connection to a decided address.
type Connection struct {
	net.Conn
	offer *offer
}

// TLS is the session the connection negotiated, and false for a plain one.
func (c *Connection) TLS() (tls.ConnectionState, bool) {
	secured, ok := c.Conn.(*tls.Conn)
	if !ok {
		return tls.ConnectionState{}, false
	}
	return secured.ConnectionState(), true
}

// ClientCertificateRequested reports that the endpoint asked for a client
// certificate during the handshake.
func (c *Connection) ClientCertificateRequested() bool { return c.offer.requested }

// ClientCertificatePresented reports that the endpoint asked for a client
// certificate and this connection had one to present.
func (c *Connection) ClientCertificatePresented() bool {
	return c.offer.requested && c.offer.hasCertificate
}

// Classify names a failure on this connection after it opened. A TLS 1.3
// endpoint judges a client certificate after the client's handshake completes,
// so a rejected one arrives here rather than as a failure to open.
func (c *Connection) Classify(ctx context.Context, err error) Kind {
	return classify(ctx, err, c.offer.requested)
}

// Phase is where a connection could not be opened.
type Phase string

const (
	Dial      Phase = "dial"
	Handshake Phase = "tls"
)

// Kind names one transport or certificate failure. The set is closed and
// detailed, so each caller maps it onto its own vocabulary without keeping a
// second copy of the platform detail. A failure it cannot name is
// NetworkError, never a connection that was established.
type Kind string

const (
	Timeout           Kind = "timeout"
	Cancelled         Kind = "cancelled"
	ConnectionRefused Kind = "connection_refused"
	// Disconnected is a connection that ended: end of stream, or a use of a
	// connection already closed.
	Disconnected Kind = "disconnected"
	// ConnectionReset is a connection the platform reports reset, aborted or
	// broken, or a write that could not complete.
	ConnectionReset Kind = "connection_reset"
	NetworkError    Kind = "network_error"

	CertificateExpired Kind = "certificate_expired"
	UntrustedAuthority Kind = "untrusted_authority"
	HostnameMismatch   Kind = "hostname_mismatch"
	// CertificateUnverified is any other refusal to verify the presented
	// certificate.
	CertificateUnverified Kind = "certificate_unverified"
	// ClientCertificateRejected is an endpoint that asked for a client
	// certificate and then refused the connection at the TLS layer.
	ClientCertificateRejected Kind = "client_certificate_rejected"
	// HandshakeRefused is an endpoint that refused the connection at the TLS
	// layer without having asked for a client certificate.
	HandshakeRefused Kind = "handshake_refused"
)

// Verification reports whether the kind is readmit refusing to verify the
// certificate the endpoint presented, rather than the endpoint refusing readmit.
func (k Kind) Verification() bool {
	switch k {
	case CertificateExpired, UntrustedAuthority, HostnameMismatch, CertificateUnverified:
		return true
	}
	return false
}

// Failure is a connection that could not be opened.
type Failure struct {
	Phase Phase
	Kind  Kind
	// Peer is the address a connection reached before its handshake failed,
	// read from the connection itself. It is empty when the dial failed.
	Peer string
	// Unverified holds the certificates the endpoint presented when
	// verification refused them, so a certificate failure can be diagnosed
	// from the connection that failed. Nothing established that they identify
	// the endpoint.
	Unverified []*x509.Certificate
}

func (f *Failure) Error() string {
	return "the connection failed during " + string(f.Phase) + ": " + string(f.Kind)
}

// Classify names a failure on a connection that asked for no client
// certificate.
func Classify(ctx context.Context, err error) Kind { return classify(ctx, err, false) }

// classify names one failure. Expiry and cancellation of the caller's own
// context are decided first, so a deadline that interrupted the work is never
// reported as the endpoint's answer.
func classify(ctx context.Context, err error, requested bool) Kind {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return Timeout
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		return Cancelled
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
	// crypto/tls surfaces an alert the peer sent as a net.OpError whose
	// operation is "remote error" and keeps its own alert type unexported, so
	// the operation identifies one without matching an error message.
	var operation *net.OpError
	if errors.As(err, &operation) && operation.Op == "remote error" {
		if requested {
			return ClientCertificateRejected
		}
		return HandshakeRefused
	}
	var expired net.Error
	var verification *tls.CertificateVerificationError
	switch {
	case errors.As(err, &expired) && expired.Timeout():
		return Timeout
	case connectionRefused(err):
		return ConnectionRefused
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed):
		return Disconnected
	case errors.Is(err, io.ErrShortWrite) || connectionReset(err):
		return ConnectionReset
	case errors.As(err, &verification):
		return CertificateUnverified
	}
	return NetworkError
}
