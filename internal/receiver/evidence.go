package receiver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

type evidenceChunk struct {
	start, end  int
	observation bundle.Observation
}

// stream is one connection's own recording state. Nothing in it is shared, so a
// connection appends its bytes and reconciles its frame boundaries without
// touching another connection's evidence. ordinal is the case source this
// connection became, claimed the first time it retained a byte.
type stream struct {
	input   bundle.Input
	chunks  []evidenceChunk
	ordinal int
}

func newStream() *stream {
	return &stream{input: bundle.Input{Options: hl7.Options{Format: hl7.MLLP}, Observations: make(map[int]bundle.Observation)}}
}

// recorder accumulates the exact bytes one bounded MLLP session read and wrote,
// with each nonempty connection as a distinct case source. The fixture receiver
// and the generic collector share it so both retain evidence identically.
//
// Connections may be served concurrently, so every member below is guarded by
// mu. A connection's own bytes live in its stream and never take the lock; the
// lock covers the session totals, the ordered source slots, and the stop
// decision every connection reads.
type recorder struct {
	mu                           sync.Mutex
	inputs                       []bundle.Input
	totalBytes, received, events int
	stopped                      bool
	// pending counts connections that have been accepted and have not yet
	// claimed a case source. The accept loop counts them against the source
	// limit, because each of them may still become one: without that, several
	// connections in flight could together claim more sources than the case
	// contract holds, and the session would only discover it at finalization,
	// where the whole capture would fail to seal.
	pending int
	// reserving counts connections that have been allowed to read one more
	// frame and have not yet produced it. A declared message limit counts them,
	// because each is a frame this session has already committed to accept:
	// without that, connections in flight would together read past the limit by
	// however many were reading at once.
	reserving int
	// closeListener stops the accept loop from blocking after a controlled
	// stop. It is installed by sessions and called at most once.
	closeListener func()
	// waiting holds the connections blocked on their next frame. A controlled
	// stop expires exactly those reads. It expires rather than closes, because
	// a connection that has just taken a frame and is about to answer it may
	// not have left this map yet: a past read deadline ends a blocked read and
	// cannot interrupt the write of an acknowledgement already begun.
	waiting map[*stream]net.Conn
	// onSource declares a newly claimed source while the lock is held, so an
	// ordered list the owner keeps beside the case sources is claimed in the
	// same order. It is nil for an owner that keeps no such list.
	onSource func(ordinal int)
}

// claimLocked reserves this connection's case source. It runs under the lock so
// the source order, and any ordered list the owner keeps beside it, are decided
// in one place rather than by whichever goroutine finishes first.
func (r *recorder) claimLocked(s *stream) {
	if s.ordinal != 0 {
		return
	}
	r.inputs = append(r.inputs, bundle.Input{})
	r.pending--
	s.ordinal = len(r.inputs)
	if r.onSource != nil {
		r.onSource(s.ordinal)
	}
}

func (r *recorder) retain(s *stream, raw []byte, direction bundle.Direction) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.retainLocked(s, raw, direction)
}

func (r *recorder) retainLocked(s *stream, raw []byte, direction bundle.Direction) string {
	r.claimLocked(s)
	sequence := len(s.input.Observations) + 1
	now := time.Now().UTC()
	s.input.Data = append(s.input.Data, raw...)
	s.input.Observations[sequence] = bundle.Observation{Direction: direction, ObservedAt: &now}
	s.chunks = append(s.chunks, evidenceChunk{start: len(s.input.Data) - len(raw), end: len(s.input.Data), observation: s.input.Observations[sequence]})
	r.totalBytes += len(raw)
	return fmt.Sprintf("s%04d-e%06d", s.ordinal, sequence)
}

func (r *recorder) retainBufferedLocked(s *stream, data []byte) {
	reader, _ := mllp.NewReader(bytes.NewReader(data), len(data)+1)
	for {
		raw, err := reader.ReadFrame()
		if err != nil {
			raw = append(raw, reader.Buffered()...)
		}
		if len(raw) > 0 {
			r.retainLocked(s, raw, bundle.Inbound)
		}
		if err != nil {
			return
		}
	}
}

