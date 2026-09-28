package observeinterval

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/durablelog"
	"path/filepath"
	"sync"
	"time"
)

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "observation interval", AllowedDirectories: []string{"samples"}, Nested: []string{"samples", "capture"}, RequiredFiles: []string{"definition.json", "binding.json", "journal.jsonl", "manifest.json", "identity.sha256"}, AllowFile: func(n string) bool {
	return n == "definition.json" || n == "binding.json" || n == "journal.jsonl" || n == "manifest.json" || n == "identity.sha256"
}, MaxFiles: 50000, MaxFileBytes: 64 << 20, MaxBytes: 256 << 20}, Seal: artifactdir.DirectoryHash(ResultSchema)}

type entry struct {
	durablelog.Envelope
	Record Record `json:"record"`
}
type Session struct {
	lastAcquisition        time.Time
	lastBarrierAcquisition time.Time
	lastBarrierIdentity    string
	mu                     sync.Mutex
	clock                  Clock
	writer                 *artifactdir.Writer
	file                   *artifactdir.Member
	log                    *durablelog.Writer
	result                 Result
	ended                  bool
	failed                 error
	sampleSchema           string
	opener                 SampleOpener
}

func Arm(ctx context.Context, d Definition, b dataset.Binding, path string, clock Clock) (*Session, error) {
	return arm(ctx, d, b, path, clock, "")
}

