package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/replay"
)

// ConnectedActionOptions select local compiled engine inputs. The existing
// catalog/review caller supplies these entries; no second approval is issued.
type ConnectedActionOptions struct {
	Plan       string `json:"plan"`
	Policy     string `json:"policy"`
	Credential string `json:"credential,omitzero"`
}
type connectedBinding struct {
	prepared     *connectedtransport.Prepared
	root, output string
}
type executionReview struct {
	token, intent string
	expires       time.Time
}

func bindConnectedSend(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if request.Replay == nil || request.Replay.Connected == nil || len(request.Items) != 1 || request.Items[0].Kind != CaseItem || request.Destination == nil || request.Destination.Kind != EnvironmentItem || len(request.Replay.Messages) > 0 || len(request.Replay.Transformations) > 0 || request.Replay.Policy != "" {
		return nil, refusal{Failed, "a connected send uses its compiled messages and one named case and environment"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, []ItemRef{request.Items[0], *request.Destination})
	if loaded == nil {
		return nil, declined
	}
	opts := request.Replay.Connected
	planPath, err := artifactpath.Child(loaded.root, opts.Plan)
	if err != nil {
		return nil, refusal{Failed, "the compiled plan is unavailable"}
	}
	plan, err := connectedtest.OpenPlan(planPath)
	if err != nil {
		return nil, refusal{Failed, "the compiled plan cannot be verified"}
	}
	casePath, err := artifactpath.Child(loaded.root, records[0].Entry)
	if err != nil {
		return nil, refusal{Failed, "the named case is unavailable"}
	}
	targetPath, err := artifactpath.File(loaded.root, backingEntry(records[1]))
	if err != nil {
		return nil, refusal{Failed, "the named environment is unavailable"}
	}
	policyPath, err := artifactpath.File(loaded.root, opts.Policy)
	if err != nil {
		return nil, refusal{Failed, "the scoped policy is unavailable"}
	}
	credential := ""
	if opts.Credential != "" {
		credential, err = artifactpath.File(loaded.root, opts.Credential)
		if err != nil {
			return nil, refusal{Failed, "the credential scope is unavailable"}
		}
	}
	prepared, err := connectedtransport.Prepare(plan, connectedtransport.Selection{Case: casePath, Target: targetPath, Policy: policyPath, Credential: credential})
	if err != nil {
		return nil, refusal{Failed, "the selected objects differ from the compiled action"}
	}
	destination, declined := destinationFor(loaded.root, "", "connected")
	if destination.Name == "" {
		return nil, declined
	}
	target, messages := prepared.Preview()
	preview := &ReplayPreview{Case: records[0].Entry, Identity: prepared.Binding().Plan, SourceIdentity: prepared.Binding().Source, Target: targetView(target), Messages: []ReplayMessage{}, Changes: []ReplayChange{}, Transformations: []replay.Transformation{}, Destination: destination, DecisionFile: filepath.Join(destination.Name, "decision.json"), Sendable: destination.Fresh, Admission: RunAdmission{Admitted: true}}
	if !held {
		preview.Admission = a.admissionPreview(ctx)
		preview.Sendable = preview.Sendable && preview.Admission.Admitted
	}
	for _, m := range messages {
		preview.Messages = append(preview.Messages, ReplayMessage{Source: m.Source, Outbound: m.Outbound, WireBytes: m.WireBytes})
	}
	raw, _ := json.Marshal(prepared.Binding(), json.Deterministic(true))
	return &boundAction{action: ReplaySendAction, origin: request, binding: binding(loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, true, held), string(raw), destination.Name), connected: &connectedBinding{prepared: prepared, root: loaded.root, output: destination.Name}, review: ActionReview{Items: items, Ready: preview.Sendable, Replay: preview, Destination: ReviewDestination{Name: target.Name, Classification: string(target.Classification), Address: target.Address, Output: destination.Name}}}, refusal{}
}

type connectedReviewAuthority struct {
	app              *App
	bound            *boundAction
	actor            networkaction.Actor
	reviewer, policy string
}

