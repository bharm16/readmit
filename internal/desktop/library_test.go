package desktop_test

// The library's check groups, profiles and scenarios are saved whole through
// SaveItem and read back through OpenItemDraft, from real files. A check group
// keeps every check and refuses to save one it cannot evaluate; a profile
// version is sealed once and never republished; a scenario's seed and base
// time are allocated once, its preview writes nothing, and a case is
// generated from it once per submission.

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/assertionauthor"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/profileversion"
)

// checkGroupDraft is the fixture assertion set as the editor holds it.
func checkGroupDraft(t *testing.T) *desktop.CheckGroupDraft {
	t.Helper()
	set, err := assertionauthor.ImportDocument([]byte(fixture(t, "assertion-set.json")))
	if err != nil {
		t.Fatal(err)
	}
	return &desktop.CheckGroupDraft{Set: set, Unsupported: []assertionauthor.UnsupportedClause{}}
}

func TestACheckGroupSaveIsOneRevisionAndKeepsEveryClause(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	fresh := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.CheckGroupItem}})
	if fresh.State != desktop.Completed || !fresh.New || fresh.Draft.CheckGroup == nil || len(fresh.Draft.CheckGroup.Set.Assertions) != 0 {
		t.Fatalf("a new check group: %+v", fresh)
	}

	group := checkGroupDraft(t)
	operators := map[assertion.Operator]bool{}
	for _, clause := range group.Set.Assertions {
		operators[clause.Operator] = true
	}
	if len(operators) != len(assertion.Operators()) {
		t.Fatalf("the fixture exercises %d of the %d operators", len(operators), len(assertion.Operators()))
	}
	before := entries(t, root)
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CheckGroupItem,
		Draft: desktop.ItemDraft{Name: "Reschedule checks", CheckGroup: group}, IntentID: "checks-1"})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil || saved.Saved.Revision != "1" {
		t.Fatalf("save: %+v", saved)
	}
	written := slices.DeleteFunc(entries(t, root), func(name string) bool { return slices.Contains(before, name) })
	if len(written) != 1 {
		t.Fatalf("one save wrote %v", written)
	}
	data, err := os.ReadFile(filepath.Join(root, written[0]))
	if err != nil {
		t.Fatal(err)
	}
	set, err := assertion.Decode(data)
	if err != nil || len(set.Assertions) != len(group.Set.Assertions) {
		t.Fatalf("the saved set: %v, %d checks", err, len(set.Assertions))
	}

	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.State != desktop.Completed || opened.Draft.CheckGroup == nil || !reflect.DeepEqual(opened.Draft.CheckGroup.Set, group.Set) ||
		len(opened.Draft.CheckGroup.Unsupported) != 0 {
		t.Fatalf("reopened: %+v", opened)
	}

	// Editing one check and adding one keeps every other check and its
	// identity; the new check is given an identity of its own.
	edited := opened.Draft.CheckGroup.Set
	edited.Assertions = slices.Clone(edited.Assertions)
	text := "AE"
	edited.Assertions[1].Expected.Field.Text = &text
	added := edited.Assertions[0]
	added.ID = ""
	edited.Assertions = append(edited.Assertions, added)
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CheckGroupItem, Item: saved.Saved.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Name: "Reschedule checks", CheckGroup: &desktop.CheckGroupDraft{Set: edited}}, IntentID: "checks-2"})
	if second.Outcome != desktop.SavedOutcome || second.Saved.Revision != "2" {
		t.Fatalf("second save: %+v", second)
	}
	clauses := second.Projection.CheckGroup.Set.Assertions
	if len(clauses) != len(group.Set.Assertions)+1 || clauses[len(clauses)-1].ID == "" || *clauses[1].Expected.Field.Text != "AE" {
		t.Fatalf("the second revision: %+v", clauses)
	}
	for i, clause := range group.Set.Assertions {
		if clauses[i].ID != clause.ID || (i != 1 && !reflect.DeepEqual(clauses[i], clause)) {
			t.Fatalf("check %d changed: %+v, was %+v", i, clauses[i], clause)
		}
	}
	// The earlier revision still reads as it was saved.
	first := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.CheckGroupItem, ID: saved.Saved.ID, Revision: "1"}})
	if first.State != desktop.Completed || first.Ref.Revision != "1" || !reflect.DeepEqual(first.Draft.CheckGroup.Set, group.Set) {
		t.Fatalf("revision 1: %+v", first)
	}
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CheckGroupItem})
	if listing.Page == nil || len(listing.Page.Items) != 1 || listing.Page.Items[0].Summary.CheckGroup == nil ||
		listing.Page.Items[0].Summary.CheckGroup.Revision != "2" || listing.Page.Items[0].Summary.CheckGroup.Assertions != len(clauses) ||
		!slices.Contains(listing.Page.Items[0].Capabilities, desktop.SaveAction) {
		t.Fatalf("the listing: %+v", listing)
	}
}

