package diagnose

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"

	"github.com/bharm16/readmit/internal/authority"
)

var configToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+/-]{0,127}$`)

func DefaultConfig() Config {
	return Config{Schema: ConfigSchema, Profile: Profile, Ruleset: Ruleset, Rules: slices.Clone(supportedRules), Namespaces: []Namespace{{Key: "READMIT", Namespace: "READMIT"}}}
}

// LifecycleConfig selects the separate ADT and appointment lifecycle contract.
// It is the same readmit-diagnose-config/v1 document with different selections;
// nothing chooses it implicitly, and DefaultConfig keeps naming readmit-siu-v1.
func LifecycleConfig() Config {
	return Config{Schema: ConfigSchema, Profile: LifecycleProfile, Ruleset: LifecycleRuleset, Rules: slices.Clone(lifecycleRules), Namespaces: []Namespace{{Key: "READMIT", Namespace: "READMIT"}}}
}

// OrderConfig selects the separate order, result and acknowledgement contract.
// It is again the same readmit-diagnose-config/v1 document with different
// selections; nothing chooses it implicitly.
func OrderConfig() Config {
	return Config{Schema: ConfigSchema, Profile: OrderProfile, Ruleset: OrderRuleset, Rules: slices.Clone(orderRules), Namespaces: []Namespace{{Key: "READMIT", Namespace: "READMIT"}}}
}

// MaxConfigBytes bounds a readmit-diagnose-config/v1 document. One past it is
// refused, never truncated.
const MaxConfigBytes = 1 << 20

// ParseConfig rejects unknown members, duplicate keys, missing contract versions,
// and conflicting authority mappings. An unknown profile/rule is valid data and
// is reported explicitly as unsupported by Run.
func ParseConfig(data []byte) (Config, error) {
	var config Config
	if len(data) > MaxConfigBytes {
		return Config{}, errors.New("diagnosis configuration exceeds 1 MiB")
	}
	if err := json.Unmarshal(data, &config, json.RejectUnknownMembers(true)); err != nil {
		return Config{}, errors.New("invalid diagnosis configuration")
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) validate() error {
	if c.Schema != ConfigSchema {
		return errors.New("unsupported diagnosis configuration schema")
	}
	if !configToken.MatchString(c.Profile) || !configToken.MatchString(c.Ruleset) {
		return errors.New("diagnosis configuration requires profile and ruleset tokens")
	}
	if len(c.Rules) == 0 || len(c.Rules) > 64 {
		return errors.New("diagnosis configuration requires between 1 and 64 rule identifiers")
	}
	seenRules := make(map[string]bool)
	for _, rule := range c.Rules {
		if !configToken.MatchString(rule) || seenRules[rule] {
			return errors.New("diagnosis rules must be distinct valid tokens")
		}
		seenRules[rule] = true
	}
	// The authority mappings are the same assertions a correlation rules
	// document declares; what one is and when it is refused is decided once,
	// in internal/authority, and only the wording is this contract's own.
	switch defect := authority.Validate(mappingsOf(c.Namespaces)); defect {
	case authority.OverBound:
		return errors.New("diagnosis configuration exceeds 128 authority mappings")
	case authority.Invalid:
		return errors.New("invalid diagnosis authority mapping")
	case authority.Incomplete:
		return errors.New("authority mapping requires a namespace or universal identifier and type")
	case authority.Duplicate:
		return errors.New("duplicate diagnosis authority mapping")
	}
	return nil
}

// mappingsOf states the configuration's own mapping type as the set
// internal/authority validates and resolves.
func mappingsOf(namespaces []Namespace) []authority.Mapping {
	mappings := make([]authority.Mapping, len(namespaces))
	for i, ns := range namespaces {
		mappings[i] = authority.Mapping(ns)
	}
	return mappings
}
