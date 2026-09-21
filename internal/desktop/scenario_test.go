package desktop_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
)

func TestScenarioCatalogListsUnavailableEventsWithReasons(t *testing.T) {
	app := workspaceApp(t)
	result := app.ScenarioCatalog()
	if result.State != desktop.Completed || result.Catalog == nil {
		t.Fatalf("catalog: %+v", result)
	}
	if result.Catalog.GeneratorVersion != "readmit-scenario-generator-v1" {
		t.Fatalf("generator version: %q", result.Catalog.GeneratorVersion)
	}
	if len(result.Catalog.Profiles) != 4 {
		t.Fatalf("profiles: %d", len(result.Catalog.Profiles))
	}
	var sawUnavailable bool
	for _, event := range result.Catalog.AllEvents {
		if !event.Available && event.Reason != "" {
			sawUnavailable = true
			break
		}
	}
	if !sawUnavailable {
		t.Fatal("expected unavailable events with reasons")
	}
}

func TestPreviewScenarioMasksIdentifiersUntilDeliberatelyRevealed(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "scenario-siu.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scenario.json"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	masked := app.PreviewScenario(desktop.ScenarioPreviewRequest{Workspace: root, Document: "scenario.json"})
	if masked.State != desktop.Completed || len(masked.Subjects) == 0 {
		t.Fatalf("masked preview: %+v", masked)
	}
	for _, subject := range masked.Subjects {
		if !subject.Masked || subject.Identifier != "" || subject.Namespace != "" {
			t.Fatalf("identifiers must stay masked: %+v", subject)
		}
	}
	revealed := app.PreviewScenario(desktop.ScenarioPreviewRequest{
		Workspace: root, Document: "scenario.json", RevealSensitive: true,
	})
	if revealed.State != desktop.Completed {
		t.Fatalf("revealed preview: %+v", revealed)
	}
	var sawIdentity bool
	for _, subject := range revealed.Subjects {
		if !subject.Masked && subject.Identifier != "" {
			sawIdentity = true
		}
	}
	if !sawIdentity {
		t.Fatal("deliberate reveal must show identifiers")
	}
}

func TestPreviewScenarioRefusesUnsupportedEventsAndMalformedDocuments(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	bad := `{"schema":"readmit-scenario/v1","scenario":{"id":"x","version":"1"},"profile":"readmit-siu-lifecycle-v1","base_time":"2026-01-01T12:00:00Z","subjects":[{"id":"patient-a","kind":"patient","namespace":"READMIT","identifier":"A","initial_state":"active"},{"id":"appointment-a","kind":"appointment","namespace":"READMIT","identifier":"B","patient":"patient-a","initial_state":"none"}],"steps":[{"id":"admit","event":"A01","subject":"appointment-a","after":"0s","expect":"accepted"}]}`
	if got := app.PreviewScenario(desktop.ScenarioPreviewRequest{Workspace: root, Document: bad}); got.State != desktop.Failed || !strings.Contains(got.Reason, "declares no event") {
		t.Fatalf("unsupported event: %+v", got)
	}
	if got := app.PreviewScenario(desktop.ScenarioPreviewRequest{Workspace: root, Document: `{"not":"a scenario"}`}); got.State != desktop.Failed {
		t.Fatalf("malformed: %+v", got)
	}
	if got := app.SaveScenario(desktop.ScenarioSaveRequest{Workspace: root, Document: bad, Output: "bad.json"}); got.State != desktop.Failed {
		t.Fatalf("save unsupported: %+v", got)
	}
}

func TestSaveOpenScenarioRoundTripAndDuplicateNameRefusal(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "scenario-adt.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved := app.SaveScenario(desktop.ScenarioSaveRequest{Workspace: root, Document: string(fixture), Output: "adt.json"})
	if saved.State != desktop.Completed || saved.Profile == "" {
		t.Fatalf("save: %+v", saved)
	}
	opened := app.OpenScenario(root, "adt.json")
	if opened.State != desktop.Completed || opened.ID == "" || opened.Document == "" {
		t.Fatalf("open: %+v", opened)
	}
	dup := app.SaveScenario(desktop.ScenarioSaveRequest{Workspace: root, Document: string(fixture), Output: "adt.json"})
	if dup.State != desktop.Failed || !strings.Contains(dup.Reason, "already exists") {
		t.Fatalf("duplicate: %+v", dup)
	}
}

