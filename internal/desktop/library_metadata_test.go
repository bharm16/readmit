package desktop_test

// What the Library keeps beside the documents the command line reads, and
// what it records elsewhere in the project, from real files: an imported
// package's metadata pack becomes the project's own, a scenario keeps the
// local profile it is authored for, a check group keeps the names given its
// checks, and upgrading tests to a reviewed profile version publishes one new
// release of each and moves nothing else.

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/profileversion"
)

func TestAnImportedPackageRecordsItsMetadataPackInTheProject(t *testing.T) {
	// A package exported from a project that holds the pack.
	source, sourceContext, pack := profileProject(t)
	saved := source.SaveItem(desktop.SaveItemRequest{Context: sourceContext, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: localProfile(t), Pack: &pack, Origin: profileOrigin(t)}}, IntentID: "profile-1"})
	packaged := filepath.Join(t.TempDir(), "scheduling-profile.json")
	if exported := source.ExportLibraryItem(desktop.LibraryExportRequest{Context: sourceContext, Ref: *saved.Saved, Destination: packaged}); exported.State != desktop.Completed {
		t.Fatalf("export: %+v", exported)
	}

	// A project without it imports the package: the pack travels in the
	// draft, and the profile's Save records it as the project's own pack.
	app, context := namedProject(t)
	imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: packaged})
	if imported.State != desktop.Completed || imported.Draft.Profile.Pack != nil || imported.Draft.Profile.PackDocument == "" {
		t.Fatalf("import: %+v", imported)
	}
	// A save that does not complete records neither the profile nor its pack.
	desktop.SetSaveFaultForTest(app, func(point string) error {
		if point == "member:pack" {
			return errors.New("interrupted")
		}
		return nil
	})
	if interrupted := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: *imported.Draft, IntentID: "import-0"}); interrupted.Outcome == desktop.SavedOutcome {
		t.Fatalf("an interrupted save: %+v", interrupted)
	}
	desktop.SetSaveFaultForTest(app, nil)
	if none := app.MetadataPacks(context); len(none.Packs) != 0 {
		t.Fatalf("an interrupted save left a pack: %+v", none)
	}
	if profiles := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ProfileItem}); profiles.Page != nil && len(profiles.Page.Items) != 0 {
		t.Fatalf("an interrupted save left a profile: %+v", profiles.Page.Items)
	}
	app.DiscardIncompleteSave(desktop.IncompleteSaveRequest{Context: context, Operation: "import-0"})
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: *imported.Draft, IntentID: "import-1"})
	if first.Outcome != desktop.SavedOutcome || first.Projection.Profile.Pack == nil || first.Projection.Profile.PackDocument != "" {
		t.Fatalf("save: %+v", first)
	}
	if again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: *imported.Draft, IntentID: "import-1"}); !again.Replayed ||
		*again.Projection.Profile.Pack != *first.Projection.Profile.Pack {
		t.Fatalf("a repeated click: %+v", again)
	}
	packs := app.MetadataPacks(context)
	if packs.State != desktop.Completed || len(packs.Packs) != 1 || packs.Packs[0].Pack == nil || packs.Packs[0].Pack.ID != "fixture-siu" ||
		packs.Packs[0].Item.Ref.ID != first.Projection.Profile.Pack.ID {
		t.Fatalf("the project's packs: %+v", packs)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *first.Saved})
	if opened.Draft.Profile.Pack == nil || opened.Draft.Profile.Pack.ID != first.Projection.Profile.Pack.ID {
		t.Fatalf("reopened: %+v", opened.Draft.Profile)
	}
	if resolved := app.ResolveProfileDraft(desktop.DraftRequest{Context: context, Kind: desktop.ProfileItem, Draft: *opened.Draft}); resolved.State != desktop.Completed ||
		resolved.Resolution == nil || !resolved.Resolution.Pinned {
		t.Fatalf("resolution: %+v", resolved)
	}
	if exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *first.Saved, Destination: filepath.Join(t.TempDir(), "again.json")}); exported.State != desktop.Completed {
		t.Fatalf("export from the importing project: %+v", exported)
	}

	// A later version of the profile carries its pack forward.
	next := opened.Draft.Profile.Profile
	next.Identity.Version = "3"
	edited := *opened.Draft.Profile
	edited.Profile = next
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: first.Saved.ID, BaseRevision: first.Saved.Revision,
		Draft: desktop.ItemDraft{Name: opened.Draft.Name, Profile: &edited}, IntentID: "import-2"})
	if second.Outcome != desktop.SavedOutcome {
		t.Fatalf("a later version: %+v", second)
	}
	if packs := app.MetadataPacks(context); len(packs.Packs) != 1 || packs.Packs[0].Pack == nil || packs.Packs[0].Pack.ID != "fixture-siu" || packs.Packs[0].Item.Name != "fixture-siu" {
		t.Fatalf("the pack after a later version: %+v", packs)
	}
	if exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *second.Saved, Destination: filepath.Join(t.TempDir(), "later.json")}); exported.State != desktop.Completed {
		t.Fatalf("export of the later version: %+v", exported)
	}

	// Importing again reuses the pack the project now holds.
	if reused := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: packaged}); reused.Draft.Profile.PackDocument != "" ||
		reused.Draft.Profile.Pack == nil || reused.Draft.Profile.Pack.ID != first.Projection.Profile.Pack.ID {
		t.Fatalf("a second import: %+v", reused.Draft.Profile)
	}
	// A different pack under the same identity is refused, not saved over it.
	different := *imported.Draft.Profile
	different.PackDocument = strings.Replace(different.PackDocument, "readmit fixture authors", "another author", 1)
	validated := source.ValidateDraft(desktop.DraftRequest{Context: sourceContext, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Copy", Profile: &different}})
	if !slices.ContainsFunc(validated.Problems, func(problem desktop.FieldProblem) bool {
		return problem.Field == "profile.pack_document" && strings.Contains(problem.Problem, "different metadata pack")
	}) {
		t.Fatalf("a different pack under the same identity: %+v", validated)
	}
}

func TestAScenarioKeepsTheLocalProfileItIsAuthoredFor(t *testing.T) {
	app, context, pack := profileProject(t)
	profile := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: localProfile(t), Pack: &pack}}, IntentID: "profile-1"})
	if profile.Outcome != desktop.SavedOutcome {
		t.Fatalf("profile: %+v", profile)
	}
	held := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}}).Draft.Scenario
	held.Profile = profile.Saved
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Reschedule", Scenario: held}, IntentID: "scenario-1"})
	if saved.Outcome != desktop.SavedOutcome || saved.Projection.Scenario.Profile == nil || *saved.Projection.Scenario.Profile != *profile.Saved {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.Draft.Scenario.Profile == nil || *opened.Draft.Scenario.Profile != *profile.Saved {
		t.Fatalf("reopened: %+v", opened.Draft.Scenario)
	}
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ScenarioItem})
	if listing.Page == nil || len(listing.Page.Items) != 1 || listing.Page.Items[0].Summary.Scenario.LocalProfile == nil ||
		*listing.Page.Items[0].Summary.Scenario.LocalProfile != *profile.Saved {
		t.Fatalf("the listing: %+v", listing)
	}
	// The plan saved is the generator's document alone.
	destination := filepath.Join(t.TempDir(), "plan.json")
	if exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *saved.Saved, Destination: destination}); exported.State != desktop.Completed ||
		exported.Schema != "readmit-scenario-generator/v1" {
		t.Fatalf("export: %+v", exported)
	}
	// Edit JSON keeps it.
	document := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: *opened.Draft})
	if applied := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.ScenarioItem, Draft: *opened.Draft, Document: document.Document}); applied.Draft.Scenario.Profile == nil ||
		*applied.Draft.Scenario.Profile != *profile.Saved {
		t.Fatalf("Edit JSON: %+v", applied.Draft.Scenario)
	}

	// Another family, an inexact version or another kind is a problem there.
	adt := localProfile(t)
	adt.Identity.ID, adt.Base.Family = "admissions", "ADT"
	other := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Admissions", Profile: &desktop.ProfileDraft{Profile: adt}}, IntentID: "profile-2"})
	if other.Outcome != desktop.SavedOutcome {
		t.Fatalf("an ADT profile: %+v", other)
	}
	for name, ref := range map[string]desktop.ItemRef{
		"another family":   *other.Saved,
		"no exact version": {Kind: desktop.ProfileItem, ID: profile.Saved.ID},
		"another kind":     *saved.Saved,
	} {
		wrong := *held
		wrong.Profile = &ref
		validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Reschedule", Scenario: &wrong}})
		if !slices.ContainsFunc(validated.Problems, func(problem desktop.FieldProblem) bool { return problem.Field == "scenario.profile" }) {
			t.Fatalf("%s: %+v", name, validated)
		}
	}
}

