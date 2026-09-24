package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/corpus"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/operation"
)

// corpusOperation names generation and scanning while either holds the slot,
// so the performance-corpus screen's cancel control stops exactly the one it
// started. Both are local work that reaches no destination.
const corpusOperation = "corpus"

// The case-bounds verdicts of a scan, as `readmit corpus scan` reports them:
// an import of the same bytes would fit, would be past the named bounds, or
// was never asked about because the scan was cancelled and knows nothing about
// the rest of the stream.
const (
	CaseBoundsWithin       = "within"
	CaseBoundsExceeded     = "exceeded"
	CaseBoundsNotEvaluated = "not-evaluated"
)

// CorpusPathResult is one native dialog answer for the performance-corpus
// screen: the folder a new corpus and its manifest are written into, the
// stream to scan, or the folder a new benchmark is written into.
type CorpusPathResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Kind   string `json:"kind,omitzero"`
	Path   string `json:"path,omitzero"`
}

func (r *CorpusPathResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CorpusGenerateRequest declares one corpus the way `readmit corpus generate`
// does: the four generator inputs, the message count and the plan it is framed
// under, and two new entries of a natively chosen folder. The seed is the
// text of the seed, read as the command's flag reads it, because a seed up to
// 2^64-1 does not survive a JavaScript number.
type CorpusGenerateRequest struct {
	Seed             string        `json:"seed"`
	BaseTime         string        `json:"base_time"`
	GeneratorVersion string        `json:"generator_version"`
	ProfileVersion   string        `json:"profile_version"`
	Messages         int           `json:"messages"`
	Plan             importer.Plan `json:"plan"`
	Folder           string        `json:"folder"`
	CorpusName       string        `json:"corpus_name"`
	ManifestName     string        `json:"manifest_name"`
}

// CorpusManifestView is the readmit-corpus/v1 manifest a generation wrote,
// member for member, with the seed as a decimal string.
type CorpusManifestView struct {
	Schema           string        `json:"schema"`
	Seed             string        `json:"seed"`
	BaseTime         string        `json:"base_time"`
	GeneratorVersion string        `json:"generator_version"`
	ProfileVersion   string        `json:"profile_version"`
	Messages         int           `json:"messages"`
	Plan             importer.Plan `json:"plan"`
	Bytes            int64         `json:"bytes"`
	SHA256           string        `json:"sha256"`
}

// CorpusGenerateResult carries one state. Corpus and ManifestPath are the two
// files a completed generation wrote; a cancelled or failed one leaves
// neither, because the partial corpus is removed and the manifest is written
// last.
type CorpusGenerateResult struct {
	State        State               `json:"state"`
	Reason       string              `json:"reason,omitzero"`
	Corpus       string              `json:"corpus,omitzero"`
	ManifestPath string              `json:"manifest_path,omitzero"`
	Manifest     *CorpusManifestView `json:"manifest,omitzero"`
}

func (r *CorpusGenerateResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CorpusScanRequest declares one scan the way `readmit corpus scan` does: the
// stream, the plan it divides under, the optional batch bounds and rendered
// window, and optionally one new entry of a natively chosen folder for the
// benchmark.
type CorpusScanRequest struct {
	File         string        `json:"file"`
	Plan         importer.Plan `json:"plan"`
	BatchRecords int           `json:"batch_records,omitzero"`
	BatchBytes   int           `json:"batch_bytes,omitzero"`
	WindowOffset int           `json:"window_offset,omitzero"`
	WindowLimit  int           `json:"window_limit,omitzero"`
	ReportFolder string        `json:"report_folder,omitzero"`
	ReportName   string        `json:"report_name,omitzero"`
}

// CorpusScanView is what one scan counted and every line `readmit corpus
// scan` prints, as data: counts, the declared bounds, the case-bounds verdict,
// the rendered window of rows and the proposed-targets note. It carries no
// byte of the stream and no decoded value.
type CorpusScanView struct {
	Plan                importer.Plan  `json:"plan"`
	Bytes               int64          `json:"bytes"`
	SHA256              string         `json:"sha256,omitzero"`
	Records             int64          `json:"records"`
	Occurrences         int64          `json:"occurrences"`
	Decoded             int64          `json:"decoded"`
	Undecodable         int64          `json:"undecodable"`
	Batches             int64          `json:"batches"`
	PeakResidentBytes   int            `json:"peak_resident_bytes"`
	Bounds              corpus.Bounds  `json:"bounds"`
	CaseBounds          string         `json:"case_bounds"`
	Exceeded            []string       `json:"exceeded,omitzero"`
	WindowOffset        int            `json:"window_offset"`
	WindowLimit         int            `json:"window_limit"`
	Rows                []importer.Row `json:"rows"`
	ElapsedMilliseconds int64          `json:"elapsed_milliseconds"`
	Targets             string         `json:"targets"`
}

// CorpusScanResult carries one state. A cancelled scan carries the counts it
// reached with the case bounds not evaluated and no benchmark; a benchmark
// that could not be written after a completed scan fails with the scan still
// reported, as the command prints its summary before it writes one.
type CorpusScanResult struct {
	State     State           `json:"state"`
	Reason    string          `json:"reason,omitzero"`
	Scan      *CorpusScanView `json:"scan,omitzero"`
	Benchmark string          `json:"benchmark,omitzero"`
}

func (r *CorpusScanResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CorpusProgress is what a running generation or scan has reached: counts
// only, the same counts the command writes to its diagnostic stream with
// --progress, so it names no file and carries no byte of the stream.
type CorpusProgress struct {
	Operation   string `json:"operation"`
	Messages    int64  `json:"messages"`
	Bytes       int64  `json:"bytes"`
	Records     int64  `json:"records"`
	Occurrences int64  `json:"occurrences"`
	Batches     int64  `json:"batches"`
}

// CorpusProgressResult is Empty when no generation or scan is running, and
// Completed with the counts one has reached when one is.
type CorpusProgressResult struct {
	State    State           `json:"state"`
	Reason   string          `json:"reason,omitzero"`
	Progress *CorpusProgress `json:"progress,omitzero"`
}

func (r *CorpusProgressResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// The paths ChooseCorpusPath chooses.
const (
	corpusFolderPath    = "corpus-folder"
	scanFilePath        = "scan-file"
	benchmarkFolderPath = "benchmark-folder"
)

// ChooseCorpusPath presents the host's native dialog for the folder a new
// corpus and manifest are written into ("corpus-folder"), the one stream to
// scan ("scan-file"), or the folder a new benchmark is written into
// ("benchmark-folder"). Choosing reads and writes nothing.
func (a *App) ChooseCorpusPath(kind string) CorpusPathResult {
	return run(a, true, false, func(ctx context.Context) CorpusPathResult {
		var path string
		var declined refusal
		switch kind {
		case corpusFolderPath:
			path, declined = a.chooseFolder(ctx, "Choose the folder for the new corpus and its manifest")
		case scanFilePath:
			path, declined = a.chooseOneFile(ctx, "Choose the stream to scan")
		case benchmarkFolderPath:
			path, declined = a.chooseFolder(ctx, "Choose the folder for the new benchmark")
		default:
			return CorpusPathResult{State: Failed, Reason: "unknown corpus path kind"}
		}
		if path == "" {
			return CorpusPathResult{State: declined.state, Reason: declined.reason, Kind: kind}
		}
		return CorpusPathResult{State: Completed, Kind: kind, Path: path}
	})
}

// GenerateCorpus is `readmit corpus generate`: corpus.Write streams the
// declared corpus to one new entry of the chosen folder and writes its
// readmit-corpus/v1 manifest to another, after it. Generation is new
// authoring, admitted as the command is. It is interruptible: a cancelled or
// failed generation removes the partial corpus it created and writes no
// manifest, and the progress it reached is readable meanwhile through
// CorpusProgress.
func (a *App) GenerateCorpus(request CorpusGenerateRequest) CorpusGenerateResult {
	return runNamed[CorpusGenerateResult, *CorpusGenerateResult](a, profiles["GenerateCorpus"], func(ctx context.Context) CorpusGenerateResult {
		// The command reads --seed as its flag library reads every unsigned
		// number, so a seed is read the same way here and one spelling means
		// one seed in both places.
		seed, err := strconv.ParseUint(request.Seed, 0, 64)
		if err != nil {
			return CorpusGenerateResult{State: Failed, Reason: "the seed must be a whole number from 0 to " + strconv.FormatUint(^uint64(0), 10)}
		}
		base, err := operation.DeclaredBaseTime(request.BaseTime)
		if err != nil {
			return CorpusGenerateResult{State: Failed, Reason: err.Error()}
		}
		inputs := corpus.Inputs{
			Generator: bundle.GeneratorInputs{Seed: seed, BaseTime: base, GeneratorVersion: request.GeneratorVersion, ProfileVersion: request.ProfileVersion},
			Messages:  request.Messages,
			Plan:      request.Plan,
		}
		if err := inputs.Validate(); err != nil {
			return CorpusGenerateResult{State: Failed, Reason: err.Error()}
		}
		root, declined := chosenFolder(request.Folder)
		if root == "" {
			return CorpusGenerateResult{State: declined.state, Reason: declined.reason}
		}
		if artifactpath.EntryName(request.CorpusName) != nil || artifactpath.EntryName(request.ManifestName) != nil {
			return CorpusGenerateResult{State: Failed, Reason: "the corpus and its manifest are each one new entry of the chosen folder, named by one valid file name"}
		}
		corpusPath, manifestPath := filepath.Join(root, request.CorpusName), filepath.Join(root, request.ManifestName)
		a.startCorpusProgress("generate")
		defer a.endCorpusProgress()
		manifest, err := corpus.Write(ctx, corpusPath, manifestPath, inputs, func(p corpus.Progress) {
			a.reportCorpusProgress(func(progress *CorpusProgress) { progress.Messages, progress.Bytes = p.Messages, p.Bytes })
		})
		if errors.Is(err, corpus.ErrCancelled) {
			return CorpusGenerateResult{State: Cancelled, Reason: "the generation was cancelled; the partial corpus was removed and no manifest was written"}
		}
		if err != nil {
			return CorpusGenerateResult{State: refusalState(err), Reason: err.Error()}
		}
		return CorpusGenerateResult{State: Completed, Corpus: corpusPath, ManifestPath: manifestPath, Manifest: &CorpusManifestView{
			Schema:           manifest.Schema,
			Seed:             strconv.FormatUint(manifest.Inputs.Generator.Seed, 10),
			BaseTime:         manifest.Inputs.Generator.BaseTime.UTC().Format(time.RFC3339),
			GeneratorVersion: manifest.Inputs.Generator.GeneratorVersion,
			ProfileVersion:   manifest.Inputs.Generator.ProfileVersion,
			Messages:         manifest.Inputs.Messages,
			Plan:             manifest.Inputs.Plan,
			Bytes:            manifest.Bytes,
			SHA256:           manifest.SHA256,
		}}
	})
}

// ScanCorpus is `readmit corpus scan`: the shared operation streams the one
// declared file through importer.Scan, holding one read window, one record and
// one parsing batch whatever the stream's length, and the benchmark is written
// to one new entry of the chosen folder only when the scan completed. A scan
// writes no evidence and needs no activation, as the command does not. It is
// interruptible: a cancelled scan answers with the counts it reached, the case
// bounds not evaluated and no benchmark.
func (a *App) ScanCorpus(request CorpusScanRequest) CorpusScanResult {
	return runNamed[CorpusScanResult, *CorpusScanResult](a, profiles["ScanCorpus"], func(ctx context.Context) CorpusScanResult {
		if !filepath.IsAbs(request.File) {
			return CorpusScanResult{State: Failed, Reason: "choose the stream with the file dialog; a scan reads one file named by its full path"}
		}
		if err := request.Plan.Validate(); err != nil {
			return CorpusScanResult{State: Failed, Reason: err.Error()}
		}
		report := ""
		if request.ReportFolder != "" || request.ReportName != "" {
			root, declined := chosenFolder(request.ReportFolder)
			if root == "" {
				return CorpusScanResult{State: declined.state, Reason: declined.reason}
			}
			if artifactpath.EntryName(request.ReportName) != nil {
				return CorpusScanResult{State: Failed, Reason: "the benchmark is one new entry of the chosen folder, named by one valid file name"}
			}
			report = filepath.Join(root, request.ReportName)
		}
		a.startCorpusProgress("scan")
		defer a.endCorpusProgress()
		scan, err := operation.ScanCorpus(ctx, request.File, importer.ScanOptions{
			Plan:         request.Plan,
			Window:       importer.Window{Offset: request.WindowOffset, Limit: request.WindowLimit},
			BatchRecords: request.BatchRecords,
			BatchBytes:   request.BatchBytes,
			Report: func(p importer.Progress) {
				a.reportCorpusProgress(func(progress *CorpusProgress) {
					progress.Bytes, progress.Records, progress.Occurrences, progress.Batches = p.Bytes, p.Records, p.Occurrences, p.Batches
				})
			},
		}, report)
		if err != nil {
			return CorpusScanResult{State: refusalState(err), Reason: err.Error()}
		}
		view := scanView(scan)
		if scan.Cancelled {
			return CorpusScanResult{State: Cancelled, Reason: "the scan was cancelled; these are the counts it reached, the case bounds were not evaluated and no benchmark was written", Scan: view}
		}
		if report == "" {
			return CorpusScanResult{State: Completed, Scan: view}
		}
		if err := operation.WriteBenchmark(report, scan); err != nil {
			return CorpusScanResult{State: refusalState(err), Reason: err.Error(), Scan: view}
		}
		return CorpusScanResult{State: Completed, Scan: view, Benchmark: report}
	})
}

// CorpusProgress reads what the running generation or scan has reached. It
// does not claim the operation slot, so the screen can read it while the
// operation holds the slot, and it starts and changes nothing.
func (a *App) CorpusProgress() CorpusProgressResult {
	a.corpusMu.Lock()
	defer a.corpusMu.Unlock()
	if a.corpusProgress == nil {
		return CorpusProgressResult{State: Empty}
	}
	reached := *a.corpusProgress
	return CorpusProgressResult{State: Completed, Progress: &reached}
}

func (a *App) startCorpusProgress(operation string) {
	a.corpusMu.Lock()
	defer a.corpusMu.Unlock()
	a.corpusProgress = &CorpusProgress{Operation: operation}
}

func (a *App) reportCorpusProgress(update func(*CorpusProgress)) {
	a.corpusMu.Lock()
	defer a.corpusMu.Unlock()
	if a.corpusProgress != nil {
		update(a.corpusProgress)
	}
}

func (a *App) endCorpusProgress() {
	a.corpusMu.Lock()
	defer a.corpusMu.Unlock()
	a.corpusProgress = nil
}

// scanView is the scan as data, the way renderScan prints it: a cancelled
// scan is not asked about the case bounds at all, because reporting an unread
// remainder as within them would report unknown as a pass.
func scanView(scan operation.CorpusScan) *CorpusScanView {
	result := scan.Result
	view := &CorpusScanView{
		Plan: scan.Plan, Bytes: result.Bytes, SHA256: result.SHA256,
		Records: result.Records, Occurrences: result.Occurrences, Decoded: result.Decoded,
		Undecodable: result.Undecodable, Batches: result.Batches, PeakResidentBytes: result.PeakResidentBytes,
		Bounds: scan.Bounds, CaseBounds: CaseBoundsNotEvaluated,
		WindowOffset: result.Window.Offset, WindowLimit: result.Window.Limit, Rows: []importer.Row{},
		ElapsedMilliseconds: scan.Elapsed.Milliseconds(), Targets: corpus.TargetNote,
	}
	if result.Rows != nil {
		view.Rows = result.Rows
	}
	if !scan.Cancelled {
		view.CaseBounds = CaseBoundsWithin
		if past := result.ExceedsCase(scan.Plan); len(past) > 0 {
			view.CaseBounds, view.Exceeded = CaseBoundsExceeded, past
		}
	}
	return view
}
