package desktop_test

// What a saved test links beside its spec and what its runs do with each
// link: the named observation whose ledger is fixed into the spec at save,
// the environment a run follows to its current revision, and the check
// groups linked by exact version and decided against each run's evidence.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// emptyLedger is a receiver's readmit-observation/v1 ledger holding nothing.
func emptyLedger(t *testing.T) string {
	t.Helper()
	data, err := observation.Encode(observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: strings.Repeat("0", 32),
		Mode: observation.Fixed, Consistent: true, Processed: []observation.Occurrence{}, Records: []observation.Record{}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// namedObservation saves an observation whose file export reads file.
func namedObservation(t *testing.T, app *desktop.App, context desktop.RequestContext, name, file string) desktop.ItemRef {
	t.Helper()
	draft := observationDraft(t, "appointments")
	draft.Source.File.Path = file
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, IntentID: "observation-" + file,
		Draft: desktop.ItemDraft{Name: name, Observation: draft}})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("observation %s: %+v", name, saved)
	}
	return *saved.Saved
}

// A test of appointment records names the observation it reads; saving it
// fixes the ledger that observation's source reads into the spec, which is
// what a run reads. An observation no run can read, or one named by an
// acknowledgement test, is a problem at the observation. A test saved before
// it recorded its observation reopens naming the one that reads its ledger.
func TestATestReadsTheNamedObservationItLinksFixedAtSave(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	writeDocument(t, context.Project, "appointments.json", emptyLedger(t))
	writeDocument(t, context.Project, "export.csv", "appointment,start\n")
	ledger := namedObservation(t, app, context, "Appointments", "appointments.json")
	export := namedObservation(t, app, context, "Archive export", "export.csv")

	var caseRef desktop.ItemRef
	for _, item := range listed(t, app, context.Project, desktop.CaseItem) {
		caseRef = item.Ref
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem}, From: &desktop.TestOrigin{Case: caseRef, Messages: []string{}}})
	if opened.Test == nil || len(opened.Test.Observations) != 2 {
		t.Fatalf("the observations a test can choose: %+v", opened.Test)
	}
	if shown := opened.Test.Observations; shown[0].Name != "Appointments" || !shown[0].Readable || shown[0].Ref.ID != ledger.ID || shown[0].Ref.Revision != "1" ||
		shown[1].Name != "Archive export" || shown[1].Readable || shown[1].Reason == "" {
		t.Fatalf("the observations a test can choose: %+v", shown)
	}

	count := 0
	draft := ackTest(t, identity, "Reads the ledger")
	draft.Boundary = testrunner.LedgerBoundary
	draft.Expectations = []testauthor.Expectation{{ID: "none", Operator: testauthor.LedgerCount, Count: &count}}
	links := &desktop.TestLinks{Environment: environment.ID, Observation: ledger.ID}
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "ledger", Draft: desktop.ItemDraft{Test: draft, TestLinks: links}})
	summary := listed(t, app, context.Project, desktop.TestItem)["Reads the ledger"].Summary.Test
	if spec, err := testrunner.ReadSpec(filepath.Join(context.Project, summary.Entry)); err != nil || spec.Observation.Path != "appointments.json" {
		t.Fatalf("the saved spec reads %q: %v", spec.Observation.Path, err)
	}
	reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
	if reopened.Draft == nil || reopened.Draft.TestLinks == nil || reopened.Draft.TestLinks.Observation != ledger.ID || reopened.Draft.Test.Observation != "appointments.json" {
		t.Fatalf("reopened: %+v", reopened.Draft)
	}

	unreadable := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.TestItem,
		Draft: desktop.ItemDraft{Test: draft, TestLinks: &desktop.TestLinks{Environment: environment.ID, Observation: export.ID}}})
	if !slices.Equal(fields(unreadable.Problems), []string{"test.observation"}) || !strings.Contains(unreadable.Problems[0].Problem, "receiver ledger") {
		t.Fatalf("an observation no run can read: %+v", unreadable.Problems)
	}
	acknowledged := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.TestItem,
		Draft: desktop.ItemDraft{Test: ackTest(t, identity, "Acknowledged"), TestLinks: links}})
	if !slices.Contains(fields(acknowledged.Problems), "test.observation") {
		t.Fatalf("an acknowledgement test naming an observation: %+v", acknowledged.Problems)
	}

	legacy := *draft
	legacy.Name, legacy.Observation = "Saved before observations were named", "appointments.json"
	old := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "legacy", Draft: desktop.ItemDraft{Test: &legacy,
		TestLinks: &desktop.TestLinks{Environment: environment.ID}}})
	if again := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: old}); again.Draft == nil || again.Draft.TestLinks == nil ||
		again.Draft.TestLinks.Observation != ledger.ID || again.Draft.TestLinks.Environment != environment.ID {
		t.Fatalf("a test saved before it named its observation: %+v", again.Draft)
	}
}

