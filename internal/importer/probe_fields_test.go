package importer_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/importer"
)

func TestJSONProbeKeepsRecordAndContinuationPathsOutsideClosedObjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.json")
	if err := os.WriteFile(path, []byte(`{"appointments":[{"id":"qa","nested":{"status.code":"booked"}}],"paging":{"next.token":""}}`), 0600); err != nil {
		t.Fatal(err)
	}
	probe, err := importer.ProbeInputs(t.Context(), []string{path}, nil, nil)
	if err != nil || probe.Sample == nil {
		t.Fatal(err)
	}
	want := [][]string{{"appointments"}, {"appointments", "id"}, {"appointments", "nested"}, {"appointments", "nested", "status.code"}, {"paging"}, {"paging", "next.token"}}
	if !reflect.DeepEqual(probe.Sample.Paths, want) {
		t.Fatalf("closing a record changed the outer document scope: %+v", probe.Sample.Paths)
	}
}
