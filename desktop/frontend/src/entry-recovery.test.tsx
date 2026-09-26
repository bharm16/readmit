import { findCaseRow } from "./testkit/navigation";
// Context-sensitive recovery at the entry surfaces: a refused workspace open
// and a refused case verification each offer the action that actually repairs
// them, a preflight refusal on the run panel opens the configuration the
// refusal is about, and no supported journey dead-ends in a CLI instruction.
import { expect, test } from "vitest";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { goToView, page, sidebar } from "./testkit/navigation";
import { CASE_ENTRY, folderChosen, folderWithCase, refused, WORKSPACE_ROOT } from "./testkit/fixtures";
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
  await user.click(page().getByRole("button", { name: "Open" }));
  expect(page().getByText(/this account cannot read that folder/i)).toBeTruthy();
  // The repair action is the real dialog again, not advice to read a manual.
  facade.reply({ SelectWorkspace: () => folderWithCase() });
  await user.click(page().getByRole("button", { name: "Choose another folder…" }));
  expect(facade.callsTo("SelectWorkspace").length).toBe(2);
  expect(await sidebar().findByRole("button", { name: /^Project: / })).toBeTruthy();
});

test("a refused case verification offers the way back to the folder listing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => refused("the case cannot be verified"),
  });
  await user.click(page().getByRole("button", { name: "Open" }));
  await user.click(await findCaseRow(CASE_ENTRY));
  expect(await page().findByText(/the case cannot be verified/i)).toBeTruthy();
  // The refusal is shown on the case list itself, which stays usable: the way
  // back is already on screen.
  expect(await findCaseRow(CASE_ENTRY)).toBeTruthy();
  expect(sidebar().getByRole("button", { name: "Cases" }).getAttribute("aria-current")).toBe("page");
  expect(facade.callsTo("OpenCase").length).toBe(1);
});

test("a run preflight refusal opens the configuration the refusal is about", async () => {
  const user = userEvent.setup();
  await renderApp({
    SelectWorkspace: () => folderWithSpec(),
    PreflightRun: () => refused("the send policy refuses this destination"),
  });
  await user.click(page().getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  await goToView(user, "Runs", "Run test");
  await user.selectOptions(page().getByLabelText("Saved test or suite"), SAVED_SPEC);
  await user.click(page().getByRole("button", { name: "Preview run" }));
  expect(await page().findByText(/the send policy refuses this destination/i)).toBeTruthy();
  // Each refusal's next action is the real configuration screen.
  await user.click(page().getByRole("button", { name: "Environments" }));
  expect(page().getByRole("heading", { level: 1, name: "Environments" })).toBeTruthy();
  expect(document.activeElement?.classList.contains("region-evidence")).toBe(true);
  await goToView(user, "Runs", "Run test");
  await user.click(page().getByRole("button", { name: "License" }));
  expect(page().getByRole("heading", { level: 1, name: "Settings" })).toBeTruthy();
  expect(page().getByRole("button", { name: "License" }).getAttribute("aria-current")).toBe("page");
});

test("no supported journey ends in a CLI instruction", async () => {
  await renderApp();
  const source = document.body.textContent ?? "";
  expect(source).not.toMatch(/readmit (project revise|diagnose review|run|replay)\b/);
});
