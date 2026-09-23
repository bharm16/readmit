package desktop

import "context"

// refused is implemented by every operation result through a pointer receiver,
// so the one admission runner below can fill in the single state-plus-reason
// refusal each result carries. The states are the same six for every operation;
// only the result's own payload differs.
type refused interface {
	refuse(state State, reason string)
}

// run is the one admission pipeline behind a facade operation: it reserves the
// operation slot, refuses with Busy when another operation holds it, admits the
// author when the operation writes, and hands the work a context it can be
// cancelled through. The slot is always released, including after a failure or
// a cancellation, and the work itself runs once. interruptible selects the
// cancellable variant of the slot, the one Cancel can stop; writes selects
// author admission, so an operation that changes nothing cannot be held to a
// seat it does not use. An interruptible operation started this way is
// unnamed: only the window's own cancel command can stop it, which is why a
// panel with its own cancel control starts its operation through runNamed.
func run[R any, PR interface {
	*R
	refused
}](a *App, interruptible, writes bool, work func(context.Context) R) R {
	return runNamed[R, PR](a, "", interruptible, writes, work)
}

// runNamed is run for an interruptible operation that names itself, so a panel
// can cancel exactly the operation it started and nothing else. The name is
// empty for work that is not interruptible or names no panel of its own.
func runNamed[R any, PR interface {
	*R
	refused
}](a *App, operation string, interruptible, writes bool, work func(context.Context) R) R {
	var release func()
	var claimed bool
	ctx := context.Background()
	if interruptible {
		ctx, release, claimed = a.begin(operation)
	} else {
		release, claimed = a.claim()
	}
	if !claimed {
		var refused R
		PR(&refused).refuse(Busy, busyRefusal.reason)
		return refused
	}
	defer release()
	if writes {
		if err := a.admitAuthorContext(ctx); err != nil {
			declined := admissionRefusal(ctx, err)
			var denied R
			PR(&denied).refuse(declined.state, declined.reason)
			return denied
		}
	}
	return work(ctx)
}

// admissionRefusal is how an operation answers when author or execution
// admission did not admit it. Admission can wait — it retries while another
// update of the operation clock is retained — and a cancellation that arrives
// meanwhile is the person's cancellation: nothing was refused, so it never
// reads as a denied license.
func admissionRefusal(ctx context.Context, err error) refusal {
	if ctx.Err() != nil {
		return cancelledRefusal
	}
	return refusal{PermissionDenied, err.Error()}
}
