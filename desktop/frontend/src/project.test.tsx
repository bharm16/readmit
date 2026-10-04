// The project's Cases, driven as a person drives them: the list the catalog
// reads, each case's tasks from its menu — Edit details, Notes, Attachments,
// Remove from project — and the project's own settings from its switcher.
// Every step is a real user event over the real components, with only the
// typed facade stubbed; what a save or removal means is decided on the Go
// side, and these tests prove the window sends exactly what was edited and
// keeps what was typed when it is refused.
import { expect, test, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  OTHER_CASE_ENTRY,
  WORKSPACE_ROOT,
  caseCatalogItem,
  caseResult,
  folderChosen,
  projectOverviewResult,
} from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { findCaseRow, goTo, page, readCaseIdentity, sidebar } from "./testkit/navigation";
import type { CatalogItem, CatalogQuery, CatalogResult, RequestContext, SaveItemRequest, SaveItemResult } from "./bindings";
import type { FacadeHandlers } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const PROJECT: CatalogItem = {
  ref: { kind: "project", id: "p1", revision: "rev-project-1" },
  name: "Scheduling investigation",
  created_at: null,
  updated_at: null,
  last_opened_at: "2026-09-26T10:00:00Z",
  availability: "available",
  capabilities: [],
  summary: {
    project: {
      folder: WORKSPACE_ROOT,
      schema: "readmit-project/v2",
      cases: 1,
      interface_versions: ["v1"],
      owner: "Integration team",
      tags: ["scheduling"],
      revisions: [
        { id: "v1", name: "v1", default: true },
        { id: "upgrade", name: "Upgrade 2027", default: false },
      ],
    },
  },
};

function registered(entry: string): CatalogItem {
  const item = caseCatalogItem(entry, "", "investigating");
  return {
    ...item,
    ref: { ...item.ref, revision: `rev-${entry}` },
    name: entry === CASE_ENTRY ? "Duplicate appointment after reschedule" : entry,
    summary: { case: { ...item.summary.case!, owner: "Integration team", tags: ["scheduling"], incidents: ["INC-42"], interface_revision: "v1" } },
  };
}

function catalog(cases: CatalogItem[]) {
  return (query: CatalogQuery): CatalogResult => {
    const items = query.kind === "project" ? [PROJECT] : query.kind === "case" ? cases : [];
    return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
  };
}

/** Opens the project with its registered cases listed. */
async function openProject(user: User, handlers: FacadeHandlers = {}, cases = [registered(CASE_ENTRY)]) {
  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: "project.json", kind: "project", schema: "readmit-project/v2" },
        ...cases.map((item) => ({ name: item.summary.case!.entry, kind: "case" as const, schema: "readmit-case/v3", provenance: "generated" })),
      ]),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: { project: "", generation: 0 }, recorded: true }),
    ListCatalog: catalog(cases),
    ...handlers,
  });
  await user.click(screen.getAllByRole("button", { name: "Open" })[0] as HTMLElement);
  await sidebar().findByRole("button", { name: "Project: Scheduling investigation" });
  return { facade };
}

async function caseMenu(user: User, name: string, action: string) {
  const row = await findCaseRow(name);
  await user.click(within(row).getByRole("button", { name: `More actions for ${name}` }));
  await user.click(screen.getByRole("menuitem", { name: action }));
}

test("the Cases list fills the page with Case, Status, Owner and Updated, and no project form, guide or file tree", async () => {
  const user = userEvent.setup();
  await openProject(user);
  const row = await findCaseRow("Duplicate appointment after reschedule");
  expect(within(row).getByText("Investigating")).toBeTruthy();
  expect(within(row).getByText("Integration team")).toBeTruthy();
  expect(page().queryByText(/Other files in this folder/)).toBeNull();
  expect(page().queryByRole("button", { name: "Observations" })).toBeNull();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
});

test("Edit details sends one whole case at the revision it opened with, and a refusal keeps every value", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, {
    SaveItem: (request) => ({
      state: "failed",
      context: request.context,
      outcome: "invalid",
      replayed: false,
      problems: [{ field: "name", problem: "A case needs a name." }],
    }),
  });
  await caseMenu(user, "Duplicate appointment after reschedule", "Edit details");
  const sheet = within(await screen.findByRole("dialog", { name: "Edit details" }));
  expect((sheet.getByLabelText("Status") as HTMLSelectElement).value).toBe("investigating");
  expect(within(sheet.getByLabelText("Status")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Open", "Investigating", "Resolved", "Closed"]);
  await user.selectOptions(sheet.getByLabelText("Status"), "resolved");
  await user.clear(sheet.getByLabelText("Owner"));
  await user.type(sheet.getByLabelText("Owner"), "Scheduling desk");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  const [request] = facade.oneCall("SaveItem");
  expect(request).toMatchObject({
    kind: "case",
    item: `case-${CASE_ENTRY}`,
    base_revision: `rev-${CASE_ENTRY}`,
    draft: { case: { name: "Duplicate appointment after reschedule", status: "resolved", owner: "Scheduling desk", tags: ["scheduling"], interface_revision: "v1", incidents: ["INC-42"] } },
  });
  expect(request.intent_id).not.toBe("");
  expect(await sheet.findByText("A case needs a name.")).toBeTruthy();
  expect((sheet.getByLabelText("Owner") as HTMLInputElement).value).toBe("Scheduling desk");
  // Saved, the sheet closes and the list is read again.
  const before = facade.callsTo("ListCatalog").length;
  facade.reply({ SaveItem: (retry) => ({ state: "completed", context: retry.context, outcome: "saved", replayed: false, problems: [], saved: retry.draft.case ? { kind: "case", id: `case-${CASE_ENTRY}` } : undefined }) as never });
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit details" })).toBeNull());
  expect(facade.callsTo("ListCatalog").length).toBeGreaterThan(before);
});

