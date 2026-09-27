package tests

// The Library's scenarios and profiles are the command line's documents: a
// case created from a saved scenario is `readmit scenario generate`'s
// generation of that plan, a preview of a draft steps through the workflow
// `readmit scenario preview` shows, and a profile package the Library exports
// or imports is one `readmit profile export` and `readmit profile import`
// write and read alike.

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepackage"
)

// libraryProject is a new named project of an activated window.
func libraryProject(t *testing.T) (*desktop.App, desktop.RequestContext) {
	t.Helper()
	app := desktopApp(t, t.TempDir())
	created := app.CreateNamedProject(desktop.NewProjectRequest{Name: "Scheduling QA", Location: app.ChooseProjectLocation().Location})
	if created.State != desktop.Completed {
		t.Fatalf("create: %+v", created)
	}
	return app, created.Context
}

// refusedAs is the sentence the command line printed when it refused: its
// stderr without the program name and the line end.
func refusedAs(stderr string) string {
	return strings.TrimSuffix(strings.TrimPrefix(stderr, "readmit: "), "\n")
}

// A case created from a saved Library scenario is generated from exactly the
// saved plan: its generation record is the one `readmit scenario generate`
// writes for the plan exported from the Library, byte for byte.
func TestACaseCreatedFromALibraryScenarioIsTheCommandLinesGeneration(t *testing.T) {
	app, context := libraryProject(t)
	root := context.Project
	imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ScenarioItem,
		Path: filepath.Join(resolvedFixtures(t), "scenario-generator.json")})
	if imported.State != desktop.Completed {
		t.Fatalf("import: %+v", imported)
	}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: *imported.Draft, IntentID: "scenario-1"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	plan := filepath.Join(t.TempDir(), "plan.json")
	if exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *saved.Saved, Destination: plan}); exported.State != desktop.Completed {
		t.Fatalf("export: %+v", exported)
	}
	command := filepath.Join(t.TempDir(), "family")
	if _, stderr, err := run(t, "scenario", "generate", plan, "--output", command); err != nil || stderr != "" {
		t.Fatalf("scenario generate: %v %s", err, stderr)
	}
	created := app.CreateScenarioCase(desktop.ScenarioCaseRequest{Context: context, Scenario: *saved.Saved, IntentID: "case-1"})
	if created.State != desktop.Completed || created.Case == nil {
		t.Fatalf("create case: %+v", created)
	}
	window := mustRead(t, filepath.Join(root, strings.TrimSuffix(created.Entry, "-case")+"-generation", "generation.json"))
	if !bytes.Equal(window, mustRead(t, filepath.Join(command, "generation.json"))) {
		t.Fatal("the window's generation record is not the command line's for the same plan")
	}
	for _, item := range app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem}).Page.Items {
		if item.Ref.ID == created.Case.ID && (item.Summary.Case.Provenance != "synthetic" || item.Summary.Case.Scenario == nil || item.Summary.Case.Scenario.Ref != *saved.Saved) {
			t.Fatalf("the created case: %+v", item.Summary.Case)
		}
	}
}

// A draft's preview steps through its workflow as `readmit scenario preview`
// shows that workflow: the baseline variant's messages are the steps it
// lists, in its order, at its times and with its expectations.
func TestAScenarioPreviewStepsThroughTheWorkflowTheCommandLinePreviews(t *testing.T) {
	app, context := libraryProject(t)
	draft := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: desktop.ItemRef{Kind: desktop.ScenarioItem}}).Draft
	// The scenario is named as a person names it before previewing.
	draft.Name, draft.Scenario.Template.Scenario.ID = "Reschedule", "reschedule"
	previewed := app.PreviewScenarioDraft(desktop.DraftRequest{Context: context, Kind: desktop.ScenarioItem, Draft: *draft})
	if previewed.State != desktop.Completed || previewed.Origin != "synthetic" {
		t.Fatalf("preview: %+v", previewed)
	}
	template, err := json.Marshal(draft.Scenario.Template)
	if err != nil {
		t.Fatal(err)
	}
	workflow := writeDocument(t, t.TempDir(), "workflow.json", string(template))
	stdout, stderr, err := run(t, "scenario", "preview", workflow)
	if err != nil || stderr != "" {
		t.Fatalf("scenario preview: %v %s", err, stderr)
	}
	var listed [][3]string
	_, table, _ := strings.Cut(stdout, "\n\n")
	lines := strings.Split(strings.TrimRight(table, "\n"), "\n")
	expected := strings.Index(lines[0], "Expected")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		listed = append(listed, [3]string{fields[1], fields[2], strings.TrimSpace(line[expected : expected+8])})
	}
	var shown [][3]string
	for _, message := range previewed.Messages {
		if message.Variant == "baseline" {
			shown = append(shown, [3]string{message.At, message.Event, message.Expect})
		}
	}
	if len(listed) == 0 || !slicesEqual(shown, listed) {
		t.Fatalf("the window previewed %v; the command line lists %v", shown, listed)
	}
}

