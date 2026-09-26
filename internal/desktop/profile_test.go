package desktop_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profileversion"
)

func TestInspectProfilePack(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()

	packFixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pack.json"), packFixture, 0600); err != nil {
		t.Fatal(err)
	}

	result := app.InspectProfilePack(root, "pack.json")
	if result.State != desktop.Completed || result.Pack == nil {
		t.Fatalf("expected pack inspection to succeed: %+v", result)
	}

	if result.Pack.ID != "fixture-siu" || result.Pack.Version != "1" {
		t.Fatalf("unexpected pack identity: %+v", result.Pack)
	}
	if result.Provenance.RightsReview.Status != "approved" {
		t.Fatalf("unexpected rights review status: %s", result.Provenance.RightsReview.Status)
	}
	if !result.Bundleable {
		t.Fatal("expected approved pack to be bundleable")
	}

	// Verify coverage levels
	var siu *profilepack.Coverage
	for i := range result.Coverage {
		if result.Coverage[i].Family == "SIU" {
			siu = &result.Coverage[i]
			break
		}
	}
	if siu == nil {
		t.Fatal("expected SIU coverage entry")
	}
	if siu.Parse != "supported" || siu.Labels != "supported" || siu.Structural != "unsupported" || siu.Workflow != "unsupported" {
		t.Fatalf("unexpected SIU coverage levels: %+v", siu)
	}

	// Test malformed pack refused
	if err := os.WriteFile(filepath.Join(root, "bad-pack.json"), []byte(`{"not":"a pack"}`), 0600); err != nil {
		t.Fatal(err)
	}
	refusedResult := app.InspectProfilePack(root, "bad-pack.json")
	if refusedResult.State != desktop.Failed {
		t.Fatalf("expected malformed pack to fail: %+v", refusedResult)
	}
}

func TestOpenProfileLibrary(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	libraryDir := filepath.Join(root, "library")
	if err := os.Mkdir(libraryDir, 0700); err != nil {
		t.Fatal(err)
	}

	packFixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libraryDir, "pack.json"), packFixture, 0600); err != nil {
		t.Fatal(err)
	}

	result := app.OpenProfileLibrary(root, "library")
	if result.State != desktop.Completed {
		t.Fatalf("expected library scan to succeed: %+v", result)
	}
	if len(result.Entries) == 0 {
		t.Fatal("expected at least one valid pack entry in library")
	}

	// Non-existent directory should return Failed
	missingResult := app.OpenProfileLibrary(root, "non-existent")
	if missingResult.State != desktop.Failed {
		t.Fatalf("expected missing library dir to fail: %+v", missingResult)
	}
}

func TestValidateAndOpenProfile(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()

	validDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	packDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	packPath := filepath.Join(root, "pack.json")
	if err := os.WriteFile(packPath, packDoc, 0600); err != nil {
		t.Fatal(err)
	}

	// Validate valid profile
	valResult := app.ValidateProfile(desktop.ProfileValidateRequest{
		Workspace: root,
		Document:  string(validDoc),
		Pack:      "pack.json",
	})
	if valResult.State != desktop.Completed || valResult.Profile == nil || valResult.Seal == nil {
		t.Fatalf("expected validation to succeed: %+v", valResult)
	}
	if valResult.Seal.Profile.ID != "fixture-local-siu" {
		t.Fatalf("unexpected profile id: %s", valResult.Seal.Profile.ID)
	}

	// Validate invalid / refused profile
	refusedDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile-refused.json"))
	if err != nil {
		t.Fatal(err)
	}
	valRefused := app.ValidateProfile(desktop.ProfileValidateRequest{
		Workspace: root,
		Document:  string(refusedDoc),
		Pack:      "pack.json",
	})
	if valRefused.State != desktop.Failed {
		t.Fatalf("expected invalid profile to fail: %+v", valRefused)
	}
	if valRefused.Reason == "" {
		t.Fatal("expected reason for invalid profile")
	}

	// Write profile to workspace and OpenProfile
	localProfilePath := filepath.Join(root, "profile.json")
	if err := os.WriteFile(localProfilePath, validDoc, 0600); err != nil {
		t.Fatal(err)
	}
	openResult := app.OpenProfile(root, "profile.json", "pack.json")
	if openResult.State != desktop.Completed || openResult.Profile == nil {
		t.Fatalf("expected OpenProfile to succeed: %+v", openResult)
	}
}