test("Remove from project names the case and its one consequence, and removes only the association", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, {
    RemoveCaseFromProject: (request) => ({ state: "completed", context: request.context }),
  });
  await caseMenu(user, "Duplicate appointment after reschedule", "Remove from project");
  const sheet = within(await screen.findByRole("dialog", { name: "Remove Duplicate appointment after reschedule?" }));
  expect(sheet.getByText("Removes this case from the project; files stay on this computer.")).toBeTruthy();
  await user.click(sheet.getByRole("button", { name: "Remove" }));
  expect(facade.oneCall("RemoveCaseFromProject")[0]).toMatchObject({ ref: { kind: "case", id: `case-${CASE_ENTRY}` } });
  expect(facade.callsTo("DeleteProjectFiles" as never)).toHaveLength(0);
});

test("Project settings saves name, owner, tags and revisions as one project, and a removed revision in use asks where its cases go", async () => {
  const user = userEvent.setup();
  let asked = 0;
  const { facade } = await openProject(user, {
    SaveItem: (request) =>
      ++asked === 1
        ? {
            state: "failed",
            context: request.context,
            outcome: "invalid",
            replayed: false,
            problems: [{ field: "revisions.upgrade", problem: "Cases still use this revision.", referring: [{ ref: { kind: "case", id: "c2" }, name: "Cancellation rejected" }] }],
          }
        : { state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { kind: "project", id: "p1" } },
  });
  await user.click(sidebar().getByRole("button", { name: "Project: Scheduling investigation" }));
  await user.click(screen.getByRole("menuitem", { name: "Project settings" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Project settings" }));
  expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe("Scheduling investigation");
  expect(sheet.getByText(WORKSPACE_ROOT)).toBeTruthy();
  await user.click(sheet.getByRole("button", { name: "Remove Upgrade 2027" }));
  await user.click(sheet.getByRole("button", { name: "Save" }));
  expect(facade.callsTo("SaveItem")[0]?.args[0]).toMatchObject({
    kind: "project",
    item: "p1",
    base_revision: "rev-project-1",
    draft: { project: { name: "Scheduling investigation", owner: "Integration team", tags: ["scheduling"], revisions: [{ id: "v1", name: "v1", default: true }], reassign: [] } },
  });
  expect(await sheet.findByText(/Upgrade 2027 is used by Cancellation rejected/)).toBeTruthy();
  await user.selectOptions(sheet.getByLabelText("Move these cases to"), "v1");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  expect(facade.callsTo("SaveItem")[1]?.args[0]).toMatchObject({ draft: { project: { reassign: [{ from: "upgrade", to: "v1" }] } } });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Project settings" })).toBeNull());
});

test("the project's location is a value that Show reveals in the host's file browser", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, { RevealItem: (request) => ({ state: "completed", context: request.context }) });
  await user.click(sidebar().getByRole("button", { name: "Project: Scheduling investigation" }));
  await user.click(screen.getByRole("menuitem", { name: "Project settings" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Project settings" }));
  await user.click(sheet.getByRole("button", { name: "Show" }));
  expect(facade.oneCall("RevealItem")[0]).toMatchObject({ ref: { kind: "project", id: "p1" } });
});

test("a case's notes are listed, and a new note is one sheet with one Save about that case", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, {
    ListNotes: (request) => ({ state: "completed", context: request.context, notes: [{ id: "n1", name: "First look", content: "The second S12 arrives before the ACK.", case: { kind: "case", id: `case-${CASE_ENTRY}` }, updated_at: null }] }),
    SaveNoteItem: (request) => ({ state: "completed", context: request.context, notes: [], saved: { id: "n2", name: request.note.name, content: request.note.content, updated_at: null } }),
  });
  await caseMenu(user, "Duplicate appointment after reschedule", "Notes");
  expect(await page().findByRole("heading", { level: 1, name: "Notes" })).toBeTruthy();
  expect(facade.callsTo("ListNotes")[0]?.args[0]).toMatchObject({ case: { kind: "case", id: `case-${CASE_ENTRY}` } });
  await user.click(await within(page().getByRole("table", { name: "Notes" })).findByRole("row", { name: "First look" }));
  expect(page().getByText("The second S12 arrives before the ACK.")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "New note" }));
  const sheet = within(await screen.findByRole("dialog", { name: "New note" }));
  expect(sheet.getByText("Duplicate appointment after reschedule")).toBeTruthy();
  await user.type(sheet.getByLabelText("Name"), "Hypothesis");
  await user.type(sheet.getByLabelText("Content"), "The receiver keys on SCH-1 only.");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  expect(facade.oneCall("SaveNoteItem")[0]).toMatchObject({ note: { name: "Hypothesis", content: "The receiver keys on SCH-1 only.", case: { kind: "case", id: `case-${CASE_ENTRY}` } } });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New note" })).toBeNull());
  // Back returns to the case list.
  await user.click(page().getByRole("button", { name: "Back to Duplicate appointment after reschedule" }));
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
});

