package scenario

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ProfileName is one implemented lifecycle profile. A scenario names one, and
// what that profile declares is the whole of what the scenario may use.
type ProfileName string

const (
	// ADTLifecycle is the readmit fixture lifecycle for patient
	// administration: a visit registered, admitted, transferred, updated,
	// discharged, and the cancellations of those; and a patient identity
	// merged into another.
	ADTLifecycle ProfileName = "readmit-adt-lifecycle-v1"
	// SIULifecycle is the readmit fixture lifecycle for scheduling: an
	// appointment booked, rescheduled, modified, cancelled, or recorded as a
	// patient who did not arrive.
	SIULifecycle ProfileName = "readmit-siu-lifecycle-v1"
)

// Kind is what a linked identity is. A profile declares which kinds it carries
// and an event acts on exactly one of them.
type Kind string

const (
	// PatientSubject is the patient identity every other subject belongs to.
	PatientSubject Kind = "patient"
	// VisitSubject is one episode of care under a patient identity.
	VisitSubject Kind = "visit"
	// AppointmentSubject is one booking under a patient identity.
	AppointmentSubject Kind = "appointment"
)

// State is the lifecycle state one subject is in. States are per kind: a visit
// is never in an appointment's state, and neither is ever in a state some
// other profile declares.
type State string

const (
	// PatientActive is a patient identity that still stands.
	PatientActive State = "active"
	// PatientMerged is a patient identity that was merged into another. It
	// survives nothing: no event reaches it or the visits and appointments
	// booked under it.
	PatientMerged State = "merged"

	// VisitNone is a visit nothing has been recorded against yet.
	VisitNone State = "none"
	// VisitPreadmit is a registered visit that has not been admitted.
	VisitPreadmit State = "preadmit"
	// VisitAdmitted is an admitted visit.
	VisitAdmitted State = "admitted"
	// VisitDischarged is a discharged visit.
	VisitDischarged State = "discharged"
	// VisitCancelled is a visit whose admission was cancelled.
	VisitCancelled State = "cancelled"

	// AppointmentNone is an appointment nothing has been booked for yet.
	AppointmentNone State = "none"
	// AppointmentBooked is a booked appointment.
	AppointmentBooked State = "booked"
	// AppointmentCancelled is a cancelled appointment.
	AppointmentCancelled State = "cancelled"
	// AppointmentNoShow is an appointment the patient did not arrive for.
	AppointmentNoShow State = "noshow"
)

// Event is one trigger event of a lifecycle profile.
type Event string

// transition is one typed lifecycle operator: what the event acts on, what it
// means, the states it may be taken from, and the state it leaves behind.
//
// keeps is how an event that reports rather than moves says so: an update
// carries new information about a visit and leaves that visit exactly where it
// was. A merge is the one operator that reads a second subject, so it says so
// rather than being recognized by its name anywhere else.
type transition struct {
	kind        Kind
	description string
	from        []State
	to          State
	keeps       bool
	merge       bool
}

// profile is one implemented lifecycle profile: the subject kinds it carries
// with the states each of them may be in, and the events it declares over
// them.
//
// States belong to the profile and not to the kind alone. A scheduling
// scenario's patient identity is only ever active, because this profile
// declares no merge; declaring one merged would let every appointment step
// refuse for a reason no event of this profile could ever have caused.
type profile struct {
	states map[Kind]map[State]bool
	events map[Event]transition
}

func (p profile) carries(kind Kind) bool {
	_, declared := p.states[kind]
	return declared
}

// begins answers whether a scenario may declare a subject of this kind already
// in this state before its first step.
func (p profile) begins(kind Kind, state State) bool { return p.states[kind][state] }

// mergePatient is shared by every profile that carries a merge: a patient
// identity that still stands is merged into another that still stands, and
// stops standing.
var mergePatient = transition{
	kind: PatientSubject, description: "merge patient identifier list",
	from: []State{PatientActive}, to: PatientMerged, merge: true,
}

