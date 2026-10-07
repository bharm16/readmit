package connectedrun_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestRuntimeFlowKeepsApprovedInputStableAndRetainsActualDerivedExecution(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	d, files := flowWireDependencies(t, h)
	d.Schema = connectedtest.RuntimeFlowTestSchema
	d.Variables = append(d.Variables, connectedtest.Variable{ID: "runtime", Kind: "runtime-instance"})
	for i := range d.Steps {
		d.Steps[i].V2.Assignments = append(d.Steps[i].V2.Assignments, connectedtest.Assignment{Selector: "MSH-10", Variable: "runtime"})
	}
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	config.Schema = "readmit-connected-run-config/v5"
	flowWireInstall(t, h, d, files, config)
	first := flowWirePrepare(t, h, "runtime-first")
	firstInput, err := first.InputIdentity()
	if err != nil {
		t.Fatal(err)
	}
	before := capturedLifecycle(t, h.root)
	second, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "runtime-second")
	if err != nil {
		t.Fatal(err)
	}
	secondInput, err := second.InputIdentity()
	if err != nil || firstInput != secondInput {
		t.Fatal("runtime occurrence changed approved input", err)
	}
	if !reflect.DeepEqual(before, capturedLifecycle(t, h.root)) {
		t.Fatal("passive runtime preparation wrote files")
	}
	if first.Bindings()["booking:stimulus"] == second.Bindings()["booking:stimulus"] {
		t.Fatal("runtime occurrences borrowed the same concrete stimulus binding")
	}
	snapshot, err := first.InputSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := first.RegistrySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := os.ReadFile(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(h.root, "runtime-result")
	result, err := connectedrun.ExecuteFlow(t.Context(), first, output, testisolation.Confirmation{})
	if err != nil || result.State != "complete" || result.Verdict != assertion.VerdictPass || result.Plan != h.plan.Identity() || result.Schema != "readmit-connected-run/v5" {
		t.Fatalf("actual runtime execution: %+v %v", result, err)
	}
	if err := connectedrun.VerifyFlowInput(t.Context(), output, snapshot, configuration, "runtime-first", registry); err != nil {
		t.Fatal("runtime execution lost approval provenance", err)
	}
	if err := connectedrun.VerifyFlowInput(t.Context(), output, snapshot, configuration, "runtime-second", registry); err == nil {
		t.Fatal("borrowed another occurrence's runtime execution")
	}
	evidence, err := connectedrun.OpenFlowEvidence(t.Context(), output)
	if err != nil || evidence.Result.Plan != h.plan.Identity() || evidence.Phases["booking"].Transport == nil {
		t.Fatal("runtime evidence cannot be opened", err)
	}
	retained := capturedLifecycle(t, output)
	relocated := filepath.Join(h.root, "relocated-runtime")
	if err := os.Rename(output, relocated); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(h.planPath, filepath.Join(h.root, "original-plan-unavailable")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(h.root, "case"), filepath.Join(h.root, "original-case-unavailable")); err != nil {
		t.Fatal(err)
	}
	witness := h.fixture.target.received.Load()
	analysis, err := connectedrun.ReanalyzeFlow(t.Context(), relocated)
	if err != nil || analysis.OriginalIdentity != evidence.Identity || analysis.Original.Verdict != assertion.VerdictPass || analysis.Original.Engine != result.Engine || analysis.Reanalysis.Verdict != assertion.VerdictPass || analysis.Reanalysis.Engine != engine.Version() || !reflect.DeepEqual(analysis.Result, result) {
		t.Fatalf("runtime offline reanalysis lost original identity or verdict: %+v %v", analysis, err)
	}
	if !reflect.DeepEqual(retained, capturedLifecycle(t, relocated)) || h.fixture.target.received.Load() != witness {
		t.Fatal("runtime reanalysis changed evidence or repeated stimulus")
	}
	historical := filepath.Join(h.root, "historical-runtime")
	if err := os.CopyFS(historical, os.DirFS(relocated)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"started.json", "manifest.json", "execution/started.json", "execution/manifest.json"} {
		var record connectedrun.FlowResult
		flowContractRead(t, filepath.Join(historical, name), &record)
		record.Engine = "retained-historical-engine"
		write(t, filepath.Join(historical, name), record)
	}
	resealDirectory(t, filepath.Join(historical, "execution"), connectedrun.FlowSchema)
	resealDirectory(t, historical, connectedrun.RuntimeFlowSchema)
	historicalBytes := capturedLifecycle(t, historical)
	historicalAnalysis, err := connectedrun.ReanalyzeFlow(t.Context(), historical)
	if err != nil || historicalAnalysis.Original.Engine != "retained-historical-engine" || historicalAnalysis.Result.Engine != "retained-historical-engine" || historicalAnalysis.OriginalIdentity != artifactdir.Identity(connectedrun.RuntimeFlowSchema, historicalBytes) || historicalAnalysis.Original.Verdict != assertion.VerdictPass || historicalAnalysis.Reanalysis.Engine != engine.Version() || historicalAnalysis.Reanalysis.Verdict != assertion.VerdictPass {
		t.Fatalf("historical runtime stamp was rewritten or refused: %+v %v", historicalAnalysis, err)
	}
	if !reflect.DeepEqual(historicalBytes, capturedLifecycle(t, historical)) || h.fixture.target.received.Load() != witness {
		t.Fatal("historical reanalysis changed evidence or repeated stimulus")
	}
	if _, err := connectedrun.VerifyFlowEvidence(t.Context(), retained); err != nil {
		t.Fatal("runtime evidence reopened external paths", err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(retained["derivation.json"], &receipt); err != nil {
		t.Fatal(err)
	}
	receipt["instance"] = "runtime-second"
	retained["derivation.json"], err = json.Marshal(receipt, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	retained["identity.sha256"] = []byte(artifactdir.Identity(connectedrun.RuntimeFlowSchema, retained) + "\n")
	if _, err := connectedrun.VerifyFlowEvidence(t.Context(), retained); err == nil {
		t.Fatal("resealed runtime evidence accepted another occurrence's derivation")
	}
}

func TestRuntimeFlowRefusesChangedOriginalBeforeEffects(t *testing.T) {
	h := newFlowContractHarness(t)
	d, files := flowWireDependencies(t, h)
	d.Schema = connectedtest.RuntimeFlowTestSchema
	d.Variables = append(d.Variables, connectedtest.Variable{ID: "runtime", Kind: "runtime-instance"})
	for i := range d.Steps {
		d.Steps[i].V2.Assignments = append(d.Steps[i].V2.Assignments, connectedtest.Assignment{Selector: "MSH-10", Variable: "runtime"})
	}
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	config.Schema = connectedrun.RuntimeFlowConfigSchema
	flowWireInstall(t, h, d, files, config)
	p := flowWirePrepare(t, h, "changed-original")
	original := filepath.Join(h.root, "case", "payloads", "s0001-e000001.bin")
	if err := os.WriteFile(original, []byte("changed-original"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(h.root, "refused-runtime")
	if _, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{}); err == nil {
		t.Fatal("changed original allowed runtime effects")
	}
	if h.fixture.target.received.Load() != 0 {
		t.Fatal("changed original reached target")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("refused runtime started output", err)
	}
}

func TestRuntimeFlowCancellationRetainsCompletedPrefixAndCannotResume(t *testing.T) {
	h := newFlowContractHarnessForRecovery(t)
	d, files := flowWireDependencies(t, h)
	d.Schema = connectedtest.RuntimeFlowTestSchema
	d.Variables = append(d.Variables, connectedtest.Variable{ID: "runtime", Kind: "runtime-instance"})
	for i := range d.Steps {
		d.Steps[i].V2.Assignments = append(d.Steps[i].V2.Assignments, connectedtest.Assignment{Selector: "MSH-10", Variable: "runtime"})
	}
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	config.Schema = connectedrun.RuntimeFlowConfigSchema
	flowWireInstall(t, h, d, files, config)
	p := flowWirePrepare(t, h, "cancel-runtime")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	output := filepath.Join(h.root, "cancelled-runtime")
	result, err := connectedrun.ExecuteFlow(ctx, p, output, testisolation.Confirmation{}, func(phase connectedrun.FlowPhaseResult) {
		if phase.ID == "booking" && phase.State == "complete" {
			cancel()
		}
	})
	if err != nil || result.State != "cancelled" || result.Verdict == assertion.VerdictPass || result.Phases[0].State != "complete" || result.Phases[1].State != "not-attempted" || h.fixture.target.received.Load() != 1 {
		t.Fatalf("runtime cancellation: %+v %v", result, err)
	}
	if _, err := connectedrun.PrepareFlowResume(p, output); err == nil {
		t.Fatal("runtime derivation granted continuation")
	}
	if err := os.Remove(filepath.Join(output, "identity.sha256")); err != nil {
		t.Fatal(err)
	}
	interrupted, err := connectedrun.InspectFlow(t.Context(), output)
	if err != nil || interrupted.Verdict == assertion.VerdictPass || interrupted.Phases[0].State != "complete" {
		t.Fatalf("interrupted runtime lost evidence: %+v %v", interrupted, err)
	}
}
