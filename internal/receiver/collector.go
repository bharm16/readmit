package receiver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

// Echoed header bytes are bounded before they are spliced into a composed
// acknowledgement. The bound is the collection record's own control-ID limit,
// so an acknowledged control ID always fits the evidence that records it.
const collectorMaxEchoBytes = collection.MaxControlIDBytes

// Reasons are letters and spaces only, so they carry no delimiter of any
// declared encoding and disclose no message value on the wire or in evidence.
const (
	reasonUnparseable    = "frame is not supported HL7 syntax"
	reasonNoControlID    = "message header does not carry a usable control ID"
	reasonNoVersion      = "message header does not declare a usable version"
	reasonWideType       = "message type exceeds the supported text bound"
	reasonEnhancedMode   = "enhanced acknowledgement mode is not supported by the configured receiver policy"
	reasonUnknownMode    = "message header declares an acknowledgement condition this receiver does not support"
	reasonUnacceptedType = "message type is not accepted by the configured receiver policy"
	reasonNotComposable  = "acknowledgement could not be composed for this header"
	reasonPartialWrite   = "acknowledgement was not written completely"
	reasonNotRequested   = "sender declared no acknowledgement of this stage"
	reasonStageNotSent   = "acknowledgement stage was not reached after the preceding stage failed"
	reasonUndelivered    = "separate application acknowledgement endpoint did not accept the frame in time"
	reasonJournalStopped = "acknowledgement was not sent because the capture journal stopped"
)

// errJournalStopped is what a send reports when the capture journal could not
// record the intent to make it. Nothing was written to the peer, so the stage
// records that rather than a partial write, and the capture stops.
var errJournalStopped = errors.New("capture journal stopped before an acknowledgement was sent")

// downgradeSend records why a stage did not answer. A journal that stopped and
// a write that did not complete are different facts about the same stage, and
// the record states the one that happened.
func downgradeSend(stage *collection.Stage, err error) {
	if errors.Is(err, errJournalStopped) {
		downgrade(stage, reasonJournalStopped)
		return
	}
	downgrade(stage, reasonPartialWrite)
}

type CollectorConfig struct {
	Policy             collection.Policy
	OutputPath         string
	JournalPath        string // empty means this capture cannot be recovered after a kill
	MaxFrameBytes      int
	IdleTimeout        time.Duration
	ApplicationTimeout time.Duration // bounds one separate application-ACK delivery
	MaxMessages        int           // zero means run until cancellation or a session limit
	// MaxConnections is how many peers are served at once. Zero means one.
	MaxConnections int
	// MaxSessions is how many connections this capture serves in total, and
	// MaxCaptureBytes how many bytes it retains. Zero declares neither, leaving
	// only the structural limits of the case contract. Reaching a declared
	// budget is a controlled stop; reaching a structural limit is an error,
	// because a session that no longer fits its own contract did not complete.
	MaxSessions     int
	MaxCaptureBytes int
	// TLS and ClientCertificate describe the listener the caller hands to
	// Serve. They are recorded, never applied here: wrapping the socket is the
	// caller's, so one TLS rule serves every path.
	TLS               bool
	ClientCertificate bool
}

// Collector is a generic bounded MLLP receiver. It retains every byte it reads
// and writes, acknowledges receipt according to its declarative policy, and
// labels each retained source with the declared origin of that evidence. It
// applies no message: neither a commit acknowledgement nor an application
// acknowledgement reports that a downstream application processed anything.
//
// Up to MaxConnections peers are served at once, each becoming its own case
// source. Every frame is answered by the connection that carried it, so
// concurrency changes how many peers are served and nothing about what any one
// of them is told.
type Collector struct {
	recorder
	config    CollectorConfig
	startedAt time.Time
	record    collection.Record
	journal   *capturejournal.Writer
	summary   *capturejournal.Summary
	served    bool
}

