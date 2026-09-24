package diagnose

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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
	if len(c.Namespaces) > 128 {
		return errors.New("diagnosis configuration exceeds 128 authority mappings")
	}
	seen := make(map[authority]bool)
	for _, ns := range c.Namespaces {
		if !configToken.MatchString(ns.Key) || !safeConfigValue(ns.Namespace) || !safeConfigValue(ns.UniversalID) || !safeConfigValue(ns.UniversalIDType) {
			return errors.New("invalid diagnosis authority mapping")
		}
		if ns.Namespace == "" && ns.UniversalID == "" || (ns.UniversalID == "") != (ns.UniversalIDType == "") {
			return errors.New("authority mapping requires a namespace or universal identifier and type")
		}
		a := authority{ns.Namespace, ns.UniversalID, ns.UniversalIDType}
		if seen[a] {
			return errors.New("duplicate diagnosis authority mapping")
		}
		seen[a] = true
	}
	return nil
}

func safeConfigValue(s string) bool {
	return len(s) <= 256 && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

type authority struct{ namespace, universalID, universalType string }
