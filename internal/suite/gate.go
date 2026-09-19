package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/testrunner"
)

const GatePolicySchema = "readmit-ci-gate-policy/v1"
const GateReportSchema = "readmit-ci-gate/v1"

// GatePolicy is reviewed customer-local data. Its externally selected identity
// binds approval, the baseline results, exclusions, pins and retention together.
// Local reviewer labels and hashes are not authenticated team authorization.
type GatePolicy struct {
	Schema      string                  `json:"schema"`
	Environment string                  `json:"environment"`
	Revision    string                  `json:"revision_assumption"`
	Engine      string                  `json:"engine"`
	Promotion   string                  `json:"promotion_identity"`
	Coverage    CoverageDocument        `json:"coverage"`
	Baseline    []CoverageSpecification `json:"baseline_results"`
	MaxBytes    int64                   `json:"max_bytes"`
	RetainUntil string                  `json:"retain_until"`
	Approver    string                  `json:"approver"`
	Rationale   string                  `json:"rationale"`
}

func (p GatePolicy) Identity() string { return identity(p) }
func (p *GatePolicy) UnmarshalJSON(raw []byte) error {
	type plain GatePolicy
	return required(raw, (*plain)(p), "schema", "environment", "revision_assumption", "engine", "promotion_identity", "coverage", "baseline_results", "retain_until", "max_bytes", "approver", "rationale")
}
func DecodeGatePolicy(raw []byte) (GatePolicy, error) {
	var p GatePolicy
	bad := errors.New("invalid CI gate policy")
	if len(raw) > MaxBytes || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != GatePolicySchema || p.MaxBytes < 1 || p.MaxBytes > gateMaxBytes || !identifier.MatchString(p.Environment) || !text(p.Revision, 256) || !text(p.Engine, 256) || !validDigest(p.Promotion) || !text(p.Approver, 256) || !text(p.Rationale, 1024) {
		return p, bad
	}
	coverage, _ := json.Marshal(p.Coverage)
	if _, e := DecodeCoverage(coverage); e != nil {
		return p, bad
	}
	until, e := time.Parse(time.RFC3339, p.RetainUntil)
	if e != nil || until.IsZero() || until.UTC().Format(time.RFC3339) != p.RetainUntil || len(p.Baseline) != len(p.Coverage.Specifications) {
		return p, bad
	}
	seen := map[string]bool{}
	for _, b := range p.Baseline {
		if !identifier.MatchString(b.Job) || seen[b.Job] || !validDigest(b.SHA256) {
			return p, bad
		}
		seen[b.Job] = true
	}
	for _, s := range p.Coverage.Specifications {
		if !seen[s.Job] {
			return p, bad
		}
	}
	return p, nil
}

// GateReport is safe for logs: no customer labels, paths or evidence hashes.
// Unknown is not failure evidence, and neither state can pass a change gate.
type GateReport struct {
	Schema         string `json:"schema"`
	State          string `json:"state"`
	ExitCode       int    `json:"exit_code"`
	Approval       string `json:"approval"`
	Pins           string `json:"pins"`
	Coverage       string `json:"coverage"`
	Baseline       string `json:"baseline"`
	Retention      string `json:"retention"`
	TargetRevision string `json:"target_revision"`
}

func gateUnknown() GateReport {
	return GateReport{GateReportSchema, "unknown", 2, "unknown", "unknown", "unknown", "unknown", "unknown", "unknown"}
}
func (r *GateReport) UnmarshalJSON(raw []byte) error {
	type plain GateReport
	return required(raw, (*plain)(r), "schema", "state", "exit_code", "approval", "pins", "coverage", "baseline", "retention", "target_revision")
}
func DecodeGateReport(raw []byte) (GateReport, error) {
	var r GateReport
	bad := errors.New("invalid CI gate summary")
	if len(raw) > 4096 || json.Unmarshal(raw, &r, json.RejectUnknownMembers(true)) != nil || r.Schema != GateReportSchema {
		return r, bad
	}
	for _, s := range []string{r.Approval, r.Pins, r.Coverage, r.Baseline} {
		if s != "passed" && s != "failed" && s != "unknown" {
			return r, bad
		}
	}
	if r.Retention != "retained" && r.Retention != "expired" && r.Retention != "unknown" {
		return r, bad
	}
	if r.TargetRevision != "unknown" && r.TargetRevision != "operator_asserted" {
		return r, bad
	}
	switch r.State {
	case "passed":
		if r.ExitCode != 0 || r.Approval != "passed" || r.Pins != "passed" || r.Coverage != "passed" || r.Baseline != "passed" || r.Retention != "retained" || r.TargetRevision != "operator_asserted" {
			return r, bad
		}
	case "failed":
		if r.ExitCode != 1 {
			return r, bad
		}
	case "unknown":
		if r.ExitCode != 2 {
			return r, bad
		}
	default:
		return r, bad
	}
	return r, nil
}

