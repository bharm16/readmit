package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// The replay screen is `readmit replay` in the window: it previews selected
// messages of the verified case against one target configuration and, only
// once that preview is explicitly approved, sends them once. Both halves go
// through operation.PrepareReplay, the path the command takes, and a send
// through replay.ExecuteWithPolicy, which decides the send policy again at the
// point of the send and retains that decision before anything is opened. The
// run and its decision are written as new entries of the open workspace, as
// the command writes them. Nothing here retries, resumes or resends: a new
// send is always a new preview, a new approval and a new run folder.

const (
	// replayPreviewOperation is the name a replay preview holds the slot
	// under. It opens no connection; with a send policy selected it resolves
	// the target's host name to reach the decision a send would get, so the
	// privacy status reports it with the send-policy evaluations.
	replayPreviewOperation = "replay-preview"
	// replayOperation is the name a replay's send holds the slot under, and
	// the name its cancel gives.
	replayOperation = "replay"
	// replayDecisionSuffix names the decision a send retains beside its run
	// folder, as `readmit replay --send` does when no --decision is given.
	replayDecisionSuffix = ".decision.json"
)

// ReplayRequest is one replay of the verified case, as `readmit replay` takes
// it: the case the window verified and the identity it verified, the target
// configuration and the optional send policy (entries of the open workspace),
// the selected message occurrences (none selects every message, in source
// order), the named transformations, and the new run folder a send writes.
// Output empty asks a preview to propose the next free name. Reveal is the
// deliberate local reveal of the values a transformation changes.
type ReplayRequest struct {
	Workspace       string                  `json:"workspace"`
	Case            string                  `json:"case"`
	Identity        string                  `json:"identity"`
	Target          string                  `json:"target"`
	Policy          string                  `json:"policy,omitzero"`
	Messages        []string                `json:"messages"`
	Transformations []replay.Transformation `json:"transformations"`
	Output          string                  `json:"output,omitzero"`
	Reveal          bool                    `json:"reveal"`
}

// ReplaySendRequest is one explicitly approved send of what one preview
// showed. Expected is the identity that preview fixed: a case, target, policy,
// selection or transformation that changed since is refused rather than sent
// as though nothing had. Approved is the person's explicit approval; without
// it nothing is admitted, decided or sent.
type ReplaySendRequest struct {
	Replay   ReplayRequest `json:"replay"`
	Expected string        `json:"expected_identity"`
	Approved bool          `json:"approved"`
}

// ReplayResult carries one state. Decision is the send decision reached
// whenever one was, including beside a refusal: a preview's, or the one a send
// retained. Preview is present for a completed preview and Run for a send that
// wrote a run, including a cancelled one.
type ReplayResult struct {
	State    State                `json:"state"`
	Reason   string               `json:"reason,omitzero"`
	Decision *sendpolicy.Decision `json:"decision,omitzero"`
	Preview  *ReplayPreview       `json:"preview,omitzero"`
	Run      *ReplayRun           `json:"run,omitzero"`
}

