package observeinterval_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestHTTPPaginationFailureInvalidatesFullIntervalCoverage(t *testing.T) {
	var pagination atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Age", "0")
		if pagination.Load() {
			w.Write([]byte(`{"records":[],"next":"uncollected-page"}`))
		} else {
			w.Write([]byte(`{"records":[],"next":""}`))
		}
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	source := observesource.Source{Schema: observesource.SchemaV1, Observes: observewindow.Source{Kind: observesource.HTTPAPI, Identity: "independent", Scope: "records"}, Enabled: true, Freshness: observesource.Freshness{MaxAge: "10s"}, HTTP: &observesource.HTTP{URL: server.URL, Classification: "nonproduction", ServerName: "example.com", Timeout: "1s", MaxBytes: 65536, Retry: observesource.Retry{Attempts: 0, Delay: "5ms"}}, Extraction: &observesource.Extraction{Envelope: importer.JSONEnvelope, Encoding: importer.UTF8, JSON: &importer.DocumentDialect{RecordPath: []string{"records"}}, RecordKey: importer.Locator{"id"}}}
	policy, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "records", Operation: sendpolicy.ObservationRead, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}})
	// Source CA path is only configuration; the scoped action pins the exact CA
	// bytes and the same source identity before it can read a response.
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	source.HTTP.CAFile = caPath
	projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "records", Format: "json", Order: "source", Continuation: importer.Locator{"next"}, Envelope: &dataset.Envelope{Encoding: importer.UTF8, JSON: source.Extraction.JSON}, Columns: []dataset.Column{{Name: "id", Type: "text", Locator: importer.Locator{"id"}, Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
	spec := networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: dataset.Digest([]byte("plan")), Source: source.Identity(), Project: "lab", Environment: "test", Revision: "1", Endpoint: "records", Classification: "nonproduction", Operation: sendpolicy.ObservationRead, Method: "GET", URL: server.URL, ServerName: "example.com", Authorities: ca, TimeoutMS: 1000, MaxBytes: 65536}
	encoded, _ := json.Marshal(spec)
	network, err := networkaction.PrepareHTTP(encoded, policy)
	if err != nil {
		t.Fatal(err)
	}
	binding := dataset.Binding{Run: "run-one", Phase: "after", Source: source.Identity(), Namespace: "records"}
	d := observeinterval.Definition{Schema: observeinterval.Schema, Source: binding.Source, Namespace: binding.Namespace, Enabled: true, Mode: "snapshots", Freshness: "snapshot-only", HorizonMS: 100, SampleMS: 10, MaxGapMS: 100, MaxSamples: 10, MaxRecords: 10, MaxBytes: 65536}
	clock := &clock{}
	session, err := observeinterval.Arm(context.Background(), d, binding, filepath.Join(t.TempDir(), "interval"), clock)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	acquire := func(name string) *dataset.Snapshot {
		snapshot, err := observesource.CollectDataset(context.Background(), observesource.DatasetRequest{Source: source, Projection: projection, Binding: binding, Output: filepath.Join(t.TempDir(), name), Network: network, NetworkAuthority: authority{network.Binding()}, Authorize: func(context.Context) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	baseline := acquire("baseline")
	if !baseline.Usable() {
		t.Fatal(baseline.Document())
	}
	if session.Append(context.Background(), observeinterval.Observation{Binding: binding, Status: "healthy", Snapshot: baseline}) != nil || session.StimulusStarted() != nil || session.StimulusFinished() != nil {
		t.Fatal("source not ready")
	}
	pagination.Store(true)
	clock.elapsed = 100
	partial := acquire("partial")
	if partial.Usable() {
		t.Fatal("unfetched pagination passed")
	}
	if err = session.Append(context.Background(), observeinterval.Observation{Binding: binding, Status: "healthy", Snapshot: partial}); err != nil {
		t.Fatal(err)
	}
	result, err := session.Finish(context.Background())
	if err != nil || result.Sufficient() {
		t.Fatal(result, err)
	}
}
