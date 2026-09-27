package smartbackend_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/smartbackend"
)

func TestSMARTKeyProvider(t *testing.T) {
	for i, a := range os.Args {
		if a == "--smart-key" && i+1 < len(os.Args) {
			b, e := os.ReadFile(os.Args[i+1])
			if e != nil {
				os.Exit(2)
			}
			os.Stdout.Write(b)
			os.Exit(0)
		}
	}
}

type auth struct {
	mu       sync.Mutex
	bindings map[networkaction.Binding]bool
	actor    networkaction.Actor
	denied   bool
}

func (a *auth) Check(_ context.Context, b networkaction.Binding) (networkaction.Actor, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.denied || !a.bindings[b] {
		return networkaction.Actor{}, fmt.Errorf("denied")
	}
	return a.actor, nil
}
func (a *auth) allow(b networkaction.Binding) { a.mu.Lock(); defer a.mu.Unlock(); a.bindings[b] = true }

type fixture struct {
	requested string
	tokenDate string
	public    crypto.PublicKey

	t             *testing.T
	server        *httptest.Server
	config        smartbackend.Config
	policy        []byte
	client        *smartbackend.Client
	authority     *auth
	mu            sync.Mutex
	tokens, reads int
	jtis          map[string]bool
	token         string
	tokenStatus   int
	grants        string
	expiry        int
	rejectFirst   bool
	assertion     string
	keyPEM        []byte
}

