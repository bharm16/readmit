package suite

import (
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"path/filepath"
	"time"
)

const CISchema = "readmit-suite-ci/v1"

// CIRequest selects the existing execution and optional coverage gates. It is
// an in-process request, not a stored contract or a source of implicit approval.
type CIRequest struct {
	Path, Environment, Output                        string
	Releases, Promotion, PromotionIdentity, Revision string
	Requirements                                     string
	Previous                                         []string
}

// CIReport contains only fixed vocabulary and counts. Customer-authored labels,
// paths, diagnostics, evidence identities and values never enter CI output.
type CIReport struct {
	Schema   string `json:"schema"`
	State    string `json:"state"`
	ExitCode int    `json:"exit_code"`
	Jobs     int    `json:"jobs"`
	Executed int    `json:"executed"`
	Skipped  int    `json:"skipped"`
	Coverage string `json:"coverage"`
}

func (r *CIReport) UnmarshalJSON(raw []byte) error {
	type plain CIReport
	return required(raw, (*plain)(r), "schema", "state", "exit_code", "jobs", "executed", "skipped", "coverage")
}

func DecodeCI(raw []byte) (CIReport, error) {
	var r CIReport
	if len(raw) > 4096 || json.Unmarshal(raw, &r, json.RejectUnknownMembers(true)) != nil || r.Schema != CISchema || r.Jobs < 0 || r.Jobs > 64 || r.Executed < 0 || r.Executed > r.Jobs || r.Skipped < 0 || r.Skipped > r.Jobs-r.Executed || r.ExitCode < 0 || r.ExitCode > 2 {
		return CIReport{}, errors.New("invalid CI summary")
	}
	switch r.State {
	case "passed":
		if r.ExitCode != 0 || r.Jobs == 0 || r.Executed != r.Jobs || r.Skipped != 0 || r.Coverage == "failed" {
			return CIReport{}, errors.New("invalid passing CI summary")
		}
	case "failed":
		if r.ExitCode != 1 || r.Coverage == "failed" {
			return CIReport{}, errors.New("invalid failed CI summary")
		}
	case "error":
		if r.ExitCode != 2 {
			return CIReport{}, errors.New("invalid error CI summary")
		}
	default:
		return CIReport{}, errors.New("invalid CI summary state")
	}
	if r.Coverage != "not_requested" && r.Coverage != "passed" && r.Coverage != "failed" {
		return CIReport{}, errors.New("invalid CI coverage state")
	}
	return r, nil
}

// CIError is a safe default even when admission fails before any job exists.
func CIError() CIReport {
	return CIReport{Schema: CISchema, State: "error", ExitCode: 2, Coverage: "not_requested"}
}

// RunCI executes once and keeps the unchanged durable suite evidence. Only the
// new aggregate files are suitable for a CI log; neither is execution evidence.
// It is a reporting wrapper: the run itself is the one Run over the one
// Request, and nothing here chooses among execution variants.
func RunCI(ctx context.Context, request CIRequest) CIReport {
	result := CIError()
	if request.Path == "" || request.Environment == "" || request.Output == "" || len(request.Previous) > 15 || (request.Requirements == "" && len(request.Previous) > 0) {
		return result
	}
	run := Request{Path: request.Path, Environment: request.Environment, Output: request.Output,
		References: request.Releases, Promotion: request.Promotion, PromotionIdentity: request.PromotionIdentity, Revision: request.Revision}
	if err := run.Validate(); err != nil {
		return result
	}
	var coverageRaw []byte
	if request.Requirements != "" {
		result.Coverage = "failed"
		var err error
		coverageRaw, err = read(request.Requirements, MaxBytes)
		if err != nil {
			return result
		}
		if _, err = DecodeCoverage(coverageRaw); err != nil {
			return result
		}
	}
	// A preparation failure can mean the directory belongs to another run. Never
	// write into it. Storage failures also remain errors even if some jobs passed.
	report, err := Run(ctx, run)
	if err != nil {
		return result
	}
	result.Jobs = len(report.Jobs)
	result.Executed = report.Executed
	result.Skipped = report.Skipped
	result.ExitCode = report.ExitCode()
	if result.Jobs == 0 || ctx.Err() != nil {
		result.ExitCode = 2
	}
	if coverageRaw != nil {
		if retain(request.Output, "ci-coverage.json", coverageRaw) != nil {
			return CIError()
		}
		coverage, e := AssessCoverage(ctx, request.Output, filepath.Join(request.Output, "ci-coverage.json"), request.Previous, time.Now().UTC())
		eligible := e == nil && coverage.Passed == coverage.Denominator
		for _, job := range coverage.Jobs {
			eligible = eligible && job.Eligible
		}
		if eligible {
			result.Coverage = "passed"
		} else {
			result.ExitCode = 2
		}
	}
	result.State = "passed"
	if result.ExitCode == 1 {
		result.State = "failed"
	} else if result.ExitCode != 0 {
		result.State = "error"
	}
	encoded, e := json.Marshal(result, json.Deterministic(true))
	if e != nil || retain(request.Output, "ci.json", encoded) != nil {
		return CIError()
	}
	junit := ciJUnit{Name: "readmit", Tests: 1, Case: ciJUnitCase{Name: "saved-suite-gate", Class: "readmit"}}
	if result.ExitCode != 0 {
		junit.Failures = 1
		junit.Case.Failure = &ciJUnitFailure{Message: "Suite gate did not pass; inspect retained evidence privately"}
	}
	encoded, e = xml.MarshalIndent(junit, "", "  ")
	if e != nil || retain(request.Output, "junit.xml", append([]byte(xml.Header), encoded...)) != nil {
		return CIError()
	}
	return result
}

type ciJUnit struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Case     ciJUnitCase `xml:"testcase"`
}
type ciJUnitCase struct {
	Name    string          `xml:"name,attr"`
	Class   string          `xml:"classname,attr"`
	Failure *ciJUnitFailure `xml:"failure,omitempty"`
}
type ciJUnitFailure struct {
	Message string `xml:"message,attr"`
}
