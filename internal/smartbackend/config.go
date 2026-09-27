// Package smartbackend implements explicit SMART Backend Services 2.2
// authorization. It is independent of Hub identity and grants no workflow pass.
package smartbackend

import (
	"encoding/json/v2"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/destination"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const ConfigSchema = "readmit-smart-backend/v1"
const StatusSchema = "readmit-smart-status/v1"

type Code string

const (
	Ready               Code = "ready"
	RegistrationMissing Code = "registration-missing"
	GrantInsufficient   Code = "grant-insufficient"
	ClockInvalid        Code = "clock-outside-validity"
	KeyUnavailable      Code = "key-unavailable"
	ServerUnsupported   Code = "server-unsupported"
	AuthorityChanged    Code = "authority-changed"
	AuthUnavailable     Code = "auth-unavailable"
	Disconnected        Code = "disconnected"
)

type Status struct {
	Schema string `json:"schema"`
	Code   Code   `json:"code"`
}

func (s Status) CredentialCode() string { return string(s.Code) }
func (s Status) Error() string          { return "SMART authorization refused: " + string(s.Code) }
func failure(c Code) error              { return Status{Schema: StatusSchema, Code: c} }

type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n,omitzero"`
	E   string `json:"e,omitzero"`
	Crv string `json:"crv,omitzero"`
	X   string `json:"x,omitzero"`
	Y   string `json:"y,omitzero"`
}
type JWKS struct {
	Keys []JWK `json:"keys"`
}
type KeyReference struct {
	Kid        string                 `json:"kid"`
	Generation string                 `json:"generation"`
	Locator    networkaction.Provider `json:"locator"`
	JWKS       JWKS                   `json:"jwks"`
	JWKSURL    string                 `json:"jwks_url,omitzero"`
}
type Config struct {
	Schema           string                 `json:"schema"`
	FHIRBase         string                 `json:"fhir_base"`
	TokenEndpoint    string                 `json:"token_endpoint"`
	ClientID         string                 `json:"client_id"`
	Audience         string                 `json:"audience"`
	Algorithm        string                 `json:"algorithm"`
	Role             string                 `json:"role"`
	Scopes           []string               `json:"scopes"`
	Key              KeyReference           `json:"key"`
	MetadataIdentity string                 `json:"metadata_identity,omitzero"`
	Token            networkaction.HTTPSpec `json:"token"`
}
type Client struct {
	config      Config
	identity    string
	token       *networkaction.RuntimeHTTPPlan
	public      JWK
	permissions map[string]string
}

var id = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var fhirID = regexp.MustCompile(`^[A-Za-z0-9\-.]{1,64}$`)

func validFHIRID(v string) bool { return fhirID.MatchString(v) && v != "." && v != ".." }

func https(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 4096 || u.RawQuery != "" || u.ForceQuery || u.RawPath != "" || (u.Path != "" && u.Path != path.Clean(u.Path)) || raw != u.String() || strings.Contains(u.Path, "//") {
		return false
	}
	_, e = destination.ScopedLink(raw, raw)
	return e == nil
}