// unsupportedChecks is the fixture set with one check naming an operator this
// release does not evaluate.
func unsupportedChecks(t *testing.T) string {
	t.Helper()
	return strings.Replace(fixture(t, "assertion-set.json"), `"operator": "field_not_equals"`, `"operator": "field_resembles"`, 1)
}

func TestAnUnsupportedClauseIsShownAndBlocksASave(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	chosen := filepath.Join(t.TempDir(), "imported-checks.json")
	if err := os.WriteFile(chosen, []byte(unsupportedChecks(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	before := entries(t, root)
	imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.CheckGroupItem, Path: chosen})
	if imported.State != desktop.Completed || !imported.New || imported.Draft.CheckGroup == nil {
		t.Fatalf("import: %+v", imported)
	}
	unsupported := imported.Draft.CheckGroup.Unsupported
	if len(unsupported) != 1 || unsupported[0].Operator != "field_resembles" || unsupported[0].ID != "ack-not-rejected" ||
		!strings.Contains(unsupported[0].Raw, "field_resembles") || unsupported[0].Reason == "" {
		t.Fatalf("the unsupported check: %+v", unsupported)
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("an import wrote %v", now)
	}
	refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.CheckGroupItem, Draft: *imported.Draft, IntentID: "import-1"})
	if refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) != 1 || refused.Problems[0].Field != "check_group.unsupported" {
		t.Fatalf("a save holding an unsupported check: %+v", refused)
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("a refused save wrote %v", now)
	}

	// The same set placed in the project is listed with its reason and opens
	// with every check shown; it cannot be saved.
	writeDocument(t, root, "placed-checks.json", unsupportedChecks(t))
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CheckGroupItem})
	if listing.Page == nil || len(listing.Page.Items) != 1 {
		t.Fatalf("listing: %+v", listing)
	}
	listed := listing.Page.Items[0]
	if listed.Availability != desktop.ItemUnsupported || listed.Reason == "" || listed.Summary.CheckGroup == nil || listed.Summary.CheckGroup.Unsupported != 1 ||
		slices.Contains(listed.Capabilities, desktop.SaveAction) {
		t.Fatalf("the placed set: %+v", listed)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: listed.Ref})
	if opened.State != desktop.Completed || len(opened.Draft.CheckGroup.Unsupported) != 1 ||
		len(opened.Draft.CheckGroup.Set.Assertions)+1 != listed.Summary.CheckGroup.Assertions {
		t.Fatalf("the placed set opened: %+v", opened)
	}
}

// localProfile is the fixture local profile.
func localProfile(t *testing.T) localprofile.Profile {
	t.Helper()
	profile, err := localprofile.Decode([]byte(fixture(t, "local-profile.json")))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

// profileProject is a project holding the metadata pack the fixture profile
// pins, and that pack's reference.
func profileProject(t *testing.T) (*desktop.App, desktop.RequestContext, desktop.ItemRef) {
	t.Helper()
	app, context := namedProject(t)
	writeDocument(t, context.Project, "siu-pack.json", fixture(t, "profile-pack.json"))
	packs := app.MetadataPacks(context)
	if packs.State != desktop.Completed || len(packs.Packs) != 1 || packs.Packs[0].Pack == nil || packs.Packs[0].Pack.ID != "fixture-siu" {
		t.Fatalf("metadata packs: %+v", packs)
	}
	matrix := packs.Packs[0].Matrix
	unknown := 0
	for _, row := range matrix {
		if row.Parse == "unknown" {
			unknown++
		}
	}
	if len(matrix) != 28 || unknown == 0 || unknown == len(matrix) {
		t.Fatalf("the support matrix answers every combination, unknown where the pack declares nothing: %d rows, %d unknown", len(matrix), unknown)
	}
	return app, context, packs.Packs[0].Item.Ref
}

// versionedProfileDoc is the owned v3 SIU profile or pack rewritten as the
// schema version named: v2 drops the v3-only members, v4 and v5 keep the v3
// shape the SIU fixture's empty segments and datatypes already satisfy.
func versionedProfileDoc(t *testing.T, name, schema string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "profile-evaluation", "v3", "2.5.1", "SIU", name))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(schema, "/v2") {
		delete(envelope, "datatypes")
		if workflows, ok := envelope["workflows"].([]any); ok {
			for _, workflow := range workflows {
				delete(workflow.(map[string]any), "parent_segments")
			}
		}
	}
	envelope["schema"] = schema
	raw, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A profile or pack this release evaluates is also read where it is kept:
