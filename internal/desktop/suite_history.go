package desktop

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A suite's history is read, never written: its versions and the approvals
// each carries, the retained runs of each version, what changed from one
// version to another, and the declared coverage of one version over one of
// its runs. Nothing here runs, sends or records anything.

// suiteTestVersions reads each test version a suite pins as the suite uses
// it, in the order given.
func (c *loadedCatalog) suiteTestVersions(refs []ItemRef) []SuiteTestVersion {
	versions := []SuiteTestVersion{}
	for _, ref := range refs {
		versions = append(versions, c.suiteTestVersion(ref))
	}
	return versions
}

// suiteTestVersion reads one saved test version as a suite uses it: its
// checks, the messages it sends in its own order, and whether it reads
// appointment records. Why it cannot be used is its Reason.
func (c *loadedCatalog) suiteTestVersion(ref ItemRef) SuiteTestVersion {
	shown := SuiteTestVersion{Ref: ref, Checks: []testauthor.Expectation{}, Messages: []TestMessage{}, Sequence: []string{}}
	index := c.document.Find(ref.ID)
	if ref.ID == "" || ref.Kind != TestItem || index < 0 || c.document.Items[index].Kind != string(TestItem) || c.removed(c.document.Items[index]) {
		shown.Reason = "the project holds no such test"
		return shown
	}
	item := c.document.Items[index]
	shown.Name, shown.Version = item.Name, cmp.Or(ref.Revision, item.RevisionLabel())
	saved, err := c.testOf(item, ref.Revision)
	if errors.Is(err, errConnectedTest) {
		connected, err := c.connectedTestOf(item, ref.Revision)
		if err != nil {
			shown.Reason = "this version of the test cannot be read: " + err.Error()
			return shown
		}
		shown.Connected = &SuiteConnectedVersion{Phases: []string{}, Checks: []string{}, Expected: []SuiteConnectedExpected{}}
		if connected.links != nil {
			shown.Connected.Environment = connected.links.Environment
		}
		shown.Connected.Server = connected.draft.Server
		pinned := c.pinnedOffers(connected.draft)
		for _, phase := range connected.draft.Phases {
			shown.Connected.Phases = append(shown.Connected.Phases, phase.Name)
			for _, check := range phase.Checks {
				shown.Connected.Checks = append(shown.Connected.Checks, phase.Name+" · "+check.Name)
				if check.Check.Expected == nil {
					continue
				}
				expected := SuiteConnectedExpected{Key: connectedRowKey(phase.ID, check.Check.ID), Name: phase.Name + " · " + check.Name, Field: ConnectedColumn{Name: check.Check.Column, Type: check.Check.Expected.Type, States: []string{}}, Value: *check.Check.Expected}
				for _, observed := range phase.Observations {
					offer := pinned[observed.Observation.ID+"@"+observed.Observation.Revision]
					if at := slices.IndexFunc(offer.Columns, func(column ConnectedColumn) bool { return column.Name == check.Check.Column }); observed.Dataset == check.Check.Subject.Dataset && at >= 0 {
						expected.Field = offer.Columns[at]
					}
				}
				shown.Connected.Expected = append(shown.Connected.Expected, expected)
			}
		}
		return shown
	}
	if err != nil {
		shown.Reason = "this version of the test cannot be read: " + err.Error()
		return shown
	}
	shown.Name = cmp.Or(item.Name, saved.spec.Name)
	shown.Checks, shown.Sequence = checksOf(saved.spec), slices.Clone(saved.spec.Input.Messages)
	shown.Ledger = saved.spec.Observation.Boundary == testrunner.LedgerBoundary
	shown.Messages = c.sentMessages(saved.spec)
	if saved.release {
		shown.Reason = "this version is a test release, which a suite cannot run as its template"
	}
	return shown
}

// checksOf are a spec's checks as a person edits them, each with its typed
// expected value, in the order the spec declares them.
func checksOf(spec testrunner.Spec) []testauthor.Expectation {
	checks := []testauthor.Expectation{}
	for _, assertion := range spec.Assertions {
		expected := assertion.Expected
		checks = append(checks, testauthor.Expectation{ID: assertion.ID, Operator: assertion.Operator, Message: assertion.Message,
			Selector: assertion.Selector, Count: expected.Count, Records: expected.Records, Field: expected.Field})
	}
	return checks
}

