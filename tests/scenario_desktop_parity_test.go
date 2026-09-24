package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"github.com/bharm16/readmit/internal/synth"
)

// Desktop scenario generation and `readmit scenario generate` must retain the
// same generation.json for the same plan. The generated case is a desktop
// handoff artifact; stream bytes and the generation record stay CLI-identical.
func TestDesktopScenarioGenerateMatchesCLI(t *testing.T) {
	planPath := filepath.Join("..", "testdata", "fixtures", "scenario-generator.json")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}

	cliRoot := t.TempDir()
	cliFamily := filepath.Join(cliRoot, "family")
	if _, err := scenariogen.Write(t.Context(), cliFamily, plan); err != nil {
		t.Fatal(err)
	}
	cliGen, err := os.ReadFile(filepath.Join(cliFamily, "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := run(t, "scenario", "generate", planPath, "--output", filepath.Join(cliRoot, "cli-family")); err != nil || stderr != "" {
		t.Fatalf("cli generate: %v %s", err, stderr)
	}
	cliCmdGen, err := os.ReadFile(filepath.Join(cliRoot, "cli-family", "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(cliGen) != string(cliCmdGen) {
		t.Fatal("scenariogen.Write and readmit scenario generate disagreed")
	}

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "plan.json"), plan, 0600); err != nil {
		t.Fatal(err)
	}
	app := desktopApp(t, workspace)
	got := app.GenerateScenario(desktop.ScenarioGenerateRequest{
		Workspace:  workspace,
		Document:   "plan.json",
		OutputName: "family",
		CaseName:   "generated-case",
	})
	if got.State != desktop.Completed || got.ProvenanceMode != "generated" {
		t.Fatalf("desktop generate: %+v", got)
	}
	deskGen, err := os.ReadFile(filepath.Join(workspace, "family", "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(deskGen) != string(cliGen) {
		t.Fatal("desktop generation.json disagrees with CLI for the same plan")
	}
	opened := app.OpenCase(workspace, "generated-case")
	if opened.State != desktop.Completed || opened.Case == nil || opened.Case.Provenance != "generated" {
		t.Fatalf("generated case: %+v", opened)
	}
}

// refusedAs is the sentence the command line printed when it refused: its
// stderr without the program name and the line end.
func refusedAs(stderr string) string {
	return strings.TrimSuffix(strings.TrimPrefix(stderr, "readmit: "), "\n")
}

