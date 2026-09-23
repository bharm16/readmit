package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runexplain"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The durable-run panels. This file connects what the window already authors
// and lists into execution and analysis: preflight reads the exact plan a send
// would execute, execution is the existing durable path under an identity the
// preflight fixed, progress is a read of the journal the run is writing, and
// the evidence view opens retained results and jobs through the readers the
// command line uses. Nothing here schedules, evaluates or resets anything the
// existing packages do not already own.

// runOperation is the operation name the durable-run panels cancel through.
const runOperation = "durable-run"

// DurableRunResult separates facade success from the run's execution state.
// A completed read can report an interrupted or assertion-failed run.
type DurableRunResult struct {
	State  State               `json:"state"`
	Reason string              `json:"reason,omitzero"`
	Run    *durablerun.Summary `json:"run,omitzero"`
}

func (r *DurableRunResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// DurableRunRequest is one deliberate execution: the open workspace, the saved
// spec entry, the fresh output entry it is written into, and the spec identity
// the preflight fixed. A spec whose bytes no longer hash to Expected is
// refused rather than executed as though nothing had changed, so a preflight
// can never go stale in silence.
type DurableRunRequest struct {
	Workspace string `json:"workspace"`
	Spec      string `json:"spec"`
	Output    string `json:"output"`
	Expected  string `json:"expected_identity"`
}

// StartDurableRun sends once with an explicit operator action, retaining a new
// journal directory. Cancel stops future sends; in-flight effects remain
// visible. Execution happens only under the operation guard's own admission:
// an unconfigured, expired or released term refuses here before anything is
// created, exactly as the preflight said it would.
func (a *App) StartDurableRun(request DurableRunRequest) DurableRunResult {
	return runNamed[DurableRunResult, *DurableRunResult](a, runOperation, true, false, func(ctx context.Context) (out DurableRunResult) {
		guard, _ := a.selectedOperation()
		settle, admissionErr := guard.AdmitContext(ctx, "execute")
		if admissionErr != nil {
			declined := admissionRefusal(ctx, admissionErr)
			return DurableRunResult{State: declined.state, Reason: declined.reason}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State = Failed
				out.Reason = "runner settlement failed; reconcile the retained admission before new work"
			}
		}()
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return DurableRunResult{State: declined.state, Reason: declined.reason}
		}
		specPath, err := runEntryPath(root, request.Spec)
		if err != nil {
			return DurableRunResult{State: Failed, Reason: "the saved test must be one regular entry of the open workspace"}
		}
		output, err := runEntryPath(root, request.Output)
		if err != nil {
			return DurableRunResult{State: Failed, Reason: "the run folder must be one new entry of the open workspace"}
		}
		prepared, err := durablerun.Prepare(specPath)
		if err != nil {
			return DurableRunResult{State: Failed, Reason: "the saved test could not be prepared; nothing was sent"}
		}
		if request.Expected != "" && digestOf(prepared.PinnedInputs().Spec) != request.Expected {
			return DurableRunResult{State: Failed, Reason: "the selected test changed after the preflight; preflight it again before executing"}
		}
		// The folder this run is writing is named while it executes, so the
		// progress read can tell a journal this window is still writing from
		// one a crash left behind, which is the difference between a live
		// count and an interruption.
		a.setRunOutput(output)
		defer a.setRunOutput("")
		bounded, cancel := context.WithTimeout(ctx, operationguard.MaxDuration)
		defer cancel()
		result, err := prepared.Start(bounded, output)
		if err != nil {
			if result.Schema != "" {
				return DurableRunResult{State: Failed, Run: &result, Reason: "journal persistence failed; recover retained output before any new execution"}
			}
			return DurableRunResult{State: Failed, Reason: "the durable run could not finish; recover the retained output to inspect partial evidence"}
		}
		return DurableRunResult{State: Completed, Run: &result}
	})
}

// setRunOutput records the folder the durable-run operation is executing into.
func (a *App) setRunOutput(output string) {
	a.mu.Lock()
	a.runOutput = output
	a.mu.Unlock()
}

// executing reports whether the operation holding the slot is this window's
// durable run writing exactly path.
func (a *App) executing(path string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running && a.operation == runOperation && a.runOutput == path
}

// OpenDurableRun is read-only recovery and never acquires send authority.
func (a *App) OpenDurableRun(path string) DurableRunResult {
	return run(a, false, false, func(context.Context) DurableRunResult {
		result, err := durablerun.Open(path)
		if errors.Is(err, engine.ErrUnsupportedVersion) {
			return DurableRunResult{State: Failed, Reason: "the durable run was evaluated by a version this release cannot read; its evidence has not been changed"}
		}
		if err != nil {
			return DurableRunResult{State: Failed, Reason: "the durable run could not be verified; partial evidence has not been changed"}
		}
		return DurableRunResult{State: Completed, Run: &result}
	})
}

