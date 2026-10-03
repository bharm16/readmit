// The home page: the projects this viewer works in and the three ways to
// start — a new project, an existing one, or the demo — with nothing about
// licensing, connections or what the build writes on it.
import { expect, test } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { goTo, page, sidebar } from "./testkit/navigation";
import { CASE_ENTRY, caseCatalogItem, editorDraft, folderChosen, folderWithCase, projectOverviewResult, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { FacadeHandlers } from "./testkit/wails";
import type { CatalogItem, CatalogQuery, CatalogResult, EditorDraft } from "./bindings";

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
  expect(sidebar().queryByRole("button", { name: "Captures" })).toBeNull();
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
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  expect(sidebar().getByRole("button", { name: "Project: workspace-under-test" })).toBeTruthy();
  expect(sidebar().getByRole("button", { name: /^Project: / })).toBeTruthy();
  expect(sidebar().getByRole("button", { name: "Captures" }).getAttribute("aria-current")).toBe("page");
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
  expect(facade.oneCall("CreateNamedProject")).toEqual([{ name: "Scheduling investigation", location: WORKSPACE_ROOT }]);
  // No folder picker opens after Create.
  expect(facade.callsTo("ChooseProjectLocation")).toHaveLength(0);
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  expect(page().getByText("No captures yet")).toBeTruthy();
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

test("a project refused for want of a license activates one and returns to New project, which creates only when asked again", async () => {
  const user = userEvent.setup();
  const document = {
    version: "readmit-entitlement/v2", id: "ENT-0002", organization: "example-hospital", plan: "annual", sequence: 1,
    not_before: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z", assignments: [{ author: "alice", devices: ["laptop"] }],
    operation_capable: true,
  };
  const { facade } = await renderApp({
    ProjectLocation: () => ({ state: "completed", location: WORKSPACE_ROOT }),
    CreateNamedProject: () => ({ state: "permission_denied", reason: "new work needs an active license on this computer", context: noContext, recorded: false }),
    ChooseLicenseFile: () => ({ state: "completed", path: "/private/received/license.json", name: "license.json" }),
    ReviewLicense: () => ({ state: "completed", entitlement: "/private/received/license.json", trust: "/private/received/keys.json", digest: "d1", document, renewal: false, choose_keys: false }),
    ActivateLicense: () => ({
      state: "completed", outcome: "activated",
      license: {
        document_id: "ENT-0002", organization: "example-hospital", plan: "annual", sequence: 1, author_seats: 1, runner_slots: 0, author: "alice", device: "laptop",
        starts: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z", grace_ends: "2027-09-18T00:00:00Z", term: "active", days_left: 300,
        renew_soon: false, activated: "2026-09-29T00:00:00Z", deactivated: false, new_work: true, current_format: true, clock_rollback: false,
      },
    }),
  });
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  let sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await sheet.findByText(WORKSPACE_ROOT);
  await user.type(sheet.getByLabelText("Name"), "Scheduling investigation");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  await user.click(await sheet.findByRole("button", { name: "Activate" }));
  // The activation flow opens on License, and nothing is installed until its
  // final action.
  const flow = within(await screen.findByRole("dialog", { name: "Activate license" }));
  await user.click(flow.getByRole("button", { name: "Choose file" }));
  await user.click(await flow.findByRole("button", { name: "Continue" }));
  await user.click(await flow.findByRole("button", { name: "Continue" }));
  await user.click(await flow.findByRole("button", { name: "Activate" }));
  // Back where the task was refused, with the task to take again: nothing
  // queued ran on its own.
  sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  expect(screen.queryByRole("dialog", { name: "Activate license" })).toBeNull();
  expect(page().getByRole("heading", { level: 1, name: "Projects" })).toBeTruthy();
  expect(facade.callsTo("ActivateLicense")).toHaveLength(1);
  expect(facade.callsTo("CreateNamedProject")).toHaveLength(1);
  expect(sheet.getByRole("button", { name: "Create" })).toBeTruthy();
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
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  expect(facade.callsTo("OpenWorkspace")[0]?.args).toEqual([WORKSPACE_ROOT]);
});

test("unsaved work is one compact item on Projects, reviewed in a sheet; a draft no editor takes back is offered only for Discard", async () => {
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
  expect(review.getByRole("rowheader", { name: "Test · sample-case" })).toBeTruthy();
  expect(review.queryByRole("button", { name: "Test · sample-case" })).toBeNull();
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
      drafts: [
        editorDraft(
          "d1",
          "suite-editor",
          { schema: "readmit-suite-editor/v1", name: "Scheduling regression", suite: { tags: [], concurrency: 1, tests: [], datasets: [], environments: [], requirements: [], exclusions: [] } },
          { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-suite-editor/v1" },
        ),
      ],
    }),
    OpenWorkspace: () => folderWithCase(),
  });
  await user.click(await page().findByRole("button", { name: "Review" }));
  await user.click(within(screen.getByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: "Suite" }));
  expect(await page().findByRole("heading", { level: 1, name: "New suite" })).toBeTruthy();
  expect(page().getByRole("tab", { name: "Tests" }).getAttribute("aria-selected")).toBe("true");
  expect(facade.callsTo("OpenWorkspace")[0]?.args).toEqual([WORKSPACE_ROOT]);
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});

