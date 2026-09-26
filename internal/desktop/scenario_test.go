package desktop_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"github.com/bharm16/readmit/internal/synth"
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
		Workspace: root, OutputName: "siu-family", Seed: "0",
		BaseTime: "2026-01-01T12:00:00Z", GeneratorVersion: "readmit-synth-v1", ProfileVersion: "readmit-siu-v1",
	})
	if result.State != desktop.Completed || len(result.Cases) == 0 {
		t.Fatalf("synth: %+v", result)
	}
	if got := app.GenerateSynth(desktop.SynthGenerateRequest{
		Workspace: root, OutputName: "siu-family", Seed: "0",
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

// The window declares every input `readmit synth` requires and reads each the
// way the command does: the seed as the command's flag library reads an
// unsigned number, up to 2^64-1, and the base time through the shared
// declaration. A refused input writes nothing, and a written family reports
// each case bundle's identity from its completion record.
func TestGenerateSynthReadsEveryDeclaredInputAsTheCommandDoes(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	declared := func(output, seed, base, generator, profile string) desktop.SynthGenerateRequest {
		return desktop.SynthGenerateRequest{Workspace: root, OutputName: output, Seed: seed, BaseTime: base,
			GeneratorVersion: generator, ProfileVersion: profile}
	}
	const base = "2026-01-01T12:00:00Z"
	seedRefusal := "the seed must be a whole number from 0 to 18446744073709551615"
	for name, tc := range map[string]struct {
		request desktop.SynthGenerateRequest
		reason  string
	}{
		"no seed":                  {declared("refused", "", base, "readmit-synth-v1", "readmit-siu-v1"), seedRefusal},
		"a negative seed":          {declared("refused", "-1", base, "readmit-synth-v1", "readmit-siu-v1"), seedRefusal},
		"a seed past 2^64-1":       {declared("refused", "18446744073709551616", base, "readmit-synth-v1", "readmit-siu-v1"), seedRefusal},
		"a fractional seed":        {declared("refused", "1.5", base, "readmit-synth-v1", "readmit-siu-v1"), seedRefusal},
		"a base time with no zone": {declared("refused", "0", "2026-01-01T12:00:00", "readmit-synth-v1", "readmit-siu-v1"), operation.ErrBaseTime.Error()},
		"a fractional base time":   {declared("refused", "0", "2026-01-01T12:00:00.5Z", "readmit-synth-v1", "readmit-siu-v1"), operation.ErrBaseTime.Error()},
		"an unimplemented generator": {declared("refused", "0", base, "readmit-synth-v2", "readmit-siu-v1"),
			"unsupported generator version; supported: readmit-synth-v1"},
		"an unsupported profile": {declared("refused", "0", base, "readmit-synth-v1", "readmit-siu-v2"),
			"unsupported profile version; supported: readmit-siu-v1"},
		"no output": {declared("", "0", base, "readmit-synth-v1", "readmit-siu-v1"), "synth requires a new output directory name"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := app.GenerateSynth(tc.request); got.State != desktop.Failed || got.Reason != tc.reason || len(got.Variants) != 0 {
				t.Fatalf("refusal: %+v, want %q", got, tc.reason)
			}
			if entries := entriesOf(t, root); len(entries) != 0 {
				t.Fatalf("a refused generation wrote %v", entries)
			}
		})
	}

	largest := app.GenerateSynth(declared("largest", "18446744073709551615", "2026-01-01T12:00:00+02:00", "readmit-synth-v1", "readmit-siu-v1"))
	if largest.State != desktop.Completed || len(largest.Variants) != 3 {
		t.Fatalf("largest seed: %+v", largest)
	}
	var family synth.Manifest
	if err := json.Unmarshal(mustRead(t, filepath.Join(root, "largest", "family.json")), &family, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if family.Generator.Seed != ^uint64(0) || !family.Generator.BaseTime.Equal(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("the family records other inputs than were declared: %+v", family.Generator)
	}
	for i, variant := range largest.Variants {
		recorded := family.Cases[i]
		if variant.Variant != recorded.Variant || variant.Path != recorded.Path || variant.Identity != recorded.Identity || variant.KnownDefect != recorded.KnownDefect {
			t.Fatalf("variant %d disagrees with the completion record: %+v vs %+v", i, variant, recorded)
		}
	}
	if largest.Variants[2].Variant != "invalid" || largest.Variants[2].KnownDefect == "" {
		t.Fatalf("the known-invalid case does not name its defect: %+v", largest.Variants)
	}
}

// wideLibrary is the shipped cancel-then-book template regenerated as the
// widest plan a library holds, eight rows by sixteen variants, with
// expectations that pin it and cover only its first stream. The check
// regenerates all 128 streams in memory before it compares anything, so the
// failure it answers is the coverage one, and it can never pass.
func wideLibrary(t *testing.T) (library, expectations string) {
	t.Helper()
	var shipped scenariolibrary.Library
	if err := json.Unmarshal([]byte(fixture(t, "scenario-library.json")), &shipped); err != nil {
		t.Fatal(err)
	}
	var plan map[string]jsontext.Value
	if err := json.Unmarshal(shipped.Templates[0].Plan, &plan); err != nil {
		t.Fatal(err)
	}
	var rows, variants []map[string]jsontext.Value
	if err := json.Unmarshal(plan["rows"], &rows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plan["variants"], &variants); err != nil {
		t.Fatal(err)
	}
	var wideRows, wideVariants []map[string]jsontext.Value
	for i := range 8 {
		row := maps.Clone(rows[0])
		row["id"] = jsontext.Value(fmt.Sprintf(`"row-%d"`, i+1))
		wideRows = append(wideRows, row)
	}
	for i := range 16 {
		variant := maps.Clone(variants[0])
		variant["id"] = jsontext.Value(fmt.Sprintf(`"variant-%d"`, i+1))
		wideVariants = append(wideVariants, variant)
	}
	var err error
	if plan["rows"], err = json.Marshal(wideRows); err != nil {
		t.Fatal(err)
	}
	if plan["variants"], err = json.Marshal(wideVariants); err != nil {
		t.Fatal(err)
	}
	wide := shipped.Templates[0]
	wide.ID, wide.Coverage = "siu-cancel-book-wide", []string{"wide"}
	if wide.Plan, err = json.Marshal(plan); err != nil {
		t.Fatal(err)
	}
	digest, err := scenariolibrary.PlanDigest(wide.Plan)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(scenariolibrary.Library{Schema: shipped.Schema, Templates: []scenariolibrary.Template{wide}})
	if err != nil {
		t.Fatal(err)
	}
	var oracle scenariolibrary.Expectations
	if err := json.Unmarshal([]byte(fixture(t, "scenario-expectations.json")), &oracle); err != nil {
		t.Fatal(err)
	}
	oracle.Template, oracle.PlanSHA256, oracle.Streams = wide.ID, digest, oracle.Streams[:1]
	pinned, err := json.Marshal(oracle)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded), string(pinned)
}

// A check regenerates in memory, so it writes nothing anywhere: the widest
// plan a library holds is regenerated whole, answered in the reader's words
// when the oracle does not cover every generated stream, and the temporary
// folder the process shares stays empty through the failing check, a passing
// one and a repeat of each.
func TestCheckScenarioLibraryWritesNothingAndAnswersAsTheReaderDoes(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	app := workspaceApp(t)
	root := t.TempDir()
	library, expectations := wideLibrary(t)
	writeDocument(t, root, "wide-library.json", library)
	writeDocument(t, root, "wide-expectations.json", expectations)
	check := func(libraryName, expectationsName string) desktop.ScenarioLibraryResult {
		return app.CheckScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: libraryName, Expectations: expectationsName})
	}
	if got := check("wide-library.json", "wide-expectations.json"); got.State != desktop.Failed ||
		got.Reason != "oracle must cover every generated stream" || got.Streams != 0 || got.Fields != 0 {
		t.Fatalf("a check of the widest plan against an oracle of its first stream: %+v", got)
	}
	if left := entriesOf(t, temporary); len(left) != 0 {
		t.Fatalf("a failing check wrote %v", left)
	}
	writeDocument(t, root, "shipped-library.json", fixture(t, "scenario-library.json"))
	writeDocument(t, root, "expectations.json", fixture(t, "scenario-expectations.json"))
	for i := range 2 {
		if got := check("shipped-library.json", "expectations.json"); got.State != desktop.Completed ||
			got.Streams != 2 || got.Fields != 11 || got.Target != "unverified" {
			t.Fatalf("passing check %d: %+v", i, got)
		}
		if got := check("wide-library.json", "wide-expectations.json"); got.State != desktop.Failed ||
			got.Reason != "oracle must cover every generated stream" {
			t.Fatalf("failing check %d: %+v", i, got)
		}
		if left := entriesOf(t, temporary); len(left) != 0 {
			t.Fatalf("check %d wrote %v", i, left)
		}
	}
}