func (r *ReplayResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReplayPreview is what one send would do, read out of the sealed plan it
// would execute: the messages in the order they would leave and the bytes
// each would put on the wire, every field a transformation changes, the
// target, the fresh run folder and the decision file beside it, and the
// operation guard's own admission. It is local: nothing was sent or written.
// Sendable is the backend's answer to whether a send of exactly this would be
// attempted, and Refusal says why not.
type ReplayPreview struct {
	Case            string                  `json:"case"`
	Identity        string                  `json:"identity"`
	SourceIdentity  string                  `json:"source_identity"`
	Target          RunTargetView           `json:"target"`
	Messages        []ReplayMessage         `json:"messages"`
	Transformations []replay.Transformation `json:"transformations"`
	Changes         []ReplayChange          `json:"changes"`
	Destination     RunDestination          `json:"destination"`
	DecisionFile    string                  `json:"decision_file"`
	Admission       RunAdmission            `json:"admission"`
	Sendable        bool                    `json:"sendable"`
	Refusal         string                  `json:"refusal,omitzero"`
	Revealed        bool                    `json:"revealed"`
}

// ReplayMessage is one selected message as the command's preview lists it.
type ReplayMessage struct {
	Source    string `json:"source"`
	Outbound  string `json:"outbound"`
	WireBytes int    `json:"wire_bytes"`
}

// ReplayChange is one field a named transformation changes, as the run's
// manifest will record it. Old and New are the field's bytes before and after,
// present only under the deliberate reveal.
type ReplayChange struct {
	Transformation string `json:"transformation"`
	Source         string `json:"source"`
	Outbound       string `json:"outbound"`
	Selector       string `json:"selector"`
	OldState       string `json:"old_state"`
	NewState       string `json:"new_state"`
	Old            string `json:"old,omitzero"`
	New            string `json:"new,omitzero"`
}

// ReplayRun is one retained run as the command's summary reports it: where it
// and its decision were written, its identity and contract, and every selected
// message's outcome and delivery. Uncertain counts the deliveries no
// correlated acknowledgement settled; none of them is ever sent again.
type ReplayRun struct {
	Output               string          `json:"output"`
	DecisionFile         string          `json:"decision_file"`
	Identity             string          `json:"identity"`
	Schema               string          `json:"schema"`
	State                string          `json:"state"`
	ContainsSourceValues bool            `json:"contains_source_values"`
	ExportPolicy         string          `json:"export_policy"`
	Successful           bool            `json:"successful"`
	Uncertain            int             `json:"uncertain"`
	Messages             []ReplayOutcome `json:"messages"`
}

// ReplayOutcome is one selected message and what its send established.
type ReplayOutcome struct {
	Source        string `json:"source"`
	Outbound      string `json:"outbound"`
	Outcome       string `json:"outcome"`
	Delivery      string `json:"delivery"`
	SentBytes     int    `json:"sent_bytes"`
	ReceivedBytes int    `json:"received_bytes"`
	ACK           string `json:"ack"`
	Correlation   string `json:"correlation"`
	Elapsed       string `json:"elapsed"`
	ErrorClass    string `json:"error_class,omitzero"`
	ErrorPhase    string `json:"error_phase,omitzero"`
}

// PreviewReplay reports what one replay would send without sending it: the
// dry run `readmit replay` prints, from the same shared preparation. It reads
// the verified case, the target configuration, the send policy and the
// selected messages and transformations, and asks the send policy its
// question without requesting a send. It opens no connection and writes
// nothing — not even the decision, which a send retains beside its run.
func (a *App) PreviewReplay(request ReplayRequest) ReplayResult {
	return runNamed[ReplayResult, *ReplayResult](a, profiles["PreviewReplay"], func(ctx context.Context) ReplayResult {
		inputs, declined := replayInputsOf(request)
		if declined.state != "" {
			return ReplayResult{State: declined.state, Reason: declined.reason}
		}
		destination, declined := replayDestination(inputs.root, request.Output)
		if declined.state != "" {
			return ReplayResult{State: declined.state, Reason: declined.reason}
		}
		var decision sendpolicy.Decision
		plan, err := operation.PrepareReplay(ctx, inputs.casePath, inputs.target, inputs.options, inputs.policy, false,
			func(value sendpolicy.Decision) error { decision = value; return nil }, sendpolicy.SystemResolver)
		if ctx.Err() != nil {
			// A cancelled lookup reads as an unresolvable destination; it is
			// the person's cancellation, and nothing was decided.
			return ReplayResult{State: Cancelled, Reason: cancelledRefusal.reason}
		}
		if err != nil {
			return ReplayResult{State: Failed, Reason: err.Error(), Decision: decisionReached(decision)}
		}
		if plan.SourceIdentity() != request.Identity {
			return ReplayResult{State: Failed, Reason: operation.ErrCaseIdentityChanged.Error()}
		}
		identity, err := replayIdentity(plan, inputs)
		if err != nil {
			return ReplayResult{State: Failed, Reason: "the replay plan could not be identified"}
		}
		preview := ReplayPreview{
			Case: request.Case, Identity: identity, SourceIdentity: plan.SourceIdentity(),
			Target: targetView(plan.Configuration()), Messages: []ReplayMessage{},
			Transformations: slices.Clone(inputs.options.Transformations), Changes: []ReplayChange{},
			Destination: destination, DecisionFile: destination.Name + replayDecisionSuffix, Revealed: request.Reveal,
		}
		if preview.Transformations == nil {
			preview.Transformations = []replay.Transformation{}
		}
		for _, mapping := range plan.Mappings() {
			wire, err := plan.Outbound(mapping.OutboundOccurrence)
			if err != nil {
				return ReplayResult{State: Failed, Reason: "the replay plan could not be read back"}
			}
			preview.Messages = append(preview.Messages, ReplayMessage{Source: mapping.SourceOccurrence, Outbound: mapping.OutboundOccurrence, WireBytes: len(wire)})
		}
		for _, change := range plan.Changes() {
			row := ReplayChange{Transformation: change.Transformation, Source: change.SourceOccurrence, Outbound: change.OutboundOccurrence,
				Selector: change.Selector, OldState: string(change.OldState), NewState: string(change.NewState)}
			if request.Reveal {
				row.Old, row.New = strings.ToValidUTF8(string(change.Old), "�"), strings.ToValidUTF8(string(change.New), "�")
			}
			preview.Changes = append(preview.Changes, row)
		}
		preview.Admission = a.admissionPreview(ctx)
		switch {
		case decision.Reason != sendpolicy.SendNotExplicit:
			preview.Refusal = "the send policy refuses this destination (" + string(decision.Reason) + "); a send is refused before anything is sent"
		case !preview.Admission.Admitted:
			preview.Refusal = preview.Admission.Reason
		case !destination.Fresh:
			preview.Refusal = destination.Reason
		default:
			preview.Sendable = true
		}
		return ReplayResult{State: Completed, Decision: &decision, Preview: &preview}
	})
}

// SendReplay sends what one preview showed, once, after the person approved
// it. Approval is checked before anything else, then the operation guard
// admits execution as `readmit replay --send` is admitted, then the inputs are
// read and prepared again and must still be exactly what the preview
// identified. The send policy is decided again at the point of the send and
// the decision is retained beside the run folder before any connection is
// opened, including a denial. Cancel stops at the message in flight: later
// messages are recorded as not attempted and nothing is ever sent again.
func (a *App) SendReplay(request ReplaySendRequest) ReplayResult {
	approved := func() (ReplayResult, bool) {
		if !request.Approved {
			return ReplayResult{State: Failed, Reason: "a replay sends only once its preview is explicitly approved; nothing was sent"}, false
		}
		if request.Expected == "" || request.Replay.Output == "" {
			return ReplayResult{State: Failed, Reason: "a replay sends only what its preview showed, into the run folder it named; preview it before sending. Nothing was sent"}, false
		}
		return ReplayResult{}, true
	}
	return runNamedChecked[ReplayResult, *ReplayResult](a, profiles["SendReplay"], approved, func(ctx context.Context) ReplayResult {
		inputs, declined := replayInputsOf(request.Replay)
		if declined.state != "" {
			return ReplayResult{State: declined.state, Reason: declined.reason}
		}
		destination, declined := replayDestination(inputs.root, request.Replay.Output)
		if declined.state != "" {
			return ReplayResult{State: declined.state, Reason: declined.reason}
		}
		if !destination.Fresh {
			return ReplayResult{State: Failed, Reason: destination.Reason + ". Nothing was sent"}
		}
		output := filepath.Join(inputs.root, destination.Name)
		decisionFile := destination.Name + replayDecisionSuffix
		var decision sendpolicy.Decision
		record := func(value sendpolicy.Decision) error {
			decision = value
			return sendpolicy.WriteDecision(filepath.Join(inputs.root, decisionFile), value)
		}
		plan, err := operation.PrepareReplay(ctx, inputs.casePath, inputs.target, inputs.options, inputs.policy, true, record, sendpolicy.SystemResolver)
		if err != nil {
			return ReplayResult{State: Failed, Reason: err.Error(), Decision: decisionReached(decision)}
		}
		if identity, err := replayIdentity(plan, inputs); err != nil || identity != request.Expected || plan.SourceIdentity() != request.Replay.Identity {
			return ReplayResult{State: Failed, Reason: "the replay changed after its preview; preview it again before sending. Nothing was sent"}
		}
		run, err := replay.ExecuteWithPolicy(ctx, plan, output, inputs.policy, record)
		if err != nil && errors.Is(ctx.Err(), context.Canceled) {
			// A lookup the cancellation interrupted decides nothing about the
			// destination; the person cancelled, and nothing was sent.
			return ReplayResult{State: Cancelled, Reason: "the replay was cancelled before anything was sent; the decision it had reached is retained beside the run folder", Decision: decisionReached(decision)}
		}
		if err != nil {
			return ReplayResult{State: Failed, Reason: err.Error(), Decision: decisionReached(decision)}
		}
		result := ReplayResult{State: Completed, Decision: decisionReached(decision), Run: replayRunView(run, destination.Name, decisionFile)}
		if errors.Is(ctx.Err(), context.Canceled) {
			result.State, result.Reason = Cancelled, "the replay was cancelled; messages after the one in flight were not attempted, and nothing is sent again"
		}
		return result
	})
}

// replayInputs is one replay request resolved against the open workspace.
type replayInputs struct {
	root     string
	casePath string
	target   replay.Target
	policy   *sendpolicy.Policy
	options  replay.Options
}

// replayInputsOf verifies the case under the identity the window verified and
// reads the target configuration and the optional send policy through the
// readers the command line uses. Each document is one regular file of the open
// workspace, never a symbolic link, as the listing offers them.
func replayInputsOf(request ReplayRequest) (replayInputs, refusal) {
	root, _, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return replayInputs{}, declined
	}
	targetPath, err := artifactpath.File(root, request.Target)
	if err != nil {
		return replayInputs{}, refusal{Failed, "the target configuration must be one regular file of the open workspace, never a symbolic link"}
	}
	target, err := operation.ReadTarget(targetPath)
	if err != nil {
		return replayInputs{}, refusal{Failed, err.Error()}
	}
	var policy *sendpolicy.Policy
	if request.Policy != "" {
		policyPath, err := artifactpath.File(root, request.Policy)
		if err != nil {
			return replayInputs{}, refusal{Failed, "the send policy must be one regular file of the open workspace, never a symbolic link"}
		}
		read, err := operation.ReadSendPolicy(policyPath)
		if err != nil {
			return replayInputs{}, refusal{Failed, err.Error()}
		}
		policy = &read
	}
	return replayInputs{
		root: root, casePath: filepath.Join(root, request.Case), target: target, policy: policy,
		options: replay.Options{Occurrences: slices.Clone(request.Messages), Transformations: slices.Clone(request.Transformations)},
	}, refusal{}
}

