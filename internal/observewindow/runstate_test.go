package observewindow_test

import (
	"testing"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/testrunner"
)

// every status this contract defines. A status added without a run state would
// fall through to execution_error, which is the safe direction; this list keeps
// the check honest about what it covered.
var everyStatus = []observewindow.Status{
	observewindow.Complete, observewindow.Incomplete, observewindow.Missing,
	observewindow.Ambiguous, observewindow.Stale, observewindow.Truncated,
	observewindow.Unsupported, observewindow.Failed, observewindow.Cancelled,
	observewindow.TimedOut, observewindow.Interrupted,
}

// The composition claim in this package's doc comment is a property, not
// prose. This test is what makes it one: the strings the contract produces are
// pinned here against the durable run's and the test runner's own constants,
// which neither package may import back without creating the dependency cycle
// the contract exists to avoid.
func TestWindowStatusesComposeWithTheDurableRunVocabulary(t *testing.T) {
	for status, want := range map[observewindow.Status]durablerun.State{
		observewindow.Cancelled:   durablerun.Cancelled,
		observewindow.TimedOut:    durablerun.TimedOut,
		observewindow.Interrupted: durablerun.Interrupted,
	} {
		if got := status.RunState(); got != string(want) {
			t.Fatalf("window status %q produced run state %q, want the durable run's %q", status, got, want)
		}
	}
	// A completed window is trustworthy evidence, not a verdict about the run.
	if got := observewindow.Complete.RunState(); got != "" {
		t.Fatalf("a completed window decided the run state %q", got)
	}
	for _, status := range []observewindow.Status{
		observewindow.Incomplete, observewindow.Missing, observewindow.Ambiguous,
		observewindow.Stale, observewindow.Truncated, observewindow.Unsupported, observewindow.Failed,
	} {
		got := status.RunState()
		if got != string(durablerun.ExecutionError) {
			t.Fatalf("window status %q produced run state %q, want the durable run's %q", status, got, durablerun.ExecutionError)
		}
		if got != string(testrunner.ExecutionError) {
			t.Fatalf("window status %q produced run state %q, want the test runner's %q", status, got, testrunner.ExecutionError)
		}
	}
	// A status nobody recognized is an execution error, not a silent pass.
	if got := observewindow.Status("probably_fine").RunState(); got != string(durablerun.ExecutionError) {
		t.Fatalf("an unrecognized window status produced run state %q", got)
	}
}

// Unknown and unsupported are not pass, and a timeout is not a negative
// application result. No window status may produce either verdict in either
// vocabulary, whatever the source did.
func TestNoWindowStatusEverProducesAPassOrAnAssertionFailure(t *testing.T) {
	forbidden := []string{
		string(durablerun.Passed), string(durablerun.AssertionFailed),
		string(testrunner.Pass), string(testrunner.AssertionFailure),
	}
	for _, status := range append(everyStatus, observewindow.Status("probably_fine")) {
		for _, verdict := range forbidden {
			if status.RunState() == verdict {
				t.Fatalf("window status %q produced the verdict %q", status, verdict)
			}
		}
	}
}

// Reading a source sends nothing, so a window has no analogue of a durable
// run's delivery-uncertain state and must never claim one.
func TestNoWindowStatusClaimsDeliveryUncertainty(t *testing.T) {
	for _, status := range everyStatus {
		if status.RunState() == string(durablerun.DeliveryUncertain) {
			t.Fatalf("window status %q claimed uncertainty about a delivery it never made", status)
		}
	}
}
