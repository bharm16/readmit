package redact

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Late proof/diagnosis/scan refusals retain a located private attempt review.
// They never produce a completed public export or silently lose the findings.
func blockedAttempt(stage string, review *Review, findings []exportreview.Finding, scan exportreview.Scan) error {
	value := struct {
		Schema         string                 `json:"schema"`
		State          string                 `json:"state"`
		ApprovedReview string                 `json:"approved_review_identity"`
		Findings       []exportreview.Finding `json:"findings"`
		Residual       exportreview.Scan      `json:"residual_scan"`
		Scope          string                 `json:"scope"`
	}{Schema: "readmit-export-attempt/v1", State: "blocked", ApprovedReview: review.Identity, Findings: findings, Residual: scan, Scope: exportreview.Scope}
	raw, err := encode(value)
	if err != nil {
		return err
	}
	if err := writeFile(stage, "attempt-review.json", raw); err != nil {
		return err
	}
	return errors.New("generated artifact review blocked; located attempt-review.json and proof remain in local-state; no export written")
}

func proofFindings(dir string, required []int) []exportreview.Finding {
	findings := []exportreview.Finding{}
	add := func(location, reason string) {
		findings = append(findings, exportreview.Finding{Location: location, Class: "other-unique-identifiers", Reason: reason, Resolved: false})
	}
	for _, mode := range []string{"baseline", "postfix"} {
		result, err := testrunner.Open(filepath.Join(dir, mode, "result"))
		if err != nil {
			add("proof/"+mode+"/result", "unverified-result")
			continue
		}
		if result.Result.Status == testrunner.ExecutionError {
			add("proof/"+mode+"/result", "execution-error-is-not-proof")
			continue
		}
		for i, assertion := range result.Result.Assertions {
			shouldFail := mode == "baseline" && slices.Contains(required, i+1)
			if (assertion.Status == "failed") != shouldFail {
				add(fmt.Sprintf("proof/%s/assertions/%d", mode, i+1), "assertion-failure-set-changed")
			}
		}
	}
	if len(findings) == 0 {
		add("proof", "fixture-proof-could-not-be-verified")
	}
	return findings
}

func residualFindings(scan exportreview.Scan) []exportreview.Finding {
	findings := []exportreview.Finding{}
	for _, location := range scan.Locations {
		findings = append(findings, exportreview.Finding{Location: location, Class: "other-unique-identifiers", Reason: "known-residual", Resolved: false})
	}
	return findings
}
