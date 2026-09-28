package connectedrun_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/testisolation"
	"github.com/bharm16/readmit/internal/testlicense"
)

// The oracle for the appointment integration is authored here, apart from the
// lab and engine: one Appointment per business ID, with the reviewed times.
const (
	bookedAt      = "2026-03-01T09:00:00Z"
	rescheduledAt = "2026-03-02T10:30:00Z"
	statusSystem  = "http://hl7.org/fhir/appointmentstatus"
)

func appointmentMessages() []connectedlab.Message {
	return []connectedlab.Message{
		{Step: "book", Raw: "MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000000||SIU^S12|BOOK|P|2.5.1\rSCH|APPT-1||||||||||" + bookedAt + "\r"},
		{Step: "move", Raw: "MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000001||SIU^S13|MOVE|P|2.5.1\rSCH|APPT-1||||||||||" + rescheduledAt + "\r"},
	}
}

// appointmentFlow sends v2 through the independent engine and observes the
// receiving FHIR store; ACK checks are reported apart from downstream state.
func appointmentFlow(h *connectedlab.Harness) connectedtest.FlowTest {
	query := "identifier=urn%3Areadmit-lab%3Aappointment%7CAPPT-1"
	appointments := func(phase, id, when string) connectedtest.Dataset {
		return h.Observe(phase, id, when, "Appointment", query, "reference-fhir-store",
			connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"),
			connectedlab.FieldColumn("status", "code", statusSystem, false, true, "status"),
			connectedlab.FieldColumn("start", "datetime", "", false, true, "start"),
			connectedlab.IdentityColumn())
	}
	ackSet := func(name, step string) connectedtest.Reference {
		raw := `{"schema":"readmit-assertion-set/v1","name":"Positive ACK","assertions":[{"id":"accepted","operator":"field_equals","subject":{"field":{"scope":"observed","message":"` + h.Occurrences[step] + `","selector":"MSA-1"}},"when":null,"expected":{"field":{"state":"present","text":"AA"}}}]}`
		return h.Ref(name, assertion.Schema, name+".json", []byte(raw))
	}
	ack, moveAck := ackSet("ack-checks", "book"), ackSet("move-ack-checks", "move")
	before, booked := appointments("booking", "prior", "before"), appointments("booking", "appointments", "after")
	moved := appointments("reschedule", "appointments", "after")
	return connectedtest.FlowTest{ID: "appointment-integration", Steps: []connectedtest.Step{h.V2Step("book"), h.V2Step("move", "book")}, Phases: []connectedtest.FlowPhase{
		{ID: "booking", Steps: []string{"book"}, After: []connectedtest.PhaseDependency{}, Datasets: []connectedtest.Dataset{before, booked}, Wire: &connectedtest.WireChecks{Set: ack, Observed: "transport-acks"}, Checks: h.Checks("booking", []connectedtest.Dataset{before, booked},
			connectedlab.RowCount("none-before", "prior", 0), connectedlab.RowCount("one", "appointments", 1), connectedlab.Unique("unique", "appointments"),
			connectedlab.Equals("booked", "appointments", "status", connectedlab.Code("booked", statusSystem)), connectedlab.Instant("start", "appointments", "start", bookedAt))},
		{ID: "reschedule", Steps: []string{"move"}, After: []connectedtest.PhaseDependency{{Phase: "booking", Requires: "pass"}}, Datasets: []connectedtest.Dataset{moved}, Wire: &connectedtest.WireChecks{Set: moveAck, Observed: "transport-acks"}, Checks: h.Checks("reschedule", []connectedtest.Dataset{moved},
			connectedlab.RowCount("one", "appointments", 1), connectedlab.Unique("unique", "appointments"),
			connectedlab.Equals("booked", "appointments", "status", connectedlab.Code("booked", statusSystem)), connectedlab.Instant("start", "appointments", "start", rescheduledAt))},
	}}
}

// labLifecycle runs a whole lab lifecycle test once without race
// instrumentation, through make test-fhir-lab. Every effect re-verifies the
// complete selected configuration, which race instrumentation slows several
// times over; the race suite keeps the defect cycle below instead.
func labLifecycle(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("the FHIR lab lifecycles run uninstrumented through make test-fhir-lab")
	}
	t.Parallel()
}

func TestFHIRFlowV2ToFHIRDetectsDuplicateAppointmentDefectFixAndReintroduction(t *testing.T) {
	modes := []string{"defective", "fixed", "defective"}
	if testing.Short() {
		modes = modes[:1]
	} else {
		t.Parallel()
	}
	h := connectedlab.New(t, "", appointmentMessages()...)
	h.Compile(appointmentFlow(h))
	for i, mode := range modes {
		h.Engine.SetMode(mode)
		r, _ := h.Run([]string{"defect", "fix", "again"}[i])
		reschedule := connectedlab.PhaseResult(r, "reschedule")
		if connectedlab.CheckOutcome(reschedule, "wire:accepted") != assertion.OutcomePassed || connectedlab.CheckOutcome(connectedlab.PhaseResult(r, "booking"), "wire:accepted") != assertion.OutcomePassed {
			t.Fatal("the engine's positive ACKs were not retained as passing transport checks")
		}
		want := assertion.VerdictPass
		if mode == "defective" {
			want = assertion.VerdictFail
			if connectedlab.CheckOutcome(reschedule, "typed:one") != assertion.OutcomeFailed || connectedlab.CheckOutcome(reschedule, "typed:unique") != assertion.OutcomeFailed || h.Lab.Count("Appointment", connectedlab.AppointmentSystem, "APPT-1") != 2 {
				t.Fatal("duplicate Appointment creation was not detected from the receiving FHIR state")
			}
		}
		if r.Verdict != want || r.State != "complete" {
			t.Fatalf("%s: verdict=%s state=%s", mode, r.Verdict, r.State)
		}
	}
}

const (
	nativeBooked    = "2026-04-01T08:00:00Z"
	nativeMoved     = "2026-04-01T13:15:00Z"
	nativeStale     = "2026-04-02T07:00:00Z"
	encounterSystem = "http://hl7.org/fhir/encounter-status"
)

func appointmentBody(id, status, start string) string {
	head := `{"resourceType":"Appointment",`
	if id != "" {
		head += `"id":"` + id + `",`
	}
	return head + `"identifier":[{"system":"urn:readmit-lab:appointment","value":"NATIVE-1"}],"status":"` + status + `","start":"` + start + `","participant":[{"status":"accepted","actor":{"display":"Synthetic patient"}}]}`
}

// bookingBody books with the practitioner and location created as
// prerequisites; their server-assigned IDs are bound in the same phase.
const bookingBody = `{"resourceType":"Appointment","identifier":[{"system":"urn:readmit-lab:appointment","value":"NATIVE-1"}],"status":"booked","start":"` + nativeBooked + `","participant":[{"status":"accepted","actor":{"reference":"Practitioner/{practitioner-id}"}},{"status":"accepted","actor":{"reference":"Location/{location-id}"}}]}`