func newFixture(t *testing.T, alg string) *fixture {
	t.Helper()
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	f := &fixture{requested: "system/Patient.rs", t: t, jtis: map[string]bool{}, token: "fixture-opaque-access-token", grants: "system/Patient.rs", expiry: 300}
	var key crypto.Signer
	if alg == "RS384" {
		k, e := rsa.GenerateKey(rand.Reader, 2048)
		if e != nil {
			t.Fatal(e)
		}
		key = k
	} else {
		k, e := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		key = k
	}
	enc := base64.RawURLEncoding.EncodeToString
	jwk := smartbackend.JWK{Kid: "registered-key", Alg: alg, Use: "sig"}
	switch p := key.Public().(type) {
	case *rsa.PublicKey:
		jwk.Kty = "RSA"
		jwk.N = enc(p.N.Bytes())
		jwk.E = enc(big.NewInt(int64(p.E)).Bytes())
	case *ecdsa.PublicKey:
		jwk.Kty = "EC"
		jwk.Crv = "P-384"
		jwk.X = enc(p.X.FillBytes(make([]byte, 48)))
		jwk.Y = enc(p.Y.FillBytes(make([]byte, 48)))
	}
	f.public = key.Public()
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/token" {
			f.tokens++
			r.ParseForm()
			assertion := r.Form.Get("client_assertion")
			f.assertion = assertion
			parts := strings.Split(assertion, ".")
			if len(parts) != 3 {
				t.Error("not signed JWT")
				w.WriteHeader(400)
				return
			}
			var header struct {
				Alg string `json:"alg"`
				Kid string `json:"kid"`
				Typ string `json:"typ"`
			}
			var claims struct {
				Iss string `json:"iss"`
				Sub string `json:"sub"`
				Aud string `json:"aud"`
				Jti string `json:"jti"`
				Iat int64  `json:"iat"`
				Exp int64  `json:"exp"`
			}
			h, _ := base64.RawURLEncoding.DecodeString(parts[0])
			c, _ := base64.RawURLEncoding.DecodeString(parts[1])
			sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
			json.Unmarshal(h, &header)
			json.Unmarshal(c, &claims)
			sum := sha512.Sum384([]byte(parts[0] + "." + parts[1]))
			valid := false
			switch p := f.public.(type) {
			case *rsa.PublicKey:
				valid = rsa.VerifyPKCS1v15(p, crypto.SHA384, sum[:], sig) == nil
			case *ecdsa.PublicKey:
				valid = len(sig) == 96 && ecdsa.Verify(p, sum[:], new(big.Int).SetBytes(sig[:48]), new(big.Int).SetBytes(sig[48:]))
			}
			now := time.Now().Unix()
			if !valid || header.Alg != alg || header.Kid != "registered-key" || header.Typ != "JWT" || claims.Iss != "registered-client" || claims.Sub != claims.Iss || claims.Aud != f.server.URL+"/token" || claims.Exp <= now || claims.Exp > now+300 || claims.Iat > now+2 || len(claims.Jti) < 32 || f.jtis[claims.Jti] || r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("scope") != f.requested || r.Form.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
				t.Error("independent assertion validation failed")
				w.WriteHeader(400)
				return
			}
			f.jtis[claims.Jti] = true
			if f.tokenStatus != 0 {
				w.WriteHeader(f.tokenStatus)
				w.Write([]byte(f.token + assertion))
				return
			}
			if f.tokenDate != "" {
				w.Header().Set("Date", f.tokenDate)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":%d,"scope":%q}`, f.token, f.expiry, f.grants)
			return
		}
		if r.URL.Path == "/fhir/.well-known/smart-configuration" {
			if r.Header.Get("Accept") != "application/json" {
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"token_endpoint":%q,"grant_types_supported":["client_credentials"],"token_endpoint_auth_methods_supported":["private_key_jwt"],"token_endpoint_auth_signing_alg_values_supported":["RS384","ES384"],"capabilities":["client-confidential-asymmetric","permission-v2"]}`, f.server.URL+"/token")
			return
		}
		f.reads++
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			t.Error("protected request missing scoped bearer")
			w.WriteHeader(401)
			return
		}
		if f.rejectFirst && f.reads == 1 {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"resourceType":"Patient","id":"fixture"}`))
	}))
	t.Cleanup(f.server.Close)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	f.keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "private-provider-store")
	os.WriteFile(path, f.keyPEM, 0600)
	exe, _ := os.Executable()
	uhost, port, _ := net.SplitHostPort(f.server.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	ip := netip.MustParseAddr(uhost)
	rules := []sendpolicy.ScopeRule{}
	for _, op := range []sendpolicy.Operation{sendpolicy.SMARTToken, sendpolicy.FHIRSearch, sendpolicy.FHIRAction, sendpolicy.FHIRMetadata} {
		rules = append(rules, sendpolicy.ScopeRule{Endpoint: "fixture", Operation: op, Port: p, Destinations: []string{netip.PrefixFrom(ip, ip.BitLen()).String()}, Selection: "single-address"})
	}
	f.policy, _ = json.Marshal(sendpolicy.ScopedPolicy{Schema: sendpolicy.ScopedPolicySchema, Project: "lab", Environment: "test", Revision: "1", Rules: rules})
	f.config = smartbackend.Config{Schema: smartbackend.ConfigSchema, FHIRBase: f.server.URL + "/fhir", TokenEndpoint: f.server.URL + "/token", ClientID: "registered-client", Audience: f.server.URL + "/token", Algorithm: alg, Role: "observer", Scopes: []string{"system/Patient.rs"}, Key: smartbackend.KeyReference{Kid: jwk.Kid, Generation: "1", Locator: networkaction.Provider{Command: exe, Arguments: []string{"-test.run=^TestSMARTKeyProvider$", "--", "--smart-key", path}}, JWKS: smartbackend.JWKS{Keys: []smartbackend.JWK{jwk}}}, Token: networkaction.HTTPSpec{Schema: networkaction.HTTPSchema, Plan: networkaction.Digest([]byte("plan")), Source: networkaction.Digest([]byte("source")), Project: "lab", Environment: "test", Revision: "1", Endpoint: "fixture", Classification: "nonproduction", Operation: sendpolicy.SMARTToken, Method: "POST", URL: f.server.URL + "/token", ServerName: "example.com", Authorities: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.server.Certificate().Raw}), TimeoutMS: 3000, MaxBytes: 65536}}
	raw, _ := json.Marshal(f.config)
	var err error
	f.client, err = smartbackend.Prepare(raw, f.policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.authority = &auth{bindings: map[networkaction.Binding]bool{}, actor: networkaction.Actor{Kind: "runner", ID: "fixture", Generation: "1", EvidenceIdentity: networkaction.Digest([]byte("grant")), Expires: time.Now().Add(time.Hour)}}
	f.authority.allow(f.client.TokenBinding())
	return f
}
func (f *fixture) plan(method, path string) *networkaction.RuntimeHTTPPlan {
	f.t.Helper()
	s := f.config.Token
	s.Method = method
	s.URL = f.config.FHIRBase + path
	s.Body = nil
	s.Operation = sendpolicy.FHIRSearch
	if method != "GET" {
		s.Operation = sendpolicy.FHIRAction
	}
	raw, _ := json.Marshal(networkaction.RuntimeHTTPSpec{Schema: networkaction.RuntimeHTTPSchema, Authorization: f.client.Identity(), HTTP: s})
	p, e := networkaction.PrepareRuntimeHTTP(raw, f.policy)
	if e != nil {
		f.t.Fatal(e)
	}
	f.authority.allow(p.Binding())
	return p
}
func TestSMARTIndependentAlgorithmsAndProtectedHTTP(t *testing.T) {
	for _, alg := range []string{"RS384", "ES384"} {
		t.Run(alg, func(t *testing.T) {
			f := newFixture(t, alg)
			if f.tokens != 0 || f.reads != 0 {
				t.Fatal("passive effects")
			}
			session := f.client.Session(f.authority, nil)
			plan := f.plan("GET", "/Patient/fixture")
			response, receipts, err := session.Execute(context.Background(), plan, f.authority, nil)
			if err != nil || response.Status != 200 || len(receipts) != 1 {
				t.Fatal(err, receipts)
			}
			if f.tokens != 1 || f.reads != 1 {
				t.Fatal("actual transport not used")
			}
			if _, _, err = session.Execute(context.Background(), plan, f.authority, nil); err != nil || f.tokens != 1 {
				t.Fatal("cached token not consumed", err)
			}
		})
	}
}