func TestACheckGroupKeepsItsCheckNamesBesideTheSet(t *testing.T) {
	app, context := namedProject(t)
	group := checkGroupDraft(t)
	first, second := group.Set.Assertions[0].ID, group.Set.Assertions[1].ID
	group.Names = map[string]string{first: "  Appointment is accepted  ", second: "Reason is kept"}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: desktop.ItemDraft{Name: "Reschedule checks", CheckGroup: group}, IntentID: "checks-1"})
	if saved.Outcome != desktop.SavedOutcome || saved.Projection.CheckGroup.Names[first] != "Appointment is accepted" {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if !reflect.DeepEqual(opened.Draft.CheckGroup.Names, map[string]string{first: "Appointment is accepted", second: "Reason is kept"}) {
		t.Fatalf("reopened: %+v", opened.Draft.CheckGroup.Names)
	}
	// The set exported is the assertion set alone, which the shared reader
	// reads; the names are not in it.
	destination := filepath.Join(t.TempDir(), "checks.json")
	if exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *saved.Saved, Destination: destination}); exported.State != desktop.Completed {
		t.Fatalf("export: %+v", exported)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assertion.Decode(data); err != nil || strings.Contains(string(data), "Appointment is accepted") {
		t.Fatalf("the exported set: %v", err)
	}
	// Edit JSON keeps the names of the checks still held.
	document := app.LibraryDocument(desktop.DraftRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *opened.Draft})
	if applied := app.ApplyLibraryDocument(desktop.LibraryDocumentRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *opened.Draft, Document: document.Document}); !reflect.DeepEqual(applied.Draft.CheckGroup.Names, opened.Draft.CheckGroup.Names) {
		t.Fatalf("Edit JSON: %+v", applied.Draft.CheckGroup.Names)
	}
	for name, names := range map[string]map[string]string{
		"a check the group does not hold": {"not-a-check": "Name"},
		"an empty name":                   {first: "   "},
		"a name too long":                 {first: strings.Repeat("x", 201)},
	} {
		wrong := *opened.Draft.CheckGroup
		wrong.Names = names
		validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: desktop.ItemDraft{Name: "Reschedule checks", CheckGroup: &wrong}})
		if !slices.ContainsFunc(validated.Problems, func(problem desktop.FieldProblem) bool { return strings.HasPrefix(problem.Field, "check_group.names.") }) {
			t.Fatalf("%s: %+v", name, validated)
		}
	}
}

