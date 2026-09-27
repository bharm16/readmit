package observeinterval_test

import (
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observeinterval"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func snapshot(t *testing.T, c *clock, binding dataset.Binding, rawSource []byte, body string, p dataset.Projection) *dataset.Snapshot {
	t.Helper()
	stamp := c.Now()
	s, err := dataset.Build(context.Background(), binding, p, dataset.Acquisition{Kind: "file", Status: "complete", StartedAt: stamp.At, CompletedAt: stamp.At, SourceConfiguration: rawSource, Completion: "snapshot", Facts: &dataset.AcquisitionFacts{AsOf: stamp.At.Add(-time.Hour), Status: "observed"}}, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func snapshotFixture(t *testing.T) (observeinterval.Definition, dataset.Binding, dataset.Projection, []byte) {
	t.Helper()
	source := []byte(`{"schema":"owned-independent-export/v1"}`)
	binding := dataset.Binding{Run: "run-one", Phase: "after", Source: dataset.Digest(source), Namespace: "appointments"}
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "state", Format: "csv", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 2}}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"key"}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}, Limits: dataset.Limits{MaxRows: 20, MaxBytes: 65536, TimeoutMS: 1000}}
	d := observeinterval.Definition{Schema: observeinterval.Schema, Source: binding.Source, Namespace: binding.Namespace, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 100, SampleMS: 10, MaxGapMS: 100, MaxSamples: 30, MaxRecords: 10, MaxBytes: 65536}
	return d, binding, projection, source
}
func TestHealthyEmptyIntervalAndUnhealthyEmptyHaveDifferentOutcomes(t *testing.T) {
	for _, status := range []string{"healthy", "collector-failed", "stale", "truncated", "wrong-scope", "lost-coverage"} {
		t.Run(status, func(t *testing.T) {
			d, b, p, raw := snapshotFixture(t)
			c := &clock{}
			s, err := observeinterval.Arm(context.Background(), d, b, filepath.Join(t.TempDir(), "interval"), c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			initial := snapshot(t, c, b, raw, "key,status\n", p)
			if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: initial}); err != nil {
				t.Fatal(err)
			}
			if s.StimulusStarted() != nil || s.StimulusFinished() != nil {
				t.Fatal("not armed")
			}
			c.elapsed = 100
			empty := snapshot(t, c, b, raw, "key,status\n", p)
			if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: status, Snapshot: empty}); err != nil {
				t.Fatal(err)
			}
			got, err := s.Finish(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Sufficient() != (status == "healthy") {
				t.Fatal(status, got)
			}
			// Old timestamps carried by unchanged business state do not by themselves
			// turn a fresh, healthy source snapshot into a stale observation.
			if status == "healthy" && got.Boundary != "full-bounded-horizon" {
				t.Fatal(got)
			}
		})
	}
}
func TestArmFailuresAndSafetyCeilingsCannotProveAbsence(t *testing.T) {
	for _, name := range []string{"disabled", "failed-arm", "stale-baseline", "wrong-run", "clock-backward", "lost-sampling-coverage", "record-limit", "byte-limit", "runner-deadline"} {
		t.Run(name, func(t *testing.T) {
			d, b, p, raw := snapshotFixture(t)
			c := &clock{}
			if name == "disabled" {
				d.Enabled = false
			}
			if name == "byte-limit" {
				d.MaxBytes = 100
			}
			s, err := observeinterval.Arm(context.Background(), d, b, filepath.Join(t.TempDir(), "interval"), c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			baseline := observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: snapshot(t, c, b, raw, "key,status\n", p)}
			if name == "failed-arm" {
				baseline.Status = "collector-failed"
			}
			if name == "stale-baseline" {
				baseline.Status = "stale"
			}
			if err = s.Append(context.Background(), baseline); err != nil {
				t.Fatal(err)
			}
			if name == "disabled" || name == "failed-arm" || name == "stale-baseline" {
				if s.StimulusStarted() == nil {
					t.Fatal("failed arm allowed stimulus")
				}
				got, err := s.Finish(context.Background())
				if err != nil || got.Sufficient() {
					t.Fatal(got, err)
				}
				return
			}
			if s.StimulusStarted() != nil || s.StimulusFinished() != nil {
				t.Fatal("not armed")
			}
			c.elapsed = 100
			body := "key,status\n"
			if name == "byte-limit" {
				body += "same," + strings.Repeat("x", 100) + "\n"
			}
			if name == "record-limit" {
				for i := 0; i < 10; i++ {
					body += "same,active\n"
				}
			}
			observed := observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: snapshot(t, c, b, raw, body, p)}
			if name == "wrong-run" {
				observed.Binding.Run = "another"
			}
			if name == "clock-backward" {
				c.elapsed = -1
			}
			if name == "lost-sampling-coverage" {
				c.elapsed = 101
			}

			if err = s.Append(context.Background(), observed); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if name == "runner-deadline" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			got, err := s.Finish(ctx)
			if err != nil || got.Sufficient() {
				t.Fatal(got, err)
			}
		})
	}
}
func TestProcessingBarrierRequiresIndependentScopedRetainedEvidence(t *testing.T) {
	for _, variant := range []string{"correct", "wrong-run", "wrong-work", "wrong-destination", "pre-existing", "duplicate", "pending"} {
		t.Run(variant, func(t *testing.T) {
			d, b, p, raw := snapshotFixture(t)
			source := []byte(`{"schema":"owned-independent-processing-barrier/v1"}`)
			projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "barrier", Format: "csv", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 4}}, Columns: []dataset.Column{{Name: "run", Type: "text", Locator: importer.Locator{"run"}, Key: true, Required: true}, {Name: "work", Type: "text", Locator: importer.Locator{"work"}, Required: true}, {Name: "destination", Type: "text", Locator: importer.Locator{"destination"}, Required: true}, {Name: "state", Type: "text", Locator: importer.Locator{"state"}, Required: true}}, Limits: dataset.Limits{MaxRows: 20, MaxBytes: 65536, TimeoutMS: 1000}}
			d.Barrier = &observeinterval.Barrier{Source: dataset.Digest(source), Destination: "application", Work: "change-one", Projection: projection}
			barrierBinding := dataset.Binding{Run: b.Run, Phase: "after", Source: d.Barrier.Source, Namespace: "barrier"}
			c := &clock{}
			s, err := observeinterval.Arm(context.Background(), d, b, filepath.Join(t.TempDir(), "interval"), c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			header := "run,work,destination,state\n"
			baselineBody := header
			if variant == "pre-existing" {
				baselineBody += "run-one,change-one,application,complete\n"
			}
			initial := observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: snapshot(t, c, b, raw, "key,status\n", p), BarrierSnapshot: snapshot(t, c, barrierBinding, source, baselineBody, projection)}
			if err = s.Append(context.Background(), initial); err != nil {
				t.Fatal(err)
			}
			if variant == "pre-existing" {
				if s.StimulusStarted() == nil {
					t.Fatal("old completed barrier allowed stimulus")
				}
				return
			}
			if s.StimulusStarted() != nil || s.StimulusFinished() != nil {
				t.Fatal("not armed")
			}
			c.elapsed = 10
			row := "run-one,change-one,application,complete\n"
			switch variant {
			case "wrong-run":
				row = "another,change-one,application,complete\n"
			case "wrong-work":
				row = "run-one,other,application,complete\n"
			case "wrong-destination":
				row = "run-one,change-one,other,complete\n"
			case "duplicate":
				row += row
			case "pending":
				row = "run-one,change-one,application,pending\n"
			}
			observed := observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: snapshot(t, c, b, raw, "key,status\n", p), BarrierSnapshot: snapshot(t, c, barrierBinding, source, header+row, projection)}
			if err = s.Append(context.Background(), observed); err != nil {
				t.Fatal(err)
			}
			got, err := s.Finish(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Sufficient() != (variant == "correct") {
				encoded, _ := json.Marshal(got)
				t.Fatalf("%s %s", variant, encoded)
			}
			if variant == "correct" && got.Boundary != "independently-observed-processing-barrier" {
				t.Fatal(got)
			}
		})
	}
}
