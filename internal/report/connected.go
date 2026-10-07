package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/runcompare"
)

// ConnectedSchema is the retained packet of actual connected lifecycle runs:
// readmit-connected-run/v3 (v2 through an actual integration) and v4 (FHIR).
// readmit-retained-packet/v1 and its reader are unchanged; a v1 reader refuses
// this version and this reader refuses v1.
const ConnectedSchema = "readmit-retained-packet/v2"

// The evidence classes a packet can be, kept apart: original customer-local
// evidence is never a reviewed transformed extract, and neither is a
// regression-equivalent reproducer unless a replay proves it.
const (
	EvidenceOriginal = "original-customer-local"
	EvidenceExtract  = "disclosure-reviewed-extract"
)

// Equivalence states. Only two retained actual executions of the same plan
// against the same declared target, failing with the same signature, are a
// reproduced regression; everything else says what it is not.
const (
	EquivalenceNotClaimed    = "not-claimed"
	EquivalenceReproduced    = "reproduced"
	EquivalenceNotReproduced = "not-reproduced"
	EquivalenceUnverified    = "unverified"
)

// Connected packets hold whole lifecycles, which are larger and deeper than
// the v1 bounds; they are still bounded and read without links.
const (
	connectedMaxFiles     = 60000
	connectedMaxFileBytes = 64 << 20
	connectedMaxBytes     = 512 << 20
	connectedMaxPath      = 400
	connectedMaxDepth     = 16
)

// ConnectedInput selects retained lifecycle results; none is executed.
// Baseline is an optional earlier distinct execution to compare against;
// Replay is an optional distinct later execution of the same plan against the
// same declared target, retained as the proof a failure reproduces.
type ConnectedInput struct{ Current, Baseline, Replay string }

// ConnectedEnvironment is the environment, policy and target revision
// provenance the lifecycle's plan pinned, as declarations, never as proof of
// reachability, authorization or target software identity.
type ConnectedEnvironment struct {
	Project               string                       `json:"project"`
	ID                    string                       `json:"id"`
	Revision              string                       `json:"revision"`
	Classification        string                       `json:"classification"`
	TargetIdentity        string                       `json:"target_identity"`
	AddressPolicyIdentity string                       `json:"address_policy_identity"`
	TargetRevision        connectedtest.TargetRevision `json:"target_revision"`
	Servers               []ConnectedServer            `json:"fhir_servers"`
}

// ConnectedServer is one pinned FHIR server: its base and the digest of the
// reviewed CapabilityStatement the plan was authored against.
type ConnectedServer struct {
	ID         string `json:"id"`
	Base       string `json:"base"`
	Capability string `json:"capability"`
}

// ConnectedPhase summarizes one phase: its state and verdict as the
// lifecycle's own reader re-derived them, and every check's outcome in order.
type ConnectedPhase struct {
	ID      string                   `json:"id"`
	State   string                   `json:"state"`
	Verdict string                   `json:"verdict"`
	Checks  []connectedrun.FlowCheck `json:"checks"`
}

// ConnectedRun is one retained lifecycle as its verified reader states it.
// Failures is the failure signature: every failed check as phase/check.
type ConnectedRun struct {
	Identity      string                   `json:"identity"`
	Schema        string                   `json:"schema"`
	Plan          string                   `json:"plan"`
	Test          string                   `json:"test"`
	Instance      string                   `json:"instance"`
	Engine        string                   `json:"engine"`
	Boundary      string                   `json:"boundary"`
	State         string                   `json:"state"`
	Verdict       string                   `json:"verdict"`
	Setup         string                   `json:"setup"`
	Cleanup       string                   `json:"cleanup"`
	Environment   ConnectedEnvironment     `json:"environment"`
	Qualification []connectedrun.FlowClaim `json:"qualification"`
	Phases        []ConnectedPhase         `json:"phases"`
	Failures      []string                 `json:"failures"`
}

