package testauthor_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The two occurrences of the frozen regression case: a booking and the
// rescheduling of that booking.
const (
	booking     = "s0001-e000001"
	reschedules = "s0001-e000002"
)

// reviewedRun is the whole prerequisite of this delivery: a workspace holding
// the sample case, a saved test over it, and one run of that test somebody has
// already looked at. The run is executed exactly as the window executes a
// practice run, and its result directory is then one entry of the workspace,
// which is how `readmit test --output` writes one.
func reviewedRun(t *testing.T, corrected bool) (string, *bundle.Bundle, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "readmit-sample")
	if _, err := synth.Write(root, bundle.GeneratorInputs{
		Seed:             0,
		BaseTime:         time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		GeneratorVersion: "readmit-synth-v1",
		ProfileVersion:   "readmit-siu-v1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := guide.PrepareWorkspace(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	source, err := bundle.Open(filepath.Join(root, guide.CaseName))
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	draft := answered(t, source.Identity, []testauthor.Expectation{{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &one}})
	if _, err := testauthor.Save(root, source, draft, "reschedule-test.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := guide.Run(t.Context(), root, "reschedule-test.json", "practice", corrected); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "practice", "result"), filepath.Join(root, "known-good")); err != nil {
		t.Fatal(err)
	}
	return root, source, "known-good"
}

// answered is the draft this flow suggests into: everything the seven stages
// ask, with whatever expectations a test names so far.
func answered(t *testing.T, identity string, expectations []testauthor.Expectation) testauthor.Draft {
	t.Helper()
	draft, err := testauthor.NewDraft(guide.CaseName, identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{booking, reschedules}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
		{Stage: testauthor.StageExpectations, Expectations: expectations},
	} {
		next, err := draft.Answer(answer)
		if err != nil {
			t.Fatalf("%s: %v", answer.Stage, err)
		}
		draft = next
	}
	return draft
}

func find(t *testing.T, set testauthor.Suggestions, message, selector string) testauthor.Suggestion {
	t.Helper()
	for _, suggestion := range set.Suggestions {
		if suggestion.Message == message && suggestion.Selector == selector {
			return suggestion
		}
	}
	t.Fatalf("the set proposes nothing for %s at %s: %+v", message, selector, set.Suggestions)
	return testauthor.Suggestion{}
}

// A suggestion is a claim about what should be true, derived from what was true
// once, so it says which run it came from and where in that run each value was
// read.
func TestSuggestionsCarryTheRunAndThePositionTheyWereReadFrom(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{
		Result: result, Ledger: true, Positions: []string{"MSA-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if set.SupportedCount != 3 || set.UnsupportedCount != 0 {
		t.Fatalf("the reviewed run supported %d of %d proposals: %+v", set.SupportedCount, len(set.Suggestions), set.Suggestions)
	}
	if set.Origin.Result != result || set.Origin.Status != string(testrunner.Pass) || set.Origin.Boundary != testrunner.LedgerBoundary {
		t.Fatalf("the set does not name the run it came from: %+v", set.Origin)
	}
	if set.Origin.InputIdentity != source.Identity {
		t.Fatalf("the set does not name the evidence the run replayed: %+v", set.Origin)
	}
	for _, named := range []string{set.Origin.Identity, set.Origin.SpecIdentity, set.Origin.RunIdentity, set.Origin.TargetIdentity} {
		if len(named) == 0 {
			t.Fatalf("the set leaves part of the run's provenance unnamed: %+v", set.Origin)
		}
	}
	ledger := set.Suggestions[0]
	if ledger.Operator != testauthor.LedgerCount || ledger.Count == nil || *ledger.Count != 1 {
		t.Fatalf("the corrected fixture recorded one appointment; the set proposes %+v", ledger)
	}
	if ledger.Evidence.Payload == "" || ledger.Evidence.Digest == "" {
		t.Fatalf("the ledger proposal does not name the observation it was read from: %+v", ledger.Evidence)
	}
	acknowledged := find(t, set, booking, "MSA-1")
	if acknowledged.Field == nil || acknowledged.Field.Text == nil || *acknowledged.Field.Text != "AA" {
		t.Fatalf("the run acknowledged the booking with AA; the set proposes %+v", acknowledged.Field)
	}
	if acknowledged.Evidence.Message != booking || acknowledged.Evidence.Selector != "MSA-1" || acknowledged.Evidence.Payload == "" {
		t.Fatalf("the acknowledgement proposal does not name where it was read: %+v", acknowledged.Evidence)
	}
}

// Generating proposes and nothing else. There is no path from suggesting to
// recording, so a draft that has been suggested into is a draft nobody has
// approved anything for.
func TestSuggestingRecordsNoExpectation(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	if _, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{
		Result: result, Ledger: true, Positions: []string{"MSA-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(draft.Expectations) != 0 {
		t.Fatalf("suggesting recorded %d expectations: %+v", len(draft.Expectations), draft.Expectations)
	}
	if _, err := testauthor.Generate(draft); err == nil {
		t.Fatal("a draft nobody approved anything for generated a test spec")
	}
}

// Approval is the whole point: only what a person named is recorded, a refusal
// is an outcome of its own, and a proposal nobody looked at is neither.
func TestOnlyApprovedSuggestionsBecomeExpectations(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{
		Result: result, Ledger: true, Positions: []string{"MSA-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, approval, err := testauthor.Approve(draft, set, testauthor.Review{
		Result: result, Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{
			{Suggestion: set.Suggestions[0].ID, Approved: true},
			{Suggestion: find(t, set, booking, "MSA-1").ID, Approved: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if approval.ApprovedCount != 1 || approval.RejectedCount != 1 || approval.NotReviewedCount != 1 {
		t.Fatalf("the review is not reported one outcome per proposal: %+v", approval)
	}
	if len(approval.Reviewed) != len(set.Suggestions) {
		t.Fatalf("the approval leaves a proposal unaccounted for: %+v", approval.Reviewed)
	}
	if len(approved.Expectations) != 1 || approved.Expectations[0].Operator != testauthor.LedgerCount {
		t.Fatalf("approving one proposal recorded %+v", approved.Expectations)
	}
	if approval.Result != result || approval.Identity != set.Origin.Identity {
		t.Fatalf("the approval does not name the run it was made over: %+v", approval)
	}
}

// A review that decides nothing approves nothing. The default is not
// acceptance, so a person who read the proposals and closed the panel has
// recorded no expectation.
func TestAReviewThatDecidesNothingApprovesNothing(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{
		Result: result, Ledger: true, Positions: []string{"MSA-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, approval, err := testauthor.Approve(draft, set, testauthor.Review{Result: result, Identity: set.Origin.Identity})
	if err != nil {
		t.Fatal(err)
	}
	if approval.ApprovedCount != 0 || approval.NotReviewedCount != len(set.Suggestions) {
		t.Fatalf("a review deciding nothing reported %+v", approval)
	}
	if len(unchanged.Expectations) != 0 {
		t.Fatalf("a review deciding nothing recorded %+v", unchanged.Expectations)
	}
}

// A reviewer edits what the operator declares: the identifier a result will
// name the expectation by, and the value itself.
func TestApprovingRecordsTheEditsTheReviewerMade(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{
		Result: result, Ledger: true, Positions: []string{"MSA-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	two := 2
	rejected := "AR"
	acknowledged := find(t, set, reschedules, "MSA-1")
	approved, _, err := testauthor.Approve(draft, set, testauthor.Review{
		Result: result, Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{
			{Suggestion: set.Suggestions[0].ID, Approved: true, ID: "two-appointments", Count: &two},
			{Suggestion: acknowledged.ID, Approved: true, ID: "reschedule-rejected",
				Field: &testrunner.FieldValue{State: "present", Text: &rejected}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(approved.Expectations) != 2 {
		t.Fatalf("approving two proposals recorded %+v", approved.Expectations)
	}
	if approved.Expectations[0].ID != "two-appointments" || *approved.Expectations[0].Count != 2 {
		t.Fatalf("the edited record count was not recorded: %+v", approved.Expectations[0])
	}
	if approved.Expectations[1].ID != "reschedule-rejected" || *approved.Expectations[1].Field.Text != "AR" {
		t.Fatalf("the edited value was not recorded: %+v", approved.Expectations[1])
	}
	// What a reviewer approved is an ordinary expectation, so the draft it went
	// into generates the spec `readmit test` executes.
	data, err := testauthor.Generate(approved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testrunner.DecodeSpec(data); err != nil {
		t.Fatalf("approved expectations did not generate a spec this release reads: %v", err)
	}
}

// An edit stays inside the operator it was proposed for: a record count is not
// turned into a value, and a value is not turned into a count.
func TestAnApprovalDoesNotEditAnotherOperatorsMember(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{
		Result: result, Ledger: true, Positions: []string{"MSA-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	three := 3
	text := "AA"
	for _, decision := range []testauthor.Decision{
		{Suggestion: set.Suggestions[0].ID, Approved: true, Field: &testrunner.FieldValue{State: "present", Text: &text}},
		{Suggestion: find(t, set, booking, "MSA-1").ID, Approved: true, Count: &three},
	} {
		if _, _, err := testauthor.Approve(draft, set, testauthor.Review{
			Result: result, Identity: set.Origin.Identity, Decisions: []testauthor.Decision{decision},
		}); err == nil {
			t.Fatalf("an approval edited a member the proposal's operator does not declare: %+v", decision)
		}
	}
}

// Approvals are recorded through the answer a person typing an expectation
// gives, so a set that would contradict the draft leaves it exactly as it was.
func TestApprovingTheSameSuggestionTwiceIsRefused(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{
		Result: result, Ledger: true, Positions: []string{"MSA-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	review := testauthor.Review{Result: result, Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{{Suggestion: set.Suggestions[0].ID, Approved: true}}}
	once, _, err := testauthor.Approve(draft, set, review)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := testauthor.Approve(once, set, review); err == nil {
		t.Fatal("approving one proposal twice recorded the same expectation twice")
	}
}

// A run whose own expectations did not hold is a run under investigation, not a
// reviewed known-good one: deriving expectations from it would carry the defect
// it recorded into the test.
func TestSuggestingRefusesARunWhoseOwnExpectationsDidNotHold(t *testing.T) {
	root, source, result := reviewedRun(t, false)
	draft := answered(t, source.Identity, nil)
	_, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{Result: result, Ledger: true})
	if err == nil || !strings.Contains(err.Error(), "whose own expectations held") {
		t.Fatalf("a failing run was suggested from: %v", err)
	}
}

// Every refusal about the run itself, each a different question the run
// answers.
func TestSuggestingRefusesARunThatAnswersADifferentQuestion(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	elsewhere := answered(t, strings.Repeat("a", 64), nil)
	if _, err := testauthor.Suggest(root, elsewhere, testauthor.SuggestionRequest{Result: result, Ledger: true}); err == nil {
		t.Fatal("a run of different evidence was suggested from")
	}
	acknowledgement, err := answered(t, source.Identity, nil).Answer(testauthor.Answer{
		Stage: testauthor.StageBoundary, Boundary: testrunner.ACKBoundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testauthor.Suggest(root, acknowledgement, testauthor.SuggestionRequest{
		Result: result, Positions: []string{"MSA-1"},
	}); err == nil {
		t.Fatal("a run observed at another boundary was suggested from")
	}
	if _, err := testauthor.Suggest(root, acknowledgement, testauthor.SuggestionRequest{Result: result, Ledger: true}); err == nil {
		t.Fatal("the ack-contract boundary was asked for a record count")
	}
	draft := answered(t, source.Identity, nil)
	for _, entry := range []string{"", "../known-good", guide.TargetName, guide.CaseName, "missing"} {
		if _, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{Result: entry, Ledger: true}); err == nil {
			t.Fatalf("%q was read as a reviewed run result", entry)
		}
	}
}

// A request this release cannot address is refused before any evidence is
// opened, because it is a question about the request and not about the run.
func TestSuggestingRefusesARequestThisReleaseCannotAddress(t *testing.T) {
	draft := answered(t, strings.Repeat("b", 64), nil)
	positions := make([]string, 0, 17)
	for i := range 17 {
		positions = append(positions, fmt.Sprintf("MSA-%d", i+1))
	}
	for name, request := range map[string]testauthor.SuggestionRequest{
		"a position outside MSA and ERR":  {Result: "known-good", Positions: []string{"PID-5"}},
		"a position that is no selector":  {Result: "known-good", Positions: []string{"not a selector"}},
		"the same position twice":         {Result: "known-good", Positions: []string{"MSA-1", "MSA-1"}},
		"more positions than are offered": {Result: "known-good", Positions: positions},
		"nothing at all":                  {Result: "known-good"},
	} {
		if _, err := testauthor.Suggest("", draft, request); err == nil {
			t.Fatalf("%s was proposed", name)
		}
	}
	// The bound on the whole set is the bound a test holds, and it is reached
	// before a run is opened.
	wide := draft
	wide.Messages = make([]string, 0, 17)
	for i := range 17 {
		wide.Messages = append(wide.Messages, fmt.Sprintf("s0001-e%06d", i+1))
	}
	if _, err := testauthor.Suggest("", wide, testauthor.SuggestionRequest{Result: "known-good", Positions: positions[:16]}); err == nil {
		t.Fatal("a review proposed more expectations than a test holds")
	}
	// The bound counts what the draft already expects, so a set that could not
	// be approved is refused before a run is opened rather than after its
	// values have been read.
	full := draft
	records := 1
	full.Expectations = make([]testauthor.Expectation, 0, 250)
	for i := range 250 {
		full.Expectations = append(full.Expectations, testauthor.Expectation{
			ID: fmt.Sprintf("already-expected-%d", i), Operator: testauthor.LedgerCount, Count: &records,
		})
	}
	if _, err := testauthor.Suggest("", full, testauthor.SuggestionRequest{Result: "known-good", Positions: positions[:16]}); err == nil {
		t.Fatal("a review proposed past the expectations the draft already holds")
	}
}

// A proposal nobody could justify is named as such: it states why, carries no
// expected value, and cannot be approved.
func TestAnUnsupportedSuggestionSaysWhyAndCannotBeApproved(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	// This occurrence is one of the case; the reviewed run did not send it.
	elsewhere, err := draft.Answer(testauthor.Answer{Stage: testauthor.StageMessages, Messages: []string{booking, "s0001-e000003"}})
	if err != nil {
		t.Fatal(err)
	}
	set, err := testauthor.Suggest(root, elsewhere, testauthor.SuggestionRequest{Result: result, Positions: []string{"MSA-1", "ERR-3"}})
	if err != nil {
		t.Fatal(err)
	}
	unsent := find(t, set, "s0001-e000003", "MSA-1")
	if unsent.Outcome != testauthor.Unsupported || unsent.Reason == "" {
		t.Fatalf("a proposal for an occurrence the run did not send is %+v", unsent)
	}
	if unsent.Field != nil || unsent.Count != nil {
		t.Fatalf("an unsupported proposal carries a value: %+v", unsent)
	}
	// The acknowledgements of this profile carry no ERR segment. That is a
	// reading, not a failure: an absent position is one of the four states an
	// expectation states, and the three absences stay separate.
	absent := find(t, set, booking, "ERR-3")
	if absent.Outcome != testauthor.Supported || absent.Field == nil || absent.Field.State != "omitted" || absent.Field.Text != nil {
		t.Fatalf("a position this acknowledgement does not carry is %+v", absent.Field)
	}
	if set.UnsupportedCount == 0 || set.SupportedCount+set.UnsupportedCount != len(set.Suggestions) {
		t.Fatalf("the set does not account for every proposal: %+v", set)
	}
	if _, _, err := testauthor.Approve(elsewhere, set, testauthor.Review{
		Result: result, Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{{Suggestion: unsent.ID, Approved: true}},
	}); err == nil {
		t.Fatal("a proposal the run does not support was approved")
	}
}

// A review is bound to the proposals it was made over, one decision to one
// proposal.
func TestApprovingRefusesAReviewItCannotMatch(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{Result: result, Ledger: true, Positions: []string{"MSA-1"}})
	if err != nil {
		t.Fatal(err)
	}
	first := set.Suggestions[0].ID
	for name, review := range map[string]testauthor.Review{
		"a review of another run": {Result: result, Identity: strings.Repeat("c", 64),
			Decisions: []testauthor.Decision{{Suggestion: first, Approved: true}}},
		"a review naming another result": {Result: "elsewhere", Identity: set.Origin.Identity,
			Decisions: []testauthor.Decision{{Suggestion: first, Approved: true}}},
		"a decision naming no proposal": {Result: result, Identity: set.Origin.Identity,
			Decisions: []testauthor.Decision{{Suggestion: "nothing-proposed-this", Approved: true}}},
		"one proposal decided twice": {Result: result, Identity: set.Origin.Identity,
			Decisions: []testauthor.Decision{{Suggestion: first, Approved: true}, {Suggestion: first, Approved: false}}},
	} {
		if _, _, err := testauthor.Approve(draft, set, review); err == nil {
			t.Fatalf("%s was applied", name)
		}
	}
	if _, _, err := testauthor.Approve(draft, testauthor.Suggestions{}, testauthor.Review{}); err == nil {
		t.Fatal("an empty set was approved")
	}
}

// A suggestion is derived from real evidence, which is exactly where a value
// leaks into a document somebody commits. What crosses is what the expectation
// it proposes would carry: a count of records, and one acknowledgement value at
// a position that was asked for. The ledger's own records do not.
func TestASuggestionCarriesNoLedgerRecordAndNoUnaskedPosition(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{Result: result, Ledger: true, Positions: []string{"MSA-1"}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := testrunner.Open(filepath.Join(root, result))
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.FinalObservation.Records) == 0 {
		t.Fatal("the reviewed run observed no ledger record to hold this to")
	}
	data, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	answer := string(data)
	for _, record := range artifact.FinalObservation.Records {
		for _, value := range []string{record.PatientID.Value, record.PlacerID.Value, record.FillerID.Value, record.AppointmentStart} {
			if value != "" && strings.Contains(answer, value) {
				t.Fatalf("a ledger record value reached the suggestion set: %q", value)
			}
		}
	}
	// The sent message holds the patient's own identifiers; a proposal about an
	// acknowledgement reads none of them.
	raw, err := artifact.Run.Raw(artifact.Run.Events[0].Sent)
	if err != nil {
		t.Fatal(err)
	}
	for _, segment := range strings.Split(strings.ReplaceAll(string(raw), "\r", "\n"), "\n") {
		if !strings.HasPrefix(segment, "PID") && !strings.HasPrefix(segment, "SCH") {
			continue
		}
		if strings.Contains(answer, segment) {
			t.Fatalf("a sent segment reached the suggestion set: %q", segment)
		}
	}
}

// The preview says what the expectations decide and what they leave undecided,
// as positions rather than values.
func TestCoverageNamesWhatNothingDecides(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	draft := answered(t, source.Identity, nil)
	empty := testauthor.Cover(draft)
	if !empty.Ledger.Applies || empty.Ledger.Covered || len(empty.Uncovered) != 2 {
		t.Fatalf("a draft expecting nothing covers %+v", empty)
	}
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{Result: result, Ledger: true, Positions: []string{"MSA-1"}})
	if err != nil {
		t.Fatal(err)
	}
	approved, _, err := testauthor.Approve(draft, set, testauthor.Review{
		Result: result, Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{
			{Suggestion: set.Suggestions[0].ID, Approved: true},
			{Suggestion: find(t, set, booking, "MSA-1").ID, Approved: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	covered := testauthor.Cover(approved)
	if !covered.Ledger.Covered || covered.Ledger.Expectation == "" {
		t.Fatalf("an approved record count left the ledger uncovered: %+v", covered.Ledger)
	}
	if len(covered.Uncovered) != 1 || covered.Uncovered[0] != reschedules {
		t.Fatalf("the message nothing decides is not named: %+v", covered.Uncovered)
	}
	if len(covered.Messages) != 2 || len(covered.Messages[0].Positions) != 1 || covered.Messages[0].Positions[0] != "MSA-1" {
		t.Fatalf("the positions an expectation addresses are not reported: %+v", covered.Messages)
	}
	// The ack-contract boundary makes no ledger claim, so a ledger nothing
	// decides there is not a gap.
	acknowledgement, err := draft.Answer(testauthor.Answer{Stage: testauthor.StageBoundary, Boundary: testrunner.ACKBoundary})
	if err != nil {
		t.Fatal(err)
	}
	if testauthor.Cover(acknowledgement).Ledger.Applies {
		t.Fatal("the ack-contract boundary reported a ledger to cover")
	}
}

// A suggested identifier never collides with one a person typed, so approving a
// proposal is not refused for a name the draft already holds.
func TestSuggestedIdentifiersAvoidTheOnesTheDraftHolds(t *testing.T) {
	root, source, result := reviewedRun(t, true)
	one := 1
	draft := answered(t, source.Identity, []testauthor.Expectation{{ID: "ledger-records", Operator: testauthor.LedgerCount, Count: &one}})
	set, err := testauthor.Suggest(root, draft, testauthor.SuggestionRequest{Result: result, Ledger: true, Positions: []string{"MSA-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if set.Suggestions[0].ID == "ledger-records" {
		t.Fatalf("a proposal took an identifier the draft already holds: %+v", set.Suggestions[0])
	}
	decisions := make([]testauthor.Decision, 0, len(set.Suggestions))
	for _, suggestion := range set.Suggestions {
		decisions = append(decisions, testauthor.Decision{Suggestion: suggestion.ID, Approved: true})
	}
	approved, approval, err := testauthor.Approve(draft, set, testauthor.Review{Result: result, Identity: set.Origin.Identity, Decisions: decisions})
	if err != nil {
		t.Fatal(err)
	}
	if approval.ApprovedCount != len(set.Suggestions) || len(approved.Expectations) != len(set.Suggestions)+1 {
		t.Fatalf("approving every proposal recorded %+v", approved.Expectations)
	}
}