// nativeFlow issues reviewed FHIR writes and observes both the resource and
// the application's declared downstream Encounter. IDs and versions come only
// from actual responses and only address later requests.
func nativeFlow(h *connectedlab.Harness) connectedtest.FlowTest {
	query := "identifier=urn%3Areadmit-lab%3Aappointment%7CNATIVE-1"
	base := []fhirobserve.Column{connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.FieldColumn("status", "code", statusSystem, false, true, "status"), connectedlab.FieldColumn("start", "datetime", "", false, true, "start"), connectedlab.IdentityColumn()}
	appointments := func(phase string, extra ...fhirobserve.Column) connectedtest.Dataset {
		return h.Observe(phase, "appointments", "after", "Appointment", query, "reference-fhir-store", append(slices.Clone(base), extra...)...)
	}
	encounters := func(phase string) connectedtest.Dataset {
		return h.Observe(phase, "encounters", "after", "Encounter", query, "authoritative-application-api",
			connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"),
			connectedlab.FieldColumn("status", "code", encounterSystem, false, true, "status"),
			connectedlab.FieldColumn("start", "datetime", "", false, true, "period", "start"),
			connectedlab.ReferenceColumn("appointment", "appointment#0", "reference"))
	}
	resource := func(id, typ, system, value string) connectedtest.Dataset {
		return h.Observe("book", id, "after", typ, "identifier="+url.QueryEscape(system+"|"+value), "reference-fhir-store", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.IdentityColumn())
	}
	ifMatch := func(v string) connectedtest.FHIRHeaders { return connectedtest.FHIRHeaders{IfMatch: `W/"{` + v + `}"`} }
	bind := connectedlab.BindID
	book := []connectedtest.Dataset{appointments("book", connectedlab.ReferenceColumn("practitioner", "participant#0", "actor", "reference"), connectedlab.ReferenceColumn("location", "participant#1", "actor", "reference")), encounters("book"), resource("practitioners", "Practitioner", "urn:readmit-lab:npi", "NPI-1"), resource("locations", "Location", "urn:readmit-lab:location", "LOC-1")}
	phase := func(id string, steps []string, after []connectedtest.PhaseDependency, responses []connectedtest.ResponseCheck, checks ...assertion.DatasetAssertion) connectedtest.FlowPhase {
		datasets := []connectedtest.Dataset{appointments(id), encounters(id)}
		return connectedtest.FlowPhase{ID: id, Steps: steps, After: after, Datasets: datasets, Responses: responses, Checks: h.Checks(id, datasets, checks...)}
	}
	return connectedtest.FlowTest{ID: "native-scheduling", Variables: []connectedtest.Variable{{ID: "practitioner-id", Kind: "response"}, {ID: "location-id", Kind: "response"}, {ID: "appointment-id", Kind: "response"}, {ID: "appointment-version", Kind: "response"}, {ID: "moved-version", Kind: "response"}},
		Steps: []connectedtest.Step{
			h.FHIRStep("practitioner", "POST", "Practitioner", `{"resourceType":"Practitioner","identifier":[{"system":"urn:readmit-lab:npi","value":"NPI-1"}],"active":true}`, connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{bind("practitioner-id", "logical-id", "phase")}),
			h.FHIRStep("location", "POST", "Location", `{"resourceType":"Location","identifier":[{"system":"urn:readmit-lab:location","value":"LOC-1"}],"name":"Synthetic clinic"}`, connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{bind("location-id", "logical-id", "phase")}),
			h.FHIRStep("create", "POST", "Appointment", bookingBody, connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{bind("appointment-id", "logical-id", "lifecycle"), bind("appointment-version", "version-id", "lifecycle")}, "practitioner", "location"),
			h.FHIRStep("move", "PUT", "Appointment/{appointment-id}", appointmentBody("{appointment-id}", "booked", nativeMoved), ifMatch("appointment-version"), []connectedtest.ResponseBinding{bind("moved-version", "version-id", "lifecycle")}, "create"),
			h.FHIRStep("stale", "PUT", "Appointment/{appointment-id}", appointmentBody("{appointment-id}", "booked", nativeStale), ifMatch("appointment-version"), nil, "create", "move"),
			h.FHIRStep("cancel", "PUT", "Appointment/{appointment-id}", appointmentBody("{appointment-id}", "cancelled", nativeMoved), ifMatch("moved-version"), nil, "create", "move"),
		},
		Phases: []connectedtest.FlowPhase{
			{ID: "book", Steps: []string{"practitioner", "location", "create"}, Datasets: book, Responses: []connectedtest.ResponseCheck{{ID: "practitioner-created", Step: "practitioner", Outcome: "succeeded"}, {ID: "location-created", Step: "location", Outcome: "succeeded"}, {ID: "created", Step: "create", Outcome: "succeeded"}}, Checks: h.Checks("book", book,
				connectedlab.RowCount("one-appointment", "appointments", 1), connectedlab.Equals("booked", "appointments", "status", connectedlab.Code("booked", statusSystem)), connectedlab.Instant("start", "appointments", "start", nativeBooked),
				connectedlab.Related("with-practitioner", "appointments", "practitioner", "practitioners", "identity"), connectedlab.Related("at-location", "appointments", "location", "locations", "identity"),
				connectedlab.RowCount("one-encounter", "encounters", 1), connectedlab.Equals("planned", "encounters", "status", connectedlab.Code("planned", encounterSystem)), connectedlab.Related("linked", "encounters", "appointment", "appointments", "identity"))},
			phase("reschedule", []string{"move", "stale"}, []connectedtest.PhaseDependency{{Phase: "book", Requires: "pass"}}, []connectedtest.ResponseCheck{{ID: "moved", Step: "move", Outcome: "succeeded"}, {ID: "stale-refused", Step: "stale", Outcome: "conflict"}},
				connectedlab.RowCount("one-appointment", "appointments", 1), connectedlab.Instant("start", "appointments", "start", nativeMoved),
				connectedlab.RowCount("one-encounter", "encounters", 1), connectedlab.Instant("encounter-start", "encounters", "start", nativeMoved), connectedlab.Related("linked", "encounters", "appointment", "appointments", "identity")),
			phase("cancel", []string{"cancel"}, []connectedtest.PhaseDependency{{Phase: "book", Requires: "pass"}, {Phase: "reschedule", Requires: "pass"}}, []connectedtest.ResponseCheck{{ID: "cancelled", Step: "cancel", Outcome: "succeeded"}},
				connectedlab.RowCount("still-present", "appointments", 1), connectedlab.Equals("cancelled", "appointments", "status", connectedlab.Code("cancelled", statusSystem)),
				connectedlab.RowCount("one-encounter", "encounters", 1), connectedlab.Equals("encounter-cancelled", "encounters", "status", connectedlab.Code("cancelled", encounterSystem))),
		}}
}

// nativeBooking is the booking phase alone with its prerequisites.
func nativeBooking(h *connectedlab.Harness) connectedtest.FlowTest {
	flow := nativeFlow(h)
	flow.Steps, flow.Phases, flow.Variables = flow.Steps[:3], flow.Phases[:1], flow.Variables[:4]
	return flow
}

func TestFHIRFlowNativeCreateUpdateConflictAndDeclaredDownstream(t *testing.T) {
	labLifecycle(t)
	h := connectedlab.New(t, "")
	h.Compile(nativeFlow(h))
	for i, downstream := range []string{"encounter-duplicate", "encounter", "encounter-duplicate"} {
		h.Lab.SetDownstream(downstream)
		r, _ := h.Run([]string{"defect", "fix", "again"}[i])
		reschedule := connectedlab.PhaseResult(r, "reschedule")
		if connectedlab.CheckOutcome(reschedule, "response:moved") != assertion.OutcomePassed || connectedlab.CheckOutcome(reschedule, "response:stale-refused") != assertion.OutcomePassed || connectedlab.CheckOutcome(connectedlab.PhaseResult(r, "book"), "response:created") != assertion.OutcomePassed {
			t.Fatal("authorized create/update and the stale-version conflict were not validated as responses")
		}
		if downstream == "encounter-duplicate" {
			if r.Verdict != assertion.VerdictFail || connectedlab.CheckOutcome(reschedule, "typed:one-encounter") != assertion.OutcomeFailed || connectedlab.CheckOutcome(reschedule, "typed:one-appointment") != assertion.OutcomePassed {
				t.Fatal("successful writes hid the application's duplicate downstream Encounter", r.Verdict)
			}
			continue
		}
		if r.Verdict != assertion.VerdictPass || r.State != "complete" || connectedlab.CheckOutcome(connectedlab.PhaseResult(r, "cancel"), "typed:still-present") != assertion.OutcomePassed {
			t.Fatal("corrected downstream behaviour did not pass", r.Verdict, r.State)
		}
	}
	claims := map[string]string{}
	for _, claim := range connectedrunClaims(t, h) {
		claims[claim.Dataset] = claim.Boundary
		if claim.Boundary == "reference-fhir-store" && !strings.Contains(claim.Meaning, "not evidence that an EHR") {
			t.Fatal("a reference FHIR store observation is not limited to its boundary", claim.Meaning)
		}
	}
	if claims["appointments"] != "reference-fhir-store" || claims["encounters"] != "authoritative-application-api" {
		t.Fatal("declared observation boundaries are missing from the result", claims)
	}
}

