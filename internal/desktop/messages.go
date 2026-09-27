package desktop

import (
	"context"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

// messageTypeBytes bounds the MSH-9 bytes one row reports for its message code
// and for its trigger event. A longer declaration is shown in part and ends
// with an ellipsis, so one malformed header cannot carry a message into the
// list.
const messageTypeBytes = 32

// messageTypeField is the field an in-memory index is built on when a query
// asks the index nothing: the reader still reads every occurrence through one
// index document, and this retains only a decoded state, in memory.
const messageTypeField = "MSH[1]-9[1]"

// MessagesRequest reads one window of one case's messages under one transient
// query. Identity binds the read to the case the window displayed. Sort is
// empty for evidence order, or time-ascending or time-descending, which list
// occurrences with no recorded time last. A zero Limit is the facade's bound.
type MessagesRequest struct {
	Workspace string     `json:"workspace"`
	Case      string     `json:"case"`
	Identity  string     `json:"identity"`
	Query     grid.Query `json:"query"`
	Sort      grid.Order `json:"sort"`
	Offset    int        `json:"offset"`
	Limit     int        `json:"limit"`
}

// MessageRow is one occurrence in the message list: where it is, what it is
// and when it was observed. MessageCode and TriggerEvent are the parsed MSH-9
// components, escaped and bounded, and are empty where the occurrence declares
// none or could not be decoded; nothing is invented in their place. ObservedAt
// is present only where the case recorded one. No field value is here.
type MessageRow struct {
	ID           string           `json:"id"`
	SourceID     string           `json:"source_id"`
	Sequence     int              `json:"sequence"`
	Offset       int              `json:"offset"`
	Size         int              `json:"size"`
	Kind         bundle.EventKind `json:"kind"`
	Direction    bundle.Direction `json:"direction"`
	ObservedAt   *time.Time       `json:"observed_at"`
	Decoded      bool             `json:"decoded"`
	MessageCode  string           `json:"message_code"`
	TriggerEvent string           `json:"trigger_event"`
}

// MessageFacets are the choices the filter sheet offers for this case: the
// message types it actually holds, in the order they first occur, its source
// IDs in the order the case declares them, and the acknowledgement codes its
// acknowledgements carry.
type MessageFacets struct {
	Types    []grid.MessageType `json:"types"`
	Sources  []string           `json:"sources"`
	AckCodes []string           `json:"ack_codes"`
}

// MessagesResult is one window of a case's messages and what is not in it.
//
// Completed carries the window, including a window of no rows when the query
// matched nothing: Total is then nonzero and Matched zero, which is a filtered
// view, not an empty case. Empty is a case that holds no occurrence at all.
// Total counts every occurrence, Matched those the query kept; Undecided and
// Undecodable are what grid.Page says they are. Complete says every occurrence
// was examined, and Scanned how many were: a case is bounded to
// bundle.MaxEvents occurrences held in memory by the verified reader, so no
// bound of this reader stops a scan short of it.
type MessagesResult struct {
	State       State         `json:"state"`
	Reason      string        `json:"reason,omitzero"`
	Rows        []MessageRow  `json:"rows"`
	Total       int           `json:"total"`
	Matched     int           `json:"matched"`
	Undecided   int           `json:"undecided"`
	Undecodable int           `json:"undecodable"`
	Complete    bool          `json:"complete"`
	Scanned     int           `json:"scanned"`
	Facets      MessageFacets `json:"facets"`
}

func (r *MessagesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func refusedMessages(state State, reason string) MessagesResult {
	return MessagesResult{State: state, Reason: reason, Rows: []MessageRow{},
		Facets: MessageFacets{Types: []grid.MessageType{}, Sources: []string{}, AckCodes: []string{}}}
}

// ReadMessages reads one window of one verified case under a transient query.
//
// The case is re-verified against the identity the window displayed. Value
// and state questions are asked through index.Document.Search, the one search
// path of ADR-0008: an index of this exact case beside it is reused when its
// retention covers now and it retains what the query asks, and otherwise an
// index is built in memory for this one read and discarded. Nothing is
// written: a query is never stored, no index is created, replaced or extended,
// and an index whose declared retention has ended is passed over rather than
// read. It holds the operation slot but is not interruptible.
func (a *App) ReadMessages(request MessagesRequest) MessagesResult {
	return run(a, false, false, func(ctx context.Context) MessagesResult {
		return readMessages(ctx, request)
	})
}

func readMessages(ctx context.Context, request MessagesRequest) MessagesResult {
	limit := request.Limit
	if limit == 0 {
		limit = grid.MaxRows
	}
	if request.Offset < 0 || limit < 1 || limit > grid.MaxRows {
		return refusedMessages(Failed, "a message window begins at or after the first message and lists at most "+strconv.Itoa(grid.MaxRows)+" of them")
	}
	switch request.Sort {
	case grid.EvidenceOrder, grid.TimeAscending, grid.TimeDescending:
	default:
		return refusedMessages(Failed, "messages are listed in evidence order or by observed time")
	}
	if err := grid.ValidateQuery(request.Query); err != nil {
		return refusedMessages(Failed, err.Error())
	}
	root, opened, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return refusedMessages(declined.state, declined.reason)
	}
	at := time.Now().UTC()
	facts := readFacts(opened)
	document, err := queryIndex(ctx, root, opened, request.Query, at)
	if err != nil {
		return refusedMessages(Failed, err.Error())
	}
	page, err := grid.SelectQuery(document, at, request.Query, facts, request.Sort, grid.Window{Offset: request.Offset, Limit: limit})
	if err != nil {
		return refusedMessages(Failed, refusedQuery(err))
	}
	result := MessagesResult{
		State: Completed, Rows: make([]MessageRow, 0, len(page.Rows)),
		Total: page.Total, Matched: page.Matched, Undecided: page.Undecided, Undecodable: page.Undecodable,
		Complete: true, Scanned: page.Total, Facets: facts.facets(opened),
	}
	for _, record := range page.Rows {
		declared := facts.types[record.ID]
		result.Rows = append(result.Rows, MessageRow{
			ID: record.ID, SourceID: record.SourceID, Sequence: record.Sequence, Offset: record.Offset, Size: record.Size,
			Kind: record.Kind, Direction: record.Direction, ObservedAt: record.ObservedAt, Decoded: record.ParseError == "",
			MessageCode: declared.Code, TriggerEvent: declared.Trigger,
		})
	}
	if page.Total == 0 {
		result.State, result.Reason = Empty, "this case holds no messages"
	}
	return result
}

// queryIndex returns the index a query is answered through: an index of this
// exact case beside it that can answer every question the query asks, or one
// built in memory. The in-memory index retains only the fields the query asks
// about, as values when a value is asked and as states otherwise, and is never
// written.
func queryIndex(ctx context.Context, root string, opened *bundle.Bundle, query grid.Query, at time.Time) (index.Document, error) {
	fields := query.IndexedFields()
	if len(fields) > 0 {
		if reused, ok := reusableIndex(root, opened, query, at); ok {
			return reused, nil
		}
	}
	policy := index.Policy{Fields: fields, Retention: index.RetainStates}
	if len(fields) == 0 {
		policy.Fields = []string{messageTypeField}
	}
	if len(query.AckCodes) > 0 || slices.ContainsFunc(query.Fields, func(p grid.FieldPredicate) bool { return p.Match != index.State }) {
		policy.Retention = index.RetainValues
	}
	return index.Build(ctx, opened, policy, at)
}

// reusableIndex finds an index in the workspace that describes this exact
// case, whose declared retention still covers now and which retains what the
// query asks, in the form it asks it. A file name is not ownership: every
// candidate is read and checked against the verified case, and one that is
// foreign, damaged, unsupported or expired is passed over without a word.
func reusableIndex(root string, opened *bundle.Bundle, query grid.Query, at time.Time) (index.Document, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return index.Document{}, false
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if _, err := artifactpath.File(root, entry.Name()); err != nil {
			continue
		}
		if kind, ok := classify(root, entry.Name(), false); !ok || kind != IndexArtifact {
			continue
		}
		document, err := index.Open(artifactpath.JoinReference(root, entry.Name()))
		if err != nil || document.Case.Identity != opened.Identity || document.Describes(opened) != nil ||
			document.Usable(at) != nil || !query.Answerable(document.Policy) {
			continue
		}
		return document, true
	}
	return index.Document{}, false
}