func TestUpgradingPinsPublishesOneReleaseOfEachTestAndMovesNothingElse(t *testing.T) {
	app, context, pack := profileProject(t)
	root := context.Project
	profile := localProfile(t)
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack}}, IntentID: "profile-1"})
	seal, err := profileversion.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	spec := []byte(fixture(t, "test-reschedule.json"))
	review, err := expectation.Review("booking", spec, []profileversion.Version{seal}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.Approve("booking", spec, []profileversion.Version{seal}, nil, review.Identity, "reviewer", "reviewed synthetic booking")
	if err != nil {
		t.Fatal(err)
	}
	releasePath := filepath.Join(root, "booking-release.json")
	if err := expectation.Save(releasePath, release); err != nil {
		t.Fatal(err)
	}
	released, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatal(err)
	}
	edited := profile
	edited.Identity.Version = "2"
	edited.Segments = slices.Clone(edited.Segments)
	edited.Segments[0].Description = "Constrained further in version 2"
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: first.Saved.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: edited, Pack: &pack}}, IntentID: "profile-2"})
	affected := app.ProfileAffectedTests(desktop.ItemRequest{Context: context, Ref: *second.Saved})
	if len(affected.Tests) != 1 || affected.Tests[0].Impact != "affected" {
		t.Fatalf("affected: %+v", affected)
	}
	reviewed := affected.Tests[0].Ref

	upgraded := app.UpgradeProfilePins(desktop.ProfilePinsRequest{Context: context, Profile: *second.Saved, Tests: []desktop.ItemRef{reviewed}, IntentID: "upgrade-1"})
	if upgraded.State != desktop.Completed || len(upgraded.Upgraded) != 1 || len(upgraded.Refused) != 0 || upgraded.Upgraded[0].ID != reviewed.ID {
		t.Fatalf("upgrade: %+v", upgraded)
	}
	// The test now pins version 2, as a new release whose parent is the one
	// reviewed, with the same expectations; the reviewed file is untouched.
	now := app.ProfileAffectedTests(desktop.ItemRequest{Context: context, Ref: *second.Saved})
	if len(now.Tests) != 1 || now.Tests[0].Impact != "current" || now.Tests[0].Pinned.Version != "2" || now.Tests[0].Ref != upgraded.Upgraded[0] {
		t.Fatalf("after the upgrade: %+v", now)
	}
	if after, err := os.ReadFile(releasePath); err != nil || string(after) != string(released) {
		t.Fatalf("the reviewed release changed: %v", err)
	}
	history := app.ItemHistory(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.TestItem, ID: reviewed.ID}})
	if len(history.Revisions) != 1 || history.Revisions[0].Ref != upgraded.Upgraded[0] {
		t.Fatalf("history: %+v", history)
	}
	// The same submission again moves nothing further.
	if again := app.UpgradeProfilePins(desktop.ProfilePinsRequest{Context: context, Profile: *second.Saved, Tests: []desktop.ItemRef{reviewed}, IntentID: "upgrade-1"}); again.State != desktop.Completed ||
		again.Upgraded[0] != upgraded.Upgraded[0] {
		t.Fatalf("the same submission again: %+v", again)
	}
	// A test changed since its review, or pinning another profile, is refused.
	if stale := app.UpgradeProfilePins(desktop.ProfilePinsRequest{Context: context, Profile: *first.Saved, Tests: []desktop.ItemRef{reviewed}, IntentID: "upgrade-2"}); stale.State != desktop.Failed ||
		len(stale.Refused) != 1 || !strings.Contains(stale.Refused[0].Reason, "changed since it was reviewed") {
		t.Fatalf("a stale review: %+v", stale)
	}
	other := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Other", Profile: &desktop.ProfileDraft{Profile: withID(localProfile(t), "other-siu"), Pack: &pack}}, IntentID: "other-1"})
	if unpinned := app.UpgradeProfilePins(desktop.ProfilePinsRequest{Context: context, Profile: *other.Saved, Tests: []desktop.ItemRef{upgraded.Upgraded[0]}, IntentID: "upgrade-3"}); unpinned.State != desktop.Failed ||
		len(unpinned.Refused) != 1 || !strings.Contains(unpinned.Refused[0].Reason, "pins no version of this profile") {
		t.Fatalf("a test pinning another profile: %+v", unpinned)
	}
}

