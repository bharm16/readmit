// The home page: the projects this viewer works in and the three ways to
// start — a new project, an existing one, or the demo — with nothing about
// licensing, connections or what the build writes on it.
import { expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { page, sidebar } from "./testkit/navigation";
import { editorDraft, folderChosen, folderWithCase, guideResult, projectOverviewResult, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { CatalogItem, CatalogQuery, CatalogResult } from "./bindings";

test("a window with no project open offers new, open and the demo, and nothing else to set up", async () => {
  const { facade } = await renderApp();
  expect(page().getByRole("heading", { level: 1, name: "Projects" })).toBeTruthy();
  // With no projects yet, the list is its title and New project.
  expect(await page().findByText("No projects yet")).toBeTruthy();
  expect(page().getAllByRole("button", { name: "New project" })).toHaveLength(2);
  expect(page().getByRole("button", { name: "Open" })).toBeTruthy();
  expect(page().getByRole("button", { name: "Try demo" })).toBeTruthy();
  // Licensing and connections are Settings, never the first screen.
  expect(page().queryByRole("button", { name: /activate/i })).toBeNull();
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
  await user.click(page().getByRole("button", { name: "Open" }));
  expect(facade.oneCall("SelectWorkspace")).toEqual([]);
  // The folder opens on its cases, and the sidebar names it.
  expect(await page().findByRole("heading", { level: 1, name: "Cases" })).toBeTruthy();
  expect(sidebar().getByRole("button", { name: "Project: workspace-under-test" })).toBeTruthy();
  expect(sidebar().getByRole("button", { name: /^Project: / })).toBeTruthy();
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
  expect(await sidebar().findByRole("button", { name: /^Project: / })).toBeTruthy();
  expect(sidebar().getByRole("button", { name: "Cases" }).getAttribute("aria-current")).toBe("page");
});

const noContext = { project: "", generation: 0 };

/** A project as the catalog lists it among this viewer's projects. */
function listedProject(id: string, name: string, folder: string, opened: string | null, availability: CatalogItem["availability"] = "available"): CatalogItem {
  return {
    ref: { kind: "project", id },
    name,
    created_at: null,
    updated_at: null,
    last_opened_at: opened,
    availability,
    ...(availability === "available" ? {} : { reason: "The project folder is not where it was." }),
    capabilities: [],
    summary: { project: { folder, schema: "readmit-project/v2", cases: 0, interface_versions: [], tags: [], revisions: [] } },
  };
}

function projectsCatalog(items: CatalogItem[]) {
  return (query: CatalogQuery): CatalogResult =>
    query.kind === "project"
      ? { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } }
      : { state: "completed", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } };
}

test("a new project asks only for a name, goes into the remembered location, and opens on its empty Cases", async () => {
  const user = userEvent.setup();
  const created = `${WORKSPACE_ROOT}/Scheduling investigation`;
  const { facade } = await renderApp({
    ProjectLocation: () => ({ state: "completed", location: WORKSPACE_ROOT }),
    CreateNamedProject: (request) => ({
      state: "completed",
      context: noContext,
      recorded: true,
      project: listedProject("p1", request.name, created, null),
    }),
    OpenWorkspace: () => folderChosen(created, [{ name: "project.json", kind: "project" }]),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: noContext, recorded: true }),
  });
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  expect(sheet.getAllByRole("textbox")).toHaveLength(1);
  expect(await sheet.findByText(WORKSPACE_ROOT)).toBeTruthy();
  await user.type(sheet.getByLabelText("Name"), "  Scheduling investigation ");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  expect(facade.oneCall("CreateNamedProject")).toEqual([{ name: "Scheduling investigation" }]);
  // No folder picker opens after Create.
  expect(facade.callsTo("ChooseProjectLocation")).toHaveLength(0);
  expect(await page().findByRole("heading", { level: 1, name: "Cases" })).toBeTruthy();
  expect(page().getByText("No cases yet")).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "New project" })).toBeNull();
});

test("a refused project creation stays in the sheet beside what was typed", async () => {
  const user = userEvent.setup();
  await renderApp({
    ProjectLocation: () => ({ state: "completed", location: WORKSPACE_ROOT }),
    CreateNamedProject: () => ({ state: "permission_denied", reason: "this account cannot write into that folder", context: noContext, recorded: false }),
  });
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await sheet.findByText(WORKSPACE_ROOT);
  await user.type(sheet.getByLabelText("Name"), "Scheduling investigation");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  expect(await sheet.findByText("this account cannot write into that folder")).toBeTruthy();
  expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe("Scheduling investigation");
});

