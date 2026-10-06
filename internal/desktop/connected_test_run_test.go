package desktop_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/desktop"
)

func TestSavedConnectedTestNormalRunReviewsExactRevisionBeforeAnyEffects(t *testing.T) {
	f := newConnectedAuthoring(t)
	test := f.save(t, "Reschedule observed state", "individual-connected", f.reschedule(), f.v2.ID)
	before := f.lab.Creates.Load()
	prepared := f.app.PrepareAction(desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	if prepared.State != desktop.Completed || prepared.Review == nil || !prepared.Review.Ready || prepared.Review.Run == nil || prepared.Review.Run.Test == nil || *prepared.Review.Run.Test != test {
		t.Fatalf("normal Run did not review saved connected revision: %+v", prepared)
	}
	if f.lab.Creates.Load() != before {
		t.Fatal("preparing Run executed a lifecycle effect")
	}
}

func TestSavedConnectedNormalRunExecutesUnchangedExpectationsAcrossDefectFixRegression(t *testing.T) {
	parallelLifecycleTest(t)
	f := newConnectedAuthoring(t)
	test := f.save(t, "Reschedule observed state", "connected-regression", f.reschedule(), f.v2.ID)
	request := desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}}
	for i, mode := range []string{"defective", "fixed", "defective"} {
		f.engine.SetMode(mode)
		review := prepared(t, f.app, request)
		result := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: fmt.Sprintf("connected-run-%d", i)})
		if result.State != desktop.Completed || result.Lifecycle == nil {
			t.Fatalf("actual %s lifecycle: %+v", mode, result)
		}
		want := assertion.VerdictFail
		if mode == "fixed" {
			want = assertion.VerdictPass
		}
		if result.Lifecycle.Verdict != want || result.Lifecycle.Cleanup != "complete" {
			t.Fatalf("actual %s outcome: %+v", mode, result.Lifecycle)
		}
		if result.Run == nil {
			t.Fatalf("normal run did not retain discoverable test association: %+v", result)
		}
		detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *result.Run, Reveal: true})
		if detail.State != desktop.Completed || detail.Run == nil || detail.Run.Lifecycle == nil {
			t.Fatalf("actual retained evidence: %+v", detail)
		}
		found := false
		for _, check := range detail.Run.Lifecycle.Checks {
			if check.ID == "typed:one-appointment" {
				found = true
				wanted := 2
				if mode == "fixed" {
					wanted = 1
				}
				if check.ExpectedCount == nil || *check.ExpectedCount != 1 || check.ObservedCount == nil || *check.ObservedCount != wanted {
					t.Fatalf("expected/observed came from wrong evidence: %+v", check)
				}
			}
		}
		if !found {
			t.Fatal("retained check disappeared")
		}
		if len(detail.Run.Lifecycle.Steps) != 2 || detail.Run.Lifecycle.Steps[0].ACK != "AA" || detail.Run.Lifecycle.Steps[1].ACK != "AA" {
			t.Fatalf("transport receipts: %+v", detail.Run.Lifecycle.Steps)
		}

	}
}

func TestSavedConnectedNormalFHIRRunRetainsResponsesAndBoundedObservations(t *testing.T) {
	parallelLifecycleTest(t)
	f := newConnectedAuthoring(t)
	test := f.save(t, "FHIR normal run", "individual-fhir", f.booking(), f.fhir.ID)
	review := prepared(t, f.app, desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	result := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: review.Token, IntentID: "normal-fhir-run"})
	if result.State != desktop.Completed || result.Run == nil || result.Lifecycle == nil || result.Lifecycle.Verdict != assertion.VerdictPass {
		t.Fatalf("actual FHIR lifecycle: %+v", result)
	}
	detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *result.Run})
	if detail.Run == nil || detail.Run.Lifecycle == nil || len(detail.Run.Lifecycle.Steps) != 2 || detail.Run.Lifecycle.Steps[0].HTTPStatus != 201 || detail.Run.Lifecycle.Steps[1].HTTPStatus != 200 {
		t.Fatalf("actual HTTP receipts: %+v", detail)
	}
	before := f.lab.Creates.Load()
	request := desktop.ConnectedObservationRequest{Context: f.context, Run: *result.Run, Phase: "create", Dataset: "appointments", Limit: 1}
	masked := f.app.ReadConnectedObservation(request)
	if masked.State != desktop.Completed || !masked.Available || !masked.Hidden || masked.Total != 1 || len(masked.Rows) != 1 || masked.Identity == "" {
		t.Fatalf("masked retained page: %+v", masked)
	}
	for _, value := range masked.Rows[0].Values {
		if value.Text != "" {
			t.Fatalf("retained value exposed without Reveal: %+v", value)
		}
	}
	request.Reveal = true
	revealed := f.app.ReadConnectedObservation(request)
	if revealed.State != desktop.Completed || revealed.Hidden || revealed.Identity != masked.Identity || len(revealed.Rows) != 1 {
		t.Fatalf("revealed retained page: %+v", revealed)
	}
	present := false
	for _, value := range revealed.Rows[0].Values {
		present = present || value.Text != ""
	}
	if !present {
		t.Fatal("Reveal did not read actual retained values")
	}
	request.Offset = 1
	empty := f.app.ReadConnectedObservation(request)
	if empty.State != desktop.Completed || !empty.Available || empty.Total != 1 || len(empty.Rows) != 0 {
		t.Fatalf("bounded page: %+v", empty)
	}
	request.Limit = 201
	if refused := f.app.ReadConnectedObservation(request); refused.State != desktop.Failed {
		t.Fatalf("unbounded page admitted: %+v", refused)
	}
	request.Limit = 1
	request.Dataset = "absent"
	if absent := f.app.ReadConnectedObservation(request); absent.Available || absent.Reason == "" {
		t.Fatalf("unavailable observation claimed absence: %+v", absent)
	}
	if f.lab.Creates.Load() != before {
		t.Fatal("reading retained evidence repeated lifecycle effects")
	}
}

