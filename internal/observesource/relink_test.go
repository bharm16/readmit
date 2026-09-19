package observesource_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

// The completion record retains a count and a state digest, never a key list,
// and this release does not widen it to hold one. So the keys an observation
// settled on are read again out of the capture and checked against what the
// record does retain. Both halves matter: the keys are what an ordered or a
// naming assertion needs, and the check is what makes them the observation's
// own records rather than a second, later reading of the same directory.
func TestRelinkDerivesTheRecordsACompletedCaptureSettledOn(t *testing.T) {
	source, _, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamBooking, downstreamMove)
	completion := collect(t, source, window(t, capturedWindow), snapshot)
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	keys, err := observesource.Relink(source, completion)
	if err != nil {
		t.Fatalf("a completed capture could not be relinked to its own records: %v", err)
	}
	if !slices.Equal(keys, []string{"APPT-001", "APPT-002"}) {
		t.Fatalf("relinked records = %v, want the capture's own keys in observed order", keys)
	}
	if len(keys) != completion.RecordsObserved {
		t.Fatalf("relinked %d records for a completion that settled on %d", len(keys), completion.RecordsObserved)
	}
}

// An observed empty scope is evidence, and relinking reports it as the empty
// observation it was rather than refusing to answer.
func TestRelinkReportsAnObservedEmptyScopeAsNoRecords(t *testing.T) {
	source, _, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamAck)
	completion := collect(t, source, window(t, capturedWindow), snapshot)
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	keys, err := observesource.Relink(source, completion)
	if err != nil || len(keys) != 0 {
		t.Fatalf("an observed empty scope relinked to %v, %v", keys, err)
	}
}

// A file export and an HTTP API are somebody else's material at a moment that
// has passed. Reporting keys read out of the file as it stands now would
// present a second reading as the reading the observation made, so both are
// refused by name rather than answered with something narrower.
func TestRelinkRefusesASourceWhoseRecordsCannotBeDerivedAgain(t *testing.T) {
	source, _, snapshot := declared(t, declaredFileSource, exportHeader+"A1,booked\nA2,booked\n")
	completion := collect(t, source, window(t, declaredWindow), snapshot)
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	keys, err := observesource.Relink(source, completion)
	if !errors.Is(err, observesource.ErrUnrelinkableSource) {
		t.Fatalf("a file export relinked to %v, %v; want the unrelinkable refusal", keys, err)
	}
}

// Evidence that changed underneath a verdict is unusable, not smaller. A
// capture that no longer holds the records its completion settled on is
// refused rather than reported as a shorter or a different observation.
func TestRelinkRefusesACaptureThatNoLongerHoldsWhatTheCompletionSettledOn(t *testing.T) {
	source, capture, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamBooking, downstreamMove)
	completion := collect(t, source, window(t, capturedWindow), snapshot)
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	// The record retains what the window settled on. A completion claiming a
	// state the capture does not hold is exactly the disagreement this check
	// exists to catch, whichever side of it moved.
	completion.Samples[len(completion.Samples)-1].StateDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	if keys, err := observesource.Relink(source, completion); !errors.Is(err, observesource.ErrRelinkDisagrees) {
		t.Fatalf("a disagreeing capture relinked to %v, %v", keys, err)
	}
	if _, err := os.Stat(filepath.Join(capture, "identity.sha256")); err != nil {
		t.Fatalf("relinking disturbed the capture: %v", err)
	}
}

// Failed collection never becomes a passing absence assertion, and it never
// becomes an empty key list either. An observation that did not complete names
// no records at all.
func TestRelinkRefusesAnObservationThatDidNotComplete(t *testing.T) {
	source, _, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamNoKey)
	completion := collect(t, source, window(t, capturedWindow), snapshot)
	if completion.Trustworthy() {
		t.Fatal("a capture holding an occurrence without the declared key completed")
	}
	if keys, err := observesource.Relink(source, completion); err == nil {
		t.Fatalf("an incomplete observation relinked to %v", keys)
	}
}

// A collector never quietly observes a source other than the one declared, and
// neither does relinking one.
func TestRelinkRefusesASourceThisCompletionDidNotObserve(t *testing.T) {
	source, _, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamBooking)
	completion := collect(t, source, window(t, capturedWindow), snapshot)
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	completion.Source = observewindow.Source{Kind: observesource.DownstreamCapture, Identity: "some-other-system", Scope: "appointments"}
	if keys, err := observesource.Relink(source, completion); err == nil {
		t.Fatalf("a completion of another source relinked to %v", keys)
	}
}
