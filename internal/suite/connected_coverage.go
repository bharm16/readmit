package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/runqueue"
)

const ConnectedCoverageSchema = "readmit-suite-coverage/v2"

type ConnectedCoverageSpecification struct {
	Job        string `json:"job"`
	Plan       string `json:"plan"`
	Definition string `json:"definition"`
	Release    string `json:"release"`
}

func (p *ConnectedCoverageSpecification) UnmarshalJSON(raw []byte) error {
	type plain ConnectedCoverageSpecification
	return required(raw, (*plain)(p), "job", "plan", "definition", "release")
}

type ConnectedCoverageDocument struct {
	Schema         string                           `json:"schema"`
	SuiteSHA256    string                           `json:"suite_sha256"`
	Specifications []ConnectedCoverageSpecification `json:"specifications"`
	Requirements   []Requirement                    `json:"requirements"`
	Exclusions     []Exclusion                      `json:"exclusions"`
}

func (d *ConnectedCoverageDocument) UnmarshalJSON(raw []byte) error {
	type plain ConnectedCoverageDocument
	return required(raw, (*plain)(d), "schema", "suite_sha256", "specifications", "requirements", "exclusions")
}

func DecodeConnectedCoverage(raw []byte) (ConnectedCoverageDocument, error) {
	var d ConnectedCoverageDocument
	fail := errors.New("invalid connected suite coverage declaration")
	if len(raw) > MaxBytes || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != ConnectedCoverageSchema || !validDigest(d.SuiteSHA256) || len(d.Specifications) < 1 || len(d.Specifications) > 64 {
		return d, fail
	}
	// Reuse the existing requirement/exclusion vocabulary and denominator
	// rules, while the new envelope pins connected plans and approvals.
	legacy := CoverageDocument{Schema: CoverageSchema, SuiteSHA256: d.SuiteSHA256, Specifications: []CoverageSpecification{}, Requirements: d.Requirements, Exclusions: d.Exclusions}
	for _, p := range d.Specifications {
		if !validDigest(p.Plan) || !validDigest(p.Definition) || !validDigest(p.Release) {
			return d, fail
		}
		legacy.Specifications = append(legacy.Specifications, CoverageSpecification{Job: p.Job, SHA256: p.Plan})
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		return d, fail
	}
	if _, err = DecodeCoverage(encoded); err != nil {
		return d, err
	}
	return d, nil
}

// AssessConnectedCoverage reads actual child verdicts through the one sealed
// suite reader. It performs no new test, collection, approval or validation.
func AssessConnectedCoverage(ctx context.Context, directory, policy string, now time.Time) (CoverageReport, error) {
	raw, err := read(policy, MaxBytes)
	if err != nil {
		return CoverageReport{}, err
	}
	doc, err := DecodeConnectedCoverage(raw)
	if err != nil {
		return CoverageReport{}, err
	}
	execution, err := OpenConnectedExecution(ctx, directory)
	if err != nil {
		return CoverageReport{}, err
	}
	if execution.Preparation.Suite != doc.SuiteSHA256 || len(execution.Queue.Jobs) != len(doc.Specifications) {
		return CoverageReport{}, errors.New("connected coverage differs from the exact suite denominator")
	}
	pins := map[string]ConnectedCoverageSpecification{}
	for _, pin := range doc.Specifications {
		pins[pin.Job] = pin
	}
	result := CoverageReport{Suite: execution.Preparation.Suite, Environment: execution.Preparation.Environment, Scope: "pinned connected plans and approved expectations", At: now.UTC(), Denominator: len(doc.Requirements), Requirements: []RequirementCoverage{}, Jobs: []JobCoverage{}}
	eligible := map[string]bool{}
	for i, job := range execution.Queue.Jobs {
		test := execution.Document.Tests[i]
		pin, ok := pins[job.ID]
		if !ok || pin.Plan != job.PlanIdentity || pin.Definition != test.Definition || pin.Release != test.ReleaseIdentity {
			return CoverageReport{}, errors.New("connected coverage plan or release pin differs")
		}
		actual := execution.Report.Jobs[i]
		qualifies := !actual.ExecutionError && actual.Admission == runqueue.Executed && actual.Flow != nil && actual.Flow.State == "complete" && actual.Flow.Verdict == assertion.VerdictPass && job.State == "enabled"
		view := JobCoverage{ID: job.ID, Eligible: qualifies, Execution: string(actual.Admission)}
		for _, exclusion := range doc.Exclusions {
			if exclusion.Job == job.ID {
				view.Eligible = false
				view.Exclusion = exclusion.State
				view.Reason = exclusion.Reason
			}
		}
		eligible[job.ID] = view.Eligible
		result.Jobs = append(result.Jobs, view)
	}
	for _, requirement := range doc.Requirements {
		pass := true
		for _, job := range requirement.Jobs {
			pass = pass && eligible[job]
		}
		state := "incomplete"
		if pass {
			state = "passed"
			result.Passed++
		}
		result.Requirements = append(result.Requirements, RequirementCoverage{ID: requirement.ID, Jobs: requirement.Jobs, State: state})
	}
	if result.Denominator > 0 {
		result.Percent = 100 * float64(result.Passed) / float64(result.Denominator)
	}
	return result, nil
}
