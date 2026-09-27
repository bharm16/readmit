package connectedrun

import (
	"context"
	"encoding/json/v2"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/networkaction"
)

func expectedFlowStart(ctx context.Context, path string, files map[string][]byte, plan *connectedtest.FlowPlan, r FlowResult) (FlowResult, error) {
	expected := initialFlow(plan, r.Instance, r.StartedAt)
	expected.Engine = r.Engine
	expected.RecoveryStore = r.RecoveryStore
	if r.RecoveryStore != nil && (!filepath.IsAbs(r.RecoveryStore.Path) || !networkaction.ValidDigest(r.RecoveryStore.Identity)) {
		return FlowResult{}, invalid
	}
	previous := artifactdir.Subtree(files, "previous")
	if r.Previous == "" {
		if r.Inherited != 0 || len(previous) != 0 || len(artifactdir.Subtree(files, "continuation")) != 0 {
			return FlowResult{}, invalid
		}
		return expected, nil
	}
	if r.Inherited < 0 || r.Inherited >= len(expected.Phases) || len(previous) == 0 || len(artifactdir.Subtree(files, "isolation")) != 0 || len(artifactdir.Subtree(files, "preflight")) != 0 || len(artifactdir.Subtree(files, "transitions")) != 0 {
		return FlowResult{}, invalid
	}
	// Only one supported continuation link is inspected. Reject a nested link
	// before recursion, rather than relying on filesystem depth to bound it.
	var head struct {
		Previous string `json:"previous"`
	}
	if json.Unmarshal(previous["manifest.json"], &head) != nil || head.Previous != "" {
		return FlowResult{}, invalid
	}
	prior, err := openFlowFiles(ctx, filepath.Join(path, "previous"), previous)
	if err != nil || prior.RecoveryStore == nil || r.RecoveryStore == nil || *prior.RecoveryStore != *r.RecoveryStore || prior.Plan != r.Plan || prior.Instance != r.Instance || prior.CompletedAt.After(r.StartedAt) || prior.Setup != "ready" || prior.Cleanup != "not-started" || prior.State == "complete" || prior.State == "uncertain" || artifactdir.Identity(FlowSchema, previous) != r.Previous {
		return FlowResult{}, invalid
	}
	for i, phase := range prior.Phases {
		if i < r.Inherited {
			if phase.State != "complete" {
				return FlowResult{}, invalid
			}
		} else if phase.State != "not-attempted" {
			return FlowResult{}, invalid
		}
	}
	expected.Previous = r.Previous
	expected.Inherited = r.Inherited
	copy(expected.Phases[:r.Inherited], prior.Phases[:r.Inherited])
	return expected, nil
}
