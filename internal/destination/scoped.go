package destination

import (
	"context"
	"errors"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
	"sync/atomic"
	"time"
)

// ScopedRoute can only be obtained after a fresh action authorization and a
// recorded scoped policy decision. Its fields cannot be edited by a caller.
type ScopedRoute struct {
	route     Route
	check     func(context.Context) error
	operation sendpolicy.Operation
	address   string
	used      *atomic.Bool
	deadline  time.Time
}
type ScopedRequest struct {
	Policy    sendpolicy.ScopedPolicy
	Request   sendpolicy.ScopedRequest
	Budget    time.Duration
	Resolve   sendpolicy.Resolver
	Authorize func(context.Context) error
	Record    func(sendpolicy.ScopedDecision) error
}

func AdmitScoped(ctx context.Context, r ScopedRequest) (ScopedRoute, error) {
	if r.Authorize == nil || r.Record == nil || r.Budget <= 0 || r.Budget > 5*time.Minute {
		return ScopedRoute{}, errors.New("scoped connection requires bounded authority and retention")
	}
	ctx, cancel := context.WithTimeout(ctx, r.Budget)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := r.Authorize(ctx); err != nil {
		return ScopedRoute{}, err
	}
	d := sendpolicy.DecideScoped(ctx, r.Policy, r.Request, r.Resolve)
	host, port, _ := net.SplitHostPort(r.Request.Address)
	route := Route{address: net.JoinHostPort(d.SelectedAddress, port), host: host, budget: r.Budget}
	if err := r.Record(d); err != nil {
		return ScopedRoute{}, err
	}
	if !d.Allowed || ctx.Err() != nil {
		return ScopedRoute{}, errors.New("scoped destination refused")
	}
	if err := r.Authorize(ctx); err != nil {
		return ScopedRoute{}, err
	}
	return ScopedRoute{route: route, check: r.Authorize, operation: r.Request.Operation, address: r.Request.Address, used: new(atomic.Bool), deadline: deadline}, nil
}
func (r ScopedRoute) Open(ctx context.Context, security *Security) (*Connection, error) {
	if r.check == nil || r.operation == sendpolicy.CaptureListen || !r.used.CompareAndSwap(false, true) {
		return nil, errors.New("no outbound scoped authority")
	}
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	route := r.route
	route.budget = time.Until(r.deadline)
	if route.budget <= 0 {
		return nil, errors.New("scoped connection budget exhausted")
	}
	return route.Open(ctx, security)
}
func (r ScopedRoute) Listen(ctx context.Context) (net.Listener, error) {
	if r.check == nil || r.operation != sendpolicy.CaptureListen || !r.used.CompareAndSwap(false, true) {
		return nil, errors.New("no listener scoped authority")
	}
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	if !time.Now().Before(r.deadline) {
		return nil, errors.New("scoped listener budget exhausted")
	}
	return (&net.ListenConfig{}).Listen(ctx, "tcp", r.route.address)
}
func (r ScopedRoute) Address() string { return r.route.Address() }

// Matches prevents an admitted route being substituted for another action.
func (r ScopedRoute) Matches(address string, operation sendpolicy.Operation) bool {
	return r.check != nil && r.address == address && r.operation == operation
}

// Check revalidates authority immediately before a payload write.
func (r ScopedRoute) Check(ctx context.Context) error {
	if r.check == nil {
		return errors.New("no scoped authority")
	}
	return r.check(ctx)
}
