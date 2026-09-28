package report

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// RevalidationSchema is a later analysis of a retained connected packet's FHIR
// validations: the pinned validator run again, explicitly, on the resource
// bytes each historical validation retained. It is written beside the packet,
// never into it, and never replaces a historical verdict.
const RevalidationSchema = "readmit-connected-revalidation/v1"

// Revalidation statuses and agreements.
const (
	Revalidated    = "revalidated"
	NotRevalidated = "not-revalidated"

	AgreementAgrees     = "agrees"
	AgreementDiffers    = "differs"
	AgreementNoOutcome  = "no-historical-outcome"
	AgreementNotCompare = "not-compared"
)

// The reasons a validation was not run again before any worker started. A
// worker that started and stopped reports its own worker or runtime state.
var revalidationRefusals = []string{"capability-not-installed", "capability-mismatch", "no-retained-validation", "phase-not-attempted", "request-not-declared", "request-refused", "request-identity-mismatch"}

const revalidationScope = "Explicit revalidation: the installed validator capability, pinned identically to the historical one, was run again locally on the resource bytes each historical validation retained, with the historical request, through the local worker with networking disabled and isolation verified before start. Nothing was re-fetched and no target was contacted. The historical verdicts are unchanged; this is a separate analysis. A validation that was not run again says why, and is never a pass."

// RevalidationOptions selects what the operator installed: the staged
// capability directory and the local engine. Nothing is taken from the packet.
type RevalidationOptions struct {
	Capability string
	Engine     string
	Socket     string
}

// ValidationOutcome is one validation result by identity and outcome. It
// carries no finding text: diagnostics stay in the retained evidence.
type ValidationOutcome struct {
	Identity      string `json:"identity"`
	Capability    string `json:"capability"`
	Request       string `json:"request"`
	Input         string `json:"input_sha256"`
	State         string `json:"state"`
	Verdict       string `json:"verdict"`
	Worker        string `json:"worker"`
	RuntimeState  string `json:"runtime_state"`
	Findings      int    `json:"findings"`
	EngineVersion string `json:"engine_version"`
}

// RevalidatedValidation is one declared validation of an attempted phase: its
// historical outcome, whether it was run again and why not, the new outcome
// and how the two compare.
type RevalidatedValidation struct {
	Section     string             `json:"section"`
	Phase       string             `json:"phase"`
	Check       string             `json:"check"`
	Step        string             `json:"step"`
	Historical  *ValidationOutcome `json:"historical"`
	Status      string             `json:"status"`
	Reason      string             `json:"reason"`
	Revalidated *ValidationOutcome `json:"revalidated"`
	Agreement   string             `json:"agreement"`
	Differences []string           `json:"differences"`
	Evidence    string             `json:"evidence"`
}

// RevalidationEngine is the local engine selection and what it answered.
type RevalidationEngine struct {
	Selection string `json:"selection"`
	State     string `json:"state"`
}

type RevalidationManifest struct {
	Schema         string                  `json:"schema"`
	PacketIdentity string                  `json:"packet_identity"`
	Capability     string                  `json:"capability"`
	Engine         RevalidationEngine      `json:"engine"`
	Validations    []RevalidatedValidation `json:"validations"`
	Scope          string                  `json:"scope"`
	Files          []bundle.Payload        `json:"files"`
}

// ConnectedRevalidation is a verified revalidation of a verified packet.
type ConnectedRevalidation struct {
	Manifest RevalidationManifest
	Identity string
}

var revalidationFamily = func() artifactdir.Family {
	f := sealed("manifest.json", "identity.sha256", nil, []string{"validations"})
	f.Layout.MaxFiles, f.Layout.MaxFileBytes, f.Layout.MaxBytes = connectedMaxFiles, connectedMaxFileBytes, connectedMaxBytes
	return f
}()