func TestValidateProfileRefusesUnreadableNamedPackLikeOpenProfile(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	profile, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	refused, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack-refused.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"profile.json":      profile,
		"pack.json":         pack,
		"refused-pack.json": refused,
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	for _, entry := range []string{"missing-pack.json", "refused-pack.json"} {
		t.Run(entry, func(t *testing.T) {
			opened := app.OpenProfile(root, "profile.json", entry)
			validated := app.ValidateProfile(desktop.ProfileValidateRequest{
				Workspace: root, Document: string(profile), Pack: entry,
			})
			if opened.State != desktop.Failed || opened.Reason == "" {
				t.Fatalf("OpenProfile did not refuse %s: %+v", entry, opened)
			}
			if validated.State != opened.State || validated.Reason != opened.Reason ||
				validated.Profile != nil || validated.Resolution != nil || validated.Seal != nil {
				t.Fatalf("ValidateProfile accepted or gave a different refusal for %s: open %+v, validate %+v", entry, opened, validated)
			}
		})
	}
	validated := app.ValidateProfile(desktop.ProfileValidateRequest{
		Workspace: root, Document: string(profile), Pack: "pack.json",
	})
	if validated.State != desktop.Completed || validated.Resolution == nil || !validated.Resolution.Pinned {
		t.Fatalf("named readable pack did not resolve the pin: %+v", validated)
	}

	// With no pack named, validation still works without a workspace: the
	// document is validated and sealed, but no pack can answer its pin.
	withoutPack := app.ValidateProfile(desktop.ProfileValidateRequest{
		Workspace: filepath.Join(root, "missing-workspace"), Document: string(profile),
	})
	if withoutPack.State != desktop.Completed || withoutPack.Seal == nil ||
		withoutPack.Resolution == nil || withoutPack.Resolution.Pinned {
		t.Fatalf("unnamed-pack validation changed: %+v", withoutPack)
	}
}

func TestSaveProfile(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()

	validDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	packDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pack.json"), packDoc, 0600); err != nil {
		t.Fatal(err)
	}

	// Save to new file
	saveResult := app.SaveProfile(desktop.ProfileSaveRequest{
		Workspace:  root,
		Output:     "saved-profile.json",
		SealOutput: "saved-profile-seal.json",
		Document:   string(validDoc),
	})
	if saveResult.State != desktop.Completed || saveResult.Seal == nil {
		t.Fatalf("expected SaveProfile to succeed: %+v", saveResult)
	}

	savedBytes, err := os.ReadFile(filepath.Join(root, "saved-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(savedBytes) == 0 {
		t.Fatal("expected saved file to have content")
	}

	// Immutability: attempt to overwrite existing approved profile file
	overwriteResult := app.SaveProfile(desktop.ProfileSaveRequest{
		Workspace: root,
		Output:    "saved-profile.json",
		Document:  string(validDoc),
	})
	if overwriteResult.State != desktop.Failed {
		t.Fatalf("expected overwrite of approved profile to fail: %+v", overwriteResult)
	}
	if !strings.Contains(overwriteResult.Reason, "immutable") {
		t.Fatalf("expected immutability reason: %s", overwriteResult.Reason)
	}
}

