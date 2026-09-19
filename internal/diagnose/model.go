// Package diagnose evaluates small, named fixture profiles and their rulesets
// against verified case evidence. Findings describe the capture window, never a
// complete lifecycle.
package diagnose

import (
	"time"

	"github.com/bharm16/readmit/internal/hl7"
)

const (
	Schema             = "readmit-diagnosis/v1"
	ConfigSchema       = "readmit-diagnose-config/v1"
	Profile            = "readmit-siu-v1"
	Ruleset            = "readmit-siu-diagnosis/v1"
	DuplicateControl   = "message.duplicate-control-id"
	ACKOutcome         = "ack.msa-outcome"
	ACKError           = "ack.err-outcome"
	RequiredField      = "siu.required-field"
	BookingNotObserved = "siu.booking-not-observed"
)

// The lifecycle ruleset is a separate named contract over ADT identity/visit and
// SIU appointment occurrences. It never changes a byte of readmit-siu-v1.
const (
	LifecycleProfile           = "readmit-lifecycle-v1"
	LifecycleRuleset           = "readmit-lifecycle-diagnosis/v1"
	LifecycleRequiredField     = "lifecycle.required-field"
	EventTypeMismatch          = "lifecycle.event-type-mismatch"
	VisitNotObserved           = "lifecycle.visit-not-observed"
	AppointmentNotObserved     = "lifecycle.appointment-not-observed"
	MergeIdentifierNotObserved = "lifecycle.merge-identifier-not-observed"
)

var supportedRules = []string{DuplicateControl, ACKOutcome, ACKError, RequiredField, BookingNotObserved}

var lifecycleRules = []string{DuplicateControl, ACKOutcome, ACKError, LifecycleRequiredField, EventTypeMismatch, VisitNotObserved, AppointmentNotObserved, MergeIdentifierNotObserved}

type Namespace struct {
	Key             string `json:"key"`
	Namespace       string `json:"namespace"`
	UniversalID     string `json:"universal_id"`
	UniversalIDType string `json:"universal_id_type"`
}

type Config struct {
	Schema     string      `json:"schema"`
	Profile    string      `json:"profile"`
	Ruleset    string      `json:"ruleset"`
	Rules      []string    `json:"rules"`
	Namespaces []Namespace `json:"namespaces"`
}

type Window struct {
	Description          string         `json:"description"`
	Occurrences          int            `json:"occurrences"`
	ObservedStart        *time.Time     `json:"observed_start"`
	ObservedEnd          *time.Time     `json:"observed_end"`
	UnknownObservedTimes int            `json:"unknown_observed_times"`
	Sources              []SourceWindow `json:"sources"`
}

type SourceWindow struct {
	SourceID        string `json:"source_id"`
	FirstOccurrence string `json:"first_occurrence"`
	LastOccurrence  string `json:"last_occurrence"`
}

type Evidence struct {
	Occurrence string    `json:"occurrence"`
	Field      string    `json:"field"`
	State      hl7.State `json:"state"`
	Offset     *int      `json:"offset"`
	Length     *int      `json:"length"`
}

type Finding struct {
	ID             string     `json:"id"`
	RuleID         string     `json:"rule_id"`
	Classification string     `json:"classification"`
	Profile        string     `json:"profile"`
	Ruleset        string     `json:"ruleset"`
	Summary        string     `json:"summary"`
	Window         string     `json:"window,omitempty"`
	Evidence       []Evidence `json:"evidence"`
}

type Unsupported struct {
	Code       string `json:"code"`
	Occurrence string `json:"occurrence,omitempty"`
	Field      string `json:"field,omitempty"`
	Detail     string `json:"detail"`
}

type Report struct {
	Schema       string        `json:"schema"`
	ConfigSHA256 string        `json:"config_sha256"`
	CaseIdentity string        `json:"case_identity"`
	Profile      string        `json:"profile"`
	Ruleset      string        `json:"ruleset"`
	Rules        []string      `json:"rules"`
	Window       Window        `json:"window"`
	Findings     []Finding     `json:"findings"`
	Unsupported  []Unsupported `json:"unsupported"`
	Scope        string        `json:"scope"`
	NoFindings   string        `json:"no_findings,omitempty"`
}
