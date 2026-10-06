package connectedtest

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/testisolation"
)

const FlowTestSchema = "readmit-connected-test/v4"
const FlowPlanSchema = "readmit-execution-plan/v4"

// A scheduled lifecycle is a v4 lifecycle that sends one case of one case
// generation on the delays that generation declared.
const ScheduledFlowTestSchema = "readmit-connected-test/v6"
const ScheduledFlowPlanSchema = "readmit-execution-plan/v6"

// FlowTest authors one test. Phase plans are derived from its selected original
// steps, never supplied as manually executed intermediate artifacts.
type FlowTest struct {
	Schema      string      `json:"schema"`
	Project     string      `json:"project"`
	ID          string      `json:"id"`
	Revision    string      `json:"revision"`
	Environment Environment `json:"environment"`
	Isolation   Reference   `json:"isolation"`
	Boundary    string      `json:"boundary"`
	Steps       []Step      `json:"steps"`
	Variables   []Variable  `json:"variables"`
	Profiles    []Reference `json:"profiles"`
	Phases      []FlowPhase `json:"phases"`
	Limits      Limits      `json:"limits"`
	// Servers exist only in readmit-connected-test/v5.
	Servers []FHIRServer `json:"fhir_servers,omitzero"`
	// Schedule exists only in readmit-connected-test/v6.
	Schedule *Schedule `json:"schedule,omitzero"`
}

// Schedule names the case generation record, by digest, and the case in it,
// by row and variant, whose delays a scheduled lifecycle sends on. Its phases
// are the case's phases and its steps the case's occurrences, in order; the
// delays are read from the record, never from the case's bytes, which another
// variant may share.
type Schedule struct {
	Generation Reference `json:"generation"`
	Row        string    `json:"row"`
	Variant    string    `json:"variant"`
}
type PhaseDependency struct {
	Phase    string `json:"phase"`
	Requires string `json:"requires"`
}
type PhaseCondition struct {
	Phase   string `json:"phase"`
	Check   string `json:"check"`
	Outcome string `json:"outcome"`
}
type KeyBinding struct {
	Dataset string `json:"dataset"`
	Column  string `json:"column"`
}
type WireChecks struct {
	Set              Reference         `json:"set"`
	Acknowledgements map[string]string `json:"acknowledgements,omitzero"`
	Observed         string            `json:"observed"` // dataset ID or transport-acks
	Before           *KeyBinding       `json:"before,omitzero"`
	After            *KeyBinding       `json:"after,omitzero"`
}
type IsolationChange struct {
	Alias      string            `json:"alias"`
	Attributes map[string]string `json:"attributes"`
}
type FlowPhase struct {
	IsolationChanges []IsolationChange `json:"isolation_changes,omitzero"`
	ID               string            `json:"id"`
	Steps            []string          `json:"steps"`
	After            []PhaseDependency `json:"after"`
	When             *PhaseCondition   `json:"when"`
	Datasets         []Dataset         `json:"datasets"`
	Checks           Reference         `json:"checks"`
	Wire             *WireChecks       `json:"wire,omitzero"`
	// Response and validation checks exist only in readmit-connected-test/v5.
	Responses   []ResponseCheck   `json:"responses,omitzero"`
	Validations []ValidationCheck `json:"validations,omitzero"`
}
type FlowDocument struct {
	Schema     string            `json:"schema"`
	Test       FlowTest          `json:"test"`
	Generation Generation        `json:"generation"`
	Phases     map[string]string `json:"phase_plans"`
	// Delays exist only in readmit-execution-plan/v6: each step's delay, in
	// whole milliseconds, after its phase's sending begins.
	Delays  map[string]int64 `json:"delays_ms,omitzero"`
	Members []Member         `json:"members"`
}
type FlowPlan struct {
	document FlowDocument
	phases   map[string]*Plan
	files    map[string][]byte
	identity string
}

