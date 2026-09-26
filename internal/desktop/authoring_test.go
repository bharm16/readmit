package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// The same incident the reproducer panel reads: a booking, its acknowledgement,
// a reschedule, and one occurrence nothing decoded.
const authoringTarget = `{"schema":"readmit-target/v1","test_endpoint":true,"address":"127.0.0.1:2575","transport":"plain","approved_transport":false,"connect_timeout":"2s","message_timeout":"5s","max_ack_bytes":65536}`

// authoringWorkspace is that incident in an open workspace with one target
// configuration beside it, the app that reads both, and the identity the grid
// would have displayed.
func authoringWorkspace(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	root := t.TempDir()
	app := workspaceApp(t)
	incident := writeCase(t, root, "incident", framed(repBooking)+framed(repAccepted)+framed(repReschedule)+framed(repGarbage))
	if err := os.WriteFile(filepath.Join(root, "test-target.json"), []byte(authoringTarget), 0600); err != nil {
		t.Fatal(err)
	}
	return app, root, incident.Identity
}

func authoringRequest(root, identity string, draft testauthor.Draft, answer testauthor.Answer) desktop.TestRequest {
	return desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity, Draft: draft, Answer: answer}
}

// answer applies one stage and fails the test if the facade refused it.
func answer(t *testing.T, app *desktop.App, root, identity string, draft testauthor.Draft, given testauthor.Answer) testauthor.Draft {
	t.Helper()
	result := app.AuthorTest(authoringRequest(root, identity, draft, given))
	if result.State != desktop.Completed || result.Test == nil {
		t.Fatalf("the facade refused the %s stage: %+v", given.Stage, result)
	}
	return result.Test.Draft
}

// ackText is an expected acknowledgement value, which is a customer-local
// literal a person typed rather than anything read out of the evidence.
func ackText(text string) *testrunner.FieldValue {
	return &testrunner.FieldValue{State: hl7.Present, Text: &text}
}

