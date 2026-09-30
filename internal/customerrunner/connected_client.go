package customerrunner

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// ConnectedClient owns one durable connected dispatch. It holds no resolved
// credential and never retries an initial claim or attempted effect. A refused
// or ambiguous response stops effects; only explicit uncertain settlement is
// available after that. Caller-supplied requests and provider arguments are
// copied so the exact agreement cannot change beneath a live fence.
type ConnectedClient struct {
	mu       sync.Mutex
	config   Config
	request  runnerprotocol.ConnectedRequest
	lease    runnerprotocol.ConnectedLease
	started  time.Time
	deadline time.Time
	failed   bool
	settled  bool
	attempts map[string]bool
}

func EnrollConnected(ctx context.Context, c Config, request runnerprotocol.ConnectedRequest) (*ConnectedClient, error) {
	if c.validate() != nil || request.Validate() != nil || c.Environment != request.Environment || request.Engine != engine.Version() {
		return nil, ErrRefused
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, ErrRefused
	}
	owned, err := runnerprotocol.DecodeConnectedRequest(raw)
	if err != nil {
		return nil, ErrRefused
	}
	c.Key.Arguments = slices.Clone(c.Key.Arguments)
	c.Token.Arguments = slices.Clone(c.Token.Arguments)
	client := &ConnectedClient{config: c, request: owned, attempts: map[string]bool{}}
	data, err := client.call(ctx, "", raw, http.StatusOK)
	if err != nil {
		return nil, err
	}
	lease, err := runnerprotocol.DecodeConnectedLease(data)
	if err != nil || client.accept(lease, true) != nil {
		return nil, ErrRefused
	}
	return client, nil
}

func (c *ConnectedClient) Lease() runnerprotocol.ConnectedLease {
	if c == nil {
		return runnerprotocol.ConnectedLease{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lease
}

func (c *ConnectedClient) Check(ctx context.Context) error {
	if c == nil {
		return ErrRefused
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.check(ctx)
}

func (c *ConnectedClient) check(ctx context.Context) error {
	if c.settled || c.failed || !time.Now().Before(c.lease.Expires) || !time.Now().Before(c.deadline) {
		c.failed = true
		return ErrRefused
	}
	data, err := c.control(ctx, "renew", "", "", http.StatusOK)
	if err != nil {
		c.failed = true
		return err
	}
	lease, err := runnerprotocol.DecodeConnectedLease(data)
	if err != nil || c.accept(lease, false) != nil {
		c.failed = true
		return ErrRefused
	}
	return nil
}

func (c *ConnectedClient) accept(lease runnerprotocol.ConnectedLease, initial bool) error {
	now := time.Now()
	if initial {
		// The first short lease is capped at ten seconds or the whole job
		// duration, whichever is smaller. Its expiry therefore supplies the
		// issuer's admitted-at boundary without treating credential lookup or
		// transport latency before admission as execution time.
		c.started = lease.Expires.Add(-time.Duration(min(10, lease.MaxSeconds)) * time.Second)
	}
	deadline := c.started.Add(time.Duration(lease.MaxSeconds) * time.Second)
	remaining := lease.Expires.Sub(now)
	if remaining <= 0 || remaining > time.Duration(min(10, lease.MaxSeconds))*time.Second || lease.Expires.After(deadline) || !now.Before(deadline) ||
		!initial && (lease.Generation != c.lease.Generation || lease.MaxSeconds > c.lease.MaxSeconds || lease.MaxJobs > c.lease.MaxJobs) {
		return ErrRefused
	}
	if initial || deadline.Before(c.deadline) {
		c.deadline = deadline
	}
	c.lease = lease
	return nil
}

// Attempt renews the same fence, then durably records one numbered effect
// before the caller reaches its target. Reusing a step, losing authority or
// receiving an ambiguous response leaves no permission to continue.
func (c *ConnectedClient) Attempt(ctx context.Context, step string) error {
	if c == nil {
		return ErrRefused
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !runnerprotocol.ID(step) || c.attempts[step] {
		c.failed = true
		return ErrRefused
	}
	if err := c.check(ctx); err != nil {
		return err
	}
	c.attempts[step] = true
	if _, err := c.control(ctx, "attempt", step, "", http.StatusNoContent); err != nil {
		c.failed = true
		return err
	}
	if !time.Now().Before(c.lease.Expires) || !time.Now().Before(c.deadline) {
		c.failed = true
		return ErrRefused
	}
	return nil
}

// Settle reports the terminal state using only the admitted request and fence.
// It never renews or reacquires effect authority. A terminal uncertain result
// keeps resource ownership held; a settled result releases only this fence.
func (c *ConnectedClient) Settle(ctx context.Context, uncertain bool) error {
	if c == nil {
		return ErrRefused
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.settled || c.failed && !uncertain {
		return ErrRefused
	}
	outcome := "settled"
	if uncertain {
		outcome = "uncertain"
	}
	if _, err := c.control(ctx, "settle", "", outcome, http.StatusNoContent); err != nil {
		c.failed = true
		return err
	}
	c.settled = true
	return nil
}

func (c *ConnectedClient) control(ctx context.Context, action, step, outcome string, status int) ([]byte, error) {
	raw, err := json.Marshal(runnerprotocol.ConnectedCommand{Schema: runnerprotocol.ConnectedCommandSchema, Request: c.request, Generation: c.lease.Generation, Step: step, Outcome: outcome})
	if err != nil || len(raw) > runnerprotocol.MaxConnectedBytes {
		return nil, ErrRefused
	}
	return c.call(ctx, "/"+action, raw, status)
}

func (c *ConnectedClient) call(ctx context.Context, suffix string, raw []byte, status int) ([]byte, error) {
	if c.config.validate() != nil || len(raw) > runnerprotocol.MaxConnectedBytes {
		return nil, ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	response, finish, err := runnerRequest(ctx, c.config, http.MethodPost, "/v2/projects/"+c.config.Project+"/runner"+suffix, raw)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrRefused
	}
	defer finish()
	defer response.Body.Close()
	if response.StatusCode != status {
		// No response phrase is echoed: even an authenticated server can have
		// a diagnostic bug that includes a header, token or private target.
		return nil, fmt.Errorf("%w: %w (%s): %s", ErrRefused, ErrHubRefused, http.StatusText(response.StatusCode), connectedRefusal(response.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 {
		return nil, ErrRefused
	}
	return data, nil
}

func connectedRefusal(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "verified runner certificate required"
	case http.StatusForbidden:
		return "runner scope, engine or capability grant refused"
	case http.StatusConflict:
		return "connected dispatch, resource fence or authority transition refused"
	case http.StatusServiceUnavailable:
		return "connected authority, capacity or durable dispatch storage unavailable"
	default:
		return "connected runner request refused"
	}
}
