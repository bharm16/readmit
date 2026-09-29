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
	chosen := app.ChooseProjectLocation()
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Restarts", Location: chosen.Location})
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
	// The export review is a report of the project, offered for export; its
	// private local state is the project's to resolve.
	report := listed(t, app, root, desktop.ReportItem)["@review"]
	if report.Summary.Report == nil || report.Summary.Report.Form != "export-review" || report.Summary.Report.Status != "ready-for-approval" ||
		!slices.Contains(report.Capabilities, desktop.ExportPacketAction) {
		t.Fatalf("the export review: %+v", report)
	}
	request := desktop.PrepareActionRequest{Context: opened.Context, Action: desktop.ExportPacketAction, Items: []desktop.ItemRef{report.Ref}}
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

// An export review is a report of its project, paired with its private local
// state by the commitment it records, never by a name: two reviews each find
// their own, and a private state that is gone or held twice leaves the review
// a named, unreadable row that offers no export.
func TestAnExportReviewIsAReportWithItsPrivateStateResolved(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	derived(t, app, privacyRequest(root, "policy.json"))
	blocked, err := os.ReadFile("../../testdata/fixtures/redact-policy-blocked.json")
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "blocked.json", string(blocked))
	second := privacyRequest(root, "blocked.json")
	second.Output, second.LocalState = "second", "second-private"
	derived(t, app, second)
	writeProject(t, root, "")
	if opened := app.OpenNamedProject(root); opened.State != desktop.Completed {
		t.Fatalf("%+v", opened)
	}
	original := listed(t, app, root, desktop.CaseItem)["@original.case"]
	reports := listed(t, app, root, desktop.ReportItem)
	ready, held := reports["@review"], reports["@second"]
	if ready.Summary.Report == nil || ready.Summary.Report.Form != "export-review" || ready.Summary.Report.Status != "ready-for-approval" ||
		ready.Summary.Report.RelatedCase == nil || ready.Summary.Report.RelatedCase.ID != original.Ref.ID || !slices.Contains(ready.Capabilities, desktop.ExportPacketAction) {
		t.Fatalf("the ready review: %+v", ready)
	}
	if held.Summary.Report == nil || held.Summary.Report.Status != "blocked" || held.Summary.Report.RelatedCase == nil || slices.Contains(held.Capabilities, desktop.ExportPacketAction) {
		t.Fatalf("the blocked review: %+v", held)
	}
	// The ready review's private state replaced by the other's: one review
	// has none and the other two, and neither is offered for export.
	writeDocument(t, filepath.Join(root, "review-private"), "state.json", read(t, filepath.Join(root, "second-private", "state.json")))
	reports = listed(t, app, root, desktop.ReportItem)
	for _, name := range []string{"@review", "@second"} {
		if row := reports[name]; row.Availability != desktop.ItemUnreadable || row.Reason == "" || slices.Contains(row.Capabilities, desktop.ExportPacketAction) {
			t.Fatalf("%s without its one private state: %+v", name, row)
		}
	}
}

// deriveProject is the privacy fixture registered as a project, with the
// review of a derivation of its case prepared.
func deriveProject(t *testing.T) (*desktop.App, desktop.RequestContext, desktop.PrepareActionRequest) {
	t.Helper()
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	writeProject(t, root, "")
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("%+v", opened)
	}
	original := listed(t, app, root, desktop.CaseItem)["@original.case"]
	if !slices.Contains(original.Capabilities, desktop.DeriveReviewAction) {
		t.Fatalf("a case does not offer its derivation: %+v", original)
	}
	return app, opened.Context, desktop.PrepareActionRequest{Context: opened.Context, Action: desktop.DeriveReviewAction, Items: []desktop.ItemRef{original.Ref},
		DeriveReview: &desktop.DeriveReviewOptions{Spec: "spec.json", Policy: "policy.json", Inventory: "inventory.json"}}
}

