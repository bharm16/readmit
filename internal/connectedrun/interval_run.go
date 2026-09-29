package connectedrun

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"path/filepath"
	"sync"
	"time"
)

type stimulusFinish struct {
	Plan      string    `json:"plan"`
	Instance  string    `json:"instance"`
	Transport string    `json:"transport"`
	State     string    `json:"state"`
	At        time.Time `json:"at"`
}
type IntervalRun struct {
	Boundaries   map[string]string   `json:"boundaries"`
	Schema       string              `json:"schema"`
	Summary      Result              `json:"summary"`
	Intervals    map[string]string   `json:"intervals"`
	Acquisitions map[string][]string `json:"acquisitions"`
}

var intervalFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected interval execution", AllowedDirectories: []string{"observations", "intervals"}, Nested: []string{"plan", "transport", "observations", "intervals", "evaluation"}, RequiredFiles: []string{"started.json", "manifest.json", "identity.sha256"}, AllowFile: func(n string) bool {
	return n == "started.json" || n == "stimulus-intent.json" || n == "stimulus-finished.json" || n == "manifest.json" || n == "identity.sha256"
}, MaxFiles: 100000, MaxFileBytes: 64 << 20, MaxBytes: 512 << 20}, Seal: artifactdir.DirectoryHash(SchemaV2)}

type runningInterval struct {
	source   sourcePlan
	session  *observeinterval.Session
	capture  *observeinterval.Capture
	last     *dataset.Snapshot
	barrier  *dataset.Snapshot
	sequence int
	result   observeinterval.Result
	err      error
}

