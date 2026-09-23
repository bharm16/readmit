package hub

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"time"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
)

// sendScheduleAlert has no proxy, redirects, authentication headers or response
// logging. Routing is an administrator-approved HTTPS origin, never a template.
func sendScheduleAlert(ctx context.Context, destination string, body []byte) error {
	return scheduleAlert(ctx, destination, body, nil)
}
func scheduleAlert(ctx context.Context, destination string, body []byte, roots *x509.CertPool) error {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 8192}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(ctx, "POST", destination, bytes.NewReader(body))
	if e != nil {
		return ErrSchedule
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Do(req)
	if e != nil {
		return ErrSchedule
	}
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ErrSchedule
	}
	return nil
}

// ExecuteScheduledRun uses the existing customer-runner execution boundary.
func ExecuteScheduledRun(ctx context.Context, spec Schedule, id string) string {
	c, e := customerrunner.ReadConfig(spec.Runner)
	if e != nil {
		return "error"
	}
	// RunPinned uses the ordinary remote mTLS admission route and renewal loop;
	// executing in this process never bypasses current hub authorization.
	summary, e := customerrunner.RunPinned(ctx, c, customerrunner.Job{Schema: "readmit-runner-job/v1", ID: id, Spec: spec.Spec}, spec.Input)
	if summary.DeliveryUncertain || summary.JournalIncomplete {
		return "uncertain"
	}
	if ctx.Err() != nil {
		return "cancelled"
	}
	if e != nil {
		return "error"
	}
	switch summary.State {
	case durablerun.Passed:
		return "passed"
	case durablerun.AssertionFailed:
		return "failed"
	case durablerun.Cancelled:
		return "cancelled"
	default:
		return "error"
	}
}

// ServeSchedules requires the Store's exclusive database lease. Its journal is
// tied to this artifact root; an absent or changed policy fails closed.
func (s *Store) ServeSchedules(ctx context.Context, access *Access, runnerPath, policyPath string) error {
	return s.serveSchedules(ctx, access, runnerPath, policyPath, time.Now)
}

// serveSchedules waits out the runner hold on clock. Application builds pass
// time.Now; only tests pass another clock.
func (s *Store) serveSchedules(ctx context.Context, access *Access, runnerPath, policyPath string, clock func() time.Time) error {
	if access == nil || runnerPath == "" {
		return ErrSchedule
	}
	p, e := schedulePolicyFile(policyPath)
	if e != nil {
		return e
	}
	authorize := func() error {
		current, e := schedulePolicyFile(policyPath)
		if e != nil || policyHash(current) != policyHash(p) {
			return ErrSchedule
		}
		return nil
	}
	execute := func(jobCtx context.Context, spec Schedule, id string) string {
		return ExecuteScheduledRun(customerrunner.WithOperationGuard(jobCtx, s.operationGuard()), spec, id)
	}
	scheduler, e := OpenScheduler(scheduleDirectory(s.config.Root), p, execute, sendScheduleAlert, authorize)
	if e != nil {
		return e
	}
	defer scheduler.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handler, readyAt, e := s.runnerService(ctx, access, runnerPath, clock)
	if e != nil {
		return e
	}
	result := make(chan error, 1)
	go func() { result <- s.serve(ctx, handler); cancel() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var scheduleErr error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			if clock().Before(readyAt) {
				continue
			}
			// Changes require stop/review; removing the file stops admission, rather than
			// retaining authority from a stale in-memory configuration.
			current, err := schedulePolicyFile(policyPath)
			if err != nil || policyHash(current) != policyHash(p) {
				scheduleErr = ErrSchedule
				cancel()
				break loop
			}
			if err = scheduler.Tick(ctx, time.Now()); err != nil && ctx.Err() == nil {
				scheduleErr = err
				cancel()
				break loop
			}
		}
	}
	serveErr := <-result
	if scheduleErr != nil {
		return scheduleErr
	}
	return serveErr
}
