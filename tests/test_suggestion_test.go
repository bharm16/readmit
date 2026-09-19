package tests

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// suggestionOf finds the proposal made for one position of one message.
func suggestionOf(t *testing.T, set *testauthor.Suggestions, message, selector string) testauthor.Suggestion {
	t.Helper()
	for _, suggestion := range set.Suggestions {
		if suggestion.Message == message && suggestion.Selector == selector {
			return suggestion
		}
	}
	t.Fatalf("nothing was proposed for %s at %s: %+v", message, selector, set.Suggestions)
	return testauthor.Suggestion{}
}

// answerStages answers everything the flow asks except what the run should have
// produced, which is what this delivery proposes.
func answerStages(t *testing.T, app *desktop.App, request desktop.TestRequest) desktop.TestRequest {
	t.Helper()
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "The reviewed run is what this test expects"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
	} {
		request.Answer = answer
		authored := app.AuthorTest(request)
		if authored.State != desktop.Completed || authored.Test == nil {
			t.Fatalf("%s: %+v", answer.Stage, authored)
		}
		request.Draft = authored.Test.Draft
	}
	request.Answer = testauthor.Answer{}
	return request
}

// The whole of this delivery through the interface a person uses.
//
// An engineer runs the test they authored, looks at the result, and asks for
// the expectations that run would support. Nothing is recorded by asking. They
// approve one proposal unedited, approve a second with the value corrected,
// refuse a third and never look at the fourth — and the test they save holds
// exactly the two they approved. The command line then reads that spec.
func TestExpectationsSuggestedFromAReviewedRunNeedApproval(t *testing.T) {
	parent := t.TempDir()
	app := desktopApp(t, parent)
	created := app.CreateSampleWorkspace()
	if created.State != desktop.Completed || created.Workspace == nil {
		t.Fatalf("create the sample workspace: %+v", created)
	}
	root := created.Workspace.Root
	guidedTest(t, app, root, "reschedule-test.json")

	// Two runs of that test: one against the fixture's defect, one against the
	// corrected fixture. Only the second is a run whose own expectations held.
	for trial, output := range map[string]string{guide.StepBaseline: "baseline-run", guide.StepPostFix: "post-fix-run"} {
		practice := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: "reschedule-test.json", Trial: trial, Output: output})
		if practice.State != desktop.Completed || practice.Practice == nil {
			t.Fatalf("%s: %+v", trial, practice)
		}
	}
	// A result directory is one entry of the workspace, which is where
	// `readmit test --output` writes one; a practice run writes its own beside
	// the receiver evidence it also kept.
	for from, to := range map[string]string{"post-fix-run": "known-good", "baseline-run": "still-failing"} {
		if err := os.Rename(filepath.Join(root, from, "result"), filepath.Join(root, to)); err != nil {
			t.Fatal(err)
		}
	}

	opened := app.OpenCase(root, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	request := answerStages(t, app, desktop.TestRequest{Workspace: root, Case: guide.CaseName, Identity: opened.Case.Identity})

	// Nothing about this test is decided yet, and the preview says so: two
	// messages are sent and nothing is expected of either, and nothing decides
	// the ledger the boundary declared.
	empty := app.AuthorTest(request)
	if empty.Test == nil || len(empty.Test.Resolution.Coverage.Uncovered) != 2 || empty.Test.Resolution.Coverage.Ledger.Covered {
		t.Fatalf("an unanswered test previews as %+v", empty.Test)
	}

	// A run whose own expectations did not hold is not a known-good one.
	request.Suggest = &testauthor.SuggestionRequest{Result: "still-failing", Ledger: true}
	if refused := app.SuggestExpectations(request); refused.State != desktop.Failed {
		t.Fatalf("a failing run was suggested from: %+v", refused)
	}

	request.Suggest = &testauthor.SuggestionRequest{Result: "known-good", Ledger: true, Positions: []string{"MSA-1", "MSA-3"}}
	proposed := app.SuggestExpectations(request)
	if proposed.State != desktop.Completed || proposed.Test == nil || proposed.Test.Suggestions == nil {
		t.Fatalf("expectations were not proposed: %+v", proposed)
	}
	set := proposed.Test.Suggestions
	if len(set.Suggestions) != 5 || set.SupportedCount != 5 {
		t.Fatalf("the reviewed run proposed %+v", set)
	}
	if set.Origin.Result != "known-good" || set.Origin.Status != string(testrunner.Pass) || set.Origin.Identity == "" {
		t.Fatalf("the proposals do not name the run they came from: %+v", set.Origin)
	}
	// Asking recorded nothing: the draft that comes back is the one that went
	// in, and it still expects nothing of the run.
	if len(proposed.Test.Draft.Expectations) != 0 || proposed.Test.Resolution.Coverage.Ledger.Covered {
		t.Fatalf("proposing expectations recorded one: %+v", proposed.Test.Draft.Expectations)
	}

	ledger := set.Suggestions[0]
	booking := suggestionOf(t, set, "s0001-e000001", "MSA-1")
	reschedule := suggestionOf(t, set, "s0001-e000002", "MSA-1")
	if ledger.Count == nil || *ledger.Count != 1 {
		t.Fatalf("the corrected fixture recorded one appointment; the proposal is %+v", ledger)
	}
	if booking.Field == nil || booking.Field.Text == nil || *booking.Field.Text != "AA" {
		t.Fatalf("the booking was acknowledged AA; the proposal is %+v", booking.Field)
	}

	// One approved unedited, one approved with the identifier and the value a
	// reviewer chose, one refused, and two nobody looked at.
	rejected := "AR"
	request.Review = &testauthor.Review{
		Result: "known-good", Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{
			{Suggestion: ledger.ID, Approved: true},
			{Suggestion: booking.ID, Approved: true, ID: "booking-was-rejected",
				Field: &testrunner.FieldValue{State: hl7.Present, Text: &rejected}},
			{Suggestion: reschedule.ID, Approved: false},
		},
	}
	recorded := app.ApproveExpectations(request)
	if recorded.State != desktop.Completed || recorded.Test == nil || recorded.Test.Approval == nil {
		t.Fatalf("the decisions were not recorded: %+v", recorded)
	}
	approval := recorded.Test.Approval
	if approval.ApprovedCount != 2 || approval.RejectedCount != 1 || approval.NotReviewedCount != 2 {
		t.Fatalf("the review is not reported one outcome per proposal: %+v", approval)
	}
	if len(approval.Reviewed) != len(set.Suggestions) {
		t.Fatalf("a proposal is unaccounted for: %+v", approval.Reviewed)
	}
	request.Draft, request.Review, request.Suggest = recorded.Test.Draft, nil, nil
	if len(request.Draft.Expectations) != 2 {
		t.Fatalf("approving two proposals recorded %+v", request.Draft.Expectations)
	}
	coverage := recorded.Test.Resolution.Coverage
	if !coverage.Ledger.Covered || len(coverage.Uncovered) != 1 || coverage.Uncovered[0] != "s0001-e000002" {
		t.Fatalf("the preview does not report what is still undecided: %+v", coverage)
	}

	// The saved test holds what was approved and nothing else, and the command
	// line reads the document the window wrote.
	request.Output = "reviewed-test.json"
	saved := app.SaveTest(request)
	if saved.State != desktop.Completed || saved.Test.Output != "reviewed-test.json" {
		t.Fatalf("the reviewed test was not written: %+v", saved)
	}
	spec, err := testrunner.ReadSpec(filepath.Join(root, "reviewed-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Assertions) != 2 || spec.Assertions[1].ID != "booking-was-rejected" {
		t.Fatalf("the saved test holds %+v", spec.Assertions)
	}
	if *spec.Assertions[0].Expected.Count != 1 || *spec.Assertions[1].Expected.Field.Text != "AR" {
		t.Fatalf("the saved test does not hold what was approved: %+v", spec.Assertions)
	}
	stdout, stderr, err := run(t, "test", filepath.Join(root, "reviewed-test.json"))
	if err != nil || stderr != "" {
		t.Fatalf("readmit test: %v %s", err, stderr)
	}
	for _, want := range []string{"Observation boundary: appointment-ledger", "Messages: 2"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("the command line disagrees about the reviewed spec (%q):\n%s", want, stdout)
		}
	}

	// A proposal derived from real evidence is where a value leaks into a
	// document somebody commits, so what crosses this boundary is held to what
	// the expectation it proposes would carry. The ledger's own records do not,
	// and neither does any message the run sent.
	artifact, err := testrunner.Open(filepath.Join(root, "known-good"))
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact.FinalObservation.Records) == 0 {
		t.Fatal("the reviewed run observed no ledger record to hold this to")
	}
	for _, answer := range []desktop.TestResult{proposed, recorded} {
		data, err := json.Marshal(answer)
		if err != nil {
			t.Fatal(err)
		}
		crossed := string(data)
		for _, record := range artifact.FinalObservation.Records {
			for _, value := range []string{
				record.PatientID.Value, record.PlacerID.Value, record.FillerID.Value, record.AppointmentStart,
			} {
				if value != "" && strings.Contains(crossed, value) {
					t.Fatalf("a ledger record value reached the window: %q", value)
				}
			}
		}
		sent, err := artifact.Run.Raw(artifact.Run.Events[0].Sent)
		if err != nil {
			t.Fatal(err)
		}
		for _, segment := range strings.Split(strings.ReplaceAll(string(sent), "\r", "\n"), "\n") {
			if strings.HasPrefix(segment, "PID") || strings.HasPrefix(segment, "SCH") {
				if strings.Contains(crossed, segment) {
					t.Fatalf("a sent segment reached the window: %q", segment)
				}
			}
		}
	}
}

