package desktop

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/fhirevidence"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// An existing readmit-connected-test/v5 lifecycle opens in the connected
// editor as a new test: its sealed plan is read whole, and every clause the
// editor represents becomes the typed draft it compiles back to — each input
// from the case of the project holding its exact bytes, each observation
// from the saved version whose definition it pins. A check the editor does
// not represent is kept read-only as its exact text, decided as declared. A
// lifecycle holding anything the editor can neither represent nor keep is
// refused rather than converted with a loss.

// importedLifecycle reads the sealed plan a chosen file belongs to, when it
// is a connected lifecycle: the plan folder itself, or its test.json or
// flow.json.
func importedLifecycle(file string) (*connectedtest.FlowPlan, bool, error) {
	if !declares(file, connectedtest.FHIRFlowTestSchema) && !declares(file, connectedtest.FHIRFlowPlanSchema) {
		return nil, false, nil
	}
	plan, err := connectedtest.OpenFlowPlan(filepath.Dir(file))
	if err != nil {
		return nil, true, errors.New("this connected lifecycle's plan does not verify: " + err.Error())
	}
	return plan, true, nil
}

// importedConnected opens a sealed connected lifecycle as a connected draft.
func (c *loadedCatalog) importedConnected(result ItemDraftResult, plan *connectedtest.FlowPlan) ItemDraftResult {
	document := plan.Document()
	flow := document.Test
	if reason := keepsWhole(flow); reason != "" {
		result.refuse(Failed, "this lifecycle cannot be edited here without changing it: "+reason)
		return result
	}
	d := ConnectedTestDraft{Schema: ConnectedTestSchema, Boundary: flow.Boundary, Generation: document.Generation, Variables: []ConnectedVariable{}, Steps: []ConnectedStep{}, Phases: []ConnectedPhase{}}
	problems := []FieldProblem{}
	for _, v := range flow.Variables {
		d.Variables = append(d.Variables, ConnectedVariable{ID: v.ID, Kind: v.Kind, Namespace: v.Namespace, Value: v.Value, OffsetMS: v.OffsetMS})
	}
	for i, step := range flow.Steps {
		authored := ConnectedStep{ID: step.ID, After: orEmpty(slices.Clone(step.After))}
		field := connectedField("steps", i, "source")
		switch {
		case step.V2 != nil:
			authored.V2 = &ConnectedV2{}
			if source := c.caseSending(dependency(plan, step.V2.Input), step.V2.Occurrence); source != nil {
				authored.Source = *source
			} else {
				problems = append(problems, FieldProblem{Field: field, Problem: "the project holds no case with this message; add the case and choose its message"})
			}
		case step.Interaction != nil:
			request := importedRequest(*step.Interaction)
			authored.FHIR = &request
			var body []byte
			if step.Interaction.Body != nil {
				body = dependency(plan, *step.Interaction.Body)
			}
			if source := c.evidenceSending(body, request); source != nil {
				authored.Source = *source
			} else {
				problems = append(problems, FieldProblem{Field: field, Problem: "the project holds no FHIR evidence with this request's resource; add it and choose its resource"})
			}
		}
		d.Steps = append(d.Steps, authored)
	}
	for p, phase := range flow.Phases {
		authored := ConnectedPhase{ID: phase.ID, Name: readableName(phase.ID), Steps: slices.Clone(phase.Steps), After: orEmpty(slices.Clone(phase.After)), When: phase.When,
			Observations: []ConnectedPhaseObservation{}, Checks: []ConnectedCheck{}, Responses: []ConnectedResponseCheck{}, Validations: []ConnectedValidationCheck{}, Acks: []ConnectedAckCheck{}}
		datasets := map[string]bool{}
		for o, ds := range phase.Datasets {
			datasets[ds.ID] = true
			observed := ConnectedPhaseObservation{Dataset: ds.ID, When: ds.Phase}
			if ref := c.observationPinning(dependency(plan, *ds.Projection)); ref != nil {
				observed.Observation = *ref
			} else {
				observed.Observation = ItemRef{Kind: ObservationItem}
				problems = append(problems, FieldProblem{Field: connectedField("phases", p, "observations", o), Problem: "the project holds no saved observation with this definition; choose an observation"})
			}
			authored.Observations = append(authored.Observations, observed)
		}
		if phase.Checks.SHA256 != "" {
			var set assertion.DatasetSetDocument
			if err := json.Unmarshal(dependency(plan, phase.Checks), &set); err != nil {
				result.refuse(Failed, "this lifecycle's checks cannot be read")
				return result
			}
			for _, check := range set.Assertions {
				if reasons := checkShapeProblems(check, datasets); len(reasons) > 0 {
					authored.Unsupported = append(authored.Unsupported, clauseOf("check", check.ID, check, reasons[0]))
					continue
				}
				authored.Checks = append(authored.Checks, ConnectedCheck{Name: readableName(check.ID), Check: check})
			}
		}
		for _, check := range phase.Responses {
			authored.Responses = append(authored.Responses, ConnectedResponseCheck{Name: readableName(check.ID), Check: check})
		}
		for _, check := range phase.Validations {
			if reflect.DeepEqual(check, connectedValidation(check.ID, check.Step)) {
				authored.Validations = append(authored.Validations, ConnectedValidationCheck{ID: check.ID, Name: readableName(check.ID), Step: check.Step})
				continue
			}
			authored.Unsupported = append(authored.Unsupported, clauseOf("validation", check.ID, check, "it names profiles or requirements the editor does not set"))
		}
		if phase.Wire != nil {
			var wire assertion.Set
			if err := json.Unmarshal(dependency(plan, phase.Wire.Set), &wire); err != nil {
				result.refuse(Failed, "this lifecycle's acknowledgement checks cannot be read")
				return result
			}
			for _, check := range wire.Assertions {
				if ack, held := importedAck(check, flow.Steps, phase.Steps); held {
					authored.Acks = append(authored.Acks, ack)
					continue
				}
				authored.Unsupported = append(authored.Unsupported, clauseOf("acknowledgement", check.ID, check, "it checks more than one input's acknowledgement code"))
			}
		}
		d.Phases = append(d.Phases, authored)
	}
	problems = append(problems, FieldProblem{Field: "test.environment", Problem: "choose the environment this test runs against"})
	for _, phase := range d.Phases {
		for _, observed := range phase.Observations {
			if read, err := c.connectedObservation(observed.Observation.ID, observed.Observation.Revision); err == nil && read.kind == "fhir" && d.Server == "" {
				d.Server = read.setup.Environment
			}
		}
	}
	result.State, result.New, result.Ref, result.Problems = Completed, true, &ItemRef{Kind: TestItem}, problems
	result.Draft = &ItemDraft{Name: readableName(flow.ID), ConnectedTest: &d, TestLinks: &TestLinks{Schema: TestLinksSchema, Tags: []string{}, Reset: ResetFromEnvironment}}
	result.Test = &TestContext{Messages: []TestMessage{}, Observations: c.testObservations(), Unsupported: []TestClause{}, Proposals: []TestProposal{}, Connected: c.connectedContext(&d)}
	return result
}