// ArmSamples arms an interval whose samples are retained under another typed
// contract, named by sampleSchema and verified by opener. It is sealed as
// SamplesSchema so a reader of dataset/v1 intervals never reinterprets it; its
// completion logic is the same.
func ArmSamples(ctx context.Context, d Definition, b dataset.Binding, path string, clock Clock, sampleSchema string, opener SampleOpener) (*Session, error) {
	if !sampleContract.MatchString(sampleSchema) || sampleSchema == dataset.Schema || opener == nil || d.Mode != "snapshots" || d.Freshness != "snapshot-only" {
		return nil, invalid
	}
	s, err := arm(ctx, d, b, path, clock, sampleSchema)
	if s != nil {
		s.opener = opener
	}
	return s, err
}
func arm(ctx context.Context, d Definition, b dataset.Binding, path string, clock Clock, sampleSchema string) (*Session, error) {
	if ctx.Err() != nil || d.Validate() != nil || dataset.ValidateBinding(b) != nil || b.Source != d.Source || b.Namespace != d.Namespace || clock == nil {
		return nil, invalid
	}
	f, schema := family, ResultSchema
	if sampleSchema != "" {
		f, schema = samplesFamily, SamplesSchema
	}
	w, err := artifactdir.Create(path, f, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	fail := true
	defer func() {
		if fail {
			w.Close()
		}
	}()
	def, err := json.Marshal(d, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	binding, _ := json.Marshal(b, json.Deterministic(true))
	if w.WriteFile("definition.json", def) != nil || w.WriteFile("binding.json", binding) != nil || w.Mkdir("samples") != nil {
		return nil, invalid
	}
	if sampleSchema != "" && w.WriteFile("samples.json", sampleDeclaration(sampleSchema)) != nil {
		return nil, invalid
	}
	journal, err := w.Open("journal.jsonl")
	if err != nil {
		return nil, err
	}
	if w.Sync() != nil {
		journal.Close()
		return nil, invalid
	}
	s := &Session{clock: clock, writer: w, file: journal, sampleSchema: sampleSchema, result: Result{Schema: schema, Binding: b, Definition: d, State: "incomplete", Reason: "not-ready", Records: []Record{}}}
	s.log = durablelog.NewWriter(journal, durablelog.Digest(append(def, binding...)), 4<<20, durablelog.Messages{Limit: invalid, Sync: invalid, Encode: invalid})
	fail = false
	if !d.Enabled {
		s.result.Reason = "disabled"
	}
	return s, nil
}
func (s *Session) record(r Record) error {
	if s.ended || s.failed != nil {
		return invalid
	}
	r.Stamp = s.clock.Now()
	r.RecordedAt = time.Now().UTC()
	if err := s.log.Append(&entry{Record: r}); err != nil {
		s.failed = err
		return err
	}
	s.result.Records = append(s.result.Records, r)
	return nil
}
func (s *Session) Append(ctx context.Context, o Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || s.ended || s.failed != nil {
		return invalid
	}
	kind := "sample"
	if len(s.result.Records) == 0 {
		kind = "baseline"
	}
	status := o.Status
	if o.Binding.Run != s.result.Binding.Run || o.Binding.Source != s.result.Binding.Source || o.Binding.Namespace != s.result.Binding.Namespace || o.Binding.Phase != s.result.Binding.Phase && (kind != "baseline" || o.Binding.Phase != "before") {
		status = "wrong-scope"
	}
	binding := o.Binding
	r := Record{Binding: &binding, CapturePath: o.CapturePath, Excluded: o.Excluded, Kind: kind, Status: status, Records: o.Records, Bytes: o.Bytes, Watermark: o.Watermark, SourceAt: o.SourceAt, Barrier: nil}
	if o.Barrier != nil {
		return invalid
	}
	if o.BarrierSnapshot != nil {
		if s.result.Definition.Barrier == nil {
			return invalid
		}
		name := fmt.Sprintf("samples/barrier-%04d", len(s.result.Records)+1)
		if err := o.BarrierSnapshot.Write(ctx, filepath.Join(s.writer.Path(), name)); err != nil {
			s.failed = err
			return err
		}
		barrierDocument := o.BarrierSnapshot.Document()
		reusedFinal := o.CapturePath != "" && o.BarrierSnapshot.Identity() == s.lastBarrierIdentity
		if !s.lastBarrierAcquisition.IsZero() && !barrierDocument.Acquisition.StartedAt.After(s.lastBarrierAcquisition) && !reusedFinal {
			r.Status = "reused-acquisition"
		}
		s.lastBarrierAcquisition = barrierDocument.Acquisition.CompletedAt
		s.lastBarrierIdentity = o.BarrierSnapshot.Identity()
		evidence, err := barrierEvidence(o.BarrierSnapshot, *s.result.Definition.Barrier, s.result.Binding.Run)
		if err != nil {
			r.Status = "barrier-unusable"
		} else {
			r.Barrier = &evidence
			r.BarrierPath = name
			if kind == "baseline" && evidence.Complete {
				r.Status = "barrier-preexisting"
			}
		}
	}
	if o.Sample != nil && (s.sampleSchema == "" || o.Snapshot != nil || o.CapturePath != "") || o.Snapshot != nil && s.sampleSchema != "" {
		return invalid
	}
	if o.Sample != nil {
		if r.Barrier != nil && r.Barrier.Complete && o.BarrierSnapshot != nil && o.Sample.Started().Before(o.BarrierSnapshot.Document().Acquisition.CompletedAt) {
			r.Status = "snapshot-before-barrier"
		}
		if !s.lastAcquisition.IsZero() && !o.Sample.Started().After(s.lastAcquisition) {
			r.Status = "reused-acquisition"
		}
		s.lastAcquisition = o.Sample.Completed()
		if o.Sample.Binding() != o.Binding || !o.Sample.Usable() {
			r.Status = "unusable-snapshot"
		}
		name := fmt.Sprintf("samples/%04d", len(s.result.Records)+1)
		if err := o.Sample.Write(ctx, filepath.Join(s.writer.Path(), name)); err != nil {
			s.failed = err
			return err
		}
		r.Snapshot = name
		r.Identity = o.Sample.Identity()
		r.Records = o.Sample.Records()
		r.Bytes = o.Sample.Size()
	}
	if o.Snapshot != nil {
		doc := o.Snapshot.Document()
		if r.Barrier != nil && r.Barrier.Complete && o.BarrierSnapshot != nil && doc.Acquisition.StartedAt.Before(o.BarrierSnapshot.Document().Acquisition.CompletedAt) {
			r.Status = "snapshot-before-barrier"
		}
		if s.result.Definition.Mode == "snapshots" {
			if !s.lastAcquisition.IsZero() && !doc.Acquisition.StartedAt.After(s.lastAcquisition) {
				r.Status = "reused-acquisition"
			}
			s.lastAcquisition = doc.Acquisition.CompletedAt
		}
		if s.result.Definition.Mode == "snapshots" && doc.Acquisition.Kind == "capture" || s.result.Definition.Mode == "stream" && doc.Acquisition.Kind != "capture" {
			r.Status = "source-kind-mismatch"
		}
		if s.result.Definition.Freshness == "source-timestamp" && (doc.Acquisition.Facts == nil || !o.SourceAt.Equal(doc.Acquisition.Facts.AsOf)) {
			r.Status = "freshness-unknown"
		}
		if doc.Binding != o.Binding || !o.Snapshot.Usable() {
			r.Status = "unusable-snapshot"
		}
		name := fmt.Sprintf("samples/%04d", len(s.result.Records)+1)
		if err := o.Snapshot.Write(ctx, filepath.Join(s.writer.Path(), name)); err != nil {
			s.failed = err
			return err
		}
		r.Snapshot = name
		r.Identity = o.Snapshot.Identity()
		r.Records = len(doc.Rows)
		r.Bytes = doc.Material.Size
	}
	return s.record(r)
}
func (s *Session) StimulusStarted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.result.Definition.Enabled || decide(s.result).Reason != "not-ready" || len(s.result.Records) != 1 || s.result.Records[0].Kind != "baseline" || s.result.Records[0].Status != "healthy" {
		return invalid
	}
	return s.record(Record{Kind: "stimulus-started", Status: "healthy"})
}
func (s *Session) StimulusFinished() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := false
	for _, r := range s.result.Records {
		if r.Kind == "stimulus-finished" {
			return invalid
		}
		seen = seen || r.Kind == "stimulus-started"
	}
	if !seen {
		return invalid
	}
	return s.record(Record{Kind: "stimulus-finished", Status: "healthy"})
}
func (s *Session) ReadyToFinish() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return decide(s.result).Sufficient()
}
func (s *Session) Path() string { return s.writer.Path() }
func (s *Session) Finish(ctx context.Context) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended || s.failed != nil {
		return Result{}, invalid
	}
	s.ended = true
	defer s.writer.Close()
	defer s.file.Close()
	r := finalDecision(s.result)
	if ctx.Err() != nil && (r.Sufficient() || r.Reason == "not-ready" || r.Reason == "horizon-incomplete" || r.Reason == "barrier-not-observed" || r.Reason == "capture-not-finalized") {
		r.State = "incomplete"
		r.Reason = "runner-deadline-or-cancellation"
		r.Boundary = "insufficient"
	}
	raw, err := json.Marshal(r, json.Deterministic(true))
	if err != nil || s.file.Sync() != nil || s.writer.WriteFile("manifest.json", raw) != nil {
		return Result{}, invalid
	}
	if _, err = s.writer.Seal(nil); err != nil {
		return Result{}, err
	}
	if s.opener != nil {
		return OpenSamples(context.WithoutCancel(ctx), s.writer.Path(), s.opener)
	}
	return Open(context.WithoutCancel(ctx), s.writer.Path())
}

