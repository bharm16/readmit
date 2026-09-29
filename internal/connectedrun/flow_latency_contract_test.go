package connectedrun_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/replay"
)

type slowFlowAuthority struct {
	authority connectedtransport.FileAuthority
	checks    int
}

func (a *slowFlowAuthority) Check(ctx context.Context, binding connectedtransport.Binding) (connectedtransport.Actor, error) {
	a.checks++
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return a.authority.Check(ctx, binding)
	case <-ctx.Done():
		return connectedtransport.Actor{}, ctx.Err()
	}
}

// The phase's real transport repeats authority and selected-file checks during
// admission and again before dialing/writing. Model a loaded runner at that
// seam, rather than assuming a one-second connection budget reaches the target.
func TestFlowFixtureConnectionBudgetIncludesRepeatedAuthorityChecks(t *testing.T) {
	h := newFlowContractHarnessWithTiming(t, false, true)
	p, err := connectedtransport.PrepareSequence(h.plan.Phase("booking"), connectedtransport.Selection{
		Case: filepath.Join(h.root, "case"), Target: filepath.Join(h.root, "target.json"), Policy: filepath.Join(h.root, "policy.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	grant := filepath.Join(h.root, "slow-send-grant.json")
	write(t, grant, networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: "runner", Generation: "1", Binding: p.Binding(), IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	authority := &slowFlowAuthority{authority: connectedtransport.FileAuthority{Path: grant, Actor: "runner", Generation: "1"}}
	out := filepath.Join(h.root, "slow-transport")
	r, err := connectedtransport.Execute(t.Context(), p, authority, "slow-transport", out, nil)
	if err != nil || r.State != "settled" || h.fixture.target.received.Load() != 1 {
		t.Fatalf("slow authority checks stopped a healthy phase before its target behavior: state=%s sends=%d err=%v", r.State, h.fixture.target.received.Load(), err)
	}
	if authority.checks < 3 {
		t.Fatal("the regression did not exercise repeated authority checks")
	}
	run, err := replay.Open(filepath.Join(out, "run"))
	if err != nil || len(run.Events) != 1 || run.Events[0].Delivery != "acknowledged" {
		t.Fatal("the phase did not retain its actual acknowledged send", err)
	}
}
