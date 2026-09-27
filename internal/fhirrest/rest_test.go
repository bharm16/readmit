package fhirrest_test

import (
	"context"
	jsonv2 "encoding/json/v2"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/fhirrest"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const capability = `{"resourceType":"CapabilityStatement","status":"active","date":"2026-01-01","kind":"instance","fhirVersion":"4.0.1","format":["application/fhir+json"],"patchFormat":["application/json-patch+json"],"rest":[{"mode":"server","interaction":[{"code":"transaction"},{"code":"batch"}],"resource":[{"type":"Patient","versioning":"versioned-update","conditionalCreate":true,"conditionalUpdate":true,"conditionalRead":"full-support","readHistory":true,"updateCreate":true,"searchInclude":["Patient:general-practitioner"],"interaction":[{"code":"read"},{"code":"vread"},{"code":"search-type"},{"code":"create"},{"code":"update"},{"code":"patch"},{"code":"delete"}],"searchParam":[{"name":"identifier","type":"token"},{"name":"active","type":"token"}]}]}]}`

type grant struct {
	binding networkaction.Binding
	actor   networkaction.Actor
}

func (a grant) Check(_ context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	if a.binding != b {
		return networkaction.Actor{}, fmt.Errorf("denied")
	}
	return a.actor, nil
}
func admission(p *fhirrest.Plan) grant {
	return grant{p.Binding(), networkaction.Actor{Kind: "runner", ID: "fixture", Generation: "1", EvidenceIdentity: networkaction.Digest([]byte("independent-grant")), Expires: time.Now().Add(time.Hour)}}
}

type server struct {
	holdCreate      time.Duration
	history         map[string]map[int][]byte
	statusOnce      bool
	capOverride     string
	pages           []string
	statusOverride  int
	bodyOverride    []byte
	headersOverride map[string]string
	requests        int
	t               *testing.T
	s               *httptest.Server
	mu              sync.Mutex
	resources       map[string][]byte
	versions        map[string]int
	creates         int
	dropCreate      bool
	policy          []byte
}

func newServer(t *testing.T) *server {
	t.Helper()
	f := &server{history: map[string]map[int][]byte{}, t: t, resources: map[string][]byte{}, versions: map[string]int{}}
	f.s = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/fhir+json")
		if r.Header.Get("Accept") != "application/fhir+json" {
			w.WriteHeader(406)
			return
		}
		if r.URL.Path == "/fhir/metadata" {
			if f.capOverride != "" {
				w.Write([]byte(f.capOverride))
			} else {
				w.Write([]byte(capability))
			}
			return
		}
		f.requests++
		if f.statusOverride != 0 {
			code := f.statusOverride
			if f.statusOnce {
				f.statusOverride = 0
			}
			for k, v := range f.headersOverride {
				w.Header().Set(k, v)
			}
			w.WriteHeader(code)
			w.Write(f.bodyOverride)
			return
		}
		if len(f.pages) > 0 && (r.Method == "GET" || strings.HasSuffix(r.URL.Path, "/_search")) {
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page < 1 {
				page = 1
			}
			if page > len(f.pages) {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(strings.ReplaceAll(f.pages[page-1], "BASE", f.s.URL+"/fhir")))
			return
		}
		body, _ := io.ReadAll(r.Body)
		code, headers, response := f.respond(r.Method, strings.TrimPrefix(r.URL.RequestURI(), "/fhir/"), r.Header, body)
		if f.dropCreate && r.Method == "POST" && r.URL.Path == "/fhir/Patient" && code == 201 {
			if f.holdCreate > 0 {
				time.Sleep(f.holdCreate)
			}
			conn, _, e := w.(http.Hijacker).Hijack()
			if e == nil {
				conn.Close()
			}
			return
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(code)
		w.Write(response)

	}))
	t.Cleanup(f.s.Close)
	host, port, _ := net.SplitHostPort(f.s.Listener.Addr().String())
	number, _ := strconv.Atoi(port)
	ip := netip.MustParseAddr(host)
	rules := []sendpolicy.ScopeRule{}
	for _, op := range []sendpolicy.Operation{sendpolicy.FHIRMetadata, sendpolicy.FHIRSearch, sendpolicy.FHIRAction, sendpolicy.SetupAction} {
		rules = append(rules, sendpolicy.ScopeRule{Endpoint: "fixture", Operation: op, Port: number, Destinations: []string{netip.PrefixFrom(ip, ip.BitLen()).String()}, Selection: "single-address"})
	}
	f.policy, _ = jsonv2.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: rules})
	return f
}
func (f *server) spec(method, path string, body []byte) fhirrest.Spec {
	op := sendpolicy.FHIRAction
	if method == "GET" {
		op = sendpolicy.FHIRSearch
	}
	if method == "DELETE" {
		op = sendpolicy.SetupAction
	}
	ct := ""
	if len(body) > 0 {
		ct = "application/fhir+json"
	}
	return fhirrest.Spec{Schema: fhirrest.PlanSchema, Base: f.s.URL + "/fhir", HTTP: networkaction.RuntimeHTTPSpecV2{Schema: networkaction.RuntimeHTTPSchemaV2, Accept: "application/fhir+json", HTTP: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("parent-plan")), Source: networkaction.Digest([]byte("source")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "fixture", Classification: "nonproduction", Operation: op, Method: method, URL: f.s.URL + "/fhir" + path, Body: body, ContentType: ct, ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.s.Certificate().Raw}), TimeoutMS: 1000, MaxBytes: 1 << 20}}, Capability: []byte(capability), Budget: fhirrest.Budget{Pages: 8, Rows: 100, Bytes: 8 << 20, TimeoutMS: 10000}, Retry: fhirrest.Retry{MaxAttempts: 2, MaxDelayMS: 100}}
}
func (f *server) execute(spec fhirrest.Spec) (fhirrest.Result, string) {
	f.t.Helper()
	raw, _ := jsonv2.Marshal(spec)
	p, e := fhirrest.Prepare(raw, f.policy)
	if e != nil {
		f.t.Fatal(e)
	}
	output := filepath.Join(f.t.TempDir(), "result")
	r, e := p.Execute(context.Background(), admission(p), nil, output, nil)
	if e != nil {
		f.t.Fatal(e)
	}
	opened, e := fhirrest.Open(context.Background(), output)
	if e != nil || opened.State != r.State {
		f.t.Fatal("offline reopen", e, r.State)
	}
	return r, output
}
func TestFHIRRESTCreateReadAndCommittedResponseLoss(t *testing.T) {
	f := newServer(t)
	r, _ := f.execute(f.spec("POST", "/Patient", []byte(`{"resourceType":"Patient","active":true}`)))
	if r.State != "succeeded" || f.creates != 1 {
		t.Fatal(r.State, f.creates)
	}
	r, _ = f.execute(f.spec("GET", "/Patient/1", nil))
	if r.State != "succeeded" {
		t.Fatal(r.State)
	}
	f.dropCreate = true
	r, _ = f.execute(f.spec("POST", "/Patient", []byte(`{"resourceType":"Patient","active":false}`)))
	if r.State != "delivery-uncertain" || f.creates != 2 {
		t.Fatal("committed create retried or uncertainty lost", r.State, f.creates)
	}
	if len(r.Attempts) != 2 {
		t.Fatal("write retry occurred")
	}
}