// dependency is the exact bytes a lifecycle pins by one reference: its own,
// or a phase plan's.
func dependency(plan *connectedtest.FlowPlan, ref connectedtest.Reference) []byte {
	if held := plan.Dependency(ref); held != nil {
		return held
	}
	for _, phase := range plan.Document().Test.Phases {
		if child := plan.Phase(phase.ID); child != nil {
			if held, ok := child.Files()["dependencies/"+ref.SHA256]; ok {
				return held
			}
		}
	}
	return nil
}

// keepsWhole says what of a lifecycle the editor can neither represent nor
// keep, or nothing.
func keepsWhole(flow connectedtest.FlowTest) string {
	switch {
	case flow.Schema != connectedtest.FHIRFlowTestSchema:
		return "it is not a readmit-connected-test/v5 lifecycle"
	case flow.Schedule != nil:
		return "it sends on a schedule"
	case len(flow.Profiles) > 0:
		return "it pins profiles"
	case flow.Limits != connectedLimits:
		return "it declares its own limits"
	}
	for _, step := range flow.Steps {
		switch {
		case len(step.BusinessKeys) > 0:
			return "an input declares business keys"
		case step.FHIR != nil:
			return "an input is a FHIR request template"
		case step.V2 != nil && len(step.V2.Assignments) > 0:
			return "a message assigns variables"
		case step.Interaction != nil && (step.Interaction.Budget != connectedRequestBudget || step.Interaction.Retry != fhirrest.Retry{MaxAttempts: 1}):
			return "a FHIR request declares its own budget or retries"
		case step.Interaction != nil && step.Interaction.Server != connectedServer:
			return "a FHIR request reaches more than one server"
		}
	}
	for _, phase := range flow.Phases {
		if len(phase.IsolationChanges) > 0 {
			return "a phase changes its isolation"
		}
		if phase.Wire != nil && phase.Wire.Observed != "transport-acks" {
			return "a phase checks acknowledgements an observation reads"
		}
		for _, ds := range phase.Datasets {
			if ds.Completion.Kind != "full-horizon" || ds.Projection == nil {
				return "an observation completes on a processing barrier"
			}
		}
	}
	return ""
}

// connectedValidation is the validation the editor compiles for one step.
func connectedValidation(id, step string) connectedtest.ValidationCheck {
	return connectedtest.ValidationCheck{ID: id, Step: step, Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "not-requested", Invariants: "required", FailSeverities: []string{"fatal", "error"}}, TimeoutMS: 60000, MaxOutputBytes: 1 << 20}
}

// clauseOf keeps one clause the editor does not represent as its exact text.
func clauseOf(kind, id string, value any, reason string) ConnectedClause {
	text, _ := json.Marshal(value, json.Deterministic(true))
	return ConnectedClause{Kind: kind, ID: id, Name: readableName(id), Reason: reason, Text: string(text)}
}

