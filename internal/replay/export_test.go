package replay

import (
	"context"
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
