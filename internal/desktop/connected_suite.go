package desktop

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
)

const ConnectedSuiteDefinitionSchema = "readmit-suite-definition/v2"

// connectedReportView preserves the semantic report while withholding original
// wire field text until the run is deliberately reopened with Reveal. It never
// changes the private report retained by the lifecycle owner.
func connectedReportView(report runqueue.ConnectedReport, reveal bool) *runqueue.ConnectedReport {
	view := report
	view.Jobs = slices.Clone(report.Jobs)
	fieldView := func(field *assertion.FieldValue) *assertion.FieldValue {
		if field == nil {
			return nil
		}
		value := *field
		if !reveal {
			value.Text = nil
		} else if value.Text != nil {
			text := *value.Text
			value.Text = &text
		}
		return &value
	}
	for j := range view.Jobs {
		if view.Jobs[j].Flow == nil {
			continue
		}
		flow := *view.Jobs[j].Flow
		flow.Phases = slices.Clone(flow.Phases)
		for p := range flow.Phases {
			if flow.Phases[p].Wire == nil {
				continue
			}
			wire := *flow.Phases[p].Wire
			wire.Results = slices.Clone(wire.Results)
			for c := range wire.Results {
				reading := &wire.Results[c].Observed
				reading.Field = fieldView(reading.Field)
				reading.Compared = fieldView(reading.Compared)
			}
			flow.Phases[p].Wire = &wire
		}
		view.Jobs[j].Flow = &flow
	}
	return &view
}

// ConnectedSuiteDraft carries the connected suite language through the same
// named catalog save and revision service as historical SuiteDrafts. It does
// not add an editor, execute a test or confer runner authority.
type ConnectedSuiteDraft struct {
	Document suite.ConnectedDocument `json:"document"`
}

// ConnectedSuiteRunOptions selects installed customer authority; it carries
// references and pins, never a callback, credential or Desktop review token.
type ConnectedSuiteRunOptions struct {
	RunnerConfig      string `json:"runner_config"`
	Authority         string `json:"authority"`
	Promotion         string `json:"promotion"`
	PromotionIdentity string `json:"promotion_identity"`
	Revision          string `json:"revision"`
	Instance          string `json:"instance"`
}

type ConnectedSuitePreflight struct {
	Input        string                      `json:"input"`
	Project      string                      `json:"project"`
	Environment  string                      `json:"environment"`
	Capabilities runnerprotocol.Capabilities `json:"capabilities"`
	Jobs         []ConnectedSuiteJobView     `json:"jobs"`
}

type ConnectedSuiteJobView struct {
	ID           string   `json:"id"`
	State        string   `json:"state"`
	PlanIdentity string   `json:"plan_identity"`
	Input        string   `json:"input"`
	After        []string `json:"after"`
}

type connectedSuiteSelection struct {
	path      string
	prepared  *suite.ConnectedPrepared
	config    customerrunner.Config
	authority string
	promotion string
	expected  string
}

