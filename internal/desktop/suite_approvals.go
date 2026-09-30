package desktop

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/hubclient"
	"github.com/bharm16/readmit/internal/hubprotocol"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/suite"
)

// The approvals of a suite are its append-only history: one revision of the
// suite's SuiteApprovalItem per approval, each bound to one exact suite
// version and never retargeted, as a relationship review keeps its decisions
// (ADR 0023). Four kinds are kept apart, each with its own actor:
//
//   - a local baseline releases every test version the version pins, as a
//     readmit-test-release/v1 approved under the local reviewer's name,
//     each continuing the release history of its test;
//   - a team review request asks one reviewer, through the signed-in
//     customer hub, to review those exact release bytes;
//   - a team release approval answers that request, as the signed-in
//     reviewer the request was addressed to;
//   - an environment approval approves the version and its release pins for
//     one of its environments under an operator's target revision, through
//     suite.ApprovePromotion, and records a local approval. It never deploys
//     and never authorizes a send.
//
// A release is materialized in a private temporary folder only while a
// promotion review or a hub upload reads it, and removed afterwards; the
// approval keeps its exact bytes.

// suiteApprovalRecord is one approval of a suite version as its approval
// history records it, the readmit-suite-approval/v1 document.
type suiteApprovalRecord struct {
	Schema      string               `json:"schema"`
	Scope       SuiteApprovalScope   `json:"scope"`
	Suite       string               `json:"suite"`
	Revision    string               `json:"revision"`
	SuiteSHA256 string               `json:"suite_sha256"`
	Actor       string               `json:"actor"`
	At          string               `json:"at"`
	Reason      string               `json:"reason"`
	Tests       []approvedTest       `json:"tests"`
	Environment *approvedEnvironment `json:"environment,omitzero"`
	Reviewer    string               `json:"reviewer,omitzero"`
	Hub         []approvalEvent      `json:"hub,omitzero"`
}

// approvedTest is one suite test an approval binds: the test version it
// pins and the exact bytes and identity of that version's release.
type approvedTest struct {
	Test    string  `json:"test"`
	Ref     ItemRef `json:"ref"`
	Release string  `json:"release"`
	Bytes   []byte  `json:"bytes"`
}

// approvedEnvironment is what an environment approval binds beyond the
// version: the suite environment, the environment revisions its bindings
// followed, the operator's target revision and the promotion approval
// suite.ApprovePromotion wrote.
type approvedEnvironment struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Site           string    `json:"site"`
	Environments   []ItemRef `json:"environments"`
	TargetRevision string    `json:"target_revision"`
	Promotion      []byte    `json:"promotion"`
}

// approvalEvent is one review event the customer hub recorded for a team
// request or release.
type approvalEvent struct {
	Project  string `json:"project"`
	Sequence int    `json:"sequence"`
	Command  string `json:"command"`
	Kind     string `json:"kind"`
	Release  string `json:"release"`
	Actor    string `json:"actor"`
	At       string `json:"at"`
}

// approvalScopes are the kinds of approval a history records.
var approvalScopes = []SuiteApprovalScope{BaselineApproval, ReviewRequested, ReleaseApproval, EnvironmentApproval}

// decodeSuiteApproval reads one approval exactly as written: its scope,
// suite and version, and every release it binds verifying as the release it
// names.
func decodeSuiteApproval(data []byte) (suiteApprovalRecord, error) {
	var record suiteApprovalRecord
	invalid := errors.New("the suite approval cannot be read")
	if len(data) > catalog.MaxMemberBytes || json.Unmarshal(data, &record, json.RejectUnknownMembers(true)) != nil || record.Schema != SuiteApprovalSchema ||
		!slices.Contains(approvalScopes, record.Scope) || !catalog.ValidID(record.Suite) || record.Revision == "" || !suiteText(record.Actor, 256) {
		return suiteApprovalRecord{}, invalid
	}
	if _, err := time.Parse(time.RFC3339, record.At); err != nil {
		return suiteApprovalRecord{}, invalid
	}
	for _, test := range record.Tests {
		release, err := expectation.Decode(test.Bytes)
		if err != nil || release.Identity() != test.Release {
			return suiteApprovalRecord{}, invalid
		}
	}
	if (record.Scope == EnvironmentApproval) != (record.Environment != nil) {
		return suiteApprovalRecord{}, invalid
	}
	if record.Environment != nil {
		if _, err := suite.DecodePromotion(record.Environment.Promotion); err != nil {
			return suiteApprovalRecord{}, invalid
		}
	}
	return record, nil
}

