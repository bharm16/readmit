package observewindow_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observewindow"
)

// FuzzObservationWindowDocument exercises the declared-window reader: the
// strict decode that refuses unknown members and the bounds every declaration
// is held to. No input may panic, and a window the reader accepts must be one
// the completion rule can actually apply, so a document nobody could honour is
// never read as though it had been.
func FuzzObservationWindowDocument(f *testing.F) {
	for _, seed := range []string{
		declaredWindow,
		strings.Replace(declaredWindow, `"declaration": "declared-empty", "baseline_identity": ""`,
			`"declaration": "recorded-baseline", "baseline_identity": "`+emptyState+`"`, 1),
		strings.Replace(declaredWindow, `"kind": "declared-position", "position": "2026-01-03T11:00:00Z"`,
			`"kind": "none", "position": ""`, 1),
		strings.Replace(declaredWindow, `"quiet_period": "2s"`, `"quiet_period": "40s"`, 1),
		strings.Replace(declaredWindow, `"stable_samples": 3`, `"stable_samples": 1`, 1),
		strings.Replace(declaredWindow, "window/v1", "window/v2", 1),
		`{}`,
		`{"schema":"readmit-observation-window/v1"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		window, err := observewindow.DecodeWindow(data)
		if err != nil {
			return
		}
		// An accepted window declares a rule two identical observations can
		// satisfy inside its own deadline, and an identity that names it.
		if window.Completion.StableSamples < 2 || window.Completion.MaxSamples < window.Completion.StableSamples || window.Completion.MaxRecords < 1 {
			t.Fatalf("accepted a window whose rule cannot be applied: %+v", window.Completion)
		}
		if len(window.Identity()) != 64 {
			t.Fatalf("accepted a window with no identity: %+v", window)
		}
		// The canonical form of an accepted window is itself accepted, and
		// names the same window.
		canonical, err := observewindow.EncodeWindow(window)
		if err != nil {
			t.Fatalf("accepted a window that cannot be re-encoded: %v", err)
		}
		again, err := observewindow.DecodeWindow(canonical)
		if err != nil || again.Identity() != window.Identity() {
			t.Fatalf("the canonical form of an accepted window was refused or renamed: %v", err)
		}
		// A window the reader accepted can decide a collection without
		// panicking, and an empty collection is never trustworthy.
		record, err := window.Decide(observewindow.Collection{OpenedAt: opened, ClosedAt: at(1), Stop: observewindow.StopRule})
		if err != nil {
			t.Fatalf("an accepted window refused a well-formed empty collection: %v", err)
		}
		if record.Trustworthy() || record.AbsenceEvidence() == nil {
			t.Fatalf("a collection with no observation supported an absence assertion: %+v", record)
		}
	})
}

// FuzzObservationCompletionRecord exercises the retained-record reader. A
// record it accepts must be internally consistent: a status it does not
// recognize, a settled count on a window that did not complete, and a digest on
// a sample that was not an observation are each refused, and an accepted record
// that is not complete never reads as evidence of absence.
func FuzzObservationCompletionRecord(f *testing.F) {
	identity := strings.Repeat("a", 64)
	authored := strings.Replace(retainedCompletion, "WINDOW", identity, 1)
	for _, seed := range []string{
		authored,
		strings.Replace(strings.Replace(authored, `"status": "complete"`, `"status": "failed"`, 1),
			`"stable_samples": 3`, `"stable_samples": 0`, 1),
		strings.Replace(authored, `"stop": "none"`, `"stop": "cancelled"`, 1),
		strings.Replace(authored, `"pre_existing_basis": "declared-empty"`, `"pre_existing_basis": "unknown"`, 1),
		strings.Replace(authored, `"records_observed": 0`, `"records_observed": 7`, 1),
		strings.Replace(authored, `"baseline": null`, `"baseline": {"at": "2026-01-03T10:59:59Z", "status": "failed", "record_count": 0, "state_digest": "", "evidence_identity": ""}`, 1),
		strings.Replace(authored, `"kind": "unmatched"`, `"kind": "matched"`, 1),
		strings.Replace(authored, "completion/v1", "completion/v2", 1),
		`{}`,
		`{"schema":"readmit-observation-completion/v1","boundary":"observation-window"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		record, err := observewindow.DecodeCompletion(data)
		if err != nil {
			return
		}
		if record.Boundary != observewindow.Boundary || len(record.WindowIdentity) != 64 {
			t.Fatalf("accepted a record that does not name what it evaluated: %+v", record)
		}
		if record.Status != observewindow.Complete {
			if record.Trustworthy() || record.Err() == nil || record.AbsenceEvidence() == nil {
				t.Fatalf("a record that did not complete read as evidence: %+v", record)
			}
			if record.RecordsObserved != 0 || record.StableSamples != 0 {
				t.Fatalf("a record that did not complete carries settled counts: %+v", record)
			}
			if _, err := record.ProducedInWindow(); err == nil {
				t.Fatalf("a record that did not complete attributed records to a run: %+v", record)
			}
		}
		if record.Status.RunState() == "passed" || record.Status.RunState() == "assertion_failed" {
			t.Fatalf("an accepted record produced a verdict about the run: %+v", record)
		}
		if record.PreExistingBasis == observewindow.UnknownPreExisting {
			if _, err := record.ProducedInWindow(); err == nil {
				t.Fatalf("a record over an unknown prior state attributed records to a run: %+v", record)
			}
		}
		if (record.Baseline != nil) && record.PreExistingBasis != observewindow.RecordedBaseline {
			t.Fatalf("an accepted record retains a baseline its basis never declared: %+v", record)
		}
		if _, err := observewindow.EncodeCompletion(record); err != nil {
			t.Fatalf("accepted a record that cannot be re-encoded: %v", err)
		}
	})
}