// the catalog names every readable version, the metadata matrix answers for
// every readable pack, and evaluation finds the pack a later profile pins.
func TestLaterProfileAndPackVersionsReadWhereV1Reads(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	profiles := map[string][]byte{
		"profile-v2.json": versionedProfileDoc(t, "profile.json", profileeval.ProfileSchema),
		"profile-v3.json": versionedProfileDoc(t, "profile.json", profileeval.ProfileSchemaV3),
	}
	packs := map[string][]byte{
		"pack-v2.json": versionedProfileDoc(t, "pack.json", profileeval.PackSchema),
		"pack-v3.json": versionedProfileDoc(t, "pack.json", profileeval.PackSchemaV3),
		"pack-v4.json": versionedProfileDoc(t, "pack.json", profileeval.PackSchemaV4),
		"pack-v5.json": versionedProfileDoc(t, "pack.json", profileeval.PackSchemaV5),
	}
	for name, doc := range profiles {
		writeDocument(t, root, name, string(doc))
	}
	for name, doc := range packs {
		writeDocument(t, root, name, string(doc))
	}
	listed := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ProfileItem})
	if listed.Page == nil || len(listed.Page.Items) != len(profiles)+len(packs) {
		t.Fatalf("profiles: %+v", listed)
	}
	for _, item := range listed.Page.Items {
		opened := app.OpenItem(desktop.ItemRequest{Context: context, Ref: item.Ref})
		if opened.State != desktop.Completed || opened.Item.Summary.Profile == nil {
			t.Fatalf("a later version does not open: %+v", opened)
		}
		summary := opened.Item.Summary.Profile
		if opened.Item.Name != "owned-siu-2-5-1" || summary.Family != "SIU" || summary.ProtocolVersion != "2.5.1" || summary.PublishedVersion != "1" {
			t.Fatalf("a later version reads as another: %+v", opened.Item)
		}
	}
	answered := app.MetadataPacks(context)
	if answered.State != desktop.Completed || len(answered.Packs) != len(packs) {
		t.Fatalf("metadata packs: %+v", answered)
	}
	for _, pack := range answered.Packs {
		if pack.Pack == nil || len(pack.Matrix) != 28 {
			t.Fatalf("a later pack has no matrix: %+v", pack)
		}
	}

	evaluator, project := namedProject(t)
	place := func(name string, doc []byte) {
		writeDocument(t, project.Project, name, string(doc))
	}
	place("profile.json", profiles["profile-v3.json"])
	place("pack.json", packs["pack-v3.json"])
	writeCase(t, project.Project, "booking", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
	cases := evaluator.ListCatalog(desktop.CatalogQuery{Context: project, Kind: desktop.CaseItem})
	if cases.Page == nil || len(cases.Page.Items) != 1 {
		t.Fatalf("cases: %+v", cases)
	}
	objects := evaluator.ListCatalog(desktop.CatalogQuery{Context: project, Kind: desktop.ProfileItem})
	profile := desktop.ItemRef{}
	for _, item := range objects.Page.Items {
		if item.Summary.Profile != nil && item.Summary.Profile.Form == "local-profile" {
			profile = item.Ref
		}
	}
	if profile.ID == "" {
		t.Fatalf("the v3 profile is not listed: %+v", objects)
	}
	evaluated := evaluator.EvaluateProfile(desktop.ProfileEvaluationRequest{Context: project, Profile: profile, Case: cases.Page.Items[0].Ref})
	if evaluated.State != desktop.Completed || evaluated.Report == nil {
		t.Fatalf("the v3 profile did not evaluate against its pinned pack: %+v", evaluated)
	}
	want, err := profileeval.EvaluateBundle(t.Context(), profiles["profile-v3.json"], packs["pack-v3.json"], filepath.Join(project.Project, "booking"), profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*evaluated.Report, want) {
		t.Fatalf("the window's report differs from the bundle's:\n%+v\n%+v", *evaluated.Report, want)
	}
}

