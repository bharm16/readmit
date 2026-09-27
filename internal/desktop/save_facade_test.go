package desktop_test

// One editor Save publishes one logical revision. These tests drive SaveItem
// through the facade: a stale base is refused and the draft kept, one click
// submitted twice publishes once, a different submission under one click is
// refused, an invalid draft writes nothing, and a copy is a new object.

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
)

// targetDraft is a target configuration draft of the laboratory at address,
// decoded from the document a person would have written.
func targetDraft(address string) *replay.Target {
	var target replay.Target
	if err := json.Unmarshal([]byte(replayTarget(address, "nonproduction")), &target); err != nil {
		panic(err)
	}
	return &target
}

// observationDraft is the facade fixture's source and window, the source
// declaring its collection path relative to the project.
func observationDraft(t *testing.T, scope string) *desktop.ObservationDraft {
	t.Helper()
	var source observesource.Source
	var window observewindow.Window
	if err := json.Unmarshal([]byte(facadeSourceDocument), &source); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(facadeWindowDocument), &window); err != nil {
		t.Fatal(err)
	}
	source.Observes.Scope, window.Source.Scope = scope, scope
	return &desktop.ObservationDraft{Source: source, Window: window}
}

// namedProject creates a project from a name and returns the app and the
// context later requests about it carry.
func namedProject(t *testing.T) (*desktop.App, desktop.RequestContext) {
	t.Helper()
	app := newApp(t, &chooser{folder: t.TempDir()})
	app.ChooseProjectLocation()
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA"})
	if created.State != desktop.Completed {
		t.Fatalf("create: %+v", created)
	}
	return app, created.Context
}

func entries(t *testing.T, root string) []string {
	t.Helper()
	return slices.DeleteFunc(entriesOf(t, root), func(name string) bool { return name == ".readmit" })
}

func TestASaveIsOneWholeRevisionAndOneClickSavesOnce(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments archive", Observation: observationDraft(t, "appointments")}, IntentID: "click-1"})
	if first.State != desktop.Completed || first.Outcome != desktop.SavedOutcome || first.Saved == nil || first.Saved.Revision != "1" || first.Projection == nil {
		t.Fatalf("first save: %+v", first)
	}
	item := *first.Saved

	// The same click arriving again is the same revision, and nothing is
	// written again.
	written := entries(t, root)
	again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments archive", Observation: observationDraft(t, "appointments")}, IntentID: "click-1"})
	if again.Outcome != desktop.SavedOutcome || !again.Replayed || *again.Saved != item {
		t.Fatalf("a repeated click: %+v", again)
	}
	if now := entries(t, root); !slices.Equal(now, written) {
		t.Fatalf("a repeated click wrote again: %v, was %v", now, written)
	}
	// A different draft under that click is refused.
	if reused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: item.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Observation: observationDraft(t, "cancellations")}, IntentID: "click-1"}); reused.Outcome != desktop.FailedOutcome {
		t.Fatalf("a different draft under one click: %+v", reused)
	}

	// Two editors started from revision 1: the first publishes 2, the second
	// is told the current revision, publishes nothing, and keeps its draft.
	second := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: item.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Observation: observationDraft(t, "cancellations")}, IntentID: "editor-a"})
	if second.Outcome != desktop.SavedOutcome || second.Saved.Revision != "2" {
		t.Fatalf("second save: %+v", second)
	}
	written = entries(t, root)
	stale := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: item.ID, BaseRevision: "1",
		Draft: desktop.ItemDraft{Observation: observationDraft(t, "reschedules")}, IntentID: "editor-b"})
	if stale.Outcome != desktop.ConflictOutcome || stale.CurrentRevision != "2" || stale.Saved != nil {
		t.Fatalf("a stale base: %+v", stale)
	}
	if now := entries(t, root); !slices.Equal(now, written) {
		t.Fatalf("a refused save wrote: %v", now)
	}
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ObservationItem})
	if listing.Page == nil || len(listing.Page.Items) != 1 || listing.Page.Items[0].Ref.Revision != "2" || listing.Page.Items[0].Name != "Appointments archive" ||
		listing.Page.Items[0].UpdatedAt == nil || listing.Page.Items[0].CreatedAt == nil {
		t.Fatalf("the observation after two saves: %+v", listing)
	}

	// An invalid draft reports its problems at their fields and writes nothing.
	mismatched := observationDraft(t, "appointments")
	mismatched.Window.Source.Identity = "another-archive"
	invalid := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem, Item: item.ID, BaseRevision: "2",
		Draft: desktop.ItemDraft{Observation: mismatched}, IntentID: "editor-c"})
	if invalid.Outcome != desktop.InvalidOutcome || len(invalid.Problems) != 1 || invalid.Problems[0].Field != "observation.window.source" {
		t.Fatalf("an invalid draft: %+v", invalid)
	}
	if validated := app.ValidateDraft(desktop.DraftRequest{Context: context, Kind: desktop.ObservationItem, Draft: desktop.ItemDraft{Observation: mismatched}}); len(validated.Problems) != 1 || validated.Projection != nil {
		t.Fatalf("validate: %+v", validated)
	}
	if now := entries(t, root); !slices.Equal(now, written) {
		t.Fatalf("an invalid draft wrote: %v", now)
	}

	// Save a copy: a new object with a new identity; the original is as it was.
	copied := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Name: "Appointments archive copy", Observation: observationDraft(t, "cancellations")}, IntentID: "copy-1"})
	if copied.Outcome != desktop.SavedOutcome || copied.Saved.ID == item.ID || copied.Saved.Revision != "1" {
		t.Fatalf("a copy: %+v", copied)
	}
	for _, listed := range app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ObservationItem}).Page.Items {
		if listed.Ref.ID == item.ID && listed.Ref.Revision != "2" {
			t.Fatalf("saving a copy changed the original: %+v", listed)
		}
	}
}

