package diagnose

import (
	"bytes"
	"testing"
)

func orderRuleset(t *testing.T) rulesetDefinition {
	t.Helper()
	for _, set := range rulesets {
		if set.ruleset == OrderRuleset {
			return set
		}
	}
	t.Fatal("the order ruleset is not registered")
	return rulesetDefinition{}
}

func TestEveryRegisteredRulesetReadsItsOwnEmbeddedDocument(t *testing.T) {
	for _, set := range rulesets {
		if _, err := set.load(); err != nil {
			t.Fatalf("%s does not read its own document: %v", set.ruleset, err)
		}
	}
}

// A profile document is data this package interprets, so a document it cannot
// stand behind is refused at the seam rather than partly evaluated. These are
// the refusals that keep a typo from silently disabling a rule.
func TestTheOrderDocumentIsRefusedWhereItCannotBeInterpreted(t *testing.T) {
	set := orderRuleset(t)
	for _, tc := range []struct{ name, from, to string }{
		{"unknown member", `"hl7_version"`, `"hl7_versions"`},
		{"another contract's ruleset", OrderRuleset, LifecycleRuleset},
		{"another contract's profile", OrderProfile, LifecycleProfile},
		{"a status no explanation covers", `{"vocabulary": "order-status", "code": "SC"`, `{"vocabulary": "order-status", "code": "ZZ"`},
		{"an output naming an undeclared vocabulary", `"vocabulary": "order-status",` + "\n", `"vocabulary": "no-such-vocabulary",` + "\n"},
		{"an output without a status", `"status": "ORC-5",`, `"status": "",`},
		{"an output without a complete authority", `"authority": ["ORC-3.2", "ORC-3.3", "ORC-3.4"],`, `"authority": ["ORC-3.2"],`},
		{"an output no selector can reach", `"identity": "ORC-3.1"`, `"identity": "not a selector"`},
		{"an output declared by its members alone", `"identity": "ORC-3.1",`, `"identity": "",`},
		{"a correlation for a rule this ruleset does not define", `"rule": "order.order-not-observed", "role": "antecedent"`, `"rule": "lifecycle.visit-not-observed", "role": "antecedent"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doctored := set
			doctored.document = bytes.Replace(set.document, []byte(tc.from), []byte(tc.to), 1)
			if bytes.Equal(doctored.document, set.document) {
				t.Fatalf("the document does not contain %q", tc.from)
			}
			if _, err := doctored.load(); err == nil {
				t.Fatal("an uninterpretable profile document was accepted")
			}
		})
	}
}

// A status vocabulary and an output declaration are read by named rules. A
// ruleset that defines none of them must not silently ignore either, because
// nothing would then report that the declaration is never evaluated — and
// because the contract of another ruleset would have gained a member it does
// not mean.
func TestStatusesAndOutputsAreRefusedByARulesetThatReadsNone(t *testing.T) {
	doctored := orderRuleset(t)
	doctored.outputRules = nil
	if _, err := doctored.load(); err == nil {
		t.Fatal("an output no rule reads was accepted")
	}
	for _, set := range rulesets {
		if len(set.outputRules) > 0 {
			continue
		}
		doctored := set
		doctored.document = bytes.Replace(set.document, []byte(`"hl7_version"`), []byte(`"statuses": [{"vocabulary": "order-status", "code": "SC", "final": false}], "hl7_version"`), 1)
		if bytes.Equal(doctored.document, set.document) {
			t.Fatalf("%s declares no hl7_version", set.ruleset)
		}
		if _, err := doctored.load(); err == nil {
			t.Fatalf("%s accepted a status vocabulary no rule of it reads", set.ruleset)
		}
	}
}