func NewCollector(config CollectorConfig) (*Collector, error) {
	if err := config.Policy.Validate(); err != nil {
		return nil, err
	}
	// Own a validated snapshot: a caller cannot change approvals or executable
	// behavior later by mutating slices or pointers supplied at construction.
	if config.Policy.Faults != nil {
		encoded, err := collection.EncodePolicy(config.Policy)
		if err != nil {
			return nil, err
		}
		config.Policy, err = collection.DecodePolicy(encoded)
		if err != nil {
			return nil, err
		}
	}
	if err := validBounds(config.MaxFrameBytes, config.IdleTimeout, config.MaxMessages); err != nil {
		return nil, err
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = 1
	}
	if config.MaxConnections < 1 || config.MaxConnections > MaxConnections {
		return nil, errors.New("concurrent connections must be between 1 and 64")
	}
	if config.MaxSessions < 0 || config.MaxSessions > bundle.MaxSources {
		return nil, errors.New("the declared connection budget must be between 0 and 128")
	}
	if config.MaxCaptureBytes < 0 || config.MaxCaptureBytes > bundle.MaxEvidenceBytes {
		return nil, errors.New("the declared capture quota must be between 0 and 64 MiB")
	}
	if config.MaxCaptureBytes > 0 && config.MaxCaptureBytes < config.MaxFrameBytes+frameReserve {
		return nil, errors.New("the declared capture quota must hold at least one frame of the declared size")
	}
	if config.Policy.Faults != nil && config.MaxConnections != 1 {
		// A fault plan selects by the session-wide ordinal of inbound frames.
		// Concurrent connections decide that ordinal by arrival, so the plan
		// would name a different message on every run. Refuse rather than
		// simulate a fault against whichever frame happened to arrive first.
		return nil, errors.New("a fault policy selects messages by session ordinal and requires one connection at a time")
	}
	if config.ApplicationTimeout <= 0 || config.ApplicationTimeout > 5*time.Minute {
		return nil, errors.New("application acknowledgement timeout must be positive and at most five minutes")
	}
	if config.OutputPath == "" {
		return nil, errors.New("collector requires a new case destination")
	}
	output, err := artifactpath.Destination(config.OutputPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return nil, errors.New("case destination must be new")
	}
	if config.JournalPath != "" {
		journal, err := artifactpath.Destination(config.JournalPath)
		if err != nil {
			return nil, err
		}
		if journal == output {
			return nil, errors.New("case and capture journal destinations must differ")
		}
		config.JournalPath = journal
	}
	config.OutputPath = output
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, errors.New("cannot initialize collector session")
	}
	c := &Collector{config: config, startedAt: time.Now().UTC()}
	c.record = collection.Record{
		Schema: collection.Schema, SessionID: hex.EncodeToString(entropy[:]), Policy: config.Policy,
		ApplicationProcessing: collection.NoApplicationProcessing,
		Sessions:              []collection.Session{}, Received: []collection.Received{},
	}
	if config.Policy.Faults != nil {
		c.record.Schema = collection.FaultSchema
	}
	// A connection becomes a case source and a recorded session in the same
	// step, under the recorder's own lock, so concurrent connections cannot
	// claim the two in different orders.
	c.onSource = c.declareSession
	if config.JournalPath == "" {
		return c, nil
	}
	policy, err := collection.EncodePolicy(config.Policy)
	if err != nil {
		return nil, err
	}
	journal, err := capturejournal.Create(config.JournalPath, capturejournal.Capture{
		CreatedAt: c.startedAt, SessionID: c.record.SessionID,
		PolicyName: config.Policy.Name, PolicySchema: config.Policy.Schema, PolicySHA256: digestOf(policy),
		Limits: capturejournal.Limits{MaxConnections: config.MaxConnections, MaxSessions: config.MaxSessions,
			MaxMessages: config.MaxMessages, MaxCaptureBytes: config.MaxCaptureBytes, MaxFrameBytes: config.MaxFrameBytes},
		Transport: capturejournal.Transport{TLS: config.TLS, ClientCertificate: config.ClientCertificate},
	})
	if err != nil {
		return nil, err
	}
	// Case-insensitive filesystems can alias distinct resolved leaf spellings,
	// so exclusivity is confirmed again now that the journal directory exists.
	// A case destination that is no longer new would be sealed over the journal
	// at finalization, losing the whole capture after it had been acknowledged.
	if outputInfo, err := os.Lstat(output); !os.IsNotExist(err) {
		if journalInfo, journalErr := os.Lstat(config.JournalPath); err == nil && journalErr == nil && os.SameFile(outputInfo, journalInfo) {
			if err := journal.Discard(); err != nil {
				return nil, err
			}
		} else {
			journal.Close()
		}
		return nil, errors.New("case destination is no longer new; the capture was not started")
	}
	c.journal = journal
	return c, nil
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Journal is the capture journal's own summary once the session finalized, or
// nil when no journal was configured. It states what was received and which
// acknowledgements have an unknown effect; it is not a verdict about a test.
func (c *Collector) Journal() *capturejournal.Summary { return c.summary }

