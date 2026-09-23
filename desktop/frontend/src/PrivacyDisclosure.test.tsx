// The privacy region's per-operation disclosure: every deliberately
// configurable activity is shown with its destination, data category,
// authorization and its live connected/offline state, and each row's next
// action opens the real screen where that activity is configured or run.
import { expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { disclosureStatusResult, shellResult } from "./testkit/fixtures";

function privacyRegion() {
  return within(screen.getByRole("region", { name: "Privacy" }));
}

test("every disclosed operation is drawn with destination, data, authorization and live state", async () => {
  await renderApp();
  const table = screen.getByRole("table", { name: /deliberately configured activities/i });
  const rows = within(table).getAllByRole("row");
  // A header row plus one per disclosed operation.
  expect(rows.length).toBe(1 + (shellResult().shell?.privacy.operations.length ?? 0));
  const run = within(table).getByRole("row", { name: /Durable test execution/ });
  expect(within(run).getByText(/approved send policy/i)).toBeTruthy();
  expect(await within(run).findByText(/Idle/i)).toBeTruthy();
  const hub = within(table).getByRole("row", { name: /Customer artifact hub/ });
  expect(await within(hub).findByText(/Not configured/i)).toBeTruthy();
});

test("the live states come from the facade and refresh deliberately", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    DisclosureStatus: () =>
      disclosureStatusResult([
        { id: "run", state: "active", detail: "A durable run is executing now." },
        { id: "runner", state: "idle", detail: "No recurring execution is in progress." },
        { id: "capture", state: "idle", detail: "No capture or collection is in progress." },
        { id: "observe", state: "idle", detail: "No observation window is open." },
        { id: "hub", state: "offline", detail: "A hub configuration is selected; not connected." },
        { id: "portal", state: "configured", detail: "A portal destination is configured." },
      ]),
  });
  const initialCalls = facade.callsTo("DisclosureStatus").length;
  expect(initialCalls).toBeGreaterThanOrEqual(1);
  const table = screen.getByRole("table", { name: /deliberately configured activities/i });
  const run = within(table).getByRole("row", { name: /Durable test execution/ });
  expect(await within(run).findByText(/Active now/i)).toBeTruthy();
  const hub = within(table).getByRole("row", { name: /Customer artifact hub/ });
  expect(await within(hub).findByText(/Configured, offline/i)).toBeTruthy();
  await user.click(privacyRegion().getByRole("button", { name: "Refresh the states" }));
  expect(facade.callsTo("DisclosureStatus").length).toBeGreaterThan(initialCalls);
});

test("a refused state read is shown instead of a stale table answer", async () => {
  const { facade } = await renderApp({
    DisclosureStatus: () => ({ state: "busy", reason: "another operation holds the slot" }),
  });
  const privacy = privacyRegion();
  // A busy read is asked again for about a second before busy is shown.
  expect(
    (await privacy.findAllByText(/another operation holds the slot/i, {}, { timeout: 4000 })).length,
  ).toBeGreaterThan(0);
  expect(facade.callsTo("DisclosureStatus").length).toBeGreaterThanOrEqual(20);
});

test("each activity's next action opens the screen where that activity lives", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => ({
      state: "completed",
      workspace: { root: "/workspace-under-test", artifacts: [] },
    }),
  });
  // The workspace is open first: capture and observation setup live behind
  // an open workspace, exactly as the disclosure says.
  await user.click(screen.getByRole("button", { name: "Open an existing workspace…" }));
  await screen.findByText("/workspace-under-test");
  const table = screen.getByRole("table", { name: /deliberately configured activities/i });
  const run = within(table).getByRole("row", { name: /Durable test execution/ });
  await user.click(within(run).getByRole("button", { name: /Open the run panel/i }));
  expect(document.activeElement?.classList.contains("region-evidence")).toBe(true);
  // Capture setup is a real screen: with a workspace open it opens directly.
  const capture = within(table).getByRole("row", { name: /Capture and source collection/ });
  await user.click(within(capture).getByRole("button", { name: /Set up capture/i }));
  expect(
    screen.getByRole("region", { name: "Evidence" }).textContent,
  ).toMatch(/Capture and collect/i);
  expect(facade.callsTo("StartCapture").length).toBe(0);
});

test("the support guidance names the ledger rows still open and the qualification refusals", async () => {
  await renderApp();
  const privacy = privacyRegion();
  expect(privacy.getByRole("heading", { name: "What this build supports" })).toBeTruthy();
  expect(privacy.getByText(/generate and stream a declared performance corpus/i)).toBeTruthy();
  expect(privacy.getByText(/declared, not qualified/i)).toBeTruthy();
  expect(privacy.getByText(/selected and unqualified/i)).toBeTruthy();
});