func connectedrunClaims(t *testing.T, h *connectedlab.Harness) []connectedrun.FlowClaim {
	t.Helper()
	r, err := connectedrun.OpenFlow(t.Context(), filepath.Join(h.Root, "fix"))
	if err != nil {
		t.Fatal(err)
	}
	return r.Qualification
}

// resultOracle is the laboratory's expected results, authored in a separate
// fixture from the reviewed interface contract, never from the engine.
type resultOracle struct {
	StatusSystem       string `json:"status_system"`
	ReportStatusSystem string `json:"report_status_system"`
	Unit               string `json:"unit"`
	UnitSystem         string `json:"unit_system"`
	Final              struct {
		Status string `json:"status"`
		Value  string `json:"value"`
	} `json:"final"`
	Corrected struct {
		Status string `json:"status"`
		Value  string `json:"value"`
	} `json:"corrected"`
}

func loadResultOracle(t *testing.T) resultOracle {
	t.Helper()
	var oracle resultOracle
	flowContractRead(t, filepath.Join("testdata", "fhir-result-oracle.json"), &oracle)
	return oracle
}

func resultMessages() []connectedlab.Message {
	return []connectedlab.Message{
		{Step: "result", Raw: "MSH|^~\\&|LIS|LAB|ENGINE|LAB|20260101000000||ORU^R01|RESULT|P|2.5.1\rPID|1||MRN-7\rOBR|1|PLACER-7|FILLER-7|2345-7\rOBX|1|NM|2345-7||7.10|mmol/L|||||F\r"},
		{Step: "correction", Raw: "MSH|^~\\&|LIS|LAB|ENGINE|LAB|20260101000001||ORU^R01|CORRECT|P|2.5.1\rPID|1||MRN-7\rOBR|1|PLACER-7|FILLER-7|2345-7\rOBX|1|NM|2345-7||7.30|mmol/L|||||C\r"},
	}
}

// resultFlow places an order through reviewed FHIR setup requests, sends the
// v2 results through the engine, checks the linked Observation and report,
// and removes its own prerequisites under separate setup-action authority.
func resultFlow(h *connectedlab.Harness, oracle resultOracle) connectedtest.FlowTest {
	patients := func(phase string) connectedtest.Dataset {
		return h.Observe(phase, "patients", "after", "Patient", "identifier=urn%3Areadmit-lab%3Amrn%7CMRN-7", "reference-fhir-store",
			connectedlab.FieldColumn("mrn", "text", "", true, true, "identifier#0", "value"), connectedlab.IdentityColumn())
	}
	orders := func(phase string) connectedtest.Dataset {
		return h.Observe(phase, "orders", "after", "ServiceRequest", "identifier=urn%3Areadmit-lab%3Aplacer%7CPLACER-7", "reference-fhir-store",
			connectedlab.FieldColumn("placer", "text", "", true, true, "identifier#0", "value"), connectedlab.ReferenceColumn("subject", "subject", "reference"), connectedlab.IdentityColumn())
	}
	results := func(phase string) connectedtest.Dataset {
		return h.Observe(phase, "results", "after", "Observation", "identifier=urn%3Areadmit-lab%3Aresult%7CFILLER-7&_include=Observation%3Asubject", "reference-fhir-store",
			connectedlab.FieldColumn("filler", "text", "", true, true, "identifier#0", "value"),
			connectedlab.FieldColumn("status", "code", oracle.StatusSystem, false, true, "status"),
			connectedlab.FieldColumn("value", "decimal", "", false, true, "valueQuantity", "value"),
			connectedlab.FieldColumn("unit", "code", oracle.UnitSystem, false, true, "valueQuantity", "code"),
			connectedlab.ReferenceColumn("subject", "subject", "reference"), connectedlab.ReferenceColumn("order", "basedOn#0", "reference"), connectedlab.IdentityColumn())
	}
	reports := func(phase string) connectedtest.Dataset {
		return h.Observe(phase, "reports", "after", "DiagnosticReport", "identifier=urn%3Areadmit-lab%3Aresult%7CFILLER-7", "reference-fhir-store",
			connectedlab.FieldColumn("filler", "text", "", true, true, "identifier#0", "value"),
			connectedlab.FieldColumn("status", "code", oracle.ReportStatusSystem, false, true, "status"),
			connectedlab.ReferenceColumn("result", "result#0", "reference"), connectedlab.ReferenceColumn("order", "basedOn#0", "reference"))
	}
	decimal := func(text string) dataset.Value {
		return dataset.Value{State: "present", Type: "decimal", Text: text, Precision: strconv.Itoa(len(text) - strings.IndexByte(text, '.') - 1)}
	}
	linked := func(phase, status, value string) connectedtest.FlowPhase {
		datasets := []connectedtest.Dataset{results(phase), reports(phase), patients(phase), orders(phase)}
		return connectedtest.FlowPhase{ID: phase, Steps: []string{phase}, Datasets: datasets, Checks: h.Checks(phase, datasets,
			connectedlab.RowCount("one-result", "results", 1), connectedlab.Equals("status", "results", "status", connectedlab.Code(status, oracle.StatusSystem)), connectedlab.Equals("value", "results", "value", decimal(value)), connectedlab.Equals("unit", "results", "unit", connectedlab.Code(oracle.Unit, oracle.UnitSystem)),
			connectedlab.Related("subject", "results", "subject", "patients", "identity"), connectedlab.Related("order", "results", "order", "orders", "identity"),
			connectedlab.RowCount("one-report", "reports", 1), connectedlab.Equals("report-status", "reports", "status", connectedlab.Code(status, oracle.ReportStatusSystem)), connectedlab.Related("reports-result", "reports", "result", "results", "identity"), connectedlab.Related("report-order", "reports", "order", "orders", "identity"))}
	}
	order := connectedtest.FlowPhase{ID: "order", Steps: []string{"patient", "order"}, Responses: []connectedtest.ResponseCheck{{ID: "patient-created", Step: "patient", Outcome: "succeeded"}, {ID: "order-created", Step: "order", Outcome: "succeeded"}}}
	order.Datasets = []connectedtest.Dataset{patients("order"), orders("order")}
	order.Checks = h.Checks("order", order.Datasets, connectedlab.RowCount("one-patient", "patients", 1), connectedlab.RowCount("one-order", "orders", 1), connectedlab.Related("order-subject", "orders", "subject", "patients", "identity"))
	result, correction := linked("result", oracle.Final.Status, oracle.Final.Value), linked("correction", oracle.Corrected.Status, oracle.Corrected.Value)
	result.After = []connectedtest.PhaseDependency{{Phase: "order", Requires: "pass"}}
	correction.After = []connectedtest.PhaseDependency{{Phase: "result", Requires: "complete"}}
	cleanup := connectedtest.FlowPhase{ID: "cleanup", Steps: []string{"remove-order", "remove-patient"}, After: []connectedtest.PhaseDependency{{Phase: "order", Requires: "complete"}, {Phase: "correction", Requires: "complete"}}, Responses: []connectedtest.ResponseCheck{{ID: "order-removed", Step: "remove-order", Outcome: "succeeded"}, {ID: "patient-removed", Step: "remove-patient", Outcome: "succeeded"}}}
	cleanup.Datasets = []connectedtest.Dataset{patients("cleanup"), orders("cleanup")}
	cleanup.Checks = h.Checks("cleanup", cleanup.Datasets, connectedlab.RowCount("no-patient", "patients", 0), connectedlab.RowCount("no-order", "orders", 0))
	bind := connectedlab.BindID
	return connectedtest.FlowTest{ID: "order-result", Variables: []connectedtest.Variable{{ID: "patient-id", Kind: "response"}, {ID: "order-id", Kind: "response"}},
		Steps: []connectedtest.Step{
			h.FHIRStep("patient", "POST", "Patient", `{"resourceType":"Patient","identifier":[{"system":"urn:readmit-lab:mrn","value":"MRN-7"}],"active":true}`, connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{bind("patient-id", "logical-id", "lifecycle")}),
			h.FHIRStep("order", "POST", "ServiceRequest", `{"resourceType":"ServiceRequest","identifier":[{"system":"urn:readmit-lab:placer","value":"PLACER-7"}],"status":"active","intent":"order","code":{"coding":[{"system":"http://loinc.org","code":"2345-7"}]},"subject":{"reference":"Patient/{patient-id}"}}`, connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{bind("order-id", "logical-id", "lifecycle")}, "patient"),
			h.V2Step("result"), h.V2Step("correction", "result"),
			h.FHIRStep("remove-order", "DELETE", "ServiceRequest/{order-id}", "", connectedtest.FHIRHeaders{}, nil, "order"),
			h.FHIRStep("remove-patient", "DELETE", "Patient/{patient-id}", "", connectedtest.FHIRHeaders{}, nil, "patient", "remove-order"),
		},
		Phases: []connectedtest.FlowPhase{order, result, correction, cleanup}}
}