func selectConnectedSuite(root string, raw []byte, environment string, options *ConnectedSuiteRunOptions) (*connectedSuiteSelection, func(), error) {
	path, removeSuite, err := placeCompiled(root, raw)
	if err != nil {
		return nil, func() {}, errors.New("the connected suite cannot be prepared in this project")
	}
	prepared, removePreparation, err := previewConnectedSuite(path, environment)
	if err != nil {
		removeSuite()
		return nil, func() {}, errors.New("the connected suite's pinned plans, releases and selected configurations do not verify")
	}
	remove := func() { removePreparation(); removeSuite() }
	selected := &connectedSuiteSelection{path: path, prepared: prepared}
	var runnerRaw, authorityRaw []byte
	if options != nil {
		if !fullIdentity(options.PromotionIdentity) || !runnerprotocol.ID(options.Instance) || strings.TrimSpace(options.Revision) == "" || len(options.Revision) > 256 || !utf8.ValidString(options.Revision) || strings.ContainsFunc(options.Revision, unicode.IsControl) {
			remove()
			return nil, func() {}, errors.New("select the exact connected promotion, target revision and one dispatch identity")
		}
		files := map[string]string{}
		for _, reference := range []struct{ name, entry string }{{"runner", options.RunnerConfig}, {"authority", options.Authority}, {"promotion", options.Promotion}} {
			resolved, err := artifactpath.File(root, reference.entry)
			if err != nil {
				remove()
				return nil, func() {}, errors.New("connected runner configuration, authority and promotion must be scoped regular artifacts of this project")
			}
			files[reference.name] = resolved
		}
		var declined refusal
		runnerRaw, declined = readPrivateFile(files["runner"], 16384)
		if declined.reason != "" {
			remove()
			return nil, func() {}, errors.New("the customer runner configuration cannot be read privately")
		}
		selected.config, err = customerrunner.ReadConfig(files["runner"])
		if err != nil {
			remove()
			return nil, func() {}, errors.New("the installed customer runner configuration or private jobs root does not verify")
		}
		authorityRaw, declined = readPrivateFile(files["authority"], 64<<10)
		if declined.reason != "" {
			remove()
			return nil, func() {}, errors.New("the connected runner authority cannot be read privately")
		}
		authority, err := customerrunner.DecodeConnectedAuthority(authorityRaw)
		if err != nil {
			remove()
			return nil, func() {}, errors.New("install a valid finite connected runner authority for this suite")
		}
		promotion, err := prepared.VerifyPromotion(files["promotion"], options.PromotionIdentity, environment, options.Revision)
		if err != nil {
			remove()
			return nil, func() {}, errors.New("the connected promotion no longer approves these exact suite inputs")
		}
		project, scope, err := prepared.RuntimeScope()
		capability, capabilityErr := prepared.Capabilities.Identity()
		operations := slices.Clone(authority.Operations)
		slices.Sort(operations)
		now := time.Now()
		if err != nil || capabilityErr != nil || selected.config.Project != project || selected.config.Environment != scope || authority.Input != prepared.Identity || authority.Capabilities != capability || authority.Promotion != promotion.Identity() || authority.IssuedAt.After(now) || !authority.Expires.After(now) || !slices.Equal(operations, prepared.Operations()) {
			remove()
			return nil, func() {}, errors.New("the current runner scope or installed authority differs from this exact promoted suite")
		}
		selected.authority, selected.promotion = files["authority"], files["promotion"]
	}
	identity, err := json.Marshal(struct {
		Suite     string                    `json:"suite"`
		Input     string                    `json:"input"`
		Options   *ConnectedSuiteRunOptions `json:"options"`
		Runner    string                    `json:"runner"`
		Authority string                    `json:"authority"`
	}{suite.Identity(raw), prepared.Identity, options, networkaction.Digest(runnerRaw), networkaction.Digest(authorityRaw)}, json.Deterministic(true))
	if err != nil {
		remove()
		return nil, func() {}, errors.New("the connected suite review identity could not be prepared")
	}
	selected.expected = networkaction.Digest(identity)
	return selected, remove, nil
}

