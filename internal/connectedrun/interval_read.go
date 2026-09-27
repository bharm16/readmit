package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"path/filepath"
	"strings"
	"time"
)

func openIntervals(ctx context.Context, directory string) (Result, error) {
	files, err := artifactdir.Read(directory, intervalFamily.Layout)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(SchemaV2, files) {
		return Result{}, invalid
	}
	var record IntervalRun
	var start Result
	if json.Unmarshal(files["manifest.json"], &record, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["started.json"], &start, json.RejectUnknownMembers(true)) != nil || record.Schema != SchemaV2 {
		return Result{}, invalid
	}
	r := record.Summary
	if r.Schema != SchemaV2 || !safeID(r.Instance) || r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) {
		return Result{}, invalid
	}
	plan, err := connectedtest.OpenPlan(filepath.Join(directory, "plan"))
	if err != nil || plan.Identity() != r.Plan || plan.Document().Schema != connectedtest.PlanSchemaV3 || !artifactdir.MatchesSubtree(files, "plan", connectedtest.PlanSchemaV3, artifactdir.Identity(plan.Document().Schema, plan.Files())) {
		return Result{}, invalid
	}
	expected := Result{Schema: SchemaV2, Plan: r.Plan, Instance: r.Instance, State: "incomplete", Verdict: assertion.VerdictUndecided, Phase: "arming", StartedAt: r.StartedAt, Observations: map[string]string{}, Armed: map[string]string{}}
	a, _ := json.Marshal(expected, json.Deterministic(true))
	b, _ := json.Marshal(start, json.Deterministic(true))
	if !bytes.Equal(a, b) {
		return Result{}, invalid
	}
	definitions := map[string]connectedtest.Dataset{}
	policies := map[string]observeinterval.Definition{}
	projections := map[string]dataset.Projection{}
	for _, d := range plan.Document().Test.Datasets {
		definitions[d.ID] = d
		if d.Completion.Policy == nil || d.Projection == nil {
			return Result{}, invalid
		}
		policy, e := observeinterval.Decode(plan.Files()["dependencies/"+d.Completion.Policy.SHA256])
		if e != nil {
			return Result{}, e
		}
		policies[d.ID] = policy
		projection, e := dataset.DecodeProjection(plan.Files()["dependencies/"+d.Projection.SHA256])
		if e != nil {
			return Result{}, e
		}
		projections[d.ID] = projection
		if policy.Barrier != nil {
			bd := connectedtest.Dataset{ID: d.ID + "-barrier", Source: policy.Barrier.Source, Namespace: "processing-barrier", Phase: "after"}
			definitions[bd.ID] = bd
			projections[bd.ID] = policy.Barrier.Projection
		}
	}
	intervals := map[string]observeinterval.Result{}
	acquired := map[string]map[string]*dataset.Snapshot{}
	for id, names := range record.Acquisitions {
		d, ok := definitions[id]
		if !ok || len(names) > 4098 {
			return Result{}, invalid
		}
		acquired[id] = map[string]*dataset.Snapshot{}
		for _, name := range names {
			if !safeID(name) {
				return Result{}, invalid
			}
			path := filepath.Join(directory, "observations", name)
			snapshot, e := observesource.OpenDataset(ctx, path)
			if e != nil {
				return Result{}, e
			}
			doc := snapshot.Document()
			if !withinRunIO(r, doc.Acquisition.StartedAt, doc.Acquisition.CompletedAt) {
				return Result{}, invalid
			}
			if doc.Binding.Run != r.Instance || doc.Binding.Source != d.Source || doc.Binding.Namespace != d.Namespace || doc.Binding.Phase != "before" && doc.Binding.Phase != d.Phase || doc.Projection.Identity() != projections[id].Identity() {
				return Result{}, invalid
			}
			initial := artifactdir.Subtree(files, "observations/"+name)
			var receipt struct {
				Schema   string          `json:"schema"`
				Binding  dataset.Binding `json:"binding"`
				Identity string          `json:"dataset_identity"`
			}
			if json.Unmarshal(initial["manifest.json"], &receipt, json.RejectUnknownMembers(true)) != nil || receipt.Identity != snapshot.Identity() || receipt.Binding != doc.Binding || receipt.Schema != observesource.DatasetAcquisitionSchema && receipt.Schema != observesource.ScopedDatasetAcquisitionSchema || !artifactdir.MatchesSubtree(files, "observations/"+name, receipt.Schema, strings.TrimSpace(string(initial["identity.sha256"]))) || !artifactdir.MatchesSubtree(initial, "dataset", dataset.Schema, snapshot.Identity()) {
				return Result{}, invalid
			}
			if err := verifyAcquisitionAuthority(plan, id, d.Source, initial, snapshot); err != nil {
				return Result{}, err
			}
			acquired[id][snapshot.Identity()] = snapshot
		}
	}
	evidence := map[string]*dataset.Snapshot{}
	expectedBoundaries := map[string]string{}
	primary := map[string]bool{}
	for _, d := range plan.Document().Test.Datasets {
		primary[d.ID] = true
		expectedBoundaries[d.ID] = "not-armed"
	}
	for id := range record.Intervals {
		if !primary[id] || definitions[id].Phase != "after" {
			return Result{}, invalid
		}
	}
	for id := range r.Armed {
		if !primary[id] {
			return Result{}, invalid
		}
	}
	for id := range r.Observations {
		if !primary[id] {
			return Result{}, invalid
		}
	}
	allComplete := true
	for _, d := range plan.Document().Test.Datasets {
		if d.Phase == "before" {
			if name, ok := r.Observations[d.ID]; ok {
				if name != "before-"+d.ID {
					return Result{}, invalid
				}
				snapshot, e := observesource.OpenDataset(ctx, filepath.Join(directory, "observations", name))
				if e != nil || acquired[d.ID][snapshot.Identity()] == nil || r.Armed[d.ID] != snapshot.Identity() {
					return Result{}, invalid
				}
				evidence[d.ID] = snapshot
				if d.Phase == "before" {
					expectedBoundaries[d.ID] = "verified-baseline-snapshot"
				}
			} else {
				allComplete = false
			}
			continue
		}
		identity, ok := record.Intervals[d.ID]
		if !ok {
			if r.Phase != "arming" {
				expectedBoundaries[d.ID] = "insufficient"
			}
			allComplete = false
			continue
		}
		path := filepath.Join(directory, "intervals", d.ID)
		interval, e := observeinterval.Open(ctx, path)
		if e != nil || interval.Identity != identity || interval.Binding.Run != r.Instance || interval.Binding.Source != d.Source || interval.Binding.Namespace != d.Namespace || interval.Binding.Phase != "after" || !artifactdir.MatchesSubtree(files, "intervals/"+d.ID, observeinterval.ResultSchema, interval.Identity) {
			return Result{}, invalid
		}
		encoded, _ := json.Marshal(interval.Definition, json.Deterministic(true))
		wanted, _ := json.Marshal(policies[d.ID], json.Deterministic(true))
		if !bytes.Equal(encoded, wanted) {
			return Result{}, invalid
		}
		expectedBoundaries[d.ID] = interval.Boundary
		if policies[d.ID].Mode == "snapshots" && (len(interval.Records) == 0 || r.Armed[d.ID] != interval.Records[0].Identity) {
			return Result{}, invalid
		}
		intervals[d.ID] = interval
		for _, sample := range interval.Records {
			if !withinRunIO(r, sample.RecordedAt, sample.RecordedAt) {
				return Result{}, invalid
			}
		}
		if !interval.Sufficient() {
			allComplete = false
		}
		for _, sample := range interval.Records {
			if sample.Barrier != nil && acquired[d.ID+"-barrier"][sample.Barrier.Evidence] == nil {
				return Result{}, invalid
			}
			if sample.Snapshot != "" && sample.CapturePath == "" && acquired[d.ID][sample.Identity] == nil {
				return Result{}, invalid
			}
			if sample.CapturePath != "" {
				capture, e := networkaction.OpenCaptureEvidence(filepath.Join(path, sample.CapturePath))
				action := capture.Result
				if capture.Capture == nil || capture.Capture.Manifest.Provenance.StartedAt == nil || !withinRunIO(r, *capture.Capture.Manifest.Provenance.StartedAt, *capture.Capture.Manifest.Provenance.StartedAt) {
					return Result{}, invalid
				}
				for _, event := range capture.Capture.Events {
					if event.ObservedAt != nil && !withinRunIO(r, *event.ObservedAt, *event.ObservedAt) {
						return Result{}, invalid
					}
				}
				if e != nil || !captureBindingMatches(plan, d, action.Binding) || r.Armed[d.ID] != action.Binding.Configuration || !artifactdir.MatchesSubtree(files, "intervals/"+d.ID+"/"+sample.CapturePath, networkaction.ResultSchema, capture.Identity) {
					return Result{}, invalid
				}
			}
		}
		if interval.FinalSnapshot != "" {
			expectedPath := "intervals/" + d.ID + "/" + interval.FinalSnapshot
			if r.Observations[d.ID] != expectedPath {
				return Result{}, invalid
			}
			snapshot, e := dataset.Open(ctx, filepath.Join(directory, expectedPath))
			if e != nil || snapshot.Document().Projection.Identity() != projections[d.ID].Identity() || !artifactdir.MatchesSubtree(files, expectedPath, dataset.Schema, snapshot.Identity()) {
				return Result{}, invalid
			}
			acquisition := snapshot.Document().Acquisition
			if !withinRunIO(r, acquisition.StartedAt, acquisition.CompletedAt) {
				return Result{}, invalid
			}
			evidence[d.ID] = snapshot
		}
	}
	a, _ = json.Marshal(expectedBoundaries, json.Deterministic(true))
	b, _ = json.Marshal(record.Boundaries, json.Deterministic(true))
	if !bytes.Equal(a, b) {
		return Result{}, invalid
	}
	r.boundaries = expectedBoundaries
	var intent networkaction.Binding
	if raw, ok := files["stimulus-intent.json"]; ok {
		if json.Unmarshal(raw, &intent, json.RejectUnknownMembers(true)) != nil || intent.Plan != r.Plan || len(r.Armed) != len(plan.Document().Test.Datasets) {
			return Result{}, invalid
		}
	} else if r.Transport != "" || r.Evaluation != "" || r.Phase != "arming" {
		return Result{}, invalid
	}
	var finish stimulusFinish
	if raw, ok := files["stimulus-finished.json"]; ok {
		if json.Unmarshal(raw, &finish, json.RejectUnknownMembers(true)) != nil || finish.Plan != r.Plan || finish.Instance != r.Instance || finish.Transport != r.Transport || !finish.At.Equal(r.SentAt) || !withinRunIO(r, finish.At, finish.At) {
			return Result{}, invalid
		}
	} else if !r.SentAt.IsZero() || r.Transport != "" || r.Evaluation != "" {
		return Result{}, invalid
	}
	if r.Transport != "" {
		transportEvidence, e := connectedtransport.OpenEvidence(filepath.Join(directory, "transport"))
		transport := transportEvidence.Receipt
		encoded, _ := json.Marshal(transport, json.Deterministic(true))
		if e != nil || !artifactdir.MatchesSubtree(files, "transport", connectedtransport.ReceiptSchema, transportEvidence.Identity) || !bytes.Equal(encoded, files["transport/receipt.json"]) || transport.Binding.Configuration != artifactdir.Identity("readmit-connected-configuration/v1", artifactdir.Subtree(files, "transport/configuration")) || !artifactdir.MatchesSubtree(files, "transport/plan", connectedtest.PlanSchemaV3, artifactdir.Identity(plan.Document().Schema, plan.Files())) || !artifactdir.MatchesSubtree(files, "transport/run", replay.Schema, transport.RunIdentity) || transport.Instance != r.Instance || transport.Binding != intent || transport.RunIdentity != r.Transport || transport.State == "uncertain" && r.State != "uncertain" || finish.State != transport.State {
			return Result{}, invalid
		}
		run, e := replay.Open(filepath.Join(directory, "transport", "run"))
		if e != nil || !artifactdir.MatchesSubtree(files, "transport/run", replay.Schema, run.Identity) || !withinRunIO(r, run.Manifest.StartedAt, run.Manifest.CompletedAt) || r.SentAt.Before(run.Manifest.CompletedAt) || r.SentAt.After(r.CompletedAt) {
			return Result{}, invalid
		}
		for _, d := range plan.Document().Test.Datasets {
			if d.Phase == "before" {
				if snapshot := evidence[d.ID]; snapshot != nil && snapshot.Document().Acquisition.CompletedAt.After(run.Manifest.StartedAt) {
					return Result{}, invalid
				}
				continue
			}
			interval, ok := intervals[d.ID]
			if !ok {
				continue
			}
			var begun, finished time.Time
			for _, sample := range interval.Records {
				switch sample.Kind {
				case "baseline":
					if sample.RecordedAt.After(run.Manifest.StartedAt) {
						return Result{}, invalid
					}
				case "stimulus-started":
					begun = sample.RecordedAt
					if begun.After(run.Manifest.StartedAt) {
						return Result{}, invalid
					}
				case "stimulus-finished":
					finished = sample.RecordedAt
					if finished.Before(run.Manifest.CompletedAt) || finished.Before(r.SentAt) {
						return Result{}, invalid
					}
				}
				if sample.Snapshot != "" {
					snapshot, e := dataset.Open(ctx, filepath.Join(directory, "intervals", d.ID, sample.Snapshot))
					if e != nil || !artifactdir.MatchesSubtree(files, "intervals/"+d.ID+"/"+sample.Snapshot, dataset.Schema, snapshot.Identity()) {
						return Result{}, invalid
					}
					a := snapshot.Document().Acquisition
					if !withinRunIO(r, a.StartedAt, a.CompletedAt) || a.CompletedAt.After(sample.RecordedAt) || sample.Kind == "baseline" && a.CompletedAt.After(run.Manifest.StartedAt) {
						return Result{}, invalid
					}
				}
				if sample.BarrierPath != "" {
					snapshot, e := dataset.Open(ctx, filepath.Join(directory, "intervals", d.ID, sample.BarrierPath))
					if e != nil || !artifactdir.MatchesSubtree(files, "intervals/"+d.ID+"/"+sample.BarrierPath, dataset.Schema, snapshot.Identity()) {
						return Result{}, invalid
					}
					a := snapshot.Document().Acquisition
					if !withinRunIO(r, a.StartedAt, a.CompletedAt) || a.CompletedAt.After(sample.RecordedAt) {
						return Result{}, invalid
					}
				}
			}
			if interval.Sufficient() {
				snapshot := evidence[d.ID]
				if snapshot == nil || begun.IsZero() || finished.IsZero() || snapshot.Document().Acquisition.StartedAt.Before(finished) || snapshot.Document().Acquisition.StartedAt.Before(run.Manifest.CompletedAt) {
					return Result{}, invalid
				}
			}
		}
	}
	if r.Evaluation == "" {
		if r.Verdict != assertion.VerdictUndecided || r.State != "incomplete" && r.State != "uncertain" {
			return Result{}, invalid
		}
		return r, nil
	}
	if !allComplete || len(evidence) != len(plan.Document().Test.Datasets) || r.Transport == "" || r.Phase != "finished" {
		return Result{}, invalid
	}
	evaluated, e := connectedtest.OpenDatasetResult(ctx, filepath.Join(directory, "evaluation"))
	if e != nil {
		return Result{}, e
	}
	encoded, _ := json.Marshal(evaluated, json.Deterministic(true))
	if !bytes.Equal(encoded, files["evaluation/result.json"]) || !artifactdir.MatchesSubtree(files, "evaluation/plan", connectedtest.PlanSchemaV3, artifactdir.Identity(plan.Document().Schema, plan.Files())) || dataset.Digest(encoded) != r.Evaluation || evaluated.PlanIdentity != plan.Identity() || evaluated.Execution.Instance != r.Instance || evaluated.Execution.State != r.State || evaluated.Verdict != r.Verdict {
		return Result{}, invalid
	}
	for id, snapshot := range evidence {
		if evaluated.Report.Evidence[id] != snapshot.Identity() || !artifactdir.MatchesSubtree(files, "evaluation/datasets/"+id, dataset.Schema, snapshot.Identity()) {
			return Result{}, invalid
		}
	}
	run, e := replay.Open(filepath.Join(directory, "transport", "run"))
	if e != nil || len(run.Events) != len(evaluated.Execution.Attempts) {
		return Result{}, invalid
	}
	for i, event := range run.Events {
		attempt := evaluated.Execution.Attempts[i]
		if attempt.Step != plan.Document().Order[i] || attempt.Kind != "v2-send" || (attempt.Outcome == "complete") != (event.Delivery == "acknowledged") || attempt.Uncertain != (event.Delivery == "uncertain") {
			return Result{}, invalid
		}
	}
	return r, nil
}
func verifyAcquisitionAuthority(plan *connectedtest.Plan, id, source string, files map[string][]byte, snapshot *dataset.Snapshot) error {
	doc := snapshot.Document()
	kind := doc.Acquisition.Kind
	var action networkaction.Result
	var err error
	switch kind {
	case "http":
		action, err = networkaction.VerifyHTTP(artifactdir.Subtree(files, "network"))
	case "database":
		action, err = observesource.VerifyDatabaseAction(artifactdir.Subtree(files, "network"))
	default:
		return nil
	}
	if snapshot.Usable() && (action.State != "responded" || !action.ResponseRetained || action.ResponseDigest != doc.Material.SHA256 || kind == "http" && action.HTTPStatus != 200) {
		return invalid
	}
	b, env := action.Binding, plan.Document().Environment
	if err != nil || b.Plan != plan.Identity() || b.Source != source || b.Endpoint != id || b.Project != env.Project || b.Environment != env.ID || b.Revision != env.Revision || b.Policy != env.AddressPolicyIdentity || b.Operation != sendpolicy.ObservationRead {
		return invalid
	}
	return nil
}
func captureBindingMatches(plan *connectedtest.Plan, d connectedtest.Dataset, b networkaction.Binding) bool {
	env := plan.Document().Environment
	return b.Plan == plan.Identity() && b.Source == d.Source && b.Endpoint == d.ID && b.Project == env.Project && b.Environment == env.ID && b.Revision == env.Revision && b.Policy == env.AddressPolicyIdentity && b.Operation == sendpolicy.CaptureListen
}

func withinRunIO(r Result, start, end time.Time) bool {
	return !start.IsZero() && !end.IsZero() && !end.Before(start) && !start.Before(r.StartedAt) && !end.After(r.CompletedAt)
}