func (c *Collector) bounds() limits {
	return limits{maxFrameBytes: c.config.MaxFrameBytes, maxMessages: c.config.MaxMessages,
		maxConnections: c.config.MaxConnections, maxSessions: c.config.MaxSessions, maxCaptureBytes: c.config.MaxCaptureBytes}
}

// Serve owns and closes listener. Cancellation interrupts a blocked accept,
// read, or acknowledgement write and then finalizes the retained bytes.
//
// A declared connection budget, capture quota or message limit stops the
// capture in a controlled way instead: the listener stops accepting, peers
// waiting for their next frame are closed, and every connection already
// answering a frame finishes that answer before the case is sealed.
//
// An abrupt process termination cannot finalize in-memory wire evidence. A
// configured journal is what survives one, and it is retained as the capture
// runs rather than at the end.
func (c *Collector) Serve(ctx context.Context, listener net.Listener) (*bundle.Bundle, error) {
	if c.served {
		return nil, errors.New("collector session has already been served")
	}
	c.served = true
	if c.config.Policy.Faults != nil {
		if err := c.config.Policy.Faults.ApproveEndpoint(listener.Addr().String()); err != nil {
			listener.Close()
			c.finalizeJournal(ctx, errors.New("refused"))
			return nil, err
		}
	}
	err := c.sessions(ctx, listener, c.connection, c.bounds())
	c.orderReceipts()
	c.finalizeJournal(ctx, err)
	b, writeErr := bundle.WriteCollected(c.config.OutputPath, c.sources(), c.startedAt, c.record)
	if writeErr != nil {
		return nil, errors.Join(err, writeErr)
	}
	return b, err
}

// orderReceipts seals the receipts in evidence order. Concurrent connections
// commit in whatever order they answer, and the record lists complete inbound
// frames in the order the case itself lists those occurrences: source by
// source, in sequence. Occurrence identifiers are zero padded, so ordering them
// as text is ordering them as evidence. A record from a concurrent capture then
// reads exactly the way a sequential one does.
func (c *Collector) orderReceipts() {
	c.mu.Lock()
	defer c.mu.Unlock()
	slices.SortFunc(c.record.Received, func(a, b collection.Received) int {
		return strings.Compare(a.OccurrenceID, b.OccurrenceID)
	})
}

// finalizeJournal records where the capture stopped. Cancellation and an
// execution error keep #85's own names; a capture that stopped because it
// reached what the operator declared is finalized, which is not a pass: a
// capture evaluates no assertion and returns no verdict.
func (c *Collector) finalizeJournal(ctx context.Context, err error) {
	if c.journal == nil {
		return
	}
	stop := capturejournal.Finalized
	switch {
	case err != nil:
		stop = durablerun.ExecutionError
	case errors.Is(ctx.Err(), context.Canceled):
		stop = durablerun.Cancelled
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		stop = durablerun.TimedOut
	}
	summary := c.journal.Finish(stop)
	c.summary = &summary
	_ = c.journal.Close()
}

