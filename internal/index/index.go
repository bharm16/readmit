// Package index answers "where is this value in this case" without reading
// every occurrence again. It holds one derived, disposable document beside a
// case bundle: the decoded state and byte span of the fields an operator named,
// the source metadata the bundle's own manifest declares, and — only where the
// operator asked for it — a bounded copy or a digest of the decoded bytes.
//
// Nothing here is evidence. ADR-0002 makes the case bundle directory the
// canonical artifact; an index is a second reading of it that can be deleted at
// any moment and rebuilt from the same bytes, so a damaged, truncated, expired
// or stale index is a rebuild and never a loss. Every query re-verifies the case
// through the shared reader and refuses the moment the index and the evidence
// disagree, so an answer is never served from an index the evidence no longer
// supports. Nothing in this package writes, repairs or reorders a byte of a
// case, and artifactpath refuses an index destination inside retained evidence.
//
// A retained decoded field is patient data, not harmless metadata. What is
// retained, in what form, and until when are three declarations the operator
// makes explicitly; there is no default field set, no default form and no
// default expiry. ADR-0008 records why this is a readmit-owned file and not a
// database.
package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
)

const (
	// Schema is the only index contract this release reads. A new member means
	// a new version string and a reader for both, never an added member here
	// and never an in-place migration.
	Schema = "readmit-index/v1"

	// MaxFields bounds how many fields one index retains, MaxValueBytes bounds
	// how much of one decoded value is retained, and MaxIndexBytes bounds the
	// whole document. An index past a bound is refused, never truncated: the
	// operator narrows the field list rather than receiving a partial index.
	MaxFields     = 16
	MaxValueBytes = 128
	MaxIndexBytes = 16 << 20

	digestLength = 64
)

// ErrUnsupportedVersion reports an index written under a contract version this
// release does not read. It is distinct from an index this release reads and
// rejects, so a caller can say which one it was handed.
var ErrUnsupportedVersion = errors.New("unsupported index document version")

// ErrDamaged reports an index whose contents no longer match what was recorded
// for them. The evidence is untouched; the answer is to build the index again.
var ErrDamaged = errors.New("index contents do not match the index that was written; rebuild it from the case")

// ErrStale reports an index built over evidence other than the case it was
// opened against. Serving its answers would describe a case that is not there.
var ErrStale = errors.New("index was built from different evidence than this case; rebuild it from the case")

// ErrExpired reports an index past the retention its own policy declared.
var ErrExpired = errors.New("the retention declared for this index has ended; rebuild it or delete it")

// Retention is the form a decoded value is retained in. There is deliberately
// no default: retaining a decoded HL7 field means retaining patient data, so
// the operator states which of these three an index is worth.
type Retention string

const (
	// RetainValues keeps the first MaxValueBytes bytes of each present value.
	// It answers exact and substring queries, and it is the only form that
	// holds message content at rest.
	RetainValues Retention = "values"
	// RetainDigests keeps a SHA-256 of each complete present value and no
	// value bytes. It answers exact queries over values of any length. It is
	// not de-identification: a short value drawn from a small set is recovered
	// by hashing that set, and the documentation says so.
	RetainDigests Retention = "digests"
	// RetainStates keeps neither, only the decoded state and byte span. It
	// answers which occurrences declare, omit, empty or null a field, and holds
	// nothing that was read out of a message.
	RetainStates Retention = "states"
)

// Policy is the complete set of retention declarations one index is built
// under. Every member is required and none has a default.
type Policy struct {
	// Fields are the canonical selectors this index retains, in the order a
	// record lists its values. An empty list is refused: an index of nothing
	// is a mistake, not a default.
	Fields []string `json:"fields"`
	// Retention is the form each value is retained in.
	Retention Retention `json:"retention"`
	// RetainUntil is the instant past which this index is no longer served.
	// An explicit null declares that it is retained until someone deletes it,
	// which is a decision the operator states rather than one taken for them.
	RetainUntil *time.Time `json:"retain_until"`
}

