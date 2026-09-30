package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testisolation"
)

const EnvironmentIsolationSchema = "readmit-environment-isolation/v1"

const (
	PreflightIsolationAction ActionID = "environment.isolation.preflight"
	SetupIsolationAction     ActionID = "environment.isolation.setup"
	ReconcileIsolationAction ActionID = "environment.isolation.reconcile"
	CleanupIsolationAction   ActionID = "environment.isolation.cleanup"
)

// EnvironmentIsolation is the managed Reset editor's typed isolation plan.
// The registered adapter, its scope and three credential roles stay operator
// controlled. IDs, policies and runtime evidence names are generated internally.
// Historical fixturereset plans keep their separate unchanged contract.
type EnvironmentIsolation struct {
	Schema       string              `json:"schema"`
	Name         string              `json:"name"`
	RegistryFile string              `json:"registry_file"`
	Adapter      string              `json:"adapter"`
	Mode         string              `json:"mode"`
	Resources    []IsolationResource `json:"resources"`
	Manual       []IsolationManual   `json:"manual"`
}

type IsolationResource struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Kind        string                     `json:"kind"`
	Template    string                     `json:"template"`
	Ownership   string                     `json:"ownership"`
	LogicalID   string                     `json:"logical_id,omitzero"`
	Version     string                     `json:"version,omitzero"`
	DependsOn   []string                   `json:"depends_on"`
	Attributes  map[string]string          `json:"attributes"`
	Identifiers []testisolation.Identifier `json:"identifiers"`
}

type IsolationManual struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
}

// Registry choices contain only the scope and the registered typed templates.
// Provider arguments, credential material and certificate bytes never enter
// the editor, connection inventory or inspector result.
type IsolationAdapterChoice struct {
	ID                  string                   `json:"id"`
	Revision            string                   `json:"revision"`
	Project             string                   `json:"project"`
	Environment         string                   `json:"environment"`
	EnvironmentRevision string                   `json:"environment_revision"`
	Tenant              string                   `json:"tenant"`
	Namespace           string                   `json:"namespace"`
	Address             string                   `json:"address"`
	Templates           []testisolation.Template `json:"templates"`
}

type IsolationEditorRequest struct {
	Context      RequestContext `json:"context"`
	RegistryFile string         `json:"registry_file"`
}

type IsolationEditorResult struct {
	State         State                    `json:"state"`
	Reason        string                   `json:"reason,omitzero"`
	Context       RequestContext           `json:"context"`
	Adapters      []IsolationAdapterChoice `json:"adapters"`
	Modes         []string                 `json:"modes"`
	ResourceKinds []string                 `json:"resource_kinds"`
	Ownership     []string                 `json:"ownership"`
}

func (r *IsolationEditorResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// GetIsolationEditor reads one operator-selected local registry for supported
// choices. It never performs discovery, reads credentials or contacts a target.
func (a *App) GetIsolationEditor(request IsolationEditorRequest) IsolationEditorResult {
	return runRead(a, false, func(ctx context.Context) IsolationEditorResult {
		vocabulary := testisolation.EditorChoices()
		result := IsolationEditorResult{Context: request.Context, Adapters: []IsolationAdapterChoice{}, Modes: vocabulary.Modes, ResourceKinds: vocabulary.ResourceKinds, Ownership: vocabulary.Ownership}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.State, result.Reason = declined.state, declined.reason
			return result
		}
		choices, err := isolationRegistryChoices(request.RegistryFile)
		if err != nil {
			result.State, result.Reason = Failed, err.Error()
			return result
		}
		for _, choice := range choices {
			if choice.Project == loaded.document.Project.ID {
				result.Adapters = append(result.Adapters, choice)
			}
		}
		result.State = Completed
		if len(result.Adapters) == 0 {
			result.State, result.Reason = Empty, "No registered fixture adapter is scoped to this project"
		}
		return result
	})
}

func isolationRegistry(path string) (testisolation.Registry, []byte, error) {
	var registry testisolation.Registry
	if !filepath.IsAbs(path) {
		return registry, nil, errors.New("Choose the operator's registered fixture adapters")
	}
	raw, err := (artifactdir.Document{MaxBytes: testisolation.MaxBytes}).Read(path)
	if err != nil || json.Unmarshal(raw, &registry, json.RejectUnknownMembers(true)) != nil || registry.Schema != testisolation.RegistrySchema || len(registry.Adapters) == 0 || len(registry.Adapters) > 32 {
		return registry, nil, errors.New("The registered fixture adapters cannot be read")
	}
	return registry, raw, nil
}