// Deriving an export review needs its inventory declared complete in the
// final click, for exactly the inventory the review showed: without the
// declaration, or with one for other content, nothing is written, and an
// inventory edited after the review was shown is a stale review.
func TestAnExportReviewNeedsItsInventoryDeclaredInTheFinalAction(t *testing.T) {
	app, context, request := deriveProject(t)
	root := context.Project
	before := entries(t, root)
	review := prepared(t, app, request)
	if review.Consent != desktop.DeriveConsent || !slices.Equal(review.Requirements, []desktop.ReviewRequirement{desktop.InventoryDeclarationRequirement}) ||
		review.Derive == nil || review.Derive.Inventory.Digest == "" || review.Derive.Inventory.ResidualValues != 11 || review.Derive.Inventory.Entry != "inventory.json" ||
		review.Destination.Output != review.Derive.Review || review.Derive.Private == "" {
		t.Fatalf("the derivation's review: %+v", review)
	}
	for name, decisions := range map[string]desktop.ReviewDecisions{
		"no declaration":        {},
		"another inventory":     {DeclaredInventory: strings.Repeat("0", 64)},
		"generic consent alone": {Rationale: "reviewed", Confirmed: []string{"inventory.json"}},
	} {
		refused := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "derive-" + strings.ReplaceAll(name, " ", "-"), Decisions: decisions})
		if refused.Outcome != desktop.ActionRefused || refused.Derived != nil {
			t.Fatalf("%s: %+v", name, refused)
		}
		if after := entries(t, root); !slices.Equal(after, before) {
			t.Fatalf("%s: a refused derivation wrote %v", name, after)
		}
	}
	// The inventory edited after the review was shown: the declaration made
	// for what it showed is for other content.
	inventory := read(t, filepath.Join(root, "inventory.json"))
	writeDocument(t, root, "inventory.json", strings.Replace(inventory, `"PLANTED-DIAG-MAPLE"`, `"PLANTED-DIAG-MAPLE", "PLANTED-LATER-OAK"`, 1))
	stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "derive-edited",
		Decisions: desktop.ReviewDecisions{DeclaredInventory: review.Derive.Inventory.Digest}})
	if stale.Outcome != desktop.ActionStale || stale.Refreshed == nil || stale.Refreshed.Derive == nil ||
		stale.Refreshed.Derive.Inventory.Digest == review.Derive.Inventory.Digest || stale.Refreshed.Derive.Inventory.ResidualValues != 12 {
		t.Fatalf("an edited inventory: %+v", stale)
	}
	if after := entries(t, root); !slices.Equal(after, before) {
		t.Fatalf("a stale derivation wrote %v", after)
	}
	writeDocument(t, root, "inventory.json", inventory)
	final := prepared(t, app, request)
	derived := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: final.Token, IntentID: "derive-declared",
		Decisions: desktop.ReviewDecisions{DeclaredInventory: final.Derive.Inventory.Digest}})
	if derived.Outcome != desktop.ActionCompleted || derived.Derived == nil || derived.Derived.Review != final.Derive.Review || derived.Derived.Private != final.Derive.Private {
		t.Fatalf("the declared derivation: %+v", derived)
	}
	if !slices.Contains(entries(t, root), final.Derive.Review) || !slices.Contains(entries(t, root), final.Derive.Private) {
		t.Fatal("the derivation wrote no review")
	}
}

// A declaration is not consent to anything else: an export's final click
// requires none, and a declaration made for one derivation is not one made
// for another inventory.
func TestADeclarationIsNotGenericConsent(t *testing.T) {
	app, context, request := deriveProject(t)
	root := context.Project
	first := prepared(t, app, request)
	writeDocument(t, root, "other-inventory.json", strings.Replace(read(t, filepath.Join(root, "inventory.json")), `"PLANTED-DIAG-MAPLE"`, `"PLANTED-DIAG-MAPLE", "PLANTED-OTHER-FIR"`, 1))
	other := request
	other.DeriveReview = &desktop.DeriveReviewOptions{Spec: "spec.json", Policy: "policy.json", Inventory: "other-inventory.json"}
	second := prepared(t, app, other)
	if refused := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: second.Token, IntentID: "derive-borrowed",
		Decisions: desktop.ReviewDecisions{DeclaredInventory: first.Derive.Inventory.Digest}}); refused.Outcome != desktop.ActionRefused {
		t.Fatalf("a declaration for another inventory: %+v", refused)
	}
	derived := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: first.Token, IntentID: "derive-own",
		Decisions: desktop.ReviewDecisions{DeclaredInventory: first.Derive.Inventory.Digest}})
	if derived.Derived == nil || derived.Derived.State != "ready-for-approval" {
		t.Fatalf("derive: %+v", derived)
	}
	report := listed(t, app, root, desktop.ReportItem)["@"+derived.Derived.Review]
	export := prepared(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.ExportPacketAction, Items: []desktop.ItemRef{report.Ref}})
	if len(export.Requirements) != 0 {
		t.Fatalf("an export asks for more than its click: %+v", export.Requirements)
	}
	if exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: export.Token, IntentID: "export-click"}); exported.Outcome != desktop.ActionCompleted {
		t.Fatalf("export: %+v", exported)
	}
}