func (c *Collector) connection(ctx context.Context, connection net.Conn, state *stream) (bool, error) {
	bounds := c.bounds()
	reader, _ := mllp.NewReader(deadlineReader{connection, &c.recorder, c.config.IdleTimeout}, c.config.MaxFrameBytes)
	var refused []byte
	defer func() {
		// A coalesced TCP read may include frames beyond --max-messages, and a
		// refusal drains the frame its peer had already begun. Both are
		// retained as received evidence, never claimed as acknowledged.
		c.close(state, append(reader.Buffered(), refused...))
	}()
	for {
		admitted, err := c.admitFrame(state, bounds)
		if err != nil {
			return false, err
		}
		// A reached quota or a stop another connection decided ends this one
		// before a frame is read, so a message this capture would not retain
		// stays in the sender's socket rather than being consumed and dropped.
		if !admitted {
			return c.complete(ctx, c.config.MaxMessages), nil
		}
		if !c.awaiting(state, connection) {
			return true, nil
		}
		// The slot in a declared message limit is claimed only once this peer
		// has actually begun the next frame. A slot claimed while merely
		// waiting is promised on behalf of a peer that may have finished
		// sending, and a limit spent on promises like that starves a peer whose
		// frame has already arrived.
		if err := reader.Await(); err != nil {
			c.engaged(state)
			// An idle or disconnected peer never began another frame. Anything
			// it did send is read-ahead the deferred close still retains.
			return false, nil
		}
		if !c.reserveFrame(bounds) {
			// Every frame still to come is promised to a peer already reading
			// one, so this peer's is refused. It is drained rather than left in
			// the socket: closing over it would reset this connection instead
			// of ending it, and the reset would take the acknowledgements this
			// capture has already sent along with it.
			buffered := reader.Buffered()
			refused = append(buffered, c.drainRefused(connection, bounds.maxFrameBytes+frameReserve-len(buffered))...)
			c.engaged(state)
			// A refusal is not the end of the session: the frames it is still
			// waiting for have not arrived. Only a session that has read every
			// frame it was asked for finishes here.
			return c.complete(ctx, c.config.MaxMessages), nil
		}
		raw, readErr := reader.ReadFrame()
		c.engaged(state)
		if readErr != nil {
			c.releaseFrame()
			raw = append(raw, reader.Buffered()...)
			if len(raw) > 0 {
				c.retain(state, raw, bundle.Inbound)
			}
			// A malformed, oversized, idle, or disconnected peer leaves its
			// consumed prefix in the final case without any receipt claim.
			return false, nil
		}
		stop, usable, err := c.frame(ctx, connection, state, raw)
		if err != nil {
			return false, err
		}
		if stop {
			return c.complete(ctx, c.config.MaxMessages), nil
		}
		if c.complete(ctx, c.config.MaxMessages) {
			return true, nil
		}
		if !usable {
			// A header this receiver cannot answer safely ends the connection
			// rather than guessing a correlation for the next frame.
			return false, nil
		}
	}
}

// frame handles one complete inbound frame on the connection that carried it.
// The receipt is preflighted and, when a journal is configured, the frame's
// bytes are synced before any stage is answered, so the evidence of what was
// received always survives the acknowledgement that claims it.
func (c *Collector) frame(ctx context.Context, connection net.Conn, state *stream, raw []byte) (stop bool, usableHeader bool, err error) {
	id := c.retain(state, raw, bundle.Inbound)
	ordinal := c.nextFrame()
	session := sessionName(state.ordinal)
	result := c.decide(raw, ordinal)
	if c.journal != nil {
		if err := c.journal.Received(session, id, result.controlID, raw); err != nil {
			return false, false, err
		}
	}
	entry := collection.Received{SessionID: session, OccurrenceID: id, ControlID: result.controlID, Mode: result.mode, Accept: result.accept, Application: result.application}
	if step := c.config.Policy.Faults.Step(ordinal); step != nil {
		entry.Fault = &collection.FaultEvent{Action: step.Action, Stage: step.Stage, Status: "not-reached"}
	}
	index, err := c.commit(entry)
	if err != nil {
		return false, false, err
	}
	// The receipt is updated in one place when the frame is done, so a stage
	// that downgrades never publishes a half-written entry to a concurrent
	// connection preflighting its own receipt.
	defer func() { c.store(index, entry) }()
	// The accept stage answers here; only the application stage may travel.
	for _, stage := range []string{collection.AcceptStage, collection.ApplicationStage} {
		stop := c.answerStage(ctx, state, connection, result, stage, ordinal, &entry)
		if err := c.journalError(); err != nil {
			return false, false, err
		}
		if stop {
			return true, result.usableHeader, nil
		}
	}
	return false, result.usableHeader, nil
}

