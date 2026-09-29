package connectedrun

import (
	"bytes"
	"encoding/json/v2"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
)

const FlowConfigSchema = "readmit-connected-run-config/v3"
const FlowSchema = "readmit-connected-run/v3"

type IsolationSelection struct {
	TransitionRead    *Grant `json:"transition_read,omitzero"`
	TransitionCleanup *Grant `json:"transition_cleanup,omitzero"`
	Registry          string `json:"registry"`
	Policy            string `json:"policy"`
	Read              Grant  `json:"read"`
	Setup             Grant  `json:"setup"`
	Cleanup           Grant  `json:"cleanup"`
}
type FlowConfig struct {
	RecoveryStore string              `json:"recovery_store,omitzero"`
	Schema        string              `json:"schema"`
	Phases        map[string]ConfigV2 `json:"phases"`
	Isolation     IsolationSelection  `json:"isolation"`
	Seed          uint64              `json:"seed"`
}
type PreparedFlow struct {
	store                          *RecoveryStore
	transitions                    *testisolation.TransitionPrepared
	plan                           *connectedtest.FlowPlan
	phases                         map[string]*Prepared
	isolation                      *testisolation.Prepared
	fhir                           *fhirFlow
	selection                      IsolationSelection
	instance, planPath, configPath string
	raw                            []byte
}

// PrepareFlow derives every executable phase from a single pinned test. It
// reads configuration only; grants and manual confirmation confer no authority
// until execution checks them at the actual effect boundary.
func PrepareFlow(planPath, configPath, instance string) (*PreparedFlow, error) {
	if !safeID(instance) {
		return nil, invalid
	}
	plan, err := connectedtest.OpenFlowPlan(planPath)
	if err != nil {
		return nil, err
	}
	raw, err := (artifactdir.Document{MaxBytes: 2 << 20}).Read(configPath)
	if err != nil {
		return nil, err
	}
	if plan.Document().Schema == connectedtest.FHIRFlowPlanSchema {
		return prepareFHIRFlow(plan, planPath, configPath, instance, raw)
	}
	var c FlowConfig
	if json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil || c.Schema != FlowConfigSchema || len(c.Phases) != len(plan.Document().Test.Phases) {
		return nil, invalid
	}
	root, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		return nil, err
	}
	anchor := func(s string) string {
		if s == "" {
			return ""
		}
		return artifactpath.JoinReference(root, s)
	}
	selection := anchorIsolation(c.Isolation, anchor)
	isolation, err := prepareFlowIsolation(plan, selection, instance, c.Seed)
	if err != nil {
		return nil, err
	}
	p := &PreparedFlow{plan: plan, phases: map[string]*Prepared{}, isolation: isolation, selection: selection, instance: instance, planPath: planPath, configPath: configPath, raw: bytes.Clone(raw)}
	if c.RecoveryStore != "" {
		store, e := readRecoveryStore(anchor(c.RecoveryStore))
		if e != nil {
			return nil, e
		}
		p.store = &store
	}
	if err = p.prepareTransitions(); err != nil {
		return nil, err
	}
	for _, phase := range plan.Document().Test.Phases {
		config, ok := c.Phases[phase.ID]
		if !ok {
			return nil, invalid
		}
		childRaw, _ := json.Marshal(config, json.Deterministic(true))
		child, err := prepareIntervalMode(filepath.Join(planPath, "phases", phase.ID), configPath, plan.Phase(phase.ID), childRaw, true, plan.Schedule(phase.ID))
		if err != nil {
			return nil, err
		}
		child.verify = p.unchanged
		child.store = p.store
		p.phases[phase.ID] = child
	}
	if plan.Document().Test.Boundary == "application-state" {
		business := false
		for _, child := range p.phases {
			for _, source := range child.sources {
				if source.definition.Phase == "after" && source.capture == nil && source.source.Capture == nil {
					business = true
				}
			}
		}
		if !business {
			return nil, invalid
		}
	}
	return p, nil
}
func anchorIsolation(selection IsolationSelection, anchor func(string) string) IsolationSelection {
	selection.Registry = anchor(selection.Registry)
	selection.Policy = anchor(selection.Policy)
	selection.Read.Path = anchor(selection.Read.Path)
	selection.Setup.Path = anchor(selection.Setup.Path)
	selection.Cleanup.Path = anchor(selection.Cleanup.Path)
	if selection.TransitionRead != nil {
		v := *selection.TransitionRead
		v.Path = anchor(v.Path)
		selection.TransitionRead = &v
	}
	if selection.TransitionCleanup != nil {
		v := *selection.TransitionCleanup
		v.Path = anchor(v.Path)
		selection.TransitionCleanup = &v
	}
	return selection
}
func prepareFlowIsolation(plan *connectedtest.FlowPlan, selection IsolationSelection, instance string, seed uint64) (*testisolation.Prepared, error) {
	return testisolation.Prepare(plan.Dependency(plan.Document().Test.Isolation), selection.Registry, selection.Policy, testisolation.Options{ParentPlan: plan.Identity(), Instance: instance, Seed: seed})
}

