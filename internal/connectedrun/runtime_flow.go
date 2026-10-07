package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/testisolation"
)

const RuntimeFlowConfigSchema = "readmit-connected-run-config/v5"
const RuntimeFlowSchema = "readmit-connected-run/v5"
const runtimeInputSchema = "readmit-connected-runtime-input/v1"
const runtimeDerivationSchema = "readmit-connected-runtime-derivation/v1"

// A runtime flow retains the immutable template separately from the ordinary
// v4 executable. Its instance is supplied by the existing owner, never issued
// by preparation, inferred from a literal, or borrowed from a previous run.
type runtimeFlow struct {
	template    *connectedtest.FlowPlan
	raw         []byte
	derivations map[string]*reproducer.Prepared
}

type runtimeInput struct {
	Schema        string            `json:"schema"`
	Template      string            `json:"template"`
	Configuration string            `json:"configuration"`
	Originals     map[string]string `json:"originals"`
	Metadata      flowInputSnapshot `json:"metadata"`
}

type runtimeDerivation struct {
	Schema   string            `json:"schema"`
	Template string            `json:"template"`
	Instance string            `json:"instance"`
	Plan     string            `json:"plan"`
	Cases    map[string]string `json:"cases"`
}

var runtimeFlowFamily = artifactdir.Family{
	Layout: artifactdir.Layout{Noun: "runtime connected lifecycle", RequiredFiles: []string{"started.json", "configuration.json", "input.json", "registry.json", "derivation.json", "execution-input.json", "execution-configuration.json", "actions.json"},
		Nested: []string{"template", "originals", "derivations", "execution"}, MaxFiles: 400000, MaxFileBytes: 64 << 20, MaxBytes: 2 << 30,
		AllowFile: func(name string) bool {
			return slices.Contains([]string{"started.json", "manifest.json", "identity.sha256", "configuration.json", "input.json", "registry.json", "derivation.json", "execution-input.json", "execution-configuration.json", "actions.json"}, name)
		}},
	Seal: artifactdir.DirectoryHash(RuntimeFlowSchema),
}

func prepareRuntimeFlow(template *connectedtest.FlowPlan, planPath, configPath, instance string, raw []byte) (*PreparedFlow, error) {
	return prepareRuntimeFlowBound(template, planPath, configPath, instance, raw, nil)
}

func prepareRuntimeFlowBound(template *connectedtest.FlowPlan, planPath, configPath, instance string, raw []byte, reuse *PreparedFlow) (*PreparedFlow, error) {
	var config FlowConfig
	if json.Unmarshal(raw, &config, json.RejectUnknownMembers(true)) != nil || config.Schema != RuntimeFlowConfigSchema || config.RecoveryStore != "" {
		return nil, invalid
	}
	var concrete *connectedtest.FlowPlan
	var err error
	if reuse != nil {
		concrete = reuse.plan
	} else {
		concrete, err = template.BindRuntime(instance)
		if err != nil {
			return nil, err
		}
	}
	root, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return nil, err
	}
	runtime := &runtimeFlow{template: template, raw: bytes.Clone(raw), derivations: map[string]*reproducer.Prepared{}}
	transports := map[string]*connectedtransport.Prepared{}
	for _, phase := range template.Document().Test.Phases {
		if len(phase.IsolationChanges) != 0 {
			return nil, invalid
		}
		selection, ok := config.Phases[phase.ID]
		if !ok {
			return nil, invalid
		}
		anchor := func(path string) string {
			if path == "" {
				return ""
			}
			return artifactpath.JoinReference(root, path)
		}
		originalPath := anchor(selection.Definition.Case)
		var derived *reproducer.Prepared
		if reuse != nil {
			derived = reuse.runtime.derivations[phase.ID]
			if derived == nil || derived.VerifyUnchanged() != nil {
				return nil, invalid
			}
		} else {
			original, err := bundle.Open(originalPath)
			if err != nil {
				return nil, err
			}
			plan, err := runtimeCasePlan(template, concrete, phase, original)
			if err != nil {
				return nil, err
			}
			derived, err = reproducer.Prepare(originalPath, plan)
			if err != nil {
				return nil, err
			}
		}
		for _, occurrence := range derived.Manifest().Occurrences {
			if occurrence.Parent != occurrence.Derived {
				return nil, invalid
			}
		}
		runtime.derivations[phase.ID] = derived
		transports[phase.ID], err = connectedtransport.PrepareDerivedSequence(concrete.Phase(phase.ID), connectedtransport.Selection{
			Case: originalPath, Target: anchor(selection.Definition.Target), Policy: anchor(selection.Definition.Policy), Credential: anchor(selection.Definition.Credential),
		}, derived)
		if err != nil {
			return nil, err
		}
	}
	config.Schema = FlowConfigSchema
	concreteRaw := canonicalFlow(config)
	p, err := prepareFlowSelection(concrete, planPath, configPath, instance, concreteRaw, transports)
	if err != nil {
		return nil, err
	}
	p.runtime = runtime
	if _, err := runtimeActions(p); err != nil {
		return nil, err
	}
	return p, nil
}

