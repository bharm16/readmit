package desktop

import (
	"context"

	"github.com/bharm16/readmit/internal/secret"
)

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
// seat it does not use. An operation started this way is unnamed: only the
// window's own cancel command can stop it, and the privacy status cannot
// attribute it to any disclosed activity. It is for local work alone; see
// runNamed for what must be named.
func run[R any, PR interface {
	*R
	refused
}](a *App, interruptible, writes bool, work func(context.Context) R) R {
	return runNamed[R, PR](a, "", interruptible, writes, work)
}

// runNamed is run for an operation that names itself. The name does two
// things while the operation holds the slot: a panel can cancel exactly the
// interruptible operation it started and nothing else, and DisclosureStatus
// reports the disclosed activity the name belongs to as active.
//
// Every operation that can reach a network destination or change a target —
// a connectivity check, a fixture reset, a send-policy evaluation that
// resolves names, a source diagnosis or collection, a capture, a send, a
// practice run, a disclosure review or derived export whose proof sends to
// its own loopback receivers, a synthetic packet's generation, which sends to
// the built-in receivers it starts on loopback, an observation, a reduction,
// runner enrollment and execution, and every hub request and step of a
// sign-in — starts here, under a name disclosure.go maps to its row, whether
// or not it is interruptible. Only local work may be unnamed.
//
// The work's context also carries the observer of operator-declared programs
// (secret.ObserveDeclaredPrograms), so every program the work runs — a
// credential reference's locator, a hub, runner or protection key command, a
// source's transfer program — reports itself while it runs, and the privacy
// status's declared-program row is active exactly then. Every operation that
// can run one — testing, rotating or scanning credential references
// included — is named, and disclosure.go says what each name's program is.
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
		release, claimed = a.claim(operation)
	}
	if !claimed {
		var refused R
		PR(&refused).refuse(Busy, busyRefusal.reason)
		return refused
	}
	defer release()
	ctx = secret.ObserveDeclaredPrograms(ctx, a.declaredProgramStarted)
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
