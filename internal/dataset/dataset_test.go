package dataset_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
)

func fixture() (dataset.Binding, dataset.Projection, dataset.Acquisition) {
	config := []byte(`{"schema":"synthetic-source/v1"}`)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return dataset.Binding{Run: "run-1", Phase: "after", Source: dataset.Digest(config), Namespace: "appointments"}, dataset.Projection{Schema: dataset.ProjectionSchema, ID: "appointments", Format: "json", Order: "source", Envelope: &dataset.Envelope{Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{"rows"}}}, Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}, {Name: "start", Type: "datetime", Locator: importer.Locator{"start"}, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}, dataset.Acquisition{Kind: "file", Status: "complete", StartedAt: now, CompletedAt: now.Add(time.Millisecond), SourceConfiguration: config, Completion: "snapshot"}
}
func TestDatasetRetainsDuplicateRowsAndReDerivesAfterSourceRemoval(t *testing.T) {
	b, p, a := fixture()
	raw := []byte(`{"rows":[{"id":"same","start":"2026-01-01","status":"booked"},{"id":"same","start":"2026-01-02T10:30","status":"moved"}]}`)
	s, err := dataset.Build(context.Background(), b, p, a, raw)
	if err != nil || !s.Usable() {
		t.Fatal(err)
	}
	d := s.Document()
	if len(d.Rows) != 2 || d.Rows[0].ID == d.Rows[1].ID || d.Rows[0].Values[0].Text != d.Rows[1].Values[0].Text || d.Rows[0].Values[1].Precision != "date" || d.Rows[1].Values[1].Timezone != "absent" {
		t.Fatalf("%+v", d.Rows)
	}
	out := filepath.Join(t.TempDir(), "dataset")
	if err = s.Write(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	clear(raw)
	reopened, err := dataset.Open(context.Background(), out)
	if err != nil || reopened.Identity() != s.Identity() {
		t.Fatal(err)
	}
	// A recomputed outer digest cannot make changed mapped values agree with raw evidence.
	manifestPath := filepath.Join(out, "manifest.json")
	manifest, _ := os.ReadFile(manifestPath)
	json.Unmarshal(manifest, &d)
	d.Rows[0].Values[2].Text = "tampered"
	manifest, _ = json.Marshal(d, json.Deterministic(true))
	os.WriteFile(manifestPath, manifest, 0600)
	material, _ := os.ReadFile(filepath.Join(out, "material.bin"))
	identity := artifactdir.Identity(dataset.Schema, map[string][]byte{"manifest.json": manifest, "material.bin": material})
	os.WriteFile(filepath.Join(out, "identity.sha256"), []byte(identity+"\n"), 0600)
	if _, err := dataset.Open(context.Background(), out); err == nil {
		t.Fatal("tampered projection accepted")
	}
}
func TestDatasetDistinguishesEmptyFromUnusableAndRetainsTypedStates(t *testing.T) {
	b, p, a := fixture()
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}, {Name: "amount", Type: "decimal", Locator: importer.Locator{"amount"}}, {Name: "nothing", Type: "text", Locator: importer.Locator{"nothing"}}, {Name: "empty", Type: "text", Locator: importer.Locator{"empty"}}, {Name: "absent", Type: "text", Locator: importer.Locator{"absent"}}, {Name: "flags", Type: "boolean", Locator: importer.Locator{"flags"}, Repeated: true}}
	s, err := dataset.Build(context.Background(), b, p, a, []byte(`{"rows":[{"id":"one","amount":9007199254740993.1200,"nothing":null,"empty":"","flags":[true,false,true]}]}`))
	if err != nil || !s.Usable() {
		t.Fatal(err)
	}
	v := s.Document().Rows[0].Values
	if v[1].Text != "9007199254740993.1200" || v[1].Precision != "4" || v[2].State != "null" || v[3].State != "empty" || v[4].State != "absent" || len(v[5].Items) != 3 {
		t.Fatal(v)
	}
	for _, raw := range [][]byte{[]byte(`{"rows":[{}]}`), []byte(`{"rows":[`), {'\xff'}, []byte(`{"rows":[{"id":"one","amount":"bad"}]}`)} {
		s, err = dataset.Build(context.Background(), b, p, a, raw)
		if err != nil || s.Usable() {
			t.Fatalf("bad material usable: %v", err)
		}
	}
	s, err = dataset.Build(context.Background(), b, p, a, []byte(`{"rows":[]}`))
	if err != nil || !s.Usable() || len(s.Document().Rows) != 0 {
		t.Fatal("complete empty lost")
	}
	for _, status := range []string{"missing", "failed", "truncated", "cancelled"} {
		a.Status = status
		s, err = dataset.Build(context.Background(), b, p, a, nil)
		if err != nil || s.Usable() {
			t.Fatal("failed read became absence")
		}
	}
}

