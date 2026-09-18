package bundle

import (
	"errors"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
)

// attachCollection binds a generic receiver's collection record to the bytes it
// actually retained. Sessions must name real sources and received frames must
// reference inbound occurrences in evidence order with their literal MSH-10.
// The record remains receiver testimony about receipt; nothing here recomputes
// or certifies what a downstream application did with a message.
func attachCollection(b *Bundle, record collection.Record) error {
	data, err := collection.Encode(record)
	if err != nil {
		return err
	}
	if b.Manifest.Provenance.Mode != Collected || b.Manifest.Provenance.SessionID != record.SessionID {
		return errors.New("collection record does not belong to collected session")
	}
	if len(record.Sessions) != len(b.Manifest.Sources) {
		return errors.New("collection record does not label every retained source")
	}
	for i, session := range record.Sessions {
		if session.SourceID != b.Manifest.Sources[i].ID {
			return errors.New("collected session does not name its retained source")
		}
	}
	next := 0
	for _, event := range b.Events {
		if next == len(record.Received) {
			break
		}
		received := record.Received[next]
		if event.ID != received.OccurrenceID {
			continue
		}
		if event.Direction != Inbound {
			return errors.New("collection record claims an outbound occurrence was received")
		}
		if err := agreesWithEvidence(b, event, received); err != nil {
			return err
		}
		next++
	}
	if next != len(record.Received) {
		return errors.New("collection record references missing or out-of-order occurrences")
	}
	sealed, err := collection.Decode(data)
	if err != nil {
		return err
	}
	b.Collection = &sealed
	b.Manifest.Collection = &Payload{Path: "collection.json", Size: len(data), SHA256: digest(data)}
	return nil
}

// A named control ID must be the occurrence's literal MSH-10. An empty control
// ID is retained evidence that the header could not supply one, so the stored
// occurrence must genuinely lack a usable control ID.
func agreesWithEvidence(b *Bundle, event Event, received collection.Received) error {
	usable := event.Kind != Unparsed && event.Fields != nil && event.Fields.ControlID.State == hl7.Present
	if received.ControlID == "" {
		if usable {
			return errors.New("collection record dropped the control ID of a readable occurrence")
		}
		return nil
	}
	if !usable || string(b.Value(event.ID, event.Fields.ControlID)) != received.ControlID {
		return errors.New("collection record control ID disagrees with evidence")
	}
	return nil
}
