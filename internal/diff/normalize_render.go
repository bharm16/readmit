package diff

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

func NormalizationJSON(report NormalizationReport) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode normalization report")
	}
	return append(data, '\n'), nil
}

// All three renderings carry the same content, including every rule that
// addressed nothing and every difference a rule suppressed. A rendering that
// dropped the suppressed ones would be the failure this report exists to
// prevent: a reader believing two collections agree where nobody looked.
func NormalizationTerminal(report NormalizationReport) []byte {
	return write(normalizationContent(report), false)
}

func NormalizationMarkdown(report NormalizationReport) []byte {
	return write(normalizationContent(report), true)
}

func normalizationContent(report NormalizationReport) []line {
	lines := []line{{true, "readmit normalization policy"}}
	add := func(format string, args ...any) { lines = append(lines, line{text: fmt.Sprintf(format, args...)}) }
	heading := func(text string) { lines = append(lines, line{heading: true, text: text}) }
	add("Contract: %s; policy contract: %s", report.Schema, report.PolicySchema)
	add("Boundary: %s. %s", report.Boundary, report.Scope)
	add("Values are never displayed by this report and source paths are never included.")
	for _, side := range []struct {
		name    string
		summary InputSummary
	}{{"Left", report.Left}, {"Right", report.Right}} {
		s := side.summary
		add("%s: %s; identity=%s; %s; occurrences=%d; excluded by boundary=%d", side.name, s.Kind, s.Identity, s.Payloads, s.Occurrences, s.Excluded)
		if s.SourceIdentity != "" {
			add("%s source identity: %s", side.name, s.SourceIdentity)
		}
		if s.TargetIdentity != "" {
			add("%s target configuration identity: %s", side.name, s.TargetIdentity)
		}
		if s.ResultStatus != "" {
			add("%s recorded result: %s; its observation boundary=%s (not evaluated here)", side.name, s.ResultStatus, s.ResultBoundary)
		}
	}
	add("Alignment: %s", report.Alignment)
	if len(report.Keys) == 0 {
		add("Declared keys: none")
	} else {
		add("Declared keys: %s (used only for declared-keys alignment)", strings.Join(report.Keys, ", "))
	}
	if len(report.Fields) == 0 {
		add("Field scope: all encountered field repetitions")
	} else {
		add("Field scope: %s", strings.Join(report.Fields, ", "))
	}
	heading("Applied rules (exact selector scope)")
	for _, rule := range report.Rules {
		add("%s: %s on %s%s; compared=%d; suppressed=%d; retained=%d; undecided=%d",
			rule.ID, rule.Operator, rule.Selector, parameter(rule), rule.Compared, rule.Suppressed, rule.Retained, rule.Undecided)
	}
	heading("Summary")
	s := report.Summary
	add("Paired=%d; differences found=%d; uncompared fields=%d; suppressed=%d; retained=%d; undecided=%d; unaddressed=%d", s.Paired, s.Differences, s.Uncompared, s.Suppressed, s.Retained, s.Undecided, s.Unaddressed)
	add("Not addressable by any rule: inserted=%d; missing=%d; ambiguous groups=%d; unaligned=%d; unsupported=%d", s.Inserted, s.Missing, s.Ambiguous, s.Unaligned, len(report.Unsupported))
	for _, group := range []struct{ title, outcome string }{
		{"Suppressed differences (previewed, never hidden)", Suppressed},
		{"Differences a rule did not suppress", Retained},
		{"Differences nothing here could decide", Undecided},
		{"Differences no rule addressed", Unaddressed},
	} {
		heading(group.title)
		shown := 0
		for _, difference := range report.Differences {
			if difference.Outcome != group.outcome {
				continue
			}
			shown++
			add("%s", differenceText(difference))
		}
		if shown == 0 {
			add("None")
		}
	}
	heading("Unsupported evidence and labels")
	if len(report.Unsupported) == 0 {
		add("None")
	}
	for _, item := range report.Unsupported {
		add("%s %s %s: %s", item.Side, item.Occurrence, item.Selector, item.Code)
	}
	return lines
}

func parameter(rule RuleReport) string {
	if rule.Precision != "" {
		return " to " + rule.Precision + " precision"
	}
	if rule.Tolerance != "" {
		return " within " + rule.Tolerance
	}
	return ""
}

func differenceText(difference Difference) string {
	label := difference.Selector
	if difference.Name != "" {
		label += " " + difference.Name
	}
	text := fmt.Sprintf("%s -> %s: %s: %s; left=%s; right=%s", difference.LeftOccurrence, difference.RightOccurrence, label, difference.Status, difference.LeftState, difference.RightState)
	if difference.Rule != "" {
		text += "; rule=" + difference.Rule
	}
	if difference.Reason != "" {
		text += "; reason=" + difference.Reason
	}
	return text
}
