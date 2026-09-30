package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testauthor"
)

// One editor Save validates and publishes one logical revision. An object
// the application saves is published whole through internal/catalog: every
// member is validated first, staged as a new file of the project, read back
// through its own reader, and only then named current. Nothing here writes a
// file an older panel wrote, and nothing is reported saved until the whole
// revision is.

// savedKinds are the kinds this release saves whole. Each one's editor lives
// with its screen; the guarantees are these.
var savedKinds = []ItemKind{EnvironmentItem, TestItem, ObservationItem, CaseItem, ProjectItem, AnalysisSettingsItem, FindingReviewItem, VariantItem, ProfileItem, CheckGroupItem, ScenarioItem, LinkRulesItem, CoverageItem, MappingItem, SourceItem, SuiteItem, ReportItem, NormalizationPolicyItem, RunnerItem}

// savedKindsRule is the refusal of a kind this release does not save.
const savedKindsRule = "this release saves environments, tests, observations, case details, project settings, analysis settings, finding reviews, variants, profiles, check groups, scenarios, link rules, coverage, mapping presets, capture sources, suites, reports, normalization policies and runners whole"

// documentKinds are the saved kinds whose state the project document holds
// rather than a revision the catalog publishes.
var documentKinds = []ItemKind{CaseItem, ProjectItem}

// ItemDraft is the whole of one object as its editor holds it: the name a
// person gives it and the one member of its kind. Environment is a target
// configuration, Test a test draft answered against its source case, and
// Observation a source together with the window it is collected through.
//
// Case and Project are the details of a case and the settings of a project,
// which the project document holds; each carries its own name.
//
// An environment may also carry the send policy its destinations are decided
// under, its reset plan and its links; every member it carries is published
// in the same revision as its target, or none is.
//
// A test may carry its links — the environment it runs against, how its
// reset is decided, its tags and where it came from — beside its draft. A
// test the editor cannot represent whole is saved from TestDocument, the
// exact readmit-test/v1 bytes, instead of Test.
type ItemDraft struct {
	Name         string                `json:"name,omitzero"`
	Environment  *replay.Target        `json:"environment,omitzero"`
	FHIR         *FHIRConnection       `json:"fhir,omitzero"`
	Isolation    *EnvironmentIsolation `json:"isolation,omitzero"`
	SendPolicy   *sendpolicy.Policy    `json:"policy,omitzero"`
	ResetPlan    *fixturereset.Plan    `json:"reset,omitzero"`
	Links        *EnvironmentLinks     `json:"links,omitzero"`
	Test         *testauthor.Draft     `json:"test,omitzero"`
	TestLinks    *TestLinks            `json:"test_links,omitzero"`
	TestDocument string                `json:"test_document,omitzero"`
	Observation  *ObservationDraft     `json:"observation,omitzero"`
	Case         *CaseDraft            `json:"case,omitzero"`
	Project      *ProjectDraft         `json:"project,omitzero"`
	// AnalysisSettings is a named diagnosis configuration, saved as the
	// readmit-diagnose-config/v1 document `readmit diagnose --config` reads.
	AnalysisSettings *diagnose.Config `json:"analysis_settings,omitzero"`
	// FindingReview is the decisions a person made about one analysis,
	// saved as the readmit-finding-decisions/v1 document `readmit diagnose
	// review` reads.
	FindingReview *FindingReviewDraft `json:"finding_review,omitzero"`
	// Variant is a new case derived from a registered one by a reproducer
	// plan, saved with its lineage and project association.
	Variant *VariantDraft `json:"variant,omitzero"`
	// Profile is a local interface profile, saved with the seal of its
	// version and its origin in one revision.
	Profile *ProfileDraft `json:"profile,omitzero"`
	// CheckGroup is a reusable check group, saved as the
	// readmit-assertion-set/v1 document the command line evaluates.
	CheckGroup *CheckGroupDraft `json:"check_group,omitzero"`
	// Scenario is a synthetic generator plan, saved as the
	// readmit-scenario-generator/v1 document `readmit scenario generate` reads.
	Scenario  *ScenarioDraft   `json:"scenario,omitzero"`
	LinkRules *correlate.Rules `json:"link_rules,omitzero"`
	Coverage  *CoverageDraft   `json:"coverage,omitzero"`
	// Mapping is a saved mapping preset, and Source a capture source whose
	// members are published together.
	Mapping *importer.Recipe    `json:"mapping,omitzero"`
	Source  *CaptureSourceDraft `json:"source,omitzero"`
	// Suite is a whole suite, published as one version.
	Suite *SuiteDraft `json:"suite,omitzero"`
	// NormalizationPolicy is a named normalization policy, saved as the
	// readmit-normalization-policy/v1 document `readmit normalize` reads.
	NormalizationPolicy *diff.Policy `json:"normalization_policy,omitzero"`
	// Report is a report made from runs: created with its retained packet,
	// and edited as its title and notes.
	Report *ReportDraft `json:"report,omitzero"`
	// Runner is a named runner: its readmit-runner/v1 configuration and the
	// project environment it is assigned to.
	Runner *RunnerDraft `json:"runner,omitzero"`
}

