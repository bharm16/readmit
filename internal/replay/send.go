package replay

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// DecisionSuffix names the decision a send retains beside its run folder, as
// every caller of this package has retained it. The sidecar's spelling lives
// here, where the decision is written, and nowhere else.
const DecisionSuffix = ".decision.json"

// SendOptions is the one options value a replay's preview and send take: the
// send policy decided against, where the one decision is retained, and the
// durability boundary of the send itself.
type SendOptions struct {
	// Policy is the send policy decided against. nil asks the loopback-only
	// defaults a preview and a send without a selected policy are held to.
	Policy *sendpolicy.Policy
	// DecisionPath is where the decision reached before anything is opened is
	// retained, including a refusal. Empty retains it through Record alone.
	DecisionPath string
	// DecisionDurability is how the decision file at DecisionPath is written.
	DecisionDurability artifactdir.Durability
	// Record observes every decision reached, before the sidecar is written,
	// and its error refuses the send. The command line and the window capture
	// the decision here to report it.
	Record func(sendpolicy.Decision) error
	// Observer is the synchronous durability boundary of the send itself.
	Observer Observer
}

// recorder composes the one decision retention a preview or send performs:
// the caller's capture first, then the decision file this package owns the
// spelling of. A failed write refuses, with the decision already captured.
func (o SendOptions) recorder() (func(sendpolicy.Decision) error, error) {
	if o.Policy != nil && o.Record == nil && o.DecisionPath == "" {
		return nil, errors.New("a selected send policy requires a decision recorder")
	}
	return func(value sendpolicy.Decision) error {
		if o.Record != nil {
			if err := o.Record(value); err != nil {
				return err
			}
		}
		if o.DecisionPath != "" {
			return sendpolicy.WriteDecisionWithDurability(o.DecisionPath, value, o.DecisionDurability)
		}
		return nil
	}, nil
}

// Preview prepares what one replay would send and asks the send policy its
// question without requesting a send, retaining that decision before anything
// is prepared. It opens no connection and writes no run. resolver answers the
// decision reached here.
func Preview(ctx context.Context, sourcePath string, target Target, selection Options, execution SendOptions) (*Plan, error) {
	return prepare(ctx, sourcePath, target, selection, false, execution, sendpolicy.SystemResolver)
}

// PrepareSend prepares one explicit send. A destination recorded as production
// is refused by Prepare for every caller; the refusal is decided and retained
// here first, before preparation refuses it, so it survives as inspectable
// evidence. Every other send is not decided here: it is decided again at the
// point of the send, by Send, which retains its own decision before it opens
// anything.
func PrepareSend(ctx context.Context, sourcePath string, target Target, selection Options, execution SendOptions) (*Plan, error) {
	return prepare(ctx, sourcePath, target, selection, true, execution, sendpolicy.SystemResolver)
}

func prepare(ctx context.Context, sourcePath string, target Target, selection Options, send bool, execution SendOptions, resolver sendpolicy.Resolver) (*Plan, error) {
	if !send || sendpolicy.RefusesEverySend(string(target.Environment().Classification)) {
		record, err := execution.recorder()
		if err != nil {
			return nil, err
		}
		purpose := destination.Check
		if send {
			purpose = destination.Send
		}
		duration, _ := time.ParseDuration(target.ConnectTimeout)
		if _, err := destination.Decide(ctx, destination.Request{
			Purpose: purpose, Address: target.Address, Classification: string(target.Environment().Classification),
			Policy: execution.Policy, Budget: duration, Record: record, Resolve: resolver,
		}); err != nil {
			return nil, err
		}
	}
	return Prepare(sourcePath, target, selection)
}

// Send is the only replay operation that accesses the network. A new run is
// reserved before dialing; transport failures are finalized evidence in Events,
// not returned errors. Errors indicate invalid input or failure to store
// evidence. There is one dial operation, one outstanding message, and no
// reconnect/retry. The send policy is decided at the point of the send and the
// decision is retained before anything is opened, including a denial.
func Send(ctx context.Context, plan *Plan, output string, execution SendOptions) (*Run, error) {
	record, err := execution.recorder()
	if err != nil {
		return nil, err
	}
	return sendWithResolver(ctx, plan, output, execution.Policy, record, sendpolicy.SystemResolver, execution.Observer)
}

func sendWithResolver(ctx context.Context, plan *Plan, output string, policy *sendpolicy.Policy, record func(sendpolicy.Decision) error, resolve sendpolicy.Resolver, observer Observer) (*Run, error) {
	if plan == nil || len(plan.messages) == 0 {
		return nil, errors.New("replay requires a prepared plan")
	}
	if policy != nil && record == nil {
		return nil, errors.New("a selected send policy requires a decision recorder")
	}
	duration, _ := time.ParseDuration(plan.target.ConnectTimeout)
	decision, err := destination.Decide(ctx, destination.Request{
		Purpose: destination.Send, Address: plan.target.Address, Classification: string(plan.target.Environment().Classification),
		Policy: policy, Budget: duration, Record: record, Resolve: resolve,
	})
	if err != nil {
		return nil, err
	}
	route, admitted := decision.Route()
	if !admitted {
		return nil, errors.New("the send was refused by policy before anything was sent: " + string(decision.Reason))
	}
	// File persistence is not network connection time. DNS consumes the
	// connection budget; recording and reserving evidence do not.
	return executeObserved(ctx, plan, output, observer, func(ctx context.Context, p *Plan) (net.Conn, *TransportError) {
		return connect(ctx, p, route)
	})
}