// RevalidateConnected verifies the packet, then runs each retained FHIR
// validation again with the installed capability on the bytes it retained,
// writing a new sealed analysis at output. Opening or verifying a packet never
// does this; only this explicit operation starts a worker.
func RevalidateConnected(ctx context.Context, packetPath string, options RevalidationOptions, output string) (*ConnectedRevalidation, error) {
	if options.Engine != "local" && options.Engine != "none" || options.Engine == "none" && options.Socket != "" {
		return nil, errors.New("select the local validation engine, or none to record what is not run")
	}
	packet, err := OpenConnected(ctx, packetPath)
	if err != nil {
		return nil, err
	}
	dir, err := artifactpath.Directory(packetPath)
	if err != nil {
		return nil, err
	}
	files, err := readConnected(dir, false)
	if err != nil {
		return nil, err
	}
	if changes := indexChanges(packet.Manifest.Files, files); len(changes) > 0 {
		return nil, &ChangedEvidenceError{Changes: changes}
	}
	var installed *fhirvalidator.Capability
	if options.Capability != "" {
		if inside(dir, options.Capability) {
			return nil, errors.New("the capability copy retained in a packet is evidence, never the installed capability; select the administrator's staged capability")
		}
		if installed, err = fhirvalidator.OpenCapability(options.Capability); err != nil {
			return nil, errors.New("the selected capability is not a staged offline FHIR validation capability this release reads")
		}
	}
	m := RevalidationManifest{Schema: RevalidationSchema, PacketIdentity: packet.Identity, Engine: RevalidationEngine{Selection: options.Engine, State: "not-selected"}, Validations: []RevalidatedValidation{}, Scope: revalidationScope}
	if installed != nil {
		m.Capability = installed.Identity()
	}
	var engine *fhirvalidator.Engine
	if options.Engine == "local" {
		m.Engine.State = "ready"
		if engine, err = fhirvalidator.LocalEngine(options.Socket); err != nil {
			var status fhirvalidator.Status
			m.Engine.State = "worker-unavailable"
			if errors.As(err, &status) {
				m.Engine.State = status.State
			}
			engine = nil
		} else {
			defer engine.Close()
		}
	}
	w, err := artifactdir.Create(output, revalidationFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	written := map[string][]byte{}
	scratch, err := os.MkdirTemp("", "readmit-revalidation-")
	if err != nil {
		return nil, errors.New("cannot create a private revalidation workspace")
	}
	defer os.RemoveAll(scratch)
	for _, v := range declaredValidations(packet) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if v.Reason != "" {
			m.Validations = append(m.Validations, v)
			continue
		}
		prefix := v.Section + "/phases/" + v.Phase + "/validations/" + v.Check + "/"
		request, input := files[prefix+"request.json"], files[prefix+"input.json"]
		v.Status, v.Agreement, v.Differences = NotRevalidated, AgreementNotCompare, []string{}
		switch reason := v.requestReason(packet, request); {
		case reason != "":
			v.Reason = reason
		case installed == nil:
			v.Reason = "capability-not-installed"
		case installed.Identity() != v.Historical.Capability:
			v.Reason = "capability-mismatch"
		}
		if v.Reason != "" {
			m.Validations = append(m.Validations, v)
			continue
		}
		plan, err := fhirvalidator.Prepare(request, input, installed)
		switch {
		case err != nil:
			v.Reason = "request-refused"
		case plan.Identity() != v.Historical.Request:
			v.Reason = "request-identity-mismatch"
		}
		if v.Reason != "" {
			m.Validations = append(m.Validations, v)
			continue
		}
		run := filepath.Join(scratch, fmt.Sprintf("%04d", len(m.Validations)+1))
		if _, err := plan.Execute(ctx, engine, run); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		retained, err := artifactdir.Read(run, artifactdir.Layout{Nested: []string{"capability"}, AllowFile: func(n string) bool { return !strings.Contains(n, "/") }, MaxFiles: 1024, MaxFileBytes: 32 << 20, MaxBytes: 192 << 20})
		if err != nil {
			return nil, errors.New("cannot read the new validation back")
		}
		v.Evidence = "validations/" + filepath.Base(run)
		for name, data := range retained {
			written[v.Evidence+"/"+name] = data
		}
		if err := copyFiles(w, retained, "", v.Evidence); err != nil {
			return nil, err
		}
		outcome, err := revalidatedOutcome(ctx, filepath.Join(w.Path(), filepath.FromSlash(v.Evidence)))
		if err != nil {
			return nil, err
		}
		v.settle(outcome)
		v.compareFindings(filepath.Join(dir, v.Section, "phases", v.Phase, "validations", v.Check), filepath.Join(w.Path(), filepath.FromSlash(v.Evidence)))
		m.Validations = append(m.Validations, v)
	}
	m.Files = index(written)
	raw, err := encode(m)
	if err != nil {
		return nil, err
	}
	if err := w.WriteFile("manifest.json", raw); err != nil {
		return nil, err
	}
	if _, err := w.Seal(nil); err != nil {
		return nil, err
	}
	return OpenConnectedRevalidation(ctx, w.Path(), packetPath)
}