// sentMessages are the occurrences of a spec's case it sends, in the order it
// sends them, when the case is one the project holds and verifies.
func (c *loadedCatalog) sentMessages(spec testrunner.Spec) []TestMessage {
	messages := []TestMessage{}
	if artifactpath.EntryName(spec.Input.Case) != nil {
		return messages
	}
	_, _, source := c.caseOf(spec.Input.Case)
	if source == nil {
		return messages
	}
	byID := map[string]TestMessage{}
	for _, message := range caseMessages(source) {
		byID[message.ID] = message
	}
	for _, id := range spec.Input.Messages {
		if message, held := byID[id]; held {
			messages = append(messages, message)
		}
	}
	return messages
}

// suiteHistory is one suite's versions, newest first and each with its
// approvals, the retained runs of it, newest first, and each test's result
// in the latest run of the current version.
func (c *loadedCatalog) suiteHistory(result *SuiteHistoryResult, item catalog.Item) {
	records := c.suiteApprovals(item.ID)
	current := currentLabel(item)
	names := map[string]bool{}
	versions := map[string]*suiteVersion{}
	for _, label := range slices.Backward(suiteLabels(item)) {
		shown := SuiteVersion{Current: label == current, Approvals: []SuiteApproval{}}
		if label == originalVersion {
			shown.Original = true
		} else {
			revision := item.Revisions[slices.IndexFunc(item.Revisions, func(revision catalog.Revision) bool { return strconv.Itoa(revision.Number) == label })]
			shown.Revision, shown.PublishedAt, shown.Author = label, stamped(revision.PublishedAt), revision.Author
		}
		for _, record := range records {
			if record.Revision == label {
				shown.Approvals = append(shown.Approvals, c.approvalView(item, record, records))
			}
		}
		result.Versions = append(result.Versions, shown)
		if version, err := c.suiteVersion(item, label); err == nil {
			versions[label] = version
			names[version.draft.ID] = true
		}
	}
	var held *suiteView
	for _, view := range c.suites() {
		if view.item.ID == item.ID {
			held = &view
		}
	}
	for _, run := range c.suiteExecutions() {
		label := ""
		if held != nil {
			label = held.versionOf(run)
		}
		if label == "" && !names[run.suite] {
			continue
		}
		row := SuiteRunRow{Run: ItemRef{Kind: RunItem, ID: run.id}, Revision: label, Environment: run.environment, StartedAt: stampedTime(run.started), Outcome: run.outcome}
		if version := versions[label]; version != nil {
			if at := slices.IndexFunc(version.draft.Environments, func(environment SuiteEnvironment) bool { return environment.ID == run.environment }); at >= 0 {
				row.Environment = version.draft.Environments[at].Name
			}
		}
		result.Runs = append(result.Runs, row)
	}
	slices.SortFunc(result.Runs, func(x, y SuiteRunRow) int {
		return cmp.Or(cmp.Compare(stampOf(y.StartedAt), stampOf(x.StartedAt)), cmp.Compare(x.Run.ID, y.Run.ID))
	})
	version := versions[current]
	runs := c.suiteRunsOf(item, current)
	if version == nil || len(runs) == 0 {
		return
	}
	latest := runs[0]
	outcomes := jobResults(latest)
	for _, test := range version.draft.Tests {
		worst := SuitePassed
		jobs := 0
		for _, declared := range latest.document.Tests {
			if declared.ID != test.ID {
				continue
			}
			for _, table := range latest.document.Tables {
				if table.ID != declared.Table {
					continue
				}
				for _, row := range table.Rows {
					jobs++
					if outcome := outcomes[declared.ID+"-"+row.ID]; resultRank(outcome) > resultRank(worst) {
						worst = outcome
					}
				}
			}
		}
		if jobs == 0 {
			worst = SuiteUnknown
		}
		result.Results = append(result.Results, SuiteTestResult{Test: test.ID, Result: worst, Run: ItemRef{Kind: RunItem, ID: latest.id}})
	}
}

// resultRank orders the results of one test's rows from best to worst: a
// pass, a declared skip, no retained execution, an unsettled delivery, a
// failed check and an execution error.
func resultRank(result SuiteResult) int {
	return slices.Index([]SuiteResult{SuitePassed, SuiteSkipped, SuiteUnknown, SuiteUncertain, SuiteFailed, SuiteError}, result)
}

