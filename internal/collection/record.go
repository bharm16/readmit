package collection

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	Schema      = "readmit-collection/v1"
	MaxBytes    = 8 << 20
	MaxSessions = 128
	MaxReceived = 5000
	// NoApplicationProcessing is the only supported value of a record's
	// application_processing member. A generic receiver acknowledges receipt of
	// bytes; it never applies a message, so an accept code is not evidence that
	// a downstream application processed anything.
	NoApplicationProcessing = "none"
	// NotAcknowledged marks a retained frame that was never acknowledged. An
	// absent acknowledgement is not a negative application result.
	NotAcknowledged   = "none"
	MaxControlIDBytes = 1024
	MaxReasonBytes    = 256
)

// Session is one transport connection, labelled with the declared origin of the
// evidence it carried. The label is configuration the operator supplied, never
// a verified property of the peer.
type Session struct {
	SessionID string `json:"session_id"`
	SourceID  string `json:"source_id"`
	Label     string `json:"label"`
}

// Received is one complete inbound frame and what the receiver sent back. The
// control ID is the literal MSH-10 bytes; it is empty only when the header
// could not supply one.
type Received struct {
	SessionID       string `json:"session_id"`
	OccurrenceID    string `json:"occurrence_id"`
	ControlID       string `json:"control_id"`
	Acknowledgement string `json:"acknowledgement"`
	Reason          string `json:"reason"`
}

type Record struct {
	Schema                string     `json:"schema"`
	SessionID             string     `json:"session_id"`
	Policy                Policy     `json:"policy"`
	ApplicationProcessing string     `json:"application_processing"`
	Sessions              []Session  `json:"sessions"`
	Received              []Received `json:"received"`
}

var (
	sessionPattern    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	occurrencePattern = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)
)

func (r *Record) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema                *string     `json:"schema"`
		SessionID             *string     `json:"session_id"`
		Policy                *Policy     `json:"policy"`
		ApplicationProcessing *string     `json:"application_processing"`
		Sessions              *[]Session  `json:"sessions"`
		Received              *[]Received `json:"received"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil || required.SessionID == nil || required.Policy == nil || required.ApplicationProcessing == nil || required.Sessions == nil || required.Received == nil {
		return errors.New("collection record requires schema, session, policy, application processing, sessions, and received frames")
	}
	type plainRecord Record
	var value plainRecord
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid collection record JSON")
	}
	*r = Record(value)
	return nil
}

func (s *Session) UnmarshalJSON(data []byte) error {
	var required struct {
		SessionID *string `json:"session_id"`
		SourceID  *string `json:"source_id"`
		Label     *string `json:"label"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.SessionID == nil || required.SourceID == nil || required.Label == nil {
		return errors.New("collected session requires a session, a source, and an explicit label")
	}
	*s = Session{SessionID: *required.SessionID, SourceID: *required.SourceID, Label: *required.Label}
	return nil
}

func (r *Received) UnmarshalJSON(data []byte) error {
	var required struct {
		SessionID       *string `json:"session_id"`
		OccurrenceID    *string `json:"occurrence_id"`
		ControlID       *string `json:"control_id"`
		Acknowledgement *string `json:"acknowledgement"`
		Reason          *string `json:"reason"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.SessionID == nil || required.OccurrenceID == nil || required.ControlID == nil || required.Acknowledgement == nil || required.Reason == nil {
		return errors.New("received frame requires session, occurrence, control ID, acknowledgement, and reason members")
	}
	*r = Received{SessionID: *required.SessionID, OccurrenceID: *required.OccurrenceID, ControlID: *required.ControlID, Acknowledgement: *required.Acknowledgement, Reason: *required.Reason}
	return nil
}

func (r Record) Validate() error {
	if r.Schema != Schema || !sessionPattern.MatchString(r.SessionID) || r.ApplicationProcessing != NoApplicationProcessing {
		return errors.New("invalid collection schema, session, or application processing statement")
	}
	if err := r.Policy.Validate(); err != nil {
		return err
	}
	if len(r.Sessions) > MaxSessions || len(r.Received) > MaxReceived {
		return errors.New("collection record exceeds session or occurrence limit")
	}
	labelled := make(map[string]string, len(r.Sessions))
	for i, session := range r.Sessions {
		if session.SessionID != fmt.Sprintf("c%04d", i+1) || session.SourceID != fmt.Sprintf("s%04d", i+1) || !labelPattern.MatchString(session.Label) {
			return errors.New("collected sessions must be sequential, source-bound, and explicitly labelled")
		}
		labelled[session.SessionID] = session.SourceID
	}
	seen := make(map[string]bool, len(r.Received))
	for _, received := range r.Received {
		source, collected := labelled[received.SessionID]
		// A frame belongs to the connection that carried it: its occurrence
		// must live in that session's own source, never in another session's.
		if !collected || !occurrencePattern.MatchString(received.OccurrenceID) || seen[received.OccurrenceID] || !strings.HasPrefix(received.OccurrenceID, source+"-") {
			return errors.New("received frames must name a collected session and a distinct occurrence within its source")
		}
		seen[received.OccurrenceID] = true
		if err := received.validateOutcome(); err != nil {
			return err
		}
	}
	return nil
}

func (r Received) validateOutcome() error {
	switch r.Acknowledgement {
	case AcceptCode, ApplicationErrorCode, RejectCode:
		if r.ControlID == "" {
			return errors.New("an acknowledged frame must name the control ID it echoed")
		}
	case NotAcknowledged:
	default:
		return errors.New("acknowledgement must be AA, AE, AR, or none")
	}
	if len(r.ControlID) > MaxControlIDBytes || !utf8.ValidString(r.ControlID) {
		return errors.New("received control ID exceeds the supported text bound")
	}
	if r.Acknowledgement == AcceptCode && r.Reason != "" {
		return errors.New("an accepted frame carries no rejection reason")
	}
	if len(r.Reason) > MaxReasonBytes {
		return errors.New("received reason exceeds the supported text bound")
	}
	for _, b := range []byte(r.Reason) {
		if b < 0x20 || b > 0x7e {
			return errors.New("received reason must be printable ASCII")
		}
	}
	return nil
}

func Encode(record Record) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(record, json.Deterministic(true))
	if err != nil || len(data) >= MaxBytes {
		return nil, errors.New("cannot encode bounded collection record")
	}
	return append(data, '\n'), nil
}

func Decode(data []byte) (Record, error) {
	var record Record
	if len(data) > MaxBytes {
		return record, errors.New("collection record exceeds size limit")
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, errors.New("invalid collection record JSON")
	}
	return record, record.Validate()
}
