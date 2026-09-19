package testauthor

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// What proposing one expectation from a run came to. A proposal nobody could
// justify from the run it was asked about is named as such rather than dropped
// from the set or carried as though the run supported it.
const (
	// Supported is a proposal the reviewed run's own evidence justifies. It is
	// not an approval: nothing here decides that the value should be expected,
	// only that this run produced it.
	Supported = "supported"

	// Unsupported is a proposal that could not be justified, carrying the
	// reason it could not. It states no expected value and cannot be approved.
	Unsupported = "unsupported"
)

// What one reviewer's act on one suggestion came to. A suggestion nobody
// decided is neither an approval nor a refusal, and it is reported as its own
// outcome: a flow whose default is acceptance is not a review.
const (
	// Approved is a suggestion a person approved into the draft.
	Approved = "approved"

	// Rejected is a suggestion a person refused.
	Rejected = "rejected"

	// NotReviewed is a suggestion the review said nothing about.
	NotReviewed = "not_reviewed"
)

// MaxPositions bounds the acknowledgement positions one request proposes an
// expectation for. Every position is proposed for every message the test sends,
// so the whole set is additionally held to the expectations a test can hold.
const MaxPositions = 16

// position is one acknowledgement position of a request: the selector a value
// is read with, and the spelling an expectation records it as. The two are kept
// together because a position is recorded as it was written and never
// canonicalised.
type position struct {
	text     string
	selector hl7.Selector
}

// Origin is the run one suggestion set was derived from: what it decided, and
// the evidence it decided over. Every member of it is a retained artifact or
// the identity of one, so a reviewer can follow a suggestion back to what was
// read without this package restating what is in it.
type Origin struct {
	// Result is the entry of the open workspace holding the run result, and
	// Identity is the identity that result's own reader records for it.
	Result   string `json:"result"`
	Identity string `json:"identity"`
	// Status is the verdict the result records and Boundary is what the run
	// decided, both restated rather than recomputed here.
	Status   string `json:"status"`
	Boundary string `json:"boundary"`
	// SpecIdentity, InputIdentity, RunIdentity and TargetIdentity name the spec
	// that was executed, the case it replayed, the replay evidence it produced
	// and the configuration it ran against.
	SpecIdentity   string `json:"spec_identity"`
	InputIdentity  string `json:"input_identity"`
	RunIdentity    string `json:"run_identity"`
	TargetIdentity string `json:"target_identity"`
}

// Link names where one suggested value was read, as a position in retained
// evidence rather than as a value. A field link names the occurrence, the
// payload file inside the run bundle and the position it was read at; a ledger
// link names the observation document the run retained and its digest.
type Link struct {
	Artifact string `json:"artifact"`
	Payload  string `json:"payload,omitzero"`
	Message  string `json:"message,omitzero"`
	Selector string `json:"selector,omitzero"`
	Digest   string `json:"digest,omitzero"`
}

// Suggestion is one proposed expectation: a claim about what should be true,
// derived from what was true once. It carries where it was read from, and it
// becomes an expectation only when a person approves it.
//
// A supported suggestion carries exactly the members the expectation it
// proposes would carry and nothing else — a record count, or one value at one
// acknowledgement position. An unsupported one carries neither, and says why.
type Suggestion struct {
	ID       string                 `json:"id"`
	Operator string                 `json:"operator"`
	Outcome  string                 `json:"outcome"`
	Reason   string                 `json:"reason,omitzero"`
	Message  string                 `json:"message,omitzero"`
	Selector string                 `json:"selector,omitzero"`
	Count    *int                   `json:"count,omitzero"`
	Field    *testrunner.FieldValue `json:"field,omitzero"`
	Evidence Link                   `json:"evidence"`
}

// Suggestions is one set of proposals and the run they came from. The two
// counts say how the set divides, so a reviewer reading a short list of usable
// proposals knows the rest was refused rather than never asked for.
type Suggestions struct {
	Origin           Origin       `json:"origin"`
	Suggestions      []Suggestion `json:"suggestions"`
	SupportedCount   int          `json:"supported"`
	UnsupportedCount int          `json:"unsupported"`
}