// frameObservations reconciles transport chunks with the bundle's exact MLLP
// boundary policy. A truncated outbound ACK followed by buffered inbound bytes
// is one unparsed suffix, with unknown direction rather than a fabricated
// complete ACK.
func frameObservations(s *stream) {
	observations := make(map[int]bundle.Observation)
	for start, sequence := 0, 1; start < len(s.input.Data); sequence++ {
		end, _ := bundle.NextOccurrence(s.input.Data, start, hl7.MLLP)
		var observed bundle.Observation
		for _, chunk := range s.chunks {
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
	s.input.Observations = observations
}

// close retains buffered read-ahead bytes and fills this connection's reserved
// source slot when it carried any evidence. It reports whether a source exists.
func (r *recorder) close(s *stream, buffered []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.waiting, s)
	r.retainBufferedLocked(s, buffered)
	if s.ordinal == 0 {
		// This connection carried nothing, so it never became a source and
		// releases the room the accept loop reserved for it.
		r.pending--
		return false
	}
	frameObservations(s)
	r.events += len(s.input.Observations)
	r.inputs[s.ordinal-1] = s.input
	return true
}

// admitSource reserves room for one more connection to become a case source. It
// answers before a connection is served, so a session refuses a peer it could
// not retain rather than accepting bytes it would have to throw away.
func (r *recorder) admitSource() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.inputs)+r.pending >= bundle.MaxSources {
		return false
	}
	r.pending++
	return true
}

// sources returns the finalized case sources in claim order. A reserved slot is
// always filled: an ordinal is claimed only when a byte is retained, and every
// connection closes its own stream before the session finalizes.
func (r *recorder) sources() []bundle.Input {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inputs
}

// limits are the bounds one session serves under. maxFrameBytes and maxMessages
// are what this session was configured with; maxConnections, maxSessions and
// maxCaptureBytes are the operator's declared capacity, and reaching one of
// those is a controlled stop rather than an error. idle bounds one read and one
// acknowledgement write.
type limits struct {
	maxFrameBytes   int
	maxMessages     int
	maxConnections  int
	maxSessions     int
	maxCaptureBytes int
	idle            time.Duration
}

// sessions serves bounded connections over listener, which it owns and closes.
// Cancellation interrupts a blocked accept and every connection in flight.
// handle processes a single connection and reports whether the session is
// complete.
//
// At most maxConnections connections are served at once. An additional peer is
// not accepted until a slot frees, so the sender feels the limit as TCP
// backpressure rather than as a message this session accepted and dropped.
func (r *recorder) sessions(ctx context.Context, listener net.Listener, handle func(context.Context, net.Conn, *stream) (bool, error), bounds limits) (failure error) {
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var group sync.WaitGroup
	var once sync.Once
	var reported error
	// Connections in flight finish their own frame before the session
	// finalizes, so every accepted byte reaches the retained evidence.
	defer func() {
		group.Wait()
		if failure == nil {
			failure = reported
		}
	}()
	r.mu.Lock()
	r.closeListener = sync.OnceFunc(func() { _ = listener.Close() })
	r.mu.Unlock()
	slots := make(chan struct{}, max(1, bounds.maxConnections))
	served := 0
	for {
		if ctx.Err() != nil || r.stopping() {
			return nil
		}
		if bounds.maxSessions > 0 && served >= bounds.maxSessions {
			// The declared connection budget is spent. Accepting stops here,
			// which keeps the next peer's messages in its own socket instead of
			// in a receiver that would not retain them. The connections already
			// accepted still finish; a budget bounds how many peers are served,
			// not how long the last of them may take.
			return nil
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return nil
		}
		connection, err := listener.Accept()
		if err != nil {
			<-slots
			if ctx.Err() != nil || r.stopping() {
				return nil
			}
			return errors.New("session cannot accept connection")
		}
		if !r.admitSource() {
			connection.Close()
			<-slots
			return errors.New("session reached source limit")
		}
		served++
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			stopConnection := context.AfterFunc(ctx, func() { _ = connection.Close() })
			done, err := handle(ctx, connection, newStream())
			stopConnection()
			connection.Close()
			if err != nil {
				once.Do(func() { reported = err })
				r.stop()
				return
			}
			if done {
				r.stop()
			}
		}()
	}
}

// stop ends this session in a controlled way: the listener stops accepting, the
// reads of connections waiting for their next frame expire, and every
// connection already answering a frame finishes that answer before its evidence
// is sealed.
func (r *recorder) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *recorder) stopLocked() {
	r.stopped = true
	if r.closeListener != nil {
		r.closeListener()
	}
	for s, connection := range r.waiting {
		_ = connection.SetReadDeadline(time.Now())
		delete(r.waiting, s)
	}
}

