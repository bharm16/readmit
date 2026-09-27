package grid_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

// caseFacts answers a query's type and search axes from the verified case, the
// way the desktop reader does.
type caseFacts struct{ opened *bundle.Bundle }

func (f caseFacts) event(record index.Record) bundle.Event {
	for _, event := range f.opened.Events {
		if event.ID == record.ID {
			return event
		}
	}
	return bundle.Event{}
}

func (f caseFacts) part(record index.Record, path string) string {
	event := f.event(record)
	if event.Kind == bundle.Unparsed {
		return ""
	}
	document, err := f.opened.Document(event)
	if err != nil {
		return ""
	}
	selector, _ := hl7.ParseSelector(path)
	value, _ := document.Select(0, selector)
	return string(document.Bytes(value.Span))
}

func (f caseFacts) Type(record index.Record) grid.MessageType {
	return grid.MessageType{Kind: record.Kind, Code: f.part(record, "MSH-9.1"), Trigger: f.part(record, "MSH-9.2")}
}

func (f caseFacts) ControlID(record index.Record) []byte { return []byte(f.part(record, "MSH-10")) }

func (f caseFacts) SourceName(index.Record) string { return "" }

func (f caseFacts) Content(record index.Record) []byte {
	raw, _ := f.opened.Raw(record.ID)
	return raw
}

// directed declares one direction and time per occurrence.
func directed(entries ...any) map[int]bundle.Observation {
	declared := map[int]bundle.Observation{}
	for i := 0; i < len(entries); i += 2 {
		observation := bundle.Observation{Direction: entries[i].(bundle.Direction)}
		if minute, ok := entries[i+1].(int); ok {
			observation.ObservedAt = at(minute)
		}
		declared[i/2+1] = observation
	}
	return declared
}

func queryFixture(t *testing.T) (index.Document, caseFacts) {
	t.Helper()
	opened := caseOf(t,
		source{frame(booking) + frame(accepted), directed(bundle.Outbound, 5, bundle.Inbound, nil)},
		source{frame(rebooked) + frame(rejected) + frame(garbage), directed(bundle.Outbound, 2, bundle.Inbound, 3, bundle.Unknown, nil)},
	)
	return indexOf(t, opened, policy(index.RetainValues, nil, patient, nulled, grid.AckCodeSelector)), caseFacts{opened}
}

func queried(t *testing.T, document index.Document, facts grid.Facts, query grid.Query, order grid.Order) grid.Page {
	t.Helper()
	selected, err := grid.SelectQuery(document, now(), query, facts, order, whole())
	if err != nil {
		t.Fatalf("select query: %v", err)
	}
	return selected
}

