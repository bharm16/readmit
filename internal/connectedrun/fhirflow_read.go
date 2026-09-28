package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// evaluate is the one evaluation of a v5 phase, used by execution and by
// the offline reader. Typed checks run through the shared evaluator only when
// the stimulus settled and every declared observation is complete.
func (x fhirReader) evaluate(ctx context.Context, r FHIRPhaseRun, evidence map[string]assertion.Table) (FHIREvaluation, error) {
	plan, dir := x.plan, x.dir
	d := plan.Document().Test
	e := FHIREvaluation{Schema: FHIREvaluationSchema, Responses: []FlowCheck{}, Validations: []FlowCheck{}}
	if r.State == "complete" && len(evidence) == len(d.Datasets) {
		set, err := assertion.DecodeDatasets(plan.Files()["dependencies/"+d.Checks.SHA256])
		if err != nil {
			return e, err
		}
		if report, err := set.EvaluateTables(ctx, r.Instance, evidence); err == nil {
			e.Typed = &report
		}
	}
	for _, c := range d.Responses {
		outcome := assertion.OutcomeUndecided
		for _, s := range r.Steps {
			if s.Step == c.Step && s.State == "complete" {
				outcome = assertion.OutcomeFailed
				if s.Outcome == c.Outcome {
					outcome = assertion.OutcomePassed
				}
			}
		}
		e.Responses = append(e.Responses, FlowCheck{ID: "response:" + c.ID, Outcome: outcome})
	}
	for _, c := range d.Validations {
		outcome := assertion.OutcomeUndecided
		if identity := r.Validations[c.ID]; networkaction.ValidDigest(identity) {
			retained, err := fhirvalidator.Open(ctx, filepath.Join(dir, "validations", c.ID))
			if err != nil || retained.Identity() != identity {
				return e, invalid
			}
			check, err := retained.Check(c.ID)
			if err != nil {
				return e, invalid
			}
			outcome = check.Result.Outcome
		}
		e.Validations = append(e.Validations, FlowCheck{ID: "validation:" + c.ID, Outcome: outcome})
	}
	if x.phase.Wire != nil && r.Transport != "" {
		if report, err := x.wireReport(ctx); err == nil {
			e.Wire = &report
		}
	}
	return e, nil
}

// wireReport evaluates the pinned wire assertions over the original inputs
// and the actual acknowledgements only; application state is never inferred.
func (x fhirReader) wireReport(ctx context.Context) (assertion.Report, error) {
	flow, phase, dir := x.flow, x.phase, x.dir
	evidence := assertion.Evidence{Input: map[string]assertion.Message{}, Observed: map[string]assertion.Message{}}
	run, err := replay.Open(filepath.Join(dir, "transport", "run"))
	if err != nil {
		return assertion.Report{}, err
	}
	parse := func(raw []byte) (assertion.Message, error) {
		doc, e := hl7.Parse(raw, hl7.Options{})
		if e != nil || len(doc.Messages) != 1 {
			return assertion.Message{}, invalid
		}
		return assertion.Message{Document: doc}, nil
	}
	for _, event := range run.Events {
		raw, e := run.Raw(event.Sent)
		if e != nil {
			return assertion.Report{}, e
		}
		if evidence.Input[event.SourceOccurrence], e = parse(raw); e != nil {
			return assertion.Report{}, e
		}
		raw, e = run.Raw(event.Received)
		if e != nil {
			return assertion.Report{}, e
		}
		key := event.SourceOccurrence
		for alias, step := range phase.Wire.Acknowledgements {
			for i, id := range phase.Steps {
				if id == step && run.Events[i].OutboundOccurrence == event.OutboundOccurrence {
					key = alias
				}
			}
		}
		if _, exists := evidence.Observed[key]; exists {
			return assertion.Report{}, invalid
		}
		if evidence.Observed[key], e = parse(raw); e != nil {
			return assertion.Report{}, e
		}
	}
	set, err := assertion.Decode(flow.Dependency(phase.Wire.Set))
	if err != nil {
		return assertion.Report{}, err
	}
	return set.Evaluate(ctx, evidence)
}

