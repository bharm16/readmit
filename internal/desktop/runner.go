package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/suite"
)

// This file is the customer-runner surface of the window: authoring the exact
// strict documents a hub-enrolled runner and its schedules read, inspecting a
// configured runner's health and retained work, and executing one explicitly
// pinned job through the same admission the command line takes. Every document
// is generated and validated through the shared readers — a runner policy, a
// runner configuration, a job document and a schedule revision are never
// hand-authored JSON here — and every execution is the existing
// internal/customerrunner path with its leases, quotas, duplicate-admission
// refusals and uncertain-delivery rules unchanged. Installation of a service,
// provisioning of external credentials, restarting the customer hub with a new
// schedule policy, and authorizing a third-party CI service remain explicit
// customer-administrator actions outside this window; the panel prepares their
// inputs and says so.

// runnerConfigBytes is the configuration document's own bound.
const runnerConfigBytes = 16384

// RunnerDocumentResult carries one generated operator document: the canonical
// text, where a save wrote it, and its digest. No result ever contains a
// credential value; credential members are the references ADR-0006 registers.
type RunnerDocumentResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Document string `json:"document,omitzero"`
	Output   string `json:"output,omitzero"`
	SHA256   string `json:"sha256,omitzero"`
}

func (r *RunnerDocumentResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunnerReferenceInput is one credential reference as the structured form
// holds it: the absolute path of the program that reads the value back and
// the arguments that select it. The value itself is never an input here.
type RunnerReferenceInput struct {
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
}

// RunnerConfigRequest names every member a readmit-runner/v1 document
// requires. All are required; there are no defaults to invent.
type RunnerConfigRequest struct {
	Hub          string               `json:"hub"`
	Project      string               `json:"project"`
	Environment  string               `json:"environment"`
	Root         string               `json:"root"`
	CA           string               `json:"ca"`
	Certificate  string               `json:"certificate"`
	Key          RunnerReferenceInput `json:"key"`
	Token        RunnerReferenceInput `json:"token"`
	UpdateKey    string               `json:"update_key"`
	UpdateEngine string               `json:"update_engine"`
	Output       string               `json:"output"`
}

// PreviewRunnerConfig validates the structured form through the runner
// configuration's own strict reader and returns the canonical document
// without writing anything.
func (a *App) PreviewRunnerConfig(request RunnerConfigRequest) RunnerDocumentResult {
	return run(a, false, false, func(context.Context) RunnerDocumentResult {
		config, declined := runnerConfigFrom(request)
		if declined.reason != "" {
			return RunnerDocumentResult{State: declined.state, Reason: declined.reason}
		}
		return runnerDocumentResult(config)
	})
}

// SaveRunnerConfig writes one validated readmit-runner/v1 document as a new
// private file. An existing destination is refused rather than replaced: the
// configuration on the runner host is an operator-controlled file, and a
// revision is installed deliberately by the administrator.
func (a *App) SaveRunnerConfig(request RunnerConfigRequest) RunnerDocumentResult {
	return run(a, false, true, func(context.Context) RunnerDocumentResult {
		config, declined := runnerConfigFrom(request)
		if declined.reason != "" {
			return RunnerDocumentResult{State: declined.state, Reason: declined.reason}
		}
		result := runnerDocumentResult(config)
		if result.State != Completed {
			return result
		}
		if declined := writePrivateDocument(request.Output, []byte(result.Document)); declined.reason != "" {
			return RunnerDocumentResult{State: declined.state, Reason: declined.reason}
		}
		result.Output = request.Output
		return result
	})
}

func runnerConfigFrom(request RunnerConfigRequest) (customerrunner.Config, refusal) {
	config := customerrunner.Config{
		Schema: "readmit-runner/v1", Hub: request.Hub, Project: request.Project,
		Environment: request.Environment, Root: request.Root, CA: request.CA,
		Certificate:  request.Certificate,
		Key:          customerrunner.Reference{Command: request.Key.Command, Arguments: request.Key.Arguments},
		Token:        customerrunner.Reference{Command: request.Token.Command, Arguments: request.Token.Arguments},
		UpdateKey:    request.UpdateKey,
		UpdateEngine: request.UpdateEngine,
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return config, refusal{Failed, "the runner configuration could not be encoded"}
	}
	if _, err := customerrunner.DecodeConfig(raw); err != nil {
		return config, refusal{Failed, "the runner configuration is not complete; every member is required, the hub must be an HTTPS origin, and both credential references name an absolute program"}
	}
	return config, refusal{}
}

// runnerDocumentResult canonicalizes one validated document value.
func runnerDocumentResult(value any) RunnerDocumentResult {
	canonical, err := canonicalDocument(value)
	if err != nil {
		return RunnerDocumentResult{State: Failed, Reason: "the runner document could not be canonicalized"}
	}
	return RunnerDocumentResult{State: Completed, Document: string(canonical), SHA256: digestOf(canonical)}
}

// RunnerGrantRequest adds or replaces one grant of a readmit-runner-policy/v1
// document: the hub authority that admits this runner's project and
// environment for one subject at one exact engine. An empty Engine names the
// running build's pin, which is what an enrollment requests; any other value
// must match a build the customer deliberately approved.
type RunnerGrantRequest struct {
	Policy      string `json:"policy"`
	Project     string `json:"project"`
	Subject     string `json:"subject"`
	Environment string `json:"environment"`
	Engine      string `json:"engine"`
	Spec        string `json:"spec"`
	Profile     string `json:"profile"`
	MaxSeconds  int    `json:"max_seconds"`
	MaxJobs     int    `json:"max_jobs"`
	Output      string `json:"output"`
}

// SaveRunnerGrant reads the operator's existing policy when one is named,
// replaces the grant for this project and environment pair, validates the
// whole document through the admission protocol's own reader, and writes the
// revision to a new file. Installing it on the hub and reloading its tokens
// remain the administrator's actions.
func (a *App) SaveRunnerGrant(request RunnerGrantRequest) RunnerDocumentResult {
	return run(a, false, true, func(context.Context) RunnerDocumentResult {
		if !runnerprotocol.ID(request.Project) || !runnerprotocol.ID(request.Environment) || strings.TrimSpace(request.Subject) == "" || len(request.Subject) > 256 {
			return RunnerDocumentResult{State: Failed, Reason: "a grant names a project and environment the admission protocol accepts and one subject"}
		}
		engineName := request.Engine
		if engineName == "" {
			engineName = engine.Current("readmit-test/v1").Engine
		}
		spec := request.Spec
		if spec == "" {
			spec = "readmit-test/v1"
		}
		profile := request.Profile
		if profile == "" {
			profile = "readmit-siu-v1"
		}
		grant := runnerprotocol.Grant{Project: request.Project, Subject: request.Subject, Environment: request.Environment, Engine: engineName, Spec: spec, Profile: profile, MaxSeconds: request.MaxSeconds, MaxJobs: request.MaxJobs}
		policy := runnerprotocol.Policy{Schema: "readmit-runner-policy/v1"}
		if request.Policy != "" {
			raw, declined := readPrivateFile(request.Policy, 1<<20)
			if declined.reason != "" {
				return RunnerDocumentResult{State: declined.state, Reason: declined.reason}
			}
			parsed, err := runnerprotocol.DecodePolicy(raw)
			if err != nil {
				return RunnerDocumentResult{State: Failed, Reason: "the existing runner policy could not be read through its own strict reader"}
			}
			policy = parsed
		}
		replaced := false
		for i, existing := range policy.Runners {
			if existing.Project == grant.Project && existing.Environment == grant.Environment {
				policy.Runners[i] = grant
				replaced = true
				break
			}
		}
		if !replaced {
			policy.Runners = append(policy.Runners, grant)
		}
		raw, err := json.Marshal(policy)
		if err != nil {
			return RunnerDocumentResult{State: Failed, Reason: "the runner policy could not be encoded"}
		}
		if _, err := runnerprotocol.DecodePolicy(raw); err != nil {
			return RunnerDocumentResult{State: Failed, Reason: "the admission protocol refuses this grant; check the engine identity, the seconds and jobs bounds, and the exact spec and profile pins"}
		}
		result := runnerDocumentResult(policy)
		if result.State != Completed {
			return result
		}
		if declined := writePrivateDocument(request.Output, []byte(result.Document)); declined.reason != "" {
			return RunnerDocumentResult{State: declined.state, Reason: declined.reason}
		}
		result.Output = request.Output
		return result
	})
}

// RunnerJobRequest generates one readmit-runner-job/v1 document: the job id
// the admission protocol accepts and the absolute spec path it runs.
type RunnerJobRequest struct {
	ID     string `json:"id"`
	Spec   string `json:"spec"`
	Output string `json:"output"`
}

// SaveRunnerJob writes one validated job document as a new private file. An
// inbox document is consumed by the runner service on the host; the job id is
// never reused after a retained execution, and the panel says so rather than
// generating a new attempt automatically.
func (a *App) SaveRunnerJob(request RunnerJobRequest) RunnerDocumentResult {
	return run(a, false, true, func(context.Context) RunnerDocumentResult {
		job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: request.ID, Spec: request.Spec}
		if customerrunner.ValidateJob(job) != nil {
			return RunnerDocumentResult{State: Failed, Reason: "a job id is 1-64 lowercase letters, digits or hyphens starting with a letter or digit, and the spec is one absolute path"}
		}
		result := runnerDocumentResult(job)
		if result.State != Completed {
			return result
		}
		if declined := writePrivateDocument(request.Output, []byte(result.Document)); declined.reason != "" {
			return RunnerDocumentResult{State: declined.state, Reason: declined.reason}
		}
		result.Output = request.Output
		return result
	})
}

