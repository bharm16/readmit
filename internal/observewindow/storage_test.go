package observewindow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/observewindow"
)

// retainedCompletion is a hand-authored record. It is written out as text so
// the reader is exercised against a document a collector could have retained
// rather than against whatever the encoder happens to produce.
const retainedCompletion = `{
  "schema": "readmit-observation-completion/v1",
  "boundary": "observation-window",
  "window_identity": "WINDOW",
  "source": {"kind": "downstream-capture", "identity": "scheduling-archive", "scope": "appointments"},
  "watermark": {"kind": "declared-position", "position": "2026-01-03T11:00:00Z"},
  "status": "complete",
  "stop": "none",
  "opened_at": "2026-01-03T11:00:00Z",
  "closed_at": "2026-01-03T11:00:03Z",
  "baseline": null,
  "samples": [
    {"at": "2026-01-03T11:00:01Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"},
    {"at": "2026-01-03T11:00:02Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"},
    {"at": "2026-01-03T11:00:03Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"}
  ],
  "correlations": [{"produced": "s0001-e000001", "observed": "", "kind": "unmatched"}],
  "stable_samples": 3,
  "quiet_period": "2s",
  "records_observed": 0,
  "pre_existing_basis": "declared-empty"
}`

func authoredCompletion(t *testing.T) string {
	t.Helper()
	return strings.Replace(retainedCompletion, "WINDOW", mustDecodeWindow(t, declaredWindow).Identity(), 1)
}

func TestRetainedCompletionReadsBackAndIsVerifiedAgainstItsWindow(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	record, err := observewindow.DecodeCompletion([]byte(authoredCompletion(t)))
	if err != nil {
		t.Fatalf("an independently authored completion was refused: %v", err)
	}
	if err := window.Verify(record); err != nil {
		t.Fatalf("a completion its own evidence supports was refused: %v", err)
	}
	if !record.Trustworthy() || record.AbsenceEvidence() != nil {
		t.Fatalf("a completed record was not read as evidence: %+v", record)
	}
	// What this package writes is what this package reads.
	data, err := observewindow.EncodeCompletion(record)
	if err != nil {
		t.Fatal(err)
	}
	reread, err := observewindow.DecodeCompletion(data)
	if err != nil || reread.Status != record.Status || len(reread.Samples) != len(record.Samples) {
		t.Fatalf("a retained completion did not survive its own encoding: %v", err)
	}
}

// Regression: a completion that claims a recorded baseline must retain the
// observation of it. Re-deciding a record may not manufacture the very
// evidence it exists to check.
func TestCompletionClaimingABaselineMustRetainIt(t *testing.T) {
	window := baselineWindow(t)
	document := strings.Replace(strings.Replace(retainedCompletion, "WINDOW", window.Identity(), 1),
		`"pre_existing_basis": "declared-empty"`, `"pre_existing_basis": "recorded-baseline"`, 1)
	record, err := observewindow.DecodeCompletion([]byte(document))
	if err != nil {
		return // Refused by the reader, which is the same refusal.
	}
	if err := window.Verify(record); err == nil {
		t.Fatal("a completion claiming a baseline it never retained was verified")
	}
}

