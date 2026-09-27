package importer_test

import (
	"github.com/bharm16/readmit/internal/importer"
	"testing"
)

func TestProjectionPreservesJSONStatesRepeatsAndRecordSpans(t *testing.T) {
	shape := importer.EnvelopeShape{Envelope: importer.JSONEnvelope, Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{"rows"}}}
	raw := []byte(`{"rows":[{"id":"same","amount":9007199254740993.1200,"nil":null,"empty":"","repeat":["a","a",null]},{"id":"same"}]}`)
	rows, err := shape.Project([]importer.Locator{{"id"}, {"amount"}, {"nil"}, {"empty"}, {"repeat"}}, raw)
	if err != nil || len(rows) != 2 {
		t.Fatalf("projection: %v %d", err, len(rows))
	}
	if rows[0].Values[1].Text != "9007199254740993.1200" || rows[0].Values[2].State != "null" || rows[0].Values[3].State != "empty" || len(rows[0].Values[4].Items) != 3 || rows[1].Values[1].State != "absent" {
		t.Fatalf("states: %+v", rows)
	}
	if string(raw[rows[1].Offset:rows[1].Offset+rows[1].Size]) != `{"id":"same"}` {
		t.Fatal("record provenance lost")
	}
	for _, bad := range []string{`{"rows":[{"id":"x"}]`, `{"rows":[]} trailing`, `{"rows":[{"id":"x","id":"y"}]}`} {
		if _, err := shape.Project([]importer.Locator{{"id"}}, []byte(bad)); err == nil {
			t.Fatal("malformed or ambiguous JSON accepted")
		}
	}
}

func TestProjectionRejectsNamespaceAndDuplicateScalarAmbiguity(t *testing.T) {
	shape := importer.EnvelopeShape{Envelope: importer.XMLEnvelope, Encoding: importer.UTF8, XML: &importer.DocumentDialect{RecordPath: []string{"rows", "row"}}}
	for _, raw := range []string{`<rows><row><id>a</id></row>`, `<rows/><rows/>`, `<rows xmlns="urn:unknown"><row><id>a</id></row></rows>`} {
		if _, err := shape.Project([]importer.Locator{{"id"}}, []byte(raw)); err == nil {
			t.Fatal("ambiguous XML accepted")
		}
	}
	rows, err := shape.Project([]importer.Locator{{"id"}}, []byte(`<rows><row><id>a</id><id>b</id></row></rows>`))
	if err != nil || rows[0].Values[0].Kind != "array" || len(rows[0].Values[0].Items) != 2 {
		t.Fatal("XML first-match fallback", err)
	}
}
