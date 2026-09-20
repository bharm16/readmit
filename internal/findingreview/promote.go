package findingreview

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The named outcomes a promotion reports when a finding, or one reference of
// it, cannot be expressed as a test. Each is recorded rather than dropped: a
// finding silently left out of a promoted test, or approximated into a weaker
// assertion, is a test that no longer tests what the finding found.
const (
	// OccurrenceNotInCase: the report names an occurrence this case does not
	// hold. The two identities agree, so this is a report that describes the
	// case incompletely rather than a different case.
	OccurrenceNotInCase = "occurrence_not_in_case"

	// NoAcknowledgementEvidence: the reference is not to a captured
	// acknowledgement. readmit-test/v1 states what a run produced, so it makes
	// no claim about a field of a message the test itself sends.
	NoAcknowledgementEvidence = "no_acknowledgement_evidence"

	// PositionOutsideContract: the reference is to an acknowledgement, at a
	// position readmit-test/v1 does not address.
	PositionOutsideContract = "position_outside_acknowledgement_contract"

	// AcknowledgementNotCorrelated: the captured case links this
	// acknowledgement to no single sent occurrence, so no test can say which
	// message's acknowledgement is being read.
	AcknowledgementNotCorrelated = "acknowledgement_not_correlated"

	// PositionHasNoVocabulary: the position holds a value, and the named
	// profile declares no closed code vocabulary for it. A promoted test
	// carries no other value.
	PositionHasNoVocabulary = "position_has_no_declared_vocabulary"

	// ValueOutsideVocabulary: the position holds a value the named profile
	// does not declare. It is refused rather than copied.
	ValueOutsideVocabulary = "value_outside_declared_vocabulary"

	// UnreadableOccurrence: the verified occurrence could not be read at the
	// position the finding names.
	UnreadableOccurrence = "unreadable_occurrence"

	// DraftRefusedTheAssertions: the authoring flow refused the expectations
	// this finding derived, and its own sentence says why. Nothing weaker is
	// recorded in their place.
	DraftRefusedTheAssertions = "draft_refused_the_assertions"
)

// The closed vocabularies a promoted expectation may carry a value from. They
// are the named profile's own declared codes, owned by [diagnose] and read from
// it rather than copied, so the gate below cannot drift from the rules that
// produced the finding.
//
// The gate is the privacy decision of this delivery, and it is structural
// rather than advisory: a promoted test is a document somebody commits to a
// repository, and the only values it may carry are these codes. MSA-2 holds the
// control identifier an acknowledgement echoes, and it is refused here by
// construction rather than by anyone remembering to.
//
// The keys are canonical selectors of the shared grammar, at the first
// repetition of each field: the profile reads these positions as scalars, so a
// further repetition of one of them is not one of these positions.
var vocabularies = map[string][]string{
	"MSA-1[1]":   diagnose.DeclaredACKOutcomes(),
	"ERR-3[1].1": diagnose.DeclaredErrorCodes(),
	"ERR-4[1]":   diagnose.DeclaredErrorSeverities(),
}

// canonicalPosition splits the shared selector grammar's canonical form into
// the segment and the position inside it. Comparing canonical forms is a
// comparison, not a spelling: an expectation keeps the selector the diagnosis
// wrote, and open decision #164 is not answered here.
var canonicalPosition = regexp.MustCompile(`^([A-Z][A-Z0-9]{2})\[[0-9]+\]-([0-9]+\[[0-9]+\](?:\.[0-9]+){0,2})$`)

// Unsupported is one thing a promotion could not express, named.
type Unsupported struct {
	Code       string `json:"code"`
	Occurrence string `json:"occurrence,omitzero"`
	Field      string `json:"field,omitzero"`
	Detail     string `json:"detail"`
}

// Promotion is what one confirmed finding becomes: the occurrences a test would
// send, the expectations a draft accepted, and everything the finding states
// that no test in this release can.
type Promotion struct {
	Messages     []string                 `json:"messages"`
	Expectations []testauthor.Expectation `json:"expectations"`
	Unsupported  []Unsupported            `json:"unsupported"`
}

// candidate is one expectation derived from one evidence reference, before the
// authoring flow has accepted it.
type candidate struct {
	message     string
	expectation testauthor.Expectation
}

// promote derives, from one finding, the assertions a regression test draft
// holds, and answers them into the draft the review opened so that every
// refusal the authoring flow makes is made again here. Nothing is carried in
// from a caller: the expectations are derived from the verified case each time,
// so a promotion cannot assert a value with the authority of evidence that
// never held it.
//
// It refuses nothing outright. Everything this release cannot express is a
// named outcome of the promotion, so one finding it cannot state never costs a
// review the decisions recorded beside it.
func promote(source *bundle.Bundle, finding diagnose.Finding, base testauthor.Draft) Promotion {
	promotion := Promotion{Messages: []string{}, Expectations: []testauthor.Expectation{}, Unsupported: []Unsupported{}}
	events := make(map[string]bundle.Event, len(source.Events))
	for _, event := range source.Events {
		events[event.ID] = event
	}
	acknowledged := make(map[string]string, len(source.Correlations))
	for _, link := range source.Correlations {
		if link.Kind == bundle.Matched && len(link.MessageIDs) == 1 {
			acknowledged[link.ACKID] = link.MessageIDs[0]
		}
	}
	candidates := make([]candidate, 0, len(finding.Evidence))
	for i, reference := range finding.Evidence {
		found, item := candidateFor(source, events[reference.Occurrence], acknowledged, finding, reference, i+1)
		if item != nil {
			promotion.Unsupported = append(promotion.Unsupported, *item)
			continue
		}
		candidates = append(candidates, found)
	}
	if len(candidates) == 0 {
		return promotion
	}
	draft, err := answer(source, candidates, base)
	if err != nil {
		promotion.Unsupported = append(promotion.Unsupported, Unsupported{Code: DraftRefusedTheAssertions, Detail: err.Error()})
		return promotion
	}
	promotion.Messages, promotion.Expectations = draft.Messages, draft.Expectations
	return promotion
}

