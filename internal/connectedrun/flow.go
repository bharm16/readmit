package connectedrun

import (
	"context"
	"encoding/json/v2"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
)

type FlowCheck struct {
	ID      string            `json:"id"`
	Outcome assertion.Outcome `json:"outcome"`
}
type FlowPhaseResult struct {
	ID              string                  `json:"id"`
	State           string                  `json:"state"`
	Verdict         assertion.Verdict       `json:"verdict"`
	RunIdentity     string                  `json:"run_identity"`
	Checks          []FlowCheck             `json:"checks"`
	Steps           []connectedtest.Attempt `json:"steps"`
	Wire            *assertion.Report       `json:"wire,omitzero"`
	EvaluationError string                  `json:"evaluation_error,omitzero"`
}

// FlowClaim is the fixed meaning of one declared FHIR observation boundary.
// It comes from the reviewed plan, never from observed data, and limits what
// a verdict over that observation may be read to say.
type FlowClaim struct {
	Phase    string `json:"phase"`
	Dataset  string `json:"dataset"`
	Boundary string `json:"boundary"`
	Meaning  string `json:"meaning"`
}
type FlowResult struct {
	Qualification []FlowClaim       `json:"qualification,omitzero"`
	RecoveryStore *RecoveryStore    `json:"recovery_store,omitzero"`
	Previous      string            `json:"previous,omitzero"`
	Inherited     int               `json:"inherited,omitzero"`
	Schema        string            `json:"schema"`
	Plan          string            `json:"plan"`
	Instance      string            `json:"instance"`
	Boundary      string            `json:"boundary"`
	Engine        string            `json:"engine"`
	State         string            `json:"state"`
	Verdict       assertion.Verdict `json:"verdict"`
	StartedAt     time.Time         `json:"started_at"`
	CompletedAt   time.Time         `json:"completed_at"`
	Setup         string            `json:"setup"`
	Cleanup       string            `json:"cleanup"`
	Isolation     string            `json:"isolation"`
	Phases        []FlowPhaseResult `json:"phases"`
}
type flowIntent struct {
	Phase string    `json:"phase"`
	Plan  string    `json:"plan"`
	At    time.Time `json:"at"`
}

var flowResultFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected lifecycle", AllowEmpty: flowEmptyDirectory, AllowedDirectories: []string{"intents"}, Nested: []string{"plan", "phases", "preflight", "isolation", "previous", "continuation", "transitions"}, RequiredFiles: []string{"started.json"}, AllowFile: func(n string) bool {
	return n == "manifest.json" || n == "identity.sha256" || n == "started.json" || strings.HasPrefix(n, "phase-") && strings.HasSuffix(n, ".json") || len(n) > 8 && n[:8] == "intents/"
}, MaxFiles: 200000, MaxFileBytes: 64 << 20, MaxBytes: 1 << 30}, Seal: artifactdir.DirectoryHash(FlowSchema)}

func (p *PreparedFlow) IsolationIdentity() string { return p.isolation.Identity() }

// ExecuteFlow is the single lifecycle service call. Preparation is not a
// verdict; every phase is acquired and executed here under live authority.
func ExecuteFlow(ctx context.Context, p *PreparedFlow, output string, confirmation testisolation.Confirmation, observers ...func(FlowPhaseResult)) (FlowResult, error) {
	if p != nil && p.runtime != nil {
		return executeRuntimeFlow(ctx, p, output, confirmation, observers...)
	}
	return executeConcreteFlow(ctx, p, output, confirmation, observers...)
}

