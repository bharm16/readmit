package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
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
	return RunnerDocumentResult{State: Completed, Document: string(canonical), SHA256: digest(canonical)}
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
	return run(a, false, true, func(ctx context.Context) RunnerEnrollmentResult {
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
// execution would, without contacting the hub or opening a connection.
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

// ExecuteRunnerJob runs one job through the same enrolled path as the
// command line: explicit operation admission first, then the runner's own
// lease, duplicate-admission and state-isolation rules, which this facade
// does not widen. Cancel stops future sends and retains any uncertain
// delivery exactly as `run start` does.
func (a *App) ExecuteRunnerJob(request RunnerExecuteRequest) RunnerExecutionResult {
	return runNamed[RunnerExecutionResult, *RunnerExecutionResult](a, "runner", true, true, func(ctx context.Context) (out RunnerExecutionResult) {
		// The explicit execution approval is the runner's own admission,
		// asked of the selected operation policy exactly once, as the command
		// line asks it. This seam is also where a team authority (the
		// administration surface) settles what may execute.
		guard, _ := a.selectedOperation()
		ctx = customerrunner.WithOperationGuard(ctx, guard)
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
			// broken execution: the authority declined this instance.
			if !errors.Is(err, customerrunner.ErrRefused) && summary.Schema == "" {
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

// RunnerUpdateResult reports one explicit update verification.
type RunnerUpdateResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
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
		return RunnerUpdateResult{State: Completed}
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
	a.hubMu.Lock()
	client, session := a.hubClient, a.hubSession
	a.hubMu.Unlock()
	if client == nil || session == nil || session.IsExpired(time.Now()) {
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
// configurations are held to: a regular, private, bounded file.
func readPrivateFile(path string, limit int64) ([]byte, refusal) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrPermission) {
		return nil, refusal{PermissionDenied, "this account cannot read the selected file"}
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !privateFileMode(info.Mode()) || info.Size() > limit {
		return nil, refusal{Failed, "the selected file must be a private regular file"}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, refusal{PermissionDenied, "this account cannot read the selected file"}
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, refusal{Failed, "the selected file must be a private regular file"}
	}
	return raw, refusal{}
}

// privateFileMode mirrors the runner's own rule: Windows ACLs are the
// administrator's boundary and mode bits do not represent them.
func privateFileMode(mode os.FileMode) bool {
	return runtime.GOOS == "windows" || mode.Perm()&0077 == 0
}

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
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return refusal{Failed, "an existing document is never replaced; choose a new destination"}
		}
		if errors.Is(err, fs.ErrPermission) {
			return refusal{PermissionDenied, "this account cannot write the destination"}
		}
		return refusal{Failed, "cannot create the destination file"}
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destination)
		return refusal{Failed, "cannot write the destination file"}
	}
	return refusal{}
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
// defaults to today; it never changes what a save writes.
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
	return run(a, false, false, func(context.Context) SchedulePreviewResult {
		return schedulePreview(request)
	})
}

// schedulePreview is the computation behind both the preview operation and a
// save; it holds no operation slot of its own, so a save composes it once.
func schedulePreview(request SchedulePolicyRequest) SchedulePreviewResult {
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
	today := time.Now().UTC()
	if request.Anchor != "" {
		anchored, err := time.Parse("2006-01-02", request.Anchor)
		if err != nil {
			return SchedulePreviewResult{State: Failed, Reason: "the preview anchor is one day in 2006-01-02 form"}
		}
		today = anchored
	}
	for i, entry := range request.Entries {
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
			day := today.In(time.UTC).AddDate(0, 0, dayOffset).Format("2006-01-02")
			occurrence := ScheduleOccurrence{Day: day, State: "scheduled"}
			if due, exists := runnerprotocol.DailyOccurrence(day, policy.Schedules[i].At, policy.Schedules[i].Zone); !exists {
				occurrence.State = "dst-gap"
			} else {
				occurrence.UTC = due.UTC().Format(time.RFC3339)
				if time.Now().UTC().Sub(due) > time.Duration(policy.Schedules[i].WindowSeconds)*time.Second {
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
		preview := schedulePreview(request)
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
		return schedulePreview(SchedulePolicyRequest{Entries: entries})
	})
}

// --- CI configuration and result handoffs ------------------------------------

// CIHandoffRequest names the six non-secret path and selection variables the
// supported CI integrations are documented around, the integration to
// prepare, and where the reviewed file is written.
type CIHandoffRequest struct {
	Integration     string `json:"integration"`
	Binary          string `json:"binary"`
	OperationPolicy string `json:"operation_policy"`
	SuiteFile       string `json:"suite_file"`
	Environment     string `json:"environment"`
	RunDirectory    string `json:"run_directory"`
	CoverageFile    string `json:"coverage_file"`
	Output          string `json:"output"`
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
// provisions on a trusted self-hosted agent. The generated text contains no
// value, no credential and no patient data.
func (a *App) SaveCIHandoff(request CIHandoffRequest) CIHandoffResult {
	return run(a, false, true, func(context.Context) CIHandoffResult {
		switch request.Integration {
		case "posix", "github", "azure":
		default:
			return CIHandoffResult{State: Failed, Reason: "the supported integrations are the POSIX shell, GitHub Actions and Azure DevOps workflows the customer CI documentation describes"}
		}
		if !runnerprotocol.ID(request.Environment) {
			return CIHandoffResult{State: Failed, Reason: "the environment is the named nonproduction environment the suite binds to"}
		}
		for name, value := range map[string]string{
			"the installed readmit executable": request.Binary,
			"the activated operation policy":   request.OperationPolicy,
			"the saved suite":                  request.SuiteFile,
			"the run directory":                request.RunDirectory,
			"the coverage declaration":         request.CoverageFile,
		} {
			if !filepath.IsAbs(value) || filepath.Clean(value) != value {
				return CIHandoffResult{State: Failed, Reason: name + " must be a cleaned absolute path on the agent"}
			}
		}
		document := ciHandoffDocument(request)
		if declined := writePrivateDocument(request.Output, []byte(document)); declined.reason != "" {
			return CIHandoffResult{State: declined.state, Reason: declined.reason}
		}
		return CIHandoffResult{State: Completed, Document: document, Output: request.Output}
	})
}

// ciWorkflowMarker separates the provisioning checklist (which names the
// customer's reviewed values) from the workflow text (which is env-var driven
// and identical for every customer). Tests and reviewers read the workflow
// part alone.
const ciWorkflowMarker = "# --- reviewed workflow (install as-is) ---"

// ciHandoffDocument renders the supported integrations' exact documented
// workflows. The command text is the command the documentation promises and
// the differential tests execute; every line before the marker is a comment,
// so the whole document is valid for its target and nothing customer-specific
// reaches the workflow itself.
func ciHandoffDocument(request CIHandoffRequest) string {
	checklist := []string{
		"Provision these six non-secret path/selection variables on the trusted customer-owned agent:",
		"  OPERATION_POLICY=" + request.OperationPolicy,
		"  READMIT_BIN=" + request.Binary,
		"  SUITE_FILE=" + request.SuiteFile,
		"  SUITE_ENVIRONMENT=" + request.Environment,
		"  RUN_DIRECTORY=" + request.RunDirectory + " (a new path on the persistent private volume for this one invocation)",
		"  COVERAGE_FILE=" + request.CoverageFile,
		"No checkout, upload, retry or network install runs here. Raw evidence and reports stay private and are not CI artifacts.",
		"Gate on the process exit; disable automatic reruns and inspect receiver state before authorizing another execution.",
		"",
		ciWorkflowMarker,
		"",
	}
	header := ""
	for _, line := range checklist {
		header += "# " + line + "\n"
	}
	command := `"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m`
	switch request.Integration {
	case "github":
		return header + `name: Customer saved suite
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
	case "azure":
		return header + `trigger: none
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
	default:
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
