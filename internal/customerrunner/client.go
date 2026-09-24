package customerrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/transportsecurity"
)

// Enroll proves current certificate-bound enrollment and execution scope,
// approved environment, and exact engine/spec/profile agreement. No secret is saved.
func Enroll(ctx context.Context, c Config) (runnerprotocol.Lease, error) {
	return claim(ctx, remote{c}, hostClock{}, strings.ToLower(rand.Text()), "enrollment")
}

// Hub is the admission authority a runner holds its environment under: a
// claim for one job, its renewal while the job runs, and its release when the
// job ends. A claim or renewal answers a lease, which the runner holds to its
// own rule (see claim); any error refuses it, and a refused renewal stops the
// job. Every build asks the configured hub over mutual TLS; the package's
// tests hold one in memory.
type Hub interface {
	Claim(ctx context.Context, instance, job string) (runnerprotocol.Lease, error)
	Renew(ctx context.Context, instance, job string) (runnerprotocol.Lease, error)
	Release(ctx context.Context, instance, job string) error
}

// remote is the configured hub, asked over the certificate-bound mutual-TLS
// route. A claim and a renewal are the same request: the hub renews a lease
// the same instance and job already hold.
type remote struct{ c Config }

func (h remote) Claim(ctx context.Context, instance, job string) (runnerprotocol.Lease, error) {
	return admission(ctx, h.c, instance, job, "POST")
}
func (h remote) Renew(ctx context.Context, instance, job string) (runnerprotocol.Lease, error) {
	return admission(ctx, h.c, instance, job, "POST")
}
func (h remote) Release(ctx context.Context, instance, job string) error {
	_, err := admission(ctx, h.c, instance, job, "DELETE")
	return err
}

// claim and renew hold every lease the hub answers to the runner's own rule:
// it must still be current by clock and grant at most ten seconds, whatever
// the hub believes.
func claim(ctx context.Context, h Hub, clock Clock, instance, job string) (runnerprotocol.Lease, error) {
	lease, err := h.Claim(ctx, instance, job)
	if err != nil {
		return runnerprotocol.Lease{}, err
	}
	return checkLease(clock, lease)
}
func renew(ctx context.Context, h Hub, clock Clock, instance, job string) (runnerprotocol.Lease, error) {
	lease, err := h.Renew(ctx, instance, job)
	if err != nil {
		return runnerprotocol.Lease{}, err
	}
	return checkLease(clock, lease)
}
func checkLease(clock Clock, lease runnerprotocol.Lease) (runnerprotocol.Lease, error) {
	remaining := lease.Expires.Sub(clock.Now())
	if remaining <= 0 || remaining > 10*time.Second {
		return runnerprotocol.Lease{}, ErrRefused
	}
	return lease, nil
}

// ErrHubRefused names the refusal the hub itself answered with, as distinct
// from a local configuration or admission failure, so a caller can show the
// hub's reasoned answer without matching prose.
var ErrHubRefused = errors.New("hub refused admission")

func admission(ctx context.Context, c Config, instance, job, method string) (runnerprotocol.Lease, error) {
	var zero runnerprotocol.Lease
	if c.validate() != nil {
		return zero, ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	key, err := c.Key.locator().Read(ctx)
	if err != nil {
		return zero, ErrRefused
	}
	token, err := c.Token.locator().Read(ctx)
	if err != nil {
		return zero, ErrRefused
	}
	cert, err := privateRead(c.Certificate, 1<<20)
	if err != nil || len(cert) > 1<<20 {
		return zero, ErrRefused
	}
	ca, err := privateRead(c.CA, 1<<20)
	if err != nil || len(ca) > 1<<20 {
		return zero, ErrRefused
	}
	u, err := url.Parse(c.Hub)
	if err != nil {
		return zero, ErrRefused
	}
	tc, err := transportsecurity.ClientConfig(u.Hostname(), ca)
	if err != nil {
		return zero, ErrRefused
	}
	pair, err := tls.X509KeyPair(cert, key.Expose())
	if err != nil {
		return zero, ErrRefused
	}
	tc.MinVersion = tls.VersionTLS13
	tc.Certificates = []tls.Certificate{pair}
	tr := &http.Transport{TLSClientConfig: tc, DisableKeepAlives: true, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 8192}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRefused }}
	pin := engine.Current("readmit-test/v1")
	raw, _ := json.Marshal(runnerprotocol.Request{Schema: "readmit-runner-request/v1", Environment: c.Environment, Instance: instance, Job: job, Engine: pin.Engine, Spec: pin.Spec, Profile: pin.Profile})
	req, err := http.NewRequestWithContext(ctx, method, c.Hub+"/v1/projects/"+c.Project+"/runner", bytes.NewReader(raw))
	if err != nil {
		return zero, ErrRefused
	}
	req.Header.Set("Authorization", "Bearer "+string(token.Expose()))
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return zero, ErrRefused
	}
	defer response.Body.Close()
	if method == "DELETE" && response.StatusCode == 204 {
		return zero, nil
	}
	if response.StatusCode != 200 {
		return zero, fmt.Errorf("%w: %w (%s): %s", ErrRefused, ErrHubRefused, http.StatusText(response.StatusCode), refusalReason(response.Body))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return zero, ErrRefused
	}
	lease, err := runnerprotocol.DecodeLease(data)
	if err != nil {
		return zero, ErrRefused
	}
	return lease, nil
}

// refusalReason carries the hub's own fixed refusal phrase, so the
// application can show why admission was denied instead of a bare refusal.
// The phrase is bounded server text; it is diagnostic, never command output.
func refusalReason(body io.Reader) string {
	reason, _ := io.ReadAll(io.LimitReader(body, 256))
	reason = []byte(strings.TrimSpace(string(reason)))
	for i, r := range string(reason) {
		if r < 0x20 || r == 0x7f {
			reason = reason[:i]
			break
		}
	}
	if len(reason) == 0 {
		return "no reason given"
	}
	return string(reason)
}
