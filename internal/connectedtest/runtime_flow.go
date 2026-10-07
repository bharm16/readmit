package connectedtest

import (
	"encoding/json/v2"
	"slices"

	"github.com/bharm16/readmit/internal/hl7"
)

const RuntimeFlowTestSchema = "readmit-connected-test/v7"
const RuntimeFlowPlanSchema = "readmit-execution-plan/v7"

// RuntimeScoped identifies a template whose reviewed input assignments bind
// to the execution instance. Historical literal variables never acquire this
// meaning, regardless of their name or value.
func (p *FlowPlan) RuntimeScoped() bool {
	return p != nil && p.document.Schema == RuntimeFlowPlanSchema
}

// BindRuntime deterministically derives an ordinary v4 lifecycle from one
// reviewed v7 template. It retains the original dependencies and changes only
// the explicitly declared runtime variable. It performs no I/O and grants no
// execution authority; the caller must retain both plans and their binding.
func (p *FlowPlan) BindRuntime(instance string) (*FlowPlan, error) {
	if !p.RuntimeScoped() || !identifier.MatchString(instance) {
		return nil, invalid
	}
	d := p.Document().Test
	if err := bindRuntimeVariable(&d, instance); err != nil {
		return nil, err
	}
	supplied := map[string][]byte{d.Isolation.File: p.Dependency(d.Isolation)}
	for _, phase := range d.Phases {
		child := p.Phase(phase.ID)
		for _, ref := range references(child.document.Test) {
			supplied[ref.File] = slices.Clone(child.files["dependencies/"+ref.SHA256])
		}
		if ref := child.document.Test.Environment.TargetRevision.Evidence; ref != nil {
			var receipt RevisionEvidence
			if json.Unmarshal(supplied[ref.File], &receipt, json.RejectUnknownMembers(true)) != nil {
				return nil, invalid
			}
			supplied[receipt.Source.File] = slices.Clone(child.files["dependencies/"+receipt.Source.SHA256])
		}
		if phase.Wire != nil {
			supplied[phase.Wire.Set.File] = p.Dependency(phase.Wire.Set)
		}
	}
	raw, err := encode(d)
	if err != nil {
		return nil, err
	}
	return CompileFlow(raw, supplied, p.document.Generation)
}

func compileRuntimeFlow(d FlowTest, supplied map[string][]byte, g Generation) (*FlowPlan, error) {
	// Child plans are inspection-only templates until the enclosing lifecycle
	// binds its actual instance. Their ordinary v1 contract remains unchanged.
	canonical, err := encode(d)
	if err != nil {
		return nil, err
	}
	var concrete FlowTest
	if json.Unmarshal(canonical, &concrete) != nil || bindRuntimeVariable(&concrete, "runtime-unbound") != nil {
		return nil, invalid
	}
	raw, _ := encode(concrete)
	p, err := CompileFlow(raw, supplied, g)
	if err != nil {
		return nil, err
	}
	p.document.Schema, p.document.Test = RuntimeFlowPlanSchema, d
	p.files["test.json"] = canonical
	for i := range p.document.Members {
		member := &p.document.Members[i]
		if member.Path == "test.json" {
			member.SHA256, member.Size = Digest(canonical), len(canonical)
		}
	}
	raw, err = encode(p.document)
	if err != nil || len(raw) > MaxBytes {
		return nil, invalid
	}
	p.files["flow.json"], p.identity = raw, Digest(raw)
	return p, nil
}

func bindRuntimeVariable(d *FlowTest, instance string) error {
	if d.Schema != RuntimeFlowTestSchema || d.Schedule != nil || d.Servers != nil {
		return invalid
	}
	name := ""
	for i := range d.Variables {
		v := &d.Variables[i]
		if v.Kind == "runtime-instance" {
			if name != "" || !identifier.MatchString(v.ID) || v.Value != "" || v.Namespace != "" || v.OffsetMS != 0 {
				return invalid
			}
			name = v.ID
			v.Kind, v.Value = "literal", instance
		}
	}
	if name == "" {
		return invalid
	}
	for _, step := range d.Steps {
		if step.V2 == nil {
			return invalid
		}
		markers := 0
		selectors := []hl7.Parts{}
		for _, assignment := range step.V2.Assignments {
			selector, err := hl7.ParseSelector(assignment.Selector)
			if err != nil {
				return invalid
			}
			parts := selector.Parts()
			for _, prior := range selectors {
				if overlappingRuntimeSelectors(prior, parts) {
					return invalid
				}
			}
			selectors = append(selectors, parts)
			if assignment.Variable == name {
				markers++
			}
		}
		if markers != 1 {
			return invalid
		}
	}
	d.Schema = FlowTestSchema
	return nil
}

func overlappingRuntimeSelectors(a, b hl7.Parts) bool {
	return a.Segment == b.Segment && a.Occurrence == b.Occurrence && a.Field == b.Field && a.Repetition == b.Repetition &&
		(a.Component == 0 || b.Component == 0 || a.Component == b.Component &&
			(a.Subcomponent == 0 || b.Subcomponent == 0 || a.Subcomponent == b.Subcomponent))
}