// SuggestionRequest is what a person asked to have proposed: the reviewed run,
// whether the ledger it settled on should be proposed as a record count, and
// the acknowledgement positions to propose a value for. Every position is
// proposed for every message the draft sends, in the order the case records
// them.
type SuggestionRequest struct {
	Result    string   `json:"result"`
	Ledger    bool     `json:"ledger"`
	Positions []string `json:"positions,omitzero"`
}

// Decision is one reviewer's act on one suggestion, naming it by the identifier
// the set gave it. Approving is the only thing that puts an expectation in a
// draft, and it is never the default: a suggestion no decision names is not
// approved.
//
// ID, Count and Field are the edits a reviewer may make while approving. Each
// is a member the suggestion's own operator declares, so an edit corrects what
// a run produced without turning one expectation into another.
type Decision struct {
	Suggestion string                 `json:"suggestion"`
	Approved   bool                   `json:"approved"`
	ID         string                 `json:"id,omitzero"`
	Count      *int                   `json:"count,omitzero"`
	Field      *testrunner.FieldValue `json:"field,omitzero"`
}

// Review is what a person decided about one suggestion set. It names the set it
// was made against, so decisions taken over one run are never applied to
// proposals derived from another.
type Review struct {
	Result    string     `json:"result"`
	Identity  string     `json:"identity"`
	Decisions []Decision `json:"decisions"`
}

// Reviewed is what became of one suggestion under review, and the identifier
// the draft records an approved one under.
type Reviewed struct {
	Suggestion  string `json:"suggestion"`
	Outcome     string `json:"outcome"`
	Expectation string `json:"expectation,omitzero"`
}

// Approval is the record of one person's act, separate from the generating that
// proposed the suggestions. Every suggestion of the set appears in it exactly
// once, so what was refused and what was never looked at are as visible as what
// was approved.
type Approval struct {
	Result           string     `json:"result"`
	Identity         string     `json:"identity"`
	Reviewed         []Reviewed `json:"reviewed"`
	ApprovedCount    int        `json:"approved"`
	RejectedCount    int        `json:"rejected"`
	NotReviewedCount int        `json:"not_reviewed"`
}

// LedgerCoverage is whether anything in this draft decides the observed ledger.
// Applies is false at the ack-contract boundary, which makes no ledger claim at
// all, so an uncovered ledger there is not a gap.
type LedgerCoverage struct {
	Applies     bool   `json:"applies"`
	Covered     bool   `json:"covered"`
	Expectation string `json:"expectation,omitzero"`
}

// MessageCoverage is what this draft decides about one message it sends:
// the acknowledgement positions its expectations address, as those expectations
// spell them. A message with none is one the test sends and asserts nothing
// about.
type MessageCoverage struct {
	Message   string   `json:"message"`
	Positions []string `json:"positions"`
}

// Coverage is what a draft's expectations decide and what they leave undecided.
// It reports positions, never values: what a test inspects is a place in an
// acknowledgement and a count of records.
type Coverage struct {
	Ledger   LedgerCoverage    `json:"ledger"`
	Messages []MessageCoverage `json:"messages"`
	// Uncovered names the messages this test sends and decides nothing about. A
	// ledger nothing decides is reported on the ledger itself.
	Uncovered []string `json:"uncovered"`
}

// Cover reports what a draft's expectations decide and what they leave
// undecided, over the messages it sends and the boundary it declared. It reads
// the draft alone: nothing is opened and no value is read.
func Cover(draft Draft) Coverage {
	coverage := Coverage{
		Ledger:    LedgerCoverage{Applies: draft.Boundary == testrunner.LedgerBoundary},
		Messages:  make([]MessageCoverage, 0, len(draft.Messages)),
		Uncovered: make([]string, 0, len(draft.Messages)),
	}
	addressed := make(map[string][]string, len(draft.Messages))
	for _, expectation := range draft.Expectations {
		switch expectation.Operator {
		case LedgerCount:
			if !coverage.Ledger.Covered {
				coverage.Ledger.Covered, coverage.Ledger.Expectation = true, expectation.ID
			}
		case ACKFieldEquals:
			addressed[expectation.Message] = append(addressed[expectation.Message], expectation.Selector)
		}
	}
	for _, id := range draft.Messages {
		positions := addressed[id]
		if positions == nil {
			positions = []string{}
			coverage.Uncovered = append(coverage.Uncovered, id)
		}
		coverage.Messages = append(coverage.Messages, MessageCoverage{Message: id, Positions: positions})
	}
	return coverage
}