// Every library the window opens, saves, exports or imports is read by the
// reader `readmit scenario check-library` uses, so the window never writes a
// library the command refuses and refuses the others in the command's words,
// with nothing written. A library or expectations document past the plan
// bound but inside the command's 4 MiB is read, as the command reads it.
func TestScenarioLibraryDocumentsAreHeldToTheCommandsReader(t *testing.T) {
	t.Parallel()
	app := workspaceApp(t)
	root := t.TempDir()
	plan := fixture(t, "scenario-generator.json")
	writeDocument(t, root, "plan.json", plan)
	saved := func(request desktop.ScenarioLibraryRequest) desktop.ScenarioLibraryResult {
		request.Workspace, request.Plan = root, "plan.json"
		if request.TemplateID == "" {
			request.TemplateID = "siu-appointment-lifecycle"
		}
		if request.Profile == "" {
			request.Profile = "readmit-siu-lifecycle-v1"
		}
		return app.SaveScenarioLibraryEntry(request)
	}
	for name, tc := range map[string]struct {
		request desktop.ScenarioLibraryRequest
		reason  string
	}{
		"a profile the plan does not preview on": {desktop.ScenarioLibraryRequest{Output: "refused.json", TemplateVer: "1", Profile: "readmit-adt-lifecycle-v1"},
			"template profile differs from plan"},
		"a template id outside the library's names": {desktop.ScenarioLibraryRequest{Output: "refused.json", TemplateID: "not an id", TemplateVer: "1"},
			"invalid or duplicate template identity"},
		"a repeated coverage tag": {desktop.ScenarioLibraryRequest{Output: "refused.json", TemplateVer: "1", Coverage: "baseline, baseline"},
			"invalid or duplicate coverage tag"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := saved(tc.request); got.State != desktop.Failed || got.Reason != tc.reason {
				t.Fatalf("save: %+v, want %q", got, tc.reason)
			}
			if _, err := os.Stat(filepath.Join(root, "refused.json")); !os.IsNotExist(err) {
				t.Fatalf("a refused save wrote the library: %v", err)
			}
		})
	}

	// Sixteen revisions fill a library; the seventeenth is refused and the
	// library keeps the sixteen it had.
	for version := 1; version <= 16; version++ {
		library := "full.json"
		if version == 1 {
			library = ""
		}
		if got := saved(desktop.ScenarioLibraryRequest{Library: library, Output: "full.json", TemplateVer: fmt.Sprint(version)}); got.State != desktop.Completed {
			t.Fatalf("revision %d: %+v", version, got)
		}
	}
	full := mustRead(t, filepath.Join(root, "full.json"))
	if got := saved(desktop.ScenarioLibraryRequest{Library: "full.json", Output: "full.json", TemplateVer: "17"}); got.State != desktop.Failed || got.Reason != "library requires 1 to 16 templates" {
		t.Fatalf("seventeenth revision: %+v", got)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(root, "full.json")), full) {
		t.Fatal("a refused seventeenth revision changed the library")
	}

	// A library the command refuses is refused on open, export and import,
	// in the reader's words, and neither export nor import writes it.
	var shipped scenariolibrary.Library
	if err := json.Unmarshal([]byte(fixture(t, "scenario-library.json")), &shipped); err != nil {
		t.Fatal(err)
	}
	shipped.Templates[0].Profile = "readmit-adt-lifecycle-v1"
	falseProfile, err := json.Marshal(shipped)
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "false-profile.json", string(falseProfile))
	outside := filepath.Join(t.TempDir(), "false-profile.json")
	if err := os.WriteFile(outside, falseProfile, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]desktop.ScenarioLibraryResult{
		"open":   app.OpenScenarioLibrary(root, "false-profile.json"),
		"export": app.ExportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: "false-profile.json", Output: "exported.json"}),
		"import": app.ImportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: outside, Output: "imported.json"}),
		"add to": saved(desktop.ScenarioLibraryRequest{Library: "false-profile.json", Output: "false-profile.json", TemplateVer: "1"}),
	} {
		if got.State != desktop.Failed || got.Reason != "template profile differs from plan" {
			t.Errorf("%s a library the command refuses: %+v", name, got)
		}
	}
	for _, name := range []string{"exported.json", "imported.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s was written from a refused library: %v", name, err)
		}
	}
	if !bytes.Equal(mustRead(t, filepath.Join(root, "false-profile.json")), falseProfile) {
		t.Error("adding to a refused library changed it")
	}

	// The library to import comes from elsewhere on the machine and is named
	// by its absolute path; a relative one is refused before it is read.
	writeDocument(t, root, "shipped-library.json", fixture(t, "scenario-library.json"))
	if got := app.ImportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: "shipped-library.json", Output: "imported.json"}); got.State != desktop.Failed ||
		got.Reason != "the library to import is named by its absolute path" {
		t.Fatalf("a relative import: %+v", got)
	}

	// Documents past the plan bound, inside the command's 4 MiB.
	padding := strings.Repeat(" ", 300<<10)
	writeDocument(t, root, "padded-library.json", fixture(t, "scenario-library.json")+padding)
	writeDocument(t, root, "padded-expectations.json", fixture(t, "scenario-expectations.json")+padding)
	checked := app.CheckScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: "padded-library.json", Expectations: "padded-expectations.json"})
	if checked.State != desktop.Completed || checked.Streams != 2 || checked.Fields != 11 || checked.Target != "unverified" {
		t.Fatalf("a check of documents past the plan bound: %+v", checked)
	}
}