// UnmarshalJSON requires every declaration explicitly, so an omitted one cannot
// decode into a permissive zero value, and then re-decodes rejecting unknown
// members so a misspelled declaration is an error rather than one that quietly
// did nothing. Both passes are needed: a custom unmarshaler does not inherit
// the caller's strictness. A valid document states retain_until as an explicit
// null whenever no end was declared, and a typed pointer cannot tell that from
// an absent member, so presence is read from the raw member the way every other
// presence check in readmit reads it.
func (p *Policy) UnmarshalJSON(data []byte) error {
	invalid := errors.New("invalid index retention policy")
	var members struct {
		Fields      jsontext.Value `json:"fields"`
		Retention   jsontext.Value `json:"retention"`
		RetainUntil jsontext.Value `json:"retain_until"`
	}
	if err := json.Unmarshal(data, &members, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	if len(members.Fields) == 0 || len(members.Retention) == 0 || len(members.RetainUntil) == 0 {
		return errors.New("an index policy declares its fields, its retention form, and an explicit retention end")
	}
	type plainPolicy Policy
	var value plainPolicy
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return invalid
	}
	*p = Policy(value)
	return nil
}

// Source is the metadata one indexed source declares. It is copied from the
// case manifest the shared reader accepted, never read off the filesystem.
// Path is present only where the case itself records one: derived and collected
// evidence carries no original source path, and an index never invents one.
type Source struct {
	ID          string         `json:"id"`
	Path        string         `json:"path,omitzero"`
	Format      hl7.Format     `json:"format"`
	Terminator  hl7.Terminator `json:"terminator"`
	Size        int            `json:"size"`
	SHA256      string         `json:"sha256"`
	Occurrences int            `json:"occurrences"`
}

// Case names the exact evidence an index describes. Identity is the case
// bundle's own content identity, which is what makes a stale index detectable.
type Case struct {
	Identity   string   `json:"identity"`
	Schema     string   `json:"schema"`
	Provenance string   `json:"provenance"`
	Sources    []Source `json:"sources"`
}

// Value is one indexed field of one occurrence. Offset and Length are the byte
// span inside that occurrence's retained payload, so a reader navigates to the
// exact original bytes rather than to this copy of them.
type Value struct {
	Selector string    `json:"selector"`
	State    hl7.State `json:"state"`
	Offset   int       `json:"offset"`
	Length   int       `json:"length"`
	// Bytes is the retained prefix under RetainValues and is absent otherwise.
	Bytes []byte `json:"bytes,omitzero"`
	// Digest is the SHA-256 of the complete value under RetainDigests.
	Digest string `json:"digest,omitzero"`
	// Truncated says the retained prefix is shorter than the value. It is
	// always stated, so a shortened value is never mistaken for a whole one.
	Truncated bool `json:"truncated"`
}

// Record is one indexed occurrence. ParseError is the reason the case itself
// recorded for an occurrence this release cannot decode; such a record carries
// no values at all rather than claiming every indexed field is absent from it.
type Record struct {
	ID         string           `json:"id"`
	SourceID   string           `json:"source_id"`
	Sequence   int              `json:"sequence"`
	Offset     int              `json:"offset"`
	Size       int              `json:"size"`
	Kind       bundle.EventKind `json:"kind"`
	Direction  bundle.Direction `json:"direction"`
	ObservedAt *time.Time       `json:"observed_at"`
	ImportedAt *time.Time       `json:"imported_at"`
	ParseError string           `json:"parse_error,omitzero"`
	Values     []Value          `json:"values"`
}

// Document is one complete index. Digest covers everything else in it, so a
// document altered after it was written is reported as damaged instead of
// answering a query from bytes nothing stands behind. It detects damage, not
// forgery: anyone who can rewrite the file can recompute the digest, exactly as
// ADR-0004 says a hash is not source authentication. It is recorded by Encode
// and verified by Decode; a document that was built and never written carries
// no digest, because nothing has been written yet for one to stand behind.
type Document struct {
	Schema  string    `json:"schema"`
	Case    Case      `json:"case"`
	Policy  Policy    `json:"policy"`
	BuiltAt time.Time `json:"built_at"`
	Records []Record  `json:"records"`
	Digest  string    `json:"digest"`
}

