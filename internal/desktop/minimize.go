package desktop

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testauthor"
)

// Minimize failure (#558). A saved failed test run is reduced to the smallest
// sequence that still fails the chosen checks, by the engine internal/reduce
// runs: every trial resets the chosen environment by its saved reset and sends
// through a durable run of the exact test version that failed. The whole
// bounded series is one reviewed action — the run, the checks, the
// environment and its reset actions, the grouping and both bounds are bound
// by the review, and Start is the one consent to all of it. A trial whose
// reset is not confirmed or whose delivery is uncertain stops the series;
// nothing is retried or resent. Only a reduced result is published as a
// variant of the case the run sent from.

const (
	// MinimizeFailureAction reduces one failed run of a test.
	MinimizeFailureAction ActionID = "run.minimize"
	// MinimizeConsent: the displayed environment is reset and the displayed
	// test's messages are sent, trial after trial, within the displayed
	// bounds.
	MinimizeConsent Consent = "minimize"
)

func init() {
	actionPolicies[MinimizeFailureAction] = actionPolicy{consent: MinimizeConsent, requirements: []ReviewRequirement{},
		review: slot{}, perform: slot{profile: "StartReduction"}, bind: bindMinimize, execute: executeMinimize}
}

// MinimizeOptions are what a minimization is asked for: the failed checks
// the result must still fail, how the sequence is taken apart (each message
// alone, or the groups one set of link rules relates), and the two bounds.
type MinimizeOptions struct {
	Connected     *ConnectedMinimizeOptions `json:"connected,omitzero"`
	Checks        []string                  `json:"checks"`
	Grouping      string                    `json:"grouping"`
	Rules         *ItemRef                  `json:"rules,omitzero"`
	Trials        int                       `json:"trials"`
	Confirmations int                       `json:"confirmations"`
}

// MinimizeReview is what one minimization will do, as its review shows it:
// the run and the exact test version that failed, the checks it keeps
// failing, the case its messages come from, the grouping and how many groups
// it makes, the bounds, and the environment with the reset every trial runs.
type MinimizeReview struct {
	Connected       *ConnectedMinimizeReview `json:"connected,omitzero"`
	Run             ItemRef                  `json:"run"`
	Test            *ItemRef                 `json:"test,omitzero"`
	TestName        string                   `json:"test_name"`
	Version         string                   `json:"version,omitzero"`
	Case            *ItemRef                 `json:"case,omitzero"`
	CaseName        string                   `json:"case_name,omitzero"`
	Checks          []testauthor.Expectation `json:"checks"`
	Messages        []TestMessage            `json:"messages"`
	Grouping        string                   `json:"grouping"`
	Rules           *ItemRef                 `json:"rules,omitzero"`
	RulesName       string                   `json:"rules_name,omitzero"`
	Groups          int                      `json:"groups"`
	Pinned          int                      `json:"pinned"`
	Trials          int                      `json:"trials"`
	Confirmations   int                      `json:"confirmations"`
	Environment     *ItemRef                 `json:"environment,omitzero"`
	EnvironmentName string                   `json:"environment_name,omitzero"`
	Address         string                   `json:"address,omitzero"`
	Resets          []ResetReviewAction      `json:"resets"`
	Unsupported     []reduce.Unsupported     `json:"unsupported"`
	Refusal         RunRefusal               `json:"refusal,omitzero"`
}

// MinimizeOutcome is what a minimization reached: the engine's outcome and
// reason, whether it claims anything minimal, the messages it started from
// and the ones it kept, every trial, where its trial material is kept, and
// the variant a reduced result was published as.
type MinimizeOutcome struct {
	Outcome    reduce.Outcome    `json:"outcome"`
	Reason     reduce.Reason     `json:"reason"`
	Minimality reduce.Minimality `json:"minimality"`
	Original   []TestMessage     `json:"original"`
	Retained   []TestMessage     `json:"retained"`
	Groups     []reduce.Group    `json:"groups"`
	Trials     []reduce.Trial    `json:"trials"`
	Budget     int               `json:"budget"`
	Output     string            `json:"output,omitzero"`
	Variant    *ItemRef          `json:"variant,omitzero"`
	// VariantRefusal says why a reduced result was not published.
	VariantRefusal string `json:"variant_refusal,omitzero"`
}

