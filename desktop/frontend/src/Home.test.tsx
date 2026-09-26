// The home page: the projects this viewer works in and the three ways to
// start — a new project, an existing one, or the demo — with nothing about
// licensing, connections or what the build writes on it.
import { expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { page, sidebar } from "./testkit/navigation";
import { folderWithCase, guideResult, projectOverviewResult, recentResult, WORKSPACE_ROOT } from "./testkit/fixtures";

test("a window with no project open offers new, open and the demo, and nothing else to set up", async () => {
  const { facade } = await renderApp();
  expect(page().getByRole("heading", { level: 1, name: "Projects" })).toBeTruthy();
  expect(page().getByRole("button", { name: "New project" })).toBeTruthy();
  expect(page().getByRole("button", { name: "Open…" })).toBeTruthy();
  expect(page().getByRole("button", { name: "Try demo" })).toBeTruthy();
  // Licensing and connections are Settings, never the first screen.
  expect(page().queryByRole("button", { name: /activate/i })).toBeNull();
  expect(page().queryByRole("table")).toBeNull();
  // The project destinations exist only once a project is open.
  expect(sidebar().queryByRole("button", { name: "Cases" })).toBeNull();
  expect(facade.callsTo("SelectWorkspace").length).toBe(0);
  expect(facade.callsTo("CreateSampleWorkspace").length).toBe(0);
});

test("choosing a real project opens a chosen folder and reaches the project screens", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
  });
  await user.click(page().getByRole("button", { name: "Open…" }));
  expect(facade.oneCall("SelectWorkspace")).toEqual([]);
  // The folder opens on its cases, and the sidebar names it.
  expect(await page().findByRole("heading", { level: 1, name: "workspace-under-test" })).toBeTruthy();
  expect(sidebar().getByTitle(WORKSPACE_ROOT)).toBeTruthy();
  expect(sidebar().getByRole("button", { name: "Cases" }).getAttribute("aria-current")).toBe("page");
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
  await user.click(page().getByRole("button", { name: "Try demo" }));
  expect(facade.oneCall("CreateSampleWorkspace")).toEqual([]);
  expect(await sidebar().findByTitle(WORKSPACE_ROOT)).toBeTruthy();
  expect(sidebar().getByRole("button", { name: "Cases" }).getAttribute("aria-current")).toBe("page");
});

test("a new project asks for its name and versions, then its location, and opens once created", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    CreateProject: (name, title, owner, versions) => {
      expect(name).toBe("scheduling-investigation");
      // An empty title takes the name.
      expect(title).toBe("scheduling-investigation");
      expect(owner).toBe("");
      expect(versions).toEqual(["2.5.1", "2.3"]);
      return projectOverviewResult([]);
    },
    OpenWorkspace: () => ({ state: "completed", workspace: { root: WORKSPACE_ROOT, artifacts: [{ name: "readmit-project.json", kind: "project" }] } }),
    OpenProjectOverview: () => projectOverviewResult([]),
  });
  await user.click(page().getByRole("button", { name: "New project" }));
  const dialog = within(screen.getByRole("dialog", { name: "New project" }));
  const create = dialog.getByRole("button", { name: "Choose location and create…" }) as HTMLButtonElement;
  // Nothing can be created without a name and at least one version.
  expect(create.disabled).toBe(true);
  await user.type(dialog.getByLabelText("Name"), "scheduling-investigation");
  await user.type(dialog.getByLabelText("Interface versions"), "2.5.1, 2.3");
  expect(create.disabled).toBe(false);
  await user.click(create);
  expect(facade.callsTo("CreateProject").length).toBe(1);
  // The created project opens, and the sheet closes.
  expect(await sidebar().findByTitle(WORKSPACE_ROOT)).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "New project" })).toBeNull();
});

test("a refused project creation stays in the sheet beside what was typed", async () => {
  const user = userEvent.setup();
  await renderApp({
    CreateProject: () => ({ state: "cancelled" }),
  });
  await user.click(page().getByRole("button", { name: "New project" }));
  const dialog = within(screen.getByRole("dialog", { name: "New project" }));
  await user.type(dialog.getByLabelText("Name"), "scheduling-investigation");
  await user.type(dialog.getByLabelText("Interface versions"), "2.5.1");
  await user.click(dialog.getByRole("button", { name: "Choose location and create…" }));
  expect(await dialog.findByText("Not created")).toBeTruthy();
  expect((dialog.getByLabelText("Name") as HTMLInputElement).value).toBe("scheduling-investigation");
});

test("recent projects reopen from the home page", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    RecentWorkspaces: () => recentResult([WORKSPACE_ROOT]),
    OpenWorkspace: () => folderWithCase(),
  });
  const list = within(await page().findByRole("list", { name: "Recent projects" }));
  expect(list.getByText("workspace-under-test")).toBeTruthy();
  await user.click(list.getByRole("button", { name: `Open ${WORKSPACE_ROOT}` }));
  expect(facade.oneCall("OpenWorkspace")[0]).toBe(WORKSPACE_ROOT);
  expect(await sidebar().findByTitle(WORKSPACE_ROOT)).toBeTruthy();
});
