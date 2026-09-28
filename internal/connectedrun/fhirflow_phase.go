package connectedrun

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
)

// FHIRStepRecord is the retained outcome of one reviewed FHIR request. State
// is not-attempted, blocked (a needed response value is absent), refused (no
// request effect), complete (a classified response) or uncertain.
type FHIRStepRecord struct {
	Step    string `json:"step"`
	State   string `json:"state"`
	Outcome string `json:"outcome,omitzero"`
	Result  string `json:"result,omitzero"`
}

// FHIRPhaseRun is the manifest of one v5 phase. Inputs are response values the
// phase used from earlier phases; Bound are values its own responses supplied.
type FHIRPhaseRun struct {
	Schema       string              `json:"schema"`
	Plan         string              `json:"plan"`
	Instance     string              `json:"instance"`
	State        string              `json:"state"`
	Stage        string              `json:"stage"`
	StartedAt    time.Time           `json:"started_at"`
	StimulusAt   time.Time           `json:"stimulus_at"`
	SentAt       time.Time           `json:"sent_at"`
	CompletedAt  time.Time           `json:"completed_at"`
	Inputs       map[string]string   `json:"inputs"`
	Bound        map[string]string   `json:"bound"`
	Preflight    map[string]string   `json:"preflight"`
	Armed        map[string]string   `json:"armed"`
	Observations map[string]string   `json:"observations"`
	Intervals    map[string]string   `json:"intervals"`
	Boundaries   map[string]string   `json:"boundaries"`
	Acquisitions map[string][]string `json:"acquisitions"`
	Transport    string              `json:"transport,omitzero"`
	Steps        []FHIRStepRecord    `json:"steps"`
	Validations  map[string]string   `json:"validations"`
	Evaluation   string              `json:"evaluation,omitzero"`
}

// FHIREvaluation retains response, validation and downstream typed outcomes
// separately. A response or conformant resource is never downstream success.
type FHIREvaluation struct {
	Schema      string                   `json:"schema"`
	Typed       *assertion.DatasetReport `json:"typed,omitzero"`
	Responses   []FlowCheck              `json:"responses"`
	Validations []FlowCheck              `json:"validations"`
	Wire        *assertion.Report        `json:"wire,omitzero"`
}

const FHIREvaluationSchema = "readmit-connected-fhir-evaluation/v1"

var fhirPhaseFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected FHIR phase", AllowedDirectories: []string{"observations", "intervals", "steps", "preflight", "validations"}, Nested: []string{"plan", "transport", "observations", "intervals", "steps", "preflight", "validations"}, RequiredFiles: []string{"started.json"}, AllowFile: func(n string) bool {
	return n == "started.json" || n == "stimulus-intent.json" || n == "stimulus-finished.json" || n == "evaluation.json" || n == "manifest.json" || n == "identity.sha256"
}, MaxFiles: 200000, MaxFileBytes: 64 << 20, MaxBytes: 1 << 30}, Seal: artifactdir.DirectoryHash(PhaseSchemaV2)}

type fhirInterval struct {
	id       string
	typed    *sourcePlan
	fhir     *fhirSource
	session  *observeinterval.Session
	barrier  *dataset.Snapshot
	sequence int
	result   observeinterval.Result
	err      error
}