// Every refusal this flow makes at the window's own boundary, including the one
// that matters most: a review that decides nothing approves nothing.
func TestSuggestedExpectationsAreRefusedRatherThanAssumed(t *testing.T) {
	parent := t.TempDir()
	app := desktopApp(t, parent)
	created := app.CreateSampleWorkspace()
	if created.State != desktop.Completed || created.Workspace == nil {
		t.Fatalf("create the sample workspace: %+v", created)
	}
	root := created.Workspace.Root
	guidedTest(t, app, root, "reschedule-test.json")
	for trial, output := range map[string]string{guide.StepBaseline: "baseline-run", guide.StepPostFix: "post-fix-run"} {
		if practice := app.RunPractice(desktop.PracticeRequest{
			Workspace: root, Spec: "reschedule-test.json", Trial: trial, Output: output,
		}); practice.State != desktop.Completed {
			t.Fatalf("%s: %+v", trial, practice)
		}
	}
	if err := os.Rename(filepath.Join(root, "post-fix-run", "result"), filepath.Join(root, "known-good")); err != nil {
		t.Fatal(err)
	}
	opened := app.OpenCase(root, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	base := answerStages(t, app, desktop.TestRequest{Workspace: root, Case: guide.CaseName, Identity: opened.Case.Identity})

	// A call that names nothing to propose, a position this release does not
	// address, and an entry that is not a result are each refused before
	// anything is recorded.
	for name, suggest := range map[string]*testauthor.SuggestionRequest{
		"no proposal at all":              {Result: "known-good"},
		"a position outside MSA and ERR":  {Result: "known-good", Positions: []string{"PID-5"}},
		"an entry that is not a result":   {Result: guide.TargetName, Ledger: true},
		"an entry outside the workspace":  {Result: "../known-good", Ledger: true},
		"an entry this workspace has not": {Result: "nothing-here", Ledger: true},
	} {
		request := base
		request.Suggest = suggest
		if refused := app.SuggestExpectations(request); refused.State != desktop.Failed {
			t.Fatalf("%s was proposed from: %+v", name, refused)
		}
	}
	request := base
	if refused := app.SuggestExpectations(request); refused.State != desktop.Failed {
		t.Fatalf("a call naming no reviewed run was accepted: %+v", refused)
	}

	request.Suggest = &testauthor.SuggestionRequest{Result: "known-good", Ledger: true, Positions: []string{"MSA-1"}}
	proposed := app.SuggestExpectations(request)
	if proposed.State != desktop.Completed || proposed.Test == nil || proposed.Test.Suggestions == nil {
		t.Fatalf("expectations were not proposed: %+v", proposed)
	}
	set := proposed.Test.Suggestions

	// A review that decides nothing approves nothing: the default is not
	// acceptance, so closing the panel records no expectation.
	request.Review = &testauthor.Review{Result: "known-good", Identity: set.Origin.Identity}
	silent := app.ApproveExpectations(request)
	if silent.State != desktop.Completed || silent.Test == nil || silent.Test.Approval == nil {
		t.Fatalf("an empty review: %+v", silent)
	}
	if silent.Test.Approval.ApprovedCount != 0 || len(silent.Test.Draft.Expectations) != 0 {
		t.Fatalf("a review deciding nothing recorded %+v", silent.Test.Draft.Expectations)
	}
	if silent.Test.Approval.NotReviewedCount != len(set.Suggestions) {
		t.Fatalf("the proposals nobody looked at are not reported: %+v", silent.Test.Approval)
	}

	// A review made over another run, and a decision naming no proposal, are
	// refused rather than applied to whatever is in front of them.
	for name, review := range map[string]*testauthor.Review{
		"a review of another run": {Result: "known-good", Identity: strings.Repeat("a", 64),
			Decisions: []testauthor.Decision{{Suggestion: set.Suggestions[0].ID, Approved: true}}},
		"a decision naming no proposal": {Result: "known-good", Identity: set.Origin.Identity,
			Decisions: []testauthor.Decision{{Suggestion: "nothing-proposed-this", Approved: true}}},
	} {
		refused := request
		refused.Review = review
		if result := app.ApproveExpectations(refused); result.State != desktop.Failed {
			t.Fatalf("%s was applied: %+v", name, result)
		}
	}
	missing := request
	missing.Review = nil
	if result := app.ApproveExpectations(missing); result.State != desktop.Failed {
		t.Fatalf("an approval naming no decisions was accepted: %+v", result)
	}
}
