// Package guide is the guided sample: the ordered steps that take somebody from
// the sample workspace this product writes to a regression test that fails
// against the fixture's defect and passes once that defect is corrected.
//
// Every step of it is an operation the window already performs, so this package
// adds no second engine. What it adds is the two things a guided path needs and
// nothing here had: a practice endpoint the sample workspace offers, so a test
// can name a target without anybody hand-writing a configuration file, and a
// practice run that binds a fixture receiver on loopback in this process and
// sends the saved spec at it, so a test can be executed without a second
// terminal running `readmit listen`.
//
// Progress is read back from the workspace, never remembered. There is no
// tutorial state, no completion flag and no document of its own: a step is done
// because the evidence it produces is on disk and the readers that own that
// evidence accept it. Closing the window and reopening the folder therefore
// reports exactly what was really done, and a step somebody performed from the
// command line counts the same as one they performed in the window.
//
// Nothing here is a claim about a real interface. The practice receiver is the
// built-in fixture, its observation boundary is that fixture's appointment
// ledger, and the evidence is the frozen synthetic family; see
// docs/guided-sample.md for what that does and does not establish.
package guide

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

const (
	// CaseName is the generated case the guided path authors its test against.
	// It is the reschedule regression of the frozen synthetic family: a booking
	// followed by the rescheduling of that booking, which a correct receiver
	// records as one appointment and the defective fixture records as two.
	CaseName = "regression"

	// CaseIdentity pins the frozen synthetic walkthrough and prevents a renamed
	// customer case from becoming a free practice execution.
	CaseIdentity = "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df"

	// TargetName is the practice endpoint the sample workspace offers. It is an
	// ordinary readmit-target/v1 configuration, written beside the evidence so
	// that the authoring flow has a target to choose and so the command line
	// reads the saved spec unchanged.
	TargetName = "practice-target.json"

	// IndexName is the disposable index of the sample case the window's message
	// grid reads. Building one is `readmit index build` on the command line and
	// nothing in the window at all, and the authoring flow selects the
	// occurrences a test sends out of the grid — so without an index in the
	// folder the guided path would stop at a terminal. The sample workspace
	// therefore ships one, derived and disposable like every other index.
	IndexName = "regression.index.json"

	// practiceAddress is the loopback endpoint the practice target declares. It
	// is the port `readmit listen` is documented with, so the saved spec means
	// the same thing to somebody who runs it from the command line. A practice
	// run in the window binds its own port instead and says that it did.
	practiceAddress = "127.0.0.1:2575"

	// familyRecord is the completion record `readmit synth` writes once every
	// case of a family is complete. Its presence is what marks the directory as
	// retained evidence, which is why a sample workspace does not keep one.
	familyRecord = "family.json"

	// maxEntries bounds one workspace scan, as every other listing in this
	// product does. A folder past the bound is refused rather than read in part.
	maxEntries = 1024

	// practiceTimeout bounds one practice run end to end, and the practice
	// receiver waits as long for the run's own sender. Two synthetic messages
	// over loopback take milliseconds; this is the wall a run that is going
	// nowhere meets.
	practiceTimeout = 30 * time.Second
)

// The steps of the guided sample, in the order they are performed.
const (
	// StepSample is the sample workspace: the frozen synthetic family and the
	// practice endpoint beside it.
	StepSample = "sample"
	// StepTest is the saved regression test: a readmit-test/v1 spec over the
	// sample case, authored one stage at a time and written to the workspace.
	StepTest = "test"
	// StepBaseline is the run against the fixture as it misbehaves. The test is
	// meant to fail here, and the result that records the failure is evidence.
	StepBaseline = "baseline"
	// StepPostFix is the same spec against the corrected fixture, which passes.
	StepPostFix = "post-fix"
)

// ErrCancelled is a practice run the caller stopped. Whatever the run had
// already written is retained and never claims a verdict of its own.
var ErrCancelled = errors.New("the practice run was cancelled")

// ErrCannotWrite is the one refusal a caller separates from the rest: the
// workspace could not be written at all, which may mean this account cannot
// write where it was pointed rather than anything about the request.
var ErrCannotWrite = errors.New("cannot write into the workspace; destination must be new and the folder writable")