// RunnerConfigView is what the panel displays about one installed// RunnerConfigView is what the panel displays about one installed
// configuration. Credential members are shown as the references they are;
// their values stay in the customer's store.
type RunnerConfigView struct {
	Hub          string               `json:"hub"`
	Project      string               `json:"project"`
	Environment  string               `json:"environment"`
	Root         string               `json:"root"`
	UpdateEngine string               `json:"update_engine"`
	Key          RunnerReferenceInput `json:"key"`
	Token        RunnerReferenceInput `json:"token"`
}

// RunnerJobState is one retained job of the runner root, read through the
// durable-run recovery reader the command line uses.
type RunnerJobState struct {
	ID                string `json:"id"`
	State             string `json:"state,omitzero"`
	StopReason        string `json:"stop_reason,omitzero"`
	DeliveryUncertain bool   `json:"delivery_uncertain"`
	JournalIncomplete bool   `json:"journal_incomplete"`
	Reason            string `json:"reason,omitzero"`
}

// RunnerInspectResult is one read of a configured runner as it stands now:
// the configuration, this build's engine pin, the local health snapshot, the
// retained jobs and the queued inbox documents. Reading reconnects to the
// current state and offers no action by itself.
type RunnerInspectResult struct {
	State      State                  `json:"state"`
	Reason     string                 `json:"reason,omitzero"`
	Config     *RunnerConfigView      `json:"config,omitzero"`
	Engine     string                 `json:"engine,omitzero"`
	Health     *customerrunner.Status `json:"health,omitzero"`
	HealthNote string                 `json:"health_note,omitzero"`
	Jobs       []RunnerJobState       `json:"jobs,omitzero"`
	Queued     []string               `json:"queued,omitzero"`
}

func (r *RunnerInspectResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReadRunnerConfig reads one installed runner configuration and the state of
// its runner root as this machine can see it. The root is usually on the
// runner host; when it is not, the configuration still reads and the health
// section says so instead of pretending to see the runner.
func (a *App) ReadRunnerConfig(configPath string) RunnerInspectResult {
	return run(a, false, false, func(context.Context) RunnerInspectResult {
		config, declined := readRunnerConfig(configPath)
		if declined.reason != "" {
			return RunnerInspectResult{State: declined.state, Reason: declined.reason}
		}
		result := RunnerInspectResult{
			State: Completed,
			Config: &RunnerConfigView{
				Hub: config.Hub, Project: config.Project, Environment: config.Environment,
				Root: config.Root, UpdateEngine: config.UpdateEngine,
				Key:   RunnerReferenceInput{Command: config.Key.Command, Arguments: config.Key.Arguments},
				Token: RunnerReferenceInput{Command: config.Token.Command, Arguments: config.Token.Arguments},
			},
			Engine: engine.Current("readmit-test/v1").Engine,
			Jobs:   []RunnerJobState{},
			Queued: []string{},
		}
		if _, err := os.Lstat(config.Root); err != nil {
			result.HealthNote = "the runner root is not visible on this machine; health and retained jobs live on the runner host"
			return result
		}
		health, err := customerrunner.Health(config.Root)
		if err != nil {
			result.HealthNote = "the runner root is not a private directory this account can read"
			return result
		}
		result.Health = &health
		result.Jobs = retainedRunnerJobs(config.Root)
		return result
	})
}

// retainedRunnerJobs reads every retained job's durable summary read-only.
func retainedRunnerJobs(root string) []RunnerJobState {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []RunnerJobState{}
	}
	jobs := []RunnerJobState{}
	for _, entry := range entries {
		if !entry.IsDir() || !runnerprotocol.ID(entry.Name()) || entry.Name() == ".active" {
			continue
		}
		state := RunnerJobState{ID: entry.Name()}
		recovery, err := durablerun.Recover(filepath.Join(root, entry.Name(), "run"))
		if err != nil {
			state.Reason = "the retained run could not be read; recovery requires the runner host"
		} else {
			state.State = string(recovery.Run.State)
			state.StopReason = string(recovery.Run.StopReason)
			state.DeliveryUncertain = recovery.Run.DeliveryUncertain
			state.JournalIncomplete = recovery.Run.JournalIncomplete
		}
		jobs = append(jobs, state)
	}
	return jobs
}

// RunnerEnrollmentResult carries one enrollment probe: the lease the hub
// issued and the capacity it grants. A refusal names the hub's own reason —
// a version or environment disagreement, a leased environment, exhausted
// capacity — because admission is negotiated by explicit agreement, never a
// nearest version.
type RunnerEnrollmentResult struct {
	State       State  `json:"state"`
	Reason      string `json:"reason,omitzero"`
	Project     string `json:"project,omitzero"`
	Environment string `json:"environment,omitzero"`
	Engine      string `json:"engine,omitzero"`
	ExpiresAt   string `json:"expires_at,omitzero"`
	MaxSeconds  int    `json:"max_seconds,omitzero"`
	MaxJobs     int    `json:"max_jobs,omitzero"`
}

