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
// writing into the workspace. The durable oracle is built only to resolve the
// sequence and signature pins the chosen assertions name, in a private folder
// outside the workspace that is removed before the preview answers.
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
			declined := admissionRefusal(ctx, admissionErr)
			return ReductionResult{State: declined.state, Reason: declined.reason}
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
		// A working folder no trial wrote into holds nothing: the engine
		// refused the plan before its first trial, a reset let no trial run,
		// or the reduction was stopped before one. It is removed rather than
		// left to refuse the next attempt under the same name; os.Remove
		// leaves a folder any trial wrote into.
		emptied := os.Remove(prepared.work) == nil
		if err != nil {
			return ReductionResult{State: Failed, Reason: err.Error()}
		}
		view := &ReductionView{
			Case: request.Case, Spec: request.Spec, Work: request.Work,
			Report: &report, Boundary: report.Scope, Observation: reductionObservation,
		}
		if emptied {
			view.Work = ""
		}
		// A person who stopped the reduction stopped it, whichever way the
		// trial it interrupted then ended: a send cancelled while it waited on
		// an acknowledgement ends that trial as an uncertain delivery, which
		// the report names. A stop that arrived after the reduction had
		// reached an outcome stopped nothing, and the outcome stands.
		if report.Outcome == reduce.OutcomeUndecided && (ctx.Err() != nil || report.Reason == reduce.OperatorStopped) {
			return ReductionResult{State: Cancelled, Reason: "reduction stopped; retained trials were not resent", Reduction: view}
		}
		return ReductionResult{State: Completed, Reduction: view}
	})
}

type preparedReduction struct {
	request reduce.Request
	oracle  *reduce.DurableOracle
	// work is the working folder a run created, and empty for a preview.
	work string
}

func (a *App) prepareReduction(request ReductionRequest, createWork bool) (preparedReduction, refusal) {
	root, source, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return preparedReduction{}, declined
	}
	// Every document a reduction reads is one the panel offered from the
	// workspace listing, so each is held to the listing's rule here, before it
	// is read: one regular file of the open workspace, never a symbolic link.
	specPath, err := artifactpath.File(root, request.Spec)
	if err != nil {
		return preparedReduction{}, refusal{Failed, "the regression test must be one regular file of the open workspace, never a symbolic link"}
	}
	targetPath, err := artifactpath.File(root, request.Target)
	if err != nil {
		return preparedReduction{}, refusal{Failed, "the approved environment must be one regular file of the open workspace, never a symbolic link"}
	}
	target, err := operation.ReadTarget(targetPath)
	if err != nil {
		return preparedReduction{}, refusal{Failed, err.Error()}
	}
	planPath, err := artifactpath.File(root, request.ResetPlan)
	if err != nil {
		return preparedReduction{}, refusal{Failed, "the reviewed reset plan must be one regular file of the open workspace, never a symbolic link"}
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
		polPath, err := artifactpath.File(root, request.Policy)
		if err != nil {
			return preparedReduction{}, refusal{Failed, "the approved-destination policy must be one regular file of the open workspace, never a symbolic link"}
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
		// A preview spends no trial. The oracle it builds only to read the
		// sequence and the occurrences the signature pins works in a private
		// folder outside the workspace, removed before the preview answers,
		// so a preview writes nothing into the folder the person opened and
		// never removes an entry of it.
		scratch, err := os.MkdirTemp("", "readmit-reduction-preview-")
		if err != nil {
			return preparedReduction{}, refusal{Failed, "cannot prepare the reduction preview"}
		}
		defer os.RemoveAll(scratch)
		workPath = filepath.Join(scratch, "trials")
	}
	oracle, err := reduce.NewDurableOracle(reduce.OracleRequest{
		SpecPath: specPath, Workspace: workPath, Signature: plan.Signature,
		Reset: reset, Resolve: sendpolicy.SystemResolver,
	})
	if err != nil {
		return preparedReduction{}, refusal{Failed, err.Error()}
	}
	casePath := artifactpath.JoinReference(root, request.Case)
	prepared := preparedReduction{
		request: reduce.Request{
			Case: casePath, Plan: plan, Rules: rules,
			Messages: oracle.Messages(), Required: oracle.Required(),
		},
	}
	if createWork {
		prepared.oracle, prepared.work = oracle, workPath
	}
	return prepared, refusal{}
}
