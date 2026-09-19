package correlate_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/correlate"
)

const completeRules = `{
  "schema": "readmit-correlation-rules/v1",
  "authorities": [
    {"key": "READMIT-MR", "namespace": "READMIT", "universal_id": "", "universal_id_type": ""},
    {"key": "READMIT-MR", "namespace": "READMIT-OLD", "universal_id": "1.2.3", "universal_id_type": "ISO"}
  ],
  "rules": [
    {"id": "acknowledgements", "operator": "acknowledges", "scope": "source"},
    {"id": "one-session", "operator": "control-id", "scope": "session"},
    {"id": "across", "operator": "control-id", "scope": "declared", "sources": ["s0001", "s0002"]},
    {"id": "patient", "operator": "identifier", "scope": "declared", "sources": ["s0001"],
     "value": "PID-3.1", "authority": ["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"]}
  ]
}`

func TestRulesReaderAcceptsOneCompleteDocument(t *testing.T) {
	rules, err := correlate.ParseRules([]byte(completeRules))
	if err != nil {
		t.Fatalf("complete rules refused: %v", err)
	}
	if rules.Schema != correlate.RulesSchema || len(rules.Rules) != 4 || len(rules.Authorities) != 2 {
		t.Fatalf("decoded %+v", rules)
	}
	// Two authorities sharing one key is an explicit assertion of equivalence,
	// so the reader keeps both mappings rather than collapsing them.
	if rules.Authorities[0].Key != rules.Authorities[1].Key || rules.Authorities[1].UniversalIDType != "ISO" {
		t.Fatalf("authority aliasing lost: %+v", rules.Authorities)
	}
	patient := rules.Rules[3]
	if patient.Operator != correlate.Identifier || patient.Scope != correlate.DeclaredScope ||
		patient.Value != "PID-3.1" || len(patient.Authority) != 3 || len(patient.Sources) != 1 {
		t.Fatalf("identifier rule decoded as %+v", patient)
	}
}

