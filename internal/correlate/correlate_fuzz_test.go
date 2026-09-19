package correlate_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/hl7"
)

// FuzzCorrelationRulesDocument exercises the rules reader: the strict decode
// that refuses unknown members, the nested presence-then-strict decoders, and
// every bound an accepted document is held to.
//
// No input may panic, and no bytes at all may produce a rules value that names
// an operator or a scope outside the closed sets, carries a member another
// operator owns, repeats a rule identifier or an authority mapping, or holds a
// selector the shared grammar does not accept. That is the ticket's promise
// stated as a property: a correlation rule cannot be talked into becoming a
// script, and a scope cannot be talked into disappearing, so nothing readmit
// links can escape a boundary an operator declared.
func FuzzCorrelationRulesDocument(f *testing.F) {
	for _, seed := range []string{
		completeRules,
		strings.Replace(completeRules, `"operator": "acknowledges"`, `"operator": "identifier"`, 1),
		strings.Replace(completeRules, `"scope": "declared", "sources": ["s0001", "s0002"]`, `"scope": "source"`, 1),
		strings.Replace(completeRules, `"value": "PID-3.1"`, `"value": "PID[2]-3[2].4.2"`, 1),
		strings.Replace(completeRules, `"rules": [`, `"rules": [{"id":"x","operator":"control-id","scope":"session"},`, 1),
		strings.Replace(completeRules, "readmit-correlation-rules/v1", "readmit-correlation-rules/v2", 1),
		`{"schema":"readmit-correlation-rules/v1","rules":[{"id":"a","operator":"control-id","scope":"source"}]}`,
		`{"schema":"readmit-correlation-rules/v1","rules":[]}`,
		`{"schema":"readmit-correlation-rules/v1"}`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	operators := []correlate.Operator{correlate.Acknowledges, correlate.ControlID, correlate.Identifier}
	scopes := []correlate.Scope{correlate.SourceScope, correlate.SessionScope, correlate.DeclaredScope}
	f.Fuzz(func(t *testing.T, data []byte) {
		rules, err := correlate.ParseRules(data)
		if err != nil {
			return
		}
		if rules.Schema != correlate.RulesSchema {
			t.Fatalf("accepted the contract %q", rules.Schema)
		}
		if len(rules.Rules) == 0 || len(rules.Rules) > correlate.MaxRules {
			t.Fatalf("accepted %d rules", len(rules.Rules))
		}
		if len(rules.Authorities) > correlate.MaxAuthorities {
			t.Fatalf("accepted %d authority mappings", len(rules.Authorities))
		}
		mappings := map[correlate.Authority]bool{}
		for _, mapping := range rules.Authorities {
			identity := correlate.Authority{Namespace: mapping.Namespace, UniversalID: mapping.UniversalID, UniversalIDType: mapping.UniversalIDType}
			if mappings[identity] {
				t.Fatalf("accepted one assigning authority twice: %+v", mapping)
			}
			mappings[identity] = true
			if mapping.Key == "" {
				t.Fatalf("accepted an authority mapping with no key: %+v", mapping)
			}
			if mapping.Namespace == "" && mapping.UniversalID == "" {
				t.Fatalf("accepted an authority that identifies nothing: %+v", mapping)
			}
			if (mapping.UniversalID == "") != (mapping.UniversalIDType == "") {
				t.Fatalf("accepted a half-declared universal identifier: %+v", mapping)
			}
		}
		identifiers := map[string]bool{}
		for _, rule := range rules.Rules {
			if rule.ID == "" || identifiers[rule.ID] {
				t.Fatalf("accepted the rule identifier %q twice or empty", rule.ID)
			}
			identifiers[rule.ID] = true
			if !slices.Contains(operators, rule.Operator) {
				t.Fatalf("accepted the operator %q", rule.Operator)
			}
			if !slices.Contains(scopes, rule.Scope) {
				t.Fatalf("accepted the scope %q", rule.Scope)
			}
			// A scope is the only boundary correlation has. A declared scope
			// always names sources; the others never do.
			if (rule.Scope == correlate.DeclaredScope) != (len(rule.Sources) > 0) {
				t.Fatalf("accepted the scope %q with %d sources", rule.Scope, len(rule.Sources))
			}
			if len(rule.Sources) > correlate.MaxRuleSources {
				t.Fatalf("accepted %d declared sources", len(rule.Sources))
			}
			listed := map[string]bool{}
			for _, source := range rule.Sources {
				if listed[source] {
					t.Fatalf("accepted the source %q twice", source)
				}
				listed[source] = true
			}
			// Value and authority belong to the identifier operator alone, and
			// every selector went through the shared grammar.
			if (rule.Operator == correlate.Identifier) != (rule.Value != "") {
				t.Fatalf("accepted the operator %q with value %q", rule.Operator, rule.Value)
			}
			if (rule.Operator == correlate.Identifier) != (len(rule.Authority) == 3) {
				t.Fatalf("accepted the operator %q with %d authority selectors", rule.Operator, len(rule.Authority))
			}
			for _, selector := range append([]string{rule.Value}, rule.Authority...) {
				if selector == "" {
					continue
				}
				if _, err := hl7.ParseSelector(selector); err != nil {
					t.Fatalf("accepted the selector %q", selector)
				}
			}
		}
	})
}
