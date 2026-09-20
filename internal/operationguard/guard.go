// Package operationguard admits new commercial work locally. Evidence readers
// never acquire a guard; an absent or damaged installation only refuses new work.
package operationguard

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

// MaxDuration bounds an admitted execution even if its signed term ends first.
const MaxDuration = entitlement.MaxLease

var (
	ErrUnavailable = errors.New("operation activation is missing or invalid; select and activate an operation policy")
	ErrRollback    = errors.New("local UTC moved backwards more than five minutes; correct the clock and explicitly resolve operation state")
	ErrUpdate      = errors.New("operation state update is unavailable or interrupted; recover the retained update before new work")
	ErrBusy        = errors.New("another operation state update is in progress or retained; retry or recover the retained update")
)

// Instant separates wall UTC from monotonic elapsed time. Production uses
// time.Since; tests may supply independent clock movements at this OS seam.
type Instant struct {
	UTC     time.Time
	Elapsed time.Duration
}
type Clock func() Instant

type Guard struct {
	path      string
	clock     Clock
	mu        sync.Mutex
	anchor    Instant
	effective time.Time
	active    *execution
}
type execution struct{ organization, authority, admissions, instance string }

func localClock() Clock {
	started := time.Now()
	return func() Instant {
		return Instant{UTC: time.Now().UTC().Truncate(time.Second), Elapsed: time.Since(started)}
	}
}

// New remembers an explicit policy without opening it; missing policy never
// makes a viewer fail to start. Admission itself always fails closed.
func New(path string) *Guard { return NewWithClock(path, localClock()) }
func NewWithClock(path string, clock Clock) *Guard {
	if clock == nil {
		clock = localClock()
	}
	at := clock()
	return &Guard{path: path, clock: clock, anchor: at, effective: at.UTC}
}

// Activate explicitly creates new local state against signed UTC dates. It
// never issues a trial, changes signed dates, or repairs an existing state file.
func Activate(path string) error { return ActivateWithClock(path, localClock()) }
func ActivateWithClock(path string, clock Clock) error {
	if clock == nil {
		return ErrUnavailable
	}
	p, grant, err := load(path)
	if err != nil {
		return err
	}
	at := clock().UTC
	if entitlement.ValidateInstant(at) != nil {
		return ErrUnavailable
	}
	if p.Author != "" {
		if err = grant.Assigned(p.Author, p.Device); err != nil {
			return err
		}
	}
	if p.Authority != "" {
		if _, err = grant.Authority(p.Authority); err != nil {
			return err
		}
	}
	if at.Before(grant.Claims.Issued) {
		at = grant.Claims.Issued
	}
	state := State{Schema: StateSchema, Organization: grant.Claims.Organization, Sequence: grant.Claims.Sequence, HighWater: at}
	if _, err = os.Lstat(p.State); !os.IsNotExist(err) {
		return ErrUnavailable
	}
	// Publish the clock last. A failed activation never permits authoring from
	// a clock whose runner companion was not created. Retry may reuse only an
	// empty matching companion retained before that final publish.
	if p.Authority != "" {
		if _, err = entitlement.CreateAdmissions(p.Admissions, grant, p.Authority); err != nil {
			companion, openErr := entitlement.OpenAdmissions(p.Admissions)
			if openErr != nil || companion.Record.Organization != grant.Claims.Organization || companion.Record.Authority != p.Authority || len(companion.Record.Admissions) != 0 {
				return ErrUnavailable
			}
		}
	}
	return create(p.State, state)
}

// Admit author checks the configured named assignment; execute reserves one
// signed runner instance. Call once per bounded job, never once per service.
func (g *Guard) Admit(capability string) (func() error, error) {
	return g.AdmitContext(context.Background(), capability)
}
func (g *Guard) AdmitContext(ctx context.Context, capability string) (func() error, error) {
	switch capability {
	case "author":
		return g.admitWithRetry(ctx, configuredAuthor, "", "")
	case "execute":
		return g.admitWithRetry(ctx, newExecution, "", "")
	case "hub":
		return g.admitWithRetry(ctx, hubOperation, "", "")
	default:
		return nil, entitlement.ErrCapabilityNotGranted
	}
}

