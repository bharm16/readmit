package testrunner

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
)

// Run explicitly sends a spec, or writes a configuration-error result when local
// preparation fails. It cannot produce an artifact if storage is unavailable or
// the output would modify existing evidence. It never executes reset prose.
func Run(ctx context.Context, specPath, output string) (*Artifact, error) {
	plan, err := Prepare(specPath)
	if err == nil {
		return Execute(ctx, plan, output)
	}
	dir, err := reserve(output, nil)
	if err != nil {
		return nil, err
	}
	result := emptyResult()
	result.ErrorClass = "configuration"
	if raw, err := readLocal(specPath, MaxSpecBytes); err == nil {
		ref, err := retain(dir, "spec.json", raw)
		if err != nil {
			return nil, err
		}
		result.Spec = ref
		result.SpecIdentity = ref.SHA256
		if spec, err := DecodeSpec(raw); err == nil {
			result.ObservationBoundary = spec.Observation.Boundary
			result.Assertions = pending(spec)
		}
	}
	return finish(dir, result)
}

// Execute opens a network connection only after reserving new local evidence
// storage and verifying the declared initial ledger state. No retry is hidden.
func Execute(ctx context.Context, plan *Plan, output string) (*Artifact, error) {
	if plan == nil || plan.replay == nil {
		return nil, errors.New("test requires a prepared plan")
	}
	dir, err := reserve(output, plan.sourceInfo)
	if err != nil {
		return nil, err
	}
	result := emptyResult()
	result.Spec, err = retain(dir, "spec.json", plan.raw)
	if err != nil {
		return nil, err
	}
	result.SpecIdentity = result.Spec.SHA256
	result.InputBundleIdentity = plan.replay.SourceIdentity()
	target := plan.replay.Target()
	result.Target = &target
	result.TargetIdentity = targetIdentity(target)
	result.ObservationBoundary = plan.Boundary()
	result.Assertions = pending(plan.spec)
	currentSpec, err := readLocal(plan.specPath, MaxSpecBytes)
	if err != nil || !bytes.Equal(currentSpec, plan.raw) {
		result.ErrorClass = "configuration_changed"
		return plan.named(finish(dir, result))
	}
	var initial, final *observation.Snapshot
	if plan.Boundary() == LedgerBoundary {
		result.InitialObservation, initial, err = collectObservation(dir, "initial-observation.json", plan.observationPath)
		if err != nil {
			return nil, err
		}
		if !initialState(initial) {
			result.ErrorClass = "initial_observation"
			return plan.named(finish(dir, result))
		}
	}
	run, err := replay.Execute(ctx, plan.replay, filepath.Join(dir, "run"))
	if err != nil {
		// A storage failure may leave an incomplete run. Do not certify that
		// directory with a completed result identity or fabricate observations.
		return nil, errors.New("cannot execute or finalize test replay; retained output may be incomplete")
	}
	result.Run = &RunReference{Path: "run", Identity: run.Identity}
	if plan.Boundary() == LedgerBoundary {
		result.FinalObservation, final, err = collectObservation(dir, "observation.json", plan.observationPath)
		if err != nil {
			return nil, err
		}
	}
	result.Status, result.ErrorClass, result.Assertions = evaluate(plan.spec, run, initial, final)
	if result.Status != ExecutionError && final != nil {
		result.ReceiverSessionID, result.ReceiverMode = final.SessionID, final.Mode
	}
	return plan.named(finish(dir, result))
}

// named records the environment an execution was pointed at on the artifact it
// produced, so a console states what a send was aimed at. A reopened result
// carries none: readmit-result/v1 is frozen and records the transport rather
// than the environment, and a spec whose configuration never validated names no
// environment at all.
func (p *Plan) named(artifact *Artifact, err error) (*Artifact, error) {
	if artifact != nil {
		artifact.Environment = p.Environment()
	}
	return artifact, err
}

func emptyResult() Result {
	return Result{Schema: Schema, State: "complete", ContainsSourceValues: true, ExportPolicy: "customer-local-only", Status: ExecutionError, Assertions: []AssertionResult{}}
}

// A bounded partial/invalid snapshot is retained as diagnostic evidence. Missing
// and oversized files have no descriptor. Either case evaluates to an error.
func collectObservation(dir, name, path string) (*bundle.Payload, *observation.Snapshot, error) {
	raw, err := readLocal(path, observation.MaxBytes)
	if err != nil {
		return nil, nil, nil
	}
	ref, err := retain(dir, name, raw)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := observation.Decode(raw)
	if err != nil {
		return ref, nil, nil
	}
	return ref, &snapshot, nil
}