func runtimeActions(p *PreparedFlow) (map[string][]byte, error) {
	actions := map[string][]byte{}
	add := func(key string, source sourcePlan) error {
		if source.database != nil {
			return invalid
		}
		if source.capture != nil {
			actions[key] = source.capture.DeclarationBytes()
		}
		if source.http != nil {
			actions[key] = canonicalFlow(source.http.Declaration())
		}
		return nil
	}
	for phase, prepared := range p.phases {
		for _, source := range prepared.sources {
			if err := add(phase+":dataset:"+source.definition.ID, source); err != nil {
				return nil, err
			}
			if source.barrier != nil {
				if err := add(phase+":barrier:"+source.definition.ID, *source.barrier); err != nil {
					return nil, err
				}
			}
		}
	}
	return actions, nil
}

// Derivation admits only the assignments already compiled from the approved
// template. Every selected original occurrence must match its authored input.
func runtimeCasePlan(template, concrete *connectedtest.FlowPlan, phase connectedtest.FlowPhase, original *bundle.Bundle) (reproducer.Plan, error) {
	plan, err := reproducer.NewPlan(original.Identity)
	if err != nil {
		return plan, err
	}
	for _, occurrence := range original.Events {
		plan, err = reproducer.Append(plan, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: occurrence.ID})
		if err != nil {
			return plan, err
		}
	}
	child := template.Phase(phase.ID)
	resolved := concrete.Phase(phase.ID).Document().Resolution
	edits := map[string]string{}
	for _, step := range child.Document().Test.Steps {
		if step.V2 == nil {
			return plan, invalid
		}
		raw, err := original.Raw(step.V2.Occurrence)
		if err != nil || !bytes.Equal(raw, child.Files()["dependencies/"+step.V2.Input.SHA256]) {
			return plan, invalid
		}
		for _, assignment := range step.V2.Assignments {
			value, ok := resolved[assignment.Variable]
			if !ok {
				return plan, invalid
			}
			key := step.V2.Occurrence + ":" + assignment.Selector
			if prior, exists := edits[key]; exists {
				if prior != value {
					return plan, invalid
				}
				continue
			}
			edits[key] = value
			plan, err = reproducer.Append(plan, reproducer.Step{Operator: reproducer.SetField, Occurrence: step.V2.Occurrence, Selector: assignment.Selector, Value: value})
			if err != nil {
				return plan, err
			}
		}
	}
	return plan, nil
}

