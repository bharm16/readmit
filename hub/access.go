package hub

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math/big"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/hubprotocol"
)

// AccessPolicy is customer-controlled admission configuration, never evidence.
// Replacing it atomically revokes grants on the next request, including tokens.
type AccessPolicy struct {
	Schema   string         `json:"schema"`
	Issuer   string         `json:"issuer"`
	Audience string         `json:"audience"`
	Clients  []string       `json:"clients"`
	Keys     []AccessKey    `json:"keys"`
	Grants   []ProjectGrant `json:"grants"`
	Tokens   []ScopedToken  `json:"tokens"`
}
type AccessKey struct {
	ID string `json:"kid"`
	N  string `json:"n"`
	E  string `json:"e"`
}
type ProjectGrant struct {
	Project string `json:"project"`
	Subject string `json:"subject"`
	Role    string `json:"role"`
}

// ScopedToken contains only a hash of 32 random bytes held in a customer store.
// Every token is constrained by its subject's current grant and certificate.
type ScopedToken struct {
	Hash        string   `json:"sha256"`
	Subject     string   `json:"subject"`
	Project     string   `json:"project"`
	Actions     []string `json:"actions"`
	Expires     string   `json:"expires_at"`
	Certificate string   `json:"certificate_sha256"`
	Kind        string   `json:"kind"`
}

var actions = []string{"evidence.read", "evidence.write", "execution", "approval", "export", "enrollment", "admin", "ownership"}

