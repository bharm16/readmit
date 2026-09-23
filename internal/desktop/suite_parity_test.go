package desktop_test

// The suite operations are the same operations `readmit suite` runs, not a
// second implementation. These tests hold the window to byte-for-byte
// equality with the command-line path on the same inputs: a prepared
// directory, a promotion review identity, a coverage assessment and pasted
// suite text.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
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

// Pasting suite JSON into the window is reading it with the reader the
// command line applies: the typed document is the one `readmit suite` reads
// from the same bytes, the canonical form and identity are what a save of the
// same text writes, and every text the reader refuses is refused with the
// reader's own reason — the reason `readmit suite prepare` reports for the
// same bytes. Validating writes nothing.
func TestValidateSuiteReadsPastedTextWithTheSuiteReader(t *testing.T) {
	app, root := suiteApp(t)
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	refused := map[string]string{
		"malformed":            suiteFixture[:len(suiteFixture)-1],
		"unknown member":       replaceOnce(t, suiteFixture, `"owner":"interop",`, `"owner":"interop","disabled":true,`),
		"unsupported version":  replaceOnce(t, suiteFixture, `"readmit-suite/v1"`, `"readmit-suite/v2"`),
		"invalid declarations": replaceOnce(t, suiteFixture, `"parallelism":2`, `"parallelism":0`),
	}
	validated := app.ValidateSuite(suiteFixture)
	engine, err := suite.Decode([]byte(suiteFixture))
	if err != nil {
		t.Fatal(err)
	}
	if validated.State != desktop.Completed || validated.Suite == nil || string(marshal(t, *validated.Suite)) != string(marshal(t, engine)) {
		t.Fatalf("the window read %+v, the suite reader %+v", validated, engine)
	}
	reasons := map[string]string{}
	for name, text := range refused {
		result := app.ValidateSuite(text)
		_, decodeErr := suite.Decode([]byte(text))
		if decodeErr == nil || result.State != desktop.Failed || result.Suite != nil || result.Document != "" || result.Reason != decodeErr.Error() {
			t.Fatalf("%s: the window answered %+v, the suite reader %v", name, result, decodeErr)
		}
		reasons[name] = result.Reason
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatal("validating pasted text wrote into the workspace")
	}
	saved := app.SaveSuite(desktop.RuleDocumentSaveRequest{Workspace: root, Document: suiteFixture, Output: "pasted.json"})
	if saved.State != desktop.Completed || saved.Document != validated.Document || saved.SHA256 != validated.SHA256 {
		t.Fatalf("a save of the pasted text wrote %+v, validation showed %+v", saved, validated)
	}
	policy := testlicense.New(t)
	for name, text := range refused {
		entry := strings.ReplaceAll(name, " ", "-") + ".json"
		writeDocument(t, root, entry, text)
		_, stderr, err := commandLine(t, "--operation-policy", policy, "suite", "prepare", filepath.Join(root, entry), "--environment", "east", "--output", filepath.Join(root, "prepared-"+entry))
		if err == nil || stderr != "readmit: "+reasons[name]+"\n" {
			t.Fatalf("%s: the command line refused with %q, the window with %q", name, stderr, reasons[name])
		}
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
