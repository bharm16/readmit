package suite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

const CoverageSchema = "readmit-suite-coverage/v1"

// CoverageDocument declares a finite denominator over the exact retained suite.
// Exclusions affect assessment only: they never filter or authorize execution.
type CoverageDocument struct {
	Schema         string                  `json:"schema"`
	SuiteSHA256    string                  `json:"suite_sha256"`
	Specifications []CoverageSpecification `json:"specifications"`
	Requirements   []Requirement           `json:"requirements"`
	Exclusions     []Exclusion             `json:"exclusions"`
}
type CoverageSpecification struct {
	Job    string `json:"job"`
	SHA256 string `json:"sha256"`
}

func (v *CoverageSpecification) UnmarshalJSON(raw []byte) error {
	type plain CoverageSpecification
	return required(raw, (*plain)(v), "job", "sha256")
}

type Requirement struct {
	ID   string   `json:"id"`
	Jobs []string `json:"jobs"`
}
type Exclusion struct {
	Job     string `json:"job"`
	State   string `json:"state"`
	Reason  string `json:"reason"`
	Expires string `json:"expires"`
}

func (v *CoverageDocument) UnmarshalJSON(raw []byte) error {
	type plain CoverageDocument
	return required(raw, (*plain)(v), "schema", "suite_sha256", "specifications", "requirements", "exclusions")
}
func (v *Requirement) UnmarshalJSON(raw []byte) error {
	type plain Requirement
	return required(raw, (*plain)(v), "id", "jobs")
}
func (v *Exclusion) UnmarshalJSON(raw []byte) error {
	type plain Exclusion
	return required(raw, (*plain)(v), "job", "state", "reason", "expires")
}
func DecodeCoverage(raw []byte) (CoverageDocument, error) {
	var d CoverageDocument
	invalid := errors.New("invalid suite coverage declarations")
	if len(raw) > MaxBytes || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != CoverageSchema || len(d.Requirements) < 1 || len(d.Requirements) > 256 || len(d.Exclusions) > 64 {
		return d, invalid
	}
	digest, err := hex.DecodeString(d.SuiteSHA256)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != d.SuiteSHA256 {
		return d, invalid
	}
	seen := map[string]bool{}
	if len(d.Specifications) < 1 || len(d.Specifications) > 64 {
		return d, invalid
	}
	for _, pin := range d.Specifications {
		digest, err := hex.DecodeString(pin.SHA256)
		if !identifier.MatchString(pin.Job) || seen[pin.Job] || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != pin.SHA256 {
			return d, invalid
		}
		seen[pin.Job] = true
	}
	seen = map[string]bool{}
	for _, r := range d.Requirements {
		if !identifier.MatchString(r.ID) || seen[r.ID] || len(r.Jobs) > 64 || !unique(r.Jobs, identifier.MatchString) {
			return d, invalid
		}
		seen[r.ID] = true
	}
	seen = map[string]bool{}
	for _, e := range d.Exclusions {
		if !identifier.MatchString(e.Job) || seen[e.Job] || !text(e.Reason, 1024) {
			return d, invalid
		}
		seen[e.Job] = true
		switch e.State {
		case "skipped", "unsupported", "quarantined", "disabled":
		default:
			return d, invalid
		}
		expiry, err := time.Parse(time.RFC3339, e.Expires)
		if err != nil || expiry.IsZero() || expiry.UTC().Format(time.RFC3339) != e.Expires {
			return d, invalid
		}
	}
	return d, nil
}

// CoverageReport is a read-only view, not retained execution evidence. Every
// expanded job remains visible, including jobs no requirement names.
type CoverageReport struct {
	Suite        string                `json:"suite"`
	Environment  string                `json:"environment"`
	At           time.Time             `json:"at"`
	Denominator  int                   `json:"denominator"`
	Passed       int                   `json:"passed"`
	Percent      float64               `json:"percent"`
	Requirements []RequirementCoverage `json:"requirements"`
	Jobs         []JobCoverage         `json:"jobs"`
	Scope        string                `json:"scope"`
}
type RequirementCoverage struct {
	ID    string   `json:"id"`
	Jobs  []string `json:"jobs"`
	State string   `json:"state"`
}
type JobCoverage struct {
	ID              string               `json:"id"`
	Execution       string               `json:"execution"`
	Reason          string               `json:"reason"`
	Expiry          string               `json:"expiry"`
	Exclusion       string               `json:"exclusion"`
	ExclusionReason string               `json:"exclusion_reason"`
	Expires         string               `json:"expires"`
	Expired         bool                 `json:"expired"`
	Eligible        bool                 `json:"eligible"`
	Stability       runcompare.Stability `json:"stability"`
}

