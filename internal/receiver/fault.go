package receiver

import (
	"context"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/mllp"
)

const reasonInjectedReject = "declared test fault rejected this stage"
const reasonInjectedMissing = "declared test fault withheld this stage"
const reasonInjectedDisconnect = "declared test fault disconnected before this stage"
const reasonInjectedMalformed = "declared test fault sent a malformed acknowledgement"
const reasonFaultCancelled = "declared test fault was interrupted"

func (c *Collector) rejectStage(stage string) bool {
	step := c.config.Policy.Faults.Step(c.received)
	return step != nil && step.Stage == stage && step.Action == "reject"
}

func waitFault(ctx context.Context, milliseconds int) bool {
	timer := time.NewTimer(time.Duration(milliseconds) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

// answerStage applies a single declarative fault at an ordinary ACK seam.
// It never manufactures an ACK when the sender did not request that stage.
// true ends this connection; later connections may continue the ordinal plan.
func (c *Collector) answerStage(ctx context.Context, input *bundle.Input, conn net.Conn, result outcome, stage string) bool {
	ack := result.acceptACK
	recorded := &c.answering().Accept
	if stage == collection.ApplicationStage {
		ack = result.applicationACK
		recorded = &c.answering().Application
	}
	step := c.config.Policy.Faults.Step(c.received)
	selected := step != nil && step.Stage == stage
	if ack == nil {
		if selected {
			c.answering().Fault.Status = "not-requested"
		}
		return false
	}
	stop := false
	if selected {
		c.answering().Fault.Status = "interrupted"
		switch step.Action {
		case "delay", "missing-response":
			if !waitFault(ctx, step.DelayMS) {
				downgrade(recorded, reasonFaultCancelled)
				if stage == collection.AcceptStage {
					downgrade(&c.answering().Application, reasonStageNotSent)
				}
				return true
			}
			if step.Action == "missing-response" {
				downgrade(recorded, reasonInjectedMissing)
				if stage == collection.AcceptStage {
					downgrade(&c.answering().Application, reasonStageNotSent)
				}
				c.answering().Fault.Status = "completed"
				return true
			}
		case "disconnect":
			downgrade(recorded, reasonInjectedDisconnect)
			if stage == collection.AcceptStage {
				downgrade(&c.answering().Application, reasonStageNotSent)
			}
			c.answering().Fault.Status = "completed"
			return true
		case "malformed-ack":
			// Fixed, non-PHI, complete MLLP framing around deliberately invalid HL7.
			// Complete framing keeps subsequent retained occurrences unambiguous.
			ack = mllp.Frame([]byte("READMIT MALFORMED ACK\r"))
			result.applicationACK = ack
			stop = true
		}
	}
	var err error
	if stage == collection.AcceptStage {
		err = c.sendHere(conn, input, ack)
		if err != nil {
			downgrade(recorded, reasonPartialWrite)
		}
	} else {
		err = c.deliverApplication(ctx, input, conn, result)
	}
	if selected {
		if err == nil && recorded.Code != collection.NotAcknowledged {
			c.answering().Fault.Status = "completed"
		}
		if step.Action == "malformed-ack" && recorded.Code != collection.NotAcknowledged {
			downgrade(recorded, reasonInjectedMalformed)
		}
	}
	if err != nil || stop {
		if stage == collection.AcceptStage {
			downgrade(&c.answering().Application, reasonStageNotSent)
		}
		return true
	}
	return false
}