// result summarizes a verified v5 phase for the flow result.
func (x fhirReader) result(r FHIRPhaseRun, e FHIREvaluation, identity string) (FlowPhaseResult, error) {
	flow, phase, dir := x.flow, x.phase, x.dir
	out := unexecutedPhase(flow, phase, r.State)
	out.RunIdentity = identity
	if r.Transport != "" {
		transport, err := replay.Open(filepath.Join(dir, "transport", "run"))
		if err != nil || transport.Identity != r.Transport || len(transport.Events) != len(out.Steps) {
			return out, invalid
		}
		for i, event := range transport.Events {
			attempt := connectedtest.Attempt{Step: out.Steps[i].Step, Kind: "v2-send", Outcome: "unknown"}
			switch event.Delivery {
			case "acknowledged":
				attempt.Outcome = "complete"
			case "uncertain":
				attempt.Uncertain = true
			case "not_sent":
				attempt.Outcome = "not-attempted"
			}
			out.Steps[i] = attempt
		}
	}
	for i, s := range r.Steps {
		if out.Steps[i].Step != s.Step || out.Steps[i].Kind != "fhir-interaction" {
			return out, invalid
		}
		attempt := connectedtest.Attempt{Step: s.Step, Kind: "fhir-interaction", Outcome: s.State}
		if s.State == "uncertain" {
			attempt.Outcome, attempt.Uncertain = "unknown", true
		}
		out.Steps[i] = attempt
	}
	checks := []FlowCheck{}
	set, err := assertion.DecodeDatasets(flow.Phase(phase.ID).Files()["dependencies/"+phase.Checks.SHA256])
	if err != nil {
		return out, err
	}
	for i, c := range set.Document().Assertions {
		outcome := assertion.OutcomeUndecided
		if e.Typed != nil {
			if e.Typed.Results[i].ID != c.ID {
				return out, invalid
			}
			outcome = e.Typed.Results[i].Outcome
		}
		checks = append(checks, FlowCheck{ID: "typed:" + c.ID, Outcome: outcome})
	}
	if phase.Wire != nil {
		wire, err := assertion.Decode(flow.Dependency(phase.Wire.Set))
		if err != nil {
			return out, err
		}
		for i, c := range wire.Assertions {
			outcome := assertion.OutcomeUndecided
			if e.Wire != nil {
				outcome = e.Wire.Results[i].Outcome
			}
			checks = append(checks, FlowCheck{ID: "wire:" + c.ID, Outcome: outcome})
		}
		out.Wire = e.Wire
	}
	checks = append(append(checks, e.Responses...), e.Validations...)
	if len(checks) != len(out.Checks) {
		return out, invalid
	}
	for i := range checks {
		if checks[i].ID != out.Checks[i].ID {
			return out, invalid
		}
	}
	out.Checks = checks
	out.Verdict = phaseVerdict(out.Checks, r.State)
	return out, nil
}

// fhirReader holds what verifying one retained v5 phase needs: the plan, the
// response values earlier phases bound, and the phase's own retained bytes.
type fhirReader struct {
	flow   *connectedtest.FlowPlan
	phase  connectedtest.FlowPhase
	plan   *connectedtest.Plan
	dir    string
	files  map[string][]byte
	inputs map[string]string
}

// openFHIRPhase verifies one sealed v5 phase offline. It returns the phase
// summary and the response values its steps bound for later phases.
func openFHIRPhase(ctx context.Context, flow *connectedtest.FlowPlan, phase connectedtest.FlowPhase, dir string, files map[string][]byte, inputs map[string]string) (FlowPhaseResult, FHIRPhaseRun, string, error) {
	result, r, identity, _, err := openFHIRPhaseTables(ctx, flow, phase, dir, files, inputs)
	return result, r, identity, err
}