// RunPreflightRequest asks what one execution would do without doing any of
// it. Spec names one regular workspace entry declaring readmit-test/v1, or one
// declaring readmit-suite/v1 together with the Environment it is executed at.
// Output names the new entry the run writes into; empty asks for the next free
// generated name, so a person never copies an internal path by hand.
type RunPreflightRequest struct {
	Workspace   string `json:"workspace"`
	Spec        string `json:"spec"`
	Environment string `json:"environment,omitzero"`
	Output      string `json:"output,omitzero"`
}

// RunPreflightResult carries one state. Preflight is present whenever the
// selection could be prepared; a preparation refusal is a Failed result with
// the reason, never a partial preflight that reads as approved.
type RunPreflightResult struct {
	State     State         `json:"state"`
	Reason    string        `json:"reason,omitzero"`
	Preflight *RunPreflight `json:"preflight,omitzero"`
}

func (r *RunPreflightResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunPreflight is what one execution would do, read out of the exact plan a
// send would execute. It is a local validation: no network connection is
// opened, nothing is sent or reset, and no result verdict exists yet. The
// identity is what execution pins itself to, so a changed input is refused by
// the send rather than read as the input that was preflighted.
type RunPreflight struct {
	Kind         string          `json:"kind"` // "test" or "suite"
	Spec         string          `json:"spec"`
	Name         string          `json:"name"`
	Schema       string          `json:"schema"`
	Identity     string          `json:"identity"`
	Selected     []RunSelected   `json:"selected"`
	Target       RunTargetView   `json:"target"`
	Boundary     string          `json:"boundary"`
	Observation  string          `json:"observation,omitzero"`
	InitialState string          `json:"initial_state"`
	Reset        string          `json:"reset,omitzero"`
	Engine       RunEnginePin    `json:"engine"`
	Deadline     string          `json:"deadline"`
	Destination  RunDestination  `json:"destination"`
	Admission    RunAdmission    `json:"admission"`
	Suite        *SuitePreflight `json:"suite,omitzero"`
}

// RunSelected is one occurrence an execution sends, named the way the source
// case names it and the way the run journal will.
type RunSelected struct {
	Source   string `json:"source"`
	Outbound string `json:"outbound"`
}

// RunTargetView is the sealed target an execution would connect to and the
// environment it records. No credential value is here and none exists to
// show: a target names a reference, and the reference names a program.
type RunTargetView struct {
	Name              string `json:"name,omitzero"`
	Classification    string `json:"classification"`
	Address           string `json:"address"`
	Transport         string `json:"transport"`
	TestEndpoint      bool   `json:"test_endpoint"`
	ApprovedTransport bool   `json:"approved_transport"`
	ConnectTimeout    string `json:"connect_timeout"`
	MessageTimeout    string `json:"message_timeout"`
	MaxACKBytes       int    `json:"max_ack_bytes"`
	Credential        bool   `json:"credential"`
}

// RunEnginePin is the three versions a verdict will depend on: the build that
// would execute, the contract the spec declares and the profile its
// observations are evaluated under.
type RunEnginePin struct {
	Engine  string `json:"engine"`
	Spec    string `json:"spec"`
	Profile string `json:"profile"`
}

// RunDestination is the fresh output entry an execution requires. Generated is
// true when this preflight proposed the name because none was given.
type RunDestination struct {
	Name      string `json:"name"`
	Generated bool   `json:"generated"`
	Fresh     bool   `json:"fresh"`
	Reason    string `json:"reason,omitzero"`
}

// RunAdmission is the backend's own decision about new work, asked the same
// way execution asks it. A denied preflight names why; it is never a verdict
// about the test and never becomes one by executing anyway.
type RunAdmission struct {
	Admitted bool   `json:"admitted"`
	Reason   string `json:"reason,omitzero"`
}

// SuitePreflight is the suite half of a preflight: the environments it
// declares, the tests it would execute, and — once one environment is
// selected — the target configurations that environment's bindings resolve
// to. It decodes and reads only; the queue the suite expands into is written
// by execution, and nothing here sends.
type SuitePreflight struct {
	ID           string          `json:"id"`
	Environments []string        `json:"environments"`
	Environment  string          `json:"environment,omitzero"`
	Site         string          `json:"site,omitzero"`
	Parallelism  int             `json:"parallelism"`
	References   string          `json:"references,omitzero"`
	Jobs         []SuiteJobView  `json:"jobs"`
	Targets      []RunTargetView `json:"targets"`
}

// SuiteJobView is one test of a suite as its own document declares it.
type SuiteJobView struct {
	ID        string   `json:"id"`
	Spec      string   `json:"spec"`
	Parameter string   `json:"parameter"`
	Isolation string   `json:"isolation"`
	After     []string `json:"after"`
	Rows      int      `json:"rows"`
	Sequence  int      `json:"sequence"`
}

// PreflightRun validates one execution locally and reports exactly what it
// would do. It reads the spec, its case, its target configuration and the
// operation guard's admission state; it opens no network connection, sends
// nothing, resets nothing and reports no result verdict. Changed inputs
// invalidate the preflight through the identity it returns, which execution
// requires before it starts.
func (a *App) PreflightRun(request RunPreflightRequest) RunPreflightResult {
	return run(a, false, false, func(ctx context.Context) RunPreflightResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return RunPreflightResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEntryPath(root, request.Spec)
		if err != nil {
			return RunPreflightResult{State: Failed, Reason: "the saved test must be one regular entry of the open workspace"}
		}
		if kind := declaredEntryKind(root, request.Spec); kind == SuiteArtifact {
			return a.preflightSuite(ctx, root, request, path)
		}
		if kind := declaredEntryKind(root, request.Spec); kind != SpecArtifact {
			return RunPreflightResult{State: Failed, Reason: "that entry does not declare a saved test this release executes"}
		}
		return a.preflightTest(ctx, root, request, path)
	})
}

