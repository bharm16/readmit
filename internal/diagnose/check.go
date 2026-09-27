package diagnose

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
)

// ConfigSHA256 is the identity a report records for the configuration it ran
// under: the SHA-256 of its deterministic encoding, in lowercase hexadecimal.
// Two configurations with the same identity select the same evaluation.
func ConfigSHA256(config Config) (string, error) {
	data, err := json.Marshal(config, json.Deterministic(true))
	if err != nil {
		return "", errors.New("cannot encode diagnosis configuration")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// checked is what a configuration selects over one case before any
// occurrence is read: the rules that will be evaluated, whether the profile's
// own rules run at all, and the unsupported items that say why not.
type checked struct {
	rules            map[string]bool
	profileSupported bool
	unsupported      []Unsupported
}

// preflight decides, from the configuration and the generator profile a
// bundle declares, which rules a diagnosis evaluates. Run records exactly
// these items first, in this order; Check answers them without reading an
// occurrence.
func preflight(config Config, set rulesetDefinition, rulesetSupported bool, generator *bundle.GeneratorInputs) checked {
	out := checked{rules: make(map[string]bool), unsupported: []Unsupported{}}
	add := func(code, detail string) {
		out.unsupported = append(out.unsupported, Unsupported{Code: code, Detail: detail})
	}
	// Without a named ruleset no rule identifier has a meaning here, so a rule is
	// only unsupported when no registered ruleset defines it, and a profile only
	// when no registered ruleset names it. Nothing is evaluated either way.
	known := set.rules
	if !rulesetSupported {
		known = registeredRules()
	}
	for _, rule := range config.Rules {
		if !slices.Contains(known, rule) {
			add("unsupported_rule", "A configured rule is unsupported: "+rule)
			continue
		}
		out.rules[rule] = true
	}
	out.profileSupported = rulesetSupported && config.Profile == set.profile
	if !registeredProfile(config.Profile) || rulesetSupported && !out.profileSupported {
		add("unsupported_profile", "The configured profile is unsupported: "+config.Profile)
	}
	if !rulesetSupported {
		add("unsupported_ruleset", "The configured ruleset is unsupported: "+config.Ruleset)
	}
	if !out.profileSupported {
		clear(out.rules)
	}
	if generator != nil && generator.ProfileVersion != set.profile {
		add("unsupported_bundle_profile", "The bundle declares an unsupported generator profile; "+set.profileKind+" profile rules were not evaluated.")
		out.profileSupported = false
		for _, rule := range set.profileRules {
			delete(out.rules, rule)
		}
	}
	return out
}

// Check answers, without interpreting any occurrence, the profile-level
// unsupported items a diagnosis of the case at path under config would
// record first: rules, a profile or a ruleset this release does not define,
// and a generator profile the case declares that the ruleset's profile rules
// do not cover. It reads the case's manifest alone; Run still verifies the
// whole bundle. An invalid configuration is an error, as it is for Run.
func Check(path string, config Config) ([]Unsupported, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	set, rulesetSupported := rulesetFor(config.Ruleset)
	manifest, err := bundle.Describe(path)
	if err != nil {
		return nil, err
	}
	return preflight(config, set, rulesetSupported, manifest.Provenance.Generator).unsupported, nil
}

// Refuses reports whether one preflight item means the selected profile's
// evaluation does not run as selected: the profile or ruleset is unknown, or
// the case was generated under another profile. A rule this release does not
// define is reported and skipped; the others still run.
func Refuses(item Unsupported) bool {
	switch item.Code {
	case "unsupported_profile", "unsupported_ruleset", "unsupported_bundle_profile":
		return true
	}
	return false
}

// Severity is how much one rule's findings matter to a person, as this
// release declares it for the rule. It is display metadata only: a report,
// its identity and the command line's output never carry it, and it is never
// derived from a rule's name or a finding's summary.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Rank orders severities for a person: an error before a warning before
// information, and all of them before a rule with no declared severity.
func (s Severity) Rank() int {
	switch s {
	case SeverityError:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

// RuleInfo names one rule of one registered ruleset for a person: its
// identifier, the ruleset it belongs to, a short human name and the severity
// this release declares for it, when it declares one.
type RuleInfo struct {
	ID       string   `json:"id"`
	Ruleset  string   `json:"ruleset"`
	Name     string   `json:"name"`
	Severity Severity `json:"severity,omitzero"`
}

// ruleNames are the human names of every registered rule. A rule shared by
// several rulesets means the same thing in each and has one name.
var ruleNames = map[string]string{
	DuplicateControl:           "Repeated control ID",
	ACKOutcome:                 "Acknowledgement outcome",
	ACKError:                   "Acknowledgement error",
	RequiredField:              "Missing required field",
	BookingNotObserved:         "Booking not observed",
	LifecycleRequiredField:     "Missing required field",
	EventTypeMismatch:          "Event type mismatch",
	VisitNotObserved:           "Visit not observed",
	AppointmentNotObserved:     "Appointment not observed",
	MergeIdentifierNotObserved: "Merged identifier not observed",
	ACKStageNotObserved:        "Missing acknowledgement",
	ACKErrorLocation:           "Acknowledgement error location",
	OrderRequiredField:         "Missing required field",
	OrderNotObserved:           "Order not observed",
	DuplicateOutput:            "Repeated output",
	StatusProgression:          "Conflicting final status",
}

// ruleSeverities are the severities this release declares for its rules. A
// rule shared by several rulesets has one severity.
var ruleSeverities = map[string]Severity{
	DuplicateControl:           SeverityWarning,
	ACKOutcome:                 SeverityError,
	ACKError:                   SeverityError,
	RequiredField:              SeverityError,
	BookingNotObserved:         SeverityWarning,
	LifecycleRequiredField:     SeverityError,
	EventTypeMismatch:          SeverityError,
	VisitNotObserved:           SeverityWarning,
	AppointmentNotObserved:     SeverityWarning,
	MergeIdentifierNotObserved: SeverityWarning,
	ACKStageNotObserved:        SeverityWarning,
	ACKErrorLocation:           SeverityInfo,
	OrderRequiredField:         SeverityError,
	OrderNotObserved:           SeverityWarning,
	DuplicateOutput:            SeverityWarning,
	StatusProgression:          SeverityError,
}

// RuleSeverity is the severity this release declares for one rule, or empty
// for a rule it does not define.
func RuleSeverity(rule string) Severity { return ruleSeverities[rule] }

// Rules lists every rule of every registered ruleset, in registration order.
func Rules() []RuleInfo {
	var rules []RuleInfo
	for _, set := range rulesets {
		for _, rule := range set.rules {
			rules = append(rules, RuleInfo{ID: rule, Ruleset: set.ruleset, Name: ruleNames[rule], Severity: ruleSeverities[rule]})
		}
	}
	return rules
}
