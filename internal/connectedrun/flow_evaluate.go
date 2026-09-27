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
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/replay"
)

func evaluateFlowPhase(ctx context.Context, plan *connectedtest.FlowPlan, phase connectedtest.FlowPhase, run Result, path string) (FlowPhaseResult, error) {
	r := unexecutedPhase(plan, phase, run.State)
	evidence, e := OpenEvidence(ctx, path)
	r.RunIdentity = evidence.Identity
	if e != nil || !bytes.Equal(canonicalFlow(evidence.Result), canonicalFlow(run)) {
		return r, invalid
	}
	if run.Transport != "" {
		transport, e := replay.Open(filepath.Join(path, "transport", "run"))
		if e != nil || transport.Identity != run.Transport || len(transport.Events) != len(r.Steps) {
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
	typed, err := connectedtest.OpenDatasetResult(ctx, filepath.Join(path, "evaluation"))
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
		evidence, err := flowWireEvidence(ctx, plan, phase, run, path)
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
	r.Verdict = assertion.VerdictUndecided
	passed, failed, undecided := 0, 0, false
	for _, c := range r.Checks {
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
		r.Verdict = assertion.VerdictFail
	} else if passed > 0 && !undecided && run.State == "complete" {
		r.Verdict = assertion.VerdictPass
	}
	return r, nil
}
func flowWireEvidence(ctx context.Context, plan *connectedtest.FlowPlan, phase connectedtest.FlowPhase, result Result, path string) (assertion.Evidence, error) {
	evidence := assertion.Evidence{Input: map[string]assertion.Message{}, Observed: map[string]assertion.Message{}}
	run, err := replay.Open(filepath.Join(path, "transport", "run"))
	if err != nil || run.Identity != result.Transport {
		return evidence, invalid
	}
	evaluated, e := connectedtest.OpenDatasetResult(ctx, filepath.Join(path, "evaluation"))
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
		dir := filepath.Join(path, "observations", name)
		var snap *dataset.Snapshot
		var e error
		if strings.HasPrefix(name, "intervals/") {
			dir = filepath.Join(path, name)
			snap, e = dataset.Open(ctx, dir)
		} else {
			snap, e = observesource.OpenDataset(ctx, dir)
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
		raw, e := (artifactdir.Document{MaxBytes: 64 << 20}).Read(filepath.Join(dir, "dataset", material.Path))
		if e != nil {
			raw, e = (artifactdir.Document{MaxBytes: 64 << 20}).Read(filepath.Join(dir, material.Path))
		}
		if e != nil || dataset.Digest(raw) != material.SHA256 || len(raw) != material.Size {
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
