package desktop

import (
	"errors"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/reproducer"
)

// ReproducerRequest is one edit of a reproducer over one verified case.
//
// Identity binds the request to the evidence the grid displayed, exactly as an
// inspection does. Plan is the whole of what has been decided so far: the shell
// holds no session, so every call carries the plan and gets the next one back.
// Step is the operator being added and is read by EditReproducer alone; Output
// is the new folder a build writes into and is read by BuildReproducer alone.
type ReproducerRequest struct {
	Workspace string          `json:"workspace"`
	Case      string          `json:"case"`
	Identity  string          `json:"identity"`
	Plan      reproducer.Plan `json:"plan"`
	Step      reproducer.Step `json:"step,omitzero"`
	Output    string          `json:"output,omitzero"`
}

// Reproducer is the plan and what it means over the evidence: which occurrences
// it retains and why, where every edit lands, and everything a dependency step
// reached and could not settle. Output and Identity are present only after a
// build, and name the folder that was written and the derived case in it.
type Reproducer struct {
	Plan       reproducer.Plan       `json:"plan"`
	Resolution reproducer.Resolution `json:"resolution"`
	Output     string                `json:"output,omitzero"`
	Identity   string                `json:"identity,omitzero"`
}

// ReproducerResult carries one state. Reproducer is present whenever the plan
// resolved, including when it retains nothing, because an empty reproducer is
// the answer to a plan that has not selected anything yet.
type ReproducerResult struct {
	State      State       `json:"state"`
	Reason     string      `json:"reason,omitzero"`
	Reproducer *Reproducer `json:"reproducer,omitzero"`
}

func (r refusal) reproducer() ReproducerResult {
	return ReproducerResult{State: r.state, Reason: r.reason}
}

// EditReproducer adds one step to a plan and reports what the plan now means
// over the case.
//
// The evidence is verified again on every call, the way every read of a case in
// this window is, and the step is applied by the same engine a build applies it
// with — so what the window shows is what a build would write. A step the
// evidence does not support leaves the plan exactly as it was and reports why.
// It runs to completion under the case reader's own limits once it starts, so
// it holds the operation slot but is not interruptible.
func (a *App) EditReproducer(request ReproducerRequest) ReproducerResult {
	return a.changed(request, func(plan reproducer.Plan) (reproducer.Plan, error) {
		return reproducer.Append(plan, request.Step)
	})
}

// UndoReproducer removes the last step of a plan and reports what remains.
//
// A plan is replayed from its steps, so undoing one restores exactly the
// reproducer that existed before it: an occurrence a drop removed comes back
// with the edits that were made to it. Nothing about the removed step is kept.
func (a *App) UndoReproducer(request ReproducerRequest) ReproducerResult {
	return a.changed(request, reproducer.Undo)
}

// changed verifies the case, applies one change to the plan and reports what
// the plan now means. Adding a step and removing the last one differ only in
// that change, so the verification, the operation slot and every refusal around
// them are written here once.
func (a *App) changed(request ReproducerRequest, apply func(reproducer.Plan) (reproducer.Plan, error)) ReproducerResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.reproducer()
	}
	defer release()
	_, source, plan, declined := a.opened(request)
	if source == nil {
		return declined.reproducer()
	}
	changed, err := apply(plan)
	if err != nil {
		return ReproducerResult{State: Failed, Reason: err.Error()}
	}
	return resolved(source, changed)
}

// BuildReproducer writes the reproducer into a new folder of the open
// workspace: a derived case holding what the plan retained, and the
// transformation manifest beside it.
//
// This is the one operation in this window that writes evidence, and it writes
// only new evidence. The case it reads is not touched, the destination must be
// a folder that does not exist yet, and a destination inside any retained
// artifact is refused by the same output policy the command line uses. The
// result is read back from what was actually written.
func (a *App) BuildReproducer(request ReproducerRequest) ReproducerResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.reproducer()
	}
	defer release()
	root, source, plan, declined := a.opened(request)
	if source == nil {
		return declined.reproducer()
	}
	if err := artifactpath.EntryName(request.Output); err != nil {
		return ReproducerResult{State: Failed, Reason: "a reproducer is written to one new entry of the open workspace"}
	}
	destination := artifactpath.JoinReference(root, request.Output)
	casePath, err := artifactpath.Child(root, request.Case)
	if err != nil {
		return ReproducerResult{State: Failed, Reason: "a case must be named by one directory entry of the open workspace"}
	}
	manifest, err := reproducer.Create(source, casePath, plan, destination)
	if errors.Is(err, reproducer.ErrCannotWrite) {
		// A folder this account cannot write is separated from the rest only
		// after the write has already failed, and the probe never touches the
		// destination.
		return probeWriteFailure(root,
			"this account cannot write into the open workspace",
			"the reproducer folder could not be created in the open workspace").reproducer()
	}
	if err != nil {
		return ReproducerResult{State: Failed, Reason: err.Error()}
	}
	return ReproducerResult{State: Completed, Reproducer: &Reproducer{
		Plan:       manifest.Plan,
		Resolution: reproducer.Resolution{Occurrences: manifest.Occurrences, Edits: manifest.Edits, Unresolved: manifest.Unresolved},
		Output:     request.Output,
		Identity:   manifest.Derived.Identity,
	}}
}

// opened verifies the case a request names and returns the plan to work from.
// A request carrying no plan starts one bound to the evidence just verified, so
// the contract a plan declares is written here rather than in the interface.
func (a *App) opened(request ReproducerRequest) (string, *bundle.Bundle, reproducer.Plan, refusal) {
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return "", nil, reproducer.Plan{}, declined
	}
	path, err := artifactpath.Child(root, request.Case)
	if err != nil {
		return "", nil, reproducer.Plan{}, refusal{Failed, "a case must be named by one directory entry of the open workspace"}
	}
	source, err := bundle.Open(path)
	if err != nil {
		return "", nil, reproducer.Plan{}, refusal{Failed, "the case could not be verified as complete, unmodified evidence"}
	}
	if request.Identity == "" || request.Identity != source.Identity {
		return "", nil, reproducer.Plan{}, refusal{Failed, "the case identity changed; reopen the grid before editing a reproducer"}
	}
	plan := request.Plan
	if plan.Schema == "" && plan.Case == "" && len(plan.Steps) == 0 {
		started, err := reproducer.NewPlan(source.Identity)
		if err != nil {
			return "", nil, reproducer.Plan{}, refusal{Failed, err.Error()}
		}
		plan = started
	}
	return root, source, plan, refusal{}
}

// resolved reports what one plan means. A plan retaining nothing is Empty with
// the reason it is empty: a reproducer that holds no occurrence is a state a
// person is in on the way to one, not a failure.
func resolved(source *bundle.Bundle, plan reproducer.Plan) ReproducerResult {
	resolution, err := reproducer.Resolve(source, plan)
	if err != nil {
		return ReproducerResult{State: Failed, Reason: err.Error()}
	}
	described := &Reproducer{Plan: plan, Resolution: resolution}
	if len(resolution.Occurrences) == 0 {
		return ReproducerResult{State: Empty, Reason: "this reproducer retains no occurrence of the case yet", Reproducer: described}
	}
	return ReproducerResult{State: Completed, Reproducer: described}
}
