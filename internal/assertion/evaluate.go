package assertion

import (
	"context"
	"math/big"
	"regexp"

	"github.com/bharm16/readmit/internal/hl7"
)

// Message is one parsed message a field reference may address, and the
// zero-based index of the message inside its document. The evaluator reads
// the document through the shared selector and never rewrites it.
type Message struct {
	Document *hl7.Document
	Index    int
}

// Collection is one observation's records, as the ordered keys a collector
// read out of them. Complete is the window's own verdict, and it is the whole
// of the rule this package enforces: only a collection that completed is
// evidence, and one that did not is an execution error rather than a count.
//
// Keys are the only thing an observation reads out of a record, so nothing
// else about a record reaches an assertion, a report or a diagnostic.
type Collection struct {
	Complete bool
	Keys     []string
}

// Evidence is what a set is evaluated against: the messages the interface
// produced, the messages the run sent, and the observations taken before and
// after. A caller supplies material it has already verified; this package
// opens no file, reads no path and sends nothing.
type Evidence struct {
	Observed map[string]Message
	Input    map[string]Message
	Before   Collection
	After    Collection
}

// Outcome is what one assertion produced.
type Outcome string

const (
	// OutcomePassed: the evidence decided the question and agrees.
	OutcomePassed Outcome = "passed"
	// OutcomeFailed: the evidence decided the question and disagrees.
	OutcomeFailed Outcome = "failed"
	// OutcomeUndecided: the evidence cannot decide the question either way.
	// It is never a pass and never a failure.
	OutcomeUndecided Outcome = "undecided"
	// OutcomeSkipped: the assertion's condition did not hold, so it asserted
	// nothing. A skipped assertion is not a pass.
	OutcomeSkipped Outcome = "skipped"
)

// Verdict is what one set produced.
type Verdict string

const (
	// VerdictPass: at least one assertion was decided, every decided
	// assertion agreed, and none was undecided.
	VerdictPass Verdict = "pass"
	// VerdictFail: at least one assertion disagreed.
	VerdictFail Verdict = "fail"
	// VerdictUndecided: nothing disagreed and nothing settled the question —
	// either an assertion could not be decided, or every one was skipped.
	VerdictUndecided Verdict = "undecided"
)

// Reading is what the evaluator read for one assertion. Exactly the members
// the operator reads are set and the rest are nil, so a report never states a
// count for an assertion that read no collection.
//
// A reading holds original source values. It is customer-local evidence, in
// the same sense a result directory is, and is never a share-approved
// artifact.
type Reading struct {
	// Field is the value a field subject resolved to, and the left side of a
	// pair.
	Field *FieldValue
	// Compared is the right side of a pair.
	Compared *FieldValue
	// Records is how many records the subject collection held.
	Records *int
	// Matched is how many of them matched, under a quantifier.
	Matched *int
	// Added and Removed are the distinct keys one transition gained and lost.
	Added, Removed *int
}

// Result is one assertion's outcome and what was read to reach it.
type Result struct {
	ID       string
	Operator Operator
	Outcome  Outcome
	Observed Reading
}

// Report is what one evaluation produced. An execution error produces no
// report at all: every assertion is left unevaluated rather than reported
// against evidence the evaluator could not stand behind.
type Report struct {
	Verdict                            Verdict
	Results                            []Result
	Passed, Failed, Undecided, Skipped int
}

// Error is an execution error: the evaluator could not read the evidence one
// assertion names. It carries the fixed class and the author-chosen assertion
// id, and never a value, a path or a selector.
type Error struct {
	Class     string
	Assertion string
}

func (e *Error) Error() string {
	if e.Assertion == "" {
		return "assertion evaluation: " + e.Class
	}
	return "assertion " + e.Assertion + ": " + e.Class
}