// sendJournal records one acknowledgement stage around the write that sends it.
// The intent is synced first, so a process killed during the write leaves an
// intent with no completion, which recovery reports as uncertain and never
// resolves by sending again.
//
// The error it returns is the send's own. A journal failure is sticky and is
// read back through journalError, because it stops the whole capture rather
// than describing what one acknowledgement did.
func (c *Collector) sendJournal(entry *collection.Received, stage string, send func() (int, error)) (int, error) {
	if c.journal == nil {
		return send()
	}
	if err := c.journal.Intent(entry.SessionID, entry.OccurrenceID, stage, stageOf(entry, stage).Destination); err != nil {
		return 0, errJournalStopped
	}
	sent, err := send()
	// A write that did not complete is recorded as what it was. It resolves the
	// intent, because the outcome is known, and it is never counted as an
	// acknowledgement: bytes may have reached the peer, and the peer that never
	// read them was not acknowledged.
	if err != nil {
		_ = c.journal.Unsent(entry.SessionID, entry.OccurrenceID, stage, sent)
		return sent, err
	}
	_ = c.journal.Sent(entry.SessionID, entry.OccurrenceID, stage, sent)
	return sent, nil
}

// journalError reports a capture that can no longer state what it did. The
// capture stops there: continuing would answer peers whose frames the recovery
// record would not name.
func (c *Collector) journalError() error {
	if c.journal == nil {
		return nil
	}
	return c.journal.Err()
}

func stageOf(entry *collection.Received, stage string) *collection.Stage {
	if stage == collection.AcceptStage {
		return &entry.Accept
	}
	return &entry.Application
}

// deliverApplication sends the application stage where the policy declares. A
// separate endpoint is a second transport: a failure or timeout there is an
// execution error recorded against this frame, never an application result, and
// it does not end the healthy connection that delivered the message.
func (c *Collector) deliverApplication(ctx context.Context, state *stream, connection net.Conn, result outcome, entry *collection.Received) error {
	if result.application.Destination == collection.SameConnection {
		if err := c.sendStage(connection, state, entry, collection.ApplicationStage, result.applicationACK); err != nil {
			downgradeSend(&entry.Application, err)
			return err
		}
		return nil
	}
	if c.config.Policy.Faults != nil {
		if err := c.config.Policy.Faults.ApproveEndpoint(c.config.Policy.Enhanced.ApplicationEndpoint); err != nil {
			downgrade(&entry.Application, reasonUndelivered)
			return err
		}
	}
	dialer := net.Dialer{Timeout: c.config.ApplicationTimeout}
	endpoint, err := dialer.DialContext(ctx, "tcp", c.config.Policy.Enhanced.ApplicationEndpoint)
	if err != nil {
		// Nothing reached the peer, so nothing is retained and no stage is
		// claimed. An absent application acknowledgement is not a rejection.
		downgrade(&entry.Application, reasonUndelivered)
		return nil
	}
	defer endpoint.Close()
	// Cancellation interrupts a blocked delivery write as well as a blocked
	// accept or receive; bytes already sent are still retained below.
	stop := context.AfterFunc(ctx, func() { _ = endpoint.Close() })
	defer stop()
	if err := endpoint.SetWriteDeadline(time.Now().Add(c.config.ApplicationTimeout)); err != nil {
		downgrade(&entry.Application, reasonUndelivered)
		return nil
	}
	sent, writeErr := c.sendJournal(entry, collection.ApplicationStage, func() (int, error) {
		return writeAll(endpoint, result.applicationACK)
	})
	if sent > 0 {
		c.retain(state, result.applicationACK[:sent], bundle.Outbound)
	}
	if writeErr != nil {
		// Bytes already sent cannot be retracted, and a peer that never read
		// them was not acknowledged. Retain both facts explicitly.
		downgradeSend(&entry.Application, writeErr)
	}
	return nil
}

