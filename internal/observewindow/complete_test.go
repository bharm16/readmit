package observewindow_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observewindow"
)

// Hand-authored identities. Nothing here derives them from the code under
// test: a state digest only has to identify a state, and an evidence identity
// only has to name the original material a collector retained.
const (
	emptyState       = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	oneBooking       = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	retainedEvidence = "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"
)

var opened = time.Date(2026, 1, 3, 11, 0, 0, 0, time.UTC)

func at(seconds int) time.Time { return opened.Add(time.Duration(seconds) * time.Second) }

func observed(seconds, count int, digest string) observewindow.Sample {
	return observewindow.Sample{At: at(seconds), Status: observewindow.Observed, RecordCount: count, StateDigest: digest, EvidenceIdentity: retainedEvidence}
}

func failedSample(seconds int, status observewindow.SampleStatus) observewindow.Sample {
	return observewindow.Sample{At: at(seconds), Status: status}
}

// settled is the collection the declared window is written for: the source held
// still across three observations spanning the declared two-second quiet period.
func settled(count int, digest string) observewindow.Collection {
	return observewindow.Collection{
		OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
		Samples: []observewindow.Sample{observed(1, count, digest), observed(2, count, digest), observed(3, count, digest)},
	}
}

func complete(t *testing.T, window observewindow.Window, collection observewindow.Collection) observewindow.Completion {
	t.Helper()
	record, err := window.Decide(collection)
	if err != nil {
		t.Fatalf("a well-formed collection was refused: %v", err)
	}
	return record
}

// baselineWindow declares that a baseline must be observed before the window
// opens, which is the only declaration under which a record can be attributed
// to the run rather than to what was already there.
func baselineWindow(t *testing.T) observewindow.Window {
	t.Helper()
	return mustDecodeWindow(t, strings.Replace(declaredWindow,
		`"pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""}`,
		`"pre_existing_state": {"declaration": "recorded-baseline", "baseline_identity": "`+emptyState+`"}`, 1))
}

func takenBaseline(count int) *observewindow.Sample {
	return &observewindow.Sample{At: at(-1), Status: observewindow.Observed, RecordCount: count, StateDigest: emptyState, EvidenceIdentity: retainedEvidence}
}

// An observed empty state is evidence. It is the only kind of emptiness that
// is, and it is the one path by which an absence assertion may be supported.
func TestObservedEmptyStateCompletesAndSupportsAbsence(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	record := complete(t, window, settled(0, emptyState))
	if record.Status != observewindow.Complete || !record.Trustworthy() {
		t.Fatalf("a settled empty observation did not complete: %+v", record)
	}
	if err := record.AbsenceEvidence(); err != nil {
		t.Fatalf("a completed window did not support an absence assertion: %v", err)
	}
	if record.RecordsObserved != 0 || record.StableSamples != 3 || record.QuietPeriod != "2s" {
		t.Fatalf("a completed window did not record what it settled on: %+v", record)
	}
	if record.Boundary != observewindow.Boundary || record.WindowIdentity != window.Identity() {
		t.Fatalf("a verdict did not name the boundary and window it evaluated: %+v", record)
	}
	produced, err := record.ProducedInWindow()
	if err != nil || produced != 0 {
		t.Fatalf("a declared-empty window did not attribute its records: %d %v", produced, err)
	}
	// A completed window is evidence; it is not a verdict about the run.
	if record.Status.RunState() != "" {
		t.Fatalf("a completed window decided a run state: %q", record.Status.RunState())
	}
}

