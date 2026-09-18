package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusInputs are the declarations every corpus in these tests is written
// from. They are stated once so a test that changes one changes it on purpose.
var corpusInputs = []string{
	"--seed", "7", "--base-time", "2026-01-02T03:04:05Z",
	"--generator-version", "readmit-corpus-v1", "--profile-version", "readmit-siu-v1",
}

// generateCorpus writes one corpus and its manifest into a new directory and
// returns the corpus path and the summary the command printed.
func generateCorpus(t *testing.T, name string, messages string, framing ...string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	stream := filepath.Join(directory, name)
	args := append([]string{"corpus", "generate", "--output", stream, "--manifest", filepath.Join(directory, name+".json"), "--messages", messages}, corpusInputs...)
	stdout, stderr, err := run(t, append(args, framing...)...)
	if err != nil || stderr != "" {
		t.Fatalf("corpus generate: %v %s", err, stderr)
	}
	return stream, stdout
}

var framedCorpus = []string{"--framing", "mllp", "--terminator", "cr", "--encoding", "us-ascii", "--direction", "inbound"}
var batchCorpus = []string{"--framing", "batch", "--batch-boundary", "segment-start", "--terminator", "cr", "--encoding", "us-ascii", "--direction", "inbound"}

func digestOf(t *testing.T, summary string) string {
	t.Helper()
	for line := range strings.SplitSeq(summary, "\n") {
		if after, ok := strings.CutPrefix(line, "Digest: "); ok {
			return after
		}
	}
	t.Fatalf("no digest in:\n%s", summary)
	return ""
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return string(body)
}

func summaryField(t *testing.T, summary, name string) string {
	t.Helper()
	for line := range strings.SplitSeq(summary, "\n") {
		if after, ok := strings.CutPrefix(line, name+": "); ok {
			return after
		}
	}
	t.Fatalf("no %q in:\n%s", name, summary)
	return ""
}

func TestCorpusGeneratesTheSameStreamFromTheSameDeclaredInputs(t *testing.T) {
	_, first := generateCorpus(t, "a.mllp", "2000", framedCorpus...)
	_, second := generateCorpus(t, "b.mllp", "2000", framedCorpus...)
	if digestOf(t, first) != digestOf(t, second) {
		t.Fatalf("the same declarations produced different corpora:\n%s\n%s", first, second)
	}
	for _, want := range []string{
		"Corpus manifest: readmit-corpus/v1", "Generator: readmit-corpus-v1", "Profile: readmit-siu-v1",
		"Seed: 7", "Base time: 2026-01-02T03:04:05Z", "Framing: mllp", "Messages: 2000",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("missing %q in:\n%s", want, first)
		}
	}
	// The summary reports the declarations and the counts, never a location.
	for _, secret := range []string{"a.mllp", "b.mllp", ".json", "/var", "/tmp"} {
		if strings.Contains(first, secret) {
			t.Errorf("the generation summary disclosed %q", secret)
		}
	}
}

func TestCorpusScanHoldsOneBatchHoweverLongTheStreamIs(t *testing.T) {
	small, _ := generateCorpus(t, "small.mllp", "2000", framedCorpus...)
	large, _ := generateCorpus(t, "large.mllp", "16000", framedCorpus...)
	scanSmall, stderr, err := run(t, append([]string{"corpus", "scan", small}, framedCorpus...)...)
	if err != nil || stderr != "" {
		t.Fatalf("corpus scan: %v %s", err, stderr)
	}
	scanLarge, stderr, err := run(t, append([]string{"corpus", "scan", large}, framedCorpus...)...)
	if err != nil || stderr != "" {
		t.Fatalf("corpus scan: %v %s", err, stderr)
	}
	if summaryField(t, scanSmall, "Records") != "2000" || summaryField(t, scanLarge, "Records") != "16000" {
		t.Fatalf("records %q and %q, want 2000 and 16000", summaryField(t, scanSmall, "Records"), summaryField(t, scanLarge, "Records"))
	}
	peak := summaryField(t, scanSmall, "Peak resident bytes")
	if other := summaryField(t, scanLarge, "Peak resident bytes"); peak != other {
		t.Errorf("peak resident moved from %s to %s across an eightfold stream", peak, other)
	}
	if summaryField(t, scanLarge, "State") != "completed" || summaryField(t, scanSmall, "Case bounds") != "within" {
		t.Errorf("unexpected states in:\n%s", scanLarge)
	}
	if !strings.Contains(scanLarge, "Targets: engineering targets, not measurements or customer requirements") {
		t.Errorf("the scan summary reported its targets as something else:\n%s", scanLarge)
	}
}

