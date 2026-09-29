package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testisolation"
)

// A successful acquisition may still violate a tighter observation gap. This
// independently holds a coherent HTTP response after the actual stimulus while
// staying within its ten-second acquisition budget. The parent has thirty
// seconds: neither transport completion nor parent time remaining proves the
// declared sampling coverage.
func TestFlowResumeBudgetSeparatesAcquisitionFromCoverage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		maxGapMS int64
		ready    bool
	}{{"gap-shorter-than-admitted-acquisition", 1000, false}, {"resume-fixture-gap-covers-parent-budget", 30000, true}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newFlowContractHarnessWithSource(t, true)
			d, files := flowWireDependencies(t, h)
			if d.Limits.DeadlineMS != 30000 {
				t.Fatal("diagnostic parent budget changed")
			}
			for i := range d.Phases {
				for j := range d.Phases[i].Datasets {
					ds := &d.Phases[i].Datasets[j]
					projection, err := dataset.DecodeProjection(files[ds.Projection.File])
					if err != nil || projection.Limits.TimeoutMS != flowContractIOBudget.Milliseconds() {
						t.Fatal("diagnostic acquisition budget changed", err)
					}
					definition, err := observeinterval.Decode(files[ds.Completion.Policy.File])
					if err != nil || definition.HorizonMS != 120 || definition.SampleMS != 20 {
						t.Fatal("diagnostic business horizon changed", err)
					}
					definition.MaxGapMS = tc.maxGapMS
					files[ds.Completion.Policy.File], err = json.Marshal(definition)
					if err != nil {
						t.Fatal(err)
					}
					ds.Completion.Policy.SHA256 = dataset.Digest(files[ds.Completion.Policy.File])
				}
			}
			var config connectedrun.FlowConfig
			flowContractRead(t, h.configPath, &config)
			store := filepath.Join(h.root, "resume-budget-store")
			if err := os.Mkdir(store, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(store, "store-id"), []byte(networkaction.Digest([]byte("resume budget test store"))), 0600); err != nil {
				t.Fatal(err)
			}
			config.RecoveryStore = store
			flowWireInstall(t, h, d, files, config)
			p := h.prepare(t, "resume-budget")
			held := make(chan time.Time, 1)
			release := make(chan struct{})
			var closeOnce sync.Once
			unblock := func() { closeOnce.Do(func() { close(release) }) }
			defer unblock()
			var claimed atomic.Bool
			var requestCancelled atomic.Bool
			h.fixture.mu.Lock()
			h.fixture.beforeSnapshot = func(ctx context.Context) {
				if h.fixture.target.received.Load() == 0 || !claimed.CompareAndSwap(false, true) {
					return
				}
				held <- time.Now()
				select {
				case <-release:
				case <-ctx.Done():
					requestCancelled.Store(true)
				}
			}
			h.fixture.mu.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type answer struct {
				result connectedrun.FlowResult
				err    error
			}
			done := make(chan answer, 1)
			output := filepath.Join(h.root, "resume-budget")
			go func() {
				r, err := connectedrun.ExecuteFlow(ctx, p, output, testisolation.Confirmation{}, func(phase connectedrun.FlowPhaseResult) {
					if tc.ready && phase.ID == "booking" && phase.State == "complete" && phase.Verdict == assertion.VerdictPass {
						cancel()
					}
				})
				done <- answer{r, err}
			}()
			var began time.Time
			select {
			case began = <-held:
			case got := <-done:
				t.Fatalf("run settled before controlled response: state=%s err=%v", got.result.State, got.err)
			case <-time.After(15 * time.Second):
				t.Fatal("controlled after-stimulus HTTP response was never requested")
			}
			// The response is released 1.2 seconds after it was requested, timed
			// from the request itself: the checks below must not push the release
			// into the remaining acquisition budget on a slow runner. Authority,
			// configuration and TLS work before the request also spend that budget.
			releaseTimer := time.AfterFunc(time.Until(began.Add(1200*time.Millisecond)), unblock)
			defer releaseTimer.Stop()
			waitStimulusFinish(t, t.Context(), filepath.Join(output, "phases", "booking"))
			run, err := replay.Open(filepath.Join(output, "phases", "booking", "transport", "run"))
			if err != nil || len(run.Events) != 1 || run.Events[0].Delivery != "acknowledged" {
				t.Fatal("the held observation must follow an actually acknowledged send", err)
			}
			var got answer
			select {
			case got = <-done:
			case <-time.After(15 * time.Second):
				t.Fatal("released response did not settle lifecycle")
			}
			if got.err != nil || requestCancelled.Load() || h.fixture.target.received.Load() != 1 {
				t.Fatalf("acquisition did not finish inside its declared budget: err=%v cancelled=%t sends=%d", got.err, requestCancelled.Load(), h.fixture.target.received.Load())
			}
			interval, err := observeinterval.Open(t.Context(), filepath.Join(output, "phases", "booking", "intervals", "after"))
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range interval.Records {
				if record.Kind == "baseline" || record.Kind == "sample" {
					if record.Status != "healthy" {
						t.Fatalf("source failure confounded coverage diagnostic: %+v", record)
					}
				}
			}
			acquisitions, err := filepath.Glob(filepath.Join(output, "phases", "booking", "observations", "after-*"))
			if err != nil || len(acquisitions) == 0 {
				t.Fatal("missing actual after acquisition", err)
			}
			for _, path := range acquisitions {
				snapshot, err := observesource.OpenDataset(t.Context(), path)
				if err != nil || !snapshot.Usable() || snapshot.Document().Acquisition.Facts.HTTPStatus != 200 {
					t.Fatal("HTTP acquisition itself failed", err)
				}
			}
			if tc.ready {
				if !interval.Sufficient() || got.result.State != "cancelled" || got.result.Phases[0].State != "complete" || got.result.Phases[0].Verdict != assertion.VerdictPass || got.result.Phases[1].State != "not-attempted" {
					t.Fatalf("corrected non-timing fixture failed to establish its resume checkpoint: %+v reason=%s", got.result, interval.Reason)
				}
				if _, err := connectedrun.PrepareFlowResume(p, output); err != nil {
					t.Fatal("verified untouched suffix was not resumable", err)
				}
			} else {
				if ctx.Err() != nil || interval.Reason != "lost-coverage" || interval.Sufficient() || got.result.Phases[0].State != "incomplete" || got.result.Phases[0].Verdict != assertion.VerdictUndecided || got.result.Verdict == assertion.VerdictPass {
					t.Fatalf("admitted acquisition did not expose independent coverage failure: %+v reason=%s context=%v", got.result, interval.Reason, ctx.Err())
				}
				if _, err := connectedrun.PrepareFlowResume(p, output); err == nil {
					t.Fatal("incomplete observation became a resumable completed prefix")
				}
			}
		})
	}
}