test("attachments are added through the host's file dialog, and removing one only unlinks it from the case", async () => {
  const user = userEvent.setup();
  const file = { id: "a1", name: "receiver-log.txt", type: "Text", added_at: null };
  const { facade } = await openProject(user, {
    ListAttachments: (request) => ({ state: "completed", context: request.context, attachments: [] }),
    AddAttachments: (request) => ({ state: "completed", context: request.context, attachments: [file] }),
    RemoveAttachment: (request) => ({ state: "completed", context: request.context, attachments: [] }),
  });
  await caseMenu(user, "Duplicate appointment after reschedule", "Attachments");
  await user.click(await page().findByRole("button", { name: "Add attachment" }));
  expect(facade.callsTo("AddAttachments")).toHaveLength(1);
  const row = await within(page().getByRole("table", { name: "Attachments" })).findByRole("row", { name: "receiver-log.txt" });
  await user.click(within(row).getByRole("button", { name: "More actions for receiver-log.txt" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove from case" }));
  expect(facade.oneCall("RemoveAttachment")[0]).toMatchObject({ case: { kind: "case", id: `case-${CASE_ENTRY}` }, id: "a1" });
  expect(await page().findByText("No attachments")).toBeTruthy();
});

test("search and filters narrow the list without saving anything, and chips take them off", async () => {
  const user = userEvent.setup();
  const other = { ...registered(OTHER_CASE_ENTRY), name: "Cancellation rejected", summary: { case: { ...registered(OTHER_CASE_ENTRY).summary.case!, status: "open" as const, owner: "", tags: [] } } };
  const { facade } = await openProject(user, {}, [registered(CASE_ENTRY), other]);
  await findCaseRow("Cancellation rejected");
  await user.click(page().getByRole("button", { name: "Filter cases" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Filter cases" }));
  await user.click(sheet.getByRole("checkbox", { name: "Open" }));
  await user.click(sheet.getByRole("button", { name: "Apply" }));
  expect(page().queryByRole("row", { name: "Duplicate appointment after reschedule" })).toBeNull();
  expect(page().getByRole("row", { name: "Cancellation rejected" })).toBeTruthy();
  await user.click(within(page().getByRole("group", { name: "Applied filters" })).getByRole("button", { name: "Remove Open" }));
  expect(await findCaseRow("Duplicate appointment after reschedule")).toBeTruthy();
  await user.click(page().getByRole("button",{name:"Capture list actions"}));
  await user.click(screen.getByRole("menuitem",{name:"Search cases"}));
  await user.type(within(await screen.findByRole("dialog", { name: "Search cases" })).getByLabelText("Search"), "nothing like this{Enter}");
  expect(await page().findByText("No matching cases")).toBeTruthy();
  await user.click(page().getAllByRole("button", { name: "Clear filters" })[0]!);
  expect(await findCaseRow("Cancellation rejected")).toBeTruthy();
  // Nothing about the view was written anywhere.
  expect(facade.callsTo("SaveFilter")).toHaveLength(0);
});

test("a row opens its case on Messages, and Back returns to the list with that case selected and its sort", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, { OpenCase: () => caseResult() });
  await findCaseRow("Duplicate appointment after reschedule");
  await user.click(within(page().getByRole("table",{name:"Cases"})).getByRole("button",{name:"Capture"}));
  await user.click(within(page().getByRole("table",{name:"Cases"})).getByRole("button",{name:/Capture/}));
  await user.click(await findCaseRow("Duplicate appointment after reschedule"));
  expect(facade.oneCall("OpenCase")).toEqual([WORKSPACE_ROOT, CASE_ENTRY]);
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  expect(screen.getByRole("tab", { name: "Messages", selected: true })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Back to captures" }));
  expect((await findCaseRow("Duplicate appointment after reschedule")).getAttribute("aria-selected")).toBe("true");
  // The column the list was sorted by comes back with it.
  expect(page().getByRole("columnheader", { name: /Capture/ }).getAttribute("aria-sort")).toBe("descending");
});

test("search results open the artifact they matched, not only its name", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
      ]),
    Search: () => ({
      state: "completed" as const,
      matches: [
        { kind: "artifact" as const, name: OTHER_CASE_ENTRY, label: OTHER_CASE_ENTRY, field: "name", region: "navigation" as const },
      ],
    }),
    OpenCase: () => caseResult(OTHER_CASE_ENTRY),
  });
  await user.click(screen.getAllByRole("button", { name: "Open" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  await user.keyboard("{Control>}f{/Control}");
  await user.type(screen.getByRole("searchbox", { name: "Search this project" }), "other{Enter}");
  await user.click(within(await screen.findByRole("list", { name: "Search results" })).getByRole("button", { name: /other-case/ }));
  // Opening the result verified and opened the matched case, carrying its
  // counts into the inspector.
  expect(facade.oneCall("OpenCase")).toEqual([WORKSPACE_ROOT, OTHER_CASE_ENTRY]);
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
});

test("a second navigation started while one runs starts nothing else, so no stale result can land under another case", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
      ]),
    Search: () => ({
      state: "completed" as const,
      matches: [
        { kind: "artifact" as const, name: CASE_ENTRY, label: CASE_ENTRY, field: "name", region: "navigation" as const },
        { kind: "artifact" as const, name: OTHER_CASE_ENTRY, label: OTHER_CASE_ENTRY, field: "name", region: "navigation" as const },
      ],
    }),
  });
  await user.click(screen.getAllByRole("button", { name: "Open" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  await user.keyboard("{Control>}f{/Control}");
  await user.type(screen.getByRole("searchbox", { name: "Search this project" }), "case{Enter}");
  const results = within(await screen.findByRole("list", { name: "Search results" })).getAllByRole("button");
  // The first open parks mid-verification; the second result is offered but
  // the window starts nothing else while an operation runs.
  const parked = facade.park("OpenCase");
  await user.click(results[0]!);
  await user.click(results[1]!);
  expect(facade.callsTo("OpenCase")).toHaveLength(1);
  parked.resolve(caseResult(CASE_ENTRY));
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  // The one case that ran is the one that renders.
  expect(facade.callsTo("OpenCase")[0]?.args[1]).toBe(CASE_ENTRY);
});


test("the command palette lists the selected case's own actions, and Remove from project only opens its confirmation", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, {
    RemoveCaseFromProject: (request) => ({ state: "completed", context: request.context }),
  });
  const row = await findCaseRow("Duplicate appointment after reschedule");
  row.focus();
  await user.keyboard("{Home}");
  await user.keyboard("{Control>}k{/Control}");
  await user.keyboard("Remove");
  const option = within(await screen.findByRole("listbox", { name: "Commands" })).getAllByRole("option")[0]!;
  expect(option.textContent).toBe("Remove from projectDuplicate appointment after reschedule");
  // Enter opens the confirmation the case's menu opens; it removes nothing.
  await user.keyboard("{Enter}");
  const sheet = within(await screen.findByRole("dialog", { name: "Remove Duplicate appointment after reschedule?" }));
  expect(document.activeElement).not.toBe(sheet.getByRole("button", { name: "Remove" }));
  await user.keyboard("{Enter}");
  expect(facade.callsTo("RemoveCaseFromProject")).toHaveLength(0);
});