func TestDatasetXMLTextHL7RepeatsAndExplicitLimits(t *testing.T) {
	b, p, a := fixture()
	p.Format = "xml"
	p.Envelope = &dataset.Envelope{Encoding: importer.UTF8, XML: &importer.DocumentDialect{RecordPath: []string{"rows", "row"}}}
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}, {Name: "values", Type: "text", Locator: importer.Locator{"value"}, Repeated: true}}
	s, err := dataset.Build(context.Background(), b, p, a, []byte(`<rows><row><id>same</id><value>a</value><value>a</value><value/></row></rows>`))
	if err != nil || !s.Usable() || len(s.Document().Rows[0].Values[1].Items) != 3 {
		t.Fatal("XML repeats", err)
	}
	p.Columns[1].Repeated = false
	s, err = dataset.Build(context.Background(), b, p, a, []byte(`<rows><row><id>same</id><value>a</value><value>b</value></row></rows>`))
	if err != nil || s.Usable() {
		t.Fatal("ambiguous XML passed", err)
	}
	s, err = dataset.Build(context.Background(), b, p, a, []byte(`<rows/>`))
	if err != nil || !s.Usable() || len(s.Document().Rows) != 0 {
		t.Fatal("empty XML", err)
	}
	p.Format = "text"
	p.Envelope = &dataset.Envelope{Encoding: importer.UTF8, Text: &importer.TextDialect{FieldSeparator: "|", RecordSeparator: importer.LFSeparator, Fields: 2}}
	p.Columns[0].Locator = importer.Locator{"1"}
	p.Columns[1].Locator = importer.Locator{"2"}
	s, err = dataset.Build(context.Background(), b, p, a, []byte("same|\nsame|value\n"))
	if err != nil || !s.Usable() || s.Document().Rows[0].Values[1].State != "empty" {
		t.Fatal("text states", err)
	}
	p.Limits.MaxRows = 1
	s, err = dataset.Build(context.Background(), b, p, a, []byte("same|a\nsame|b\n"))
	if err != nil || s.Usable() || s.Document().Status != "row-limit" {
		t.Fatal("row limit", err)
	}
	p.Limits.MaxBytes = 1
	s, err = dataset.Build(context.Background(), b, p, a, []byte("same|a\n"))
	if err != nil || s.Usable() {
		t.Fatal("byte limit", err)
	}
	p.Limits.MaxBytes = 65536
	a.CompletedAt = a.StartedAt.Add(2 * time.Second)
	s, err = dataset.Build(context.Background(), b, p, a, []byte("same|a\n"))
	if err != nil || s.Usable() {
		t.Fatal("time limit", err)
	}
	b, p, a = fixture()
	p.Format = "hl7"
	p.Envelope = nil
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Selector: "MSH-10", Key: true, Required: true}, {Name: "repeats", Type: "text", Selector: "OBX-5", Repeated: true}}
	a.Kind = "capture"
	material, _ := json.Marshal(dataset.CaptureRead{Schema: dataset.CaptureSchema, Identity: dataset.Digest([]byte("verified-capture")), Rows: []dataset.CaptureRow{{Occurrence: "s0001-e000001", Raw: []byte("MSH|^~\\&|LAB|LAB|TARGET|LAB|20260101000000||ORU^R01|SAME|P|2.5.1\rOBX|1|ST|CODE||a~a~\"\"~\r")}}})
	s, err = dataset.Build(context.Background(), b, p, a, material)
	if err != nil || !s.Usable() {
		t.Fatal("HL7 repeats", err)
	}
	items := s.Document().Rows[0].Values[1].Items
	if len(items) != 4 || items[2].State != "null" || items[3].State != "empty" {
		t.Fatal(items)
	}
}

