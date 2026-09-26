package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/runcompare"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

const RetainedSchema = "readmit-retained-packet/v1"

// The packet's own sensitivity statements, stated once and reported by both
// the preview and the sealed manifest.
const (
	retainedExportPolicy         = "customer-local-only"
	retainedContainsSourceValues = true
)

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
// The sections it copies and the checks its sealed manifest must satisfy are
// the one layout and the one set of checks PreviewRetained reports, so a
// preview cannot describe a packet assembly would not seal.
func Assemble(ctx context.Context, in RetainedInput, output string) (*RetainedPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files, err := admitRetained(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	packet, err := artifactdir.Create(output, retainedFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer packet.Close()
	dir := packet.Path()
	if err := copyFiles(packet, files, "", ""); err != nil {
		return nil, err
	}
	manifest, summary, err := inspectRetained(ctx, dir, files)
	if err != nil {
		return nil, err
	}
	files["SUMMARY.md"] = summary
	files["RERUN.md"] = retainedInstructions()
	for _, name := range []string{"SUMMARY.md", "RERUN.md"} {
		if err := packet.WriteFile(name, files[name]); err != nil {
			return nil, err
		}
	}
	manifest.Files = index(files)
	raw, err := encode(manifest)
	if err != nil {
		return nil, err
	}
	if err := packet.WriteFile("manifest.json", raw); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := packet.Seal(nil); err != nil {
		return nil, err
	}
	return OpenRetained(ctx, dir)
}

// admitRetained is the one admission check a retained-packet request passes,
// made alike by the preview and the assembly: the files of every section read
// under the packet's aggregate path, file and byte bounds, the exact retained
// specification, and each selected execution bound to the original payloads of
// its own case. It returns the files assembly copies and writes nothing, so a
// preview that admits a request describes a packet assembly will copy, and
// one it refuses names the sentence assembly refuses with.
func admitRetained(ctx context.Context, in RetainedInput) (map[string][]byte, error) {
	sections, err := retainedSections(in)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	total := 0
	for prefix, source := range sections {
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
	for _, run := range [][2]string{{"case", "current"}, {"baseline-case", "baseline"}} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, selected := sections[run[1]]; !selected {
			continue
		}
		if _, err := inspectRetainedRun(sections[run[0]], sections[run[1]]); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// retainedSections is the one packet layout the preview and the assembly
// share: the section each selected input is copied under. A baseline's own
// case defaults to the current case. It is also the one admission check both
// make of a request, in the one sentence.
func retainedSections(in RetainedInput) (map[string]string, error) {
	if in.Case == "" || in.Spec == "" || in.Current == "" || (in.Baseline == "" && in.BaselineCase != "") {
		return nil, errors.New("select case, exact retained spec and current result; baseline case requires baseline result")
	}
	sections := map[string]string{"case": in.Case, "current": in.Current}
	if in.Baseline != "" {
		sections["baseline"] = in.Baseline
		sections["baseline-case"] = in.BaselineCase
		if in.BaselineCase == "" {
			sections["baseline-case"] = in.Case
		}
	}
	return sections, nil
}

// RetainedInputView is one selected input as the preview verified it. Found is
// what the shared reader could open; Match members compare the input against
// what the current execution retained, so a historical specification that is
// not the exact one the run kept is visible before assembly instead of
// silently replaced. Problems are fixed sentences; none of them is ever
// resolved by substituting different evidence.
type RetainedInputView struct {
	Found             bool
	Identity          string
	Provenance        string
	Status            string
	ErrorClass        string
	Boundary          string
	RunState          string
	Durable           bool
	JournalIncomplete bool
	DeliveryUncertain bool
	ResultIdentity    string
	SpecIdentity      string
	CaseIdentity      string
	TargetIdentity    string
	SpecMatch         bool
	CaseMatch         bool
	Problems          []string
}

// RetainedPreview is what one assembly would copy, read from the inputs
// themselves: each input as the shared readers verified it, the problems
// assembly refuses on, the sections the packet will hold, and what a supplied
// baseline does and does not establish. It writes nothing and contacts
// nothing. A preview with no problems is exactly the input assembly accepts;
// every problem names an input assembly would refuse.
type RetainedPreview struct {
	Case                 *RetainedInputView
	Spec                 *RetainedInputView
	Current              *RetainedInputView
	Baseline             *RetainedInputView
	BaselineCase         *RetainedInputView
	BaselineSupplied     bool
	ExportPolicy         string
	ContainsSourceValues bool
	Problems             []string
	Limitations          []string
	Inventory            []string
}

// PreviewRetained verifies the exact inputs one retained-packet assembly would
// copy, through the same readers assembly verifies them with — the case
// bundle, the bounded specification and its decoder, the retained execution —
// and reports what it found without writing anything. The sections it names
// are the ones Assemble copies and the checks it reports are the ones
// Assemble's sealed manifest must satisfy; inputs that verify one by one are
// then admitted through Assemble's own admission check, so the packet's
// aggregate bounds and the original-payload binding refuse here exactly what
// they refuse there.
func PreviewRetained(ctx context.Context, in RetainedInput) (*RetainedPreview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sections, err := retainedSections(in)
	if err != nil {
		return nil, err
	}
	p := &RetainedPreview{
		BaselineSupplied:     in.Baseline != "",
		ExportPolicy:         retainedExportPolicy,
		ContainsSourceValues: retainedContainsSourceValues,
		Problems:             []string{},
		Limitations:          []string{},
		Inventory:            []string{},
	}
	p.Case = previewCase(sections["case"])
	current, currentView := previewRun(sections["current"])
	p.Current = currentView
	if current != nil && current.Artifact != nil && p.Case.Found {
		p.Case.CaseMatch = p.Case.Identity == current.Artifact.Result.InputBundleIdentity
		if !p.Case.CaseMatch {
			p.Case.Problems = append(p.Case.Problems, "the case is not the case the current result retained")
		}
	}
	p.Spec = previewSpec(in.Spec, current)
	p.Inventory = retainedPacketInventory(p, current)
	if in.Baseline != "" {
		baseline, baselineView := previewRun(sections["baseline"])
		p.Baseline = baselineView
		p.BaselineCase = previewCase(sections["baseline-case"])
		if baseline != nil && current != nil {
			if baseline.Artifact != nil && current.Artifact != nil && baseline.Artifact.Identity == current.Artifact.Identity {
				p.Problems = append(p.Problems, "the baseline and the current result are the same retained execution; a baseline must be a distinct retained execution")
			}
			if baseline.Artifact != nil && p.BaselineCase.Found {
				p.BaselineCase.CaseMatch = p.BaselineCase.Identity == baseline.Artifact.Result.InputBundleIdentity
				if !p.BaselineCase.CaseMatch {
					p.BaselineCase.Problems = append(p.BaselineCase.Problems, "the baseline case is not the case the baseline execution retained")
				}
			}
		}
		p.Inventory = append(p.Inventory, retainedBaselineInventory(p, baseline)...)
		p.Limitations = append(p.Limitations, retainedBaselineBoundaries(baseline, current)...)
	} else {
		p.Limitations = append(p.Limitations,
			"No observed baseline was supplied. This single-run report proves no before/after improvement or regression.")
	}
	p.Problems = append(p.Problems, inputProblems(p.Case, p.Spec, p.Current)...)
	if p.Baseline != nil {
		p.Problems = append(p.Problems, inputProblems(p.Baseline)...)
	}
	if p.BaselineCase != nil {
		p.Problems = append(p.Problems, inputProblems(p.BaselineCase)...)
	}
	// Inputs that verify one by one still pass assembly's own admission — the
	// packet's aggregate bounds and the original payloads — before the
	// preview reports none, in the sentence assembly would refuse with.
	if len(p.Problems) == 0 {
		if _, err := admitRetained(ctx, in); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			p.Problems = append(p.Problems, err.Error())
		}
	}
	return p, nil
}

// previewCase verifies one case input through the same bundle reader
// assembly's inspection opens the copied case with.
func previewCase(path string) *RetainedInputView {
	view := &RetainedInputView{Problems: []string{}}
	opened, err := bundle.Open(path)
	if err != nil {
		view.Problems = append(view.Problems, "the case is not a case bundle this release verifies")
		return view
	}
	view.Found = true
	view.Identity = opened.Identity
	view.Provenance = string(opened.Manifest.Provenance.Mode)
	return view
}

// previewSpec reads and decodes one specification input through the same
// bounded reader and decoder assembly reads the copied specification with,
// and compares its identity with the specification the current execution
// retained, so the exact historical bytes are what assembly sees — never the
// current editable test.
func previewSpec(path string, current *runresult.Result) *RetainedInputView {
	view := &RetainedInputView{Problems: []string{}}
	raw, err := retainedSpecFile.Read(path)
	if err != nil {
		view.Problems = append(view.Problems, "the specification is not a bounded regular file")
		return view
	}
	if _, err := testrunner.DecodeSpec(raw); err != nil {
		view.Problems = append(view.Problems, "the specification is not one this release assembles")
		return view
	}
	view.Found = true
	view.Identity = digest(raw)
	if current != nil && current.Artifact != nil {
		view.SpecMatch = view.Identity == current.Artifact.Result.SpecIdentity
		if !view.SpecMatch {
			view.Problems = append(view.Problems, "the specification is not the exact one the current result retained; assembly never substitutes the current editable test for a historical one")
		}
	}
	return view
}

// previewRun opens one retained execution through the same reader assembly's
// inspection opens the copied execution with and reports its separate facts.
func previewRun(path string) (*runresult.Result, *RetainedInputView) {
	view := &RetainedInputView{Problems: []string{}}
	retained, err := runresult.Open(path)
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		view.Problems = append(view.Problems, "the retained execution was evaluated by a version this release cannot read")
		return nil, view
	}
	if err != nil {
		view.Problems = append(view.Problems, "the entry is not a retained execution this release verifies")
		return nil, view
	}
	view.Found = true
	view.Durable = retained.Durable
	if retained.Durable {
		view.RunState = string(retained.Lifecycle.State)
		view.JournalIncomplete = retained.Lifecycle.JournalIncomplete
		view.DeliveryUncertain = retained.Lifecycle.DeliveryUncertain
	}
	if retained.Artifact == nil {
		view.Problems = append(view.Problems, "the retained execution has no finalized result; assembly refuses an incomplete job")
		return retained, view
	}
	if usable, _ := retained.Usable(); !usable {
		view.Problems = append(view.Problems, "the retained execution is incomplete or its delivery is uncertain; assembly refuses it")
	}
	artifact := retained.Artifact
	view.Status = string(artifact.Result.Status)
	view.ErrorClass = artifact.Result.ErrorClass
	view.Boundary = artifact.Result.ObservationBoundary
	view.ResultIdentity = artifact.Identity
	view.SpecIdentity = artifact.Result.SpecIdentity
	view.CaseIdentity = artifact.Result.InputBundleIdentity
	view.TargetIdentity = artifact.Result.TargetIdentity
	return retained, view
}

// inputProblems collects the input views' own sentences into the preview's one
// problem list.
func inputProblems(views ...*RetainedInputView) []string {
	problems := []string{}
	for _, view := range views {
		if view == nil {
			continue
		}
		problems = append(problems, view.Problems...)
	}
	return problems
}

// retainedBaselineBoundaries states what a supplied baseline does and does not
// establish, from the two runs' own verified facts.
func retainedBaselineBoundaries(baseline, current *runresult.Result) []string {
	limitations := []string{}
	if baseline == nil || baseline.Artifact == nil || current == nil || current.Artifact == nil {
		return limitations
	}
	sameCase := baseline.Artifact.Result.InputBundleIdentity == current.Artifact.Result.InputBundleIdentity
	sameTarget := baseline.Artifact.Result.TargetIdentity == current.Artifact.Result.TargetIdentity
	if sameCase && sameTarget {
		limitations = append(limitations, "Baseline and current share one input and one target configuration identity; their outcomes differ only as the retained evidence records.")
	} else if sameCase {
		limitations = append(limitations, "The baseline input identity matches the current one; the target configuration identity changed between the two executions.")
	} else {
		limitations = append(limitations, "The baseline was executed against a different case identity than the current result; the comparison spans changed inputs.")
	}
	return limitations
}

// retainedPacketInventory names the sections the packet will hold, with the
// counts the verified evidence itself reports, from the same layout assembly
// copies. Every evidence file is copied byte for byte; the assembled packet's
// manifest is the complete index.
func retainedPacketInventory(p *RetainedPreview, current *runresult.Result) []string {
	inventory := []string{}
	if p.Case != nil && p.Case.Found {
		inventory = append(inventory, "case/ — the verified case bundle, copied byte for byte")
	}
	inventory = append(inventory, "spec.json — the exact historical specification bytes")
	if current != nil && current.Artifact != nil && p.Current != nil {
		summary := "current/ — the retained execution, byte for byte"
		summary += " (" + string(current.Artifact.Result.Status) + ", boundary " + current.Artifact.Result.ObservationBoundary
		if current.Spec != nil {
			summary += fmt.Sprintf(", %d selected messages", len(current.Spec.Input.Messages))
		}
		summary += ")"
		inventory = append(inventory, summary)
	}
	inventory = append(inventory, "SUMMARY.md and RERUN.md — regenerated outcomes, limitations and rerun instructions")
	return inventory
}

// retainedBaselineInventory names the sections a supplied baseline adds.
func retainedBaselineInventory(p *RetainedPreview, baseline *runresult.Result) []string {
	inventory := []string{}
	if p.Baseline != nil && p.Baseline.Found {
		summary := "baseline/ — the retained baseline execution, byte for byte"
		if baseline != nil && baseline.Artifact != nil {
			summary += " (" + string(baseline.Artifact.Result.Status) + ")"
		}
		inventory = append(inventory, summary)
	}
	if p.BaselineCase != nil && p.BaselineCase.Found {
		inventory = append(inventory, "baseline-case/ — the baseline's own source case, byte for byte")
	}
	return inventory
}

func retainedSpec(path string) ([]byte, error) {
	raw, err := retainedSpecFile.Read(path)
	if err != nil {
		return nil, err
	}
	if _, err := testrunner.DecodeSpec(raw); err != nil {
		return nil, errors.New("unsupported retained specification")
	}
	return raw, nil
}

// retainedSpecFile is how a retained specification is read: never through a
// link, and never past its bound.
var retainedSpecFile = artifactdir.Document{
	MaxBytes: testrunner.MaxSpecBytes,
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("specification must be a bounded regular file"),
		Read:      errors.New("specification must be a bounded regular file"),
	},
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
	m := RetainedManifest{Schema: RetainedSchema, State: "complete", ExportPolicy: retainedExportPolicy, ContainsSourceValues: retainedContainsSourceValues}
	invalid := errors.New("retained case, specification and execution identity or provenance mismatch")
	current, err := inspectRetainedRun(filepath.Join(dir, "case"), filepath.Join(dir, "current"))
	if err != nil {
		return m, nil, err
	}
	m.Current = current
	if digest(files["spec.json"]) != current.SpecIdentity || !bytes.Equal(files["spec.json"], files[retainedResultPrefix(files, "current")+"/spec.json"]) {
		return m, nil, invalid
	}
	hasBaseline := files["baseline/identity.sha256"] != nil || runresult.ExecutionFamilyIn(files, "baseline") == runresult.JobFamily
	allowed := []string{"case/", "current/"}
	if hasBaseline {
		baseline, err := inspectRetainedRun(filepath.Join(dir, "baseline-case"), filepath.Join(dir, "baseline"))
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

// inspectRetainedRun opens one retained execution and the case it must bind,
// and checks every original payload the execution retained against that
// case's bytes. Admission makes it of the selected inputs and the sealing
// inspection of the packet's own copies.
func inspectRetainedRun(casePath, resultPath string) (RetainedRun, error) {
	invalid := errors.New("retained execution does not bind the supplied case and selected original bytes")
	c, err := bundle.Open(casePath)
	if err != nil {
		return RetainedRun{}, invalid
	}
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

// retainedResultPrefix is where the execution retained under name keeps its
// result: inside result/ for a durable run, at name itself for a result.
func retainedResultPrefix(files map[string][]byte, name string) string {
	if runresult.ExecutionFamilyIn(files, name) == runresult.JobFamily {
		return name + "/result"
	}
	return name
}
