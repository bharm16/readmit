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
	reasonUnacceptedType = "message type is not accepted by the configured receiver policy"
	reasonNotComposable  = "acknowledgement could not be composed for this header"
	reasonPartialWrite   = "acknowledgement was not written completely"
)

type CollectorConfig struct {
	Policy        collection.Policy
	OutputPath    string
	MaxFrameBytes int
	IdleTimeout   time.Duration
	MaxMessages   int // zero means run until cancellation or a session limit
}

// Collector is a generic bounded MLLP receiver. It retains every byte it reads
// and writes, acknowledges receipt according to its declarative policy, and
// labels each retained source with the declared origin of that evidence. It
// applies no message: an accept code reports that bytes arrived, never that a
// downstream application processed them. One connection is served at a time.
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
	if err := validBounds(config.MaxFrameBytes, config.IdleTimeout, config.MaxMessages); err != nil {
		return nil, err
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
		entry := collection.Received{SessionID: session, OccurrenceID: id, ControlID: result.controlID, Acknowledgement: collection.NotAcknowledged, Reason: result.reason}
		if result.ack != nil {
			entry.Acknowledgement = result.code
		}
		if err := c.commit(ordinal, session, entry); err != nil {
			return false, err
		}
		if result.ack != nil {
			if err := connection.SetWriteDeadline(time.Now().Add(c.config.IdleTimeout)); err != nil {
				return false, errors.New("cannot set collector write deadline")
			}
			sent, err := writeAll(connection, result.ack)
			if sent > 0 {
				c.retain(&input, result.ack[:sent], bundle.Outbound)
			}
			if err != nil {
				// Bytes already sent cannot be retracted, and a peer that never
				// read them was not acknowledged. Retain both facts explicitly.
				last := &c.record.Received[len(c.record.Received)-1]
				last.Acknowledgement, last.Reason = collection.NotAcknowledged, reasonPartialWrite
				return c.complete(ctx, c.config.MaxMessages), nil
			}
		}
		if c.complete(ctx, c.config.MaxMessages) {
			return true, nil
		}
		if result.ack == nil {
			// A header this receiver cannot answer safely ends the connection
			// rather than guessing a correlation for the next frame.
			return false, nil
		}
	}
}

func (c *Collector) declareSession(ordinal int, session string) {
	if len(c.record.Sessions) >= ordinal {
		return
	}
	c.record.Sessions = append(c.record.Sessions, collection.Session{SessionID: session, SourceID: fmt.Sprintf("s%04d", ordinal), Label: c.config.Policy.SourceLabel})
}

// commit preflights the record before anything is acknowledged, leaving room
// for the longest reason a failed write can substitute. A finalized session can
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
	if err != nil || len(data)+collection.MaxReasonBytes+8 > collection.MaxBytes {
		return errors.New("collector session reached collection record size limit")
	}
	c.record = candidate
	return nil
}

type outcome struct {
	controlID, code, reason string
	ack                     []byte
}

// decide applies the declarative policy to one complete frame. It reads only
// the header values an acknowledgement needs; it never interprets the message.
func (c *Collector) decide(raw []byte) outcome {
	doc, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil {
		return outcome{reason: reasonUnparseable}
	}
	message := doc.Messages[0]
	header := message.Segments[0]
	control := doc.Bytes(header.Field(10).Span)
	if header.Field(10).State != hl7.Present || len(control) > collectorMaxEchoBytes || !utf8.Valid(control) {
		return outcome{reason: reasonNoControlID}
	}
	result := outcome{controlID: string(control), code: c.config.Policy.Acknowledgement.Code}
	version := doc.Bytes(header.Field(12).Span)
	if header.Field(12).State != hl7.Present || len(version) > collectorMaxEchoBytes {
		return outcome{controlID: result.controlID, reason: reasonNoVersion}
	}
	declared := doc.Bytes(header.Field(9).Span)
	if len(declared) > collectorMaxEchoBytes {
		return outcome{controlID: result.controlID, reason: reasonWideType}
	}
	parts := bytes.SplitN(declared, []byte{message.Delimiters.Component}, 3)
	var trigger []byte
	if len(parts) > 1 {
		trigger = parts[1]
	}
	switch {
	case populated(header.Field(15)) || populated(header.Field(16)):
		result.code, result.reason = collection.RejectCode, reasonEnhancedMode
	case !c.config.Policy.Accepts(string(parts[0]), string(trigger)):
		result.code, result.reason = collection.RejectCode, reasonUnacceptedType
	}
	ack, composed := c.acknowledgement(doc, result.code, result.reason, control, version, trigger)
	if !composed {
		return outcome{controlID: result.controlID, reason: reasonNotComposable}
	}
	result.ack = ack
	return result
}

// acknowledgement composes an original-mode ACK using the sender's own declared
// delimiters, so echoed header bytes keep their literal meaning. Echoed bytes
// are attacker-controlled, so the composed frame is read back before it is
// offered for sending; a frame that does not read back is never sent.
func (c *Collector) acknowledgement(doc *hl7.Document, code, reason string, control, version, trigger []byte) ([]byte, bool) {
	message := doc.Messages[0]
	separator := string(message.Delimiters.Field)
	declared := "ACK"
	if len(trigger) > 0 {
		declared += string(message.Delimiters.Component) + string(trigger)
	}
	text := "MSH" + separator + string(doc.Bytes(message.Segments[0].Field(2).Span)) +
		separator + "READMIT" + separator + "COLLECT" + separator + separator +
		separator + time.Now().UTC().Format("20060102150405-0700") + separator +
		separator + declared + separator + fmt.Sprintf("READMITCOLLECT%06d", c.received) +
		separator + "P" + separator + string(version) + "\r" +
		"MSA" + separator + code + separator + string(control)
	if reason != "" {
		text += separator + reason
	}
	ack := mllp.Frame([]byte(text + "\r"))
	check, err := hl7.Parse(ack, hl7.Options{Format: hl7.MLLP})
	if err != nil || len(check.Messages[0].Segments) != 2 {
		return nil, false
	}
	msa := check.Messages[0].Segments[1]
	if msa.ID != "MSA" || string(check.Bytes(msa.Field(1).Span)) != code || !bytes.Equal(check.Bytes(msa.Field(2).Span), control) {
		return nil, false
	}
	return ack, true
}
