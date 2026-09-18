package receiver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
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
)

type CollectorConfig struct {
	Policy             collection.Policy
	OutputPath         string
	MaxFrameBytes      int
	IdleTimeout        time.Duration
	ApplicationTimeout time.Duration // bounds one separate application-ACK delivery
	MaxMessages        int           // zero means run until cancellation or a session limit
}

// Collector is a generic bounded MLLP receiver. It retains every byte it reads
// and writes, acknowledges receipt according to its declarative policy, and
// labels each retained source with the declared origin of that evidence. It
// applies no message: neither a commit acknowledgement nor an application
// acknowledgement reports that a downstream application processed anything.
// One connection is served at a time.
type Collector struct {
	recorder
	config    CollectorConfig
	startedAt time.Time
	record    collection.Record
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
	return c, nil
}

// Serve owns and closes listener. Cancellation interrupts a blocked accept,
// read, or acknowledgement write and then finalizes the retained bytes. An
// abrupt process termination cannot finalize in-memory wire evidence.
func (c *Collector) Serve(ctx context.Context, listener net.Listener) (*bundle.Bundle, error) {
	if c.served {
		return nil, errors.New("collector session has already been served")
	}
	c.served = true
	if c.config.Policy.Faults != nil {
		if err := c.config.Policy.Faults.ApproveEndpoint(listener.Addr().String()); err != nil {
			listener.Close()
			return nil, err
		}
	}
	err := c.sessions(ctx, listener, c.connection)
	b, writeErr := bundle.WriteCollected(c.config.OutputPath, c.inputs, c.startedAt, c.record)
	if writeErr != nil {
		return nil, errors.Join(err, writeErr)
	}
	return b, err
}

func (c *Collector) connection(ctx context.Context, connection net.Conn) (bool, error) {
	input := bundle.Input{Options: hl7.Options{Format: hl7.MLLP}, Observations: make(map[int]bundle.Observation)}
	c.chunks = nil
	ordinal := len(c.inputs) + 1
	session := fmt.Sprintf("c%04d", ordinal)
	reader, _ := mllp.NewReader(deadlineReader{connection, c.config.IdleTimeout}, c.config.MaxFrameBytes)
	defer func() {
		// A coalesced TCP read may include frames beyond --max-messages. They
		// are retained as received evidence, never claimed as acknowledged.
		if c.close(&input, reader.Buffered()) {
			c.declareSession(ordinal, session)
		}
	}()
	for {
		if err := c.withinLimits(&input, c.config.MaxFrameBytes); err != nil {
			return false, err
		}
		raw, readErr := reader.ReadFrame()
		if readErr != nil {
			raw = append(raw, reader.Buffered()...)
			if len(raw) > 0 {
				c.retain(&input, raw, bundle.Inbound)
			}
			// A malformed, oversized, idle, or disconnected peer leaves its
			// consumed prefix in the final case without any receipt claim.
			return false, nil
		}
		id := c.retain(&input, raw, bundle.Inbound)
		c.received++
		result := c.decide(raw)
		entry := collection.Received{SessionID: session, OccurrenceID: id, ControlID: result.controlID, Mode: result.mode, Accept: result.accept, Application: result.application}
		if step := c.config.Policy.Faults.Step(c.received); step != nil {
			entry.Fault = &collection.FaultEvent{Action: step.Action, Stage: step.Stage, Status: "not-reached"}
		}
		if err := c.commit(ordinal, session, entry); err != nil {
			return false, err
		}
		// The accept stage answers here; only the application stage may travel.
		for _, stage := range []string{collection.AcceptStage, collection.ApplicationStage} {
			stop := c.answerStage(ctx, &input, connection, result, stage)
			if stop {
				return c.complete(ctx, c.config.MaxMessages), nil
			}
		}
		if c.complete(ctx, c.config.MaxMessages) {
			return true, nil
		}
		if !result.usableHeader {
			// A header this receiver cannot answer safely ends the connection
			// rather than guessing a correlation for the next frame.
			return false, nil
		}
	}
}

// deliverApplication sends the application stage where the policy declares. A
// separate endpoint is a second transport: a failure or timeout there is an
// execution error recorded against this frame, never an application result, and
// it does not end the healthy connection that delivered the message.
func (c *Collector) deliverApplication(ctx context.Context, input *bundle.Input, connection net.Conn, result outcome) error {
	if result.application.Destination == collection.SameConnection {
		if err := c.sendHere(connection, input, result.applicationACK); err != nil {
			downgrade(&c.answering().Application, reasonPartialWrite)
			return err
		}
		return nil
	}
	if c.config.Policy.Faults != nil {
		if err := c.config.Policy.Faults.ApproveEndpoint(c.config.Policy.Enhanced.ApplicationEndpoint); err != nil {
			downgrade(&c.answering().Application, reasonUndelivered)
			return err
		}
	}
	dialer := net.Dialer{Timeout: c.config.ApplicationTimeout}
	endpoint, err := dialer.DialContext(ctx, "tcp", c.config.Policy.Enhanced.ApplicationEndpoint)
	if err != nil {
		// Nothing reached the peer, so nothing is retained and no stage is
		// claimed. An absent application acknowledgement is not a rejection.
		downgrade(&c.answering().Application, reasonUndelivered)
		return nil
	}
	defer endpoint.Close()
	// Cancellation interrupts a blocked delivery write as well as a blocked
	// accept or receive; bytes already sent are still retained below.
	stop := context.AfterFunc(ctx, func() { _ = endpoint.Close() })
	defer stop()
	if err := endpoint.SetWriteDeadline(time.Now().Add(c.config.ApplicationTimeout)); err != nil {
		downgrade(&c.answering().Application, reasonUndelivered)
		return nil
	}
	sent, writeErr := writeAll(endpoint, result.applicationACK)
	if sent > 0 {
		c.retain(input, result.applicationACK[:sent], bundle.Outbound)
	}
	if writeErr != nil {
		// Bytes already sent cannot be retracted, and a peer that never read
		// them was not acknowledged. Retain both facts explicitly.
		downgrade(&c.answering().Application, reasonPartialWrite)
	}
	return nil
}