// retainedSpec is the spec one run folder retained.
func retainedSpec(t *testing.T, folder string) testrunner.Spec {
	t.Helper()
	var found []testrunner.Spec
	filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() && entry.Name() == "spec.json" {
			if spec, err := testrunner.ReadSpec(path); err == nil {
				found = append(found, spec)
			}
		}
		return nil
	})
	if len(found) != 1 {
		t.Fatalf("the run retained %d specs", len(found))
	}
	return found[0]
}

// A saved test's Run preflights locally: it names the test version and the
// environment revision it follows, and sends nothing. Saving the environment
// again moves the run to its new target without saving the test, the send
// refuses the older preflight, and the run that executes retains the exact
// target it used; the test's list and history still count it as its run.
func TestASavedTestRunFollowsItsEnvironmentAndSendsNothingBeforeSend(t *testing.T) {
	first, second := newAckingPeer(t, "AA"), newAckingPeer(t, "AA")
	app, context := namedProject(t)
	written := writeCase(t, context.Project, "incident", framed(fixture(t, "listen-s12.hl7")))
	environment := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, IntentID: "environment-1",
		Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: targetDraft(first.address)}})
	if environment.Outcome != desktop.SavedOutcome {
		t.Fatalf("environment: %+v", environment)
	}
	draft := ackTest(t, written.Identity, "Follows its environment")
	draft.Messages = []string{"s0001-e000001"}
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "create", Draft: desktop.ItemDraft{Test: draft,
		TestLinks: &desktop.TestLinks{Environment: environment.Saved.ID}}})
	entry := listed(t, app, context.Project, desktop.TestItem)["Follows its environment"].Summary.Test.Entry
	before := entries(t, context.Project)

	preflight := app.PreflightRun(desktop.RunPreflightRequest{Workspace: context.Project, Spec: entry})
	if preflight.State != desktop.Completed || preflight.Preflight == nil {
		t.Fatalf("preflight: %+v", preflight)
	}
	plan := preflight.Preflight
	if plan.Test == nil || *plan.Test != ref || plan.Environment == nil || *plan.Environment != *environment.Saved || plan.Target.Address != first.address {
		t.Fatalf("the preflight of a saved test: %+v", plan)
	}
	if first.deliveries() != 0 || !slices.Equal(entries(t, context.Project), before) {
		t.Fatal("a preflight sent or wrote something")
	}

	moved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: environment.Saved.ID, BaseRevision: "1", IntentID: "environment-2",
		Draft: desktop.ItemDraft{Name: "Scheduling lab", Environment: targetDraft(second.address)}})
	if moved.Outcome != desktop.SavedOutcome {
		t.Fatalf("the environment's next revision: %+v", moved)
	}
	stale := app.StartDurableRun(desktop.DurableRunRequest{Workspace: context.Project, Spec: entry, Output: plan.Destination.Name, Expected: plan.Identity})
	if stale.State != desktop.Failed || first.deliveries()+second.deliveries() != 0 {
		t.Fatalf("a send the environment moved away from: %+v", stale)
	}
	again := app.PreflightRun(desktop.RunPreflightRequest{Workspace: context.Project, Spec: entry})
	if again.Preflight == nil || *again.Preflight.Environment != *moved.Saved || again.Preflight.Target.Address != second.address || again.Preflight.Identity == plan.Identity {
		t.Fatalf("the preflight after the environment moved: %+v", again)
	}
	executed := app.StartDurableRun(desktop.DurableRunRequest{Workspace: context.Project, Spec: entry, Output: again.Preflight.Destination.Name, Expected: again.Preflight.Identity})
	if executed.State != desktop.Completed || first.deliveries() != 0 || second.deliveries() != 1 {
		t.Fatalf("the run: %+v (deliveries %d, %d)", executed, first.deliveries(), second.deliveries())
	}
	if spec := retainedSpec(t, filepath.Join(context.Project, again.Preflight.Destination.Name)); !strings.HasPrefix(spec.Target, "environment-"+moved.Saved.ID[:8]) ||
		spec.Target == draft.Target || spec.Name != "Follows its environment" {
		t.Fatalf("the run retained the target %q", spec.Target)
	}
	if saved, err := testrunner.ReadSpec(filepath.Join(context.Project, entry)); err != nil || !strings.HasPrefix(saved.Target, "environment-"+environment.Saved.ID[:8]) {
		t.Fatalf("the saved test changed: %+v %v", saved, err)
	}
	if test := listed(t, app, context.Project, desktop.TestItem)["Follows its environment"].Summary.Test; test.LatestRun == nil {
		t.Fatalf("the run of a test that follows its environment is not its latest: %+v", test)
	}
	history := app.TestHistory(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem, ID: ref.ID}})
	if len(history.Versions) != 1 || len(history.Runs) != 1 || history.Runs[0].Revision != "1" {
		t.Fatalf("history: %+v", history)
	}
}

