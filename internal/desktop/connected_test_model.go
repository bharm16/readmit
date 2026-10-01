package desktop

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
)

// A connected test is a saved test whose outcome is decided by what the
// systems under test recorded — the engine's output or the application's
// records — rather than by an acknowledgement or the receiver ledger. It is
// authored in the same New test → Setup → Checks → Review flow and saved as
// the same named test object, with its readmit-connected-test-authoring/v1
// member in place of a readmit-test/v1 spec.
//
// The authoring document names project objects, never files: each step's
// source by its case and the evidence identity it was read from, each
// observation by its exact saved version, and the FHIR server by its named
// environment. Compiling it against a named environment yields the
// readmit-connected-test/v5 lifecycle the connected runner executes; the
// compiled lifecycle's definition excludes the environment, so the same saved
// test keeps one approved definition across every environment it is bound to.

// ConnectedTestSchema is the contract of a connected test's authoring member.
const ConnectedTestSchema = "readmit-connected-test-authoring/v1"

// The outcome boundaries a connected test observes.
const (
	EngineOutputBoundary = "engine-output"
	ApplicationBoundary  = "application-state"
)

// Bounds of one authored connected test, inside the lifecycle's own.
const (
	maxConnectedSteps     = 64
	maxConnectedPhases    = 16
	maxConnectedVariables = 64
	maxConnectedChecks    = 128
	maxPhaseObservations  = 8
	maxClauseBytes        = 64 << 10
)

var connectedIdentifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// ConnectedTestDraft is a connected test as its editor holds it.
type ConnectedTestDraft struct {
	Schema   string `json:"schema"`
	Boundary string `json:"boundary"`
	// Server is the named FHIR environment FHIR requests and FHIR
	// observations reach, when it is not the environment the test links.
	Server string `json:"server,omitzero"`
	// Generation is the seed and base time the test's synthetic identifiers
	// and timestamps are derived from: authored facts, kept with the test.
	Generation connectedtest.Generation `json:"generation"`
	Variables  []ConnectedVariable      `json:"variables"`
	Steps      []ConnectedStep          `json:"steps"`
	Phases     []ConnectedPhase         `json:"phases"`
}

// ConnectedVariable is one runtime variable: a literal, a generated
// synthetic identifier, a timestamp relative to the run's base time, or a
// server-assigned identity a FHIR response binds. A response variable is
// used only to address later requests; it never becomes an expected value.
type ConnectedVariable struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitzero"`
	Value     string `json:"value,omitzero"`
	OffsetMS  int64  `json:"offset_ms,omitzero"`
}

// ConnectedSource is the evidence one step sends: a case of the project, the
// identity its evidence verified as when it was chosen, and the occurrence.
type ConnectedSource struct {
	Case       ItemRef `json:"case"`
	Identity   string  `json:"identity"`
	Occurrence string  `json:"occurrence"`
}

// ConnectedStep is one input: an original v2 message or a reviewed FHIR
// request, each from its source, and the steps that must complete first.
type ConnectedStep struct {
	ID     string          `json:"id"`
	After  []string        `json:"after"`
	Source ConnectedSource `json:"source"`
	V2     *ConnectedV2    `json:"v2,omitzero"`
	FHIR   *ConnectedFHIR  `json:"fhir,omitzero"`
}

// ConnectedV2 sends the source message's original bytes unchanged.
type ConnectedV2 struct{}

// ConnectedFHIR is one reviewed FHIR R4 request, described by finite typed
// choices rather than a URL or script: its method, what it addresses, a
// conditional-create identifier, the version a conditional write requires,
// the representation it asks for and the server-assigned identities it binds.
type ConnectedFHIR struct {
	Method   string                          `json:"method"`
	Resource string                          `json:"resource"`
	Target   ConnectedFHIRTarget             `json:"target"`
	IfNone   *ConnectedIdentifier            `json:"if_none_exist,omitzero"`
	IfMatch  string                          `json:"if_match,omitzero"`
	Prefer   string                          `json:"prefer,omitzero"`
	Bind     []connectedtest.ResponseBinding `json:"bind"`
}

// ConnectedFHIRTarget is what a FHIR request addresses: the resource type,
// one resource by a variable holding its identity, or the resources matching
// one business identifier.
type ConnectedFHIRTarget struct {
	Kind       string               `json:"kind"`
	Variable   string               `json:"variable,omitzero"`
	Identifier *ConnectedIdentifier `json:"identifier,omitzero"`
}

// ConnectedIdentifier is one business identifier: its system and value.
type ConnectedIdentifier struct {
	System string `json:"system"`
	Value  string `json:"value"`
}

// ConnectedPhase is one group of steps sent together, the named observations
// read around them, and the checks decided from what those observations
// collected. A phase runs after the phases it depends on.
type ConnectedPhase struct {
	ID           string                          `json:"id"`
	Name         string                          `json:"name"`
	Steps        []string                        `json:"steps"`
	After        []connectedtest.PhaseDependency `json:"after"`
	When         *connectedtest.PhaseCondition   `json:"when,omitzero"`
	Observations []ConnectedPhaseObservation     `json:"observations"`
	Checks       []ConnectedCheck                `json:"checks"`
	Responses    []ConnectedResponseCheck        `json:"responses"`
	Validations  []ConnectedValidationCheck      `json:"validations"`
	Acks         []ConnectedAckCheck             `json:"acknowledgements"`
	// Unsupported are the imported clauses of this phase the editor does not
	// represent, kept exactly as written and compiled as they are.
	Unsupported []ConnectedClause `json:"unsupported,omitzero"`
}

