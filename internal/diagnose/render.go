package diagnose

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"strings"
)

func JSON(report Report) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Markdown renders the same report model as JSON, including every finding and
// evidence reference. It never reparses payloads or independently runs rules.
func Markdown(report Report) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# readmit diagnosis\n\nCase identity: `%s`\n\nConfiguration SHA-256: `%s`\n\nProfile: `%s`\n\nRuleset: `%s`\n\nRules examined: %s\n\n", report.CaseIdentity, report.ConfigSHA256, report.Profile, report.Ruleset, strings.Join(report.Rules, ", "))
	fmt.Fprintf(&out, "## Observed case window\n\n%s\n\n", report.Window.Description)
	for _, source := range report.Window.Sources {
		fmt.Fprintf(&out, "- %s: %s through %s (source sequence, not inferred chronology)\n", source.SourceID, source.FirstOccurrence, source.LastOccurrence)
	}
	fmt.Fprintf(&out, "\n%s\n", report.Scope)
	if report.NoFindings != "" {
		fmt.Fprintf(&out, "\n%s\n", report.NoFindings)
	}
	fmt.Fprint(&out, "\n## Findings\n")
	for _, finding := range report.Findings {
		fmt.Fprintf(&out, "\n### %s — %s\n\nClassification: %s; profile: %s; ruleset: %s.\n\n%s\n", finding.ID, finding.RuleID, finding.Classification, finding.Profile, finding.Ruleset, finding.Summary)
		if finding.Window != "" {
			fmt.Fprintf(&out, "\nWindow: %s\n", finding.Window)
		}
		fmt.Fprintln(&out)
		for _, evidence := range finding.Evidence {
			fmt.Fprintf(&out, "- %s, %s: %s", evidence.Occurrence, evidence.Field, evidence.State)
			if evidence.Offset != nil {
				fmt.Fprintf(&out, " (payload byte offset %d, length %d)", *evidence.Offset, *evidence.Length)
			}
			fmt.Fprintln(&out)
		}
	}
	fmt.Fprint(&out, "\n## Unsupported coverage\n\n")
	if len(report.Unsupported) == 0 {
		fmt.Fprintln(&out, "None.")
	}
	for _, item := range report.Unsupported {
		fmt.Fprintf(&out, "- %s %s %s: %s\n", item.Code, item.Occurrence, item.Field, item.Detail)
	}
	return out.Bytes()
}