func isolationRegistryChoices(path string) ([]IsolationAdapterChoice, error) {
	registry, _, err := isolationRegistry(path)
	if err != nil {
		return nil, err
	}
	choices := []IsolationAdapterChoice{}
	for _, adapter := range registry.Adapters {
		if adapter.Classification != "nonproduction" || len(adapter.Templates) == 0 || len(adapter.Templates) > 32 {
			continue
		}
		templates := []testisolation.Template{}
		for _, template := range adapter.Templates {
			if slices.Contains(testisolation.EditorChoices().ResourceKinds, template.Kind) {
				templates = append(templates, testisolation.Template{ID: template.ID, Kind: template.Kind, Attributes: slices.Clone(template.Attributes)})
			}
		}
		choices = append(choices, IsolationAdapterChoice{ID: adapter.ID, Revision: adapter.Revision, Project: adapter.Project, Environment: adapter.Environment, EnvironmentRevision: adapter.EnvironmentRevision, Tenant: adapter.Tenant, Namespace: adapter.Namespace, Address: adapter.URL, Templates: templates})
	}
	return choices, nil
}

func normalizeIsolation(draft EnvironmentIsolation) (EnvironmentIsolation, []FieldProblem) {
	raw, err := encodeMember(draft)
	if err != nil {
		return draft, []FieldProblem{{Field: "isolation", Problem: "The complete isolation plan exceeds the editor's bounds"}}
	}
	var normalized EnvironmentIsolation
	_ = json.Unmarshal(raw, &normalized)
	if normalized.Schema == "" {
		normalized.Schema = EnvironmentIsolationSchema
	}
	problems := []FieldProblem{}
	add := func(field, reason string) {
		problems = append(problems, FieldProblem{Field: "isolation." + field, Problem: reason})
	}
	if normalized.Schema != EnvironmentIsolationSchema {
		add("schema", "This isolation plan needs an editor for its version")
	}
	if !catalog.ValidName(normalized.Name) {
		add("name", nameRule)
	}
	if !slices.Contains(testisolation.EditorChoices().Modes, normalized.Mode) {
		add("mode", "Choose an isolated tenant or a reserved namespace; keep recorded baseline proof in its supported workflow")
	}
	resourceIDs := map[string]bool{}
	manualIDs := map[string]bool{}
	for _, resource := range normalized.Resources {
		if resource.ID != "" {
			resourceIDs[resource.ID] = true
		}
	}
	for _, manual := range normalized.Manual {
		if manual.ID != "" {
			manualIDs[manual.ID] = true
		}
	}
	allocate := func(name string, used map[string]bool) string {
		base := actionSlug(name)
		if base == "" {
			base = "resource"
		}
		id := base
		for i := 2; used[id]; i++ {
			id = base + "-" + strconv.Itoa(i)
		}
		used[id] = true
		return id
	}
	if len(normalized.Resources) == 0 || len(normalized.Resources) > 32 {
		add("resources", "Choose between one and 32 registered prerequisites")
	}
	seen := map[string]string{}
	previousIDs := map[string]bool{}
	newNames := map[string]bool{}
	for i := range normalized.Resources {
		resource := &normalized.Resources[i]
		prefix := "resources." + strconv.Itoa(i)
		if !catalog.ValidName(resource.Name) || seen[resource.Name] != "" {
			add(prefix+".name", "Name each prerequisite distinctly")
		}
		generated := resource.ID == ""
		if generated {
			resource.ID = allocate(resource.Name, resourceIDs)
		}
		for j, dependency := range resource.DependsOn {
			if previousIDs[dependency] {
				if newNames[dependency] && seen[dependency] != dependency {
					add(prefix+".depends_on", "Give the new prerequisite a distinct name before selecting it")
				}
				continue
			}
			if id, ok := seen[dependency]; ok {
				resource.DependsOn[j] = id
			}
		}
		seen[resource.Name] = resource.ID
		previousIDs[resource.ID] = true
		newNames[resource.Name] = generated
	}
	if len(normalized.Manual) > 16 {
		add("manual", "Choose at most 16 explicit manual claims")
	}
	names := map[string]bool{}
	for i := range normalized.Manual {
		manual := &normalized.Manual[i]
		if !catalog.ValidName(manual.Name) || names[manual.Name] {
			add("manual."+strconv.Itoa(i)+".name", "Name each manual claim distinctly")
		}
		names[manual.Name] = true
		if manual.ID == "" {
			manual.ID = allocate(manual.Name, manualIDs)
		}
	}
	return normalized, problems
}

