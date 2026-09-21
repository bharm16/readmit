package desktop_test

import (
	"os"
	"path/filepath"
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
	libraryDir := t.TempDir()

	packFixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libraryDir, "pack.json"), packFixture, 0600); err != nil {
		t.Fatal(err)
	}

	result := app.OpenProfileLibrary(root, libraryDir)
	if result.State != desktop.Completed {
		t.Fatalf("expected library scan to succeed: %+v", result)
	}
	if len(result.Entries) == 0 {
		t.Fatal("expected at least one valid pack entry in library")
	}

	// Non-existent directory should return Failed
	missingResult := app.OpenProfileLibrary(root, filepath.Join(root, "non-existent"))
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

	// Validate valid profile
	valResult := app.ValidateProfile(desktop.ProfileValidateRequest{
		Workspace: root,
		Document:  string(validDoc),
		Pack:      string(packDoc),
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
		Pack:      string(packDoc),
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
	packPath := filepath.Join(root, "pack.json")
	if err := os.WriteFile(packPath, packDoc, 0600); err != nil {
		t.Fatal(err)
	}

	openResult := app.OpenProfile(root, "profile.json", "pack.json")
	if openResult.State != desktop.Completed || openResult.Profile == nil {
		t.Fatalf("expected OpenProfile to succeed: %+v", openResult)
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

	// Compute target seal for NowPin
	targetVal := app.ValidateProfile(desktop.ProfileValidateRequest{
		Workspace: root,
		Document:  modDoc,
	})
	if targetVal.Seal == nil {
		t.Fatalf("expected target seal: %+v", targetVal)
	}

	// Explicitly upgrade pin for test-reschedule.json
	upgradeResult := app.UpgradeProfilePin(desktop.ProfileUpgradePinRequest{
		Workspace:  root,
		References: "references.json",
		Test:       "test-reschedule.json",
		WasPin:     reschedule.Pinned,
		NowPin: profileversion.Pin{
			ID:      targetVal.Seal.Profile.ID,
			Version: targetVal.Seal.Profile.Version,
			SHA256:  targetVal.Seal.Content.SHA256,
		},
		Output: "references.json",
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
