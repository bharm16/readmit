package connectedrun

import (
	"context"
	"encoding/json/v2"
	"maps"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
)

type Result struct {
	boundaries   map[string]string
	Schema       string            `json:"schema"`
	Plan         string            `json:"plan_identity"`
	Instance     string            `json:"instance"`
	State        string            `json:"state"`
	Verdict      assertion.Verdict `json:"verdict"`
	Phase        string            `json:"phase"`
	StartedAt    time.Time         `json:"started_at"`
	SentAt       time.Time         `json:"sent_at"`
	CompletedAt  time.Time         `json:"completed_at"`
	Observations map[string]string `json:"observations"`
	Armed        map[string]string `json:"armed"`
	Evaluation   string            `json:"evaluation_identity"`
	Transport    string            `json:"transport_identity"`
}

// Boundaries states the exact sufficient (or missing) coverage for a v2 run.
func (r Result) Boundaries() map[string]string { return maps.Clone(r.boundaries) }

var family = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected execution", AllowedDirectories: []string{"observations"}, Nested: []string{"plan", "transport", "observations", "evaluation"}, RequiredFiles: []string{"manifest.json", "identity.sha256"}, AllowFile: func(n string) bool {
	return n == "started.json" || n == "stimulus-intent.json" || n == "manifest.json" || n == "identity.sha256"
}, MaxFiles: 50000, MaxFileBytes: 64 << 20, MaxBytes: 512 << 20}, Seal: artifactdir.DirectoryHash(Schema)}

func put(w *artifactdir.Writer, name string, v any) error {
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return err
	}
	return w.WriteFile(name, b)
}

type liveAuthority struct {
	prepared  *Prepared
	authority networkaction.Authority
}

func (a liveAuthority) Check(ctx context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if ctx.Err() != nil || a.prepared.unchanged() != nil {
		return networkaction.Actor{}, invalid
	}
	return a.authority.Check(ctx, b)
}

