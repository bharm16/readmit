package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/testisolation"
)

var ErrFlowInput = errors.New("retained lifecycle runtime inputs differ from the approved input snapshot")

type flowInputSnapshot struct {
	Plan          string                           `json:"plan"`
	Configuration string                           `json:"configuration"`
	Bindings      map[string]networkaction.Binding `json:"bindings"`
	Sources       map[string]string                `json:"sources"`
	Registry      string                           `json:"isolation_registry"`
	Policy        string                           `json:"isolation_policy"`
	Validation    string                           `json:"validation"`
}

// VerifyFlowInputSnapshot checks an immutable preparation's retained input
// contract without opening any referenced path. Execution separately proves
// that its actual evidence instantiates these pins.
func VerifyFlowInputSnapshot(plan *connectedtest.FlowPlan, snapshot, configuration, registry []byte) error {
	if plan == nil || len(snapshot) > 1<<20 || len(configuration) > 2<<20 || len(registry) > 1<<20 {
		return ErrFlowInput
	}
	var input flowInputSnapshot
	if plan.RuntimeScoped() {
		var runtime runtimeInput
		var config FlowConfig
		metadata, err := plan.BindRuntime("input-metadata")
		if err != nil || json.Unmarshal(snapshot, &runtime, json.RejectUnknownMembers(true)) != nil || runtime.Schema != runtimeInputSchema || runtime.Template != plan.Identity() || runtime.Configuration != networkaction.Digest(configuration) || len(runtime.Originals) != len(plan.Document().Test.Phases) ||
			json.Unmarshal(configuration, &config, json.RejectUnknownMembers(true)) != nil || config.Schema != RuntimeFlowConfigSchema || config.RecoveryStore != "" {
			return ErrFlowInput
		}
		for _, phase := range plan.Document().Test.Phases {
			if !networkaction.ValidDigest(runtime.Originals[phase.ID]) {
				return ErrFlowInput
			}
		}
		config.Schema = FlowConfigSchema
		input = runtime.Metadata
		if input.Plan != metadata.Identity() || input.Configuration != networkaction.Digest(canonicalFlow(config)) {
			return ErrFlowInput
		}
	} else {
		if runnerprotocol.Exact(snapshot, "plan", "configuration", "bindings", "sources", "isolation_registry", "isolation_policy", "validation") != nil || json.Unmarshal(snapshot, &input, json.RejectUnknownMembers(true)) != nil || input.Plan != plan.Identity() || input.Configuration != networkaction.Digest(configuration) {
			return ErrFlowInput
		}
	}
	if !networkaction.ValidDigest(input.Registry) || !networkaction.ValidDigest(input.Policy) || input.Validation != "" && !networkaction.ValidDigest(input.Validation) || len(input.Bindings) == 0 || input.Sources == nil || input.Registry != networkaction.Digest(registry) {
		return ErrFlowInput
	}
	return nil
}

// VerifyFlowInput verifies actual retained lifecycle facts against the exact
// approved inputs. Registry bytes are separately retained approved input, whose
// digest is checked and whose decoded contents must match the genuine child
// isolation plan. It proves that semantic registry agreement, not that the
// child consumed a particular whitespace encoding. Original paths, providers,
// worker engines and network destinations are never opened.
func VerifyFlowInput(ctx context.Context, path string, snapshot, configRaw []byte, expectedInstance string, registryRaw ...[]byte) error {
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(snapshot, &header) == nil && header.Schema == runtimeInputSchema {
		return verifyRuntimeFlowInput(ctx, path, snapshot, configRaw, expectedInstance, registryRaw...)
	}
	files, err := artifactdir.Read(path, flowResultFamily.Layout)
	if err != nil {
		return ErrFlowInput
	}
	return verifyFlowInputFiles(ctx, path, files, snapshot, configRaw, expectedInstance, registryRaw...)
}

