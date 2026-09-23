package hubclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Session holds an active authenticated identity in memory. Following ADR-0006,
// the access token exists only in memory in the Go process; it is never written
// to disk, browser storage, or logs.
type Session struct {
	token           string
	Subject         string    `json:"subject"`
	Issuer          string    `json:"issuer"`
	Audience        string    `json:"audience"`
	Scopes          []string  `json:"scopes"`
	Expires         time.Time `json:"expires_at"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
}

// Token returns the unexported token string.
func (s *Session) Token() string {
	if s == nil {
		return ""
	}
	return s.token
}

// String masks the session to prevent accidental credential leakage in formatting.
func (s *Session) String() string {
	if s == nil {
		return "<nil session>"
	}
	return fmt.Sprintf("Session(subject=%s, issuer=%s, expires=%s)", s.Subject, s.Issuer, s.Expires.Format(time.RFC3339))
}

// Format masks the token under every format verb.
func (s *Session) Format(f fmt.State, _ rune) {
	io.WriteString(f, s.String())
}

// IsExpired reports whether the session has reached its expiry instant.
func (s *Session) IsExpired(now time.Time) bool {
	if s == nil || s.Expires.IsZero() {
		return true
	}
	return !now.Before(s.Expires)
}

// Allows reports whether the token's granted scopes include the requested action.
func (s *Session) Allows(action string) bool {
	if s == nil || s.IsExpired(time.Now()) {
		return false
	}
	for _, scope := range s.Scopes {
		if scope == action {
			return true
		}
	}
	return false
}

// BearerHeader returns the authorization header value.
func (s *Session) BearerHeader() string {
	if s == nil || s.token == "" {
		return ""
	}
	return "Bearer " + s.token
}

type authCallbackResult struct {
	code  string
	state string
	err   error
}

// AuthFlow coordinates the first-party system-browser PKCE authentication loopback.
type AuthFlow struct {
	mu          sync.Mutex
	Config      Config
	Verifier    string
	State       string
	Port        int
	RedirectURI string
	AuthURL     string
	listener    net.Listener
	server      *http.Server
	resultChan  chan authCallbackResult
	closed      bool
}

// GeneratePKCE creates the random verifier, challenge (S256), and state for PKCE.
func GeneratePKCE() (verifier, challenge, state string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	sb := make([]byte, 16)
	if _, err = rand.Read(sb); err != nil {
		return "", "", "", err
	}
	state = base64.RawURLEncoding.EncodeToString(sb)
	return verifier, challenge, state, nil
}

// StartAuthFlow initializes a local loopback server and constructs the authorization URL.
func StartAuthFlow(c Config) (*AuthFlow, error) {
	verifier, challenge, state, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("cannot generate PKCE parameters: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("cannot start loopback authentication listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	authURL, err := buildAuthURL(c.IdP, redirectURI, challenge, state)
	if err != nil {
		listener.Close()
		return nil, err
	}

	flow := &AuthFlow{
		Config:      c,
		Verifier:    verifier,
		State:       state,
		Port:        port,
		RedirectURI: redirectURI,
		AuthURL:     authURL,
		listener:    listener,
		resultChan:  make(chan authCallbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", flow.handleCallback)

	flow.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		flow.server.Serve(listener)
	}()

	return flow, nil
}

func buildAuthURL(idp IdPConfig, redirectURI, challenge, state string) (string, error) {
	u, err := url.Parse(idp.AuthorizeEndpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", idp.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(idp.Scopes, " "))
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("audience", idp.Audience)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (f *AuthFlow) handleCallback(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	query := r.URL.Query()
	returnedState := query.Get("state")
	code := query.Get("code")
	errParam := query.Get("error")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if errParam != "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<!DOCTYPE html><html><body><h3>Authentication Denied</h3><p>The identity provider returned an error. You can close this window.</p></body></html>`))
		f.sendResult("", "", fmt.Errorf("IdP returned error: %s", errParam))
		return
	}

	if returnedState != f.State {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<!DOCTYPE html><html><body><h3>Invalid State</h3><p>The state parameter did not match. You can close this window.</p></body></html>`))
		f.sendResult("", "", errors.New("state mismatch in authentication callback"))
		return
	}

	if code == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`<!DOCTYPE html><html><body><h3>Missing Code</h3><p>No authorization code received. You can close this window.</p></body></html>`))
		f.sendResult("", "", errors.New("missing authorization code in callback"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html><html><body><h3>Authentication Complete</h3><p>You can close this tab and return to readmit.</p></body></html>`))
	f.sendResult(code, returnedState, nil)
}