// Step is one step of the guided sample and what the workspace says about it.
// Done, Entry and Status are read back from the folder every time: nothing here
// is remembered between calls, so a step is complete because its evidence is,
// and a folder that lost that evidence reports the step as incomplete again.
type Step struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Done   bool   `json:"done"`
	// Entry is the workspace entry that shows the step was done, when it was.
	Entry string `json:"entry,omitzero"`
	// Status is the verdict the retained result recorded, for the two run
	// steps. It is the runner's own word, re-derived by the runner's reader.
	Status string `json:"status,omitzero"`
}

// Progress is the guided sample read out of one workspace folder: the steps,
// what the folder shows about each, and the step to perform next. Case, Identity
// and Spec name the evidence the later steps are bound to, so a caller never has
// to guess which case or which spec the guided path means.
type Progress struct {
	Case     string `json:"case,omitzero"`
	Identity string `json:"identity,omitzero"`
	Spec     string `json:"spec,omitzero"`
	Steps    []Step `json:"steps"`
	Next     string `json:"next,omitzero"`
}

// Complete reports whether every step of the guided sample is done.
func (p Progress) Complete() bool { return p.Next == "" }

// Steps is the guided sample with nothing done: the fixed description of the
// path, in the order it is walked.
func Steps() []Step {
	return []Step{
		{ID: StepSample, Title: "Create the sample workspace",
			Detail: "Writes the frozen synthetic cases, an index of the one this path uses, and a practice endpoint beside them. The evidence is generated, never imported, and the folder is new."},
		{ID: StepTest, Title: "Author a regression test over the sample case",
			Detail: "Answers the authoring stages over the verified case and saves a readmit-test/v1 spec into the workspace. Expect one appointment on the ledger."},
		{ID: StepBaseline, Title: "Run it against the fixture as it misbehaves",
			Detail: "Sends the saved spec at a practice receiver in its defective mode. The test is meant to fail here: the reschedule leaves a second appointment behind."},
		{ID: StepPostFix, Title: "Run the same spec against the corrected fixture",
			Detail: "Sends the same bytes at a practice receiver whose defect is corrected. The reschedule now updates the original appointment and the test passes."},
	}
}

// Target is the practice endpoint configuration the sample workspace offers. It
// is a test endpoint on loopback with no approved transport and no credential,
// so nothing about it can point a run at a real interface.
func Target() replay.Target {
	return replay.Target{
		Schema: replay.TargetSchema, TestEndpoint: true, Address: practiceAddress,
		Transport: "plain", ApprovedTransport: false,
		ConnectTimeout: "2s", MessageTimeout: "5s", MaxACKBytes: 65536,
	}
}

// PrepareWorkspace turns the generated family a caller has just written into the
// workspace the guided sample is walked in.
//
// Two things separate a workspace from a family. A family carries a completion
// record, and a directory carrying one is retained evidence that artifactpath
// refuses every write inside — including the test spec the authoring flow saves
// and the evidence a practice run retains — so a guided path cannot be walked in
// one at all. And a test names a target configuration, which a generated family
// does not hold, so without one the authoring flow could only be completed by
// somebody writing a configuration file by hand. The record is therefore removed
// and the practice endpoint written in its place.
//
// The case bundles are not touched. They are the bytes `readmit synth` wrote,
// verified by the same reader, carrying the same generator provenance and the
// same identities; what this leaves behind is three generated cases and an
// endpoint to send them at, which is a workspace rather than a family.
func PrepareWorkspace(ctx context.Context, root string) error {
	source, err := bundle.Open(artifactpath.JoinReference(root, CaseName))
	if err != nil {
		return errors.New("the generated sample case could not be verified as complete evidence")
	}
	// A folder with no completion record is not a family this call can prepare:
	// it was prepared already, or it was never generated. Either is a statement
	// about the folder rather than about whether this account can write.
	if err := os.Remove(filepath.Join(root, familyRecord)); errors.Is(err, fs.ErrNotExist) {
		return errors.New("this folder is not a generated family, or it was prepared as a workspace already")
	} else if err != nil {
		return ErrCannotWrite
	}
	if err := writeTarget(root); err != nil {
		return err
	}
	return writeIndex(ctx, root, source)
}

