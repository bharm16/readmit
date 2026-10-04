package connectedrun

import (
	"context"
	"encoding/json/v2"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/replay"
)

// FlowEvidence is a verified retained lifecycle with the evidence its verdicts
// were derived from: the plan it pinned and, for every attempted phase, the
// observation tables its evaluator read, the response values it used and bound,
// and the validator runs its validation checks retained. Everything is re-read
// from retained bytes by the same readers OpenFlow uses; nothing is contacted.
type FlowEvidence struct {
	Result   FlowResult
	Identity string
	Plan     *connectedtest.FlowPlan
	Phases   map[string]PhaseEvidence
}

// PhaseEvidence is one attempted phase's retained evidence. Tables holds the
// final after-phase and before-phase observations the evaluator read, keyed by
// dataset ID, and Observations where each is retained below the phase; a
// dataset whose observation never completed has neither.
// Inputs are response values the phase used from earlier phases and Bound the
// values its own responses supplied: the runtime identity mapping, which is
// server-assigned addressing, never expected clinical truth.
type PhaseEvidence struct {
	Intervals    map[string]observeinterval.Result
	Captures     map[string]*bundle.Bundle
	HTTPStatus   map[string]int
	Transport    *replay.Run
	Tables       map[string]assertion.Table
	Observations map[string]string
	Boundaries   map[string]string
	Inputs       map[string]string
	Bound        map[string]string
	Steps        []FHIRStepRecord
	Validators   map[string]ValidatorRun
}

// ValidatorRun is what one retained validation recorded about the validator:
// the capability it ran, the container engine version when it ran at all, and
// the runtime state when no worker produced an outcome.
type ValidatorRun struct {
	Capability    string `json:"capability"`
	EngineVersion string `json:"engine_version,omitzero"`
	RuntimeState  string `json:"runtime_state,omitzero"`
}

// OpenFlowEvidence verifies a retained lifecycle exactly as OpenFlow does and
// then answers the evidence behind its verdicts. It never repeats a request.
func OpenFlowEvidence(ctx context.Context, path string) (FlowEvidence, error) {
	files, err := artifactdir.Read(path, flowResultFamily.Layout)
	if err != nil {
		return FlowEvidence{}, err
	}
	return verifyFlowEvidence(ctx, path, files)
}