// AdmitAuthor binds an already authenticated hub principal and device using
// server-owned mapping. It never accepts an identity from a request body.
func (g *Guard) AdmitAuthor(author, device string) (func() error, error) {
	return g.AdmitAuthorContext(context.Background(), author, device)
}
func (g *Guard) AdmitAuthorContext(ctx context.Context, author, device string) (func() error, error) {
	return g.admitWithRetry(ctx, mappedAuthor, author, device)
}

type admissionMode uint8

const (
	configuredAuthor admissionMode = iota
	mappedAuthor
	newExecution
	existingExecution
	hubOperation
)

func (m admissionMode) capability() string {
	switch m {
	case configuredAuthor, mappedAuthor:
		return "author"
	case newExecution, existingExecution:
		return "execute"
	default:
		return "hub"
	}
}
func (g *Guard) admit(mode admissionMode, author, device string) (func() error, error) {
	capability := mode.capability()
	if g == nil {
		return nil, ErrUnavailable
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	p, grant, err := load(g.path)
	if err != nil {
		return nil, err
	}
	if capability == "execute" {
		if mode == newExecution && g.active != nil {
			return nil, entitlement.ErrInstanceAdmitted
		}
		if mode == existingExecution && (g.active == nil || g.active.organization != grant.Claims.Organization || g.active.authority != p.Authority || g.active.admissions != p.Admissions) {
			return nil, ErrUnavailable
		}
		if _, err = grant.Authority(p.Authority); err != nil {
			return nil, err
		}
	}
	if mode == mappedAuthor {
		if err = grant.Allows("hub", grant.Claims.NotBefore); err != nil {
			return nil, err
		}
	}
	if capability == "author" {
		if mode == configuredAuthor {
			author, device = p.Author, p.Device
		}
		if err = grant.Assigned(author, device); err != nil {
			return nil, err
		}
	}
	sample := g.clock()
	if entitlement.ValidateInstant(g.anchor.UTC) != nil || g.anchor.Elapsed < 0 || entitlement.ValidateInstant(sample.UTC) != nil || sample.Elapsed < g.anchor.Elapsed {
		return nil, ErrUnavailable
	}
	candidate := g.effective.Add(sample.Elapsed - g.anchor.Elapsed)
	if candidate.Before(sample.UTC) {
		candidate = sample.UTC
	}
	var refusal error
	state, err := update(p.State, func(s *State) error {
		if s.Organization != grant.Claims.Organization {
			return entitlement.ErrDifferentOrganization
		}
		if s.Sequence > grant.Claims.Sequence {
			return entitlement.ErrSuperseded
		}
		if s.Released {
			return entitlement.ErrReleased
		}
		if candidate.Before(s.HighWater) {
			candidate = s.HighWater
		}
		// UTC claims/state are whole seconds; compare tolerance at that same
		// precision while retaining fractional elapsed time in candidate.
		if sample.UTC.Before(candidate.Truncate(time.Second).Add(-5 * time.Minute)) {
			s.Rollback = true
		}
		s.HighWater = candidate.Truncate(time.Second)
		s.Sequence = grant.Claims.Sequence
		if s.Rollback {
			refusal = ErrRollback
		} else {
			refusal = grant.Allows(capability, candidate)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	g.effective = candidate
	g.anchor = sample
	if refusal != nil {
		return nil, refusal
	}
	if capability != "execute" {
		return func() error { return nil }, nil
	}
	admissions, err := entitlement.OpenAdmissions(p.Admissions)
	if err != nil {
		return nil, err
	}
	if _, err = admissions.Capacity(grant, state.HighWater); err != nil {
		return nil, err
	}
	if admissions.Record.Authority != p.Authority {
		return nil, entitlement.ErrAuthorityNotNamed
	}
	if mode == existingExecution {
		for _, held := range admissions.Record.Admissions {
			if held.Instance == g.active.instance && held.StateAt(state.HighWater) == entitlement.AdmissionActive {
				return func() error { return nil }, nil
			}
		}
		return nil, entitlement.ErrInstanceUnknown
	}
	instance := "operation-" + strings.ToLower(rand.Text())
	if err = admissions.Admit(grant, instance, state.HighWater, state.HighWater.Add(entitlement.MaxLease)); err != nil {
		return nil, err
	}
	g.active = &execution{organization: grant.Claims.Organization, authority: p.Authority, admissions: p.Admissions, instance: instance}
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			// Release is settlement, never new paid work. Even rollback/expiry cannot
			// strand a finished instance; retain an instant no earlier than admission.
			at := g.clock().UTC
			if at.Before(state.HighWater) {
				at = state.HighWater
			}
			g.mu.Lock()
			defer g.mu.Unlock()
			releaseErr = retryRecord(context.Background(), func() error { return admissions.Release(instance, at) })
			if releaseErr == nil {
				g.active = nil
			}
		})
		return releaseErr
	}, nil
}

// Resolve clears the rollback latch only after wall UTC reaches the retained
// high-water. It cannot decrease time or silently repair absent/corrupt state.
func Resolve(path string) error { return ResolveWithClock(path, localClock()) }
func ResolveWithClock(path string, clock Clock) error {
	if clock == nil {
		return ErrUnavailable
	}
	p, grant, err := load(path)
	if err != nil {
		return err
	}
	at := clock().UTC
	if entitlement.ValidateInstant(at) != nil {
		return ErrUnavailable
	}
	_, err = update(p.State, func(s *State) error {
		if s.Organization != grant.Claims.Organization {
			return entitlement.ErrDifferentOrganization
		}
		if s.Released {
			return entitlement.ErrReleased
		}
		if at.Before(s.HighWater) {
			return ErrRollback
		}
		s.HighWater = at
		s.Rollback = false
		return nil
	})
	return err
}

// Release deactivates this local operation authority. Evidence remains free;
// settlement callbacks already issued remain usable.
func Release(path string) error {
	data, err := readFile(path)
	if err != nil {
		return err
	}
	p, err := DecodePolicy(data)
	if err != nil {
		return err
	}
	_, err = update(p.State, func(s *State) error { s.Released = true; return nil })
	return err
}

// TermStatus is a read-only report. It does not initialize state, admit work,
// clear rollback, or advance the clock; the next admission makes those decisions.
type TermStatus struct {
	State     entitlement.State
	Starts    time.Time
	Expires   time.Time
	GraceEnds time.Time
	Authors   int
	Runners   int
}

func Describe(path string) (TermStatus, error) {
	p, grant, err := load(path)
	if err != nil {
		return TermStatus{}, err
	}
	state, err := readState(p.State)
	if err != nil {
		return TermStatus{}, err
	}
	if state.Organization != grant.Claims.Organization {
		return TermStatus{}, entitlement.ErrDifferentOrganization
	}
	at := time.Now().UTC()
	if at.Before(state.HighWater) {
		at = state.HighWater
	}
	return TermStatus{State: grant.StateAt(at), Starts: grant.Claims.NotBefore, Expires: grant.Claims.Expires, GraceEnds: grant.Claims.GraceEnds(), Authors: grant.Claims.Authors.Seats, Runners: grant.Claims.Runners.Instances}, nil
}

// CheckExecution rechecks a new job under this process's existing instance
// lease. It neither takes another capacity slot nor extends an already admitted
// job. Changed authority or an ended/reconciled lease refuses further work.
func (g *Guard) CheckExecution() error { return g.CheckExecutionContext(context.Background()) }
func (g *Guard) CheckExecutionContext(ctx context.Context) error {
	_, err := g.admitWithRetry(ctx, existingExecution, "", "")
	return err
}

// Lock contention is not permission to repeat execution. Only metadata operations
// which have not admitted work, or settlement of an already finished instance,
// retry briefly. A retained interrupted lock is never removed automatically.
func (g *Guard) admitWithRetry(ctx context.Context, mode admissionMode, author, device string) (func() error, error) {
	var release func() error
	err := retryRecord(ctx, func() error { var err error; release, err = g.admit(mode, author, device); return err })
	return release, err
}
func retryRecord(ctx context.Context, change func() error) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := change()
		if (!errors.Is(err, ErrBusy) && !errors.Is(err, entitlement.ErrAdmissionUpdate)) || attempt == 100 {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
