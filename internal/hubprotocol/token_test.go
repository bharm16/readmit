package hubprotocol_test

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// The access-token claim rule is the hub's and the application's alike; the
// application asks it claims-only and without requiring a jti, which the hub
// requires (ClaimPolicy.RequireID).
func TestAccessTokenClaimsFollowTheOneClaimRule(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	policy := hubprotocol.ClaimPolicy{Issuer: "https://idp.example", Audience: "hub", Clients: []string{"desktop", "cli"}}
	claims := func(change func(map[string]any)) []byte {
		c := map[string]any{"iss": "https://idp.example", "sub": "ana", "aud": "hub", "client_id": "desktop", "jti": "t-1",
			"iat": now.Unix() - 60, "exp": now.Unix() + 600, "scope": "evidence.read approval", "extension": true}
		if change != nil {
			change(c)
		}
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	got, err := hubprotocol.ReadAccessClaims(claims(nil), policy, now)
	if err != nil || got.Subject != "ana" || got.Issuer != policy.Issuer || got.Audience != "hub" ||
		strings.Join(got.Scopes, ",") != "evidence.read,approval" || got.Expires != now.Unix()+600 {
		t.Fatalf("claims %+v, %v", got, err)
	}
	if _, err := hubprotocol.ReadAccessClaims(claims(func(c map[string]any) { c["aud"] = []string{"hub"} }), policy, now); err != nil {
		t.Fatalf("a one-member audience list: %v", err)
	}
	for want, change := range map[string]func(map[string]any){
		"issuer":                    func(c map[string]any) { c["iss"] = "https://other.example" },
		"client_id":                 func(c map[string]any) { c["client_id"] = "portal" },
		"subject":                   func(c map[string]any) { c["sub"] = strings.Repeat("s", 257) },
		"missing exp":               func(c map[string]any) { delete(c, "exp") },
		"not yet valid":             func(c map[string]any) { c["iat"] = now.Unix() + 5 },
		"expired":                   func(c map[string]any) { c["exp"] = now.Unix() },
		"1 hour":                    func(c map[string]any) { c["iat"], c["exp"] = now.Unix()-3000, now.Unix()+601 },
		"nbf":                       func(c map[string]any) { c["nbf"] = now.Unix() + 5 },
		"match configured audience": func(c map[string]any) { c["aud"] = []string{"hub", "other"} },
		"configured audience":       func(c map[string]any) { c["aud"] = "other" },
		"invalid token claims":      func(c map[string]any) { c["sub"] = 7 },
	} {
		if _, err := hubprotocol.ReadAccessClaims(claims(change), policy, now); err == nil || !strings.Contains(err.Error(), strings.Split(want, " ")[len(strings.Split(want, " "))-1]) {
			t.Errorf("a token refused for its %s: %v", want, err)
		}
	}
	noID := claims(func(c map[string]any) { delete(c, "jti") })
	if _, err := hubprotocol.ReadAccessClaims(noID, policy, now); err != nil {
		t.Fatalf("claims-only reading refused a token without a jti: %v", err)
	}
	policy.RequireID = true
	if _, err := hubprotocol.ReadAccessClaims(noID, policy, now); err == nil {
		t.Fatal("the hub's profile accepted a token without a jti")
	}
	if _, err := hubprotocol.ReadAccessClaims(claims(nil), policy, now); err != nil {
		t.Fatalf("the hub's profile refused a whole token: %v", err)
	}
}