// publishCheckGroup publishes one version of a check group holding set, as
// the Library saves one.
func publishCheckGroup(t *testing.T, root, id, base, intent, set string) desktop.ItemRef {
	t.Helper()
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(set))
	saved, err := store.Save(catalog.Draft{Kind: string(desktop.CheckGroupItem), ItemID: id, Base: base, Name: "Reschedule accepted", Intent: intent, Digest: hex.EncodeToString(digest[:]),
		Members: []catalog.Staged{{Role: "check-group", File: "check-group.json", Data: []byte(set)}}}, nil, catalog.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return desktop.ItemRef{Kind: desktop.CheckGroupItem, ID: saved.Item.ID, Revision: saved.Item.RevisionLabel()}
}

// A test links a check group at the exact version it uses: a version the
// project does not hold, a group only discovered as a file, or a malformed
// link is a problem at that link. Each run of the test version is decided
// against the linked version's set, as `readmit explain` decides it, and a
// later version of the group does not change what the test links.
func TestALinkedCheckGroupIsPinnedToItsVersionAndDecidedAgainstEachRun(t *testing.T) {
	app, context, retained := runsProject(t)
	group := publishCheckGroup(t, context.Project, "", "", "group-1", reviewedSet)
	later := publishCheckGroup(t, context.Project, group.ID, "1", "group-2", strings.Replace(reviewedSet, `"text": "AA"`, `"text": "AE"`, 1))
	if later.Revision != "2" {
		t.Fatalf("the later version: %+v", later)
	}
	writeDocument(t, context.Project, "accepted.json", reviewedSet)
	discovered := listed(t, app, context.Project, desktop.CheckGroupItem)["@accepted.json"].Ref

	for name, linked := range map[string][]desktop.ItemRef{
		"a version the project does not hold": {{Kind: desktop.CheckGroupItem, ID: group.ID, Revision: "3"}},
		"a discovered file":                   {{Kind: desktop.CheckGroupItem, ID: discovered.ID, Revision: "1"}},
		"no version":                          {{Kind: desktop.CheckGroupItem, ID: group.ID}},
		"a group linked twice":                {group, group},
	} {
		validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.TestItem,
			Draft: desktop.ItemDraft{TestDocument: retained, TestLinks: &desktop.TestLinks{Checks: linked}}})
		if !slices.ContainsFunc(validated.Problems, func(problem desktop.FieldProblem) bool { return strings.HasPrefix(problem.Field, "test.checks.") }) {
			t.Fatalf("%s: %+v", name, validated.Problems)
		}
	}

	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "linked", Draft: desktop.ItemDraft{TestDocument: retained,
		TestLinks: &desktop.TestLinks{Checks: []desktop.ItemRef{group}}}})
	runs := listed(t, app, context.Project, desktop.RunItem)
	decided := app.TestRunChecks(desktop.TestRunChecksRequest{Context: context, Test: ref, Run: runs["@post-fix"].Ref})
	if decided.State != desktop.Completed || len(decided.Checks) != 1 {
		t.Fatalf("the linked checks: %+v", decided)
	}
	if check := decided.Checks[0]; check.Group != group || check.Name != "Reschedule accepted" || check.State != desktop.Completed ||
		check.Explanation == nil || check.Explanation.Verdict != "pass" {
		t.Fatalf("the linked group at version 1: %+v", check)
	}
	if opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref}); opened.Draft == nil || !slices.Equal(opened.Draft.TestLinks.Checks, []desktop.ItemRef{group}) {
		t.Fatalf("a later version moved the link: %+v", opened.Draft)
	}

	relinked := saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "1", IntentID: "relinked", Draft: desktop.ItemDraft{TestDocument: retained,
		TestLinks: &desktop.TestLinks{Checks: []desktop.ItemRef{later}}}})
	if decided := app.TestRunChecks(desktop.TestRunChecksRequest{Context: context, Test: relinked, Run: runs["@post-fix"].Ref}); decided.State != desktop.Completed ||
		decided.Checks[0].Explanation == nil || decided.Checks[0].Explanation.Verdict == "pass" {
		t.Fatalf("the linked group at version 2: %+v", decided)
	}
	history := app.TestHistory(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem, ID: ref.ID}})
	if len(history.Versions) != 2 || !slices.Equal(history.Versions[0].Changes, []desktop.TestChange{desktop.ChangeChecks}) {
		t.Fatalf("history: %+v", history.Versions)
	}
	unlinked := saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "2", IntentID: "unlinked", Draft: desktop.ItemDraft{TestDocument: retained}})
	if none := app.TestRunChecks(desktop.TestRunChecksRequest{Context: context, Test: unlinked, Run: runs["@post-fix"].Ref}); none.State != desktop.Empty {
		t.Fatalf("a version linking none: %+v", none)
	}
	changed := strings.Replace(retained, `"count":1`, `"count":2`, 1)
	other := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "other", Draft: desktop.ItemDraft{TestDocument: changed,
		TestLinks: &desktop.TestLinks{Checks: []desktop.ItemRef{group}}}})
	if refused := app.TestRunChecks(desktop.TestRunChecksRequest{Context: context, Test: other, Run: runs["@post-fix"].Ref}); refused.State != desktop.Failed {
		t.Fatalf("a run of another test version: %+v", refused)
	}
}