func TestSMARTConcurrentRenewalRevocationAndReadOnlyRetry(t *testing.T) {
	f := newFixture(t, "ES384")
	session := f.client.Session(f.authority, nil)
	plan := f.plan("GET", "/Patient/fixture")
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, _, e := session.Execute(context.Background(), plan, f.authority, nil); e != nil {
				t.Error(e)
			}
		}()
	}
	group.Wait()
	f.mu.Lock()
	tokens, reads := f.tokens, f.reads
	f.mu.Unlock()
	if tokens != 1 || reads != 12 {
		t.Fatalf("single flight: %d tokens, %d reads", tokens, reads)
	}
	f.authority.mu.Lock()
	f.authority.actor.Generation = "2"
	f.authority.mu.Unlock()
	if _, _, e := session.Execute(context.Background(), plan, f.authority, nil); e == nil {
		t.Fatal("changed authority reused cached token")
	}
	f.mu.Lock()
	if f.tokens != tokens || f.reads != reads {
		t.Error("changed grant had effects")
	}
	f.mu.Unlock()
	fresh := f.client.Session(f.authority, nil)
	fresh.Disconnect()
	if _, _, e := fresh.Execute(context.Background(), plan, f.authority, nil); e == nil {
		t.Fatal("disconnected session reused")
	}
	retry := newFixture(t, "ES384")
	retry.rejectFirst = true
	s := retry.client.Session(retry.authority, nil)
	if _, receipts, e := s.Execute(context.Background(), retry.plan("GET", "/Patient/fixture"), retry.authority, nil); e != nil || len(receipts) != 2 || retry.tokens != 2 || retry.reads != 2 {
		t.Fatal("safe read renewal", e, len(receipts))
	}
}

