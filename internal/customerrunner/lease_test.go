package customerrunner_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// storedLease is the lease the runner last stored for its root.
func storedLease(t *testing.T, root string) runnerprotocol.Lease {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".active", "lease.json"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := runnerprotocol.DecodeLease(raw)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

// A job holds its environment by claiming a lease, renewing it every second
// for as long as it runs — well past what any one lease granted — and
// releasing it when it ends. Each renewal is stored and moves the expiry the
// job is stopped at.
func TestRunHoldsItsLeaseThroughRenewalsAndReleasesIt(t *testing.T) {
	c, _ := localConfig(t)
	clock := newFakeClock()
	hub := newMemoryHub(clock)
	fixture := newReceiver(t)
	job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "nightly-001", Spec: jobSpec(t, fixture.address, "lab")}
	runs := start(t, c, job, hub, clock)
	fixture.waitReceived(t)
	hub.expect(t, "claim nightly-001", "renew nightly-001")
	expires := clock.Now().Add(10 * time.Second)
	if !clock.armedAt(expires) || !storedLease(t, c.Root).Expires.Equal(expires) {
		t.Fatal("the job is not timed by the lease it holds")
	}
	for range 12 {
		clock.Advance(time.Second)
		hub.expect(t, "renew nightly-001")
		expires = clock.Now().Add(10 * time.Second)
		eventually(t, "the renewed lease to time the job", func() bool { return clock.armedAt(expires) })
	}
	if lease := storedLease(t, c.Root); !lease.Expires.Equal(expires) {
		t.Fatalf("stored lease %v; want the last renewal's %v", lease.Expires, expires)
	}
	if health, err := customerrunner.Health(c.Root); err != nil || health.State != "lease_current" {
		t.Fatalf("health while the job runs: %+v %v", health, err)
	}
	close(fixture.ack)
	run := finished(t, runs)
	if run.err != nil || run.summary.State != durablerun.Passed {
		t.Fatalf("run: %+v %v", run.summary, run.err)
	}
	hub.expect(t, "release nightly-001")
	hub.quiet(t)
	if health, err := customerrunner.Health(c.Root); err != nil || health.State != "idle" || health.Jobs != 1 {
		t.Fatalf("health after the job: %+v %v", health, err)
	}
	recovery, err := customerrunner.Recover(c.Root, job.ID)
	if err != nil || recovery.Acknowledged != 1 || recovery.Run.State != durablerun.Passed {
		t.Fatalf("recovery: %+v %v", recovery, err)
	}
}

// A lease nobody renews stops the job when it expires, on its own timer,
// while the renewal is still unanswered; a renewal that arrives afterwards
// does not revive it. The delivery stays uncertain and the id is never run
// again.
func TestRunStopsWhenItsLeaseExpiresUnrenewed(t *testing.T) {
	c, _ := localConfig(t)
	clock := newFakeClock()
	hub := newMemoryHub(clock)
	fixture := newReceiver(t)
	job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "nightly-001", Spec: jobSpec(t, fixture.address, "lab")}
	runs := start(t, c, job, hub, clock)
	fixture.waitReceived(t)
	hub.expect(t, "claim nightly-001", "renew nightly-001")
	expires := clock.Now().Add(10 * time.Second)
	stall := make(chan struct{})
	hub.set(func(h *memoryHub) { h.stall = stall })
	clock.Advance(time.Second)
	hub.expect(t, "renew nightly-001")
	clock.Advance(8 * time.Second)
	if !clock.armedAt(expires) {
		t.Fatal("the job was stopped before its lease expired")
	}
	clock.Advance(time.Second)
	fixture.waitEnded(t)
	close(stall)
	run := finished(t, runs)
	if run.err != nil || run.summary.State != durablerun.DeliveryUncertain || run.summary.StopReason != durablerun.Cancelled {
		t.Fatalf("expiry: %+v %v", run.summary, run.err)
	}
	hub.expect(t, "release nightly-001")
	recovery, err := customerrunner.Recover(c.Root, job.ID)
	if err != nil || !recovery.Run.DeliveryUncertain || recovery.SafeToRepeat {
		t.Fatalf("recovery: %+v %v", recovery, err)
	}
	if run := admitted(t, c, job, hub, clock); !errors.Is(run.err, customerrunner.ErrRetained) || run.summary.Schema != "" {
		t.Fatalf("an expired job ran again: %+v %v", run.summary, run.err)
	}
	hub.expect(t, "claim nightly-001", "release nightly-001")
	hub.quiet(t)
}

// A hub that revokes the runner, or narrows the duration or the number of
// jobs it grants, stops the job at the next renewal, leaving its delivery
// uncertain, and the runner still releases what it held.
func TestRunStopsWhenTheHubRevokesOrNarrowsItsLease(t *testing.T) {
	for name, change := range map[string]func(*memoryHub){
		"revoked":           func(h *memoryHub) { h.revoked = true },
		"a shorter job":     func(h *memoryHub) { h.maxSeconds-- },
		"fewer jobs":        func(h *memoryHub) { h.maxJobs-- },
		"another job holds": func(h *memoryHub) { h.holder = "another" },
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := localConfig(t)
			clock := newFakeClock()
			hub := newMemoryHub(clock)
			fixture := newReceiver(t)
			job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "nightly-001", Spec: jobSpec(t, fixture.address, "lab")}
			runs := start(t, c, job, hub, clock)
			fixture.waitReceived(t)
			hub.expect(t, "claim nightly-001", "renew nightly-001")
			hub.set(change)
			clock.Advance(time.Second)
			hub.expect(t, "renew nightly-001")
			fixture.waitEnded(t)
			run := finished(t, runs)
			if run.err != nil || run.summary.State != durablerun.DeliveryUncertain || run.summary.StopReason != durablerun.Cancelled {
				t.Fatalf("run: %+v %v", run.summary, run.err)
			}
			hub.expect(t, "release nightly-001")
			hub.quiet(t)
		})
	}
}

// The runner holds every lease to its own rule, whatever the hub believes: one
// granting more than ten seconds, or one already over, is refused before the
// job's id is reserved or anything is sent.
func TestRunRefusesALeaseItCannotHold(t *testing.T) {
	for name, ttl := range map[string]time.Duration{
		"longer than ten seconds": 10*time.Second + time.Nanosecond,
		"already over":            0,
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := localConfig(t)
			clock := newFakeClock()
			hub := newMemoryHub(clock)
			hub.ttl = ttl
			job := customerrunner.Job{Schema: "readmit-runner-job/v1", ID: "nightly-001", Spec: jobSpec(t, "127.0.0.1:9", "lab")}
			if run := admitted(t, c, job, hub, clock); !errors.Is(run.err, customerrunner.ErrRefused) {
				t.Fatalf("a lease of %v was held: %v", ttl, run.err)
			}
			hub.expect(t, "claim nightly-001")
			hub.quiet(t)
			if health, err := customerrunner.Health(c.Root); err != nil || health.State != "idle" || health.Jobs != 0 {
				t.Fatalf("health: %+v %v", health, err)
			}
		})
	}
}