func readSuiteApproval(path string) (suiteApprovalRecord, error) {
	data, err := boundedFile(path, catalog.MaxMemberBytes)
	if err != nil {
		return suiteApprovalRecord{}, err
	}
	return decodeSuiteApproval(data)
}

// readSuiteApprovalItem reads a suite's approval history, which is listed
// only as its suite's history.
func readSuiteApprovalItem(_ *loadedCatalog, _ catalog.Item, paths map[string]string) (view, error) {
	_, err := readSuiteApproval(paths[primaryRole(SuiteApprovalItem)])
	return view{}, err
}

// suiteApprovals are every approval the project records for one suite,
// oldest first.
func (c *loadedCatalog) suiteApprovals(suiteID string) []suiteApprovalRecord {
	records := []suiteApprovalRecord{}
	for _, item := range c.document.Items {
		if item.Kind != string(SuiteApprovalItem) || c.removed(item) {
			continue
		}
		for _, revision := range item.Revisions {
			paths, availability, _ := c.revisionBacking(item, strconv.Itoa(revision.Number))
			if availability != ItemAvailable {
				continue
			}
			if record, err := readSuiteApproval(paths[primaryRole(SuiteApprovalItem)]); err == nil && record.Suite == suiteID {
				records = append(records, record)
			}
		}
	}
	slices.SortStableFunc(records, func(x, y suiteApprovalRecord) int { return cmp.Compare(x.At, y.At) })
	return records
}

// suiteApprovalHistory is the catalog item holding one suite's approval
// history, or nil before its first approval.
func (c *loadedCatalog) suiteApprovalHistory(suiteID string) *catalog.Item {
	for i, item := range c.document.Items {
		if item.Kind != string(SuiteApprovalItem) || c.removed(item) || item.Current() == nil {
			continue
		}
		paths, availability, _ := c.backing(item)
		if availability != ItemAvailable {
			continue
		}
		if record, err := readSuiteApproval(paths[primaryRole(SuiteApprovalItem)]); err == nil && record.Suite == suiteID {
			return &c.document.Items[i]
		}
	}
	return nil
}

// latestBaseline is the latest baseline of one version of a suite: the
// release pins its team review and environment approvals bind.
func latestBaseline(records []suiteApprovalRecord, revision string) *suiteApprovalRecord {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Scope == BaselineApproval && records[i].Revision == revision {
			return &records[i]
		}
	}
	return nil
}

// sameReleases reports whether two approvals bind the same releases to the
// same suite tests.
func sameReleases(x, y []approvedTest) bool {
	return slices.EqualFunc(x, y, func(a, b approvedTest) bool { return a.Test == b.Test && a.Release == b.Release })
}

// approvalView is one recorded approval as a suite's history shows it, with
// whether it still binds what it approved: a baseline and a team release
// stay current for their version; a review request is stale once a later
// baseline of the version released other test versions; an environment
// approval is stale once an environment it bound moved to a newer revision
// or the version's release pins changed. A stale approval is never renewed.
func (c *loadedCatalog) approvalView(item catalog.Item, record suiteApprovalRecord, records []suiteApprovalRecord) SuiteApproval {
	at := record.At
	shown := SuiteApproval{Scope: record.Scope, Revision: record.Revision, Actor: record.Actor, At: &at, Reviewer: record.Reviewer,
		Reason: record.Reason, Current: true}
	stale := ""
	pins := latestBaseline(records, record.Revision)
	switch record.Scope {
	case ReviewRequested:
		if pins == nil || !sameReleases(pins.Tests, record.Tests) {
			stale = "a later baseline of this version released other test versions"
		}
	case EnvironmentApproval:
		shown.Environment, shown.TargetRevision = record.Environment.Name, record.Environment.TargetRevision
		for _, bound := range record.Environment.Environments {
			index := c.document.Find(bound.ID)
			switch {
			case index < 0 || c.removed(c.document.Items[index]):
				stale = cmp.Or(stale, "an environment this approval bound is no longer in the project")
			case c.document.Items[index].RevisionLabel() != bound.Revision:
				stale = cmp.Or(stale, c.nameOf(bound, "an environment")+" changed since this approval: version "+bound.Revision+", now version "+c.document.Items[index].RevisionLabel())
			}
		}
		if pins == nil || !sameReleases(pins.Tests, record.Tests) {
			stale = cmp.Or(stale, "the released tests of this version changed since this approval")
		}
	}
	if stale != "" {
		shown.Current, shown.Stale = false, stale
	}
	return shown
}

