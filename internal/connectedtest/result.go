package connectedtest

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/hl7"
)

// EvidenceReader is the shared typed, read-only seam for retained observations.
// Live collection and authority belong to an execution adapter, never this reader.
type EvidenceReader interface {
	ReadEvidence(context.Context, Dataset) (Sample, error)
}
type Sample struct {
	Complete       bool
	Messages       map[string][]byte
	Keys           []string
	SourceIdentity string
}
type Evidence struct{ Datasets map[string]Sample }

func (e Evidence) ReadEvidence(_ context.Context, d Dataset) (Sample, error) {
	s, ok := e.Datasets[d.ID]
	if !ok {
		return Sample{}, errors.New("missing observation dataset")
	}
	return s, nil
}

type Attempt struct {
	Step      string `json:"step"`
	Kind      string `json:"kind"`
	Outcome   string `json:"outcome"`
	Uncertain bool   `json:"uncertain"`
}
type Execution struct {
	Instance string    `json:"instance"`
	State    string    `json:"state"`
	Engine   string    `json:"engine"`
	Setup    string    `json:"setup"`
	Cleanup  string    `json:"cleanup"`
	Attempts []Attempt `json:"attempts"`
}
type RetainedSample struct {
	ID             string            `json:"id"`
	Complete       bool              `json:"complete"`
	SourceIdentity string            `json:"source_identity,omitzero"`
	Messages       map[string]Member `json:"messages"`
	Keys           []string          `json:"keys"`
}
type ResultDocument struct {
	Schema          string             `json:"schema"`
	PlanIdentity    string             `json:"plan_identity"`
	CheckIdentity   string             `json:"check_identity"`
	Environment     Environment        `json:"environment"`
	Execution       Execution          `json:"execution"`
	Samples         []RetainedSample   `json:"samples"`
	Verdict         assertion.Verdict  `json:"verdict"`
	Checks          []assertion.Result `json:"checks"`
	EvaluationError string             `json:"evaluation_error,omitzero"`
}
type Result struct {
	document ResultDocument
	plan     *Plan
	files    map[string][]byte
}

func (r *Result) Document() ResultDocument {
	var d ResultDocument
	b, _ := encode(r.document)
	_ = json.Unmarshal(b, &d)
	return d
}