func TestSavedConnectedRunRefusesExpiredReviewAndChangedEnvironmentBeforeEffects(t *testing.T) {
	f := newConnectedAuthoring(t)
	test := f.save(t, "Expiry and target fences", "fenced-connected", f.reschedule(), f.v2.ID)
	request := desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}}
	clock := time.Now()
	desktop.SetClockForTest(f.app, func() time.Time { return clock })
	reviewed := prepared(t, f.app, request)
	before := f.lab.Creates.Load()
	clock = clock.Add(16 * time.Minute)
	expired := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: reviewed.Token, IntentID: "expired-connected"})
	if expired.Outcome != desktop.ActionExpired || f.lab.Creates.Load() != before {
		t.Fatalf("expired review reached effects: %+v", expired)
	}
	clock = time.Now()
	reviewed = prepared(t, f.app, request)
	opened := f.app.OpenItemDraft(desktop.ItemRequest{Context: f.context, Ref: f.v2})
	if opened.Draft == nil || opened.Draft.Environment == nil {
		t.Fatalf("environment: %+v", opened)
	}
	opened.Draft.Environment.MessageTimeout = "59s"
	changed := f.app.SaveItem(desktop.SaveItemRequest{Context: f.context, Kind: desktop.EnvironmentItem, Item: f.v2.ID, BaseRevision: f.v2.Revision, IntentID: "changed-connected-target", Draft: *opened.Draft})
	if changed.Saved == nil {
		t.Fatalf("change: %+v", changed)
	}
	stale := f.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: f.context, Token: reviewed.Token, IntentID: "stale-connected"})
	if stale.Outcome != desktop.ActionStale || f.lab.Creates.Load() != before {
		t.Fatalf("changed target reached effects: %+v", stale)
	}
}

func TestSavedConnectedRunDoubleSubmitAndCancellationKeepActualUncertainty(t *testing.T) {
	f := newConnectedAuthoring(t)
	test := f.save(t, "Cancel and deduplicate", "cancel-connected", f.reschedule(), f.v2.ID)
	request := desktop.PrepareActionRequest{Context: f.context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}}
	reviewed := prepared(t, f.app, request)
	held := make(chan struct{})
	reached := make(chan struct{}, 1)
	f.engine.SetAfter(func() {
		select {
		case reached <- struct{}{}:
		default:
		}
		<-held
	})
	defer close(held)
	click := desktop.ExecuteActionRequest{Context: f.context, Token: reviewed.Token, IntentID: "cancel-connected-run"}
	done := make(chan desktop.ReviewedActionResult, 1)
	repeated := make(chan desktop.ReviewedActionResult, 1)
	go func() { done <- f.app.ExecuteReviewedAction(click) }()
	select {
	case <-reached:
	case <-time.After(30 * time.Second):
		t.Fatal("actual stimulus never reached independent engine")
	}
	go func() { repeated <- f.app.ExecuteReviewedAction(click) }()
	f.app.CancelOperation(click.IntentID)
	var result desktop.ReviewedActionResult
	select {
	case result = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cancel did not settle owned lifecycle")
	}
	if result.Lifecycle == nil || result.Lifecycle.State != "uncertain" || result.Outcome != desktop.ActionUncertain || result.Run == nil {
		t.Fatalf("cancel lost actual delivery uncertainty: %+v", result)
	}
	again := <-repeated
	if !again.Replayed || again.Lifecycle.Output != result.Lifecycle.Output {
		t.Fatalf("double submit dispatched independently: %+v", again)
	}
	detail := f.app.OpenRun(desktop.RunRequest{Context: f.context, Run: *result.Run})
	if detail.Run == nil || !detail.Run.DeliveryUncertain || detail.Run.Result == desktop.RunPassed {
		t.Fatalf("uncertain retained result became passing: %+v", detail)
	}
}