func (a *App) preflightTest(ctx context.Context, root string, request RunPreflightRequest, specPath string) RunPreflightResult {
	prepared, err := durablerun.Prepare(specPath)
	if err != nil {
		return RunPreflightResult{State: Failed, Reason: "the saved test could not be prepared: its case, target or selection did not verify"}
	}
	inputs := prepared.PinnedInputs()
	spec, err := testrunner.DecodeSpec(inputs.Spec)
	if err != nil {
		return RunPreflightResult{State: Failed, Reason: "the saved test could not be read back"}
	}
	destination, refused := destinationFor(root, request.Output, "job")
	if refused.state != "" {
		return RunPreflightResult{State: refused.state, Reason: refused.reason}
	}
	preflight := RunPreflight{
		Kind:         "test",
		Spec:         request.Spec,
		Name:         spec.Name,
		Schema:       spec.Schema,
		Identity:     digestOf(inputs.Spec),
		Selected:     selected(inputs.Mappings),
		Target:       targetView(inputs.Configuration),
		Boundary:     spec.Observation.Boundary,
		Observation:  spec.Observation.Path,
		InitialState: spec.Setup.InitialState,
		Reset:        spec.Setup.ResetInstructions,
		Engine:       enginePin(spec.Schema),
		Deadline:     operationguard.MaxDuration.String(),
		Destination:  destination,
		Suite:        nil,
	}
	preflight.Admission = a.admissionPreview(ctx)
	return RunPreflightResult{State: Completed, Preflight: &preflight}
}

func (a *App) preflightSuite(ctx context.Context, root string, request RunPreflightRequest, suitePath string) RunPreflightResult {
	raw, err := readBoundedEntry(suitePath, suite.MaxBytes)
	if err != nil {
		return RunPreflightResult{State: Failed, Reason: "the suite document could not be read"}
	}
	document, err := suite.Decode(raw)
	if err != nil {
		return RunPreflightResult{State: Failed, Reason: "that entry does not declare a suite this release reads"}
	}
	var selected *suite.Environment
	environments := make([]string, 0, len(document.Environments))
	for i := range document.Environments {
		environments = append(environments, document.Environments[i].ID)
		if document.Environments[i].ID == request.Environment {
			selected = &document.Environments[i]
		}
	}
	if request.Environment != "" && selected == nil {
		return RunPreflightResult{State: Failed, Reason: "the suite does not declare the selected environment"}
	}
	destination, refused := destinationFor(root, request.Output, "job")
	if refused.state != "" {
		return RunPreflightResult{State: refused.state, Reason: refused.reason}
	}
	view := SuitePreflight{ID: document.ID, Environments: environments, Parallelism: document.Parallelism, Jobs: []SuiteJobView{}, Targets: []RunTargetView{}}
	if selected != nil {
		view.Environment, view.Site = selected.ID, selected.Site
	}
	rows := map[string]int{}
	for _, table := range document.Tables {
		rows[table.ID] = len(table.Rows)
	}
	for _, test := range document.Tests {
		view.Jobs = append(view.Jobs, SuiteJobView{ID: test.ID, Spec: test.Spec, Parameter: test.Parameter, Isolation: string(test.Isolation), After: test.After, Rows: rows[test.Table], Sequence: len(test.Sequence)})
	}
	if selected != nil {
		// Each binding is resolved through the target reader, which is the
		// local validation a send would run: an unreadable configuration
		// refuses the preflight rather than the execution.
		for _, binding := range selected.Bindings {
			targetPath, err := runEntryPath(root, binding.Target)
			if err != nil {
				return RunPreflightResult{State: Failed, Reason: "an environment binding names a target that is not one regular entry of the open workspace"}
			}
			target, err := replay.ReadTarget(targetPath)
			if err != nil {
				return RunPreflightResult{State: Failed, Reason: "an environment binding names a target configuration this release cannot read"}
			}
			view.Targets = append(view.Targets, targetView(target))
		}
	}
	preflight := RunPreflight{
		Kind:        "suite",
		Spec:        request.Spec,
		Name:        document.ID,
		Schema:      suite.Schema,
		Identity:    digestOf(raw),
		Selected:    []RunSelected{},
		Target:      RunTargetView{},
		Boundary:    "",
		Engine:      enginePin(testrunner.SpecSchema),
		Deadline:    operationguard.MaxDuration.String(),
		Destination: destination,
		Suite:       &view,
	}
	if selected == nil {
		// No environment is selected yet: the preflight shows the suite's
		// shape and the fresh destination but no admission, because there is
		// nothing to admit until an environment is chosen.
		preflight.Admission = RunAdmission{Admitted: false, Reason: "select one of the environments the suite declares"}
		return RunPreflightResult{State: Completed, Preflight: &preflight}
	}
	preflight.Admission = a.admissionPreview(ctx)
	return RunPreflightResult{State: Completed, Preflight: &preflight}
}