// Evaluate never promotes incomplete orchestration. Per-check outcomes remain
// visible even when a cancelled or uncertain execution cannot yield a pass.
func Evaluate(ctx context.Context, p *Plan, execution Execution, reader EvidenceReader) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, invalid
	}
	if err := validateExecution(p, execution); err != nil {
		return nil, err
	}
	d := p.document.Test
	uncertain := false
	r := &Result{plan: p, files: map[string][]byte{}, document: ResultDocument{Schema: ResultSchema, PlanIdentity: p.Identity(), CheckIdentity: d.Checks.SHA256, Environment: p.document.Environment, Execution: execution}}
	for n, b := range p.Files() {
		r.files["plan/"+n] = b
	}
	evidence := assertion.Evidence{Input: map[string]assertion.Message{}, Observed: map[string]assertion.Message{}}
	for _, s := range d.Steps {
		if s.V2 != nil {
			doc, err := hl7.Parse(p.files["inputs/"+s.ID+".hl7"], hl7.Options{})
			if err != nil {
				return nil, err
			}
			evidence.Input[s.V2.Occurrence] = assertion.Message{Document: doc}
		}
	}
	totalBytes := 0
	totalRecords := 0
	for _, dataset := range d.Datasets {
		sample, err := reader.ReadEvidence(ctx, dataset)
		if err != nil {
			return nil, err
		}
		if len(sample.Messages)+len(sample.Keys) > dataset.Completion.MaxRecords || sample.SourceIdentity != "" && !hash.MatchString(sample.SourceIdentity) {
			return nil, invalid
		}
		if dataset.Kind == "v2-messages" && len(sample.Keys) > 0 || dataset.Kind == "record-keys" && len(sample.Messages) > 0 || dataset.Kind == "fhir-resources" {
			return nil, errors.New("unsupported or incompatible observation evidence")
		}
		totalRecords += len(sample.Messages) + len(sample.Keys)
		if totalRecords > 8192 {
			return nil, errors.New("retained observation record limit")
		}
		// Charge JSON/container overhead before copying caller-owned collections.
		retainedCost := 512 + 512*len(sample.Messages) + 16*len(sample.Keys)
		for _, key := range sample.Keys {
			retainedCost += 6 * len(key)
		}
		totalBytes += retainedCost
		if totalBytes > d.Limits.MaxBytes {
			return nil, errors.New("retained observation metadata byte limit")
		}
		retained := RetainedSample{ID: dataset.ID, Complete: sample.Complete, SourceIdentity: sample.SourceIdentity, Messages: map[string]Member{}, Keys: slices.Clone(sample.Keys)}
		ids := make([]string, 0, len(sample.Messages))
		for id := range sample.Messages {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		size := 0
		for _, id := range ids {
			b := sample.Messages[id]
			size += len(b)
			if !occurrence.MatchString(id) || size > dataset.Completion.MaxBytes {
				return nil, invalid
			}
			doc, err := hl7.Parse(b, hl7.Options{})
			if err != nil || len(doc.Messages) != 1 {
				return nil, errors.New("unreadable observation message")
			}
			name := fmt.Sprintf("observations/%s-%s.hl7", dataset.ID, id)
			r.files[name] = bytes.Clone(b)
			retained.Messages[id] = Member{Path: name, SHA256: Digest(b), Size: len(b)}
			if d.Bindings.Observed == dataset.ID && sample.Complete {
				evidence.Observed[id] = assertion.Message{Document: doc}
			}
		}
		for _, key := range sample.Keys {
			size += len(key)
			if len(key) > 128 || size > dataset.Completion.MaxBytes {
				return nil, invalid
			}
		}
		if d.Bindings.Before == dataset.ID {
			evidence.Before = assertion.Collection{Complete: sample.Complete, Keys: slices.Clone(sample.Keys)}
		}
		if d.Bindings.After == dataset.ID {
			evidence.After = assertion.Collection{Complete: sample.Complete, Keys: slices.Clone(sample.Keys)}
		}
		totalBytes += size
		if totalBytes > d.Limits.MaxBytes {
			return nil, errors.New("retained observation byte limit")
		}
		r.document.Samples = append(r.document.Samples, retained)
		if !sample.Complete {
			uncertain = true
		}
	}
	checks, err := assertion.Decode(p.files["dependencies/"+d.Checks.SHA256])
	if err != nil {
		return nil, err
	}
	report, err := checks.Evaluate(ctx, evidence)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var e *assertion.Error
		if !errors.As(err, &e) {
			return nil, err
		}
		r.document.EvaluationError = e.Class
		r.document.Verdict = assertion.VerdictUndecided
	} else {
		r.document.Checks = report.Results
		r.document.Verdict = report.Verdict
	}
	if execution.State != "complete" || uncertain {
		r.document.Verdict = assertion.VerdictUndecided
	}
	b, err := encode(r.document)
	if err != nil {
		return nil, err
	}
	if len(b) > MaxBytes {
		return nil, errors.New("result document byte limit")
	}
	r.files["result.json"] = b
	retainedBytes := 0
	for _, b := range r.files {
		retainedBytes += len(b)
	}
	if len(r.files)+1 > resultFamily.Layout.MaxFiles || retainedBytes > resultFamily.Layout.MaxBytes {
		return nil, errors.New("result artifact limit")
	}
	return r, nil
}
func (r *Result) Write(ctx context.Context, output string) error {
	_, err := artifactdir.Write(ctx, output, resultFamily, artifactdir.Durable, r.files)
	return err
}
func OpenResult(ctx context.Context, directory string) (*Result, error) {
	files, err := artifactdir.Read(directory, resultFamily.Layout)
	if err != nil {
		return nil, err
	}
	if !sealed(ResultSchema, files) {
		return nil, errors.New("connected result identity mismatch")
	}
	r, err := readResult(ctx, files)
	if err != nil {
		return nil, err
	}
	if _, ok := files["legacy/result.json"]; ok {
		a, err := testrunner.Open(filepath.Join(directory, "legacy"))
		if err != nil {
			return nil, err
		}
		execution, evidence, err := legacyEvidence(r.plan, a, r.document.Execution.Instance, r.document.Execution.Engine)
		if err != nil {
			return nil, err
		}
		if r.document.Execution.State == "cancelled" && execution.State != "complete" {
			execution.State = "cancelled"
		}
		derived, err := Evaluate(ctx, r.plan, execution, evidence)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(derived.files["result.json"], files["result.json"]) {
			return nil, errors.New("legacy execution testimony mismatch")
		}
	}
	return r, nil
}
func readResult(ctx context.Context, files map[string][]byte) (*Result, error) {
	var d ResultDocument
	if json.Unmarshal(files["result.json"], &d, json.RejectUnknownMembers(true)) != nil || d.Schema != ResultSchema {
		return nil, invalid
	}
	planFiles := map[string][]byte{}
	for n, b := range files {
		if strings.HasPrefix(n, "plan/") {
			planFiles[strings.TrimPrefix(n, "plan/")] = b
		}
	}
	p, err := readPlan(planFiles)
	if err != nil {
		return nil, err
	}
	e := Evidence{Datasets: map[string]Sample{}}
	for _, s := range d.Samples {
		if _, ok := e.Datasets[s.ID]; ok {
			return nil, invalid
		}
		sample := Sample{Complete: s.Complete, SourceIdentity: s.SourceIdentity, Keys: s.Keys, Messages: map[string][]byte{}}
		for id, m := range s.Messages {
			b, ok := files[m.Path]
			if !ok || len(b) != m.Size || Digest(b) != m.SHA256 {
				return nil, errors.New("observation identity mismatch")
			}
			sample.Messages[id] = b
		}
		e.Datasets[s.ID] = sample
	}
	if len(e.Datasets) != len(p.document.Test.Datasets) {
		return nil, invalid
	}
	r, err := Evaluate(ctx, p, d.Execution, e)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(r.files["result.json"], files["result.json"]) {
		return nil, errors.New("retained connected verdict does not reproduce")
	}
	for n, b := range r.files {
		if !bytes.Equal(b, files[n]) {
			return nil, errors.New("retained member mismatch")
		}
	}
	for n, b := range files {
		if strings.HasPrefix(n, "legacy/") || n == "legacy"+replay.DecisionSuffix {
			r.files[n] = b
		} else if n != "identity.sha256" {
			if _, ok := r.files[n]; !ok {
				return nil, errors.New("unexpected result member")
			}
		}
	}
	return r, nil
}

