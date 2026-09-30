package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/runcompare"
)

const ConnectedGatePolicySchema = "readmit-ci-gate-policy/v2"
const ConnectedGateSchema = "readmit-ci-gate/v2"
const ConnectedGateRetentionSchema = "readmit-ci-retention/v2"

type ConnectedGatePolicy struct {
	Schema      string                    `json:"schema"`
	Environment string                    `json:"environment"`
	Revision    string                    `json:"revision_assumption"`
	Engine      string                    `json:"engine"`
	Promotion   string                    `json:"promotion_identity"`
	Input       string                    `json:"input"`
	Baseline    string                    `json:"baseline_execution"`
	Coverage    ConnectedCoverageDocument `json:"coverage"`
	MaxBytes    int64                     `json:"max_bytes"`
	RetainUntil string                    `json:"retain_until"`
	Approver    string                    `json:"approver"`
	Rationale   string                    `json:"rationale"`
}

func (p ConnectedGatePolicy) Identity() string { return identity(p) }
func (p *ConnectedGatePolicy) UnmarshalJSON(raw []byte) error {
	type plain ConnectedGatePolicy
	return required(raw, (*plain)(p), "schema", "environment", "revision_assumption", "engine", "promotion_identity", "input", "baseline_execution", "coverage", "max_bytes", "retain_until", "approver", "rationale")
}
func DecodeConnectedGatePolicy(raw []byte) (ConnectedGatePolicy, error) {
	var p ConnectedGatePolicy
	if len(raw) > MaxBytes || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != ConnectedGatePolicySchema || !identifier.MatchString(p.Environment) || !text(p.Revision, 256) || !text(p.Engine, 256) || !validDigest(p.Promotion) || !validDigest(p.Input) || !validDigest(p.Baseline) || p.MaxBytes < 1 || p.MaxBytes > gateMaxBytes || !text(p.Approver, 256) || !text(p.Rationale, 1024) {
		return p, errors.New("invalid connected gate policy")
	}
	raw, _ = json.Marshal(p.Coverage)
	if _, err := DecodeConnectedCoverage(raw); err != nil {
		return p, err
	}
	until, err := time.Parse(time.RFC3339, p.RetainUntil)
	if err != nil || until.UTC().Format(time.RFC3339) != p.RetainUntil {
		return p, errors.New("invalid connected gate retention")
	}
	return p, nil
}

type connectedGateManifest struct {
	Schema   string     `json:"schema"`
	Policy   string     `json:"policy"`
	Current  string     `json:"current"`
	Baseline string     `json:"baseline"`
	At       time.Time  `json:"at"`
	Report   GateReport `json:"report"`
}

var connectedGateFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected gate snapshot", RequiredFiles: []string{"manifest.json", "policy.json", "coverage.json", "gate.json", "identity.sha256"}, Nested: []string{"current", "baseline"}, AllowEmpty: func(n string, _ map[string][]byte) bool {
	return strings.HasPrefix(n, "current/") || strings.HasPrefix(n, "baseline/")
}, MaxFiles: 400000, MaxFileBytes: 64 << 20, MaxBytes: gateMaxBytes, AllowFile: func(n string) bool {
	return n == "manifest.json" || n == "policy.json" || n == "coverage.json" || n == "identity.sha256" || n == "gate.json"
}}, Seal: artifactdir.DirectoryHash(ConnectedGateRetentionSchema)}

func connectedGateUnknown() GateReport { r := gateUnknown(); r.Schema = ConnectedGateSchema; return r }

