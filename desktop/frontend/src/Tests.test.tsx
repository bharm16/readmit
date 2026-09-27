// Tests: the saved tests list, the one editor every entry reaches (Setup,
// Checks, Review and one Create test; Setup and Checks and one Save when
// editing), and a saved test's Setup, Checks and History. Nothing here sends:
// Run hands the saved version to the run review. Fixtures carry names,
// states and synthetic tokens only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CatalogQuery, ItemDraftResult, ItemRequest, SaveItemRequest, TestContext, TestDraftDocument, TestHistoryResult } from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, CASE_IDENTITY, caseCatalogItem, caseResult, catalogOfListing, folderWithCase, GRID_OCCURRENCE, messageRow, messagesResult, NEXT_OCCURRENCE, WORKSPACE_ROOT } from "./testkit/fixtures";
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
  summary: { environment: { classification: "nonproduction", address: "peer-under-test:2575", transport: "plain", last_checked_at: null, observation: null, has_policy: true, reset_actions: 1, reset_name: "Empty appointments" } },
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

function testContext(overrides: Partial<TestContext> = {}): TestContext {
  return { case: CASE.ref, case_name: CASE_ENTRY, messages: MESSAGES, unsupported: [], proposals: [], read_only: false, ...overrides };
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
    draft: { name: "Reschedule keeps one appointment", test: SAVED_DRAFT, test_links: { environment: QA.ref.id, reset: "environment" } },
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
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  expect(rowsOf(table).map((row) => row[0])).toEqual(["Reschedule keeps one appointment", "Cancellation removes appointment"]);
  expect(rowsOf(table)[0]!.slice(1, 3)).toEqual([CASE_ENTRY, "Failed"]);
  expect(page().getByRole("tab", { name: "Tests", selected: true })).toBeTruthy();
  expect(page().getByRole("button", { name: "New test" })).toBeTruthy();
  expect(page().getByRole("button", { name: "Library" })).toBeTruthy();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  expect(page().queryByText(/readmit-test/)).toBeNull();
});

test("a test with no compatible run shows — and a failed latest run shows Failed", async () => {
  const user = userEvent.setup();
  await openTests(user, [CANCEL, RESCHEDULE]);
  const table = await page().findByRole("table", { name: "Tests" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  expect(rowsOf(table).map((row) => row[2])).toEqual(["Failed", "—"]);
});

test("No tests yet offers New test, and a filter that matches nothing offers Clear filters", async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, []);
  expect(await page().findByText("No tests yet")).toBeTruthy();
  facade.reply({ ListCatalog: (query) => catalog([CANCEL])(query, facade) });
  await goTo(user, "Projects");
  await goTo(user, "Tests");
  await user.click(page().getByRole("button", { name: "Search tests" }));
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
  await user.click(page().getByRole("button", { name: "Search tests" }));
  await user.type(within(await screen.findByRole("dialog", { name: "Search tests" })).getByLabelText("Search"), "cancel");
  await user.click(within(screen.getByRole("dialog", { name: "Search tests" })).getByRole("button", { name: "Search" }));
  await waitFor(() => expect(rowsOf(table).map((row) => row[0])).toEqual(["Cancellation removes appointment"]));
  await user.click(page().getAllByRole("button", { name: "Clear filters" })[0]!);
  await user.click(page().getByRole("button", { name: "Filter tests" }));
  const filter = await screen.findByRole("dialog", { name: "Filter tests" });
  await user.click(within(filter).getByRole("checkbox", { name: "Failed" }));
  await user.click(within(filter).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(rowsOf(table).map((row) => row[0])).toEqual(["Reschedule keeps one appointment"]));
});