// ConnectedEquivalence is the packet's regression-equivalence claim and the
// fixed sentence explaining it.
type ConnectedEquivalence struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type ConnectedManifest struct {
	Schema               string               `json:"schema"`
	State                string               `json:"state"`
	ExportPolicy         string               `json:"export_policy"`
	ContainsSourceValues bool                 `json:"contains_source_values"`
	EvidenceClass        string               `json:"evidence_class"`
	Current              ConnectedRun         `json:"current"`
	Baseline             *ConnectedRun        `json:"baseline"`
	Replay               *ConnectedRun        `json:"replay"`
	Equivalence          ConnectedEquivalence `json:"equivalence"`
	Instructions         string               `json:"instructions,omitzero"`
	Files                []bundle.Payload     `json:"files"`
}

// ConnectedReanalysis is later analysis, recorded apart from the sealed
// original: the original engine and verdict, this release's engine and the
// verdict it re-derives, and what it could not re-evaluate.
type ConnectedReanalysis struct {
	Section           string   `json:"section"`
	OriginalEngine    string   `json:"original_engine"`
	OriginalVerdict   string   `json:"original_verdict"`
	CurrentEngine     string   `json:"current_engine"`
	ReanalyzedVerdict string   `json:"reanalyzed_verdict"`
	Limitations       []string `json:"limitations"`
}

// ConnectedPacket is a verified packet. Reanalysis and Comparison are computed
// by this reader from the verified evidence and are never part of the seal.
type ConnectedPacket struct {
	Manifest   ConnectedManifest
	Identity   string
	Reanalysis []ConnectedReanalysis
	Comparison *runcompare.FlowComparison
	evidence   map[string]connectedrun.FlowEvidence
	// historical is every retained validation's verified outcome, by
	// section/phase/check.
	historical map[string]*ValidationOutcome
}

// EvidenceChange names one retained file whose bytes are not what the packet
// sealed, by its section and the evidence surface it belongs to.
type EvidenceChange struct {
	Path    string `json:"path"`
	Section string `json:"section"`
	Surface string `json:"surface"`
	Kind    string `json:"kind"`
}

// ChangedEvidenceError refuses a packet and names what changed. It carries
// relative packet paths and fixed surface names, never evidence values.
type ChangedEvidenceError struct{ Changes []EvidenceChange }

func (e *ChangedEvidenceError) Error() string {
	parts := []string{}
	for _, c := range e.Changes {
		parts = append(parts, c.Section+": "+c.Surface+" ("+c.Kind+" "+c.Path+")")
	}
	return "retained connected packet refused; changed evidence: " + strings.Join(parts, "; ")
}

var connectedSections = []string{"current", "baseline", "replay"}

var connectedFamily = func() artifactdir.Family {
	f := sealed("manifest.json", "identity.sha256", nil, connectedSections, "SUMMARY.md", "RERUN.md")
	f.Layout.MaxFiles, f.Layout.MaxFileBytes, f.Layout.MaxBytes = connectedMaxFiles, connectedMaxFileBytes, connectedMaxBytes
	return f
}()

// connectedLayout reads a connected packet or one lifecycle to copy: bounded
// regular files, no links, short and shallow names. A lifecycle keeps empty
// operational directories (a phase with no validation, say); they carry no
// evidence and are not copied, so a packet itself holds no empty directory.
func connectedLayout(empty bool) artifactdir.Layout {
	entries := 0
	admit := func(name string) bool {
		entries++
		return entries <= connectedMaxFiles*2 && len(name) <= connectedMaxPath && strings.Count(name, "/") <= connectedMaxDepth && !strings.Contains(name, "\\")
	}
	return artifactdir.Layout{
		AllowDirectory: admit,
		AllowFile:      admit,
		AllowEmpty:     func(string, map[string][]byte) bool { return empty },
		MaxFiles:       connectedMaxFiles,
		MaxFileBytes:   connectedMaxFileBytes,
		MaxBytes:       connectedMaxBytes,
	}
}