// ConnectedClause is one imported check the editor does not represent: a
// check of the phase's observations ("check"), an acknowledgement check
// ("acknowledgement") or a validation ("validation"), as its exact JSON
// text, with its name and why the editor does not represent it. It is shown
// read-only and decided exactly as an imported lifecycle declared it.
type ConnectedClause struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
	Text   string `json:"text"`
}

// connectedClauseKinds are the clauses a phase keeps without representing.
var connectedClauseKinds = []string{"check", "acknowledgement", "validation"}

// declared is the identity a clause's text declares, or why it has none.
func (c ConnectedClause) declared() (string, error) {
	var declared struct {
		ID string `json:"id"`
	}
	if !slices.Contains(connectedClauseKinds, c.Kind) || len(c.Text) > maxClauseBytes || json.Unmarshal([]byte(c.Text), &declared) != nil {
		return "", errors.New("this imported check cannot be read")
	}
	return declared.ID, nil
}

// ConnectedValidationCheck asks the environment's local FHIR validator to
// validate the resource one FHIR request returned.
type ConnectedValidationCheck struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Step string `json:"step"`
}

// ConnectedPhaseObservation reads one saved version of a named observation
// before or after the phase's steps, as one dataset of the phase.
type ConnectedPhaseObservation struct {
	Dataset     string  `json:"dataset"`
	Observation ItemRef `json:"observation"`
	When        string  `json:"when"`
}

// ConnectedCheck is one typed check of a phase's datasets, with the name a
// person gave it.
type ConnectedCheck struct {
	Name  string                     `json:"name"`
	Check assertion.DatasetAssertion `json:"check"`
}

// ConnectedResponseCheck expects one FHIR request's HTTP outcome class.
type ConnectedResponseCheck struct {
	Name  string                      `json:"name"`
	Check connectedtest.ResponseCheck `json:"check"`
}

// ConnectedAckCheck expects one v2 step's acknowledgement code.
type ConnectedAckCheck struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Step string `json:"step"`
	Code string `json:"code"`
}

// ConnectedColumn is one typed field a named observation projects: its
// name, value type, code system, whether it is a business key, required or
// repeated, and the expected states its source can represent.
type ConnectedColumn struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	CodeSystem string   `json:"code_system,omitzero"`
	Key        bool     `json:"key"`
	Required   bool     `json:"required"`
	Repeated   bool     `json:"repeated"`
	States     []string `json:"states"`
}

// hl7States are the expected states a typed source keeps apart: a value, an
// empty field, an explicit null and a field that is not there.
var hl7States = []string{"present", "empty", "null", "absent"}

// fhirStates are the expected states a FHIR element can be checked for. A
// FHIR reading that is invalid is never an expected value, and FHIR has no
// HL7 empty or null to expect.
var fhirStates = []string{"present", "absent"}

// connectedOperators are the typed dataset operators a connected check uses,
// each with the value types its column must hold, or none for any.
var connectedOperators = map[string][]string{
	"value-equals":    nil,
	"decimal-equals":  {"decimal"},
	"instant-equals":  {"date", "datetime"},
	"value-related":   nil,
	"value-changed":   nil,
	"row-count":       nil,
	"unique-keys":     nil,
	"each-equals":     nil,
	"sequence-equals": nil,
}

func connectedField(parts ...any) string {
	field := "connected"
	for _, part := range parts {
		field += "." + fmt.Sprint(part)
	}
	return field
}

func connectedText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// protocolOf is the protocol one step sends.
func (s ConnectedStep) protocolOf() string {
	switch {
	case s.V2 != nil && s.FHIR == nil:
		return "v2"
	case s.FHIR != nil && s.V2 == nil:
		return "fhir"
	}
	return ""
}

