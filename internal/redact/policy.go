package redact

import (
	"encoding/json/v2"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
)

var scopeToken = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var segmentToken = regexp.MustCompile(`^[A-Z][A-Z0-9]{2}$`)

func DecodePolicy(data []byte) (Policy, error) {
	var policy Policy
	invalid := errors.New("invalid redaction policy; only explicit named policies are supported")
	if len(data) > maxConfigBytes || json.Unmarshal(data, &policy, json.RejectUnknownMembers(true)) != nil || policy.Schema != PolicySchema || len(policy.Fields) > 2048 || len(policy.SpecBindings) > 4096 || len(policy.RemoveSegments) > 128 || len(policy.PacketPolicies) > 5 || len(policy.RequiredFailures) == 0 || len(policy.RequiredFailures) > 256 {
		return Policy{}, invalid
	}
	if _, err := hl7.ParseSelector(policy.Patient.Selector); err != nil || !validSelectors(policy.Patient.Authority) {
		return Policy{}, invalid
	}
	seen := map[string]bool{}
	for _, rule := range policy.Fields {
		selector, err := hl7.ParseSelector(rule.Selector)
		if err != nil || seen[selector.String()] || rule.Class != "structural" && !slices.Contains(exportreview.Categories, rule.Class) {
			return Policy{}, invalid
		}
		seen[selector.String()] = true
		switch rule.Policy {
		case Surrogate:
			if !scopeToken.MatchString(rule.Scope) || !validSelectors(rule.Authority) || rule.Replacement != nil || len(rule.Allowed) != 0 || rule.Class == "structural" {
				return Policy{}, invalid
			}
		case DateShift:
			if rule.Class != "dates-and-ages" || rule.Scope != "" || len(rule.Authority) != 0 || rule.Replacement != nil || len(rule.Allowed) != 0 {
				return Policy{}, invalid
			}
		case Remove, Replace, Retain:
			if rule.Scope != "" || len(rule.Authority) != 0 || rule.Policy != Replace && rule.Replacement != nil || rule.Policy != Retain && len(rule.Allowed) != 0 {
				return Policy{}, invalid
			}
			if rule.Policy == Replace && (rule.Replacement == nil || !safeReplacement(*rule.Replacement)) || rule.Policy == Retain && (len(rule.Allowed) == 0 || len(rule.Allowed) > 64 || rule.Class != "structural") {
				return Policy{}, invalid
			}
			for _, value := range rule.Allowed {
				if len(value) > 4096 || !utf8.ValidString(value) {
					return Policy{}, invalid
				}
			}
		default:
			return Policy{}, invalid
		}
	}
	seen = map[string]bool{}
	for _, segment := range policy.RemoveSegments {
		if !segmentToken.MatchString(segment) || segment == "MSH" || seen[segment] {
			return Policy{}, invalid
		}
		seen[segment] = true
	}
	seen = map[string]bool{}
	for _, name := range policy.PacketPolicies {
		if !slices.Contains([]string{Filenames, Metadata, SpecLiterals, Diagnosis, Rerun}, name) || seen[name] {
			return Policy{}, invalid
		}
		seen[name] = true
	}
	seen = map[string]bool{}
	for _, binding := range policy.SpecBindings {
		if len(binding.Location) == 0 || len(binding.Location) > 128 || seen[binding.Location] {
			return Policy{}, invalid
		}
		seen[binding.Location] = true
		if binding.Constant != nil {
			// Fixed protocol codes are the only constants accepted for expected
			// text. Identifiers and dates must bind to transformed source fields.
			if binding.Occurrence != "" || binding.Selector != "" || !slices.Contains([]string{"AA", "AE", "AR", "CA", "CE", "CR"}, *binding.Constant) {
				return Policy{}, invalid
			}
		} else if _, err := hl7.ParseSelector(binding.Selector); err != nil || !occurrencePattern.MatchString(binding.Occurrence) {
			return Policy{}, invalid
		}
	}
	seenFailure := map[int]bool{}
	for _, index := range policy.RequiredFailures {
		if index < 1 || index > 256 || seenFailure[index] {
			return Policy{}, invalid
		}
		seenFailure[index] = true
	}
	slices.Sort(policy.RequiredFailures)
	return policy, nil
}

func validSelectors(values []string) bool {
	if len(values) > 4 {
		return false
	}
	for _, value := range values {
		if _, err := hl7.ParseSelector(value); err != nil {
			return false
		}
	}
	return true
}

func safeReplacement(value string) bool {
	return len(value) <= 1024 && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0 && !strings.ContainsAny(value, "|^~\\&\"")
}