func (p *FlowPlan) Identity() string { return p.identity }
func (p *FlowPlan) Document() FlowDocument {
	var d FlowDocument
	raw, _ := encode(p.document)
	_ = json.Unmarshal(raw, &d)
	return d
}
func (p *FlowPlan) Phase(id string) *Plan { return p.phases[id] }

// Schedule is one phase's delays in its steps' order, or nil when the
// lifecycle is not scheduled.
func (p *FlowPlan) Schedule(phase string) []time.Duration {
	if p.document.Delays == nil {
		return nil
	}
	out := []time.Duration{}
	for _, f := range p.document.Test.Phases {
		if f.ID == phase {
			for _, id := range f.Steps {
				out = append(out, time.Duration(p.document.Delays[id])*time.Millisecond)
			}
		}
	}
	return out
}
func (p *FlowPlan) Files() map[string][]byte {
	out := map[string][]byte{}
	for n, b := range p.files {
		out[n] = slices.Clone(b)
	}
	return out
}
func (p *FlowPlan) Dependency(ref Reference) []byte {
	return slices.Clone(p.files["dependencies/"+ref.SHA256])
}

var flowFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected lifecycle plan", AllowedDirectories: []string{"dependencies"}, Nested: []string{"phases"}, RequiredFiles: []string{"flow.json", "test.json", "identity.sha256"}, AllowFile: func(n string) bool {
	return n == "flow.json" || n == "test.json" || n == "identity.sha256" || strings.HasPrefix(n, "dependencies/")
}, MaxFiles: 20000, MaxFileBytes: MaxBytes, MaxBytes: 64 << 20}, Seal: artifactdir.DirectoryHash(FlowPlanSchema)}