func TestSMARTRefusesDeniedGrantAndMasksAllOutput(t *testing.T) {
	f := newFixture(t, "ES384")
	f.grants = "system/Patient.s"
	s := f.client.Session(f.authority, nil)
	plan := f.plan("GET", "/Patient/fixture")
	response, receipts, err := s.Execute(context.Background(), plan, f.authority, nil)
	if err == nil || f.reads != 0 {
		t.Fatal("reduced grant executed")
	}
	var output strings.Builder
	fmt.Fprintf(&output, "%v %+v %#v %s %q %x %v %v", s, s, s, s, s, s, response, err)
	b, e := json.Marshal(s)
	if e == nil {
		t.Fatal("session serializable")
	}
	output.Write(b)
	b, _ = json.Marshal(receipts)
	output.Write(b)
	for _, secret := range []string{f.token, f.assertion, string(f.keyPEM), base64.StdEncoding.EncodeToString(f.keyPEM)} {
		if secret != "" && strings.Contains(output.String(), secret) {
			t.Fatal("output disclosed secret")
		}
	}
}

func TestSMARTShortTokenIsBoundedAndRefusalsStayTyped(t *testing.T) {
	f := newFixture(t, "ES384")
	f.expiry = 1
	s := f.client.Session(f.authority, nil)
	p := f.plan("GET", "/Patient/fixture")
	if _, _, e := s.Execute(context.Background(), p, f.authority, nil); e != nil || f.tokens != 1 || f.reads != 1 {
		t.Fatal("short expiry loop or refusal", e, f.tokens)
	}
	if _, _, e := s.Execute(context.Background(), p, f.authority, nil); e != nil || f.tokens != 1 {
		t.Fatal("fresh short token not cached", e)
	}
	time.Sleep(1100 * time.Millisecond)
	f.grants = "system/Patient.s"
	_, _, e := s.Execute(context.Background(), p, f.authority, nil)
	if e == nil || !strings.Contains(e.Error(), "grant-insufficient") || f.tokens != 2 || f.reads != 2 {
		t.Fatal("typed reduced grant lost", e)
	}
	for _, path := range []string{"/Patient/$anything", "/Patient/.", "/Patient/..", "/Patient/p/_history/$operation", "/Patient/p/_history/.."} {
		_, _, e := s.Execute(context.Background(), f.plan("GET", path), f.authority, nil)
		if e == nil {
			t.Fatal("unsafe resource path", path)
		}
	}
	if f.tokens != 2 || f.reads != 2 {
		t.Fatal("rejected paths acquired credentials or sent")
	}
	key := newFixture(t, "ES384")
	key.config.Key.Locator.Arguments[len(key.config.Key.Locator.Arguments)-1] = "/missing-key-store"
	raw, _ := json.Marshal(key.config)
	c, e := smartbackend.Prepare(raw, key.policy, nil)
	if e != nil {
		t.Fatal(e)
	}
	key.client = c
	key.authority.allow(c.TokenBinding())
	_, _, e = c.Session(key.authority, nil).Execute(context.Background(), key.plan("GET", "/Patient/fixture"), key.authority, nil)
	if e == nil || !strings.Contains(e.Error(), "key-unavailable") {
		t.Fatal("key refusal lost", e)
	}
}