// The sentence this package exists for. Every collection below observed exactly
// the same number of records as the passing case above — none — and not one of
// them may be read as evidence that there are none.
func TestFailedCollectionNeverBecomesAPassingAbsenceAssertion(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	for name, testCase := range map[string]struct {
		collection observewindow.Collection
		want       observewindow.Status
	}{
		"a collector that never ran": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule},
			want:       observewindow.Missing,
		},
		"a collector that could not read the source": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
				Samples: []observewindow.Sample{failedSample(1, observewindow.SampleMissing)}},
			want: observewindow.Missing,
		},
		"a source that returned data from before the window": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
				Samples: []observewindow.Sample{observed(1, 0, emptyState), failedSample(2, observewindow.SampleStale)}},
			want: observewindow.Stale,
		},
		"a capture a limit cut short": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
				Samples: []observewindow.Sample{failedSample(1, observewindow.SampleTruncated)}},
			want: observewindow.Truncated,
		},
		"a source holding more than the window declares": {
			collection: settled(101, oneBooking),
			want:       observewindow.Truncated,
		},
		"a connection lost mid-collection": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
				Samples: []observewindow.Sample{observed(1, 0, emptyState), observed(2, 0, emptyState), failedSample(3, observewindow.SampleFailed)}},
			want: observewindow.Failed,
		},
		"a source whose status has no single reading": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
				Samples: []observewindow.Sample{failedSample(1, observewindow.SampleAmbiguous)}},
			want: observewindow.Ambiguous,
		},
		"a source kind this collector does not support": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
				Samples: []observewindow.Sample{failedSample(1, observewindow.SampleUnsupported)}},
			want: observewindow.Unsupported,
		},
		"a clock that ran backwards between samples": {
			collection: observewindow.Collection{OpenedAt: opened, ClosedAt: at(4), Stop: observewindow.StopRule,
				Samples: []observewindow.Sample{observed(3, 0, emptyState), observed(1, 0, emptyState), observed(4, 0, emptyState)}},
			want: observewindow.Ambiguous,
		},
	} {
		record := complete(t, window, testCase.collection)
		if record.Status != testCase.want {
			t.Fatalf("%s produced status %q, want %q", name, record.Status, testCase.want)
		}
		if record.Trustworthy() {
			t.Fatalf("%s was trustworthy", name)
		}
		if err := record.Err(); err == nil {
			t.Fatalf("%s produced no execution error", name)
		}
		absence := record.AbsenceEvidence()
		if absence == nil {
			t.Fatalf("%s supported an absence assertion", name)
		}
		if !strings.Contains(absence.Error(), "collection completed") {
			t.Fatalf("%s did not explain what an absence assertion needs: %v", name, absence)
		}
		if record.RecordsObserved != 0 || record.StableSamples != 0 {
			t.Fatalf("%s settled on counts it never observed: %+v", name, record)
		}
		if _, err := record.ProducedInWindow(); err == nil {
			t.Fatalf("%s attributed records to the run", name)
		}
		if state := record.Status.RunState(); state != "execution_error" {
			t.Fatalf("%s produced run state %q, want execution_error", name, state)
		}
	}
}

// Bounded eventually-consistent polling is not "stop at the first convenient
// answer". Each collection below reaches the state a caller might be waiting
// for and still has to hold it for the declared quiet period.
func TestWindowDoesNotStopAtTheFirstConvenientSuccess(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	for name, collection := range map[string]observewindow.Collection{
		"one observation of the wanted state": {
			OpenedAt: opened, ClosedAt: at(1), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{observed(1, 1, oneBooking)},
		},
		"the wanted state that then changed": {
			OpenedAt: opened, ClosedAt: at(4), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{observed(1, 1, oneBooking), observed(2, 1, oneBooking), observed(3, 1, oneBooking), observed(4, 2, emptyState)},
		},
		"enough samples taken too close together": {
			OpenedAt: opened, ClosedAt: at(1), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{observed(0, 1, oneBooking), observed(0, 1, oneBooking), observed(1, 1, oneBooking)},
		},
		"a long enough span with too few samples": {
			OpenedAt: opened, ClosedAt: at(9), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{observed(1, 2, emptyState), observed(9, 2, emptyState)},
		},
		"a state that settled only after the deadline": {
			OpenedAt: opened, ClosedAt: at(40), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{observed(36, 1, oneBooking), observed(38, 1, oneBooking), observed(40, 1, oneBooking)},
		},
	} {
		record := complete(t, window, collection)
		if record.Status != observewindow.Incomplete {
			t.Fatalf("%s produced status %q, want incomplete", name, record.Status)
		}
		if record.AbsenceEvidence() == nil || record.Err() == nil {
			t.Fatalf("%s was read as evidence", name)
		}
	}
}

// Cancellation, a timeout and an interruption each say why sampling stopped.
// None of them says anything about the application, so none may be read as a
// negative result.
func TestInterruptedSamplingIsNotANegativeResult(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	for stop, want := range map[observewindow.Stop]observewindow.Status{
		observewindow.StopCancelled:   observewindow.Cancelled,
		observewindow.StopTimedOut:    observewindow.TimedOut,
		observewindow.StopInterrupted: observewindow.Interrupted,
	} {
		// The samples below would have completed the window on their own, so
		// what is being tested is that an interruption still wins.
		collection := settled(0, emptyState)
		collection.Stop = stop
		record := complete(t, window, collection)
		if record.Status != want {
			t.Fatalf("stopping by %q produced status %q, want %q", stop, record.Status, want)
		}
		if record.Stop != stop || record.Err() == nil || record.AbsenceEvidence() == nil {
			t.Fatalf("stopping by %q was read as evidence: %+v", stop, record)
		}
	}
	// A collection that failed before it was cancelled reports the failure,
	// because that is the earliest point at which it stopped being trustworthy.
	collection := observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopCancelled,
		Samples: []observewindow.Sample{failedSample(1, observewindow.SampleFailed)}}
	if record := complete(t, window, collection); record.Status != observewindow.Failed {
		t.Fatalf("a cancelled collection hid the failure that preceded it: %+v", record)
	}
}

