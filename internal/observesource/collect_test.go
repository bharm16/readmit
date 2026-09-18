package observesource_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

// The documents below are authored here rather than produced by the engine, so
// the collector is exercised against text an operator could have written.
const declaredWindow = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "5s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 64}
}`

const declaredFileSource = `{
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

const exportHeader = "appointment,status\n"

func write(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// declared writes one source document and one export beside it and reads the
// source back, so every test exercises the same public reader a command uses.
func declared(t *testing.T, document, export string) (observesource.Source, string, string) {
	t.Helper()
	directory := t.TempDir()
	path := write(t, directory, "source.json", document)
	exportPath := write(t, directory, "export.csv", export)
	source, err := observesource.ReadSource(path)
	if err != nil {
		t.Fatal(err)
	}
	return source, exportPath, filepath.Join(t.TempDir(), "snapshot")
}

func window(t *testing.T, document string) observewindow.Window {
	t.Helper()
	declared, err := observewindow.DecodeWindow([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	return declared
}

func collect(t *testing.T, source observesource.Source, declaredWindow observewindow.Window, snapshot string, produced ...string) observewindow.Completion {
	t.Helper()
	completion, err := observesource.Collect(context.Background(), source, declaredWindow, observesource.Options{Snapshot: snapshot, Produced: produced})
	if err != nil {
		t.Fatal(err)
	}
	return completion
}

func TestObservedExportCompletesTheWindowAndNamesWhatItRead(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\nA2,booked\n")
	completion := collect(t, source, window(t, declaredWindow), snapshot)
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	if completion.RecordsObserved != 2 {
		t.Fatalf("records observed = %d, want 2", completion.RecordsObserved)
	}
	if err := completion.AbsenceEvidence(); err != nil {
		t.Fatalf("a completed window supports an absence claim: %v", err)
	}
	for _, sample := range completion.Samples {
		if sample.EvidenceIdentity == "" {
			t.Fatal("an observation names the original material it read")
		}
	}
	// The original material is retained exactly as it was read, so a completed
	// window can be re-examined against the bytes it was decided from.
	body, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "body"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != exportHeader+"A1,booked\nA2,booked\n" {
		t.Fatal("the retained snapshot is not the bytes that were read")
	}
}

func TestObservedEmptyExportIsEvidenceRatherThanAFailure(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader)
	completion := collect(t, source, window(t, declaredWindow), snapshot)
	if err := completion.AbsenceEvidence(); err != nil {
		t.Fatalf("an observed empty export is the one emptiness that is evidence: %v", err)
	}
	if completion.RecordsObserved != 0 {
		t.Fatalf("records observed = %d, want 0", completion.RecordsObserved)
	}
}

// Every case below observed zero records and none of them observed that there
// are none. Each must name its own execution error rather than passing.
func TestFailedCollectionNeverBecomesAPassingAbsenceAssertion(t *testing.T) {
	for _, test := range []struct {
		name    string
		source  string
		export  string
		prepare func(t *testing.T, export string)
		status  observewindow.Status
	}{
		{
			name:   "disabled collector",
			source: strings.Replace(declaredFileSource, `"enabled": true`, `"enabled": false`, 1),
			export: exportHeader + "A1,booked\n",
			status: observewindow.Missing,
		},
		{
			name:    "absent export",
			source:  declaredFileSource,
			export:  exportHeader,
			prepare: func(t *testing.T, export string) { os.Remove(export) },
			status:  observewindow.Missing,
		},
		{
			name:   "truncated export",
			source: strings.Replace(declaredFileSource, `"max_bytes": 65536`, `"max_bytes": 8`, 1),
			export: exportHeader + "A1,booked\n",
			status: observewindow.Truncated,
		},
		{
			name:   "stale export",
			source: strings.Replace(declaredFileSource, `"max_age": "1h"`, `"max_age": "1s"`, 1),
			export: exportHeader + "A1,booked\n",
			prepare: func(t *testing.T, export string) {
				old := time.Now().Add(-time.Hour)
				if err := os.Chtimes(export, old, old); err != nil {
					t.Fatal(err)
				}
			},
			status: observewindow.Stale,
		},
		{
			name:    "export that is not a regular file",
			source:  declaredFileSource,
			export:  exportHeader,
			prepare: func(t *testing.T, export string) { replaceWithDirectory(t, export) },
			status:  observewindow.Failed,
		},
		{
			name:   "export contradicting the declared schema",
			source: declaredFileSource,
			export: "unexpected,columns,here\nA1,booked,extra\n",
			status: observewindow.Ambiguous,
		},
		{
			name:   "export missing the declared record key",
			source: strings.Replace(declaredFileSource, `"record_key": ["appointment"]`, `"record_key": ["absent"]`, 1),
			export: exportHeader + "A1,booked\n",
			status: observewindow.Ambiguous,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, export, snapshot := declared(t, test.source, test.export)
			if test.prepare != nil {
				test.prepare(t, export)
			}
			completion := collect(t, source, window(t, declaredWindow), snapshot)
			if completion.Status != test.status {
				t.Fatalf("status = %q, want %q", completion.Status, test.status)
			}
			if completion.Trustworthy() {
				t.Fatal("failed collection reported itself as trustworthy evidence")
			}
			if err := completion.AbsenceEvidence(); err == nil {
				t.Fatal("failed collection supported an absence assertion")
			}
			if completion.RecordsObserved != 0 {
				t.Fatal("a window that did not complete reported a settled count")
			}
			if completion.Status.RunState() != "execution_error" {
				t.Fatalf("run state = %q, want execution_error", completion.Status.RunState())
			}
		})
	}
}

func TestASourceHoldingMoreThanTheWindowBoundsIsTruncated(t *testing.T) {
	bounded := strings.Replace(declaredWindow, `"max_records": 100`, `"max_records": 1`, 1)
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\nA2,booked\n")
	completion := collect(t, source, window(t, bounded), snapshot)
	if completion.Status != observewindow.Truncated {
		t.Fatalf("status = %q, want truncated", completion.Status)
	}
	if err := completion.AbsenceEvidence(); err == nil {
		t.Fatal("a truncated observation supported an absence assertion")
	}
	// A settled verdict stops the sampling rather than running out the
	// deadline waiting for it to change.
	if len(completion.Samples) != 1 {
		t.Fatalf("took %d samples after a settled verdict, want 1", len(completion.Samples))
	}
}

func TestAWindowOverAnotherSourceIsUnsupportedRatherThanEmpty(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	elsewhere := window(t, strings.Replace(declaredWindow, `"scope": "appointments"`, `"scope": "orders"`, 1))
	completion := collect(t, source, elsewhere, snapshot)
	if completion.Status != observewindow.Unsupported {
		t.Fatalf("status = %q, want unsupported", completion.Status)
	}
	if err := completion.AbsenceEvidence(); err == nil {
		t.Fatal("an unsupported scope supported an absence assertion")
	}
}

func TestADeclaredPositionWatermarkRefusesEvidenceFromBeforeIt(t *testing.T) {
	ahead := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	watermarked := strings.Replace(declaredWindow,
		`"watermark": {"kind": "none", "position": ""}`,
		`"watermark": {"kind": "declared-position", "position": "`+ahead+`"}`, 1)
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	completion := collect(t, source, window(t, watermarked), snapshot)
	if completion.Status != observewindow.Stale {
		t.Fatalf("status = %q, want stale", completion.Status)
	}
}

func TestADeclaredPositionThisCollectorCannotCompareIsUnsupported(t *testing.T) {
	watermarked := strings.Replace(declaredWindow,
		`"watermark": {"kind": "none", "position": ""}`,
		`"watermark": {"kind": "declared-position", "position": "offset-4711"}`, 1)
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	completion := collect(t, source, window(t, watermarked), snapshot)
	if completion.Status != observewindow.Unsupported {
		t.Fatalf("status = %q, want unsupported", completion.Status)
	}
}

func TestCorrelationAnswersOnlyForWhatTheWindowMatched(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\nA2,booked\nA2,moved\n")
	completion := collect(t, source, window(t, declaredWindow), snapshot, "A1", "A2", "A3")
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	if err := completion.Correlated("A1"); err != nil {
		t.Fatalf("A1 was observed exactly once: %v", err)
	}
	for _, produced := range []string{"A2", "A3", "A4"} {
		if err := completion.Correlated(produced); err == nil {
			t.Fatalf("%s was reported as correlated", produced)
		}
	}
}

func TestACancelledWindowIsCancellationRatherThanANegativeResult(t *testing.T) {
	// A quiet period this window can never reach inside its deadline keeps the
	// collector sampling until the caller cancels it.
	unsettled := strings.Replace(declaredWindow, `"quiet_period": "10ms"`, `"quiet_period": "4s"`, 1)
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	completion, err := observesource.Collect(ctx, source, window(t, unsettled), observesource.Options{Snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Status != observewindow.Cancelled {
		t.Fatalf("status = %q, want cancelled", completion.Status)
	}
	if completion.Status.RunState() != "cancelled" {
		t.Fatalf("run state = %q, want cancelled", completion.Status.RunState())
	}
	if completion.RecordsObserved != 0 {
		t.Fatal("a cancelled window reported a settled count")
	}
}

func TestAWindowThatNeverSettlesReportsItsDeadlineRatherThanACount(t *testing.T) {
	brief := strings.Replace(declaredWindow, `"deadline": "5s"`, `"deadline": "30ms"`, 1)
	brief = strings.Replace(brief, `"quiet_period": "10ms"`, `"quiet_period": "25ms"`, 1)
	brief = strings.Replace(brief, `"stable_samples": 2`, `"stable_samples": 8`, 1)
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	completion := collect(t, source, window(t, brief), snapshot)
	if completion.Status != observewindow.Incomplete {
		t.Fatalf("status = %q, want incomplete", completion.Status)
	}
	if completion.RecordsObserved != 0 {
		t.Fatal("an incomplete window reported a settled count")
	}
}

func TestARecordedBaselineIsObservedBeforeTheWindowOpens(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	// The baseline identity is the digest of the state that was already there,
	// which a first collection over the same export reports.
	first := collect(t, source, window(t, declaredWindow), snapshot)
	baselined := strings.Replace(declaredWindow,
		`"pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""}`,
		`"pre_existing_state": {"declaration": "recorded-baseline", "baseline_identity": "`+first.Samples[0].StateDigest+`"}`, 1)
	second, _, again := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	completion := collect(t, second, window(t, baselined), again)
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	if completion.Baseline == nil || completion.Baseline.Status != observewindow.Observed {
		t.Fatal("a completed window over a recorded baseline retains the observation of that baseline")
	}
	produced, err := completion.ProducedInWindow()
	if err != nil || produced != 0 {
		t.Fatalf("produced in window = %d, %v; state already there is not evidence the run produced it", produced, err)
	}
}

func TestABaselineOverAnotherStateIsAmbiguousRatherThanAccepted(t *testing.T) {
	baselined := strings.Replace(declaredWindow,
		`"pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""}`,
		`"pre_existing_state": {"declaration": "recorded-baseline", "baseline_identity": "`+strings.Repeat("a", 64)+`"}`, 1)
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	completion := collect(t, source, window(t, baselined), snapshot)
	if completion.Status != observewindow.Ambiguous {
		t.Fatalf("status = %q, want ambiguous", completion.Status)
	}
}

func TestARetainedCompletionIsReDecidedFromItsOwnSamples(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\n")
	declaredWindow := window(t, declaredWindow)
	completion := collect(t, source, declaredWindow, snapshot)
	if err := declaredWindow.Verify(completion); err != nil {
		t.Fatalf("a collected record does not support its own verdict: %v", err)
	}
	data, err := observewindow.EncodeCompletion(completion)
	if err != nil {
		t.Fatal(err)
	}
	read, err := observewindow.DecodeCompletion(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := declaredWindow.Verify(read); err != nil {
		t.Fatalf("a retained record does not support its own verdict: %v", err)
	}
}

func TestASnapshotDestinationThatAlreadyExistsIsRefused(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader)
	if err := os.Mkdir(snapshot, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := observesource.Collect(context.Background(), source, window(t, declaredWindow), observesource.Options{Snapshot: snapshot})
	if err == nil {
		t.Fatal("an existing snapshot destination was accepted")
	}
}

func TestDuplicateProducedOccurrencesAreRefused(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader)
	_, err := observesource.Collect(context.Background(), source, window(t, declaredWindow),
		observesource.Options{Snapshot: snapshot, Produced: []string{"A1", "A1"}})
	if err == nil {
		t.Fatal("the same produced occurrence was accepted twice")
	}
}

// replaceWithDirectory makes the declared export something that is not a
// regular file, which a collector reports as a failed read rather than reading
// through.
func replaceWithDirectory(t *testing.T, export string) {
	t.Helper()
	if err := os.Remove(export); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(export, 0700); err != nil {
		t.Fatal(err)
	}
}
