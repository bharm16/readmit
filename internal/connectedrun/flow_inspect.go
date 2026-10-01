package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/testisolation"
)

// InspectFlow reports an interrupted writer without turning an intent into a
// successful attempt or reconstructing permission to continue it.
func InspectFlow(ctx context.Context, path string) (FlowResult, error) {
	files, err := artifactdir.Read(path, flowResultFamily.Layout)
	if err != nil {
		return FlowResult{}, err
	}
	if _, ok := files["identity.sha256"]; ok {
		return openFlowFiles(ctx, path, files)
	}
	var start FlowResult
	if json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || start.Schema != FlowSchema && start.Schema != FlowSchemaV4 || start.StartedAt.IsZero() || !safeID(start.Instance) {
		return FlowResult{}, invalid
	}
	plan, err := connectedtest.VerifyFlowPlan(artifactdir.Subtree(files, "plan"))
	if err != nil || plan.Identity() != start.Plan || flowSchemaFor(plan) != start.Schema || !artifactdir.MatchesSubtree(files, "plan", plan.Document().Schema, artifactdir.Identity(plan.Document().Schema, plan.Files())) {
		return FlowResult{}, invalid
	}
	fhir := plan.Document().Schema == connectedtest.FHIRFlowPlanSchema
	bound := map[string]string{}
	expected, e := expectedFlowStart(ctx, path, files, plan, start)
	if e != nil {
		return FlowResult{}, e
	}

	if !bytes.Equal(canonicalFlow(start), canonicalFlow(expected)) {
		return FlowResult{}, invalid
	}
	if validateFlowInventory(files, plan, start.Inherited) != nil {
		return FlowResult{}, invalid
	}
	r := start
	r.State = "incomplete"
	isolation, present, e := inspectFlowIsolation(files, plan, start)
	if e != nil {
		return FlowResult{}, e
	}
	if present {
		r.Setup = isolation.Setup
		r.Cleanup = isolation.Cleanup
		r.Isolation = isolation.Plan
	}

	for i, phase := range plan.Document().Test.Phases {
		if i < start.Inherited {
			continue
		}
		raw, ok := files["intents/"+phase.ID+".json"]
		if !ok {
			continue
		}
		if phaseDecision(phase, r.Phases[:i]) != "ready" {
			return FlowResult{}, invalid
		}
		var intent flowIntent
		if json.Unmarshal(raw, &intent, json.RejectUnknownMembers(true)) != nil || intent.Phase != phase.ID || intent.Plan != plan.Phase(phase.ID).Identity() || intent.At.Before(r.StartedAt) {
			return FlowResult{}, invalid
		}
		if fhir {
			prefix := "phases/" + phase.ID
			if _, sealed := files[prefix+"/identity.sha256"]; sealed {
				evaluated, child, identity, e := openFHIRPhase(ctx, plan, phase, filepath.Join(path, "phases", phase.ID), artifactdir.Subtree(files, prefix), bound)
				if e != nil || child.Plan != intent.Plan || child.Instance != r.Instance || !artifactdir.MatchesSubtree(files, prefix, PhaseSchemaV2, identity) {
					return FlowResult{}, invalid
				}
				if raw, ok := files["phase-"+phase.ID+".json"]; ok && !bytes.Equal(raw, canonicalFlow(evaluated)) {
					return FlowResult{}, invalid
				}
				for k, v := range child.Bound {
					bound[k] = v
				}
				r.Phases[i] = evaluated
				continue
			}
			partial, e := interruptedFHIRPhase(plan, phase, artifactdir.Subtree(files, prefix))
			if e != nil {
				return FlowResult{}, e
			}
			r.Phases[i] = partial
			continue
		}
		verified, e := VerifyEvidence(ctx, artifactdir.Subtree(files, "phases/"+phase.ID))
		child := verified.Result
		if e == nil && child.Plan == intent.Plan && child.Instance == r.Instance && !child.StartedAt.Before(intent.At) && artifactdir.MatchesSubtree(files, "phases/"+phase.ID, phaseSchemaFor(plan), verified.Identity) {
			evaluated, e := evaluateOwnedPhase(ctx, plan, phase, verified)
			if e == nil {
				if raw, ok := files["phase-"+phase.ID+".json"]; ok && !bytes.Equal(raw, canonicalFlow(evaluated)) {
					return FlowResult{}, invalid
				}
				r.Phases[i] = evaluated
				continue
			}
		}
		if _, sealed := files["phases/"+phase.ID+"/identity.sha256"]; sealed {
			return FlowResult{}, invalid
		}
		partial, e := interruptedPhase(plan, phase, artifactdir.Subtree(files, "phases/"+phase.ID))
		if e != nil {
			return FlowResult{}, e
		}
		r.Phases[i] = partial
	}
	summarizeFlow(&r)
	// An unfinished parent cannot claim clean completion even when its last
	// retained child settled. Cleanup/parent completion may not have happened.
	if r.State == "complete" {
		r.State = "incomplete"
	}
	if r.Verdict == assertion.VerdictPass {
		r.Verdict = assertion.VerdictUndecided
	}
	return r, nil
}

