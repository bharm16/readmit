// Runs (#555): history with the running run first, the one reviewed send
// that alone sends, a run's own page with its checks, messages, details and
// recovery, a check group decided against a run, and two runs compared.
// Fixtures carry names, states and synthetic tokens only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ActionReview, CatalogItem, CatalogQuery, ReviewedActionResult, RunDetail, RunSummary } from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, caseCatalogItem, caseResult, catalogOfListing, folderChosen, folderWithCase, GRID_OCCURRENCE, messageRow, messagesResult, NEXT_OCCURRENCE, WORKSPACE_ROOT } from "./testkit/fixtures";
import { findCaseRow, findMessageRow, goTo, page, sidebar } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);

function item(kind: CatalogItem["ref"]["kind"], id: string, name: string, summary: CatalogItem["summary"] = {}, revision?: string): CatalogItem {
  return { ref: { kind, id, ...(revision ? { revision } : {}) }, name, created_at: null, updated_at: null, last_opened_at: null, availability: "available", capabilities: [], summary };
}

const QA = item("environment", "env-qa", "Scheduling QA", {}, "3");
const TEST = item("test", "t-reschedule", "Reschedule keeps one appointment", { test: { source_case: CASE.ref, latest_run: null, assertions: 2, current_version: "4", entry: "t-reschedule.json" } }, "4");
const SUITE = item("suite", "s-smoke", "Scheduling smoke", { suite: { tests: 2, runnable: true, environments: ["Scheduling QA"], latest_run: null } }, "2");
const GROUP = item("check-group", "g-reschedule", "Reschedule checks", { check_group: { assertions: 2, unsupported: 0 } }, "2");

/** An instant today at the given hour and minute, local time. */
function today(hour: number, minute: number, seconds = 0): string {
  const at = new Date();
  at.setHours(hour, minute, seconds, 0);
  return at.toISOString();
}

function run(id: string, name: string, summary: Partial<RunSummary>): CatalogItem {
  return item("run", id, name, { run: { started_at: null, completed_at: null, uncertain: 0, delivery_uncertain: false, active: false, ...summary } });
}

const FAILED = run("r-failed", "Reschedule keeps one appointment", {
  kind: "test", test: { kind: "test", id: TEST.ref.id, revision: "4" }, version: "4", environment: { kind: "environment", id: QA.ref.id, revision: "3" },
  environment_name: "Scheduling QA", started_at: today(12, 14), completed_at: today(12, 14, 2), result: "failed", entry: "job-002",
});
const PASSED = run("r-passed", "Reschedule keeps one appointment", {
  kind: "test", test: { kind: "test", id: TEST.ref.id, revision: "3" }, version: "3", environment_name: "Scheduling QA", started_at: today(9, 5), completed_at: today(9, 5, 1), result: "passed", entry: "job-001",
});
const SMOKE_RUN = run("r-smoke", "Scheduling smoke", { kind: "suite", suite: SUITE.ref, version: "2", environment_name: "Scheduling QA", started_at: today(11, 40), completed_at: today(11, 41), result: "passed" });
const UNCERTAIN = run("r-uncertain", "Reschedule keeps one appointment", { kind: "test", version: "4", environment_name: "Staging", started_at: "2026-01-02T08:00:00Z", result: "incomplete", delivery_uncertain: true });
const ACTIVE = run("r-active", "Booking receives ACK", { kind: "test", version: "1", environment_name: "Scheduling QA", started_at: today(12, 20), active: true, result: "running" });

const S12 = { id: GRID_OCCURRENCE, kind: "message" as const, message_code: "SIU", trigger_event: "S12", sendable: true };
const S13 = { id: NEXT_OCCURRENCE, kind: "message" as const, message_code: "SIU", trigger_event: "S13", sendable: true };

function review(overrides: Partial<ActionReview> = {}, run: Partial<NonNullable<ActionReview["run"]>> = {}): ActionReview {
  return {
    token: "run-token",
    action: "run.test",
    consent: "send",
    items: [TEST],
    destination: { name: "Scheduling QA", classification: "nonproduction", address: "peer-under-test:2575", output: "job-003" },
    requirements: ["setup"],
    ready: true,
    run: {
      kind: "test", name: "Reschedule keeps one appointment", version: "4", test: TEST.ref, environment: QA.ref, environment_name: "Scheduling QA",
      address: "peer-under-test:2575", messages: [S12, S13], message_count: 2, setup: [{ id: "reset", name: "Reset", instructions: "Empty the appointment store" }],
      resets: [], jobs: [], targets: [], environments: [], ...run,
    },
    ...overrides,
  };
}

