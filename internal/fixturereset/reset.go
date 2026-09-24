package fixturereset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/environment"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// OutcomeSchema is the contract a retained reset outcome declares.
const OutcomeSchema = "readmit-reset-outcome/v1"

// Outcome is what one action, or one whole reset, established. The set is
// closed and Confirmed is the only member that is a pass: an outcome readmit
// cannot name is not a reset that happened.
type Outcome string

const (
	// Confirmed is a reset step readmit or a person established.
	Confirmed Outcome = "confirmed"
	// Unconfirmed is a step that may or may not have happened. readmit does
	// not assume it did, so it is an execution error like a failure.
	Unconfirmed Outcome = "unconfirmed"
	// Failed is a step readmit established did not happen.
	Failed Outcome = "failed"
	// Refused is a step readmit would not run: an environment nobody recorded
	// as nonproduction, a plan it will not read, or a destination the approved
	// -destination decision denies.
	Refused Outcome = "refused"
	// Cancelled is a reset a person interrupted.
	Cancelled Outcome = "cancelled"
	// NotAttempted is a step an earlier one stopped. It is recorded as what it
	// is and is never read as a pass.
	NotAttempted Outcome = "not_attempted"
)

// Reason names why one outcome came out the way it did, in readmit's own closed
// vocabulary. It holds no path, no argument and no value from an environment,
// so a retained outcome can be read and shared without carrying any of them.
type Reason string

const (
	OperatorConfirmed    Reason = "operator_confirmed"
	LedgerEmpty          Reason = "ledger_empty"
	EndpointReachable    Reason = "endpoint_reachable"
	EveryActionConfirmed Reason = "every_action_confirmed"

	AwaitingOperator      Reason = "awaiting_operator_confirmation"
	ObservationUnreadable Reason = "observation_unreadable"
	LedgerNotEmpty        Reason = "ledger_not_empty"
	// EndpointRefusedConnection and EndpointNotQuiet are two different
	// findings and are named separately: an endpoint that refused the
	// connection was silent, and saying it was not quiet would assert
	// something readmit never observed.
	EndpointRefusedConnection Reason = "endpoint_refused_connection"
	EndpointNotQuiet          Reason = "endpoint_not_quiet"
	EndpointNotConfirmed      Reason = "endpoint_not_confirmed"
	EndpointUnusable          Reason = "endpoint_configuration_unusable"

	PlanRefused                Reason = "plan_refused"
	PlanEnvironmentMismatch    Reason = "plan_environment_mismatch"
	ProductionEnvironment      Reason = "production_environment"
	UnrecordedEnvironment      Reason = "environment_not_recorded_nonproduction"
	DestinationRefused         Reason = "destination_refused"
	ApprovalNamesMachineAction Reason = "operator_approval_names_a_machine_action"
	EarlierActionStopped       Reason = "earlier_action_stopped_the_reset"
	Interrupted                Reason = "interrupted"
)

// Request is one reset attempt: the environment configuration an operator
// selected, the bytes of the plan they selected, the approved-destination
// document they selected, the directory a read-authority action's declared file
// is opened inside, and the action ids they explicitly confirmed performing.
type Request struct {
	Target        replay.Target
	PlanBytes     []byte
	PlanDirectory string
	Policy        *sendpolicy.Policy
	Confirmed     []string
}

// ActionOutcome is what one reviewed action established, beside the operator it
// named and the authority it ran under.
type ActionOutcome struct {
	ID        string    `json:"id"`
	Operator  Operator  `json:"operator"`
	Authority Authority `json:"authority"`
	Outcome   Outcome   `json:"outcome"`
	Reason    Reason    `json:"reason"`
	// Diagnosis is the transport outcome a connect-authority action observed.
	// It names the evidence behind the verdict rather than leaving the verdict
	// standing on its own.
	Diagnosis environment.Outcome `json:"diagnosis,omitzero"`
}

// Result is retained evidence of one reset attempt. It names the environment it
// was produced against, the plan bytes it ran, and every action's own outcome,
// so a reset can be read later without the plan document beside it.
type Result struct {
	Schema         string                `json:"schema"`
	State          durablerun.State      `json:"state"`
	Outcome        Outcome               `json:"outcome"`
	Reason         Reason                `json:"reason"`
	Environment    string                `json:"environment"`
	Classification replay.Classification `json:"classification"`
	PlanSHA256     string                `json:"plan_sha256"`
	// Decision is the approved-destination reason this reset was held to, when
	// the plan declared an action that would open a connection and readmit
	// therefore asked. It is absent when no action needed one.
	Decision    sendpolicy.Reason `json:"decision,omitzero"`
	Actions     []ActionOutcome   `json:"actions"`
	AttemptedAt time.Time         `json:"attempted_at"`
}

