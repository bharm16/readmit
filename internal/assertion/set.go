// Package assertion reads the shared assertion contract of ADR-0003: one
// versioned strict-JSON document holding a finite set of typed assertions
// about what an interface produced, evaluated by typed Go operators against
// evidence a caller already verified.
//
// A set is data that names an operator, never code. There is no expression
// language, no embedded script, no interpreter, no program path and no
// callback: a new question is a new typed operator in Go, with tests. Field
// values are addressed with the one shared selector grammar
// `internal/hl7` owns, so an assertion, a diagnosis and a diff ask the same
// bytes the same question.
//
// Four states are four separate answers. `present`, `empty`, `null` and
// `omitted` never collapse into one another, and the operators that read a
// value rather than its shape decide only on a present value they can read:
// an empty, null or omitted value, and present text an operator's own type
// cannot read, are undecided. Undecided is never a pass and never a failure,
// because "we could not tell" is a third answer and this contract exists to
// keep it from being mistaken for either of the other two.
//
// A collection assertion is evaluated only against an observation that
// completed. Following the rule docs/observe.md states, failed collection
// never becomes a passing absence assertion: an incomplete observation is an
// execution error here, not a count of zero.
//
// A set names no file, no path, no endpoint and no credential, and evaluating
// one opens nothing and sends nothing. No command reads an assertion set in
// this release. See docs/assertions.md.
package assertion

import (
	"encoding/json/v2"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
)

// Schema is the contract an assertion set declares.
const Schema = "readmit-assertion-set/v1"

const (
	// MaxSetBytes bounds the document a reader decodes before decoding it.
	MaxSetBytes = 256 << 10
	// MaxAssertions bounds one set, matching the bound readmit-test/v1 holds
	// its own assertions to.
	MaxAssertions = 256
	// MaxMatchBytes bounds the text one pattern or quantifier is matched
	// against. Longer text is undecided rather than matched on a prefix.
	MaxMatchBytes = 4 << 10
	// maxRecords bounds the records one collection assertion accounts for. It
	// is the bound readmit-observation-window/v1 holds one window's declared
	// scope and one sample's record count to, so a window that completed
	// legitimately is never past it.
	maxRecords = 1 << 20
	// maxKeys bounds the record keys one assertion may declare.
	maxKeys = 512
	// maxKeyBytes bounds one declared record key, as a collector's own
	// assigned identity is bounded.
	maxKeyBytes = 128
	// maxPatternBytes bounds one regular expression's source.
	maxPatternBytes = 256
	// maxNumberBytes bounds one decimal literal.
	maxNumberBytes = 32
	// maxTextBytes bounds an expected present value, as readmit-test/v1
	// bounds its own.
	maxTextBytes = 65536
	// maxNameBytes bounds the set's local description.
	maxNameBytes = 256
	// timestampLayout is the one HL7 timestamp form a temporal assertion
	// reads: whole seconds with an explicit numeric offset. Nothing is
	// assumed about a value that carries no offset.
	timestampLayout = "20060102150405-0700"
)

// MessageScope names which side of a run a field reference addresses.
type MessageScope string

const (
	// ObservedMessages are the messages the interface produced.
	ObservedMessages MessageScope = "observed"
	// InputMessages are the messages the run sent. A mapping expectation is
	// one field reference into each scope.
	InputMessages MessageScope = "input"
)

// RecordScope names which observation a collection reference addresses.
type RecordScope string

const (
	// BeforeRecords is what was observed before the window opened.
	BeforeRecords RecordScope = "before"
	// AfterRecords is what was observed after it closed.
	AfterRecords RecordScope = "after"
)

// Quantifier says how many records of a collection a per-record assertion
// must hold for.
type Quantifier string

const (
	// QuantifierEvery requires every observed record.
	QuantifierEvery Quantifier = "every"
	// QuantifierAny requires at least one observed record.
	QuantifierAny Quantifier = "any"
	// QuantifierNone requires no observed record.
	QuantifierNone Quantifier = "none"
)