// executeFHIRPhase runs one v5 phase under the flow's live authority. inputs
// are response values bound by earlier phases, never values read from a file.
func executeFHIRPhase(ctx context.Context, p *PreparedFlow, phase connectedtest.FlowPhase, inputs map[string]string, output string) (FHIRPhaseRun, error) {
	fp := p.fhir.phases[phase.ID]
	instance := p.instance
	w, err := artifactdir.Create(output, fhirPhaseFamily, artifactdir.Durable)
	if err != nil {
		return FHIRPhaseRun{}, err
	}
	defer w.Close()
	used := map[string]string{}
	for _, s := range fp.plan.Document().Test.Steps {
		for _, name := range connectedtest.ResponseVariables(s, fp.plan.Document().Test.Variables, fp.plan.Files()["inputs/"+s.ID+".fhir"]) {
			if v, ok := inputs[name]; ok {
				used[name] = v
			}
		}
	}
	r := initialFHIRPhase(fp, instance, time.Now().UTC(), used)
	if put(w, "started.json", r) != nil || w.Mkdir("observations") != nil || w.Mkdir("intervals") != nil || w.Mkdir("steps") != nil || w.Mkdir("preflight") != nil || w.Mkdir("validations") != nil || fp.plan.Write(ctx, filepath.Join(w.Path(), "plan")) != nil || w.Sync() != nil {
		return FHIRPhaseRun{}, invalid
	}
	intervals := []*fhirInterval{}
	var stops []func()
	finish := func() (FHIRPhaseRun, error) {
		for _, state := range intervals {
			if r.Stage == "arming" || r.Stage == "preflight" {
				if completed, e := state.session.Finish(context.WithoutCancel(ctx)); e == nil {
					state.result = completed
					r.Intervals[state.id] = completed.Identity
					r.Boundaries[state.id] = completed.Boundary
					if completed.FinalSnapshot != "" {
						r.Observations[state.id] = "intervals/" + state.id + "/" + completed.FinalSnapshot
					}
				}
			}
		}
		if ctx.Err() != nil && r.State != "uncertain" {
			r.State = "cancelled"
		}
		r.CompletedAt = time.Now().UTC()
		if put(w, "manifest.json", r) != nil {
			return FHIRPhaseRun{}, invalid
		}
		if _, err := w.Seal(nil); err != nil {
			return FHIRPhaseRun{}, err
		}
		return r, nil
	}
	reader := fhirReader{flow: p.plan, phase: phase, plan: fp.plan, dir: w.Path()}
	// Each consumer (a grant under one role) has its own credential session,
	// so concurrent collectors never evict each other's token.
	var sessions sync.Mutex
	providers := map[string]networkaction.RuntimeProvider{}
	provider := func(server *fhirServer, role string, grant Grant) networkaction.RuntimeProvider {
		sessions.Lock()
		defer sessions.Unlock()
		key := server.id + ":" + role + ":" + grant.Path
		if existing, ok := providers[key]; ok {
			return existing
		}
		auth := server.observation
		if role == "action" {
			auth = server.action
		}
		created, stop := auth.provider(p)
		providers[key] = created
		stops = append(stops, stop)
		return created
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	// Every server this phase writes to must still admit every request exactly
	// as reviewed before anything is armed or sent.
	for _, id := range slices.Sorted(maps.Keys(fp.preflight)) {
		plan := fp.preflight[id]
		dir := filepath.Join(w.Path(), "preflight", id)
		if _, err := plan.Execute(ctx, flowAuthority{flow: p, authority: fp.grants["preflight:"+id].authority()}, provider(p.fhir.servers[id], "observation", fp.grants["preflight:"+id]), dir, nil); err != nil {
			return finish()
		}
		evidence, err := fhirrest.OpenEvidence(context.WithoutCancel(ctx), dir)
		if err != nil {
			return finish()
		}
		r.Preflight[id] = evidence.Identity()
		if reader.admits(id, evidence) != nil {
			return finish()
		}
	}
	r.Stage = "arming"
	var acquired sync.Mutex
	acquireTyped := func(ctx context.Context, s sourcePlan, phase, name string) (*dataset.Snapshot, error) {
		request := observesource.DatasetRequest{Source: s.source, Projection: s.projection, Binding: dataset.Binding{Run: instance, Phase: phase, Namespace: s.definition.Namespace, Source: s.definition.Source}, Output: filepath.Join(w.Path(), "observations", name), Network: s.http, DatabaseNetwork: s.database, Authorize: func(ctx context.Context) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return p.unchanged()
		}}
		if s.http != nil || s.database != nil {
			request.NetworkAuthority = flowAuthority{flow: p, authority: s.grant.authority()}
		}
		snapshot, err := observesource.CollectDataset(ctx, request)
		if err == nil {
			acquired.Lock()
			r.Acquisitions[s.definition.ID] = append(r.Acquisitions[s.definition.ID], name)
			acquired.Unlock()
		}
		return snapshot, err
	}
	acquireFHIR := func(ctx context.Context, s *fhirSource, phase, name string) (*fhirobserve.Sample, error) {
		expect := fhirobserve.Expect{Base: s.base, URL: s.url, Binding: dataset.Binding{Run: instance, Phase: phase, Namespace: s.definition.Namespace, Source: s.definition.Source}}
		sample, err := fhirobserve.Acquire(ctx, s.observation, expect, s.plan, flowAuthority{flow: p, authority: s.grant.authority()}, provider(s.server, "observation", s.grant), nil, filepath.Join(w.Path(), "observations", name))
		if sample != nil {
			acquired.Lock()
			r.Acquisitions[s.definition.ID] = append(r.Acquisitions[s.definition.ID], name)
			acquired.Unlock()
		}
		return sample, err
	}
	evidence := map[string]assertion.Table{}
	for _, id := range datasetOrder(fp) {
		r.Boundaries[id] = "not-armed"
	}
	for _, id := range datasetOrder(fp) {
		typed, fhir := fp.source(id)
		d := fp.definition(id)
		if d.Phase == "before" {
			if typed != nil {
				snapshot, err := acquireTyped(ctx, *typed, "before", "before-"+id)
				if err != nil || !snapshot.Usable() {
					return finish()
				}
				evidence[id] = assertion.SnapshotTable(snapshot)
				r.Armed[id] = snapshot.Identity()
			} else {
				sample, err := acquireFHIR(ctx, fhir, "before", "before-"+id)
				if err != nil || !sample.Usable() {
					return finish()
				}
				evidence[id] = sample.Table()
				r.Armed[id] = sample.Identity()
			}
			r.Boundaries[id] = "verified-baseline-snapshot"
			r.Observations[id] = "observations/before-" + id
			continue
		}
		state := &fhirInterval{id: id, typed: typed, fhir: fhir}
		binding := dataset.Binding{Run: instance, Phase: "after", Namespace: d.Namespace, Source: d.Source}
		path := filepath.Join(w.Path(), "intervals", id)
		if typed != nil {
			state.session, err = observeinterval.Arm(ctx, *typed.interval, binding, path, observeinterval.SystemClock())
		} else {
			observation := fhir.observation
			state.session, err = observeinterval.ArmSamples(ctx, fhir.interval, binding, path, observeinterval.SystemClock(), fhirobserve.SampleSchema, func(ctx context.Context, dir string) (observeinterval.Sample, error) {
				return reader.openSample(ctx, observation, dir, dataset.Binding{})
			})
		}
		if err != nil {
			return finish()
		}
		defer state.session.Close()
		intervals = append(intervals, state)
		// The baseline is journaled even when it failed, so the retained
		// interval states why nothing was sent.
		baseline, err := state.observe(ctx, instance, "armed-"+id, acquireTyped, acquireFHIR, "before")
		appendErr := state.session.Append(ctx, baseline)
		if baseline.Snapshot != nil {
			r.Armed[id] = baseline.Snapshot.Identity()
		}
		if baseline.Sample != nil {
			r.Armed[id] = baseline.Sample.Identity()
		}
		if err != nil || appendErr != nil || baseline.Status != "healthy" || state.session.StimulusStarted() != nil {
			return finish()
		}
	}
	observeCtx, stopObserving := context.WithCancel(ctx)
	defer stopObserving()
	stimulusDone := make(chan struct{})
	var group sync.WaitGroup
	for _, state := range intervals {
		group.Add(1)
		go func() {
			defer group.Done()
			state.err = state.session.Observe(observeCtx, func(ctx context.Context) (observeinterval.Observation, error) {
				state.sequence++
				return state.observe(ctx, instance, fmt.Sprintf("%s-%04d", state.id, state.sequence), acquireTyped, acquireFHIR, "after")
			}, stimulusDone)
			if state.err != nil {
				stopObserving()
			}
			result, finishErr := state.session.Finish(observeCtx)
			state.result = result
			if finishErr != nil {
				state.err = finishErr
			}
			if state.err != nil || !state.result.Sufficient() {
				stopObserving()
			}
		}()
	}
	r.Stage = "stimulus"
	r.StimulusAt = time.Now().UTC()
	if put(w, "stimulus-intent.json", r.StimulusAt) != nil || w.Sync() != nil {
		stopObserving()
		group.Wait()
		return FHIRPhaseRun{}, invalid
	}
	settled := true
	if fp.transport != nil {
		receipt, sendErr := connectedtransport.Execute(observeCtx, fp.transport, flowAuthority{flow: p, authority: fp.send.authority()}, instance, filepath.Join(w.Path(), "transport"), nil)
		r.Transport = receipt.RunIdentity
		if sendErr != nil || receipt.State == "uncertain" {
			r.State = "uncertain"
		}
		settled = sendErr == nil && receipt.State == "settled"
	} else {
		bound := maps.Clone(inputs)
		for i, step := range fp.steps {
			record, values := runFHIRStep(observeCtx, p, fp, step, bound, provider(step.server, roleName(step.method), step.grant), filepath.Join(w.Path(), "steps", step.id))
			r.Steps[i] = record
			for k, v := range values {
				bound[k] = v
				r.Bound[k] = v
			}
			if record.State == "uncertain" {
				r.State = "uncertain"
			}
			if record.State != "complete" || len(values) != len(stepBinds(fp, step.id)) {
				settled = false
				break
			}
		}
	}
	r.SentAt = time.Now().UTC()
	close(stimulusDone)
	if put(w, "stimulus-finished.json", r.SentAt) != nil || w.Sync() != nil {
		stopObserving()
		group.Wait()
		return FHIRPhaseRun{}, invalid
	}
	if !settled {
		stopObserving()
	}
	group.Wait()
	r.Stage = "observed"
	complete := settled
	for _, state := range intervals {
		r.Boundaries[state.id] = "insufficient"
		if state.result.Identity != "" {
			r.Boundaries[state.id] = state.result.Boundary
			r.Intervals[state.id] = state.result.Identity
		}
		if state.err != nil || !state.result.Sufficient() {
			complete = false
		}
		if state.result.FinalSnapshot != "" {
			final := "intervals/" + state.id + "/" + state.result.FinalSnapshot
			r.Observations[state.id] = final
			// Evaluate exactly the retained final sample the reader will reopen.
			ds, _, observation, _ := reader.dataset(state.id)
			table, err := reader.openTable(context.WithoutCancel(ctx), ds, observation, filepath.Join(w.Path(), final), dataset.Binding{})
			if err != nil {
				complete = false
				continue
			}
			evidence[state.id] = table
		}
	}
	// Response validation runs on retained response bytes only, whatever the
	// downstream observations showed; it never contacts the server again.
	for _, check := range fp.plan.Document().Test.Validations {
		r.Validations[check.ID] = runValidation(context.WithoutCancel(ctx), p.fhir, fp, check, r.Steps, w.Path())
	}
	if r.State != "uncertain" && ctx.Err() == nil {
		r.State = "incomplete"
		if complete {
			r.State = "complete"
		}
	}
	evaluation, err := reader.evaluate(context.WithoutCancel(ctx), r, evidence)
	if err != nil {
		return FHIRPhaseRun{}, err
	}
	if put(w, "evaluation.json", evaluation) != nil {
		return FHIRPhaseRun{}, invalid
	}
	r.Evaluation = flowDigest(evaluation)
	r.Stage = "evaluated"
	return finish()
}

