// Package scenario reads one designed interface workflow: an ordered sequence
// of lifecycle events over identities that stay linked from the first message
// to the last.
//
// The hazard this package exists for is a workflow nobody can trust as a
// negative case. Teams need the failing sequence as much as the passing one —
// cancelling an appointment that was never booked, merging a patient identity
// into itself — and a designer that quietly accepts both is worth nothing,
// because the author never learns which of the two they wrote. So a step
// declares the outcome its author intended and the profile's typed transition
// operators decide whether that is what the sequence actually does. A step
// declared accepted that the lifecycle refuses, and a step declared refused
// that the lifecycle accepts, are both refused by name.
//
// A scenario is data interpreted by typed Go operators, exactly as a test spec
// is (ADR-0003). It carries no command, script, interpreter, expression or
// program path: an event is one member of a closed set the bound profile
// declares, and a transition is Go code in this package. Nothing here writes
// evidence, generates message bytes, opens a case, or reaches a network.
//
// What a profile makes available is the profile's decision. A scenario names
// one lifecycle profile and may use only the subject kinds and events that
// profile declares; nothing is borrowed from the other profile and no event is
// accepted because it looks like one that exists. These are readmit fixture
// lifecycle profiles, not a claim of HL7 conformance and not the
// readmit-siu-v1 message profile `readmit synth` implements.
package scenario

import (
	"encoding/json/v2"
	"errors"
	"time"
)

// Schema is the contract an explicitly selected scenario declares.
const Schema = "readmit-scenario/v1"

const (
	// MaxBytes bounds the document a caller reads before decoding it.
	MaxBytes = 64 << 10
	// maxSubjects bounds the linked identities one scenario declares.
	maxSubjects = 32
	// maxSteps bounds one sequence. A sequence nobody can read through is not
	// a reviewed one, and an unbounded list is refused rather than truncated.
	maxSteps = 64
	// maxNameBytes bounds one scenario-local name.
	maxNameBytes = 64
	// maxVersionBytes bounds the version a scenario declares for itself.
	maxVersionBytes = 32
	// maxValueBytes bounds one declared identifier or assigning authority.
	maxValueBytes = 64
	// maxOffset bounds how far after the base time a step may sit. A year is
	// far past any interface workflow and keeps the arithmetic obvious.
	maxOffset = 8760 * time.Hour
)

// Expectation is the outcome a step's author intended. There is no third
// value: a step whose outcome nobody wrote down is refused as omitted.
type Expectation string

const (
	// Accepted means the lifecycle must take this step from the state the
	// sequence has reached.
	Accepted Expectation = "accepted"
	// Refused means the lifecycle must not. The subject keeps the state it
	// had, and the sequence continues, which is how a negative case lives
	// inside one designed workflow rather than in a file of its own.
	Refused Expectation = "refused"
)

