package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
)

var versionToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]{0,127}$`)

func build(inputs []Input, provenance Provenance) (*Bundle, error) {
	if len(inputs) == 0 && provenance.Mode != Recorded && provenance.Mode != Collected || len(inputs) > MaxSources {
		return nil, errors.New("bundle requires between 1 and 128 sources")
	}
	if err := validateProvenance(provenance); err != nil {
		return nil, err
	}
	b := &Bundle{Manifest: Manifest{Schema: Schema, State: "complete", Provenance: provenance}, payloads: make(map[string][]byte)}
	if provenance.Mode == Recorded {
		b.Manifest.Schema = RecordedSchema
	}
	if provenance.Mode == Derived {
		b.Manifest.Schema = DerivedSchema
	}
	if provenance.Mode == Collected {
		b.Manifest.Schema = CollectedSchema
	}
	total := 0
	for i, input := range inputs {
		total += len(input.Data)
		if len(input.Data) > MaxSourceBytes || total > MaxEvidenceBytes {
			return nil, errors.New("source exceeds 16 MiB or bundle evidence exceeds 64 MiB")
		}
		if provenance.Mode != Imported && input.Path != "" || provenance.Mode == Imported && (input.Path == "" || !utf8.ValidString(input.Path)) {
			return nil, errors.New("source path does not match provenance mode")
		}
		format, terminator, err := inputOptions(input)
		if err != nil {
			return nil, err
		}
		source := Source{ID: fmt.Sprintf("s%04d", i+1), Path: input.Path, Format: format, Terminator: terminator, Size: len(input.Data), SHA256: digest(input.Data)}
		for start := 0; start < len(input.Data) || source.Occurrences == 0; {
			end, framingError := NextOccurrence(input.Data, start, format)
			source.Occurrences++
			if len(b.Events) == MaxEvents {
				return nil, errors.New("bundle exceeds 10000 occurrences")
			}
			observation := input.Observations[source.Occurrences]
			if observation.Direction == "" {
				observation.Direction = Unknown
			}
			if observation.Direction != Unknown && observation.Direction != Inbound && observation.Direction != Outbound {
				return nil, errors.New("direction must be unknown, inbound, or outbound")
			}
			if observation.ObservedAt != nil && (provenance.Mode == Generated || provenance.Mode == Derived || !validTime(*observation.ObservedAt)) {
				return nil, errors.New("observed time must be a valid imported observation")
			}
			raw := bytes.Clone(input.Data[start:end])
			id := fmt.Sprintf("%s-e%06d", source.ID, source.Occurrences)
			event := Event{ID: id, SourceID: source.ID, Sequence: source.Occurrences, Offset: start, Kind: Unparsed, Direction: observation.Direction, ObservedAt: observation.ObservedAt, ImportedAt: provenance.ImportedAt, Payload: Payload{Path: "payloads/" + id + ".bin", Size: len(raw), SHA256: digest(raw)}}
			if framingError != "" {
				event.ParseError = framingError
			} else {
				parseOccurrence(&event, raw, hl7.Options{Format: format, Terminator: terminator})
			}
			b.Events = append(b.Events, event)
			b.payloads[id] = raw
			start = end
		}
		for sequence := range input.Observations {
			if sequence < 1 || sequence > source.Occurrences {
				return nil, errors.New("observation references an unknown occurrence")
			}
		}
		b.Manifest.Sources = append(b.Manifest.Sources, source)
	}
	b.Manifest.EventCount = len(b.Events)
	b.Correlations = correlate(b)
	return b, nil
}

func validateProvenance(p Provenance) error {
	if p.Mode != Derived && p.Derivation != "" {
		return errors.New("derivation is only valid for derived evidence")
	}
	switch p.Mode {
	case Imported:
		if p.ImportedAt == nil || !validTime(*p.ImportedAt) || p.Generator != nil || p.StartedAt != nil || p.SessionID != "" {
			return errors.New("imported provenance requires import time and no generator inputs")
		}
	case Generated:
		if p.ImportedAt != nil || p.Generator == nil || !validTime(p.Generator.BaseTime) || !versionToken.MatchString(p.Generator.GeneratorVersion) || !versionToken.MatchString(p.Generator.ProfileVersion) || p.StartedAt != nil || p.SessionID != "" {
			return errors.New("generated provenance requires only seed, base time, generator version, and profile version")
		}
	case Recorded, Collected:
		if p.ImportedAt != nil || p.Generator != nil || p.StartedAt == nil || !validTime(*p.StartedAt) || len(p.SessionID) != 32 {
			return errors.New("recorded provenance requires startup time and session ID")
		}
	case Derived:
		if p.ImportedAt != nil || p.Generator != nil || p.StartedAt != nil || p.SessionID != "" || p.Derivation != "readmit-redact/v1" {
			return errors.New("derived provenance requires only the named testing derivation")
		}
	default:
		return errors.New("unsupported provenance mode")
	}
	return nil
}

func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }

func inputOptions(input Input) (hl7.Format, hl7.Terminator, error) {
	format := input.Options.Format
	if format == "" || format == "auto" {
		format = hl7.Raw
		if len(input.Data) > 0 && input.Data[0] == 0x0b {
			format = hl7.MLLP
		}
	}
	terminator := input.Options.Terminator
	if terminator == "" {
		terminator = "auto"
	}
	if format != hl7.Raw && format != hl7.MLLP {
		return "", "", errors.New("format must be auto, raw, or mllp")
	}
	if terminator != "auto" && terminator != hl7.CR && terminator != hl7.LF && terminator != hl7.CRLF {
		return "", "", errors.New("terminator must be auto, cr, lf, or crlf")
	}
	return format, terminator, nil
}

// NextOccurrence returns the exclusive end and framing diagnostic for the next
// retained occurrence. Callers supply a validated Raw or MLLP format and a start
// within raw. Capture and recorded-wire observation indexing share this policy:
// complete frames survive malformed payloads; broken framing retains the entire
// remaining suffix without guessing a resynchronization point.
func NextOccurrence(raw []byte, start int, format hl7.Format) (int, string) {
	if format == hl7.Raw {
		return len(raw), ""
	}
	if start >= len(raw) || raw[start] != 0x0b {
		return len(raw), "invalid MLLP framing: expected start block; remainder preserved"
	}
	n := bytes.IndexByte(raw[start+1:], 0x1c)
	if n < 0 {
		return len(raw), "invalid MLLP framing: missing end block; remainder preserved"
	}
	end := start + 1 + n
	if end+1 >= len(raw) || raw[end+1] != '\r' {
		return len(raw), "invalid MLLP framing: missing final CR; remainder preserved"
	}
	return end + 2, ""
}

func parseOccurrence(event *Event, raw []byte, options hl7.Options) {
	doc, err := hl7.Parse(raw, options)
	if err != nil {
		event.ParseError = err.Error()
		return
	}
	m := doc.Messages[0]
	event.Kind, event.Terminator = Message, m.Terminator
	header := m.Segments[0]
	event.Fields = &Fields{DeclaredTime: fieldReference(header.Field(7)), ControlID: fieldReference(header.Field(10))}
	messageType := doc.Bytes(header.Field(9).Span)
	if bytes.Equal(bytes.SplitN(messageType, []byte{m.Delimiters.Component}, 2)[0], []byte("ACK")) {
		event.Kind = Acknowledgement
	}
	for _, segment := range m.Segments {
		if segment.ID == "MSA" {
			event.Kind = Acknowledgement
			event.Fields.AcknowledgedControlIDs = append(event.Fields.AcknowledgedControlIDs, fieldReference(segment.Field(2)))
		}
	}
}

func fieldReference(f hl7.Field) Field {
	return Field{State: f.State, Offset: f.Span.Start, Length: f.Span.End - f.Span.Start}
}

func correlate(b *Bundle) []Correlation {
	type key struct{ source, control string }
	messages := make(map[key][]string)
	for _, event := range b.Events {
		if event.Kind == Message && event.Fields.ControlID.State == hl7.Present {
			k := key{event.SourceID, string(b.Value(event.ID, event.Fields.ControlID))}
			messages[k] = append(messages[k], event.ID)
		}
	}
	var links []Correlation
	acknowledged := make(map[string]bool)
	for _, event := range b.Events {
		if event.Kind != Acknowledgement {
			continue
		}
		link := Correlation{Kind: UnmatchedACK, ACKID: event.ID}
		// Multiple MSA segments cannot choose a single initiating occurrence.
		if targets := event.Fields.AcknowledgedControlIDs; len(targets) == 1 && targets[0].State == hl7.Present {
			link.MessageIDs = messages[key{event.SourceID, string(b.Value(event.ID, targets[0]))}]
		}
		switch len(link.MessageIDs) {
		case 0:
		case 1:
			link.Kind = Matched
			acknowledged[link.MessageIDs[0]] = true
		default:
			link.Kind = AmbiguousACK
		}
		links = append(links, link)
	}
	for _, event := range b.Events {
		if event.Kind == Message && !acknowledged[event.ID] {
			links = append(links, Correlation{Kind: Unacknowledged, MessageIDs: []string{event.ID}})
		}
	}
	return links
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