// A person answers the flow one stage at a time and saves the test. The window
// decides nothing: it sends the draft and the answer, and the engine reports
// the question that is still open and, at the end, the spec that was written.
func TestAuthoringATestAnswersOneStageAtATimeAndWritesASpec(t *testing.T) {
	app, root, identity := authoringWorkspace(t)

	opened := app.AuthorTest(desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity})
	if opened.State != desktop.Empty || opened.Test == nil || opened.Test.Draft.Case.Identity != identity {
		t.Fatalf("a request carrying no draft did not start one bound to this evidence: %+v", opened)
	}
	if opened.Test.Resolution.Stage != testauthor.StageName {
		t.Fatalf("the flow did not open on its first question: %q", opened.Test.Resolution.Stage)
	}
	// The targets of the workspace are offered rather than typed from memory,
	// with the classification each configuration records.
	if len(opened.Test.Resolution.Targets) != 1 || opened.Test.Resolution.Targets[0].Name != "test-target.json" {
		t.Fatalf("the workspace offered %+v", opened.Test.Resolution.Targets)
	}

	draft := opened.Test.Draft
	count := 1
	for _, given := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{repRescheduleID, repBookingID}},
		{Stage: testauthor.StageTarget, Target: "test-target.json"},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "test-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Restart the listener from an empty ledger before running this."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &count},
			{ID: "booking-ack", Operator: testauthor.ACKFieldEquals, Message: repBookingID, Selector: "MSA-1", Field: ackText("AA")},
		}},
	} {
		draft = answer(t, app, root, identity, draft, given)
	}

	final := app.AuthorTest(authoringRequest(root, identity, draft, testauthor.Answer{}))
	if final.State != desktop.Completed || len(final.Test.Resolution.Missing) != 0 {
		t.Fatalf("an answered draft still reported work to do: %+v", final.Test.Resolution)
	}
	// The occurrences are reported in the order the case records them, not the
	// order they were clicked in.
	if got := final.Test.Resolution.Messages; len(got) != 2 || got[0] != repBookingID || got[1] != repRescheduleID {
		t.Fatalf("selected messages were not reported in evidence order: %v", got)
	}

	saved := app.SaveTest(desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity, Draft: draft, Output: "reschedule-test.json"})
	if saved.State != desktop.Completed || saved.Test == nil || saved.Test.Output != "reschedule-test.json" {
		t.Fatalf("the test spec was not written: %+v", saved)
	}
	written, err := os.ReadFile(filepath.Join(root, "reschedule-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.DecodeSpec(written)
	if err != nil {
		t.Fatalf("the window wrote a document the runner does not read: %v", err)
	}
	if spec.Input.Case != "incident" || spec.Target != "test-target.json" || len(spec.Assertions) != 2 {
		t.Fatalf("the written spec does not say what was answered: %+v", spec)
	}
	// Writing the spec changed no evidence: the case is still the case the
	// grid verified.
	if opened := app.OpenCase(root, "incident"); opened.State != desktop.Completed || opened.Case.Identity != identity {
		t.Fatalf("authoring a test changed the evidence it was authored from: %+v", opened)
	}
}

// Every refusal this surface has, and the one state each of them reports. A
// refused answer leaves the draft exactly as it was, which is what makes the
// flow recoverable: the next answer continues from the same draft.
func TestAuthoringRefusesWhatTheEvidenceAndTheWorkspaceDoNotSupport(t *testing.T) {
	app, root, identity := authoringWorkspace(t)
	draft := answer(t, app, root, identity, testauthor.Draft{}, testauthor.Answer{Stage: testauthor.StageName, Name: "A regression"})

	for name, request := range map[string]desktop.TestRequest{
		"a workspace that is not a folder":    {Workspace: filepath.Join(root, "incident", "manifest.json"), Case: "incident", Identity: identity},
		"a case that is not one entry":        {Workspace: root, Case: "../incident", Identity: identity},
		"a case this workspace does not hold": {Workspace: root, Case: "absent", Identity: identity},
		"evidence whose identity changed":     {Workspace: root, Case: "incident", Identity: "0000000000000000000000000000000000000000000000000000000000000000"},
		"no identity at all":                  {Workspace: root, Case: "incident"},
	} {
		if result := app.AuthorTest(request); result.State != desktop.Failed || result.Test != nil {
			t.Errorf("the facade authored a test against %s: %+v", name, result)
		}
	}
	for name, given := range map[string]testauthor.Answer{
		"an acknowledgement as a sent message":  {Stage: testauthor.StageMessages, Messages: []string{repAcceptedID}},
		"an occurrence nothing decoded":         {Stage: testauthor.StageMessages, Messages: []string{repGarbageID}},
		"an occurrence this case does not hold": {Stage: testauthor.StageMessages, Messages: []string{"s0009-e000001"}},
		"a target this workspace does not hold": {Stage: testauthor.StageTarget, Target: "absent-target.json"},
		"a stage this flow never asks":          {Stage: "deadline", Name: "later"},
	} {
		result := app.AuthorTest(authoringRequest(root, identity, draft, given))
		if result.State != desktop.Failed || result.Reason == "" {
			t.Errorf("the facade accepted %s: %+v", name, result)
		}
	}
	// The draft the window is holding is untouched by every refusal above, so
	// the flow continues from exactly where it was.
	if kept := answer(t, app, root, identity, draft, testauthor.Answer{Stage: testauthor.StageMessages, Messages: []string{repBookingID}}); len(kept.Messages) != 1 || kept.Name != "A regression" {
		t.Fatalf("a refused answer changed the draft the window holds: %+v", kept)
	}
}

// Saving is refused before the flow has an answer to every question it asks,
// and a destination that already exists or reaches outside one entry of the
// workspace is refused rather than replaced.
func TestSavingATestIsRefusedUntilEveryStageIsAnswered(t *testing.T) {
	app, root, identity := authoringWorkspace(t)
	draft := answer(t, app, root, identity, testauthor.Draft{}, testauthor.Answer{Stage: testauthor.StageName, Name: "A regression"})
	if result := app.SaveTest(desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity, Draft: draft, Output: "partial.json"}); result.State != desktop.Failed {
		t.Fatalf("a half-answered draft was written as a test: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "partial.json")); !os.IsNotExist(err) {
		t.Fatal("a refused save left a file behind")
	}

	count := 0
	for _, given := range []testauthor.Answer{
		{Stage: testauthor.StageMessages, Messages: []string{repBookingID}},
		{Stage: testauthor.StageTarget, Target: "test-target.json"},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "test-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Restart the listener from an empty ledger."},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{{ID: "nothing-booked", Operator: testauthor.LedgerCount, Count: &count}}},
	} {
		draft = answer(t, app, root, identity, draft, given)
	}
	if result := app.SaveTest(desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity, Draft: draft, Output: "booking-test.json"}); result.State != desktop.Completed {
		t.Fatalf("an answered draft was not written: %+v", result)
	}
	for name, output := range map[string]string{
		"an entry that already exists":  "booking-test.json",
		"a name that is not one entry":  "specs/booking-test.json",
		"a destination inside the case": "incident",
		"no name at all":                "",
	} {
		result := app.SaveTest(desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity, Draft: draft, Output: output})
		if result.State != desktop.Failed || result.Reason == "" {
			t.Errorf("a test spec was written to %s: %+v", name, result)
		}
	}
	// A refused save wrote nothing and retracted nothing, so saving again under
	// a new name is all the recovery there is.
	if result := app.SaveTest(desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity, Draft: draft, Output: "booking-test-2.json"}); result.State != desktop.Completed {
		t.Fatalf("the flow did not recover from a refused save: %+v", result)
	}
}

