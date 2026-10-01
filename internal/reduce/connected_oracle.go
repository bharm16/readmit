package reduce

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
)

var unsupportedConnectedReduction = errors.New("connected reduction requires one original v2 phase with unconditional field checks; unsupported clauses are retained and must be reviewed in their owning editor")

// ConnectedOracleRequest selects an already verified lifecycle and its
// independently selected local configuration. Authorize must bind each newly
// prepared trial to a current reviewed action; no retained grant is copied or
// manufactured. Confirmation is supplied afresh to each runtime instance.
type ConnectedOracleRequest struct {
	Plan          *connectedtest.FlowPlan
	Configuration string
	Workspace     string
	Signature     Signature
	Confirmed     []string
	Authorize     func(context.Context, int, *connectedrun.PreparedFlow) (networkaction.Authority, error)
}

// ConnectedOracle adapts ExecuteFlow to the existing reduction algorithm.
// It supports one v2 phase, including its original typed observations and
// checks. Protocol writes, cross-phase dependencies, assignments and scheduled
// cases remain explicit unsupported clauses, never silently removed.
type ConnectedOracle struct {
	request  ConnectedOracleRequest
	messages []string
	required []string
	supplied map[string][]byte
	wire     *assertion.Set
	trials   int
	instance string
}

// InspectConnectedOracle reads the supported sequence and signature pins
// without writing trial material or acquiring any execution authority.
func InspectConnectedOracle(plan *connectedtest.FlowPlan, signature Signature) ([]string, []string, error) {
	o, err := inspectConnectedOracle(ConnectedOracleRequest{Plan: plan, Signature: signature})
	if err != nil {
		return nil, nil, err
	}
	return o.Messages(), o.Required(), nil
}

func NewConnectedOracle(request ConnectedOracleRequest) (*ConnectedOracle, error) {
	o, err := inspectConnectedOracle(request)
	if err != nil {
		return nil, err
	}
	if request.Configuration == "" || request.Authorize == nil {
		return nil, errors.New("a connected trial needs separately selected configuration and fresh reviewed authority")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.New("a fresh reduction instance could not be allocated")
	}
	o.instance = "reduction-" + hex.EncodeToString(nonce[:])
	workspace, err := artifactpath.Destination(request.Workspace)
	if err != nil {
		return nil, err
	}
	if err = os.Mkdir(workspace, 0700); err != nil {
		return nil, errors.New("the reduction workspace must be a new writable entry")
	}
	o.request.Workspace = workspace
	o.request.Confirmed = slices.Clone(request.Confirmed)
	return o, nil
}