func readConnected(dir string, empty bool) (map[string][]byte, error) {
	files, err := artifactdir.Read(dir, connectedLayout(empty))
	if err != nil {
		return nil, errors.New("connected evidence must be bounded regular files without symlinks or empty directories")
	}
	return files, nil
}

// AssembleConnected copies whole retained lifecycles into a new private
// packet, verifies the copies through the lifecycle reader and seals it. It
// sends nothing, reruns nothing and never manufactures a baseline or replay.
func AssembleConnected(ctx context.Context, in ConnectedInput, output string) (*ConnectedPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.Current == "" {
		return nil, errors.New("select the retained current lifecycle result")
	}
	sources := map[string]string{"current": in.Current, "baseline": in.Baseline, "replay": in.Replay}
	files := map[string][]byte{}
	total := 0
	for _, section := range connectedSections {
		if sources[section] == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// A source is verified before anything is written, then its exact
		// snapshot is copied and the copy verified again before sealing.
		if _, err := connectedrun.OpenFlowEvidence(ctx, sources[section]); err != nil {
			return nil, errors.New("the " + section + " selection is not a verified retained connected lifecycle result this release reads")
		}
		tree, err := readConnected(sources[section], true)
		if err != nil {
			return nil, err
		}
		for name, data := range tree {
			name = section + "/" + name
			total += len(data)
			if len(files) >= connectedMaxFiles-4 || total > connectedMaxBytes-(8<<20) || len(name) > connectedMaxPath || strings.Count(name, "/") > connectedMaxDepth {
				return nil, errors.New("retained connected packet exceeds path, file or byte limits")
			}
			files[name] = data
		}
	}
	packet, err := artifactdir.Create(output, connectedFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer packet.Close()
	dir := packet.Path()
	if err := copyFiles(packet, files, "", ""); err != nil {
		return nil, err
	}
	manifest, summary, _, _, err := inspectConnected(ctx, dir, files)
	if err != nil {
		return nil, err
	}
	files["SUMMARY.md"] = summary
	files["RERUN.md"] = connectedInstructions()
	for _, name := range []string{"SUMMARY.md", "RERUN.md"} {
		if err := packet.WriteFile(name, files[name]); err != nil {
			return nil, err
		}
	}
	manifest.Instructions = connectedInstructionsBlock.current
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
	return OpenConnected(ctx, dir)
}

// OpenConnected verifies a connected packet offline: its seal, every indexed
// file, every nested lifecycle through its own reader, and every claim the
// manifest and summary make. A changed file is refused by name and surface.
// It then re-analyzes each lifecycle with this release, recorded apart.
func OpenConnected(ctx context.Context, dir string) (*ConnectedPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}
	files, err := readConnected(dir, false)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid, incomplete, changed or unsupported retained connected packet")
	raw := files["manifest.json"]
	var stored ConnectedManifest
	if len(raw) > 64<<20 || json.Unmarshal(raw, &stored, json.RejectUnknownMembers(true)) != nil || stored.Schema != ConnectedSchema {
		return nil, invalid
	}
	if changes := indexChanges(stored.Files, files); len(changes) > 0 {
		return nil, &ChangedEvidenceError{Changes: changes}
	}
	if string(files["identity.sha256"]) != digest(raw)+"\n" {
		return nil, &ChangedEvidenceError{Changes: []EvidenceChange{{Path: "identity.sha256", Section: "packet", Surface: "packet seal", Kind: "changed"}}}
	}
	expected, summary, evidence, comparison, err := inspectConnected(ctx, dir, files)
	if err != nil {
		return nil, err
	}
	// The recorded instructions version is a sealed claim checked below
	// against the known texts, not evidence re-derived above.
	expected.Instructions = stored.Instructions
	expected.Files = index(files)
	canonical, err := encode(expected)
	if err != nil {
		return nil, invalid
	}
	changes := []EvidenceChange{}
	if !bytes.Equal(raw, canonical) {
		changes = append(changes, manifestChanges(stored, expected)...)
	}
	if !bytes.Equal(summary, files["SUMMARY.md"]) {
		changes = append(changes, EvidenceChange{Path: "SUMMARY.md", Section: "packet", Surface: "packet summary claims", Kind: "contradicts evidence"})
	}
	if !connectedInstructionsBlock.check(stored.Instructions, files["RERUN.md"]) {
		changes = append(changes, EvidenceChange{Path: "RERUN.md", Section: "packet", Surface: "rerun instructions", Kind: "contradicts evidence"})
	}
	if len(changes) > 0 {
		return nil, &ChangedEvidenceError{Changes: changes}
	}
	p := &ConnectedPacket{Manifest: expected, Identity: digest(raw), Reanalysis: []ConnectedReanalysis{}, Comparison: comparison, evidence: evidence, historical: map[string]*ValidationOutcome{}}
	for _, section := range connectedSections {
		e, ok := evidence[section]
		if !ok {
			continue
		}
		for phase, pe := range e.Phases {
			for check := range pe.Validators {
				validation := connectedrun.ValidationDir(phase, check)
				retained, err := fhirvalidator.Open(ctx, filepath.Join(dir, section, filepath.FromSlash(validation)))
				if err != nil {
					return nil, &ChangedEvidenceError{Changes: []EvidenceChange{{Path: section + "/" + validation + "/", Section: section, Surface: EvidenceSurface(validation + "/"), Kind: "refused"}}}
				}
				p.historical[section+"/"+phase+"/"+check] = outcomeOf(retained)
			}
		}
		analysis, err := reanalyze(ctx, filepath.Join(dir, section), section, e)
		if err != nil {
			return nil, err
		}
		p.Reanalysis = append(p.Reanalysis, analysis)
	}
	return p, nil
}

