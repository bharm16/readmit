package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

// Open verifies and re-evaluates only retained evidence. It never reconstructs
// live execution configuration, resumes an intent or reconnects to a source.
func Open(ctx context.Context, directory string) (Result, error) {
	files, err := artifactdir.Read(directory, family.Layout)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(Schema, files) {
		return Result{}, invalid
	}
	var r, start Result
	if json.Unmarshal(files["manifest.json"], &r, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || r.Schema != Schema || !safeID(r.Instance) || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) {
		return Result{}, invalid
	}
	plan, err := connectedtest.OpenPlan(filepath.Join(directory, "plan"))
	if err != nil || plan.Identity() != r.Plan {
		return Result{}, invalid
	}
	expected := Result{Schema: Schema, Plan: r.Plan, Instance: r.Instance, State: "incomplete", Verdict: assertion.VerdictUndecided, Phase: "arming", StartedAt: r.StartedAt, Observations: map[string]string{}, Armed: map[string]string{}}
	a, _ := json.Marshal(expected, json.Deterministic(true))
	b, _ := json.Marshal(start, json.Deterministic(true))
	if !bytes.Equal(a, b) {
		return Result{}, invalid
	}
	latestArmed := r.StartedAt
	definitions := map[string]connectedtest.Dataset{}
	for _, d := range plan.Document().Test.Datasets {
		definitions[d.ID] = d
	}
	for id, identity := range r.Armed {
		d, ok := definitions[id]
		if !ok {
			return Result{}, invalid
		}
		name := "armed-" + id
		if d.Phase == "before" {
			name = "before-" + id
		}
		snapshot, err := openAcquisition(ctx, plan, d, filepath.Join(directory, "observations", name))
		if err != nil || !snapshot.Usable() || snapshot.Identity() != identity {
			return Result{}, invalid
		}
		acquisition := snapshot.Document().Acquisition
		if acquisition.StartedAt.Before(r.StartedAt) || acquisition.CompletedAt.After(r.CompletedAt) {
			return Result{}, invalid
		}
		binding := snapshot.Document().Binding
		if binding.Run != r.Instance || binding.Source != d.Source || binding.Namespace != d.Namespace || binding.Phase != "before" {
			return Result{}, invalid
		}
		if acquisition.CompletedAt.After(latestArmed) {
			latestArmed = acquisition.CompletedAt
		}
	}
	for id, name := range r.Observations {
		d, ok := definitions[id]
		if !ok || name != d.Phase+"-"+id {
			return Result{}, invalid
		}
		snapshot, err := openAcquisition(ctx, plan, d, filepath.Join(directory, "observations", name))
		if err != nil || !snapshot.Usable() {
			return Result{}, invalid
		}
		acquisition := snapshot.Document().Acquisition
		if acquisition.StartedAt.Before(r.StartedAt) || acquisition.CompletedAt.After(r.CompletedAt) {
			return Result{}, invalid
		}
		binding := snapshot.Document().Binding
		if binding.Run != r.Instance || binding.Source != d.Source || binding.Namespace != d.Namespace || binding.Phase != d.Phase {
			return Result{}, invalid
		}
		if d.Phase == "after" && (r.SentAt.IsZero() || snapshot.Document().Acquisition.StartedAt.Before(r.SentAt.Add(duration(d.Completion.HorizonMS)))) {
			return Result{}, invalid
		}
	}
	var intent networkaction.Binding
	if raw, exists := files["stimulus-intent.json"]; exists {
		if json.Unmarshal(raw, &intent, json.RejectUnknownMembers(true)) != nil || intent.Plan != r.Plan || len(r.Armed) != len(definitions) {
			return Result{}, invalid
		}
	} else if r.Transport != "" || !r.SentAt.IsZero() || r.Evaluation != "" || r.Phase != "arming" {
		return Result{}, invalid
	}
	if r.Transport != "" {
		receipt, err := connectedtransport.Open(filepath.Join(directory, "transport"))
		if err != nil || receipt.Binding.Plan != r.Plan || receipt.Instance != r.Instance || receipt.RunIdentity != r.Transport || receipt.Binding != intent || receipt.State == "uncertain" && r.State != "uncertain" {
			return Result{}, invalid
		}
		run, err := replay.Open(filepath.Join(directory, "transport", "run"))
		if err != nil || r.SentAt.Before(run.Manifest.CompletedAt) || r.SentAt.After(r.CompletedAt) || run.Manifest.StartedAt.Before(latestArmed) {
			return Result{}, invalid
		}
	}
	if r.Evaluation == "" {
		if r.Verdict != assertion.VerdictUndecided || r.State == "complete" {
			return Result{}, invalid
		}
		switch r.State {
		case "incomplete", "cancelled", "uncertain":
		default:
			return Result{}, invalid
		}
		return r, nil
	}
	if len(r.Armed) != len(definitions) || len(r.Observations) != len(definitions) || r.Transport == "" || r.Phase != "finished" {
		return Result{}, invalid
	}
	evaluated, err := connectedtest.OpenDatasetResult(ctx, filepath.Join(directory, "evaluation"))
	if err != nil {
		return Result{}, err
	}
	raw, _ := json.Marshal(evaluated, json.Deterministic(true))
	if networkaction.Digest(raw) != r.Evaluation || evaluated.PlanIdentity != r.Plan || evaluated.Execution.Instance != r.Instance || evaluated.Execution.State != r.State || evaluated.Verdict != r.Verdict {
		return Result{}, invalid
	}
	// The result must use precisely the acquisitions this run recorded, not an
	// unrelated usable snapshot with compatible names.
	for id, name := range r.Observations {
		snapshot, err := observesource.OpenDataset(ctx, filepath.Join(directory, "observations", name))
		if err != nil || evaluated.Report.Evidence[id] != snapshot.Identity() {
			return Result{}, invalid
		}
	}
	run, err := replay.Open(filepath.Join(directory, "transport", "run"))
	if err != nil {
		return Result{}, err
	}
	if len(run.Events) != len(evaluated.Execution.Attempts) {
		return Result{}, invalid
	}
	for i, e := range run.Events {
		attempt := evaluated.Execution.Attempts[i]
		if attempt.Step != plan.Document().Order[i] || attempt.Kind != "v2-send" || (attempt.Outcome == "complete") != (e.Delivery == "acknowledged") || attempt.Uncertain != (e.Delivery == "uncertain") {
			return Result{}, invalid
		}
	}
	return r, nil
}

