package desktop

import (
	"context"
	"errors"

	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/secret"
)

// refused is implemented by every operation result through a pointer receiver,
// so the one admission runner below can fill in the single state-plus-reason
// refusal each result carries. The states are the same six for every operation;
// only the result's own payload differs.
type refused interface {
	refuse(state State, reason string)
}

// refusedExecution is implemented by a result that says more than its state
// and reason when its execution admission declines it: a capture reports the
// phase it ended in. Every other refusal is refuse's.
type refusedExecution interface {
	refuseExecution(state State, reason string)
}

// settlementFailed is the one reason an execution whose admission could not
// be settled gives, whatever it did before.
const settlementFailed = "runner settlement failed; reconcile the retained admission before new work"

// run is the admission pipeline for local work: it reserves the operation
// slot, refuses with Busy when another operation holds it, admits the author
// when the operation writes, and hands the work a context it can be cancelled
// through. The slot is always released, including after a failure or a
// cancellation, and the work itself runs once. interruptible selects the
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
	return operate[R, PR](a, operationguard.Profile{Interruptible: interruptible, Author: writes}, nil, work)
}

// runNamed is run for an operation that names itself, under the profile the
// operation declares in profiles: its name, whether it is interruptible, and
// the admission it takes. The name does two things while the operation holds
// the slot: a panel can cancel exactly the interruptible operation it started
// and nothing else, and DisclosureStatus reports the disclosed activity the
// name belongs to as active.
//
// Every operation that can reach a network destination or change a target —
// a connectivity check, a fixture reset, a send-policy evaluation that
// resolves names, a source diagnosis or collection, a capture, a send, a
// practice run, a disclosure review or derived export whose proof sends to
// its own loopback receivers, a synthetic packet's generation, which sends to
// the built-in receivers it starts on loopback, an observation, a reduction,
// runner enrollment and execution, and every hub request and step of a
// sign-in — starts here, under a name disclosure.go maps to its row, whether
// or not it is interruptible. Only local work may be unnamed, so a profile
// without a name is refused rather than run unattributed.
//
// An execution is admitted, bounded, rechecked before each job it queues and
// settled by the operation guard (operationguard.Guard.Run), exactly as the
// command line admits the same operation.
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
}](a *App, profile operationguard.Profile, work func(context.Context) R) R {
	return runNamedChecked[R, PR](a, profile, nil, work)
}

// runNamedChecked is runNamed for an operation that refuses a request it can
// judge on its own — a send nobody approved — while it holds the slot and
// before admission is asked: check answers that refusal, or proceeds.
func runNamedChecked[R any, PR interface {
	*R
	refused
}](a *App, profile operationguard.Profile, check func() (R, bool), work func(context.Context) R) R {
	if profile.Name == "" {
		var undeclared R
		PR(&undeclared).refuse(Failed, "this operation declares no profile to run under")
		return undeclared
	}
	return operate[R, PR](a, profile, check, work)
}

// operate holds the slot under profile, answers check's refusal if it has
// one, takes the admission the profile declares, runs the work and answers
// with the work's result or the one refusal admission or settlement gave. A
// cancellation that arrives while admission waits — it retries while another
// update of the operation clock is retained — is the person's cancellation:
// nothing was refused, so it never reads as a denied license. A settlement
// failure overrides whatever the work answered.
func operate[R any, PR interface {
	*R
	refused
}](a *App, profile operationguard.Profile, check func() (R, bool), work func(context.Context) R) R {
	var release func()
	var claimed bool
	ctx := context.Background()
	if profile.Interruptible {
		ctx, release, claimed = a.begin(profile.Name)
	} else {
		release, claimed = a.claim(profile.Name)
	}
	if !claimed {
		var busy R
		PR(&busy).refuse(Busy, busyRefusal.reason)
		return busy
	}
	defer release()
	if check != nil {
		if refusal, proceed := check(); !proceed {
			return refusal
		}
	}
	ctx = secret.ObserveDeclaredPrograms(ctx, a.declaredProgramStarted)
	guard, _ := a.selectedOperation()
	var out R
	err := guard.Run(ctx, profile, func(ctx context.Context) error {
		out = work(ctx)
		return nil
	})
	var declined *operationguard.Declined
	var unsettled *operationguard.Unsettled
	switch {
	case errors.As(err, &declined):
		var denied R
		state, reason := PermissionDenied, declined.Error()
		if declined.Cancelled {
			state, reason = cancelledRefusal.state, cancelledRefusal.reason
		}
		if execution, ok := any(&denied).(refusedExecution); ok && declined.Execution {
			execution.refuseExecution(state, reason)
		} else {
			PR(&denied).refuse(state, reason)
		}
		return denied
	case errors.As(err, &unsettled):
		PR(&out).refuse(Failed, settlementFailed)
	}
	return out
}