func (r *RunnerEnrollmentResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// EnrollRunner proves current admission exactly as `readmit runner enroll`
// does: one certificate-bound probe that reserves the environment for at most
// ten seconds and saves no credential or registration state.
func (a *App) EnrollRunner(configPath string) RunnerEnrollmentResult {
	return runNamed[RunnerEnrollmentResult, *RunnerEnrollmentResult](a, profiles["EnrollRunner"], func(ctx context.Context) RunnerEnrollmentResult {
		config, declined := readRunnerConfig(configPath)
		if declined.reason != "" {
			return RunnerEnrollmentResult{State: declined.state, Reason: declined.reason}
		}
		if declined := a.runnerAdministrationGate(ctx, config.Project, "enrollment"); declined.reason != "" {
			return RunnerEnrollmentResult{State: declined.state, Reason: declined.reason}
		}
		result := RunnerEnrollmentResult{State: Failed, Project: config.Project, Environment: config.Environment, Engine: engine.Current("readmit-test/v1").Engine}
		lease, err := customerrunner.Enroll(ctx, config)
		if err != nil {
			result.Reason = err.Error()
			if errors.Is(err, customerrunner.ErrHubRefused) {
				result.State = PermissionDenied
			}
			return result
		}
		result.State = Completed
		result.ExpiresAt = lease.Expires.UTC().Format(time.RFC3339)
		result.MaxSeconds = lease.MaxSeconds
		result.MaxJobs = lease.MaxJobs
		return result
	})
}

// RunnerJobPreviewResult is the pin an execution would bind to: the prepared
// inputs' identity and the environment binding, established without sending
// anything. A job whose prepared inputs bind another environment is reported
// here before any admission is asked.
type RunnerJobPreviewResult struct {
	State         State  `json:"state"`
	Reason        string `json:"reason,omitzero"`
	JobID         string `json:"job_id,omitzero"`
	Spec          string `json:"spec,omitzero"`
	InputIdentity string `json:"input_identity,omitzero"`
	Environment   string `json:"environment,omitzero"`
}

func (r *RunnerJobPreviewResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// InspectRunnerJob reads one job document and prepares its spec exactly as
// execution would, without contacting the hub or opening a connection. When
// the runner root is on this machine and already holds the job id, the id is
// occupied: the runner reserves it permanently, whatever became of the job,
// so the preflight names the id rather than a pin that can never run.
func (a *App) InspectRunnerJob(configPath, jobPath string) RunnerJobPreviewResult {
	return run(a, false, false, func(context.Context) RunnerJobPreviewResult {
		config, declined := readRunnerConfig(configPath)
		if declined.reason != "" {
			return RunnerJobPreviewResult{State: declined.state, Reason: declined.reason}
		}
		job, declined := readRunnerJob(jobPath)
		if declined.reason != "" {
			return RunnerJobPreviewResult{State: declined.state, Reason: declined.reason}
		}
		result := RunnerJobPreviewResult{State: Completed, JobID: job.ID, Spec: job.Spec}
		prepared, err := durablerun.Prepare(job.Spec)
		if err != nil {
			return RunnerJobPreviewResult{State: Failed, Reason: "the job's spec could not be prepared; nothing was read from the hub and nothing was sent"}
		}
		identity, err := prepared.InputIdentity()
		if err != nil {
			return RunnerJobPreviewResult{State: Failed, Reason: "the prepared inputs have no identity; nothing was sent"}
		}
		result.InputIdentity = identity
		for _, resource := range prepared.Resources() {
			if resource.Kind == durablerun.EnvironmentResource {
				result.Environment = resource.Name
				if resource.Name != config.Environment {
					return RunnerJobPreviewResult{State: Failed, JobID: job.ID, Spec: job.Spec, InputIdentity: identity,
						Reason: fmt.Sprintf("the prepared inputs bind environment %s; this runner is configured for %s", resource.Name, config.Environment)}
				}
			}
		}
		// Only a root this machine can read answers here; one it cannot read
		// decides nothing, and the runner's own claim at execution stays the
		// rule either way.
		if retained, err := customerrunner.Retained(config.Root, job.ID); err == nil && retained {
			return RunnerJobPreviewResult{State: Failed, JobID: job.ID, Spec: job.Spec,
				Reason: fmt.Sprintf("job id %s is already retained in this runner's root and never runs again; read its recovery, and save a new job document with a new job id once receiver state is established", job.ID)}
		}
		return result
	})
}

// RunnerExecuteRequest is one deliberate execution: the configured runner to
// admit through, the job document to run, and the input identity the
// preflight fixed. Execution refuses a changed pin before anything runs.
type RunnerExecuteRequest struct {
	ConfigPath string `json:"config_path"`
	JobPath    string `json:"job_path"`
	Expected   string `json:"expected_identity"`
}

// RunnerExecutionResult carries the durable summary exactly as the runner
// retains it. Delivery that stayed uncertain is reported as uncertain and
// nothing here offers to resend it: a lost response is never a safe replay,
// and a new execution is a new job id the operator chooses after reading
// receiver state.
type RunnerExecutionResult struct {
	State   State               `json:"state"`
	Reason  string              `json:"reason,omitzero"`
	JobID   string              `json:"job_id,omitzero"`
	Output  string              `json:"output,omitzero"`
	Summary *durablerun.Summary `json:"summary,omitzero"`
}

func (r *RunnerExecutionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// runnerOperation names a runner execution while it holds the slot, so the
// runner panel's cancel control stops exactly the execution it started.
const runnerOperation = "runner"

// ExecuteRunnerJob runs one job through the same enrolled path as the
// command line: the runner admits the job as its own execution, then applies
// its own lease, duplicate-admission and state-isolation rules, which this
// facade does not widen. The window also admits the author first, which
// `readmit runner execute` does not (profiles). Cancel stops future sends and
// retains any uncertain delivery exactly as `run start` does.
func (a *App) ExecuteRunnerJob(request RunnerExecuteRequest) RunnerExecutionResult {
	return runNamed[RunnerExecutionResult, *RunnerExecutionResult](a, profiles["ExecuteRunnerJob"], func(ctx context.Context) (out RunnerExecutionResult) {
		// The explicit execution approval is the runner's own admission of the
		// job, asked of the selected operation policy exactly once through the
		// profile's each-job execution, as the command line asks it. This seam
		// is also where a team authority (the administration surface) settles
		// what may execute.
		config, declined := readRunnerConfig(request.ConfigPath)
		if declined.reason != "" {
			return RunnerExecutionResult{State: declined.state, Reason: declined.reason}
		}
		if declined := a.runnerAdministrationGate(ctx, config.Project, "execution"); declined.reason != "" {
			return RunnerExecutionResult{State: declined.state, Reason: declined.reason}
		}
		job, declined := readRunnerJob(request.JobPath)
		if declined.reason != "" {
			return RunnerExecutionResult{State: declined.state, Reason: declined.reason}
		}
		out.JobID = job.ID
		if request.Expected != "" {
			prepared, err := durablerun.Prepare(job.Spec)
			if err != nil {
				return RunnerExecutionResult{State: Failed, JobID: job.ID, Reason: "the job's spec could not be prepared; nothing was admitted and nothing was sent"}
			}
			identity, err := prepared.InputIdentity()
			if err != nil || identity != request.Expected {
				return RunnerExecutionResult{State: Failed, JobID: job.ID, Reason: "the job changed after the preflight; preflight it again before executing"}
			}
		}
		var summary durablerun.Summary
		var err error
		if request.Expected != "" {
			summary, err = customerrunner.RunPinned(ctx, config, job, request.Expected)
		} else {
			summary, err = customerrunner.Run(ctx, config, job)
		}
		out.Output = filepath.Join(config.Root, job.ID, "run")
		if summary.Schema != "" {
			retained := summary
			out.Summary = &retained
		}
		if err != nil {
			out.State = Failed
			out.Reason = err.Error()
			// A refusal before any run existed is an admission answer, not a
			// broken execution: the authority declined this instance. A
			// cancellation while the job's admission waited refused nothing.
			var admission *operationguard.Declined
			switch {
			case errors.As(err, &admission) && admission.Cancelled:
				out.State, out.Reason = cancelledRefusal.state, cancelledRefusal.reason
			case !errors.Is(err, customerrunner.ErrRefused) && summary.Schema == "":
				out.State = PermissionDenied
			}
			return out
		}
		out.State = Completed
		return out
	})
}

// RunnerRecoveryResult is the recovery read of one retained job. It reports
// the durable vocabulary — acknowledged, uncertain and not-attempted
// deliveries — and never sends, resumes or resets anything.
type RunnerRecoveryResult struct {
	State        State               `json:"state"`
	Reason       string              `json:"reason,omitzero"`
	JobID        string              `json:"job_id,omitzero"`
	Acknowledged int                 `json:"acknowledged"`
	Uncertain    int                 `json:"uncertain"`
	NotAttempted int                 `json:"not_attempted"`
	Summary      *durablerun.Summary `json:"summary,omitzero"`
}

func (r *RunnerRecoveryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReadRunnerRecovery reads one retained job's durable run as it stands now,
// exactly as `readmit run status --recovery` does. Recovery requires the
// runner host's files and never contacts the hub.
func (a *App) ReadRunnerRecovery(configPath, jobID string) RunnerRecoveryResult {
	return run(a, false, false, func(context.Context) RunnerRecoveryResult {
		config, declined := readRunnerConfig(configPath)
		if declined.reason != "" {
			return RunnerRecoveryResult{State: declined.state, Reason: declined.reason}
		}
		if !runnerprotocol.ID(jobID) {
			return RunnerRecoveryResult{State: Failed, Reason: "a retained job is named by its configured job id"}
		}
		recovery, err := durablerun.Recover(filepath.Join(config.Root, jobID, "run"))
		if err != nil {
			return RunnerRecoveryResult{State: Failed, JobID: jobID, Reason: "the retained run could not be read on this machine"}
		}
		summary := recovery.Run
		return RunnerRecoveryResult{State: Completed, JobID: jobID,
			Acknowledged: recovery.Acknowledged, Uncertain: recovery.Uncertain, NotAttempted: recovery.NotAttempted,
			Summary: &summary}
	})
}

// RunnerUpdateResult reports one explicit update verification. Engine is the
// build the configuration approves, which a verified manifest names exactly.
type RunnerUpdateResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Engine string `json:"engine,omitzero"`
}

func (r *RunnerUpdateResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// VerifyRunnerUpdate verifies one staged update candidate against the
// configuration's pinned customer deployment key, exactly as
// `readmit runner verify-update` does. It never executes the candidate;
// installing it remains the administrator's action.
func (a *App) VerifyRunnerUpdate(configPath, manifest, binary string) RunnerUpdateResult {
	return run(a, false, false, func(context.Context) RunnerUpdateResult {
		config, declined := readRunnerConfig(configPath)
		if declined.reason != "" {
			return RunnerUpdateResult{State: declined.state, Reason: declined.reason}
		}
		if err := customerrunner.VerifyUpdate(config, manifest, binary); err != nil {
			return RunnerUpdateResult{State: Failed, Reason: "the staged candidate is not the approved update; " + err.Error()}
		}
		return RunnerUpdateResult{State: Completed, Engine: config.UpdateEngine}
	})
}

// readRunnerConfig reads an installed configuration through its own strict
// reader. The runner root may be on another host; only bytes are required.
func readRunnerConfig(configPath string) (customerrunner.Config, refusal) {
	raw, declined := readPrivateFile(configPath, runnerConfigBytes)
	if declined.reason != "" {
		return customerrunner.Config{}, declined
	}
	config, err := customerrunner.DecodeConfig(raw)
	if err != nil {
		return config, refusal{Failed, "the runner configuration could not be read through its own strict reader"}
	}
	return config, refusal{}
}

// readRunnerJob reads one job document the way `runner execute` does.
func readRunnerJob(jobPath string) (customerrunner.Job, refusal) {
	raw, declined := readPrivateFile(jobPath, 16384)
	if declined.reason != "" {
		return customerrunner.Job{}, declined
	}
	var job customerrunner.Job
	if json.Unmarshal(raw, &job, json.RejectUnknownMembers(true)) != nil || customerrunner.ValidateJob(job) != nil {
		return job, refusal{Failed, "the job document is not a valid readmit-runner-job/v1 document"}
	}
	return job, refusal{}
}

// runnerAdministrationGate consults the window's authenticated hub session and
// the project's administration log (#262's authority) before a runner
// lifecycle action. It is an additional gate, never a substitute for the
// backend: the hub re-checks the access policy's roles — enrollment and
// execution through roleAllows — and the same removal log at every admission,
// and without a window session the runner's own certificate-bound credential
// path applies unchanged. A session that cannot establish the administration
// state refuses new runner work: an unknown authority is not a pass.
func (a *App) runnerAdministrationGate(ctx context.Context, project, action string) refusal {
	client, session, err := a.hub.SignedIn()
	if err != nil {
		return refusal{}
	}
	if !session.Allows(action) {
		return refusal{PermissionDenied, "the signed-in hub identity's granted scopes do not include " + action + "; the hub would refuse this admission"}
	}
	history, err := client.GetLifecycle(ctx, project)
	if err != nil {
		return refusal{PermissionDenied, "the project administration log could not be read; resolve the hub session before new runner work"}
	}
	if history.Removed(session.Issuer, session.Subject) {
		return refusal{PermissionDenied, "the signed-in hub identity has been removed from this project's administration log; runner work is refused"}
	}
	return refusal{}
}

// readPrivateFile applies the one private-file rule the runner and hub
// configurations are held to: a regular, private, bounded file, read through
// the shared document store. It mirrors the runner's own rule: Windows ACLs
// are the administrator's boundary and mode bits do not represent them, so
// only elsewhere must the file be owner-only.
func readPrivateFile(path string, limit int64) ([]byte, refusal) {
	raw, err := artifactdir.Document{
		MaxBytes:  int(limit),
		OwnerOnly: runtime.GOOS != "windows",
		Refusals:  artifactdir.DocumentRefusals{Irregular: errPrivateIrregular, Open: errPrivateUnopened, Read: errPrivateIrregular},
	}.Read(path)
	switch {
	case err == nil:
		return raw, refusal{}
	case errors.Is(err, errPrivateUnopened) || errors.Is(err, fs.ErrPermission):
		return nil, refusal{PermissionDenied, "this account cannot read the selected file"}
	}
	return nil, refusal{Failed, "the selected file must be a private regular file"}
}

// errPrivateIrregular and errPrivateUnopened are what reading a private file
// found: one that is not a private regular file within its bound, and one this
// account could not open.
var (
	errPrivateIrregular = errors.New("the selected file must be a private regular file")
	errPrivateUnopened  = errors.New("this account cannot read the selected file")
)

// writePrivateDocument writes one validated operator document as a new
// private file at an absolute path. An existing destination is refused rather
// than replaced, and the write is synced before it is reported.
func writePrivateDocument(path string, data []byte) refusal {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return refusal{Failed, "choose a cleaned absolute destination for the document"}
	}
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return refusal{Failed, "cannot write the document here; choose a new destination outside evidence"}
	}
	parent, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return refusal{Failed, "the destination directory must already exist"}
	}
	parent.Close()
	err = privateDocument.Create(destination, data)
	switch {
	case err == nil:
		return refusal{}
	case errors.Is(err, errPrivateUncreated) && errors.Is(err, fs.ErrExist):
		return refusal{Failed, "an existing document is never replaced; choose a new destination"}
	case errors.Is(err, errPrivateUncreated) && errors.Is(err, fs.ErrPermission):
		return refusal{PermissionDenied, "this account cannot write the destination"}
	}
	return refusal{Failed, err.Error()}
}

// errPrivateUncreated is a private document that could not be created.
var errPrivateUncreated = errors.New("cannot create the destination file")

// privateDocument is how a private runner document is created, through the
// shared document store.
var privateDocument = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Create: errPrivateUncreated,
		Write:  errors.New("cannot write the destination file"),
	},
}