func assessConnectedGate(ctx context.Context, root string, p ConnectedGatePolicy, at time.Time) GateReport {
	r := connectedGateUnknown()
	until, _ := time.Parse(time.RFC3339, p.RetainUntil)
	if !at.Before(until) {
		r.Retention = "expired"
		return r
	}
	r.Retention = "retained"
	current, e := OpenConnectedExecution(ctx, filepath.Join(root, "current"))
	if e != nil {
		return r
	}
	baseline, e := OpenConnectedExecution(ctx, filepath.Join(root, "baseline"))
	if e != nil || baseline.Identity != p.Baseline {
		return r
	}
	for _, execution := range []ConnectedExecution{current, baseline} {
		if execution.Preparation.Input != p.Input || execution.Preparation.Environment != p.Environment || execution.Preparation.Capabilities.Engine != p.Engine || execution.Preparation.Suite != p.Coverage.SuiteSHA256 {
			return r
		}
		raw, e := read(filepath.Join(root, func() string {
			if execution.Identity == current.Identity {
				return "current"
			}
			return "baseline"
		}(), "prepared", "promotion.json"), MaxBytes)
		if e != nil {
			return r
		}
		promotion, e := DecodeConnectedPromotion(raw)
		if e != nil || promotion.Identity() != p.Promotion || promotion.Review.Input != p.Input || promotion.Review.Revision != p.Revision {
			return r
		}
	}
	r.Approval = "passed"
	r.Pins = "passed"
	r.TargetRevision = "operator_asserted"
	if baseline.Report.ExitCode() != 0 {
		return r
	}
	r.Baseline = "passed"
	if current.Report.ExitCode() != 0 {
		if current.Report.ExitCode() == 1 {
			r.State = "failed"
			r.ExitCode = 1
			r.Baseline = "failed"
		}
		return r
	}
	for _, job := range current.Queue.Jobs {
		comparison, e := runcompare.CompareFlows(ctx, filepath.Join(root, "baseline", "runs", job.ID), filepath.Join(root, "current", "runs", job.ID))
		if e != nil {
			return r
		}
		for _, dimension := range comparison.Dimensions {
			if dimension.State != "unchanged" {
				return r
			}
		}
		for _, check := range comparison.Checks {
			if check.Definition != "unchanged" || check.Behavior != "unchanged" {
				r.State = "failed"
				r.ExitCode = 1
				r.Baseline = "failed"
				return r
			}
		}
	}
	for _, name := range []string{"current", "baseline"} {
		coverage, e := AssessConnectedCoverage(ctx, filepath.Join(root, name), filepath.Join(root, "coverage.json"), at)
		if e != nil {
			return r
		}
		for _, job := range coverage.Jobs {
			if !job.Eligible {
				return r
			}
		}
		if coverage.Passed != coverage.Denominator {
			return r
		}
	}
	r.Coverage = "passed"
	r.State = "passed"
	r.ExitCode = 0
	return r
}

func RetainConnectedGate(ctx context.Context, current, baseline, policy, pin, output string, at time.Time) GateReport {
	r := connectedGateUnknown()
	raw, e := read(policy, MaxBytes)
	if e != nil {
		return r
	}
	p, e := DecodeConnectedGatePolicy(raw)
	if e != nil || p.Identity() != pin {
		return r
	}
	current, e = artifactpath.Directory(current)
	if e != nil {
		return r
	}
	baseline, e = artifactpath.Directory(baseline)
	if e != nil || baseline == current {
		return r
	}
	out, e := artifactpath.Destination(output)
	if e != nil {
		return r
	}
	for _, source := range []string{current, baseline} {
		rel, e := filepath.Rel(source, out)
		if e != nil || rel == "." || filepath.IsLocal(rel) {
			return r
		}
	}
	w, e := artifactdir.Create(out, connectedGateFamily, artifactdir.Durable)
	if e != nil {
		return r
	}
	defer w.Close()
	var copied int64
	identities := []string{}
	for i, path := range []string{current, baseline} {
		execution, e := OpenConnectedExecution(ctx, path)
		if e != nil {
			return r
		}
		identities = append(identities, execution.Identity)
		files, e := artifactdir.Read(path, connectedExecutionFamily.Layout)
		if e != nil {
			return r
		}
		prefix := []string{"current", "baseline"}[i]
		for name, bytes := range files {
			copied += int64(len(bytes))
			if copied > p.MaxBytes || ctx.Err() != nil || w.WriteFile(prefix+"/"+name, bytes) != nil {
				return r
			}
		}
	}
	coverage, _ := json.Marshal(p.Coverage, json.Deterministic(true))
	copied += int64(len(raw) + len(coverage))
	if copied > p.MaxBytes || w.WriteFile("policy.json", raw) != nil || w.WriteFile("coverage.json", coverage) != nil {
		return r
	}
	r = assessConnectedGate(ctx, w.Path(), p, at)
	reportBytes, _ := json.Marshal(r, json.Deterministic(true))
	copied += int64(len(reportBytes))
	if copied > p.MaxBytes || w.WriteFile("gate.json", reportBytes) != nil {
		return connectedGateUnknown()
	}
	manifest, _ := json.Marshal(connectedGateManifest{Schema: ConnectedGateRetentionSchema, Policy: p.Identity(), Current: identities[0], Baseline: identities[1], At: at.UTC(), Report: r}, json.Deterministic(true))
	if copied+int64(len(manifest)+65) > p.MaxBytes || w.WriteFile("manifest.json", manifest) != nil {
		return connectedGateUnknown()
	}
	if _, e = w.Seal(nil); e != nil {
		return connectedGateUnknown()
	}
	return r
}

