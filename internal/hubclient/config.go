// Package hubclient provides a narrow, typed Go client for connecting to a
// customer-controlled artifact hub, authenticating with a customer IdP over
// mTLS, discovering authorized projects, and transferring artifacts. The same
// transport reaches an operator-only hub's artifact store with the client
// certificate alone (OperatorClient).
package hubclient

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/secret"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const (
	// Schema is the strict JSON contract for hub client configuration.
	Schema = "readmit-hub-client/v1"

	// MaxConfigBytes bounds one hub client configuration document.
	MaxConfigBytes = 64 << 10

	// MaxProjects bounds the projects a client may configure to probe.
	MaxProjects = 256
)

var (
	// ErrUnsupportedVersion reports a configuration declaring an unsupported schema.
	ErrUnsupportedVersion = errors.New("unsupported hub client configuration version")

	// ErrRefused reports invalid, unreadable, or denied hub configuration.
	ErrRefused = errors.New("hub configuration refused; check parameters and certificate references")
)

var validScopes = []string{
	"evidence.read",
	"evidence.write",
	"execution",
	"approval",
	"export",
	"enrollment",
	"admin",
	"ownership",
}

// KeyReference names the program that reads the client certificate's private key.
// Following ADR-0006, readmit holds no key value; it runs the declared command.
type KeyReference struct {
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
}

// Locator converts the key reference into a secret.Locator.
func (k KeyReference) Locator() secret.Locator {
	return secret.Locator{Command: k.Command, Arguments: k.Arguments}
}

// IdPConfig declares the customer identity provider parameters.
type IdPConfig struct {
	Issuer            string   `json:"issuer"`
	ClientID          string   `json:"client_id"`
	Audience          string   `json:"audience"`
	AuthorizeEndpoint string   `json:"authorize_endpoint"`
	TokenEndpoint     string   `json:"token_endpoint"`
	Scopes            []string `json:"scopes"`
}

// Config declares the customer-controlled hub endpoint, credentials references,
// customer IdP endpoints, and authorized projects to probe.
type Config struct {
	Schema      string       `json:"schema"`
	Hub         string       `json:"hub"`
	CA          string       `json:"ca"`
	Certificate string       `json:"certificate"`
	Key         KeyReference `json:"key"`
	IdP         IdPConfig    `json:"idp"`
	Projects    []string     `json:"projects"`
}

var configDoc = strictdoc.Document{
	MaxBytes:    MaxConfigBytes,
	Schema:      Schema,
	Required:    []string{"hub", "ca", "certificate", "key", "idp", "projects"},
	Invalid:     "invalid hub client configuration",
	TooLarge:    "hub client configuration exceeds its size limit",
	MustDeclare: "a hub client configuration declares its contract version",
	Unsupported: ErrUnsupportedVersion,
	Requires:    "a hub client configuration declares all required members",
}

func requireExactMembers(data []byte, names ...string) error {
	var m map[string]jsontext.Value
	if json.Unmarshal(data, &m) != nil || len(m) != len(names) {
		return ErrRefused
	}
	for _, n := range names {
		if v, ok := m[n]; !ok || string(v) == "null" {
			return ErrRefused
		}
	}
	return nil
}

// DecodeConfig reads and validates a strict hub client configuration document.
func DecodeConfig(data []byte) (Config, error) {
	var c Config
	if err := configDoc.Decode(data, &c); err != nil {
		return c, err
	}
	var raw struct {
		Key jsontext.Value `json:"key"`
		IdP jsontext.Value `json:"idp"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return c, ErrRefused
	}
	if requireExactMembers(raw.Key, "command", "arguments") != nil {
		return c, ErrRefused
	}
	if requireExactMembers(raw.IdP, "issuer", "client_id", "audience", "authorize_endpoint", "token_endpoint", "scopes") != nil {
		return c, ErrRefused
	}
	if err := Validate(c); err != nil {
		return c, err
	}
	return c, nil
}

// ReadConfig reads a hub client configuration file from disk strictly.
func ReadConfig(path string) (Config, error) {
	data, err := readConfigFile(path)
	if err != nil {
		return Config{}, err
	}
	return DecodeConfig(data)
}

// readConfigFile reads one regular configuration file within its bound.
func readConfigFile(path string) ([]byte, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, ErrRefused
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxConfigBytes {
		return nil, ErrRefused
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, ErrRefused
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxConfigBytes+1))
	if err != nil || len(data) > MaxConfigBytes {
		return nil, ErrRefused
	}
	return data, nil
}

// Validate verifies all parameters of a Config struct.
func Validate(c Config) error {
	if c.Schema != Schema {
		return ErrUnsupportedVersion
	}
	if err := validateIdentity(c.Hub, c.CA, c.Certificate, c.Key); err != nil {
		return err
	}
	if err := validateIdP(c.IdP); err != nil {
		return err
	}
	if len(c.Projects) == 0 {
		return errors.New("at least one project must be configured")
	}
	if len(c.Projects) > MaxProjects {
		return errors.New("configured projects exceed limit")
	}
	seen := make(map[string]bool, len(c.Projects))
	for _, p := range c.Projects {
		if !validProject(p) {
			return errors.New("invalid project identifier: " + p)
		}
		if seen[p] {
			return errors.New("duplicate project configured: " + p)
		}
		seen[p] = true
	}
	return nil
}

// validateIdentity holds the hub endpoint and the mutual-TLS client identity
// every hub client configuration names to one set of rules.
func validateIdentity(hub, ca, certificate string, key KeyReference) error {
	u, err := url.Parse(hub)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("hub endpoint must be a valid https URL with host and optional port")
	}
	if !filepath.IsAbs(ca) || filepath.Clean(ca) != ca {
		return errors.New("ca must be a cleaned absolute file path")
	}
	if !filepath.IsAbs(certificate) || filepath.Clean(certificate) != certificate {
		return errors.New("certificate must be a cleaned absolute file path")
	}
	if err := key.Locator().Validate(); err != nil {
		return errors.New("key reference: " + err.Error())
	}
	return nil
}

func validateIdP(idp IdPConfig) error {
	u, err := url.Parse(idp.Issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("idp issuer must be a valid https URL without query or userinfo")
	}
	if strings.TrimSpace(idp.ClientID) == "" || len(idp.ClientID) > 256 {
		return errors.New("idp client_id must not be empty and at most 256 characters")
	}
	if strings.TrimSpace(idp.Audience) == "" || len(idp.Audience) > 256 {
		return errors.New("idp audience must not be empty and at most 256 characters")
	}
	authURL, err := url.Parse(idp.AuthorizeEndpoint)
	if err != nil || (authURL.Scheme != "https" && !(authURL.Scheme == "http" && isLoopback(authURL.Hostname()))) || authURL.Host == "" {
		return errors.New("idp authorize_endpoint must be a valid https URL (or http loopback for tests)")
	}
	tokenURL, err := url.Parse(idp.TokenEndpoint)
	if err != nil || (tokenURL.Scheme != "https" && !(tokenURL.Scheme == "http" && isLoopback(tokenURL.Hostname()))) || tokenURL.Host == "" {
		return errors.New("idp token_endpoint must be a valid https URL (or http loopback for tests)")
	}
	if len(idp.Scopes) == 0 {
		return errors.New("idp scopes must not be empty")
	}
	seen := make(map[string]bool, len(idp.Scopes))
	for _, s := range idp.Scopes {
		if !slices.Contains(validScopes, s) {
			return errors.New("unsupported scope: " + s)
		}
		if seen[s] {
			return errors.New("duplicate scope: " + s)
		}
		seen[s] = true
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func validProject(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
