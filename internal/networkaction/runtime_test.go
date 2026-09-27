package networkaction_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

type runtimeProvider struct {
	guard    func(context.Context) error
	calls    int
	identity string
}

func (p *runtimeProvider) Identity() string { return p.identity }
func (p *runtimeProvider) Material(ctx context.Context, target networkaction.RuntimeTarget) (networkaction.RuntimeMaterial, error) {
	p.calls++
	return networkaction.BearerMaterial([]byte("private-fixture-token")).WhileAuthorized(p.guard), nil
}
func TestRuntimeHTTPUsesMemoryCredentialOnlyAfterAdmission(t *testing.T) {
	calls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer private-fixture-token" {
			t.Error("missing runtime token")
		}
		w.Write([]byte("synthetic"))
	}))
	defer s.Close()
	provider := &runtimeProvider{identity: networkaction.Digest([]byte("config"))}
	spec := networkaction.RuntimeHTTPSpec{Schema: networkaction.RuntimeHTTPSchema, Authorization: provider.Identity(), HTTP: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("source")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: sendpolicy.FHIRSearch, Method: "GET", URL: s.URL + "/Patient", ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), TimeoutMS: 1000, MaxBytes: 4096}}
	raw, _ := json.Marshal(spec)
	plan, err := networkaction.PrepareRuntimeHTTP(raw, policy(t, s.Listener.Addr().String(), sendpolicy.FHIRSearch))
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 || calls != 0 {
		t.Fatal("passive preparation had effects")
	}
	a := approved(plan.Binding())
	bad := a
	bad.binding.Configuration = "wrong"
	if _, _, err := plan.Execute(context.Background(), bad, nil, provider); err == nil || provider.calls != 0 {
		t.Fatal("resolved before authority")
	}
	response, receipt, err := plan.Execute(context.Background(), a, nil, provider)
	if err != nil || response.Status != 200 || calls != 1 || provider.calls != 1 {
		t.Fatal(err, receipt)
	}
	provider.guard = func(context.Context) error { return context.Canceled }
	if _, _, err := plan.Execute(context.Background(), a, nil, provider); err == nil || calls != 1 {
		t.Fatal("disconnected provider sent after material resolution")
	}
	b, _ := json.Marshal(receipt)
	if strings.Contains(string(b)+fmt.Sprintf("%+v %#v", response, response), "private-fixture-token") {
		t.Fatal("secret exposed")
	}
}

func TestRuntimeHTTPDeniesDestinationBeforeMaterialAndNeverFollowsRedirect(t *testing.T) {
	targetCalls := 0
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer other.Close()
	sourceCalls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls++
		http.Redirect(w, r, other.URL+"/stolen", 302)
	}))
	defer s.Close()
	provider := &runtimeProvider{identity: networkaction.Digest([]byte("bound-provider"))}
	spec := networkaction.RuntimeHTTPSpec{Schema: networkaction.RuntimeHTTPSchema, Authorization: provider.identity, HTTP: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("source")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: sendpolicy.FHIRSearch, Method: "GET", URL: s.URL + "/Patient", ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), TimeoutMS: 1000, MaxBytes: 4096}}
	raw, _ := json.Marshal(spec)
	denied, _ := networkaction.PrepareRuntimeHTTP(raw, policy(t, other.Listener.Addr().String(), sendpolicy.FHIRSearch))
	if _, _, e := denied.Execute(context.Background(), approved(denied.Binding()), nil, provider); e == nil || provider.calls != 0 || sourceCalls != 0 {
		t.Fatal("destination denial resolved provider")
	}
	allowed, e := networkaction.PrepareRuntimeHTTP(raw, policy(t, s.Listener.Addr().String(), sendpolicy.FHIRSearch))
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("HTTPS_PROXY", other.URL)
	t.Setenv("HTTP_PROXY", other.URL)
	if _, _, e = allowed.Execute(context.Background(), approved(allowed.Binding()), nil, provider); e == nil || sourceCalls != 1 || targetCalls != 0 {
		t.Fatal("redirect or ambient proxy escaped")
	}
}