// indexChanges compares the sealed index with the files present, naming every
// changed, missing and unindexed file by its evidence surface.
func indexChanges(indexed []bundle.Payload, files map[string][]byte) []EvidenceChange {
	changes := []EvidenceChange{}
	seen := map[string]bool{"manifest.json": true, "identity.sha256": true}
	for _, entry := range indexed {
		seen[entry.Path] = true
		data, ok := files[entry.Path]
		switch {
		case !ok:
			changes = append(changes, evidenceChange(entry.Path, "missing"))
		case len(data) != entry.Size || digest(data) != entry.SHA256:
			changes = append(changes, evidenceChange(entry.Path, "changed"))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if !seen[name] {
			changes = append(changes, evidenceChange(name, "added"))
		}
	}
	return changes
}

func evidenceChange(path, kind string) EvidenceChange {
	section, rest, found := strings.Cut(path, "/")
	if !found || !slices.Contains(connectedSections, section) {
		return EvidenceChange{Path: path, Section: "packet", Surface: "packet " + strings.TrimSuffix(strings.ToLower(path), ".md") + " file", Kind: kind}
	}
	return EvidenceChange{Path: path, Section: section, Surface: EvidenceSurface(rest), Kind: kind}
}

// EvidenceSurface names the evidence surface one file of a retained
// lifecycle belongs to, from its relative path alone. The layout itself is
// classified once, beside its writers; this names the parts.
func EvidenceSurface(rel string) string {
	switch loc := connectedrun.Locate(rel); loc.Area {
	case connectedrun.AreaSeal:
		return "lifecycle seal"
	case connectedrun.AreaRecord:
		return "lifecycle record (verdicts, intents and times)"
	case connectedrun.AreaPlanDeps:
		return "pinned dependency (check set, profile, capability, projection or completion policy)"
	case connectedrun.AreaPlan, connectedrun.AreaRuntimeInputs:
		return "compiled plan (definitions, inputs, environment and policy pins)"
	case connectedrun.AreaSetup:
		return "setup and cleanup evidence"
	case connectedrun.AreaPhase:
		surface := "phase record"
		switch loc.Sub {
		case connectedrun.PhaseSteps:
			surface = "FHIR request and response evidence"
		case connectedrun.PhaseTransport:
			surface = "v2 transport evidence (sent messages and acknowledgements)"
		case connectedrun.PhaseObservations:
			surface = "observation snapshot"
		case connectedrun.PhaseIntervals:
			surface = "observation completion record"
			if loc.Samples {
				surface = "observation sample (retained search responses and typed rows)"
			}
		case connectedrun.PhaseValidations:
			surface = "validator evidence"
		case connectedrun.PhasePreflight:
			surface = "capability preflight"
		case connectedrun.PhaseEvaluation, connectedrun.PhaseEvaluationDatasets:
			surface = "phase evaluation"
		case connectedrun.PhasePlan:
			surface = "phase plan"
		}
		return "phase " + loc.Phase + ": " + surface
	default:
		return "lifecycle file"
	}
}

// manifestChanges names which claims of a stored manifest the evidence does
// not support, when every indexed byte is intact but a claim was rewritten.
func manifestChanges(stored, expected ConnectedManifest) []EvidenceChange {
	changes := []EvidenceChange{}
	claim := func(section, surface string, a, b any) {
		if !bytes.Equal(canonicalJSON(a), canonicalJSON(b)) {
			changes = append(changes, EvidenceChange{Path: "manifest.json", Section: section, Surface: surface, Kind: "contradicts evidence"})
		}
	}
	claim("current", "run claims", stored.Current, expected.Current)
	claim("baseline", "run claims", stored.Baseline, expected.Baseline)
	claim("replay", "run claims", stored.Replay, expected.Replay)
	claim("packet", "equivalence claim", stored.Equivalence, expected.Equivalence)
	claim("packet", "evidence class and sensitivity", []any{stored.State, stored.ExportPolicy, stored.ContainsSourceValues, stored.EvidenceClass}, []any{expected.State, expected.ExportPolicy, expected.ContainsSourceValues, expected.EvidenceClass})
	if len(changes) == 0 {
		changes = append(changes, EvidenceChange{Path: "manifest.json", Section: "packet", Surface: "packet manifest encoding", Kind: "not canonical"})
	}
	return changes
}

func canonicalJSON(v any) []byte {
	raw, _ := json.Marshal(v, json.Deterministic(true))
	return raw
}

// inspectConnected re-derives every claim from the packet's own copies.
func inspectConnected(ctx context.Context, dir string, files map[string][]byte) (ConnectedManifest, []byte, map[string]connectedrun.FlowEvidence, *runcompare.FlowComparison, error) {
	m := ConnectedManifest{Schema: ConnectedSchema, State: "complete", ExportPolicy: retainedExportPolicy, ContainsSourceValues: retainedContainsSourceValues, EvidenceClass: EvidenceOriginal}
	present := map[string]bool{}
	for name := range files {
		section, _, found := strings.Cut(name, "/")
		if found && slices.Contains(connectedSections, section) {
			present[section] = true
			continue
		}
		if !slices.Contains([]string{"manifest.json", "identity.sha256", "SUMMARY.md", "RERUN.md"}, name) {
			return m, nil, nil, nil, &ChangedEvidenceError{Changes: []EvidenceChange{evidenceChange(name, "added")}}
		}
	}
	if !present["current"] {
		return m, nil, nil, nil, errors.New("a retained connected packet holds its current lifecycle")
	}
	evidence := map[string]connectedrun.FlowEvidence{}
	runs := map[string]*ConnectedRun{}
	for _, section := range connectedSections {
		if !present[section] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return m, nil, nil, nil, err
		}
		e, err := connectedrun.OpenFlowEvidence(ctx, filepath.Join(dir, section))
		if err != nil {
			return m, nil, nil, nil, &ChangedEvidenceError{Changes: []EvidenceChange{{Path: section + "/", Section: section, Surface: "retained lifecycle does not re-derive from its own evidence", Kind: "refused"}}}
		}
		evidence[section] = e
		run := connectedRun(e)
		runs[section] = &run
	}
	m.Current, m.Baseline, m.Replay = *runs["current"], runs["baseline"], runs["replay"]
	for _, other := range []*ConnectedRun{m.Baseline, m.Replay} {
		if other != nil && other.Identity == m.Current.Identity {
			return m, nil, nil, nil, errors.New("a baseline or replay must be a distinct retained execution, never the current run relabelled")
		}
	}
	if m.Baseline != nil && m.Replay != nil && m.Baseline.Identity == m.Replay.Identity {
		return m, nil, nil, nil, errors.New("the baseline and the replay must be distinct retained executions")
	}
	m.Equivalence = equivalence(m.Current, m.Replay)
	var comparison *runcompare.FlowComparison
	if b, ok := evidence["baseline"]; ok {
		c, err := runcompare.CompareFlowEvidence(ctx, b, evidence["current"])
		if err != nil {
			return m, nil, nil, nil, err
		}
		comparison = &c
	}
	return m, connectedSummary(m, comparison), evidence, comparison, nil
}