func VerifyConnectedGate(ctx context.Context, directory, pin string, now time.Time) GateReport {
	r := connectedGateUnknown()
	files, e := artifactdir.Read(directory, connectedGateFamily.Layout)
	if e != nil || strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ConnectedGateRetentionSchema, files) {
		return r
	}
	p, e := DecodeConnectedGatePolicy(files["policy.json"])
	if e != nil || p.Identity() != pin {
		return r
	}
	var manifest connectedGateManifest
	if json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != ConnectedGateRetentionSchema || manifest.Policy != pin {
		return r
	}
	then := assessConnectedGate(ctx, directory, p, manifest.At)
	gate, e := DecodeConnectedGateReport(files["gate.json"])
	if e != nil || identity(gate) != identity(manifest.Report) || identity(then) != identity(manifest.Report) {
		return r
	}
	return assessConnectedGate(ctx, directory, p, now)
}

// InspectConnectedGateSummary reads the sealed historical summary. Reading it
// approves no policy: VerifyConnectedGate still requires the independently
// pinned policy identity and re-evaluates the complete retained proof.
func InspectConnectedGateSummary(directory string) (GateReport, error) {
	fail := errors.New("connected gate summary provenance cannot be verified")
	files, err := artifactdir.Read(directory, connectedGateFamily.Layout)
	if err != nil || strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ConnectedGateRetentionSchema, files) {
		return GateReport{}, fail
	}
	var manifest connectedGateManifest
	if json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != ConnectedGateRetentionSchema || !validDigest(manifest.Policy) || !validDigest(manifest.Current) || !validDigest(manifest.Baseline) || manifest.At.IsZero() {
		return GateReport{}, fail
	}
	report, err := DecodeConnectedGateReport(files["gate.json"])
	if err != nil || identity(report) != identity(manifest.Report) {
		return GateReport{}, fail
	}
	return report, nil
}

// DecodeConnectedGateReport preserves the fixed aggregate vocabulary in its
// explicit connected version, while the historical v1 reader still refuses it.
func DecodeConnectedGateReport(raw []byte) (GateReport, error) {
	var report GateReport
	if len(raw) > 4096 || json.Unmarshal(raw, &report, json.RejectUnknownMembers(true)) != nil || report.Schema != ConnectedGateSchema {
		return GateReport{}, errors.New("invalid connected gate summary")
	}
	legacy := report
	legacy.Schema = GateReportSchema
	encoded, e := json.Marshal(legacy)
	if e != nil {
		return GateReport{}, e
	}
	if _, e = DecodeGateReport(encoded); e != nil {
		return GateReport{}, e
	}
	return report, nil
}