// structureProblems are every problem of the draft's own shape: its
// identifiers, bounds, step and phase dependencies and the references its
// checks make inside it. It reads nothing.
func (d ConnectedTestDraft) structureProblems() []FieldProblem {
	problems := []FieldProblem{}
	add := func(field, problem string) {
		problems = append(problems, FieldProblem{Field: field, Problem: problem})
	}
	if d.Schema != ConnectedTestSchema {
		add("connected", "this connected test declares a contract this release does not read")
	}
	if d.Boundary != EngineOutputBoundary && d.Boundary != ApplicationBoundary {
		add("connected.boundary", "choose Engine output or Application records")
	}
	if _, err := time.Parse(time.RFC3339, d.Generation.BaseTime); err != nil {
		add("connected.generation", "the test's base time is one exact instant")
	}
	if len(d.Variables) > maxConnectedVariables {
		add("connected.variables", "a test declares at most "+strconv.Itoa(maxConnectedVariables)+" variables")
	}
	variables := map[string]ConnectedVariable{}
	for i, v := range d.Variables {
		field := connectedField("variables", i)
		if !connectedIdentifier.MatchString(v.ID) || variables[v.ID].ID != "" {
			add(field, "name each variable once, with lowercase letters, digits and hyphens")
			continue
		}
		variables[v.ID] = v
		switch v.Kind {
		case "literal":
			if !connectedText(v.Value, 256) || strings.ContainsAny(v.Value, "|^~\\&{}") || v.Namespace != "" || v.OffsetMS != 0 {
				add(field, "a literal holds one value without delimiters")
			}
		case "synthetic-id":
			if !connectedIdentifier.MatchString(v.Namespace) || v.Value != "" || v.OffsetMS != 0 {
				add(field, "a synthetic identifier names its namespace")
			}
		case "timestamp":
			if v.Value != "" || v.Namespace != "" || v.OffsetMS < -86400000 || v.OffsetMS > 86400000 {
				add(field, "a timestamp is within one day of the run's base time")
			}
		case "response":
			if v.Value != "" || v.Namespace != "" || v.OffsetMS != 0 {
				add(field, "a server-assigned identity is bound from a response, never written")
			}
		default:
			add(field, "choose Literal, Synthetic identifier, Timestamp or Server-assigned identity")
		}
	}

	if len(d.Steps) == 0 {
		add("connected.steps", "add at least one input")
	}
	if len(d.Steps) > maxConnectedSteps {
		add("connected.steps", "a test sends at most "+strconv.Itoa(maxConnectedSteps)+" inputs")
	}
	steps := map[string]int{}
	for i, s := range d.Steps {
		if !connectedIdentifier.MatchString(s.ID) {
			add(connectedField("steps", i), "this input has no valid identity")
			continue
		}
		if _, taken := steps[s.ID]; taken {
			add(connectedField("steps", i), "this input's identity is used twice")
			continue
		}
		steps[s.ID] = i
	}
	bound := map[string]string{}
	for i, s := range d.Steps {
		field := connectedField("steps", i)
		switch s.protocolOf() {
		case "":
			add(field, "an input is one v2 message or one FHIR request")
		case "fhir":
			for _, problem := range s.FHIR.problems(variables) {
				add(field+".fhir", problem)
			}
			for _, b := range s.FHIR.Bind {
				if holder, taken := bound[b.Variable]; taken && holder != s.ID {
					add(field+".fhir", "the identity "+b.Variable+" is bound by another request")
				}
				bound[b.Variable] = s.ID
			}
		}
		if s.Source.Case.ID == "" || s.Source.Identity == "" || s.Source.Occurrence == "" {
			add(field+".source", "choose the evidence this input sends")
		}
		seen := map[string]bool{}
		for _, after := range s.After {
			if _, known := steps[after]; !known || after == s.ID || seen[after] {
				add(field+".after", "an input waits only for other inputs of this test, each once")
			}
			seen[after] = true
		}
	}
	for i, v := range d.Variables {
		if v.Kind == "response" && variables[v.ID] == v && bound[v.ID] == "" {
			add(connectedField("variables", i), "bind "+v.ID+" from a request's response")
		}
	}
	for _, cycle := range stepCycles(d.Steps) {
		add(connectedField("steps", steps[cycle]), "this input waits on itself through its dependencies")
	}

	if len(d.Phases) == 0 {
		add("connected.phases", "add at least one phase")
	}
	if len(d.Phases) > maxConnectedPhases {
		add("connected.phases", "a test runs at most "+strconv.Itoa(maxConnectedPhases)+" phases")
	}
	phaseOf := map[string]int{}
	phases := map[string]int{}
	checks := map[string]map[string]bool{}
	total := 0
	for p, phase := range d.Phases {
		field := connectedField("phases", p)
		if !connectedIdentifier.MatchString(phase.ID) {
			add(field, "this phase has no valid identity")
			continue
		}
		if _, taken := phases[phase.ID]; taken {
			add(field, "this phase's identity is used twice")
			continue
		}
		if !connectedText(phase.Name, 200) {
			add(field+".name", "name this phase")
		}
		if len(phase.Steps) == 0 {
			add(field+".steps", "a phase sends at least one input")
		}
		protocol, source := "", ""
		for _, id := range phase.Steps {
			i, known := steps[id]
			if !known {
				add(field+".steps", "a phase sends only this test's inputs")
				continue
			}
			if held, taken := phaseOf[id]; taken {
				if held != p {
					add(connectedField("steps", i), "an input belongs to one phase")
				}
				continue
			}
			phaseOf[id] = p
			switch sent := d.Steps[i].protocolOf(); {
			case protocol == "":
				protocol = sent
			case sent != "" && sent != protocol:
				add(field+".steps", "a phase sends v2 messages or FHIR requests, not both; put them in separate phases")
			}
			if d.Steps[i].V2 != nil {
				if source != "" && source != d.Steps[i].Source.Case.ID {
					add(field+".steps", "a phase sends the v2 messages of one case; put another case's messages in their own phase")
				}
				source = d.Steps[i].Source.Case.ID
			}
		}
		dependencies := map[string]bool{}
		for _, dependency := range phase.After {
			if _, earlier := phases[dependency.Phase]; !earlier || dependencies[dependency.Phase] {
				add(field+".after", "a phase waits only for earlier phases, each once")
			}
			if dependency.Requires != "pass" && dependency.Requires != "complete" {
				add(field+".after", "a phase waits for an earlier phase to pass or to complete")
			}
			dependencies[dependency.Phase] = true
		}
		if when := phase.When; when != nil {
			if !dependencies[when.Phase] || !checks[when.Phase][when.Check] || !slices.Contains([]string{"passed", "failed", "skipped"}, when.Outcome) {
				add(field+".when", "a phase runs on the outcome of one check of a phase it waits for")
			}
		}
		datasets := map[string]bool{}
		if len(phase.Observations) == 0 {
			add(field+".observations", "read at least one observation in this phase")
		}
		if len(phase.Observations) > maxPhaseObservations {
			add(field+".observations", "a phase reads at most "+strconv.Itoa(maxPhaseObservations)+" observations")
		}
		after := false
		for o, observed := range phase.Observations {
			ofield := connectedField("phases", p, "observations", o)
			if !connectedIdentifier.MatchString(observed.Dataset) || datasets[observed.Dataset] {
				add(ofield, "this observation's reading has no valid identity")
			}
			datasets[observed.Dataset] = true
			if observed.When != "before" && observed.When != "after" {
				add(ofield, "read an observation before or after the phase's inputs")
			}
			after = after || observed.When == "after"
			if observed.Observation.Kind != ObservationItem || observed.Observation.ID == "" || observed.Observation.Revision == "" {
				add(ofield, "choose a saved observation")
			}
		}
		if !after {
			add(field+".observations", "read at least one observation after this phase's inputs")
		}
		checks[phase.ID] = map[string]bool{}
		names := map[string]bool{}
		named := func(cfield, kind, id, name string) {
			if !connectedIdentifier.MatchString(id) || names[id] {
				add(cfield, "this check has no valid identity")
			}
			names[id] = true
			if !connectedText(name, 200) {
				add(cfield+".name", "name this check")
			}
			checks[phase.ID][kind+":"+id] = true
		}
		for c, clause := range phase.Unsupported {
			cfield := connectedField("phases", p, "unsupported", c)
			id, err := clause.declared()
			if err != nil || id != clause.ID {
				add(cfield, "this imported check cannot be read")
			}
			named(cfield, map[string]string{"check": "typed", "acknowledgement": "wire", "validation": "validation"}[clause.Kind], clause.ID, clause.Name)
		}
		for c, check := range phase.Checks {
			cfield := connectedField("phases", p, "checks", c)
			named(cfield, "typed", check.Check.ID, check.Name)
			for _, problem := range checkShapeProblems(check.Check, datasets) {
				add(cfield, problem)
			}
		}
		for c, check := range phase.Responses {
			cfield := connectedField("phases", p, "responses", c)
			named(cfield, "response", check.Check.ID, check.Name)
			i, known := steps[check.Check.Step]
			if !known || !slices.Contains(phase.Steps, check.Check.Step) || d.Steps[i].FHIR == nil {
				add(cfield, "a response check reads a FHIR request of this phase")
			}
			if !slices.Contains(connectedtest.ResponseOutcomes, check.Check.Outcome) {
				add(cfield, "choose one response outcome")
			}
		}
		for c, check := range phase.Acks {
			cfield := connectedField("phases", p, "acknowledgements", c)
			named(cfield, "wire", check.ID, check.Name)
			i, known := steps[check.Step]
			if !known || !slices.Contains(phase.Steps, check.Step) || d.Steps[i].V2 == nil {
				add(cfield, "an acknowledgement check reads a v2 message of this phase")
			}
			if !slices.Contains([]string{"AA", "AE", "AR", "CA", "CE", "CR"}, check.Code) {
				add(cfield, "choose one acknowledgement code")
			}
		}
		for c, check := range phase.Validations {
			cfield := connectedField("phases", p, "validations", c)
			named(cfield, "validation", check.ID, check.Name)
			i, known := steps[check.Step]
			if !known || !slices.Contains(phase.Steps, check.Step) || d.Steps[i].FHIR == nil || d.Steps[i].FHIR.Method == "DELETE" {
				add(cfield, "a validation check reads the resource a FHIR request of this phase returns")
			}
		}
		total += len(phase.Checks) + len(phase.Responses) + len(phase.Validations) + len(phase.Acks) + len(phase.Unsupported)
		if len(phase.Checks) == 0 && !slices.ContainsFunc(phase.Unsupported, func(c ConnectedClause) bool { return c.Kind == "check" }) {
			add(field+".checks", "add at least one check of what this phase's observations read")
		}
		phases[phase.ID] = p
	}
	if total > maxConnectedChecks {
		add("connected.phases", "a test holds at most "+strconv.Itoa(maxConnectedChecks)+" checks")
	}
	for i, s := range d.Steps {
		if at, known := steps[s.ID]; known && at == i {
			if _, held := phaseOf[s.ID]; !held {
				add(connectedField("steps", i), "put this input in a phase")
			}
		}
	}
	// A dependency across phases is kept only when the waiting phase waits
	// for the phase that sends what it waits for.
	for i, s := range d.Steps {
		p, held := phaseOf[s.ID]
		if !held {
			continue
		}
		for _, after := range s.After {
			q, sent := phaseOf[after]
			if !sent || q == p {
				continue
			}
			if !slices.ContainsFunc(d.Phases[p].After, func(dependency connectedtest.PhaseDependency) bool { return dependency.Phase == d.Phases[q].ID }) {
				add(connectedField("steps", i, "after"), "this input waits for an input of another phase; make its phase wait for that phase")
			}
		}
		if s.FHIR == nil {
			continue
		}
		for _, name := range s.FHIR.variablesUsed() {
			v := variables[name]
			if v.Kind != "response" {
				continue
			}
			holder := bound[name]
			if holder == "" || holder == s.ID {
				continue
			}
			if !slices.Contains(s.After, holder) {
				add(connectedField("steps", i, "after"), "this request uses the identity "+name+"; make it wait for the request that binds it")
				continue
			}
			binding := d.Steps[steps[holder]].FHIR.binding(name)
			if binding.Scope == "phase" && phaseOf[holder] != p {
				add(connectedField("steps", i, "fhir"), "the identity "+name+" is bound for its own phase only")
			}
		}
	}
	return problems
}