func initialFHIRPhase(fp *fhirPhase, instance string, at time.Time, inputs map[string]string) FHIRPhaseRun {
	r := FHIRPhaseRun{Schema: PhaseSchemaV2, Plan: fp.plan.Identity(), Instance: instance, State: "incomplete", Stage: "preflight", StartedAt: at, Inputs: inputs, Bound: map[string]string{}, Preflight: map[string]string{}, Armed: map[string]string{}, Observations: map[string]string{}, Intervals: map[string]string{}, Boundaries: map[string]string{}, Acquisitions: map[string][]string{}, Steps: []FHIRStepRecord{}, Validations: map[string]string{}}
	for _, s := range fp.steps {
		r.Steps = append(r.Steps, FHIRStepRecord{Step: s.id, State: "not-attempted"})
	}
	return r
}

// observe acquires one sample for an interval: a typed-row snapshot or a FHIR
// search sample, plus its independently configured barrier when declared.
func (s *fhirInterval) observe(ctx context.Context, instance, name string, typed func(context.Context, sourcePlan, string, string) (*dataset.Snapshot, error), fhir func(context.Context, *fhirSource, string, string) (*fhirobserve.Sample, error), phase string) (observeinterval.Observation, error) {
	var barrier *sourcePlan
	var d connectedtest.Dataset
	if s.typed != nil {
		barrier, d = s.typed.barrier, s.typed.definition
	} else {
		barrier, d = s.fhir.barrier, s.fhir.definition
	}
	o := observeinterval.Observation{Binding: dataset.Binding{Run: instance, Phase: phase, Namespace: d.Namespace, Source: d.Source}, Status: "healthy"}
	if barrier != nil {
		snapshot, err := typed(ctx, *barrier, "after", name+"-barrier")
		if err != nil {
			return o, err
		}
		s.barrier = snapshot
	}
	o.BarrierSnapshot = s.barrier
	if s.typed != nil {
		snapshot, err := typed(ctx, *s.typed, phase, name)
		if err != nil || snapshot == nil {
			o.Status = "collector-failed"
			return o, err
		}
		o.Snapshot = snapshot
		o.Binding = snapshot.Document().Binding
		if !snapshot.Usable() {
			o.Status = "unusable-snapshot"
		}
		if facts := snapshot.Document().Acquisition.Facts; facts != nil {
			o.SourceAt = facts.AsOf
		}
		return o, nil
	}
	sample, err := fhir(ctx, s.fhir, phase, name)
	if err != nil || sample == nil {
		o.Status = "collector-failed"
		return o, err
	}
	o.Sample = sample
	o.Binding = sample.Binding()
	if !sample.Usable() {
		o.Status = "unusable-snapshot"
	}
	return o, nil
}

