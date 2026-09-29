package desktop

import (
	"context"

	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A suite is a named object of the project: the saved test versions it runs,
// the datasets those tests run over, the environments they are bound to, and
// the requirements and exclusions its coverage is assessed by. Each Save
// publishes one immutable suite version whose members are the canonical
// readmit-suite/v1 document the command line reads, compiled from the draft,
// and the readmit-suite-definition/v1 draft it was compiled from, which keeps
// the names and object references the canonical document has no room for.
//
// A readmit-suite/v1 file the project already held is the same named object,
// read as it is: its original bytes are its original version, never renamed
// v1 and never rewritten. The first Save of an edit publishes the first
// managed version beside them.
//
// Approvals are an append-only history of the suite, each bound to one exact
// version: a local baseline, a team review request and release, and the
// approval of a version for one of its environments. None of them sends,
// deploys or authorizes a later send.

// SuiteDefinitionSchema is the contract of a suite version's definition
// member: the SuiteDraft it was compiled from.
const SuiteDefinitionSchema = "readmit-suite-definition/v1"

// SuiteApprovalSchema is the contract of one approval of a suite version.
const SuiteApprovalSchema = "readmit-suite-approval/v1"

// SuiteApprovalItem is the approval history of one suite: each approval is
// one revision. It is listed only as its suite's history.
const SuiteApprovalItem ItemKind = "suite-approval"

// The suite approval actions. Each is reviewed and bound like every other
// reviewed action: its review shows the exact version, tests, changes and
// actor it records, and a changed version, environment or permission refuses
// the final click and refreshes the review.
const (
	// ApproveSuiteBaselineAction records the local expected-test baseline of
	// one suite version: every test version it pins is released locally,
	// under the local reviewer's name.
	ApproveSuiteBaselineAction ActionID = "suite.approve-baseline"
	// RequestSuiteReviewAction asks one team reviewer, through the signed-in
	// customer hub, to review the releases of one baselined suite version.
	RequestSuiteReviewAction ActionID = "suite.request-review"
	// ApproveSuiteReleaseAction records the signed-in reviewer's team
	// approval of those releases, answering the outstanding request.
	ApproveSuiteReleaseAction ActionID = "suite.approve-release"
)

// SuiteDraft is a whole suite as its editor holds it. Every identifier is
// the suite's own and internal: the window shows names, and a draft opened
// from an existing suite keeps the identifiers it declares. A draft may
// reference a test, case, environment or observation this project does not
// hold (an imported or original suite); Source then holds the reference as
// the document declared it, and a save refuses it at that member.
type SuiteDraft struct {
	// ID is the suite document's identifier; empty for a new suite, which a
	// save derives from the name.
	ID          string   `json:"id,omitzero"`
	Owner       string   `json:"owner,omitzero"`
	Tags        []string `json:"tags"`
	Concurrency int      `json:"concurrency"`
	// Tests, in the order the suite declares them. Order is not execution
	// order: that is each test's dependencies and the queue's.
	Tests        []SuiteTestDraft   `json:"tests"`
	Datasets     []SuiteDataset     `json:"datasets"`
	Environments []SuiteEnvironment `json:"environments"`
	Requirements []SuiteRequirement `json:"requirements"`
	Exclusions   []SuiteExclusion   `json:"exclusions"`
}

// SuiteTestDraft is one test of a suite: a saved test at one exact version,
// the dataset its rows come from, the environment parameter it is bound
// through, the suite tests that must pass first, whether it shares state,
// and the exact messages it sends, in send order.
type SuiteTestDraft struct {
	ID        string             `json:"id"`
	Test      ItemRef            `json:"test"`
	Source    string             `json:"source,omitzero"`
	Dataset   string             `json:"dataset"`
	Parameter string             `json:"parameter"`
	After     []string           `json:"after"`
	Isolation runqueue.Isolation `json:"isolation"`
	Sequence  []string           `json:"sequence"`
	Owner     string             `json:"owner,omitzero"`
	Tags      []string           `json:"tags"`
}

// SuiteDataset is one named dataset: the rows a test runs over, each one
// case and the expected values it overrides.
type SuiteDataset struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Rows []SuiteDataRow `json:"rows"`
}

// SuiteDataRow is one row: a case of the project and, by check identity, the
// complete typed expected value it replaces. A check it does not name keeps
// the test's own expectation.
type SuiteDataRow struct {
	ID       string                      `json:"id"`
	Case     ItemRef                     `json:"case"`
	Source   string                      `json:"source,omitzero"`
	Expected map[string]testrunner.Value `json:"expected,omitzero"`
}