// Binding reads the local profile with the reader every local profile is read
// with and pins the lifecycle profile its message family selects. A profile
// that reader refuses — an unsupported family among them — is refused in its
// words and pins nothing; nothing is substituted for it.
func TestBindScenarioProfileRefusesWhatTheLocalProfileReaderRefuses(t *testing.T) {
	app := workspaceApp(t)
	root := t.TempDir()
	profile := fixture(t, "local-profile.json")
	for name, document := range map[string]string{
		"an unsupported family": strings.Replace(profile, `"family": "SIU"`, `"family": "MDM"`, 1),
		"an unknown member":     strings.Replace(profile, `"profile": {`, `"unexpected": true, "profile": {`, 1),
		"another schema":        strings.Replace(profile, `"readmit-local-profile/v1"`, `"readmit-local-profile/v2"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if document == profile {
				t.Fatal("the mutation did not apply")
			}
			writeDocument(t, root, "refused.json", document)
			_, want := localprofile.Decode([]byte(document))
			got := app.BindScenarioProfile(desktop.ScenarioProfileBindRequest{Workspace: root, Entry: "refused.json"})
			if want == nil || got.State != desktop.Failed || got.Reason != want.Error() || got.LifecycleProfile != "" || got.Available {
				t.Fatalf("bind: %+v, want the reader's %v", got, want)
			}
		})
	}
	for family, lifecycle := range map[string]string{
		"ADT": "readmit-adt-lifecycle-v1", "SIU": "readmit-siu-lifecycle-v1",
		"ORM": "readmit-orm-lifecycle-v1", "ORU": "readmit-oru-lifecycle-v1",
	} {
		document := strings.Replace(profile, `"family": "SIU"`, `"family": "`+family+`"`, 1)
		decoded, err := localprofile.Decode([]byte(document))
		if err != nil {
			t.Fatalf("the %s profile: %v", family, err)
		}
		writeDocument(t, root, family+".json", document)
		got := app.BindScenarioProfile(desktop.ScenarioProfileBindRequest{Workspace: root, Entry: family + ".json"})
		if got.State != desktop.Completed || !got.Available || got.LifecycleProfile != lifecycle || got.Family != family ||
			got.ProfileID != decoded.Identity.ID || got.ProfileVersion != decoded.Identity.Version || got.GeneratorVersion != "readmit-scenario-generator-v1" {
			t.Fatalf("bind %s: %+v", family, got)
		}
	}
}

// The library to import is chosen in the host's file dialog and answered as
// the full path the import reads, which then copies exactly those bytes; a
// dismissed dialog chooses nothing and two files are refused.
func TestChooseScenarioLibraryImportAnswersThePathTheImportReads(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "shipped-library.json")
	if err := os.WriteFile(outside, []byte(fixture(t, "scenario-library.json")), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &chooser{files: []string{outside}}
	app := newApp(t, c)
	root := t.TempDir()
	chosen := app.ChooseScenarioLibraryImport()
	if chosen.State != desktop.Completed || chosen.Path != outside {
		t.Fatalf("chosen: %+v", chosen)
	}
	if got := strings.Join(c.titles, "|"); got != "Choose the scenario library to import" {
		t.Fatalf("dialog title: %s", got)
	}
	imported := app.ImportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: chosen.Path, Output: "imported.json"})
	if imported.State != desktop.Completed {
		t.Fatalf("import of the chosen library: %+v", imported)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(root, "imported.json")), mustRead(t, outside)) {
		t.Fatal("the import is not the chosen library's bytes")
	}
	c.files = []string{outside, outside}
	if got := app.ChooseScenarioLibraryImport(); got.State != desktop.Failed || got.Path != "" || got.Reason != "choose exactly one file" {
		t.Fatalf("two files: %+v", got)
	}
	c.files = nil
	if got := app.ChooseScenarioLibraryImport(); got.State != desktop.Cancelled || got.Path != "" {
		t.Fatalf("dismissed: %+v", got)
	}
}
