package observeinterval_test

import (
	"context"
	"testing"

	"github.com/bharm16/readmit/internal/observeinterval"
)

func TestOwnedCaptureV2RetainsPreStimulusTrafficButCreditsOnlyAfterCheckpoint(t *testing.T) {
	session, capture, clock, _, binding := liveCapture(t, 10, captureOptions{owned: true, deferStimulus: true})
	// This independent sender has the correct runtime and key, but its actual
	// bytes arrive before the retained stimulus checkpoint.
	sendIndependent(t, capture.Address(), binding.Run, 1)
	if session.StimulusStarted() != nil {
		t.Fatal("stimulus checkpoint")
	}
	capture.ScopeAfter(session.StimulusTime())
	sendIndependent(t, capture.Address(), binding.Run, 1)
	sample, err := capture.Poll(context.Background())
	if err != nil || session.Append(context.Background(), sample) != nil {
		t.Fatal("scope poll", err)
	}
	if session.StimulusFinished() != nil {
		t.Fatal("completion checkpoint")
	}
	clock.elapsed = 100
	sample, err = capture.Poll(context.Background())
	if err != nil || session.Append(context.Background(), sample) != nil {
		t.Fatal("horizon poll", err)
	}
	final, err := capture.Finalize(context.Background())
	if err != nil || final.Snapshot == nil || len(final.Snapshot.Document().Rows) != 1 {
		t.Fatalf("pre-stimulus output credited: %+v %v", final, err)
	}
	if final.Excluded["s0001-e000001"] != "pre-stimulus-or-unavailable-time" {
		t.Fatalf("original exclusion lost: %+v", final.Excluded)
	}
	if session.Append(context.Background(), final) != nil {
		t.Fatal("scope evidence not appended")
	}
	result, err := session.Finish(context.Background())
	if err != nil || !result.Sufficient() {
		t.Fatalf("complete bounded scope: %+v %v", result, err)
	}
	reopened, err := observeinterval.Open(context.Background(), session.Path())
	if err != nil || !reopened.Sufficient() || reopened.Identity != result.Identity {
		t.Fatalf("retained scope checkpoint not verified: %+v %v", reopened, err)
	}
}
