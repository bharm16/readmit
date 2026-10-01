package connectedrun

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
)

func evaluateFlowPhase(ctx context.Context, plan *connectedtest.FlowPlan, phase connectedtest.FlowPhase, run Result, path string) (FlowPhaseResult, error) {
	evidence, err := OpenEvidence(ctx, path)
	if err != nil || !bytes.Equal(canonicalFlow(evidence.Result), canonicalFlow(run)) {
		return FlowPhaseResult{}, invalid
	}
	return evaluateOwnedPhase(ctx, plan, phase, evidence)
}
func evaluateOwnedPhase(ctx context.Context, plan *connectedtest.FlowPlan, phase connectedtest.FlowPhase, evidence Evidence) (FlowPhaseResult, error) {
	run := evidence.Result
	files := evidence.files
	r := unexecutedPhase(plan, phase, run.State)
	r.RunIdentity = evidence.Identity
	if run.Transport != "" {
		transport, e := replay.Verify(artifactdir.Subtree(files, "transport/run"))
		if e != nil || transport.Identity != run.Transport || len(transport.Events) != len(r.Steps) {
			return r, invalid
		}
		// A scheduled phase sent on exactly the delays the reviewed plan
		// declares for it; its transport evidence proves each send began no
		// earlier.
		if evidence.Schema != phaseSchemaFor(plan) || !slices.Equal(evidence.Result.schedule, plan.Schedule(phase.ID)) {
			return r, invalid
		}
		for i, event := range transport.Events {
			attempt := connectedtest.Attempt{Step: r.Steps[i].Step, Kind: "v2-send", Outcome: "not-attempted"}
			switch event.Delivery {
			case "acknowledged":
				attempt.Outcome = "complete"
			case "uncertain":
				attempt.Outcome = "unknown"
				attempt.Uncertain = true
			case "not_sent":
				attempt.Outcome = "not-attempted"
			default:
				attempt.Outcome = "unknown"
			}
			r.Steps[i] = attempt
		}
	}
	if run.Evaluation == "" {
		return r, nil
	}
	typed, err := connectedtest.VerifyDatasetResult(ctx, artifactdir.Subtree(files, "evaluation"))
	if err != nil || flowDigest(typed) != run.Evaluation {
		return r, invalid
	}
	for i, c := range typed.Report.Results {
		if r.Checks[i].ID != "typed:"+c.ID {
			return r, invalid
		}
		r.Checks[i].Outcome = c.Outcome
	}
	if phase.Wire != nil {
		evidence, err := flowWireEvidence(ctx, plan, phase, run, files)
		if err != nil {
			r.EvaluationError = "evidence_unavailable"
		} else {
			set, e := assertion.Decode(plan.Dependency(phase.Wire.Set))
			if e != nil {
				return r, e
			}
			report, e := set.Evaluate(ctx, evidence)
			if e != nil {
				r.EvaluationError = "evidence_unavailable"
			} else {
				r.Wire = &report
				for i, c := range report.Results {
					r.Checks[len(typed.Report.Results)+i].Outcome = c.Outcome
				}
			}
		}
	}
	r.Verdict = phaseVerdict(r.Checks, run.State)
	return r, nil
}

