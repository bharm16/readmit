package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

const RetainedSchema = "readmit-retained-packet/v1"

// RetainedInput selects historical artifacts; no path is executed. BaselineCase
// defaults to Case. Spec must be the exact specification retained by Current.
type RetainedInput struct{ Case, Spec, Current, Baseline, BaselineCase string }
type RetainedRun struct {
	RunState          string `json:"run_state"`
	JournalIncomplete bool   `json:"journal_incomplete"`
	DeliveryUncertain bool   `json:"delivery_uncertain"`
	Identity          string `json:"identity"`
	CaseIdentity      string `json:"case_identity"`
	CaseProvenance    string `json:"case_provenance"`
	SpecIdentity      string `json:"spec_identity"`
	TargetIdentity    string `json:"target_identity"`
	Boundary          string `json:"boundary"`
	Status            string `json:"status"`
	ErrorClass        string `json:"error_class"`
}
type RetainedManifest struct {
	Schema               string           `json:"schema"`
	State                string           `json:"state"`
	ExportPolicy         string           `json:"export_policy"`
	ContainsSourceValues bool             `json:"contains_source_values"`
	Current              RetainedRun      `json:"current"`
	Baseline             *RetainedRun     `json:"baseline"`
	Files                []bundle.Payload `json:"files"`
}
type RetainedPacket struct {
	Manifest RetainedManifest
	Identity string
}

// Assemble copies bounded snapshots into a new private directory and verifies
// those copies before sealing. Failure retains an incomplete directory; retry
// always requires a new destination. It never manufactures missing evidence.
func Assemble(ctx context.Context, in RetainedInput, output string) (*RetainedPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.Case == "" || in.Spec == "" || in.Current == "" || (in.Baseline == "" && in.BaselineCase != "") {
		return nil, errors.New("select case, exact retained spec and current result; baseline case requires baseline result")
	}
	files := map[string][]byte{}
	total := 0
	inputs := map[string]string{"case": in.Case, "current": in.Current}
	if in.Baseline != "" {
		inputs["baseline"] = in.Baseline
		inputs["baseline-case"] = in.BaselineCase
		if in.BaselineCase == "" {
			inputs["baseline-case"] = in.Case
		}
	}
	for prefix, source := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tree, err := readTreeAllowEmptySent(source, prefix == "current" || prefix == "baseline")
		if err != nil {
			return nil, err
		}
		for name, data := range tree {
			name = prefix + "/" + name
			total += len(data)
			if len(name) > 200 || strings.Count(name, "/") > 5 || len(files) >= maxFiles-5 || total > maxPacketBytes-(2<<20) {
				return nil, errors.New("retained packet exceeds path, file or byte limits")
			}
			files[name] = data
		}
	}
	raw, err := retainedSpec(in.Spec)
	if err != nil {
		return nil, err
	}
	files["spec.json"] = raw
	total += len(raw)
	if len(files) > maxFiles-4 || total > maxPacketBytes-(1<<20) {
		return nil, errors.New("retained packet exceeds file or byte limits")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent, dir, err := reserve(output)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err := copyFiles(files, "", dir); err != nil {
		return nil, err
	}
	manifest, summary, err := inspectRetained(ctx, dir, files)
	if err != nil {
		return nil, err
	}
	files["SUMMARY.md"] = summary
	files["RERUN.md"] = retainedInstructions()
	for _, name := range []string{"SUMMARY.md", "RERUN.md"} {
		if err := writeFile(dir, name, files[name]); err != nil {
			return nil, err
		}
	}
	manifest.Files = index(files)
	raw, err = encode(manifest)
	if err != nil {
		return nil, err
	}
	if err := writeFile(dir, "manifest.json", raw); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := writeFile(dir, "identity.sha256", []byte(digest(raw)+"\n")); err != nil {
		return nil, err
	}
	if err := syncEntries(parent, dir, files); err != nil {
		return nil, err
	}
	return OpenRetained(ctx, dir)
}

