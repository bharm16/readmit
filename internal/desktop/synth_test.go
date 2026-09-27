package desktop_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/scenariolibrary"
	"github.com/bharm16/readmit/internal/synth"
)

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