// messageFacts is what the list reads from the verified case beside the
// index: the parsed type and control ID of every occurrence and its original
// bytes. It implements grid.Facts.
type messageFacts struct {
	opened   *bundle.Bundle
	types    map[string]grid.MessageType
	controls map[string][]byte
	ackCodes []string
}

func readFacts(opened *bundle.Bundle) messageFacts {
	facts := messageFacts{opened: opened, types: make(map[string]grid.MessageType, len(opened.Events)),
		controls: make(map[string][]byte, len(opened.Events)), ackCodes: []string{}}
	code, _ := hl7.ParseSelector("MSH-9.1")
	trigger, _ := hl7.ParseSelector("MSH-9.2")
	outcome, _ := hl7.ParseSelector(grid.AckCodeSelector)
	for _, event := range opened.Events {
		declared := grid.MessageType{Kind: event.Kind}
		if event.Fields != nil && event.Fields.ControlID.State == hl7.Present {
			facts.controls[event.ID] = opened.Value(event.ID, event.Fields.ControlID)
		}
		if event.ParseError == "" && event.Kind != bundle.Unparsed {
			if document, err := opened.Document(event); err == nil {
				declared.Code = typePart(document, 0, code)
				declared.Trigger = typePart(document, 0, trigger)
				if event.Kind == bundle.Acknowledgement {
					if value, err := document.Select(0, outcome); err == nil && value.State == hl7.Present {
						raw := string(document.Bytes(value.Span))
						if raw != "" && len(raw) <= grid.MaxAckCodeBytes && escapeBytes([]byte(raw)) == raw && !slices.Contains(facts.ackCodes, raw) {
							facts.ackCodes = append(facts.ackCodes, raw)
						}
					}
				}
			}
		}
		facts.types[event.ID] = declared
	}
	return facts
}