// Suggest proposes expectations from one run somebody has already reviewed.
//
// It proposes; it never approves. Nothing here returns a draft, so there is no
// path from generating a suggestion to recording it as an expectation: that is
// [Approve], and it needs a decision. The run is opened through the reader
// `readmit test` verifies a result with, and every value is read exactly as the
// evaluator reads it, so a suggestion approved unedited is one the same run
// would decide again.
//
// A proposal this run does not justify is reported as unsupported with the
// reason, never omitted from the set and never carried as though it were.
func Suggest(root string, draft Draft, request SuggestionRequest) (Suggestions, error) {
	if err := check(draft); err != nil {
		return Suggestions{}, err
	}
	if draft.Boundary == "" {
		return Suggestions{}, errors.New("the outcome boundary decides which expectations a test can make; answer it before them")
	}
	if request.Ledger && draft.Boundary != testrunner.LedgerBoundary {
		return Suggestions{}, errors.New("a record count is an expectation of the appointment-ledger boundary; the ack-contract boundary makes no ledger claim")
	}
	positions, err := checkPositions(request.Positions)
	if err != nil {
		return Suggestions{}, err
	}
	if len(positions) > 0 && len(draft.Messages) == 0 {
		return Suggestions{}, errors.New("this test sends no occurrence of the case yet")
	}
	proposed := len(positions) * len(draft.Messages)
	if request.Ledger {
		proposed++
	}
	if proposed == 0 {
		return Suggestions{}, errors.New("a suggestion proposes the observed ledger, one acknowledgement position, or both")
	}
	// The bound is the one a test is held to, counting what the draft already
	// expects, so a set that could not be approved is refused before a run is
	// opened rather than after its values have been read.
	if proposed+len(draft.Expectations) > MaxExpectations {
		return Suggestions{}, errors.New("a review proposes at most 256 expectations, which is the most a test holds")
	}
	artifact, origin, err := knownGood(root, draft, request.Result)
	if err != nil {
		return Suggestions{}, err
	}
	taken := make(map[string]bool, len(draft.Expectations)+proposed)
	for _, expectation := range draft.Expectations {
		taken[expectation.ID] = true
	}
	set := Suggestions{Origin: origin, Suggestions: make([]Suggestion, 0, proposed)}
	if request.Ledger {
		set.Suggestions = append(set.Suggestions, ledgerSuggestion(artifact, origin, taken))
	}
	sent := acknowledged(artifact.Run)
	for _, id := range draft.Messages {
		for _, addressed := range positions {
			set.Suggestions = append(set.Suggestions, ackSuggestion(artifact.Run, sent[id], origin, taken, id, addressed))
		}
	}
	for _, suggestion := range set.Suggestions {
		if suggestion.Outcome == Supported {
			set.SupportedCount++
			continue
		}
		set.UnsupportedCount++
	}
	return set, nil
}