// profileOrigin is the fixture origin.
func profileOrigin(t *testing.T) *profilepackage.Origin {
	t.Helper()
	origin, err := profilepackage.DecodeOrigin([]byte(fixture(t, "profile-origin.json")))
	if err != nil {
		t.Fatal(err)
	}
	return &origin
}

func TestAProfileSavePublishesANewVersionAndKeepsPinsOriginAndAttribution(t *testing.T) {
	app, context, pack := profileProject(t)
	root := context.Project
	profile := localProfile(t)
	origin := profileOrigin(t)
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack, Origin: origin}}, IntentID: "profile-1"})
	if first.Outcome != desktop.SavedOutcome || first.Saved.Revision != "1" {
		t.Fatalf("first save: %+v", first)
	}
	resolved := app.ResolveProfileDraft(desktop.DraftRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack}}})
	if resolved.State != desktop.Completed || resolved.Resolution == nil || !resolved.Resolution.Pinned || resolved.Seal == nil || len(resolved.Resolution.Segments) == 0 {
		t.Fatalf("resolution: %+v", resolved)
	}

	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *first.Saved})
	if opened.State != desktop.Completed || opened.Draft.Profile == nil || opened.Draft.Profile.Pack == nil || opened.Draft.Profile.Pack.ID != pack.ID ||
		!reflect.DeepEqual(opened.Draft.Profile.Origin, origin) || opened.Draft.Profile.Profile.Base.Pack != profile.Base.Pack {
		t.Fatalf("reopened: %+v", opened)
	}

	// Editing one field's name publishes version 2 and keeps every other
	// clause, the pack pin, the origin and its attribution.
	edited := opened.Draft.Profile.Profile
	edited.Identity.Version = "2"
	edited.Segments = slices.Clone(edited.Segments)
	edited.Segments[0].Fields = slices.Clone(edited.Segments[0].Fields)
	edited.Segments[0].Fields[0].Name = "Placer appointment identifier"
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: first.Saved.ID, BaseRevision: "1",
		Draft:    desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: edited, Pack: opened.Draft.Profile.Pack, Origin: opened.Draft.Profile.Origin}},
		IntentID: "profile-2"})
	if second.Outcome != desktop.SavedOutcome || second.Saved.Revision != "2" {
		t.Fatalf("second save: %+v", second)
	}
	now := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *second.Saved})
	was := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ProfileItem, ID: first.Saved.ID, Revision: "1"}})
	if now.Draft.Profile.Profile.Identity.Version != "2" || was.Draft.Profile.Profile.Identity.Version != "1" {
		t.Fatalf("versions: now %+v, was %+v", now.Draft.Profile.Profile.Identity, was.Draft.Profile.Profile.Identity)
	}
	after, before := now.Draft.Profile.Profile, was.Draft.Profile.Profile
	if after.Segments[0].Fields[0].Name != "Placer appointment identifier" || !reflect.DeepEqual(after.Segments[1:], before.Segments[1:]) ||
		!reflect.DeepEqual(after.Segments[0].Fields[1:], before.Segments[0].Fields[1:]) || !reflect.DeepEqual(after.Terminology, before.Terminology) ||
		!reflect.DeepEqual(after.Authorities, before.Authorities) || !reflect.DeepEqual(after.Dates, before.Dates) || after.Base != before.Base ||
		!reflect.DeepEqual(now.Draft.Profile.Origin, origin) {
		t.Fatalf("an untouched clause changed:\nnow %+v\nwas %+v", after, before)
	}
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ProfileItem, Name: "Scheduling"})
	if listing.Page == nil || len(listing.Page.Items) != 1 || listing.Page.Items[0].Summary.Profile.PublishedVersion != "2" {
		t.Fatalf("the profile listing: %+v", listing)
	}

	// Export writes a package with the seal, the pinned pack and the origin,
	// to the destination the save dialog names; import reads it back into a
	// draft without writing.
	destination := filepath.Join(t.TempDir(), "scheduling-profile.json")
	exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *second.Saved, Destination: destination})
	if exported.State != desktop.Completed || exported.Schema != profilepackage.Schema {
		t.Fatalf("export: %+v", exported)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profilepackage.Decode(data); err != nil {
		t.Fatalf("the exported package: %v", err)
	}
	entriesBefore := entries(t, root)
	imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: destination})
	if imported.State != desktop.Completed || !reflect.DeepEqual(imported.Draft.Profile.Origin, origin) || imported.Draft.Profile.Profile.Identity.Version != "2" ||
		imported.Draft.Profile.Pack == nil || imported.Draft.Profile.Pack.ID != pack.ID {
		t.Fatalf("import: %+v", imported)
	}
	if now := entries(t, root); !slices.Equal(now, entriesBefore) {
		t.Fatalf("an import wrote %v", now)
	}

	// A profile that records no origin is not exported as a package.
	bare := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Unattributed", Profile: &desktop.ProfileDraft{Profile: withID(localProfile(t), "unattributed-siu"), Pack: &pack}}, IntentID: "bare-1"})
	if bare.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", bare)
	}
	refused := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *bare.Saved, Destination: filepath.Join(t.TempDir(), "bare.json")})
	if refused.State != desktop.Failed || !strings.Contains(refused.Reason, "origin") {
		t.Fatalf("an export without an origin: %+v", refused)
	}
}

