package operation

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// ObservationAdapterSupport describes one transport the collector reaches and
// whether this release may claim it as a supported production adapter. An
// unqualified database driver is listed so the UI can show it without silently
// upgrading it to a production claim (#75 owns live qualification).
type ObservationAdapterSupport struct {
	Kind            string `json:"kind"`
	Schema          string `json:"schema"`
	Adapter         string `json:"adapter"`
	Version         string `json:"version"`
	Qualification   string `json:"qualification"`
	ProductionClaim bool   `json:"production_claim"`
	Notes           string `json:"notes,omitzero"`
}

// ObservationSupport lists the adapters this release exposes through the shared
// collectors. Qualification for database drivers stays pending until #75.
func ObservationSupport() []ObservationAdapterSupport {
	return []ObservationAdapterSupport{
		{Kind: observesource.FileExport, Schema: observesource.SchemaV1, Adapter: "file-export", Version: "v1", Qualification: "supported", ProductionClaim: true},
		{Kind: observesource.HTTPAPI, Schema: observesource.SchemaV1, Adapter: "http-api", Version: "v1", Qualification: "supported", ProductionClaim: true},
		{Kind: observesource.DownstreamCapture, Schema: observesource.Schema, Adapter: "downstream-capture", Version: "v2", Qualification: "supported", ProductionClaim: true},
		{Kind: observesource.DatabaseQuery, Schema: observesource.SchemaDatabase, Adapter: "postgresql", Version: "lab-harness", Qualification: "unqualified", ProductionClaim: false, Notes: "Live qualification evidence is owned by bharm16/readmit#75"},
		{Kind: observesource.DatabaseQuery, Schema: observesource.SchemaDatabase, Adapter: "sqlserver", Version: "lab-harness", Qualification: "unqualified", ProductionClaim: false, Notes: "Live qualification evidence is owned by bharm16/readmit#75"},
		{Kind: observesource.DatabaseQuery, Schema: observesource.SchemaDatabase, Adapter: "oracle", Version: "lab-harness", Qualification: "unqualified", ProductionClaim: false, Notes: "Live qualification evidence is owned by bharm16/readmit#75"},
	}
}

// DefaultObservationWindow returns a starting window an editor can fill in.
// It is not a default used at collection time: collection still requires an
// explicitly selected document.
func DefaultObservationWindow() observewindow.Window {
	return observewindow.Window{
		Schema:      observewindow.WindowSchema,
		Source:      observewindow.Source{Kind: observesource.FileExport, Identity: "scheduling-archive", Scope: "appointments"},
		Watermark:   observewindow.Watermark{Kind: observewindow.NoWatermark, Position: ""},
		PreExisting: observewindow.PreExisting{Declaration: observewindow.DeclaredEmpty, BaselineIdentity: ""},
		Completion:  observewindow.Rule{Deadline: "30s", QuietPeriod: "2s", StableSamples: 3, MaxRecords: 100, MaxSamples: 16},
	}
}

// DefaultObservationSource returns a starting file-export source for an editor.
func DefaultObservationSource() observesource.Source {
	return observesource.Source{
		Schema:    observesource.SchemaV1,
		Observes:  observewindow.Source{Kind: observesource.FileExport, Identity: "scheduling-archive", Scope: "appointments"},
		Enabled:   true,
		Freshness: observesource.Freshness{MaxAge: "1h"},
		Extraction: &observesource.Extraction{
			Envelope:  importer.CSVEnvelope,
			Encoding:  importer.UTF8,
			CSV:       &importer.CSVDialect{Delimiter: ",", RecordSeparator: importer.LFSeparator, Header: importer.HeaderPresent, Fields: 2},
			RecordKey: importer.Locator{"appointment"},
		},
		File: &observesource.File{Path: "export.csv", MaxBytes: 65536},
	}
}

// OpenOrNewObservationWindow reads a window to edit, or returns the default
// starting values when the file does not exist yet.
func OpenOrNewObservationWindow(path string) (observewindow.Window, error) {
	if path == "" {
		return observewindow.Window{}, errors.New("observation window path must not be empty")
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return DefaultObservationWindow(), nil
	}
	return observewindow.ReadWindow(path)
}

// SaveObservationWindow writes one window atomically and reads it back.
func SaveObservationWindow(path string, window observewindow.Window) (observewindow.Window, string, error) {
	if path == "" {
		return observewindow.Window{}, "", errors.New("observation window path must not be empty")
	}
	makeDocumentFolder(path)
	if window.Schema == "" {
		window.Schema = observewindow.WindowSchema
	}
	if err := observewindow.WriteWindow(path, window); err != nil {
		return observewindow.Window{}, "", err
	}
	return ValidateObservationWindow(path)
}

