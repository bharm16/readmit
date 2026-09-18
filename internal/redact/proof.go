package redact

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

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
	casePath, err = canonical(casePath)
	if err != nil {
		return Proof{}, err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return Proof{}, errors.New("cannot reserve private proof directory")
	}
	dir, err = canonical(dir)
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
		return Proof{}, err
	}
	postfix, err := runFixture(ctx, spec, caseReference, filepath.Join(dir, "postfix"), observation.Fixed)
	if err != nil {
		return Proof{}, err
	}
	if baseline.Result.Status != testrunner.AssertionFailure || postfix.Result.Status != testrunner.Pass || !slices.Equal(failedAssertions(baseline), required) || baseline.Result.InputBundleIdentity != source.Identity || postfix.Result.InputBundleIdentity != source.Identity || !sameAssertionContract(baseline.Spec, postfix.Spec) {
		return Proof{}, errors.New("fixture proof did not preserve the exact agreed failures and full fixed pass")
	}
	for _, artifact := range []*testrunner.Artifact{baseline, postfix} {
		if err := verifyFixtureACKs(source, artifact); err != nil {
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

func runFixture(ctx context.Context, spec testrunner.Spec, caseReference, dir string, mode observation.Mode) (*testrunner.Artifact, error) {
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, errors.New("cannot reserve fixture proof session")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("cannot bind local proof receiver")
	}
	defer listener.Close()
	r, err := receiver.New(receiver.Config{Mode: mode, OutputPath: filepath.Join(dir, "receiver.case"), ObservationPath: filepath.Join(dir, "observation.json"), MaxFrameBytes: 1 << 20, IdleTimeout: 5 * time.Second, MaxMessages: len(spec.Input.Messages)})
	if err != nil {
		return nil, err
	}
	proofCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
	if err := writeFile(dir, "target.json", targetBytes); err != nil {
		return nil, err
	}
	if err := writeFile(dir, "spec.json", specBytes); err != nil {
		return nil, err
	}
	_, err = testrunner.Run(proofCtx, filepath.Join(dir, "spec.json"), filepath.Join(dir, "result"))
	if err != nil {
		return nil, err
	}
	cancel()
	serveErr := <-done
	waited = true
	if serveErr != nil {
		return nil, errors.New("fixture receiver could not finalize proof evidence")
	}
	if _, err := bundle.Open(filepath.Join(dir, "receiver.case")); err != nil {
		return nil, err
	}
	return testrunner.Open(filepath.Join(dir, "result"))
}
