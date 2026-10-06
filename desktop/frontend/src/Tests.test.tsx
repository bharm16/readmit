// Tests: the saved tests list, the one editor every entry reaches (Setup,
// Checks, Review and one Create test; Setup and Checks and one Save when
// editing), and a saved test's Setup, Checks and History. Nothing here sends:
// Run hands the saved version to the run review. Fixtures carry names,
// states and synthetic tokens only.
import { expect, test } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CatalogQuery, IncompleteSave, ItemDraftResult, ItemRequest, RunExplanation, SaveItemRequest, TestContext, TestDraftDocument, TestHistoryResult } from "./bindings";
import { renderApp } from "./testkit/app";
import { editorDraft, CASE_ENTRY, CASE_IDENTITY, caseCatalogItem, caseResult, catalogOfListing, folderWithCase, GRID_OCCURRENCE, messageRow, messagesResult, NEXT_OCCURRENCE, WORKSPACE_ROOT } from "./testkit/fixtures";
import { findCaseRow, goTo, page } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);
const QA: CatalogItem = {
  ref: { kind: "environment", id: "env-qa", revision: "3" },
  name: "Scheduling QA",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { environment: { classification: "nonproduction", address: "peer-under-test:2575", transport: "plain", transport_approved: true, approval_required: true, last_checked_at: null, observation: null, has_policy: true, reset_actions: 1, reset_name: "Empty appointments" } },
};

function testItem(id: string, name: string, updated: string | null, summary: Partial<NonNullable<CatalogItem["summary"]["test"]>> = {}): CatalogItem {
  return {
    ref: { kind: "test", id, revision: "2" },
    name,
    created_at: "2026-01-01T09:00:00Z",
    updated_at: updated,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { test: { source_case: CASE.ref, latest_run: null, assertions: 1, entry: `${id}.json`, ...summary } },
  };
}

const RESCHEDULE = testItem("t-reschedule", "Reschedule keeps one appointment", "2026-01-03T09:00:00Z", { latest_result: "assertion_failure", boundary: "appointment-ledger" });
const CANCEL = testItem("t-cancel", "Cancellation removes appointment", "2026-01-02T09:00:00Z", { boundary: "ack-contract", tags: ["cancel"] });

const MESSAGES: TestContext["messages"] = [
  { id: GRID_OCCURRENCE, kind: "message", message_code: "SIU", trigger_event: "S12", sendable: true },
  { id: "occ-ack", kind: "ack", message_code: "ACK", trigger_event: "", sendable: false },
  { id: NEXT_OCCURRENCE, kind: "message", message_code: "SIU", trigger_event: "S13", sendable: true },
];

function draft(overrides: Partial<TestDraftDocument> = {}): TestDraftDocument {
  return {
    schema: "readmit-test-draft/v1",
    case: { entry: CASE_ENTRY, identity: CASE_IDENTITY },
    name: "",
    messages: [GRID_OCCURRENCE, NEXT_OCCURRENCE],
    target: "",
    boundary: "",
    observation: "",
    reset: "",
    expectations: [],
    ...overrides,
  };
}

/** The named observations a test can read, and one it cannot. */
const OBSERVATIONS = [
  { ref: { kind: "observation" as const, id: "obs-appointments", revision: "1" }, name: "Appointments", readable: true },
  { ref: { kind: "observation" as const, id: "obs-archive", revision: "1" }, name: "Archive API", readable: false, reason: "a test run reads a receiver ledger" },
];

function testContext(overrides: Partial<TestContext> = {}): TestContext {
  return { case: CASE.ref, case_name: CASE_ENTRY, messages: MESSAGES, observations: OBSERVATIONS, unsupported: [], proposals: [], read_only: false, ...overrides };
}

function newDraftAnswer(request: ItemRequest, overrides: Partial<TestDraftDocument> = {}): ItemDraftResult {
  return {
    state: "completed",
    context: request.context,
    new: true,
    ref: { kind: "test", id: "" },
    draft: {
      name: request.from?.title ?? CASE_ENTRY,
      // With nothing chosen, the facade selects every sendable message.
      test: draft({ messages: request.from && request.from.messages.length > 0 ? request.from.messages.filter((id) => id !== "occ-ack") : [GRID_OCCURRENCE, NEXT_OCCURRENCE], ...overrides }),
    },
    test: testContext(request.from?.proposals ? { proposals: request.from.proposals } : {}),
  };
}

const SAVED_DRAFT = draft({
  name: "Reschedule keeps one appointment",
  target: "qa-target.json",
  boundary: "appointment-ledger",
  observation: "appointments-2026-01-02.json",
  reset: "Empty appointments",
  expectations: [
    { id: "check-1", operator: "ledger_count", count: 1 },
    { id: "check-2", operator: "ack_field_equals", message: NEXT_OCCURRENCE, selector: "MSA-1", field: { state: "present", text: "AA" } },
  ],
});

function savedAnswer(request: ItemRequest, overrides: Partial<ItemDraftResult> = {}): ItemDraftResult {
  return {
    state: "completed",
    context: request.context,
    new: false,
    ref: { kind: "test", id: request.ref.id, revision: "2" },
    draft: { name: "Reschedule keeps one appointment", test: SAVED_DRAFT, test_links: { environment: QA.ref.id, observation: "obs-appointments", reset: "environment" } },
    test: testContext({ document: '{"schema":"readmit-test/v1"}' }),
    ...overrides,
  };
}