// ValidateObservationWindow reads and validates without observing anything.
func ValidateObservationWindow(path string) (observewindow.Window, string, error) {
	window, err := observewindow.ReadWindow(path)
	if err != nil {
		return observewindow.Window{}, "", err
	}
	return window, window.Identity(), nil
}

// OpenOrNewObservationSource reads a source to edit, or returns defaults. It
// answers the source as the document declares it, so what an editor saves
// back keeps the paths the document declared rather than this machine's.
func OpenOrNewObservationSource(path string) (observesource.Source, error) {
	if path == "" {
		return observesource.Source{}, errors.New("observation source path must not be empty")
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return DefaultObservationSource(), nil
	}
	source, _, err := ValidateObservationSource(path)
	return source, err
}

// CaptureObservationBinding is the navigation handoff from a retained capture
// (#250) into observation authoring. It names an existing verified case only;
// opening the editor with this binding never starts a listener or collector.
type CaptureObservationBinding struct {
	CasePath       string   `json:"case_path"`
	Identity       string   `json:"identity"`
	Scope          string   `json:"scope"`
	Kinds          []string `json:"kinds"`
	RecordKey      string   `json:"record_key"`
	MaxOccurrences int      `json:"max_occurrences"`
	Freshness      string   `json:"freshness"`
}

// SourceFromCaptureBinding builds a downstream-capture source declaration from
// a retained case path. Paths stay relative to the document that will hold them.
func SourceFromCaptureBinding(binding CaptureObservationBinding, relativeCase string) (observesource.Source, error) {
	if binding.CasePath == "" && relativeCase == "" {
		return observesource.Source{}, errors.New("a capture observation requires a retained case path")
	}
	path := relativeCase
	if path == "" {
		path = binding.CasePath
	}
	identity := binding.Identity
	if identity == "" {
		identity = "downstream-capture"
	}
	scope := binding.Scope
	if scope == "" {
		scope = "appointments"
	}
	kinds := binding.Kinds
	if len(kinds) == 0 {
		kinds = []string{"message"}
	}
	recordKey := binding.RecordKey
	if recordKey == "" {
		recordKey = "SCH-1.1"
	}
	max := binding.MaxOccurrences
	if max == 0 {
		max = 100
	}
	freshness := binding.Freshness
	if freshness == "" {
		freshness = "1h"
	}
	source := observesource.Source{
		Schema:    observesource.Schema,
		Observes:  observewindow.Source{Kind: observesource.DownstreamCapture, Identity: identity, Scope: scope},
		Enabled:   true,
		Freshness: observesource.Freshness{MaxAge: freshness},
		Capture: &observesource.Capture{
			Path:           path,
			Kinds:          append([]string(nil), kinds...),
			RecordKey:      recordKey,
			MaxOccurrences: max,
		},
	}
	if err := source.Validate(); err != nil {
		return observesource.Source{}, err
	}
	return source, nil
}

// SaveObservationSource writes one source atomically and reads it back the way
// ValidateObservationSource reads it, so a save and a later validation of the
// same document report one identity.
func SaveObservationSource(path string, source observesource.Source) (observesource.Source, string, error) {
	if path == "" {
		return observesource.Source{}, "", errors.New("observation source path must not be empty")
	}
	makeDocumentFolder(path)
	if err := observesource.WriteSource(path, source); err != nil {
		return observesource.Source{}, "", err
	}
	return ValidateObservationSource(path)
}

// ValidateObservationSource reads and validates without collecting, through
// the reader `readmit observe collect` reads a source with. It answers the
// source as the document declares it and that declaration's identity, the
// digest of the canonical form the window writes: the paths a document
// declares relative to its own folder are resolved only where it is
// collected, so its identity does not depend on where the folder is.
func ValidateObservationSource(path string) (observesource.Source, string, error) {
	source, _, err := observesource.ReadDeclaredSource(path)
	if err != nil {
		return observesource.Source{}, "", err
	}
	return source, source.Identity(), nil
}

// makeDocumentFolder creates the folder a declared document is saved into,
// but only where the shared output reservation allows the first folder it
// would create. Inside retained evidence it creates nothing, and the writer
// then refuses the document in its own words, so a refused save leaves no
// folder behind either.
func makeDocumentFolder(path string) {
	missing := ""
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil || filepath.Dir(dir) == dir {
			break
		}
		missing = dir
	}
	if missing == "" {
		return
	}
	if _, err := artifactpath.Destination(missing); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0755)
	}
}