func (p *FlowPlan) Write(ctx context.Context, output string) error {
	f := flowFamily
	f.Seal = artifactdir.DirectoryHash(p.document.Schema)
	_, err := artifactdir.Write(ctx, output, f, artifactdir.Durable, p.Files())
	return err
}
func CompileFlow(raw []byte, supplied map[string][]byte, g Generation) (*FlowPlan, error) {
	var d FlowTest
	if len(raw) > MaxBytes || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != FlowTestSchema && d.Schema != FHIRFlowTestSchema && d.Schema != ScheduledFlowTestSchema || !validProjectID(d.Project) || !identifier.MatchString(d.ID) || !short(d.Revision) || d.Environment.Project != d.Project || d.Environment.Classification != "nonproduction" || len(d.Phases) < 1 || len(d.Phases) > 32 || len(d.Steps) < 1 || len(d.Steps) > 256 || len(d.Steps) > d.Limits.MaxSteps || !slices.Contains([]string{"application-state", "engine-output"}, d.Boundary) {
		return nil, invalid
	}
	fhirFlow := d.Schema == FHIRFlowTestSchema
	scheduled := d.Schema == ScheduledFlowTestSchema
	planSchema, childSchema := FlowPlanSchema, PhaseTestSchema
	if fhirFlow {
		planSchema, childSchema = FHIRFlowPlanSchema, PhaseTestSchemaV2
	} else if d.Servers != nil {
		return nil, invalid
	}
	if scheduled {
		planSchema = ScheduledFlowPlanSchema
	}
	if scheduled != (d.Schedule != nil) {
		return nil, invalid
	}
	p := &FlowPlan{document: FlowDocument{Schema: planSchema, Test: d, Generation: g, Phases: map[string]string{}, Members: []Member{}}, phases: map[string]*Plan{}, files: map[string][]byte{}}
	canonical, err := encode(d)
	if err != nil {
		return nil, err
	}
	p.files["test.json"] = canonical
	resolve := func(ref Reference, schema string) ([]byte, error) {
		b, ok := supplied[ref.File]
		if !ok || ref.Project != d.Project || !identifier.MatchString(ref.ID) || ref.Schema != schema || !fs.ValidPath(ref.File) || strings.Contains(ref.File, "\\") || len(b) > MaxBytes || Digest(b) != ref.SHA256 {
			return nil, invalid
		}
		p.files["dependencies/"+ref.SHA256] = slices.Clone(b)
		return b, nil
	}
	isolation, err := resolve(d.Isolation, testisolation.ContractSchema)
	if err != nil {
		return nil, err
	}
	var contract testisolation.Contract
	if json.Unmarshal(isolation, &contract, json.RejectUnknownMembers(true)) != nil || contract.Schema != testisolation.ContractSchema || contract.Project != d.Project || contract.Environment != d.Environment.ID || contract.Revision != d.Environment.Revision {
		return nil, invalid
	}
	steps := map[string]Step{}
	assigned := map[string]string{}
	checkNames := map[string]map[string]bool{}
	for _, s := range d.Steps {
		if !identifier.MatchString(s.ID) || steps[s.ID].ID != "" || s.FHIR != nil || (s.V2 == nil) == (s.Interaction == nil) || !fhirFlow && s.Interaction != nil {
			return nil, invalid
		}
		steps[s.ID] = s
	}
	for _, phase := range d.Phases {
		if !identifier.MatchString(phase.ID) || p.phases[phase.ID] != nil || len(phase.Steps) < 1 || len(phase.Datasets) < 1 || len(phase.After) > 32 || !fhirFlow && (phase.Responses != nil || phase.Validations != nil) {
			return nil, invalid
		}
		// A phase sends either original v2 messages or reviewed FHIR requests.
		for _, id := range phase.Steps {
			if (steps[id].Interaction != nil) != (steps[phase.Steps[0]].Interaction != nil) || steps[id].Interaction != nil && phase.Wire != nil {
				return nil, invalid
			}
		}
		// A v5 wire check reads the phase's actual acknowledgements only; its
		// downstream state is a typed FHIR or application observation.
		if fhirFlow && phase.Wire != nil && (phase.Wire.Observed != "transport-acks" || phase.Wire.Before != nil || phase.Wire.After != nil) {
			return nil, invalid
		}
		aliases := map[string]bool{}
		for _, change := range phase.IsolationChanges {
			if !identifier.MatchString(change.Alias) || aliases[change.Alias] || len(change.Attributes) < 1 || len(change.Attributes) > 64 {
				return nil, invalid
			}
			aliases[change.Alias] = true
			for k, v := range change.Attributes {
				if !short(k) || !short(v) {
					return nil, invalid
				}
			}
		}
		dependencies := map[string]bool{}
		for _, dep := range phase.After {
			if p.phases[dep.Phase] == nil || dependencies[dep.Phase] || !slices.Contains([]string{"pass", "complete"}, dep.Requires) {
				return nil, invalid
			}
			dependencies[dep.Phase] = true
		}
		if when := phase.When; when != nil {
			if p.phases[when.Phase] == nil || !dependencies[when.Phase] || !checkNames[when.Phase][when.Check] || !slices.Contains([]string{"passed", "failed", "skipped"}, when.Outcome) {
				return nil, invalid
			}
		}
		selected := map[string]bool{}
		for _, id := range phase.Steps {
			if steps[id].ID == "" || assigned[id] != "" || selected[id] {
				return nil, invalid
			}
			selected[id] = true
		}
		child := Test{Schema: childSchema, Project: d.Project, ID: d.ID, Revision: d.Revision, Environment: d.Environment, Variables: d.Variables, Profiles: d.Profiles, Setup: Setup{Kind: "operator-declared", Isolation: "parent-owned", Instructions: "Setup is owned and verified by the containing connected lifecycle"}, Datasets: phase.Datasets, Checks: phase.Checks, OperatorVersion: OperatorVersionV2, Limits: d.Limits, Servers: d.Servers, Responses: phase.Responses, Validations: phase.Validations}
		for _, id := range phase.Steps {
			s := steps[id]
			s.After = nil
			for _, pre := range steps[id].After {
				if selected[pre] {
					s.After = append(s.After, pre)
				} else if assigned[pre] == "" || !dependencies[assigned[pre]] {
					return nil, invalid
				}
			}
			child.Steps = append(child.Steps, s)
			assigned[id] = phase.ID
		}
		childRaw, _ := encode(child)
		compiled, e := compile(childRaw, supplied, g, true)
		if e != nil {
			return nil, e
		}
		if !slices.Equal(compiled.document.Order, phase.Steps) {
			return nil, invalid
		}
		p.phases[phase.ID] = compiled
		p.document.Phases[phase.ID] = compiled.Identity()
		for name, b := range compiled.Files() {
			p.files["phases/"+phase.ID+"/"+name] = b
		}
		p.files["phases/"+phase.ID+"/identity.sha256"] = []byte(artifactdir.Identity(compiled.document.Schema, compiled.Files()) + "\n")
		set, e := assertion.DecodeDatasets(compiled.files["dependencies/"+phase.Checks.SHA256])
		if e != nil {
			return nil, e
		}
		checkNames[phase.ID] = map[string]bool{}
		for _, check := range set.Document().Assertions {
			checkNames[phase.ID]["typed:"+check.ID] = true
		}
		for _, check := range phase.Responses {
			checkNames[phase.ID]["response:"+check.ID] = true
		}
		for _, check := range phase.Validations {
			checkNames[phase.ID]["validation:"+check.ID] = true
		}
		if phase.Wire != nil {
			b, e := resolve(phase.Wire.Set, assertion.Schema)
			if e != nil {
				return nil, e
			}
			wire, e := assertion.Decode(b)
			if e != nil {
				return nil, e
			}
			if e = validateWireReferences(*phase.Wire, wire, child.Steps); e != nil {
				return nil, e
			}
			for _, check := range wire.Assertions {
				checkNames[phase.ID]["wire:"+check.ID] = true
			}
			if e = validateWireBindings(*phase.Wire, phase.Datasets, compiled); e != nil {
				return nil, e
			}
		}
	}
	if len(assigned) != len(steps) || fhirFlow && (validateResponseBindings(d, supplied) != nil || validateReceivingWrites(d, p) != nil) {
		return nil, invalid
	}
	if scheduled {
		raw, e := resolve(d.Schedule.Generation, casegen.RecordSchema)
		if e != nil {
			return nil, e
		}
		if p.document.Delays, e = scheduleOf(d, raw, steps); e != nil {
			return nil, e
		}
	}
	observationBudget := 0
	for _, phase := range d.Phases {
		for _, ds := range phase.Datasets {
			observationBudget += ds.Completion.MaxBytes
			if observationBudget > d.Limits.MaxBytes {
				return nil, invalid
			}
		}
	}
	names := make([]string, 0, len(p.files))
	total := 0
	for n, b := range p.files {
		names = append(names, n)
		total += len(b)
	}
	if total > 64<<20 || total > d.Limits.MaxBytes || len(names) > 19998 {
		return nil, invalid
	}
	slices.Sort(names)
	for _, n := range names {
		p.document.Members = append(p.document.Members, Member{Path: n, SHA256: Digest(p.files[n]), Size: len(p.files[n])})
	}
	planBytes, err := encode(p.document)
	if err != nil || len(planBytes) > MaxBytes {
		return nil, invalid
	}
	p.files["flow.json"] = planBytes
	p.identity = Digest(planBytes)
	return p, nil
}