// Operator names one typed question. The set is closed: a document naming
// anything else is refused, and the table below is the only place an operator
// is bound to the subject it reads and the expectation it takes.
type Operator string

const (
	// FieldEquals compares state and text exactly. It always decides.
	FieldEquals Operator = "field_equals"
	// FieldNotEquals passes when state or text differs. It always decides.
	FieldNotEquals Operator = "field_not_equals"
	// FieldState compares only the four-state shape, which is how presence,
	// absence, empty and HL7 null are expressed. It always decides.
	FieldState Operator = "field_state"
	// TextMatches applies one bounded regular expression to present text.
	TextMatches Operator = "text_matches"
	// NumericRange places a present decimal value in an inclusive range.
	NumericRange Operator = "numeric_range"
	// NumericTolerance places a present decimal value within a tolerance of
	// an expected one.
	NumericTolerance Operator = "numeric_tolerance"
	// DateWindow places a present HL7 timestamp in an inclusive window.
	DateWindow Operator = "date_window"
	// ValuesEqual relates two field references: the same field of two
	// messages, or an input field and the observed field it maps to.
	ValuesEqual Operator = "values_equal"
	// RecordCount compares how many records one observation held.
	RecordCount Operator = "record_count"
	// RecordsUnique reports whether every observed record key is distinct.
	RecordsUnique Operator = "records_unique"
	// RecordsContain requires every declared key to have been observed.
	RecordsContain Operator = "records_contain"
	// RecordsOrdered requires the declared keys in that relative order.
	RecordsOrdered Operator = "records_ordered"
	// RecordMultiplicity counts how many records one key produced.
	RecordMultiplicity Operator = "record_multiplicity"
	// RecordsAbsent is the bounded no-output assertion. Only a completed
	// observation can support it.
	RecordsAbsent Operator = "records_absent"
	// RecordKeyMatches applies one bounded pattern per record under a
	// quantifier.
	RecordKeyMatches Operator = "record_key_matches"
	// RecordsChanged compares two observations as sets of keys.
	RecordsChanged Operator = "records_changed"
)

// Set is one assertion set exactly as written. Only Decode produces one that
// evaluates: a Set assembled in Go without going through the reader refuses
// to evaluate, so the reader's refusals cannot be bypassed by construction.
type Set struct {
	Schema string `json:"schema"`
	// Name is a local description. It is never a path and never an identity.
	Name       string      `json:"name"`
	Assertions []Assertion `json:"assertions"`

	decoded bool
}

// Assertion is one typed question. Every member is required; When is
// explicitly null for an unconditional assertion, because an omitted
// declaration is an error here rather than a likely value.
type Assertion struct {
	ID       string     `json:"id"`
	Operator Operator   `json:"operator"`
	Subject  Subject    `json:"subject"`
	When     *Condition `json:"when"`
	Expected Expected   `json:"expected"`

	// parsed is what the reader made of this assertion's expectation, so a
	// set that decoded holds nothing the evaluator must read again.
	parsed reading
}

// reading is what the reader parsed once. The evaluator parses no expectation
// and discards no error, because a bound that did not parse never reached it.
// Exactly the members this assertion's own operator needs are set.
type reading struct {
	// pattern belongs to text_matches and record_key_matches.
	pattern *regexp.Regexp
	// minimum and maximum belong to numeric_range; expected and allowed
	// belong to numeric_tolerance.
	minimum, maximum  *big.Rat
	expected, allowed *big.Rat
	// from and to belong to date_window.
	from, to time.Time
}

// Subject is a closed typed union naming what one assertion reads. Exactly
// one member is present.
type Subject struct {
	Field      *FieldRef      `json:"field,omitzero"`
	Pair       *PairRef       `json:"pair,omitzero"`
	Collection *RecordRef     `json:"collection,omitzero"`
	Each       *EachRef       `json:"each,omitzero"`
	Transition *TransitionRef `json:"transition,omitzero"`
}