func connectedRun(e connectedrun.FlowEvidence) ConnectedRun {
	r := e.Result
	doc := e.Plan.Document()
	env := doc.Test.Environment
	run := ConnectedRun{Identity: e.Identity, Schema: r.Schema, Plan: r.Plan, Test: doc.Test.ID + "@" + doc.Test.Revision, Instance: r.Instance, Engine: r.Engine, Boundary: r.Boundary, State: r.State, Verdict: string(r.Verdict), Setup: r.Setup, Cleanup: r.Cleanup,
		Environment:   ConnectedEnvironment{Project: env.Project, ID: env.ID, Revision: env.Revision, Classification: env.Classification, TargetIdentity: env.TargetIdentity, AddressPolicyIdentity: env.AddressPolicyIdentity, TargetRevision: env.TargetRevision, Servers: []ConnectedServer{}},
		Qualification: []connectedrun.FlowClaim{}, Phases: []ConnectedPhase{}, Failures: []string{}}
	for _, s := range doc.Test.Servers {
		run.Environment.Servers = append(run.Environment.Servers, ConnectedServer{ID: s.ID, Base: s.Base, Capability: s.Capability.SHA256})
	}
	run.Qualification = append(run.Qualification, r.Qualification...)
	for _, phase := range r.Phases {
		checks := append([]connectedrun.FlowCheck{}, phase.Checks...)
		run.Phases = append(run.Phases, ConnectedPhase{ID: phase.ID, State: phase.State, Verdict: string(phase.Verdict), Checks: checks})
		for _, c := range phase.Checks {
			if c.Outcome == assertion.OutcomeFailed {
				run.Failures = append(run.Failures, phase.ID+"/"+c.ID)
			}
		}
	}
	return run
}

