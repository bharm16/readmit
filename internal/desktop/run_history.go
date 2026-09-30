package desktop

import (
	"cmp"
	"path/filepath"
	"reflect"
	"strconv"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Run history (#555). Each retained run is listed as what it executed — the
// test or suite by the name and version it ran, the named environment it
// reached — and with one result, decided here once from what the run's own
// readers established, so a window never ranks lifecycle and verdict itself.

// RunResult is the one result a run is shown with. Its order is its
// priority: a run whose lifecycle is incomplete is never shown as passing,
// whatever its checks decided, and a refusal is not an error.
type RunResult string

const (
	// RunRunning: this window is executing the run now.
	RunRunning RunResult = "running"
	// RunInterrupted: the journal never recorded how the run ended.
	RunInterrupted RunResult = "interrupted"
	// RunIncomplete: the run stopped before a decided result, or a delivery
	// no acknowledgement settled stands beside a passing one.
	RunIncomplete RunResult = "incomplete"
	// RunBlocked: admission refused work the run was asked to do.
	RunBlocked RunResult = "blocked"
	// RunErrored: the run could not evaluate its checks.
	RunErrored RunResult = "error"
	// RunFailed: a completed check failed.
	RunFailed RunResult = "failed"
	// RunPassed: every check of a completed run passed.
	RunPassed RunResult = "passed"
	// RunAccepted and RunNotAccepted are a send of messages without checks:
	// whether every message's acknowledgement accepted it. An accepting
	// acknowledgement is never a passed check.
	RunAccepted    RunResult = "accepted"
	RunNotAccepted RunResult = "not_accepted"
)

// RunKind is what a run executed: a saved test, a suite, or a send of
// selected messages.
type RunKind string

const (
	TestRunKind  RunKind = "test"
	SuiteRunKind RunKind = "suite"
	SendRunKind  RunKind = "send"
)

// A run is read by readRunItem, registered here rather than in the readers
// literal because naming what a run executed reads other objects through
// the same readers.
func init() { readers[RunItem] = readRunItem }

// readRunItem reads one retained run and names what it executed. A run this
// window is writing now is listed as running even while its folder cannot be
// read whole yet.
func readRunItem(c *loadedCatalog, item catalog.Item, paths map[string]string) (view, error) {
	path := paths[primaryRole(RunItem)]
	if declares(filepath.Join(path, "manifest.json"), suite.ConnectedExecutionSchema) {
		return readConnectedSuiteRun(c, item, path)
	}
	active := c.executing != nil && c.executing(path)
	read, err := readRun(c, item, paths)
	if err != nil {
		if !active {
			return read, err
		}
		read = view{summary: ItemSummary{Run: &RunSummary{}}}
	}
	summary := read.summary.Run
	summary.Entry, summary.Active = item.Entry, active
	switch {
	case summary.Suite != nil || regular(filepath.Join(path, "suite.json")):
		summary.Kind = SuiteRunKind
		read.name = c.describeSuiteRun(item, path, summary)
	case summary.Boundary == "transport-only" || summary.Outcome == "accepted" || summary.Outcome == "not-accepted":
		summary.Kind = SendRunKind
		read.name = c.describeSend(path, summary)
	default:
		summary.Kind = TestRunKind
		read.name = c.describeTestRun(path, summary)
	}
	if active {
		summary.Result = RunRunning
	}
	return read, nil
}

// describeTestRun names the test a durable run or result executed, as the
// run retained it, the version of the saved test it was, the environment it
// reached, and its result.
func (c *loadedCatalog) describeTestRun(path string, summary *RunSummary) string {
	opened, err := runresult.Open(path)
	var spec *testrunner.Spec
	if err == nil {
		spec = opened.Spec
		summary.Result = testRunResult(opened)
	}
	if spec == nil {
		// A job that never finalized a result still retained what it was
		// asked to execute, and when.
		if inputs, created, err := durablerun.RetainedInputs(path); err == nil {
			if decoded, err := testrunner.DecodeSpec(inputs.Spec); err == nil {
				spec = &decoded
			}
			if summary.StartedAt == nil && !created.IsZero() {
				summary.StartedAt = stampedTime(created)
			}
			if summary.Target == "" {
				summary.Target = inputs.Configuration.Address
			}
		}
	}
	if spec == nil {
		return ""
	}
	origin := c.originOfRun(path, *spec)
	summary.Test, summary.Environment, summary.EnvironmentName = origin.test, origin.environment, origin.environmentName
	if origin.test != nil {
		summary.Version = origin.test.Revision
	}
	return spec.Name
}

// testRunResult is the one result of a test run, in the order RunResult
// ranks them.
func testRunResult(opened *runresult.Result) RunResult {
	if opened.Durable {
		job := opened.Lifecycle
		switch {
		case job.State == durablerun.Interrupted || job.Recovered || !job.State.Terminal() && job.State != durablerun.DeliveryUncertain:
			return RunInterrupted
		// A run that was stopped, or whose delivery no acknowledgement
		// settled, is incomplete whatever its verdict says.
		case job.JournalIncomplete, job.ResultIdentity == "", job.DeliveryUncertain, job.State == durablerun.DeliveryUncertain,
			job.State == durablerun.Cancelled, job.State == durablerun.TimedOut, job.StopReason == durablerun.Cancelled, job.StopReason == durablerun.TimedOut:
			return RunIncomplete
		}
	}
	if opened.Artifact == nil {
		return RunIncomplete
	}
	switch opened.Artifact.Result.Status {
	case testrunner.ExecutionError:
		return RunErrored
	case testrunner.AssertionFailure:
		return RunFailed
	case testrunner.Pass:
		return RunPassed
	}
	return RunIncomplete
}

// describeSuiteRun names the suite an execution ran, the version of it and
// the suite environment it ran at, and its result: the worst of its jobs'.
func (c *loadedCatalog) describeSuiteRun(item catalog.Item, path string, summary *RunSummary) string {
	name := ""
	execution, err := suiteExecution(item.ID, path)
	if err != nil {
		summary.Result = RunInterrupted
		return name
	}
	name = readableName(execution.suite)
	if record, held, err := c.recordedRunOrigin(path, fileDigest(filepath.Join(path, "suite.json"))); held && record.Source.Kind == SuiteItem {
		summary.Suite, summary.Version, name = &record.Source, record.Source.Revision, record.Name
		summary.EnvironmentName = record.EnvironmentName
	} else if err != nil || held {
		summary.Suite, summary.Version = nil, ""
	}
	if summary.Suite != nil {
		if index := c.document.Find(summary.Suite.ID); index >= 0 {
			held := c.document.Items[index]
			summary.Version = summary.Suite.Revision
			if version, err := c.suiteVersion(held, summary.Suite.Revision); err == nil {
				for _, environment := range version.draft.Environments {
					if environment.ID == execution.environment {
						summary.EnvironmentName = environment.Name
					}
				}
			}
		}
	}
	summary.EnvironmentName = cmp.Or(summary.EnvironmentName, readableName(execution.environment))
	summary.Result = c.suiteRunResult(execution)
	return name
}

// suiteRunResult ranks an execution's jobs: an execution with no queue
// report was interrupted, a stopped queue or an unsettled delivery is
// incomplete, a refused job blocks it, and otherwise the worst job decides.
func (c *loadedCatalog) suiteRunResult(execution suiteRunView) RunResult {
	opened, err := suite.OpenExecution(execution.dir)
	if err != nil || opened.Report == nil {
		return RunInterrupted
	}
	if execution.outcome != "executed" && opened.Report.Refused == 0 {
		return RunIncomplete
	}
	worst := RunPassed
	for _, job := range opened.Report.Jobs {
		result := RunPassed
		switch job.Admission {
		case "refused", "start_failed":
			result = RunBlocked
		case "skipped":
			continue
		default:
			retained, err := runresult.Open(filepath.Join(execution.dir, "runs", job.ID))
			if err != nil {
				result = RunInterrupted
			} else {
				result = testRunResult(retained)
			}
		}
		if rank(result) < rank(worst) {
			worst = result
		}
	}
	if execution.uncertain > 0 && rank(worst) > rank(RunIncomplete) {
		return RunIncomplete
	}
	return worst
}

// rank orders results by priority, the most important first.
func rank(result RunResult) int {
	for i, ranked := range []RunResult{RunRunning, RunInterrupted, RunIncomplete, RunBlocked, RunErrored, RunFailed, RunNotAccepted, RunPassed, RunAccepted} {
		if ranked == result {
			return i
		}
	}
	return 9
}

// describeSend names a send of selected messages by the case they came
// from. Whether every acknowledgement accepted its message is all it
// establishes.
func (c *loadedCatalog) describeSend(path string, summary *RunSummary) string {
	switch summary.Outcome {
	case "accepted":
		summary.Result = RunAccepted
	case "not-accepted":
		summary.Result = RunNotAccepted
	default:
		summary.Result = RunIncomplete
		if summary.Outcome == "completed" {
			summary.Result = RunAccepted
		}
	}
	if summary.Uncertain > 0 || summary.DeliveryUncertain {
		summary.DeliveryUncertain = true
		if rank(summary.Result) > rank(RunIncomplete) {
			summary.Result = RunIncomplete
		}
	}
	if environment, held := c.environmentOfAddress(summary.Target); held {
		summary.Environment, summary.EnvironmentName = &environment.ref, environment.name
	}
	if summary.SourceCase == nil {
		bundle := path
		if summary.Boundary == "transport-only" {
			bundle = filepath.Join(path, "run")
		}
		if run, err := replay.Open(bundle); err == nil {
			summary.SourceCase = c.caseByIdentity(run.Manifest.SourceBundleIdentity)
		}
	}
	if summary.SourceCase == nil {
		return ""
	}
	return c.itemName(summary.SourceCase.ID)
}

// itemName is the name one object of the project is listed under, read once
// per load.
func (c *loadedCatalog) itemName(id string) string {
	if name, held := c.names[id]; held {
		return name
	}
	name := ""
	if index := c.document.Find(id); index >= 0 {
		name = c.read(c.document.Items[index]).Name
	}
	if c.names == nil {
		c.names = map[string]string{}
	}
	c.names[id] = name
	return name
}

// runOrigin is what a retained test is as a saved object: the test and the
// version of it, and the named environment and the revision of it whose
// target it reached.
type runOrigin struct {
	reason          string
	test            *ItemRef
	environment     *ItemRef
	environmentName string
}

// savedTestVersion is one revision of one saved test, read once per load.
type savedTestVersion struct {
	ref  ItemRef
	spec testrunner.Spec
}

// namedTarget is a named environment's revision that declares one target.
type namedTarget struct {
	ref  ItemRef
	name string
}

// originOf finds the saved test version a retained test is, and the named
// environment its target belongs to. A test follows its environment, so
// the target a run reached stands for any environment revision's target: a
// retained test is a version of a saved test when the two differ in nothing
// but that.
func (c *loadedCatalog) originOf(retained testrunner.Spec) runOrigin {
	origin := runOrigin{}
	named, held := c.targetEnvironments()[retained.Target]
	if held {
		origin.environment, origin.environmentName = &named.ref, named.name
	}
	for _, saved := range c.testVersions() {
		candidate := retained
		if held {
			if _, follows := c.targetEnvironments()[saved.spec.Target]; follows {
				candidate.Target = saved.spec.Target
			}
		}
		if reflect.DeepEqual(candidate, saved.spec) {
			ref := saved.ref
			if origin.test != nil {
				// Equal bytes do not identify which publication was selected.
				origin.test = nil
				return origin
			}
			origin.test = &ref
		}
	}
	return origin
}

// testVersions are every saved revision of every test the project holds,
// the newest revision of each test first.
func (c *loadedCatalog) testVersions() []savedTestVersion {
	if c.savedTests != nil {
		return c.savedTests
	}
	c.savedTests = []savedTestVersion{}
	for _, item := range c.document.Items {
		if item.Kind != string(TestItem) || c.removed(item) {
			continue
		}
		labels := []string{""}
		if len(item.Revisions) > 0 {
			labels = labels[:0]
			for i := len(item.Revisions) - 1; i >= 0; i-- {
				labels = append(labels, strconv.Itoa(item.Revisions[i].Number))
			}
		}
		for _, label := range labels {
			saved, err := c.testOf(item, label)
			if err != nil {
				continue
			}
			c.savedTests = append(c.savedTests, savedTestVersion{ref: ItemRef{Kind: TestItem, ID: item.ID, Revision: label}, spec: saved.spec})
		}
	}
	return c.savedTests
}

// targetEnvironments are the named environments by the target entry each
// of their revisions declares.
func (c *loadedCatalog) targetEnvironments() map[string]namedTarget {
	if c.namedTargets != nil {
		return c.namedTargets
	}
	c.namedTargets = map[string]namedTarget{}
	for _, item := range c.document.Items {
		if item.Kind != string(EnvironmentItem) || c.removed(item) {
			continue
		}
		name := cmp.Or(item.Name, c.itemName(item.ID))
		for _, revision := range item.Revisions {
			label := strconv.Itoa(revision.Number)
			if paths, availability, _ := c.revisionBacking(item, label); availability == ItemAvailable {
				if entry := c.entryOf(paths["target"]); entry != "" {
					c.namedTargets[entry] = namedTarget{ref: ItemRef{Kind: EnvironmentItem, ID: item.ID, Revision: label}, name: name}
				}
			}
		}
		if item.Entry != "" && len(item.Revisions) == 0 {
			c.namedTargets[item.Entry] = namedTarget{ref: ItemRef{Kind: EnvironmentItem, ID: item.ID}, name: name}
		}
	}
	return c.namedTargets
}

// environmentOfAddress is the one named environment whose current target
// has an address, when exactly one has it.
func (c *loadedCatalog) environmentOfAddress(address string) (namedTarget, bool) {
	if address == "" {
		return namedTarget{}, false
	}
	var found []namedTarget
	for _, item := range c.document.Items {
		if item.Kind != string(EnvironmentItem) || c.removed(item) {
			continue
		}
		paths, availability, _ := c.backing(item)
		if availability != ItemAvailable {
			continue
		}
		target, err := operation.ReadTarget(paths["target"])
		if err == nil && target.Address == address {
			found = append(found, namedTarget{ref: ItemRef{Kind: EnvironmentItem, ID: item.ID, Revision: item.RevisionLabel()}, name: cmp.Or(item.Name, c.itemName(item.ID))})
		}
	}
	if len(found) != 1 {
		return namedTarget{}, false
	}
	return found[0], true
}