// stepCycles are the steps that wait on themselves through their
// dependencies.
func stepCycles(steps []ConnectedStep) []string {
	after := map[string][]string{}
	for _, s := range steps {
		after[s.ID] = s.After
	}
	state := map[string]int{}
	cyclic := map[string]bool{}
	var visit func(id string, path []string)
	visit = func(id string, path []string) {
		switch state[id] {
		case 1:
			at := slices.Index(path, id)
			for _, member := range path[at:] {
				cyclic[member] = true
			}
			return
		case 2:
			return
		}
		state[id] = 1
		for _, next := range after[id] {
			if _, known := after[next]; known {
				visit(next, append(path, id))
			}
		}
		state[id] = 2
	}
	for _, s := range steps {
		visit(s.ID, nil)
	}
	out := []string{}
	for _, s := range steps {
		if cyclic[s.ID] && !slices.Contains(out, s.ID) {
			out = append(out, s.ID)
		}
	}
	return out
}

// problems are the problems of one FHIR request's typed choices.
func (f ConnectedFHIR) problems(variables map[string]ConnectedVariable) []string {
	problems := []string{}
	if !slices.Contains([]string{"POST", "PUT", "DELETE", "GET"}, f.Method) {
		problems = append(problems, "choose the request's method")
	}
	if !fhirResourceName.MatchString(f.Resource) {
		problems = append(problems, "choose the resource type the request addresses")
	}
	switch f.Target.Kind {
	case "type":
		if f.Method != "POST" || f.Target.Variable != "" || f.Target.Identifier != nil {
			problems = append(problems, "only a create addresses the resource type")
		}
	case "instance":
		v, declared := variables[f.Target.Variable]
		if f.Method == "POST" || !declared || v.Kind == "timestamp" || f.Target.Identifier != nil {
			problems = append(problems, "address one resource by a variable that holds its identity")
		}
	case "conditional":
		if f.Method == "POST" || f.Target.Variable != "" || !f.Target.Identifier.valid() {
			problems = append(problems, "address the resources matching one identifier, by its system and value")
		}
	default:
		problems = append(problems, "choose what the request addresses")
	}
	if f.IfNone != nil && (f.Method != "POST" || !f.IfNone.valid()) {
		problems = append(problems, "only a create is conditional on an identifier, by its system and value")
	}
	if f.IfMatch != "" {
		v, declared := variables[f.IfMatch]
		if f.Method != "PUT" && f.Method != "DELETE" || !declared || v.Kind != "response" {
			problems = append(problems, "a write requires a version a response bound")
		}
	}
	if f.Prefer != "" && !slices.Contains([]string{"return=minimal", "return=representation", "return=OperationOutcome"}, f.Prefer) {
		problems = append(problems, "choose the representation the request asks for")
	}
	seen := map[string]bool{}
	for _, b := range f.Bind {
		v, declared := variables[b.Variable]
		if !declared || v.Kind != "response" || seen[b.Variable] || b.From != "logical-id" && b.From != "version-id" || b.Multiplicity != "exactly-one" || b.Scope != "phase" && b.Scope != "lifecycle" {
			problems = append(problems, "bind a declared server-assigned identity once, from the response's logical or version identity")
		}
		seen[b.Variable] = true
	}
	return problems
}