func (a *App) preflightConnectedSuite(ctx context.Context, root string, request RunPreflightRequest, raw []byte) RunPreflightResult {
	document, err := suite.DecodeConnected(raw)
	if err != nil {
		return RunPreflightResult{State: Failed, Reason: "that entry is not a valid connected suite"}
	}
	destination, refused := destinationFor(root, request.Output, "job")
	if refused.state != "" {
		return RunPreflightResult{State: refused.state, Reason: refused.reason}
	}
	view := SuitePreflight{ID: document.ID, Parallelism: document.Parallelism, Environments: []string{}, Jobs: []SuiteJobView{}, Targets: []RunTargetView{}}
	for _, environment := range document.Environments {
		view.Environments = append(view.Environments, environment.ID)
	}
	for _, test := range document.Tests {
		view.Jobs = append(view.Jobs, SuiteJobView{ID: test.ID, Isolation: "shared", After: test.After, Rows: 1})
	}
	preflight := RunPreflight{Kind: "suite", Spec: request.Spec, Name: document.ID, Schema: suite.ConnectedSchema, Identity: suite.Identity(raw),
		Selected: []RunSelected{}, Deadline: operationguard.MaxDuration.String(), Destination: destination, Suite: &view}
	if request.Environment == "" {
		preflight.Admission = RunAdmission{Reason: "select one of the connected suite's environments"}
		return RunPreflightResult{State: Completed, Preflight: &preflight}
	}
	selected, remove, err := selectConnectedSuite(root, raw, request.Environment, request.Connected)
	if err != nil {
		return RunPreflightResult{State: Failed, Reason: err.Error()}
	}
	defer remove()
	project, environment, err := selected.prepared.RuntimeScope()
	if err != nil {
		return RunPreflightResult{State: Failed, Reason: "the connected suite does not declare one customer runner scope"}
	}
	connected := &ConnectedSuitePreflight{Input: selected.prepared.Identity, Project: project, Environment: environment, Capabilities: selected.prepared.Capabilities, Jobs: []ConnectedSuiteJobView{}}
	for _, job := range selected.prepared.Queue.Jobs {
		connected.Jobs = append(connected.Jobs, ConnectedSuiteJobView{ID: job.ID, State: job.State, PlanIdentity: job.PlanIdentity, Input: job.Input, After: slices.Clone(job.After)})
	}
	preflight.Identity, preflight.Connected, view.Environment = selected.expected, connected, request.Environment
	preflight.Engine = RunEnginePin{Engine: selected.prepared.Capabilities.Engine, Spec: suite.ConnectedSchema}
	if request.Connected == nil {
		preflight.Admission = RunAdmission{Reason: "select the customer runner, installed finite authority, exact promotion and dispatch identity"}
	} else {
		preflight.Admission = a.admissionPreview(ctx)
	}
	return RunPreflightResult{State: Completed, Preflight: &preflight}
}

func (a *App) startConnectedSuite(ctx context.Context, root, path, output string, request SuiteRunRequest, raw []byte) SuiteRunResult {
	if request.Connected == nil || request.References != "" {
		return SuiteRunResult{State: Failed, Reason: "a connected suite needs its selected customer runner, finite authority and exact promotion; its releases are already pinned"}
	}
	selected, remove, err := selectConnectedSuite(root, raw, request.Environment, request.Connected)
	if err != nil {
		return SuiteRunResult{State: Failed, Reason: err.Error()}
	}
	defer remove()
	if selected.expected != request.Expected {
		return SuiteRunResult{State: Failed, Reason: "the connected suite or selected runner authority changed after preflight; preflight it again before executing"}
	}
	a.reach(reachingTarget{kind: ConnectionRun, name: selected.prepared.Document.ID, destination: selected.config.Hub})
	a.setRunOutput(output)
	defer a.setRunOutput("")
	options := request.Connected
	report, err := customerrunner.RunConnectedSuite(ctx, selected.config, suite.ConnectedRequest{Path: selected.path, Environment: request.Environment, Output: output,
		Promotion: selected.promotion, PromotionIdentity: options.PromotionIdentity, Revision: options.Revision, Instance: options.Instance}, selected.authority)
	if err != nil && report.Schema == "" {
		return SuiteRunResult{State: Failed, Reason: "the connected customer runner refused execution; verify its current admission, installed authority and retained occurrence before retrying"}
	}
	result := SuiteRunResult{State: Completed, Output: request.Output, ConnectedReport: connectedReportView(report, false)}
	if err != nil {
		result.Reason = "the connected queue stopped before every job completed; inspect the retained output before another execution"
	}
	return result
}

func declaresConnectedSuite(raw []byte) bool {
	var header struct {
		Schema string `json:"schema"`
	}
	return json.Unmarshal(raw, &header) == nil && header.Schema == suite.ConnectedSchema
}