func datasetOrder(fp *fhirPhase) []string {
	out := []string{}
	for _, d := range fp.plan.Document().Test.Datasets {
		out = append(out, d.ID)
	}
	return out
}
func (fp *fhirPhase) source(id string) (*sourcePlan, *fhirSource) {
	for i := range fp.sources {
		if fp.sources[i].definition.ID == id {
			return &fp.sources[i], nil
		}
	}
	for i := range fp.fhir {
		if fp.fhir[i].definition.ID == id {
			return nil, &fp.fhir[i]
		}
	}
	return nil, nil
}
func (fp *fhirPhase) definition(id string) connectedtest.Dataset {
	for _, d := range fp.plan.Document().Test.Datasets {
		if d.ID == id {
			return d
		}
	}
	return connectedtest.Dataset{}
}
func stepBinds(fp *fhirPhase, id string) []connectedtest.ResponseBinding {
	for _, s := range fp.plan.Document().Test.Steps {
		if s.ID == id && s.Interaction != nil {
			return s.Interaction.Bind
		}
	}
	return nil
}

// stepSpec is the exact reviewed transport declaration for a resolved call.
func stepSpec(lifecycle string, fp *fhirPhase, step fhirStep, call connectedtest.FHIRCall) fhirrest.Spec {
	spec := fhirSpec(lifecycle, fp.plan.Document().Environment, step.server, step.server.role(call.Method), "step-"+step.id, step.server.id, stepOperation(call.Method), call.Budget, call.Retry)
	spec.HTTP.HTTP.Method, spec.HTTP.HTTP.URL, spec.HTTP.HTTP.Body, spec.HTTP.HTTP.ContentType = call.Method, call.URL, call.Body, call.ContentType
	spec.HTTP.Headers = call.Headers
	return spec
}