function detail(overrides: Partial<RunDetail> = {}): RunDetail {
  return {
    item: FAILED,
    name: FAILED.name,
    result: "failed",
    delivery_uncertain: false,
    checks: [
      { check: { id: "count", operator: "ledger_count", count: 1 }, result: "failed", observed: { count: 2 }, hidden: false, messages: [GRID_OCCURRENCE, NEXT_OCCURRENCE] },
      { check: { id: "after", operator: "ledger_count", count: 0 }, result: "not_evaluated", hidden: false, unavailable: "the appointment records after the run were not observed", messages: [] },
      { check: { id: "ack", operator: "ack_field_equals", message: GRID_OCCURRENCE, selector: "MSA-1", field: { state: "present" } }, result: "passed", observed: { field: { state: "present" } }, hidden: true, messages: [GRID_OCCURRENCE] },
    ],
    messages: [
      { source: GRID_OCCURRENCE, message: S12, delivery: "acknowledged", ack_code: "AA" },
      { source: NEXT_OCCURRENCE, message: S13, delivery: "acknowledged", ack_code: "AA" },
    ],
    jobs: [],
    details: {
      address: "peer-under-test:2575", transport: "plain", boundary: "appointment-ledger", initial_records: 0, final_records: 2, journal_incomplete: false, recovered: false,
      started_at: today(12, 14), completed_at: today(12, 14, 2), gaps: [], lifecycle_state: "assertion_failed", status: "assertion_failure", source_case: CASE.ref,
    },
    report: { run: "job-002", case: CASE_ENTRY, case_item: CASE.ref, spec: "t-reschedule-v4.json", test: TEST.ref },
    revealed: false,
    ...overrides,
  };
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

function listing(facade: FacadeStub, runs: CatalogItem[], extra: Partial<Record<string, CatalogItem[]>> = {}) {
  const lists: Record<string, CatalogItem[]> = { run: runs, test: [TEST], suite: [SUITE], environment: [QA], "check-group": [GROUP], ...extra };
  facade.reply({
    ListCatalog: (query: CatalogQuery) =>
      lists[query.kind]
        ? { state: lists[query.kind]!.length ? "completed" : "empty", context: query.context, page: { items: lists[query.kind]!, total: lists[query.kind]!.length, snapshot: "s", recorded: true, incomplete: [] } }
        : catalogOfListing(query, facade),
  });
}

async function openRuns(user: User, runs: CatalogItem[], handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: "t-reschedule-v4.json", kind: "spec" },
        { name: "job-002", kind: "job" },
      ]),
    ...handlers,
  });
  listing(rendered.facade, runs);
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Runs");
  return rendered;
}

/** Run test from Runs: the named picker, then the review of that test. */
async function reviewTest(user: User): Promise<HTMLElement> {
  await user.click(page().getByRole("button", { name: "Run test" }));
  const picker = await screen.findByRole("dialog", { name: "Run test" });
  await user.click(within(picker).getByRole("radio", { name: "Reschedule keeps one appointment · v4" }));
  await user.click(within(picker).getByRole("button", { name: "Continue" }));
  await screen.findByRole("button", { name: "Send" });
  return screen.getByRole("dialog", { name: "Run test" });
}

