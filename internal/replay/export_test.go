package replay

import (
	"context"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
)

// ExecuteWithConnectionForTest substitutes only the network boundary. The send,
// outcome classification, retained evidence and finalization are the same as Execute.
// Application builds always use the real, policy-checked connector.
func ExecuteWithConnectionForTest(ctx context.Context, plan *Plan, output string, connection net.Conn) (*Run, error) {
	return execute(ctx, plan, output, func(context.Context, *Plan) (net.Conn, *TransportError) {
		return connection, nil
	})
}

// ExecuteWithResolverForTest substitutes DNS only; approval, recording, dialing,
// TLS, payload exchange and evidence storage use the production implementation.
func ExecuteWithResolverForTest(ctx context.Context, plan *Plan, output string, policy *sendpolicy.Policy, record func(sendpolicy.Decision) error, resolve sendpolicy.Resolver) (*Run, error) {
	return executeWithPolicy(ctx, plan, output, policy, record, resolve)
}
