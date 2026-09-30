package desktop

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/runexplain"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A run's own page (#555): what it executed and what it established, read
// through the run's own readers. Checks are listed failed and undecided
// first; a value the run did not observe is unavailable with the reason, and
// text is held back until it is shown on purpose. Every read here is local
// and changes nothing.

// RunRequest names one run of the project, one job of a suite run when Job
// is set, and whether expected and observed text is shown.
type RunRequest struct {
	Context RequestContext `json:"context"`
	Run     ItemRef        `json:"run"`
	Job     string         `json:"job,omitzero"`
	Reveal  bool           `json:"reveal"`
}

// RunDetailResult carries one run's page.
type RunDetailResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Run     *RunDetail     `json:"run,omitzero"`
}

func (r *RunDetailResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunDetail is one run, or one job of a suite run: the run as its list row
// shows it, the job's own name and result, its checks, the messages it sent
// and what came back, a suite run's jobs, the details it was run with and,
// for a run the journal recorded, what recovery establishes.
type RunDetail struct {
	ConnectedReport   *runqueue.ConnectedReport `json:"connected_report,omitzero"`
	Item              CatalogItem               `json:"item"`
	Job               string                    `json:"job,omitzero"`
	Name              string                    `json:"name"`
	Result            RunResult                 `json:"result,omitzero"`
	DeliveryUncertain bool                      `json:"delivery_uncertain"`
	Checks            []RunCheck                `json:"checks"`
	Messages          []RunMessage              `json:"messages"`
	Jobs              []RunJob                  `json:"jobs"`
	Details           RunDetails                `json:"details"`
	Recovery          *RunRecovery              `json:"recovery,omitzero"`
	Report            *RunReportSource          `json:"report,omitzero"`
	Revealed          bool                      `json:"revealed"`
}

// RunReportSource is what Create report hands the report it starts: the run,
// the case it sent from and the exact version of the test it executed, each
// as the project holds it.
type RunReportSource struct {
	Run      string   `json:"run"`
	Case     string   `json:"case,omitzero"`
	CaseItem *ItemRef `json:"case_item,omitzero"`
	Spec     string   `json:"spec,omitzero"`
	Test     *ItemRef `json:"test,omitzero"`
}

// RunCheckResult is what a run decided about one check.
type RunCheckResult string

const (
	CheckPassed       RunCheckResult = "passed"
	CheckFailed       RunCheckResult = "failed"
	CheckNotEvaluated RunCheckResult = "not_evaluated"
)

// RunCheck is one check of the run: its definition with what it expected,
// what the run observed, and the result. Text is withheld until revealed
// (Hidden); a record list is counted, its records shown only when revealed.
// Unavailable says why nothing was observed, which is never zero. Messages
// are the source occurrences the check is supported by.
type RunCheck struct {
	Check           testauthor.Expectation `json:"check"`
	Result          RunCheckResult         `json:"result"`
	Observed        *testrunner.Value      `json:"observed,omitzero"`
	ExpectedRecords *int                   `json:"expected_records,omitzero"`
	ObservedRecords *int                   `json:"observed_records,omitzero"`
	Hidden          bool                   `json:"hidden"`
	Unavailable     string                 `json:"unavailable,omitzero"`
	Messages        []string               `json:"messages"`
}

// RunDelivery is what is known about one message a run was to send.
type RunDelivery string

const (
	DeliveryAcknowledged RunDelivery = "acknowledged"
	DeliveryUncertain    RunDelivery = "uncertain"
	DeliveryNotAttempted RunDelivery = "not_attempted"
)

// RunMessage is one message a run was to send, as the source case names
// it, what is known about its delivery and the acknowledgement code the
// receiver answered with, when one was recorded.
type RunMessage struct {
	Source   string         `json:"source"`
	Message  *TestMessage   `json:"message,omitzero"`
	Delivery RunDelivery    `json:"delivery"`
	ACKCode  string         `json:"ack_code,omitzero"`
	Outcome  replay.Outcome `json:"outcome,omitzero"`
}

// RunJob is one job of a suite run: the test it ran, its result, and what
// the queue decided about it.
type RunJob struct {
	ID                string             `json:"id"`
	Test              string             `json:"test"`
	Result            RunResult          `json:"result,omitzero"`
	DeliveryUncertain bool               `json:"delivery_uncertain"`
	Admission         runqueue.Admission `json:"admission,omitzero"`
	Reason            string             `json:"reason,omitzero"`
	StateSharing      runqueue.Isolation `json:"state_sharing,omitzero"`
}

// RunDetails is what a run was run with and what it retained: the target
// it reached, the boundary it was observed at, how many records the
// observations before and after it held, the engine it was decided by, the
// raw states its readers report, and what it could not establish.
type RunDetails struct {
	Address           string        `json:"address,omitzero"`
	Transport         string        `json:"transport,omitzero"`
	Classification    string        `json:"classification,omitzero"`
	Boundary          string        `json:"boundary,omitzero"`
	InitialRecords    *int          `json:"initial_records,omitzero"`
	FinalRecords      *int          `json:"final_records,omitzero"`
	Engine            *RunEnginePin `json:"engine,omitzero"`
	LifecycleState    string        `json:"lifecycle_state,omitzero"`
	StopReason        string        `json:"stop_reason,omitzero"`
	Status            string        `json:"status,omitzero"`
	ErrorClass        string        `json:"error_class,omitzero"`
	JournalIncomplete bool          `json:"journal_incomplete"`
	Recovered         bool          `json:"recovered"`
	StartedAt         *string       `json:"started_at"`
	CompletedAt       *string       `json:"completed_at"`
	Site              string        `json:"site,omitzero"`
	SourceCase        *ItemRef      `json:"source_case,omitzero"`
	Gaps              []string      `json:"gaps"`
	Reason            string        `json:"reason,omitzero"`
}

// RunRecovery is what recovery establishes about a run its journal
// recorded: why it stopped, whether the never-attempted rest can be sent by
// Resume remaining and why not, and the state of its lease, which Clear
// stale lock removes only once the run has ended.
type RunRecovery struct {
	StopReason    string `json:"stop_reason,omitzero"`
	Terminal      bool   `json:"terminal"`
	CanResume     bool   `json:"can_resume"`
	ResumeRefusal string `json:"resume_refusal,omitzero"`
	Lease         string `json:"lease,omitzero"`
	CanClearLock  bool   `json:"can_clear_lock"`
}

// OpenRun reads one run of the project, or one job of a suite run, for its
// page. It is a read: nothing is resumed, reset or sent, and a run this
// window is writing reads as running.
func (a *App) OpenRun(request RunRequest) RunDetailResult {
	return runRead(a, false, func(ctx context.Context) RunDetailResult {
		result := RunDetailResult{Context: request.Context}
		loaded, item, path, refused := a.runOf(ctx, request)
		if loaded == nil {
			result.refuse(refused.state, refused.reason)
			return result
		}
		detail := &RunDetail{Item: *item, Name: item.Name, Checks: []RunCheck{}, Messages: []RunMessage{}, Jobs: []RunJob{},
			Details: RunDetails{Gaps: []string{}}, Revealed: request.Reveal}
		summary := item.Summary.Run
		if summary == nil {
			summary = &RunSummary{}
		}
		detail.Result, detail.DeliveryUncertain = summary.Result, summary.DeliveryUncertain
		detail.Details.StartedAt, detail.Details.CompletedAt = summary.StartedAt, summary.CompletedAt
		switch {
		case item.Availability != ItemAvailable:
			detail.Details.Reason = item.Reason
		case declares(filepath.Join(path, "manifest.json"), suite.ConnectedExecutionSchema):
			execution, err := suite.OpenConnectedExecution(ctx, path)
			if err != nil {
				result.refuse(Failed, "the connected suite execution and its linked proof do not verify")
				return result
			}
			detail.ConnectedReport = connectedReportView(execution.Report, request.Reveal)
			if request.Job != "" && !slices.ContainsFunc(execution.Report.Jobs, func(job runqueue.ConnectedJobReport) bool { return job.ID == request.Job }) {
				result.refuse(Failed, "the connected suite run holds no such job")
				return result
			}
			for _, job := range execution.Report.Jobs {
				outcome := RunIncomplete
				if !job.ExecutionError && job.Flow != nil && job.Flow.State == "complete" {
					switch job.Flow.Verdict {
					case assertion.VerdictPass:
						outcome = RunPassed
					case assertion.VerdictFail:
						outcome = RunFailed
					}
				}
				detail.Jobs = append(detail.Jobs, RunJob{ID: job.ID, Test: job.ID, Result: outcome, Admission: job.Admission, Reason: job.Reason})
				if request.Job == job.ID && job.Flow != nil {
					detail.Job, detail.Name, detail.Result = job.ID, job.ID, outcome
				}
			}
		case summary.Kind == SuiteRunKind && request.Job == "":
			loaded.suiteDetail(path, detail)
		case summary.Kind == SuiteRunKind:
			job, err := runEvidencePath(loaded.root, filepath.Join(item.Summary.Run.Entry, "runs", request.Job))
			if err != nil || strings.Contains(request.Job, "/") {
				result.refuse(Failed, "the suite run holds no such job")
				return result
			}
			detail.Job, detail.Name = request.Job, request.Job
			loaded.testDetail(job, detail, request.Reveal, false)
			if detail.Recovery != nil {
				detail.Recovery.CanResume = false
				detail.Recovery.ResumeRefusal = "this suite job cannot be resumed separately; Run again reviews a new suite execution"
			}
		case summary.Kind == SendRunKind:
			loaded.sendDetail(path, summary, detail)
		default:
			loaded.testDetail(path, detail, request.Reveal, summary.Active)
		}
		result.State, result.Run = Completed, detail
		return result
	})
}

// runOf reads the run a request names and the folder it is retained in.
func (a *App) runOf(ctx context.Context, request RunRequest) (*loadedCatalog, *CatalogItem, string, refusal) {
	if request.Run.Kind != RunItem {
		return nil, nil, "", refusal{Failed, "only a run is opened here"}
	}
	loaded, item, refused := a.catalogItem(ctx, request.Context, request.Run, false)
	if loaded == nil {
		return nil, nil, "", refusal{refused.State, refused.Reason}
	}
	record := loaded.document.Items[loaded.document.Find(item.Ref.ID)]
	if record.Entry == "" {
		return nil, nil, "", refusal{Failed, "this run has no retained folder"}
	}
	return loaded, item, runFolder(filepath.Join(loaded.root, record.Entry)), refusal{}
}

// runFolder is the folder a run's result is read from: the entry itself, or
// the result a practice run keeps inside it.
func runFolder(path string) string {
	if practiceRun(path) {
		return filepath.Join(path, practiceResult)
	}
	return path
}

// testDetail reads a test run: its checks, its messages and their
// deliveries, its details and its recovery.
func (c *loadedCatalog) testDetail(path string, detail *RunDetail, reveal, active bool) {
	opened, err := runresult.Open(path)
	var spec *testrunner.Spec
	var inputs *testrunner.PinnedInputs
	if retained, _, err := durablerun.RetainedInputs(path); err == nil {
		inputs = &retained
		if decoded, err := testrunner.DecodeSpec(retained.Spec); err == nil {
			spec = &decoded
		}
	}
	if err != nil && !active {
		detail.Details.Reason = "this run could not be verified"
		if errors.Is(err, engine.ErrUnsupportedVersion) {
			detail.Details.Reason = "this run was evaluated by a version this release cannot read"
		}
	}
	if opened != nil && opened.Spec != nil {
		spec = opened.Spec
	}
	if detail.Job != "" {
		if opened != nil {
			detail.Result = testRunResult(opened)
			detail.DeliveryUncertain = opened.Durable && opened.Lifecycle.DeliveryUncertain
		}
		if spec != nil {
			detail.Name = spec.Name
		}
	}
	types := map[string]TestMessage{}
	if spec != nil {
		for _, message := range c.sentMessages(*spec) {
			types[message.ID] = message
		}
	}
	// Messages: the run's own events, then what the journal says of every
	// planned message, which is the delivery a person reads.
	byOutbound := map[string]int{}
	if opened != nil && opened.Run != nil {
		if described, err := runexplain.DescribeRun(opened.Run); err == nil {
			for _, message := range described.Messages {
				row := RunMessage{Source: message.Source, Delivery: deliveryOf(message.Delivery), ACKCode: message.ACKCode, Outcome: message.Outcome}
				if typed, held := types[message.Source]; held {
					row.Message = &typed
				}
				byOutbound[message.Outbound] = len(detail.Messages)
				detail.Messages = append(detail.Messages, row)
			}
		}
	}
	if recovery, err := durablerun.Recover(path); err == nil {
		sources := map[string]string{}
		if inputs != nil {
			for _, mapping := range inputs.Mappings {
				sources[mapping.OutboundOccurrence] = mapping.SourceOccurrence
			}
		}
		for _, occurrence := range recovery.Occurrences {
			if at, held := byOutbound[occurrence.ID]; held {
				detail.Messages[at].Delivery = deliveryOf(occurrence.Delivery)
				continue
			}
			row := RunMessage{Source: sources[occurrence.ID], Delivery: deliveryOf(occurrence.Delivery)}
			if typed, held := types[row.Source]; held {
				row.Message = &typed
			}
			detail.Messages = append(detail.Messages, row)
		}
		detail.Recovery = &RunRecovery{StopReason: string(recovery.Run.StopReason), Terminal: recovery.Terminal, CanResume: recovery.SafeToRepeat && !active,
			ResumeRefusal: recovery.ResumeRefusal, Lease: recovery.Lease, CanClearLock: recovery.Terminal && recovery.Lease == durablerun.LeaseStale && !active}
		if active {
			detail.Recovery.ResumeRefusal = "the run is still running"
		}
		detail.Details.LifecycleState, detail.Details.StopReason = string(recovery.Run.State), string(recovery.Run.StopReason)
		detail.Details.JournalIncomplete, detail.Details.Recovered = recovery.Run.JournalIncomplete, recovery.Run.Recovered
	}
	if inputs != nil {
		detail.Details.Address, detail.Details.Transport = inputs.Configuration.Address, inputs.Configuration.Transport
		detail.Details.Classification = string(inputs.Configuration.Environment().Classification)
	}
	if opened != nil && opened.Pin != nil && opened.Pin.Document != nil {
		detail.Details.Engine = &RunEnginePin{Engine: opened.Pin.Document.Engine, Spec: opened.Pin.Document.Spec, Profile: opened.Pin.Document.Profile}
	}
	if spec != nil {
		detail.Details.Boundary = spec.Observation.Boundary
		detail.Details.SourceCase = c.specCase(*spec)
		detail.Report = &RunReportSource{Run: c.entryOf(path), CaseItem: detail.Details.SourceCase}
		if artifactEntry := spec.Input.Case; detail.Details.SourceCase != nil {
			detail.Report.Case = artifactEntry
		}
		origin := c.originOfRun(path, *spec)
		if origin.reason != "" {
			detail.Details.Gaps = append(detail.Details.Gaps, origin.reason)
		}
		if origin.test != nil {
			detail.Report.Test = origin.test
			if index := c.document.Find(origin.test.ID); index >= 0 {
				if versionPath, _, err := c.testVersionPath(c.document.Items[index], origin.test.Revision); err == nil {
					detail.Report.Spec = c.entryOf(versionPath)
				}
			}
		}
	}
	if opened == nil || opened.Artifact == nil {
		if spec != nil {
			// The run never decided its checks: each is listed as the test
			// declared it, not evaluated.
			for _, declared := range spec.Assertions {
				check := RunCheck{Check: expectationOf(declared), Result: CheckNotEvaluated, Messages: supporting(declared, detail.Messages),
					Unavailable: "the run ended before its checks were evaluated"}
				hide(&check, reveal)
				detail.Checks = append(detail.Checks, check)
			}
			if !active {
				detail.Details.Gaps = append(detail.Details.Gaps, "no finalized result: checks and observations are unknown")
			}
		}
		return
	}
	artifact := opened.Artifact
	detail.Details.Status, detail.Details.ErrorClass = string(artifact.Result.Status), artifact.Result.ErrorClass
	detail.Details.Boundary = artifact.Result.ObservationBoundary
	if record := artifact.Result.Target; record != nil {
		detail.Details.Address, detail.Details.Transport = record.Address, record.Transport
	}
	if artifact.InitialObservation != nil {
		count := len(artifact.InitialObservation.Records)
		detail.Details.InitialRecords = &count
	}
	if artifact.FinalObservation != nil {
		count := len(artifact.FinalObservation.Records)
		detail.Details.FinalRecords = &count
	}
	if detail.Details.Boundary == testrunner.ACKBoundary {
		detail.Details.Gaps = append(detail.Details.Gaps, "the appointment records were not observed by this acknowledgement-only test")
	}
	if detail.Details.Boundary == testrunner.LedgerBoundary && artifact.FinalObservation == nil {
		detail.Details.Gaps = append(detail.Details.Gaps, "the appointment records after the run were not observed")
	}
	readable := map[string]bool{}
	for _, message := range detail.Messages {
		readable[message.Source] = message.Delivery == DeliveryAcknowledged
	}
	for _, retained := range opened.Assertions {
		check := RunCheck{Check: expectationOf(retained.Assertion), Result: RunCheckResult(retained.Status), Messages: supporting(retained.Assertion, detail.Messages)}
		if retained.Observed != nil {
			observed := *retained.Observed
			check.Observed = &observed
		} else {
			check.Unavailable = unavailable(retained.Assertion, artifact, readable)
		}
		hide(&check, reveal)
		detail.Checks = append(detail.Checks, check)
	}
	// Failed and undecided checks first, then in the order the test
	// declares them.
	slices.SortStableFunc(detail.Checks, func(x, y RunCheck) int { return cmp.Compare(checkRank(x.Result), checkRank(y.Result)) })
}

func checkRank(result RunCheckResult) int {
	switch result {
	case CheckFailed:
		return 0
	case CheckNotEvaluated:
		return 1
	}
	return 2
}

// unavailable is why a check has no observed value.
func unavailable(declared testrunner.Assertion, artifact *testrunner.Artifact, readable map[string]bool) string {
	switch {
	case artifact.Result.ErrorClass != "":
		return "the run stopped with an error before this check was evaluated"
	case declared.Operator == testauthor.ACKFieldEquals && !readable[declared.Message]:
		return "no readable acknowledgement was retained for this message"
	case declared.Operator != testauthor.ACKFieldEquals && artifact.FinalObservation == nil:
		return "the appointment records after the run were not observed"
	}
	return "the run retained no observed value for this check"
}

// supporting are the source occurrences a check is supported by: the
// message an acknowledgement check reads, or every message sent before the
// records a record check reads.
func supporting(declared testrunner.Assertion, messages []RunMessage) []string {
	if declared.Operator == testauthor.ACKFieldEquals {
		return []string{declared.Message}
	}
	sources := []string{}
	for _, message := range messages {
		if message.Source != "" {
			sources = append(sources, message.Source)
		}
	}
	return sources
}

// expectationOf is a retained assertion as the test editor names a check.
func expectationOf(declared testrunner.Assertion) testauthor.Expectation {
	check := testauthor.Expectation{ID: declared.ID, Operator: declared.Operator, Message: declared.Message, Selector: declared.Selector}
	if declared.Expected.Count != nil {
		count := *declared.Expected.Count
		check.Count = &count
	}
	if declared.Expected.Records != nil {
		records := slices.Clone(*declared.Expected.Records)
		check.Records = &records
	}
	if declared.Expected.Field != nil {
		field := *declared.Expected.Field
		check.Field = &field
	}
	return check
}

// hide withholds a check's text until it is revealed: a field's text and a
// record list's records, each counted instead.
func hide(check *RunCheck, reveal bool) {
	if check.Check.Records != nil {
		count := len(*check.Check.Records)
		check.ExpectedRecords = &count
	}
	if check.Observed != nil && check.Observed.Records != nil {
		count := len(*check.Observed.Records)
		check.ObservedRecords = &count
	}
	if reveal {
		return
	}
	withheld := false
	if check.Check.Field != nil && check.Check.Field.Text != nil {
		field := testrunner.FieldValue{State: check.Check.Field.State}
		check.Check.Field, withheld = &field, true
	}
	if check.Check.Records != nil {
		check.Check.Records, withheld = nil, true
	}
	if check.Observed != nil {
		observed := *check.Observed
		if observed.Field != nil && observed.Field.Text != nil {
			observed.Field, withheld = &testrunner.FieldValue{State: observed.Field.State}, true
		}
		if observed.Records != nil {
			observed.Records, withheld = nil, true
		}
		check.Observed = &observed
	}
	check.Hidden = withheld
}

// deliveryOf is one delivery in the recovery vocabulary.
func deliveryOf(delivery string) RunDelivery {
	switch delivery {
	case durablerun.Acknowledged:
		return DeliveryAcknowledged
	case durablerun.Uncertain:
		return DeliveryUncertain
	}
	return DeliveryNotAttempted
}

// sendDetail reads a send of messages: each message's delivery and
// acknowledgement. It has no checks.
func (c *loadedCatalog) sendDetail(path string, summary *RunSummary, detail *RunDetail) {
	bundle := path
	if summary.Boundary == "transport-only" {
		bundle = filepath.Join(path, "run")
	}
	run, err := replay.Open(bundle)
	if err != nil {
		detail.Details.Reason = "the send's retained run could not be verified"
		return
	}
	detail.Details.Address, detail.Details.Transport = run.Manifest.Target.Address, run.Manifest.Target.Transport
	detail.Details.SourceCase = summary.SourceCase
	types := map[string]TestMessage{}
	if summary.SourceCase != nil {
		if index := c.document.Find(summary.SourceCase.ID); index >= 0 {
			if _, _, source := c.caseOf(c.document.Items[index].Entry); source != nil {
				for _, message := range caseMessages(source) {
					types[message.ID] = message
				}
			}
		}
	}
	for _, event := range run.Events {
		row := RunMessage{Source: event.SourceOccurrence, Delivery: deliveryOf(event.Delivery), ACKCode: event.ACK.Code, Outcome: event.Outcome}
		if typed, held := types[event.SourceOccurrence]; held {
			row.Message = &typed
		}
		detail.Messages = append(detail.Messages, row)
	}
}

// suiteDetail reads a suite run's jobs: each test's result and what the
// queue decided about it.
func (c *loadedCatalog) suiteDetail(path string, detail *RunDetail) {
	if _, _, err := c.recordedRunOrigin(path, fileDigest(filepath.Join(path, "suite.json"))); err != nil {
		detail.Details.Gaps = append(detail.Details.Gaps, err.Error())
	}
	execution, err := suite.OpenExecution(path)
	if err != nil {
		detail.Details.Reason = "the suite run could not be verified"
		return
	}
	for _, environment := range execution.Suite.Environments {
		if environment.ID == execution.Environment {
			detail.Details.Site = environment.Site
		}
	}
	decided := map[string]runqueue.JobReport{}
	if execution.Report != nil {
		for _, job := range execution.Report.Jobs {
			decided[job.ID] = job
		}
	} else {
		detail.Details.Gaps = append(detail.Details.Gaps, "the queue never recorded its report: the run was interrupted")
	}
	for _, queued := range execution.Queue.Jobs {
		job := RunJob{ID: queued.ID, Test: queued.ID, StateSharing: queued.Isolation}
		if report, held := decided[queued.ID]; held {
			job.Admission, job.Reason = report.Admission, report.Reason
		}
		jobPath := filepath.Join(path, "runs", queued.ID)
		if opened, err := runresult.Open(jobPath); err == nil {
			job.Result, job.DeliveryUncertain = testRunResult(opened), opened.Durable && opened.Lifecycle.DeliveryUncertain
			if opened.Spec != nil {
				job.Test = opened.Spec.Name
			}
		} else if inputs, _, err := durablerun.RetainedInputs(jobPath); err == nil {
			job.Result = RunInterrupted
			if spec, err := testrunner.DecodeSpec(inputs.Spec); err == nil {
				job.Test = spec.Name
			}
		} else if job.Admission == runqueue.Refused || job.Admission == runqueue.StartFailed {
			job.Result = RunBlocked
		}
		// A job that never ran, such as one skipped after its dependency
		// failed, is named by the compiled test the suite retained for it.
		if job.Test == queued.ID && filepath.Base(queued.Spec) == queued.Spec {
			if spec, err := testrunner.ReadSpec(filepath.Join(path, queued.Spec)); err == nil && spec.Name != "" {
				job.Test = spec.Name
			}
		}
		detail.Jobs = append(detail.Jobs, job)
	}
}

// ClearStaleRunLock removes the lease a run left behind once its journal
// recorded how it ended, exactly as `readmit run clean` does. A run still
// holding its lease is refused, and no evidence is removed or changed.
func (a *App) ClearStaleRunLock(request RunRequest) RunDetailResult {
	return run(a, false, false, func(ctx context.Context) RunDetailResult {
		result := RunDetailResult{Context: request.Context}
		loaded, item, path, refused := a.runOf(ctx, request)
		if loaded == nil {
			result.refuse(refused.state, refused.reason)
			return result
		}
		if request.Job != "" {
			if item.Summary.Run == nil || item.Summary.Run.Kind != SuiteRunKind || strings.ContainsAny(request.Job, "/\\") || request.Job == "." || request.Job == ".." {
				result.refuse(Failed, "the suite run holds no such job")
				return result
			}
			path = filepath.Join(path, "runs", request.Job)
		}
		if a.executing(path) {
			result.refuse(Failed, "the run is still running; its lock is in use")
			return result
		}
		if _, err := durablerun.Clean(path); err != nil {
			switch err.Error() {
			case "completion was not recorded; the writer may still hold its lease and nothing was removed",
				"durable run holds an entry this release did not write; nothing was removed",
				"cannot remove the stale durable lease; evidence is unchanged":
				result.refuse(Failed, err.Error())
			default:
				result.refuse(Failed, "the run could not be verified; nothing was removed")
			}
			return result
		}
		result.State = Completed
		return result
	})
}

// RunAnalysisRequest names one run, or one job of a suite run, and the
// saved check group version to decide against its retained evidence.
type RunAnalysisRequest struct {
	Context RequestContext `json:"context"`
	Run     ItemRef        `json:"run"`
	Job     string         `json:"job,omitzero"`
	Checks  ItemRef        `json:"checks"`
	Reveal  bool           `json:"reveal"`
}

// RunAnalysisResult is a check group decided against a run: a local
// analysis, labelled with the group and version it decided, that changes
// nothing the run recorded. Missing names the evidence a group needs that
// the run's observation did not collect.
type RunAnalysisResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Group   string         `json:"group,omitzero"`
	Version string         `json:"version,omitzero"`
	Missing []string       `json:"missing"`
	// Observation is the observation whose collections are missing, to
	// collect from.
	Observation *ItemRef        `json:"observation,omitzero"`
	Explanation *RunExplanation `json:"explanation,omitzero"`
}

