package diagnose

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

func JSON(report Report) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

var (
	findingPattern  = regexp.MustCompile(`^f[0-9]{6}$`)
	identityPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ParseReport reads a retained diagnosis: the one a review is made over, and
// the one the window reopens. It is the same strict reading every other reader
// of a versioned document does; readmit-diagnosis/v1 gains no member here and
// changes no byte.
func ParseReport(data []byte) (Report, error) {
	if len(data) > MaxReportBytes {
		return Report{}, errors.New("diagnosis report exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(data, &declared) != nil {
		return Report{}, errors.New("invalid diagnosis report")
	}
	if declared.Schema != Schema {
		return Report{}, errors.New("diagnosis report declares a contract version this release does not read")
	}
	var report Report
	if json.Unmarshal(data, &report, json.RejectUnknownMembers(true)) != nil {
		return Report{}, errors.New("invalid diagnosis report")
	}
	for _, finding := range report.Findings {
		if !findingPattern.MatchString(finding.ID) {
			return Report{}, errors.New("a diagnosis report names each finding once, as this release writes them")
		}
	}
	if !identityPattern.MatchString(report.CaseIdentity) {
		return Report{}, errors.New("a diagnosis report names the verified identity of the case it was run over")
	}
	return report, nil
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
