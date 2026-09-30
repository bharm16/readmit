package desktop_test

// A test is one saved object of a project: its readmit-test/v1 spec and its
// links, validated and published whole through SaveItem, reopened exactly
// through OpenItemDraft, listed with the latest run that executed it, and
// read back version by version with the runs of each. These drive the
// facade the Tests screens call, against the readers the command line uses.

import (
	"encoding/json/v2"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// testsProject is a project holding the incident case and one saved
// environment, and the context, case identity and environment reference
// later requests carry.
func testsProject(t *testing.T) (*desktop.App, desktop.RequestContext, string, desktop.ItemRef) {
	t.Helper()
	app, context := namedProject(t)
	written := writeCase(t, context.Project, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7")))
	environment := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem,
		Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: targetDraft("127.0.0.1:2575")}, IntentID: "environment-1"})
	if environment.Outcome != desktop.SavedOutcome {
		t.Fatalf("environment: %+v", environment)
	}
	return app, context, written.Identity, *environment.Saved
}

// ackTest is a whole ACK-boundary test draft over the incident case, with no
// target: the environment it runs against is named in its links.
func ackTest(t *testing.T, identity, name string) *testauthor.Draft {
	t.Helper()
	draft, err := testauthor.NewDraft("incident", identity)
	if err != nil {
		t.Fatal(err)
	}
	draft.Name, draft.Messages, draft.Boundary = name, []string{"s0001-e000001", "s0001-e000002"}, testrunner.ACKBoundary
	draft.Reset = "Restart the listener before running this."
	draft.Expectations = []testauthor.Expectation{{ID: "accepted", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: ackText("AA")}}
	return &draft
}

func saveTest(t *testing.T, app *desktop.App, request desktop.SaveItemRequest) desktop.ItemRef {
	t.Helper()
	request.Kind = desktop.TestItem
	saved := app.SaveItem(request)
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save %s: %+v", request.IntentID, saved)
	}
	return *saved.Saved
}

func fields(problems []desktop.FieldProblem) []string {
	out := []string{}
	for _, problem := range problems {
		out = append(out, problem.Field)
	}
	return out
}

// A test names its environment by identity, never by a file: a save resolves
// it to the target of that environment's current revision, which the saved
// spec names, and records the environment and the sorted tags in its links.
func TestATestNamesItsEnvironmentByIdentityAndSavesItsTargetMember(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "create-1", Draft: desktop.ItemDraft{
		Test: ackTest(t, identity, "Reschedule keeps one appointment"), TestLinks: &desktop.TestLinks{Environment: environment.ID, Tags: []string{"scheduling", "reschedule"}}}})

	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.Test == nil || opened.Draft.TestLinks == nil || opened.Test == nil {
		t.Fatalf("reopen: %+v", opened)
	}
	targetEntry := opened.Draft.Test.Target
	declared, err := os.ReadFile(filepath.Join(context.Project, targetEntry))
	if err != nil || !strings.Contains(string(declared), "127.0.0.1:2575") || !strings.HasPrefix(targetEntry, "environment-"+environment.ID[:8]) {
		t.Fatalf("the test's target %q is not the environment's saved target: %v", targetEntry, err)
	}
	if links := opened.Draft.TestLinks; links.Environment != environment.ID || !slices.Equal(links.Tags, []string{"reschedule", "scheduling"}) || links.Schema != desktop.TestLinksSchema {
		t.Fatalf("links: %+v", links)
	}
	summary := listed(t, app, context.Project, desktop.TestItem)["Reschedule keeps one appointment"].Summary.Test
	if summary == nil || summary.Boundary != testrunner.ACKBoundary || !slices.Equal(summary.Tags, []string{"reschedule", "scheduling"}) || summary.Entry == "" ||
		summary.SourceCase == nil || summary.LatestRun != nil || summary.LatestResult != "" {
		t.Fatalf("summary: %+v", summary)
	}
	saved, err := testrunner.ReadSpec(filepath.Join(context.Project, summary.Entry))
	if err != nil || saved.Target != targetEntry {
		t.Fatalf("the saved spec names %q: %v", saved.Target, err)
	}

	// Following the environment's reset needs an environment that has one.
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, IntentID: "create-2", Draft: desktop.ItemDraft{
		Test: ackTest(t, identity, "Follows a reset"), TestLinks: &desktop.TestLinks{Environment: environment.ID, Reset: desktop.ResetFromEnvironment}}})
	if refused.Outcome != desktop.InvalidOutcome || !slices.Contains(fields(refused.Problems), "test.reset") {
		t.Fatalf("a reset the environment does not have: %+v", refused)
	}
}

