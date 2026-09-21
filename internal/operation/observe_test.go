package operation_test

import (
	"context"
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