func TestFHIRFlowOrderResultOracleCatchesReferenceRepeatStatusAndPrecisionDefects(t *testing.T) {
	labLifecycle(t)
	oracle := loadResultOracle(t)
	h := connectedlab.New(t, "", resultMessages()...)
	full := resultFlow(h, oracle)
	h.Compile(full)
	for _, tc := range []struct{ mode, phase, check string }{{"fixed", "", ""}, {"repeated", "correction", "typed:one-result"}, {"not-corrected", "correction", "typed:status"}} {
		h.Engine.SetMode(tc.mode)
		r, output := h.Run(tc.mode)
		if tc.mode == "fixed" {
			if r.Verdict != assertion.VerdictPass || r.State != "complete" {
				t.Fatalf("correct engine did not pass: %s %s %+v", r.Verdict, r.State, r.Phases)
			}
			// The searched Patient arrived as an included resource and was not
			// counted as a second Observation.
			if !retainedIncludes(t, filepath.Join(output, "phases", "result"), "results") {
				t.Fatal("the result search did not retain its included subject")
			}
			// Cleanup removed exactly the prerequisites this test created, under
			// the separate setup-action authority its plan declared.
			if h.Lab.Count("ServiceRequest", connectedlab.OrderSystem, "PLACER-7") != 0 || h.Lab.Count("Patient", connectedlab.PatientSystem, "MRN-7") != 0 || h.Lab.Count("ServiceRequest", connectedlab.OrderSystem, "DECOY") != 1 {
				t.Fatal("cleanup did not remove exactly the test's own prerequisites")
			}
			continue
		}
		if r.Verdict != assertion.VerdictFail || connectedlab.CheckOutcome(connectedlab.PhaseResult(r, tc.phase), tc.check) != assertion.OutcomeFailed {
			t.Fatalf("%s was not caught by %s: %s %+v", tc.mode, tc.check, r.Verdict, connectedlab.PhaseResult(r, tc.phase))
		}
	}
	// Each result defect alone, against the same unchanged expectations.
	short := full
	short.Steps, short.Phases = short.Steps[:3], short.Phases[:2]
	h.Compile(short)
	for _, tc := range []struct{ mode, check string }{{"wrong-subject", "typed:subject"}, {"wrong-order", "typed:order"}, {"precision", "typed:value"}, {"unit", "typed:unit"}} {
		h.Engine.SetMode(tc.mode)
		r, _ := h.Run(tc.mode)
		result := connectedlab.PhaseResult(r, "result")
		failed := 0
		for _, c := range result.Checks {
			if c.Outcome == assertion.OutcomeFailed {
				failed++
			}
		}
		if r.Verdict != assertion.VerdictFail || connectedlab.CheckOutcome(result, tc.check) != assertion.OutcomeFailed || tc.mode != "wrong-order" && failed != 1 {
			t.Fatalf("%s was not caught by %s alone: %s %+v", tc.mode, tc.check, r.Verdict, result)
		}
	}
}

// retainedIncludes reports whether the final retained search of a dataset
// carried included entries alongside its matches.
func retainedIncludes(t *testing.T, phase, id string) bool {
	t.Helper()
	var manifest connectedrun.FHIRPhaseRun
	flowContractRead(t, filepath.Join(phase, "manifest.json"), &manifest)
	var result struct {
		Search struct {
			Includes int `json:"includes"`
			Matches  int `json:"matches"`
		} `json:"search"`
	}
	flowContractRead(t, filepath.Join(phase, manifest.Observations[id], "http", "result.json"), &result)
	return result.Search.Includes > 0 && result.Search.Matches == 1
}

// bookingFlow is the booking phase alone, for failure scenarios.
func bookingFlow(h *connectedlab.Harness) connectedtest.FlowTest {
	flow := appointmentFlow(h)
	flow.Steps, flow.Phases = flow.Steps[:1], flow.Phases[:1]
	return flow
}

// lookupFlow binds an ID by business-identifier search, then updates it.
func lookupFlow(h *connectedlab.Harness) connectedtest.FlowTest {
	query := "identifier=urn%3Areadmit-lab%3Aappointment%7CNATIVE-1"
	appointments := func(phase string) connectedtest.Dataset {
		return h.Observe(phase, "appointments", "after", "Appointment", query, "reference-fhir-store", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.FieldColumn("start", "datetime", "", false, true, "start"))
	}
	find, update := appointments("find"), appointments("update")
	return connectedtest.FlowTest{ID: "lookup-update", Variables: []connectedtest.Variable{{ID: "appointment-id", Kind: "response"}}, Steps: []connectedtest.Step{
		h.FHIRStep("find", "GET", "Appointment?"+query, "", connectedtest.FHIRHeaders{}, []connectedtest.ResponseBinding{connectedlab.BindID("appointment-id", "logical-id", "lifecycle")}),
		h.FHIRStep("update", "PUT", "Appointment/{appointment-id}", appointmentBody("{appointment-id}", "booked", nativeMoved), connectedtest.FHIRHeaders{IfMatch: `W/"1"`}, nil, "find"),
	}, Phases: []connectedtest.FlowPhase{
		{ID: "find", Steps: []string{"find"}, Datasets: []connectedtest.Dataset{find}, Responses: []connectedtest.ResponseCheck{{ID: "found", Step: "find", Outcome: "succeeded"}}, Checks: h.Checks("find", []connectedtest.Dataset{find}, connectedlab.RowCount("one", "appointments", 1))},
		{ID: "update", Steps: []string{"update"}, After: []connectedtest.PhaseDependency{{Phase: "find", Requires: "complete"}}, Datasets: []connectedtest.Dataset{update}, Checks: h.Checks("update", []connectedtest.Dataset{update}, connectedlab.Instant("moved", "appointments", "start", nativeMoved))},
	}}
}

