package desktop

import (
	"errors"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/replay"
	"path/filepath"
	"reflect"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/testauthor"
)

// TestRequest is one answer in a guided authoring session over one verified
// case.
//
// Identity binds the request to the evidence the grid displayed, exactly as an
// inspection does. Draft is the whole of what has been answered so far: the
// shell holds no session, so every call carries the draft and gets the next one
// back. Answer is the stage being answered and is read by AuthorTest alone;
// Output is the new entry a save writes into and is read by SaveTest alone.
//
// Suggest names the reviewed run expectations are proposed from and is read by
// SuggestExpectations and ApproveExpectations; Review is what a person decided
// about those proposals and is read by ApproveExpectations alone. No suggested
// value is ever carried back across this boundary: approving derives the
// proposals from the run again and applies the decisions to them, so what an
// approval records is what that run produced plus the edits the review made.
type TestRequest struct {
	Workspace string                        `json:"workspace"`
	Case      string                        `json:"case"`
	Identity  string                        `json:"identity"`
	Draft     testauthor.Draft              `json:"draft"`
	Answer    testauthor.Answer             `json:"answer,omitzero"`
	Output    string                        `json:"output,omitzero"`
	Suggest   *testauthor.SuggestionRequest `json:"suggest,omitzero"`
	Review    *testauthor.Review            `json:"review,omitzero"`
}

// TestDraft is the draft and what it means over the evidence and the open
// workspace: which stage the flow asks next, everything still unanswered, the
// initial state the chosen boundary fixes, the selected occurrences in the
// order a run sends them, and the targets this workspace offers. Output and
// Identity are present only after a save, and name the entry that was written
// and the identity `readmit test` records for those exact bytes.
type TestDraft struct {
	Draft      testauthor.Draft      `json:"draft"`
	Resolution testauthor.Resolution `json:"resolution"`
	Output     string                `json:"output,omitzero"`
	Identity   string                `json:"identity,omitzero"`
	// Suggestions is present after expectations were proposed from a reviewed
	// run, and Approval after a person decided about them. Neither is stored
	// anywhere: both are what the call that produced them answered.
	Suggestions *testauthor.Suggestions `json:"suggestions,omitzero"`
	Approval    *testauthor.Approval    `json:"approval,omitzero"`
}

// TestResult carries one state. Test is present whenever the draft resolved,
// including when nothing has been answered yet, because an unanswered draft is
// the answer to a session that has just been opened.
type TestResult struct {
	State  State      `json:"state"`
	Reason string     `json:"reason,omitzero"`
	Test   *TestDraft `json:"test,omitzero"`
}

func (r refusal) test() TestResult { return TestResult{State: r.state, Reason: r.reason} }

// AuthorTest answers one stage of a test draft and reports what the draft now
// means over the case and the workspace.
//
// The evidence is verified again on every call, the way every read of a case in
// this window is, and the answer is applied by the same engine a save generates
// the spec with — so the flow shows what a saved test would say. An answer the
// draft, the evidence or the chosen boundary does not support leaves the draft
// exactly as it was and reports why. A request carrying no answer asks the
// question the flow is on, which is how a session opens. It runs to completion
// under the case reader's own limits once it starts, so it holds the operation
// slot but is not interruptible.
func (a *App) AuthorTest(request TestRequest) TestResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.test()
	}
	defer release()
	root, source, draft, declined := a.authoring(request)
	if source == nil {
		return declined.test()
	}
	if !sampleAuthoring(request, root, source) {
		if err := a.admitAuthor(); err != nil {
			return TestResult{State: PermissionDenied, Reason: err.Error()}
		}
	}
	if request.Answer.Stage != "" {
		answered, err := draft.Answer(request.Answer)
		if err != nil {
			return TestResult{State: Failed, Reason: err.Error()}
		}
		draft = answered
	}
	return authored(root, source, draft, TestDraft{})
}

// SaveTest writes the generated spec into one new entry of the open workspace.
//
// A test is a document, not evidence, and this writes only a new one: the case
// it names is not touched, the destination must be an entry that does not exist
// yet, and a destination inside any retained artifact is refused by the same
// output policy the command line uses. The spec is read back and decoded again
// afterwards, so the identity this reports is the identity of the bytes that
// are on disk.
func (a *App) SaveTest(request TestRequest) TestResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.test()
	}
	defer release()
	root, source, draft, declined := a.authoring(request)
	if source == nil {
		return declined.test()
	}
	if !sampleAuthoring(request, root, source) {
		if err := a.admitAuthor(); err != nil {
			return TestResult{State: PermissionDenied, Reason: err.Error()}
		}
	}
	if err := artifactpath.EntryName(request.Output); err != nil {
		return TestResult{State: Failed, Reason: "a test spec is written to one new entry of the open workspace"}
	}
	saved, err := testauthor.Save(root, source, draft, request.Output)
	if errors.Is(err, testauthor.ErrCannotWrite) {
		// A folder this account cannot write is separated from the rest only
		// after the write has already failed, and the probe never touches the
		// destination.
		return probeWriteFailure(root,
			"this account cannot write into the open workspace",
			"the test spec could not be created in the open workspace").test()
	}
	if err != nil {
		return TestResult{State: Failed, Reason: err.Error()}
	}
	return authored(root, source, draft, TestDraft{Output: saved.Output, Identity: saved.Identity})
}

