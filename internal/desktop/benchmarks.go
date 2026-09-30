package desktop

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/corpus"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
)

// Benchmarks are the measured scans run over the open project and the
// synthetic inputs generated to run them on. Both live in the project's own
// area, so they are listed, backed up and removed with the project: a
// generated input is a readmit-corpus/v1 corpus and its manifest, and a
// completed benchmark is a readmit-benchmark/v1 document, each in a folder
// the application names. The project document readmit-benchmarks/v1 records
// what each one is — the name a person gave an input, when it was written,
// and what each benchmark measured — because the files record none of that,
// and a date is never read from a file.

// BenchmarksSchema is the versioned contract of the project document that
// records the generated inputs and the benchmark results. It holds no
// evidence: a generated input is synthetic, and a result holds counts.
const BenchmarksSchema = "readmit-benchmarks/v1"

// MaxBenchmarkRecords bounds the inputs, and separately the results, one
// project records; the oldest record is forgotten first. Forgetting a record
// deletes nothing.
const MaxBenchmarkRecords = 1024

const maxBenchmarksBytes = 8 << 20

// MaxBenchmarkWindow is the most scanned records one benchmark renders.
const MaxBenchmarkWindow = importer.MaxScanWindowRows

// The folders and file names benchmarks are written into, inside the
// project's own area.
const (
	benchmarksFolder   = "benchmarks"
	benchmarksDocName  = "benchmarks.json"
	inputsFolder       = "inputs"
	resultsFolder      = "results"
	inputCorpusName    = "corpus"
	inputManifestName  = "manifest.json"
	resultDocumentName = "benchmark.json"
)

var benchmarksFile = artifactdir.Document{MaxBytes: maxBenchmarksBytes}

// The completion of one benchmark: a scan that read the whole input, or one
// that was stopped and carries only the counts it reached.
type BenchmarkCompletion string

const (
	BenchmarkComplete   BenchmarkCompletion = "complete"
	BenchmarkIncomplete BenchmarkCompletion = "incomplete"
)

// The inputs a benchmark scans: an input this application generated, or one
// file a person chose.
type BenchmarkInputKind string

const (
	GeneratedInput BenchmarkInputKind = "generated"
	ChosenFile     BenchmarkInputKind = "file"
)

// BenchmarkDefaults is every choice Generate input and Start benchmark offer
// and the value each starts from, as the generator and the scanner declare
// them, so the window keeps no copy. Plan is the default declaration of a new
// input: MLLP framing, CR, UTF-8 and an unknown direction. BaseTime is this
// moment, to a whole second, for a draft to capture once. The batch limits
// are the scanner's documented defaults and bounds, and the window starts at
// counts only.
type BenchmarkDefaults struct {
	GeneratorVersion string              `json:"generator_version"`
	ProfileVersion   string              `json:"profile_version"`
	MaxMessages      int                 `json:"max_messages"`
	Seed             string              `json:"seed"`
	BaseTime         string              `json:"base_time"`
	Framings         []importer.Framing  `json:"framings"`
	BatchBoundaries  []importer.Boundary `json:"batch_boundaries"`
	Terminators      []hl7.Terminator    `json:"terminators"`
	Encodings        []importer.Encoding `json:"encodings"`
	Directions       []bundle.Direction  `json:"directions"`
	Plan             importer.Plan       `json:"plan"`
	BatchRecords     int                 `json:"batch_records"`
	MaxBatchRecords  int                 `json:"max_batch_records"`
	BatchBytes       int                 `json:"batch_bytes"`
	MaxBatchBytes    int                 `json:"max_batch_bytes"`
	WindowLimit      int                 `json:"window_limit"`
	MaxWindowLimit   int                 `json:"max_window_limit"`
}

// BenchmarkDefaultsResult is always Completed.
type BenchmarkDefaultsResult struct {
	State    State             `json:"state"`
	Reason   string            `json:"reason,omitzero"`
	Defaults BenchmarkDefaults `json:"defaults"`
}

