package desktop_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/smartbackend"
)

func TestFHIRAuthorizationUsesRegisteredKeyAndChangedKeyOrPolicyStalesTheReview(t *testing.T) {
	app, ctx := namedProject(t)
	var tokens, metadata atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			metadata.Add(1)
			t.Error("authorization test contacted FHIR resources")
			return
		}
		tokens.Add(1)
		if r.Method != "POST" || r.ParseForm() != nil || r.Form.Get("client_assertion") == "" || r.Form.Get("scope") != "system/Appointment.rs" {
			t.Error("not a SMART backend token request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-private-bearer","token_type":"Bearer","expires_in":120,"scope":"system/Appointment.rs"}`))
	}))
	defer server.Close()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateRaw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(t.TempDir(), "private.pem")
	if err := os.WriteFile(private, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateRaw}), 0600); err != nil {
		t.Fatal(err)
	}
	public := smartbackend.JWKS{Keys: []smartbackend.JWK{{Kty: "EC", Kid: "qa-key", Alg: "ES384", Use: "sig", Crv: "P-384", X: base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 48))), Y: base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 48)))}}}
	publicRaw, _ := json.Marshal(public)
	publicFile := filepath.Join(t.TempDir(), "public.json")
	if err := os.WriteFile(publicFile, publicRaw, 0600); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	credential := desktop.CredentialSaveRequest{Context: ctx, Name: "smart-signing", Purpose: secret.SourceEndpoint, Store: secret.OSKeychain, Address: server.Listener.Addr().String(), Command: "/bin/cat", Arguments: []string{private}}
	if saved := app.SaveCredential(credential); saved.State != desktop.Completed {
		t.Fatalf("key reference: %+v", saved)
	}
	draft := desktop.ItemDraft{Name: "SMART QA", FHIR: &desktop.FHIRConnection{Schema: desktop.FHIRConnectionSchema, Version: "4.0.1", Base: server.URL + "/fhir", ServerName: "example.com", CAFile: ca, Authentication: "smart", Classification: replay.Nonproduction, ClientID: "qa-client", TokenEndpoint: server.URL + "/token", Algorithm: "ES384", Scopes: []string{"system/Appointment.rs"}, KeyReference: "smart-signing", KeyID: "qa-key", PublicKeysFile: publicFile}, SendPolicy: &sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"127.0.0.1/32"}}}
	ref := saveEnvironment(t, app, ctx, desktop.SaveItemRequest{Draft: draft, IntentID: "smart-connection"})
	prepare := func(ref desktop.ItemRef) desktop.ActionReviewResult {
		return app.PrepareAction(desktop.PrepareActionRequest{Context: ctx, Action: desktop.CheckFHIRAuthorizationAction, Items: []desktop.ItemRef{ref}})
	}
	review := prepare(ref)
	if review.Review == nil || !review.Review.Ready {
		t.Fatalf("authorization review: %+v", review)
	}
	if tokens.Load() != 0 {
		t.Fatal("saving or reviewing requested a token")
	}
	checked := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: review.Review.Token, IntentID: "authorize-client"})
	if checked.State != desktop.Completed || checked.FHIRCheck == nil || checked.FHIRCheck.Outcome != "authorized" || tokens.Load() != 1 || metadata.Load() != 0 {
		t.Fatalf("authorization: %+v tokens=%d metadata=%d", checked, tokens.Load(), metadata.Load())
	}
	for _, result := range []any{checked, app.OpenItemDraft(desktop.ItemRequest{Context: ctx, Ref: ref}), app.ListConnections(ctx)} {
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "test-private-bearer") || strings.Contains(string(raw), "PRIVATE KEY") {
			t.Fatal("authorization material reached facade documents")
		}
	}
	staleKey := prepare(ref)
	credential.Update = true
	credential.Command = "/usr/bin/false"
	credential.ReplaceArguments = true
	credential.Arguments = []string{}
	if changed := app.SaveCredential(credential); changed.State != desktop.Completed {
		t.Fatalf("change key locator: %+v", changed)
	}
	refused := app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: staleKey.Review.Token, IntentID: "old-key"})
	if refused.Outcome != desktop.ActionStale || tokens.Load() != 1 {
		t.Fatalf("changed key reused review: %+v", refused)
	}
	stalePolicy := prepare(ref)
	if stalePolicy.Review == nil || !stalePolicy.Review.Ready {
		t.Fatalf("fresh policy review: %+v", stalePolicy)
	}
	draft.SendPolicy.ApprovedDestinations = []string{"127.0.0.2/32"}
	if changed := app.SaveItem(desktop.SaveItemRequest{Context: ctx, Kind: desktop.EnvironmentItem, Item: ref.ID, BaseRevision: ref.Revision, Draft: draft, IntentID: "policy-change"}); changed.Outcome != desktop.SavedOutcome {
		t.Fatalf("policy edit: %+v", changed)
	}
	refused = app.ExecuteReviewedAction(desktop.ExecuteActionRequest{Context: ctx, Token: stalePolicy.Review.Token, IntentID: "old-policy"})
	if refused.Outcome != desktop.ActionStale || tokens.Load() != 1 {
		t.Fatalf("changed policy reused review: %+v", refused)
	}
}
