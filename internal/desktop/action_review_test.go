package desktop_test

// A reviewed action binds what its review showed: the exact case, the target
// configuration and the key material it trusts, the send policy, the
// operation policy and its grants, and the output. Every one of them is read
// again at the final Send, and a change is answered as a stale review with a
// fresh one — never a send, never a refreshed plan executed on the old click.
// Every send here reaches only a loopback receiver the test starts, so what a
// refusal kept from leaving is counted.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
)

// sendProject is a named project holding one case of a booking and its
// reschedule, a laboratory at the receiver and a send policy approving only
// loopback. It returns the refs of the case and the laboratory.
func sendProject(t *testing.T, app *desktop.App, context desktop.RequestContext, address string) (desktop.ItemRef, desktop.ItemRef) {
	t.Helper()
	root := context.Project
	writeCase(t, root, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7")))
	writeDocument(t, root, "lab.json", replayTarget(address, "nonproduction"))
	writeDocument(t, root, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
	return listed(t, app, root, desktop.CaseItem)["@incident"].Ref, listed(t, app, root, desktop.EnvironmentItem)["lab-replay"].Ref
}

func sendRequest(context desktop.RequestContext, incident, lab desktop.ItemRef) desktop.PrepareActionRequest {
	return desktop.PrepareActionRequest{Context: context, Action: desktop.ReplaySendAction, Items: []desktop.ItemRef{incident}, Destination: &lab,
		Replay: &desktop.ReplayActionOptions{Messages: []string{"s0001-e000001", "s0001-e000002"}, Policy: "policy.json"}}
}

func prepared(t *testing.T, app *desktop.App, request desktop.PrepareActionRequest) *desktop.ActionReview {
	t.Helper()
	result := app.PrepareAction(request)
	if result.State != desktop.Completed || result.Review == nil || !result.Review.Ready || result.Review.Token == "" {
		t.Fatalf("prepare: %+v", result)
	}
	return result.Review
}

func TestAReviewedSendRefusesEveryChangedBindingAndSendsOnce(t *testing.T) {
	receiver := newReplayReceiver(t)
	app, context := namedProject(t)
	root := context.Project
	incident, lab := sendProject(t, app, context, receiver.address)
	request := sendRequest(context, incident, lab)

	review := prepared(t, app, request)
	if review.Consent != desktop.SendConsent || review.Destination.Address != receiver.address || review.Destination.Classification != "nonproduction" ||
		review.Destination.Output != "replay-001" || len(review.Items) != 2 || review.Items[0].Ref.ID != incident.ID || review.Items[1].Ref.ID != lab.ID || review.ExpiresAt == "" ||
		review.Replay == nil || len(review.Replay.Messages) != 2 || len(review.Requirements) != 0 {
		t.Fatalf("the review: %+v", review)
	}
	if receiver.reached() != 0 || slices.Contains(entriesOf(t, root), "replay-001") {
		t.Fatal("preparing a review sent or wrote")
	}

	// Each binding changed after the review: the final Send is refused as a
	// stale review, with a refreshed one to look at, and nothing leaves.
	lanes := []struct {
		name           string
		change, undo   func()
		policySwitched bool
	}{
		{name: "case", change: func() {
			os.RemoveAll(filepath.Join(root, "incident"))
			writeCase(t, root, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s12.hl7")))
		}, undo: func() {
			os.RemoveAll(filepath.Join(root, "incident"))
			writeCase(t, root, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7")))
		}},
		{name: "target", change: func() {
			writeDocument(t, root, "lab.json", strings.Replace(replayTarget(receiver.address, "nonproduction"), `"5s"`, `"4s"`, 1))
		}, undo: func() { writeDocument(t, root, "lab.json", replayTarget(receiver.address, "nonproduction")) }},
		{name: "send policy", change: func() {
			writeDocument(t, root, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32","10.1.0.0/16"]}`)
		}, undo: func() {
			writeDocument(t, root, "policy.json", `{"schema":"readmit-send-policy/v1","approved_destinations":["127.0.0.1/32"]}`)
		}},
		{name: "destination", change: func() {
			if err := os.Mkdir(filepath.Join(root, "replay-001"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, undo: func() { os.Remove(filepath.Join(root, "replay-001")) }},
	}
	for _, lane := range lanes {
		review := prepared(t, app, request)
		lane.change()
		stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "click-" + strings.ReplaceAll(lane.name, " ", "-")})
		if stale.Outcome != desktop.ActionStale || stale.Replay != nil || stale.Context != context {
			t.Fatalf("%s changed: %+v", lane.name, stale)
		}
		if lane.name != "case" && (stale.Refreshed == nil || stale.Refreshed.Token == "" || stale.Refreshed.Token == review.Token) {
			t.Fatalf("%s changed: no refreshed review to look at: %+v", lane.name, stale)
		}
		// The old click on the old review is spent; nothing refreshed is
		// executed without a new click.
		if again := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "another-" + lane.name}); again.Outcome != desktop.ActionRefused {
			t.Fatalf("%s: a spent review executed: %+v", lane.name, again)
		}
		lane.undo()
		if receiver.reached() != 0 {
			t.Fatalf("%s changed and the target was reached", lane.name)
		}
	}

	// The key material a TLS target trusts is bound as well.
	writeDocument(t, root, "ca.pem", string(newHubTestAuthority(t, "replay-lab").pem))
	writeDocument(t, root, "lab.json", strings.Replace(replayTarget(receiver.address, "nonproduction"), `"transport":"plain"`, `"transport":"tls","ca_file":"ca.pem"`, 1))
	secured := prepared(t, app, request)
	writeDocument(t, root, "ca.pem", string(newHubTestAuthority(t, "another-lab").pem))
	if stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: secured.Token, IntentID: "click-key"}); stale.Outcome != desktop.ActionStale {
		t.Fatalf("a replaced certificate authority: %+v", stale)
	}
	writeDocument(t, root, "lab.json", replayTarget(receiver.address, "nonproduction"))

	// The operation policy and its grants: a window whose license grants no
	// runner authority is a different reviewer's authority.
	restricted := windowWith(t, authorOnlyPolicy(t))
	if review := restricted.PrepareAction(request); review.Review == nil || review.Review.Ready || review.Review.Token != "" || review.Review.Refusal == "" {
		t.Fatalf("a window without runner authority was offered a send: %+v", review)
	}
	// The same window's license changed to one without runner authority
	// after the review was shown: the grants it bound are gone.
	granted := prepared(t, app, request)
	if selected := app.SelectOperationPolicy(authorOnlyPolicy(t)); selected.State != desktop.Completed {
		t.Fatal(selected)
	}
	if stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: granted.Token, IntentID: "click-role"}); stale.Outcome != desktop.ActionStale || stale.Replay != nil {
		t.Fatalf("a changed license and role: %+v", stale)
	}
	if selected := app.SelectOperationPolicy(testlicense.New(t)); selected.State != desktop.Completed {
		t.Fatal(selected)
	}
	if receiver.reached() != 0 {
		t.Fatal("a refused final action reached the target")
	}

	// The send itself: once, and a repeated click is answered with the
	// original result without sending again.
	final := prepared(t, app, request)
	sent := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: final.Token, IntentID: "send-click"})
	if sent.State != desktop.Completed || sent.Outcome != desktop.ActionCompleted || sent.Replay == nil || sent.Replay.Output != final.Destination.Output || sent.Operation == "" {
		t.Fatalf("the reviewed send: %+v", sent)
	}
	if len(receiver.received()) != 2 {
		t.Fatalf("the send delivered %d messages", len(receiver.received()))
	}
	repeated := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: final.Token, IntentID: "send-click"})
	if !repeated.Replayed || repeated.Replay == nil || repeated.Replay.Identity != sent.Replay.Identity || repeated.Operation != sent.Operation {
		t.Fatalf("a repeated click: %+v", repeated)
	}
	if changed := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: final.Token, IntentID: "send-click", Decisions: desktop.ReviewDecisions{Rationale: "other"}}); changed.Outcome != desktop.ActionRefused {
		t.Fatalf("a different request under one click: %+v", changed)
	}
	if reused := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: final.Token, IntentID: "second-click"}); reused.Outcome != desktop.ActionRefused {
		t.Fatalf("a used review was used again: %+v", reused)
	}
	if len(receiver.received()) != 2 || receiver.reached() != 1 {
		t.Fatalf("a repeated click resent: %d messages over %d connections", len(receiver.received()), receiver.reached())
	}
	// The run is catalogued with what it actually did.
	if run := listed(t, app, root, desktop.RunItem)["@"+final.Destination.Output]; run.Summary.Run == nil || run.Summary.Run.Outcome != "accepted" {
		t.Fatalf("the run: %+v", run)
	}
}

