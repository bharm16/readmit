package desktop

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The reexecution step of the privacy review. `readmit redact reexecute` runs
// one reviewed transformed phase against the same explicitly recorded target
// an actual original execution used, and this file connects that operation to
// the window without adding a gate of its own: the preview is the command's
// own preparation, which opens no connection, and the send is the command's
// own execution through the durable runner, taken only under an explicit
// authorization of that one send and execution admission asked exactly as the
// command line asks it. Every refusal — an unapproved or changed review, a
// target that is not the one the original execution recorded, a
// production-classified target, a private linkage that differs — is the
// operation's own sentence. Nothing here resets a target, retries a send or
// resumes a phase, and the private local state is only handed to the
// operation, never read here.

// reexecutionOperation is the name a reexecution sends under: a panel cancels
// it through this name, and the privacy status reports its activity active
// while it holds the slot.
const reexecutionOperation = "reexecution"

// reexecutionBoundaries is what the step states before and after anybody acts
// on it. Every sentence is a fact about this build.
var reexecutionBoundaries = []string{
	"The preview sends nothing. A send happens only after you authorize this one execution, and only to the target the actual original execution recorded.",
	"Nothing here resets the target, retries a send or resumes an interrupted phase: perform the reset the specification declares yourself before authorizing, and reconcile any uncertain delivery at the target before a separately authorized new attempt.",
	"A matched phase is not external equivalence: target software, reset equivalence and repeat stability stay unverified, and every assessment declines the claim.",
	"New acknowledgements, observations and metadata stay customer-local; the review approved none of them for sharing, and they need a fresh disclosure review first.",
}

// ReexecutionRequest names what `readmit redact reexecute` reads, each one
// entry of the open workspace: the approved review, the private local state
// its derivation wrote, the retained packet whose current run is the actual
// original phase, the rebound execution specification, the phase, and the
// exact review identity the person types now. Output names the new job folder
// a send would write; empty proposes the next free generated name.
type ReexecutionRequest struct {
	Workspace      string `json:"workspace"`
	Review         string `json:"review"`
	LocalState     string `json:"local_state"`
	OriginalPacket string `json:"original_packet"`
	Spec           string `json:"spec"`
	Phase          string `json:"phase"`
	Approval       string `json:"approval"`
	Output         string `json:"output,omitzero"`
}

// ReexecutionPreview is what one send would do, read out of the command's own
// preparation: the assessment binding it would record, the target it would
// connect to, the occurrences of the approved derived case it would send, the
// fresh job folder, and whether execution would be admitted, asked the way the
// send asks it. Identity pins all of it: a send is refused unless the inputs
// still prepare to exactly this preview.
type ReexecutionPreview struct {
	Identity     string                       `json:"identity"`
	Assessment   redact.ReexecutionAssessment `json:"assessment"`
	Target       RunTargetView                `json:"target"`
	Selected     []RunSelected                `json:"selected"`
	InitialState string                       `json:"initial_state"`
	Reset        string                       `json:"reset,omitzero"`
	Destination  RunDestination               `json:"destination"`
	Admission    RunAdmission                 `json:"admission"`
	Limitations  []string                     `json:"limitations"`
}

// ReexecutionPreviewResult carries one state. Preview is present whenever the
// operation prepared the execution; a refusal is Failed with the operation's
// own sentence, never a partial preview that reads as approved.
type ReexecutionPreviewResult struct {
	State   State               `json:"state"`
	Reason  string              `json:"reason,omitzero"`
	Preview *ReexecutionPreview `json:"preview,omitzero"`
}

func (r *ReexecutionPreviewResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// PreviewReexecution prepares one reexecution exactly as `readmit redact
// reexecute` does without --send: it verifies the approval against the
// review's identity now, rechecks the private linkage, opens the original
// packet and the rebound specification, and binds the target and the derived
// case, all locally. It opens no network connection, sends nothing, resets
// nothing and writes nothing.
func (a *App) PreviewReexecution(request ReexecutionRequest) ReexecutionPreviewResult {
	return run(a, false, false, func(ctx context.Context) ReexecutionPreviewResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ReexecutionPreviewResult{State: declined.state, Reason: declined.reason}
		}
		plan, refused := prepareReexecution(ctx, root, request)
		if refused.state != "" {
			return ReexecutionPreviewResult{State: refused.state, Reason: refused.reason}
		}
		preview, err := reexecutionPreview(plan)
		if err != nil {
			return ReexecutionPreviewResult{State: Failed, Reason: "the rebound specification could not be read back"}
		}
		preview.Destination, refused = destinationFor(root, request.Output, "reexecution")
		if refused.state != "" {
			return ReexecutionPreviewResult{State: refused.state, Reason: refused.reason}
		}
		preview.Admission = a.admissionPreview(ctx)
		return ReexecutionPreviewResult{State: Completed, Preview: &preview}
	})
}

