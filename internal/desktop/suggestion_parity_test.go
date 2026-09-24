package desktop_test

// Proposing expectations in the window reads a reviewed run through the
// verifying reader `readmit diff` opens a result directory with, and approving
// them writes the spec `readmit test` reads. The runs here are the ones the
// installed native window retained in September
// (testdata/acceptance/native-109): its post-fix result passed and its
// baseline did not. They are copied into a workspace and never rewritten.
// Approving the ledger proposal of the passing run, named as that test named
// its expectation, over the answers that test gave, writes the spec the native
// window saved by hand byte for byte, asserting what that run decided: a
// proposal approved unedited is what the same run decided.

import (
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// nativeAcceptance is the evidence the installed native window retained.
var nativeAcceptance = filepath.Join("..", "..", "testdata", "acceptance", "native-109")

// retainedRunWorkspace is a workspace holding copies of the retained case and
// both retained results, beside the practice endpoint the retained spec names.
func retainedRunWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{guide.CaseName, "baseline", "post-fix"} {
		copyEntry(t, filepath.Join(nativeAcceptance, name), filepath.Join(root, name))
	}
	target, err := json.Marshal(guide.Target(), json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, guide.TargetName, string(target)+"\n")
	return root
}

// retainedDraft opens the retained case and answers every stage but the
// expectations exactly as the retained native test answered them.
func retainedDraft(t *testing.T, app *desktop.App, root string) desktop.TestRequest {
	t.Helper()
	opened := app.OpenCase(root, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the retained case: %+v", opened)
	}
	request := desktop.TestRequest{Workspace: root, Case: guide.CaseName, Identity: opened.Case.Identity}
	for _, given := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Native acceptance reschedule"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "test-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh synthetic practice receiver with an empty ledger."},
	} {
		request.Answer = given
		answered := app.AuthorTest(request)
		if answered.State != desktop.Completed || answered.Test == nil {
			t.Fatalf("%s: %+v", given.Stage, answered)
		}
		request.Draft = answered.Test.Draft
	}
	request.Answer = testauthor.Answer{}
	return request
}