func (a connectedReviewAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	deny := errors.New("the connected action review is no longer authorized")
	if ctx.Err() != nil || a.bound.executionReview == nil || a.bound.connected == nil || b != a.bound.connected.prepared.Binding() {
		return networkaction.Actor{}, deny
	}
	lease := a.bound.executionReview
	store := &a.app.reviews
	store.mu.Lock()
	held := store.reviews[lease.token]
	valid := held != nil && !held.withdrawn && held.consumed == lease.intent && store.running == lease.intent && a.app.now().Before(held.expires)
	store.mu.Unlock()
	if !valid || a.app.reviewer() != a.reviewer || a.app.policyBinding(ctx, true, true) != a.policy {
		return networkaction.Actor{}, deny
	}
	guard, _ := a.app.selectedOperation()
	if guard.CheckExecutionContext(ctx) != nil {
		return networkaction.Actor{}, deny
	}
	// Rebinding reads only local inputs; the already reserved output is excluded
	// from this comparison because reserving it is this action's own effect.
	fresh, declined := bindConnectedSend(a.app, ctx, a.bound.origin, true)
	if fresh == nil || declined.reason != "" || fresh.connected.prepared.Binding() != b {
		return networkaction.Actor{}, deny
	}
	return a.actor, nil
}
func executeConnectedSend(a *App, ctx context.Context, bound *boundAction) ReviewedActionResult {
	if bound.executionReview == nil {
		return ReviewedActionResult{State: Failed, Outcome: ActionRefused, Reason: "the connected send requires its final reviewed action"}
	}
	lease := bound.executionReview
	actor := networkaction.Actor{Kind: "action-review", ID: "reviewer-" + binding(a.reviewer())[:16], Generation: "review-" + binding(lease.token)[:16], EvidenceIdentity: bound.binding, Expires: lease.expires}
	authority := connectedReviewAuthority{app: a, bound: bound, actor: actor, reviewer: a.reviewer(), policy: a.policyBinding(ctx, true, true)}
	output := filepath.Join(bound.connected.root, bound.connected.output)
	receipt, err := connectedtransport.Execute(ctx, bound.connected.prepared, authority, "desktop-"+binding(lease.intent)[:16], output, nil)
	if err != nil {
		return ReviewedActionResult{State: Failed, Outcome: ActionUncertain, Reason: "the connected action stopped; retained evidence must be inspected and nothing is resent automatically"}
	}
	run, err := replay.Open(filepath.Join(output, "run"))
	if err != nil {
		return ReviewedActionResult{State: Failed, Outcome: ActionUncertain, Reason: "the connected run cannot be verified"}
	}
	result := ReviewedActionResult{State: Completed, Outcome: ActionCompleted, Replay: replayRunView(run, bound.connected.output, filepath.Join(bound.connected.output, "decision.json"))}
	if receipt.State != "settled" {
		result.Outcome = ActionUncertain
	}
	return result
}
func connectedRunEvidence(path string, request RunEvidenceRequest) RunEvidenceResult {
	receipt, err := connectedtransport.Open(path)
	if err != nil {
		return RunEvidenceResult{State: Failed, Reason: "the connected transport evidence cannot be verified"}
	}
	run, err := replay.Open(filepath.Join(path, "run"))
	if err != nil {
		return RunEvidenceResult{State: Failed, Reason: "the connected run cannot be verified"}
	}
	e := &RunEvidence{Entry: request.Entry, Terminal: true, RunState: receipt.State, Status: receipt.ApplicationVerdict, Identity: receipt.RunIdentity, SpecIdentity: receipt.Binding.Plan, SourceIdentity: receipt.Binding.Source, Boundary: "transport-only", Planned: len(run.Events), Messages: []RunMessageEvidence{}, Assertions: []RunAssertionEvidence{}, Gaps: []string{"transport receipts do not establish downstream application correctness"}, StartedAt: run.Manifest.StartedAt.Format(time.RFC3339Nano), CompletedAt: run.Manifest.CompletedAt.Format(time.RFC3339Nano)}
	for _, event := range run.Events {
		readable := event.Received.Size > 0
		e.Messages = append(e.Messages, RunMessageEvidence{Source: event.SourceOccurrence, Outbound: event.OutboundOccurrence, Response: filepath.Join("run", event.Received.Path), Readable: readable})
		if readable {
			e.Readable++
		}
		switch event.Delivery {
		case "acknowledged":
			e.Acknowledged++
		case "uncertain":
			e.Uncertain++
		default:
			e.NotAttempted++
		}
	}
	e.Unreadable = e.Planned - e.Readable
	e.DeliveryUncertain = e.Uncertain > 0
	return RunEvidenceResult{State: Completed, Evidence: e}
}