func TestGenerateScenarioWritesGeneratedCaseAndRegistersWithoutMutatingFamily(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	plan, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "scenario-generator.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan.json"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	first := app.GenerateScenario(desktop.ScenarioGenerateRequest{
		Workspace: root, Document: "plan.json", OutputName: "family-one",
		CaseName: "generated-one", RegisterInProject: true, CaseTitle: "Generated workflow",
	})
	if first.State != desktop.Completed || first.StreamCount == 0 || first.CaseName == "" || first.ProvenanceMode != "generated" || !first.Registered {
		t.Fatalf("generate: %+v", first)
	}
	if _, err := os.Stat(filepath.Join(root, "family-one", "generation.json")); err != nil {
		t.Fatal(err)
	}
	opened := app.OpenCase(root, first.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil || opened.Case.Provenance != "generated" {
		t.Fatalf("open generated case: %+v", opened)
	}
	again := app.GenerateScenario(desktop.ScenarioGenerateRequest{
		Workspace: root, Document: "plan.json", OutputName: "family-one", CaseName: "generated-two",
	})
	if again.State != desktop.Failed {
		t.Fatalf("overwrite family: %+v", again)
	}
	if got := app.GenerateScenario(desktop.ScenarioGenerateRequest{
		Workspace: root, Document: `{"schema":"readmit-scenario-generator/v1"}`, OutputName: "family-bad",
	}); got.State != desktop.Failed {
		t.Fatalf("malformed plan: %+v", got)
	}
}

func TestScenarioLibraryVersioningCompareImportExportAndCheck(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	plan, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "scenario-generator.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved := app.SaveScenarioLibraryEntry(desktop.ScenarioLibraryRequest{
		Workspace: root, Output: "library.json", TemplateID: "siu-appointment-lifecycle",
		TemplateVer: "1", Profile: "readmit-siu-lifecycle-v1", Plan: string(plan), Coverage: "baseline,desktop",
	})
	if saved.State != desktop.Completed || len(saved.Templates) != 1 {
		t.Fatalf("save library: %+v", saved)
	}
	// Same version with same plan refused; bump required to change.
	dup := app.SaveScenarioLibraryEntry(desktop.ScenarioLibraryRequest{
		Workspace: root, Library: "library.json", Output: "library.json",
		TemplateID: "siu-appointment-lifecycle", TemplateVer: "1",
		Profile: "readmit-siu-lifecycle-v1", Plan: string(plan), Coverage: "baseline",
	})
	if dup.State != desktop.Failed {
		t.Fatalf("duplicate revision: %+v", dup)
	}
	v2 := app.SaveScenarioLibraryEntry(desktop.ScenarioLibraryRequest{
		Workspace: root, Library: "library.json", Output: "library.json",
		TemplateID: "siu-appointment-lifecycle", TemplateVer: "2",
		Profile: "readmit-siu-lifecycle-v1", Plan: string(plan), Coverage: "baseline",
	})
	if v2.State != desktop.Completed || len(v2.Templates) != 2 {
		t.Fatalf("version bump: %+v", v2)
	}
	compared := app.CompareScenarioLibraryEntries(desktop.ScenarioLibraryRequest{
		Workspace: root, Library: "library.json", TemplateID: "siu-appointment-lifecycle",
		TemplateVer: "1", Expectations: "2",
	})
	if compared.State != desktop.Completed || len(compared.Compared) != 1 || !compared.Compared[0].SamePlan {
		t.Fatalf("compare: %+v", compared)
	}
	exported := app.ExportScenarioLibrary(desktop.ScenarioLibraryRequest{
		Workspace: root, Library: "library.json", Output: "library-export.json",
	})
	if exported.State != desktop.Completed {
		t.Fatalf("export: %+v", exported)
	}
	imported := app.ImportScenarioLibrary(desktop.ScenarioLibraryRequest{
		Workspace: root, Library: filepath.Join(root, "library-export.json"), Output: "library-import.json",
	})
	if imported.State != desktop.Completed || len(imported.Templates) != 2 {
		t.Fatalf("import: %+v", imported)
	}
	// Historical fixture round-trip.
	fixtureLib, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "scenario-library.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "historical-library.json"), fixtureLib, 0600); err != nil {
		t.Fatal(err)
	}
	fixtureExp, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "scenario-expectations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "historical-expectations.json"), fixtureExp, 0600); err != nil {
		t.Fatal(err)
	}
	opened := app.OpenScenarioLibrary(root, "historical-library.json")
	if opened.State != desktop.Completed || len(opened.Templates) == 0 {
		t.Fatalf("historical open: %+v", opened)
	}
	checked := app.CheckScenarioLibrary(desktop.ScenarioLibraryRequest{
		Workspace: root, Library: "historical-library.json", Expectations: "historical-expectations.json",
	})
	if checked.State != desktop.Completed || checked.Streams == 0 {
		t.Fatalf("historical check: %+v", checked)
	}
}

