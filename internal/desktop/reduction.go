package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// ReductionRequest configures one controlled reduction over the verified case.
// Spec is the regression test whose failure signature is held; ResetPlan and
// Target are the reviewed reset and approved environment every trial uses;
// Policy is the optional approved-destination document a connect-authority
// reset needs. Confirmed names the reset actions the operator has authorised.
// Work is a new workspace entry that receives trial material and is never
// evidence.
type ReductionRequest struct {
	Workspace     string   `json:"workspace"`
	Case          string   `json:"case"`
	Identity      string   `json:"identity"`
	Spec          string   `json:"spec"`
	Rules         string   `json:"rules,omitzero"`
	Grouping      string   `json:"grouping"`
	Assertions    []string `json:"assertions"`
	Trials        int      `json:"trials"`
	Confirmations int      `json:"confirmations"`
	ResetPlan     string   `json:"reset_plan"`
	Target        string   `json:"target"`
	Policy        string   `json:"policy,omitzero"`
	Confirmed     []string `json:"confirmed"`
	Work          string   `json:"work"`
}

// ReductionView is one preview or finished reduction as the window shows it.
type ReductionView struct {
	Case        string          `json:"case"`
	Spec        string          `json:"spec"`
	Work        string          `json:"work,omitzero"`
	Preview     *reduce.Preview `json:"preview,omitzero"`
	Report      *reduce.Report  `json:"report,omitzero"`
	Boundary    string          `json:"boundary"`
	Observation string          `json:"observation"`
}

// ReductionResult carries one state. Reduction is present whenever a preview
// or a run produced an answer, including an undecided or cancelled outcome.
type ReductionResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Reduction *ReductionView `json:"reduction,omitzero"`
}

func (r *ReductionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) reduction() ReductionResult {
	return ReductionResult{State: r.state, Reason: r.reason}
}

const reductionObservation = "Each retained trial is one durable run after one reviewed reset. " +
	"Unsupported, incomplete, timed-out or delivery-uncertain trials cannot establish reproduction of a workflow failure. " +
	"Cancel stops further trials and recovers retained trial output without resending."

// PreviewReduction reports how the declared plan would take the sequence apart
// and what side effects a run would spend, without resetting, sending, or
// writing evidence. The durable oracle is built only to resolve the sequence
// and signature pins the chosen assertions name.
func (a *App) PreviewReduction(request ReductionRequest) ReductionResult {
	return run(a, false, false, func(context.Context) ReductionResult {
		prepared, declined := a.prepareReduction(request, false)
		if declined.state != "" {
			return declined.reduction()
		}
		preview, err := reduce.PreviewPlan(prepared.request)
		if err != nil {
			return ReductionResult{State: Failed, Reason: err.Error()}
		}
		return ReductionResult{State: Completed, Reduction: &ReductionView{
			Case: request.Case, Spec: request.Spec, Preview: &preview,
			Boundary: preview.Scope, Observation: reductionObservation,
		}}
	})
}

// StartReduction runs one controlled reduction under existing execution and
// reset authorisation. Cancel stops further trials; retained trial directories
// are left for recovery and nothing is resent. The report's own outcome names
// whether anything was established.
func (a *App) StartReduction(request ReductionRequest) ReductionResult {
	return runNamed[ReductionResult, *ReductionResult](a, "reduction", true, false, func(ctx context.Context) (out ReductionResult) {
		guard, _ := a.selectedOperation()
		settle, admissionErr := guard.AdmitContext(ctx, "execute")
		if admissionErr != nil {
			return ReductionResult{State: PermissionDenied, Reason: admissionErr.Error()}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State = Failed
				out.Reason = "runner settlement failed; reconcile the retained admission before new work"
			}
		}()
		prepared, declined := a.prepareReduction(request, true)
		if declined.state != "" {
			return declined.reduction()
		}
		bounded, cancel := context.WithTimeout(ctx, operationguard.MaxDuration)
		defer cancel()
		report, err := reduce.Run(bounded, prepared.request, prepared.oracle)
		if err != nil {
			return ReductionResult{State: Failed, Reason: err.Error()}
		}
		view := &ReductionView{
			Case: request.Case, Spec: request.Spec, Work: request.Work,
			Report: &report, Boundary: report.Scope, Observation: reductionObservation,
		}
		if report.Outcome == reduce.OutcomeUndecided && report.Reason == reduce.OperatorStopped {
			return ReductionResult{State: Cancelled, Reason: "reduction stopped; retained trials were not resent", Reduction: view}
		}
		return ReductionResult{State: Completed, Reduction: view}
	})
}