// SuggestExpectations proposes expectations from one run somebody has already
// reviewed, and records nothing.
//
// It is the reading half of this flow: the run is opened through the reader
// `readmit test` verifies a result with, every value is read exactly as the
// evaluator reads it, and the draft comes back unchanged. A proposal the run
// does not justify is reported as unsupported with the reason rather than
// dropped from the set. Nothing here approves anything, and no proposal reaches
// the draft until ApproveExpectations is given a decision naming it.
func (a *App) SuggestExpectations(request TestRequest) TestResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.test()
	}
	defer release()
	root, source, draft, suggestions, declined := a.proposing(request)
	if source == nil {
		return declined.test()
	}
	return authored(root, source, draft, TestDraft{Suggestions: &suggestions})
}

// ApproveExpectations records what a person decided about proposed expectations
// and reports the draft their approvals produced.
//
// The proposals are derived from the reviewed run again here rather than read
// back from the window, so a suggested value never crosses this boundary in the
// direction of the draft: what an approval records is what that run produced,
// with the edits the review named. A run that changed since it was read is
// refused by identity, an unsupported proposal cannot be approved at all, and a
// proposal no decision names is reported as not reviewed and recorded nowhere.
func (a *App) ApproveExpectations(request TestRequest) TestResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.test()
	}
	defer release()
	if err := a.admitAuthor(); err != nil {
		return TestResult{State: PermissionDenied, Reason: err.Error()}
	}
	if request.Review == nil {
		return TestResult{State: Failed, Reason: "an approval states what was decided about the proposals it applies to"}
	}
	root, source, draft, suggestions, declined := a.proposing(request)
	if source == nil {
		return declined.test()
	}
	approved, approval, err := testauthor.Approve(draft, suggestions, *request.Review)
	if err != nil {
		return TestResult{State: Failed, Reason: err.Error()}
	}
	return authored(root, source, approved, TestDraft{Suggestions: &suggestions, Approval: &approval})
}

// proposing verifies the case and derives the proposals the request asks for.
// Both calls read the run the same way, because approving is proposing and then
// applying the decisions: the set a review is applied to is read out of the run
// rather than carried back from the window.
func (a *App) proposing(request TestRequest) (string, *bundle.Bundle, testauthor.Draft, testauthor.Suggestions, refusal) {
	root, source, draft, declined := a.authoring(request)
	if source == nil {
		return "", nil, draft, testauthor.Suggestions{}, declined
	}
	if request.Suggest == nil {
		return "", nil, draft, testauthor.Suggestions{}, refusal{Failed, "a suggestion names the reviewed run to propose expectations from"}
	}
	suggestions, err := testauthor.Suggest(root, draft, *request.Suggest)
	if err != nil {
		return "", nil, draft, testauthor.Suggestions{}, refusal{Failed, err.Error()}
	}
	return root, source, draft, suggestions, refusal{}
}

// authoring verifies the case a request names and returns the draft to work
// from. A request carrying no draft starts one bound to the evidence just
// verified, so the contract a draft declares is written here rather than in the
// interface.
func (a *App) authoring(request TestRequest) (string, *bundle.Bundle, testauthor.Draft, refusal) {
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return "", nil, testauthor.Draft{}, declined
	}
	path, err := artifactpath.Child(root, request.Case)
	if err != nil {
		return "", nil, testauthor.Draft{}, refusal{Failed, "a case must be named by one directory entry of the open workspace"}
	}
	source, err := operation.OpenVerifiedCase(path, request.Identity)
	if err != nil {
		return "", nil, testauthor.Draft{}, refusal{Failed, err.Error()}
	}
	draft := request.Draft
	if draft.Schema == "" {
		started, err := testauthor.NewDraft(request.Case, source.Identity)
		if err != nil {
			return "", nil, testauthor.Draft{}, refusal{Failed, err.Error()}
		}
		draft = started
	}
	return root, source, draft, refusal{}
}

// authored reports what one draft means. A draft that has answered nothing is
// Empty with the reason it is empty: an unanswered test is a state a person is
// in on the way to one, not a failure.
func authored(root string, source *bundle.Bundle, draft testauthor.Draft, saved TestDraft) TestResult {
	resolution, err := testauthor.Resolve(root, source, draft)
	if err != nil {
		return TestResult{State: Failed, Reason: err.Error()}
	}
	saved.Draft, saved.Resolution = draft, resolution
	if len(resolution.Missing) == len(testauthor.Stages()) {
		return TestResult{State: Empty, Reason: "this test has not been answered yet", Test: &saved}
	}
	return TestResult{State: Completed, Test: &saved}
}

// sampleAuthoring admits only the frozen case at the exact bundled practice
// target. It cannot author tests over a renamed customer case or external target.
func sampleAuthoring(request TestRequest, root string, source *bundle.Bundle) bool {
	if request.Case != guide.CaseName || source.Identity != guide.CaseIdentity {
		return false
	}
	if request.Draft.Target != "" && request.Draft.Target != guide.TargetName {
		return false
	}
	if request.Answer.Target != "" && request.Answer.Target != guide.TargetName {
		return false
	}
	target, err := replay.ReadDeclaredTarget(filepath.Join(root, guide.TargetName))
	return err == nil && reflect.DeepEqual(target, guide.Target())
}