// SuiteEnvironment is one environment of the suite: its name, the site it
// declares, and one binding of every parameter its tests use.
type SuiteEnvironment struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Site     string         `json:"site"`
	Bindings []SuiteBinding `json:"bindings"`
}

// SuiteBinding binds one parameter to a named environment of the project
// and, for a test that reads appointment records, a named observation. A run
// follows each named environment to its current version and records the
// version it ran against.
type SuiteBinding struct {
	Parameter         string   `json:"parameter"`
	Target            ItemRef  `json:"target"`
	TargetSource      string   `json:"target_source,omitzero"`
	Observation       *ItemRef `json:"observation,omitzero"`
	ObservationSource string   `json:"observation_source,omitzero"`
}

// SuiteRequirement is one declared requirement: its name, its internal
// identity, and the suite tests that establish it. No tests declares it
// explicitly uncovered.
type SuiteRequirement struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tests []string `json:"tests"`
}

// SuiteExclusion declares one suite test skipped, unsupported, quarantined
// or disabled until an exact UTC time, for a reason. It is an assessment
// declaration: it never stops the test from running, and its expiry never
// permits a send.
type SuiteExclusion struct {
	Test   string `json:"test"`
	State  string `json:"state"`
	Reason string `json:"reason"`
	Until  string `json:"until"`
}

// SuiteContext is what a suite editor reads beside its draft: whether it is
// an original file with no managed version, whether it is read-only, every
// test version it pins and whether it can run.
type SuiteContext struct {
	Original bool               `json:"original"`
	ReadOnly bool               `json:"read_only"`
	Tests    []SuiteTestVersion `json:"tests"`
	// Document is the canonical suite text of this version, for Details.
	Document string `json:"document,omitzero"`
	// Runnable says the version compiles to a suite that can run: it has a
	// test and an environment. An original file always is.
	Runnable bool `json:"runnable"`
}

// SuiteTestVersion is one saved test version as a suite uses it: its name,
// its checks, the messages it sends in source order (the one sequence a
// suite can declare for it) and whether it reads appointment records, so its
// binding needs an observation. Reason says why it cannot be read.
type SuiteTestVersion struct {
	Ref      ItemRef                  `json:"ref"`
	Name     string                   `json:"name"`
	Version  string                   `json:"version,omitzero"`
	Checks   []testauthor.Expectation `json:"checks"`
	Messages []TestMessage            `json:"messages"`
	Sequence []string                 `json:"sequence"`
	Ledger   bool                     `json:"ledger"`
	Reason   string                   `json:"reason,omitzero"`
}

// SuiteTestsRequest asks for saved test versions as a suite uses them.
type SuiteTestsRequest struct {
	Context RequestContext `json:"context"`
	Tests   []ItemRef      `json:"tests"`
}

// SuiteTestsResult answers them, in the order asked.
type SuiteTestsResult struct {
	State   State              `json:"state"`
	Reason  string             `json:"reason,omitzero"`
	Context RequestContext     `json:"context"`
	Tests   []SuiteTestVersion `json:"tests"`
}