func (r *RunAnalysisResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// AnalyzeRun decides one saved check group version against the evidence one
// run retained, as `readmit explain` decides it. When the group asks about
// the appointment records before or after the run, they are read from the
// collections of the observation the run's test links, the latest closed
// before the run started and the first closed after it ended; a collection
// that does not exist is named as missing, never supplied some other way.
// Nothing is sent, and the run's own result is not changed.
func (a *App) AnalyzeRun(request RunAnalysisRequest) RunAnalysisResult {
	return runNamed[RunAnalysisResult, *RunAnalysisResult](a, profiles["ExplainRun"], func(ctx context.Context) RunAnalysisResult {
		result := RunAnalysisResult{Context: request.Context, Missing: []string{}}
		loaded, item, path, refused := a.runOf(ctx, RunRequest{Context: request.Context, Run: request.Run})
		if loaded == nil {
			result.refuse(refused.state, refused.reason)
			return result
		}
		entry := item.Summary.Run.Entry
		if request.Job != "" {
			entry = entry + "/runs/" + request.Job
			jobPath, err := runEvidencePath(loaded.root, entry)
			if err != nil || strings.Contains(request.Job, "/") {
				result.refuse(Failed, "the suite run holds no such job")
				return result
			}
			path = jobPath
		}
		index := loaded.document.Find(request.Checks.ID)
		if request.Checks.Kind != CheckGroupItem || index < 0 || loaded.document.Items[index].Kind != string(CheckGroupItem) {
			result.refuse(Failed, "the project holds no such check group")
			return result
		}
		group := loaded.document.Items[index]
		version := cmp.Or(request.Checks.Revision, group.RevisionLabel())
		result.Group, result.Version = loaded.itemName(group.ID), version
		setPath, err := loaded.checkGroupSet(group, version)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		explain := RunExplanationRequest{Run: entry, Assertions: loaded.entryOf(setPath), Reveal: request.Reveal}
		before, after := asksRecords(setPath)
		if before || after {
			pairs, missing, observation := loaded.observedAround(path)
			result.Observation = observation
			if before {
				explain.Before, explain.BeforeSource = pairs.before, pairs.source
				if pairs.before == "" {
					result.Missing = append(result.Missing, missing.before)
				}
			}
			if after {
				explain.After, explain.AfterSource = pairs.after, pairs.source
				if pairs.after == "" {
					result.Missing = append(result.Missing, missing.after)
				}
			}
			if len(result.Missing) > 0 {
				result.refuse(Failed, "this check group reads appointment records this run's observation did not collect")
				return result
			}
		}
		explained := explainRunAt(ctx, loaded.root, explain)
		result.State, result.Reason, result.Explanation = explained.State, explained.Reason, explained.Explanation
		return result
	})
}

// asksRecords reports whether a check group asks about the records observed
// before a run, after it, or both.
func asksRecords(path string) (bool, bool) {
	data, err := boundedFile(path, assertion.MaxSetBytes)
	if err != nil {
		return false, false
	}
	set, err := assertion.Decode(data)
	if err != nil {
		return false, false
	}
	before, after := false, false
	note := func(scope assertion.RecordScope) {
		before = before || scope == assertion.BeforeRecords
		after = after || scope == assertion.AfterRecords
	}
	for _, declared := range set.Assertions {
		switch subject := declared.Subject; {
		case subject.Collection != nil:
			note(subject.Collection.Scope)
		case subject.Each != nil:
			note(subject.Each.Scope)
		case subject.Transition != nil:
			note(subject.Transition.From)
			note(subject.Transition.To)
		}
	}
	return before, after
}

// observed are the completion records around a run and the source they
// read, as project entries; missing says what is not there.
type observed struct{ before, after, source string }

// observedAround finds the collections of the observation the run's test
// links that bracket the run, and names that observation.
func (c *loadedCatalog) observedAround(path string) (observed, observed, *ItemRef) {
	missing := observed{before: "a collection of the observation before this run", after: "a collection of the observation after this run"}
	found := observed{}
	var spec *testrunner.Spec
	var started, completed time.Time
	if opened, err := runresult.Open(path); err == nil {
		spec = opened.Spec
		if opened.Run != nil {
			started, completed = opened.Run.Manifest.StartedAt, opened.Run.Manifest.CompletedAt
		}
	}
	if spec == nil {
		return found, missing, nil
	}
	origin := c.originOfRun(path, *spec)
	if origin.test == nil {
		missing.before, missing.after = "the observation this run's test links, which the project no longer holds", "the observation this run's test links, which the project no longer holds"
		return found, missing, nil
	}
	saved, err := c.testOf(c.document.Items[c.document.Find(origin.test.ID)], origin.test.Revision)
	if err != nil || saved.links == nil || saved.links.Observation == "" {
		missing.before, missing.after = "an observation linked to this run's test", "an observation linked to this run's test"
		return found, missing, nil
	}
	index := c.document.Find(saved.links.Observation)
	if index < 0 || c.document.Items[index].Kind != string(ObservationItem) {
		missing.before, missing.after = "the observation this run's test links", "the observation this run's test links"
		return found, missing, nil
	}
	item := c.document.Items[index]
	ref := &ItemRef{Kind: ObservationItem, ID: item.ID, Revision: item.RevisionLabel()}
	name := c.itemName(item.ID)
	missing.before, missing.after = "a collection of "+name+" before this run", "a collection of "+name+" after this run"
	declared, err := c.observationOf(item)
	if err != nil {
		return found, missing, ref
	}
	paths, availability, _ := c.backing(item)
	if availability != ItemAvailable {
		return found, missing, ref
	}
	found.source = c.entryOf(paths["source"])
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return found, missing, ref
	}
	var beforeAt, afterAt time.Time
	for _, entry := range entries {
		candidate := filepath.Join(c.root, entry.Name())
		if !entry.Type().IsRegular() || !declares(candidate, observewindow.CompletionSchema) {
			continue
		}
		completion, err := observewindow.ReadCompletion(candidate)
		if err != nil || completion.Source != declared.Source.Observes || !completion.Trustworthy() {
			continue
		}
		closed := completion.ClosedAt
		if !started.IsZero() && closed.Before(started) && closed.After(beforeAt) {
			found.before, beforeAt = entry.Name(), closed
		}
		if !completed.IsZero() && closed.After(completed) && (afterAt.IsZero() || closed.Before(afterAt)) {
			found.after, afterAt = entry.Name(), closed
		}
	}
	return found, missing, ref
}

