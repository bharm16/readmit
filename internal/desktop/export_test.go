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