// FieldRef addresses one value: which side of the run, which message, and
// which field, in the shared selector grammar. There are no wildcards and no
// "find any" semantics; see docs/selectors.md.
type FieldRef struct {
	Scope    MessageScope `json:"scope"`
	Message  string       `json:"message"`
	Selector string       `json:"selector"`

	selector hl7.Selector
}

// PairRef addresses two values for one relationship.
type PairRef struct {
	Left  FieldRef `json:"left"`
	Right FieldRef `json:"right"`
}

// RecordRef addresses one observation's records.
type RecordRef struct {
	Scope RecordScope `json:"scope"`
}

// EachRef addresses one observation's records under a quantifier.
type EachRef struct {
	Scope      RecordScope `json:"scope"`
	Quantifier Quantifier  `json:"quantifier"`
}

// TransitionRef addresses two observations as a before and an after. The two
// scopes must differ: a transition from an observation to itself is not one.
type TransitionRef struct {
	From RecordScope `json:"from"`
	To   RecordScope `json:"to"`
}

// Condition is the one conditional this contract carries: an assertion is
// evaluated only when the named field holds exactly this value, and is
// reported skipped otherwise. A skipped assertion asserts nothing; it is
// never a pass.
type Condition struct {
	Field  FieldRef   `json:"field"`
	Equals FieldValue `json:"equals"`
}

// FieldValue distinguishes omission, empty and HL7 null explicitly. Text
// belongs to a present value and to no other state.
type FieldValue struct {
	State hl7.State `json:"state"`
	Text  *string   `json:"text,omitzero"`
}

// Expected is a closed typed union. Exactly one member belongs to each
// operator, and the reader refuses any other pairing.
type Expected struct {
	Field        *FieldValue   `json:"field,omitzero"`
	State        *hl7.State    `json:"state,omitzero"`
	Pattern      *string       `json:"pattern,omitzero"`
	Range        *Range        `json:"range,omitzero"`
	Tolerance    *Tolerance    `json:"tolerance,omitzero"`
	Window       *Window       `json:"window,omitzero"`
	Holds        *bool         `json:"holds,omitzero"`
	Count        *int          `json:"count,omitzero"`
	Keys         *[]string     `json:"keys,omitzero"`
	Multiplicity *Multiplicity `json:"multiplicity,omitzero"`
	Change       *Change       `json:"change,omitzero"`
}

// Range is an inclusive decimal range. Both bounds are written as decimal
// strings and compared exactly: a regression expectation is not a float.
type Range struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

// Tolerance is an expected decimal value and the exact distance from it that
// still passes. Both are decimal strings and the tolerance is not negative.
type Tolerance struct {
	Value     string `json:"value"`
	Tolerance string `json:"tolerance"`
}

// Window is an inclusive instant range. Both bounds are HL7 timestamps with
// an explicit numeric offset, because a window with no zone is not a window.
type Window struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Multiplicity is how many records one key is expected to have produced.
type Multiplicity struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// Change is how many distinct keys one transition added and removed.
type Change struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// subjectKind and expectedMember name the members of the two typed unions, so
// operator compatibility is one table rather than a switch in every reader.
type subjectKind string

const (
	subjectField      subjectKind = "field"
	subjectPair       subjectKind = "pair"
	subjectCollection subjectKind = "collection"
	subjectEach       subjectKind = "each"
	subjectTransition subjectKind = "transition"
)

type expectedMember string

const (
	expectField        expectedMember = "field"
	expectState        expectedMember = "state"
	expectPattern      expectedMember = "pattern"
	expectRange        expectedMember = "range"
	expectTolerance    expectedMember = "tolerance"
	expectWindow       expectedMember = "window"
	expectHolds        expectedMember = "holds"
	expectCount        expectedMember = "count"
	expectKeys         expectedMember = "keys"
	expectMultiplicity expectedMember = "multiplicity"
	expectChange       expectedMember = "change"
)