func connectedDraft(document suite.ConnectedDocument) SuiteDraft {
	return normalizedSuite(SuiteDraft{ID: document.ID, Owner: document.Owner, Tags: document.Tags, Concurrency: document.Parallelism,
		Connected: &ConnectedSuiteDraft{Document: document}})
}

func validateConnectedSuiteItem(scope draftScope, draft ItemDraft) ([]catalog.Staged, ItemDraft, []FieldProblem) {
	normalized := normalizedSuite(*draft.Suite)
	projection := ItemDraft{Name: draft.Name, Suite: &normalized}
	refuse := func(reason string) ([]catalog.Staged, ItemDraft, []FieldProblem) {
		return nil, projection, []FieldProblem{{Field: "suite", Problem: reason}}
	}
	if draft.Name == "" {
		return nil, projection, []FieldProblem{{Field: "name", Problem: nameRule}}
	}
	if len(normalized.Tests)+len(normalized.Datasets)+len(normalized.Environments)+len(normalized.Requirements)+len(normalized.Exclusions) != 0 {
		return refuse("a connected suite retains its connected tests and environment bindings; legacy templates and coverage cannot be mixed into it")
	}
	raw, err := json.Marshal(normalized.Connected.Document, json.Deterministic(true))
	if err != nil || len(raw) > suite.MaxBytes {
		return refuse("the connected suite cannot be encoded within its bound")
	}
	var document suite.ConnectedDocument
	if json.Unmarshal(raw, &document, json.RejectUnknownMembers(true)) != nil {
		return refuse("the connected suite declarations cannot be read")
	}
	document.ID = cmp.Or(document.ID, normalized.ID, suiteID(draft.Name))
	document.Owner = cmp.Or(document.Owner, normalized.Owner, unassignedOwner)
	if document.Schema == "" {
		document.Schema = suite.ConnectedSchema
	}
	if document.Parallelism == 0 {
		document.Parallelism = cmp.Or(normalized.Concurrency, 1)
	}
	raw, err = json.Marshal(document, json.Deterministic(true))
	if err != nil {
		return refuse("the connected suite cannot be encoded")
	}
	decoded, err := suite.DecodeConnected(raw)
	if err != nil {
		return refuse("the connected suite does not declare valid pinned tests and environment bindings")
	}
	path, remove, err := placeCompiled(scope.root, raw)
	if err != nil {
		return refuse("the connected suite cannot be prepared in this project")
	}
	defer remove()
	for _, environment := range decoded.Environments {
		prepared, release, err := previewConnectedSuite(path, environment.ID)
		if err != nil {
			return refuse("the connected suite's released expectations, plans or environment configurations do not verify")
		}
		_ = prepared
		release()
	}
	normalized = connectedDraft(decoded)
	projection.Suite = &normalized
	definition, err := encodeMember(suiteDefinition{Schema: ConnectedSuiteDefinitionSchema, Draft: normalized})
	if err != nil || len(definition) > suite.MaxBytes {
		return refuse("the connected suite definition cannot be encoded within its bound")
	}
	return []catalog.Staged{{Role: "suite", File: "suite.json", Data: raw}, {Role: "definition", File: "definition.json", Data: definition}}, projection, nil
}

func previewConnectedSuite(path, environment string) (*suite.ConnectedPrepared, func(), error) {
	directory, err := os.MkdirTemp("", "readmit-desktop-connected-suite-")
	if err != nil {
		return nil, func() {}, err
	}
	remove := func() { _ = os.RemoveAll(directory) }
	prepared, err := suite.PrepareConnected(suite.ConnectedRequest{Path: path, Environment: environment, Output: filepath.Join(directory, "prepared")})
	if err != nil {
		remove()
		return nil, func() {}, err
	}
	return prepared, remove, nil
}

func verifyConnectedSuiteDocument(raw []byte) error {
	_, err := suite.DecodeConnected(raw)
	return err
}

