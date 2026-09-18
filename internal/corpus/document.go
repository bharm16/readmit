package corpus

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"runtime"
	"time"

	"github.com/bharm16/readmit/internal/importer"
)

// ErrUnsupportedVersion reports a document written under a contract version
// this release does not read. It is distinct from a document this release reads
// and rejects, so a caller can say which one it was handed.
var ErrUnsupportedVersion = errors.New("unsupported corpus document version")

// Manifest is what one generation wrote: the declarations it ran under, the
// length of the corpus and its digest. It records no path and no file name —
// the corpus is identified by what it is, not by where somebody put it — and it
// holds no byte of the corpus itself.
//
// It is written after the corpus, so a manifest beside a corpus is the
// completion marker for it: a generation interrupted at any point leaves no
// manifest, and Write removes the partial corpus it created.
type Manifest struct {
	Schema string `json:"schema"`
	Inputs Inputs `json:"inputs"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// UnmarshalJSON checks the declared members are present before the strict
// decode, so an absent count is told apart from a declared zero.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema *string `json:"schema"`
		Bytes  *int64  `json:"bytes"`
		SHA256 *string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil {
		return errors.New("a corpus manifest declares its contract version")
	}
	if *required.Schema != ManifestSchema {
		return ErrUnsupportedVersion
	}
	if required.Bytes == nil || required.SHA256 == nil {
		return errors.New("a corpus manifest declares its length and its digest")
	}
	type plainManifest Manifest
	var value plainManifest
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid corpus manifest")
	}
	*m = Manifest(value)
	return nil
}

// Validate reports the first reason a manifest cannot be used.
func (m Manifest) Validate() error {
	if m.Schema != ManifestSchema {
		return ErrUnsupportedVersion
	}
	if err := m.Inputs.Validate(); err != nil {
		return err
	}
	if m.Bytes < 1 || m.Bytes > importer.MaxStreamBytes {
		return errors.New("a corpus manifest records a length inside the stream limit")
	}
	if len(m.SHA256) != 64 {
		return errors.New("a corpus manifest records a SHA-256 digest of the corpus")
	}
	return nil
}

// Hardware is the machine a benchmark ran on, at the resolution a published
// number needs and no finer. It names no host, no user and no path: which
// operating system, which instruction set, how many processors and which
// compiler are what another person needs in order to repeat the run.
type Hardware struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CPUs      int    `json:"cpus"`
	GoVersion string `json:"go_version"`
}

// ThisMachine describes the machine this process is running on.
func ThisMachine() Hardware {
	return Hardware{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), GoVersion: runtime.Version()}
}

// Scanned is the corpus a benchmark ran against: the declarations it was read
// under and the digest of the bytes that were read. A corpus manifest records
// the digest of the bytes that were written, so equal digests are what says the
// two documents describe the same corpus.
type Scanned struct {
	Plan   importer.Plan `json:"plan"`
	Bytes  int64         `json:"bytes"`
	SHA256 string        `json:"sha256"`
}

// Bounds are the declared limits the scan ran under, so a number can be
// compared against another run only when the bounds match.
type Bounds struct {
	BatchRecords  int `json:"batch_records"`
	BatchBytes    int `json:"batch_bytes"`
	RecordBytes   int `json:"record_bytes"`
	ResidentBound int `json:"resident_bound"`
}

// Measured is what the run actually did. Every member here is an observation of
// this run on this machine. None of it is a target, and none of it is a claim
// about any other machine.
type Measured struct {
	ElapsedMilliseconds int64 `json:"elapsed_milliseconds"`
	Bytes               int64 `json:"bytes"`
	Records             int64 `json:"records"`
	Occurrences         int64 `json:"occurrences"`
	Decoded             int64 `json:"decoded"`
	Undecodable         int64 `json:"undecodable"`
	Batches             int64 `json:"batches"`
	PeakResidentBytes   int   `json:"peak_resident_bytes"`
}

// TargetNote is the one sentence that keeps this document honest. #25 states it
// directly, and repeating it inside the artifact means a number lifted out of
// this file cannot arrive somewhere else as a measurement.
const TargetNote = "engineering targets, not measurements or customer requirements"

// Targets are the performance envelope proposed in #25, recorded beside the
// measurement so the two are read together and never confused. They are
// declared here as data, not asserted: nothing in this release compares a
// measured number against one of them and reports a verdict.
type Targets struct {
	Note                    string `json:"note"`
	Messages                int64  `json:"messages"`
	Bytes                   int64  `json:"bytes"`
	WarmSearchP95Ms         int    `json:"warm_search_p95_milliseconds"`
	NavigationMs            int    `json:"navigation_milliseconds"`
	CancelAcknowledgementMs int    `json:"cancel_acknowledgement_milliseconds"`
}

// ProposedTargets is the envelope #25 proposes, as data.
func ProposedTargets() Targets {
	return Targets{
		Note: TargetNote, Messages: 1_000_000, Bytes: 5 << 30,
		WarmSearchP95Ms: 1000, NavigationMs: 200, CancelAcknowledgementMs: 2000,
	}
}

// Benchmark is one measured scan, published with everything another person
// needs in order to repeat it: the declared corpus, the declared bounds, the
// machine, and the proposed targets it is NOT a verdict against.
type Benchmark struct {
	Schema   string   `json:"schema"`
	Corpus   Scanned  `json:"corpus"`
	Bounds   Bounds   `json:"bounds"`
	Measured Measured `json:"measured"`
	Hardware Hardware `json:"hardware"`
	Targets  Targets  `json:"targets"`
}

// NewBenchmark composes the document for one completed scan.
func NewBenchmark(plan importer.Plan, result importer.ScanResult, bounds Bounds, elapsed time.Duration) Benchmark {
	return Benchmark{
		Schema: BenchmarkSchema,
		Corpus: Scanned{Plan: plan, Bytes: result.Bytes, SHA256: result.SHA256},
		Bounds: bounds,
		Measured: Measured{
			ElapsedMilliseconds: elapsed.Milliseconds(),
			Bytes:               result.Bytes,
			Records:             result.Records,
			Occurrences:         result.Occurrences,
			Decoded:             result.Decoded,
			Undecodable:         result.Undecodable,
			Batches:             result.Batches,
			PeakResidentBytes:   result.PeakResidentBytes,
		},
		Hardware: ThisMachine(),
		Targets:  ProposedTargets(),
	}
}

// UnmarshalJSON checks the declared version before the strict decode, so a
// later version is reported as unsupported rather than as invalid.
func (b *Benchmark) UnmarshalJSON(data []byte) error {
	var required struct {
		Schema *string `json:"schema"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Schema == nil {
		return errors.New("a benchmark declares its contract version")
	}
	if *required.Schema != BenchmarkSchema {
		return ErrUnsupportedVersion
	}
	type plainBenchmark Benchmark
	var value plainBenchmark
	if err := json.Unmarshal(data, &value, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid benchmark document")
	}
	*b = Benchmark(value)
	return nil
}

