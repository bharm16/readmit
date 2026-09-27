package observesource_test

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/jackc/pgx/v5/pgproto3"
)

func datasetProjection(format string) dataset.Projection {
	return dataset.Projection{Schema: dataset.ProjectionSchema, ID: "appointments", Format: format, Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"appointment"}, Key: true, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 2000}}
}
func datasetRequest(source observesource.Source, p dataset.Projection, output string) observesource.DatasetRequest {
	return observesource.DatasetRequest{Source: source, Projection: p, Binding: dataset.Binding{Run: "run", Phase: "after", Namespace: "appointments", Source: source.Identity()}, Output: output, Authorize: func(context.Context) error { return nil }}
}
func TestTypedDatasetFileAndCaptureReopenWithoutOriginalSources(t *testing.T) {
	source, path, _ := declared(t, declaredFileSource, "appointment,status\nsame,booked\nsame,moved\n")
	p := datasetProjection("csv")
	p.Envelope = &dataset.Envelope{Encoding: importer.UTF8, CSV: source.Extraction.CSV}
	output := filepath.Join(t.TempDir(), "typed")
	result, err := observesource.CollectDataset(context.Background(), datasetRequest(source, p, output))
	if err != nil || !result.Usable() {
		t.Fatal(err)
	}
	if len(result.Document().Rows) != 2 || result.Document().Rows[1].Values[1].Text != "moved" {
		t.Fatal("lost duplicate typed row")
	}
	os.Remove(path)
	reopened, err := observesource.OpenDataset(context.Background(), output)
	if err != nil || reopened.Identity() != result.Identity() {
		t.Fatal(err)
	}
	capture, original, _ := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamBooking, downstreamBooking)
	p = datasetProjection("hl7")
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Selector: "SCH-1.1", Key: true, Required: true}, {Name: "sender", Type: "text", Selector: "MSH-3", Required: true}}
	output = filepath.Join(t.TempDir(), "typed-capture")
	result, err = observesource.CollectDataset(context.Background(), datasetRequest(capture, p, output))
	if err != nil || !result.Usable() {
		t.Fatal(err)
	}
	facts := result.Document().Acquisition.Facts
	if facts == nil || facts.ObservedFrom.IsZero() || facts.AsOf.IsZero() || facts.Records != 2 {
		t.Fatal("capture interval lost")
	}
	os.Rename(original, original+"-moved")
	reopened, err = observesource.OpenDataset(context.Background(), output)
	if err != nil || len(reopened.Document().Rows) != 2 || reopened.Document().Rows[0].Provenance.SourceRecord == reopened.Document().Rows[1].Provenance.SourceRecord {
		t.Fatal("capture re-derivation lost occurrence identity", err)
	}
}
func TestTypedHTTPDatasetRetainsBodyAndRefusesContinuation(t *testing.T) {
	for _, mode := range []string{"complete", "next", "link"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Age", "0")
				if mode == "link" {
					w.Header().Set("Link", `<https://elsewhere.invalid>; rel="next"`)
				}
				body := `{"appointments":[{"id":"same","status":"moved"}]}`
				if mode == "next" {
					body = `{"appointments":[{"id":"same","status":"moved"}],"next":"page-2"}`
				}
				w.Write([]byte(body))
			}))
			defer server.Close()
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			source, _ := declaredHTTP(t, ca, strings.TrimPrefix(server.URL, "https://"), declaredHTTPSource)
			p := datasetProjection("json")
			p.Columns[0].Locator = importer.Locator{"id"}
			p.Continuation = importer.Locator{"next"}
			p.Envelope = &dataset.Envelope{Encoding: importer.UTF8, JSON: source.Extraction.JSON}
			output := filepath.Join(t.TempDir(), "http-dataset")
			result, err := observesource.CollectDataset(context.Background(), datasetRequest(source, p, output))
			if err != nil {
				t.Fatal(err)
			}
			if result.Usable() != (mode == "complete") {
				t.Fatalf("completeness: %s", result.Document().Status)
			}
			if mode == "complete" {
				facts := result.Document().Acquisition.Facts
				if facts == nil || facts.HTTPStatus != 200 || facts.Attempts != 1 || facts.Records != 1 || facts.Bytes == 0 || facts.AsOf.IsZero() {
					t.Fatal("acquisition facts lost")
				}
			}
			server.Close()
			reopened, err := observesource.OpenDataset(context.Background(), output)
			if err != nil || reopened.Identity() != result.Identity() || calls.Load() != 1 {
				t.Fatal("reopen accessed live HTTP", err)
			}
		})
	}
}
func TestTypedDatabaseUsesMultiColumnBoundQueryAndReopensDriverSnapshot(t *testing.T) {
	rows := [][][]byte{{[]byte("same"), []byte("2026-01-01 10:00:00"), []byte("9007199254740993.1200"), []byte("booked")}, {[]byte("same"), []byte("2026-01-02 10:00:00"), []byte("9007199254740993.1200"), []byte("moved")}}
	names := []string{"appointment", "start", "amount", "status"}
	oids := []uint32{25, 1114, 1700, 25}
	fields := []pgproto3.FieldDescription{}
	for i, n := range names {
		fields = append(fields, pgproto3.FieldDescription{Name: []byte(n), DataTypeOID: oids[i], DataTypeSize: -1, TypeModifier: -1})
	}
	queries := make(chan struct{}, 5)
	address, ca := postgresRowsFixture(t, rows, fields, `SELECT "appointment", "start", "amount", "status" FROM "public"."observed" WHERE "status" = $1 LIMIT `, false, false, queries)
	source := databaseDeclared(t, address, ca)
	p := datasetProjection("database")
	p.Order = "unordered"
	p.Columns = []dataset.Column{{Name: "key", Type: "text", Locator: importer.Locator{"appointment"}, Key: true, Required: true}, {Name: "start", Type: "datetime", Locator: importer.Locator{"start"}, Required: true}, {Name: "amount", Type: "decimal", Locator: importer.Locator{"amount"}, Required: true}, {Name: "status", Type: "text", Locator: importer.Locator{"status"}, Required: true}}
	out := filepath.Join(t.TempDir(), "database-dataset")
	result, err := observesource.CollectDataset(context.Background(), datasetRequest(source, p, out))
	if err != nil || !result.Usable() {
		if result != nil {
			t.Logf("status=%s", result.Document().Status)
			raw, _ := os.ReadFile(filepath.Join(out, "dataset", "material.bin"))
			t.Log(string(raw))
		}
		t.Fatal(err)
	}
	values := result.Document().Rows[0].Values
	if len(result.Document().Rows) != 2 || values[1].Timezone != "absent" || values[2].Text != "9007199254740993.1200" || result.Document().Material.Meaning != "typed-driver-result" {
		t.Fatal("driver precision or source meaning lost", values)
	}
	<-queries
	rows[0][3] = []byte("changed")
	os.Remove(source.Database.Credential.Arguments[0])
	reopened, err := observesource.OpenDataset(context.Background(), out)
	if err != nil || reopened.Document().Rows[0].Values[3].Text != "booked" {
		t.Fatal("live database re-read", err)
	}
	select {
	case <-queries:
		t.Fatal("reopen issued query")
	default:
	}
}
