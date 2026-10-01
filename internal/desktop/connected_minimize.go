package desktop

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ConnectedMinimizeOptions chooses a saved suite's approved lifecycle pin.
// The suite owns the local plan/configuration names; the window chooses only
// the named object, authored test ID and saved environment binding.
type ConnectedMinimizeOptions struct {
	Suite       ItemRef `json:"suite"`
	Test        string  `json:"test"`
	Environment string  `json:"environment"`
}
type ConnectedMinimizeChoice struct {
	Options  ConnectedMinimizeOptions `json:"options"`
	Name     string                   `json:"name"`
	Eligible bool                     `json:"eligible"`
	Refusal  string                   `json:"refusal,omitzero"`
}
type ConnectedMinimizeObservation struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Phase     string `json:"phase"`
	HorizonMS int64  `json:"horizon_ms"`
	Boundary  string `json:"boundary,omitzero"`
	Meaning   string `json:"meaning,omitzero"`
}
type ConnectedMinimizeReview struct {
	Suite        ItemRef                        `json:"suite"`
	Name         string                         `json:"name"`
	Test         string                         `json:"test"`
	Boundary     string                         `json:"boundary"`
	Checks       assertion.DatasetSetDocument   `json:"checks"`
	Observations []ConnectedMinimizeObservation `json:"observations"`
	Isolation    IsolationActionReview          `json:"isolation"`
}
type connectedMinimizeBinding struct {
	plan     *connectedtest.FlowPlan
	config   string
	identity string
	review   ConnectedMinimizeReview
	project  string
	files    map[string]string
	items    map[string]string
}

func connectedMinimizeChoices(loaded *loadedCatalog, run *minimizeRun) []ConnectedMinimizeChoice {
	choices := []ConnectedMinimizeChoice{}
	var environment *environmentRun
	if run.summary.Environment != nil {
		environment, _ = loaded.namedEnvironment(run.summary.Environment.ID)
	}
	for _, item := range loaded.document.Items {
		if item.Kind != string(SuiteItem) || loaded.removed(item) {
			continue
		}
		version, err := loaded.suiteVersion(item, "")
		if err != nil || version.connected == nil {
			continue
		}
		for _, scope := range version.connected.Environments {
			for _, pin := range scope.Bindings {
				options := ConnectedMinimizeOptions{Suite: ItemRef{Kind: SuiteItem, ID: item.ID, Revision: version.label}, Test: pin.Test, Environment: scope.ID}
				choice := ConnectedMinimizeChoice{Options: options, Name: loaded.suiteName(item) + " · " + pin.Test + " · " + scope.ID}
				_, err := selectConnectedMinimize(loaded, run, environment, options)
				choice.Eligible = err == nil
				if err != nil {
					choice.Refusal = err.Error()
				}
				choices = append(choices, choice)
			}
		}
	}
	return choices
}