test("Create test from selected case messages opens Setup prefilled in source order without visiting Messages", async () => {
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
  await user.click(await screen.findByRole("button", { name: "More case actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Create test" }));
  expect(await page().findByRole("heading", { level: 1, name: "New test" })).toBeTruthy();
  const opened = facade.oneCall("OpenItemDraft")[0] as ItemRequest;
  expect(opened.from).toMatchObject({ case: CASE.ref, messages: [] });
  expect(await page().findByText("SIU · S12, SIU · S13")).toBeTruthy();
  expect((page().getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe(CASE_ENTRY);
});

test("an ACK-only test is created through Setup, Checks and Review with one Create test", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [], {
    OpenItemDraft: (request) => newDraftAnswer(request),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => saved(request),
  });
  await user.click(await page().findByRole("button", { name: "New test" }));
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
  await user.selectOptions(page().getByRole("combobox", { name: "Environment" }), QA.ref.id);
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
  expect(rowsOf(checks)[0]!.slice(0, 2)).toEqual(["ACK MSA-1 · SIU · S13", "AA"]);

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
  await user.click(await page().findByRole("button", { name: "New test" }));
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
  expect(rowsOf(await page().findByRole("table", { name: "Checks" }))[0]![1]).toBe("Null");
});

test("a downstream-record test needs an observation and a record check, and saves once", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openTests(user, [], {
    OpenItemDraft: (request) => newDraftAnswer(request),
    ListReceiverSnapshots: (request) => ({ state: "completed", context: request.context, snapshots: [{ entry: "appointments-2026-01-02.json", collected_at: "2026-01-02T09:00:00Z" }] }),
    ValidateDraft: (request) => ({ state: "completed", context: request.context, problems: [] }),
    SaveItem: (request) => saved(request),
  });
  await user.click(await page().findByRole("button", { name: "New test" }));
  await user.selectOptions(await page().findByRole("combobox", { name: "Case" }), CASE.ref.id);
  await page().findByRole("textbox", { name: "Name" });
  await user.click(page().getAllByRole("button", { name: "Change" })[1]!);
  await user.click(within(await screen.findByRole("dialog", { name: "Messages" })).getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(within(screen.getByRole("dialog", { name: "Messages" })).getByRole("button", { name: "Apply" }));
  await user.selectOptions(page().getByRole("combobox", { name: "Environment" }), QA.ref.id);
  await user.click(page().getByRole("radio", { name: "Appointment records" }));
  const observation = await page().findByRole("combobox", { name: "Observation" });
  await waitFor(() => expect(within(observation).getByRole("option", { name: /^Collected / })).toBeTruthy());
  await user.selectOptions(observation, "appointments-2026-01-02.json");
  await user.selectOptions(page().getByRole("combobox", { name: "Reset" }), "environment");
  await user.click(page().getByRole("button", { name: "Next" }));
  await user.click(page().getByRole("button", { name: "Add check" }));
  await user.click(await screen.findByRole("menuitem", { name: "Record count" }));
  const sheet = await screen.findByRole("dialog", { name: "Add record count check" });
  await user.type(within(sheet).getByLabelText("Expected count"), "0");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  // Explicit zero is kept as zero.
  expect(rowsOf(await page().findByRole("table", { name: "Checks" }))[0]!.slice(0, 2)).toEqual(["Record count", "0"]);
  await user.click(page().getByRole("button", { name: "Review" }));
  await page().findByLabelText("Setup", { selector: "dl" });
  await user.click(page().getByRole("button", { name: "Create test" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.draft.test).toMatchObject({ boundary: "appointment-ledger", observation: "appointments-2026-01-02.json", expectations: [{ id: "check-1", operator: "ledger_count", count: 0 }] });
  expect(request.draft.test_links).toMatchObject({ environment: QA.ref.id, reset: "environment" });
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
  await user.click(page().getByRole("tab", { name: "Checks" }));
  expect(rowsOf(await page().findByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["ACK MSA-1 · SIU · S13"]);
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
  await user.click(await page().findByRole("button", { name: "New test" }));
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
  await user.selectOptions(page().getByRole("combobox", { name: "Environment" }), QA.ref.id);
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
    summary: { run: { started_at: null, completed_at: null, outcome: "pass", uncertain: 0, delivery_uncertain: false, boundary: "ack-contract", source_case: CASE.ref } },
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
  await user.click(await page().findByRole("button", { name: "New test" }));
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
  expect(rowsOf(await page().findByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["ACK MSA-1 · SIU · S12"]);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("a saved test opens on Setup with case, messages, environment, outcome, observation and reset", async () => {
  const user = userEvent.setup();
  await openTests(user, [RESCHEDULE], {
    OpenItemDraft: (request) => savedAnswer(request),
    TestHistory: (request) => ({ state: "completed", context: request.context, versions: [], runs: [] }),
    ListReceiverSnapshots: (request) => ({ state: "completed", context: request.context, snapshots: [{ entry: "appointments-2026-01-02.json", collected_at: "2026-01-02T09:00:00Z" }] }),
  });
  await user.dblClick(await page().findByText("Reschedule keeps one appointment"));
  const setup = await page().findByLabelText("Setup", { selector: "dl" });
  await waitFor(() => expect(within(setup).getByText("Scheduling QA")).toBeTruthy());
  expect(within(setup).getByText(CASE_ENTRY)).toBeTruthy();
  expect(within(setup).getByText("SIU · S12, SIU · S13")).toBeTruthy();
  expect(within(setup).getByText("Appointment records")).toBeTruthy();
  expect(await within(setup).findByText(/^Collected /)).toBeTruthy();
  expect(within(setup).queryByText("appointments-2026-01-02.json")).toBeNull();
  expect(within(setup).getByText("Empty appointments")).toBeTruthy();
  expect(page().getByRole("tab", { name: "Setup", selected: true })).toBeTruthy();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  await user.click(page().getByRole("tab", { name: "Checks" }));
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
  expect(await page().findByRole("tab", { name: "Setup", selected: true })).toBeTruthy();
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
  expect(await page().findByRole("heading", { level: 1, name: "Run test" })).toBeTruthy();
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
  await user.click(await page().findByRole("tab", { name: "History" }));
  const versions = await page().findByRole("table", { name: "Versions" });
  await waitFor(() => expect(rowsOf(versions)).toHaveLength(2));
  expect(rowsOf(versions)[0]!.slice(0, 1).concat(rowsOf(versions)[0]!.slice(2))).toEqual(["v2", "Avery QA", "Checks, Reset"]);
  expect(rowsOf(page().getByRole("table", { name: "Runs" }))[0]!.slice(1)).toEqual(["v2", "Failed"]);

  facade.reply({ TestHistory: (request) => ({ ...history, context: request.context, runs: [] }) });
  await goTo(user, "Projects");
  await goTo(user, "Tests");
  await user.click(await page().findByRole("tab", { name: "History" }));
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
  await user.click(await page().findByRole("tab", { name: "Checks" }));
  await user.click(await page().findByRole("button", { name: /^ACK MSA-1/ }));
  const details = await screen.findByRole("dialog", { name: "ACK MSA-1 · SIU · S13" });
  expect(within(details).getByText("Present")).toBeTruthy();
  expect(within(details).getByText("AA")).toBeTruthy();
  expect(within(details).queryAllByRole("textbox")).toHaveLength(0);
  await user.click(within(details).getByRole("button", { name: "Close" }));

  await user.click(page().getByRole("button", { name: "Edit" }));
  await user.click(await page().findByRole("tab", { name: "Checks" }));
  const table = await page().findByRole("table", { name: "Checks" });
  await user.click(within(table).getByRole("button", { name: "More actions for Record count" }));
  await user.click(await screen.findByRole("menuitem", { name: "Remove check" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["ACK MSA-1 · SIU · S13"]));
  await user.click(page().getByRole("button", { name: "Undo" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Checks" })).map((row) => row[0])).toEqual(["Record count", "ACK MSA-1 · SIU · S13"]));
});
