package desktop_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/corpus"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/testlicense"
)

// corpusPlan is the plan a corpus is declared under, as the screen's
// structured controls send it.
func corpusPlan(framing importer.Framing, boundary importer.Boundary, encoding importer.Encoding) importer.Plan {
	return importer.Plan{Schema: importer.PlanSchema, Framing: framing, BatchBoundary: boundary, Terminator: hl7.CR,
		Encoding: encoding, Direction: bundle.Inbound, Members: []string{}}
}

// planFlags are the command's declaration flags for the same plan.
func planFlags(plan importer.Plan) []string {
	flags := []string{"--framing", string(plan.Framing), "--terminator", string(plan.Terminator), "--encoding", string(plan.Encoding), "--direction", string(plan.Direction)}
	if plan.BatchBoundary != "" {
		flags = append(flags, "--batch-boundary", string(plan.BatchBoundary))
	}
	return flags
}

// corpusCommand runs `readmit corpus ...` in process, activated the way the
// window is, and returns what it printed.
func corpusCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cli.Execute("dev", append([]string{"--operation-policy", testlicense.New(t), "corpus"}, args...), &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func generationRequest(folder, name, seed string, messages int, plan importer.Plan) desktop.CorpusGenerateRequest {
	return desktop.CorpusGenerateRequest{Seed: seed, BaseTime: "2026-01-02T03:04:05+02:00", GeneratorVersion: corpus.GeneratorVersion,
		ProfileVersion: corpus.ProfileVersion, Messages: messages, Plan: plan, Folder: folder, CorpusName: name, ManifestName: name + ".json"}
}

func generateArgs(output, seed string, messages int, plan importer.Plan) []string {
	return append([]string{"generate", "--output", output, "--manifest", output + ".json", "--seed", seed,
		"--base-time", "2026-01-02T03:04:05+02:00", "--generator-version", corpus.GeneratorVersion,
		"--profile-version", corpus.ProfileVersion, "--messages", fmt.Sprint(messages)}, planFlags(plan)...)
}

// The window generates a corpus through corpus.Write, the operation `readmit
// corpus generate` calls, so the same declarations write the same corpus and
// the same manifest byte for byte, for either framing and for a seed no
// JavaScript number can hold. The manifest the window reports is the one it
// wrote, member for member.
func TestCorpusGenerationWritesTheCommandsCorpusAndManifestBytes(t *testing.T) {
	app := workspaceApp(t)
	for _, plan := range []importer.Plan{
		corpusPlan(importer.MLLPFraming, "", importer.USASCII),
		corpusPlan(importer.BatchFraming, importer.SegmentStart, importer.UTF8),
	} {
		// The command reads a seed as its flag library reads an unsigned
		// number: 010 is octal and 0x1F hexadecimal there, and so here.
		for _, seed := range []string{"0", "18446744073709551615", "010", "0x1F"} {
			commandDir, windowDir := t.TempDir(), t.TempDir()
			stdout, stderr, err := corpusCommand(t, generateArgs(filepath.Join(commandDir, "corpus"), seed, 5000, plan)...)
			if err != nil || stderr != "" {
				t.Fatalf("corpus generate: %v %s", err, stderr)
			}
			result := app.GenerateCorpus(generationRequest(windowDir, "corpus", seed, 5000, plan))
			if result.State != desktop.Completed || result.Manifest == nil {
				t.Fatalf("GenerateCorpus: %+v", result)
			}
			if result.Corpus != filepath.Join(resolved(t, windowDir), "corpus") || result.ManifestPath != filepath.Join(resolved(t, windowDir), "corpus.json") {
				t.Fatalf("GenerateCorpus wrote elsewhere: %+v", result)
			}
			for _, name := range []string{"corpus", "corpus.json"} {
				command, window := mustReadFile(t, filepath.Join(commandDir, name)), mustReadFile(t, filepath.Join(windowDir, name))
				if !bytes.Equal(command, window) {
					t.Fatalf("%s %s seed %s: the window's %s differs from the command's", plan.Framing, plan.Encoding, seed, name)
				}
			}
			written, err := corpus.DecodeManifest(mustReadFile(t, filepath.Join(windowDir, "corpus.json")))
			if err != nil {
				t.Fatal(err)
			}
			view := result.Manifest
			if view.Schema != written.Schema || view.Seed != fmt.Sprint(written.Inputs.Generator.Seed) || view.BaseTime != "2026-01-02T01:04:05Z" ||
				view.GeneratorVersion != written.Inputs.Generator.GeneratorVersion || view.ProfileVersion != written.Inputs.Generator.ProfileVersion ||
				view.Messages != written.Inputs.Messages || !reflect.DeepEqual(view.Plan, written.Inputs.Plan) ||
				view.Bytes != written.Bytes || view.SHA256 != written.SHA256 {
				t.Fatalf("the window reported %+v for the manifest %+v", view, written)
			}
			// The command's summary states the same digest and count.
			if !strings.Contains(stdout, "Digest: "+view.SHA256) || !strings.Contains(stdout, "Messages: 5000") || !strings.Contains(stdout, "Seed: "+view.Seed+"\n") {
				t.Fatalf("the command summarised a different corpus:\n%s", stdout)
			}
			if progress := app.CorpusProgress(); progress.State != desktop.Empty || progress.Progress != nil {
				t.Fatalf("progress outlived the generation: %+v", progress)
			}
		}
	}
}

// Declarations the command refuses, the window refuses with the command's
// sentence, and an existing destination is never written over: neither the
// corpus nor the manifest is created and what was there is unchanged.
func TestCorpusGenerationRefusesWhatTheCommandRefuses(t *testing.T) {
	app := workspaceApp(t)
	folder := t.TempDir()
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	raw := corpusPlan(importer.RawFraming, "", importer.USASCII)
	latin := corpusPlan(importer.MLLPFraming, "", importer.Latin1)
	lf := framed
	lf.Terminator = hl7.LF
	existing := filepath.Join(folder, "existing")
	writeDocument(t, folder, "existing", "synthetic bytes somebody else wrote")
	for name, change := range map[string]struct {
		change func(*desktop.CorpusGenerateRequest)
		args   func([]string) []string
	}{
		"a fractional base time": {
			func(r *desktop.CorpusGenerateRequest) { r.BaseTime = "2026-01-02T03:04:05.5Z" },
			func(a []string) []string { return replaceFlag(a, "--base-time", "2026-01-02T03:04:05.5Z") }},
		"a base time without a zone": {
			func(r *desktop.CorpusGenerateRequest) { r.BaseTime = "2026-01-02T03:04:05" },
			func(a []string) []string { return replaceFlag(a, "--base-time", "2026-01-02T03:04:05") }},
		"an unimplemented generator": {
			func(r *desktop.CorpusGenerateRequest) { r.GeneratorVersion = "readmit-corpus-v2" },
			func(a []string) []string { return replaceFlag(a, "--generator-version", "readmit-corpus-v2") }},
		"an unimplemented profile": {
			func(r *desktop.CorpusGenerateRequest) { r.ProfileVersion = "readmit-adt-v1" },
			func(a []string) []string { return replaceFlag(a, "--profile-version", "readmit-adt-v1") }},
		"no messages": {
			func(r *desktop.CorpusGenerateRequest) { r.Messages = 0 },
			func(a []string) []string { return replaceFlag(a, "--messages", "0") }},
		"more messages than one corpus holds": {
			func(r *desktop.CorpusGenerateRequest) { r.Messages = corpus.MaxMessages + 1 },
			func(a []string) []string { return replaceFlag(a, "--messages", fmt.Sprint(corpus.MaxMessages+1)) }},
		"raw framing": {
			func(r *desktop.CorpusGenerateRequest) { r.Plan = raw },
			func(a []string) []string { return replaceFlag(a, "--framing", "raw") }},
		"a latin-1 encoding": {
			func(r *desktop.CorpusGenerateRequest) { r.Plan = latin },
			func(a []string) []string { return replaceFlag(a, "--encoding", "iso-8859-1") }},
		"an lf terminator": {
			func(r *desktop.CorpusGenerateRequest) { r.Plan = lf },
			func(a []string) []string { return replaceFlag(a, "--terminator", "lf") }},
		"an existing corpus destination": {
			func(r *desktop.CorpusGenerateRequest) { r.CorpusName = "existing" },
			func(a []string) []string { return replaceFlag(a, "--output", existing) }},
		"an existing manifest destination": {
			func(r *desktop.CorpusGenerateRequest) { r.ManifestName = "existing" },
			func(a []string) []string { return replaceFlag(a, "--manifest", existing) }},
	} {
		request := generationRequest(folder, "fresh", "7", 10, framed)
		change.change(&request)
		args := change.args(generateArgs(filepath.Join(folder, "fresh"), "7", 10, framed))
		_, stderr, cliErr := corpusCommand(t, args...)
		result := app.GenerateCorpus(request)
		if cliErr == nil || result.State != desktop.Failed || result.Manifest != nil {
			t.Fatalf("%s: the command answered %v and the window %+v", name, cliErr, result)
		}
		if want := "readmit: " + result.Reason + "\n"; stderr != want {
			t.Errorf("%s: the window refused with %q where the command printed %q", name, result.Reason, stderr)
		}
	}
	if listed := entriesOf(t, folder); !reflect.DeepEqual(listed, []string{"existing"}) {
		t.Fatalf("a refused generation wrote into the folder: %v", listed)
	}
	if got := string(mustReadFile(t, existing)); got != "synthetic bytes somebody else wrote" {
		t.Fatal("a refused generation changed an existing file")
	}
	for name, request := range map[string]desktop.CorpusGenerateRequest{
		"a seed that is not a number": generationRequest(folder, "fresh", "seven", 10, framed),
		"a negative seed":             generationRequest(folder, "fresh", "-1", 10, framed),
		"a seed past 2^64-1":          generationRequest(folder, "fresh", "18446744073709551616", 10, framed),
		"a nested corpus name":        generationRequest(folder, "nested/fresh", "7", 10, framed),
		"an escaping corpus name":     generationRequest(folder, "../fresh", "7", 10, framed),
		"the same name for both files": func() desktop.CorpusGenerateRequest {
			r := generationRequest(folder, "fresh", "7", 10, framed)
			r.ManifestName = "fresh"
			return r
		}(),
		"a folder that does not exist": generationRequest(filepath.Join(folder, "absent"), "fresh", "7", 10, framed),
	} {
		if result := app.GenerateCorpus(request); result.State != desktop.Failed {
			t.Errorf("%s: %+v", name, result)
		}
	}
	if listed := entriesOf(t, folder); !reflect.DeepEqual(listed, []string{"existing"}) {
		t.Fatalf("a refused generation wrote into the folder: %v", listed)
	}
}

func replaceFlag(args []string, flag, value string) []string {
	out := append([]string{}, args...)
	for i := range out {
		if out[i] == flag {
			out[i+1] = value
			return out
		}
	}
	panic("no flag " + flag)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return data
}

// scanLines writes a scan the way the command prints it, from what the window
// was given. Only the elapsed time is left out: two runs never take the same
// time, and the benchmark comparison below states how that one member is
// compared.
func scanLines(view *desktop.CorpusScanView, state string) string {
	var out strings.Builder
	boundary := string(view.Plan.BatchBoundary)
	if boundary == "" {
		boundary = "not declared"
	}
	fmt.Fprintf(&out, "Scan plan: %s\nFraming: %s\nBatch boundary: %s\nTerminator: %s\nEncoding: %s\nDirection: %s\n",
		view.Plan.Schema, view.Plan.Framing, boundary, view.Plan.Terminator, view.Plan.Encoding, view.Plan.Direction)
	fmt.Fprintf(&out, "State: %s\nBytes: %d\nDigest: %s\nRecords: %d\nOccurrences: %d\nDecoded: %d\nUndecodable: %d\n",
		state, view.Bytes, view.SHA256, view.Records, view.Occurrences, view.Decoded, view.Undecodable)
	fmt.Fprintf(&out, "Parsing batches: %d\nBatch bounds: %d records, %d bytes\nPeak resident bytes: %d\nResident bound: %d\n",
		view.Batches, view.Bounds.BatchRecords, view.Bounds.BatchBytes, view.PeakResidentBytes, view.Bounds.ResidentBound)
	switch view.CaseBounds {
	case desktop.CaseBoundsNotEvaluated:
		out.WriteString("Case bounds: not evaluated; the scan was cancelled\n")
	case desktop.CaseBoundsWithin:
		out.WriteString("Case bounds: within\n")
	default:
		fmt.Fprintf(&out, "Case bounds: exceeded %s\n", strings.Join(view.Exceeded, ", "))
	}
	if view.WindowLimit == 0 {
		out.WriteString("Window: none requested\n")
	} else {
		fmt.Fprintf(&out, "Window: %d records from offset %d of %d\n", len(view.Rows), view.WindowOffset, view.Records)
		for _, row := range view.Rows {
			fmt.Fprintf(&out, "  record %d offset %d size %d occurrences %d decoded %d undecodable %d\n",
				row.Ordinal, row.Offset, row.Size, row.Occurrences, row.Decoded, row.Undecodable)
		}
	}
	fmt.Fprintf(&out, "Targets: %s\n", view.Targets)
	return out.String()
}

// withoutElapsed drops the command's elapsed-time line.
func withoutElapsed(stdout string) string {
	var kept []string
	for line := range strings.SplitSeq(stdout, "\n") {
		if !strings.HasPrefix(line, "Elapsed: ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// A scan in the window streams the file through the same shared operation and
// importer.Scan the command does, so every count, bound, verdict and window
// row the command prints the window reports, and the benchmark it writes is
// the command's document: equal in every member except the elapsed time each
// run measured for itself. It holds what the scanner's documented bounds
// hold, not what the stream holds, and reads it without changing it.
func TestCorpusScanMatchesTheScanCommand(t *testing.T) {
	app := workspaceApp(t)
	source := t.TempDir()
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	batch := corpusPlan(importer.BatchFraming, importer.SegmentStart, importer.USASCII)
	for _, plan := range []importer.Plan{framed, batch} {
		name := string(plan.Framing)
		if generated := app.GenerateCorpus(generationRequest(source, name, "7", 300, plan)); generated.State != desktop.Completed {
			t.Fatal(generated)
		}
	}
	// A hand-written batch whose middle record the parser cannot decode.
	message := "MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||SIU^S12|MIXED-%d|T|2.5.1\rPID|1||R-1\r"
	writeDocument(t, source, "mixed", fmt.Sprintf(message, 1)+"MSH|^~\\&|SYNTH|LAB\rPID|1\x00\r"+fmt.Sprintf(message, 3))
	for label, scan := range map[string]struct {
		file    string
		request desktop.CorpusScanRequest
		flags   []string
	}{
		"framed, whole bounds, no window": {"mllp", desktop.CorpusScanRequest{Plan: framed}, planFlags(framed)},
		"framed, a window":                {"mllp", desktop.CorpusScanRequest{Plan: framed, WindowOffset: 150, WindowLimit: 4}, append(planFlags(framed), "--window-offset", "150", "--window-limit", "4")},
		"batch past the case bounds, small batches": {"batch", desktop.CorpusScanRequest{Plan: batch, BatchRecords: 7, BatchBytes: 5000, WindowLimit: 200},
			append(planFlags(batch), "--batch-records", "7", "--batch-bytes", "5000", "--window-limit", "200")},
		"a batch holding an undecodable record": {"mixed", desktop.CorpusScanRequest{Plan: batch, WindowLimit: 3},
			append(planFlags(batch), "--window-limit", "3")},
	} {
		path := filepath.Join(source, scan.file)
		before := sourceStateOf(t, path)
		reports := t.TempDir()
		stdout, stderr, err := corpusCommand(t, append([]string{"scan", path, "--report", filepath.Join(reports, "command.json")}, scan.flags...)...)
		if err != nil || stderr != "" {
			t.Fatalf("%s: corpus scan: %v %s", label, err, stderr)
		}
		request := scan.request
		request.File, request.ReportFolder, request.ReportName = path, reports, "window.json"
		result := app.ScanCorpus(request)
		if result.State != desktop.Completed || result.Scan == nil || result.Benchmark != filepath.Join(resolved(t, reports), "window.json") {
			t.Fatalf("%s: ScanCorpus: %+v", label, result)
		}
		if scan.file == "mixed" && (result.Scan.Undecodable != 1 || result.Scan.Decoded != 2) {
			t.Errorf("%s: the undecodable record was not counted: %+v", label, result.Scan)
		}
		if window, command := scanLines(result.Scan, "completed"), withoutElapsed(stdout); window != command {
			t.Errorf("%s: the window and the command differ:\nwindow:\n%s\ncommand:\n%s", label, window, command)
		}
		if result.Scan.PeakResidentBytes > result.Scan.Bounds.ResidentBound || result.Scan.Bounds.ResidentBound != importer.ResidentBound {
			t.Errorf("%s: the scan held %d bytes against a bound of %d", label, result.Scan.PeakResidentBytes, result.Scan.Bounds.ResidentBound)
		}
		command, err := corpus.DecodeBenchmark(mustReadFile(t, filepath.Join(reports, "command.json")))
		if err != nil {
			t.Fatal(err)
		}
		window, err := corpus.DecodeBenchmark(mustReadFile(t, result.Benchmark))
		if err != nil {
			t.Fatal(err)
		}
		if window.Measured.ElapsedMilliseconds != result.Scan.ElapsedMilliseconds {
			t.Errorf("%s: the benchmark measured %d ms where the window reported %d", label, window.Measured.ElapsedMilliseconds, result.Scan.ElapsedMilliseconds)
		}
		command.Measured.ElapsedMilliseconds, window.Measured.ElapsedMilliseconds = 0, 0
		if !reflect.DeepEqual(command, window) {
			t.Errorf("%s: the benchmarks differ:\ncommand %+v\nwindow  %+v", label, command, window)
		}
		before.unchanged(t, path)
	}
	// Without a benchmark destination nothing is written anywhere.
	listed := entriesOf(t, source)
	if result := app.ScanCorpus(desktop.CorpusScanRequest{File: filepath.Join(source, "mllp"), Plan: framed}); result.State != desktop.Completed || result.Benchmark != "" {
		t.Fatalf("a scan without a benchmark: %+v", result)
	}
	if after := entriesOf(t, source); !reflect.DeepEqual(after, listed) {
		t.Fatalf("a scan wrote beside the stream: %v", after)
	}
}

// A scan the command refuses the window refuses with the same sentence, and a
// refused scan writes no benchmark. The benchmark destination is checked
// before the stream is read, so an existing one is refused at once and never
// written over.
func TestCorpusScanRefusesWhatTheCommandRefuses(t *testing.T) {
	app := workspaceApp(t)
	source := t.TempDir()
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	if generated := app.GenerateCorpus(generationRequest(source, "mllp", "7", 20, framed)); generated.State != desktop.Completed {
		t.Fatal(generated)
	}
	stream := filepath.Join(source, "mllp")
	reports := t.TempDir()
	writeDocument(t, reports, "existing.json", "a benchmark somebody kept")
	utf8 := framed
	utf8.Encoding = importer.UTF8
	raw := corpusPlan(importer.RawFraming, "", importer.USASCII)
	// Raw framing declares the whole stream one message, so a stream past the
	// record bound is refused rather than buffered.
	oversized := filepath.Join(source, "oversized.hl7")
	if err := os.WriteFile(oversized, append([]byte("MSH|^~\\&|SYNTH|LAB\r"), bytes.Repeat([]byte("A"), importer.MaxRecordBytes)...), 0o600); err != nil {
		t.Fatal(err)
	}
	for label, scan := range map[string]struct {
		request desktop.CorpusScanRequest
		args    []string
	}{
		"raw framing over a framed stream": {desktop.CorpusScanRequest{File: stream, Plan: raw}, append([]string{"scan", stream}, planFlags(raw)...)},
		"a window past its bound":          {desktop.CorpusScanRequest{File: stream, Plan: framed, WindowLimit: 201}, append([]string{"scan", stream, "--window-limit", "201"}, planFlags(framed)...)},
		"a batch past its bound":           {desktop.CorpusScanRequest{File: stream, Plan: framed, BatchRecords: 257}, append([]string{"scan", stream, "--batch-records", "257"}, planFlags(framed)...)},
		"a stream that does not exist":     {desktop.CorpusScanRequest{File: filepath.Join(source, "absent"), Plan: framed}, append([]string{"scan", filepath.Join(source, "absent")}, planFlags(framed)...)},
		"a record past its bound":          {desktop.CorpusScanRequest{File: oversized, Plan: raw}, append([]string{"scan", oversized}, planFlags(raw)...)},
		"a folder":                         {desktop.CorpusScanRequest{File: source, Plan: framed}, append([]string{"scan", source}, planFlags(framed)...)},
		"an existing benchmark": {desktop.CorpusScanRequest{File: stream, Plan: utf8, ReportFolder: reports, ReportName: "existing.json"},
			append([]string{"scan", stream, "--report", filepath.Join(reports, "existing.json")}, planFlags(utf8)...)},
	} {
		stdout, stderr, cliErr := corpusCommand(t, scan.args...)
		result := app.ScanCorpus(scan.request)
		if cliErr == nil || stdout != "" || result.State != desktop.Failed || result.Scan != nil || result.Benchmark != "" {
			t.Fatalf("%s: the command answered %v %q and the window %+v", label, cliErr, stdout, result)
		}
		if want := "readmit: " + result.Reason + "\n"; stderr != want {
			t.Errorf("%s: the window refused with %q where the command printed %q", label, result.Reason, stderr)
		}
	}
	if got := string(mustReadFile(t, filepath.Join(reports, "existing.json"))); got != "a benchmark somebody kept" {
		t.Fatal("a refused scan wrote over an existing benchmark")
	}
	for label, request := range map[string]desktop.CorpusScanRequest{
		"a relative stream":            {File: "mllp", Plan: framed},
		"a plan that names members":    {File: stream, Plan: func() importer.Plan { p := framed; p.Members = []string{".mllp"}; return p }()},
		"a benchmark with no folder":   {File: stream, Plan: framed, ReportName: "new.json"},
		"a benchmark with no name":     {File: stream, Plan: framed, ReportFolder: reports},
		"a nested benchmark name":      {File: stream, Plan: framed, ReportFolder: reports, ReportName: "nested/new.json"},
		"a benchmark folder not there": {File: stream, Plan: framed, ReportFolder: filepath.Join(reports, "absent"), ReportName: "new.json"},
	} {
		if result := app.ScanCorpus(request); result.State != desktop.Failed || result.Scan != nil {
			t.Errorf("%s: %+v", label, result)
		}
	}
	if listed := entriesOf(t, reports); !reflect.DeepEqual(listed, []string{"existing.json"}) {
		t.Fatalf("a refused scan wrote a benchmark: %v", listed)
	}
}

// awaitCorpusProgress waits until the running corpus operation reports that
// it has begun reading or writing, which is how a test knows a cancellation
// arrives while it runs, and fails the test if the operation answers first.
func awaitCorpusProgress[R any](t *testing.T, app *desktop.App, begun func(desktop.CorpusProgress) bool, answered chan R) desktop.CorpusProgress {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if progress := app.CorpusProgress(); progress.State == desktop.Completed && progress.Progress != nil && begun(*progress.Progress) {
			return *progress.Progress
		}
		select {
		case result := <-answered:
			t.Fatalf("the operation answered before it reported progress: %+v", result)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the operation reported no progress")
		}
		time.Sleep(time.Millisecond)
	}
}

// Cancelling a scan while it streams is answered with the truthful partial
// state the command prints: the counts it reached, fewer than the stream
// holds, the case bounds not evaluated, and no benchmark, although one was
// asked for. Progress is readable while the scan holds the slot, names
// nothing, and ends with the scan. The stream is unchanged, and scanning it
// again completes.
func TestCancellingACorpusScanReportsTheCountsItReachedAndWritesNoBenchmark(t *testing.T) {
	app := workspaceApp(t)
	source := t.TempDir()
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	const messages = 60000
	if generated := app.GenerateCorpus(generationRequest(source, "mllp", "7", messages, framed)); generated.State != desktop.Completed {
		t.Fatal(generated)
	}
	stream := filepath.Join(source, "mllp")
	before := sourceStateOf(t, stream)
	reports := t.TempDir()
	request := desktop.CorpusScanRequest{File: stream, Plan: framed, BatchRecords: 1, ReportFolder: reports, ReportName: "benchmark.json"}

	answered := make(chan desktop.CorpusScanResult, 1)
	go func() { answered <- app.ScanCorpus(request) }()
	reached := awaitCorpusProgress(t, app, func(p desktop.CorpusProgress) bool { return p.Batches > 0 }, answered)
	if reached.Operation != "scan" || reached.Records < 1 || reached.Messages != 0 {
		t.Fatalf("scan progress: %+v", reached)
	}
	if busy := app.GenerateCorpus(generationRequest(source, "other", "7", 1, framed)); busy.State != desktop.Busy {
		t.Fatalf("a second corpus operation ran beside the scan: %+v", busy)
	}
	app.Cancel("another-operation")
	app.Cancel("corpus")
	cancelled := <-answered
	if cancelled.State != desktop.Cancelled || cancelled.Scan == nil || cancelled.Benchmark != "" {
		t.Fatalf("a cancelled scan answered %+v", cancelled)
	}
	view := cancelled.Scan
	if view.CaseBounds != desktop.CaseBoundsNotEvaluated || len(view.Exceeded) != 0 || view.Records < reached.Records || view.Records >= messages || view.Occurrences != view.Records {
		t.Fatalf("a cancelled scan reported %+v", view)
	}
	if listed := entriesOf(t, reports); len(listed) != 0 {
		t.Fatalf("a cancelled scan wrote a benchmark: %v", listed)
	}
	if progress := app.CorpusProgress(); progress.State != desktop.Empty {
		t.Fatalf("progress outlived the scan: %+v", progress)
	}
	before.unchanged(t, stream)
	again := app.ScanCorpus(request)
	if again.State != desktop.Completed || again.Scan.Records != messages || again.Benchmark == "" {
		t.Fatalf("scanning again after a cancellation: %+v", again)
	}
}

// Cancelling a generation while it writes removes the partial corpus it
// created and writes no manifest, so nothing is left that a manifest does not
// describe, and the same destination generates whole afterwards.
func TestCancellingACorpusGenerationLeavesNeitherCorpusNorManifest(t *testing.T) {
	app := workspaceApp(t)
	folder := t.TempDir()
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	request := generationRequest(folder, "corpus", "7", 200000, framed)

	answered := make(chan desktop.CorpusGenerateResult, 1)
	go func() { answered <- app.GenerateCorpus(request) }()
	reached := awaitCorpusProgress(t, app, func(p desktop.CorpusProgress) bool { return p.Messages > 0 }, answered)
	if reached.Operation != "generate" || reached.Bytes < 1 || reached.Records != 0 {
		t.Fatalf("generation progress: %+v", reached)
	}
	app.Cancel("corpus")
	cancelled := <-answered
	if cancelled.State != desktop.Cancelled || cancelled.Manifest != nil || cancelled.Corpus != "" ||
		cancelled.Reason != "the generation was cancelled; the partial corpus was removed and no manifest was written" {
		t.Fatalf("a cancelled generation answered %+v", cancelled)
	}
	if listed := entriesOf(t, folder); len(listed) != 0 {
		t.Fatalf("a cancelled generation left %v", listed)
	}
	request.Messages = 10
	if again := app.GenerateCorpus(request); again.State != desktop.Completed {
		t.Fatalf("generating again after a cancellation: %+v", again)
	}
}

// Generation is new authoring, admitted as `readmit corpus generate` is: a
// window with no activated license is refused and writes nothing. A scan
// writes no evidence and needs no activation, as the command does not.
func TestCorpusGenerationNeedsAuthorAdmissionAndAScanDoesNot(t *testing.T) {
	state := t.TempDir()
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
	folder := t.TempDir()
	framed := corpusPlan(importer.MLLPFraming, "", importer.USASCII)
	if denied := app.GenerateCorpus(generationRequest(folder, "corpus", "7", 10, framed)); denied.State != desktop.PermissionDenied {
		t.Fatalf("an unactivated window generated a corpus: %+v", denied)
	}
	if listed := entriesOf(t, folder); len(listed) != 0 {
		t.Fatalf("a refused generation wrote %v", listed)
	}
	stream := filepath.Join(folder, "stream.mllp")
	message := "MSH|^~\\&|SYNTH|LAB|RECV|LAB|20260101120000||SIU^S12|SCAN-1|T|2.5.1\rPID|1||R-1\r"
	writeDocument(t, folder, "stream.mllp", "\x0b"+message+"\x1c\r")
	if scanned := app.ScanCorpus(desktop.CorpusScanRequest{File: stream, Plan: framed}); scanned.State != desktop.Completed || scanned.Scan.Records != 1 {
		t.Fatalf("an unactivated window could not scan: %+v", scanned)
	}
}

// The corpus dialogs choose one folder or exactly one stream, and a person
// who dismisses one is answered cancelled with nothing chosen.
func TestChooseCorpusPathKinds(t *testing.T) {
	c := &chooser{files: []string{"/chosen/stream.mllp"}, folder: "/chosen/folder"}
	app := newApp(t, c)
	for kind, want := range map[string]string{"corpus-folder": "/chosen/folder", "scan-file": "/chosen/stream.mllp", "benchmark-folder": "/chosen/folder"} {
		if got := app.ChooseCorpusPath(kind); got.State != desktop.Completed || got.Path != want || got.Kind != kind {
			t.Fatalf("%s: %+v", kind, got)
		}
	}
	for _, title := range []string{"Choose the folder for the new corpus and its manifest", "Choose the stream to scan", "Choose the folder for the new benchmark"} {
		if !strings.Contains(strings.Join(c.titles, "|"), title) {
			t.Fatalf("no dialog titled %q: %v", title, c.titles)
		}
	}
	c.files = []string{"/chosen/a", "/chosen/b"}
	if got := app.ChooseCorpusPath("scan-file"); got.State != desktop.Failed || got.Reason != "choose exactly one file" {
		t.Fatalf("two streams: %+v", got)
	}
	c.files, c.folder = nil, ""
	for _, kind := range []string{"corpus-folder", "scan-file", "benchmark-folder"} {
		if got := app.ChooseCorpusPath(kind); got.State != desktop.Cancelled || got.Path != "" {
			t.Fatalf("dismissed %s: %+v", kind, got)
		}
	}
	if got := app.ChooseCorpusPath("elsewhere"); got.State != desktop.Failed {
		t.Fatalf("unknown kind: %+v", got)
	}
}