// Build reads one verified case bundle and returns the index of it. It is a
// pure function of the evidence, the policy and the build instant, which is
// what makes rebuilding from the canonical directory possible at any time: the
// same case and the same policy always produce the same records.
//
// It never writes, and a cancelled build produces nothing at all rather than a
// partial index.
func Build(ctx context.Context, opened *bundle.Bundle, policy Policy, builtAt time.Time) (Document, error) {
	if opened == nil {
		return Document{}, errors.New("an index is built from an opened case bundle")
	}
	parsed, err := selectors(policy)
	if err != nil {
		return Document{}, err
	}
	if !validTime(builtAt) {
		return Document{}, errors.New("an index records the instant it was built")
	}
	document := Document{
		Schema: Schema,
		Case: Case{
			Identity:   opened.Identity,
			Schema:     opened.Manifest.Schema,
			Provenance: string(opened.Manifest.Provenance.Mode),
		},
		Policy:  policy,
		BuiltAt: builtAt.UTC(),
		Records: make([]Record, 0, len(opened.Events)),
	}
	document.Case.Sources = make([]Source, 0, len(opened.Manifest.Sources))
	options := make(map[string]hl7.Options, len(opened.Manifest.Sources))
	for _, source := range opened.Manifest.Sources {
		document.Case.Sources = append(document.Case.Sources, Source{
			ID: source.ID, Path: source.Path, Format: source.Format, Terminator: source.Terminator,
			Size: source.Size, SHA256: source.SHA256, Occurrences: source.Occurrences,
		})
		options[source.ID] = hl7.Options{Format: source.Format, Terminator: source.Terminator}
	}
	for _, event := range opened.Events {
		if err := ctx.Err(); err != nil {
			return Document{}, errors.New("index build cancelled; nothing was written")
		}
		record := Record{
			ID: event.ID, SourceID: event.SourceID, Sequence: event.Sequence, Offset: event.Offset,
			Size: event.Payload.Size, Kind: event.Kind, Direction: event.Direction,
			ObservedAt: event.ObservedAt, ImportedAt: event.ImportedAt, Values: []Value{},
		}
		if event.ParseError != "" {
			record.ParseError = event.ParseError
			document.Records = append(document.Records, record)
			continue
		}
		raw, err := opened.Raw(event.ID)
		if err != nil {
			return Document{}, errors.New("case evidence does not hold every occurrence it records")
		}
		decoded, err := hl7.Parse(raw, options[event.SourceID])
		if err != nil {
			return Document{}, errors.New("case evidence no longer decodes the way its manifest records")
		}
		for i, selector := range parsed {
			value, err := decoded.Select(0, selector)
			if err != nil {
				return Document{}, errors.New("cannot read an indexed field of this case")
			}
			record.Values = append(record.Values, retain(policy.Retention, policy.Fields[i], value, decoded))
		}
		document.Records = append(document.Records, record)
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// retain applies the declared retention form to one selected value. A value
// longer than MaxValueBytes keeps its first MaxValueBytes bytes and says so;
// it is never silently shortened and never silently dropped.
func retain(form Retention, selector string, value hl7.Value, decoded *hl7.Document) Value {
	retained := Value{Selector: selector, State: value.State, Offset: value.Span.Start, Length: value.Span.End - value.Span.Start}
	if value.State != hl7.Present {
		// Only a present value has bytes to retain. The span of an empty or
		// null field is still recorded, so a reader goes to the exact place
		// the case declares one rather than to where it might have been.
		return retained
	}
	raw := decoded.Bytes(value.Span)
	switch form {
	case RetainValues:
		if len(raw) > MaxValueBytes {
			retained.Bytes, retained.Truncated = raw[:MaxValueBytes], true
			return retained
		}
		retained.Bytes = raw
	case RetainDigests:
		retained.Digest = digest(raw)
	}
	return retained
}

// Validate reports the first reason a document cannot be stored or served.
// Diagnostics name the member at fault and never repeat the value that failed.
func Validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if err := ValidatePolicy(document.Policy); err != nil {
		return err
	}
	if len(document.Case.Identity) != digestLength || !hexadecimal(document.Case.Identity) {
		return errors.New("an index names the content identity of the case it describes")
	}
	if document.Case.Schema == "" || document.Case.Provenance == "" {
		return errors.New("an index records the contract version and provenance the case declared")
	}
	if !validTime(document.BuiltAt) {
		return errors.New("an index records the instant it was built")
	}
	if len(document.Case.Sources) > bundle.MaxSources {
		return errors.New("an index describes at most " + strconv.Itoa(bundle.MaxSources) + " sources")
	}
	if len(document.Records) > bundle.MaxEvents {
		return errors.New("an index describes at most " + strconv.Itoa(bundle.MaxEvents) + " occurrences")
	}
	// Sources and occurrences are named the way the case that holds them names
	// them, and in the same order. A case reader reconstructs exactly this
	// layout, so an index whose records do not line up with the sources it
	// declares is describing something other than a case bundle.
	position := 0
	disagrees := errors.New("index records disagree with the sources they name")
	for i, source := range document.Case.Sources {
		if source.ID != fmt.Sprintf("s%04d", i+1) {
			return errors.New("an index names each source of a case the way the case does")
		}
		if source.Occurrences < 1 || source.Occurrences > bundle.MaxEvents || source.Size < 0 ||
			len(source.SHA256) != digestLength || !hexadecimal(source.SHA256) {
			return errors.New("an index records the size, digest and occurrence count of each source")
		}
		for sequence := 1; sequence <= source.Occurrences; sequence++ {
			if position >= len(document.Records) {
				return disagrees
			}
			record := document.Records[position]
			if record.SourceID != source.ID || record.Sequence != sequence || record.ID != fmt.Sprintf("%s-e%06d", source.ID, sequence) {
				return disagrees
			}
			if err := validateRecord(document.Policy, record); err != nil {
				return err
			}
			position++
		}
	}
	if position != len(document.Records) {
		return disagrees
	}
	return nil
}

func validateRecord(policy Policy, record Record) error {
	if record.Offset < 0 || record.Size < 0 {
		return errors.New("an index record names its position in the source that holds it")
	}
	switch record.Kind {
	case bundle.Message, bundle.Acknowledgement, bundle.Unparsed:
	default:
		return errors.New("an index record carries the occurrence kind the case recorded")
	}
	switch record.Direction {
	case bundle.Unknown, bundle.Inbound, bundle.Outbound:
	default:
		return errors.New("an index record carries the direction the case recorded")
	}
	if record.ObservedAt != nil && !validTime(*record.ObservedAt) || record.ImportedAt != nil && !validTime(*record.ImportedAt) {
		return errors.New("an index record carries only valid recorded times")
	}
	// An occurrence the case could not decode is indexed with no fields at all.
	// Indexing it as though every named field were absent from it would report
	// an undecodable occurrence as one that simply does not carry the field.
	if record.ParseError != "" {
		if len(record.Values) != 0 {
			return errors.New("an occurrence the case could not decode carries no indexed fields")
		}
		return nil
	}
	if len(record.Values) != len(policy.Fields) {
		return errors.New("an index record carries one value for each declared field")
	}
	for i, value := range record.Values {
		if value.Selector != policy.Fields[i] {
			return errors.New("an index record lists its values in the order the policy declares them")
		}
		if err := validateValue(policy.Retention, value, record.Size); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(form Retention, value Value, payload int) error {
	switch value.State {
	case hl7.Present, hl7.Empty, hl7.Null, hl7.Omitted:
	default:
		return errors.New("an indexed value carries a decoded state of present, empty, null or omitted")
	}
	// The span is read against the occurrence's own payload, so an index cannot
	// send a reader past the end of the evidence it claims to describe.
	if value.Offset < 0 || value.Length < 0 || value.Offset > payload || value.Length > payload-value.Offset {
		return errors.New("an indexed value names a byte span inside its occurrence")
	}
	if value.State != hl7.Present && (len(value.Bytes) != 0 || value.Digest != "" || value.Truncated) {
		return errors.New("only a present value retains bytes or a digest")
	}
	switch form {
	case RetainStates:
		if len(value.Bytes) != 0 || value.Digest != "" || value.Truncated {
			return errors.New("an index that retains only states carries no value bytes or digests")
		}
	case RetainDigests:
		if len(value.Bytes) != 0 || value.Truncated {
			return errors.New("an index that retains digests carries no value bytes")
		}
		if value.State == hl7.Present && (len(value.Digest) != digestLength || !hexadecimal(value.Digest)) {
			return errors.New("an index that retains digests carries one digest for each present value")
		}
	case RetainValues:
		if value.Digest != "" {
			return errors.New("an index that retains values carries no digests")
		}
		if value.State == hl7.Present {
			if value.Truncated != (value.Length > MaxValueBytes) {
				return errors.New("an index that retains values states whether each value was shortened")
			}
			if len(value.Bytes) != min(value.Length, MaxValueBytes) {
				return errors.New("an index that retains values carries the recorded length of each value")
			}
		}
	}
	return nil
}

// ValidatePolicy reports the first reason a set of retention declarations
// cannot be used.
func ValidatePolicy(policy Policy) error {
	_, err := selectors(policy)
	return err
}

// selectors parses the declared fields once and returns them in the declared
// order. A field that is not already in canonical form is refused rather than
// rewritten, so the selector a record reports is the selector that was asked
// for.
func selectors(policy Policy) ([]hl7.Selector, error) {
	switch policy.Retention {
	case RetainValues, RetainDigests, RetainStates:
	default:
		return nil, errors.New("retention must be declared as values, digests, or states")
	}
	if policy.RetainUntil != nil && !validTime(*policy.RetainUntil) {
		return nil, errors.New("a declared retention end must be a valid instant")
	}
	if len(policy.Fields) == 0 {
		return nil, errors.New("an index declares at least one field to retain")
	}
	if len(policy.Fields) > MaxFields {
		return nil, errors.New("an index declares at most " + strconv.Itoa(MaxFields) + " fields")
	}
	parsed := make([]hl7.Selector, 0, len(policy.Fields))
	for i, field := range policy.Fields {
		selector, err := hl7.ParseSelector(field)
		if err != nil || selector.String() != field {
			return nil, errors.New("an index declares each field as one canonical field selector")
		}
		if slices.Contains(policy.Fields[:i], field) {
			return nil, errors.New("an index declares the same field twice")
		}
		parsed = append(parsed, selector)
	}
	return parsed, nil
}

// Encode writes a validated document deterministically and seals it with the
// digest of everything else in it.
func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	_, data, err := seal(document)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > MaxIndexBytes {
		return nil, errors.New("index exceeds its size limit; declare fewer fields or retain digests or states")
	}
	return data, nil
}

// Decode reads an index document. Unknown members and unknown versions are
// errors, and a document that no longer matches its own digest is reported as
// damaged. There is no migration and no repair: the answer to every one of
// these is to build the index again from the case.
func Decode(data []byte) (Document, error) {
	if len(data) > MaxIndexBytes {
		return Document{}, errors.New("index exceeds its size limit")
	}
	// The declared version is read before the strict decode. A later release
	// bumps the version precisely because it adds members, so deciding
	// strictness first would report such a document as invalid rather than as
	// the version it plainly declares.
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Document{}, errors.New("invalid index document")
	}
	if declared.Schema != Schema {
		return Document{}, ErrUnsupportedVersion
	}
	var document Document
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return Document{}, errors.New("invalid index document")
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	sealed, _, err := seal(document)
	if err != nil {
		return Document{}, err
	}
	if sealed.Digest != document.Digest {
		return Document{}, ErrDamaged
	}
	return document, nil
}

// seal returns the document carrying the digest of its own contents and the
// deterministic encoding of that sealed document, so the writer and the reader
// share one implementation instead of agreeing on one. The digest member is
// cleared before the hashed encoding is taken, so the same document always
// seals to the same value however it was produced.
func seal(document Document) (Document, []byte, error) {
	cannot := errors.New("cannot encode index document")
	document.Digest = ""
	contents, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return Document{}, nil, cannot
	}
	document.Digest = digest(contents)
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return Document{}, nil, cannot
	}
	return document, data, nil
}

