// Package fixturereset returns one named nonproduction test environment to its
// declared starting state before a regression run, and says plainly when it
// could not.
//
// A reset plan is data that names an operator, exactly as a test spec is. Every
// action it can hold is one of a closed set of typed Go operators this release
// reviewed. There is no member anywhere in the contract that carries a command,
// a script, an interpreter, an argument vector, a path to a program or an
// expression, so importing a regression packet a customer keeps and reruns in
// CI cannot make readmit execute anything its reviewers did not read. Reset
// instructions stay operator-readable prose and are never executed. See
// ADR-0003.
//
// Authority is declared, never assumed. Each reviewed operator requires exactly
// one authority, the plan writes that authority down beside the operator it
// names, and a plan whose declared authority is not the one its operator
// requires is refused rather than run under the wider of the two. An action
// with no authority performs nothing at all: a person does the work and
// explicitly confirms it, which is what operator-assisted means here.
//
// Nothing in this package passes a pass it did not establish. A reset that
// failed, one whose outcome could not be confirmed, one an unreviewed action
// asked for and one aimed at an environment nobody recorded as nonproduction
// are each an execution error in the vocabulary internal/durablerun owns. None
// of them is an assertion failure, because a fixture that did not reset is not
// evidence that an expectation was wrong.
package fixturereset