// openFHIRPhaseTables is openFHIRPhase that also answers the tables the
// evaluator read, each re-derived from retained bytes by its own reader.
func openFHIRPhaseTables(ctx context.Context, flow *connectedtest.FlowPlan, phase connectedtest.FlowPhase, dir string, files map[string][]byte, inputs map[string]string) (FlowPhaseResult, FHIRPhaseRun, string, map[string]assertion.Table, error) {
	x := fhirReader{flow: flow, phase: phase, plan: flow.Phase(phase.ID), dir: dir, files: files, inputs: inputs}
	identity := strings.TrimSpace(string(files["identity.sha256"]))
	var r, start FHIRPhaseRun
	if identity == "" || identity != artifactdir.Identity(PhaseSchemaV2, files) || json.Unmarshal(files["manifest.json"], &r, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || r.Schema != PhaseSchemaV2 || r.Plan != x.plan.Identity() || !safeID(r.Instance) || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) {
		return FlowPhaseResult{}, r, "", nil, invalid
	}
	if !artifactdir.MatchesSubtree(files, "plan", connectedtest.PhasePlanSchemaV2, artifactdir.Identity(connectedtest.PhasePlanSchemaV2, x.plan.Files())) {
		return FlowPhaseResult{}, r, "", nil, invalid
	}
	used := map[string]string{}
	for _, s := range x.plan.Document().Test.Steps {
		for _, name := range connectedtest.ResponseVariables(s, x.plan.Document().Test.Variables, x.plan.Files()["inputs/"+s.ID+".fhir"]) {
			if v, ok := inputs[name]; ok {
				used[name] = v
			}
		}
	}
	expected := x.initial(r.Instance, r.StartedAt, used)
	if !bytes.Equal(canonicalFlow(start), canonicalFlow(expected)) {
		return FlowPhaseResult{}, r, "", nil, invalid
	}
	derived, evidence, err := x.derive(ctx, r)
	if err != nil {
		return FlowPhaseResult{}, r, "", nil, err
	}
	if !bytes.Equal(canonicalFlow(derived), canonicalFlow(r)) {
		return FlowPhaseResult{}, r, "", nil, invalid
	}
	evaluation, err := x.evaluate(ctx, r, evidence)
	if err != nil {
		return FlowPhaseResult{}, r, "", nil, err
	}
	if r.Stage == "evaluated" {
		if r.Evaluation != flowDigest(evaluation) || !bytes.Equal(files["evaluation.json"], canonicalFlow(evaluation)) {
			return FlowPhaseResult{}, r, "", nil, invalid
		}
	} else if r.Evaluation != "" || files["evaluation.json"] != nil {
		return FlowPhaseResult{}, r, "", nil, invalid
	}
	result, err := x.result(r, evaluation, identity)
	return result, r, identity, evidence, err
}

func (x fhirReader) initial(instance string, at time.Time, inputs map[string]string) FHIRPhaseRun {
	r := FHIRPhaseRun{Schema: PhaseSchemaV2, Plan: x.plan.Identity(), Instance: instance, State: "incomplete", Stage: "preflight", StartedAt: at, Inputs: inputs, Bound: map[string]string{}, Preflight: map[string]string{}, Armed: map[string]string{}, Observations: map[string]string{}, Intervals: map[string]string{}, Boundaries: map[string]string{}, Acquisitions: map[string][]string{}, Steps: []FHIRStepRecord{}, Validations: map[string]string{}}
	for _, s := range x.plan.Document().Test.Steps {
		if s.Interaction != nil {
			r.Steps = append(r.Steps, FHIRStepRecord{Step: s.ID, State: "not-attempted"})
		}
	}
	return r
}

// within reports whether retained actual I/O falls inside the phase's own run.
func within(r FHIRPhaseRun, start, end time.Time) bool {
	return !start.IsZero() && !end.IsZero() && !end.Before(start) && !start.Before(r.StartedAt) && !end.After(r.CompletedAt)
}

