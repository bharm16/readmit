package findingreview_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
	"github.com/bharm16/readmit/internal/hl7"
)

// handWritten builds a diagnosis report naming the given positions of one
// occurrence. A report is an input document, so the positions a review is asked
// to promote are not only the ones this release's rules happen to produce.
func handWritten(source *bundle.Bundle, occurrence string, fields ...string) diagnose.Report {
	evidence := make([]diagnose.Evidence, 0, len(fields))
	for _, field := range fields {
		evidence = append(evidence, diagnose.Evidence{Occurrence: occurrence, Field: field, State: hl7.Present})
	}
	return diagnose.Report{
		Schema: diagnose.Schema, CaseIdentity: source.Identity, Profile: diagnose.Profile, Ruleset: diagnose.Ruleset,
		Findings: []diagnose.Finding{{
			ID: "f000001", RuleID: diagnose.ACKOutcome, Classification: "observed_fact",
			Profile: diagnose.Profile, Ruleset: diagnose.Ruleset, Summary: "a position this review is asked to promote",
			Evidence: evidence,
		}},
	}
}

func promoted(t *testing.T, report diagnose.Report, source *bundle.Bundle) findingreview.Promotion {
	t.Helper()
	identity := digest(t, "report")
	record := review(t, report, identity, source,
		findingreview.Decision{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "confirmed for this test"})
	reported := status(t, record, "f000001")
	if reported.Promotion == nil {
		t.Fatal("a confirmed finding reported no promotion at all")
	}
	return *reported.Promotion
}

func onlyRefusal(t *testing.T, promotion findingreview.Promotion, code string) {
	t.Helper()
	if len(promotion.Expectations) != 0 {
		t.Fatalf("a finding this release cannot express became an assertion anyway: %+v", promotion.Expectations)
	}
	if !slices.ContainsFunc(promotion.Unsupported, func(item findingreview.Unsupported) bool { return item.Code == code }) {
		t.Fatalf("expected %s, got %+v", code, promotion.Unsupported)
	}
}

func TestAFindingAboutASentMessageIsNamedRatherThanApproximated(t *testing.T) {
	path, source := openCase(t, frame(incompleteBooking))
	report, identity := diagnosed(t, path)
	at := slices.IndexFunc(report.Findings, func(f diagnose.Finding) bool { return f.RuleID == diagnose.RequiredField })
	if at < 0 {
		t.Fatalf("the fixture produced no profile violation: %+v", report.Findings)
	}
	record := review(t, report, identity, source,
		findingreview.Decision{Finding: report.Findings[at].ID, Verdict: findingreview.Confirmed, Rationale: "the sender must stop omitting this"})
	onlyRefusal(t, *status(t, record, report.Findings[at].ID).Promotion, findingreview.NoAcknowledgementEvidence)
}

func TestAnAcknowledgementNothingSentIsNotPromoted(t *testing.T) {
	path, source := openCase(t, frame(unlinkedACK))
	report, identity := diagnosed(t, path)
	if len(report.Findings) == 0 {
		t.Fatal("the fixture produced no acknowledgement finding")
	}
	record := review(t, report, identity, source,
		findingreview.Decision{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "the acceptance must keep holding"})
	onlyRefusal(t, *status(t, record, "f000001").Promotion, findingreview.AcknowledgementNotCorrelated)
}

func TestAPositionOutsideTheAcknowledgementContractIsRefused(t *testing.T) {
	_, source := openCase(t, frame(booking, acknowledgement))
	onlyRefusal(t, promoted(t, handWritten(source, "s0001-e000002", "MSH-10"), source), findingreview.PositionOutsideContract)
}

func TestAValueTheProfileDoesNotDeclareIsNeverCopiedIntoAnAssertion(t *testing.T) {
	undeclared := strings.Replace(acknowledgement, "MSA|AE|", "MSA|ZZ|", 1)
	_, source := openCase(t, frame(booking, undeclared))
	onlyRefusal(t, promoted(t, handWritten(source, "s0001-e000002", "MSA[1]-1"), source), findingreview.ValueOutsideVocabulary)
}

