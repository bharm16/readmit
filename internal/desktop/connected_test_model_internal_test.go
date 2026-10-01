package desktop

import (
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
)

// A mixed v2 → FHIR test: a reschedule message, then a conditional update of
// the appointment it should have produced, each phase reading the
// application's appointments after its inputs.
func mixedConnectedDraft() ConnectedTestDraft {
	count := 1
	booked := dataset.Value{State: "present", Type: "code", Text: "booked", CodeSystem: "http://hl7.org/fhir/appointmentstatus"}
	start := dataset.Value{State: "present", Type: "datetime", Text: "2026-03-02T09:30:00Z", Precision: "second", Timezone: "+00:00"}
	key := dataset.Value{State: "present", Type: "text", Text: "APT-1"}
	where := []assertion.RowFilter{{Column: "key", Equals: key}}
	return ConnectedTestDraft{
		Schema:     ConnectedTestSchema,
		Boundary:   ApplicationBoundary,
		Generation: connectedtest.Generation{Seed: 7, BaseTime: "2026-03-01T12:00:00Z"},
		Variables: []ConnectedVariable{
			{ID: "appointment-id", Kind: "response"},
			{ID: "appointment-version", Kind: "response"},
		},
		Steps: []ConnectedStep{
			{ID: "step-1", After: []string{}, Source: ConnectedSource{Case: ItemRef{Kind: CaseItem, ID: "case-v2"}, Identity: "v2-identity", Occurrence: "s0001-e000001"}, V2: &ConnectedV2{}},
			{ID: "step-2", After: []string{"step-1"}, Source: ConnectedSource{Case: ItemRef{Kind: CaseItem, ID: "case-v2"}, Identity: "v2-identity", Occurrence: "s0001-e000002"}, V2: &ConnectedV2{}},
			{ID: "step-3", After: []string{}, Source: ConnectedSource{Case: ItemRef{Kind: CaseItem, ID: "case-fhir"}, Identity: "fhir-identity", Occurrence: "r1"},
				FHIR: &ConnectedFHIR{Method: "POST", Resource: "Appointment", Target: ConnectedFHIRTarget{Kind: "type"}, IfNone: &ConnectedIdentifier{System: "urn:example:appointment", Value: "APT-1"},
					Bind: []connectedtest.ResponseBinding{{Variable: "appointment-id", From: "logical-id", Multiplicity: "exactly-one", Scope: "lifecycle"}, {Variable: "appointment-version", From: "version-id", Multiplicity: "exactly-one", Scope: "phase"}}}},
			{ID: "step-4", After: []string{"step-3"}, Source: ConnectedSource{Case: ItemRef{Kind: CaseItem, ID: "case-fhir"}, Identity: "fhir-identity", Occurrence: "r1"},
				FHIR: &ConnectedFHIR{Method: "PUT", Resource: "Appointment", Target: ConnectedFHIRTarget{Kind: "instance", Variable: "appointment-id"}, IfMatch: "appointment-version", Bind: []connectedtest.ResponseBinding{}}},
		},
		Phases: []ConnectedPhase{
			{ID: "reschedule", Name: "Reschedule", Steps: []string{"step-1", "step-2"}, After: []connectedtest.PhaseDependency{},
				Observations: []ConnectedPhaseObservation{{Dataset: "appointments", Observation: ItemRef{Kind: ObservationItem, ID: "obs-1", Revision: "2"}, When: "after"}},
				Checks: []ConnectedCheck{
					{Name: "One appointment", Check: assertion.DatasetAssertion{ID: "one", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "appointments", Where: where}, Count: &count}},
					{Name: "New start", Check: assertion.DatasetAssertion{ID: "start", Operator: "instant-equals", Subject: assertion.RowSelection{Dataset: "appointments", Where: where}, Column: "start", Expected: &start}},
					{Name: "Still booked", Check: assertion.DatasetAssertion{ID: "status", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "appointments", Where: where}, Column: "status", Expected: &booked}},
				},
				Responses: []ConnectedResponseCheck{}, Validations: []ConnectedValidationCheck{}, Acks: []ConnectedAckCheck{{ID: "accepted", Name: "Accepted", Step: "step-2", Code: "AA"}}},
			{ID: "update", Name: "Update", Steps: []string{"step-3", "step-4"}, After: []connectedtest.PhaseDependency{{Phase: "reschedule", Requires: "pass"}},
				When:         &connectedtest.PhaseCondition{Phase: "reschedule", Check: "typed:one", Outcome: "passed"},
				Observations: []ConnectedPhaseObservation{{Dataset: "appointments", Observation: ItemRef{Kind: ObservationItem, ID: "obs-1", Revision: "2"}, When: "after"}},
				Checks:       []ConnectedCheck{{Name: "Still one", Check: assertion.DatasetAssertion{ID: "still-one", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "appointments", Where: where}, Count: &count}}},
				Responses:    []ConnectedResponseCheck{{Name: "Updated", Check: connectedtest.ResponseCheck{ID: "updated", Step: "step-4", Outcome: "succeeded"}}},
				Validations:  []ConnectedValidationCheck{},
				Acks:         []ConnectedAckCheck{}},
		},
	}
}

