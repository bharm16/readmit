package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
)

// ConnectedRunReview is the exact individual lifecycle selected by normal Run.
// It contains only declared effects/boundaries and local input identities;
// preparation never contacts a target, reads a credential or grants authority.
type ConnectedDerivedInputReview struct {
	Step     string `json:"step"`
	Selector string `json:"selector"`
	Value    string `json:"value"`
}
type ConnectedRunCollectorReview struct {
	Phase      string `json:"phase"`
	Dataset    string `json:"dataset"`
	Kind       string `json:"kind"`
	Address    string `json:"address,omitzero"`
	Credential string `json:"credential,omitzero"`
	Generation string `json:"generation,omitzero"`
	HorizonMS  int64  `json:"horizon_ms"`
	Meaning    string `json:"meaning"`
}
type ConnectedRunReview struct {
	Collectors            []ConnectedRunCollectorReview `json:"collectors,omitzero"`
	DerivedInputs         []ConnectedDerivedInputReview `json:"derived_inputs,omitzero"`
	RuntimeMarkerRequired bool                          `json:"runtime_marker_required,omitzero"`
	Plan                  string                        `json:"plan"`
	Input                 string                        `json:"input"`
	Boundary              string                        `json:"boundary"`
	Instance              string                        `json:"instance"`
	Phases                []ConnectedRunPhaseReview     `json:"phases"`
	Effects               []ConnectedRunEffect          `json:"effects"`
	Endpoints             []ReviewDestination           `json:"endpoints"`
}
type ConnectedRunPhaseReview struct {
	ID           string   `json:"id"`
	Steps        []string `json:"steps"`
	Checks       int      `json:"checks"`
	Observations []string `json:"observations"`
}
type ConnectedRunEffect struct {
	Phase     string `json:"phase"`
	Resource  string `json:"resource"`
	Operation string `json:"operation"`
}
type connectedIndividualBinding struct {
	compiled        *connectedCompiled
	draft           ConnectedTestDraft
	instance, input string
	localInput      string
	runtimeMarker   string
	bindings        map[string]networkaction.Binding
}

func prepareConnectedIndividual(ctx context.Context, compiled *connectedCompiled, instance string) (*connectedrun.PreparedFlow, func(), error) {
	folder, err := os.MkdirTemp("", "readmit-connected-review-")
	if err != nil {
		return nil, func() {}, err
	}
	remove := func() { _ = os.RemoveAll(folder) }
	plan, config, err := compiled.write(ctx, folder, "")
	if err != nil {
		remove()
		return nil, func() {}, err
	}
	prepared, err := connectedrun.PrepareFlow(plan, config, instance)
	if err != nil {
		remove()
		return nil, func() {}, err
	}
	return prepared, remove, nil
}