// The window's SIU fixture generation is `readmit synth`: the same declared
// inputs write the same family, file for file and byte for byte, the window
// names each case bundle by the identity the command prints, a seed is read
// as the command's flag reads it, and every input the command refuses is
// refused in the command's words with nothing written by either.
func TestDesktopSynthMatchesTheCommandLine(t *testing.T) {
	commandRoot, workspace := t.TempDir(), t.TempDir()
	app := desktopApp(t, workspace)
	command := func(output, seed, base, generator, profile string) []string {
		return []string{"synth", "--seed", seed, "--base-time", base, "--generator-version", generator,
			"--profile-version", profile, "--output", filepath.Join(commandRoot, output)}
	}
	window := func(output, seed, base, generator, profile string) desktop.SynthGenerateResult {
		return app.GenerateSynth(desktop.SynthGenerateRequest{Workspace: workspace, OutputName: output, Seed: seed,
			BaseTime: base, GeneratorVersion: generator, ProfileVersion: profile})
	}
	for i, tc := range []struct {
		seed, base string
		recorded   uint64
	}{
		{"0", "2026-01-01T12:00:00Z", 0},
		// The command's flag reads a leading zero as octal, so the window
		// does too: one spelling is one seed in both places.
		{"010", "2026-01-02T03:04:05+02:00", 8},
		{"18446744073709551615", "9999-12-29T23:00:00Z", ^uint64(0)},
	} {
		output := fmt.Sprintf("family-%d", i)
		stdout, stderr, err := run(t, command(output, tc.seed, tc.base, "readmit-synth-v1", "readmit-siu-v1")...)
		if err != nil || stderr != "" {
			t.Fatalf("synth --seed %s: %v %s", tc.seed, err, stderr)
		}
		written := window(output, tc.seed, tc.base, "readmit-synth-v1", "readmit-siu-v1")
		if written.State != desktop.Completed || filepath.Base(written.OutputPath) != output || len(written.Variants) != 3 {
			t.Fatalf("window synth --seed %s: %+v", tc.seed, written)
		}
		commandTree, windowTree := treeOf(t, filepath.Join(commandRoot, output)), treeOf(t, filepath.Join(workspace, output))
		if !reflect.DeepEqual(commandTree, windowTree) {
			t.Fatalf("seed %s: the window's family is not the command's, file for file and byte for byte", tc.seed)
		}
		for _, variant := range written.Variants {
			if !strings.Contains(stdout, variant.Variant+": "+variant.Identity+"\n") {
				t.Fatalf("seed %s: the window names %s by %s, which the command did not print:\n%s", tc.seed, variant.Variant, variant.Identity, stdout)
			}
		}
		if family := readStrictDocument[synth.Manifest](t, filepath.Join(workspace, output, "family.json")); family.Generator.Seed != tc.recorded {
			t.Fatalf("seed %s was recorded as %d", tc.seed, family.Generator.Seed)
		}
	}

	// The frozen readmit-synth-v1 vector is unchanged through the window.
	// These identities are recorded in docs/synth-v1-vector.md and were
	// authored independently of synth; never refresh them from its output.
	frozen := map[string]string{
		"regression":   "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",
		"cancellation": "96077b34226faa19325f01f3fbb0d728de88644c22ba8428659c883703d3f438",
		"invalid":      "ab6d014aa0fc9e2ed9cba7160e73bba17b8753f6a7ca5c3a8618f3ec8ba095a9",
	}
	for variant, identity := range frozen {
		if b := openSynthCase(t, filepath.Join(workspace, "family-0"), variant); b.Identity != identity {
			t.Errorf("the window changed the frozen %s identity: %s", variant, b.Identity)
		}
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(treeOf(t, filepath.Join(workspace, "family-0"))["family.json"])); got != "33b99fe63076f1ee3b9921e1753b6be66edc25cc044c339b356a816838608eda" {
		t.Errorf("the window changed the frozen family record: %s", got)
	}

	before := map[string]map[string][]byte{"command": treeOf(t, commandRoot), "window": treeOf(t, workspace)}
	for name, tc := range map[string]struct{ output, seed, base, generator, profile string }{
		"a base time with no zone":     {"refused", "0", "2026-01-01T12:00:00", "readmit-synth-v1", "readmit-siu-v1"},
		"a fractional base time":       {"refused", "0", "2026-01-01T12:00:00.5Z", "readmit-synth-v1", "readmit-siu-v1"},
		"a scenario past year 9999":    {"refused", "0", "9999-12-31T00:00:00Z", "readmit-synth-v1", "readmit-siu-v1"},
		"an unimplemented generator":   {"refused", "0", "2026-01-01T12:00:00Z", "readmit-synth-v2", "readmit-siu-v1"},
		"an unsupported profile":       {"refused", "0", "2026-01-01T12:00:00Z", "readmit-synth-v1", "readmit-siu-v2"},
		"a family that already exists": {"family-0", "0", "2026-01-01T12:00:00Z", "readmit-synth-v1", "readmit-siu-v1"},
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, err := run(t, command(tc.output, tc.seed, tc.base, tc.generator, tc.profile)...)
			refused := window(tc.output, tc.seed, tc.base, tc.generator, tc.profile)
			if err == nil || stdout != "" || refused.State != desktop.Failed || refused.Reason != refusedAs(stderr) || len(refused.Variants) != 0 {
				t.Fatalf("the command refused with %q (%v); the window answered %+v", stderr, err, refused)
			}
		})
	}
	// The command refuses a seed that is not a number as a misuse of its
	// flag and the window in its own words; neither writes anything.
	if _, _, err := run(t, command("refused", "seven", "2026-01-01T12:00:00Z", "readmit-synth-v1", "readmit-siu-v1")...); err == nil {
		t.Fatal("the command accepted a seed that is not a number")
	}
	if refused := window("refused", "seven", "2026-01-01T12:00:00Z", "readmit-synth-v1", "readmit-siu-v1"); refused.State != desktop.Failed {
		t.Fatalf("the window accepted a seed that is not a number: %+v", refused)
	}
	if !reflect.DeepEqual(before["command"], treeOf(t, commandRoot)) || !reflect.DeepEqual(before["window"], treeOf(t, workspace)) {
		t.Fatal("a refused generation wrote or changed a family")
	}
}

