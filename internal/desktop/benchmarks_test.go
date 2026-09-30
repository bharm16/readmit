package desktop_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/corpus"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
)

// benchmarkApp is an activated window over a new named project, the
// project's context, and the folder benchmarks are kept in inside it.
func benchmarkApp(t *testing.T) (*desktop.App, desktop.RequestContext, string) {
	t.Helper()
	app, context := connectionsProject(t)
	return app, context, filepath.Join(context.Project, ".readmit", "benchmarks")
}

// inputRequest is a Generate input request as the window sends it: the
// defaults the facade reports, with a name, a seed and a count.
func inputRequest(t *testing.T, app *desktop.App, context desktop.RequestContext, name, seed string, messages int) desktop.GenerateInputRequest {
	t.Helper()
	defaults := app.BenchmarkDefaults().Defaults
	return desktop.GenerateInputRequest{Context: context, Name: name, Seed: seed, BaseTime: "2026-01-02T03:04:05Z", GeneratorVersion: defaults.GeneratorVersion,
		ProfileVersion: defaults.ProfileVersion, Messages: messages, Plan: defaults.Plan}
}

func generateInput(t *testing.T, app *desktop.App, request desktop.GenerateInputRequest) desktop.BenchmarkInput {
	t.Helper()
	result := app.GenerateInput(request)
	if result.State != desktop.Completed || result.Input == nil {
		t.Fatalf("GenerateInput: %+v", result)
	}
	return *result.Input
}

// A generated input retains the seed, base time and versions that produced
// it, and generating again from exactly those retained declarations writes
// the same bytes, which are the bytes `readmit corpus generate` writes.
func TestGeneratedInputIsReproducibleFromItsRetainedSeedTimeAndVersions(t *testing.T) {
	app, context, state := benchmarkApp(t)
	first := generateInput(t, app, inputRequest(t, app, context, "Scheduling sample", "18446744073709551615", 500))
	retained := first.Manifest
	again := generateInput(t, app, desktop.GenerateInputRequest{Context: context, Name: "Again", Seed: retained.Seed, BaseTime: retained.BaseTime,
		GeneratorVersion: retained.GeneratorVersion, ProfileVersion: retained.ProfileVersion, Messages: retained.Messages, Plan: retained.Plan})
	if again.ID == first.ID || again.Manifest.SHA256 != first.Manifest.SHA256 || again.Manifest.Bytes != first.Manifest.Bytes {
		t.Fatalf("the retained declarations did not reproduce the input: %+v %+v", first, again)
	}
	command := t.TempDir()
	output := filepath.Join(command, "corpus")
	args := append([]string{"generate", "--output", output, "--manifest", output + ".json", "--seed", retained.Seed,
		"--base-time", retained.BaseTime, "--generator-version", retained.GeneratorVersion,
		"--profile-version", retained.ProfileVersion, "--messages", strconv.Itoa(retained.Messages)}, planFlags(retained.Plan)...)
	if _, stderr, err := corpusCommand(t, args...); err != nil {
		t.Fatalf("corpus generate: %v %s", err, stderr)
	}
	managed := mustReadFile(t, filepath.Join(state, "inputs", first.ID, "corpus"))
	if string(managed) != string(mustReadFile(t, filepath.Join(command, "corpus"))) {
		t.Fatal("the generated input differs from the command's corpus for the same declarations")
	}
	if first.Manifest.GeneratorVersion != corpus.GeneratorVersion || first.Manifest.ProfileVersion != corpus.ProfileVersion || first.Manifest.BaseTime != "2026-01-02T03:04:05Z" {
		t.Fatalf("the input did not retain its declarations: %+v", first.Manifest)
	}
}