func TestCorpusScanRendersOneWindowAndNamesTheCaseBoundsItIsPast(t *testing.T) {
	stream, _ := generateCorpus(t, "batch.hl7", "300", batchCorpus...)
	stdout, stderr, err := run(t, append([]string{"corpus", "scan", stream, "--window-offset", "150", "--window-limit", "4"}, batchCorpus...)...)
	if err != nil || stderr != "" {
		t.Fatalf("corpus scan: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Window: 4 records from offset 150 of 300") {
		t.Errorf("missing the rendered window in:\n%s", stdout)
	}
	for _, want := range []string{"record 151 offset", "record 154 offset"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "record 155 ") || strings.Contains(stdout, "record 150 ") {
		t.Errorf("the window rendered a record outside it:\n%s", stdout)
	}
	// 300 batch records are 300 case bundle sources, which is past the bound a
	// case holds. The scan names the bound; it does not widen it.
	if bounds := summaryField(t, stdout, "Case bounds"); !strings.Contains(bounds, "exceeded sources (128)") {
		t.Errorf("case bounds %q, want the source bound named", bounds)
	}
	// No window renders no rows, and a window past the render bound is refused.
	counted, _, err := run(t, append([]string{"corpus", "scan", stream}, batchCorpus...)...)
	if err != nil || !strings.Contains(counted, "Window: none requested") {
		t.Fatalf("a scan with no window: %v\n%s", err, counted)
	}
	_, stderr, err = run(t, append([]string{"corpus", "scan", stream, "--window-limit", "201"}, batchCorpus...)...)
	if err == nil || !strings.Contains(stderr, "renders at most 200") {
		t.Fatalf("an oversized window was accepted: %v %s", err, stderr)
	}
}

func TestCorpusScanWritesABenchmarkNamingItsInputsBoundsAndMachine(t *testing.T) {
	stream, generated := generateCorpus(t, "bench.mllp", "1000", framedCorpus...)
	directory := t.TempDir()
	written := filepath.Join(directory, "benchmark.json")
	stdout, stderr, err := run(t, append([]string{"corpus", "scan", stream, "--report", written}, framedCorpus...)...)
	if err != nil || stderr != "" {
		t.Fatalf("corpus scan: %v %s", err, stderr)
	}
	if summaryField(t, stdout, "Digest") != digestOf(t, generated) {
		t.Fatalf("the scan digested %s where the generation wrote %s", summaryField(t, stdout, "Digest"), digestOf(t, generated))
	}
	body := readFile(t, written)
	for _, want := range []string{
		`"schema": "readmit-benchmark/v1"`, `"readmit-import-plan/v1"`,
		`"note": "engineering targets, not measurements or customer requirements"`,
		`"resident_bound"`, `"peak_resident_bytes"`, `"go_version"`, `"cpus"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in the benchmark:\n%s", want, body)
		}
	}
	// A benchmark records what it read, never where it read it.
	for _, secret := range []string{"bench.mllp", "benchmark.json", directory} {
		if strings.Contains(body, secret) {
			t.Errorf("the benchmark disclosed %q", secret)
		}
	}
	// The destination is never overwritten.
	_, stderr, err = run(t, append([]string{"corpus", "scan", stream, "--report", written}, framedCorpus...)...)
	if err == nil || !strings.Contains(stderr, "must be a new file") {
		t.Fatalf("an existing benchmark was overwritten: %v %s", err, stderr)
	}
}

func TestCorpusReportsBoundedProgressThatNamesNothing(t *testing.T) {
	stream, _ := generateCorpus(t, "progress.mllp", "20000", framedCorpus...)
	stdout, stderr, err := run(t, append([]string{"corpus", "scan", stream, "--progress"}, framedCorpus...)...)
	if err != nil {
		t.Fatalf("corpus scan: %v %s", err, stderr)
	}
	if !strings.Contains(stderr, "scanning: bytes=") || !strings.Contains(stderr, "records=") {
		t.Fatalf("no progress reported:\n%s", stderr)
	}
	if lines := strings.Count(stderr, "\n"); lines > 16 {
		t.Errorf("progress reported %d lines for one scan", lines)
	}
	for _, secret := range []string{"progress.mllp", "MSH", "CORPUS-", "PLACER-", "/var", "/tmp"} {
		if strings.Contains(stderr, secret) {
			t.Errorf("progress disclosed %q", secret)
		}
	}
	if summaryField(t, stdout, "Records") != "20000" {
		t.Errorf("records %q, want 20000", summaryField(t, stdout, "Records"))
	}
}

func TestCorpusRefusesDeclarationsAndDestinationsItWasNotGiven(t *testing.T) {
	stream, _ := generateCorpus(t, "declared.mllp", "20", framedCorpus...)
	directory := t.TempDir()
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"a generation with no message count", append([]string{"corpus", "generate", "--output", filepath.Join(directory, "a"), "--manifest", filepath.Join(directory, "a.json")}, append(append([]string{}, corpusInputs...), framedCorpus...)...), "corpus generate requires"},
		{"a generation with no declared framing", append([]string{"corpus", "generate", "--output", filepath.Join(directory, "b"), "--manifest", filepath.Join(directory, "b.json"), "--messages", "4"}, corpusInputs...), "requires --plan"},
		{"a generation over an existing corpus", append([]string{"corpus", "generate", "--output", stream, "--manifest", filepath.Join(directory, "c.json"), "--messages", "4"}, append(append([]string{}, corpusInputs...), framedCorpus...)...), "must be new"},
		{"a scan of a declaration the bytes contradict", append([]string{"corpus", "scan", stream}, batchCorpus...), "contradict the declared framing"},
		{"a scan with no declaration at all", []string{"corpus", "scan", stream}, "requires --plan"},
		{"a scan of a directory", append([]string{"corpus", "scan", directory}, framedCorpus...), "readable regular file"},
		{"a corpus command with no subcommand", []string{"corpus"}, "requires a subcommand"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, err := run(t, c.args...)
			if err == nil {
				t.Fatalf("accepted:\n%s", stdout)
			}
			if !strings.Contains(stderr, c.want) {
				t.Fatalf("diagnostic %q, want one naming %q", stderr, c.want)
			}
		})
	}
}