test("Back from a case keeps the list's applied filter", async () => {
  const user = userEvent.setup();
  await openProject(user, { OpenCase: () => caseResult() });
  await findCaseRow("Duplicate appointment after reschedule");
  await user.click(page().getByRole("button", { name: "Filter cases" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Filter cases" }));
  await user.click(sheet.getByRole("checkbox", { name: "Investigating" }));
  await user.click(sheet.getByRole("button", { name: "Apply" }));
  await user.click(await findCaseRow("Duplicate appointment after reschedule"));
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Back to captures" }));
  expect(within(page().getByRole("group", { name: "Applied filters" })).getByRole("button", { name: "Remove Investigating" })).toBeTruthy();
  expect(page().getByRole("row", { name: "Duplicate appointment after reschedule" })).toBeTruthy();
});

test("Edit details names the case's sources, and a cleared name reads by its ID again", async () => {
  const user = userEvent.setup();
  const item = registered(CASE_ENTRY);
  const named = { ...item, summary: { case: { ...item.summary.case!, sources: [{ id: "s0001", name: "Scheduler" }, { id: "s0002", name: "" }] } } };
  const { facade } = await openProject(
    user,
    { SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { kind: "case", id: `case-${CASE_ENTRY}` } }) as never },
    [named],
  );
  await caseMenu(user, "Duplicate appointment after reschedule", "Edit details");
  const sheet = within(await screen.findByRole("dialog", { name: "Edit details" }));
  expect((sheet.getByRole("textbox", { name: "Name of source s0001" }) as HTMLInputElement).value).toBe("Scheduler");
  await user.clear(sheet.getByRole("textbox", { name: "Name of source s0001" }));
  await user.type(sheet.getByRole("textbox", { name: "Name of source s0002" }), "Lab feed");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  const [request] = facade.oneCall("SaveItem") as [SaveItemRequest];
  expect(request.draft.case?.sources).toEqual([
    { id: "s0001", name: "" },
    { id: "s0002", name: "Lab feed" },
  ]);
});
// ——— Delete from this computer, from a project's or a case's own menu ———

const ARCHIVE = { id: "a1", project: "Scheduling investigation", project_id: "p1", created_at: "2026-01-02T09:00:00Z", size: 44_040_192, availability: "available" as const, folder: `${WORKSPACE_ROOT}-backups/Scheduling investigation archive`, reason: "archive" as const };
const DELETE_CONSEQUENCE = "Archives the selected source, then deletes it from this computer; this is not secure erasure.";

function deleteReview(storage: Partial<NonNullable<import("./bindings").ActionReview["storage"]>>, ready = true, items: CatalogItem[] = [], refusal?: string): import("./bindings").ActionReview {
  return { token: "storage.delete-source-token", action: "storage.delete-source", consent: "restore", items, destination: {}, requirements: [], ready, ...(refusal ? { refusal } : {}), storage: { consequence: DELETE_CONSEQUENCE, ...storage } };
}

async function projectRowMenu(user: User, action: string) {
  const table = within(await page().findByRole("table", { name: "Projects" }));
  await user.click(await table.findByRole("button", { name: "More actions for Scheduling investigation" }));
  await user.click(screen.getByRole("menuitem", { name: action }));
}

test("Delete from this computer on a project row names its related work and retention, deletes only against the verified archive and reports a partial deletion", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ListCatalog: catalog([]),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: deleteReview({
        project: "Scheduling investigation",
        backup: ARCHIVE,
        related: [{ kind: "case", count: 2 }, { kind: "report", count: 1 }],
        retention: [{ kind: "search-index", entry: "sample-case.index.json", case: CASE_ENTRY, state: "within-retention", until: "2027-01-01T00:00:00Z" }],
      }),
    }),
    ExecuteReviewedAction: (request) => ({ state: "failed", reason: "not every file was removed", context: request.context, outcome: "completed", replayed: false, storage: { source: "removal-incomplete", remainder: `${WORKSPACE_ROOT}.retiring` } }),
  });
  await projectRowMenu(user, "Delete from this computer…");
  const sheet = await screen.findByRole("dialog", { name: "Delete from this computer" });
  // Reviewed under that project, which need not be open.
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.delete-source", items: [], context: { project: WORKSPACE_ROOT, project_id: "p1" } });
  expect(await within(sheet).findByText(DELETE_CONSEQUENCE)).toBeTruthy();
  expect(within(sheet).getByText(/^Scheduling investigation · /)).toBeTruthy();
  expect(within(sheet).getByText("2 cases, 1 report")).toBeTruthy();
  expect(within(within(sheet).getByRole("list", { name: "Retention" })).getByRole("listitem").textContent).toMatch(/^Search index sample-case\.index\.json · retained until /);
  await user.click(within(sheet).getByRole("button", { name: "Delete" }));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "storage.delete-source-token" });
  const done = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect(within(done).getAllByRole("alert").map((alert) => alert.textContent)).toEqual([
    "not every file was removed",
    `Archived, but not every file was deleted. What remains is in ${WORKSPACE_ROOT}.retiring.`,
  ]);
});

