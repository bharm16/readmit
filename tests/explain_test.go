package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The expectations below are authored here rather than produced by a command,
// because nothing in this release authors them and an operator would write
// this file by hand. Every identifier is synthetic.
const explainAcceptedSet = `{
  "schema": "readmit-assertion-set/v1",
  "name": "Booking expectations",
  "assertions": [
    {"id": "ack-accepted", "operator": "field_equals",
     "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"}},
     "when": null,
     "expected": {"field": {"state": "present", "text": "AA"}}},
    {"id": "appointment-present", "operator": "field_state",
     "subject": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "SCH-1.1"}},
     "when": null,
     "expected": {"state": "present"}},
    {"id": "sex-omitted", "operator": "field_state",
     "subject": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "PID-8"}},
     "when": null,
     "expected": {"state": "omitted"}}
  ]
}`

// One expectation this run disagrees with, and one the evidence cannot decide.
// The two are different answers and the explanation reports them as such.
const explainDisagreeingSet = `{
  "schema": "readmit-assertion-set/v1",
  "name": "Reschedule expectations",
  "assertions": [
    {"id": "moved-the-appointment", "operator": "field_equals",
     "subject": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "MSH-9.2"}},
     "when": null,
     "expected": {"field": {"state": "present", "text": "S13"}}},
    {"id": "duration-in-range", "operator": "numeric_range",
     "subject": {"field": {"scope": "input", "message": "s0001-e000001", "selector": "PID-5.1"}},
     "when": null,
     "expected": {"range": {"min": "1", "max": "60"}}},
    {"id": "only-when-rejected", "operator": "field_state",
     "subject": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-3"}},
     "when": {"field": {"scope": "observed", "message": "s0001-e000001", "selector": "MSA-1"},
              "equals": {"state": "present", "text": "AR"}},
     "expected": {"state": "present"}}
  ]
}`

// A set asking about the records one observation held. Nothing shipped
// retained that list, so explaining it means deriving the keys again from the
// capture the observation read.
const explainRecordsSet = `{
  "schema": "readmit-assertion-set/v1",
  "name": "Downstream ledger expectations",
  "assertions": [
    {"id": "one-appointment", "operator": "record_count",
     "subject": {"collection": {"scope": "after"}},
     "when": null,
     "expected": {"count": 1}},
    {"id": "names-the-appointment", "operator": "records_contain",
     "subject": {"collection": {"scope": "after"}},
     "when": null,
     "expected": {"keys": ["APPT-7710"]}}
  ]
}`

// explainableRun replays one synthetic booking into the bundled fixture
// receiver and returns the verified run bundle it produced, with a directory
// to write authored documents beside.
func explainableRun(t *testing.T) (runPath, directory string) {
	t.Helper()
	directory = t.TempDir()
	source := filepath.Join(directory, "case")
	if _, stderr, err := run(t, "capture", "../testdata/fixtures/listen-s12.hl7", "--output", source); err != nil {
		t.Fatalf("capture: %v %s", err, stderr)
	}
	receiver := startReceiver(t, 20*time.Second, "listen", "--address", "127.0.0.1:0", "--mode", "fixed",
		"--output", filepath.Join(directory, "recorded"), "--observation", filepath.Join(directory, "observation.json"),
		"--max-messages", "1", "--idle-timeout", "3s")
	runPath = filepath.Join(directory, "run")
	if _, stderr, err := run(t, "replay", source, "--target",
		replayTarget(t, receiver.address),
		"--send", "--output", runPath); err != nil {
		t.Fatalf("replay: %v %s", err, stderr)
	}
	receiver.wait(t)
	return runPath, directory
}

// A recipient with no access to the original developer reads the result, the
// values it was decided on, the timings, the configuration and where every
// value came from — from the console, without opening a JSON file.
func TestExplainExecutableReportsAPassingRunWithoutOpeningAnyJSON(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	stdout, stderr, err := run(t, "explain", bundlePath, "--assertions", writeDocument(t, directory, "assertions.json", explainAcceptedSet))
	if err != nil || stderr != "" {
		t.Fatalf("a passing run was not explained: %v %s", err, stderr)
	}
	for _, expected := range []string{
		"Verdict: pass",
		"Assertions: 3 declared; 3 passed, 0 failed, 0 undecided, 0 skipped",
		"Set contract: readmit-assertion-set/v1",
		"Run contract: readmit-run/v1",
		"Run state: complete",
		"Contains source values: true (customer-local-only)",
		"Explained by readmit ",
		"Target identity: ",
		"Transformations: none",
		"Elapsed: ",
		"s0001-e000001 as o000001: application_accepted",
		"acknowledgement AA matched",
		"ack-accepted: field_equals passed",
		"Reads: MSA-1 of observed message s0001-e000001",
		"sex-omitted: field_state passed",
		"Expected: state omitted",
		"Observed: omitted",
		"Evidence: observed MSA-1 of s0001-e000001: ",
		"payloads/o000001-received.bin",
		"Unknown is a third answer and it is not a pass",
		"sent nothing and wrote nothing",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("the explanation did not report %q:\n%s", expected, stdout)
		}
	}
	// Values are customer-local evidence. The structure of the reasoning is
	// always visible; what was in the message is not, unless it is asked for.
	for _, private := range []string{"APPT-001", "SYNTH-001", "EXAMPLE", "LISTEN-BOOK", "FILL-001"} {
		if strings.Contains(stdout+stderr, private) {
			t.Fatalf("the default explanation disclosed %q:\n%s", private, stdout)
		}
	}
	if !strings.Contains(stdout, "text hidden") {
		t.Fatalf("the explanation did not say a value was hidden:\n%s", stdout)
	}
}