// Identity is what a scenario is referred to by: an id and a version compared
// byte for byte. There are no ranges and no "latest".
type Identity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// UnmarshalJSON reads one identity exactly as written.
func (i *Identity) UnmarshalJSON(data []byte) error {
	var required struct {
		ID      *string `json:"id"`
		Version *string `json:"version"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Version == nil {
		return errors.New("a scenario identity requires id and version")
	}
	type identity Identity
	var decoded identity
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a scenario identity declares no member beyond id and version")
	}
	*i = Identity(decoded)
	return nil
}

// Subject is one identity the workflow keeps linked across its messages: what
// it is, which assigning authority names it, the identifier that authority
// gave it, and the lifecycle state it is in before the first step runs.
//
// Patient is the patient identity a visit or an appointment belongs to. It is
// how a merge reaches the work booked under the identity that was merged away,
// and it is required of every subject that is not itself a patient.
type Subject struct {
	ID           string `json:"id"`
	Kind         Kind   `json:"kind"`
	Namespace    string `json:"namespace"`
	Identifier   string `json:"identifier"`
	Patient      string `json:"patient,omitzero"`
	InitialState State  `json:"initial_state"`
}

// UnmarshalJSON reads one subject exactly as written. Presence is checked
// first and the same bytes are then re-read rejecting unknown members, so an
// omitted initial state is refused as omitted rather than read as a zero
// value, and a member this contract never declared is refused rather than
// ignored.
func (s *Subject) UnmarshalJSON(data []byte) error {
	var required struct {
		ID           *string `json:"id"`
		Kind         *Kind   `json:"kind"`
		Namespace    *string `json:"namespace"`
		Identifier   *string `json:"identifier"`
		InitialState *State  `json:"initial_state"`
	}
	if err := json.Unmarshal(data, &required); err != nil ||
		required.ID == nil || required.Kind == nil || required.Namespace == nil ||
		required.Identifier == nil || required.InitialState == nil {
		return errors.New("a subject requires id, kind, namespace, identifier and initial_state")
	}
	type subject Subject
	var decoded subject
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a subject declares no member beyond id, kind, namespace, identifier, patient and initial_state")
	}
	*s = Subject(decoded)
	return nil
}

// Step is one event of the sequence: which trigger event, over which subject,
// how long after the scenario's base time, and the outcome its author
// intended. Into is the surviving identity of a merge and is declared by no
// other event.
type Step struct {
	ID      string      `json:"id"`
	Event   Event       `json:"event"`
	Subject string      `json:"subject"`
	Into    string      `json:"into,omitzero"`
	After   string      `json:"after"`
	Expect  Expectation `json:"expect"`
}

// UnmarshalJSON reads one step exactly as written.
func (s *Step) UnmarshalJSON(data []byte) error {
	var required struct {
		ID      *string      `json:"id"`
		Event   *Event       `json:"event"`
		Subject *string      `json:"subject"`
		After   *string      `json:"after"`
		Expect  *Expectation `json:"expect"`
	}
	if err := json.Unmarshal(data, &required); err != nil ||
		required.ID == nil || required.Event == nil || required.Subject == nil ||
		required.After == nil || required.Expect == nil {
		return errors.New("a step requires id, event, subject, after and expect")
	}
	type step Step
	var decoded step
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a step declares no member beyond id, event, subject, into, after and expect")
	}
	*s = Step(decoded)
	return nil
}

// Scenario is one designed workflow exactly as its author wrote it. readmit
// reads it and never rewrites it: editing a scenario is editing this document.
type Scenario struct {
	Schema string `json:"schema"`
	// Scenario is what this workflow is referred to by.
	Scenario Identity `json:"scenario"`
	// Profile is the lifecycle profile that decides which subject kinds and
	// events this workflow may use.
	Profile ProfileName `json:"profile"`
	// BaseTime is the declared scenario instant every step is offset from. It
	// is never the clock: a preview of the same document is the same preview
	// on any machine on any day.
	BaseTime time.Time `json:"base_time"`
	Subjects []Subject `json:"subjects"`
	Steps    []Step    `json:"steps"`
}

// Decode reads one scenario exactly as written and checks everything that can
// be checked without walking the sequence: the contract, the bounds, the names,
// the profile's own vocabulary, and that every reference resolves. Whether a
// step is accepted or refused is a question about state, so it belongs to
// Preview and not here.
func Decode(data []byte) (Scenario, error) {
	if len(data) > MaxBytes {
		return Scenario{}, errors.New("a scenario exceeds its size limit")
	}
	var designed Scenario
	if err := json.Unmarshal(data, &designed, json.RejectUnknownMembers(true)); err != nil {
		return Scenario{}, errors.New("invalid scenario JSON")
	}
	if err := validate(designed); err != nil {
		return Scenario{}, err
	}
	return designed, nil
}

func validate(designed Scenario) error {
	if designed.Schema != Schema {
		return errors.New("a scenario must declare " + Schema)
	}
	if err := name(designed.Scenario.ID, "a scenario id"); err != nil {
		return err
	}
	if err := version(designed.Scenario.Version); err != nil {
		return err
	}
	profile, err := lookup(designed.Profile)
	if err != nil {
		return err
	}
	if err := baseTime(designed.BaseTime); err != nil {
		return err
	}
	subjects, err := declaredSubjects(designed, profile)
	if err != nil {
		return err
	}
	return declaredSteps(designed, profile, subjects)
}

// declaredSubjects checks every linked identity and returns them by their
// scenario-local name.
func declaredSubjects(designed Scenario, profile profile) (map[string]Subject, error) {
	if len(designed.Subjects) == 0 || len(designed.Subjects) > maxSubjects {
		return nil, errors.New("a scenario declares between 1 and 32 subjects")
	}
	subjects := make(map[string]Subject, len(designed.Subjects))
	for _, subject := range designed.Subjects {
		if err := name(subject.ID, "a subject id"); err != nil {
			return nil, err
		}
		if _, exists := subjects[subject.ID]; exists {
			return nil, errors.New("a subject id is declared twice")
		}
		if !profile.carries(subject.Kind) {
			return nil, errors.New("profile " + string(designed.Profile) + " declares no subject of kind " + quoted(string(subject.Kind)))
		}
		if !profile.begins(subject.Kind, subject.InitialState) {
			return nil, errors.New("profile " + string(designed.Profile) + " does not begin a " +
				string(subject.Kind) + " in the state " + quoted(string(subject.InitialState)))
		}
		if err := value(subject.Namespace, "an assigning authority"); err != nil {
			return nil, err
		}
		if err := value(subject.Identifier, "an identifier"); err != nil {
			return nil, err
		}
		if subject.Kind == PatientSubject && subject.Patient != "" {
			return nil, errors.New("a patient subject declares no patient of its own")
		}
		if subject.Kind != PatientSubject && subject.Patient == "" {
			return nil, errors.New("a " + string(subject.Kind) + " names the patient identity it belongs to")
		}
		subjects[subject.ID] = subject
	}
	// A link is resolved once every subject is known, so the order subjects
	// are written in never decides whether a scenario reads.
	for _, subject := range designed.Subjects {
		if subject.Patient == "" {
			continue
		}
		linked, declared := subjects[subject.Patient]
		if !declared || linked.Kind != PatientSubject {
			return nil, errors.New("a subject names a patient this scenario does not declare")
		}
	}
	return subjects, nil
}

// declaredSteps checks the sequence itself: the profile's vocabulary, the
// references, and the timing.
func declaredSteps(designed Scenario, profile profile, subjects map[string]Subject) error {
	if len(designed.Steps) == 0 || len(designed.Steps) > maxSteps {
		return errors.New("a scenario declares between 1 and 64 steps")
	}
	referenced := make(map[string]bool, len(subjects))
	for _, subject := range designed.Subjects {
		if subject.Patient != "" {
			referenced[subject.Patient] = true
		}
	}
	ids := make(map[string]bool, len(designed.Steps))
	previous := time.Duration(-1)
	for _, step := range designed.Steps {
		if err := name(step.ID, "a step id"); err != nil {
			return err
		}
		if ids[step.ID] {
			return errors.New("a step id is declared twice")
		}
		ids[step.ID] = true
		if step.Expect != Accepted && step.Expect != Refused {
			return errors.New("a step declares its expected outcome as accepted or refused; an outcome is never assumed")
		}
		transition, declared := profile.events[step.Event]
		if !declared {
			return errors.New("profile " + string(designed.Profile) + " declares no event " + quoted(string(step.Event)))
		}
		subject, exists := subjects[step.Subject]
		if !exists {
			return errors.New("a step names a subject this scenario does not declare")
		}
		if subject.Kind != transition.kind {
			return errors.New("event " + string(step.Event) + " acts on a " + string(transition.kind) + ", not a " + string(subject.Kind))
		}
		referenced[step.Subject] = true
		if err := merged(step, transition, subjects); err != nil {
			return err
		}
		if step.Into != "" {
			referenced[step.Into] = true
		}
		offset, err := timing(step.After, previous, designed.BaseTime)
		if err != nil {
			return err
		}
		previous = offset
	}
	for _, subject := range designed.Subjects {
		if !referenced[subject.ID] {
			return errors.New("a scenario declares a subject no step reaches; remove it or give it a step")
		}
	}
	return nil
}

// merged checks the surviving identity a merge names, and that no other event
// names one. Whether that merge is legal from the state the sequence reached
// is a transition question, answered by Preview.
func merged(step Step, transition transition, subjects map[string]Subject) error {
	if !transition.merge {
		if step.Into != "" {
			return errors.New("only a merge names a surviving identity; event " + string(step.Event) + " does not")
		}
		return nil
	}
	if step.Into == "" {
		return errors.New("a merge names the surviving identity it merges into")
	}
	surviving, exists := subjects[step.Into]
	if !exists || surviving.Kind != PatientSubject {
		return errors.New("a merge names a surviving patient this scenario does not declare")
	}
	return nil
}

// timing reads one step's offset from the base time. Offsets rise strictly, so
// the document's order is the sequence's order and a preview never has to
// choose between two events at one instant. Out-of-order arrival is a variant
// of a designed sequence, not a way to write one down.
func timing(after string, previous time.Duration, base time.Time) (time.Duration, error) {
	offset, err := offsetOf(after)
	if err != nil {
		return 0, err
	}
	if offset < 0 || offset > maxOffset {
		return 0, errors.New("a step sits between 0s and 8760h after the base time")
	}
	if offset%time.Second != 0 {
		return 0, errors.New("a step's offset from the base time is a whole number of seconds")
	}
	if offset <= previous {
		return 0, errors.New("each step sits strictly later than the step before it")
	}
	if base.Add(offset).Year() > 9999 {
		return 0, errors.New("a step falls outside years 0001 through 9999")
	}
	return offset, nil
}

// offsetOf reads one step's declared offset. It is the one place that turns
// that member into a duration, so the reader and the preview cannot disagree
// about what a step's timing says.
func offsetOf(after string) (time.Duration, error) {
	offset, err := time.ParseDuration(after)
	if err != nil {
		return 0, errors.New("a step's offset from the base time is a duration such as 0s, 30m or 24h")
	}
	return offset, nil
}

// baseTime holds the scenario's declared instant to the same precision HL7
// carries and `readmit synth` declares, so a designed time is never a time no
// message could state.
func baseTime(base time.Time) error {
	if base.IsZero() || base.Year() < 1 || base.Year() > 9999 || base.Nanosecond() != 0 {
		return errors.New("a base time is a nonzero whole second within years 0001 through 9999")
	}
	return nil
}

// name bounds the scenario-local names a person reads in the preview. It is
// the same shape a queued job id and a spec assertion id require, so a person
// reading any of those documents is reading one convention.
func name(id, what string) error {
	if id == "" || len(id) > maxNameBytes || id[0] < 'a' || id[0] > 'z' {
		return errors.New(what + " begins with a lowercase letter and is at most 64 bytes")
	}
	for _, r := range id {
		if r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return errors.New(what + " holds lowercase letters, digits and '-' only")
		}
	}
	return nil
}

func version(declared string) error {
	if declared == "" || len(declared) > maxVersionBytes {
		return errors.New("a scenario version is between 1 and 32 bytes")
	}
	for _, r := range declared {
		if r != '.' && r != '-' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return errors.New("a scenario version holds letters, digits, '.' and '-' only")
		}
	}
	return nil
}

// value bounds one declared identifier or assigning authority and refuses the
// bytes that would stop it being one field of one message: the HL7 delimiters
// this fixture profile fixes, and anything outside printable ASCII.
func value(declared, what string) error {
	if declared == "" || len(declared) > maxValueBytes {
		return errors.New(what + " is between 1 and 64 bytes")
	}
	for i := 0; i < len(declared); i++ {
		c := declared[i]
		if c < 0x21 || c > 0x7e {
			return errors.New(what + " holds printable ASCII without spaces")
		}
		if c == '|' || c == '^' || c == '~' || c == '\\' || c == '&' {
			return errors.New(what + " holds none of the HL7 delimiters | ^ ~ \\ &")
		}
	}
	return nil
}

func quoted(value string) string { return "\"" + value + "\"" }