// inside reports whether path resolves to dir or below it.
func inside(dir, path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, resolved)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// declaredValidations lists every declared validation of every retained
// lifecycle with its historical outcome, or why there is none to run again.
func declaredValidations(p *ConnectedPacket) []RevalidatedValidation {
	out := []RevalidatedValidation{}
	for _, section := range connectedSections {
		e, ok := p.evidence[section]
		if !ok {
			continue
		}
		for _, phase := range e.Plan.Document().Test.Phases {
			_, attempted := e.Phases[phase.ID]
			for _, check := range phase.Validations {
				v := RevalidatedValidation{Section: section, Phase: phase.ID, Check: check.ID, Step: check.Step, Status: NotRevalidated, Agreement: AgreementNotCompare, Differences: []string{}}
				switch {
				case !attempted:
					v.Reason = "phase-not-attempted"
				case p.historical[section+"/"+phase.ID+"/"+check.ID] == nil:
					v.Reason = "no-retained-validation"
				default:
					v.Historical = p.historical[section+"/"+phase.ID+"/"+check.ID]
				}
				out = append(out, v)
			}
		}
	}
	return out
}

// requestReason refuses a retained request that is not the plan's declared
// validation of this check: its profiles, requirements and bounds must be the
// ones the plan pinned, so nothing but the retained bytes is run again.
func (v RevalidatedValidation) requestReason(p *ConnectedPacket, raw []byte) string {
	var r fhirvalidator.Request
	if json.Unmarshal(raw, &r, json.RejectUnknownMembers(true)) != nil {
		return "request-not-declared"
	}
	for _, phase := range p.evidence[v.Section].Plan.Document().Test.Phases {
		if phase.ID != v.Phase {
			continue
		}
		for _, check := range phase.Validations {
			if check.ID == v.Check && bytes.Equal(canonicalJSON([]any{check.Profiles, check.Requirements, check.TimeoutMS, check.MaxOutputBytes}), canonicalJSON([]any{r.Profiles, r.Requirements, r.TimeoutMS, r.MaxOutputBytes})) && r.Capability == v.Historical.Capability {
				return ""
			}
		}
	}
	return "request-not-declared"
}

func outcomeOf(e *fhirvalidator.Evidence) *ValidationOutcome {
	r := e.Result()
	o := &ValidationOutcome{Identity: e.Identity(), Capability: r.Capability, Request: r.RequestSHA256, Input: r.InputSHA256, State: r.State, Verdict: string(r.Verdict), Worker: r.Worker.State, Findings: len(r.Findings)}
	if r.RuntimeStatus != nil {
		o.RuntimeState = r.RuntimeStatus.State
	}
	if r.Engine != nil {
		o.EngineVersion = r.Engine.Version
	}
	return o
}

func revalidatedOutcome(ctx context.Context, dir string) (*ValidationOutcome, error) {
	e, err := fhirvalidator.Open(ctx, dir)
	if err != nil {
		return nil, errors.New("the new validation does not verify")
	}
	return outcomeOf(e), nil
}

// semantic is what two validator runs over the same pinned inputs must agree
// on: state, verdict, coverage and every finding, never the worker's private
// diagnostic stream, the engine that hosted it or the job.
func semanticResult(dir string) []byte {
	raw, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		return nil
	}
	var r fhirvalidator.Result
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	return canonicalJSON([]any{r.State, r.Verdict, r.Coverage, r.Findings})
}

// settle records the new outcome and its agreement with the historical one.
func (v *RevalidatedValidation) settle(o *ValidationOutcome) {
	v.Revalidated = o
	v.Status, v.Reason, v.Agreement, v.Differences = NotRevalidated, "", AgreementNotCompare, []string{}
	if o.RuntimeState != "" || o.Worker != "evaluated" {
		v.Reason = o.RuntimeState
		if v.Reason == "" {
			v.Reason = o.Worker
		}
		return
	}
	v.Status = Revalidated
	if v.Historical.Worker != "evaluated" || v.Historical.RuntimeState != "" {
		v.Agreement = AgreementNoOutcome
		return
	}
	v.Agreement = AgreementAgrees
	for _, d := range []struct {
		name string
		a, b string
	}{{"state", v.Historical.State, o.State}, {"verdict", v.Historical.Verdict, o.Verdict}} {
		if d.a != d.b {
			v.Differences = append(v.Differences, d.name)
		}
	}
	if len(v.Differences) > 0 {
		v.Agreement = AgreementDiffers
	}
}

