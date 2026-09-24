// Package destination opens every connection readmit makes to a configured
// destination, for one declared purpose: a send, an observation's read, the one
// connection a fixture reset may open, and a connectivity check.
//
// The rule it owns is "connect only to the address that was decided". The
// destination is decided by internal/sendpolicy, within the connection's
// budget; the decision is retained through the caller's recorder before it is
// used; the purpose admits the connection or refuses it; and an admitted
// connection dials exactly the one address the decision checked, never a
// second resolution of the configured name. TLS is completed through
// internal/transportsecurity, and a failure is named in one detailed
// classification each caller maps onto its own vocabulary.
//
// The resolver is the seam. Everything else a caller reaches through this
// package is the production implementation, so a test that substitutes the
// resolver proves which address a connection reached.
package destination

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

// Purpose is why a connection is opened. It fixes whether a send is requested
// of the policy, what admits the connection, and whether resolving the name
// spends the connection's budget.
type Purpose int

const (
	// Send is an explicit send. The decision must allow it, and resolving
	// the name and opening the connection share one connect budget.
	Send Purpose = iota + 1
	// Observe is an explicit read of an approved source, held to the rule a
	// send is held to. A source is read again for every sample of a window,
	// so each connection it opens has the whole declared bound.
	Observe
	// ResetCheck is the one connection a fixture reset may open. A reset
	// requests no send, so what admits it is that nothing about the
	// destination refuses: the decision's send_not_explicit. Like a send, it
	// connects on the decision's authority and shares the connect budget.
	ResetCheck
	// Check reports the decision a send would get without requesting one. It
	// admits nothing and refuses nothing: a check's connection reaches the
	// configured address whatever the decision said, because diagnosing an
	// endpoint no policy approves is what a check is for. A preview is a
	// check that opens nothing.
	Check
)

// Request is one destination a caller is about to reach, and why.
type Request struct {
	Purpose Purpose
	// Address is the host and port the configuration declares.
	Address string
	// Classification is the class recorded for the environment, exactly as
	// it was written.
	Classification string
	// Policy is the explicitly selected approved-destination document, or
	// nil when the operator selected none.
	Policy *sendpolicy.Policy
	// Budget bounds resolving the name, and then each connection the route
	// opens: dialling and TLS setup together.
	Budget time.Duration
	// Record retains the decision before anything uses it. A failure to
	// retain it stops the caller: a decision nobody can read afterwards is not
	// evidence that one was made. It is optional.
	Record func(sendpolicy.Decision) error
	// Resolve turns the configured name into the addresses it resolves to
	// now. Nil is the operating system's resolver.
	Resolve sendpolicy.Resolver
}

// Decision is what the send policy answered for one purpose, already retained.
type Decision struct {
	sendpolicy.Decision
	route    Route
	admitted bool
}

// Route is the connection this decision admits for its purpose. It reports
// false when the purpose may not connect: the policy denied the destination,
// or the decision did not establish exactly one address. A check is always
// admitted, to the configured address.
func (d Decision) Route() (Route, bool) {
	return d.route, d.admitted
}

// Decide asks internal/sendpolicy about the destination for the request's
// purpose, within the request's budget, and retains the decision through the
// request's recorder before returning it. It opens nothing. The only error is
// the recorder's.
func Decide(ctx context.Context, request Request) (Decision, error) {
	resolve := request.Resolve
	if resolve == nil {
		resolve = sendpolicy.SystemResolver
	}
	deadline := time.Now().Add(request.Budget)
	decisionCtx, cancel := context.WithDeadline(ctx, deadline)
	answer := sendpolicy.Decide(decisionCtx, request.Policy, sendpolicy.Request{
		Address: request.Address, Classification: request.Classification,
		Explicit: request.Purpose == Send || request.Purpose == Observe,
	}, resolve)
	remaining := max(time.Duration(0), time.Until(deadline))
	cancel()
	host, port, _ := net.SplitHostPort(request.Address)
	decided := Decision{Decision: answer}
	switch request.Purpose {
	case Check:
		decided.admitted = true
	case ResetCheck:
		decided.admitted = answer.Reason == sendpolicy.SendNotExplicit && len(answer.ResolvedAddresses) == 1
	default:
		decided.admitted = answer.Allowed && len(answer.ResolvedAddresses) == 1
	}
	if decided.admitted {
		decided.route = Route{address: request.Address, host: host, budget: request.Budget}
		// The admitted address is taken before the decision is retained, so a
		// recorder holding the decision's slices cannot change what is dialled.
		if request.Purpose != Check {
			decided.route.address = net.JoinHostPort(answer.ResolvedAddresses[0], port)
		}
		// Resolving is part of connecting when the connection follows at once
		// on the decision's authority. An observation reads many times over a
		// window, and a check's decision is only reported, so neither spends it.
		if request.Purpose == Send || request.Purpose == ResetCheck {
			decided.route.budget = remaining
		}
	}
	if request.Record != nil {
		if err := request.Record(answer); err != nil {
			return Decision{Decision: answer}, err
		}
	}
	return decided, nil
}

// Route reaches one decided address. The zero Route reaches nothing.
type Route struct {
	// address is what is dialled: the address the decision checked, or the
	// configured address for a check.
	address string
	// host is the configured host, the name a certificate is verified
	// against when the configuration declares none.
	host   string
	budget time.Duration
}

// Address is the host and port this route dials.
func (r Route) Address() string { return r.address }

// DialContext opens one TCP connection to the route's address, whatever
// network and address the caller names, within the route's budget. It is the
// dialer a protocol that negotiates TLS itself — an HTTP client, a database
// driver, including any redirection it attempts — is given, so no re-resolution
// or failover can widen the decision.
func (r Route) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, r.budget)
	defer cancel()
	return (&net.Dialer{}).DialContext(dialCtx, "tcp", r.address)
}

// Security is the TLS a connection completes: the name the endpoint's
// certificate is verified against, the authorities that replace the platform
// roots, and the client certificate presented when the endpoint asks for one.
type Security struct {
	// ServerName is the name verified. Empty is the configured host, never
	// the address the decision pinned.
	ServerName string
	// Authorities are PEM certificates that replace the platform roots.
	Authorities []byte
	// Certificate is presented whenever the endpoint asks for a client
	// certificate. Without one, an empty certificate is presented.
	Certificate *tls.Certificate
}

// ClientConfig is the TLS configuration a protocol that negotiates TLS itself
// uses on this route. The rule is internal/transportsecurity's; this settles
// which name is verified and which certificate is presented.
func (r Route) ClientConfig(security Security) (*tls.Config, error) {
	name := security.ServerName
	if name == "" {
		name = r.host
	}
	config, err := transportsecurity.ClientConfig(name, security.Authorities)
	if err != nil {
		return nil, err
	}
	config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		if security.Certificate == nil {
			return &tls.Certificate{}, nil
		}
		return security.Certificate, nil
	}
	return config, nil
}
