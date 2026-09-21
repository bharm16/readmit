package durablerun

import (
	"errors"
	"fmt"

	"github.com/bharm16/readmit/internal/durablelog"
	"github.com/bharm16/readmit/internal/replay"
)

// The journal record protocol. A durable run appends six kinds of record, and
// recovery re-derives the run from them. This file is the one spelling of that
// protocol: the record shapes, the outbound identity grammar they use, the
// writer's appends, and the reader's validation. The safety-critical rule —
// an intent with no acknowledged outcome stays uncertain, and a journal that
// ends before the resolution leaves it uncertain — is written once, here, so
// the two sides cannot disagree about what a journal says.

const (
	recordReady    = "ready"
	recordRunning  = "running"
	recordIntent   = "intent"
	recordSent     = "sent"
	recordRecorded = "recorded"
	recordFinished = "finished"
)

// entry is one journal record. Exactly one of the optional members is present,
// selected by Kind.
type entry struct {
	durablelog.Envelope
	Kind       string        `json:"kind"`
	Occurrence string        `json:"occurrence,omitzero"`
	Sent       *payload      `json:"sent,omitzero"`
	Event      *replay.Event `json:"event,omitzero"`
	Final      *Summary      `json:"final,omitzero"`
}

// errRecord is what the reader's validation returns for a journal whose
// records do not follow the protocol. readJob maps it to its own refusal.
var errRecord = errors.New("durable journal record does not follow the protocol")

// occurrenceID is the outbound identity grammar: sequence-based, never random,
// so a journal and the plan beside it name the same occurrence the same way.
func occurrenceID(sequence int) string { return fmt.Sprintf("o%06d", sequence) }

func intendedPath(id string) string { return "intended/" + id + ".bin" }
func sentPath(id string) string     { return "sent/" + id + ".bin" }

// evidencePath is where a recorded event retains one of its four payloads.
func evidencePath(id, name string) string { return "payloads/" + id + "-" + name + ".bin" }

// appendReady records that the plan, engine pin and lease are on storage.
func (w *writer) appendReady() error {
	return w.log.Append(&entry{Kind: recordReady})
}

// appendRunning records that execution began. Nothing follows the first two
// records in any other order, on either side of the protocol.
func (w *writer) appendRunning() error {
	return w.log.Append(&entry{Kind: recordRunning})
}

// appendIntent is synced before the network write it precedes. Setting the
// summary uncertain before the append, and un-setting it only when the log
// refused the record before writing any byte, is the rule that makes an
// interrupted journal report an uncertain delivery rather than a missing one.
func (w *writer) appendIntent(id string) error {
	w.summary.DeliveryUncertain = true
	err := w.log.Append(&entry{Kind: recordIntent, Occurrence: id})
	if errors.Is(err, errJournalLimit) {
		w.summary.DeliveryUncertain = false
	}
	return err
}

// appendSent records the exact bytes a send put on the wire, after they are
// retained and their directory is synced.
func (w *writer) appendSent(id string, raw []byte) error {
	p := payload{sentPath(id), len(raw), digest(raw)}
	return w.log.Append(&entry{Kind: recordSent, Occurrence: id, Sent: &p})
}

// appendRecorded records one outcome. Only a matched acknowledgement resolves
// a synced intent; Recorded is advanced once the record is in the journal.
func (w *writer) appendRecorded(id string, event replay.Event) error {
	if err := w.log.Append(&entry{Kind: recordRecorded, Occurrence: id, Event: &event}); err != nil {
		return err
	}
	w.summary.Recorded++
	if event.Delivery == "acknowledged" {
		w.summary.DeliveryUncertain = false
	}
	return nil
}

// appendFinished records the summary a run stopped with. Recovery refuses a
// finished record unless it agrees with the certainty the preceding records
// re-derived, so the two sides cannot disagree about what happened.
func (w *writer) appendFinished() error {
	return w.log.Append(&entry{Kind: recordFinished, Final: &w.summary})
}

// protocol re-derives a run from its records. It owns every rule about the
// sequence itself: which record may follow which, which occurrence a record
// names, when a delivery is uncertain and when an outcome resolves it, and
// what a finished record must agree with. Evidence verification — that named
// bytes exist and match their digests — stays with the reader that has the
// files open; this type says what the records mean.
type protocol struct {
	summary     Summary
	occurrences []Occurrence
	// intended and sources are the per-occurrence digests the plan recorded,
	// against which a recorded event's own identities are checked.
	intended []payload
	sources  []string

	sequence     int
	pending      string
	sentRecorded bool
	finished     bool

	recordedEvents []replay.Event
}