// compareFindings extends an agreement to every finding: two evaluated runs
// agree only when their state, verdict, coverage and findings are the same.
func (v *RevalidatedValidation) compareFindings(historical, revalidated string) {
	if v.Agreement != AgreementAgrees && v.Agreement != AgreementDiffers {
		return
	}
	if !bytes.Equal(semanticResult(historical), semanticResult(revalidated)) {
		v.Agreement = AgreementDiffers
		if len(v.Differences) == 0 {
			v.Differences = []string{"findings"}
		}
	}
}

// OpenConnectedRevalidation verifies a revalidation offline against the packet
// it names: the seal and index, every new validation through its own reader,
// that each ran on exactly the historical input and request, and every claim.
// It starts nothing.
func OpenConnectedRevalidation(ctx context.Context, dir, packetPath string) (*ConnectedRevalidation, error) {
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}
	packet, err := OpenConnected(ctx, packetPath)
	if err != nil {
		return nil, err
	}
	packetDir, err := artifactpath.Directory(packetPath)
	if err != nil {
		return nil, err
	}
	files, err := readConnected(dir, false)
	if err != nil {
		return nil, err
	}
	invalid := errors.New("invalid, incomplete, changed or unsupported connected revalidation")
	raw := files["manifest.json"]
	var stored RevalidationManifest
	if len(raw) > 64<<20 || string(files["identity.sha256"]) != digest(raw)+"\n" || json.Unmarshal(raw, &stored, json.RejectUnknownMembers(true)) != nil || stored.Schema != RevalidationSchema || stored.Scope != revalidationScope || stored.PacketIdentity != packet.Identity {
		return nil, invalid
	}
	if len(indexChanges(stored.Files, files)) > 0 {
		return nil, invalid
	}
	expected := declaredValidations(packet)
	if len(expected) != len(stored.Validations) {
		return nil, invalid
	}
	used := map[string]bool{}
	for i, v := range stored.Validations {
		want := expected[i]
		if v.Section != want.Section || v.Phase != want.Phase || v.Check != want.Check || v.Step != want.Step || !bytes.Equal(canonicalJSON(v.Historical), canonicalJSON(want.Historical)) {
			return nil, invalid
		}
		if v.Evidence == "" {
			// Not run: the reason is one of the closed refusals, or the
			// historical state that left nothing to run; no new outcome.
			if v.Status != NotRevalidated || v.Revalidated != nil || v.Agreement != AgreementNotCompare || len(v.Differences) != 0 || !slices.Contains(revalidationRefusals, v.Reason) || want.Reason != "" && v.Reason != want.Reason || want.Reason == "" && slices.Contains([]string{"phase-not-attempted", "no-retained-validation"}, v.Reason) {
				return nil, invalid
			}
			// What the manifest says was installed must be what the reason
			// states: nothing, or a capability that is not the historical pin.
			if v.Reason == "capability-not-installed" && stored.Capability != "" || v.Reason == "capability-mismatch" && (stored.Capability == "" || want.Historical == nil || stored.Capability == want.Historical.Capability) {
				return nil, invalid
			}
			continue
		}
		if !strings.HasPrefix(v.Evidence, "validations/") || strings.Count(v.Evidence, "/") != 1 || used[v.Evidence] || want.Historical == nil {
			return nil, invalid
		}
		used[v.Evidence] = true
		outcome, err := revalidatedOutcome(ctx, filepath.Join(dir, filepath.FromSlash(v.Evidence)))
		if err != nil || outcome.Input != want.Historical.Input || outcome.Request != want.Historical.Request || outcome.Capability != want.Historical.Capability || outcome.Capability != stored.Capability {
			return nil, invalid
		}
		rebuilt := want
		rebuilt.Evidence = v.Evidence
		rebuilt.settle(outcome)
		rebuilt.compareFindings(filepath.Join(packetDir, want.Section, "phases", want.Phase, "validations", want.Check), filepath.Join(dir, filepath.FromSlash(v.Evidence)))
		if !bytes.Equal(canonicalJSON(rebuilt), canonicalJSON(v)) {
			return nil, invalid
		}
	}
	for name := range files {
		if name == "manifest.json" || name == "identity.sha256" {
			continue
		}
		evidence, _, _ := strings.Cut(strings.TrimPrefix(name, "validations/"), "/")
		if !used["validations/"+evidence] {
			return nil, invalid
		}
	}
	return &ConnectedRevalidation{Manifest: stored, Identity: digest(raw)}, nil
}
