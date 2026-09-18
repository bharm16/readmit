package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observewindow"
)

// The two documents below are authored here rather than produced by the engine,
// so the command is exercised against text an operator or a collector could
// have written.
const observeWindowDocument = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "downstream-capture", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "declared-position", "position": "2026-01-03T11:00:00Z"},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "30s", "quiet_period": "2s", "stable_samples": 3, "max_records": 100, "max_samples": 16}
}`

const observeCompletionDocument = `{
  "schema": "readmit-observation-completion/v1",
  "boundary": "observation-window",
  "window_identity": "WINDOW",
  "source": {"kind": "downstream-capture", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "declared-position", "position": "2026-01-03T11:00:00Z"},
  "status": "STATUS",
  "stop": "none",
  "opened_at": "2026-01-03T11:00:00Z",
  "closed_at": "2026-01-03T11:00:03Z",
  "baseline": null,
  "samples": [SAMPLES],
  "correlations": [],
  "stable_samples": STABLE,
  "quiet_period": "QUIET",
  "records_observed": 0,
  "pre_existing_basis": "declared-empty"
}`

const observedSamples = `{"at": "2026-01-03T11:00:01Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"},
    {"at": "2026-01-03T11:00:02Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"},
    {"at": "2026-01-03T11:00:03Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"}`

func observeDocuments(t *testing.T) (directory, window string) {
	t.Helper()
	directory = t.TempDir()
	window = filepath.Join(directory, "window.json")
	if err := os.WriteFile(window, []byte(observeWindowDocument), 0600); err != nil {
		t.Fatal(err)
	}
	return directory, window
}

// writeObserveDocument renders one completion without writing it, so a test
// can adjust the text further before it reaches disk.
func observeDocument(t *testing.T, status, samples, stable, quiet string) string {
	t.Helper()
	declared, err := observewindow.DecodeWindow([]byte(observeWindowDocument))
	if err != nil {
		t.Fatal(err)
	}
	return strings.NewReplacer(
		"WINDOW", declared.Identity(), "STATUS", status, "SAMPLES", samples,
		"STABLE", stable, "QUIET", quiet).Replace(observeCompletionDocument)
}

func writeObserveCompletion(t *testing.T, directory, name, status, samples, stable, quiet string) string {
	t.Helper()
	document := observeDocument(t, status, samples, stable, quiet)
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestObserveValidateReadsADeclaredWindowWithoutObservingAnything(t *testing.T) {
	_, window := observeDocuments(t)
	stdout, stderr, err := run(t, "observe", "validate", window)
	if err != nil {
		t.Fatalf("a valid observation window was refused: %v %s", err, stderr)
	}
	for _, expected := range []string{"Observation window:", "kind downstream-capture", "scope appointments",
		"Watermark: declared-position", "Pre-existing state: declared-empty",
		"3 stable samples spanning 2s, within 30s", "observes no source and produces no verdict"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("validation did not report %q:\n%s", expected, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("validation wrote diagnostics for a valid window: %q", stderr)
	}

	canonical, _, err := run(t, "observe", "validate", window, "--json")
	if err != nil {
		t.Fatalf("machine-readable validation failed: %v", err)
	}
	decoded, decodeErr := observewindow.DecodeWindow([]byte(canonical))
	if decodeErr != nil {
		t.Fatalf("the canonical window the command wrote does not read back: %v", decodeErr)
	}
	if decoded.Source.Scope != "appointments" {
		t.Fatalf("the canonical window is not the declared one: %+v", decoded)
	}
}

func TestObserveValidateRefusesAWindowThatCannotComplete(t *testing.T) {
	directory, _ := observeDocuments(t)
	broken := filepath.Join(directory, "broken.json")
	document := strings.Replace(observeWindowDocument, `"quiet_period": "2s"`, `"quiet_period": "40s"`, 1)
	if err := os.WriteFile(broken, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "observe", "validate", broken)
	if exitCode(t, err) == 0 {
		t.Fatal("a window whose quiet period outlasts its deadline was accepted")
	}
	if stdout != "" {
		t.Fatalf("a refused window produced command output: %q", stdout)
	}
	if !strings.Contains(stderr, "quiet period") {
		t.Fatalf("the refusal did not say what was wrong: %q", stderr)
	}
	if strings.Contains(stderr, broken) || strings.Contains(stderr, "40s") {
		t.Fatalf("the diagnostic echoed the path or the document: %q", stderr)
	}
}

func TestObserveExplainReportsWhatACompletedWindowSupports(t *testing.T) {
	directory, window := observeDocuments(t)
	path := writeObserveCompletion(t, directory, "complete.json", "complete", observedSamples, "3", "2s")
	stdout, stderr, err := run(t, "observe", "explain", path, "--window", window)
	if err != nil {
		t.Fatalf("a completed window was refused: %v %s", err, stderr)
	}
	for _, expected := range []string{"Status: complete", "Boundary: observation-window",
		"Records observed: 0", "Absence assertion: supported by this completion",
		"Durable run state: none; a completed window leaves the verdict to the assertion"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("explanation did not report %q:\n%s", expected, stdout)
		}
	}
}

// The command surface of the one rule this contract exists for: collection that
// failed observed no records, and reports that it cannot show there are none.
func TestObserveExplainNeverReadsFailedCollectionAsAbsence(t *testing.T) {
	directory, window := observeDocuments(t)
	for name, status := range map[string]string{
		"a collector that never ran":   "missing",
		"a lost connection":            "failed",
		"a truncated capture":          "truncated",
		"an ambiguous source status":   "ambiguous",
		"data from before the window":  "stale",
		"a source that is unsupported": "unsupported",
		"a deadline that passed":       "incomplete",
	} {
		path := writeObserveCompletion(t, directory, status+".json", status, "", "0", "0s")
		stdout, stderr, err := run(t, "observe", "explain", path)
		if code := exitCode(t, err); code != 2 {
			t.Fatalf("%s exited %d, want the observation error status 2", name, code)
		}
		if !strings.Contains(stdout, "Status: "+status) {
			t.Fatalf("%s did not report its status:\n%s", name, stdout)
		}
		if !strings.Contains(stdout, "Absence assertion: not supported") {
			t.Fatalf("%s was read as an absence assertion:\n%s", name, stdout)
		}
		if strings.Contains(stdout, "Absence assertion: supported") {
			t.Fatalf("%s claimed support for an absence assertion:\n%s", name, stdout)
		}
		if !strings.Contains(stdout, "Records observed: not settled") {
			t.Fatalf("%s reported a settled count:\n%s", name, stdout)
		}
		// The window explains itself in the durable run's own vocabulary, and
		// never as a pass or an assertion failure.
		if !strings.Contains(stdout, "Durable run state: "+runStateOf(status)) {
			t.Fatalf("%s did not report its durable run state:\n%s", name, stdout)
		}
		if stderr != "" {
			t.Fatalf("%s interleaved a diagnostic with its output: %q", name, stderr)
		}
		// The same untrustworthy record is untrustworthy against the window it
		// names, so re-deciding it changes nothing.
		if _, _, err := run(t, "observe", "explain", path, "--window", window); exitCode(t, err) != 2 {
			t.Fatalf("%s passed once its window was supplied", name)
		}
	}
}

func TestObserveExplainRefusesACompletionFromAnotherWindow(t *testing.T) {
	directory, _ := observeDocuments(t)
	path := writeObserveCompletion(t, directory, "complete.json", "complete", observedSamples, "3", "2s")
	other := filepath.Join(directory, "other-window.json")
	document := strings.Replace(observeWindowDocument, `"scope": "appointments"`, `"scope": "orders"`, 1)
	if err := os.WriteFile(other, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "observe", "explain", path, "--window", other)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("an unrelated completion exited %d, want 2", code)
	}
	if stdout != "" {
		t.Fatalf("an unrelated completion produced command output: %q", stdout)
	}
	if !strings.Contains(stderr, "different observation window") {
		t.Fatalf("the refusal did not say the completion is unrelated: %q", stderr)
	}

	// A verdict its own samples do not support is refused the same way.
	forged := writeObserveCompletion(t, directory, "forged.json", "complete", observedSamples, "3", "30s")
	if _, stderr, err := run(t, "observe", "explain", forged, "--window", filepath.Join(directory, "window.json")); exitCode(t, err) != 2 {
		t.Fatalf("a verdict its samples do not support was accepted: %q", stderr)
	}
}

// Machine-readable mode keeps the record on stdout and the diagnostic on
// stderr, so a caller parsing one never has to strip the other.
func TestObserveExplainSeparatesRecordFromDiagnostic(t *testing.T) {
	directory, _ := observeDocuments(t)
	path := writeObserveCompletion(t, directory, "failed.json", "failed", "", "0", "0s")
	stdout, stderr, err := run(t, "observe", "explain", path, "--json")
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("a failed collection exited %d, want 2", code)
	}
	record, decodeErr := observewindow.DecodeCompletion([]byte(stdout))
	if decodeErr != nil {
		t.Fatalf("the record the command wrote does not read back: %v", decodeErr)
	}
	if record.Status != observewindow.Failed || record.Trustworthy() || record.AbsenceEvidence() == nil {
		t.Fatalf("the record was written as evidence: %+v", record)
	}
	if !strings.Contains(stderr, "collection itself failed") {
		t.Fatalf("the diagnostic did not name the failure: %q", stderr)
	}
}

func TestObserveRefusesArgumentsItCannotRead(t *testing.T) {
	directory, window := observeDocuments(t)
	for name, args := range map[string][]string{
		"no window":           {"observe", "validate"},
		"two windows":         {"observe", "validate", window, window},
		"a missing window":    {"observe", "validate", filepath.Join(directory, "absent.json")},
		"a directory":         {"observe", "validate", directory},
		"no completion":       {"observe", "explain"},
		"a missing record":    {"observe", "explain", filepath.Join(directory, "absent.json")},
		"a record that isn't": {"observe", "explain", window},
	} {
		if _, _, err := run(t, args...); exitCode(t, err) == 0 {
			t.Fatalf("observe accepted %s", name)
		}
	}
}

// runStateOf names the durable-run state the command must report for a window
// status. Only an interruption keeps its own name; every other failure is an
// execution error, and none of them is ever a pass.
func runStateOf(status string) string {
	switch status {
	case "cancelled", "timed_out", "interrupted":
		return status
	}
	return "execution_error"
}

// Regression: a declared window has observed nothing, so validating one must
// never report a count of what was already present. Reporting "0 records
// already present" for a window that never looked is the zero-from-nothing
// conflation these contracts exist to prevent.
func TestObserveValidateNeverReportsCountsAWindowNeverObserved(t *testing.T) {
	directory, _ := observeDocuments(t)
	path := filepath.Join(directory, "baseline-window.json")
	document := strings.Replace(observeWindowDocument,
		`"pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""}`,
		`"pre_existing_state": {"declaration": "recorded-baseline", "baseline_identity": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}`, 1)
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := run(t, "observe", "validate", path)
	if err != nil {
		t.Fatalf("a window declaring a recorded baseline was refused: %v %s", err, stderr)
	}
	if !strings.Contains(stdout, "Pre-existing state: recorded-baseline, an observation of the source is required before the window opens") {
		t.Fatalf("validation did not say what the declaration requires:\n%s", stdout)
	}
	if strings.Contains(stdout, "records already present") {
		t.Fatalf("validating a window reported a count it never observed:\n%s", stdout)
	}
}

