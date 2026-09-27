package grid

import (
	"bytes"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
)

// The bounds of one query beyond the ones a saved filter already has.
const (
	// MaxTypes bounds how many message types one query names on each of its
	// type axes.
	MaxTypes = 64
	// MaxTypeBytes bounds the escaped message code or trigger event one type
	// names.
	MaxTypeBytes = 64
	// MaxSearchBytes bounds one literal search.
	MaxSearchBytes = 200
)

// MessageType is what the message list shows in its Type column. A Kind of
// message names the exact parsed MSH-9 message code and trigger event, in the
// escaped form the list shows them, with an empty code and trigger naming a
// message that declared no type. A Kind of ack or unparsed names that whole
// occurrence kind and carries no code or trigger: ACK and Unparsed are the
// types a person chooses, not every acknowledgement code a case happens to
// carry.
type MessageType struct {
	Kind    bundle.EventKind `json:"kind"`
	Code    string           `json:"code"`
	Trigger string           `json:"trigger"`
}

// SearchScope is where a literal search looks.
type SearchScope string

const (
	// SearchMetadata looks at what the list already shows or holds about an
	// occurrence: its message type, source ID and declared source name, and
	// MSH-10 control ID.
	SearchMetadata SearchScope = "metadata"
	// SearchContent looks at the original bytes of each occurrence of the
	// selected case.
	SearchContent SearchScope = "content"
)

// TextSearch is one literal search. Text is matched byte for byte: there is no
// pattern language, no case folding and no tokenizing.
type TextSearch struct {
	Scope SearchScope `json:"scope"`
	Text  string      `json:"text"`
}

// Scope narrows a query to the rows its own counts describe, so a count of
// what it could not settle or decode leads to exactly those rows.
type Scope string

const (
	// AllRows is every row the query answers.
	AllRows Scope = ""
	// UndecidedRows are the rows the query's field questions could neither keep
	// nor exclude, because a value the index kept was shortened: no question
	// excluded them, and at least one could not settle them.
	UndecidedRows Scope = "undecided"
	// UndecodableRows are the occurrences the case could not decode, which no
	// field question can answer; the query's other rules still apply to them.
	UndecodableRows Scope = "undecodable"
)

// Query is one transient question about one case. Every axis is a list or a
// bound, and an empty one constrains nothing. Within an axis the values are
// alternatives; across axes an occurrence has to satisfy all of them. A
// negated axis (NotTypes, NotSources) excludes every occurrence it names.
//
// A Query is what the filter sheet applies and what a saved view stores. It
// is never written anywhere by applying it.
type Query struct {
	Kinds         []bundle.EventKind `json:"kinds"`
	Types         []MessageType      `json:"types"`
	NotTypes      []MessageType      `json:"not_types"`
	Sources       []string           `json:"sources"`
	NotSources    []string           `json:"not_sources"`
	Directions    []bundle.Direction `json:"directions"`
	ObservedFrom  *time.Time         `json:"observed_from"`
	ObservedUntil *time.Time         `json:"observed_until"`
	AckCodes      []string           `json:"ack_codes"`
	Fields        []FieldPredicate   `json:"fields"`
	Search        *TextSearch        `json:"search"`
	Scope         Scope              `json:"scope,omitzero"`
}

// Query is the saved filter as a transient query, so a v1 filter narrows a
// case exactly as it did when it was saved.
func (f Filter) Query() Query {
	return Query{
		Kinds: f.Kinds, Sources: f.Sources, ObservedFrom: f.ObservedFrom, ObservedUntil: f.ObservedUntil,
		AckCodes: f.AckCodes, Fields: f.Fields,
	}
}

// IndexedFields are the canonical selectors an index has to retain to answer
// this query's value and state questions, in the order they are asked. An
// empty list means the query asks the index nothing.
func (q Query) IndexedFields() []string {
	fields := []string{}
	for _, predicate := range q.Fields {
		if !slices.Contains(fields, predicate.Selector) {
			fields = append(fields, predicate.Selector)
		}
	}
	if len(q.AckCodes) > 0 && !slices.Contains(fields, AckCodeSelector) {
		fields = append(fields, AckCodeSelector)
	}
	return fields
}

// Answerable reports whether an index built under this policy can answer every
// value and state question this query asks. The index still decides for
// itself when asked; this is how a caller chooses which index to ask.
func (q Query) Answerable(policy index.Policy) bool {
	for _, field := range q.IndexedFields() {
		if !slices.Contains(policy.Fields, field) {
			return false
		}
	}
	if len(q.AckCodes) > 0 && policy.Retention == index.RetainStates {
		return false
	}
	for _, predicate := range q.Fields {
		switch {
		case predicate.Match == index.State:
		case policy.Retention == index.RetainStates:
			return false
		case predicate.Match == index.Contains && policy.Retention == index.RetainDigests:
			return false
		}
	}
	return true
}

