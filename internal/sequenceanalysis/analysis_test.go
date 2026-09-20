package sequenceanalysis_test

import (
	"github.com/bharm16/readmit/internal/sequenceanalysis"
	"strings"
	"testing"
)

const declaration = `{"schema":"readmit-sequence-analysis/v1","case_identity":"identity","rules_sha256":"","clock_tolerance_seconds":0,"windows":[{"source":"s0001","start":"2026-01-01T00:00:00Z","end":"2026-01-01T01:00:00Z","coverage":"partial"}],"retries":[{"first":"s0001-e000001","retry":"s0001-e000002","basis":"operator_reported_retry"}],"downstream":[{"occurrence":"s0001-e000001","source":"s0002","rule":"control"}]}`

func TestStrictSequenceAnalysisRefusesUnknownMissingNullAndOversize(t *testing.T) {
	if _, err := sequenceanalysis.Parse([]byte(declaration)); err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		strings.Replace(declaration, `"clock_tolerance_seconds":0,`, "", 1),
		strings.Replace(declaration, `"clock_tolerance_seconds":0`, `"clock_tolerance_seconds":null`, 1),
		strings.Replace(declaration, `"clock_tolerance_seconds":0`, `"clock_tolerance_seconds":-1`, 1),
		strings.Replace(declaration, `"coverage":"partial"`, `"coverage":"partial","secret":"patient"`, 1),
		strings.Replace(declaration, `"start":"2026-01-01T00:00:00Z",`, "", 1),
		strings.Replace(declaration, `"basis":"operator_reported_retry"`, `"basis":null`, 1),
		strings.Replace(declaration, `"rule":"control"`, `"rule":"control","secret":true`, 1),
		strings.Replace(declaration, `"schema":`, `"schema":"duplicate","schema":`, 1),
		strings.Replace(declaration, `/v1`, `/v2`, 1),
		strings.Replace(declaration, `00:00:00Z`, `00:00:00-00:00`, 1),
		declaration + `{}`,
		strings.Repeat(" ", sequenceanalysis.MaxBytes+1),
	}
	for _, input := range invalid {
		if _, err := sequenceanalysis.Parse([]byte(input)); err == nil {
			t.Fatal("invalid declaration accepted")
		} else if strings.Contains(err.Error(), "patient") {
			t.Fatal("value leaked")
		}
	}
}
