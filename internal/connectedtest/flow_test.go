package connectedtest_test

import (
	"bytes"
	"encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/testisolation"
)

func flowDefinition(t *testing.T) (connectedtest.FlowTest, map[string][]byte) {
	d, f := intervalDefinition(t)
	d.Environment.Classification = "nonproduction"
	contract := testisolation.Contract{Schema: testisolation.ContractSchema, Project: d.Project, Environment: d.Environment.ID, Revision: d.Environment.Revision}
	f["isolation.json"], _ = json.Marshal(contract)
	flow := connectedtest.FlowTest{Schema: connectedtest.FlowTestSchema, Project: d.Project, ID: d.ID, Revision: d.Revision, Environment: d.Environment, Isolation: connectedtest.Reference{Project: d.Project, ID: "isolation", Schema: testisolation.ContractSchema, File: "isolation.json", SHA256: connectedtest.Digest(f["isolation.json"])}, Boundary: "application-state", Steps: d.Steps, Limits: d.Limits, Phases: []connectedtest.FlowPhase{{ID: "first", Steps: []string{d.Steps[0].ID}, Datasets: d.Datasets, Checks: d.Checks}}}
	second := d.Steps[0]
	second.ID = "repeat"
	second.After = []string{d.Steps[0].ID}
	flow.Steps = append(flow.Steps, second)
	flow.Phases = append(flow.Phases, connectedtest.FlowPhase{ID: "second", Steps: []string{"repeat"}, After: []connectedtest.PhaseDependency{{Phase: "first", Requires: "complete"}}, When: &connectedtest.PhaseCondition{Phase: "first", Check: "typed:absence", Outcome: "passed"}, Datasets: d.Datasets, Checks: d.Checks})
	return flow, f
}
func TestFlowPlanDerivesPinnedPhasesPreservesInputsAndConditions(t *testing.T) {
	d, f := flowDefinition(t)
	raw, _ := json.Marshal(d)
	p, err := connectedtest.CompileFlow(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Document().Phases) != 2 || p.Phase("first").Document().Order[0] != "book" || p.Phase("second").Document().Order[0] != "repeat" {
		t.Fatal("phase derivation lost authored order")
	}
	if !bytes.Equal(p.Phase("first").Files()["inputs/book.hl7"], p.Phase("second").Files()["inputs/repeat.hl7"]) {
		t.Fatal("intentional repeated original rewritten")
	}
	path := filepath.Join(t.TempDir(), "plan")
	if err = p.Write(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	reopened, err := connectedtest.OpenFlowPlan(path)
	if err != nil || reopened.Identity() != p.Identity() {
		t.Fatal(err)
	}
}
func TestFlowPlanRefusesUnsafeCompositionBeforeEffects(t *testing.T) {
	for _, name := range []string{"total-steps", "total-bytes", "missing-step", "duplicate-phase", "future-dependency", "missing-phase-dependency", "unknown-condition-check", "wrong-isolation-scope", "unknown-wire-input", "missing-before-binding", "duplicate-ack-unmapped"} {
		t.Run(name, func(t *testing.T) {
			d, f := flowDefinition(t)
			switch name {
			case "total-steps":
				d.Limits.MaxSteps = 1
			case "total-bytes":
				d.Limits.MaxBytes = 70000
			case "missing-step":
				d.Phases = d.Phases[:1]
			case "duplicate-phase":
				d.Phases[1].ID = "first"
			case "future-dependency":
				d.Phases[0].After = []connectedtest.PhaseDependency{{Phase: "second", Requires: "pass"}}
			case "missing-phase-dependency":
				d.Phases[1].After = nil
			case "unknown-condition-check":
				d.Phases[1].When.Check = "typed:absent"
			case "wrong-isolation-scope":
				d.Isolation.Project = "foreign"
			default:
				raw := []byte(`{"schema":"readmit-assertion-set/v1","name":"wire","assertions":[{"id":"value","operator":"field_equals","subject":{"field":{"scope":"input","message":"s0001-e000009","selector":"MSH-9"}},"when":null,"expected":{"field":{"state":"present","text":"SIU^S12"}}}]}`)
				if name == "missing-before-binding" {
					raw = []byte(`{"schema":"readmit-assertion-set/v1","name":"wire","assertions":[{"id":"count","operator":"record_count","subject":{"collection":{"scope":"before"}},"when":null,"expected":{"count":0}}]}`)
				}
				f["wire.json"] = raw
				d.Phases[0].Wire = &connectedtest.WireChecks{Set: connectedtest.Reference{Project: d.Project, ID: "wire", File: "wire.json", Schema: assertion.Schema, SHA256: connectedtest.Digest(raw)}, Observed: "transport-acks"}
				if name == "duplicate-ack-unmapped" {
					d.Phases[0].Steps = append(d.Phases[0].Steps, "repeat")
					d.Phases = d.Phases[:1]
					d.Steps[1].After = nil
				}
			}
			raw, _ := json.Marshal(d)
			if _, err := connectedtest.CompileFlow(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
				t.Fatal("unsafe composition accepted")
			}
		})
	}
}