// jobResults is the result of each job of one retained suite execution: its
// run's result where it retains one, and otherwise what the queue decided.
func jobResults(run suiteRunView) map[string]SuiteResult {
	results := map[string]SuiteResult{}
	execution, err := suite.OpenExecution(run.dir)
	if err != nil {
		return results
	}
	admissions := map[string]runqueue.Admission{}
	if execution.Report != nil {
		for _, job := range execution.Report.Jobs {
			admissions[job.ID] = job.Admission
		}
	}
	for _, job := range execution.Queue.Jobs {
		path := filepath.Join(run.dir, "runs", job.ID)
		if _, err := os.Lstat(path); err != nil {
			switch admissions[job.ID] {
			case runqueue.Skipped:
				results[job.ID] = SuiteSkipped
			case runqueue.Refused, runqueue.StartFailed:
				results[job.ID] = SuiteError
			default:
				results[job.ID] = SuiteUnknown
			}
			continue
		}
		opened, err := runresult.Open(path)
		if err != nil {
			results[job.ID] = SuiteUnknown
			continue
		}
		result := SuiteUnknown
		if opened.Artifact != nil {
			switch opened.Artifact.Result.Status {
			case testrunner.Pass:
				result = SuitePassed
			case testrunner.AssertionFailure:
				result = SuiteFailed
			case testrunner.ExecutionError:
				result = SuiteError
			}
		}
		if opened.Lifecycle.DeliveryUncertain && resultRank(result) < resultRank(SuiteUncertain) {
			result = SuiteUncertain
		}
		results[job.ID] = result
	}
	return results
}

// compareSuite answers what changed from one version of a suite to another,
// or, with no earlier version, the later one alone as the first.
func (c *loadedCatalog) compareSuite(item catalog.Item, from, to string) (SuiteComparison, error) {
	comparison := SuiteComparison{State: Completed, From: from, To: to, Changes: []SuiteChange{}, Tests: []SuiteTestChange{}}
	later, err := c.suiteVersion(item, to)
	if err != nil {
		return comparison, err
	}
	comparison.To = later.label
	if later.connected != nil && !later.authored {
		return comparison, errors.New("connected suite changes require inspection of their pinned connected revisions; the legacy template comparison does not assess them")
	}
	if from == "" {
		comparison.First = true
		for _, test := range later.draft.Tests {
			changed := SuiteTestChange{Test: test.Test, Name: c.nameOf(test.Test, test.ID), To: test.Test.Revision, Checks: []SuiteCheckChange{},
				Messages: []TestMessage{}, Changes: []TestChange{}}
			if saved := c.pinnedVersion(test.Test); saved != nil {
				for _, check := range checksOf(saved.spec) {
					changed.Checks = append(changed.Checks, SuiteCheckChange{Later: &check})
				}
				changed.Messages = c.sentMessages(saved.spec)
			}
			comparison.Tests = append(comparison.Tests, changed)
		}
		return comparison, nil
	}
	earlier, err := c.suiteVersion(item, from)
	if err != nil {
		return comparison, err
	}
	comparison.From = earlier.label
	if earlier.connected != nil && !earlier.authored || earlier.authored != later.authored {
		return comparison, errors.New("a connected suite revision cannot be compared as a legacy template suite")
	}
	comparison.Changes = c.suiteChanges(earlier.draft, later.draft)
	for _, test := range later.draft.Tests {
		at := slices.IndexFunc(earlier.draft.Tests, func(prior SuiteTestDraft) bool { return prior.ID == test.ID })
		if at < 0 || earlier.draft.Tests[at].Test == test.Test {
			continue
		}
		prior := earlier.draft.Tests[at].Test
		changed := SuiteTestChange{Test: test.Test, Name: c.nameOf(test.Test, test.ID), From: prior.Revision, To: test.Test.Revision,
			Checks: []SuiteCheckChange{}, Messages: []TestMessage{}, Changes: []TestChange{}}
		if later.authored {
			before, after := c.pinnedConnected(prior), c.pinnedConnected(test.Test)
			if before != nil && after != nil {
				changed.Changes = connectedChanges(before, after)
			}
			comparison.Tests = append(comparison.Tests, changed)
			continue
		}
		before, after := c.pinnedVersion(prior), c.pinnedVersion(test.Test)
		if before != nil && after != nil {
			changed.Checks = checkChanges(checksOf(before.spec), checksOf(after.spec))
			changed.Changes = testChanges(before, after)
		}
		if after != nil {
			changed.Messages = c.sentMessages(after.spec)
		}
		comparison.Tests = append(comparison.Tests, changed)
	}
	return comparison, nil
}

// pinnedConnected reads the connected test version a reference pins, or nil.
func (c *loadedCatalog) pinnedConnected(ref ItemRef) *savedConnected {
	index := c.document.Find(ref.ID)
	if ref.ID == "" || index < 0 || c.document.Items[index].Kind != string(TestItem) {
		return nil
	}
	saved, err := c.connectedTestOf(c.document.Items[index], ref.Revision)
	if err != nil {
		return nil
	}
	return saved
}