// Usable reports whether the retention the index declares still covers the
// given instant. Nothing about the evidence changes when it does not.
func (d Document) Usable(at time.Time) error {
	if d.Policy.RetainUntil != nil && !at.Before(*d.Policy.RetainUntil) {
		return ErrExpired
	}
	return nil
}

// Describes reports whether this index was built from exactly the evidence the
// caller opened. The case bundle's identity is a hash over its own files, so a
// case that gained, lost or changed a byte no longer matches the index of it.
//
// Everything the index restates about the case is checked against the verified
// bundle, not only that identity: the contract version, the provenance mode,
// every source's metadata and every occurrence's position, kind, direction and
// recorded times. The seal detects damage rather than forgery, so restated
// metadata that nothing was compared against would be metadata the reader is
// simply trusting. Only the decoded field values are read from the index alone,
// because reproducing those is what an index exists to avoid.
func (d Document) Describes(opened *bundle.Bundle) error {
	if opened == nil {
		return errors.New("an index is checked against an opened case bundle")
	}
	if d.Case.Identity != opened.Identity || d.Case.Schema != opened.Manifest.Schema ||
		d.Case.Provenance != string(opened.Manifest.Provenance.Mode) ||
		len(d.Case.Sources) != len(opened.Manifest.Sources) || len(d.Records) != len(opened.Events) {
		return ErrStale
	}
	for i, source := range d.Case.Sources {
		declared := opened.Manifest.Sources[i]
		if source.ID != declared.ID || source.Path != declared.Path || source.Format != declared.Format ||
			source.Terminator != declared.Terminator || source.Size != declared.Size ||
			source.SHA256 != declared.SHA256 || source.Occurrences != declared.Occurrences {
			return ErrStale
		}
	}
	for i, record := range d.Records {
		event := opened.Events[i]
		if record.ID != event.ID || record.SourceID != event.SourceID || record.Sequence != event.Sequence ||
			record.Offset != event.Offset || record.Size != event.Payload.Size || record.Kind != event.Kind ||
			record.Direction != event.Direction || record.ParseError != event.ParseError ||
			!sameInstant(record.ObservedAt, event.ObservedAt) || !sameInstant(record.ImportedAt, event.ImportedAt) {
			return ErrStale
		}
	}
	return nil
}

