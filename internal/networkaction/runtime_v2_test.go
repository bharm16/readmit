package networkaction_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeHTTPV2BindsConditionsAndPreservesV1Rejection(t *testing.T) {
	calls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-Match") != `W/"7"` || r.Header.Get("Prefer") != "return=minimal" {
			t.Error("bound conditions missing")
		}
		w.Header().Set("ETag", `W/"8"`)
		w.Header().Set("Set-Cookie", "private-cookie")
		w.WriteHeader(200)
	}))
	defer s.Close()
	spec := networkaction.RuntimeHTTPSpecV2{Schema: networkaction.RuntimeHTTPSchemaV2, Accept: "application/fhir+json", Headers: networkaction.HTTPHeadersV2{IfMatch: `W/"7"`, Prefer: "return=minimal"}, HTTP: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("source")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: sendpolicy.FHIRAction, Method: "PUT", URL: s.URL + "/Patient/p", ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), TimeoutMS: 1000, MaxBytes: 4096}}
	raw, _ := json.Marshal(spec)
	p, e := networkaction.PrepareRuntimeHTTPV2(raw, policy(t, s.Listener.Addr().String(), sendpolicy.FHIRAction))
	if e != nil {
		t.Fatal(e)
	}
	a := approved(p.Binding())
	response, _, e := p.Execute(context.Background(), a, nil, nil)
	if e != nil || response.Metadata()["ETag"] != `W/"8"` || response.Metadata()["Set-Cookie"] != "" {
		t.Fatal("response metadata", e)
	}
	spec.Headers.IfMatch = `W/"9"`
	raw, _ = json.Marshal(spec)
	changed, e := networkaction.PrepareRuntimeHTTPV2(raw, policy(t, s.Listener.Addr().String(), sendpolicy.FHIRAction))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = changed.Execute(context.Background(), a, nil, nil); e == nil || calls != 1 {
		t.Fatal("changed header reused authority")
	}
	spec.Schema = networkaction.RuntimeHTTPSchema
	raw, _ = json.Marshal(spec)
	if _, e := networkaction.PrepareRuntimeHTTP(raw, policy(t, s.Listener.Addr().String(), sendpolicy.FHIRAction)); e == nil {
		t.Fatal("v1 accepted v2 headers")
	}
}
