package connectedrun_test

import (
	"encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestFlowChildVersionsCannotSilentlyEnterStandaloneOldExecution(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	child := h.plan.Phase("booking")
	if child.Document().Schema != connectedtest.PhasePlanSchema || child.Document().Test.Schema != connectedtest.PhaseTestSchema {
		t.Fatal("flow reused frozen child schema")
	}
	if _, err := connectedtest.Compile(child.Files()["test.json"], nil, child.Document().Generation); err == nil {
		t.Fatal("standalone compiler accepted owned phase contract")
	}
	if _, err := connectedrun.Prepare(filepath.Join(h.planPath, "phases", "booking"), h.configPath); err == nil {
		t.Fatal("old executor silently selected sequence semantics")
	}
	if _, err := connectedtransport.Prepare(child, connectedtransport.Selection{}); err == nil {
		t.Fatal("old transport accepted owned sequence plan")
	}
	p := h.prepare(t, "compatibility")
	out := filepath.Join(h.root, "compatibility")
	if _, err := connectedrun.ExecuteFlow(t.Context(), p, out, testisolation.Confirmation{}); err != nil {
		t.Fatal(err)
	}
	phase := filepath.Join(out, "phases", "booking")
	e, err := connectedrun.OpenEvidence(t.Context(), phase)
	if err != nil || e.Schema != connectedrun.PhaseSchema {
		t.Fatal("phase execution version", err, e)
	}
	run, err := replay.Open(filepath.Join(phase, "transport", "run"))
	if err != nil || run.Manifest.Schema != replay.SequenceSchema {
		t.Fatal("sequence run version", err)
	}
	var manifest map[string]any
	flowContractRead(t, filepath.Join(phase, "manifest.json"), &manifest)
	manifest["schema"] = connectedrun.SchemaV2
	manifest["summary"].(map[string]any)["schema"] = connectedrun.SchemaV2
	write(t, filepath.Join(phase, "manifest.json"), manifest)
	resealDirectory(t, phase, connectedrun.SchemaV2)
	if _, err = connectedrun.Open(t.Context(), phase); err == nil {
		t.Fatal("new child transport entered frozen interval result")
	}
	var d connectedtest.Test
	_ = json.Unmarshal(child.Files()["test.json"], &d)
	d.Schema = connectedtest.TestSchemaV3
	duplicate := d.Steps[0]
	duplicate.ID = "duplicate"
	d.Steps = append(d.Steps, duplicate)
	_, files := flowWireDependencies(t, h)
	raw, _ := json.Marshal(d)
	if _, err = connectedtest.Compile(raw, files, child.Document().Generation); err == nil {
		t.Fatal("frozen v3 accepted repeated original")
	}
}