// Prepare is purely local: even metadata and public JWKS are explicitly
// selected reviewed bytes. No discovery, key provider or token request occurs.
func Prepare(raw, policy, metadata []byte) (*Client, error) {
	var c Config
	if len(raw) > 2<<20 || json.Unmarshal(raw, &c, json.RejectUnknownMembers(true)) != nil || c.Schema != ConfigSchema {
		return nil, failure(ServerUnsupported)
	}
	if c.ClientID == "" || len(c.ClientID) > 256 || strings.ContainsAny(c.ClientID, "\r\n\x00") || !id.MatchString(c.Key.Kid) || !id.MatchString(c.Key.Generation) || len(c.Key.JWKS.Keys) == 0 {
		return nil, failure(RegistrationMissing)
	}
	if !https(c.FHIRBase) || strings.HasSuffix(c.FHIRBase, "/") || !https(c.TokenEndpoint) || c.Audience != c.TokenEndpoint || c.Token.URL != c.TokenEndpoint || c.Token.Operation != sendpolicy.SMARTToken || c.Token.Method != "POST" || len(c.Token.Body) != 0 || c.Token.Credential != nil || (c.Algorithm != "RS384" && c.Algorithm != "ES384") || (c.Role != "observer" && c.Role != "setup") {
		return nil, failure(ServerUnsupported)
	}
	if c.Key.JWKSURL != "" && !https(c.Key.JWKSURL) {
		return nil, failure(ServerUnsupported)
	}
	if (secret.Locator{Command: c.Key.Locator.Command, Arguments: c.Key.Locator.Arguments}).Validate() != nil {
		return nil, failure(KeyUnavailable)
	}
	permissions, err := parseScopes(c.Scopes)
	if err != nil {
		return nil, err
	}
	if c.Role == "observer" {
		for _, p := range permissions {
			if strings.ContainsAny(p, "cud") {
				return nil, failure(GrantInsufficient)
			}
		}
	}
	if len(c.Key.JWKS.Keys) > 32 {
		return nil, failure(RegistrationMissing)
	}
	var selected JWK
	seen := map[string]bool{}
	for _, key := range c.Key.JWKS.Keys {
		if seen[key.Kid] || !id.MatchString(key.Kid) {
			return nil, failure(RegistrationMissing)
		}
		seen[key.Kid] = true
		if _, err := publicKey(key); err != nil {
			return nil, err
		}
		if key.Kid == c.Key.Kid {
			selected = key
		}
	}
	if selected.Kid == "" || selected.Alg != c.Algorithm {
		return nil, failure(RegistrationMissing)
	}
	if c.MetadataIdentity != "" {
		discovery, discoveryErr := DecodeDiscovery(metadata)
		if !networkaction.ValidDigest(c.MetadataIdentity) || networkaction.Digest(metadata) != c.MetadataIdentity || discoveryErr != nil || discovery.FHIRBase != c.FHIRBase || validateMetadata(discovery.Metadata, c) != nil {
			return nil, failure(ServerUnsupported)
		}
	} else if len(metadata) != 0 {
		return nil, failure(ServerUnsupported)
	}
	canonical, _ := json.Marshal(c, json.Deterministic(true))
	identity := networkaction.Digest(append(append(canonical, byte('\n')), policy...))
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {strings.Join(c.Scopes, " ")}, "client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}}
	tokenSpec := c.Token
	tokenSpec.Body = []byte(form.Encode())
	tokenSpec.ContentType = "application/x-www-form-urlencoded"
	b, _ := json.Marshal(networkaction.RuntimeHTTPSpec{Schema: networkaction.RuntimeHTTPSchema, Authorization: identity, HTTP: tokenSpec})
	plan, err := networkaction.PrepareRuntimeHTTP(b, policy)
	if err != nil {
		return nil, failure(ServerUnsupported)
	}
	return &Client{config: c, identity: identity, token: plan, public: selected, permissions: permissions}, nil
}
func (c *Client) Identity() string                    { return c.identity }
func (c *Client) TokenBinding() networkaction.Binding { return c.token.Binding() }
func (c *Client) Preflight() Status                   { return Status{Schema: StatusSchema, Code: Ready} }
func parseScopes(scopes []string) (map[string]string, error) {
	if len(scopes) < 1 || len(scopes) > 64 {
		return nil, failure(GrantInsufficient)
	}
	out := map[string]string{}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if len(scope) > 256 || seen[scope] || !strings.HasPrefix(scope, "system/") {
			return nil, failure(GrantInsufficient)
		}
		seen[scope] = true
		parts := strings.Split(strings.TrimPrefix(scope, "system/"), ".")
		if len(parts) != 2 || (!fhirr4.KnownResourceType(parts[0]) && parts[0] != "*") || parts[1] == "" {
			return nil, failure(ServerUnsupported)
		}
		ordered := ""
		for _, p := range "cruds" {
			if strings.ContainsRune(parts[1], p) {
				ordered += string(p)
			}
		}
		if parts[1] != ordered {
			return nil, failure(ServerUnsupported)
		}
		for _, p := range ordered {
			if !strings.ContainsRune(out[parts[0]], p) {
				out[parts[0]] += string(p)
			}
		}
	}
	return out, nil
}
func covers(grant, requested map[string]string) bool {
	for r, permissions := range requested {
		for _, p := range permissions {
			if !strings.ContainsRune(grant[r]+grant["*"], p) {
				return false
			}
		}
	}
	return true
}
func validateMetadata(raw []byte, c Config) error {
	var m struct {
		TokenEndpoint string   `json:"token_endpoint"`
		GrantTypes    []string `json:"grant_types_supported"`
		Methods       []string `json:"token_endpoint_auth_methods_supported"`
		Algorithms    []string `json:"token_endpoint_auth_signing_alg_values_supported"`
		Capabilities  []string `json:"capabilities"`
	}
	// Discovery has extensible standardized members. Duplicate JSON members are
	// still rejected by json/v2; unknown claims are never used for permission.
	if len(raw) > 256<<10 || json.Unmarshal(raw, &m) != nil || m.TokenEndpoint != c.TokenEndpoint || !slices.Contains(m.GrantTypes, "client_credentials") || !slices.Contains(m.Methods, "private_key_jwt") || !slices.Contains(m.Algorithms, c.Algorithm) || !slices.Contains(m.Capabilities, "client-confidential-asymmetric") || !slices.Contains(m.Capabilities, "permission-v2") {
		return failure(ServerUnsupported)
	}
	return nil
}