// Generating writes a synthetic corpus and its manifest into the
// application's own folder and records the named input. It imports no case,
// measures nothing, and is new authoring: an unactivated window is refused
// and writes nothing.
func TestGeneratedInputIsSyntheticAndImportsNothing(t *testing.T) {
	app, context, state := benchmarkApp(t)
	input := generateInput(t, app, inputRequest(t, app, context, "Scheduling sample", "7", 20))
	if input.Origin != desktop.SyntheticOrigin || input.Name != "Scheduling sample" || input.Availability != desktop.ItemAvailable || input.CreatedAt == "" {
		t.Fatalf("the generated input: %+v", input)
	}
	listed := app.ListBenchmarkInputs(context)
	if listed.State != desktop.Completed || len(listed.Inputs) != 1 || !reflect.DeepEqual(listed.Inputs[0], input) {
		t.Fatalf("the input list: %+v", listed)
	}
	if results := app.ListBenchmarks(context); results.State != desktop.Empty || len(results.Results) != 0 {
		t.Fatalf("generating recorded a measurement: %+v", results)
	}
	if got := entriesOf(t, filepath.Join(state, "inputs", input.ID)); !reflect.DeepEqual(got, []string{"corpus", "manifest.json"}) {
		t.Fatalf("the input folder holds %v", got)
	}
	if got := entriesOf(t, state); !reflect.DeepEqual(got, []string{"benchmarks.json", "inputs"}) {
		t.Fatalf("the project's benchmarks folder holds %v", got)
	}
	if got := entriesOf(t, context.Project); slices.ContainsFunc(got, func(name string) bool { return name != ".readmit" && name != "project.json" }) {
		t.Fatalf("generating wrote an entry of the project: %v", got)
	}

	unactivated := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()})
	refused := unactivated.GenerateInput(inputRequest(t, unactivated, context, "Refused", "7", 20))
	if refused.State != desktop.PermissionDenied || refused.Input != nil {
		t.Fatalf("an unactivated generation: %+v", refused)
	}
	if listed := app.ListBenchmarkInputs(context); len(listed.Inputs) != 1 {
		t.Fatalf("a refused generation recorded %+v", listed)
	}
	if listed := app.ListBenchmarkInputs(desktop.RequestContext{}); listed.State == desktop.Completed || listed.State == desktop.Empty {
		t.Fatalf("benchmarks were listed without a project: %+v", listed)
	}
}

// The message count is bounded by the generator's own bound, which the
// defaults report; a count outside it is refused and nothing is recorded.
func TestGenerateInputRefusesCountsPastTheGeneratorBound(t *testing.T) {
	app, context, _ := benchmarkApp(t)
	defaults := app.BenchmarkDefaults()
	if defaults.State != desktop.Completed || defaults.Defaults.MaxMessages != corpus.MaxMessages {
		t.Fatalf("the defaults: %+v", defaults)
	}
	for _, messages := range []int{0, -1, corpus.MaxMessages + 1} {
		refused := app.GenerateInput(inputRequest(t, app, context, "Too many", "7", messages))
		if refused.State != desktop.Failed || refused.Reason != "a corpus holds between 1 and 1048576 messages" {
			t.Errorf("%d messages: %+v", messages, refused)
		}
	}
	unnamed := app.GenerateInput(inputRequest(t, app, context, "  ", "7", 1))
	if unnamed.State != desktop.Failed {
		t.Errorf("an unnamed input: %+v", unnamed)
	}
	raw := inputRequest(t, app, context, "Raw", "7", 1)
	raw.Plan.Framing = importer.RawFraming
	if refused := app.GenerateInput(raw); refused.State != desktop.Failed {
		t.Errorf("a raw-framed input: %+v", refused)
	}
	if listed := app.ListBenchmarkInputs(context); listed.State != desktop.Empty {
		t.Fatalf("a refused generation recorded %+v", listed)
	}
}