// Validating a whole test draft names every problem at once, each at the
// field of its stage, and an expectation's at its position; it writes nothing.
func TestValidateDraftNamesEveryProblemOfATestAtItsStage(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	before := entries(t, context.Project)
	blank, err := testauthor.NewDraft("incident", identity)
	if err != nil {
		t.Fatal(err)
	}
	validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &blank}})
	if want := []string{"test.name", "test.messages", "test.environment", "test.boundary", "test.reset", "test.expectations"}; validated.State != desktop.Completed ||
		!slices.Equal(fields(validated.Problems), want) || validated.Projection != nil {
		t.Fatalf("a blank test: %v, want %v (%+v)", fields(validated.Problems), want, validated)
	}
	wrong := ackTest(t, identity, "Wrong")
	wrong.Boundary, wrong.Observation = testrunner.LedgerBoundary, ""
	count := 1
	wrong.Expectations = append(wrong.Expectations,
		testauthor.Expectation{ID: "ledger", Operator: testauthor.LedgerCount, Count: &count},
		testauthor.Expectation{ID: "unsent", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000009", Selector: "MSA-1", Field: ackText("AA")})
	validated = app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.TestItem,
		Draft: desktop.ItemDraft{Test: wrong, TestLinks: &desktop.TestLinks{Environment: strings.Repeat("a", 24)}}})
	if want := []string{"test.environment", "test.environment", "test.observation", "test.expectations.2"}; !slices.Equal(fields(validated.Problems), want) {
		t.Fatalf("a wrong test: %v, want %v (%+v)", fields(validated.Problems), want, validated.Problems)
	}
	// A name is 1 to 200 characters however many bytes each takes, and
	// holds no control character.
	for name, valid := range map[string]bool{strings.Repeat("é", 120): true, strings.Repeat("a", 201): false, "Tab\u0085name": false, "   ": false} {
		validated = app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: ackTest(t, identity, name),
			TestLinks: &desktop.TestLinks{Environment: environment.ID}}})
		if slices.Contains(fields(validated.Problems), "test.name") == valid {
			t.Fatalf("the name %q: %+v", name, validated.Problems)
		}
	}
	if now := entries(t, context.Project); !slices.Equal(now, before) {
		t.Fatalf("validating wrote %v", now)
	}
}

// Two editors began from one version: the first publishes the next, and the
// second is told the current version, publishes nothing and keeps its draft.
func TestATestSavedAgainstAStaleBaseKeepsTheCurrentRevision(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	links := &desktop.TestLinks{Environment: environment.ID}
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "create", Draft: desktop.ItemDraft{Test: ackTest(t, identity, "First"), TestLinks: links}})
	edited := saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "1", IntentID: "editor-a",
		Draft: desktop.ItemDraft{Test: ackTest(t, identity, "Edited by A"), TestLinks: links}})
	if edited.Revision != "2" {
		t.Fatalf("the first edit: %+v", edited)
	}
	written := entries(t, context.Project)
	stale := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, Item: ref.ID, BaseRevision: "1", IntentID: "editor-b",
		Draft: desktop.ItemDraft{Test: ackTest(t, identity, "Edited by B"), TestLinks: links}})
	if stale.Outcome != desktop.ConflictOutcome || stale.CurrentRevision != "2" || stale.Saved != nil {
		t.Fatalf("a stale base: %+v", stale)
	}
	if now := entries(t, context.Project); !slices.Equal(now, written) {
		t.Fatalf("a refused save wrote %v", now)
	}
	current := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem, ID: ref.ID}})
	if current.Draft == nil || current.Draft.Test.Name != "Edited by A" || current.Ref.Revision != "2" {
		t.Fatalf("the current version: %+v", current)
	}
}