// MinimizeSetup is what a minimization of one run starts from, read from the
// run itself: whether it can be minimized at all, the exact test version,
// the checks it failed, the case and the environment it ran against.
type MinimizeSetup struct {
	Connected       []ConnectedMinimizeChoice `json:"connected"`
	Run             ItemRef                   `json:"run"`
	RunName         string                    `json:"run_name"`
	Eligible        bool                      `json:"eligible"`
	Refusal         string                    `json:"refusal,omitzero"`
	Test            *ItemRef                  `json:"test,omitzero"`
	Version         string                    `json:"version,omitzero"`
	Case            *ItemRef                  `json:"case,omitzero"`
	Failed          []testauthor.Expectation  `json:"failed"`
	Messages        []TestMessage             `json:"messages"`
	Environment     *ItemRef                  `json:"environment,omitzero"`
	EnvironmentName string                    `json:"environment_name,omitzero"`
	// Bounds and Groupings are the ones this release offers: no default is
	// chosen for a person.
	MaxTrials        int      `json:"max_trials"`
	MaxConfirmations int      `json:"max_confirmations"`
	Groupings        []string `json:"groupings"`
}

// MinimizeSetupResult answers one setup.
type MinimizeSetupResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Setup   *MinimizeSetup `json:"setup,omitzero"`
}

