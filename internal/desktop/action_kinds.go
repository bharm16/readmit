package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// The reviewed actions of a named environment and observation: reading an
// observation's source once, running an environment's reset plan, and
// scanning the project's configuration files for its registered credentials.
// Each is prepared from the saved objects, binds exactly what its review
// shows, and runs only on its own final click.

// CollectReview is what one collection reads: the observation's revision,
// the source by its declared identity, kind and scope, the window's
// identity, the completion bounds it is held to, and where the source is
// read from.
type CollectReview struct {
	Revision    string             `json:"revision"`
	Source      string             `json:"source"`
	SourceType  string             `json:"source_type"`
	Scope       string             `json:"scope"`
	Window      string             `json:"window"`
	Bounds      observewindow.Rule `json:"bounds"`
	Destination string             `json:"destination"`
}

// ResetReviewAction is one saved reset action as its review shows it: its
// identity, the name a person gave it, its type, the instructions it
// carries and what it does. A check of an empty observation names the
// observation, at the revision whose collections it reads.
type ResetReviewAction struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Type            fixturereset.Operator `json:"type"`
	Instructions    string                `json:"instructions"`
	Effect          string                `json:"effect"`
	Observation     *ItemRef              `json:"observation,omitzero"`
	ObservationName string                `json:"observation_name,omitzero"`
}

// EnvironmentResetReview is the reset one environment's saved plan runs: the target it
// runs against and every action, in order.
type EnvironmentResetReview struct {
	Target  string              `json:"target"`
	Name    string              `json:"name,omitzero"`
	Actions []ResetReviewAction `json:"actions"`
}

// ScanReview is exactly what a credential scan reads: the project's
// configuration files, by entry, and the one reference it scans for, or
// none for every registered one.
type ScanReview struct {
	Files     []string `json:"files"`
	Reference string   `json:"reference,omitzero"`
}

// EnvironmentReset is what a reset actually did, action by action, and the
// project entry its outcome was retained in.
type EnvironmentReset struct {
	Result *fixturereset.Result `json:"result"`
	Output string               `json:"output"`
}

// ScanOutcome is what a credential scan found in the files it read.
type ScanOutcome struct {
	Scan    exportreview.Scan `json:"scan"`
	Skipped int               `json:"skipped"`
	Files   []string          `json:"files"`
}

type collectBinding struct {
	request     operation.ObservationCollectRequest
	output      string
	observation ItemRef
	stable      int
}

type resetBinding struct {
	request fixturereset.Request
	output  string
}

type scanBinding struct {
	secrets   string
	files     []string
	reference string
}

