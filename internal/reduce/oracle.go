package reduce

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testrunner"
)

// OracleRequest is what a durable oracle is built from: the regression test the
// unreduced sequence runs, the reset an operator selected, and one new working
// directory to spend trials in.
type OracleRequest struct {
	// SpecPath is one readmit-test/v1 spec. Every trial runs a copy of it that
	// names fewer messages; the spec itself is never modified.
	SpecPath string
	// Workspace is a new directory outside every artifact. Each trial writes
	// one narrowed spec and one durable run into it. It is working material,
	// not evidence, and nothing reads it back as a case.
	Workspace string
	// Signature is the failure being held, so the oracle can refuse a plan
	// naming an assertion this spec does not declare rather than spending a
	// budget establishing that it never fails.
	Signature Signature
	// Reset is the explicitly selected reset plan every trial runs first, and
	// Resolve the name resolution its destination decision is allowed.
	Reset   fixturereset.Request
	Resolve sendpolicy.Resolver
}

// DurableOracle answers one trial as one durable run of a narrowed copy of one
// test spec, after returning the environment to its declared starting state.
//
// This is the whole of what "execute through durable runs" means here: each
// trial is [durablerun.Start] over its own fresh destination, so every trial
// leaves a journal, an intent record for each message and a lease of its own,
// and a trial interrupted halfway is recoverable as exactly what it was rather
// than repeated. Nothing is resumed and nothing is resent: a candidate that did
// not finish is an undecided observation, and reduction stops on one.
type DurableOracle struct {
	spec      testrunner.Spec
	workspace string
	reset     fixturereset.Request
	resolve   sendpolicy.Resolver
	signature Signature
	required  []string
	trials    int
}

// NewDurableOracle reads the spec, resolves every path it names so a narrowed
// copy can run from anywhere, and creates the working directory. It executes
// nothing and opens no network connection.
func NewDurableOracle(request OracleRequest) (*DurableOracle, error) {
	if err := validateSignature(request.Signature); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(request.SpecPath)
	if err != nil {
		return nil, errors.New("cannot resolve the test spec this reduction runs")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, errors.New("cannot resolve the test spec this reduction runs")
	}
	spec, err := testrunner.ReadSpec(resolved)
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(resolved)
	spec.Input.Case = artifactpath.JoinReference(directory, spec.Input.Case)
	spec.Target = artifactpath.JoinReference(directory, spec.Target)
	if spec.Observation.Path != "" {
		spec.Observation.Path = artifactpath.JoinReference(directory, spec.Observation.Path)
	}
	required, err := requiredBy(spec, request.Signature)
	if err != nil {
		return nil, err
	}
	workspace, err := artifactpath.Destination(request.Workspace)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(workspace, 0700); err != nil {
		return nil, errors.New("cannot create the reduction workspace; it must be new and its parent writable")
	}
	return &DurableOracle{
		spec: spec, workspace: workspace, reset: request.Reset,
		resolve: request.Resolve, signature: request.Signature, required: required,
	}, nil
}

// requiredBy reports the occurrences the chosen signature is stated about. An
// assertion about one message cannot survive a candidate that does not send it:
// dropping the message would drop the assertion, and a signature nothing
// evaluates is never reproduced. Those occurrences are therefore retained by
// every candidate, and the reduction records that it never tried to remove them.
func requiredBy(spec testrunner.Spec, signature Signature) ([]string, error) {
	declared := make(map[string]string, len(spec.Assertions))
	for _, assertion := range spec.Assertions {
		declared[assertion.ID] = assertion.Message
	}
	required := make([]string, 0, len(signature.Assertions))
	for _, id := range signature.Assertions {
		message, ok := declared[id]
		if !ok {
			return nil, errors.New("this signature names an assertion the test spec does not declare, so no sequence can reproduce it")
		}
		if message != "" && !slices.Contains(required, message) {
			required = append(required, message)
		}
	}
	return required, nil
}