// The editor appends a segment it adds after the ones it opened, so the
// document it hands over holds segments in the order they were added. Saving
// writes the canonical document whatever that order: segments by identifier,
// fields by position, members in the order the contract declares them. What is
// written, what is returned and what the seal was computed over are one set of
// bytes, and opening or validating the same rules answers that document too.
func TestSaveProfileWritesTheCanonicalDocument(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	canonical := fixture(t, "local-profile.json")

	var document map[string]any
	if err := json.Unmarshal([]byte(canonical), &document); err != nil {
		t.Fatal(err)
	}
	segments := document["segments"].([]any)
	slices.Reverse(segments)
	for _, segment := range segments {
		slices.Reverse(segment.(map[string]any)["fields"].([]any))
	}
	outOfOrder, err := json.Marshal(document, json.Deterministic(true), jsontext.WithIndent("    "))
	if err != nil {
		t.Fatal(err)
	}
	if string(outOfOrder) == canonical {
		t.Fatal("the reordered document is the canonical one")
	}

	saved := app.SaveProfile(desktop.ProfileSaveRequest{
		Workspace:  root,
		Document:   string(outOfOrder),
		Output:     "saved-profile.json",
		SealOutput: "saved-profile-seal.json",
	})
	if saved.State != desktop.Completed || saved.Seal == nil {
		t.Fatalf("expected SaveProfile to succeed: %+v", saved)
	}
	if written := read(t, filepath.Join(root, "saved-profile.json")); written != canonical {
		t.Fatalf("SaveProfile wrote a document that is not canonical:\n%s", written)
	}
	if saved.Document != canonical {
		t.Fatalf("SaveProfile returned a document that is not canonical:\n%s", saved.Document)
	}
	if read(t, filepath.Join(root, "saved-profile-seal.json")) != fixture(t, "profile-version.json") {
		t.Fatal("the seal SaveProfile wrote is not the fixture profile's seal")
	}

	// A profile already on disk out of order still opens, and seals as the
	// same rules written canonically do.
	validated := app.ValidateProfile(desktop.ProfileValidateRequest{Workspace: root, Document: string(outOfOrder)})
	if validated.State != desktop.Completed || validated.Document != canonical || validated.Seal == nil || *validated.Seal != *saved.Seal {
		t.Fatalf("ValidateProfile answered a document that is not canonical: %+v", validated)
	}
	writeDocument(t, root, "out-of-order.json", string(outOfOrder))
	opened := app.OpenProfile(root, "out-of-order.json", "")
	if opened.State != desktop.Completed || opened.Document != canonical || opened.Seal == nil || *opened.Seal != *saved.Seal {
		t.Fatalf("OpenProfile answered a document that is not canonical: %+v", opened)
	}
}