func selectConnectedMinimize(loaded *loadedCatalog, run *minimizeRun, environment *environmentRun, options ConnectedMinimizeOptions) (*connectedMinimizeBinding, error) {
	refused := errors.New("choose an approved connected lifecycle with the same original messages, every saved ACK expectation and this environment's exact isolation setup")
	if run.spec == nil || environment == nil || run.spec.spec.Observation.Boundary != testrunner.ACKBoundary {
		return nil, refused
	}
	if run.spec.release {
		return nil, errors.New("this connected adapter does not execute released tests; their profile and predecessor pins remain retained")
	}
	item, err := loaded.suiteItem(options.Suite)
	if err != nil {
		return nil, err
	}
	version, err := loaded.suiteVersion(item, options.Suite.Revision)
	if err != nil || version.connected == nil {
		return nil, refused
	}
	scope := slices.IndexFunc(version.connected.Environments, func(e suite.ConnectedEnvironment) bool { return e.ID == options.Environment })
	if scope < 0 {
		return nil, refused
	}
	pinIndex := slices.IndexFunc(version.connected.Environments[scope].Bindings, func(b suite.ConnectedBinding) bool { return b.Test == options.Test })
	testIndex := slices.IndexFunc(version.connected.Tests, func(t suite.ConnectedTest) bool { return t.ID == options.Test })
	if pinIndex < 0 || testIndex < 0 || version.connected.Tests[testIndex].State != "enabled" || len(version.connected.Tests[testIndex].After) != 0 {
		return nil, refused
	}
	pin := version.connected.Environments[scope].Bindings[pinIndex]
	planPath, err := artifactpath.Child(loaded.root, pin.Plan)
	if err != nil {
		return nil, refused
	}
	config, err := artifactpath.File(loaded.root, pin.Config)
	if err != nil {
		return nil, refused
	}
	plan, err := connectedtest.OpenFlowPlan(planPath)
	if err != nil || plan.Identity() != pin.PlanIdentity {
		return nil, refused
	}
	pinned := version.connected.Tests[testIndex]
	authored, err := expectation.ReviewConnected(planPath)
	if err != nil || authored.ID != pinned.ID || authored.Revision != pinned.Revision || authored.Definition != pinned.Definition {
		return nil, refused
	}
	releasePath, err := artifactpath.File(loaded.root, pinned.Release)
	if err != nil {
		return nil, refused
	}
	raw, err := boundedFile(releasePath, expectation.MaxBytes)
	if err != nil {
		return nil, refused
	}
	release, err := expectation.DecodeConnected(raw)
	if err != nil || release.Identity() != pinned.ReleaseIdentity || release.Review != authored {
		return nil, refused
	}
	doc := plan.Document()
	if doc.Test.Project != loaded.document.Project.ID {
		return nil, refused
	}
	signature := reduce.Signature{State: "assertion_failed", Assertions: expectationIDs(run.failed)}
	messages, _, err := reduce.InspectConnectedOracle(plan, signature)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(messages, run.spec.spec.Input.Messages) {
		return nil, refused
	}
	sourcePath := artifactpath.JoinReference(filepath.Dir(run.specPath), run.spec.spec.Input.Case)
	source, err := bundle.Open(sourcePath)
	if err != nil {
		return nil, refused
	}
	for _, step := range doc.Test.Steps {
		index := slices.IndexFunc(source.Events, func(e bundle.Event) bool { return e.ID == step.V2.Occurrence })
		if index < 0 {
			return nil, refused
		}
		message, err := source.Document(source.Events[index])
		if err != nil {
			return nil, refused
		}
		raw := message.Bytes(message.Messages[0].Span)
		original, err := source.Raw(step.V2.Occurrence)
		input := connectedMinimizeDependency(plan, step.V2.Input)
		if err != nil || !bytes.Equal(raw, input) && !bytes.Equal(original, input) {
			return nil, refused
		}
	}
	phase := doc.Test.Phases[0]
	if phase.Wire == nil {
		return nil, refused
	}
	wire, err := assertion.Decode(plan.Dependency(phase.Wire.Set))
	if err != nil || len(wire.Assertions) != len(run.spec.spec.Assertions) {
		return nil, refused
	}
	for _, original := range run.spec.spec.Assertions {
		index := slices.IndexFunc(wire.Assertions, func(a assertion.Assertion) bool { return a.ID == original.ID })
		if index < 0 {
			return nil, refused
		}
		check := wire.Assertions[index]
		if original.Operator != "ack_field_equals" || original.Expected.Field == nil {
			return nil, refused
		}
		want := assertion.FieldValue{State: original.Expected.Field.State, Text: original.Expected.Field.Text}
		if check.Operator != assertion.FieldEquals || check.When != nil || check.Subject.Field == nil ||
			check.Subject.Field.Scope != assertion.ObservedMessages || check.Subject.Field.Message != original.Message || check.Subject.Field.Selector != original.Selector ||
			check.Expected.Field == nil || !reflect.DeepEqual(*check.Expected.Field, want) {
			return nil, refused
		}
	}
	rp, err := replay.Prepare(sourcePath, environment.members.target, replay.Options{Occurrences: messages})
	if err != nil || rp.Target().Identity() != doc.Test.Environment.TargetIdentity {
		return nil, refused
	}
	paths, isolation, classification, err := isolationScope(loaded, environment.record)
	if err != nil {
		return nil, refused
	}
	contract, _, _, err := isolationConfiguration(loaded.document.Project.ID, isolation, classification, nil)
	if err != nil {
		return nil, refused
	}
	var selected testisolation.Contract
	if json.Unmarshal(plan.Dependency(doc.Test.Isolation), &selected, json.RejectUnknownMembers(true)) != nil || !reflect.DeepEqual(contract, selected) {
		return nil, refused
	}
	// Every historical manual reset clause is retained in the lifecycle's
	// per-execution confirmation, including the test's own instructions.
	manual := map[string]string{}
	for _, step := range contract.Manual {
		manual[step.ID] = step.Instructions
	}
	if environment.members.reset != nil {
		for _, action := range environment.members.reset.Actions {
			if action.Operator != fixturereset.OperatorConfirms || manual[action.ID] != action.Instructions {
				return nil, refused
			}
		}
	}
	if !slices.ContainsFunc(contract.Manual, func(step testisolation.ManualStep) bool {
		return step.Instructions == run.spec.spec.Setup.ResetInstructions
	}) {
		return nil, refused
	}
	prepared, err := connectedrun.PrepareFlow(planPath, config, "minimize-review")
	if err != nil {
		return nil, refused
	}
	registry, err := prepared.RegistrySnapshot()
	if err != nil {
		return nil, refused
	}
	_, namedRegistry, err := isolationRegistry(isolation.RegistryFile)
	if err != nil || !bytes.Equal(registry, namedRegistry) {
		return nil, refused
	}
	input, err := prepared.InputIdentity()
	if err != nil {
		return nil, refused
	}
	checks, err := assertion.DecodeDatasets(connectedMinimizeDependency(plan, phase.Checks))
	if err != nil {
		return nil, err
	}
	review := ConnectedMinimizeReview{Suite: ItemRef{Kind: SuiteItem, ID: item.ID, Revision: version.label}, Name: loaded.suiteName(item), Test: options.Test,
		Boundary: doc.Test.Boundary, Checks: checks.Document(), Observations: []ConnectedMinimizeObservation{}, Isolation: IsolationActionReview{Name: isolation.Name,
			Adapter: contract.Adapter, RegisteredEnvironment: contract.Environment, EnvironmentRevision: contract.Revision, Tenant: contract.Tenant, Namespace: contract.Namespace,
			Effects: []IsolationEffectReview{}, Manual: slices.Clone(isolation.Manual)}}
	for _, ds := range phase.Datasets {
		observation := ConnectedMinimizeObservation{ID: ds.ID, Kind: ds.Kind, Phase: ds.Phase, HorizonMS: ds.Completion.HorizonMS}
		if ds.Kind == "fhir-resources" {
			definition, _, _, err := plan.Phase(phase.ID).ObservationURL(ds.ID)
			if err != nil {
				return nil, refused
			}
			observation.Boundary, observation.Meaning = definition.Boundary, fhirobserve.Meaning(definition.Boundary)
		}
		review.Observations = append(review.Observations, observation)
	}
	for _, resource := range isolation.Resources {
		review.Isolation.Effects = append(review.Isolation.Effects, IsolationEffectReview{Resource: resource.Name, Kind: resource.Kind, Operation: resource.Ownership, LogicalID: resource.LogicalID, Version: resource.Version})
	}
	encoded, _ := json.Marshal(review, json.Deterministic(true))
	selectedBinding := &connectedMinimizeBinding{plan: plan, config: config, review: review, project: loaded.document.Project.ID, files: map[string]string{}, items: map[string]string{}, identity: binding(input, version.label, networkaction.Digest(version.data), networkaction.Digest(encoded), memberDigest(environment.record, "isolation"), memberDigest(environment.record, "isolation-policy"), fileDigest(paths["isolation"]))}
	for _, path := range []string{run.specPath, config, releasePath, isolation.RegistryFile} {
		selectedBinding.files[path] = fileDigest(path)
	}
	for name := range plan.Files() {
		path := filepath.Join(planPath, name)
		selectedBinding.files[path] = fileDigest(path)
	}
	runIndex := loaded.document.Find(run.item.Ref.ID)
	if runIndex < 0 || run.testItem == nil {
		return nil, refused
	}
	for _, record := range []catalog.Item{item, environment.record, *run.testItem, loaded.document.Items[runIndex]} {
		selectedBinding.items[record.ID] = connectedMinimizeItemIdentity(record)
		for _, revision := range record.Revisions {
			for _, member := range revision.Members {
				path := filepath.Join(loaded.root, member.Path)
				selectedBinding.files[path] = fileDigest(path)
			}
		}
	}
	return selectedBinding, nil
}