// fileDigest is the SHA-256 of one bounded file, or a marker it cannot be
// read.
func fileDigest(path string) string {
	data, err := boundedFile(path, catalog.MaxMemberBytes)
	if err != nil {
		return "unreadable"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sourceDestination is where a source is read from, as it declares it.
func sourceDestination(source observesource.Source) string {
	switch {
	case source.File != nil:
		return source.File.Path
	case source.HTTP != nil:
		return source.HTTP.URL
	case source.Capture != nil:
		return source.Capture.Path
	case source.Database != nil:
		return source.Database.Address
	}
	return ""
}

func bindCollect(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != ObservationItem || request.Destination != nil && request.Destination.Kind != EnvironmentItem {
		return nil, refusal{Failed, "a collection is reviewed for one observation, under an environment's send policy or none"}
	}
	refs := slices.Clone(request.Items)
	if request.Destination != nil {
		refs = append(refs, *request.Destination)
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, refs)
	if loaded == nil {
		return nil, declined
	}
	paths, _, _ := loaded.backing(records[0])
	if paths["window"] == "" {
		return nil, refusal{Failed, "an observation is collected through its window; save the observation with one first"}
	}
	source, window, err := operation.ValidateObservationPair(paths["source"], paths["window"])
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	_, sourceIdentity, err := operation.ValidateObservationSource(paths["source"])
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	// The source as it declares itself, for the review to show.
	declared, err := loaded.observationOf(records[0])
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	policyPath, policyDigest := "", ""
	if request.Destination != nil {
		environment, _, _ := loaded.backing(records[1])
		if path, held := environment["policy"]; held {
			policyPath, policyDigest = path, fileDigest(path)
		}
	}
	completion, snapshot, refused := completionOutput(loaded.root)
	if refused.state != "" {
		return nil, refused
	}
	ready, reason := source.Enabled, ""
	if !ready {
		reason = "collection is not enabled for this source; enable it and save the observation first"
	}
	review := &CollectReview{Revision: items[0].Ref.Revision, Source: source.Observes.Identity, SourceType: source.Observes.Kind, Scope: source.Observes.Scope,
		Window: window.Identity(), Bounds: window.Completion, Destination: sourceDestination(declared.Source)}
	collect := &collectBinding{output: completion, observation: items[0].Ref, stable: window.Completion.StableSamples, request: operation.ObservationCollectRequest{
		SourcePath: paths["source"], WindowPath: paths["window"], OutputPath: filepath.Join(loaded.root, completion),
		SnapshotPath: filepath.Join(loaded.root, snapshot), PolicyPath: policyPath, Authorize: true,
		ExpectedSourceIdentity: sourceIdentity, ExpectedWindowIdentity: window.Identity()}}
	return &boundAction{action: CollectObservationAction, origin: request, collect: collect,
		binding: binding(string(CollectObservationAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
			records[0].ID, items[0].Ref.Revision, memberDigest(records[0], "source"), memberDigest(records[0], "window"), sourceIdentity, window.Identity(),
			policyPath, policyDigest, completion, snapshot),
		review: ActionReview{Items: items[:1], Ready: ready, Refusal: reason, Collect: review,
			Destination: ReviewDestination{Name: items[0].Name, Address: review.Destination, Output: completion}}}, noRefusal
}

// executeCollect reads the bound source once and retains its completion. A
// collection that did not complete is reported with the reason it observed
// nothing, never as having found no records.
func executeCollect(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	a.reach(reachingTarget{ref: "observation:" + bound.origin.Items[0].ID, name: bound.review.Items[0].Name,
		kind: ConnectionSource, destination: bound.review.Destination.Address})
	request := bound.collect.request
	report, done := a.measureCollection(bound.collect.observation, bound.collect.stable)
	defer done()
	request.Progress = report
	completion, err := operation.CollectObservation(ctx, request)
	result := ReviewedActionResult{Outcome: ActionCompleted}
	switch {
	case err != nil && errors.Is(ctx.Err(), context.Canceled):
		result.State, result.Reason, result.Outcome = Cancelled, err.Error(), ActionCancelled
		return result
	case err != nil:
		result.State, result.Reason, result.Outcome = Failed, err.Error(), ActionRefused
		return result
	}
	row := collectionRow(bound.collect.output, completion)
	result.Collected = &row
	result.State = Completed
	if !row.Trustworthy {
		result.State, result.Reason = Failed, row.Reason
	}
	return result
}

// resetEffect is what one reset action does, in the terms its review shows.
func resetEffect(action fixturereset.Action, address string) string {
	switch action.Operator {
	case fixturereset.OperatorConfirms:
		return "You do this step yourself and confirm it; readmit changes nothing."
	case fixturereset.ObservationEmpty:
		return "Reads " + action.Observation + " once to check it is empty; nothing is changed."
	case fixturereset.CollectionEmpty:
		return "Reads the latest completed collection of " + action.Observation + " to check it found no records; nothing is collected or changed."
	case fixturereset.EndpointQuiet:
		return "Connects to " + address + "; no messages are sent."
	}
	return ""
}

// resetOutput is where a reset retains its outcome: a new entry of the
// project the application names.
var resetOutput = outputRule{prefix: "reset-outcome",
	invalid:   "a reset outcome is written to one new entry of the project",
	exhausted: "the project holds more reset outcomes than this release numbers",
	taken:     "that entry already exists; an outcome is written as a new entry",
}

func bindReset(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != EnvironmentItem {
		return nil, refusal{Failed, "a reset is reviewed for one environment"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	members, err := loaded.environmentOf(records[0])
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	if members.reset == nil {
		return nil, refusal{Failed, "this environment has no reset; add one and save it first"}
	}
	target, err := operation.ReadTarget(members.paths["target"])
	if err != nil {
		return nil, refusal{Failed, approvalReason(err)}
	}
	plan, err := boundedFile(members.paths["reset"], fixturereset.MaxPlanBytes)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	// A check of an empty observation reads the observation's current
	// revision, whichever revision the plan was saved against.
	current := *members.reset
	current.Actions = slices.Clone(current.Actions)
	observed := map[string]catalog.Item{}
	followed := []string{}
	missing := ""
	for i := range current.Actions {
		action := &current.Actions[i]
		if action.Operator != fixturereset.CollectionEmpty {
			continue
		}
		item, found := loaded.observationSaving(action.Observation)
		if !found || sourceMember(item) == "" {
			missing = "an observation this reset checks is no longer in the project; choose another in the reset first"
			continue
		}
		observed[action.ID] = item
		action.Observation = sourceMember(item)
		followed = append(followed, item.ID, item.RevisionLabel(), memberDigest(item, "source"))
	}
	if len(observed) > 0 {
		if plan, err = encodeMember(current); err != nil {
			return nil, refusal{Failed, err.Error()}
		}
	}
	destination, refused := resetOutput.destination(loaded.root, "")
	if refused.state != "" {
		return nil, refused
	}
	classification := target.Environment().Classification
	ready, reason := true, ""
	switch {
	case missing != "":
		ready, reason = false, missing
	case sendpolicy.RefusesEverySend(string(classification)):
		ready, reason = false, "a production environment is never reset"
	case classification != replay.Nonproduction:
		ready, reason = false, "only an environment recorded as nonproduction is reset"
	}
	review := &EnvironmentResetReview{Target: target.Name, Name: members.links.ResetName, Actions: []ResetReviewAction{}}
	requirements := []ReviewRequirement{}
	for i, action := range current.Actions {
		name := ""
		if i < len(members.links.ActionNames) {
			name = members.links.ActionNames[i]
		}
		reviewed := ResetReviewAction{ID: action.ID, Name: name, Type: action.Operator, Instructions: action.Instructions}
		if item, found := observed[action.ID]; found {
			reviewed.Observation = &ItemRef{Kind: ObservationItem, ID: item.ID, Revision: item.RevisionLabel()}
			reviewed.ObservationName = loaded.read(item).Name
			action.Observation = reviewed.ObservationName
		}
		reviewed.Effect = resetEffect(action, target.Address)
		review.Actions = append(review.Actions, reviewed)
		if action.Operator == fixturereset.OperatorConfirms && len(requirements) == 0 {
			requirements = append(requirements, ConfirmationsRequirement)
		}
	}
	reset := &resetBinding{output: destination.Name, request: fixturereset.Request{Target: target, PlanBytes: plan, PlanDirectory: loaded.root, Policy: members.policy}}
	return &boundAction{action: ResetEnvironmentAction, origin: request, reset: reset,
		binding: binding(string(ResetEnvironmentAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held),
			records[0].ID, items[0].Ref.Revision, memberDigest(records[0], "target"), memberDigest(records[0], "reset"), memberDigest(records[0], "policy"),
			memberDigest(records[0], "links"), fileDigest(members.paths["reset"]), string(classification), target.Address, destination.Name,
			strings.Join(followed, "\x00")),
		review: ActionReview{Items: items, Ready: ready && destination.Fresh, Refusal: cmp.Or(reason, destination.Reason), Reset: review, Requirements: requirements,
			Destination: ReviewDestination{Name: target.Name, Classification: string(classification), Address: target.Address, Output: destination.Name}}}, noRefusal
}

// executeReset runs the bound plan once with the manual steps the click
// confirmed, and reports every action's own outcome.
func executeReset(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	a.reach(reachingTarget{ref: "environment:" + bound.origin.Items[0].ID, name: bound.review.Items[0].Name,
		kind: ConnectionEnvironment, destination: bound.review.Destination.Address})
	request := bound.reset.request
	request.Confirmed = slices.Clone(decisions.Confirmed)
	outcome, _, err := operation.ResetEnvironment(ctx, request, filepath.Join(request.PlanDirectory, bound.reset.output), sendpolicy.SystemResolver)
	result := ReviewedActionResult{Outcome: ActionCompleted, Reset: &EnvironmentReset{Result: &outcome, Output: bound.reset.output}}
	switch {
	// Each action's own outcome and reason are in the result; the reason
	// here is the one sentence the window shows above them.
	case outcome.Outcome == fixturereset.Cancelled:
		result.State, result.Outcome, result.Reason = Cancelled, ActionCancelled, "the reset was stopped"
	case outcome.Outcome == fixturereset.Refused:
		result.State, result.Outcome, result.Reason = Failed, ActionRefused, "the reset was refused"
	case outcome.Outcome != fixturereset.Confirmed:
		result.State, result.Reason = Failed, "the reset did not complete"
	default:
		result.State = Completed
	}
	if err != nil {
		result.State, result.Reason = Failed, err.Error()
	}
	return result
}

func bindScan(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 0 {
		return nil, refusal{Failed, "a credential scan is reviewed for the project"}
	}
	options := ScanActionOptions{}
	if request.Scan != nil {
		options = *request.Scan
	}
	loaded, _, _, declined := a.scoped(ctx, request.Context, nil)
	if loaded == nil {
		return nil, declined
	}
	secrets := filepath.Join(loaded.root, ProjectSecrets)
	document, err := operation.ReadSecrets(secrets)
	if err != nil {
		return nil, refusal{Failed, "the project registers no credentials to scan for"}
	}
	if options.Reference != "" {
		if _, err := secret.Find(document, options.Reference); err != nil {
			return nil, refusal{Failed, err.Error()}
		}
	}
	files := []string{}
	for _, item := range loaded.document.Items {
		if item.Kind != string(EnvironmentItem) && item.Kind != string(ObservationItem) || loaded.removed(item) {
			continue
		}
		if current := item.Current(); current != nil {
			for _, member := range current.Members {
				files = append(files, member.Path)
			}
		} else if item.Entry != "" {
			files = append(files, item.Entry)
		}
	}
	slices.Sort(files)
	files = slices.Compact(files)
	parts := []string{string(ScanSecretsAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, false, held),
		fileDigest(secrets), options.Reference}
	for _, file := range files {
		parts = append(parts, file, fileDigest(filepath.Join(loaded.root, file)))
	}
	listed := append([]string{ProjectSecrets}, files...)
	return &boundAction{action: ScanSecretsAction, origin: request, scan: &scanBinding{secrets: secrets, files: files, reference: options.Reference},
		binding: binding(parts...),
		review:  ActionReview{Ready: true, Scan: &ScanReview{Files: listed, Reference: options.Reference}}}, noRefusal
}

// executeScan scans exactly the bound files for the registered credentials.
func executeScan(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	root := filepath.Dir(bound.scan.secrets)
	paths := make([]string, 0, len(bound.scan.files))
	for _, file := range bound.scan.files {
		paths = append(paths, filepath.Join(root, file))
	}
	scan, skipped, err := operation.ScanSecrets(ctx, bound.scan.secrets, paths, bound.scan.reference)
	if err != nil {
		result := ReviewedActionResult{Outcome: ActionRefused}
		result.refuse(Failed, err.Error())
		return result
	}
	return ReviewedActionResult{State: Completed, Outcome: ActionCompleted,
		Scan: &ScanOutcome{Scan: scan, Skipped: skipped, Files: append([]string{ProjectSecrets}, bound.scan.files...)}}
}
