package desktop

import (
	"context"
	"errors"

	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// CanonicalTestRequest carries the full canonical document, not a partial
// projection of its clauses. It remains unstored until ExportTest succeeds.
type CanonicalTestRequest struct {
	Workspace string `json:"workspace"`
	Document  string `json:"document"`
	Output    string `json:"output"`
}

type CanonicalTestResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Document string `json:"document,omitzero"`
	Output   string `json:"output,omitzero"`
	Identity string `json:"identity,omitzero"`
}

func (r *CanonicalTestResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ImportTest opens an existing canonical spec for editing. These bounded local
// operations hold the operation slot and are not interruptible. They send no
// messages; cancellation of an edit is simply discarding the unstored text.
func (a *App) ImportTest(workspace, entry string) CanonicalTestResult {
	return run(a, false, false, func(context.Context) CanonicalTestResult {
		return a.importTest(workspace, entry)
	})
}

func (a *App) importTest(workspace, entry string) CanonicalTestResult {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return canonicalRefusal(declined)
	}
	data, err := testauthor.Import(root, entry)
	if err != nil {
		return CanonicalTestResult{State: Failed, Reason: err.Error()}
	}
	return CanonicalTestResult{State: Completed, Document: string(data)}
}

// ValidateTest uses exactly the strict reader that headless execution uses.
// Unknown schemas, members and operators are errors, never discarded clauses.
func (a *App) ValidateTest(document string) CanonicalTestResult {
	return run(a, false, false, func(context.Context) CanonicalTestResult {
		if _, err := testrunner.DecodeSpec([]byte(document)); err != nil {
			return CanonicalTestResult{State: Failed, Reason: err.Error()}
		}
		return CanonicalTestResult{State: Completed, Document: document}
	})
}

// ExportTest writes the edited canonical document as a new test spec, which is
// authoring, admitted exactly as saving an authored test is.
func (a *App) ExportTest(request CanonicalTestRequest) CanonicalTestResult {
	return run(a, false, true, func(context.Context) CanonicalTestResult {
		return a.exportTest(request)
	})
}

func (a *App) exportTest(request CanonicalTestRequest) CanonicalTestResult {
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return canonicalRefusal(declined)
	}
	saved, err := testauthor.Export(root, []byte(request.Document), request.Output)
	if errors.Is(err, testauthor.ErrCannotWrite) {
		return canonicalRefusal(probeWriteFailure(root, "this account cannot write into the open workspace", "the test spec could not be created in the open workspace"))
	}
	if err != nil {
		return CanonicalTestResult{State: Failed, Reason: err.Error()}
	}
	return CanonicalTestResult{State: Completed, Document: request.Document, Output: saved.Output, Identity: saved.Identity}
}

func canonicalRefusal(r refusal) CanonicalTestResult {
	return CanonicalTestResult{State: r.state, Reason: r.reason}
}
