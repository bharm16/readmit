// Suites: the named collection, a saved suite's Tests, Data, Coverage and
// Versions, the one whole-suite editor and its Save, version review with its
// separately scoped approvals, and the handoffs to the run review, schedules
// and CI. Nothing here sends. Fixtures carry names, states and synthetic
// tokens only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type {
  ActionReview,
  CatalogItem,
  CatalogQuery,
  ItemDraftResult,
  ItemRequest,
  PrepareActionRequest,
  SaveItemRequest,
  SuiteContext,
  SuiteDraft,
  SuiteHistoryResult,
  SuiteTestsRequest,
  SuiteTestVersion,
} from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, caseCatalogItem, catalogOfListing, connectedSuiteVersion, folderWithCase, GRID_OCCURRENCE, NEXT_OCCURRENCE } from "./testkit/fixtures";
import { goTo, page } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);
const OTHER_CASE = caseCatalogItem("second-appointment");

function item(kind: CatalogItem["ref"]["kind"], id: string, name: string, summary: CatalogItem["summary"] = {}, revision?: string): CatalogItem {
  return { ref: { kind, id, ...(revision ? { revision } : {}) }, name, created_at: "2026-01-01T09:00:00Z", updated_at: "2026-01-02T09:00:00Z", last_opened_at: null, availability: "available", capabilities: [], summary };
}

const QA = item("environment", "env-qa", "Scheduling QA", {}, "3");
const STAGING = item("environment", "env-staging", "Scheduling staging", {}, "1");
const APPOINTMENTS = item("observation", "obs-appointments", "Appointments", {}, "1");
const RESCHEDULE = item("test", "t-reschedule", "Reschedule keeps one appointment", { test: { source_case: CASE.ref, latest_run: null, assertions: 2, current_version: "4", entry: "t-reschedule.json" } }, "4");
const BOOKING = item("test", "t-booking", "Booking receives ACK", { test: { source_case: CASE.ref, latest_run: null, assertions: 1, current_version: "2", entry: "t-booking.json" } }, "2");

const MESSAGES: SuiteTestVersion["messages"] = [
  { id: GRID_OCCURRENCE, kind: "message", message_code: "SIU", trigger_event: "S12", sendable: true },
  { id: NEXT_OCCURRENCE, kind: "message", message_code: "SIU", trigger_event: "S13", sendable: true },
];

function version(test: CatalogItem, revision: string, overrides: Partial<SuiteTestVersion> = {}): SuiteTestVersion {
  const ledger = test === RESCHEDULE;
  return {
    ref: { kind: "test", id: test.ref.id, revision },
    name: test.name,
    version: revision,
    checks: ledger
      ? [
          { id: "check-1", operator: "ledger_count", count: 1 },
          { id: "check-2", operator: "ack_field_equals", message: NEXT_OCCURRENCE, selector: "MSA-1", field: { state: "present", text: "AA" } },
        ]
      : [{ id: "check-2", operator: "ack_field_equals", message: GRID_OCCURRENCE, selector: "MSA-1", field: { state: "present", text: "AA" } }],
    messages: MESSAGES,
    sequence: ledger ? [GRID_OCCURRENCE, NEXT_OCCURRENCE] : [GRID_OCCURRENCE],
    ledger,
    ...overrides,
  };
}

/** A saved suite of two tests over one dataset of two rows, bound in two
 * environments through two parameters each, with one requirement, one
 * uncovered requirement and one exclusion. */
function suiteDraft(overrides: Partial<SuiteDraft> = {}): SuiteDraft {
  return {
    id: "scheduling-smoke",
    tags: ["smoke"],
    concurrency: 2,
    tests: [
      { id: "booking", test: { kind: "test", id: BOOKING.ref.id, revision: "2" }, dataset: "cases", parameter: "acknowledgements", after: [], isolation: "shared", sequence: [GRID_OCCURRENCE], tags: [] },
      { id: "reschedule", test: { kind: "test", id: RESCHEDULE.ref.id, revision: "4" }, dataset: "cases", parameter: "records", after: ["booking"], isolation: "shared", sequence: [GRID_OCCURRENCE, NEXT_OCCURRENCE], tags: [] },
    ],
    datasets: [
      {
        id: "cases",
        name: "Scheduling cases",
        rows: [
          { id: "one", case: { kind: "case", id: CASE.ref.id } },
          { id: "two", case: { kind: "case", id: OTHER_CASE.ref.id }, expected: { "check-2": { field: { state: "present", text: "AE" } } } },
        ],
      },
    ],
    environments: [
      {
        id: "qa",
        name: "Scheduling QA",
        site: "east",
        bindings: [
          { parameter: "acknowledgements", target: { kind: "environment", id: QA.ref.id } },
          { parameter: "records", target: { kind: "environment", id: QA.ref.id }, observation: { kind: "observation", id: APPOINTMENTS.ref.id } },
        ],
      },
      {
        id: "staging",
        name: "Scheduling staging",
        site: "west",
        bindings: [
          { parameter: "acknowledgements", target: { kind: "environment", id: STAGING.ref.id } },
          { parameter: "records", target: { kind: "environment", id: STAGING.ref.id }, observation: { kind: "observation", id: APPOINTMENTS.ref.id } },
        ],
      },
    ],
    requirements: [
      { id: "booking-accepted", name: "Booking accepted", tests: ["booking"] },
      { id: "persistence", name: "Downstream persistence", tests: [] },
    ],
    exclusions: [{ test: "reschedule", state: "quarantined", reason: "Fixture refuses reschedules intermittently", until: "2026-10-01T00:00:00Z" }],
    ...overrides,
  };
}

