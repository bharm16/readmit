package receiver

import (
	_ "embed"
	"encoding/json/v2"
	"errors"
	"regexp"

	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
)

//go:embed profiles/readmit-siu-v1.json
var fixtureProfileJSON []byte

type action string

const (
	book       action = "book"
	reschedule action = "reschedule"
)

type segmentRule struct {
	Segment  string `json:"segment"`
	Operator string `json:"operator"`
	Count    int    `json:"count"`
}

type identifierFields struct {
	Value           string `json:"value"`
	Namespace       string `json:"namespace"`
	UniversalID     string `json:"universal_id"`
	UniversalIDType string `json:"universal_id_type"`
}

func (i identifierFields) paths() []string {
	return []string{i.Value, i.Namespace, i.UniversalID, i.UniversalIDType}
}

type timeRule struct {
	Selector string `json:"selector"`
	Operator string `json:"operator"`
}

// A profile selects the inputs to a small, fixed set of Go operations. It is
// data, not executable expressions or a pluggable rule engine.
type receiverProfile struct {
	Schema              string            `json:"schema"`
	Name                string            `json:"name"`
	HL7Version          string            `json:"hl7_version"`
	MessageType         string            `json:"message_type"`
	AcknowledgementMode string            `json:"acknowledgement_mode"`
	FieldSeparator      string            `json:"field_separator"`
	EncodingCharacters  string            `json:"encoding_characters"`
	Triggers            map[string]action `json:"triggers"`
	RequiredSegments    []segmentRule     `json:"required_segments"`
	PatientIdentifier   identifierFields  `json:"patient_identifier"`
	PlacerIdentifier    identifierFields  `json:"placer_identifier"`
	FillerIdentifier    identifierFields  `json:"filler_identifier"`
	AppointmentStart    timeRule          `json:"appointment_start"`
	MaxTextBytes        int               `json:"max_text_bytes"`
}

var segmentName = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}$`)

func decodeProfile(data []byte) (receiverProfile, error) {
	var profile receiverProfile
	invalid := errors.New("invalid embedded receiver profile")
	if len(data) > 64<<10 {
		return profile, invalid
	}
	if err := json.Unmarshal(data, &profile, json.RejectUnknownMembers(true)); err != nil {
		return profile, invalid
	}
	if profile.Schema != "readmit-receiver-profile/v1" || profile.Name != observation.Profile || profile.HL7Version == "" || profile.MessageType == "" || profile.AcknowledgementMode != "original" || profile.FieldSeparator != "|" || profile.EncodingCharacters != `^~\&` || profile.MaxTextBytes < 1 || profile.MaxTextBytes > 1024 {
		return profile, invalid
	}
	if len(profile.Triggers) != 2 || len(profile.RequiredSegments) != 2 || profile.AppointmentStart.Operator != "hl7-ts-whole-seconds-optional-offset" {
		return profile, invalid
	}
	actions := make(map[action]bool)
	for trigger, operation := range profile.Triggers {
		if !segmentName.MatchString(trigger) || operation != book && operation != reschedule || actions[operation] {
			return profile, invalid
		}
		actions[operation] = true
	}
	seenSegments := make(map[string]bool)
	for _, rule := range profile.RequiredSegments {
		if !segmentName.MatchString(rule.Segment) || rule.Operator != "exact-count" || rule.Count != 1 || seenSegments[rule.Segment] {
			return profile, invalid
		}
		seenSegments[rule.Segment] = true
	}
	paths := []string{profile.AppointmentStart.Selector}
	for _, identifier := range []identifierFields{profile.PatientIdentifier, profile.PlacerIdentifier, profile.FillerIdentifier} {
		paths = append(paths, identifier.paths()...)
	}
	for _, path := range paths {
		if _, err := hl7.ParseSelector(path); err != nil {
			return profile, invalid
		}
	}
	return profile, nil
}