// A profile package exported from the Library is one the command line imports,
// holding the documents `readmit profile export` packages for the same
// profile, pack, seal and origin; a package the command line refuses, the
// Library refuses in its words and nothing is written.
func TestAProfilePackageMovesBetweenTheLibraryAndTheCommandLine(t *testing.T) {
	app, context := libraryProject(t)
	root := context.Project
	fixtures := resolvedFixtures(t)
	writeDocument(t, root, "siu-pack.json", string(mustRead(t, filepath.Join(fixtures, "profile-pack.json"))))
	packs := app.MetadataPacks(context)
	if len(packs.Packs) != 1 {
		t.Fatalf("packs: %+v", packs)
	}
	profile, err := localprofile.Decode(mustRead(t, filepath.Join(fixtures, "local-profile.json")))
	if err != nil {
		t.Fatal(err)
	}
	origin, err := profilepackage.DecodeOrigin(mustRead(t, filepath.Join(fixtures, "profile-origin.json")))
	if err != nil {
		t.Fatal(err)
	}
	pack := packs.Packs[0].Item.Ref
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ProfileItem,
		Draft: desktop.ItemDraft{Name: "Scheduling profile", Profile: &desktop.ProfileDraft{Profile: profile, Pack: &pack, Origin: &origin}}, IntentID: "profile-1"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	window := filepath.Join(t.TempDir(), "window-package.json")
	if exported := app.ExportLibraryItem(desktop.LibraryExportRequest{Context: context, Ref: *saved.Saved, Destination: window}); exported.State != desktop.Completed {
		t.Fatalf("export: %+v", exported)
	}
	command := filepath.Join(t.TempDir(), "command-package.json")
	if _, stderr, err := run(t, "profile", "export", filepath.Join(fixtures, "local-profile.json"), "--pack", filepath.Join(fixtures, "profile-pack.json"),
		"--version", filepath.Join(fixtures, "profile-version.json"), "--origin", filepath.Join(fixtures, "profile-origin.json"), "--output", command, "--reviewed"); err != nil {
		t.Fatalf("profile export: %v %s", err, stderr)
	}
	documents := func(path string) map[string]any {
		t.Helper()
		verified, err := profilepackage.Decode(mustRead(t, path))
		if err != nil {
			t.Fatal(err)
		}
		held, err := verified.Documents()
		if err != nil {
			t.Fatal(err)
		}
		read := map[string]any{}
		for name, data := range held {
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			read[name] = value
		}
		return read
	}
	if got, want := documents(window), documents(command); !jsonEqual(t, got, want) {
		t.Fatalf("the Library packaged other documents than the command line:\n%v\n%v", got, want)
	}
	if _, stderr, err := run(t, "profile", "import", window, "--output", filepath.Join(t.TempDir(), "imported")); err != nil {
		t.Fatalf("the command line refused the Library's package: %v %s", err, stderr)
	}
	imported := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: command})
	if imported.State != desktop.Completed || imported.Draft.Profile.Origin == nil || imported.Draft.Profile.Profile.Identity != profile.Identity {
		t.Fatalf("the Library refused the command line's package: %+v", imported)
	}

	tampered := writeDocument(t, t.TempDir(), "tampered.json", strings.Replace(string(mustRead(t, command)), `"sha256":"`, `"sha256":"0`, 1))
	_, stderr, err := run(t, "profile", "import", tampered, "--output", filepath.Join(t.TempDir(), "never"))
	if err == nil {
		t.Fatal("the command line imported a tampered package")
	}
	before := entriesUnder(t, root)
	refused := app.ImportLibraryItem(desktop.LibraryImportRequest{Context: context, Kind: desktop.ProfileItem, Path: tampered})
	if refused.State != desktop.Failed || refused.Reason != refusedAs(stderr) {
		t.Fatalf("the Library answered %q; the command line %q", refused.Reason, refusedAs(stderr))
	}
	if after := entriesUnder(t, root); !slicesEqual(after, before) {
		t.Fatalf("a refused import wrote %v", after)
	}
}

// resolvedFixtures is the absolute folder of the shipped fixtures.
func resolvedFixtures(t *testing.T) string {
	t.Helper()
	folder, err := filepath.Abs(filepath.Join("..", "testdata", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	return folder
}

// entriesUnder names every entry of a folder.
func entriesUnder(t *testing.T, folder string) []string {
	t.Helper()
	listed, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range listed {
		names = append(names, entry.Name())
	}
	return names
}

func slicesEqual[T comparable](x, y []T) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// jsonEqual compares two decoded documents by their canonical encoding.
func jsonEqual(t *testing.T, x, y any) bool {
	t.Helper()
	a, err := json.Marshal(x, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(y, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(a, b)
}
