import { findCaseRow } from "./testkit/navigation";
// Context-sensitive recovery at the entry surfaces: a refused workspace open
// and a refused case verification each offer the action that actually repairs
// them, a run review's refusal opens the configuration the
// refusal is about, and no supported journey dead-ends in a CLI instruction.
import { expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { goTo, page, sidebar } from "./testkit/navigation";
import { CASE_ENTRY, catalogOfListing, folderChosen, folderWithCase, refused, WORKSPACE_ROOT } from "./testkit/fixtures";
import { facadeStub } from "./testkit/wails";
import type { Artifact, CatalogItem, RunRefusal } from "./bindings";

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

test("a run review refusal opens the configuration the refusal is about", async () => {
  const user = userEvent.setup();
  const saved: CatalogItem = {
    ref: { kind: "test", id: "t-reschedule", revision: "2" },
    name: "Reschedule keeps one appointment",
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { test: { source_case: null, latest_run: null, assertions: 1, current_version: "2" } },
  };
  let refusal: RunRefusal = "environment";
  await renderApp({
    SelectWorkspace: () => folderWithSpec(),
    ListCatalog: (query) =>
      query.kind === "test" ? { state: "completed", context: query.context, page: { items: [saved], total: 1, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, facadeStub()),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: {
        action: "run.test", consent: "send", items: [saved], destination: {}, requirements: [], ready: false,
        refusal: refusal === "environment" ? "the send policy refuses this destination" : "the operation term has expired",
        run: {
          kind: "test", name: saved.name, version: "2", environment: { kind: "environment", id: "env-qa" }, environment_name: "Scheduling QA", messages: [], message_count: 1,
          setup: [], resets: [], jobs: [], targets: [], environments: [], refusal,
        },
      },
    }),
  });
  await user.click(page().getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  const review = async () => {
    await goTo(user, "Runs");
    await user.click(await page().findByRole("button", { name: "Run test" }));
    const picker = await screen.findByRole("dialog", { name: "Run test" });
    await user.click(await within(picker).findByRole("radio", { name: "Reschedule keeps one appointment · v2" }));
    await user.click(within(picker).getByRole("button", { name: "Continue" }));
    return screen.findByRole("alert");
  };
  expect((await review()).textContent).toBe("the send policy refuses this destination");
  // Each refusal's next action is the real configuration screen.
  await user.click(screen.getByRole("button", { name: "Edit environment" }));
  expect(sidebar().getByRole("button", { name: "Environments" }).getAttribute("aria-current")).toBe("page");
  expect(document.activeElement?.classList.contains("region-evidence")).toBe(true);
  refusal = "license";
  expect((await review()).textContent).toBe("the operation term has expired");
  await user.click(screen.getByRole("button", { name: "Activate" }));
  expect(page().getByRole("heading", { level: 1, name: "Settings" })).toBeTruthy();
  expect(page().getByRole("button", { name: "License" }).getAttribute("aria-current")).toBe("page");
});

test("no supported journey ends in a CLI instruction", async () => {
  await renderApp();
  const source = document.body.textContent ?? "";
  expect(source).not.toMatch(/readmit (project revise|diagnose review|run|replay)\b/);
});
