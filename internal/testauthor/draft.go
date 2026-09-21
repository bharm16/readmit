// Package testauthor builds a regression test out of evidence a person
// selected, without anyone opening a document in a text editor.
//
// A draft is answered one stage at a time — what the test is called, which
// occurrences of the verified case it sends, which target it sends them to,
// which boundary decides the outcome, where that observation is read from, how
// the fixture is reset, and what the run should have produced. Each answer is a
// typed operator this package interprets ([ADR-0003]), not a fragment of a
// document: an answer a stage does not declare, and one the evidence or the
// boundary does not support, is refused by name and leaves the draft exactly as
// it was.
//
// What a completed draft generates is [testrunner] bytes: an ordinary
// readmit-test/v1 spec, accepted by the same reader `readmit test` runs it
// with. Nothing here executes a test, opens a connection or changes evidence.
//
// [ADR-0003]: docs/adr/0003-specs-are-strict-json-with-typed-operators.md
package testauthor

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Schema is the draft a guided authoring session holds. It is not a test: it
// is what has been answered so far, bound to the evidence it was answered
// against, and it is read strictly so a stage nobody answered stays unanswered
// rather than becoming a likely value.
const Schema = "readmit-test-draft/v1"

// The stages of the flow, in the order they are asked. The order is not a
// preference: a boundary decides whether an observation source is read at all
// and which expectations a test can make, so it is answered before both.
const (
	// StageName is the local description the spec carries.
	StageName = "name"

	// StageMessages is the case input: the occurrences of the verified case
	// this test sends, in the order the case records them.
	StageMessages = "messages"

	// StageTarget is the explicit readmit-target configuration the run is
	// pointed at. There is no implicit target and no discovered endpoint.
	StageTarget = "target"

	// StageBoundary is the outcome boundary: what deciding this test means.
	StageBoundary = "boundary"

	// StageObservation is where the ledger boundary reads its observation
	// from. The ACK boundary observes correlated acknowledgements and reads no
	// observation document, so this stage does not apply to it.
	StageObservation = "observation"

	// StageReset is the operator-readable prose that returns the fixture to
	// the declared initial state. A spec names no reset action, plan, command
	// or hook, and nothing here is ever executed.
	StageReset = "reset"

	// StageExpectations is what the run should have produced.
	StageExpectations = "expectations"
)

// stageRule binds one stage to everything the flow knows about it: the answer
// member it declares, whether a draft has answered it, and what to say when it
// has not. One table owns all of that, so the order the flow asks its questions
// in, what counts as an answer and the sentence a person is shown are decided in
// one place rather than in a switch each.
type stageRule struct {
	name       string
	declared   func(Answer) bool
	answered   func(Draft) bool
	unanswered string
}

var stageRules = []stageRule{
	{StageName,
		func(a Answer) bool { return a.Name != "" },
		func(d Draft) bool { return d.Name != "" },
		"this test has no name yet"},
	{StageMessages,
		func(a Answer) bool { return len(a.Messages) != 0 },
		func(d Draft) bool { return len(d.Messages) != 0 },
		"this test sends no occurrence of the case yet"},
	{StageTarget,
		func(a Answer) bool { return a.Target != "" },
		func(d Draft) bool { return d.Target != "" },
		"this test names no target configuration yet"},
	{StageBoundary,
		func(a Answer) bool { return a.Boundary != "" },
		func(d Draft) bool { return d.Boundary != "" },
		"this test declares no outcome boundary yet"},
	// The ACK boundary observes correlated acknowledgements and reads no
	// document, so this stage is answered by not applying to it.
	{StageObservation,
		func(a Answer) bool { return a.Observation != "" },
		func(d Draft) bool { return d.Boundary == testrunner.ACKBoundary || d.Observation != "" },
		"the appointment-ledger boundary reads an observation document, and this test names none yet"},
	{StageReset,
		func(a Answer) bool { return a.Reset != "" },
		func(d Draft) bool { return d.Reset != "" },
		"this test states no reset instructions yet; they are prose an operator reads and readmit never executes"},
	// A test at the ledger boundary decides on the ledger it observed, so
	// stating only what an acknowledgement said leaves that boundary undecided;
	// readmit-test/v1 refuses such a spec, and so does this.
	{StageExpectations,
		func(a Answer) bool { return len(a.Expectations) != 0 },
		func(d Draft) bool {
			if len(d.Expectations) == 0 {
				return false
			}
			if d.Boundary != testrunner.LedgerBoundary {
				return true
			}
			return slices.ContainsFunc(d.Expectations, func(e Expectation) bool {
				return e.Operator == LedgerCount || e.Operator == LedgerEquals
			})
		},
		"this test expects nothing of the run yet"},
}