const (
	// ErrorNotDecoded: the set did not come through Decode, so nothing about
	// it has been bounded or accepted.
	ErrorNotDecoded = "set_not_decoded"
	// ErrorUnknownMessage: the set names a message this evidence does not
	// hold. Absent evidence is not an absent value.
	ErrorUnknownMessage = "unknown_message"
	// ErrorUnreadableValue: a selected value could not be read — an
	// unsupported escape or invalid UTF-8 in the evidence itself.
	ErrorUnreadableValue = "unreadable_value"
	// ErrorIncompleteObservation: a collection assertion was asked of an
	// observation that did not complete. Failed collection is never a
	// passing absence assertion.
	ErrorIncompleteObservation = "incomplete_observation"
	// ErrorRecordLimit: an observation holds more records than one assertion
	// accounts for, so what it holds is a prefix rather than the source.
	ErrorRecordLimit = "record_limit"
	// ErrorCancelled: evaluation was cancelled. A cancelled evaluation
	// produces no verdict.
	ErrorCancelled = "cancelled"
)

// Evaluate decides every assertion of the set against the evidence. It is a
// pure function of the two: it retains nothing, so evaluating the same set
// against the same evidence again after a cancellation produces the same
// report.
func (s Set) Evaluate(ctx context.Context, evidence Evidence) (Report, error) {
	if !s.decoded {
		return Report{}, &Error{Class: ErrorNotDecoded}
	}
	report := Report{Results: make([]Result, 0, len(s.Assertions))}
	for _, assertion := range s.Assertions {
		if ctx.Err() != nil {
			return Report{}, &Error{Class: ErrorCancelled, Assertion: assertion.ID}
		}
		result, err := evidence.decide(assertion)
		if err != nil {
			return Report{}, err
		}
		report.Results = append(report.Results, result)
		switch result.Outcome {
		case OutcomePassed:
			report.Passed++
		case OutcomeFailed:
			report.Failed++
		case OutcomeUndecided:
			report.Undecided++
		default:
			report.Skipped++
		}
	}
	switch {
	case report.Failed > 0:
		report.Verdict = VerdictFail
	case report.Undecided > 0 || report.Passed == 0:
		report.Verdict = VerdictUndecided
	default:
		report.Verdict = VerdictPass
	}
	return report, nil
}

