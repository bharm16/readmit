package desktop

import "time"

// CompleteHubAuthWithinForTest is CompleteHubAuth with a shorter wait for the
// browser, so a test reaches the sign-in timeout without waiting minutes. The
// slot, the listener and every refusal are the production ones. It is a
// function rather than a method so the bound facade stays exactly what the
// application binds.
func CompleteHubAuthWithinForTest(a *App, code, state string, wait time.Duration) HubResult {
	return a.completeHubAuth(code, state, wait)
}

// HoldSlotForTest claims the operation slot under name, exactly as runNamed
// claims it for an operation that is not interruptible, and returns its
// release: a test reads what the privacy status reports for an operation of
// that name without starting the operation's work.
func HoldSlotForTest(a *App, name string) (func(), bool) {
	return a.claim(name)
}