func (r *SuiteTestsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SuiteResult is the outcome of one suite test in one run, the worst of its
// rows: passed, failed (a check failed), error, skipped, uncertain (a
// delivery no acknowledgement settled) or unknown (no retained execution).
type SuiteResult string

const (
	SuitePassed    SuiteResult = "passed"
	SuiteFailed    SuiteResult = "failed"
	SuiteError     SuiteResult = "error"
	SuiteSkipped   SuiteResult = "skipped"
	SuiteUncertain SuiteResult = "uncertain"
	SuiteUnknown   SuiteResult = "unknown"
)

// SuiteApprovalScope is what one approval is.
type SuiteApprovalScope string

const (
	BaselineApproval    SuiteApprovalScope = "baseline"
	ReviewRequested     SuiteApprovalScope = "review-request"
	ReleaseApproval     SuiteApprovalScope = "release"
	EnvironmentApproval SuiteApprovalScope = "environment"
)

// SuiteApproval is one recorded approval of a suite version: its scope, who
// made it and when, the environment and target revision an environment
// approval names, the reviewer a request asked, and the reason given.
// Current says it still binds that version's content, environment and
// permissions; Stale says why it no longer does. A stale approval stays in
// history and is never renewed.
type SuiteApproval struct {
	Scope          SuiteApprovalScope `json:"scope"`
	Revision       string             `json:"revision,omitzero"`
	Actor          string             `json:"actor"`
	At             *string            `json:"at"`
	Environment    string             `json:"environment,omitzero"`
	TargetRevision string             `json:"target_revision,omitzero"`
	Reviewer       string             `json:"reviewer,omitzero"`
	Reason         string             `json:"reason,omitzero"`
	Current        bool               `json:"current"`
	Stale          string             `json:"stale,omitzero"`
}

// SuiteVersion is one version of a suite. Original is the file the project
// held before any managed version, which has no number.
type SuiteVersion struct {
	Revision    string          `json:"revision,omitzero"`
	Original    bool            `json:"original"`
	PublishedAt *string         `json:"published_at"`
	Author      string          `json:"author,omitzero"`
	Current     bool            `json:"current"`
	Approvals   []SuiteApproval `json:"approvals"`
}

// SuiteRunRow is one retained run of a suite: the version it ran (empty when
// no version of the suite matches it), the suite environment it ran against,
// when it started and its outcome.
type SuiteRunRow struct {
	Run         ItemRef `json:"run"`
	Revision    string  `json:"revision,omitzero"`
	Environment string  `json:"environment,omitzero"`
	StartedAt   *string `json:"started_at"`
	Outcome     string  `json:"outcome,omitzero"`
}

// SuiteTestResult is the result of one suite test in the latest run of the
// current version.
type SuiteTestResult struct {
	Test   string      `json:"test"`
	Result SuiteResult `json:"result"`
	Run    ItemRef     `json:"run"`
}

// SuiteHistoryResult is a suite's versions, newest first, each with its
// approvals; its runs, newest first; and each test's latest result.
type SuiteHistoryResult struct {
	State    State             `json:"state"`
	Reason   string            `json:"reason,omitzero"`
	Context  RequestContext    `json:"context"`
	Versions []SuiteVersion    `json:"versions"`
	Runs     []SuiteRunRow     `json:"runs"`
	Results  []SuiteTestResult `json:"results"`
}

func (r *SuiteHistoryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SuiteCompareRequest compares two versions of one suite. From is the
// earlier version, empty for none: the later version is then shown alone as
// the first version. "original" names an original file's version.
type SuiteCompareRequest struct {
	Context RequestContext `json:"context"`
	Suite   ItemRef        `json:"suite"`
	From    string         `json:"from,omitzero"`
	To      string         `json:"to"`
}

// SuiteChange is one change between two suite versions: the area it is in
// (test, dataset, row, environment, binding, requirement, exclusion or
// setting), the named subject, and its value before and after, empty where
// it did not exist.
type SuiteChange struct {
	Area    string `json:"area"`
	Subject string `json:"subject"`
	Earlier string `json:"earlier,omitzero"`
	Later   string `json:"later,omitzero"`
}

// SuiteCheckChange is one check whose expectation differs between two
// versions of a test, each side absent where the check does not exist.
type SuiteCheckChange struct {
	Earlier *testauthor.Expectation `json:"earlier,omitzero"`
	Later   *testauthor.Expectation `json:"later,omitzero"`
}

// SuiteTestChange is one suite test whose pinned version moved: the test,
// its two versions, its changed checks and every other change of its
// definition.
type SuiteTestChange struct {
	Test     ItemRef            `json:"test"`
	Name     string             `json:"name"`
	From     string             `json:"from,omitzero"`
	To       string             `json:"to,omitzero"`
	Checks   []SuiteCheckChange `json:"checks"`
	Messages []TestMessage      `json:"messages"`
	Changes  []TestChange       `json:"changes"`
}

// SuiteComparison is what changed from one suite version to another.
type SuiteComparison struct {
	State   State             `json:"state"`
	Reason  string            `json:"reason,omitzero"`
	Context RequestContext    `json:"context"`
	From    string            `json:"from,omitzero"`
	To      string            `json:"to"`
	First   bool              `json:"first"`
	Changes []SuiteChange     `json:"changes"`
	Tests   []SuiteTestChange `json:"tests"`
}

func (r *SuiteComparison) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SuiteCoverageRequest assesses one suite version's declared coverage over
// one of its retained runs, the latest when none is named, with named
// earlier runs of the same version and environment as history. It reads
// retained evidence only: nothing runs.
type SuiteCoverageRequest struct {
	Context  RequestContext `json:"context"`
	Suite    ItemRef        `json:"suite"`
	Run      *ItemRef       `json:"run,omitzero"`
	Previous []ItemRef      `json:"previous"`
}

// SuiteRequirementResult is one declared requirement's assessment: passed,
// uncovered or not_passed, as the coverage assessment decides.
type SuiteRequirementResult struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// SuiteJobCoverage is one expanded job's assessment: the suite test and
// data row it runs, its actual execution, any exclusion and whether it
// counts toward a requirement.
type SuiteJobCoverage struct {
	Test      string `json:"test"`
	Row       string `json:"row"`
	Execution string `json:"execution"`
	Reason    string `json:"reason,omitzero"`
	Exclusion string `json:"exclusion,omitzero"`
	Expired   bool   `json:"expired"`
	Stability string `json:"stability"`
	Eligible  bool   `json:"eligible"`
}

// SuiteAssessment is the assessment of one version over one run. A
// version with no retained run answers Empty with no assessment.
type SuiteAssessment struct {
	State        State                    `json:"state"`
	Reason       string                   `json:"reason,omitzero"`
	Context      RequestContext           `json:"context"`
	Run          *ItemRef                 `json:"run,omitzero"`
	Environment  string                   `json:"environment,omitzero"`
	At           string                   `json:"at,omitzero"`
	Denominator  int                      `json:"denominator"`
	Passed       int                      `json:"passed"`
	Requirements []SuiteRequirementResult `json:"requirements"`
	Jobs         []SuiteJobCoverage       `json:"jobs"`
}

func (r *SuiteAssessment) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SuiteApprovalOptions are what one suite approval review is asked for:
// the suite environment and the operator's target revision of an
// environment approval, the team reviewer a review request asks, and the
// earlier version the changes are shown against (empty: the latest version
// with the same kind of approval).
type SuiteApprovalOptions struct {
	Environment string `json:"environment,omitzero"`
	Revision    string `json:"revision,omitzero"`
	Reviewer    string `json:"reviewer,omitzero"`
	From        string `json:"from,omitzero"`
}

// SuiteApprovalTest is one test version an approval binds, and its release
// where the version has one.
type SuiteApprovalTest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Release string `json:"release,omitzero"`
}

// SuiteApprovalTarget is one binding an environment approval binds.
type SuiteApprovalTarget struct {
	Parameter   string `json:"parameter"`
	Target      string `json:"target"`
	Version     string `json:"version,omitzero"`
	Observation string `json:"observation,omitzero"`
}

// SuiteApprovalReview is the review of one suite approval: its scope, the
// exact suite version and test versions it binds, who it records as the
// actor, the reviewer a request asks, an environment approval's environment,
// site, bindings and target revision, the outstanding request a release
// answers, and the exact changes since the version compared with.
type SuiteApprovalReview struct {
	Scope          SuiteApprovalScope    `json:"scope"`
	Suite          string                `json:"suite"`
	Version        string                `json:"version"`
	Actor          string                `json:"actor"`
	Reviewer       string                `json:"reviewer,omitzero"`
	Request        string                `json:"request,omitzero"`
	Tests          []SuiteApprovalTest   `json:"tests"`
	Environment    string                `json:"environment,omitzero"`
	Site           string                `json:"site,omitzero"`
	Targets        []SuiteApprovalTarget `json:"targets"`
	TargetRevision string                `json:"target_revision,omitzero"`
	Comparison     *SuiteComparison      `json:"comparison,omitzero"`
}

// SuiteReviewersResult is the team reviewers a review request can ask: the
// people the signed-in hub project's history names, other than the person
// signed in.
type SuiteReviewersResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Context   RequestContext `json:"context"`
	SignedIn  string         `json:"signed_in,omitzero"`
	Reviewers []string       `json:"reviewers"`
}