test("Runs lists what executed, the running run first, each with its own result, and filters it", async () => {
  const user = userEvent.setup();
  await openRuns(user, [UNCERTAIN, PASSED, SMOKE_RUN, FAILED, ACTIVE]);
  const table = await page().findByRole("table", { name: "Runs" });
  const rows = rowsOf(table);
  expect(rows.map((row) => row.slice(1, 2).concat(row.slice(-1)))).toEqual([
    ["Booking receives ACK · v1", "Running"],
    ["Reschedule keeps one appointment · v4", "Failed"],
    ["Scheduling smoke · v2", "Passed"],
    ["Reschedule keeps one appointment · v3", "Passed"],
    ["Reschedule keeps one appointment · v4", "IncompleteDelivery uncertain"],
  ]);
  expect(rows[1]).toContain("Scheduling QA");
  expect(rows[1]!.some((cell) => /^Today \d/.test(cell))).toBe(true);
  expect(rows[1]).toContain("2.0 s");
  expect(page().queryByRole("textbox")).toBeNull();

  await user.click(page().getByRole("button", { name: "Filter runs" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter runs" });
  expect(within(sheet).getByRole("group", { name: new RegExp(`^Started · ${Intl.DateTimeFormat().resolvedOptions().timeZone}`) })).toBeTruthy();
  await user.click(within(sheet).getByRole("checkbox", { name: "Failed" }));
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Runs" })).map((row) => row[1])).toEqual(["Reschedule keeps one appointment · v4"]));
  expect(page().getByRole("group", { name: "Applied filters" }).textContent).toContain("Failed");
  await user.click(page().getByRole("button", { name: "Filter runs" }));
  const again = await screen.findByRole("dialog", { name: "Filter runs" });
  await user.click(within(again).getByRole("checkbox", { name: "Blocked" }));
  await user.click(within(again).getByRole("checkbox", { name: "Failed" }));
  await user.click(within(again).getByRole("button", { name: "Apply" }));
  expect(await page().findByText("No matching runs")).toBeTruthy();
  await user.click(page().getAllByRole("button", { name: "Clear filters" })[0]!);
  expect(rowsOf(await page().findByRole("table", { name: "Runs" }))).toHaveLength(5);
});

test("an empty history offers Run test, and nothing sends until the review's Send", async () => {
  const user = userEvent.setup();
  const { facade } = await openRuns(user, []);
  expect(await page().findByText("No runs yet")).toBeTruthy();
  expect(page().getAllByRole("button", { name: "Run test" }).length).toBeGreaterThan(0);
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("Run test reviews the exact version, environment, messages and setup, sends once on Send and opens the running run, which Stop stops", async () => {
  const user = userEvent.setup();
  const { facade } = await openRuns(user, [FAILED], {
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review() }),
    DurableRunProgress: () => ({ state: "completed", progress: { executing: true, phase: "executing", acknowledged: 1, uncertain: 0, not_attempted: 1 } }),
    CancelOperation: async () => {},
    OpenRun: (request) => ({ state: "completed", context: request.context, run: detail() }),
  });
  const sheet = await reviewTest(user);
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "run.test", items: [{ kind: "test", id: TEST.ref.id, revision: "4" }] });
  const values = within(await within(sheet).findByRole("group", { name: "Review" }).catch(() => sheet));
  expect(values.getByText("Reschedule keeps one appointment · v4")).toBeTruthy();
  expect(values.getByText("Scheduling QA · peer-under-test:2575")).toBeTruthy();
  expect(values.getByText("SIU · S12, SIU · S13")).toBeTruthy();
  expect(within(sheet).getByText("Empty the appointment store")).toBeTruthy();
  expect(within(sheet).getByText("Sends 2 messages to Scheduling QA once.")).toBeTruthy();
  const send = within(sheet).getByRole("button", { name: "Send" }) as HTMLButtonElement;
  expect(send.disabled).toBe(true);
  await user.click(within(sheet).getByRole("button", { name: "Show all" }));
  expect(within(within(sheet).getByRole("list", { name: "Messages in send order" })).getAllByRole("listitem").map((entry) => entry.textContent)).toEqual(["SIU · S12", "SIU · S13"]);
  await user.click(within(sheet).getByRole("checkbox", { name: "Mark complete" }));
  expect(send.disabled).toBe(false);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(facade.callsTo("DurableRunProgress")).toHaveLength(0);

  const sending = facade.park("ExecuteReviewedAction");
  await user.click(send);
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  const execute = facade.oneCall("ExecuteReviewedAction")[0];
  expect(execute).toMatchObject({ token: "run-token", decisions: { confirmed: ["reset"] } });
  // The run's folder is named in the session before the Send reaches the facade.
  const order = facade.calls.map((call) => call.method);
  expect(order.lastIndexOf("RecordView")).toBeLessThan(order.indexOf("ExecuteReviewedAction"));
  expect(facade.callsTo("RecordView").some((call) => (call.args[0] as { run: string }).run === `${WORKSPACE_ROOT}/job-003`)).toBe(true);

  expect(await page().findByRole("heading", { level: 1, name: "Reschedule keeps one appointment" })).toBeTruthy();
  expect(page().getByText("Running · Scheduling QA · peer-under-test:2575")).toBeTruthy();
  expect(await page().findByText("1 of 2 messages")).toBeTruthy();
  expect(page().queryByRole("table", { name: "Checks" })).toBeNull();
  const indicator = within(sidebar().getByRole("status"));
  expect(indicator.getByRole("button", { name: "Running Reschedule keeps one appointment" })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Stop" }));
  expect(facade.oneCall("CancelOperation")[0]).toBe(execute!.intent_id);

  sending.resolve({ state: "cancelled", context: execute!.context, outcome: "cancelled", replayed: false, operation: execute!.intent_id, run: { kind: "run", id: FAILED.ref.id } } satisfies ReviewedActionResult);
  await waitFor(() => expect(facade.callsTo("OpenRun").length).toBeGreaterThan(0));
  expect(facade.callsTo("OpenRun")[0]!.args[0]).toMatchObject({ run: { kind: "run", id: FAILED.ref.id } });
  await page().findByRole("table", { name: "Checks" });
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1);
});

test("a production environment is refused with Edit environment, and an inactive license with Activate that reviews again afterwards", async () => {
  const user = userEvent.setup();
  let refusal: "environment" | "license" = "environment";
  let activated = false;
  const { facade } = await openRuns(user, [FAILED], {
    LicenseStatus: () => ({ state: "empty" }),
    ChooseLicenseFile: () => ({ state: "completed", path: "/private/received/license.json", name: "license.json" }),
    ReviewLicense: () => ({ state: "completed", entitlement: "/private/received/license.json", trust: "/private/received/keys.json", digest: "d1", renewal: false, choose_keys: false,
      document: { version: "readmit-entitlement/v2", id: "ENT-0002", organization: "example-hospital", plan: "annual", sequence: 1,
        not_before: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z", assignments: [{ author: "alice", devices: ["laptop"] }], operation_capable: true } }),
    ActivateLicense: () => {
      activated = true;
      return { state: "completed", outcome: "activated", license: {
        document_id: "ENT-0002", organization: "example-hospital", plan: "annual", sequence: 1, author_seats: 1, runner_slots: 0, author: "alice", device: "laptop",
        starts: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z", grace_ends: "2027-09-18T00:00:00Z", term: "active", days_left: 300,
        renew_soon: false, activated: "2026-09-29T00:00:00Z", deactivated: false, new_work: true, current_format: true, clock_rollback: false,
      } };
    },
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: review(
        activated ? {} : { ready: false, token: "", refusal: refusal === "environment" ? "Scheduling QA is a production environment; runs never send to one" : "the operation term has expired" },
        activated ? {} : { refusal },
      ),
    }),
    WithdrawReview: () => ({ state: "completed", context: { project: "", generation: 0 } }),
  });
  let sheet = await reviewTest(user);
  expect(within(sheet).getByRole("alert").textContent).toBe("Scheduling QA is a production environment; runs never send to one");
  expect((within(sheet).getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(within(sheet).getByRole("button", { name: "Edit environment" }));
  expect(await page().findByRole("heading", { level: 1, name: "Scheduling QA" })).toBeTruthy();

  refusal = "license";
  await goTo(user, "Runs");
  await goTo(user, "Runs");
  sheet = await reviewTest(user);
  expect(within(sheet).getByRole("alert").textContent).toBe("the operation term has expired");
  await user.click(within(sheet).getByRole("button", { name: "Activate" }));
  expect(await page().findByRole("heading", { level: 1, name: "Settings" })).toBeTruthy();
  expect(page().getByRole("button", { name: "License" }).getAttribute("aria-current")).toBe("page");
  const prepared = facade.callsTo("PrepareAction").length;
  const activation = within(await screen.findByRole("dialog", { name: "Activate license" }));
  await user.click(activation.getByRole("button", { name: "Choose file" }));
  await user.click(await activation.findByRole("button", { name: "Continue" }));
  await user.click(await activation.findByRole("button", { name: "Continue" }));
  await user.click(await activation.findByRole("button", { name: "Activate" }));
  // Successful activation returns to a fresh review and restores no consent.
  await screen.findByRole("button", { name: "Send" });
  expect(facade.callsTo("PrepareAction").length).toBe(prepared + 1);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a Send whose review went stale sends nothing and Review again prepares a fresh review; Cancel withdraws a review", async () => {
  const user = userEvent.setup();
  const { facade } = await openRuns(user, [FAILED], {
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review({}, { setup: [] }) }),
    ExecuteReviewedAction: (request) => ({ state: "failed", context: request.context, outcome: "stale", replayed: false, reason: "what this review showed has changed; look at it again before acting. Nothing was done" }),
    DurableRunProgress: () => ({ state: "empty", reason: "no run is retained at that entry yet" }),
    WithdrawReview: () => ({ state: "completed", context: { project: "", generation: 0 } }),
  });
  let sheet = await reviewTest(user);
  await user.click(within(sheet).getByRole("button", { name: "Send" }));
  expect(await page().findByText("Nothing was sent")).toBeTruthy();
  expect(page().getByRole("alert").textContent).toContain("what this review showed has changed");
  await user.click(page().getByRole("button", { name: "Review again" }));
  sheet = await screen.findByRole("dialog", { name: "Run test" });
  await within(sheet).findByRole("button", { name: "Send" });
  expect(facade.callsTo("PrepareAction")).toHaveLength(2);
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(facade.callsTo("WithdrawReview").map((call) => call.args[0])).toContain("run-token"));
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1);
});