// The window's fixture check is `readmit scenario check-library`: over the
// same library and expectations it reports the counts the command prints,
// refuses every oracle the command refuses in the command's words, reads
// documents up to the command's 4 MiB bound, and writes nothing.
func TestDesktopScenarioLibraryCheckMatchesTheCommandLine(t *testing.T) {
	workspace := t.TempDir()
	app := desktopApp(t, workspace)
	library, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", "scenario-library.json"))
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", "scenario-expectations.json"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		return name
	}
	check := func(libraryName, oracleName string) (string, string, error, desktop.ScenarioLibraryResult) {
		t.Helper()
		stdout, stderr, err := run(t, "scenario", "check-library", filepath.Join(workspace, libraryName), filepath.Join(workspace, oracleName))
		return stdout, stderr, err, app.CheckScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: libraryName, Expectations: oracleName})
	}
	passed := func(label string, stdout, stderr string, err error, got desktop.ScenarioLibraryResult) {
		t.Helper()
		if err != nil || stderr != "" || got.State != desktop.Completed ||
			stdout != fmt.Sprintf("Fixture checks passed: %d streams, %d fields. External target outcomes: %s.\n", got.Streams, got.Fields, got.Target) {
			t.Fatalf("%s: the command printed %q (%v %s); the window answered %+v", label, stdout, err, stderr, got)
		}
	}
	write("library.json", library)
	write("expectations.json", oracle)
	stdout, stderr, err, got := check("library.json", "expectations.json")
	passed("the shipped fixtures", stdout, stderr, err, got)
	if got.Streams != 2 || got.Fields != 11 || got.Target != "unverified" {
		t.Fatalf("the shipped fixtures: %+v", got)
	}

	// Documents past the generator plan's bound and inside the command's.
	padding := bytes.Repeat([]byte(" "), 300<<10)
	write("padded-library.json", append(append([]byte{}, library...), padding...))
	write("padded-expectations.json", append(append([]byte{}, oracle...), padding...))
	stdout, stderr, err, got = check("padded-library.json", "padded-expectations.json")
	passed("documents past 256 KiB", stdout, stderr, err, got)

	var falseProfile scenariolibrary.Library
	if err := json.Unmarshal(library, &falseProfile); err != nil {
		t.Fatal(err)
	}
	falseProfile.Templates[0].Profile = "readmit-adt-lifecycle-v1"
	encoded, err := json.Marshal(falseProfile)
	if err != nil {
		t.Fatal(err)
	}
	write("false-profile.json", encoded)
	for i, tc := range []struct{ name, library, old, new string }{
		{"a wrong field", "library.json", "5349555e533132", "5349555e533133"},
		{"a wrong lifecycle state", "library.json", `"to": "booked"`, `"to": "cancelled"`},
		{"a wrong field state", "library.json", `"state": "omitted"`, `"state": "empty"`},
		{"another template version", "library.json", `"template_version": "1"`, `"template_version": "2"`},
		{"a wrong arrival", "library.json", `"after": "1m0s"`, `"after": "2m0s"`},
		{"another plan's pin", "library.json", `"plan_sha256": "8da8`, `"plan_sha256": "0da8`},
		{"an omitted duplicate flag", "library.json", `"duplicate": false,`, ``},
		{"an unknown member", "library.json", `"duplicate": false,`, `"duplicate": false, "extra": true,`},
		{"a template whose profile is not its plan's", "false-profile.json", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := bytes.Replace(oracle, []byte(tc.old), []byte(tc.new), 1)
			if tc.old != "" && bytes.Equal(changed, oracle) {
				t.Fatal("the mutation did not apply")
			}
			stdout, stderr, err, got := check(tc.library, write(fmt.Sprintf("oracle-%d.json", i), changed))
			if err == nil || stdout != "" || got.State != desktop.Failed || got.Reason != refusedAs(stderr) || got.Streams != 0 {
				t.Fatalf("the command refused with %q (%v); the window answered %+v", stderr, err, got)
			}
		})
	}
	names := map[string]bool{}
	for name := range treeOf(t, workspace) {
		names[name] = true
	}
	if len(names) != 5+9 {
		t.Fatalf("a check wrote into the workspace: %v", names)
	}
}

