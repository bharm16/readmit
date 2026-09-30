package expectation_test

import (
	"encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/expectation"
)

func TestConnectedTestReleaseSeparatesEnvironmentBindingsFromReviewedExpectations(t *testing.T) {
	h := connectedlab.New(t, "")
	rows := h.Observe("book", "appointments", "after", "Appointment", "identifier=urn%3Areadmit-lab%3Aappointment%7CRELEASE-1", "reference-fhir-store", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"))
	datasets := []connectedtest.Dataset{rows}
	flow := connectedtest.FlowTest{ID: "booking", Steps: []connectedtest.Step{h.FHIRStep("create", "POST", "Appointment", `{"resourceType":"Appointment","identifier":[{"system":"urn:readmit-lab:appointment","value":"RELEASE-1"}],"status":"booked","participant":[{"status":"accepted"}]}`, connectedtest.FHIRHeaders{}, nil)}, Phases: []connectedtest.FlowPhase{{ID: "book", Steps: []string{"create"}, Datasets: datasets, Checks: h.Checks("book", datasets, connectedlab.RowCount("one", "appointments", 1))}}}
	h.Compile(flow)
	review, err := expectation.ReviewConnected(h.PlanPath)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.ApproveConnected(h.PlanPath, "", review.Identity(), "Reviewer", "Independent appointment oracle", filepath.Join(h.Root, "release.json"))
	if err != nil || release.Review.Definition != review.Definition {
		t.Fatalf("approved release: %+v %v", release, err)
	}
	if h.Lab.Creates.Load() != 0 {
		t.Fatal("review or approval contacted the target")
	}
	// An environment label and target-version declaration change the plan's
	// binding, while the independently authored check stays the same.
	d := h.Plan.Document()
	d.Test.Environment.Name = "Promoted environment"
	d.Test.Environment.TargetRevision.Value = "reviewed-upgrade"
	raw, err := json.Marshal(d.Test)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := connectedtest.CompileFlow(raw, h.Files, d.Generation)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.Root, "promoted")
	if err = promoted.Write(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	after, err := expectation.ReviewConnected(path)
	if err != nil || after.Definition != review.Definition || promoted.Identity() == h.Plan.Identity() {
		t.Fatal("promotion changed the oracle or failed to change its binding", err)
	}
	flow.Phases[0].Checks = h.Checks("changed", datasets, connectedlab.RowCount("one", "appointments", 2))
	h.Compile(flow)
	changed, err := expectation.ReviewConnected(h.PlanPath)
	if err != nil || changed.Definition == review.Definition {
		t.Fatal("a changed expectation retained its approval identity", err)
	}
}
