package importer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

// MappingState is what a recipe did with one envelope record: the declared
// operators resolved every value, or they did not and the record's own bytes
// were retained instead. There is no third state, and no partly mapped record:
// provenance that is half read is the kind that gets believed.
type MappingState string

const (
	Mapped   MappingState = "mapped"
	Unmapped MappingState = "unmapped"
)

// The reasons one record is retained unmapped. Each is a fixed sentence: a
// reason names the declaration that did not resolve and never repeats a column
// name, a located value, or any byte of the evidence.
const (
	ReasonRecord    = "the record's bytes do not divide into envelope fields"
	ReasonFields    = "the record does not hold the declared number of envelope fields"
	ReasonMissing   = "a declared locator names nothing in this record"
	ReasonContent   = "a declared value is not character data that can be read without altering it"
	ReasonRemainder = "the remaining bytes of the member do not divide into envelope records"
	ReasonPayload   = "the declared payload value does not decode under the declared payload operator"
	ReasonFraming   = "the decoded payload contradicts the declared payload framing"
	ReasonTime      = "the declared observed time does not read under the declared time operator"
	ReasonDirection = "the declared direction value table has no entry for this record's value"
	ReasonLabel     = "a declared source or channel value is not one bounded printable label"
)

// The instants a mapped observed time may name, matching the range a case
// bundle stores. A value outside them is a value this release will not write,
// so it leaves the record unmapped rather than being clamped into range.
const (
	minUnixSecond = -62135596800
	maxUnixSecond = 253402300799
)

// Mapping is what a recipe declared about one extracted source. The observed
// time and direction reach the case bundle, because a case already holds an
// explicit observation for every occurrence. The declared source label and
// channel are recorded here instead, for the reason below.
//
// An unmapped record carries no mapped value at all: its observed time is
// absent, its direction is explicitly unknown, and its source and channel are
// empty. Its bytes are in the case, and Reason says which declaration did not
// resolve so the operator can extend the recipe and re-run. This list is the
// only place that says which sources a recipe mapped: a case bundle records the
// physical file each source was read from, and no existing case version has a
// member for a mapping state, a declared source label or a channel. Adding one
// would be a new case version, which this change does not make.
type Mapping struct {
	SourceID    string           `json:"source_id"`
	State       MappingState     `json:"state"`
	Reason      string           `json:"reason,omitzero"`
	PayloadSize int              `json:"payload_size"`
	ObservedAt  *time.Time       `json:"observed_at"`
	Source      string           `json:"source"`
	Direction   bundle.Direction `json:"direction"`
	Channel     string           `json:"channel"`
}

// MappingPreview is what a recipe would extract, before anything is written.
// It carries the recipe and the identity of the recipe's own bytes, every
// container member with the records it would become, and what each record maps
// to. It names no case, because no case exists.
type MappingPreview struct {
	Schema     string      `json:"schema"`
	Recipe     Recipe      `json:"recipe"`
	Identity   string      `json:"recipe_identity"`
	Containers []Container `json:"containers"`
	Mappings   []Mapping   `json:"mappings"`
	Totals     Totals      `json:"totals"`
	// UnmappedRecords counts the mappings whose declared operators did not all
	// resolve, which is the count a person has to act on.
	UnmappedRecords int `json:"unmapped_records"`
}

// MappingReceipt is what a recipe did extract: the same recipe, identity,
// containers and mappings the preview carried, the case it produced, and every
// occurrence the case retained as quarantined evidence rather than parsed.
type MappingReceipt struct {
	Schema          string        `json:"schema"`
	Recipe          Recipe        `json:"recipe"`
	Identity        string        `json:"recipe_identity"`
	ImportedAt      time.Time     `json:"imported_at"`
	Case            CaseRef       `json:"case"`
	Containers      []Container   `json:"containers"`
	Mappings        []Mapping     `json:"mappings"`
	Quarantined     []Quarantined `json:"quarantined"`
	Totals          Totals        `json:"totals"`
	UnmappedRecords int           `json:"unmapped_records"`
}