// --- Recurring schedules -----------------------------------------------------

// ScheduleEntryInput is one entry of a readmit-hub-schedules/v1 revision as
// the structured form holds it. The input pin is the prepared inputs'
// identity the hub will refuse execution without; the route is an approved
// HTTPS origin an administrator owns.
type ScheduleEntryInput struct {
	ID            string `json:"id"`
	Zone          string `json:"zone"`
	At            string `json:"at"`
	WindowSeconds int    `json:"window_seconds"`
	Runner        string `json:"runner_config"`
	Spec          string `json:"spec"`
	Input         string `json:"input_sha256"`
	Route         string `json:"route"`
	Approved      bool   `json:"approved"`
}

// SchedulePolicyRequest is one revision of the schedule policy. Concurrency
// is not an input: the contract defines serial-skip-missed and nothing else.
// Anchor is display-only: the day the preview counts occurrences from, which
// defaults to each entry's current local day; it never changes what a save writes.
type SchedulePolicyRequest struct {
	Output  string               `json:"output"`
	Anchor  string               `json:"anchor"`
	Entries []ScheduleEntryInput `json:"entries"`
}

// ScheduleOccurrence is one computed occurrence of one entry, computed by the
// hub's own occurrence function. A local minute that does not exist is
// reported as the contract's dst-gap and is never shifted.
type ScheduleOccurrence struct {
	Day   string `json:"day"`
	UTC   string `json:"utc,omitzero"`
	State string `json:"state"`
}

