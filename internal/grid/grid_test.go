package grid_test

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
)

// The fixture holds one booked appointment and its accepted acknowledgement on
// the first source, and a second booking, a rejected acknowledgement and an
// occurrence nothing can decode on the second. That is every axis a filter
// narrows on: two types, two sources, distinct observed times, an accepted and
// a rejected outcome, present and null field states, and one occurrence the
// case itself could not read.
const (
	booking  = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE||\"\"|\rSCH|1||||||||||20260101130000\r"
	rebooked = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S13|CTL-3|P|2.5.1\rPID|1||MRN-2^^^READMIT^MR||ROE^RICHARD||\"\"|\rSCH|1||||||||||20260101140000\r"
	accepted = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|CTL-2|P|2.5.1\rMSA|AA|CTL-1\r"
	rejected = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120101||ACK^S13|CTL-4|P|2.5.1\rMSA|AE|CTL-3\r"
	garbage  = "NOT-HL7-AT-ALL"
)

func frame(message string) string { return "\x0b" + message + "\x1c\r" }

func at(minute int) *time.Time {
	instant := time.Date(2026, 1, 1, 12, minute, 0, 0, time.UTC)
	return &instant
}

func now() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) }

const (
	patient = "PID[1]-3[1]"
	nulled  = "PID[1]-7[1]"
)

// observed records one explicitly supplied observed time per occurrence. A case
// never infers one from file metadata or from a field, so a test that filters
// by time has to declare them the way a capture does.
func observed(minutes ...int) map[int]bundle.Observation {
	declared := make(map[int]bundle.Observation, len(minutes))
	for i, minute := range minutes {
		declared[i+1] = bundle.Observation{Direction: bundle.Inbound, ObservedAt: at(minute)}
	}
	return declared
}

type source struct {
	data     string
	observed map[int]bundle.Observation
}

func caseOf(t testing.TB, sources ...source) *bundle.Bundle {
	t.Helper()
	inputs := make([]bundle.Input, 0, len(sources))
	for i, declared := range sources {
		inputs = append(inputs, bundle.Input{
			Path:         "fixture-" + string(rune('a'+i)),
			Data:         []byte(declared.data),
			Options:      hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR},
			Observations: declared.observed,
		})
	}
	written, err := bundle.Write(filepath.Join(t.TempDir(), "case"), inputs,
		bundle.Provenance{Mode: bundle.Imported, ImportedAt: at(0)})
	if err != nil {
		t.Fatalf("case bundle: %v", err)
	}
	return written
}

// fixture is the two-source case above, indexed on the patient identifier, an
// explicitly null field and the acknowledgement code.
func fixture(t testing.TB, retention index.Retention) index.Document {
	t.Helper()
	opened := caseOf(t,
		source{frame(booking) + frame(accepted), observed(0, 1)},
		source{frame(rebooked) + frame(rejected) + frame(garbage), observed(2, 3, 4)},
	)
	return indexOf(t, opened, policy(retention, nil, patient, nulled, grid.AckCodeSelector))
}

func policy(retention index.Retention, until *time.Time, fields ...string) index.Policy {
	return index.Policy{Fields: fields, Retention: retention, RetainUntil: until}
}

func indexOf(t testing.TB, opened *bundle.Bundle, declared index.Policy) index.Document {
	t.Helper()
	document, err := index.Build(context.Background(), opened, declared, now())
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return document
}

func page(t *testing.T, document index.Document, filter *grid.Filter, window grid.Window) grid.Page {
	t.Helper()
	selected, err := grid.Select(document, now(), filter, window)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	return selected
}

func ids(selected grid.Page) []string {
	found := make([]string, 0, len(selected.Rows))
	for _, record := range selected.Rows {
		found = append(found, record.ID)
	}
	return found
}

func whole() grid.Window { return grid.Window{Offset: 0, Limit: grid.MaxRows} }

func equal(a, b []string) bool {
	return len(a) == len(b) && strings.Join(a, ",") == strings.Join(b, ",")
}