// indexPolicy is what the sample index retains: the message type, the message
// control identifier and the filler appointment identifier, written as the
// canonical selectors the index contract declares its fields in.
//
// It keeps the decoded state and byte span of those three positions and no
// value at all, because a retained decoded field is patient data and nothing
// about walking a sample needs one. The retention has no end, which is a
// decision stated here rather than a default: an index is derived and
// disposable, and deleting this one is a person's decision.
func indexPolicy() index.Policy {
	return index.Policy{
		Fields:      []string{"MSH[1]-9[1]", "MSH[1]-10[1]", "SCH[1]-2[1]"},
		Retention:   index.RetainStates,
		RetainUntil: nil,
	}
}

// writeIndex builds the sample index from the verified case and writes it. Its
// records are a pure function of that evidence and the policy above, so an index
// deleted at any moment is built again from the case it describes; only the
// instant it records differs between two builds.
func writeIndex(ctx context.Context, root string, source *bundle.Bundle) error {
	document, err := index.Build(ctx, source, indexPolicy(), time.Now().UTC())
	if err != nil {
		return errors.New("the sample index could not be built from the generated case")
	}
	if _, err := index.Write(artifactpath.JoinReference(root, IndexName), document); err != nil {
		return ErrCannotWrite
	}
	return nil
}

// writeTarget writes the practice endpoint, and refuses an entry that already
// exists rather than replacing one. The configuration is read back through the
// reader a spec resolves a target with, so what it leaves behind is a target
// this release reads and not merely a file that looks like one.
func writeTarget(root string) error {
	destination, err := artifactpath.Destination(artifactpath.JoinReference(root, TargetName))
	if err != nil {
		return ErrCannotWrite
	}
	data, err := json.Marshal(Target(), json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode the practice endpoint")
	}
	if err := writeNew(destination, append(data, '\n')); err != nil {
		return err
	}
	if _, err := replay.ReadDeclaredTarget(destination); err != nil {
		return errors.New("the practice endpoint written to the workspace is not one this release reads")
	}
	return nil
}

// Read reports the guided sample over one workspace folder.
//
// Every step is decided by the reader that owns the evidence it produces: the
// case bundle reader verifies the sample, the spec reader decodes the saved
// test, and the result reader re-derives each verdict from the retained run. A
// folder that is not a sample workspace is not an error — it is the guided
// sample with nothing done yet.
func Read(root string) (Progress, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return Progress{}, errors.New("the workspace folder cannot be read")
	}
	if len(entries) > maxEntries {
		return Progress{}, errors.New("the folder holds more entries than this release reads")
	}
	progress := Progress{Steps: Steps()}
	source, err := bundle.Open(artifactpath.JoinReference(root, CaseName))
	if err != nil || source.Manifest.Provenance.Mode != bundle.Generated {
		return progress.settled(), nil
	}
	if _, err := replay.ReadDeclaredTarget(artifactpath.JoinReference(root, TargetName)); err != nil {
		return progress.settled(), nil
	}
	// The grid the authoring flow selects occurrences from reads an index, and
	// the window builds none, so a sample workspace without one is a sample
	// somebody would have to leave for a terminal to finish.
	if document, err := index.Open(artifactpath.JoinReference(root, IndexName)); err != nil || document.Describes(source) != nil {
		return progress.settled(), nil
	}
	progress.Case, progress.Identity = CaseName, source.Identity
	progress.Steps[0].Done, progress.Steps[0].Entry = true, CaseName

	name, saved := savedSpec(root, entries)
	if name == "" {
		return progress.settled(), nil
	}
	progress.Spec = name
	progress.Steps[1].Done, progress.Steps[1].Entry = true, name

	// Each run step is completed by the outcome it names and by nothing else: a
	// run against the defect that did not fail, and a run against the corrected
	// fixture that did not pass, are both real results and neither is the step.
	// The order they were run in does not matter, because each is read back from
	// what it retained rather than from when it was written.
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mode, status, ok := verdict(root, entry.Name(), source.Identity, saved)
		if !ok {
			continue
		}
		at := -1
		if mode == observation.Defective && status == testrunner.AssertionFailure {
			at = 2
		}
		if mode == observation.Fixed && status == testrunner.Pass {
			at = 3
		}
		if at < 0 || progress.Steps[at].Done {
			continue
		}
		progress.Steps[at].Done = true
		progress.Steps[at].Entry, progress.Steps[at].Status = entry.Name(), string(status)
	}
	return progress.settled(), nil
}

// settled names the first step that is not done, which is the step to perform
// next, and nothing when the guided sample is complete.
func (p Progress) settled() Progress {
	for _, step := range p.Steps {
		if !step.Done {
			p.Next = step.ID
			return p
		}
	}
	return p
}