// A generation cancelled while it writes records nothing and leaves nothing
// in the application's folder.
func TestACancelledGenerationRecordsNoInput(t *testing.T) {
	app, context, state := benchmarkApp(t)
	answered := make(chan desktop.GenerateInputResult, 1)
	go func() { answered <- app.GenerateInput(inputRequest(t, app, context, "Large", "7", 200000)) }()
	awaitCorpusProgress(t, app, func(p desktop.CorpusProgress) bool { return p.Messages > 0 }, answered)
	app.Cancel("corpus")
	cancelled := <-answered
	if cancelled.State != desktop.Cancelled || cancelled.Input != nil {
		t.Fatalf("a cancelled generation answered %+v", cancelled)
	}
	if listed := app.ListBenchmarkInputs(context); listed.State != desktop.Empty {
		t.Fatalf("a cancelled generation recorded %+v", listed)
	}
	if left := entriesOf(t, filepath.Join(state, "inputs")); len(left) != 0 {
		t.Fatalf("a cancelled generation left %v", left)
	}
}

// Start benchmark's limits default to the scanner's documented limits, which
// the defaults report and a result records as the bounds it ran under; a
// changed limit is the one the scan runs under.
func TestBenchmarkUsesTheDocumentedDefaultLimits(t *testing.T) {
	app, context, _ := benchmarkApp(t)
	defaults := app.BenchmarkDefaults().Defaults
	if defaults.BatchRecords != 256 || defaults.BatchBytes != 8388608 || defaults.MaxBatchRecords != 256 || defaults.MaxBatchBytes != 8388608 ||
		defaults.WindowLimit != 0 || defaults.MaxWindowLimit != 200 || defaults.Seed != "0" {
		t.Fatalf("the documented limits: %+v", defaults)
	}
	if defaults.Plan.Framing != importer.MLLPFraming || defaults.Plan.Terminator != hl7.CR || defaults.Plan.Encoding != importer.UTF8 || defaults.Plan.Direction != bundle.Unknown {
		t.Fatalf("the default format: %+v", defaults.Plan)
	}
	input := generateInput(t, app, inputRequest(t, app, context, "Sample", "7", 300))
	started := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, InputID: input.ID})
	if started.State != desktop.Completed || started.Result == nil || started.Result.Bounds.BatchRecords != 256 || started.Result.Bounds.BatchBytes != 8388608 {
		t.Fatalf("a benchmark at the defaults: %+v", started)
	}
	if started.Scan == nil || len(started.Scan.Rows) != 0 || started.Result.Batches != 2 {
		t.Fatalf("a counts-only benchmark: %+v", started.Scan)
	}
	changed := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, InputID: input.ID, BatchRecords: 16, BatchBytes: 4096})
	if changed.State != desktop.Completed || changed.Result.Bounds.BatchRecords != 16 || changed.Result.Bounds.BatchBytes != 4096 || changed.Result.Batches < 19 {
		t.Fatalf("a benchmark under changed limits: %+v", changed.Result)
	}
	for _, refused := range []desktop.StartBenchmarkRequest{
		{Context: context, InputID: input.ID, BatchRecords: 257},
		{Context: context, InputID: input.ID, BatchBytes: 8388609},
	} {
		if result := app.StartBenchmark(refused); result.State != desktop.Failed || result.Result != nil {
			t.Errorf("limits past the scanner's bounds: %+v", result)
		}
	}
}