test("a Locate the host answered with another folder says why on the row and keeps the entry", async () => {
  const user = userEvent.setup();
  const refusal = "that folder does not hold this project: the folder holds a different project than the one the window opened";
  const { facade } = await renderApp({
    ListCatalog: projectsCatalog([listedProject("gone", "Moved away", "/p/gone", null, "missing")]),
    LocateItem: (request) => ({ state: "cancelled", context: request.context }),
  });
  const table = within(await page().findByRole("table", { name: "Projects" }));
  // A cancelled dialog changes nothing and says nothing more.
  await user.click(within(table.getByRole("row", { name: "Moved away" })).getByRole("button", { name: "Locate" }));
  await waitFor(() => expect(facade.callsTo("LocateItem")).toHaveLength(1));
  expect(within(table.getByRole("row", { name: "Moved away" })).getByText("The project folder is not where it was.")).toBeTruthy();
  // A folder holding another project is refused with the facade's reason.
  facade.reply({ LocateItem: (request) => ({ state: "failed", reason: refusal, context: request.context }) });
  await user.click(within(table.getByRole("row", { name: "Moved away" })).getByRole("button", { name: "Locate" }));
  const row = await table.findByRole("row", { name: "Moved away" });
  expect(await within(row).findByText(refusal)).toBeTruthy();
  // The entry stays, still offering Locate, and nothing was forgotten.
  expect(within(row).getByRole("button", { name: "Locate" })).toBeTruthy();
  expect(facade.callsTo("LocateItem")[1]?.args[0]).toMatchObject({ ref: { kind: "project", id: "gone" } });
  expect(facade.callsTo("ForgetProject")).toHaveLength(0);
});