func newProtocol(planned int, intended []payload, sources []string) *protocol {
	p := &protocol{
		summary:     Summary{Schema: Schema, State: Interrupted, StopReason: Interrupted, Planned: planned, Recovered: true},
		occurrences: make([]Occurrence, planned),
		intended:    intended,
		sources:     sources,
	}
	for i := range p.occurrences {
		p.occurrences[i] = Occurrence{ID: occurrenceID(i + 1), Delivery: NotAttempted}
	}
	return p
}

// apply validates one record and advances the re-derivation.
func (p *protocol) apply(e *entry) error {
	if p.finished {
		return errRecord
	}
	p.sequence++
	switch e.Kind {
	case recordReady:
		if p.sequence != 1 {
			return errRecord
		}
	case recordRunning:
		if p.sequence != 2 {
			return errRecord
		}
	case recordIntent:
		if p.sequence < 3 || p.pending != "" || e.Occurrence != occurrenceID(p.summary.Recorded+1) {
			return errRecord
		}
		p.pending = e.Occurrence
		p.sentRecorded = false
		p.summary.DeliveryUncertain = true
		p.occurrences[p.summary.Recorded].Delivery = Uncertain
	case recordSent:
		if p.sentRecorded || p.pending == "" || e.Occurrence != p.pending || e.Sent == nil || e.Sent.Path != sentPath(p.pending) {
			return errRecord
		}
		p.sentRecorded = true
	case recordRecorded:
		if _, err := p.recorded(e); err != nil {
			return err
		}
	case recordFinished:
		return errRecord
	default:
		return errRecord
	}
	return nil
}

// recorded validates one recorded outcome and reports the four evidence
// payloads it must retain, named as the protocol names them. A matched
// acknowledgement is the only outcome that resolves a synced intent, and only
// for the occurrence that intent named.
func (p *protocol) recorded(e *entry) ([4]payload, error) {
	var refs [4]payload
	if p.finished {
		return refs, errRecord
	}
	if p.summary.Recorded >= p.summary.Planned || e.Event == nil || e.Occurrence != occurrenceID(p.summary.Recorded+1) || e.Event.OutboundOccurrence != e.Occurrence {
		return refs, errRecord
	}
	if e.Event.Intended.SHA256 != p.intended[p.summary.Recorded].SHA256 || e.Event.Intended.Size != p.intended[p.summary.Recorded].Size || e.Event.Source.SHA256 != p.sources[p.summary.Recorded] {
		return refs, errRecord
	}
	declared := [4]payload{
		{e.Event.Source.Path, e.Event.Source.Size, e.Event.Source.SHA256},
		{e.Event.Intended.Path, e.Event.Intended.Size, e.Event.Intended.SHA256},
		{e.Event.Sent.Path, e.Event.Sent.Size, e.Event.Sent.SHA256},
		{e.Event.Received.Path, e.Event.Received.Size, e.Event.Received.SHA256},
	}
	for i, name := range []string{"source", "intended", "sent", "received"} {
		if declared[i].Path != evidencePath(e.Occurrence, name) {
			return refs, errRecord
		}
		refs[i] = declared[i]
	}
	p.recordedEvents = append(p.recordedEvents, *e.Event)
	// A recorded event with no intent before it was never attempted: the
	// transport halted before this occurrence, or failed to dial.
	if e.Event.Delivery == "acknowledged" {
		if p.pending != e.Occurrence {
			return refs, errRecord
		}
		p.occurrences[p.summary.Recorded].Delivery = Acknowledged
		p.summary.DeliveryUncertain = false
		p.pending = ""
	}
	p.summary.Recorded++
	return refs, nil
}

// finish validates a finished record against the certainty the preceding
// records re-derived. The caller verifies the result it names and then
// commits it; until then the run remains unfinished.
func (p *protocol) finish(e *entry) (Summary, error) {
	final := Summary{}
	if p.finished || e.Final == nil {
		return final, errRecord
	}
	if !e.Final.StopReason.Terminal() || e.Final.Planned != p.summary.Planned || e.Final.Recorded != p.summary.Recorded || e.Final.Schema != Schema || e.Final.DeliveryUncertain != p.summary.DeliveryUncertain {
		return final, errRecord
	}
	if e.Final.Recovered || e.Final.JournalIncomplete {
		return final, errRecord
	}
	expected := e.Final.StopReason
	if p.summary.DeliveryUncertain {
		expected = DeliveryUncertain
	}
	if e.Final.State != expected {
		return final, errRecord
	}
	return *e.Final, nil
}

// commit accepts a verified finished record.
func (p *protocol) commit(final Summary) {
	p.summary = final
	p.finished = true
}
