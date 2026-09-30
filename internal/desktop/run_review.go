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

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The reviewed run (#555). Running a saved test, a suite version, the
// never-attempted rest of an interrupted run or a reviewed test derived from
// an export review is one reviewed action each, prepared from the saved
// objects and executed only by its own Send: the review binds the exact
// version, the named environment's current target, its send policy and reset
// plan, the manual setup the person marks complete, the admission and the
// fresh output the application names, and a change to any of them refuses the
// Send as stale. Nothing here opens a connection before Send.

const (
	// RunTestAction runs one saved test version against a named environment.
	RunTestAction ActionID = "run.test"
	// RunSuiteAction runs one saved suite version at one of its environments.
	RunSuiteAction ActionID = "run.suite"
	// ResumeRunAction sends the never-attempted rest of an interrupted run.
	ResumeRunAction ActionID = "run.resume"
	// RunReviewedTestAction runs a test rebound to an approved export
	// review's derived case against the target the original run reached.
	RunReviewedTestAction ActionID = "run.reviewed-test"
)

// SetupRequirement: every manual setup step a run review lists is marked
// complete, by its identity, in the final click.
const SetupRequirement ReviewRequirement = "setup"

// RunActionOptions are what a run review is asked for beyond the objects it
// is scoped to: the suite environment a suite version runs at, and the
// original phase a reviewed test reproduces (failure or pass).
type RunActionOptions struct {
	Environment string `json:"environment,omitzero"`
	Phase       string `json:"phase,omitzero"`
}

// RunRefusal says what a person changes to make a run review ready: the
// environment (its classification or its transport), or the license.
type RunRefusal string

const (
	EnvironmentRefusal RunRefusal = "environment"
	LicenseRefusal     RunRefusal = "license"
)

// RunReview is what one run will do, as its review shows it: the test or
// suite by its name and exact version, the named environment and the address
// it reaches, the messages it sends in order, the manual setup to mark
// complete, the reset actions that run before any message is sent, a suite's
// jobs and targets, and what a resume repeats or a reviewed test reproduces.
// MessageCount is the number of messages sent when it is known, and absent
// when it is not.
type RunReview struct {
	Kind            RunKind               `json:"kind"`
	Name            string                `json:"name"`
	Version         string                `json:"version,omitzero"`
	Test            *ItemRef              `json:"test,omitzero"`
	Environment     *ItemRef              `json:"environment,omitzero"`
	EnvironmentName string                `json:"environment_name,omitzero"`
	Site            string                `json:"site,omitzero"`
	Address         string                `json:"address,omitzero"`
	Messages        []TestMessage         `json:"messages"`
	MessageCount    *int                  `json:"message_count"`
	Setup           []RunSetupStep        `json:"setup"`
	Resets          []ResetReviewAction   `json:"resets"`
	Jobs            []RunReviewJob        `json:"jobs"`
	Targets         []RunReviewTarget     `json:"targets"`
	Environments    []SuiteEnvironmentRef `json:"environments"`
	Resume          *RunResumeReview      `json:"resume,omitzero"`
	Reviewed        *RunReviewedTest      `json:"reviewed,omitzero"`
	Refusal         RunRefusal            `json:"refusal,omitzero"`
}

// RunSetupStep is one manual step a run needs before it sends: its name and
// its exact instructions. Marking it complete is scoped to this review.
type RunSetupStep struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
}

// RunReviewJob is one test of a suite run: its test at its version, the
// dataset its rows come from, the named environment it reaches, whether it
// shares state with other jobs, and the suite tests it waits for.
type RunReviewJob struct {
	ID           string             `json:"id"`
	Test         string             `json:"test"`
	Version      string             `json:"version,omitzero"`
	Dataset      string             `json:"dataset,omitzero"`
	Rows         int                `json:"rows"`
	Target       string             `json:"target,omitzero"`
	StateSharing runqueue.Isolation `json:"state_sharing"`
	DependsOn    []string           `json:"depends_on"`
}

// RunReviewTarget is one named environment a suite run reaches.
type RunReviewTarget struct {
	Environment    ItemRef `json:"environment"`
	Name           string  `json:"name"`
	Address        string  `json:"address"`
	Classification string  `json:"classification"`
}

// SuiteEnvironmentRef is one environment a suite version declares, which a
// suite review can be changed to.
type SuiteEnvironmentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RunResumeReview is what a resume repeats: the run it continues and how
// many never-attempted messages it sends.
type RunResumeReview struct {
	From     ItemRef `json:"from"`
	Repeated int     `json:"repeated"`
}

// RunReviewedTest is what a reviewed test reproduces: the approved export
// review, the original packet and its phase, and the assessment it records.
type RunReviewedTest struct {
	Review string `json:"review"`
	Packet string `json:"packet"`
	Phase  string `json:"phase"`
}

// runBinding is what a run's final click executes.
type runBinding struct {
	kind RunKind
	root string
	// A test run and a resume execute a prepared spec at a target.
	prepared *durablerun.Prepared
	identity string
	specPath string
	target   string
	job      string
	// A suite run executes compiled suite bytes at one environment.
	suiteData   []byte
	environment string
	// A reviewed test executes the reexecution plan.
	reviewed   ReexecutionRequest
	output     string
	reset      *boundAction
	resets     []*boundAction
	source     ItemRef
	specDigest string
	setup      []string
	saved      savedRun
	name       string
}