// BenchmarkInput is one input this application generated. Manifest is the
// readmit-corpus/v1 manifest it wrote, which retains the seed, base time and
// versions that reproduce it. Availability is whether the corpus is still
// the size its manifest records.
type BenchmarkInput struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	CreatedAt    string             `json:"created_at"`
	Origin       string             `json:"origin"`
	Manifest     CorpusManifestView `json:"manifest"`
	Availability Availability       `json:"availability"`
}

// BenchmarkSource is what one benchmark scanned: a generated input, by its
// identity and name, or a chosen file, by its name alone.
type BenchmarkSource struct {
	Kind BenchmarkInputKind `json:"kind"`
	ID   string             `json:"id,omitzero"`
	Name string             `json:"name"`
}

// BenchmarkRecord is one benchmark as it was measured. Records, Bytes and
// Batches are what the scan read. ElapsedMilliseconds, PeakScanBufferBytes,
// SHA256 and CaseBounds belong to a complete scan and are absent from an
// incomplete one, which read only part of its input: nothing is claimed
// about the rest of it. PeakScanBufferBytes is the peak capacity of the
// scanner's own buffers, never the process's resident memory.
type BenchmarkRecord struct {
	ID                  string              `json:"id"`
	CreatedAt           string              `json:"created_at"`
	Input               BenchmarkSource     `json:"input"`
	Completion          BenchmarkCompletion `json:"completion"`
	Plan                importer.Plan       `json:"plan"`
	Bounds              corpus.Bounds       `json:"bounds"`
	Records             int64               `json:"records"`
	Bytes               int64               `json:"bytes"`
	Batches             int64               `json:"batches"`
	ElapsedMilliseconds *int64              `json:"elapsed_milliseconds,omitzero"`
	PeakScanBufferBytes *int                `json:"peak_scan_buffer_bytes,omitzero"`
	SHA256              string              `json:"sha256,omitzero"`
	CaseBounds          string              `json:"case_bounds,omitzero"`
	Exceeded            []string            `json:"exceeded,omitzero"`
	Hardware            corpus.Hardware     `json:"hardware"`
}

type inputRecord struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	CreatedAt string             `json:"created_at"`
	Manifest  CorpusManifestView `json:"manifest"`
}

type benchmarksDocument struct {
	Schema  string            `json:"schema"`
	Inputs  []inputRecord     `json:"inputs"`
	Results []BenchmarkRecord `json:"results"`
}

// GenerateInputRequest declares one input of the open project: its name, the
// four generator inputs, the message count and the format it is written in.
// The seed is the text of the seed, as the command reads it.
type GenerateInputRequest struct {
	Context          RequestContext `json:"context"`
	Name             string         `json:"name"`
	Seed             string         `json:"seed"`
	BaseTime         string         `json:"base_time"`
	GeneratorVersion string         `json:"generator_version"`
	ProfileVersion   string         `json:"profile_version"`
	Messages         int            `json:"messages"`
	Plan             importer.Plan  `json:"plan"`
}

// GenerateInputResult carries one state and, when Completed, the input that
// was generated and recorded.
type GenerateInputResult struct {
	State   State           `json:"state"`
	Reason  string          `json:"reason,omitzero"`
	Context RequestContext  `json:"context"`
	Input   *BenchmarkInput `json:"input,omitzero"`
}