// scheduleOf reads a scheduled lifecycle's delays from the case its schedule
// names. Each phase must be that case's phase and each step that phase's
// occurrence, sent unchanged: its case event and exactly its bytes, with no
// synthetic assignment. A delay the lifecycle cannot hold — below a
// millisecond, or past the deadline with its earlier phases' delays — is
// refused here, before any execution, and never shortened.
func scheduleOf(d FlowTest, raw []byte, steps map[string]Step) (map[string]int64, error) {
	record, err := casegen.ReadRecord(raw)
	if err != nil {
		return nil, invalid
	}
	i := slices.IndexFunc(record.Cases, func(c casegen.Case) bool { return c.Row == d.Schedule.Row && c.Variant == d.Schedule.Variant })
	if i < 0 {
		return nil, errors.New("the scheduled generation holds no case " + d.Schedule.Row + "/" + d.Schedule.Variant)
	}
	c := record.Cases[i]
	if len(c.Phases) != len(d.Phases) {
		return nil, errors.New("a scheduled lifecycle's phases are its case's phases")
	}
	delays := map[string]int64{}
	total := time.Duration(0)
	for n, phase := range d.Phases {
		if len(phase.Steps) != len(c.Phases[n].Occurrences) {
			return nil, errors.New("scheduled phase " + phase.ID + " does not send its case phase's occurrences")
		}
		longest := time.Duration(0)
		for k, id := range phase.Steps {
			ordinal := c.Phases[n].Occurrences[k]
			if ordinal < 1 || ordinal > len(c.Occurrences) {
				return nil, invalid
			}
			o, s := c.Occurrences[ordinal-1], steps[id]
			if s.V2 == nil || len(s.V2.Assignments) != 0 || s.V2.Occurrence != o.CaseEvent || s.V2.Input.SHA256 != o.SHA256 {
				return nil, errors.New("scheduled step " + id + " is not occurrence " + strconv.Itoa(ordinal) + " of its case, unchanged")
			}
			delay, err := time.ParseDuration(o.Delay)
			if err != nil || delay < 0 || delay%time.Millisecond != 0 {
				return nil, errors.New("occurrence " + strconv.Itoa(ordinal) + " declares a delay this lifecycle cannot keep")
			}
			delays[id] = delay.Milliseconds()
			longest = max(longest, delay)
		}
		if total += longest; total >= time.Duration(d.Limits.DeadlineMS)*time.Millisecond {
			return nil, errors.New("the lifecycle's deadline cannot hold its phases' declared delays")
		}
	}
	return delays, nil
}

