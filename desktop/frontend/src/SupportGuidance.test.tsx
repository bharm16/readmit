// Help's support guidance: the qualification and certification refusals the
// verified state holds, carried from the facade.
import { expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { goTo } from "./testkit/navigation";

function privacyRegion() {
  return within(screen.getByRole("region", { name: "Main content" }));
}

test("the support guidance names the ledger rows still open and the qualification refusals", async () => {
  await renderApp();
  await goTo(userEvent.setup(), "Help");
  const privacy = privacyRegion();
  expect(privacy.getByRole("heading", { name: "Capabilities" })).toBeTruthy();
  expect(privacy.getByText(/generate reproducible SIU synthetic case bundles from declared inputs/i)).toBeTruthy();
  expect(privacy.getByText(/declared, not qualified/i)).toBeTruthy();
  expect(privacy.getByText(/selected and unqualified/i)).toBeTruthy();
});
