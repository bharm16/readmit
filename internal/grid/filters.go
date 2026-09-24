// Package grid answers "which occurrences of this case am I looking at right
// now", and — just as importantly — how many it left out.
//
// A filtered view that quietly drops records is the interface equivalent of a
// false negative, so every answer here carries the number of occurrences the
// filter removed, the number of retained values it could not settle, and the
// number of occurrences the case itself could not decode. A caller can always
// tell "this case holds nothing like that" from "I am not showing you this".
//
// Nothing here reads evidence. A filter is applied to the derived index of
// ADR-0008, which is the one search path over a case: value and state questions
// are asked through index.Document.Search, so the retention an operator
// declared is enforced by the index itself rather than beside it, and a field
// the index does not retain is refused by name instead of answered from
// something narrower. Occurrence type, source and recorded time are read from
// the index records, which the caller has already checked against the verified
// bundle through Describes.
//
// A saved filter is per-viewer working state, not evidence: it is one bounded,
// versioned readmit-filters/v1 document (ADR-0003) that no case, run, result,
// review or report ever holds. It does hold what a person typed to filter by,
// and a value typed to match an HL7 field is the same patient data it matches;
// this package treats it as such and never renders one.
package grid

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

const (
	// Schema is the only saved-filter contract this release reads. A new member
	// means a new version string and a reader for both, never an added member
	// here and never an in-place migration.
	Schema = "readmit-filters/v1"

	// AckCodeSelector is the canonical selector of the acknowledgement code an
	// ACK outcome is read from. It is fixed rather than configurable: MSA-1 is
	// where HL7 puts the outcome, in both the original and the enhanced
	// acknowledgement modes internal/collection implements.
	AckCodeSelector = "MSA[1]-1[1]"
)

// The bounds of one saved-filter document. Past any of them the document is
// refused, never truncated: a filter that quietly dropped a predicate would
// hide records without saying so, which is the failure this package exists to
// prevent.
const (
	MaxFilters       = 32
	MaxNameBytes     = 64
	MaxSources       = bundle.MaxSources
	MaxAckCodes      = 8
	MaxAckCodeBytes  = 16
	MaxPredicates    = index.MaxFields
	MaxTermBytes     = index.MaxValueBytes
	MaxDocumentBytes = 1 << 18

	// MaxRows bounds one rendered window. A grid over a large case renders a
	// window of it, never all of it.
	MaxRows = 200
)

// ErrUnsupportedVersion reports a saved-filter document written under a
// contract version this release does not read. It is distinct from a document
// this release reads and rejects, so a caller can say which one it was handed.
var ErrUnsupportedVersion = errors.New("unsupported saved filter document version")

// FieldPredicate is one question about one indexed field. Match is the index's
// own vocabulary, so a predicate asks exactly what an index can answer: Equals
// and Contains compare Term against the retained value, and State compares the
// decoded state, which every retention form keeps. A field the index does not
// declare is refused by name when the filter is applied.
//
// Term is text a person typed, so it is a string rather than the raw bytes an
// index retains: the command line takes a search term the same way.
type FieldPredicate struct {
	Selector string      `json:"selector"`
	Match    index.Match `json:"match"`
	Term     string      `json:"term"`
	State    hl7.State   `json:"state"`
}

// UnmarshalJSON requires every member explicitly and then re-decodes rejecting
// unknown members, so an omitted member cannot decode into a permissive zero
// value and a misspelled one is an error rather than one that quietly did
// nothing. Both passes are needed: a custom unmarshaler does not inherit the
// caller's strictness.
func (p *FieldPredicate) UnmarshalJSON(data []byte) error {
	var members struct {
		Selector jsontext.Value `json:"selector"`
		Match    jsontext.Value `json:"match"`
		Term     jsontext.Value `json:"term"`
		State    jsontext.Value `json:"state"`
	}
	if err := json.Unmarshal(data, &members, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid field predicate")
	}
	if len(members.Selector) == 0 || len(members.Match) == 0 || len(members.Term) == 0 || len(members.State) == 0 {
		return errors.New("a field predicate states its selector, its match, its term and its state")
	}
	type plainPredicate FieldPredicate
	var value plainPredicate
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid field predicate")
	}
	*p = FieldPredicate(value)
	return nil
}