// Verify re-decides from the evidence a record retained, so a window that
// honestly failed on its baseline is reproduced rather than called
// inconsistent. Each record below is written by the engine, reopened through
// the reader, and re-decided against the window that produced it.
func TestVerifyReproducesEveryBaselineOutcome(t *testing.T) {
	window := baselineWindow(t)
	for name, collection := range map[string]observewindow.Collection{
		"an observed baseline": func() observewindow.Collection {
			c := settled(3, oneBooking)
			c.Baseline = takenBaseline(2)
			return c
		}(),
		"a baseline that was never taken": settled(3, oneBooking),
		"a baseline whose collection failed": func() observewindow.Collection {
			c := settled(3, oneBooking)
			c.Baseline = &observewindow.Sample{At: at(-1), Status: observewindow.SampleFailed}
			return c
		}(),
		"a baseline larger than what was observed": func() observewindow.Collection {
			c := settled(1, oneBooking)
			c.Baseline = takenBaseline(2)
			return c
		}(),
	} {
		record := complete(t, window, collection)
		data, err := observewindow.EncodeCompletion(record)
		if err != nil {
			t.Fatalf("%s produced a record that cannot be retained: %v", name, err)
		}
		reopened, err := observewindow.DecodeCompletion(data)
		if err != nil {
			t.Fatalf("%s produced a record that does not read back: %v", name, err)
		}
		if reopened.Status != record.Status {
			t.Fatalf("%s changed status on the way to disk: %q became %q", name, record.Status, reopened.Status)
		}
		if err := window.Verify(reopened); err != nil {
			t.Fatalf("%s was called inconsistent by the window that produced it: %v", name, err)
		}
	}
}

// A record is evidence of what was observed. It is not permission to skip
// re-deciding what those observations mean.
func TestRetainedCompletionMustSupportItsOwnVerdict(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	authored := authoredCompletion(t)
	for name, document := range map[string]string{
		"a verdict its samples never reached": strings.Replace(authored,
			`"stable_samples": 3`, `"stable_samples": 1`, 1),
		"a count it never settled on": strings.Replace(authored,
			`"records_observed": 0`, `"records_observed": 7`, 1),
		"a quiet period longer than it observed": strings.Replace(authored,
			`"quiet_period": "2s"`, `"quiet_period": "30s"`, 1),
	} {
		record, err := observewindow.DecodeCompletion([]byte(document))
		if err != nil {
			continue // Refused by the reader, which is the same refusal.
		}
		if err := window.Verify(record); err == nil {
			t.Fatalf("a completion recording %s was verified", name)
		}
	}

	// A completion from some other window is unrelated evidence, never this
	// window's answer.
	other := mustDecodeWindow(t, strings.Replace(declaredWindow, `"scope": "appointments"`, `"scope": "orders"`, 1))
	record, err := observewindow.DecodeCompletion([]byte(authored))
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Verify(record); err == nil || !strings.Contains(err.Error(), "different observation window") {
		t.Fatalf("a completion was verified against a window it does not belong to: %v", err)
	}
}

func TestCompletionRecordRefusesWhatItCannotHaveObserved(t *testing.T) {
	authored := authoredCompletion(t)
	observation := `{"at": "2026-01-03T11:00:01Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"}`
	for name, document := range map[string]string{
		"a later contract":       strings.Replace(authored, "completion/v1", "completion/v2", 1),
		"another boundary":       strings.Replace(authored, `"boundary": "observation-window"`, `"boundary": "appointment-ledger"`, 1),
		"an unknown member":      strings.Replace(authored, `"stable_samples": 3`, `"collector_says_fine": true, "stable_samples": 3`, 1),
		"an omitted stop":        strings.Replace(authored, `"stop": "none",`, "", 1),
		"an omitted baseline":    strings.Replace(authored, `"baseline": null,`, "", 1),
		"omitted correlations":   strings.Replace(authored, `"correlations": [{"produced": "s0001-e000001", "observed": "", "kind": "unmatched"}],`, "", 1),
		"an unknown status":      strings.Replace(authored, `"status": "complete"`, `"status": "probably_fine"`, 1),
		"an unknown stop":        strings.Replace(authored, `"stop": "none"`, `"stop": "gave_up"`, 1),
		"closing before opening": strings.Replace(authored, `"closed_at": "2026-01-03T11:00:03Z"`, `"closed_at": "2026-01-02T11:00:03Z"`, 1),
		"an observation with no retained evidence": strings.Replace(authored, observation,
			`{"at": "2026-01-03T11:00:01Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": ""}`, 1),
		"a digest on a failure": strings.Replace(authored, observation,
			`{"at": "2026-01-03T11:00:01Z", "status": "failed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": ""}`, 1),
		"a baseline on a window that declared none": strings.Replace(authored, `"baseline": null`,
			`"baseline": {"at": "2026-01-03T10:59:59Z", "status": "observed", "record_count": 0, "state_digest": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "evidence_identity": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"}`, 1),
		"a correlation naming an unmatched record": strings.Replace(authored,
			`{"produced": "s0001-e000001", "observed": "", "kind": "unmatched"}`,
			`{"produced": "s0001-e000001", "observed": "appointment-7", "kind": "unmatched"}`, 1),
		"an unknown correlation kind": strings.Replace(authored, `"kind": "unmatched"`, `"kind": "probably"`, 1),
		"a settled count on a failed window": strings.Replace(strings.Replace(authored,
			`"status": "complete"`, `"status": "failed"`, 1), `"records_observed": 0`, `"records_observed": 3`, 1),
		"a window identity that is not one": strings.Replace(authored,
			mustDecodeWindow(t, declaredWindow).Identity(), "not-a-digest", 1),
	} {
		if _, err := observewindow.DecodeCompletion([]byte(document)); err == nil {
			t.Fatalf("a completion recording %s was accepted", name)
		}
	}
	if _, err := observewindow.DecodeCompletion([]byte(strings.Repeat("x", observewindow.MaxCompletionBytes+1))); err == nil {
		t.Fatal("an oversized completion was accepted")
	}
}

