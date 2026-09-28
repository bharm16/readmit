package desktop

import (
	"context"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/testisolation"
)

// ConnectedLifecycleView is the redacted outcome of one connected lifecycle:
// states, verdicts, declared observation boundaries and per-check outcomes. It
// carries no observed value, server-assigned identity, private address or
// machine path; the retained result holds those as private evidence.
type ConnectedLifecycleView struct {
	Output        string                   `json:"output"`
	State         string                   `json:"state"`
	Verdict       assertion.Verdict        `json:"verdict"`
	Boundary      string                   `json:"boundary"`
	Setup         string                   `json:"setup"`
	Cleanup       string                   `json:"cleanup"`
	Qualification []connectedrun.FlowClaim `json:"qualification"`
	Phases        []ConnectedPhaseView     `json:"phases"`
}
type ConnectedPhaseView struct {
	ID      string                   `json:"id"`
	State   string                   `json:"state"`
	Verdict assertion.Verdict        `json:"verdict"`
	Checks  []connectedrun.FlowCheck `json:"checks"`
	Steps   []connectedtest.Attempt  `json:"steps"`
}

// connectedLifecycleRequest names a saved lifecycle plan and its selected
// runtime configuration by project entries, the instance, an optional new
// output entry and the manual setup steps confirmed now.
type connectedLifecycleRequest struct {
	Context  RequestContext
	Plan     string
	Config   string
	Instance string
	Output   string
	Confirm  []string
}

// runConnectedLifecycle is the facade's adapter onto the one connected
// lifecycle service the command line's `test --connected-config` runs. It runs
// only inside an operation already admitted for execution, reads only project
// entries and writes one new project entry; the answer is the retained result
// reopened offline, not a second result model. Grants, network policy and
// isolation authority are the configuration's own, checked at every effect.
// The redesigned Run and Report owners bind it; no facade method does yet.
func (a *App) runConnectedLifecycle(ctx context.Context, request connectedLifecycleRequest) (ConnectedLifecycleView, refusal) {
	guard, _ := a.selectedOperation()
	if guard.CheckExecutionContext(ctx) != nil {
		return ConnectedLifecycleView{}, refusal{Failed, "a connected lifecycle runs only inside an operation admitted for execution"}
	}
	root, declined := a.projectRoot(ctx, request.Context)
	if root == "" {
		return ConnectedLifecycleView{}, declined
	}
	plan, err := artifactpath.Child(root, request.Plan)
	if err != nil {
		return ConnectedLifecycleView{}, refusal{Failed, "the saved lifecycle plan is not an entry of the project"}
	}
	config, err := artifactpath.File(root, request.Config)
	if err != nil {
		return ConnectedLifecycleView{}, refusal{Failed, "the lifecycle configuration is not an entry of the project"}
	}
	destination, declined := destinationFor(root, request.Output, "lifecycle")
	if destination.Name == "" {
		return ConnectedLifecycleView{}, declined
	}
	prepared, err := connectedrun.PrepareFlow(plan, config, request.Instance)
	if err != nil {
		return ConnectedLifecycleView{}, refusal{Failed, "the saved lifecycle and its configuration do not prepare; nothing was sent"}
	}
	confirmation := testisolation.Confirmation{Plan: prepared.IsolationIdentity(), Instance: request.Instance, Steps: request.Confirm}
	result, err := connectedrun.ExecuteFlow(ctx, prepared, filepath.Join(root, destination.Name), confirmation)
	if err != nil {
		return ConnectedLifecycleView{}, refusal{Failed, "the lifecycle stopped; its retained evidence must be inspected and nothing is resent automatically"}
	}
	return connectedLifecycleView(destination.Name, result), refusal{}
}

// openConnectedLifecycle reopens a retained lifecycle result offline; nothing
// is contacted, resolved or resent.
func (a *App) openConnectedLifecycle(ctx context.Context, request RequestContext, output string) (ConnectedLifecycleView, refusal) {
	root, declined := a.projectRoot(ctx, request)
	if root == "" {
		return ConnectedLifecycleView{}, declined
	}
	path, err := artifactpath.Child(root, output)
	if err != nil {
		return ConnectedLifecycleView{}, refusal{Failed, "the lifecycle result is not an entry of the project"}
	}
	result, err := connectedrun.OpenFlow(ctx, path)
	if err != nil {
		return ConnectedLifecycleView{}, refusal{Failed, "the retained lifecycle result cannot be verified"}
	}
	return connectedLifecycleView(output, result), refusal{}
}

func connectedLifecycleView(output string, r connectedrun.FlowResult) ConnectedLifecycleView {
	view := ConnectedLifecycleView{Output: output, State: r.State, Verdict: r.Verdict, Boundary: r.Boundary, Setup: r.Setup, Cleanup: r.Cleanup, Qualification: r.Qualification, Phases: []ConnectedPhaseView{}}
	if view.Qualification == nil {
		view.Qualification = []connectedrun.FlowClaim{}
	}
	for _, p := range r.Phases {
		view.Phases = append(view.Phases, ConnectedPhaseView{ID: p.ID, State: p.State, Verdict: p.Verdict, Checks: p.Checks, Steps: p.Steps})
	}
	return view
}