func inspectConnectedOracle(request ConnectedOracleRequest) (*ConnectedOracle, error) {
	if err := validateSignature(request.Signature); err != nil {
		return nil, err
	}
	if request.Plan == nil {
		return nil, unsupportedConnectedReduction
	}
	d := request.Plan.Document().Test
	if d.Schema != connectedtest.FlowTestSchema && d.Schema != connectedtest.FHIRFlowTestSchema || len(d.Phases) != 1 || len(d.Phases[0].After) != 0 || d.Phases[0].When != nil || len(d.Phases[0].IsolationChanges) != 0 || len(d.Phases[0].Responses) != 0 || len(d.Phases[0].Validations) != 0 {
		return nil, unsupportedConnectedReduction
	}
	o := &ConnectedOracle{request: request, supplied: map[string][]byte{}, required: []string{}}
	files := request.Plan.Files()
	add := func(ref connectedtest.Reference) {
		raw := files["dependencies/"+ref.SHA256]
		if raw == nil {
			raw = request.Plan.Phase(d.Phases[0].ID).Files()["dependencies/"+ref.SHA256]
		}
		o.supplied[ref.File] = raw
	}
	add(d.Isolation)
	for _, ref := range d.Profiles {
		add(ref)
	}
	for _, ref := range d.Environment.Grants {
		add(ref)
	}
	if ref := d.Environment.TargetRevision.Evidence; ref != nil {
		add(*ref)
		var evidence connectedtest.RevisionEvidence
		if json.Unmarshal(o.supplied[ref.File], &evidence, json.RejectUnknownMembers(true)) != nil {
			return nil, unsupportedConnectedReduction
		}
		add(evidence.Source)
	}
	for _, server := range d.Servers {
		add(server.Capability)
	}
	stepByID := map[string]connectedtest.Step{}
	occurrences := map[string]bool{}
	for _, step := range d.Steps {
		if step.V2 == nil || len(step.V2.Assignments) != 0 || len(step.After) != 0 || occurrences[step.V2.Occurrence] {
			return nil, unsupportedConnectedReduction
		}
		occurrences[step.V2.Occurrence] = true
		stepByID[step.ID] = step
		add(step.V2.Input)
	}
	for _, id := range d.Phases[0].Steps {
		step, ok := stepByID[id]
		if !ok {
			return nil, unsupportedConnectedReduction
		}
		o.messages = append(o.messages, step.V2.Occurrence)
	}
	declared := map[string]bool{}
	phase := d.Phases[0]
	for _, ds := range phase.Datasets {
		if ds.Projection != nil {
			add(*ds.Projection)
		}
		if ds.Completion.Policy != nil {
			add(*ds.Completion.Policy)
		}
		if ds.Completion.Barrier != nil {
			add(*ds.Completion.Barrier)
		}
	}
	add(phase.Checks)
	checks, err := assertion.DecodeDatasets(o.supplied[phase.Checks.File])
	if err != nil {
		return nil, err
	}
	for _, check := range checks.Document().Assertions {
		declared[check.ID] = true
	}
	if w := phase.Wire; w != nil {
		if w.Observed != "transport-acks" || w.Before != nil || w.After != nil || len(w.Acknowledgements) != 0 {
			return nil, unsupportedConnectedReduction
		}
		add(w.Set)
		set, err := assertion.Decode(o.supplied[w.Set.File])
		if err != nil {
			return nil, err
		}
		o.wire = &set
		for _, check := range set.Assertions {
			if declared[check.ID] || check.When != nil || check.Subject.Field == nil || check.Subject.Field.Scope != assertion.ObservedMessages {
				return nil, unsupportedConnectedReduction
			}
			declared[check.ID] = true
			if slices.Contains(request.Signature.Assertions, check.ID) && !slices.Contains(o.required, check.Subject.Field.Message) {
				o.required = append(o.required, check.Subject.Field.Message)
			}
		}
	}
	for _, id := range request.Signature.Assertions {
		if !declared[id] {
			return nil, errors.New("this signature names a check the connected lifecycle does not declare")
		}
	}
	return o, nil
}

func (o *ConnectedOracle) Messages() []string { return slices.Clone(o.messages) }
func (o *ConnectedOracle) Required() []string { return slices.Clone(o.required) }

// Connected setup cannot be claimed outside its complete lifecycle call.
func (o *ConnectedOracle) Reset(context.Context) (fixturereset.Outcome, fixturereset.Reason) {
	return fixturereset.Unconfirmed, fixturereset.AwaitingOperator
}
func (o *ConnectedOracle) Observe(context.Context, []string) (Observation, error) {
	return Observation{}, errors.New("connected trials execute setup and cleanup through RunTrial")
}