func TestAnOccurrenceTheCaseDoesNotHoldIsNamedRatherThanAssumed(t *testing.T) {
	_, source := openCase(t, frame(booking, acknowledgement))
	onlyRefusal(t, promoted(t, handWritten(source, "s0001-e000009", "MSA[1]-1"), source), findingreview.OccurrenceNotInCase)
}

func TestAssertionsTheAuthoringFlowRefusesAreNamedAndCostNoOtherDecision(t *testing.T) {
	// One finding past the draft's own bound on expectations. The review still
	// reports every other decision; only this finding records the refusal, in
	// the authoring flow's own words.
	_, source := openCase(t, frame(booking, acknowledgement))
	fields := make([]string, 0, 300)
	for range 300 {
		fields = append(fields, "MSA[1]-1")
	}
	promotion := promoted(t, handWritten(source, "s0001-e000002", fields...), source)
	onlyRefusal(t, promotion, findingreview.DraftRefusedTheAssertions)
	if promotion.Unsupported[0].Detail == "" {
		t.Fatal("a refusal was recorded without the reason the authoring flow gave")
	}
}

func TestAnAbsentPositionPromotesExactlyTheStateTheEvidenceHeld(t *testing.T) {
	// An acknowledgement whose ERR declares no coding system. The state, not a
	// value, is the whole expectation: absent, empty and explicit null stay
	// three separate claims rather than collapsing into one.
	withoutSystem := strings.Replace(acknowledgement, "101^FREE-TEXT-NEVER-COPIED^HL70357", "101", 1)
	_, source := openCase(t, frame(booking, withoutSystem))
	promotion := promoted(t, handWritten(source, "s0001-e000002", "ERR[1]-3.3"), source)
	if len(promotion.Expectations) != 1 {
		t.Fatalf("an omitted position did not promote: %+v", promotion)
	}
	field := promotion.Expectations[0].Field
	if field == nil || field.State != hl7.Omitted || field.Text != nil {
		t.Fatalf("an omitted position promoted something other than its own state: %+v", field)
	}
}

func TestAReviewIsBoundToTheDocumentsItWasMadeOver(t *testing.T) {
	path, source := openCase(t, frame(booking, acknowledgement))
	report, identity := diagnosed(t, path)
	confirm := findingreview.Decision{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "keep it"}
	_, unrelated := openCase(t, frame(strings.ReplaceAll(booking, "BOOK-1", "BOOK-9")))
	for name, attempt := range map[string]struct {
		reviewed  findingreview.Reviewed
		decisions findingreview.Decisions
	}{
		"decisions read against another report":   {reviewed(report, identity, source), decisions(digest(t, "another diagnosis"), confirm)},
		"evidence the diagnosis was not run over": {reviewed(report, identity, unrelated), decisions(identity, confirm)},
		"a decision about a finding nobody produced": {reviewed(report, identity, source),
			decisions(identity, findingreview.Decision{Finding: "f009999", Verdict: findingreview.Dismissed, Rationale: "not here"})},
		"a report named by something that is not a digest": {reviewed(report, "not-a-digest", source), decisions(identity, confirm)},
		"a case not named by one workspace entry": {findingreview.Reviewed{Report: report, Identity: identity, Case: source, Entry: ".."},
			decisions(identity, confirm)},
		"no case at all": {findingreview.Reviewed{Report: report, Identity: identity, Entry: "case"}, decisions(identity, confirm)},
	} {
		if _, err := findingreview.Review(attempt.reviewed, attempt.decisions, digest(t, "decisions")); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestTheSameReviewIsWrittenTheSameWayEveryTime(t *testing.T) {
	path, source := openCase(t, frame(booking, acknowledgement))
	report, identity := diagnosed(t, path)
	confirm := findingreview.Decision{Finding: "f000001", Verdict: findingreview.Confirmed, Rationale: "keep it"}
	written := make([]string, 0, 2)
	for range 2 {
		data, err := findingreview.JSON(review(t, report, identity, source, confirm))
		if err != nil {
			t.Fatal(err)
		}
		written = append(written, string(data))
	}
	if written[0] != written[1] {
		t.Fatal("the same review over the same documents was written differently")
	}
	if !strings.Contains(written[0], fmt.Sprintf("%q", findingreview.Boundary)) {
		t.Fatal("the review does not state the boundary its promotions were drafted at")
	}
}