// The typed expectations this flow authors. They are readmit-test/v1's own
// operators, because a test this flow saves is a test `readmit test` executes.
const (
	// LedgerCount expects the observed ledger to hold exactly N records.
	LedgerCount = "ledger_count"

	// LedgerEquals expects the observed ledger to hold exactly these records
	// in order, including identifiers and appointment start.
	LedgerEquals = "ledger_equals"

	// ACKFieldEquals expects one MSA or ERR position of the acknowledgement
	// correlated to one sent message to hold exactly one value.
	ACKFieldEquals = "ack_field_equals"
)

// Bounds. A draft past one of these is refused, never truncated. Each is the
// bound readmit-test/v1 already holds the member it generates to, so a draft
// cannot be answered into a spec its own reader would refuse.
const (
	MaxDraftBytes    = 256 << 10
	MaxNameBytes     = 256
	MaxEntryBytes    = 255
	MaxResetBytes    = 8192
	MaxMessages      = replay.MaxMessages
	MaxExpectations  = testrunner.MaxAssertions
	MaxExpectedBytes = testrunner.MaxExpectedTextBytes
	MaxCount         = observation.MaxOccurrences
)

var (
	occurrencePattern  = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)
	identityPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	expectationPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

// Evidence is the case a draft is bound to: the entry of the workspace that
// holds it, and the identity the shared reader verified. A draft answered
// against one case is never generated against another.
type Evidence struct {
	Entry    string `json:"entry"`
	Identity string `json:"identity"`
}

// Expectation is one typed expectation. Only the members its operator declares
// may be present; the rest are refused rather than ignored, so an expectation
// always means exactly one thing.
type Expectation struct {
	ID       string                 `json:"id"`
	Operator string                 `json:"operator"`
	Count    *int                   `json:"count,omitzero"`
	Records  *[]observation.Record  `json:"records,omitzero"`
	Message  string                 `json:"message,omitzero"`
	Selector string                 `json:"selector,omitzero"`
	Field    *testrunner.FieldValue `json:"field,omitzero"`
}

// Draft is one authoring session. Every member is declared, because a stage
// nobody has answered is an empty answer rather than an absent one: the flow
// reports which stages are still missing instead of inferring them.
type Draft struct {
	Schema       string        `json:"schema"`
	Case         Evidence      `json:"case"`
	Name         string        `json:"name"`
	Messages     []string      `json:"messages"`
	Target       string        `json:"target"`
	Boundary     string        `json:"boundary"`
	Observation  string        `json:"observation"`
	Reset        string        `json:"reset"`
	Expectations []Expectation `json:"expectations"`
}

// Answer is one typed answer to one stage. It carries the member its own stage
// declares and no other, and it replaces that stage's answer rather than adding
// to it, so changing an answer and correcting a mistake are the same operation.
type Answer struct {
	Stage        string        `json:"stage"`
	Name         string        `json:"name,omitzero"`
	Messages     []string      `json:"messages,omitzero"`
	Target       string        `json:"target,omitzero"`
	Boundary     string        `json:"boundary,omitzero"`
	Observation  string        `json:"observation,omitzero"`
	Reset        string        `json:"reset,omitzero"`
	Expectations []Expectation `json:"expectations,omitzero"`
}

// NewDraft begins a draft bound to one verified case. The entry is the name
// the generated spec will resolve its input by, and the identity is what the
// shared reader verified, so a draft carries both rather than trusting a name
// to still mean the same evidence.
func NewDraft(entry, identity string) (Draft, error) {
	if artifactpath.EntryName(entry) != nil || !printable(entry, MaxEntryBytes) {
		return Draft{}, errors.New("a draft names the case by one entry of the open workspace")
	}
	if !identityPattern.MatchString(identity) {
		return Draft{}, errors.New("a draft names the verified identity of the case it is authored against")
	}
	return Draft{Schema: Schema, Case: Evidence{Entry: entry, Identity: identity}, Messages: []string{}, Expectations: []Expectation{}}, nil
}

