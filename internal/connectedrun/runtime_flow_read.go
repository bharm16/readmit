package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/testisolation"
)

func openRuntimeFlowFiles(ctx context.Context, path string, files map[string][]byte, inspect bool) (FlowResult, error) {
	var start FlowResult
	if json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || start.Schema != RuntimeFlowSchema || !safeID(start.Instance) || start.StartedAt.IsZero() {
		return FlowResult{}, invalid
	}
	_, sealed := files["identity.sha256"]
	if !inspect && !sealed || sealed && !runtimeSealValid(files) {
		return FlowResult{}, invalid
	}
	template, concrete, metadata, expectedCases, err := verifyRuntimeDerivation(files, start)
	if err != nil {
		return FlowResult{}, err
	}
	expectedStart := initialFlow(concrete, start.Instance, start.StartedAt)
	expectedStart.Schema, expectedStart.Plan = RuntimeFlowSchema, template.Identity()
	expectedStart.Engine = start.Engine
	if !bytes.Equal(canonicalFlow(start), canonicalFlow(expectedStart)) {
		return FlowResult{}, invalid
	}
	childFiles := artifactdir.Subtree(files, "execution")
	if len(childFiles) == 0 && inspect && !sealed {
		return start, nil
	}
	childPlan, err := connectedtest.VerifyFlowPlan(artifactdir.Subtree(childFiles, "plan"))
	if err != nil || childPlan.Identity() != concrete.Identity() {
		return FlowResult{}, invalid
	}
	inner, err := openFlowFiles(ctx, filepath.Join(path, "execution"), childFiles)
	if err != nil && inspect && !sealed {
		inner, err = inspectFlowFiles(ctx, filepath.Join(path, "execution"), childFiles)
	}
	if err != nil || inner.Instance != start.Instance || inner.Plan != concrete.Identity() || inner.Engine != start.Engine || inner.StartedAt.Before(start.StartedAt) || inner.Previous != "" || inner.RecoveryStore != nil {
		return FlowResult{}, invalid
	}
	if err := verifyRuntimeApproval(ctx, filepath.Join(path, "execution"), files, childFiles, concrete, metadata, expectedCases, start.Instance); err != nil {
		return FlowResult{}, err
	}
	result := runtimeResult(inner, template.Identity(), start.StartedAt)
	if sealed {
		var retained FlowResult
		if json.Unmarshal(files["manifest.json"], &retained, json.RejectUnknownMembers(true)) != nil || !bytes.Equal(canonicalFlow(retained), canonicalFlow(result)) {
			return FlowResult{}, invalid
		}
	} else {
		if result.State == "complete" {
			result.State = "incomplete"
		}
		if result.Verdict == assertion.VerdictPass {
			result.Verdict = assertion.VerdictUndecided
		}
	}
	return result, nil
}

// The same pure compiler and reproducer recompute every permitted byte change.
// The retained executable is never trusted merely because its receipt names a
// template, and the reader never consults a path from retained configuration.
func verifyRuntimeDerivation(files map[string][]byte, start FlowResult) (*connectedtest.FlowPlan, *connectedtest.FlowPlan, *connectedtest.FlowPlan, map[string]string, error) {
	fail := func() (*connectedtest.FlowPlan, *connectedtest.FlowPlan, *connectedtest.FlowPlan, map[string]string, error) {
		return nil, nil, nil, nil, invalid
	}
	template, err := connectedtest.VerifyFlowPlan(artifactdir.Subtree(files, "template"))
	if err != nil || !template.RuntimeScoped() || template.Identity() != start.Plan {
		return fail()
	}
	concrete, err := template.BindRuntime(start.Instance)
	if err != nil {
		return fail()
	}
	metadata, err := template.BindRuntime("input-metadata")
	if err != nil {
		return fail()
	}
	var receipt runtimeDerivation
	if json.Unmarshal(files["derivation.json"], &receipt, json.RejectUnknownMembers(true)) != nil || receipt.Schema != runtimeDerivationSchema || receipt.Template != template.Identity() || receipt.Instance != start.Instance || receipt.Plan != concrete.Identity() || len(receipt.Cases) != len(template.Document().Test.Phases) {
		return fail()
	}
	var input runtimeInput
	if json.Unmarshal(files["input.json"], &input, json.RejectUnknownMembers(true)) != nil || input.Schema != runtimeInputSchema || input.Template != template.Identity() || input.Configuration != networkaction.Digest(files["configuration.json"]) || len(input.Originals) != len(receipt.Cases) {
		return fail()
	}
	var config FlowConfig
	if json.Unmarshal(files["configuration.json"], &config, json.RejectUnknownMembers(true)) != nil || config.Schema != RuntimeFlowConfigSchema || config.RecoveryStore != "" || len(config.Phases) != len(receipt.Cases) {
		return fail()
	}
	config.Schema = FlowConfigSchema
	if !bytes.Equal(canonicalFlow(config), files["execution-configuration.json"]) {
		return fail()
	}
	expectedCases := map[string]string{}
	known := map[string]bool{}
	for _, phase := range template.Document().Test.Phases {
		known[phase.ID] = true
		originalFiles := artifactdir.Subtree(files, "originals/"+phase.ID)
		original, err := bundle.Verify(originalFiles)
		if err != nil || original.Identity != input.Originals[phase.ID] {
			return fail()
		}
		plan, err := runtimeCasePlan(template, concrete, phase, original)
		if err != nil {
			return fail()
		}
		derived, err := reproducer.PrepareFiles(originalFiles, plan)
		if err != nil || derived.Identity() != receipt.Cases[phase.ID] || !bytes.Equal(canonicalFlow(derived.Files()), canonicalFlow(artifactdir.Subtree(files, "derivations/"+phase.ID))) {
			return fail()
		}
		metadataPlan, err := runtimeCasePlan(template, metadata, phase, original)
		if err != nil {
			return fail()
		}
		metadataCase, err := reproducer.PrepareFiles(originalFiles, metadataPlan)
		if err != nil {
			return fail()
		}
		expectedCases[phase.ID] = metadataCase.Identity()
	}
	for name := range files {
		for _, prefix := range []string{"originals/", "derivations/"} {
			if strings.HasPrefix(name, prefix) {
				parts := strings.SplitN(strings.TrimPrefix(name, prefix), "/", 2)
				if len(parts) != 2 || !known[parts[0]] {
					return fail()
				}
			}
		}
	}
	return template, concrete, metadata, expectedCases, nil
}

