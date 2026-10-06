package connectedtest_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
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

func TestPreparedFlowPlanChecksEveryMemberAgainWithoutRecompiling(t *testing.T) {
	d, supplied := flowDefinition(t)
	raw, _ := json.Marshal(d)
	p, err := connectedtest.CompileFlow(raw, supplied, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "original")
	if err = p.Write(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if err = p.VerifyUnchanged(path); err != nil {
		t.Fatal("unchanged plan refused", err)
	}
	for _, kind := range []string{"changed-input", "resealed-input", "missing-member", "extra-member", "linked-member"} {
		t.Run(kind, func(t *testing.T) {
			copy := filepath.Join(t.TempDir(), "plan")
			if err := os.CopyFS(copy, os.DirFS(path)); err != nil {
				t.Fatal(err)
			}
			if err := p.VerifyUnchanged(copy); err != nil {
				t.Fatal("unchanged relocated plan refused", err)
			}
			member := filepath.Join(copy, "phases", "first", "inputs", "book.hl7")
			switch kind {
			case "changed-input", "resealed-input":
				if err := os.WriteFile(member, []byte("altered original bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "resealed-input" {
					files := p.Files()
					files["phases/first/inputs/book.hl7"] = []byte("altered original bytes")
					phase := artifactdir.Subtree(files, "phases/first")
					seal := []byte(artifactdir.Identity(p.Phase("first").Document().Schema, phase) + "\n")
					files["phases/first/identity.sha256"] = seal
					if err := os.WriteFile(filepath.Join(copy, "phases", "first", "identity.sha256"), seal, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(copy, "identity.sha256"), []byte(artifactdir.Identity(p.Document().Schema, files)+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "missing-member", "linked-member":
				if err := os.Remove(member); err != nil {
					t.Fatal(err)
				}
				if kind == "linked-member" {
					if err := os.Symlink(filepath.Join(path, "phases", "first", "inputs", "book.hl7"), member); err != nil {
						t.Skip("symlinks unavailable")
					}
				}
			case "extra-member":
				if err := os.WriteFile(filepath.Join(copy, "dependencies", "extra"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := p.VerifyUnchanged(copy); err == nil {
				t.Fatal("changed plan accepted")
			}
		})
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

// FHIR members exist only in v5 flows and their derived v2 phase plans; a
// frozen schema carrying them is refused rather than reinterpreted.
func TestFrozenConnectedSchemasRefuseFHIRMembers(t *testing.T) {
	v3, f := intervalDefinition(t)
	server := []connectedtest.FHIRServer{{ID: "lab", Base: "https://lab.test/fhir"}}
	for name, change := range map[string]func(*connectedtest.Test){
		"v3 servers":           func(d *connectedtest.Test) { d.Servers = server },
		"v3 empty servers":     func(d *connectedtest.Test) { d.Servers = []connectedtest.FHIRServer{} },
		"v3 empty validations": func(d *connectedtest.Test) { d.Validations = []connectedtest.ValidationCheck{} },
		"v3 response checks": func(d *connectedtest.Test) {
			d.Responses = []connectedtest.ResponseCheck{{ID: "ok", Step: d.Steps[0].ID, Outcome: "succeeded"}}
		},
		"v3 response variable": func(d *connectedtest.Test) {
			d.Variables = append(d.Variables, connectedtest.Variable{ID: "server-id", Kind: "response"})
		},
		"v3 interaction": func(d *connectedtest.Test) {
			d.Steps[0].Interaction = &connectedtest.FHIRInteraction{Server: "lab", Method: "GET", Path: "Patient"}
		},
		"standalone v2 phase schema": func(d *connectedtest.Test) { d.Schema = connectedtest.PhaseTestSchemaV2 },
	} {
		d := v3
		d.Steps = append([]connectedtest.Step(nil), v3.Steps...)
		d.Variables = append([]connectedtest.Variable(nil), v3.Variables...)
		change(&d)
		raw, _ := json.Marshal(d)
		if _, err := connectedtest.Compile(raw, f, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
			t.Error(name, "accepted")
		}
	}
	// A v1 declaration of a FHIR resource dataset keeps compiling exactly as it
	// did; only a v2 phase plan reads it as an executable FHIR observation.
	raw, files := example(t)
	var legacy connectedtest.Test
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Datasets = append(legacy.Datasets, connectedtest.Dataset{ID: "resources", Kind: "fhir-resources", Phase: "after", Source: "declared-fhir", Completion: connectedtest.Completion{Kind: "bounded-horizon", HorizonMS: 1000, MaxRecords: 10, MaxBytes: 65536}})
	raw, _ = json.Marshal(legacy)
	if _, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err != nil {
		t.Error("a v1 FHIR resource dataset declaration no longer compiles", err)
	}
	for _, servers := range [][]connectedtest.FHIRServer{server, {}} {
		flow, files := flowDefinition(t)
		flow.Servers = servers
		raw, _ := json.Marshal(flow)
		if _, err := connectedtest.CompileFlow(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"}); err == nil {
			t.Error("a v4 flow accepted FHIR servers", len(servers))
		}
	}
}
