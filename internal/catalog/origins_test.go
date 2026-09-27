package catalog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
)

// An origin is recorded once for its object and kept as first recorded; one
// that names nothing it could have come from is refused.
func TestAnOriginIsRecordedOnceAndKeptAsFirstRecorded(t *testing.T) {
	root := t.TempDir()
	store, err := catalog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if held, err := store.Origins(); err != nil || len(held) != 0 {
		t.Fatalf("a project that generated nothing: %+v %v", held, err)
	}
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	generated, scenario, fixture := "0123456789abcdef01234567", "89abcdef0123456789abcdef", "fedcba9876543210fedcba98"
	first, err := store.RecordOrigin(catalog.Origin{Item: generated, Kind: catalog.OriginScenario, Source: scenario, Revision: 2}, now)
	if err != nil || first.RecordedAt == "" {
		t.Fatalf("record: %+v %v", first, err)
	}
	again, err := store.RecordOrigin(catalog.Origin{Item: generated, Kind: catalog.OriginScenario, Source: scenario, Revision: 3}, now.Add(time.Hour))
	if err != nil || again != first {
		t.Fatalf("recorded again: %+v %v", again, err)
	}
	if _, err := store.RecordOrigin(catalog.Origin{Item: fixture, Kind: catalog.OriginFixture, Mode: "defective", Observation: "siu-fixture-observation.json"}, now); err != nil {
		t.Fatal(err)
	}
	for name, refused := range map[string]catalog.Origin{
		"another kind":            {Item: "0000000000000000000000aa", Kind: "import", Source: scenario, Revision: 1},
		"no revision":             {Item: "0000000000000000000000ab", Kind: catalog.OriginScenario, Source: scenario},
		"a fixture naming a path": {Item: "0000000000000000000000ac", Kind: catalog.OriginFixture, Mode: "fixed", Observation: "../ledger.json"},
		"a fixture of no mode":    {Item: "0000000000000000000000ad", Kind: catalog.OriginFixture, Observation: "ledger.json"},
	} {
		if _, err := store.RecordOrigin(refused, now); err == nil {
			t.Fatalf("%s was recorded", name)
		}
	}
	held, err := store.Origins()
	if err != nil || len(held) != 2 || held[0] != first || held[1].Mode != "defective" {
		t.Fatalf("held: %+v %v", held, err)
	}
	data, err := os.ReadFile(filepath.Join(root, catalog.Folder, catalog.OriginsDocumentName))
	if err != nil || !strings.Contains(string(data), catalog.OriginsSchema) {
		t.Fatalf("the document: %s %v", data, err)
	}
}
