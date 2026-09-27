package smartbackend_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/smartbackend"
)

func adversarialConfig(t *testing.T, f *fixture) smartbackend.Config {
	t.Helper()
	raw, err := json.Marshal(f.config)
	if err != nil {
		t.Fatal(err)
	}
	var copied smartbackend.Config
	if err := json.Unmarshal(raw, &copied); err != nil {
		t.Fatal(err)
	}
	return copied
}

func adversarialCounts(f *fixture) (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens, f.reads
}

func TestSMARTAdversarialKeyProvider(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--adversarial-smart-key" && i+2 < len(os.Args) {
			if os.WriteFile(os.Args[i+1], []byte("invoked"), 0600) != nil {
				os.Exit(2)
			}
			raw, err := os.ReadFile(os.Args[i+2])
			if err != nil {
				os.Exit(2)
			}
			_, _ = os.Stdout.Write(raw)
			os.Exit(0)
		}
	}
}

func TestSMARTAdversarialPassivePreparationAndDeniedGrantDoNotResolveKeys(t *testing.T) {
	f := newFixture(t, "ES384")
	c := adversarialConfig(t, f)
	marker := filepath.Join(t.TempDir(), "provider-invoked")
	key := c.Key.Locator.Arguments[len(c.Key.Locator.Arguments)-1]
	c.Key.Locator.Arguments = []string{"-test.run=^TestSMARTAdversarialKeyProvider$", "--", "--adversarial-smart-key", marker, key}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	client, err := smartbackend.Prepare(raw, f.policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.client = client
	f.authority.allow(client.TokenBinding())
	plan := f.plan("GET", "/Patient/fixture")
	_ = client.Preflight()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("passive preparation resolved signing key")
	}
	f.authority.mu.Lock()
	f.authority.denied = true
	f.authority.mu.Unlock()
	if _, _, err := client.Session(f.authority, nil).Execute(t.Context(), plan, f.authority, nil); err == nil {
		t.Fatal("denied grant executed")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("denied grant resolved signing key")
	}
	if tokens, reads := adversarialCounts(f); tokens != 0 || reads != 0 {
		t.Fatal("denied grant reached server")
	}
	f.authority.mu.Lock()
	f.authority.denied = false
	f.authority.mu.Unlock()
	if _, _, err := client.Session(f.authority, nil).Execute(t.Context(), plan, f.authority, nil); err != nil {
		t.Fatal("authorized provider control", err)
	}
	if raw, err := os.ReadFile(marker); err != nil || string(raw) != "invoked" {
		t.Fatal("positive control did not invoke provider", err)
	}
}

