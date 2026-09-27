package observesource_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"github.com/bharm16/readmit/internal/artifactdir"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

type datasetAuthority struct {
	binding networkaction.Binding
	actor   networkaction.Actor
}

func (a datasetAuthority) Check(_ context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if a.binding != b {
		return networkaction.Actor{}, context.Canceled
	}
	return a.actor, nil
}
func TestTypedHTTPAcquisitionConsumesScopedNetworkAuthority(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Age", "0")
		w.Write([]byte(`{"appointments":[{"id":"one","status":"moved"}]}`))
	}))
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	source, _ := declaredHTTP(t, ca, strings.TrimPrefix(server.URL, "https://"), declaredHTTPSource)
	projection := datasetProjection("json")
	projection.Columns[0].Locator = importer.Locator{"id"}
	projection.Continuation = importer.Locator{"next"}
	projection.Envelope = &dataset.Envelope{Encoding: importer.UTF8, JSON: source.Extraction.JSON}
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	policy, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "observation", Operation: sendpolicy.ObservationRead, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}})
	raw, _ := json.Marshal(networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: source.Identity(), Project: "lab", Environment: "test", Revision: "1", Endpoint: "observation", Classification: "nonproduction", Operation: sendpolicy.ObservationRead, Method: "GET", URL: source.HTTP.URL, ServerName: "127.0.0.1", Authorities: ca, TimeoutMS: projection.Limits.TimeoutMS, MaxBytes: projection.Limits.MaxBytes})
	action, err := networkaction.PrepareHTTP(raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	request := datasetRequest(source, projection, filepath.Join(t.TempDir(), "scoped-dataset"))
	request.Network = action
	request.NetworkAuthority = datasetAuthority{binding: action.Binding(), actor: networkaction.Actor{Kind: "runner", ID: "reader", Generation: "1", EvidenceIdentity: networkaction.Digest([]byte("grant")), Expires: time.Now().Add(time.Hour)}}
	result, err := observesource.CollectDataset(context.Background(), request)
	if err != nil || !result.Usable() {
		t.Fatal(err)
	}
	server.Close()
	reopened, err := observesource.OpenDataset(context.Background(), request.Output)
	if err != nil || reopened.Identity() != result.Identity() {
		t.Fatal(err)
	}
}

func TestTypedDatabaseAcquisitionConsumesScopedAuthority(t *testing.T) {
	address, ca := postgresFixture(t, [][]byte{[]byte("A1")}, 25, false, false)
	source := databaseDeclared(t, address, ca)
	projection := datasetProjection("database")
	projection.Order = "unordered"
	projection.Columns = projection.Columns[:1]
	_, port, _ := net.SplitHostPort(address)
	number, _ := strconv.Atoi(port)
	policy, _ := json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: []sendpolicy.ScopeRule{{Endpoint: "observation", Operation: sendpolicy.ObservationRead, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"}}})
	action, err := observesource.PrepareDatabaseAction(source, projection, observesource.DatabaseActionContext{Plan: networkaction.Digest([]byte("plan")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "observation", CredentialGeneration: "1"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	request := datasetRequest(source, projection, filepath.Join(t.TempDir(), "scoped-db"))
	request.DatabaseNetwork = action
	request.NetworkAuthority = datasetAuthority{binding: action.Binding(), actor: networkaction.Actor{Kind: "runner", ID: "reader", Generation: "1", EvidenceIdentity: networkaction.Digest([]byte("grant")), Expires: time.Now().Add(time.Hour)}}
	result, err := observesource.CollectDataset(context.Background(), request)
	if err != nil || !result.Usable() {
		t.Fatal(err)
	}
	if _, err := observesource.OpenDataset(context.Background(), request.Output); err != nil {
		t.Fatal("database scope did not reopen", err)
	}
	network := filepath.Join(request.Output, "network")
	rawResult, _ := os.ReadFile(filepath.Join(network, "result.json"))
	for _, mutate := range []func(*networkaction.Result){func(r *networkaction.Result) { r.Schema = "unknown/v1" }, func(r *networkaction.Result) { r.Actor.ID = "" }, func(r *networkaction.Result) { r.Actor.Generation = "" }, func(r *networkaction.Result) { r.ResponseDigest = "" }} {
		var r networkaction.Result
		json.Unmarshal(rawResult, &r)
		mutate(&r)
		raw, _ := json.Marshal(r, json.Deterministic(true))
		os.WriteFile(filepath.Join(network, "result.json"), raw, 0600)
		files := map[string][]byte{}
		entries, _ := os.ReadDir(network)
		for _, entry := range entries {
			files[entry.Name()], _ = os.ReadFile(filepath.Join(network, entry.Name()))
		}
		identity := artifactdir.Identity(networkaction.ResultSchema, files)
		os.WriteFile(filepath.Join(network, "identity.sha256"), []byte(identity+"\n"), 0600)
		if _, err := observesource.OpenDatabaseAction(network); err == nil {
			t.Fatal("invalid database authority accepted")
		}
	}
}