// derive rebuilds the manifest from retained evidence alone. Each claimed
// artifact is reopened by its own reader; state is recomputed, not trusted.
func (x fhirReader) derive(ctx context.Context, claimed FHIRPhaseRun) (FHIRPhaseRun, map[string]assertion.Table, error) {
	d := x.plan.Document().Test
	r := x.initial(claimed.Instance, claimed.StartedAt, claimed.Inputs)
	r.CompletedAt, r.StimulusAt, r.SentAt = claimed.CompletedAt, claimed.StimulusAt, claimed.SentAt
	evidence := map[string]assertion.Table{}
	servers := map[string]bool{}
	for _, s := range d.Steps {
		if s.Interaction != nil {
			servers[s.Interaction.Server] = true
		}
	}
	preflight := true
	for _, id := range slices.Sorted(maps.Keys(servers)) {
		identity, ok := claimed.Preflight[id]
		if !ok {
			preflight = false
			break
		}
		e, err := x.openHTTP("preflight/"+id, identity)
		if err != nil {
			return r, nil, err
		}
		spec := e.spec
		capability, base, _ := x.plan.ServerCapability(id)
		if spec.Base != base || spec.HTTP.HTTP.URL != base+"/metadata" || spec.HTTP.HTTP.Method != "GET" || spec.HTTP.HTTP.Operation != sendpolicy.FHIRMetadata || !x.boundTo(spec, id) || !bytes.Equal(spec.Capability, capability) {
			return r, nil, invalid
		}
		r.Preflight[id] = identity
		if x.admits(id, e.evidence) != nil {
			preflight = false
			break
		}
	}
	if len(r.Preflight) != len(claimed.Preflight) {
		return r, nil, invalid
	}
	if !preflight {
		if claimed.Stage != "preflight" || x.files["stimulus-intent.json"] != nil || len(claimed.Acquisitions) > 0 {
			return r, nil, invalid
		}
		r.State = claimed.State
		if r.State != "incomplete" && r.State != "cancelled" {
			return r, nil, invalid
		}
		return r, evidence, nil
	}
	r.Stage = "arming"
	// Every acquisition the manifest names must reopen and bind this run.
	acquisitions := map[string]map[string]bool{}
	for id, names := range claimed.Acquisitions {
		ds, typed, observation, ok := x.dataset(strings.TrimSuffix(id, "-barrier"))
		if !ok || len(names) > 4098 {
			return r, nil, invalid
		}
		acquisitions[id] = map[string]bool{}
		for _, name := range names {
			if !safeID(name) {
				return r, nil, invalid
			}
			identity, started, completed, err := x.openAcquisition(ctx, ds, id != ds.ID, typed, observation, name, claimed.Instance)
			if err != nil || !within(claimed, started, completed) {
				return r, nil, invalid
			}
			acquisitions[id][identity] = true
		}
		r.Acquisitions[id] = names
	}
	settledObservations := true
	for _, ds := range d.Datasets {
		r.Boundaries[ds.ID] = "not-armed"
	}
	for _, ds := range d.Datasets {
		_, _, observation, _ := x.dataset(ds.ID)
		if ds.Phase == "before" {
			path, ok := claimed.Observations[ds.ID]
			if !ok {
				settledObservations = false
				continue
			}
			if path != "observations/before-"+ds.ID {
				return r, nil, invalid
			}
			table, err := x.openTable(ctx, ds, observation, filepath.Join(x.dir, path), dataset.Binding{Run: claimed.Instance, Phase: "before", Namespace: ds.Namespace, Source: ds.Source})
			if err != nil || !table.Usable || claimed.Armed[ds.ID] != table.Identity || !acquisitions[ds.ID][table.Identity] {
				return r, nil, invalid
			}
			evidence[ds.ID] = table
			r.Armed[ds.ID], r.Observations[ds.ID], r.Boundaries[ds.ID] = table.Identity, path, "verified-baseline-snapshot"
			continue
		}
		identity, ok := claimed.Intervals[ds.ID]
		if !ok {
			settledObservations = false
			if claimed.Stage != "preflight" && claimed.Stage != "arming" {
				r.Boundaries[ds.ID] = "insufficient"
			}
			if armed, ok := claimed.Armed[ds.ID]; ok {
				r.Armed[ds.ID] = armed
			}
			continue
		}
		path := filepath.Join(x.dir, "intervals", ds.ID)
		var interval observeinterval.Result
		var err error
		schema := observeinterval.ResultSchema
		if observation != nil {
			schema = observeinterval.SamplesSchema
			interval, err = observeinterval.OpenSamples(ctx, path, func(ctx context.Context, dir string) (observeinterval.Sample, error) {
				return x.openSample(ctx, *observation, dir, dataset.Binding{})
			})
		} else {
			interval, err = observeinterval.Open(ctx, path)
		}
		if err != nil || interval.Identity != identity || interval.Binding != (dataset.Binding{Run: claimed.Instance, Phase: "after", Namespace: ds.Namespace, Source: ds.Source}) || !artifactdir.MatchesSubtree(x.files, "intervals/"+ds.ID, schema, identity) {
			return r, nil, invalid
		}
		definition, _ := observeinterval.Decode(x.plan.Files()["dependencies/"+ds.Completion.Policy.SHA256])
		if !bytes.Equal(canonicalFlow(interval.Definition), canonicalFlow(definition)) {
			return r, nil, invalid
		}
		r.Intervals[ds.ID], r.Boundaries[ds.ID] = identity, interval.Boundary
		if len(interval.Records) > 0 && interval.Records[0].Identity != "" {
			r.Armed[ds.ID] = interval.Records[0].Identity
		}
		for _, record := range interval.Records {
			if record.Snapshot != "" && !acquisitions[ds.ID][record.Identity] || record.Barrier != nil && !acquisitions[ds.ID+"-barrier"][record.Barrier.Evidence] || !within(claimed, record.RecordedAt, record.RecordedAt) {
				return r, nil, invalid
			}
			switch record.Kind {
			case "baseline", "stimulus-started":
				if !claimed.StimulusAt.IsZero() && record.RecordedAt.After(claimed.StimulusAt) {
					return r, nil, invalid
				}
			case "stimulus-finished":
				if record.RecordedAt.Before(claimed.SentAt) {
					return r, nil, invalid
				}
			}
		}
		if !interval.Sufficient() {
			settledObservations = false
		}
		if interval.FinalSnapshot != "" {
			final := "intervals/" + ds.ID + "/" + interval.FinalSnapshot
			table, err := x.openTable(ctx, ds, observation, filepath.Join(x.dir, final), dataset.Binding{})
			if err != nil || claimed.Observations[ds.ID] != final {
				return r, nil, invalid
			}
			r.Observations[ds.ID] = final
			evidence[ds.ID] = table
			if interval.Sufficient() {
				started, _ := x.acquisitionTime(ctx, ds, observation, filepath.Join(x.dir, final))
				if started.Before(claimed.SentAt) {
					return r, nil, invalid
				}
			}
		}
	}
	if len(r.Observations) != len(claimed.Observations) || len(r.Intervals) != len(claimed.Intervals) {
		return r, nil, invalid
	}
	if _, sent := x.files["stimulus-intent.json"]; !sent {
		// Arming stopped: no effect may be claimed.
		if claimed.Stage != "arming" || claimed.StimulusAt != (time.Time{}) || claimed.Transport != "" || x.files["stimulus-finished.json"] != nil || len(artifactdir.Subtree(x.files, "steps")) != 0 {
			return r, nil, invalid
		}
		r.Stage, r.State = claimed.Stage, claimed.State
		if r.State != "incomplete" && r.State != "cancelled" {
			return r, nil, invalid
		}
		return r, evidence, nil
	}
	var stimulusAt, sentAt time.Time
	if json.Unmarshal(x.files["stimulus-intent.json"], &stimulusAt) != nil || !stimulusAt.Equal(claimed.StimulusAt) || json.Unmarshal(x.files["stimulus-finished.json"], &sentAt) != nil || !sentAt.Equal(claimed.SentAt) || !within(claimed, stimulusAt, sentAt) {
		return r, nil, invalid
	}
	r.Stage = "stimulus"
	settled := true
	if len(r.Steps) == 0 {
		transport, err := connectedtransport.OpenEvidence(filepath.Join(x.dir, "transport"))
		receipt := transport.Receipt
		if err != nil || receipt.Schema != connectedtransport.ReceiptSchemaV2 || !artifactdir.MatchesSubtree(x.files, "transport", receipt.Schema, transport.Identity) || !artifactdir.MatchesSubtree(x.files, "transport/plan", connectedtest.PhasePlanSchemaV2, artifactdir.Identity(connectedtest.PhasePlanSchemaV2, x.plan.Files())) || receipt.Binding.Plan != x.plan.Identity() || receipt.Instance != claimed.Instance || receipt.RunIdentity != claimed.Transport {
			return r, nil, invalid
		}
		run, err := replay.Open(filepath.Join(x.dir, "transport", "run"))
		if err != nil || run.Identity != receipt.RunIdentity || !within(claimed, run.Manifest.StartedAt, run.Manifest.CompletedAt) || run.Manifest.StartedAt.Before(stimulusAt) {
			return r, nil, invalid
		}
		r.Transport = receipt.RunIdentity
		if receipt.State == "uncertain" {
			r.State = "uncertain"
		}
		settled = receipt.State == "settled"
	} else {
		bound := maps.Clone(x.inputs)
		for i, record := range claimed.Steps {
			if record.Step != r.Steps[i].Step {
				return r, nil, invalid
			}
			if record.State == "not-attempted" || record.State == "blocked" || record.State == "refused" && record.Result == "" {
				if len(artifactdir.Subtree(x.files, "steps/"+record.Step)) != 0 || record.Outcome != "" || record.Result != "" {
					return r, nil, invalid
				}
				if record.State != "not-attempted" {
					if _, err := x.plan.FHIRCall(record.Step, bound); (err == nil) == (record.State == "blocked") {
						return r, nil, invalid
					}
					settled = false
				}
				r.Steps[i] = record
				if record.State != "not-attempted" {
					for _, rest := range claimed.Steps[i+1:] {
						if rest.State != "not-attempted" {
							return r, nil, invalid
						}
					}
					break
				}
				continue
			}
			call, err := x.plan.FHIRCall(record.Step, bound)
			if err != nil {
				return r, nil, invalid
			}
			e, err := x.openHTTP("steps/"+record.Step, record.Result)
			if err != nil {
				return r, nil, err
			}
			spec := e.spec
			h := spec.HTTP.HTTP
			if spec.Base != call.Base || h.Method != call.Method || h.URL != call.URL || !bytes.Equal(h.Body, call.Body) || h.ContentType != call.ContentType || spec.HTTP.Headers != call.Headers || h.Operation != stepOperation(call.Method) || spec.Budget != call.Budget || spec.Retry != call.Retry || !x.boundTo(spec, call.Server) || h.Source != networkaction.Digest([]byte("step-"+record.Step)) {
				return r, nil, invalid
			}
			for _, a := range e.evidence.Result().Attempts {
				if !within(claimed, a.Started, a.Completed) || a.Started.Before(stimulusAt) || a.Completed.After(sentAt) {
					return r, nil, invalid
				}
			}
			derived, values := classifyStep(record.Step, e.evidence, call)
			r.Steps[i] = derived
			for k, v := range values {
				bound[k] = v
				r.Bound[k] = v
			}
			if derived.State == "uncertain" {
				r.State = "uncertain"
			}
			if derived.State != "complete" || len(values) != len(call.Bind) {
				settled = false
				for _, rest := range claimed.Steps[i+1:] {
					if rest.State != "not-attempted" {
						return r, nil, invalid
					}
				}
				break
			}
		}
	}
	r.Stage = "observed"
	for _, c := range d.Validations {
		value, ok := claimed.Validations[c.ID]
		if !ok || !networkaction.ValidDigest(value) && !slices.Contains([]string{"capability-not-configured", "response-unavailable", "validation-unavailable"}, value) {
			return r, nil, invalid
		}
		if networkaction.ValidDigest(value) {
			retained, err := fhirvalidator.Open(ctx, filepath.Join(x.dir, "validations", c.ID))
			step := slices.IndexFunc(r.Steps, func(s FHIRStepRecord) bool { return s.Step == c.Step })
			if err != nil || retained.Identity() != value || step < 0 || !artifactdir.MatchesSubtree(x.files, "validations/"+c.ID, fhirvalidator.ResultSchema, value) {
				return r, nil, invalid
			}
			e, err := x.openHTTP("steps/"+c.Step, r.Steps[step].Result)
			if err != nil {
				return r, nil, invalid
			}
			result := e.evidence.Result()
			input, _ := e.evidence.ResponseBytes(len(result.Attempts) - 1)
			if retained.Result().InputSHA256 != networkaction.Digest(input) {
				return r, nil, invalid
			}
		}
		r.Validations[c.ID] = value
	}
	if len(r.Validations) != len(claimed.Validations) {
		return r, nil, invalid
	}
	if r.State != "uncertain" {
		r.State = "incomplete"
		if settled && settledObservations && claimed.State != "cancelled" {
			r.State = "complete"
		}
		if claimed.State == "cancelled" {
			r.State = "cancelled"
		}
	}
	if claimed.Stage == "evaluated" {
		r.Stage = "evaluated"
		r.Evaluation = claimed.Evaluation
	} else if claimed.Stage != "observed" {
		return r, nil, invalid
	}
	return r, evidence, nil
}