// ExtractMapped reads every declared container under one mapping recipe and
// returns the case bundle sources it would produce. It is the same container
// walk, the same bounds and the same refusals an import plan runs through; only
// how one member divides into sources differs. Nothing is written.
func ExtractMapped(ctx context.Context, recipe Recipe, files, folders, archives []string) (*Extraction, error) {
	if err := recipe.Validate(); err != nil {
		return nil, err
	}
	return extract(ctx, recipeDivider{recipe: recipe, locators: recipe.locators()}, files, folders, archives)
}

// locators are the declared locations one recipe reads from a record, in a
// fixed order so an envelope reader resolves the same set every time.
func (r Recipe) locators() []Locator {
	locators := []Locator{r.Payload.Locator}
	for _, declared := range []Locator{r.ObservedAt.Locator, r.Source.Locator, r.Direction.Locator, r.Channel.Locator} {
		if declared != nil {
			locators = append(locators, declared)
		}
	}
	return locators
}

// recipeDivider divides one envelope member into the sources an import writes.
type recipeDivider struct {
	recipe   Recipe
	locators []Locator
}

func (d recipeDivider) suffixes() []string { return d.recipe.Members }
func (d recipeDivider) encoding() Encoding { return d.recipe.Encoding }

func (d recipeDivider) divide(data []byte) ([]extractedSource, error) {
	records, err := envelopeRecords(d.recipe, d.locators, data)
	if err != nil {
		return nil, err
	}
	sources := make([]extractedSource, 0, len(records))
	for _, record := range records {
		sources = append(sources, d.resolve(record, data[record.offset:record.offset+record.size]))
	}
	return sources, nil
}

// resolve reads one record through the recipe's declared operators. Every
// operator must resolve: the first that does not leaves the record's own bytes
// retained with that reason and every mapped value explicitly absent.
func (d recipeDivider) resolve(record envelopeRecord, content []byte) extractedSource {
	if record.reason != "" {
		return d.retain(record, content, record.reason)
	}
	payload, ok := d.payload(record)
	if !ok {
		return d.retain(record, content, ReasonPayload)
	}
	if _, err := recordStarts(d.recipe.Payload.Framing, "", d.recipe.Payload.Terminator, payload); err != nil {
		return d.retain(record, content, ReasonFraming)
	}
	observedAt, ok := d.observedAt(record)
	if !ok {
		return d.retain(record, content, ReasonTime)
	}
	source, ok := mappedLabel(d.recipe.Source, record)
	if !ok {
		return d.retain(record, content, ReasonLabel)
	}
	channel, ok := mappedLabel(d.recipe.Channel, record)
	if !ok {
		return d.retain(record, content, ReasonLabel)
	}
	direction, ok := d.direction(record)
	if !ok {
		return d.retain(record, content, ReasonDirection)
	}
	return extractedSource{
		offset:      record.offset,
		size:        record.size,
		data:        payload,
		format:      d.recipe.Payload.Framing.storedFormat(),
		terminator:  d.recipe.Payload.Terminator,
		observation: bundle.Observation{Direction: direction, ObservedAt: observedAt},
		mapping: &Mapping{
			State:       Mapped,
			PayloadSize: len(payload),
			ObservedAt:  observedAt,
			Source:      source,
			Direction:   direction,
			Channel:     channel,
		},
	}
}

// retain keeps one record this recipe could not map, with all of its own bytes
// and none of its provenance. The record is evidence: it is stored as a source
// of the case exactly as it was read. Nothing is repaired and nothing is
// dropped, and in particular nothing is appended to mark it, because that would
// alter the bytes.
//
// Whether the case's own parser can read those bytes as a message is a separate
// fact, and it is not always no: a record whose bytes happen to begin with a
// message header parses, and appears in the case as an ordinary occurrence.
// What it never gains is provenance — its direction is explicitly unknown and
// it has no observed time — and the mapping beside it is the record of the
// declaration that did not resolve. That is why an import writes its receipt or
// writes nothing.
func (d recipeDivider) retain(record envelopeRecord, content []byte, reason string) extractedSource {
	data := bytes.Clone(content)
	return extractedSource{
		offset:      record.offset,
		size:        record.size,
		data:        data,
		format:      hl7.Raw,
		terminator:  d.recipe.Payload.Terminator,
		observation: bundle.Observation{Direction: bundle.Unknown},
		mapping: &Mapping{
			State:       Unmapped,
			Reason:      reason,
			PayloadSize: len(data),
			Direction:   bundle.Unknown,
		},
	}
}