// compatibility is the one table binding an operator to the subject it reads
// and the expectation it takes. An operator absent from it is not an
// operator, and a document pairing one with any other subject or expectation
// is refused when it is read rather than discovered while it is evaluated.
var compatibility = map[Operator]struct {
	subject  subjectKind
	expected expectedMember
}{
	FieldEquals:        {subjectField, expectField},
	FieldNotEquals:     {subjectField, expectField},
	FieldState:         {subjectField, expectState},
	TextMatches:        {subjectField, expectPattern},
	NumericRange:       {subjectField, expectRange},
	NumericTolerance:   {subjectField, expectTolerance},
	DateWindow:         {subjectField, expectWindow},
	ValuesEqual:        {subjectPair, expectHolds},
	RecordCount:        {subjectCollection, expectCount},
	RecordsUnique:      {subjectCollection, expectHolds},
	RecordsContain:     {subjectCollection, expectKeys},
	RecordsOrdered:     {subjectCollection, expectKeys},
	RecordMultiplicity: {subjectCollection, expectMultiplicity},
	RecordsAbsent:      {subjectCollection, expectHolds},
	RecordKeyMatches:   {subjectEach, expectPattern},
	RecordsChanged:     {subjectTransition, expectChange},
}

var (
	assertionID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	messageKey  = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)
	// decimal admits any precision; maxNumberBytes is the only bound, so a
	// literal is never refused for a reason the documentation does not state.
	decimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

// Decode reads one assertion set exactly as written and refuses every
// document it could not stand behind: an oversized document, unknown members
// at any nesting, a contract this release does not read, a duplicate or
// malformed assertion id, an operator paired with a subject or expectation it
// does not take, a selector outside the shared grammar, and any bound an
// accepted set is held to.
func Decode(data []byte) (Set, error) {
	if len(data) > MaxSetBytes {
		return Set{}, errors.New("an assertion set exceeds its 256 KiB size limit")
	}
	var required struct {
		Schema     *string      `json:"schema"`
		Name       *string      `json:"name"`
		Assertions *[]Assertion `json:"assertions"`
	}
	if err := json.Unmarshal(data, &required); err != nil {
		return Set{}, errors.New("invalid assertion set JSON")
	}
	if required.Schema == nil || *required.Schema != Schema {
		return Set{}, errors.New("an assertion set must declare " + Schema)
	}
	if required.Name == nil || required.Assertions == nil {
		return Set{}, errors.New("an assertion set requires name and assertions")
	}
	var set Set
	if err := json.Unmarshal(data, &set, json.RejectUnknownMembers(true)); err != nil {
		return Set{}, errors.New("an assertion set declares no member beyond schema, name and assertions")
	}
	if err := validate(&set); err != nil {
		return Set{}, err
	}
	set.decoded = true
	return set, nil
}

func validate(set *Set) error {
	if !readableText(set.Name, maxNameBytes) {
		return errors.New("an assertion set name is one nonempty line of at most 256 readable UTF-8 bytes")
	}
	if len(set.Assertions) == 0 || len(set.Assertions) > MaxAssertions {
		return errors.New("an assertion set holds between 1 and 256 assertions")
	}
	ids := make(map[string]bool, len(set.Assertions))
	for i := range set.Assertions {
		assertion := &set.Assertions[i]
		if !assertionID.MatchString(assertion.ID) {
			return errors.New("an assertion id begins with a lowercase letter and holds lowercase letters, digits and '-' only, at most 64 bytes")
		}
		if ids[assertion.ID] {
			return errors.New("an assertion set declares one assertion id twice")
		}
		ids[assertion.ID] = true
		if err := validateAssertion(assertion); err != nil {
			return err
		}
	}
	return nil
}

