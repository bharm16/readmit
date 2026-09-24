package operation

import (
	"context"
	"time"

	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// PrepareReplay prepares the selected messages of one case for one target the
// way `readmit replay` does, for the command line and the window alike, so
// neither holds a send decision of its own.
//
// A preview (send false) asks the send policy its question without requesting
// a send, and record retains that decision before anything is prepared. So does
// a send to an environment recorded as production, whose refusal is retained
// before preparation refuses it, so it survives as inspectable evidence. Every
// other send is decided again at the point of the send, by
// replay.ExecuteWithPolicy, which retains its own decision before it opens
// anything. resolver answers the decision reached here. A nil record retains
// nothing; a failed record refuses before the plan is prepared.
//
// The plan is nil whenever preparation refused. Nothing here opens a
// connection or writes a run.
func PrepareReplay(ctx context.Context, casePath string, target replay.Target, options replay.Options, policy *sendpolicy.Policy, send bool, record func(sendpolicy.Decision) error, resolver sendpolicy.Resolver) (*replay.Plan, error) {
	if resolver == nil {
		resolver = sendpolicy.SystemResolver
	}
	if !send || sendpolicy.RefusesEverySend(string(target.Environment().Classification)) {
		duration, _ := time.ParseDuration(target.ConnectTimeout)
		decisionCtx, stop := context.WithTimeout(ctx, duration)
		decision := sendpolicy.Decide(decisionCtx, policy, sendpolicy.Request{
			Address: target.Address, Classification: string(target.Environment().Classification), Explicit: send,
		}, resolver)
		stop()
		if record != nil {
			if err := record(decision); err != nil {
				return nil, err
			}
		}
	}
	return replay.Prepare(casePath, target, options)
}