// ValidateQuery reports the first reason a query cannot be applied, naming the
// axis at fault and never repeating a value. A combination this release cannot
// answer is refused with its reason rather than executed as another question.
func ValidateQuery(query Query) error {
	if err := validatePredicates(Filter{
		Kinds: query.Kinds, Sources: query.Sources, ObservedFrom: query.ObservedFrom,
		ObservedUntil: query.ObservedUntil, AckCodes: query.AckCodes, Fields: query.Fields,
	}); err != nil {
		return err
	}
	if len(query.IndexedFields()) > index.MaxFields {
		return errors.New("a query asks about at most " + strconv.Itoa(index.MaxFields) + " fields, the acknowledgement code included")
	}
	for _, axis := range [][]MessageType{query.Types, query.NotTypes} {
		if len(axis) > MaxTypes {
			return errors.New("a query names at most " + strconv.Itoa(MaxTypes) + " message types on each type rule")
		}
		for i, declared := range axis {
			if err := validateType(declared); err != nil {
				return err
			}
			if slices.Contains(axis[:i], declared) {
				return errors.New("a type rule names each message type at most once")
			}
		}
	}
	for _, declared := range query.Types {
		if slices.Contains(query.NotTypes, declared) {
			return errors.New("a message type cannot be both required and excluded")
		}
	}
	if len(query.NotSources) > MaxSources {
		return errors.New("a query excludes at most " + strconv.Itoa(MaxSources) + " sources")
	}
	for i, source := range query.NotSources {
		if !printable(source, MaxNameBytes) {
			return errors.New("a query names each source the way the case names it")
		}
		if slices.Contains(query.NotSources[:i], source) {
			return errors.New("a query excludes each source at most once")
		}
		if slices.Contains(query.Sources, source) {
			return errors.New("a source cannot be both required and excluded")
		}
	}
	if len(query.Directions) > 3 {
		return errors.New("a query names each direction at most once")
	}
	for i, direction := range query.Directions {
		switch direction {
		case bundle.Inbound, bundle.Outbound, bundle.Unknown:
		default:
			return errors.New("a direction is inbound, outbound, or unknown")
		}
		if slices.Contains(query.Directions[:i], direction) {
			return errors.New("a query names each direction at most once")
		}
	}
	switch query.Scope {
	case AllRows, UndecodableRows:
	case UndecidedRows:
		if len(query.IndexedFields()) == 0 {
			return errors.New("only a query that asks about a field has rows it could not settle")
		}
	default:
		return errors.New("a query lists all its rows, the rows it could not settle, or the rows that could not be decoded")
	}
	if query.Search != nil {
		switch query.Search.Scope {
		case SearchMetadata, SearchContent:
		default:
			return errors.New("a search looks at metadata or message content")
		}
		if !printable(query.Search.Text, MaxSearchBytes) {
			return errors.New("a search looks for bounded printable text")
		}
	}
	return nil
}

func validateType(declared MessageType) error {
	switch declared.Kind {
	case bundle.Message:
		if declared.Code != "" && !printable(declared.Code, MaxTypeBytes) ||
			declared.Trigger != "" && !printable(declared.Trigger, MaxTypeBytes) {
			return errors.New("a message type names the escaped code and trigger the list shows")
		}
		if declared.Code == "" && declared.Trigger != "" {
			return errors.New("a message type with a trigger event names its message code")
		}
	case bundle.Acknowledgement, bundle.Unparsed:
		if declared.Code != "" || declared.Trigger != "" {
			return errors.New("ACK and Unparsed are whole occurrence kinds and name no code or trigger")
		}
	default:
		return errors.New("a message type is a message, ack, or unparsed occurrence")
	}
	return nil
}

// Facts supplies what an index record does not carry: the parsed message type
// the list shows, the MSH-10 control ID, the name declared for the record's
// source (empty when nothing names it), and the original occurrence bytes. It
// is read from the verified case the index was checked against. A query that
// asks nothing of them never calls it, so a nil Facts is valid for one.
type Facts interface {
	Type(record index.Record) MessageType
	ControlID(record index.Record) []byte
	SourceName(record index.Record) string
	Content(record index.Record) []byte
}

// Order is the order a page lists the occurrences it kept in.
type Order string

const (
	// EvidenceOrder is the order the case holds its occurrences in: source by
	// source, each in the sequence it was captured or imported.
	EvidenceOrder Order = ""
	// TimeAscending and TimeDescending order by recorded observed time. An
	// occurrence with no recorded time is listed after every one with one, in
	// evidence order, in both directions: unknown is not early or late.
	TimeAscending  Order = "time-ascending"
	TimeDescending Order = "time-descending"
)

