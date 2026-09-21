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
// seat it does not use.
func run[R any, PR interface {
	*R
	refused
}](a *App, interruptible, writes bool, work func(context.Context) R) R {
	var release func()
	var claimed bool
	ctx := context.Background()
	if interruptible {
		ctx, release, claimed = a.begin()
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
		if err := a.admitAuthor(); err != nil {
			var denied R
			PR(&denied).refuse(PermissionDenied, err.Error())
			return denied
		}
	}
	return work(ctx)
}
