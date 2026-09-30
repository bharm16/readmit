package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
)

// MinDeriverKeyBytes is the shortest customer-local key a [Deriver] accepts.
const MinDeriverKeyBytes = 32

// A Deriver applies one policy's field rules and removed segments to single
// messages, exactly as Create derives a case, but draws each surrogate and
// each patient's date shift from a customer-local key instead of fresh
// randomness: the same source value in the same scope always derives the
// same way under the same key. That makes a derivation reproducible, so a
// sharing preview can be generated again and compared byte for byte, and it
// lets values a report restates be derived consistently with the messages
// they came from. It writes nothing and keeps no private state beyond the
// process; the key is the only linkage and never enters a derived byte.
type Deriver struct {
	t        *transformer
	values   map[string]*DerivedValue
	examples map[string][]Example
}

// Example is one source value of a field and what it derived to.
type Example struct {
	Original string `json:"original"`
	Derived  string `json:"derived"`
}

// maxExamples bounds the examples kept for one field.
const maxExamples = 3

// DerivedValue is what one source value derived to under one rule.
type DerivedValue struct {
	// Derived is the value's derived text; empty when the rule removed it.
	Derived string
	// Rule is the field rule that derived it.
	Rule FieldRule
	// Conflict is set when the same source text derived differently under
	// different rules, so a restated copy of it cannot be derived by lookup.
	Conflict bool
}

// NewDeriver prepares a derivation of single messages under policy. The key
// must be at least [MinDeriverKeyBytes] long.
func NewDeriver(policy Policy, key []byte) (*Deriver, error) {
	if len(key) < MinDeriverKeyBytes {
		return nil, errors.New("a derivation key is at least 32 bytes")
	}
	return &Deriver{
		t: &transformer{
			policy:     policy,
			findingLog: findingLog{Findings: []exportreview.Finding{}, Policies: map[string]bool{}},
			local:      localState{Schema: PrivateSchema, Mappings: []mapping{}, Shifts: []shift{}, ResidualValues: [][]byte{}},
			original:   map[string]*hl7.Document{},
			derived:    map[string]*hl7.Document{},
			keyed:      slices.Clone(key),
		},
		values:   map[string]*DerivedValue{},
		examples: map[string][]Example{},
	}, nil
}

// Message derives one message. id names it in the findings, which locate
// every field the policy handled and every value it left unresolved, as
// Create's review does ("case/<id>/..."). An unparsed message, or one with
// non-standard delimiters, is returned unchanged with its unresolved finding.
func (d *Deriver) Message(id string, raw []byte, kind bundle.EventKind, terminator hl7.Terminator) ([]byte, []exportreview.Finding, error) {
	first := len(d.t.Findings)
	derived, err := d.t.transformOccurrence(bundle.Event{ID: id, Kind: kind, Terminator: terminator}, raw)
	if err != nil {
		return nil, nil, err
	}
	findings := slices.Clone(d.t.Findings[first:])
	d.record(id, findings)
	return derived, findings, nil
}

// record keeps, for every field rule the message's derivation handled, the
// source text and what it derived to.
func (d *Deriver) record(id string, findings []exportreview.Finding) {
	original, derived := d.t.original[id], d.t.derived[id]
	if original == nil || derived == nil {
		return
	}
	for _, rule := range d.t.policy.Fields {
		selector, err := hl7.ParseSelector(rule.Selector)
		if err != nil {
			continue
		}
		handled := slices.ContainsFunc(findings, func(f exportreview.Finding) bool {
			return f.Resolved && f.Reason == "named-field" && f.Location == "case/"+id+"/"+selector.String()
		})
		if !handled {
			continue
		}
		before, ok := readText(original, selector)
		if !ok || before == "" {
			continue
		}
		after, _ := readText(derived, selector)
		example := Example{Original: before, Derived: after}
		if held := d.examples[selector.String()]; len(held) < maxExamples && !slices.Contains(held, example) {
			d.examples[selector.String()] = append(held, example)
		}
		held := d.values[before]
		switch {
		case held == nil:
			d.values[before] = &DerivedValue{Derived: after, Rule: rule}
		case held.Derived != after || held.Rule.Policy != rule.Policy:
			held.Conflict = true
		}
	}
}

func readText(doc *hl7.Document, selector hl7.Selector) (string, bool) {
	value, err := doc.Read(0, selector, hl7.IgnoreMSH18)
	if err != nil || value.State != hl7.Present {
		return "", err == nil
	}
	return value.Text()
}

// Lookup is what a source value the derived messages held derived to, for a
// report restating it. It is nil when no handled field held exactly that text.
func (d *Deriver) Lookup(text string) *DerivedValue {
	return d.values[text]
}

// Examples are up to three source values the field at selector held and
// what each derived to, for a person who asks to see them.
func (d *Deriver) Examples(selector hl7.Selector) []Example {
	return slices.Clone(d.examples[selector.String()])
}

// Terms are the source values the derivation replaced: the known values a
// residual scan of anything made from it looks for.
func (d *Deriver) Terms() [][]byte {
	terms := make([][]byte, 0, len(d.t.local.ResidualValues))
	for _, value := range d.t.local.ResidualValues {
		terms = append(terms, slices.Clone(value))
	}
	return terms
}

// keyedSum is the keyed digest one derived surrogate or date shift is drawn
// from: labeled, so a surrogate and a date shift of the same scope never
// share a draw.
func (t *transformer) keyedSum(label, scope string) []byte {
	mac := hmac.New(sha256.New, t.keyed)
	mac.Write([]byte(label))
	mac.Write([]byte{0})
	mac.Write([]byte(scope))
	return mac.Sum(nil)
}
