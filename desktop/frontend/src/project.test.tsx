// The project's Cases, driven as a person drives them: the list the catalog
// reads, each case's tasks from its menu — Edit details, Notes, Attachments,
// Remove from project — and the project's own settings from its switcher.
// Every step is a real user event over the real components, with only the
// typed facade stubbed; what a save or removal means is decided on the Go
// side, and these tests prove the window sends exactly what was edited and
// keeps what was typed when it is refused.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
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
import { findCaseRow, page, readCaseIdentity, sidebar } from "./testkit/navigation";
import type { CatalogItem, CatalogQuery, CatalogResult } from "./bindings";
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
  expect(await page().findByRole("heading", { level: 1, name: "Cases" })).toBeTruthy();
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
  await user.click(page().getByRole("button", { name: "Search cases" }));
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
  await user.click(page().getByRole("button", { name: "Case" }));
  await user.click(page().getByRole("button", { name: /Case/ }));
  await user.click(await findCaseRow("Duplicate appointment after reschedule"));
  expect(facade.oneCall("OpenCase")).toEqual([WORKSPACE_ROOT, CASE_ENTRY]);
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  expect(screen.getByRole("tab", { name: "Messages", selected: true })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Back to cases" }));
  expect((await findCaseRow("Duplicate appointment after reschedule")).getAttribute("aria-selected")).toBe("true");
  // The column the list was sorted by comes back with it.
  expect(page().getByRole("columnheader", { name: /Case/ }).getAttribute("aria-sort")).toBe("descending");
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