func (f *AuthFlow) sendResult(code, state string, err error) {
	select {
	case f.resultChan <- authCallbackResult{code: code, state: state, err: err}:
	default:
	}
}

// WaitForCallback waits for the browser redirect to arrive on the loopback listener.
func (f *AuthFlow) WaitForCallback(ctx context.Context) (code string, err error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-f.resultChan:
		if res.err != nil {
			return "", res.err
		}
		return res.code, nil
	}
}

// Close shuts down the loopback listener. The listener is closed here as well
// as through the server, because a server that has not begun serving yet
// would otherwise close it only once it does.
func (f *AuthFlow) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		if f.server != nil {
			f.server.Close()
		}
		f.listener.Close()
	}
}

// ExchangeCode exchanges the authorization code for an RFC 9068 access token at the token endpoint.
func ExchangeCode(ctx context.Context, c Config, code, verifier, redirectURI string) (*Session, error) {
	if code == "" || verifier == "" {
		return nil, errors.New("authorization code and verifier are required")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", c.IdP.ClientID)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, "POST", c.IdP.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request rejected with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("cannot read token response: %w", err)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   *int64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		return nil, errors.New("invalid token response from identity provider")
	}

	return ValidateAccessToken(tokenResp.AccessToken, c.IdP, time.Now())
}

// ValidateAccessToken parses and validates an RFC 9068 access token structure and claims.
func ValidateAccessToken(token string, idp IdPConfig, now time.Time) (*Session, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("token must be a 3-part JWT")
	}
	decode := func(s string) ([]byte, error) {
		return base64.RawURLEncoding.Strict().DecodeString(s)
	}
	headBytes, err := decode(parts[0])
	if err != nil {
		return nil, errors.New("invalid JWT header encoding")
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(headBytes, &header) != nil || header.Alg != "RS256" || (header.Typ != "at+jwt" && header.Typ != "application/at+jwt") || header.Kid == "" {
		return nil, errors.New("token must be RS256 with typ at+jwt and kid")
	}

	bodyBytes, err := decode(parts[1])
	if err != nil {
		return nil, errors.New("invalid JWT payload encoding")
	}

	var claims struct {
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
	if json.Unmarshal(bodyBytes, &claims) != nil {
		return nil, errors.New("invalid token claims")
	}

	if claims.Issuer != idp.Issuer {
		return nil, fmt.Errorf("token issuer %q does not match configured issuer %q", claims.Issuer, idp.Issuer)
	}
	if claims.Client != idp.ClientID {
		return nil, fmt.Errorf("token client_id %q does not match configured client_id %q", claims.Client, idp.ClientID)
	}
	if claims.Subject == "" || len(claims.Subject) > 256 {
		return nil, errors.New("invalid token subject")
	}
	if claims.Exp == nil || claims.Iat == nil {
		return nil, errors.New("token missing exp or iat")
	}

	nowSec := now.Unix()
	if *claims.Iat > nowSec || *claims.Exp <= nowSec || *claims.Exp <= *claims.Iat {
		return nil, errors.New("token is expired or not yet valid")
	}
	if *claims.Exp-*claims.Iat > 3600 {
		return nil, errors.New("token lifetime exceeds 1 hour limit")
	}
	if claims.Nbf != nil && *claims.Nbf > nowSec {
		return nil, errors.New("token not valid before nbf")
	}

	var audience string
	if json.Unmarshal(claims.Audience, &audience) != nil {
		var list []string
		if json.Unmarshal(claims.Audience, &list) != nil || len(list) != 1 {
			return nil, errors.New("token audience must match configured audience")
		}
		audience = list[0]
	}
	if audience != idp.Audience {
		return nil, fmt.Errorf("token audience %q does not match configured audience %q", audience, idp.Audience)
	}

	scopes := strings.Fields(claims.Scope)
	return &Session{
		token:           token,
		Subject:         claims.Subject,
		Issuer:          claims.Issuer,
		Audience:        audience,
		Scopes:          scopes,
		Expires:         time.Unix(*claims.Exp, 0),
		AuthenticatedAt: now,
	}, nil
}
