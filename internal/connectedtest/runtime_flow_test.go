package connectedtest_test

import (
	"bytes"
	"encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/connectedtest"
)

func runtimeFlowDefinition(t *testing.T) (connectedtest.FlowTest, map[string][]byte) {
	t.Helper()
	d, files := flowDefinition(t)
	d.Schema = connectedtest.RuntimeFlowTestSchema
	d.Variables = []connectedtest.Variable{{ID: "runtime", Kind: "runtime-instance"}}
	for i := range d.Steps {
		// Each step owns its assignment declaration, including repeated input.
		v2 := *d.Steps[i].V2
		v2.Assignments = []connectedtest.Assignment{{Selector: "MSH-3", Variable: "runtime"}}
		d.Steps[i].V2 = &v2
	}
	return d, files
}

func TestRuntimeFlowBindsFreshInstancesWithoutChangingApprovedTemplate(t *testing.T) {
	d, files := runtimeFlowDefinition(t)
	raw, _ := json.Marshal(d)
	p, err := connectedtest.CompileFlow(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	identity := p.Identity()
	if !p.RuntimeScoped() || p.Document().Schema != connectedtest.RuntimeFlowPlanSchema {
		t.Fatal("runtime declaration lost")
	}
	a, err := p.BindRuntime("job-a")
	if err != nil {
		t.Fatal(err)
	}
	again, err := p.BindRuntime("job-a")
	if err != nil || a.Identity() != again.Identity() {
		t.Fatal("same binding is not deterministic", err)
	}
	b, err := p.BindRuntime("job-b")
	if err != nil || a.Identity() == b.Identity() {
		t.Fatal("different runtime did not produce distinct inputs", err)
	}
	if p.Identity() != identity || a.RuntimeScoped() || a.Document().Schema != connectedtest.FlowPlanSchema {
		t.Fatal("binding changed template or concrete contract")
	}
	for _, step := range d.Steps {
		if !bytes.Equal(a.Phase("first").Files()["dependencies/"+step.V2.Input.SHA256], files[step.V2.Input.File]) {
			t.Fatal("original stimulus bytes lost")
		}
	}
	if !bytes.Contains(a.Phase("first").Files()["inputs/book.hl7"], []byte("|job-a|")) || !bytes.Contains(b.Phase("first").Files()["inputs/book.hl7"], []byte("|job-b|")) {
		t.Fatal("declared marker was not applied to wire bytes")
	}
	path := filepath.Join(t.TempDir(), "template")
	if err := p.Write(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	opened, err := connectedtest.OpenFlowPlan(path)
	if err != nil || opened.Identity() != identity {
		t.Fatal("template did not reopen", err)
	}
	rebound, err := opened.BindRuntime("job-a")
	if err != nil || rebound.Identity() != a.Identity() {
		t.Fatal("reopened template bound differently", err)
	}
}

func TestRuntimeFlowRefusesUndeclaredOrAmbiguousDerivation(t *testing.T) {
	for _, kind := range []string{"missing-variable", "two-runtime-variables", "variable-value", "variable-namespace", "variable-offset", "missing-assignment", "two-marker-fields", "duplicate-selector", "overlapping-selector", "undefined-variable", "frozen-v4", "frozen-v5", "frozen-v6"} {
		t.Run(kind, func(t *testing.T) {
			d, files := runtimeFlowDefinition(t)
			switch kind {
			case "missing-variable":
				d.Variables = nil
			case "two-runtime-variables":
				d.Variables = append(d.Variables, connectedtest.Variable{ID: "another", Kind: "runtime-instance"})
			case "variable-value":
				d.Variables[0].Value = "fixed-marker"
			case "variable-namespace":
				d.Variables[0].Namespace = "namespace"
			case "variable-offset":
				d.Variables[0].OffsetMS = 1
			case "missing-assignment":
				d.Steps[0].V2.Assignments = nil
			case "two-marker-fields":
				d.Steps[0].V2.Assignments = append(d.Steps[0].V2.Assignments, connectedtest.Assignment{Selector: "MSH-4", Variable: "runtime"})
			case "duplicate-selector", "overlapping-selector":
				d.Variables = append(d.Variables, connectedtest.Variable{ID: "literal", Kind: "literal", Value: "unchanged"})
				selector := "MSH[1]-3[1]"
				if kind == "overlapping-selector" {
					selector = "MSH-3.1"
				}
				d.Steps[0].V2.Assignments = append(d.Steps[0].V2.Assignments, connectedtest.Assignment{Selector: selector, Variable: "literal"})
			case "undefined-variable":
				d.Steps[0].V2.Assignments[0].Variable = "undefined"
			case "frozen-v4":
				d.Schema = connectedtest.FlowTestSchema
			case "frozen-v5":
				d.Schema = connectedtest.FHIRFlowTestSchema
			case "frozen-v6":
				d.Schema = connectedtest.ScheduledFlowTestSchema
			}
			raw, _ := json.Marshal(d)
			if _, err := connectedtest.CompileFlow(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
				t.Fatal("invalid runtime derivation admitted")
			}
		})
	}
}

func TestRuntimeBindingRefusesInvalidInstanceAndLeavesHistoricalLiteralAlone(t *testing.T) {
	d, files := runtimeFlowDefinition(t)
	raw, _ := json.Marshal(d)
	p, err := connectedtest.CompileFlow(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range []string{"", "UPPERCASE", "job|unsafe", "job\runsafe", "../job"} {
		if _, err := p.BindRuntime(instance); err == nil {
			t.Fatal("invalid execution instance admitted")
		}
	}
	d.Schema = connectedtest.FlowTestSchema
	d.Variables[0] = connectedtest.Variable{ID: "readmit-runtime-marker", Kind: "literal", Value: "original-literal"}
	for i := range d.Steps {
		d.Steps[i].V2.Assignments[0].Variable = "readmit-runtime-marker"
	}
	raw, _ = json.Marshal(d)
	legacy, err := connectedtest.CompileFlow(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil || legacy.RuntimeScoped() {
		t.Fatal("historical literal changed meaning", err)
	}
	if _, err := legacy.BindRuntime("new-instance"); err == nil {
		t.Fatal("historical literal was rebound")
	}
	if !bytes.Contains(legacy.Phase("first").Files()["inputs/book.hl7"], []byte("|original-literal|")) {
		t.Fatal("historical literal bytes changed")
	}
}