// pinnedVersion reads the test version a reference pins, or nil.
func (c *loadedCatalog) pinnedVersion(ref ItemRef) *savedTest {
	index := c.document.Find(ref.ID)
	if ref.ID == "" || index < 0 || c.document.Items[index].Kind != string(TestItem) {
		return nil
	}
	saved, err := c.testOf(c.document.Items[index], ref.Revision)
	if err != nil {
		return nil
	}
	return saved
}

// checkChanges are the checks whose expectation differs between two
// versions of a test: every check of the later version that is new or
// changed, in its order, then every check it no longer declares.
func checkChanges(before, after []testauthor.Expectation) []SuiteCheckChange {
	changes := []SuiteCheckChange{}
	for _, check := range after {
		at := slices.IndexFunc(before, func(prior testauthor.Expectation) bool { return prior.ID == check.ID })
		switch {
		case at < 0:
			changes = append(changes, SuiteCheckChange{Later: &check})
		case !reflect.DeepEqual(before[at], check):
			changes = append(changes, SuiteCheckChange{Earlier: &before[at], Later: &check})
		}
	}
	for _, prior := range before {
		if !slices.ContainsFunc(after, func(check testauthor.Expectation) bool { return check.ID == prior.ID }) {
			changes = append(changes, SuiteCheckChange{Earlier: &prior})
		}
	}
	return changes
}

// nameOf is the name a reference's object is shown by, or fallback.
func (c *loadedCatalog) nameOf(ref ItemRef, fallback string) string {
	index := c.document.Find(ref.ID)
	if ref.ID == "" || index < 0 {
		return fallback
	}
	item := c.document.Items[index]
	if item.Name != "" {
		return item.Name
	}
	return cmp.Or(c.read(item).Name, fallback)
}

