package desktop

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
)

// FindingReviewRequest carries the report the decisions were read against,
// bound by identity, and the analyst's typed decisions. ReportSHA256 is the
// identity the panel displayed for the report; a report that changed since is
// refused rather than reviewed.
type FindingReviewRequest struct {
	Workspace       string                   `json:"workspace"`
	Case            string                   `json:"case"`
	Identity        string                   `json:"identity"`
	Report          string                   `json:"report"`
	ReportSHA256    string                   `json:"report_sha256"`
	Decisions       []findingreview.Decision `json:"decisions"`
	Offset          int                      `json:"offset"`
	Output          string                   `json:"output,omitzero"`           // review directory — DecideFindings alone
	DecisionsOutput string                   `json:"decisions_output,omitzero"` // decisions document — DecideFindings alone
}

// FindingReview is one findingreview.Record windowed for the panes. Total is
// how many findings the review holds, so a window can never read as all of it.
type FindingReview struct {
	Record findingreview.Record `json:"record"`
	Offset int                  `json:"offset"`
	Total  int                  `json:"total"`
}

// FindingReviewResult carries one state. Review is present whenever the
// diagnosis and the decisions joined, including when nothing was decided,
// because every finding is then reported as not reviewed.
type FindingReviewResult struct {
	State           State          `json:"state"`
	Reason          string         `json:"reason,omitzero"`
	Output          string         `json:"output,omitzero"`
	DecisionsOutput string         `json:"decisions_output,omitzero"`
	Review          *FindingReview `json:"review,omitzero"`
}

func (r *FindingReviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReviewFindings joins the diagnosis and the analyst's decisions and reports
// every verdict, basis, suppression scope and promotion — writing nothing.
func (a *App) ReviewFindings(request FindingReviewRequest) FindingReviewResult {
	return a.findingReview(request, false)
}

// DecideFindings re-verifies all inputs and persists the decisions document
// and the review directory, exactly as `readmit diagnose review` writes them.
func (a *App) DecideFindings(request FindingReviewRequest) FindingReviewResult {
	return a.findingReview(request, true)
}

func (a *App) findingReview(request FindingReviewRequest, write bool) FindingReviewResult {
	return run(a, false, write, func(context.Context) FindingReviewResult {
		return a.reviewFindings(request, write)
	})
}

func (a *App) reviewFindings(request FindingReviewRequest, write bool) FindingReviewResult {
	failure := func(reason string) FindingReviewResult {
		return FindingReviewResult{State: Failed, Reason: reason}
	}
	if request.Offset < 0 {
		return failure("a finding review window cannot begin before its first finding")
	}
	root, opened, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return FindingReviewResult{State: declined.state, Reason: declined.reason}
	}
	reportDir, retained, declined := retainedDiagnosis(root, request.Report, request.ReportSHA256)
	if reportDir == "" {
		return FindingReviewResult{State: declined.state, Reason: declined.reason}
	}
	// The decisions the panel holds become the same document the command line
	// reads: encoded deterministically, then read back through the contract's
	// own strict parser, so every refusal `readmit diagnose review` makes over
	// a decisions file is made here over the panel's typed decisions.
	typed := findingreview.Decisions{Schema: findingreview.DecisionsSchema, Report: retained.Identity, Decisions: request.Decisions}
	decisionsData, err := json.Marshal(typed, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return failure("cannot encode finding decisions")
	}
	decisionsData = append(decisionsData, '\n')
	decisions, err := findingreview.ParseDecisions(decisionsData)
	if err != nil {
		return failure(err.Error())
	}
	record, err := findingreview.Review(findingreview.Reviewed{Report: retained.Report, Identity: retained.Identity, Case: opened, Entry: request.Case}, decisions, diagnose.Identity(decisionsData))
	if err != nil {
		return failure(err.Error())
	}
	result := windowedFindingReview(record, request.Offset)
	if !write {
		return result
	}
	if artifactpath.EntryName(request.DecisionsOutput) != nil {
		return failure("the decisions are written to one new entry of the open workspace")
	}
	if artifactpath.EntryName(request.Output) != nil {
		return failure("a review is written to one new directory entry of the open workspace")
	}
	if err := writeWorkspaceEntry(root, request.DecisionsOutput, decisionsData); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return FindingReviewResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
		}
		return failure(err.Error())
	}
	if err := findingreview.Write(filepath.Join(root, request.Output), record, artifactpath.JoinReference(root, request.Case), reportDir); err != nil {
		return failure(err.Error())
	}
	result.Output = request.Output
	result.DecisionsOutput = request.DecisionsOutput
	return result
}

// windowedFindingReview reports the requested window of one record's findings.
// A review of a diagnosis that produced no findings is complete, not empty:
// there was nothing to review, and the record says so itself.
func windowedFindingReview(record findingreview.Record, offset int) FindingReviewResult {
	total := len(record.Findings)
	start := min(offset, total)
	record.Findings = record.Findings[start : start+min(MaxDiagnosisFindings, total-start)]
	if record.Findings == nil {
		record.Findings = []findingreview.Status{}
	}
	review := &FindingReview{Record: record, Offset: offset, Total: total}
	if total > 0 && len(record.Findings) == 0 {
		return FindingReviewResult{State: Empty, Reason: "this window begins past the last finding of this review", Review: review}
	}
	return FindingReviewResult{State: Completed, Review: review}
}