// SelectQuery applies one query to one index and returns the requested window
// in the requested order. It is Select for a transient query: the value and
// state questions are asked of the index through Search, so retention is the
// index's own decision, and the axes the index records carry are read from
// them. Type and search axes are read through facts.
func SelectQuery(document index.Document, at time.Time, query Query, facts Facts, order Order, window Window) (Page, error) {
	if window.Offset < 0 {
		return Page{}, errors.New("a window begins at or after the first matching occurrence")
	}
	if window.Limit < 1 || window.Limit > MaxRows {
		return Page{}, errors.New("a window renders between 1 and " + strconv.Itoa(MaxRows) + " occurrences")
	}
	switch order {
	case EvidenceOrder, TimeAscending, TimeDescending:
	default:
		return Page{}, errors.New("messages are listed in evidence order or by observed time")
	}
	if err := ValidateQuery(query); err != nil {
		return Page{}, err
	}
	if facts == nil && (len(query.Types) > 0 || len(query.NotTypes) > 0 || query.Search != nil) {
		return Page{}, errors.New("a type or search rule is answered from the case the index describes")
	}
	return selectQuery(document, at, query, facts, order, window)
}

func selectQuery(document index.Document, at time.Time, query Query, facts Facts, order Order, window Window) (Page, error) {
	if err := document.Usable(at); err != nil {
		return Page{}, err
	}
	page := Page{Rows: []index.Record{}, Offset: window.Offset, Limit: window.Limit, Total: len(document.Records)}
	filter := Filter{AckCodes: query.AckCodes, Fields: query.Fields}
	kept, open, asked, err := answered(document, at, &filter, &page)
	if err != nil {
		return Page{}, err
	}
	matched := []index.Record{}
	for _, record := range document.Records {
		if record.ParseError != "" {
			page.Undecodable++
		}
		if !keeps(query, facts, record) {
			continue
		}
		switch query.Scope {
		case UndecidedRows:
			if !open[record.ID] || kept[record.ID] {
				continue
			}
		case UndecodableRows:
			if record.ParseError == "" {
				continue
			}
		default:
			if asked && !kept[record.ID] {
				continue
			}
		}
		page.Matched++
		if order != EvidenceOrder {
			matched = append(matched, record)
		} else if page.Matched > window.Offset && len(page.Rows) < window.Limit {
			page.Rows = append(page.Rows, record)
		}
	}
	if order != EvidenceOrder {
		sortByTime(matched, order)
		if window.Offset < len(matched) {
			page.Rows = matched[window.Offset:min(len(matched), window.Offset+window.Limit)]
		}
	}
	page.Excluded = page.Total - page.Matched
	return page, nil
}

// sortByTime orders by recorded observed time and keeps evidence order among
// equal times. Unknown times come last in both directions.
func sortByTime(records []index.Record, order Order) {
	slices.SortStableFunc(records, func(a, b index.Record) int {
		switch {
		case a.ObservedAt == nil && b.ObservedAt == nil:
			return 0
		case a.ObservedAt == nil:
			return 1
		case b.ObservedAt == nil:
			return -1
		}
		compared := a.ObservedAt.Compare(*b.ObservedAt)
		if order == TimeDescending {
			return -compared
		}
		return compared
	})
}

// keeps reports whether one record satisfies every axis not asked of the index.
func keeps(query Query, facts Facts, record index.Record) bool {
	if !narrows(&Filter{Kinds: query.Kinds, Sources: query.Sources, ObservedFrom: query.ObservedFrom, ObservedUntil: query.ObservedUntil}, record) {
		return false
	}
	if slices.Contains(query.NotSources, record.SourceID) {
		return false
	}
	if len(query.Directions) > 0 && !slices.Contains(query.Directions, record.Direction) {
		return false
	}
	if len(query.Types) > 0 || len(query.NotTypes) > 0 {
		declared := typeOf(facts, record)
		if len(query.Types) > 0 && !slices.Contains(query.Types, declared) || slices.Contains(query.NotTypes, declared) {
			return false
		}
	}
	if query.Search == nil {
		return true
	}
	text := []byte(query.Search.Text)
	if query.Search.Scope == SearchContent {
		return bytes.Contains(facts.Content(record), text)
	}
	declared := facts.Type(record)
	return strings.Contains(declared.Code, query.Search.Text) || strings.Contains(declared.Trigger, query.Search.Text) ||
		declared.Code != "" && strings.Contains(declared.Code+"^"+declared.Trigger, query.Search.Text) ||
		strings.Contains(record.SourceID, query.Search.Text) || strings.Contains(facts.SourceName(record), query.Search.Text) ||
		bytes.Contains(facts.ControlID(record), text)
}

// typeOf is the type a type rule compares: the whole kind for an
// acknowledgement or an unparsed occurrence, the parsed code and trigger for a
// message.
func typeOf(facts Facts, record index.Record) MessageType {
	if record.Kind != bundle.Message {
		return MessageType{Kind: record.Kind}
	}
	declared := facts.Type(record)
	declared.Kind = bundle.Message
	return declared
}