func verifyRuntimeApproval(ctx context.Context, path string, files, childFiles map[string][]byte, concrete, metadata *connectedtest.FlowPlan, metadataCases map[string]string, instance string) error {
	if verifyFlowInputFiles(ctx, path, childFiles, files["execution-input.json"], files["execution-configuration.json"], instance, files["registry.json"]) != nil {
		return ErrFlowInput
	}
	var approved runtimeInput
	var actual flowInputSnapshot
	if json.Unmarshal(files["input.json"], &approved, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["execution-input.json"], &actual, json.RejectUnknownMembers(true)) != nil || actual.Plan != concrete.Identity() {
		return ErrFlowInput
	}
	actual.Plan = metadata.Identity()
	var receipt runtimeDerivation
	if json.Unmarshal(files["derivation.json"], &receipt, json.RejectUnknownMembers(true)) != nil {
		return ErrFlowInput
	}
	var actions map[string][]byte
	if json.Unmarshal(files["actions.json"], &actions, json.RejectUnknownMembers(true)) != nil || actions == nil {
		return ErrFlowInput
	}
	actionCount := 0
	for key, binding := range actual.Bindings {
		if strings.HasPrefix(key, "isolation:") {
			continue
		}
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || concrete.Phase(parts[0]) == nil || binding.Plan != concrete.Phase(parts[0]).Identity() {
			return ErrFlowInput
		}
		binding.Plan = metadata.Phase(parts[0]).Identity()
		if parts[1] == "stimulus" {
			if binding.Source != receipt.Cases[parts[0]] {
				return ErrFlowInput
			}
			binding.Source = metadataCases[parts[0]]
		} else {
			raw, ok := actions[key]
			if !ok || networkaction.Digest(raw) != binding.Configuration {
				return ErrFlowInput
			}
			normalized, err := runtimeActionMetadata(raw, concrete.Phase(parts[0]).Identity(), metadata.Phase(parts[0]).Identity())
			if err != nil {
				return ErrFlowInput
			}
			binding.Configuration = networkaction.Digest(normalized)
			actionCount++
		}
		actual.Bindings[key] = binding
	}
	if actionCount != len(actions) {
		return ErrFlowInput
	}
	isolation := childFiles["isolation/plan.json"]
	if isolation == nil {
		isolation = childFiles["preflight/plan.json"]
	}
	bindings, _, err := testisolation.RetainedRuntimeInputBindings(isolation, files["registry.json"], "input-metadata", metadata.Identity())
	if err != nil {
		return ErrFlowInput
	}
	for key, binding := range bindings {
		actual.Bindings[key] = binding
	}
	if !bytes.Equal(canonicalFlow(actual), canonicalFlow(approved.Metadata)) {
		return ErrFlowInput
	}
	return nil
}

func runtimeActionMetadata(raw []byte, concrete, metadata string) ([]byte, error) {
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(raw, &header) != nil {
		return nil, invalid
	}
	switch header.Schema {
	case networkaction.HTTPSchema:
		var action networkaction.HTTPSpec
		if json.Unmarshal(raw, &action, json.RejectUnknownMembers(true)) != nil || action.Plan != concrete {
			return nil, invalid
		}
		action.Plan = metadata
		return canonicalFlow(action), nil
	case networkaction.CaptureActionSchemaV2:
		var action networkaction.CaptureSpecV2
		if json.Unmarshal(raw, &action, json.RejectUnknownMembers(true)) != nil || action.Definition.Plan != concrete {
			return nil, invalid
		}
		action.Definition.Plan = metadata
		return canonicalFlow(action), nil
	default:
		return nil, invalid
	}
}

func verifyRuntimeFlowInput(ctx context.Context, path string, snapshot, configuration []byte, instance string, registry ...[]byte) error {
	if len(registry) != 1 {
		return ErrFlowInput
	}
	files, err := artifactdir.Read(path, runtimeFlowFamily.Layout)
	if err != nil || !bytes.Equal(files["input.json"], snapshot) || !bytes.Equal(files["configuration.json"], configuration) || !bytes.Equal(files["registry.json"], registry[0]) {
		return ErrFlowInput
	}
	result, err := openRuntimeFlowFiles(ctx, path, files, true)
	if err != nil || result.Instance != instance {
		return ErrFlowInput
	}
	return nil
}