// knownGood opens the run a suggestion set is derived from and holds it to the
// draft it will be suggested into. Each refusal is a different question about
// the run, so each says which one.
func knownGood(root string, draft Draft, entry string) (*testrunner.Artifact, Origin, error) {
	if artifactpath.EntryName(entry) != nil || !printable(entry, MaxEntryBytes) {
		return nil, Origin{}, errors.New("a reviewed run is named by one entry of the open workspace")
	}
	path, err := artifactpath.Child(root, entry)
	if err != nil {
		return nil, Origin{}, errors.New("a reviewed run is one directory entry of the open workspace")
	}
	artifact, err := testrunner.Open(path)
	if err != nil {
		return nil, Origin{}, errors.New("that entry is not a run result this release verifies")
	}
	if artifact.Result.Status != testrunner.Pass {
		return nil, Origin{}, errors.New("expectations are suggested from a run whose own expectations held; this one did not")
	}
	if artifact.Run == nil {
		return nil, Origin{}, errors.New("that run retained no replay evidence to suggest an expectation from")
	}
	if artifact.Result.InputBundleIdentity != draft.Case.Identity {
		return nil, Origin{}, errors.New("that run replayed different evidence than the case this test is authored against")
	}
	if artifact.Result.ObservationBoundary != draft.Boundary {
		return nil, Origin{}, errors.New("that run was observed at a different boundary than this test declares")
	}
	origin := Origin{
		Result:         entry,
		Identity:       artifact.Identity,
		Status:         string(artifact.Result.Status),
		Boundary:       artifact.Result.ObservationBoundary,
		SpecIdentity:   artifact.Result.SpecIdentity,
		InputIdentity:  artifact.Result.InputBundleIdentity,
		TargetIdentity: artifact.Result.TargetIdentity,
	}
	if artifact.Result.Run != nil {
		origin.RunIdentity = artifact.Result.Run.Identity
	}
	return artifact, origin, nil
}

// checkPositions holds the requested positions to what readmit-test/v1
// addresses, before any evidence is opened: a position this release cannot
// address is a question about the request rather than about the run.
func checkPositions(requested []string) ([]position, error) {
	if len(requested) > MaxPositions {
		return nil, errors.New("a review proposes at most 16 acknowledgement positions")
	}
	positions := make([]position, 0, len(requested))
	for _, text := range requested {
		selector, err := hl7.ParseSelector(text)
		if err != nil {
			return nil, errors.New("an acknowledgement position is not one this release addresses")
		}
		if !strings.HasPrefix(selector.String(), "MSA[") && !strings.HasPrefix(selector.String(), "ERR[") {
			return nil, errors.New("an acknowledgement expectation addresses an MSA or ERR position")
		}
		// A position is proposed as it was written, because that is how the
		// expectation will spell it; two spellings of one position are two
		// proposals and only an identical one is a repeat.
		if slices.ContainsFunc(positions, func(p position) bool { return p.text == text }) {
			return nil, errors.New("an acknowledgement position is proposed once")
		}
		positions = append(positions, position{text: text, selector: selector})
	}
	return positions, nil
}

// acknowledged indexes a run by the occurrence each event sent, keeping the
// first. A run that sent one occurrence twice is one the result reader already
// refuses; keeping the first rather than the last means this never depends on
// which it was.
func acknowledged(run *replay.Run) map[string]*replay.Event {
	events := make(map[string]*replay.Event, len(run.Events))
	for i := range run.Events {
		if _, seen := events[run.Events[i].SourceOccurrence]; !seen {
			events[run.Events[i].SourceOccurrence] = &run.Events[i]
		}
	}
	return events
}

// ledgerSuggestion proposes the record count the reviewed run settled on. The
// count alone crosses: the records are the ledger's own source values, and what
// readmit-test/v1 expects of them is how many there are.
func ledgerSuggestion(artifact *testrunner.Artifact, origin Origin, taken map[string]bool) Suggestion {
	suggestion := Suggestion{
		ID:       identifier("ledger-records", taken),
		Operator: LedgerCount,
		Evidence: Link{Artifact: origin.Result},
	}
	if artifact.Result.FinalObservation != nil {
		suggestion.Evidence.Payload = artifact.Result.FinalObservation.Path
		suggestion.Evidence.Digest = artifact.Result.FinalObservation.SHA256
	}
	if artifact.FinalObservation == nil {
		return unsupported(suggestion, "that run retained no final observation of the ledger")
	}
	records := len(artifact.FinalObservation.Records)
	suggestion.Outcome, suggestion.Count = Supported, &records
	return suggestion
}

