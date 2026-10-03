package desktop_test

import (
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/valuemap"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mapDraft() valuemap.Document {
	return valuemap.Document{Schema: valuemap.Schema, Name: "Owned sender mapping", Edition: "2.5.1", SourceSelector: "MSH[1]-3[1]", DestinationSelector: "MSH[1]-3[1]", SourceMeaning: "Application declared by sender", DestinationMeaning: "Authored receiver application name", Provenance: "Owned local declaration", Table: &valuemap.Table{ID: "0003", Edition: "2.5.1", Provenance: "Owned explicit table reference"}, Entries: []valuemap.Entry{{Source: "READMIT", Destination: "OWNED-RECEIVER", SourceMeaning: "Fixture sender", DestinationMeaning: "Fixture receiver", Provenance: "Owned local note"}}, Associations: []valuemap.Association{}}
}
func TestValueMapFacadePublishesImmutableRevisionsExportsImportsAndNeverBroadensScope(t *testing.T) {
	app, ctx := namedProject(t)
	target := saveEnvironment(t, app, ctx, desktop.SaveItemRequest{IntentID: "map-target", Draft: desktop.ItemDraft{Name: "Map declaration target", Environment: targetDraft("127.0.0.1:65531")}})
	request := desktop.ValueMapSaveRequest{Context: ctx, IntentID: "map-one", Draft: mapDraft()}
	request.Draft.Associations = []valuemap.Association{{Kind: "environment", Item: target.ID, Revision: target.Revision}}
	one := app.SaveValueMap(request)
	if one.State != desktop.Completed || one.Ref == nil {
		t.Fatal(one)
	}
	listed := app.ListValueMaps(ctx)
	if len(listed.Items) != 1 || slices.Contains(listed.Items[0].Capabilities, desktop.RenameAction) || slices.Contains(listed.Items[0].Capabilities, desktop.SaveAction) {
		t.Fatal(listed)
	}
	source := writeCase(t, ctx.Project, "map-source", framed(repBooking))
	caseRef := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.CaseItem}).Page.Items[0].Ref
	inspect := desktop.ValueMapInspectionRequest{Context: ctx, Ref: *one.Ref, Source: caseRef, Identity: source.Identity, Occurrence: "s0001-e000001", Selector: "MSH-3"}
	hidden := app.InspectValueMap(inspect)
	if hidden.State != desktop.Completed || hidden.Mapping != "hidden" || hidden.Entry != nil {
		t.Fatal(hidden)
	}
	inspect.Reveal = true
	mapped := app.InspectValueMap(inspect)
	if mapped.Mapping != "mapped" || mapped.Entry.Destination != "OWNED-RECEIVER" {
		t.Fatal(mapped)
	}
	inspect.Selector = "MSH-4"
	outside := app.InspectValueMap(inspect)
	if outside.Mapping != "not_applicable" || outside.Entry != nil {
		t.Fatal(outside)
	}
	inspect.Selector = "MSH-3"
	request.Base = one.Ref
	request.IntentID = "map-two"
	request.Draft.Entries[0].Source = "DIFFERENT"
	two := app.SaveValueMap(request)
	if two.State != desktop.Completed || two.Ref.Revision != "2" {
		t.Fatal(two)
	}
	inspect.Ref = *two.Ref
	if result := app.InspectValueMap(inspect); result.Mapping != "unmapped" || result.Entry != nil {
		t.Fatal(result)
	}
	old := app.ReadValueMap(desktop.ItemRequest{Context: ctx, Ref: *one.Ref})
	if old.Map.Entries[0].Source != "READMIT" {
		t.Fatal("old map changed")
	}
	request.IntentID = "map-conflict"
	if conflict := app.SaveValueMap(request); conflict.State != desktop.Failed {
		t.Fatal("conflicting map save published")
	}
	dest := filepath.Join(t.TempDir(), "owned.csv")
	exported := app.ExportValueMapCSV(desktop.ValueMapExportRequest{Context: ctx, Ref: *one.Ref, Destination: dest})
	if exported.State != desktop.Completed || exported.Bytes == 0 {
		t.Fatal(exported)
	}
	imported := app.ImportValueMapCSV(desktop.ValueMapImportRequest{Context: ctx, Path: dest})
	if imported.State != desktop.Completed || imported.Map.Table.ID != "0003" || imported.Map.Entries[0].Source != "READMIT" {
		t.Fatal(imported)
	}
	if len(app.ListValueMaps(ctx).Items) != 1 {
		t.Fatal("CSV import published draft")
	}
	raw, _ := os.ReadFile(dest)
	if again := app.ExportValueMapCSV(desktop.ValueMapExportRequest{Context: ctx, Ref: *one.Ref, Destination: dest}); again.State != desktop.Failed {
		t.Fatal("export overwrote file")
	}
	after, _ := os.ReadFile(dest)
	if string(after) != string(raw) {
		t.Fatal("failed export changed prior CSV")
	}
	request.Base = two.Ref
	request.Draft.Project = "0123456789abcdef01234567"
	if result := app.SaveValueMap(request); result.State != desktop.Failed {
		t.Fatal("map inherited another project's scope")
	}
}

