package observeinterval_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/durablelog"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observeinterval"
)

func TestIntervalDefinitionRefusesUnknownMembersAndInvalidBounds(t *testing.T) {
	d, _, _, _ := snapshotFixture(t)
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = observeinterval.Decode(raw); err != nil {
		t.Fatal(err)
	}
	unknown := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"expected_count":2}`)...)
	if _, err = observeinterval.Decode(unknown); err == nil {
		t.Fatal("unknown completion instruction accepted")
	}
	for name, change := range map[string]func(*observeinterval.Definition){
		"zero-horizon":                   func(d *observeinterval.Definition) { d.HorizonMS = 0 },
		"unbounded-horizon":              func(d *observeinterval.Definition) { d.HorizonMS = 300001 },
		"sample-after-horizon":           func(d *observeinterval.Definition) { d.SampleMS = d.HorizonMS + 1 },
		"gap-before-sample":              func(d *observeinterval.Definition) { d.MaxGapMS = d.SampleMS - 1 },
		"one-sample":                     func(d *observeinterval.Definition) { d.MaxSamples = 1 },
		"unbounded-samples":              func(d *observeinterval.Definition) { d.MaxSamples = 4097 },
		"zero-records":                   func(d *observeinterval.Definition) { d.MaxRecords = 0 },
		"unbounded-bytes":                func(d *observeinterval.Definition) { d.MaxBytes = (64 << 20) + 1 },
		"stream-with-snapshot-freshness": func(d *observeinterval.Definition) { d.Mode = "stream" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := d
			change(&changed)
			raw, _ := json.Marshal(changed)
			if _, err := observeinterval.Decode(raw); err == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
}

func TestIntervalCannotConvertRepeatedSealedCaptureIntoLiveCoverage(t *testing.T) {
	d, b, _, raw := snapshotFixture(t)
	p := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "events", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "PID-3.1", Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 20, MaxBytes: 65536, TimeoutMS: 1000}}
	c := &clock{}
	stamp := c.Now().At
	material, _ := json.Marshal(dataset.CaptureRead{Schema: dataset.CaptureSchema, Identity: dataset.Digest([]byte("sealed-capture"))})
	old, err := dataset.Build(context.Background(), b, p, dataset.Acquisition{Kind: "capture", Status: "complete", StartedAt: stamp.Add(-time.Hour), CompletedAt: stamp.Add(-time.Hour), SourceConfiguration: raw, Completion: "snapshot"}, material)
	if err != nil {
		t.Fatal(err)
	}
	sealed := filepath.Join(t.TempDir(), "old-capture")
	if err = old.Write(context.Background(), sealed); err != nil {
		t.Fatal(err)
	}
	old, err = dataset.Open(context.Background(), sealed)
	if err != nil || !old.Usable() {
		t.Fatal("positive control: sealed capture must be valid historical evidence", err)
	}
	s, err := observeinterval.Arm(context.Background(), d, b, filepath.Join(t.TempDir(), "interval"), c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: old}); err != nil {
		return
	}
	if s.StimulusStarted() != nil {
		return
	}
	if err = s.StimulusFinished(); err != nil {
		t.Fatal(err)
	}
	c.elapsed = 100
	if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: old}); err != nil {
		return
	}
	got, err := s.Finish(context.Background())
	if err == nil && got.Sufficient() {
		t.Fatal("repeated sealed capture falsely established a new live observation interval")
	}
}

func TestIntervalClosedSessionCannotResumeOrRewriteRecovery(t *testing.T) {
	d, b, p, raw := snapshotFixture(t)
	c := &clock{}
	path := filepath.Join(t.TempDir(), "interval")
	s, err := observeinterval.Arm(context.Background(), d, b, path, c)
	if err != nil {
		t.Fatal(err)
	}
	o := observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: snapshot(t, c, b, raw, "key,status\n", p)}
	if err = s.Append(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	s.Close()
	before := adversarialFiles(t, path)
	if s.Append(context.Background(), o) == nil || s.StimulusStarted() == nil || s.StimulusFinished() == nil {
		t.Fatal("closed session resumed")
	}
	if _, err = s.Finish(context.Background()); err == nil {
		t.Fatal("closed session finalized")
	}
	for i := 0; i < 2; i++ {
		got, err := observeinterval.Recover(context.Background(), path)
		if err != nil || got.Sufficient() || got.Reason != "interrupted" {
			t.Fatal(got, err)
		}
	}
	if _, err = observeinterval.Arm(context.Background(), d, b, path, c); err == nil {
		t.Fatal("rearmed existing evidence")
	}
	after := adversarialFiles(t, path)
	a, _ := json.Marshal(before, json.Deterministic(true))
	z, _ := json.Marshal(after, json.Deterministic(true))
	if !bytes.Equal(a, z) {
		t.Fatal("recovery changed evidence")
	}
}

// These mutations recompute the journal chain and outer identity, so rejection
// must come from the retained lifecycle or source meaning, not a stale seal.
func TestIntervalResealedSemanticTamperingCannotCreateCompletion(t *testing.T) {
	for _, variant := range []string{"unchanged", "manifest-boundary", "premature-horizon", "readiness-reordered", "forged-barrier", "unusable-source", "wrong-source-scope", "reused-acquisition", "preexisting-barrier", "sealed-capture-as-snapshot"} {
		t.Run(variant, func(t *testing.T) {
			d, b, p, source := snapshotFixture(t)
			c := &clock{}
			path := filepath.Join(t.TempDir(), "interval")
			var initialBarrier, completedBarrier, preexistingBarrier *dataset.Snapshot
			if variant == "preexisting-barrier" {
				rawBarrier := []byte(`{"schema":"owned-barrier/v1"}`)
				projection := p
				projection.ID = "barrier"
				projection.Envelope = &dataset.Envelope{Encoding: importer.UTF8, CSV: &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 4}}
				projection.Columns = nil
				for _, name := range []string{"run", "work", "destination", "state"} {
					projection.Columns = append(projection.Columns, dataset.Column{Name: name, Type: "text", Locator: importer.Locator{name}, Key: name == "run", Required: true})
				}
				d.Barrier = &observeinterval.Barrier{Source: dataset.Digest(rawBarrier), Destination: "application", Work: "change", Projection: projection}
				binding := dataset.Binding{Run: b.Run, Phase: "after", Source: d.Barrier.Source, Namespace: "barrier"}
				initialBarrier = snapshot(t, c, binding, rawBarrier, "run,work,destination,state\n", projection)
				preexistingBarrier = snapshot(t, c, binding, rawBarrier, "run,work,destination,state\nrun-one,change,application,complete\n", projection)
				c.elapsed = 100
				completedBarrier = snapshot(t, c, binding, rawBarrier, "run,work,destination,state\nrun-one,change,application,complete\n", projection)
				c.elapsed = 0
			}
			s, err := observeinterval.Arm(context.Background(), d, b, path, c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: snapshot(t, c, b, source, "key,status\n", p), BarrierSnapshot: initialBarrier}); err != nil {
				t.Fatal(err)
			}
			if s.StimulusStarted() != nil || s.StimulusFinished() != nil {
				t.Fatal("not armed")
			}
			c.elapsed = 100
			final := snapshot(t, c, b, source, "key,status\na,active\n", p)
			if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: final, BarrierSnapshot: completedBarrier}); err != nil {
				t.Fatal(err)
			}
			result, err := s.Finish(context.Background())
			if err != nil || !result.Sufficient() {
				t.Fatal(result, err)
			}
			files := adversarialFiles(t, path)
			records := result.Records
			switch variant {
			case "manifest-boundary":
				result.Boundary = "independently-observed-processing-barrier"
			case "premature-horizon":
				d.HorizonMS = 200
				result.Definition = d
				files["definition.json"], _ = json.Marshal(d, json.Deterministic(true))
			case "readiness-reordered":
				records[0], records[1] = records[1], records[0]
			case "forged-barrier":
				records[len(records)-1].Barrier = &observeinterval.BarrierEvidence{Source: d.Source, Destination: "application", Work: "work", Run: b.Run, Complete: true, Evidence: final.Identity()}
			case "preexisting-barrier":
				replacementPath := filepath.Join(t.TempDir(), "barrier")
				if err = preexistingBarrier.Write(context.Background(), replacementPath); err != nil {
					t.Fatal(err)
				}
				for name, raw := range adversarialFiles(t, replacementPath) {
					files[records[0].BarrierPath+"/"+name] = raw
				}
				records[0].Barrier.Complete = true
				records[0].Barrier.Evidence = preexistingBarrier.Identity()
			case "sealed-capture-as-snapshot":
				projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "events", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "PID-3.1", Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 20, MaxBytes: 65536, TimeoutMS: 1000}}
				material, _ := json.Marshal(dataset.CaptureRead{Schema: dataset.CaptureSchema, Identity: dataset.Digest([]byte("old-capture"))})
				acquisition := final.Document().Acquisition
				acquisition.Kind = "capture"
				replacement, err := dataset.Build(context.Background(), b, projection, acquisition, material)
				if err != nil || !replacement.Usable() {
					t.Fatal(err)
				}
				replacementPath := filepath.Join(t.TempDir(), "capture-snapshot")
				if err = replacement.Write(context.Background(), replacementPath); err != nil {
					t.Fatal(err)
				}
				last := &records[len(records)-1]
				for name, raw := range adversarialFiles(t, replacementPath) {
					files[last.Snapshot+"/"+name] = raw
				}
				last.Identity = replacement.Identity()
				last.Records = 0
				last.Bytes = len(material)
			case "unusable-source", "wrong-source-scope", "reused-acquisition":
				doc := final.Document()
				a := doc.Acquisition
				binding := b
				if variant == "unusable-source" {
					a.Status = "failed"
				} else if variant == "wrong-source-scope" {
					binding.Namespace = "other"
				}
				if variant == "reused-acquisition" {
					a.StartedAt = c.Now().At.Add(-100 * time.Millisecond)
					a.CompletedAt = a.StartedAt
				}
				replacement, err := dataset.Build(context.Background(), binding, p, a, []byte("key,status\na,active\n"))
				if err != nil {
					t.Fatal(err)
				}
				replacementPath := filepath.Join(t.TempDir(), "replacement")
				if err = replacement.Write(context.Background(), replacementPath); err != nil {
					t.Fatal(err)
				}
				last := &records[len(records)-1]
				for name, raw := range adversarialFiles(t, replacementPath) {
					files[last.Snapshot+"/"+name] = raw
				}
				last.Identity = replacement.Identity()
			}
			result.Records = records
			files["manifest.json"], _ = json.Marshal(result, json.Deterministic(true))
			var journal bytes.Buffer
			previous := durablelog.Digest(append(bytes.Clone(files["definition.json"]), files["binding.json"]...))
			for i, record := range records {
				entry := struct {
					durablelog.Envelope
					Record observeinterval.Record `json:"record"`
				}{durablelog.Envelope{Sequence: i + 1, Previous: previous, At: c.Now().At}, record}
				raw, err := json.Marshal(entry, json.Deterministic(true))
				if err != nil {
					t.Fatal(err)
				}
				journal.Write(raw)
				journal.WriteByte('\n')
				previous = durablelog.Digest(raw)
			}
			files["journal.jsonl"] = journal.Bytes()
			files["identity.sha256"] = []byte(artifactdir.Identity(observeinterval.ResultSchema, files) + "\n")
			for name, raw := range files {
				if err = os.WriteFile(filepath.Join(path, filepath.FromSlash(name)), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := observeinterval.Open(context.Background(), path)
			if variant == "unchanged" {
				if err != nil || !got.Sufficient() {
					t.Fatal("positive control failed after valid reseal", got, err)
				}
			} else if err == nil {
				t.Fatal("semantically forged completion accepted", got)
			}
		})
	}
}

func adversarialFiles(t *testing.T, path string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	if err := filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		name, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(name)], err = os.ReadFile(p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestIntervalFreshAcquisitionsMayRetainUnchangedBusinessTimestamps(t *testing.T) {
	for _, replay := range []bool{false, true} {
		name := "fresh-acquisitions"
		if replay {
			name = "replayed-acquisition"
		}
		t.Run(name, func(t *testing.T) {
			d, b, p, source := snapshotFixture(t)
			p.Envelope.CSV.Fields = 3
			p.Columns = append(p.Columns, dataset.Column{Name: "updated", Type: "datetime", Locator: importer.Locator{"updated"}, Required: true})
			c := &clock{}
			s, err := observeinterval.Arm(context.Background(), d, b, filepath.Join(t.TempDir(), "interval"), c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			businessTime := c.Now().At.Add(-24 * time.Hour)
			acquire := func() *dataset.Snapshot {
				stamp := c.Now().At
				got, err := dataset.Build(context.Background(), b, p, dataset.Acquisition{Kind: "file", Status: "complete", StartedAt: stamp, CompletedAt: stamp, SourceConfiguration: source, Completion: "snapshot", Facts: &dataset.AcquisitionFacts{AsOf: stamp, Status: "observed"}}, []byte("key,status,updated\na,active,"+businessTime.Format(time.RFC3339)+"\n"))
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			initial := acquire()
			if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: initial}); err != nil {
				t.Fatal(err)
			}
			if s.StimulusStarted() != nil || s.StimulusFinished() != nil {
				t.Fatal("not armed")
			}
			c.elapsed = 100
			final := initial
			if !replay {
				final = acquire()
			}
			if err = s.Append(context.Background(), observeinterval.Observation{Binding: b, Status: "healthy", Snapshot: final}); err != nil {
				if replay {
					return
				}
				t.Fatal(err)
			}
			result, err := s.Finish(context.Background())
			if replay {
				if err == nil && result.Sufficient() {
					t.Fatal("reused acquisition established fresh interval coverage")
				}
				return
			}
			if err != nil || !result.Sufficient() {
				t.Fatal("fresh acquisition with unchanged older business time was rejected", result, err)
			}
			reopened, err := observeinterval.Open(context.Background(), s.Path())
			if err != nil || !reopened.Sufficient() {
				t.Fatal(reopened, err)
			}
		})
	}
}
