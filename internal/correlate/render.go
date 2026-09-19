package correlate

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

// JSON encodes the report deterministically as one readmit-correlation/v1
// document. It reopens no evidence and re-decides nothing.
func JSON(report Report) ([]byte, error) {
	data, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode correlation report")
	}
	return append(data, '\n'), nil
}

// Terminal renders the same report model as JSON, including every declared
// rule that reached nothing and every collision that merged nothing.
func Terminal(report Report) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "Contract: %s\nCase identity: %s\nRules SHA-256: %s\nRecorded session: %t\n",
		report.Schema, report.CaseIdentity, report.RulesSHA256, report.SessionDeclared)
	fmt.Fprintf(&out, "Occurrences: %d\nLinks: %d (%d observed, %d inferred)\nCollisions: %d\nUnsupported: %d\n",
		report.Summary.Occurrences, report.Summary.Links, report.Summary.Observed, report.Summary.Inferred,
		report.Summary.Collisions, report.Summary.Unsupported)

	fmt.Fprintln(&out, "\nDeclared rules:")
	for _, rule := range report.Rules {
		scopeText := string(rule.Scope)
		if rule.Scope == DeclaredScope {
			scopeText += " " + strings.Join(rule.Sources, ",")
		}
		fmt.Fprintf(&out, "  %s operator=%s scope=%s applied=%t considered=%d linked=%d unlinked=%d\n",
			rule.ID, rule.Operator, scopeText, rule.Applied, rule.Considered, rule.Linked, rule.Unlinked)
	}

	fmt.Fprintln(&out, "\nLinks:")
	if len(report.Links) == 0 {
		fmt.Fprintln(&out, "  None.")
	}
	for _, link := range report.Links {
		fmt.Fprintf(&out, "  %s rule=%s linkage=%s", link.ID, link.Rule, link.Linkage)
		if link.Authority != "" {
			fmt.Fprintf(&out, " authority=%s", link.Authority)
		}
		fmt.Fprintf(&out, " %s\n", references(link.Occurrences))
	}

	fmt.Fprintln(&out, "\nCollisions (nothing was merged):")
	if len(report.Collisions) == 0 {
		fmt.Fprintln(&out, "  None.")
	}
	for _, collision := range report.Collisions {
		fmt.Fprintf(&out, "  rule=%s reason=%s", collision.Rule, collision.Reason)
		if collision.Declaring != nil {
			fmt.Fprintf(&out, " declaring=%s", collision.Declaring.Occurrence)
		}
		fmt.Fprintf(&out, " candidates=%s\n", references(collision.Occurrences))
	}

	fmt.Fprintln(&out, "\nUnsupported (not evaluated, not passed):")
	if len(report.Unsupported) == 0 {
		fmt.Fprintln(&out, "  None.")
	}
	for _, item := range report.Unsupported {
		fmt.Fprintf(&out, "  %s", item.Code)
		for _, part := range []struct{ name, value string }{{"rule", item.Rule}, {"occurrence", item.Occurrence}, {"field", item.Field}} {
			if part.value != "" {
				fmt.Fprintf(&out, " %s=%s", part.name, part.value)
			}
		}
		fmt.Fprintf(&out, ": %s\n", item.Detail)
	}

	fmt.Fprintf(&out, "\n%s\n", report.Scope)
	return out.Bytes()
}

func references(refs []Reference) string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Occurrence+"("+ref.Kind+")")
	}
	return strings.Join(names, " ")
}