// fhirAppointments is the observation the draft pins: a FHIR search
// projecting the appointment's business key, start and status.
func fhirAppointments(revision, projection string) ConnectedObservationOffer {
	return ConnectedObservationOffer{
		Ref: ItemRef{Kind: ObservationItem, ID: "obs-1", Revision: revision}, Name: "Appointments", Protocol: "fhir", Phases: []string{"both"}, Projection: projection, Readable: true,
		Columns: []ConnectedColumn{
			{Name: "key", Type: "text", Key: true, Required: true, States: fhirStates},
			{Name: "start", Type: "datetime", Required: true, States: fhirStates},
			{Name: "status", Type: "code", CodeSystem: "http://hl7.org/fhir/appointmentstatus", States: fhirStates},
		},
	}
}

func offersByVersion(offers ...ConnectedObservationOffer) map[string]ConnectedObservationOffer {
	out := map[string]ConnectedObservationOffer{}
	for _, offer := range offers {
		out[offer.Ref.ID+"@"+offer.Ref.Revision] = offer
	}
	return out
}

func problemAt(problems []FieldProblem, field, contains string) bool {
	return slices.ContainsFunc(problems, func(p FieldProblem) bool { return p.Field == field && strings.Contains(p.Problem, contains) })
}

func TestAConnectedMixedDraftWithExplicitDependenciesHasNoStructureProblem(t *testing.T) {
	if problems := mixedConnectedDraft().structureProblems(); len(problems) != 0 {
		t.Fatalf("a complete mixed v2 → FHIR draft was refused: %+v", problems)
	}
	if problems := mixedConnectedDraft().bindingProblems(offersByVersion(fhirAppointments("2", "p1"))); len(problems) != 0 {
		t.Fatalf("every check binds a projected field: %+v", problems)
	}
}