// candidateFor reports the expectation one evidence reference supports, or the
// named reason it supports none.
func candidateFor(source *bundle.Bundle, event bundle.Event, acknowledged map[string]string, finding diagnose.Finding, reference diagnose.Evidence, index int) (candidate, *Unsupported) {
	refuse := func(code, detail string) (candidate, *Unsupported) {
		return candidate{}, &Unsupported{Code: code, Occurrence: reference.Occurrence, Field: reference.Field, Detail: detail}
	}
	switch {
	case event.ID == "":
		return refuse(OccurrenceNotInCase, "This report names an occurrence the verified case does not hold; review the report this case was diagnosed with.")
	case event.Kind != bundle.Acknowledgement:
		return refuse(NoAcknowledgementEvidence, "This reference is not to a captured acknowledgement. A test states what a run produced, and this release's test contract makes no claim about a field of a message the test sends.")
	}
	selector, err := hl7.ParseSelector(reference.Field)
	if err != nil {
		return refuse(PositionOutsideContract, "The position this finding names is not one the shared selector grammar addresses.")
	}
	position := canonicalPosition.FindStringSubmatch(selector.String())
	if position == nil || position[1] != "MSA" && position[1] != "ERR" {
		return refuse(PositionOutsideContract, "This release addresses an acknowledgement at its MSA and ERR positions; a test makes no claim about any other position of one.")
	}
	message, linked := acknowledged[event.ID]
	if !linked {
		return refuse(AcknowledgementNotCorrelated, "The verified case links this acknowledgement to no single sent occurrence, so no test can say whose acknowledgement it reads.")
	}
	value, err := read(source, event, selector)
	if err != nil {
		return refuse(UnreadableOccurrence, "The verified occurrence could not be read at the position this finding names.")
	}
	field := testrunner.FieldValue{State: value.State}
	if value.State == hl7.Present {
		declared, known := vocabularies[position[1]+"-"+position[2]]
		if !known {
			return refuse(PositionHasNoVocabulary, "The captured position holds a value, and the named profile declares no closed code vocabulary for it. A promoted test carries a value only from a vocabulary the profile declares.")
		}
		if !slices.Contains(declared, value.Text) {
			return refuse(ValueOutsideVocabulary, "The captured position holds a value the named profile does not declare; it was not copied into an expectation.")
		}
		text := value.Text
		field.Text = &text
	}
	return candidate{message: message, expectation: testauthor.Expectation{
		ID:       fmt.Sprintf("%s-%d", finding.ID, index),
		Operator: testauthor.ACKFieldEquals,
		Message:  message,
		// The selector is kept exactly as the diagnosis wrote it.
		Selector: reference.Field,
		Field:    &field,
	}}, nil
}

// decoded is one field of a verified occurrence: the state the shared selector
// returns, and the decoded text of a present one.
type decoded struct {
	State hl7.State
	Text  string
}

// read reads one position of one verified occurrence. It reads the case rather
// than the report, because a report deliberately records that a field was
// present without recording what it held.
func read(source *bundle.Bundle, event bundle.Event, selector hl7.Selector) (decoded, error) {
	document, err := source.Document(event)
	if err != nil {
		return decoded{}, err
	}
	value, err := document.Select(0, selector)
	if err != nil {
		return decoded{}, err
	}
	if value.State != hl7.Present {
		return decoded{State: value.State}, nil
	}
	text, err := hl7.Decode(document.Bytes(value.Span), document.Messages[0].Delimiters)
	if err != nil {
		return decoded{}, err
	}
	return decoded{State: hl7.Present, Text: string(text)}, nil
}

// answer records the derived expectations through the authoring flow's own
// Answer. There is no second authoring path: what a promotion reports is what a
// readmit-test-draft/v1 accepted, in the order the case records the occurrences.
func answer(source *bundle.Bundle, candidates []candidate, draft testauthor.Draft) (testauthor.Draft, error) {
	selected := make(map[string]bool, len(candidates))
	for _, item := range candidates {
		selected[item.message] = true
	}
	messages := make([]string, 0, len(selected))
	for _, event := range source.Events {
		if selected[event.ID] {
			messages = append(messages, event.ID)
		}
	}
	draft, err := draft.Answer(testauthor.Answer{Stage: testauthor.StageMessages, Messages: messages})
	if err != nil {
		return testauthor.Draft{}, err
	}
	expectations := make([]testauthor.Expectation, 0, len(candidates))
	for _, item := range candidates {
		expectations = append(expectations, item.expectation)
	}
	return draft.Answer(testauthor.Answer{Stage: testauthor.StageExpectations, Expectations: expectations})
}