// replayDestination is the run folder a send writes and the decision file it
// retains beside it, both of which must be new entries of the open workspace.
// Empty proposes the next name free for both, since a refused send retains a
// decision without a run. A name already taken is reported, not refused: a
// preview still shows what would be sent, and the send is what refuses.
func replayDestination(root, requested string) (RunDestination, refusal) {
	free := func(name string) (bool, error) {
		for _, entry := range []string{name, name + replayDecisionSuffix} {
			if _, err := os.Lstat(filepath.Join(root, entry)); err == nil {
				return false, nil
			} else if !os.IsNotExist(err) {
				return false, err
			}
		}
		return true, nil
	}
	if requested == "" {
		for i := 1; i <= 999; i++ {
			candidate := fmt.Sprintf("replay-%03d", i)
			fresh, err := free(candidate)
			if err != nil {
				return RunDestination{}, probeReadFailure(root)
			}
			if fresh {
				return RunDestination{Name: candidate, Generated: true, Fresh: true}, refusal{}
			}
		}
		return RunDestination{}, refusal{Failed, "the workspace holds more generated replay folders than this release proposes"}
	}
	if artifactpath.EntryName(requested) != nil || artifactpath.EntryName(requested+replayDecisionSuffix) != nil {
		return RunDestination{Name: requested}, refusal{Failed, "a replay's run folder must be one new entry of the open workspace"}
	}
	fresh, err := free(requested)
	if err != nil {
		return RunDestination{Name: requested}, probeReadFailure(root)
	}
	if !fresh {
		return RunDestination{Name: requested, Reason: "that run folder or the decision file beside it already exists; a send writes both as new entries"}, refusal{}
	}
	return RunDestination{Name: requested, Fresh: true}, refusal{}
}