func executeConcreteFlow(ctx context.Context, p *PreparedFlow, output string, confirmation testisolation.Confirmation, observers ...func(FlowPhaseResult)) (FlowResult, error) {
	if p == nil || len(observers) > 1 || p.unchanged() != nil {
		return FlowResult{}, invalid
	}
	ctx, cancel := context.WithTimeout(ctx, duration(p.plan.Document().Test.Limits.DeadlineMS))
	defer cancel()
	w, err := artifactdir.Create(output, flowFamilyFor(p.plan), artifactdir.Durable)
	if err != nil {
		return FlowResult{}, err
	}
	defer w.Close()
	r := initialFlow(p.plan, p.instance, time.Now().UTC())
	r.RecoveryStore = p.store
	if put(w, "started.json", r) != nil || w.Mkdir("intents") != nil || w.Mkdir("phases") != nil {
		return FlowResult{}, invalid
	}
	if err = p.plan.Write(ctx, filepath.Join(w.Path(), "plan")); err != nil {
		return FlowResult{}, err
	}
	if err = w.Sync(); err != nil {
		return FlowResult{}, err
	}
	finish := func() (FlowResult, error) {
		summarizeFlow(&r)
		r.CompletedAt = time.Now().UTC()
		if ctx.Err() != nil && r.State != "uncertain" {
			r.State = "cancelled"
			if r.Verdict == assertion.VerdictPass {
				r.Verdict = assertion.VerdictUndecided
			}
		}
		if put(w, "manifest.json", r) != nil {
			return FlowResult{}, invalid
		}
		if _, err := w.Seal(nil); err != nil {
			return FlowResult{}, err
		}
		return OpenFlow(context.WithoutCancel(ctx), w.Path())
	}
	if _, err = p.isolation.Preflight(ctx, p.authorities(), filepath.Join(w.Path(), "preflight")); err != nil {
		r.Setup = "failed"
		return finish()
	}
	session, err := testisolation.Start(ctx, p.isolation, p.authorities(), filepath.Join(w.Path(), "preflight"), filepath.Join(w.Path(), "isolation"), confirmation)
	if session == nil {
		r.Setup = "failed"
		return finish()
	}
	defer session.Close()
	r.Isolation = p.isolation.Identity()
	r.Setup = session.Result().Setup
	r.Cleanup = session.Result().Cleanup
	var observer func(FlowPhaseResult)
	if len(observers) == 1 {
		observer = observers[0]
	}
	var transitions *testisolation.TransitionSession
	if err == nil && session.Ready() && p.transitions != nil {
		transitions, err = testisolation.StartTransitions(ctx, session, p.transitions, testisolation.Authorities{Read: storeAuthority{p.selection.TransitionRead.authority(), p.store}, Cleanup: storeAuthority{p.selection.TransitionCleanup.authority(), p.store}}, filepath.Join(w.Path(), "transitions"))
	}
	if transitions != nil {
		defer transitions.Close()
		if err == nil && transitions.Ready() {
			accept := func(phase connectedtest.FlowPhase, result FlowPhaseResult) error {
				if len(phase.IsolationChanges) == 0 {
					return nil
				}
				if result.State != "complete" || result.Verdict != assertion.VerdictPass {
					return invalid
				}
				return transitions.Accept(ctx, phase.ID, flowDigest(result))
			}
			if err = executeFlowPhases(ctx, p, w, &r, 0, transitions.Check, accept, observer); err != nil {
				return FlowResult{}, err
			}
		}
		if ctx.Err() == nil {
			_ = transitions.Cleanup(ctx)
		}
		r.Setup = transitions.Result().Setup
		r.Cleanup = transitions.Result().Cleanup
		transitions.Close()
	} else {
		if err == nil && session.Ready() {
			if err = executeFlowPhases(ctx, p, w, &r, 0, session.CheckReady, nil, observer); err != nil {
				return FlowResult{}, err
			}
		}
		if ctx.Err() == nil {
			_ = session.Cleanup(ctx)
		}
		r.Setup = session.Result().Setup
		r.Cleanup = session.Result().Cleanup
		session.Close()
	}

	return finish()
}

// flowFamilyFor seals a v5 flow under its own result version.
func flowFamilyFor(plan *connectedtest.FlowPlan) artifactdir.Family {
	f := flowResultFamily
	f.Seal = artifactdir.DirectoryHash(flowSchemaFor(plan))
	return f
}
func flowSchemaFor(plan *connectedtest.FlowPlan) string {
	if plan.Document().Schema == connectedtest.FHIRFlowPlanSchema {
		return FlowSchemaV4
	}
	return FlowSchema
}

