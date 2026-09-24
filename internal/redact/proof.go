package redact

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

// runProof creates two genuinely fresh receivers, verifies each retained result
// through the public reader, and compares the exact agreed assertion positions.
// Only the fixed built-in fixture is executable here; spec prose is never code.
func runProof(ctx context.Context, spec testrunner.Spec, casePath, dir string, required []int) (Proof, error) {
	var err error
	casePath, err = artifactpath.Resolve(casePath)
	if err != nil {
		return Proof{}, err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return Proof{}, errors.New("cannot reserve private proof directory")
	}
	dir, err = artifactpath.Resolve(dir)
	if err != nil {
		return Proof{}, err
	}
	source, err := bundle.Open(casePath)
	if err != nil {
		return Proof{}, err
	}
	caseReference, err := filepath.Rel(dir, casePath)
	if err != nil {
		return Proof{}, errors.New("cannot resolve proof source")
	}
	caseReference = filepath.ToSlash(filepath.Join("..", caseReference))
	baseline, err := runFixture(ctx, spec, caseReference, filepath.Join(dir, "baseline"), observation.Defective)
	if err != nil {
		return Proof{}, fmt.Errorf("baseline fixture: %w", err)
	}
	postfix, err := runFixture(ctx, spec, caseReference, filepath.Join(dir, "postfix"), observation.Fixed)
	if err != nil {
		return Proof{}, fmt.Errorf("postfix fixture: %w", err)
	}
	if baseline.Result.Status != testrunner.AssertionFailure || postfix.Result.Status != testrunner.Pass || !slices.Equal(failedAssertions(baseline), required) || baseline.Result.InputBundleIdentity != source.Identity || postfix.Result.InputBundleIdentity != source.Identity || !sameAssertionContract(baseline.Spec, postfix.Spec) {
		return Proof{}, fmt.Errorf("fixture proof did not preserve the exact agreed failures %v and full fixed pass: baseline %s; postfix %s", required, outcome(baseline), outcome(postfix))
	}
	for _, artifact := range []*testrunner.Artifact{baseline, postfix} {
		if err := verifyFixtureProof(source, artifact); err != nil {
			return Proof{}, err
		}
	}
	return Proof{Profile: observation.Profile, Boundary: testrunner.LedgerBoundary, BaselineIdentity: baseline.Identity, PostfixIdentity: postfix.Identity, BaselineStatus: baseline.Result.Status, PostfixStatus: postfix.Result.Status, FailedAssertions: failedAssertions(baseline)}, nil
}

func sameAssertionContract(a, b *testrunner.Spec) bool {
	if a == nil || b == nil {
		return false
	}
	left, _ := encode(a.Assertions)
	right, _ := encode(b.Assertions)
	return string(left) == string(right) && slices.Equal(a.Input.Messages, b.Input.Messages) && a.Observation.Boundary == b.Observation.Boundary
}

func fixtureTarget(address string) replay.Target {
	return replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: address, Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "3s", MaxACKBytes: 16384}
}

// fixtureBudget bounds one fixture execution. The fixture receiver waits as
// long for this proof's own sender, which syncs the run evidence it has just
// recorded before sending the next message: a shorter idle limit let that
// sender's storage, rather than the fixture, end the session.
const fixtureBudget = 30 * time.Second

// fixtureReceiver configures the fresh built-in fixture one proof execution
// sends to. Its live ledger is read back only by this process, and every copy
// retained from it (the result's observations, the receiver case, an export
// packet) is written synced, so it is installed in process: flushing it before
// each ACK put the disk's latency inside the fixture target's message timeout,
// which export packets record and cannot change.
func fixtureReceiver(mode observation.Mode, dir string, messages int) receiver.Config {
	return receiver.Config{Mode: mode, OutputPath: filepath.Join(dir, "receiver.case"), ObservationPath: filepath.Join(dir, "observation.json"), MaxFrameBytes: 1 << 20, IdleTimeout: fixtureBudget, MaxMessages: messages, InProcess: true}
}

// executeFixture sends one fixture spec to its receiver. It is the ordinary
// test runner; it is the one seam this package's proof tests take, to stand
// in a sender whose own storage stalls.
var executeFixture = testrunner.Run