func TestFHIRFlowFailuresNeverProduceAFalsePass(t *testing.T) {
	labLifecycle(t)
	appointment := func(l *connectedlab.FHIRLab, id string) {
		l.Put("Appointment", id, map[string]any{"identifier": []any{map[string]any{"system": connectedlab.AppointmentSystem, "value": "NATIVE-1"}}, "status": "booked", "start": nativeBooked, "participant": []any{map[string]any{"status": "accepted"}}}, 0)
	}
	retrying := func(f *connectedtest.FlowTest) {
		// Retries are allowed for safe reads; a write is still sent once.
		for i := range f.Steps {
			f.Steps[i].Interaction.Retry = fhirrest.Retry{MaxAttempts: 4, MaxDelayMS: 100}
		}
	}
	for _, tc := range []struct {
		name, flow, engine string
		author             func(*connectedtest.FlowTest)
		arrange            func(*connectedlab.Harness)
		verdict            assertion.Verdict
		state              string
		sent               bool
	}{
		// The duplicate arrives after the ACK and after an early stable sample.
		{name: "late-duplicate", flow: "booking", engine: "late-duplicate", verdict: assertion.VerdictFail, state: "complete", sent: true},
		// Output that appears only after the ACK is observed within the horizon.
		{name: "delayed-output", flow: "booking", engine: "delayed", verdict: assertion.VerdictPass, state: "complete", sent: true},
		{name: "api-outage", flow: "booking", engine: "fixed", arrange: func(h *connectedlab.Harness) { h.Engine.SetAfter(func() { h.Lab.Outage.Store(true) }) }, verdict: assertion.VerdictUndecided, state: "incomplete", sent: true},
		{name: "truncated-pages", flow: "booking", engine: "fixed", arrange: func(h *connectedlab.Harness) { h.Engine.SetAfter(func() { h.Lab.Truncate.Store(true) }) }, verdict: assertion.VerdictUndecided, state: "incomplete", sent: true},
		// More matching pages than the reviewed budget: a first page that looks
		// right is not the complete search.
		{name: "page-budget", flow: "booking", engine: "fixed", arrange: func(h *connectedlab.Harness) {
			h.Engine.SetAfter(func() {
				h.Lab.SetPageSize(1)
				h.Lab.With(func() {
					for _, id := range []string{"a", "b", "c", "d"} {
						h.Lab.Put("Appointment", "extra-"+id, map[string]any{"identifier": []any{map[string]any{"system": connectedlab.AppointmentSystem, "value": "APPT-1"}}, "status": "booked", "start": bookedAt, "participant": []any{map[string]any{"status": "accepted"}}}, 0)
					}
				})
			})
		}, verdict: assertion.VerdictUndecided, state: "incomplete", sent: true},
		// An included same-identifier Appointment is never the missing match.
		{name: "included-same-type", flow: "booking", engine: "lost", arrange: func(h *connectedlab.Harness) { h.Lab.IncludeSameType.Store(true) }, verdict: assertion.VerdictFail, state: "complete", sent: true},
		// The observed server's FHIR version changed: its baseline refuses
		// before the v2 stimulus reaches the engine.
		{name: "observed-server-version", flow: "booking", engine: "fixed", arrange: func(h *connectedlab.Harness) {
			h.Lab.CapabilityOverride.Store(strings.Replace(connectedlab.Capability, `"fhirVersion":"4.0.1"`, `"fhirVersion":"4.3.0"`, 1))
		}, verdict: assertion.VerdictUndecided, state: "incomplete"},
		// Residual state from an earlier run cannot stand in for this run's output.
		{name: "stale-baseline", flow: "booking", engine: "fixed", arrange: func(h *connectedlab.Harness) {
			h.Lab.Extra = func() {
				h.Lab.Put("Appointment", "left-over", map[string]any{"identifier": []any{map[string]any{"system": connectedlab.AppointmentSystem, "value": "APPT-1"}}, "status": "booked", "start": bookedAt, "participant": []any{map[string]any{"status": "accepted"}}}, 0)
			}
		}, verdict: assertion.VerdictFail, state: "complete", sent: true},
		{name: "baseline-unavailable", flow: "booking", engine: "fixed", arrange: func(h *connectedlab.Harness) { h.Lab.Extra = func() { h.Lab.Outage.Store(true) } }, verdict: assertion.VerdictUndecided, state: "incomplete"},
		{name: "lost-write-response", flow: "native", author: retrying, arrange: func(h *connectedlab.Harness) { h.Lab.DropCreate.Store(true) }, verdict: assertion.VerdictUndecided, state: "uncertain"},
		{name: "missing-business-id", flow: "lookup", verdict: assertion.VerdictUndecided, state: "incomplete"},
		{name: "multiple-business-id", flow: "lookup", arrange: func(h *connectedlab.Harness) {
			h.Lab.Extra = func() { appointment(h.Lab, "first"); appointment(h.Lab, "second") }
		}, verdict: assertion.VerdictUndecided, state: "incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := appointmentMessages()
			if tc.flow != "booking" {
				messages = nil
			}
			h := connectedlab.New(t, "", messages...)
			if tc.engine == "late-duplicate" || tc.engine == "delayed" {
				// The engine's write lands 60ms after the ACK; the horizon
				// leaves room for it whatever else the machine is running.
				h.HorizonMS = 1000
			}
			flow := map[string]func(*connectedlab.Harness) connectedtest.FlowTest{"booking": bookingFlow, "native": nativeFlow, "lookup": lookupFlow}[tc.flow](h)
			if tc.author != nil {
				tc.author(&flow)
			}
			h.Compile(flow)
			if tc.engine != "" {
				h.Engine.SetMode(tc.engine)
			}
			if tc.arrange != nil {
				tc.arrange(h)
			}
			r, _ := h.Run("fault")
			if r.Verdict != tc.verdict || r.State != tc.state {
				t.Fatalf("verdict=%s state=%s, want %s %s", r.Verdict, r.State, tc.verdict, tc.state)
			}
			switch tc.name {
			case "baseline-unavailable", "observed-server-version":
				if h.Lab.Count("Appointment", connectedlab.AppointmentSystem, "APPT-1") != 0 || connectedlab.PhaseResult(r, "booking").Steps[0].Outcome == "complete" {
					t.Fatal("an unavailable baseline still let the stimulus reach the engine")
				}
			case "lost-write-response":
				book := connectedlab.PhaseResult(r, "book")
				if h.Lab.Creates.Load() != 1 || !book.Steps[0].Uncertain || connectedlab.PhaseResult(r, "reschedule").State != "blocked" && connectedlab.PhaseResult(r, "reschedule").State != "not-attempted" {
					t.Fatal("a lost create response was retried or continued", h.Lab.Creates.Load(), book)
				}
			case "missing-business-id", "multiple-business-id":
				if connectedlab.PhaseResult(r, "update").Steps[0].Outcome != "not-attempted" && connectedlab.PhaseResult(r, "update").State != "blocked" {
					t.Fatal("an unbound identity addressed a write", connectedlab.PhaseResult(r, "update"))
				}
				if h.Lab.Writes.Load() != 0 {
					t.Fatal("the target was written without an exactly-one binding")
				}
			}
		})
	}
}