func bindConnectedTestRun(a *App, ctx context.Context, request PrepareActionRequest, held bool, loaded *loadedCatalog, items []CatalogItem, record catalog.Item) (*boundAction, refusal) {
	_, revision, err := loaded.testVersionPath(record, request.Items[0].Revision)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	saved, err := loaded.connectedTestOf(record, revision)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	environment := ""
	if request.Destination != nil {
		environment = request.Destination.ID
	} else if saved.links != nil {
		environment = saved.links.Environment
	}
	marker := ""
	if needsConnectedRuntimeMarker(saved.draft) {
		if request.Run != nil {
			marker = request.Run.RuntimeMarker
		}
		if marker != "" {
			if _, _, err := a.connectedRuntimeMarker(ctx, request.Context, marker, held, loaded); err != nil {
				return nil, refusal{Failed, err.Error()}
			}
		}
	}
	compiled, problems := loaded.compileConnected(saved.draft, connectedLifecycleID(record.ID), revision, connectedEnvironments{environment: environment, server: saved.draft.Server, runtimeMarker: func() string {
		if marker != "" {
			return "run-" + marker
		}
		return ""
	}()})
	if len(problems) > 0 || compiled == nil {
		return nil, refusal{Failed, "the connected test could not be prepared: " + firstProblem(problems)}
	}
	selected, err := loaded.connectedEnvironment(environment)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	destination, declined := destinationFor(loaded.root, "", "job")
	if destination.Name == "" {
		return nil, declined
	}
	instance := "desktop-" + binding(loaded.document.Project.ID, destination.Name)[:24]
	if marker != "" {
		instance = "run-" + marker
	}
	prepared, remove, err := prepareConnectedIndividual(ctx, compiled, instance)
	if err != nil {
		return nil, refusal{Failed, "the exact connected test and selected configuration do not prepare; nothing was sent"}
	}
	defer remove()
	input, err := prepared.InputIdentity()
	if err != nil {
		return nil, refusal{Failed, "the connected inputs could not be identified"}
	}
	ref := ItemRef{Kind: TestItem, ID: record.ID, Revision: revision}
	count := len(saved.draft.Steps)
	review := &RunReview{Kind: TestRunKind, Name: record.Name, Version: revision, Test: &ref, Environment: &selected.ref, EnvironmentName: selected.name,
		Messages: []TestMessage{}, MessageCount: &count, Setup: []RunSetupStep{}, Resets: []ResetReviewAction{}, Jobs: []RunReviewJob{}, Targets: []RunReviewTarget{}, Environments: []SuiteEnvironmentRef{}}
	review.Lifecycle = &ConnectedRunReview{RuntimeMarkerRequired: needsConnectedRuntimeMarker(saved.draft) && marker == "", Plan: compiled.plan.Identity(), Input: input, Boundary: saved.draft.Boundary, Instance: instance, Phases: []ConnectedRunPhaseReview{}, Effects: []ConnectedRunEffect{}, Endpoints: []ReviewDestination{}}
	for _, step := range saved.draft.Steps {
		if step.V2 != nil && step.V2.RuntimeMarkerSelector != "" {
			value := "Awaiting fresh marker"
			if marker != "" {
				value = instance
			}
			review.Lifecycle.DerivedInputs = append(review.Lifecycle.DerivedInputs, ConnectedDerivedInputReview{Step: step.ID, Selector: step.V2.RuntimeMarkerSelector, Value: value})
		}
	}
	bindings := prepared.Bindings()
	addresses := map[string]string{}
	for _, phase := range saved.draft.Phases {
		for _, observation := range phase.Observations {
			if read, err := loaded.connectedObservation(observation.Observation.ID, observation.Observation.Revision); err == nil {
				view := ConnectedRunCollectorReview{Phase: phase.ID, Dataset: observation.Dataset, Kind: read.kind, HorizonMS: read.definition.HorizonMS, Meaning: "Observed samples over the declared horizon; this does not establish upstream freshness"}
				if read.typed != nil && read.typed.Database != nil {
					view.Kind = "database-query"
					view.Address = read.typed.Database.Address
					view.Credential = read.credentialName
					view.Generation = read.credentialGeneration
					addresses[observation.Dataset] = view.Address
					view.Meaning = "Actual typed driver rows from the approved view; no database wire evidence or business-commit freshness is inferred"
				}
				review.Lifecycle.Collectors = append(review.Lifecycle.Collectors, view)
			}
		}
	}
	addresses[connectedReceiver] = selected.members.target.Address
	for _, phase := range saved.draft.Phases {
		for _, observation := range phase.Observations {
			if read, err := loaded.connectedObservation(observation.Observation.ID, observation.Observation.Revision); err == nil && read.capture != nil {
				addresses[observation.Dataset] = read.capture.Address
			}
		}
	}
	serverID := saved.draft.Server
	if serverID == "" {
		serverID = selected.ref.ID
	}
	if server, err := loaded.connectedEnvironment(serverID); err == nil && server.members.fhir != nil {
		addresses[connectedServer] = server.members.fhir.Base
		addresses["token"] = server.members.fhir.TokenEndpoint
		for _, phase := range saved.draft.Phases {
			for _, observation := range phase.Observations {
				addresses[observation.Dataset] = server.members.fhir.Base
			}
		}
	}
	if registry, _, err := isolationRegistry(selected.members.isolation.RegistryFile); err == nil {
		for _, adapter := range registry.Adapters {
			if adapter.ID == selected.members.isolation.Adapter {
				addresses[adapter.Read.Endpoint] = adapter.URL
				addresses[adapter.Setup.Endpoint] = adapter.URL
				addresses[adapter.Cleanup.Endpoint] = adapter.URL
			}
		}
	}
	endpoints := map[string]bool{}
	for _, binding := range bindings {
		if endpoints[binding.Endpoint] {
			continue
		}
		endpoints[binding.Endpoint] = true
		address := addresses[binding.Endpoint]
		review.Lifecycle.Endpoints = append(review.Lifecycle.Endpoints, ReviewDestination{Name: binding.Endpoint, Address: address, Classification: "nonproduction"})
	}
	slices.SortFunc(review.Lifecycle.Endpoints, func(a, b ReviewDestination) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	if selected.members.fhir != nil {
		review.Address = selected.members.fhir.Base
	} else {
		review.Address = selected.members.target.Address
	}
	for _, phase := range saved.draft.Phases {
		view := ConnectedRunPhaseReview{ID: phase.ID, Steps: slices.Clone(phase.Steps), Checks: len(phase.Checks) + len(phase.Responses) + len(phase.Validations) + len(phase.Acks) + len(phase.Unsupported), Observations: []string{}}
		for _, observed := range phase.Observations {
			view.Observations = append(view.Observations, observed.Dataset)
		}
		review.Lifecycle.Phases = append(review.Lifecycle.Phases, view)
	}
	isolation := prepared.IsolationReviews()
	for _, role := range []string{"setup", "cleanup"} {
		for _, effect := range isolation[role].Effects {
			review.Lifecycle.Effects = append(review.Lifecycle.Effects, ConnectedRunEffect{Phase: role, Resource: effect.Alias, Operation: effect.Operation})
		}
	}
	for _, manual := range isolation["setup"].Manual {
		review.Setup = append(review.Setup, RunSetupStep{ID: manual.ID, Name: manual.ID, Instructions: manual.Instructions})
	}
	ready, reason, kind := a.runReady(ctx, held, selected.name, nil, destination, "")
	if review.Lifecycle.RuntimeMarkerRequired {
		ready, reason = false, "issue a fresh project runtime marker for the explicitly selected derived input"
	}
	review.Refusal = kind
	raw, _ := json.Marshal(bindings, json.Deterministic(true))
	localInput, err := compiled.localInputIdentity()
	if err != nil {
		return nil, refusal{Failed, "the exact local connected inputs cannot be identified"}
	}
	run := &runBinding{kind: TestRunKind, root: loaded.root, output: destination.Name, name: record.Name, specDigest: compiled.plan.Identity(), identity: input,
		lifecycle: &connectedIndividualBinding{compiled: compiled, draft: saved.draft, instance: instance, input: input, runtimeMarker: marker, bindings: bindings, localInput: localInput}}
	for _, manual := range review.Setup {
		run.setup = append(run.setup, manual.ID)
	}
	return &boundAction{action: RunTestAction, origin: request, run: run,
		binding: binding(string(RunTestAction), loaded.document.Project.ID, loaded.root, a.reviewer(), a.policyBinding(ctx, true, held), record.ID, revision, digestOf(saved.data), input, string(raw), destination.Name),
		review:  ActionReview{Items: items, Ready: ready, Refusal: reason, Run: review, Requirements: setupRequirement(review), Destination: ReviewDestination{Name: selected.name, Address: review.Address, Classification: "nonproduction", Output: destination.Name}}}, refusal{}
}

// connectedIndividualAuthority exists only while the final reviewed action
// runs. Every compiler-derived effect must recheck current review/guard and
// exact source/configuration bindings; no imported file can restore it.
type connectedIndividualAuthority struct {
	app              *App
	bound            *boundAction
	actor            networkaction.Actor
	reviewer, policy string
}

func (a connectedIndividualAuthority) Check(ctx context.Context, wanted networkaction.Binding) (networkaction.Actor, error) {
	deny := errors.New("the connected lifecycle's exact reviewed action is no longer authorized")
	if ctx.Err() != nil || a.bound.executionReview == nil || a.bound.run == nil || a.bound.run.lifecycle == nil {
		return networkaction.Actor{}, deny
	}
	lease := a.bound.executionReview
	store := &a.app.reviews
	store.mu.Lock()
	review := store.reviews[lease.token]
	valid := review != nil && !review.withdrawn && review.consumed == lease.intent && store.running == lease.intent && a.app.now().Before(review.expires)
	store.mu.Unlock()
	if !valid || a.app.reviewer() != a.reviewer || a.app.policyBinding(ctx, true, true) != a.policy {
		return networkaction.Actor{}, deny
	}
	guard, _ := a.app.selectedOperation()
	if guard.CheckExecutionContext(ctx) != nil {
		return networkaction.Actor{}, deny
	}
	found := false
	for _, binding := range a.bound.run.lifecycle.bindings {
		if binding == wanted {
			found = true
			break
		}
	}
	if !found {
		return networkaction.Actor{}, deny
	}
	loaded, items, records, declined := a.app.scopedAt(ctx, a.bound.origin.Context, a.bound.origin.Items)
	if loaded == nil || declined.state != "" || len(records) != 1 {
		return networkaction.Actor{}, deny
	}
	_ = items
	individual := a.bound.run.lifecycle
	_, revision, err := loaded.testVersionPath(records[0], a.bound.origin.Items[0].Revision)
	if err != nil {
		return networkaction.Actor{}, deny
	}
	saved, err := loaded.connectedTestOf(records[0], revision)
	if err != nil {
		return networkaction.Actor{}, deny
	}
	environment := ""
	if a.bound.origin.Destination != nil {
		environment = a.bound.origin.Destination.ID
	} else if saved.links != nil {
		environment = saved.links.Environment
	}
	marker := ""
	if needsConnectedRuntimeMarker(saved.draft) {
		if _, _, err := a.app.connectedRuntimeMarker(ctx, a.bound.origin.Context, individual.runtimeMarker, true, loaded); err != nil {
			return networkaction.Actor{}, deny
		}
		marker = "run-" + individual.runtimeMarker
	}
	fresh, problems := loaded.compileConnected(saved.draft, connectedLifecycleID(records[0].ID), revision, connectedEnvironments{environment: environment, server: saved.draft.Server, runtimeMarker: marker})
	if fresh == nil || len(problems) != 0 {
		return networkaction.Actor{}, deny
	}
	localInput, err := fresh.localInputIdentity()
	if err != nil || localInput != individual.localInput {
		return networkaction.Actor{}, deny
	}
	return a.actor, nil
}

func executeConnectedIndividual(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions, output string) ReviewedActionResult {
	if bound.executionReview == nil {
		return ReviewedActionResult{State: Failed, Outcome: ActionRefused, Reason: "a connected lifecycle requires final reviewed consent"}
	}
	individual := bound.run.lifecycle
	if needsConnectedRuntimeMarker(individual.draft) {
		folder, issued, err := a.unusedRuntimeMarker(ctx, bound.origin.Context, individual.runtimeMarker)
		if err != nil {
			return ReviewedActionResult{State: Failed, Outcome: ActionRefused, Reason: "the runtime marker is already used or unavailable; nothing was armed or sent"}
		}
		reserved := connectedRuntimeReservation{Schema: connectedRuntimeReservationSchema, Marker: issued.Marker, Project: issued.Project, Run: bound.run.output, Input: individual.input, Plan: individual.compiled.plan.Identity()}
		raw, err := encodeMember(reserved)
		if err != nil || exchangeFile.Create(filepath.Join(folder, "reservation.json"), raw) != nil {
			return ReviewedActionResult{State: Failed, Outcome: ActionRefused, Reason: "the runtime marker could not be reserved once; nothing was armed or sent"}
		}
	}
	prepared, remove, err := prepareConnectedIndividual(ctx, individual.compiled, individual.instance)
	if err != nil {
		return ReviewedActionResult{State: Failed, Outcome: ActionStale, Reason: "the connected configuration changed; review it again"}
	}
	defer remove()
	input, err := prepared.InputIdentity()
	if err != nil || input != individual.input {
		return ReviewedActionResult{State: Failed, Outcome: ActionStale, Reason: "the connected inputs changed; review it again"}
	}
	lease := bound.executionReview
	actor := networkaction.Actor{Kind: "action-review", ID: "reviewer-" + binding(a.reviewer())[:16], Generation: "review-" + binding(lease.token)[:16], EvidenceIdentity: bound.binding, Expires: lease.expires}
	authority := connectedIndividualAuthority{app: a, bound: bound, actor: actor, reviewer: a.reviewer(), policy: a.policyBinding(ctx, true, true)}
	current := networkaction.WithAuthority(ctx, authority)
	confirmation := testisolation.Confirmation{Plan: prepared.IsolationIdentity(), Instance: individual.instance, Steps: slices.Clone(decisions.Confirmed)}
	actual, err := connectedrun.ExecuteFlow(current, prepared, output, confirmation)
	result := ReviewedActionResult{State: Completed, Outcome: ActionCompleted}
	if actual.Schema == "" {
		result.State, result.Outcome, result.Reason = Failed, ActionRefused, "the connected lifecycle refused before a retained result; inspect available work and review again"
		return result
	}
	view := connectedLifecycleView(bound.run.output, actual)
	result.Lifecycle = &view
	result.Run = a.runItemOf(context.WithoutCancel(ctx), bound.origin.Context, bound.run.output)
	if actual.State == "uncertain" {
		result.Outcome = ActionUncertain
	} else if ctx.Err() != nil {
		result.State, result.Outcome = Cancelled, ActionCancelled
	}
	if err != nil {
		result.Reason = "the connected lifecycle stopped; retained phases and cleanup remain inspectable, and nothing is resent automatically"
	}
	return result
}

func connectedIndividualArtifact(path string) bool {
	return declares(filepath.Join(path, "manifest.json"), connectedrun.FlowSchema) || declares(filepath.Join(path, "manifest.json"), connectedrun.FlowSchemaV4) || declares(filepath.Join(path, "started.json"), connectedrun.FlowSchema) || declares(filepath.Join(path, "started.json"), connectedrun.FlowSchemaV4)
}
func connectedIndividualResult(r connectedrun.FlowResult) RunResult {
	if r.State == "uncertain" || r.State != "complete" || r.Cleanup != "complete" {
		return RunIncomplete
	}
	switch r.Verdict {
	case assertion.VerdictPass:
		return RunPassed
	case assertion.VerdictFail:
		return RunFailed
	}
	return RunIncomplete
}
func readConnectedIndividualRun(c *loadedCatalog, item catalog.Item, path string) (view, error) {
	evidence, err := connectedrun.OpenFlowEvidence(context.Background(), path)
	if err != nil {
		// Interrupted lifecycles remain inspectable through the engine's recovery
		// reader. No assertion is promoted to a passing result from partial work.
		recovery, err := connectedrun.InspectFlow(context.Background(), path)
		if err != nil {
			return view{}, err
		}
		unsealed := false
		summary := &RunSummary{CanCompare: &unsealed, Kind: TestRunKind, Entry: item.Entry, Result: RunInterrupted, DeliveryUncertain: recovery.State == "uncertain", Boundary: recovery.Boundary, StartedAt: stampedTime(recovery.StartedAt)}
		name := "Interrupted connected test"
		if origin, held, err := c.recordedRunOrigin(path, recovery.Plan); err == nil && held && origin.Source.Kind == TestItem {
			summary.Test = &origin.Source
			summary.SourceCases = slices.Clone(origin.SourceCases)
			summary.Version = origin.Source.Revision
			summary.Environment = origin.Environment
			summary.EnvironmentName = origin.EnvironmentName
			name = origin.Name
		}
		c.connectedRunAssociations(summary)
		return view{name: name, summary: ItemSummary{Run: summary}}, nil
	}
	r := evidence.Result
	comparable := true
	summary := &RunSummary{CanCompare: &comparable, Kind: TestRunKind, Entry: item.Entry, Result: connectedIndividualResult(r), Outcome: r.State, Boundary: r.Boundary,
		StartedAt: stampedTime(r.StartedAt), CompletedAt: stampedTime(r.CompletedAt), DeliveryUncertain: r.State == "uncertain"}
	name := evidence.Plan.Document().Test.ID
	origin, held, err := c.recordedRunOrigin(path, r.Plan)
	if err == nil && held && origin.Source.Kind == TestItem && evidence.Plan.Document().Test.ID == connectedLifecycleID(origin.Source.ID) && evidence.Plan.Document().Test.Revision == origin.Source.Revision {
		summary.Test = &origin.Source
		summary.SourceCases = slices.Clone(origin.SourceCases)
		summary.Version = origin.Source.Revision
		summary.Environment = origin.Environment
		summary.EnvironmentName = origin.EnvironmentName
		name = origin.Name
	}
	c.connectedRunAssociations(summary)
	if c.executing != nil && c.executing(path) {
		summary.Active = true
		summary.Result = RunRunning
	}
	return view{name: name, summary: ItemSummary{Run: summary}, createdAt: &r.StartedAt}, nil
}