// sendStage writes one acknowledgement on the connection that delivered the
// message and retains exactly the bytes the socket took.
func (c *Collector) sendStage(connection net.Conn, state *stream, entry *collection.Received, stage string, ack []byte) error {
	if err := connection.SetWriteDeadline(time.Now().Add(c.config.IdleTimeout)); err != nil {
		return errors.New("cannot set collector write deadline")
	}
	sent, err := c.sendJournal(entry, stage, func() (int, error) { return writeAll(connection, ack) })
	if sent > 0 {
		c.retain(state, ack[:sent], bundle.Outbound)
	}
	return err
}

// downgrade records that a stage this session intended to answer did not reach
// its peer. The stage keeps no code, control ID, or destination, because none
// of those happened; the reason states what did.
func downgrade(stage *collection.Stage, reason string) {
	if stage.Code == collection.NotAcknowledged {
		return
	}
	*stage = collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: reason}
}

// declareSession runs as the recorder claims this connection's case source,
// with the recorder's lock already held. Claiming both in one step is what
// keeps the recorded sessions in the same order as the case sources when
// several connections are served at once.
func (c *Collector) declareSession(ordinal int) {
	if len(c.record.Sessions) >= ordinal {
		return
	}
	c.record.Sessions = append(c.record.Sessions, collection.Session{
		SessionID: sessionName(ordinal), SourceID: fmt.Sprintf("s%04d", ordinal), Label: c.config.Policy.SourceLabel})
}

// sessionName is how a connection's ordinal is written wherever the record
// names the connection, so the declaration and the frames agree by construction.
func sessionName(ordinal int) string { return fmt.Sprintf("c%04d", ordinal) }

// commit preflights the record before anything is acknowledged, leaving room
// for the longest reason each stage can substitute. A finalized session can
// then always encode testimony for every acknowledgement it actually sent. It
// returns the receipt's own position, which is how the frame's own goroutine
// updates exactly its own entry when the stages are done.
func (c *Collector) commit(entry collection.Received) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.record.Received) >= collection.MaxReceived {
		return 0, errors.New("collector session reached collection record limit")
	}
	candidate := c.record
	received := make([]collection.Received, 0, len(c.record.Received)+1)
	candidate.Received = append(append(received, c.record.Received...), entry)
	data, err := collection.Encode(candidate)
	if err != nil || len(data)+2*collection.MaxReasonBytes+16 > collection.MaxBytes {
		return 0, errors.New("collector session reached collection record size limit")
	}
	c.record = candidate
	return len(candidate.Received) - 1, nil
}

// store publishes the finished receipt for one frame. Only the frame's own
// goroutine writes its own position, and it does so under the recorder's lock
// so a concurrent preflight always encodes a complete record.
func (c *Collector) store(index int, entry collection.Received) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.record.Received[index] = entry
}

type outcome struct {
	controlID           string
	mode                string
	accept, application collection.Stage
	acceptACK           []byte
	applicationACK      []byte
	// usableHeader is false when this receiver could not read the header well
	// enough to answer it, which ends the connection rather than guessing a
	// correlation for whatever the peer sends next.
	usableHeader bool
}

func unreadable(controlID, reason string) outcome {
	return outcome{controlID: controlID, mode: collection.UnknownMode,
		accept:      collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: reason},
		application: collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: reason}}
}

func declined(reason string) collection.Stage {
	return collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination, Reason: reason}
}

// decide applies the declarative policy to one complete frame. It reads only
// the header values an acknowledgement needs; it never interprets the message.
func (c *Collector) decide(raw []byte, ordinal int) outcome {
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		return unreadable("", reasonUnparseable)
	}
	message := doc.Messages[0]
	header := message.Segments[0]
	control := doc.Bytes(header.Field(10).Span)
	if header.Field(10).State != hl7.Present || len(control) > collectorMaxEchoBytes || !utf8.Valid(control) {
		return unreadable("", reasonNoControlID)
	}
	controlID := string(control)
	version := doc.Bytes(header.Field(12).Span)
	if header.Field(12).State != hl7.Present || len(version) > collectorMaxEchoBytes {
		return unreadable(controlID, reasonNoVersion)
	}
	declaredType := doc.Bytes(header.Field(9).Span)
	if len(declaredType) > collectorMaxEchoBytes {
		return unreadable(controlID, reasonWideType)
	}
	parts := bytes.SplitN(declaredType, []byte{message.Delimiters.Component}, 3)
	var trigger []byte
	if len(parts) > 1 {
		trigger = parts[1]
	}
	accepted := c.config.Policy.Accepts(string(parts[0]), string(trigger))
	acceptCondition, applicationCondition := condition(doc, header.Field(15)), condition(doc, header.Field(16))
	composer := acknowledgementComposer{doc: doc, control: control, version: version, trigger: trigger}
	if acceptCondition == "" && applicationCondition == "" {
		return c.original(composer, controlID, accepted, ordinal)
	}
	return c.enhanced(composer, controlID, accepted, acceptCondition, applicationCondition, ordinal)
}