import (
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// PlanSchema is the contract an explicitly selected reset plan declares.
const PlanSchema = "readmit-reset-plan/v1"

const (
	// MaxPlanBytes bounds the document a command reads before decoding it.
	MaxPlanBytes = 64 << 10
	// maxActions bounds one plan. A plan nobody can read through is not a
	// reviewed one, and an unbounded list is refused rather than truncated.
	maxActions = 32
	// maxInstructions bounds the prose one action carries for the person who
	// performs it. Prose is printed for that person and executed by nothing.
	maxInstructions = 4096
)

// Operator names one reviewed reset action. The set is closed: an operator this
// release did not review is refused by the reader, so no imported document can
// introduce a new one.
type Operator string

const (
	// OperatorConfirms performs nothing. A person resets the fixture and
	// explicitly confirms this action by name; without that confirmation the
	// step is unconfirmed, never assumed.
	OperatorConfirms Operator = "operator_confirms"
	// ObservationEmpty confirms that the receiver the environment exports
	// through has come back up on an empty ledger with nothing processed.
	ObservationEmpty Operator = "observation_empty"
	// EndpointQuiet confirms that the environment accepts a connection again
	// and sends nothing unprompted. It sends no HL7 payload.
	EndpointQuiet Operator = "endpoint_quiet"
)

// Authority is the narrowest thing a reviewed operator needs to be allowed to
// do. It is a member of the plan so that reading the plan is enough to know
// what running it permits; readmit checks the declaration against the reviewed
// table rather than granting whatever the operator happens to want.
type Authority string

const (
	// NoAuthority performs nothing. readmit reads no file and opens no
	// connection for it.
	NoAuthority Authority = "none"
	// ReadDeclaredFile reads exactly the one file the action declares, inside
	// the plan's own directory, and writes nothing.
	ReadDeclaredFile Authority = "read_declared_file"
	// ConnectApprovedTarget opens one connection to the explicitly selected
	// target, held to the same approved-destination decision a send is held
	// to, and sends no HL7 payload.
	ConnectApprovedTarget Authority = "connect_approved_target"
)

// reviewed is the closed set of reset actions and the one authority each of
// them requires. It is the review: an operator absent from this table cannot be
// named by any document, and an operator present in it can never be run under
// an authority other than the one recorded beside it here.
var reviewed = map[Operator]Authority{
	OperatorConfirms: NoAuthority,
	ObservationEmpty: ReadDeclaredFile,
	EndpointQuiet:    ConnectApprovedTarget,
}

// Plan is the reset an operator selected explicitly. readmit has no default
// plan, no discovered one and no plan member inside a test spec: a spec names
// prose for a person, and a reviewed action comes from a document somebody
// chose on the command line.
type Plan struct {
	Schema string `json:"schema"`
	// Environment is the name recorded for the environment this plan resets.
	// It must match the selected configuration, so a plan written for one
	// environment cannot be pointed at another by changing one flag.
	Environment string   `json:"environment"`
	Actions     []Action `json:"actions"`
}

// Action is one reviewed step. Its members are a name, an operator, the
// authority that operator requires, prose for the person who performs it, and
// for a read-authority action the one file it may read. Nothing else: there is
// no command, no argument, no interpreter and no expression.
type Action struct {
	ID           string    `json:"id"`
	Operator     Operator  `json:"operator"`
	Authority    Authority `json:"authority"`
	Instructions string    `json:"instructions"`
	// Observation is the receiver observation file an ObservationEmpty action
	// reads, as one path inside the plan's own directory. It is required for
	// that operator and refused for every other, so no action carries a file
	// it has no authority to read.
	Observation string `json:"observation,omitzero"`
}

// UnmarshalJSON reads one action exactly as written. Presence is checked first
// and the same bytes are then re-read rejecting unknown members, so an omitted
// member is refused as omitted rather than read as a zero value, and a member
// this contract never declared is refused rather than ignored.
func (a *Action) UnmarshalJSON(data []byte) error {
	var required struct {
		ID           *string    `json:"id"`
		Operator     *Operator  `json:"operator"`
		Authority    *Authority `json:"authority"`
		Instructions *string    `json:"instructions"`
		Observation  *string    `json:"observation"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Operator == nil || required.Authority == nil || required.Instructions == nil {
		return errors.New("a reset action requires id, operator, authority and instructions")
	}
	type action Action
	var decoded action
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a reset action declares no member beyond id, operator, authority, instructions and observation")
	}
	*a = Action(decoded)
	return nil
}

// DecodePlan reads one reset plan exactly as written and refuses every document
// that names something this release did not review. Unknown members are
// refused, so a plan authored against a later contract is never read as though
// this one had always allowed it, and a member somebody added to smuggle a
// command through is a decode error rather than a silently ignored extra.
func DecodePlan(data []byte) (Plan, error) {
	if len(data) > MaxPlanBytes {
		return Plan{}, errors.New("a reset plan exceeds its size limit")
	}
	var plan Plan
	if err := json.Unmarshal(data, &plan, json.RejectUnknownMembers(true)); err != nil {
		return Plan{}, errors.New("invalid reset plan JSON")
	}
	if err := validatePlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func validatePlan(plan Plan) error {
	if plan.Schema != PlanSchema {
		return errors.New("a reset plan must declare " + PlanSchema)
	}
	if err := environmentName(plan.Environment); err != nil {
		return err
	}
	if len(plan.Actions) == 0 || len(plan.Actions) > maxActions {
		return errors.New("a reset plan declares between 1 and 32 reset actions")
	}
	declared := make(map[string]bool, len(plan.Actions))
	for _, action := range plan.Actions {
		if err := validateAction(action); err != nil {
			return err
		}
		if declared[action.ID] {
			return errors.New("a reset action is declared twice")
		}
		declared[action.ID] = true
	}
	return nil
}

func validateAction(action Action) error {
	if err := actionID(action.ID); err != nil {
		return err
	}
	required, ok := reviewed[action.Operator]
	if !ok {
		return errors.New("a reset action names an operator this release did not review; the reviewed operators are " + strings.Join(reviewedOperators(), ", "))
	}
	if action.Authority != required {
		return errors.New("a reset action must declare the one authority its reviewed operator requires: " + string(required))
	}
	if action.Instructions == "" || len(action.Instructions) > maxInstructions || !readableProse(action.Instructions) {
		return errors.New("a reset action requires operator-readable instructions of at most 4096 UTF-8 bytes, with no control characters beyond tab and newline")
	}
	if action.Operator != ObservationEmpty {
		if action.Observation != "" {
			return errors.New("only a reset action with read_declared_file authority declares an observation file")
		}
		return nil
	}
	// A read-authority action names its one file lexically inside the plan's
	// own directory. readmit opens it within that directory rather than by the
	// name alone, so a plan cannot direct a read at a file elsewhere on the
	// machine, and it is opened read-only through the observation contract.
	if action.Observation == "" || !filepath.IsLocal(action.Observation) {
		return errors.New("an observation_empty action names one receiver observation file inside the plan's own directory")
	}
	return nil
}

// readableProse bounds what an action may say to the person performing it.
// The prose is printed for that person and executed by nothing, so the only
// question is whether it can be read: valid UTF-8, and no control character
// beyond tab and newline, because a document somebody imported must not be able
// to drive the terminal it is displayed on.
func readableProse(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r != '\t' && r != '\n' && (r < 0x20 || r == 0x7f) {
			return false
		}
	}
	return true
}

// actionID bounds the one name a person types after --confirm. It is the same
// shape a test spec requires of an assertion id, so a person reading either
// document is reading one convention.
func actionID(id string) error {
	if id == "" || len(id) > 64 || id[0] < 'a' || id[0] > 'z' {
		return errors.New("a reset action id begins with a lowercase letter and is at most 64 bytes")
	}
	for _, r := range id {
		if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return errors.New("a reset action id holds lowercase letters, digits and '-' only")
		}
	}
	return nil
}

// environmentName bounds the environment a plan declares with the same rule
// readmit-target/v3 bounds the name it records, so the two can be compared as
// written and neither has a spelling the other would refuse.
func environmentName(name string) error {
	if name == "" || len(name) > 64 {
		return errors.New("a reset plan names the environment it resets, of at most 64 bytes")
	}
	for _, r := range name {
		if r != '-' && r != '_' && r != '.' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return errors.New("the name for the environment holds letters, digits, '-', '_' and '.' only")
		}
	}
	return nil
}

// reviewedOperators names what a document may ask for, read from the review
// itself rather than restated beside it, so the refusal a reader gives can
// never list a different set from the one the reader applies.
func reviewedOperators() []string {
	named := make([]string, 0, len(reviewed))
	for operator := range reviewed {
		named = append(named, string(operator))
	}
	slices.Sort(named)
	return named
}

// Instructions is the prose one action carries for the person who performs it,
// or nothing when this plan declares no such action. The pairing lives with the
// plan because the plan is what holds the prose; a caller showing a person what
// is still theirs to do reads it here rather than walking the actions itself.
func (p Plan) Instructions(id string) string {
	for _, action := range p.Actions {
		if action.ID == id {
			return action.Instructions
		}
	}
	return ""
}

// RequiresConnection reports whether this plan holds an action that would open
// a connection. readmit resolves and dials for a reset only when the plan says
// it must, so selecting a plan that touches no endpoint costs no lookup.
func (p Plan) RequiresConnection() bool {
	for _, action := range p.Actions {
		if action.Authority == ConnectApprovedTarget {
			return true
		}
	}
	return false
}
