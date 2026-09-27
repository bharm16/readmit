package observesource

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const DatasetAcquisitionSchema = "readmit-dataset-acquisition/v1"

type DatasetRequest struct {
	Source     Source
	Projection dataset.Projection
	Binding    dataset.Binding
	Output     string
	Policy     *sendpolicy.Policy
	Resolve    sendpolicy.Resolver
	// An execution adapter binds this callback to its exact approved inputs.
	Authorize func(context.Context) error
}
type datasetReceipt struct {
	Schema          string          `json:"schema"`
	Binding         dataset.Binding `json:"binding"`
	DatasetIdentity string          `json:"dataset_identity"`
}

var datasetFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "dataset acquisition", Nested: []string{"dataset"}, RequiredFiles: []string{"manifest.json", "identity.sha256"}, AllowFile: func(n string) bool { return n == "manifest.json" || n == "decision.json" || n == "identity.sha256" }, MaxFiles: 8, MaxFileBytes: 64 << 20, MaxBytes: 80 << 20}, Seal: artifactdir.DirectoryHash(DatasetAcquisitionSchema)}

// CollectDataset is the connected orchestrator's typed acquisition seam. It
// reuses existing readers, TLS/credential policy, parsers and fixed SQL builder.
// One complete snapshot is not a claim that a downstream horizon has closed.
func CollectDataset(ctx context.Context, request DatasetRequest) (*dataset.Snapshot, error) {
	refuse := errors.New("typed observation could not be acquired")
	sourceRaw, err := EncodeSource(request.Source)
	if err != nil || request.Projection.Validate() != nil || dataset.ValidateBinding(request.Binding) != nil || request.Binding.Source != request.Source.Identity() || request.Authorize == nil {
		return nil, refuse
	}
	source, err := DecodeSource(sourceRaw)
	if err != nil {
		return nil, refuse
	}
	rawProjection, _ := json.Marshal(request.Projection)
	projection, err := dataset.DecodeProjection(rawProjection)
	if err != nil {
		return nil, refuse
	}
	if source.Extraction != nil {
		a, _ := json.Marshal(source.Extraction.Shape())
		b, _ := json.Marshal(projection.Shape())
		if !bytes.Equal(a, b) {
			return nil, refuse
		}
	}
	if source.Observes.Kind == DatabaseQuery && projection.Format != "database" || source.Observes.Kind == DownstreamCapture && projection.Format != "hl7" || source.Observes.Kind == HTTPAPI && projection.Format == "json" && len(projection.Continuation) == 0 {
		return nil, refuse
	}
	if source.File != nil && projection.Limits.MaxBytes > source.File.MaxBytes {
		return nil, refuse
	}
	if source.HTTP != nil {
		duration, _ := time.ParseDuration(source.HTTP.Timeout)
		if projection.Limits.MaxBytes > source.HTTP.MaxBytes || time.Duration(projection.Limits.TimeoutMS)*time.Millisecond > duration {
			return nil, refuse
		}
	}
	if source.Database != nil {
		limits := source.Database.limits()
		duration, _ := time.ParseDuration(limits.Timeout)
		if projection.Limits.MaxBytes > limits.MaxBytes || projection.Limits.MaxRows > limits.MaxRows || time.Duration(projection.Limits.TimeoutMS)*time.Millisecond > duration {
			return nil, refuse
		}
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(projection.Limits.TimeoutMS)*time.Millisecond)
	defer cancel()
	authorize := func() error {
		if bounded.Err() != nil {
			return bounded.Err()
		}
		return request.Authorize(bounded)
	}
	if authorize() != nil {
		return nil, refuse
	}
	writer, err := artifactdir.Create(request.Output, datasetFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	retention := &snapshot{recordDecision: func(d sendpolicy.Decision) error {
		raw, err := sendpolicy.EncodeDecision(d)
		if err != nil {
			return err
		}
		if err = writer.WriteFile("decision.json", raw); err != nil {
			return err
		}
		return writer.Sync()
	}}
	started := time.Now().UTC()
	a := dataset.Acquisition{Kind: map[string]string{FileExport: "file", HTTPAPI: "http", DatabaseQuery: "database", DownstreamCapture: "capture"}[source.Observes.Kind], Status: "failed", StartedAt: started, SourceConfiguration: sourceRaw, Completion: "snapshot"}
	var material []byte
	if source.Enabled && authorize() == nil {
		open, err := newReader(bounded, source, retention, Options{Policy: request.Policy, Resolve: request.Resolve})
		if err == nil {
			defer open.close()
			if h, ok := open.(*httpReader); ok {
				h.datasetRead = true
				h.endpoint.MaxBytes = projection.Limits.MaxBytes
			}
			if f, ok := open.(*fileReader); ok {
				f.export.MaxBytes = projection.Limits.MaxBytes
			}
			if authorize() == nil {
				var taken attempt
				if db, ok := open.(*databaseReader); ok {
					taken = db.readDataset(bounded, projection)
				} else {
					taken = open.read(bounded)
				}
				switch taken.status {
				case observewindow.Observed:
					a.Status = "complete"
				case observewindow.SampleMissing:
					a.Status = "missing"
				case observewindow.SampleTruncated:
					a.Status = "truncated"
				}
				records := len(taken.keys)
				if source.Database != nil {
					records = taken.record.Records
				}
				a.Facts = &dataset.AcquisitionFacts{Status: string(taken.status), Attempts: taken.record.Attempts, Retries: taken.record.Retries, HTTPStatus: taken.record.HTTPStatus, StatedAge: taken.record.StatedAge, Bytes: taken.record.Bytes, Records: records, AsOf: taken.asOf, ObservedFrom: taken.from, Note: taken.record.Note}
				material = taken.evidence["body"]
				if source.Database != nil {
					material = taken.evidence["dataset-database.json"]
				}
				if source.Capture != nil && taken.status == observewindow.Observed {
					capture, err := bundle.Open(source.Capture.Path)
					if err != nil || !bytes.Equal(taken.evidence["identity.sha256"], []byte(capture.Identity+"\n")) {
						a.Status = "failed"
					} else {
						record := dataset.CaptureRead{Schema: dataset.CaptureSchema, Identity: capture.Identity, Rows: []dataset.CaptureRow{}}
						for _, event := range capture.Events {
							if !slices.Contains(source.Capture.Kinds, string(event.Kind)) {
								continue
							}
							raw, err := capture.Raw(event.ID)
							if err != nil {
								a.Status = "failed"
								break
							}
							record.Rows = append(record.Rows, dataset.CaptureRow{Occurrence: event.ID, Raw: raw})
						}
						material, err = json.Marshal(record, json.Deterministic(true))
						if err != nil {
							a.Status = "failed"
						}
					}
				}
			}
		}
	}
	if bounded.Err() != nil {
		a.Status = "cancelled"
	}
	a.CompletedAt = time.Now().UTC()
	if len(material) > dataset.MaxBytes {
		a.Status = "truncated"
		material = nil
	}
	result, err := dataset.Build(context.WithoutCancel(ctx), request.Binding, projection, a, material)
	if err != nil {
		return nil, refuse
	}
	if err = result.Write(context.WithoutCancel(ctx), filepath.Join(writer.Path(), "dataset")); err != nil {
		return nil, err
	}
	manifest, _ := json.Marshal(datasetReceipt{Schema: DatasetAcquisitionSchema, Binding: request.Binding, DatasetIdentity: result.Identity()}, json.Deterministic(true))
	if writer.WriteFile("manifest.json", manifest) != nil {
		return nil, refuse
	}
	if _, err = writer.Seal(nil); err != nil {
		return nil, err
	}
	return OpenDataset(context.WithoutCancel(ctx), writer.Path())
}
func OpenDataset(ctx context.Context, path string) (*dataset.Snapshot, error) {
	files, err := artifactdir.Read(path, datasetFamily.Layout)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(DatasetAcquisitionSchema, files) {
		return nil, errors.New("dataset acquisition identity mismatch")
	}
	var receipt datasetReceipt
	if json.Unmarshal(files["manifest.json"], &receipt, json.RejectUnknownMembers(true)) != nil || receipt.Schema != DatasetAcquisitionSchema {
		return nil, errors.New("invalid dataset acquisition")
	}
	result, err := dataset.Open(ctx, filepath.Join(path, "dataset"))
	if err != nil {
		return nil, err
	}
	if result.Identity() != receipt.DatasetIdentity || result.Document().Binding != receipt.Binding {
		return nil, errors.New("dataset acquisition binding mismatch")
	}
	return result, nil
}
