package networkaction_test

import (
	"context"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestFileAuthorityAdmissionRefusesBeforeTargetAndRetainsExactGrant(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	provider := &runtimeProvider{identity: networkaction.Digest([]byte("registered-provider"))}
	var resolutions atomic.Int32
	resolve := func(context.Context, string) ([]netip.Addr, error) {
		resolutions.Add(1)
		return []netip.Addr{netip.MustParseAddr(host)}, nil
	}
	spec := networkaction.RuntimeHTTPSpecV2{Schema: networkaction.RuntimeHTTPSchemaV2, Authorization: provider.Identity(), Accept: "application/fhir+json", HTTP: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("source")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "endpoint", Classification: "nonproduction", Operation: sendpolicy.FHIRAction, Method: "PUT", URL: "https://runner-target.test:" + port + "/Patient/p", ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), TimeoutMS: 1000, MaxBytes: 4096}}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	p, err := networkaction.PrepareRuntimeHTTPV2(raw, policy(t, server.Listener.Addr().String(), sendpolicy.FHIRAction))
	if err != nil {
		t.Fatal(err)
	}
	a := networkaction.FileAuthority{Path: filepath.Join(t.TempDir(), "grant.json"), Actor: "runner", Generation: "1"}
	grant := networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: a.Actor, Generation: a.Generation, Binding: p.Binding(), IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)}
	raw, err = json.Marshal(grant)
	if err != nil || os.WriteFile(a.Path, raw, 0600) != nil {
		t.Fatal("could not provision grant", err)
	}
	denied := errors.New("runner fence changed")
	ctx := networkaction.WithAdmission(t.Context(), func(context.Context) error { return denied })
	if _, err = a.Check(ctx, p.Binding()); !errors.Is(err, denied) {
		t.Fatal("admission reason was not returned to the runner", err)
	}
	if _, _, err = p.Execute(ctx, a, resolve, provider); err == nil || requests.Load() != 0 || resolutions.Load() != 0 || provider.calls != 0 {
		t.Fatal("refused runner admission reached DNS, credential provider or target", err)
	}
	ctx = networkaction.WithAdmission(t.Context(), func(context.Context) error { return nil })
	if _, _, err = p.Execute(ctx, a, resolve, provider); err != nil || requests.Load() != 1 || resolutions.Load() != 1 || provider.calls != 1 {
		t.Fatal("current runner and exact grant did not execute", err)
	}
	wrong := p.Binding()
	wrong.Operation = sendpolicy.SetupAction
	if _, err = a.Check(ctx, wrong); err == nil {
		t.Fatal("runtime admission bypassed the exact grant")
	}
	// Selecting the provisioned file as an explicit runtime authority still
	// checks that file exactly, without recursively selecting the override.
	ctx = networkaction.WithAuthority(ctx, a)
	if _, err = a.Check(ctx, p.Binding()); err != nil {
		t.Fatal("runtime delegation did not reach the exact file grant", err)
	}
}

func TestFileAuthorityNestedAdmissionsRecheckEveryLease(t *testing.T) {
	denied := errors.New("outer lease expired")
	var revoked atomic.Bool
	outer := networkaction.WithAdmission(t.Context(), func(context.Context) error {
		if revoked.Load() {
			return denied
		}
		return nil
	})
	ctx := networkaction.WithAdmission(outer, func(context.Context) error { return nil })
	a := networkaction.FileAuthority{Path: filepath.Join(t.TempDir(), "unprovisioned"), Actor: "runner", Generation: "1"}
	revoked.Store(true)
	if _, err := a.Check(ctx, networkaction.Binding{}); !errors.Is(err, denied) {
		t.Fatal("nested admission hid the outer lease refusal", err)
	}
}

func TestFileAuthorityExplicitRuntimeAuthorityStillRequiresExactBindingAndAdmission(t *testing.T) {
	binding := networkaction.Binding{Plan: networkaction.Digest([]byte("current compiled occurrence")), Project: "lab", Environment: "test", Endpoint: "fixture", Operation: sendpolicy.SetupAction}
	a := networkaction.FileAuthority{Path: filepath.Join(t.TempDir(), "unprovisioned"), Actor: "runner", Generation: "1"}
	if _, err := a.Check(t.Context(), binding); err == nil {
		t.Fatal("ordinary context bypassed its unprovisioned grant")
	}
	runtime := approved(binding)
	ctx := networkaction.WithAuthority(t.Context(), runtime)
	actor, err := a.Check(ctx, binding)
	if err != nil || actor != runtime.actor {
		t.Fatal("explicit runtime authority did not admit its exact occurrence", err)
	}
	wrong := binding
	wrong.Plan = networkaction.Digest([]byte("another occurrence"))
	if _, err = a.Check(ctx, wrong); err == nil {
		t.Fatal("runtime authority admitted a different compiled occurrence")
	}
	denied := errors.New("fence changed")
	ctx = networkaction.WithAdmission(ctx, func(context.Context) error { return denied })
	if _, err = a.Check(ctx, binding); !errors.Is(err, denied) {
		t.Fatal("runtime authority bypassed live admission", err)
	}
}