func TestDatasetDatabaseDriverKindsNeverInventPrecisionOrValidText(t *testing.T) {
	b, p, a := fixture()
	p.Format = "database"
	p.Order = "unordered"
	p.Envelope = nil
	a.Kind = "database"
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"key"}, Key: true, Required: true}, {Name: "value", Type: "decimal", Locator: importer.Locator{"value"}, Required: true}}
	for _, input := range []any{float64(9007199254740993), string([]byte{255})} {
		value, err := dataset.FromDriver(input)
		if err != nil {
			t.Fatal(err)
		}
		read := dataset.DatabaseRead{Schema: dataset.DatabaseSchema, Driver: "postgresql", Columns: []dataset.DatabaseColumn{{Name: "key", Type: "TEXT"}, {Name: "value", Type: "NUMERIC"}}, Rows: [][]dataset.DriverValue{{{Kind: "string", Text: "key"}, value}}}
		raw, _ := json.Marshal(read)
		snapshot, err := dataset.Build(context.Background(), b, p, a, raw)
		if err != nil || snapshot.Usable() {
			t.Fatal("lossy driver value passed", err)
		}
	}
}

func TestDatasetRefusesExpandedValueBudgetAndRemainsRetainable(t *testing.T) {
	b, p, a := fixture()
	p.Limits.MaxRows = 100
	p.Limits.MaxBytes = dataset.MaxBytes
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}}
	array := `[` + strings.Repeat("null,", 1023) + `null]`
	row := `{"id":"same"`
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("values%d", i)
		p.Columns = append(p.Columns, dataset.Column{Name: name, Type: "text", Locator: importer.Locator{name}, Repeated: true})
		row += `,"` + name + `":` + array
	}
	row += `}`
	raw := []byte(`{"rows":[` + strings.Repeat(row+",", 99) + row + `]}`)
	if len(raw) > dataset.MaxBytes {
		t.Fatal("fixture exceeds input bound")
	}
	s, err := dataset.Build(context.Background(), b, p, a, raw)
	if err != nil || s.Usable() || s.Document().Status != "projection-limit" {
		t.Fatal("expanded metadata escaped budget", err)
	}
	out := filepath.Join(t.TempDir(), "bounded")
	if err = s.Write(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	reopened, err := dataset.Open(context.Background(), out)
	if err != nil || reopened.Usable() {
		t.Fatal("budget refusal did not reopen", err)
	}
}
func TestDatasetPreservesCollectionNullAndRejectsAmbiguousHL7AndXML(t *testing.T) {
	b, p, a := fixture()
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}, {Name: "values", Type: "text", Locator: importer.Locator{"values"}, Repeated: true}}
	var states []dataset.Value
	for _, raw := range []string{`{"rows":[{"id":"same","values":null}]}`, `{"rows":[{"id":"same","values":[null]}]}`} {
		s, err := dataset.Build(context.Background(), b, p, a, []byte(raw))
		if err != nil || !s.Usable() {
			t.Fatal(err)
		}
		states = append(states, s.Document().Rows[0].Values[1])
	}
	if states[0].State != "null" || states[0].Items != nil || states[1].State != "present" || len(states[1].Items) != 1 || dataset.Equal(states[0], states[1]) {
		t.Fatal("collection null collapsed into null member")
	}
	p.Format = "xml"
	p.Envelope = &dataset.Envelope{Encoding: importer.UTF8, XML: &importer.DocumentDialect{RecordPath: []string{"rows", "row"}}}
	p.Columns[1].Repeated = false
	s, err := dataset.Build(context.Background(), b, p, a, []byte(`<rows xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><row><id>same</id><values xsi:nil="true"><child>payload</child></values></row></rows>`))
	if err != nil || s.Usable() {
		t.Fatal("invalid XML child became null", err)
	}
	p.Format = "hl7"
	p.Envelope = nil
	a.Kind = "capture"
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Selector: "MSH-10", Key: true, Required: true}, {Name: "value", Type: "text", Selector: "OBX-5"}}
	material, _ := json.Marshal(dataset.CaptureRead{Schema: dataset.CaptureSchema, Identity: dataset.Digest([]byte("capture")), Rows: []dataset.CaptureRow{{Occurrence: "s0001-e000001", Raw: []byte("MSH|^~\\&|LAB|LAB|TARGET|LAB|20260101000000||ORU^R01|SAME|P|2.5.1\rOBX|1|ST|CODE||a~b\r")}}})
	s, err = dataset.Build(context.Background(), b, p, a, material)
	if err != nil || s.Usable() {
		t.Fatal("implicit first repetition passed", err)
	}
	p.Columns[1].Selector = "OBX-5[2]"
	s, err = dataset.Build(context.Background(), b, p, a, material)
	if err != nil || !s.Usable() || s.Document().Rows[0].Values[1].Text != "b" {
		t.Fatal("explicit repetition refused", err)
	}
}