func interruptedPhase(plan *connectedtest.FlowPlan, phase connectedtest.FlowPhase, files map[string][]byte) (FlowPhaseResult, error) {
	r := unexecutedPhase(plan, phase, "not-attempted")
	r.State = "incomplete"
	for name, raw := range files {
		if !strings.HasPrefix(name, "transport/intents/") {
			continue
		}
		// A scheduled phase's intent also records its delay and when its send
		// began; either form is read strictly.
		var intent struct {
			Step            string `json:"step"`
			Occurrence      string `json:"occurrence"`
			State           string `json:"state"`
			DeclaredDelayMS *int64 `json:"declared_delay_ms,omitzero"`
			StartedAfterMS  *int64 `json:"started_after_ms,omitzero"`
		}
		scheduled := plan.Schedule(phase.ID) != nil
		if json.Unmarshal(raw, &intent, json.RejectUnknownMembers(true)) != nil || intent.State != "uncertain-until-settled" || scheduled != (intent.DeclaredDelayMS != nil) || scheduled != (intent.StartedAfterMS != nil) {
			return r, invalid
		}
		found := false
		for i, id := range phase.Steps {
			if intent.Step == id && intent.Occurrence == fmt.Sprintf("o%06d", i+1) && name == "transport/intents/"+intent.Occurrence+".json" {
				found = true
				r.Steps[i].Outcome = "unknown"
				r.Steps[i].Uncertain = true
				r.State = "uncertain"
			}
		}
		if !found {
			return r, invalid
		}
	}
	return r, nil
}

func inspectFlowIsolation(files map[string][]byte, plan *connectedtest.FlowPlan, start FlowResult) (testisolation.Result, bool, error) {
	var result testisolation.Result
	var err error
	var initial map[string][]byte
	continuation := artifactdir.Subtree(files, "continuation")
	transitions := artifactdir.Subtree(files, "transitions")
	original := artifactdir.Subtree(files, "isolation")
	switch {
	case len(continuation) > 0:
		if start.Previous == "" {
			return result, true, invalid
		}
		if _, sealed := continuation["identity.sha256"]; sealed {
			result, err = testisolation.VerifyContinuation(continuation)
		} else {
			result, err = testisolation.InspectContinuation(continuation)
		}
		initial = artifactdir.Subtree(continuation, "previous")
		if artifactdir.Identity(testisolation.ResultSchema, initial) != artifactdir.Identity(testisolation.ResultSchema, artifactdir.Subtree(files, "previous/isolation")) {
			return result, true, invalid
		}
		result.Setup = "interrupted"
	case len(transitions) > 0:
		policy := flowTransitionPolicy(plan)
		if start.Previous != "" || len(policy.Phases) == 0 {
			return result, true, invalid
		}
		if _, sealed := transitions["identity.sha256"]; sealed {
			result, err = testisolation.VerifyTransitionsForPolicy(transitions, policy)
		} else {
			result, err = testisolation.InspectTransitions(transitions, policy)
		}
		initial = artifactdir.Subtree(transitions, "previous")
		held := artifactdir.Subtree(files, "isolation")
		delete(held, "interrupted.json")
		copied := artifactdir.Subtree(transitions, "previous")
		delete(copied, "interrupted.json")
		if artifactdir.Identity(testisolation.ResultSchema, held) != artifactdir.Identity(testisolation.ResultSchema, copied) {
			return result, true, invalid
		}
		result.Setup = "interrupted"
	case len(original) > 0:
		result, err = testisolation.InspectRetained(original)
		initial = original
	default:
		return result, false, nil
	}
	if err != nil {
		return result, true, err
	}
	var bound struct {
		Options testisolation.Options `json:"options"`
	}
	if json.Unmarshal(initial["plan.json"], &bound) != nil || bound.Options.ParentPlan != start.Plan || bound.Options.Instance != start.Instance {
		return result, true, invalid
	}
	result.Complete = false
	result.Manual = nil
	return result, true, nil
}