// ReexecutionSendRequest is one deliberate send: everything a preview named,
// the preview identity the person reviewed, the new job folder, and Authorize,
// the explicit authorization of this single nonproduction send that `--send`
// is on the command line. Without it nothing is sent.
type ReexecutionSendRequest struct {
	Workspace      string `json:"workspace"`
	Review         string `json:"review"`
	LocalState     string `json:"local_state"`
	OriginalPacket string `json:"original_packet"`
	Spec           string `json:"spec"`
	Phase          string `json:"phase"`
	Approval       string `json:"approval"`
	Output         string `json:"output"`
	Expected       string `json:"expected_identity"`
	Authorize      bool   `json:"authorize"`
}

// ReexecutionOutcome is one send as the operation assessed it: the job folder
// it retained, the operation's own assessment — `matched`, `changed` or
// `unavailable-or-unstable`, always declining external equivalence — and what
// a read-only recovery of the job establishes now about every delivery.
type ReexecutionOutcome struct {
	Job         string                       `json:"job"`
	Assessment  redact.ReexecutionAssessment `json:"assessment"`
	Retained    *RunProgress                 `json:"retained,omitzero"`
	Limitations []string                     `json:"limitations"`
}

// ReexecutionResult carries one state. Completed means the send retained a
// decided result the operation assessed as matched. A changed phase is
// refused, Failed, as the command line refuses it with exit status 2, and a
// cancelled, timed-out, interrupted or delivery-uncertain send is Cancelled or
// Failed; each still carries the outcome so what the job retained is read.
type ReexecutionResult struct {
	State   State               `json:"state"`
	Reason  string              `json:"reason,omitzero"`
	Outcome *ReexecutionOutcome `json:"outcome,omitzero"`
}

func (r *ReexecutionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReexecuteReviewedEvidence sends once, exactly as `readmit redact reexecute
// --send --output` does: under an explicit authorization of this one send,
// under execution admission, and only while the inputs still prepare to the
// preview the person reviewed. The operation re-verifies everything before
// the first byte, sends through the durable runner into a new job folder, and
// assesses what it retained. Cancelling stops further sends; whatever may
// already have been delivered stays uncertain in the job and is never resent.
func (a *App) ReexecuteReviewedEvidence(request ReexecutionSendRequest) ReexecutionResult {
	return runNamed[ReexecutionResult, *ReexecutionResult](a, reexecutionOperation, true, false, func(ctx context.Context) (out ReexecutionResult) {
		if !request.Authorize {
			return ReexecutionResult{State: Failed, Reason: "a reexecution sends only under an explicit authorization of this single send; the preview sent nothing"}
		}
		if request.Expected == "" {
			return ReexecutionResult{State: Failed, Reason: "preview the reexecution first; a send executes only the preview a person reviewed"}
		}
		settle, admissionErr := a.admitExecution(ctx)
		if admissionErr != nil {
			declined := admissionRefusal(ctx, admissionErr)
			return ReexecutionResult{State: declined.state, Reason: declined.reason}
		}
		defer func() {
			if err := settle(); err != nil {
				out.State, out.Reason = Failed, settlementFailed
			}
		}()
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ReexecutionResult{State: declined.state, Reason: declined.reason}
		}
		plan, refused := prepareReexecution(ctx, root, request.previewed())
		if refused.state != "" {
			return ReexecutionResult{State: refused.state, Reason: refused.reason}
		}
		if preview, err := reexecutionPreview(plan); err != nil || preview.Identity != request.Expected {
			return ReexecutionResult{State: Failed, Reason: "the reexecution inputs changed after the preview; preview again and authorize the send it shows"}
		}
		destination, refused := destinationFor(root, request.Output, "reexecution")
		if refused.state != "" {
			return ReexecutionResult{State: refused.state, Reason: refused.reason}
		}
		output := filepath.Join(root, destination.Name)
		// The job is named while it is written, so the run panel's progress
		// read tells a send in flight from one a crash interrupted.
		a.setRunOutput(output)
		defer a.setRunOutput("")
		bounded, cancel := context.WithTimeout(ctx, operationguard.MaxDuration)
		defer cancel()
		assessment, err := plan.Execute(bounded, output)
		var outcome *ReexecutionOutcome
		if _, statErr := os.Lstat(output); statErr == nil {
			outcome = &ReexecutionOutcome{Job: destination.Name, Assessment: assessment, Retained: retainedJob(output), Limitations: reexecutionBoundaries}
		}
		return answerReexecution(ctx.Err() != nil, assessment, outcome, err)
	})
}

