package fhirvalidator_test

import (
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/fhirvalidator"
)

// Without an installed worker a requested validation is retained as an
// undecided check with its actionable state, never skipped into a pass.
func TestFHIRValidationWithoutAWorkerIsARetainedUndecidedCheck(t *testing.T) {
	p := fixturePlan(t, required)
	output := filepath.Join(t.TempDir(), "result")
	evidence, err := p.Execute(t.Context(), nil, output)
	if err != nil {
		t.Fatal(err)
	}
	if r := evidence.Result(); r.State != "worker-missing" || r.RuntimeStatus == nil || r.RuntimeStatus.Requirement == "" || r.Verdict != assertion.VerdictUndecided {
		t.Fatalf("%v %+v", r, r.RuntimeStatus)
	}
	check, err := connectedtest.ReadFHIRValidationCheck(t.Context(), output, evidence.Identity(), p.Request().InputSHA256, "fhir-validation")
	if err != nil || check.Result.Outcome != assertion.OutcomeUndecided || check.Result.Operator != fhirvalidator.CheckOperator || check.Validation != evidence.Identity() {
		t.Fatalf("%v %v", check, err)
	}
	for name, read := range map[string]func() error{
		"another validation": func() error {
			_, err := connectedtest.ReadFHIRValidationCheck(t.Context(), output, p.Request().InputSHA256, p.Request().InputSHA256, "fhir-validation")
			return err
		},
		"another input": func() error {
			_, err := connectedtest.ReadFHIRValidationCheck(t.Context(), output, evidence.Identity(), evidence.Identity(), "fhir-validation")
			return err
		},
	} {
		if read() == nil {
			t.Fatal(name, "accepted")
		}
	}
}