// RunSpecChoiceResult carries one state. Entry names the selected workspace
// entry; a file outside the open workspace is refused with the reason, because
// execution names a saved test of the open workspace.
type RunSpecChoiceResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Entry  string `json:"entry,omitzero"`
}

func (r *RunSpecChoiceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ChooseRunSpec is the native advanced file selection of a saved test or
// suite: the host's file dialog over JSON documents. The chosen file must be
// one entry of the open workspace, which is what execution names; a dismissed
// dialog is a cancellation.
func (a *App) ChooseRunSpec(workspace string) RunSpecChoiceResult {
	return run(a, true, false, func(ctx context.Context) RunSpecChoiceResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return RunSpecChoiceResult{State: declined.state, Reason: declined.reason}
		}
		files, declined := a.chooseFiles(ctx, "Choose a saved test or suite", "readmit documents", "*.json")
		if len(files) == 0 {
			return RunSpecChoiceResult{State: declined.state, Reason: declined.reason}
		}
		relative, err := filepath.Rel(root, files[0])
		if err != nil || artifactpath.EntryName(relative) != nil {
			return RunSpecChoiceResult{State: Failed, Reason: "a saved test or suite is one entry of the open workspace; advanced selection cannot reach outside it"}
		}
		return RunSpecChoiceResult{State: Completed, Entry: relative}
	})
}

// admissionPreview asks the operation guard the same question execution asks,
// and settles immediately: the decision shown is the decision a send would
// get, and asking it changes nothing.
func (a *App) admissionPreview(ctx context.Context) RunAdmission {
	guard, _ := a.selectedOperation()
	settle, err := guard.AdmitContext(ctx, "execute")
	if err != nil {
		return RunAdmission{Admitted: false, Reason: err.Error()}
	}
	if err := settle(); err != nil {
		return RunAdmission{Admitted: false, Reason: "the operation clock could not be reconciled; resolve it before new work"}
	}
	return RunAdmission{Admitted: true}
}

// SuiteRunRequest is one deliberate suite execution: the suite entry, the
// environment it is executed at, the optional released-expectation references
// that make it an approved suite, the fresh output entry, and the suite
// identity the preflight fixed.
type SuiteRunRequest struct {
	Workspace   string `json:"workspace"`
	Suite       string `json:"suite"`
	Environment string `json:"environment"`
	References  string `json:"references,omitzero"`
	Output      string `json:"output"`
	Expected    string `json:"expected_identity"`
}

// SuiteRunResult carries one state. Report is the queue's own report — what
// was admitted, refused, skipped and executed, and each job's own durable
// summary — exactly as `readmit suite run` retains it.
type SuiteRunResult struct {
	State  State           `json:"state"`
	Reason string          `json:"reason,omitzero"`
	Output string          `json:"output,omitzero"`
	Report *suiteRunReport `json:"report,omitzero"`
}