// Answer replaces one stage's answer and reports the draft that results.
//
// The whole draft is checked afterwards, so an answer that would contradict
// another one — dropping a message an expectation reads, or changing the
// boundary under expectations that boundary cannot make — is refused and the
// draft is left exactly as it was. A half-answered draft is a state this flow
// has; a contradictory one is not.
func (draft Draft) Answer(answer Answer) (Draft, error) {
	if err := check(draft); err != nil {
		return Draft{}, err
	}
	invalid := errors.New("an answer carries only the member its own stage declares")
	switch answer.Stage {
	case StageName:
		if answer.Name == "" || !only(answer, StageName) {
			return Draft{}, invalid
		}
		draft.Name = answer.Name
	case StageMessages:
		// An empty list is how the last selection is withdrawn, which is a
		// state the flow already has: a draft nobody has answered selects
		// nothing. What that must not do is leave an expectation naming a
		// message the test no longer sends, and check refuses exactly that.
		if !only(answer, StageMessages) {
			return Draft{}, invalid
		}
		draft.Messages = slices.Clone(answer.Messages)
	case StageTarget:
		if answer.Target == "" || !only(answer, StageTarget) {
			return Draft{}, invalid
		}
		draft.Target = answer.Target
	case StageBoundary:
		if answer.Boundary == "" || !only(answer, StageBoundary) {
			return Draft{}, invalid
		}
		draft.Boundary = answer.Boundary
		// The ACK boundary reads no observation document, so choosing it
		// withdraws an observation source a ledger boundary had asked for
		// rather than generating a spec its reader refuses.
		if answer.Boundary == testrunner.ACKBoundary {
			draft.Observation = ""
		}
	case StageObservation:
		if answer.Observation == "" || !only(answer, StageObservation) {
			return Draft{}, invalid
		}
		draft.Observation = answer.Observation
	case StageReset:
		if answer.Reset == "" || !only(answer, StageReset) {
			return Draft{}, invalid
		}
		draft.Reset = answer.Reset
	case StageExpectations:
		// Removing the last expectation is the same operation, for the same
		// reason: a test that expects nothing yet is on the way to one.
		if !only(answer, StageExpectations) {
			return Draft{}, invalid
		}
		draft.Expectations = slices.Clone(answer.Expectations)
	default:
		return Draft{}, errors.New("this flow does not ask that stage")
	}
	if err := check(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

// only reports whether an answer carries nothing but the member of its stage.
// An answer that could mean two things is an answer nobody can read back.
func only(answer Answer, stage string) bool {
	for _, rule := range stageRules {
		if rule.name != stage && rule.declared(answer) {
			return false
		}
	}
	return true
}

// DecodeDraft reads a draft. Unknown members and unknown versions are errors;
// there is no migration and no repair.
func DecodeDraft(data []byte) (Draft, error) {
	if len(data) > MaxDraftBytes {
		return Draft{}, errors.New("test draft exceeds its size limit")
	}
	// The declared contract version is read before the strict decode, so a
	// draft written under a later version is reported as one this release does
	// not read rather than as an invalid document.
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Draft{}, errors.New("invalid test draft")
	}
	if declared.Schema != Schema {
		return Draft{}, errors.New("test draft declares a contract version this release does not read")
	}
	var present struct {
		Case         *Evidence      `json:"case"`
		Name         *string        `json:"name"`
		Messages     *[]string      `json:"messages"`
		Target       *string        `json:"target"`
		Boundary     *string        `json:"boundary"`
		Observation  *string        `json:"observation"`
		Reset        *string        `json:"reset"`
		Expectations *[]Expectation `json:"expectations"`
	}
	if json.Unmarshal(data, &present) != nil || present.Case == nil || present.Name == nil || present.Messages == nil ||
		present.Target == nil || present.Boundary == nil || present.Observation == nil || present.Reset == nil || present.Expectations == nil {
		return Draft{}, errors.New("a test draft declares every stage, answered or not")
	}
	var draft Draft
	if json.Unmarshal(data, &draft, json.RejectUnknownMembers(true)) != nil {
		return Draft{}, errors.New("invalid test draft")
	}
	if err := check(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

// check holds a draft to everything that is true of it whether or not it is
// finished. Completeness is Generate's question; this one is consistency.
func check(draft Draft) error {
	if draft.Schema != Schema {
		return errors.New("test draft declares a contract version this release does not read")
	}
	if artifactpath.EntryName(draft.Case.Entry) != nil || !printable(draft.Case.Entry, MaxEntryBytes) || !identityPattern.MatchString(draft.Case.Identity) {
		return errors.New("a draft names one entry of the open workspace and the verified identity of the case in it")
	}
	if draft.Name != "" && !printable(draft.Name, MaxNameBytes) {
		return errors.New("a test name is 1 to 256 bytes of printable text")
	}
	if err := checkMessages(draft.Messages); err != nil {
		return err
	}
	if draft.Target != "" && (artifactpath.EntryName(draft.Target) != nil || !printable(draft.Target, MaxEntryBytes)) {
		return errors.New("a target is named by one entry of the open workspace")
	}
	if draft.Boundary != "" && draft.Boundary != testrunner.LedgerBoundary && draft.Boundary != testrunner.ACKBoundary {
		return errors.New("this release decides a test at the appointment-ledger or the ack-contract boundary")
	}
	if draft.Observation != "" {
		if draft.Boundary != testrunner.LedgerBoundary {
			return errors.New("only the appointment-ledger boundary reads an observation document; the ack-contract boundary observes correlated acknowledgements")
		}
		if artifactpath.EntryName(draft.Observation) != nil || !printable(draft.Observation, MaxEntryBytes) {
			return errors.New("an observation source is named by one entry of the open workspace")
		}
	}
	if draft.Reset != "" && !printable(draft.Reset, MaxResetBytes) {
		return errors.New("reset instructions are 1 to 8192 bytes of printable text")
	}
	return checkExpectations(draft)
}

func checkMessages(messages []string) error {
	if len(messages) > MaxMessages {
		return errors.New("a test sends at most 4000 occurrences of one case")
	}
	seen := make(map[string]bool, len(messages))
	for _, id := range messages {
		if !occurrencePattern.MatchString(id) || seen[id] {
			return errors.New("a selected message is one occurrence of the case, named once")
		}
		seen[id] = true
	}
	return nil
}

// checkExpectations holds every expectation to its own operator and to the two
// answers it depends on: the boundary that decides whether the expectation can
// be made at all, and the messages an acknowledgement expectation reads.
func checkExpectations(draft Draft) error {
	if len(draft.Expectations) > MaxExpectations {
		return errors.New("a test holds at most 256 expectations")
	}
	if len(draft.Expectations) > 0 && draft.Boundary == "" {
		return errors.New("the outcome boundary decides which expectations a test can make; answer it before them")
	}
	sent := make(map[string]bool, len(draft.Messages))
	for _, id := range draft.Messages {
		sent[id] = true
	}
	ids := make(map[string]bool, len(draft.Expectations))
	for _, expectation := range draft.Expectations {
		if !expectationPattern.MatchString(expectation.ID) || ids[expectation.ID] {
			return errors.New("an expectation is named once, by lowercase letters, digits and hyphens beginning with a letter")
		}
		ids[expectation.ID] = true
		if err := checkExpectation(draft, expectation, sent); err != nil {
			return err
		}
	}
	return nil
}

func checkExpectation(draft Draft, expectation Expectation, sent map[string]bool) error {
	invalid := errors.New("an expectation carries only the members its own operator declares")
	switch expectation.Operator {
	case LedgerCount:
		if expectation.Message != "" || expectation.Selector != "" || expectation.Field != nil || expectation.Records != nil {
			return invalid
		}
		if draft.Boundary != testrunner.LedgerBoundary {
			return errors.New("a record count is an expectation of the appointment-ledger boundary; the ack-contract boundary makes no ledger claim")
		}
		if expectation.Count == nil || *expectation.Count < 0 || *expectation.Count > MaxCount {
			return errors.New("an expected record count is a whole number this observation contract can hold")
		}
	case LedgerEquals:
		if expectation.Message != "" || expectation.Selector != "" || expectation.Field != nil || expectation.Count != nil {
			return invalid
		}
		if draft.Boundary != testrunner.LedgerBoundary {
			return errors.New("an exact ledger is an expectation of the appointment-ledger boundary; the ack-contract boundary makes no ledger claim")
		}
		if expectation.Records == nil {
			return errors.New("an exact ledger expectation declares the records the observation should hold")
		}
		snapshot := observation.Snapshot{
			Schema: observation.Schema, Profile: observation.Profile,
			SessionID: strings.Repeat("0", 32), Mode: observation.Fixed, Consistent: true,
			Processed: []observation.Occurrence{}, Records: *expectation.Records,
		}
		if snapshot.Validate() != nil {
			return errors.New("an exact ledger expectation holds records this observation contract accepts")
		}
	case ACKFieldEquals:
		if expectation.Count != nil || expectation.Records != nil {
			return invalid
		}
		if !sent[expectation.Message] {
			return errors.New("an acknowledgement expectation reads the acknowledgement of a message this test sends")
		}
		selector, err := hl7.ParseSelector(expectation.Selector)
		if err != nil {
			return errors.New("an acknowledgement position is not one this release addresses")
		}
		if !strings.HasPrefix(selector.String(), "MSA[") && !strings.HasPrefix(selector.String(), "ERR[") {
			return errors.New("an acknowledgement expectation addresses an MSA or ERR position")
		}
		if err := checkField(expectation.Field); err != nil {
			return err
		}
	default:
		return errors.New("this flow does not author that expectation")
	}
	return nil
}

// checkField holds an expected value to the four states the shared selector
// returns. The rule is readmit-test/v1's own, applied by the reader that
// executes the spec rather than by a second copy of it here; only the sentence
// is this flow's, because a person is being told what to answer.
func checkField(field *testrunner.FieldValue) error {
	if field == nil {
		return errors.New("an acknowledgement expectation declares the value the position should hold")
	}
	if field.Validate() != nil {
		return errors.New("a value is present, empty, null or omitted, and only a present one carries 1 to 65536 bytes of valid UTF-8")
	}
	return nil
}

// printable accepts bounded text a person typed. It is the rule the shell holds
// its own local documents to, for the same reason: control characters are
// refused rather than escaped, so nothing a draft carries can rewrite a line it
// is displayed on.
func printable(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