// replayIdentity is what a send pins itself to: the case the plan was sealed
// from, the target configuration and the transport it records, the send
// policy, the transformations and every outbound message with the bytes it
// would put on the wire. Any of them changing changes the identity.
func replayIdentity(plan *replay.Plan, inputs replayInputs) (string, error) {
	type message struct {
		Mapping replay.Mapping `json:"mapping"`
		Wire    string         `json:"wire_sha256"`
	}
	pinned := struct {
		Source          string                  `json:"source"`
		Target          replay.Target           `json:"target"`
		Transport       replay.TargetRecord     `json:"transport"`
		Policy          *sendpolicy.Policy      `json:"policy"`
		Transformations []replay.Transformation `json:"transformations"`
		Messages        []message               `json:"messages"`
	}{Source: plan.SourceIdentity(), Target: plan.Configuration(), Transport: plan.Target(), Policy: inputs.policy,
		Transformations: inputs.options.Transformations, Messages: []message{}}
	for _, mapping := range plan.Mappings() {
		wire, err := plan.Outbound(mapping.OutboundOccurrence)
		if err != nil {
			return "", err
		}
		pinned.Messages = append(pinned.Messages, message{Mapping: mapping, Wire: digestOf(wire)})
	}
	encoded, err := json.Marshal(pinned, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	return digestOf(encoded), nil
}

// replayRunView is one retained run as the command's summary reads it.
func replayRunView(run *replay.Run, output, decisionFile string) *ReplayRun {
	view := &ReplayRun{Output: output, DecisionFile: decisionFile, Identity: run.Identity, Schema: run.Manifest.Schema,
		State: run.Manifest.State, ContainsSourceValues: run.Manifest.ContainsSourceValues, ExportPolicy: run.Manifest.ExportPolicy,
		Successful: run.Successful(), Messages: []ReplayOutcome{}}
	for _, event := range run.Events {
		row := ReplayOutcome{Source: event.SourceOccurrence, Outbound: event.OutboundOccurrence, Outcome: string(event.Outcome),
			Delivery: event.Delivery, SentBytes: event.Sent.Size, ReceivedBytes: event.Received.Size, ACK: event.ACK.Code,
			Correlation: event.ACK.Correlation, Elapsed: time.Duration(event.ElapsedNS).String()}
		if event.TransportError != nil {
			row.ErrorClass, row.ErrorPhase = event.TransportError.Class, event.TransportError.Phase
		}
		if event.Delivery == "uncertain" {
			view.Uncertain++
		}
		view.Messages = append(view.Messages, row)
	}
	return view
}

// decisionReached is the decision a replay reached, or nil when it reached
// none.
func decisionReached(decision sendpolicy.Decision) *sendpolicy.Decision {
	if decision.Schema == "" {
		return nil
	}
	return &decision
}
