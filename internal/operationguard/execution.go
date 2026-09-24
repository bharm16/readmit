package operationguard

import (
	"context"
	"errors"
)

// Profile is what one operation declares about how it runs, as data every
// entry point reads the same way: the name it is disclosed under, whether a
// person can stop it, and the admission it takes before its work starts. The
// command line reads one from each command's annotations, the desktop facade
// declares one per named operation, and the hub declares its scheduled runs'.
// Only Author and Execution reach the guard; the name and interruptibility
// are the entry point's own to act on.
type Profile struct {
	Name          string
	Interruptible bool
	// Author admits the configured author before anything else.
	Author bool
	// Execution is how the work is admitted to execute.
	Execution Execution
}

// Execution is how an operation's work is admitted to execute.
type Execution uint8

const (
	// NoExecution reaches no destination and executes nothing.
	NoExecution Execution = iota
	// Execute holds one runner instance for the whole operation: admitted
	// before the work starts, bounded by MaxDuration, settled when the work
	// ends, and every job the work queues or starts is rechecked under it.
	Execute
	// ExecuteEachJob admits nothing up front: each job the work starts is
	// admitted, bounded and settled as its own execution. A runner that serves
	// job after job takes it, because one instance never outlives MaxDuration.
	ExecuteEachJob
)

// Prerequisites states the profile's admission in the capability ledger's
// words, so a ledger row and the operation it covers are compared directly.
func (p Profile) Prerequisites() []string {
	var words []string
	if p.Author {
		words = append(words, "author")
	}
	if p.Execution != NoExecution {
		words = append(words, "execute")
	}
	return words
}

// Declined is admission declining an operation: none of its work ran.
// Cancelled marks a cancellation that arrived while admission waited, which
// is the person's cancellation and never a refused license. Execution marks
// the execution admission declining, after any author admission the profile
// also declares was decided.
type Declined struct {
	Cancelled bool
	Execution bool
	Err       error
}

func (d *Declined) Error() string { return d.Err.Error() }
func (d *Declined) Unwrap() error { return d.Err }

func declined(ctx context.Context, execution bool, err error) error {
	return &Declined{Cancelled: errors.Is(ctx.Err(), context.Canceled), Execution: execution, Err: err}
}

// Unsettled is an execution whose instance could not be released after its
// work ran. It overrides whatever the work answered, because the retained
// admission must be reconciled before new work; Result keeps the work's own
// answer beside it, and the two read as errors.Join reads them.
type Unsettled struct {
	Err    error
	Result error
}

func (u *Unsettled) Error() string { return errors.Join(u.Result, u.Err).Error() }
func (u *Unsettled) Unwrap() []error {
	if u.Result == nil {
		return []error{u.Err}
	}
	return []error{u.Result, u.Err}
}

// jobsKey carries, in the work's context, how each job the work starts is
// admitted: the one key a queue's recheck and a runner's job both read.
type jobsKey struct{}

type jobs struct {
	guard *Guard
	// held is set while an execution of this operation holds an instance;
	// each job is then rechecked under it rather than admitted again.
	held bool
}

// Run is the one admitted execution: it takes the admission profile declares,
// runs work, and settles. An admission refusal is a *Declined and the work
// never ran; a settlement failure is an *Unsettled; otherwise Run answers what
// the work answered. Evidence reads declare no admission and reach no guard.
func (g *Guard) Run(ctx context.Context, profile Profile, work func(context.Context) error) error {
	if profile.Author {
		if _, err := g.AdmitContext(ctx, "author"); err != nil {
			return declined(ctx, false, err)
		}
	}
	switch profile.Execution {
	case Execute:
		return g.execute(ctx, work)
	case ExecuteEachJob:
		return work(context.WithValue(ctx, jobsKey{}, jobs{guard: g}))
	}
	return work(ctx)
}

func (g *Guard) execute(ctx context.Context, work func(context.Context) error) error {
	settle, err := g.AdmitContext(ctx, "execute")
	if err != nil {
		return declined(ctx, true, err)
	}
	bounded, stop := context.WithTimeout(ctx, MaxDuration)
	defer stop()
	result := work(context.WithValue(bounded, jobsKey{}, jobs{guard: g, held: true}))
	if err := settle(); err != nil {
		return &Unsettled{Err: err, Result: result}
	}
	return result
}

// RunJob runs one job a runner started under the admission its context
// carries. Under an execution that already holds an instance the job is
// rechecked there; otherwise the job is admitted, bounded and settled as its
// own execution through the operation's guard. A context that carries no
// admission refuses the job.
func RunJob(ctx context.Context, work func(context.Context) error) error {
	carried, _ := ctx.Value(jobsKey{}).(jobs)
	if carried.held {
		if err := carried.guard.CheckExecutionContext(ctx); err != nil {
			return declined(ctx, true, err)
		}
		return work(ctx)
	}
	return carried.guard.execute(ctx, work)
}

// Recheck is a queue's check before each new job. It neither takes another
// instance nor extends the one held: a term, clock or authority that no
// longer admits execution refuses the job. A queue whose context carries no
// admission at all is a pure engine caller and is not rechecked; one whose
// jobs are each admitted on their own holds nothing to recheck and is refused.
func Recheck(ctx context.Context) error {
	carried, found := ctx.Value(jobsKey{}).(jobs)
	if !found {
		return nil
	}
	if !carried.held {
		return ErrUnavailable
	}
	return carried.guard.CheckExecutionContext(ctx)
}