// savedSpec finds the regression test the guided path saved: the first entry,
// in the order the folder lists them, that decodes as a readmit-test/v1 spec
// sending the sample case at the practice endpoint.
func savedSpec(root string, entries []os.DirEntry) (string, testrunner.Spec) {
	for _, entry := range entries {
		if !entry.Type().IsRegular() || entry.Name() == TargetName || entry.Name() == IndexName {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() > testrunner.MaxSpecBytes {
			continue
		}
		spec, err := testrunner.ReadSpec(artifactpath.JoinReference(root, entry.Name()))
		if err != nil || spec.Input.Case != CaseName || spec.Target != TargetName {
			continue
		}
		return entry.Name(), spec
	}
	return "", testrunner.Spec{}
}

// verdict reports what one entry of the workspace decided about one saved test.
//
// The result is opened through the runner's own reader, which re-derives every
// verdict from the retained run and observation evidence, so nothing here is a
// stored word taken on trust. The entry counts as a run of this test when the
// case it sent is the sample case and the spec it retained is the saved one with
// the three bindings a practice run rebinds put back: a run of some other test,
// or of this one against other evidence, is not this step.
func verdict(root, name, identity string, saved testrunner.Spec) (observation.Mode, testrunner.Status, bool) {
	result := filepath.Join(root, name, "result")
	if runresult.ExecutionFamily(result, runresult.RegularFile) != runresult.ResultFamily {
		return "", "", false
	}
	artifact, err := testrunner.Open(result)
	if err != nil || artifact.Spec == nil || artifact.Result.InputBundleIdentity != identity {
		return "", "", false
	}
	executed := *artifact.Spec
	executed.Input.Case, executed.Target = saved.Input.Case, saved.Target
	executed.Observation.Path = saved.Observation.Path
	if !sameSpec(executed, saved) {
		return "", "", false
	}
	return artifact.Result.ReceiverMode, artifact.Result.Status, true
}

// sameSpec compares two specs as documents. Both sides are re-encoded here, so
// what is compared is what each one declares rather than how it was formatted.
func sameSpec(left, right testrunner.Spec) bool {
	encodedLeft, leftErr := json.Marshal(left, json.Deterministic(true))
	encodedRight, rightErr := json.Marshal(right, json.Deterministic(true))
	return leftErr == nil && rightErr == nil && string(encodedLeft) == string(encodedRight)
}

// Run executes a saved spec against a practice receiver in this process.
//
// The receiver is the built-in fixture, bound to a loopback port it chooses
// itself; corrected decides whether that fixture behaves or reproduces its
// defect. Three bindings of the spec are rebound onto the run's own directory —
// the target, the observation document and the path back to the case — and
// everything a verdict depends on, the selected occurrences, the boundary and
// every assertion, is the saved spec's own. Nothing else is opened: no other
// host is contacted, and the spec in the workspace is never rewritten.
//
// A cancelled run stops sending. What it had already written is retained where
// it was written and reported as cancelled, never as a verdict.
func Run(ctx context.Context, root, spec, output string, corrected bool) (*testrunner.Artifact, error) {
	if ctx.Err() != nil {
		return nil, ErrCancelled
	}
	if artifactpath.EntryName(output) != nil {
		return nil, errors.New("a practice run is written to one new entry of the open workspace")
	}
	saved, err := readSaved(root, spec)
	if err != nil {
		return nil, err
	}
	dir, err := artifactpath.Destination(artifactpath.JoinReference(root, output))
	if err != nil {
		return nil, errors.New("a practice run must be written outside every retained artifact of this workspace")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, ErrCannotWrite
	}
	mode := observation.Defective
	if corrected {
		mode = observation.Fixed
	}
	artifact, err := execute(ctx, dir, saved, mode)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ErrCancelled
	}
	return artifact, nil
}

// readSaved reads the spec a practice run executes and holds it to the guided
// sample: it sends the sample case at the practice endpoint. A spec pointed
// somewhere else is refused here rather than silently rebound onto a fixture.
func readSaved(root, name string) (testrunner.Spec, error) {
	path, err := artifactpath.File(root, name)
	if err != nil {
		return testrunner.Spec{}, errors.New("a test spec is one regular file of the open workspace, never a symbolic link")
	}
	spec, err := testrunner.ReadSpec(path)
	if err != nil {
		return testrunner.Spec{}, errors.New("that entry is not a test spec this release reads")
	}
	if spec.Input.Case != CaseName || spec.Target != TargetName {
		return testrunner.Spec{}, errors.New("a practice run executes a test that sends the sample case at the practice endpoint")
	}
	if source, err := bundle.Open(artifactpath.JoinReference(root, CaseName)); err != nil || source.Identity != CaseIdentity {
		return testrunner.Spec{}, errors.New("the sample case could not be verified as complete, unmodified evidence")
	}
	return spec, nil
}