// Validate reports the first reason a benchmark cannot be used.
func (b Benchmark) Validate() error {
	if b.Schema != BenchmarkSchema {
		return ErrUnsupportedVersion
	}
	if err := b.Corpus.Plan.Validate(); err != nil {
		return err
	}
	if b.Targets.Note != TargetNote {
		return errors.New("a benchmark records the proposed targets as targets, never as measurements")
	}
	if b.Measured.Records < 0 || b.Measured.Bytes < 0 || b.Measured.PeakResidentBytes < 0 {
		return errors.New("a benchmark records counts that were observed")
	}
	if b.Bounds.ResidentBound < b.Measured.PeakResidentBytes {
		return errors.New("a benchmark cannot record a peak past the resident bound it ran under")
	}
	return nil
}

// EncodeManifest and EncodeBenchmark write each document deterministically, so
// the same document produces the same bytes.
func EncodeManifest(m Manifest) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return encode(m, "cannot encode the corpus manifest")
}

func EncodeBenchmark(b Benchmark) ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return encode(b, "cannot encode the benchmark")
}

func encode(document any, failure string) ([]byte, error) {
	data, err := json.Marshal(document, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, errors.New(failure)
	}
	return append(data, '\n'), nil
}

// DecodeManifest and DecodeBenchmark read each document. Unknown members and
// unknown versions are errors; there is no migration and no repair.
func DecodeManifest(data []byte) (Manifest, error) {
	if len(data) > MaxManifestBytes {
		return Manifest{}, errors.New("corpus manifest exceeds its size limit")
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return Manifest{}, ErrUnsupportedVersion
		}
		return Manifest{}, errors.New("invalid corpus manifest")
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func DecodeBenchmark(data []byte) (Benchmark, error) {
	if len(data) > MaxManifestBytes {
		return Benchmark{}, errors.New("benchmark exceeds its size limit")
	}
	var b Benchmark
	if err := json.Unmarshal(data, &b); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return Benchmark{}, ErrUnsupportedVersion
		}
		return Benchmark{}, errors.New("invalid benchmark document")
	}
	if err := b.Validate(); err != nil {
		return Benchmark{}, err
	}
	return b, nil
}