// ObservationDraft is an observation source and its window, which only mean
// something together and are published together or not at all. Credential
// names the project credential reference an HTTPS or database source
// presents, empty for none: a save resolves it into the source, and a draft
// opened from a saved observation never carries the reference's arguments.
type ObservationDraft struct {
	Connected  *ConnectedObservation `json:"connected,omitzero"`
	Source     observesource.Source  `json:"source,omitzero"`
	Window     observewindow.Window  `json:"window,omitzero"`
	Credential string                `json:"credential,omitzero"`
}

// FieldProblem is one reason a draft cannot be saved, at the member it is
// about.
// Referring names the objects that hold the member where it is, such as the
// cases still assigned to a revision a project save would remove.
type FieldProblem struct {
	Field     string     `json:"field"`
	Problem   string     `json:"problem"`
	Referring []Referrer `json:"referring,omitzero"`
}

// Referrer is one object a problem names, by reference and by its name.
type Referrer struct {
	Ref  ItemRef `json:"ref"`
	Name string  `json:"name"`
}

// DraftRequest is one draft to validate. Item names the object an edit
// began from, empty for a new object, so an environment's name is decided as
// its save would decide it.
type DraftRequest struct {
	Context RequestContext `json:"context"`
	Kind    ItemKind       `json:"kind"`
	Item    string         `json:"item,omitzero"`
	Draft   ItemDraft      `json:"draft"`
}

// DraftValidation is what validating a draft found: every problem, or the
// normalized draft a save would publish.
type DraftValidation struct {
	State      State          `json:"state"`
	Reason     string         `json:"reason,omitzero"`
	Context    RequestContext `json:"context"`
	Problems   []FieldProblem `json:"problems"`
	Projection *ItemDraft     `json:"projection,omitzero"`
}

func (r *DraftValidation) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ValidateDraft validates a whole draft through the readers a save stages it
// through, and writes nothing. Validation is also the first step of every
// save, so a window never needs to ask for it before saving.
func (a *App) ValidateDraft(request DraftRequest) DraftValidation {
	return runRead(a, false, func(ctx context.Context) DraftValidation {
		result := DraftValidation{Context: request.Context, Problems: []FieldProblem{}}
		if slices.Contains(documentKinds, request.Kind) {
			return a.validateDocumentDraft(ctx, request)
		}
		scope := draftScope{item: request.Item}
		if request.Kind == EnvironmentItem || request.Kind == TestItem || request.Kind == FindingReviewItem || request.Kind == VariantItem || request.Kind == ProfileItem || request.Kind == ScenarioItem || request.Kind == CoverageItem || request.Kind == SourceItem || request.Kind == SuiteItem || request.Kind == ReportItem || request.Kind == RunnerItem {
			loaded, declined := a.loadCatalog(ctx, request.Context, false)
			if loaded == nil {
				result.refuse(declined.state, declined.reason)
				return result
			}
			scope.root, scope.loaded = loaded.root, loaded
		} else {
			root, declined := a.projectRoot(ctx, request.Context)
			if root == "" {
				result.refuse(declined.state, declined.reason)
				return result
			}
			scope.root = root
		}
		staged, projection, problems := validateItemDraft(scope, request.Kind, request.Draft)
		result.State, result.Problems = Completed, problems
		if staged != nil {
			result.Projection = projection
		}
		return result
	})
}

