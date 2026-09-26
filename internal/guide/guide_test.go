package guide_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/synth"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The independently authored identity of the frozen regression case, quoted
// from docs/synth-v1-vector.md.
const frozenRegressionIdentity = "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df"

// sampleInputs are the frozen readmit-synth-v1 reference vector, which is what
// the sample workspace is generated from.
var sampleInputs = bundle.GeneratorInputs{
	Seed:             0,
	BaseTime:         time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	GeneratorVersion: "readmit-synth-v1",
	ProfileVersion:   "readmit-siu-v1",
}

// sampleWorkspace writes what the window's sample creation writes: the frozen
// synthetic family and the practice endpoint beside it.
func sampleWorkspace(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "readmit-sample")
	if _, err := synth.Write(root, sampleInputs); err != nil {
		t.Fatal(err)
	}
	if err := guide.PrepareWorkspace(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	return root
}

// saveTest authors the guided sample's regression test over the verified case
// and writes it into the workspace, exactly as the authoring flow does.
func saveTest(t *testing.T, root, output string, appointments int) string {
	t.Helper()
	source, err := bundle.Open(filepath.Join(root, guide.CaseName))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := testauthor.NewDraft(guide.CaseName, source.Identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &appointments},
		}},
	} {
		draft, err = draft.Answer(answer)
		if err != nil {
			t.Fatalf("%s: %v", answer.Stage, err)
		}
	}
	saved, err := testauthor.Save(root, source, draft, output)
	if err != nil {
		t.Fatal(err)
	}
	return saved.Output
}

func read(t *testing.T, root string) guide.Progress {
	t.Helper()
	progress, err := guide.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	return progress
}

func step(t *testing.T, progress guide.Progress, id string) guide.Step {
	t.Helper()
	for _, step := range progress.Steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("the guided sample declares no step %q", id)
	return guide.Step{}
}

// The guided sample is read back from the folder every time. Nothing remembers
// a step: each one is complete because the evidence it produces is on disk and
// the reader that owns that evidence accepts it, which is what makes closing the
// window and reopening the folder report the truth about that folder.
func TestEveryStepIsReadBackFromTheWorkspaceAndNothingIsRemembered(t *testing.T) {
	empty := t.TempDir()
	nothing := read(t, empty)
	if nothing.Next != guide.StepSample || nothing.Case != "" || nothing.Spec != "" {
		t.Fatalf("an unrelated folder reported progress: %+v", nothing)
	}
	for _, declared := range nothing.Steps {
		if declared.Done || declared.Title == "" || declared.Detail == "" {
			t.Fatalf("a step of an untouched folder is %+v", declared)
		}
	}

	root := sampleWorkspace(t)
	created := read(t, root)
	if created.Next != guide.StepTest || created.Case != guide.CaseName || created.Identity == "" {
		t.Fatalf("the sample workspace did not complete its own step: %+v", created)
	}

	spec := saveTest(t, root, "reschedule-test.json", 1)
	authored := read(t, root)
	if authored.Next != guide.StepBaseline || authored.Spec != spec {
		t.Fatalf("the saved spec did not complete the authoring step: %+v", authored)
	}

	baseline, err := guide.Run(t.Context(), root, spec, "baseline-run", false)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Result.Status != testrunner.AssertionFailure {
		t.Fatalf("the test did not fail against the defect: %s", baseline.Result.Status)
	}
	failed := read(t, root)
	if failed.Next != guide.StepPostFix {
		t.Fatalf("the failing run did not complete its step: %+v", failed)
	}
	if recorded := step(t, failed, guide.StepBaseline); recorded.Entry != "baseline-run" || recorded.Status != string(testrunner.AssertionFailure) {
		t.Fatalf("the failing run was not named by the step it completed: %+v", recorded)
	}

	fixed, err := guide.Run(t.Context(), root, spec, "post-fix-run", true)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Result.Status != testrunner.Pass {
		t.Fatalf("the same spec did not pass against the corrected fixture: %s", fixed.Result.Status)
	}
	complete := read(t, root)
	if !complete.Complete() {
		t.Fatalf("the guided sample did not complete: %+v", complete)
	}
	// Both runs decided the same bytes: the trials differ by the fixture's
	// behaviour, never by the test.
	if baseline.Result.SpecIdentity != fixed.Result.SpecIdentity {
		t.Fatal("the two practice runs executed different specs")
	}

	// Nothing is remembered: removing the saved spec takes every step that
	// depends on it back to incomplete, although both results are still there.
	if err := os.Remove(filepath.Join(root, spec)); err != nil {
		t.Fatal(err)
	}
	if reread := read(t, root); reread.Next != guide.StepTest || reread.Spec != "" {
		t.Fatalf("progress survived the evidence it was derived from: %+v", reread)
	}
}