// A review dies with the process that prepared it and when it is withdrawn:
// nothing is resent or renewed by restoring anything.
func TestAReviewDoesNotSurviveARestartOrAWithdrawal(t *testing.T) {
	receiver := newReplayReceiver(t)
	state := t.TempDir()
	folder := t.TempDir()
	app := activatedApp(t, &chooser{folder: folder}, state)
	app.ChooseProjectLocation()
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Restarts"})
	incident, lab := sendProject(t, app, created.Context, receiver.address)
	review := prepared(t, app, sendRequest(created.Context, incident, lab))

	restarted := activatedApp(t, &chooser{folder: folder}, state)
	if after := restarted.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: created.Context, Token: review.Token, IntentID: "after-restart"}); after.Outcome != desktop.ActionRefused {
		t.Fatalf("a review survived a restart: %+v", after)
	}
	if withdrawn := app.WithdrawReview(review.Token); withdrawn.State != desktop.Completed {
		t.Fatalf("withdraw: %+v", withdrawn)
	}
	if after := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: created.Context, Token: review.Token, IntentID: "after-withdrawal"}); after.Outcome != desktop.ActionRefused {
		t.Fatalf("a withdrawn review executed: %+v", after)
	}
	if receiver.reached() != 0 {
		t.Fatal("a dead review reached the target")
	}
	// A draft never retains a live review.
	live := prepared(t, app, sendRequest(created.Context, incident, lab))
	retained := app.SaveEditorDraft(desktop.EditorDraft{Kind: "note", Workspace: created.Context.Project, ContentSchema: desktop.NoteDraftSchema,
		Content: []byte(`{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"` + live.Token + `"}`)})
	if retained.State != desktop.Failed || !strings.Contains(retained.Reason, "review") {
		t.Fatalf("a draft retained a review: %+v", retained)
	}
}

