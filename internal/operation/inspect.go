package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/dictionary"
	"github.com/bharm16/readmit/internal/hl7"
)

// How a framing or terminator reached the parser: the person declared it, or
// the parser detected it because the declaration was automatic.
const (
	InspectDeclared = "declared"
	InspectDetected = "detected"
)

// The two ways a message's fields are named. Labels come from the bundled
// v2.5.1 dictionary only when the message itself declares that version; any
// other message is named by position alone, never by a borrowed meaning.
const (
	InspectLabelled   = "HL7 v2.5.1 field labels v1 (syntax only)"
	InspectPositional = "positional only"
)

// The kinds of one inspection row, in the order a message reports them.
const (
	InspectMessageRow    = "message"
	InspectSegmentRow    = "segment"
	InspectFieldRow      = "field"
	InspectRepetitionRow = "repetition"
)

// The declarations `readmit inspect` accepts, refused in its own words.
var (
	ErrInspectFormat     = errors.New("format must be auto, raw, or mllp")
	ErrInspectTerminator = errors.New("terminator must be auto, cr, lf, or crlf")
)

// The framings an inspection is declared under; auto detects it from the file.
const (
	autoFormat = "auto"
	rawFormat  = string(hl7.Raw)
	mllpFormat = string(hl7.MLLP)
)

// The segment terminators an inspection is declared under; auto detects it.
const (
	autoTerminator = "auto"
	crTerminator   = string(hl7.CR)
	lfTerminator   = string(hl7.LF)
	crlfTerminator = string(hl7.CRLF)
)

// InspectOptions checks the framing and terminator an inspection is declared
// under, each either named or auto, and returns the parser's options. The
// command line and the desktop facade both declare through it.
func InspectOptions(format, terminator string) (hl7.Options, error) {
	if format != autoFormat && format != rawFormat && format != mllpFormat {
		return hl7.Options{}, ErrInspectFormat
	}
	if terminator != autoTerminator && terminator != crTerminator && terminator != lfTerminator && terminator != crlfTerminator {
		return hl7.Options{}, ErrInspectTerminator
	}
	return hl7.Options{Format: hl7.Format(format), Terminator: hl7.Terminator(terminator)}, nil
}

// InspectionRow is one line of what `readmit inspect` reports about a file:
// a message, a segment, a field or one repetition of a repeated field, in the
// order the command prints them. Start and End are the half-open byte range
// in the original file, MLLP offsets included; an omitted field has no bytes
// and reports neither. Rows carry no value: a caller that shows values asks
// Inspected.Value for the rows it keeps, and sets Value, ValueTruncated and
// ValueShownBytes when it shows a field's value.
type InspectionRow struct {
	Kind       string         `json:"kind"`
	Message    int            `json:"message"`
	Segment    string         `json:"segment,omitzero"`
	Field      int            `json:"field,omitzero"`
	Repetition int            `json:"repetition,omitzero"`
	Label      string         `json:"label,omitzero"`
	Profile    string         `json:"profile,omitzero"`
	Terminator hl7.Terminator `json:"terminator,omitzero"`
	State      hl7.State      `json:"state,omitzero"`
	Start      int            `json:"start"`
	End        int            `json:"end"`
	// Value is the field's bytes as an escaped ASCII string, so no byte of the
	// file reaches a display unescaped.
	Value           string `json:"value,omitzero"`
	ValueTruncated  bool   `json:"value_truncated,omitzero"`
	ValueShownBytes int    `json:"value_shown_bytes,omitzero"`
}

// Inspected is one file read whole and parsed under the declared framing and
// terminator. It holds the parsed view and nothing it wrote: inspecting never
// changes the source, and the only file it may create is the round trip a
// caller named, which is the source's bytes exactly.
type Inspected struct {
	Format              hl7.Format
	FormatSelection     string
	TerminatorSelection string

	size     int
	document *hl7.Document
	labels   *dictionary.Dictionary
}

// loadLabels reads the bundled field labels from the executable once: they
// never change while it runs, and every inspection names fields by them.
var loadLabels = sync.OnceValues(dictionary.Load)

