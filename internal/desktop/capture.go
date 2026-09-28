package desktop

import (
	"context"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/capturejournal"
	"github.com/bharm16/readmit/internal/collection"
)

// CapturePhase names what the capture screen is doing without disclosing values.
type CapturePhase string

const (
	CaptureIdle       CapturePhase = "idle"
	CaptureCollecting CapturePhase = "collecting"
	CaptureStopping   CapturePhase = "stopping"
	CaptureStopped    CapturePhase = "stopped"
	CaptureFailed     CapturePhase = "failed"
)

// PathChoiceResult is the outcome of a native file or folder dialog used to
// fill a source or listener field.
type PathChoiceResult struct {
	State  State    `json:"state"`
	Reason string   `json:"reason,omitzero"`
	Kind   string   `json:"kind,omitzero"`
	Paths  []string `json:"paths,omitzero"`
}

func (r *PathChoiceResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CapturePathKind is what a capture source editor chooses in the host's
// dialog: a local folder, a transfer program, a listener's certificate or its
// client certificate authority.
type CapturePathKind string

// The capture path kinds.
const (
	SourceRootPath      CapturePathKind = "source-root"
	TransferProgramPath CapturePathKind = "transfer-program"
	CertificatePath     CapturePathKind = "certificate"
	ClientCAPath        CapturePathKind = "client-ca"
)

// ChooseCapturePath presents the host's dialog for a capture source's local
// folder, its transfer program, or a listener's certificate or client
// certificate authority. Choosing reads and writes nothing.
func (a *App) ChooseCapturePath(kind CapturePathKind) PathChoiceResult {
	return run(a, true, false, func(ctx context.Context) PathChoiceResult {
		var paths []string
		var declined refusal
		switch kind {
		case SourceRootPath:
			folder, refused := a.chooseFolder(ctx, "Choose the local export folder")
			declined = refused
			if folder != "" {
				paths = []string{folder}
			}
		case TransferProgramPath:
			paths, declined = a.chooseFiles(ctx, "Choose the customer transfer program", "Programs (*.*)", "*.*")
		case CertificatePath, ClientCAPath:
			paths, declined = a.chooseFiles(ctx, "Choose a PEM certificate", "PEM certificates (*.pem *.crt)", "*.pem;*.crt")
		default:
			return PathChoiceResult{State: Failed, Reason: "unsupported capture path kind"}
		}
		if len(paths) == 0 {
			return PathChoiceResult{State: declined.state, Reason: declined.reason}
		}
		return PathChoiceResult{State: Completed, Kind: string(kind), Paths: paths[:1]}
	})
}

// ReceiverPolicyChoices is what a person chose in the window for one
// responder policy: its name, label and original-mode rules, whether it
// answers enhanced mode with the fixed codes, and one fault step (Fault is
// "none" or empty for none) held to the endpoint the capture listens on.
// Opened is the document the window opened, if any: while the enhanced and
// fault choices still say what it declares, its version, enhanced rule and
// faults are kept exactly as declared, so what the controls cannot express
// is never lost.
type ReceiverPolicyChoices struct {
	Name                 string                     `json:"name"`
	SourceLabel          string                     `json:"source_label"`
	Acknowledgement      collection.AckRule         `json:"acknowledgement"`
	AcceptedMessageTypes collection.MessageTypeRule `json:"accepted_message_types"`
	Enhanced             bool                       `json:"enhanced"`
	Fault                string                     `json:"fault,omitzero"`
	FaultDelayMS         int                        `json:"fault_delay_ms,omitzero"`
	Endpoint             string                     `json:"endpoint,omitzero"`
	Opened               *collection.Policy         `json:"opened,omitzero"`
}

// noFault is the fault choice that declares none.
const noFault = "none"

// defaultFaultDelayMS is the delay the window starts a waiting fault with.
const defaultFaultDelayMS = 50

// declaredFault is the one fault step a policy's controls show, and the delay
// they show beside it: the first step's, or none with the window's starting
// delay.
func declaredFault(policy collection.Policy) (string, int) {
	if policy.Faults == nil || len(policy.Faults.Steps) == 0 {
		return noFault, defaultFaultDelayMS
	}
	return policy.Faults.Steps[0].Action, policy.Faults.Steps[0].DelayMS
}

// declaresEnhanced reports whether a policy's enhanced rule is the one the
// window's enhanced control expresses.
func declaresEnhanced(policy collection.Policy) bool {
	return policy.Enhanced != nil && policy.Enhanced.Operator == collection.EnhancedFixedCodes
}

// policyChoices is what the window's controls show for a declared policy:
// its own members, whether it declares the fixed-code enhanced rule, its
// first fault step and the first test endpoint it approves.
func policyChoices(policy collection.Policy) *ReceiverPolicyChoices {
	fault, delay := declaredFault(policy)
	choices := &ReceiverPolicyChoices{
		Name: policy.Name, SourceLabel: policy.SourceLabel,
		Acknowledgement: policy.Acknowledgement, AcceptedMessageTypes: policy.AcceptedMessageTypes,
		Enhanced: declaresEnhanced(policy), Fault: fault, FaultDelayMS: delay,
	}
	if policy.Faults != nil && len(policy.Faults.ApprovedTestEndpoints) > 0 {
		choices.Endpoint = policy.Faults.ApprovedTestEndpoints[0]
	}
	return choices
}

// policy composes the document the choices describe. The version is the
// first that declares what was chosen: the original-mode rule alone is v1, an
// enhanced rule v2, and a fault step v3, which declares the enhanced rule as
// unsupported when none was chosen.
func (c ReceiverPolicyChoices) policy() collection.Policy {
	composed := collection.Policy{
		Name: c.Name, SourceLabel: c.SourceLabel,
		Acknowledgement: c.Acknowledgement, AcceptedMessageTypes: c.AcceptedMessageTypes,
	}
	fault := c.Fault
	if fault == "" {
		fault = noFault
	}
	if opened := c.Opened; opened != nil {
		openedFault, openedDelay := declaredFault(*opened)
		if c.Enhanced == declaresEnhanced(*opened) && fault == openedFault && (fault == noFault || c.FaultDelayMS == openedDelay) {
			composed.Schema, composed.Enhanced, composed.Faults = opened.Schema, opened.Enhanced, opened.Faults
			return composed
		}
	}
	composed.Schema = collection.PolicySchemaV1
	if c.Enhanced {
		composed.Schema = collection.PolicySchema
		composed.Enhanced = &collection.EnhancedRule{
			Operator: collection.EnhancedFixedCodes, AcceptCode: collection.CommitAcceptCode,
			ApplicationCode: collection.AcceptCode, ApplicationDelivery: collection.SameConnection,
		}
	}
	if fault != noFault {
		composed.Schema = collection.FaultPolicySchema
		if composed.Enhanced == nil {
			composed.Enhanced = &collection.EnhancedRule{Operator: collection.EnhancedUnsupported}
		}
		delay := 0
		if collection.FaultWaits(fault) {
			delay = c.FaultDelayMS
		}
		composed.Faults = &collection.FaultPolicy{
			EnvironmentClass:      "nonproduction",
			ApprovedTestEndpoints: []string{c.Endpoint},
			Steps:                 []collection.FaultStep{{Message: 1, Stage: collection.ApplicationStage, Action: fault, DelayMS: delay}},
		}
	}
	return composed
}

// CaptureRequest starts a capture from a saved capture source (#552):
// Context names the project, Source the saved source at the revision the
// setup read, Name the case, Environment the named environment it is
// recorded against, Limits what bounds this one capture beyond the source's
// own, and IntentID the click.
type CaptureRequest struct {
	Context     RequestContext `json:"context,omitzero"`
	Source      *ItemRef       `json:"source,omitzero"`
	Name        string         `json:"name,omitzero"`
	Environment *ItemRef       `json:"environment,omitzero"`
	Limits      *CaptureLimits `json:"limits,omitzero"`
	IntentID    string         `json:"intent_id,omitzero"`
}

// CaptureSessionResult is the outcome of one authorized listen/collect serve.
type CaptureSessionResult struct {
	State           State                   `json:"state"`
	Reason          string                  `json:"reason,omitzero"`
	Phase           CapturePhase            `json:"phase,omitzero"`
	BoundAddress    string                  `json:"bound_address,omitzero"`
	Case            *Case                   `json:"case,omitzero"`
	CasePath        string                  `json:"case_path,omitzero"`
	Journal         *capturejournal.Summary `json:"journal,omitzero"`
	ObservationPath string                  `json:"observation_path,omitzero"`
	Received        int                     `json:"received,omitzero"`
	Connections     int                     `json:"connections,omitzero"`
	Dropped         int                     `json:"dropped,omitzero"`
	Ledger          *FixtureLedger          `json:"ledger,omitzero"`
	// Outcome, Session and CaseRef answer a capture from a saved source:
	// finished with the registered case, cancelled or interrupted with its
	// data kept under the session, or finalize-failed with the session a
	// retry names.
	Outcome  CaptureOutcome `json:"outcome,omitzero"`
	Session  string         `json:"session,omitzero"`
	CaseRef  *ItemRef       `json:"case_ref,omitzero"`
	Replayed bool           `json:"replayed,omitzero"`
}

func (r *CaptureSessionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// refuseExecution answers a capture its execution admission declined: it
// never started, so it failed, or stopped when the person cancelled while
// admission waited.
func (r *CaptureSessionResult) refuseExecution(state State, reason string) {
	r.State, r.Reason, r.Phase = state, reason, CaptureFailed
	if state == Cancelled {
		r.Phase = CaptureStopped
	}
}

// FixtureLedger is the appointment ledger a fixture listen sealed into its
// case, counted as `readmit listen` and `readmit timeline` print it: the
// receiver's testimony, never a verdict, and never one of its values.
type FixtureLedger struct {
	Schema     string `json:"schema"`
	Profile    string `json:"profile"`
	Mode       string `json:"mode"`
	Processed  int    `json:"processed"`
	Records    int    `json:"records"`
	Consistent bool   `json:"consistent"`
}

// CaptureProgress is where a running collector or fixture accepts
// connections: the address it bound, which is the only place a port of 0
// becomes a port a sender can be pointed at.
//
// A capture started from a saved source (#552) also carries its session, its
// source, its name, when it started, how many messages arrived, whether a
// finish was asked for, and the metadata of the most recent messages, at most
// MaxCaptureMessages: when each arrived, its declared type and the
// connection, never a value.
type CaptureProgress struct {
	Kind         string            `json:"kind"`
	BoundAddress string            `json:"bound_address"`
	Session      string            `json:"session,omitzero"`
	Name         string            `json:"name,omitzero"`
	Source       *ItemRef          `json:"source,omitzero"`
	SourceName   string            `json:"source_name,omitzero"`
	SourceType   CaptureSourceType `json:"source_type,omitzero"`
	StartedAt    *string           `json:"started_at,omitzero"`
	Received     int               `json:"received,omitzero"`
	Finishing    bool              `json:"finishing,omitzero"`
	Messages     []CaptureMessage  `json:"messages,omitzero"`
}

// CaptureMessage is the metadata of one received message. Connection is
// which of the capture's connections it arrived on, numbered from 1 in the
// order they first sent.
type CaptureMessage struct {
	At         string `json:"at"`
	Type       string `json:"type"`
	Connection int    `json:"connection"`
}

// CaptureProgressResult is Empty while nothing listens, and Completed with the
// bound address while a collector or fixture is ready to accept.
type CaptureProgressResult struct {
	State    State            `json:"state"`
	Reason   string           `json:"reason,omitzero"`
	Progress *CaptureProgress `json:"progress,omitzero"`
}

func (r *CaptureProgressResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// CaptureProgress reads where the running capture listens. It does not claim
// the operation slot, so the screen can read it while StartCapture holds the
// slot, and it starts, binds and changes nothing.
func (a *App) CaptureProgress() CaptureProgressResult {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	if a.captureProgress == nil {
		return CaptureProgressResult{State: Empty}
	}
	listening := *a.captureProgress
	listening.Messages = slices.Clone(listening.Messages)
	return CaptureProgressResult{State: Completed, Progress: &listening}
}

func (a *App) reportCaptureProgress(kind string) func(bound string) error {
	return func(bound string) error {
		a.captureMu.Lock()
		defer a.captureMu.Unlock()
		if a.captureProgress != nil && a.captureProgress.Session != "" {
			a.captureProgress.Kind, a.captureProgress.BoundAddress = kind, bound
			return nil
		}
		a.captureProgress = &CaptureProgress{Kind: kind, BoundAddress: bound, Messages: []CaptureMessage{}}
		return nil
	}
}

func (a *App) endCaptureProgress() {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	a.captureProgress, a.captureStop, a.captureFinishing = nil, nil, false
}

// captureOperation names a capture while it holds the slot, so the capture
// panel's cancel control stops exactly the collector or listener it started.
const captureOperation = "capture"

// StartCapture starts a capture from a saved capture source, only on the
// explicit Start capture action (#552). A person's cancel stops it through
// the shared engine; FinishCapture finishes it.
func (a *App) StartCapture(request CaptureRequest) CaptureSessionResult {
	return runNamed[CaptureSessionResult, *CaptureSessionResult](a, profiles["StartCapture"], func(ctx context.Context) CaptureSessionResult {
		if request.Source == nil {
			return CaptureSessionResult{State: Failed, Reason: "a capture starts from a saved capture source", Phase: CaptureFailed}
		}
		return a.startSourceCapture(ctx, request)
	})
}

func (a *App) finishCapture(ctx context.Context, out CaptureSessionResult, b *bundle.Bundle, serveErr error) CaptureSessionResult {
	if b != nil {
		out.Case = caseView(out.CasePath, b)
		out.Connections = len(b.Manifest.Sources)
		if snapshot := b.Observation; snapshot != nil {
			out.Ledger = &FixtureLedger{
				Schema: snapshot.Schema, Profile: snapshot.Profile, Mode: string(snapshot.Mode),
				Processed: len(snapshot.Processed), Records: len(snapshot.Records), Consistent: snapshot.Consistent,
			}
		}
	}
	// A person's cancellation is answered as one even when the serve finalized
	// in an orderly way, as a fixture's and a collector's controlled stops do:
	// the case sealed what arrived before it, not what was declared. Reaching
	// the bound on an admitted execution is not a cancellation.
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		out.State = Cancelled
		out.Reason = cancelledRefusal.reason
		if b != nil {
			out.Phase = CaptureStopped
		} else {
			out.Phase = CaptureStopping
		}
	case ctx.Err() != nil:
		out.State = Failed
		out.Reason = "the capture stopped at the longest an admitted execution may run"
		out.Phase = CaptureFailed
		if b != nil {
			out.Phase = CaptureStopped
		}
	case serveErr == nil:
		out.State = Completed
		out.Phase = CaptureStopped
	default:
		out.State = Failed
		out.Reason = serveErr.Error()
		if b != nil {
			out.Phase = CaptureStopped
		} else {
			out.Phase = CaptureFailed
		}
	}
	return out
}