// RunComparisonItemsRequest names the two runs a comparison is between and
// up to fourteen further runs of the same test whose results are counted.
type RunComparisonItemsRequest struct {
	Context RequestContext `json:"context"`
	Runs    []ItemRef      `json:"runs"`
}

// RunComparisonItemsResult carries one comparison.
type RunComparisonItemsResult struct {
	State      State              `json:"state"`
	Reason     string             `json:"reason,omitzero"`
	Context    RequestContext     `json:"context"`
	Comparison *RunComparisonView `json:"comparison,omitzero"`
}

func (r *RunComparisonItemsResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// CheckChange is how one check changed from the earlier run to the later.
// A changed definition is a changed check, never a regression.
type CheckChange string

const (
	CheckImproved         CheckChange = "improved"
	CheckRegressed        CheckChange = "regressed"
	CheckUnchanged        CheckChange = "unchanged"
	CheckObservedChanged  CheckChange = "observed_changed"
	CheckDefinitionChange CheckChange = "changed_check"
	CheckAdded            CheckChange = "added"
	CheckRemoved          CheckChange = "removed"
	CheckNotCompared      CheckChange = "not_compared"
)

// RunComparisonView is a comparison of two runs: which is earlier and
// which later by when each started, each check aligned by its identity and
// definition, each part of the configuration compared on its own, and the
// results of every run counted across the repeats.
type RunComparisonView struct {
	Earlier       RunComparisonSide      `json:"earlier"`
	Later         RunComparisonSide      `json:"later"`
	Repeats       []RunComparisonSide    `json:"repeats"`
	Checks        []RunCheckComparison   `json:"checks"`
	Configuration []RunConfigurationPart `json:"configuration"`
	Specification string                 `json:"specification"`
	Stability     RunStability           `json:"stability"`
}

// RunComparisonSide is one run of a comparison as its row names it.
type RunComparisonSide struct {
	Run             ItemRef   `json:"run"`
	Name            string    `json:"name"`
	Version         string    `json:"version,omitzero"`
	EnvironmentName string    `json:"environment_name,omitzero"`
	StartedAt       *string   `json:"started_at"`
	Result          RunResult `json:"result,omitzero"`
}

// RunCheckComparison is one check in both runs: its definition, what each
// run observed and decided, and how it changed. An observed value's text is
// never shown here; counts are.
type RunCheckComparison struct {
	Check           testauthor.Expectation `json:"check"`
	Earlier         string                 `json:"earlier"`
	Later           string                 `json:"later"`
	EarlierObserved *int                   `json:"earlier_observed,omitzero"`
	LaterObserved   *int                   `json:"later_observed,omitzero"`
	Change          CheckChange            `json:"change"`
}

// RunConfigurationPart is one part of what the runs were run with, compared
// on its own: input, target, engine or profile. Parts name what differs,
// never a value; no cause is inferred.
type RunConfigurationPart struct {
	Part    string   `json:"part"`
	Outcome string   `json:"outcome"`
	Parts   []string `json:"parts"`
	Reason  string   `json:"reason,omitzero"`
}

// RunStability is what the retained results of every compared run show:
// how many distinct results there are and how they ended, and the checks
// that both passed and failed across them. It infers no probability.
type RunStability struct {
	State      string   `json:"state"`
	Runs       int      `json:"runs"`
	Passes     int      `json:"passes"`
	Failures   int      `json:"failures"`
	Errors     int      `json:"errors"`
	Incomplete int      `json:"incomplete"`
	Flaky      []string `json:"flaky"`
	Reason     string   `json:"reason,omitzero"`
}

// CompareRunItems compares two runs of the project, and counts up to
// fourteen further runs, through the retained-execution comparison
// `readmit compare-runs` reports. The earlier run is the one that started
// first. It is a read.
func (a *App) CompareRunItems(request RunComparisonItemsRequest) RunComparisonItemsResult {
	return runNamed[RunComparisonItemsResult, *RunComparisonItemsResult](a, profiles["CompareRuns"], func(ctx context.Context) RunComparisonItemsResult {
		result := RunComparisonItemsResult{Context: request.Context}
		if len(request.Runs) < 2 || len(request.Runs) > 2+runcompare.MaxRepeats {
			result.refuse(Failed, "a comparison is between two runs, with up to fourteen more")
			return result
		}
		loaded, declined := a.loadCatalog(ctx, request.Context, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		sides := make([]RunComparisonSide, 0, len(request.Runs))
		opened := make([]*runresult.Result, 0, len(request.Runs))
		seen := map[string]bool{}
		for _, ref := range request.Runs {
			window, index, declined := a.findAcross(ctx, request.Context, loaded, ref.ID, false)
			if window == nil {
				result.refuse(declined.state, declined.reason)
				return result
			}
			if index < 0 || ref.Kind != RunItem || window.document.Items[index].Kind != string(RunItem) || seen[ref.ID] {
				result.refuse(Failed, "a comparison is between distinct runs of the project")
				return result
			}
			seen[ref.ID] = true
			record := window.document.Items[index]
			item := window.read(record)
			summary := item.Summary.Run
			if item.Availability != ItemAvailable || summary == nil || summary.Kind != TestRunKind || summary.Active {
				result.refuse(Failed, cmp.Or(item.Name, "A run")+" cannot be compared: only a finished run of a test is")
				return result
			}
			retained, err := runresult.Open(runFolder(filepath.Join(window.root, record.Entry)))
			if err != nil {
				result.refuse(Failed, cmp.Or(item.Name, "A run")+" cannot be compared: its retained evidence did not verify")
				return result
			}
			opened = append(opened, retained)
			sides = append(sides, RunComparisonSide{Run: item.Ref, Name: item.Name, Version: summary.Version, EnvironmentName: summary.EnvironmentName,
				StartedAt: summary.StartedAt, Result: summary.Result})
		}
		// Earlier and later are set by when each run started; the later run's
		// start is unknown last.
		if stampOf(sides[1].StartedAt) != "" && (stampOf(sides[0].StartedAt) == "" || stampOf(sides[1].StartedAt) < stampOf(sides[0].StartedAt)) {
			sides[0], sides[1] = sides[1], sides[0]
			opened[0], opened[1] = opened[1], opened[0]
		}
		report, err := runcompare.CompareOpened(ctx, runcompare.OpenedInput{Baseline: opened[0], Current: opened[1], Repeats: opened[2:]})
		if errors.Is(err, context.Canceled) {
			result.refuse(Cancelled, "the comparison was stopped")
			return result
		}
		if err != nil {
			result.refuse(Failed, comparisonRefusal(err))
			return result
		}
		view := &RunComparisonView{Earlier: sides[0], Later: sides[1], Repeats: sides[2:], Checks: []RunCheckComparison{},
			Configuration: []RunConfigurationPart{}, Specification: report.Specification}
		definitions := map[string]testrunner.AssertionResult{}
		earlier := map[string]testrunner.AssertionResult{}
		for _, retained := range opened[0].Assertions {
			definitions[retained.Assertion.ID], earlier[retained.Assertion.ID] = retained, retained
		}
		later := map[string]testrunner.AssertionResult{}
		for _, retained := range opened[1].Assertions {
			definitions[retained.Assertion.ID], later[retained.Assertion.ID] = retained, retained
		}
		for _, row := range report.Assertions {
			compared := RunCheckComparison{Check: expectationOf(definitions[row.ID].Assertion), Earlier: row.Baseline, Later: row.Current, Change: checkChange(row)}
			compared.EarlierObserved = observedCount(earlier[row.ID].Observed)
			compared.LaterObserved = observedCount(later[row.ID].Observed)
			hidden := RunCheck{Check: compared.Check}
			hide(&hidden, false)
			compared.Check = hidden.Check
			view.Checks = append(view.Checks, compared)
		}
		for _, part := range report.Drift.Drift {
			view.Configuration = append(view.Configuration, RunConfigurationPart{Part: part.Cause, Outcome: part.Outcome, Parts: slices.Clone(part.Parts), Reason: part.Reason})
		}
		stability := report.Stability
		view.Stability = RunStability{State: stability.State, Runs: stability.Runs, Passes: stability.Passes, Failures: stability.Failures,
			Errors: stability.Errors, Incomplete: stability.Incomplete, Flaky: slices.Clone(stability.FlakyAssertions), Reason: stability.Reason}
		result.State, result.Comparison = Completed, view
		return result
	})
}

// checkChange is how one aligned check changed. Only an unchanged
// definition compares results.
func checkChange(row runcompare.AssertionComparison) CheckChange {
	switch row.Definition {
	case "added":
		return CheckAdded
	case "removed":
		return CheckRemoved
	case "changed":
		return CheckDefinitionChange
	case "unchanged":
	default:
		return CheckNotCompared
	}
	switch {
	case row.Baseline == testrunner.NotEvaluated || row.Current == testrunner.NotEvaluated:
		return CheckNotCompared
	case row.Baseline == testrunner.Failed && row.Current == testrunner.Passed:
		return CheckImproved
	case row.Baseline == testrunner.Passed && row.Current == testrunner.Failed:
		return CheckRegressed
	case row.Behavior == "changed":
		return CheckObservedChanged
	}
	return CheckUnchanged
}

// observedCount is an observed value a comparison shows: a count, or how
// many records were observed. Text is never shown here.
func observedCount(value *testrunner.Value) *int {
	switch {
	case value == nil:
		return nil
	case value.Count != nil:
		count := *value.Count
		return &count
	case value.Records != nil:
		count := len(*value.Records)
		return &count
	}
	return nil
}

// comparisonRefusal is the comparison's own refusal, without a local path.
func comparisonRefusal(err error) string {
	switch text := err.Error(); {
	case strings.Contains(text, string(filepath.Separator)):
		return "these runs cannot be compared: their retained evidence did not verify"
	default:
		return text
	}
}

// suiteProgress is how far a suite run writing its jobs has got: how many
// of its queued jobs have a retained run that ended.
func suiteProgress(path string) (RunProgress, error) {
	raw, err := readBoundedEntry(filepath.Join(path, "queue.json"), runqueue.MaxPlanBytes)
	if err != nil {
		return RunProgress{}, err
	}
	plan, err := runqueue.DecodePlan(raw)
	if err != nil {
		return RunProgress{}, err
	}
	progress := RunProgress{Phase: "executing", Jobs: len(plan.Jobs)}
	for _, job := range plan.Jobs {
		if recovery, err := durablerun.Recover(filepath.Join(path, "runs", job.ID)); err == nil {
			if recovery.Terminal {
				progress.JobsDone++
			}
			progress.Acknowledged += recovery.Acknowledged
			progress.Uncertain += recovery.Uncertain
		}
	}
	return progress, nil
}