// ValidateObservationPair checks that a source and window agree locally without
// querying any endpoint or database.
func ValidateObservationPair(sourcePath, windowPath string) (observesource.Source, observewindow.Window, error) {
	source, err := observesource.ReadSource(sourcePath)
	if err != nil {
		return observesource.Source{}, observewindow.Window{}, err
	}
	window, err := observewindow.ReadWindow(windowPath)
	if err != nil {
		return observesource.Source{}, observewindow.Window{}, err
	}
	if source.Observes != window.Source {
		return observesource.Source{}, observewindow.Window{}, errors.New("the observation source and window must declare the same source kind, identity, and scope")
	}
	return source, window, nil
}

// ObservationCollectRequest names the documents and destinations for one
// authorized collection. Authorize must be true; opening an editor never sets it.
type ObservationCollectRequest struct {
	SourcePath   string
	WindowPath   string
	OutputPath   string
	SnapshotPath string
	PolicyPath   string
	Produced     []string
	Authorize    bool
}

// CollectObservation runs one authorized read-only collection through the same
// collectors the CLI uses. Without Authorize it refuses rather than querying.
func CollectObservation(ctx context.Context, request ObservationCollectRequest) (observewindow.Completion, error) {
	if !request.Authorize {
		return observewindow.Completion{}, errors.New("collection requires explicit authorization; opening an editor never queries a source")
	}
	if request.SourcePath == "" || request.WindowPath == "" || request.OutputPath == "" || request.SnapshotPath == "" {
		return observewindow.Completion{}, errors.New("collection requires a source, window, completion destination, and snapshot directory")
	}
	source, window, err := ValidateObservationPair(request.SourcePath, request.WindowPath)
	if err != nil {
		return observewindow.Completion{}, err
	}
	var policy *sendpolicy.Policy
	if request.PolicyPath != "" {
		declared, readErr := ReadSendPolicy(request.PolicyPath)
		if readErr != nil {
			return observewindow.Completion{}, readErr
		}
		policy = &declared
	}
	completion, err := observesource.Collect(ctx, source, window, observesource.Options{
		Snapshot: request.SnapshotPath,
		Policy:   policy,
		Resolve:  sendpolicy.SystemResolver,
		Produced: request.Produced,
	})
	if err != nil {
		return observewindow.Completion{}, err
	}
	if err := observewindow.WriteCompletion(request.OutputPath, completion); err != nil {
		return observewindow.Completion{}, err
	}
	return observewindow.ReadCompletion(request.OutputPath)
}

// ExplainObservation re-reads a retained completion, optionally against its window.
func ExplainObservation(completionPath, windowPath string) (observewindow.Completion, error) {
	completion, err := observewindow.ReadCompletion(completionPath)
	if err != nil {
		return observewindow.Completion{}, err
	}
	if windowPath == "" {
		return completion, nil
	}
	window, err := observewindow.ReadWindow(windowPath)
	if err != nil {
		return observewindow.Completion{}, err
	}
	if err := window.Verify(completion); err != nil {
		return observewindow.Completion{}, err
	}
	return completion, nil
}

// ResolveObservationPath joins a workspace-relative entry onto a workspace root.
func ResolveObservationPath(workspace, entry string) (string, error) {
	if entry == "" {
		return "", errors.New("an observation document path is required")
	}
	if filepath.IsAbs(entry) {
		return artifactpath.Destination(entry)
	}
	if workspace == "" {
		return "", errors.New("a workspace is required for a relative observation path")
	}
	return artifactpath.Child(workspace, entry)
}

// ObservationAbsenceSummary reports whether a completion can support an absence claim.
type ObservationAbsenceSummary struct {
	Supported   bool   `json:"supported"`
	Reason      string `json:"reason,omitzero"`
	Status      string `json:"status"`
	Records     int    `json:"records_observed"`
	Mapped      int    `json:"mapped_correlations"`
	Unmapped    int    `json:"unmapped_correlations"`
	Stale       bool   `json:"stale"`
	Partial     bool   `json:"partial"`
	Trustworthy bool   `json:"trustworthy"`
}

// SummarizeObservationCompletion derives UI counts without leaking values.
func SummarizeObservationCompletion(completion observewindow.Completion) ObservationAbsenceSummary {
	summary := ObservationAbsenceSummary{
		Status:      string(completion.Status),
		Records:     completion.RecordsObserved,
		Trustworthy: completion.Trustworthy(),
		Stale:       completion.Status == observewindow.Stale,
		Partial:     completion.Status == observewindow.Incomplete || completion.Status == observewindow.Truncated,
	}
	for _, correlation := range completion.Correlations {
		if correlation.Kind == observewindow.Matched {
			summary.Mapped++
		} else {
			summary.Unmapped++
		}
	}
	if err := completion.AbsenceEvidence(); err != nil {
		summary.Reason = err.Error()
		return summary
	}
	summary.Supported = true
	return summary
}