type coverageSuite struct {
	dir        string
	document   Document
	selection  Selection
	queue      runqueue.Plan
	admissions map[string]runqueue.JobReport
}

func sameCoverageJSON(a, b any) bool {
	x, _ := json.Marshal(a, json.Deterministic(true))
	y, _ := json.Marshal(b, json.Deterministic(true))
	return bytes.Equal(x, y)
}
func loadCoverageSuite(path, digest string) (coverageSuite, error) {
	var s coverageSuite
	bad := errors.New("coverage requires matching retained suite, selection and queue")
	dir, err := artifactpath.Directory(path)
	if err != nil {
		return s, bad
	}
	s.dir = dir
	raw, err := read(filepath.Join(dir, "suite.json"), MaxBytes)
	if err != nil {
		return s, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != digest {
		return s, bad
	}
	s.document, err = Decode(raw)
	if err != nil {
		return s, err
	}
	raw, err = read(filepath.Join(dir, "selection.json"), 4096)
	if err != nil {
		return s, err
	}
	s.selection, err = DecodeSelection(raw)
	if err != nil {
		return s, err
	}
	found := false
	for _, e := range s.document.Environments {
		if s.selection.Suite == s.document.ID && s.selection.Environment == e.ID && s.selection.Site == e.Site {
			found = true
		}
	}
	if !found {
		return s, bad
	}
	raw, err = read(filepath.Join(dir, "queue.json"), runqueue.MaxPlanBytes)
	if err != nil {
		return s, err
	}
	s.queue, err = runqueue.DecodePlan(raw)
	if err != nil {
		return s, err
	}
	expected, err := s.document.queue()
	if err != nil || !sameCoverageJSON(expected, s.queue) {
		return s, bad
	}
	if _, err = artifactpath.Directory(filepath.Join(dir, "runs")); err != nil {
		return s, bad
	}
	s.admissions = map[string]runqueue.JobReport{}
	reportPath := filepath.Join(dir, "report.json")
	if _, err = os.Lstat(reportPath); os.IsNotExist(err) {
		return s, nil
	}
	raw, err = read(reportPath, MaxBytes)
	if err != nil {
		return s, err
	}
	var report runqueue.Report
	if required(raw, &report, "schema", "parallelism", "jobs", "executed", "start_failed", "refused", "skipped") != nil || report.Schema != runqueue.ReportSchema || report.Parallelism != s.queue.Parallelism || len(report.Jobs) != len(s.queue.Jobs) {
		return s, bad
	}
	var executed, startFailed, refused, skipped int
	for i, j := range report.Jobs {
		if j.ID != s.queue.Jobs[i].ID || j.Isolation != s.queue.Jobs[i].Isolation {
			return s, bad
		}
		switch j.Admission {
		case runqueue.Executed:
			executed++
		case runqueue.StartFailed:
			startFailed++
		case runqueue.Refused:
			refused++
		case runqueue.Skipped:
			skipped++
		default:
			return s, bad
		}
		if j.Admission != runqueue.Executed && (!text(j.Reason, 4096) || j.Run != nil) {
			return s, bad
		}
		s.admissions[j.ID] = j
	}
	if report.Executed != executed || report.StartFailed != startFailed || report.Refused != refused || report.Skipped != skipped {
		return s, bad
	}
	return s, nil
}

// AssessCoverage reads retained suites only, including after a cancelled or
// interrupted execution. Missing evidence is unknown; corrupt evidence refuses.
// now makes exclusion expiry explicit and reproducible without changing a file.
func AssessCoverage(ctx context.Context, directory, policy string, repeats []string, now time.Time) (CoverageReport, error) {
	var result CoverageReport
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if now.IsZero() || len(repeats) > 15 {
		return result, errors.New("coverage needs an assessment instant and at most fifteen previous suites")
	}
	raw, err := read(policy, MaxBytes)
	if err != nil {
		return result, err
	}
	d, err := DecodeCoverage(raw)
	if err != nil {
		return result, err
	}
	current, err := loadCoverageSuite(directory, d.SuiteSHA256)
	if err != nil {
		return result, err
	}
	histories := []coverageSuite{}
	seen := map[string]bool{current.dir: true}
	for _, path := range repeats {
		prior, err := loadCoverageSuite(path, d.SuiteSHA256)
		if err != nil {
			return result, err
		}
		if seen[prior.dir] || prior.selection != current.selection {
			return result, errors.New("coverage history needs distinct suites from the same declared environment")
		}
		seen[prior.dir] = true
		histories = append(histories, prior)
	}
	pins := map[string]string{}
	for _, pin := range d.Specifications {
		pins[pin.Job] = pin.SHA256
	}
	if len(pins) != len(current.queue.Jobs) {
		return result, errors.New("coverage must pin every prepared job specification")
	}
	for _, s := range append([]coverageSuite{current}, histories...) {
		for _, job := range s.queue.Jobs {
			if err = verifyCoverageSpec(s, job, pins[job.ID]); err != nil {
				return result, err
			}
		}
	}
	jobs := map[string]bool{}
	for _, j := range current.queue.Jobs {
		jobs[j.ID] = true
	}
	for _, r := range d.Requirements {
		for _, id := range r.Jobs {
			if !jobs[id] {
				return result, errors.New("coverage requirement names an undeclared suite job")
			}
		}
	}
	exclusions := map[string]Exclusion{}
	for _, e := range d.Exclusions {
		if !jobs[e.Job] {
			return result, errors.New("coverage exclusion names an undeclared suite job")
		}
		exclusions[e.Job] = e
	}
	result = CoverageReport{Suite: current.document.ID, Environment: current.selection.Environment, At: now.UTC(), Denominator: len(d.Requirements), Requirements: []RequirementCoverage{}, Jobs: []JobCoverage{}, Scope: "Only the explicitly declared requirements form the denominator; this is not universal HL7 assurance. Exclusions are assessment declarations, not scheduling controls or execution states. Expiry never removes an exclusion or authorizes a send. Retained failures remain visible. Metadata can be sensitive; this view is customer-local, not disclosure approval."}
	eligible := map[string]bool{}
	for _, j := range current.queue.Jobs {
		if err = ctx.Err(); err != nil {
			return CoverageReport{}, err
		}
		row, path, err := coverageJob(ctx, current, j)
		if err != nil {
			return CoverageReport{}, err
		}
		historyPaths := []string{}
		incomplete := false
		for _, h := range histories {
			_, p, e := coverageJob(ctx, h, j)
			if e != nil {
				return CoverageReport{}, e
			}
			if p == "" {
				incomplete = true
			} else {
				historyPaths = append(historyPaths, p)
			}
		}
		if path != "" && len(historyPaths) > 0 {
			comparison, e := runcompare.Compare(ctx, runcompare.Input{Baseline: historyPaths[0], Current: path, Repeats: historyPaths[1:]})
			if e != nil {
				return CoverageReport{}, e
			}
			row.Stability = comparison.Stability
		}
		if incomplete {
			row.Stability.State = "unresolved"
			row.Stability.Reason = "A selected prior suite has no finalized execution for this job."
			row.Eligible = false
		}
		if row.Stability.State == "possible_flakiness" || row.Stability.State == "unresolved" {
			row.Eligible = false
		}
		if e, ok := exclusions[j.ID]; ok {
			row.Exclusion = e.State
			row.ExclusionReason = e.Reason
			row.Expires = e.Expires
			expiry, _ := time.Parse(time.RFC3339, e.Expires)
			row.Expired = !now.Before(expiry)
			row.Eligible = false
		}
		eligible[j.ID] = row.Eligible
		result.Jobs = append(result.Jobs, row)
	}
	for _, r := range d.Requirements {
		row := RequirementCoverage{ID: r.ID, Jobs: r.Jobs, State: "passed"}
		if len(r.Jobs) == 0 {
			row.State = "uncovered"
		} else {
			for _, j := range r.Jobs {
				if !eligible[j] {
					row.State = "not_passed"
				}
			}
		}
		if row.State == "passed" {
			result.Passed++
		}
		result.Requirements = append(result.Requirements, row)
	}
	result.Percent = 100 * float64(result.Passed) / float64(result.Denominator)
	if err = ctx.Err(); err != nil {
		return CoverageReport{}, err
	}
	return result, nil
}

func coverageJob(ctx context.Context, s coverageSuite, j runqueue.Job) (JobCoverage, string, error) {
	row := JobCoverage{ID: j.ID, Execution: "unknown", Reason: "No durable execution exists; completion is unknown.", Expiry: "not_applicable", Exclusion: "none", Stability: runcompare.Stability{State: "insufficient_history", Reason: "No repeated comparable executions selected.", FlakyAssertions: []string{}}}
	path := filepath.Join(s.dir, "runs", j.ID)
	admission, hasReport := s.admissions[j.ID]
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		if hasReport {
			if admission.Admission == runqueue.Executed {
				return row, "", errors.New("suite report claims an execution whose evidence is absent")
			}
			row.Execution = string(admission.Admission)
			row.Reason = admission.Reason
		}
		return row, "", nil
	}
	if hasReport && admission.Admission != runqueue.Executed {
		return row, "", errors.New("suite report excludes an existing execution")
	}
	retained, err := runresult.Open(path)
	if err != nil {
		return row, "", err
	}
	summary := retained.Lifecycle
	row.Execution = string(summary.State)
	row.Reason = "Retained durable execution; see run status for delivery and recovery details."
	if summary.ResultIdentity == "" {
		return row, "", nil
	}
	a := retained.Artifact
	specRaw, err := read(filepath.Join(s.dir, j.Spec), testrunner.MaxSpecBytes)
	if err != nil {
		return row, "", err
	}
	spec, err := testrunner.DecodeSpec(specRaw)
	if err != nil {
		return row, "", err
	}
	if a.Identity != summary.ResultIdentity || retained.Spec == nil || !sameCoverageJSON(*retained.Spec, spec) {
		return row, "", errors.New("retained execution does not match the prepared suite job")
	}
	usable, _ := retained.Usable()
	comparison, err := runcompare.Compare(ctx, runcompare.Input{Baseline: path, Current: path})
	if err != nil {
		return row, "", err
	}
	row.Stability = comparison.Stability
	row.Eligible = usable && summary.State == durablerun.Passed && comparison.Current.RunState == string(durablerun.Passed) && comparison.Current.Identity == a.Identity && comparison.Current.Status == string(testrunner.Pass)
	return row, path, nil
}

