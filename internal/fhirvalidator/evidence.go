package fhirvalidator

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
)

var resultFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "FHIR validation result", Nested: []string{"capability"}, RequiredFiles: []string{"request.json", "input.json", "worker.json", "result.json", "identity.sha256"}, AllowFile: func(n string) bool {
	return slices.Contains([]string{"request.json", "input.json", "worker.json", "result.json", "identity.sha256"}, n)
}, MaxFiles: 1024, MaxFileBytes: 32 << 20, MaxBytes: 192 << 20}, Seal: artifactdir.DirectoryHash(ResultSchema)}

type Evidence struct {
	result   Result
	plan     *Plan
	worker   WorkerResponse
	identity string
}

func (e *Evidence) Identity() string     { return e.identity }
func (e *Evidence) Result() Result       { var r Result; _ = json.Unmarshal(encode(e.result), &r); return r }
func (e *Evidence) OutcomeBytes() []byte { return bytes.Clone(e.worker.Outcome) }
func (Evidence) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "FHIR validation evidence (private)")
}

// Execute retains the requested input and capability before invoking the fixed
// local worker. Every runtime refusal remains explicit and cannot be a pass.
func (p *Plan) Execute(ctx context.Context, engine *Engine, output string) (*Evidence, error) {
	w, err := artifactdir.Create(output, resultFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	assets := map[string][]byte{}
	for name, raw := range p.capability.files {
		if name != "manifest.json" && name != "identity.sha256" {
			assets[name] = raw
		}
	}
	retained, err := Stage(ctx, filepath.Join(output, "capability"), p.capability.manifest, assets)
	if err != nil || retained.identity != p.capability.identity {
		return nil, invalid
	}
	if w.WriteFile("request.json", encode(p.request)) != nil || w.WriteFile("input.json", p.input) != nil || w.Sync() != nil {
		return nil, invalid
	}
	worker, ran, runErr := engine.run(ctx, p)
	var status *Status
	if runErr != nil {
		var typed Status
		if errors.As(runErr, &typed) {
			status = &typed
		} else {
			status = &Status{State: "worker-crashed", Requirement: "verify the staged worker capability"}
		}
	}
	if worker.Schema == "" {
		worker = stopped("00000000000000000000000000000000", "worker-crashed")
	}
	result, err := p.Interpret(worker)
	if err != nil {
		return nil, err
	}
	if status != nil {
		result.RuntimeStatus = status
		result.State = status.State
		result.Verdict = assertion.VerdictUndecided
	}
	if ran != (EngineRecord{}) {
		result.Engine = &ran
	}
	if w.WriteFile("worker.json", encode(worker)) != nil || w.WriteFile("result.json", encode(result)) != nil {
		return nil, invalid
	}
	identity, err := w.Seal(nil)
	if err != nil {
		return nil, err
	}
	return &Evidence{result: result, plan: p, worker: worker, identity: identity}, nil
}
func Open(ctx context.Context, directory string) (*Evidence, error) {
	files, err := artifactdir.Read(directory, resultFamily.Layout)
	if err != nil {
		return nil, invalid
	}
	return verifyEvidence(ctx, files)
}

// Verify evaluates one captured validation receipt, with no worker effects.
func Verify(ctx context.Context, captured map[string][]byte) (*Evidence, error) {
	files, err := artifactdir.Snapshot(captured, resultFamily.Layout)
	if err != nil {
		return nil, invalid
	}
	return verifyEvidence(ctx, files)
}
func verifyEvidence(ctx context.Context, files map[string][]byte) (*Evidence, error) {
	identity := strings.TrimSpace(string(files["identity.sha256"]))
	if identity != artifactdir.Identity(ResultSchema, files) {
		return nil, invalid
	}
	nested := map[string][]byte{}
	for name, raw := range files {
		if strings.HasPrefix(name, "capability/") {
			nested[strings.TrimPrefix(name, "capability/")] = raw
		}
	}
	capability, err := verifyCapability(nested)
	if err != nil {
		return nil, err
	}
	plan, err := Prepare(files["request.json"], files["input.json"], capability)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var worker WorkerResponse
	var result Result
	if json.Unmarshal(files["worker.json"], &worker, json.RejectUnknownMembers(true)) != nil || json.Unmarshal(files["result.json"], &result, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	rebuilt, err := plan.Interpret(worker)
	if err != nil {
		return nil, err
	}
	if result.RuntimeStatus != nil {
		if !slices.Contains([]string{"worker-missing", "worker-unavailable", "unsupported-runtime", "worker-crashed", "cleanup-unconfirmed"}, result.RuntimeStatus.State) || len(result.RuntimeStatus.Requirement) > 128 {
			return nil, invalid
		}
		rebuilt.RuntimeStatus = result.RuntimeStatus
		rebuilt.State = result.RuntimeStatus.State
		rebuilt.Verdict = assertion.VerdictUndecided
	}
	if result.Engine != nil {
		if !engineVersion.MatchString(result.Engine.Version) || result.Engine.OS != "linux" || result.Engine.Architecture != "aarch64" && result.Engine.Architecture != "arm64" {
			return nil, invalid
		}
		rebuilt.Engine = result.Engine
	}
	if !bytes.Equal(encode(rebuilt), encode(result)) {
		return nil, invalid
	}
	return &Evidence{result: result, plan: plan, worker: worker, identity: identity}, nil
}

const CheckSchema = "readmit-fhir-validation-check/v1"
const CheckOperator assertion.Operator = "fhir-validation"

type Check struct {
	Schema     string           `json:"schema"`
	Validation string           `json:"validation_identity"`
	Input      string           `json:"input_sha256"`
	Capability string           `json:"capability_identity"`
	Request    string           `json:"request_identity"`
	Result     assertion.Result `json:"result"`
}

func (e *Evidence) Check(id string) (Check, error) {
	if !packageID.MatchString(id) {
		return Check{}, invalid
	}
	outcome := assertion.OutcomeUndecided
	switch e.result.Verdict {
	case assertion.VerdictPass:
		outcome = assertion.OutcomePassed
	case assertion.VerdictFail:
		outcome = assertion.OutcomeFailed
	}
	return Check{Schema: CheckSchema, Validation: e.identity, Input: e.result.InputSHA256, Capability: e.result.Capability, Request: e.result.RequestSHA256, Result: assertion.Result{ID: id, Operator: CheckOperator, Outcome: outcome}}, nil
}

func (c Check) Format(w fmt.State, _ rune) {
	_, _ = fmt.Fprintf(w, "FHIR validation check (%s)", c.Result.Outcome)
}