// SaveOutcome is what one save did.
type SaveOutcome string

const (
	// SavedOutcome: the whole revision is published and current.
	SavedOutcome SaveOutcome = "saved"
	// InvalidOutcome: the draft has field problems; nothing was written.
	InvalidOutcome SaveOutcome = "invalid"
	// ConflictOutcome: the object changed since the edit began; nothing was
	// published and the draft is the window's to keep.
	ConflictOutcome SaveOutcome = "conflict"
	// FailedOutcome: the save did not publish. When Operation is set, the work
	// it staged is recoverable under that operation; the previous revision
	// is current.
	FailedOutcome SaveOutcome = "failed"
)

// SaveItemRequest is one deliberate save of a whole draft. Item is empty to
// create an object, and to save a copy of one under a new identity.
// BaseRevision is the revision the edit began from. IntentID is allocated
// once when the person submits and reused for every retry of that submission.
type SaveItemRequest struct {
	Context      RequestContext `json:"context"`
	Kind         ItemKind       `json:"kind"`
	Item         string         `json:"item,omitzero"`
	BaseRevision string         `json:"base_revision,omitzero"`
	Draft        ItemDraft      `json:"draft"`
	IntentID     string         `json:"intent_id"`
}

// SaveItemResult answers one save. Saved names the published revision; the
// window refreshes its lists afterwards, and a refresh that fails does not
// unsay a save.
type SaveItemResult struct {
	State           State          `json:"state"`
	Reason          string         `json:"reason,omitzero"`
	Context         RequestContext `json:"context"`
	Outcome         SaveOutcome    `json:"outcome"`
	Saved           *ItemRef       `json:"saved,omitzero"`
	Replayed        bool           `json:"replayed"`
	Projection      *ItemDraft     `json:"projection,omitzero"`
	Problems        []FieldProblem `json:"problems"`
	CurrentRevision string         `json:"current_revision,omitzero"`
	Operation       string         `json:"operation,omitzero"`
}

func (r *SaveItemResult) refuse(state State, reason string) {
	r.State, r.Reason, r.Outcome = state, reason, FailedOutcome
	// A refusal before any validation, busy included, still answers the
	// list of problems it declares, empty.
	if r.Problems == nil {
		r.Problems = []FieldProblem{}
	}
}

// SaveItem publishes one whole draft as one revision, or nothing. The draft
// is validated first; a base that is not current is a conflict; a submission
// already published is answered with its revision and written nowhere again;
// and a different submission under the same identity is refused. Saving is
// authoring, admitted as authoring, except the guided sample's own test, which
// the sample walks through without a license.
func (a *App) SaveItem(request SaveItemRequest) SaveItemResult {
	return run(a, false, false, func(ctx context.Context) SaveItemResult {
		return a.saveItem(ctx, request)
	})
}

