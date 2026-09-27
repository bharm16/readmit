package tests

// The window's SIU fixture families and fixture checks are `readmit synth`
// and `readmit scenario check-library`: the same inputs write the same
// family and decide the same check, refused in the command's words.

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
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"github.com/bharm16/readmit/internal/synth"
)

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