func TestAQueryNarrowsByTypeNegationDirectionSearchAndEveryFilterAxis(t *testing.T) {
	document, facts := queryFixture(t)
	s13 := grid.MessageType{Kind: bundle.Message, Code: "SIU", Trigger: "S13"}
	ack := grid.MessageType{Kind: bundle.Acknowledgement}
	unparsed := grid.MessageType{Kind: bundle.Unparsed}
	for name, expected := range map[string]struct {
		query grid.Query
		rows  []string
	}{
		"type":                              {grid.Query{Types: []grid.MessageType{s13}}, []string{"s0002-e000001"}},
		"ack is a type":                     {grid.Query{Types: []grid.MessageType{ack, unparsed}}, []string{"s0001-e000002", "s0002-e000002", "s0002-e000003"}},
		"type is not":                       {grid.Query{NotTypes: []grid.MessageType{ack, unparsed}}, []string{"s0001-e000001", "s0002-e000001"}},
		"source is not":                     {grid.Query{NotSources: []string{"s0001"}}, []string{"s0002-e000001", "s0002-e000002", "s0002-e000003"}},
		"direction":                         {grid.Query{Directions: []bundle.Direction{bundle.Unknown}}, []string{"s0002-e000003"}},
		"directions":                        {grid.Query{Directions: []bundle.Direction{bundle.Outbound, bundle.Unknown}}, []string{"s0001-e000001", "s0002-e000001", "s0002-e000003"}},
		"exclusive end":                     {grid.Query{ObservedFrom: at(2), ObservedUntil: at(5)}, []string{"s0002-e000001", "s0002-e000002"}},
		"ack code":                          {grid.Query{AckCodes: []string{"AE"}}, []string{"s0002-e000002"}},
		"null state":                        {grid.Query{Fields: []grid.FieldPredicate{{Selector: nulled, Match: index.State, State: hl7.Null}}}, []string{"s0001-e000001", "s0002-e000001"}},
		"omitted state":                     {grid.Query{Fields: []grid.FieldPredicate{{Selector: patient, Match: index.State, State: hl7.Omitted}}}, []string{"s0001-e000002", "s0002-e000002"}},
		"metadata":                          {grid.Query{Search: &grid.TextSearch{Scope: grid.SearchMetadata, Text: "CTL-4"}}, []string{"s0002-e000002"}},
		"metadata type":                     {grid.Query{Search: &grid.TextSearch{Scope: grid.SearchMetadata, Text: "SIU^S12"}}, []string{"s0001-e000001"}},
		"metadata never reads content":      {grid.Query{Search: &grid.TextSearch{Scope: grid.SearchMetadata, Text: "MRN-2"}}, nil},
		"content":                           {grid.Query{Search: &grid.TextSearch{Scope: grid.SearchContent, Text: "MRN-2"}}, []string{"s0002-e000001"}},
		"content of an unparsed occurrence": {grid.Query{Search: &grid.TextSearch{Scope: grid.SearchContent, Text: "HL7-AT"}}, []string{"s0002-e000003"}},
		"axes and":                          {grid.Query{Types: []grid.MessageType{ack}, Sources: []string{"s0002"}}, []string{"s0002-e000002"}},
	} {
		selected := queried(t, document, facts, expected.query, grid.EvidenceOrder)
		if !equal(ids(selected), expected.rows) {
			t.Fatalf("the %s query kept %v, want %v", name, ids(selected), expected.rows)
		}
		if selected.Total != 5 || selected.Excluded != 5-len(expected.rows) {
			t.Fatalf("the %s query reported %+v", name, selected)
		}
	}
}

func TestAnEmptyQueryIsUnfilteredAndAZeroMatchQueryIsStillAnAnswer(t *testing.T) {
	document, facts := queryFixture(t)
	if all := queried(t, document, facts, grid.Query{}, grid.EvidenceOrder); all.Matched != 5 || all.Excluded != 0 || all.Undecodable != 1 {
		t.Fatalf("an empty query narrowed: %+v", all)
	}
	none := queried(t, document, facts, grid.Query{Sources: []string{"s0009"}}, grid.EvidenceOrder)
	if none.Total != 5 || none.Matched != 0 || len(none.Rows) != 0 {
		t.Fatalf("a query matching nothing: %+v", none)
	}
}

func TestSortingByTimeKeepsUnknownTimesLastInBothDirections(t *testing.T) {
	document, facts := queryFixture(t)
	for order, want := range map[grid.Order][]string{
		grid.EvidenceOrder:  {"s0001-e000001", "s0001-e000002", "s0002-e000001", "s0002-e000002", "s0002-e000003"},
		grid.TimeAscending:  {"s0002-e000001", "s0002-e000002", "s0001-e000001", "s0001-e000002", "s0002-e000003"},
		grid.TimeDescending: {"s0001-e000001", "s0002-e000002", "s0002-e000001", "s0001-e000002", "s0002-e000003"},
	} {
		if found := ids(queried(t, document, facts, grid.Query{}, order)); !equal(found, want) {
			t.Fatalf("order %q listed %v, want %v", order, found, want)
		}
	}
	windowed, err := grid.SelectQuery(document, now(), grid.Query{}, facts, grid.TimeAscending, grid.Window{Offset: 3, Limit: 1})
	if err != nil || !equal(ids(windowed), []string{"s0001-e000002"}) || windowed.Matched != 5 {
		t.Fatalf("a sorted window: %v %+v", err, windowed)
	}
	if _, err := grid.SelectQuery(document, now(), grid.Query{}, facts, "causal", whole()); err == nil {
		t.Fatal("an unknown order was accepted")
	}
}