// prepareTransitions binds declared successor states to their own reviews.
func (p *PreparedFlow) prepareTransitions() error {
	policy := flowTransitionPolicy(p.plan)
	if len(policy.Phases) == 0 {
		if p.selection.TransitionRead != nil || p.selection.TransitionCleanup != nil {
			return invalid
		}
		return nil
	}
	if p.selection.TransitionRead == nil || p.selection.TransitionCleanup == nil {
		return invalid
	}
	var err error
	p.transitions, err = testisolation.PrepareTransitions(p.isolation, policy)
	return err
}
func (p *PreparedFlow) Bindings() map[string]networkaction.Binding {
	out := map[string]networkaction.Binding{}
	if p.fhir != nil {
		out = p.fhir.bindings(p.store)
	}
	for id, phase := range p.phases {
		for role, b := range phase.Bindings() {
			out[id+":"+role] = scopeStore(b, p.store)
		}
	}
	for _, role := range []string{"read", "setup", "cleanup"} {
		out["isolation:"+role] = scopeStore(p.isolation.Review(role).Binding, p.store)
	}
	if p.transitions != nil {
		for _, role := range []string{"read", "cleanup"} {
			review, _ := p.transitions.Review(role)
			out["isolation:transition-"+role] = scopeStore(review.Binding, p.store)
		}
	}
	return out
}
func (p *PreparedFlow) IsolationReviews() map[string]testisolation.Review {
	out := map[string]testisolation.Review{}
	for _, role := range []string{"read", "setup", "cleanup"} {
		review := p.isolation.Review(role)
		review.Binding = scopeStore(review.Binding, p.store)
		out[role] = review
	}
	if p.transitions != nil {
		for _, role := range []string{"read", "cleanup"} {
			review, _ := p.transitions.Review(role)
			review.Binding = scopeStore(review.Binding, p.store)
			out["transition-"+role] = review
		}
	}
	return out
}
func (p *PreparedFlow) authorities() testisolation.Authorities {
	return testisolation.Authorities{Read: storeAuthority{p.selection.Read.authority(), p.store}, Setup: storeAuthority{p.selection.Setup.authority(), p.store}, Cleanup: storeAuthority{p.selection.Cleanup.authority(), p.store}}
}
func (p *PreparedFlow) unchanged() error {
	raw, err := (artifactdir.Document{MaxBytes: 2 << 20}).Read(p.configPath)
	if err != nil || !bytes.Equal(raw, p.raw) {
		return invalid
	}
	fresh, err := PrepareFlow(p.planPath, p.configPath, p.instance)
	if err != nil || fresh.plan.Identity() != p.plan.Identity() || fresh.isolation.Identity() != p.isolation.Identity() {
		return invalid
	}
	a, _ := json.Marshal(p.Bindings(), json.Deterministic(true))
	b, _ := json.Marshal(fresh.Bindings(), json.Deterministic(true))
	if !bytes.Equal(a, b) {
		return invalid
	}
	for id, phase := range p.phases {
		for i, s := range phase.sources {
			if s.source.Identity() != fresh.phases[id].sources[i].source.Identity() {
				return invalid
			}
		}
	}
	if (p.fhir == nil) != (fresh.fhir == nil) {
		return invalid
	}
	if p.fhir != nil {
		for id, phase := range p.fhir.phases {
			for i, s := range phase.sources {
				if s.source.Identity() != fresh.fhir.phases[id].sources[i].source.Identity() {
					return invalid
				}
			}
		}
	}
	return nil
}

func flowTransitionPolicy(plan *connectedtest.FlowPlan) testisolation.TransitionPolicy {
	p := testisolation.TransitionPolicy{Schema: testisolation.TransitionPolicySchema, ParentPlan: plan.Identity(), Phases: []testisolation.Transition{}}
	for _, phase := range plan.Document().Test.Phases {
		for _, change := range phase.IsolationChanges {
			p.Phases = append(p.Phases, testisolation.Transition{Phase: phase.ID, Alias: change.Alias, Attributes: change.Attributes})
		}
	}
	return p
}