// A new test can start from a variant the project registered: its messages in
// the order the variant records them, the variant named as the case it sends
// and recorded as its source.
func TestATestDraftStartsFromAVariantAndRecordsItsSource(t *testing.T) {
	app, context := namedProject(t)
	parent := writeCase(t, context.Project, "incident", framed(fixture(t, "listen-s12.hl7")))
	revised := writeCase(t, context.Project, "incident-revised", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7")))
	if err := project.WriteRevisions(context.Project, project.Revisions{Schema: project.RevisionsSchema, Notes: []project.Note{}, Revisions: []project.Revision{{
		Name: "incident-revised", Identity: revised.Identity, Schema: bundle.Schema, Provenance: project.DerivedProvenance,
		Operation: project.Operation{Name: "readmit-transform/v1", Parent: "incident", ParentIdentity: parent.Identity}}}}); err != nil {
		t.Fatal(err)
	}
	variants := listed(t, app, context.Project, desktop.VariantItem)
	variant, held := variants["@incident-revised"]
	if !held {
		for name := range variants {
			variant = variants[name]
		}
	}
	// The window finds the variant a case entry is by the entry it names.
	if variant.Summary.Variant == nil || variant.Summary.Variant.Entry != "incident-revised" {
		t.Fatalf("the variant does not name its case entry: %+v", variant.Summary.Variant)
	}
	source := &desktop.TestSource{Kind: desktop.SourceVariant, Variant: &variant.Ref}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem},
		From: &desktop.TestOrigin{Case: variant.Ref, Messages: []string{}, Source: source}})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.Test == nil || opened.Test == nil {
		t.Fatalf("a test from a variant: %+v", opened)
	}
	if test := opened.Draft.Test; test.Case.Entry != "incident-revised" || test.Case.Identity != revised.Identity ||
		!slices.Equal(test.Messages, []string{"s0001-e000001", "s0001-e000002"}) {
		t.Fatalf("the draft: %+v", test)
	}
	if opened.Test.Case == nil || *opened.Test.Case != variant.Ref || opened.Draft.TestLinks == nil || opened.Draft.TestLinks.Source == nil ||
		opened.Draft.TestLinks.Source.Kind != desktop.SourceVariant || *opened.Draft.TestLinks.Source.Variant != variant.Ref {
		t.Fatalf("the variant is not the case and the source: %+v %+v %+v", *opened.Test.Case, variant.Ref, *opened.Draft.TestLinks.Source)
	}
}

