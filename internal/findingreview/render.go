package findingreview

import (
	"bytes"
	"encoding/json/v2"
	"fmt"

	"github.com/bharm16/readmit/internal/hl7"
)

func JSON(record Record) ([]byte, error) {
	data, err := json.Marshal(record, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Markdown renders the same record as JSON. It re-reads no case, re-derives no
// expectation and decides nothing: what it prints is what the record holds.
func Markdown(record Record) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# readmit finding review\n\nEngine build: `%s`\n\nDiagnosis contract: `%s`\n\nReport SHA-256: `%s`\n\nDecisions SHA-256: `%s`\n\nCase identity: `%s`\n\nConfiguration SHA-256: `%s`\n\nProfile: `%s`\n\nRuleset: `%s`\n\nPromotion boundary: `%s`\n\n%s\n",
		record.Engine, record.Diagnosis.Schema, record.Diagnosis.Report, record.Decisions, record.Diagnosis.CaseIdentity, record.Diagnosis.ConfigSHA256, record.Diagnosis.Profile, record.Diagnosis.Ruleset, record.Boundary, record.Statement)
	fmt.Fprint(&out, "\n## Reviewed findings\n")
	if len(record.Findings) == 0 {
		fmt.Fprint(&out, "\nThis diagnosis produced no findings, so there was nothing to review.\n")
	}
	for _, status := range record.Findings {
		fmt.Fprintf(&out, "\n### %s — %s\n\nClassification: %s; verdict: %s (%s).\n", status.Finding, status.RuleID, status.Classification, status.Verdict, status.Basis)
		if status.Scope != "" {
			fmt.Fprintf(&out, "\nSuppression scope: %s.\n", status.Scope)
		}
		if status.SuppressedBy != "" {
			fmt.Fprintf(&out, "\nSuppressed by the decision recorded on %s.\n", status.SuppressedBy)
		}
		if status.Rationale != "" {
			fmt.Fprintf(&out, "\nReason given: %s\n", status.Rationale)
		}
		fmt.Fprintf(&out, "\nNext evidence: %s\n", status.NextEvidence)
		promotion(&out, status)
	}
	return out.Bytes()
}

// promotion prints what one reviewed finding became, and says plainly when it
// became nothing: a finding this release cannot express as a test is named
// here, never left out.
func promotion(out *bytes.Buffer, status Status) {
	if status.Promotion == nil {
		fmt.Fprint(out, "\nNot promoted: only a finding a person confirmed becomes a test.\n")
		return
	}
	if len(status.Promotion.Expectations) == 0 {
		fmt.Fprint(out, "\nConfirmed, and this release expresses no part of it as a test. The reasons are listed below; nothing weaker was written in its place.\n")
	} else {
		fmt.Fprintf(out, "\nPromoted to %d draft assertions over %d sent occurrences:\n\n", len(status.Promotion.Expectations), len(status.Promotion.Messages))
		for _, expectation := range status.Promotion.Expectations {
			fmt.Fprintf(out, "- `%s`: %s of the acknowledgement to %s at `%s` is %s", expectation.ID, expectation.Operator, expectation.Message, expectation.Selector, expectation.Field.State)
			if expectation.Field.State == hl7.Present {
				fmt.Fprintf(out, " `%s`", *expectation.Field.Text)
			}
			fmt.Fprintln(out)
		}
	}
	if len(status.Promotion.Unsupported) == 0 {
		return
	}
	fmt.Fprint(out, "\nNot expressible as a test:\n\n")
	for _, item := range status.Promotion.Unsupported {
		fmt.Fprintf(out, "- %s %s %s: %s\n", item.Code, item.Occurrence, item.Field, item.Detail)
	}
}
