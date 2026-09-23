import { expect, test } from "vitest";
import * as React from "react";
import { holdOwnerStackBudgetSpent } from "./owner-stacks";

type Internals = { recentlyCreatedOwnerStacks: number };
const internals = (
  React as unknown as {
    __CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE: Internals;
  }
).__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE;

test("under the setup no element records an owner stack of its own, however many are created", () => {
  const first = <div />;
  const later = Array.from({ length: 20_000 }, () => <span />).at(-1);
  // A recorded stack is a new Error per element; the fallback is one shared
  // value.
  expect((first as unknown as { _debugStack: unknown })._debugStack).toBe(
    (later as unknown as { _debugStack: unknown })._debugStack,
  );
});

test("the owner-stack count stays spent when React resets it", () => {
  // react-dom writes zero here when a render starts a second after its last
  // reset.
  internals.recentlyCreatedOwnerStacks = 0;
  expect(internals.recentlyCreatedOwnerStacks).toBe(Number.POSITIVE_INFINITY);
});

test("a React that no longer keeps its owner-stack count is refused", () => {
  const refusal = /React no longer keeps its owner-stack count/;
  expect(() => holdOwnerStackBudgetSpent({})).toThrow(refusal);
  expect(() =>
    holdOwnerStackBudgetSpent({ __CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE: {} }),
  ).toThrow(refusal);
});
