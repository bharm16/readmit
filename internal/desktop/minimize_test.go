package desktop_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/testauthor"
)

// minimizeProject is a project holding the booking, reschedule and update
// case, an environment at the receiver whose one reset action a person
// confirms, and a test expecting every message accepted, which fails the
// reschedule there. It answers the failed run.
func minimizeProject(t *testing.T, receiver *reductionReceiver) (*desktop.App, desktop.RequestContext, desktop.ItemRef, desktop.ItemRef) {
	t.Helper()
	app, context := namedProject(t)
	root := context.Project
	written := writeCase(t, root, "incident", framed(rdBooking)+framed(rdReschedule)+framed(rdUpdate))
	registerCase(t, root, "incident", "Rescheduling incident")
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{IntentID: "environment", Draft: desktop.ItemDraft{Name: "Scheduling QA",
		Environment: targetDraft(receiver.address()),
		ResetPlan:   &fixturereset.Plan{Actions: []fixturereset.Action{{Operator: fixturereset.OperatorConfirms, Instructions: "Restart the receiver on an empty ledger."}}},
		Links:       &desktop.EnvironmentLinks{ResetName: "Empty ledger", ActionNames: []string{"Restart receiver"}}}})
	draft := ackTest(t, written.Identity, "Rescheduling is acknowledged")
	draft.Messages = slices.Clone(rdSequence)
	draft.Expectations = nil
	for i, occurrence := range rdSequence {
		draft.Expectations = append(draft.Expectations, testauthor.Expectation{ID: []string{"booking-accepted", rdAssertion, "update-accepted"}[i],
			Operator: testauthor.ACKFieldEquals, Message: occurrence, Selector: "MSA-1", Field: ackText("AA")})
	}
	test := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "test", Draft: desktop.ItemDraft{Test: draft, TestLinks: &desktop.TestLinks{Environment: environment.ID}}})
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	sent := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "run", Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset"}}})
	if sent.Run == nil {
		t.Fatalf("the failing run: %+v", sent)
	}
	return app, context, *sent.Run, environment
}