func TestCompareProfilesAndUpgradePin(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()

	baseDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	refDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-references.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Target document with bumped version "2" and modified description
	modDoc := strings.Replace(string(baseDoc), `"version": "1"`, `"version": "2"`, 1)
	modDoc = strings.Replace(modDoc, "The scheduling segment, constrained beyond what the pinned pack labels.", "The scheduling segment, revised.", 1)

	if err := os.WriteFile(filepath.Join(root, "profile-v1.json"), baseDoc, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profile-v2.json"), []byte(modDoc), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "references.json"), refDoc, 0600); err != nil {
		t.Fatal(err)
	}

	compareResult := app.CompareProfiles(desktop.ProfileCompareRequest{
		Workspace:  root,
		From:       "profile-v1.json",
		To:         "profile-v2.json",
		References: "references.json",
	})
	if compareResult.State != desktop.Completed {
		t.Fatalf("expected comparison to succeed: %+v", compareResult)
	}
	if compareResult.Comparison == nil || len(compareResult.Comparison.Changes) == 0 {
		t.Fatal("expected comparison changes")
	}
	if compareResult.Assessment == nil || len(compareResult.Assessment.Tests) == 0 {
		t.Fatal("expected assessments for test references")
	}

	// Verify test-reschedule.json is affected
	var reschedule *profileversion.AssessedTest
	for i := range compareResult.Assessment.Tests {
		if compareResult.Assessment.Tests[i].Test == "test-reschedule.json" {
			reschedule = &compareResult.Assessment.Tests[i]
			break
		}
	}
	if reschedule == nil {
		t.Fatal("expected test-reschedule.json in assessment")
	}
	if reschedule.Impact != profileversion.ImpactAffected {
		t.Fatalf("expected test-reschedule.json to be affected, got: %s", reschedule.Impact)
	}

	// The comparison decides the exact pin an affected test may move to: the
	// seal over the later profile it compared, as the profile reader seals it.
	targetVal := app.ValidateProfile(desktop.ProfileValidateRequest{
		Workspace: root,
		Document:  modDoc,
	})
	if targetVal.Seal == nil {
		t.Fatalf("expected target seal: %+v", targetVal)
	}
	if compareResult.LaterPin == nil || compareResult.LaterPin.Pin == nil || compareResult.LaterPin.Refusal != "" {
		t.Fatalf("expected the later profile's pin: %+v", compareResult.LaterPin)
	}
	if *compareResult.LaterPin.Pin != targetVal.Seal.Pin() {
		t.Fatalf("later pin = %+v, want the later profile's seal %+v", *compareResult.LaterPin.Pin, targetVal.Seal.Pin())
	}

	// Explicitly upgrade pin for test-reschedule.json
	upgradeResult := app.UpgradeProfilePin(desktop.ProfileUpgradePinRequest{
		Workspace:  root,
		References: "references.json",
		Test:       "test-reschedule.json",
		WasPin:     reschedule.Pinned,
		NowPin:     *compareResult.LaterPin.Pin,
		Output:     "references.json",
	})
	if upgradeResult.State != desktop.Completed || upgradeResult.References == nil {
		t.Fatalf("expected UpgradeProfilePin to succeed: %+v", upgradeResult)
	}

	// Verify pin was updated
	found := false
	for _, pin := range upgradeResult.References.Tests {
		if pin.Test == "test-reschedule.json" {
			found = true
			if pin.Pinned.Version != "2" || pin.Pinned.SHA256 != targetVal.Seal.Content.SHA256 {
				t.Fatalf("pin was not upgraded properly: %+v", pin.Pinned)
			}
		}
	}
	if !found {
		t.Fatal("did not find upgraded pin for test-reschedule.json")
	}

	// Upgrade with bad WasPin should fail
	badUpgrade := app.UpgradeProfilePin(desktop.ProfileUpgradePinRequest{
		Workspace:  root,
		References: "references.json",
		Test:       "test-reschedule.json",
		WasPin:     reschedule.Pinned, // already upgraded to v2, so wasPin v1 should fail
		NowPin:     targetVal.Seal.Pin(),
		Output:     "references.json",
	})
	if badUpgrade.State != desktop.Failed {
		t.Fatalf("expected stale pin upgrade to fail: %+v", badUpgrade)
	}
}

// TestProfileConditionIsNeverAssumed holds validating and saving a conditional
// field to the condition the person authored: with none, or with a member the
// editor left blank, both are refused saying what is missing, and nothing is
// written.
func TestProfileConditionIsNeverAssumed(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	baseDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ usage, reason string }{
		{`"usage": "C",`, "SCH-1 is conditional, so it declares the condition its presence depends on"},
		{`"usage": "C", "condition": {"segment": "ZPD", "position": 0, "operator": ""},`, "a condition names a position between 1 and 999"},
		{`"usage": "C", "condition": {"segment": "", "position": 7, "operator": "absent"},`, "a condition names a segment of three uppercase letters or digits"},
		{`"usage": "C", "condition": {"segment": "ZPD", "position": 7, "operator": ""},`, "a condition operator is one of present, absent or value_in"},
	} {
		document := strings.Replace(string(baseDoc), `"usage": "R",`, tc.usage, 1)
		validated := app.ValidateProfile(desktop.ProfileValidateRequest{Workspace: root, Document: document})
		if validated.State != desktop.Failed || !strings.Contains(validated.Reason, tc.reason) {
			t.Fatalf("validating %s: %+v, want refused with %q", tc.usage, validated, tc.reason)
		}
		saved := app.SaveProfile(desktop.ProfileSaveRequest{Workspace: root, Document: document, Output: "profile.json", SealOutput: "seal.json"})
		if saved.State != desktop.Failed || !strings.Contains(saved.Reason, tc.reason) {
			t.Fatalf("saving %s: %+v, want refused with %q", tc.usage, saved, tc.reason)
		}
		if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
			t.Fatalf("a refused save wrote %v (%v)", entries, err)
		}
	}
}