// releaseID is the identity a test's releases share, whichever suite
// releases it: the test's own, so every release of it continues one
// history.
func releaseID(test ItemRef) string { return "test-" + test.ID }

// latestRelease is the latest local release of one test, in any suite's
// baselines, which its next release continues.
func (c *loadedCatalog) latestRelease(id string) *expectation.Release {
	var latest *expectation.Release
	for _, item := range c.document.Items {
		if item.Kind != string(SuiteApprovalItem) || c.removed(item) {
			continue
		}
		for _, revision := range item.Revisions {
			paths, availability, _ := c.revisionBacking(item, strconv.Itoa(revision.Number))
			if availability != ItemAvailable {
				continue
			}
			record, err := readSuiteApproval(paths[primaryRole(SuiteApprovalItem)])
			if err != nil || record.Scope != BaselineApproval {
				continue
			}
			for _, test := range record.Tests {
				release, err := expectation.Decode(test.Bytes)
				if err == nil && release.ID == id && (latest == nil || release.Baseline.Revision > latest.Baseline.Revision) {
					latest = &release
				}
			}
		}
	}
	return latest
}

// suiteApprovalBinding is what one suite approval review resolved, which
// its final action records.
type suiteApprovalBinding struct {
	scope   SuiteApprovalScope
	suite   catalog.Item
	version *suiteVersion
	actor   string
	tests   []baselineTest
	pins    *suiteApprovalRecord
	// environment, its bound environment revisions, the target revision and
	// the compiled suite are an environment approval's.
	environment    *SuiteEnvironment
	environments   []ItemRef
	targetRevision string
	compiled       []byte
	reviewed       string
	// project, reviewer and requests are a team review's.
	project  string
	reviewer string
	requests []hubprotocol.ReviewEvent
}

// baselineTest is one suite test a baseline releases: the exact version
// bytes it releases, the release it continues and the review identity of
// the new release, or the existing release of exactly this version.
type baselineTest struct {
	test     SuiteTestDraft
	data     []byte
	previous *expectation.Release
	review   string
	reuse    []byte
}

func bindSuiteBaseline(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	return a.bindSuiteApproval(ctx, request, held, BaselineApproval, ApproveSuiteBaselineAction)
}

func bindSuiteReviewRequest(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	return a.bindSuiteApproval(ctx, request, held, ReviewRequested, RequestSuiteReviewAction)
}

func bindSuiteRelease(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	return a.bindSuiteApproval(ctx, request, held, ReleaseApproval, ApproveSuiteReleaseAction)
}

func bindPromotion(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	return a.bindSuiteApproval(ctx, request, held, EnvironmentApproval, ApprovePromotionAction)
}