// The six axes the delivery names, each one narrowing the same case.
func TestAFilterNarrowsByTypeSourceTimeStateAckOutcomeAndFieldValue(t *testing.T) {
	document := fixture(t, index.RetainValues)

	for name, expected := range map[string]struct {
		filter grid.Filter
		rows   []string
	}{
		"type": {grid.Filter{Kinds: []bundle.EventKind{bundle.Acknowledgement}},
			[]string{"s0001-e000002", "s0002-e000002"}},
		"source": {grid.Filter{Sources: []string{"s0001"}},
			[]string{"s0001-e000001", "s0001-e000002"}},
		"time": {grid.Filter{ObservedFrom: at(1), ObservedUntil: at(3)},
			[]string{"s0001-e000002", "s0002-e000001"}},
		"state": {grid.Filter{Fields: []grid.FieldPredicate{{Selector: nulled, Match: index.State, State: hl7.Null}}},
			[]string{"s0001-e000001", "s0002-e000001"}},
		"ack outcome": {grid.Filter{AckCodes: []string{"AE"}},
			[]string{"s0002-e000002"}},
		"field value": {grid.Filter{Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "MRN-2"}}},
			[]string{"s0002-e000001"}},
	} {
		filter := expected.filter
		filter.Name = name
		selected := page(t, document, &filter, whole())
		if !equal(ids(selected), expected.rows) {
			t.Fatalf("the %s filter kept %v, want %v", name, ids(selected), expected.rows)
		}
		if selected.Matched != len(expected.rows) || selected.Excluded != selected.Total-selected.Matched {
			t.Fatalf("the %s filter reported %d matched and %d excluded of %d", name, selected.Matched, selected.Excluded, selected.Total)
		}
	}
}

// Axes are combined, not ranked: an occurrence has to satisfy all of them, and
// the alternatives within one axis are alternatives.
func TestAxesNarrowTogetherAndAlternativesWithinOneAxisDoNot(t *testing.T) {
	document := fixture(t, index.RetainValues)

	both := grid.Filter{Name: "either outcome", AckCodes: []string{"AA", "AE"}}
	if found := ids(page(t, document, &both, whole())); !equal(found, []string{"s0001-e000002", "s0002-e000002"}) {
		t.Fatalf("alternatives within one axis narrowed each other: %v", found)
	}

	together := grid.Filter{Name: "rejected on the second source", AckCodes: []string{"AA", "AE"}, Sources: []string{"s0002"}}
	if found := ids(page(t, document, &together, whole())); !equal(found, []string{"s0002-e000002"}) {
		t.Fatalf("two axes did not narrow together: %v", found)
	}

	// An axis nothing satisfies is an answer, not a failure, and the excluded
	// count is what says so.
	none := grid.Filter{Name: "nothing", Sources: []string{"s0009"}}
	empty := page(t, document, &none, whole())
	if empty.Matched != 0 || empty.Excluded != empty.Total || len(empty.Rows) != 0 {
		t.Fatalf("a filter matching nothing did not report every occurrence as excluded: %+v", empty)
	}
}

// A view that hides records without saying how many is the interface equivalent
// of a false negative. Every page states it, including the unfiltered one.
func TestEveryPageStatesHowManyOccurrencesItExcluded(t *testing.T) {
	document := fixture(t, index.RetainValues)

	unfiltered := page(t, document, nil, whole())
	if unfiltered.Total != 5 || unfiltered.Matched != 5 || unfiltered.Excluded != 0 {
		t.Fatalf("no filter excluded something: %+v", unfiltered)
	}
	if unfiltered.Undecodable != 1 {
		t.Fatalf("an occurrence the case could not decode was not reported: %+v", unfiltered)
	}

	filter := grid.Filter{Name: "acknowledgements", Kinds: []bundle.EventKind{bundle.Acknowledgement}}
	filtered := page(t, document, &filter, whole())
	if filtered.Total != 5 || filtered.Matched != 2 || filtered.Excluded != 3 {
		t.Fatalf("the excluded count does not account for every occurrence: %+v", filtered)
	}

	// The window a caller renders never changes what was excluded: a page of
	// one still reports the whole case behind it.
	narrow := page(t, document, &filter, grid.Window{Offset: 1, Limit: 1})
	if narrow.Matched != 2 || narrow.Excluded != 3 || len(narrow.Rows) != 1 || narrow.Rows[0].ID != "s0002-e000002" {
		t.Fatalf("a window changed what the filter reported it excluded: %+v", narrow)
	}
}

// An occurrence the case could not decode carries no indexed field, so a
// question about a field cannot be answered about it. Counting it as a plain
// exclusion would report unknown as a "no".
func TestAnUndecodableOccurrenceIsReportedRatherThanCountedAsAbsent(t *testing.T) {
	document := fixture(t, index.RetainValues)

	omitted := grid.Filter{Name: "omitted identifier",
		Fields: []grid.FieldPredicate{{Selector: patient, Match: index.State, State: hl7.Omitted}}}
	selected := page(t, document, &omitted, whole())
	if selected.Matched != 2 {
		t.Fatalf("the acknowledgements omit the patient identifier: %+v", ids(selected))
	}
	if selected.Undecodable != 1 {
		t.Fatalf("the occurrence the case could not decode was not named: %+v", selected)
	}
	for _, record := range selected.Rows {
		if record.ParseError != "" {
			t.Fatal("an occurrence nothing decoded was reported as one that omits the field")
		}
	}
}