func (o *ConnectedOracle) narrow(occurrences []string) (*connectedtest.FlowPlan, error) {
	d := o.request.Plan.Document()
	held := map[string]bool{}
	for _, id := range occurrences {
		if !slices.Contains(o.messages, id) || held[id] {
			return nil, unsupportedConnectedReduction
		}
		held[id] = true
	}
	phase := &d.Test.Phases[0]
	steps := map[string]bool{}
	d.Test.Steps = slices.DeleteFunc(d.Test.Steps, func(step connectedtest.Step) bool {
		keep := held[step.V2.Occurrence]
		if keep {
			steps[step.ID] = true
		}
		return !keep
	})
	phase.Steps = slices.DeleteFunc(phase.Steps, func(id string) bool { return !steps[id] })
	supplied := make(map[string][]byte, len(o.supplied))
	for file, raw := range o.supplied {
		supplied[file] = slices.Clone(raw)
	}
	if o.wire != nil {
		set := *o.wire
		set.Assertions = slices.Clone(set.Assertions)
		set.Assertions = slices.DeleteFunc(set.Assertions, func(check assertion.Assertion) bool { return !held[check.Subject.Field.Message] })
		if len(set.Assertions) == 0 {
			return nil, unsupportedConnectedReduction
		}
		raw, err := json.Marshal(set, json.Deterministic(true))
		if err != nil {
			return nil, err
		}
		supplied[phase.Wire.Set.File] = raw
		phase.Wire.Set.SHA256 = connectedtest.Digest(raw)
	}
	raw, err := json.Marshal(d.Test, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return connectedtest.CompileFlow(raw, supplied, d.Generation)
}

// RunTrial spends one fresh instance, exact authority, full observation and
// owned cleanup. Only an offline-verified, complete lifecycle with clean
// cleanup can answer whether its unchanged failure signature reproduced.
func (o *ConnectedOracle) RunTrial(ctx context.Context, occurrences []string) (fixturereset.Outcome, fixturereset.Reason, Observation, error) {
	narrowed, err := o.narrow(occurrences)
	if err != nil {
		return fixturereset.Unconfirmed, fixturereset.AwaitingOperator, Observation{}, err
	}
	o.trials++
	name := fmt.Sprintf("t%04d", o.trials)
	instance := fmt.Sprintf("%s-%d", o.instance, o.trials)
	planPath := filepath.Join(o.request.Workspace, name+"-plan")
	if err = narrowed.Write(ctx, planPath); err != nil {
		return fixturereset.Unconfirmed, fixturereset.AwaitingOperator, Observation{}, err
	}
	p, err := connectedrun.PrepareFlow(planPath, o.request.Configuration, instance)
	if err != nil {
		return fixturereset.Unconfirmed, fixturereset.AwaitingOperator, Observation{}, err
	}
	authority, err := o.request.Authorize(ctx, o.trials, p)
	if err != nil || authority == nil {
		return fixturereset.Unconfirmed, fixturereset.AwaitingOperator, Observation{}, errors.New("this trial has no fresh reviewed authority")
	}
	trialCtx := networkaction.WithAuthority(ctx, authority)
	output := filepath.Join(o.request.Workspace, name)
	_, err = connectedrun.ExecuteFlow(trialCtx, p, output, testisolation.Confirmation{Plan: p.IsolationIdentity(), Instance: instance, Steps: slices.Clone(o.request.Confirmed)})
	if err != nil {
		return fixturereset.Unconfirmed, fixturereset.AwaitingOperator, Observation{}, err
	}
	r, err := connectedrun.OpenFlow(context.WithoutCancel(ctx), output)
	if err != nil {
		return fixturereset.Unconfirmed, fixturereset.AwaitingOperator, Observation{}, err
	}
	observed := Observation{Failed: []string{}}
	for _, phase := range r.Phases {
		for _, check := range phase.Checks {
			if check.Outcome == assertion.OutcomeFailed {
				_, id, _ := strings.Cut(check.ID, ":")
				observed.Failed = append(observed.Failed, id)
			}
			if check.Outcome == assertion.OutcomeUndecided || check.Outcome == assertion.OutcomeSkipped {
				observed.State = durablerun.ExecutionError
			}
		}
	}
	if r.State == "uncertain" {
		observed.DeliveryUncertain = true
	}
	if r.Cleanup != "complete" && r.Cleanup != "not-started" {
		observed.StopReason = CleanupUnresolved
	}
	if r.Setup != "ready" {
		return fixturereset.Unconfirmed, fixturereset.AwaitingOperator, observed, nil
	}
	if r.Cleanup != "complete" {
		observed.StopReason = CleanupUnresolved
	}
	if observed.State == "" {
		switch {
		case r.State == "cancelled":
			observed.State = durablerun.Cancelled
		case r.State != "complete":
			observed.State = durablerun.ExecutionError
		case r.Verdict == assertion.VerdictPass:
			observed.State = durablerun.Passed
		case r.Verdict == assertion.VerdictFail:
			observed.State = durablerun.AssertionFailed
		default:
			observed.State = durablerun.ExecutionError
		}
	}
	if observed.State == durablerun.AssertionFailed && !o.request.Signature.matches(observed.Failed) {
		observed.StopReason = FailureSignatureChanged
	}
	return fixturereset.Confirmed, fixturereset.EveryActionConfirmed, observed, nil
}