type openedHTTP struct {
	evidence *fhirrest.Evidence
	spec     fhirrest.Spec
}

func (x fhirReader) openHTTP(prefix, identity string) (openedHTTP, error) {
	e, err := fhirrest.OpenEvidence(context.Background(), filepath.Join(x.dir, filepath.FromSlash(prefix)))
	if err != nil || e.Identity() != identity || !artifactdir.MatchesSubtree(x.files, prefix, fhirrest.ResultSchema, identity) {
		return openedHTTP{}, invalid
	}
	var spec fhirrest.Spec
	if json.Unmarshal(x.files[prefix+"/plan.json"], &spec) != nil {
		return openedHTTP{}, invalid
	}
	return openedHTTP{evidence: e, spec: spec}, nil
}

// boundTo checks that a retained request was bound to this lifecycle's plan,
// environment and endpoint; transport security and credentials were checked
// at the time.
func (x fhirReader) boundTo(spec fhirrest.Spec, endpoint string) bool {
	h, env := spec.HTTP.HTTP, x.plan.Document().Environment
	return h.Plan == x.flow.Identity() && h.Project == env.Project && h.Environment == env.ID && h.Revision == env.Revision && h.Endpoint == endpoint && h.Classification == "nonproduction"
}

// admits checks a server's current metadata against every request this
// phase sends it, before any collector is armed or effect sent.
func (x fhirReader) admits(server string, e *fhirrest.Evidence) error {
	result := e.Result()
	if result.State != "succeeded" || len(result.Attempts) < 2 {
		return invalid
	}
	current, ok := e.ResponseBytes(len(result.Attempts) - 1)
	capability, _, _ := x.plan.ServerCapability(server)
	if !ok {
		return invalid
	}
	for _, s := range x.plan.Document().Test.Steps {
		if s.Interaction == nil || s.Interaction.Server != server {
			continue
		}
		call, err := x.plan.FHIRCallShape(s.ID)
		if err != nil || fhirrest.Unchanged(call.Base, capability, current, call.Interaction()) != nil {
			return invalid
		}
	}
	return nil
}