// State that was already there when the window opened is not evidence that this
// run produced it.
func TestPreExistingStateIsNotEvidenceTheRunProducedIt(t *testing.T) {
	window := baselineWindow(t)
	collection := settled(3, oneBooking)
	collection.Baseline = takenBaseline(2)
	record := complete(t, window, collection)
	if record.Status != observewindow.Complete || record.RecordsObserved != 3 || record.Baseline == nil || record.Baseline.RecordCount != 2 {
		t.Fatalf("a recorded baseline was not carried into the verdict: %+v", record)
	}
	produced, err := record.ProducedInWindow()
	if err != nil || produced != 1 {
		t.Fatalf("a window with a baseline attributed %d records, want 1: %v", produced, err)
	}

	// A window declared over an unknown prior state can still say what is
	// present and absent now, and can still never attribute a record to a run.
	unknownWindow := mustDecodeWindow(t, strings.Replace(declaredWindow, `"declaration": "declared-empty"`, `"declaration": "unknown"`, 1))
	unknown := complete(t, unknownWindow, settled(3, oneBooking))
	if unknown.Status != observewindow.Complete || unknown.AbsenceEvidence() != nil {
		t.Fatalf("an unknown prior state blocked a completed window: %+v", unknown)
	}
	if _, err := unknown.ProducedInWindow(); err == nil || !strings.Contains(err.Error(), "declared unknown") {
		t.Fatalf("an unknown prior state attributed records to the run: %v", err)
	}

	// A baseline the window required and did not get is failed collection, and
	// a baseline that itself failed is reported as what it was. Both retain
	// what the collector actually said about it.
	missing := complete(t, window, settled(3, oneBooking))
	if missing.Status != observewindow.Missing || missing.Baseline != nil {
		t.Fatalf("a required baseline that was never taken produced %+v", missing)
	}
	lost := settled(3, oneBooking)
	lost.Baseline = &observewindow.Sample{At: at(-1), Status: observewindow.SampleFailed}
	record = complete(t, window, lost)
	if record.Status != observewindow.Failed || record.Baseline == nil || record.Baseline.Status != observewindow.SampleFailed {
		t.Fatalf("a baseline whose collection failed produced %+v", record)
	}

	// Fewer records than the baseline opened on is a window that cannot be
	// read one way, not a negative count of what the run produced.
	shrunk := settled(1, oneBooking)
	shrunk.Baseline = takenBaseline(2)
	if record := complete(t, window, shrunk); record.Status != observewindow.Ambiguous {
		t.Fatalf("a window that lost records produced %q", record.Status)
	}
}

// A correlation says what the window observed of one thing the run produced.
// Only a match is an answer.
func TestOnlyAMatchedCorrelationAnswersForWhatTheRunProduced(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	collection := settled(1, oneBooking)
	collection.Correlations = []observewindow.Correlation{
		{Produced: "s0001-e000001", Observed: "appointment-7", Kind: observewindow.Matched},
		{Produced: "s0001-e000002", Kind: observewindow.Unmatched},
		{Produced: "s0001-e000003", Kind: observewindow.AmbiguousMatch},
	}
	record := complete(t, window, collection)
	if record.Status != observewindow.Complete || len(record.Correlations) != 3 {
		t.Fatalf("correlations were not carried into the verdict: %+v", record)
	}
	if err := record.Correlated("s0001-e000001"); err != nil {
		t.Fatalf("a matched correlation was not an answer: %v", err)
	}
	for _, produced := range []string{"s0001-e000002", "s0001-e000003", "s0001-e000009"} {
		if err := record.Correlated(produced); err == nil {
			t.Fatalf("%s was answered by a correlation that names nothing", produced)
		}
	}
	// An untrustworthy window answers nothing at all, however it correlates.
	collection.Stop = observewindow.StopCancelled
	if err := complete(t, window, collection).Correlated("s0001-e000001"); err == nil {
		t.Fatal("a cancelled window answered for what the run produced")
	}
}