// Filter is one saved set of questions. Every axis is a list or a bound, and an
// empty one constrains nothing: a filter narrows only along the axes a person
// declared. Within an axis the values are alternatives; across axes an
// occurrence has to satisfy all of them.
//
// The six axes are the ones an investigation asks for: the occurrence type, the
// source it arrived on, when it was observed, the decoded state of a field, the
// acknowledgement outcome, and the value of a field.
type Filter struct {
	Name  string             `json:"name"`
	Kinds []bundle.EventKind `json:"kinds"`
	// Sources are source IDs as the case names them. A source this case does
	// not hold matches nothing; a saved filter outlives the case it was made
	// for and is not refused for naming one.
	Sources []string `json:"sources"`
	// ObservedFrom and ObservedUntil bound the observed time as a half-open
	// interval. An occurrence the case recorded no observed time for cannot
	// satisfy either bound: unknown is not a pass.
	ObservedFrom  *time.Time `json:"observed_from"`
	ObservedUntil *time.Time `json:"observed_until"`
	// AckCodes are literal MSA-1 acknowledgement codes. They are answered from
	// AckCodeSelector, so a filter asking for an outcome is refused by name
	// unless the index retains that field.
	AckCodes []string         `json:"ack_codes"`
	Fields   []FieldPredicate `json:"fields"`
}

// UnmarshalJSON requires every member explicitly and then re-decodes rejecting
// unknown members, for the reason FieldPredicate.UnmarshalJSON gives. A valid
// document states an absent time bound as an explicit null, and a typed pointer
// cannot tell that from an absent member, so presence is read from the raw
// member the way every other presence check in readmit reads it.
func (f *Filter) UnmarshalJSON(data []byte) error {
	var members struct {
		Name          jsontext.Value `json:"name"`
		Kinds         jsontext.Value `json:"kinds"`
		Sources       jsontext.Value `json:"sources"`
		ObservedFrom  jsontext.Value `json:"observed_from"`
		ObservedUntil jsontext.Value `json:"observed_until"`
		AckCodes      jsontext.Value `json:"ack_codes"`
		Fields        jsontext.Value `json:"fields"`
	}
	if err := json.Unmarshal(data, &members, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid saved filter")
	}
	for _, member := range []jsontext.Value{
		members.Name, members.Kinds, members.Sources, members.ObservedFrom,
		members.ObservedUntil, members.AckCodes, members.Fields,
	} {
		if len(member) == 0 {
			return errors.New("a saved filter states its name, every axis it narrows, and an explicit null for a time bound it does not declare")
		}
	}
	type plainFilter Filter
	var value plainFilter
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid saved filter")
	}
	*f = Filter(value)
	return nil
}

// Document is the complete saved-filter state of one viewer: the filters a
// person saved and the one that is selected now. Selected is what survives
// navigating from one case to another, so the view a person set up is still the
// view they get; it is a name, and an empty one selects no filter at all.
//
// It carries no digest. It is not evidence and nothing is derived from it: a
// document this release cannot read is reported and left exactly as written,
// never migrated, repaired or replaced.
type Document struct {
	Schema   string   `json:"schema"`
	Filters  []Filter `json:"filters"`
	Selected string   `json:"selected"`
}

// Empty is the state of a viewer who has saved nothing. It is what a missing
// document means, and it is a complete, valid document rather than a zero value.
func Empty() Document {
	return Document{Schema: Schema, Filters: []Filter{}, Selected: ""}
}

// Find returns the saved filter of that name. A nil filter selects nothing, and
// Select applies no narrowing at all to one.
func (d Document) Find(name string) *Filter {
	for i := range d.Filters {
		if d.Filters[i].Name == name {
			return &d.Filters[i]
		}
	}
	return nil
}

// Validate reports the first reason a document cannot be stored or used.
// Diagnostics name the declaration at fault and never repeat a term.
func Validate(document Document) error {
	if document.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if len(document.Filters) > MaxFilters {
		return errors.New("a viewer saves at most " + strconv.Itoa(MaxFilters) + " filters")
	}
	for i, filter := range document.Filters {
		if err := ValidateFilter(filter); err != nil {
			return err
		}
		for _, earlier := range document.Filters[:i] {
			if earlier.Name == filter.Name {
				return errors.New("two saved filters share one name")
			}
		}
	}
	if document.Selected != "" && document.Find(document.Selected) == nil {
		return errors.New("the selected filter is not one this document saves")
	}
	return nil
}

// ValidateFilter reports the first reason one saved filter cannot be stored or
// applied. It never inspects an index: whether a field is retained is the
// index's decision, made when the filter is applied to one.
func ValidateFilter(filter Filter) error {
	if !printable(filter.Name, MaxNameBytes) {
		return errors.New("a saved filter is named with bounded printable text")
	}
	return validatePredicates(filter)
}