// A completion says what it actually retained about the state before the
// window: an observed baseline reports its count, and one that was never taken
// or that failed says so instead of reporting the zero it never counted.
func TestObserveExplainDistinguishesAnUnobservedBaselineFromAnEmptyOne(t *testing.T) {
	directory, _ := observeDocuments(t)
	for name, testCase := range map[string]struct {
		baseline string
		status   string
		expected string
	}{
		"a baseline that was never taken": {"null", "missing", "recorded-baseline, no baseline was ever taken"},
		"a baseline whose collection failed": {
			`{"at": "2026-01-03T10:59:59Z", "status": "failed", "record_count": 0, "state_digest": "", "evidence_identity": ""}`,
			"failed", "recorded-baseline, the baseline's own collection reported failed"},
	} {
		document := strings.Replace(strings.Replace(
			observeDocument(t, testCase.status, "", "0", "0s"),
			`"baseline": null`, `"baseline": `+testCase.baseline, 1),
			`"pre_existing_basis": "declared-empty"`, `"pre_existing_basis": "recorded-baseline"`, 1)
		path := filepath.Join(directory, testCase.status+"-baseline.json")
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		stdout, _, err := run(t, "observe", "explain", path)
		if exitCode(t, err) != 2 {
			t.Fatalf("%s did not exit with the observation error status", name)
		}
		if !strings.Contains(stdout, "Pre-existing state: "+testCase.expected) {
			t.Fatalf("%s was not distinguished from an observed empty baseline:\n%s", name, stdout)
		}
		if strings.Contains(stdout, "0 records already present") {
			t.Fatalf("%s reported a count nobody measured:\n%s", name, stdout)
		}
	}
}
