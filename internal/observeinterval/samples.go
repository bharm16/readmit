package observeinterval

import (
	"context"
	"encoding/json/v2"
	"regexp"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
)

// SamplesSchema seals an interval whose samples are retained under another
// typed contract, such as a FHIR search. The dataset/v1 interval reader refuses
// it; completion, coverage and barrier decisions are this package's own.
const SamplesSchema = "readmit-observation-interval-samples/v1"

var sampleContract = regexp.MustCompile(`^readmit-[a-z0-9-]{1,64}/v[1-9][0-9]{0,3}$`)

var samplesFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "observation interval", AllowedDirectories: []string{"samples"}, Nested: []string{"samples"}, RequiredFiles: []string{"definition.json", "binding.json", "samples.json", "journal.jsonl", "manifest.json", "identity.sha256"}, AllowFile: func(n string) bool {
	return n == "definition.json" || n == "binding.json" || n == "samples.json" || n == "journal.jsonl" || n == "manifest.json" || n == "identity.sha256"
}, MaxFiles: 50000, MaxFileBytes: 64 << 20, MaxBytes: 1 << 30}, Seal: artifactdir.DirectoryHash(SamplesSchema)}

// Sample is one retained acquisition under a typed contract other than
// dataset/v1. Write retains its sealed bytes; the contract's own reader, given
// to OpenSamples, verifies them again without contacting the source.
type Sample interface {
	Identity() string
	Usable() bool
	Binding() dataset.Binding
	Started() time.Time
	Completed() time.Time
	Records() int
	Size() int
	Write(context.Context, string) error
}
type SampleOpener func(ctx context.Context, files map[string][]byte) (Sample, error)

type sampleHead struct {
	Schema string `json:"schema"`
}

func sampleDeclaration(schema string) []byte {
	raw, _ := json.Marshal(sampleHead{Schema: schema}, json.Deterministic(true))
	return raw
}

// OpenSamples verifies a sealed samples interval with the contract's reader.
func OpenSamples(ctx context.Context, path string, opener SampleOpener) (Result, error) {
	if opener == nil {
		return Result{}, invalid
	}
	return read(ctx, path, true, opener)
}

// RecoverSamples is the read-only view of an interrupted samples interval.
func RecoverSamples(ctx context.Context, path string, opener SampleOpener) (Result, error) {
	if opener == nil {
		return Result{}, invalid
	}
	return read(ctx, path, false, opener)
}

// primary is the retained sample of one journal record, whichever contract
// retained it. Only a dataset/v1 snapshot can carry capture material.
type primary struct {
	identity           string
	binding            dataset.Binding
	kind               string
	started, completed time.Time
	asOf               *time.Time
	records, size      int
	usable             bool
	snapshot           *dataset.Snapshot
}

func openPrimary(ctx context.Context, files map[string][]byte, opener SampleOpener) (primary, error) {
	if opener != nil {
		s, err := opener(ctx, files)
		if err != nil || s == nil {
			return primary{}, invalid
		}
		return primary{identity: s.Identity(), binding: s.Binding(), kind: "sample", started: s.Started(), completed: s.Completed(), records: s.Records(), size: s.Size(), usable: s.Usable()}, nil
	}
	snapshot, err := dataset.Verify(ctx, files)
	if err != nil {
		return primary{}, err
	}
	doc := snapshot.Document()
	p := primary{identity: snapshot.Identity(), binding: doc.Binding, kind: doc.Acquisition.Kind, started: doc.Acquisition.StartedAt, completed: doc.Acquisition.CompletedAt, records: len(doc.Rows), size: doc.Material.Size, usable: snapshot.Usable(), snapshot: snapshot}
	if doc.Acquisition.Facts != nil {
		at := doc.Acquisition.Facts.AsOf
		p.asOf = &at
	}
	return p, nil
}