func (r *runtimeFlow) unchanged(p *PreparedFlow) error {
	raw, err := (artifactdir.Document{MaxBytes: 2 << 20}).Read(p.configPath)
	if err != nil || !bytes.Equal(raw, r.raw) || r.template.VerifyUnchanged(p.planPath) != nil {
		return invalid
	}
	for _, derived := range r.derivations {
		if derived.VerifyUnchanged() != nil {
			return invalid
		}
	}
	// The template's seal and every original have just been rechecked. Reuse
	// their pure compilation while rebuilding live configuration from fresh
	// reads; no approval, policy or credential generation is cached here.
	fresh, err := prepareRuntimeFlowBound(r.template, p.planPath, p.configPath, p.instance, raw, p)
	if err != nil || fresh.plan.Identity() != p.plan.Identity() || fresh.isolation.Identity() != p.isolation.Identity() || !bytes.Equal(canonicalFlow(fresh.Bindings()), canonicalFlow(p.Bindings())) || !bytes.Equal(canonicalFlow(sourceIdentities(fresh)), canonicalFlow(sourceIdentities(p))) {
		return invalid
	}
	return nil
}

// concreteRuntimeSnapshot describes the genuine executable and uses only a
// metadata isolation allocation. Transport and capture bindings are concrete.
func concreteRuntimeSnapshot(p *PreparedFlow) (flowInputSnapshot, error) {
	var config FlowConfig
	if p.runtime == nil || json.Unmarshal(p.raw, &config) != nil {
		return flowInputSnapshot{}, invalid
	}
	isolation, err := prepareFlowIsolation(p.plan, p.selection, "input-metadata", config.Seed)
	if err != nil {
		return flowInputSnapshot{}, err
	}
	bindings := p.Bindings()
	for _, role := range []string{"read", "setup", "cleanup"} {
		bindings["isolation:"+role] = isolation.Review(role).Binding
	}
	registry, err := (artifactdir.Document{MaxBytes: 1 << 20}).Read(p.selection.Registry)
	if err != nil {
		return flowInputSnapshot{}, err
	}
	policy, err := (artifactdir.Document{MaxBytes: 1 << 20}).Read(p.selection.Policy)
	if err != nil {
		return flowInputSnapshot{}, err
	}
	return flowInputSnapshot{Plan: p.plan.Identity(), Configuration: networkaction.Digest(p.raw), Bindings: bindings, Sources: sourceIdentities(p), Registry: networkaction.Digest(registry), Policy: networkaction.Digest(policy), Validation: validationIdentity(p)}, nil
}

func runtimeInputSnapshot(p *PreparedFlow) ([]byte, error) {
	metadata, err := prepareRuntimeFlow(p.runtime.template, p.planPath, p.configPath, "input-metadata", p.runtime.raw)
	if err != nil {
		return nil, err
	}
	snapshot, err := concreteRuntimeSnapshot(metadata)
	if err != nil {
		return nil, err
	}
	originals := map[string]string{}
	for phase, derived := range p.runtime.derivations {
		originals[phase] = derived.SourceIdentity()
	}
	return canonicalFlow(runtimeInput{Schema: runtimeInputSchema, Template: p.runtime.template.Identity(), Configuration: networkaction.Digest(p.runtime.raw), Originals: originals, Metadata: snapshot}), nil
}