// previewed is the part of a send its preview named.
func (r ReexecutionSendRequest) previewed() ReexecutionRequest {
	return ReexecutionRequest{Workspace: r.Workspace, Review: r.Review, LocalState: r.LocalState,
		OriginalPacket: r.OriginalPacket, Spec: r.Spec, Phase: r.Phase, Approval: r.Approval}
}

// answerReexecution is the one state a send ends in. Only a matched
// assessment completes; any other is refused as the command line refuses it,
// with the assessment's own reason, and the outcome is still carried so what
// the job retained is read. A cancellation is its own state, and whatever it
// may have delivered is never resent.
func answerReexecution(cancelled bool, assessment redact.ReexecutionAssessment, outcome *ReexecutionOutcome, err error) ReexecutionResult {
	switch {
	case cancelled && outcome == nil:
		// A job exists before the first byte is sent, so without one nothing
		// was sent.
		return ReexecutionResult{State: Cancelled, Reason: "the reexecution was cancelled before anything was sent; no job was created"}
	case cancelled:
		return ReexecutionResult{State: Cancelled, Outcome: outcome,
			Reason: "the reexecution was cancelled; the job retains what happened, and whatever may already have been delivered is never resent — reconcile it at the target before a separately authorized new attempt"}
	case err != nil:
		state, reason := privacyRefusal(err)
		return ReexecutionResult{State: state, Reason: reason, Outcome: outcome}
	case assessment.Criteria == "matched":
		return ReexecutionResult{State: Completed, Outcome: outcome}
	default:
		return ReexecutionResult{State: Failed, Reason: assessment.Reason, Outcome: outcome}
	}
}

// prepareReexecution resolves the request's entries in the open workspace and
// prepares the execution through the operation itself. The refusals here name
// the entry that was wrong so a fix is a selection; the operation's own
// refusals stay the operation's sentences.
func prepareReexecution(ctx context.Context, root string, request ReexecutionRequest) (*redact.Reexecution, refusal) {
	reviewPath, err := runEntryPath(root, request.Review)
	if err != nil {
		return nil, refusal{Failed, "the approved review must be one entry of the open workspace"}
	}
	privatePath, err := artifactpath.Child(root, request.LocalState)
	if err != nil {
		return nil, refusal{Failed, "the private local state must be one folder of the open workspace, never a symbolic link"}
	}
	packetPath, err := runEntryPath(root, request.OriginalPacket)
	if err != nil {
		return nil, refusal{Failed, "the original packet must be one entry of the open workspace"}
	}
	specPath, err := artifactpath.File(root, request.Spec)
	if err != nil {
		return nil, refusal{Failed, "the rebound execution specification must be one regular entry of the open workspace"}
	}
	plan, err := redact.PrepareReexecution(ctx, redact.ReexecutionRequest{
		ReviewPath: reviewPath, LocalState: privatePath, Approval: request.Approval,
		OriginalPacket: packetPath, SpecPath: specPath, Phase: request.Phase,
	})
	if err != nil {
		state, reason := privacyRefusal(err)
		return nil, refusal{state, reason}
	}
	return plan, refusal{}
}

// reexecutionPreview projects one prepared execution and pins it: the
// identity is the digest of everything the preview shows about what a send
// would do, so a changed review, packet, specification, case or target
// refuses the send rather than executing inputs nobody reviewed.
func reexecutionPreview(plan *redact.Reexecution) (ReexecutionPreview, error) {
	inputs := plan.PinnedInputs()
	spec, err := testrunner.DecodeSpec(inputs.Spec)
	if err != nil {
		return ReexecutionPreview{}, err
	}
	preview := ReexecutionPreview{
		Assessment:   plan.Preview(),
		Target:       targetView(inputs.Configuration),
		Selected:     selected(inputs.Mappings),
		InitialState: spec.Setup.InitialState,
		Reset:        spec.Setup.ResetInstructions,
		Limitations:  reexecutionBoundaries,
	}
	pinned, err := json.Marshal(struct {
		Assessment   redact.ReexecutionAssessment
		Target       RunTargetView
		Selected     []RunSelected
		InitialState string
		Reset        string
	}{preview.Assessment, preview.Target, preview.Selected, preview.InitialState, preview.Reset}, json.Deterministic(true))
	if err != nil {
		return ReexecutionPreview{}, err
	}
	preview.Identity = digestOf(pinned)
	return preview, nil
}

// retainedJob is the read-only recovery read of the job a send wrote, the one
// `readmit run status --recovery` performs, or nil when the job it left cannot
// be verified — an interrupted write is incomplete, never a result.
func retainedJob(path string) *RunProgress {
	recovery, err := durablerun.Recover(path)
	if err != nil {
		return nil
	}
	progress := progressOf(recovery)
	return &progress
}
