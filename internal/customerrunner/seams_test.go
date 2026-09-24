package customerrunner_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

// fakeClock is a customerrunner.Clock that moves only when the test advances
// it. It starts at the host's now, so a deadline taken from it is a sensible
// host deadline too.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

// fakeTimer is one pending AfterFunc call, or a ticker when ticks is set.
type fakeTimer struct {
	clock  *fakeClock
	at     time.Time
	armed  bool
	listed bool
	f      func()
	ticks  chan time.Time
	every  time.Duration
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Now()} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) customerrunner.Timer {
	t := &fakeTimer{clock: c, f: f}
	t.Reset(d)
	return t
}

func (c *fakeClock) Tick(d time.Duration) (<-chan time.Time, func()) {
	t := &fakeTimer{clock: c, ticks: make(chan time.Time, 1), every: d}
	c.mu.Lock()
	t.at, t.armed, t.listed = c.now.Add(d), true, true
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	return t.ticks, func() { t.Stop() }
}

// Reset arms the timer d from now; like the host's, a timer due now fires at
// once.
func (t *fakeTimer) Reset(d time.Duration) bool {
	c := t.clock
	c.mu.Lock()
	was := t.armed
	t.at, t.armed = c.now.Add(d), d > 0
	if t.armed && !t.listed {
		t.listed = true
		c.timers = append(c.timers, t)
	}
	c.mu.Unlock()
	if d <= 0 {
		go t.f()
	}
	return was
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := t.armed
	t.armed = false
	return was
}

// Advance moves the clock by d and fires what fell due: an AfterFunc call in
// its own goroutine, a tick dropped when the last one was not yet read.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.armed && !t.at.After(c.now) {
			if t.ticks != nil {
				select {
				case t.ticks <- t.at:
				default:
				}
				for !t.at.After(c.now) {
					t.at = t.at.Add(t.every)
				}
			} else {
				t.armed = false
				due = append(due, t.f)
			}
		}
		if t.armed {
			kept = append(kept, t)
		} else {
			t.listed = false
		}
	}
	c.timers = kept
	c.mu.Unlock()
	for _, f := range due {
		go f()
	}
}

// armedAt reports whether an AfterFunc call is pending for exactly at.
func (c *fakeClock) armedAt(at time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.timers {
		if t.armed && t.ticks == nil && t.at.Equal(at) {
			return true
		}
	}
	return false
}

// eventually waits, on the host's clock, for what the runner does in its own
// goroutines.
func eventually(t *testing.T, what string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !done(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("waited ten seconds for %s", what)
		}
	}
}

var errRevoked = errors.New("access refused")

// memoryHub is a customerrunner.Hub held in memory that answers by the
// test's clock as the customer hub does: a claim grants the environment to one
// instance and job for ttl, a renewal extends that same holder's lease, and a
// release ends it. Revoking it refuses every later answer, narrowing its
// grant narrows every later lease, and stalling it leaves renewals unanswered
// until the stall ends. Every call is recorded, in order, as it arrives.
type memoryHub struct {
	clock *fakeClock
	calls chan string

	mu         sync.Mutex
	ttl        time.Duration
	maxSeconds int
	maxJobs    int
	revoked    bool
	stall      chan struct{}
	holder     string
}

func newMemoryHub(clock *fakeClock) *memoryHub {
	return &memoryHub{clock: clock, calls: make(chan string, 256), ttl: 10 * time.Second, maxSeconds: 60, maxJobs: 4}
}

func (h *memoryHub) lease() runnerprotocol.Lease {
	return runnerprotocol.Lease{Schema: "readmit-runner-lease/v1", Expires: h.clock.Now().Add(h.ttl), MaxSeconds: h.maxSeconds, MaxJobs: h.maxJobs}
}

func (h *memoryHub) Claim(_ context.Context, instance, job string) (runnerprotocol.Lease, error) {
	h.calls <- "claim " + job
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.revoked {
		return runnerprotocol.Lease{}, errRevoked
	}
	h.holder = instance + "/" + job
	return h.lease(), nil
}

func (h *memoryHub) Renew(_ context.Context, instance, job string) (runnerprotocol.Lease, error) {
	h.calls <- "renew " + job
	h.mu.Lock()
	stall := h.stall
	h.mu.Unlock()
	if stall != nil {
		<-stall
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.revoked || h.holder != instance+"/"+job {
		return runnerprotocol.Lease{}, errRevoked
	}
	return h.lease(), nil
}

func (h *memoryHub) Release(_ context.Context, instance, job string) error {
	h.calls <- "release " + job
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.holder != instance+"/"+job {
		return errRevoked
	}
	h.holder = ""
	return nil
}

// set changes the hub's answers from now on.
func (h *memoryHub) set(change func(h *memoryHub)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	change(h)
}

// next is the hub's next recorded call.
func (h *memoryHub) next(t *testing.T) string {
	t.Helper()
	select {
	case call := <-h.calls:
		return call
	case <-time.After(10 * time.Second):
		t.Fatal("the runner asked the hub nothing for ten seconds")
		return ""
	}
}

// expect reads the hub's next calls, which must be want in order.
func (h *memoryHub) expect(t *testing.T, want ...string) {
	t.Helper()
	for _, call := range want {
		if got := h.next(t); got != call {
			t.Fatalf("the hub was asked %q; want %q", got, call)
		}
	}
}

// quiet reports that the hub has no unread call.
func (h *memoryHub) quiet(t *testing.T) {
	t.Helper()
	select {
	case call := <-h.calls:
		t.Fatalf("the hub was asked %q", call)
	default:
	}
}