func withID(profile localprofile.Profile, id string) localprofile.Profile {
	profile.Identity.ID = id
	return profile
}

func TestAffectedTestsAreThePinnedReleases(t *testing.T) {
	app, context, pack := profileProject(t)
	root := context.Project
	profile := localProfile(t)
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack}}, IntentID: "profile-1"})
	if first.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", first)
	}
	// A released test pins version 1 exactly.
	seal, err := profileversion.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	spec := []byte(fixture(t, "test-reschedule.json"))
	pins := []profileversion.Version{seal}
	review, err := expectation.Review("booking", spec, pins, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.Approve("booking", spec, pins, nil, review.Identity, "reviewer", "reviewed synthetic booking")
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

	current := app.ProfileAffectedTests(desktop.ItemRequest{Context: context, Ref: *first.Saved})
	if current.State != desktop.Completed || len(current.Tests) != 1 || current.Tests[0].Impact != "current" || current.Tests[0].Pinned != seal.Pin() {
		t.Fatalf("at version 1: %+v", current)
	}

	edited := profile
	edited.Identity.Version = "2"
	edited.Segments = slices.Clone(edited.Segments)
	edited.Segments[0].Description = "Constrained further in version 2"
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: first.Saved.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: edited, Pack: &pack}}, IntentID: "profile-2"})
	if second.Outcome != desktop.SavedOutcome {
		t.Fatalf("second save: %+v", second)
	}
	affected := app.ProfileAffectedTests(desktop.ItemRequest{Context: context, Ref: *second.Saved})
	if affected.State != desktop.Completed || len(affected.Tests) != 1 || affected.Tests[0].Impact != "affected" ||
		affected.Tests[0].Pinned.Version != "1" || affected.Profile == nil || affected.Profile.Version != "2" || affected.Tests[0].Ref.Kind != desktop.TestItem {
		t.Fatalf("at version 2: %+v", affected)
	}
	// Reading what a change reaches moves no pin.
	if after, err := os.ReadFile(releasePath); err != nil || string(after) != string(released) {
		t.Fatalf("the release changed: %v", err)
	}
	// A profile no test pins reaches none.
	other := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Other", Profile: &desktop.ProfileDraft{Profile: withID(localProfile(t), "other-siu"), Pack: &pack}}, IntentID: "other-1"})
	if none := app.ProfileAffectedTests(desktop.ItemRequest{Context: context, Ref: *other.Saved}); none.State != desktop.Empty || len(none.Tests) != 0 {
		t.Fatalf("an unpinned profile: %+v", none)
	}
}

// fixedClock is a clock that reads one instant with a fraction of a second.
func fixedClock() time.Time {
	return time.Date(2026, 9, 26, 14, 30, 15, 987654321, time.FixedZone("", -5*3600))
}