const SMOKE = item("suite", "s-smoke", "Scheduling smoke tests", { suite: { tests: 2, environments: ["Scheduling QA", "Scheduling staging"], latest_run: { kind: "run", id: "r-1" }, latest_outcome: "executed", entry: "suite-s-smoke.json", runnable: true } }, "4");

function suiteAnswer(request: ItemRequest, overrides: Partial<ItemDraftResult> = {}, draft = suiteDraft()): ItemDraftResult {
  return {
    state: "completed",
    context: request.context,
    new: false,
    ref: { kind: "suite", id: request.ref.id, revision: "4" },
    draft: { name: SMOKE.name, suite: draft },
    suite: { original: false, read_only: false, runnable: true, tests: [version(BOOKING, "2"), version(RESCHEDULE, "4")] },
    ...overrides,
  };
}

function history(request: ItemRequest, overrides: Partial<SuiteHistoryResult> = {}): SuiteHistoryResult {
  return {
    state: "completed",
    context: request.context,
    versions: [
      { revision: "4", original: false, published_at: "2026-01-04T09:00:00Z", author: "Synthetic author", current: true, approvals: [] },
      { revision: "3", original: false, published_at: "2026-01-03T09:00:00Z", author: "Synthetic author", current: false, approvals: [{ scope: "baseline", revision: "3", actor: "Synthetic reviewer", at: "2026-01-03T10:00:00Z", current: true }] },
    ],
    runs: [{ run: { kind: "run", id: "r-1" }, revision: "4", environment: "Scheduling QA", started_at: "2026-01-04T10:00:00Z", outcome: "executed" }],
    results: [
      { test: "booking", result: "passed", run: { kind: "run", id: "r-1" } },
      { test: "reschedule", result: "failed", run: { kind: "run", id: "r-1" } },
    ],
    ...overrides,
  };
}

function catalog(suites: CatalogItem[]) {
  const lists: Record<string, CatalogItem[]> = { suite: suites, test: [RESCHEDULE, BOOKING], case: [CASE, OTHER_CASE], environment: [QA, STAGING], observation: [APPOINTMENTS] };
  return (query: CatalogQuery, facade: FacadeStub) => {
    const items = lists[query.kind];
    if (items) return { state: items.length > 0 ? ("completed" as const) : ("empty" as const), context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    return catalogOfListing(query, facade);
  };
}

/** The facade a saved suite is read through. */
function suiteHandlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    OpenItemDraft: (request) => suiteAnswer(request),
    SuiteHistory: (request) => history(request),
    SuiteTests: (request: SuiteTestsRequest) => ({ state: "completed", context: request.context, tests: request.tests.map((ref) => version(ref.id === RESCHEDULE.ref.id ? RESCHEDULE : BOOKING, ref.revision ?? "1")) }),
    SuiteCoverage: (request) => ({ state: "empty", context: request.context, reason: "No run of this version yet", denominator: 0, passed: 0, requirements: [], jobs: [] }),
    ...extra,
  };
}