// A value the index shortened cannot settle a substring question either way.
// Reporting that as a miss would say the case does not hold something nobody
// looked at.
func TestAShortenedValueIsUndecidedRatherThanAMiss(t *testing.T) {
	long := strings.Repeat("A", index.MaxValueBytes) + "TAIL"
	message := "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-9|P|2.5.1\rPID|1||" + long + "\r"
	opened := caseOf(t, source{frame(message), observed(0)})
	document := indexOf(t, opened, policy(index.RetainValues, nil, patient))

	kept := grid.Filter{Name: "prefix", Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "AAAA"}}}
	if found := page(t, document, &kept, whole()); found.Matched != 1 || found.Undecided != 0 {
		t.Fatalf("a substring inside the retained prefix was not a match: %+v", found)
	}

	cut := grid.Filter{Name: "tail", Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "TAIL"}}}
	beyond := page(t, document, &cut, whole())
	if beyond.Matched != 0 || beyond.Undecided != 1 || beyond.Excluded != 1 {
		t.Fatalf("a substring past the retained prefix was reported as a miss: %+v", beyond)
	}
}

// A grid over a large case renders a window of it. Nothing here materializes
// every matching occurrence for the caller, and the counts stay whole.
func TestAWindowRendersPartOfALargeCaseAndTheCountsStayWhole(t *testing.T) {
	const occurrences = 60
	var wire strings.Builder
	for i := range occurrences {
		control := "CTL-" + strconv.Itoa(i)
		wire.WriteString(frame("MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|" + control +
			"|P|2.5.1\rPID|1||MRN-" + strconv.Itoa(i) + "^^^READMIT^MR\r"))
	}
	document := indexOf(t, caseOf(t, source{wire.String(), nil}), policy(index.RetainValues, nil, patient))

	seen := make([]string, 0, occurrences)
	for offset := 0; offset < occurrences; offset += 25 {
		window := page(t, document, nil, grid.Window{Offset: offset, Limit: 25})
		if window.Total != occurrences || window.Matched != occurrences || window.Excluded != 0 {
			t.Fatalf("a window changed the counts of the case behind it: %+v", window)
		}
		if len(window.Rows) > 25 {
			t.Fatalf("a window of 25 rendered %d rows", len(window.Rows))
		}
		seen = append(seen, ids(window)...)
	}
	if len(seen) != occurrences {
		t.Fatalf("walking the windows rendered %d of %d occurrences", len(seen), occurrences)
	}
	for i, id := range seen {
		if id != "s0001-e"+strconv.Itoa(i + 1000001)[1:] {
			t.Fatalf("window %d rendered %s", i, id)
		}
	}

	// Past the last matching occurrence is an empty window, not an error.
	if beyond := page(t, document, nil, grid.Window{Offset: occurrences, Limit: 10}); len(beyond.Rows) != 0 || beyond.Matched != occurrences {
		t.Fatalf("a window past the last occurrence: %+v", beyond)
	}
}

func TestAWindowIsBounded(t *testing.T) {
	document := fixture(t, index.RetainValues)
	for name, window := range map[string]grid.Window{
		"no rows":                     {Offset: 0, Limit: 0},
		"negative rows":               {Offset: 0, Limit: -1},
		"unbounded":                   {Offset: 0, Limit: grid.MaxRows + 1},
		"before the first occurrence": {Offset: -1, Limit: 10},
	} {
		if _, err := grid.Select(document, now(), nil, window); err == nil {
			t.Fatalf("a window with %s was rendered", name)
		}
	}
}