func TestSMARTAdversarialConfigurationRefusesUnapprovedSigningAndAudience(t *testing.T) {
	f := newFixture(t, "ES384")
	for _, tc := range []struct {
		name   string
		change func(*smartbackend.Config)
	}{
		{"wrong audience", func(c *smartbackend.Config) { c.Audience = f.server.URL + "/another-audience" }},
		{"unsigned algorithm", func(c *smartbackend.Config) { c.Algorithm = "none" }},
		{"HMAC confusion", func(c *smartbackend.Config) { c.Algorithm = "HS384"; c.Key.JWKS.Keys[0].Alg = "HS384" }},
		{"RSA EC confusion", func(c *smartbackend.Config) { c.Algorithm = "RS384"; c.Key.JWKS.Keys[0].Alg = "RS384" }},
		{"encryption key", func(c *smartbackend.Config) { c.Key.JWKS.Keys[0].Use = "enc" }},
		{"duplicate kid", func(c *smartbackend.Config) { c.Key.JWKS.Keys = append(c.Key.JWKS.Keys, c.Key.JWKS.Keys[0]) }},
		{"unapproved selected kid", func(c *smartbackend.Config) { c.Key.Kid = "unregistered-key" }},
		{"wrong curve", func(c *smartbackend.Config) { c.Key.JWKS.Keys[0].Crv = "P-256" }},
		{"invalid curve point", func(c *smartbackend.Config) {
			c.Key.JWKS.Keys[0].X = base64.RawURLEncoding.EncodeToString(make([]byte, 48))
			c.Key.JWKS.Keys[0].Y = c.Key.JWKS.Keys[0].X
		}},
		{"mixed key members", func(c *smartbackend.Config) { c.Key.JWKS.Keys[0].N = "AQAB"; c.Key.JWKS.Keys[0].E = "AQAB" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := adversarialConfig(t, f)
			tc.change(&c)
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := smartbackend.Prepare(raw, f.policy, nil); err == nil {
				t.Fatal("unsafe registration accepted")
			}
		})
	}
	for _, member := range []string{"d", "p", "q", "dp", "dq", "qi", "k"} {
		t.Run("private member "+member, func(t *testing.T) {
			raw, err := json.Marshal(f.config)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			keys := object["key"].(map[string]any)["jwks"].(map[string]any)["keys"].([]any)
			keys[0].(map[string]any)[member] = "synthetic-private-material"
			raw, err = json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := smartbackend.Prepare(raw, f.policy, nil); err == nil || strings.Contains(err.Error(), "synthetic-private-material") {
				t.Fatal("private member accepted or disclosed")
			}
		})
	}
	if tokens, reads := adversarialCounts(f); tokens != 0 || reads != 0 {
		t.Fatal("configuration validation reached a server")
	}
	// The unchanged registration must still execute against the independent verifier.
	if _, _, err := f.client.Session(f.authority, nil).Execute(t.Context(), f.plan("GET", "/Patient/fixture"), f.authority, nil); err != nil {
		t.Fatal("valid registration control", err)
	}
}

func TestSMARTAdversarialUnsafeBasesAreRefusedLocally(t *testing.T) {
	f := newFixture(t, "ES384")
	for _, suffix := range []string{"/fhir/.", "/fhir/..", "/fhir/../outside", "/fhir/./inner", "/fhir//inner", "/fhir?scope=other", "/fhir#fragment", "/fhir/%2e%2e"} {
		t.Run(suffix, func(t *testing.T) {
			c := adversarialConfig(t, f)
			c.FHIRBase = f.server.URL + suffix
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := smartbackend.Prepare(raw, f.policy, nil); err == nil {
				t.Fatal("unsafe FHIR base accepted")
			}
		})
	}
	if tokens, reads := adversarialCounts(f); tokens != 0 || reads != 0 {
		t.Fatal("passive base validation contacted server")
	}
}

func TestSMARTAdversarialUnsafeRequestsNeverAcquireTokens(t *testing.T) {
	f := newFixture(t, "ES384")
	session := f.client.Session(f.authority, nil)
	defer session.Disconnect()
	for _, path := range []string{"/Patient/$export", "/Patient/_search", "/Patient/a/$everything", "/Patient/.", "/Patient/..", "/Patient/a/_history/..", "/Patient/a/../b", "/Patient//a", "/Patient/%2e%2e", "/Patient/%2fa"} {
		t.Run(path, func(t *testing.T) {
			if _, _, err := session.Execute(t.Context(), f.plan("GET", path), f.authority, nil); err == nil {
				t.Fatal("unsafe resource operation accepted")
			}
		})
	}
	for _, destination := range []string{f.server.URL + "/other/Patient/a", f.server.URL + "/fhir-escape/Patient/a", "https://unapproved.invalid/fhir/Patient/a"} {
		t.Run(destination, func(t *testing.T) {
			spec := f.plan("GET", "/Patient/a").Declaration()
			spec.HTTP.URL = destination
			raw, err := json.Marshal(spec)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := networkaction.PrepareRuntimeHTTP(raw, f.policy)
			if err != nil {
				return
			}
			f.authority.allow(plan.Binding())
			if _, _, err := session.Execute(t.Context(), plan, f.authority, nil); err == nil {
				t.Fatal("token used outside approved base")
			}
		})
	}
	if tokens, reads := adversarialCounts(f); tokens != 0 || reads != 0 {
		t.Fatalf("denied paths acquired/sent: tokens=%d reads=%d", tokens, reads)
	}
	if _, _, err := session.Execute(t.Context(), f.plan("GET", "/Patient/fixture"), f.authority, nil); err != nil {
		t.Fatal("safe request control", err)
	}
}

