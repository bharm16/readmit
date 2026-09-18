package bundle

import (
	"errors"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
)

func attachObservation(b *Bundle, snapshot observation.Snapshot) error {
	data, err := observation.Encode(snapshot)
	if err != nil {
		return err
	}
	if b.Manifest.Provenance.Mode != Recorded || b.Manifest.Provenance.SessionID != snapshot.SessionID {
		return errors.New("observation does not belong to recorded session")
	}
	// Processed occurrence references must be distinct, in evidence order, and
	// name the literal MSH-10 of inbound messages. A ledger is receiver testimony,
	// not independently recomputed workflow truth.
	next := 0
	for _, event := range b.Events {
		if next == len(snapshot.Processed) {
			break
		}
		processed := snapshot.Processed[next]
		if event.ID != processed.OccurrenceID {
			continue
		}
		if event.Kind == Unparsed || event.Direction != Inbound || event.Fields == nil || event.Fields.ControlID.State != hl7.Present || string(b.Value(event.ID, event.Fields.ControlID)) != processed.ControlID {
			return errors.New("observation processed occurrence disagrees with evidence")
		}
		next++
	}
	if next != len(snapshot.Processed) {
		return errors.New("observation references missing or out-of-order occurrences")
	}
	copy, err := observation.Decode(data)
	if err != nil {
		return err
	}
	b.Observation = &copy
	b.Manifest.Observation = &Payload{Path: "observation.json", Size: len(data), SHA256: digest(data)}
	return nil
}