// ExitCode is the process status one reset deserves. A confirmed reset exits 0
// and everything else exits 2. There is no 1: a fixture that did not reset is
// an execution error, never an assertion failure, because nothing about it is
// evidence that an expectation was wrong.
func (r Result) ExitCode() int {
	if r.State == durablerun.Passed {
		return 0
	}
	return 2
}

// Run performs one reset and names what it established. It returns no error:
// every way a reset can end, including every way it can be refused, is a named
// Result, so no caller has to decide what an unnamed failure meant. It also
// returns the plan it read, so a caller that must show a person what is still
// theirs to do does not decode the same bytes a second time; that plan is the
// zero value when the reset was refused before one was read.
//
// The recorded class is read before anything else, exactly as the send decision
// reads it, so a production environment is refused as the production
// environment it is rather than as whatever else is wrong beside it. An
// environment nobody recorded as nonproduction is refused too: an absent claim
// is not a nonproduction one.
func Run(ctx context.Context, request Request, resolve sendpolicy.Resolver) (Result, Plan) {
	named := request.Target.Environment()
	result := Result{
		Schema: OutcomeSchema, Environment: named.Name, Classification: named.Classification,
		PlanSHA256: digest(request.PlanBytes), Actions: []ActionOutcome{},
		AttemptedAt: time.Now().UTC(),
	}
	if sendpolicy.RefusesEverySend(string(named.Classification)) {
		return refuse(result, ProductionEnvironment), Plan{}
	}
	if named.Classification != replay.Nonproduction {
		return refuse(result, UnrecordedEnvironment), Plan{}
	}
	plan, err := DecodePlan(request.PlanBytes)
	if err != nil {
		return refuse(result, PlanRefused), Plan{}
	}
	if plan.Environment != named.Name {
		return refuse(result, PlanEnvironmentMismatch), plan
	}
	if !approvalsNameOperatorActions(plan, request.Confirmed) {
		return refuse(result, ApprovalNamesMachineAction), plan
	}
	// A name is resolved and a socket opened only when the plan says an action
	// needs one. A plan that touches no endpoint therefore has no destination
	// to decide about, and readmit asks nothing of the network for it.
	var route destination.Route
	if plan.RequiresConnection() {
		connect, err := time.ParseDuration(request.Target.ConnectTimeout)
		if err != nil || connect <= 0 {
			return refuse(result, EndpointUnusable), plan
		}
		// The same rule a send is held to, asked here with no send requested,
		// because a reset sends no HL7 payload. Nothing about the destination
		// refusing is the only decision that lets a connection be opened; a
		// reset never reaches the explicit-send rule, so it can never be
		// allowed by one implementation while a send is denied by another.
		// The lookup and the connection share the configuration's own connect
		// timeout, and the connection reaches only the address the decision
		// checked: the name is never resolved a second time.
		decision, _ := destination.Decide(ctx, destination.Request{
			Purpose: destination.ResetCheck, Address: request.Target.Address,
			Classification: string(named.Classification), Policy: request.Policy,
			Budget: connect, Resolve: resolve,
		})
		result.Decision = decision.Reason
		var admitted bool
		route, admitted = decision.Route()
		if !admitted {
			return refuse(result, DestinationRefused), plan
		}
	}
	stopped := false
	for _, action := range plan.Actions {
		if stopped {
			result.Actions = append(result.Actions, ActionOutcome{
				ID: action.ID, Operator: action.Operator, Authority: action.Authority,
				Outcome: NotAttempted, Reason: EarlierActionStopped,
			})
			continue
		}
		performed := perform(ctx, request, action, route)
		result.Actions = append(result.Actions, performed)
		if performed.Outcome != Confirmed {
			stopped = true
			result.Outcome, result.Reason = performed.Outcome, performed.Reason
		}
	}
	if !stopped {
		result.Outcome, result.Reason = Confirmed, EveryActionConfirmed
	}
	return settle(result), plan
}

// perform runs one reviewed action under the one authority it requires. The
// switch is the whole of what a plan can cause: there is no default branch that
// runs something a reviewer did not read, because the reader already refused
// every operator absent from the reviewed table.
func perform(ctx context.Context, request Request, action Action, route destination.Route) ActionOutcome {
	performed := ActionOutcome{ID: action.ID, Operator: action.Operator, Authority: action.Authority}
	if err := ctx.Err(); err != nil {
		performed.Outcome, performed.Reason = Cancelled, Interrupted
		return performed
	}
	switch action.Operator {
	case OperatorConfirms:
		if !slices.Contains(request.Confirmed, action.ID) {
			performed.Outcome, performed.Reason = Unconfirmed, AwaitingOperator
			return performed
		}
		performed.Outcome, performed.Reason = Confirmed, OperatorConfirmed
	case ObservationEmpty:
		performed.Outcome, performed.Reason = emptyLedger(request.PlanDirectory, action.Observation)
	case EndpointQuiet:
		performed.Outcome, performed.Reason, performed.Diagnosis = quietEndpoint(ctx, request.Target, route)
	}
	return performed
}

