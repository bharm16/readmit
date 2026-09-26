package receiver

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/mllp"
)

// frameDecision is what an owner's handler reports about one complete inbound
// frame. err is a session-wide failure that stops the whole session; stop
// offers the session's end, which the loop still measures against what the
// session was asked for; usable reports whether this connection may carry
// another frame.
type frameDecision struct {
	stop   bool
	usable bool
	err    error
}

// frameLoop serves one connection through the sequence the fixture receiver and
// the generic collector share: admission, awaiting the peer's next frame,
// claiming the slot, the refused-frame drain, the read itself, and the retained
// consumed prefix when a read fails. What one complete frame does — what to
// answer, and whether this connection or the session goes on — belongs to the
// owner's handler.
func (r *recorder) frameLoop(ctx context.Context, connection net.Conn, state *stream, bounds limits, answer func(context.Context, net.Conn, *stream, []byte) frameDecision) (bool, error) {
	reader, _ := mllp.NewReader(deadlineReader{connection, r, bounds.idle}, bounds.maxFrameBytes)
	var refused []byte
	// A coalesced TCP read may include frames beyond --max-messages, and a
	// refusal drains the frame its peer had already begun. Both are retained as
	// received evidence, never claimed as answered.
	defer func() { r.close(state, append(reader.Buffered(), refused...)) }()
	for {
		admitted, err := r.admitFrame(state, bounds)
		if err != nil {
			return false, err
		}
		// A reached quota or a stop another connection decided ends this one
		// before a frame is read, so a message this session would not retain
		// stays in the sender's socket rather than being consumed and dropped.
		if !admitted {
			return r.complete(ctx, bounds.maxMessages), nil
		}
		if !r.awaiting(state, connection) {
			return true, nil
		}
		// The slot in a declared message limit is claimed only once this peer
		// has actually begun the next frame. A slot claimed while merely waiting
		// is promised on behalf of a peer that may have finished sending, and a
		// limit spent on promises like that starves a peer whose frame has
		// already arrived.
		if err := reader.Await(); err != nil {
			r.engaged(state)
			// An idle or disconnected peer never began another frame. Anything
			// it did send is read-ahead the deferred close still retains.
			return false, nil
		}
		if !r.reserveFrame(bounds) {
			// Every frame still to come is promised to a peer already reading
			// one, so this peer's is refused. It is drained rather than left in
			// the socket: closing over it would reset this connection instead of
			// ending it, and the reset would take whatever was answered earlier
			// along with it.
			buffered := reader.Buffered()
			refused = append(buffered, r.drainRefused(connection, bounds.maxFrameBytes+frameReserve-len(buffered))...)
			r.engaged(state)
			// A refusal is not the end of the session: the frames it is still
			// waiting for have not arrived. Only a session that has read every
			// frame it was asked for finishes here.
			return r.complete(ctx, bounds.maxMessages), nil
		}
		raw, readErr := reader.ReadFrame()
		r.engaged(state)
		if readErr != nil {
			r.releaseFrame()
			raw = append(raw, reader.Buffered()...)
			if len(raw) > 0 {
				r.retain(state, raw, bundle.Inbound)
			}
			// A malformed, oversized, idle, or disconnected peer leaves its
			// consumed prefix in the final case without any answer claim.
			return false, nil
		}
		decision := answer(ctx, connection, state, raw)
		if decision.err != nil {
			return false, decision.err
		}
		if decision.stop {
			return r.complete(ctx, bounds.maxMessages), nil
		}
		if r.complete(ctx, bounds.maxMessages) {
			return true, nil
		}
		if !decision.usable {
			// A header this receiver cannot answer safely, or a frame this
			// fixture cannot apply, ends the connection rather than guessing at
			// whatever the peer sends next.
			return false, nil
		}
	}
}

// writeAnswer sends one acknowledgement on the connection that carried its
// frame: the write deadline first, then the bytes through send, and exactly the
// bytes the socket took are retained. send exists so the collector can record
// its journal intent around the write. A connection that cannot take a deadline
// is reported through deadlineErr separately, because owners treat that
// differently from a refused answer: the fixture ends as a session error, and
// the collector records a stage that was not written completely.
func (r *recorder) writeAnswer(connection net.Conn, state *stream, idle time.Duration, ack []byte, what string, send func() (int, error)) (deadlineErr, answerErr error) {
	if err := connection.SetWriteDeadline(time.Now().Add(idle)); err != nil {
		return fmt.Errorf("cannot set %s write deadline", what), nil
	}
	sent, err := send()
	if sent > 0 {
		r.retain(state, ack[:sent], bundle.Outbound)
	}
	return nil, err
}

func writeAll(writer io.Writer, data []byte) (int, error) {
	total := 0
	for total < len(data) {
		n, err := writer.Write(data[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