// sameInstant compares two recorded times, either of which may be unknown. An
// unknown time equals only another unknown one; it is never treated as zero.
func sameInstant(recorded, declared *time.Time) bool {
	if recorded == nil || declared == nil {
		return recorded == nil && declared == nil
	}
	return recorded.Equal(*declared)
}

// Match is how a query compares against what the index retained.
type Match string

const (
	// Equals matches a value byte for byte. It is answered from retained bytes
	// or from a digest of the term.
	Equals Match = "equals"
	// Contains matches a value that holds the term. Only retained bytes can
	// answer it.
	Contains Match = "contains"
	// State matches the decoded state of a field, which every index retains.
	State Match = "state"
)

// Query is one question asked of an index. An empty Field asks it of every
// declared field.
type Query struct {
	Field string
	Match Match
	Term  []byte
	State hl7.State
}

// Hit is one indexed value that answered the query, with the occurrence it
// belongs to so a reader can go to the original bytes.
type Hit struct {
	Record Record
	Value  Value
}

// Result carries what the index could and could not answer. Undecided counts
// present values whose retained prefix was shortened and therefore cannot
// settle the query either way; Undecodable counts occurrences the case itself
// could not decode and which carry no indexed fields. Neither is a miss, and
// neither is silently dropped.
type Result struct {
	Hits        []Hit
	Undecided   int
	Undecodable int
}

