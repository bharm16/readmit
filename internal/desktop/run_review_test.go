package desktop_test

// The reviewed run (#555): a saved test, a suite or the rest of an
// interrupted run is reviewed from the saved objects and sent only by its
// own Send, once; a change to what the review bound refuses the Send; and the
// run it retained is listed and opened as what it executed.

import (
	"bytes"
	gocontext "context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// runProject is a named project holding the incident case, a nonproduction
// environment at the acking peer and a saved acknowledgement test that
// follows it.
func runProject(t *testing.T, code string) (*desktop.App, desktop.RequestContext, *ackingPeer, desktop.ItemRef, desktop.ItemRef) {
	t.Helper()
	peer := newAckingPeer(t, code)
	app, context := namedProject(t)
	written := writeCase(t, context.Project, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7")))
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem,
		Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: targetDraft(peer.address)}, IntentID: "environment-1"})
	test := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "test-1",
		Draft: desktop.ItemDraft{Test: oneMessageTest(t, written.Identity), TestLinks: &desktop.TestLinks{Environment: environment.ID}}})
	return app, context, peer, test, environment
}

// oneMessageTest sends the booking alone: the acking peer answers every
// frame for it.
func oneMessageTest(t *testing.T, identity string) *testauthor.Draft {
	t.Helper()
	draft := ackTest(t, identity, "Reschedule keeps one appointment")
	draft.Messages = []string{"s0001-e000001"}
	return draft
}

func runReview(t *testing.T, app *desktop.App, request desktop.PrepareActionRequest) *desktop.ActionReview {
	t.Helper()
	result := app.PrepareAction(request)
	if result.State != desktop.Completed || result.Review == nil {
		t.Fatalf("prepare %s: %+v", request.Action, result)
	}
	return result.Review
}