// dataset resolves a phase dataset and, for a FHIR dataset, its observation.
func (x fhirReader) dataset(id string) (connectedtest.Dataset, bool, *fhirobserve.Observation, bool) {
	for _, ds := range x.plan.Document().Test.Datasets {
		if ds.ID != id {
			continue
		}
		if ds.Kind == "fhir-resources" {
			o, _, _, err := x.plan.ObservationURL(id)
			return ds, false, &o, err == nil
		}
		return ds, true, nil, true
	}
	return connectedtest.Dataset{}, false, nil, false
}

func (x fhirReader) openSample(ctx context.Context, o fhirobserve.Observation, dir string, binding dataset.Binding) (*fhirobserve.Sample, error) {
	_, base, address, err := x.plan.ObservationURL(x.observationDataset(o))
	if err != nil {
		return nil, err
	}
	return fhirobserve.Open(ctx, dir, o, fhirobserve.Expect{Base: base, URL: address, Binding: binding})
}
func (x fhirReader) observationDataset(o fhirobserve.Observation) string {
	for _, ds := range x.plan.Document().Test.Datasets {
		if ds.Kind == "fhir-resources" && ds.Source == o.Identity() {
			return ds.ID
		}
	}
	return ""
}

// openAcquisition reopens one named acquisition under its own reader.
func (x fhirReader) openAcquisition(ctx context.Context, ds connectedtest.Dataset, barrier, typed bool, o *fhirobserve.Observation, name, instance string) (string, time.Time, time.Time, error) {
	dir := filepath.Join(x.dir, "observations", name)
	if o != nil && !barrier {
		sample, err := x.openSample(ctx, *o, dir, dataset.Binding{})
		if err != nil || sample.Binding().Run != instance || !artifactdir.MatchesSubtree(x.files, "observations/"+name, fhirobserve.SampleSchema, sample.Identity()) {
			return "", time.Time{}, time.Time{}, invalid
		}
		// The sample's own retained request was bound to this dataset's scope.
		var spec fhirrest.Spec
		if json.Unmarshal(x.files["observations/"+name+"/http/plan.json"], &spec) != nil || !x.boundTo(spec, ds.ID) || spec.HTTP.HTTP.Source != o.Identity() {
			return "", time.Time{}, time.Time{}, invalid
		}
		return sample.Identity(), sample.Started(), sample.Completed(), nil
	}
	snapshot, err := observesource.OpenDataset(ctx, dir)
	if err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	doc := snapshot.Document()
	source := ds.Source
	id := ds.ID
	if barrier {
		definition, err := observeinterval.Decode(x.plan.Files()["dependencies/"+ds.Completion.Policy.SHA256])
		if err != nil || definition.Barrier == nil {
			return "", time.Time{}, time.Time{}, invalid
		}
		source, id = definition.Barrier.Source, ds.ID+"-barrier"
	}
	initial := artifactdir.Subtree(x.files, "observations/"+name)
	if doc.Binding.Run != instance || doc.Binding.Source != source || verifyAcquisitionAuthority(x.plan, id, source, initial, snapshot) != nil {
		return "", time.Time{}, time.Time{}, invalid
	}
	return snapshot.Identity(), doc.Acquisition.StartedAt, doc.Acquisition.CompletedAt, nil
}