// Search answers one query from what was retained, and refuses a question this
// index cannot answer rather than answering a narrower one. Retention is
// enforced here rather than beside it, so an index past its declared end
// answers nobody — not the command line, and not a facade calling this package
// directly. The instant is supplied by the caller the way every other term
// decision in readmit is; it reads only the index, and the caller checks the
// index against the case first.
func (d Document) Search(at time.Time, query Query) (Result, error) {
	if err := d.Usable(at); err != nil {
		return Result{}, err
	}
	if query.Field != "" && !slices.Contains(d.Policy.Fields, query.Field) {
		return Result{}, errors.New("that field is not retained by this index; only a field it declares can be asked for")
	}
	switch query.Match {
	case State:
		switch query.State {
		case hl7.Present, hl7.Empty, hl7.Null, hl7.Omitted:
		default:
			return Result{}, errors.New("a state query names present, empty, null, or omitted")
		}
	case Equals, Contains:
		if len(query.Term) == 0 {
			return Result{}, errors.New("a value query needs something to look for")
		}
		if d.Policy.Retention == RetainStates {
			return Result{}, errors.New("this index retains no values; query by state, or rebuild it retaining values or digests")
		}
		if query.Match == Contains && d.Policy.Retention == RetainDigests {
			return Result{}, errors.New("an index of digests answers exact matches only; query with equals or by state, or rebuild it retaining values")
		}
	default:
		return Result{}, errors.New("a query is equals, contains, or state")
	}
	result := Result{Hits: []Hit{}}
	term := digest(query.Term)
	for _, record := range d.Records {
		if record.ParseError != "" {
			result.Undecodable++
			continue
		}
		for _, value := range record.Values {
			if query.Field != "" && value.Selector != query.Field {
				continue
			}
			switch matches(d.Policy.Retention, query, value, term) {
			case hit:
				result.Hits = append(result.Hits, Hit{Record: record, Value: value})
			case undecided:
				result.Undecided++
			}
		}
	}
	return result, nil
}