// execute binds the fixture receiver, rebinds the three paths onto dir, and runs
// the spec. The receiver is started before the runner can connect, and the
// listener is closed and awaited before the artifact is read back.
func execute(ctx context.Context, dir string, spec testrunner.Spec, mode observation.Mode) (*testrunner.Artifact, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("cannot bind the practice receiver on this machine")
	}
	defer listener.Close()
	// The receiver waits as long as the run may take for this run's own sender,
	// which syncs the evidence it has just recorded before sending the next
	// message. Its ledger is read back only by this process, so each snapshot is
	// installed without being flushed first; flushing it before each ACK put the
	// disk's latency inside the practice target's message timeout. The run keeps
	// that ledger, so flushKeptLedger flushes it once the session is over.
	fixture, err := receiver.New(receiver.Config{
		Mode: mode, OutputPath: filepath.Join(dir, "receiver"),
		ObservationPath: filepath.Join(dir, "observation.json"),
		MaxMessages:     len(spec.Input.Messages), MaxFrameBytes: 1 << 20, IdleTimeout: practiceTimeout,
		InProcess: true,
	})
	if err != nil {
		return nil, errors.New("cannot prepare the practice receiver")
	}
	target := Target()
	target.Address = listener.Addr().String()
	// The executed copy sits one directory below the saved one, so the reference
	// back to the case traverses one level up. artifactpath.JoinReference does
	// not clean a reference, which is what makes that traversal visible rather
	// than silently resolved; it is safe here because dir is a directory this
	// call created inside the workspace holding the case.
	spec.Input.Case = "../" + CaseName
	spec.Target = "target.json"
	if spec.Observation.Boundary == testrunner.LedgerBoundary {
		spec.Observation.Path = "observation.json"
	}
	if err := write(dir, "target.json", target); err != nil {
		return nil, err
	}
	if err := write(dir, "spec.json", spec); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, practiceTimeout)
	defer cancel()
	served := make(chan error, 1)
	go func() { _, err := fixture.Serve(runCtx, listener); served <- err }()
	// New has installed the empty observation before the runner can connect.
	artifact, runErr := sendPractice(runCtx, filepath.Join(dir, "spec.json"), filepath.Join(dir, "result"))
	cancel()
	serveErr := <-served
	ledgerErr := flushKeptLedger(filepath.Join(dir, "observation.json"))
	if ctx.Err() != nil {
		return nil, ErrCancelled
	}
	if runErr != nil || serveErr != nil || ledgerErr != nil || artifact == nil {
		return nil, errors.New("the practice run did not finish; whatever it retained is kept")
	}
	return artifact, nil
}

// sendPractice sends the executed spec to the practice receiver. It is the
// ordinary test runner, which this package's tests replace to stand in a
// sender whose own storage stalls.
var sendPractice = testrunner.Run

// syncLedger flushes the ledger a practice run keeps. It is the seam this
// package's tests take to see that the kept ledger is flushed, and when.
var syncLedger = (*os.File).Sync

// flushKeptLedger flushes the practice receiver's ledger once its session is
// over, so the copy the run keeps beside its result is synced like everything
// else it retains. No send or receive waits on the disk here.
func flushKeptLedger(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	err = syncLedger(file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func write(dir, name string, value any) error {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode the practice run configuration")
	}
	return writeNew(filepath.Join(dir, name), append(data, '\n'))
}

// writeNew creates a file exclusively, so an entry that already exists is
// refused rather than replaced. What it writes is owner-readable: a spec carries
// the expected values somebody typed, which are the same local literals the
// evidence beside them holds.
func writeNew(destination string, data []byte) error {
	return practiceFile.Create(destination, data)
}

// practiceFile is how a practice run's configuration is created, through the
// shared document store.
var practiceFile = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{Destination: ErrCannotWrite, Create: ErrCannotWrite, Write: ErrCannotWrite},
}
