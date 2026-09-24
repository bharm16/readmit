package desktop

import (
	"context"
	"time"
)

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

// DeclaredProgramRunningForTest counts one operator-declared program as
// running, exactly as a program an operation runs reports itself, and returns
// the function that reports it ended: a test reads what the privacy status
// says while a program runs under a held name without running one.
func DeclaredProgramRunningForTest(a *App) func() {
	return a.declaredProgramStarted()
}

// ImportProfilePackageWithinForTest is ImportProfilePackage's work under a
// context the test controls, so a cancellation lands before the import
// directory exists or after one of its documents was written rather than
// wherever scheduling puts it. The reader, the import and every refusal are
// the production ones; only the operation slot is not taken.
func ImportProfilePackageWithinForTest(ctx context.Context, request ProfilePackageImportRequest) ProfilePackageResult {
	return importProfilePackage(ctx, request)
}

// ProfileImportOperationForTest is the name a package import holds the slot
// under, which the profile panel's cancel must name.
const ProfileImportOperationForTest = profileImportOperation
