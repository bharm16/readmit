package desktop

import (
	"context"
	"testing"
	"time"
)

// A capture that stopped keeps three answers apart: a person's cancellation,
// reaching the bound on an admitted execution, and an orderly end. Only the
// first is cancelled, even when the serve itself finalized without error.
func TestAStoppedCaptureSaysWhoStoppedIt(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, stopped := range []struct {
		name  string
		ctx   context.Context
		state State
		phase CapturePhase
	}{
		{"cancelled", cancelled, Cancelled, CaptureStopping},
		{"bounded", expired, Failed, CaptureFailed},
		{"orderly", context.Background(), Completed, CaptureStopped},
	} {
		got := (&App{}).finishCapture(stopped.ctx, CaptureSessionResult{}, nil, nil, CaptureListening)
		if got.State != stopped.state || got.Phase != stopped.phase {
			t.Errorf("%s: %+v, want %s in phase %s", stopped.name, got, stopped.state, stopped.phase)
		}
		if stopped.state == Cancelled && got.Reason != cancelledRefusal.reason {
			t.Errorf("a cancellation reads %q", got.Reason)
		}
		if stopped.name == "bounded" && got.Reason == cancelledRefusal.reason {
			t.Errorf("reaching the execution bound reads as a cancellation: %+v", got)
		}
	}
}