// isolationConfiguration preserves the selected operator registration's scope.
// Selecting an adapter never silently reassigns its project, environment,
// revision, tenant, credential roles or templates to the local connection.
func isolationConfiguration(project string, draft EnvironmentIsolation, classification replay.Classification, approved *sendpolicy.Policy) (testisolation.Contract, testisolation.Registry, sendpolicy.ScopedPolicy, error) {
	refused := errors.New("Choose an authorized fixture adapter scoped to this project and a Nonproduction connection")
	registry, _, err := isolationRegistry(draft.RegistryFile)
	if err != nil {
		return testisolation.Contract{}, registry, sendpolicy.ScopedPolicy{}, err
	}
	var registration *testisolation.Registration
	for i := range registry.Adapters {
		if registry.Adapters[i].ID == draft.Adapter {
			if registration != nil {
				return testisolation.Contract{}, registry, sendpolicy.ScopedPolicy{}, refused
			}
			registration = &registry.Adapters[i]
		}
	}
	if registration == nil || registration.Project != project || registration.Classification != "nonproduction" || classification != replay.Nonproduction {
		return testisolation.Contract{}, registry, sendpolicy.ScopedPolicy{}, refused
	}
	a := *registration
	contract := testisolation.Contract{Schema: testisolation.ContractSchema, Project: a.Project, Environment: a.Environment, Revision: a.EnvironmentRevision, Adapter: a.ID, Tenant: a.Tenant, Namespace: a.Namespace, Mode: draft.Mode, Concurrency: "exclusive-target-lease", Resources: []testisolation.Requirement{}, Manual: []testisolation.ManualStep{}}
	for _, resource := range draft.Resources {
		contract.Resources = append(contract.Resources, testisolation.Requirement{ID: resource.ID, Kind: resource.Kind, Template: resource.Template, Ownership: resource.Ownership, LogicalID: resource.LogicalID, Version: resource.Version, DependsOn: resource.DependsOn, Attributes: resource.Attributes, Identifiers: resource.Identifiers})
	}
	for _, manual := range draft.Manual {
		contract.Manual = append(contract.Manual, testisolation.ManualStep{ID: manual.ID, Instructions: manual.Instructions})
	}
	policy := sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: a.Project, Environment: a.Environment, Revision: a.EnvironmentRevision, Rules: []sendpolicy.ScopeRule{}}
	u, err := url.Parse(a.URL)
	if err != nil {
		return contract, registry, policy, refused
	}
	port := 443
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil {
			return contract, registry, policy, refused
		}
	}
	destinations := []string{}
	if approved != nil {
		destinations = slices.Clone(approved.ApprovedDestinations)
	}
	for _, role := range []testisolation.Credential{a.Read, a.Setup, a.Cleanup} {
		policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: role.Endpoint, Operation: role.Reference.Purpose, Port: port, Destinations: slices.Clone(destinations), Selection: "single-address"})
	}
	return contract, registry, policy, nil
}

func validateIsolationDraft(project string, draft EnvironmentIsolation, classification replay.Classification, approved *sendpolicy.Policy) ([]catalog.Staged, EnvironmentIsolation, []FieldProblem) {
	normalized, problems := normalizeIsolation(draft)
	contract, registry, policy, err := isolationConfiguration(project, normalized, classification, approved)
	contractRaw, _ := encodeMember(contract)
	registryRaw, _ := encodeMember(registry)
	policyRaw, _ := encodeMember(policy)
	if err == nil {
		err = testisolation.ValidateConfiguration(contractRaw, registryRaw, policyRaw, testisolation.Options{ParentPlan: networkaction.Digest(contractRaw), Instance: "editor", Seed: 0})
	}
	if err != nil {
		problems = append(problems, FieldProblem{Field: "isolation", Problem: "The selected adapter, registered fields, exact versions and prerequisites must authorize this isolation plan"})
	}
	if len(problems) > 0 {
		return nil, normalized, problems
	}
	raw, err := encodeMember(normalized)
	if err != nil {
		return nil, normalized, []FieldProblem{{Field: "isolation", Problem: "The complete isolation plan exceeds the editor's bounds"}}
	}
	return []catalog.Staged{{Role: "isolation", File: "isolation.json", Data: raw}, {Role: "isolation-policy", File: "isolation-policy.json", Data: policyRaw}}, normalized, nil
}

func readEnvironmentIsolation(path string) (EnvironmentIsolation, error) {
	raw, err := (artifactdir.Document{MaxBytes: testisolation.MaxBytes}).Read(path)
	var plan EnvironmentIsolation
	if err != nil || json.Unmarshal(raw, &plan, json.RejectUnknownMembers(true)) != nil || plan.Schema != EnvironmentIsolationSchema {
		return plan, errors.New("The saved isolation plan cannot be read")
	}
	normalized, problems := normalizeIsolation(plan)
	before, _ := encodeMember(plan)
	after, _ := encodeMember(normalized)
	if len(problems) != 0 || string(before) != string(after) {
		return plan, errors.New("The isolation plan needs its original supported editor")
	}
	return plan, nil
}