// Messages is the sequence the unreduced run sends, in the order the spec
// declares it.
func (o *DurableOracle) Messages() []string { return slices.Clone(o.spec.Input.Messages) }

// Required is the occurrences the chosen signature is stated about.
func (o *DurableOracle) Required() []string { return slices.Clone(o.required) }

// Reset runs the explicitly selected reset plan and names what it established.
// Nothing here decides that an unconfirmed reset is good enough: the outcome is
// returned exactly as the reset reported it.
func (o *DurableOracle) Reset(ctx context.Context) (fixturereset.Outcome, fixturereset.Reason) {
	result, _ := fixturereset.Run(ctx, o.reset, o.resolve)
	return result.Outcome, result.Reason
}

// Observe runs one candidate as one durable run and reports what it
// established. An assertion the candidate no longer sends a message for is
// dropped with that message, because an assertion about a message nobody sent
// is not an expectation this run can evaluate.
func (o *DurableOracle) Observe(ctx context.Context, occurrences []string) (Observation, error) {
	narrowed, err := o.narrow(occurrences)
	if err != nil {
		return Observation{}, err
	}
	o.trials++
	document, err := json.Marshal(narrowed, json.Deterministic(true))
	if err != nil {
		return Observation{}, errors.New("cannot encode the narrowed test spec for this trial")
	}
	specPath := filepath.Join(o.workspace, fmt.Sprintf("t%04d.spec.json", o.trials))
	if err := os.WriteFile(specPath, append(document, '\n'), 0600); err != nil {
		return Observation{}, errors.New("cannot write the narrowed test spec for this trial")
	}
	summary, err := durablerun.Start(ctx, specPath, filepath.Join(o.workspace, fmt.Sprintf("t%04d", o.trials)))
	if summary.Schema == "" {
		// Nothing was journalled at all, so there is no run to read a state
		// out of. The oracle could not answer; it did not answer "no".
		if err == nil {
			err = errors.New("the trial produced no durable run")
		}
		return Observation{}, err
	}
	observed := Observation{State: summary.State, DeliveryUncertain: summary.DeliveryUncertain, Failed: []string{}}
	if summary.ResultIdentity == "" {
		// A run that stopped as a verdict about an expectation but retained no
		// result has nothing to read that verdict out of. Reporting it with an
		// empty assertion set would score it as a candidate that stopped
		// failing, which is the one direction an unreadable trial must never
		// be resolved in.
		if summary.State == durablerun.Passed || summary.State == durablerun.AssertionFailed {
			return Observation{}, errors.New("this trial reported a verdict with no result to read it out of, so nothing was established about the failure")
		}
		return observed, nil
	}
	artifact, err := testrunner.Open(filepath.Join(o.workspace, fmt.Sprintf("t%04d", o.trials), "result"))
	if err != nil {
		return Observation{}, errors.New("the result of this trial could not be verified, so nothing was established about the failure")
	}
	for _, assertion := range artifact.Result.Assertions {
		if assertion.Status == "failed" {
			observed.Failed = append(observed.Failed, assertion.Assertion.ID)
		}
	}
	return observed, nil
}

// narrow is the candidate spec: the same declarations, fewer messages, and only
// the assertions those messages can still be about.
func (o *DurableOracle) narrow(occurrences []string) (testrunner.Spec, error) {
	sending := make(map[string]bool, len(occurrences))
	for _, id := range occurrences {
		sending[id] = true
	}
	narrowed := o.spec
	narrowed.Input.Messages = slices.Clone(occurrences)
	narrowed.Assertions = nil
	for _, assertion := range o.spec.Assertions {
		if assertion.Message == "" || sending[assertion.Message] {
			narrowed.Assertions = append(narrowed.Assertions, assertion)
		}
	}
	if err := narrowed.Validate(); err != nil {
		return testrunner.Spec{}, errors.New("this candidate leaves a test spec this release will not run, so nothing can be established about it")
	}
	return narrowed, nil
}