test("a suite is reviewed in the wide sheet with each test's dataset, target, state sharing and dependencies, an unknown count is never invented, and Send runs it once", async () => {
  const user = userEvent.setup();
  const { facade } = await openRuns(user, [], {
    DurableRunProgress: () => ({ state: "completed", progress: { executing: true, phase: "executing", acknowledged: 0, uncertain: 0, not_attempted: 0, jobs: 2, jobs_done: 1 } }),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: review(
        { action: "run.suite", items: [SUITE], requirements: [] },
        {
          kind: "suite", name: "Scheduling smoke", version: "2", environment_name: "Scheduling QA", site: "Hospital A", messages: [], setup: [],
          targets: [{ environment: QA.ref, name: "Scheduling QA", address: "peer-under-test:2575", classification: "nonproduction" }],
          environments: [{ id: "qa", name: "Scheduling QA" }],
          jobs: [
            { id: "booking", test: "Booking receives ACK", version: "2", dataset: "Scheduling cases", rows: 2, target: "Scheduling QA", state_sharing: "shared", depends_on: [] },
            { id: "reschedule", test: "Reschedule keeps one appointment", version: "4", dataset: "Scheduling cases", rows: 1, target: "Scheduling QA", state_sharing: "isolated", depends_on: ["Booking receives ACK"] },
          ],
        },
      ),
    }),
  });
  await page().findByText("No runs yet");
  await user.click(page().getAllByRole("button", { name: "Run test" })[0]!);
  const picker = await screen.findByRole("dialog", { name: "Run test" });
  await user.click(within(picker).getByRole("radio", { name: "Scheduling smoke · v2" }));
  await user.click(within(picker).getByRole("button", { name: "Continue" }));
  const sheet = await screen.findByRole("dialog", { name: "Run suite" });
  expect(sheet.className).toContain("sheet-wide");
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "run.suite", items: [{ kind: "suite", id: SUITE.ref.id, revision: "2" }] });
  expect(await within(sheet).findByText("Scheduling smoke · v2")).toBeTruthy();
  expect(within(sheet).getByText("Scheduling QA · Hospital A")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Show tests" }));
  const cells = Array.from(within(sheet).getByRole("table", { name: "Suite tests" }).querySelectorAll("tbody tr")).map((row) => Array.from(row.querySelectorAll("td")).map((cell) => cell.textContent));
  expect(cells).toEqual([
    ["Booking receives ACK · v2", "Scheduling cases · 2 rows", "Scheduling QA", "Shared", "—"],
    ["Reschedule keeps one appointment · v4", "Scheduling cases · 1 row", "Scheduling QA", "Isolated", "Booking receives ACK"],
  ]);
  expect(within(sheet).getByText("Sends the selected suite to Scheduling QA once.")).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  const sending = facade.park("ExecuteReviewedAction");
  await user.click(within(sheet).getByRole("button", { name: "Send" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "run-token", decisions: { confirmed: [] } });
  expect(await page().findByRole("heading", { level: 1, name: "Scheduling smoke" })).toBeTruthy();
  expect(await page().findByText("1 of 2 finished")).toBeTruthy();
  sending.resolve({ state: "completed", context: facade.oneCall("ExecuteReviewedAction")[0]!.context, outcome: "completed", replayed: false });
});