func (i *ConnectedIdentifier) valid() bool {
	return i != nil && connectedText(i.System, 1024) && connectedText(i.Value, 1024) && !strings.ContainsAny(i.System+i.Value, "|{}") && !strings.ContainsAny(i.System, " ")
}

var fhirResourceName = regexp.MustCompile(`^[A-Z][A-Za-z]{1,63}$`)

// variablesUsed are the variables a request's address and headers name.
func (f ConnectedFHIR) variablesUsed() []string {
	used := []string{}
	for _, name := range []string{f.Target.Variable, f.IfMatch} {
		if name != "" && !slices.Contains(used, name) {
			used = append(used, name)
		}
	}
	return used
}

func (f ConnectedFHIR) binding(variable string) connectedtest.ResponseBinding {
	for _, b := range f.Bind {
		if b.Variable == variable {
			return b
		}
	}
	return connectedtest.ResponseBinding{}
}

// checkShapeProblems are the problems of one typed check's shape against the
// datasets its phase reads. Which columns exist is decided against the
// observations' projections separately.
func checkShapeProblems(a assertion.DatasetAssertion, datasets map[string]bool) []string {
	problems := []string{}
	if _, known := connectedOperators[a.Operator]; !known {
		return append(problems, "choose the type of this check")
	}
	selection := func(s assertion.RowSelection) {
		if !datasets[s.Dataset] {
			problems = append(problems, "this check reads an observation this phase does not read")
		}
		if s.Row != "" {
			problems = append(problems, "select the expected record by its business identity, not by its position in an earlier output")
		}
	}
	selection(a.Subject)
	field, expected, other, count, quantifier, sequence := false, false, false, false, false, false
	switch a.Operator {
	case "value-equals", "decimal-equals", "instant-equals":
		field, expected = true, true
	case "value-related", "value-changed":
		field, other = true, true
	case "row-count":
		count = true
	case "each-equals":
		field, expected, quantifier = true, true, true
	case "sequence-equals":
		field, sequence = true, true
	}
	if field != (a.Column != "") {
		problems = append(problems, "choose the field this check reads")
	}
	if expected && (a.Expected == nil || !dataset.ValidExpected(*a.Expected)) {
		problems = append(problems, "enter the expected value and its state")
	}
	if other {
		if a.Other == nil || a.OtherColumn == "" {
			problems = append(problems, "choose the field it is compared with")
		} else {
			selection(*a.Other)
		}
	}
	if count && (a.Count == nil || *a.Count < 0 || *a.Count > 10000) {
		problems = append(problems, "enter a count from 0 to 10000")
	}
	if quantifier && !slices.Contains([]string{"every", "any", "none"}, a.Quantifier) {
		problems = append(problems, "choose which records must hold the value")
	}
	if sequence && a.Sequence == nil {
		problems = append(problems, "enter the expected values in order")
	}
	if a.When != nil {
		selection(a.When.Subject)
		if a.When.Column == "" || !dataset.ValidExpected(a.When.Equals) {
			problems = append(problems, "a condition reads one field and its expected value")
		}
	}
	return problems
}