// assessGate only reads the copied snapshot. It does not reopen template paths,
// resolve credentials, probe a target, send, or trust old CI/JUnit verdicts.
func assessGate(ctx context.Context, root string, p GatePolicy, at time.Time) GateReport {
	r := gateUnknown()
	until, _ := time.Parse(time.RFC3339, p.RetainUntil)
	if !at.Before(until) {
		r.Retention = "expired"
		return r
	}
	r.Retention = "retained"
	current := filepath.Join(root, "current")
	prior := filepath.Join(root, "baseline")
	currentSuite, e := loadCoverageSuite(current, p.Coverage.SuiteSHA256)
	if e != nil {
		return r
	}
	priorSuite, e := loadCoverageSuite(prior, p.Coverage.SuiteSHA256)
	if e != nil {
		return r
	}
	for _, s := range []coverageSuite{currentSuite, priorSuite} {
		if len(s.admissions) != len(s.queue.Jobs) || s.selection.Environment != p.Environment {
			return r
		}
		if e = gateApproval(s, p); e != nil {
			return r
		}
	}
	r.Approval = "passed"
	r.TargetRevision = "operator_asserted"
	pins := map[string]string{}
	for _, b := range p.Baseline {
		pins[b.Job] = b.SHA256
	}
	for _, job := range currentSuite.queue.Jobs {
		if e = ctx.Err(); e != nil {
			return r
		}
		left := filepath.Join(prior, "runs", job.ID)
		right := filepath.Join(current, "runs", job.ID)
		for _, path := range []string{left, right} {
			pin, e := durablerun.Engine(path)
			if e != nil || pin.Engine != p.Engine {
				return r
			}
		}
		comparison, e := runcompare.Compare(ctx, runcompare.Input{Baseline: left, Current: right})
		if e != nil || comparison.Baseline.Identity != pins[job.ID] || comparison.Specification != "unchanged" {
			return r
		}
		if comparison.Baseline.RunState != "passed" || comparison.Baseline.Status != string(testrunner.Pass) {
			return r
		}
		if comparison.Current.RunState != "passed" || comparison.Current.Status != string(testrunner.Pass) {
			if comparison.Current.RunState == "assertion_failed" {
				r.State = "failed"
				r.ExitCode = 1
				r.Baseline = "failed"
			}
			return r
		}
		// Same outcomes under different retained inputs/configuration cannot prove
		// the declared unchanged-behavior gate. Missing pins remain unknown.
		for _, d := range comparison.Drift.Drift {
			if d.Outcome != "unchanged" {
				return r
			}
		}
		for _, a := range comparison.Assertions {
			if a.Definition != "unchanged" || a.Behavior != "unchanged" {
				r.State = "failed"
				r.ExitCode = 1
				r.Baseline = "failed"
				return r
			}
		}
	}
	r.Pins = "passed"
	r.Baseline = "passed"
	for _, directory := range []string{current, prior} {
		coverage, e := AssessCoverage(ctx, directory, filepath.Join(root, "coverage.json"), nil, at)
		if e != nil {
			return r
		}
		eligible := coverage.Passed == coverage.Denominator
		for _, j := range coverage.Jobs {
			eligible = eligible && j.Eligible
		}
		if !eligible {
			r.Coverage = "failed"
			r.State = "failed"
			r.ExitCode = 1
			return r
		}
	}
	r.Coverage = "passed"
	r.State = "passed"
	r.ExitCode = 0
	return r
}

func gateApproval(s coverageSuite, p GatePolicy) error {
	bad := errors.New("retained suite does not match reviewed approval")
	raw, e := read(filepath.Join(s.dir, "promotion.json"), MaxBytes)
	if e != nil {
		return bad
	}
	promotion, e := DecodePromotion(raw)
	if e != nil || promotion.Identity() != p.Promotion || promotion.Review.Environment != p.Environment || promotion.Review.Revision != p.Revision || promotion.Review.Suite != p.Coverage.SuiteSHA256 {
		return bad
	}
	raw, e = read(filepath.Join(s.dir, "release-references.json"), MaxBytes)
	if e != nil || promotionHash(raw) != promotion.Review.Releases {
		return bad
	}
	if len(promotion.Review.Jobs) != len(s.queue.Jobs) {
		return bad
	}
	approvedJobs := map[string]string{}
	for _, pin := range promotion.Review.Jobs {
		approvedJobs[pin.Job] = pin.SHA256
	}
	for _, job := range s.queue.Jobs {
		pin, exists := approvedJobs[job.ID]
		if !exists {
			return bad
		}
		retained, e := durablerun.RetainedInputIdentity(filepath.Join(s.dir, "runs", job.ID))
		if e != nil || retained != pin {
			return bad
		}
	}
	refs, e := DecodeReleases(raw)
	if e != nil || len(refs.Tests) != len(s.document.Tests) {
		return bad
	}
	for _, test := range s.document.Tests {
		pin := ""
		for _, ref := range refs.Tests {
			if ref.Test == test.ID {
				pin = ref.Identity
			}
		}
		raw, e = read(filepath.Join(s.dir, "release-"+test.ID+".json"), expectation.MaxBytes)
		if e != nil {
			return bad
		}
		release, e := expectation.Decode(raw)
		if e != nil || release.Identity() != pin {
			return bad
		}
		for _, table := range s.document.Tables {
			if table.ID != test.Table {
				continue
			}
			for _, row := range table.Rows {
				raw, e = read(filepath.Join(s.dir, test.ID+"-"+row.ID+".json"), testrunner.MaxSpecBytes)
				if e != nil {
					return bad
				}
				compiled, e := testrunner.DecodeSpec(raw)
				if e != nil {
					return bad
				}
				approved := release.Baseline.Spec
				approved.Input.Case = compiled.Input.Case
				approved.Target = compiled.Target
				approved.Observation.Path = compiled.Observation.Path
				if !sameCoverageJSON(approved, compiled) {
					return bad
				}
			}
		}
	}
	return nil
}
