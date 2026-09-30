package desktop_test

import (
	cctx "context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

func TestFHIRConnectionSavesAsOneNamedRevisionWithoutReachingIt(t *testing.T) {
	app, context := namedProject(t)
	draft := desktop.ItemDraft{Name: "FHIR QA", FHIR: &desktop.FHIRConnection{
		Schema: desktop.FHIRConnectionSchema, Base: "https://unreachable.invalid/fhir", Version: "4.0.1",
		Classification: replay.Unclassified, Authentication: "none", ServerName: "unreachable.invalid",
	}}
	ref := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: draft, IntentID: "fhir-new"})
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
	if opened.State != desktop.Completed || opened.Draft == nil || opened.Draft.FHIR == nil || opened.Draft.FHIR.Base != draft.FHIR.Base || opened.Draft.Environment != nil {
		t.Fatalf("reopen saved FHIR connection: %+v", opened)
	}
	row := catalogRow(t, app, context, ref)
	if row.Summary.Environment.Protocol != "fhir-r4" || row.Summary.Environment.Classification != "unclassified" || row.Summary.Environment.LastCheckedAt != nil {
		t.Fatalf("passive summary changed meaning: %+v", row.Summary.Environment)
	}
	draft.FHIR.Base = "https://unreachable.invalid/fhir-v2"
	edited := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: ref.ID, BaseRevision: ref.Revision, Draft: draft, IntentID: "fhir-edit"})
	if edited.Outcome != desktop.SavedOutcome {
		t.Fatalf("edit: %+v", edited)
	}
	stale := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: ref.ID, BaseRevision: ref.Revision, Draft: draft, IntentID: "fhir-stale"})
	if stale.Outcome != desktop.ConflictOutcome || stale.Projection != nil {
		t.Fatalf("stale edit: %+v", stale)
	}
}

func TestMissingLocalFHIRValidatorKeepsTheSavedConnectionAndNeverStartsAWorker(t *testing.T) {
	app, ctx := namedProject(t)
	draft := desktop.ItemDraft{Name: "FHIR with local validation", FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: "https://qa.invalid/fhir", ServerName: "qa.invalid", Classification: replay.Unclassified, Authentication: "none", Validation: &desktop.ConnectionValidation{Capability: filepath.Join(t.TempDir(), "missing-capability"), Engine: "local"}}}
	ref := saveEnvironment(t, app, ctx, desktop.SaveItemRequest{Draft: draft, IntentID: "missing-worker"})
	opened := app.OpenItemDraft(desktop.ItemRequest{Context: ctx, Ref: ref})
	if opened.State != desktop.Completed || opened.Draft.FHIR.Validation == nil {
		t.Fatalf("missing worker lost saved configuration: %+v", opened)
	}
	row := catalogRow(t, app, ctx, ref)
	if row.Summary.Environment.Validator != "capability-unavailable" {
		t.Fatalf("missing capability was usable: %+v", row.Summary.Environment)
	}
	inventory := app.ListConnections(ctx)
	found := false
	for _, connection := range inventory.Rows {
		if connection.Ref == "environment:"+ref.ID {
			found = true
			if connection.Detail.Validator != "capability-unavailable" || connection.State != desktop.ConnectionNotChecked {
				t.Fatalf("inventory implies running validator: %+v", connection)
			}
		}
		if connection.Kind == desktop.ConnectionProgram {
			t.Fatal("listing a local validator started a program")
		}
	}
	if !found {
		t.Fatal("configured connection absent from inventory")
	}
}