// profiles is the closed set of implemented lifecycle profiles. A profile
// outside it is refused by name; nothing is inferred from a profile that looks
// similar, and no event is borrowed across profiles.
var profiles = map[ProfileName]profile{
	ADTLifecycle: {
		states: map[Kind]map[State]bool{
			PatientSubject: {PatientActive: true, PatientMerged: true},
			VisitSubject: {VisitNone: true, VisitPreadmit: true, VisitAdmitted: true,
				VisitDischarged: true, VisitCancelled: true},
		},
		events: map[Event]transition{
			"A04": {kind: VisitSubject, description: "register a patient", from: []State{VisitNone}, to: VisitPreadmit},
			"A01": {kind: VisitSubject, description: "admit or visit notification", from: []State{VisitNone, VisitPreadmit}, to: VisitAdmitted},
			"A02": {kind: VisitSubject, description: "transfer a patient", from: []State{VisitAdmitted}, to: VisitAdmitted},
			"A08": {kind: VisitSubject, description: "update patient information", from: []State{VisitPreadmit, VisitAdmitted, VisitDischarged}, keeps: true},
			"A03": {kind: VisitSubject, description: "discharge or end visit", from: []State{VisitAdmitted}, to: VisitDischarged},
			"A11": {kind: VisitSubject, description: "cancel admit or visit notification", from: []State{VisitAdmitted}, to: VisitCancelled},
			"A13": {kind: VisitSubject, description: "cancel discharge or end visit", from: []State{VisitDischarged}, to: VisitAdmitted},
			"A40": mergePatient,
		},
	},
	SIULifecycle: {
		states: map[Kind]map[State]bool{
			PatientSubject: {PatientActive: true},
			AppointmentSubject: {AppointmentNone: true, AppointmentBooked: true,
				AppointmentCancelled: true, AppointmentNoShow: true},
		},
		events: map[Event]transition{
			"S12": {kind: AppointmentSubject, description: "new appointment booking", from: []State{AppointmentNone}, to: AppointmentBooked},
			"S13": {kind: AppointmentSubject, description: "appointment rescheduling", from: []State{AppointmentBooked}, to: AppointmentBooked},
			"S14": {kind: AppointmentSubject, description: "appointment modification", from: []State{AppointmentBooked}, to: AppointmentBooked},
			"S15": {kind: AppointmentSubject, description: "appointment cancellation", from: []State{AppointmentBooked}, to: AppointmentCancelled},
			"S26": {kind: AppointmentSubject, description: "patient did not show up for appointment", from: []State{AppointmentBooked}, to: AppointmentNoShow},
		},
	},
}

// implemented lists the lifecycle profiles in the order a refusal names them.
var implemented = []ProfileName{ADTLifecycle, SIULifecycle}

// lookup resolves the profile a scenario names. It is the one place that
// refuses an unimplemented profile, so a reader and a preview cannot disagree
// about which profiles exist.
func lookup(named ProfileName) (profile, error) {
	bound, known := profiles[named]
	if !known {
		names := make([]string, 0, len(implemented))
		for _, declared := range implemented {
			names = append(names, string(declared))
		}
		return profile{}, errors.New("a scenario names one implemented lifecycle profile; supported: " + strings.Join(names, ", "))
	}
	return bound, nil
}

// Occurrence is one step of a previewed workflow: where it sits in the
// sequence, the instant it declares, what it does, and what the profile's
// transition operators make of it.
//
// It names the scenario-local subject, never the identifier that subject
// declares. A preview is a picture of a sequence, and the identifiers a
// scenario carries are for whatever generates messages from it.
type Occurrence struct {
	Ordinal     int
	ID          string
	At          time.Time
	Event       Event
	Description string
	Subject     string
	Into        string
	Expect      Expectation
	From        State
	To          State
	// Reason is why the lifecycle refused this step. It is present exactly
	// when the step was refused, which is exactly when the step declared
	// Refused, because a disagreement is not previewed at all.
	Reason string
}

// Timeline is the whole previewed workflow: the scenario's own identity, the
// profile that decided it, the base time every step is offset from, and each
// step in the order it happens.
type Timeline struct {
	Scenario Identity
	Profile  ProfileName
	BaseTime time.Time
	Subjects []Subject
	Steps    []Occurrence
	Accepted int
	Refused  int
}