func (r *SuiteRunResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// suiteRunReport is the facade's view of the run-queue report: the counts and
// one row per job with its admission decision and its own run summary.
type suiteRunReport struct {
	Schema      string        `json:"schema"`
	Parallelism int           `json:"parallelism"`
	Executed    int           `json:"executed"`
	StartFailed int           `json:"start_failed"`
	Refused     int           `json:"refused"`
	Skipped     int           `json:"skipped"`
	Jobs        []suiteRunJob `json:"jobs"`
}

type suiteRunJob struct {
	ID        string              `json:"id"`
	Admission string              `json:"admission"`
	Isolation string              `json:"isolation"`
	Reason    string              `json:"reason,omitzero"`
	Run       *durablerun.Summary `json:"run,omitzero"`
}

// StartSuiteRun executes a suite's jobs through the existing durable queue:
// one foreground execution into a fresh destination, admitting against the
// same leases and serializing the same shared resources the command line
// does. Scheduling, isolation and every refusal are the queue's own; this
// panel adds no parallelism and relaxes no rule. Cancel stops future jobs;
// what already ran is retained exactly as the queue retains it.
func (a *App) StartSuiteRun(request SuiteRunRequest) SuiteRunResult {
	return runNamed[SuiteRunResult, *SuiteRunResult](a, runOperation, true, false, func(ctx context.Context) (out SuiteRunResult) {
		guard, _ := a.selectedOperation()
		settle, admissionErr := guard.AdmitContext(ctx, "execute")
		if admissionErr != nil {
			declined := admissionRefusal(ctx, admissionErr)
			return SuiteRunResult{State: declined.state, Reason: declined.reason}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State = Failed
				out.Reason = "runner settlement failed; reconcile the retained admission before new work"
			}
		}()
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SuiteRunResult{State: declined.state, Reason: declined.reason}
		}
		suitePath, err := runEntryPath(root, request.Suite)
		if err != nil {
			return SuiteRunResult{State: Failed, Reason: "the suite must be one regular entry of the open workspace"}
		}
		output, err := runEntryPath(root, request.Output)
		if err != nil {
			return SuiteRunResult{State: Failed, Reason: "the suite output must be one new entry of the open workspace"}
		}
		raw, err := readBoundedEntry(suitePath, suite.MaxBytes)
		if err != nil {
			return SuiteRunResult{State: Failed, Reason: "the suite document could not be read; nothing was sent"}
		}
		if request.Expected != "" && digestOf(raw) != request.Expected {
			return SuiteRunResult{State: Failed, Reason: "the selected suite changed after the preflight; preflight it again before executing"}
		}
		references := ""
		if request.References != "" {
			referencePath, err := runEntryPath(root, request.References)
			if err != nil {
				return SuiteRunResult{State: Failed, Reason: "released references must be one regular entry of the open workspace"}
			}
			references = referencePath
		}
		bounded, cancel := context.WithTimeout(ctx, operationguard.MaxDuration)
		defer cancel()
		var report runqueue.Report
		var runErr error
		if references != "" {
			report, runErr = suite.RunApproved(bounded, suitePath, request.Environment, output, references)
		} else {
			report, runErr = suite.Run(bounded, suitePath, request.Environment, output)
		}
		if runErr != nil && report.Schema == "" {
			return SuiteRunResult{State: Failed, Reason: "the suite could not be prepared; nothing was sent"}
		}
		view := suiteRunReport{Schema: report.Schema, Parallelism: report.Parallelism,
			Executed: report.Executed, StartFailed: report.StartFailed, Refused: report.Refused, Skipped: report.Skipped, Jobs: []suiteRunJob{}}
		for _, job := range report.Jobs {
			row := suiteRunJob{ID: job.ID, Admission: string(job.Admission), Isolation: string(job.Isolation), Reason: job.Reason}
			if job.Run != nil {
				summary := *job.Run
				row.Run = &summary
			}
			view.Jobs = append(view.Jobs, row)
		}
		result := SuiteRunResult{State: Completed, Output: request.Output, Report: &view}
		if runErr != nil {
			result.Reason = "the queue stopped before every job executed; what ran is retained in the suite output"
		}
		return result
	})
}

// RunProgressResult is a read of one run folder as it stands now. It claims no
// operation slot, so the panel watching a run can poll it while the run holds
// the slot, and the counts it reports are the journal's own.
type RunProgressResult struct {
	State    State        `json:"state"`
	Reason   string       `json:"reason,omitzero"`
	Progress *RunProgress `json:"progress,omitzero"`
}

func (r *RunProgressResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunProgress is what one read of a run folder established. Executing is true
// only when this window's run operation is writing that exact folder; a
// journal nobody is writing stays what recovery says it is, never a live
// phase. The counts are the recovery vocabulary: acknowledged, uncertain and
// not-attempted deliveries, not delivered-message counts.
type RunProgress struct {
	Executing    bool                `json:"executing"`
	Phase        string              `json:"phase"`
	Run          *durablerun.Summary `json:"run,omitzero"`
	Acknowledged int                 `json:"acknowledged"`
	Uncertain    int                 `json:"uncertain"`
	NotAttempted int                 `json:"not_attempted"`
	Lease        string              `json:"lease,omitzero"`
}

// DurableRunProgress reads one workspace run entry read-only. It is the same
// recovery read the command line performs, plus the one fact only this
// process knows: whether its own execution is writing the folder right now.
// It never sends, resumes or resets anything.
func (a *App) DurableRunProgress(workspace, entry string) RunProgressResult {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return RunProgressResult{State: declined.state, Reason: declined.reason}
	}
	path, err := runEvidencePath(root, entry)
	if err != nil {
		return RunProgressResult{State: Failed, Reason: "a run is named by one workspace entry, or one job inside a suite execution's runs"}
	}
	if a.executing(path) {
		progress := RunProgress{Executing: true, Phase: "executing"}
		if recovery, err := durablerun.Recover(path); err == nil {
			progress.Run = &recovery.Run
			progress.Acknowledged, progress.Uncertain, progress.NotAttempted = recovery.Acknowledged, recovery.Uncertain, recovery.NotAttempted
			progress.Lease = recovery.Lease
		}
		return RunProgressResult{State: Completed, Progress: &progress}
	}
	recovery, err := durablerun.Recover(path)
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		return RunProgressResult{State: Failed, Reason: "the durable run was evaluated by a version this release cannot read; its evidence has not been changed"}
	}
	if err != nil {
		if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
			return RunProgressResult{State: Empty, Reason: "no run is retained at that entry yet"}
		}
		return RunProgressResult{State: Failed, Reason: "the durable run could not be verified; partial evidence has not been changed"}
	}
	progress := RunProgress{Phase: string(recovery.Run.State), Run: &recovery.Run,
		Acknowledged: recovery.Acknowledged, Uncertain: recovery.Uncertain, NotAttempted: recovery.NotAttempted, Lease: recovery.Lease}
	return RunProgressResult{State: Completed, Progress: &progress}
}