// TestCompareProfilesDecidesTheLaterProfilePin covers when a comparison offers
// the later profile's pin and when it refuses one: only an affected test needs
// it, and only a later profile read from a file of the workspace can be pinned.
func TestCompareProfilesDecidesTheLaterProfilePin(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	baseDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	refDoc, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-references.json"))
	if err != nil {
		t.Fatal(err)
	}
	modDoc := strings.Replace(string(baseDoc), `"version": "1"`, `"version": "2"`, 1)
	modDoc = strings.Replace(modDoc, "The scheduling segment, constrained beyond what the pinned pack labels.", "The scheduling segment, revised.", 1)
	for name, data := range map[string][]byte{"profile-v1.json": baseDoc, "profile-v2.json": []byte(modDoc), "references.json": refDoc} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Without a references file no test is affected, so no pin is decided.
	unassessed := app.CompareProfiles(desktop.ProfileCompareRequest{Workspace: root, From: "profile-v1.json", To: "profile-v2.json"})
	if unassessed.State != desktop.Completed || unassessed.LaterPin != nil {
		t.Fatalf("a comparison with no affected test decides no pin: %+v", unassessed)
	}

	// A later profile given as a document rather than a workspace file is
	// compared, but no saved test is pinned to it.
	inline := app.CompareProfiles(desktop.ProfileCompareRequest{Workspace: root, From: "profile-v1.json", To: modDoc, References: "references.json"})
	if inline.State != desktop.Completed || inline.LaterPin == nil {
		t.Fatalf("expected a completed comparison deciding the pin: %+v", inline)
	}
	if inline.LaterPin.Pin != nil || inline.LaterPin.Refusal != "the later profile is not a file of the open workspace, so no saved test can be pinned to it" {
		t.Fatalf("an inline later profile must be refused a pin: %+v", inline.LaterPin)
	}
}