// bindSuiteApproval reviews one approval of one saved suite version: the
// exact version and the test versions it pins, what changed since the
// version last approved the same way, and who it records as the actor. The
// binding covers everything the review shows, so any change to the version,
// its releases, an environment, the hub's requests or the signed-in person
// answers the final click as stale with a refreshed review.
func (a *App) bindSuiteApproval(ctx context.Context, request PrepareActionRequest, held bool, scope SuiteApprovalScope, action ActionID) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != SuiteItem {
		return nil, refusal{Failed, "a suite approval is reviewed for one suite version"}
	}
	loaded, declined := a.loadCatalog(ctx, request.Context, false)
	if loaded == nil {
		return nil, declined
	}
	loaded, index, declined := a.findAcross(ctx, request.Context, loaded, request.Items[0].ID, false)
	if loaded == nil {
		return nil, declined
	}
	if index < 0 || loaded.document.Items[index].Kind != string(SuiteItem) || loaded.removed(loaded.document.Items[index]) {
		return nil, refusal{Failed, "the project holds no such suite"}
	}
	item := loaded.document.Items[index]
	label := cmp.Or(request.Items[0].Revision, currentLabel(item))
	if label == originalVersion {
		return nil, refusal{Failed, "Save this suite to approve it."}
	}
	version, err := loaded.suiteVersion(item, label)
	if err != nil {
		return nil, refusal{Failed, "this suite cannot be read: " + err.Error()}
	}
	if !version.runnable() {
		return nil, refusal{Failed, notRunnable}
	}
	if version.connected != nil {
		return nil, refusal{Failed, "this connected suite retains exact connected releases and promotion; a legacy baseline or team review cannot approve them"}
	}
	options := SuiteApprovalOptions{}
	if request.SuiteApproval != nil {
		options = *request.SuiteApproval
	}
	records := loaded.suiteApprovals(item.ID)
	bound := &suiteApprovalBinding{scope: scope, suite: item, version: version, tests: []baselineTest{}}
	display := SuiteApprovalReview{Scope: scope, Suite: loaded.suiteName(item), Version: label, Tests: []SuiteApprovalTest{}, Targets: []SuiteApprovalTarget{}}
	ready, refused := true, ""
	notReady := func(reason string) {
		if ready {
			ready, refused = false, reason
		}
	}

	// The version compared with: the one the person chose, or else the latest
	// other version approved the same way.
	from := options.From
	if from == "" {
		for _, record := range slices.Backward(records) {
			if record.Scope == scope && record.Revision != label {
				from = record.Revision
				break
			}
		}
	}
	comparison, err := loaded.compareSuite(item, from, label)
	if err != nil {
		return nil, refusal{Failed, "the version compared with cannot be read: " + err.Error()}
	}
	display.Comparison = &comparison

	switch scope {
	case BaselineApproval:
		bound.actor = a.reviewerName()
		if declined := loaded.planBaseline(bound, &display); declined.state != "" {
			return nil, declined
		}
	default:
		bound.pins = latestBaseline(records, label)
		if bound.pins == nil {
			notReady("Approve a baseline of this version first: this approval binds the test releases a baseline records")
		} else {
			for _, test := range bound.pins.Tests {
				shown := SuiteApprovalTest{Name: loaded.nameOf(test.Ref, test.Test), Version: test.Ref.Revision}
				if release, err := expectation.Decode(test.Bytes); err == nil {
					shown.Release = strconv.Itoa(release.Baseline.Revision)
				}
				display.Tests = append(display.Tests, shown)
			}
		}
	}
	switch scope {
	case EnvironmentApproval:
		bound.actor = a.reviewerName()
		declined := loaded.planEnvironmentApproval(bound, &display, options, notReady)
		if declined.state != "" {
			return nil, declined
		}
	case ReviewRequested, ReleaseApproval:
		declined := a.planTeamReview(ctx, bound, &display, options, notReady)
		if declined.state != "" {
			return nil, declined
		}
	}
	display.Actor = bound.actor
	shown, _ := json.Marshal(display, json.Deterministic(true))
	parts := []string{string(action), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, false, held),
		item.ID, label, digestOf(version.data), string(shown), bound.reviewed}
	for _, test := range bound.tests {
		parts = append(parts, digestOf(test.data), test.review, string(test.reuse))
	}
	for _, request := range bound.requests {
		parts = append(parts, request.Command.ID, strconv.Itoa(request.Sequence))
	}
	items := []CatalogItem{loaded.read(item)}
	return &boundAction{action: action, origin: request, suiteApproval: bound, binding: binding(parts...),
		review: ActionReview{Items: items, Ready: ready, Refusal: refused, SuiteApproval: &display}}, noRefusal
}

