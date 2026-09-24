package diagnose

import (
	"bytes"
	_ "embed"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/hl7"
)

//go:embed profile.json
var profileJSON []byte

//go:embed lifecycle-profile.json
var lifecycleProfileJSON []byte

//go:embed order-profile.json
var orderProfileJSON []byte

// ProfileSnapshot returns the actual embedded readmit-siu-v1 profile and ruleset
// definition for retained evidence, without exposing mutable package storage.
func ProfileSnapshot() []byte { return bytes.Clone(profileJSON) }

type profileDefinition struct {
	Profile         string              `json:"profile"`
	Ruleset         string              `json:"ruleset"`
	HL7Version      string              `json:"hl7_version"`
	Statuses        []statusDefinition  `json:"statuses"`
	Triggers        []triggerDefinition `json:"triggers"`
	AuthorityFields [][]string          `json:"authority_fields"`
}

// statusDefinition is one declared status code of one named vocabulary and
// whether this profile treats it as the final word about its identity. Finality
// is the profile's decision, not a property of the code: a second profile may
// declare the same code and not end there. The prose that explains a code lives
// in Go, so a document declares only data.
type statusDefinition struct {
	Vocabulary string `json:"vocabulary"`
	Code       string `json:"code"`
	Final      bool   `json:"final"`
}

type triggerDefinition struct {
	Kind            string        `json:"kind"`
	Code            string        `json:"code"`
	RequiredFields  []string      `json:"required_fields"`
	AuthorityFields [][]string    `json:"authority_fields"`
	EventType       string        `json:"event_type"`
	ScalarFields    []string      `json:"scalar_fields"`
	Output          output        `json:"output"`
	Correlations    []correlation `json:"correlations"`
}

// output declares that an occurrence reports one identity as standing in one
// declared status. It says nothing about when: the rules it feeds compare the
// statuses one identity carries inside the window and never order them.
type output struct {
	Identity   string   `json:"identity"`
	Authority  []string `json:"authority"`
	Status     string   `json:"status"`
	Vocabulary string   `json:"vocabulary"`
	Evidence   []string `json:"evidence"`
}

func (o output) declared() bool { return o.Identity != "" }

// correlation declares that an occurrence either establishes the identity one
// rule is about or depends on one. It relates captured identities inside one
// window; it is not a transition model and never orders occurrences.
type correlation struct {
	Rule      string   `json:"rule"`
	Role      string   `json:"role"`
	Value     string   `json:"value"`
	Authority []string `json:"authority"`
	Evidence  []string `json:"evidence"`
}

const (
	antecedent = "antecedent"
	subsequent = "subsequent"
)

// rulesetDefinition names one selectable diagnosis contract and the embedded
// document it interprets. Profile rules are evaluated only when both the
// configured and the bundle generator profile are the ruleset's own; message
// rules never depend on a profile. statesWindow repeats the observed capture
// window on every finding the ruleset produces.
type rulesetDefinition struct {
	ruleset      string
	profile      string
	rules        []string
	profileRules []string
	outputRules  []string
	requiredRule string
	profileKind  string
	statesWindow bool
	document     []byte
	normalize    func(*profileDefinition)
}

var rulesets = []rulesetDefinition{
	{ruleset: Ruleset, profile: Profile, rules: supportedRules, profileRules: []string{RequiredField, BookingNotObserved}, requiredRule: RequiredField, profileKind: "SIU", document: profileJSON, normalize: siuBookings},
	{ruleset: LifecycleRuleset, profile: LifecycleProfile, rules: lifecycleRules, profileRules: []string{LifecycleRequiredField, EventTypeMismatch, VisitNotObserved, AppointmentNotObserved, MergeIdentifierNotObserved}, requiredRule: LifecycleRequiredField, profileKind: "lifecycle", statesWindow: true, document: lifecycleProfileJSON},
	{ruleset: OrderRuleset, profile: OrderProfile, rules: orderRules, profileRules: []string{OrderRequiredField, OrderNotObserved, DuplicateOutput, StatusProgression}, outputRules: []string{DuplicateOutput, StatusProgression}, requiredRule: OrderRequiredField, profileKind: "order and result", statesWindow: true, document: orderProfileJSON},
}

// rulesetFor reports the selected contract. An unknown ruleset falls back to the
// first registered document only so the shared HL7 version and message-type
// checks stay identical; Run never treats that definition as selected, because
// an unnamed ruleset leaves its profile unsupported and every rule cleared.
func rulesetFor(name string) (rulesetDefinition, bool) {
	for _, set := range rulesets {
		if set.ruleset == name {
			return set, true
		}
	}
	return rulesets[0], false
}

// registeredRules lists every rule identifier any registered ruleset defines,
// in registration order and without repetition.
func registeredRules() []string {
	var all []string
	for _, set := range rulesets {
		for _, rule := range set.rules {
			if !slices.Contains(all, rule) {
				all = append(all, rule)
			}
		}
	}
	return all
}

func registeredProfile(name string) bool {
	for _, set := range rulesets {
		if set.profile == name {
			return true
		}
	}
	return false
}