// equivalence decides the packet's reproduction claim from the two retained
// executions alone: never from a residual scan, a fixture or a transformed
// extract.
func equivalence(current ConnectedRun, replay *ConnectedRun) ConnectedEquivalence {
	if replay == nil {
		return ConnectedEquivalence{State: EquivalenceNotClaimed, Reason: "No replay execution is retained; this packet is original evidence, not a regression-equivalent reproducer."}
	}
	not := func(reason string) ConnectedEquivalence {
		return ConnectedEquivalence{State: EquivalenceNotReproduced, Reason: reason}
	}
	switch {
	case current.State != "complete" || replay.State != "complete":
		return not("An execution did not complete (uncertain, incomplete or cancelled); an unresolved execution is never reproduction.")
	case current.Plan != replay.Plan:
		return not("The replay ran a different plan; changed definitions, inputs or pins are not a reproduction of the same test.")
	case !bytes.Equal(canonicalJSON(current.Environment), canonicalJSON(replay.Environment)):
		return not("The replay declared a different environment, policy, target or target revision.")
	case current.Verdict != string(assertion.VerdictFail) || len(current.Failures) == 0:
		return not("The current execution records no failure, so there is no failure signature to reproduce.")
	case !slices.Equal(current.Failures, replay.Failures):
		return not("The replay failed with a different failure signature.")
	}
	return ConnectedEquivalence{State: EquivalenceReproduced, Reason: "Two retained actual executions of the same plan against the same declared target and revision failed with the same failure signature. This is within the declared observation boundaries only; hashes do not authenticate the target software."}
}