// planBaseline resolves every test version a baseline releases: a version
// its test's latest release already holds exactly is kept as that release;
// any other is reviewed as the next release of its test.
func (c *loadedCatalog) planBaseline(bound *suiteApprovalBinding, display *SuiteApprovalReview) refusal {
	pinned := map[string]string{}
	for _, test := range bound.version.draft.Tests {
		saved, name, _, reason := c.pinnedTest(test)
		if reason != "" {
			return refusal{Failed, name + ": " + reason}
		}
		if revision, held := pinned[test.Test.ID]; held && revision != test.Test.Revision {
			return refusal{Failed, "two tests of this suite pin different versions of " + name + "; a baseline releases one version of a test at a time"}
		}
		pinned[test.Test.ID] = test.Test.Revision
		planned := baselineTest{test: test, data: saved.data, previous: c.latestRelease(releaseID(test.Test))}
		shown := SuiteApprovalTest{Name: name, Version: test.Test.Revision}
		canonical, _ := json.Marshal(saved.spec, json.Deterministic(true))
		if previous := planned.previous; previous != nil && len(previous.Profiles) == 0 {
			released, _ := json.Marshal(previous.Baseline.Spec, json.Deterministic(true))
			if string(released) == string(canonical) {
				planned.reuse, _ = previous.Encode()
				shown.Release = strconv.Itoa(previous.Baseline.Revision)
			}
		}
		if planned.reuse == nil {
			review, err := expectation.Review(releaseID(test.Test), saved.data, []profileversion.Version{}, planned.previous, false)
			if err != nil {
				return refusal{Failed, name + ": " + err.Error()}
			}
			planned.review, shown.Release = review.Identity, strconv.Itoa(review.Revision)
		}
		bound.tests = append(bound.tests, planned)
		display.Tests = append(display.Tests, shown)
	}
	return refusal{}
}

// planEnvironmentApproval reviews the version and its release pins for one
// of its environments under the operator's target revision, exactly as
// `readmit suite review-promotion` does, over the version compiled against
// the current revision of each environment it binds.
func (c *loadedCatalog) planEnvironmentApproval(bound *suiteApprovalBinding, display *SuiteApprovalReview, options SuiteApprovalOptions, notReady func(string)) refusal {
	draft := bound.version.draft
	at := slices.IndexFunc(draft.Environments, func(environment SuiteEnvironment) bool {
		return environment.ID == options.Environment || environment.Name == options.Environment
	})
	if at < 0 {
		return refusal{Failed, "choose one of this suite's environments"}
	}
	environment := draft.Environments[at]
	bound.environment, bound.targetRevision = &environment, strings.TrimSpace(options.Revision)
	display.Environment, display.Site, display.TargetRevision = environment.Name, environment.Site, bound.targetRevision
	plan, problems := c.planSuite(draft)
	if len(problems) > 0 {
		notReady("this version no longer compiles against the project: " + firstProblem(problems))
		return refusal{}
	}
	for m, binding := range environment.Bindings {
		revision := plan.environments[at][m]
		shown := SuiteApprovalTarget{Parameter: binding.Parameter, Target: c.nameOf(binding.Target, binding.TargetSource), Version: revision.Revision}
		if binding.Observation != nil {
			shown.Observation = c.nameOf(*binding.Observation, "")
		}
		display.Targets = append(display.Targets, shown)
		if !slices.Contains(bound.environments, revision) {
			bound.environments = append(bound.environments, revision)
		}
	}
	if bound.targetRevision == "" {
		notReady("Enter the target revision this approval assumes")
		return refusal{}
	}
	if bound.pins == nil {
		return refusal{}
	}
	_, compiled, declined := c.compiled(bound.version)
	if declined.state != "" {
		notReady(declined.reason)
		return refusal{}
	}
	bound.compiled = compiled
	review, err := c.promotion(bound, func(path, sidecar string) (string, error) {
		reviewed, err := suite.ReviewPromotion(path, environment.ID, sidecar, bound.targetRevision)
		return reviewed.Identity(), err
	})
	if err != nil {
		notReady(err.Error())
		return refusal{}
	}
	bound.reviewed = review
	return refusal{}
}

// promotion runs one promotion step over a bound environment approval: the
// compiled suite placed in the project root, where its references resolve,
// and its release pins in a private folder, each removed afterwards.
func (c *loadedCatalog) promotion(bound *suiteApprovalBinding, step func(path, sidecar string) (string, error)) (string, error) {
	_, sidecar, remove, err := materializeReleases(bound.pins.Tests)
	if err != nil {
		return "", err
	}
	defer remove()
	path, removeSuite, err := placeCompiled(c.root, bound.compiled)
	if err != nil {
		return "", err
	}
	defer removeSuite()
	return step(path, sidecar)
}