function catalog(tests: CatalogItem[], extra: Partial<Record<string, CatalogItem[]>> = {}) {
  return (query: CatalogQuery, facade: FacadeStub) => {
    if (query.kind === "test") return { state: "completed" as const, context: query.context, page: { items: tests, total: tests.length, snapshot: "s", recorded: true, incomplete: [] } };
    if (query.kind === "environment") return { state: "completed" as const, context: query.context, page: { items: [QA], total: 1, snapshot: "s", recorded: true, incomplete: [] } };
    if (query.kind === "case") return { state: "completed" as const, context: query.context, page: { items: [CASE], total: 1, snapshot: "s", recorded: true, incomplete: [] } };
    const items = extra[query.kind] ?? [];
    if (items.length > 0) return { state: "completed" as const, context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    return catalogOfListing(query, facade);
  };
}

async function openTests(user: User, tests: CatalogItem[], handlers: FacadeHandlers = {}, extra: Partial<Record<string, CatalogItem[]>> = {}) {
  const rendered = await renderApp({ SelectWorkspace: () => folderWithCase(), ...handlers });
  rendered.facade.reply({ ListCatalog: (query) => catalog(tests, extra)(query, rendered.facade) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tests");
  return rendered;
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

function saved(request: SaveItemRequest, id = "t-new") {
  return { state: "completed" as const, context: request.context, outcome: "saved" as const, saved: { kind: "test" as const, id, revision: "1" }, replayed: false, problems: [] };
}

test("Tests lands on the saved tests list sorted by last update, with no editor, JSON or suite controls", async () => {
  const user = userEvent.setup();
  await openTests(user, [CANCEL, RESCHEDULE]);
  const table = await page().findByRole("table", { name: "Tests" });
  await waitFor(() => expect(checkRowsOf(table)).toHaveLength(2));
  expect(checkRowsOf(table).map((row) => row[0])).toEqual(["Reschedule keeps one appointment", "Cancellation removes appointment"]);
  expect(checkRowsOf(table)[0]!.slice(2, 4)).toEqual([CASE_ENTRY, "Failed"]);
  expect(page().getByRole("tab", { name: "Test cases", selected: true })).toBeTruthy();
  expect(page().getByRole("button", { name: "New test case" })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Test page actions" }));
  expect(page().getByRole("menuitem", { name: "Library" })).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  expect(page().queryByText(/readmit-test/)).toBeNull();
});

test("a test with no compatible run shows — and a failed latest run shows Failed", async () => {
  const user = userEvent.setup();
  await openTests(user, [CANCEL, RESCHEDULE]);
  const table = await page().findByRole("table", { name: "Tests" });
  await waitFor(() => expect(checkRowsOf(table)).toHaveLength(2));
  expect(checkRowsOf(table).map((row) => row[3])).toEqual(["Failed", "—"]);
});

test("No tests yet offers New test, and a filter that matches nothing offers Clear filters", async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, []);
  expect(await page().findByText("No tests yet")).toBeTruthy();
  facade.reply({ ListCatalog: (query) => catalog([CANCEL])(query, facade) });
  await goTo(user, "Projects");
  await goTo(user, "Tests");
  await user.click(page().getByRole("button", { name: "Test page actions" }));
  await user.click(page().getByRole("menuitem", { name: "Search tests" }));
  const search = await screen.findByRole("dialog", { name: "Search tests" });
  await user.type(within(search).getByLabelText("Search"), "nothing like this");
  await user.click(within(search).getByRole("button", { name: "Search" }));
  expect(await page().findByText("No matching tests")).toBeTruthy();
  await user.click(page().getAllByRole("button", { name: "Clear filters" })[0]!);
  expect(await page().findByRole("table", { name: "Tests" })).toBeTruthy();
});

test("Search matches names and authored tags only; Filter narrows by case, result and tag", async () => {
  const user = userEvent.setup();
  await openTests(user, [CANCEL, RESCHEDULE]);
  const table = await page().findByRole("table", { name: "Tests" });
  await user.click(page().getByRole("button", { name: "Test page actions" }));
  await user.click(page().getByRole("menuitem", { name: "Search tests" }));
  await user.type(within(await screen.findByRole("dialog", { name: "Search tests" })).getByLabelText("Search"), "cancel");
  await user.click(within(screen.getByRole("dialog", { name: "Search tests" })).getByRole("button", { name: "Search" }));
  await waitFor(() => expect(checkRowsOf(table).map((row) => row[0])).toEqual(["Cancellation removes appointment"]));
  await user.click(page().getAllByRole("button", { name: "Clear filters" })[0]!);
  await user.click(page().getByRole("button", { name: "Test page actions" }));
  await user.click(page().getByRole("menuitem", { name: "Filter tests" }));
  const filter = await screen.findByRole("dialog", { name: "Filter tests" });
  await user.click(within(filter).getByRole("checkbox", { name: "Failed" }));
  await user.click(within(filter).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(checkRowsOf(table).map((row) => row[0])).toEqual(["Reschedule keeps one appointment"]));
});

test.each([false, true])("Create test uses checked messages or the inspected message without selecting the whole capture (checked: %s)", async (checked) => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => caseResult(),
    ReadMessages: () => messagesResult([messageRow(GRID_OCCURRENCE), messageRow("occ-ack", "ack"), messageRow(NEXT_OCCURRENCE, "message", { trigger_event: "S13" })]),
    OpenItemDraft: (request) => newDraftAnswer(request),
  });
  facade.reply({ ListCatalog: (query) => catalog([])(query, facade) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Cases");
  await user.dblClick(await findCaseRow(CASE_ENTRY));
  await page().findByRole("table", { name: "Messages" });
  const messages = page().getByRole("table", { name: "Messages" });
  await user.click(messages.querySelector<HTMLElement>(`tr[data-row-id="${NEXT_OCCURRENCE}"]`)!);
  if (checked) await user.click(within(messages.querySelector<HTMLElement>(`tr[data-row-id="${GRID_OCCURRENCE}"]`)!).getByRole("checkbox"));
  await user.click(await screen.findByRole("button", { name: "Create test case" }));
  expect(await page().findByRole("heading", { level: 1, name: "New test" })).toBeTruthy();
  const opened = facade.oneCall("OpenItemDraft")[0] as ItemRequest;
  expect(opened.from).toMatchObject({ case: CASE.ref, identity: CASE_IDENTITY, messages: [checked ? GRID_OCCURRENCE : NEXT_OCCURRENCE] });
  expect((page().getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe(CASE_ENTRY);
});

test("an environment saved after Tests was last listed is offered to a test created from a case's messages", async () => {
  const user = userEvent.setup();
  let environments: CatalogItem[] = [];
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => caseResult(),
    ReadMessages: () => messagesResult([messageRow(GRID_OCCURRENCE)]),
    OpenItemDraft: (request) => newDraftAnswer(request),
  });
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "environment"
        ? { state: "completed", context: query.context, page: { items: environments, total: environments.length, snapshot: "s", recorded: true, incomplete: [] } }
        : catalog([])(query, facade),
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tests");
  await page().findByText("No tests yet");
  // The environment is saved elsewhere after the list was read.
  environments = [QA];
  await goTo(user, "Cases");
  await user.dblClick(await findCaseRow(CASE_ENTRY));
  const messages = await page().findByRole("table", { name: "Messages" });
  await user.click(messages.querySelector<HTMLElement>(`tr[data-row-id="${GRID_OCCURRENCE}"]`)!);
  await user.click(await screen.findByRole("button", { name: "Create test case" }));
  const choice = await page().findByRole("combobox", { name: /^(Target|Environment)$/ });
  await waitFor(() => expect(within(choice).queryByRole("option", { name: "Scheduling QA" })).toBeTruthy());
});

test("an abandoned draft-opening reply cannot populate a newer blank test", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(), OpenCase: () => caseResult(),
    ReadMessages: () => messagesResult([messageRow(GRID_OCCURRENCE)]),
  });
  facade.reply({ListCatalog: query => catalog([])(query, facade)});
  const opening = facade.park("OpenItemDraft");
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", {name: "Open"}));
  await goTo(user, "Cases");
  await user.dblClick(await findCaseRow(CASE_ENTRY));
  const messages = await page().findByRole("table", {name: "Messages"});
  await user.click(messages.querySelector<HTMLElement>(`tr[data-row-id="${GRID_OCCURRENCE}"]`)!);
  await user.click(screen.getByRole("button", {name: "Create test case"}));
  await waitFor(() => expect(opening.size).toBe(1));
  const request = facade.oneCall("OpenItemDraft")[0];
  await goTo(user, "Tests");
  await user.click(await page().findByRole("button", {name: "New test case"}));
  await page().findByRole("combobox", {name: "Case"});
  await act(async () => opening.resolve(newDraftAnswer(request, {name: "Abandoned draft"})));
  expect(page().getByRole("combobox", {name: "Case"})).toHaveProperty("value", "");
  expect(page().queryByRole("textbox", {name: "Name"})).toBeNull();
});

function checkRowsOf(table:HTMLElement):string[][] {
 const headers=within(table).getAllByRole("columnheader").map(cell=>cell.textContent?.trim());
 const check=headers.indexOf("Check"), expected=headers.indexOf("Expected");
 if(check<0 || expected<0)return rowsOf(table);
 return rowsOf(table).map(row=>[row[check]??"",row[expected]??""]);
}