// saveItem is one save inside a slot its caller already holds.
func (a *App) saveItem(ctx context.Context, request SaveItemRequest) SaveItemResult {
	result := SaveItemResult{Context: request.Context, Problems: []FieldProblem{}}
	if !slices.Contains(savedKinds, request.Kind) {
		result.refuse(Failed, savedKindsRule)
		return result
	}
	if !a.demoSave(request) {
		if err := a.admitAuthor(); err != nil {
			result.refuse(PermissionDenied, err.Error())
			return result
		}
	}
	if slices.Contains(documentKinds, request.Kind) {
		return a.saveDocumentItem(ctx, request)
	}
	// Recording the catalog first settles interrupted saves and records
	// the object this save edits, if it was only discovered so far.
	loaded, declined := a.loadCatalog(ctx, request.Context, true)
	if loaded == nil {
		result.refuse(declined.state, declined.reason)
		return result
	}
	root, store := loaded.root, loaded.store
	scope := draftScope{root: root, loaded: loaded, item: request.Item, intent: request.IntentID}
	staged, projection, problems := validateItemDraft(scope, request.Kind, request.Draft)
	if staged == nil {
		result.State, result.Outcome, result.Problems = Failed, InvalidOutcome, problems
		result.Reason = "the draft has problems to fix; nothing was saved"
		return result
	}
	var entry *catalog.Entry
	if request.Kind == VariantItem {
		// The derived case is published beside the plans it was built by.
		resolved, problems := resolveVariant(scope, *projection.Variant, false)
		if problems != nil {
			result.State, result.Outcome, result.Problems = Failed, InvalidOutcome, problems
			result.Reason = "the draft has problems to fix; nothing was saved"
			return result
		}
		entry = variantEntry(resolved)
	}
	if request.Kind == ReportItem && request.Item == "" {
		// A new report is published with the retained packet of its runs.
		built, problem := reportEntry(ctx, scope, projection.Report)
		if problem != nil {
			result.State, result.Outcome = Failed, InvalidOutcome
			result.Problems = []FieldProblem{*problem}
			result.Reason = "the draft has problems to fix; nothing was saved"
			return result
		}
		entry = built
	}
	saved, err := store.Save(catalog.Draft{
		Kind: string(request.Kind), ItemID: request.Item, Name: projection.Name, Base: request.BaseRevision,
		Intent: request.IntentID, Digest: submissionDigest(request, staged), Author: a.reviewerName(), Members: staged, Entry: entry,
	}, verifierFor(request.Kind), catalog.Options{Now: a.now, Fault: a.saveFault, Associate: associateEntry(root)})
	var conflict *catalog.Conflict
	switch {
	case errors.As(err, &conflict):
		result.State, result.Outcome, result.CurrentRevision = Failed, ConflictOutcome, conflict.Current
		result.Reason = "the object changed since this edit began; nothing was saved and the draft is kept"
		return result
	case errors.Is(err, catalog.ErrIntentReused):
		result.refuse(Failed, "this submission was already used for different content; nothing was saved")
		return result
	case errors.Is(err, catalog.ErrTooManyPending):
		result.refuse(Failed, "this project holds as many interrupted saves as it keeps; retry or discard one first. Nothing was saved")
		return result
	case errors.Is(err, catalog.ErrNoItem):
		result.refuse(Failed, "the project holds no such object")
		return result
	case err != nil:
		result.refuse(Failed, "the save did not complete; the previous revision is still current")
		if store.Pending(request.IntentID) {
			result.Operation = request.IntentID
		}
		return result
	}
	result.State, result.Outcome, result.Replayed, result.Projection = Completed, SavedOutcome, saved.Replayed, projection
	result.Saved = &ItemRef{Kind: request.Kind, ID: saved.Item.ID, Revision: revisionLabel(saved.Revision)}
	if request.Kind == ProfileItem && slices.ContainsFunc(staged, func(member catalog.Staged) bool { return member.Role == packRole }) {
		// The profile carries its pack: the pin names the profile itself.
		self := *result.Saved
		projection.Profile.Pack = &self
	}
	return result
}

// IncompleteSaveRequest names one save an interruption left unpublished.
type IncompleteSaveRequest struct {
	Context   RequestContext `json:"context"`
	Operation string         `json:"operation"`
}

// DiscardIncompleteSave drops one unpublished save: the files it staged,
// which no revision names, and its pending record. The current revision is
// not touched.
func (a *App) DiscardIncompleteSave(request IncompleteSaveRequest) CatalogResult {
	return run(a, false, true, func(ctx context.Context) CatalogResult {
		result := CatalogResult{Context: request.Context}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		store, err := catalog.Open(root)
		if err == nil {
			err = store.Discard(request.Operation)
		}
		if errors.Is(err, catalog.ErrEntryPublished) {
			result.refuse(Failed, "this save already placed its case in the project; retry it to register the case, which nothing else does")
			return result
		}
		if err != nil {
			result.refuse(Failed, "no incomplete save of this project is held under that operation")
			return result
		}
		result.State = Completed
		return result
	})
}

