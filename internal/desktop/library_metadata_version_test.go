package desktop_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/scenariogen"
)

func TestLibraryGenerationMetadataUsesV2WithoutChangingV1Objects(t *testing.T) {
	app, context, scenarioRef, profile, settings := generationProject(t,
		[]scenariogen.Row{{ID: "plain", PatientName: "DOE", Notes: []string{}, Encoding: "utf-8"}},
		[]scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}})
	openedProfile := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: profile})
	openedProfile.Draft.Profile.Profile.Identity.Version = "2"
	profileSave := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem, Item: profile.ID,
		Draft: *openedProfile.Draft, IntentID: "metadata-profile"})
	if profileSave.Outcome != desktop.SavedOutcome {
		t.Fatalf("profile save: %+v", profileSave)
	}
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: scenarioRef})
	opened.Draft.Scenario.Profile = profileSave.Saved
	v1 := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Item: scenarioRef.ID, BaseRevision: scenarioRef.Revision,
		Draft: *opened.Draft, IntentID: "metadata-v1"})
	if v1.Outcome != desktop.SavedOutcome {
		t.Fatalf("legacy metadata save: %+v", v1)
	}
	v1Path, v1Raw := librarySavedMember(t, context.Project, *v1.Saved, "metadata")
	if schemaFromTestBytes(t, v1Raw) != "readmit-library-metadata/v1" || bytes.Contains(v1Raw, []byte(`"generation"`)) {
		t.Fatalf("the original name/profile metadata shape changed: %s", v1Raw)
	}
	reopenedV1 := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *v1.Saved})
	if reopenedV1.State != desktop.Completed || reopenedV1.Draft.Scenario.Generation != nil ||
		!reflect.DeepEqual(reopenedV1.Draft.Scenario.Profile, profileSave.Saved) {
		t.Fatalf("v1 named open changed meaning: %+v", reopenedV1)
	}
	reopenedV1.Draft.Scenario.Generation = &settings
	v2 := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Item: v1.Saved.ID, BaseRevision: v1.Saved.Revision,
		Draft: *reopenedV1.Draft, IntentID: "metadata-v2"})
	if v2.Outcome != desktop.SavedOutcome {
		t.Fatalf("generation metadata save: %+v", v2)
	}
	_, v2Raw := librarySavedMember(t, context.Project, *v2.Saved, "metadata")
	if schemaFromTestBytes(t, v2Raw) != "readmit-library-metadata/v2" {
		t.Fatalf("generation was written under the original contract: %s", v2Raw)
	}
	reopenedV2 := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *v2.Saved})
	if reopenedV2.State != desktop.Completed || !reflect.DeepEqual(reopenedV2.Draft.Scenario.Generation, &settings) {
		t.Fatalf("v2 settings did not reopen: %+v", reopenedV2)
	}
	old := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: *v1.Saved})
	unchanged, err := os.ReadFile(v1Path)
	if err != nil || !bytes.Equal(unchanged, v1Raw) || old.State != desktop.Completed || old.Draft.Scenario.Generation != nil {
		t.Fatalf("saving v2 migrated or reinterpreted the old metadata: %v %+v", err, old)
	}

	// Model a preexisting well-sealed catalog snapshot containing a member
	// this release must refuse. The new aggregate cannot widen its v1 reader.
	_, plan := librarySavedMember(t, context.Project, *v2.Saved, "scenario")
	malformed := bytes.Replace(v2Raw, []byte("readmit-library-metadata/v2"), []byte("readmit-library-metadata/v1"), 1)
	store, err := catalog.Open(context.Project)
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.Save(catalog.Draft{Kind: string(desktop.ScenarioItem), Name: "Unsupported legacy metadata", Intent: "legacy-unknown-generation",
		Digest: networkaction.Digest(append(bytes.Clone(plan), malformed...)), Members: []catalog.Staged{
			{Role: "scenario", File: "plan.json", Data: plan}, {Role: "metadata", File: "metadata.json", Data: malformed}}},
		func(map[string]string) error { return nil }, catalog.Options{})
	if err != nil {
		t.Fatal(err)
	}
	refused := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem, ID: published.Item.ID}})
	if refused.State != desktop.Failed || refused.Draft != nil {
		t.Fatalf("v1 silently accepted the new generation member: %+v", refused)
	}
}

func librarySavedMember(t *testing.T, root string, ref desktop.ItemRef, role string) (string, []byte) {
	t.Helper()
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	item := document.Items[document.Find(ref.ID)]
	for _, revision := range item.Revisions {
		if ref.Revision != "" && strconv.Itoa(revision.Number) != ref.Revision {
			continue
		}
		for _, member := range revision.Members {
			if member.Role != role {
				continue
			}
			path := store.Path(member)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			return path, raw
		}
	}
	t.Fatalf("the saved revision has no %s member", role)
	return "", nil
}

func schemaFromTestBytes(t *testing.T, raw []byte) string {
	t.Helper()
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(raw, &declared); err != nil {
		t.Fatal(err)
	}
	return declared.Schema
}