test("an ACK-only test is created through Setup, Checks and Review with one Create test", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [], {
    OpenItemDraft: (request) => newDraftAnswer(request),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => saved(request),
  });
  await user.click(await page().findByRole("button", { name: "New test case" }));
  expect(await page().findByRole("heading", { level: 1, name: "New test" })).toBeTruthy();
  await user.selectOptions(page().getByRole("combobox", { name: "Case" }), CASE.ref.id);
  const name = await page().findByRole("textbox", { name: "Name" });
  await user.clear(name);
  await user.type(name, "Reschedule is acknowledged");
  await user.click(page().getAllByRole("button", { name: "Change" })[1]!);
  const pick = await screen.findByRole("dialog", { name: "Messages" });
  expect(within(pick).getByRole("checkbox", { name: /ACK/ })).toHaveProperty("disabled", true);
  await user.click(within(pick).getByRole("checkbox", { name: /SIU · S13/ }));
  await user.click(within(pick).getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(within(pick).getByRole("button", { name: "Apply" }));
  await user.selectOptions(page().getByRole("combobox", { name: /^(Target|Environment)$/ }), QA.ref.id);
  await user.click(page().getByRole("radio", { name: "Acknowledgements" }));
  // Only this step's inputs are drawn.
  expect(page().queryByLabelText("Expected count")).toBeNull();
  await user.click(page().getByRole("button", { name: "Next" }));

  await user.click(page().getByRole("button", { name: "Add check" }));
  // Record checks belong to Appointment records only.
  expect(screen.queryByRole("menuitem", { name: "Record count" })).toBeNull();
  await user.click(await screen.findByRole("menuitem", { name: "ACK field" }));
  const sheet = await screen.findByRole("dialog", { name: "Add ack field check" });
  await user.selectOptions(within(sheet).getByLabelText("Message"), NEXT_OCCURRENCE);
  await user.selectOptions(within(sheet).getByLabelText("Field"), "MSA-1");
  await user.type(within(sheet).getByLabelText("Expected value"), "AA");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  const checks = await page().findByRole("table", { name: "Checks" });
  expect(checkRowsOf(checks)[0]!.slice(0, 2)).toEqual(["ACK MSA-1 · SIU · S13", "AA"]);

  await user.click(page().getByRole("button", { name: "Review" }));
  const setup = await page().findByLabelText("Setup", { selector: "dl" });
  expect(within(setup).getByText("Reschedule is acknowledged")).toBeTruthy();
  expect(within(setup).getByText("SIU · S12, SIU · S13")).toBeTruthy();
  expect(within(setup).getByText("Scheduling QA")).toBeTruthy();
  expect(within(setup).getByText("Acknowledgements")).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await user.click(page().getByRole("button", { name: "Create test" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.kind).toBe("test");
  expect(request.item).toBeUndefined();
  expect(request.draft.test).toMatchObject({ name: "Reschedule is acknowledged", messages: [GRID_OCCURRENCE, NEXT_OCCURRENCE], boundary: "ack-contract" });
  expect(request.draft.test?.expectations).toEqual([{ id: "check-1", operator: "ack_field_equals", message: NEXT_OCCURRENCE, selector: "MSA-1", field: { state: "present", text: "AA" } }]);
  expect(request.draft.test_links).toMatchObject({ environment: QA.ref.id });
  // No run follows creation.
  expect(facade.callsTo("RunTest" as never)).toHaveLength(0);
});

test("an ACK field check offers Present, Empty, Null and Not present, and a value only for Present", async () => {
  const user = userEvent.setup();
  await openTests(user, [], { OpenItemDraft: (request) => newDraftAnswer(request, { boundary: "ack-contract" }) });
  await user.click(await page().findByRole("button", { name: "New test case" }));
  await user.selectOptions(await page().findByRole("combobox", { name: "Case" }), CASE.ref.id);
  await user.click((await page().findAllByRole("button", { name: "Change" }))[1]!);
  await user.click(within(await screen.findByRole("dialog", { name: "Messages" })).getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(within(screen.getByRole("dialog", { name: "Messages" })).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Next" }));
  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "ACK field" }));
  const sheet = await screen.findByRole("dialog", { name: "Add ack field check" });
  expect(within(sheet).getAllByRole("radio").map((radio) => radio.parentElement?.textContent)).toEqual(["Present", "Empty", "Null", "Not present"]);
  expect(within(sheet).getByLabelText("Expected value")).toBeTruthy();
  await user.click(within(sheet).getByRole("radio", { name: "Null" }));
  expect(within(sheet).queryByLabelText("Expected value")).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect(checkRowsOf(await page().findByRole("table", { name: "Checks" }))[0]![1]).toBe("Null");
});

test("a downstream-record test needs an observation and a record check, and saves once", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [], {
    OpenItemDraft: (request) => newDraftAnswer(request),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => saved(request),
  });
  await user.click(await page().findByRole("button", { name: "New test case" }));
  await user.selectOptions(await page().findByRole("combobox", { name: "Case" }), CASE.ref.id);
  await page().findByRole("textbox", { name: "Name" });
  await user.click(page().getAllByRole("button", { name: "Change" })[1]!);
  await user.click(within(await screen.findByRole("dialog", { name: "Messages" })).getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(within(screen.getByRole("dialog", { name: "Messages" })).getByRole("button", { name: "Apply" }));
  await user.selectOptions(page().getByRole("combobox", { name: /^(Target|Environment)$/ }), QA.ref.id);
  await user.click(page().getByRole("radio", { name: "Appointment records" }));
  const observation = await page().findByRole("combobox", { name: "Observation" });
  // A named observation a test run cannot read is listed with its reason, and cannot be chosen.
  expect(within(observation).getByRole("option", { name: /^Archive API \(/ })).toHaveProperty("disabled", true);
  await user.selectOptions(observation, "obs-appointments");
  await user.selectOptions(page().getByRole("combobox", { name: "Reset" }), "environment");
  await user.click(page().getByRole("button", { name: "Next" }));
  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "Record count" }));
  const sheet = await screen.findByRole("dialog", { name: "Add record count check" });
  await user.type(within(sheet).getByLabelText("Expected count"), "0");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  // Explicit zero is kept as zero.
  expect(checkRowsOf(await page().findByRole("table", { name: "Checks" }))[0]!.slice(0, 2)).toEqual(["Record count", "0"]);
  await user.click(page().getByRole("button", { name: "Review" }));
  await page().findByLabelText("Setup", { selector: "dl" });
  await user.click(page().getByRole("button", { name: "Create test" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.draft.test).toMatchObject({ boundary: "appointment-ledger", expectations: [{ id: "check-1", operator: "ledger_count", count: 0 }] });
  expect(request.draft.test_links).toMatchObject({ environment: QA.ref.id, observation: "obs-appointments", reset: "environment" });
});

