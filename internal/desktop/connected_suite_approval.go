package desktop

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/suite"
)

// digested is what an approval of a version binds: an authored version's
// definition, or any other version's suite document.
func (v *suiteVersion) digested() []byte {
	if v.authored {
		return v.definition
	}
	return v.data
}

// connectedReleases are the distinct releases one job of a suite's
// baselines recorded, oldest first.
func connectedReleases(records []suiteApprovalRecord, job string) []approvedTest {
	held := []approvedTest{}
	for _, record := range records {
		if record.Schema != ConnectedSuiteApprovalSchema || record.Scope != BaselineApproval {
			continue
		}
		for _, test := range record.Tests {
			if test.Test == job && !slices.ContainsFunc(held, func(h approvedTest) bool { return h.Release == test.Release }) {
				held = append(held, test)
			}
		}
	}
	return held
}

// connectedReleaseNumber is the place of one release in its job's history
// within the suite, from 1, or 0 when the suite never recorded it.
func connectedReleaseNumber(records []suiteApprovalRecord, job, release string) int {
	return slices.IndexFunc(connectedReleases(records, job), func(test approvedTest) bool { return test.Release == release }) + 1
}

// planConnectedBaseline plans the baseline of a version of a suite of
// connected tests: every job compiled in every environment the version
// binds, at the environment versions it pins. A job whose latest release in
// this suite approved exactly its reviewed expectations keeps that release;
// any other is approved as the next release of its job.
func (c *loadedCatalog) planConnectedBaseline(bound *suiteApprovalBinding, display *SuiteApprovalReview, records []suiteApprovalRecord, notReady func(string)) refusal {
	plan, problems := c.planConnectedSuite(bound.version.draft)
	if len(problems) > 0 {
		notReady("this version no longer compiles against the project: " + firstProblem(problems))
		return refusal{}
	}
	bound.connected = plan
	for j, job := range plan.jobs {
		test := bound.version.draft.Tests[job.test]
		shown := SuiteApprovalTest{Name: job.name, Version: test.Test.Revision}
		var parent, kept []byte
		history := connectedReleases(records, job.id)
		if len(history) > 0 {
			latest := history[len(history)-1]
			if release, err := expectation.DecodeConnected(latest.Bytes); err == nil {
				switch {
				case release.Review == plan.reviews[j]:
					kept, shown.Release = latest.Bytes, strconv.Itoa(len(history))
				case release.Review.Revision != plan.reviews[j].Revision:
					// A release continues the history of its job only for a
					// later version of its test.
					parent = latest.Bytes
				}
			}
		}
		if kept == nil {
			shown.Release = strconv.Itoa(len(history) + 1)
		}
		bound.parents, bound.kept = append(bound.parents, parent), append(bound.kept, kept)
		display.Tests = append(display.Tests, shown)
	}
	return refusal{}
}

// planConnectedPromotion reviews the promotion of a baselined version of a
// suite of connected tests to one of its environments, through the review
// suite.ApproveConnectedPromotion approves: the environments it binds at the
// versions the suite pins, and the operator's target revision.
func (c *loadedCatalog) planConnectedPromotion(bound *suiteApprovalBinding, display *SuiteApprovalReview, environment SuiteEnvironment, notReady func(string)) {
	for _, binding := range environment.Bindings {
		for _, ref := range []*ItemRef{&binding.Target, binding.Server} {
			if ref == nil {
				continue
			}
			display.Targets = append(display.Targets, SuiteApprovalTarget{Parameter: binding.Parameter, Target: c.nameOf(*ref, ""), Version: ref.Revision})
			if !slices.Contains(bound.environments, *ref) {
				bound.environments = append(bound.environments, *ref)
			}
		}
		if reason := c.pinnedBinding(binding); reason != "" {
			notReady(reason)
		}
	}
	if bound.targetRevision == "" {
		notReady("Enter the target revision this approval assumes")
		return
	}
	if bound.pins == nil || bound.version.connected == nil {
		return
	}
	bound.compiled = bound.version.data
	placed, remove, err := placeCompiled(c.root, bound.compiled)
	if err != nil {
		notReady(err.Error())
		return
	}
	defer remove()
	review, err := suite.ReviewConnectedPromotion(placed, environment.ID, bound.targetRevision)
	if err != nil {
		notReady(err.Error())
		return
	}
	bound.reviewed = review.Identity()
}

// executeConnectedApproval records a reviewed approval of a version of a
// suite of connected tests. A baseline approves each job's reviewed
// expectations, places the files its document names under the project and
// records the document with its releases; when the record is not published,
// every file it placed is removed again. An environment approval approves
// the baselined document's promotion to one environment.
func (a *App) executeConnectedApproval(ctx context.Context, bound *boundAction, record suiteApprovalRecord, refuse func(State, string) ReviewedActionResult) ReviewedActionResult {
	approval := bound.suiteApproval
	loaded, declined := a.loadCatalog(ctx, bound.origin.Context, false)
	if loaded == nil {
		return refuse(declined.state, declined.reason)
	}
	remove := func() {}
	switch approval.scope {
	case BaselineApproval:
		plan := approval.connected
		releases := [][]byte{}
		for j, job := range plan.jobs {
			data := approval.kept[j]
			if data == nil {
				var err error
				data, err = approveCompiled(plan.compiled[0][j], approval.parents[j], plan.reviews[j].Identity(), approval.actor, record.Reason)
				if err != nil {
					return refuse(Failed, "the baseline was not recorded: "+err.Error())
				}
			}
			release, _ := expectation.DecodeConnected(data)
			releases = append(releases, data)
			record.Tests = append(record.Tests, approvedTest{Test: job.id, Ref: approval.version.draft.Tests[job.test].Test, Release: release.Identity(), Bytes: data})
		}
		document, files, err := loaded.connectedDocument(approval.version.draft, plan, releases)
		if err == nil {
			record.Connected, err = json.Marshal(document, json.Deterministic(true))
		}
		if err == nil {
			remove, err = files.write(ctx, loaded.root)
		}
		if err != nil {
			return refuse(Failed, "the baseline was not recorded: "+err.Error())
		}
	case EnvironmentApproval:
		record.Tests, record.Connected = approval.pins.Tests, approval.pins.Connected
		placed, removePlaced, err := placeCompiled(loaded.root, approval.compiled)
		if err != nil {
			return refuse(Failed, "the environment approval was not recorded: "+err.Error())
		}
		defer removePlaced()
		folder, err := os.MkdirTemp("", "readmit-connected-promotion-")
		if err != nil {
			return refuse(Failed, "the environment approval was not recorded: "+err.Error())
		}
		defer os.RemoveAll(folder)
		output := filepath.Join(folder, "promotion.json")
		if _, err := suite.ApproveConnectedPromotion(placed, approval.environment.ID, approval.targetRevision, approval.reviewed, approval.actor, record.Reason, output); err != nil {
			return refuse(Failed, "the environment approval was not recorded: "+err.Error())
		}
		promotion, err := os.ReadFile(output)
		if err != nil {
			return refuse(Failed, "the environment approval was not recorded: "+err.Error())
		}
		record.Environment = &approvedEnvironment{ID: approval.environment.ID, Name: approval.environment.Name, Site: approval.environment.Site,
			Environments: approval.environments, TargetRevision: approval.targetRevision, Promotion: promotion}
	}
	recorded, declined := a.recordSuiteApproval(ctx, bound, record)
	if declined.state != "" {
		remove()
		return refuse(declined.state, declined.reason)
	}
	return ReviewedActionResult{State: Completed, Outcome: ActionCompleted, SuiteApproval: recorded}
}