// A report no collector could honestly have produced is a defect in the caller,
// not an observation about the source, so it is refused rather than recorded.
func TestReportsACollectorCouldNotHaveProducedAreRefused(t *testing.T) {
	window := mustDecodeWindow(t, declaredWindow)
	declared := baselineWindow(t)
	for name, testCase := range map[string]struct {
		window     observewindow.Window
		collection observewindow.Collection
	}{
		"no opening time":        {window, observewindow.Collection{ClosedAt: at(3), Stop: observewindow.StopRule}},
		"closing before opening": {window, observewindow.Collection{OpenedAt: at(3), ClosedAt: opened, Stop: observewindow.StopRule}},
		"an unrecognized stop":   {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: "abandoned"}},
		"an unrecognized status": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule, Samples: []observewindow.Sample{{At: at(1), Status: "probably"}}}},
		"a sample with no time":  {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule, Samples: []observewindow.Sample{{Status: observewindow.Observed, StateDigest: emptyState, EvidenceIdentity: retainedEvidence}}}},
		"an observation with no digest": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{{At: at(1), Status: observewindow.Observed, EvidenceIdentity: retainedEvidence}}}},
		"an observation whose evidence nobody kept": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{{At: at(1), Status: observewindow.Observed, StateDigest: emptyState}}}},
		"a failure that kept evidence it never read": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{{At: at(1), Status: observewindow.SampleFailed, EvidenceIdentity: retainedEvidence}}}},
		"a failure carrying a digest": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{{At: at(1), Status: observewindow.SampleFailed, StateDigest: emptyState}}}},
		"a failure carrying a count": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{{At: at(1), Status: observewindow.SampleFailed, RecordCount: 4}}}},
		"a negative record count": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(3), Stop: observewindow.StopRule,
			Samples: []observewindow.Sample{{At: at(1), Status: observewindow.Observed, RecordCount: -1, StateDigest: emptyState, EvidenceIdentity: retainedEvidence}}}},
		"more samples than the window allows": {window, observewindow.Collection{OpenedAt: opened, ClosedAt: at(20), Stop: observewindow.StopRule,
			Samples: make([]observewindow.Sample, 17)}},
		"a baseline the window never declared": {window, func() observewindow.Collection {
			c := settled(0, emptyState)
			c.Baseline = takenBaseline(0)
			return c
		}()},
		"a baseline taken after the window opened": {declared, func() observewindow.Collection {
			c := settled(3, oneBooking)
			c.Baseline = &observewindow.Sample{At: at(2), Status: observewindow.Observed, RecordCount: 2, StateDigest: emptyState, EvidenceIdentity: retainedEvidence}
			return c
		}()},
		"a baseline with a count but no digest": {declared, func() observewindow.Collection {
			c := settled(3, oneBooking)
			c.Baseline = &observewindow.Sample{At: at(-1), Status: observewindow.Observed, RecordCount: 2, EvidenceIdentity: retainedEvidence}
			return c
		}()},
		"a correlation naming no occurrence": {window, func() observewindow.Collection {
			c := settled(0, emptyState)
			c.Correlations = []observewindow.Correlation{{Kind: observewindow.Unmatched}}
			return c
		}()},
		"an unmatched correlation that names a record": {window, func() observewindow.Collection {
			c := settled(0, emptyState)
			c.Correlations = []observewindow.Correlation{{Produced: "s0001-e000001", Observed: "appointment-7", Kind: observewindow.Unmatched}}
			return c
		}()},
		"a matched correlation that names none": {window, func() observewindow.Collection {
			c := settled(0, emptyState)
			c.Correlations = []observewindow.Correlation{{Produced: "s0001-e000001", Kind: observewindow.Matched}}
			return c
		}()},
		"the same produced occurrence twice": {window, func() observewindow.Collection {
			c := settled(0, emptyState)
			c.Correlations = []observewindow.Correlation{{Produced: "s0001-e000001", Kind: observewindow.Unmatched}, {Produced: "s0001-e000001", Kind: observewindow.Unmatched}}
			return c
		}()},
		"an unrecognized correlation kind": {window, func() observewindow.Collection {
			c := settled(0, emptyState)
			c.Correlations = []observewindow.Correlation{{Produced: "s0001-e000001", Kind: "probably"}}
			return c
		}()},
	} {
		if _, err := testCase.window.Decide(testCase.collection); err == nil {
			t.Fatalf("a collection reporting %s was accepted", name)
		}
	}
	// An invalid window cannot evaluate anything, and what it returns must
	// still fail closed rather than read as a pass.
	invalid := window
	invalid.Completion.MaxRecords = 0
	record, err := invalid.Decide(settled(0, emptyState))
	if err == nil {
		t.Fatal("an invalid window evaluated a collection")
	}
	if record.Trustworthy() || record.Err() == nil || record.AbsenceEvidence() == nil {
		t.Fatalf("a completion that was never evaluated read as evidence: %+v", record)
	}
}

// The zero value is what a caller holds when it never asked, and it must never
// be mistaken for a window that completed.
func TestUnevaluatedCompletionFailsClosed(t *testing.T) {
	var record observewindow.Completion
	if record.Trustworthy() {
		t.Fatal("the zero completion is trustworthy")
	}
	for _, err := range []error{record.Err(), record.AbsenceEvidence(), record.Correlated("s0001-e000001")} {
		if err == nil || !strings.Contains(err.Error(), "no observation window was evaluated") {
			t.Fatalf("the zero completion did not say it was never evaluated: %v", err)
		}
	}
	if _, err := record.ProducedInWindow(); err == nil {
		t.Fatal("the zero completion attributed records to a run")
	}
}