// sessionFamily is one fixture proof session: the target and spec it sends,
// beside the receiver's case and ledger and the result the runner writes. It
// is a workspace, so it writes no completion record.
var sessionFamily = artifactdir.Family{
	Layout: artifactdir.Layout{
		AllowFile: func(name string) bool { return name == "target.json" || name == "spec.json" },
	},
	Errors: artifactdir.Errors{
		Reserve: errors.New("cannot reserve fixture proof session"),
		Create:  errCreateFile,
		Write:   errWriteFile,
	},
}

func runFixture(ctx context.Context, spec testrunner.Spec, caseReference, dir string, mode observation.Mode) (*testrunner.Artifact, error) {
	session, err := artifactdir.Create(dir, sessionFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("cannot bind local proof receiver")
	}
	defer listener.Close()
	r, err := receiver.New(fixtureReceiver(mode, dir, len(spec.Input.Messages)))
	if err != nil {
		return nil, err
	}
	proofCtx, cancel := context.WithTimeout(ctx, fixtureBudget)
	done := make(chan error, 1)
	go func() { _, err := r.Serve(proofCtx, listener); done <- err }()
	waited := false
	defer func() {
		cancel()
		if !waited {
			<-done
		}
	}()
	spec.Input.Case = caseReference
	spec.Target = "target.json"
	spec.Observation.Path = "observation.json"
	targetBytes, err := encode(fixtureTarget(listener.Addr().String()))
	if err != nil {
		return nil, err
	}
	specBytes, err := encode(spec)
	if err != nil {
		return nil, err
	}
	if err := session.WriteFile("target.json", targetBytes); err != nil {
		return nil, err
	}
	if err := session.WriteFile("spec.json", specBytes); err != nil {
		return nil, err
	}
	executed, err := executeFixture(proofCtx, filepath.Join(dir, "spec.json"), filepath.Join(dir, "result"))
	if err != nil {
		return nil, fmt.Errorf("fixture test did not complete: %w", err)
	}
	cancel()
	serveErr := <-done
	waited = true
	if serveErr != nil {
		return nil, fmt.Errorf("fixture receiver could not finalize proof evidence (%w); sender %s", serveErr, outcome(executed))
	}
	if _, err := bundle.Open(filepath.Join(dir, "receiver.case")); err != nil {
		return nil, fmt.Errorf("fixture receiver case: %w", err)
	}
	artifact, err := testrunner.Open(filepath.Join(dir, "result"))
	if err != nil {
		return nil, fmt.Errorf("fixture result: %w", err)
	}
	return artifact, nil
}

// outcome states what one fixture execution retained, in the words of its
// result and run: the status, its error class, the failed assertion positions
// and the first message whose delivery failed, with the transport phase and
// the time that message took. It names no path and carries no evidence value,
// so a proof failure can be reported wherever the proof ran.
func outcome(artifact *testrunner.Artifact) string {
	if artifact == nil {
		return "retained no result"
	}
	text := string(artifact.Result.Status)
	if artifact.Result.ErrorClass != "" {
		text += " (" + artifact.Result.ErrorClass + ")"
	}
	if failed := failedAssertions(artifact); len(failed) > 0 {
		text += fmt.Sprintf(" with failed assertions %v", failed)
	}
	if artifact.Run != nil {
		for i, event := range artifact.Run.Events {
			if event.TransportError != nil {
				text += fmt.Sprintf(" at message %d: %s during %s after %s", i+1, event.TransportError.Class, event.TransportError.Phase, time.Duration(event.ElapsedNS).Round(time.Millisecond))
				break
			}
		}
	}
	return text
}

// verifyFixtureProof owns the successful fixture contract used both after fresh
// execution and when reopening an export. Matching retained copies alone cannot
// establish that their ledger followed from the approved unchanged requests.
func verifyFixtureProof(source *bundle.Bundle, artifact *testrunner.Artifact) error {
	if artifact.Run == nil || !artifact.Run.Successful() || artifact.InitialObservation == nil || artifact.FinalObservation == nil || len(artifact.Run.Manifest.Transformations) != 0 {
		return errors.New("fixture proof requires unchanged requests and complete observations")
	}
	if err := verifyProofSources(source, artifact.Run); err != nil {
		return err
	}
	if err := verifyFixtureACKs(source, artifact); err != nil {
		return err
	}
	requests := make([][]byte, 0, len(artifact.Run.Events))
	for _, event := range artifact.Run.Events {
		raw, err := artifact.Run.Raw(event.Sent)
		if err != nil {
			return err
		}
		requests = append(requests, raw)
	}
	return receiver.VerifyLedger(requests, *artifact.InitialObservation, *artifact.FinalObservation)
}
