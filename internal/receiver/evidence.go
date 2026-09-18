package receiver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

type evidenceChunk struct {
	start, end  int
	observation bundle.Observation
}

// recorder accumulates the exact bytes one bounded MLLP session read and wrote,
// with each nonempty connection as a distinct case source. The fixture receiver
// and the generic collector share it so both retain evidence identically.
type recorder struct {
	inputs                       []bundle.Input
	chunks                       []evidenceChunk
	totalBytes, received, events int
}

func (r *recorder) retain(input *bundle.Input, raw []byte, direction bundle.Direction) string {
	sequence := len(input.Observations) + 1
	now := time.Now().UTC()
	input.Data = append(input.Data, raw...)
	input.Observations[sequence] = bundle.Observation{Direction: direction, ObservedAt: &now}
	r.chunks = append(r.chunks, evidenceChunk{start: len(input.Data) - len(raw), end: len(input.Data), observation: input.Observations[sequence]})
	r.totalBytes += len(raw)
	return fmt.Sprintf("s%04d-e%06d", len(r.inputs)+1, sequence)
}

func (r *recorder) retainBuffered(input *bundle.Input, data []byte) {
	reader, _ := mllp.NewReader(bytes.NewReader(data), len(data)+1)
	for {
		raw, err := reader.ReadFrame()
		if err != nil {
			raw = append(raw, reader.Buffered()...)
		}
		if len(raw) > 0 {
			r.retain(input, raw, bundle.Inbound)
		}
		if err != nil {
			return
		}
	}
}

// Reconcile transport chunks with the bundle's exact MLLP boundary policy. A
// truncated outbound ACK followed by buffered inbound bytes is one unparsed
// suffix, with unknown direction rather than a fabricated complete ACK.
func (r *recorder) frameObservations(input *bundle.Input) {
	observations := make(map[int]bundle.Observation)
	for start, sequence := 0, 1; start < len(input.Data); sequence++ {
		end, _ := bundle.NextOccurrence(input.Data, start, hl7.MLLP)
		var observed bundle.Observation
		for _, chunk := range r.chunks {
			if chunk.end <= start || chunk.start >= end {
				continue
			}
			if observed.ObservedAt == nil {
				observed = chunk.observation
			} else if observed.Direction != chunk.observation.Direction {
				observed.Direction = bundle.Unknown
			}
		}
		observations[sequence] = observed
		start = end
	}
	input.Observations = observations
}

// close retains buffered read-ahead bytes and adds the connection as a source
// when it carried any evidence. It reports whether a source was added.
func (r *recorder) close(input *bundle.Input, buffered []byte) bool {
	r.retainBuffered(input, buffered)
	if len(input.Data) == 0 {
		return false
	}
	r.frameObservations(input)
	r.events += len(input.Observations)
	r.inputs = append(r.inputs, *input)
	return true
}

// sessions serializes one bounded foreground session over listener, which it
// owns and closes. Cancellation interrupts a blocked accept. handle processes a
// single connection and reports whether the session is complete.
func (r *recorder) sessions(ctx context.Context, listener net.Listener, handle func(context.Context, net.Conn) (bool, error)) error {
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("session cannot accept connection")
		}
		if len(r.inputs) >= bundle.MaxSources {
			connection.Close()
			return errors.New("session reached source limit")
		}
		stopConnection := context.AfterFunc(ctx, func() { _ = connection.Close() })
		done, err := handle(ctx, connection)
		stopConnection()
		connection.Close()
		if err != nil {
			return err
		}
		if done || ctx.Err() != nil {
			return nil
		}
	}
}

// validBounds accepts the per-session frame, timeout, and message limits that
// both the fixture receiver and the generic collector enforce.
func validBounds(maxFrameBytes int, idle time.Duration, maxMessages int) error {
	if maxFrameBytes < 1 || maxFrameBytes > bundle.MaxSourceBytes-frameReserve || idle <= 0 || maxMessages < 0 || maxMessages > MaxMessages {
		return errors.New("invalid session frame, timeout, or message limit")
	}
	return nil
}

// complete reports that this session has read every frame it was asked for, or
// that cancellation ended it. Buffered read-ahead beyond the limit stays in the
// evidence but is never processed.
func (r *recorder) complete(ctx context.Context, maxMessages int) bool {
	return maxMessages > 0 && r.received >= maxMessages || ctx.Err() != nil
}

// withinLimits reports the first session limit this connection has reached,
// before another frame of at most maxFrameBytes is read and acknowledged.
func (r *recorder) withinLimits(input *bundle.Input, maxFrameBytes int) error {
	// bufio reads at most 4096 bytes ahead: fewer than 1400 minimum-size
	// frames. Reserve those slots even if a peer sends only bad frames.
	if r.events+len(input.Observations) > bundle.MaxEvents-1400 {
		return errors.New("session reached occurrence limit")
	}
	if len(input.Data)+maxFrameBytes+frameReserve > bundle.MaxSourceBytes || r.totalBytes+maxFrameBytes+frameReserve > bundle.MaxEvidenceBytes {
		return errors.New("session reached evidence byte limit")
	}
	if r.received >= MaxMessages {
		return errors.New("session reached message limit")
	}
	return nil
}

type deadlineReader struct {
	net.Conn
	idle time.Duration
}

func (r deadlineReader) Read(data []byte) (int, error) {
	if err := r.SetReadDeadline(time.Now().Add(r.idle)); err != nil {
		return 0, err
	}
	return r.Conn.Read(data)
}
