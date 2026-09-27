package fhirvalidator

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"regexp"
	"slices"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/networkaction"
)

var workerJob = regexp.MustCompile(`^[a-f0-9]{32}$`)

type outcomeIssue struct {
	Severity    string   `json:"severity"`
	Code        string   `json:"code"`
	Diagnostics string   `json:"diagnostics"`
	Expression  []string `json:"expression"`
	Location    []string `json:"location"`
	Details     struct {
		Text string `json:"text"`
	} `json:"details"`
	Extension []struct {
		URL         string `json:"url"`
		ValueString string `json:"valueString"`
		ValueCode   string `json:"valueCode"`
	} `json:"extension"`
}

// Interpret is the sole Go severity/coverage policy. It never executes a
// validator or rewrites its raw outcome, and is also used by the offline reader.
func (p *Plan) Interpret(worker WorkerResponse) (Result, error) {
	r := Result{Schema: ResultSchema, Policy: Policy, InputSHA256: p.request.InputSHA256, Capability: p.capability.identity, RequestSHA256: p.identity, State: "worker-crashed", Verdict: assertion.VerdictUndecided, Findings: []Finding{}, Coverage: map[string]string{"structure": "unavailable", "profiles": "unavailable", "invariants": "unavailable", "terminology": "unavailable"}, Worker: WorkerRecord{State: worker.State, ExitCode: worker.ExitCode, DiagnosticSHA256: worker.DiagnosticSHA256}}
	if worker.Schema != WorkerResponseSchema || !workerJob.MatchString(worker.Job) || int64(len(worker.Outcome)) > p.request.MaxOutputBytes || worker.DiagnosticSHA256 != "" && !networkaction.ValidDigest(worker.DiagnosticSHA256) {
		return r, invalid
	}
	if len(worker.Outcome) > 0 {
		r.Worker.OutcomeSHA256 = digest(worker.Outcome)
	}
	if !slices.Contains([]string{"evaluated", "worker-crashed", "invalid-input", "unsupported-runtime", "package-unavailable", "parent-disconnected", "invalid-ipc", "timed-out", "cancelled", "output-limit"}, worker.State) {
		return r, invalid
	}
	if worker.State != "evaluated" {
		r.State = worker.State
		return r, nil
	}
	if worker.ExitCode != 0 && worker.ExitCode != 1 {
		return r, nil
	}
	var outcome struct {
		ResourceType string         `json:"resourceType"`
		Issue        []outcomeIssue `json:"issue"`
	}
	// An outcome the policy refuses evaluated nothing and states no finding.
	refused := r
	refused.State = "worker-output-invalid"
	if !boundedJSON(worker.Outcome) || json.Unmarshal(worker.Outcome, &outcome) != nil || outcome.ResourceType != "OperationOutcome" || len(outcome.Issue) > 10000 {
		return refused, nil
	}
	r.Coverage = map[string]string{"structure": "evaluated", "profiles": "evaluated", "invariants": "evaluated", "terminology": "local-offline-only"}
	requested := map[string]bool{"structure": true, "profiles": true, "invariants": p.request.Requirements.Invariants == "required", "terminology": p.request.Requirements.Terminology == "required"}
	blocked := ""
	gateFailed, nonconforms, reported := false, false, false
	for _, issue := range outcome.Issue {
		if !slices.Contains([]string{"fatal", "error", "warning", "information"}, issue.Severity) || issue.Code == "" || len(issue.Code) > 128 || len(issue.Expression) > 64 || len(issue.Location) > 64 || len(issue.Diagnostics)+len(issue.Details.Text) > 65536 || len(issue.Extension) > 64 {
			return refused, nil
		}
		f := Finding{Severity: issue.Severity, Code: issue.Code, Expressions: append([]string{}, issue.Expression...), Diagnostics: issue.Details.Text, InputSHA256: p.request.InputSHA256}
		if f.Diagnostics == "" {
			f.Diagnostics = issue.Diagnostics
		}
		if len(f.Expressions) == 0 {
			f.Expressions = append([]string{}, issue.Location...)
		}
		for _, expression := range f.Expressions {
			if len(expression) > 4096 {
				return refused, nil
			}
		}
		for _, extension := range issue.Extension {
			if extension.URL == "http://hl7.org/fhir/StructureDefinition/operationoutcome-message-id" {
				if f.MessageID != "" || extension.ValueString != "" && extension.ValueCode != "" || len(extension.ValueString)+len(extension.ValueCode) > 4096 {
					return refused, nil
				}
				f.MessageID = extension.ValueCode
				if f.MessageID == "" {
					f.MessageID = extension.ValueString
				}
			}
		}
		r.Findings = append(r.Findings, f)
		reported = reported || f.Severity == "error" || f.Severity == "fatal"
		if area := unchecked(f); area != "" {
			r.Coverage[area] = "unavailable"
			if blocked == "" && requested[area] {
				blocked = blockedStates[area]
			}
			continue
		}
		if f.Severity == "error" || f.Severity == "fatal" {
			nonconforms = true
		}
		if slices.Contains(p.request.Requirements.FailSeverities, f.Severity) {
			gateFailed = true
		}
	}
	// The validator exits 1 exactly when it reported an error or fatal issue.
	if reported != (worker.ExitCode == 1) {
		return refused, nil
	}
	// A decided error is a failure whatever else went unchecked; an unchecked
	// or unrequested area otherwise leaves the result undecided, never a pass.
	switch {
	case nonconforms:
		r.State, r.Verdict = "nonconforms", assertion.VerdictFail
	case blocked != "":
		r.State = blocked
	case !requested["terminology"] || !requested["invariants"]:
		for area, wanted := range requested {
			if !wanted && r.Coverage[area] != "unavailable" {
				r.Coverage[area] = "not-requested"
			}
		}
		r.State = "not-fully-evaluated"
	default:
		r.State, r.Verdict = "conforms", assertion.VerdictPass
		if gateFailed {
			r.Verdict = assertion.VerdictFail
		}
	}
	return r, nil
}
func (r Result) Format(w fmt.State, _ rune) {
	_, _ = fmt.Fprintf(w, "FHIR validation (%s; %s; %d findings)", r.State, r.Verdict, len(r.Findings))
}
func (Finding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "FHIR validation finding (private)")
}
