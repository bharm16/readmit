package testrunner

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
)

// Run explicitly sends a spec, or writes a configuration-error result when local
// preparation fails. It cannot produce an artifact if storage is unavailable or
// the output would modify existing evidence. It never executes reset prose.
func Run(ctx context.Context, specPath, output string) (*Artifact, error) {
	return RunWithDurability(ctx, specPath, output, artifactdir.Durable)
}

// RunWithDurability is Run with the durability its caller chose for the result
// and run it writes: Scratch only for a result in a throwaway workspace its
// owner removes before it answers, keeping any copy through its own synced
// write. The result's bytes, identity and verdict do not depend on it.
func RunWithDurability(ctx context.Context, specPath, output string, durability artifactdir.Durability) (*Artifact, error) {
	plan, err := PrepareWithDurability(specPath, durability)
	if err == nil {
		return Execute(ctx, plan, output)
	}
	writer, err := artifactdir.Create(output, resultFamily, durability)
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	result := emptyResult()
	result.ErrorClass = "configuration"
	if raw, err := readLocal(specPath, MaxSpecBytes); err == nil {
		ref, err := retain(writer, "spec.json", raw)
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
	return finish(writer, result)
}

// Execute opens a network connection only after reserving new local evidence
// storage and verifying the declared initial ledger state. No retry is hidden.
func Execute(ctx context.Context, plan *Plan, output string) (*Artifact, error) {
	return ExecuteObserved(ctx, plan, output, nil)
}

// ExecuteObserved adds synchronous durability notifications without changing the
// frozen result/run contracts or the checked destination policy boundary.
func ExecuteObserved(ctx context.Context, plan *Plan, output string, observer replay.Observer) (*Artifact, error) {
	if plan == nil || plan.replay == nil {
		return nil, errors.New("test requires a prepared plan")
	}
	// The result is made outside the case it sends, and the folder holding it
	// is synced last, so one this result cannot open is refused before
	// anything is created or sent.
	writer, err := artifactdir.Create(output, resultFamily, plan.durability, plan.sourceInfo)
	if err != nil {
		return nil, err
	}
	defer writer.Close()
	dir := writer.Path()
	result := emptyResult()
	result.Spec, err = retain(writer, "spec.json", plan.raw)
	if err != nil {
		return nil, err
	}
	result.SpecIdentity = result.Spec.SHA256
	result.InputBundleIdentity = plan.replay.SourceIdentity()
	target := plan.replay.Target()
	result.Target = &target
	result.TargetIdentity = target.Identity()
	result.ObservationBoundary = plan.Boundary()
	result.Assertions = pending(plan.spec)
	currentSpec, err := readLocal(plan.specPath, MaxSpecBytes)
	if err != nil || !bytes.Equal(currentSpec, plan.raw) {
		result.ErrorClass = "configuration_changed"
		return plan.named(finish(writer, result))
	}
	var initial, final *observation.Snapshot
	if plan.Boundary() == LedgerBoundary {
		result.InitialObservation, initial, err = collectObservation(writer, "initial-observation.json", plan.observationPath)
		if err != nil {
			return nil, err
		}
		if !initialState(initial) {
			result.ErrorClass = "initial_observation"
			return plan.named(finish(writer, result))
		}
	}
	run, err := replay.Send(ctx, plan.replay, filepath.Join(dir, "run"), replay.SendOptions{
		Observer:           observer,
		DecisionPath:       dir + replay.DecisionSuffix,
		DecisionDurability: plan.durability,
	})
	if err != nil {
		// A storage failure may leave an incomplete run. Do not certify that
		// directory with a completed result identity or fabricate observations.
		return nil, errors.New("cannot execute or finalize test replay; retained output may be incomplete: " + err.Error())
	}
	result.Run = &RunReference{Path: "run", Identity: run.Identity}
	if plan.Boundary() == LedgerBoundary {
		result.FinalObservation, final, err = collectObservation(writer, "observation.json", plan.observationPath)
		if err != nil {
			return nil, err
		}
	}
	result.Status, result.ErrorClass, result.Assertions = evaluate(plan.spec, run, initial, final)
	if result.Status != ExecutionError && final != nil {
		result.ReceiverSessionID, result.ReceiverMode = final.SessionID, final.Mode
	}
	return plan.named(finish(writer, result))
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
func collectObservation(writer *artifactdir.Writer, name, path string) (*bundle.Payload, *observation.Snapshot, error) {
	raw, err := readLocal(path, observation.MaxBytes)
	if err != nil {
		return nil, nil, nil
	}
	ref, err := retain(writer, name, raw)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := observation.Decode(raw)
	if err != nil {
		return ref, nil, nil
	}
	return ref, &snapshot, nil
}