// Create test clicked twice — one after the other, or both at once — is one
// submission: one test, one version.
func TestARepeatedCreatePublishesOneTestVersion(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	request := desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, IntentID: "create-once",
		Draft: desktop.ItemDraft{Test: ackTest(t, identity, "Once"), TestLinks: &desktop.TestLinks{Environment: environment.ID}}}
	answers := make(chan desktop.SaveItemResult, 3)
	for range 2 {
		go func() { answers <- app.SaveItem(request) }()
	}
	var refs []desktop.ItemRef
	for range 2 {
		switch answer := <-answers; {
		case answer.Outcome == desktop.SavedOutcome:
			refs = append(refs, *answer.Saved)
		case answer.State != desktop.Busy:
			t.Fatalf("a double click: %+v", answer)
		}
	}
	again := app.SaveItem(request)
	if again.Outcome != desktop.SavedOutcome || !again.Replayed {
		t.Fatalf("a repeated click: %+v", again)
	}
	refs = append(refs, *again.Saved)
	tests := listed(t, app, context.Project, desktop.TestItem)
	if len(tests) != 1 || tests["Once"].Ref.Revision != "1" {
		t.Fatalf("a repeated create published %+v", tests)
	}
	for _, ref := range refs {
		if ref != tests["Once"].Ref {
			t.Fatalf("answers named different versions: %v", refs)
		}
	}
}

