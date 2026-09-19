// Package runcompare compares verified retained executions without sending,
// rewriting evidence, or attributing a behavioral change to configuration drift.
package runcompare

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/baseline"
	"github.com/bharm16/readmit/internal/drift"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runexplain"
	"github.com/bharm16/readmit/internal/testrunner"
)

const MaxRepeats = 14

// Input explicitly selects a baseline execution, current execution, optional
// approved specification and additional executions. No latest pointer is used.
type Input struct {
	Baseline, Current, Approval string
	Repeats                     []string
}

// Execution deliberately carries no patient values, target addresses or paths.
// Excluded is unknown because reopening the original case would substitute
// today's source for the source that execution actually used.
type Execution struct {
	RunState    string           `json:"run_state"`
	Identity    string           `json:"identity"`
	Status      string           `json:"status"`
	ErrorClass  string           `json:"error_class"`
	Boundary    string           `json:"boundary"`
	Planned     int              `json:"planned"`
	Observed    int              `json:"observed"`
	Unobserved  int              `json:"unobserved"`
	Unevaluated int              `json:"unevaluated"`
	Excluded    string           `json:"excluded"`
	Gaps        []string         `json:"gaps"`
	Assertions  []AssertionState `json:"assertions"`
}
type AssertionState struct {
	ID       string `json:"id"`
	Operator string `json:"operator"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Selector string `json:"selector"`
	Evidence string `json:"evidence"`
}
type AssertionComparison struct {
	ID         string `json:"id"`
	Baseline   string `json:"baseline"`
	Current    string `json:"current"`
	Definition string `json:"definition"`
	Behavior   string `json:"behavior"`
}
type Stability struct {
	FlakyAssertions []string `json:"flaky_assertions"`
	Incomplete      int      `json:"incomplete"`
	State           string   `json:"state"`
	Runs            int      `json:"runs"`
	Failures        int      `json:"failures"`
	Passes          int      `json:"passes"`
	Errors          int      `json:"errors"`
	Reason          string   `json:"reason"`
}

// Comparison is an in-memory view, not a new stored evidence contract.
type Comparison struct {
	Baseline         Execution             `json:"baseline"`
	Current          Execution             `json:"current"`
	Repeats          []Execution           `json:"repeats"`
	Drift            drift.Report          `json:"drift"`
	Assertions       []AssertionComparison `json:"assertions"`
	Specification    string                `json:"specification"`
	Approval         string                `json:"approval"`
	ApprovalRevision int                   `json:"approval_revision"`
	Stability        Stability             `json:"stability"`
	Scope            string                `json:"scope"`
}
type opened struct {
	view     Execution
	artifact *testrunner.Artifact
	path     string
}

func Compare(ctx context.Context, input Input) (Comparison, error) {
	if len(input.Repeats) > MaxRepeats {
		return Comparison{}, errors.New("compare at most sixteen retained executions")
	}
	paths := append([]string{input.Baseline, input.Current}, input.Repeats...)
	runs := make([]opened, 0, len(paths))
	seen := map[string]bool{}
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return Comparison{}, err
		}
		run, err := open(path)
		if err != nil {
			return Comparison{}, err
		}
		// Comparing an execution to itself is useful; it is never a repeat sample.
		if i >= 2 && (run.view.Identity == "" || seen[run.view.Identity]) {
			return Comparison{}, errors.New("repeat samples must be distinct complete retained results")
		}
		if run.view.Identity != "" {
			seen[run.view.Identity] = true
		}
		runs = append(runs, run)
	}
	d, err := drift.Compare(input.Baseline, input.Current)
	if err != nil {
		return Comparison{}, err
	}
	if d.Left.Identity != runs[0].view.Identity || d.Right.Identity != runs[1].view.Identity {
		return Comparison{}, errors.New("retained executions changed during comparison; retry after execution finishes")
	}
	result := Comparison{Baseline: runs[0].view, Current: runs[1].view, Repeats: []Execution{}, Drift: d, Approval: "not_selected", Specification: "unknown", Assertions: []AssertionComparison{}, Scope: "Behavior is compared only for unchanged assertion definitions. Drift does not establish causality. Target software revisions and unselected source coverage remain unknown. All retained failures remain visible; nothing here approves a run, sends messages or writes evidence."}
	a, b := runs[0].artifact, runs[1].artifact
	var leftAssertions, rightAssertions []testrunner.AssertionResult
	leftKnown, rightKnown := a != nil && a.Spec != nil, b != nil && b.Spec != nil
	if a != nil {
		leftAssertions = a.Result.Assertions
	}
	if b != nil {
		rightAssertions = b.Result.Assertions
	}
	if leftKnown && rightKnown {
		result.Specification = changed(a.Spec, b.Spec)
	}
	result.Assertions = compareAssertions(leftAssertions, rightAssertions, leftKnown, rightKnown)
	if input.Approval != "" {
		revision, err := baseline.Read(input.Approval)
		if err != nil {
			return Comparison{}, err
		}
		result.ApprovalRevision = revision.Revision
		result.Approval = "unknown"
		if a := runs[0].artifact; a != nil && a.Spec != nil {
			result.Approval = "different_specification"
			if equal(revision.Spec, *a.Spec) {
				result.Approval = "matches_baseline_specification"
			}
		}
	}
	for _, r := range runs[2:] {
		result.Repeats = append(result.Repeats, r.view)
	}
	result.Stability, err = stability(ctx, runs)
	if err != nil {
		return Comparison{}, err
	}
	if err := ctx.Err(); err != nil {
		return Comparison{}, err
	}
	return result, nil
}

func open(path string) (opened, error) {
	dir, err := artifactpath.Directory(path)
	if err != nil {
		return opened{}, errors.New("execution must be a readable result or durable run directory")
	}
	v := Execution{Status: "unknown", Boundary: "unknown", Excluded: "unknown: the original case is not reopened", Gaps: []string{}, Assertions: []AssertionState{}}
	resultPath := dir
	if _, err := os.Lstat(filepath.Join(dir, "engine.json")); err == nil {
		job, err := durablerun.Open(dir)
		if err != nil {
			return opened{}, err
		}
		v.RunState = string(job.State)
		v.Status = "unknown"
		if job.JournalIncomplete {
			v.Gaps = append(v.Gaps, "journal incomplete: finalized result does not prove execution completed")
		}
		if job.DeliveryUncertain {
			v.Gaps = append(v.Gaps, "delivery uncertain; do not infer an unobserved message was not sent")
		}
		v.Planned = job.Planned
		v.Unobserved = job.Planned
		if job.ResultIdentity == "" {
			v.Gaps = append(v.Gaps, "no finalized result: assertions and observations are unknown")
			return opened{view: v, path: dir}, nil
		}
		resultPath, err = artifactpath.Child(dir, "result")
		if err != nil {
			return opened{}, err
		}
	}
	a, err := testrunner.Open(resultPath)
	if err != nil {
		return opened{}, err
	}
	if v.RunState == "" {
		v.RunState = "not_recorded"
	}
	v.Identity = a.Identity
	v.Status = string(a.Result.Status)
	v.ErrorClass = a.Result.ErrorClass
	v.Boundary = a.Result.ObservationBoundary
	if v.Boundary == "" {
		v.Boundary = "unknown"
	}
	if a.Spec != nil {
		v.Planned = len(a.Spec.Input.Messages)
	} else {
		v.Gaps = append(v.Gaps, "specification unavailable: planned selection and expectations unknown")
	}
	responses := map[string]string{}
	if a.Run != nil {
		described, err := runexplain.DescribeRun(a.Run)
		if err != nil {
			return opened{}, err
		}
		for _, m := range described.Messages {
			if m.ReceivedReadable {
				responses[m.Source] = "run/" + m.Received
				v.Observed++
			}
		}
	}
	v.Unobserved = max(0, v.Planned-v.Observed)
	if v.Unobserved > 0 {
		v.Gaps = append(v.Gaps, "selected messages have no readable retained response")
	}
	if v.Boundary == testrunner.ACKBoundary {
		v.Gaps = append(v.Gaps, "downstream application state was not observed by this ACK-only test")
	}
	if v.Boundary == testrunner.LedgerBoundary && a.FinalObservation == nil {
		v.Gaps = append(v.Gaps, "final ledger observation unavailable")
	}
	for _, a := range a.Result.Assertions {
		s := AssertionState{ID: a.Assertion.ID, Operator: a.Assertion.Operator, Status: a.Status, Message: a.Assertion.Message, Selector: a.Assertion.Selector, Evidence: "unobserved"}
		if a.Status == testrunner.NotEvaluated {
			v.Unevaluated++
		} else if a.Assertion.Operator == "ack_field_equals" {
			s.Evidence = responses[a.Assertion.Message]
		} else {
			s.Evidence = "observation.json"
		}
		v.Assertions = append(v.Assertions, s)
	}
	return opened{view: v, artifact: a, path: dir}, nil
}
func equal(a, b any) bool {
	left, _ := json.Marshal(a, json.Deterministic(true))
	right, _ := json.Marshal(b, json.Deterministic(true))
	return bytes.Equal(left, right)
}
func changed(a, b any) string {
	if equal(a, b) {
		return "unchanged"
	}
	return "changed"
}
func compareAssertions(left, right []testrunner.AssertionResult, leftKnown, rightKnown bool) []AssertionComparison {
	rows := []AssertionComparison{}
	rightByID := map[string]testrunner.AssertionResult{}
	for _, r := range right {
		rightByID[r.Assertion.ID] = r
	}
	for _, l := range left {
		row := AssertionComparison{ID: l.Assertion.ID, Baseline: l.Status, Current: "excluded", Definition: "removed", Behavior: "not_compared"}
		if !rightKnown {
			row.Current = "unknown"
			row.Definition = "unknown"
		}
		if r, ok := rightByID[l.Assertion.ID]; ok {
			row.Current = r.Status
			row.Definition = changed(l.Assertion, r.Assertion)
			if row.Definition == "unchanged" && l.Status != testrunner.NotEvaluated && r.Status != testrunner.NotEvaluated {
				row.Behavior = changed(struct {
					Status   string
					Observed *testrunner.Value
				}{l.Status, l.Observed}, struct {
					Status   string
					Observed *testrunner.Value
				}{r.Status, r.Observed})
			}
			delete(rightByID, l.Assertion.ID)
		}
		rows = append(rows, row)
	}
	for _, r := range right {
		if _, ok := rightByID[r.Assertion.ID]; ok {
			row := AssertionComparison{ID: r.Assertion.ID, Baseline: "excluded", Current: r.Status, Definition: "added", Behavior: "not_compared"}
			if !leftKnown {
				row.Baseline = "unknown"
				row.Definition = "unknown"
			}
			rows = append(rows, row)
		}
	}
	return rows
}
func stability(ctx context.Context, runs []opened) (Stability, error) {
	s := Stability{FlakyAssertions: []string{}, State: "insufficient_history", Reason: "One retained execution does not establish stability or flakiness; target software revision is unknown."}
	distinct := map[string]bool{}
	comparable := true
	var first *opened
	outcomes := map[string]map[string]bool{}
	journals := map[string]bool{}
	for i := range runs {
		r := &runs[i]
		unfinished := r.view.RunState == string(durablerun.Interrupted) || r.view.RunState == string(durablerun.DeliveryUncertain) || r.view.RunState == string(durablerun.Running) || r.view.RunState == string(durablerun.Ready)
		if unfinished && !journals[r.path] {
			s.Incomplete++
		}
		journals[r.path] = true
		if r.view.RunState != "not_recorded" && r.view.RunState != string(durablerun.Passed) && r.view.RunState != string(durablerun.AssertionFailed) {
			comparable = false
		}
		if r.view.Identity == "" {
			s.Errors++
			comparable = false
			continue
		}
		// A shared result identity deduplicates verdict counts, never its enclosing
		// journal or pin: those may disagree even when the retained result is equal.
		if first == nil {
			first = r
		} else {
			if err := ctx.Err(); err != nil {
				return Stability{}, err
			}
			d, err := drift.Compare(first.path, r.path)
			if err != nil {
				return Stability{}, err
			}
			if d.Left.Identity != first.view.Identity || d.Right.Identity != r.view.Identity {
				return Stability{}, errors.New("retained executions changed during comparison")
			}
			if d.Attribution.Outcome != drift.NoDeclaredChange || first.artifact == nil || r.artifact == nil || first.artifact.Spec == nil || r.artifact.Spec == nil || !equal(first.artifact.Spec, r.artifact.Spec) || first.artifact.Result.ReceiverMode != r.artifact.Result.ReceiverMode {
				comparable = false
			}
		}
		if distinct[r.view.Identity] {
			continue
		}
		distinct[r.view.Identity] = true
		s.Runs++
		switch r.view.Status {
		case string(testrunner.Pass):
			s.Passes++
		case string(testrunner.AssertionFailure):
			s.Failures++
		default:
			s.Errors++
		}
		for _, assertion := range r.view.Assertions {
			if outcomes[assertion.ID] == nil {
				outcomes[assertion.ID] = map[string]bool{}
			}
			outcomes[assertion.ID][assertion.Status] = true
		}
	}

	if s.Runs < 2 {
		return s, nil
	}
	s.State = "unresolved"
	s.Reason = "Retained runs differ in configuration or lack comparable input, rule, engine or specification records; variation is not established flakiness. Target software revision is unknown."
	if comparable && s.Errors == 0 {
		s.State = "no_observed_flakiness"
		s.Reason = "No assertion switches between pass and failure in these runs under unchanged retained configuration; this finite history does not prove stability and target software revision is unknown."
		for _, a := range first.view.Assertions {
			if outcomes[a.ID][testrunner.Passed] && outcomes[a.ID][testrunner.Failed] {
				s.FlakyAssertions = append(s.FlakyAssertions, a.ID)
			}
		}
		if len(s.FlakyAssertions) > 0 {
			s.State = "possible_flakiness"
			s.Reason = "An unchanged assertion has both passing and failing outcomes under unchanged recorded configuration. Target software revision and external state remain unknown; this is a flakiness signal, not a causal diagnosis."
		}
	}
	return s, nil
}
