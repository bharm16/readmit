package correlate

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/hl7"
)

const (
	// MaxRulesBytes bounds the document a reader decodes.
	MaxRulesBytes = 1 << 20
	// MaxRules bounds the declared rules of one document.
	MaxRules = 64
	// MaxAuthorities bounds the declared authority mappings, matching the
	// diagnosis configuration an operator already maintains.
	MaxAuthorities = 128
	// MaxRuleSources bounds a declared scope by the case reader's own source
	// limit: a rule cannot name more sources than a case can hold.
	MaxRuleSources = 128
	// authorityParts is the namespace, universal ID and universal ID type of
	// one HL7 assigning authority, in that order.
	authorityParts = 3
)

var (
	ruleToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+/-]{0,127}$`)
	sourceID  = regexp.MustCompile(`^s[0-9]{4,}$`)
)

// UnmarshalJSON reads one rule exactly as written: presence first, so an
// omitted declaration cannot read as the empty string, then the same bytes
// again rejecting unknown members.
func (r *Rule) UnmarshalJSON(data []byte) error {
	var required struct {
		ID       *string   `json:"id"`
		Operator *Operator `json:"operator"`
		Scope    *Scope    `json:"scope"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.ID == nil || required.Operator == nil || required.Scope == nil {
		return errors.New("a correlation rule requires id, operator and scope")
	}
	type rule Rule
	var decoded rule
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("a correlation rule declares no member beyond id, operator, scope, sources, value and authority")
	}
	*r = Rule(decoded)
	return nil
}

// UnmarshalJSON reads one authority mapping exactly as written.
func (a *Authority) UnmarshalJSON(data []byte) error {
	var required struct {
		Key *string `json:"key"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Key == nil {
		return errors.New("an authority mapping requires a key")
	}
	type authorityMapping Authority
	var decoded authorityMapping
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("an authority mapping declares no member beyond key, namespace, universal_id and universal_id_type")
	}
	*a = Authority(decoded)
	return nil
}

// ParseRules reads one correlation rules document exactly as written and
// refuses every document it could not stand behind: unknown members, a
// contract this release does not read, an operator outside the closed set, a
// scope the operator cannot use, a member another operator owns, a selector
// the shared grammar does not accept, and a conflicting authority mapping.
func ParseRules(data []byte) (Rules, error) {
	if len(data) > MaxRulesBytes {
		return Rules{}, errors.New("a correlation rules document exceeds its 1 MiB size limit")
	}
	var required struct {
		Schema *string `json:"schema"`
		Rules  *[]Rule `json:"rules"`
	}
	if err := json.Unmarshal(data, &required); err != nil {
		return Rules{}, errors.New("invalid correlation rules JSON")
	}
	if required.Schema == nil || *required.Schema != RulesSchema {
		return Rules{}, errors.New("correlation rules must declare " + RulesSchema)
	}
	if required.Rules == nil {
		return Rules{}, errors.New("a correlation rules document requires rules")
	}
	var rules Rules
	if err := json.Unmarshal(data, &rules, json.RejectUnknownMembers(true)); err != nil {
		return Rules{}, errors.New("invalid correlation rules JSON")
	}
	if err := rules.validate(); err != nil {
		return Rules{}, err
	}
	return rules, nil
}

func (r Rules) validate() error {
	if r.Schema != RulesSchema {
		return errors.New("correlation rules must declare " + RulesSchema)
	}
	if err := validateAuthorities(r.Authorities); err != nil {
		return err
	}
	if len(r.Rules) == 0 || len(r.Rules) > MaxRules {
		return errors.New("a correlation rules document declares between 1 and 64 rules")
	}
	seen := make(map[string]bool, len(r.Rules))
	for _, rule := range r.Rules {
		if !ruleToken.MatchString(rule.ID) || seen[rule.ID] {
			return errors.New("correlation rule identifiers must be distinct tokens")
		}
		seen[rule.ID] = true
		if err := rule.validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthorities(authorities []Authority) error {
	if len(authorities) > MaxAuthorities {
		return errors.New("correlation rules declare at most 128 authority mappings")
	}
	seen := make(map[authority]bool, len(authorities))
	for _, mapping := range authorities {
		if !ruleToken.MatchString(mapping.Key) || !safeValue(mapping.Namespace) || !safeValue(mapping.UniversalID) || !safeValue(mapping.UniversalIDType) {
			return errors.New("invalid correlation authority mapping")
		}
		if mapping.Namespace == "" && mapping.UniversalID == "" || (mapping.UniversalID == "") != (mapping.UniversalIDType == "") {
			return errors.New("an authority mapping requires a namespace or a universal identifier and type")
		}
		key := authority{mapping.Namespace, mapping.UniversalID, mapping.UniversalIDType}
		if seen[key] {
			return errors.New("duplicate correlation authority mapping")
		}
		seen[key] = true
	}
	return nil
}

func (r Rule) validate() error {
	switch r.Operator {
	case Acknowledges, ControlID:
		if r.Value != "" || len(r.Authority) != 0 {
			return errors.New("only an identifier rule declares value and authority")
		}
	case Identifier:
		if _, err := hl7.ParseSelector(r.Value); err != nil {
			return errors.New("an identifier rule declares one value selector in the shared grammar")
		}
		if len(r.Authority) != authorityParts {
			return errors.New("an identifier rule declares exactly three authority selectors: namespace, universal ID and universal ID type")
		}
		for _, selector := range r.Authority {
			if _, err := hl7.ParseSelector(selector); err != nil {
				return errors.New("an identifier rule declares authority selectors in the shared grammar")
			}
		}
	default:
		return errors.New("a correlation operator is acknowledges, control-id or identifier")
	}
	switch r.Scope {
	case SourceScope, SessionScope:
		if len(r.Sources) != 0 {
			return errors.New("only a declared scope lists sources")
		}
	case DeclaredScope:
		if len(r.Sources) == 0 || len(r.Sources) > MaxRuleSources {
			return errors.New("a declared scope lists between 1 and 128 case source identifiers")
		}
		listed := make(map[string]bool, len(r.Sources))
		for _, source := range r.Sources {
			if !sourceID.MatchString(source) || listed[source] {
				return errors.New("a declared scope lists distinct case source identifiers")
			}
			listed[source] = true
		}
	default:
		return errors.New("a correlation scope is source, session or declared")
	}
	return nil
}

// safeValue bounds what a mapping may say: valid UTF-8 with no control
// character, because a document somebody imported must not be able to drive
// the terminal a diagnostic is displayed on.
func safeValue(value string) bool {
	return len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

type authority struct{ namespace, universalID, universalType string }