// The suite's old template references carry no digest. The new coverage author's
// explicit spec pins bind the actual assessed assertions without reopening those
// mutable templates. Recorded row/binding strings are checked offline as written.
func verifyCoverageSpec(s coverageSuite, j runqueue.Job, pin string) error {
	raw, err := read(filepath.Join(s.dir, j.Spec), testrunner.MaxSpecBytes)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != pin {
		return errors.New("prepared specification differs from its coverage pin")
	}
	spec, err := testrunner.DecodeSpec(raw)
	if err != nil {
		return err
	}
	bad := errors.New("prepared specification differs from declared suite row or environment")
	for _, test := range s.document.Tests {
		for _, table := range s.document.Tables {
			if table.ID != test.Table {
				continue
			}
			for _, row := range table.Rows {
				if test.ID+"-"+row.ID != j.ID {
					continue
				}
				suffix := string(os.PathSeparator) + row.Case
				if !strings.HasSuffix(spec.Input.Case, suffix) || !slices.Equal(spec.Input.Messages, test.Sequence) {
					return bad
				}
				root := strings.TrimSuffix(spec.Input.Case, suffix)
				if !filepath.IsAbs(root) || filepath.Clean(root) != root {
					return bad
				}
				used := 0
				for _, a := range spec.Assertions {
					if expected, ok := row.Expected[a.ID]; ok {
						if !sameCoverageJSON(a.Expected, expected) {
							return bad
						}
						used++
					}
				}
				if used != len(row.Expected) {
					return bad
				}
				for _, env := range s.document.Environments {
					if env.ID != s.selection.Environment {
						continue
					}
					for _, binding := range env.Bindings {
						if binding.Parameter != test.Parameter {
							continue
						}
						if spec.Target != artifactpath.JoinReference(root, binding.Target) {
							return bad
						}
						if spec.Observation.Boundary == testrunner.LedgerBoundary {
							if binding.Observation == "" || spec.Observation.Path != artifactpath.JoinReference(root, binding.Observation) {
								return bad
							}
						} else if binding.Observation != "" {
							return bad
						}
						return nil
					}
				}
			}
		}
	}
	return bad
}