func ReadAccessPolicy(data []byte) (AccessPolicy, error) {
	var p AccessPolicy
	if len(data) > 1<<20 || requireExactMembers(data, "schema", "issuer", "audience", "clients", "keys", "grants", "tokens") != nil {
		return p, errAccess
	}
	if json.Unmarshal(data, &p, json.RejectUnknownMembers(true)) != nil {
		return p, errAccess
	}
	var raw struct {
		Keys   []jsontext.Value `json:"keys"`
		Grants []jsontext.Value `json:"grants"`
		Tokens []jsontext.Value `json:"tokens"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return p, errAccess
	}
	for _, x := range raw.Keys {
		if requireExactMembers(x, "kid", "n", "e") != nil {
			return p, errAccess
		}
	}
	for _, x := range raw.Grants {
		if requireExactMembers(x, "project", "subject", "role") != nil {
			return p, errAccess
		}
	}
	for _, x := range raw.Tokens {
		if requireExactMembers(x, "sha256", "subject", "project", "actions", "expires_at", "certificate_sha256", "kind") != nil {
			return p, errAccess
		}
	}
	u, e := url.Parse(p.Issuer)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || p.Schema != "readmit-hub-access/v1" || p.Audience == "" || len(p.Clients) == 0 || len(p.Keys) == 0 || len(p.Keys) > 16 || len(p.Grants) > 4096 || len(p.Tokens) > 4096 {
		return p, errAccess
	}
	seen := map[string]bool{}
	for _, c := range p.Clients {
		if c == "" || seen[c] {
			return p, errAccess
		}
		seen[c] = true
	}
	seen = map[string]bool{}
	for _, k := range p.Keys {
		if k.ID == "" || seen[k.ID] {
			return p, errAccess
		}
		seen[k.ID] = true
		if _, e := k.public(); e != nil {
			return p, e
		}
	}
	seen = map[string]bool{}
	for _, g := range p.Grants {
		if !validProject(g.Project) || g.Subject == "" || len(g.Subject) > 256 || !slices.Contains([]string{"owner", "admin", "analyst", "reviewer", "runner", "viewer"}, g.Role) || seen[g.Project+"\x00"+g.Subject] || strings.ContainsRune(g.Subject, 0) {
			return p, errAccess
		}
		seen[g.Project+"\x00"+g.Subject] = true
	}
	seen = map[string]bool{}
	for _, t := range p.Tokens {
		expiry, e := time.Parse(time.RFC3339, t.Expires)
		if !validDigest(t.Hash) || !validDigest(t.Certificate) || seen[t.Hash] || e != nil || expiry.IsZero() || len(t.Actions) == 0 || !slices.Contains([]string{"api", "runner"}, t.Kind) {
			return p, errAccess
		}
		seen[t.Hash] = true
		role := p.role(t.Subject, t.Project)
		if role == "" || (t.Kind == "runner" && role != "runner") {
			return p, errAccess
		}
		used := map[string]bool{}
		for _, a := range t.Actions {
			if used[a] || !roleAllows(role, a) || a == "ownership" || a == "admin" || a == "approval" || (a == "enrollment" && t.Kind != "runner") {
				return p, errAccess
			}
			used[a] = true
		}
	}
	return p, nil
}
func (k AccessKey) public() (*rsa.PublicKey, error) {
	n, e := base64.RawURLEncoding.Strict().DecodeString(k.N)
	if e != nil {
		return nil, errAccess
	}
	exponent, e := base64.RawURLEncoding.Strict().DecodeString(k.E)
	if e != nil || len(exponent) > 4 {
		return nil, errAccess
	}
	key := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(exponent).Int64())}
	if key.N.BitLen() < 2048 || key.N.BitLen() > 4096 || key.E != 65537 {
		return nil, errAccess
	}
	return key, nil
}
func (p AccessPolicy) role(subject, project string) string {
	for _, g := range p.Grants {
		if g.Subject == subject && g.Project == project {
			return g.Role
		}
	}
	return ""
}
func roleAllows(role, action string) bool {
	if !slices.Contains(actions, action) {
		return false
	}
	switch role {
	case "owner":
		return true
	case "admin":
		return action != "ownership" && action != "approval"
	case "analyst":
		return slices.Contains([]string{"evidence.read", "evidence.write", "execution", "export"}, action)
	case "reviewer":
		return slices.Contains([]string{"evidence.read", "approval", "export"}, action)
	case "runner":
		return slices.Contains([]string{"evidence.read", "evidence.write", "execution", "enrollment"}, action)
	case "viewer":
		return action == "evidence.read"
	}
	return false
}

// Access reloads a private regular policy file for every decision. It contains no
// cache of successful identities: unreadable or invalid replacement denies all.
type Access struct{ path string }

func OpenAccess(path string) (*Access, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errAccess
	}
	a := &Access{path: path}
	_, e := a.policy()
	return a, e
}

// roles reads the policy's grants in one project when a project-log rule asks.
func (a *Access) roles(project string) accessPolicy {
	return func() (func(string) string, error) {
		policy, e := a.policy()
		if e != nil {
			return nil, e
		}
		return func(subject string) string { return policy.role(subject, project) }, nil
	}
}
func (a *Access) policy() (AccessPolicy, error) {
	b, e := readPrivatePolicy(a.path)
	if e != nil {
		return AccessPolicy{}, errAccess
	}
	return ReadAccessPolicy(b)
}

// Principal is only returned after signature/token validation and a current
// project grant. Never derive approval identity from a request body or header.
type Principal struct {
	Issuer  string
	Subject string
	Project string
	Role    string
	Kind    string
}

func (a *Access) Authorize(r *http.Request, project, action string) (Principal, error) {
	var none Principal
	if r.Context().Err() != nil || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.VerifiedChains[0]) == 0 || !validProject(project) || len(r.Header.Values("Authorization")) != 1 {
		return none, errAccess
	}
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") || len(value) > 16384 {
		return none, errAccess
	}
	token := strings.TrimPrefix(value, "Bearer ")
	p, e := a.policy()
	if e != nil {
		return none, e
	}
	now := time.Now()
	var subject, kind string
	var scopes []string
	if strings.HasPrefix(token, "rh_") {
		raw, e := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(token, "rh_"))
		if e != nil || len(raw) != 32 {
			return none, errAccess
		}
		h := sha256.Sum256([]byte(token))
		cert := sha256.Sum256(r.TLS.VerifiedChains[0][0].Raw)
		for _, t := range p.Tokens {
			if t.Hash == hex.EncodeToString(h[:]) && t.Project == project && t.Certificate == hex.EncodeToString(cert[:]) {
				expiry, _ := time.Parse(time.RFC3339, t.Expires)
				if !now.Before(expiry) {
					return none, errAccess
				}
				subject = t.Subject
				kind = t.Kind
				scopes = t.Actions
				break
			}
		}
	} else {
		subject, scopes, e = p.accessToken(token, now)
		if e != nil {
			return none, e
		}
		kind = "oidc"
	}
	role := p.role(subject, project)
	if subject == "" || !roleAllows(role, action) || !slices.Contains(scopes, action) || (role == "runner" && kind != "runner") {
		return none, errAccess
	}
	return Principal{Issuer: p.Issuer, Subject: subject, Project: project, Role: role, Kind: kind}, nil
}

// accessToken implements the constrained RFC 9068 resource-server profile:
// RS256 only, pinned issuer/key/client/resource, at+jwt type, bounded lifetime.
// OIDC ID tokens and token-controlled remote key URLs are never accepted.
func (p AccessPolicy) accessToken(token string, now time.Time) (string, []string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", nil, errAccess
	}
	decode := func(s string) ([]byte, error) { return base64.RawURLEncoding.Strict().DecodeString(s) }
	head, e := decode(parts[0])
	if e != nil {
		return "", nil, errAccess
	}
	var h struct {
		Alg  string `json:"alg"`
		Type string `json:"typ"`
		ID   string `json:"kid"`
	}
	if requireExactMembers(head, "alg", "typ", "kid") != nil || json.Unmarshal(head, &h, json.RejectUnknownMembers(true)) != nil || h.Alg != "RS256" || (h.Type != "at+jwt" && h.Type != "application/at+jwt") {
		return "", nil, errAccess
	}
	var key *rsa.PublicKey
	for _, k := range p.Keys {
		if k.ID == h.ID {
			key, e = k.public()
			break
		}
	}
	signature, e := decode(parts[2])
	if e != nil || key == nil {
		return "", nil, errAccess
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return "", nil, errAccess
	}
	body, e := decode(parts[1])
	if e != nil {
		return "", nil, errAccess
	}
	// The claims are the protocol's one access-token claim rule; this profile
	// also requires a jti.
	claims, e := hubprotocol.ReadAccessClaims(body, hubprotocol.ClaimPolicy{Issuer: p.Issuer, Audience: p.Audience, Clients: p.Clients, RequireID: true}, now)
	if e != nil {
		return "", nil, errAccess
	}
	return claims.Subject, claims.Scopes, nil
}