// Every refusal below is a declaration readmit could not stand behind. None of
// them may be repaired, defaulted, or silently ignored.
func TestRulesReaderRefusesDeclarationsItCannotStandBehind(t *testing.T) {
	for name, document := range map[string]string{
		"no schema":                `{"rules":[{"id":"a","operator":"control-id","scope":"source"}]}`,
		"another contract version": strings.Replace(completeRules, "readmit-correlation-rules/v1", "readmit-correlation-rules/v2", 1),
		"another contract name":    strings.Replace(completeRules, "readmit-correlation-rules/v1", "readmit-correlation/v1", 1),
		"no rules member":          `{"schema":"readmit-correlation-rules/v1"}`,
		"empty rules":              `{"schema":"readmit-correlation-rules/v1","rules":[]}`,
		"unknown document member":  strings.Replace(completeRules, `"rules": [`, `"transform": "upper", "rules": [`, 1),
		"unknown rule member":      strings.Replace(completeRules, `"id": "acknowledgements",`, `"id": "acknowledgements", "script": "sh -c true",`, 1),
		"unknown authority member": strings.Replace(completeRules, `"key": "READMIT-MR",`, `"key": "READMIT-MR", "alias": true,`, 1),
		"rule without id":          `{"schema":"readmit-correlation-rules/v1","rules":[{"operator":"control-id","scope":"source"}]}`,
		"rule without operator":    `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"a","scope":"source"}]}`,
		"rule without scope":       `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"a","operator":"control-id"}]}`,
		"authority without key":    strings.Replace(completeRules, `"key": "READMIT-MR", "namespace": "READMIT",`, `"namespace": "READMIT",`, 1),
		"unknown operator":         strings.Replace(completeRules, `"operator": "acknowledges"`, `"operator": "patient-name"`, 1),
		"unknown scope":            strings.Replace(completeRules, `"scope": "source"`, `"scope": "everything"`, 1),
		"sources outside a declared scope": strings.Replace(completeRules,
			`{"id": "acknowledgements", "operator": "acknowledges", "scope": "source"}`,
			`{"id": "acknowledgements", "operator": "acknowledges", "scope": "source", "sources": ["s0001"]}`, 1),
		"declared scope without sources":                strings.Replace(completeRules, `"scope": "declared", "sources": ["s0001", "s0002"]`, `"scope": "declared"`, 1),
		"declared scope with a source no case can hold": strings.Replace(completeRules, `"sources": ["s0001", "s0002"]`, `"sources": ["s0001", "source-two"]`, 1),
		"declared scope repeating a source":             strings.Replace(completeRules, `"sources": ["s0001", "s0002"]`, `"sources": ["s0001", "s0001"]`, 1),
		"value on an acknowledges rule": strings.Replace(completeRules,
			`{"id": "acknowledgements", "operator": "acknowledges", "scope": "source"}`,
			`{"id": "acknowledgements", "operator": "acknowledges", "scope": "source", "value": "PID-3.1"}`, 1),
		"authority on a control-id rule": strings.Replace(completeRules,
			`{"id": "one-session", "operator": "control-id", "scope": "session"}`,
			`{"id": "one-session", "operator": "control-id", "scope": "session", "authority": ["PID-3.4.1","PID-3.4.2","PID-3.4.3"]}`, 1),
		"identifier without a value":                strings.Replace(completeRules, `"value": "PID-3.1", `, "", 1),
		"identifier value outside the grammar":      strings.Replace(completeRules, `"value": "PID-3.1"`, `"value": "PID-*"`, 1),
		"identifier with two authority parts":       strings.Replace(completeRules, `["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"]`, `["PID-3.4.1", "PID-3.4.2"]`, 1),
		"identifier authority outside the grammar":  strings.Replace(completeRules, `"PID-3.4.3"`, `"pid-3"`, 1),
		"repeated rule identifier":                  strings.Replace(completeRules, `"id": "one-session"`, `"id": "acknowledgements"`, 1),
		"rule identifier outside the token":         strings.Replace(completeRules, `"id": "across"`, `"id": "a cross"`, 1),
		"repeated authority mapping":                strings.Replace(completeRules, `"namespace": "READMIT-OLD", "universal_id": "1.2.3", "universal_id_type": "ISO"`, `"namespace": "READMIT", "universal_id": "", "universal_id_type": ""`, 1),
		"authority with no identity at all":         strings.Replace(completeRules, `"namespace": "READMIT", "universal_id": "", "universal_id_type": ""`, `"namespace": "", "universal_id": "", "universal_id_type": ""`, 1),
		"authority with a universal id and no type": strings.Replace(completeRules, `"universal_id": "1.2.3", "universal_id_type": "ISO"`, `"universal_id": "1.2.3", "universal_id_type": ""`, 1),
		"authority carrying a control character":    strings.Replace(completeRules, `"namespace": "READMIT-OLD"`, `"namespace": "READ\tMIT"`, 1),
		"not JSON at all":                           "readmit-correlation-rules/v1",
	} {
		t.Run(name, func(t *testing.T) {
			rules, err := correlate.ParseRules([]byte(document))
			if err == nil {
				t.Fatalf("accepted %+v", rules)
			}
			for _, leaked := range []string{"sh -c true", "patient-name", "everything", "PID-*", "READMIT-OLD", "source-two"} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("diagnostic repeated the refused declaration: %v", err)
				}
			}
		})
	}
}

func TestRulesReaderRefusesDocumentsBeyondItsBounds(t *testing.T) {
	var oversize strings.Builder
	oversize.WriteString(`{"schema":"readmit-correlation-rules/v1","rules":[`)
	for oversize.Len() <= correlate.MaxRulesBytes {
		oversize.WriteString(`{"id":"a","operator":"control-id","scope":"source"},`)
	}
	oversize.WriteString(`{"id":"b","operator":"control-id","scope":"source"}]}`)
	if _, err := correlate.ParseRules([]byte(oversize.String())); err == nil {
		t.Fatal("accepted a document beyond the 1 MiB bound")
	}

	var many strings.Builder
	many.WriteString(`{"schema":"readmit-correlation-rules/v1","rules":[`)
	for i := range correlate.MaxRules + 1 {
		if i > 0 {
			many.WriteByte(',')
		}
		many.WriteString(`{"id":"r`)
		many.WriteByte(byte('a' + i%26))
		many.WriteByte(byte('a' + i/26))
		many.WriteString(`","operator":"control-id","scope":"source"}`)
	}
	many.WriteString(`]}`)
	if _, err := correlate.ParseRules([]byte(many.String())); err == nil {
		t.Fatal("accepted more than 64 rules")
	}
}