// Approve records a durable decision about one exact version and sends
// nothing. The decision stays evidence of that version: a changed revision is
// a different review, and the recorded approval still names the original.
func TestAnApprovalIsDurableAndStaysTiedToItsExactVersion(t *testing.T) {
	app, root := releaseWorkspace(t)
	writeProject(t, root, "")
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("%+v", opened)
	}
	context := opened.Context
	suites := listed(t, app, root, desktop.SuiteItem)
	nightly := suites["nightly"]
	if !slices.Contains(nightly.Capabilities, desktop.ApprovePromotionAction) {
		t.Fatalf("the suite: %+v", nightly)
	}
	request := desktop.PrepareActionRequest{Context: context, Action: desktop.ApprovePromotionAction, Items: []desktop.ItemRef{nightly.Ref},
		Promotion: &desktop.PromotionActionOptions{Environment: "east", Releases: "releases.json", Revision: "fixture-build-7"}}
	review := prepared(t, app, request)
	if review.Consent != desktop.ApproveConsent || review.Promotion == nil || !slices.Equal(review.Requirements, []desktop.ReviewRequirement{desktop.RationaleRequirement}) {
		t.Fatalf("the approval review: %+v", review)
	}
	// A changed revision assumption file between review and Approve: the
	// release pins the approval would record are no longer what it showed.
	changedReview := prepared(t, app, request)
	pins := read(t, filepath.Join(root, "releases.json"))
	writeDocument(t, root, "releases.json", `{"schema":"readmit-suite-releases/v1","tests":[]}`)
	if stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: changedReview.Token, IntentID: "approve-changed",
		Decisions: desktop.ReviewDecisions{Rationale: "changed"}}); stale.Outcome != desktop.ActionStale || stale.Approval != nil {
		t.Fatalf("a changed release pin set was approved: %+v", stale)
	}
	writeDocument(t, root, "releases.json", pins)
	// Approving needs its rationale; without one nothing is recorded and the
	// review stays usable.
	if missing := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "approve-1"}); missing.Outcome != desktop.ActionRefused {
		t.Fatalf("an approval without a rationale: %+v", missing)
	}
	approved := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "approve-2",
		Decisions: desktop.ReviewDecisions{Rationale: "Reviewed the east mapping"}})
	if approved.Outcome != desktop.ActionCompleted || approved.Approval == nil || approved.Approval.Reviewed != review.Promotion.Identity() {
		t.Fatalf("approve: %+v", approved)
	}
	record, err := suite.DecodePromotion([]byte(read(t, filepath.Join(root, approved.Approval.Output))))
	if err != nil || record.Identity() != approved.Approval.Identity || record.Reviewed != review.Promotion.Identity() || record.Rationale != "Reviewed the east mapping" {
		t.Fatalf("the durable record: %+v %v", record, err)
	}
	// A later revision assumption is a different version: its review names a
	// different identity, and the recorded approval is still exactly the old.
	later := request
	later.Promotion = &desktop.PromotionActionOptions{Environment: "east", Releases: "releases.json", Revision: "fixture-build-8"}
	if next := prepared(t, app, later); next.Promotion.Identity() == record.Reviewed {
		t.Fatal("a changed version reviewed as the approved one")
	}
	if kept, _ := suite.DecodePromotion([]byte(read(t, filepath.Join(root, approved.Approval.Output)))); kept.Identity() != record.Identity() {
		t.Fatal("the durable approval changed")
	}
}