// undeclaredCondition stands in for a populated MSH-15/MSH-16 value this
// receiver will not read further. It is deliberately not a declared condition,
// so it can only ever take the unsupported-mode path.
const undeclaredCondition = "?"

// condition returns a populated MSH-15/MSH-16 value, bounded so an oversized
// field is read as an undeclared condition rather than echoed anywhere. An
// empty or omitted field asks for nothing, which is original mode.
func condition(doc *hl7.Document, field hl7.Field) string {
	if !populated(field) {
		return ""
	}
	value := doc.Bytes(field.Span)
	if len(value) > 8 || !utf8.Valid(value) {
		return undeclaredCondition
	}
	return string(value)
}

// original answers the single acknowledgement of original mode, whose MSA-1 is
// an application-stage code. There is no accept stage in original mode.
func (c *Collector) original(composer acknowledgementComposer, controlID string, accepted bool, ordinal int) outcome {
	code, reason := c.config.Policy.Acknowledgement.Code, ""
	if c.rejectStage(collection.ApplicationStage, ordinal) {
		code, reason = collection.RejectCode, reasonInjectedReject
	}
	if !accepted {
		code, reason = collection.RejectCode, reasonUnacceptedType
	}
	return c.singleAnswer(composer, controlID, collection.OriginalMode, code, reason, ordinal)
}

// singleAnswer composes one original-mode acknowledgement on the receiving
// connection. That is the whole of original mode, and it is also the refusal an
// unsupported enhanced request gets, because answering inside a protocol this
// receiver does not implement would invent an outcome. mode is what the sender
// asked for, which is what the record must state.
func (c *Collector) singleAnswer(composer acknowledgementComposer, controlID, mode, code, reason string, ordinal int) outcome {
	own := fmt.Sprintf("READMITCOLLECT%06d", ordinal)
	ack, composed := composer.compose(own, code, reason)
	if !composed {
		return unreadable(controlID, reasonNotComposable)
	}
	declinedReason := reasonNotRequested
	if mode == collection.EnhancedMode {
		declinedReason = reason
	}
	return outcome{
		controlID: controlID, mode: mode, usableHeader: true,
		accept:         declined(declinedReason),
		application:    collection.Stage{Code: code, ControlID: own, Destination: collection.SameConnection, Reason: reason},
		applicationACK: ack,
	}
}