test("Delete from this computer is refused while a protected package is within its retention", async () => {
  const user = userEvent.setup();
  const refusal = "handoff.pkg is declared retained until 2027-01-01T00:00:00Z; nothing is deleted";
  const { facade } = await renderApp({
    ListCatalog: catalog([]),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: deleteReview(
        { project: "Scheduling investigation", backup: ARCHIVE, retention: [{ kind: "transfer-package", entry: "handoff.pkg", state: "within-retention", until: "2027-01-01T00:00:00Z", blocks: true }] },
        false,
        [],
        refusal,
      ),
    }),
  });
  await projectRowMenu(user, "Delete from this computer…");
  const sheet = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect(await within(sheet).findByText(refusal)).toBeTruthy();
  expect(within(within(sheet).getByRole("list", { name: "Retention" })).getByRole("listitem").textContent).toMatch(/^Protected package handoff\.pkg · retained until /);
  expect((within(sheet).getByRole("button", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("Enter in the Delete from this computer review deletes nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ListCatalog: catalog([]),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: deleteReview({ project: "Scheduling investigation", backup: ARCHIVE }) }),
  });
  await projectRowMenu(user, "Delete from this computer…");
  const sheet = await screen.findByRole("dialog", { name: "Delete from this computer" });
  await within(sheet).findByText(DELETE_CONSEQUENCE);
  expect(document.activeElement).not.toBe(within(sheet).getByRole("button", { name: "Delete" }));
  await user.keyboard("{Enter}");
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("Delete from this computer on a case row reviews that case against its archive and takes it off Cases", async () => {
  const user = userEvent.setup();
  const kase = registered(CASE_ENTRY);
  const { facade } = await openProject(user, {
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: deleteReview({ project: "Scheduling investigation", backup: { ...ARCHIVE, project: kase.name, case: kase.ref.id }, related: [{ kind: "analysis", count: 1 }] }, true, [kase]),
    }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, storage: { source: "deleted" } }),
  });
  await caseMenu(user, "Duplicate appointment after reschedule", "Delete from this computer…");
  const sheet = await screen.findByRole("dialog", { name: "Delete from this computer" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "storage.delete-source", items: [{ kind: "case", id: `case-${CASE_ENTRY}` }], context: { project: WORKSPACE_ROOT } });
  expect(await within(sheet).findByText(DELETE_CONSEQUENCE)).toBeTruthy();
  const rows = Array.from(sheet.querySelectorAll(".value-row")).map((row) => [row.querySelector("dt")?.textContent, row.querySelector("dd")?.textContent]);
  expect(rows).toContainEqual(["Case", "Duplicate appointment after reschedule"]);
  expect(rows).toContainEqual(["Related work", "1 analysis"]);
  const before = facade.callsTo("ListCatalog").length;
  await user.click(within(sheet).getByRole("button", { name: "Delete" }));
  expect((await within(await screen.findByRole("dialog", { name: "Delete from this computer" })).findByRole("status")).textContent).toBe("Deleted from this computer. The archive remains.");
  // Cases is read again, so the deleted case leaves the list.
  await waitFor(() => expect(facade.callsTo("ListCatalog").length).toBeGreaterThan(before));
});

// ——— RD03 follow-up: return, project switch, parked saves, recovery, notes, files, attachments ———

/** A case of the list, registered at a status, by name. */
function listedCase(entry: string, name: string, status: "open" | "investigating" = "open"): CatalogItem {
  const item = caseCatalogItem(entry, "", status);
  return { ...item, ref: { ...item.ref, revision: `rev-${entry}` }, name };
}

async function projectSettings(user: User) {
  await user.click(sidebar().getByRole("button", { name: "Project: Scheduling investigation" }));
  await user.click(screen.getByRole("menuitem", { name: "Project settings" }));
  return within(await screen.findByRole("dialog", { name: "Project settings" }));
}

const saved = (request: { context: RequestContext }, kind: "case" | "project", id: string): SaveItemResult => ({
  state: "completed",
  context: request.context,
  outcome: "saved",
  replayed: false,
  problems: [],
  saved: { kind, id },
});

