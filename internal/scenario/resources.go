package scenario

import (
	"encoding/json/v2"
	"errors"
)

// ResourceSchema is readmit-scenario/v1 with resource subjects: a service or
// resource participating in an appointment, whose participation is added and
// cancelled by the scheduling events HL7 defines for that. The v1 reader,
// its profiles and every v1 document are unchanged.
const ResourceSchema = "readmit-scenario/v2"

const (
	// SIUResourceLifecycle is readmit-siu-lifecycle-v1 with resource
	// participation: every v1 event, plus S18 (addition of a service or
	// resource on an appointment) and S20 (cancellation of its participation).
	SIUResourceLifecycle ProfileName = "readmit-siu-lifecycle-v2"
	// ResourceSubject is one service or resource participating in an
	// appointment.
	ResourceSubject Kind = "resource"

	// ResourceNone is a resource not participating in its appointment yet.
	ResourceNone State = "none"
	// ResourceBooked is a resource participating in its appointment.
	ResourceBooked State = "booked"
	// ResourceCancelled is a resource whose participation was cancelled. A
	// cancellation is not a deletion: the participation was valid and is
	// stopped, and the resource is never added again by this profile.
	ResourceCancelled State = "cancelled"
)

// resourceProfiles are the lifecycle profiles only readmit-scenario/v2 may
// name. A v1 document naming one is refused, as it names no v2 member.
var resourceProfiles = map[ProfileName]profile{
	SIUResourceLifecycle: {
		states: map[Kind]map[State]bool{
			PatientSubject: {PatientActive: true},
			AppointmentSubject: {AppointmentNone: true, AppointmentBooked: true,
				AppointmentCancelled: true, AppointmentNoShow: true},
			ResourceSubject: {ResourceNone: true, ResourceBooked: true, ResourceCancelled: true},
		},
		events: map[Event]transition{
			"S12": profiles[SIULifecycle].events["S12"],
			"S13": profiles[SIULifecycle].events["S13"],
			"S14": profiles[SIULifecycle].events["S14"],
			"S15": profiles[SIULifecycle].events["S15"],
			"S26": profiles[SIULifecycle].events["S26"],
			"S18": {kind: ResourceSubject, description: "addition of service/resource on appointment", from: []State{ResourceNone}, to: ResourceBooked},
			"S20": {kind: ResourceSubject, description: "cancellation of service/resource on appointment", from: []State{ResourceBooked}, to: ResourceCancelled},
		},
	},
}

func lookupResources(named ProfileName) (profile, error) {
	bound, known := resourceProfiles[named]
	if !known {
		return profile{}, errors.New("a " + ResourceSchema + " scenario names " + string(SIUResourceLifecycle))
	}
	return bound, nil
}

// DecodeResources reads one readmit-scenario/v2 workflow exactly as written
// and checks it as Decode checks v1, against the v2 profiles.
func DecodeResources(data []byte) (Scenario, error) {
	if len(data) > MaxBytes {
		return Scenario{}, errors.New("a scenario exceeds its size limit")
	}
	var designed Scenario
	if err := json.Unmarshal(data, &designed, json.RejectUnknownMembers(true)); err != nil {
		return Scenario{}, errors.New("invalid scenario JSON")
	}
	if designed.Schema != ResourceSchema {
		return Scenario{}, errors.New("a scenario must declare " + ResourceSchema)
	}
	if err := validateWith(designed, lookupResources); err != nil {
		return Scenario{}, err
	}
	return designed, nil
}

// PreviewResources walks a v2 workflow through its profile's operators.
func PreviewResources(designed Scenario) (Timeline, error) {
	if err := validateWith(designed, lookupResources); err != nil {
		return Timeline{}, err
	}
	bound, _ := lookupResources(designed.Profile)
	return preview(designed, bound)
}