func TestGenerateScenarioBusyCancellationAndMissingProfile(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	plan, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "scenario-generator.json"))
	if err != nil {
		t.Fatal(err)
	}
	missing := strings.Replace(string(plan), `"profile": "readmit-siu-lifecycle-v1"`, `"profile": "readmit-missing-lifecycle-v1"`, 1)
	if got := app.GenerateScenario(desktop.ScenarioGenerateRequest{
		Workspace: root, Document: missing, OutputName: "missing-profile",
	}); got.State != desktop.Failed {
		t.Fatalf("missing profile: %+v", got)
	}

	done := make(chan struct{})
	started := make(chan struct{})
	c := &chooser{before: func() {
		close(started)
		<-done
	}}
	active := newApp(t, c)
	go func() { active.ChooseImportSources("folder") }()
	<-started
	busy := active.GenerateScenario(desktop.ScenarioGenerateRequest{
		Workspace: root, Document: string(plan), OutputName: "busy-family", CaseName: "busy-case",
	})
	close(done)
	if busy.State != desktop.Busy {
		t.Fatalf("expected busy while dialog holds the slot: %+v", busy)
	}
}

func TestGenerateSynthProducesDeclaredFamily(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	result := app.GenerateSynth(desktop.SynthGenerateRequest{
		Workspace: root, OutputName: "siu-family", Seed: 0,
		BaseTime: "2026-01-01T12:00:00Z", GeneratorVersion: "readmit-synth-v1", ProfileVersion: "readmit-siu-v1",
	})
	if result.State != desktop.Completed || len(result.Cases) == 0 {
		t.Fatalf("synth: %+v", result)
	}
	if got := app.GenerateSynth(desktop.SynthGenerateRequest{
		Workspace: root, OutputName: "siu-family", Seed: 0,
		BaseTime: "2026-01-01T12:00:00Z", GeneratorVersion: "readmit-synth-v1", ProfileVersion: "readmit-siu-v1",
	}); got.State != desktop.Failed {
		t.Fatalf("overwrite synth: %+v", got)
	}
}

func TestBindScenarioProfileUsesLocalProfileFamilyWithoutSubstitution(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "local-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pack.json"), pack, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profile.json"), profile, 0600); err != nil {
		t.Fatal(err)
	}
	bound := app.BindScenarioProfile(desktop.ScenarioProfileBindRequest{
		Workspace: root, Entry: "profile.json", PackEntry: "pack.json",
	})
	if bound.State != desktop.Completed || !bound.Available || bound.LifecycleProfile != "readmit-siu-lifecycle-v1" {
		t.Fatalf("bind: %+v", bound)
	}
}
