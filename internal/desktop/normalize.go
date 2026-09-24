package desktop

import (
	"context"
	"strconv"

	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/operation"
)

// NormalizeRequest names the two collections, the declared policy, and the
// alignment, exactly as CompareRequest does — plus the policy entry.
type NormalizeRequest struct {
	Workspace string   `json:"workspace"`
	Left      string   `json:"left"`
	Identity  string   `json:"identity"`
	Right     string   `json:"right"`
	Policy    string   `json:"policy"`
	Keys      []string `json:"keys"`
	Fields    []string `json:"fields"`
	Offset    int      `json:"offset"`
	Limit     int      `json:"limit"`
}

// Normalization is one policy-scoped reading of one comparison, windowed.
// The raw comparison remains readmit-diff/v1, unchanged by anything here.
// PolicySHA256 is the digest of the exact bytes the policy entry holds, so a
// preview states which policy revision it ran under.
type Normalization struct {
	Left         string                    `json:"left"`
	Right        string                    `json:"right"`
	Policy       string                    `json:"policy"`
	PolicySHA256 string                    `json:"policy_sha256"`
	Report       string                    `json:"report"`
	PolicySchema string                    `json:"policy_schema"`
	Scope        string                    `json:"scope"`
	Boundary     diff.Boundary             `json:"boundary"`
	LeftSummary  diff.InputSummary         `json:"left_summary"`
	RightSummary diff.InputSummary         `json:"right_summary"`
	Alignment    string                    `json:"alignment"`
	Keys         []string                  `json:"keys"`
	Fields       []string                  `json:"fields"`
	Rules        []diff.RuleReport         `json:"rules"`
	Summary      diff.NormalizationSummary `json:"summary"`
	Offset       int                       `json:"offset"`
	Limit        int                       `json:"limit"`
	Total        int                       `json:"total"`
	Differences  []diff.Difference         `json:"differences"`
	Unsupported  []diff.Unsupported        `json:"unsupported"`
}

// NormalizeResult carries one state. Normalization is present whenever both
// collections were read under the policy, including when the window holds no
// difference, because the counts and the rule reports are the answer then.
type NormalizeResult struct {
	State         State          `json:"state"`
	Reason        string         `json:"reason,omitzero"`
	Normalization *Normalization `json:"normalization,omitzero"`
}

func (r *NormalizeResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// NormalizeCompare reads two collections under a declared policy and reports
// every difference beside what the policy did about it — suppressed, retained,
// undecided or unaddressed. It never edits the raw comparison and never
// changes a source byte. The same engine `readmit normalize` runs decides
// everything here; the window only lays the report out.
func (a *App) NormalizeCompare(request NormalizeRequest) NormalizeResult {
	return run(a, false, false, func(context.Context) NormalizeResult {
		return a.normalizeCompare(request)
	})
}

func (a *App) normalizeCompare(request NormalizeRequest) NormalizeResult {
	if request.Offset < 0 || request.Limit < 1 || request.Limit > MaxComparisonRows {
		return NormalizeResult{State: Failed, Reason: "a normalization renders a window beginning at or after its first difference, of between 1 and " + strconv.Itoa(MaxComparisonRows) + " differences"}
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return NormalizeResult{State: declined.state, Reason: declined.reason}
	}
	left, declined := comparedCase(root, request.Left)
	if left == "" {
		return NormalizeResult{State: declined.state, Reason: declined.reason}
	}
	right, declined := comparedCase(root, request.Right)
	if right == "" {
		return NormalizeResult{State: declined.state, Reason: declined.reason}
	}
	// The left collection is the one the window verified and displayed, so the
	// normalization is bound to that identity the way the raw comparison is.
	if _, err := operation.OpenVerifiedCase(left, request.Identity); err != nil {
		return NormalizeResult{State: Failed, Reason: err.Error()}
	}
	policyData, declined := workspaceDocument(root, request.Policy, diff.MaxPolicyBytes, "the normalization policy")
	if policyData == nil {
		return NormalizeResult{State: declined.state, Reason: declined.reason}
	}
	policy, err := diff.DecodePolicy(policyData)
	if err != nil {
		return NormalizeResult{State: Failed, Reason: err.Error()}
	}
	report, err := diff.Normalize(
		diff.Input{Path: left},
		diff.Input{Path: right},
		diff.Options{Keys: request.Keys, Fields: request.Fields},
		policy,
	)
	if err != nil {
		return NormalizeResult{State: Failed, Reason: refusedComparison(err)}
	}
	return windowedNormalization(request, digestOf(policyData), report)
}

// windowedNormalization returns the requested window of one report's
// differences. A window holding none is empty with the reason it is empty, and
// still carries every rule report and count, because how much the policy
// suppressed is the answer in that case.
func windowedNormalization(request NormalizeRequest, policySHA256 string, report diff.NormalizationReport) NormalizeResult {
	total := len(report.Differences)
	window := make([]diff.Difference, 0, request.Limit)
	if request.Offset < total {
		window = append(window, report.Differences[request.Offset:min(total, request.Offset+request.Limit)]...)
	}
	described := &Normalization{
		Left: request.Left, Right: request.Right,
		Policy: request.Policy, PolicySHA256: policySHA256,
		Report: report.Schema, PolicySchema: report.PolicySchema,
		Scope: report.Scope, Boundary: report.Boundary,
		LeftSummary: report.Left, RightSummary: report.Right,
		Alignment: report.Alignment,
		Keys:      report.Keys, Fields: report.Fields,
		Rules: report.Rules, Summary: report.Summary,
		Offset: request.Offset, Limit: request.Limit, Total: total,
		Differences: window, Unsupported: report.Unsupported,
	}
	if described.Keys == nil {
		described.Keys = []string{}
	}
	if described.Fields == nil {
		described.Fields = []string{}
	}
	if described.Rules == nil {
		described.Rules = []diff.RuleReport{}
	}
	if described.Unsupported == nil {
		described.Unsupported = []diff.Unsupported{}
	}
	switch {
	case total == 0:
		return NormalizeResult{State: Empty, Reason: "this comparison reported no difference for the policy to address", Normalization: described}
	case len(window) == 0:
		return NormalizeResult{State: Empty, Reason: "this window begins past the last difference of this comparison", Normalization: described}
	}
	return NormalizeResult{State: Completed, Normalization: described}
}