func TestProfilePackageExportImportInspect(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()

	fixturesDir := filepath.Join("..", "..", "testdata", "fixtures")
	for _, f := range []string{"local-profile.json", "profile-pack.json", "profile-version.json", "profile-origin.json"} {
		data, err := os.ReadFile(filepath.Join(fixturesDir, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Export without reviewed flag -> must fail
	unreviewedResult := app.ExportProfilePackage(desktop.ProfilePackageExportRequest{
		Workspace: root,
		Profile:   "local-profile.json",
		Pack:      "profile-pack.json",
		Version:   "profile-version.json",
		Origin:    "profile-origin.json",
		Output:    "package.json",
		Reviewed:  false,
	})
	if unreviewedResult.State != desktop.Failed {
		t.Fatalf("expected unreviewed export to fail: %+v", unreviewedResult)
	}

	// Export with reviewed: true -> must succeed
	exportResult := app.ExportProfilePackage(desktop.ProfilePackageExportRequest{
		Workspace: root,
		Profile:   "local-profile.json",
		Pack:      "profile-pack.json",
		Version:   "profile-version.json",
		Origin:    "profile-origin.json",
		Output:    "package.json",
		Reviewed:  true,
	})
	if exportResult.State != desktop.Completed || exportResult.Pack == nil {
		t.Fatalf("expected export to succeed: %+v", exportResult)
	}

	// Inspect exported package
	inspectResult := app.InspectProfilePackage(root, "package.json")
	if inspectResult.State != desktop.Completed || inspectResult.Profile == nil {
		t.Fatalf("expected inspect package to succeed: %+v", inspectResult)
	}
	if inspectResult.Profile.ID != "fixture-local-siu" {
		t.Fatalf("unexpected profile id in package: %s", inspectResult.Profile.ID)
	}

	// Import to subfolder
	importResult := app.ImportProfilePackage(desktop.ProfilePackageImportRequest{
		Workspace: root,
		Package:   "package.json",
		Output:    "imported",
	})
	if importResult.State != desktop.Completed {
		t.Fatalf("expected import to succeed: %+v", importResult)
	}

	// Verify imported files exist
	for _, f := range []string{"profile.json", "pack.json", "version.json", "origin.json"} {
		if _, err := os.Stat(filepath.Join(root, "imported", f)); err != nil {
			t.Fatalf("expected imported file %s to exist: %v", f, err)
		}
	}

	// Import again to same destination -> must fail (cannot overwrite existing)
	conflictResult := app.ImportProfilePackage(desktop.ProfilePackageImportRequest{
		Workspace: root,
		Package:   "package.json",
		Output:    "imported",
	})
	if conflictResult.State != desktop.Failed {
		t.Fatalf("expected conflicting import to fail: %+v", conflictResult)
	}
}

// TestOpenValidateAndSaveResolveAProfileAgainstOnePack is the #452 class of
// divergence closed for good: opening a profile, validating its document and
// saving it choose the pack by one rule, so the same profile in the same
// workspace resolves identically through all three. A named pack is read where
// it is named; with none named, the pinned pack is looked for among the open
// workspace's own entries, never in the folder the profile happens to be
// opened from, so a profile an import wrote resolves the same way opened as its
// document does in the editor. Before, opening found the pack beside the
// imported profile and validating and saving its document did not.
func TestOpenValidateAndSaveResolveAProfileAgainstOnePack(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	document := fixture(t, "local-profile.json")
	if err := os.Mkdir(filepath.Join(root, "imported"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, filepath.Join("imported", "profile.json"), document)
	writeDocument(t, root, filepath.Join("imported", "pack.json"), fixture(t, "profile-pack.json"))
	writeDocument(t, root, "adt-pack.json", fixture(t, "profile-pack-adt.json"))

	saves := 0
	resolved := func(pack string) []desktop.LocalProfileResult {
		t.Helper()
		results := []desktop.LocalProfileResult{
			app.OpenProfile(root, "imported/profile.json", pack),
			app.ValidateProfile(desktop.ProfileValidateRequest{Workspace: root, Document: document, Pack: pack}),
		}
		if pack == "" {
			saves++
			name := "saved-" + strconv.Itoa(saves) + ".json"
			results = append(results, app.SaveProfile(desktop.ProfileSaveRequest{Workspace: root, Document: document, Output: name}))
		}
		for _, result := range results {
			if result.State != desktop.Completed || result.Resolution == nil || result.Seal == nil {
				t.Fatalf("pack %q: %+v", pack, result)
			}
			if !reflect.DeepEqual(result.Resolution, results[0].Resolution) || *result.Seal != *results[0].Seal {
				t.Fatalf("pack %q: opening, validating and saving resolved differently:\n%+v\n%+v", pack, results[0].Resolution, result.Resolution)
			}
		}
		return results
	}

	// The pinned pack is only beside the imported profile, and nobody named
	// it: no pack among the workspace's own entries answers, for all three.
	if resolved("")[0].Resolution.Pinned {
		t.Fatal("the pack beside the imported profile answered with none named")
	}
	// Named, it answers opening and validating alike.
	if !resolved("imported/pack.json")[0].Resolution.Pinned {
		t.Fatal("the named pinned pack did not answer")
	}
	// A pack that is not the pinned one is read for nothing by either.
	if resolved("adt-pack.json")[0].Resolution.Pinned {
		t.Fatal("a pack the profile does not pin answered")
	}

	// The pinned pack among the workspace's own entries answers all three.
	writeDocument(t, root, "profile-pack.json", fixture(t, "profile-pack.json"))
	if !resolved("")[0].Resolution.Pinned {
		t.Fatal("the workspace's pinned pack did not answer")
	}

	// A second, different document claiming the pinned id and version leaves
	// nothing to choose by, so all three resolve against no pack.
	writeDocument(t, root, "another-siu.json", strings.Replace(fixture(t, "profile-pack.json"), `"hl7_version": "2.4"`, `"hl7_version": "2.6"`, 1))
	if resolved("")[0].Resolution.Pinned {
		t.Fatal("one of two different packs claiming the pin was chosen")
	}
}