// Each trial gets a new generation over its newly compiled exact bindings.
// Every effect rechecks the executing one-action lease, admission, selected
// named versions and all local configuration before the network boundary.
func (a *App) authorizeMinimizeTrial(bound *boundAction) func(context.Context, int, *connectedrun.PreparedFlow) (networkaction.Authority, error) {
	return func(ctx context.Context, trial int, prepared *connectedrun.PreparedFlow) (networkaction.Authority, error) {
		lease := bound.executionReview
		if lease == nil || bound.minimize.connected == nil {
			return nil, errors.New("a connected minimization needs its final reviewed action")
		}
		loaded, run, declined := a.readMinimizeRun(ctx, bound.origin.Context, bound.origin.Items[0])
		if loaded == nil || declined.state != "" || run.refusal != "" {
			return nil, errors.New("the connected minimization inputs changed")
		}
		environment, _ := loaded.namedEnvironment(bound.review.Minimize.Environment.ID)
		fresh, err := selectConnectedMinimize(loaded, run, environment, *bound.origin.Minimize.Connected)
		if err != nil || fresh.identity != bound.minimize.connected.identity {
			return nil, errors.New("the connected minimization inputs changed")
		}
		actor := networkaction.Actor{Kind: "action-review", ID: "reviewer-" + binding(a.reviewer())[:16], Generation: "trial-" + strconv.Itoa(trial) + "-" + binding(lease.intent)[:16], EvidenceIdentity: binding(bound.binding, prepared.IsolationIdentity()), Expires: lease.expires}
		return connectedMinimizeAuthority{app: a, bound: bound, bindings: prepared.Bindings(), actor: actor, reviewer: a.reviewer(), policy: a.policyBinding(ctx, true, true)}, nil
	}
}