// ConnectedObservationOffer is one named observation a connected test can
// read: its current saved version, name, protocol, the phases its setup maps
// it to, the FHIR environment it reaches, the typed fields it projects and
// the identity of that projection, and why it cannot be read when it cannot.
type ConnectedObservationOffer struct {
	Ref         ItemRef           `json:"ref"`
	Name        string            `json:"name"`
	Protocol    string            `json:"protocol"`
	Phases      []string          `json:"phases"`
	Environment string            `json:"environment,omitzero"`
	Columns     []ConnectedColumn `json:"columns"`
	Projection  string            `json:"projection,omitzero"`
	// HorizonMS is how long after a run's inputs the observation reads, and
	// Barrier says it completes on a processing barrier instead.
	HorizonMS int64  `json:"horizon_ms"`
	Barrier   bool   `json:"barrier,omitzero"`
	Readable  bool   `json:"readable"`
	Reason    string `json:"reason,omitzero"`
}

// bindingProblems decide every check's fields against the projections of
// the observations its phase reads, by the saved versions the phase pins. An
// observation that cannot be read, or a field its projection does not hold,
// is a binding problem at the check: never a reading of zero.
func (d ConnectedTestDraft) bindingProblems(pinned map[string]ConnectedObservationOffer) []FieldProblem {
	problems := []FieldProblem{}
	for p, phase := range d.Phases {
		columns := map[string][]ConnectedColumn{}
		unavailable := map[string]string{}
		for _, observed := range phase.Observations {
			offer, held := pinned[observed.Observation.ID+"@"+observed.Observation.Revision]
			switch {
			case !held:
				unavailable[observed.Dataset] = "the observation this check reads is no longer in the project; choose another observation"
			case !offer.Readable:
				unavailable[observed.Dataset] = "the observation this check reads cannot be read: " + offer.Reason
			case !slices.Contains(offer.Phases, observed.When) && !slices.Contains(offer.Phases, "both"):
				unavailable[observed.Dataset] = "the observation " + offer.Name + " is not set up to read " + observed.When + " a run"
			default:
				columns[observed.Dataset] = offer.Columns
			}
		}
		column := func(dataset, name string) (ConnectedColumn, string) {
			if reason, held := unavailable[dataset]; held {
				return ConnectedColumn{}, reason
			}
			held, read := columns[dataset]
			if !read {
				return ConnectedColumn{}, ""
			}
			i := slices.IndexFunc(held, func(c ConnectedColumn) bool { return c.Name == name })
			if i < 0 {
				return ConnectedColumn{}, "the observation no longer projects the field " + name + "; choose a field it projects"
			}
			return held[i], ""
		}
		for c, check := range phase.Checks {
			field := connectedField("phases", p, "checks", c)
			report := func(problem string) {
				if problem != "" && !slices.ContainsFunc(problems, func(f FieldProblem) bool { return f.Field == field && f.Problem == problem }) {
					problems = append(problems, FieldProblem{Field: field, Problem: problem})
				}
			}
			a := check.Check
			selection := func(s assertion.RowSelection) {
				if reason, held := unavailable[s.Dataset]; held {
					report(reason)
					return
				}
				for _, filter := range s.Where {
					found, reason := column(s.Dataset, filter.Column)
					report(reason)
					if reason == "" && found.Name != "" {
						if !found.Key {
							report("select records by a business key field; " + filter.Column + " is not one")
						}
						report(connectedExpectedProblem(found, filter.Equals))
					}
				}
			}
			selection(a.Subject)
			if a.Column != "" {
				found, reason := column(a.Subject.Dataset, a.Column)
				report(reason)
				if reason == "" && found.Name != "" {
					if types := connectedOperators[a.Operator]; types != nil && !slices.Contains(types, found.Type) {
						report("this check type needs a " + strings.Join(types, " or ") + " field; " + a.Column + " holds " + found.Type)
					}
					if a.Expected != nil {
						report(connectedExpectedProblem(found, *a.Expected))
					}
					for _, v := range a.Sequence {
						element := found
						element.Repeated = false
						report(connectedExpectedProblem(element, v))
					}
				}
			}
			if a.Other != nil {
				selection(*a.Other)
				if a.OtherColumn != "" {
					_, reason := column(a.Other.Dataset, a.OtherColumn)
					report(reason)
				}
			}
			if a.When != nil {
				selection(a.When.Subject)
				found, reason := column(a.When.Subject.Dataset, a.When.Column)
				report(reason)
				if reason == "" && found.Name != "" {
					report(connectedExpectedProblem(found, a.When.Equals))
				}
			}
		}
	}
	return problems
}