// Close retains the unfinished journal. It never sends, resumes or deletes.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ended {
		s.ended = true
		s.file.Close()
		s.writer.Close()
	}
}
func decide(r Result) Result {
	r.State = "incomplete"
	r.Reason = "not-ready"
	r.Boundary = "insufficient"
	r.FinalSnapshot = ""
	if !r.Definition.Enabled {
		r.Reason = "disabled"
		return r
	}
	var started, finished *Record
	var previous *Record
	samples := 0
	totalBytes := 0
	for i := range r.Records {
		current := &r.Records[i]
		if current.Stamp.At.IsZero() || current.Stamp.MonotonicMS < 0 {
			r.Reason = "clock-invalid"
			return r
		}
		if previous != nil {
			elapsed := current.Stamp.MonotonicMS - previous.Stamp.MonotonicMS
			wall := current.Stamp.At.Sub(previous.Stamp.At).Milliseconds()
			if elapsed < 0 || wall < 0 || wall-elapsed > 1000 || elapsed-wall > 1000 {
				r.Reason = "clock-shift"
				return r
			}
		}
		previous = current
		if current.Kind == "baseline" || current.Kind == "sample" {
			b := current.Binding
			if b == nil || b.Run != r.Binding.Run || b.Source != r.Binding.Source || b.Namespace != r.Binding.Namespace || b.Phase != r.Binding.Phase && (current.Kind != "baseline" || b.Phase != "before") {
				r.Reason = "wrong-scope"
				return r
			}
		} else if current.Binding != nil {
			r.Reason = "invalid-lifecycle"
			return r
		}
		if current.Kind == "baseline" && current.Barrier != nil && current.Barrier.Complete {
			r.Reason = "barrier-preexisting"
			return r
		}
		if current.Status != "healthy" {
			r.Reason = current.Status
			return r
		}
		switch current.Kind {
		case "baseline":
			if i != 0 {
				r.Reason = "invalid-lifecycle"
				return r
			}
		case "stimulus-started":
			if started != nil || i < 1 || r.Records[0].Kind != "baseline" {
				r.Reason = "invalid-lifecycle"
				return r
			}
			started = current
		case "stimulus-finished":
			if started == nil || finished != nil {
				r.Reason = "invalid-lifecycle"
				return r
			}
			finished = current
		case "sample":
			if started == nil {
				r.Reason = "invalid-lifecycle"
				return r
			}
		default:
			r.Reason = "invalid-lifecycle"
			return r
		}
		if current.Kind == "baseline" || current.Kind == "sample" {
			samples++
			totalBytes += current.Bytes
			if current.Records < 0 || current.Bytes < 0 || current.Records >= r.Definition.MaxRecords || current.Bytes >= r.Definition.MaxBytes || samples > r.Definition.MaxSamples || totalBytes > 128<<20 {
				r.Reason = "safety-limit"
				return r
			}
			if r.Definition.Mode == "snapshots" && current.Snapshot == "" {
				r.Reason = "missing-snapshot"
				return r
			}
			if r.Definition.Freshness == "source-timestamp" && current.SourceAt.IsZero() {
				r.Reason = "freshness-unknown"
				return r
			}
			if current.Snapshot != "" {
				r.FinalSnapshot = current.Snapshot
			}
		}
	}
	if started == nil || finished == nil {
		return r
	}
	lastSample := r.Records[0]
	for _, current := range r.Records {
		if current.Kind != "sample" {
			continue
		}
		if current.Stamp.MonotonicMS-lastSample.Stamp.MonotonicMS > r.Definition.MaxGapMS {
			r.Reason = "lost-coverage"
			return r
		}
		lastSample = current
	}
	if lastSample.Kind != "sample" {
		r.Reason = "no-coverage"
		return r
	}
	if r.Definition.Barrier != nil {
		b := lastSample.Barrier
		want := r.Definition.Barrier
		if b == nil || !b.Complete || b.Source != want.Source || b.Destination != want.Destination || b.Work != want.Work || b.Run != r.Binding.Run || !hash.MatchString(b.Evidence) || lastSample.Stamp.MonotonicMS < finished.Stamp.MonotonicMS {
			r.Reason = "barrier-not-observed"
			if lastSample.Stamp.MonotonicMS-finished.Stamp.MonotonicMS >= r.Definition.HorizonMS {
				r.Reason = "barrier-timeout"
			}
			return r
		}
		r.Boundary = "independently-observed-processing-barrier"
	} else {
		if lastSample.Stamp.MonotonicMS-finished.Stamp.MonotonicMS < r.Definition.HorizonMS {
			r.Reason = "horizon-incomplete"
			return r
		}
		r.Boundary = "full-bounded-horizon"
	}
	r.State = "complete"
	r.Reason = "complete"
	return r
}