// A stopped scan is recorded as incomplete with the records and bytes it
// actually read, and with no duration, peak, digest or case-bounds verdict
// claimed for the whole input; no benchmark document is written for it.
func TestAnIncompleteBenchmarkCarriesNoCompleteFileMetrics(t *testing.T) {
	app, context, state := benchmarkApp(t)
	const messages = 60000
	input := generateInput(t, app, inputRequest(t, app, context, "Large", "7", messages))
	answered := make(chan desktop.StartBenchmarkResult, 1)
	go func() {
		answered <- app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, InputID: input.ID, BatchRecords: 1})
	}()
	reached := awaitCorpusProgress(t, app, func(p desktop.CorpusProgress) bool { return p.Batches > 0 }, answered)
	app.Cancel("corpus")
	stopped := <-answered
	if stopped.State != desktop.Cancelled || stopped.Result == nil {
		t.Fatalf("a stopped benchmark: %+v", stopped)
	}
	result := *stopped.Result
	if result.Completion != desktop.BenchmarkIncomplete || result.Records < reached.Records || result.Records >= messages || result.Bytes < 1 || result.Bytes >= input.Manifest.Bytes {
		t.Fatalf("the incomplete result: %+v", result)
	}
	if result.ElapsedMilliseconds != nil || result.PeakScanBufferBytes != nil || result.SHA256 != "" || result.CaseBounds != "" || len(result.Exceeded) != 0 {
		t.Fatalf("an incomplete result claimed a complete-file metric: %+v", result)
	}
	if left := entriesOf(t, state); slices.Contains(left, "results") && len(entriesOf(t, filepath.Join(state, "results"))) != 0 {
		t.Fatal("a stopped scan wrote a benchmark document")
	}
	listed := app.ListBenchmarks(context)
	if listed.State != desktop.Completed || len(listed.Results) != 1 || !reflect.DeepEqual(listed.Results[0], result) {
		t.Fatalf("the incomplete result is not listed as recorded: %+v", listed)
	}
	opened := app.OpenBenchmark(desktop.BenchmarkRequest{Context: context, ID: result.ID})
	if opened.State != desktop.Completed || opened.Availability != desktop.ItemAvailable || !reflect.DeepEqual(*opened.Result, result) {
		t.Fatalf("opening the incomplete result: %+v", opened)
	}
	if progress := app.CorpusProgress(); progress.State != desktop.Empty {
		t.Fatalf("progress outlived the benchmark: %+v", progress)
	}
}