// emptyLedger confirms that the receiver this environment exports through came
// back up on the declared starting state: an empty ledger, nothing processed,
// and a snapshot the receiver itself called consistent. It opens exactly the
// one declared file, inside the plan's own directory, read-only, and reads it
// through the observation contract's own decoder rather than a second one.
//
// A file it cannot read as that contract leaves the reset unconfirmed rather
// than failed: readmit established nothing about the fixture either way, and
// not knowing is not a pass.
func emptyLedger(directory, name string) (Outcome, Reason) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Unconfirmed, ObservationUnreadable
	}
	defer root.Close()
	data, err := artifactdir.Document{MaxBytes: observation.MaxBytes, Links: artifactdir.FollowLinks}.ReadIn(root, name)
	if err != nil {
		return Unconfirmed, ObservationUnreadable
	}
	snapshot, err := observation.Decode(data)
	if err != nil {
		return Unconfirmed, ObservationUnreadable
	}
	if !snapshot.EmptyInitialState() {
		return Failed, LedgerNotEmpty
	}
	return Confirmed, LedgerEmpty
}

// quietEndpoint confirms the environment accepts a connection again and sends
// nothing unprompted, through the one diagnosis readmit already owns, over the
// route the reset's decision admitted. It sends no HL7 payload: reaching an
// endpoint is evidence about the transport and never evidence that an
// application accepted, processed or stored anything, and a reset claims only
// the former.
//
// A refused connection and bytes arriving unprompted are both things readmit
// established, so they are failures. Everything else that is not reachable
// leaves the reset merely unconfirmed: a timeout in particular is not a finding
// about the fixture at all, it is readmit not having established anything, and
// a certificate that would not verify says nothing about a ledger either way.
func quietEndpoint(ctx context.Context, target replay.Target, route destination.Route) (Outcome, Reason, environment.Outcome) {
	report, err := environment.Diagnose(ctx, target, route)
	if err != nil {
		return Refused, EndpointUnusable, ""
	}
	switch report.Outcome {
	case environment.Reachable:
		return Confirmed, EndpointReachable, report.Outcome
	case environment.Cancelled:
		return Cancelled, Interrupted, report.Outcome
	case environment.ConnectionRefused:
		return Failed, EndpointRefusedConnection, report.Outcome
	case environment.UnsolicitedBytes:
		return Failed, EndpointNotQuiet, report.Outcome
	}
	return Unconfirmed, EndpointNotConfirmed, report.Outcome
}

// approvalsNameOperatorActions reports whether every id a person confirmed
// names an action a person performs. Confirming a machine action would let
// somebody assert an outcome readmit is supposed to establish itself, and
// confirming an id no plan declares is a reset aimed at something that is not
// there; both are refused rather than ignored.
func approvalsNameOperatorActions(plan Plan, confirmed []string) bool {
	for _, id := range confirmed {
		index := slices.IndexFunc(plan.Actions, func(action Action) bool { return action.ID == id })
		if index < 0 || plan.Actions[index].Operator != OperatorConfirms {
			return false
		}
	}
	return true
}

// refuse records a reset that never ran an action, with the reason it did not.
func refuse(result Result, reason Reason) Result {
	result.Outcome, result.Reason = Refused, reason
	return settle(result)
}

// settle maps what a reset established onto the execution vocabulary durable
// runs already own, so one run journal and one reset speak about a stopped
// execution in the same words. Confirmed is the only pass. Cancellation is
// cancellation. Everything else — failed, unconfirmed, refused — is an
// execution error, never an assertion failure.
func settle(result Result) Result {
	switch result.Outcome {
	case Confirmed:
		result.State = durablerun.Passed
	case Cancelled:
		result.State = durablerun.Cancelled
	default:
		result.State = durablerun.ExecutionError
	}
	return result
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// EncodeOutcome renders one reset outcome as the document a command retains.
// The caller performs the write, so every artifact readmit produces is reserved
// by one path owner rather than by each package with something to record.
func EncodeOutcome(result Result) ([]byte, error) {
	data, err := json.Marshal(result, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode the reset outcome")
	}
	return append(data, '\n'), nil
}