// The demo saves its own test through the one Save without a license, as
// authoring the sample always did; any other test, and the same test in a
// project that is not the demo, is authoring and is refused without one.
func TestTheDemoSavesItsTestWithoutALicense(t *testing.T) {
	app := desktop.New(&chooser{folder: t.TempDir()}, desktop.ShellDocuments{Folder: t.TempDir()})
	demo := app.OpenDemoProject()
	if demo.State != desktop.Completed {
		t.Fatalf("demo: %+v", demo)
	}
	root := demo.Context.Project
	opened := app.OpenCase(root, guide.CaseName)
	if opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	draft, err := testauthor.NewDraft(guide.CaseName, opened.Case.Identity)
	if err != nil {
		t.Fatal(err)
	}
	count := 1
	draft.Name, draft.Messages, draft.Target = "Rescheduling updates the original appointment", []string{"s0001-e000001", "s0001-e000002"}, guide.TargetName
	draft.Boundary, draft.Observation, draft.Reset = testrunner.LedgerBoundary, "practice-observation.json", "Start a fresh practice receiver."
	draft.Expectations = []testauthor.Expectation{{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: &count}}
	context := demo.Context
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &draft}, IntentID: "sample-1"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("the demo's test was not saved without a license: %+v", saved)
	}
	if progress := guideOf(t, app, root).Guide; !slices.ContainsFunc(progress.Steps, func(step guide.Step) bool { return step.ID == guide.StepTest && step.Done }) {
		t.Fatalf("the guided sample does not read the saved test: %+v", progress)
	}
	other := draft
	other.Target = "other-target.json"
	writeDocument(t, root, "other-target.json", replayTarget("127.0.0.1:2575", "nonproduction"))
	if refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &other}, IntentID: "other-1"}); refused.State != desktop.PermissionDenied {
		t.Fatalf("a test outside the sample was saved without a license: %+v", refused)
	}

	// The same frozen sample in a folder of the person's own is not the demo.
	created := app.CreateSampleWorkspace()
	if created.State != desktop.Completed {
		t.Fatalf("sample: %+v", created)
	}
	elsewhere := created.Workspace.Root
	document, err := project.Encode(project.Document{Schema: project.SchemaV2, Settings: project.Settings{Title: "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, elsewhere, project.DocumentName, string(document))
	if refused := app.SaveItem(desktop.SaveItemRequest{Context: desktop.RequestContext{Project: elsewhere}, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &draft}, IntentID: "sample-2"}); refused.State != desktop.PermissionDenied {
		t.Fatalf("the sample's test was saved without a license outside the demo: %+v", refused)
	}
}

// A new test starts over the case its origin names, sending the selected
// messages it can send in the order the case records them, whatever order
// they were selected in; an acknowledgement is listed but never selected,
// and proposals arrive undecided and outside the draft.
func TestATestDraftStartsFromItsCaseInSourceOrderWithSendableMessagesOnly(t *testing.T) {
	app, context := namedProject(t)
	ack := "MSH|^~\\&|FIXTURE|LAB|READMIT|TEST|20260101120000||ACK|ACK-1|P|2.5.1\rMSA|AA|LISTEN-BOOK\r"
	writeCase(t, context.Project, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(ack)+framed(fixture(t, "listen-s13.hl7")))
	var caseRef desktop.ItemRef
	for _, item := range listed(t, app, context.Project, desktop.CaseItem) {
		if item.Summary.Case != nil && item.Summary.Case.Entry == "incident" {
			caseRef = item.Ref
		}
	}
	proposal := desktop.TestProposal{ID: "code", Source: desktop.ProposalFromFinding,
		Check: testauthor.Expectation{ID: "code", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000003", Selector: "MSA-1", Field: ackText("AA")}}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem},
		From: &desktop.TestOrigin{Case: caseRef, Messages: []string{"s0001-e000003", "s0001-e000002", "s0001-e000001"}, Proposals: []desktop.TestProposal{proposal}}})
	if opened.State != desktop.Completed || !opened.New || opened.Draft == nil || opened.Draft.Test == nil || opened.Test == nil {
		t.Fatalf("a new test: %+v", opened)
	}
	test := opened.Draft.Test
	if !slices.Equal(test.Messages, []string{"s0001-e000001", "s0001-e000003"}) || test.Case.Entry != "incident" || test.Target != "" || len(test.Expectations) != 0 {
		t.Fatalf("the draft: %+v", test)
	}
	if !slices.Equal(fields(opened.Problems), []string{"test.messages"}) {
		t.Fatalf("the acknowledgement that was not selected is not said: %+v", opened.Problems)
	}
	listedMessages := []string{}
	for _, message := range opened.Test.Messages {
		listedMessages = append(listedMessages, message.ID+":"+string(message.Kind)+":"+strings.Repeat("sendable", map[bool]int{true: 1}[message.Sendable]))
	}
	if !slices.Equal(listedMessages, []string{"s0001-e000001:message:sendable", "s0001-e000002:ack:", "s0001-e000003:message:sendable"}) ||
		opened.Test.Messages[0].MessageCode != "SIU" || opened.Test.Case == nil || *opened.Test.Case != caseRef {
		t.Fatalf("the case's messages: %v %+v", listedMessages, opened.Test)
	}
	if len(opened.Test.Proposals) != 1 || !reflect.DeepEqual(opened.Test.Proposals[0], proposal) {
		t.Fatalf("the proposals: %+v", opened.Test.Proposals)
	}
	// With nothing selected, every message a test can send is, in the order
	// the case records them, and nothing is reported dropped.
	all := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem},
		From: &desktop.TestOrigin{Case: caseRef, Messages: []string{}}})
	if all.State != desktop.Completed || all.Draft == nil || all.Draft.Test == nil ||
		!slices.Equal(all.Draft.Test.Messages, []string{"s0001-e000001", "s0001-e000003"}) || len(all.Problems) != 0 {
		t.Fatalf("a new test with nothing selected: %+v", all)
	}
}