// A new scenario takes its identity from the name it is saved under, and the
// version its workflow carries is the saved revision the Library lists.
func TestANewScenarioIsNamedByItsSaveAndVersionedByEachRevision(t *testing.T) {
	app, context := namedProject(t)
	held := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}}).Draft.Scenario
	if held.Template == nil || held.Template.Scenario.ID != "" {
		t.Fatalf("a new scenario's workflow: %+v", held.Template)
	}
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Reschedule keeps one", Scenario: held}, IntentID: "scenario-1"})
	if first.Outcome != desktop.SavedOutcome || first.Projection.Scenario.Template.Scenario.ID != "reschedule-keeps-one" || first.Projection.Scenario.Template.Scenario.Version != "1" {
		t.Fatalf("first save: %+v", first.Projection.Scenario.Template)
	}
	other := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}}).Draft.Scenario
	another := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Admission", Scenario: other}, IntentID: "scenario-2"})
	if another.Outcome != desktop.SavedOutcome || another.Projection.Scenario.Template.Scenario.ID != "admission" {
		t.Fatalf("another scenario: %+v", another.Projection.Scenario.Template)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *first.Saved}).Draft
	opened.Scenario.Template.Steps = opened.Scenario.Template.Steps[:1]
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Item: first.Saved.ID, BaseRevision: "1", Draft: *opened, IntentID: "scenario-3"})
	if second.Outcome != desktop.SavedOutcome || second.Projection.Scenario.Template.Scenario.ID != "reschedule-keeps-one" || second.Projection.Scenario.Template.Scenario.Version != "2" {
		t.Fatalf("second save: %+v", second)
	}
	if again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Item: first.Saved.ID, BaseRevision: "1", Draft: *opened, IntentID: "scenario-3"}); !again.Replayed {
		t.Fatalf("a repeated click: %+v", again)
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ScenarioItem})
	versions := map[string]string{}
	for _, item := range listed.Page.Items {
		versions[item.Name] = item.Summary.Scenario.Version
	}
	if versions["Reschedule keeps one"] != "2" || versions["Admission"] != "1" {
		t.Fatalf("the listed versions: %v", versions)
	}
}