func TestCompletionIsRetainedAtANewDestinationOnly(t *testing.T) {
	directory := t.TempDir()
	record, err := observewindow.DecodeCompletion([]byte(authoredCompletion(t)))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "completion.json")
	if err := observewindow.WriteCompletion(path, record); err != nil {
		t.Fatalf("a completion could not be retained: %v", err)
	}
	// A verdict is evidence, so a second run writes a second record beside the
	// first rather than replacing it.
	if err := observewindow.WriteCompletion(path, record); err == nil {
		t.Fatal("a retained completion was overwritten")
	}
	reopened, err := observewindow.ReadCompletion(path)
	if err != nil {
		t.Fatalf("a retained completion could not be reopened: %v", err)
	}
	if reopened.WindowIdentity != record.WindowIdentity || reopened.Status != record.Status {
		t.Fatalf("a retained completion changed on the way to disk: %+v", reopened)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("a retained completion is readable beyond its owner: %v", info.Mode())
	}
	// Retained evidence is not a place to write a new verdict into.
	evidence := filepath.Join(directory, "result")
	if err := os.Mkdir(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte(strings.Repeat("a", 64)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := observewindow.WriteCompletion(filepath.Join(evidence, "completion.json"), record); err == nil {
		t.Fatal("a completion was written inside retained evidence")
	}
	for _, name := range []string{"", ".", ".."} {
		if err := observewindow.WriteCompletion(filepath.Join(directory, name), record); err == nil {
			t.Fatalf("a completion was written to %q", name)
		}
	}
}

func TestObservationDocumentsMustBeRegularFiles(t *testing.T) {
	directory := t.TempDir()
	if _, err := observewindow.ReadWindow(directory); err == nil {
		t.Fatal("a directory was read as an observation window")
	}
	if _, err := observewindow.ReadCompletion(directory); err == nil {
		t.Fatal("a directory was read as an observation completion")
	}
	if _, err := observewindow.ReadWindow(filepath.Join(directory, "absent.json")); err == nil {
		t.Fatal("a missing file was read as an observation window")
	}
	path := filepath.Join(directory, "window.json")
	if err := os.WriteFile(path, []byte(declaredWindow), 0o600); err != nil {
		t.Fatal(err)
	}
	window, err := observewindow.ReadWindow(path)
	if err != nil {
		t.Fatalf("a declared window on disk was refused: %v", err)
	}
	if window.Identity() != mustDecodeWindow(t, declaredWindow).Identity() {
		t.Fatal("a window read from disk is not the window that was written")
	}
}
