package diff

import (
	"errors"

	"github.com/bharm16/readmit/internal/hl7"
)

// outcomeCounts is the one place an outcome becomes a number. A rule's counts
// and the report's totals are filed through it together, so the two can never
// disagree about what a rule did.
type outcomeCounts struct{ suppressed, retained, undecided, unaddressed int }

func (c *outcomeCounts) add(outcome string) {
	switch outcome {
	case Suppressed:
		c.suppressed++
	case Retained:
		c.retained++
	case Undecided:
		c.undecided++
	default:
		c.unaddressed++
	}
}

// appliedRule is one authored rule and what it has established so far.
type appliedRule struct {
	rule     Rule
	selector string
	compared int
	counts   outcomeCounts
}

// appliedPolicy is the per-comparison state a rule set accumulates. It is
// threaded through the one comparison the engine already performs; nothing here
// reopens evidence or compares a field a second time.
type appliedPolicy struct {
	order       []*appliedRule
	bySelector  map[string]*appliedRule
	totals      outcomeCounts
	differences []Difference
}

// Normalize runs exactly the comparison Compare runs and reports what an
// authored policy did to it.
//
// It refuses two options rather than accepting them quietly. A declared ignore
// selector would suppress a difference this report could not attribute to a
// named rule, and displayed values have no member to live in here: deciding
// equivalence is the only thing this package does with a decoded value, and the
// decision is all that leaves.
func Normalize(left, right Input, options Options, policy Policy) (NormalizationReport, error) {
	if err := policy.Validate(); err != nil {
		return NormalizationReport{}, err
	}
	if len(options.Ignore) > 0 {
		return NormalizationReport{}, errors.New("a normalization report suppresses differences only through its policy")
	}
	if options.ShowValues {
		return NormalizationReport{}, errors.New("a normalization report never displays values")
	}
	applied := &appliedPolicy{bySelector: make(map[string]*appliedRule, len(policy.Rules))}
	for _, rule := range policy.Rules {
		selector, err := hl7.ParseSelector(rule.Selector)
		if err != nil {
			return NormalizationReport{}, err
		}
		entry := &appliedRule{rule: rule, selector: selector.String()}
		applied.order = append(applied.order, entry)
		applied.bySelector[entry.selector] = entry
	}
	raw, err := compare(left, right, options, applied)
	if err != nil {
		return NormalizationReport{}, err
	}
	report := NormalizationReport{
		Schema: NormalizationSchema, PolicySchema: policy.Schema, Scope: normalizationScope,
		Boundary: raw.Boundary, Left: raw.Left, Right: raw.Right,
		Alignment: raw.Alignment, Keys: raw.Keys, Fields: raw.Fields,
		Differences: applied.differences, Unsupported: raw.Unsupported,
		Summary: NormalizationSummary{
			Paired:      raw.Summary.Paired,
			Suppressed:  applied.totals.suppressed,
			Retained:    applied.totals.retained,
			Undecided:   applied.totals.undecided,
			Unaddressed: applied.totals.unaddressed,
			Inserted:    raw.Summary.Inserted, Missing: raw.Summary.Missing,
			Ambiguous: raw.Summary.Ambiguous, Unaligned: raw.Summary.Unaligned,
		},
	}
	for _, difference := range applied.differences {
		if difference.Status == "uncompared" {
			report.Summary.Uncompared++
		} else {
			report.Summary.Differences++
		}
	}
	for _, entry := range applied.order {
		report.Rules = append(report.Rules, RuleReport{
			ID: entry.rule.ID, Selector: entry.selector, Operator: entry.rule.Operator,
			Precision: entry.rule.Precision, Tolerance: entry.rule.Tolerance,
			Compared: entry.compared, Suppressed: entry.counts.suppressed,
			Retained: entry.counts.retained, Undecided: entry.counts.undecided,
		})
	}
	return report, nil
}

// compared counts one selection a rule was scoped to, whether or not the two
// sides differ, so a rule that saw the field and had nothing to do is told
// apart from one that never saw it.
func (p *appliedPolicy) compared(selector string) {
	if entry, addressed := p.bySelector[selector]; addressed {
		entry.compared++
	}
}

// record files one reported field and the rule outcome for it. A field no rule
// addressed is recorded too: this list is everything the comparison reported,
// not the part a policy happened to touch. A field this build did not decode is
// undecided whatever any rule says, because naming an undecodable field in a
// policy does not make it comparable.
func (p *appliedPolicy) record(left, right *occurrence, change FieldChange, leftBytes, rightBytes []byte) {
	difference := Difference{
		LeftOccurrence: left.ref.Occurrence, RightOccurrence: right.ref.Occurrence,
		Selector: change.Selector, Name: change.Name, Status: change.Status,
		LeftState: change.Left.State, RightState: change.Right.State,
		Outcome: Unaddressed,
	}
	if change.Status == "uncompared" {
		difference.Outcome, difference.Reason = Undecided, UnsupportedEvidence
	}
	if entry, addressed := p.bySelector[change.Selector]; addressed {
		difference.Rule = entry.rule.ID
		difference.Outcome, difference.Reason = entry.rule.decide(change.Left, change.Right, leftBytes, rightBytes)
		entry.counts.add(difference.Outcome)
	}
	p.totals.add(difference.Outcome)
	p.differences = append(p.differences, difference)
}
