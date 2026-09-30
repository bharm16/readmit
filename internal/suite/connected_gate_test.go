package suite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestConnectedSuiteGateRetainsApprovedPinsAndReassessesWithoutTargets(t *testing.T) {
	h, path := connectedSuiteFixture(t)
	review, e := suite.ReviewConnectedPromotion(path, "qa", "independent-lab")
	if e != nil {
		t.Fatal(e)
	}
	promotionPath := filepath.Join(h.Root, "promotion.json")
	promotion, e := suite.ApproveConnectedPromotion(path, "qa", "independent-lab", review.Identity(), "Operator", "Approved synthetic QA binding", promotionPath)
	if e != nil {
		t.Fatal(e)
	}
	outputs := []string{filepath.Join(h.Root, "baseline"), filepath.Join(h.Root, "current")}
	for i, output := range outputs {
		r, e := suite.RunConnected(t.Context(), suite.ConnectedRequest{Path: path, Environment: "qa", Output: output, Instance: "gate-" + string(rune('a'+i)), Promotion: promotionPath, PromotionIdentity: promotion.Identity(), Revision: "independent-lab", Execute: func(ctx context.Context, p *connectedrun.PreparedFlow, out string) (connectedrun.FlowResult, error) {
			for name, binding := range p.Bindings() {
				connectedlab.WriteJSON(t, filepath.Join(h.Root, connectedlab.GrantFile(name)), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
			}
			return connectedrun.ExecuteFlow(ctx, p, out, testisolation.Confirmation{})
		}})
		if e != nil || r.ExitCode() != 0 {
			t.Fatalf("literal passing baseline/current: %+v %v", r, e)
		}
	}
	baseline, e := suite.OpenConnectedExecution(t.Context(), outputs[0])
	if e != nil {
		t.Fatal(e)
	}
	current, e := suite.OpenConnectedExecution(t.Context(), outputs[1])
	if e != nil {
		t.Fatal(e)
	}
	test := current.Document.Tests[0]
	coverage := suite.ConnectedCoverageDocument{Schema: suite.ConnectedCoverageSchema, SuiteSHA256: current.Preparation.Suite, Specifications: []suite.ConnectedCoverageSpecification{{Job: test.ID, Plan: current.Queue.Jobs[0].PlanIdentity, Definition: test.Definition, Release: test.ReleaseIdentity}}, Requirements: []suite.Requirement{{ID: "one-encounter", Jobs: []string{"booking"}}}, Exclusions: []suite.Exclusion{}}
	policy := suite.ConnectedGatePolicy{Schema: suite.ConnectedGatePolicySchema, Environment: "qa", Revision: "independent-lab", Engine: current.Preparation.Capabilities.Engine, Promotion: promotion.Identity(), Input: current.Preparation.Input, Baseline: baseline.Identity, Coverage: coverage, MaxBytes: 64 << 20, RetainUntil: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Approver: "Reviewer", Rationale: "Reviewed literal downstream oracle"}
	policyPath := filepath.Join(h.Root, "gate-policy.json")
	connectedlab.WriteJSON(t, policyPath, policy)
	gatePath := filepath.Join(h.Root, "gate")
	gate := suite.RetainConnectedGate(t.Context(), outputs[1], outputs[0], policyPath, policy.Identity(), gatePath, time.Now())
	if gate.ExitCode != 0 {
		t.Fatalf("retained connected gate: %+v", gate)
	}
	h.Lab.Server().Close()
	if e = os.Remove(promotionPath); e != nil {
		t.Fatal(e)
	}
	verified := suite.VerifyConnectedGate(t.Context(), gatePath, policy.Identity(), time.Now())
	if verified.ExitCode != 0 {
		t.Fatalf("offline gate: %+v", verified)
	}
	// Unknown baseline, altered policy and expired retention never pass.
	if r := suite.VerifyConnectedGate(t.Context(), gatePath, test.Definition, time.Now()); r.ExitCode == 0 {
		t.Fatal("wrong reviewed policy identity passed")
	}
	if r := suite.VerifyConnectedGate(t.Context(), gatePath, policy.Identity(), time.Now().Add(2*time.Hour)); r.ExitCode == 0 {
		t.Fatal("expired retained gate passed")
	}
	if e = os.WriteFile(filepath.Join(gatePath, "current", "manifest.json"), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if r := suite.VerifyConnectedGate(t.Context(), gatePath, policy.Identity(), time.Now()); r.ExitCode == 0 {
		t.Fatal("altered actual execution retained a passing gate")
	}
}