async function openSuites(user: User, suites: CatalogItem[], handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({ SelectWorkspace: () => folderWithCase(), ...handlers });
  rendered.facade.reply({ ListCatalog: (query) => catalog(suites)(query, rendered.facade) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tests");
  await user.click(await page().findByRole("tab", { name: "Suites" }));
  return rendered;
}

async function openSmoke(user: User, handlers: FacadeHandlers = {}) {
  const rendered = await openSuites(user, [SMOKE], suiteHandlers(handlers));
  await user.dblClick(await page().findByText(SMOKE.name));
  await page().findByRole("tab", { name: "Tests", selected: true });
  return rendered;
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

function saved(request: SaveItemRequest, id = "s-new") {
  return { state: "completed" as const, context: request.context, outcome: "saved" as const, saved: { kind: "suite" as const, id, revision: "5" }, replayed: false, problems: [] };
}

function review(request: PrepareActionRequest, overrides: Partial<ActionReview> = {}) {
  return {
    state: "completed" as const,
    context: request.context,
    review: {
      token: `${request.action}-token`,
      action: request.action,
      consent: "approve" as const,
      items: [SMOKE],
      destination: {},
      requirements: ["rationale" as const],
      ready: true,
      suite_approval: {
        scope: "baseline" as const,
        suite: SMOKE.name,
        version: "4",
        actor: "Synthetic reviewer",
        tests: [
          { name: BOOKING.name, version: "2" },
          { name: RESCHEDULE.name, version: "4" },
        ],
        targets: [],
      },
      ...overrides,
    },
  };
}

test("Suites opens a named collection and a new suite is created from saved tests with one Save", async () => {
  const user = userEvent.setup();
  const { facade } = await openSuites(user, [SMOKE], {
    ...suiteHandlers(),
    OpenItemDraft: (request) =>
      request.ref.kind === "test"
        ? { state: "completed", context: request.context, new: false, ref: request.ref, draft: { name: RESCHEDULE.name, test_links: { environment: QA.ref.id, observation: APPOINTMENTS.ref.id } } }
        : suiteAnswer(request),
    SaveItem: (request) => saved(request),
  });
  const table = await page().findByRole("table", { name: "Suites" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(1));
  expect(rowsOf(table)[0]).toEqual([SMOKE.name, "2", "Scheduling QA, Scheduling staging", "Completed"]);
  expect(page().getByRole("heading", { level: 1, name: "Suites" })).toBeTruthy();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  expect(page().queryByRole("tab", { name: "Prepare" })).toBeNull();

  await user.click(page().getByRole("button", { name: "New suite" }));
  const sheet = await screen.findByRole("dialog", { name: "New suite" });
  await user.type(within(sheet).getByLabelText("Name"), "Reschedule regression");
  await user.click(within(sheet).getByRole("checkbox", { name: RESCHEDULE.name }));
  await user.click(within(sheet).getByRole("button", { name: "Create" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.kind).toBe("suite");
  expect(request.item).toBeUndefined();
  expect(request.draft.name).toBe("Reschedule regression");
  const suite = request.draft.suite!;
  // The test is pinned to the version it was added at, over a dataset of its
  // own case, bound where its setup sends with the observation it reads.
  expect(suite.tests).toEqual([expect.objectContaining({ test: { kind: "test", id: RESCHEDULE.ref.id, revision: "4" }, sequence: [GRID_OCCURRENCE, NEXT_OCCURRENCE], isolation: "shared", after: [] })]);
  expect(suite.datasets).toEqual([expect.objectContaining({ name: CASE_ENTRY, rows: [expect.objectContaining({ case: { kind: "case", id: CASE.ref.id } })] })]);
  expect(suite.environments).toEqual([
    expect.objectContaining({ name: QA.name, bindings: [{ parameter: suite.tests[0]!.parameter, target: { kind: "environment", id: QA.ref.id }, observation: { kind: "observation", id: APPOINTMENTS.ref.id } }] }),
  ]);
  expect(suite.concurrency).toBe(1);
});

test("a saved suite opens read-only with Tests, Data, Coverage and Versions and edits every section in one Save", async () => {
  const user = userEvent.setup();
  const { facade } = await openSmoke(user, { SaveItem: (request) => saved(request, SMOKE.ref.id), ItemHistory: (request) => ({ state: "completed", context: request.context, revisions: [] }) });
  expect(page().getByRole("heading", { level: 1, name: `${SMOKE.name} · v4` })).toBeTruthy();
  expect(page().getAllByRole("tab").map((tab) => tab.textContent)).toEqual(["Tests", "Data", "Coverage", "Versions"]);
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  const tests = await page().findByRole("table", { name: "Tests" });
  expect(rowsOf(tests).map((row) => row.slice(0, 4))).toEqual([
    [BOOKING.name, `${QA.name} · ${STAGING.name}`, "Passed", "Open"],
    [RESCHEDULE.name, `${QA.name} · ${STAGING.name}`, "Failed", "Open"],
  ]);
  // Every binding remains available through its real recorded-configuration owner.
  await user.click(page().getByText("Recorded execution bindings",{selector:"summary"}));
  expect(rowsOf(page().getByRole("table", { name: "Environments" }))).toEqual([
    ["Scheduling QA", "acknowledgements", QA.name, "—"],
    ["Scheduling QA", "records", QA.name, APPOINTMENTS.name],
    ["Scheduling staging", "acknowledgements", STAGING.name, "—"],
    ["Scheduling staging", "records", STAGING.name, APPOINTMENTS.name],
  ]);

  await user.click(page().getByRole("tab", { name: "Data" }));
  expect(rowsOf(await page().findByRole("table", { name: "Scheduling cases rows" }))).toEqual([
    [CASE_ENTRY, "—"],
    ["second-appointment", "ACK MSA-1 · SIU · S12 = AE"],
  ]);

  await user.click(page().getByRole("button", { name: "Edit" }));
  await page().findByRole("tab", { name: "Data", selected: true });
  // Data: the first row gains an override; the second keeps its own.
  await user.dblClick(within(await page().findByRole("table", { name: "Datasets" })).getByText("Scheduling cases"));
  const dataset = await screen.findByRole("dialog", { name: "Edit dataset" });
  await user.click(within(dataset).getByRole("button", { name: "Edit Record count for row 1" }));
  const value = await screen.findByRole("dialog", { name: "Expected value" });
  await user.clear(within(value).getByLabelText("Expected count"));
  await user.type(within(value).getByLabelText("Expected count"), "0");
  await user.click(within(value).getByRole("button", { name: "Apply" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Edit dataset" })).getByRole("button", { name: "Apply" }));
  // Tests: one test becomes isolated.
  await user.click(page().getByRole("tab", { name: "Tests" }));
  await user.dblClick(within(await page().findByRole("table", { name: "Tests" })).getByText(BOOKING.name));
  const row = await screen.findByRole("dialog", { name: BOOKING.name });
  await user.click(within(row).getByRole("radio", { name: "Isolated" }));
  await user.click(within(row).getByRole("button", { name: "Apply" }));
  // Coverage: a new requirement.
  await user.click(page().getByRole("tab", { name: "Coverage" }));
  await user.click(page().getByRole("button", { name: "Add requirement" }));
  const requirement = await screen.findByRole("dialog", { name: "Add requirement" });
  await user.type(within(requirement).getByLabelText("Name"), "Reschedule persisted");
  await user.click(within(requirement).getByRole("checkbox", { name: RESCHEDULE.name }));
  await user.click(within(requirement).getByRole("button", { name: "Apply" }));

  expect(page().getAllByRole("button", { name: "Save" })).toHaveLength(1);
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "suite", item: SMOKE.ref.id, base_revision: "4" });
  const suite = request.draft.suite!;
  expect(suite.datasets[0]!.rows).toEqual([
    { id: "one", case: { kind: "case", id: CASE.ref.id }, expected: { "check-1": { count: 0 } } },
    { id: "two", case: { kind: "case", id: OTHER_CASE.ref.id }, expected: { "check-2": { field: { state: "present", text: "AE" } } } },
  ]);
  expect(suite.tests.map((test) => test.isolation)).toEqual(["isolated", "shared"]);
  // Nothing unedited moved: pins, both environments' four bindings, the
  // uncovered requirement and the exclusion are as they were.
  expect(suite.tests.map((test) => test.test.revision)).toEqual(["2", "4"]);
  expect(suite.environments).toEqual(suiteDraft().environments);
  expect(suite.requirements).toEqual([...suiteDraft().requirements, { id: "reschedule-persisted", name: "Reschedule persisted", tests: ["reschedule"] }]);
  expect(suite.exclusions).toEqual(suiteDraft().exclusions);
});

test("an original suite file opens as it is and its first Save publishes a managed version", async () => {
  const user = userEvent.setup();
  const original: SuiteContext = { original: true, read_only: true, runnable: true, tests: [version(BOOKING, "2"), version(RESCHEDULE, "4")] };
  const { facade } = await openSmoke(user, {
    OpenItemDraft: (request) => suiteAnswer(request, { ref: { kind: "suite", id: request.ref.id }, suite: original }),
    SuiteHistory: (request) => history(request, { versions: [{ original: true, published_at: null, current: true, approvals: [] }] }),
    SaveItem: (request) => saved(request, SMOKE.ref.id),
  });
  expect(page().getByRole("heading", { level: 1, name: SMOKE.name })).toBeTruthy();
  await user.click(page().getByRole("tab", { name: "Versions" }));
  expect(rowsOf(await page().findByRole("table", { name: "Versions" }))[0]!.slice(1, 3)).toEqual(["Original", "—"]);
  await user.click(page().getByRole("button", { name: "Edit" }));
  await user.click(await page().findByRole("button", { name: "Suite settings" }));
  const settings = await screen.findByRole("dialog", { name: "Suite settings" });
  await user.clear(within(settings).getByLabelText("Owner"));
  await user.type(within(settings).getByLabelText("Owner"), "Interface team");
  await user.click(within(settings).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  // The first managed version of the same suite: no base revision, and every
  // member the original declared.
  expect(request.item).toBe(SMOKE.ref.id);
  expect(request.base_revision).toBeUndefined();
  expect(request.draft.suite).toEqual({ ...suiteDraft(), owner: "Interface team" });
});

test("dependency cycles, unsupported isolation and partial rows stay visible with targeted errors", async () => {
  const user = userEvent.setup();
  const cyclic = suiteDraft();
  cyclic.tests[0]!.after = ["reschedule"];
  let answer: "invalid" | "saved" = "invalid";
  const { facade } = await openSmoke(user, {
    OpenItemDraft: (request) => suiteAnswer(request, {}, cyclic),
    SaveItem: (request) =>
      answer === "invalid"
        ? {
            state: "completed",
            context: request.context,
            outcome: "invalid",
            replayed: false,
            problems: [
              { field: "tests.1.isolation", problem: "Isolated state is not supported for a test that reads appointment records." },
              { field: "datasets.0.rows.1.case", problem: "This case does not contain the messages its tests send." },
            ],
          }
        : saved(request, SMOKE.ref.id),
    ItemHistory: (request) => ({ state: "completed", context: request.context, revisions: [] }),
  });
  await user.click(page().getByRole("button", { name: "Edit" }));
  const tests = await page().findByRole("table", { name: "Tests" });
  // Both tests of the cycle say so; neither is reordered or dropped, and
  // nothing is saved.
  await waitFor(() => expect(within(tests).getAllByText("These dependencies form a cycle.")).toHaveLength(2));
  expect(rowsOf(tests).map((row) => row[0]!.startsWith(BOOKING.name) || row[0]!.startsWith(RESCHEDULE.name))).toEqual([true, true]);
  await user.click(page().getByRole("button", { name: "Suite settings" }));
  const settings = await screen.findByRole("dialog", { name: "Suite settings" });
  await user.type(within(settings).getByLabelText("Owner"), "Interface team");
  await user.click(within(settings).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  expect(await page().findByText("Fix the problems shown before saving.")).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);

  // Break the cycle; the facade's own refusals land on their rows.
  await user.dblClick(within(tests).getByText(BOOKING.name));
  const row = await screen.findByRole("dialog", { name: BOOKING.name });
  await user.click(within(row).getByRole("checkbox", { name: RESCHEDULE.name }));
  await user.click(within(row).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(await within(page().getByRole("table", { name: "Tests" })).findByText("Isolated state is not supported for a test that reads appointment records.")).toBeTruthy();
  await user.click(page().getByRole("tab", { name: "Data" }));
  expect(await within(await page().findByRole("table", { name: "Scheduling cases rows" })).findByText("This case does not contain the messages its tests send.")).toBeTruthy();
  // The draft is kept whole for a retry.
  answer = "saved";
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  expect((facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest).draft.suite!.datasets[0]!.rows).toHaveLength(2);
});

test("coverage lists every requirement and exclusion and never runs anything", async () => {
  const user = userEvent.setup();
  const { facade } = await openSmoke(user, {
    SuiteCoverage: (request) => ({
      state: "completed",
      context: request.context,
      run: { kind: "run", id: "r-1" },
      environment: "qa",
      at: "2026-01-04T11:00:00Z",
      denominator: 2,
      passed: 1,
      requirements: [
        { id: "booking-accepted", state: "passed" },
        { id: "persistence", state: "uncovered" },
      ],
      jobs: [
        { test: "booking", row: "one", execution: "passed", expired: false, stability: "insufficient_history", eligible: true },
        { test: "reschedule", row: "one", execution: "assertion_failure", exclusion: "quarantined", expired: true, stability: "insufficient_history", eligible: false },
      ],
    }),
  });
  await user.click(page().getByRole("tab", { name: "Coverage" }));
  const requirements = await page().findByRole("table", { name: "Requirements" });
  await waitFor(() => expect(rowsOf(requirements)).toEqual([
    ["Booking accepted", BOOKING.name, "Passed"],
    ["Downstream persistence", "Uncovered", "Uncovered"],
  ]));
  expect(page().getByRole("status").textContent).toMatch(/^1 of 2 requirements passed/);
  const exclusions = rowsOf(page().getByRole("table", { name: "Exclusions" }));
  expect(exclusions[0]!.slice(0, 3)).toEqual([`${RESCHEDULE.name}Expired`, "Quarantined", "Fixture refuses reschedules intermittently"]);
  expect(facade.oneCall("SuiteCoverage")[0]).toMatchObject({ suite: { kind: "suite", id: SMOKE.ref.id, revision: "4" }, previous: [] });
  expect(facade.callsTo("PreflightRun")).toHaveLength(0);
  expect(facade.callsTo("StartSuiteRun")).toHaveLength(0);
});

test("two versions compare with their exact check changes and a first version stands alone", async () => {
  const user = userEvent.setup();
  const { facade } = await openSmoke(user, {
    SuiteReviewers: (context) => ({ state: "failed", context, reason: "not signed in", reviewers: [] }),
    CompareSuiteVersions: (request) =>
      request.from
        ? {
            state: "completed",
            context: request.context,
            from: request.from,
            to: request.to,
            first: false,
            changes: [{ area: "row", subject: "Scheduling cases › second-appointment", earlier: "Present · AA", later: "Present · AE" }],
            tests: [
              {
                test: { kind: "test", id: RESCHEDULE.ref.id },
                name: RESCHEDULE.name,
                from: "3",
                to: "4",
                checks: [{ earlier: { id: "check-1", operator: "ledger_count", count: 2 }, later: { id: "check-1", operator: "ledger_count", count: 1 } }],
                messages: MESSAGES,
                changes: ["checks"],
              },
            ],
          }
        : { state: "completed", context: request.context, to: request.to, first: true, changes: [], tests: [{ test: { kind: "test", id: BOOKING.ref.id }, name: BOOKING.name, to: "2", checks: [{ later: version(BOOKING, "2").checks[0]! }], messages: MESSAGES, changes: [] }] },
  });
  await user.click(page().getByRole("tab", { name: "Versions" }));
  const versions = await page().findByRole("table", { name: "Versions" });
  await waitFor(() => expect(rowsOf(versions)).toHaveLength(2));
  for (const checkbox of within(versions).getAllByRole("checkbox")) await user.click(checkbox);
  await user.click(page().getByRole("button", { name: "Compare" }));
  expect(await page().findByText(`${SMOKE.name} · v3 → v4`)).toBeTruthy();
  expect(facade.oneCall("CompareSuiteVersions")[0]).toMatchObject({ from: "3", to: "4" });
  const checks = page().getAllByRole("table")[0]!;
  expect(rowsOf(checks).length === 0 ? Array.from(checks.querySelectorAll("tbody tr")).map((tr) => Array.from(tr.children).map((cell) => cell.textContent)) : rowsOf(checks)).toEqual([["Record count", "2", "1"]]);
  expect(page().getByText(`${RESCHEDULE.name} · v3 → v4`)).toBeTruthy();
  expect(page().getByText("Present · AE")).toBeTruthy();

  // The first version alone: never a comparison with nothing.
  facade.reply({ SuiteHistory: (request) => history(request, { versions: [history(request).versions[0]!] }) });
  await user.click(page().getByRole("button", { name: "Back to scheduling smoke tests" }));
  await user.click(await page().findByRole("tab", { name: "Versions" }));
  await user.dblClick(await within(await page().findByRole("table", { name: "Versions" })).findByText("v4"));
  expect(await page().findByText(`${SMOKE.name} · v4 · First version`)).toBeTruthy();
  expect(page().queryByText("No changes")).toBeNull();
  expect(page().getByRole("columnheader", { name: "Expected" })).toBeTruthy();
});

test("baseline, team release and environment approvals keep their own scopes and actors", async () => {
  const user = userEvent.setup();
  let signedIn = "";
  const { facade } = await openSmoke(user, {
    SuiteReviewers: (context) =>
      signedIn ? { state: "completed", context, signed_in: signedIn, reviewers: ["reviewer@idp.test"] } : { state: "failed", context, reason: "not signed in", reviewers: [] },
    CompareSuiteVersions: (request) => ({ state: "completed", context: request.context, from: request.from ?? "", to: request.to, first: false, changes: [], tests: [] }),
    PrepareAction: (request) =>
      request.action === "suite.approve-promotion"
        ? review(request, {
            suite_approval: {
              scope: "environment",
              suite: SMOKE.name,
              version: "4",
              actor: "Synthetic reviewer",
              tests: [{ name: BOOKING.name, version: "2" }],
              environment: "Scheduling QA",
              site: "east",
              targets: [{ parameter: "acknowledgements", target: QA.name, version: "3" }],
              target_revision: request.suite_approval?.revision ?? "",
            },
          })
        : request.action === "suite.approve-release"
          ? review(request, { suite_approval: { scope: "release", suite: SMOKE.name, version: "4", actor: "reviewer@idp.test", request: "Requested by analyst@idp.test", tests: [], targets: [] } })
          : review(request),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, suite_approval: { scope: "baseline", actor: "Synthetic reviewer", at: null, current: true } }),
    WithdrawReview: (token) => ({ state: "completed", context: { project: "", generation: 0 }, reason: token }),
  });
  // Local baseline: the local reviewer, a reason, and exactly one approval.
  await user.click(page().getByRole("tab", { name: "Versions" }));
  await user.dblClick(await within(await page().findByRole("table", { name: "Versions" })).findByText("v4"));
  await user.click(await page().findByRole("button", { name: "Approve version" }));
  const baseline = await screen.findByRole("dialog", { name: "Approve baseline" });
  expect(await within(baseline).findByText("Local approval · Synthetic reviewer")).toBeTruthy();
  expect(within(baseline).queryByText(/token|sha256|identity/i)).toBeNull();
  expect(within(baseline).getByRole("button", { name: "Approve" })).toHaveProperty("disabled", true);
  await user.type(within(baseline).getByLabelText("Reason"), "Reviewed the reschedule expectation");
  await user.click(within(baseline).getByRole("button", { name: "Approve" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "suite.approve-baseline", items: [{ kind: "suite", id: SMOKE.ref.id, revision: "4" }] });
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "suite.approve-baseline-token", decisions: { rationale: "Reviewed the reschedule expectation" } });
  // Success closes the sheet; the approval shows in the version's history.
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Approve baseline" })).toBeNull());

  // A team release answers the request the hub holds for the person signed
  // in: the facade is asked, and its probe review is withdrawn.
  signedIn = "reviewer@idp.test";
  await user.click(page().getByRole("button", { name: "Back to scheduling smoke tests" }));
  await user.click(await page().findByRole("tab", { name: "Versions" }));
  await user.dblClick(await within(await page().findByRole("table", { name: "Versions" })).findByText("v4"));
  await waitFor(() => expect(page().getByRole("button", { name: "Request review" })).toHaveProperty("disabled", false));
  await user.click(page().getByRole("button", { name: "Approve version" }));
  expect(facade.callsTo("WithdrawReview").map((call) => call.args[0])).toContain("suite.approve-release-token");
  const release = await screen.findByRole("dialog", { name: "Approve release" });
  expect(await within(release).findByText("Requested by analyst@idp.test")).toBeTruthy();
  expect(within(release).getByText("reviewer@idp.test")).toBeTruthy();
  await user.click(within(release).getByRole("button", { name: "Cancel" }));
  expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "suite.approve-release" });

  // Request review names a team reviewer and a reason.
  await user.click(page().getByRole("button", { name: "Request review" }));
  const request = await screen.findByRole("dialog", { name: "Request review" });
  await user.selectOptions(within(request).getByLabelText("Reviewer"), "reviewer@idp.test");
  await waitFor(() => expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "suite.request-review", suite_approval: { reviewer: "reviewer@idp.test" } }));
  await user.click(within(request).getByRole("button", { name: "Cancel" }));

  // Environment approval is its own sheet: environment, operator-entered
  // target revision and reason; changing a field reviews again.
  await user.click(page().getByRole("button", { name: "Back to scheduling smoke tests" }));
  await user.click(await page().findByRole("button", { name: "More suite actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Approve for environment" }));
  const environment = await screen.findByRole("dialog", { name: "Approve for environment" });
  expect(within(environment).getByText("Records local approval; nothing is deployed.")).toBeTruthy();
  await user.type(within(environment).getByLabelText("Target revision"), "build-7");
  await waitFor(() => expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "suite.approve-promotion", suite_approval: { environment: "qa", revision: "build-7" } }));
  expect(await within(environment).findByText("build-7")).toBeTruthy();
  expect(within(environment).getByText("Local approval · Synthetic reviewer")).toBeTruthy();
  await user.type(within(environment).getByLabelText("Reason"), "QA mapping reviewed");
  await user.click(within(environment).getByRole("button", { name: "Approve" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(2));
  expect(facade.callsTo("ExecuteReviewedAction")[1]!.args[0]).toMatchObject({ token: "suite.approve-promotion-token", decisions: { rationale: "QA mapping reviewed" } });
  expect(facade.callsTo("StartSuiteRun")).toHaveLength(0);
});

test("Run hands one exact suite version and environment to the run review", async () => {
  const user = userEvent.setup();
  const { facade } = await openSmoke(user, { PrepareAction: (request) => ({ state: "failed", context: request.context, reason: "synthetic review refusal" }) });
  await user.click(page().getByRole("button", { name: "Run" }));
  const sheet = await screen.findByRole("dialog", { name: "Run suite" });
  await user.selectOptions(within(sheet).getByLabelText("Environment"), "staging");
  await user.click(within(sheet).getByRole("button", { name: "Continue" }));
  await waitFor(() => expect(facade.callsTo("PrepareAction").some((call) => (call.args[0] as PrepareActionRequest).action === "run.suite")).toBe(true));
  expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "run.suite", items: [{ kind: "suite", id: SMOKE.ref.id, revision: "4" }], run: { environment: "staging" } });
  expect(await screen.findByText("synthetic review refusal")).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("import fills one draft, and export and run configuration write only where the person chooses", async () => {
  const user = userEvent.setup();
  const { facade } = await openSmoke(user, {
    ExportSuiteItem: (request) => ({ state: "completed", context: request.context, output: "/Users/synthetic/Exports/scheduling-smoke-tests.json" }),
    ExportSuiteRunConfiguration: (request) => ({ state: "completed", context: request.context, output: "/Users/synthetic/Exports/qa-run" }),
    ImportSuiteItem: (context) => ({ state: "completed", context, new: true, draft: { name: "Imported smoke", suite: suiteDraft({ id: "imported-smoke" }) }, suite: { original: false, read_only: false, runnable: false, tests: [version(BOOKING, "2"), version(RESCHEDULE, "4")] } }),
    SaveItem: (request) => saved(request),
  });
  await user.click(page().getByRole("button", { name: "More suite actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Export suite" }));
  expect(await page().findByText("Exported scheduling-smoke-tests.json")).toBeTruthy();
  expect(facade.oneCall("ExportSuiteItem")[0]).toMatchObject({ suite: { kind: "suite", id: SMOKE.ref.id, revision: "4" } });

  await user.click(page().getByRole("button", { name: "More suite actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Export run configuration" }));
  const sheet = await screen.findByRole("dialog", { name: "Export run configuration" });
  await user.click(within(sheet).getByRole("button", { name: "Export" }));
  expect(await page().findByText("Exported qa-run")).toBeTruthy();
  expect(facade.oneCall("ExportSuiteRunConfiguration")[0]).toMatchObject({ suite: { kind: "suite", id: SMOKE.ref.id, revision: "4" }, environment: "qa" });

  await user.click(page().getByRole("button", { name: "More suite actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Import suite" }));
  expect(await page().findByRole("heading", { level: 1, name: "New suite" })).toBeTruthy();
  expect(await page().findByText("Imported smoke")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request.item).toBeUndefined();
  expect(request.draft).toEqual({ name: "Imported smoke", suite: suiteDraft({ id: "imported-smoke" }) });
  expect(facade.callsTo("PrepareAction").filter((call) => (call.args[0] as PrepareActionRequest).action.startsWith("run."))).toHaveLength(0);
});

test("Schedule opens this suite's schedules and Set up CI opens its sheet for the exact version", async () => {
  const user = userEvent.setup();
  const { facade } = await openSmoke(user, {
    ListSchedules: (request) => ({ state: "completed", context: request.context, schedules: [] }),
    ListRunners: (context) => ({ state: "completed", context, runners: [] }),
  });
  await user.click(page().getByRole("button", { name: "Schedule" }));
  expect(await page().findByRole("heading", { level: 1, name: "Schedules" })).toBeTruthy();
  await waitFor(() => expect(facade.callsTo("ListSchedules").length).toBeGreaterThan(0));
  expect(facade.callsTo("ListSchedules")[0]!.args[0]).toMatchObject({ suite: SMOKE.ref.id });

  // Tests returns to the suite it was left on.
  await goTo(user, "Tests");
  await user.click(await page().findByRole("button", { name: "More suite actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Set up CI" }));
  const sheet = await screen.findByRole("dialog", { name: "Set up CI" });
  expect(within(sheet).getByText("Version 4")).toBeTruthy();
  expect(within(sheet).getByLabelText("Environment")).toBeTruthy();
  expect(facade.callsTo("SaveCIHandoff")).toHaveLength(0);
});

test("a dependency on a removed test stays listed and can be unticked before Save", async () => {
  const user = userEvent.setup();
  const { facade } = await openSmoke(user, { SaveItem: (request) => saved(request, SMOKE.ref.id), ItemHistory: (request) => ({ state: "completed", context: request.context, revisions: [] }) });
  await user.click(page().getByRole("button", { name: "Edit" }));
  const tests = await page().findByRole("table", { name: "Tests" });
  await user.click(within(tests).getByRole("button", { name: `Actions for ${BOOKING.name}` }));
  await user.click(await screen.findByRole("menuitem", { name: "Remove test" }));
  const remove = await screen.findByRole("dialog", { name: "Remove test" });
  expect(within(remove).getByText(RESCHEDULE.name)).toBeTruthy();
  await user.click(within(remove).getByRole("button", { name: "Remove" }));
  expect(await within(tests).findByText("Depends on a test this suite no longer has.")).toBeTruthy();
  await user.dblClick(within(tests).getByText(RESCHEDULE.name));
  const row = await screen.findByRole("dialog", { name: RESCHEDULE.name });
  const gone = within(row).getByRole("checkbox", { name: "Removed test" }) as HTMLInputElement;
  expect(gone.checked).toBe(true);
  await user.click(gone);
  await user.click(within(row).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(within(tests).queryByText("Depends on a test this suite no longer has.")).toBeNull());
  // The requirement that named it is fixed the same way before Save.
  await user.click(page().getByRole("tab", { name: "Coverage" }));
  expect(await page().findByText("Names a test this suite no longer has.")).toBeTruthy();
  await user.dblClick(within(page().getByRole("table", { name: "Requirements" })).getByText("Booking accepted"));
  const requirement = await screen.findByRole("dialog", { name: "Edit requirement" });
  await user.click(within(requirement).getByRole("checkbox", { name: "Removed test" }));
  await user.click(within(requirement).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const suite = (facade.oneCall("SaveItem")[0] as SaveItemRequest).draft.suite!;
  expect(suite.tests.map((test) => [test.id, test.after])).toEqual([["reschedule", []]]);
  expect(suite.requirements[0]!.tests).toEqual([]);
});

test("an environment list answered busy past its retries is read again, so a binding never names a live environment Removed", async () => {
  const user = userEvent.setup();
  const rendered = await renderApp({ SelectWorkspace: () => folderWithCase(), ...suiteHandlers() });
  const listed = catalog([SMOKE]);
  // Busy for longer than one read's retries, from when the suite is opened.
  let busy = 0;
  rendered.facade.reply({
    ListCatalog: (query) => (query.kind === "environment" && busy-- > 0 ? { state: "busy", reason: "another operation is already running", context: { project: "", generation: 0 } } : listed(query, rendered.facade)),
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tests");
  await user.click(await page().findByRole("tab", { name: "Suites" }));
  await page().findByText(SMOKE.name);
  await waitFor(() => expect(rendered.facade.callsTo("ListCatalog").length).toBeGreaterThan(4));
  busy = 25;
  await user.dblClick(page().getByText(SMOKE.name));
  const environments = await page().findByRole("table", { name: "Environments" });
  // The suite's lists are read again once it opens, the environments past
  // their busy answers.
  const observationsRead = () => rendered.facade.callsTo("ListCatalog").filter((call) => (call.args[0] as CatalogQuery).kind === "observation").length;
  await waitFor(() => expect(observationsRead()).toBe(2), { timeout: 10_000 });
  await waitFor(() => expect(rowsOf(environments)[0]).toEqual(["Scheduling QA", "acknowledgements", QA.name, "—"]), { timeout: 10_000 });
  expect(within(environments).queryByText("Removed")).toBeNull();
}, 20_000);

test("a suite of connected tests binds their FHIR server and runs one over a dataset whose rows override its own expected values", async () => {
  const user = userEvent.setup();
  const FHIR = item("environment", "env-fhir", "Scheduling FHIR", { environment: { classification: "nonproduction", address: "fhir.invalid", transport: "https", transport_approved: true, approval_required: false, last_checked_at: null, observation: null, has_policy: true, reset_actions: 0, protocol: "fhir-r4" } }, "2");
  const CONNECTED = item("test", "t-connected", "Reschedule keeps one record", { test: { source_case: CASE.ref, latest_run: null, assertions: 3, current_version: "3", boundary: "application-state" } }, "3");
  const start = connectedSuiteVersion({ id: "" }, "").connected!.expected[0]!.value;
  const connectedVersion = (ref: { id: string; revision?: string }): SuiteTestVersion => ({ ...connectedSuiteVersion(ref, CONNECTED.name), connected: { ...connectedSuiteVersion(ref, CONNECTED.name).connected!, environment: QA.ref.id, server: FHIR.ref.id } });

  const parameterized: SuiteDraft = {
    id: "scheduling-regression",
    tags: [],
    concurrency: 1,
    tests: [{ id: "reschedule", test: { kind: "test", id: CONNECTED.ref.id, revision: "3" }, dataset: "schedules", parameter: "engine", after: [], isolation: "shared", sequence: [], tags: [] }],
    datasets: [{ id: "schedules", name: "Schedules", rows: [{ id: "on-time", case: { kind: "case", id: CASE.ref.id } }] }],
    environments: [{ id: "qa", name: "QA", site: "", bindings: [{ parameter: "engine", target: { kind: "environment", id: QA.ref.id, revision: "3" }, server: { kind: "environment", id: FHIR.ref.id, revision: "2" } }] }],
    requirements: [],
    exclusions: [],
  };
  const SUITE = item("suite", "s-connected", "Scheduling regression", { suite: { tests: 1, environments: ["QA"], latest_run: null, latest_outcome: "", runnable: false } }, "1");
  const rendered = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ...suiteHandlers(),
    OpenItemDraft: (request) =>
      request.ref.kind === "suite"
        ? suiteAnswer(request, { ref: { kind: "suite", id: SUITE.ref.id, revision: "1" }, draft: { name: SUITE.name, suite: parameterized }, suite: { original: false, read_only: false, runnable: false, tests: [connectedVersion({ id: CONNECTED.ref.id })] } })
        : { state: "completed", context: request.context, new: false, ref: request.ref, draft: { name: CONNECTED.name, test_links: { environment: QA.ref.id, reset: "environment" } } },
    SuiteTests: (request: SuiteTestsRequest) => ({ state: "completed", context: request.context, tests: request.tests.map(connectedVersion) }),
    SaveItem: (request) => saved(request, "s-connected"),
  });
  const lists: Record<string, CatalogItem[]> = { suite: [SUITE], test: [CONNECTED], case: [CASE], environment: [QA, FHIR], observation: [APPOINTMENTS] };
  rendered.facade.reply({
    ListCatalog: (query) => {
      const items = lists[query.kind];
      return items ? { state: "completed" as const, context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, rendered.facade);
    },
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Tests");
  await user.click(await page().findByRole("tab", { name: "Suites" }));

  // A new suite of a connected test runs it once and binds its FHIR server.
  await user.click(await page().findByRole("button", { name: "New suite" }));
  const sheet = await screen.findByRole("dialog", { name: "New suite" });
  await user.type(within(sheet).getByLabelText("Name"), "Scheduling smoke");
  await user.click(within(sheet).getByRole("checkbox", { name: CONNECTED.name }));
  await user.click(within(sheet).getByRole("button", { name: "Create" }));
  await waitFor(() => expect(rendered.facade.callsTo("SaveItem")).toHaveLength(1));
  const created = (rendered.facade.oneCall("SaveItem")[0] as SaveItemRequest).draft.suite!;
  expect(created.datasets).toEqual([]);
  expect(created.tests).toEqual([expect.objectContaining({ test: { kind: "test", id: CONNECTED.ref.id, revision: "3" }, dataset: "", sequence: [] })]);
  expect(created.environments).toEqual([expect.objectContaining({ bindings: [{ parameter: created.tests[0]!.parameter, target: { kind: "environment", id: QA.ref.id }, server: { kind: "environment", id: FHIR.ref.id } }] })]);

  // A row of its dataset overrides one of its own expected values, typed as
  // the field it is expected of.
  await user.click(await page().findByRole("button", { name: "Edit" }));
  await user.click(await page().findByRole("tab", { name: "Data" }));
  await user.dblClick(within(await page().findByRole("table", { name: "Datasets" })).getByText("Schedules"));
  const dataset = await screen.findByRole("dialog", { name: "Edit dataset" });
  expect(within(dataset).getByText("2026-03-02T09:30:00Z (test)")).toBeTruthy();
  await user.click(within(dataset).getByRole("button", { name: "Edit Reschedule · Moved start for row 1" }));
  const value = await screen.findByRole("dialog", { name: "Expected value" });
  expect(within(value).getAllByRole("option").map((option) => option.textContent)).toEqual(["Present", "Not present"]);
  await user.clear(within(value).getByLabelText("Value"));
  await user.type(within(value).getByLabelText("Value"), "2026-03-02T09:45:00Z");
  await user.click(within(value).getByRole("button", { name: "Apply" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Edit dataset" })).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(rendered.facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0));
  expect(rendered.facade.callsTo("SaveEditorDraft").at(-1)!.args[0]).toMatchObject({ kind: "suite-editor", content_schema: "readmit-suite-editor/v2" });
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(rendered.facade.callsTo("SaveItem")).toHaveLength(2));
  const edited = (rendered.facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest).draft.suite!;
  expect(edited.datasets[0]!.rows[0]!.connected_expected).toEqual({ "reschedule/moved-start": { ...start, text: "2026-03-02T09:45:00Z" } });
  expect(edited.environments[0]!.bindings[0]!.target.revision).toBe("3");
});