func (r *recorder) stopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

// awaiting registers a connection as blocked on its next frame and reports
// whether it may wait at all. A session already stopping answers false, so a
// connection never begins a read the finalized evidence will not cover.
func (r *recorder) awaiting(s *stream, connection net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return false
	}
	if r.waiting == nil {
		r.waiting = make(map[*stream]net.Conn)
	}
	r.waiting[s] = connection
	return true
}

// engaged records that this connection now holds a frame to answer, so a
// controlled stop leaves it alone until that answer is finished.
func (r *recorder) engaged(s *stream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.waiting, s)
}

// validBounds accepts the per-session frame, timeout, and message limits that
// both the fixture receiver and the generic collector enforce.
func validBounds(maxFrameBytes int, idle time.Duration, maxMessages int) error {
	if maxFrameBytes < 1 || maxFrameBytes > bundle.MaxSourceBytes-frameReserve || idle <= 0 || maxMessages < 0 || maxMessages > MaxMessages {
		return errors.New("invalid session frame, timeout, or message limit")
	}
	return nil
}

// complete reports that this session has read every frame it was asked for,
// that it has been stopped, or that cancellation ended it. Buffered read-ahead
// beyond the limit stays in the evidence but is never processed.
func (r *recorder) complete(ctx context.Context, maxMessages int) bool {
	r.mu.Lock()
	reached := maxMessages > 0 && r.received >= maxMessages
	stopped := r.stopped
	r.mu.Unlock()
	return reached || stopped || ctx.Err() != nil
}

// admitFrame reports whether this session still has room for one more frame of
// at most maxFrameBytes, and an error for a structural limit of the case
// contract that this session no longer fits.
//
// It answers before a connection waits for its peer's next frame, so a quota
// another connection spent ends this one as soon as it is spent rather than
// whenever its own peer happens to send again. No frame is consumed before it
// admits, so a frame is never taken that the finalized evidence could not hold.
// That is the whole of the backpressure a bound applies: the frames a
// reached bound will not take stay in their senders' sockets, where the senders
// still see them, rather than being dropped inside a receiver that promised to
// keep them. Only reserveFrame refuses a frame that has already begun arriving,
// and that one is drained into the evidence rather than left to be reset over.
//
// Admission is room in the session, not this connection's claim on it. A
// declared message limit is claimed by reserveFrame, once the frame that will
// fill the claim has actually begun arriving.
func (r *recorder) admitFrame(s *stream, bounds limits) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return false, nil
	}
	// bufio reads at most 4096 bytes ahead: fewer than 1400 minimum-size
	// frames. Reserve those slots even if a peer sends only bad frames.
	if r.events+len(s.input.Observations) > bundle.MaxEvents-1400 {
		return false, errors.New("session reached occurrence limit")
	}
	if len(s.input.Data)+bounds.maxFrameBytes+frameReserve > bundle.MaxSourceBytes || r.totalBytes+bounds.maxFrameBytes+frameReserve > bundle.MaxEvidenceBytes {
		return false, errors.New("session reached evidence byte limit")
	}
	if r.received+r.reserving >= MaxMessages {
		return false, errors.New("session reached message limit")
	}
	// A declared message limit or capture quota is what the operator asked for,
	// so reaching one stops the capture in a controlled way. The structural
	// limits above are the case contract's own, and a session that no longer
	// fits one of them is an error rather than a completed capture.
	if bounds.maxMessages > 0 && r.received >= bounds.maxMessages ||
		bounds.maxCaptureBytes > 0 && r.totalBytes+bounds.maxFrameBytes+frameReserve > bounds.maxCaptureBytes {
		r.stopLocked()
		return false, nil
	}
	return true, nil
}

// reserveFrame claims one frame of a declared message limit for the connection
// whose peer has begun sending it, and reports whether the claim was granted.
//
// It is asked only once bytes of that frame have arrived, so every reservation
// stands for a frame that exists. A connection that claimed a slot while merely
// waiting would hold it on behalf of a peer that had finished sending, and a
// limit spent on promises like that refuses a peer whose frame has already
// arrived: the frames the limit counts as promised must be frames in flight.
//
// A reservation is released again by nextFrame when the frame arrives, or by
// releaseFrame when the connection ends instead.
func (r *recorder) reserveFrame(bounds limits) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return false
	}
	// The case contract's own cap is checked again here, not only in
	// admitFrame, because the claim is what must not overshoot it: several
	// connections can pass admission before any of them claims.
	if r.received+r.reserving >= MaxMessages {
		return false
	}
	// The remaining frames are all promised to connections already reading one.
	// This connection is refused a frame it would have to read past the limit,
	// and only this connection: the session is not finished, because the frames
	// it is still waiting for have not arrived.
	if bounds.maxMessages > 0 && r.received+r.reserving >= bounds.maxMessages {
		return false
	}
	r.reserving++
	return true
}