// connectedExpectedProblem is why an expected value does not fit a field: a state its
// source cannot represent, or a value not of its type.
func connectedExpectedProblem(c ConnectedColumn, v dataset.Value) string {
	if !slices.Contains(c.States, v.State) {
		return "the field " + c.Name + " cannot be expected to be " + stateWord(v.State)
	}
	if !dataset.Compatible(dataset.Column{Name: c.Name, Type: c.Type, CodeSystem: c.CodeSystem, Repeated: c.Repeated}, v) {
		return "the expected value of " + c.Name + " is not a " + c.Type + " value it can hold"
	}
	return ""
}

func stateWord(state string) string {
	switch state {
	case "absent":
		return "not present"
	case "":
		return "unset"
	}
	return state
}

// connectedReach is what a connected test reaches now: the environment it
// links and the FHIR server its requests and FHIR observations reach, each by
// identity, with every named environment by identity as it is now.
type connectedReach struct {
	environment string
	server      string
	offers      map[string]ConnectedEnvironmentOffer
}

// invalidations are the checks and inputs a change outside the draft made
// stale: an observation whose current version projects differently than the
// version a phase pins, a source whose evidence no longer verifies as the
// identity it was chosen from, an environment its inputs are sent to that is
// gone or no longer speaks their protocol, and an observation of records of
// another FHIR server than the one the test now reaches. Each is reported
// where it is, with the checks it affects, and the draft keeps everything
// until a person resolves it; nothing is removed. A nil reach is a test a
// suite binds to its own environments.
func (d ConnectedTestDraft) invalidations(current map[string]ConnectedObservationOffer, pinned map[string]ConnectedObservationOffer, sources map[string]string, reach *connectedReach) []FieldProblem {
	problems := []FieldProblem{}
	if reach != nil {
		problems = append(problems, d.reachInvalidations(pinned, *reach)...)
	}
	for p, phase := range d.Phases {
		for o, observed := range phase.Observations {
			now, held := current[observed.Observation.ID]
			was := pinned[observed.Observation.ID+"@"+observed.Observation.Revision]
			if !held || now.Ref.Revision == observed.Observation.Revision {
				continue
			}
			if now.Projection != was.Projection || now.Protocol != was.Protocol {
				problems = append(problems, FieldProblem{Field: connectedField("phases", p, "observations", o), Problem: "the observation " + now.Name + " changed since this test read it; review its checks and use its current version"})
				for c, check := range phase.Checks {
					if checkReads(check.Check, observed.Dataset) {
						problems = append(problems, FieldProblem{Field: connectedField("phases", p, "checks", c), Problem: "the observation " + now.Name + " this check reads changed; review the check before saving"})
					}
				}
			}
		}
	}
	for i, s := range d.Steps {
		identity, held := sources[s.Source.Case.ID]
		if !held {
			problems = append(problems, FieldProblem{Field: connectedField("steps", i, "source"), Problem: "the case this input sends is no longer in the project"})
		} else if identity != s.Source.Identity {
			problems = append(problems, FieldProblem{Field: connectedField("steps", i, "source"), Problem: "the case this input sends changed since it was chosen; choose its evidence again"})
		}
	}
	return problems
}