// Asking for values displays both halves of every comparison, so an expected
// and an observed value can be read side by side.
func TestExplainExecutableShowsExpectedAndObservedValuesOnlyWhenAsked(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	stdout, stderr, err := run(t, "explain", bundlePath,
		"--assertions", writeDocument(t, directory, "assertions.json", explainAcceptedSet), "--show-values")
	if err != nil || stderr != "" {
		t.Fatalf("explain --show-values: %v %s", err, stderr)
	}
	for _, expected := range []string{`Expected: present, text "AA"`, `Observed: present, text "AA"`} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("the explanation did not display %q:\n%s", expected, stdout)
		}
	}
	if strings.Contains(stdout, "text hidden") {
		t.Fatalf("values were still hidden after --show-values:\n%s", stdout)
	}
}

// Three answers, three exit codes' worth of meaning. A decided disagreement is
// status 1; an undecided assertion leaves the question open, which is status 2
// and never a pass; and a skipped assertion asserted nothing.
func TestExplainExecutableDistinguishesFailedUndecidedAndSkipped(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	stdout, _, err := run(t, "explain", bundlePath, "--assertions", writeDocument(t, directory, "assertions.json", explainDisagreeingSet))
	if code := exitCode(t, err); code != 1 {
		t.Fatalf("a decided disagreement exited %d, want 1:\n%s", code, stdout)
	}
	for _, expected := range []string{
		"Verdict: fail",
		"Assertions: 3 declared; 0 passed, 1 failed, 1 undecided, 1 skipped",
		"moved-the-appointment: field_equals failed",
		"duration-in-range: numeric_range undecided",
		"only-when-rejected: field_state skipped",
		"Observed: not read; the condition did not hold, so this assertion asserted nothing",
		"Condition: MSA-1 of observed message s0001-e000001 equals present, text hidden",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("the explanation did not report %q:\n%s", expected, stdout)
		}
	}
}

// Evidence the set names and the run does not hold produces no verdict at all.
// The assertion is still listed with what it was asking about.
func TestExplainExecutableProducesNoVerdictForEvidenceItCannotRead(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	missing := `{"schema": "readmit-assertion-set/v1", "name": "Absent evidence",
  "assertions": [{"id": "absent", "operator": "field_state",
    "subject": {"field": {"scope": "observed", "message": "s0001-e000404", "selector": "MSA-1"}},
    "when": null, "expected": {"state": "present"}}]}`
	stdout, _, err := run(t, "explain", bundlePath, "--assertions", writeDocument(t, directory, "assertions.json", missing))
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("an execution error exited %d, want 2:\n%s", code, stdout)
	}
	for _, expected := range []string{
		"Verdict: none, execution error unknown_message",
		"Assertion: absent",
		"Every assertion is left unevaluated",
		"this run retained no readable payload for that occurrence",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("the explanation did not report %q:\n%s", expected, stdout)
		}
	}
}

// The completion record retains a count and a state digest rather than a key
// list, and this release does not widen it. So a collection question is
// answered by deriving the keys again from the capture the observation read
// and checking them against what the record does retain.
func TestExplainExecutableAnswersACollectionQuestionFromTheCaptureThatWasObserved(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	captureDirectory, window, source := captureDocuments(t)
	sealDownstreamCapture(t, captureDirectory)
	record := filepath.Join(captureDirectory, "completion.json")
	if _, stderr, err := run(t, "observe", "collect", source, "--window", window,
		"--out", record, "--snapshot", filepath.Join(captureDirectory, "snapshot")); err != nil {
		t.Fatalf("the downstream capture was not observed: %v %s", err, stderr)
	}
	assertions := writeDocument(t, directory, "records.json", explainRecordsSet)
	stdout, stderr, err := run(t, "explain", bundlePath, "--assertions", assertions, "--after", record, "--after-source", source)
	if err != nil || stderr != "" {
		t.Fatalf("a collection question was not answered: %v %s\n%s", err, stderr, stdout)
	}
	for _, expected := range []string{
		"Verdict: pass",
		"Observation (after): complete",
		"Correlations: none recorded, so nothing in this record binds the observation to this run",
		"Observation contract: readmit-observation-completion/v1, read through readmit-observation-source/v2",
		"Source: kind downstream-capture",
		"Records derived again from: ",
		"retains a count and a state digest rather than a key list",
		"Keys: 1, hidden",
		"one-appointment: record_count passed",
		"Observed: 1 records held",
		"Expected: 1 declared key, hidden",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("the explanation did not report %q:\n%s", expected, stdout)
		}
	}
	if strings.Contains(stdout, "APPT-7710") {
		t.Fatalf("the default explanation disclosed an observed record key:\n%s", stdout)
	}
	shown, _, err := run(t, "explain", bundlePath, "--assertions", assertions, "--after", record, "--after-source", source, "--show-values")
	if err != nil || !strings.Contains(shown, `Keys: "APPT-7710"`) {
		t.Fatalf("--show-values did not display the observed records: %v\n%s", err, shown)
	}
	// A question nobody could answer never becomes an outcome: the same set
	// without the observation is refused before anything is evaluated.
	stdout, stderr, err = run(t, "explain", bundlePath, "--assertions", assertions)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("a collection question with no observation exited %d, want 2:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "asks about the records of the after observation") {
		t.Fatalf("the refusal did not name what was missing: %s", stderr)
	}
}