func TestANewScenarioDraftAllocatesSeedAndBaseTimeOnce(t *testing.T) {
	app, context := namedProject(t)
	desktop.SetClockForTest(app, fixedClock)
	draws := 0
	desktop.SetSeedsForTest(app, func() uint64 { draws++; return uint64(40 + draws) })

	fresh := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}})
	if fresh.State != desktop.Completed || fresh.Draft.Scenario == nil || fresh.Draft.Scenario.Template == nil {
		t.Fatalf("a new scenario: %+v", fresh)
	}
	held := fresh.Draft.Scenario
	want := time.Date(2026, 9, 26, 19, 30, 15, 0, time.UTC)
	if draws != 1 || held.Plan.Seed != 41 || !held.Template.BaseTime.Equal(want) || held.Template.BaseTime.Nanosecond() != 0 {
		t.Fatalf("seed %d after %d draws, base time %s", held.Plan.Seed, draws, held.Template.BaseTime)
	}

	// Previewing and saving draw nothing; the saved plan keeps the seed and
	// base time it was created with, and reopening it rerolls nothing.
	preview := app.PreviewScenarioDraft(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Reschedule", Scenario: held}})
	if preview.State != desktop.Completed || preview.Seed != 41 || preview.BaseTime != "2026-09-26T19:30:15Z" {
		t.Fatalf("preview: %+v", preview)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem,
		Draft: desktop.ItemDraft{Name: "Reschedule", Scenario: held}, IntentID: "scenario-1"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
	if opened.State != desktop.Completed || opened.Draft.Scenario.Plan.Seed != 41 || !opened.Draft.Scenario.Template.BaseTime.Equal(want) || draws != 1 {
		t.Fatalf("reopened after %d draws: %+v", draws, opened.Draft.Scenario)
	}
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ScenarioItem})
	summary := listing.Page.Items[0].Summary.Scenario
	if summary == nil || !summary.Plan || summary.Seed == nil || *summary.Seed != 41 || summary.BaseTime == nil || *summary.BaseTime != "2026-09-26T19:30:15Z" ||
		summary.Family != "SIU" || summary.GeneratorVersion == "" {
		t.Fatalf("the scenario listing: %+v", listing.Page.Items[0])
	}
	// A second new scenario is a second draw.
	if another := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}}); another.Draft.Scenario.Plan.Seed != 42 || draws != 2 {
		t.Fatalf("another new scenario: %+v", another.Draft.Scenario.Plan)
	}
	// A seed the window cannot carry exactly is refused.
	large := *held
	large.Plan.Seed = 1 << 60
	if refused := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Scenario: &large}}); len(refused.Problems) != 1 ||
		refused.Problems[0].Field != "scenario.plan.seed" {
		t.Fatalf("a large seed: %+v", refused)
	}
}

