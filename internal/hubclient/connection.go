package hubclient

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	// ErrNotSelected reports that no hub configuration is selected.
	ErrNotSelected = errors.New("no customer hub configuration selected")

	// ErrNotConnected reports a request with no connection to the hub.
	ErrNotConnected = errors.New("not connected to customer hub")

	// ErrSignInRequired reports a request with no current session: never
	// signed in, signed out, or expired.
	ErrSignInRequired = errors.New("sign-in required or session expired")

	// ErrNoSignIn reports a sign-in completion with no sign-in pending.
	ErrNoSignIn = errors.New("no authentication flow in progress; start sign-in first")

	// ErrNotRemembered reports a selection the connection's memory could not
	// keep, so the selection is unchanged.
	ErrNotRemembered = errors.New("cannot retain the hub configuration selection, so the selection is unchanged; choose a hub configuration again")

	// ErrSignInTimedOut reports a sign-in whose browser never returned.
	ErrSignInTimedOut = errors.New("sign-in timed out: the browser did not return; sign in again")

	// ErrStateMismatch reports a sign-in answered with a state its flow did
	// not issue.
	ErrStateMismatch = errors.New("state mismatch in authentication response")
)

// Memory keeps the path of the selected configuration for the next
// application to restore. The application owns where and how it is kept.
type Memory interface {
	// Recall answers the path an earlier application remembered: empty with
	// no error when nothing is remembered, and an error when what is
	// remembered cannot be read.
	Recall() (string, error)
	// Remember keeps path. An error means it is not kept.
	Remember(path string) error
}

// Connection is one application's session with its customer hub, from the
// configuration a person selects to the session they sign out of: select,
// connect, sign in, expire and disconnect. It holds the selected
// configuration, the mutual-TLS client, the signed-in session and the one
// sign-in that may be pending, under one lock, and every rule about how they
// change together lives here. The token stays in memory in this process.
type Connection struct {
	mu     sync.Mutex
	memory Memory
	// path is the selected configuration's path, or the path a remembered
	// selection named when it could not be restored.
	path   string
	config *Config
	// refusal says why a remembered selection could not be restored, until
	// a configuration is selected.
	refusal string
	client  *Client
	session *Session
	flow    *AuthFlow
}

// NewConnection is a connection with nothing selected. memory, when not nil,
// keeps every selection for the next application; nothing is recalled.
func NewConnection(memory Memory) *Connection {
	return &Connection{memory: memory}
}

// RestoreConnection is a connection over the selection memory recalls.
// Restoring reads what memory keeps and the configuration it names, and
// nothing else: it connects to no hub, starts no sign-in and renews no
// session, so a restored configuration is selected and offline. A remembered
// selection that cannot be restored is not dropped: why is kept, beside the
// path it named, until a configuration is selected again.
func RestoreConnection(memory Memory) *Connection {
	c := &Connection{memory: memory}
	path, err := memory.Recall()
	switch {
	case err != nil:
		c.refusal = "the remembered hub selection cannot be read; choose a hub configuration again"
	case path == "":
	default:
		c.path = path
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			c.refusal = "the remembered hub configuration is no longer there; choose a hub configuration again"
			break
		}
		cfg, err := ReadConfig(path)
		if err != nil {
			c.refusal = "the remembered hub configuration no longer validates (" + err.Error() + "); choose a hub configuration again"
			break
		}
		c.config = &cfg
	}
	return c
}

// Status is what a connection holds at one instant.
type Status struct {
	// ConfigPath is the selected configuration's path, or the path a
	// remembered selection named when it could not be restored.
	ConfigPath string
	// Config is the selected configuration; nil when none is selected.
	Config *Config
	// Refusal says why a remembered selection could not be restored.
	Refusal string
	// Client is the connection to the hub; nil when not connected.
	Client *Client
	// Session is the signed-in session, current or expired; nil before a
	// sign-in and after a disconnect.
	Session *Session
}

// SignedIn reports whether the status holds a connection and a session that
// has not expired: what every hub request needs.
func (s Status) SignedIn() bool {
	return s.Client != nil && s.Session != nil && !s.Session.IsExpired(time.Now())
}

// Status reports what the connection holds now.
func (c *Connection) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{ConfigPath: c.path, Config: c.config, Refusal: c.refusal, Client: c.client, Session: c.session}
}