func TestSMARTAdversarialUnavailableKeyAndDeniedAuthorityHaveNoNetworkEffects(t *testing.T) {
	for _, scenario := range []string{"denied authority", "missing key", "unapproved private key"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, "ES384")
			config := adversarialConfig(t, f)
			if scenario == "missing key" {
				config.Key.Locator.Arguments[len(config.Key.Locator.Arguments)-1] = filepath.Join(t.TempDir(), "missing")
			}
			if scenario == "unapproved private key" {
				other := newFixture(t, "ES384")
				config.Key.Locator = other.config.Key.Locator
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			client, err := smartbackend.Prepare(raw, f.policy, nil)
			if err != nil {
				t.Fatal(err)
			}
			f.client = client
			f.authority.allow(client.TokenBinding())
			plan := f.plan("GET", "/Patient/fixture")
			if scenario == "denied authority" {
				f.authority.mu.Lock()
				f.authority.denied = true
				f.authority.mu.Unlock()
			}
			_, _, err = client.Session(f.authority, nil).Execute(t.Context(), plan, f.authority, nil)
			if err == nil {
				t.Fatal("unavailable authority/key executed")
			}
			if scenario != "denied authority" && !strings.Contains(err.Error(), "key-unavailable") {
				t.Fatal("key refusal lost typed status", err)
			}
			if tokens, reads := adversarialCounts(f); tokens != 0 || reads != 0 {
				t.Fatal("refusal contacted a token or resource server")
			}
		})
	}
}

func TestSMARTAdversarialHostileTokenErrorsAndCachedStateRemainRedacted(t *testing.T) {
	for _, status := range []int{200, 400, 401, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFixture(t, "ES384")
			if status != 200 {
				f.tokenStatus = status
			}
			session := f.client.Session(f.authority, nil)
			defer session.Disconnect()
			response, receipts, err := session.Execute(context.Background(), f.plan("GET", "/Patient/fixture"), f.authority, nil)
			if status == 200 && err != nil || status != 200 && err == nil {
				t.Fatal("unexpected control outcome", err)
			}
			f.mu.Lock()
			assertion, token := f.assertion, f.token
			f.mu.Unlock()
			if assertion == "" {
				t.Fatal("hostile response test did not exercise signed token request")
			}
			secrets := map[string]string{"token": token, "assertion": assertion, "private key": string(f.keyPEM), "encoded private key": base64.StdEncoding.EncodeToString(f.keyPEM), "hex token": fmt.Sprintf("%x", token)}
			check := func(channel string, raw []byte) {
				t.Helper()
				for kind, secret := range secrets {
					if bytes.Contains(raw, []byte(secret)) {
						t.Fatalf("%s disclosed %s", channel, kind)
					}
				}
			}
			for name, value := range map[string]any{"session pointer": session, "session value": reflect.ValueOf(session).Elem().Interface(), "response": response, "receipts": receipts, "error": err, "preflight": f.client.Preflight()} {
				for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
					check(name+" "+verb, []byte(fmt.Sprintf(verb, value)))
				}
				encoded, marshalErr := json.Marshal(value)
				if strings.HasPrefix(name, "session") && marshalErr == nil {
					t.Fatalf("%s was serializable", name)
				}
				check(name+" JSON", encoded)
				encoded, marshalErr = json.Marshal(map[string]any{"state": value})
				if strings.HasPrefix(name, "session") && marshalErr == nil {
					t.Fatalf("nested %s was serializable", name)
				}
				check(name+" nested JSON", encoded)
			}
			if status != 200 {
				if _, reads := adversarialCounts(f); reads != 0 {
					t.Fatal("token refusal reached resource server")
				}
			}
		})
	}
}