// payload reads the declared payload value into the bytes a case bundle stores.
// verbatim keeps them as the envelope held them; base64 decodes strict padded
// standard base64, which is how an envelope that cannot carry a byte sequence
// verbatim carries it without either side altering the evidence.
func (d recipeDivider) payload(record envelopeRecord) ([]byte, bool) {
	value, located := record.value(d.recipe.Payload.Locator)
	if !located || len(value) == 0 {
		return nil, false
	}
	if d.recipe.Payload.Operator == Base64Payload {
		decoded, err := base64.StdEncoding.Strict().DecodeString(string(value))
		if err != nil || len(decoded) == 0 {
			return nil, false
		}
		return decoded, true
	}
	return bytes.Clone(value), true
}

func (d recipeDivider) observedAt(record envelopeRecord) (*time.Time, bool) {
	if d.recipe.ObservedAt.Operator == UnknownTime {
		return nil, true
	}
	value, located := record.value(d.recipe.ObservedAt.Locator)
	if !located {
		return nil, false
	}
	moment, ok := readTime(d.recipe.ObservedAt.Operator, string(value))
	if !ok {
		return nil, false
	}
	return &moment, true
}

func (d recipeDivider) direction(record envelopeRecord) (bundle.Direction, bool) {
	if d.recipe.Direction.Operator == DeclaredDirection {
		return d.recipe.Direction.Declared, true
	}
	value, located := record.value(d.recipe.Direction.Locator)
	if !located {
		return bundle.Unknown, false
	}
	for _, entry := range d.recipe.Direction.Values {
		if entry.Envelope == string(value) {
			return entry.Mapped, true
		}
	}
	return bundle.Unknown, false
}

func mappedLabel(mapping LabelMapping, record envelopeRecord) (string, bool) {
	switch mapping.Operator {
	case UnknownLabel:
		return "", true
	case DeclaredLabel:
		return mapping.Declared, true
	default:
		value, located := record.value(mapping.Locator)
		if !located || !label(string(value)) {
			return "", false
		}
		return string(value), true
	}
}

// readTime applies one typed time operator. Every operator requires the value
// to carry its own UTC offset, so an observed time is read out of the evidence
// rather than completed from a zone nobody declared. The instant is kept in UTC
// because that is the instant; the offset the source wrote is not provenance
// readmit can vouch for and is not stored as though it were.
func readTime(operator TimeOperator, value string) (time.Time, bool) {
	switch operator {
	case RFC3339Time:
		moment, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, false
		}
		return validMoment(moment)
	case UnixSecondsTime:
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil || seconds < minUnixSecond || seconds > maxUnixSecond {
			return time.Time{}, false
		}
		return validMoment(time.Unix(seconds, 0))
	case UnixMilliseconds:
		millis, err := strconv.ParseInt(value, 10, 64)
		if err != nil || millis < minUnixSecond*1000 || millis > maxUnixSecond*1000+999 {
			return time.Time{}, false
		}
		return validMoment(time.UnixMilli(millis))
	case HL7DTMTime:
		return hl7DTM(value)
	default:
		return time.Time{}, false
	}
}