// A test is saved from its answered draft through the generator and reader
// every other test is, and a draft that has not answered what its boundary
// asks names what is missing and saves nothing.
func TestATestIsSavedWholeFromItsAnsweredDraft(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	written := writeCase(t, root, "incident", framed(fixture(t, "listen-s12.hl7"))+framed(fixture(t, "listen-s13.hl7")))
	writeDocument(t, root, "lab.json", replayTarget("127.0.0.1:2575", "nonproduction"))
	draft, err := testauthor.NewDraft("incident", written.Identity)
	if err != nil {
		t.Fatal(err)
	}
	draft.Name, draft.Messages, draft.Target = "Reschedule keeps one appointment", []string{"s0001-e000001", "s0001-e000002"}, "lab.json"
	unanswered := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &draft}, IntentID: "test-1"})
	if unanswered.Outcome != desktop.InvalidOutcome || len(unanswered.Problems) == 0 || !strings.HasPrefix(unanswered.Problems[0].Field, "test") {
		t.Fatalf("an unanswered test: %+v", unanswered)
	}
	draft.Boundary, draft.Reset = testrunner.ACKBoundary, "Restart the listener from an empty ledger before running this."
	draft.Expectations = []testauthor.Expectation{{ID: "accepted", Operator: testauthor.ACKFieldEquals, Message: "s0001-e000001", Selector: "MSA-1", Field: ackText("AA")}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.TestItem, Draft: desktop.ItemDraft{Test: &draft}, IntentID: "test-2"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("an answered test: %+v", saved)
	}
	tests := listed(t, app, root, desktop.TestItem)
	test := tests["Reschedule keeps one appointment"]
	if test.Ref != *saved.Saved || test.Summary.Test.SourceCase == nil || test.Summary.Test.CurrentVersion != "1" {
		t.Fatalf("the saved test: %+v", tests)
	}
	if _, err := os.Stat(filepath.Join(root, "incident", "manifest.json")); err != nil {
		t.Fatal("the case the test names was touched")
	}
}

