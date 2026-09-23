// React's development build records an owner stack — a captured Error, and a
// console task where the console offers one — for each element it creates
// while fewer than 10,000 elements have been created since it last reset that
// count, which it does when a render starts more than a second after the
// previous reset. The same test therefore does more work the slower the
// machine runs it: a loaded machine spreads it over more resets, nearly every
// element records a stack, and rendering the whole window costs about twice
// as much. Owner stacks only feed React's development diagnostics — the stack
// appended to its console warnings and captureOwnerStack() — which no test
// reads, and a production build records none. Holding the count spent sends
// every element down the fallback React already takes once the budget runs
// out, so a test does the same work under any load.

type ReactModule = {
  __CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE?: object;
};

/** Holds React's owner-stack budget spent for the rest of the run. A React
 * that no longer keeps the count where this reads it is refused, so an upgrade
 * that moves it fails the setup instead of quietly bringing the cost back. */
export function holdOwnerStackBudgetSpent(react: object): void {
  const internals = (react as ReactModule)
    .__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE;
  if (!internals || !("recentlyCreatedOwnerStacks" in internals)) {
    throw new Error(
      "React no longer keeps its owner-stack count where the test setup holds it spent",
    );
  }
  // Configurable, so a setup that runs again over the same loaded React
  // replaces this instead of throwing.
  Object.defineProperty(internals, "recentlyCreatedOwnerStacks", {
    configurable: true,
    get: () => Number.POSITIVE_INFINITY,
    set: () => {},
  });
}
