package observeinterval_test

import (
	"context"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/observeinterval"
	"strings"
	"testing"
	"time"
)

type clock struct{ elapsed int64 }

func (c *clock) Now() observeinterval.Stamp {
	return observeinterval.Stamp{At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(c.elapsed) * time.Millisecond), MonotonicMS: c.elapsed}
}
func (c *clock) Wait(context.Context, time.Duration) error { return nil }
func TestFullHorizonNeverStopsAtExpectedCountOrStableSamples(t *testing.T) {
	c := &clock{}
	d := observeinterval.Definition{Schema: observeinterval.Schema, Source: strings.Repeat("a", 64), Namespace: "appointments", Enabled: true, Mode: "stream", Freshness: "ingress", HorizonMS: 100, SampleMS: 10, MaxGapMS: 100, MaxSamples: 20, MaxRecords: 10, MaxBytes: 65536}
	binding := dataset.Binding{Run: "run-one", Phase: "after", Source: d.Source, Namespace: d.Namespace}
	s, err := observeinterval.Arm(context.Background(), d, binding, t.TempDir()+"/interval", c)
	if err != nil {
		t.Fatal(err)
	}
	observe := func(n int) {
		t.Helper()
		if err := s.Append(context.Background(), observeinterval.Observation{Binding: binding, Status: "healthy", Records: n, Bytes: n * 10}); err != nil {
			t.Fatal(err)
		}
	}
	observe(0)
	if err := s.StimulusStarted(); err != nil {
		t.Fatal(err)
	}
	if err := s.StimulusFinished(); err != nil {
		t.Fatal(err)
	}
	c.elapsed = 10
	observe(2)
	if s.ReadyToFinish() {
		t.Fatal("expected count ended observation")
	}
	c.elapsed = 50
	observe(2)
	if s.ReadyToFinish() {
		t.Fatal("stable sample ended observation")
	}
	c.elapsed = 99
	observe(3)
	if s.ReadyToFinish() {
		t.Fatal("horizon ended early")
	}
	c.elapsed = 100
	observe(3)
	if !s.ReadyToFinish() {
		t.Fatal("healthy full interval did not complete")
	}
	r, err := s.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Sufficient() || r.Reason != "capture-not-finalized" || r.Records[len(r.Records)-1].Records != 3 {
		t.Fatal(r)
	}
}