// Execute owns the complete supported run: arm/baseline, send once, wait the
// declared final-state horizon, acquire, evaluate and retain. No resume or retry
// is inferred from an existing directory or an uncertain transport outcome.
func Execute(ctx context.Context, p *Prepared, instance, output string) (Result, error) {
	if p != nil && p.intervals {
		return ExecuteWithClock(ctx, p, instance, output, observeinterval.SystemClock())
	}
	if p == nil || !safeID(instance) || p.unchanged() != nil {
		return Result{}, invalid
	}
	ctx, cancel := context.WithTimeout(ctx, duration(p.plan.Document().Test.Limits.DeadlineMS))
	defer cancel()
	authority := liveAuthority{prepared: p, authority: p.send.authority()}
	if _, err := authority.Check(ctx, p.transport.Binding()); err != nil {
		return Result{}, err
	}
	w, err := artifactdir.Create(output, family, artifactdir.Durable)
	if err != nil {
		return Result{}, err
	}
	defer w.Close()
	r := Result{Schema: Schema, Plan: p.plan.Identity(), Instance: instance, State: "incomplete", Verdict: assertion.VerdictUndecided, Phase: "arming", StartedAt: time.Now().UTC(), Observations: map[string]string{}, Armed: map[string]string{}}
	if put(w, "started.json", r) != nil || w.Mkdir("observations") != nil {
		return Result{}, invalid
	}
	if err := p.plan.Write(ctx, filepath.Join(w.Path(), "plan")); err != nil {
		return Result{}, err
	}
	if err = w.Sync(); err != nil {
		return Result{}, err
	}
	finish := func() (Result, error) {
		r.CompletedAt = time.Now().UTC()
		if ctx.Err() != nil && r.State != "complete" && r.State != "uncertain" {
			r.State = "cancelled"
		}
		if put(w, "manifest.json", r) != nil {
			return Result{}, invalid
		}
		if _, err := w.Seal(nil); err != nil {
			return Result{}, err
		}
		return Open(context.WithoutCancel(ctx), w.Path())
	}
	evidence := map[string]*dataset.Snapshot{}
	acquire := func(s sourcePlan, name, phase string) (*dataset.Snapshot, error) {
		if p.unchanged() != nil || ctx.Err() != nil {
			return nil, invalid
		}
		req := observesource.DatasetRequest{Source: s.source, Projection: s.projection, Binding: dataset.Binding{Run: instance, Phase: phase, Namespace: s.definition.Namespace, Source: s.definition.Source}, Output: filepath.Join(w.Path(), "observations", name), Network: s.http, DatabaseNetwork: s.database, Authorize: func(ctx context.Context) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return p.unchanged()
		}}
		if s.http != nil || s.database != nil {
			req.NetworkAuthority = liveAuthority{prepared: p, authority: s.grant.authority()}
		}
		return observesource.CollectDataset(ctx, req)
	}
	for _, s := range p.sources {
		name := "armed-" + s.definition.ID
		if s.definition.Phase == "before" {
			name = "before-" + s.definition.ID
		}
		snapshot, err := acquire(s, name, "before")
		if err != nil || !snapshot.Usable() {
			return finish()
		}
		r.Armed[s.definition.ID] = snapshot.Identity()
		if s.definition.Phase == "before" {
			evidence[s.definition.ID] = snapshot
			r.Observations[s.definition.ID] = name
		}
	}
	r.Phase = "stimulus"
	if put(w, "stimulus-intent.json", p.transport.Binding()) != nil {
		return Result{}, invalid
	}
	transport, err := connectedtransport.Execute(ctx, p.transport, authority, instance, filepath.Join(w.Path(), "transport"), nil)
	if err != nil {
		r.State = "uncertain"
		return finish()
	}
	r.Transport = transport.RunIdentity
	if transport.State == "uncertain" {
		r.State = "uncertain"
	}
	r.SentAt = time.Now().UTC()
	r.Phase = "horizon"
	for _, s := range p.sources {
		if s.definition.Phase != "after" {
			continue
		}
		remaining := time.Until(r.SentAt.Add(duration(s.definition.Completion.HorizonMS)))
		if remaining > 0 {
			timer := time.NewTimer(remaining)
			select {
			case <-ctx.Done():
				timer.Stop()
				return finish()
			case <-timer.C:
			}
		}
		snapshot, err := acquire(s, "after-"+s.definition.ID, "after")
		if err != nil || !snapshot.Usable() {
			return finish()
		}
		evidence[s.definition.ID] = snapshot
		r.Observations[s.definition.ID] = "after-" + s.definition.ID
	}
	r.Phase = "evaluation"
	if ctx.Err() != nil {
		return finish()
	}
	run, err := replay.Open(filepath.Join(w.Path(), "transport", "run"))
	if err != nil {
		return finish()
	}
	execution := connectedtest.Execution{Instance: instance, State: "complete", Engine: engine.Version(), Setup: "operator-declared", Cleanup: "not-requested", Attempts: []connectedtest.Attempt{}}
	for i, event := range run.Events {
		attempt := connectedtest.Attempt{Step: p.plan.Document().Order[i], Kind: "v2-send", Outcome: "complete"}
		if event.Delivery != "acknowledged" {
			attempt.Outcome = "unknown"
			attempt.Uncertain = event.Delivery == "uncertain"
			execution.State = "uncertain"
		}
		execution.Attempts = append(execution.Attempts, attempt)
	}
	evaluated, err := connectedtest.RetainDatasetResult(ctx, p.plan, execution, evidence, filepath.Join(w.Path(), "evaluation"))
	if err != nil {
		return finish()
	}
	encoded, _ := json.Marshal(evaluated, json.Deterministic(true))
	r.Evaluation = networkaction.Digest(encoded)
	r.Verdict = evaluated.Verdict
	r.State = execution.State
	r.Phase = "finished"
	return finish()
}