type preparedReduction struct {
	request reduce.Request
	oracle  *reduce.DurableOracle
}

func (a *App) prepareReduction(request ReductionRequest, createWork bool) (preparedReduction, refusal) {
	root, source, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return preparedReduction{}, declined
	}
	specPath, declined := resolveWorkspacePath(request.Workspace, request.Spec)
	if specPath == "" {
		return preparedReduction{}, declined
	}
	targetPath, declined := resolveWorkspacePath(request.Workspace, request.Target)
	if targetPath == "" {
		return preparedReduction{}, declined
	}
	target, err := operation.ReadTarget(targetPath)
	if err != nil {
		return preparedReduction{}, refusal{Failed, err.Error()}
	}
	planPath, declined := resolveWorkspacePath(request.Workspace, request.ResetPlan)
	if planPath == "" {
		return preparedReduction{}, declined
	}
	resolvedPlan, err := artifactpath.Resolve(planPath)
	if err != nil {
		return preparedReduction{}, refusal{Failed, "cannot resolve the reset plan"}
	}
	planBytes, err := readOperationFileBounded(planPath, fixturereset.MaxPlanBytes)
	if err != nil {
		return preparedReduction{}, refusal{Failed, err.Error()}
	}
	var policy *sendpolicy.Policy
	if request.Policy != "" {
		polPath, declined := resolveWorkspacePath(request.Workspace, request.Policy)
		if polPath == "" {
			return preparedReduction{}, declined
		}
		p, err := operation.ReadSendPolicy(polPath)
		if err != nil {
			return preparedReduction{}, refusal{Failed, err.Error()}
		}
		policy = &p
	}
	grouping := request.Grouping
	if grouping == "" {
		grouping = reduce.GroupPerOccurrence
	}
	var rules correlate.Rules
	var rulesDigest string
	if request.Rules != "" {
		data, declined := workspaceDocument(root, request.Rules, correlate.MaxRulesBytes, "the correlation rules document")
		if declined.state != "" {
			return preparedReduction{}, declined
		}
		parsed, err := correlate.ParseRules(data)
		if err != nil {
			return preparedReduction{}, refusal{Failed, err.Error()}
		}
		rules = parsed
		encoded, err := json.Marshal(parsed, json.Deterministic(true))
		if err != nil {
			return preparedReduction{}, refusal{Failed, "the correlation rules could not be canonicalized"}
		}
		sum := sha256.Sum256(encoded)
		rulesDigest = hex.EncodeToString(sum[:])
	}
	if grouping == reduce.GroupByCorrelation && rulesDigest == "" {
		return preparedReduction{}, refusal{Failed, "a correlation grouping names the rules whose relations form the groups"}
	}
	if grouping == reduce.GroupPerOccurrence && rulesDigest != "" {
		return preparedReduction{}, refusal{Failed, "this plan groups per occurrence; a reduction applies the correlation rules a plan named or none"}
	}
	plan := reduce.Plan{
		Schema:        reduce.PlanSchema,
		Case:          source.Identity,
		Grouping:      grouping,
		Rules:         rulesDigest,
		Signature:     reduce.Signature{State: durablerun.AssertionFailed, Assertions: request.Assertions},
		Trials:        request.Trials,
		Confirmations: request.Confirmations,
	}
	reset := fixturereset.Request{
		Target: target, PlanBytes: planBytes, PlanDirectory: filepath.Dir(resolvedPlan),
		Policy: policy, Confirmed: request.Confirmed,
	}
	workPath := ""
	if createWork {
		if err := artifactpath.EntryName(request.Work); err != nil {
			return preparedReduction{}, refusal{Failed, "reduction working material must be named by one new entry of the open workspace"}
		}
		workPath = artifactpath.JoinReference(root, request.Work)
	} else {
		workPath = filepath.Join(root, ".readmit-reduction-preview")
		_ = os.RemoveAll(workPath)
	}
	oracle, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: specPath, Workspace: workPath, Signature: plan.Signature,
		Reset: reset, Resolve: sendpolicy.SystemResolver,
	})
	if err != nil {
		return preparedReduction{}, refusal{Failed, err.Error()}
	}
	if !createWork {
		_ = os.RemoveAll(workPath)
	}
	casePath := artifactpath.JoinReference(root, request.Case)
	prepared := preparedReduction{
		request: reduce.Request{
			Case: casePath, Plan: plan, Rules: rules,
			Messages: oracle.Messages(), Required: oracle.Required(),
		},
	}
	if createWork {
		prepared.oracle = oracle
	}
	return prepared, refusal{}
}