func TestFHIRFlowCapabilityVersionAndAuthorityMismatchesStopBeforeEffects(t *testing.T) {
	labLifecycle(t)
	for _, tc := range []struct{ name, capability string }{
		{"create-withdrawn", strings.Replace(connectedlab.Capability, `{"type":"Appointment","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},{"code":"create"},`, `{"type":"Appointment","versioning":"versioned-update","interaction":[{"code":"read"},{"code":"search-type"},`, 1)},
		{"fhir-version-changed", strings.Replace(connectedlab.Capability, `"fhirVersion":"4.0.1"`, `"fhirVersion":"4.3.0"`, 1)},
		{"authority-missing", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := connectedlab.New(t, "")
			h.Compile(nativeFlow(h))
			if tc.capability != "" {
				h.Lab.CapabilityOverride.Store(tc.capability)
			}
			p := h.Prepare("mismatch")
			if tc.name == "authority-missing" {
				if err := os.Remove(filepath.Join(h.Root, connectedlab.GrantFile("book:step:practitioner"))); err != nil {
					t.Fatal(err)
				}
			}
			r, err := connectedrun.ExecuteFlow(t.Context(), p, filepath.Join(h.Root, "mismatch"), testisolationConfirmation(p, "mismatch"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := connectedrun.OpenFlow(t.Context(), filepath.Join(h.Root, "mismatch")); err != nil {
				t.Fatal(err)
			}
			if h.Lab.Writes.Load() != 0 || h.Lab.Creates.Load() != 0 {
				t.Fatal("a mismatch still reached the server with a write")
			}
			book := connectedlab.PhaseResult(r, "book")
			if r.Verdict == assertion.VerdictPass || book.State == "complete" || book.Steps[0].Outcome == "complete" {
				t.Fatal("mismatched capability or authority did not stop the phase", r.Verdict, book)
			}
		})
	}
	// A grant scoped to an observation cannot authorize a write.
	t.Run("observation-grant-for-write", func(t *testing.T) {
		h := connectedlab.New(t, "")
		h.Compile(nativeFlow(h))
		p := h.Prepare("borrowed")
		observation, err := os.ReadFile(filepath.Join(h.Root, connectedlab.GrantFile("book:dataset:appointments")))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.Root, connectedlab.GrantFile("book:step:practitioner")), observation, 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := connectedrun.ExecuteFlow(t.Context(), p, filepath.Join(h.Root, "borrowed"), testisolationConfirmation(p, "borrowed"))
		if err != nil {
			t.Fatal(err)
		}
		if r.Verdict == assertion.VerdictPass || h.Lab.Creates.Load() != 0 || connectedlab.PhaseResult(r, "book").Steps[0].Outcome != "refused" {
			t.Fatal("an observation grant authorized a write", connectedlab.PhaseResult(r, "book").Steps)
		}
	})
	t.Run("no-json-fallback", func(t *testing.T) {
		h := connectedlab.New(t, "")
		h.Compile(nativeFlow(h))
		var config connectedrun.FHIRFlowConfig
		flowContractRead(t, h.ConfigPath, &config)
		phase := config.Phases["book"]
		// A FHIR dataset cannot be satisfied by a generic observation source.
		phase.Sources["appointments"] = connectedrun.SourceSelection{Path: "source.json"}
		config.Phases["book"] = phase
		write(t, h.ConfigPath, config)
		if _, err := connectedrun.PrepareFlow(h.PlanPath, h.ConfigPath, "fallback"); err == nil {
			t.Fatal("a FHIR dataset accepted a non-FHIR source selection")
		}
	})
}

func TestFHIRFlowValidationIsOptionalLocalAndNeverAPass(t *testing.T) {
	labLifecycle(t)
	for _, staged := range []bool{false, true} {
		h := connectedlab.New(t, "")
		h.Lab.SetDownstream("encounter")
		flow := nativeBooking(h)
		flow.Phases[0].Validations = []connectedtest.ValidationCheck{{ID: "appointment", Step: "create", Profiles: []fhirvalidator.Canonical{}, Requirements: fhirvalidator.Requirements{Terminology: "not-requested", Invariants: "required", FailSeverities: []string{"fatal", "error"}}, TimeoutMS: 60000, MaxOutputBytes: 1 << 20}}
		if staged {
			h.Validation = &connectedrun.ValidationSelection{Capability: stageValidatorCapability(t, h.Root), Engine: "none"}
		}
		h.Compile(flow)
		r, output := h.Run("validate")
		book := connectedlab.PhaseResult(r, "book")
		if connectedlab.CheckOutcome(book, "typed:one-appointment") != assertion.OutcomePassed || connectedlab.CheckOutcome(book, "response:created") != assertion.OutcomePassed {
			t.Fatal("downstream and response checks were not reported apart from validation", book)
		}
		if connectedlab.CheckOutcome(book, "validation:appointment") != assertion.OutcomeUndecided || r.Verdict != assertion.VerdictUndecided {
			t.Fatal("an unavailable validator produced a decided or passing outcome", book, r.Verdict)
		}
		var manifest connectedrun.FHIRPhaseRun
		flowContractRead(t, filepath.Join(output, "phases", "book", "manifest.json"), &manifest)
		retained := manifest.Validations["appointment"]
		if !staged && retained != "capability-not-configured" || staged && len(retained) != 64 {
			t.Fatal("validation state not retained", retained)
		}
		if staged {
			evidence, err := fhirvalidator.Open(t.Context(), filepath.Join(output, "phases", "book", "validations", "appointment"))
			if err != nil || evidence.Result().State != "worker-missing" {
				t.Fatal("staged validation evidence", err)
			}
		}
	}
}