// Preview walks the sequence through the bound profile's typed transition
// operators and reports what happens at each step.
//
// It is where an authored expectation meets the lifecycle. A step the
// lifecycle refuses where its author declared it accepted, and a step the
// lifecycle takes where its author declared it refused, each refuse the whole
// scenario by name: a workflow whose negative cases are not negative is worse
// than no workflow, because nobody reading it can tell which they have. A
// refused step leaves every subject exactly as it was, so the steps after it
// are read against the state the sequence actually reached.
//
// Preview reads. It writes nothing, generates no message bytes, and its result
// is a pure function of the document: the same scenario previews identically
// on every machine and on every day.
func Preview(designed Scenario) (Timeline, error) {
	bound, err := lookup(designed.Profile)
	if err != nil {
		return Timeline{}, err
	}
	reached := walk(designed.Subjects)
	timeline := Timeline{
		Scenario: designed.Scenario, Profile: designed.Profile,
		BaseTime: designed.BaseTime.UTC(), Subjects: slices.Clone(designed.Subjects),
		Steps: make([]Occurrence, 0, len(designed.Steps)),
	}
	for index, step := range designed.Steps {
		offset, err := offsetOf(step.After)
		if err != nil {
			return Timeline{}, err
		}
		operator, declared := bound.events[step.Event]
		if !declared {
			return Timeline{}, errors.New("profile " + string(designed.Profile) + " declares no event " + quoted(string(step.Event)))
		}
		from, exists := reached.state[step.Subject]
		if !exists {
			return Timeline{}, errors.New("a step names a subject this scenario does not declare")
		}
		reason := reached.refuse(step, operator)
		occurrence := Occurrence{
			Ordinal: index + 1, ID: step.ID, At: designed.BaseTime.UTC().Add(offset),
			Event: step.Event, Description: operator.description, Subject: step.Subject,
			Into: step.Into, Expect: step.Expect, From: from, To: from, Reason: reason,
		}
		if reason == "" {
			if step.Expect != Accepted {
				return Timeline{}, errors.New("step " + strconv.Itoa(index+1) + " " + quoted(step.ID) +
					" is declared refused, but profile " + string(designed.Profile) + " takes it")
			}
			if !operator.keeps {
				reached.state[step.Subject] = operator.to
			}
			occurrence.To = reached.state[step.Subject]
			timeline.Accepted++
		} else {
			if step.Expect != Refused {
				return Timeline{}, errors.New("step " + strconv.Itoa(index+1) + " " + quoted(step.ID) +
					" is declared accepted, but profile " + string(designed.Profile) + " refuses it: " + reason)
			}
			timeline.Refused++
		}
		timeline.Steps = append(timeline.Steps, occurrence)
	}
	return timeline, nil
}

// sequence is the state one preview has reached: where every identity is now,
// and which patient identity each of them belongs to.
type sequence struct {
	state   map[string]State
	patient map[string]string
}

// walk starts a sequence from the states its subjects declare they begin in.
func walk(subjects []Subject) sequence {
	reached := sequence{
		state:   make(map[string]State, len(subjects)),
		patient: make(map[string]string, len(subjects)),
	}
	for _, subject := range subjects {
		reached.state[subject.ID] = subject.InitialState
		reached.patient[subject.ID] = subject.Patient
	}
	return reached
}

// refuse answers why the lifecycle will not take one step, or "" when it will.
// Every answer is a state the sequence reached, never a shape the document
// has: Decode already refused a document whose shape is wrong.
func (s sequence) refuse(step Step, operator transition) string {
	// A patient identity that was merged away survives nothing. The work
	// booked under it is reached through the identity it was merged into, and
	// readmit will not pretend a message addressed to the old one lands.
	if patient := s.patient[step.Subject]; patient != "" && s.state[patient] == PatientMerged {
		return "the patient identity this " + string(operator.kind) + " belongs to was merged away"
	}
	if !slices.Contains(operator.from, s.state[step.Subject]) {
		return string(step.Event) + " (" + operator.description + ") is not taken from " + quoted(string(s.state[step.Subject]))
	}
	if operator.merge {
		if step.Into == step.Subject {
			return "a patient identity is never merged into itself"
		}
		if s.state[step.Into] == PatientMerged {
			return "the surviving identity was itself merged away"
		}
	}
	return ""
}
