package observeinterval

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/durablelog"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/networkaction"
	"path/filepath"
	"strings"
	"time"
)

// Recover is strictly read-only. A torn tail or missing completion marker is
// interrupted evidence, never permission to resume a collector or a send.
func Recover(ctx context.Context, path string) (Result, error) { return read(ctx, path, false) }
func Open(ctx context.Context, path string) (Result, error)    { return read(ctx, path, true) }
func read(ctx context.Context, path string, sealed bool) (Result, error) {
	layout := family.Layout
	if !sealed {
		layout.RequiredFiles = []string{"definition.json", "binding.json", "journal.jsonl"}
	}
	files, err := artifactdir.Read(path, layout)
	if err != nil {
		return Result{}, err
	}
	d, err := Decode(files["definition.json"])
	if err != nil {
		return Result{}, err
	}
	var binding dataset.Binding
	if json.Unmarshal(files["binding.json"], &binding, json.RejectUnknownMembers(true)) != nil || dataset.ValidateBinding(binding) != nil || binding.Source != d.Source || binding.Namespace != d.Namespace {
		return Result{}, invalid
	}
	r := Result{Schema: ResultSchema, Definition: d, Binding: binding, Records: []Record{}}
	var lastAcquisition, lastBarrierAcquisition time.Time
	lastBarrierIdentity := ""
	torn, err := durablelog.Scan(files["journal.jsonl"], durablelog.Digest(append(bytes.Clone(files["definition.json"]), files["binding.json"]...)), invalid, func(raw []byte) durablelog.Record {
		var e entry
		if json.Unmarshal(raw, &e, json.RejectUnknownMembers(true)) != nil {
			return nil
		}
		return &e
	}, func(_ int, raw durablelog.Record) error {
		record := raw.(*entry).Record
		if record.RecordedAt.IsZero() {
			return invalid
		}
		if record.Snapshot != "" {
			if !strings.HasPrefix(record.Snapshot, "samples/") || strings.Contains(record.Snapshot, "..") || strings.Contains(record.Snapshot, "\\") {
				return invalid
			}
			snapshot, err := dataset.Open(ctx, filepath.Join(path, record.Snapshot))
			if err != nil || snapshot.Identity() != record.Identity || !artifactdir.MatchesSubtree(files, record.Snapshot, dataset.Schema, snapshot.Identity()) {
				return invalid
			}
			doc := snapshot.Document()
			if d.Mode == "snapshots" {
				if record.Status == "healthy" && !lastAcquisition.IsZero() && !doc.Acquisition.StartedAt.After(lastAcquisition) {
					return invalid
				}
				lastAcquisition = doc.Acquisition.CompletedAt
			}
			if record.Status == "healthy" && (d.Mode == "snapshots" && doc.Acquisition.Kind == "capture" || d.Mode == "stream" && doc.Acquisition.Kind != "capture") {
				return invalid
			}
			if record.Status == "healthy" && d.Freshness == "source-timestamp" && (doc.Acquisition.Facts == nil || !record.SourceAt.Equal(doc.Acquisition.Facts.AsOf)) {
				return invalid
			}
			if record.Status == "healthy" && (!snapshot.Usable() || record.Binding == nil || doc.Binding != *record.Binding) {
				return invalid
			}
			if doc.Binding.Run != binding.Run || doc.Binding.Source != binding.Source || doc.Binding.Namespace != binding.Namespace || doc.Binding.Phase != binding.Phase && (record.Kind != "baseline" || doc.Binding.Phase != "before") || record.Records != len(doc.Rows) || record.Bytes != doc.Material.Size {
				return invalid
			}

			if record.CapturePath != "" {
				if record.CapturePath != "capture" {
					return invalid
				}
				readback, err := networkaction.OpenCaptureEvidence(filepath.Join(path, "capture"))
				result, capture := readback.Result, readback.Capture
				if err != nil || capture == nil || result.Binding.Source != binding.Source || !artifactdir.MatchesSubtree(files, "capture", networkaction.ResultSchema, readback.Identity) {
					return invalid
				}
				journal, err := capturejournal.Verify(artifactdir.Subtree(files, "capture/journal"))
				if err != nil {
					return err
				}
				if record.Status == "healthy" && (journal.JournalIncomplete || journal.DeliveryUncertain || journal.Unsent > 0 || journal.StopReason != durablerun.Cancelled) {
					return invalid
				}
				source, err := DecodeCapture(doc.Acquisition.SourceConfiguration)
				if err != nil {
					return err
				}
				material, excluded, status, err := captureMaterial(capture, source, binding)
				if err != nil || dataset.Digest(material) != doc.Material.SHA256 {
					return invalid
				}
				a, _ := json.Marshal(excluded, json.Deterministic(true))
				b, _ := json.Marshal(record.Excluded, json.Deterministic(true))
				if !bytes.Equal(a, b) || status != "healthy" && record.Status == "healthy" {
					return invalid
				}
			} else if len(record.Excluded) > 0 {
				return invalid
			}
		} else if record.Identity != "" {
			return invalid
		}
		if record.BarrierPath != "" {
			if d.Barrier == nil || !strings.HasPrefix(record.BarrierPath, "samples/barrier-") || strings.ContainsAny(record.BarrierPath[len("samples/barrier-"):], "/\\.") {
				return invalid
			}
			snapshot, err := dataset.Open(ctx, filepath.Join(path, record.BarrierPath))
			if err != nil {
				return err
			}
			if !artifactdir.MatchesSubtree(files, record.BarrierPath, dataset.Schema, snapshot.Identity()) {
				return invalid
			}
			barrierDocument := snapshot.Document()
			reusedFinal := record.CapturePath != "" && snapshot.Identity() == lastBarrierIdentity
			if record.Status == "healthy" && !lastBarrierAcquisition.IsZero() && !barrierDocument.Acquisition.StartedAt.After(lastBarrierAcquisition) && !reusedFinal {
				return invalid
			}
			lastBarrierAcquisition = barrierDocument.Acquisition.CompletedAt
			lastBarrierIdentity = snapshot.Identity()
			expected, err := barrierEvidence(snapshot, *d.Barrier, binding.Run)
			if err != nil && record.Status == "healthy" {
				return invalid
			}
			if err == nil {
				if expected.Complete && record.Snapshot != "" && record.Status == "healthy" {
					primary, e := dataset.Open(ctx, filepath.Join(path, record.Snapshot))
					if e != nil || !artifactdir.MatchesSubtree(files, record.Snapshot, dataset.Schema, primary.Identity()) || primary.Document().Acquisition.StartedAt.Before(barrierDocument.Acquisition.CompletedAt) {
						return invalid
					}
				}
				a, _ := json.Marshal(expected, json.Deterministic(true))
				b, _ := json.Marshal(record.Barrier, json.Deterministic(true))
				if !bytes.Equal(a, b) {
					return invalid
				}
			}
		} else if record.Barrier != nil {
			return invalid
		}
		r.Records = append(r.Records, record)
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	r = finalDecision(r)
	if len(files["identity.sha256"]) == 0 {
		if sealed {
			return Result{}, invalid
		}
		r.State = "incomplete"
		r.Reason = "interrupted"
		r.Boundary = "insufficient"
		return r, nil
	}
	if torn || strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ResultSchema, files) {
		return Result{}, invalid
	}
	var declared Result
	if json.Unmarshal(files["manifest.json"], &declared, json.RejectUnknownMembers(true)) != nil {
		return Result{}, invalid
	}
	// A cancellation can make a complete interval unusable, never the reverse.
	if declared.Reason == "runner-deadline-or-cancellation" {
		r.State = "incomplete"
		r.Reason = declared.Reason
		r.Boundary = "insufficient"
	}
	a, _ := json.Marshal(r, json.Deterministic(true))
	b, _ := json.Marshal(declared, json.Deterministic(true))
	if !bytes.Equal(a, b) {
		return Result{}, invalid
	}
	r.Identity = strings.TrimSpace(string(files["identity.sha256"]))
	return r, nil
}
