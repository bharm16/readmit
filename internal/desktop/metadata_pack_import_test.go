package desktop_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/scenariogen"
)

func TestNamedLibraryImportsEverySupportedPackWholeWithoutMigration(t *testing.T) {
	for _, schema := range []string{"readmit-profile-pack/v1", "readmit-profile-pack/v2", "readmit-profile-pack/v3", "readmit-profile-pack/v4", "readmit-profile-pack/v5"} {
		t.Run(schema, func(t *testing.T) {
			app, context := namedProject(t)
			var raw []byte
			if schema == "readmit-profile-pack/v1" {
				raw = []byte(fixture(t, "profile-pack.json"))
			} else {
				raw = versionedProfileDoc(t, "pack.json", schema)
			}
			path := filepath.Join(t.TempDir(), "metadata-pack.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			before := entries(t, context.Project)
			imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: path})
			if imported.State != desktop.Completed || imported.Draft == nil || imported.Draft.Profile == nil || imported.Draft.Profile.MetadataPack == nil {
				t.Fatalf("native-selected %s pack could not enter the existing Library save flow: %+v", schema, imported)
			}
			if !reflect.DeepEqual(before, entries(t, context.Project)) {
				t.Fatal("import published before explicit Save")
			}
			decoded, err := profileeval.DecodePack(raw)
			if err != nil {
				t.Fatal(err)
			}
			if imported.Draft.Name != decoded.Metadata.Identity.ID || imported.Draft.Profile.MetadataPack.Schema != schema ||
				imported.Draft.Profile.MetadataPack.Document != string(raw) || !reflect.DeepEqual(imported.Draft.Profile.MetadataPack.Metadata, decoded.Metadata) {
				t.Fatal("import altered pack identity, provenance, declared support or original clauses")
			}
			wire, err := json.Marshal(imported.Draft)
			if err != nil {
				t.Fatal(err)
			}
			var boundDraft desktop.ItemDraft
			if err := json.Unmarshal(wire, &boundDraft); err != nil {
				t.Fatal(err)
			}
			saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: boundDraft, IntentID: "pack-import"})
			if saved.Outcome != desktop.SavedOutcome {
				t.Fatalf("whole pack save: %+v", saved)
			}
			_, published := librarySavedMember(t, context.Project, *saved.Saved, "profile")
			if !bytes.Equal(published, raw) {
				t.Fatal("saving normalized or migrated the pack")
			}
			reopened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *saved.Saved})
			if reopened.State != desktop.Completed || reopened.Draft.Profile.MetadataPack == nil || reopened.Draft.Profile.MetadataPack.Document != string(raw) {
				t.Fatalf("pack is not a reusable named Library object: %+v", reopened)
			}
			packs := app.MetadataPacks(context)
			if packs.State != desktop.Completed || len(packs.Packs) != 1 || packs.Packs[0].Item.Ref.ID != saved.Saved.ID ||
				packs.Packs[0].Pack == nil || *packs.Packs[0].Pack != decoded.Metadata.Identity ||
				!reflect.DeepEqual(packs.Packs[0].Provenance, &decoded.Metadata.Provenance) {
				t.Fatalf("saved pack projection lost declared metadata: %+v", packs)
			}
			if again := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: *imported.Draft, IntentID: "pack-import"}); !again.Replayed {
				t.Fatalf("repeated Save did not retain one intent: %+v", again)
			}
			beforeDuplicate := entries(t, context.Project)
			duplicate := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: *imported.Draft, IntentID: "same-pack-new-intent"})
			if duplicate.Outcome == desktop.SavedOutcome || !reflect.DeepEqual(beforeDuplicate, entries(t, context.Project)) {
				t.Fatalf("a second object made the same metadata pin ambiguous: %+v", duplicate)
			}
		})
	}
}

func TestImportedNamedPackPinsActualSavedScenarioGeneration(t *testing.T) {
	app, context := namedProject(t)
	packFile, err := filepath.Abs(filepath.Join("..", "..", "testdata", "casegen", "owned", "pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	pack := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: packFile})
	if pack.State != desktop.Completed {
		t.Fatalf("import required pack: %+v", pack)
	}
	packSave := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: *pack.Draft, IntentID: "required-pack"})
	if packSave.Outcome != desktop.SavedOutcome {
		t.Fatalf("save required pack: %+v", packSave)
	}
	profileFile, err := filepath.Abs(filepath.Join("..", "..", "testdata", "casegen", "owned", "profile-siu.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: profileFile})
	if profile.State != desktop.Completed || profile.Draft.Profile.Pack == nil || profile.Draft.Profile.Pack.ID != packSave.Saved.ID {
		t.Fatalf("profile cannot use the named imported pin: %+v", profile)
	}
	profileSave := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Draft: *profile.Draft, IntentID: "pinned-profile"})
	if profileSave.Outcome != desktop.SavedOutcome {
		t.Fatalf("save profile: %+v", profileSave)
	}
	var request casegen.Request
	if err := json.Unmarshal(casegenFixture(t, "request-book-reschedule-cancel.json"), &request); err != nil {
		t.Fatal(err)
	}
	settings := desktop.CaseGenerationSettings{Wire: request.Wire, Bindings: request.Bindings, Variants: []casegen.Variant{}}
	plan := scenariogen.Plan{Schema: scenariogen.Schema, GeneratorVersion: scenariogen.Version, Seed: 7, Template: casegenFixture(t, "scenario-book-reschedule-cancel.json"),
		Rows: []scenariogen.Row{{ID: "plain", PatientName: "DOE", Notes: []string{}, Encoding: "utf-8"}}, Variants: []scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}}}
	scenarioSave := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, IntentID: "pinned-scenario",
		Draft: desktop.ItemDraft{Name: "Book, move, cancel", Scenario: &desktop.ScenarioDraft{Plan: plan, Profile: profileSave.Saved, Generation: &settings}}})
	if scenarioSave.Outcome != desktop.SavedOutcome {
		t.Fatalf("save scenario: %+v", scenarioSave)
	}
	generated := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: *scenarioSave.Saved, IntentID: "from-imported-pack"})
	if generated.State != desktop.Completed || len(generated.Cases) != 1 || generated.Cases[0].Messages != 3 || generated.Cases[0].Verdict != "pass" {
		t.Fatalf("actual encoder did not use the managed imported pin: %+v", generated)
	}
	if opened := app.OpenCase(context.Project, generated.Cases[0].Entry); opened.State != desktop.Completed || opened.Case == nil || opened.Case.Occurrences != 3 {
		t.Fatalf("generated bytes are not usable: %+v", opened)
	}
	badFile := filepath.Join(t.TempDir(), "future-pack.json")
	bad := strings.Replace(string(casegenFixture(t, "owned/pack.json")), `"schema":`, `"future_clause":true,"schema":`, 1)
	if err := os.WriteFile(badFile, []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	before := entries(t, context.Project)
	unsupported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: badFile})
	if unsupported.State != desktop.Failed || unsupported.Draft != nil || !reflect.DeepEqual(before, entries(t, context.Project)) {
		t.Fatalf("unsupported clauses were removed to import a pack: %+v", unsupported)
	}
}