// A saved test reopens as exactly the draft that saved it: a count of zero,
// records with every identifier authority in order, an empty ledger, and
// every field state. An earlier version opens as it was, read-only.
func TestASavedTestReopensWithEveryCheckStateAndRecordExactly(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	zero := 0
	records := []observation.Record{
		{RecordID: "r000001", PatientID: observation.Identifier{Value: "P2", Namespace: "HOSP", UniversalID: "1.2.3", UniversalIDType: "ISO"},
			PlacerID: observation.Identifier{Value: "PL2"}, FillerID: observation.Identifier{Value: "F2", Namespace: "SCHED", UniversalID: "2.16.840", UniversalIDType: "ISO"}, AppointmentStart: "202601020900"},
		{RecordID: "r000002", PatientID: observation.Identifier{Value: "P1", Namespace: "HOSP"},
			PlacerID: observation.Identifier{Value: "PL1", Namespace: "ORD"}, FillerID: observation.Identifier{Value: "F1"}, AppointmentStart: "202601020800"},
	}
	empty := []observation.Record{}
	draft := ackTest(t, identity, "Every state")
	draft.Boundary, draft.Observation = testrunner.LedgerBoundary, "appointments.json"
	draft.Expectations = []testauthor.Expectation{
		{ID: "none-yet", Operator: testauthor.LedgerCount, Count: &zero},
		{ID: "exact", Operator: testauthor.LedgerEquals, Records: &records},
		{ID: "no-records", Operator: testauthor.LedgerEquals, Records: &empty},
		{ID: "present", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: ackText("AA")},
		{ID: "empty", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-3", Field: &testrunner.FieldValue{State: hl7.Empty}},
		{ID: "null", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000002", Selector: "ERR-1", Field: &testrunner.FieldValue{State: hl7.Null}},
		{ID: "not-present", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000002", Selector: "ERR-3", Field: &testrunner.FieldValue{State: hl7.Omitted}},
	}
	links := &desktop.TestLinks{Environment: environment.ID}
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "create", Draft: desktop.ItemDraft{Test: draft, TestLinks: links}})
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
	if opened.Draft == nil || opened.Draft.Test == nil || opened.Test == nil || opened.Test.ReadOnly || len(opened.Test.Unsupported) != 0 {
		t.Fatalf("reopen: %+v", opened)
	}
	if !reflect.DeepEqual(opened.Draft.Test.Expectations, draft.Expectations) {
		t.Fatalf("the checks reopened as %+v", opened.Draft.Test.Expectations)
	}
	summary := listed(t, app, context.Project, desktop.TestItem)["Every state"].Summary.Test
	if held, err := os.ReadFile(filepath.Join(context.Project, summary.Entry)); err != nil || string(held) != opened.Test.Document {
		t.Fatalf("the document is not the saved bytes: %v", err)
	}

	renamed := *opened.Draft.Test
	renamed.Name = "Every state, renamed"
	saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "1", IntentID: "rename", Draft: desktop.ItemDraft{Test: &renamed, TestLinks: links}})
	first := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem, ID: ref.ID, Revision: "1"}})
	if first.Draft == nil || first.Draft.Test.Name != "Every state" || !first.Test.ReadOnly || first.Ref.Revision != "1" {
		t.Fatalf("the first version: %+v", first)
	}
}

// A saved spec holding what the editor cannot represent — here reset
// instructions over two lines — opens with that clause named and read-only,
// and a save from the draft that would lose it is refused; its document is
// saved exactly through Edit JSON.
func TestAnUnrepresentableTestOpensReadOnlyAndRefusesALossySave(t *testing.T) {
	app, context, identity, _ := testsProject(t)
	writeDocument(t, context.Project, "lab.json", replayTarget("127.0.0.1:2575", "nonproduction"))
	draft := ackTest(t, identity, "Two-line reset")
	draft.Target = "lab.json"
	data, err := testauthor.Generate(*draft)
	if err != nil {
		t.Fatal(err)
	}
	document := strings.Replace(string(data), "Restart the listener before running this.", `Stop the listener.\nStart it again.`, 1)
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "document", Draft: desktop.ItemDraft{TestDocument: document}})
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
	if opened.Test == nil || !opened.Test.ReadOnly || opened.Test.Document != document || len(opened.Test.Unsupported) != 1 ||
		opened.Test.Unsupported[0].Clause != "test.reset" || opened.Draft.Test.Reset != "" || len(opened.Draft.Test.Expectations) != 1 {
		t.Fatalf("an unrepresentable test: %+v %+v", opened.Test, opened.Draft)
	}
	lossy := *opened.Draft.Test
	lossy.Reset = "Stop the listener. Start it again."
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, Item: ref.ID, BaseRevision: "1", IntentID: "lossy",
		Draft: desktop.ItemDraft{Test: &lossy}})
	if refused.Outcome != desktop.InvalidOutcome || !slices.Contains(fields(refused.Problems), "test") {
		t.Fatalf("a lossy save: %+v", refused)
	}
	edited := strings.Replace(document, "Two-line reset", "Two-line reset, edited", 1)
	saved := saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "1", IntentID: "json", Draft: desktop.ItemDraft{TestDocument: edited}})
	if saved.Revision != "2" {
		t.Fatalf("Edit JSON: %+v", saved)
	}
	if invalid := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{TestDocument: `{"schema":"readmit-test/v1"}`}}); !slices.Equal(fields(invalid.Problems), []string{"test_document"}) {
		t.Fatalf("an invalid document: %+v", invalid)
	}
}