func (r *MinimizeSetupResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// minimizeRun is one run as a minimization reads it.
type minimizeRun struct {
	item     *CatalogItem
	summary  RunSummary
	failed   []testauthor.Expectation
	testItem *catalog.Item
	spec     *savedTest
	specPath string
	label    string
	refusal  string
}

// readMinimizeRun reads one run and decides whether it can be minimized:
// a finished run of a saved test version that failed at least one check,
// every delivery settled.
func (a *App) readMinimizeRun(ctx context.Context, request RequestContext, ref ItemRef) (*loadedCatalog, *minimizeRun, refusal) {
	loaded, item, path, refused := a.runOf(ctx, RunRequest{Context: request, Run: ItemRef{Kind: RunItem, ID: ref.ID}})
	if loaded == nil {
		return nil, nil, refused
	}
	run := &minimizeRun{item: item, failed: []testauthor.Expectation{}}
	if item.Summary.Run != nil {
		run.summary = *item.Summary.Run
	}
	detail := &RunDetail{Checks: []RunCheck{}, Messages: []RunMessage{}}
	if item.Availability == ItemAvailable && run.summary.Kind == TestRunKind && !run.summary.Active {
		loaded.testDetail(path, detail, false, false)
	}
	for _, check := range detail.Checks {
		if check.Result == CheckFailed {
			run.failed = append(run.failed, check.Check)
		}
	}
	switch {
	case item.Availability != ItemAvailable:
		run.refusal = "this run cannot be read: " + item.Reason
	case run.summary.Kind != TestRunKind:
		run.refusal = "only a run of one test is minimized"
	case run.summary.Active:
		run.refusal = "this run has not finished"
	case run.summary.DeliveryUncertain:
		run.refusal = "a delivery of this run is uncertain, so its failure cannot be held"
	case run.summary.Result != RunFailed || len(run.failed) == 0:
		run.refusal = "this run failed no check"
	case run.summary.Test == nil:
		run.refusal = "the test version this run executed is no longer in the project"
	}
	if run.summary.Test != nil {
		if index := loaded.document.Find(run.summary.Test.ID); index >= 0 && !loaded.removed(loaded.document.Items[index]) {
			record := loaded.document.Items[index]
			run.testItem = &record
			saved, err := loaded.testOf(record, run.summary.Version)
			if err == nil {
				run.spec = saved
				run.specPath, run.label, err = loaded.testVersionPath(record, run.summary.Version)
			}
			if err != nil && run.refusal == "" {
				run.refusal = "the test version this run executed cannot be read: " + err.Error()
			}
		} else if run.refusal == "" {
			run.refusal = "the test version this run executed is no longer in the project"
		}
	}
	return loaded, run, noRefusal
}

// MinimizeSetup reads what minimizing one run starts from. It is a read:
// nothing is reset, sent or written.
func (a *App) MinimizeSetup(request RunRequest) MinimizeSetupResult {
	return runRead(a, false, func(ctx context.Context) MinimizeSetupResult {
		result := MinimizeSetupResult{Context: request.Context}
		loaded, run, declined := a.readMinimizeRun(ctx, request.Context, request.Run)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		setup := &MinimizeSetup{Run: run.item.Ref, RunName: run.item.Name, Eligible: run.refusal == "", Refusal: run.refusal,
			Test: run.summary.Test, Version: run.summary.Version, Failed: run.failed, Messages: []TestMessage{},
			Environment: run.summary.Environment, EnvironmentName: run.summary.EnvironmentName,
			MaxTrials: reduce.MaxTrials, MaxConfirmations: reduce.MaxConfirmations,
			Groupings: []string{reduce.GroupPerOccurrence, reduce.GroupByCorrelation}}
		if run.spec != nil {
			setup.Messages = loaded.sentMessages(run.spec.spec)
			setup.Case = loaded.entryRef(CaseItem, run.spec.spec.Input.Case)
			if setup.Case == nil {
				setup.Case = loaded.entryRef(VariantItem, run.spec.spec.Input.Case)
			}
			setup.Connected = connectedMinimizeChoices(loaded, run)
		}
		if setup.Connected == nil {
			setup.Connected = []ConnectedMinimizeChoice{}
		}
		result.State, result.Setup = Completed, setup
		return result
	})
}

// minimizeBinding is what a minimization's Start executes.
type minimizeBinding struct {
	connected *connectedMinimizeBinding
	root      string
	specPath  string
	target    string
	reset     *boundAction
	plan      reduce.Plan
	rules     correlate.Rules
	casePath  string
	caseRef   *ItemRef
	caseName  string
	output    string
	messages  []TestMessage
}

func bindMinimize(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != RunItem || request.Minimize == nil || request.Destination != nil && request.Destination.Kind != EnvironmentItem {
		return nil, refusal{Failed, "a minimization is reviewed for one failed run, its checks and bounds, and the environment its trials reach"}
	}
	options := *request.Minimize
	loaded, run, declined := a.readMinimizeRun(ctx, request.Context, request.Items[0])
	if loaded == nil {
		return nil, declined
	}
	if run.refusal != "" {
		return nil, refusal{Failed, run.refusal}
	}
	spec := run.spec.spec
	review := &MinimizeReview{Run: run.item.Ref, Test: run.summary.Test, TestName: spec.Name, Version: run.label, Checks: []testauthor.Expectation{},
		Messages: loaded.sentMessages(spec), Grouping: options.Grouping, Trials: options.Trials, Confirmations: options.Confirmations,
		Resets: []ResetReviewAction{}, Unsupported: []reduce.Unsupported{}}
	items := []CatalogItem{*run.item}
	parts := []string{string(MinimizeFailureAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
		run.item.Ref.ID, run.testItem.ID, run.label, fileDigest(run.specPath)}
	// The checks the result keeps failing: failed checks of this run, each
	// once, in the order the run lists them.
	for _, check := range run.failed {
		if slices.Contains(options.Checks, check.ID) {
			review.Checks = append(review.Checks, check)
		}
	}
	other := ""
	if len(review.Checks) == 0 || len(review.Checks) != len(slices.Compact(slices.Sorted(slices.Values(options.Checks)))) {
		other = "choose the failed checks the result keeps failing"
	}
	if options.Trials < 1 || options.Trials > reduce.MaxTrials {
		other = cmp.Or(other, "the trial limit is a whole number from 1 to "+strconv.Itoa(reduce.MaxTrials))
	}
	if options.Confirmations < 1 || options.Confirmations > reduce.MaxConfirmations {
		other = cmp.Or(other, "the confirmation count is a whole number from 1 to "+strconv.Itoa(reduce.MaxConfirmations))
	}
	planned := &minimizeBinding{root: loaded.root, specPath: run.specPath, messages: review.Messages}
	switch options.Grouping {
	case reduce.GroupPerOccurrence:
		if options.Rules != nil {
			other = cmp.Or(other, "grouping per message relates nothing; choose linked groups to use link rules")
		}
	case reduce.GroupByCorrelation:
		if options.Rules == nil {
			other = cmp.Or(other, "linked groups need the link rules that relate them")
			break
		}
		rules, exact, name, err := loaded.linkRulesAt(*options.Rules)
		if err != nil {
			other = cmp.Or(other, err.Error())
			break
		}
		planned.rules, review.Rules, review.RulesName = rules, &exact, name
		parts = append(parts, exact.ID, exact.Revision, memberDigest(loaded.document.Items[loaded.document.Find(exact.ID)], string(LinkRulesItem)))
	default:
		other = cmp.Or(other, "choose how the messages are grouped")
	}
	// The environment every trial resets and sends to: the one chosen, or
	// the one the run reached.
	environmentID := ""
	if request.Destination != nil {
		environmentID = request.Destination.ID
	} else if run.summary.Environment != nil {
		environmentID = run.summary.Environment.ID
	}
	destination, refused := destinationFor(loaded.root, "", "minimize")
	if refused.state != "" {
		return nil, refused
	}
	planned.output = destination.Name
	var environment *environmentRun
	if environmentID == "" {
		other = cmp.Or(other, "choose the environment the trials run against")
		review.Refusal = EnvironmentRefusal
	} else {
		environment, declined = loaded.namedEnvironment(environmentID)
		if environment == nil {
			return nil, declined
		}
		planned.target = filepath.Join(loaded.root, environment.target)
		review.Environment, review.EnvironmentName, review.Address = &environment.ref, environment.name, environment.members.target.Address
		items = append(items, loaded.read(environment.record))
		parts = append(parts, environment.ref.ID, environment.ref.Revision, memberDigest(environment.record, "target"), memberDigest(environment.record, "policy"),
			memberDigest(environment.record, "reset"), memberDigest(environment.record, "links"))
		if reason := environmentRefusal(environment.name, environment.members.target); reason != "" {
			other, review.Refusal = cmp.Or(other, reason), EnvironmentRefusal
		} else if options.Connected != nil && environment.members.isolation != nil {
			// The selected lifecycle owns this saved typed isolation. It is
			// bound below, with every manual setup clause retained.
		} else if environment.members.reset == nil {
			other, review.Refusal = cmp.Or(other, "every trial resets "+environment.name+" first, and it has no reset; add one to the environment"), EnvironmentRefusal
		} else {
			reset, declined := bindResetAt(a, ctx, PrepareActionRequest{Context: request.Context, Action: ResetEnvironmentAction, Items: []ItemRef{{Kind: EnvironmentItem, ID: environment.ref.ID}}},
				held, destination.Name+"-reset")
			if reset == nil {
				other, review.Refusal = cmp.Or(other, declined.reason), EnvironmentRefusal
			} else {
				planned.reset = reset
				for _, action := range reset.review.Reset.Actions {
					action.EnvironmentName = environment.name
					review.Resets = append(review.Resets, action)
				}
				if !reset.review.Ready {
					other, review.Refusal = cmp.Or(other, reset.review.Refusal), EnvironmentRefusal
				}
				parts = append(parts, reset.binding)
			}
		}
	}
	// The source case is verified now and the reduction's partition is read
	// over it, so the review shows how many groups there are and which the
	// checks pin, and nothing is reset or sent to find out.
	casePath := filepath.Join(filepath.Dir(run.specPath), spec.Input.Case)
	identity, err := readCaseIdentity(casePath)
	if err != nil {
		return nil, refusal{Failed, "the case this test sends from cannot be verified: " + err.Error()}
	}
	planned.casePath = casePath
	planned.plan = reduce.Plan{Schema: reduce.PlanSchema, Case: identity, Grouping: options.Grouping,
		Signature: reduce.Signature{State: durablerun.AssertionFailed, Assertions: expectationIDs(review.Checks)},
		Trials:    options.Trials, Confirmations: options.Confirmations}
	if planned.rules.Schema != "" {
		planned.plan.Rules, _ = correlate.RulesDigest(planned.rules)
	}
	parts = append(parts, identity)
	planned.caseRef, planned.caseName, _ = loaded.caseOf(spec.Input.Case)
	if planned.caseRef != nil {
		review.Case, review.CaseName = planned.caseRef, planned.caseName
	}
	if options.Connected != nil {
		connected, err := selectConnectedMinimize(loaded, run, environment, *options.Connected)
		if err != nil {
			other = cmp.Or(other, err.Error())
		} else {
			planned.connected, review.Connected = connected, &connected.review
			parts = append(parts, connected.identity)
		}
	}
	if other == "" {
		preview, err := previewMinimize(planned)
		if err != nil {
			other = err.Error()
		} else {
			review.Groups, review.Unsupported = len(preview.Groups), preview.Unsupported
			for _, group := range preview.Groups {
				if group.Required {
					review.Pinned++
				}
			}
		}
	}
	ready := other == ""
	if !held && ready {
		if admission := a.admissionPreview(ctx); !admission.Admitted {
			ready, other, review.Refusal = false, admission.Reason, LicenseRefusal
		}
	}
	if ready && !destination.Fresh {
		ready, other = false, cmp.Or(destination.Reason, "the minimization needs a new folder")
	}
	parts = append(parts, strings.Join(options.Checks, "\x00"), options.Grouping, strconv.Itoa(options.Trials), strconv.Itoa(options.Confirmations), destination.Name)
	bound := &boundAction{action: MinimizeFailureAction, origin: request, minimize: planned, binding: binding(parts...),
		review: ActionReview{Items: items, Ready: ready, Refusal: other, Minimize: review, Requirements: []ReviewRequirement{}}}
	if planned.reset != nil {
		// Every manual step of the reset is confirmed once, for the whole
		// series, in the review's Start.
		bound.review.Reset = planned.reset.review.Reset
		bound.review.Requirements = slices.Clone(planned.reset.review.Requirements)
	}
	if planned.connected != nil {
		bound.review.Isolation = &planned.connected.review.Isolation
		if len(bound.review.Isolation.Manual) > 0 && !slices.Contains(bound.review.Requirements, ConfirmationsRequirement) {
			bound.review.Requirements = append(bound.review.Requirements, ConfirmationsRequirement)
		}
		if bound.review.Reset == nil {
			bound.review.Reset = &EnvironmentResetReview{Target: review.EnvironmentName, Name: planned.connected.review.Isolation.Name, Actions: []ResetReviewAction{}}
		}
		if bound.review.Reset != nil {
			reset := *bound.review.Reset
			reset.Actions = slices.Clone(reset.Actions)
			for _, manual := range bound.review.Isolation.Manual {
				if !slices.ContainsFunc(reset.Actions, func(action ResetReviewAction) bool { return action.ID == manual.ID }) {
					reset.Actions = append(reset.Actions, ResetReviewAction{ID: manual.ID, Name: manual.Name, Type: fixturereset.OperatorConfirms, Instructions: manual.Instructions})
				}
			}
			bound.review.Reset = &reset
			review.Resets = slices.Clone(reset.Actions)
		}
	}
	bound.review.Destination = ReviewDestination{Output: destination.Name}
	if environment != nil {
		bound.review.Destination.Name, bound.review.Destination.Address = environment.name, environment.members.target.Address
		bound.review.Destination.Classification = string(environment.members.target.Environment().Classification)
	}
	return bound, noRefusal
}

// readCaseIdentity is the identity one case verifies as now.
func readCaseIdentity(path string) (string, error) {
	opened, err := bundle.Open(path)
	if err != nil {
		return "", err
	}
	return opened.Identity, nil
}

func expectationIDs(checks []testauthor.Expectation) []string {
	ids := make([]string, 0, len(checks))
	for _, check := range checks {
		ids = append(ids, check.ID)
	}
	return ids
}

// previewMinimize reads how the reduction takes the sequence apart, with an
// oracle that works in a private folder removed before it answers.
func previewMinimize(binding *minimizeBinding) (reduce.Preview, error) {
	if binding.connected != nil {
		messages, required, err := reduce.InspectConnectedOracle(binding.connected.plan, binding.plan.Signature)
		if err != nil {
			return reduce.Preview{}, err
		}
		return reduce.PreviewPlan(reduce.Request{Case: binding.casePath, Plan: binding.plan, Rules: binding.rules, Messages: messages, Required: required})
	}
	scratch, err := os.MkdirTemp("", "readmit-minimize-preview-")
	if err != nil {
		return reduce.Preview{}, errors.New("the minimization cannot be previewed now")
	}
	defer os.RemoveAll(scratch)
	oracle, err := reduce.NewDurableOracle(reduce.OracleRequest{SpecPath: binding.specPath, Workspace: filepath.Join(scratch, "trials"),
		Signature: binding.plan.Signature, Target: binding.target, Resolve: sendpolicy.SystemResolver})
	if err != nil {
		return reduce.Preview{}, err
	}
	return reduce.PreviewPlan(reduce.Request{Case: binding.casePath, Plan: binding.plan, Rules: binding.rules,
		Messages: oracle.Messages(), Required: oracle.Required()})
}

// MinimizeProgress is the minimization running now: every trial so far, the
// one running, the budget and how many messages it started from.
type MinimizeProgress struct {
	Operation string         `json:"operation"`
	Groups    []reduce.Group `json:"groups"`
	Trials    []reduce.Trial `json:"trials"`
	Current   *reduce.Trial  `json:"current,omitzero"`
	Budget    int            `json:"budget"`
	Messages  int            `json:"messages"`
}

// MinimizeProgressResult answers what the running minimization has done;
// Progress is absent when none runs.
type MinimizeProgressResult struct {
	State    State             `json:"state"`
	Progress *MinimizeProgress `json:"progress,omitzero"`
}

// minimizeState is the minimization running now, which MinimizeProgress
// reads without taking the operation slot.
type minimizeState struct {
	mu       sync.Mutex
	progress *MinimizeProgress
}

// MinimizeProgress reads the running minimization. It takes no slot: it is
// what the series has done so far, read while it runs.
func (a *App) MinimizeProgress() MinimizeProgressResult {
	a.minimizing.mu.Lock()
	defer a.minimizing.mu.Unlock()
	if a.minimizing.progress == nil {
		return MinimizeProgressResult{State: Empty}
	}
	progress := *a.minimizing.progress
	progress.Trials = slices.Clone(progress.Trials)
	progress.Groups = slices.Clone(progress.Groups)
	return MinimizeProgressResult{State: Completed, Progress: &progress}
}

func executeMinimize(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	binding := bound.minimize
	review := bound.review.Minimize
	result := ReviewedActionResult{Outcome: ActionCompleted}
	output := filepath.Join(binding.root, binding.output)
	var oracle minimizeOracle
	var err error
	if binding.connected != nil {
		oracle, err = reduce.NewConnectedOracle(reduce.ConnectedOracleRequest{Plan: binding.connected.plan, Configuration: binding.connected.config, Workspace: output,
			Signature: binding.plan.Signature, Confirmed: slices.Clone(decisions.Confirmed), Authorize: a.authorizeMinimizeTrial(bound)})
	} else {
		reset := binding.reset.reset.request
		reset.Confirmed = slices.Clone(decisions.Confirmed)
		oracle, err = reduce.NewDurableOracle(reduce.OracleRequest{SpecPath: binding.specPath, Workspace: output, Signature: binding.plan.Signature,
			Target: binding.target, Reset: reset, Resolve: sendpolicy.SystemResolver})
	}
	if err != nil {
		result.refuse(Failed, err.Error()+"; nothing was reset or sent")
		return result
	}
	a.reach(reachingTarget{ref: "environment:" + review.Environment.ID, name: review.EnvironmentName, kind: ConnectionRun, destination: review.Address})
	progress := &MinimizeProgress{Operation: bound.executionReview.intent, Groups: []reduce.Group{}, Trials: []reduce.Trial{}, Budget: binding.plan.Trials, Messages: len(oracle.Messages())}
	if preview, err := previewMinimize(binding); err == nil {
		progress.Groups = preview.Groups
	}
	a.minimizing.mu.Lock()
	a.minimizing.progress = progress
	a.minimizing.mu.Unlock()
	defer func() {
		a.minimizing.mu.Lock()
		a.minimizing.progress = nil
		a.minimizing.mu.Unlock()
	}()
	request := reduce.Request{Case: binding.casePath, Plan: binding.plan, Rules: binding.rules, Messages: oracle.Messages(), Required: oracle.Required(),
		Progress: func(trial reduce.Trial, finished bool) {
			a.minimizing.mu.Lock()
			defer a.minimizing.mu.Unlock()
			if finished {
				progress.Trials, progress.Current = append(progress.Trials, trial), nil
			} else {
				progress.Current = &trial
			}
		}}
	report, err := reduce.Run(ctx, request, oracle)
	if err != nil {
		// Nothing ran: the engine refused the plan before its first trial.
		_ = os.Remove(output)
		result.refuse(Failed, err.Error()+"; nothing was reset or sent")
		return result
	}
	outcome := &MinimizeOutcome{Outcome: report.Outcome, Reason: report.Reason, Minimality: report.Minimality, Trials: nonNil(report.Trials), Groups: nonNil(report.Groups),
		Budget: binding.plan.Trials, Output: binding.output, Original: binding.messages, Retained: []TestMessage{}}
	for _, message := range binding.messages {
		if slices.Contains(report.Retained, message.ID) {
			outcome.Retained = append(outcome.Retained, message)
		}
	}
	if document, err := reduce.JSON(report); err == nil {
		_ = os.WriteFile(filepath.Join(output, "reduction.json"), document, 0o600)
	}
	result.State, result.Minimize = Completed, outcome
	stopped := errors.Is(ctx.Err(), context.Canceled) || report.Reason == reduce.OperatorStopped
	switch {
	case slices.ContainsFunc(report.Trials, func(trial reduce.Trial) bool {
		return trial.Reason == reduce.RunDeliveryUnknown || trial.Reason == reduce.CleanupUnresolved
	}):
		result.Outcome = ActionUncertain
	case stopped && report.Outcome == reduce.OutcomeUndecided:
		result.State, result.Outcome, result.Reason = Cancelled, ActionCancelled, "the minimization was stopped; no trial was resent"
	}
	if report.Outcome == reduce.OutcomeReduced && result.Outcome == ActionCompleted {
		outcome.Variant, outcome.VariantRefusal = a.publishMinimized(context.WithoutCancel(ctx), bound, report)
	}
	return result
}

type minimizeOracle interface {
	reduce.Oracle
	Messages() []string
	Required() []string
}

// publishMinimized publishes a reduced result as one variant of the case the
// run sent from: the messages the reduction kept, selected from it.
func (a *App) publishMinimized(ctx context.Context, bound *boundAction, report reduce.Report) (*ItemRef, string) {
	binding := bound.minimize
	if binding.caseRef == nil {
		return nil, "the case this test sends from is not an object of the project"
	}
	plan, err := reproducer.NewPlan(report.Case.Identity)
	if err != nil {
		return nil, err.Error()
	}
	for _, id := range report.Retained {
		plan.Steps = append(plan.Steps, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: id})
	}
	name := cmp.Or(binding.caseName, "Case") + " minimized"
	saved := a.saveItem(ctx, SaveItemRequest{Context: bound.origin.Context, Kind: VariantItem, IntentID: bound.executionReview.intent,
		Draft: ItemDraft{Name: name, Variant: &VariantDraft{Source: ItemRef{Kind: binding.caseRef.Kind, ID: binding.caseRef.ID}, Plan: plan}}})
	if saved.Saved == nil {
		return nil, cmp.Or(saved.Reason, "the reduced result could not be saved as a variant")
	}
	return saved.Saved, ""
}