// A library the window saves, versions, exports or imports is one the
// command's check reads: a template saved from the shipped plan carries the
// plan digest the independently authored expectations pin, a second
// revision of the same plan compares as the same plan and passes the same
// check, an export and an import are the library's exact bytes, and a
// revision or an entry that already exists is never overwritten.
func TestDesktopScenarioLibrariesAreTheCommandLinesLibraries(t *testing.T) {
	workspace, elsewhere := t.TempDir(), t.TempDir()
	app := desktopApp(t, workspace)
	shipped, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", "scenario-library.json"))
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", "scenario-expectations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var library scenariolibrary.Library
	if err := json.Unmarshal(shipped, &library); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"cancel-book-plan.json": library.Templates[0].Plan,
		"expectations.json":     oracle,
		"expectations-v2.json":  bytes.Replace(oracle, []byte(`"template_version": "1"`), []byte(`"template_version": "2"`), 1),
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	checks := func(name, expectations string) {
		t.Helper()
		stdout, stderr, err := run(t, "scenario", "check-library", filepath.Join(workspace, name), filepath.Join(workspace, expectations))
		if err != nil || stderr != "" || stdout != "Fixture checks passed: 2 streams, 11 fields. External target outcomes: unverified.\n" {
			t.Fatalf("check-library %s %s: %q %v %s", name, expectations, stdout, err, stderr)
		}
	}
	save := func(library, version, plan string) desktop.ScenarioLibraryResult {
		return app.SaveScenarioLibraryEntry(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: library, Output: "authored.json",
			TemplateID: "siu-cancel-book", TemplateVer: version, Profile: "readmit-siu-lifecycle-v1", Plan: plan,
			Coverage: "cancel-before-book, booking, absent-patient-name"})
	}
	// The digest the expectations pin was written by hand beside the shipped
	// library; the window's template of the same plan carries it.
	const pinned = "8da8dd5e30cc9f7765471e240ab2de7f29da65238f2b435a080da03b5caf53f2"
	first := save("", "1", "cancel-book-plan.json")
	if first.State != desktop.Completed || first.Output != "authored.json" || len(first.Templates) != 1 || first.Templates[0].PlanSHA != pinned {
		t.Fatalf("the first revision: %+v", first)
	}
	checks("authored.json", "expectations.json")
	if again := save("", "1", "cancel-book-plan.json"); again.State != desktop.Failed || again.Reason != "cannot create destination; file already exists in workspace" {
		t.Fatalf("a new library over an existing entry: %+v", again)
	}
	second := save("authored.json", "2", "cancel-book-plan.json")
	if second.State != desktop.Completed || len(second.Templates) != 2 || second.Templates[1].PlanSHA != pinned {
		t.Fatalf("the second revision: %+v", second)
	}
	checks("authored.json", "expectations.json")
	checks("authored.json", "expectations-v2.json")
	compared := app.CompareScenarioLibraryEntries(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: "authored.json",
		TemplateID: "siu-cancel-book", TemplateVer: "1", Expectations: "2"})
	if compared.State != desktop.Completed || len(compared.Compared) != 1 || !compared.Compared[0].SamePlan ||
		compared.Compared[0].FromSHA != pinned || compared.Compared[0].ToSHA != pinned {
		t.Fatalf("comparing two revisions of one plan: %+v", compared)
	}

	// Another plan under a pinned revision is refused, and the library keeps
	// its bytes; the command still checks it.
	written, err := os.ReadFile(filepath.Join(workspace, "authored.json"))
	if err != nil {
		t.Fatal(err)
	}
	reseeded := bytes.Replace(library.Templates[0].Plan, []byte(`"seed":0`), []byte(`"seed":7`), 1)
	if bytes.Equal(reseeded, library.Templates[0].Plan) {
		reseeded = bytes.Replace(library.Templates[0].Plan, []byte(`"seed": 0`), []byte(`"seed": 7`), 1)
	}
	if refused := save("authored.json", "2", string(reseeded)); refused.State != desktop.Failed || refused.Reason != "cannot overwrite another library revision; bump the template version" {
		t.Fatalf("another plan under revision 2: %+v", refused)
	}
	if after, _ := os.ReadFile(filepath.Join(workspace, "authored.json")); !bytes.Equal(after, written) {
		t.Fatal("a refused revision changed the library")
	}
	checks("authored.json", "expectations-v2.json")

	// An export and an import are the library's exact bytes, and each is
	// checked by the command; neither writes over an existing entry.
	exported := app.ExportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: "authored.json", Output: "exported.json"})
	if exported.State != desktop.Completed || exported.Output != "exported.json" {
		t.Fatalf("export: %+v", exported)
	}
	if copied, _ := os.ReadFile(filepath.Join(workspace, "exported.json")); !bytes.Equal(copied, written) {
		t.Fatal("the export is not the library's bytes")
	}
	checks("exported.json", "expectations.json")
	if again := app.ExportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: "cancel-book-plan.json", Output: "exported.json"}); again.State != desktop.Failed {
		t.Fatalf("an export of a plan that is not a library, over an existing entry: %+v", again)
	}
	if again := app.ExportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: "authored.json", Output: "exported.json"}); again.State != desktop.Failed ||
		again.Reason != "cannot create destination; file already exists in workspace" {
		t.Fatalf("an export over an existing entry: %+v", again)
	}
	outside := filepath.Join(elsewhere, "shipped-library.json")
	if err := os.WriteFile(outside, shipped, 0o600); err != nil {
		t.Fatal(err)
	}
	imported := app.ImportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: outside, Output: "imported.json"})
	if imported.State != desktop.Completed || len(imported.Templates) != 1 || imported.Templates[0].PlanSHA != pinned {
		t.Fatalf("import: %+v", imported)
	}
	if copied, _ := os.ReadFile(filepath.Join(workspace, "imported.json")); !bytes.Equal(copied, shipped) {
		t.Fatal("the import is not the library's bytes")
	}
	checks("imported.json", "expectations.json")
	if again := app.ImportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: workspace, Library: outside, Output: "imported.json"}); again.State != desktop.Failed ||
		again.Reason != "cannot create destination; file already exists in workspace" {
		t.Fatalf("an import over an existing entry: %+v", again)
	}
	if copied, _ := os.ReadFile(filepath.Join(workspace, "exported.json")); !bytes.Equal(copied, written) {
		t.Fatal("a refused export changed the earlier one")
	}
}