// typePart is one MSH-9 component, escaped and bounded.
func typePart(document *hl7.Document, message int, selector hl7.Selector) string {
	value, err := document.Select(message, selector)
	if err != nil || value.State != hl7.Present {
		return ""
	}
	raw := document.Bytes(value.Span)
	if len(raw) > messageTypeBytes {
		return escapeBytes(raw[:messageTypeBytes]) + "…"
	}
	return escapeBytes(raw)
}

func (f messageFacts) Type(record index.Record) grid.MessageType { return f.types[record.ID] }

func (f messageFacts) ControlID(record index.Record) []byte { return f.controls[record.ID] }

func (f messageFacts) Content(record index.Record) []byte {
	raw, err := f.opened.Raw(record.ID)
	if err != nil {
		return nil
	}
	return raw
}

// facets lists the choices the filter sheet offers for this case. A message
// type is listed as the type rule compares it: ACK and Unparsed as whole kinds.
// A declaration no type rule can name, one too long to name exactly, is left
// out of the choices; its rows still show it.
func (f messageFacts) facets(opened *bundle.Bundle) MessageFacets {
	facets := MessageFacets{Types: []grid.MessageType{}, Sources: []string{}, AckCodes: f.ackCodes}
	for _, source := range opened.Manifest.Sources {
		facets.Sources = append(facets.Sources, source.ID)
	}
	for _, event := range opened.Events {
		declared := grid.MessageType{Kind: event.Kind}
		if event.Kind == bundle.Message {
			declared = f.types[event.ID]
		}
		if !slices.Contains(facets.Types, declared) && grid.ValidateQuery(grid.Query{Types: []grid.MessageType{declared}}) == nil {
			facets.Types = append(facets.Types, declared)
		}
	}
	return facets
}