// Select makes the configuration at path the selection. The path is
// remembered before it is selected, so a selection the next application
// could not restore is refused and changes nothing. Selecting ends a pending
// sign-in and drops the client and session, which belonged to the
// configuration it replaces, and clears a restore refusal.
func (c *Connection) Select(path string) (Config, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Config{}, errors.New("select a cleaned absolute configuration file path")
	}
	cfg, err := ReadConfig(path)
	if err != nil {
		return Config{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.memory != nil && c.memory.Remember(path) != nil {
		return Config{}, ErrNotRemembered
	}
	c.endSignIn()
	c.client, c.session = nil, nil
	c.path, c.config, c.refusal = path, &cfg, ""
	return cfg, nil
}

// Connect opens the mutual-TLS client to the selected hub and checks its
// liveness and readiness probes. A session already signed in rides the new
// client. A failed check keeps the connection as it was.
func (c *Connection) Connect(ctx context.Context) error {
	c.mu.Lock()
	cfg, session := c.config, c.session
	c.mu.Unlock()
	if cfg == nil {
		return ErrNotSelected
	}
	client, err := New(ctx, *cfg, session)
	if err != nil {
		return fmt.Errorf("cannot create mutual TLS client: %v", err)
	}
	if err := client.CheckHealth(ctx); err != nil {
		return fmt.Errorf("hub connection failed: %v", err)
	}
	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
	return nil
}

// Disconnect ends the pending sign-in and drops the client and the session;
// the token is gone from memory. The selection stays.
func (c *Connection) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endSignIn()
	c.client, c.session = nil, nil
}

// StartSignIn begins a PKCE sign-in with a new loopback listener, verifier
// and state, ending any sign-in still pending, and answers the authorization
// URL the browser opens and the listener's port.
func (c *Connection) StartSignIn() (string, int, error) {
	c.mu.Lock()
	cfg := c.config
	c.endSignIn()
	c.mu.Unlock()
	if cfg == nil {
		return "", 0, ErrNotSelected
	}
	flow, err := StartAuthFlow(*cfg)
	if err != nil {
		return "", 0, err
	}
	c.mu.Lock()
	c.endSignIn()
	c.flow = flow
	c.mu.Unlock()
	return flow.AuthURL, flow.Port, nil
}

// CompleteSignIn ends the pending sign-in, with a session or without one. An
// empty code waits up to wait for the browser to return to the listener;
// otherwise code and state are what the browser was answered with. It takes
// the attempt StartSignIn opened, so however it ends — refused, forged,
// cancelled, timed out or answered — the listener is closed before anything
// else happens and no later call can complete it; nothing is retried. When
// ctx ends before the session is kept, nothing is kept and ctx's error is
// answered. A connected client is renewed with the new session.
func (c *Connection) CompleteSignIn(ctx context.Context, code, state string, wait time.Duration) error {
	c.mu.Lock()
	cfg, flow := c.config, c.flow
	c.flow = nil
	c.mu.Unlock()
	if cfg == nil {
		if flow != nil {
			flow.Close()
		}
		return ErrNotSelected
	}
	if flow == nil {
		return ErrNoSignIn
	}
	var err error
	if code == "" {
		waitCtx, cancel := context.WithTimeout(ctx, wait)
		code, err = flow.WaitForCallback(waitCtx)
		cancel()
		state = flow.State
	}
	flow.Close()
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, context.DeadlineExceeded):
		return ErrSignInTimedOut
	case err != nil:
		return fmt.Errorf("authentication callback failed: %w", err)
	case state != flow.State:
		return ErrStateMismatch
	}
	session, err := ExchangeCode(ctx, *cfg, code, flow.Verifier, flow.RedirectURI)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("token exchange failed: %w", err)
	}
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client != nil {
		if renewed, err := New(ctx, *cfg, session); err == nil {
			client = renewed
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.mu.Lock()
	c.session, c.client = session, client
	c.mu.Unlock()
	return nil
}

// EndSignIn ends a pending sign-in without completing it: its listener
// closes and no later call can complete it.
func (c *Connection) EndSignIn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endSignIn()
}

func (c *Connection) endSignIn() {
	if c.flow != nil {
		c.flow.Close()
		c.flow = nil
	}
}

// SignedIn is the gate every hub request passes: the connected client and its
// current session. It answers ErrNotConnected without a client, and
// ErrSignInRequired without a session or once the session has expired.
func (c *Connection) SignedIn() (*Client, *Session, error) {
	status := c.Status()
	if status.Client == nil {
		return nil, nil, ErrNotConnected
	}
	if !status.SignedIn() {
		return nil, nil, ErrSignInRequired
	}
	return status.Client, status.Session, nil
}