type Analysis struct {
	Schema            string             `json:"schema"`
	Instance          string             `json:"instance"`
	ExecutionIdentity string             `json:"execution_identity"`
	Verdict           assertion.Verdict  `json:"verdict"`
	Checks            []assertion.Result `json:"checks"`
	Identity          string             `json:"identity"`
}

func (r *Result) Reanalyze(ctx context.Context, instance string) (Analysis, error) {
	if !identifier.MatchString(instance) {
		return Analysis{}, invalid
	}
	verified, err := readResult(ctx, r.files)
	if err != nil {
		return Analysis{}, err
	}
	a := Analysis{Schema: AnalysisSchema, Instance: instance, ExecutionIdentity: artifactdir.Identity(ResultSchema, r.files), Verdict: verified.document.Verdict, Checks: verified.document.Checks}
	b, err := encode(a)
	if err != nil {
		return Analysis{}, err
	}
	a.Identity = Digest(b)
	return a, nil
}

func validateExecution(p *Plan, execution Execution) error {
	if p == nil || !identifier.MatchString(execution.Instance) || !short(execution.Engine) || !slices.Contains([]string{"complete", "incomplete", "failed", "cancelled", "uncertain"}, execution.State) || !slices.Contains([]string{"operator-declared", "complete", "failed", "unknown"}, execution.Setup) || !slices.Contains([]string{"not-requested", "complete", "failed", "unknown"}, execution.Cleanup) {
		return invalid
	}
	d := p.document.Test
	attempted := map[string]bool{}
	seenAttempts := map[string]bool{}
	uncertain := false
	for _, a := range execution.Attempts {
		i := slices.IndexFunc(p.document.Effects, func(e Effect) bool { return e.Step == a.Step && e.Kind == a.Kind })
		if i < 0 || seenAttempts[a.Step] || !slices.Contains([]string{"complete", "failed", "unknown", "not-attempted"}, a.Outcome) {
			return invalid
		}
		seenAttempts[a.Step] = true
		attempted[a.Step] = a.Outcome == "complete"
		uncertain = uncertain || a.Uncertain || a.Outcome != "complete"
	}
	if execution.State == "complete" {
		if (d.Setup.Kind == "fixture-reset" || d.Setup.Plan != nil) && execution.Setup != "complete" || d.Setup.Cleanup != nil && execution.Cleanup != "complete" {
			return errors.New("required setup or cleanup was not completed")
		}
		if len(attempted) != len(d.Steps) || uncertain || execution.Setup == "failed" || execution.Setup == "unknown" || execution.Cleanup == "failed" || execution.Cleanup == "unknown" {
			return errors.New("complete execution requires settled effects, setup and cleanup")
		}
	}

	return nil
}
