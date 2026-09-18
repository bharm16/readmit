package corpus_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/corpus"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

func baseTime(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, "2026-01-02T03:04:05Z")
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func declared(t *testing.T, messages int) corpus.Inputs {
	t.Helper()
	return corpus.Inputs{
		Generator: bundle.GeneratorInputs{
			Seed: 42, BaseTime: baseTime(t),
			GeneratorVersion: corpus.GeneratorVersion, ProfileVersion: corpus.ProfileVersion,
		},
		Messages: messages,
		Plan: importer.Plan{
			Schema: importer.PlanSchema, Framing: importer.MLLPFraming,
			Terminator: hl7.CR, Encoding: importer.USASCII, Direction: "inbound", Members: []string{},
		},
	}
}

func generate(t *testing.T, inputs corpus.Inputs) (corpus.Summary, []byte) {
	t.Helper()
	var written bytes.Buffer
	summary, err := corpus.Generate(context.Background(), &written, inputs, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return summary, written.Bytes()
}

func TestGenerateReproducesTheSameBytesFromTheSameDeclaredInputs(t *testing.T) {
	inputs := declared(t, 64)
	first, bytesWritten := generate(t, inputs)
	second, again := generate(t, inputs)
	if !bytes.Equal(bytesWritten, again) {
		t.Fatal("the same declared inputs produced different corpus bytes")
	}
	if first.SHA256 != second.SHA256 || first.Bytes != second.Bytes || first.Messages != 64 {
		t.Fatalf("summaries %+v and %+v disagree about the same corpus", first, second)
	}
	if first.Bytes != int64(len(bytesWritten)) {
		t.Errorf("summary records %d bytes of a %d byte corpus", first.Bytes, len(bytesWritten))
	}
	// Each declared input is an input: changing one changes the corpus.
	for _, change := range []func(*corpus.Inputs){
		func(i *corpus.Inputs) { i.Generator.Seed = 43 },
		func(i *corpus.Inputs) { i.Generator.BaseTime = i.Generator.BaseTime.Add(time.Hour) },
		func(i *corpus.Inputs) { i.Messages = 65 },
	} {
		changed := declared(t, 64)
		change(&changed)
		if _, other := generate(t, changed); bytes.Equal(bytesWritten, other) {
			t.Error("a changed declared input produced the same corpus")
		}
	}
	// Nothing in the corpus depends on the clock or the machine, so its first
	// message is a byte literal a person can check.
	if got := string(bytesWritten[:len(firstMessage)]); got != firstMessage {
		t.Errorf("first message\n%q\nwant\n%q", got, firstMessage)
	}
}

// firstMessage is the first message of the 64-message corpus the declarations
// above name, authored here as an explicit byte literal from the
// readmit-corpus-v1 draw order and the readmit-siu-v1 field contract. It was
// not exported by the generator.
//
// Its four identifiers are the first four outputs of the PCG stream
// readmit-corpus-v1 declares, computed independently of Go with 64-bit and
// 128-bit Python integer arithmetic over the algorithm documented in the pinned
// go1.27.1 source at src/math/rand/v2/pcg.go. The initial 128-bit state is the
// declared seed 42 as the high 64 bits and the fixed stream 726561646D697463
// as the low 64 bits, and the successive outputs are D851B2B93899832F (the
// trigger, reduced over four triggers to S15), 090A6095BCE528D8 (the patient),
// F1287093EEE68127 (the placer) and 7C32B9E2ED1EAB66 (the filler). The declared
// time is the base time plus one minute, and the appointment is a day later
// and thirty minutes long.
const firstMessage = "\x0bMSH|^~\\&|READMIT|CORPUS|RECEIVER|READMIT|20260102030505+0000||SIU^S15|CORPUS-00000001|T|2.5.1\r" +
	"SCH|PLACER-F1287093EEE68127^READMIT|FILLER-7C32B9E2ED1EAB66^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260103030505+0000^20260103033505+0000\r" +
	"PID|1||CORPUS-090A6095BCE528D8^^^READMIT^MR||SYNTHETIC^PATIENT\r\x1c\r"

func TestAGeneratedCorpusScansBackToWhatWasDeclared(t *testing.T) {
	for _, framing := range []struct {
		name    string
		framing importer.Framing
		bound   importer.Boundary
	}{
		{"mllp", importer.MLLPFraming, ""},
		{"batch", importer.BatchFraming, importer.SegmentStart},
	} {
		t.Run(framing.name, func(t *testing.T) {
			inputs := declared(t, 300)
			inputs.Plan.Framing = framing.framing
			inputs.Plan.BatchBoundary = framing.bound
			summary, written := generate(t, inputs)
			result, err := importer.Scan(context.Background(), bytes.NewReader(written), importer.ScanOptions{Plan: inputs.Plan})
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if result.Records != 300 || result.Occurrences != 300 || result.Decoded != 300 || result.Undecodable != 0 {
				t.Fatalf("records %d occurrences %d decoded %d undecodable %d, want 300 300 300 0",
					result.Records, result.Occurrences, result.Decoded, result.Undecodable)
			}
			if result.SHA256 != summary.SHA256 {
				t.Errorf("the scan read %s where the generation wrote %s", result.SHA256, summary.SHA256)
			}
		})
	}
}

func TestGenerateRefusesInputsThatWereNotDeclared(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*corpus.Inputs)
		want   string
	}{
		{"an unimplemented generator", func(i *corpus.Inputs) { i.Generator.GeneratorVersion = "readmit-corpus-v2" }, "generator version"},
		{"an unimplemented profile", func(i *corpus.Inputs) { i.Generator.ProfileVersion = "readmit-siu-v2" }, "profile version"},
		{"a base time nobody declared", func(i *corpus.Inputs) { i.Generator.BaseTime = time.Time{} }, "base time"},
		{"a fractional base time", func(i *corpus.Inputs) { i.Generator.BaseTime = i.Generator.BaseTime.Add(time.Millisecond) }, "whole second"},
		{"no messages", func(i *corpus.Inputs) { i.Messages = 0 }, "between 1 and"},
		{"more messages than one corpus holds", func(i *corpus.Inputs) { i.Messages = corpus.MaxMessages + 1 }, "between 1 and"},
		{"raw framing", func(i *corpus.Inputs) { i.Plan.Framing = importer.RawFraming }, "mllp or batch"},
		{"an hl7 batch envelope", func(i *corpus.Inputs) {
			i.Plan.Framing, i.Plan.BatchBoundary = importer.BatchFraming, importer.HL7Batch
		}, "segment-start"},
		{"a terminator this generator does not write", func(i *corpus.Inputs) { i.Plan.Terminator = hl7.LF }, "cr segment terminator"},
		{"an encoding this generator does not write", func(i *corpus.Inputs) { i.Plan.Encoding = importer.Latin1 }, "us-ascii or utf-8"},
		{"declared container members", func(i *corpus.Inputs) { i.Plan.Members = []string{".hl7"} }, "one stream"},
		{"an unsupported plan version", func(i *corpus.Inputs) { i.Plan.Schema = "readmit-import-plan/v2" }, "unsupported import plan version"},
	} {
		t.Run(c.name, func(t *testing.T) {
			inputs := declared(t, 4)
			c.change(&inputs)
			var written bytes.Buffer
			_, err := corpus.Generate(context.Background(), &written, inputs, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("generate error %v, want one naming %q", err, c.want)
			}
			if written.Len() != 0 {
				t.Errorf("a refused generation wrote %d bytes", written.Len())
			}
		})
	}
}

