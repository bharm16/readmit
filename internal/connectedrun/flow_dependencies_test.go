package connectedrun_test

import (
	"encoding/json/v2"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestFlowDependenciesKeepFailedSkippedAndUndecidedDenominators(t *testing.T) {
	for _, kind := range []string{"failed-prerequisite", "skipped-phase", "all-checks-skipped"} {
		t.Run(kind, func(t *testing.T) {
			h := newFlowContractHarnessWithSource(t, true)
			d, files := flowWireDependencies(t, h)
			if kind == "skipped-phase" {
				d.Phases[1].When = &connectedtest.PhaseCondition{Phase: "booking", Check: "typed:status", Outcome: "failed"}
			} else {
				ref := d.Phases[0].Checks
				var checks assertion.DatasetSetDocument
				if err := json.Unmarshal(files[ref.File], &checks); err != nil {
					t.Fatal(err)
				}
				if kind == "failed-prerequisite" {
					two := 2
					checks.Assertions[1].Count = &two
				} else {
					for i := range checks.Assertions {
						checks.Assertions[i].When = &assertion.DatasetCondition{Subject: assertion.RowSelection{Dataset: "after"}, Column: "status", Equals: dataset.Value{State: "present", Type: "text", Text: "never"}}
					}
				}
				files[ref.File], _ = json.Marshal(checks)
				d.Phases[0].Checks.SHA256 = dataset.Digest(files[ref.File])
			}
			raw, _ := json.Marshal(d)
			plan, err := connectedtest.CompileFlow(raw, files, h.plan.Document().Generation)
			if err != nil {
				t.Fatal(err)
			}
			h.plan = plan
			h.planPath = filepath.Join(h.root, "dependent-plan")
			if err = plan.Write(t.Context(), h.planPath); err != nil {
				t.Fatal(err)
			}
			p := h.prepare(t, "dependent")
			out := filepath.Join(h.root, "dependent")
			r, err := connectedrun.ExecuteFlow(t.Context(), p, out, testisolation.Confirmation{})
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Phases) != 2 || len(r.Phases[1].Checks) != 5 || len(r.Phases[1].Steps) != 1 || h.fixture.target.received.Load() != 1 {
				t.Fatalf("lost denominator or sent dependent work: %+v", r)
			}
			expected := "blocked"
			if kind == "skipped-phase" {
				expected = "skipped"
			}
			if r.Phases[1].State != expected {
				t.Fatalf("dependent state=%s", r.Phases[1].State)
			}
			if kind == "all-checks-skipped" && r.Verdict == assertion.VerdictPass {
				t.Fatal("all skipped checks became pass")
			}
			if _, err = connectedrun.OpenFlow(t.Context(), out); err != nil {
				t.Fatal(err)
			}
		})
	}
}
