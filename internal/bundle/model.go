// Package bundle stores immutable, byte-preserving case evidence in versioned
// directories. It has no clock, random source, network access, or CLI dependency.
package bundle

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"regexp"
	"time"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
)

const (
	Schema           = "readmit-case/v1"
	RecordedSchema   = "readmit-case/v2"
	DerivedSchema    = "readmit-case/v3"
	CollectedSchema  = "readmit-case/v4"
	MaxSources       = 128
	MaxEvents        = 10000
	MaxSourceBytes   = hl7.MaxInputBytes
	MaxEvidenceBytes = 64 << 20
	maxFileBytes     = 16 << 20
	maxBundleBytes   = 96 << 20
)

type Mode string

const (
	Imported  Mode = "imported"
	Generated Mode = "generated"
	Recorded  Mode = "recorded"
	Derived   Mode = "derived"
	Collected Mode = "collected"
)

type Direction string

const (
	Unknown  Direction = "unknown"
	Inbound  Direction = "inbound"
	Outbound Direction = "outbound"
)

// GeneratorInputs are the complete provenance contract shared with synth (#8).
// BaseTime is a declared scenario input, never an observed or import timestamp.
type GeneratorInputs struct {
	Seed             uint64    `json:"seed"`
	BaseTime         time.Time `json:"base_time"`
	GeneratorVersion string    `json:"generator_version"`
	ProfileVersion   string    `json:"profile_version"`
}

// UnmarshalJSON distinguishes a declared zero seed from an absent/null seed.
// A plain uint64 alone would silently fabricate seed zero during decoding.
func (g *GeneratorInputs) UnmarshalJSON(data []byte) error {
	var required struct {
		Seed *uint64 `json:"seed"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Seed == nil {
		return errors.New("generator inputs require an explicit seed")
	}
	type plainInputs GeneratorInputs
	var value plainInputs
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid generator inputs")
	}
	*g = GeneratorInputs(value)
	return nil
}

type Provenance struct {
	Mode       Mode             `json:"mode"`
	ImportedAt *time.Time       `json:"imported_at,omitzero"`
	Generator  *GeneratorInputs `json:"generator,omitzero"`
	StartedAt  *time.Time       `json:"started_at,omitzero"`
	SessionID  string           `json:"session_id,omitzero"`
	Derivation string           `json:"derivation,omitzero"`
}

// Observation is explicitly supplied evidence. File metadata and HL7 fields
// never supply an observed time or direction implicitly.
type Observation struct {
	Direction  Direction  `json:"direction"`
	ObservedAt *time.Time `json:"observed_at"`
}

type Input struct {
	Path         string
	Data         []byte
	Options      hl7.Options
	Observations map[int]Observation // one-based occurrence sequence in this input
}

type Source struct {
	ID          string         `json:"id"`
	Path        string         `json:"path,omitzero"`
	Format      hl7.Format     `json:"format"`
	Terminator  hl7.Terminator `json:"terminator"`
	Size        int            `json:"size"`
	SHA256      string         `json:"sha256"`
	Occurrences int            `json:"occurrences"`
}

type Manifest struct {
	Schema      string     `json:"schema"`
	State       string     `json:"state"`
	Provenance  Provenance `json:"provenance"`
	Sources     []Source   `json:"sources"`
	EventCount  int        `json:"event_count"`
	Observation *Payload   `json:"observation,omitzero"`
	Collection  *Payload   `json:"collection,omitzero"`
}

// Field refers to exact bytes within an occurrence's payload file. Values never
// pass through JSON strings, including values with invalid UTF-8 bytes.
type Field struct {
	State  hl7.State `json:"state"`
	Offset int       `json:"offset"`
	Length int       `json:"length"`
}

type Fields struct {
	DeclaredTime           Field   `json:"declared_time"`
	ControlID              Field   `json:"control_id"`
	AcknowledgedControlIDs []Field `json:"acknowledged_control_ids"`
}

type EventKind string

const (
	Message         EventKind = "message"
	Acknowledgement EventKind = "ack"
	Unparsed        EventKind = "unparsed"
)

type Payload struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type Event struct {
	ID         string         `json:"id"`
	SourceID   string         `json:"source_id"`
	Sequence   int            `json:"sequence"`
	Offset     int            `json:"offset"`
	Kind       EventKind      `json:"kind"`
	Direction  Direction      `json:"direction"`
	ObservedAt *time.Time     `json:"observed_at"`
	ImportedAt *time.Time     `json:"imported_at"`
	Fields     *Fields        `json:"fields"`
	Terminator hl7.Terminator `json:"terminator,omitzero"`
	ParseError string         `json:"parse_error,omitzero"`
	Payload    Payload        `json:"payload"`
}

type LinkKind string

const (
	Matched        LinkKind = "matched"
	UnmatchedACK   LinkKind = "unmatched_ack"
	AmbiguousACK   LinkKind = "ambiguous_ack"
	Unacknowledged LinkKind = "unacknowledged_message"
)

type Correlation struct {
	Kind       LinkKind `json:"kind"`
	ACKID      string   `json:"ack_id,omitzero"`
	MessageIDs []string `json:"message_ids"`
}

type Bundle struct {
	Manifest     Manifest
	Events       []Event
	Correlations []Correlation
	Identity     string
	Observation  *observation.Snapshot
	Collection   *collection.Record
	payloads     map[string][]byte
}

// Counts totals occurrences by kind. Callers reporting a case summary share
// this tally instead of each deriving one from Events.
func (b *Bundle) Counts() map[EventKind]int {
	counts := make(map[EventKind]int, 3)
	for _, event := range b.Events {
		counts[event.Kind]++
	}
	return counts
}

// Raw returns a private copy of the complete occurrence, including framing.
func (b *Bundle) Raw(eventID string) ([]byte, error) {
	raw, ok := b.payloads[eventID]
	if !ok {
		return nil, errors.New("unknown occurrence")
	}
	return bytes.Clone(raw), nil
}

// declaredTimestamp is the shape of an HL7 DTM: a year, optionally narrowing to
// a month, day, hour, minute and second, optionally a fraction, optionally a
// UTC offset, and optionally the degree of precision the sender declared.
var declaredTimestamp = regexp.MustCompile(`^[0-9]{4}([0-9]{2}){0,5}(\.[0-9]{1,4})?([+-][0-9]{4})?(\^[YLDHMS])?$`)

// DeclaredTimestamp reports whether these declared-time bytes are shaped like a
// timestamp and nothing else. A malformed declared-time field can hold any
// bytes at all, so a view that displays one without asking first displays only
// the ones that can be nothing but a time. Everything else is a value, read
// deliberately like every other value.
//
// This is one rule for one question, held here rather than once per view, so
// the command line and the desktop shell cannot come to disagree about which
// declared times a person sees without asking for them.
func DeclaredTimestamp(value []byte) bool { return declaredTimestamp.Match(value) }

// Value returns the exact field bytes; the caller must separately inspect State.
func (b *Bundle) Value(eventID string, field Field) []byte {
	raw := b.payloads[eventID]
	if field.Offset < 0 || field.Length < 0 || field.Offset > len(raw) || field.Length > len(raw)-field.Offset {
		return nil
	}
	return bytes.Clone(raw[field.Offset : field.Offset+field.Length])
}