func TestGenerateReportsBoundedProgressAndStopsWhenCancelled(t *testing.T) {
	// Progress is counts only, and the counts only ever grow.
	var reports []corpus.Progress
	var written bytes.Buffer
	if _, err := corpus.Generate(context.Background(), &written, declared(t, 9_000), func(p corpus.Progress) {
		reports = append(reports, p)
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(reports) < 2 {
		t.Fatalf("a 9000 message corpus reported progress %d times", len(reports))
	}
	for i := 1; i < len(reports); i++ {
		if reports[i].Messages <= reports[i-1].Messages || reports[i].Bytes <= reports[i-1].Bytes {
			t.Fatalf("progress went from %+v to %+v", reports[i-1], reports[i])
		}
	}
	// A cancellation stops the generation where it is. The bytes already handed
	// to the writer were written; a cancellation does not unwrite them, and the
	// summary says how far it got rather than claiming a whole corpus.
	ctx, cancel := context.WithCancel(context.Background())
	stopping := &cancelling{cancel: cancel, after: 4 << 10}
	summary, err := corpus.Generate(ctx, stopping, declared(t, 9_000), nil)
	cancel()
	if !errors.Is(err, corpus.ErrCancelled) {
		t.Fatalf("generate error %v, want %v", err, corpus.ErrCancelled)
	}
	if summary.Messages == 0 || summary.Messages >= 9_000 {
		t.Errorf("a cancelled generation reported %d of 9000 messages", summary.Messages)
	}
	if summary.SHA256 != "" {
		t.Error("a cancelled generation reported a digest of a corpus it did not finish")
	}
}

func TestWriteLeavesNeitherACorpusNorAManifestWhenItCannotFinish(t *testing.T) {
	directory := t.TempDir()
	stream := filepath.Join(directory, "corpus.mllp")
	manifest := filepath.Join(directory, "corpus.json")
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := corpus.Write(stopped, stream, manifest, declared(t, 1_000), nil); !errors.Is(err, corpus.ErrCancelled) {
		t.Fatalf("write error %v, want %v", err, corpus.ErrCancelled)
	}
	for _, path := range []string{stream, manifest} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("a cancelled generation left %s behind", filepath.Base(path))
		}
	}
}