// ExecuteWithClock is the deterministic lifecycle seam. Production callers use
// Execute, which always supplies the process's monotonic clock.
func ExecuteWithClock(ctx context.Context, p *Prepared, instance, output string, clock observeinterval.Clock) (Result, error) {
	if p == nil || !p.intervals || !safeID(instance) || clock == nil || p.unchanged() != nil {
		return Result{}, invalid
	}
	ctx, cancel := context.WithTimeout(ctx, duration(p.plan.Document().Test.Limits.DeadlineMS))
	defer cancel()
	authority := liveAuthority{prepared: p, authority: p.send.authority()}
	if _, err := authority.Check(ctx, p.transport.Binding()); err != nil {
		return Result{}, err
	}
	// A phase whose declared delays the remaining budget cannot hold is
	// refused before anything is armed or sent.
	if !p.transport.Holds(ctx) {
		return Result{}, invalid
	}
	schema := SchemaV2
	if p.sequence {
		schema = PhaseSchema
	}
	if p.transport.Scheduled() {
		schema = ScheduledPhaseSchema
	}
	f := intervalFamily
	f.Seal = artifactdir.DirectoryHash(schema)
	w, err := artifactdir.Create(output, f, artifactdir.Durable)
	if err != nil {
		return Result{}, err
	}
	defer w.Close()
	r := IntervalRun{Boundaries: map[string]string{}, Schema: schema, Summary: Result{Schema: schema, Plan: p.plan.Identity(), Instance: instance, State: "incomplete", Verdict: assertion.VerdictUndecided, Phase: "arming", StartedAt: time.Now().UTC(), Observations: map[string]string{}, Armed: map[string]string{}}, Intervals: map[string]string{}, Acquisitions: map[string][]string{}}
	if put(w, "started.json", r.Summary) != nil || w.Mkdir("observations") != nil || w.Mkdir("intervals") != nil || p.plan.Write(ctx, filepath.Join(w.Path(), "plan")) != nil || w.Sync() != nil {
		return Result{}, invalid
	}
	intervals := []*runningInterval{}
	finish := func() (Result, error) {
		// Quiesce every writer before sealing even an early arming failure. A
		// cancelled listener can still be flushing its recoverable spool.
		if r.Summary.Phase == "arming" {
			for _, state := range intervals {
				if state.capture != nil {
					final, e := state.capture.Finalize(context.WithoutCancel(ctx))
					if e == nil {
						_ = state.session.Append(context.WithoutCancel(ctx), final)
					}
				}
				completed, e := state.session.Finish(context.WithoutCancel(ctx))
				if e == nil {
					state.result = completed
					r.Intervals[state.source.definition.ID] = completed.Identity
					r.Boundaries[state.source.definition.ID] = completed.Boundary
					if completed.FinalSnapshot != "" {
						r.Summary.Observations[state.source.definition.ID] = "intervals/" + state.source.definition.ID + "/" + completed.FinalSnapshot
					}
				}
			}
		}
		for _, state := range intervals {
			if state.capture != nil {
				_ = state.capture.Close()
			}
		}

		r.Summary.CompletedAt = time.Now().UTC()
		if put(w, "manifest.json", r) != nil {
			return Result{}, invalid
		}
		if _, err := w.Seal(nil); err != nil {
			return Result{}, err
		}
		return Open(context.WithoutCancel(ctx), w.Path())
	}
	var acquired sync.Mutex
	acquire := func(ctx context.Context, s sourcePlan, phase, name string) (*dataset.Snapshot, error) {
		request := observesource.DatasetRequest{Source: s.source, Projection: s.projection, Binding: dataset.Binding{Run: instance, Phase: phase, Namespace: s.definition.Namespace, Source: s.definition.Source}, Output: filepath.Join(w.Path(), "observations", name), Network: s.http, DatabaseNetwork: s.database, Authorize: func(ctx context.Context) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return p.unchanged()
		}}
		if s.http != nil || s.database != nil {
			request.NetworkAuthority = liveAuthority{prepared: p, authority: s.grant.authority()}
		}
		snapshot, err := observesource.CollectDataset(ctx, request)
		if err == nil {
			acquired.Lock()
			r.Acquisitions[s.definition.ID] = append(r.Acquisitions[s.definition.ID], name)
			acquired.Unlock()
		}
		return snapshot, err
	}
	observation := func(snapshot *dataset.Snapshot, s sourcePlan) observeinterval.Observation {
		o := observeinterval.Observation{Binding: dataset.Binding{Run: instance, Phase: s.definition.Phase, Namespace: s.definition.Namespace, Source: s.definition.Source}, Status: "healthy", Snapshot: snapshot}
		if snapshot == nil {
			o.Status = "collector-failed"
			return o
		}
		doc := snapshot.Document()
		o.Binding = doc.Binding
		if !snapshot.Usable() {
			o.Status = "unusable-snapshot"
		}
		if doc.Acquisition.Facts != nil {
			o.SourceAt = doc.Acquisition.Facts.AsOf
		}
		return o
	}
	evidence := map[string]*dataset.Snapshot{}
	// Every listener is open and every source baseline is retained before the
	// single stimulus intent is committed. A failed arm cannot reach a send.
	for _, source := range p.sources {
		r.Boundaries[source.definition.ID] = "not-armed"
	}
	for _, source := range p.sources {
		if source.definition.Phase == "before" {
			snapshot, err := acquire(ctx, source, "before", "before-"+source.definition.ID)
			if err != nil || !snapshot.Usable() {
				return finish()
			}
			evidence[source.definition.ID] = snapshot
			r.Boundaries[source.definition.ID] = "verified-baseline-snapshot"
			r.Summary.Armed[source.definition.ID] = snapshot.Identity()
			r.Summary.Observations[source.definition.ID] = "before-" + source.definition.ID
			continue
		}
		state := &runningInterval{source: source}
		binding := dataset.Binding{Run: instance, Phase: "after", Namespace: source.definition.Namespace, Source: source.definition.Source}
		state.session, err = observeinterval.Arm(ctx, *source.interval, binding, filepath.Join(w.Path(), "intervals", source.definition.ID), clock)
		if err != nil {
			return finish()
		}
		defer state.session.Close()
		intervals = append(intervals, state)
		var baseline observeinterval.Observation
		if source.capture != nil {
			state.capture, err = observeinterval.ArmCapture(ctx, source.captureRaw, source.capture, liveAuthority{prepared: p, authority: source.grant.authority()}, binding, source.projection, filepath.Join(state.session.Path(), "capture"), nil)
			if err != nil {
				return finish()
			}
			defer state.capture.Stop()
			baseline, err = state.capture.Poll(ctx)
			r.Summary.Armed[source.definition.ID] = source.capture.Binding().Configuration
		} else {
			snapshot, readErr := acquire(ctx, source, "before", "armed-"+source.definition.ID)
			err = readErr
			baseline = observation(snapshot, source)
			if snapshot != nil {
				r.Summary.Armed[source.definition.ID] = snapshot.Identity()
			}
		}
		if source.barrier != nil {
			var barrierErr error
			baseline.BarrierSnapshot, barrierErr = acquire(ctx, *source.barrier, "after", "armed-"+source.definition.ID+"-barrier")
			if barrierErr != nil {
				err = barrierErr
			}
		}
		if err != nil || state.session.Append(ctx, baseline) != nil || state.session.StimulusStarted() != nil {
			return finish()
		}
	}
	observeCtx, stopObserving := context.WithCancel(ctx)
	defer stopObserving()
	stimulusDone := make(chan struct{})
	var group sync.WaitGroup
	for _, state := range intervals {
		group.Add(1)
		go func() {
			defer group.Done()
			state.err = state.session.Observe(observeCtx, func(ctx context.Context) (observeinterval.Observation, error) {
				state.sequence++
				var o observeinterval.Observation
				var err error
				if state.source.barrier != nil {
					var e error
					state.barrier, e = acquire(ctx, *state.source.barrier, "after", fmt.Sprintf("%s-barrier-%04d", state.source.definition.ID, state.sequence))
					if e != nil {
						return observeinterval.Observation{}, e
					}
				}
				if state.capture != nil {
					o, err = state.capture.Poll(ctx)
				} else {
					state.last, err = acquire(ctx, state.source, "after", fmt.Sprintf("%s-%04d", state.source.definition.ID, state.sequence))
					o = observation(state.last, state.source)
				}
				o.BarrierSnapshot = state.barrier
				return o, err
			}, stimulusDone)
			if state.err != nil {
				stopObserving()
			}
			if state.capture != nil {
				final, e := state.capture.Finalize(context.WithoutCancel(ctx))
				if e == nil {
					state.last = final.Snapshot
					final.BarrierSnapshot = state.barrier
					e = state.session.Append(context.WithoutCancel(ctx), final)
				}
				if e != nil {
					state.err = e
				}
			}
			result, finishErr := state.session.Finish(observeCtx)
			state.result = result
			if finishErr != nil {
				state.err = finishErr
			}
			if state.err != nil || !state.result.Sufficient() {
				stopObserving()
			}
		}()
	}
	r.Summary.Phase = "stimulus"
	if put(w, "stimulus-intent.json", p.transport.Binding()) != nil || w.Sync() != nil {
		stopObserving()
		group.Wait()
		return Result{}, invalid
	}
	transport, sendErr := connectedtransport.Execute(observeCtx, p.transport, authority, instance, filepath.Join(w.Path(), "transport"), nil)
	r.Summary.Transport = transport.RunIdentity
	r.Summary.SentAt = time.Now().UTC()
	if sendErr != nil || transport.State == "uncertain" {
		r.Summary.State = "uncertain"
	}
	close(stimulusDone)
	if put(w, "stimulus-finished.json", stimulusFinish{Plan: r.Summary.Plan, Instance: instance, Transport: r.Summary.Transport, State: transport.State, At: r.Summary.SentAt}) != nil || w.Sync() != nil {
		stopObserving()
		group.Wait()
		return Result{}, invalid
	}
	if sendErr != nil {
		stopObserving()
	}
	group.Wait()
	complete := sendErr == nil
	for _, state := range intervals {
		r.Boundaries[state.source.definition.ID] = "insufficient"
		if state.result.Identity != "" {
			r.Boundaries[state.source.definition.ID] = state.result.Boundary
			r.Intervals[state.source.definition.ID] = state.result.Identity
		}
		if state.err != nil || !state.result.Sufficient() {
			complete = false
		}
		if state.last != nil {
			evidence[state.source.definition.ID] = state.last
		}
		// A cancelled subsequent acquisition can clear last while the interval
		// still retains its previous final snapshot. Name the journal's actual
		// retained evidence even when coverage is insufficient.
		if state.result.FinalSnapshot != "" {
			r.Summary.Observations[state.source.definition.ID] = "intervals/" + state.source.definition.ID + "/" + state.result.FinalSnapshot
		}
	}
	if !complete || ctx.Err() != nil {
		return finish()
	}
	run, err := replay.Open(filepath.Join(w.Path(), "transport", "run"))
	if err != nil {
		return finish()
	}
	execution := connectedtest.Execution{Instance: instance, State: "complete", Engine: engine.Version(), Setup: "operator-declared", Cleanup: "not-requested", Attempts: []connectedtest.Attempt{}}
	for i, event := range run.Events {
		attempt := connectedtest.Attempt{Step: p.plan.Document().Order[i], Kind: "v2-send", Outcome: "complete"}
		if event.Delivery != "acknowledged" {
			attempt.Outcome = "unknown"
			attempt.Uncertain = event.Delivery == "uncertain"
			execution.State = "uncertain"
		}
		execution.Attempts = append(execution.Attempts, attempt)
	}
	result, err := connectedtest.RetainDatasetResult(ctx, p.plan, execution, evidence, filepath.Join(w.Path(), "evaluation"))
	if err != nil {
		return finish()
	}
	encoded, _ := json.Marshal(result, json.Deterministic(true))
	r.Summary.Evaluation = networkaction.Digest(encoded)
	r.Summary.Verdict = result.Verdict
	r.Summary.State = execution.State
	r.Summary.Phase = "finished"
	return finish()
}

const PhaseSchema = "readmit-connected-phase/v1"

// ScheduledPhaseSchema is a scheduled lifecycle's phase: its transport is
// readmit-connected-transport/v3, holding each occurrence's declared delay.
const ScheduledPhaseSchema = "readmit-connected-phase/v3"
