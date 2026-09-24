package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observewindow"
)

// Both documents below are authored here rather than produced by the engine, so
// the collector is exercised against text an operator could have written.
const collectWindowDocument = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "3s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 32}
}`

const collectSourceDocument = `{
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

const collectExport = "appointment,status\nA1,booked\nA2,booked\n"

// collectDocuments writes one window, one source and the export the source
// names, and returns the directory they share.
func collectDocuments(t *testing.T, source string) (directory, window, sourcePath string) {
	t.Helper()
	directory = t.TempDir()
	window = filepath.Join(directory, "window.json")
	sourcePath = filepath.Join(directory, "source.json")
	for path, document := range map[string]string{
		window:                                 collectWindowDocument,
		sourcePath:                             source,
		filepath.Join(directory, "export.csv"): collectExport,
	} {
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory, window, sourcePath
}

func TestObserveCollectObservesADeclaredExportAndRetainsWhatItRead(t *testing.T) {
	directory, window, source := collectDocuments(t, collectSourceDocument)
	record := filepath.Join(directory, "completion.json")
	snapshot := filepath.Join(directory, "snapshot")
	stdout, stderr, err := run(t, "observe", "collect", source, "--window", window,
		"--out", record, "--snapshot", snapshot, "--produced", "A1")
	if err != nil {
		t.Fatalf("a completed collection was refused: %v %s", err, stderr)
	}
	for _, expected := range []string{"Status: complete", "Boundary: observation-window",
		"kind file-export", "Records observed: 2", "Correlations: 1 recorded, 1 matched",
		"Absence assertion: supported by this completion"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("collection did not report %q:\n%s", expected, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("a completed collection wrote diagnostics: %q", stderr)
	}
	// The original material is retained exactly as it was read.
	body, readErr := os.ReadFile(filepath.Join(snapshot, "read-0000", "body"))
	if readErr != nil || string(body) != collectExport {
		t.Fatalf("the retained snapshot is not the bytes that were read: %v", readErr)
	}
	// The retained record re-decides to the same verdict against its window.
	if _, _, err := run(t, "observe", "explain", record, "--window", window); err != nil {
		t.Fatalf("the retained completion did not re-decide to its own verdict: %v", err)
	}
}

func TestObserveCollectNeverReadsFailedCollectionAsAbsence(t *testing.T) {
	for name, document := range map[string]string{
		"a disabled collector": strings.Replace(collectSourceDocument, `"enabled": true`, `"enabled": false`, 1),
		"a truncated export":   strings.Replace(collectSourceDocument, `"max_bytes": 65536`, `"max_bytes": 8`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			directory, window, source := collectDocuments(t, document)
			record := filepath.Join(directory, "completion.json")
			stdout, stderr, err := run(t, "observe", "collect", source, "--window", window,
				"--out", record, "--snapshot", filepath.Join(directory, "snapshot"))
			if code := exitCode(t, err); code != 2 {
				t.Fatalf("%s exited %d, want the observation error status 2", name, code)
			}
			if !strings.Contains(stdout, "Absence assertion: not supported") {
				t.Fatalf("%s was read as an absence assertion:\n%s", name, stdout)
			}
			if !strings.Contains(stdout, "Records observed: not settled") {
				t.Fatalf("%s reported a settled count:\n%s", name, stdout)
			}
			if !strings.Contains(stdout, "Durable run state: execution_error") {
				t.Fatalf("%s was not an execution error:\n%s", name, stdout)
			}
			// The record is retained whatever it says, so the failure is
			// evidence rather than an absence of evidence.
			data, readErr := os.ReadFile(record)
			if readErr != nil {
				t.Fatalf("%s retained no record: %v", name, readErr)
			}
			completion, decodeErr := observewindow.DecodeCompletion(data)
			if decodeErr != nil || completion.Trustworthy() {
				t.Fatalf("%s retained a record reading as trustworthy: %v", name, decodeErr)
			}
			if stderr != "" {
				t.Fatalf("%s interleaved a diagnostic with its output: %q", name, stderr)
			}
		})
	}
}

// A source that declares a different source than the window observes is a
// mistake in the two declarations, not an observation: it is refused before
// anything is read or retained, in the words the window refuses it with,
// rather than retained as a completion saying the source was unsupported.
func TestObserveCollectRefusesASourceTheWindowDoesNotObserve(t *testing.T) {
	for name, document := range map[string]string{
		"another scope":    strings.Replace(collectSourceDocument, `"scope": "appointments"`, `"scope": "orders"`, 1),
		"another identity": strings.Replace(collectSourceDocument, `"identity": "scheduling-archive"`, `"identity": "billing-archive"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			directory, window, source := collectDocuments(t, document)
			record, snapshot := filepath.Join(directory, "completion.json"), filepath.Join(directory, "snapshot")
			for _, format := range [][]string{nil, {"--json"}} {
				stdout, stderr, err := run(t, append([]string{"observe", "collect", source, "--window", window,
					"--out", record, "--snapshot", snapshot}, format...)...)
				if code := exitCode(t, err); code != 1 || stdout != "" ||
					stderr != "readmit: the observation source and window must declare the same source kind, identity, and scope\n" {
					t.Fatalf("a mismatched pair exited %d with %q and %q", code, stdout, stderr)
				}
				for _, written := range []string{record, snapshot} {
					if _, err := os.Lstat(written); !os.IsNotExist(err) {
						t.Fatalf("a refused pair retained %s", filepath.Base(written))
					}
				}
			}
		})
	}
}

func TestObserveCollectWritesTheCanonicalRecordInMachineReadableMode(t *testing.T) {
	directory, window, source := collectDocuments(t, collectSourceDocument)
	record := filepath.Join(directory, "completion.json")
	stdout, stderr, err := run(t, "observe", "collect", source, "--window", window,
		"--out", record, "--snapshot", filepath.Join(directory, "snapshot"), "--json")
	if err != nil {
		t.Fatalf("a completed collection was refused: %v %s", err, stderr)
	}
	completion, decodeErr := observewindow.DecodeCompletion([]byte(stdout))
	if decodeErr != nil {
		t.Fatalf("the canonical record the command wrote does not read back: %v", decodeErr)
	}
	if completion.Status != observewindow.Complete || completion.RecordsObserved != 2 {
		t.Fatalf("the canonical record is not the collected one: %+v", completion)
	}
	if stderr != "" {
		t.Fatalf("machine-readable mode interleaved a diagnostic: %q", stderr)
	}
}

func TestObserveCollectRefusesToRewriteRetainedEvidence(t *testing.T) {
	directory, window, source := collectDocuments(t, collectSourceDocument)
	record := filepath.Join(directory, "completion.json")
	snapshot := filepath.Join(directory, "snapshot")
	if _, _, err := run(t, "observe", "collect", source, "--window", window, "--out", record, "--snapshot", snapshot); err != nil {
		t.Fatalf("the first collection was refused: %v", err)
	}
	stdout, stderr, err := run(t, "observe", "collect", source, "--window", window, "--out", record, "--snapshot", snapshot)
	if exitCode(t, err) != 1 {
		t.Fatal("a second collection overwrote the first")
	}
	if stdout != "" {
		t.Fatalf("a refused collection produced command output: %q", stdout)
	}
	if strings.Contains(stderr, snapshot) || strings.Contains(stderr, "A1") {
		t.Fatalf("the diagnostic echoed a path or an observed value: %q", stderr)
	}
}

func TestObserveCollectRequiresTheWindowRecordAndSnapshotItIsGiven(t *testing.T) {
	directory, window, source := collectDocuments(t, collectSourceDocument)
	for name, args := range map[string][]string{
		"no window":   {"observe", "collect", source, "--out", filepath.Join(directory, "a.json"), "--snapshot", filepath.Join(directory, "a")},
		"no record":   {"observe", "collect", source, "--window", window, "--snapshot", filepath.Join(directory, "b")},
		"no snapshot": {"observe", "collect", source, "--window", window, "--out", filepath.Join(directory, "c.json")},
		"no source":   {"observe", "collect", "--window", window, "--out", filepath.Join(directory, "d.json"), "--snapshot", filepath.Join(directory, "d")},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := run(t, args...)
			// A missing flag is a misuse refusal, so it carries the usage
			// status every command reaches through argument parsing.
			if exitCode(t, err) != exitRefused {
				t.Fatalf("%s was accepted", name)
			}
			if stdout != "" {
				t.Fatalf("%s produced command output: %q", name, stdout)
			}
			if stderr == "" {
				t.Fatalf("%s reported nothing", name)
			}
		})
	}
}
