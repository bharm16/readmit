package desktop

import (
	"context"
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
)

// AssertionSetRequest carries one structured assertion-set draft over the open
// workspace. Draft is the whole of what has been answered so far. Output is
// the new entry a save writes into.
type AssertionSetRequest struct {
	Workspace  string                   `json:"workspace"`
	Draft      assertionauthor.Draft    `json:"draft"`
	Name       string                   `json:"name,omitzero"`
	Assertions []assertionauthor.Clause `json:"assertions,omitzero"`
	Output     string                   `json:"output,omitzero"`
}

// AssertionSetDraft is the draft and, after a save, the entry and identity of
// the bytes on disk.
type AssertionSetDraft struct {
	Draft    assertionauthor.Draft `json:"draft"`
	Output   string                `json:"output,omitzero"`
	Identity string                `json:"identity,omitzero"`
}

// AssertionSetResult carries one state for structured assertion-set authoring.
type AssertionSetResult struct {
	State  State              `json:"state"`
	Reason string             `json:"reason,omitzero"`
	Set    *AssertionSetDraft `json:"set,omitzero"`
}

func (r *AssertionSetResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) assertionSet() AssertionSetResult {
	return AssertionSetResult{State: r.state, Reason: r.reason}
}

// CanonicalAssertionRequest carries complete assertion-set document bytes for
// the advanced JSON path.
type CanonicalAssertionRequest struct {
	Workspace string `json:"workspace"`
	Document  string `json:"document"`
	Output    string `json:"output"`
}

// CanonicalAssertionResult mirrors CanonicalTestResult for assertion sets.
type CanonicalAssertionResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Document string `json:"document,omitzero"`
	Output   string `json:"output,omitzero"`
	Identity string `json:"identity,omitzero"`
}

func (r *CanonicalAssertionResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// AuthorAssertionSet answers one structured edit of an assertion-set draft.
// Carrying a Name replaces the local description; carrying Assertions replaces
// the clause list. Carrying neither opens or reports the draft as it stands.
func (a *App) AuthorAssertionSet(request AssertionSetRequest) AssertionSetResult {
	return run(a, false, false, func(context.Context) AssertionSetResult {
		return a.authorAssertionSet(request)
	})
}

func (a *App) authorAssertionSet(request AssertionSetRequest) AssertionSetResult {
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return declined.assertionSet()
	}
	if err := a.admitAuthor(); err != nil {
		return AssertionSetResult{State: PermissionDenied, Reason: err.Error()}
	}
	draft := request.Draft
	if draft.Schema == "" {
		draft = assertionauthor.NewDraft()
	}
	var err error
	if request.Name != "" {
		draft, err = draft.SetName(request.Name)
		if err != nil {
			return AssertionSetResult{State: Failed, Reason: err.Error()}
		}
	}
	if request.Assertions != nil {
		draft, err = draft.SetAssertions(request.Assertions)
		if err != nil {
			return AssertionSetResult{State: Failed, Reason: err.Error()}
		}
	}
	state := Completed
	if draft.Name == "" && len(draft.Assertions) == 0 {
		state = Empty
	}
	return AssertionSetResult{State: state, Set: &AssertionSetDraft{Draft: draft}}
}

// SaveAssertionSet writes the generated set into one new workspace entry.
func (a *App) SaveAssertionSet(request AssertionSetRequest) AssertionSetResult {
	return run(a, false, false, func(context.Context) AssertionSetResult {
		return a.saveAssertionSet(request)
	})
}

func (a *App) saveAssertionSet(request AssertionSetRequest) AssertionSetResult {
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return declined.assertionSet()
	}
	if err := a.admitAuthor(); err != nil {
		return AssertionSetResult{State: PermissionDenied, Reason: err.Error()}
	}
	draft := request.Draft
	if draft.Schema == "" {
		return AssertionSetResult{State: Failed, Reason: "there is no assertion set draft to save"}
	}
	if err := artifactpath.EntryName(request.Output); err != nil {
		return AssertionSetResult{State: Failed, Reason: "an assertion set is written to one new entry of the open workspace"}
	}
	saved, err := assertionauthor.Save(root, draft, request.Output)
	if errors.Is(err, assertionauthor.ErrCannotWrite) {
		return assertionWriteRefusal(root, request.Output).assertionSet()
	}
	if err != nil {
		return AssertionSetResult{State: Failed, Reason: err.Error()}
	}
	return AssertionSetResult{
		State: Completed,
		Set:   &AssertionSetDraft{Draft: draft, Output: saved.Output, Identity: saved.Identity},
	}
}

// ImportAssertionSet opens an existing complete set into the structured draft,
// preserving every clause the shared reader retained.
func (a *App) ImportAssertionSet(workspace, entry string) AssertionSetResult {
	return run(a, false, false, func(context.Context) AssertionSetResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return declined.assertionSet()
		}
		data, err := assertionauthor.Import(root, entry)
		if err != nil {
			return AssertionSetResult{State: Failed, Reason: err.Error()}
		}
		draft, err := assertionauthor.ImportDocument(data)
		if err != nil {
			return AssertionSetResult{State: Failed, Reason: err.Error()}
		}
		return AssertionSetResult{State: Completed, Set: &AssertionSetDraft{Draft: draft}}
	})
}

// ValidateAssertionSet uses the same strict reader explain evaluates with.
func (a *App) ValidateAssertionSet(document string) CanonicalAssertionResult {
	return run(a, false, false, func(context.Context) CanonicalAssertionResult {
		if _, err := assertion.Decode([]byte(document)); err != nil {
			return CanonicalAssertionResult{State: Failed, Reason: err.Error()}
		}
		return CanonicalAssertionResult{State: Completed, Document: document}
	})
}

// ExportAssertionSet writes exact reviewed bytes to a new workspace entry. A
// new assertion set is authoring, admitted exactly as saving one is.
func (a *App) ExportAssertionSet(request CanonicalAssertionRequest) CanonicalAssertionResult {
	return run(a, false, true, func(context.Context) CanonicalAssertionResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return CanonicalAssertionResult{State: declined.state, Reason: declined.reason}
		}
		saved, err := assertionauthor.Export(root, []byte(request.Document), request.Output)
		if errors.Is(err, assertionauthor.ErrCannotWrite) {
			refused := assertionWriteRefusal(root, request.Output)
			return CanonicalAssertionResult{State: refused.state, Reason: refused.reason}
		}
		if err != nil {
			return CanonicalAssertionResult{State: Failed, Reason: err.Error()}
		}
		return CanonicalAssertionResult{
			State: Completed, Document: request.Document,
			Output: saved.Output, Identity: saved.Identity,
		}
	})
}

// assertionWriteRefusal says why an assertion set was not created at one entry
// of the open workspace, once the exclusive create has already failed. The
// entry is only looked at, never touched: a name that is already an entry is
// the person's to change, so it is refused as one rather than as a failure of
// the folder, and the rest is separated as every other write in this window is.
func assertionWriteRefusal(root, output string) refusal {
	if _, err := os.Lstat(artifactpath.JoinReference(root, output)); err == nil {
		return refusal{Failed, "that name is already an entry of this workspace; an assertion set is written to a new entry"}
	}
	return probeWriteFailure(root,
		"this account cannot write into the open workspace",
		"the assertion set could not be created in the open workspace")
}