func TestValueMapEditorDraftRetainsIncompleteAuthoredEntriesAtExactSourceScope(t *testing.T) {
	app, ctx := namedProject(t)
	source := writeCase(t, ctx.Project, "map-draft-source", framed(repBooking))
	ref := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.CaseItem}).Page.Items[0].Ref
	content := desktop.ValueMapEditorDraft{Schema: desktop.ValueMapEditorDraftSchema, Source: ref, Identity: source.Identity, Occurrence: "s0001-e000001", Selector: "MSH[1]-3[1]", Edition: "2.5.1", Draft: mapDraft(), WorkingEntry: &valuemap.Entry{Source: "Incomplete authored source"}}
	content.Draft.Name = ""
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	held := app.SaveEditorDraft(desktop.EditorDraft{Kind: "field-value-map", Workspace: ctx.Project, ContentSchema: desktop.ValueMapEditorDraftSchema, Content: raw})
	if held.State != desktop.Completed || len(held.Drafts) != 1 {
		t.Fatal(held)
	}
	request := desktop.ValueMapDraftRequest{Context: ctx, Source: ref, Identity: source.Identity, Occurrence: "s0001-e000001", Selector: "MSH-3", Edition: "2.5.1"}
	restored := app.ReadValueMapDraft(request)
	if restored.State != desktop.Completed || restored.Draft == nil || restored.Draft.WorkingEntry.Source != "Incomplete authored source" || restored.Draft.Draft.Name != "" {
		t.Fatal(restored)
	}
	request.Identity = strings.Repeat("a", 64)
	if wrong := app.ReadValueMapDraft(request); wrong.State != desktop.Empty || wrong.Draft != nil {
		t.Fatal("draft rebound to changed source")
	}
	if after := app.EditorDrafts(); len(after.Drafts) != 1 {
		t.Fatal("unavailable source draft disappeared")
	}
	content.Identity = strings.Repeat("z", 64)
	bad, _ := json.Marshal(content)
	if result := app.SaveEditorDraft(desktop.EditorDraft{Kind: "field-value-map", Workspace: ctx.Project, ContentSchema: desktop.ValueMapEditorDraftSchema, Content: bad}); result.State != desktop.Failed {
		t.Fatal("nonhex hash retained")
	}
}

func TestValueMapDraftRetainsUnknownSourceEditionWithoutInferringMapApplicability(t *testing.T) {
	app, ctx := namedProject(t)
	source := writeCase(t, ctx.Project, "map-unknown-edition", framed(strings.Replace(repBooking, "|2.5.1\r", "|\r", 1)))
	ref := app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.CaseItem}).Page.Items[0].Ref
	content := desktop.ValueMapEditorDraft{Schema: desktop.ValueMapEditorDraftSchema, Source: ref, Identity: source.Identity, Occurrence: "s0001-e000001", Selector: "MSH[1]-3[1]", Edition: "", Draft: mapDraft(), WorkingEntry: &valuemap.Entry{Source: "Incomplete owner note"}}
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	saved := app.SaveEditorDraft(desktop.EditorDraft{Kind: "field-value-map", Workspace: ctx.Project, ContentSchema: desktop.ValueMapEditorDraftSchema, Content: raw})
	if saved.State != desktop.Completed {
		t.Fatalf("unknown source edition discarded authored draft: %+v", saved)
	}
	restored := app.ReadValueMapDraft(desktop.ValueMapDraftRequest{Context: ctx, Source: ref, Identity: source.Identity, Occurrence: content.Occurrence, Selector: content.Selector, Edition: ""})
	if restored.State != desktop.Completed || restored.Draft.Edition != "" || restored.Draft.Draft.Edition != "2.5.1" {
		t.Fatal("source edition guessed from authored map")
	}
	published := app.SaveValueMap(desktop.ValueMapSaveRequest{Context: ctx, IntentID: "unknown-edition-declaration", Draft: mapDraft()})
	if published.State != desktop.Completed {
		t.Fatal(published)
	}
	shown := app.InspectValueMap(desktop.ValueMapInspectionRequest{Context: ctx, Ref: *published.Ref, Source: ref, Identity: source.Identity, Occurrence: content.Occurrence, Selector: content.Selector, Reveal: true})
	if shown.State != desktop.Completed || shown.Mapping != "incompatible" || shown.Entry != nil {
		t.Fatalf("unknown source inherited authored edition: %+v", shown)
	}
}
