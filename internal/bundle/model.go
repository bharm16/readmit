// Package bundle stores immutable, byte-preserving case evidence in versioned
// directories. It has no clock, random source, network access, or CLI dependency.
package bundle

import (
	"bytes"
	"errors"
	"time"

	"github.com/bharm16/readmit/internal/hl7"
)

const (
	Schema           = "readmit-case/v1"
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

type Provenance struct {
	Mode       Mode             `json:"mode"`
	ImportedAt *time.Time       `json:"imported_at,omitzero"`
	Generator  *GeneratorInputs `json:"generator,omitzero"`
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
	Schema     string     `json:"schema"`
	State      string     `json:"state"`
	Provenance Provenance `json:"provenance"`
	Sources    []Source   `json:"sources"`
	EventCount int        `json:"event_count"`
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
	payloads     map[string][]byte
}

// Raw returns a private copy of the complete occurrence, including framing.
func (b *Bundle) Raw(eventID string) ([]byte, error) {
	raw, ok := b.payloads[eventID]
	if !ok {
		return nil, errors.New("unknown occurrence")
	}
	return bytes.Clone(raw), nil
}

// Value returns the exact field bytes; the caller must separately inspect State.
func (b *Bundle) Value(eventID string, field Field) []byte {
	raw := b.payloads[eventID]
	if field.Offset < 0 || field.Length < 0 || field.Offset > len(raw) || field.Length > len(raw)-field.Offset {
		return nil
	}
	return bytes.Clone(raw[field.Offset : field.Offset+field.Length])
}