test("Back from Messages restores the filter, sort, selection and scroll of the case list", async () => {
  const user = userEvent.setup();
  const ROW = 44;
  // The list's measured height and each drawn row's place in it, which a real
  // layout gives them and jsdom does not.
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.classList.contains("table-view") ? 440 : 0;
  });
  const measured = HTMLElement.prototype.getBoundingClientRect;
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const view = this.closest<HTMLElement>(".table-view");
    const rect = (top: number, height: number) => ({ top, bottom: top + height, height, left: 0, right: 800, width: 800, x: 0, y: top, toJSON: () => ({}) }) as DOMRect;
    if (view && this.tagName === "THEAD") return rect(0, ROW);
    if (view && this.tagName === "TR" && this.dataset.rowId !== undefined) return rect((Number(this.getAttribute("aria-rowindex")) - 1) * ROW - view.scrollTop, ROW);
    if (this.classList.contains("table-view")) return rect(0, 440);
    return measured.call(this);
  });
  try {
    const many = Array.from({ length: 1000 }, (_, i) => listedCase(`case-${String(i).padStart(4, "0")}`, `Case ${String(i).padStart(4, "0")}`));
    await openProject(user, { OpenCase: () => caseResult() }, [...many, listedCase("elsewhere", "Already investigated", "investigating")]);
    await findCaseRow("Case 0000");
    await user.click(page().getByRole("button", { name: "Filter cases" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Filter cases" }));
    await user.click(sheet.getByRole("checkbox", { name: "Open" }));
    await user.click(sheet.getByRole("button", { name: "Apply" }));
    await user.click(within(page().getByRole("table",{name:"Cases"})).getByRole("button",{name:"Capture"}));
    const view = page().getByRole("table", { name: "Cases" }).closest<HTMLElement>(".table-view")!;
    view.scrollTop = 880;
    fireEvent.scroll(view);
    await user.click(await findCaseRow("Case 0025"));
    expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
    await user.click(page().getByRole("button", { name: "Back to captures" }));
    const returned = await findCaseRow("Case 0025");
    expect(returned.getAttribute("aria-selected")).toBe("true");
    expect(within(page().getByRole("group", { name: "Applied filters" })).getByRole("button", { name: "Remove Open" })).toBeTruthy();
    expect(page().queryByRole("row", { name: "Already investigated" })).toBeNull();
    expect(page().getByRole("columnheader", { name: /Capture/ }).getAttribute("aria-sort")).toBe("ascending");
    const back = page().getByRole("table", { name: "Cases" }).closest<HTMLElement>(".table-view")!;
    expect(back).not.toBe(view);
    expect(back.scrollTop).toBe(880);
  } finally {
    vi.restoreAllMocks();
  }
}, 15_000);

test("switching project never shows the previous project's cases", async () => {
  const user = userEvent.setup();
  const OTHER_ROOT = "/other-project-under-test";
  const other: CatalogItem = { ...PROJECT, ref: { kind: "project", id: "p2", revision: "rev-project-2" }, name: "Registration upgrade", last_opened_at: "2026-09-20T10:00:00Z", summary: { project: { ...PROJECT.summary.project!, folder: OTHER_ROOT } } };
  const waiting: (() => void)[] = [];
  const page_ = (query: CatalogQuery, items: CatalogItem[]): CatalogResult => ({ state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } });
  const { facade } = await openProject(user, {
    ListCatalog: (query) => {
      if (query.kind === "project") return page_(query, [PROJECT, other]);
      if (query.kind !== "case") return page_(query, []);
      if (query.context.project !== OTHER_ROOT) return page_(query, [registered(CASE_ENTRY)]);
      return new Promise<CatalogResult>((resolve) => waiting.push(() => resolve(page_(query, [listedCase("registration", "Registration message rejected")]))));
    },
    OpenWorkspace: (folder) => folderChosen(folder, [{ name: "project.json", kind: "project", schema: "readmit-project/v2" }]),
  });
  await findCaseRow("Duplicate appointment after reschedule");
  await goTo(user,"Projects");
  await user.click(within(await page().findByRole("table", { name: "Projects" })).getByRole("row", { name: "Registration upgrade" }));
  expect(await page().findByRole("heading", { level: 1, name: "Captures" })).toBeTruthy();
  await waitFor(() => expect(waiting.length).toBeGreaterThan(0));
  expect(facade.callsTo("OpenWorkspace").at(-1)?.args).toEqual([OTHER_ROOT]);
  // While the other project's cases are read, none of the first one's show.
  expect(page().queryByRole("row", { name: "Duplicate appointment after reschedule" })).toBeNull();
  expect(page().queryByText("No cases yet")).toBeNull();
  for (const release of waiting.splice(0)) release();
  expect(await findCaseRow("Registration message rejected")).toBeTruthy();
  expect(page().queryByRole("row", { name: "Duplicate appointment after reschedule" })).toBeNull();
}, 15_000);