// runsProject is a project holding the retained native-acceptance case and
// both of its retained runs, laid out so the spec those runs retained
// resolves from the project: its case one folder up, its target beside it.
func runsProject(t *testing.T) (*desktop.App, desktop.RequestContext, string) {
	t.Helper()
	app, context := namedProject(t)
	root := context.Project
	for _, name := range []string{"baseline", "post-fix", guide.CaseName} {
		copyEntry(t, filepath.Join(nativeAcceptance, name), filepath.Join(root, name))
	}
	copyEntry(t, filepath.Join(nativeAcceptance, guide.CaseName), filepath.Join(filepath.Dir(root), guide.CaseName))
	target, err := json.Marshal(guide.Target(), json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "target.json", string(target)+"\n")
	spec, err := os.ReadFile(filepath.Join(nativeAcceptance, "post-fix", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	return app, context, string(spec)
}

// The list shows the outcome of the latest run that executed exactly the
// test's current version, and nothing at all for a version no run executed.
func TestTheTestsListShowsTheLatestCompatibleResultOrNone(t *testing.T) {
	app, context, retained := runsProject(t)
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "retained", Draft: desktop.ItemDraft{TestDocument: retained}})
	runs := listed(t, app, context.Project, desktop.RunItem)
	test := listed(t, app, context.Project, desktop.TestItem)["Native acceptance reschedule"].Summary.Test
	if test.LatestResult != testrunner.Pass || test.LatestRun == nil || *test.LatestRun != (desktop.ItemRef{Kind: desktop.RunItem, ID: runs["@post-fix"].Ref.ID}) ||
		test.LatestRunAt == nil || !strings.HasPrefix(*test.LatestRunAt, "2026-09-19T23:43:07") {
		t.Fatalf("the latest run: %+v", test)
	}
	if run := runs["@post-fix"].Summary.Run; run.Boundary != testrunner.LedgerBoundary || run.Outcome != string(testrunner.Pass) {
		t.Fatalf("the run: %+v", run)
	}
	if err := os.RemoveAll(filepath.Join(context.Project, "post-fix")); err != nil {
		t.Fatal(err)
	}
	if test := listed(t, app, context.Project, desktop.TestItem)["Native acceptance reschedule"].Summary.Test; test.LatestResult != testrunner.AssertionFailure {
		t.Fatalf("the failed run: %+v", test)
	}
	changed := strings.Replace(retained, `"count":1`, `"count":2`, 1)
	saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "1", IntentID: "changed", Draft: desktop.ItemDraft{TestDocument: changed}})
	if test := listed(t, app, context.Project, desktop.TestItem)["Native acceptance reschedule"].Summary.Test; test.LatestResult != "" || test.LatestRun != nil || test.LatestRunAt != nil {
		t.Fatalf("a version no run executed: %+v", test)
	}
}