test("with no remembered location, Change chooses one in the host's dialog while the sheet stays open", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ProjectLocation: () => ({ state: "empty" }),
    ChooseProjectLocation: () => ({ state: "completed", location: "/Users/someone/Projects" }),
  });
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await user.type(sheet.getByLabelText("Name"), "Registration upgrade");
  expect((sheet.getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(sheet.getByRole("button", { name: "Change" }));
  expect(await sheet.findByText("/Users/someone/Projects")).toBeTruthy();
  expect(facade.callsTo("ChooseProjectLocation")).toHaveLength(1);
  expect((sheet.getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(false);
});

test("projects this viewer opened are listed newest first; a row opens its project and a missing one offers Locate", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ListCatalog: projectsCatalog([
      listedProject("old", "Registration upgrade", "/p/old", "2026-09-20T10:00:00Z"),
      listedProject("new", "Scheduling investigation", WORKSPACE_ROOT, "2026-09-26T10:00:00Z"),
      listedProject("gone", "Moved away", "/p/gone", null, "missing"),
    ]),
    OpenWorkspace: () => folderWithCase(),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: noContext, recorded: true }),
    LocateItem: (request) => ({ state: "cancelled", context: request.context }),
    ForgetProject: () => ({ state: "completed" }),
  });
  const table = within(await page().findByRole("table", { name: "Projects" }));
  const rows = table.getAllByRole("row").filter((row) => row.hasAttribute("data-row-id"));
  expect(rows.map((row) => row.getAttribute("aria-label"))).toEqual(["Scheduling investigation", "Registration upgrade", "Moved away"]);
  // A missing project stays listed with its reason, and Locate asks the host.
  const missing = table.getByRole("row", { name: "Moved away" });
  expect(within(missing).getByText("The project folder is not where it was.")).toBeTruthy();
  await user.click(within(missing).getByRole("button", { name: "Locate" }));
  expect(facade.oneCall("LocateItem")[0]).toMatchObject({ ref: { kind: "project", id: "gone" } });
  // Remove from recents forgets it from the list only.
  await user.click(table.getByRole("button", { name: "More actions for Registration upgrade" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove from recents" }));
  expect(facade.oneCall("ForgetProject")).toEqual(["old"]);
  // The row itself opens the project, on its Cases.
  await user.click(table.getByRole("row", { name: "Scheduling investigation" }));
  expect(await page().findByRole("heading", { level: 1, name: "Cases" })).toBeTruthy();
  expect(facade.callsTo("OpenWorkspace")[0]?.args).toEqual([WORKSPACE_ROOT]);
});

test("unsaved work is one compact item on Projects, reviewed in a sheet, resumed or discarded by hand", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    EditorDrafts: () => ({
      state: "completed",
      drafts: [editorDraft("d1", "canonical-test", {}, { workspace: WORKSPACE_ROOT, case: "sample-case" })],
    }),
    OpenWorkspace: () => folderWithCase(),
  });
  const item = await page().findByRole("group", { name: "Drafts to restore" });
  await user.click(within(item).getByRole("button", { name: "Review" }));
  const review = within(screen.getByRole("dialog", { name: "Drafts to restore" }));
  expect(review.getByRole("button", { name: "Test · sample-case" })).toBeTruthy();
  await user.click(review.getByRole("button", { name: "Discard" }));
  expect(facade.oneCall("DiscardEditorDraft")).toEqual(["d1"]);
  // Nothing was resumed, sent or opened by merely listing it.
  expect(facade.callsTo("OpenWorkspace")).toHaveLength(0);
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});

test("a create the license refuses says so in the sheet and offers Activate", async () => {
  const user = userEvent.setup();
  await renderApp({
    ProjectLocation: () => ({ state: "completed", location: WORKSPACE_ROOT }),
    CreateNamedProject: () => ({ state: "permission_denied", reason: "this computer's license does not admit new work", context: noContext, recorded: false }),
  });
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await sheet.findByText(WORKSPACE_ROOT);
  await user.type(sheet.getByLabelText("Name"), "Scheduling investigation");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  expect(await sheet.findByText("this computer's license does not admit new work")).toBeTruthy();
  await user.click(sheet.getByRole("button", { name: "Activate" }));
  expect(await page().findByRole("heading", { level: 1, name: "Settings" })).toBeTruthy();
  expect(page().getByRole("button", { name: "License" }).getAttribute("aria-current")).toBe("page");
});

test("resuming a draft opens its project on the editor it belongs to, and sends nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    EditorDrafts: () => ({
      state: "completed",
      drafts: [editorDraft("d1", "canonical-test", {}, { workspace: WORKSPACE_ROOT, case: "" })],
    }),
    OpenWorkspace: () => folderWithCase(),
  });
  await user.click(await page().findByRole("button", { name: "Review" }));
  await user.click(within(screen.getByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: "Test" }));
  expect(await page().findByRole("heading", { level: 1, name: "Tests" })).toBeTruthy();
  expect(facade.callsTo("OpenWorkspace")[0]?.args).toEqual([WORKSPACE_ROOT]);
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});