// VerifyFlowEvidence owns one captured lifecycle snapshot. Its plans, phases,
// evaluations and final tables are verified without reopening any directory.
func VerifyFlowEvidence(ctx context.Context, captured map[string][]byte) (FlowEvidence, error) {
	files, err := artifactdir.Snapshot(captured, flowResultFamily.Layout)
	if err != nil {
		return FlowEvidence{}, err
	}
	return verifyFlowEvidence(ctx, "retained-lifecycle", files)
}
func verifyFlowEvidence(ctx context.Context, path string, files map[string][]byte) (FlowEvidence, error) {
	r, err := openFlowFiles(ctx, path, files)
	if err != nil {
		return FlowEvidence{}, err
	}
	plan, err := connectedtest.VerifyFlowPlan(artifactdir.Subtree(files, "plan"))
	if err != nil {
		return FlowEvidence{}, invalid
	}
	e := FlowEvidence{Result: r, Identity: artifactdir.Identity(r.Schema, files), Plan: plan, Phases: map[string]PhaseEvidence{}}
	fhir := plan.Document().Schema == connectedtest.FHIRFlowPlanSchema
	bound := map[string]string{}
	for i, phase := range plan.Document().Test.Phases {
		prefix := "phases/" + phase.ID
		if i < r.Inherited {
			continue
		}
		if _, sealed := files[prefix+"/identity.sha256"]; !sealed {
			continue
		}
		dir := filepath.Join(path, "phases", phase.ID)
		pe := PhaseEvidence{Tables: map[string]assertion.Table{}, Observations: map[string]string{}, Boundaries: map[string]string{}, Inputs: map[string]string{}, Bound: map[string]string{}, Steps: []FHIRStepRecord{}, Validators: map[string]ValidatorRun{}, HTTPStatus: map[string]int{}, Captures: map[string]*bundle.Bundle{}, Intervals: map[string]observeinterval.Result{}}
		if fhir {
			_, run, _, tables, err := openFHIRPhaseTables(ctx, plan, phase, dir, artifactdir.Subtree(files, prefix), bound)
			if err != nil {
				return FlowEvidence{}, invalid
			}
			for k, v := range run.Bound {
				bound[k] = v
			}
			pe.Tables, pe.Boundaries, pe.Inputs, pe.Bound, pe.Steps = tables, run.Boundaries, run.Inputs, run.Bound, run.Steps
			for _, definition := range phase.Datasets {
				if definition.Kind == "typed-rows" {
					if _, ok := files[prefix+"/intervals/"+definition.ID+"/manifest.json"]; ok {
						interval, err := observeinterval.Verify(ctx, artifactdir.Subtree(files, prefix+"/intervals/"+definition.ID))
						if err != nil {
							return FlowEvidence{}, invalid
						}
						pe.Intervals[definition.ID] = interval
					}
				}
			}
			for _, step := range run.Steps {
				if step.Result != "" {
					response, err := fhirrest.VerifyEvidence(ctx, artifactdir.Subtree(files, prefix+"/steps/"+step.Step))
					if err != nil {
						return FlowEvidence{}, invalid
					}
					actual := response.Result()
					if len(actual.Attempts) > 0 {
						pe.HTTPStatus[step.Step] = actual.Attempts[len(actual.Attempts)-1].Receipt.HTTPStatus
					}
				}
			}
			for id := range tables {
				pe.Observations[id] = run.Observations[id]
			}
			for id, identity := range run.Validations {
				var result fhirvalidator.Result
				if json.Unmarshal(files[prefix+"/validations/"+id+"/result.json"], &result) != nil || artifactdir.Identity(fhirvalidator.ResultSchema, artifactdir.Subtree(files, prefix+"/validations/"+id)) != identity {
					return FlowEvidence{}, invalid
				}
				v := ValidatorRun{Capability: result.Capability}
				if result.Engine != nil {
					v.EngineVersion = result.Engine.Version
				}
				if result.RuntimeStatus != nil {
					v.RuntimeState = result.RuntimeStatus.State
				}
				pe.Validators[id] = v
			}
		} else {
			verified, err := VerifyEvidence(ctx, artifactdir.Subtree(files, prefix))
			if err != nil {
				return FlowEvidence{}, invalid
			}
			pe.Boundaries = verified.Result.Boundaries()
			for _, ds := range phase.Datasets {
				if _, ok := files[prefix+"/intervals/"+ds.ID+"/manifest.json"]; ok {
					interval, err := observeinterval.Verify(ctx, artifactdir.Subtree(files, prefix+"/intervals/"+ds.ID))
					if err != nil {
						return FlowEvidence{}, invalid
					}
					pe.Intervals[ds.ID] = interval
				}
				if _, ok := files[prefix+"/intervals/"+ds.ID+"/capture/identity.sha256"]; ok {
					capture, err := networkaction.VerifyCaptureEvidence(artifactdir.Subtree(files, prefix+"/intervals/"+ds.ID+"/capture"))
					if err != nil {
						return FlowEvidence{}, invalid
					}
					pe.Captures[ds.ID] = capture.Capture
				}
				name := prefix + "/evaluation/datasets/" + ds.ID + "/identity.sha256"
				if _, ok := files[name]; !ok {
					continue
				}
				snapshot, err := dataset.Verify(ctx, artifactdir.Subtree(files, prefix+"/evaluation/datasets/"+ds.ID))
				if err != nil {
					return FlowEvidence{}, invalid
				}
				pe.Tables[ds.ID] = assertion.SnapshotTable(snapshot)
				pe.Observations[ds.ID] = "evaluation/datasets/" + ds.ID
			}
		}
		if _, ok := files[prefix+"/transport/run/identity.sha256"]; ok {
			transport, err := replay.Verify(artifactdir.Subtree(files, prefix+"/transport/run"))
			if err != nil {
				return FlowEvidence{}, invalid
			}
			pe.Transport = transport
		}
		e.Phases[phase.ID] = pe
	}
	return e, nil
}