func validatePredicates(filter Filter) error {
	if len(filter.Kinds) > 3 {
		return errors.New("a filter names each occurrence type at most once")
	}
	for i, kind := range filter.Kinds {
		switch kind {
		case bundle.Message, bundle.Acknowledgement, bundle.Unparsed:
		default:
			return errors.New("an occurrence type is message, ack, or unparsed")
		}
		if slices.Contains(filter.Kinds[:i], kind) {
			return errors.New("a filter names each occurrence type at most once")
		}
	}
	if len(filter.Sources) > MaxSources {
		return errors.New("a filter names at most " + strconv.Itoa(MaxSources) + " sources")
	}
	for i, source := range filter.Sources {
		if !printable(source, MaxNameBytes) {
			return errors.New("a filter names each source the way the case names it")
		}
		if slices.Contains(filter.Sources[:i], source) {
			return errors.New("a filter names each source at most once")
		}
	}
	if filter.ObservedFrom != nil && !validTime(*filter.ObservedFrom) ||
		filter.ObservedUntil != nil && !validTime(*filter.ObservedUntil) {
		return errors.New("a declared time bound must be a valid instant")
	}
	if filter.ObservedFrom != nil && filter.ObservedUntil != nil && !filter.ObservedFrom.Before(*filter.ObservedUntil) {
		return errors.New("a time filter begins before it ends")
	}
	if len(filter.AckCodes) > MaxAckCodes {
		return errors.New("a filter names at most " + strconv.Itoa(MaxAckCodes) + " acknowledgement outcomes")
	}
	for i, code := range filter.AckCodes {
		if !printable(code, MaxAckCodeBytes) {
			return errors.New("an acknowledgement outcome is the bounded printable code the message carries")
		}
		if slices.Contains(filter.AckCodes[:i], code) {
			return errors.New("a filter names each acknowledgement outcome at most once")
		}
	}
	if len(filter.Fields) > MaxPredicates {
		return errors.New("a filter asks at most " + strconv.Itoa(MaxPredicates) + " questions of indexed fields")
	}
	for _, predicate := range filter.Fields {
		if err := validatePredicate(predicate); err != nil {
			return err
		}
	}
	return nil
}

func validatePredicate(predicate FieldPredicate) error {
	selector, err := hl7.ParseSelector(predicate.Selector)
	if err != nil || selector.String() != predicate.Selector {
		return errors.New("a field predicate names one canonical field selector")
	}
	switch predicate.Match {
	case index.Equals, index.Contains:
		if !printable(predicate.Term, MaxTermBytes) {
			return errors.New("a value predicate looks for bounded printable text")
		}
		if predicate.State != hl7.NoState {
			return errors.New("a value predicate compares a value, not a decoded state")
		}
	case index.State:
		switch predicate.State {
		case hl7.Present, hl7.Empty, hl7.Null, hl7.Omitted:
		default:
			return errors.New("a state predicate names present, empty, null, or omitted")
		}
		if predicate.Term != "" {
			return errors.New("a state predicate compares a decoded state, not a value")
		}
	default:
		return errors.New("a field predicate matches with equals, contains, or state")
	}
	return nil
}

// Encode writes a validated document deterministically.
func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode saved filter document")
	}
	data = append(data, '\n')
	if len(data) > MaxDocumentBytes {
		return nil, errors.New("saved filters exceed their size limit; save fewer filters")
	}
	return data, nil
}

// Decode reads a saved-filter document. Unknown members and unknown versions
// are errors: there is no migration and no repair, and the remedy is the
// person's, because nothing here was derived from evidence.
func Decode(data []byte) (Document, error) {
	if len(data) > MaxDocumentBytes {
		return Document{}, errors.New("saved filters exceed their size limit")
	}
	// The declared version is read before the strict decode. A later release
	// bumps the version precisely because it adds members, so deciding
	// strictness first would report such a document as invalid rather than as
	// the version it plainly declares.
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Document{}, errors.New("invalid saved filter document")
	}
	if declared.Schema != Schema {
		return Document{}, ErrUnsupportedVersion
	}
	// Presence is read before the typed decode, for the reason every nested
	// decoder here reads it: an omitted member would otherwise decode into a
	// permissive zero value, and a document that saves no filter is a different
	// statement from one that forgot to say.
	var members struct {
		Filters  jsontext.Value `json:"filters"`
		Selected jsontext.Value `json:"selected"`
	}
	if err := json.Unmarshal(data, &members); err != nil {
		return Document{}, errors.New("invalid saved filter document")
	}
	if len(members.Filters) == 0 || len(members.Selected) == 0 {
		return Document{}, errors.New("a saved filter document states its filters and the one selected")
	}
	var document Document
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil {
		return Document{}, errors.New("invalid saved filter document")
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

// printable accepts bounded text a person typed. Control characters are refused
// rather than escaped, so nothing stored here can rewrite a terminal or a
// rendered line when a later release displays a filter name back.
func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
