package desktop

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/environment"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// The names the environment operations that reach the recorded environment
// run under, so the privacy status reports its environment row active while
// one of them holds the slot.
const (
	targetCheckOperation = "target-check"
	targetResetOperation = "target-reset"
	sendPolicyOperation  = "send-policy-evaluation"
)

// TargetSaveRequest saves one target configuration in the open workspace or at an absolute path.
type TargetSaveRequest struct {
	Workspace  string        `json:"workspace"`
	TargetFile string        `json:"target_file"`
	Target     replay.Target `json:"target"`
}

// TargetResult carries one target configuration.
type TargetResult struct {
	State      State          `json:"state"`
	Reason     string         `json:"reason,omitzero"`
	Target     *replay.Target `json:"target,omitzero"`
	TargetFile string         `json:"target_file,omitzero"`
}

func (r *TargetResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// TargetCheckRequest checks reachability and send policy for one target without sending HL7 payloads.
type TargetCheckRequest struct {
	Workspace    string `json:"workspace"`
	TargetFile   string `json:"target_file"`
	PolicyFile   string `json:"policy_file,omitzero"`
	DecisionFile string `json:"decision_file,omitzero"`
}

// EnvironmentReport describes the transport outcome and TLS details of checking an environment.
type EnvironmentReport struct {
	Name                       string `json:"name"`
	Classification             string `json:"classification"`
	Peer                       string `json:"peer"`
	Outcome                    string `json:"outcome"`
	Phase                      string `json:"phase"`
	ServerName                 string `json:"server_name,omitzero"`
	CipherSuite                string `json:"cipher_suite,omitzero"`
	TLSVersion                 string `json:"tls_version,omitzero"`
	ClientCertificateRequested bool   `json:"client_certificate_requested,omitzero"`
	ClientCertificatePresented bool   `json:"client_certificate_presented,omitzero"`
	Unsolicited                int    `json:"unsolicited"`
}

func toEnvironmentReport(rep environment.Report) *EnvironmentReport {
	res := &EnvironmentReport{
		Name:           rep.Environment.Name,
		Classification: string(rep.Environment.Classification),
		Peer:           rep.Peer,
		Outcome:        string(rep.Outcome),
		Phase:          rep.Phase,
		Unsolicited:    rep.Unsolicited,
	}
	if rep.TLS != nil {
		res.ServerName = rep.TLS.ServerName
		res.CipherSuite = rep.TLS.CipherSuite
		res.TLSVersion = rep.TLS.Version
		res.ClientCertificateRequested = rep.TLS.ClientCertificateRequested
		res.ClientCertificatePresented = rep.TLS.ClientCertificatePresented
	}
	return res
}

// TargetCheckResult carries the outcome of checking a target.
type TargetCheckResult struct {
	State    State                `json:"state"`
	Reason   string               `json:"reason,omitzero"`
	Report   *EnvironmentReport   `json:"report,omitzero"`
	Decision *sendpolicy.Decision `json:"decision,omitzero"`
}

func (r *TargetCheckResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// TargetResetRequest executes a reviewed fixture reset plan against a target environment.
type TargetResetRequest struct {
	Workspace   string   `json:"workspace"`
	TargetFile  string   `json:"target_file"`
	PlanFile    string   `json:"plan_file"`
	OutcomeFile string   `json:"outcome_file"`
	PolicyFile  string   `json:"policy_file,omitzero"`
	Confirmed   []string `json:"confirmed,omitzero"`
}

// TargetResetResult carries the retained outcome and reviewed plan of a fixture reset.
type TargetResetResult struct {
	State  State                `json:"state"`
	Reason string               `json:"reason,omitzero"`
	Result *fixturereset.Result `json:"result,omitzero"`
	Plan   *fixturereset.Plan   `json:"plan,omitzero"`
}

func (r *TargetResetResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SecretsResult carries one secret reference document.
type SecretsResult struct {
	State       State            `json:"state"`
	Reason      string           `json:"reason,omitzero"`
	Document    *secret.Document `json:"document,omitzero"`
	SecretsFile string           `json:"secrets_file,omitzero"`
}

func (r *SecretsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SecretSaveRequest adds or updates one reference in a secret store.
type SecretSaveRequest struct {
	Workspace   string           `json:"workspace"`
	SecretsFile string           `json:"secrets_file"`
	Reference   secret.Reference `json:"reference"`
	IsUpdate    bool             `json:"is_update,omitzero"`
}

// SecretTestResult reports whether a reference locator resolves without storing the credential.
type SecretTestResult struct {
	State   State  `json:"state"`
	Reason  string `json:"reason,omitzero"`
	Name    string `json:"name,omitzero"`
	Success bool   `json:"success"`
}

func (r *SecretTestResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SecretScanRequest checks files for residual credential leaks.
type SecretScanRequest struct {
	Workspace   string   `json:"workspace"`
	SecretsFile string   `json:"secrets_file"`
	Paths       []string `json:"paths"`
	Name        string   `json:"name,omitzero"`
}

// SecretScanResult carries the scan report.
type SecretScanResult struct {
	State   State              `json:"state"`
	Reason  string             `json:"reason,omitzero"`
	Scan    *exportreview.Scan `json:"scan,omitzero"`
	Skipped int                `json:"skipped"`
}

func (r *SecretScanResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SendPolicyResult carries one send policy document.
type SendPolicyResult struct {
	State      State              `json:"state"`
	Reason     string             `json:"reason,omitzero"`
	Policy     *sendpolicy.Policy `json:"policy,omitzero"`
	PolicyFile string             `json:"policy_file,omitzero"`
}

func (r *SendPolicyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SendPolicySaveRequest saves one send policy document.
type SendPolicySaveRequest struct {
	Workspace  string            `json:"workspace"`
	PolicyFile string            `json:"policy_file"`
	Policy     sendpolicy.Policy `json:"policy"`
}

// SendPolicyEvalRequest checks a destination against a policy locally without connecting.
type SendPolicyEvalRequest struct {
	Workspace      string `json:"workspace"`
	PolicyFile     string `json:"policy_file,omitzero"`
	Address        string `json:"address"`
	Classification string `json:"classification"`
	Explicit       bool   `json:"explicit,omitzero"`
}

// SendPolicyEvalResult carries the send decision.
type SendPolicyEvalResult struct {
	State    State                `json:"state"`
	Reason   string               `json:"reason,omitzero"`
	Decision *sendpolicy.Decision `json:"decision,omitzero"`
}

func (r *SendPolicyEvalResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ResetPlanResult carries one fixture reset plan document.
type ResetPlanResult struct {
	State    State              `json:"state"`
	Reason   string             `json:"reason,omitzero"`
	Plan     *fixturereset.Plan `json:"plan,omitzero"`
	PlanFile string             `json:"plan_file,omitzero"`
}

func (r *ResetPlanResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ResetPlanSaveRequest saves one fixture reset plan.
type ResetPlanSaveRequest struct {
	Workspace string            `json:"workspace"`
	PlanFile  string            `json:"plan_file"`
	Plan      fixturereset.Plan `json:"plan"`
}

// SaveTarget writes one target configuration in the open workspace or at an absolute path.
func (a *App) SaveTarget(request TargetSaveRequest) TargetResult {
	return run(a, false, true, func(context.Context) TargetResult {
		path, ref := resolveWorkspacePath(request.Workspace, request.TargetFile)
		if path == "" {
			return TargetResult{State: ref.state, Reason: ref.reason}
		}
		saved, err := operation.SaveTarget(path, request.Target)
		if err != nil {
			return TargetResult{State: Failed, Reason: err.Error()}
		}
		return TargetResult{State: Completed, Target: &saved, TargetFile: request.TargetFile}
	})
}

// ReadTarget reads one target configuration, or returns the default target if the file does not exist yet.
func (a *App) ReadTarget(workspace, targetFile string) TargetResult {
	return run(a, false, false, func(context.Context) TargetResult {
		path, ref := resolveWorkspacePath(workspace, targetFile)
		if path == "" {
			return TargetResult{State: ref.state, Reason: ref.reason}
		}
		target, err := operation.OpenOrNewTarget(path)
		if err != nil {
			return TargetResult{State: Failed, Reason: err.Error()}
		}
		return TargetResult{State: Completed, Target: &target, TargetFile: targetFile}
	})
}

// CheckTarget checks transport, TLS status and send decision for one target without sending HL7 payloads.
// It reaches the recorded address, so it is admitted as execution, as
// `readmit target check` is: an unactivated or expired term, or an activation
// with no runner authority, refuses it before anything is reached.
func (a *App) CheckTarget(request TargetCheckRequest) TargetCheckResult {
	return runNamed[TargetCheckResult, *TargetCheckResult](a, targetCheckOperation, true, false, func(ctx context.Context) (out TargetCheckResult) {
		settle, admitted := a.admitExecution(ctx)
		if admitted != nil {
			return TargetCheckResult{State: PermissionDenied, Reason: admitted.Error()}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State, out.Reason = Failed, settlementFailed
			}
		}()
		targetPath, ref := resolveWorkspacePath(request.Workspace, request.TargetFile)
		if targetPath == "" {
			return TargetCheckResult{State: ref.state, Reason: ref.reason}
		}
		target, err := operation.ReadTarget(targetPath)
		if err != nil {
			return TargetCheckResult{State: Failed, Reason: err.Error()}
		}
		var policy *sendpolicy.Policy
		if request.PolicyFile != "" {
			polPath, pRef := resolveWorkspacePath(request.Workspace, request.PolicyFile)
			if polPath == "" {
				return TargetCheckResult{State: pRef.state, Reason: pRef.reason}
			}
			p, err := operation.ReadSendPolicy(polPath)
			if err != nil {
				return TargetCheckResult{State: Failed, Reason: err.Error()}
			}
			policy = &p
		}
		report, decision, err := operation.DiagnoseTarget(ctx, target, policy, sendpolicy.SystemResolver)
		if request.DecisionFile != "" {
			decPath, dRef := resolveWorkspacePath(request.Workspace, request.DecisionFile)
			if decPath == "" {
				return TargetCheckResult{State: dRef.state, Reason: dRef.reason}
			}
			if err := sendpolicy.WriteDecision(decPath, decision); err != nil {
				return TargetCheckResult{State: Failed, Reason: err.Error()}
			}
		}
		rep := toEnvironmentReport(report)
		if err != nil {
			return TargetCheckResult{State: Failed, Reason: err.Error(), Report: rep, Decision: &decision}
		}
		return TargetCheckResult{State: Completed, Report: rep, Decision: &decision}
	})
}

// ResetTarget executes a reviewed fixture reset plan against a target environment.
// Like `readmit target reset`, it is admitted as execution as well as authoring.
func (a *App) ResetTarget(request TargetResetRequest) TargetResetResult {
	return runNamed[TargetResetResult, *TargetResetResult](a, targetResetOperation, true, true, func(ctx context.Context) (out TargetResetResult) {
		settle, admitted := a.admitExecution(ctx)
		if admitted != nil {
			return TargetResetResult{State: PermissionDenied, Reason: admitted.Error()}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State, out.Reason = Failed, settlementFailed
			}
		}()
		targetPath, ref := resolveWorkspacePath(request.Workspace, request.TargetFile)
		if targetPath == "" {
			return TargetResetResult{State: ref.state, Reason: ref.reason}
		}
		target, err := operation.ReadTarget(targetPath)
		if err != nil {
			return TargetResetResult{State: Failed, Reason: err.Error()}
		}
		planPath, pRef := resolveWorkspacePath(request.Workspace, request.PlanFile)
		if planPath == "" {
			return TargetResetResult{State: pRef.state, Reason: pRef.reason}
		}
		resolvedPlan, err := artifactpath.Resolve(planPath)
		if err != nil {
			return TargetResetResult{State: Failed, Reason: "cannot resolve the reset plan"}
		}
		data, err := readOperationFileBounded(planPath, fixturereset.MaxPlanBytes)
		if err != nil {
			return TargetResetResult{State: Failed, Reason: err.Error()}
		}
		var policy *sendpolicy.Policy
		if request.PolicyFile != "" {
			polPath, polRef := resolveWorkspacePath(request.Workspace, request.PolicyFile)
			if polPath == "" {
				return TargetResetResult{State: polRef.state, Reason: polRef.reason}
			}
			p, err := operation.ReadSendPolicy(polPath)
			if err != nil {
				return TargetResetResult{State: Failed, Reason: err.Error()}
			}
			policy = &p
		}
		var outcomePath string
		if request.OutcomeFile != "" {
			var oRef refusal
			outcomePath, oRef = resolveWorkspacePath(request.Workspace, request.OutcomeFile)
			if outcomePath == "" {
				return TargetResetResult{State: oRef.state, Reason: oRef.reason}
			}
		}
		result, plan, err := operation.ResetEnvironment(ctx, fixturereset.Request{
			Target:        target,
			PlanBytes:     data,
			PlanDirectory: filepath.Dir(resolvedPlan),
			Policy:        policy,
			Confirmed:     request.Confirmed,
		}, outcomePath, sendpolicy.SystemResolver)
		if err != nil {
			return TargetResetResult{State: Failed, Reason: err.Error(), Result: &result, Plan: &plan}
		}
		return TargetResetResult{State: Completed, Result: &result, Plan: &plan}
	})
}

// ReadSecrets reads one secret reference document.
func (a *App) ReadSecrets(workspace, secretsFile string) SecretsResult {
	return run(a, false, false, func(context.Context) SecretsResult {
		path, ref := resolveWorkspacePath(workspace, secretsFile)
		if path == "" {
			return SecretsResult{State: ref.state, Reason: ref.reason}
		}
		doc, err := operation.OpenOrEmptySecrets(path)
		if err != nil {
			return SecretsResult{State: Failed, Reason: err.Error()}
		}
		return SecretsResult{State: Completed, Document: &doc, SecretsFile: secretsFile}
	})
}

// SaveSecretReference adds or updates one reference in a secret store.
func (a *App) SaveSecretReference(request SecretSaveRequest) SecretsResult {
	return run(a, false, true, func(context.Context) SecretsResult {
		path, ref := resolveWorkspacePath(request.Workspace, request.SecretsFile)
		if path == "" {
			return SecretsResult{State: ref.state, Reason: ref.reason}
		}
		var doc secret.Document
		var err error
		if request.IsUpdate {
			store := request.Reference.Store
			addr := request.Reference.Address
			cmd := request.Reference.Command
			args := request.Reference.Arguments
			maxAge := request.Reference.MaxAge
			doc, _, err = operation.UpdateSecretReference(path, request.Reference.Name, secret.Change{
				Store:     &store,
				Address:   &addr,
				Command:   &cmd,
				Arguments: &args,
				MaxAge:    &maxAge,
			})
		} else {
			doc, _, err = operation.AddSecretReference(path, request.Reference)
		}
		if err != nil {
			return SecretsResult{State: Failed, Reason: err.Error()}
		}
		return SecretsResult{State: Completed, Document: &doc, SecretsFile: request.SecretsFile}
	})
}

// RemoveSecretReference removes one registered reference by name.
func (a *App) RemoveSecretReference(workspace, secretsFile, name string) SecretsResult {
	return run(a, false, true, func(context.Context) SecretsResult {
		path, ref := resolveWorkspacePath(workspace, secretsFile)
		if path == "" {
			return SecretsResult{State: ref.state, Reason: ref.reason}
		}
		doc, err := operation.RemoveSecretReference(path, name)
		if err != nil {
			return SecretsResult{State: Failed, Reason: err.Error()}
		}
		return SecretsResult{State: Completed, Document: &doc, SecretsFile: secretsFile}
	})
}

// TestSecretReference checks if the declared locator resolves the credential without storing or logging it.
func (a *App) TestSecretReference(workspace, secretsFile, name string) SecretTestResult {
	return run(a, true, false, func(ctx context.Context) SecretTestResult {
		path, ref := resolveWorkspacePath(workspace, secretsFile)
		if path == "" {
			return SecretTestResult{State: ref.state, Reason: ref.reason}
		}
		doc, err := operation.ReadSecrets(path)
		if err != nil {
			return SecretTestResult{State: Failed, Reason: err.Error()}
		}
		entry, err := secret.Find(doc, name)
		if err != nil {
			return SecretTestResult{State: Failed, Reason: err.Error()}
		}
		if err := operation.TestSecretReference(ctx, entry); err != nil {
			return SecretTestResult{State: Failed, Reason: err.Error(), Name: name, Success: false}
		}
		return SecretTestResult{State: Completed, Name: name, Success: true}
	})
}

// RotateSecretReference verifies resolution, increments generation, stamps rotation time, and saves.
func (a *App) RotateSecretReference(workspace, secretsFile, name string) SecretsResult {
	return run(a, true, true, func(ctx context.Context) SecretsResult {
		path, ref := resolveWorkspacePath(workspace, secretsFile)
		if path == "" {
			return SecretsResult{State: ref.state, Reason: ref.reason}
		}
		doc, _, err := operation.RotateSecretReference(ctx, path, name)
		if err != nil {
			return SecretsResult{State: Failed, Reason: err.Error()}
		}
		return SecretsResult{State: Completed, Document: &doc, SecretsFile: secretsFile}
	})
}

// ScanSecrets checks workspace files and configurations for residual credential leaks.
func (a *App) ScanSecrets(request SecretScanRequest) SecretScanResult {
	return run(a, true, false, func(ctx context.Context) SecretScanResult {
		secretsPath, ref := resolveWorkspacePath(request.Workspace, request.SecretsFile)
		if secretsPath == "" {
			return SecretScanResult{State: ref.state, Reason: ref.reason}
		}
		var checkPaths []string
		for _, p := range request.Paths {
			resolved, pRef := resolveWorkspacePath(request.Workspace, p)
			if resolved != "" && pRef.state == "" {
				checkPaths = append(checkPaths, resolved)
			}
		}
		scan, skipped, err := operation.ScanSecrets(ctx, secretsPath, checkPaths, request.Name)
		if err != nil {
			return SecretScanResult{State: Failed, Reason: err.Error()}
		}
		return SecretScanResult{State: Completed, Scan: &scan, Skipped: skipped}
	})
}

// ReadSendPolicy reads and decodes an approved-destination policy document.
func (a *App) ReadSendPolicy(workspace, policyFile string) SendPolicyResult {
	return run(a, false, false, func(context.Context) SendPolicyResult {
		path, ref := resolveWorkspacePath(workspace, policyFile)
		if path == "" {
			return SendPolicyResult{State: ref.state, Reason: ref.reason}
		}
		policy, err := operation.ReadSendPolicy(path)
		if err != nil {
			return SendPolicyResult{State: Failed, Reason: err.Error()}
		}
		return SendPolicyResult{State: Completed, Policy: &policy, PolicyFile: policyFile}
	})
}

// SaveSendPolicy writes an approved-destination policy atomically.
func (a *App) SaveSendPolicy(request SendPolicySaveRequest) SendPolicyResult {
	return run(a, false, true, func(context.Context) SendPolicyResult {
		path, ref := resolveWorkspacePath(request.Workspace, request.PolicyFile)
		if path == "" {
			return SendPolicyResult{State: ref.state, Reason: ref.reason}
		}
		saved, err := operation.SaveSendPolicy(path, request.Policy)
		if err != nil {
			return SendPolicyResult{State: Failed, Reason: err.Error()}
		}
		return SendPolicyResult{State: Completed, Policy: &saved, PolicyFile: request.PolicyFile}
	})
}

// EvaluateSendPolicy checks whether an address and classification is approved without connecting.
// A host name it is asked about is resolved through the system's resolver, so it
// runs under a name the privacy status reports.
func (a *App) EvaluateSendPolicy(request SendPolicyEvalRequest) SendPolicyEvalResult {
	return runNamed[SendPolicyEvalResult, *SendPolicyEvalResult](a, sendPolicyOperation, false, false, func(ctx context.Context) SendPolicyEvalResult {
		var policy *sendpolicy.Policy
		if request.PolicyFile != "" {
			path, ref := resolveWorkspacePath(request.Workspace, request.PolicyFile)
			if path == "" {
				return SendPolicyEvalResult{State: ref.state, Reason: ref.reason}
			}
			p, err := operation.ReadSendPolicy(path)
			if err != nil {
				return SendPolicyEvalResult{State: Failed, Reason: err.Error()}
			}
			policy = &p
		}
		decision := operation.EvaluateSendPolicy(ctx, policy, request.Address, request.Classification, request.Explicit, sendpolicy.SystemResolver)
		return SendPolicyEvalResult{State: Completed, Decision: &decision}
	})
}

// ReadResetPlan reads and decodes a fixture reset plan document.
func (a *App) ReadResetPlan(workspace, planFile string) ResetPlanResult {
	return run(a, false, false, func(context.Context) ResetPlanResult {
		path, ref := resolveWorkspacePath(workspace, planFile)
		if path == "" {
			return ResetPlanResult{State: ref.state, Reason: ref.reason}
		}
		plan, err := operation.ReadResetPlan(path)
		if err != nil {
			return ResetPlanResult{State: Failed, Reason: err.Error()}
		}
		return ResetPlanResult{State: Completed, Plan: &plan, PlanFile: planFile}
	})
}

// SaveResetPlan writes a fixture reset plan atomically.
func (a *App) SaveResetPlan(request ResetPlanSaveRequest) ResetPlanResult {
	return run(a, false, true, func(context.Context) ResetPlanResult {
		path, ref := resolveWorkspacePath(request.Workspace, request.PlanFile)
		if path == "" {
			return ResetPlanResult{State: ref.state, Reason: ref.reason}
		}
		saved, err := operation.SaveResetPlan(path, request.Plan)
		if err != nil {
			return ResetPlanResult{State: Failed, Reason: err.Error()}
		}
		return ResetPlanResult{State: Completed, Plan: &saved, PlanFile: request.PlanFile}
	})
}

func resolveWorkspacePath(workspace, entryOrPath string) (string, refusal) {
	if entryOrPath == "" {
		return "", refusal{Failed, "file path must not be empty"}
	}
	if workspace == "" {
		if filepath.IsAbs(entryOrPath) {
			return entryOrPath, refusal{}
		}
		return "", refusal{Failed, "file path must be absolute when no workspace is open"}
	}
	root, ref := resolveFolder(workspace)
	if root == "" {
		return "", ref
	}
	if filepath.IsAbs(entryOrPath) {
		return entryOrPath, refusal{}
	}
	clean := filepath.Clean(entryOrPath)
	if !filepath.IsLocal(clean) {
		return "", refusal{Failed, "file path must remain within the workspace"}
	}
	return filepath.Join(root, clean), refusal{}
}

func readOperationFileBounded(path string, limit int) ([]byte, error) {
	data, err := readOperationFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("file exceeds size limit")
	}
	return data, nil
}
