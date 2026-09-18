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
	SchemaV1    = "readmit-collection/v1"
	Schema      = "readmit-collection/v2"
	MaxBytes    = 8 << 20
	MaxSessions = 128
	MaxReceived = 5000
	// NoApplicationProcessing is the only supported value of a record's
	// application_processing member. A generic receiver acknowledges receipt of
	// bytes; it never applies a message, so neither a commit acknowledgement
	// nor an application acknowledgement is evidence that a downstream
	// application processed anything.
	NoApplicationProcessing = "none"
	// NotAcknowledged marks a stage that sent no acknowledgement. An absent or
	// undelivered acknowledgement is not a negative application result.
	NotAcknowledged = "none"
	// UnknownMode marks a frame whose header could not declare an
	// acknowledgement mode. Unknown is not original, and it is not a pass.
	UnknownMode       = "unknown"
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

// Stage is one acknowledgement stage's outcome for a received frame. The accept
// stage answers MSH-15 with a commit code and the application stage answers
// MSH-16 with an application code; the two vocabularies never mix, so a commit
// acceptance can never be read back as an application result. ControlID is the
// MSH-10 the receiver put on the acknowledgement it sent, which is what lets an
// acknowledgement delivered to a separate endpoint be correlated back here.
type Stage struct {
	Code        string `json:"code"`
	ControlID   string `json:"control_id"`
	Destination string `json:"destination"`
	Reason      string `json:"reason"`
}

// Received is one complete inbound frame, the acknowledgement mode its sender
// declared, and what each stage answered. ControlID is the literal MSH-10 bytes
// of the inbound frame; it is empty only when the header could not supply one.
type Received struct {
	SessionID    string `json:"session_id"`
	OccurrenceID string `json:"occurrence_id"`
	ControlID    string `json:"control_id"`
	Mode         string `json:"mode"`
	Accept       Stage  `json:"accept"`
	Application  Stage  `json:"application"`
}

type Record struct {
	Schema                string     `json:"schema"`
	SessionID             string     `json:"session_id"`
	Policy                Policy     `json:"policy"`
	ApplicationProcessing string     `json:"application_processing"`
	Sessions              []Session  `json:"sessions"`
	Received              []Received `json:"received"`
}

// receivedV1 is the frozen readmit-collection/v1 member set for one frame: an
// original-mode application acknowledgement on the receiving connection, and
// nothing else. No member is ever added to it.
type receivedV1 struct {
	SessionID       string `json:"session_id"`
	OccurrenceID    string `json:"occurrence_id"`
	ControlID       string `json:"control_id"`
	Acknowledgement string `json:"acknowledgement"`
	Reason          string `json:"reason"`
}

type recordV1 struct {
	Schema                string       `json:"schema"`
	SessionID             string       `json:"session_id"`
	Policy                Policy       `json:"policy"`
	ApplicationProcessing string       `json:"application_processing"`
	Sessions              []Session    `json:"sessions"`
	Received              []receivedV1 `json:"received"`
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

func (s *Stage) UnmarshalJSON(data []byte) error {
	var required struct {
		Code        *string `json:"code"`
		ControlID   *string `json:"control_id"`
		Destination *string `json:"destination"`
		Reason      *string `json:"reason"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Code == nil || required.ControlID == nil || required.Destination == nil || required.Reason == nil {
		return errors.New("an acknowledgement stage requires a code, a control ID, a destination, and a reason")
	}
	type plainStage Stage
	var value plainStage
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid acknowledgement stage")
	}
	*s = Stage(value)
	return nil
}

func (r *Received) UnmarshalJSON(data []byte) error {
	var required struct {
		SessionID    *string `json:"session_id"`
		OccurrenceID *string `json:"occurrence_id"`
		ControlID    *string `json:"control_id"`
		Mode         *string `json:"mode"`
		Accept       *Stage  `json:"accept"`
		Application  *Stage  `json:"application"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.SessionID == nil || required.OccurrenceID == nil || required.ControlID == nil || required.Mode == nil || required.Accept == nil || required.Application == nil {
		return errors.New("received frame requires session, occurrence, control ID, mode, and both acknowledgement stages")
	}
	type plainReceived Received
	var value plainReceived
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid received frame")
	}
	*r = Received(value)
	return nil
}

func (r *receivedV1) UnmarshalJSON(data []byte) error {
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
	// A v1 frame keeps its frozen member set: a v2 member here is an error,
	// not a member silently carried into a v1 file.
	type plainReceived receivedV1
	var value plainReceived
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid received frame")
	}
	*r = receivedV1(value)
	return nil
}