// projectRoot resolves the project a request names and refuses a folder that
// now holds a different project than the window opened.
func (a *App) projectRoot(ctx context.Context, request RequestContext) (string, refusal) {
	opened, declined := openProjectFolder(request.Project)
	if opened == nil {
		return "", declined
	}
	if request.ProjectID != "" {
		store, err := catalog.Open(opened.Root)
		if err != nil {
			return "", refusal{Failed, "a project must be an existing folder that is not a symbolic link"}
		}
		document, present, err := store.Read()
		if err != nil {
			return "", refusal{Failed, "the project's catalog cannot be read; it is left exactly as written"}
		}
		if !present || document.Project.ID != request.ProjectID {
			return "", refusal{Failed, errForeignProject.Error()}
		}
	}
	return opened.Root, refusal{}
}

// submissionDigest is the digest of everything a submission asks for, so the
// same submission is recognized and another under the same identity is not.
func submissionDigest(request SaveItemRequest, staged []catalog.Staged) string {
	digest := sha256.New()
	parts := []string{string(request.Kind), request.Item, request.BaseRevision, request.Draft.Name}
	if variant := request.Draft.Variant; variant != nil {
		parts = append(parts, string(variant.Source.Kind)+":"+variant.Source.ID)
	}
	for _, part := range parts {
		digest.Write([]byte(part + "\x00"))
	}
	for _, member := range staged {
		sum := sha256.Sum256(member.Data)
		digest.Write([]byte(member.Role + "\x00" + hex.EncodeToString(sum[:]) + "\x00"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// draftScope is what validating a draft is decided against: the project
// folder, its catalog when the kind needs it, the object an edit began from
// and the click that submits it.
type draftScope struct {
	root   string
	loaded *loadedCatalog
	item   string
	intent string
}

// validateItemDraft validates a whole draft of kind and answers the files a save
// stages and the normalized draft, or every problem found.
func validateItemDraft(scope draftScope, kind ItemKind, draft ItemDraft) ([]catalog.Staged, *ItemDraft, []FieldProblem) {
	problems := []FieldProblem{}
	if draft.Name != "" && !catalog.ValidName(draft.Name) {
		problems = append(problems, FieldProblem{Field: "name", Problem: nameRule})
	}
	if kind != EnvironmentItem && (draft.SendPolicy != nil || draft.ResetPlan != nil || draft.Links != nil) {
		problems = append(problems, FieldProblem{Field: "kind", Problem: "only an environment carries a send policy, a reset plan or links"})
	}
	if kind != EnvironmentItem && (draft.FHIR != nil || draft.Isolation != nil) {
		problems = append(problems, FieldProblem{Field: "kind", Problem: "Only an environment carries protocol connections or isolation"})
	}
	if kind != TestItem && (draft.TestLinks != nil || draft.TestDocument != "") {
		problems = append(problems, FieldProblem{Field: "kind", Problem: "only a test carries test links or a test document"})
	}
	var staged []catalog.Staged
	normalized := ItemDraft{Name: draft.Name}
	switch kind {
	case EnvironmentItem:
		if draft.FHIR != nil {
			members, environment, found := validateFHIRConnectionDraft(scope, draft)
			problems = append(problems, found...)
			staged, normalized = members, environment
			break
		}
		if draft.Environment == nil {
			return nil, nil, append(problems, FieldProblem{Field: "environment", Problem: "an environment is a target configuration"})
		}
		members, environment, found := validateEnvironmentDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized = members, environment
		}
	case TestItem:
		members, test, found := validateTestDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized = members, test
		}
	case ObservationItem:
		if draft.Observation == nil {
			return nil, nil, append(problems, FieldProblem{Field: "observation", Problem: "an observation is a source and its window"})
		}
		members, observation, found := validateObservationDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.Observation = members, observation
		}
	case AnalysisSettingsItem:
		if draft.AnalysisSettings == nil {
			return nil, nil, append(problems, FieldProblem{Field: "analysis_settings", Problem: "analysis settings are one diagnosis configuration"})
		}
		members, config, found := validateAnalysisSettings(*draft.AnalysisSettings)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.AnalysisSettings = members, config
		}
	case RunnerItem:
		members, runner, found := validateRunnerDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.Runner = members, runner
		}
	case CheckGroupItem:
		if draft.CheckGroup == nil {
			return nil, nil, append(problems, FieldProblem{Field: "check_group", Problem: "a check group is a named set of checks"})
		}
		members, group, found := validateCheckGroupDraft(draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.CheckGroup = members, group
		}
	case ProfileItem:
		if draft.Profile == nil {
			return nil, nil, append(problems, FieldProblem{Field: "profile", Problem: "a profile is a local interface profile"})
		}
		members, profile, found := validateProfileDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.Profile = members, profile
		}
	case VariantItem:
		members, variant, found := validateVariantDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized = members, variant
		}
	case FindingReviewItem:
		if draft.FindingReview == nil {
			return nil, nil, append(problems, FieldProblem{Field: "finding_review", Problem: "a finding review is the decisions made about one analysis"})
		}
		members, review, found := validateFindingReview(scope, *draft.FindingReview)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.FindingReview = members, review
		}
	case ScenarioItem:
		if draft.Scenario == nil {
			return nil, nil, append(problems, FieldProblem{Field: "scenario", Problem: "a scenario is a synthetic generator plan"})
		}
		members, plan, found := validateScenarioDraft(draft, scope.nextRevision())
		problems = append(problems, found...)
		if len(found) == 0 && draft.Scenario.Profile != nil {
			// The local profile the scenario is authored for is kept beside
			// the plan, and must cover the family of its workflow.
			if problem := scope.scenarioProfile(*draft.Scenario.Profile, plan.Plan); problem != nil {
				problems = append(problems, *problem)
			} else if metadata, err := metadataMember(libraryMetadata{Profile: draft.Scenario.Profile}); err != nil {
				problems = append(problems, FieldProblem{Field: "scenario.profile", Problem: err.Error()})
			} else {
				ref := *draft.Scenario.Profile
				members, plan.Profile = append(members, metadata...), &ref
			}
		}
		if len(problems) == 0 {
			staged, normalized.Scenario = members, plan
		}
	case LinkRulesItem:
		members, rules, found := validateLinkRulesDraft(draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.LinkRules = members, rules
		}
	case NormalizationPolicyItem:
		members, policy, found := validateNormalizationPolicyDraft(draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.NormalizationPolicy = members, policy
		}
	case CoverageItem:
		members, coverage, found := validateCoverageDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.Coverage = members, coverage
		}
	case MappingItem:
		if draft.Mapping == nil {
			return nil, nil, append(problems, FieldProblem{Field: "mapping", Problem: "a mapping preset is a mapping recipe"})
		}
		members, found := validateMapping(draft.Mapping)
		problems = append(problems, found...)
		if len(found) == 0 {
			recipe := *draft.Mapping
			if recipe.Schema == "" {
				recipe.Schema = importer.RecipeSchema
			}
			staged, normalized.Mapping = members, &recipe
		}
	case SourceItem:
		if draft.Source == nil {
			return nil, nil, append(problems, FieldProblem{Field: "source", Problem: "a capture source declares its type and settings"})
		}
		members, source, found := validateCaptureSource(scope, draft.Source)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized.Source = members, source
		}
	case ReportItem:
		members, reportDraft, found := validateReportDraft(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized = members, reportDraft
		}
	case SuiteItem:
		if draft.Suite == nil {
			return nil, nil, append(problems, FieldProblem{Field: "suite", Problem: "a suite is its tests, data, environments and coverage"})
		}
		members, suiteDraft, found := validateSuiteItem(scope, draft)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged, normalized = members, suiteDraft
		}
	default:
		return nil, nil, append(problems, FieldProblem{Field: "kind", Problem: savedKindsRule})
	}
	if kind == EnvironmentItem && draft.Isolation != nil && scope.loaded != nil {
		classification := replay.Unclassified
		if normalized.FHIR != nil {
			classification = normalized.FHIR.Classification
		} else if normalized.Environment != nil {
			classification = normalized.Environment.Classification
		}
		members, isolation, found := validateIsolationDraft(scope.loaded.document.Project.ID, *draft.Isolation, classification, normalized.SendPolicy)
		problems = append(problems, found...)
		if len(found) == 0 {
			staged = append(staged, members...)
			normalized.Isolation = &isolation
		}
		if draft.ResetPlan != nil && len(draft.ResetPlan.Actions) > 0 {
			problems = append(problems, FieldProblem{Field: "isolation", Problem: "Choose check-only reset or typed isolation explicitly; the saved check-only plan is never changed into mutations"})
		}
	}
	if len(problems) > 0 {
		return nil, nil, problems
	}
	return staged, &normalized, problems
}

