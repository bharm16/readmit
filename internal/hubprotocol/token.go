package hubprotocol

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ClaimPolicy is what an access token's claims must name: the pinned issuer
// and audience, and the clients the token may have been issued to.
type ClaimPolicy struct {
	Issuer   string
	Audience string
	Clients  []string
	// RequireID refuses a token without a jti. The hub requires one;
	// hubclient's sign-in check has never required it, and a token without
	// one is refused by the hub at its first request instead.
	RequireID bool
}

// AccessClaims are the claims one access token was issued with.
type AccessClaims struct {
	Issuer   string
	Subject  string
	Audience string
	Scopes   []string
	Expires  int64
}

// ReadAccessClaims applies the constrained RFC 9068 claim rule to a token's
// decoded payload: the pinned issuer, one of the pinned clients, a subject of
// at most 256 bytes, an issue and expiry time with a lifetime of at most an
// hour that holds now and has begun, and exactly one audience, the pinned one.
//
// It verifies no signature and reads no header. The hub verifies the token's
// RS256 signature against its pinned keys before it asks this rule; hubclient
// asks it claims-only, so the application can say why a sign-in will not be
// accepted, and the hub stays the authority that verifies the token on every
// request. External RFC claims are extensible, unlike readmit's own strict
// documents, so members this rule does not read are ignored; json/v2 still
// rejects duplicate keys and malformed Unicode.
func ReadAccessClaims(payload []byte, policy ClaimPolicy, now time.Time) (AccessClaims, error) {
	var c struct {
		Issuer   string         `json:"iss"`
		Subject  string         `json:"sub"`
		Audience jsontext.Value `json:"aud"`
		Client   string         `json:"client_id"`
		ID       string         `json:"jti"`
		Exp      *int64         `json:"exp"`
		Iat      *int64         `json:"iat"`
		Nbf      *int64         `json:"nbf"`
		Scope    string         `json:"scope"`
	}
	if json.Unmarshal(payload, &c) != nil {
		return AccessClaims{}, errors.New("invalid token claims")
	}
	if c.Issuer != policy.Issuer {
		return AccessClaims{}, fmt.Errorf("token issuer %q does not match configured issuer %q", c.Issuer, policy.Issuer)
	}
	if !slices.Contains(policy.Clients, c.Client) {
		return AccessClaims{}, fmt.Errorf("token client_id %q does not match configured client_id %q", c.Client, strings.Join(policy.Clients, ", "))
	}
	if c.Subject == "" || len(c.Subject) > 256 {
		return AccessClaims{}, errors.New("invalid token subject")
	}
	if policy.RequireID && c.ID == "" {
		return AccessClaims{}, errors.New("token has no jti")
	}
	if c.Exp == nil || c.Iat == nil {
		return AccessClaims{}, errors.New("token missing exp or iat")
	}
	t := now.Unix()
	if *c.Iat > t || *c.Exp <= t || *c.Exp <= *c.Iat {
		return AccessClaims{}, errors.New("token is expired or not yet valid")
	}
	if *c.Iat <= 0 || *c.Exp-*c.Iat > 3600 {
		return AccessClaims{}, errors.New("token lifetime exceeds 1 hour limit")
	}
	if c.Nbf != nil && *c.Nbf > t {
		return AccessClaims{}, errors.New("token not valid before nbf")
	}
	var audience string
	if json.Unmarshal(c.Audience, &audience) != nil {
		var list []string
		if json.Unmarshal(c.Audience, &list) != nil || len(list) != 1 {
			return AccessClaims{}, errors.New("token audience must match configured audience")
		}
		audience = list[0]
	}
	if audience != policy.Audience {
		return AccessClaims{}, fmt.Errorf("token audience %q does not match configured audience %q", audience, policy.Audience)
	}
	return AccessClaims{
		Issuer: c.Issuer, Subject: c.Subject, Audience: audience,
		Scopes: strings.Fields(c.Scope), Expires: *c.Exp,
	}, nil
}