// materializeReleases writes the releases an approval recorded into a new
// private folder, beside the readmit-suite-releases/v1 sidecar naming each
// by its suite test and exact identity, and answers the folder, the
// sidecar's path and the function that removes them.
func materializeReleases(tests []approvedTest) (string, string, func(), error) {
	folder, err := os.MkdirTemp("", "readmit-suite-releases-")
	if err != nil {
		return "", "", func() {}, errors.New("the released tests cannot be held for this review")
	}
	remove := func() { os.RemoveAll(folder) }
	references := suite.ReleaseReferences{Schema: suite.ReleasesSchema, Tests: []suite.ReleaseReference{}}
	for _, test := range tests {
		name := "release-" + test.Test + ".json"
		if err := os.WriteFile(filepath.Join(folder, name), test.Bytes, 0o600); err != nil {
			remove()
			return "", "", func() {}, errors.New("the released tests cannot be held for this review")
		}
		references.Tests = append(references.Tests, suite.ReleaseReference{Test: test.Test, Release: name, Identity: test.Release})
	}
	data, err := json.Marshal(references, json.Deterministic(true))
	sidecar := filepath.Join(folder, "releases.json")
	if err == nil {
		err = os.WriteFile(sidecar, data, 0o600)
	}
	if err != nil {
		remove()
		return "", "", func() {}, errors.New("the released tests cannot be held for this review")
	}
	return folder, sidecar, remove, nil
}

// hubProject is the customer hub project a suite's team reviews are made
// in: the one project the selected hub configuration names. A configuration
// naming several is refused rather than one being chosen for the person.
func (a *App) hubProject() (string, refusal) {
	status := a.hub.Status()
	switch {
	case status.Config == nil || len(status.Config.Projects) == 0:
		return "", refusal{Failed, "the selected customer hub configuration names no project"}
	case len(status.Config.Projects) > 1:
		return "", refusal{Failed, "the selected customer hub configuration names more than one project; select a configuration for this project's team"}
	}
	return status.Config.Projects[0], refusal{}
}

// signedIn is the signed-in hub session a team review is made by, as the
// window reports its absence: no connection fails, and no current session is
// permission denied.
func (a *App) signedIn() (*hubclient.Client, *hubclient.Session, refusal) {
	client, session, err := a.hub.SignedIn()
	switch {
	case errors.Is(err, hubclient.ErrNotConnected):
		return nil, nil, refusal{Failed, "connect to the customer hub and sign in to review a suite with the team"}
	case err != nil:
		return nil, nil, refusal{PermissionDenied, "sign in to the customer hub to review a suite with the team"}
	}
	return client, session, refusal{}
}

// planTeamReview reviews a team request or release of the version's release
// pins: made by the signed-in hub subject, a request names the reviewer it
// asks; a release answers the outstanding request of each release that was
// addressed to that subject by someone else.
func (a *App) planTeamReview(ctx context.Context, bound *suiteApprovalBinding, display *SuiteApprovalReview, options SuiteApprovalOptions, notReady func(string)) refusal {
	client, session, declined := a.signedIn()
	if declined.state != "" {
		return declined
	}
	project, declined := a.hubProject()
	if declined.state != "" {
		return declined
	}
	bound.actor, bound.project = session.Subject, project
	if bound.scope == ReviewRequested {
		bound.reviewer = strings.TrimSpace(options.Reviewer)
		display.Reviewer = bound.reviewer
		switch {
		case bound.reviewer == "":
			notReady("Choose the reviewer this request asks")
		case bound.reviewer == session.Subject:
			notReady("A review is requested of someone other than yourself")
		}
		return refusal{}
	}
	if bound.pins == nil {
		return refusal{}
	}
	history, err := client.ListHistory(ctx, project)
	if err != nil {
		return refusal{Failed, "the customer hub's review history cannot be read: " + err.Error()}
	}
	reviews := hubprotocol.DeriveReviews(history.Events)
	requesters := []string{}
	for _, digest := range releaseDigests(bound.pins.Tests) {
		request, held := reviews.ReleaseRequest(digest)
		if !held || request.Command.Recipient != session.Subject || request.Actor == session.Subject {
			notReady("No outstanding review request of this version's releases is addressed to you")
			return refusal{}
		}
		bound.requests = append(bound.requests, request)
		if !slices.Contains(requesters, request.Actor) {
			requesters = append(requesters, request.Actor)
		}
	}
	display.Request = strings.Join(requesters, ", ")
	return refusal{}
}