// A saved test is reviewed as what it will send, where, and what must be
// done first, and nothing reaches the target until Send. Send without the
// setup marked complete sends nothing and can be made again; Send with it
// sends each message once; the same click again answers the same run.
func TestARunIsReviewedFromTheSavedTestAndSentOnceByItsSend(t *testing.T) {
	app, context, peer, test, environment := runProject(t, "AA")
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	run := review.Run
	if !review.Ready || review.Token == "" || review.Consent != desktop.SendConsent || run == nil || run.Kind != desktop.TestRunKind ||
		run.Name != "Reschedule keeps one appointment" || run.Version != "1" || run.EnvironmentName != "Scheduling QA" || run.Address != peer.address ||
		run.Environment == nil || run.Environment.ID != environment.ID || run.MessageCount == nil || *run.MessageCount != 1 || len(run.Messages) != 1 ||
		run.Messages[0].MessageCode != "SIU" || len(run.Setup) != 1 || run.Setup[0].Instructions != "Restart the listener before running this." ||
		!slices.Equal(review.Requirements, []desktop.ReviewRequirement{desktop.SetupRequirement}) || review.Destination.Output == "" {
		t.Fatalf("the run review: %+v %+v", review, run)
	}
	if peer.deliveries() != 0 {
		t.Fatal("preparing a run review reached the target")
	}

	unmarked := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "click-unmarked"})
	if unmarked.Outcome != desktop.ActionRefused || !strings.Contains(unmarked.Reason, "setup step") || unmarked.Run != nil || peer.deliveries() != 0 {
		t.Fatalf("a Send without its setup marked complete: %+v", unmarked)
	}
	sent := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "click-1",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset"}}})
	if sent.State != desktop.Completed || sent.Outcome != desktop.ActionCompleted || sent.Run == nil || sent.Operation != "click-1" || peer.deliveries() != 1 {
		t.Fatalf("the Send: %+v (%d deliveries)", sent, peer.deliveries())
	}
	again := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "click-1",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset"}}})
	if !again.Replayed || again.Run == nil || again.Run.ID != sent.Run.ID || peer.deliveries() != 1 {
		t.Fatalf("the same click again: %+v (%d deliveries)", again, peer.deliveries())
	}
	if used := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "click-2",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset"}}}); used.Outcome != desktop.ActionRefused || peer.deliveries() != 1 {
		t.Fatalf("a used review: %+v", used)
	}

	var listed *desktop.CatalogItem
	result := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.RunItem})
	for i := range result.Page.Items {
		if result.Page.Items[i].Ref.ID == sent.Run.ID {
			listed = &result.Page.Items[i]
		}
	}
	if listed == nil || listed.Name != "Reschedule keeps one appointment" || listed.Summary.Run == nil {
		t.Fatalf("the run in history: %+v", result.Page)
	}
	if summary := listed.Summary.Run; summary.Kind != desktop.TestRunKind || summary.Test == nil || summary.Test.ID != test.ID || summary.Version != "1" ||
		summary.EnvironmentName != "Scheduling QA" || summary.Result != desktop.RunPassed || summary.Active || summary.StartedAt == nil || summary.CompletedAt == nil {
		t.Fatalf("what the run executed: %+v", summary)
	}

	opened := app.OpenRun(desktop.RunRequest{Context: context, Run: *sent.Run})
	detail := opened.Run
	if opened.State != desktop.Completed || detail == nil || len(detail.Checks) != 1 || detail.Checks[0].Result != desktop.CheckPassed ||
		detail.Checks[0].Check.Operator != testauthor.ACKFieldEquals || !detail.Checks[0].Hidden || detail.Checks[0].Observed == nil ||
		detail.Checks[0].Observed.Field.Text != nil || len(detail.Messages) != 1 {
		t.Fatalf("the run's page: %+v", opened)
	}
	for _, message := range detail.Messages {
		if message.Delivery != desktop.DeliveryAcknowledged || message.ACKCode != "AA" || message.Message == nil {
			t.Fatalf("a message: %+v", message)
		}
	}
	if detail.Report == nil || detail.Report.Test == nil || detail.Report.Test.Revision != "1" || detail.Report.Spec == "" || detail.Report.Run == "" {
		t.Fatalf("what Create report hands on: %+v", detail.Report)
	}
	shown := app.OpenRun(desktop.RunRequest{Context: context, Run: *sent.Run, Reveal: true})
	if check := shown.Run.Checks[0]; check.Hidden || check.Observed.Field.Text == nil || *check.Observed.Field.Text != "AA" {
		t.Fatalf("a check shown on purpose: %+v", check)
	}
	// Changing links without changing the executed spec must not relabel history.
	draft := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: test})
	draft.Draft.TestLinks.Tags = []string{"new-tag"}
	saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: test.ID, BaseRevision: test.Revision, IntentID: "tags-only", Draft: *draft.Draft})
	after := app.OpenRun(desktop.RunRequest{Context: context, Run: *sent.Run})
	if after.Run == nil || after.Run.Item.Summary.Run.Version != "1" || after.Run.Report.Test.Revision != "1" {
		t.Fatalf("an old run changed version after a links-only save: %+v", after)
	}
	origins, err := os.ReadDir(filepath.Join(context.Project, ".readmit", "run-origins"))
	if err != nil || len(origins) != 1 {
		t.Fatalf("the origin record: %v %v", origins, err)
	}
	originPath := filepath.Join(context.Project, ".readmit", "run-origins", origins[0].Name())
	origin, err := os.ReadFile(originPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originPath, bytes.Replace(origin, []byte(`"schema":`), []byte(`"unknown":true,"schema":`), 1), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := app.OpenRun(desktop.RunRequest{Context: context, Run: *sent.Run})
	if invalid.Run == nil || invalid.Run.Report.Test != nil || !strings.Contains(strings.Join(invalid.Run.Details.Gaps, " "), "historical publication") || invalid.Run.Result != desktop.RunPassed {
		t.Fatalf("invalid origin metadata was inferred or changed the historical verdict: %+v", invalid)
	}
}

// What a run review bound — the test, its environment and the admission —
// cannot change under its Send: a changed one is a stale review with a fresh
// one to look at, and nothing is sent.
func TestAChangedTestOrEnvironmentRefusesAStaleRunReview(t *testing.T) {
	app, context, peer, test, environment := runProject(t, "AA")
	confirmed := desktop.ReviewDecisions{Confirmed: []string{"reset"}}

	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{{Kind: desktop.TestItem, ID: test.ID}}})
	saveEnvironment(t, app, context, desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: environment.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: targetDraft(peer.address)}, IntentID: "environment-2"})
	stale := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "click-environment", Decisions: confirmed})
	if stale.Outcome != desktop.ActionStale || stale.Refreshed == nil || stale.Refreshed.Token == review.Token || peer.deliveries() != 0 {
		t.Fatalf("a Send after the environment was saved again: %+v", stale)
	}

	review = stale.Refreshed
	draft := oneMessageTest(t, caseIdentity(t, context.Project, "incident"))
	draft.Reset = "Restart the listener, then clear it."
	saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: test.ID, BaseRevision: "1", IntentID: "test-2",
		Draft: desktop.ItemDraft{Test: draft, TestLinks: &desktop.TestLinks{Environment: environment.ID}}})
	changed := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "click-test", Decisions: confirmed})
	if changed.Outcome != desktop.ActionStale || peer.deliveries() != 0 {
		t.Fatalf("a Send after the test was saved again: %+v", changed)
	}
}