// Export binds the exact review identity and writes the packet the export
// operation writes, without the person transcribing that identity; an export
// review that changed after its review exports nothing.
func TestAReviewedExportBindsItsReviewWithoutATypedIdentity(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	derived(t, app, privacyRequest(root, "policy.json"))
	writeProject(t, root, "")
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("%+v", opened)
	}
	request := desktop.PrepareActionRequest{Context: opened.Context, Action: desktop.ExportPacketAction,
		Export: &desktop.ExportActionOptions{Review: "review", LocalState: "review-private"}}
	review := prepared(t, app, request)
	if review.Consent != desktop.ExportConsent || review.Export == nil || review.Export.Unresolved != 0 || review.Destination.Output == "" {
		t.Fatalf("the export review: %+v", review)
	}
	exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: opened.Context, Token: review.Token, IntentID: "export-1"})
	if exported.Outcome != desktop.ActionCompleted || exported.Export == nil || exported.Export.Packet != review.Destination.Output {
		t.Fatalf("export: %+v", exported)
	}
	// A second review of the same export names a fresh output; blocking the
	// review after it was shown exports nothing.
	second := prepared(t, app, request)
	if second.Destination.Output == review.Destination.Output {
		t.Fatalf("the output was not fresh: %+v", second)
	}
	blocked := privacyFixture(t, "policy.json", "policy-blocked")
	os.RemoveAll(filepath.Join(root, "review"))
	os.RemoveAll(filepath.Join(root, "review-private"))
	derived(t, app, privacyRequest(blocked, "policy.json"))
	copyEntry(t, filepath.Join(blocked, "review"), filepath.Join(root, "review"))
	copyEntry(t, filepath.Join(blocked, "review-private"), filepath.Join(root, "review-private"))
	if stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: opened.Context, Token: second.Token, IntentID: "export-2"}); stale.Outcome != desktop.ActionStale || stale.Export != nil {
		t.Fatalf("a changed review exported: %+v", stale)
	}
	if slices.Contains(entriesOf(t, root), second.Destination.Output) {
		t.Fatal("a stale export wrote its packet")
	}
}

// Stop names the operation the click runs as: another operation's identity
// stops nothing, and the send's own stops it at the message in flight, whose
// delivery is uncertain: the outcome is uncertain, never a clean stop.
func TestStopTargetsTheReviewedSendByItsOperation(t *testing.T) {
	receiver := newReplayReceiver(t)
	receiver.hold()
	app, context := namedProject(t)
	incident, lab := sendProject(t, app, context, receiver.address)
	review := prepared(t, app, sendRequest(context, incident, lab))
	answered := make(chan desktop.ReviewedActionResult, 1)
	go func() {
		answered <- app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "send-click"})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for len(receiver.received()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first message never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.CancelOperation("another-click")
	select {
	case result := <-answered:
		t.Fatalf("another operation's stop ended the send: %+v", result)
	case <-time.After(200 * time.Millisecond):
	}
	app.CancelOperation("send-click")
	result := <-answered
	if result.Outcome != desktop.ActionUncertain || result.State != desktop.Cancelled || result.Operation != "send-click" || result.Replay == nil || result.Replay.Uncertain != 1 {
		t.Fatalf("the stopped send: %+v", result)
	}
	receiver.releaseHeld()
	if len(receiver.received()) != 1 {
		t.Fatalf("a stopped send sent %d messages", len(receiver.received()))
	}
}