// A scenario the window saves is the document the command previews: the
// saved canonical form previews exactly as the original, including a
// declared outcome the profile refuses, and opens back unchanged. A document
// the command's reader refuses is refused on save and on open in its words,
// and a save never replaces an existing entry.
func TestDesktopScenarioDocumentsMatchTheCommandLine(t *testing.T) {
	workspace := t.TempDir()
	app := desktopApp(t, workspace)
	preview := func(path string) (string, string, error) {
		t.Helper()
		return run(t, "scenario", "preview", path)
	}
	for _, name := range []string{"scenario-siu.json", "scenario-adt.json", "scenario-orm.json", "scenario-oru.json", "scenario-refused.json"} {
		original := filepath.Join("..", "testdata", "fixtures", name)
		document, err := os.ReadFile(original)
		if err != nil {
			t.Fatal(err)
		}
		saved := app.SaveScenario(desktop.ScenarioSaveRequest{Workspace: workspace, Document: string(document), Output: name})
		if saved.State != desktop.Completed || saved.Output != name {
			t.Fatalf("save %s: %+v", name, saved)
		}
		written, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil || string(written) != saved.Document {
			t.Fatalf("%s: the window wrote other bytes than it showed: %v", name, err)
		}
		wantOut, wantErr, wantStatus := preview(original)
		gotOut, gotErr, gotStatus := preview(filepath.Join(workspace, name))
		if gotOut != wantOut || gotErr != wantErr || (gotStatus == nil) != (wantStatus == nil) {
			t.Fatalf("%s: the saved document previews as %q %q, the original as %q %q", name, gotOut, gotErr, wantOut, wantErr)
		}
		opened := app.OpenScenario(workspace, name)
		if opened.State != desktop.Completed || opened.Document != saved.Document || opened.ID != saved.ID || opened.Version != saved.Version || opened.Profile != saved.Profile {
			t.Fatalf("%s: reopened as %+v", name, opened)
		}
		if again := app.SaveScenario(desktop.ScenarioSaveRequest{Workspace: workspace, Document: string(document), Output: name}); again.State != desktop.Failed ||
			again.Reason != "cannot create destination; file already exists in workspace" {
			t.Fatalf("%s: a save over the saved document: %+v", name, again)
		}
		if after, _ := os.ReadFile(filepath.Join(workspace, name)); !bytes.Equal(after, written) {
			t.Fatalf("%s: a refused save changed the document", name)
		}
	}

	siu, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", "scenario-siu.json"))
	if err != nil {
		t.Fatal(err)
	}
	unsupported := bytes.Replace(siu, []byte(`"event": "S12"`), []byte(`"event": "A01"`), 1)
	if bytes.Equal(unsupported, siu) {
		t.Fatal("the mutation did not apply")
	}
	outside := filepath.Join(t.TempDir(), "unsupported.json")
	if err := os.WriteFile(outside, unsupported, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := preview(outside)
	if err == nil {
		t.Fatal("the command previewed an event its profile does not declare")
	}
	refused := app.SaveScenario(desktop.ScenarioSaveRequest{Workspace: workspace, Document: string(unsupported), Output: "unsupported.json"})
	if refused.State != desktop.Failed || refused.Reason != refusedAs(stderr) {
		t.Fatalf("the command refused with %q; the window's save answered %+v", stderr, refused)
	}
	if _, err := os.Stat(filepath.Join(workspace, "unsupported.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused save wrote the document: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "unsupported.json"), unsupported, 0o600); err != nil {
		t.Fatal(err)
	}
	if opened := app.OpenScenario(workspace, "unsupported.json"); opened.State != desktop.Failed || opened.Reason != refusedAs(stderr) || opened.Document != "" {
		t.Fatalf("the command refused with %q; the window's open answered %+v", stderr, opened)
	}
}
