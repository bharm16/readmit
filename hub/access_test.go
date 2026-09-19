package hub_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"github.com/bharm16/readmit/hub"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAccessPolicyRefusesIncompleteContracts(t *testing.T) {
	for _, data := range []string{`{}`, `{"schema":"readmit-hub-access/v1"}`, `null`} {
		if _, err := hub.ReadAccessPolicy([]byte(data)); err == nil {
			t.Fatal("accepted incomplete policy")
		}
	}
}

func accessFixture(t *testing.T) (*hub.Access, hub.AccessPolicy, *rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := hub.AccessPolicy{Schema: "readmit-hub-access/v1", Issuer: "https://idp.example", Audience: "https://hub.example", Clients: []string{"readmit-client"}, Keys: []hub.AccessKey{{ID: "current", N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()), E: "AQAB"}}, Grants: []hub.ProjectGrant{}, Tokens: []hub.ScopedToken{}}
	for _, role := range []string{"owner", "admin", "analyst", "reviewer", "runner", "viewer"} {
		p.Grants = append(p.Grants, hub.ProjectGrant{Project: "alpha", Subject: role, Role: role})
	}
	path := filepath.Join(t.TempDir(), "access.json")
	writePolicy(t, path, p)
	a, err := hub.OpenAccess(path)
	if err != nil {
		t.Fatal(err)
	}
	return a, p, key, path
}
func writePolicy(t *testing.T, path string, p hub.AccessPolicy) {
	t.Helper()
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func claims(subject string) map[string]any {
	now := time.Now().Unix()
	return map[string]any{"iss": "https://idp.example", "aud": "https://hub.example", "sub": subject, "client_id": "readmit-client", "jti": "fixture-id", "iat": now - 1, "exp": now + 300, "scope": "evidence.read evidence.write execution approval export enrollment admin ownership"}
}
func signed(t *testing.T, key *rsa.PrivateKey, c map[string]any, header string) string {
	t.Helper()
	body, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	input := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + base64.RawURLEncoding.EncodeToString(body)
	h := sha256.Sum256([]byte(input))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if e != nil {
		t.Fatal(e)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

const accessHeader = `{"alg":"RS256","typ":"at+jwt","kid":"current"}`

func request(token string) *http.Request {
	r := httptest.NewRequest("GET", "https://hub.example/", nil)
	r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{Raw: []byte("synthetic-client-cert")}}}}
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestPublicAuthorizationRoleMatrix(t *testing.T) {
	a, _, key, _ := accessFixture(t)
	matrix := map[string]string{"owner": "evidence.read evidence.write execution approval export enrollment admin ownership", "admin": "evidence.read evidence.write execution export enrollment admin", "analyst": "evidence.read evidence.write execution export", "reviewer": "evidence.read approval export", "viewer": "evidence.read", "runner": ""}
	for role, allowed := range matrix {
		r := request(signed(t, key, claims(role), accessHeader))
		for _, action := range strings.Fields("evidence.read evidence.write execution approval export enrollment admin ownership unknown") {
			principal, err := a.Authorize(r, "alpha", action)
			want := slices.Contains(strings.Fields(allowed), action)
			if (err == nil) != want {
				t.Fatalf("%s %s allowed=%v error=%v", role, action, want, err)
			}
			if err == nil && principal.Subject != role {
				t.Fatal("identity lost")
			}
		}
		if _, e := a.Authorize(r, "beta", "evidence.read"); e == nil {
			t.Fatal("cross-project admission")
		}
	}
}
func TestPublicAuthorizationRejectsWrongTokenAndRevocation(t *testing.T) {
	a, p, key, path := accessFixture(t)
	changes := map[string]func(map[string]any){"issuer": func(c map[string]any) { c["iss"] = "https://evil.example" }, "audience": func(c map[string]any) { c["aud"] = "other" }, "multi audience": func(c map[string]any) { c["aud"] = []string{"https://hub.example", "other"} }, "client": func(c map[string]any) { c["client_id"] = "other" }, "expiry": func(c map[string]any) { c["exp"] = time.Now().Unix() }, "future": func(c map[string]any) { c["iat"] = time.Now().Unix() + 100 }, "not before": func(c map[string]any) { c["nbf"] = time.Now().Unix() + 100 }, "lifetime": func(c map[string]any) { c["exp"] = time.Now().Unix() + 7200 }, "missing exp": func(c map[string]any) { delete(c, "exp") }, "scope": func(c map[string]any) { c["scope"] = "execution" }, "subject": func(c map[string]any) { c["sub"] = "unassigned" }}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			c := claims("viewer")
			change(c)
			if _, e := a.Authorize(request(signed(t, key, c, accessHeader)), "alpha", "evidence.read"); e == nil {
				t.Fatal("invalid token admitted")
			}
		})
	}
	for _, header := range []string{`{"alg":"RS256","typ":"JWT","kid":"current"}`, `{"alg":"none","typ":"at+jwt","kid":"current"}`, `{"alg":"RS256","typ":"at+jwt","kid":"other"}`, `{"alg":"RS256","typ":"at+jwt","kid":"current","jku":"https://evil.example"}`} {
		if _, e := a.Authorize(request(signed(t, key, claims("viewer"), header)), "alpha", "evidence.read"); e == nil {
			t.Fatal("wrong token type/key admitted")
		}
	}
	token := signed(t, key, claims("viewer"), accessHeader)
	if _, e := a.Authorize(request(token+"x"), "alpha", "evidence.read"); e == nil {
		t.Fatal("bad signature")
	}
	noTLS := request(token)
	noTLS.TLS = nil
	if _, e := a.Authorize(noTLS, "alpha", "evidence.read"); e == nil {
		t.Fatal("missing TLS")
	}
	cancelled := request(token)
	ctx, cancel := context.WithCancel(cancelled.Context())
	cancel()
	if _, e := a.Authorize(cancelled.WithContext(ctx), "alpha", "evidence.read"); e == nil {
		t.Fatal("cancelled admitted")
	}
	p.Grants = nil
	writePolicy(t, path, p)
	if _, e := a.Authorize(request(token), "alpha", "evidence.read"); e == nil {
		t.Fatal("revoked subject admitted")
	}
	if e := os.WriteFile(path, []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := a.Authorize(request(token), "alpha", "evidence.read"); e == nil {
		t.Fatal("invalid policy retained old grants")
	}
}
func TestScopedRunnerCertificateAndImmediateTokenRemoval(t *testing.T) {
	a, p, _, path := accessFixture(t)
	token := "rh_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	hash := sha256.Sum256([]byte(token))
	cert := sha256.Sum256([]byte("synthetic-client-cert"))
	p.Tokens = []hub.ScopedToken{{Hash: hex.EncodeToString(hash[:]), Subject: "runner", Project: "alpha", Actions: []string{"enrollment", "execution", "evidence.read"}, Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Certificate: hex.EncodeToString(cert[:]), Kind: "runner"}}
	writePolicy(t, path, p)
	for _, action := range []string{"enrollment", "execution", "evidence.read"} {
		if _, e := a.Authorize(request(token), "alpha", action); e != nil {
			t.Fatal(e)
		}
	}
	for _, action := range []string{"evidence.write", "approval", "export", "ownership"} {
		if _, e := a.Authorize(request(token), "alpha", action); e == nil {
			t.Fatal("token gained scope", action)
		}
	}
	r := request(token)
	r.TLS.VerifiedChains[0][0].Raw = []byte("other-cert")
	if _, e := a.Authorize(r, "alpha", "execution"); e == nil {
		t.Fatal("enrollment moved to other cert")
	}
	if _, e := a.Authorize(request(token), "beta", "execution"); e == nil {
		t.Fatal("runner moved project")
	}
	p.Tokens = nil
	writePolicy(t, path, p)
	if _, e := a.Authorize(request(token), "alpha", "execution"); e == nil {
		t.Fatal("revoked token admitted")
	}
}