// Both operations verify a case, so each one claims the operation slot rather
// than racing another, and the slot is released when the first one finishes.
func TestAuthoringOperationsRunOneAtATime(t *testing.T) {
	app, root, identity := authoringWorkspace(t)
	reentrant := &chooser{folder: root}
	second := activatedApp(t, reentrant, t.TempDir())
	var answered, written desktop.TestResult
	reentrant.before = func() {
		start := desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity, Output: "test.json"}
		answered = second.AuthorTest(start)
		written = second.SaveTest(start)
	}
	if opened := second.SelectWorkspace(); opened.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", opened)
	}
	for name, result := range map[string]desktop.TestResult{"answering a stage": answered, "saving a test": written} {
		if result.State != desktop.Busy || result.Test != nil {
			t.Errorf("%s ran while another operation held the facade: %+v", name, result)
		}
	}
	if recovered := app.AuthorTest(desktop.TestRequest{Workspace: root, Case: "incident", Identity: identity}); recovered.State != desktop.Empty {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", recovered)
	}
}

// Every reason this panel shows is a fixed sentence. Authoring forwards engine
// diagnostics rather than literals written in the facade, so the property
// `docs/desktop.md` states — a reason never repeats a path or a value — is
// asserted here rather than left to review. An expected value a person typed is
// the same customer-local literal the field it describes holds, which is exactly
// what a refusal must not echo back.
func TestNoAuthoringRefusalRepeatsAPathOrAValue(t *testing.T) {
	app, root, identity := authoringWorkspace(t)
	const secret = "MRN-SHOULD-NEVER-APPEAR"
	const selector = "PID[1]-3[1].1"
	draft := answer(t, app, root, identity, testauthor.Draft{}, testauthor.Answer{Stage: testauthor.StageName, Name: "A regression"})

	count := -1
	refusals := []desktop.TestResult{}
	for _, given := range []testauthor.Answer{
		{Stage: "exec-shell/v1", Name: secret},
		{Stage: testauthor.StageName, Name: secret, Target: secret},
		{Stage: testauthor.StageMessages, Messages: []string{"s0009-e000001"}},
		{Stage: testauthor.StageMessages, Messages: []string{repAcceptedID}},
		{Stage: testauthor.StageTarget, Target: secret + ".json"},
		{Stage: testauthor.StageBoundary, Boundary: secret},
		{Stage: testauthor.StageObservation, Observation: secret + ".json"},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: secret, Operator: testauthor.LedgerCount, Count: &count},
		}},
		{Stage: testauthor.StageExpectations, Expectations: []testauthor.Expectation{
			{ID: "leaky", Operator: testauthor.ACKFieldEquals, Message: "s0009-e000001", Selector: selector, Field: ackText(secret)},
		}},
	} {
		refusals = append(refusals, app.AuthorTest(authoringRequest(root, identity, draft, given)))
	}
	for _, output := range []string{"incident", "nested/one", "..", ""} {
		refusals = append(refusals, app.SaveTest(desktop.TestRequest{
			Workspace: root, Case: "incident", Identity: identity, Draft: draft, Output: output + secret,
		}))
	}
	for _, result := range refusals {
		if result.State != desktop.Failed || result.Reason == "" {
			t.Fatalf("a refusal gave the window nothing to show: %+v", result)
		}
		for _, leaked := range []string{root, secret, selector, "s0009"} {
			if strings.Contains(result.Reason, leaked) {
				t.Errorf("a refusal repeated %q: %s", leaked, result.Reason)
			}
		}
	}
}