func verifyConnectedDefinition(draft SuiteDraft) error {
	if draft.Connected == nil || len(draft.Tests)+len(draft.Datasets)+len(draft.Environments)+len(draft.Requirements)+len(draft.Exclusions) != 0 {
		return errors.New("invalid connected suite definition")
	}
	raw, err := json.Marshal(draft.Connected.Document, json.Deterministic(true))
	if err != nil {
		return err
	}
	return verifyConnectedSuiteDocument(raw)
}

func (a *App) exportConnectedRunConfiguration(ctx context.Context, loaded *loadedCatalog, version *suiteVersion, request SuiteExportRequest) SuiteExportResult {
	result := SuiteExportResult{Context: request.Context}
	if !slices.ContainsFunc(version.connected.Environments, func(environment suite.ConnectedEnvironment) bool { return environment.ID == request.Environment }) {
		result.refuse(Failed, "choose one of this connected suite's environments")
		return result
	}
	destination, declined := a.chooseDestination(ctx, "Export run configuration")
	if destination == "" {
		result.refuse(declined.state, declined.reason)
		return result
	}
	path, remove, err := placeCompiled(loaded.root, version.data)
	if err != nil {
		result.refuse(Failed, "the connected suite cannot be prepared in this project")
		return result
	}
	defer remove()
	if _, err := suite.PrepareConnected(suite.ConnectedRequest{Path: path, Environment: request.Environment, Output: destination}); err != nil {
		result.refuse(Failed, "the connected suite's reviewed inputs could not be exported completely")
		return result
	}
	result.State, result.Output = Completed, destination
	return result
}

