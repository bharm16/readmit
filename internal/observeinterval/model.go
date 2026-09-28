// Package observeinterval owns the completion semantics of a live observation
// interval. Source acquisition and transport have separate effect boundaries.
// Historical observewindow/v1 records are never interpreted by this reader.
package observeinterval

import (
	"context"
	"encoding/json/v2"
	"errors"
	"github.com/bharm16/readmit/internal/dataset"
	"regexp"
	"time"
)

const Schema = "readmit-observation-interval/v1"
const ResultSchema = "readmit-observation-interval-result/v1"

var invalid = errors.New("invalid observation interval or retained coverage")
var token = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var hash = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Definition contains no expected values or expected output count. Limits are
// safety ceilings; reaching them cannot satisfy either completion strategy.
type Definition struct {
	Schema     string   `json:"schema"`
	Source     string   `json:"source"`
	Namespace  string   `json:"namespace"`
	Enabled    bool     `json:"enabled"`
	Mode       string   `json:"mode"`      // snapshots or stream
	Freshness  string   `json:"freshness"` // source-timestamp, snapshot-only, ingress
	HorizonMS  int64    `json:"horizon_ms"`
	SampleMS   int64    `json:"sample_ms"`
	MaxGapMS   int64    `json:"max_gap_ms"`
	MaxSamples int      `json:"max_samples"`
	MaxRecords int      `json:"max_records"`
	MaxBytes   int      `json:"max_bytes"`
	Barrier    *Barrier `json:"barrier,omitzero"`
}
type Barrier struct {
	Projection  dataset.Projection `json:"projection"`
	Source      string             `json:"source"`
	Destination string             `json:"destination"`
	Work        string             `json:"work"`
}

// BarrierEvidence is obtained from the separately configured source. Its
// retained dataset must contain exactly one matching scoped completion row.
type BarrierEvidence struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Work        string `json:"work"`
	Run         string `json:"run"`
	Complete    bool   `json:"complete"`
	Evidence    string `json:"evidence"`
}
type Stamp struct {
	At          time.Time `json:"at"`
	MonotonicMS int64     `json:"monotonic_ms"`
}
type Clock interface {
	Now() Stamp
	Wait(context.Context, time.Duration) error
}
type systemClock struct{ start time.Time }

func SystemClock() Clock { return &systemClock{start: time.Now()} }
func (c *systemClock) Now() Stamp {
	n := time.Now()
	return Stamp{At: n.UTC(), MonotonicMS: n.Sub(c.start).Milliseconds()}
}
func (c *systemClock) Wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func Decode(raw []byte) (Definition, error) {
	var d Definition
	if len(raw) > 65536 || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Validate() != nil {
		return d, invalid
	}
	return d, nil
}
func (d Definition) Validate() error {
	if d.Schema != Schema || !hash.MatchString(d.Source) || !token.MatchString(d.Namespace) || d.HorizonMS < 1 || d.HorizonMS > 300000 || d.SampleMS < 1 || d.SampleMS > d.HorizonMS || d.MaxGapMS < d.SampleMS || d.MaxGapMS > 300000 || d.MaxSamples < 2 || d.MaxSamples > 4096 || d.MaxRecords < 1 || d.MaxRecords > 10000 || d.MaxBytes < 1 || d.MaxBytes > 64<<20 {
		return invalid
	}
	if d.Mode != "snapshots" && d.Mode != "stream" || d.Mode == "stream" && d.Freshness != "ingress" || d.Mode == "snapshots" && d.Freshness != "source-timestamp" && d.Freshness != "snapshot-only" {
		return invalid
	}
	if b := d.Barrier; b != nil {
		if !hash.MatchString(b.Source) || b.Source == d.Source || !token.MatchString(b.Destination) || !token.MatchString(b.Work) || b.Projection.Validate() != nil {
			return invalid
		}
	}
	return nil
}

// Observation is the adapter's coverage report, with the exact source/run
// scope, retained snapshot and health independently of its record count.
type Observation struct {
	// Sample replaces Snapshot only in a session armed with ArmSamples.
	Sample          Sample
	BarrierSnapshot *dataset.Snapshot
	CapturePath     string
	Excluded        map[string]string
	Binding         dataset.Binding
	Status          string
	Snapshot        *dataset.Snapshot
	Records         int
	Bytes           int
	Watermark       string
	SourceAt        time.Time
	Barrier         *BarrierEvidence
}
type Record struct {
	// RecordedAt is actual local I/O/journal time, independent of the injected business clock.
	RecordedAt  time.Time         `json:"recorded_at"`
	Binding     *dataset.Binding  `json:"binding,omitzero"`
	BarrierPath string            `json:"barrier_snapshot,omitzero"`
	CapturePath string            `json:"capture,omitzero"`
	Excluded    map[string]string `json:"excluded,omitzero"`
	Kind        string            `json:"kind"`
	Stamp       Stamp             `json:"stamp"`
	Status      string            `json:"status"`
	Snapshot    string            `json:"snapshot,omitzero"`
	Identity    string            `json:"identity,omitzero"`
	Records     int               `json:"records"`
	Bytes       int               `json:"bytes"`
	Watermark   string            `json:"watermark"`
	SourceAt    time.Time         `json:"source_at"`
	Barrier     *BarrierEvidence  `json:"barrier,omitzero"`
}
type Result struct {
	Schema        string          `json:"schema"`
	Binding       dataset.Binding `json:"binding"`
	State         string          `json:"state"`
	Reason        string          `json:"reason"`
	Boundary      string          `json:"boundary"`
	Definition    Definition      `json:"definition"`
	Records       []Record        `json:"records"`
	FinalSnapshot string          `json:"final_snapshot"`
	Identity      string          `json:"-"`
}

func (r Result) Sufficient() bool { return r.State == "complete" }