func (c *Collector) sendHere(connection net.Conn, input *bundle.Input, ack []byte) error {
	if err := connection.SetWriteDeadline(time.Now().Add(c.config.IdleTimeout)); err != nil {
		return errors.New("cannot set collector write deadline")
	}
	sent, err := writeAll(connection, ack)
	if sent > 0 {
		c.retain(input, ack[:sent], bundle.Outbound)
	}
	return err
}

// answering is the receipt commit just preflighted: the frame every send in
// this iteration answers.
func (c *Collector) answering() *collection.Received {
	return &c.record.Received[len(c.record.Received)-1]
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

func (c *Collector) declareSession(ordinal int, session string) {
	if len(c.record.Sessions) >= ordinal {
		return
	}
	c.record.Sessions = append(c.record.Sessions, collection.Session{SessionID: session, SourceID: fmt.Sprintf("s%04d", ordinal), Label: c.config.Policy.SourceLabel})
}

// commit preflights the record before anything is acknowledged, leaving room
// for the longest reason each stage can substitute. A finalized session can
// then always encode testimony for every acknowledgement it actually sent.
func (c *Collector) commit(ordinal int, session string, entry collection.Received) error {
	c.declareSession(ordinal, session)
	if len(c.record.Received) >= collection.MaxReceived {
		return errors.New("collector session reached collection record limit")
	}
	candidate := c.record
	received := make([]collection.Received, 0, len(c.record.Received)+1)
	candidate.Received = append(append(received, c.record.Received...), entry)
	data, err := collection.Encode(candidate)
	if err != nil || len(data)+2*collection.MaxReasonBytes+16 > collection.MaxBytes {
		return errors.New("collector session reached collection record size limit")
	}
	c.record = candidate
	return nil
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
func (c *Collector) decide(raw []byte) outcome {
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
		return c.original(composer, controlID, accepted)
	}
	return c.enhanced(composer, controlID, accepted, acceptCondition, applicationCondition)
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
func (c *Collector) original(composer acknowledgementComposer, controlID string, accepted bool) outcome {
	code, reason := c.config.Policy.Acknowledgement.Code, ""
	if c.rejectStage(collection.ApplicationStage) {
		code, reason = collection.RejectCode, reasonInjectedReject
	}
	if !accepted {
		code, reason = collection.RejectCode, reasonUnacceptedType
	}
	return c.singleAnswer(composer, controlID, collection.OriginalMode, code, reason)
}

// singleAnswer composes one original-mode acknowledgement on the receiving
// connection. That is the whole of original mode, and it is also the refusal an
// unsupported enhanced request gets, because answering inside a protocol this
// receiver does not implement would invent an outcome. mode is what the sender
// asked for, which is what the record must state.
func (c *Collector) singleAnswer(composer acknowledgementComposer, controlID, mode, code, reason string) outcome {
	own := fmt.Sprintf("READMITCOLLECT%06d", c.received)
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
func (c *Collector) enhanced(composer acknowledgementComposer, controlID string, accepted bool, acceptCondition, applicationCondition string) outcome {
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
		return c.singleAnswer(composer, controlID, collection.EnhancedMode, collection.RejectCode, refusal)
	}
	rule := *c.config.Policy.Enhanced
	acceptCode, applicationCode, reason := rule.AcceptCode, rule.ApplicationCode, ""
	if !accepted {
		acceptCode, applicationCode, reason = collection.CommitRejectCode, collection.RejectCode, reasonUnacceptedType
	}
	acceptReason, applicationReason := reason, reason
	if c.rejectStage(collection.AcceptStage) {
		acceptCode, acceptReason = collection.CommitRejectCode, reasonInjectedReject
	}
	if c.rejectStage(collection.ApplicationStage) {
		applicationCode, applicationReason = collection.RejectCode, reasonInjectedReject
	}
	result.accept, result.acceptACK = c.composeStage(composer, "READMITACC", acceptCode, acceptReason,
		collection.Requested(acceptCondition, acceptCode == collection.CommitAcceptCode), collection.SameConnection)
	result.application, result.applicationACK = c.composeStage(composer, "READMITAPP", applicationCode, applicationReason,
		collection.Requested(applicationCondition, applicationCode == collection.AcceptCode), rule.ApplicationDelivery)
	if result.acceptACK == nil && result.applicationACK == nil {
		return result
	}
	if result.accept.Code != collection.NotAcknowledged && result.acceptACK == nil || result.application.Code != collection.NotAcknowledged && result.applicationACK == nil {
		return unreadable(controlID, reasonNotComposable)
	}
	return result
}

// composeStage builds one stage's acknowledgement, or records why none was sent.
func (c *Collector) composeStage(composer acknowledgementComposer, prefix, code, reason string, requested bool, destination string) (collection.Stage, []byte) {
	if !requested {
		declinedReason := reasonNotRequested
		if reason != "" {
			declinedReason = reason
		}
		return declined(declinedReason), nil
	}
	own := fmt.Sprintf("%s%06d", prefix, c.received)
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