// The test editor's work is retained under its own contract, which carries
// the whole draft and nothing of a run: content naming a preflight, an edit
// that names no test, or any member a test does not carry is refused.
func TestATestEditorDraftIsRetainedWithoutAnySendState(t *testing.T) {
	app := workspaceApp(t)
	workspace := t.TempDir()
	content := func(value any) []byte {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	name := "Reschedule keeps one appointment"
	work := map[string]any{"schema": desktop.TestEditorDraftSchema, "mode": "new", "step": "checks", "draft": map[string]any{"name": name}}
	retained := app.SaveEditorDraft(desktop.EditorDraft{Kind: desktop.TestEditorDraftKind, Workspace: workspace, ContentSchema: desktop.TestEditorDraftSchema, Content: content(work)})
	if retained.State != desktop.Completed || len(retained.Drafts) != 1 {
		t.Fatalf("a test editor draft: %+v", retained)
	}
	for label, refused := range map[string]desktop.EditorDraft{
		"a preflight": {Kind: desktop.TestEditorDraftKind, Workspace: workspace, ContentSchema: desktop.TestEditorDraftSchema,
			Content: content(map[string]any{"schema": desktop.TestEditorDraftSchema, "mode": "new", "step": "review", "draft": map[string]any{}, "preflight": map[string]any{"identity": "x"}})},
		"an edit of nothing": {Kind: desktop.TestEditorDraftKind, Workspace: workspace, ContentSchema: desktop.TestEditorDraftSchema,
			Content: content(map[string]any{"schema": desktop.TestEditorDraftSchema, "mode": "edit", "step": "setup", "draft": map[string]any{}})},
		"an environment": {Kind: desktop.TestEditorDraftKind, Workspace: workspace, ContentSchema: desktop.TestEditorDraftSchema,
			Content: content(map[string]any{"schema": desktop.TestEditorDraftSchema, "mode": "new", "step": "setup", "draft": map[string]any{"environment": targetDraft("127.0.0.1:2575")}})},
		"another kind": {Kind: "note", Workspace: workspace, ContentSchema: desktop.TestEditorDraftSchema, Content: content(work)},
	} {
		if result := app.SaveEditorDraft(refused); result.State == desktop.Completed {
			t.Fatalf("%s was retained: %+v", label, result)
		}
	}
	if held := app.EditorDrafts(); len(held.Drafts) != 1 || !strings.Contains(string(held.Drafts[0].Content), name) {
		t.Fatalf("the retained drafts: %+v", held)
	}
}

// Creating a test and saving an edit of it publishes the test's own files
// and leaves the case it sends exactly as it was.
func TestSavingATestLeavesItsCaseEvidenceUnchanged(t *testing.T) {
	app, context, identity, environment := testsProject(t)
	before := bytesUnder(t, filepath.Join(context.Project, "incident"))
	links := &desktop.TestLinks{Environment: environment.ID}
	ref := saveTest(t, app, desktop.SaveItemRequest{Context: context, IntentID: "create", Draft: desktop.ItemDraft{Test: ackTest(t, identity, "Evidence stays"), TestLinks: links}})
	saveTest(t, app, desktop.SaveItemRequest{Context: context, Item: ref.ID, BaseRevision: "1", IntentID: "edit",
		Draft: desktop.ItemDraft{Test: ackTest(t, identity, "Evidence stays, edited"), TestLinks: links}})
	if after := bytesUnder(t, filepath.Join(context.Project, "incident")); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatal("saving a test changed the case it sends")
	}
}