// phaseSchemaFor is the version each v2 phase of the lifecycle is retained
// under: a scheduled lifecycle's phases record their declared delays.
func phaseSchemaFor(plan *connectedtest.FlowPlan) string {
	if plan.Document().Schema == connectedtest.ScheduledFlowPlanSchema {
		return ScheduledPhaseSchema
	}
	return PhaseSchema
}
func initialFlow(plan *connectedtest.FlowPlan, instance string, at time.Time) FlowResult {
	d := plan.Document().Test
	r := FlowResult{Schema: flowSchemaFor(plan), Plan: plan.Identity(), Instance: instance, Boundary: d.Boundary, Engine: engine.Version(), State: "incomplete", Verdict: assertion.VerdictUndecided, StartedAt: at, Setup: "not-started", Cleanup: "not-started", Phases: []FlowPhaseResult{}}
	for _, phase := range d.Phases {
		r.Phases = append(r.Phases, unexecutedPhase(plan, phase, "not-attempted"))
		for _, ds := range phase.Datasets {
			if ds.Kind != "fhir-resources" {
				continue
			}
			o, _, _, err := plan.Phase(phase.ID).ObservationURL(ds.ID)
			if err == nil {
				r.Qualification = append(r.Qualification, FlowClaim{Phase: phase.ID, Dataset: ds.ID, Boundary: o.Boundary, Meaning: fhirobserve.Meaning(o.Boundary)})
			}
		}
	}
	return r
}
func unexecutedPhase(plan *connectedtest.FlowPlan, p connectedtest.FlowPhase, state string) FlowPhaseResult {
	r := FlowPhaseResult{ID: p.ID, State: state, Verdict: assertion.VerdictUndecided, Checks: []FlowCheck{}, Steps: []connectedtest.Attempt{}}
	outcome := assertion.OutcomeUndecided
	if state == "skipped" {
		outcome = assertion.OutcomeSkipped
	}
	set, _ := assertion.DecodeDatasets(plan.Phase(p.ID).Files()["dependencies/"+p.Checks.SHA256])
	for _, c := range set.Document().Assertions {
		r.Checks = append(r.Checks, FlowCheck{ID: "typed:" + c.ID, Outcome: outcome})
	}
	if p.Wire != nil {
		set, _ := assertion.Decode(plan.Dependency(p.Wire.Set))
		for _, c := range set.Assertions {
			r.Checks = append(r.Checks, FlowCheck{ID: "wire:" + c.ID, Outcome: outcome})
		}
	}
	for _, c := range p.Responses {
		r.Checks = append(r.Checks, FlowCheck{ID: "response:" + c.ID, Outcome: outcome})
	}
	for _, c := range p.Validations {
		r.Checks = append(r.Checks, FlowCheck{ID: "validation:" + c.ID, Outcome: outcome})
	}
	steps := plan.Document().Test.Steps
	for _, id := range p.Steps {
		kind := "v2-send"
		if i := slices.IndexFunc(steps, func(s connectedtest.Step) bool { return s.ID == id }); i >= 0 && steps[i].Interaction != nil {
			kind = "fhir-interaction"
		}
		r.Steps = append(r.Steps, connectedtest.Attempt{Step: id, Kind: kind, Outcome: state})
	}
	return r
}
func phaseDecision(p connectedtest.FlowPhase, prior []FlowPhaseResult) string {
	find := func(id string) FlowPhaseResult {
		for _, r := range prior {
			if r.ID == id {
				return r
			}
		}
		return FlowPhaseResult{}
	}
	for _, dep := range p.After {
		r := find(dep.Phase)
		if r.State != "complete" || dep.Requires == "pass" && r.Verdict != assertion.VerdictPass {
			return "blocked"
		}
	}
	if p.When != nil {
		r := find(p.When.Phase)
		for _, c := range r.Checks {
			if c.ID == p.When.Check {
				if c.Outcome == assertion.OutcomeUndecided {
					return "blocked"
				}
				if string(c.Outcome) != p.When.Outcome {
					return "skipped"
				}
				return "ready"
			}
		}
		return "blocked"
	}
	return "ready"
}
func summarizeFlow(r *FlowResult) {
	r.State = "complete"
	r.Verdict = assertion.VerdictUndecided
	passed, failed, unresolved := 0, 0, false
	for _, phase := range r.Phases {
		if phase.State != "complete" && phase.State != "skipped" {
			unresolved = true
			r.State = "incomplete"
		}
		if phase.State == "uncertain" {
			r.State = "uncertain"
		}
		for _, c := range phase.Checks {
			switch c.Outcome {
			case assertion.OutcomePassed:
				passed++
			case assertion.OutcomeFailed:
				failed++
			case assertion.OutcomeUndecided:
				unresolved = true
			}
		}
	}
	// Uncertainty cannot be overwritten by a later unattempted phase.
	for _, phase := range r.Phases {
		if phase.State == "uncertain" {
			r.State = "uncertain"
		}
	}
	if r.Setup != "ready" && r.Setup != "continued-ready" && r.Setup != "transition-ready" || r.Cleanup != "complete" {
		unresolved = true
		if r.State != "uncertain" {
			r.State = "incomplete"
		}
	}
	if r.Setup == "uncertain" || r.Cleanup == "uncertain" {
		r.State = "uncertain"
	}
	if failed > 0 {
		r.Verdict = assertion.VerdictFail
	} else if passed > 0 && !unresolved {
		r.Verdict = assertion.VerdictPass
	}
}
func canonicalFlow(v any) []byte { b, _ := json.Marshal(v, json.Deterministic(true)); return b }
func flowDigest(v any) string    { return networkaction.Digest(canonicalFlow(v)) }

