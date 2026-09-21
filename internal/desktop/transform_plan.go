package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/transform"
)

// TransformPlanRequest authors one transformation plan over the verified case.
// Rules names the correlation-rules document whose relations are preserved;
// Profile optionally pins one pack entry; Steps are the typed operators the
// engine already supports. Output is the new workspace entry the canonical
// plan is written into.
type TransformPlanRequest struct {
	Workspace string           `json:"workspace"`
	Case      string           `json:"case"`
	Identity  string           `json:"identity"`
	Rules     string           `json:"rules"`
	Profile   string           `json:"profile,omitzero"`
	Steps     []transform.Step `json:"steps"`
	Output    string           `json:"output"`
}

// AuthoredTransformPlan is one plan as the window holds it after a save: the
// document entry, the rules digest the plan pinned, and the plan itself.
type AuthoredTransformPlan struct {
	Output   string         `json:"output"`
	Rules    string         `json:"rules"`
	Digest   string         `json:"digest"`
	Plan     transform.Plan `json:"plan"`
	Boundary string         `json:"boundary"`
}

// TransformPlanResult carries one state. Plan is present whenever the document
// was written and re-read through the same decoder a preview uses.
type TransformPlanResult struct {
	State  State                  `json:"state"`
	Reason string                 `json:"reason,omitzero"`
	Plan   *AuthoredTransformPlan `json:"plan,omitzero"`
}

func (r *TransformPlanResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) transformPlan() TransformPlanResult {
	return TransformPlanResult{State: r.state, Reason: r.reason}
}

const transformPlanBoundary = "A transformation plan names the relations it preserves and the steps a preview " +
	"would apply. Saving one writes a new document beside the evidence and never changes the original case, " +
	"never redacts in place, and never produces a derived bundle by itself."

// SaveTransformPlan validates one authored plan through the shared reader and
// writes its canonical form into one new workspace entry. The rules digest and
// optional profile pin are taken from the documents named here, so a plan
// cannot claim relations or a pack it was not authored against. Original
// evidence is never touched.
func (a *App) SaveTransformPlan(request TransformPlanRequest) TransformPlanResult {
	return run(a, false, true, func(context.Context) TransformPlanResult {
		return a.saveTransformPlan(request)
	})
}

func (a *App) saveTransformPlan(request TransformPlanRequest) TransformPlanResult {
	root, source, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return declined.transformPlan()
	}
	rulesData, declined := workspaceDocument(root, request.Rules, correlate.MaxRulesBytes, "the correlation rules document")
	if declined.state != "" {
		return declined.transformPlan()
	}
	rules, err := correlate.ParseRules(rulesData)
	if err != nil {
		return TransformPlanResult{State: Failed, Reason: err.Error()}
	}
	encodedRules, err := json.Marshal(rules, json.Deterministic(true))
	if err != nil {
		return TransformPlanResult{State: Failed, Reason: "the correlation rules could not be canonicalized"}
	}
	sum := sha256.Sum256(encodedRules)
	plan := transform.Plan{
		Schema: transform.PlanSchema,
		Case:   source.Identity,
		Rules:  hex.EncodeToString(sum[:]),
		Steps:  request.Steps,
	}
	if request.Profile != "" {
		pack, declined := pinnedPack(root, request.Profile)
		if declined.state != "" {
			return declined.transformPlan()
		}
		plan.Profile = pack.Identity
	}
	encoded, err := json.Marshal(plan, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return TransformPlanResult{State: Failed, Reason: "the transformation plan could not be canonicalized"}
	}
	encoded = append(encoded, '\n')
	decoded, err := transform.DecodePlan(encoded)
	if err != nil {
		return TransformPlanResult{State: Failed, Reason: err.Error()}
	}
	if err := writeWorkspaceEntry(root, request.Output, encoded); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return TransformPlanResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
		}
		return TransformPlanResult{State: Failed, Reason: err.Error()}
	}
	authored := &AuthoredTransformPlan{
		Output: request.Output, Rules: request.Rules, Digest: plan.Rules,
		Plan: decoded, Boundary: transformPlanBoundary,
	}
	if len(decoded.Steps) == 0 {
		return TransformPlanResult{State: Empty, Reason: "this plan declares no step, so a preview would leave the sequence as the case's own", Plan: authored}
	}
	return TransformPlanResult{State: Completed, Plan: authored}
}

// OpenTransformPlan reads one transformation plan of the open workspace through
// the shared decoder. It writes nothing.
func (a *App) OpenTransformPlan(workspace, entry string) TransformPlanResult {
	return run(a, false, false, func(context.Context) TransformPlanResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return declined.transformPlan()
		}
		data, declined := workspaceDocument(root, entry, transform.MaxPlanBytes, "the transformation plan")
		if declined.state != "" {
			return declined.transformPlan()
		}
		decoded, err := transform.DecodePlan(data)
		if err != nil {
			return TransformPlanResult{State: Failed, Reason: err.Error()}
		}
		authored := &AuthoredTransformPlan{
			Output: entry, Digest: decoded.Rules, Plan: decoded, Boundary: transformPlanBoundary,
		}
		if len(decoded.Steps) == 0 {
			return TransformPlanResult{State: Empty, Reason: "this plan declares no step", Plan: authored}
		}
		return TransformPlanResult{State: Completed, Plan: authored}
	})
}