// History lists every version, newest first, with who saved it and what it
// changed, and the runs that executed each version exactly.
func TestTestHistoryListsVersionsAndTheRunsOfEach(t *testing.T) {
	app, context, retained := runsProject(t)
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "retained", Draft: desktop.ItemDraft{TestDocument: retained}})
	changed := strings.Replace(retained, `"count":1`, `"count":2`, 1)
	saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "1", IntentID: "changed", Draft: desktop.ItemDraft{TestDocument: changed}})
	runs := listed(t, app, context.Project, desktop.RunItem)
	history := app.TestHistory(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem, ID: ref.ID}})
	if history.State != desktop.Completed || len(history.Versions) != 2 {
		t.Fatalf("history: %+v", history)
	}
	author := reviewer(t)
	latest, first := history.Versions[0], history.Versions[1]
	if latest.Revision != "2" || !latest.Current || !slices.Equal(latest.Changes, []desktop.TestChange{desktop.ChangeChecks}) || latest.Author != author || latest.PublishedAt == nil ||
		first.Revision != "1" || first.Current || len(first.Changes) != 0 || first.Author != author {
		t.Fatalf("versions: %+v", history.Versions)
	}
	want := []desktop.TestRunRow{
		{Run: desktop.ItemRef{Kind: desktop.RunItem, ID: runs["@post-fix"].Ref.ID}, Revision: "1", Outcome: testrunner.Pass},
		{Run: desktop.ItemRef{Kind: desktop.RunItem, ID: runs["@baseline"].Ref.ID}, Revision: "1", Outcome: testrunner.AssertionFailure},
	}
	for i := range history.Runs {
		history.Runs[i].StartedAt = nil
	}
	if !reflect.DeepEqual(history.Runs, want) {
		t.Fatalf("runs: %+v", history.Runs)
	}
}

func reviewer(t *testing.T) string {
	t.Helper()
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return "Local reviewer"
}

// Every saved object's history lists its revisions newest first, each with
// the person who saved it; an object the project only discovered has none.
func TestItemHistoryListsEveryRevisionWithItsAuthor(t *testing.T) {
	app, context, _, environment := testsProject(t)
	edited := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: environment.ID, BaseRevision: "1", IntentID: "environment-2",
		Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: targetDraft("127.0.0.1:2576")}})
	if edited.Outcome != desktop.SavedOutcome {
		t.Fatalf("edit: %+v", edited)
	}
	history := app.ItemHistory(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.EnvironmentItem, ID: environment.ID}})
	author := reviewer(t)
	if history.State != desktop.Completed || len(history.Revisions) != 2 ||
		history.Revisions[0].Number != 2 || !history.Revisions[0].Current || history.Revisions[0].Ref.Revision != "2" || history.Revisions[0].Author != author ||
		history.Revisions[1].Number != 1 || history.Revisions[1].Current || history.Revisions[1].Author != author || history.Revisions[1].PublishedAt == nil {
		t.Fatalf("history: %+v", history)
	}
	var discovered desktop.ItemRef
	for _, item := range listed(t, app, context.Project, desktop.CaseItem) {
		discovered = item.Ref
	}
	if none := app.ItemHistory(desktop.ItemRequest{Context: context, Ref: discovered}); none.State != desktop.Empty || len(none.Revisions) != 0 {
		t.Fatalf("a discovered object: %+v", none)
	}
}

// Suggest checks names a saved run: its proposals come back undecided and
// the draft is unchanged, and only the proposals a decision accepts reach
// the draft. A run whose own checks failed proposes nothing.
func TestSuggestionsFromANamedRunReachTheDraftOnlyByDecision(t *testing.T) {
	app, context, _ := runsProject(t)
	runs := listed(t, app, context.Project, desktop.RunItem)
	opened := app.OpenCase(context.Project, guide.CaseName)
	if opened.Case == nil {
		t.Fatalf("open: %+v", opened)
	}
	draft, err := testauthor.NewDraft(guide.CaseName, opened.Case.Identity)
	if err != nil {
		t.Fatal(err)
	}
	draft.Name, draft.Messages, draft.Boundary = "Suggested", []string{"s0001-e000001", "s0001-e000002"}, testrunner.LedgerBoundary
	passed := runs["@post-fix"].Ref
	request := desktop.TestRequest{Context: &context, Run: &passed, Draft: draft}
	proposed := app.SuggestExpectations(request)
	if proposed.State != desktop.Completed || proposed.Test == nil || len(proposed.Test.Proposals) != 4 || len(proposed.Test.Draft.Expectations) != 0 {
		t.Fatalf("proposals: %+v", proposed)
	}
	for _, proposal := range proposed.Test.Proposals {
		if proposal.Source != desktop.ProposalFromRun || proposal.Check.ID != proposal.ID || proposal.Reason != "" {
			t.Fatalf("a proposal: %+v", proposal)
		}
	}
	accepted, rejected := proposed.Test.Proposals[0], proposed.Test.Proposals[1]
	request.Review = &testauthor.Review{Identity: proposed.Test.Suggestions.Origin.Identity, Decisions: []testauthor.Decision{
		{Suggestion: accepted.ID, Approved: true}, {Suggestion: rejected.ID, Approved: false}}}
	approved := app.ApproveExpectations(request)
	if approved.Test == nil || len(approved.Test.Draft.Expectations) != 1 || !reflect.DeepEqual(approved.Test.Draft.Expectations[0], accepted.Check) ||
		approved.Test.Approval.NotReviewedCount != 2 || approved.Test.Approval.RejectedCount != 1 {
		t.Fatalf("approval: %+v", approved)
	}
	request.Review = &testauthor.Review{Identity: proposed.Test.Suggestions.Origin.Identity, Decisions: []testauthor.Decision{}}
	if none := app.ApproveExpectations(request); none.Test == nil || len(none.Test.Draft.Expectations) != 0 {
		t.Fatalf("nothing accepted: %+v", none)
	}
	failed := runs["@baseline"].Ref
	request.Run, request.Review = &failed, nil
	if refused := app.SuggestExpectations(request); refused.State != desktop.Failed || refused.Test != nil {
		t.Fatalf("a failed run: %+v", refused)
	}
}