func TestScenarioPreviewIsDeterministicAndWritesNothing(t *testing.T) {
	app, context := namedProject(t)
	desktop.SetClockForTest(app, fixedClock)
	desktop.SetSeedsForTest(app, func() uint64 { return 7 })
	root := context.Project
	held := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}}).Draft.Scenario
	before := entries(t, root)
	request := desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Reschedule", Scenario: held}}
	first := app.PreviewScenarioDraft(request)
	second := app.PreviewScenarioDraft(request)
	if first.State != desktop.Completed || first.PreviewID == "" || first.Origin != "synthetic" || !reflect.DeepEqual(first, second) {
		t.Fatalf("two previews of one draft:\n%+v\n%+v", first, second)
	}
	if len(first.Messages) != 2 || first.Messages[0].MessageCode != "SIU" || first.Messages[0].TriggerEvent != "S12" || first.Messages[0].At != "2026-09-26T19:30:15Z" ||
		first.Messages[1].TriggerEvent != "S13" || first.Messages[1].At != "2026-09-26T19:40:15Z" || first.Messages[1].Origin != "synthetic" {
		t.Fatalf("the generated messages: %+v", first.Messages)
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("a preview wrote %v", now)
	}
	if progress := app.CaptureProgress(); progress.State != desktop.Empty {
		t.Fatalf("a preview started a receiver: %+v", progress)
	}

	// The reader reads a previewed message with no file behind it; values
	// stay hidden until revealed.
	hidden := app.InspectScenarioPreview(desktop.ScenarioPreviewInspectRequest{PreviewID: first.PreviewID, Message: 1, ByteOffset: -1})
	if hidden.State != desktop.Completed || hidden.Inspection.MessageCode != "SIU" || hidden.Inspection.TriggerEvent != "S13" || hidden.Inspection.Raw != "" {
		t.Fatalf("inspect: %+v", hidden)
	}
	shown := app.InspectScenarioPreview(desktop.ScenarioPreviewInspectRequest{PreviewID: first.PreviewID, Message: 0, Path: "MSH[1]-9", ByteOffset: -1, Reveal: true})
	if shown.State != desktop.Completed || !strings.Contains(shown.Inspection.Raw, "SIU^S12") {
		t.Fatalf("revealed: %+v", shown)
	}

	// Another seed is another preview; the first is unchanged by it.
	reseeded := *held
	reseeded.Plan.Seed = 8
	other := app.PreviewScenarioDraft(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Reschedule", Scenario: &reseeded}})
	if other.State != desktop.Completed || other.PreviewID == first.PreviewID {
		t.Fatalf("a reseeded preview: %+v", other)
	}
	if again := app.PreviewScenarioDraft(request); again.PreviewID != first.PreviewID || !reflect.DeepEqual(again.Messages, first.Messages) {
		t.Fatalf("the first preview changed: %+v", again)
	}
	if missing := app.InspectScenarioPreview(desktop.ScenarioPreviewInspectRequest{PreviewID: "not-held", ByteOffset: -1}); missing.State != desktop.Failed {
		t.Fatalf("an unheld preview: %+v", missing)
	}
}

func TestCreateScenarioCaseGeneratesOnceAndRegistersASyntheticCase(t *testing.T) {
	app, context := namedProject(t)
	desktop.SetClockForTest(app, fixedClock)
	desktop.SetSeedsForTest(app, func() uint64 { return 7 })
	root := context.Project
	held := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}}).Draft.Scenario
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem,
		Draft: desktop.ItemDraft{Name: "Reschedule", Scenario: held}, IntentID: "scenario-1"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	before := entries(t, root)
	created := app.CreateScenarioCase(desktop.ScenarioCaseRequest{Context: context, Scenario: *saved.Saved, IntentID: "case-1"})
	if created.State != desktop.Completed || created.Case == nil || created.Provenance != "synthetic" || created.Replayed || created.Seed != 7 ||
		created.BaseTime != "2026-09-26T19:30:15Z" || created.Streams != 1 || created.Identity == "" {
		t.Fatalf("create: %+v", created)
	}
	written := entries(t, root)
	again := app.CreateScenarioCase(desktop.ScenarioCaseRequest{Context: context, Scenario: *saved.Saved, IntentID: "case-1"})
	if again.State != desktop.Completed || !again.Replayed || *again.Case != *created.Case || again.Identity != created.Identity {
		t.Fatalf("the same submission again: %+v", again)
	}
	if now := entries(t, root); !slices.Equal(now, written) {
		t.Fatalf("the same submission wrote again: %v, was %v", now, written)
	}
	if len(written) <= len(before) {
		t.Fatalf("nothing was generated: %v", written)
	}

	cases := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	var generated *desktop.CatalogItem
	for i, item := range cases.Page.Items {
		if item.Ref.ID == created.Case.ID {
			generated = &cases.Page.Items[i]
		}
	}
	if generated == nil || generated.Availability != desktop.ItemAvailable || generated.Summary.Case == nil || !generated.Summary.Case.Registered ||
		generated.Summary.Case.Provenance != "synthetic" || generated.Summary.Case.Entry != created.Entry || generated.Name != "Reschedule" {
		t.Fatalf("the generated case: %+v", generated)
	}
	// The case names the scenario revision it was generated from.
	if from := generated.Summary.Case.Scenario; from == nil || from.Ref != *saved.Saved || from.Name != "Reschedule" {
		t.Fatalf("the generated case's scenario: %+v", from)
	}

	// Another submission of the same plan is the case it already generated.
	another := app.CreateScenarioCase(desktop.ScenarioCaseRequest{Context: context, Scenario: *saved.Saved, IntentID: "case-2"})
	if another.State != desktop.Completed || *another.Case != *created.Case || !another.Replayed {
		t.Fatalf("another submission: %+v", another)
	}
	if now := entries(t, root); !slices.Equal(now, written) {
		t.Fatalf("another submission wrote %v", now)
	}
	if refused := app.CreateScenarioCase(desktop.ScenarioCaseRequest{Context: context, Scenario: *saved.Saved}); refused.State != desktop.Failed {
		t.Fatalf("a submission with no identity: %+v", refused)
	}
}