// releaseDigests are the digests of the distinct releases an approval
// binds, in order: the exact bytes a hub review names.
func releaseDigests(tests []approvedTest) []string {
	digests := []string{}
	for _, test := range tests {
		if digest := digestOf(test.Bytes); !slices.Contains(digests, digest) {
			digests = append(digests, digest)
		}
	}
	return digests
}

func executeSuiteApproval(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	approval := bound.suiteApproval
	reason := strings.TrimSpace(decisions.Rationale)
	record := suiteApprovalRecord{Schema: SuiteApprovalSchema, Scope: approval.scope, Suite: approval.suite.ID, Revision: approval.version.label,
		SuiteSHA256: digestOf(approval.version.data), Actor: approval.actor, At: catalog.Stamp(a.now()), Reason: reason, Tests: []approvedTest{}}
	refuse := func(state State, text string) ReviewedActionResult {
		result := ReviewedActionResult{Outcome: ActionRefused}
		result.refuse(state, text)
		return result
	}
	switch approval.scope {
	case BaselineApproval:
		released := map[string][]byte{}
		for _, test := range approval.tests {
			data := test.reuse
			if data == nil {
				data = released[releaseID(test.test.Test)]
			}
			if data == nil {
				release, err := expectation.Approve(releaseID(test.test.Test), test.data, []profileversion.Version{}, test.previous, test.review, approval.actor, reason)
				if err == nil {
					data, err = release.Encode()
				}
				if err != nil {
					return refuse(Failed, "the baseline was not recorded: "+err.Error())
				}
				released[releaseID(test.test.Test)] = data
			}
			release, err := expectation.Decode(data)
			if err != nil {
				return refuse(Failed, "the baseline was not recorded: "+err.Error())
			}
			record.Tests = append(record.Tests, approvedTest{Test: test.test.ID, Ref: test.test.Test, Release: release.Identity(), Bytes: data})
		}
	case EnvironmentApproval:
		record.Tests = approval.pins.Tests
		var promotion []byte
		loaded, declined := a.loadCatalog(ctx, bound.origin.Context, false)
		if loaded == nil {
			return refuse(declined.state, declined.reason)
		}
		_, err := loaded.promotion(approval, func(path, sidecar string) (string, error) {
			output := filepath.Join(filepath.Dir(sidecar), "promotion.json")
			approved, err := suite.ApprovePromotion(path, approval.environment.ID, sidecar, approval.targetRevision, approval.reviewed, approval.actor, reason, output)
			if err != nil {
				return "", err
			}
			promotion, err = os.ReadFile(output)
			return approved.Identity(), err
		})
		if err != nil {
			return refuse(Failed, "the environment approval was not recorded: "+err.Error())
		}
		record.Environment = &approvedEnvironment{ID: approval.environment.ID, Name: approval.environment.Name, Site: approval.environment.Site,
			Environments: approval.environments, TargetRevision: approval.targetRevision, Promotion: promotion}
	case ReviewRequested, ReleaseApproval:
		record.Tests, record.Reviewer = approval.pins.Tests, approval.reviewer
		events, declined := a.postTeamReview(ctx, bound, reason)
		if declined.state != "" {
			return refuse(declined.state, declined.reason)
		}
		record.Hub = events
	}
	recorded, declined := a.recordSuiteApproval(ctx, bound, record)
	if declined.state != "" {
		return refuse(declined.state, declined.reason)
	}
	return ReviewedActionResult{State: Completed, Outcome: ActionCompleted, SuiteApproval: recorded}
}