func validateWireBindings(w WireChecks, definitions []Dataset, p *Plan) error {
	known := map[string]Dataset{}
	for _, d := range definitions {
		known[d.ID] = d
	}
	if w.Observed != "transport-acks" {
		d, ok := known[w.Observed]
		if !ok || d.Phase != "after" || d.Projection == nil {
			return invalid
		}
		projection, e := dataset.DecodeProjection(p.files["dependencies/"+d.Projection.SHA256])
		if e != nil || projection.Format != "hl7" {
			return invalid
		}
	}
	for phase, binding := range map[string]*KeyBinding{"before": w.Before, "after": w.After} {
		if binding == nil {
			continue
		}
		d, ok := known[binding.Dataset]
		if !ok || d.Phase != phase || d.Projection == nil {
			return invalid
		}
		projection, e := dataset.DecodeProjection(p.files["dependencies/"+d.Projection.SHA256])
		if e != nil || !slices.ContainsFunc(projection.Columns, func(c dataset.Column) bool { return c.Name == binding.Column && c.Type == "text" }) {
			return invalid
		}
	}
	return nil
}
func OpenFlowPlan(path string) (*FlowPlan, error) {
	files, err := artifactdir.Read(path, flowFamily.Layout)
	if err != nil {
		return nil, err
	}
	return verifyFlowPlan(files)
}