func TestAccessPolicyStrictNestedAndFileBoundaries(t *testing.T) {
	_, p, _, path := accessFixture(t)
	data, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	var tree map[string]any
	if e = json.Unmarshal(data, &tree); e != nil {
		t.Fatal(e)
	}
	variants := []string{strings.Replace(string(data), `"tokens":[]`, `"tokens":null`, 1), strings.Replace(string(data), `"role":"owner"`, `"role":null`, 1), strings.Replace(string(data), `"role":"owner"`, `"role":"owner","extra":true`, 1), strings.Replace(string(data), `"role":"owner",`, ``, 1), strings.Replace(string(data), `"schema":`, `"schema":"duplicate","schema":`, 1)}
	for _, v := range variants {
		if v == string(data) {
			continue
		}
		if _, e := hub.ReadAccessPolicy([]byte(v)); e == nil {
			t.Fatalf("malformed policy admitted: %s", v)
		}
	}
	p.Grants = append(p.Grants, p.Grants[0])
	b, _ := json.Marshal(p)
	if _, e := hub.ReadAccessPolicy(b); e == nil {
		t.Fatal("duplicate grant")
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := hub.OpenAccess(path); e == nil {
		t.Fatal("public policy")
	}
	if e := os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(t.TempDir(), "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e := hub.OpenAccess(link); e == nil {
		t.Fatal("symlink policy")
	}
}

func TestPublicRunnerEnrollment(t *testing.T) {
	a, p, _, path := accessFixture(t)
	token := "rh_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	hash := sha256.Sum256([]byte(token))
	cert := sha256.Sum256([]byte("synthetic-client-cert"))
	p.Tokens = []hub.ScopedToken{{Hash: hex.EncodeToString(hash[:]), Subject: "runner", Project: "alpha", Actions: []string{"enrollment"}, Expires: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Certificate: hex.EncodeToString(cert[:]), Kind: "runner"}}
	writePolicy(t, path, p)
	// Admission consults durable removals as well as the current policy.
	c := integrationConfig(t)
	db := testDatabase(t, c)
	reset(t, db)
	store := open(t, c)
	if e := store.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	h := store.TeamHandler(a)
	check := func(project string, badCert bool, want int) {
		r := request(token)
		r.Method = "POST"
		r.URL.Path = "/v1/projects/" + project + "/enrollment"
		if badCert {
			r.TLS.VerifiedChains[0][0].Raw = []byte("wrong-cert")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("enrollment status %d want %d", w.Code, want)
		}
	}
	check("alpha", false, 204)
	check("alpha", true, 403)
	check("beta", false, 403)
	p.Tokens = []hub.ScopedToken{}
	writePolicy(t, path, p)
	check("alpha", false, 403)
}