// RunEvidenceRequest opens one retained execution read-only: a result
// directory, a durable run directory, or one job inside a suite execution's
// retained runs. Reveal is the deliberate local reveal of expected and
// observed values; without it the values stay hidden and the view says so.
type RunEvidenceRequest struct {
	Workspace string `json:"workspace"`
	Entry     string `json:"entry"`
	Reveal    bool   `json:"reveal"`
}

// RunEvidenceResult carries one state.
type RunEvidenceResult struct {
	State    State        `json:"state"`
	Reason   string       `json:"reason,omitzero"`
	Evidence *RunEvidence `json:"evidence,omitzero"`
}

func (r *RunEvidenceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunEvidence is one retained execution and everything its own readers
// verified about it. Operation completion, assertion failure and execution
// error are separate facts and stay separate here: RunState is what the
// durable journal retained, Status is the result's verdict where one was
// finalized, and ErrorClass names the error an execution error retained.
type RunEvidence struct {
	Entry             string                 `json:"entry"`
	Durable           bool                   `json:"durable"`
	RunState          string                 `json:"run_state,omitzero"`
	StopReason        string                 `json:"stop_reason,omitzero"`
	DeliveryUncertain bool                   `json:"delivery_uncertain"`
	JournalIncomplete bool                   `json:"journal_incomplete"`
	Recovered         bool                   `json:"recovered"`
	Terminal          bool                   `json:"terminal"`
	Lease             string                 `json:"lease,omitzero"`
	Acknowledged      int                    `json:"acknowledged"`
	Uncertain         int                    `json:"uncertain"`
	NotAttempted      int                    `json:"not_attempted"`
	Status            string                 `json:"status,omitzero"`
	ErrorClass        string                 `json:"error_class,omitzero"`
	Identity          string                 `json:"identity,omitzero"`
	SpecIdentity      string                 `json:"spec_identity,omitzero"`
	SpecName          string                 `json:"spec_name,omitzero"`
	SourceCase        string                 `json:"source_case,omitzero"`
	SourceIdentity    string                 `json:"source_identity,omitzero"`
	Boundary          string                 `json:"boundary,omitzero"`
	StartedAt         string                 `json:"started_at,omitzero"`
	CompletedAt       string                 `json:"completed_at,omitzero"`
	Elapsed           string                 `json:"elapsed,omitzero"`
	Pin               *RunEnginePin          `json:"pin,omitzero"`
	PinRecorded       bool                   `json:"pin_recorded"`
	Planned           int                    `json:"planned"`
	Readable          int                    `json:"readable"`
	Unreadable        int                    `json:"unreadable"`
	InitialRecords    int                    `json:"initial_records,omitzero"`
	FinalRecords      int                    `json:"final_records,omitzero"`
	Messages          []RunMessageEvidence   `json:"messages"`
	Assertions        []RunAssertionEvidence `json:"assertions"`
	Gaps              []string               `json:"gaps"`
	Revealed          bool                   `json:"revealed"`
}

// RunMessageEvidence is one selected message and what the retained evidence
// says about it: where it came from, whether a readable response exists, and
// the retained payload that response lives in.
type RunMessageEvidence struct {
	Source   string `json:"source"`
	Outbound string `json:"outbound"`
	Response string `json:"response,omitzero"`
	Readable bool   `json:"readable"`
}

// RunAssertionEvidence is one assertion of the executed spec and what the run
// decided about it. Expected and Observed are the typed values the spec
// declared and the run saw, present only under the deliberate reveal, and the
// Evidence names the retained artifact and payload the decision was read from.
type RunAssertionEvidence struct {
	ID       string `json:"id"`
	Operator string `json:"operator"`
	Message  string `json:"message,omitzero"`
	Selector string `json:"selector,omitzero"`
	Status   string `json:"status"`
	Expected string `json:"expected,omitzero"`
	Observed string `json:"observed,omitzero"`
	Evidence string `json:"evidence,omitzero"`
}

// OpenRunEvidence reopens one retained execution read-only through the same
// readers the command line verifies one with: the run-result reader for the
// result, the durable recovery read for a job's lifecycle, the engine pin a
// job retained, and the run explainer's own payload readability rules. It
// sends nothing, resets nothing and resumes nothing, and a torn journal or an
// uncertain delivery stays exactly what the journal says it is.
func (a *App) OpenRunEvidence(request RunEvidenceRequest) RunEvidenceResult {
	return run(a, false, false, func(context.Context) RunEvidenceResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return RunEvidenceResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEvidencePath(root, request.Entry)
		if err != nil {
			return RunEvidenceResult{State: Failed, Reason: "a retained execution is named by one workspace entry, or one job inside a suite execution's runs"}
		}
		retained, err := runresult.Open(path)
		if errors.Is(err, engine.ErrUnsupportedVersion) {
			return RunEvidenceResult{State: Failed, Reason: "the retained execution was evaluated by a version this release cannot read; its evidence has not been changed"}
		}
		if err != nil {
			return RunEvidenceResult{State: Failed, Reason: "that entry is not a retained execution this release verifies"}
		}
		evidence := &RunEvidence{Entry: request.Entry, Durable: retained.Durable, Messages: []RunMessageEvidence{}, Assertions: []RunAssertionEvidence{}, Gaps: []string{}, Revealed: request.Reveal}
		if retained.Durable {
			job := retained.Lifecycle
			evidence.RunState = string(job.State)
			evidence.StopReason = string(job.StopReason)
			evidence.DeliveryUncertain = job.DeliveryUncertain
			evidence.JournalIncomplete = job.JournalIncomplete
			evidence.Recovered = job.Recovered
			evidence.Planned = job.Planned
			if recovery, err := durablerun.Recover(path); err == nil {
				evidence.Terminal = recovery.Terminal
				evidence.Lease = recovery.Lease
				evidence.Acknowledged, evidence.Uncertain, evidence.NotAttempted = recovery.Acknowledged, recovery.Uncertain, recovery.NotAttempted
			}
			if pin, err := durablerun.Engine(path); err == nil {
				evidence.Pin = &RunEnginePin{Engine: pin.Engine, Spec: pin.Spec, Profile: pin.Profile}
			}
			evidence.PinRecorded = evidence.Pin != nil
			if job.ResultIdentity == "" {
				evidence.Gaps = append(evidence.Gaps, "no finalized result: assertions and observations are unknown")
			}
		}
		if evidence.Pin == nil && retained.Durable {
			evidence.Gaps = append(evidence.Gaps, "the engine pin this run retained could not be read")
		}
		if retained.Artifact == nil {
			return RunEvidenceResult{State: Completed, Evidence: evidence}
		}
		artifact := retained.Artifact
		evidence.Status = string(artifact.Result.Status)
		evidence.ErrorClass = artifact.Result.ErrorClass
		evidence.Identity = artifact.Identity
		evidence.SpecIdentity = artifact.Result.SpecIdentity
		evidence.Boundary = artifact.Result.ObservationBoundary
		if retained.Spec != nil {
			evidence.SpecName = retained.Spec.Name
			evidence.SourceCase = retained.Spec.Input.Case
			evidence.Planned = len(retained.Spec.Input.Messages)
			// The original case is deliberately not reopened: what the test
			// excluded when it ran cannot be established by today's copy.
			evidence.Gaps = append(evidence.Gaps, "source occurrences this test did not select are unknown; the original case is not reopened")
		} else {
			evidence.Gaps = append(evidence.Gaps, "specification unavailable: selected messages and expectations are unknown")
		}
		evidence.SourceIdentity = artifact.Result.InputBundleIdentity
		if artifact.InitialObservation != nil {
			evidence.InitialRecords = len(artifact.InitialObservation.Records)
		}
		if artifact.FinalObservation != nil {
			evidence.FinalRecords = len(artifact.FinalObservation.Records)
		}
		responses := map[string]string{}
		if retained.Run != nil {
			described, err := runexplain.DescribeRun(retained.Run)
			if err != nil {
				return RunEvidenceResult{State: Failed, Reason: "the retained replay evidence could not be described"}
			}
			evidence.StartedAt = described.StartedAt.UTC().Format(time.RFC3339)
			evidence.CompletedAt = described.CompletedAt.UTC().Format(time.RFC3339)
			evidence.Elapsed = described.Elapsed().String()
			for _, message := range described.Messages {
				readable := message.ReceivedReadable
				if readable {
					evidence.Readable++
					responses[message.Source] = "run/" + message.Received
				} else {
					evidence.Unreadable++
				}
				evidence.Messages = append(evidence.Messages, RunMessageEvidence{Source: message.Source, Outbound: message.Outbound, Response: responses[message.Source], Readable: readable})
			}
		}
		evidence.Unreadable = max(0, evidence.Planned-evidence.Readable)
		if evidence.Planned > evidence.Readable {
			evidence.Gaps = append(evidence.Gaps, "selected messages have no readable retained response")
		}
		if evidence.Boundary == testrunner.LedgerBoundary && artifact.FinalObservation == nil {
			evidence.Gaps = append(evidence.Gaps, "final ledger observation unavailable")
		}
		if evidence.Boundary == testrunner.ACKBoundary {
			evidence.Gaps = append(evidence.Gaps, "downstream application state was not observed by this ACK-only test")
		}
		for _, assertion := range retained.Assertions {
			row := RunAssertionEvidence{ID: assertion.Assertion.ID, Operator: assertion.Assertion.Operator,
				Message: assertion.Assertion.Message, Selector: assertion.Assertion.Selector, Status: assertion.Status}
			if request.Reveal {
				row.Expected = valueText(assertion.Assertion.Expected)
				if assertion.Observed != nil {
					row.Observed = valueText(*assertion.Observed)
				}
			}
			if assertion.Assertion.Operator == "ack_field_equals" {
				row.Evidence = responses[assertion.Assertion.Message]
			} else {
				row.Evidence = "observation.json"
			}
			evidence.Assertions = append(evidence.Assertions, row)
		}
		return RunEvidenceResult{State: Completed, Evidence: evidence}
	})
}