func validateAssertion(assertion *Assertion) error {
	pairing, ok := compatibility[assertion.Operator]
	if !ok {
		return errors.New("an assertion names one of the operators this contract carries")
	}
	kind, err := assertion.Subject.kind()
	if err != nil {
		return err
	}
	if kind != pairing.subject {
		return errors.New("the operator " + string(assertion.Operator) + " reads a " + string(pairing.subject) + " subject")
	}
	member, err := assertion.Expected.member()
	if err != nil {
		return err
	}
	if member != pairing.expected {
		return errors.New("the operator " + string(assertion.Operator) + " takes the expectation " + string(pairing.expected))
	}
	if err := validateSubject(assertion.Subject); err != nil {
		return err
	}
	if assertion.When != nil {
		if err := validateFieldRef(assertion.When.Field); err != nil {
			return err
		}
		if err := validateFieldValue(assertion.When.Equals); err != nil {
			return err
		}
	}
	return validateExpected(assertion)
}

func validateSubject(subject Subject) error {
	switch {
	case subject.Field != nil:
		return validateFieldRef(*subject.Field)
	case subject.Pair != nil:
		if err := validateFieldRef(subject.Pair.Left); err != nil {
			return err
		}
		return validateFieldRef(subject.Pair.Right)
	case subject.Collection != nil:
		return validateRecordScope(subject.Collection.Scope)
	case subject.Each != nil:
		if err := validateRecordScope(subject.Each.Scope); err != nil {
			return err
		}
		switch subject.Each.Quantifier {
		case QuantifierEvery, QuantifierAny, QuantifierNone:
			return nil
		}
		return errors.New("a quantifier is every, any or none")
	case subject.Transition != nil:
		if err := validateRecordScope(subject.Transition.From); err != nil {
			return err
		}
		if err := validateRecordScope(subject.Transition.To); err != nil {
			return err
		}
		if subject.Transition.From == subject.Transition.To {
			return errors.New("a transition names two different observations")
		}
		return nil
	default:
		return errors.New("a subject names exactly one of field, pair, collection, each or transition")
	}
}

func validateFieldRef(ref FieldRef) error {
	if ref.Scope != ObservedMessages && ref.Scope != InputMessages {
		return errors.New("a field reference names the observed or the input scope")
	}
	if !messageKey.MatchString(ref.Message) {
		return errors.New("a field reference names one message occurrence id")
	}
	if ref.selector.String() == "" {
		return errors.New("a field reference carries one selector of the shared grammar")
	}
	return nil
}

func validateRecordScope(scope RecordScope) error {
	if scope != BeforeRecords && scope != AfterRecords {
		return errors.New("a collection reference names the before or the after observation")
	}
	return nil
}

func validState(state hl7.State) bool {
	return state == hl7.Present || state == hl7.Empty || state == hl7.Null || state == hl7.Omitted
}

func validateFieldValue(value FieldValue) error {
	switch value.State {
	case hl7.Present:
		if value.Text == nil || *value.Text == "" || len(*value.Text) > maxTextBytes || !utf8.ValidString(*value.Text) {
			return errors.New("a present value carries nonempty valid UTF-8 text of at most 65536 bytes")
		}
	case hl7.Empty, hl7.Null, hl7.Omitted:
		if value.Text != nil {
			return errors.New("only a present value carries text")
		}
	default:
		return errors.New("a value state is present, empty, null or omitted")
	}
	return nil
}