// Results are listed newest first, and results of the same second by
// identity.
func TestBenchmarksAreListedNewestFirst(t *testing.T) {
	app, context, _ := benchmarkApp(t)
	input := generateInput(t, app, inputRequest(t, app, context, "Sample", "7", 10))
	var ids []string
	for _, at := range []string{"2026-03-01T10:00:00Z", "2026-03-03T10:00:00Z", "2026-03-02T10:00:00Z", "2026-03-03T10:00:00Z"} {
		instant, _ := time.Parse(time.RFC3339, at)
		desktop.SetClockForTest(app, func() time.Time { return instant })
		started := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, InputID: input.ID})
		if started.State != desktop.Completed {
			t.Fatal(started)
		}
		ids = append(ids, started.Result.ID)
	}
	newest := []string{ids[1], ids[3]}
	slices.Sort(newest)
	want := append(newest, ids[2], ids[0])
	listed := app.ListBenchmarks(context)
	var got []string
	for _, result := range listed.Results {
		got = append(got, result.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

// A generated input is scanned under the format its manifest records, so
// nothing about it is detected or declared again; a different declaration is
// refused. A complete result writes the readmit-benchmark/v1 document the
// command writes, for the same bytes.
func TestBenchmarkOfAGeneratedInputUsesItsManifestPlan(t *testing.T) {
	app, context, state := benchmarkApp(t)
	request := inputRequest(t, app, context, "Batch", "7", 50)
	request.Plan = importer.Plan{Schema: importer.PlanSchema, Framing: importer.BatchFraming, BatchBoundary: importer.SegmentStart,
		Terminator: hl7.CR, Encoding: importer.USASCII, Direction: bundle.Outbound, Members: []string{}}
	input := generateInput(t, app, request)
	started := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, InputID: input.ID})
	if started.State != desktop.Completed || !reflect.DeepEqual(started.Result.Plan, input.Manifest.Plan) || started.Result.Records != 50 ||
		started.Result.SHA256 != input.Manifest.SHA256 || started.Result.Input != (desktop.BenchmarkSource{Kind: desktop.GeneratedInput, ID: input.ID, Name: "Batch"}) {
		t.Fatalf("a benchmark of a generated input: %+v", started)
	}
	if started.Result.Completion != desktop.BenchmarkComplete || started.Result.ElapsedMilliseconds == nil || started.Result.PeakScanBufferBytes == nil || started.Result.CaseBounds != desktop.CaseBoundsWithin {
		t.Fatalf("a complete result lacks what it measured: %+v", started.Result)
	}
	written, err := corpus.DecodeBenchmark(mustReadFile(t, filepath.Join(state, "results", started.Result.ID, "benchmark.json")))
	if err != nil || written.Corpus.SHA256 != input.Manifest.SHA256 || written.Measured.Records != 50 || written.Measured.PeakResidentBytes != *started.Result.PeakScanBufferBytes {
		t.Fatalf("the benchmark document: %+v %v", written, err)
	}
	if opened := app.OpenBenchmark(desktop.BenchmarkRequest{Context: context, ID: started.Result.ID}); opened.Availability != desktop.ItemAvailable {
		t.Fatalf("opening the result: %+v", opened)
	}
	other := input.Manifest.Plan
	other.Direction = bundle.Inbound
	if refused := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, InputID: input.ID, Plan: &other}); refused.State != desktop.Failed ||
		refused.Reason != "a generated input is scanned under the format its manifest records" {
		t.Fatalf("a different declaration of a generated input: %+v", refused)
	}
	file := filepath.Join(state, "inputs", input.ID, "corpus")
	if chosen := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, File: file}); chosen.State != desktop.Failed {
		t.Fatalf("a chosen file without a declared format: %+v", chosen)
	}
	chosen := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, File: file, Plan: &input.Manifest.Plan})
	if chosen.State != desktop.Completed || chosen.Result.Input != (desktop.BenchmarkSource{Kind: desktop.ChosenFile, Name: "corpus"}) || chosen.Result.Records != 50 {
		t.Fatalf("a chosen file: %+v", chosen)
	}
	if err := os.Remove(filepath.Join(state, "results", started.Result.ID, "benchmark.json")); err != nil {
		t.Fatal(err)
	}
	if opened := app.OpenBenchmark(desktop.BenchmarkRequest{Context: context, ID: started.Result.ID}); opened.State != desktop.Completed || opened.Availability != desktop.ItemMissing {
		t.Fatalf("a result whose document is gone: %+v", opened)
	}
	if missing := app.OpenBenchmark(desktop.BenchmarkRequest{Context: context, ID: "0123456789abcdef0123456789abcdef"}); missing.State != desktop.Failed || missing.Result != nil {
		t.Fatalf("an unrecorded result: %+v", missing)
	}
}

// The rendered window is 0 to 200 records from an offset at or after the
// first; anything else is refused before the input is read.
func TestStartBenchmarkRefusesADisplayWindowPast200(t *testing.T) {
	app, context, _ := benchmarkApp(t)
	input := generateInput(t, app, inputRequest(t, app, context, "Sample", "7", 250))
	for _, refused := range []desktop.StartBenchmarkRequest{
		{Context: context, InputID: input.ID, WindowLimit: 201},
		{Context: context, InputID: input.ID, WindowLimit: -1},
		{Context: context, InputID: input.ID, WindowOffset: -1},
	} {
		if result := app.StartBenchmark(refused); result.State != desktop.Failed || result.Scan != nil {
			t.Errorf("window %+v: %+v", refused, result)
		}
	}
	if results := app.ListBenchmarks(context); results.State != desktop.Empty {
		t.Fatalf("a refused benchmark recorded %+v", results)
	}
	widest := app.StartBenchmark(desktop.StartBenchmarkRequest{Context: context, InputID: input.ID, WindowOffset: 10, WindowLimit: 200})
	if widest.State != desktop.Completed || len(widest.Scan.Rows) != 200 || widest.Scan.WindowOffset != 10 {
		t.Fatalf("the widest window: %+v", widest.Scan)
	}
}