func (e Evidence) decide(assertion Assertion) (Result, error) {
	result := Result{ID: assertion.ID, Operator: assertion.Operator}
	if assertion.When != nil {
		value, err := e.resolve(assertion.When.Field, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		if !sameValue(value, assertion.When.Equals) {
			result.Outcome = OutcomeSkipped
			return result, nil
		}
	}
	switch kind, _ := assertion.Subject.kind(); kind {
	case subjectField:
		value, err := e.resolve(*assertion.Subject.Field, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		result.Observed.Field = &value
		result.Outcome = decideField(assertion, value)
	case subjectPair:
		left, err := e.resolve(assertion.Subject.Pair.Left, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		right, err := e.resolve(assertion.Subject.Pair.Right, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		result.Observed.Field, result.Observed.Compared = &left, &right
		result.Outcome = decide(sameValue(left, right) == *assertion.Expected.Holds)
	case subjectCollection:
		keys, err := e.records(assertion.Subject.Collection.Scope, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		total := len(keys)
		result.Observed.Records = &total
		result.Outcome = decideCollection(assertion, keys)
	case subjectEach:
		keys, err := e.records(assertion.Subject.Each.Scope, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		total := len(keys)
		result.Observed.Records = &total
		result.Outcome = OutcomeUndecided
		// A quantifier over no record quantifies over nothing. A vacuous
		// truth is not an observation, so it is undecided rather than a pass.
		if matched, ok := matches(assertion.parsed.pattern, keys); ok && total > 0 {
			result.Observed.Matched = &matched
			result.Outcome = decide(quantified(assertion.Subject.Each.Quantifier, matched, total))
		}
	case subjectTransition:
		from, err := e.records(assertion.Subject.Transition.From, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		to, err := e.records(assertion.Subject.Transition.To, assertion.ID)
		if err != nil {
			return Result{}, err
		}
		added, removed := missing(to, from), missing(from, to)
		result.Observed.Added, result.Observed.Removed = &added, &removed
		result.Outcome = decide(added == assertion.Expected.Change.Added && removed == assertion.Expected.Change.Removed)
	default:
		return Result{}, &Error{Class: ErrorNotDecoded, Assertion: assertion.ID}
	}
	return result, nil
}

// decideField answers one field assertion. The three shape operators always
// decide, because a state is always one of four. Every other operator reads a
// value, and reads only a present one it can read as its own type: an empty,
// null or omitted value, present text past the match bound, and present text
// that is not the number or timestamp the operator compares are each
// undecided rather than a failure, because none of them is evidence that the
// value is outside the range, the window or the pattern.
func decideField(assertion Assertion, value FieldValue) Outcome {
	switch assertion.Operator {
	case FieldEquals:
		return decide(sameValue(value, *assertion.Expected.Field))
	case FieldNotEquals:
		return decide(!sameValue(value, *assertion.Expected.Field))
	case FieldState:
		return decide(value.State == *assertion.Expected.State)
	}
	if value.State != hl7.Present || value.Text == nil {
		return OutcomeUndecided
	}
	text := *value.Text
	switch assertion.Operator {
	case TextMatches:
		if len(text) > MaxMatchBytes {
			return OutcomeUndecided
		}
		return decide(assertion.parsed.pattern.MatchString(text))
	case NumericRange:
		observed, ok := number(text)
		if !ok {
			return OutcomeUndecided
		}
		return decide(observed.Cmp(assertion.parsed.minimum) >= 0 && observed.Cmp(assertion.parsed.maximum) <= 0)
	case NumericTolerance:
		observed, ok := number(text)
		if !ok {
			return OutcomeUndecided
		}
		distance := new(big.Rat).Sub(observed, assertion.parsed.expected)
		return decide(distance.Abs(distance).Cmp(assertion.parsed.allowed) <= 0)
	case DateWindow:
		observed, ok := instant(text)
		if !ok {
			return OutcomeUndecided
		}
		return decide(!observed.Before(assertion.parsed.from) && !observed.After(assertion.parsed.to))
	default:
		return OutcomeUndecided
	}
}

// decideCollection answers one collection assertion. Every operator here
// decides: the collection completed, so what it holds is what the source
// held, and a count of zero is an observation.
func decideCollection(assertion Assertion, keys []string) Outcome {
	switch assertion.Operator {
	case RecordCount:
		return decide(len(keys) == *assertion.Expected.Count)
	case RecordsUnique:
		return decide(unique(keys) == *assertion.Expected.Holds)
	case RecordsAbsent:
		return decide((len(keys) == 0) == *assertion.Expected.Holds)
	case RecordsContain:
		return decide(containsAll(keys, *assertion.Expected.Keys))
	case RecordsOrdered:
		return decide(ordered(keys, *assertion.Expected.Keys))
	case RecordMultiplicity:
		return decide(occurrences(keys, assertion.Expected.Multiplicity.Key) == assertion.Expected.Multiplicity.Count)
	default:
		return OutcomeUndecided
	}
}

// resolve reads one value out of the evidence through the shared selector.
// Evidence the set names and the caller did not supply is an execution error,
// never an omitted value: "we did not look" is not "we looked and found none".
func (e Evidence) resolve(ref FieldRef, id string) (FieldValue, error) {
	var messages map[string]Message
	switch ref.Scope {
	case ObservedMessages:
		messages = e.Observed
	case InputMessages:
		messages = e.Input
	default:
		return FieldValue{}, &Error{Class: ErrorNotDecoded, Assertion: id}
	}
	message, ok := messages[ref.Message]
	if !ok || message.Document == nil || message.Index < 0 || message.Index >= len(message.Document.Messages) {
		return FieldValue{}, &Error{Class: ErrorUnknownMessage, Assertion: id}
	}
	reading, err := message.Document.Read(message.Index, ref.selector, hl7.IgnoreMSH18)
	if err != nil {
		return FieldValue{}, &Error{Class: ErrorUnreadableValue, Assertion: id}
	}
	resolved := FieldValue{State: reading.State}
	if reading.State == hl7.Present {
		text, ok := reading.Text()
		if !ok {
			return FieldValue{}, &Error{Class: ErrorUnreadableValue, Assertion: id}
		}
		resolved.Text = &text
	}
	return resolved, nil
}

// records reads one observation. An observation that did not complete cannot
// answer how many records a source held, and one holding more records than
// this contract accounts for holds a prefix of the source rather than the
// source; both are execution errors.
func (e Evidence) records(scope RecordScope, id string) ([]string, error) {
	var collection Collection
	switch scope {
	case BeforeRecords:
		collection = e.Before
	case AfterRecords:
		collection = e.After
	default:
		return nil, &Error{Class: ErrorNotDecoded, Assertion: id}
	}
	if !collection.Complete {
		return nil, &Error{Class: ErrorIncompleteObservation, Assertion: id}
	}
	if len(collection.Keys) > maxRecords {
		return nil, &Error{Class: ErrorRecordLimit, Assertion: id}
	}
	return collection.Keys, nil
}

func decide(agreed bool) Outcome {
	if agreed {
		return OutcomePassed
	}
	return OutcomeFailed
}

// sameValue compares two values as the four-state values they are. A present
// value equals another only when its text is identical byte for byte; no
// timezone, Unicode, case, whitespace or identifier normalization is implied.
func sameValue(left, right FieldValue) bool {
	if left.State != right.State {
		return false
	}
	if left.Text == nil || right.Text == nil {
		return left.Text == nil && right.Text == nil
	}
	return *left.Text == *right.Text
}

// matches counts the keys one pattern matches. A key past the match bound
// cannot be decided on a prefix, so it makes the whole quantifier undecided
// rather than silently counting as a miss.
func matches(pattern *regexp.Regexp, keys []string) (int, bool) {
	matched := 0
	for _, key := range keys {
		if len(key) > MaxMatchBytes {
			return 0, false
		}
		if pattern.MatchString(key) {
			matched++
		}
	}
	return matched, true
}

func quantified(quantifier Quantifier, matched, total int) bool {
	switch quantifier {
	case QuantifierEvery:
		return matched == total
	case QuantifierAny:
		return matched > 0
	case QuantifierNone:
		return matched == 0
	default:
		return false
	}
}

func unique(keys []string) bool {
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func containsAll(keys, declared []string) bool {
	observed := make(map[string]bool, len(keys))
	for _, key := range keys {
		observed[key] = true
	}
	for _, key := range declared {
		if !observed[key] {
			return false
		}
	}
	return true
}

// ordered reports whether the declared keys occur in that relative order.
// Other records may appear between them; a declared key that was never
// observed makes the order unsatisfied. Taking the earliest occurrence of each
// declared key leaves the most room for the ones after it, so one pass over
// the observed keys decides it.
func ordered(keys, declared []string) bool {
	next := 0
	for _, key := range keys {
		if next < len(declared) && key == declared[next] {
			next++
		}
	}
	return next == len(declared)
}

func occurrences(keys []string, key string) int {
	count := 0
	for _, observed := range keys {
		if observed == key {
			count++
		}
	}
	return count
}

// missing counts the distinct keys of one collection that the other does not
// hold. A transition is compared as two sets: repeating a key does not add a
// record to it.
func missing(keys, other []string) int {
	held := make(map[string]bool, len(other))
	for _, key := range other {
		held[key] = true
	}
	absent := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !held[key] {
			absent[key] = true
		}
	}
	return len(absent)
}