// bindingLane is one binding of a review changed after it was shown, and put
// back afterwards.
type bindingLane struct {
	name         string
	change, undo func()
	refreshed    bool
}

// refusesEveryChangedBinding prepares a fresh review for each lane, changes
// that binding and makes the final click: it is a stale review, with a
// refreshed one to look at when the action can still be prepared, the old
// review is spent, and wrote reports nothing was written.
func refusesEveryChangedBinding(t *testing.T, app *desktop.App, request desktop.PrepareActionRequest, decisions desktop.ReviewDecisions, lanes []bindingLane, wrote func() bool) {
	t.Helper()
	for _, lane := range lanes {
		review := prepared(t, app, request)
		lane.change()
		click := "click-" + strings.ReplaceAll(lane.name, " ", "-")
		stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: request.Context, Token: review.Token, IntentID: click, Decisions: decisions})
		if stale.Outcome != desktop.ActionStale || stale.Export != nil || stale.SuiteApproval != nil {
			t.Fatalf("%s changed: %+v", lane.name, stale)
		}
		if lane.refreshed && (stale.Refreshed == nil || stale.Refreshed.Token == "" || stale.Refreshed.Token == review.Token) {
			t.Fatalf("%s changed: no refreshed review to look at: %+v", lane.name, stale)
		}
		if again := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: request.Context, Token: review.Token, IntentID: click + "-again", Decisions: decisions}); again.Outcome != desktop.ActionRefused {
			t.Fatalf("%s: a spent review executed: %+v", lane.name, again)
		}
		if wrote() {
			t.Fatalf("%s changed and something was written", lane.name)
		}
		lane.undo()
	}
}

// An export binds its review, the private local state it was derived with,
// the operation policy and the packet it writes: each changed after the
// review was shown exports nothing.
func TestAReviewedExportRefusesEveryChangedBinding(t *testing.T) {
	app := workspaceApp(t)
	root := privacyFixture(t, "policy.json", "policy")
	derived(t, app, privacyRequest(root, "policy.json"))
	writeProject(t, root, "")
	opened := app.OpenNamedProject(root)
	if opened.State != desktop.Completed {
		t.Fatalf("%+v", opened)
	}
	report := listed(t, app, root, desktop.ReportItem)["@review"]
	request := desktop.PrepareActionRequest{Context: opened.Context, Action: desktop.ExportPacketAction, Items: []desktop.ItemRef{report.Ref}}
	state := filepath.Join(root, "review-private", "state.json")
	private := read(t, state)
	destination := prepared(t, app, request).Destination.Output
	lanes := []bindingLane{
		{name: "private state", change: func() {
			writeDocument(t, filepath.Dir(state), "state.json", strings.Replace(private, `"schema"`, ` "schema"`, 1))
		},
			undo: func() { writeDocument(t, filepath.Dir(state), "state.json", private) }},
		{name: "destination", refreshed: true, change: func() {
			if err := os.Mkdir(filepath.Join(root, destination), 0o700); err != nil {
				t.Fatal(err)
			}
		}, undo: func() { os.Remove(filepath.Join(root, destination)) }},
		{name: "license", refreshed: true, change: func() {
			if selected := app.SelectOperationPolicy(authorOnlyPolicy(t)); selected.State != desktop.Completed {
				t.Fatal(selected)
			}
		}, undo: func() {
			if selected := app.SelectOperationPolicy(testlicense.New(t)); selected.State != desktop.Completed {
				t.Fatal(selected)
			}
		}},
	}
	before := entries(t, root)
	refusesEveryChangedBinding(t, app, request, desktop.ReviewDecisions{}, lanes, func() bool {
		return slices.ContainsFunc(entries(t, root), func(entry string) bool { return !slices.Contains(before, entry) && entry != destination })
	})
	// The same click again after an export answers the one packet.
	final := prepared(t, app, request)
	exported := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: opened.Context, Token: final.Token, IntentID: "export-once"})
	repeated := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: opened.Context, Token: final.Token, IntentID: "export-once"})
	if exported.Export == nil || !repeated.Replayed || repeated.Export == nil || repeated.Export.Packet != exported.Export.Packet {
		t.Fatalf("a repeated export click: %+v %+v", exported, repeated)
	}
}