func (r *recordV1) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema                *string       `json:"schema"`
		SessionID             *string       `json:"session_id"`
		Policy                *Policy       `json:"policy"`
		ApplicationProcessing *string       `json:"application_processing"`
		Sessions              *[]Session    `json:"sessions"`
		Received              *[]receivedV1 `json:"received"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil || required.SessionID == nil || required.Policy == nil || required.ApplicationProcessing == nil || required.Sessions == nil || required.Received == nil {
		return errors.New("collection record requires schema, session, policy, application processing, sessions, and received frames")
	}
	type plainRecord recordV1
	var value plainRecord
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid collection record JSON")
	}
	*r = recordV1(value)
	return nil
}

// asV1 projects a record onto the frozen v1 member set. Callers pass a record
// that already validated as v1, which is what guarantees the projection is
// lossless: validateV1 refuses any stage, mode, or destination v1 never had, so
// a v1 file can never be written from a session that used one.
func (r Record) asV1() recordV1 {
	out := recordV1{Schema: r.Schema, SessionID: r.SessionID, Policy: r.Policy, ApplicationProcessing: r.ApplicationProcessing, Sessions: r.Sessions, Received: make([]receivedV1, 0, len(r.Received))}
	for _, received := range r.Received {
		out.Received = append(out.Received, receivedV1{SessionID: received.SessionID, OccurrenceID: received.OccurrenceID, ControlID: received.ControlID, Acknowledgement: received.Application.Code, Reason: received.Application.Reason})
	}
	return out
}

// fromV1 reads a v1 file as what v1 meant: original acknowledgement mode, no
// accept stage, and one application acknowledgement on the receiving
// connection. Nothing is invented; v1 recorded no acknowledgement control ID,
// so none appears.
func fromV1(in recordV1) Record {
	out := Record{Schema: in.Schema, SessionID: in.SessionID, Policy: in.Policy, ApplicationProcessing: in.ApplicationProcessing, Sessions: in.Sessions, Received: make([]Received, 0, len(in.Received))}
	for _, received := range in.Received {
		destination := SameConnection
		if received.Acknowledgement == NotAcknowledged {
			destination = NoDestination
		}
		out.Received = append(out.Received, Received{
			SessionID: received.SessionID, OccurrenceID: received.OccurrenceID, ControlID: received.ControlID,
			Mode:        OriginalMode,
			Accept:      Stage{Code: NotAcknowledged, Destination: NoDestination},
			Application: Stage{Code: received.Acknowledgement, Destination: destination, Reason: received.Reason},
		})
	}
	return out
}

func (r Record) Validate() error {
	switch r.Schema {
	case SchemaV1:
		return r.validateV1()
	case Schema:
		return r.validateV2()
	}
	return errors.New("unsupported collection record schema version")
}

func (r Record) common() error {
	if !sessionPattern.MatchString(r.SessionID) || r.ApplicationProcessing != NoApplicationProcessing {
		return errors.New("invalid collection session or application processing statement")
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
		if len(received.ControlID) > MaxControlIDBytes || !utf8.ValidString(received.ControlID) {
			return errors.New("received control ID exceeds the supported text bound")
		}
		answered := received.Accept.Code != NotAcknowledged || received.Application.Code != NotAcknowledged
		if answered && received.ControlID == "" {
			return errors.New("an acknowledged frame must name the control ID it echoed")
		}
	}
	return nil
}

// validateV1 additionally holds the record to what v1 could express: original
// mode, no accept stage, and no recorded acknowledgement control ID.
func (r Record) validateV1() error {
	if r.Schema != SchemaV1 || r.Policy.Schema != PolicySchemaV1 {
		return errors.New("a readmit-collection/v1 record carries a readmit-receiver-policy/v1 policy")
	}
	if err := r.common(); err != nil {
		return err
	}
	for _, received := range r.Received {
		if received.Mode != OriginalMode || received.Accept != (Stage{Code: NotAcknowledged, Destination: NoDestination}) {
			return errors.New("a readmit-collection/v1 record has no accept stage and no enhanced mode")
		}
		if received.Application.ControlID != "" || received.Application.Destination != destinationFor(received.Application.Code) {
			return errors.New("a readmit-collection/v1 record answers only on the receiving connection")
		}
		// v1 recorded no acknowledgement control ID, so the v2 coherence rule
		// that pairs a code with the control ID it sent does not apply here.
		if err := received.Application.validateCode(ApplicationStage); err != nil {
			return err
		}
	}
	return nil
}

func (r Record) validateV2() error {
	if r.Schema != Schema {
		return errors.New("unsupported collection record schema version")
	}
	if err := r.common(); err != nil {
		return err
	}
	for _, received := range r.Received {
		if err := received.validateStages(); err != nil {
			return err
		}
	}
	return nil
}

func destinationFor(code string) string {
	if code == NotAcknowledged {
		return NoDestination
	}
	return SameConnection
}

func (r Received) validateStages() error {
	switch r.Mode {
	case UnknownMode:
		// A header that could not declare a mode cannot have been answered in
		// one. Unknown is not original, and it is not a pass.
		if r.Accept.Code != NotAcknowledged || r.Application.Code != NotAcknowledged {
			return errors.New("a frame with an unknown acknowledgement mode cannot have been acknowledged")
		}
	case OriginalMode:
		if r.Accept.Code != NotAcknowledged || r.Accept.ControlID != "" || r.Accept.Destination != NoDestination {
			return errors.New("original acknowledgement mode has no accept stage")
		}
		if r.Application.Destination == SeparateEndpoint {
			return errors.New("an original-mode acknowledgement is answered on the receiving connection")
		}
	case EnhancedMode:
	default:
		return errors.New("acknowledgement mode must be original, enhanced, or unknown")
	}
	// The accept stage answers MSH-15 on the connection that delivered the
	// message; only the application stage may reach a separate endpoint.
	if r.Accept.Destination == SeparateEndpoint {
		return errors.New("a commit acknowledgement is answered on the receiving connection")
	}
	if err := r.Accept.validate(AcceptStage); err != nil {
		return err
	}
	return r.Application.validate(ApplicationStage)
}

func (s Stage) validate(stage string) error {
	if err := s.validateCode(stage); err != nil {
		return err
	}
	if (s.Code == NotAcknowledged) != (s.ControlID == "") || (s.Code == NotAcknowledged) != (s.Destination == NoDestination) {
		return errors.New("an acknowledgement stage names the control ID it sent and where it sent it, or records none of either")
	}
	if s.Destination != NoDestination && s.Destination != SameConnection && s.Destination != SeparateEndpoint {
		return errors.New("acknowledgement destination must be same-connection, separate-endpoint, or none")
	}
	if len(s.ControlID) > MaxControlIDBytes || !utf8.ValidString(s.ControlID) {
		return errors.New("acknowledgement control ID exceeds the supported text bound")
	}
	return nil
}

func (s Stage) validateCode(stage string) error {
	if s.Code != NotAcknowledged && !StageCode(stage, s.Code) {
		return errors.New("an acknowledgement stage carries only its own stage codes, or none")
	}
	if (s.Code == AcceptCode || s.Code == CommitAcceptCode) && s.Reason != "" {
		return errors.New("an accepted stage carries no rejection reason")
	}
	if len(s.Reason) > MaxReasonBytes {
		return errors.New("acknowledgement reason exceeds the supported text bound")
	}
	for _, b := range []byte(s.Reason) {
		if b < 0x20 || b > 0x7e {
			return errors.New("acknowledgement reason must be printable ASCII")
		}
	}
	return nil
}

// Encode writes the record in exactly its declared version's member set. A
// readmit-collection/v1 record keeps the bytes v1 always had.
func Encode(record Record) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	var data []byte
	var err error
	if record.Schema == SchemaV1 {
		data, err = json.Marshal(record.asV1(), json.Deterministic(true))
	} else {
		data, err = json.Marshal(record, json.Deterministic(true))
	}
	if err != nil {
		return nil, errors.New("cannot encode bounded collection record")
	}
	if len(data) >= MaxBytes {
		return nil, errors.New("cannot encode bounded collection record")
	}
	return append(data, '\n'), nil
}

// Decode reads either supported version and never migrates one into the other:
// a v1 file stays a v1 record and re-encodes to the same bytes.
func Decode(data []byte) (Record, error) {
	var record Record
	if len(data) > MaxBytes {
		return record, errors.New("collection record exceeds size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return record, errors.New("invalid collection record JSON")
	}
	if declared.Schema == SchemaV1 {
		var legacy recordV1
		if err := json.Unmarshal(data, &legacy); err != nil {
			return record, errors.New("invalid collection record JSON")
		}
		record = fromV1(legacy)
		return record, record.Validate()
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, errors.New("invalid collection record JSON")
	}
	return record, record.Validate()
}
