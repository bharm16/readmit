// The first-run choices: create or open a real project with your own
// evidence, or explore the free guided sample — with the license and setup
// states stated as the facade holds them, and the sample named as practice
// rather than as a substitute for own-evidence onboarding.
import { expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { folderWithCase, guideResult, WORKSPACE_ROOT } from "./testkit/fixtures";

function firstRunRegion(): HTMLElement {
  // The commands region hosts the first-run block while no workspace is open.
  return screen.getByRole("region", { name: "Commands" });
}

test("a window with no workspace open presents own-evidence and sample as clear choices", async () => {
  const { facade } = await renderApp();
  const region = within(firstRunRegion());
  expect(region.getByRole("heading", { name: "Start here" })).toBeTruthy();
  expect(
    region.getByRole("button", { name: "Choose a folder for a new project…" }),
  ).toBeTruthy();
  expect(region.getByRole("button", { name: "Open an existing workspace…" })).toBeTruthy();
  expect(
    region.getByRole("button", { name: "Explore the guided sample…" }),
  ).toBeTruthy();
  // The sample is named as practice, never as own-evidence onboarding.
  expect(region.getByText(/never a substitute for your own evidence/i)).toBeTruthy();
  // Both first-run choices go through the real workspace-open call.
  expect(facade.callsTo("SelectWorkspace").length).toBe(0);
  expect(facade.callsTo("CreateSampleWorkspace").length).toBe(0);
});

test("choosing a real project opens a chosen folder and reaches the project screens", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenProjectOverview: () => {
      throw new Error("not arranged");
    },
  });
  const region = within(firstRunRegion());
  await user.click(
    region.getByRole("button", { name: "Choose a folder for a new project…" }),
  );
  expect(facade.oneCall("SelectWorkspace")).toEqual([]);
  // The open workspace is announced in the navigation region, where the
  // project and import screens are reached from.
  expect(screen.getByText(WORKSPACE_ROOT)).toBeTruthy();
});

test("choosing the guided sample creates the real sample workspace", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    CreateSampleWorkspace: () => ({
      state: "completed",
      workspace: { root: WORKSPACE_ROOT, artifacts: [] },
    }),
    Guide: () => guideResult("sample", 0),
  });
  const region = within(firstRunRegion());
  await user.click(
    region.getByRole("button", { name: "Explore the guided sample…" }),
  );
  expect(facade.oneCall("CreateSampleWorkspace")).toEqual([]);
  expect(screen.getByText(WORKSPACE_ROOT)).toBeTruthy();
});

test("the license state is stated with its own next action", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    OperationStatus: () => ({ state: "empty", selected: false, author_seats: 0, runner_instances: 0 }),
  });
  const region = within(firstRunRegion());
  expect(await region.findByText(/none activated on this machine/i)).toBeTruthy();
  await user.click(region.getByRole("button", { name: "License and activation…" }));
  // The action opens the real pane where activation lives.
  expect(document.activeElement?.classList.contains("region-privacy")).toBe(true);
  expect(facade.callsTo("ActivateOperations").length).toBe(0);
});

test("an activated license is stated as active, not as a missing prerequisite", async () => {
  await renderApp({
    OperationStatus: () => ({
      state: "completed",
      selected: true,
      term: "trial",
      organization: "Hospital QI",
      expires: "2026-10-01T00:00:00Z",
      author_seats: 2,
      runner_instances: 1,
      clock: {
        schema: "readmit-operation-clock/v1",
        organization: "Hospital QI",
        sequence: 1,
        high_water: "2026-09-21T00:00:00Z",
        rollback: false,
        released: false,
      },
    }),
  });
  const region = within(firstRunRegion());
  expect(await region.findByText(/active until/i)).toBeTruthy();
  expect(region.queryByText(/none activated on this machine/i)).toBeNull();
});