// reachInvalidations are the inputs and checks a changed environment, FHIR
// server or mapping between them made stale.
func (d ConnectedTestDraft) reachInvalidations(pinned map[string]ConnectedObservationOffer, reach connectedReach) []FieldProblem {
	problems := []FieldProblem{}
	add := func(field, problem string) {
		if !slices.ContainsFunc(problems, func(p FieldProblem) bool { return p.Field == field && p.Problem == problem }) {
			problems = append(problems, FieldProblem{Field: field, Problem: problem})
		}
	}
	server := reach.server
	if linked := reach.offers[reach.environment]; server == "" && linked.Protocol == "fhir" {
		server = reach.environment
	}
	reached := func(id, protocol string) string {
		offer, held := reach.offers[id]
		switch {
		case id == "" || !held:
			return "the environment this input is sent to is no longer in the project; choose the environment again"
		case offer.Protocol != protocol:
			return offer.Name + " no longer connects by " + map[string]string{"v2": "v2", "fhir": "FHIR"}[protocol] + "; choose the environment again"
		}
		return ""
	}
	for i, step := range d.Steps {
		reason := ""
		switch {
		case step.V2 != nil:
			reason = reached(reach.environment, "v2")
		case step.FHIR != nil:
			reason = reached(server, "fhir")
		}
		if reason == "" {
			continue
		}
		add(connectedField("steps", i), reason)
		for p, phase := range d.Phases {
			if slices.Contains(phase.Steps, step.ID) {
				for c := range phase.Checks {
					add(connectedField("phases", p, "checks", c), "the environment this phase sends to changed; review the check before saving")
				}
			}
		}
	}
	for p, phase := range d.Phases {
		for o, observed := range phase.Observations {
			was, held := pinned[observed.Observation.ID+"@"+observed.Observation.Revision]
			if !held || was.Protocol != "fhir" || was.Environment == "" || server == "" || was.Environment == server {
				continue
			}
			now := reach.offers[server].Name
			add(connectedField("phases", p, "observations", o), "the observation "+was.Name+" reads records of "+cmpOr(reach.offers[was.Environment].Name, "another environment")+"; this test now reaches "+now+". Choose an observation of "+now)
			for c, check := range phase.Checks {
				if checkReads(check.Check, observed.Dataset) {
					add(connectedField("phases", p, "checks", c), "the observation "+was.Name+" this check reads observes another FHIR server; review the check before saving")
				}
			}
		}
	}
	return problems
}

// checkReads reports whether a check reads one dataset of its phase.
func checkReads(a assertion.DatasetAssertion, dataset string) bool {
	if a.Subject.Dataset == dataset || a.Other != nil && a.Other.Dataset == dataset || a.When != nil && a.When.Subject.Dataset == dataset {
		return true
	}
	return false
}

// ConnectedTestVocabulary is every finite choice a connected test's editor
// offers, as its readers accept them: the typed check operators with the
// value types each needs, the FHIR methods, representations and response
// outcomes, the acknowledgement codes, the variable kinds, the identities a
// response binds and their scopes, and what a phase waits for.
type ConnectedTestVocabulary struct {
	Boundaries       []string                  `json:"boundaries"`
	Operators        []ConnectedOperatorChoice `json:"operators"`
	Methods          []string                  `json:"methods"`
	Preferences      []string                  `json:"preferences"`
	ResponseOutcomes []string                  `json:"response_outcomes"`
	AckCodes         []string                  `json:"ack_codes"`
	VariableKinds    []string                  `json:"variable_kinds"`
	BindingFroms     []string                  `json:"binding_froms"`
	BindingScopes    []string                  `json:"binding_scopes"`
	PhaseRequires    []string                  `json:"phase_requires"`
	ConditionResults []string                  `json:"condition_results"`
	Quantifiers      []string                  `json:"quantifiers"`
}

// ConnectedOperatorChoice is one typed check operator and the value types
// the field it reads must hold; none means any.
type ConnectedOperatorChoice struct {
	Operator string   `json:"operator"`
	Types    []string `json:"types"`
}

func connectedTestVocabulary() ConnectedTestVocabulary {
	v := ConnectedTestVocabulary{Boundaries: []string{EngineOutputBoundary, ApplicationBoundary}, Operators: []ConnectedOperatorChoice{}, Methods: []string{"POST", "PUT", "DELETE", "GET"},
		Preferences: []string{"return=minimal", "return=representation", "return=OperationOutcome"}, ResponseOutcomes: slices.Clone(connectedtest.ResponseOutcomes),
		AckCodes: []string{"AA", "AE", "AR", "CA", "CE", "CR"}, VariableKinds: []string{"literal", "synthetic-id", "timestamp", "response"}, BindingFroms: []string{"logical-id", "version-id"},
		BindingScopes: []string{"phase", "lifecycle"}, PhaseRequires: []string{"pass", "complete"}, ConditionResults: []string{"passed", "failed", "skipped"}, Quantifiers: []string{"every", "any", "none"}}
	for _, operator := range []string{"value-equals", "decimal-equals", "instant-equals", "value-related", "value-changed", "row-count", "unique-keys", "each-equals", "sequence-equals"} {
		types := connectedOperators[operator]
		if types == nil {
			types = []string{}
		}
		v.Operators = append(v.Operators, ConnectedOperatorChoice{Operator: operator, Types: slices.Clone(types)})
	}
	return v
}
