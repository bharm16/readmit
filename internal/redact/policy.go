package redact

import (
	"encoding/json/v2"
	"fmt"
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

// DecodePolicy reads one disclosure policy exactly as `readmit redact` and the
// desktop policy panel do. Its acceptance set is the closed one this package
// derives with; every refusal names the declaration that failed, so an
// unsupported policy says which of its rules to fix.
func DecodePolicy(data []byte) (Policy, error) {
	var policy Policy
	refuse := func(rule string, args ...any) (Policy, error) {
		return Policy{}, fmt.Errorf("invalid redaction policy: %s", fmt.Sprintf(rule, args...))
	}
	if len(data) > maxConfigBytes {
		return refuse("the document exceeds the policy read limit")
	}
	if json.Unmarshal(data, &policy, json.RejectUnknownMembers(true)) != nil {
		return refuse("the document is not a strict JSON object holding only known members")
	}
	if policy.Schema != PolicySchema {
		return refuse("schema must be %s", PolicySchema)
	}
	if len(policy.Fields) > 2048 {
		return refuse("fields declares more than 2048 field rules")
	}
	if len(policy.SpecBindings) > 4096 {
		return refuse("spec_bindings declares more than 4096 bindings")
	}
	if len(policy.RemoveSegments) > 128 {
		return refuse("remove_segments declares more than 128 segments")
	}
	if len(policy.PacketPolicies) > 5 {
		return refuse("packet_policies declares more than 5 policies")
	}
	if len(policy.RequiredFailures) == 0 {
		return refuse("required_failures declares no assertion position")
	}
	if len(policy.RequiredFailures) > 256 {
		return refuse("required_failures declares more than 256 positions")
	}
	if _, err := hl7.ParseSelector(policy.Patient.Selector); err != nil {
		return refuse("the patient selector is not a supported field selector")
	}
	if !validSelectors(policy.Patient.Authority) {
		return refuse("the patient authority declares an unsupported field selector or more than 4 selectors")
	}
	seen := map[string]bool{}
	for _, rule := range policy.Fields {
		selector, err := hl7.ParseSelector(rule.Selector)
		if err != nil {
			return refuse("a field rule does not name a supported field selector")
		}
		if seen[selector.String()] {
			return refuse("the field rules name %s more than once", selector.String())
		}
		seen[selector.String()] = true
		if rule.Class != "structural" && !slices.Contains(exportreview.Categories, rule.Class) {
			return refuse("the field rule for %s declares a class outside the coverage checklist", selector.String())
		}
		switch rule.Policy {
		case Surrogate:
			if !scopeToken.MatchString(rule.Scope) {
				return refuse("the field rule for %s (%s) declares a scope that is not a lowercase named scope", selector.String(), rule.Policy)
			}
			if !validSelectors(rule.Authority) {
				return refuse("the field rule for %s (%s) declares an unsupported authority selector or more than 4 selectors", selector.String(), rule.Policy)
			}
			if rule.Replacement != nil {
				return refuse("the field rule for %s (%s) must not declare a replacement", selector.String(), rule.Policy)
			}
			if len(rule.Allowed) != 0 {
				return refuse("the field rule for %s (%s) must not declare allowed literals", selector.String(), rule.Policy)
			}
			if rule.Class == "structural" {
				return refuse("the field rule for %s (%s) must not declare the structural class", selector.String(), rule.Policy)
			}
		case DateShift:
			if rule.Class != "dates-and-ages" {
				return refuse("the field rule for %s (%s) must declare the dates-and-ages class", selector.String(), rule.Policy)
			}
			if rule.Scope != "" {
				return refuse("the field rule for %s (%s) must not declare a scope", selector.String(), rule.Policy)
			}
			if len(rule.Authority) != 0 {
				return refuse("the field rule for %s (%s) must not declare authority selectors", selector.String(), rule.Policy)
			}
			if rule.Replacement != nil {
				return refuse("the field rule for %s (%s) must not declare a replacement", selector.String(), rule.Policy)
			}
			if len(rule.Allowed) != 0 {
				return refuse("the field rule for %s (%s) must not declare allowed literals", selector.String(), rule.Policy)
			}
		case Remove, Replace, Retain:
			if rule.Scope != "" {
				return refuse("the field rule for %s (%s) must not declare a scope", selector.String(), rule.Policy)
			}
			if len(rule.Authority) != 0 {
				return refuse("the field rule for %s (%s) must not declare authority selectors", selector.String(), rule.Policy)
			}
			if rule.Policy != Replace && rule.Replacement != nil {
				return refuse("the field rule for %s (%s) declares a replacement, which only %s may carry", selector.String(), rule.Policy, Replace)
			}
			if rule.Policy != Retain && len(rule.Allowed) != 0 {
				return refuse("the field rule for %s (%s) declares allowed literals, which only %s may carry", selector.String(), rule.Policy, Retain)
			}
			if rule.Policy == Replace && (rule.Replacement == nil || !safeReplacement(*rule.Replacement)) {
				return refuse("the field rule for %s (%s) must declare a replacement that is bounded printable UTF-8 text without delimiters", selector.String(), rule.Policy)
			}
			if rule.Policy == Retain && (len(rule.Allowed) == 0 || len(rule.Allowed) > 64) {
				return refuse("the field rule for %s (%s) must declare between 1 and 64 allowed literals", selector.String(), rule.Policy)
			}
			if rule.Policy == Retain && rule.Class != "structural" {
				return refuse("the field rule for %s (%s) must declare the structural class", selector.String(), rule.Policy)
			}
			for _, value := range rule.Allowed {
				if len(value) > 4096 || !utf8.ValidString(value) {
					return refuse("the field rule for %s (%s) declares an allowed literal that is longer than 4096 bytes or not valid UTF-8", selector.String(), rule.Policy)
				}
			}
		default:
			return refuse("the field rule for %s declares an unsupported policy", selector.String())
		}
	}
	seen = map[string]bool{}
	for position, segment := range policy.RemoveSegments {
		if !segmentToken.MatchString(segment) {
			return refuse("remove_segments entry %d is not a segment identifier", position+1)
		}
		if segment == "MSH" {
			return refuse("remove_segments declares %s, which every message requires", segment)
		}
		if seen[segment] {
			return refuse("remove_segments declares %s more than once", segment)
		}
		seen[segment] = true
	}
	seen = map[string]bool{}
	for position, name := range policy.PacketPolicies {
		if !slices.Contains([]string{Filenames, Metadata, SpecLiterals, Diagnosis, Rerun}, name) {
			return refuse("packet_policies entry %d is not a supported packet policy", position+1)
		}
		if seen[name] {
			return refuse("packet_policies declares %s more than once", name)
		}
		seen[name] = true
	}
	seen = map[string]bool{}
	for position, binding := range policy.SpecBindings {
		if len(binding.Location) == 0 || len(binding.Location) > 128 {
			return refuse("a spec binding must name a location of 1 to 128 characters")
		}
		if seen[binding.Location] {
			return refuse("spec binding %d repeats a location", position+1)
		}
		seen[binding.Location] = true
		if binding.Constant != nil {
			// Fixed protocol codes are the only constants accepted for expected
			// text. Identifiers and dates must bind to transformed source fields.
			if binding.Occurrence != "" || binding.Selector != "" {
				return refuse("spec binding %d pins a constant, so it must not declare an occurrence or selector", position+1)
			}
			if !slices.Contains([]string{"AA", "AE", "AR", "CA", "CE", "CR"}, *binding.Constant) {
				return refuse("spec binding %d pins a constant outside the acknowledged protocol codes", position+1)
			}
		} else {
			if _, err := hl7.ParseSelector(binding.Selector); err != nil {
				return refuse("spec binding %d does not name a supported source selector", position+1)
			}
			if !occurrencePattern.MatchString(binding.Occurrence) {
				return refuse("spec binding %d does not name an occurrence like s0001-e000001", position+1)
			}
		}
	}
	seenFailure := map[int]bool{}
	for _, index := range policy.RequiredFailures {
		if index < 1 {
			return refuse("required_failures declares %d, which is below the first assertion position", index)
		}
		if index > 256 {
			return refuse("required_failures declares %d, which is beyond the last supported assertion position", index)
		}
		if seenFailure[index] {
			return refuse("required_failures declares %d more than once", index)
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
