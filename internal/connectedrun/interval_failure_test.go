package connectedrun_test

import (
	"context"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntervalStaleBaselinePreventsActualStimulus(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	p := intervalPrepared(t, dir, target)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(target.file, old, old); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "failed-arm")
	result, err := connectedrun.Execute(context.Background(), p, "run-one", output)
	if err != nil || result.State != "incomplete" || result.Verdict != assertion.VerdictUndecided || target.received.Load() != 0 {
		t.Fatal(result, err, target.received.Load())
	}
	if _, err = connectedrun.Open(context.Background(), output); err != nil {
		t.Fatal("partial arm evidence did not reopen", err)
	}
}
func TestIntervalRunnerCancellationRetainsPartialCoverageWithoutResend(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	target.notifications = make(chan int, 2)
	p := intervalPrepared(t, dir, target)
	clock := &intervalClock{base: time.Now().UTC(), waits: make(chan chan time.Duration)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type answer struct {
		r   connectedrun.Result
		err error
	}
	done := make(chan answer, 1)
	output := filepath.Join(dir, "cancelled")
	go func() {
		r, err := connectedrun.ExecuteWithClock(ctx, p, "run-one", output, clock)
		done <- answer{r, err}
	}()
	select {
	case <-clock.waits:
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not sample")
	}
	for i := 0; i < 2; i++ {
		select {
		case <-target.notifications:
		case <-time.After(5 * time.Second):
			t.Fatal("target did not receive")
		}
	}
	cancel()
	var got answer
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop runtime")
	}
	if got.err != nil || got.r.Verdict != assertion.VerdictUndecided || got.r.State == "complete" {
		t.Fatal(got.r, got.err)
	}
	before := target.received.Load()
	for i := 0; i < 2; i++ {
		if _, err := connectedrun.Open(context.Background(), output); err != nil {
			t.Fatal(err)
		}
	}
	if target.received.Load() != before || before != 2 {
		t.Fatal("reopen repeated stimulus")
	}
}