// A guided sample nobody can trust is worth nothing, so the two runs must differ
// by the fixture's defect alone. The same spec fails one and passes the other,
// and the failing run names the expectation that was not met.
func TestTheSameSpecFailsAgainstTheDefectAndPassesOnceItIsCorrected(t *testing.T) {
	root := sampleWorkspace(t)
	spec := saveTest(t, root, "reschedule-test.json", 1)

	baseline, err := guide.Run(t.Context(), root, spec, "baseline-run", false)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Result.Status != testrunner.AssertionFailure {
		t.Fatalf("the defective fixture did not fail the test: %s", baseline.Result.Status)
	}
	named := false
	for _, evaluated := range baseline.Result.Assertions {
		if evaluated.Assertion.ID == "one-appointment" && evaluated.Status == "failed" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the failing run did not name the expectation it failed: %+v", baseline.Result.Assertions)
	}
	fixed, err := guide.Run(t.Context(), root, spec, "post-fix-run", true)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Result.Status != testrunner.Pass {
		t.Fatalf("the corrected fixture did not pass the test: %s", fixed.Result.Status)
	}

	// A run is retained evidence a reader accepts on its own, not a claim this
	// package makes: the result reader re-derives the verdict from what was
	// retained, and reports the same one.
	reopened, err := testrunner.Open(filepath.Join(root, "baseline-run", "result"))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Result.Status != testrunner.AssertionFailure || reopened.Identity != baseline.Identity {
		t.Fatalf("the retained failing run reopened as %+v", reopened.Result.Status)
	}
	// The executed copy is the saved spec with three paths rebound and nothing
	// else, and the saved spec in the workspace was not rewritten.
	executed, err := testrunner.ReadSpec(filepath.Join(root, "baseline-run", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := testrunner.ReadSpec(filepath.Join(root, spec))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Input.Case != guide.CaseName || saved.Target != guide.TargetName {
		t.Fatalf("a practice run rewrote the saved spec: %+v", saved)
	}
	executed.Input.Case, executed.Target, executed.Observation.Path = saved.Input.Case, saved.Target, saved.Observation.Path
	if executed.Name != saved.Name || len(executed.Assertions) != len(saved.Assertions) || len(executed.Input.Messages) != len(saved.Input.Messages) {
		t.Fatalf("a practice run changed what the test decides: %+v", executed)
	}
}

// A step is completed by the outcome it names and by nothing else. A test that
// expects the defect rather than the corrected behaviour is a real test, and
// running it produces two real results, but neither of them is a step of this
// guided sample: the point of the sample is the defect being found and fixed.
func TestAStepIsCompletedOnlyByTheOutcomeItNames(t *testing.T) {
	root := sampleWorkspace(t)
	spec := saveTest(t, root, "two-appointments.json", 2)

	baseline, err := guide.Run(t.Context(), root, spec, "baseline-run", false)
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Result.Status != testrunner.Pass {
		t.Fatalf("a test expecting the defect did not pass against it: %s", baseline.Result.Status)
	}
	fixed, err := guide.Run(t.Context(), root, spec, "post-fix-run", true)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Result.Status != testrunner.AssertionFailure {
		t.Fatalf("a test expecting the defect passed against the corrected fixture: %s", fixed.Result.Status)
	}
	progress := read(t, root)
	if progress.Next != guide.StepBaseline {
		t.Fatalf("a run with the wrong outcome completed a step: %+v", progress)
	}
	if step(t, progress, guide.StepBaseline).Done || step(t, progress, guide.StepPostFix).Done {
		t.Fatalf("a run with the wrong outcome completed a step: %+v", progress)
	}

	// A run of some other test over the same evidence is not this step either.
	other := saveTest(t, root, "z-one-appointment.json", 1)
	if _, err := guide.Run(t.Context(), root, other, "other-run", false); err != nil {
		t.Fatal(err)
	}
	if again := read(t, root); again.Spec != spec || step(t, again, guide.StepBaseline).Done {
		t.Fatalf("a run of another test completed the step: %+v", again)
	}
}

// senderStorage stands in for a sender's own evidence storage, reporting each
// message's event once it has been recorded.
type senderStorage struct{ recorded func(replay.Event) error }

func (senderStorage) BeforeSend(string) error             { return nil }
func (senderStorage) Sent(string, []byte) error           { return nil }
func (s senderStorage) Recorded(event replay.Event) error { return s.recorded(event) }

// A practice run sends to its receiver in this process, and between messages
// its own sender syncs the evidence it has just recorded (#350). The receiver
// waits for that sender as long as the run may take, so a sender stalled one
// second past the five-second idle limit the receiver used to have still gets
// the verdict an unstalled disk gives, and the step reads it back.
func TestAPracticeRunOutlastsItsSendersStorage(t *testing.T) {
	root := sampleWorkspace(t)
	spec := saveTest(t, root, "reschedule-test.json", 1)
	stalled := false
	t.Cleanup(guide.ObserveSenderForTest(senderStorage{recorded: func(event replay.Event) error {
		if !stalled && event.SourceOccurrence == "s0001-e000001" {
			stalled = true
			time.Sleep(6 * time.Second)
		}
		return nil
	}}))
	baseline, err := guide.Run(t.Context(), root, spec, "baseline-run", false)
	if err != nil {
		t.Fatal(err)
	}
	if !stalled {
		t.Fatal("the stalled sender recorded no first message")
	}
	if baseline.Result.Status != testrunner.AssertionFailure {
		t.Fatalf("a stalled sender changed the verdict to %s", baseline.Result.Status)
	}
	if recorded := step(t, read(t, root), guide.StepBaseline); recorded.Entry != "baseline-run" || recorded.Status != string(testrunner.AssertionFailure) {
		t.Fatalf("the run did not complete its step: %+v", recorded)
	}
}

// The practice receiver installs its ledger without flushing it, because this
// process reads it back, but the run keeps that ledger beside its result
// (#350). So it is flushed once the session is over and before the run
// answers, holding the final snapshot the result retained, and what the run
// retains is otherwise exactly what it was: the result reopens as written and
// the endpoint keeps the practice target's bytes. A kept ledger that cannot be
// flushed is not a finished run.
func TestAPracticeRunKeepsItsLedgerSynced(t *testing.T) {
	root := sampleWorkspace(t)
	spec := saveTest(t, root, "reschedule-test.json", 1)
	flushed := map[string][]byte{}
	t.Cleanup(guide.ObserveLedgerSyncForTest(func(path string) error {
		// The session is over once the receiver has written its case.
		if _, err := bundle.Open(filepath.Join(filepath.Dir(path), "receiver")); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		flushed[filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))] = data
		return err
	}))
	baseline, err := guide.Run(t.Context(), root, spec, "baseline-run", false)
	if err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(root, "baseline-run")
	kept, err := os.ReadFile(filepath.Join(run, "observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(filepath.Join(run, "result", "observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if synced, ok := flushed[filepath.Join("baseline-run", "observation.json")]; len(flushed) != 1 || !ok || !bytes.Equal(synced, retained) || !bytes.Equal(kept, retained) {
		t.Fatalf("the kept ledger was not flushed once, after its session, holding the final snapshot: %d flushes", len(flushed))
	}
	if reopened, err := testrunner.Open(filepath.Join(run, "result")); err != nil || reopened.Identity != baseline.Identity || reopened.Result.Status != testrunner.AssertionFailure {
		t.Fatalf("the retained result did not reopen as written: %v", err)
	}
	declared, err := replay.ReadDeclaredTarget(filepath.Join(run, "target.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := guide.Target()
	want.Address = declared.Address
	encoded, err := json.Marshal(want, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if endpoint, err := os.ReadFile(filepath.Join(run, "target.json")); err != nil || !bytes.Equal(endpoint, append(encoded, '\n')) {
		t.Fatalf("the retained endpoint is not the practice target: %v", err)
	}

	t.Cleanup(guide.ObserveLedgerSyncForTest(func(string) error { return errors.New("injected ledger flush failure") }))
	if _, err := guide.Run(t.Context(), root, spec, "unsynced-run", false); err == nil {
		t.Fatal("a run whose kept ledger could not be flushed reported a verdict")
	}
	if _, err := testrunner.Open(filepath.Join(root, "unsynced-run", "result")); err != nil {
		t.Fatalf("what the unfinished run retained is not kept: %v", err)
	}
}

// Every refusal leaves the workspace exactly as it was, and a cancelled run
// never reaches a socket.
func TestAPracticeRunRefusesWhatItCannotExecute(t *testing.T) {
	root := sampleWorkspace(t)
	spec := saveTest(t, root, "reschedule-test.json", 1)
	before := entries(t, root)

	cancelled, stop := context.WithCancel(t.Context())
	stop()
	if _, err := guide.Run(cancelled, root, spec, "cancelled-run", false); !errors.Is(err, guide.ErrCancelled) {
		t.Fatalf("a cancelled run reported %v", err)
	}

	for _, refused := range []struct {
		name   string
		spec   string
		output string
	}{
		{"an output that is not one entry", spec, "runs/baseline"},
		{"an output that escapes the workspace", spec, "../baseline"},
		{"an output that already exists", spec, guide.CaseName},
		{"a spec that is not one entry", "runs/spec.json", "baseline-run"},
		{"an entry that is not a spec", guide.TargetName, "baseline-run"},
		{"an entry that does not exist", "absent.json", "baseline-run"},
	} {
		t.Run(refused.name, func(t *testing.T) {
			if _, err := guide.Run(t.Context(), root, refused.spec, refused.output, false); err == nil {
				t.Fatal("the practice run accepted it")
			}
		})
	}
	if now := entries(t, root); len(now) != len(before) {
		t.Fatalf("a refused practice run changed the workspace: %v became %v", before, now)
	}

	// A spec pointed at other evidence is not the guided sample's, and rebinding
	// it onto the fixture would be answering a question nobody asked.
	elsewhere := saveTest(t, root, "elsewhere.json", 1)
	repointed := filepath.Join(root, "repointed.json")
	data, err := os.ReadFile(filepath.Join(root, elsewhere))
	if err != nil {
		t.Fatal(err)
	}
	repointedSpec := strings.Replace(string(data), `"case": "regression"`, `"case": "cancellation"`, 1)
	if err := os.WriteFile(repointed, []byte(repointedSpec), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := guide.Run(t.Context(), root, "repointed.json", "baseline-run", false); err == nil {
		t.Fatal("a practice run executed a spec over other evidence")
	}

	// Evidence that no longer verifies stops the run before it opens anything.
	if err := os.WriteFile(filepath.Join(root, guide.CaseName, "identity.sha256"), []byte("0000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := guide.Run(t.Context(), root, spec, "baseline-run", false); err == nil {
		t.Fatal("a practice run sent damaged evidence")
	}
	if progress := read(t, root); progress.Next != guide.StepSample {
		t.Fatalf("damaged sample evidence still reported a complete sample step: %+v", progress)
	}
}

// A generated family is retained evidence: nothing may be written inside one, so
// a guided path could not be walked in one. Preparing the workspace is what
// makes it writable, and it never touches the generated cases.
func TestPreparingTheWorkspaceMakesTheGeneratedFamilyWritable(t *testing.T) {
	family := filepath.Join(t.TempDir(), "readmit-sample")
	if _, err := synth.Write(family, sampleInputs); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(family, guide.CaseName, "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Destination(filepath.Join(family, "anything.json")); err == nil {
		t.Fatal("a generated family accepted a new entry beside its evidence")
	}
	if err := guide.PrepareWorkspace(t.Context(), family); err != nil {
		t.Fatal(err)
	}
	if _, err := artifactpath.Destination(filepath.Join(family, "anything.json")); err != nil {
		t.Fatalf("the prepared workspace still refuses a new entry: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(family, guide.CaseName, "identity.sha256"))
	if err != nil || string(after) != string(before) {
		t.Fatal("preparing the workspace changed the generated evidence")
	}
	if _, err := os.Lstat(filepath.Join(family, "family.json")); !os.IsNotExist(err) {
		t.Fatal("the prepared workspace still carries a family completion record")
	}

	// It is written once. A folder that already holds the entry is refused
	// rather than replaced, and a folder that is not a family is refused too.
	if err := guide.PrepareWorkspace(t.Context(), family); err == nil {
		t.Fatal("a folder that was already prepared was prepared again")
	}
	if err := guide.PrepareWorkspace(t.Context(), t.TempDir()); err == nil {
		t.Fatal("a folder holding no generated sample case was prepared as a workspace")
	}
	// The grid the authoring flow selects occurrences from reads an index, and
	// the window builds none, so the sample workspace ships one of its own.
	document, err := index.Open(filepath.Join(family, guide.IndexName))
	if err != nil {
		t.Fatalf("the sample index was not written: %v", err)
	}
	if document.Case.Identity != frozenRegressionIdentity {
		t.Fatalf("the sample index describes other evidence: %s", document.Case.Identity)
	}
	if document.Policy.Retention != index.RetainStates {
		t.Fatalf("the sample index retains values: %+v", document.Policy)
	}
	// The endpoint is a test endpoint on loopback with no approved transport, so
	// nothing about it can point a run at a real interface.
	target := guide.Target()
	if !target.TestEndpoint || target.ApprovedTransport || target.Transport != "plain" {
		t.Fatalf("the practice endpoint is not a plain loopback test endpoint: %+v", target)
	}
}

// A folder past the entry bound is refused rather than read in part.
func TestAFolderPastTheEntryBoundIsRefused(t *testing.T) {
	root := t.TempDir()
	for i := range 1025 {
		if err := os.WriteFile(filepath.Join(root, "entry-"+strconv.Itoa(i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := guide.Read(root); err == nil {
		t.Fatal("a folder past the bound was read")
	}
	if _, err := guide.Read(filepath.Join(root, "absent")); err == nil {
		t.Fatal("a folder that is not there was read")
	}
}

func entries(t *testing.T, root string) []string {
	t.Helper()
	listed, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed))
	for _, entry := range listed {
		names = append(names, entry.Name())
	}
	return names
}

// The steps' titles are the reviewed short stage names (#525 GS01-GS04). Only
// the titles changed: the IDs progress is keyed by, the order the path is
// walked in and every detail sentence stay exactly as they were.
func TestStepTitlesAreTheReviewedNamesOverUnchangedIDsOrderAndDetails(t *testing.T) {
	want := []guide.Step{
		{ID: guide.StepSample, Title: "Create sample workspace",
			Detail: "Writes the frozen synthetic cases, an index of the one this path uses, and a practice endpoint beside them. The evidence is generated, never imported, and the folder is new."},
		{ID: guide.StepTest, Title: "Create sample test",
			Detail: "Answers the authoring stages over the verified case and saves a readmit-test/v1 spec into the workspace. Expect one appointment on the ledger."},
		{ID: guide.StepBaseline, Title: "Run failing example",
			Detail: "Sends the saved spec at a practice receiver in its defective mode. The test is meant to fail here: the reschedule leaves a second appointment behind."},
		{ID: guide.StepPostFix, Title: "Run fixed example",
			Detail: "Sends the same bytes at a practice receiver whose defect is corrected. The reschedule now updates the original appointment and the test passes."},
	}
	if got := guide.Steps(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the guided steps are\n%+v\nwant\n%+v", got, want)
	}
	for id, step := range map[string]string{"sample": guide.StepSample, "test": guide.StepTest, "baseline": guide.StepBaseline, "post-fix": guide.StepPostFix} {
		if id != step {
			t.Fatalf("step ID %q changed to %q", id, step)
		}
	}
}
