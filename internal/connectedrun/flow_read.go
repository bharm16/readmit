package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/testisolation"
)

// OpenFlow replays only retained readers and the existing evaluators. Nothing
// in a stored run is live authority, setup consent, or permission to resume.
func OpenFlow(ctx context.Context, path string) (FlowResult, error) {
	files, err := artifactdir.Read(path, flowResultFamily.Layout)
	if err != nil {
		return FlowResult{}, err
	}
	return openFlowFiles(ctx, path, files)
}
func openFlowFiles(ctx context.Context, path string, files map[string][]byte) (FlowResult, error) {
	var err error
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(FlowSchema, files) {
		return FlowResult{}, invalid
	}
	var r, start FlowResult
	if json.Unmarshal(files["manifest.json"], &r, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || r.Schema != FlowSchema || !safeID(r.Instance) || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) {
		return FlowResult{}, invalid
	}
	plan, err := connectedtest.OpenFlowPlan(filepath.Join(path, "plan"))
	if err != nil || plan.Identity() != r.Plan || !artifactdir.MatchesSubtree(files, "plan", connectedtest.FlowPlanSchema, artifactdir.Identity(connectedtest.FlowPlanSchema, plan.Files())) {
		return FlowResult{}, invalid
	}
	expected, e := expectedFlowStart(ctx, path, files, plan, r)
	if e != nil {
		return FlowResult{}, e
	}

	if !bytes.Equal(canonicalFlow(start), canonicalFlow(expected)) || len(r.Phases) != len(expected.Phases) || r.Boundary != expected.Boundary {
		return FlowResult{}, invalid
	}
	if r.Previous != "" {
		isolation, err := testisolation.VerifyContinuation(artifactdir.Subtree(files, "continuation"))
		if err != nil || isolation.Plan != r.Isolation || isolation.Setup != r.Setup || isolation.Cleanup != r.Cleanup {
			return FlowResult{}, invalid
		}
		if artifactdir.Identity(testisolation.ResultSchema, artifactdir.Subtree(files, "previous/isolation")) != artifactdir.Identity(testisolation.ResultSchema, artifactdir.Subtree(files, "continuation/previous")) {
			return FlowResult{}, invalid
		}
	} else if _, exists := files["transitions/result.json"]; exists {
		policy := flowTransitionPolicy(plan)
		isolation, e := testisolation.VerifyTransitionsForPolicy(artifactdir.Subtree(files, "transitions"), policy)
		if e != nil || isolation.Plan != r.Isolation || isolation.Setup != r.Setup || isolation.Cleanup != r.Cleanup {
			return FlowResult{}, invalid
		}
		// Ownership transfer retains the exact verified initial setup. Closing
		// the live session adds only an interruption checkpoint to its source.
		initial := artifactdir.Subtree(files, "isolation")
		delete(initial, "interrupted.json")
		old := artifactdir.Subtree(files, "transitions/previous")
		delete(old, "interrupted.json")
		if artifactdir.Identity(testisolation.ResultSchema, initial) != artifactdir.Identity(testisolation.ResultSchema, old) {
			return FlowResult{}, invalid
		}
		proofs, e := testisolation.TransitionProofs(artifactdir.Subtree(files, "transitions"))
		if e != nil {
			return FlowResult{}, invalid
		}
		for phase, identity := range proofs {
			found := false
			for _, result := range r.Phases {
				if result.ID == phase && result.State == "complete" && result.Verdict == assertion.VerdictPass && flowDigest(result) == identity {
					found = true
				}
			}
			if !found {
				return FlowResult{}, invalid
			}
		}
	} else if _, exists := files["isolation/plan.json"]; exists {
		nested := artifactdir.Subtree(files, "isolation")
		var isolation testisolation.Result
		if _, sealed := nested["identity.sha256"]; sealed {
			isolation, err = testisolation.Verify(nested)
		} else if r.Setup == "ready" && (r.Cleanup == "not-started" || r.Cleanup == "failed") {
			isolation, err = testisolation.VerifyHistoricalReady(nested)
		} else {
			isolation, err = testisolation.InspectRetained(nested)
		}
		if err != nil || isolation.Plan != r.Isolation || isolation.Setup != r.Setup || isolation.Cleanup != r.Cleanup {
			return FlowResult{}, invalid
		}
		var binding struct {
			Options testisolation.Options `json:"options"`
		}
		if json.Unmarshal(nested["plan.json"], &binding) != nil || binding.Options.ParentPlan != r.Plan || binding.Options.Instance != r.Instance {
			return FlowResult{}, invalid
		}
	} else if r.Isolation != "" || r.Setup != "failed" || r.Cleanup != "not-started" {
		return FlowResult{}, invalid
	}

	if validateFlowInventory(files, plan, r.Inherited) != nil {
		return FlowResult{}, invalid
	}
	known := map[string]bool{}
	for _, phase := range plan.Document().Test.Phases {
		known[phase.ID] = true
	}
	for name := range files {
		if strings.HasPrefix(name, "intents/") {
			id := strings.TrimSuffix(strings.TrimPrefix(name, "intents/"), ".json")
			if !known[id] || name != "intents/"+id+".json" {
				return FlowResult{}, invalid
			}
		}
		if strings.HasPrefix(name, "phases/") {
			rest := strings.TrimPrefix(name, "phases/")
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) != 2 || !known[parts[0]] {
				return FlowResult{}, invalid
			}
		}
		if strings.HasPrefix(name, "phase-") {
			id := strings.TrimSuffix(strings.TrimPrefix(name, "phase-"), ".json")
			if !known[id] {
				return FlowResult{}, invalid
			}
			found := false
			for _, phase := range r.Phases {
				if phase.ID == id && bytes.Equal(files[name], canonicalFlow(phase)) {
					found = true
				}
			}
			if !found {
				return FlowResult{}, invalid
			}
		}
	}
	priorTime := r.StartedAt
	for i, phase := range plan.Document().Test.Phases {
		got := r.Phases[i]
		if i < r.Inherited {
			if !bytes.Equal(canonicalFlow(got), canonicalFlow(expected.Phases[i])) {
				return FlowResult{}, invalid
			}
			if _, ok := files["intents/"+phase.ID+".json"]; ok {
				return FlowResult{}, invalid
			}
			continue
		}
		if got.ID != phase.ID {
			return FlowResult{}, invalid
		}
		decision := phaseDecision(phase, r.Phases[:i])
		raw, attempted := files["intents/"+phase.ID+".json"]
		if !attempted {
			state := "not-attempted"
			if got.State == "blocked" || got.State == "skipped" {
				if decision != got.State {
					return FlowResult{}, invalid
				}
				state = decision
			}
			if !bytes.Equal(canonicalFlow(got), canonicalFlow(unexecutedPhase(plan, phase, state))) {
				return FlowResult{}, invalid
			}
			continue
		}
		var intent flowIntent
		if decision != "ready" || json.Unmarshal(raw, &intent, json.RejectUnknownMembers(true)) != nil || intent.Phase != phase.ID || intent.Plan != plan.Phase(phase.ID).Identity() || intent.At.Before(priorTime) || intent.At.After(r.CompletedAt) {
			return FlowResult{}, invalid
		}
		prefix := "phases/" + phase.ID
		if _, sealed := files[prefix+"/identity.sha256"]; !sealed {
			partial, e := interruptedPhase(plan, phase, artifactdir.Subtree(files, prefix))
			if e != nil || !bytes.Equal(canonicalFlow(got), canonicalFlow(partial)) {
				return FlowResult{}, invalid
			}
			continue
		}
		verified, err := OpenEvidence(ctx, filepath.Join(path, "phases", phase.ID))
		child := verified.Result
		if err != nil || child.Plan != intent.Plan || child.Instance != r.Instance || child.StartedAt.Before(intent.At) || child.CompletedAt.After(r.CompletedAt) {
			return FlowResult{}, invalid
		}
		if !artifactdir.MatchesSubtree(files, prefix, PhaseSchema, verified.Identity) {
			return FlowResult{}, invalid
		}

		evaluated, err := evaluateFlowPhase(ctx, plan, phase, child, filepath.Join(path, "phases", phase.ID))
		if err != nil || !bytes.Equal(canonicalFlow(got), canonicalFlow(evaluated)) {
			return FlowResult{}, invalid
		}
		priorTime = child.CompletedAt
	}
	calculated := r
	summarizeFlow(&calculated)
	if r.State == "cancelled" && calculated.State != "uncertain" {
		calculated.State = "cancelled"
		if calculated.Verdict == assertion.VerdictPass {
			calculated.Verdict = assertion.VerdictUndecided
		}
	}
	if calculated.State != r.State || calculated.Verdict != r.Verdict {
		return FlowResult{}, invalid
	}
	return r, nil
}