// releaseFrame gives back a frame this connection was admitted to read and did
// not, so a peer that disconnected instead of sending does not consume part of
// a declared message limit.
func (r *recorder) releaseFrame() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reserving--
}

// nextFrame turns an admitted frame into the session-wide ordinal of one
// complete inbound frame. A declared fault plan selects by that ordinal and an
// acknowledgement's own control ID carries it, so it is issued once, under the
// lock, and travels with the frame rather than being read again later.
func (r *recorder) nextFrame() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reserving--
	r.received++
	return r.received
}

type deadlineReader struct {
	net.Conn
	session *recorder
	idle    time.Duration
}

func (d deadlineReader) Read(data []byte) (int, error) {
	if err := d.session.armRead(d.Conn, d.idle); err != nil {
		return 0, err
	}
	return d.Conn.Read(data)
}

// refusalLinger bounds the wait for the rest of a refused frame. The frame had
// already begun arriving when it was refused, so this waits for the tail of one
// message that is on its way, never for a peer to decide to say something.
const refusalLinger = 250 * time.Millisecond

// halfCloser is a connection that can give up its sending half on its own, so a
// refused peer learns at once that nothing more is coming. Not every transport
// is one: net.Pipe is not, and it cannot emit RST either.
type halfCloser interface{ CloseWrite() error }

// drainRefused ends a connection that was not allowed to read the frame its
// peer had already begun sending, and returns the bytes it drained so the
// caller can retain them.
//
// Closing a TCP socket that still holds unconsumed inbound bytes emits RST
// rather than FIN, and a reset also discards whatever this connection had
// already written and the peer had not yet read. A refusal would then reach the
// peer as a reset connection, and could destroy acknowledgements this capture
// had already sent and claimed, rather than as the orderly close it is. So the
// peer is told there is nothing more to read, and the frame it had begun is
// drained into the evidence it belongs in: retained as received, never claimed
// as acknowledged, which is what the bound promised about a frame it would not
// answer.
//
// Draining stops at budget, which callers set to the room this session had just
// admitted less whatever it has already buffered, so a refusal cannot carry a
// source past a limit the admission immediately before it had checked. It is
// therefore an orderly close for one refused frame and no more: a peer that goes
// on sending past that budget, or past refusalLinger, is still reset, because
// the alternative is a receiver that reads whatever it is sent to be polite.
//
// The caller leaves this connection registered as waiting, and the deadline is
// armed through armRead under the lock a controlled stop takes, so a stop that
// arrives during a drain expires it exactly as it expires a read and a drain
// cannot overwrite a stop's own expiry. A connection that will not take a
// deadline is already gone, so there is nothing left to drain and nothing a
// capture-wide error would add.
func (r *recorder) drainRefused(connection net.Conn, budget int) []byte {
	if half, ok := connection.(halfCloser); ok {
		_ = half.CloseWrite()
	}
	if err := r.armRead(connection, refusalLinger); err != nil {
		return nil
	}
	var drained []byte
	buffer := make([]byte, 4096)
	for len(drained) < budget {
		read, err := connection.Read(buffer[:min(len(buffer), budget-len(drained))])
		drained = append(drained, buffer[:read]...)
		if err != nil {
			return drained
		}
	}
	return drained
}

// armRead gives one read its idle deadline, under the same lock a controlled
// stop takes. Arming and stopping cannot interleave, so a stop's expiry is
// never overwritten by the deadline of a read that had already decided to wait,
// and a read armed just before a stop is woken by the expiry that stop sets.
func (r *recorder) armRead(connection net.Conn, idle time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	deadline := time.Now().Add(idle)
	if r.stopped {
		deadline = time.Now()
	}
	if err := connection.SetReadDeadline(deadline); err != nil {
		return errors.New("cannot set the session read deadline")
	}
	return nil
}