test("a refused Remove from recents says why on the row", async () => {
  const user = userEvent.setup();
  const refusal = "the remembered projects could not be replaced; they are left as they were";
  const { facade } = await renderApp({
    ListCatalog: projectsCatalog([listedProject("p1", "Scheduling investigation", WORKSPACE_ROOT, "2026-09-26T10:00:00Z")]),
    ForgetProject: () => ({ state: "failed", reason: refusal }),
  });
  const table = within(await page().findByRole("table", { name: "Projects" }));
  await user.click(table.getByRole("button", { name: "More actions for Scheduling investigation" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove from recents" }));
  expect(facade.oneCall("ForgetProject")).toEqual(["p1"]);
  const row = table.getByRole("row", { name: "Scheduling investigation" });
  expect(await within(row).findByText(refusal)).toBeTruthy();
});

test("Create cannot be pressed twice while a create runs; the new folder's name never shows", async () => {
  const user = userEvent.setup();
  const created = `${WORKSPACE_ROOT}/scheduling-investigation-7f3a`;
  const { facade } = await renderApp({
    ProjectLocation: () => ({ state: "completed", location: WORKSPACE_ROOT }),
    OpenWorkspace: () => folderChosen(created, [{ name: "project.json", kind: "project" }]),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: noContext, recorded: true }),
  });
  const parked = facade.park("CreateNamedProject");
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await sheet.findByText(WORKSPACE_ROOT);
  await user.type(sheet.getByLabelText("Name"), "Scheduling investigation");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  await user.click(sheet.getByRole("button", { name: "Create" }));
  expect(facade.callsTo("CreateNamedProject")).toHaveLength(1);
  // While it runs the sheet and the name stay, and Create cannot be pressed.
  expect(screen.getByRole("dialog", { name: "New project" })).toBeTruthy();
  expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe("Scheduling investigation");
  expect((sheet.getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(true);
  parked.resolve({ state: "completed", context: noContext, recorded: true, project: listedProject("p1", "Scheduling investigation", created, null) });
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New project" })).toBeNull());
  expect(facade.callsTo("CreateNamedProject")).toHaveLength(1);
  // The folder the facade generated is internal: its name is nowhere on screen.
  expect(document.body.textContent).not.toContain("scheduling-investigation-7f3a");
});

test("choosing a location and cancelling leaves the remembered one", async () => {
  const user = userEvent.setup();
  const chosen = "/Users/someone/Projects";
  const { facade } = await renderApp({
    ProjectLocation: () => ({ state: "completed", location: WORKSPACE_ROOT }),
    ChooseProjectLocation: () => ({ state: "completed", location: chosen }),
    CreateNamedProject: () => ({ state: "failed", reason: "stop here", context: noContext, recorded: false }),
  });
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  let sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await sheet.findByText(WORKSPACE_ROOT);
  await user.click(sheet.getByRole("button", { name: "Change" }));
  expect(await sheet.findByText(chosen)).toBeTruthy();
  await user.click(sheet.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New project" })).toBeNull());
  expect(facade.callsTo("CreateNamedProject")).toHaveLength(0);
  // Opened again, the sheet shows the remembered location, not the one chosen.
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  expect(await sheet.findByText(WORKSPACE_ROOT)).toBeTruthy();
  expect(sheet.queryByText(chosen)).toBeNull();
  // A Change that is kept is where Create puts the project.
  await user.click(sheet.getByRole("button", { name: "Change" }));
  await sheet.findByText(chosen);
  await user.type(sheet.getByLabelText("Name"), "Registration upgrade");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  expect(facade.oneCall("CreateNamedProject")).toEqual([{ name: "Registration upgrade", location: chosen }]);
});

test("after a license refusal the projects list, opening a project and Try demo still work", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ListCatalog: projectsCatalog([listedProject("p1", "Scheduling investigation", WORKSPACE_ROOT, "2026-09-26T10:00:00Z")]),
    ProjectLocation: () => ({ state: "completed", location: WORKSPACE_ROOT }),
    CreateNamedProject: () => ({ state: "permission_denied", reason: "this computer's license does not admit new work", context: noContext, recorded: false }),
    OpenWorkspace: () => folderWithCase(),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: noContext, recorded: true }),
    OpenDemoProject: () => ({ state: "completed", context: { project: WORKSPACE_ROOT, generation: 0 }, recorded: true }),
  });
  await user.click(page().getAllByRole("button", { name: "New project" })[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New project" }));
  await sheet.findByText(WORKSPACE_ROOT);
  await user.type(sheet.getByLabelText("Name"), "Registration upgrade");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  expect(await sheet.findByText("this computer's license does not admit new work")).toBeTruthy();
  await user.click(sheet.getByRole("button", { name: "Cancel" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New project" })).toBeNull());
  // The list is still there, and its row opens the project.
  const table = within(await page().findByRole("table", { name: "Projects" }));
  await user.click(table.getByRole("row", { name: "Scheduling investigation" }));
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  expect(facade.callsTo("OpenWorkspace")[0]?.args).toEqual([WORKSPACE_ROOT]);
  // Back on Projects, the demo still opens.
  await goTo(user, "Projects");
  await user.click(await page().findByRole("button", { name: "Try demo" }));
  await waitFor(() => expect(facade.callsTo("OpenDemoProject")).toHaveLength(1));
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
}, 15_000);

/** The open project, one case of it, and the answers opening it takes. */
const CASE_REF = { kind: "case" as const, id: `case-${CASE_ENTRY}`, revision: "rev-case" };
const OPEN_PROJECT: CatalogItem = {
  ...listedProject("p1", "Scheduling investigation", WORKSPACE_ROOT, "2026-09-26T10:00:00Z"),
  ref: { kind: "project", id: "p1", revision: "rev-project" },
};
const OPEN_CASE: CatalogItem = {
  ...caseCatalogItem(CASE_ENTRY, "", "investigating"),
  ref: CASE_REF,
  name: "Duplicate appointment after reschedule",
};

function openingProject(): FacadeHandlers {
  return {
    ListCatalog: (query) => {
      const items = query.kind === "project" ? [OPEN_PROJECT] : query.kind === "case" ? [OPEN_CASE] : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    OpenWorkspace: () => folderWithCase(),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: noContext, recorded: true }),
    ListNotes: (request) => ({ state: "completed", context: request.context, notes: [] }),
  };
}

const SHEET_DRAFTS: { draft: EditorDraft; object: string; sheet: string; fields: Record<string, string> }[] = [
  {
    draft: editorDraft(
      "dc",
      "case",
      { name: "Duplicate appointment, second look", status: "resolved", owner: "Scheduling desk", tags: ["scheduling"], revision: "", incidents: [], sources: [] },
      { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-case-details-draft/v1", item: { project_id: "p1", ref: CASE_REF } },
    ),
    object: "Case details · Duplicate appointment, second look",
    sheet: "Edit details",
    fields: { Name: "Duplicate appointment, second look", Status: "resolved", Owner: "Scheduling desk" },
  },
  {
    draft: editorDraft(
      "dp",
      "project",
      { name: "Scheduling go-live", owner: "Integration desk", tags: [], revisions: [] },
      { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-project-settings-draft/v1", item: { project_id: "p1", ref: { kind: "project", id: "p1" } } },
    ),
    object: "Project settings",
    sheet: "Project settings",
    fields: { Name: "Scheduling go-live", Owner: "Integration desk" },
  },
  {
    draft: editorDraft(
      "dn",
      "note",
      { schema: "readmit-note-draft/v1", name: "", subject: "", title: "Hypothesis", body: "The receiver keys on SCH-1 only." },
      { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-note-draft/v1", item: { project_id: "p1", ref: CASE_REF } },
    ),
    object: "Note · Hypothesis",
    sheet: "New note",
    fields: { Name: "Hypothesis", Content: "The receiver keys on SCH-1 only." },
  },
];

test("each retained sheet draft resumes in its sheet with the draft and the unsaved marker", async () => {
  for (const { draft, object, sheet: title, fields } of SHEET_DRAFTS) {
    cleanup();
    const user = userEvent.setup();
    const { facade } = await renderApp({ ...openingProject(), EditorDrafts: () => ({ state: "completed", drafts: [draft] }) });
    await user.click(await page().findByRole("button", { name: "Review" }));
    await user.click(within(screen.getByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: object }));
    const sheet = within(await screen.findByRole("dialog", { name: title }));
    // The mounted project sheet restores its fields after opening; finding
    // the dialog alone does not establish that its retained values arrived.
    await waitFor(() => {
      for (const [label, value] of Object.entries(fields)) {
        expect((sheet.getByLabelText(label) as HTMLInputElement).value).toBe(value);
      }
      expect(sheet.getByText("Unsaved")).toBeTruthy();
    });
    expect(facade.callsTo("OpenWorkspace")[0]?.args).toEqual([WORKSPACE_ROOT]);
    // Resuming restores the work; it saves, sends and runs nothing.
    expect(facade.callsTo("SaveItem")).toHaveLength(0);
    expect(facade.callsTo("SaveNoteItem")).toHaveLength(0);
    expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  }
}, 15_000);

test("a suite editor draft resumes on that editor's page; an earlier library editor's draft is offered only for Discard", async () => {
  const suite = { tags: [], concurrency: 1, tests: [], datasets: [], environments: [], requirements: [], exclusions: [] };
  const scenario = '{"schema":"readmit-scenario/v1","name":"restored-reschedule-scenario"}';
  {
    cleanup();
    const user = userEvent.setup();
    const draft = editorDraft("d3", "suite-editor", { schema: "readmit-suite-editor/v1", name: "Scheduling regression", suite }, { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-suite-editor/v1" });
    const { facade } = await renderApp({ ...openingProject(), EditorDrafts: () => ({ state: "completed", drafts: [draft] }) });
    await user.click(await page().findByRole("button", { name: "Review" }));
    await user.click(within(screen.getByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: "Suite" }));
    expect(await page().findByRole("heading", { level: 1, name: "New suite" })).toBeTruthy();
    expect(page().getByRole("tab", { name: "Tests" }).getAttribute("aria-selected")).toBe("true");
    // The editor holds the draft itself, as it was left, marked unsaved.
    expect(page().getByText("Scheduling regression")).toBeTruthy();
    expect(page().getByText("Unsaved")).toBeTruthy();
    expect(facade.callsTo("SaveItem")).toHaveLength(0);
    expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  }
  // The Library's editors now open saved objects; an earlier scenario draft has no editor to return to.
  cleanup();
  const user = userEvent.setup();
  await renderApp({ ...openingProject(), EditorDrafts: () => ({ state: "completed", drafts: [editorDraft("d1", "scenario", scenario, { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-scenario-draft/v1" })] }) });
  await user.click(await page().findByRole("button", { name: "Review" }));
  const review = within(screen.getByRole("dialog", { name: "Drafts to restore" }));
  expect(review.getByRole("rowheader", { name: "Scenario" })).toBeTruthy();
  expect(review.queryByRole("button", { name: "Scenario" })).toBeNull();
}, 15_000);

test("a draft whose case is gone says so on Cases and opens nothing", async () => {
  const user = userEvent.setup();
  const gone = editorDraft(
    "dg",
    "case",
    { name: "Removed meanwhile", status: "open", owner: "", tags: [], revision: "", incidents: [], sources: [] },
    { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-case-details-draft/v1", item: { project_id: "p1", ref: { kind: "case", id: "case-removed", revision: "r" } } },
  );
  const { facade } = await renderApp({ ...openingProject(), EditorDrafts: () => ({ state: "completed", drafts: [gone] }) });
  await user.click(await page().findByRole("button", { name: "Review" }));
  await user.click(within(screen.getByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: "Case details · Removed meanwhile" }));
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  expect(await page().findByText("The case this draft edits is no longer in the project.")).toBeTruthy();
  // No sheet opens, and nothing is saved, discarded or sent.
  expect(screen.queryByRole("dialog", { name: "Edit details" })).toBeNull();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(0);
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});


test("a retained project note reopens while the unrelated case list is still loading", async () => {
 const user = userEvent.setup();
 const draft = editorDraft("project-note", "note", { schema: "readmit-note-draft/v1", name: "", subject: "", title: "Hypothesis", body: "The receiver retains two appointments." }, { workspace: WORKSPACE_ROOT, case: "", content_schema: "readmit-note-draft/v1", item: { project_id: "p1", ref: OPEN_PROJECT.ref } });
 let finish!: () => void;
 const delayed = new Promise<void>(resolve => { finish = resolve; });
 await renderApp({ ...openingProject(), EditorDrafts: () => ({ state: "completed", drafts: [draft] }), ListCatalog: async query => {
  if (query.kind === "case") await delayed;
  const items = query.kind === "project" ? [OPEN_PROJECT] : [];
  return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
 } });
 try {
  await user.click(await page().findByRole("button", { name: "Review" }));
  await user.click(within(screen.getByRole("dialog", { name: "Drafts to restore" })).getByRole("button", { name: "Note · Hypothesis" }));
  const sheet = within(await screen.findByRole("dialog", { name: "New note" }));
  await waitFor(() => expect((sheet.getByLabelText("Content") as HTMLTextAreaElement).value).toBe("The receiver retains two appointments."));
 } finally { finish(); }
});
