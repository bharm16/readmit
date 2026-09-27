package desktop_test

// A library object is reviewed where it is kept, from real files: a profile's
// history names the exact versions it published and two of them compare
// without any pin moving; Edit JSON shows the exact document a draft saves and
// reads edited text back strictly without losing a clause; and a profile is
// evaluated against a case of the project exactly as `readmit profile
// evaluate` evaluates it.

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/profileeval"
)

func TestAProfileHistoryNamesItsVersionsAndComparesThemWithoutMovingAPin(t *testing.T) {
	app, context, pack := profileProject(t)
	root := context.Project
	profile := localProfile(t)
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack}}, IntentID: "profile-1"})
	edited := profile
	edited.Identity.Version = "2"
	edited.Segments = slices.Clone(edited.Segments)
	edited.Segments[0].Description = "Constrained further in version 2"
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: first.Saved.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: edited, Pack: &pack}}, IntentID: "profile-2"})
	if first.Outcome != desktop.SavedOutcome || second.Outcome != desktop.SavedOutcome {
		t.Fatalf("saves: %+v %+v", first, second)
	}

	history := app.ItemHistory(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProfileItem, ID: first.Saved.ID}})
	if history.State != desktop.Completed || len(history.Revisions) != 2 || history.Revisions[0].Version != "2" || !history.Revisions[0].Current ||
		history.Revisions[1].Version != "1" || history.Revisions[1].Ref.Revision != "1" {
		t.Fatalf("history: %+v", history)
	}

	before := entries(t, root)
	compared := app.CompareProfileVersions(desktop.ProfileVersionsRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProfileItem, ID: first.Saved.ID}, From: "1"})
	if compared.State != desktop.Completed || compared.Comparison == nil || compared.Comparison.From != "1" || compared.Comparison.To != "2" ||
		len(compared.Comparison.Changes) == 0 {
		t.Fatalf("comparison: %+v", compared)
	}
	// Ending at the earlier revision is not a comparison of two versions.
	if same := app.CompareProfileVersions(desktop.ProfileVersionsRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProfileItem, ID: first.Saved.ID, Revision: "1"}, From: "1"}); same.State != desktop.Failed || same.Reason == "" {
		t.Fatalf("a version compared with itself: %+v", same)
	}
	if missing := app.CompareProfileVersions(desktop.ProfileVersionsRequest{Context: context, Ref: *second.Saved, From: "9"}); missing.State != desktop.Failed ||
		!strings.Contains(missing.Reason, "version 9") {
		t.Fatalf("a version the project does not hold: %+v", missing)
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("a comparison wrote %v", now)
	}
}

func TestEditJSONShowsTheSavedDocumentAndReadsItBackWithoutLosingAClause(t *testing.T) {
	app, context, pack := profileProject(t)
	root := context.Project
	before := entries(t, root)

	// A check group holding a check this release does not evaluate is shown
	// with that check where it stood, and reads back with it still held.
	chosen := filepath.Join(t.TempDir(), "imported-checks.json")
	if err := os.WriteFile(chosen, []byte(unsupportedChecks(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.CheckGroupItem, Path: chosen})
	shown := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *imported.Draft})
	if shown.State != desktop.Completed || shown.Schema != "readmit-assertion-set/v1" || !strings.Contains(shown.Document, "field_resembles") {
		t.Fatalf("the check group's document: %+v", shown)
	}
	read := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *imported.Draft, Document: shown.Document})
	if read.State != desktop.Completed || !reflect.DeepEqual(read.Draft.CheckGroup.Set, imported.Draft.CheckGroup.Set) ||
		len(read.Draft.CheckGroup.Unsupported) != 1 || read.Draft.CheckGroup.Unsupported[0].Position != imported.Draft.CheckGroup.Unsupported[0].Position {
		t.Fatalf("the check group read back: %+v", read)
	}
	broken := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *imported.Draft,
		Document: strings.Replace(shown.Document, `"schema"`, `"schemas"`, 1)})
	if broken.State != desktop.Failed || len(broken.Problems) != 1 || broken.Problems[0].Field != "document" ||
		!reflect.DeepEqual(broken.Draft.CheckGroup, imported.Draft.CheckGroup) {
		t.Fatalf("text that does not read: %+v", broken)
	}

	// A profile's document is the profile alone; its pack and origin stay
	// with the draft when edited text replaces it.
	held := desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: localProfile(t), Pack: &pack, Origin: profileOrigin(t)}}
	document := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.ProfileItem, Draft: held})
	if document.State != desktop.Completed || document.Schema != "readmit-local-profile/v1" {
		t.Fatalf("the profile's document: %+v", document)
	}
	changed := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.ProfileItem, Draft: held,
		Document: strings.Replace(document.Document, `"version": "1"`, `"version": "2"`, 1)})
	if changed.State != desktop.Completed || changed.Draft.Profile.Profile.Identity.Version != "2" || changed.Draft.Name != held.Name ||
		!reflect.DeepEqual(changed.Draft.Profile.Origin, held.Profile.Origin) || changed.Draft.Profile.Pack.ID != pack.ID ||
		!reflect.DeepEqual(changed.Draft.Profile.Profile.Segments, held.Profile.Profile.Segments) {
		t.Fatalf("the profile read back: %+v", changed)
	}
	if unknown := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.ProfileItem, Draft: held,
		Document: strings.Replace(document.Document, `"segments"`, `"extra": 1, "segments"`, 1)}); unknown.State != desktop.Failed || unknown.Problems[0].Field != "document" {
		t.Fatalf("a member the contract does not declare: %+v", unknown)
	}

	// A scenario's document is its plan with the typed workflow written in.
	fresh := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}})
	plan := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: *fresh.Draft})
	if plan.State != desktop.Completed || plan.Schema != "readmit-scenario-generator/v1" {
		t.Fatalf("the scenario's document: %+v", plan)
	}
	again := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.ScenarioItem, Draft: *fresh.Draft, Document: plan.Document})
	if again.State != desktop.Completed || again.Draft.Scenario.Plan.Seed != fresh.Draft.Scenario.Plan.Seed || again.Draft.Scenario.Template == nil ||
		!reflect.DeepEqual(again.Draft.Scenario.Template.Steps, fresh.Draft.Scenario.Template.Steps) {
		t.Fatalf("the scenario read back: %+v", again)
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("Edit JSON wrote %v", now)
	}
}

