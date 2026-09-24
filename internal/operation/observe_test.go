package operation_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
)

const observeWindowDocument = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "3s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 32}
}`

const observeSourceDocument = `{
  "schema": "readmit-observation-source/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "enabled": true,
  "freshness": {"max_age": "1h"},
  "extraction": {
    "envelope": "csv",
    "encoding": "utf-8",
    "csv": {"delimiter": ",", "record_separator": "lf", "header": "present", "fields": 2},
    "record_key": ["appointment"]
  },
  "file": {"path": "export.csv", "max_bytes": 65536},
  "http": null
}`

func writeObserveDocs(t *testing.T) (dir, window, source string) {
	t.Helper()
	dir = t.TempDir()
	window = filepath.Join(dir, "window.json")
	source = filepath.Join(dir, "source.json")
	for path, body := range map[string]string{
		window:                           observeWindowDocument,
		source:                           observeSourceDocument,
		filepath.Join(dir, "export.csv"): "appointment,status\nA1,booked\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, window, source
}

func TestSavingAnObservationWindowRoundTripsCLIDocuments(t *testing.T) {
	dir, windowPath, _ := writeObserveDocs(t)
	original, identity, err := operation.ValidateObservationWindow(windowPath)
	if err != nil || identity == "" {
		t.Fatalf("validate: %v identity=%q", err, identity)
	}
	out := filepath.Join(dir, "rewritten-window.json")
	saved, pinned, err := operation.SaveObservationWindow(out, original)
	if err != nil {
		t.Fatal(err)
	}
	if pinned != identity || saved.Identity() != identity {
		t.Fatalf("identity changed on save: got %q want %q", pinned, identity)
	}
	reread, err := observewindow.ReadWindow(out)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Source != original.Source || reread.Completion != original.Completion {
		t.Fatalf("rewritten window dropped fields: %+v", reread)
	}
}

func TestSavingAnObservationSourceRoundTripsWithoutWideningScope(t *testing.T) {
	dir, _, sourcePath := writeObserveDocs(t)
	original, _, err := operation.ValidateObservationSource(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	// Relativize for rewrite beside a new document.
	original.File.Path = "export.csv"
	out := filepath.Join(dir, "rewritten-source.json")
	saved, identity, err := operation.SaveObservationSource(out, original)
	if err != nil || identity == "" {
		t.Fatalf("save: %v identity=%q", err, identity)
	}
	if saved.Observes.Scope != "appointments" || saved.File == nil || saved.HTTP != nil {
		t.Fatalf("scope or transport widened: %+v", saved)
	}
	bytes, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), `"scope": "orders"`) {
		t.Fatal("rewrite widened the declared scope")
	}
}

func TestCollectionWithoutAuthorizationNeverQueries(t *testing.T) {
	dir, window, source := writeObserveDocs(t)
	_, err := operation.CollectObservation(context.Background(), operation.ObservationCollectRequest{
		SourcePath: source, WindowPath: window,
		OutputPath: filepath.Join(dir, "completion.json"), SnapshotPath: filepath.Join(dir, "snapshot"),
		Authorize: false,
	})
	if err == nil || !strings.Contains(err.Error(), "explicit authorization") {
		t.Fatalf("unauthorized collect: %v", err)
	}
}

func TestAuthorizedCollectionMatchesCLICollectors(t *testing.T) {
	dir, window, source := writeObserveDocs(t)
	completion, err := operation.CollectObservation(context.Background(), operation.ObservationCollectRequest{
		SourcePath: source, WindowPath: window,
		OutputPath: filepath.Join(dir, "completion.json"), SnapshotPath: filepath.Join(dir, "snapshot"),
		Authorize: true, Produced: []string{"A1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !completion.Trustworthy() || completion.RecordsObserved != 1 {
		t.Fatalf("completion = %+v", completion)
	}
	summary := operation.SummarizeObservationCompletion(completion)
	if !summary.Supported || summary.Mapped != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	explained, err := operation.ExplainObservation(filepath.Join(dir, "completion.json"), window)
	if err != nil || explained.Status != completion.Status {
		t.Fatalf("explain: %v status=%s", err, explained.Status)
	}
}

func TestCaptureBindingBuildsDownstreamSourceWithoutCollecting(t *testing.T) {
	source, err := operation.SourceFromCaptureBinding(operation.CaptureObservationBinding{
		Identity: "scheduling-downstream", Scope: "appointments",
		Kinds: []string{"message"}, RecordKey: "SCH-1.1", MaxOccurrences: 50,
	}, "downstream.case")
	if err != nil {
		t.Fatal(err)
	}
	if source.Observes.Kind != observesource.DownstreamCapture || source.Capture == nil || source.Extraction != nil {
		t.Fatalf("binding = %+v", source)
	}
	if source.Capture.Path != "downstream.case" || source.Capture.MaxOccurrences != 50 {
		t.Fatalf("capture = %+v", source.Capture)
	}
}

func TestObservationSupportMarksDatabaseUnqualified(t *testing.T) {
	var sawDB bool
	for _, row := range operation.ObservationSupport() {
		if row.Kind == observesource.DatabaseQuery {
			sawDB = true
			if row.Qualification != "unqualified" || row.ProductionClaim {
				t.Fatalf("database row claims production support: %+v", row)
			}
		}
	}
	if !sawDB {
		t.Fatal("database adapters missing from support list")
	}
}

func TestMismatchedSourceAndWindowRefuseLocally(t *testing.T) {
	dir, window, source := writeObserveDocs(t)
	mismatched := strings.Replace(observeSourceDocument, `"scope": "appointments"`, `"scope": "orders"`, 1)
	path := filepath.Join(dir, "mismatched.json")
	if err := os.WriteFile(path, []byte(mismatched), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := operation.ValidateObservationPair(path, window); err == nil {
		t.Fatal("mismatched pair was accepted")
	}
	_ = source
}

// A source that names its export relative to its own folder has one identity:
// the identity of what the document declares, which is the digest of the
// canonical bytes the save wrote. Saving pins it, validating and reopening the
// document report it, and saving what the save answered writes the same
// bytes again, so the declared relative path never becomes this machine's
// absolute one. The reader the command line collects through still resolves
// the export beside the document.
func TestASourceWithARelativeExportPathIsSavedAndValidatedAsOneIdentity(t *testing.T) {
	declared, err := observesource.DecodeSource([]byte(strings.Replace(observeSourceDocument, `"export.csv"`, `"exports/appointments.csv"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "observations", "source.json")
	saved, pinned, err := operation.SaveObservationSource(path, declared)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes.TrimSuffix(written, []byte("\n")))
	if pinned != hex.EncodeToString(sum[:]) {
		t.Fatalf("the save pinned %s, not the identity of the bytes it wrote", pinned)
	}
	if saved.File == nil || saved.File.Path != "exports/appointments.csv" {
		t.Fatalf("the save answered %+v, not the document it wrote", saved.File)
	}
	validated, identity, err := operation.ValidateObservationSource(path)
	if err != nil || identity != pinned {
		t.Fatalf("validate reported %q (%v), the save pinned %q", identity, err, pinned)
	}
	if validated.File == nil || validated.File.Path != "exports/appointments.csv" {
		t.Fatalf("validate answered %+v, not what the document declares", validated.File)
	}
	opened, openedIdentity, err := operation.OpenOrNewObservationSource(path)
	if err != nil || openedIdentity != pinned || opened.File.Path != "exports/appointments.csv" {
		t.Fatalf("reopened %+v (%v) with identity %q, the save pinned %q", opened.File, err, openedIdentity, pinned)
	}
	if _, again, err := operation.SaveObservationSource(path, saved); err != nil || again != pinned {
		t.Fatalf("saving the answer again pinned %q (%v), not %q", again, err, pinned)
	}
	if rewritten, err := os.ReadFile(path); err != nil || !bytes.Equal(rewritten, written) {
		t.Fatalf("saving the answer again changed the document:\n%s\n%s", written, rewritten)
	}
	read, err := observesource.ReadSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(read.File.Path) || !strings.HasSuffix(read.File.Path, filepath.Join("observations", "exports", "appointments.csv")) {
		t.Fatalf("the collector's reader resolved the export to %s", read.File.Path)
	}
}

// A document the writer refuses to place inside retained evidence leaves
// nothing behind there, not even the folder it would have been written into.
func TestARefusedSaveInsideRetainedEvidenceCreatesNothing(t *testing.T) {
	evidence := filepath.Join(t.TempDir(), "incident.case")
	if err := os.Mkdir(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte(strings.Repeat("a", 64)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := observesource.DecodeSource([]byte(observeSourceDocument))
	if err != nil {
		t.Fatal(err)
	}
	window, err := observewindow.DecodeWindow([]byte(observeWindowDocument))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := operation.SaveObservationSource(filepath.Join(evidence, "observations", "source.json"), source); err == nil || err.Error() != "cannot write an observation source here" {
		t.Fatalf("a source inside retained evidence: %v", err)
	}
	if _, _, err := operation.SaveObservationWindow(filepath.Join(evidence, "windows", "deep", "window.json"), window); err == nil || err.Error() != "cannot write an observation window here" {
		t.Fatalf("a window inside retained evidence: %v", err)
	}
	entries, err := os.ReadDir(evidence)
	if err != nil || len(entries) != 1 {
		t.Fatalf("a refused save left %v inside the evidence (%v)", entries, err)
	}
}