// openTable reopens a retained final observation as the evaluator's table.
func (x fhirReader) openTable(ctx context.Context, ds connectedtest.Dataset, o *fhirobserve.Observation, dir string, binding dataset.Binding) (assertion.Table, error) {
	if o != nil {
		sample, err := x.openSample(ctx, *o, dir, binding)
		if err != nil {
			return assertion.Table{}, err
		}
		return sample.Table(), nil
	}
	var snapshot *dataset.Snapshot
	var err error
	if strings.Contains(filepath.ToSlash(dir), "/intervals/") {
		snapshot, err = dataset.Open(ctx, dir)
	} else {
		snapshot, err = observesource.OpenDataset(ctx, dir)
	}
	if err != nil {
		return assertion.Table{}, err
	}
	if binding != (dataset.Binding{}) && snapshot.Document().Binding != binding {
		return assertion.Table{}, invalid
	}
	return assertion.SnapshotTable(snapshot), nil
}
func (x fhirReader) acquisitionTime(ctx context.Context, ds connectedtest.Dataset, o *fhirobserve.Observation, dir string) (time.Time, error) {
	if o != nil {
		sample, err := x.openSample(ctx, *o, dir, dataset.Binding{})
		if err != nil {
			return time.Time{}, err
		}
		return sample.Started(), nil
	}
	snapshot, err := dataset.Open(ctx, dir)
	if err != nil {
		return time.Time{}, err
	}
	return snapshot.Document().Acquisition.StartedAt, nil
}