func retainedSpec(path string) ([]byte, error) {
	invalid := errors.New("specification must be a bounded regular file")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > testrunner.MaxSpecBytes {
		return nil, invalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, invalid
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, invalid
	}
	raw, err := io.ReadAll(io.LimitReader(f, testrunner.MaxSpecBytes+1))
	if err != nil || len(raw) > testrunner.MaxSpecBytes {
		return nil, invalid
	}
	if _, err := testrunner.DecodeSpec(raw); err != nil {
		return nil, errors.New("unsupported retained specification")
	}
	return raw, nil
}

// OpenRetained verifies both hashes and evidence-derived claims offline. It does
// not follow any historical case, observation, target or credential reference.
func OpenRetained(ctx context.Context, dir string) (*RetainedPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}
	files, err := readTree(dir)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid, incomplete, changed or unsupported retained packet")
	raw := files["manifest.json"]
	if len(raw) > 1<<20 || string(files["identity.sha256"]) != digest(raw)+"\n" {
		return nil, invalid
	}
	var stored RetainedManifest
	if json.Unmarshal(raw, &stored, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	expected, summary, err := inspectRetained(ctx, dir, files)
	if err != nil {
		return nil, err
	}
	expected.Files = index(files)
	canonical, err := encode(expected)
	if err != nil || !bytes.Equal(raw, canonical) || !bytes.Equal(summary, files["SUMMARY.md"]) || !bytes.Equal(retainedInstructions(), files["RERUN.md"]) {
		return nil, invalid
	}
	return &RetainedPacket{Manifest: expected, Identity: digest(raw)}, nil
}

func inspectRetained(ctx context.Context, dir string, files map[string][]byte) (RetainedManifest, []byte, error) {
	m := RetainedManifest{Schema: RetainedSchema, State: "complete", ExportPolicy: "customer-local-only", ContainsSourceValues: true}
	invalid := errors.New("retained case, specification and execution identity or provenance mismatch")
	current, err := inspectRetainedRun(dir, "case", "current")
	if err != nil {
		return m, nil, err
	}
	m.Current = current
	if digest(files["spec.json"]) != current.SpecIdentity || !bytes.Equal(files["spec.json"], files[retainedResultPrefix(files, "current")+"/spec.json"]) {
		return m, nil, invalid
	}
	hasBaseline := files["baseline/identity.sha256"] != nil || files["baseline/engine.json"] != nil
	allowed := []string{"case/", "current/"}
	if hasBaseline {
		baseline, err := inspectRetainedRun(dir, "baseline-case", "baseline")
		if err != nil {
			return m, nil, err
		}
		m.Baseline = &baseline
		if baseline.Identity == current.Identity {
			return m, nil, errors.New("baseline and current must be distinct retained executions")
		}
		allowed = append(allowed, "baseline/", "baseline-case/")
	}
	for name := range files {
		if name == "manifest.json" || name == "identity.sha256" || name == "SUMMARY.md" || name == "RERUN.md" || name == "spec.json" {
			continue
		}
		ok := false
		for _, prefix := range allowed {
			ok = ok || strings.HasPrefix(name, prefix)
		}
		if !ok {
			return m, nil, invalid
		}
	}
	var summary strings.Builder
	summary.WriteString("# Retained investigation packet\n\nCustomer-local only: contains original source values and historical paths/configuration. No disclosure approval or de-identification is implied.\n\n")
	fmt.Fprintf(&summary, "Current: %s\n\nObservation boundary: %s\n\nCase provenance: %s\n\n", current.Status, current.Boundary, current.CaseProvenance)
	fmt.Fprintf(&summary, "Run lifecycle: %s; incomplete journal: %t; delivery uncertain: %t\n\n", current.RunState, current.JournalIncomplete, current.DeliveryUncertain)
	if hasBaseline {
		comparison, err := runcompare.Compare(ctx, runcompare.Input{Baseline: filepath.Join(dir, "baseline"), Current: filepath.Join(dir, "current")})
		if err != nil {
			return m, nil, err
		}
		fmt.Fprintf(&summary, "Baseline: %s\n\nInput identities equal: %t\n\nTarget configuration identities equal: %t\n\nSpecification: %s\n\n", m.Baseline.Status, m.Baseline.CaseIdentity == current.CaseIdentity, m.Baseline.TargetIdentity == current.TargetIdentity, comparison.Specification)
		fmt.Fprintf(&summary, "Baseline lifecycle: %s; incomplete journal: %t; delivery uncertain: %t\n\n", m.Baseline.RunState, m.Baseline.JournalIncomplete, m.Baseline.DeliveryUncertain)
		summary.WriteString(comparison.Scope + "\n\n")
	} else {
		summary.WriteString("No observed baseline was supplied. This single-run report proves no before/after improvement or regression.\n\n")
	}
	summary.WriteString("Statuses are reevaluated from retained evidence; execution errors are not successful regression proof. ACKs do not prove downstream clinical behavior. Target software revisions, external setup/reset and source authenticity remain unverified. Hashes establish integrity, not authenticity, approval or causality. Inspect the retained result.json (inside result/ for a durable job) and raw payloads for assertions and observations. Baseline evidence, when supplied, is retained separately.\n")
	if err := ctx.Err(); err != nil {
		return m, nil, err
	}
	return m, []byte(summary.String()), nil
}