// A file export and an HTTP API retain no material an ordered key list can be
// re-derived from, and this release says so by name instead of answering with
// something narrower.
func TestExplainExecutableRefusesRecordsItCannotDeriveAgain(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	export := filepath.Join(directory, "export.csv")
	if err := os.WriteFile(export, []byte("appointment,status\nAPPT-7710,booked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fileSource := writeDocument(t, directory, "file-source.json", `{
  "schema": "readmit-observation-source/v1",
  "source": {"kind": "file-export", "identity": "integration-sink", "scope": "appointments"},
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
}`)
	fileWindow := writeDocument(t, directory, "file-window.json", `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "file-export", "identity": "integration-sink", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "5s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 32}
}`)
	record := filepath.Join(directory, "file-completion.json")
	if _, stderr, err := run(t, "observe", "collect", fileSource, "--window", fileWindow,
		"--out", record, "--snapshot", filepath.Join(directory, "file-snapshot")); err != nil {
		t.Fatalf("the export was not observed: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "explain", bundlePath,
		"--assertions", writeDocument(t, directory, "records.json", explainRecordsSet), "--after", record, "--after-source", fileSource)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("an unrelinkable source exited %d, want 2:\n%s", code, stdout)
	}
	if !strings.Contains(stderr, "downstream capture") {
		t.Fatalf("the refusal did not say which evidence can be derived again: %s", stderr)
	}
}

// The occurrences a collection recorded as produced are the only thing in a
// completion that names a run. Every one is checked against what this run
// actually produced, so an observation recorded beside some other run is
// refused rather than reported under this run's identity.
func TestExplainExecutableRefusesAnObservationRecordedBesideAnotherRun(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	captureDirectory, window, source := captureDocuments(t)
	sealDownstreamCapture(t, captureDirectory)
	record := filepath.Join(captureDirectory, "completion.json")
	// The capture holds APPT-7710; the run under explanation booked APPT-001.
	if _, stderr, err := run(t, "observe", "collect", source, "--window", window,
		"--out", record, "--snapshot", filepath.Join(captureDirectory, "snapshot"), "--produced", "APPT-7710"); err != nil {
		t.Fatalf("the downstream capture was not observed: %v %s", err, stderr)
	}
	stdout, stderr, err := run(t, "explain", bundlePath,
		"--assertions", writeDocument(t, directory, "records.json", explainRecordsSet), "--after", record, "--after-source", source)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("an observation of another run exited %d, want 2:\n%s", code, stdout)
	}
	if !strings.Contains(stderr, "this run did not produce") {
		t.Fatalf("the refusal did not say what failed to bind: %s", stderr)
	}
}

// Neither half of the invocation is inferred and neither has a default.
func TestExplainExecutableRequiresOneRunAndOneSet(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	assertions := writeDocument(t, directory, "assertions.json", explainAcceptedSet)
	for name, args := range map[string][]string{
		"no assertion set": {"explain", bundlePath},
		"no run":           {"explain", "--assertions", assertions},
		"two runs":         {"explain", bundlePath, bundlePath, "--assertions", assertions},
	} {
		stdout, _, err := run(t, args...)
		if code := exitCode(t, err); code == 0 {
			t.Fatalf("%s exited %d, want a refusal:\n%s", name, code, stdout)
		}
	}
}

// An authored document past the contract's own bound is refused rather than
// truncated: a prefix of an assertion set is a different set.
func TestExplainExecutableRefusesAnAssertionSetPastItsContractBound(t *testing.T) {
	bundlePath, directory := explainableRun(t)
	oversize := `{"schema": "readmit-assertion-set/v1", "name": "` + strings.Repeat("n", 256<<10) + `", "assertions": []}`
	stdout, stderr, err := run(t, "explain", bundlePath, "--assertions", writeDocument(t, directory, "oversize.json", oversize))
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("an oversize document exited %d, want 2:\n%s", code, stdout)
	}
	if !strings.Contains(stderr, "exceeds the size") {
		t.Fatalf("the refusal did not name the bound: %s", stderr)
	}
}