func validateExpected(assertion *Assertion) error {
	expected := assertion.Expected
	switch {
	case expected.Field != nil:
		return validateFieldValue(*expected.Field)
	case expected.State != nil:
		// A shape expectation names a state and nothing else: "present" here
		// asks whether the field is present, not what text it carries.
		if !validState(*expected.State) {
			return errors.New("a value state is present, empty, null or omitted")
		}
		return nil
	case expected.Pattern != nil:
		pattern, err := compilePattern(*expected.Pattern)
		if err != nil {
			return err
		}
		assertion.parsed.pattern = pattern
		return nil
	case expected.Range != nil:
		minimum, ok := number(expected.Range.Min)
		maximum, okMaximum := number(expected.Range.Max)
		if !ok || !okMaximum {
			return errors.New("a range bound is one decimal literal of at most 32 bytes")
		}
		if minimum.Cmp(maximum) > 0 {
			return errors.New("a range's minimum is not above its maximum")
		}
		assertion.parsed.minimum, assertion.parsed.maximum = minimum, maximum
		return nil
	case expected.Tolerance != nil:
		value, ok := number(expected.Tolerance.Value)
		if !ok {
			return errors.New("a tolerance's value is one decimal literal of at most 32 bytes")
		}
		allowed, ok := number(expected.Tolerance.Tolerance)
		if !ok || allowed.Sign() < 0 {
			return errors.New("a tolerance is one decimal literal of at most 32 bytes and is not negative")
		}
		assertion.parsed.expected, assertion.parsed.allowed = value, allowed
		return nil
	case expected.Window != nil:
		from, ok := instant(expected.Window.From)
		to, okTo := instant(expected.Window.To)
		if !ok || !okTo {
			return errors.New("a window bound is one HL7 timestamp of whole seconds with an explicit numeric offset")
		}
		if from.After(to) {
			return errors.New("a window's start is not after its end")
		}
		assertion.parsed.from, assertion.parsed.to = from, to
		return nil
	case expected.Count != nil:
		if *expected.Count < 0 || *expected.Count > maxRecords {
			return errors.New("an expected count is between 0 and 1048576")
		}
		return nil
	case expected.Keys != nil:
		return validateKeys(*expected.Keys)
	case expected.Multiplicity != nil:
		if !recordKey(expected.Multiplicity.Key) {
			return errors.New("a record key is one nonempty line of at most 128 readable UTF-8 bytes")
		}
		if expected.Multiplicity.Count < 0 || expected.Multiplicity.Count > maxRecords {
			return errors.New("an expected multiplicity is between 0 and 1048576")
		}
		return nil
	case expected.Change != nil:
		if expected.Change.Added < 0 || expected.Change.Added > maxRecords || expected.Change.Removed < 0 || expected.Change.Removed > maxRecords {
			return errors.New("an expected change counts between 0 and 1048576 added and removed keys")
		}
		return nil
	default:
		// holds is the one expectation that is a boolean. There is nothing
		// further to bound about it.
		return nil
	}
}

func validateKeys(keys []string) error {
	if len(keys) == 0 || len(keys) > maxKeys {
		return errors.New("an assertion declares between 1 and 512 record keys")
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !recordKey(key) {
			return errors.New("a record key is one nonempty line of at most 128 readable UTF-8 bytes")
		}
		if seen[key] {
			return errors.New("an assertion declares one record key twice")
		}
		seen[key] = true
	}
	return nil
}

// compilePattern bounds the regular-expression work an accepted set can ask
// for: the source is bounded and readable, and the expression is compiled
// once, here, so a document that cannot be compiled is refused when it is
// read. Go's RE2 engine has no backtracking, so matching is linear in the
// input, and MaxMatchBytes bounds that input separately.
func compilePattern(source string) (*regexp.Regexp, error) {
	if !readableText(source, maxPatternBytes) {
		return nil, errors.New("a pattern is one nonempty line of at most 256 readable UTF-8 bytes")
	}
	pattern, err := regexp.Compile(source)
	if err != nil {
		return nil, errors.New("a pattern is one regular expression this engine compiles within its own limits")
	}
	return pattern, nil
}

func recordKey(key string) bool { return readableText(key, maxKeyBytes) }

// readableText bounds what a member of a set may say to the person reading
// it: nonempty valid UTF-8 within the limit, with no control character at
// all, because a document somebody imported must not be able to drive the
// terminal it is displayed on.
func readableText(text string, limit int) bool {
	if text == "" || len(text) > limit || strings.TrimSpace(text) == "" || !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
