package connectedrun

import (
	"context"
	"encoding/json/v2"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirvalidator"
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
	r, err := openFlowFiles(ctx, path, files)
	if err != nil {
		return FlowEvidence{}, err
	}
	plan, err := connectedtest.OpenFlowPlan(filepath.Join(path, "plan"))
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
		pe := PhaseEvidence{Tables: map[string]assertion.Table{}, Observations: map[string]string{}, Boundaries: map[string]string{}, Inputs: map[string]string{}, Bound: map[string]string{}, Steps: []FHIRStepRecord{}, Validators: map[string]ValidatorRun{}}
		if fhir {
			_, run, _, tables, err := openFHIRPhaseTables(ctx, plan, phase, dir, artifactdir.Subtree(files, prefix), bound)
			if err != nil {
				return FlowEvidence{}, invalid
			}
			for k, v := range run.Bound {
				bound[k] = v
			}
			pe.Tables, pe.Boundaries, pe.Inputs, pe.Bound, pe.Steps = tables, run.Boundaries, run.Inputs, run.Bound, run.Steps
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
			verified, err := OpenEvidence(ctx, dir)
			if err != nil {
				return FlowEvidence{}, invalid
			}
			pe.Boundaries = verified.Result.Boundaries()
			for _, ds := range phase.Datasets {
				name := prefix + "/evaluation/datasets/" + ds.ID + "/identity.sha256"
				if _, ok := files[name]; !ok {
					continue
				}
				snapshot, err := dataset.Open(ctx, filepath.Join(dir, "evaluation", "datasets", ds.ID))
				if err != nil {
					return FlowEvidence{}, invalid
				}
				pe.Tables[ds.ID] = assertion.SnapshotTable(snapshot)
				pe.Observations[ds.ID] = "evaluation/datasets/" + ds.ID
			}
		}
		e.Phases[phase.ID] = pe
	}
	return e, nil
}