func (a *App) placeScheduledConnectedSuite(ctx context.Context, loaded *loadedCatalog, item catalog.Item, version *suiteVersion, environment string, options ConnectedSuiteRunOptions) (string, refusal) {
	if err := ctx.Err(); err != nil {
		return "", refusal{Cancelled, "the connected schedule was not prepared"}
	}
	selected, remove, err := selectConnectedSuite(loaded.root, version.data, environment, &options)
	if err != nil {
		return "", refusal{Failed, err.Error()}
	}
	defer remove()
	pin := selected.expected
	stableSuite := filepath.Join(loaded.root, compiledSuitePrefix+"scheduled-"+pin+".json")
	if raw, err := (artifactdir.Document{MaxBytes: suite.MaxBytes}).Read(stableSuite); err == nil {
		if !bytes.Equal(raw, version.data) {
			return "", refusal{Failed, "the existing scheduled suite differs from its exact saved bytes"}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", refusal{Failed, "the existing scheduled suite cannot be verified"}
	} else if err := (artifactdir.Document{MaxBytes: suite.MaxBytes}).Create(stableSuite, version.data); err != nil {
		return "", refusal{Failed, "the scheduled suite could not be retained privately"}
	}
	folder := filepath.Join(loaded.root, ".readmit", "scheduled", item.ID+"-"+pin)
	if err := os.MkdirAll(folder, 0700); err != nil {
		return "", refusal{Failed, "the connected schedule descriptor cannot be placed in this project"}
	}
	descriptor := filepath.Join(folder, "dispatch.json")
	request := suite.ConnectedRequest{Path: stableSuite, Environment: environment, Promotion: selected.promotion, PromotionIdentity: options.PromotionIdentity, Revision: options.Revision}
	if existing, err := suite.ReadConnectedDispatch(descriptor); err == nil {
		if existing.Suite != stableSuite || existing.Environment != environment || existing.Authority != selected.authority || existing.Promotion != selected.promotion || existing.PromotionIdentity != options.PromotionIdentity || existing.Revision != options.Revision || existing.Input != selected.prepared.Identity {
			return "", refusal{Failed, "the existing connected schedule descriptor differs from the exact reviewed inputs"}
		}
		return descriptor, refusal{}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", refusal{Failed, "the existing connected schedule descriptor cannot be verified"}
	}
	if _, err := suite.SaveConnectedDispatch(request, selected.authority, descriptor); err != nil {
		return "", refusal{Failed, "the exact promoted connected dispatch could not be retained"}
	}
	return descriptor, refusal{}
}

func bindConnectedRunSuite(a *App, ctx context.Context, request PrepareActionRequest, held bool, loaded *loadedCatalog, version *suiteVersion, data []byte) (*boundAction, refusal) {
	document, err := suite.DecodeConnected(data)
	if err != nil {
		return nil, refusal{Failed, "the connected suite version does not verify"}
	}
	review := &RunReview{Kind: SuiteRunKind, Name: loaded.suiteName(version.item), Version: version.label, Messages: []TestMessage{}, Setup: []RunSetupStep{}, Resets: []ResetReviewAction{},
		Jobs: []RunReviewJob{}, Targets: []RunReviewTarget{}, Environments: []SuiteEnvironmentRef{}}
	if version.original() {
		review.Version = ""
	}
	for _, environment := range document.Environments {
		review.Environments = append(review.Environments, SuiteEnvironmentRef{ID: environment.ID, Name: readableName(environment.ID)})
	}
	chosen := ""
	var options *ConnectedSuiteRunOptions
	if request.Run != nil {
		chosen, options = request.Run.Environment, request.Run.Connected
	}
	if chosen == "" && len(document.Environments) == 1 {
		chosen = document.Environments[0].ID
	}
	destination, declined := destinationFor(loaded.root, "", "job")
	if declined.state != "" {
		return nil, declined
	}
	other, expected := "", suite.Identity(data)
	if chosen == "" {
		other = "choose the environment this connected suite runs at"
	} else {
		selected, remove, err := selectConnectedSuite(loaded.root, data, chosen, options)
		if err != nil {
			return nil, refusal{Failed, err.Error()}
		}
		defer remove()
		expected = selected.expected
		project, environment, err := selected.prepared.RuntimeScope()
		if err != nil {
			return nil, refusal{Failed, "the connected suite does not declare one customer runner scope"}
		}
		review.EnvironmentName = readableName(chosen)
		review.Connected = &ConnectedSuitePreflight{Input: selected.prepared.Identity, Project: project, Environment: environment, Capabilities: selected.prepared.Capabilities, Jobs: []ConnectedSuiteJobView{}}
		for _, job := range selected.prepared.Queue.Jobs {
			review.Connected.Jobs = append(review.Connected.Jobs, ConnectedSuiteJobView{ID: job.ID, State: job.State, PlanIdentity: job.PlanIdentity, Input: job.Input, After: slices.Clone(job.After)})
		}
		if options == nil {
			other = "select the customer runner, installed finite authority, exact promotion and dispatch identity"
		} else {
			review.Address = selected.config.Hub
		}
	}
	for _, test := range document.Tests {
		review.Jobs = append(review.Jobs, RunReviewJob{ID: test.ID, Test: test.ID, Version: test.Revision, Rows: 1, StateSharing: runqueue.SharedState, Target: review.EnvironmentName, DependsOn: slices.Clone(test.After)})
	}
	ready, reason, kind := a.runReady(ctx, held, review.EnvironmentName, []replay.Target{}, destination, other)
	review.Refusal = kind
	run := &runBinding{kind: SuiteRunKind, root: loaded.root, suiteData: slices.Clone(data), identity: expected, environment: chosen, output: destination.Name, name: review.Name,
		source: ItemRef{Kind: SuiteItem, ID: version.item.ID, Revision: version.label}}
	if options != nil {
		copied := *options
		run.connected = &copied
	}
	return &boundAction{action: RunSuiteAction, origin: request, run: run,
		binding: binding(string(RunSuiteAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held), version.item.ID, version.label, expected, chosen, destination.Name),
		review: ActionReview{Items: []CatalogItem{loaded.read(version.item)}, Ready: ready, Refusal: reason, Run: review, Requirements: []ReviewRequirement{},
			Destination: ReviewDestination{Name: review.EnvironmentName, Address: review.Address, Classification: "nonproduction", Output: destination.Name}}}, noRefusal
}

func executeConnectedRunSuite(a *App, ctx context.Context, bound *boundAction, output string) ReviewedActionResult {
	run := bound.run
	selected, remove, err := selectConnectedSuite(run.root, run.suiteData, run.environment, run.connected)
	if err != nil {
		return ReviewedActionResult{State: Failed, Outcome: ActionStale, Reason: "the connected suite or installed authority changed after review; review it again before sending"}
	}
	defer remove()
	if selected.expected != run.identity {
		return ReviewedActionResult{State: Failed, Outcome: ActionStale, Reason: "the connected suite or installed authority changed after review; review it again before sending"}
	}
	options := run.connected
	a.reach(reachingTarget{kind: ConnectionRun, name: run.name, destination: selected.config.Hub})
	report, err := customerrunner.RunConnectedSuite(ctx, selected.config, suite.ConnectedRequest{Path: selected.path, Environment: run.environment, Output: output, Promotion: selected.promotion,
		PromotionIdentity: options.PromotionIdentity, Revision: options.Revision, Instance: options.Instance}, selected.authority)
	if err != nil && report.Schema == "" {
		return ReviewedActionResult{State: Failed, Outcome: ActionRefused, Reason: "the customer runner refused this connected dispatch; inspect its retained occurrence and installed authority before retrying"}
	}
	result := ReviewedActionResult{State: Completed, Outcome: ActionCompleted, ConnectedReport: connectedReportView(report, false)}
	for _, job := range report.Jobs {
		if job.Flow != nil && job.Flow.State == "uncertain" {
			result.Outcome = ActionUncertain
		}
	}
	if err != nil {
		result.Reason = "the connected suite stopped before every job completed; inspect the retained execution"
	}
	return result
}

func readConnectedSuiteRun(c *loadedCatalog, item catalog.Item, path string) (view, error) {
	execution, err := suite.OpenConnectedExecution(context.Background(), path)
	if err != nil {
		return view{}, errors.New("the connected suite execution and its linked proof do not verify")
	}
	summary := &RunSummary{Kind: SuiteRunKind, Jobs: len(execution.Report.Jobs), Entry: item.Entry, EnvironmentName: execution.Preparation.Environment, Boundary: "application-state"}
	switch execution.Report.ExitCode() {
	case 0:
		summary.Result, summary.Outcome = RunPassed, "passed"
	case 1:
		summary.Result, summary.Outcome = RunFailed, "failed"
	default:
		summary.Result, summary.Outcome = RunIncomplete, "incomplete"
	}
	var started, completed time.Time
	for _, job := range execution.Report.Jobs {
		if job.Flow == nil {
			continue
		}
		if started.IsZero() || job.Flow.StartedAt.Before(started) {
			started = job.Flow.StartedAt
		}
		if job.Flow.CompletedAt.After(completed) {
			completed = job.Flow.CompletedAt
		}
		for _, phase := range job.Flow.Phases {
			for _, step := range phase.Steps {
				if step.Uncertain {
					summary.Uncertain++
				}
			}
		}
	}
	summary.StartedAt, summary.CompletedAt, summary.DeliveryUncertain = stampedTime(started), stampedTime(completed), summary.Uncertain > 0
	name := execution.Document.ID
	if raw, err := json.Marshal(execution.Document, json.Deterministic(true)); err == nil {
		if origin, held, err := c.recordedRunOrigin(path, digestOf(raw)); err == nil && held && origin.Source.Kind == SuiteItem {
			ref := origin.Source
			summary.Suite, summary.Version, name = &ref, ref.Revision, origin.Name
		} else {
			for _, held := range c.suites() {
				for revision, fingerprint := range held.fingerprints {
					if fingerprint == suite.Identity(raw) {
						ref := ItemRef{Kind: SuiteItem, ID: held.item.ID, Revision: revision}
						summary.Suite, summary.Version = &ref, revision
					}
				}
			}
		}
	}
	if c.executing != nil && c.executing(path) {
		summary.Active, summary.Result = true, RunRunning
	}
	read := view{name: name, summary: ItemSummary{Run: summary}}
	if !started.IsZero() {
		read.createdAt = &started
	}
	return read, nil
}