// A production or unclassified environment is refused with the reason and
// the environment to edit; a window without a license is refused with the
// license's own reason. Neither review can be sent.
func TestARunToAProductionEnvironmentOrWithoutALicenseIsRefused(t *testing.T) {
	for _, classification := range []string{"production", "unclassified"} {
		t.Run(classification, func(t *testing.T) {
			app, context, peer, test, environment := runProject(t, "AA")
			target := targetDraft(peer.address)
			target.Classification = replay.Classification(classification)
			saveEnvironment(t, app, context, desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: environment.ID, BaseRevision: "1",
				Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: target}, IntentID: "environment-" + classification})
			review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
			if review.Ready || review.Token != "" || review.Run.Refusal != desktop.EnvironmentRefusal || !strings.Contains(review.Refusal, "Scheduling QA") ||
				review.Run.Environment == nil || review.Run.Environment.ID != environment.ID || peer.deliveries() != 0 {
				t.Fatalf("a run to a %s environment: %+v %+v", classification, review, review.Run)
			}
		})
	}

	_, context, peer, test, _ := runProject(t, "AA")
	unlicensed := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: t.TempDir()})
	review := runReview(t, unlicensed, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	if review.Ready || review.Token != "" || review.Run.Refusal != desktop.LicenseRefusal || review.Refusal == "" || peer.deliveries() != 0 {
		t.Fatalf("a run review without a license: %+v", review)
	}
}

// caseIdentity is the identity a project's case entry verifies as.
func caseIdentity(t *testing.T, root, entry string) string {
	t.Helper()
	app := workspaceApp(t)
	opened := app.OpenCase(root, entry)
	if opened.Case == nil {
		t.Fatalf("open %s: %+v", entry, opened)
	}
	return opened.Case.Identity
}

// A send of chosen messages sends exactly those, and an empty choice is
// refused rather than read as every message.
func TestASendOfMessagesSendsExactlyTheChosenOnesAndNeverAnEmptyChoice(t *testing.T) {
	app, context, peer, _, environment := runProject(t, "AA")
	incident := listed(t, app, context.Project, desktop.CaseItem)["@incident"].Ref
	empty := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.ReplaySendAction, Items: []desktop.ItemRef{incident},
		Destination: &environment, Replay: &desktop.ReplayActionOptions{Messages: []string{}}})
	if empty.State == desktop.Completed || !strings.Contains(empty.Reason, "empty selection") {
		t.Fatalf("an empty choice: %+v", empty)
	}
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.ReplaySendAction, Items: []desktop.ItemRef{incident},
		Destination: &environment, Replay: &desktop.ReplayActionOptions{Messages: []string{"s0001-e000001"}}})
	if review.Run == nil || review.Run.Kind != desktop.SendRunKind || review.Run.MessageCount == nil || *review.Run.MessageCount != 1 ||
		len(review.Run.Messages) != 1 || review.Run.Messages[0].ID != "s0001-e000001" || review.Run.EnvironmentName != "Scheduling QA" {
		t.Fatalf("the send review: %+v", review.Run)
	}
	sent := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "send-1"})
	if sent.State != desktop.Completed || sent.Run == nil || peer.deliveries() != 1 {
		t.Fatalf("the send: %+v (%d deliveries)", sent, peer.deliveries())
	}
	opened := app.OpenRun(desktop.RunRequest{Context: context, Run: *sent.Run})
	if opened.Run == nil || opened.Run.Item.Summary.Run.Kind != desktop.SendRunKind || opened.Run.Item.Summary.Run.Result != desktop.RunAccepted ||
		len(opened.Run.Messages) != 1 || opened.Run.Messages[0].Source != "s0001-e000001" || len(opened.Run.Checks) != 0 {
		t.Fatalf("the send's page: %+v", opened.Run)
	}
}