test("a run's page shows its failed check first with what it expected and observed, an unavailable observation never as zero, and text only on Show values", async () => {
  const user = userEvent.setup();
  const { facade } = await openRuns(user, [FAILED], {
    OpenRun: (request) => ({
      state: "completed",
      context: request.context,
      run: request.reveal
        ? detail({
            revealed: true,
            checks: detail().checks.map((check) =>
              check.check.id === "ack" ? { ...check, hidden: false, check: { ...check.check, field: { state: "present", text: "AA" } }, observed: { field: { state: "present", text: "AA" } } } : check,
            ),
          })
        : detail(),
    }),
  });
  await user.click(await page().findByText("Reschedule keeps one appointment · v4"));
  expect(await page().findByRole("heading", { level: 1, name: "Reschedule keeps one appointment" })).toBeTruthy();
  expect(page().getByText(/^Failed · Scheduling QA · Today \d/)).toBeTruthy();
  const checks = await page().findByRole("table", { name: "Checks" });
  expect(rowsOf(checks)).toEqual([
    ["Record count", "1", "2", "Failed"],
    ["Record count", "0", "Unavailable", "Not evaluated"],
    ["ACK MSA-1 · SIU · S12", "Hidden", "Hidden", "Passed"],
  ]);
  await user.click(within(checks).getAllByRole("row")[2]!);
  const unavailable = await page().findByRole("region", { name: "Record count" });
  expect(within(unavailable).getByText("the appointment records after the run were not observed")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Show values" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Checks" }))[2]).toEqual(["ACK MSA-1 · SIU · S12", "AA", "AA", "Passed"]));
  expect(facade.callsTo("OpenRun").at(-1)!.args[0]).toMatchObject({ reveal: true });

  await user.click(page().getByRole("tab", { name: "Messages" }));
  expect(rowsOf(await page().findByRole("table", { name: "Messages" }))).toEqual([
    ["SIU · S12", "Acknowledged", "AA"],
    ["SIU · S13", "Acknowledged", "AA"],
  ]);
  await user.click(page().getByRole("tab", { name: "Details" }));
  const details = page().getByRole("tabpanel");
  expect(within(details).getByText("Reschedule keeps one appointment · v4")).toBeTruthy();
  expect(within(details).getByText("peer-under-test:2575")).toBeTruthy();
  expect(within(details).getByText("Appointment records")).toBeTruthy();

  await user.click(page().getByRole("button", { name: "Create report" }));
  expect(await page().findByRole("heading", { level: 1, name: "Reports" })).toBeTruthy();
  const sheet = await screen.findByRole("dialog", { name: "New report" });
  await waitFor(() => expect((within(sheet).getByLabelText("Run") as HTMLSelectElement).value).toBe(FAILED.ref.id));
  expect((within(sheet).getByLabelText("Name") as HTMLInputElement).value).toBe("Reschedule keeps one appointment report");
  expect(within(sheet).queryByLabelText(/Historical test file|Case|folder/)).toBeNull();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("an interrupted run's recovery lists each message's delivery, offers Resume remaining as its own review, and clears a stale lock only on purpose", async () => {
  const user = userEvent.setup();
  const INTERRUPTED = run("r-stopped", "Reschedule keeps one appointment", { kind: "test", version: "4", environment_name: "Scheduling QA", started_at: today(10, 0), result: "incomplete", entry: "job-004" });
  const { facade } = await openRuns(user, [INTERRUPTED], {
    OpenRun: (request) => ({
      state: "completed",
      context: request.context,
      run: detail({
        item: INTERRUPTED,
        result: "incomplete",
        checks: [{ check: { id: "count", operator: "ledger_count", count: 1 }, result: "not_evaluated", hidden: false, unavailable: "the run ended before its checks were evaluated", messages: [] }],
        messages: [
          { source: GRID_OCCURRENCE, message: S12, delivery: "acknowledged", ack_code: "AA" },
          { source: NEXT_OCCURRENCE, message: S13, delivery: "not_attempted" },
        ],
        recovery: { stop_reason: "cancelled", terminal: true, can_resume: false, resume_refusal: "a delivery was acknowledged", lease: "stale", can_clear_lock: true },
      }),
    }),
    ClearStaleRunLock: (request) => ({ state: "completed", context: request.context }),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review({ action: "run.resume" }, { resume: { from: INTERRUPTED.ref, repeated: 1 }, messages: [S13], message_count: 1 }) }),
  });
  await user.click(await page().findByText("Reschedule keeps one appointment · v4"));
  await user.click(await page().findByRole("tab", { name: "Details" }));
  const recovery = await page().findByRole("region", { name: "Recovery" });
  expect(within(recovery).getByText("Ended").nextElementSibling?.textContent).toBe("Stopped");
  expect(within(recovery).getByText("Resume remaining is unavailable: a delivery was acknowledged")).toBeTruthy();
  expect(within(recovery).queryByRole("button", { name: "Resume remaining" })).toBeNull();

  await user.click(page().getByText("Diagnostics"));
  await user.click(page().getByRole("button", { name: "Clear stale lock" }));
  const confirm = await screen.findByRole("dialog", { name: "Clear stale lock" });
  expect(within(confirm).getByText("Left by the ended run")).toBeTruthy();
  expect(facade.callsTo("ClearStaleRunLock")).toHaveLength(0);
  await user.click(within(confirm).getByRole("button", { name: "Clear lock" }));
  await waitFor(() => expect(facade.callsTo("ClearStaleRunLock")).toHaveLength(1));
  expect(facade.oneCall("ClearStaleRunLock")[0]).toMatchObject({ run: { kind: "run", id: INTERRUPTED.ref.id } });

  facade.reply({
    OpenRun: (request) => ({
      state: "completed",
      context: request.context,
      run: detail({
        item: INTERRUPTED,
        result: "incomplete",
        messages: [{ source: GRID_OCCURRENCE, message: S12, delivery: "not_attempted" }, { source: NEXT_OCCURRENCE, message: S13, delivery: "not_attempted" }],
        recovery: { stop_reason: "cancelled", terminal: true, can_resume: true, lease: "released", can_clear_lock: false },
      }),
    }),
  });
  await user.click(page().getByRole("tab", { name: "Checks" }));
  await user.click(page().getByRole("tab", { name: "Details" }));
  await goTo(user, "Runs");
  await user.click(await page().findByText("Reschedule keeps one appointment · v4"));
  await user.click(await page().findByRole("tab", { name: "Details" }));
  await user.click(await within(await page().findByRole("region", { name: "Recovery" })).findByRole("button", { name: "Resume remaining" }));
  const sheet = await screen.findByRole("dialog", { name: "Resume remaining" });
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({ action: "run.resume", items: [{ kind: "run", id: INTERRUPTED.ref.id }] });
  expect(await within(sheet).findByText("Sends 1 message to Scheduling QA once.")).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("Analyze with checks decides a saved check group against the run as its own analysis, leaving the run's result as it was", async () => {
  const user = userEvent.setup();
  const { facade } = await openRuns(user, [FAILED], {
    OpenRun: (request) => ({ state: "completed", context: request.context, run: detail() }),
    AnalyzeRun: (request) => ({
      state: "completed",
      context: request.context,
      group: "Reschedule checks",
      version: "2",
      missing: [],
      explanation: {
        run: "job-002", bundle: "job-002/result/run", verdict: "fail", declared: 1, passed: 0, failed: 1, undecided: 0, skipped: 0, set_name: "Reschedule checks", set_schema: "readmit-assertion-set/v1",
        set_identity: "s", run_schema: "readmit-run/v1", run_state: "completed", run_identity: "r", source_identity: "c", contains_source_values: true, export_policy: "local", target: "t", transport: "plain",
        target_identity: "t", started_at: "", completed_at: "", elapsed: "", messages: [], observations: [],
        assertions: [{ id: "count", operator: "record_count", outcome: "failed", reads: "records after the run", expected: "Hidden", observed: "Hidden", evidence: [] }],
        revealed: false,
      },
    }),
  });
  await user.click(await page().findByText("Reschedule keeps one appointment · v4"));
  await page().findByRole("table", { name: "Checks" });
  await user.click(page().getByRole("button", { name: "More run actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Analyze with checks" }));
  const sheet = await screen.findByRole("dialog", { name: "Analyze with checks" });
  await user.click(await within(sheet).findByRole("radio", { name: "Reschedule checks · v2" }));
  await user.click(within(sheet).getByRole("button", { name: "Analyze" }));
  const analysis = await page().findByRole("region", { name: "Analysis · Reschedule checks · v2" });
  expect(facade.oneCall("AnalyzeRun")[0]).toMatchObject({ run: { kind: "run", id: FAILED.ref.id }, checks: { kind: "check-group", id: GROUP.ref.id, revision: "2" } });
  expect(within(analysis).getByText("Failed · 0 passed, 1 failed, 0 undecided")).toBeTruthy();
  expect(page().getByText(/^Failed · Scheduling QA/)).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("two finished runs of a test are compared earlier to later, a changed check is never a regression, and added runs are counted", async () => {
  const user = userEvent.setup();
  const EXTRA = run("r-extra", "Reschedule keeps one appointment", { kind: "test", test: { kind: "test", id: TEST.ref.id, revision: "4" }, version: "4", environment_name: "Scheduling QA", started_at: today(8, 0), result: "failed" });
  const { facade } = await openRuns(user, [FAILED, PASSED, EXTRA, ACTIVE], {
    CompareRunItems: (request) => ({
      state: "completed",
      context: request.context,
      comparison: {
        earlier: { run: PASSED.ref, name: PASSED.name, version: "3", environment_name: "Scheduling QA", started_at: PASSED.summary.run!.started_at, result: "passed" },
        later: { run: FAILED.ref, name: FAILED.name, version: "4", environment_name: "Scheduling QA", started_at: FAILED.summary.run!.started_at, result: "failed" },
        repeats: request.runs.length > 2 ? [{ run: EXTRA.ref, name: EXTRA.name, version: "4", started_at: EXTRA.summary.run!.started_at, result: "failed" }] : [],
        checks: [
          { check: { id: "count", operator: "ledger_count", count: 1 }, earlier: "passed", later: "failed", earlier_observed: 1, later_observed: 2, change: "changed_check" },
          { check: { id: "ack", operator: "ack_field_equals", selector: "MSA-1", message: GRID_OCCURRENCE }, earlier: "passed", later: "passed", change: "unchanged" },
        ],
        configuration: [
          { part: "input", outcome: "unchanged", parts: [] },
          { part: "target", outcome: "changed", parts: ["address"] },
        ],
        specification: "changed",
        stability: { state: "insufficient_history", runs: 3, passes: 1, failures: 2, errors: 0, incomplete: 0, flaky: [] },
      },
    }),
  });
  const table = await page().findByRole("table", { name: "Runs" });
  const compare = page().getByRole("button", { name: "Compare" }) as HTMLButtonElement;
  expect(compare.disabled).toBe(true);
  await user.click(within(table).getByRole("checkbox", { name: /^Select Booking receives ACK/ }));
  await user.click(within(table).getByRole("checkbox", { name: /^Select Reschedule keeps one appointment · v4 · Today 12/ }));
  // A running run is not a finished one.
  expect(compare.disabled).toBe(true);
  await user.click(within(table).getByRole("checkbox", { name: /^Select Booking receives ACK/ }));
  await user.click(within(table).getByRole("checkbox", { name: /^Select Reschedule keeps one appointment · v3/ }));
  expect(compare.disabled).toBe(false);
  await user.click(compare);
  expect(await page().findByRole("heading", { level: 1, name: "Compare runs" })).toBeTruthy();
  expect(facade.oneCall("CompareRunItems")[0]).toMatchObject({ runs: [{ kind: "run", id: FAILED.ref.id }, { kind: "run", id: PASSED.ref.id }] });
  expect(await page().findByRole("button", { name: /^Reschedule keeps one appointment · v3 · Today/ })).toBeTruthy();
  expect(rowsOf(page().getByRole("table", { name: "Checks" }))).toEqual([
    ["Record count", "1 / Passed", "2 / Failed", "Changed check"],
    ["ACK MSA-1", "Passed", "Passed", "Unchanged"],
  ]);
  await user.click(page().getByRole("tab", { name: "Configuration" }));
  expect(rowsOf(await page().findByRole("table", { name: "Configuration" }))).toEqual([
    ["Messages", "Unchanged", "—"],
    ["Target", "Changed", "address"],
  ]);
  await user.click(page().getByRole("button", { name: "Add runs" }));
  const sheet = await screen.findByRole("dialog", { name: "Add runs" });
  await user.click(within(sheet).getByRole("checkbox", { name: /Today 08:00|Today 8:00/ }));
  await user.click(within(sheet).getByRole("button", { name: "Compare" }));
  await waitFor(() => expect(facade.callsTo("CompareRunItems")).toHaveLength(2));
  expect(facade.callsTo("CompareRunItems")[1]!.args[0]).toMatchObject({ runs: [{ id: FAILED.ref.id }, { id: PASSED.ref.id }, { id: EXTRA.ref.id }] });
  await user.click(await page().findByRole("tab", { name: "Stability" }));
  expect(page().getByText("Not enough runs")).toBeTruthy();
  expect(rowsOf(page().getByRole("table", { name: "Compared runs" }))).toHaveLength(3);
});

test("Send selected opens the same review titled Send messages with exactly the chosen messages, prepared only once an environment is chosen", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => caseResult(),
    ReadMessages: () => messagesResult([messageRow(GRID_OCCURRENCE), messageRow(NEXT_OCCURRENCE)]),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: review(
        { action: "replay.send", items: [CASE, QA], requirements: [] },
        { kind: "send", name: CASE_ENTRY, version: "", messages: [S13], message_count: 1, setup: [] },
      ),
    }),
  });
  listing(facade, []);
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await findCaseRow());
  const row = await findMessageRow(NEXT_OCCURRENCE);
  await user.click(within(row).getByRole("checkbox"));
  await user.click(screen.getByRole("button", { name: "Send selected" }));
  const sheet = await screen.findByRole("dialog", { name: "Send messages" });
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  await user.selectOptions(await within(sheet).findByRole("combobox", { name: "Environment" }), QA.ref.id);
  await waitFor(() => expect(facade.callsTo("PrepareAction")).toHaveLength(1));
  expect(facade.oneCall("PrepareAction")[0]).toMatchObject({
    action: "replay.send",
    items: [{ kind: "case", id: CASE.ref.id }],
    destination: { kind: "environment", id: QA.ref.id },
    replay: { messages: [NEXT_OCCURRENCE], transformations: [] },
  });
  expect(await within(sheet).findByText("Sends 1 message to Scheduling QA once.")).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