func TestFHIRPassiveCatalogAndSaveResolveNoDNSOrCredentialAndConnectivitySendsNoHTTP(t *testing.T) {
	app, ctx := namedProject(t)
	var dns, connections, requests atomic.Int32
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(cctx.Context, string, string) (net.Conn, error) {
		dns.Add(1)
		return nil, errors.New("DNS observed")
	}}
	defer func() { net.DefaultResolver = oldResolver }()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "credential-read")
	provider := filepath.Join(t.TempDir(), "provider.sh")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nprintf accessed > '"+marker+"'\nprintf unavailable\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if registered := app.SaveCredential(desktop.CredentialSaveRequest{Context: ctx, Name: "signing-key", Purpose: secret.SourceEndpoint, Store: secret.OSKeychain, Address: server.Listener.Addr().String(), Command: provider, Arguments: []string{}}); registered.State != desktop.Completed {
		t.Fatalf("register: %+v", registered)
	}
	draft := desktop.ItemDraft{Name: "Passive FHIR", FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: "https://never-resolve.invalid/fhir", ServerName: "never-resolve.invalid", Authentication: "smart", ClientID: "qa-client", TokenEndpoint: server.URL + "/token", Algorithm: "RS384", Scopes: []string{"system/Appointment.rs"}, KeyReference: "signing-key", KeyID: "qa-key", PublicKeysFile: "not-yet-selected.json", Classification: replay.Unclassified}}
	ref := saveEnvironment(t, app, ctx, desktop.SaveItemRequest{Draft: draft, IntentID: "passive-new"})
	_ = app.ListCatalog(desktop.CatalogQuery{Context: ctx, Kind: desktop.EnvironmentItem})
	_ = app.OpenItem(desktop.ItemRequest{Context: ctx, Ref: ref})
	_ = app.OpenItemDraft(desktop.ItemRequest{Context: ctx, Ref: ref})
	_ = app.ListCredentials(desktop.ItemRequest{Context: ctx, Ref: ref})
	_ = app.ListConnections(ctx)
	if dns.Load() != 0 || connections.Load() != 0 || requests.Load() != 0 {
		t.Fatalf("passive activity: DNS=%d connections=%d requests=%d", dns.Load(), connections.Load(), requests.Load())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("passive configuration resolved its credential")
	}
	draft.FHIR.Base = server.URL + "/fhir"
	draft.FHIR.ServerName = "example.com"
	draft.FHIR.CAFile = ca
	draft.FHIR.Classification = replay.Nonproduction
	draft.SendPolicy = &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: ctx, Kind: desktop.EnvironmentItem, Item: ref.ID, BaseRevision: ref.Revision, Draft: draft, IntentID: "passive-edit"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("edit: %+v", saved)
	}
	review := app.PrepareAction(desktop.PrepareActionRequest{Context: ctx, Action: desktop.CheckFHIRConnectionAction, Items: []desktop.ItemRef{*saved.Saved}})
	if review.Review == nil || !review.Review.Ready {
		t.Fatalf("connectivity review: %+v", review)
	}
	if connections.Load() != 0 {
		t.Fatal("review opened TLS")
	}
	result := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: review.Review.Token, IntentID: "only-connect"})
	if result.State != desktop.Completed || connections.Load() != 1 || requests.Load() != 0 || dns.Load() != 0 {
		t.Fatalf("connectivity crossed into HTTP/auth: %+v conn=%d http=%d dns=%d", result, connections.Load(), requests.Load(), dns.Load())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("connectivity resolved a signing key")
	}
}

func TestFHIRCapabilitiesRequireTheirExplicitReviewAndBecomeStaleAfterEdit(t *testing.T) {
	app, context := namedProject(t)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/fhir/metadata" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"CapabilityStatement","status":"active","date":"2026-09-29","kind":"instance","fhirVersion":"4.0.1","format":["json"],"rest":[{"mode":"server","resource":[{"type":"Patient","interaction":[{"code":"search-type"}],"searchParam":[{"name":"identifier","type":"token"}]}]}]}`))
	}))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	draft := desktop.ItemDraft{Name: "FHIR local", FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: server.URL + "/fhir", ServerName: "example.com", CAFile: ca, Classification: replay.Nonproduction, Authentication: "none"}, SendPolicy: &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.0/8"}}}
	ref := saveEnvironment(t, app, context, desktop.SaveItemRequest{Draft: draft, IntentID: "fhir-capability-new"})
	_ = catalogRow(t, app, context, ref)
	_ = app.OpenItemDraft(desktop.ItemRequest{Context: context, Ref: ref})
	_ = app.ListConnections(context)
	prepared := app.PrepareAction(desktop.PrepareActionRequest{Context: context, Action: desktop.CheckFHIRCapabilitiesAction, Items: []desktop.ItemRef{ref}})
	if prepared.Review == nil || !prepared.Review.Ready {
		t.Fatalf("capability review: %+v", prepared)
	}
	if requests.Load() != 0 {
		t.Fatal("save, navigation or review contacted the server")
	}
	checked := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: context, Token: prepared.Review.Token, IntentID: "fhir-capability-check"})
	if checked.State != desktop.Completed || checked.FHIRCheck == nil || checked.FHIRCheck.Claims == nil || requests.Load() != 1 {
		t.Fatalf("explicit check: %+v (%d requests)", checked, requests.Load())
	}
	draft.FHIR.Authentication = "none"
	draft.FHIR.ServerName = "changed.invalid"
	edited := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.EnvironmentItem, Item: ref.ID, BaseRevision: ref.Revision, Draft: draft, IntentID: "fhir-capability-edit"})
	if edited.Outcome != desktop.SavedOutcome {
		t.Fatalf("edit: %+v", edited)
	}
	row := catalogRow(t, app, context, *edited.Saved)
	if row.Summary.Environment.Capabilities == nil || row.Summary.Environment.Capabilities.Revision == edited.Saved.Revision {
		t.Fatal("the old capability became current after a TLS edit")
	}
	if requests.Load() != 1 {
		t.Fatal("editing refreshed capabilities")
	}
}