test("a parked Edit details save keeps the sheet, and a second click sends nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const parked = facade.park("SaveItem");
  await caseMenu(user, "Duplicate appointment after reschedule", "Edit details");
  const sheet = within(await screen.findByRole("dialog", { name: "Edit details" }));
  await user.clear(sheet.getByLabelText("Owner"));
  await user.type(sheet.getByLabelText("Owner"), "Scheduling desk");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await user.click(sheet.getByRole("button", { name: "Save" }));
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
  expect(screen.getByRole("dialog", { name: "Edit details" })).toBeTruthy();
  expect((sheet.getByLabelText("Owner") as HTMLInputElement).value).toBe("Scheduling desk");
  expect((sheet.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  parked.resolve(saved(facade.callsTo("SaveItem")[0]!.args[0] as SaveItemRequest, "case", `case-${CASE_ENTRY}`));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit details" })).toBeNull());
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
});

test("a parked Project settings save keeps the sheet, and a second click sends nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const parked = facade.park("SaveItem");
  const sheet = await projectSettings(user);
  await user.clear(sheet.getByLabelText("Name"));
  await user.type(sheet.getByLabelText("Name"), "Scheduling go-live");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await user.click(sheet.getByRole("button", { name: "Save" }));
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
  expect(screen.getByRole("dialog", { name: "Project settings" })).toBeTruthy();
  expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe("Scheduling go-live");
  expect((sheet.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  parked.resolve(saved(facade.callsTo("SaveItem")[0]!.args[0] as SaveItemRequest, "project", "p1"));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Project settings" })).toBeNull());
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
});

test("a parked note save keeps the sheet, and a second click sends nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, { ListNotes: (request) => ({ state: "completed", context: request.context, notes: [] }) });
  const parked = facade.park("SaveNoteItem");
  await caseMenu(user, "Duplicate appointment after reschedule", "Notes");
  await user.click((await page().findAllByRole("button", { name: "New note" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New note" }));
  await user.type(sheet.getByLabelText("Name"), "Hypothesis");
  await user.type(sheet.getByLabelText("Content"), "The receiver keys on SCH-1 only.");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await user.click(sheet.getByRole("button", { name: "Save" }));
  expect(facade.callsTo("SaveNoteItem")).toHaveLength(1);
  expect(screen.getByRole("dialog", { name: "New note" })).toBeTruthy();
  expect((sheet.getByLabelText("Content") as HTMLTextAreaElement).value).toBe("The receiver keys on SCH-1 only.");
  expect((sheet.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  parked.resolve({ state: "completed", context: { project: WORKSPACE_ROOT, generation: 0 }, notes: [], saved: { id: "n1", name: "Hypothesis", content: "The receiver keys on SCH-1 only.", updated_at: null } });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New note" })).toBeNull());
  expect(facade.callsTo("SaveNoteItem")).toHaveLength(1);
});

test("an unreadable case stays listed with its reason and Retry; a missing one with Locate, whose refusal shows on the row", async () => {
  const user = userEvent.setup();
  const unreadable: CatalogItem = { ...listedCase("locked", "Locked case"), availability: "unreadable", reason: "this account cannot read the case folder" };
  const missing: CatalogItem = { ...listedCase("moved", "Moved case"), availability: "missing", reason: "The case folder is not where it was." };
  const refusal = "the folder chosen is not an entry of this project's folder";
  const { facade } = await openProject(
    user,
    { LocateItem: (request) => ({ state: "failed", reason: refusal, context: request.context }) },
    [registered(CASE_ENTRY), unreadable, missing],
  );
  const locked = await findCaseRow("Locked case");
  expect(within(locked).getByText("this account cannot read the case folder")).toBeTruthy();
  const before = facade.callsTo("ListCatalog").length;
  await user.click(within(locked).getByRole("button", { name: "Retry" }));
  await waitFor(() => expect(facade.callsTo("ListCatalog").length).toBeGreaterThan(before));
  expect(await findCaseRow("Locked case")).toBeTruthy();
  const moved = await findCaseRow("Moved case");
  expect(within(moved).getByText("The case folder is not where it was.")).toBeTruthy();
  await user.click(within(moved).getByRole("button", { name: "Locate" }));
  expect(facade.oneCall("LocateItem")[0]).toMatchObject({ ref: { kind: "case", id: "case-moved" } });
  const refused = await findCaseRow("Moved case");
  expect(await within(refused).findByText(refusal)).toBeTruthy();
  expect(within(refused).getByRole("button", { name: "Locate" })).toBeTruthy();
});

test("Project settings > Open notes lists the project's own notes, older ones included", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, {
    ListNotes: (request) => ({
      state: "completed",
      context: request.context,
      notes: request.case
        ? []
        : [
            { id: "n2", name: "Go-live checklist", content: "Confirm the receiver build.", updated_at: "2026-09-26T10:00:00Z" },
            { id: "n1", name: "Kickoff notes", content: "Written before notes had dates.", updated_at: null },
          ],
    }),
  });
  const sheet = await projectSettings(user);
  await user.click(sheet.getByRole("button", { name: "Open notes" }));
  expect(await page().findByRole("heading", { level: 1, name: "Notes" })).toBeTruthy();
  const table = within(page().getByRole("table", { name: "Notes" }));
  expect(await table.findByRole("row", { name: "Go-live checklist" })).toBeTruthy();
  expect(table.getByRole("row", { name: "Kickoff notes" })).toBeTruthy();
  // The project's own notes are read without a case.
  expect(facade.callsTo("ListNotes").at(-1)?.args[0]).not.toHaveProperty("case");
});

test("a note's subject changes with Change and saves under the new case", async () => {
  const user = userEvent.setup();
  const other = listedCase(OTHER_CASE_ENTRY, "Cancellation rejected");
  const { facade } = await openProject(
    user,
    {
      ListNotes: (request) => ({ state: "completed", context: request.context, notes: [] }),
      SaveNoteItem: (request) => ({ state: "completed", context: request.context, notes: [], saved: { id: "n2", name: request.note.name, content: request.note.content, updated_at: null } }),
    },
    [registered(CASE_ENTRY), other],
  );
  await caseMenu(user, "Duplicate appointment after reschedule", "Notes");
  await user.click((await page().findAllByRole("button", { name: "New note" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New note" }));
  expect(sheet.getByText("Duplicate appointment after reschedule")).toBeTruthy();
  await user.type(sheet.getByLabelText("Name"), "Same cause");
  await user.click(sheet.getByRole("button", { name: "Change" }));
  const about = sheet.getByLabelText("About") as HTMLSelectElement;
  expect(within(about).getAllByRole("option").map((option) => option.textContent)).toEqual(["Scheduling investigation", "Duplicate appointment after reschedule", "Cancellation rejected"]);
  await user.selectOptions(about, other.ref.id);
  await user.click(sheet.getByRole("button", { name: "Save" }));
  expect(facade.oneCall("SaveNoteItem")[0]).toMatchObject({ note: { name: "Same cause", case: { kind: "case", id: other.ref.id } } });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New note" })).toBeNull());
});

test("Files > Open file reads that file", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, {
    ProjectFiles: (request) => ({ state: "completed", context: request.context, files: [{ name: "legacy-feed.hl7", kind: "unsupported" }] }),
    ListFileMessages: (request) => ({
      state: "completed",
      name: "legacy-feed.hl7",
      bytes: 0,
      sha256: "0".repeat(64),
      format: "raw",
      terminator: "cr",
      format_selection: "detected",
      terminator_selection: "detected",
      total: 0,
      offset: request.offset,
      rows: [],
    }),
  });
  await user.click(sidebar().getByRole("button", { name: "Project: Scheduling investigation" }));
  await user.click(screen.getByRole("menuitem", { name: "Files" }));
  const row = await within(await page().findByRole("table", { name: "Files" })).findByRole("row", { name: "legacy-feed.hl7" });
  await user.click(within(row).getByRole("button", { name: "More actions for legacy-feed.hl7" }));
  await user.click(screen.getByRole("menuitem", { name: "Open file" }));
  expect(await page().findByRole("heading", { name: "legacy-feed.hl7" })).toBeTruthy();
  expect(facade.oneCall("ListFileMessages")[0]).toMatchObject({ file: `${WORKSPACE_ROOT}/legacy-feed.hl7` });
  // The file is named by the project; the host is never asked for one.
  expect(facade.callsTo("ChooseInspectionPath")).toHaveLength(0);
});

test("a refused attachment add or remove keeps the list and shows the reason", async () => {
  const user = userEvent.setup();
  const file = { id: "a1", name: "receiver-log.txt", type: "Text", added_at: null };
  const quota = "the project's quota does not leave room for these files; nothing was attached";
  const unknown = "the case holds no such attachment";
  const { facade } = await openProject(user, {
    ListAttachments: (request) => ({ state: "completed", context: request.context, attachments: [file] }),
    AddAttachments: (request) => ({ state: "failed", reason: quota, context: request.context, attachments: [file] }),
    RemoveAttachment: (request) => ({ state: "failed", reason: unknown, context: request.context, attachments: [file] }),
  });
  await caseMenu(user, "Duplicate appointment after reschedule", "Attachments");
  const table = within(await page().findByRole("table", { name: "Attachments" }));
  await table.findByRole("row", { name: "receiver-log.txt" });
  await user.click(page().getByRole("button", { name: "Add attachment" }));
  expect(facade.callsTo("AddAttachments")).toHaveLength(1);
  expect((await page().findByRole("alert")).textContent).toBe(quota);
  expect(table.getByRole("row", { name: "receiver-log.txt" })).toBeTruthy();
  await user.click(within(table.getByRole("row", { name: "receiver-log.txt" })).getByRole("button", { name: "More actions for receiver-log.txt" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove from case" }));
  expect(facade.oneCall("RemoveAttachment")[0]).toMatchObject({ id: "a1" });
  await waitFor(() => expect(page().getByRole("alert").textContent).toBe(unknown));
  expect(within(page().getByRole("table", { name: "Attachments" })).getByRole("row", { name: "receiver-log.txt" })).toBeTruthy();
  expect(page().queryByText("No attachments")).toBeNull();
});

test("editing a case's details retains a draft of that case until it is saved", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, {
    SaveEditorDraft: (draft) => ({ state: "completed", drafts: [{ ...draft, id: draft.id || "draft-case-1", saved_at: "2026-09-27T10:00:00Z" }] }),
    SaveItem: (request) => saved(request, "case", `case-${CASE_ENTRY}`),
  });
  await caseMenu(user, "Duplicate appointment after reschedule", "Edit details");
  const sheet = within(await screen.findByRole("dialog", { name: "Edit details" }));
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(0);
  await user.clear(sheet.getByLabelText("Owner"));
  await user.type(sheet.getByLabelText("Owner"), "Scheduling desk");
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0));
  const retained = facade.callsTo("SaveEditorDraft").at(-1)!.args[0] as import("./bindings").EditorDraft;
  expect(retained).toMatchObject({
    kind: "case",
    workspace: WORKSPACE_ROOT,
    content_schema: "readmit-case-details-draft/v1",
    content: { name: "Duplicate appointment after reschedule", owner: "Scheduling desk", status: "investigating" },
    item: { project_id: "p1", ref: { kind: "case", id: `case-${CASE_ENTRY}` } },
  });
  expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(0);
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit details" })).toBeNull());
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
  // Saved, nothing is left to restore.
  await waitFor(() => expect(facade.callsTo("DiscardEditorDraft").map((call) => call.args[0])).toContain("draft-case-1"));
});