// runFHIRStep sends one reviewed request once. A lost response is uncertain
// and is never repeated; bound values come only from the retained response.
func runFHIRStep(ctx context.Context, p *PreparedFlow, fp *fhirPhase, step fhirStep, bound map[string]string, provider networkaction.RuntimeProvider, output string) (FHIRStepRecord, map[string]string) {
	record := FHIRStepRecord{Step: step.id, State: "not-attempted"}
	if ctx.Err() != nil {
		return record, nil
	}
	call, err := fp.plan.FHIRCall(step.id, bound)
	if err != nil {
		// A needed response value was never bound; nothing is guessed or sent.
		record.State = "blocked"
		return record, nil
	}
	plan, err := fhirrest.Prepare(encodeSpec(stepSpec(p.fhir.lifecycle, fp, step, call)), p.fhir.policy)
	if err != nil {
		record.State = "refused"
		return record, nil
	}
	authority := templateAuthority{root: flowAuthority{flow: p, authority: step.grant.authority()}, template: step.template, derived: plan.Binding()}
	if _, err = plan.Execute(ctx, authority, provider, output, nil); err != nil {
		// Nothing was retained: authority or storage refused before any request.
		record.State = "refused"
		return record, nil
	}
	evidence, err := fhirrest.OpenEvidence(context.WithoutCancel(ctx), output)
	if err != nil {
		record.State = "uncertain"
		return record, nil
	}
	return classifyStep(step.id, evidence, call)
}