func TestAProfileIsEvaluatedAgainstACaseAsTheCommandLineEvaluatesIt(t *testing.T) {
	app, context, pack := profileProject(t)
	root := context.Project
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: localProfile(t), Pack: &pack}}, IntentID: "profile-1"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	writeCase(t, root, "booking", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
	cases := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	if cases.Page == nil || len(cases.Page.Items) != 1 {
		t.Fatalf("cases: %+v", cases)
	}
	caseRef := cases.Page.Items[0].Ref
	before := entries(t, root)

	// The pinned pack is used when none is chosen, and the report is the one
	// the command line writes for the same bytes.
	evaluated := app.EvaluateProfile(desktop.ProfileEvaluationRequest{Context: context, Profile: *saved.Saved, Case: caseRef})
	if evaluated.State != desktop.Completed || evaluated.Report == nil || evaluated.Report.Verdict == "" || evaluated.Report.CaseIdentity == "" {
		t.Fatalf("evaluation: %+v", evaluated)
	}
	profileBytes := savedProfileDocument(t, app, context, *saved.Saved)
	want, err := profileeval.EvaluateBundle(t.Context(), profileBytes, []byte(fixture(t, "profile-pack.json")), filepath.Join(root, "booking"), profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*evaluated.Report, want) {
		t.Fatalf("the window's report differs from the command line's:\n%+v\n%+v", *evaluated.Report, want)
	}
	chosen := app.EvaluateProfile(desktop.ProfileEvaluationRequest{Context: context, Profile: *saved.Saved, Pack: &pack, Case: caseRef, CompleteCapture: true})
	if chosen.State != desktop.Completed || !chosen.Report.CompleteCapture {
		t.Fatalf("with the pack chosen and capture declared complete: %+v", chosen)
	}
	if refused := app.EvaluateProfile(desktop.ProfileEvaluationRequest{Context: context, Profile: pack, Case: caseRef}); refused.State != desktop.Failed || refused.Reason == "" {
		t.Fatalf("a metadata pack evaluated as a profile: %+v", refused)
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("an evaluation wrote %v", now)
	}
}

// savedProfileDocument is the profile document a saved profile revision holds, as Edit
// JSON shows it: the canonical bytes its Save wrote.
func savedProfileDocument(t *testing.T, app *desktop.App, context desktop.RequestContext, ref desktop.ItemRef) []byte {
	t.Helper()
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
	if opened.State != desktop.Completed {
		t.Fatalf("open: %+v", opened)
	}
	exported := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.ProfileItem, Draft: *opened.Draft})
	if exported.State != desktop.Completed {
		t.Fatalf("document: %+v", exported)
	}
	return []byte(exported.Document)
}