func inspectRetainedRun(dir, caseName, runName string) (RetainedRun, error) {
	invalid := errors.New("retained execution does not bind the supplied case and selected original bytes")
	c, err := bundle.Open(filepath.Join(dir, caseName))
	if err != nil {
		return RetainedRun{}, invalid
	}
	resultPath := filepath.Join(dir, runName)
	runState := runresult.NoRunState
	retained, err := runresult.Open(resultPath)
	if err != nil {
		return RetainedRun{}, invalid
	}
	usable, _ := retained.Usable()
	if !usable || retained.Artifact == nil {
		return RetainedRun{}, invalid
	}
	lifecycle := retained.Lifecycle
	if retained.Durable {
		runState = string(lifecycle.State)
	}
	a := retained.Artifact
	if retained.Spec == nil || a.Result.InputBundleIdentity != c.Identity || retained.Run == nil {
		return RetainedRun{}, invalid
	}
	for _, event := range retained.Run.Events {
		original, err := c.Raw(event.SourceOccurrence)
		if err != nil {
			return RetainedRun{}, invalid
		}
		retainedBytes, err := retained.Run.Raw(event.Source)
		if err != nil || !bytes.Equal(original, retainedBytes) {
			return RetainedRun{}, invalid
		}
	}
	return RetainedRun{RunState: runState, JournalIncomplete: lifecycle.JournalIncomplete, DeliveryUncertain: lifecycle.DeliveryUncertain, Identity: a.Identity, CaseIdentity: c.Identity, CaseProvenance: string(c.Manifest.Provenance.Mode), SpecIdentity: a.Result.SpecIdentity, TargetIdentity: a.Result.TargetIdentity, Boundary: a.Result.ObservationBoundary, Status: string(a.Result.Status), ErrorClass: a.Result.ErrorClass}, nil
}
func retainedInstructions() []byte {
	return []byte(`# Rerun retained evidence

Verification is offline: readmit report verify-retained PACKET
No endpoint is contacted and no historical path is resolved by verification.

Keep this packet immutable. Copy case/ and the root spec.json into a separate
new workspace. If a baseline exists, its own case is baseline-case/ and its
historical specification is baseline/spec.json (baseline/result/spec.json for
a durable job). Never overwrite a prior run.

Historical target and observation paths are evidence, not runnable authority.
An operator must explicitly select an authorized nonproduction target and
credentials, establish the required setup/reset and observation boundary, and
rebind only those paths and the copied case path in a new specification. Preserve
its message selection and assertions. Use readmit test --help for execution
options. From the separate workspace, after rebinding and operator review:

    readmit test spec.json
    readmit test spec.json --send --output NEW_RESULT

The first command validates locally without sending. The second explicitly
authorizes execution. Exit 0 is pass, 1 assertion failure, and 2 execution error.
New specifications have new identities; retain both.

No packet operation performs setup, reset, recovery or a network send. A missing
baseline cannot be recreated by a built-in defective fixture. If the original
target or required observation is unavailable, retain the single-run report and
state that before/after proof is unavailable. After interruption use a new packet
destination; an incomplete packet is never verified as complete. Uncertain sends
must be reconciled at the target before any authorized rerun.
`)
}

func retainedResultPrefix(files map[string][]byte, name string) string {
	if files[name+"/engine.json"] != nil {
		return name + "/result"
	}
	return name
}