// searchMatches are the distinct matched resources of one retained page.
func searchMatches(base string, body []byte) (map[string]string, bool) {
	d, err := fhirr4.Decode(context.Background(), body, fhirr4.Context{Version: fhirr4.Version, Base: base, MediaType: "application/fhir+json"})
	if err != nil {
		return nil, false
	}
	bundle, err := d.HTTPBundle()
	if err != nil {
		return nil, false
	}
	out := map[string]string{}
	for n, entry := range bundle.Entries {
		if entry.SearchMode != "match" {
			continue
		}
		for _, resource := range d.Resources() {
			if resource.Pointer == fmt.Sprintf("/entry/%d/resource", n) && resource.LogicalID != "" {
				out[resource.Type+"/"+resource.LogicalID] = resource.VersionID
			}
		}
	}
	return out, true
}

// classifyStep is shared by execution and the offline reader.
func classifyStep(id string, evidence *fhirrest.Evidence, call connectedtest.FHIRCall) (FHIRStepRecord, map[string]string) {
	binds := call.Bind
	result := evidence.Result()
	record := FHIRStepRecord{Step: id, State: "complete", Outcome: result.State, Result: evidence.Identity()}
	for _, a := range result.Attempts {
		if a.Outcome.State == "delivery-uncertain" || a.Outcome.State == "response-withheld" {
			record.State = "uncertain"
		}
	}
	switch result.ExecutionState {
	case "delivery-uncertain", "response-withheld", "time-limit":
		record.State = "uncertain"
	case "refused", "capability-refused", "capability-changed":
		if record.State != "uncertain" {
			record.State = "refused"
		}
	}
	if record.State != "complete" || len(binds) == 0 {
		return record, nil
	}
	values := map[string]string{}
	var logical, version string
	if result.State != "succeeded" {
		return record, values
	}
	if call.Interaction().Kind(call.Base) == "search" {
		ids := map[string]string{}
		for i, a := range result.Attempts {
			if a.Phase != "interaction" || a.Outcome.State != "succeeded" {
				continue
			}
			body, _ := evidence.ResponseBytes(i)
			matches, ok := searchMatches(call.Base, body)
			if !ok {
				return record, values
			}
			maps.Copy(ids, matches)
		}
		// Exactly one distinct matched resource; zero or several is refused.
		if len(ids) != 1 || result.Search.Coverage != "complete" {
			return record, values
		}
		for identity, v := range ids {
			_, logical, _ = strings.Cut(identity, "/")
			version = v
		}
	} else {
		last := result.Attempts[len(result.Attempts)-1].Outcome
		logical, version = last.LogicalID, last.Version
	}
	for _, b := range binds {
		value := logical
		if b.From == "version-id" {
			value = version
		}
		if !connectedtest.ServerAssignedID(value) {
			return record, map[string]string{}
		}
		values[b.Variable] = value
	}
	return record, values
}

func runValidation(ctx context.Context, f *fhirFlow, fp *fhirPhase, check connectedtest.ValidationCheck, steps []FHIRStepRecord, root string) string {
	if f.validation == nil {
		return "capability-not-configured"
	}
	i := slices.IndexFunc(steps, func(s FHIRStepRecord) bool { return s.Step == check.Step })
	if i < 0 || steps[i].State != "complete" {
		return "response-unavailable"
	}
	evidence, err := fhirrest.OpenEvidence(ctx, filepath.Join(root, "steps", check.Step))
	if err != nil {
		return "response-unavailable"
	}
	result := evidence.Result()
	input, ok := evidence.ResponseBytes(len(result.Attempts) - 1)
	if !ok || len(input) == 0 {
		return "response-unavailable"
	}
	request := fhirvalidator.Request{Schema: fhirvalidator.RequestSchema, Capability: f.validation.Identity(), InputSHA256: networkaction.Digest(input), Profiles: check.Profiles, Requirements: check.Requirements, TimeoutMS: check.TimeoutMS, MaxOutputBytes: check.MaxOutputBytes}
	raw, _ := json.Marshal(request, json.Deterministic(true))
	plan, err := fhirvalidator.Prepare(raw, input, f.validation)
	if err != nil {
		return "validation-unavailable"
	}
	var engine *fhirvalidator.Engine
	if f.engine == "local" {
		if engine, err = fhirvalidator.LocalEngine(f.socket); err == nil {
			defer engine.Close()
		} else {
			engine = nil
		}
	}
	retained, err := plan.Execute(ctx, engine, filepath.Join(root, "validations", check.ID))
	if err != nil {
		return "validation-unavailable"
	}
	return retained.Identity()
}