test("choosing Acknowledgements shows the record checks it affects before removing them", async () => {
  const user = userEvent.setup();
  await openTests(user, [RESCHEDULE], { OpenItemDraft: (request) => savedAnswer(request), TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }) });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await user.click(await page().findByRole("button", { name: "Edit" }));
  expect(await page().findByRole("heading", { level: 1, name: "Edit Reschedule keeps one appointment" })).toBeTruthy();
  await user.click(await page().findByRole("radio", { name: "Acknowledgements" }));
  const confirm = await screen.findByRole("dialog", { name: "Change the outcome?" });
  expect(within(confirm).getByText("Record count · 1")).toBeTruthy();
  await user.click(within(confirm).getByRole("button", { name: "Keep editing" }));
  expect(page().getByRole("radio", { name: "Appointment records" })).toHaveProperty("checked", true);
  await user.click(page().getByRole("radio", { name: "Acknowledgements" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Change the outcome?" })).getByRole("button", { name: "Remove checks" }));
  await user.click(page().getByRole("tab", { name: "Expectations" }));
  expect(checkRowsOf(await page().findByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["ACK MSA-1 · SIU · S13"]);
});

test("a refused or stale save keeps the whole draft and its problems, and a repeated click publishes once", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  let answer: "invalid" | "saved" = "invalid";
  const { facade } = await openTests(user, [], {
    OpenItemDraft: (request) => newDraftAnswer(request, { boundary: "ack-contract" }),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) =>
      answer === "invalid"
        ? { state: "completed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "test.environment", problem: "Choose a saved environment." }] }
        : saved(request),
  });
  await user.click(await page().findByRole("button", { name: "New test case" }));
  await user.selectOptions(await page().findByRole("combobox", { name: "Case" }), CASE.ref.id);
  await page().findByRole("textbox", { name: "Name" });
  await user.click(page().getAllByRole("button", { name: "Change" })[1]!);
  await user.click(within(await screen.findByRole("dialog", { name: "Messages" })).getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(within(screen.getByRole("dialog", { name: "Messages" })).getByRole("button", { name: "Apply" }));
  await user.clear(page().getByRole("textbox", { name: "Name" }));
  await user.type(page().getByRole("textbox", { name: "Name" }), "Kept name");
  await user.click(page().getByRole("button", { name: "Next" }));
  await user.click(page().getByRole("button", { name: "Review" }));
  await user.click(await page().findByRole("button", { name: "Create test" }));
  // The refusal returns to the step it names, every input kept.
  expect(await page().findByText("Choose a saved environment.")).toBeTruthy();
  expect((page().getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Kept name");
  expect(page().getByText("SIU · S13")).toBeTruthy();
  const refused = facade.oneCall("SaveItem")[0] as SaveItemRequest;

  answer = "saved";
  await user.selectOptions(page().getByRole("combobox", { name: /^(Target|Environment)$/ }), QA.ref.id);
  await user.click(page().getByRole("button", { name: "Next" }));
  await user.click(page().getByRole("button", { name: "Review" }));
  const create = await page().findByRole("button", { name: "Create test" });
  await user.dblClick(create);
  await waitFor(() => expect(facade.callsTo("SaveItem").length).toBeGreaterThanOrEqual(2));
  const intents = new Set(facade.callsTo("SaveItem").slice(1).map((call) => (call.args[0] as SaveItemRequest).intent_id));
  // A changed draft is a new intent; repeated clicks of one draft reuse one.
  expect(intents.size).toBe(1);
  expect(intents.has(refused.intent_id)).toBe(false);
});

test("Suggest checks previews proposals undecided and Apply selected adds only accepted ones", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const run: CatalogItem = {
    ref: { kind: "run", id: "run-1" },
    name: "Reschedule run",
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { run: { started_at: null, completed_at: null, outcome: "pass", uncertain: 0, delivery_uncertain: false, boundary: "ack-contract", source_case: CASE.ref, active: false } },
  };
  const { facade } = await openTests(
    user,
    [],
    {
      OpenItemDraft: (request) => newDraftAnswer(request, { boundary: "ack-contract" }),
      SuggestExpectations: () => ({
        state: "completed",
        test: {
          draft: draft({ boundary: "ack-contract" }),
          resolution: { stage: "expectations", complete: false, messages: [] } as never,
          suggestions: { origin: { result: "run-1", identity: "origin-1", status: "pass", boundary: "ack-contract", spec_identity: "", input_identity: "", run_identity: "", target_identity: "" }, suggestions: [], supported: 2, unsupported: 0 },
          proposals: [
            { id: "s1", source: "run", check: { id: "", operator: "ack_field_equals", message: GRID_OCCURRENCE, selector: "MSA-1", field: { state: "present", text: "AA" } } },
            { id: "s2", source: "run", check: { id: "", operator: "ack_field_equals", message: GRID_OCCURRENCE, selector: "MSA-2", field: { state: "present", text: "C1" } } },
          ],
        },
      }),
      ApproveExpectations: () => ({
        state: "completed",
        test: { draft: draft({ boundary: "ack-contract", expectations: [{ id: "suggested-1", operator: "ack_field_equals", message: GRID_OCCURRENCE, selector: "MSA-1", field: { state: "present", text: "AA" } }] }), resolution: {} as never },
      }),
    },
    { run: [run] },
  );
  await user.click(await page().findByRole("button", { name: "New test case" }));
  await user.selectOptions(await page().findByRole("combobox", { name: "Case" }), CASE.ref.id);
  await page().findByRole("textbox", { name: "Name" });
  await user.click(page().getAllByRole("button", { name: "Change" })[1]!);
  await user.click(within(await screen.findByRole("dialog", { name: "Messages" })).getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(within(screen.getByRole("dialog", { name: "Messages" })).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Next" }));
  await user.click(page().getByRole("button", { name: "More check actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Suggest checks" }));
  const sheet = await screen.findByRole("dialog", { name: "Suggest checks" });
  await waitFor(() => expect(within(sheet).getByRole("option", { name: "Reschedule run" })).toBeTruthy());
  await user.selectOptions(within(sheet).getByLabelText("Run"), "run-1");
  await user.click(within(sheet).getByRole("button", { name: "Preview" }));
  await within(sheet).findByRole("list", { name: "Proposed checks" });
  // Every proposal starts undecided, and nothing is applied until one is accepted.
  expect(within(sheet).getAllByRole("radio").every((radio) => !(radio as HTMLInputElement).checked)).toBe(true);
  expect(within(sheet).getByRole("button", { name: "Apply selected" })).toHaveProperty("disabled", true);
  await user.click(within(within(sheet).getByRole("radiogroup", { name: /MSA-1/ })).getByRole("radio", { name: "Accept" }));
  await user.click(within(within(sheet).getByRole("radiogroup", { name: /MSA-2/ })).getByRole("radio", { name: "Reject" }));
  await user.click(within(sheet).getByRole("button", { name: "Apply selected" }));
  await waitFor(() => expect(facade.callsTo("ApproveExpectations")).toHaveLength(1));
  expect((facade.oneCall("ApproveExpectations")[0] as { review: { decisions: unknown[] } }).review.decisions).toEqual([
    { suggestion: "s1", approved: true },
    { suggestion: "s2", approved: false },
  ]);
  expect(checkRowsOf(await page().findByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["ACK MSA-1 · SIU · S12"]);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("a saved test opens on Setup with case, messages, environment, outcome, observation and reset", async () => {
  const user = userEvent.setup();
  await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) => savedAnswer(request),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  const setup = await page().findByLabelText("Setup", { selector: "dl" });
  await waitFor(() => expect(within(setup).getByText("Scheduling QA")).toBeTruthy());
  expect(within(setup).getByText(CASE_ENTRY)).toBeTruthy();
  expect(within(setup).getByText("SIU · S12, SIU · S13")).toBeTruthy();
  expect(within(setup).getByText("Appointment records")).toBeTruthy();
  expect(await within(setup).findByText("Appointments")).toBeTruthy();
  expect(within(setup).queryByText("appointments-2026-01-02.json")).toBeNull();
  expect(within(setup).getByText("Empty appointments")).toBeTruthy();
  expect(page().getByRole("tab", { name: "Inputs", selected: true })).toBeTruthy();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  await user.click(page().getByRole("tab", { name: "Expectations" }));
  const checks = page().getByRole("list", { name: "Checks" });
  expect(within(checks).getByText("Record count")).toBeTruthy();
  expect(within(checks).getByText("AA")).toBeTruthy();
});

test("Edit saves once and a stale version is refused without overwriting", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  let outcome: "conflict" | "saved" = "conflict";
  const { facade } = await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) => savedAnswer(request),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    SaveItem: (request) =>
      outcome === "conflict"
        ? { state: "completed", context: request.context, outcome: "conflict", replayed: false, problems: [], current_revision: "3" }
        : saved(request, RESCHEDULE.ref.id),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await user.click(await page().findByRole("button", { name: "Edit" }));
  const name = await page().findByRole("textbox", { name: "Name" });
  expect(page().getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);
  await user.type(name, " twice");
  await user.click(page().getByRole("button", { name: "Save" }));
  expect(await page().findByText("This test changed since you opened it. Nothing was saved.")).toBeTruthy();
  expect((page().getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Reschedule keeps one appointment twice");
  const first = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(first).toMatchObject({ item: RESCHEDULE.ref.id, base_revision: "2" });
  outcome = "saved";
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  expect(await page().findByRole("tab", { name: "Inputs", selected: true })).toBeTruthy();
});

test("Run on an edited test offers Save changes or Keep editing and opens run review without sending", async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [RESCHEDULE], { OpenItemDraft: (request) => savedAnswer(request), TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }) });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.type(await page().findByRole("textbox", { name: "Name" }), " edited");
  await user.click(page().getByRole("button", { name: "Run" }));
  const ask = await screen.findByRole("dialog", { name: "Save changes?" });
  await user.click(within(ask).getByRole("button", { name: "Keep editing" }));
  expect(page().getByRole("heading", { level: 1, name: /^Edit / })).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  // From the saved test, Run opens the run review; nothing is sent.
  await user.click(page().getByRole("button", { name: "Cancel" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  await user.click(await page().findByRole("button", { name: "Run" }));
  expect(await screen.findByRole("dialog", { name: "Run test" })).toBeTruthy();
  // The review is prepared for the version shown and sends only on its own Send.
  await waitFor(() => expect(facade.callsTo("PrepareAction")).toHaveLength(1));
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "run.test", items: [{ kind: "test", id: RESCHEDULE.ref.id }] });
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("History lists versions and their runs, or No runs yet with Run", async () => {
  const user = userEvent.setup();
  const history: TestHistoryResult = {
    state: "completed",
    context: { project: WORKSPACE_ROOT, generation: 0 },
    versions: [
      { revision: "2", published_at: "2026-01-03T09:00:00Z", author: "Avery QA", changes: ["checks", "reset"], current: true },
      { revision: "1", published_at: "2026-01-01T09:00:00Z", author: "Avery QA", changes: [], current: false },
    ],
    runs: [{ run: { kind: "run", id: "run-1" }, revision: "2", started_at: "2026-01-03T10:00:00Z", outcome: "assertion_failure" }],
  };
  const { facade } = await openTests(user, [RESCHEDULE], { OpenItemDraft: (request) => savedAnswer(request), TestHistory: (request) => ({ ...history, context: request.context }) });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await user.click(page().getByRole("button",{name:"More test actions"}));await user.click(screen.getByRole("menuitem",{name:"Versions"}));
  const versions = await page().findByRole("table", { name: "Versions" });
  await waitFor(() => expect(rowsOf(versions)).toHaveLength(2));
  expect(rowsOf(versions)[0]!.slice(0, 1).concat(rowsOf(versions)[0]!.slice(2))).toEqual(["v2", "Avery QA", "Checks, Reset"]);
  expect(rowsOf(page().getByRole("table", { name: "Runs" }))[0]!.slice(1)).toEqual(["v2", "Failed"]);

  facade.reply({ TestHistory: (request) => ({ ...history, context: request.context, runs: [] }) });
  await goTo(user, "Projects");
  await goTo(user, "Tests");
  await user.click(page().getByRole("button",{name:"More test actions"}));await user.click(screen.getByRole("menuitem",{name:"Versions"}));
  expect(await page().findByText("No runs yet")).toBeTruthy();
});

test("Import opens a draft, Export writes the saved contract, Edit JSON validates before saving", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) => savedAnswer(request),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    ExportTestItem: (request) => ({ state: "completed", context: request.context, path: "/exports/Reschedule keeps one appointment.json" }),
    SaveItem: (request) =>
      request.draft.test_document === "{"
        ? { state: "completed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "test_document", problem: "The test is not valid JSON." }] }
        : saved(request, RESCHEDULE.ref.id),
    ImportTestDraft: (context) => ({ state: "completed", context, new: true, ref: { kind: "test", id: "" }, draft: { name: "Imported", test: draft({ name: "Imported" }) }, test: testContext() }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await page().findByLabelText("Setup", { selector: "dl" });
  await user.click(page().getByRole("button", { name: "More test actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Export test" }));
  expect(await page().findByText("Exported Reschedule keeps one appointment.json")).toBeTruthy();
  expect((facade.oneCall("ExportTestItem")[0] as ItemRequest).ref).toEqual({ kind: "test", id: RESCHEDULE.ref.id });

  await user.click(page().getByRole("button", { name: "More test actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Edit JSON" }));
  const json = await screen.findByRole("dialog", { name: "Edit JSON" });
  expect((within(json).getByLabelText("Test") as HTMLTextAreaElement).value).toBe('{"schema":"readmit-test/v1"}');
  await user.clear(within(json).getByLabelText("Test"));
  await user.type(within(json).getByLabelText("Test"), "{{");
  await user.click(within(json).getByRole("button", { name: "Save" }));
  expect(await within(json).findByText("The test is not valid JSON.")).toBeTruthy();
  expect((facade.oneCall("SaveItem")[0] as SaveItemRequest)).toMatchObject({ item: RESCHEDULE.ref.id, base_revision: "2", draft: { test_document: "{" } });
  await user.click(within(json).getByRole("button", { name: "Cancel" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));

  await user.click(page().getByRole("button", { name: "More test actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Import test" }));
  expect(await page().findByRole("heading", { level: 1, name: "New test" })).toBeTruthy();
  expect((page().getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("Imported");
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
});

test("a clause the editor cannot represent stays named and read-only and blocks a lossy save", async () => {
  const user = userEvent.setup();
  await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) =>
      savedAnswer(request, {
        test: testContext({ read_only: true, unsupported: [{ clause: "test.reset", reason: "The reset spans several lines; edit it in Edit JSON." }], document: "{}" }),
      }),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  expect(await page().findByText("The reset spans several lines; edit it in Edit JSON.")).toBeTruthy();
  expect(page().getByRole("button", { name: "Edit" })).toHaveProperty("disabled", true);
  await user.click(page().getByRole("button", { name: "More test actions" }));
  expect(await screen.findByRole("menuitem", { name: "Edit JSON" })).toBeTruthy();
});

test("Duplicate saves a copy under a new name and leaves the original", async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) => savedAnswer(request),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    SaveItem: (request) => saved(request, "t-copy"),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await page().findByLabelText("Setup", { selector: "dl" });
  await user.click(page().getByRole("button", { name: "More test actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Duplicate" }));
  const sheet = await screen.findByRole("dialog", { name: "Duplicate test" });
  expect((within(sheet).getByLabelText("Name") as HTMLInputElement).value).toBe("Reschedule keeps one appointment copy");
  await user.click(within(sheet).getByRole("button", { name: "Duplicate" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.item).toBeUndefined();
  expect(request.draft.name).toBe("Reschedule keeps one appointment copy");
});

test("Remove check can be undone, and a saved check opens its full details read-only", async () => {
  const user = userEvent.setup();
  await openTests(user, [RESCHEDULE], { OpenItemDraft: (request) => savedAnswer(request), TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }) });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await user.click(await page().findByRole("tab", { name: "Expectations" }));
  await user.click(await page().findByRole("button", { name: /^ACK MSA-1/ }));
  const details = await screen.findByRole("dialog", { name: "ACK MSA-1 · SIU · S13" });
  expect(within(details).getByText("Present")).toBeTruthy();
  expect(within(details).getByText("AA")).toBeTruthy();
  expect(within(details).queryAllByRole("textbox")).toHaveLength(0);
  await user.click(within(details).getByRole("button", { name: "Close" }));

  await user.click(page().getByRole("button", { name: "Edit" }));
  await user.click(await page().findByRole("tab", { name: "Expectations" }));
  const table = await page().findByRole("table", { name: "Checks" });
  await user.click(within(table).getByRole("button", { name: "More actions for Record count" }));
  await user.click(await screen.findByRole("menuitem", { name: "Remove check" }));
  await waitFor(() => expect(checkRowsOf(page().getByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["ACK MSA-1 · SIU · S13"]));
  await user.click(page().getByRole("button", { name: "Undo" }));
  await waitFor(() => expect(checkRowsOf(page().getByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["Record count", "ACK MSA-1 · SIU · S13"]));
});

test("Run from the command palette opens the saved test's run review and sends nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [RESCHEDULE], { OpenItemDraft: (request) => savedAnswer(request), TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }) });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await page().findByRole("button", { name: "Run" });
  await user.keyboard("{Control>}k{/Control}");
  const palette = within(await screen.findByRole("listbox", { name: "Commands" }));
  // The test's own actions come first, named for the test.
  expect(palette.getAllByRole("option")[0]?.textContent).toBe("RunReschedule keeps one appointment");
  await user.keyboard("{Enter}");
  expect(await screen.findByRole("dialog", { name: "Run test" })).toBeTruthy();
  await waitFor(() => expect(facade.callsTo("PrepareAction")).toHaveLength(1));
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a test whose outcome this window does not know reads Unsupported and cannot be run", async () => {
  const user = userEvent.setup();
  await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) => {
      const answer = savedAnswer(request);
      return { ...answer, draft: { ...answer.draft!, test: { ...answer.draft!.test!, boundary: "future-boundary" } } };
    },
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  const outcome = await page().findByText("Unsupported");
  expect(outcome.closest("details")?.querySelector("code")?.textContent).toBe("future-boundary");
  expect((page().getByRole("button", { name: "Run" }) as HTMLButtonElement).disabled).toBe(true);
});

test("a run of a test that links check groups shows each group decided against that run", async () => {
  const user = userEvent.setup();
  const GROUP = { kind: "check-group" as const, id: "cg-ledger", revision: "4" };
  const BROKEN = { kind: "check-group" as const, id: "cg-broken", revision: "1" };
  const history: TestHistoryResult = {
    state: "completed",
    context: { project: WORKSPACE_ROOT, generation: 0 },
    versions: [{ revision: "2", published_at: "2026-01-03T09:00:00Z", author: "Avery QA", changes: [], current: true }],
    runs: [{ run: { kind: "run", id: "run-1" }, revision: "1", started_at: "2026-01-03T10:00:00Z", outcome: "pass" }],
  };
  const explanation = { verdict: "pass", passed: 3, failed: 0, undecided: 0 } as RunExplanation;
  const { facade } = await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) => {
      const answer = savedAnswer(request);
      return { ...answer, draft: { ...answer.draft!, test_links: { ...answer.draft!.test_links, checks: [GROUP, BROKEN] } } };
    },
    TestHistory: (request) => ({ ...history, context: request.context }),
    TestRunChecks: (request) => ({
      state: "completed",
      context: request.context,
      checks: [
        { group: GROUP, name: "Ledger stays single", state: "completed", explanation },
        { group: BROKEN, name: "Archive rules", state: "failed", reason: "the run kept no ledger evidence" },
      ],
    }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await user.click(page().getByRole("button",{name:"More test actions"}));await user.click(screen.getByRole("menuitem",{name:"Versions"}));
  const runs = await page().findByRole("table", { name: "Runs" });
  await waitFor(() => expect(rowsOf(runs)).toHaveLength(1));
  await user.click(runs.querySelector('tr[data-row-id="run-1"]')!);
  const groups = await page().findByRole("heading", { name: "Check groups" });
  expect(groups).toBeTruthy();
  expect(facade.oneCall("TestRunChecks")[0]).toMatchObject({ test: { kind: "test", id: RESCHEDULE.ref.id, revision: "1" }, run: { kind: "run", id: "run-1" } });
  expect(await page().findByText("Ledger stays single")).toBeTruthy();
  expect(page().getByText("Passed · 3 passed, 0 failed, 0 undecided")).toBeTruthy();
  expect(page().getByText("Archive rules")).toBeTruthy();
  expect(page().getByText("the run kept no ledger evidence")).toBeTruthy();
});

test("an interrupted save is listed with its reason and Discard drops only its staged files", async () => {
  const user = userEvent.setup();
  const INTERRUPTED: IncompleteSave = { operation: "op-7", kind: "test", item: RESCHEDULE.ref, name: "Reschedule keeps one appointment", reason: "The app closed while saving." };
  const { facade } = await openTests(user, [RESCHEDULE], {
    DiscardIncompleteSave: (request) => ({ state: "completed", context: request.context }),
  });
  let pending: IncompleteSave[] = [INTERRUPTED];
  facade.reply({
    ListCatalog: (query) => {
      const answer = catalog([RESCHEDULE])(query, facade);
      return query.kind === "test" && answer.page ? { ...answer, page: { ...answer.page, incomplete: pending } } : answer;
    },
    DiscardIncompleteSave: (request) => {
      pending = [];
      return { state: "completed", context: request.context };
    },
  });
  await goTo(user, "Projects");
  await goTo(user, "Tests");
  const notice = await page().findByText(/Reschedule keeps one appointment: this save did not finish/);
  expect(notice.textContent).toContain("The app closed while saving.");
  const reads = facade.callsTo("ListCatalog").filter((call) => (call.args[0] as CatalogQuery).kind === "test").length;
  await user.click(page().getByRole("button", { name: "Discard" }));
  await waitFor(() => expect(page().queryByRole("button", { name: "Discard" })).toBeNull());
  expect(facade.oneCall("DiscardIncompleteSave")[0]).toMatchObject({ operation: "op-7" });
  expect(facade.oneCall("DiscardIncompleteSave")[0].context).toBeTruthy();
  expect(facade.callsTo("ListCatalog").filter((call) => (call.args[0] as CatalogQuery).kind === "test").length).toBeGreaterThan(reads);
  expect(page().getByRole("table", { name: "Tests" })).toBeTruthy();
});


test("a failed editor read ends Reading and Retry opens the saved draft without writing", async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [RESCHEDULE], {
    OpenItemDraft: request => savedAnswer(request),
    TestHistory: request => ({ state: "completed", context: request.context, versions: [], runs: [] }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await waitFor(() => expect(page().getByRole("button", { name: "Edit" })).toHaveProperty("disabled", false));
  facade.reply({ OpenItemDraft: request => ({ state: "failed", context: request.context, new: false, reason: "The test could not be opened." }) });
  await user.click(page().getByRole("button", { name: "Edit" }));
  await page().findByText("The test could not be opened.");
  expect(page().queryByText("Reading…")).toBeNull();
  facade.reply({ OpenItemDraft: request => savedAnswer(request) });
  await user.click(page().getByRole("button", { name: "Retry" }));
  await page().findByRole("tab", { name: "Expectations" });
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});


test("returning to a dirty saved-test edit preserves its values and refreshes newly available environments", async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [RESCHEDULE], {
    OpenItemDraft: request => savedAnswer(request),
    TestHistory: request => ({ state: "completed", context: request.context, versions: [], runs: [] }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  await user.click(await page().findByRole("button", { name: "Edit" }));
  const name = await page().findByRole("textbox", { name: "Name" });
  await user.clear(name);
  await user.type(name, "Kept draft");
  await goTo(user, "Tools");
  const newEnvironment = { ...QA, ref: { ...QA.ref, id: "new-environment" }, name: "New staging" };
  facade.reply({ ListCatalog: query => query.kind === "environment"
    ? { state: "completed", context: query.context, page: { items: [QA, newEnvironment], total: 2, snapshot: "new", recorded: true, incomplete: [] } }
    : catalog([RESCHEDULE])(query, facade) });
  await goTo(user, "Tests");
  expect(await page().findByRole("textbox", { name: "Name" })).toHaveProperty("value", "Kept draft");
  await within(page().getByRole("combobox", { name: /^(Target|Environment)$/ })).findByRole("option", { name: "New staging" });
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("target setup returns to the unfinished test with its chosen messages and scroll", async () => {
  const user = userEvent.setup();
  await openTests(user, [], {
    OpenCase: () => caseResult(),
    OpenItemDraft: (request) => request.ref.kind === "environment"
      ? { state: "completed", context: request.context, new: true, draft: { name: "" } }
      : newDraftAnswer(request),
    ListCredentials: (request) => ({ state: "empty", context: request.context, credentials: [], referring: [] }),
  });
  await user.click(await page().findByRole("button", { name: "New test case" }));
  await user.selectOptions(page().getByRole("combobox", { name: "Case" }), CASE.ref.id);
  const name = await page().findByRole("textbox", { name: "Name" });
  await user.clear(name);
  await user.type(name, "Keep this draft");
  await user.click(page().getAllByRole("button", { name: "Change" })[1]!);
  const messages = within(await screen.findByRole("dialog", { name: "Messages" }));
  await user.click(messages.getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(messages.getByRole("button", { name: "Apply" }));
  const body = document.querySelector<HTMLElement>('.page[data-page="new-test"] .page-body')!;
  body.scrollTop = 440;
  await user.click(page().getByRole("button", { name: "Add environment" }));
  const setup = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await user.click(setup.getByRole("button", { name: "Cancel" }));
  const returnedName = await page().findByRole("textbox", { name: "Name" });
  expect(returnedName).toHaveProperty("value", "Keep this draft");
  expect(page().getByText("SIU · S13")).toBeTruthy();
  expect(document.querySelector<HTMLElement>('.page[data-page="new-test"] .page-body')?.scrollTop).toBe(440);
});


test("Test cases discovers named incomplete drafts without execution setup and resumes their ordered inputs",async()=>{
 const user=userEvent.setup();
 const held=editorDraft("draft-order","test-draft",{schema:"readmit-desktop-test-editor/v1",mode:"new",step:"setup",case:CASE.ref,draft:{name:"Unconfigured ordered draft",test:draft({name:"Unconfigured ordered draft",messages:[NEXT_OCCURRENCE,GRID_OCCURRENCE]}),test_links:{}}},{case:"",identity:"",content_schema:"readmit-desktop-test-editor/v1"});
 const {facade}=await openTests(user,[],{EditorDrafts:()=>({state:"completed",drafts:[held]}),OpenItemDraft:request=>newDraftAnswer(request)});
 await user.click(page().getByRole("button",{name:/^Drafts/}));
 const drafts=within(await page().findByRole("table",{name:"Test drafts"}));
 expect(drafts.getByText("Unconfigured ordered draft")).toBeTruthy();
 expect(drafts.getByText("Draft")).toBeTruthy();
 await user.click(drafts.getByRole("button",{name:"Resume Unconfigured ordered draft"}));
 expect(await page().findByDisplayValue("Unconfigured ordered draft")).toBeTruthy();
 const original=facade.callsTo("OpenItemDraft").at(-1)?.args[0];
 expect(original).toMatchObject({from:{messages:[NEXT_OCCURRENCE,GRID_OCCURRENCE]}});
 expect(page().getByRole("button",{name:"Save draft"})).toBeTruthy();
 expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("ordered input steps can move without changing authored message expectations",async()=>{
 const user=userEvent.setup();
 const {facade}=await openTests(user,[],{OpenItemDraft:request=>newDraftAnswer(request,{expectations:[{id:"kept",operator:"ack_field_equals",message:NEXT_OCCURRENCE,selector:"MSA-1",field:{state:"present",text:"AA"}}]}),SaveEditorDraft:draft=>({state:"completed",drafts:[{...draft,id:"retained-ordered"}]})});
 await user.click(page().getByRole("button",{name:"New test case"}));
 await user.selectOptions(page().getByRole("combobox",{name:"Case"}),CASE.ref.id);
 await user.type(await page().findByRole("textbox",{name:"Name"}),"Reordered draft");
 const sourceSteps=within(page().getByRole("list",{name:"Authored input steps"}));
 await user.click(sourceSteps.getAllByRole("button")[0]!);
 await user.click(page().getByRole("button",{name:"Move input 1 down"}));
 await user.click(page().getByRole("button",{name:"Save draft"}));
 await user.click(page().getByRole("button",{name:/^Drafts/}));
 await page().findByRole("table",{name:"Test drafts"});
 expect(facade.callsTo("SaveEditorDraft").at(-1)?.args[0]).toMatchObject({content:{draft:{test:{messages:[NEXT_OCCURRENCE,GRID_OCCURRENCE],expectations:[{id:"kept",message:NEXT_OCCURRENCE,field:{text:"AA"}}]}}}});
});

test("resuming a missing-source draft keeps authored inputs and exact unsupported document content",async()=>{
 const user=userEvent.setup();
 const document='{ "schema": "readmit-test/v1", "extension": "owned unsupported clause" }';
 const held=editorDraft("draft-unavailable","test-draft",{schema:"readmit-desktop-test-editor/v1",mode:"new",step:"setup",case:CASE.ref,draft:{name:"Unavailable source draft",test:draft({name:"Unavailable source draft",messages:[NEXT_OCCURRENCE,GRID_OCCURRENCE]}),test_document:document,test_links:{}}},{case:"",identity:"",content_schema:"readmit-desktop-test-editor/v1"});
 const {facade}=await openTests(user,[],{EditorDrafts:()=>({state:"completed",drafts:[held]}),OpenItemDraft:request=>({state:"failed",reason:"The original source is unavailable.",context:request.context,new:true}),SaveEditorDraft:draft=>({state:"completed",drafts:[{...draft,id:held.id}]})});
 await user.click(page().getByRole("button",{name:/^Drafts/}));
 await user.click(await page().findByRole("button",{name:"Resume Unavailable source draft"}));
 expect(await page().findByRole("textbox",{name:"Name"})).toHaveProperty("value","Unavailable source draft");
 expect(page().getByText("The original source is unavailable.")).toBeTruthy();
 await user.click(page().getByRole("button",{name:"Save draft"}));
 await user.click(page().getByRole("button",{name:/^Drafts/}));
 await page().findByRole("table",{name:"Test drafts"});
 expect(facade.callsTo("SaveEditorDraft").at(-1)?.args[0]).toMatchObject({id:held.id,content:{draft:{test_document:document,test:{messages:[NEXT_OCCURRENCE,GRID_OCCURRENCE]}}}});
 expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("a selected saved test exposes Inputs, Expectations, Runs, Before/after and Exports while its publication history remains reachable",async()=>{
 const user=userEvent.setup();const {facade}=await openTests(user,[RESCHEDULE],{OpenItemDraft:request=>savedAnswer(request),TestHistory:request=>({state:"completed",context:request.context,versions:[{revision:"2",published_at:"2026-01-03T09:00:00Z",author:"QA",changes:[],current:true}],runs:[]})});
 await user.dblClick(await page().findByText(RESCHEDULE.name));
 for(const name of ["Inputs","Expectations","Runs","Before/after","Exports"])expect(await page().findByRole("tab",{name})).toBeTruthy();
 await user.click(page().getByRole("tab",{name:"Runs"}));await page().findByText("No runs yet");
 await user.click(page().getByRole("tab",{name:"Before/after"}));await page().findByText("Choose two retained runs of this test to compare.");
 await user.click(page().getByRole("tab",{name:"Exports"}));await page().findByRole("button",{name:"Export test definition"});
 await user.click(page().getByRole("button",{name:"More test actions"}));await user.click(screen.getByRole("menuitem",{name:"Versions"}));await page().findByRole("table",{name:"Versions"});
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("an unavailable retained run list stays unavailable and can be retried without executing the test", async () => {
  const user = userEvent.setup();
  const {facade} = await openTests(user, [RESCHEDULE], {
    OpenItemDraft: request => savedAnswer(request),
    TestHistory: request => ({state: "completed", context: request.context, versions: [], runs: []}),
  });
  const retained: CatalogItem = {ref: {kind: "run", id: "retained-run"}, name: "Retained run", created_at: null, updated_at: null, last_opened_at: null, availability: "available", capabilities: [], summary: {run: {kind: "test", test: RESCHEDULE.ref, test_association: "linked", result: "passed", started_at: "2026-01-02T12:00:00Z", completed_at: "2026-01-02T12:01:00Z", uncertain: 0, delivery_uncertain: false, active: false, entry: "retained-run"}}};
  let unavailable = true;
  facade.reply({ListCatalog: query => query.kind === "run" && unavailable
    ? {state: "failed", context: query.context, reason: "Retained runs could not be read."}
    : catalog([RESCHEDULE], {run: [retained]})(query, facade)});
  await user.dblClick(await page().findByText(RESCHEDULE.name));
  await user.click(page().getByRole("tab", {name: "Runs"}));
  await page().findByText("Retained runs could not be read.");
  expect(page().queryByText("No runs yet")).toBeNull();
  unavailable = false;
  await user.click(page().getByRole("button", {name: "Retry runs"}));
  await waitFor(() => expect(rowsOf(page().getByRole("table", {name: "Runs"}))).toHaveLength(1));
  expect(page().queryByText("Retained runs could not be read.")).toBeNull();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("selected-test retained views exclude copied or other-test records and comparison keeps the explicitly chosen roles",async()=>{
 const user=userEvent.setup();const run=(id:string,test:CatalogItem,association:string):CatalogItem=>({ref:{kind:"run",id},name:id,created_at:null,updated_at:null,last_opened_at:null,availability:"available",capabilities:[],summary:{run:{kind:"test",test:test.ref,test_association:association,started_at:id==="before-run"?"2026-01-01T12:00:00Z":"2026-01-02T12:00:00Z",completed_at:"2026-01-02T12:01:00Z",uncertain:0,delivery_uncertain:false,active:false,result:"passed",entry:id}}});
 const owned=run("before-run",RESCHEDULE,"linked"),later=run("after-run",RESCHEDULE,"linked"),copy=run("copied-run",RESCHEDULE,"unlinked"),other=run("other-run",CANCEL,"linked");
 const report=(id:string,test:CatalogItem):CatalogItem=>({ref:{kind:"report",id},name:id,created_at:null,updated_at:null,last_opened_at:null,availability:"available",capabilities:[],summary:{report:{form:"report",related_case:CASE.ref,status:"reviewed",tests:[test.ref],source_runs:[owned.ref]}}});
 const {facade}=await openTests(user,[RESCHEDULE,CANCEL],{OpenItemDraft:request=>savedAnswer(request),TestHistory:request=>({state:"completed",context:request.context,versions:[],runs:[{run:copy.ref,revision:"2",started_at:null,outcome:"pass"}]}),CompareRunItems:request=>({state:"failed",context:request.context,reason:"A retained boundary remains unavailable"})},{run:[owned,later,copy,other],report:[report("owned-report",RESCHEDULE),report("other-report",CANCEL)]});
 await user.dblClick(await page().findByText(RESCHEDULE.name));await user.click(page().getByRole("tab",{name:"Runs"}));const runs=await page().findByRole("table",{name:"Runs"});await waitFor(()=>expect(rowsOf(runs)).toHaveLength(2));expect(runs.querySelector('[data-row-id="copied-run"]')).toBeNull();expect(runs.querySelector('[data-row-id="other-run"]')).toBeNull();
 await user.click(page().getByRole("tab",{name:"Before/after"}));await user.selectOptions(page().getByLabelText("Before"),later.ref.id);await user.selectOptions(page().getByLabelText("After"),owned.ref.id);await page().findByText("A retained boundary remains unavailable");expect(facade.oneCall("CompareRunItems")[0]).toMatchObject({before:later.ref,after:owned.ref});
 await user.click(page().getByRole("tab",{name:"Exports"}));const reports=await page().findByRole("table",{name:"Test reports"});await within(reports).findByText("owned-report");expect(within(reports).queryByText("other-report")).toBeNull();
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("saved-test lifecycle navigation preserves the exact dirty editor and a refused retention keeps its current view",async()=>{
 const user=userEvent.setup();let refuse=true;
 const {facade}=await openTests(user,[RESCHEDULE],{OpenItemDraft:request=>savedAnswer(request),TestHistory:request=>({state:"completed",context:request.context,versions:[],runs:[]}),SaveEditorDraft:value=>refuse ? {state:"failed",reason:"Latest private edits were not retained"}:{state:"completed",drafts:[{...value,id:"retained-edit"}]}});
 await user.dblClick(await page().findByText(RESCHEDULE.name));await user.click(page().getByRole("button",{name:"Edit"}));const name=await page().findByLabelText("Name");await user.type(name," pending");
 await user.click(page().getByRole("tab",{name:"Runs"}));await page().findAllByText("Latest private edits were not retained");expect(page().getByLabelText("Name")).toHaveProperty("value",`${RESCHEDULE.name} pending`);
 refuse=false;await user.click(page().getByRole("button",{name:"Retry draft save"}));await waitFor(()=>expect(page().queryByText("This edit was not retained.")).toBeNull());await user.click(page().getByRole("tab",{name:"Runs"}));await page().findByText("No runs yet");
 await user.click(page().getByRole("button",{name:"Edit"}));expect(await page().findByLabelText("Name")).toHaveProperty("value",`${RESCHEDULE.name} pending`);expect(facade.callsTo("SaveItem")).toHaveLength(0);
});


test("test-case browse and recent runs show reading until the actual catalogue answers, never a fabricated zero",async()=>{
 const user=userEvent.setup();let release:()=>void=()=>undefined;
 const gate=new Promise<void>(resolve=>{release=resolve;});
 const rendered=await renderApp({SelectWorkspace:()=>folderWithCase()});
 rendered.facade.reply({ListCatalog:async query=>{if(query.kind==="test"||query.kind==="run"||query.kind==="suite")await gate;return catalog([RESCHEDULE])(query,rendered.facade);}});
 await goTo(user,"Projects");await user.click(screen.getByRole("button",{name:"Open"}));await goTo(user,"Tests");
 const browse=within(screen.getByRole("complementary",{name:"Browse test cases"}));
 expect(browse.getByRole("button",{name:/All test cases/}).textContent).toContain("Reading");
 expect(page().getByText("Reading retained runs…")).toBeTruthy();
 expect(page().queryByText("No retained runs")).toBeNull();expect(browse.queryByText("0 saved tests")).toBeNull();
 release();await page().findByRole("table",{name:"Tests"});
 expect(await browse.findByText("1 saved test")).toBeTruthy();
});

test("the saved-test step rail returns from Expectations to the exact input or preparation through Inputs",async()=>{
 const user=userEvent.setup();
 const {facade}=await openTests(user,[RESCHEDULE],{OpenItemDraft:request=>savedAnswer(request),TestHistory:request=>({state:"completed",context:request.context,versions:[],runs:[]})});
 await user.dblClick(await page().findByText(RESCHEDULE.name));
 await page().findByLabelText("Setup",{selector:"dl"});
 await user.click(page().getByRole("tab",{name:"Expectations"}));
 const rail=within(page().getByRole("complementary",{name:"Saved test steps"}));
 await user.click(rail.getByRole("button",{name:new RegExp(`2 · Send.*${NEXT_OCCURRENCE}`)}));
 expect(page().getByRole("tab",{name:"Inputs",selected:true})).toBeTruthy();
 expect(page().getByRole("region",{name:"Original test input"})).toBeTruthy();
 await user.click(page().getByRole("button",{name:"Open original message"}));
 await waitFor(()=>expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({occurrence:NEXT_OCCURRENCE,reveal:false}));
 await user.click(page().getByRole("tab",{name:"Expectations"}));
 await user.click(rail.getByRole("button",{name:/^Preparation/}));
 expect(page().getByRole("tab",{name:"Inputs",selected:true})).toBeTruthy();
 expect(page().getByLabelText("Setup",{selector:"dl"})).toBeTruthy();
 expect(page().queryByRole("region",{name:"Original test input"})).toBeNull();
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