// declaredTarget is a target configuration as the shared writer writes it:
// declared as a test endpoint, validated by the shared reader before a byte
// is kept, and encoded deterministically. References it declares stay as
// declared; they are resolved from the project folder when the saved file is
// read in place.
func declaredTarget(target replay.Target) (replay.Target, []byte, error) {
	target.TestEndpoint = true
	data, err := json.Marshal(target, json.Deterministic(true))
	if err != nil {
		return replay.Target{}, nil, errors.New("the target configuration cannot be encoded")
	}
	data = append(data, '\n')
	folder, err := os.MkdirTemp("", "readmit-validate-")
	if err != nil {
		return replay.Target{}, nil, errors.New("the target configuration cannot be validated")
	}
	defer os.RemoveAll(folder)
	path := filepath.Join(folder, "target.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return replay.Target{}, nil, errors.New("the target configuration cannot be validated")
	}
	declared, err := replay.ReadDeclaredRecordedTarget(path)
	if err != nil {
		return replay.Target{}, nil, err
	}
	return declared, data, nil
}

// verifierFor reads a staged revision of kind through the readers every
// other panel and the command line read the same documents with, in place.
func verifierFor(kind ItemKind) catalog.Verifier {
	return func(files map[string]string) error {
		switch kind {
		case EnvironmentItem:
			if err := verifyEnvironment(files); err != nil {
				return err
			}
			return verifyEnvironmentIsolation(files)
		case TestItem:
			return verifyTest(files)
		case ObservationItem:
			return verifyObservation(files)
		case AnalysisSettingsItem:
			_, err := readAnalysisSettingsFile(files["config"])
			return err
		case FindingReviewItem:
			_, _, err := readDecisionsFile(files["decisions"])
			return err
		case VariantItem:
			return verifyVariant(files)
		case ProfileItem:
			return verifyProfile(files)
		case CheckGroupItem:
			return verifyCheckGroup(files)
		case ScenarioItem:
			return verifyScenario(files)
		case LinkRulesItem:
			_, err := readRulesFile(files[string(LinkRulesItem)])
			return err
		case CoverageItem:
			_, err := readCoverageFile(files[string(CoverageItem)])
			return err
		case NormalizationPolicyItem:
			_, err := readPolicyFile(files[string(NormalizationPolicyItem)])
			return err
		case LinkReviewItem:
			_, err := readReviewFile(files[string(LinkReviewItem)])
			return err
		case MappingItem:
			_, err := readMapping(files["recipe"])
			return err
		case SourceItem:
			return verifyCaptureSource(files)
		case SuiteItem:
			return verifySuite(files)
		case RunnerItem:
			return verifyRunner(files)
		case SuiteApprovalItem:
			_, err := readSuiteApproval(files[primaryRole(SuiteApprovalItem)])
			return err
		case ReportItem:
			return verifyReport(files)
		case ReportReviewItem:
			_, err := readReportApprovalFile(files[string(ReportReviewItem)])
			return err
		case ReportShareItem:
			data, err := boundedFile(files[string(ReportShareItem)], catalog.MaxMemberBytes)
			if err != nil {
				return err
			}
			_, err = decodeReportShare(data)
			return err
		}
		return errors.New("this release does not save this kind of object")
	}
}