type outcome int

const (
	miss outcome = iota
	hit
	undecided
)

// matches decides one retained value against one query. A shortened value can
// answer a substring query it satisfies within the bytes that were kept, and is
// undecided about one it does not: the prefix that is there is real, and what
// was cut is unknown rather than absent. What the index records of the whole
// value is still decisive where it can be — a value of a different length is
// not the term, whether or not its bytes were kept.
func matches(form Retention, query Query, value Value, term string) outcome {
	if query.Match == State {
		if value.State == query.State {
			return hit
		}
		return miss
	}
	if value.State != hl7.Present {
		return miss
	}
	if form == RetainDigests {
		if value.Digest == term {
			return hit
		}
		return miss
	}
	// Length is the length of the whole value, not of the retained prefix, so
	// a term that cannot fit is a miss however much of the value was kept.
	if value.Length < len(query.Term) || query.Match == Equals && value.Length != len(query.Term) {
		return miss
	}
	if query.Match == Contains {
		if bytes.Contains(value.Bytes, query.Term) {
			return hit
		}
		if value.Truncated {
			return undecided
		}
		return miss
	}
	if value.Truncated {
		return undecided
	}
	if bytes.Equal(value.Bytes, query.Term) {
		return hit
	}
	return miss
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// hexadecimal accepts the lowercase form every readmit digest is written in,
// so one digest cannot be recorded under two different spellings.
func hexadecimal(value string) bool {
	for i := range len(value) {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