func executeRuntimeFlow(ctx context.Context, p *PreparedFlow, output string, confirmation testisolation.Confirmation, observers ...func(FlowPhaseResult)) (FlowResult, error) {
	if len(observers) > 1 || p.unchanged() != nil {
		return FlowResult{}, invalid
	}
	input, err := runtimeInputSnapshot(p)
	if err != nil {
		return FlowResult{}, err
	}
	concreteInput, err := concreteRuntimeSnapshot(p)
	if err != nil {
		return FlowResult{}, err
	}
	registry, err := p.RegistrySnapshot()
	if err != nil {
		return FlowResult{}, err
	}
	actions, err := runtimeActions(p)
	if err != nil {
		return FlowResult{}, err
	}
	w, err := artifactdir.Create(output, runtimeFlowFamily, artifactdir.Durable)
	if err != nil {
		return FlowResult{}, err
	}
	defer w.Close()
	start := initialFlow(p.plan, p.instance, time.Now().UTC())
	start.Schema, start.Plan = RuntimeFlowSchema, p.runtime.template.Identity()
	receipt := runtimeDerivation{Schema: runtimeDerivationSchema, Template: p.runtime.template.Identity(), Instance: p.instance, Plan: p.plan.Identity(), Cases: map[string]string{}}
	if err = p.runtime.template.Write(ctx, filepath.Join(w.Path(), "template")); err != nil {
		return FlowResult{}, err
	}
	for phase, derived := range p.runtime.derivations {
		receipt.Cases[phase] = derived.Identity()
		if err = writeRuntimeFiles(w, "originals/"+phase, derived.OriginalFiles()); err != nil {
			return FlowResult{}, err
		}
		if err = writeRuntimeFiles(w, "derivations/"+phase, derived.Files()); err != nil {
			return FlowResult{}, err
		}
	}
	for name, raw := range map[string][]byte{"started.json": canonicalFlow(start), "configuration.json": p.runtime.raw, "input.json": input, "registry.json": registry, "derivation.json": canonicalFlow(receipt), "execution-input.json": canonicalFlow(concreteInput), "execution-configuration.json": p.raw, "actions.json": canonicalFlow(actions)} {
		if err = w.WriteFile(name, raw); err != nil {
			return FlowResult{}, err
		}
	}
	if err = w.Sync(); err != nil {
		return FlowResult{}, err
	}
	result, runErr := executeConcreteFlow(ctx, p, filepath.Join(w.Path(), "execution"), confirmation, observers...)
	if result.Schema == "" {
		return FlowResult{}, runErr
	}
	result.Schema, result.Plan, result.StartedAt = RuntimeFlowSchema, p.runtime.template.Identity(), start.StartedAt
	if err = put(w, "manifest.json", result); err != nil {
		return FlowResult{}, err
	}
	if _, err = w.Seal(nil); err != nil {
		return FlowResult{}, err
	}
	opened, err := OpenFlow(context.WithoutCancel(ctx), output)
	if err != nil {
		return FlowResult{}, err
	}
	return opened, runErr
}

func writeRuntimeFiles(w *artifactdir.Writer, prefix string, files map[string][]byte) error {
	for name, raw := range files {
		path := prefix + "/" + name
		if err := w.Mkdir(filepath.ToSlash(filepath.Dir(path))); err != nil {
			return err
		}
		if err := w.WriteFile(path, raw); err != nil {
			return err
		}
	}
	return nil
}

func runtimeHeader(raw []byte) bool {
	var header struct {
		Schema string `json:"schema"`
	}
	return json.Unmarshal(raw, &header) == nil && header.Schema == RuntimeFlowSchema
}

// ExecutionRoot returns the existing lifecycle's evidence location only after
// verifying its enclosing runtime derivation. Old lifecycles remain in place.
func ExecutionRoot(path string) (string, error) {
	result, err := OpenFlow(context.Background(), path)
	if err != nil {
		return "", err
	}
	if result.Schema == RuntimeFlowSchema {
		return filepath.Join(path, "execution"), nil
	}
	return path, nil
}

func runtimeLayout(files map[string][]byte) artifactdir.Layout {
	if runtimeHeader(files["started.json"]) {
		return runtimeFlowFamily.Layout
	}
	return flowResultFamily.Layout
}

func readFlowFiles(path string) (map[string][]byte, error) {
	raw, err := (artifactdir.Document{MaxBytes: 2 << 20}).Read(filepath.Join(path, "started.json"))
	if err != nil {
		return nil, err
	}
	if runtimeHeader(raw) {
		return artifactdir.Read(path, runtimeFlowFamily.Layout)
	}
	return artifactdir.Read(path, flowResultFamily.Layout)
}

func runtimeResult(inner FlowResult, template string, started time.Time) FlowResult {
	inner.Schema, inner.Plan, inner.StartedAt = RuntimeFlowSchema, template, started
	return inner
}

func runtimeSealValid(files map[string][]byte) bool {
	return strings.TrimSpace(string(files["identity.sha256"])) == artifactdir.Identity(RuntimeFlowSchema, files)
}