// suiteChanges names every change from one suite draft to another, each by
// the names a person reads, never by an identifier or a file.
func (c *loadedCatalog) suiteChanges(before, after SuiteDraft) []SuiteChange {
	changes := []SuiteChange{}
	add := func(area, subject, earlier, later string) {
		if earlier != later {
			changes = append(changes, SuiteChange{Area: area, Subject: subject, Earlier: earlier, Later: later})
		}
	}
	add("setting", "Owner", before.Owner, after.Owner)
	add("setting", "Tags", strings.Join(before.Tags, ", "), strings.Join(after.Tags, ", "))
	add("setting", "Concurrent jobs", strconv.Itoa(before.Concurrency), strconv.Itoa(after.Concurrency))

	testNames := func(draft SuiteDraft, ids []string) string {
		named := []string{}
		for _, id := range ids {
			if at := slices.IndexFunc(draft.Tests, func(test SuiteTestDraft) bool { return test.ID == id }); at >= 0 {
				named = append(named, c.nameOf(draft.Tests[at].Test, id))
			} else {
				named = append(named, id)
			}
		}
		return strings.Join(named, ", ")
	}
	datasetName := func(draft SuiteDraft, id string) string {
		if at := slices.IndexFunc(draft.Datasets, func(dataset SuiteDataset) bool { return dataset.ID == id }); at >= 0 {
			return draft.Datasets[at].Name
		}
		return id
	}
	for _, test := range after.Tests {
		name := c.nameOf(test.Test, test.ID)
		at := slices.IndexFunc(before.Tests, func(prior SuiteTestDraft) bool { return prior.ID == test.ID })
		if at < 0 {
			add("test", name, "", versionText(test.Test))
			continue
		}
		prior := before.Tests[at]
		if prior.Test.ID != test.Test.ID {
			add("test", name, c.nameOf(prior.Test, prior.ID)+" · "+versionText(prior.Test), name+" · "+versionText(test.Test))
		} else {
			add("test", name, versionText(prior.Test), versionText(test.Test))
		}
		add("setting", name+" · Dataset", datasetName(before, prior.Dataset), datasetName(after, test.Dataset))
		add("setting", name+" · Parameter", prior.Parameter, test.Parameter)
		add("setting", name+" · Depends on", testNames(before, prior.After), testNames(after, test.After))
		add("setting", name+" · State sharing", isolationText(prior.Isolation), isolationText(test.Isolation))
		add("setting", name+" · Messages", strings.Join(prior.Sequence, ", "), strings.Join(test.Sequence, ", "))
		add("setting", name+" · Owner", prior.Owner, test.Owner)
		add("setting", name+" · Tags", strings.Join(prior.Tags, ", "), strings.Join(test.Tags, ", "))
	}
	for _, prior := range before.Tests {
		if !slices.ContainsFunc(after.Tests, func(test SuiteTestDraft) bool { return test.ID == prior.ID }) {
			add("test", c.nameOf(prior.Test, prior.ID), versionText(prior.Test), "")
		}
	}

	caseName := func(row SuiteDataRow) string { return c.nameOf(row.Case, cmp.Or(row.Source, row.ID)) }
	for _, dataset := range after.Datasets {
		at := slices.IndexFunc(before.Datasets, func(prior SuiteDataset) bool { return prior.ID == dataset.ID })
		if at < 0 {
			add("dataset", dataset.Name, "", rowsText(len(dataset.Rows)))
			continue
		}
		prior := before.Datasets[at]
		add("dataset", dataset.Name, prior.Name, dataset.Name)
		for m, row := range dataset.Rows {
			subject := dataset.Name + " · Row " + strconv.Itoa(m+1)
			earlier := slices.IndexFunc(prior.Rows, func(held SuiteDataRow) bool { return held.ID == row.ID })
			if earlier < 0 {
				add("row", subject, "", caseName(row))
				continue
			}
			held := prior.Rows[earlier]
			add("row", subject+" · Case", caseName(held), caseName(row))
			checks := slices.Sorted(mapKeys(row.Expected))
			for check := range mapKeys(held.Expected) {
				if _, kept := row.Expected[check]; !kept {
					checks = append(checks, check)
				}
			}
			slices.Sort(checks)
			for _, check := range checks {
				add("row", subject+" · "+check, expectedText(held.Expected, check), expectedText(row.Expected, check))
			}
		}
		for m, held := range prior.Rows {
			if !slices.ContainsFunc(dataset.Rows, func(row SuiteDataRow) bool { return row.ID == held.ID }) {
				add("row", prior.Name+" · Row "+strconv.Itoa(m+1), caseName(held), "")
			}
		}
	}
	for _, prior := range before.Datasets {
		if !slices.ContainsFunc(after.Datasets, func(dataset SuiteDataset) bool { return dataset.ID == prior.ID }) {
			add("dataset", prior.Name, rowsText(len(prior.Rows)), "")
		}
	}

	bindingText := func(binding SuiteBinding) string {
		text := c.nameOf(binding.Target, binding.TargetSource)
		if binding.Observation != nil {
			text += " · " + c.nameOf(*binding.Observation, binding.ObservationSource)
		} else if binding.ObservationSource != "" {
			text += " · " + binding.ObservationSource
		}
		return text
	}
	for _, environment := range after.Environments {
		at := slices.IndexFunc(before.Environments, func(prior SuiteEnvironment) bool { return prior.ID == environment.ID })
		if at < 0 {
			add("environment", environment.Name, "", environment.Site)
			continue
		}
		prior := before.Environments[at]
		add("environment", environment.Name, prior.Name, environment.Name)
		add("environment", environment.Name+" · Site", prior.Site, environment.Site)
		for _, binding := range environment.Bindings {
			earlier := ""
			if held := slices.IndexFunc(prior.Bindings, func(bound SuiteBinding) bool { return bound.Parameter == binding.Parameter }); held >= 0 {
				earlier = bindingText(prior.Bindings[held])
			}
			add("binding", environment.Name+" · "+binding.Parameter, earlier, bindingText(binding))
		}
		for _, held := range prior.Bindings {
			if !slices.ContainsFunc(environment.Bindings, func(binding SuiteBinding) bool { return binding.Parameter == held.Parameter }) {
				add("binding", environment.Name+" · "+held.Parameter, bindingText(held), "")
			}
		}
	}
	for _, prior := range before.Environments {
		if !slices.ContainsFunc(after.Environments, func(environment SuiteEnvironment) bool { return environment.ID == prior.ID }) {
			add("environment", prior.Name, prior.Site, "")
		}
	}

	covered := func(draft SuiteDraft, requirement SuiteRequirement) string {
		if len(requirement.Tests) == 0 {
			return "Uncovered"
		}
		return testNames(draft, requirement.Tests)
	}
	for _, requirement := range after.Requirements {
		at := slices.IndexFunc(before.Requirements, func(prior SuiteRequirement) bool { return prior.ID == requirement.ID })
		if at < 0 {
			add("requirement", requirement.Name, "", covered(after, requirement))
			continue
		}
		prior := before.Requirements[at]
		add("requirement", requirement.Name, prior.Name, requirement.Name)
		add("requirement", requirement.Name+" · Tests", covered(before, prior), covered(after, requirement))
	}
	for _, prior := range before.Requirements {
		if !slices.ContainsFunc(after.Requirements, func(requirement SuiteRequirement) bool { return requirement.ID == prior.ID }) {
			add("requirement", prior.Name, covered(before, prior), "")
		}
	}

	exclusionText := func(exclusion SuiteExclusion) string {
		return readableName(exclusion.State) + " · " + exclusion.Reason + " · until " + exclusion.Until
	}
	for _, exclusion := range after.Exclusions {
		earlier := ""
		if at := slices.IndexFunc(before.Exclusions, func(prior SuiteExclusion) bool { return prior.Test == exclusion.Test }); at >= 0 {
			earlier = exclusionText(before.Exclusions[at])
		}
		add("exclusion", testNames(after, []string{exclusion.Test}), earlier, exclusionText(exclusion))
	}
	for _, prior := range before.Exclusions {
		if !slices.ContainsFunc(after.Exclusions, func(exclusion SuiteExclusion) bool { return exclusion.Test == prior.Test }) {
			add("exclusion", testNames(before, []string{prior.Test}), exclusionText(prior), "")
		}
	}
	return changes
}