func TestUnsupportedQueryCombinationsAreRefusedWithTheirReason(t *testing.T) {
	document, facts := queryFixture(t)
	s12 := grid.MessageType{Kind: bundle.Message, Code: "SIU", Trigger: "S12"}
	for name, query := range map[string]grid.Query{
		"type both required and excluded":   {Types: []grid.MessageType{s12}, NotTypes: []grid.MessageType{s12}},
		"source both required and excluded": {Sources: []string{"s0001"}, NotSources: []string{"s0001"}},
		"ack with a code":                   {Types: []grid.MessageType{{Kind: bundle.Acknowledgement, Code: "ACK"}}},
		"trigger without a code":            {Types: []grid.MessageType{{Kind: bundle.Message, Trigger: "S12"}}},
		"unknown kind":                      {Types: []grid.MessageType{{Kind: "event"}}},
		"unknown direction":                 {Directions: []bundle.Direction{"sideways"}},
		"repeated direction":                {Directions: []bundle.Direction{bundle.Inbound, bundle.Inbound}},
		"unknown scope":                     {Search: &grid.TextSearch{Scope: "regex", Text: "x"}},
		"empty search":                      {Search: &grid.TextSearch{Scope: grid.SearchMetadata}},
		"control search":                    {Search: &grid.TextSearch{Scope: grid.SearchContent, Text: "a\rb"}},
		"long search":                       {Search: &grid.TextSearch{Scope: grid.SearchContent, Text: strings.Repeat("x", grid.MaxSearchBytes+1)}},
		"end before start":                  {ObservedFrom: at(3), ObservedUntil: at(3)},
		"non-canonical field":               {Fields: []grid.FieldPredicate{{Selector: "PID-3", Match: index.Equals, Term: "x"}}},
	} {
		if _, err := grid.SelectQuery(document, now(), query, facts, grid.EvidenceOrder, whole()); err == nil {
			t.Fatalf("the %s query was accepted", name)
		} else if strings.Contains(err.Error(), "s0001") || strings.Contains(err.Error(), "SIU") {
			t.Fatalf("the %s refusal repeated a value: %v", name, err)
		}
	}
	// A question the index cannot answer is refused by the index, not
	// answered from something narrower.
	states := indexOf(t, facts.opened, policy(index.RetainStates, nil, patient))
	if _, err := grid.SelectQuery(states, now(), grid.Query{Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "MRN"}}}, facts, grid.EvidenceOrder, whole()); err == nil {
		t.Fatal("a states index answered a contains question")
	}
	expired := indexOf(t, facts.opened, policy(index.RetainStates, at(0), patient))
	if _, err := grid.SelectQuery(expired, time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC), grid.Query{}, facts, grid.EvidenceOrder, whole()); err == nil {
		t.Fatal("an expired index answered a query")
	}
}

func TestASavedFilterNarrowsAsTheQueryItConvertsTo(t *testing.T) {
	document, facts := queryFixture(t)
	filter := grid.Filter{Name: "v1", Kinds: []bundle.EventKind{bundle.Acknowledgement}, AckCodes: []string{"AA"}}
	saved := page(t, document, &filter, whole())
	converted := queried(t, document, facts, filter.Query(), grid.EvidenceOrder)
	if !equal(ids(saved), ids(converted)) || saved.Matched != converted.Matched || !equal(ids(converted), []string{"s0001-e000002"}) {
		t.Fatalf("a saved filter and its query disagree: %v %v", ids(saved), ids(converted))
	}
}