// postTeamReview posts one hub review command per distinct release the
// approval binds — each release uploaded exactly for a request — as the
// signed-in subject, and answers the events the hub recorded. The hub stays
// the authority: it re-reads each release and enforces the request and
// approval chain.
func (a *App) postTeamReview(ctx context.Context, bound *boundAction, reason string) ([]approvalEvent, refusal) {
	approval := bound.suiteApproval
	client, _, declined := a.signedIn()
	if declined.state != "" {
		return nil, declined
	}
	folder, _, remove, err := materializeReleases(approval.pins.Tests)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	defer remove()
	kind := "review-request"
	if approval.scope == ReleaseApproval {
		kind = "approval"
	}
	intent := ""
	if bound.executionReview != nil {
		intent = bound.executionReview.intent
	}
	events := []approvalEvent{}
	for _, digest := range releaseDigests(approval.pins.Tests) {
		review := hubclient.ReleaseReview{Project: approval.project, ID: hubCommandID(intent, kind, digest), Kind: kind, Release: digest, Text: reason}
		if kind == "review-request" {
			at := slices.IndexFunc(approval.pins.Tests, func(test approvedTest) bool { return digestOf(test.Bytes) == digest })
			review.Path, review.Recipient = filepath.Join(folder, "release-"+approval.pins.Tests[at].Test+".json"), approval.reviewer
		}
		event, _, err := client.PostReleaseReview(ctx, review)
		if err != nil {
			reason := "the customer hub refused the review: " + err.Error()
			if len(events) > 0 {
				reason += "; " + strconv.Itoa(len(events)) + " of the version's releases were recorded there first, and nothing was recorded here"
			}
			return nil, refusal{Failed, reason}
		}
		events = append(events, approvalEvent{Project: approval.project, Sequence: event.Sequence, Command: event.Command.ID, Kind: event.Command.Kind,
			Release: event.Command.Release, Actor: event.Actor, At: event.At})
	}
	return events, refusal{}
}

// hubCommandID is the identity of one hub review command: the click that
// made it, its kind and the release it names, so a retried click repeats the
// same command and two releases never share one.
func hubCommandID(intent, kind, digest string) string {
	sum := sha256.Sum256([]byte(intent + "\x00" + kind + "\x00" + digest))
	return "suite-" + hex.EncodeToString(sum[:])[:32]
}

// recordSuiteApproval publishes one approval as the next revision of its
// suite's approval history, or the first of a new one.
func (a *App) recordSuiteApproval(ctx context.Context, bound *boundAction, record suiteApprovalRecord) (*SuiteApproval, refusal) {
	data, err := encodeMember(record)
	if err != nil {
		return nil, refusal{Failed, "the approval cannot be encoded"}
	}
	if len(data) > catalog.MaxMemberBytes {
		return nil, refusal{Failed, "the approval's releases are larger than one approval holds; nothing was recorded"}
	}
	loaded, declined := a.loadCatalog(ctx, bound.origin.Context, true)
	if loaded == nil {
		return nil, declined
	}
	intent := ""
	if bound.executionReview != nil {
		intent = bound.executionReview.intent
	}
	sum := sha256.Sum256([]byte("suite-approval\x00" + intent))
	draft := catalog.Draft{Kind: string(SuiteApprovalItem), Intent: "suite-approval-" + hex.EncodeToString(sum[:16]), Digest: digestOf(data),
		Author: a.reviewerName(), Members: []catalog.Staged{{Role: primaryRole(SuiteApprovalItem), File: "approval.json", Data: data}}}
	if history := loaded.suiteApprovalHistory(record.Suite); history != nil {
		draft.ItemID, draft.Base = history.ID, history.RevisionLabel()
	}
	_, err = loaded.store.Save(draft, verifierFor(SuiteApprovalItem), catalog.Options{Now: a.now, Fault: a.saveFault})
	var conflict *catalog.Conflict
	switch {
	case errors.As(err, &conflict):
		return nil, refusal{Failed, "another approval of this suite was recorded meanwhile; review this approval again"}
	case err != nil:
		return nil, refusal{Failed, "the approval was not recorded; nothing changed"}
	}
	records := append(loaded.suiteApprovals(record.Suite), record)
	shown := loaded.approvalView(bound.suiteApproval.suite, record, records)
	return &shown, refusal{}
}

// SuiteReviewers are the reviewers a team review request can ask: the hub
// project's active members who may approve, other than the person signed in.
func (a *App) suiteReviewers(ctx context.Context, result *SuiteReviewersResult) {
	client, session, declined := a.signedIn()
	if declined.state != "" {
		result.refuse(declined.state, declined.reason)
		return
	}
	project, declined := a.hubProject()
	if declined.state != "" {
		result.refuse(declined.state, declined.reason)
		return
	}
	reviewers, err := client.ListReviewers(ctx, project)
	if err != nil {
		result.refuse(hubReadState(err), "the customer hub's reviewers cannot be read: "+err.Error())
		return
	}
	for _, member := range reviewers.Members {
		if member.Subject != session.Subject {
			result.Reviewers = append(result.Reviewers, member.Subject)
		}
	}
	slices.Sort(result.Reviewers)
	result.State, result.SignedIn = Completed, session.Subject
}