// phaseVerdict is a phase's verdict: any failed check fails it, and it passes
// only when every check decided, at least one passed and the phase completed.
func phaseVerdict(checks []FlowCheck, state string) assertion.Verdict {
	passed, failed, undecided := 0, 0, false
	for _, c := range checks {
		switch c.Outcome {
		case assertion.OutcomePassed:
			passed++
		case assertion.OutcomeFailed:
			failed++
		case assertion.OutcomeUndecided:
			undecided = true
		}
	}
	if failed > 0 {
		return assertion.VerdictFail
	}
	if passed > 0 && !undecided && state == "complete" {
		return assertion.VerdictPass
	}
	return assertion.VerdictUndecided
}
func flowWireEvidence(ctx context.Context, plan *connectedtest.FlowPlan, phase connectedtest.FlowPhase, result Result, files map[string][]byte) (assertion.Evidence, error) {
	evidence := assertion.Evidence{Input: map[string]assertion.Message{}, Observed: map[string]assertion.Message{}}
	run, err := replay.Verify(artifactdir.Subtree(files, "transport/run"))
	if err != nil || run.Identity != result.Transport {
		return evidence, invalid
	}
	evaluated, e := connectedtest.VerifyDatasetResult(ctx, artifactdir.Subtree(files, "evaluation"))
	if e != nil || flowDigest(evaluated) != result.Evaluation {
		return evidence, invalid
	}
	parse := func(raw []byte) (assertion.Message, error) {
		doc, e := hl7.Parse(raw, hl7.Options{})
		if e != nil || len(doc.Messages) != 1 {
			return assertion.Message{}, invalid
		}
		return assertion.Message{Document: doc}, nil
	}
	for _, event := range run.Events {
		raw, e := run.Raw(event.Sent)
		if e != nil {
			return evidence, e
		}
		message, e := parse(raw)
		if e != nil {
			return evidence, e
		}
		evidence.Input[event.SourceOccurrence] = message
		if phase.Wire.Observed == "transport-acks" {
			raw, e = run.Raw(event.Received)
			if e != nil {
				return evidence, e
			}
			message, e = parse(raw)
			if e != nil {
				return evidence, e
			}
			key := event.SourceOccurrence
			for alias, step := range phase.Wire.Acknowledgements {
				for i, id := range phase.Steps {
					if id == step && run.Events[i].OutboundOccurrence == event.OutboundOccurrence {
						key = alias
					}
				}
			}
			if _, exists := evidence.Observed[key]; exists {
				return evidence, invalid
			}
			evidence.Observed[key] = message
		}
	}
	snapshots := map[string]*dataset.Snapshot{}
	load := func(id string) (*dataset.Snapshot, string, error) {
		name, ok := result.Observations[id]
		if !ok {
			return nil, "", invalid
		}
		dir := "observations/" + name
		var snap *dataset.Snapshot
		var e error
		if strings.HasPrefix(name, "intervals/") {
			dir = name
			snap, e = dataset.Verify(ctx, artifactdir.Subtree(files, dir))
		} else {
			snap, e = observesource.VerifyDataset(ctx, artifactdir.Subtree(files, dir))
		}
		if e != nil || snap.Identity() != evaluated.Report.Evidence[id] {
			return nil, "", invalid
		}
		snapshots[id] = snap
		return snap, dir, nil
	}
	if phase.Wire.Observed != "transport-acks" {
		snap, dir, e := load(phase.Wire.Observed)
		if e != nil {
			return evidence, e
		}
		material := snap.Document().Material
		raw, ok := files[dir+"/dataset/"+material.Path]
		if !ok {
			raw, ok = files[dir+"/"+material.Path]
		}
		if !ok || dataset.Digest(raw) != material.SHA256 || len(raw) != material.Size {
			return evidence, invalid
		}
		var capture dataset.CaptureRead
		if json.Unmarshal(raw, &capture, json.RejectUnknownMembers(true)) != nil || capture.Schema != dataset.CaptureSchema {
			return evidence, invalid
		}
		for _, row := range capture.Rows {
			message, e := parse(row.Raw)
			if e != nil {
				return evidence, e
			}
			evidence.Observed[row.Occurrence] = message
		}
	}
	for _, item := range []struct {
		binding    *connectedtest.KeyBinding
		collection *assertion.Collection
	}{{phase.Wire.Before, &evidence.Before}, {phase.Wire.After, &evidence.After}} {
		if item.binding == nil {
			continue
		}
		snap := snapshots[item.binding.Dataset]
		if snap == nil {
			snap, _, err = load(item.binding.Dataset)
			if err != nil {
				return evidence, err
			}
		}
		item.collection.Complete = snap.Usable() && (snap.Document().Binding.Phase == "before" || result.Boundaries()[item.binding.Dataset] == "complete")
		// The actual interval boundary map has richer state names; a completed
		// child result already verified every final snapshot and boundary.
		if snap.Document().Binding.Phase == "after" {
			item.collection.Complete = snap.Usable() && result.State == "complete"
		}
		item.collection.Keys = []string{}
		for _, row := range snap.Document().Rows {
			column := -1
			for i, c := range snap.Document().Projection.Columns {
				if c.Name == item.binding.Column {
					column = i
				}
			}
			if column < 0 || column >= len(row.Values) {
				return evidence, invalid
			}
			v := row.Values[column]
			if v.State != "present" || v.Type != "text" {
				return evidence, invalid
			}
			item.collection.Keys = append(item.collection.Keys, v.Text)
		}
	}
	return evidence, nil
}