func connectedSummary(m ConnectedManifest, comparison *runcompare.FlowComparison) []byte {
	var s strings.Builder
	s.WriteString("# Retained connected lifecycle packet\n\nCustomer-local original evidence: contains source values, observed resources, requests, responses and historical configuration. It is not a reviewed extract, a disclosure approval or de-identification.\n\n")
	run := func(title string, r ConnectedRun) {
		fmt.Fprintf(&s, "## %s\n\nLifecycle: %s (%s); verdict: %s; state: %s\nSetup: %s; cleanup: %s\nPlan: %s; test: %s\nEngine at execution: %s\nEnvironment: %s/%s revision %s (%s); target configuration %s; address policy %s\nTarget revision: %s (%s)\n",
			title, r.Identity, r.Schema, r.Verdict, r.State, r.Setup, r.Cleanup, r.Plan, r.Test, r.Engine, r.Environment.Project, r.Environment.ID, r.Environment.Revision, r.Environment.Classification, r.Environment.TargetIdentity, r.Environment.AddressPolicyIdentity, r.Environment.TargetRevision.Value, r.Environment.TargetRevision.Provenance)
		for _, q := range r.Qualification {
			fmt.Fprintf(&s, "Observation %s/%s boundary %s: %s\n", q.Phase, q.Dataset, q.Boundary, q.Meaning)
		}
		for _, p := range r.Phases {
			fmt.Fprintf(&s, "Phase %s: %s, %s\n", p.ID, p.State, p.Verdict)
			for _, c := range p.Checks {
				fmt.Fprintf(&s, "  %s %s: %s\n", checkClaim(c.ID), c.ID, c.Outcome)
			}
		}
		s.WriteString("\n")
	}
	run("Current", m.Current)
	if m.Baseline != nil {
		run("Baseline", *m.Baseline)
	} else {
		s.WriteString("No observed baseline was supplied. This single-run packet proves no before/after improvement or regression.\n\n")
	}
	if m.Replay != nil {
		run("Replay", *m.Replay)
	}
	fmt.Fprintf(&s, "Regression equivalence: %s. %s\n\n", m.Equivalence.State, m.Equivalence.Reason)
	if comparison != nil {
		s.WriteString("## Baseline to current\n\n")
		for _, d := range comparison.Dimensions {
			fmt.Fprintf(&s, "%s: %s", d.Dimension, d.State)
			if len(d.Changed) > 0 {
				fmt.Fprintf(&s, " (%s)", strings.Join(d.Changed, ", "))
			}
			s.WriteString("\n")
		}
		fmt.Fprintf(&s, "Attribution: %s. %s\n", comparison.Attribution.Outcome, comparison.Attribution.Reason)
		for _, c := range comparison.Checks {
			fmt.Fprintf(&s, "Check %s/%s: %s -> %s; definition %s; behavior %s\n", c.Phase, c.Check, c.Baseline, c.Current, c.Definition, c.Behavior)
		}
		for _, r := range comparison.Records {
			fmt.Fprintf(&s, "Records %s/%s: %s", r.Phase, r.Dataset, r.State)
			if r.Reason != "" {
				fmt.Fprintf(&s, " (%s)", r.Reason)
			}
			s.WriteString("\n")
			for i, k := range r.Keys {
				fmt.Fprintf(&s, "  key %d: %d -> %d, %s", i+1, k.Baseline, k.Current, k.State)
				if len(k.Values) > 0 {
					fmt.Fprintf(&s, "; values %s", strings.Join(k.Values, ", "))
				}
				if len(k.Identities) > 0 {
					fmt.Fprintf(&s, "; server-assigned %s", strings.Join(k.Identities, ", "))
				}
				s.WriteString("\n")
			}
		}
		fmt.Fprintf(&s, "%s\n\n", comparison.Scope)
	}
	s.WriteString("Verdicts are re-derived offline from the retained snapshots, responses, completion records and pinned check sets; nothing is re-fetched, rerun or regenerated. Transport acceptance (wire), response outcome, profile validity (validation) and observed workflow assertions (typed) are separate claims. A missing observation is never an empty one. Hashes establish integrity, not source authenticity, target software identity, approval or a de-identification determination.\n")
	return []byte(s.String())
}