func (r *GenerateInputResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// BenchmarkInputsResult lists the project's generated inputs, newest first.
// It is Empty when there are none.
type BenchmarkInputsResult struct {
	State   State            `json:"state"`
	Reason  string           `json:"reason,omitzero"`
	Context RequestContext   `json:"context"`
	Inputs  []BenchmarkInput `json:"inputs"`
}

// StartBenchmarkRequest names what to scan — a generated input of the open
// project by its identity, or one chosen file by its full path under a
// declared format — and the scan's limits. A generated input is scanned
// under the format its manifest records. Zero batch limits are the
// documented defaults; the window renders from WindowOffset at most
// WindowLimit records, 0 to 200, and 0 is counts only.
type StartBenchmarkRequest struct {
	Context      RequestContext `json:"context"`
	InputID      string         `json:"input_id,omitzero"`
	File         string         `json:"file,omitzero"`
	Plan         *importer.Plan `json:"plan,omitzero"`
	BatchRecords int            `json:"batch_records,omitzero"`
	BatchBytes   int            `json:"batch_bytes,omitzero"`
	WindowOffset int            `json:"window_offset,omitzero"`
	WindowLimit  int            `json:"window_limit,omitzero"`
}

// StartBenchmarkResult is Completed with a complete result, Cancelled with
// the incomplete result a stopped scan reached, or a refusal with neither.
// Scan is everything the scan reported, the rendered window included.
type StartBenchmarkResult struct {
	State   State            `json:"state"`
	Reason  string           `json:"reason,omitzero"`
	Context RequestContext   `json:"context"`
	Result  *BenchmarkRecord `json:"result,omitzero"`
	Scan    *CorpusScanView  `json:"scan,omitzero"`
}

func (r *StartBenchmarkResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// BenchmarksResult lists the project's benchmark results, newest first, then
// by identity. It is Empty when there are none.
type BenchmarksResult struct {
	State   State             `json:"state"`
	Reason  string            `json:"reason,omitzero"`
	Context RequestContext    `json:"context"`
	Results []BenchmarkRecord `json:"results"`
}

// BenchmarkRequest names one recorded result of the open project.
type BenchmarkRequest struct {
	Context RequestContext `json:"context"`
	ID      string         `json:"id"`
}

// BenchmarkResult is one result. Availability is whether a complete result's
// readmit-benchmark/v1 document is still where it was written; an incomplete
// result has none and is available.
type BenchmarkResult struct {
	State        State            `json:"state"`
	Reason       string           `json:"reason,omitzero"`
	Context      RequestContext   `json:"context"`
	Result       *BenchmarkRecord `json:"result,omitzero"`
	Availability Availability     `json:"availability,omitzero"`
}

// unreadableBenchmarks refuses a benchmarks document this release cannot
// read. It is left exactly as written.
const unreadableBenchmarks = "the project's benchmarks cannot be read; they are left exactly as written"

func benchmarksPath(root string, parts ...string) string {
	return filepath.Join(append([]string{root, catalog.Folder, benchmarksFolder}, parts...)...)
}

// readBenchmarks reads the project's benchmarks document; a project that has
// none has recorded nothing.
func readBenchmarks(root string) (benchmarksDocument, error) {
	data, err := benchmarksFile.Read(benchmarksPath(root, benchmarksDocName))
	if errors.Is(err, fs.ErrNotExist) {
		return benchmarksDocument{Schema: BenchmarksSchema, Inputs: []inputRecord{}, Results: []BenchmarkRecord{}}, nil
	}
	if err != nil {
		return benchmarksDocument{}, err
	}
	var document benchmarksDocument
	if err := json.Unmarshal(data, &document, json.RejectUnknownMembers(true)); err != nil || document.Schema != BenchmarksSchema ||
		len(document.Inputs) > MaxBenchmarkRecords || len(document.Results) > MaxBenchmarkRecords {
		return benchmarksDocument{}, errNotADocument
	}
	for _, input := range document.Inputs {
		if !catalog.ValidID(input.ID) || !catalog.ValidName(input.Name) || !stampText(input.CreatedAt) || input.Manifest.Plan.Validate() != nil {
			return benchmarksDocument{}, errNotADocument
		}
	}
	for _, result := range document.Results {
		if !validResult(result) {
			return benchmarksDocument{}, errNotADocument
		}
	}
	if document.Inputs == nil {
		document.Inputs = []inputRecord{}
	}
	if document.Results == nil {
		document.Results = []BenchmarkRecord{}
	}
	return document, nil
}

func stampText(value string) bool { _, err := time.Parse(time.RFC3339, value); return err == nil }

func validResult(result BenchmarkRecord) bool {
	source := result.Input
	switch {
	case !catalog.ValidID(result.ID), !stampText(result.CreatedAt), result.Plan.Validate() != nil,
		!catalog.ValidName(source.Name), result.Records < 0 || result.Bytes < 0 || result.Batches < 0:
		return false
	case source.Kind == GeneratedInput && !catalog.ValidID(source.ID), source.Kind == ChosenFile && source.ID != "",
		source.Kind != GeneratedInput && source.Kind != ChosenFile:
		return false
	}
	switch result.Completion {
	case BenchmarkComplete:
		return result.ElapsedMilliseconds != nil && result.PeakScanBufferBytes != nil && digestText(result.SHA256) &&
			(result.CaseBounds == CaseBoundsWithin || result.CaseBounds == CaseBoundsExceeded)
	case BenchmarkIncomplete:
		return result.ElapsedMilliseconds == nil && result.PeakScanBufferBytes == nil && result.SHA256 == "" &&
			result.CaseBounds == "" && len(result.Exceeded) == 0
	}
	return false
}

// changeBenchmarks reads the project's benchmarks document, applies change,
// and replaces it whole. Only the two named benchmark operations call it, and
// they hold the operation slot, so no two changes interleave. A document this
// release cannot read is left as it is and reported.
func changeBenchmarks(root string, change func(*benchmarksDocument)) error {
	document, err := readBenchmarks(root)
	if err != nil {
		return err
	}
	change(&document)
	if len(document.Inputs) > MaxBenchmarkRecords {
		document.Inputs = document.Inputs[:MaxBenchmarkRecords]
	}
	if len(document.Results) > MaxBenchmarkRecords {
		document.Results = document.Results[:MaxBenchmarkRecords]
	}
	data, err := encodeMember(document)
	if err != nil {
		return err
	}
	if _, err := managedFolder(root, catalog.Folder, benchmarksFolder); err != nil {
		return err
	}
	return benchmarksFile.Replace(benchmarksPath(root, benchmarksDocName), data)
}

// BenchmarkDefaults reports every choice Generate input and Start benchmark
// offer and the value each starts from. It reads nothing and does not claim
// the operation slot.
func (a *App) BenchmarkDefaults() BenchmarkDefaultsResult {
	return BenchmarkDefaultsResult{State: Completed, Defaults: BenchmarkDefaults{
		GeneratorVersion: corpus.GeneratorVersion,
		ProfileVersion:   corpus.ProfileVersion,
		MaxMessages:      corpus.MaxMessages,
		Seed:             "0",
		BaseTime:         a.now().UTC().Truncate(time.Second).Format(time.RFC3339),
		Framings:         []importer.Framing{importer.MLLPFraming, importer.BatchFraming},
		BatchBoundaries:  []importer.Boundary{importer.SegmentStart},
		Terminators:      []hl7.Terminator{hl7.CR},
		Encodings:        []importer.Encoding{importer.UTF8, importer.USASCII},
		Directions:       importer.Vocabulary().Directions,
		Plan: importer.Plan{Schema: importer.PlanSchema, Framing: importer.MLLPFraming, Terminator: hl7.CR,
			Encoding: importer.UTF8, Direction: bundle.Unknown, Members: []string{}},
		BatchRecords:    importer.MaxBatchRecords,
		MaxBatchRecords: importer.MaxBatchRecords,
		BatchBytes:      importer.MaxBatchBytes,
		MaxBatchBytes:   importer.MaxBatchBytes,
		WindowLimit:     0,
		MaxWindowLimit:  MaxBenchmarkWindow,
	}}
}

// GenerateInput generates one named synthetic input into the open project's
// own area through corpus.Write, the operation `readmit corpus generate`
// calls, so the manifest it writes retains the seed, base time and versions
// that reproduce it byte for byte, and records it. Generation is new
// authoring, admitted as the command is. It imports no case, contacts
// nothing and measures nothing. It is interruptible: a cancelled or failed
// generation leaves no corpus, no manifest and no record.
func (a *App) GenerateInput(request GenerateInputRequest) GenerateInputResult {
	return runNamed[GenerateInputResult, *GenerateInputResult](a, profiles["GenerateInput"], func(ctx context.Context) GenerateInputResult {
		result := GenerateInputResult{Context: request.Context}
		name := strings.TrimSpace(request.Name)
		if !catalog.ValidName(name) {
			result.refuse(Failed, nameRule)
			return result
		}
		inputs, declined := generatorInputs(request.Seed, request.BaseTime, request.GeneratorVersion, request.ProfileVersion, request.Messages, request.Plan)
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		if _, err := readBenchmarks(root); err != nil {
			result.refuse(Failed, unreadableBenchmarks)
			return result
		}
		id, err := catalog.NewID()
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		parent, err := managedFolder(root, catalog.Folder, benchmarksFolder, inputsFolder)
		if err != nil {
			result.refuse(refusalState(err), "the folder generated inputs are kept in could not be created")
			return result
		}
		folder := filepath.Join(parent, id)
		if err := os.Mkdir(folder, 0o700); err != nil {
			result.refuse(refusalState(err), "the folder for the new input could not be created")
			return result
		}
		a.startCorpusProgress("generate")
		defer a.endCorpusProgress()
		manifest, err := corpus.Write(ctx, filepath.Join(folder, inputCorpusName), filepath.Join(folder, inputManifestName), inputs, func(p corpus.Progress) {
			a.reportCorpusProgress(func(progress *CorpusProgress) { progress.Messages, progress.Bytes = p.Messages, p.Bytes })
		})
		if err != nil {
			// The partial corpus is removed by corpus.Write and no manifest
			// was written, so the folder is empty again.
			os.Remove(folder)
			if errors.Is(err, corpus.ErrCancelled) {
				result.refuse(Cancelled, "the generation was cancelled; nothing was kept")
				return result
			}
			result.refuse(refusalState(err), err.Error())
			return result
		}
		record := inputRecord{ID: id, Name: name, CreatedAt: catalog.Stamp(a.now()), Manifest: manifestView(manifest)}
		if err := changeBenchmarks(root, func(document *benchmarksDocument) {
			document.Inputs = append([]inputRecord{record}, document.Inputs...)
		}); err != nil {
			os.RemoveAll(folder)
			result.refuse(Failed, "the generated input could not be recorded; nothing was kept")
			return result
		}
		input := inputView(root, record)
		result.State, result.Input = Completed, &input
		return result
	})
}

// generatorInputs reads a generation's declarations the way the command
// reads them.
func generatorInputs(seedText, baseTime, generatorVersion, profileVersion string, messages int, plan importer.Plan) (corpus.Inputs, refusal) {
	seed, err := strconv.ParseUint(seedText, 0, 64)
	if err != nil {
		return corpus.Inputs{}, refusal{Failed, "the seed must be a whole number from 0 to " + strconv.FormatUint(^uint64(0), 10)}
	}
	base, err := operation.DeclaredBaseTime(baseTime)
	if err != nil {
		return corpus.Inputs{}, refusal{Failed, err.Error()}
	}
	inputs := corpus.Inputs{
		Generator: bundle.GeneratorInputs{Seed: seed, BaseTime: base, GeneratorVersion: generatorVersion, ProfileVersion: profileVersion},
		Messages:  messages,
		Plan:      plan,
	}
	if err := inputs.Validate(); err != nil {
		return corpus.Inputs{}, refusal{Failed, err.Error()}
	}
	return inputs, refusal{}
}

func manifestView(manifest corpus.Manifest) CorpusManifestView {
	return CorpusManifestView{
		Schema:           manifest.Schema,
		Seed:             strconv.FormatUint(manifest.Inputs.Generator.Seed, 10),
		BaseTime:         manifest.Inputs.Generator.BaseTime.UTC().Format(time.RFC3339),
		GeneratorVersion: manifest.Inputs.Generator.GeneratorVersion,
		ProfileVersion:   manifest.Inputs.Generator.ProfileVersion,
		Messages:         manifest.Inputs.Messages,
		Plan:             manifest.Inputs.Plan,
		Bytes:            manifest.Bytes,
		SHA256:           manifest.SHA256,
	}
}

// inputView is one recorded input with whether its corpus is still there at
// the size its manifest records.
func inputView(root string, record inputRecord) BenchmarkInput {
	availability := ItemAvailable
	info, err := os.Lstat(benchmarksPath(root, inputsFolder, record.ID, inputCorpusName))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		availability = ItemMissing
	case err != nil, !info.Mode().IsRegular(), info.Size() != record.Manifest.Bytes:
		availability = ItemUnreadable
	}
	return BenchmarkInput{ID: record.ID, Name: record.Name, CreatedAt: record.CreatedAt, Origin: SyntheticOrigin,
		Manifest: record.Manifest, Availability: availability}
}

// projectBenchmarks reads the benchmarks document of the project a request
// names. It is a small local read, taken beside any operation.
func (a *App) projectBenchmarks(request RequestContext) (string, benchmarksDocument, refusal) {
	root, declined := a.projectRoot(context.Background(), request)
	if root == "" {
		return "", benchmarksDocument{}, declined
	}
	document, err := readBenchmarks(root)
	if err != nil {
		return "", benchmarksDocument{}, refusal{Failed, unreadableBenchmarks}
	}
	return root, document, refusal{}
}

// ListBenchmarkInputs lists the open project's generated inputs, newest
// first. It reads one small document and does not claim the operation slot.
func (a *App) ListBenchmarkInputs(request RequestContext) BenchmarkInputsResult {
	result := BenchmarkInputsResult{Context: request, Inputs: []BenchmarkInput{}}
	root, document, declined := a.projectBenchmarks(request)
	if root == "" {
		result.State, result.Reason = declined.state, declined.reason
		return result
	}
	for _, record := range document.Inputs {
		result.Inputs = append(result.Inputs, inputView(root, record))
	}
	slices.SortStableFunc(result.Inputs, func(x, y BenchmarkInput) int {
		return cmp.Or(strings.Compare(y.CreatedAt, x.CreatedAt), strings.Compare(x.ID, y.ID))
	})
	if len(result.Inputs) == 0 {
		result.State, result.Reason = Empty, "no inputs have been generated"
		return result
	}
	result.State = Completed
	return result
}

// StartBenchmark scans one input through the shared operation `readmit
// corpus scan` runs and records the result in the open project. A complete
// scan also writes an unchanged readmit-benchmark/v1 document into the
// project's own area. A stopped scan is recorded as incomplete, with the
// records and bytes it read and nothing it did not measure. It writes no
// evidence and needs no activation, as the command does not.
func (a *App) StartBenchmark(request StartBenchmarkRequest) StartBenchmarkResult {
	return runNamed[StartBenchmarkResult, *StartBenchmarkResult](a, profiles["StartBenchmark"], func(ctx context.Context) StartBenchmarkResult {
		result := StartBenchmarkResult{Context: request.Context}
		if request.WindowOffset < 0 {
			result.refuse(Failed, "the rendered records begin at or after the first scanned record")
			return result
		}
		if request.WindowLimit < 0 || request.WindowLimit > MaxBenchmarkWindow {
			result.refuse(Failed, "a benchmark renders between 0 and "+strconv.Itoa(MaxBenchmarkWindow)+" scanned records")
			return result
		}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		document, err := readBenchmarks(root)
		if err != nil {
			result.refuse(Failed, unreadableBenchmarks)
			return result
		}
		file, plan, source, expect, declined := benchmarkSource(root, document, request)
		if declined.state != "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		a.startCorpusProgress("scan")
		defer a.endCorpusProgress()
		scan, err := operation.ScanCorpus(ctx, file, importer.ScanOptions{
			Plan:         plan,
			Window:       importer.Window{Offset: request.WindowOffset, Limit: request.WindowLimit},
			BatchRecords: request.BatchRecords,
			BatchBytes:   request.BatchBytes,
			Report: func(p importer.Progress) {
				a.reportCorpusProgress(func(progress *CorpusProgress) {
					progress.Bytes, progress.Records, progress.Occurrences, progress.Batches = p.Bytes, p.Records, p.Occurrences, p.Batches
				})
			},
		}, "")
		if err != nil {
			result.refuse(refusalState(err), err.Error())
			return result
		}
		view := scanView(scan)
		result.Scan = view
		if !scan.Cancelled && expect != "" && scan.Result.SHA256 != expect {
			result.refuse(Failed, "the generated input changed after it was generated; no result was recorded")
			return result
		}
		id, err := catalog.NewID()
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		record := benchmarkRecord(id, catalog.Stamp(a.now()), source, scan, view)
		if !scan.Cancelled {
			parent, err := managedFolder(root, catalog.Folder, benchmarksFolder, resultsFolder)
			if err != nil {
				result.refuse(refusalState(err), "the folder benchmarks are kept in could not be created; no result was recorded")
				return result
			}
			folder := filepath.Join(parent, id)
			if err := os.Mkdir(folder, 0o700); err != nil {
				result.refuse(refusalState(err), "the folder for the benchmark could not be created; no result was recorded")
				return result
			}
			if err := operation.WriteBenchmark(filepath.Join(folder, resultDocumentName), scan); err != nil {
				os.RemoveAll(folder)
				result.refuse(refusalState(err), err.Error())
				return result
			}
		}
		if err := changeBenchmarks(root, func(document *benchmarksDocument) {
			document.Results = append([]BenchmarkRecord{record}, document.Results...)
		}); err != nil {
			result.refuse(Failed, "the benchmark ran but its result could not be recorded")
			return result
		}
		result.Result = &record
		if scan.Cancelled {
			result.refuse(Cancelled, "the benchmark was stopped; it is recorded as incomplete, with the records and bytes it read")
			return result
		}
		result.State = Completed
		return result
	})
}

// benchmarkSource resolves what a benchmark scans: the corpus of a recorded
// generated input under its manifest's format, with the digest the scan must
// reproduce, or one chosen file under the declared format.
func benchmarkSource(root string, document benchmarksDocument, request StartBenchmarkRequest) (string, importer.Plan, BenchmarkSource, string, refusal) {
	switch {
	case (request.InputID == "") == (request.File == ""):
		return "", importer.Plan{}, BenchmarkSource{}, "", refusal{Failed, "a benchmark scans one generated input or one chosen file"}
	case request.InputID != "":
		index := slices.IndexFunc(document.Inputs, func(input inputRecord) bool { return input.ID == request.InputID })
		if index < 0 {
			return "", importer.Plan{}, BenchmarkSource{}, "", refusal{Failed, "that generated input is not recorded"}
		}
		input := document.Inputs[index]
		if request.Plan != nil && !reflect.DeepEqual(*request.Plan, input.Manifest.Plan) {
			return "", importer.Plan{}, BenchmarkSource{}, "", refusal{Failed, "a generated input is scanned under the format its manifest records"}
		}
		return benchmarksPath(root, inputsFolder, input.ID, inputCorpusName), input.Manifest.Plan,
			BenchmarkSource{Kind: GeneratedInput, ID: input.ID, Name: input.Name}, input.Manifest.SHA256, refusal{}
	}
	if !filepath.IsAbs(request.File) {
		return "", importer.Plan{}, BenchmarkSource{}, "", refusal{Failed, "choose the file with the file dialog; a benchmark reads one file named by its full path"}
	}
	if request.Plan == nil {
		return "", importer.Plan{}, BenchmarkSource{}, "", refusal{Failed, "declare the format the chosen file is read under"}
	}
	if err := request.Plan.Validate(); err != nil {
		return "", importer.Plan{}, BenchmarkSource{}, "", refusal{Failed, err.Error()}
	}
	name := filepath.Base(request.File)
	if !catalog.ValidName(name) {
		name = "Chosen file"
	}
	return request.File, *request.Plan, BenchmarkSource{Kind: ChosenFile, Name: name}, "", refusal{}
}

// benchmarkRecord is what one scan measured. A stopped scan keeps the counts
// it reached and claims no duration, peak, digest or case-bounds verdict.
func benchmarkRecord(id, created string, source BenchmarkSource, scan operation.CorpusScan, view *CorpusScanView) BenchmarkRecord {
	record := BenchmarkRecord{
		ID: id, CreatedAt: created, Input: source, Completion: BenchmarkIncomplete, Plan: scan.Plan, Bounds: scan.Bounds,
		Records: scan.Result.Records, Bytes: scan.Result.Bytes, Batches: scan.Result.Batches, Hardware: corpus.ThisMachine(),
	}
	if scan.Cancelled {
		return record
	}
	elapsed, peak := scan.Elapsed.Milliseconds(), scan.Result.PeakResidentBytes
	record.Completion, record.ElapsedMilliseconds, record.PeakScanBufferBytes = BenchmarkComplete, &elapsed, &peak
	record.SHA256, record.CaseBounds, record.Exceeded = scan.Result.SHA256, view.CaseBounds, view.Exceeded
	return record
}

// ListBenchmarks lists the open project's benchmark results, newest first,
// then by identity. It reads one small document and does not claim the
// operation slot.
func (a *App) ListBenchmarks(request RequestContext) BenchmarksResult {
	result := BenchmarksResult{Context: request, Results: []BenchmarkRecord{}}
	root, document, declined := a.projectBenchmarks(request)
	if root == "" {
		result.State, result.Reason = declined.state, declined.reason
		return result
	}
	result.Results = slices.Clone(document.Results)
	slices.SortStableFunc(result.Results, func(x, y BenchmarkRecord) int {
		return cmp.Or(strings.Compare(y.CreatedAt, x.CreatedAt), strings.Compare(x.ID, y.ID))
	})
	if len(result.Results) == 0 {
		result.State, result.Reason = Empty, "no benchmarks"
		return result
	}
	result.State = Completed
	return result
}

// OpenBenchmark reads one recorded result of the open project and whether a
// complete result's benchmark document is still where it was written and
// still one this release reads. It does not claim the operation slot.
func (a *App) OpenBenchmark(request BenchmarkRequest) BenchmarkResult {
	result := BenchmarkResult{Context: request.Context}
	root, document, declined := a.projectBenchmarks(request.Context)
	if root == "" {
		result.State, result.Reason = declined.state, declined.reason
		return result
	}
	index := slices.IndexFunc(document.Results, func(record BenchmarkRecord) bool { return record.ID == request.ID })
	if index < 0 {
		result.State, result.Reason = Failed, "that benchmark is not recorded"
		return result
	}
	record := document.Results[index]
	availability := ItemAvailable
	if record.Completion == BenchmarkComplete {
		data, err := benchmarkDocumentFile.Read(benchmarksPath(root, resultsFolder, record.ID, resultDocumentName))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			availability = ItemMissing
		case err != nil:
			availability = ItemUnreadable
		default:
			if written, err := corpus.DecodeBenchmark(data); err != nil || written.Corpus.SHA256 != record.SHA256 {
				availability = ItemUnreadable
			}
		}
	}
	result.State, result.Result, result.Availability = Completed, &record, availability
	return result
}

var benchmarkDocumentFile = artifactdir.Document{MaxBytes: corpus.MaxManifestBytes}
