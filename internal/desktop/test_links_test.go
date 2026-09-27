package desktop_test

// What a saved test links beside its spec and what its runs do with each
// link: the check groups linked by exact version and decided against each
// run's evidence.

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
)

// publishCheckGroup publishes one version of a check group's assertion set,
// as a Library save does, and names that version.
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
