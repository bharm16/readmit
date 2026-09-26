package desktop

import (
	"context"
	"testing"
	"time"
)

// A review expires fifteen minutes after it was prepared, and nothing renews
// it.
func TestAReviewExpiresAndIsNeverRenewed(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	app := &App{clock: func() time.Time { return now }}
	bound := &boundAction{action: ReplaySendAction, binding: "bound", review: ActionReview{Ready: true}}
	review, err := app.issue(bound, actionPolicies[ReplaySendAction])
	if err != nil || review.Token == "" || review.ExpiresAt != "2026-09-26T12:15:00Z" {
		t.Fatalf("issue: %+v %v", review, err)
	}
	now = now.Add(reviewLifetime)
	expired := app.ExecuteReviewedAction(ExecuteActionRequest{Token: review.Token, IntentID: "late"})
	if expired.Outcome != ActionExpired {
		t.Fatalf("an expired review: %+v", expired)
	}
	if again := app.ExecuteReviewedAction(ExecuteActionRequest{Token: review.Token, IntentID: "later"}); again.Outcome != ActionRefused {
		t.Fatalf("an expired review was renewed: %+v", again)
	}
}

// A review is bound to the reviewer who prepared it: when the reviewer the
// window acts as changes — another local account, a hub sign-in or sign-out —
// the final action is a stale review and executes nothing.
func TestAReviewIsBoundToItsReviewer(t *testing.T) {
	const probe ActionID = "test.probe"
	executed := 0
	actionPolicies[probe] = actionPolicy{
		consent: SendConsent,
		bind: func(a *App, _ context.Context, request PrepareActionRequest, _ bool) (*boundAction, refusal) {
			return &boundAction{action: probe, origin: request, binding: binding(string(probe), a.reviewer()), review: ActionReview{Ready: true}}, refusal{}
		},
		execute: func(*App, context.Context, *boundAction, ReviewDecisions) ReviewedActionResult {
			executed++
			return ReviewedActionResult{State: Completed, Outcome: ActionCompleted}
		},
	}
	t.Cleanup(func() { delete(actionPolicies, probe) })
	reviewer := "local:1:reviewer-a"
	app := New(nil, ShellDocuments{Folder: t.TempDir()})
	app.actor = func() string { return reviewer }
	review := app.PrepareAction(PrepareActionRequest{Action: probe})
	if review.Review == nil || review.Review.Token == "" {
		t.Fatalf("%+v", review)
	}
	reviewer = "local:1:reviewer-a|hub:issuer:someone-else"
	if stale := app.ExecuteReviewedAction(ExecuteActionRequest{Token: review.Review.Token, IntentID: "click"}); stale.Outcome != ActionStale || executed != 0 {
		t.Fatalf("another reviewer executed the review: %+v", stale)
	}
	fresh := app.PrepareAction(PrepareActionRequest{Action: probe})
	if done := app.ExecuteReviewedAction(ExecuteActionRequest{Token: fresh.Review.Token, IntentID: "click-again"}); done.Outcome != ActionCompleted || executed != 1 {
		t.Fatalf("the reviewer's own review: %+v", done)
	}
}