func finalDecision(r Result) Result {
	r = decide(r)
	if r.Sufficient() && r.Definition.Mode == "stream" {
		if len(r.Records) == 0 || r.Records[len(r.Records)-1].CapturePath == "" || r.FinalSnapshot == "" {
			r.State = "incomplete"
			r.Reason = "capture-not-finalized"
			r.Boundary = "insufficient"
		}
	}
	return r
}

func barrierEvidence(snapshot *dataset.Snapshot, want Barrier, run string) (BarrierEvidence, error) {
	if snapshot == nil || !snapshot.Usable() {
		return BarrierEvidence{}, invalid
	}
	doc := snapshot.Document()
	if doc.Binding.Run != run || doc.Binding.Source != want.Source || doc.Projection.Identity() != want.Projection.Identity() {
		return BarrierEvidence{}, invalid
	}
	result := BarrierEvidence{Source: want.Source, Destination: want.Destination, Work: want.Work, Run: run, Evidence: snapshot.Identity()}
	matches := 0
	for _, row := range doc.Rows {
		values := map[string]string{}
		for i, column := range doc.Projection.Columns {
			v := row.Values[i]
			if v.State == "present" && v.Type == "text" {
				values[column.Name] = v.Text
			}
		}
		if values["run"] == run && values["work"] == want.Work && values["destination"] == want.Destination {
			matches++
			result.Complete = values["state"] == "complete"
		}
	}
	if matches > 1 {
		return result, invalid
	}
	return result, nil
}