// siuBookings states what the unchanged readmit-siu-v1 document always meant.
// That document predates message kinds, per-trigger authorities and declared
// correlations, so the reader supplies them rather than the contract gaining a
// member: every trigger is SIU, S12 establishes a filler identity, and the
// others depend on one.
func siuBookings(p *profileDefinition) {
	for i, trigger := range p.Triggers {
		booking := correlation{Rule: BookingNotObserved, Role: subsequent, Value: "SCH-2.1", Authority: []string{"SCH-2.2", "SCH-2.3", "SCH-2.4"}, Evidence: []string{"MSH-9.2", "SCH-2.1", "SCH-2.2", "SCH-2.3", "SCH-2.4"}}
		if trigger.Code == "S12" {
			booking.Role, booking.Evidence = antecedent, nil
		}
		p.Triggers[i].Kind = "SIU"
		p.Triggers[i].ScalarFields = []string{"SCH-1", "SCH-2"}
		p.Triggers[i].Correlations = []correlation{booking}
	}
}

// load refuses an embedded document that names another contract, declares a
// correlation for a rule this ruleset does not define or cannot explain,
// declares a status this package cannot explain or an output no rule of this
// ruleset reads, or addresses a field no selector can reach.
func (r rulesetDefinition) load() (profileDefinition, error) {
	var p profileDefinition
	invalid := errors.New("invalid embedded diagnosis profile")
	if err := json.Unmarshal(r.document, &p, json.RejectUnknownMembers(true)); err != nil || p.Profile != r.profile || p.Ruleset != r.ruleset {
		return p, invalid
	}
	if r.normalize != nil {
		r.normalize(&p)
	}
	// Statuses and outputs are read by named rules. A ruleset that defines none
	// of them accepts neither member, so a document of one contract cannot
	// quietly grow the vocabulary of another.
	if len(r.outputRules) == 0 && len(p.Statuses) > 0 {
		return p, invalid
	}
	for _, status := range p.Statuses {
		if _, explained := statusLabels[statusCode{status.Vocabulary, status.Code}]; !explained {
			return p, invalid
		}
	}
	for i, trigger := range p.Triggers {
		if len(trigger.AuthorityFields) == 0 {
			p.Triggers[i].AuthorityFields = p.AuthorityFields
		}
		paths := slices.Clone(trigger.RequiredFields)
		paths = append(paths, trigger.ScalarFields...)
		if trigger.EventType != "" {
			paths = append(paths, trigger.EventType)
		}
		for _, group := range p.Triggers[i].AuthorityFields {
			if len(group) != 3 {
				return p, invalid
			}
			paths = append(paths, group...)
		}
		out := trigger.Output
		switch {
		case out.declared():
			if len(r.outputRules) == 0 || len(out.Authority) != 3 || out.Status == "" || !p.declaresVocabulary(out.Vocabulary) {
				return p, invalid
			}
			paths = append(paths, out.Identity, out.Status)
			paths = append(paths, out.Authority...)
			paths = append(paths, out.Evidence...)
		case out.Status != "" || out.Vocabulary != "" || len(out.Authority) > 0 || len(out.Evidence) > 0:
			return p, invalid
		}
		for _, c := range p.Triggers[i].Correlations {
			if !slices.Contains(r.rules, c.Rule) || len(c.Authority) != 3 || (c.Role != antecedent && c.Role != subsequent) {
				return p, invalid
			}
			if _, explained := hypotheses[c.Rule]; !explained {
				return p, invalid
			}
			paths = append(paths, c.Value)
			paths = append(paths, c.Authority...)
			paths = append(paths, c.Evidence...)
		}
		for _, path := range paths {
			if _, err := hl7.ParseSelector(path); err != nil {
				return p, invalid
			}
		}
	}
	return p, nil
}

// declaresVocabulary reports whether this document declares any status of that
// vocabulary, so an output cannot name a table the profile never defined.
func (p profileDefinition) declaresVocabulary(name string) bool {
	for _, status := range p.Statuses {
		if status.Vocabulary == name {
			return true
		}
	}
	return false
}

// status resolves one decoded code inside one declared vocabulary.
func (p profileDefinition) status(vocabulary, code string) (statusDefinition, bool) {
	for _, declared := range p.Statuses {
		if declared.Vocabulary == vocabulary && declared.Code == code {
			return declared, true
		}
	}
	return statusDefinition{}, false
}

func (p profileDefinition) trigger(kind, code string) (triggerDefinition, bool) {
	for _, trigger := range p.Triggers {
		if trigger.Kind == kind && trigger.Code == code {
			return trigger, true
		}
	}
	return triggerDefinition{}, false
}

// messageTypes names every supported kind and trigger in declaration order, so
// the unsupported explanation always matches the embedded definition.
func (p profileDefinition) messageTypes() string {
	var kinds []string
	codes := make(map[string][]string)
	for _, trigger := range p.Triggers {
		if _, seen := codes[trigger.Kind]; !seen {
			kinds = append(kinds, trigger.Kind)
		}
		codes[trigger.Kind] = append(codes[trigger.Kind], trigger.Code)
	}
	groups := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		groups = append(groups, kind+" "+strings.Join(codes[kind], ", "))
	}
	return strings.Join(groups, ", ")
}

// segments lists the interpreted segments of one trigger in declaration order.
// MSH cannot repeat inside a parsed message and is never counted.
func (t triggerDefinition) segments() []string {
	var ids []string
	add := func(path string) {
		selector, _ := hl7.ParseSelector(path) // every profile path was parsed when the profile was read
		id := selector.Parts().Segment
		if id != "MSH" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, path := range t.RequiredFields {
		add(path)
	}
	for _, group := range t.AuthorityFields {
		add(group[0])
	}
	return ids
}
