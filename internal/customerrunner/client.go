package customerrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json/v2"
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
	return enroll(ctx, c, strings.ToLower(rand.Text()), "enrollment")
}
func enroll(ctx context.Context, c Config, instance, job string) (runnerprotocol.Lease, error) {
	return admission(ctx, c, instance, job, "POST")
}
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
		return zero, ErrRefused
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return zero, ErrRefused
	}
	lease, err := runnerprotocol.DecodeLease(data)
	remaining := time.Until(lease.Expires)
	if err != nil || remaining <= 0 || remaining > 10*time.Second {
		return zero, ErrRefused
	}
	return lease, nil
}