// resultSides is what `readmit diff` reports about the two result directories
// it read, through its machine output.
func resultSides(t *testing.T, left, right string) (string, [2]map[string]any) {
	t.Helper()
	stdout, stderr, err := commandLine(t, "diff", left, right, "--format", "json")
	if err != nil {
		return stderr, [2]map[string]any{}
	}
	var report struct {
		Left  map[string]any `json:"left"`
		Right map[string]any `json:"right"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("readmit diff printed %q: %v", stdout, err)
	}
	return "", [2]map[string]any{report.Left, report.Right}
}

// The window proposes from the retained passing run and from nothing else, and
// what it says the proposals came from is what the command line reads out of
// the same directory.
func TestTheWindowSuggestsFromTheRetainedRunTheCommandLineReads(t *testing.T) {
	app := workspaceApp(t)
	root := retainedRunWorkspace(t)
	request := retainedDraft(t, app, root)
	before := workspaceState(t, root)

	refused, sides := resultSides(t, filepath.Join(root, "baseline"), filepath.Join(root, "post-fix"))
	if refused != "" {
		t.Fatalf("readmit diff refused the retained results: %s", refused)
	}
	baseline, passed := sides[0], sides[1]
	if baseline["result_status"] != string(testrunner.AssertionFailure) || passed["result_status"] != string(testrunner.Pass) {
		t.Fatalf("the command line reads the retained verdicts as %v and %v", baseline["result_status"], passed["result_status"])
	}

	request.Suggest = &testauthor.SuggestionRequest{Result: "post-fix", Ledger: true, Positions: []string{"MSA-1"}}
	proposed := app.SuggestExpectations(request)
	if proposed.State != desktop.Completed || proposed.Test == nil || proposed.Test.Suggestions == nil {
		t.Fatalf("nothing was proposed from the retained passing run: %+v", proposed)
	}
	origin := proposed.Test.Suggestions.Origin
	if origin.Result != "post-fix" || origin.Identity != passed["identity"] || origin.Status != passed["result_status"] ||
		origin.Boundary != passed["result_boundary"] || origin.TargetIdentity != passed["target_identity"] {
		t.Fatalf("the window names the run %+v, the command line read %v", origin, passed)
	}
	artifact, err := testrunner.Open(filepath.Join(nativeAcceptance, "post-fix"))
	if err != nil {
		t.Fatal(err)
	}
	if origin.SpecIdentity != artifact.Result.SpecIdentity || origin.InputIdentity != request.Identity || origin.RunIdentity != artifact.Result.Run.Identity {
		t.Fatalf("the window names the evidence %+v, the retained result records %+v", origin, artifact.Result)
	}

	// The count proposed is the number of records the run's own final
	// observation holds, and each acknowledgement value is the one its
	// retained acknowledgement carries at that position.
	set := proposed.Test.Suggestions
	if len(set.Suggestions) != 3 || set.SupportedCount != 3 {
		t.Fatalf("the retained run proposed %+v", set.Suggestions)
	}
	if ledger := set.Suggestions[0]; ledger.Operator != testauthor.LedgerCount || ledger.Count == nil ||
		*ledger.Count != len(artifact.FinalObservation.Records) || ledger.Evidence.Digest != artifact.Result.FinalObservation.SHA256 {
		t.Fatalf("the ledger proposal is %+v", ledger)
	}
	for _, suggestion := range set.Suggestions[1:] {
		acknowledged := retainedAcknowledgement(t, artifact, suggestion.Message)
		if suggestion.Field == nil || suggestion.Field.State != hl7.Present || suggestion.Field.Text == nil ||
			*suggestion.Field.Text != acknowledged || suggestion.Evidence.Artifact != "post-fix" {
			t.Fatalf("%s proposes %+v, the retained acknowledgement holds %q", suggestion.ID, suggestion.Field, acknowledged)
		}
	}
	// Asking recorded nothing.
	if len(proposed.Test.Draft.Expectations) != 0 {
		t.Fatalf("proposing recorded %+v", proposed.Test.Draft.Expectations)
	}

	// A run the command line reads as failed, a result it cannot verify and a
	// case that is not a result propose nothing, and each refusal says which.
	broken := filepath.Join(root, "interrupted")
	copyEntry(t, filepath.Join(root, "post-fix"), broken)
	if err := os.Remove(filepath.Join(broken, "observation.json")); err != nil {
		t.Fatal(err)
	}
	if refused, _ := resultSides(t, filepath.Join(root, "baseline"), broken); refused == "" {
		t.Fatal("the command line read a result whose final observation is gone")
	}
	for entry, reason := range map[string]string{
		"baseline":      "expectations are suggested from a run whose own expectations held; this one did not",
		"interrupted":   "that entry is not a run result this release verifies",
		guide.CaseName:  "that entry is not a run result this release verifies",
		"nothing-there": "a reviewed run is one directory entry of the open workspace",
	} {
		request.Suggest = &testauthor.SuggestionRequest{Result: entry, Ledger: true}
		if refusedHere := app.SuggestExpectations(request); refusedHere.State != desktop.Failed || refusedHere.Reason != reason || refusedHere.Test != nil {
			t.Fatalf("%s: the window answered %+v, want the refusal %q", entry, refusedHere, reason)
		}
	}
	if err := os.RemoveAll(broken); err != nil {
		t.Fatal(err)
	}
	if after := workspaceState(t, root); !maps.Equal(after, before) {
		t.Fatal("proposing expectations changed the workspace")
	}
}

// retainedAcknowledgement is MSA-1 of the acknowledgement the retained run
// received for one message, read from its payload file.
func retainedAcknowledgement(t *testing.T, artifact *testrunner.Artifact, message string) string {
	t.Helper()
	for _, event := range artifact.Run.Events {
		if event.SourceOccurrence != message {
			continue
		}
		received, err := artifact.Run.Raw(event.Received)
		if err != nil {
			t.Fatal(err)
		}
		for _, segment := range strings.Split(strings.Trim(string(received), "\x0b\x1c\r\n"), "\r") {
			if fields := strings.Split(segment, "|"); fields[0] == "MSA" && len(fields) > 1 {
				return fields[1]
			}
		}
	}
	t.Fatalf("the retained run received no acknowledgement for %s", message)
	return ""
}

// Approving is the only thing that records a proposal, and what it records is
// the test the retained run executed: the ledger count approved under the name
// that test gave it, one acknowledgement proposal rejected and one never
// looked at, writes the very bytes the native window saved, and asserts what
// the passing run decided. A review of another run, a decision naming nothing
// proposed and a run that changed on disk since it was read record nothing.
func TestApprovedSuggestionsWriteTheSpecTheRetainedRunExecuted(t *testing.T) {
	app := workspaceApp(t)
	root := retainedRunWorkspace(t)
	request := retainedDraft(t, app, root)
	request.Suggest = &testauthor.SuggestionRequest{Result: "post-fix", Ledger: true, Positions: []string{"MSA-1"}}
	proposed := app.SuggestExpectations(request)
	if proposed.State != desktop.Completed || proposed.Test == nil || proposed.Test.Suggestions == nil {
		t.Fatalf("nothing was proposed: %+v", proposed)
	}
	set := proposed.Test.Suggestions
	ledger, booking, reschedule := set.Suggestions[0], set.Suggestions[1], set.Suggestions[2]
	decided := testauthor.Review{
		Result: "post-fix", Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{
			{Suggestion: ledger.ID, Approved: true, ID: "one-appointment"},
			{Suggestion: booking.ID, Approved: false},
		},
	}

	for reason, review := range map[string]testauthor.Review{
		"this review was made against a different run; suggest expectations again before approving them": {
			Result: "post-fix", Identity: strings.Repeat("0", 64), Decisions: decided.Decisions},
		"a decision names one suggestion of this set": {Result: "post-fix", Identity: set.Origin.Identity,
			Decisions: []testauthor.Decision{{Suggestion: "nothing-proposed-this", Approved: true}}},
	} {
		refused := request
		refused.Review = &review
		if result := app.ApproveExpectations(refused); result.State != desktop.Failed || result.Reason != reason || result.Test != nil {
			t.Fatalf("a review was applied or refused otherwise (want %q): %+v", reason, result)
		}
	}

	// The run changed on disk between the proposal and the review: its
	// observation is not what was proposed from, and nothing is recorded.
	observation := filepath.Join(root, "post-fix", "observation.json")
	retained := mustRead(t, observation)
	writeDocument(t, filepath.Join(root, "post-fix"), "observation.json", strings.Replace(string(retained), "}", " }", 1))
	changed := request
	changed.Review = &decided
	if result := app.ApproveExpectations(changed); result.State != desktop.Failed || result.Reason != "that entry is not a run result this release verifies" {
		t.Fatalf("a review was applied to a run that changed since it was read: %+v", result)
	}
	writeDocument(t, filepath.Join(root, "post-fix"), "observation.json", string(retained))

	request.Review = &decided
	recorded := app.ApproveExpectations(request)
	if recorded.State != desktop.Completed || recorded.Test == nil || recorded.Test.Approval == nil {
		t.Fatalf("the decisions were not recorded: %+v", recorded)
	}
	approval := recorded.Test.Approval
	if approval.ApprovedCount != 1 || approval.RejectedCount != 1 || approval.NotReviewedCount != 1 {
		t.Fatalf("the review is reported as %+v", approval)
	}
	for _, outcome := range approval.Reviewed {
		want := map[string]string{ledger.ID: testauthor.Approved, booking.ID: testauthor.Rejected, reschedule.ID: testauthor.NotReviewed}[outcome.Suggestion]
		if outcome.Outcome != want {
			t.Fatalf("%s is reported %s, want %s", outcome.Suggestion, outcome.Outcome, want)
		}
	}

	request.Draft, request.Suggest, request.Review = recorded.Test.Draft, nil, nil
	request.Output = "reviewed-test.json"
	saved := app.SaveTest(request)
	if saved.State != desktop.Completed || saved.Test == nil || saved.Test.Output != "reviewed-test.json" {
		t.Fatalf("the reviewed test was not written: %+v", saved)
	}
	// The native window saved that test by hand, and the run retained it as
	// the spec it executed: the approved spec is the saved one byte for byte,
	// and its assertions are the ones the run decided.
	written := read(t, filepath.Join(root, "reviewed-test.json"))
	if authored := read(t, filepath.Join(nativeAcceptance, "native-test.json")); written != authored {
		t.Fatalf("the approved spec is not the one the native window saved:\n%s\n%s", written, authored)
	}
	if saved.Test.Identity != fileDigest(t, filepath.Join(root, "reviewed-test.json")) {
		t.Fatalf("the window names the spec %s", saved.Test.Identity)
	}
	spec, err := testrunner.ReadSpec(filepath.Join(root, "reviewed-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	executed, err := testrunner.Open(filepath.Join(nativeAcceptance, "post-fix"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Assertions) != len(executed.Result.Assertions) {
		t.Fatalf("the approved spec asserts %+v, the retained run decided %+v", spec.Assertions, executed.Result.Assertions)
	}
	for i, decided := range executed.Result.Assertions {
		approvedJSON, _ := json.Marshal(spec.Assertions[i], json.Deterministic(true))
		decidedJSON, _ := json.Marshal(decided.Assertion, json.Deterministic(true))
		if string(approvedJSON) != string(decidedJSON) || decided.Status != "passed" {
			t.Fatalf("the approved spec asserts %s, the retained run decided %s as %s", approvedJSON, decidedJSON, decided.Status)
		}
	}

	// `readmit test` reads the spec the window wrote beside the case it names.
	stdout, stderr, err := commandLine(t, "test", filepath.Join(root, "reviewed-test.json"))
	if err != nil || stderr != "" || !strings.Contains(stdout, "Observation boundary: appointment-ledger") || !strings.Contains(stdout, "Messages: 2") {
		t.Fatalf("readmit test: %v %q %q", err, stdout, stderr)
	}
}