// A question this index cannot answer is refused by name. Answering it from
// something narrower would report a filtered view that rests on less than it
// claims to.
func TestAQuestionTheIndexCannotAnswerIsRefused(t *testing.T) {
	values := fixture(t, index.RetainValues)
	unretained := grid.Filter{Name: "unretained",
		Fields: []grid.FieldPredicate{{Selector: "PID[1]-5[1]", Match: index.Contains, Term: "DOE"}}}
	if _, err := grid.Select(values, now(), &unretained, whole()); err == nil {
		t.Fatal("a filter asked about a field this index does not retain")
	}

	digests := fixture(t, index.RetainDigests)
	substring := grid.Filter{Name: "substring",
		Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "MRN"}}}
	if _, err := grid.Select(digests, now(), &substring, whole()); err == nil {
		t.Fatal("an index of digests answered a substring question")
	}
	exact := grid.Filter{Name: "exact",
		Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Equals, Term: "MRN-1^^^READMIT^MR"}}}
	if found := page(t, digests, &exact, whole()); found.Matched != 1 {
		t.Fatalf("an index of digests did not answer an exact question: %+v", found)
	}

	states := fixture(t, index.RetainStates)
	if _, err := grid.Select(states, now(), &exact, whole()); err == nil {
		t.Fatal("an index that retains no values answered a value question")
	}
	declared := grid.Filter{Name: "declared",
		Fields: []grid.FieldPredicate{{Selector: patient, Match: index.State, State: hl7.Present}}}
	if found := page(t, states, &declared, whole()); found.Matched != 2 {
		t.Fatalf("an index that retains only states did not answer a state question: %+v", found)
	}

	// An acknowledgement outcome is one field question like any other: an index
	// that does not retain it refuses rather than reporting no outcomes.
	narrow := indexOf(t, caseOf(t, source{frame(booking) + frame(accepted), observed(0, 1)}),
		policy(index.RetainValues, nil, patient))
	outcome := grid.Filter{Name: "accepted", AckCodes: []string{"AA"}}
	if _, err := grid.Select(narrow, now(), &outcome, whole()); err == nil {
		t.Fatal("an index that does not retain the acknowledgement code reported its outcomes")
	}
}

// Retention is the index's own decision and it is made before any question, so
// a filter that asks nothing of a value is refused exactly as one that does.
func TestAnIndexPastItsRetentionAnswersNoFilter(t *testing.T) {
	ended := at(0)
	opened := caseOf(t, source{frame(booking) + frame(accepted), observed(0, 1)})
	document := indexOf(t, opened, policy(index.RetainValues, ended, patient, grid.AckCodeSelector))

	for name, filter := range map[string]*grid.Filter{
		"no filter at all":                        nil,
		"one that asks only about the occurrence": {Name: "acks", Kinds: []bundle.EventKind{bundle.Acknowledgement}},
		"one that asks about a value":             {Name: "identifier", Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "MRN"}}},
	} {
		if _, err := grid.Select(document, now(), filter, whole()); err == nil {
			t.Fatalf("an index past its retention answered %s", name)
		}
	}
	// Before the declared end it answers normally, so the refusal is retention
	// and not something about the document.
	if _, err := grid.Select(document, at(0).Add(-time.Hour), nil, whole()); err != nil {
		t.Fatalf("an index inside its retention refused: %v", err)
	}
}

// An occurrence the case recorded no observed time for cannot satisfy a time
// bound. Unknown is not a pass, and the excluded count says it was left out.
func TestAnOccurrenceWithNoRecordedTimeCannotSatisfyATimeFilter(t *testing.T) {
	opened := caseOf(t, source{frame(booking) + frame(accepted), observed(0)})
	document := indexOf(t, opened, policy(index.RetainValues, nil, patient))

	filter := grid.Filter{Name: "any time", ObservedFrom: at(0), ObservedUntil: at(59)}
	selected := page(t, document, &filter, whole())
	if selected.Matched != 1 || selected.Rows[0].ID != "s0001-e000001" || selected.Excluded != 1 {
		t.Fatalf("an occurrence with no recorded observed time was admitted by a time filter: %+v", selected)
	}
}

// What a filter could not settle is counted over the whole case, because that
// is the scope the index answers a field question over. It is an upper bound on
// the uncertainty in the view — never an under-statement — and the count below
// says so: the occurrence it belongs to was already excluded by its type.
func TestWhatCouldNotBeSettledIsAnUpperBoundOverTheWholeCase(t *testing.T) {
	long := strings.Repeat("A", index.MaxValueBytes) + "TAIL"
	message := "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-9|P|2.5.1\rPID|1||" + long + "\r"
	opened := caseOf(t, source{frame(message) + frame(accepted), observed(0, 1)})
	document := indexOf(t, opened, policy(index.RetainValues, nil, patient))

	filter := grid.Filter{Name: "acknowledged tail", Kinds: []bundle.EventKind{bundle.Acknowledgement},
		Fields: []grid.FieldPredicate{{Selector: patient, Match: index.Contains, Term: "TAIL"}}}
	selected := page(t, document, &filter, whole())
	if selected.Matched != 0 || selected.Excluded != 2 {
		t.Fatalf("the filter kept something: %+v", selected)
	}
	if selected.Undecided != 1 {
		t.Fatalf("an unsettled value of an occurrence this filter excluded by type was not counted: %+v", selected)
	}
}

func TestAckCodeSelectorIsTheCanonicalAcknowledgementCodeField(t *testing.T) {
	selector, err := hl7.ParseSelector("MSA-1")
	if err != nil || selector.String() != grid.AckCodeSelector {
		t.Fatalf("the acknowledgement outcome is read from %q, not %q: %v", selector.String(), grid.AckCodeSelector, err)
	}
}