// An editor's unsaved draft of a catalog object names that object and the
// revision the edit began from, so recovery returns it to the same object and
// its save is checked against that revision. A store holding no such draft is
// written exactly as before.
func TestADraftReturnsToTheObjectItEdits(t *testing.T) {
	store := draftsStore(t)
	app := draftsApp(t, store)
	note := editorDraft("note", desktop.NoteDraftSchema, `{"schema":"readmit-note-draft/v1","name":"","subject":"","title":"","body":"typed"}`)
	if kept := app.SaveEditorDraft(note); kept.State != desktop.Completed {
		t.Fatalf("%+v", kept)
	}
	if !strings.Contains(read(t, store), `"readmit-desktop-drafts/v1"`) {
		t.Fatal("a store of plain drafts was not written as v1")
	}
	edit := editorDraft("environment", "readmit-target/v3", `{"schema":"readmit-target/v3"}`)
	edit.Item = &desktop.DraftItem{ProjectID: strings.Repeat("a", 24), Ref: desktop.ItemRef{Kind: desktop.EnvironmentItem, ID: strings.Repeat("b", 24), Revision: "3"}}
	kept := app.SaveEditorDraft(edit)
	if kept.State != desktop.Completed || !strings.Contains(read(t, store), `"readmit-desktop-drafts/v2"`) {
		t.Fatalf("%+v", kept)
	}
	restored := draftsApp(t, store).EditorDrafts()
	var found *desktop.DraftItem
	for _, draft := range restored.Drafts {
		if draft.Item != nil {
			found = draft.Item
		}
	}
	if restored.State != desktop.Completed || found == nil || found.Ref.Revision != "3" || found.Ref.ID != strings.Repeat("b", 24) {
		t.Fatalf("the draft did not return to its object: %+v", restored)
	}
}

// A double click sends one submission twice at once: one revision is
// published, and the other answer is either that revision or busy, never a
// second revision.
func TestADoubleClickPublishesOneRevision(t *testing.T) {
	app, context := namedProject(t)
	request := desktop.SaveItemRequest{Context: context, Kind: desktop.ObservationItem,
		Draft: desktop.ItemDraft{Observation: observationDraft(t, "appointments")}, IntentID: "double-click"}
	answers := make(chan desktop.SaveItemResult, 2)
	for range 2 {
		go func() { answers <- app.SaveItem(request) }()
	}
	var saved []desktop.ItemRef
	for range 2 {
		switch answer := <-answers; {
		case answer.Outcome == desktop.SavedOutcome:
			saved = append(saved, *answer.Saved)
		case answer.State != desktop.Busy:
			t.Fatalf("a double click: %+v", answer)
		}
	}
	listing := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ObservationItem})
	if len(saved) == 0 || listing.Page == nil || len(listing.Page.Items) != 1 || listing.Page.Items[0].Ref.Revision != "1" {
		t.Fatalf("a double click published %v: %+v", saved, listing.Page)
	}
	for _, ref := range saved {
		if ref != listing.Page.Items[0].Ref {
			t.Fatalf("two answers named different revisions: %v", saved)
		}
	}
}

// variantProject is a named project registering one incident, and a plan
// that keeps its reschedule.
func variantProject(t *testing.T) (*desktop.App, desktop.RequestContext, desktop.ItemRef, reproducer.Plan) {
	t.Helper()
	app, context := namedProject(t)
	root := context.Project
	incident := writeCase(t, root, "incident", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
	registerCase(t, root, "incident", "Original incident")
	plan, err := reproducer.NewPlan(incident.Identity)
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps = append(plan.Steps, reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repRescheduleID})
	return app, context, listed(t, app, root, desktop.CaseItem)["Original incident"].Ref, plan
}

