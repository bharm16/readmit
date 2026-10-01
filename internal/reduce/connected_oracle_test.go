package reduce_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/reduce"
)

// The independent integration creates a second appointment when asked to
// reschedule one. The original messages and the FHIR store are real external
// effects; the unrelated ADT update contributes nothing to this failure.
func TestConnectedReductionUsesFreshIsolationAndRetainsTheSameExternalFailure(t *testing.T) {
	h := connectedlab.New(t, "",
		connectedlab.Message{Step: "book", Raw: "MSH|^~\\&|S|L|E|L|20260101000000||SIU^S12|BOOK|P|2.5.1\rSCH|SAME||||||||||2026-01-01T12:00:00Z\r"},
		connectedlab.Message{Step: "move", Raw: "MSH|^~\\&|S|L|E|L|20260101000000||SIU^S13|MOVE|P|2.5.1\rSCH|SAME||||||||||2026-01-02T12:00:00Z\r"},
		connectedlab.Message{Step: "noise", Raw: "MSH|^~\\&|S|L|E|L|20260101000000||ADT^A08|NOISE|P|2.5.1\rPID|1||UNRELATED\r"})
	h.Engine.SetMode("defective")
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		retained, err := os.MkdirTemp("", "readmit-connected-reduction-failure-")
		if err != nil {
			t.Logf("cannot retain failed reduction: %v", err)
			return
		}
		if err = os.CopyFS(filepath.Join(retained, "evidence"), os.DirFS(h.Root)); err != nil {
			t.Logf("partial failed reduction retained at %s: %v", retained, err)
			return
		}
		t.Logf("failed connected reduction retained at %s", retained)
	})
	ds := h.Observe("trial", "appointments", "after", "Appointment", "identifier=urn%3Areadmit-lab%3Aappointment%7CSAME", "reference-fhir-store", connectedlab.FieldColumn("key", "text", "", true, true, "id"), connectedlab.IdentityColumn())
	h.Compile(connectedtest.FlowTest{ID: "reschedule", Steps: []connectedtest.Step{h.V2Step("book"), h.V2Step("move"), h.V2Step("noise")}, Phases: []connectedtest.FlowPhase{{ID: "trial", Steps: []string{"book", "move", "noise"}, Datasets: []connectedtest.Dataset{ds}, Checks: h.Checks("trial", []connectedtest.Dataset{ds}, connectedlab.RowCount("one-appointment", "appointments", 1))}}})
	sequence := []string{h.Occurrences["book"], h.Occurrences["move"], h.Occurrences["noise"]}
	request := reduce.Request{Case: filepath.Join(h.Root, "case"), Plan: reduce.Plan{Schema: reduce.PlanSchema, Grouping: reduce.GroupPerOccurrence, Signature: reduce.Signature{State: durablerun.AssertionFailed, Assertions: []string{"one-appointment"}}, Trials: 16, Confirmations: 1}, Messages: sequence}
	// The case's verified identity, not a plan identity, binds the reduction.
	source, err := bundle.Open(request.Case)
	if err != nil {
		t.Fatal(err)
	}
	request.Plan.Case = source.Identity
	seen := map[string]bool{}
	oracle, err := reduce.NewConnectedOracle(reduce.ConnectedOracleRequest{Plan: h.Plan, Configuration: h.ConfigPath, Workspace: filepath.Join(h.Root, "trials"), Signature: request.Plan.Signature,
		Authorize: func(ctx context.Context, index int, p *connectedrun.PreparedFlow) (networkaction.Authority, error) {
			if seen[p.IsolationIdentity()] {
				t.Fatal("a trial reused its predecessor's isolation authority")
			}
			seen[p.IsolationIdentity()] = true
			return exactTrialAuthority{bindings: p.Bindings(), actor: networkaction.Actor{Kind: "action-review", ID: "reviewer", Generation: "trial", EvidenceIdentity: h.Plan.Identity(), Expires: time.Now().Add(time.Hour)}}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := reduce.Run(t.Context(), request, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != reduce.OutcomeReduced || report.Minimality != reduce.GroupOneMinimal || !slices.Equal(report.Retained, sequence[:2]) || !slices.Equal(report.Removed, sequence[2:]) {
		for _, trial := range report.Trials {
			r, _ := connectedrun.OpenFlow(t.Context(), filepath.Join(h.Root, "trials", fmt.Sprintf("t%04d", trial.Index)))
			t.Logf("retained lifecycle: %+v", r)
			raw, _ := os.ReadFile(filepath.Join(h.Root, "trials", "t0001", "phases", "trial", "manifest.json"))
			t.Logf("retained phase: %s", raw)
			for _, entry := range []string{"intervals/appointments/manifest.json", "intervals/appointments/samples/0001/manifest.json"} {
				raw, _ := os.ReadFile(filepath.Join(h.Root, "trials", "t0001", "phases", "trial", entry))
				t.Logf("retained observation: %s", raw)
			}
		}
		t.Fatalf("the actual duplicate-appointment defect was not retained: %+v", report)
	}
	for _, trial := range report.Trials {
		if trial.Reason != "" || trial.State != durablerun.Passed && trial.State != durablerun.AssertionFailed {
			t.Fatalf("a connected trial was uncertain: %+v", trial)
		}
	}
	if len(seen) != len(report.Trials) {
		t.Fatalf("each trial needs fresh authority: %d for %d trials", len(seen), len(report.Trials))
	}
}

type exactTrialAuthority struct {
	bindings map[string]networkaction.Binding
	actor    networkaction.Actor
}

func (a exactTrialAuthority) Check(ctx context.Context, requested networkaction.Binding) (networkaction.Actor, error) {
	for _, exact := range a.bindings {
		if exact == requested {
			return a.actor, ctx.Err()
		}
	}
	return networkaction.Actor{}, context.Canceled
}