func TestSMARTDiscoveryRequiresExplicitAdmissionAndPinsEndpoint(t *testing.T) {
	f := newFixture(t, "ES384")
	spec := f.config.Token
	spec.Method = "GET"
	spec.Operation = sendpolicy.FHIRMetadata
	spec.URL = f.config.FHIRBase + "/.well-known/smart-configuration"
	raw, _ := json.Marshal(spec)
	d, e := smartbackend.PrepareDiscovery(f.config.FHIRBase, raw, f.policy)
	if e != nil {
		t.Fatal(e)
	}
	if f.tokens != 0 || f.reads != 0 {
		t.Fatal("discovery preparation had effects")
	}
	if _, e = d.Execute(context.Background(), f.authority, nil); e == nil {
		t.Fatal("unapproved discovery executed")
	}
	f.authority.allow(d.Binding())
	evidence, e := d.Execute(context.Background(), f.authority, nil)
	if e != nil {
		t.Fatal(e)
	}
	f.config.MetadataIdentity = networkaction.Digest(evidence)
	raw, _ = json.Marshal(f.config)
	if _, e = smartbackend.Prepare(raw, f.policy, evidence); e != nil {
		t.Fatal("reviewed metadata failed", e)
	}
	changed := append([]byte(nil), evidence...)
	changed[len(changed)/2] ^= 1
	if _, e = smartbackend.Prepare(raw, f.policy, changed); e == nil {
		t.Fatal("changed metadata accepted")
	}
	f.config.TokenEndpoint = f.server.URL + "/changed-token"
	f.config.Audience = f.config.TokenEndpoint
	f.config.Token.URL = f.config.TokenEndpoint
	raw, _ = json.Marshal(f.config)
	if _, e = smartbackend.Prepare(raw, f.policy, evidence); e == nil {
		t.Fatal("discovered endpoint drift accepted")
	}
}

func TestSMARTExpiryRotationOutageAndMutationNotRetried(t *testing.T) {
	f := newFixture(t, "ES384")
	f.expiry = 1
	s := f.client.Session(f.authority, nil)
	p := f.plan("GET", "/Patient/fixture")
	if _, _, e := s.Execute(context.Background(), p, f.authority, nil); e != nil {
		t.Fatal(e)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, _, e := s.Execute(context.Background(), p, f.authority, nil); e != nil || f.tokens != 2 {
		t.Fatal("expired token reused", e)
	}
	s.Invalidate()
	if _, _, e := s.Execute(context.Background(), p, f.authority, nil); e == nil {
		t.Fatal("invalidated generation reused")
	}
	oldID := f.client.Identity()
	f.config.Key.Generation = "2"
	raw, _ := json.Marshal(f.config)
	rotated, e := smartbackend.Prepare(raw, f.policy, nil)
	if e != nil || rotated.Identity() == oldID {
		t.Fatal("generation not bound", e)
	}
	f.client = rotated
	f.authority.allow(rotated.TokenBinding())
	if _, _, e := rotated.Session(f.authority, nil).Execute(context.Background(), f.plan("GET", "/Patient/fixture"), f.authority, nil); e != nil || f.tokens != 3 {
		t.Fatal("rotation did not reacquire", e)
	}
	outage := newFixture(t, "ES384")
	outage.tokenStatus = 503
	_, _, e = outage.client.Session(outage.authority, nil).Execute(context.Background(), outage.plan("GET", "/Patient/fixture"), outage.authority, nil)
	if e == nil || outage.tokens != 1 || outage.reads != 0 {
		t.Fatal("outage became observation", e)
	}
	clock := newFixture(t, "ES384")
	clock.tokenDate = time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	_, _, e = clock.client.Session(clock.authority, nil).Execute(context.Background(), clock.plan("GET", "/Patient/fixture"), clock.authority, nil)
	if e == nil || !strings.Contains(e.Error(), "clock-outside-validity") || clock.reads != 0 {
		t.Fatal("clock drift accepted", e)
	}
	setup := newFixture(t, "ES384")
	setup.config.Role = "setup"
	setup.config.Scopes = []string{"system/Patient.c"}
	setup.requested = "system/Patient.c"
	setup.grants = setup.requested
	setup.rejectFirst = true
	raw, _ = json.Marshal(setup.config)
	setup.client, e = smartbackend.Prepare(raw, setup.policy, nil)
	if e != nil {
		t.Fatal(e)
	}
	setup.authority.allow(setup.client.TokenBinding())
	_, receipts, e := setup.client.Session(setup.authority, nil).Execute(context.Background(), setup.plan("POST", "/Patient"), setup.authority, nil)
	if e == nil || len(receipts) != 1 || setup.tokens != 1 || setup.reads != 1 {
		t.Fatal("uncertain write retried", e, len(receipts))
	}
}