func init() {
	actionPolicies[RunTestAction] = actionPolicy{consent: SendConsent, review: slot{}, perform: slot{profile: "StartDurableRun"},
		bind: bindRunTest, execute: executeRun}
	actionPolicies[RunSuiteAction] = actionPolicy{consent: SendConsent, review: slot{}, perform: slot{profile: "StartSuiteRun"},
		bind: bindRunSuite, execute: executeRun}
	actionPolicies[ResumeRunAction] = actionPolicy{consent: SendConsent, review: slot{}, perform: slot{profile: "ResumeDurableRun"},
		bind: bindResumeRun, execute: executeRun}
	actionPolicies[RunReviewedTestAction] = actionPolicy{consent: SendConsent, review: slot{}, perform: slot{profile: "ReexecuteReviewedEvidence"},
		bind: bindReviewedTest, execute: executeRun}
}

// scopedAt reads the objects a run is scoped to at any saved revision: a
// test can be run at the exact version a person chose, which need not be the
// current one. Each ref's revision is checked by the caller.
func (a *App) scopedAt(ctx context.Context, request RequestContext, refs []ItemRef) (*loadedCatalog, []CatalogItem, []catalog.Item, refusal) {
	current := make([]ItemRef, len(refs))
	for i, ref := range refs {
		current[i] = ItemRef{Kind: ref.Kind, ID: ref.ID}
	}
	return a.scoped(ctx, request, current)
}

// testVersionPath is the spec file of one saved revision of a test.
func (c *loadedCatalog) testVersionPath(item catalog.Item, label string) (string, string, error) {
	paths, availability, reason := c.revisionBacking(item, label)
	if availability != ItemAvailable {
		return "", "", errors.New(reason)
	}
	path := paths[primaryRole(TestItem)]
	return path, cmp.Or(label, item.RevisionLabel()), nil
}

// environmentRun is the named environment a run reaches, as the review
// shows it and binds it.
type environmentRun struct {
	record  catalog.Item
	ref     ItemRef
	name    string
	members *environmentMembers
	target  string
}

// namedEnvironment reads the named environment id of the loaded project.
func (c *loadedCatalog) namedEnvironment(id string) (*environmentRun, refusal) {
	index := c.document.Find(id)
	if id == "" || index < 0 || c.document.Items[index].Kind != string(EnvironmentItem) || c.removed(c.document.Items[index]) {
		return nil, refusal{Failed, "the environment this runs against is no longer in the project; choose another"}
	}
	record := c.document.Items[index]
	members, err := c.environmentOf(record)
	if err != nil {
		return nil, refusal{Failed, "the environment cannot be read: " + err.Error()}
	}
	target := c.entryOf(members.paths["target"])
	if target == "" {
		return nil, refusal{Failed, "the environment has no target in the project"}
	}
	return &environmentRun{record: record, ref: ItemRef{Kind: EnvironmentItem, ID: record.ID, Revision: record.RevisionLabel()},
		name: cmp.Or(record.Name, c.itemName(record.ID)), members: members, target: target}, refusal{}
}

// environmentRefusal is why a run to a target is not offered: a production
// or unclassified environment, or a transport not approved.
func environmentRefusal(name string, target replay.Target) string {
	shown := cmp.Or(name, target.Name, target.Address)
	switch classification := target.Environment().Classification; {
	case sendpolicy.RefusesEverySend(string(classification)):
		return shown + " is a production environment; runs never send to one"
	case classification != replay.Nonproduction:
		return shown + " is not classified; classify it as nonproduction to run against it"
	case approvalRequired(target) && !target.ApprovedTransport:
		return "the transport to " + target.Address + " is not approved; approve the transport first"
	}
	return ""
}

// setupSteps are the manual steps a test version names: its reset
// instructions, when it does not follow its environment's reset.
func setupSteps(spec testrunner.Spec, links *TestLinks) []RunSetupStep {
	if links != nil && links.Reset == ResetFromEnvironment {
		return []RunSetupStep{}
	}
	if instructions := strings.TrimSpace(spec.Setup.ResetInstructions); instructions != "" {
		return []RunSetupStep{{ID: "reset", Name: "Reset", Instructions: instructions}}
	}
	return []RunSetupStep{}
}

// withReset adds the environment's reset to a test's review: its manual
// actions are setup steps, and the actions that check run before any message
// is sent. A reset that is not ready refuses the run with its reason.
func (a *App) withReset(ctx context.Context, request RequestContext, environment *environmentRun, held bool, review *RunReview, output string) (*boundAction, string) {
	bound, declined := bindResetAt(a, ctx, PrepareActionRequest{Context: request, Action: ResetEnvironmentAction, Items: []ItemRef{{Kind: EnvironmentItem, ID: environment.ref.ID}}}, held, output)
	if bound == nil {
		return nil, declined.reason
	}
	for _, action := range bound.review.Reset.Actions {
		if action.Type == fixturereset.OperatorConfirms {
			review.Setup = append(review.Setup, RunSetupStep{ID: action.ID, Name: cmp.Or(action.Name, "Reset"), Instructions: action.Instructions})
		} else {
			action.EnvironmentName = environment.name
			review.Resets = append(review.Resets, action)
		}
	}
	if !bound.review.Ready {
		return bound, bound.review.Refusal
	}
	return bound, ""
}