func verifyEnvironmentIsolation(files map[string]string) error {
	path, held := files["isolation"]
	if !held {
		if files["isolation-policy"] != "" {
			return errors.New("An isolation policy needs its named isolation plan")
		}
		return nil
	}
	plan, err := readEnvironmentIsolation(path)
	if err != nil {
		return err
	}
	registry, _, err := isolationRegistry(plan.RegistryFile)
	if err != nil {
		return err
	}
	var project string
	for _, adapter := range registry.Adapters {
		if adapter.ID == plan.Adapter {
			project = adapter.Project
		}
	}
	contract, _, _, err := isolationConfiguration(project, plan, replay.Nonproduction, nil)
	if err != nil {
		return err
	}
	raw, _ := encodeMember(contract)
	_, err = testisolation.Prepare(raw, plan.RegistryFile, files["isolation-policy"], testisolation.Options{ParentPlan: networkaction.Digest(raw), Instance: "editor", Seed: 0})
	return err
}

type IsolationEffectReview struct {
	Resource  string `json:"resource"`
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	LogicalID string `json:"logical_id,omitzero"`
	Version   string `json:"version,omitzero"`
}

type IsolationActionReview struct {
	Name                  string                  `json:"name"`
	Adapter               string                  `json:"adapter"`
	RegisteredEnvironment string                  `json:"registered_environment"`
	EnvironmentRevision   string                  `json:"environment_revision"`
	Tenant                string                  `json:"tenant"`
	Namespace             string                  `json:"namespace"`
	Effects               []IsolationEffectReview `json:"effects"`
	Manual                []IsolationManual       `json:"manual"`
	Effect                string                  `json:"effect"`
}

// IsolationOutcome is a dated execution claim. Complete describes its retained
// journal, not application correctness or the target's present state.
type IsolationOutcome struct {
	Action    ActionID                    `json:"action"`
	CheckedAt string                      `json:"checked_at"`
	Setup     string                      `json:"setup"`
	Cleanup   string                      `json:"cleanup"`
	Complete  bool                        `json:"complete"`
	Resources int                         `json:"resources"`
	Manual    []testisolation.ManualClaim `json:"manual"`
}

const isolationStateSchema = "readmit-environment-isolation-state/v1"

type isolationState struct {
	Schema         string            `json:"schema"`
	Item           string            `json:"item"`
	Revision       string            `json:"revision"`
	Parent         string            `json:"parent"`
	Generation     int               `json:"generation"`
	Instance       string            `json:"instance"`
	Plan           string            `json:"plan"`
	Preflight      string            `json:"preflight,omitzero"`
	Setup          string            `json:"setup,omitzero"`
	Reconciliation string            `json:"reconciliation,omitzero"`
	Cleanup        string            `json:"cleanup,omitzero"`
	Outcome        *IsolationOutcome `json:"outcome,omitzero"`
}

var isolationStateFile = artifactdir.Document{MaxBytes: 64 << 10}

