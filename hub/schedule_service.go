package hub

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runqueue"
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

// scheduledRun is the profile the hub's scheduler declares for a scheduled
// run: the runner admits each job it runs as its own execution.
var scheduledRun = operationguard.Profile{Name: "scheduled-run", Execution: operationguard.ExecuteEachJob}

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
	execute := func(jobCtx context.Context, spec Schedule, id string) (state string) {
		// Each scheduled job is admitted, bounded and settled as its own
		// execution by the runner that runs it, through the hub's guard. The
		// profile takes no admission of its own, so Run answers only what the
		// work does, and the work answers its state.
		_ = s.operationGuard().Run(jobCtx, scheduledRun, func(ctx context.Context) error {
			state = ExecuteScheduledRun(ctx, spec, id)
			return nil
		})
		return state
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

// ExecuteManagedRun runs one managed occurrence: every test of the prepared
// suite, in dependency order, each through the existing customer-runner
// execution boundary with its own pinned input. A test whose dependency did
// not pass is not run. The occurrence passes only when every test passed.
func ExecuteManagedRun(ctx context.Context, entry runnerprotocol.ScheduleEntry, key string) string {
	c, e := customerrunner.ReadConfig(entry.RunnerConfig)
	if e != nil {
		return "error"
	}
	identity, jobs, e := runqueue.PinnedJobs(ctx, entry.Spec)
	if e != nil || identity != entry.Input {
		return "error"
	}
	passed, done := map[string]bool{}, map[string]bool{}
	worst := "passed"
	rank := map[string]int{"passed": 0, "failed": 1, "error": 2, "cancelled": 3, "uncertain": 4}
	for progressed := true; progressed; {
		progressed = false
		for i, job := range jobs {
			if done[job.ID] {
				continue
			}
			ready, blocked := true, false
			for _, after := range job.After {
				ready = ready && done[after]
				blocked = blocked || done[after] && !passed[after]
			}
			if !ready {
				continue
			}
			done[job.ID], progressed = true, true
			state := "failed"
			if !blocked {
				state = executeQueuedJob(ctx, c, fmt.Sprintf("%s-%d", key, i), job)
			}
			passed[job.ID] = state == "passed"
			if rank[state] > rank[worst] {
				worst = state
			}
			if state == "uncertain" || state == "cancelled" {
				return state
			}
		}
	}
	return worst
}

func executeQueuedJob(ctx context.Context, c customerrunner.Config, id string, job runqueue.PinnedJob) string {
	summary, e := customerrunner.RunPinned(ctx, c, customerrunner.Job{Schema: "readmit-runner-job/v1", ID: id, Spec: job.Spec}, job.Input)
	switch {
	case summary.DeliveryUncertain || summary.JournalIncomplete:
		return "uncertain"
	case ctx.Err() != nil:
		return "cancelled"
	case e != nil:
		return "error"
	}
	switch summary.State {
	case durablerun.Passed:
		return "passed"
	case durablerun.AssertionFailed:
		return "failed"
	case durablerun.Cancelled:
		return "cancelled"
	}
	return "error"
}

// RevalidateManagedRun is the check before every managed run: the runner
// configuration is readable on this host and the prepared suite still
// prepares to the pinned input. A change is never approved here; the
// schedule is paused.
func RevalidateManagedRun(ctx context.Context, project string, entry runnerprotocol.ScheduleEntry) string {
	// A schedule runs only a runner configuration of its own project: an
	// administrator of one project cannot point the scheduler at another's.
	if c, e := customerrunner.ReadConfig(entry.RunnerConfig); e != nil || c.Project != project {
		return runnerprotocol.ReasonUnreadable
	}
	identity, _, e := runqueue.PinnedJobs(ctx, entry.Spec)
	if e != nil {
		return runnerprotocol.ReasonUnreadable
	}
	if identity != entry.Input {
		return runnerprotocol.ReasonPinChanged
	}
	return ""
}

// ServeManagedSchedules serves team access and runner admission with the
// managed schedule service: the schedule route acknowledges commands and the
// scheduler dispatches occurrences while this service runs, whether or not
// any application is open.
func (s *Store) ServeManagedSchedules(ctx context.Context, access *Access, runnerPath string) error {
	return s.serveManagedSchedules(ctx, access, runnerPath, time.Now, ExecuteManagedRun, RevalidateManagedRun)
}

func (s *Store) serveManagedSchedules(ctx context.Context, access *Access, runnerPath string, clock func() time.Time, execute ManagedExecutor, revalidate ManagedRevalidator) error {
	if access == nil || runnerPath == "" {
		return ErrSchedule
	}
	runnerExecute := func(jobCtx context.Context, entry runnerprotocol.ScheduleEntry, key string) (state string) {
		state = "error"
		_ = s.operationGuard().Run(jobCtx, scheduledRun, func(ctx context.Context) error {
			state = execute(ctx, entry, key)
			return nil
		})
		return state
	}
	// Before every run: a current runner grant for the runner's project and
	// environment, and an installed operation policy (the license).
	authority := func(project string, entry runnerprotocol.ScheduleEntry) error {
		policy, e := readRunnerPolicy(runnerPath)
		if e != nil || s.operations == nil {
			return ErrSchedule
		}
		c, e := customerrunner.ReadConfig(entry.RunnerConfig)
		if e != nil || c.Project != project {
			return ErrSchedule
		}
		for _, grant := range policy.Runners {
			if grant.Project == c.Project && grant.Environment == c.Environment {
				return nil
			}
		}
		return ErrSchedule
	}
	scheduler, e := OpenManagedScheduler(managedDirectory(s.config.Root), clock(), runnerExecute, revalidate, sendScheduleAlert, authority)
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
	s.managed.Store(scheduler)
	defer s.managed.Store(nil)
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
			if err := scheduler.Tick(ctx, clock()); err != nil && ctx.Err() == nil {
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