// ScheduleEntryView is one entry as a preview shows it: the declared pins and
// limits, the hub's fixed alert for an approved route, and the input identity
// recomputed from the spec when this machine can read it.
type ScheduleEntryView struct {
	Entry        ScheduleEntryInput   `json:"entry"`
	Identity     string               `json:"identity,omitzero"`
	Occurrences  []ScheduleOccurrence `json:"occurrences,omitzero"`
	PinState     string               `json:"pin_state"`
	Notification string               `json:"notification,omitzero"`
}

// SchedulePreviewResult is one validated revision as it would take effect:
// the policy identity the hub binds its journal to, each entry's next
// occurrences, and the missed-run and overlap behavior the backend applies.
type SchedulePreviewResult struct {
	State       State               `json:"state"`
	Reason      string              `json:"reason,omitzero"`
	Identity    string              `json:"identity,omitzero"`
	Concurrency string              `json:"concurrency,omitzero"`
	Entries     []ScheduleEntryView `json:"entries,omitzero"`
	Alert       string              `json:"alert,omitzero"`
	AlertStates []string            `json:"alert_states,omitzero"`
}

func (r *SchedulePreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// schedulePolicyFrom validates the structured entries through the schedule
// contract's own strict reader, exactly as the hub service will.
func schedulePolicyFrom(request SchedulePolicyRequest) (runnerprotocol.SchedulePolicy, refusal) {
	policy := runnerprotocol.SchedulePolicy{Schema: "readmit-hub-schedules/v1", Concurrency: runnerprotocol.ScheduleConcurrency}
	for _, entry := range request.Entries {
		policy.Schedules = append(policy.Schedules, runnerprotocol.Schedule{
			ID: entry.ID, Zone: entry.Zone, At: entry.At, WindowSeconds: entry.WindowSeconds,
			Runner: entry.Runner, Spec: entry.Spec, Input: entry.Input, Route: entry.Route, Approved: entry.Approved,
		})
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return policy, refusal{Failed, "the schedule policy could not be encoded"}
	}
	parsed, err := runnerprotocol.DecodeSchedules(raw)
	if err != nil {
		return parsed, refusal{Failed, "the schedule contract refuses this revision; every member is required, the concurrency is serial-skip-missed, times are HH:MM in a named zone, and an approved entry names an HTTPS route"}
	}
	return parsed, refusal{}
}

// PreviewSchedulePolicy validates the entries and shows what the hub would
// do: the policy identity, each entry's next daily occurrences in its own
// zone, the window's missed-run marking, and the exact notification body an
// approved schedule may emit. Nothing is written and nothing is sent.
func (a *App) PreviewSchedulePolicy(request SchedulePolicyRequest) SchedulePreviewResult {
	return a.previewSchedulePolicy(request, time.Now())
}

func (a *App) previewSchedulePolicy(request SchedulePolicyRequest, now time.Time) SchedulePreviewResult {
	return run(a, false, false, func(context.Context) SchedulePreviewResult {
		return schedulePreview(request, now)
	})
}

// schedulePreview is the computation behind both the preview operation and a
// save; it holds no operation slot of its own, so a save composes it once.
// now is the one instant the whole preview is taken at: each entry's default
// local day and every occurrence's missed marking read it.
func schedulePreview(request SchedulePolicyRequest, now time.Time) SchedulePreviewResult {
	policy, declined := schedulePolicyFrom(request)
	if declined.reason != "" {
		return SchedulePreviewResult{State: declined.state, Reason: declined.reason}
	}
	result := SchedulePreviewResult{
		State:       Completed,
		Identity:    runnerprotocol.SchedulePolicyIdentity(policy),
		Concurrency: policy.Concurrency,
		Entries:     []ScheduleEntryView{},
		Alert:       string(runnerprotocol.ScheduleAlert("failed")),
		AlertStates: runnerprotocol.ScheduleAlertStates(),
	}
	var anchor time.Time
	if request.Anchor != "" {
		anchored, err := time.Parse("2006-01-02", request.Anchor)
		if err != nil {
			return SchedulePreviewResult{State: Failed, Reason: "the preview anchor is one day in 2006-01-02 form"}
		}
		anchor = anchored
	}
	for i, entry := range request.Entries {
		startDay := anchor
		if request.Anchor == "" {
			// DecodeSchedules has already validated this zone. Use the same
			// local civil day from which the hub's scheduler starts its scan.
			loc, _ := time.LoadLocation(policy.Schedules[i].Zone)
			year, month, day := now.In(loc).Date()
			startDay = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		}
		view := ScheduleEntryView{Entry: entry, PinState: "unreadable"}
		if prepared, err := durablerun.Prepare(entry.Spec); err == nil {
			if pin, err := prepared.InputIdentity(); err == nil {
				view.Identity = pin
				if pin == entry.Input {
					view.PinState = "computed"
				} else {
					view.PinState = "mismatch"
				}
			}
		}
		if entry.Approved {
			view.Notification = "approved: the hub emits only the fixed alert body shown below"
		} else {
			view.Notification = "disabled: notifications require an approved route"
		}
		for dayOffset := 0; dayOffset < 3; dayOffset++ {
			day := startDay.AddDate(0, 0, dayOffset).Format("2006-01-02")
			occurrence := ScheduleOccurrence{Day: day, State: "scheduled"}
			if due, exists := runnerprotocol.DailyOccurrence(day, policy.Schedules[i].At, policy.Schedules[i].Zone); !exists {
				occurrence.State = "dst-gap"
			} else {
				occurrence.UTC = due.UTC().Format(time.RFC3339)
				if now.Sub(due) > time.Duration(policy.Schedules[i].WindowSeconds)*time.Second {
					occurrence.State = "missed"
				}
			}
			view.Occurrences = append(view.Occurrences, occurrence)
		}
		result.Entries = append(result.Entries, view)
	}
	return result
}

// SaveSchedulePolicy writes one validated readmit-hub-schedules/v1 revision
// as a new private file. The running hub service stops on a changed policy
// rather than retaining authority from a stale configuration, so installing
// the revision and restarting the service remain the administrator's
// explicit actions; this window commits nothing to any host.
func (a *App) SaveSchedulePolicy(request SchedulePolicyRequest) SchedulePreviewResult {
	return run(a, false, true, func(context.Context) SchedulePreviewResult {
		preview := schedulePreview(request, time.Now())
		if preview.State != Completed {
			return preview
		}
		for _, entry := range preview.Entries {
			if entry.PinState == "mismatch" {
				return SchedulePreviewResult{State: Failed,
					Reason: "an entry's input pin does not match the prepared inputs of its spec; recompute the pin before enabling the schedule"}
			}
		}
		policy, declined := schedulePolicyFrom(request)
		if declined.reason != "" {
			return SchedulePreviewResult{State: declined.state, Reason: declined.reason}
		}
		canonical, err := canonicalDocument(policy)
		if err != nil {
			return SchedulePreviewResult{State: Failed, Reason: "the schedule policy could not be canonicalized"}
		}
		if declined := writePrivateDocument(request.Output, canonical); declined.reason != "" {
			return SchedulePreviewResult{State: declined.state, Reason: declined.reason}
		}
		preview.Identity = runnerprotocol.SchedulePolicyIdentity(policy)
		return preview
	})
}

// OpenSchedulePolicy reads one installed schedule policy through its own
// strict reader and reports the identity the hub binds its journal to.
func (a *App) OpenSchedulePolicy(path string) SchedulePreviewResult {
	return run(a, false, false, func(context.Context) SchedulePreviewResult {
		raw, declined := readPrivateFile(path, 1<<20)
		if declined.reason != "" {
			return SchedulePreviewResult{State: declined.state, Reason: declined.reason}
		}
		policy, err := runnerprotocol.DecodeSchedules(raw)
		if err != nil {
			return SchedulePreviewResult{State: Failed, Reason: "the schedule policy could not be read through its own strict reader"}
		}
		entries := make([]ScheduleEntryInput, len(policy.Schedules))
		for i, schedule := range policy.Schedules {
			entries[i] = ScheduleEntryInput{
				ID: schedule.ID, Zone: schedule.Zone, At: schedule.At, WindowSeconds: schedule.WindowSeconds,
				Runner: schedule.Runner, Spec: schedule.Spec, Input: schedule.Input,
				Route: schedule.Route, Approved: schedule.Approved,
			}
		}
		return schedulePreview(SchedulePolicyRequest{Entries: entries}, time.Now())
	})
}

// --- CI configuration and result handoffs ------------------------------------

// CIHandoffRequest names the six non-secret path and selection variables the
// supported CI integrations are documented around, the integration to
// prepare, and where the reviewed file is written. Gate, when present, adds
// the reviewed change-gate step after the suite run.
type CIHandoffRequest struct {
	Integration     string      `json:"integration"`
	Binary          string      `json:"binary"`
	OperationPolicy string      `json:"operation_policy"`
	SuiteFile       string      `json:"suite_file"`
	Environment     string      `json:"environment"`
	RunDirectory    string      `json:"run_directory"`
	CoverageFile    string      `json:"coverage_file"`
	Output          string      `json:"output"`
	Gate            *CIGateStep `json:"gate,omitzero"`
}

// CIGateStep is the reviewed change gate a handoff runs after the suite: the
// approved promotion `suite ci` must retain for a gate to assess it (its
// release references, approval, full approval identity and the operator's
// target revision), the privately reviewed baseline run, the reviewed gate
// policy with the identity pinned for it, and the new directory each
// invocation retains its snapshot in. Every path names the agent's
// filesystem, as the six variables do; nothing here is read on this machine.
type CIGateStep struct {
	Releases          string `json:"releases"`
	Promotion         string `json:"promotion"`
	PromotionIdentity string `json:"promotion_identity"`
	Revision          string `json:"revision"`
	Baseline          string `json:"baseline"`
	Policy            string `json:"policy"`
	PolicyIdentity    string `json:"policy_identity"`
	SnapshotDirectory string `json:"snapshot_directory"`
}

// CIHandoffResult carries one reviewed handoff document: the exact script or
// workflow text the customer installs, generated here and never assembled by
// hand. This application does not commit to a repository, authorize a
// third-party service, or upload anything; installation on customer-owned
// agents is the customer administrator's action.
type CIHandoffResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Document string `json:"document,omitzero"`
	Output   string `json:"output,omitzero"`
}