// VerifyFlowPlan reconstructs all phases from one captured flow snapshot.
func VerifyFlowPlan(captured map[string][]byte) (*FlowPlan, error) {
	files, err := artifactdir.Snapshot(captured, flowFamily.Layout)
	if err != nil {
		return nil, err
	}
	return verifyFlowPlan(files)
}
func verifyFlowPlan(files map[string][]byte) (*FlowPlan, error) {
	var declared FlowDocument
	if json.Unmarshal(files["flow.json"], &declared, json.RejectUnknownMembers(true)) != nil || declared.Schema != FlowPlanSchema && declared.Schema != FHIRFlowPlanSchema && declared.Schema != ScheduledFlowPlanSchema || !sealed(declared.Schema, files) {
		return nil, invalid
	}
	supplied := map[string][]byte{}
	supplied[declared.Test.Isolation.File] = files["dependencies/"+declared.Test.Isolation.SHA256]
	if s := declared.Test.Schedule; s != nil {
		supplied[s.Generation.File] = files["dependencies/"+s.Generation.SHA256]
	}
	for _, phase := range declared.Test.Phases {
		child, err := VerifyPlan(artifactdir.Subtree(files, "phases/"+phase.ID))
		if err != nil || child.Identity() != declared.Phases[phase.ID] || !artifactdir.MatchesSubtree(files, "phases/"+phase.ID, child.document.Schema, artifactdir.Identity(child.document.Schema, child.Files())) {
			return nil, invalid
		}
		for _, ref := range references(child.document.Test) {
			supplied[ref.File] = child.files["dependencies/"+ref.SHA256]
		}
		if ref := child.document.Test.Environment.TargetRevision.Evidence; ref != nil {
			var receipt RevisionEvidence
			if json.Unmarshal(supplied[ref.File], &receipt, json.RejectUnknownMembers(true)) != nil {
				return nil, invalid
			}
			supplied[receipt.Source.File] = child.files["dependencies/"+receipt.Source.SHA256]
		}
		if phase.Wire != nil {
			supplied[phase.Wire.Set.File] = files["dependencies/"+phase.Wire.Set.SHA256]
		}
	}
	rebuilt, err := CompileFlow(files["test.json"], supplied, declared.Generation)
	if err != nil {
		return nil, err
	}
	delete(files, "identity.sha256")
	if rebuilt.document.Schema != declared.Schema || artifactdir.Identity(declared.Schema, rebuilt.Files()) != artifactdir.Identity(declared.Schema, files) {
		return nil, invalid
	}
	return rebuilt, nil
}

// VerifyUnchanged reads the complete bounded tree again and compares it with
// this already validated plan. Identical bytes need no second compilation;
// no disk contents are cached across checks and no changed member is accepted.
func (p *FlowPlan) VerifyUnchanged(path string) error {
	if p == nil {
		return invalid
	}
	files, err := artifactdir.Read(path, flowFamily.Layout)
	if err != nil {
		return err
	}
	if !sealed(p.document.Schema, files) || len(files) != len(p.files)+1 {
		return invalid
	}
	for name, expected := range p.files {
		current, present := files[name]
		if !present || !bytes.Equal(current, expected) {
			return invalid
		}
	}
	return nil
}

func validateWireReferences(w WireChecks, set assertion.Set, steps []Step) error {
	inputs := map[string]bool{}
	counts := map[string]int{}
	ids := map[string]bool{}
	for _, s := range steps {
		inputs[s.V2.Occurrence] = true
		counts[s.V2.Occurrence]++
		ids[s.ID] = true
	}
	ackIDs := map[string]bool{}
	if w.Observed == "transport-acks" {
		if len(w.Acknowledgements) > 0 {
			used := map[string]bool{}
			if len(w.Acknowledgements) != len(steps) {
				return invalid
			}
			for alias, step := range w.Acknowledgements {
				if !occurrence.MatchString(alias) || !ids[step] || used[step] {
					return invalid
				}
				used[step] = true
				ackIDs[alias] = true
			}
		} else {
			for id, n := range counts {
				if n != 1 {
					return invalid
				}
				ackIDs[id] = true
			}
		}
	} else if len(w.Acknowledgements) > 0 {
		return invalid
	}
	field := func(f assertion.FieldRef) bool {
		if f.Scope == "input" {
			return inputs[f.Message]
		}
		return w.Observed != "transport-acks" || ackIDs[f.Message]
	}
	collection := func(scope assertion.RecordScope) bool {
		if scope == "before" {
			return w.Before != nil
		}
		return scope == "after" && w.After != nil
	}
	for _, c := range set.Assertions {
		s := c.Subject
		if s.Field != nil && !field(*s.Field) || s.Pair != nil && (!field(s.Pair.Left) || !field(s.Pair.Right)) || s.Collection != nil && !collection(s.Collection.Scope) || s.Each != nil && !collection(s.Each.Scope) || s.Transition != nil && (!collection(s.Transition.From) || !collection(s.Transition.To)) || c.When != nil && !field(c.When.Field) {
			return invalid
		}
	}
	return nil
}

