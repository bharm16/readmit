package connectedrun

import (
	"context"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
)

type FlowResume struct {
	prepared  *PreparedFlow
	previous  string
	identity  string
	files     map[string][]byte
	prior     FlowResult
	inherited int
	reviews   map[string]testisolation.Review
}

// PrepareFlowResume is read-only. A sealed stopped writer, a contiguous prefix
// of completed phases and an unchanged ready isolation checkpoint are required.
// An unresolved phase intent is never converted into permission to repeat it.
func PrepareFlowResume(p *PreparedFlow, previous string) (*FlowResume, error) {
	// A v5 lifecycle is inspection-only after a stop: response-bound identities
	// and FHIR effects are never continued from retained evidence.
	if p == nil || p.fhir != nil || p.unchanged() != nil {
		return nil, invalid
	}
	files, err := artifactdir.Read(previous, flowResultFamily.Layout)
	if err != nil {
		return nil, err
	}
	prior, err := openFlowFiles(context.Background(), previous, files)
	if err != nil || prior.Plan != p.plan.Identity() || prior.Instance != p.instance || prior.Previous != "" || p.store == nil || prior.RecoveryStore == nil || *prior.RecoveryStore != *p.store || prior.State == "complete" || prior.State == "uncertain" || prior.Setup != "ready" || prior.Cleanup != "not-started" {
		return nil, invalid
	}

	inherited := 0
	for i, phase := range prior.Phases {
		if phase.State == "complete" && i == inherited {
			inherited++
			continue
		}
		if phase.State != "not-attempted" {
			return nil, invalid
		}
		if _, ok := files["intents/"+phase.ID+".json"]; ok {
			return nil, invalid
		}
	}
	if inherited == len(prior.Phases) {
		return nil, invalid
	}
	for _, phase := range p.plan.Document().Test.Phases {
		if len(phase.IsolationChanges) > 0 {
			return nil, invalid
		}
	}
	_, priorIdentity, verifyErr := testisolation.VerifyReady(p.isolation, artifactdir.Subtree(files, "isolation"))
	if verifyErr != nil {
		return nil, verifyErr
	}
	reviews := map[string]testisolation.Review{}
	for _, role := range []string{"read", "cleanup"} {
		review, e := testisolation.ContinuationReview(p.isolation, filepath.Join(previous, "isolation"), role)
		if e != nil || review.Binding.Configuration != networkaction.Digest([]byte(p.isolation.Identity()+"/"+priorIdentity)) {
			return nil, invalid
		}
		review.Binding = scopeStore(review.Binding, p.store)
		reviews[role] = review
	}
	return &FlowResume{prepared: p, previous: previous, identity: artifactdir.Identity(FlowSchema, files), files: files, prior: prior, inherited: inherited, reviews: reviews}, nil
}
func (p *FlowResume) IsolationReviews() map[string]testisolation.Review {
	return map[string]testisolation.Review{"read": p.reviews["read"], "cleanup": p.reviews["cleanup"]}
}
func (p *FlowResume) ConfirmationIdentity() string { return p.reviews["read"].Identity }

// ResumeFlow never changes the old result. A durable one-use reservation in the trusted store
// prevents two runners from continuing the same never-attempted suffix.
// A failed continuation still consumes that reservation; it is never retried.
func ResumeFlow(ctx context.Context, p *FlowResume, output string, confirmation testisolation.Confirmation) (FlowResult, error) {
	if p == nil {
		return FlowResult{}, invalid
	}
	fresh, err := PrepareFlowResume(p.prepared, p.previous)
	if err != nil || fresh.identity != p.identity {
		return FlowResult{}, invalid
	}
	ctx, cancel := context.WithTimeout(ctx, duration(p.prepared.plan.Document().Test.Limits.DeadlineMS))
	defer cancel()
	w, err := artifactdir.Create(output, flowResultFamily, artifactdir.Durable)
	if err != nil {
		return FlowResult{}, err
	}
	defer w.Close()
	r := initialFlow(p.prepared.plan, p.prepared.instance, time.Now().UTC())
	r.RecoveryStore = p.prepared.store
	r.Previous = p.identity
	r.Inherited = p.inherited
	copy(r.Phases[:r.Inherited], p.prior.Phases[:r.Inherited])
	if put(w, "started.json", r) != nil || w.Mkdir("intents") != nil || w.Mkdir("phases") != nil {
		return FlowResult{}, invalid
	}
	if err = p.prepared.plan.Write(ctx, filepath.Join(w.Path(), "plan")); err != nil {
		return FlowResult{}, err
	}
	for name, raw := range p.files {
		if err = w.WriteFile("previous/"+name, raw); err != nil {
			return FlowResult{}, err
		}
	}
	if err = w.Sync(); err != nil {
		return FlowResult{}, err
	}
	// The original result has finished writing. The trusted-store reservation is a
	// distinct coordination artifact, not a mutation of retained evidence.
	reservation := struct{ Schema, Previous, Output string }{"readmit-connected-resume-reservation/v1", p.identity, output}
	if err = (artifactdir.Document{MaxBytes: 64 << 10}).Create(filepath.Join(p.prepared.store.Path, networkaction.Digest([]byte(p.prior.Plan+"/"+p.prior.Instance))+".resume.json"), canonicalFlow(reservation)); err != nil {
		return FlowResult{}, err
	}
	session, err := testisolation.Continue(ctx, p.prepared.isolation, p.prepared.authorities(), filepath.Join(p.previous, "isolation"), filepath.Join(w.Path(), "continuation"), confirmation)
	if session == nil {
		return FlowResult{}, err
	}
	defer session.Close()
	if err == nil && session.Ready() {
		err = executeFlowPhases(ctx, p.prepared, w, &r, r.Inherited, session.Check, nil, nil)
	}
	if err != nil {
		return FlowResult{}, err
	}
	if ctx.Err() == nil {
		_ = session.Cleanup(ctx)
	}
	r.Setup = session.Result().Setup
	r.Cleanup = session.Result().Cleanup
	r.Isolation = p.prepared.isolation.Identity()
	session.Close()
	summarizeFlow(&r)
	r.CompletedAt = time.Now().UTC()
	if ctx.Err() != nil && r.State != "uncertain" {
		r.State = "cancelled"
		if r.Verdict == assertion.VerdictPass {
			r.Verdict = assertion.VerdictUndecided
		}
	}
	if put(w, "manifest.json", r) != nil {
		return FlowResult{}, invalid
	}
	if _, err = w.Seal(nil); err != nil {
		return FlowResult{}, err
	}
	return OpenFlow(context.WithoutCancel(ctx), w.Path())
}