func TestTheSampleFixtureRefusesANonLoopbackAddress(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	before := entries(t, root)
	for _, request := range []desktop.SampleFixtureRequest{
		{Context: context, Mode: "fixed", Address: "0.0.0.0:0"},
		{Context: context, Mode: "fixed", Address: "192.0.2.10:2575"},
		{Context: context, Mode: "fixed", Address: "example.com:2575"},
		{Context: context, Mode: "fixed", Address: ":2575"},
		{Context: context, Mode: "lenient", Address: "127.0.0.1:0"},
		{Context: context, Mode: "fixed", Address: "127.0.0.1:0", MaxMessages: 5000},
	} {
		refused := app.StartSampleFixture(request)
		if refused.State != desktop.Failed || refused.Phase != desktop.CaptureFailed || refused.Origin != "synthetic" {
			t.Fatalf("%+v answered %+v", request, refused)
		}
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("a refused fixture wrote %v", now)
	}

	// On loopback it records one synthetic case the application names and
	// registers.
	answered := make(chan desktop.SampleFixtureResult, 1)
	go func() {
		answered <- app.StartSampleFixture(desktop.SampleFixtureRequest{Context: context, Mode: "fixed", Address: "127.0.0.1:0", MaxMessages: 1, IdleTimeout: "10s"})
	}()
	var bound string
	for deadline := time.Now().Add(15 * time.Second); bound == "" && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case result := <-answered:
			t.Fatalf("the fixture answered before it listened: %+v", result)
		default:
		}
		if progress := app.CaptureProgress(); progress.State == desktop.Completed {
			bound = progress.Progress.BoundAddress
		}
	}
	conn, frames := dialFixture(t, bound)
	if ack := sendFixtureMessage(t, conn, frames, "listen-s12.hl7"); !strings.Contains(ack, "MSA|AA|") {
		t.Fatalf("the fixture answered %q", ack)
	}
	result := <-answered
	if result.State != desktop.Completed || result.Received != 1 || result.Case == nil || result.Ledger == nil || result.Origin != "synthetic" {
		t.Fatalf("the fixture session: %+v", result)
	}
	var recorded desktop.CatalogItem
	for _, item := range app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem}).Page.Items {
		if item.Ref.ID == result.Case.ID {
			recorded = item
		}
	}
	if recorded.Summary.Case == nil || !recorded.Summary.Case.Registered || !slices.Contains(recorded.Summary.Case.Tags, "synthetic") ||
		recorded.Summary.Case.Provenance != "synthetic" || recorded.Summary.Case.Fixture == nil || recorded.Summary.Case.Fixture.Mode != "fixed" ||
		recorded.Summary.Case.Fixture.Observation != result.ObservationEntry {
		t.Fatalf("the recorded case: %+v", recorded)
	}
}

func TestTheVocabularyNamesEveryLibraryChoice(t *testing.T) {
	shell := workspaceApp(t).Shell()
	vocabulary := shell.Shell.Vocabulary
	if !slices.Equal(vocabulary.Checks.Operators, assertion.Operators()) || len(vocabulary.Checks.Operators) != 16 {
		t.Fatalf("operators: %v", vocabulary.Checks.Operators)
	}
	if len(vocabulary.Scenarios.Templates) == 0 || len(vocabulary.Scenarios.Catalog.Profiles) == 0 || len(vocabulary.Profiles.Usages) == 0 ||
		len(vocabulary.FixtureModes) != 2 || !slices.Contains(vocabulary.AffectedTestImpacts, "unknown") {
		t.Fatalf("vocabulary: %+v", vocabulary)
	}
	encoded, err := json.Marshal(vocabulary.Scenarios.Templates)
	if err != nil || strings.Contains(string(encoded), "null") {
		t.Fatalf("templates: %s %v", encoded, err)
	}
}
