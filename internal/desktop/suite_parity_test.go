package desktop_test

// The suite operations are the same operations `readmit suite` runs, not a
// second implementation. These tests hold the window to byte-for-byte
// equality with the command-line path on the same inputs: a prepared
// directory, a promotion review identity and a coverage assessment.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/suite"
)

// The private configuration the window retains is the configuration the
// command line retains: same files, same deterministic bytes. The compiled
// specifications name absolute paths into their own workspace, so those and
// only those are compared with their root replaced.
func TestPrepareSuiteMatchesTheCommandLinePreparation(t *testing.T) {
	app, root := suiteApp(t)
	fromWindow := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Output: "window"})
	if fromWindow.State != desktop.Completed {
		t.Fatalf("%+v", fromWindow)
	}
	other := t.TempDir()
	other, _ = filepath.EvalSymlinks(other)
	writeSuiteFixture(t, other)
	if _, err := suite.Prepare(filepath.Join(other, "suite.json"), "east", filepath.Join(other, "engine")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"suite.json", "selection.json", "queue.json"} {
		window, err := os.ReadFile(filepath.Join(root, "window", name))
		if err != nil {
			t.Fatal(err)
		}
		engine, err := os.ReadFile(filepath.Join(other, "engine", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(window) != string(engine) {
			t.Fatalf("%s differs between the window and the engine", name)
		}
	}
	windowSpec := read(t, filepath.Join(root, "window", "booking-one.json"))
	engineSpec := read(t, filepath.Join(other, "engine", "booking-one.json"))
	if replaceAll(t, windowSpec, root, "ROOT") != replaceAll(t, engineSpec, other, "ROOT") {
		t.Fatal("compiled specification differs beyond its workspace root")
	}
}

// A promotion review the window shows is the review the command line reports.
func TestPromotionReviewMatchesTheCommandLineReview(t *testing.T) {
	app, root := releaseWorkspace(t)
	request := desktop.SuitePromotionRequest{Workspace: root, Entry: "suite.json", Environment: "east", Releases: "releases.json", Revision: "fixture-build-7"}
	fromWindow := app.ReviewSuitePromotion(request)
	if fromWindow.State != desktop.Completed || fromWindow.Review == nil {
		t.Fatalf("%+v", fromWindow)
	}
	fromEngine, err := suite.ReviewPromotion(filepath.Join(root, "suite.json"), "east", filepath.Join(root, "releases.json"), "fixture-build-7")
	if err != nil {
		t.Fatal(err)
	}
	if fromWindow.Review.Identity() != fromEngine.Identity() || len(fromWindow.Review.Jobs) != len(fromEngine.Jobs) {
		t.Fatalf("window review %+v differs from engine review %+v", fromWindow.Review, fromEngine)
	}
}

// Cancelling during a coverage assessment either stops it with the cancelled
// state or does not, but never corrupts a result, and the facade stays usable.
func TestCoverageAssessmentSurvivesConcurrentCancellation(t *testing.T) {
	app, root := suiteApp(t)
	prepared := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Output: "prepared"})
	if prepared.State != desktop.Completed {
		t.Fatalf("%+v", prepared)
	}
	authored := app.SaveSuiteCoverage(desktop.SuiteCoverageSaveRequest{
		Workspace: root, Prepared: "prepared",
		Requirements: []suite.Requirement{{ID: "accept", Jobs: []string{"booking-one"}}},
		Output:       "coverage.json",
	})
	if authored.State != desktop.Completed {
		t.Fatalf("%+v", authored)
	}
	var cancelling sync.WaitGroup
	cancelling.Add(1)
	go func() {
		defer cancelling.Done()
		for range 32 {
			// The window's own cancel command names no operation and cancels
			// whatever is running; the panel's named cancel is exercised by the
			// frontend journey.
			app.Cancel("")
		}
	}()
	result := app.AssessSuiteCoverage(desktop.SuiteCoverageAssessRequest{Workspace: root, Prepared: "prepared", Requirements: "coverage.json", At: time.Now().UTC().Format(time.RFC3339)})
	cancelling.Wait()
	if result.State != desktop.Completed && result.State != desktop.Cancelled {
		t.Fatalf("cancelling turned an assessment into %+v", result)
	}
	if again := app.AssessSuiteCoverage(desktop.SuiteCoverageAssessRequest{Workspace: root, Prepared: "prepared", Requirements: "coverage.json"}); again.State != desktop.Completed {
		t.Fatalf("the facade stayed unusable after cancellation: %+v", again)
	}
}

// The assessment names itself, so the panel's cancel control can cancel it
// and another panel's cancel cannot.
func TestCoverageAssessmentCancellationNamesItself(t *testing.T) {
	app, root := suiteApp(t)
	prepared := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Output: "prepared"})
	if prepared.State != desktop.Completed {
		t.Fatalf("%+v", prepared)
	}
	authored := app.SaveSuiteCoverage(desktop.SuiteCoverageSaveRequest{
		Workspace:    root,
		Prepared:     "prepared",
		Requirements: []suite.Requirement{{ID: "accept", Jobs: []string{"booking-one"}}},
		Output:       "coverage.json",
	})
	if authored.State != desktop.Completed {
		t.Fatalf("%+v", authored)
	}
	app.Cancel("another-panel-operation")
	if result := app.AssessSuiteCoverage(desktop.SuiteCoverageAssessRequest{Workspace: root, Prepared: "prepared", Requirements: "coverage.json"}); result.State != desktop.Completed {
		t.Fatalf("a cancel naming another operation stopped this assessment: %+v", result)
	}
}

// replaceAll substitutes every occurrence of from in text, failing the test
// when the fixture string is not present at all.
func replaceAll(t *testing.T, text, from, to string) string {
	t.Helper()
	if indexOf(text, from) < 0 {
		t.Fatal("fixture string not found")
	}
	for indexOf(text, from) >= 0 {
		text = replaceOnce(t, text, from, to)
	}
	return text
}