func verifyFlowInputFiles(ctx context.Context, path string, files map[string][]byte, snapshot, configRaw []byte, expectedInstance string, registryRaw ...[]byte) error {
	var input flowInputSnapshot
	if len(snapshot) > 1<<20 || len(configRaw) > 2<<20 || len(registryRaw) != 1 || len(registryRaw[0]) > 1<<20 ||
		runnerprotocol.Exact(snapshot, "plan", "configuration", "bindings", "sources", "isolation_registry", "isolation_policy", "validation") != nil ||
		json.Unmarshal(snapshot, &input, json.RejectUnknownMembers(true)) != nil || input.Configuration != networkaction.Digest(configRaw) || input.Registry != networkaction.Digest(registryRaw[0]) || len(input.Bindings) == 0 || input.Sources == nil {
		return ErrFlowInput
	}
	actual, err := openFlowFiles(ctx, path, files)
	if err != nil {
		actual, err = inspectFlowFiles(ctx, path, files)
	}
	if err != nil || actual.Instance != expectedInstance || actual.Plan != input.Plan || actual.Previous != "" {
		return ErrFlowInput
	}
	plan, err := connectedtest.VerifyFlowPlan(artifactdir.Subtree(files, "plan"))
	if err != nil {
		return ErrFlowInput
	}
	retainedPlan := files["isolation/plan.json"]
	if retainedPlan == nil {
		retainedPlan = files["preflight/plan.json"]
	}
	var selected struct {
		Seed uint64 `json:"seed"`
	}
	var isolation struct {
		Contract testisolation.Contract `json:"contract"`
		Options  testisolation.Options  `json:"options"`
	}
	if json.Unmarshal(configRaw, &selected) != nil || json.Unmarshal(retainedPlan, &isolation) != nil || isolation.Options.ParentPlan != input.Plan || isolation.Options.Instance != expectedInstance || isolation.Options.Seed != selected.Seed || !bytes.Equal(canonicalFlow(isolation.Contract), canonicalFlowContract(plan.Dependency(plan.Document().Test.Isolation))) {
		return ErrFlowInput
	}
	bindings, policy, err := testisolation.RetainedInputBindings(retainedPlan, registryRaw[0], "input-metadata")
	if err != nil || networkaction.Digest(policy) != input.Policy {
		return ErrFlowInput
	}
	for key, binding := range bindings {
		if scopeStore(binding, actual.RecoveryStore) != input.Bindings[key] {
			return ErrFlowInput
		}
	}
	for name, raw := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		parts := strings.Split(name, "/")
		if len(parts) < 3 || parts[0] != "phases" {
			continue
		}
		phase := parts[1]
		var header struct {
			Schema string `json:"schema"`
		}
		if json.Unmarshal(raw, &header) != nil {
			continue
		}
		switch header.Schema {
		case dataset.Schema:
			if strings.HasSuffix(name, "/manifest.json") {
				var doc dataset.Document
				if json.Unmarshal(raw, &doc, json.RejectUnknownMembers(true)) != nil || !approvedSource(input, phase, networkaction.Digest(doc.Acquisition.SourceConfiguration)) {
					return ErrFlowInput
				}
			}
		case fhirrest.PlanSchema:
			if err := verifyFHIRInput(plan, phase, raw, files[strings.TrimSuffix(name, "plan.json")+"policy.json"], input, actual.RecoveryStore); err != nil {
				return err
			}
		case fhirvalidator.ResultSchema:
			var result fhirvalidator.Result
			if json.Unmarshal(raw, &result, json.RejectUnknownMembers(true)) != nil || result.Capability != input.Validation {
				return ErrFlowInput
			}
		}
		if strings.HasSuffix(name, "/receipt.json") || strings.HasSuffix(name, "/result.json") {
			var record struct {
				Binding networkaction.Binding `json:"binding"`
			}
			if json.Unmarshal(raw, &record) == nil && record.Binding != (networkaction.Binding{}) {
				// Nested runtime HTTP receipts inside a FHIR result are already
				// tied to its retained request by the existing FHIR reader; the
				// template/selected-source check above owns their approved pin.
				if strings.Contains(name, "/attempts/") || strings.Contains(name, "/pages/") {
					continue
				}
				if !approvedBinding(input, phase, scopeStore(record.Binding, actual.RecoveryStore)) {
					return ErrFlowInput
				}
			}
		}
	}
	return nil
}

func approvedBinding(input flowInputSnapshot, phase string, actual networkaction.Binding) bool {
	for key, expected := range input.Bindings {
		if (strings.HasPrefix(key, phase+":") || strings.HasPrefix(key, "token:") && actual.Operation == "smart-token") && actual == expected {
			return true
		}
	}
	return false
}

func canonicalFlowContract(raw []byte) []byte {
	var contract testisolation.Contract
	if json.Unmarshal(raw, &contract, json.RejectUnknownMembers(true)) != nil {
		return nil
	}
	return canonicalFlow(contract)
}

func approvedSource(input flowInputSnapshot, phase, actual string) bool {
	for key, expected := range input.Sources {
		if strings.HasPrefix(key, phase+":") && actual == expected {
			return true
		}
	}
	return false
}

func verifyFHIRInput(flow *connectedtest.FlowPlan, phase string, raw, policy []byte, input flowInputSnapshot, store *RecoveryStore) error {
	p, err := fhirrest.Prepare(raw, policy)
	if err != nil {
		return ErrFlowInput
	}
	if approvedBinding(input, phase, scopeStore(p.Binding(), store)) {
		return nil
	}
	spec := p.Declaration()
	child := flow.Phase(phase)
	for _, step := range child.Document().Test.Steps {
		if step.Interaction == nil || spec.HTTP.HTTP.Source != networkaction.Digest([]byte("step-"+step.ID)) {
			continue
		}
		server := &fhirServer{base: spec.Base, serverName: spec.HTTP.HTTP.ServerName, authorities: bytes.Clone(spec.HTTP.HTTP.Authorities),
			observation: fhirAuth{identity: spec.HTTP.Authorization}, action: fhirAuth{identity: spec.HTTP.Authorization}}
		derived := scopeStore(stepTemplate(flow.Identity(), child, step, server, policy), store)
		if derived == input.Bindings[phase+":step:"+step.ID] {
			return nil
		}
	}
	return ErrFlowInput
}