func (r *CIHandoffResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SaveCIHandoff generates the exact workflow a supported integration runs:
// the documented command, unchanged, with the six variables the customer
// provisions on a trusted self-hosted agent, and, when a reviewed change gate
// is requested, the documented gate step after it. Every value is validated
// before anything is written. The generated text contains no value, no
// credential and no patient data.
func (a *App) SaveCIHandoff(request CIHandoffRequest) CIHandoffResult {
	return run(a, false, true, func(context.Context) CIHandoffResult {
		if declined := validateCIHandoff(request); declined != "" {
			return CIHandoffResult{State: Failed, Reason: declined}
		}
		document := ciHandoffDocument(request)
		if declined := writePrivateDocument(request.Output, []byte(document)); declined.reason != "" {
			return CIHandoffResult{State: declined.state, Reason: declined.reason}
		}
		return CIHandoffResult{State: Completed, Document: document, Output: request.Output}
	})
}

// validateCIHandoff answers the first thing wrong with a handoff request, in
// the order the form asks for it, or nothing. Every value the checklist
// names is one line: a line break would end the checklist's comment and put
// the rest of the value into the workflow the agent runs.
func validateCIHandoff(request CIHandoffRequest) string {
	switch request.Integration {
	case "posix", "github", "azure":
	default:
		return "the supported integrations are the POSIX shell, GitHub Actions and Azure DevOps workflows the customer CI documentation describes"
	}
	if declined := uncleanPath(
		agentValue{"the installed readmit executable", request.Binary},
		agentValue{"the activated operation policy", request.OperationPolicy},
		agentValue{"the saved suite", request.SuiteFile},
	); declined != "" {
		return declined
	}
	if !runnerprotocol.ID(request.Environment) {
		return "the environment is the named nonproduction environment the suite binds to"
	}
	if declined := uncleanPath(
		agentValue{"the run directory", request.RunDirectory},
		agentValue{"the coverage declaration", request.CoverageFile},
	); declined != "" {
		return declined
	}
	gate := request.Gate
	if gate == nil {
		return ""
	}
	if declined := uncleanPath(
		agentValue{"the release references", gate.Releases},
		agentValue{"the promotion approval", gate.Promotion},
	); declined != "" {
		return declined
	}
	if !fullIdentity(gate.PromotionIdentity) {
		return "the promotion approval identity is its full 64-character lowercase SHA-256 identity"
	}
	if strings.TrimSpace(gate.Revision) == "" || len(gate.Revision) > 256 || !utf8.ValidString(gate.Revision) || strings.ContainsFunc(gate.Revision, unicode.IsControl) {
		return "the target revision is the operator's one-line assumption of at most 256 bytes"
	}
	if declined := uncleanPath(
		agentValue{"the reviewed baseline run directory", gate.Baseline},
		agentValue{"the reviewed gate policy", gate.Policy},
	); declined != "" {
		return declined
	}
	if !fullIdentity(gate.PolicyIdentity) {
		return "the reviewed gate policy identity is its full 64-character lowercase SHA-256 identity"
	}
	if declined := uncleanPath(agentValue{"the retained gate snapshot directory", gate.SnapshotDirectory}); declined != "" {
		return declined
	}
	// `suite gate` refuses a snapshot inside either run it copies, and the
	// baseline must be a run other than the one being judged.
	directories := []string{request.RunDirectory, gate.Baseline, gate.SnapshotDirectory}
	for i, one := range directories {
		for _, other := range directories[i+1:] {
			if sameOrInside(one, other) || sameOrInside(other, one) {
				return "the run directory, the reviewed baseline and the retained gate snapshot are three separate folders, none inside another"
			}
		}
	}
	return ""
}

// agentValue is one path the handoff form asks for, by the name its refusal
// gives it.
type agentValue struct{ name, path string }

// uncleanPath names the first of the values that is not a cleaned absolute
// path on the agent, on one line.
func uncleanPath(values ...agentValue) string {
	for _, value := range values {
		if !agentPath(value.path) {
			return value.name + " must be a cleaned absolute path on the agent"
		}
	}
	return ""
}

// agentPath is a cleaned absolute path on one line.
func agentPath(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

// sameOrInside reports whether path is folder or inside it.
func sameOrInside(path, folder string) bool {
	rel, err := filepath.Rel(folder, path)
	return err == nil && filepath.IsLocal(rel)
}

// fullIdentity is a full SHA-256 identity as readmit prints one: 64 lowercase
// hexadecimal digits.
func fullIdentity(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return false
		}
	}
	return true
}