// Import opens a spec a person chose as a new draft of the project: its case
// bound to the project's case by evidence, every check kept, and a target
// the project does not hold a problem at its field. Export writes the saved
// version's exact bytes to a new file, and never replaces one.
func TestAnImportedTestBecomesADraftAndExportWritesItsExactBytes(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	elsewhere := t.TempDir()
	copyEntry(t, filepath.Join(context.Project, "incident"), filepath.Join(elsewhere, "incident"))
	writeDocument(t, elsewhere, "lab.json", replayTarget("127.0.0.1:2575", "nonproduction"))
	draft := ackTest(t, identity, "Imported")
	draft.Target = "lab.json"
	data, err := testauthor.Generate(*draft)
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, elsewhere, "imported.json", string(data))
	chosen := &chooser{files: []string{filepath.Join(elsewhere, "imported.json")}}
	importing := activatedApp(t, chosen, t.TempDir())
	imported := importing.ImportTestDraft(context)
	if imported.State != desktop.Completed || !imported.New || imported.Draft == nil || imported.Draft.Test == nil || imported.Test == nil {
		t.Fatalf("import: %+v", imported)
	}
	if test := imported.Draft.Test; test.Case.Entry != "incident" || test.Case.Identity != identity || test.Target != "" ||
		!reflect.DeepEqual(test.Expectations, draft.Expectations) || imported.Test.Document != string(data) {
		t.Fatalf("the imported draft: %+v", test)
	}
	if !slices.Equal(fields(imported.Problems), []string{"test.environment"}) || !slices.Equal(chosen.titles, []string{"Import test"}) {
		t.Fatalf("import problems: %+v %v", imported.Problems, chosen.titles)
	}

	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "imported", Draft: desktop.ItemDraft{Test: imported.Draft.Test,
		TestLinks: &desktop.TestLinks{Environment: environment.ID}}})
	summary := listed(t, app, context.Project, desktop.TestItem)["Imported"].Summary.Test
	saved, err := os.ReadFile(filepath.Join(context.Project, summary.Entry))
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "exported.json")
	exporting := &chooser{destination: destination}
	exporter := activatedApp(t, exporting, t.TempDir())
	exported := exporter.ExportTestItem(desktop.ItemRequest{Context: context, Ref: ref})
	if exported.State != desktop.Completed || exported.Path == "" || !slices.Equal(exporting.named, []string{"Imported.json"}) {
		t.Fatalf("export: %+v %v", exported, exporting.named)
	}
	if written, err := os.ReadFile(exported.Path); err != nil || string(written) != string(saved) {
		t.Fatalf("the exported bytes are not the saved version's: %v", err)
	}
	if again := exporter.ExportTestItem(desktop.ItemRequest{Context: context, Ref: ref}); again.State != desktop.Failed {
		t.Fatalf("an existing file was replaced: %+v", again)
	}
}