func isolationHistoryRoot(root string, create bool) (*os.Root, error) {
	project, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer project.Close()
	if info, err := project.Lstat(catalog.Folder); err != nil || !info.IsDir() {
		return nil, errors.New("The project's managed storage is unavailable")
	}
	managed, err := project.OpenRoot(catalog.Folder)
	if err != nil {
		return nil, err
	}
	defer managed.Close()
	if create {
		if err := managed.Mkdir("isolation", 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	info, err := managed.Lstat("isolation")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("The isolation history is not a private folder of the project")
	}
	return managed.OpenRoot("isolation")
}

func readIsolationState(root, item string) (isolationState, error) {
	var state isolationState
	if !catalog.ValidID(item) {
		return state, errors.New("An isolation history belongs to one saved environment")
	}
	held, err := isolationHistoryRoot(root, false)
	if err != nil {
		return state, err
	}
	defer held.Close()
	raw, err := isolationStateFile.ReadIn(held, item+".json")
	if err != nil {
		return state, err
	}
	if json.Unmarshal(raw, &state, json.RejectUnknownMembers(true)) != nil || state.Schema != isolationStateSchema || state.Item != item || state.Generation < 1 || state.Generation > 100000 || !networkaction.ValidDigest(state.Parent) || !networkaction.ValidDigest(state.Plan) || state.Instance == "" {
		return state, errors.New("The isolation history cannot be read")
	}
	for _, entry := range []string{state.Preflight, state.Setup, state.Reconciliation, state.Cleanup} {
		if entry != "" && artifactpath.EntryName(entry) != nil {
			return state, errors.New("The isolation history names an invalid outcome")
		}
	}
	return state, nil
}

func isolationHistoryIdentity(state isolationState) string {
	raw, _ := encodeMember(state)
	return networkaction.Digest(raw)
}

// Compare and publish under a short exclusive local writer reservation. The
// reservation is never held across network I/O; a crash leaves it for storage
// inspection rather than silently taking over an interrupted publication.
func retainIsolationState(root string, state isolationState, expected string) error {
	if !catalog.ValidID(state.Item) {
		return errors.New("An isolation history belongs to one saved environment")
	}
	held, err := isolationHistoryRoot(root, true)
	if err != nil {
		return err
	}
	defer held.Close()
	lock := state.Item + ".writer"
	reservation, err := held.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("Another isolation history publication is in progress or interrupted")
	}
	if err := reservation.Close(); err != nil {
		return err
	}
	defer held.Remove(lock)
	current, err := readIsolationState(root, state.Item)
	identity := "absent"
	if err == nil {
		identity = isolationHistoryIdentity(current)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if identity != expected {
		return errors.New("The isolation history changed; review its current state again")
	}
	raw, err := encodeMember(state)
	if err != nil {
		return err
	}
	return isolationStateFile.ReplaceIn(held, state.Item+".json", raw)
}

type isolationBinding struct {
	prepared        *testisolation.Prepared
	root            string
	ref             ItemRef
	output          string
	state           isolationState
	inputIdentity   string
	historyIdentity string
	readReview      testisolation.Review
	setupReview     testisolation.Review
	cleanupReview   testisolation.Review
}

var isolationOutput = outputRule{prefix: "isolation-outcome", invalid: "An isolation outcome is retained in a new entry of this project", exhausted: "The project holds more isolation outcomes than this release numbers"}

func isolationInputs(loaded *loadedCatalog, ref ItemRef, item catalog.Item, registry []byte) string {
	return binding(loaded.document.Project.ID, ref.ID, ref.Revision, memberDigest(item, "target"), memberDigest(item, "isolation"), memberDigest(item, "isolation-policy"), memberDigest(item, "policy"), memberDigest(item, "links"), networkaction.Digest(registry))
}

func isolationScope(loaded *loadedCatalog, item catalog.Item) (map[string]string, EnvironmentIsolation, replay.Classification, error) {
	paths, availability, reason := loaded.backing(item)
	if availability != ItemAvailable {
		return nil, EnvironmentIsolation{}, "", errors.New(reason)
	}
	plan, err := readEnvironmentIsolation(paths["isolation"])
	if err != nil {
		return paths, plan, "", err
	}
	if declares(paths["target"], FHIRConnectionSchema) {
		connection, err := readFHIRConnectionFile(paths["target"])
		return paths, plan, connection.Classification, err
	}
	target, err := replay.ReadRecordedTarget(paths["target"])
	return paths, plan, target.Environment().Classification, err
}

func bindIsolationAction(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != EnvironmentItem || request.Destination != nil {
		return nil, refusal{Failed, "Choose one saved environment with authorized isolation"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	paths, plan, classification, err := isolationScope(loaded, records[0])
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	registry, registryRaw, err := isolationRegistry(plan.RegistryFile)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	contract, _, _, err := isolationConfiguration(loaded.document.Project.ID, plan, classification, nil)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	inputs := isolationInputs(loaded, items[0].Ref, records[0], registryRaw)
	parent := binding("managed-isolation", inputs)
	state, readErr := readIsolationState(loaded.root, records[0].ID)
	historyIdentity := "absent"
	if readErr == nil {
		historyIdentity = isolationHistoryIdentity(state)
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, refusal{Failed, "The isolation history is unavailable; retain it for inspection"}
	}
	if readErr != nil {
		state = isolationState{Schema: isolationStateSchema, Item: records[0].ID, Revision: items[0].Ref.Revision, Parent: parent, Generation: 0, Instance: "unprepared"}
	}
	reason := ""
	if state.Parent != parent {
		if state.Setup != "" && (state.Outcome == nil || state.Outcome.Cleanup != "complete" && state.Outcome.Cleanup != "observed-absent") {
			reason = "The connection or isolation configuration changed while a lease may remain; restore the reviewed configuration before reconciliation"
		} else if request.Action != PreflightIsolationAction {
			reason = "The connection or isolation configuration changed; check fixture capabilities again"
		}
	}
	if request.Action == PreflightIsolationAction {
		if state.Setup != "" && (state.Outcome == nil || state.Outcome.Cleanup != "complete" && state.Outcome.Cleanup != "observed-absent") {
			reason = "Reconcile and clean up the previous isolation before preparing another"
		}
		if reason == "" {
			if request.isolationInstance == "" {
				instance, err := catalog.NewID()
				if err != nil {
					return nil, refusal{Failed, "The isolation instance could not be allocated"}
				}
				request.isolationInstance = "isolation-" + instance
			}
			state = isolationState{Schema: isolationStateSchema, Item: records[0].ID, Revision: items[0].Ref.Revision, Parent: parent, Generation: state.Generation + 1, Instance: request.isolationInstance}
		}
	}
	contractRaw, _ := encodeMember(contract)
	prepared, err := testisolation.Prepare(contractRaw, plan.RegistryFile, paths["isolation-policy"], testisolation.Options{ParentPlan: parent, Instance: state.Instance, Seed: 0})
	if err != nil {
		return nil, refusal{Failed, "The registered adapter, credential references or saved isolation policy no longer authorize this plan"}
	}
	if state.Plan != "" && state.Plan != prepared.Identity() {
		reason = "The adapter or credential registration changed; restore the reviewed configuration before reconciliation"
	}
	state.Plan = prepared.Identity()
	destination, declined := isolationOutput.destination(loaded.root, "")
	if declined.state != "" {
		return nil, declined
	}
	fixed := &isolationBinding{prepared: prepared, root: loaded.root, ref: items[0].Ref, output: destination.Name, state: state, inputIdentity: inputs, historyIdentity: historyIdentity, readReview: prepared.Review("read"), setupReview: prepared.Review("setup"), cleanupReview: prepared.Review("cleanup")}
	display := IsolationActionReview{Name: plan.Name, Adapter: plan.Adapter, RegisteredEnvironment: contract.Environment, EnvironmentRevision: contract.Revision, Tenant: contract.Tenant, Namespace: contract.Namespace, Effects: []IsolationEffectReview{}, Manual: []IsolationManual{}}
	requirements := []ReviewRequirement{}
	phase := "read"
	switch request.Action {
	case PreflightIsolationAction:
		display.Effect = "Reads the fixture adapter's current capabilities. Acquires no lease and changes no resource."
	case SetupIsolationAction:
		phase = "setup"
		display.Effect = "Acquires the exclusive tenant lease, observes prerequisites, and creates or claims only the reviewed resources. The lease remains held until explicit cleanup."
		display.Manual = slices.Clone(plan.Manual)
		if len(display.Manual) > 0 {
			requirements = append(requirements, ConfirmationsRequirement)
		}
		if state.Preflight == "" {
			reason = "Check fixture capabilities explicitly before setting up isolation"
		} else if prior, err := testisolation.Open(filepath.Join(loaded.root, state.Preflight)); err != nil || prior.Plan != prepared.Identity() || prior.Setup != "preflight-verified" {
			reason = "The retained fixture capability check is unavailable or stale; check it again"
		}
		if state.Setup != "" {
			reason = "Setup was already attempted; reconcile its actual effects before cleanup"
		}
	case ReconcileIsolationAction:
		display.Effect = "Reads the exact tenant's current lease and owned resources. Records versions for a fresh cleanup review; does not retry setup or cleanup."
		if state.Setup == "" {
			reason = "There is no attempted isolation setup to reconcile"
		}
	case CleanupIsolationAction:
		phase = "cleanup"
		display.Effect = "Deletes only the exact owned resource versions observed by the reviewed reconciliation, in reverse prerequisite order, then releases the lease. Selected prerequisites stay intact."
		if state.Reconciliation == "" {
			reason = "Reconcile isolation explicitly before reviewing cleanup"
		} else {
			fixed.readReview, err = testisolation.RecoveryReview(prepared, filepath.Join(loaded.root, state.Reconciliation), "read")
			if err == nil {
				fixed.cleanupReview, err = testisolation.RecoveryReview(prepared, filepath.Join(loaded.root, state.Reconciliation), "cleanup")
			}
			if err != nil {
				reason = "The reconciliation is unavailable or already observed no owned lease; reconcile again"
			}
		}
	default:
		return nil, refusal{Failed, "Choose a supported isolation action"}
	}
	resourceNames := map[string]string{}
	versions := map[string]string{}
	for _, resource := range plan.Resources {
		resourceNames[resource.ID], versions[resource.ID] = resource.Name, resource.Version
	}
	if request.Action == CleanupIsolationAction && state.Reconciliation != "" {
		if reconciled, err := testisolation.Open(filepath.Join(loaded.root, state.Reconciliation)); err == nil {
			for _, resource := range reconciled.Resources {
				versions[resource.Alias] = resource.Version
			}
		}
	}
	effects := fixed.readReview.Effects
	if phase == "setup" {
		effects = fixed.setupReview.Effects
	}
	if phase == "cleanup" {
		effects = fixed.cleanupReview.Effects
	}
	if request.Action != PreflightIsolationAction {
		for _, effect := range effects {
			display.Effects = append(display.Effects, IsolationEffectReview{Resource: resourceNames[effect.Alias], Kind: effect.Kind, Operation: effect.Operation, LogicalID: effect.ID, Version: versions[effect.Alias]})
		}
	}
	address := ""
	for _, adapter := range registry.Adapters {
		if adapter.ID == plan.Adapter {
			address = adapter.URL
		}
	}
	if reason == "" && !held && !a.admissionPreview(ctx).Admitted {
		reason = "Execution admission is required for this isolation action"
	}
	bound := &boundAction{action: request.Action, origin: request, isolation: fixed, binding: binding(string(request.Action), inputs, a.reviewer(), a.policyBinding(ctx, true, held), prepared.Identity(), historyIdentity, state.Preflight, state.Setup, state.Reconciliation, state.Cleanup, destination.Name), review: ActionReview{Items: items, Ready: reason == "" && destination.Fresh, Refusal: reason, Isolation: &display, Requirements: requirements, Destination: ReviewDestination{Name: items[0].Name, Address: address, Classification: string(classification), Output: destination.Name}}}
	if !destination.Fresh && bound.review.Refusal == "" {
		bound.review.Refusal = destination.Reason
	}
	return bound, refusal{}
}

type isolationReviewAuthority struct {
	app              *App
	bound            *boundAction
	exact            networkaction.Binding
	actor            networkaction.Actor
	reviewer, policy string
}

func (r isolationReviewAuthority) Check(ctx context.Context, requested networkaction.Binding) (networkaction.Actor, error) {
	refused := errors.New("The isolation action review is no longer authorized")
	if ctx.Err() != nil || r.bound.executionReview == nil || r.bound.isolation == nil || requested != r.exact {
		return networkaction.Actor{}, refused
	}
	lease := r.bound.executionReview
	store := &r.app.reviews
	store.mu.Lock()
	review := store.reviews[lease.token]
	valid := review != nil && !review.withdrawn && review.consumed == lease.intent && store.running == lease.intent && r.app.now().Before(review.expires)
	store.mu.Unlock()
	if !valid || r.app.reviewer() != r.reviewer || r.app.policyBinding(ctx, true, true) != r.policy {
		return networkaction.Actor{}, refused
	}
	guard, _ := r.app.selectedOperation()
	if guard.CheckExecutionContext(ctx) != nil {
		return networkaction.Actor{}, refused
	}
	loaded, items, records, _ := r.app.scoped(ctx, r.bound.origin.Context, r.bound.origin.Items)
	if loaded == nil {
		return networkaction.Actor{}, refused
	}
	_, plan, classification, err := isolationScope(loaded, records[0])
	if err != nil || classification != replay.Nonproduction {
		return networkaction.Actor{}, refused
	}
	_, registry, err := isolationRegistry(plan.RegistryFile)
	if err != nil || isolationInputs(loaded, items[0].Ref, records[0], registry) != r.bound.isolation.inputIdentity {
		return networkaction.Actor{}, refused
	}
	return r.actor, nil
}

func isolationConfirmations(review *IsolationActionReview, confirmed []string) bool {
	if review == nil || len(confirmed) != len(review.Manual) {
		return false
	}
	expected := make([]string, 0, len(review.Manual))
	for _, manual := range review.Manual {
		expected = append(expected, manual.ID)
	}
	actual := slices.Clone(confirmed)
	slices.Sort(actual)
	slices.Sort(expected)
	return slices.Equal(actual, expected)
}

func executeIsolationAction(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	result := ReviewedActionResult{State: Failed, Outcome: ActionRefused}
	fixed, lease := bound.isolation, bound.executionReview
	if fixed == nil || lease == nil {
		result.Reason = "A fresh isolation action review is required"
		return result
	}
	if bound.action == SetupIsolationAction && !isolationConfirmations(bound.review.Isolation, decisions.Confirmed) {
		result.Reason = "Confirm every manual claim exactly once for this setup"
		return result
	}
	actor := networkaction.Actor{Kind: "action-review", ID: "reviewer-" + binding(a.reviewer())[:16], Generation: "review-" + binding(lease.token)[:16], EvidenceIdentity: bound.binding, Expires: lease.expires}
	role := func(review testisolation.Review) networkaction.Authority {
		return isolationReviewAuthority{app: a, bound: bound, exact: review.Binding, actor: actor, reviewer: a.reviewer(), policy: a.policyBinding(ctx, true, true)}
	}
	authorities := testisolation.Authorities{Read: role(fixed.readReview), Setup: role(fixed.setupReview), Cleanup: role(fixed.cleanupReview)}
	state := fixed.state
	historyIdentity := fixed.historyIdentity
	output := filepath.Join(fixed.root, fixed.output)
	// Keep a local intent before a setup can reach a target. It is routing for
	// later offline inspection, never a grant or a replayable confirmation.
	if bound.action == SetupIsolationAction || bound.action == CleanupIsolationAction {
		if bound.action == SetupIsolationAction {
			state.Setup, state.Reconciliation, state.Cleanup = fixed.output, "", ""
			state.Outcome = &IsolationOutcome{Action: bound.action, CheckedAt: catalog.Stamp(a.now()), Setup: "uncertain", Cleanup: "not-started", Manual: []testisolation.ManualClaim{}}
		} else {
			state.Cleanup = fixed.output
			state.Outcome = &IsolationOutcome{Action: bound.action, CheckedAt: catalog.Stamp(a.now()), Setup: "recovered-no-setup", Cleanup: "uncertain", Manual: []testisolation.ManualClaim{}}
		}
		if err := retainIsolationState(fixed.root, state, historyIdentity); err != nil {
			result.Reason = "The isolation intent could not be retained; no resource action was started"
			return result
		}
		historyIdentity = isolationHistoryIdentity(state)
	}
	a.reach(reachingTarget{ref: "environment:" + fixed.ref.ID, name: bound.review.Items[0].Name, destination: bound.review.Destination.Address, kind: ConnectionEnvironment})
	var outcome testisolation.Result
	var err error
	switch bound.action {
	case PreflightIsolationAction:
		_, err = fixed.prepared.Preflight(ctx, authorities, output)
		if err == nil {
			outcome, err = testisolation.Open(output)
		}
		state.Preflight = fixed.output
	case SetupIsolationAction:
		var session *testisolation.Session
		session, err = testisolation.Start(ctx, fixed.prepared, authorities, filepath.Join(fixed.root, state.Preflight), output, testisolation.Confirmation{Plan: fixed.prepared.Identity(), Instance: state.Instance, Steps: slices.Clone(decisions.Confirmed)})
		if session != nil {
			session.Close()
			outcome = session.Result()
		} else {
			state.Setup = ""
		}
	case ReconcileIsolationAction:
		prior := state.Setup
		if state.Cleanup != "" {
			if _, e := testisolation.Inspect(filepath.Join(fixed.root, state.Cleanup)); e == nil {
				prior = state.Cleanup
			}
		}
		outcome, err = testisolation.Reconcile(ctx, fixed.prepared, authorities, filepath.Join(fixed.root, prior), output)
		state.Reconciliation = fixed.output
	case CleanupIsolationAction:
		outcome, err = testisolation.CleanupReconciled(ctx, fixed.prepared, authorities, filepath.Join(fixed.root, state.Reconciliation), output)
		state.Cleanup = fixed.output
	default:
		err = errors.New("Unsupported isolation action")
	}
	if outcome.Schema == "" {
		if inspected, e := testisolation.Inspect(output); e == nil {
			outcome = inspected
		}
	}
	if outcome.Setup == "" {
		outcome.Setup = "unavailable"
	}
	if outcome.Cleanup == "" {
		outcome.Cleanup = "unavailable"
	}
	state.Outcome = &IsolationOutcome{Action: bound.action, CheckedAt: catalog.Stamp(a.now()), Setup: outcome.Setup, Cleanup: outcome.Cleanup, Complete: outcome.Complete, Resources: len(outcome.Resources), Manual: slices.Clone(outcome.Manual)}
	if state.Outcome.Manual == nil {
		state.Outcome.Manual = []testisolation.ManualClaim{}
	}
	result.Isolation = state.Outcome
	if retained := retainIsolationState(fixed.root, state, historyIdentity); retained != nil {
		result.Reason = "The isolation outcome is retained, but its local history could not be published; inspect the operation before another action"
		return result
	}
	if ctx.Err() != nil {
		result.State, result.Outcome, result.Reason = Cancelled, ActionCancelled, "Isolation was stopped; its retained effects and lease require inspection"
		return result
	}
	if err != nil {
		result.Reason = "The isolation action did not complete; inspect retained effects and reconcile before cleanup"
		return result
	}
	result.State, result.Outcome = Completed, ActionCompleted
	return result
}