// ackSuggestion proposes the value one acknowledgement held at one position. It
// reads what the evaluator reads: the received payload the run retained,
// decoded as one message, at the position asked for. Anything it cannot read
// that way is unsupported rather than an absent value, because an
// acknowledgement nobody could decode is not evidence that a position was
// omitted.
func ackSuggestion(run *replay.Run, event *replay.Event, origin Origin, taken map[string]bool, message string, addressed position) Suggestion {
	suggestion := Suggestion{
		ID:       identifier("ack-"+message+"-"+addressed.text, taken),
		Operator: ACKFieldEquals,
		Message:  message,
		Selector: addressed.text,
		Evidence: Link{Artifact: origin.Result, Message: message, Selector: addressed.text},
	}
	if event == nil {
		return unsupported(suggestion, "that run did not send this occurrence")
	}
	suggestion.Evidence.Payload = event.Received.Path
	suggestion.Evidence.Digest = event.Received.SHA256
	if event.Delivery != "acknowledged" || event.ACK.Correlation != "matched" {
		return unsupported(suggestion, "that run recorded no matched acknowledgement for this occurrence")
	}
	// Raw holds the descriptor to the bytes the bundle retained, so a payload
	// the run does not hold is refused rather than read as an absent value.
	raw, err := run.Raw(event.Received)
	if err != nil {
		return unsupported(suggestion, "that run retained no acknowledgement payload for this occurrence")
	}
	document, err := hl7.Parse(raw, hl7.Options{Format: hl7.MLLP})
	if err != nil || len(document.Messages) != 1 {
		return unsupported(suggestion, "the acknowledgement this run retained did not decode as one message")
	}
	value, err := document.Select(0, addressed.selector)
	if err != nil {
		return unsupported(suggestion, "this acknowledgement does not address that position")
	}
	field := &testrunner.FieldValue{State: value.State}
	if value.State == hl7.Present {
		decoded, err := hl7.Decode(document.Bytes(value.Span), document.Messages[0].Delimiters)
		if err != nil || !utf8.Valid(decoded) {
			return unsupported(suggestion, "the value at that position is not text an expectation can state")
		}
		text := string(decoded)
		field.Text = &text
	}
	if field.Validate() != nil {
		return unsupported(suggestion, "the value at that position is not one an expectation can state")
	}
	suggestion.Outcome, suggestion.Field = Supported, field
	return suggestion
}

// unsupported records why a proposal could not be justified and drops every
// member the expectation would have carried, so an unsupported suggestion can
// never be read as a value somebody might approve.
func unsupported(suggestion Suggestion, reason string) Suggestion {
	suggestion.Outcome, suggestion.Reason = Unsupported, reason
	suggestion.Count, suggestion.Field = nil, nil
	return suggestion
}

// identifier derives an expectation identifier nothing in the draft and nothing
// earlier in this set has taken, so a suggestion never collides with an
// expectation a person typed.
func identifier(preferred string, taken map[string]bool) string {
	base := sanitize(preferred)
	candidate := base
	for suffix := 2; taken[candidate]; suffix++ {
		candidate = trimmed(base, fmt.Sprintf("-%d", suffix))
	}
	taken[candidate] = true
	return candidate
}

// sanitize reduces a preferred name to what an expectation is named by:
// lowercase letters, digits and hyphens, beginning with a letter.
func sanitize(preferred string) string {
	var name strings.Builder
	for _, r := range strings.ToLower(preferred) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			name.WriteRune(r)
		default:
			name.WriteByte('-')
		}
	}
	candidate := trimmed(name.String(), "")
	if !expectationPattern.MatchString(candidate) {
		return "suggestion"
	}
	return candidate
}

// trimmed fits a name and the suffix that makes it unique inside the 64 bytes
// an expectation identifier holds, without ending on the hyphen a truncation
// may have left.
func trimmed(base, suffix string) string {
	if len(base)+len(suffix) > 64 {
		base = base[:64-len(suffix)]
	}
	return strings.Trim(base, "-") + suffix
}