// interruptedFHIRPhase is the inspection view of an unsealed v5 phase: any
// retained request intent without a sealed outcome stays uncertain.
func interruptedFHIRPhase(plan *connectedtest.FlowPlan, phase connectedtest.FlowPhase, files map[string][]byte) (FlowPhaseResult, error) {
	r := unexecutedPhase(plan, phase, "not-attempted")
	r.State = "incomplete"
	for i, id := range phase.Steps {
		prefix := "steps/" + id + "/"
		for name := range files {
			if strings.HasPrefix(name, prefix+"intent-") {
				r.Steps[i].Outcome, r.Steps[i].Uncertain = "unknown", true
				r.State = "uncertain"
			}
		}
	}
	for name, raw := range files {
		if !strings.HasPrefix(name, "transport/intents/") {
			continue
		}
		var intent struct {
			Step       string `json:"step"`
			Occurrence string `json:"occurrence"`
			State      string `json:"state"`
		}
		if json.Unmarshal(raw, &intent, json.RejectUnknownMembers(true)) != nil || intent.State != "uncertain-until-settled" {
			return r, invalid
		}
		found := false
		for i, id := range phase.Steps {
			if intent.Step == id {
				found = true
				r.Steps[i].Outcome, r.Steps[i].Uncertain = "unknown", true
				r.State = "uncertain"
			}
		}
		if !found {
			return r, invalid
		}
	}
	return r, nil
}