// validateResponseBindings requires each response variable to be bound by
// exactly one step, and every step that uses one to depend on its binder.
func validateResponseBindings(d FlowTest, supplied map[string][]byte) error {
	phaseOf := map[string]string{}
	for _, phase := range d.Phases {
		for _, id := range phase.Steps {
			phaseOf[id] = phase.ID
		}
	}
	boundBy := map[string]string{}
	scope := map[string]string{}
	for _, s := range d.Steps {
		if s.Interaction == nil {
			continue
		}
		for _, b := range s.Interaction.Bind {
			if boundBy[b.Variable] != "" {
				return invalid
			}
			boundBy[b.Variable], scope[b.Variable] = s.ID, b.Scope
		}
	}
	for _, v := range d.Variables {
		if v.Kind == "response" && boundBy[v.ID] == "" {
			return invalid
		}
	}
	for _, s := range d.Steps {
		var body []byte
		if s.Interaction != nil && s.Interaction.Body != nil {
			body = supplied[s.Interaction.Body.File]
		}
		for _, name := range ResponseVariables(s, d.Variables, body) {
			if !slices.Contains(s.After, boundBy[name]) || scope[name] == "phase" && phaseOf[s.ID] != phaseOf[boundBy[name]] {
				return invalid
			}
		}
	}
	return nil
}

// validateReceivingWrites refuses a reviewed update, patch or delete of a
// resource type that a later v2 phase observes: the integration's own write to
// that type is what the later phase tests, so the test may not make it first.
// Creating prerequisites and cleaning up after the last such phase remain.
func validateReceivingWrites(d FlowTest, p *FlowPlan) error {
	observedFrom := map[string]int{}
	for i, phase := range d.Phases {
		if p.phases[phase.ID].document.Test.Steps[0].V2 == nil {
			continue
		}
		for _, ds := range phase.Datasets {
			if ds.Kind != "fhir-resources" {
				continue
			}
			o, _, _, err := p.phases[phase.ID].ObservationURL(ds.ID)
			if err != nil {
				return err
			}
			observedFrom[o.Resource] = i
		}
	}
	for i, phase := range d.Phases {
		for _, s := range p.phases[phase.ID].document.Test.Steps {
			if s.Interaction == nil || s.Interaction.Method == "GET" || s.Interaction.Method == "POST" {
				continue
			}
			resource, _, _ := strings.Cut(s.Interaction.Path, "/")
			if last, observed := observedFrom[resource]; observed && last > i {
				return invalid
			}
		}
	}
	return nil
}

// PrepareFlowDirectory accepts the same bounded local source layout as legacy
// preparation, with explicit version dispatch performed by the caller.
func PrepareFlowDirectory(directory string, g Generation) (*FlowPlan, error) {
	layout := artifactdir.Layout{Noun: "connected lifecycle inputs", RequiredFiles: []string{"test.json"}, AllowFile: func(string) bool { return true }, AllowDirectory: func(string) bool { return true }, MaxFiles: 2048, MaxFileBytes: MaxBytes, MaxBytes: 2 * MaxBytes}
	files, err := artifactdir.Read(directory, layout)
	if err != nil {
		return nil, err
	}
	return CompileFlow(files["test.json"], files, g)
}