// enhanced answers a sender that declared MSH-15 or MSH-16. Each stage carries
// its own code vocabulary, and the application stage goes where the policy
// declares, which is not necessarily the connection that delivered the message.
func (c *Collector) enhanced(composer acknowledgementComposer, controlID string, accepted bool, acceptCondition, applicationCondition string, ordinal int) outcome {
	result := outcome{controlID: controlID, mode: collection.EnhancedMode, usableHeader: true}
	// An unsupported mode combination is refused by name. Answering in a
	// protocol this receiver does not implement would invent an outcome, so
	// the refusal is an original-mode rejection of the message itself.
	refusal := ""
	switch {
	case !c.config.Policy.SupportsEnhanced():
		refusal = reasonEnhancedMode
	case acceptCondition != "" && !collection.ValidCondition(acceptCondition),
		applicationCondition != "" && !collection.ValidCondition(applicationCondition):
		refusal = reasonUnknownMode
	}
	if refusal != "" {
		return c.singleAnswer(composer, controlID, collection.EnhancedMode, collection.RejectCode, refusal, ordinal)
	}
	rule := *c.config.Policy.Enhanced
	acceptCode, applicationCode, reason := rule.AcceptCode, rule.ApplicationCode, ""
	if !accepted {
		acceptCode, applicationCode, reason = collection.CommitRejectCode, collection.RejectCode, reasonUnacceptedType
	}
	acceptReason, applicationReason := reason, reason
	if c.rejectStage(collection.AcceptStage, ordinal) {
		acceptCode, acceptReason = collection.CommitRejectCode, reasonInjectedReject
	}
	if c.rejectStage(collection.ApplicationStage, ordinal) {
		applicationCode, applicationReason = collection.RejectCode, reasonInjectedReject
	}
	result.accept, result.acceptACK = c.composeStage(composer, "READMITACC", acceptCode, acceptReason,
		collection.Requested(acceptCondition, acceptCode == collection.CommitAcceptCode), collection.SameConnection, ordinal)
	result.application, result.applicationACK = c.composeStage(composer, "READMITAPP", applicationCode, applicationReason,
		collection.Requested(applicationCondition, applicationCode == collection.AcceptCode), rule.ApplicationDelivery, ordinal)
	if result.acceptACK == nil && result.applicationACK == nil {
		return result
	}
	if result.accept.Code != collection.NotAcknowledged && result.acceptACK == nil || result.application.Code != collection.NotAcknowledged && result.applicationACK == nil {
		return unreadable(controlID, reasonNotComposable)
	}
	return result
}

// composeStage builds one stage's acknowledgement, or records why none was sent.
func (c *Collector) composeStage(composer acknowledgementComposer, prefix, code, reason string, requested bool, destination string, ordinal int) (collection.Stage, []byte) {
	if !requested {
		declinedReason := reasonNotRequested
		if reason != "" {
			declinedReason = reason
		}
		return declined(declinedReason), nil
	}
	own := fmt.Sprintf("%s%06d", prefix, ordinal)
	ack, composed := composer.compose(own, code, reason)
	if !composed {
		return declined(reasonNotComposable), nil
	}
	return collection.Stage{Code: code, ControlID: own, Destination: destination, Reason: reason}, ack
}

// acknowledgementComposer holds the sender's own header bytes for the frame
// being answered, so every stage is composed with the delimiters that sender
// declared and echoes exactly the bytes it sent.
type acknowledgementComposer struct {
	doc                       *hl7.Document
	control, version, trigger []byte
}

// compose builds an acknowledgement using the sender's own declared delimiters,
// so echoed header bytes keep their literal meaning. Echoed bytes are
// attacker-controlled, so the composed frame is read back before it is offered
// for sending; a frame that does not read back is never sent.
func (a acknowledgementComposer) compose(own, code, reason string) ([]byte, bool) {
	message := a.doc.Messages[0]
	separator := string(message.Delimiters.Field)
	declaredType := "ACK"
	if len(a.trigger) > 0 {
		declaredType += string(message.Delimiters.Component) + string(a.trigger)
	}
	text := "MSH" + separator + string(a.doc.Bytes(message.Segments[0].Field(2).Span)) +
		separator + "READMIT" + separator + "COLLECT" + separator + separator +
		separator + time.Now().UTC().Format("20060102150405-0700") + separator +
		separator + declaredType + separator + own +
		separator + "P" + separator + string(a.version) + "\r" +
		"MSA" + separator + code + separator + string(a.control)
	if reason != "" {
		text += separator + reason
	}
	ack := mllp.Frame([]byte(text + "\r"))
	check, err := hl7.Parse(ack, hl7.Options{Format: hl7.MLLP})
	if err != nil || len(check.Messages[0].Segments) != 2 {
		return nil, false
	}
	msa := check.Messages[0].Segments[1]
	header := check.Messages[0].Segments[0]
	if msa.ID != "MSA" || string(check.Bytes(msa.Field(1).Span)) != code || !bytes.Equal(check.Bytes(msa.Field(2).Span), a.control) {
		return nil, false
	}
	// The acknowledgement must also read back as the one this stage recorded,
	// so a stage delivered to a separate endpoint can be correlated to it.
	if string(check.Bytes(header.Field(10).Span)) != own {
		return nil, false
	}
	return ack, true
}