func executeFlowPhases(ctx context.Context, p *PreparedFlow, w *artifactdir.Writer, r *FlowResult, start int, check func(context.Context) error, accept func(connectedtest.FlowPhase, FlowPhaseResult) error, observer func(FlowPhaseResult)) error {
	bound := map[string]string{}
	for i, phase := range p.plan.Document().Test.Phases {
		if i < start {
			continue
		}
		decision := phaseDecision(phase, r.Phases[:i])
		if decision != "ready" {
			r.Phases[i] = unexecutedPhase(p.plan, phase, decision)
			continue
		}
		if ctx.Err() != nil || p.unchanged() != nil || check(ctx) != nil {
			break
		}
		intent := flowIntent{Phase: phase.ID, Plan: p.plan.Phase(phase.ID).Identity(), At: time.Now().UTC()}
		if put(w, "intents/"+phase.ID+".json", intent) != nil || w.Sync() != nil {
			return invalid
		}
		if p.fhir != nil {
			stop, err := executeFlowFHIRPhase(ctx, p, w, r, i, phase, bound, accept, observer)
			if err != nil {
				return err
			}
			if stop {
				break
			}
			continue
		}
		result, runErr := Execute(ctx, p.phases[phase.ID], p.instance, filepath.Join(w.Path(), "phases", phase.ID))
		if runErr != nil {
			files, e := artifactdir.Read(w.Path(), flowResultFamily.Layout)
			if e != nil {
				return e
			}
			partial, e := interruptedPhase(p.plan, phase, artifactdir.Subtree(files, "phases/"+phase.ID))
			if e != nil {
				return e
			}
			r.Phases[i] = partial
			break
		}
		evaluated, e := evaluateFlowPhase(context.WithoutCancel(ctx), p.plan, phase, result, filepath.Join(w.Path(), "phases", phase.ID))
		if e != nil {
			r.Phases[i].State = "uncertain"
			break
		}
		r.Phases[i] = evaluated
		if accept != nil && accept(phase, evaluated) != nil {
			break
		}
		if put(w, "phase-"+phase.ID+".json", evaluated) != nil || w.Sync() != nil {
			return invalid
		}
		if observer != nil {
			var detached FlowPhaseResult
			_ = json.Unmarshal(canonicalFlow(evaluated), &detached)
			observer(detached)
		}
		if result.State == "uncertain" || result.State == "cancelled" {
			break
		}
	}
	return nil
}

// executeFlowFHIRPhase runs and verifies one v5 phase, carrying response
// values only from the verified retained phase into later phases.
func executeFlowFHIRPhase(ctx context.Context, p *PreparedFlow, w *artifactdir.Writer, r *FlowResult, i int, phase connectedtest.FlowPhase, bound map[string]string, accept func(connectedtest.FlowPhase, FlowPhaseResult) error, observer func(FlowPhaseResult)) (bool, error) {
	dir := filepath.Join(w.Path(), "phases", phase.ID)
	run, runErr := executeFHIRPhase(ctx, p, phase, bound, dir)
	nested, err := artifactdir.Read(dir, fhirPhaseFamily.Layout)
	if err != nil {
		return true, err
	}
	if runErr != nil {
		partial, e := interruptedFHIRPhase(p.plan, phase, nested)
		if e != nil {
			return true, e
		}
		r.Phases[i] = partial
		return true, nil
	}
	evaluated, verified, _, err := openFHIRPhase(context.WithoutCancel(ctx), p.plan, phase, dir, nested, bound)
	if err != nil {
		r.Phases[i].State = "uncertain"
		return true, nil
	}
	r.Phases[i] = evaluated
	for k, v := range verified.Bound {
		bound[k] = v
	}
	if accept != nil && accept(phase, evaluated) != nil {
		return true, nil
	}
	if put(w, "phase-"+phase.ID+".json", evaluated) != nil || w.Sync() != nil {
		return true, invalid
	}
	if observer != nil {
		var detached FlowPhaseResult
		_ = json.Unmarshal(canonicalFlow(evaluated), &detached)
		observer(detached)
	}
	return run.State == "uncertain" || run.State == "cancelled", nil
}