// InspectFile is `readmit inspect`: it reads one regular file within the
// parser's input bound, parses it under the declared options, loads the
// bundled field labels, and writes the byte-identical round trip to a new file
// when one is named. A refusal at any step writes nothing, and the round trip
// is written only once the file parsed, before anything is reported.
func InspectFile(path string, options hl7.Options, roundtrip string) (*Inspected, error) {
	data, err := ReadInputFile(path, hl7.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	document, err := hl7.Parse(data, options)
	if err != nil {
		return nil, err
	}
	named, err := loadLabels()
	if err != nil {
		return nil, err
	}
	if roundtrip != "" {
		if err := WriteNewFile(roundtrip, document.Serialize(),
			"cannot create round-trip file; destination must be new and writable",
			"cannot write round-trip file"); err != nil {
			return nil, err
		}
	}
	return &Inspected{
		Format:              document.Format,
		FormatSelection:     selection(string(options.Format)),
		TerminatorSelection: selection(string(options.Terminator)),
		size:                len(data),
		document:            document,
		labels:              named,
	}, nil
}

// Messages is how many messages the file holds.
func (i *Inspected) Messages() int { return len(i.document.Messages) }

// Bytes is how many bytes were read.
func (i *Inspected) Bytes() int { return i.size }

// Digest is the SHA-256 of the bytes that were read, which is also the digest
// of a round trip written from them. It is computed only when asked for, from
// the parser's own copy of those bytes.
func (i *Inspected) Digest() string {
	sum := sha256.Sum256(i.document.Serialize())
	return hex.EncodeToString(sum[:])
}

// Value is one field row's bytes as `readmit inspect --show-values` prints
// them: an escaped ASCII string. A positive limit escapes at most that many
// leading bytes and reports the actual byte count and whether the value was
// cut; the cut never falls inside a character, so what is shown is the start
// of what the command prints. An omitted field, and any row that is not a
// field, has no value.
func (i *Inspected) Value(row InspectionRow, limit int) (string, bool, int) {
	if row.Kind != InspectFieldRow || row.State == hl7.Omitted {
		return "", false, 0
	}
	field := i.document.Bytes(hl7.Span{Start: row.Start, End: row.End})
	if limit <= 0 || len(field) <= limit {
		return strconv.QuoteToASCII(string(field)), false, len(field)
	}
	end := limit
	for back := 0; back < utf8.UTFMax-1 && end > 0 && !utf8.RuneStart(field[end]); back++ {
		end--
	}
	return strconv.QuoteToASCII(string(field[:end])), true, end
}

// Rows visits every row the command prints, in its order, until visit
// returns false. A field is reported at every position the segment holds and
// at every further position the dictionary labels, so a labelled field the
// message omits is named as omitted rather than left out. Repetitions are
// listed only for a field that repeats.
func (i *Inspected) Rows(visit func(InspectionRow) bool) {
	for m, message := range i.document.Messages {
		number := m + 1
		labels := messageLabels(i.document, message, i.labels)
		profile := InspectPositional
		if labels != nil {
			profile = InspectLabelled
		}
		if !visit(InspectionRow{Kind: InspectMessageRow, Message: number, Terminator: message.Terminator, Profile: profile, Start: message.Span.Start, End: message.Span.End}) {
			return
		}
		for _, segment := range message.Segments {
			if !visit(InspectionRow{Kind: InspectSegmentRow, Message: number, Segment: segment.ID, Start: segment.Span.Start, End: segment.Span.End}) {
				return
			}
			last := len(segment.Fields)
			for position := range labels[segment.ID] {
				last = max(last, position)
			}
			for position := 1; position <= last; position++ {
				field := segment.Field(position)
				row := InspectionRow{Kind: InspectFieldRow, Message: number, Segment: segment.ID, Field: position, Label: labels[segment.ID][position], State: field.State}
				if field.State != hl7.Omitted {
					row.Start, row.End = field.Span.Start, field.Span.End
				}
				if !visit(row) {
					return
				}
				if len(field.Repetitions) < 2 {
					continue
				}
				for r, repetition := range field.Repetitions {
					if !visit(InspectionRow{Kind: InspectRepetitionRow, Message: number, Segment: segment.ID, Field: position, Repetition: r + 1, State: repetition.State, Start: repetition.Span.Start, End: repetition.Span.End}) {
						return
					}
				}
			}
		}
	}
}

// selection says whether a declaration was made or left to detection.
func selection(value string) string {
	if value == "" || value == "auto" {
		return InspectDetected
	}
	return InspectDeclared
}

// messageLabels are the dictionary's labels when the message declares the
// dictionary's version in MSH-12, and nil otherwise.
func messageLabels(doc *hl7.Document, message hl7.Message, labels *dictionary.Dictionary) map[string]map[int]string {
	version := doc.Bytes(message.Segments[0].Field(12).Span)
	if strings.SplitN(string(version), string(message.Delimiters.Component), 2)[0] == labels.HL7Version {
		return labels.Segments
	}
	return nil
}
