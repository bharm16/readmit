package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/importer"
)

// mappingRecipe is the recipe the command tests run under. It is written to a
// file rather than built from flags, because a recipe is the reusable artifact
// this command reads; there is no flag form of a mapping.
const mappingRecipe = `{"schema":"readmit-mapping-recipe/v1","name":"engine-csv-export","revision":2,` +
	`"envelope":"csv","encoding":"utf-8","members":[".csv"],` +
	`"csv":{"delimiter":",","record_separator":"crlf","header":"present","fields":5},` +
	`"payload":{"operator":"verbatim","locator":["message"],"framing":"raw","terminator":"cr"},` +
	`"observed_at":{"operator":"rfc3339","locator":["received"]},` +
	`"source":{"operator":"field","locator":["interface"]},` +
	`"direction":{"operator":"field","locator":["flow"],"values":[{"envelope":"IN","mapped":"inbound"}]},` +
	`"channel":{"operator":"field","locator":["channel"]}}`

// mappingCorpus writes one CSV export holding one row a recipe maps and one it
// cannot, and the recipe that reads it.
func mappingCorpus(t *testing.T) (string, string) {
	t.Helper()
	folder := t.TempDir()
	rows := []string{
		`"received","flow","channel","interface","message"`,
		`"2026-01-02T03:04:05Z","IN","adt-inbound","EPIC","` + importFixture("MAP") + `"`,
		`"2026-01-02T03:04:06Z","SIDEWAYS","adt-inbound","EPIC","` + importFixture("BAD") + `"`,
	}
	member := filepath.Join(folder, "export.csv")
	if err := os.WriteFile(member, []byte(strings.Join(rows, "\r\n")+"\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return member, writeDocument(t, t.TempDir(), "recipe.json", mappingRecipe)
}

func TestImportMapsAnEnvelopeAndRecordsTheRecipeItRanUnder(t *testing.T) {
	member, recipe := mappingCorpus(t)
	stdout, stderr, err := run(t, "import", "--recipe", recipe, "--file", member, "--preview")
	if err != nil || stderr != "" {
		t.Fatalf("preview: %v %s", err, stderr)
	}
	preview := readStrictOutput[importer.MappingPreview](t, stdout)
	if preview.Schema != importer.MappingPreviewSchema || preview.Recipe.Revision != 2 ||
		preview.Totals.Sources != 2 || preview.UnmappedRecords != 1 || len(preview.Identity) != 64 {
		t.Fatalf("preview: schema=%s revision=%d totals=%+v unmapped=%d", preview.Schema, preview.Recipe.Revision, preview.Totals, preview.UnmappedRecords)
	}
	if strings.Contains(stdout, "SYNTH-IMPORT") {
		t.Fatal("the preview carried message evidence")
	}
	if entries, err := os.ReadDir(filepath.Dir(member)); err != nil || len(entries) != 1 {
		t.Fatalf("the preview changed the declared folder: %v %d", err, len(entries))
	}

	destination := filepath.Join(t.TempDir(), "incident.case")
	receiptPath := filepath.Join(t.TempDir(), "incident-mapping.json")
	stdout, stderr, err = run(t, "import", "--recipe", recipe, "--file", member, "--output", destination, "--receipt", receiptPath)
	if err != nil || stderr != "" {
		t.Fatalf("import: %v %s", err, stderr)
	}
	for _, want := range []string{
		"Mapping recipe: " + importer.RecipeSchema, "Name: engine-csv-export", "Revision: 2",
		"Recipe identity: " + preview.Identity, "Envelope: csv", "Unmapped records: 1", "Extracted sources: 2",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in output:\n%s", want, stdout)
		}
	}
	// The summary names no container path, no member name and no value: the
	// receipt is the file the person chose to keep those in.
	for _, hidden := range []string{"export.csv", "SYNTH-IMPORT", "MSH|", "EPIC", "adt-inbound"} {
		if strings.Contains(stdout+stderr, hidden) {
			t.Errorf("the import summary carried %q", hidden)
		}
	}

	receipt := readStrictDocument[importer.MappingReceipt](t, receiptPath)
	if receipt.Identity != preview.Identity || receipt.UnmappedRecords != 1 || len(receipt.Mappings) != 2 {
		t.Fatalf("receipt: %+v", receipt)
	}
	if receipt.Mappings[0].Source != "EPIC" || receipt.Mappings[0].Channel != "adt-inbound" ||
		receipt.Mappings[0].Direction != bundle.Inbound || receipt.Mappings[0].ObservedAt == nil {
		t.Fatalf("the mapped declarations were not recorded: %+v", receipt.Mappings[0])
	}
	if receipt.Mappings[1].State != importer.Unmapped || receipt.Mappings[1].Direction != bundle.Unknown {
		t.Fatalf("the unmapped record kept provenance: %+v", receipt.Mappings[1])
	}

	// The case holds the mapped payload bytes and the retained record, and the
	// declared observation reached the occurrence the recipe mapped.
	opened, err := bundle.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Manifest.Schema != bundle.Schema || len(opened.Manifest.Sources) != 2 {
		t.Fatalf("case: %s %d sources", opened.Manifest.Schema, len(opened.Manifest.Sources))
	}
	if opened.Events[0].Direction != bundle.Inbound || opened.Events[0].ObservedAt == nil {
		t.Fatalf("the mapped observation did not reach the case: %+v", opened.Events[0])
	}
	if opened.Events[1].Direction != bundle.Unknown || opened.Events[1].ObservedAt != nil {
		t.Fatalf("the unmapped occurrence gained provenance: %+v", opened.Events[1])
	}
	payload, err := os.ReadFile(filepath.Join(destination, opened.Events[0].Payload.Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != importFixture("MAP") {
		t.Fatal("the stored payload is not the envelope's own bytes")
	}
}

func TestImportReadsARecipeOrAPlanButNeverBoth(t *testing.T) {
	member, recipe := mappingCorpus(t)
	plan := writeDocument(t, t.TempDir(), "plan.json",
		`{"schema":"readmit-import-plan/v1","framing":"raw","terminator":"cr","encoding":"utf-8","direction":"inbound","members":[]}`)
	for name, args := range map[string][]string{
		"recipe and plan":        {"--recipe", recipe, "--plan", plan},
		"recipe and declaration": {"--recipe", recipe, "--framing", "raw"},
		"recipe and direction":   {"--recipe", recipe, "--direction", "inbound"},
	} {
		_, stderr, err := run(t, append([]string{"import", "--file", member, "--preview"}, args...)...)
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
		if !strings.Contains(stderr, "never both") {
			t.Errorf("%s: %s", name, stderr)
		}
	}
}

func TestImportRefusesARecipeItCannotRead(t *testing.T) {
	member, _ := mappingCorpus(t)
	for name, document := range map[string]string{
		"later version": strings.Replace(mappingRecipe, "recipe/v1", "recipe/v2", 1),
		"unknown member": strings.Replace(mappingRecipe, `"members":[".csv"],`,
			`"members":[".csv"],"detect_envelope":true,`, 1),
		"no dialect": strings.Replace(mappingRecipe,
			`"csv":{"delimiter":",","record_separator":"crlf","header":"present","fields":5},`, "", 1),
	} {
		recipe := writeDocument(t, t.TempDir(), "recipe.json", document)
		_, stderr, err := run(t, "import", "--recipe", recipe, "--file", member, "--preview")
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
		if strings.Contains(stderr, "export.csv") || len(stderr) > 300 {
			t.Errorf("%s diagnostic: %s", name, stderr)
		}
	}
	// A member whose structure contradicts the recipe is refused by name, and
	// the diagnostic names the declaration rather than the file or its values.
	folder := t.TempDir()
	other := filepath.Join(folder, "other.csv")
	if err := os.WriteFile(other, []byte("a,b\r\n1,2\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	recipe := writeDocument(t, t.TempDir(), "recipe.json", mappingRecipe)
	_, stderr, err := run(t, "import", "--recipe", recipe, "--file", other, "--preview")
	if err == nil {
		t.Fatal("a member that is not the declared envelope was accepted")
	}
	if !strings.Contains(stderr, "declared envelope") {
		t.Fatalf("diagnostic: %s", stderr)
	}
}

func TestMappedImportRefusesToOverwriteAndLeavesNoUndescribedCase(t *testing.T) {
	member, recipe := mappingCorpus(t)
	taken := filepath.Join(t.TempDir(), "taken.json")
	if err := os.WriteFile(taken, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "incident.case")
	_, stderr, err := run(t, "import", "--recipe", recipe, "--file", member, "--output", destination, "--receipt", taken)
	if err == nil {
		t.Fatal("an existing receipt destination was accepted")
	}
	if !strings.Contains(stderr, "must be a new file") {
		t.Fatalf("diagnostic: %s", stderr)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("a case was written that no receipt describes")
	}
	// A preview writes nothing at all, so it cannot be combined with either
	// destination.
	if _, _, err := run(t, "import", "--recipe", recipe, "--file", member, "--preview", "--output", destination); err == nil {
		t.Fatal("a preview with an output destination was accepted")
	}
}