// versionText is a pinned test version as a person reads it.
func versionText(ref ItemRef) string {
	if ref.Revision == "" {
		return "no version"
	}
	return "version " + ref.Revision
}

// isolationText is a test's state sharing as a person reads it.
func isolationText(isolation runqueue.Isolation) string {
	switch isolation {
	case runqueue.SharedState:
		return "Shared"
	case runqueue.IsolatedState:
		return "Isolated"
	}
	return string(isolation)
}

// rowsText counts a dataset's rows.
func rowsText(rows int) string {
	if rows == 1 {
		return "1 row"
	}
	return strconv.Itoa(rows) + " rows"
}

// expectedText is one expected value a row overrides, as a person reads it,
// or empty where the row does not override the check.
func expectedText(expected map[string]testrunner.Value, check string) string {
	value, held := expected[check]
	switch {
	case !held:
		return ""
	case value.Count != nil:
		return strconv.Itoa(*value.Count)
	case value.Records != nil && len(*value.Records) == 1:
		return "1 record"
	case value.Records != nil:
		return strconv.Itoa(len(*value.Records)) + " records"
	case value.Field != nil:
		switch value.Field.State {
		case hl7.Present:
			text := ""
			if value.Field.Text != nil {
				text = *value.Field.Text
			}
			return "Present · " + text
		case hl7.Empty:
			return "Empty"
		case hl7.Null:
			return "Null"
		case hl7.Omitted:
			return "Not present"
		}
	}
	data, _ := json.Marshal(value, json.Deterministic(true))
	return string(data)
}

// suiteCoverageOperation names a coverage assessment while it holds the
// slot, so the cancel control of the Coverage section stops exactly the
// assessment it started.
const suiteCoverageOperation = "suite-coverage-assessment"