// ciWorkflowMarker separates the provisioning checklist (which names the
// customer's reviewed values) from the workflow text (which is env-var driven
// and identical for every customer). Tests and reviewers read the workflow
// part alone.
const ciWorkflowMarker = "# --- reviewed workflow (install as-is) ---"

// ciSuiteCommand and ciGatedSuiteCommand are the documented `suite ci`
// invocations; the gated one also retains the approved promotion the change
// gate assesses. ciGateCommand is the documented change gate. It needs no
// operation policy: assessing retained evidence is not licensed work.
const (
	ciSuiteCommand      = `"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m`
	ciGatedSuiteCommand = `"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --releases "$RELEASES_FILE" --promotion "$PROMOTION_FILE" --promotion-identity "$PROMOTION_IDENTITY" --revision "$TARGET_REVISION" --send --deadline 5m`
	ciGateCommand       = `"$READMIT_BIN" suite gate "$RUN_DIRECTORY" --baseline "$BASELINE_DIRECTORY" --policy "$GATE_POLICY" --policy-identity "$GATE_POLICY_IDENTITY" --output "$GATE_DIRECTORY"`
)

// ciHandoffDocument renders the supported integrations' exact documented
// workflows. The command text is the command the documentation promises and
// the differential tests execute; every line before the marker is a comment,
// so the whole document is valid for its target and nothing customer-specific
// reaches the workflow itself. A gated workflow runs the change gate after the
// suite whether or not the suite passed, and never lets the gate's exit
// replace the suite's: the POSIX script exits with the suite's status when it
// failed, and each hosted integration runs the gate as its own step.
func ciHandoffDocument(request CIHandoffRequest) string {
	checklist := []string{
		"Provision these six non-secret path/selection variables on the trusted customer-owned agent:",
		"  OPERATION_POLICY=" + request.OperationPolicy,
		"  READMIT_BIN=" + request.Binary,
		"  SUITE_FILE=" + request.SuiteFile,
		"  SUITE_ENVIRONMENT=" + request.Environment,
		"  RUN_DIRECTORY=" + request.RunDirectory + " (a new path on the persistent private volume for this one invocation)",
		"  COVERAGE_FILE=" + request.CoverageFile,
	}
	gate := request.Gate
	command := ciSuiteCommand
	if gate != nil {
		command = ciGatedSuiteCommand
		checklist = append(checklist,
			"The reviewed change gate adds these eight, pinned in protected customer configuration:",
			"  RELEASES_FILE="+gate.Releases,
			"  PROMOTION_FILE="+gate.Promotion,
			"  PROMOTION_IDENTITY="+gate.PromotionIdentity,
			"  TARGET_REVISION="+gate.Revision,
			"  BASELINE_DIRECTORY="+gate.Baseline,
			"  GATE_POLICY="+gate.Policy,
			"  GATE_POLICY_IDENTITY="+gate.PolicyIdentity,
			"  GATE_DIRECTORY="+gate.SnapshotDirectory+" (a new path on the persistent private volume for this one invocation)",
		)
	}
	checklist = append(checklist,
		"No checkout, upload, retry or network install runs here. Raw evidence and reports stay private and are not CI artifacts.",
		"Gate on the process exit; disable automatic reruns and inspect receiver state before authorizing another execution.",
	)
	if gate != nil {
		checklist = append(checklist,
			"The change gate runs after the suite even when it failed, never sends, and never replaces the suite's exit status; require both in branch or environment protection.",
			"Never compute and accept a new gate policy identity inside the pipeline; a changed policy needs a new review.",
		)
	}
	checklist = append(checklist, "", ciWorkflowMarker, "")
	header := ""
	for _, line := range checklist {
		header += "# " + line + "\n"
	}
	switch request.Integration {
	case "github":
		workflow := header + `name: Customer saved suite
on: workflow_dispatch
permissions: {}
concurrency:
  group: readmit-approved-fixture
  cancel-in-progress: false
jobs:
  regression:
    runs-on: [self-hosted, readmit-private]
    timeout-minutes: 10
    steps:
      - name: Execute the saved suite
        shell: bash
        run: |
          ` + command + "\n"
		if gate != nil {
			workflow += `      - name: Retain the reviewed change gate
        if: ${{ !cancelled() }}
        shell: bash
        run: |
          ` + ciGateCommand + "\n"
		}
		return workflow
	case "azure":
		workflow := header + `trigger: none
pr: none
pool:
  name: readmit-private
steps:
  - checkout: none
  - bash: |
      ` + command + `
    displayName: Execute the saved suite
    timeoutInMinutes: 10
`
		if gate != nil {
			workflow += `  - bash: |
      ` + ciGateCommand + `
    displayName: Retain the reviewed change gate
    condition: succeededOrFailed()
    timeoutInMinutes: 10
`
		}
		return workflow
	default:
		if gate != nil {
			return "#!/bin/sh\n" + header + command + "\nexecution=$?\n" + ciGateCommand + "\ngate=$?\n" +
				"if [ \"$execution\" -ne 0 ]; then\n  exit \"$execution\"\nfi\nexit \"$gate\"\n"
		}
		return "#!/bin/sh\n" + header + command + "\nexit $?\n"
	}
}

// CIResultsView is one retained CI aggregate or change-gate summary, decoded
// through its own strict reader. Every member is fixed vocabulary or a
// numeric count; that is what makes the summary safe where the retained
// evidence beside it is not.
type CIResultsView struct {
	Schema   string `json:"schema"`
	State    string `json:"state"`
	ExitCode int    `json:"exit_code"`
}

// CIInspectResult carries the retained summaries of one CI output directory:
// the suite gate aggregate and, when a reviewed change gate was retained, the
// gate summary. Missing summaries are reported; an absent summary is never a
// pass.
type CIInspectResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	CI      *CIResultsView `json:"ci,omitzero"`
	Gate    *CIResultsView `json:"gate,omitzero"`
	Warning string         `json:"warning,omitzero"`
}