// Approve records what a person decided and returns the draft their approvals
// produced.
//
// This is the only thing that turns a suggestion into an expectation, and it
// needs a decision naming it: a suggestion the review said nothing about is
// reported as not reviewed and is not in the draft. Approvals are recorded
// through the same answer a person typing an expectation gives, so every
// refusal the flow already makes is made again here, and a set that would
// contradict the draft leaves it exactly as it was.
func Approve(draft Draft, suggestions Suggestions, review Review) (Draft, Approval, error) {
	if err := check(draft); err != nil {
		return Draft{}, Approval{}, err
	}
	if suggestions.Origin.Identity == "" || len(suggestions.Suggestions) == 0 {
		return Draft{}, Approval{}, errors.New("there is nothing to review; suggest expectations from a reviewed run first")
	}
	if review.Result != suggestions.Origin.Result || review.Identity != suggestions.Origin.Identity {
		return Draft{}, Approval{}, errors.New("this review was made against a different run; suggest expectations again before approving them")
	}
	decided, err := match(suggestions, review)
	if err != nil {
		return Draft{}, Approval{}, err
	}
	approval := Approval{
		Result:   suggestions.Origin.Result,
		Identity: suggestions.Origin.Identity,
		Reviewed: make([]Reviewed, 0, len(suggestions.Suggestions)),
	}
	expectations := slices.Clone(draft.Expectations)
	for _, suggestion := range suggestions.Suggestions {
		decision, decidedOn := decided[suggestion.ID]
		switch {
		case !decidedOn:
			approval.Reviewed = append(approval.Reviewed, Reviewed{Suggestion: suggestion.ID, Outcome: NotReviewed})
			approval.NotReviewedCount++
		case !decision.Approved:
			approval.Reviewed = append(approval.Reviewed, Reviewed{Suggestion: suggestion.ID, Outcome: Rejected})
			approval.RejectedCount++
		default:
			expectation, err := expected(suggestion, decision)
			if err != nil {
				return Draft{}, Approval{}, err
			}
			expectations = append(expectations, expectation)
			approval.Reviewed = append(approval.Reviewed, Reviewed{Suggestion: suggestion.ID, Outcome: Approved, Expectation: expectation.ID})
			approval.ApprovedCount++
		}
	}
	answered, err := draft.Answer(Answer{Stage: StageExpectations, Expectations: expectations})
	if err != nil {
		return Draft{}, Approval{}, err
	}
	return answered, approval, nil
}

// match pairs the review with the set one decision at a time. A decision naming
// nothing in the set, and a suggestion decided twice, are refused by their own
// sentence: a review whose decisions cannot be matched to proposals is not a
// record of what anybody decided.
func match(suggestions Suggestions, review Review) (map[string]Decision, error) {
	known := make(map[string]bool, len(suggestions.Suggestions))
	for _, suggestion := range suggestions.Suggestions {
		known[suggestion.ID] = true
	}
	decided := make(map[string]Decision, len(review.Decisions))
	for _, decision := range review.Decisions {
		if !known[decision.Suggestion] {
			return nil, errors.New("a decision names one suggestion of this set")
		}
		if _, seen := decided[decision.Suggestion]; seen {
			return nil, errors.New("a review decides each suggestion at most once")
		}
		decided[decision.Suggestion] = decision
	}
	return decided, nil
}

// expected turns one approved suggestion into the expectation it proposed, with
// the edits the decision made. An unsupported suggestion cannot be approved at
// all: approving one would record an expectation the run it came from never
// justified.
func expected(suggestion Suggestion, decision Decision) (Expectation, error) {
	if suggestion.Outcome != Supported {
		return Expectation{}, errors.New("a suggestion this run does not support cannot be approved; it states why it could not be justified")
	}
	expectation := Expectation{
		ID:       suggestion.ID,
		Operator: suggestion.Operator,
		Message:  suggestion.Message,
		Selector: suggestion.Selector,
		Count:    suggestion.Count,
		Field:    suggestion.Field,
	}
	if decision.ID != "" {
		expectation.ID = decision.ID
	}
	edited := errors.New("an approval edits only a member the suggestion's own operator declares")
	switch suggestion.Operator {
	case LedgerCount:
		if decision.Field != nil {
			return Expectation{}, edited
		}
		if decision.Count != nil {
			records := *decision.Count
			expectation.Count = &records
		}
	case ACKFieldEquals:
		if decision.Count != nil {
			return Expectation{}, edited
		}
		if decision.Field != nil {
			field := *decision.Field
			expectation.Field = &field
		}
	default:
		return Expectation{}, edited
	}
	return expectation, nil
}
