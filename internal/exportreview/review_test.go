package exportreview_test

import (
	"encoding/base64"
	"slices"
	"testing"

	"github.com/bharm16/readmit/internal/exportreview"
)

func TestResidualScanFindsKnownNamesJSONAndReplayBase64(t *testing.T) {
	scan := exportreview.Residual(map[string][]byte{
		"PLANTED-FILENAME.txt": []byte("ordinary content"),
		"spec.json":            []byte(`{"text":"PLANTED-\"LITERAL\""}`),
		"run.json":             []byte(base64.StdEncoding.EncodeToString([]byte("PLANTED-REPLAY"))),
	}, [][]byte{[]byte("PLANTED-FILENAME"), []byte(`PLANTED-"LITERAL"`), []byte("PLANTED-REPLAY")})
	if scan.Status != "blocked" || !slices.Equal(scan.Locations, []string{"PLANTED-FILENAME.txt", "run.json", "spec.json"}) || scan.Limitations == "" {
		t.Fatalf("wrong limited scan: %+v", scan)
	}
	clean := exportreview.Residual(map[string][]byte{"result.json": []byte(`{"unknown":"not detected"}`)}, [][]byte{[]byte("KNOWN-ONLY")})
	if clean.Status != "passed" || clean.Limitations == "" {
		t.Fatal("no-hit scan lost its limitations")
	}
}