// suiteCoverage assesses one version's declared coverage over one of its
// retained runs, with earlier runs of it as history, through the one
// coverage assessment `readmit suite coverage` makes. The policy it assesses
// against is built from the version's requirements and exclusions over the
// run's own retained bytes, in a private temporary file removed afterwards.
func (c *loadedCatalog) suiteCoverage(ctx context.Context, result *SuiteAssessment, item catalog.Item, request SuiteCoverageRequest, now time.Time) {
	version, err := c.suiteVersion(item, request.Suite.Revision)
	if err != nil {
		result.refuse(Failed, "this suite cannot be read: "+err.Error())
		return
	}
	if !version.runnable() {
		result.refuse(Empty, notRunnable)
		return
	}
	if version.authored {
		c.connectedCoverage(ctx, result, item, version, request, now)
		return
	}
	if version.connected != nil {
		result.refuse(Failed, "connected coverage requires a readmit-suite-coverage/v2 policy over the sealed connected execution")
		return
	}
	if len(version.draft.Requirements) == 0 {
		result.refuse(Empty, "This version declares no requirements")
		return
	}
	runs := c.suiteRunsOf(item, version.label)
	var chosen *suiteRunView
	switch {
	case request.Run != nil:
		at := slices.IndexFunc(runs, func(run suiteRunView) bool { return run.id == request.Run.ID })
		if at < 0 {
			result.refuse(Failed, "that run did not execute this version of the suite")
			return
		}
		chosen = &runs[at]
	case len(runs) > 0:
		chosen = &runs[0]
	default:
		result.refuse(Empty, "No run of this version is retained; coverage is assessed over a run")
		return
	}
	if len(request.Previous) > 15 {
		result.refuse(Failed, "coverage reads at most fifteen earlier runs")
		return
	}
	previous := []string{}
	for _, ref := range request.Previous {
		at := slices.IndexFunc(runs, func(run suiteRunView) bool { return run.id == ref.ID })
		if at < 0 || runs[at].identity != chosen.identity || runs[at].environment != chosen.environment || runs[at].id == chosen.id {
			result.refuse(Failed, "an earlier run coverage reads is another run of this version against the same environment, as it stood")
			return
		}
		previous = append(previous, runs[at].dir)
	}
	jobs := map[string][]string{}
	rows := map[string][2]string{}
	for _, test := range chosen.document.Tests {
		for _, table := range chosen.document.Tables {
			if table.ID != test.Table {
				continue
			}
			for _, row := range table.Rows {
				job := test.ID + "-" + row.ID
				jobs[test.ID] = append(jobs[test.ID], job)
				rows[job] = [2]string{test.ID, row.ID}
			}
		}
	}
	requirements := []suite.Requirement{}
	for _, requirement := range version.draft.Requirements {
		declared := suite.Requirement{ID: requirement.ID, Jobs: []string{}}
		for _, test := range requirement.Tests {
			declared.Jobs = append(declared.Jobs, jobs[test]...)
		}
		requirements = append(requirements, declared)
	}
	exclusions := []suite.Exclusion{}
	for _, exclusion := range version.draft.Exclusions {
		for _, job := range jobs[exclusion.Test] {
			exclusions = append(exclusions, suite.Exclusion{Job: job, State: exclusion.State, Reason: exclusion.Reason, Expires: exclusion.Until})
		}
	}
	policy, err := suite.BuildCoverage(chosen.dir, requirements, exclusions)
	if err != nil {
		result.refuse(Failed, err.Error())
		return
	}
	file, err := os.CreateTemp("", "readmit-suite-coverage-*.json")
	if err != nil {
		result.refuse(Failed, "the coverage declarations cannot be held for the assessment")
		return
	}
	defer os.Remove(file.Name())
	_, err = file.Write(policy)
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err != nil {
		result.refuse(Failed, "the coverage declarations cannot be held for the assessment")
		return
	}
	report, err := suite.AssessCoverage(ctx, chosen.dir, file.Name(), previous, now)
	if errors.Is(err, context.Canceled) {
		result.refuse(Cancelled, "coverage assessment cancelled; retained evidence is unchanged")
		return
	}
	if err != nil {
		result.refuse(Failed, err.Error())
		return
	}
	result.State, result.Run, result.At = Completed, &ItemRef{Kind: RunItem, ID: chosen.id}, report.At.Format(time.RFC3339)
	result.Environment = chosen.environment
	if at := slices.IndexFunc(version.draft.Environments, func(environment SuiteEnvironment) bool { return environment.ID == chosen.environment }); at >= 0 {
		result.Environment = version.draft.Environments[at].Name
	}
	result.Denominator, result.Passed = report.Denominator, report.Passed
	for _, requirement := range report.Requirements {
		result.Requirements = append(result.Requirements, SuiteRequirementResult{ID: requirement.ID, State: requirement.State})
	}
	for _, job := range report.Jobs {
		shown := SuiteJobCoverage{Test: rows[job.ID][0], Row: rows[job.ID][1], Execution: job.Execution, Reason: job.Reason,
			Expired: job.Expired, Stability: job.Stability.State, Eligible: job.Eligible}
		if job.Exclusion != "none" {
			shown.Exclusion = job.Exclusion
		}
		result.Jobs = append(result.Jobs, shown)
	}
}