// hl7DTM reads an HL7 date/time that states its own UTC offset. A DTM without
// one names a wall-clock reading whose instant is unknown, and completing it
// from the machine running the import, from a file name, or from any other
// message would be inventing provenance, so it is not read at all.
func hl7DTM(value string) (time.Time, bool) {
	if len(value) < 5 {
		return time.Time{}, false
	}
	body, offset := value[:len(value)-5], value[len(value)-5:]
	if offset[0] != '+' && offset[0] != '-' || !decimal(offset[1:]) {
		return time.Time{}, false
	}
	fraction := ""
	if dot := strings.IndexByte(body, '.'); dot >= 0 {
		fraction, body = body[dot:], body[:dot]
		if len(fraction) < 2 || len(fraction) > 5 || !decimal(fraction[1:]) {
			return time.Time{}, false
		}
	}
	layout := ""
	switch {
	case len(body) == 12 && fraction == "":
		layout = "200601021504"
	case len(body) == 14:
		layout = "20060102150405"
	default:
		return time.Time{}, false
	}
	if !decimal(body) {
		return time.Time{}, false
	}
	if fraction != "" {
		layout += "." + strings.Repeat("0", len(fraction)-1)
	}
	moment, err := time.Parse(layout+"-0700", body+fraction+offset)
	if err != nil {
		return time.Time{}, false
	}
	return validMoment(moment)
}

// validMoment holds a mapped observed time to the range a case bundle accepts,
// in UTC, so a value a recipe reads is a value the writer will take.
func validMoment(moment time.Time) (time.Time, bool) {
	moment = moment.UTC()
	if moment.IsZero() || moment.Year() < 1 || moment.Year() > 9999 {
		return time.Time{}, false
	}
	return moment, true
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// NewMappingPreview describes a mapped extraction without writing anything. It
// validates the recipe: this is where a recipe becomes part of a published
// document, and a document must not name a mapping that could not have run.
func NewMappingPreview(recipe Recipe, e *Extraction) (MappingPreview, error) {
	if err := recipe.Validate(); err != nil {
		return MappingPreview{}, err
	}
	identity, err := recipe.Identity()
	if err != nil {
		return MappingPreview{}, err
	}
	return MappingPreview{
		Schema:          MappingPreviewSchema,
		Recipe:          recipe,
		Identity:        identity,
		Containers:      e.Containers,
		Mappings:        e.Mappings,
		Totals:          e.Totals,
		UnmappedRecords: e.UnmappedRecords,
	}, nil
}

// NewMappingReceipt describes the case a mapped extraction actually wrote. The
// written evidence is checked against the extraction it came from first, and
// every extracted source must carry exactly one mapping, so a receipt can never
// describe evidence other than the evidence beside it. The recipe is validated
// here for the same reason NewMappingPreview validates it.
func NewMappingReceipt(recipe Recipe, e *Extraction, importedAt time.Time, b *bundle.Bundle) (MappingReceipt, error) {
	if err := recipe.Validate(); err != nil {
		return MappingReceipt{}, err
	}
	identity, err := recipe.Identity()
	if err != nil {
		return MappingReceipt{}, err
	}
	if err := e.verifyWritten(b); err != nil {
		return MappingReceipt{}, err
	}
	if len(e.Mappings) != len(e.Inputs) {
		return MappingReceipt{}, errors.New("the mapped extraction does not describe every written source")
	}
	return MappingReceipt{
		Schema:          MappingReceiptSchema,
		Recipe:          recipe,
		Identity:        identity,
		ImportedAt:      importedAt,
		Case:            CaseRef{Identity: b.Identity, Schema: b.Manifest.Schema, Provenance: string(b.Manifest.Provenance.Mode)},
		Containers:      e.Containers,
		Mappings:        e.Mappings,
		Quarantined:     quarantinedOccurrences(b),
		Totals:          e.Totals,
		UnmappedRecords: e.UnmappedRecords,
	}, nil
}

// EncodeMappingPreview and EncodeMappingReceipt write one document
// deterministically, so the same mapped import produces the same bytes on
// every machine.
func EncodeMappingPreview(preview MappingPreview) ([]byte, error) {
	if preview.Schema != MappingPreviewSchema {
		return nil, ErrUnsupportedRecipe
	}
	return encodeDocument(preview)
}

func EncodeMappingReceipt(receipt MappingReceipt) ([]byte, error) {
	if receipt.Schema != MappingReceiptSchema {
		return nil, ErrUnsupportedRecipe
	}
	if receipt.Case.Identity == "" {
		return nil, errors.New("a mapping receipt names the case it wrote")
	}
	return encodeDocument(receipt)
}
