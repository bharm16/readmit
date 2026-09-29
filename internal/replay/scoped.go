package replay

import (
	"context"
	"errors"
	"net"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// SendScoped reuses the existing one-connection, no-retry sender and frozen run
// writer. The connected owner retains its environment, grant, TLS and policy
// envelope separately. A route for another endpoint or purpose cannot send.
func SendScoped(ctx context.Context, p *Plan, output string, route destination.ScopedRoute, security *destination.Security, observer Observer) (*Run, error) {
	if p == nil || !p.scoped || observer == nil || !route.Matches(p.target.Address, sendpolicy.V2Stimulus) {
		return nil, errors.New("scoped replay requires matching authority and durable observer")
	}
	if p.target.Transport == "plain" && security != nil || p.target.Transport == "tls" && (security == nil || security.ServerName != p.target.ServerName || digest(security.Authorities) != digest(p.ca)) {
		return nil, errors.New("scoped replay TLS differs")
	}
	if p.target.ClientCertificate == "" && security != nil && security.Certificate != nil {
		return nil, errors.New("undeclared scoped client certificate")
	}
	if p.target.ClientCertificate != "" && (security == nil || security.Certificate == nil) {
		return nil, errors.New("scoped client certificate required")
	}
	return executeObserved(ctx, p, output, scopedObserver{ctx, route, observer}, func(ctx context.Context, _ *Plan) (net.Conn, *TransportError) {
		conn, err := route.Open(ctx, security)
		if err != nil {
			var failure *destination.Failure
			if !errors.As(err, &failure) {
				return nil, &TransportError{Phase: "dial", Class: "network"}
			}
			return nil, connectionFailure(err)
		}
		if listener, ok := observer.(interface {
			Connected(*destination.Connection) error
		}); ok {
			if err := listener.Connected(conn); err != nil {
				conn.Close()
				return nil, connectionFailure(err)
			}
		}
		return conn, nil
	})
}

type scopedObserver struct {
	ctx   context.Context
	route destination.ScopedRoute
	Observer
}

// Await forwards the connected owner's pacing; the route's authority check
// still runs in BeforeSend, after the wait.
func (o scopedObserver) Await(ctx context.Context, id string) error {
	if pacer, ok := o.Observer.(Pacer); ok {
		return pacer.Await(ctx, id)
	}
	return nil
}

func (o scopedObserver) BeforeSend(id string) error {
	if err := o.Observer.BeforeSend(id); err != nil {
		return err
	}
	return o.route.Check(o.ctx)
}

// ScopedDestination verifies physical containment before connected retention.
func (p *Plan) ScopedDestination(output string) (string, error) { return checkDestination(p, output) }