// connectedCoverage assesses a suite of saved connected tests over one of its
// retained connected executions, with the readmit-suite-coverage/v2
// declaration built from the version's requirements and exclusions and the
// exact plans and approvals the execution pinned, through the one assessment
// `readmit suite coverage` makes of a connected execution.
func (c *loadedCatalog) connectedCoverage(ctx context.Context, result *SuiteAssessment, item catalog.Item, version *suiteVersion, request SuiteCoverageRequest, now time.Time) {
	if len(version.draft.Requirements) == 0 {
		result.refuse(Empty, "This version declares no requirements")
		return
	}
	if len(request.Previous) > 0 {
		result.refuse(Failed, "a connected suite's coverage is assessed over one run")
		return
	}
	identity := suite.Identity(version.data)
	type candidate struct {
		ref       ItemRef
		dir       string
		started   time.Time
		execution suite.ConnectedExecution
	}
	runs := []candidate{}
	for _, run := range c.document.Items {
		if run.Kind != string(RunItem) || run.Entry == "" || c.removed(run) {
			continue
		}
		dir := filepath.Join(c.root, run.Entry)
		execution, err := suite.OpenConnectedExecution(ctx, dir)
		if err != nil || execution.Preparation.Suite != identity {
			continue
		}
		held := candidate{ref: ItemRef{Kind: RunItem, ID: run.ID}, dir: dir, execution: execution}
		for _, job := range execution.Report.Jobs {
			if job.Flow != nil && (held.started.IsZero() || job.Flow.StartedAt.Before(held.started)) {
				held.started = job.Flow.StartedAt
			}
		}
		runs = append(runs, held)
	}
	slices.SortStableFunc(runs, func(x, y candidate) int { return cmp.Or(y.started.Compare(x.started), cmp.Compare(x.ref.ID, y.ref.ID)) })
	var chosen *candidate
	switch {
	case request.Run != nil:
		at := slices.IndexFunc(runs, func(run candidate) bool { return run.ref.ID == request.Run.ID })
		if at < 0 {
			result.refuse(Failed, "that run did not execute this version of the suite")
			return
		}
		chosen = &runs[at]
	case len(runs) > 0:
		chosen = &runs[0]
	default:
		result.refuse(Empty, "No run of this version is retained; coverage is assessed over a run")
		return
	}
	jobs := map[string][]string{}
	tests := map[string]string{}
	for _, test := range version.draft.Tests {
		jobs[test.ID] = connectedJobIDs(version.draft, test)
		for _, job := range jobs[test.ID] {
			tests[job] = test.ID
		}
	}
	document := suite.ConnectedCoverageDocument{Schema: suite.ConnectedCoverageSchema, SuiteSHA256: identity, Specifications: []suite.ConnectedCoverageSpecification{}, Requirements: []suite.Requirement{}, Exclusions: []suite.Exclusion{}}
	for i, job := range chosen.execution.Queue.Jobs {
		pinned := chosen.execution.Document.Tests[i]
		document.Specifications = append(document.Specifications, suite.ConnectedCoverageSpecification{Job: job.ID, Plan: job.PlanIdentity, Definition: pinned.Definition, Release: pinned.ReleaseIdentity})
	}
	for _, requirement := range version.draft.Requirements {
		declared := suite.Requirement{ID: requirement.ID, Jobs: []string{}}
		for _, test := range requirement.Tests {
			declared.Jobs = append(declared.Jobs, jobs[test]...)
		}
		document.Requirements = append(document.Requirements, declared)
	}
	for _, exclusion := range version.draft.Exclusions {
		for _, job := range jobs[exclusion.Test] {
			document.Exclusions = append(document.Exclusions, suite.Exclusion{Job: job, State: exclusion.State, Reason: exclusion.Reason, Expires: exclusion.Until})
		}
	}
	raw, err := json.Marshal(document, json.Deterministic(true))
	if err != nil {
		result.refuse(Failed, "the coverage declarations cannot be held for the assessment")
		return
	}
	file, err := os.CreateTemp("", "readmit-suite-coverage-*.json")
	if err != nil {
		result.refuse(Failed, "the coverage declarations cannot be held for the assessment")
		return
	}
	defer os.Remove(file.Name())
	_, err = file.Write(raw)
	if closed := file.Close(); err == nil {
		err = closed
	}
	if err != nil {
		result.refuse(Failed, "the coverage declarations cannot be held for the assessment")
		return
	}
	report, err := suite.AssessConnectedCoverage(ctx, chosen.dir, file.Name(), now)
	if errors.Is(err, context.Canceled) {
		result.refuse(Cancelled, "coverage assessment cancelled; retained evidence is unchanged")
		return
	}
	if err != nil {
		result.refuse(Failed, err.Error())
		return
	}
	result.State, result.Run, result.At = Completed, &chosen.ref, report.At.Format(time.RFC3339)
	result.Environment = chosen.execution.Preparation.Environment
	if at := slices.IndexFunc(version.draft.Environments, func(environment SuiteEnvironment) bool { return environment.ID == result.Environment }); at >= 0 {
		result.Environment = version.draft.Environments[at].Name
	}
	result.Denominator, result.Passed = report.Denominator, report.Passed
	for _, requirement := range report.Requirements {
		result.Requirements = append(result.Requirements, SuiteRequirementResult{ID: requirement.ID, State: requirement.State})
	}
	for _, job := range report.Jobs {
		shown := SuiteJobCoverage{Test: tests[job.ID], Execution: job.Execution, Reason: job.Reason, Expired: job.Expired, Stability: job.Stability.State, Eligible: job.Eligible}
		if job.Exclusion != "none" {
			shown.Exclusion = job.Exclusion
		}
		result.Jobs = append(result.Jobs, shown)
	}
}