// Every acquisition, including readiness probes not used by assertions, must
// implement the compiled projection and the same scoped observation authority.
func openAcquisition(ctx context.Context, plan *connectedtest.Plan, definition connectedtest.Dataset, path string) (*dataset.Snapshot, error) {
	snapshot, err := observesource.OpenDataset(ctx, path)
	if err != nil || definition.Projection == nil {
		return nil, invalid
	}
	projection, err := dataset.DecodeProjection(plan.Files()["dependencies/"+definition.Projection.SHA256])
	if err != nil || snapshot.Document().Projection.Identity() != projection.Identity() {
		return nil, invalid
	}
	var action networkaction.Result
	switch snapshot.Document().Acquisition.Kind {
	case "http":
		action, err = networkaction.OpenHTTP(filepath.Join(path, "network"))
	case "database":
		action, err = observesource.OpenDatabaseAction(filepath.Join(path, "network"))
	default:
		return snapshot, nil
	}
	b, env := action.Binding, plan.Document().Environment
	if err != nil || b.Plan != plan.Identity() || b.Project != env.Project || b.Environment != env.ID || b.Revision != env.Revision || b.Endpoint != definition.ID || b.Policy != env.AddressPolicyIdentity || b.Source != definition.Source || b.Operation != sendpolicy.ObservationRead {
		return nil, invalid
	}
	return snapshot, nil
}