func (r *CIInspectResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// InspectCIResults reads the retained aggregate files of one CI output
// directory read-only. It never re-runs, resumes or resends anything.
func (a *App) InspectCIResults(directory string) CIInspectResult {
	return run(a, false, false, func(context.Context) CIInspectResult {
		if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
			return CIInspectResult{State: Failed, Reason: "a CI output directory is named by a cleaned absolute path"}
		}
		result := CIInspectResult{State: Completed}
		raw, declined := readPrivateFile(filepath.Join(directory, "ci.json"), 4096)
		switch {
		case declined.reason != "":
			return CIInspectResult{State: Failed, Reason: "no retained CI summary could be read; gate on the process exit, never the presence of an old file"}
		default:
			report, err := suite.DecodeCI(raw)
			if err != nil {
				return CIInspectResult{State: Failed, Reason: "the retained CI summary is not a valid readmit-suite-ci/v1 document"}
			}
			result.CI = &CIResultsView{Schema: report.Schema, State: report.State, ExitCode: report.ExitCode}
		}
		if raw, declined := readPrivateFile(filepath.Join(directory, "gate.json"), 4096); declined.reason == "" {
			if report, err := suite.DecodeGateReport(raw); err == nil {
				result.Gate = &CIResultsView{Schema: report.Schema, State: report.State, ExitCode: report.ExitCode}
			} else {
				result.Warning = "a retained change-gate summary is present but not readable"
			}
		}
		return result
	})
}

// GatePolicyResult carries one reviewed CI gate policy as its strict reader
// reports it, with the canonical identity the customer pins independently in
// protected configuration. Reading a policy approves nothing.
type GatePolicyResult struct {
	State             State  `json:"state"`
	Reason            string `json:"reason,omitzero"`
	Identity          string `json:"identity,omitzero"`
	Environment       string `json:"environment,omitzero"`
	Revision          string `json:"revision,omitzero"`
	Engine            string `json:"engine,omitzero"`
	PromotionIdentity string `json:"promotion_identity,omitzero"`
	Specifications    int    `json:"specifications,omitzero"`
	RetainUntil       string `json:"retain_until,omitzero"`
	Approver          string `json:"approver,omitzero"`
	Rationale         string `json:"rationale,omitzero"`
}

func (r *GatePolicyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// InspectGatePolicy reads one reviewed gate policy and computes its canonical
// identity exactly as `readmit suite gate-policy` prints it, so the panel
// shows the identity to pin rather than asking anyone to copy a hash.
func (a *App) InspectGatePolicy(path string) GatePolicyResult {
	return run(a, false, false, func(context.Context) GatePolicyResult {
		raw, declined := readPrivateFile(path, suite.MaxBytes)
		if declined.reason != "" {
			return GatePolicyResult{State: declined.state, Reason: declined.reason}
		}
		policy, err := suite.DecodeGatePolicy(raw)
		if err != nil {
			return GatePolicyResult{State: Failed, Reason: "the gate policy is not a valid readmit-ci-gate-policy/v1 document"}
		}
		return GatePolicyResult{State: Completed, Identity: policy.Identity(), Environment: policy.Environment,
			Revision: policy.Revision, Engine: policy.Engine, PromotionIdentity: policy.Promotion,
			Specifications: len(policy.Coverage.Specifications), RetainUntil: policy.RetainUntil,
			Approver: policy.Approver, Rationale: policy.Rationale}
	})
}

// CIGateVerifyResult carries one retained change-gate snapshot's
// verification: the readmit-ci-gate/v1 summary `readmit suite verify-gate`
// prints for the same snapshot and identity, and the parts of it that stayed
// unknown, which are what the verification could not establish. A snapshot
// whose gate is unknown is not reported as completed: it could not be
// verified, and an unknown gate is never a pass. Like the summary, the result
// carries fixed vocabulary only — no path, label, hash or value.
type CIGateVerifyResult struct {
	State      State             `json:"state"`
	Reason     string            `json:"reason,omitzero"`
	Gate       *suite.GateReport `json:"gate,omitzero"`
	Unverified []string          `json:"unverified,omitzero"`
}

func (r *CIGateVerifyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// VerifyCIGate verifies one retained change-gate snapshot against the policy
// identity pinned for it, through suite.VerifyGate exactly as
// `readmit suite verify-gate` does: every retained byte is checked against the
// snapshot's manifest and the assessment is repeated at its original instant,
// with retention expiry judged by this machine's clock. It reads only the
// snapshot, never sends, reruns or resumes anything, and changes no byte.
// Cancelling it stops the reading and reaches no verdict.
func (a *App) VerifyCIGate(directory, identity string) CIGateVerifyResult {
	return runNamed[CIGateVerifyResult, *CIGateVerifyResult](a, profiles["VerifyCIGate"], func(ctx context.Context) CIGateVerifyResult {
		return verifyCIGate(ctx, directory, identity, time.Now().UTC())
	})
}

// ciGateVerifyOperation names a verification while it holds the slot, so the
// CI panel's cancel control stops exactly the verification it started. It is
// local work that reaches no destination.
const ciGateVerifyOperation = "ci-gate-verify"

// ciGateParts are the summary's component verdicts in the order it lists them.
var ciGateParts = []struct {
	name  string
	state func(suite.GateReport) string
}{
	{"approval", func(r suite.GateReport) string { return r.Approval }},
	{"pins", func(r suite.GateReport) string { return r.Pins }},
	{"coverage", func(r suite.GateReport) string { return r.Coverage }},
	{"baseline", func(r suite.GateReport) string { return r.Baseline }},
	{"retention", func(r suite.GateReport) string { return r.Retention }},
	{"target_revision", func(r suite.GateReport) string { return r.TargetRevision }},
}

func verifyCIGate(ctx context.Context, directory, identity string, now time.Time) CIGateVerifyResult {
	if !agentPath(directory) {
		return CIGateVerifyResult{State: Failed, Reason: "a retained gate snapshot is named by a cleaned absolute path"}
	}
	if !fullIdentity(identity) {
		return CIGateVerifyResult{State: Failed, Reason: "the pinned gate policy identity is its full 64-character lowercase SHA-256 identity"}
	}
	report := suite.VerifyGate(ctx, directory, identity, now)
	// A cancellation that stopped the reading leaves the gate unknown; one that
	// arrived after the verdict was reached does not take it back.
	if report.State == "unknown" && ctx.Err() != nil {
		return CIGateVerifyResult{State: Cancelled, Reason: "the verification was cancelled before it reached a verdict; nothing was changed"}
	}
	result := CIGateVerifyResult{State: Completed, Gate: &report}
	for _, part := range ciGateParts {
		if part.state(report) == "unknown" {
			result.Unverified = append(result.Unverified, part.name)
		}
	}
	// VerifyGate answers retention "retained" only once every retained byte
	// matched a manifest pinned to this identity; before that, it knows
	// nothing, and past the retention end it stops at "expired".
	if report.State == "unknown" {
		result.State = Failed
		switch report.Retention {
		case "retained":
			result.Reason = "the retained snapshot matches its manifest under this identity, but its repeated assessment is unknown; an unknown gate is never a pass"
		case "expired":
			result.Reason = "the retained snapshot matches its manifest under this identity, but its retention commitment has ended, so it no longer verifies; nothing was deleted"
		default:
			result.Reason = "the retained change gate could not be verified against this identity: it is missing, changed, retained under another policy or not a snapshot; an unknown gate is never a pass"
		}
	}
	return result
}
