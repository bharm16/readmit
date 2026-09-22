// Context-sensitive recovery at the entry surfaces: a refused workspace open
// and a refused case verification each offer the action that actually repairs
// them, a preflight refusal on the run panel opens the configuration the
// refusal is about, and no supported journey dead-ends in a CLI instruction.
import { expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { folderChosen, folderWithCase, refused, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { Artifact } from "./bindings";

const SAVED_SPEC = "saved-spec.json";

const folderWithSpec = (): ReturnType<typeof folderChosen> =>
  folderChosen(WORKSPACE_ROOT, [
    { name: SAVED_SPEC, kind: "spec" },
  ] as Artifact[]);

test("a refused workspace open offers choosing a different folder as its next action", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => refused("this account cannot read that folder"),
  });
  await user.click(screen.getByRole("button", { name: "Open an existing workspace…" }));
  expect(screen.getByText(/this account cannot read that folder/i)).toBeTruthy();
  // The repair action is the real dialog again, not advice to read a manual.
  facade.reply({ SelectWorkspace: () => folderWithCase() });
  await user.click(screen.getByRole("button", { name: "Choose a different folder…" }));
  expect(facade.callsTo("SelectWorkspace").length).toBe(2);
  expect(screen.getByText(WORKSPACE_ROOT)).toBeTruthy();
});

test("a refused case verification offers the way back to the folder listing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => refused("the case cannot be verified"),
  });
  await user.click(screen.getByRole("button", { name: "Open an existing workspace…" }));
  await screen.findByText(WORKSPACE_ROOT);
  await user.click(await screen.findByRole("button", { name: "Verify and open" }));
  expect(screen.getByText(/the case cannot be verified/i)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Back to the folder listing" }));
  expect(document.activeElement?.classList.contains("region-navigation")).toBe(true);
  expect(facade.callsTo("OpenCase").length).toBe(1);
});

test("a run preflight refusal opens the configuration the refusal is about", async () => {
  const user = userEvent.setup();
  await renderApp({
    SelectWorkspace: () => folderWithSpec(),
    PreflightRun: () => refused("the send policy refuses this destination"),
  });
  await user.click(screen.getByRole("button", { name: "Open an existing workspace…" }));
  await screen.findByText(WORKSPACE_ROOT);
  const evidence = within(screen.getByRole("region", { name: "Evidence" }));
  await user.selectOptions(evidence.getByLabelText("Saved test or suite"), SAVED_SPEC);
  await user.click(evidence.getByRole("button", { name: "Validate and preflight" }));
  expect(await screen.findByText(/the send policy refuses this destination/i)).toBeTruthy();
  // Each refusal's next action is the real configuration screen.
  await user.click(
    screen.getByRole("button", { name: "Configure environments and targets…" }),
  );
  expect(document.activeElement?.classList.contains("region-inspector")).toBe(true);
  await user.click(screen.getByRole("button", { name: "Open license and activation…" }));
  expect(document.activeElement?.classList.contains("region-privacy")).toBe(true);
});

test("no supported journey ends in a CLI instruction", async () => {
  await renderApp();
  const source = document.querySelector("main")?.textContent ?? "";
  expect(source).not.toMatch(/readmit (project revise|diagnose review|run|replay)\b/);
});
