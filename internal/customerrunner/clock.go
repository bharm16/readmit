package customerrunner

import "time"

// Clock is the time a runner holds its hub lease by: when now is, the timer
// that stops a job when its lease runs out unrenewed, and the ticker that
// renews it. Every build uses the host's clock; the package's tests use one
// that moves only when the test moves it. The job's own bound is a context
// deadline taken from Now, because the durable run records that deadline and
// reports reaching it as timed_out.
type Clock interface {
	Now() time.Time
	// AfterFunc calls f in its own goroutine once d has passed, unless the
	// timer is stopped first.
	AfterFunc(d time.Duration, f func()) Timer
	// Tick delivers the time every d, dropping ticks a slow reader misses,
	// until stop is called.
	Tick(d time.Duration) (ticks <-chan time.Time, stop func())
}

// Timer is a pending AfterFunc call.
type Timer interface {
	Stop() bool
	Reset(d time.Duration) bool
}

// hostClock is the host's clock, the production Clock.
type hostClock struct{}

func (hostClock) Now() time.Time                            { return time.Now() }
func (hostClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
func (hostClock) Tick(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}