// checkClaim is the kind of claim a check makes, from its kind prefix.
func checkClaim(id string) string {
	kind, _, _ := strings.Cut(id, ":")
	switch kind {
	case "wire":
		return "transport acceptance"
	case "response":
		return "response outcome"
	case "validation":
		return "profile validity"
	case "typed":
		return "observed workflow assertion"
	}
	return "check"
}

// reanalyze re-evaluates one retained lifecycle with this release, keeping the
// original engine and verdict apart and naming what it could not re-evaluate.
func reanalyze(ctx context.Context, path, section string, e connectedrun.FlowEvidence) (ConnectedReanalysis, error) {
	analysis, err := connectedrun.ReanalyzeFlow(ctx, path)
	if err != nil {
		return ConnectedReanalysis{}, err
	}
	r := ConnectedReanalysis{Section: section, OriginalEngine: analysis.Original.Engine, OriginalVerdict: string(analysis.Original.Verdict), CurrentEngine: analysis.Reanalysis.Engine, ReanalyzedVerdict: string(analysis.Reanalysis.Verdict), Limitations: []string{
		"Typed, wire and response checks are re-evaluated from the retained snapshots, acknowledgements and responses with the check sets the plan pinned.",
	}}
	if analysis.Original.Engine != engine.Version() {
		r.Limitations = append(r.Limitations, "The original engine ("+analysis.Original.Engine+") is not this release ("+engine.Version()+"); the retained contracts are read with their fixed-version semantics, and the original verdict stands unchanged beside this reanalysis.")
	}
	for _, phase := range e.Plan.Document().Test.Phases {
		validators := e.Phases[phase.ID].Validators
		for _, check := range phase.Validations {
			v, ok := validators[check.ID]
			switch {
			case !ok:
				r.Limitations = append(r.Limitations, "Phase "+phase.ID+" validation "+check.ID+": no validation was retained; it stays undecided and cannot be supplied now.")
			case v.RuntimeState != "":
				r.Limitations = append(r.Limitations, "Phase "+phase.ID+" validation "+check.ID+": no validator outcome was retained ("+v.RuntimeState+"); it stays undecided. Verification never starts a validator; an explicit revalidation with the installed, identically pinned capability runs it on the retained resource as a separate analysis.")
			case v.EngineVersion == "":
				r.Limitations = append(r.Limitations, "Phase "+phase.ID+" validation "+check.ID+": the validator's container engine version was not recorded; its retained output is reinterpreted with capability "+v.Capability+" only.")
			default:
				r.Limitations = append(r.Limitations, "Phase "+phase.ID+" validation "+check.ID+": the retained output of validator capability "+v.Capability+" (engine "+v.EngineVersion+") is reinterpreted offline; verification never starts a validator. An explicit revalidation with the installed, identically pinned capability reruns it on the retained resource as a separate analysis.")
			}
		}
	}
	return r, nil
}