func TestAnswerableNamesTheIndexesThatCanAnswerAQuery(t *testing.T) {
	contains := grid.Query{Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "M"}}}
	equals := grid.Query{Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Equals, Term: "M"}}}
	state := grid.Query{Fields: []grid.FieldPredicate{{Selector: patient, Match: index.State, State: hl7.Empty}}}
	ack := grid.Query{AckCodes: []string{"AA"}}
	for name, expected := range map[string]struct {
		query  grid.Query
		policy index.Policy
		want   bool
	}{
		"values contains":  {contains, policy(index.RetainValues, nil, patient), true},
		"digests contains": {contains, policy(index.RetainDigests, nil, patient), false},
		"digests equals":   {equals, policy(index.RetainDigests, nil, patient), true},
		"states equals":    {equals, policy(index.RetainStates, nil, patient), false},
		"states state":     {state, policy(index.RetainStates, nil, patient), true},
		"missing field":    {state, policy(index.RetainStates, nil, nulled), false},
		"ack on states":    {ack, policy(index.RetainStates, nil, grid.AckCodeSelector), false},
		"ack on values":    {ack, policy(index.RetainValues, nil, grid.AckCodeSelector), true},
		"nothing asked":    {grid.Query{}, policy(index.RetainStates, nil, nulled), true},
	} {
		if got := expected.query.Answerable(expected.policy); got != expected.want {
			t.Fatalf("%s: answerable %v, want %v", name, got, expected.want)
		}
	}
}

// A count of what a query could not settle, or of what the case could not
// decode, leads to exactly those rows: a scope lists them under the query's
// other rules, and nothing else.
func TestAScopeListsExactlyTheRowsThePageCounted(t *testing.T) {
	long := strings.Repeat("A", index.MaxValueBytes) + "TAIL"
	message := "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-9|P|2.5.1\rPID|1||" + long + "\r"
	opened := caseOf(t, source{frame(booking) + frame(message) + frame(accepted) + frame(garbage), observed(0, 1, 2, 3)})
	document, facts := indexOf(t, opened, policy(index.RetainValues, nil, patient)), caseFacts{opened}
	tail := grid.Query{Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "TAIL"}}}

	counted := queried(t, document, facts, tail, grid.EvidenceOrder)
	if counted.Matched != 0 || counted.Undecided != 1 || counted.Undecodable != 1 {
		t.Fatalf("the query's counts: %+v", counted)
	}
	undecided := tail
	undecided.Scope = grid.UndecidedRows
	scoped := queried(t, document, facts, undecided, grid.EvidenceOrder)
	if scoped.Matched != counted.Undecided || !equal(ids(scoped), []string{opened.Events[1].ID}) {
		t.Fatalf("the rows it could not settle are %v, want the shortened value's occurrence", ids(scoped))
	}
	undecodable := queried(t, document, facts, grid.Query{Scope: grid.UndecodableRows}, grid.EvidenceOrder)
	if undecodable.Matched != counted.Undecodable || !equal(ids(undecodable), []string{opened.Events[3].ID}) {
		t.Fatalf("the rows that could not be decoded are %v", ids(undecodable))
	}

	// The query's other rules still apply to a scope.
	acknowledged := undecided
	acknowledged.Kinds = []bundle.EventKind{bundle.Acknowledgement}
	if narrowed := queried(t, document, facts, acknowledged, grid.EvidenceOrder); narrowed.Matched != 0 {
		t.Fatalf("a scope ignored the query's type rule: %v", ids(narrowed))
	}
	for _, refused := range []grid.Query{{Scope: grid.UndecidedRows}, {Scope: "everything"}} {
		if err := grid.ValidateQuery(refused); err == nil {
			t.Fatalf("accepted scope %q", refused.Scope)
		}
	}
}