// runReady decides whether a run review can be sent: the environment is
// one a run may reach, admission is granted and the output is fresh. The
// first refusal found is the one shown.
func (a *App) runReady(ctx context.Context, held bool, environment string, targets []replay.Target, destination RunDestination, other string) (bool, string, RunRefusal) {
	for _, target := range targets {
		if reason := environmentRefusal(environment, target); reason != "" {
			return false, reason, EnvironmentRefusal
		}
	}
	if other != "" {
		return false, other, ""
	}
	if !held {
		if admission := a.admissionPreview(ctx); !admission.Admitted {
			return false, admission.Reason, LicenseRefusal
		}
	}
	if !destination.Fresh {
		return false, cmp.Or(destination.Reason, "the run needs a new output"), ""
	}
	return true, "", ""
}

func bindRunTest(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != TestItem || request.Destination != nil && request.Destination.Kind != EnvironmentItem {
		return nil, refusal{Failed, "a test run is reviewed for one saved test version and the environment it runs against"}
	}
	loaded, items, records, declined := a.scopedAt(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	record := records[0]
	saved, err := loaded.testOf(record, request.Items[0].Revision)
	if err != nil {
		return nil, refusal{Failed, "this test cannot be read: " + err.Error()}
	}
	specPath, label, err := loaded.testVersionPath(record, request.Items[0].Revision)
	if err != nil {
		return nil, refusal{Failed, "this test cannot be read: " + err.Error()}
	}
	testRef := ItemRef{Kind: TestItem, ID: record.ID, Revision: label}
	review := &RunReview{Kind: TestRunKind, Name: saved.spec.Name, Version: label, Test: &testRef, Messages: loaded.sentMessages(saved.spec),
		Setup: setupSteps(saved.spec, saved.links), Resets: []ResetReviewAction{}, Jobs: []RunReviewJob{}, Targets: []RunReviewTarget{}, Environments: []SuiteEnvironmentRef{}}
	count := len(saved.spec.Input.Messages)
	review.MessageCount = &count
	environmentID := ""
	if request.Destination != nil {
		environmentID = request.Destination.ID
	} else if saved.links != nil {
		environmentID = saved.links.Environment
	}
	run := &runBinding{kind: TestRunKind, root: loaded.root, specPath: specPath, name: saved.spec.Name}
	destination, refused := destinationFor(loaded.root, "", "job")
	if refused.state != "" {
		return nil, refused
	}
	parts := []string{string(RunTestAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
		record.ID, label, fileDigest(specPath)}
	other := ""
	var environment *environmentRun
	if environmentID != "" {
		environment, declined = loaded.namedEnvironment(environmentID)
		if environment == nil {
			return nil, declined
		}
		run.target = environment.target
		review.Environment, review.EnvironmentName, review.Address = &environment.ref, environment.name, environment.members.target.Address
		run.saved = savedRun{test: &testRef, environment: &environment.ref, environmentName: environment.name, target: environment.target}
		parts = append(parts, environment.ref.ID, environment.ref.Revision, memberDigest(environment.record, "target"), memberDigest(environment.record, "policy"),
			memberDigest(environment.record, "reset"), memberDigest(environment.record, "links"))
		if saved.links != nil && saved.links.Reset == ResetFromEnvironment {
			reset, refused := a.withReset(ctx, request.Context, environment, held, review, destination.Name+"-reset-"+environment.ref.ID)
			run.reset, other = reset, refused
			if reset != nil {
				parts = append(parts, reset.binding)
			}
		}
	} else if named, held := loaded.targetEnvironments()[saved.spec.Target]; held {
		review.Environment, review.EnvironmentName = &named.ref, named.name
	} else {
		other = "choose the environment this test runs against"
	}
	// An environment no run may reach is refused as the environment's, before
	// anything is prepared against it.
	if environment != nil {
		if reason := environmentRefusal(environment.name, environment.members.target); reason != "" {
			review.Refusal = EnvironmentRefusal
			return &boundAction{action: RunTestAction, origin: request, run: run, binding: binding(parts...),
				review: ActionReview{Items: items, Ready: false, Refusal: reason, Run: review, Requirements: setupRequirement(review),
					Destination: ReviewDestination{Name: environment.name, Classification: string(environment.members.target.Environment().Classification),
						Address: environment.members.target.Address}}}, noRefusal
		}
	}
	prepared, err := durablerun.PrepareAt(specPath, run.target)
	if err != nil {
		return nil, refusal{Failed, preparationRefusal(err)}
	}
	identity, err := prepared.InputIdentity()
	if err != nil {
		return nil, refusal{Failed, "the test could not be prepared: its case, environment or messages did not verify"}
	}
	inputs := prepared.PinnedInputs()
	executed, _ := testrunner.DecodeSpec(inputs.Spec)
	run.specDigest = specDigest(executed)
	review.Address = inputs.Configuration.Address
	if review.EnvironmentName == "" {
		review.EnvironmentName = cmp.Or(inputs.Configuration.Name, inputs.Configuration.Address)
	}
	run.prepared, run.identity, run.output = prepared, identity, destination.Name
	for _, step := range review.Setup {
		run.setup = append(run.setup, step.ID)
	}
	ready, reason, kind := a.runReady(ctx, held, review.EnvironmentName, []replay.Target{inputs.Configuration}, destination, other)
	review.Refusal = kind
	parts = append(parts, identity, destination.Name, strings.Join(run.setup, "\x00"))
	return &boundAction{action: RunTestAction, origin: request, run: run, binding: binding(parts...),
		review: ActionReview{Items: items, Ready: ready, Refusal: reason, Run: review, Requirements: setupRequirement(review),
			Destination: ReviewDestination{Name: review.EnvironmentName, Classification: string(inputs.Configuration.Environment().Classification),
				Address: inputs.Configuration.Address, Output: destination.Name}}}, noRefusal
}

// setupRequirement asks the final click to mark every setup step complete.
func setupRequirement(review *RunReview) []ReviewRequirement {
	if len(review.Setup) == 0 {
		return []ReviewRequirement{}
	}
	return []ReviewRequirement{SetupRequirement}
}

// preparationRefusal words why a test could not be prepared, keeping the
// transport sentence a person can act on.
func preparationRefusal(err error) string {
	if errors.Is(err, replay.ErrTransportNotApproved) {
		return approvalReason(err)
	}
	return "the test could not be prepared: its case, environment or messages did not verify"
}

func bindRunSuite(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != SuiteItem || request.Destination != nil {
		return nil, refusal{Failed, "a suite run is reviewed for one saved suite version at one of its environments"}
	}
	loaded, version, data, declined := a.suiteForRun(ctx, SuiteRunTarget{Context: request.Context, Suite: request.Items[0]})
	if loaded == nil {
		return nil, declined
	}
	document, err := suite.Decode(data)
	if err != nil {
		return nil, refusal{Failed, "the suite version could not be compiled"}
	}
	review := &RunReview{Kind: SuiteRunKind, Name: loaded.suiteName(version.item), Version: version.label, Messages: []TestMessage{},
		Setup: []RunSetupStep{}, Resets: []ResetReviewAction{}, Jobs: []RunReviewJob{}, Targets: []RunReviewTarget{}, Environments: []SuiteEnvironmentRef{}}
	if version.original() {
		review.Version = ""
	}
	for _, environment := range version.draft.Environments {
		review.Environments = append(review.Environments, SuiteEnvironmentRef{ID: environment.ID, Name: cmp.Or(environment.Name, readableName(environment.ID))})
	}
	chosen := ""
	if request.Run != nil {
		chosen = request.Run.Environment
	}
	if chosen == "" && len(review.Environments) == 1 {
		chosen = review.Environments[0].ID
	}
	var selected *SuiteEnvironment
	for i := range version.draft.Environments {
		if version.draft.Environments[i].ID == chosen {
			selected = &version.draft.Environments[i]
		}
	}
	other := ""
	targets := []replay.Target{}
	parts := []string{string(RunSuiteAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
		version.item.ID, version.label, suite.Identity(data), chosen}
	destination, refused := destinationFor(loaded.root, "", "job")
	if refused.state != "" {
		return nil, refused
	}
	names := map[string]string{}
	instructions := map[string]bool{}
	inherited := map[string]bool{}
	resets := []*boundAction{}
	for _, test := range version.draft.Tests {
		names[test.ID] = test.ID
		if index := loaded.document.Find(test.Test.ID); index >= 0 {
			if saved, err := loaded.testOf(loaded.document.Items[index], test.Test.Revision); err == nil {
				names[test.ID] = saved.spec.Name
				inherited[test.Parameter] = inherited[test.Parameter] || saved.links != nil && saved.links.Reset == ResetFromEnvironment
				// The manual reset each test names is the suite's to do before
				// it sends; the same instructions are one step.
				for _, step := range setupSteps(saved.spec, saved.links) {
					if !instructions[step.Instructions] {
						instructions[step.Instructions] = true
						review.Setup = append(review.Setup, RunSetupStep{ID: "reset-" + test.ID, Name: saved.spec.Name, Instructions: step.Instructions})
					}
				}
			}
		}
	}
	bound := map[string]string{}
	if selected == nil {
		other = "choose the environment this suite runs at"
	} else {
		review.EnvironmentName, review.Site = cmp.Or(selected.Name, readableName(selected.ID)), selected.Site
		if _, err := suite.Preview(loaded.root, document, chosen, ""); err != nil {
			return nil, refusal{Failed, err.Error()}
		}
		for _, binding := range selected.Bindings {
			environment, declined := loaded.namedEnvironment(binding.Target.ID)
			if environment == nil {
				return nil, declined
			}
			bound[binding.Parameter] = environment.name
			if !slices.ContainsFunc(review.Targets, func(target RunReviewTarget) bool { return target.Environment.ID == environment.ref.ID }) {
				review.Targets = append(review.Targets, RunReviewTarget{Environment: environment.ref, Name: environment.name, Address: environment.members.target.Address,
					Classification: string(environment.members.target.Environment().Classification)})
				targets = append(targets, environment.members.target)
			}
			parts = append(parts, environment.ref.ID, environment.ref.Revision, memberDigest(environment.record, "target"), memberDigest(environment.record, "policy"),
				memberDigest(environment.record, "reset"), memberDigest(environment.record, "links"))
			if inherited[binding.Parameter] && !slices.ContainsFunc(resets, func(reset *boundAction) bool { return reset.origin.Items[0].ID == environment.ref.ID }) {
				before := len(review.Setup)
				reset, reason := a.withReset(ctx, request.Context, environment, held, review, destination.Name+"-reset-"+environment.ref.ID)
				if reset != nil {
					resets = append(resets, reset)
					parts = append(parts, reset.binding)
				}
				for i := before; i < len(review.Setup); i++ {
					review.Setup[i].ID = environment.ref.ID + ":" + review.Setup[i].ID
					review.Setup[i].Name = environment.name + " · " + review.Setup[i].Name
				}
				other = cmp.Or(other, reason)
			}
		}
	}
	datasets := map[string]SuiteDataset{}
	for _, dataset := range version.draft.Datasets {
		datasets[dataset.ID] = dataset
	}
	for _, test := range version.draft.Tests {
		job := RunReviewJob{ID: test.ID, Test: names[test.ID], Version: test.Test.Revision, StateSharing: test.Isolation, Target: bound[test.Parameter], DependsOn: []string{}}
		if dataset, held := datasets[test.Dataset]; held {
			job.Dataset, job.Rows = dataset.Name, len(dataset.Rows)
		}
		for _, after := range test.After {
			job.DependsOn = append(job.DependsOn, cmp.Or(names[after], after))
		}
		review.Jobs = append(review.Jobs, job)
	}
	ready, reason, kind := a.runReady(ctx, held, "", targets, destination, other)
	review.Refusal = kind
	parts = append(parts, destination.Name)
	run := &runBinding{kind: SuiteRunKind, root: loaded.root, suiteData: data, identity: suite.Identity(data), environment: chosen, output: destination.Name, name: review.Name}
	run.source = ItemRef{Kind: SuiteItem, ID: version.item.ID, Revision: version.label}
	run.resets = resets
	for _, step := range review.Setup {
		run.setup = append(run.setup, step.ID)
	}
	parts = append(parts, strings.Join(run.setup, "\x00"))
	items := []CatalogItem{loaded.read(version.item)}
	return &boundAction{action: RunSuiteAction, origin: request, run: run, binding: binding(parts...),
		review: ActionReview{Items: items, Ready: ready, Refusal: reason, Run: review, Requirements: setupRequirement(review),
			Destination: ReviewDestination{Name: review.EnvironmentName, Output: destination.Name}}}, noRefusal
}

func bindResumeRun(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != RunItem || request.Destination != nil {
		return nil, refusal{Failed, "a resume is reviewed for one interrupted run"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	job := filepath.Join(loaded.root, records[0].Entry)
	recovery, err := durablerun.Recover(job)
	if err != nil {
		return nil, refusal{Failed, "this run's journal cannot be verified; nothing can be resumed from it"}
	}
	inputs, _, err := durablerun.RetainedInputs(job)
	if err != nil {
		return nil, refusal{Failed, "this run's journal cannot be verified; nothing can be resumed from it"}
	}
	spec, err := testrunner.DecodeSpec(inputs.Spec)
	if err != nil {
		return nil, refusal{Failed, "this run's test cannot be read back"}
	}
	origin := loaded.originOfRun(job, spec)
	review := &RunReview{Kind: TestRunKind, Name: spec.Name, Test: origin.test, Environment: origin.environment, EnvironmentName: origin.environmentName,
		Address: inputs.Configuration.Address, Messages: []TestMessage{}, Setup: []RunSetupStep{}, Resets: []ResetReviewAction{}, Jobs: []RunReviewJob{},
		Targets: []RunReviewTarget{}, Environments: []SuiteEnvironmentRef{},
		Resume: &RunResumeReview{From: items[0].Ref, Repeated: recovery.NotAttempted}}
	review.MessageCount = &recovery.NotAttempted
	notAttempted := map[string]bool{}
	for _, occurrence := range recovery.Occurrences {
		if occurrence.Delivery == durablerun.NotAttempted {
			notAttempted[occurrence.ID] = true
		}
	}
	bySource := map[string]TestMessage{}
	for _, message := range loaded.sentMessages(spec) {
		bySource[message.ID] = message
	}
	for _, mapping := range inputs.Mappings {
		if !notAttempted[mapping.OutboundOccurrence] {
			continue
		}
		message, held := bySource[mapping.SourceOccurrence]
		if !held {
			message = TestMessage{ID: mapping.SourceOccurrence, Kind: bundle.Message}
		}
		review.Messages = append(review.Messages, message)
	}
	other := ""
	run := &runBinding{kind: TestRunKind, root: loaded.root, job: job, target: spec.Target, name: spec.Name}
	run.specDigest = specDigest(spec)
	destination, refused := destinationFor(loaded.root, "", "job")
	if refused.state != "" {
		return nil, refused
	}
	switch {
	case !recovery.SafeToRepeat:
		other = "Resume is unavailable: " + cmp.Or(recovery.ResumeRefusal, "the run attempted a message")
	case origin.test == nil:
		other = "Resume is unavailable: the project no longer holds the version of the test this run executed"
	}
	if origin.test != nil {
		review.Version = origin.test.Revision
		index := loaded.document.Find(origin.test.ID)
		if path, _, err := loaded.testVersionPath(loaded.document.Items[index], origin.test.Revision); err == nil {
			run.specPath = path
			if saved, err := loaded.testOf(loaded.document.Items[index], origin.test.Revision); err == nil {
				review.Setup = setupSteps(saved.spec, saved.links)
				if saved.links != nil && saved.links.Reset == ResetFromEnvironment && other == "" {
					if origin.environment == nil {
						other = "Resume is unavailable: the environment whose reset this run used cannot be identified"
					} else if environment, declined := loaded.namedEnvironment(origin.environment.ID); environment == nil {
						other = "Resume is unavailable: " + declined.reason
					} else if environment.ref.Revision != origin.environment.Revision || environment.target != spec.Target {
						other = "Resume is unavailable: the environment or reset changed since this run; Run again reviews a new execution"
					} else {
						reset, reason := a.withReset(ctx, request.Context, environment, held, review, destination.Name+"-reset-"+environment.ref.ID)
						run.reset, other = reset, reason
					}
				}
			}
		}
	}
	targets := []replay.Target{inputs.Configuration}
	if run.specPath != "" && other == "" {
		prepared, err := durablerun.PrepareAt(run.specPath, run.target)
		if err != nil {
			other = "Resume is unavailable: " + preparationRefusal(err)
		} else {
			current, _ := json.Marshal(prepared.PinnedInputs(), json.Deterministic(true))
			retained, _ := json.Marshal(inputs, json.Deterministic(true))
			if !bytes.Equal(current, retained) {
				other = "Resume is unavailable: the test, its case or its environment changed since this run; a resume repeats only the same work"
			}
		}
	}
	run.output = destination.Name
	for _, step := range review.Setup {
		run.setup = append(run.setup, step.ID)
	}
	ready, reason, kind := a.runReady(ctx, held, review.EnvironmentName, targets, destination, other)
	review.Refusal = kind
	resetBinding := ""
	if run.reset != nil {
		resetBinding = run.reset.binding
	}
	return &boundAction{action: ResumeRunAction, origin: request, run: run,
		binding: binding(string(ResumeRunAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
			records[0].ID, fileDigest(filepath.Join(job, "journal.jsonl")), fileDigest(run.specPath), fileDigest(filepath.Join(loaded.root, run.target)),
			destination.Name, strings.Join(run.setup, "\x00"), resetBinding),
		review: ActionReview{Items: items, Ready: ready, Refusal: reason, Run: review, Requirements: setupRequirement(review),
			Destination: ReviewDestination{Name: review.EnvironmentName, Classification: string(inputs.Configuration.Environment().Classification),
				Address: inputs.Configuration.Address, Output: destination.Name}}}, noRefusal
}

func bindReviewedTest(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	// A share's check (#560) names the export review and the report alone:
	// the test it runs is the one preparing the check wrote for that review.
	check := len(request.Items) == 2
	if len(request.Items) != 3 && !check || request.Items[0].Kind != ReportItem || request.Items[1].Kind != ReportItem || !check && request.Items[2].Kind != TestItem ||
		request.Run == nil || request.Run.Phase != "failure" && request.Run.Phase != "pass" {
		return nil, refusal{Failed, "a reviewed test is reviewed for one approved export review, its original packet and phase, and one rebound test"}
	}
	loaded, items, records, declined := a.scopedAt(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	originalForm := items[1].Summary.Report != nil && (items[1].Summary.Report.Form == "packet" || check && items[1].Summary.Report.Form == "report")
	if items[0].Summary.Report == nil || items[0].Summary.Report.Form != "export-review" || !originalForm {
		return nil, refusal{Failed, "a reviewed test reproduces the phase of an original packet an export review was derived from"}
	}
	opened, private, err := loaded.exportReview(filepath.Join(loaded.root, records[0].Entry))
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	var specPath, label string
	var testRef *ItemRef
	if check {
		if specPath = shareCheckSpec(loaded.root, records[0].Entry); specPath == "" {
			return nil, refusal{Failed, "prepare the share's check again"}
		}
	} else {
		if specPath, label, err = loaded.testVersionPath(records[2], request.Items[2].Revision); err != nil {
			return nil, refusal{Failed, "this test cannot be read: " + err.Error()}
		}
		testRef = &ItemRef{Kind: TestItem, ID: records[2].ID, Revision: label}
	}
	specEntry := loaded.entryOf(specPath)
	if check {
		specEntry = specPath
	}
	reexecution := ReexecutionRequest{Workspace: loaded.root, Review: records[0].Entry, LocalState: private.entry, OriginalPacket: records[1].Entry,
		Spec: specEntry, Phase: request.Run.Phase, Approval: opened.Identity}
	plan, declined := prepareReexecution(ctx, loaded.root, reexecution)
	if plan == nil {
		return nil, declined
	}
	preview, err := reexecutionPreview(plan)
	if err != nil {
		return nil, refusal{Failed, "the rebound test could not be read back"}
	}
	inputs := plan.PinnedInputs()
	spec, err := testrunner.DecodeSpec(inputs.Spec)
	if err != nil {
		return nil, refusal{Failed, "the rebound test could not be read back"}
	}
	review := &RunReview{Kind: TestRunKind, Name: spec.Name, Version: label, Test: testRef, EnvironmentName: cmp.Or(preview.Target.Name, preview.Target.Address),
		Address: preview.Target.Address, Messages: loaded.sentMessages(spec), Setup: setupSteps(spec, nil), Resets: []ResetReviewAction{}, Jobs: []RunReviewJob{},
		Targets: []RunReviewTarget{}, Environments: []SuiteEnvironmentRef{},
		Reviewed: &RunReviewedTest{Review: items[0].Name, Packet: items[1].Name, Phase: request.Run.Phase}}
	count := len(preview.Selected)
	review.MessageCount = &count
	destination, refused := destinationFor(loaded.root, "", "reexecution")
	if refused.state != "" {
		return nil, refused
	}
	run := &runBinding{kind: TestRunKind, root: loaded.root, reviewed: reexecution, identity: preview.Identity, output: destination.Name, name: spec.Name}
	run.specDigest = specDigest(spec)
	for _, step := range review.Setup {
		run.setup = append(run.setup, step.ID)
	}
	ready, reason, kind := a.runReady(ctx, held, review.EnvironmentName, []replay.Target{inputs.Configuration}, destination, "")
	review.Refusal = kind
	return &boundAction{action: RunReviewedTestAction, origin: request, run: run,
		binding: binding(string(RunReviewedTestAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
			records[0].ID, opened.Identity, records[1].ID, testID(records), label, fileDigest(specPath), preview.Identity, destination.Name, strings.Join(run.setup, "\x00")),
		review: ActionReview{Items: items, Ready: ready, Refusal: reason, Run: review, Requirements: setupRequirement(review),
			Destination: ReviewDestination{Name: review.EnvironmentName, Classification: preview.Target.Classification, Address: preview.Target.Address, Output: destination.Name}}}, noRefusal
}

// executeRun sends a bound run once: a reset the test follows first, when it
// has one, and nothing at all when that reset does not confirm the
// environment. The result names the run the project now holds, so the
// window opens it.
func executeRun(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	run := bound.run
	result := ReviewedActionResult{Outcome: ActionCompleted}
	if err := recordRunOrigin(run, bound.review.Run); err != nil {
		result.refuse(Failed, "the selected version could not be retained; nothing was sent")
		result.Outcome = ActionRefused
		return result
	}
	resets := run.resets
	if run.reset != nil {
		resets = append([]*boundAction{run.reset}, resets...)
	}
	for _, planned := range resets {
		manual := []string{}
		for _, action := range planned.review.Reset.Actions {
			if action.Type == fixturereset.OperatorConfirms {
				manual = append(manual, action.ID)
			}
		}
		reset := executeReset(a, ctx, planned, ReviewDecisions{Confirmed: manual})
		result.Reset = reset.Reset
		if reset.State != Completed {
			result.State, result.Outcome = reset.State, ActionRefused
			if reset.State == Cancelled {
				result.Outcome = ActionCancelled
			}
			result.Reason = cmp.Or(reset.Reason, "the reset did not complete") + "; nothing was sent"
			return result
		}
	}
	output := filepath.Join(run.root, run.output)
	a.setRunOutput(output)
	defer a.setRunOutput("")
	var err error
	uncertain := false
	switch {
	case bound.action == RunSuiteAction:
		var path string
		var remove func()
		path, remove, err = placeCompiled(run.root, run.suiteData)
		if err != nil {
			result.refuse(Failed, err.Error()+"; nothing was sent")
			return result
		}
		defer remove()
		a.reach(suiteReaching(run.root, path, run.environment))
		var report runqueue.Report
		report, err = suite.Run(ctx, suite.Request{Path: path, Environment: run.environment, Output: output, Identity: run.identity})
		if errors.Is(err, suite.ErrChanged) {
			result.refuse(Failed, "the suite changed after it was reviewed; review it again. Nothing was sent")
			result.Outcome = ActionStale
			return result
		}
		if err != nil && report.Schema == "" {
			result.refuse(Failed, "the suite could not be prepared; nothing was sent")
			return result
		}
		for _, job := range report.Jobs {
			uncertain = uncertain || job.Run != nil && job.Run.DeliveryUncertain
		}
		if err != nil {
			result.Reason = "the suite stopped before every job ran; what ran is retained"
		}
		err = nil
	case bound.action == ResumeRunAction:
		a.reach(runReaching(savedRun{}, run.name, replay.Target{Address: bound.review.Destination.Address}))
		var resumed durablerun.Resumption
		resumed, err = durablerun.ResumeAt(ctx, run.job, run.specPath, run.target, output)
		uncertain = resumed.Run.DeliveryUncertain
		if err != nil && resumed.Schema == "" && strings.HasPrefix(err.Error(), "resume refused: ") {
			result.refuse(Failed, err.Error())
			return result
		}
	case bound.action == RunReviewedTestAction:
		plan, declined := prepareReexecution(ctx, run.root, run.reviewed)
		if plan == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		a.reach(runReaching(savedRun{}, run.name, plan.PinnedInputs().Configuration))
		_, err = plan.ExecutePinned(ctx, output, run.identity)
		if progress := retainedJob(output); progress != nil {
			uncertain = progress.Uncertain > 0
		}
	default:
		inputs := run.prepared.PinnedInputs()
		a.reach(runReaching(run.saved, run.name, inputs.Configuration))
		var summary durablerun.Summary
		summary, err = run.prepared.StartPinned(ctx, output, run.identity)
		uncertain = summary.DeliveryUncertain
	}
	if errors.Is(err, durablerun.ErrInputsChanged) {
		result.refuse(Failed, "what this run reviewed changed; review it again. Nothing was sent")
		result.Outcome = ActionStale
		return result
	}
	if _, statErr := os.Lstat(output); statErr != nil {
		// No run folder: nothing was sent.
		result.refuse(Failed, cmp.Or(runFailure(err), "the run could not start; nothing was sent"))
		return result
	}
	result.State, result.Run = Completed, a.runItemOf(context.WithoutCancel(ctx), bound.origin.Context, run.output)
	switch {
	case uncertain:
		result.Outcome = ActionUncertain
	case errors.Is(ctx.Err(), context.Canceled):
		result.State, result.Outcome = Cancelled, ActionCancelled
		result.Reason = "the run was stopped; what it sent is retained, and whatever may already have been delivered is never sent again"
	case err != nil:
		result.State, result.Reason = Failed, runFailure(err)
	}
	return result
}

// runFailure words a run that could not finish, without a local path.
func runFailure(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, engine.ErrUnsupportedVersion):
		return "the retained run was evaluated by a version this release cannot read"
	case strings.HasPrefix(err.Error(), "resume refused: "):
		return err.Error()
	}
	return "the run could not finish; its retained evidence shows what happened"
}

// runItemOf is the run the project lists at one entry.
func (a *App) runItemOf(ctx context.Context, request RequestContext, entry string) *ItemRef {
	loaded, _ := a.loadCatalog(ctx, request, false)
	for loaded != nil {
		if index := loaded.document.ByEntry(string(RunItem), entry); index >= 0 {
			return &ItemRef{Kind: RunItem, ID: loaded.document.Items[index].ID}
		}
		if loaded.upper == "" {
			return nil
		}
		loaded, _ = a.loadWindow(ctx, request, false, loaded.upper)
	}
	return nil
}

// setupMarked reports whether a run's final click marked every setup step
// complete, and nothing else.
func setupMarked(bound *boundAction, decisions ReviewDecisions) string {
	if bound.run == nil {
		return ""
	}
	confirmed := slices.Clone(decisions.Confirmed)
	slices.Sort(confirmed)
	required := slices.Clone(bound.run.setup)
	slices.Sort(required)
	if !slices.Equal(slices.Compact(confirmed), required) {
		return "every setup step is marked complete, and nothing else, before a run; nothing was sent"
	}
	return ""
}

// sendReview is a send of selected messages as the one review shows it: the
// case they come from, the environment, and the chosen messages in the order
// they are sent.
func sendReview(loaded *loadedCatalog, items []CatalogItem, entry string, preview *ReplayPreview) *RunReview {
	review := &RunReview{Kind: SendRunKind, Name: items[0].Name, Version: items[0].Ref.Revision, Environment: &items[1].Ref, EnvironmentName: items[1].Name,
		Address: preview.Target.Address, Messages: []TestMessage{}, Setup: []RunSetupStep{}, Resets: []ResetReviewAction{}, Jobs: []RunReviewJob{},
		Targets: []RunReviewTarget{}, Environments: []SuiteEnvironmentRef{}}
	count := len(preview.Messages)
	review.MessageCount = &count
	types := map[string]TestMessage{}
	if _, _, source := loaded.caseOf(entry); source != nil {
		for _, message := range caseMessages(source) {
			types[message.ID] = message
		}
	}
	for _, message := range preview.Messages {
		typed, held := types[message.Source]
		if !held {
			typed = TestMessage{ID: message.Source}
		}
		review.Messages = append(review.Messages, typed)
	}
	target := replay.Target{Address: preview.Target.Address, Classification: replay.Classification(preview.Target.Classification), ApprovedTransport: true}
	switch {
	case environmentRefusal(items[1].Name, target) != "":
		review.Refusal = EnvironmentRefusal
	case !preview.Admission.Admitted:
		review.Refusal = LicenseRefusal
	}
	return review
}

// testID is a reviewed test's rebound test, or none for a share's check.
func testID(records []catalog.Item) string {
	if len(records) < 3 {
		return ""
	}
	return records[2].ID
}