func TestFHIRRESTUpdatesConditionsAndVersionConflict(t *testing.T) {
	f := newServer(t)
	patient := []byte(`{"resourceType":"Patient","identifier":[{"system":"lab","value":"same"}],"active":true}`)
	spec := f.spec("POST", "/Patient", patient)
	spec.HTTP.Headers.IfNoneExist = "identifier=lab%7Csame"
	r, _ := f.execute(spec)
	if r.Attempts[1].Outcome.ConditionalMatches != "zero" || f.creates != 1 {
		t.Fatal("conditional create zero", r.State)
	}
	r, _ = f.execute(spec)
	if r.Attempts[1].Outcome.ConditionalMatches != "one" || f.creates != 1 {
		t.Fatal("conditional create one")
	}
	update := f.spec("PUT", "/Patient/1", []byte(`{"resourceType":"Patient","id":"1","active":false}`))
	update.HTTP.Headers.IfMatch = `W/"1"`
	r, _ = f.execute(update)
	if r.State != "succeeded" || f.versions["1"] != 2 {
		t.Fatal("versioned update", r.State)
	}
	r, _ = f.execute(update)
	if r.State != "conflict" || f.versions["1"] != 2 {
		t.Fatal("version conflict accepted", r.State)
	}
	patch := f.spec("PATCH", "/Patient/1", []byte(`[{"op":"replace","path":"/active","value":true}]`))
	patch.HTTP.HTTP.ContentType = "application/json-patch+json"
	patch.HTTP.Headers.IfMatch = `W/"2"`
	r, _ = f.execute(patch)
	if r.State != "succeeded" || !strings.Contains(string(f.resources["1"]), `"active":true`) {
		t.Fatal("JSON Patch ineffective", r.State)
	}
	conditional := f.spec("PUT", "/Patient?identifier=lab%7Csame", patient)
	r, _ = f.execute(conditional)
	if r.Attempts[1].Outcome.ConditionalMatches != "zero" {
		t.Fatal("conditional update zero", r.State)
	}
	r, _ = f.execute(conditional)
	if r.Attempts[1].Outcome.ConditionalMatches != "one" {
		t.Fatal("conditional update one", r.State)
	}
	f.execute(f.spec("POST", "/Patient", patient))
	r, _ = f.execute(conditional)
	if r.State != "conflict" || r.Attempts[1].Outcome.ConditionalMatches != "multiple" {
		t.Fatal("conditional update multiple", r.State)
	}
	r, _ = f.execute(spec)
	if r.State != "conflict" || r.Attempts[1].Outcome.ConditionalMatches != "multiple" {
		t.Fatal("conditional create multiple", r.State)
	}
	r, _ = f.execute(f.spec("DELETE", "/Patient/1", nil))
	if r.State != "succeeded" {
		t.Fatal("204 delete", r.State)
	}
}
func TestFHIRRESTTransactionsRollbackAndBatchEntryFailures(t *testing.T) {
	f := newServer(t)
	success := []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","active":true}},{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","active":false}}]}`)
	r, _ := f.execute(f.spec("POST", "", success))
	if r.State != "succeeded" || len(f.resources) != 2 || len(r.Attempts[1].Outcome.Entries) != 2 {
		t.Fatal("transaction", r.State, len(f.resources))
	}
	failed := []byte(`{"resourceType":"Bundle","type":"transaction","entry":[{"request":{"method":"POST","url":"Patient"},"resource":{"resourceType":"Patient","active":true}},{"request":{"method":"GET","url":"Patient/missing"}}]}`)
	r, _ = f.execute(f.spec("POST", "", failed))
	if r.State != "not-found" && r.State != "rejected-transaction" {
		t.Fatal("transaction failure", r.State)
	}
	if len(f.resources) != 2 {
		t.Fatal("transaction did not roll back independent target")
	}
	batch := []byte(strings.Replace(string(failed), `"transaction"`, `"batch"`, 1))
	r, _ = f.execute(f.spec("POST", "", batch))
	if r.State != "partial-failure" || len(f.resources) != 3 || len(r.Attempts[1].Outcome.Entries) != 2 {
		t.Fatal("batch incorrectly atomic or all-pass", r.State, len(f.resources))
	}
}

func TestFHIRRESTTimeoutAfterCommittedCreateNeverRetries(t *testing.T) {
	f := newServer(t)
	f.dropCreate = true
	f.holdCreate = 800 * time.Millisecond
	spec := f.spec("POST", "/Patient", []byte(`{"resourceType":"Patient","active":true}`))
	spec.HTTP.HTTP.TimeoutMS = 500
	r, _ := f.execute(spec)
	f.mu.Lock()
	count := f.creates
	f.mu.Unlock()
	if r.State != "delivery-uncertain" || count != 1 || len(r.Attempts) != 2 {
		t.Fatal("timed-out committed create retried", r.State, count)
	}
}