func TestConnectedStepDependencyModelRefusesCyclesAndUnplacedOrDoublyPlacedInputs(t *testing.T) {
	d := mixedConnectedDraft()
	d.Steps[0].After = []string{"step-2"}
	problems := d.structureProblems()
	if !problemAt(problems, "connected.steps.0", "waits on itself") || !problemAt(problems, "connected.steps.1", "waits on itself") {
		t.Fatalf("a dependency cycle was not reported at each input in it: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Phases[1].Steps = append(d.Phases[1].Steps, "step-1")
	if problems := d.structureProblems(); !problemAt(problems, "connected.steps.0", "one phase") {
		t.Fatalf("an input in two phases was accepted: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Phases[0].Steps = []string{"step-1"}
	if problems := d.structureProblems(); !problemAt(problems, "connected.steps.1", "put this input in a phase") {
		t.Fatalf("an input in no phase was accepted: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Steps[1].After = []string{"step-1", "step-9"}
	if problems := d.structureProblems(); !problemAt(problems, "connected.steps.1.after", "other inputs") {
		t.Fatalf("a dependency on an unknown input was accepted: %+v", problems)
	}
}

func TestConnectedPhasesKeepOneProtocolAndCrossPhaseDependenciesExplicit(t *testing.T) {
	d := mixedConnectedDraft()
	d.Phases[0].Steps = []string{"step-1", "step-2", "step-3"}
	d.Phases[1].Steps = []string{"step-4"}
	if problems := d.structureProblems(); !problemAt(problems, "connected.phases.0.steps", "not both") {
		t.Fatalf("a phase mixing v2 and FHIR was accepted: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Steps[2].After = []string{"step-2"}
	d.Phases[1].After = []connectedtest.PhaseDependency{}
	d.Phases[1].When = nil
	if problems := d.structureProblems(); !problemAt(problems, "connected.steps.2.after", "make its phase wait") {
		t.Fatalf("a cross-phase input dependency without a phase dependency was accepted: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Phases[1].When = &connectedtest.PhaseCondition{Phase: "reschedule", Check: "typed:missing", Outcome: "passed"}
	if problems := d.structureProblems(); !problemAt(problems, "connected.phases.1.when", "one check") {
		t.Fatalf("a condition on an unknown check was accepted: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Phases[0], d.Phases[1] = d.Phases[1], d.Phases[0]
	if problems := d.structureProblems(); !problemAt(problems, "connected.phases.0.after", "earlier phases") {
		t.Fatalf("a phase waiting for a later phase was accepted: %+v", problems)
	}
}

func TestServerAssignedIdentitiesAreBoundFromAnEarlierResponseOnly(t *testing.T) {
	d := mixedConnectedDraft()
	d.Steps[3].After = []string{}
	if problems := d.structureProblems(); !problemAt(problems, "connected.steps.3.after", "wait for the request that binds it") {
		t.Fatalf("a request used an identity before the request binding it: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Steps[2].FHIR.Bind = d.Steps[2].FHIR.Bind[:1]
	if problems := d.structureProblems(); !problemAt(problems, "connected.variables.1", "bind appointment-version") {
		t.Fatalf("an unbound server-assigned identity was accepted: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Variables = append(d.Variables, ConnectedVariable{ID: "typed-id", Kind: "response", Value: "123"})
	if problems := d.structureProblems(); !problemAt(problems, "connected.variables.2", "never written") {
		t.Fatalf("a server-assigned identity was given a written value: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Steps[3].FHIR.IfMatch = ""
	d.Steps[2].FHIR.IfNone = &ConnectedIdentifier{System: "urn:example", Value: "a|b"}
	if problems := d.structureProblems(); !problemAt(problems, "connected.steps.2.fhir", "system and value") {
		t.Fatalf("a conditional create identifier with a delimiter was accepted: %+v", problems)
	}
}

func TestConnectedChecksSelectRecordsByBusinessIdentityNotAnOutputPosition(t *testing.T) {
	d := mixedConnectedDraft()
	d.Phases[0].Checks[1].Check.Subject.Row = "r000002"
	if problems := d.structureProblems(); !problemAt(problems, "connected.phases.0.checks.1", "business identity") {
		t.Fatalf("a check selecting an output occurrence number was accepted: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Phases[0].Checks[1].Check.Subject.Where = []assertion.RowFilter{{Column: "status", Equals: dataset.Value{State: "present", Type: "code", Text: "booked", CodeSystem: "http://hl7.org/fhir/appointmentstatus"}}}
	if problems := d.bindingProblems(offersByVersion(fhirAppointments("2", "p1"))); !problemAt(problems, "connected.phases.0.checks.1", "business key") {
		t.Fatalf("a record selected by a non-key field was accepted: %+v", problems)
	}
}

func TestAnUnavailableSourceOrProjectionIsABindingErrorNeverAnObservedZero(t *testing.T) {
	d := mixedConnectedDraft()
	if problems := d.bindingProblems(offersByVersion()); !problemAt(problems, "connected.phases.0.checks.0", "no longer in the project") {
		t.Fatalf("a missing observation was not an actionable binding error: %+v", problems)
	}
	unreadable := fhirAppointments("2", "p1")
	unreadable.Readable, unreadable.Reason = false, "its source cannot be read"
	if problems := d.bindingProblems(offersByVersion(unreadable)); !problemAt(problems, "connected.phases.0.checks.0", "cannot be read") {
		t.Fatalf("an unreadable observation was not a binding error: %+v", problems)
	}
	narrowed := fhirAppointments("2", "p1")
	narrowed.Columns = narrowed.Columns[:2]
	if problems := d.bindingProblems(offersByVersion(narrowed)); !problemAt(problems, "connected.phases.0.checks.2", "no longer projects the field status") {
		t.Fatalf("a field the projection dropped was not a binding error: %+v", problems)
	}
	before := fhirAppointments("2", "p1")
	before.Phases = []string{"before"}
	if problems := d.bindingProblems(offersByVersion(before)); !problemAt(problems, "connected.phases.0.checks.0", "not set up to read after") {
		t.Fatalf("an observation read outside its setup's phase was accepted: %+v", problems)
	}
}

func TestExpectedStatesStayTypeAppropriateAndFHIRNeverTakesHL7EmptyOrNull(t *testing.T) {
	d := mixedConnectedDraft()
	d.Phases[0].Checks[2].Check.Expected = &dataset.Value{State: "empty", Type: "code"}
	if problems := d.bindingProblems(offersByVersion(fhirAppointments("2", "p1"))); !problemAt(problems, "connected.phases.0.checks.2", "cannot be expected to be empty") {
		t.Fatalf("a FHIR field accepted the HL7 empty state: %+v", problems)
	}
	d = mixedConnectedDraft()
	d.Phases[0].Checks[1].Check.Operator = "decimal-equals"
	if problems := d.bindingProblems(offersByVersion(fhirAppointments("2", "p1"))); !problemAt(problems, "connected.phases.0.checks.1", "needs a decimal field") {
		t.Fatalf("a number check of a date field was accepted: %+v", problems)
	}
	hl7 := fhirAppointments("2", "p1")
	hl7.Protocol = "typed"
	for i := range hl7.Columns {
		hl7.Columns[i].States = hl7States
	}
	d = mixedConnectedDraft()
	for _, state := range []string{"empty", "null", "absent"} {
		d.Phases[0].Checks[2].Check.Expected = &dataset.Value{State: state, Type: "code"}
		if problems := d.bindingProblems(offersByVersion(hl7)); len(problems) != 0 {
			t.Fatalf("an HL7 field refused the distinct %s state: %+v", state, problems)
		}
	}
}

func TestAChangedObservationInvalidatesOnlyTheChecksThatReadItAndKeepsTheDraft(t *testing.T) {
	d := mixedConnectedDraft()
	kept := mixedConnectedDraft()
	sources := map[string]string{"case-v2": "v2-identity", "case-fhir": "fhir-identity"}
	pinned := offersByVersion(fhirAppointments("2", "p1"))
	current := map[string]ConnectedObservationOffer{"obs-1": fhirAppointments("3", "p1")}
	if problems := d.invalidations(current, pinned, sources, nil); len(problems) != 0 {
		t.Fatalf("a new version with the same projection invalidated checks: %+v", problems)
	}
	current["obs-1"] = fhirAppointments("3", "p2")
	problems := d.invalidations(current, pinned, sources, nil)
	for _, field := range []string{"connected.phases.0.observations.0", "connected.phases.0.checks.0", "connected.phases.0.checks.1", "connected.phases.0.checks.2", "connected.phases.1.checks.0"} {
		if !problemAt(problems, field, "changed") {
			t.Fatalf("a changed projection did not invalidate %s: %+v", field, problems)
		}
	}
	if problemAt(problems, "connected.phases.1.responses.0", "changed") {
		t.Fatal("a response check that reads no observation was invalidated")
	}
	sources["case-fhir"] = "another-identity"
	if problems := d.invalidations(map[string]ConnectedObservationOffer{"obs-1": fhirAppointments("2", "p1")}, pinned, sources, nil); !problemAt(problems, "connected.steps.2.source", "changed since it was chosen") {
		t.Fatalf("a changed source was not reported at its input: %+v", problems)
	}
	if len(d.Phases[0].Checks) != len(kept.Phases[0].Checks) || len(d.Steps) != len(kept.Steps) {
		t.Fatal("deciding invalidations removed something from the draft")
	}
}

func TestAChangedEnvironmentOrFHIRServerInvalidatesTheInputsAndChecksItAffectsAndKeepsTheDraft(t *testing.T) {
	d := mixedConnectedDraft()
	d.Server = "fhir-1"
	sources := map[string]string{"case-v2": "v2-identity", "case-fhir": "fhir-identity"}
	observed := fhirAppointments("2", "p1")
	observed.Environment = "fhir-1"
	pinned := offersByVersion(observed)
	current := map[string]ConnectedObservationOffer{"obs-1": observed}
	reach := connectedReach{environment: "engine", server: "fhir-1", offers: map[string]ConnectedEnvironmentOffer{
		"engine": {Name: "Engine", Protocol: "v2"}, "fhir-1": {Name: "Scheduling FHIR", Protocol: "fhir"}, "fhir-2": {Name: "Staging FHIR", Protocol: "fhir"}}}
	if problems := d.invalidations(current, pinned, sources, &reach); len(problems) != 0 {
		t.Fatalf("an unchanged environment invalidated the draft: %+v", problems)
	}
	// The engine is now a FHIR environment: its v2 inputs and the checks of
	// the phase sending them are stale.
	changed := reach
	changed.offers = maps.Clone(reach.offers)
	changed.offers["engine"] = ConnectedEnvironmentOffer{Name: "Engine", Protocol: "fhir"}
	problems := d.invalidations(current, pinned, sources, &changed)
	for _, field := range []string{"connected.steps.0", "connected.steps.1", "connected.phases.0.checks.0", "connected.phases.0.checks.2"} {
		if !problemAt(problems, field, "") {
			t.Fatalf("a changed environment did not invalidate %s: %+v", field, problems)
		}
	}
	if problemAt(problems, "connected.steps.2", "") || problemAt(problems, "connected.phases.1.checks.0", "") {
		t.Fatalf("a changed v2 environment invalidated the FHIR phase: %+v", problems)
	}
	// Another FHIR server: the observations of the first one's records and
	// every check reading them are stale.
	moved := reach
	moved.server = "fhir-2"
	problems = d.invalidations(current, pinned, sources, &moved)
	for _, field := range []string{"connected.phases.0.observations.0", "connected.phases.0.checks.1", "connected.phases.1.checks.0"} {
		if !problemAt(problems, field, "") {
			t.Fatalf("a changed FHIR server did not invalidate %s: %+v", field, problems)
		}
	}
	// A FHIR server that is gone stales the requests sent to it.
	gone := reach
	gone.offers = maps.Clone(reach.offers)
	delete(gone.offers, "fhir-1")
	if problems := d.invalidations(current, pinned, sources, &gone); !problemAt(problems, "connected.steps.2", "no longer in the project") || !problemAt(problems, "connected.phases.1.checks.0", "") {
		t.Fatalf("a removed FHIR server did not invalidate its requests: %+v", problems)
	}
	if !reflect.DeepEqual(d, func() ConnectedTestDraft { kept := mixedConnectedDraft(); kept.Server = "fhir-1"; return kept }()) {
		t.Fatal("deciding invalidations changed the draft")
	}
}

// A precise expected quantity is its number and its unit, each a field the
// observation projects: the unit is a code of its code system, which a value
// must name exactly, so 5 mg and 5 g, or a unit of another system, differ.
func TestAnExpectedQuantityIsItsNumberAndItsCodedUnit(t *testing.T) {
	d := mixedConnectedDraft()
	where := d.Phases[0].Checks[0].Check.Subject.Where
	observed := fhirAppointments("2", "p1")
	observed.Columns = append(observed.Columns, ConnectedColumn{Name: "dose", Type: "decimal", States: fhirStates}, ConnectedColumn{Name: "dose-unit", Type: "code", CodeSystem: "http://unitsofmeasure.org", States: fhirStates})
	pinned := offersByVersion(observed)
	quantity := func(unit dataset.Value) []ConnectedCheck {
		number := dataset.Value{State: "present", Type: "decimal", Text: "5.0", Precision: "1"}
		return []ConnectedCheck{
			{Name: "Dose", Check: assertion.DatasetAssertion{ID: "dose", Operator: "decimal-equals", Subject: assertion.RowSelection{Dataset: "appointments", Where: where}, Column: "dose", Expected: &number}},
			{Name: "Dose unit", Check: assertion.DatasetAssertion{ID: "dose-unit", Operator: "value-equals", Subject: assertion.RowSelection{Dataset: "appointments", Where: where}, Column: "dose-unit", Expected: &unit}},
		}
	}
	d.Phases[0].Checks = quantity(dataset.Value{State: "present", Type: "code", Text: "mg", CodeSystem: "http://unitsofmeasure.org"})
	if problems := d.bindingProblems(pinned); len(problems) != 0 {
		t.Fatalf("a number with its coded unit was refused: %+v", problems)
	}
	for name, unit := range map[string]dataset.Value{
		"another code system": {State: "present", Type: "code", Text: "mg", CodeSystem: "http://snomed.info/sct"},
		"no code system":      {State: "present", Type: "code", Text: "mg"},
		"a text unit":         {State: "present", Type: "text", Text: "mg"},
	} {
		d.Phases[0].Checks = quantity(unit)
		if problems := d.bindingProblems(pinned); !problemAt(problems, "connected.phases.0.checks.1", "dose-unit") {
			t.Fatalf("a unit with %s was accepted: %+v", name, problems)
		}
	}
}

// A readmit-suite-definition/v1 or /v2 never declares what only a suite of
// connected tests declares; a /v3 definition does.
func TestOnlyASuiteOfConnectedTestsDeclaresAFHIRServerOrAConnectedOverride(t *testing.T) {
	server := SuiteDraft{Tags: []string{}, Concurrency: 1, Tests: []SuiteTestDraft{{ID: "t", Test: ItemRef{Kind: TestItem, ID: "0123456789abcdef01234567", Revision: "1"}, Parameter: "p"}},
		Environments: []SuiteEnvironment{{ID: "qa", Name: "QA", Bindings: []SuiteBinding{{Parameter: "p", Target: ItemRef{Kind: EnvironmentItem, ID: "e"}, Server: &ItemRef{Kind: EnvironmentItem, ID: "f"}}}}}}
	override := SuiteDraft{Tags: []string{}, Concurrency: 1, Tests: server.Tests, Datasets: []SuiteDataset{{ID: "rows", Name: "Rows", Rows: []SuiteDataRow{{ID: "late", ConnectedExpected: map[string]dataset.Value{"phase/check": {State: "absent", Type: "text"}}}}}}}
	for _, draft := range []SuiteDraft{server, override} {
		for schema, valid := range map[string]bool{SuiteDefinitionSchema: false, AuthoredSuiteDefinitionSchema: true} {
			raw, _ := json.Marshal(suiteDefinition{Schema: schema, Draft: draft})
			if _, _, err := decodeSuiteDefinition(raw); (err == nil) != valid {
				t.Fatalf("%s: %v", schema, err)
			}
		}
	}
}