func TestWriteRefusesDestinationsThatAreNotNewOrAreInsideEvidence(t *testing.T) {
	directory := t.TempDir()
	taken := filepath.Join(directory, "taken")
	if err := os.WriteFile(taken, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(directory, "case")
	if err := os.Mkdir(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		stream   string
		manifest string
		want     string
	}{
		{"a corpus destination that exists", taken, filepath.Join(directory, "a.json"), "must be new"},
		{"a manifest destination that exists", filepath.Join(directory, "b.mllp"), taken, "must be a new file"},
		{"one destination for both", filepath.Join(directory, "c"), filepath.Join(directory, "c"), "two new files"},
		{"a corpus inside retained evidence", filepath.Join(evidence, "d.mllp"), filepath.Join(directory, "d.json"), "outside the immutable input case"},
		{"a manifest inside retained evidence", filepath.Join(directory, "e.mllp"), filepath.Join(evidence, "e.json"), "outside the immutable input case"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := corpus.Write(context.Background(), c.stream, c.manifest, declared(t, 4), nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("write error %v, want one naming %q", err, c.want)
			}
			if _, statErr := os.Lstat(c.stream); c.stream != taken && !os.IsNotExist(statErr) {
				t.Error("a refused generation left a corpus behind")
			}
		})
	}
}