// stageValidatorCapability stages a synthetic validator capability with the
// pinned component identities; no worker is installed, so nothing can run it.
func stageValidatorCapability(t *testing.T, root string) string {
	t.Helper()
	assets := map[string][]byte{"metadata/sbom.json": []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[{"type":"application","name":"synthetic-fixture","version":"1"}]}`), "licenses/fixture.txt": []byte("Synthetic fixture, CC0-1.0"), "metadata/OfflineValidator.java": []byte("synthetic adapter source"), "metadata/adapter.jar": []byte("synthetic adapter binary")}
	pinned := func(id, version, sha string) fhirvalidator.Package {
		return fhirvalidator.Package{ID: id, Version: version, SHA256: sha, License: "CC0-1.0", Dependencies: []fhirvalidator.PackageRef{{ID: "hl7.fhir.r4.core", Version: "4.0.1"}}}
	}
	m := fhirvalidator.Manifest{Schema: fhirvalidator.CapabilitySchema, Platform: "linux/arm64", Image: "sha256:" + networkaction.Digest([]byte("immutable-image")), BaseImage: "debian@sha256:" + networkaction.Digest([]byte("base")), LauncherSHA256: networkaction.Digest([]byte("launcher")),
		Validator:         fhirvalidator.Component{Name: "org.hl7.fhir.validation.cli", Version: "6.10.4", SHA256: "1106b9d58f9e363e47bea7c4fc065841e5fc91fe9d062775c3bfdd212bd653cc", License: "Apache-2.0", SBOM: "metadata/sbom.json"},
		Runtime:           fhirvalidator.Component{Name: "Eclipse Temurin", Version: "21.0.12.1+1", SHA256: "14be1f35ebdbd1f6e8d57eb911a3ffb74d6d9aa255abc5daf2b1302002cf2cf2", License: "GPL-2.0-with-classpath-exception", SBOM: "metadata/sbom.json"},
		Base:              fhirvalidator.Component{Name: "Debian bookworm-slim", Version: "12.12", SHA256: networkaction.Digest([]byte("base")), License: "Debian package licenses", SBOM: "metadata/sbom.json"},
		Adapter:           fhirvalidator.Adapter{SourceSHA256: networkaction.Digest(assets["metadata/OfflineValidator.java"]), JarSHA256: networkaction.Digest(assets["metadata/adapter.jar"]), Compiler: fhirvalidator.Component{Name: "Eclipse Temurin JDK", Version: "21.0.12.1+1", SHA256: "23e37e026f12f3e706f18938ff611db3032d075b09d0879a25d06718c773e223", License: "GPL-2.0-only WITH Classpath-exception-2.0", SBOM: "metadata/sbom.json"}},
		Packages:          []fhirvalidator.Package{{ID: "hl7.fhir.r4.core", Version: "4.0.1", SHA256: "ebd7731df7d36b5b7d39d5fb6c9d77b44bb7fe5742f1a2e87f164738c3289d44", License: "CC0-1.0", Dependencies: []fhirvalidator.PackageRef{}}, pinned("hl7.fhir.xver-extensions", "0.1.0", "f3bb9fa2083402e88a02b41f433655274e8a1cca563211c8f7ba6fd0badf537a"), pinned("hl7.terminology.r4", "6.2.0", "79404c9cc95491fc0155627cd039c401a6eb4748175328131e91b709a41300e2"), pinned("hl7.fhir.uv.extensions.r4", "5.2.0", "b406e75575f05676559d0759770c5939d023ee72fb2ef38e0b3259328487720a")},
		ValidatorPackages: []fhirvalidator.PackageRef{{ID: "hl7.fhir.r4.core", Version: "4.0.1"}, {ID: "hl7.fhir.xver-extensions", Version: "0.1.0"}, {ID: "hl7.terminology.r4", Version: "6.2.0"}, {ID: "hl7.fhir.uv.extensions.r4", Version: "5.2.0"}},
		Profiles:          []fhirvalidator.Canonical{{URL: "http://hl7.org/fhir/StructureDefinition/Patient", Version: "4.0.1", SHA256: networkaction.Digest([]byte("profile")), Package: fhirvalidator.PackageRef{ID: "hl7.fhir.r4.core", Version: "4.0.1"}}}, Terminology: []fhirvalidator.Terminology{}, Assets: []fhirvalidator.Asset{}}
	for name, raw := range assets {
		role := map[string]string{"metadata/OfflineValidator.java": "adapter-source", "metadata/adapter.jar": "adapter-binary", "metadata/sbom.json": "sbom"}[name]
		if role == "" {
			role = "license"
		}
		m.Assets = append(m.Assets, fhirvalidator.Asset{Path: name, SHA256: networkaction.Digest(raw), Bytes: int64(len(raw)), Role: role})
	}
	path := filepath.Join(root, "validator-capability")
	if _, err := fhirvalidator.Stage(t.Context(), path, m, assets); err != nil {
		t.Fatal(err)
	}
	return "validator-capability"
}

func testisolationConfirmation(p *connectedrun.PreparedFlow, instance string) testisolation.Confirmation {
	return testisolation.Confirmation{Plan: p.IsolationIdentity(), Instance: instance}
}

// The same saved plan and expectations run through the existing commands; the
// summary states each observation boundary and carries no observed value.
func TestFHIRFlowRunsThroughExistingCommandsWithTruthfulExits(t *testing.T) {
	labLifecycle(t)
	h := connectedlab.New(t, "", appointmentMessages()...)
	h.Compile(appointmentFlow(h))
	input := filepath.Join(h.Root, "authored")
	for name, raw := range h.Files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(input, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(input, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(input, "test.json"), h.Authored, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := cli.Execute("connected", []string{"connected", "prepare", input, filepath.Join(h.Root, "cli-plan"), "--seed", "7", "--base-time", "2026-01-01T00:00:00Z"}, &stdout, &stderr); err != nil || strings.TrimSpace(stdout.String()) != h.Plan.Identity() {
		t.Fatal("connected prepare did not compile the same v5 plan", err, stdout.String(), stderr.String())
	}
	for _, tc := range []struct {
		mode string
		exit int
	}{{"fixed", 0}, {"defective", 1}, {"unresolved", 2}} {
		h.Engine.SetMode(tc.mode)
		if tc.mode == "unresolved" {
			h.Engine.SetMode("fixed")
		}
		h.Prepare("cli-" + tc.mode)
		if tc.mode == "unresolved" {
			_ = os.Remove(filepath.Join(h.Root, connectedlab.GrantFile("isolation:setup")))
		}
		output := filepath.Join(h.Root, "cli-"+tc.mode)
		stdout.Reset()
		stderr.Reset()
		err := cli.Execute("test", []string{"--operation-policy", testlicense.New(t), "test", h.PlanPath, "--connected-config", h.ConfigPath, "--instance", "cli-" + tc.mode, "--send", "--output", output}, &stdout, &stderr)
		if cli.ExitCode(err) != tc.exit {
			t.Fatalf("%s: exit=%d want=%d: %v %s", tc.mode, cli.ExitCode(err), tc.exit, err, stderr.String())
		}
		summary := stdout.String()
		if !strings.Contains(summary, `"schema":"readmit-connected-summary/v2"`) || !strings.Contains(summary, `"boundary":"reference-fhir-store"`) || strings.Contains(summary, "APPT-1") || strings.Contains(summary, h.Root) {
			t.Fatal("unsafe or unqualified lifecycle summary", summary)
		}
		stdout.Reset()
		if err := cli.Execute("run", []string{"run", "status", output, "--reanalysis", "--json"}, &stdout, &stderr); cli.ExitCode(err) != tc.exit || !strings.Contains(stdout.String(), "readmit-connected-reanalysis/v1") {
			t.Fatal("reanalysis through the existing command", err, stdout.String())
		}
	}
	// Retained evidence is re-evaluated offline with the lab and engine gone.
	h.Lab.Server().Close()
	h.Engine.Listener.Close()
	for _, mode := range []string{"fixed", "defective"} {
		analysis, err := connectedrun.ReanalyzeFlow(t.Context(), filepath.Join(h.Root, "cli-"+mode))
		if err != nil || analysis.Reanalysis.Verdict != analysis.Original.Verdict {
			t.Fatal("offline reanalysis", mode, err)
		}
	}
}

// Rewriting retained claims and resealing every enclosing layer cannot change
// what the offline reader derives from the retained response bytes.
func TestFHIRFlowReaderRefusesResealedBindingsAndRows(t *testing.T) {
	labLifecycle(t)
	h := connectedlab.New(t, "")
	h.Lab.SetDownstream("encounter")
	h.Compile(nativeFlow(h))
	_, output := h.Run("tamper")
	copyTree := func(name string) string {
		dir := filepath.Join(h.Root, name)
		if err := os.CopyFS(dir, os.DirFS(output)); err != nil {
			t.Fatal(err)
		}
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
		return dir
	}
	edit := func(path string, change func(map[string]any)) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		change(v)
		raw, _ = json.Marshal(v)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Resealing alone, with nothing changed, still reopens.
	control := copyTree("control")
	resealDirectory(t, filepath.Join(control, "phases", "book"), connectedrun.PhaseSchemaV2)
	resealDirectory(t, control, connectedrun.FlowSchemaV4)
	if _, err := connectedrun.OpenFlow(t.Context(), control); err != nil {
		t.Fatal("an unchanged resealed copy does not reopen", err)
	}
	// A different server-assigned ID claimed for the booking response.
	bound := copyTree("bound")
	edit(filepath.Join(bound, "phases", "book", "manifest.json"), func(v map[string]any) { v["bound"].(map[string]any)["appointment-id"] = "someone-else" })
	resealDirectory(t, filepath.Join(bound, "phases", "book"), connectedrun.PhaseSchemaV2)
	resealDirectory(t, bound, connectedrun.FlowSchemaV4)
	if _, err := connectedrun.OpenFlow(t.Context(), bound); err == nil {
		t.Fatal("a resealed response binding was accepted")
	}
	// A different typed value claimed for the final observed Appointment.
	rows := copyTree("rows")
	var manifest connectedrun.FHIRPhaseRun
	flowContractRead(t, filepath.Join(rows, "phases", "book", "manifest.json"), &manifest)
	final := filepath.Join(rows, "phases", "book", filepath.FromSlash(manifest.Observations["appointments"]))
	edit(filepath.Join(final, "sample.json"), func(v map[string]any) {
		row := v["table"].(map[string]any)["rows"].([]any)[0].(map[string]any)
		row["values"].([]any)[1].(map[string]any)["text"] = "cancelled"
	})
	resealDirectory(t, final, fhirobserve.SampleSchema)
	resealDirectory(t, filepath.Join(rows, "phases", "book", "intervals", "appointments"), observeinterval.SamplesSchema)
	resealDirectory(t, filepath.Join(rows, "phases", "book"), connectedrun.PhaseSchemaV2)
	resealDirectory(t, rows, connectedrun.FlowSchemaV4)
	if _, err := connectedrun.OpenFlow(t.Context(), rows); err == nil {
		t.Fatal("a resealed observed value was accepted")
	}
	if _, err := connectedrun.OpenFlow(t.Context(), output); err != nil {
		t.Fatal("the untouched original no longer reopens", err)
	}
}

// Every mutation, identity use and observation is visible and checkable in
// the plan before anything runs; unsafe authoring is refused at compile time.
func TestFHIRFlowPlanRefusesUnreviewableAuthoring(t *testing.T) {
	h := connectedlab.New(t, "", appointmentMessages()...)
	for _, tc := range []struct {
		name   string
		mutate func(*connectedtest.FlowTest)
	}{
		{"frozen-v4-schema", func(f *connectedtest.FlowTest) { f.Schema = connectedtest.FlowTestSchema }},
		{"use-without-binder-dependency", func(f *connectedtest.FlowTest) { f.Steps[5].After = []string{"move"} }},
		{"bound-twice", func(f *connectedtest.FlowTest) {
			f.Steps[4].Interaction.Bind = []connectedtest.ResponseBinding{connectedlab.BindID("moved-version", "version-id", "lifecycle")}
		}},
		{"declared-never-bound", func(f *connectedtest.FlowTest) {
			f.Variables = append(f.Variables, connectedtest.Variable{ID: "orphan", Kind: "response"})
		}},
		{"phase-scope-escapes", func(f *connectedtest.FlowTest) { f.Steps[2].Interaction.Bind[0].Scope = "phase" }},
		{"undeclared-scope", func(f *connectedtest.FlowTest) { f.Steps[0].Interaction.Bind[0].Scope = "" }},
		{"undeclared-placeholder", func(f *connectedtest.FlowTest) { f.Steps[3].Interaction.Path = "Appointment/{unknown}" }},
		{"unadvertised-interaction", func(f *connectedtest.FlowTest) { f.Steps[2].Interaction.Path = "Encounter" }},
		{"update-without-if-match", func(f *connectedtest.FlowTest) { f.Steps[3].Interaction.Headers = connectedtest.FHIRHeaders{} }},
		{"multiple-binding-cardinality", func(f *connectedtest.FlowTest) { f.Steps[2].Interaction.Bind[0].Multiplicity = "any" }},
		{"mixed-phase", func(f *connectedtest.FlowTest) {
			f.Steps = append(f.Steps, h.V2Step("book"))
			f.Phases[0].Steps = append(f.Phases[0].Steps, "book")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flow := nativeFlow(h)
			tc.mutate(&flow)
			if tc.name == "frozen-v4-schema" {
				flow.Servers = []connectedtest.FHIRServer{{ID: "lab"}}
				raw, _ := json.Marshal(flow)
				if _, err := connectedtest.CompileFlow(raw, h.Files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
					t.Fatal("a frozen v4 flow accepted FHIR members")
				}
				return
			}
			if err := h.TryCompile(flow); err == nil {
				t.Fatal("unsafe authoring compiled")
			}
		})
	}
	if err := h.TryCompile(nativeFlow(h)); err != nil {
		t.Fatal("the unmodified flow does not compile", err)
	}
	// A reviewed update of the resource type a later v2 phase observes would
	// make the write the integration is meant to make; after the last such
	// phase it is ordinary cleanup.
	receiving := func(first bool) connectedtest.FlowTest {
		flow := appointmentFlow(h)
		ds := h.Observe("overwrite", "appointments", "after", "Appointment", "identifier=urn%3Areadmit-lab%3Aappointment%7CAPPT-1", "reference-fhir-store", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"))
		phase := connectedtest.FlowPhase{ID: "overwrite", Steps: []string{"overwrite"}, Datasets: []connectedtest.Dataset{ds}, Checks: h.Checks("overwrite", []connectedtest.Dataset{ds}, connectedlab.RowCount("one", "appointments", 1))}
		flow.Steps = append(flow.Steps, h.FHIRStep("overwrite", "PUT", "Appointment/app-1", appointmentBody("app-1", "booked", rescheduledAt), connectedtest.FHIRHeaders{IfMatch: `W/"1"`}, nil))
		if first {
			flow.Phases = append([]connectedtest.FlowPhase{phase}, flow.Phases...)
		} else {
			flow.Phases = append(flow.Phases, phase)
		}
		return flow
	}
	if h.TryCompile(receiving(true)) == nil {
		t.Fatal("a reviewed update of the resource a later v2 phase observes compiled")
	}
	if err := h.TryCompile(receiving(false)); err != nil {
		t.Fatal("an update after the last observing v2 phase was refused", err)
	}
}

// Reads and writes use separate SMART Backend Services clients with their own
// scopes and token grants; a missing token grant stops before any effect.
func TestFHIRFlowSMARTBackendServicesAuthorizeEachRoleSeparately(t *testing.T) {
	labLifecycle(t)
	for _, withheld := range []bool{false, true} {
		h := connectedlab.New(t, "")
		h.EnableSMART()
		h.Lab.SetDownstream("encounter")
		h.Compile(nativeBooking(h))
		if !withheld {
			r, _ := h.Run("smart")
			if r.Verdict != assertion.VerdictPass || h.Lab.Tokens.Load() < 2 || h.Lab.Bearers.Load() == 0 {
				t.Fatal("SMART-authorized lifecycle did not pass through both roles", r.Verdict, h.Lab.Tokens.Load(), h.Lab.Bearers.Load())
			}
			continue
		}
		p := h.Prepare("smart")
		if err := os.Remove(filepath.Join(h.Root, connectedlab.GrantFile("token:lab:observation"))); err != nil {
			t.Fatal(err)
		}
		r, err := connectedrun.ExecuteFlow(t.Context(), p, filepath.Join(h.Root, "smart"), testisolationConfirmation(p, "smart"))
		if err != nil {
			t.Fatal(err)
		}
		if r.Verdict == assertion.VerdictPass || h.Lab.Creates.Load() != 0 || h.Lab.Writes.Load() != 0 {
			t.Fatal("a withheld observation token grant still let the lifecycle act", r.Verdict)
		}
	}
}