// importedAck is an acknowledgement check the editor represents: one input
// of the phase's acknowledgement code.
func importedAck(check assertion.Assertion, steps []connectedtest.Step, phase []string) (ConnectedAckCheck, bool) {
	field, expected := check.Subject.Field, check.Expected.Field
	if check.Operator != assertion.FieldEquals || field == nil || field.Scope != assertion.ObservedMessages || field.Selector != "MSA-1" || expected == nil || expected.State != "present" || expected.Text == nil {
		return ConnectedAckCheck{}, false
	}
	for _, step := range steps {
		if step.V2 != nil && step.V2.Occurrence == field.Message && slices.Contains(phase, step.ID) {
			return ConnectedAckCheck{ID: check.ID, Name: readableName(check.ID), Step: step.ID, Code: *expected.Text}, true
		}
	}
	return ConnectedAckCheck{}, false
}

// importedRequest is a reviewed FHIR request as the editor's typed choices.
func importedRequest(interaction connectedtest.FHIRInteraction) ConnectedFHIR {
	request := ConnectedFHIR{Method: interaction.Method, Prefer: interaction.Headers.Prefer, Bind: orEmpty(slices.Clone(interaction.Bind))}
	resource, rest, _ := strings.Cut(interaction.Path, "/")
	resource, query, conditional := strings.Cut(resource, "?")
	request.Resource = resource
	switch {
	case conditional:
		values, _ := url.ParseQuery(query)
		system, value, _ := strings.Cut(values.Get("identifier"), "|")
		request.Target = ConnectedFHIRTarget{Kind: "conditional", Identifier: &ConnectedIdentifier{System: system, Value: value}}
	case strings.HasPrefix(rest, "{") && strings.HasSuffix(rest, "}"):
		request.Target = ConnectedFHIRTarget{Kind: "instance", Variable: strings.Trim(rest, "{}")}
	default:
		request.Target = ConnectedFHIRTarget{Kind: "type"}
	}
	if header, held := strings.CutPrefix(interaction.Headers.IfNoneExist, "identifier="); held {
		system, value, _ := strings.Cut(header, "|")
		request.IfNone = &ConnectedIdentifier{System: system, Value: value}
	}
	if variable, held := strings.CutPrefix(interaction.Headers.IfMatch, `W/"{`); held {
		request.IfMatch = strings.TrimSuffix(variable, `}"`)
	}
	return request
}

// caseSending is the case of the project holding a message's exact bytes at
// its occurrence, as a step's source, or nil.
func (c *loadedCatalog) caseSending(raw []byte, occurrence string) *ConnectedSource {
	for _, item := range c.document.Items {
		if item.Kind != string(CaseItem) && item.Kind != string(VariantItem) || item.Entry == "" || c.removed(item) {
			continue
		}
		opened, err := bundle.Open(filepath.Join(c.root, item.Entry))
		if err != nil {
			continue
		}
		if held, err := opened.Raw(occurrence); err == nil && bytes.Equal(held, raw) {
			return &ConnectedSource{Case: c.read(item).Ref, Identity: opened.Identity, Occurrence: occurrence}
		}
	}
	return nil
}

// evidenceSending is the FHIR evidence of the project holding a request's
// resource, as the request sends it, as a step's source, or nil.
func (c *loadedCatalog) evidenceSending(body []byte, request ConnectedFHIR) *ConnectedSource {
	for _, item := range c.document.Items {
		if item.Kind != string(CaseItem) || item.Entry == "" || c.removed(item) {
			continue
		}
		path := filepath.Join(c.root, item.Entry)
		if !regular(filepath.Join(path, fhirevidence.ManifestName)) {
			continue
		}
		read := func(occurrence string) *ConnectedSource {
			source := ConnectedSource{Case: c.read(item).Ref, Occurrence: occurrence}
			found, err := c.connectedSource(source)
			if err != nil && found == nil {
				return nil
			}
			sent := found.raw
			if request.Method == "PUT" && request.Target.Kind == "instance" {
				if sent, err = withIdentity(found.raw, request.Target.Variable); err != nil {
					return nil
				}
			}
			if len(body) > 0 && !bytes.Equal(sent, body) {
				return nil
			}
			source.Identity = found.identity
			return &source
		}
		opened, err := fhirevidence.Open(context.Background(), path)
		if err != nil || opened.Document == nil {
			continue
		}
		for _, resource := range opened.Document.Resources() {
			if source := read(resource.Occurrence); source != nil {
				return source
			}
		}
	}
	return nil
}

// observationPinning is the saved observation version whose definition a
// lifecycle's dataset pins, or nil.
func (c *loadedCatalog) observationPinning(member []byte) *ItemRef {
	for _, item := range c.document.Items {
		if item.Kind != string(ObservationItem) || c.removed(item) {
			continue
		}
		for _, revision := range slices.Backward(item.Revisions) {
			read, err := c.connectedObservation(item.ID, strconv.Itoa(revision.Number))
			if err == nil && bytes.Equal(read.member, member) {
				return &read.ref
			}
		}
	}
	return nil
}