// A variant is saved as one new case with its lineage: a new entry of the
// project, registered as a revision of the case it was derived from, listed
// once by name. The case it came from is not touched, the same click saves
// it once, and a variant is never saved over.
func TestAVariantSaveIsOneCaseWithItsLineage(t *testing.T) {
	app, context, incident, plan := variantProject(t)
	root := context.Project
	before := bytesUnder(t, filepath.Join(root, "incident"))
	draft := desktop.ItemDraft{Name: "Reschedule only", Variant: &desktop.VariantDraft{Source: incident, Plan: plan}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.VariantItem, Draft: draft, IntentID: "variant-click"})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil || saved.Saved.Kind != desktop.VariantItem || saved.Saved.Revision != "1" {
		t.Fatalf("save: %+v", saved)
	}
	variant := listed(t, app, root, desktop.VariantItem)["Reschedule only"]
	if variant.Ref.ID != saved.Saved.ID || variant.Availability != desktop.ItemAvailable || variant.Summary.Variant == nil ||
		variant.Summary.Variant.Form != "revision" || variant.Summary.Variant.Parent == nil || variant.Summary.Variant.Parent.ID != incident.ID ||
		variant.Summary.Variant.Operation != reproducer.Derivation || slices.Contains(variant.Capabilities, desktop.SaveAction) {
		t.Fatalf("the saved variant: %+v", variant)
	}
	revisions, err := project.ReadRevisions(root)
	if err != nil || len(revisions.Revisions) != 1 || revisions.Revisions[0].Name != "variant-001" || revisions.Revisions[0].Operation.Parent != "incident" ||
		revisions.Revisions[0].Operation.ParentIdentity != plan.Case {
		t.Fatalf("the registered revision: %+v %v", revisions, err)
	}
	if opened := app.OpenCase(root, "variant-001"); opened.Case == nil {
		t.Fatalf("the variant's case: %+v", opened)
	}
	if after := bytesUnder(t, filepath.Join(root, "incident")); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatal("saving a variant changed the case it came from")
	}
	writeCase(t, root, "loose", framed(repBooking)+framed(repAccepted)+framed(repReschedule))
	loose := listed(t, app, root, desktop.CaseItem)["@loose"].Ref
	held := entries(t, root)
	again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.VariantItem, Draft: draft, IntentID: "variant-click"})
	if !again.Replayed || again.Saved == nil || again.Saved.ID != saved.Saved.ID || !slices.Equal(entries(t, root), held) {
		t.Fatalf("the same click saved again: %+v", again)
	}
	if staging, _ := os.ReadDir(filepath.Join(root, ".readmit", "staging")); len(staging) != 0 {
		t.Fatalf("a published variant left staging behind: %v", staging)
	}
	for name, request := range map[string]desktop.SaveItemRequest{
		"saved over":           {Context: context, Kind: desktop.VariantItem, Item: saved.Saved.ID, BaseRevision: "1", Draft: draft, IntentID: "over"},
		"another evidence":     {Context: context, Kind: desktop.VariantItem, Draft: desktop.ItemDraft{Variant: &desktop.VariantDraft{Source: incident, Plan: reproducer.Plan{Schema: plan.Schema, Case: strings.Repeat("0", 64), Steps: plan.Steps}}}, IntentID: "other"},
		"an unregistered case": {Context: context, Kind: desktop.VariantItem, Draft: desktop.ItemDraft{Variant: &desktop.VariantDraft{Source: loose, Plan: plan}}, IntentID: "loose"},
	} {
		if refused := app.SaveItem(request); refused.Outcome != desktop.InvalidOutcome || len(refused.Problems) == 0 || !slices.Equal(entries(t, root), held) {
			t.Fatalf("%s: %+v", name, refused)
		}
	}
}

// A profile is saved with the seal of its version, and a version once sealed
// is never published again with other content, by the same object or a copy.
func TestAProfileVersionCannotBeRepublished(t *testing.T) {
	app, context := namedProject(t)
	root := context.Project
	profile, err := localprofile.Decode([]byte(fixture(t, "local-profile.json")))
	if err != nil {
		t.Fatal(err)
	}
	draft := func(profile localprofile.Profile) desktop.ItemDraft {
		return desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: profile}}
	}
	first := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: draft(profile), IntentID: "profile-1"})
	if first.Outcome != desktop.SavedOutcome || first.Saved.Revision != "1" {
		t.Fatalf("save: %+v", first)
	}
	if listed := listed(t, app, root, desktop.ProfileItem)["Scheduling profile"]; listed.Summary.Profile == nil || listed.Summary.Profile.PublishedVersion != profile.Identity.Version ||
		!slices.Contains(listed.Capabilities, desktop.SaveAction) {
		t.Fatalf("the saved profile: %+v", listed)
	}
	if again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: draft(profile), IntentID: "profile-1"}); !again.Replayed {
		t.Fatalf("a repeated click: %+v", again)
	}
	before := entries(t, root)
	changed := profile
	changed.Segments = slices.Clone(changed.Segments)
	changed.Segments[0].Description = "Changed under the same version"
	for _, edited := range []localprofile.Profile{changed, profile} {
		refused := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: first.Saved.ID, BaseRevision: "1", Draft: draft(edited), IntentID: "profile-2"})
		if refused.Outcome != desktop.InvalidOutcome || !slices.ContainsFunc(refused.Problems, func(problem desktop.FieldProblem) bool { return problem.Field == "profile.profile.version" }) {
			t.Fatalf("a republished version: %+v", refused)
		}
	}
	copied := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: draft(changed), IntentID: "copy-1"})
	if copied.Outcome != desktop.InvalidOutcome || copied.Problems[0].Field != "profile.profile.version" {
		t.Fatalf("a copy under a sealed version: %+v", copied)
	}
	if now := entries(t, root); !slices.Equal(now, before) {
		t.Fatalf("a refused save wrote %v", now)
	}
}