// valueText renders one typed value as compact deterministic JSON for the
// deliberate reveal. It is the same value the result retained, not a summary
// of it.
func valueText(value testrunner.Value) string {
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return ""
	}
	return string(raw)
}

// runEntryPath resolves one entry name of an open workspace to its path,
// refusing anything that is not exactly one name. Existence and type stay the
// caller's own question: a spec is a regular file a reader opens, an output is
// an entry a writer must find absent, and each decides for itself.
func runEntryPath(root, name string) (string, error) {
	if artifactpath.EntryName(name) != nil {
		return "", errors.New("not one workspace entry")
	}
	return filepath.Join(root, name), nil
}

// runEvidencePath resolves a workspace entry naming a retained execution,
// which is either one entry or one job inside a suite execution's retained
// runs directory (`suite-output/runs/job`). Every component is validated as an
// entry name; no arbitrary path is accepted.
func runEvidencePath(root, name string) (string, error) {
	parts := splitEntryPath(name)
	switch len(parts) {
	case 1:
		return runEntryPath(root, name)
	case 3:
		if parts[1] != "runs" {
			return "", errors.New("not a retained execution entry")
		}
		path := root
		for _, part := range parts {
			resolved, err := runEntryPath(path, part)
			if err != nil {
				return "", err
			}
			path = resolved
		}
		return path, nil
	default:
		return "", errors.New("not a retained execution entry")
	}
}

