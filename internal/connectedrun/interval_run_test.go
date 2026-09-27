package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Each wait is a handshake: the preceding acquisition has finished before the
// test changes exported state or advances the monotonic clock. No sleep races.
type intervalClock struct {
	mu      sync.Mutex
	elapsed int64
	base    time.Time
	waits   chan chan time.Duration
}

func (c *intervalClock) Now() observeinterval.Stamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	return observeinterval.Stamp{At: c.base.Add(time.Duration(c.elapsed) * time.Millisecond), MonotonicMS: c.elapsed}
}
func (c *intervalClock) Wait(ctx context.Context, d time.Duration) error {
	advance := make(chan time.Duration)
	select {
	case c.waits <- advance:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case step := <-advance:
		c.mu.Lock()
		c.elapsed += step.Milliseconds()
		c.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func intervalPrepared(t *testing.T, dir string, s *target) *connectedrun.Prepared {
	t.Helper()
	prepared(t, s, dir)
	old, err := connectedtest.OpenPlan(filepath.Join(dir, "plan"))
	if err != nil {
		t.Fatal(err)
	}
	d := old.Document().Test
	d.Schema = connectedtest.TestSchemaV3
	supplied := map[string][]byte{}
	oldFiles := old.Files()
	supplied[d.Checks.File] = oldFiles["dependencies/"+d.Checks.SHA256]
	for _, step := range d.Steps {
		supplied[step.V2.Input.File] = oldFiles["dependencies/"+step.V2.Input.SHA256]
	}
	for i := range d.Datasets {
		ds := &d.Datasets[i]
		supplied[ds.Projection.File] = oldFiles["dependencies/"+ds.Projection.SHA256]
		def := observeinterval.Definition{Schema: observeinterval.Schema, Source: ds.Source, Namespace: ds.Namespace, Enabled: true, Mode: "snapshots", Freshness: "source-timestamp", HorizonMS: 100, SampleMS: 10, MaxGapMS: 30, MaxSamples: 100, MaxRecords: 10, MaxBytes: 65536}
		raw, _ := json.Marshal(def)
		name := ds.ID + "-interval.json"
		supplied[name] = raw
		ref := connectedtest.Reference{Project: d.Project, ID: ds.ID + "-interval", Schema: observeinterval.Schema, File: name, SHA256: dataset.Digest(raw)}
		ds.Completion.Kind = "full-horizon"
		ds.Completion.HorizonMS = 100
		ds.Completion.Policy = &ref
	}
	raw, _ := json.Marshal(d)
	plan, err := connectedtest.Compile(raw, supplied, old.Document().Generation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "interval-plan")
	if err = plan.Write(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, "execution.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c connectedrun.Config
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "interval-execution.json")
	write(t, config, connectedrun.ConfigV2{Schema: connectedrun.ConfigSchemaV2, Definition: c, Barriers: map[string]connectedrun.SourceSelection{}})
	result, err := connectedrun.Prepare(path, config)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "grant.json"), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: result.Bindings()["stimulus"], IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	return result
}
func TestConnectedIntervalRuntimeSamplesChangingFieldsThroughFullHorizon(t *testing.T) {
	dir := t.TempDir()
	target := startTarget(t, dir)
	target.notifications = make(chan int, 2)
	p := intervalPrepared(t, dir, target)
	clock := &intervalClock{base: time.Now().UTC(), waits: make(chan chan time.Duration)}
	type answer struct {
		result connectedrun.Result
		err    error
	}
	done := make(chan answer, 1)
	output := filepath.Join(dir, "live-run")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		result, err := connectedrun.ExecuteWithClock(ctx, p, "run-one", output, clock)
		done <- answer{result, err}
	}()
	samples := 0
	for {
		select {
		case advance := <-clock.waits:
			samples++
			if samples == 1 {
				for i := 0; i < 2; i++ {
					select {
					case <-target.notifications:
					case <-ctx.Done():
						t.Fatal("stimulus did not reach independent target")
					}
				}
			}
			if samples == 1 {
				waitStimulusFinish(t, ctx, output)
				target.mu.Lock()
				target.rows = nil
				target.persist()
				target.mu.Unlock()
			}
			// The same key changes content while observation is active. A final-state
			// expectation is decided only after the declared full interval.
			if samples == 3 {
				target.mu.Lock()
				target.rows = []string{"SAME,booked,2026-01-01T12:00\n"}
				target.persist()
				target.mu.Unlock()
			}
			if samples == 8 {
				target.mu.Lock()
				target.rows = []string{"SAME,moved,2026-01-02T12:00\n"}
				target.persist()
				target.mu.Unlock()
			}
			advance <- 10 * time.Millisecond
		case got := <-done:
			if got.err != nil || got.result.State != "complete" || got.result.Verdict != assertion.VerdictPass {
				t.Fatal(got.result, got.err)
			}
			if samples < 10 || target.received.Load() != 2 {
				t.Fatalf("premature boundary or resend: samples=%d sends=%d", samples, target.received.Load())
			}
			interval, err := observeinterval.Open(ctx, filepath.Join(output, "intervals", "after"))
			if err != nil || !interval.Sufficient() {
				t.Fatal(interval, err)
			}
			identities := map[string]bool{}
			states := map[string]bool{}
			for _, r := range interval.Records {
				if r.Identity != "" {
					identities[r.Identity] = true
					snapshot, e := dataset.Open(ctx, filepath.Join(output, "intervals", "after", r.Snapshot))
					if e != nil {
						t.Fatal(e)
					}
					rows := snapshot.Document().Rows
					if len(rows) == 0 {
						states["empty"] = true
					}
					for _, row := range rows {
						states[row.Values[1].Text] = true
					}
				}
			}
			if len(identities) < 3 || !states["empty"] || !states["booked"] || !states["moved"] {
				t.Fatal("changing field snapshots were not retained")
			}
			if _, err = connectedrun.Open(ctx, output); err != nil {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal("deterministic runtime did not complete", ctx.Err())
		}
	}
}

// Wait for an actual I/O completion record, not an assumed amount of wall time,
// before advancing the independent business clock. This cannot burn synthetic
// samples while a real transport is still retaining its receipt.
func waitStimulusFinish(t *testing.T, ctx context.Context, output string) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := os.ReadFile(filepath.Join(output, "stimulus-finished.json"))
		var record struct {
			At time.Time `json:"at"`
		}
		if err == nil && json.Unmarshal(raw, &record) == nil && !record.At.IsZero() {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("stimulus did not retain completion", ctx.Err())
		}
	}
}