// A failed run is minimized from what the run itself recorded: the exact
// test version, its failed checks and its environment. The whole bounded
// series is one review; Start without the reset's manual step confirmed
// reaches nothing, and Start with it resets before every trial, keeps only
// what the failure needs and publishes that as a variant of the case.
func TestAFailedRunIsMinimizedByOneReviewedBoundedSeries(t *testing.T) {
	receiver := newReductionReceiver(t)
	app, context, run, environment := minimizeProject(t, receiver)
	sentBefore := receiver.received()

	setup := app.MinimizeSetup(desktop.RunRequest{Context: context, Run: run})
	if setup.State != desktop.Completed || setup.Setup == nil || !setup.Setup.Eligible || setup.Setup.Version != "1" || setup.Setup.Environment == nil ||
		setup.Setup.Environment.ID != environment.ID || len(setup.Setup.Failed) != 1 || setup.Setup.Failed[0].ID != rdAssertion || setup.Setup.Case == nil {
		t.Fatalf("setup: %+v", setup)
	}
	options := desktop.MinimizeOptions{Checks: []string{rdAssertion}, Grouping: reduce.GroupPerOccurrence, Trials: 16, Confirmations: 1}
	unbounded := options
	unbounded.Trials = 0
	if refused := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.MinimizeFailureAction, Items: []desktop.ItemRef{run}, Minimize: &unbounded}); refused.Ready || refused.Token != "" {
		t.Fatalf("an unbounded search was offered: %+v", refused)
	}
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.MinimizeFailureAction, Items: []desktop.ItemRef{run}, Minimize: &options})
	minimize := review.Minimize
	if !review.Ready || review.Consent != desktop.MinimizeConsent || minimize == nil || minimize.TestName != "Rescheduling is acknowledged" || minimize.Version != "1" ||
		minimize.EnvironmentName != "Scheduling QA" || len(minimize.Checks) != 1 || minimize.Groups != 3 || minimize.Pinned != 1 || minimize.Trials != 16 ||
		review.Reset == nil || len(review.Reset.Actions) != 1 || !slices.Contains(review.Requirements, desktop.ConfirmationsRequirement) {
		t.Fatalf("the review: %+v %+v", review, minimize)
	}
	manual := review.Reset.Actions[0].ID
	unconfirmed := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "start-unconfirmed"})
	if unconfirmed.Outcome != desktop.ActionRefused || unconfirmed.Minimize != nil || receiver.received() != sentBefore {
		t.Fatalf("a Start without the reset confirmed: %+v", unconfirmed)
	}
	started := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "start",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{manual}}})
	outcome := started.Minimize
	if started.State != desktop.Completed || started.Outcome != desktop.ActionCompleted || outcome == nil || outcome.Outcome != reduce.OutcomeReduced ||
		outcome.Minimality != reduce.GroupOneMinimal || len(outcome.Original) != 3 || len(outcome.Retained) != 2 || outcome.Variant == nil || outcome.Output == "" {
		t.Fatalf("the minimization: %+v %+v", started, outcome)
	}
	for _, trial := range outcome.Trials {
		if trial.Reset != fixturereset.Confirmed {
			t.Fatalf("a trial ran without its reset: %+v", trial)
		}
	}
	retained := []string{}
	for _, message := range outcome.Retained {
		retained = append(retained, message.ID)
	}
	if strings.Join(retained, " ") != rdSequence[0]+" "+rdSequence[1] {
		t.Fatalf("retained: %v", retained)
	}
	variant := listed(t, app, context.Project, desktop.VariantItem)["Rescheduling incident minimized"]
	if variant.Ref.ID != outcome.Variant.ID || variant.Summary.Variant == nil || variant.Summary.Variant.Parent == nil {
		t.Fatalf("the published variant: %+v", variant)
	}
	if progress := app.MinimizeProgress(); progress.Progress != nil {
		t.Fatalf("a finished minimization still reads as running: %+v", progress)
	}
	passing := app.MinimizeSetup(desktop.RunRequest{Context: context, Run: desktop.ItemRef{Kind: desktop.RunItem, ID: run.ID}})
	if !passing.Setup.Eligible {
		t.Fatalf("the failed run stopped being eligible: %+v", passing)
	}
}

// Stop during a trial ends the series there: the trial whose acknowledgement
// never came is uncertain, nothing is sent again, no minimum is claimed and
// no variant is published.
func TestAStoppedMinimizationClaimsNothingAndResendsNothing(t *testing.T) {
	receiver := newReductionReceiver(t)
	app, context, run, _ := minimizeProject(t, receiver)
	options := desktop.MinimizeOptions{Checks: []string{rdAssertion}, Grouping: reduce.GroupPerOccurrence, Trials: 16, Confirmations: 2}
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.MinimizeFailureAction, Items: []desktop.ItemRef{run}, Minimize: &options})
	before := receiver.received()
	receiver.holdAcknowledgements()
	done := make(chan desktop.ReviewedActionResult, 1)
	go func() {
		done <- app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "start-stopped",
			Decisions: desktop.ReviewDecisions{Confirmed: []string{review.Reset.Actions[0].ID}}})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for receiver.received() == before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if progress := app.MinimizeProgress(); progress.Progress == nil || progress.Progress.Current == nil || progress.Progress.Budget != 16 {
		t.Fatalf("the running trial is not shown: %+v", progress)
	}
	app.CancelOperation("start-stopped")
	stopped := <-done
	receiver.releaseAcknowledgement()
	sent := receiver.received()
	outcome := stopped.Minimize
	if outcome == nil || outcome.Minimality != reduce.NoClaim || outcome.Variant != nil || outcome.Outcome == reduce.OutcomeReduced ||
		stopped.Outcome != desktop.ActionUncertain && stopped.Outcome != desktop.ActionCancelled {
		t.Fatalf("a stopped minimization: %+v %+v", stopped, outcome)
	}
	time.Sleep(200 * time.Millisecond)
	if receiver.received() != sent || sent != before+1 {
		t.Fatalf("a stopped trial was sent again or continued: %d then %d after %d", sent, receiver.received(), before)
	}
}