func splitEntryPath(name string) []string {
	if name == "" {
		return nil
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" {
			return nil
		}
	}
	return parts
}

// declaredEntryKind reports what one workspace entry declares, through the
// listing's own classification.
func declaredEntryKind(root, name string) Kind {
	if kind, known := classify(root, name, false); known {
		return kind
	}
	if info, err := os.Lstat(filepath.Join(root, name)); err == nil && info.IsDir() {
		if kind, known := classify(root, name, true); known {
			return kind
		}
	}
	return UnsupportedArtifact
}

// destinationFor validates the fresh output entry an execution requires,
// proposing the next free generated name when none was given. The prefix is
// the generating flow's own vocabulary: durable runs propose job names, and
// the packet panels propose packet names, so a generated name says what
// created it.
func destinationFor(root, requested, prefix string) (RunDestination, refusal) {
	if requested == "" {
		for i := 1; i <= 999; i++ {
			candidate := fmt.Sprintf("%s-%03d", prefix, i)
			if _, err := os.Lstat(filepath.Join(root, candidate)); os.IsNotExist(err) {
				return RunDestination{Name: candidate, Generated: true, Fresh: true}, refusal{}
			}
		}
		return RunDestination{}, refusal{Failed, "the workspace holds more generated run folders than this release proposes"}
	}
	if artifactpath.EntryName(requested) != nil {
		return RunDestination{Name: requested}, refusal{Failed, "the run folder must be one new entry of the open workspace"}
	}
	if _, err := os.Lstat(filepath.Join(root, requested)); err == nil {
		return RunDestination{Name: requested}, refusal{Failed, "the run folder already exists; execution requires a fresh destination"}
	} else if !os.IsNotExist(err) {
		return RunDestination{Name: requested}, probeReadFailure(root)
	}
	return RunDestination{Name: requested, Fresh: true}, refusal{}
}

// readBoundedEntry reads one regular file entry up to limit bytes.
func readBoundedEntry(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, int64(limit)+1))
}

// digestOf is the SHA-256 of one document's exact bytes, spelled the way
// every other identity in this product is.
func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// selected maps the pinned inputs' occurrence mappings to the view a
// preflight shows, in the order a run sends them.
func selected(mappings []replay.Mapping) []RunSelected {
	view := make([]RunSelected, 0, len(mappings))
	for _, mapping := range mappings {
		view = append(view, RunSelected{Source: mapping.SourceOccurrence, Outbound: mapping.OutboundOccurrence})
	}
	return view
}

// targetView is the target a preflight shows: the sealed configuration a run
// would connect to, and whether it names a credential reference at all. A
// credential is a reference to a program that reads a value; the value is
// nowhere in this product to show.
func targetView(target replay.Target) RunTargetView {
	return RunTargetView{
		Name:              target.Name,
		Classification:    string(target.Environment().Classification),
		Address:           target.Address,
		Transport:         target.Transport,
		TestEndpoint:      target.TestEndpoint,
		ApprovedTransport: target.ApprovedTransport,
		ConnectTimeout:    target.ConnectTimeout,
		MessageTimeout:    target.MessageTimeout,
		MaxACKBytes:       target.MaxACKBytes,
		Credential:        target.Credential.Declared(),
	}
}

// enginePin is the pin this build would write beside the plan.
func enginePin(specContract string) RunEnginePin {
	pin := engine.Current(specContract)
	return RunEnginePin{Engine: pin.Engine, Spec: pin.Spec, Profile: pin.Profile}
}