func TestWriteRecordsTheInputsAndTheDigestOfWhatItWrote(t *testing.T) {
	directory := t.TempDir()
	stream := filepath.Join(directory, "corpus.mllp")
	path := filepath.Join(directory, "corpus.json")
	inputs := declared(t, 128)
	manifest, err := corpus.Write(context.Background(), stream, path, inputs, nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	written, err := os.ReadFile(stream)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Bytes != int64(len(written)) || manifest.Schema != corpus.ManifestSchema {
		t.Fatalf("manifest %+v does not describe the %d bytes beside it", manifest, len(written))
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := corpus.DecodeManifest(encoded)
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if reopened.SHA256 != manifest.SHA256 || reopened.Inputs.Messages != 128 {
		t.Fatalf("reopened manifest %+v is not the one that was written", reopened)
	}
	// The manifest names what the corpus is, never where somebody put it.
	for _, name := range []string{"corpus.mllp", "corpus.json", directory} {
		if strings.Contains(string(encoded), name) {
			t.Errorf("the manifest disclosed the path %q", name)
		}
	}
	// Reading the corpus back with the declared plan reproduces the digest the
	// manifest recorded, which is the whole of what reproducible means here.
	opened, err := corpus.Open(stream)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer opened.Close()
	result, err := importer.Scan(context.Background(), opened, importer.ScanOptions{Plan: reopened.Inputs.Plan})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if result.SHA256 != reopened.SHA256 || result.Records != 128 {
		t.Fatalf("scan read %d records digesting %s, want 128 and %s", result.Records, result.SHA256, reopened.SHA256)
	}
}

func TestOpenRefusesAnythingThatIsNotAReadableRegularFile(t *testing.T) {
	directory := t.TempDir()
	if _, err := corpus.Open(directory); err == nil {
		t.Error("a directory was accepted as a stream")
	}
	if _, err := corpus.Open(filepath.Join(directory, "absent")); err == nil {
		t.Error("a missing file was accepted as a stream")
	}
}

func TestDocumentsRejectUnknownMembersAndUnknownVersions(t *testing.T) {
	directory := t.TempDir()
	manifest, err := corpus.Write(context.Background(), filepath.Join(directory, "c.mllp"), filepath.Join(directory, "c.json"), declared(t, 4), nil)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	encoded, err := corpus.EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, c := range []struct {
		name string
		body string
		want error
	}{
		{"an unknown member", strings.Replace(string(encoded), `"bytes"`, `"extra": 1, "bytes"`, 1), nil},
		{"a later version", strings.Replace(string(encoded), corpus.ManifestSchema, "readmit-corpus/v2", 1), corpus.ErrUnsupportedVersion},
		{"no version at all", strings.Replace(string(encoded), `"schema"`, `"version"`, 1), nil},
		{"an absent length", strings.Replace(string(encoded), `"bytes"`, `"unknown"`, 1), nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := corpus.DecodeManifest([]byte(c.body))
			if err == nil {
				t.Fatal("an invalid manifest was read")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("decode error %v, want %v", err, c.want)
			}
		})
	}
	benchmark := corpus.NewBenchmark(manifest.Inputs.Plan, importer.ScanResult{Bytes: 1, Records: 1, PeakResidentBytes: 1}, corpus.Bounds{ResidentBound: importer.ResidentBound}, time.Second)
	body, err := corpus.EncodeBenchmark(benchmark)
	if err != nil {
		t.Fatalf("encode benchmark: %v", err)
	}
	reopened, err := corpus.DecodeBenchmark(body)
	if err != nil {
		t.Fatalf("decode benchmark: %v", err)
	}
	if reopened.Measured.ElapsedMilliseconds != 1000 || reopened.Hardware.OS == "" || reopened.Hardware.GoVersion == "" {
		t.Fatalf("benchmark %+v lost what a repeat of it needs", reopened)
	}
	for _, c := range []struct {
		name string
		body string
		want error
	}{
		{"an unknown member", strings.Replace(string(body), `"bounds"`, `"extra": 1, "bounds"`, 1), nil},
		{"a later version", strings.Replace(string(body), corpus.BenchmarkSchema, "readmit-benchmark/v2", 1), corpus.ErrUnsupportedVersion},
	} {
		t.Run("benchmark with "+c.name, func(t *testing.T) {
			_, err := corpus.DecodeBenchmark([]byte(c.body))
			if err == nil {
				t.Fatal("an invalid benchmark was read")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("decode error %v, want %v", err, c.want)
			}
		})
	}
}

func TestABenchmarkKeepsProposedTargetsApartFromWhatItMeasured(t *testing.T) {
	plan := declared(t, 4).Plan
	measured := importer.ScanResult{Bytes: 4096, Records: 32, Occurrences: 32, Decoded: 32, PeakResidentBytes: 1024}
	bounds := corpus.Bounds{BatchRecords: importer.MaxBatchRecords, BatchBytes: importer.MaxBatchBytes, RecordBytes: importer.MaxRecordBytes, ResidentBound: importer.ResidentBound}
	benchmark := corpus.NewBenchmark(plan, measured, bounds, 250*time.Millisecond)
	if benchmark.Targets.Note != corpus.TargetNote {
		t.Fatalf("targets note %q, want %q", benchmark.Targets.Note, corpus.TargetNote)
	}
	if benchmark.Measured.Records != 32 || benchmark.Targets.Messages != 1_000_000 {
		t.Fatalf("benchmark %+v confused what it measured with what was proposed", benchmark)
	}
	// A document that presents a target as anything other than a target is not
	// a document this release will write or read.
	relabelled := benchmark
	relabelled.Targets.Note = "measured on this machine"
	if _, err := corpus.EncodeBenchmark(relabelled); err == nil {
		t.Error("a benchmark relabelled its targets as measurements")
	}
	// A peak past the bound it declares it ran under is not a measurement of
	// anything this release can produce.
	impossible := benchmark
	impossible.Measured.PeakResidentBytes = bounds.ResidentBound + 1
	if _, err := corpus.EncodeBenchmark(impossible); err == nil {
		t.Error("a benchmark recorded a peak past its own resident bound")
	}
}

func FuzzCorpusManifest(f *testing.F) {
	f.Add(`{"schema":"readmit-corpus/v1"}`)
	f.Add(`{"schema":"readmit-corpus/v2","bytes":1,"sha256":""}`)
	f.Fuzz(func(t *testing.T, body string) {
		manifest, err := corpus.DecodeManifest([]byte(body))
		if err != nil {
			return
		}
		if err := manifest.Validate(); err != nil {
			t.Fatalf("a decoded manifest failed its own validation: %v", err)
		}
		if manifest.Schema != corpus.ManifestSchema {
			t.Fatalf("decoded an unsupported version %q", manifest.Schema)
		}
	})
}

func FuzzBenchmarkDocument(f *testing.F) {
	f.Add(`{"schema":"readmit-benchmark/v1"}`)
	f.Add(`{"schema":"readmit-benchmark/v1","targets":{"note":"measured"}}`)
	f.Fuzz(func(t *testing.T, body string) {
		benchmark, err := corpus.DecodeBenchmark([]byte(body))
		if err != nil {
			return
		}
		if err := benchmark.Validate(); err != nil {
			t.Fatalf("a decoded benchmark failed its own validation: %v", err)
		}
		if benchmark.Targets.Note != corpus.TargetNote {
			t.Fatalf("decoded a benchmark whose targets are labelled %q", benchmark.Targets.Note)
		}
	})
}

// cancelling is a writer that cancels its context once it has accepted a
// declared number of bytes, so a cancellation lands inside a generation rather
// than before one.
type cancelling struct {
	cancel context.CancelFunc
	after  int
	taken  int
}

func (c *cancelling) Write(p []byte) (int, error) {
	c.taken += len(p)
	if c.taken >= c.after {
		c.cancel()
	}
	return len(p), nil
}

var _ io.Writer = (*cancelling)(nil)