func (r *SuiteReviewersResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SuiteExportRequest exports one suite version: as its suite document, or,
// for Environment, as the run configuration it compiles to against that
// environment. Nothing is sent.
type SuiteExportRequest struct {
	Context     RequestContext `json:"context"`
	Suite       ItemRef        `json:"suite"`
	Environment string         `json:"environment,omitzero"`
}

// SuiteExportResult is where an export was written.
type SuiteExportResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Output  string         `json:"output,omitzero"`
}

func (r *SuiteExportResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SuiteTests reads saved test versions as a suite uses them: the checks of
// each, the messages it sends in its own order and whether it reads
// appointment records. It is a read.
func (a *App) SuiteTests(request SuiteTestsRequest) SuiteTestsResult {
	return runRead(a, false, func(ctx context.Context) SuiteTestsResult {
		result := SuiteTestsResult{Context: request.Context, Tests: []SuiteTestVersion{}}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		result.State, result.Tests = Completed, loaded.suiteTestVersions(request.Tests)
		return result
	})
}

// SuiteHistory lists one suite's versions with their approvals, its runs and
// each test's latest result. It is a read: nothing runs.
func (a *App) SuiteHistory(request ItemRequest) SuiteHistoryResult {
	return runRead(a, false, func(ctx context.Context) SuiteHistoryResult {
		result := SuiteHistoryResult{Context: request.Context, Versions: []SuiteVersion{}, Runs: []SuiteRunRow{}, Results: []SuiteTestResult{}}
		if request.Ref.Kind != SuiteItem {
			result.refuse(Failed, "only a suite has versions, approvals and runs")
			return result
		}
		loaded, _, refused := a.catalogItem(ctx, request.Context, ItemRef{Kind: SuiteItem, ID: request.Ref.ID}, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		loaded.suiteHistory(&result, loaded.document.Items[loaded.document.Find(request.Ref.ID)])
		result.State = Completed
		return result
	})
}

// CompareSuiteVersions answers what changed between two versions of a suite:
// every change of its tests, data, environments, coverage and settings by
// name, and the exact check changes of each test whose pinned version moved.
// With no earlier version the later one stands alone as the first. It is a
// read.
func (a *App) CompareSuiteVersions(request SuiteCompareRequest) SuiteComparison {
	return runRead(a, false, func(ctx context.Context) SuiteComparison {
		result := SuiteComparison{Context: request.Context, To: request.To, Changes: []SuiteChange{}, Tests: []SuiteTestChange{}}
		if request.Suite.Kind != SuiteItem || request.To == "" {
			result.refuse(Failed, "a comparison names the suite and the version it compares")
			return result
		}
		loaded, _, refused := a.catalogItem(ctx, request.Context, ItemRef{Kind: SuiteItem, ID: request.Suite.ID}, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		compared, err := loaded.compareSuite(loaded.document.Items[loaded.document.Find(request.Suite.ID)], request.From, request.To)
		if err != nil {
			result.refuse(Failed, "that version of the suite cannot be read: "+err.Error())
			return result
		}
		compared.Context = request.Context
		return compared
	})
}

// SuiteCoverage assesses one suite version's declared coverage over a
// retained run of it, through the coverage assessment `readmit suite
// coverage` makes. It reads retained evidence only: nothing runs.
func (a *App) SuiteCoverage(request SuiteCoverageRequest) SuiteAssessment {
	return runNamed[SuiteAssessment, *SuiteAssessment](a, profiles["SuiteCoverage"], func(ctx context.Context) SuiteAssessment {
		result := SuiteAssessment{Context: request.Context, Requirements: []SuiteRequirementResult{}, Jobs: []SuiteJobCoverage{}}
		if request.Suite.Kind != SuiteItem {
			result.refuse(Failed, "coverage is assessed for one suite version")
			return result
		}
		loaded, _, refused := a.catalogItem(ctx, request.Context, ItemRef{Kind: SuiteItem, ID: request.Suite.ID}, false)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		loaded.suiteCoverage(ctx, &result, loaded.document.Items[loaded.document.Find(request.Suite.ID)], request, a.now())
		return result
	})
}

// SuiteReviewers lists the team reviewers a review request can ask, from the
// signed-in customer hub's review history.
func (a *App) SuiteReviewers(request RequestContext) SuiteReviewersResult {
	return runNamed[SuiteReviewersResult, *SuiteReviewersResult](a, profiles["SuiteReviewers"], func(ctx context.Context) SuiteReviewersResult {
		result := SuiteReviewersResult{Context: request, Reviewers: []string{}}
		a.suiteReviewers(ctx, &result)
		return result
	})
}

// ImportSuiteItem reads a readmit-suite/v1 file the person chooses into a
// new suite draft; nothing is saved.
func (a *App) ImportSuiteItem(request RequestContext) ItemDraftResult {
	return run(a, true, false, func(ctx context.Context) ItemDraftResult {
		return a.importSuite(ctx, request)
	})
}

// ExportSuiteItem writes one suite version's suite document to a new file
// the person chooses.
func (a *App) ExportSuiteItem(request SuiteExportRequest) SuiteExportResult {
	return run(a, true, true, func(ctx context.Context) SuiteExportResult {
		return a.exportSuite(ctx, request)
	})
}

// ExportSuiteRunConfiguration compiles one suite version against one of its
// environments into a new folder the person chooses. Nothing is sent.
func (a *App) ExportSuiteRunConfiguration(request SuiteExportRequest) SuiteExportResult {
	return run(a, true, true, func(ctx context.Context) SuiteExportResult {
		return a.exportRunConfiguration(ctx, request)
	})
}