type connectedMinimizeAuthority struct {
	app              *App
	bound            *boundAction
	bindings         map[string]networkaction.Binding
	actor            networkaction.Actor
	reviewer, policy string
}

func (r connectedMinimizeAuthority) Check(ctx context.Context, requested networkaction.Binding) (networkaction.Actor, error) {
	refused := errors.New("the connected minimization review is no longer authorized")
	if ctx.Err() != nil || !slices.Contains(slices.Collect(maps.Values(r.bindings)), requested) {
		return networkaction.Actor{}, refused
	}
	lease := r.bound.executionReview
	if lease == nil {
		return networkaction.Actor{}, refused
	}
	store := &r.app.reviews
	store.mu.Lock()
	held := store.reviews[lease.token]
	valid := held != nil && !held.withdrawn && held.consumed == lease.intent && store.running == lease.intent && r.app.now().Before(held.expires)
	store.mu.Unlock()
	if !valid || r.app.reviewer() != r.reviewer || r.app.policyBinding(ctx, true, true) != r.policy {
		return networkaction.Actor{}, refused
	}
	guard, _ := r.app.selectedOperation()
	if guard.CheckExecutionContext(ctx) != nil {
		return networkaction.Actor{}, refused
	}
	selected := r.bound.minimize.connected
	storeInputs, err := catalog.Open(r.bound.minimize.root)
	if err != nil {
		return networkaction.Actor{}, refused
	}
	document, present, err := storeInputs.Read()
	if err != nil || !present || document.Project.ID != selected.project {
		return networkaction.Actor{}, refused
	}
	for id, expected := range selected.items {
		index := document.Find(id)
		if index < 0 || connectedMinimizeItemIdentity(document.Items[index]) != expected {
			return networkaction.Actor{}, refused
		}
	}
	for path, expected := range selected.files {
		if expected == "unreadable" || fileDigest(path) != expected {
			return networkaction.Actor{}, refused
		}
	}
	return r.actor, nil
}

func connectedMinimizeItemIdentity(item catalog.Item) string {
	item.CreatedAt, item.UpdatedAt, item.LastOpenedAt = "", "", ""
	raw, _ := json.Marshal(item, json.Deterministic(true))
	return networkaction.Digest(raw)
}

func connectedMinimizeDependency(plan *connectedtest.FlowPlan, ref connectedtest.Reference) []byte {
	if raw := plan.Dependency(ref); raw != nil {
		return raw
	}
	return plan.Phase(plan.Document().Test.Phases[0].ID).Files()["dependencies/"+ref.SHA256]
}