// entryOfRun is the project entry a run is retained in.
func entryOfRun(t *testing.T, app *desktop.App, context desktop.RequestContext, ref desktop.ItemRef) string {
	t.Helper()
	opened := app.OpenRun(desktop.RunRequest{Context: context, Run: ref})
	if opened.Run == nil {
		t.Fatalf("open run: %+v", opened)
	}
	return filepath.Join(context.Project, opened.Run.Item.Summary.Run.Entry)
}

// A run that stopped before anything was attempted is recovered read-only:
// its page lists every message not attempted and its checks not evaluated,
// and nothing is sent by reading it. Resume remaining is its own review of
// exactly that rest, sent only by its Send into a new run; a run that
// attempted a message offers no resume. A lock left by an ended run is
// cleared on purpose, keeping the evidence; a run that has not recorded its
// end keeps its lock.
func TestAnInterruptedRunIsRecoveredReadOnlyAndResumedOnlyByItsReview(t *testing.T) {
	app, context, peer, test, _ := runProject(t, "AA")
	confirmed := desktop.ReviewDecisions{Confirmed: []string{"reset"}}
	first := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	sent := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: first.Token, IntentID: "first", Decisions: confirmed})
	if sent.Run == nil || peer.deliveries() != 1 {
		t.Fatalf("the first run: %+v", sent)
	}
	inputs, _, err := durablerun.RetainedInputs(entryOfRun(t, app, context, *sent.Run))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.DecodeSpec(inputs.Spec)
	if err != nil {
		t.Fatal(err)
	}
	specEntry := listed(t, app, context.Project, desktop.TestItem)["Reschedule keeps one appointment"].Summary.Test.Entry
	prepared, err := durablerun.PrepareAt(filepath.Join(context.Project, specEntry), spec.Target)
	if err != nil {
		t.Fatal(err)
	}
	stopped, cancel := gocontext.WithCancel(gocontext.Background())
	cancel()
	if _, err := prepared.Start(stopped, filepath.Join(context.Project, "job-900")); err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(context.Project, "job-900", "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	interrupted := listed(t, app, context.Project, desktop.RunItem)["@job-900"]
	if summary := interrupted.Summary.Run; summary.Result != desktop.RunIncomplete || summary.Test == nil || summary.Test.ID != test.ID {
		t.Fatalf("the stopped run in history: %+v", summary)
	}
	opened := app.OpenRun(desktop.RunRequest{Context: context, Run: interrupted.Ref})
	detail := opened.Run
	if detail == nil || detail.Recovery == nil || !detail.Recovery.CanResume || len(detail.Messages) != 1 || detail.Messages[0].Delivery != desktop.DeliveryNotAttempted ||
		len(detail.Checks) != 1 || detail.Checks[0].Result != desktop.CheckNotEvaluated || detail.Checks[0].Unavailable == "" || detail.Checks[0].Observed != nil {
		t.Fatalf("the stopped run's page: %+v", opened)
	}
	resume := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.ResumeRunAction, Items: []desktop.ItemRef{interrupted.Ref}})
	if !resume.Ready || resume.Run.Resume == nil || resume.Run.Resume.Repeated != 1 || resume.Run.Resume.From.ID != interrupted.Ref.ID || len(resume.Run.Messages) != 1 ||
		resume.Run.Version != "1" || len(resume.Run.Setup) != 1 || peer.deliveries() != 1 {
		t.Fatalf("the resume review: %+v %+v", resume, resume.Run)
	}
	resumed := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: resume.Token, IntentID: "resume", Decisions: confirmed})
	if resumed.State != desktop.Completed || resumed.Run == nil || resumed.Run.ID == interrupted.Ref.ID || peer.deliveries() != 2 {
		t.Fatalf("the resumed run: %+v (%d deliveries)", resumed, peer.deliveries())
	}
	if after, err := os.ReadFile(filepath.Join(context.Project, "job-900", "journal.jsonl")); err != nil || string(after) != string(journal) {
		t.Fatalf("resuming changed the stopped run's journal: %v", err)
	}
	refused := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.ResumeRunAction, Items: []desktop.ItemRef{*resumed.Run}})
	if refused.Ready || refused.Token != "" || !strings.Contains(refused.Refusal, "Resume is unavailable") || peer.deliveries() != 2 {
		t.Fatalf("a resume of a run that attempted its message: %+v", refused)
	}

	folder := entryOfRun(t, app, context, *resumed.Run)
	lease := durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()},
		Resources: []durablerun.Resource{{Kind: durablerun.EndpointResource, Name: peer.address}}}
	raw, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, folder, "lease.json", string(raw))
	if locked := app.OpenRun(desktop.RunRequest{Context: context, Run: *resumed.Run}); locked.Run.Recovery == nil || !locked.Run.Recovery.CanClearLock {
		t.Fatalf("an ended run's lock: %+v", locked.Run.Recovery)
	}
	if cleared := app.ClearStaleRunLock(desktop.RunRequest{Context: context, Run: *resumed.Run}); cleared.State != desktop.Completed {
		t.Fatalf("clear the stale lock: %+v", cleared)
	}
	if _, err := os.Lstat(filepath.Join(folder, "lease.json")); !os.IsNotExist(err) {
		t.Fatalf("the lock survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "journal.jsonl")); err != nil {
		t.Fatalf("clearing the lock removed evidence: %v", err)
	}
	torn, err := os.OpenFile(filepath.Join(context.Project, "job-900", "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	torn.WriteString(`{"sequence":99,"kind":"in`)
	torn.Close()
	writeDocument(t, filepath.Join(context.Project, "job-900"), "lease.json", string(raw))
	if live := app.ClearStaleRunLock(desktop.RunRequest{Context: context, Run: interrupted.Ref}); live.State != desktop.Failed || !strings.Contains(live.Reason, "completion was not recorded") {
		t.Fatalf("a lock of a run that has not recorded its end: %+v", live)
	}
	if peer.deliveries() != 2 {
		t.Fatalf("reading and recovering sent something: %d deliveries", peer.deliveries())
	}
}

// A saved suite version is reviewed with its environment, targets and tests
// — each test's dataset, target, state sharing and what it waits for — and
// sent once by Send; its run is listed as that version with each test's
// result.
func TestASuiteVersionIsReviewedWithItsTestsAndSentOnceBySend(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	p := newSuiteProject(t, peer.address)
	draft := p.smokeSuite()
	draft.Tests = draft.Tests[:1]
	draft.Datasets[0].Rows = draft.Datasets[0].Rows[1:]
	draft.Datasets = draft.Datasets[:1]
	draft.Environments = draft.Environments[:1]
	draft.Environments[0].Bindings = draft.Environments[0].Bindings[:1]
	draft.Exclusions = []desktop.SuiteExclusion{}
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", draft)

	review := runReview(t, p.app, desktop.PrepareActionRequest{Context: p.context, Action: desktop.RunSuiteAction, Items: []desktop.ItemRef{ref}})
	run := review.Run
	if !review.Ready || run.Kind != desktop.SuiteRunKind || run.Name != "Scheduling smoke" || run.Version != "1" || run.EnvironmentName != "Scheduling lab" ||
		run.Site != "hospital-a" || run.MessageCount != nil || len(run.Targets) != 1 || run.Targets[0].Address != peer.address || len(run.Jobs) != 1 {
		t.Fatalf("the suite review: %+v %+v", review, run)
	}
	if job := run.Jobs[0]; job.Test != "Booking receives ACK" || job.Dataset != "Scheduling cases" || job.Rows != 1 || job.Target != "Scheduling lab" ||
		job.StateSharing != runqueue.SharedState || len(job.DependsOn) != 0 {
		t.Fatalf("the suite's test: %+v", job)
	}
	if peer.deliveries() != 0 {
		t.Fatal("reviewing the suite reached the target")
	}
	if len(run.Setup) != 1 || run.Setup[0].ID != "reset-booking" || run.Setup[0].Instructions != "Restart the listener before running this." {
		t.Fatalf("the suite's manual setup: %+v", run.Setup)
	}
	if unmarked := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "suite-unmarked"}); unmarked.Outcome != desktop.ActionRefused || peer.deliveries() != 0 {
		t.Fatalf("a suite Send without its setup marked complete: %+v", unmarked)
	}
	sent := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "suite-1",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset-booking"}}})
	if sent.State != desktop.Completed || sent.Run == nil || peer.deliveries() != 1 {
		t.Fatalf("the suite send: %+v (%d deliveries)", sent, peer.deliveries())
	}
	listedRun := listed(t, p.app, p.context.Project, desktop.RunItem)
	var summary *desktop.RunSummary
	for _, item := range listedRun {
		if item.Ref.ID == sent.Run.ID {
			summary = item.Summary.Run
			if item.Name != "Scheduling smoke" {
				t.Fatalf("the suite run's name: %+v", item)
			}
		}
	}
	if summary == nil || summary.Kind != desktop.SuiteRunKind || summary.Result != desktop.RunPassed || summary.Version != "1" || summary.EnvironmentName != "Scheduling lab" {
		t.Fatalf("the suite run in history: %+v", summary)
	}
	opened := p.app.OpenRun(desktop.RunRequest{Context: p.context, Run: *sent.Run})
	if opened.Run == nil || len(opened.Run.Jobs) != 1 || opened.Run.Jobs[0].Result != desktop.RunPassed || opened.Run.Jobs[0].Test != "Booking receives ACK" {
		t.Fatalf("the suite run's page: %+v", opened)
	}
	job := p.app.OpenRun(desktop.RunRequest{Context: p.context, Run: *sent.Run, Job: opened.Run.Jobs[0].ID})
	if job.Run == nil || job.Run.Job != opened.Run.Jobs[0].ID || job.Run.Name != "Booking receives ACK" || job.Run.Result != desktop.RunPassed || len(job.Run.Checks) != 1 {
		t.Fatalf("one job's page: %+v", job)
	}
	p.saveSuite(t, "rename", ref.ID, ref.Revision, "Renamed smoke", draft)
	for _, item := range listed(t, p.app, p.context.Project, desktop.RunItem) {
		if item.Ref.ID == sent.Run.ID && (item.Name != "Scheduling smoke" || item.Summary.Run.Version != "1") {
			t.Fatalf("renaming the suite changed its executed history: %+v", item)
		}
	}
}

// A job the queue skipped because the job it depends on did not pass wrote no
// run of its own; its page still names the test it is of, never its job
// identifier.
func TestASkippedSuiteJobIsNamedByItsTest(t *testing.T) {
	peer := newAckingPeer(t, "AE")
	p := newSuiteProject(t, peer.address)
	draft := p.smokeSuite()
	draft.Datasets[0].Rows = draft.Datasets[0].Rows[1:]
	draft.Environments = draft.Environments[:1]
	draft.Environments[0].Bindings[1].Target = named(p.lab)
	draft.Exclusions = []desktop.SuiteExclusion{}
	draft.Concurrency = 1
	ref := p.saveSuite(t, "create", "", "", "Scheduling smoke", draft)
	review := runReview(t, p.app, desktop.PrepareActionRequest{Context: p.context, Action: desktop.RunSuiteAction, Items: []desktop.ItemRef{ref}})
	confirmed := []string{}
	for _, step := range review.Run.Setup {
		confirmed = append(confirmed, step.ID)
	}
	sent := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "suite-skipped", Decisions: desktop.ReviewDecisions{Confirmed: confirmed}})
	if sent.State != desktop.Completed || sent.Run == nil {
		t.Fatalf("the suite send: %+v", sent)
	}
	opened := p.app.OpenRun(desktop.RunRequest{Context: p.context, Run: *sent.Run})
	if opened.Run == nil || len(opened.Run.Jobs) != 2 {
		t.Fatalf("the suite run's page: %+v", opened)
	}
	if first := opened.Run.Jobs[0]; first.Test != "Booking receives ACK" || first.Result != desktop.RunFailed {
		t.Fatalf("the failed job: %+v", first)
	}
	if skipped := opened.Run.Jobs[1]; skipped.Test != "Reschedule keeps one appointment" || skipped.Admission != runqueue.Skipped || skipped.Reason == "" {
		t.Fatalf("the skipped job: %+v", skipped)
	}
}

func TestASuiteIncludesItsTestsInheritedEnvironmentReset(t *testing.T) {
	peer, backup := newAckingPeer(t, "AA"), newAckingPeer(t, "AA")
	p := newSuiteProject(t, peer.address)
	tests := []desktop.ItemRef{}
	for i, pair := range []struct {
		environment, test desktop.ItemRef
		address           string
	}{{p.lab, p.booking, peer.address}, {p.backup, p.reschedule, backup.address}} {
		environment := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: pair.environment})
		environment.Draft.Environment = targetDraft(pair.address)
		environment.Draft.ResetPlan = &fixturereset.Plan{Actions: []fixturereset.Action{{Operator: fixturereset.OperatorConfirms, Instructions: "Empty the appointment store."}}}
		environment.Draft.Links = &desktop.EnvironmentLinks{ResetName: "Empty store", ActionNames: []string{"Clear appointments"}}
		saveEnvironment(t, p.app, p.context, desktop.SaveItemRequest{Item: pair.environment.ID, BaseRevision: pair.environment.Revision, IntentID: "environment-reset-" + string(rune('a'+i)), Draft: *environment.Draft})
		test := p.app.OpenItemDraft(desktop.ItemRequest{Context: p.context, Ref: pair.test})
		test.Draft.TestLinks.Reset, test.Draft.TestLinks.Environment = desktop.ResetFromEnvironment, pair.environment.ID
		tests = append(tests, saveTest(t, p.app, desktop.SaveItemRequest{Context: p.context, Item: pair.test.ID, BaseRevision: pair.test.Revision, IntentID: "test-reset-" + string(rune('a'+i)), Draft: *test.Draft}))
	}
	draft := p.smokeSuite()
	draft.Tests[0].Test, draft.Tests[1].Test = tests[0], tests[1]
	draft.Datasets[0].Rows = draft.Datasets[0].Rows[1:]
	draft.Environments = draft.Environments[:1]
	draft.Exclusions = []desktop.SuiteExclusion{}
	ref := p.saveSuite(t, "create-reset-suite", "", "", "Scheduling reset", draft)
	review := runReview(t, p.app, desktop.PrepareActionRequest{Context: p.context, Action: desktop.RunSuiteAction, Items: []desktop.ItemRef{ref}})
	if !review.Ready || len(review.Run.Setup) != 2 || review.Run.Setup[0].Instructions != "Empty the appointment store." || peer.deliveries() != 0 || backup.deliveries() != 0 {
		t.Fatalf("the inherited resets: %+v", review)
	}
	if refused := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "unmarked"}); refused.Outcome != desktop.ActionRefused || peer.deliveries() != 0 || backup.deliveries() != 0 {
		t.Fatalf("the resets were not required: %+v", refused)
	}
	sent := p.app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: p.context, Token: review.Token, IntentID: "confirmed", Decisions: desktop.ReviewDecisions{Confirmed: []string{review.Run.Setup[0].ID, review.Run.Setup[1].ID}}})
	if sent.State != desktop.Completed || sent.Run == nil || sent.Reset == nil || sent.Reset.Result.Outcome != fixturereset.Confirmed || peer.deliveries() != 1 || backup.deliveries() != 1 {
		t.Fatalf("both resets were not retained before sending: %+v", sent)
	}
}

// A test that follows its environment's reset lists the reset's manual
// steps as its setup, and its Send runs that reset before any message: the
// reset's outcome is retained beside the run. Its prose is never executed.
func TestARunFollowingItsEnvironmentsResetRunsItFirst(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	app, context := namedProject(t)
	written := writeCase(t, context.Project, "incident", framed(fixture(t, "listen-s12.hl7")))
	plan := &fixturereset.Plan{Actions: []fixturereset.Action{{Operator: fixturereset.OperatorConfirms, Instructions: "Stop the scheduling listener and clear its store."}}}
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, IntentID: "environment",
		Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: targetDraft(peer.address), ResetPlan: plan,
			Links: &desktop.EnvironmentLinks{ResetName: "Empty appointment store", ActionNames: []string{"Clear store"}}}})
	test := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "test",
		Draft: desktop.ItemDraft{Test: oneMessageTest(t, written.Identity), TestLinks: &desktop.TestLinks{Environment: environment.ID, Reset: desktop.ResetFromEnvironment}}})
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	if !review.Ready || len(review.Run.Setup) != 1 || review.Run.Setup[0].Name != "Clear store" ||
		review.Run.Setup[0].Instructions != "Stop the scheduling listener and clear its store." || len(review.Run.Resets) != 0 {
		t.Fatalf("the reset the test follows: %+v", review.Run)
	}
	step := review.Run.Setup[0].ID
	sent := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "send",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{step}}})
	if sent.State != desktop.Completed || sent.Run == nil || sent.Reset == nil || sent.Reset.Result == nil ||
		sent.Reset.Result.Outcome != fixturereset.Confirmed || peer.deliveries() != 1 {
		t.Fatalf("the run and its reset: %+v", sent)
	}
	if _, err := os.Stat(filepath.Join(context.Project, sent.Reset.Output)); err != nil {
		t.Fatalf("the reset's outcome was not retained: %v", err)
	}
	inputs, _, err := durablerun.RetainedInputs(entryOfRun(t, app, context, *sent.Run))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := testrunner.DecodeSpec(inputs.Spec)
	if err != nil {
		t.Fatal(err)
	}
	specEntry := listed(t, app, context.Project, desktop.TestItem)[spec.Name].Summary.Test.Entry
	prepared, err := durablerun.PrepareAt(filepath.Join(context.Project, specEntry), spec.Target)
	if err != nil {
		t.Fatal(err)
	}
	stopped, cancel := gocontext.WithCancel(gocontext.Background())
	cancel()
	if _, err := prepared.Start(stopped, filepath.Join(context.Project, "job-900")); err != nil {
		t.Fatal(err)
	}
	interrupted := listed(t, app, context.Project, desktop.RunItem)["@job-900"]
	resume := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.ResumeRunAction, Items: []desktop.ItemRef{interrupted.Ref}})
	if !resume.Ready || len(resume.Run.Setup) != 1 || resume.Run.Setup[0].Instructions != plan.Actions[0].Instructions {
		t.Fatalf("resume did not require a fresh inherited reset: %+v", resume)
	}
	resumed := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: resume.Token, IntentID: "resume-with-reset",
		Decisions: desktop.ReviewDecisions{Confirmed: []string{resume.Run.Setup[0].ID}}})
	if resumed.State != desktop.Completed || resumed.Reset == nil || resumed.Reset.Result.Outcome != fixturereset.Confirmed || peer.deliveries() != 2 {
		t.Fatalf("resume did not retain its own reset: %+v", resumed)
	}
}

// A run stopped while a message is in flight keeps that delivery uncertain:
// it is listed Incomplete beside Delivery uncertain, its page shows the
// message Uncertain and offers no resume, and nothing — reading it, opening
// it or Stop — sends it again.
func TestAStoppedRunKeepsItsUncertainDeliveryAndNeverResendsIt(t *testing.T) {
	peer := newSilentPeer(t)
	app, context := namedProject(t)
	written := writeCase(t, context.Project, "incident", framed(fixture(t, "listen-s12.hl7")))
	environment := saveEnvironment(t, app, context, desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, IntentID: "environment",
		Draft: desktop.ItemDraft{Name: "Scheduling QA", Environment: targetDraft(peer.address)}})
	test := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "test",
		Draft: desktop.ItemDraft{Test: oneMessageTest(t, written.Identity), TestLinks: &desktop.TestLinks{Environment: environment.ID}}})
	review := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.RunTestAction, Items: []desktop.ItemRef{test}})
	done := make(chan desktop.ReviewedActionResult, 1)
	go func() {
		done <- app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: review.Token, IntentID: "send",
			Decisions: desktop.ReviewDecisions{Confirmed: []string{"reset"}}})
	}()
	select {
	case <-peer.received:
	case <-time.After(20 * time.Second):
		t.Fatal("nothing was sent to the test endpoint")
	}
	// While it sends, the window still reads: the run is listed as running.
	for _, item := range app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.RunItem}).Page.Items {
		if !item.Summary.Run.Active || item.Summary.Run.Result != desktop.RunRunning {
			t.Fatalf("the running run in history: %+v", item.Summary.Run)
		}
	}
	app.CancelOperation("send")
	stopped := <-done
	if stopped.Run == nil || stopped.Outcome != desktop.ActionUncertain {
		t.Fatalf("the stopped run: %+v", stopped)
	}
	summary := listed(t, app, context.Project, desktop.RunItem)
	var run desktop.CatalogItem
	for _, item := range summary {
		run = item
	}
	if run.Summary.Run.Result != desktop.RunIncomplete || !run.Summary.Run.DeliveryUncertain || run.Summary.Run.Active {
		t.Fatalf("the stopped run in history: %+v", run.Summary.Run)
	}
	opened := app.OpenRun(desktop.RunRequest{Context: context, Run: *stopped.Run})
	if opened.Run == nil || len(opened.Run.Messages) != 1 || opened.Run.Messages[0].Delivery != desktop.DeliveryUncertain ||
		opened.Run.Recovery == nil || opened.Run.Recovery.CanResume || opened.Run.Recovery.ResumeRefusal == "" {
		t.Fatalf("the stopped run's page: %+v", opened.Run)
	}
	resume := runReview(t, app, desktop.PrepareActionRequest{Context: context, Action: desktop.ResumeRunAction, Items: []desktop.ItemRef{*stopped.Run}})
	if resume.Ready || resume.Token != "" {
		t.Fatalf("a resume of an uncertain delivery: %+v", resume)
	}
	if connections, frames := peer.counts(); connections != 1 || frames != 1 {
		t.Fatalf("the uncertain message was sent again: %d connections, %d messages", connections, frames)
	}
}
