package operation

import (
	"testing"

	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
)

// Each kind's start leaves empty only what has no default, and the reader
// accepts it once those choices are made.
func TestEveryObservationStartIsReadOnceItsChoicesAreMade(t *testing.T) {
	kinds := map[string]bool{}
	for _, source := range ObservationStarts() {
		kinds[source.Observes.Kind] = true
		switch {
		case source.File != nil:
			if source.File.Path != "" || len(source.Extraction.RecordKey) != 0 {
				t.Fatalf("a file export starts from a chosen file: %+v", source.File)
			}
			source.File.Path, source.Extraction.RecordKey = "export.csv", importer.Locator{"appointment"}
		case source.HTTP != nil:
			if source.HTTP.URL != "" {
				t.Fatalf("an HTTPS source starts from a URL: %+v", source.HTTP)
			}
			source.HTTP.URL, source.Extraction.RecordKey = "https://records.example/appointments", importer.Locator{"id"}
		case source.Capture != nil:
			if source.Capture.Path != "" || source.Capture.RecordKey != "" {
				t.Fatalf("a capture starts from a chosen case: %+v", source.Capture)
			}
			source.Capture.Path, source.Capture.RecordKey = "cases/lab", "SCH-1.1"
		case source.Database != nil:
			if source.Database.Address != "" || len(source.Database.View) != 0 {
				t.Fatalf("a database view starts from a chosen view: %+v", source.Database)
			}
			database := source.Database
			database.Address, database.Name, database.Username, database.ServerName = "db.example:5432", "scheduling", "reader", "db.example"
			database.View, database.RecordKey = []string{"scheduling", "appointments"}, "id"
			database.Credential.Address, database.Credential.Command = "db.example:5432", "/usr/local/bin/locator"
		}
		encoded, err := observesource.EncodeSource(source)
		if err == nil {
			_, err = observesource.DecodeSource(encoded)
		}
		if err != nil {
			t.Fatalf("the %s start: %v", source.Observes.Kind, err)
		}
	}
	for _, kind := range []string{observesource.FileExport, observesource.HTTPAPI, observesource.DownstreamCapture, observesource.DatabaseQuery} {
		if !kinds[kind] {
			t.Fatalf("no %s start", kind)
		}
	}
}
