// Reports (#559): the library of named reports, a report made from runs with
// no packet, specification file or output path, the report read as a
// document with values withheld until shown, the exact output reviewed
// before an export writes it, and edits that keep the runs. Fixtures carry
// names, states and synthetic tokens only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, CatalogQuery, ReportView } from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, caseCatalogItem, catalogOfListing, folderChosen, GRID_OCCURRENCE, NEXT_OCCURRENCE, WORKSPACE_ROOT } from "./testkit/fixtures";
import { goTo, page, sidebar } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);

function item(kind: CatalogItem["ref"]["kind"], id: string, name: string, summary: CatalogItem["summary"] = {}, extra: Partial<CatalogItem> = {}): CatalogItem {
  return { ref: { kind, id }, name, created_at: null, updated_at: null, last_opened_at: null, availability: "available", capabilities: [], summary, ...extra };
}

const FAILED = item("run", "r-failed", "Reschedule keeps one appointment", {
  run: { kind: "test", started_at: "2026-01-02T12:14:00Z", completed_at: "2026-01-02T12:14:02Z", uncertain: 0, delivery_uncertain: false, active: false, result: "failed", entry: "job-002" },
});
const PASSED = item("run", "r-passed", "Reschedule keeps one appointment", {
  run: { kind: "test", started_at: "2026-01-01T09:05:00Z", completed_at: "2026-01-01T09:05:01Z", uncertain: 0, delivery_uncertain: false, active: false, result: "passed", entry: "job-001" },
});
const REGRESSION = item("report", "rep-regression", "Reschedule regression", { report: { form: "report", related_case: CASE.ref, status: "draft" } }, { updated_at: "2026-01-03T10:00:00Z" });
const RELEASE = item("report", "rep-release", "Scheduling release comparison", { report: { form: "report", related_case: CASE.ref, status: "reviewed" } }, { updated_at: "2026-01-01T10:00:00Z" });

const S12 = { id: GRID_OCCURRENCE, kind: "message" as const, message_code: "SIU", trigger_event: "S12", sendable: true };
const S13 = { id: NEXT_OCCURRENCE, kind: "message" as const, message_code: "SIU", trigger_event: "S13", sendable: true };

function view(overrides: Partial<ReportView> = {}): ReportView {
  const result = { outcome: "failed" as const, status: "assertion_failure", error_class: "", run_state: "no-run-state", journal_incomplete: false, delivery_uncertain: false };
  return {
    item: REGRESSION,
    form: "report",
    revision: "2",
    current: true,
    title: "Reschedule regression",
    result,
    runs: [
      { role: "current", test: "Reschedule keeps one appointment", result, started_at: FAILED.summary.run!.started_at, completed_at: FAILED.summary.run!.completed_at, boundary: "appointment-ledger",
        result_identity: "result-token-1", spec_identity: "spec-token-1", case_identity: "case-token-1", case_provenance: "imported", target_identity: "target-token-1", run: FAILED.ref, case: CASE.ref },
      { role: "comparison", test: "Reschedule keeps one appointment", result: { ...result, outcome: "passed", status: "pass" }, started_at: PASSED.summary.run!.started_at, completed_at: PASSED.summary.run!.completed_at,
        boundary: "appointment-ledger", result_identity: "result-token-2", spec_identity: "spec-token-1", case_identity: "case-token-1", case_provenance: "imported", target_identity: "target-token-1", run: PASSED.ref, case: CASE.ref },
    ],
    checks: [
      { check: { id: "count", operator: "ledger_count", count: 1 }, result: "failed", observed: { count: 2 }, hidden: false, messages: [GRID_OCCURRENCE, NEXT_OCCURRENCE] },
      { check: { id: "after", operator: "ledger_count", count: 0 }, result: "not_evaluated", hidden: false, unavailable: "the appointment records after the run were not observed", messages: [] },
      { check: { id: "ack", operator: "ack_field_equals", message: GRID_OCCURRENCE, selector: "MSA-1", field: { state: "present" } }, result: "passed", observed: { field: { state: "present" } }, hidden: true, messages: [GRID_OCCURRENCE] },
    ],
    comparison: {
      same_case: true,
      same_target: true,
      specification: "unchanged",
      checks: [
        { check: { id: "count", operator: "ledger_count" }, definition: "unchanged", before: "passed", after: "failed",
          before_observed: { count: 1 }, after_observed: { count: 2 }, before_available: true, after_available: true },
        { check: { id: "new", operator: "ledger_equals" }, definition: "added", before: "excluded", after: "passed",
          after_observed: {}, after_records: 1, before_available: false, after_available: true },
      ],
    },
    messages: [
      { source: GRID_OCCURRENCE, message: S12, delivery: "acknowledged", ack_code: "AA" },
      { source: NEXT_OCCURRENCE, message: S13, delivery: "acknowledged", ack_code: "AA" },
    ],
    notes: "Seen in QA.",
    limitations: ["Contains original source values; it is not de-identified and no disclosure is approved."],
    evidence: [{ kind: "case", role: "current", source: "", path: "case/manifest.json", sha256: "digest-token" }],
    packet: "packet-token",
    versions: [
      { revision: "2", title: "Reschedule regression", published_at: "2026-01-03T10:00:00Z", runs: ["Reschedule keeps one appointment", "Reschedule keeps one appointment"], review: "draft", current: true },
      { revision: "1", title: "Reschedule keeps one appointment report", published_at: "2026-01-02T13:00:00Z", runs: ["Reschedule keeps one appointment", "Reschedule keeps one appointment"], review: "reviewed", current: false },
    ],
    review: "draft",
    draft: { title: "Reschedule regression", notes: "Seen in QA.", run: FAILED.ref, comparison: PASSED.ref },
    revealed: false,
    shares: [],
    ...overrides,
  };
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

function listing(facade: FacadeStub, reports: CatalogItem[]) {
  const lists: Record<string, CatalogItem[]> = { report: reports, run: [FAILED, PASSED], case: [CASE] };
  facade.reply({
    ListCatalog: (query: CatalogQuery) =>
      lists[query.kind]
        ? { state: lists[query.kind]!.length ? "completed" : "empty", context: query.context, page: { items: lists[query.kind]!, total: lists[query.kind]!.length, snapshot: "s", recorded: true, incomplete: [] } }
        : catalogOfListing(query, facade),
  });
}

async function openReports(user: User, reports: CatalogItem[], handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" }]),
    ...handlers,
  });
  listing(rendered.facade, reports);
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Reports");
  return rendered;
}

test("Reports lists named reports newest first with their case and review, and filters them", async () => {
  const user = userEvent.setup();
  await openReports(user, [RELEASE, REGRESSION]);
  const table = await page().findByRole("table", { name: "Reports" });
  expect(rowsOf(table).map((row) => [row[0], row[1], row[3]])).toEqual([
    ["Reschedule regression", CASE_ENTRY, "Draft"],
    ["Scheduling release comparison", CASE_ENTRY, "Reviewed"],
  ]);
  expect(page().queryByRole("textbox")).toBeNull();
  for (const gone of ["Share", "Export review", "Packets", "Verify packet"]) expect(page().queryByRole("button", { name: gone })).toBeNull();
  await user.click(page().getByRole("button", { name: "Filter reports" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter reports" });
  await user.click(within(sheet).getByRole("checkbox", { name: "Reviewed" }));
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Reports" })).map((row) => row[0])).toEqual(["Scheduling release comparison"]));
  await user.click(page().getByRole("button", { name: "Search reports" }));
  const search = await screen.findByRole("dialog", { name: "Search reports" });
  await user.type(within(search).getByLabelText("Search"), "nothing like it");
  await user.click(within(search).getByRole("button", { name: "Search" }));
  expect(await page().findByText("No matching reports")).toBeTruthy();
  await user.click(page().getAllByRole("button", { name: "Clear filters" }).at(-1)!);
  expect(rowsOf(await page().findByRole("table", { name: "Reports" }))).toHaveLength(2);
});

test("New report creates a report from a run without a packet, specification file or output path", async () => {
  const user = userEvent.setup();
  const { facade } = await openReports(user, [], {
    SaveItem: (request) =>
      request.draft.report?.comparison?.id === FAILED.ref.id
        ? { state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "report.comparison", problem: "Choose a different run to compare with" }] }
        : { state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { kind: "report", id: "rep-new", revision: "1" } },
    OpenReport: (request) => ({ state: "completed", context: request.context, report: view({ item: { ...REGRESSION, ref: { kind: "report", id: "rep-new" } }, title: "Reschedule keeps one appointment report" }) }),
  });
  expect(await page().findByText("No reports yet")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "New report" }));
  const sheet = await screen.findByRole("dialog", { name: "New report" });
  for (const gone of [/packet/i, /historical/i, /folder/i, /output/i, /specification/i]) expect(within(sheet).queryByLabelText(gone)).toBeNull();
  await user.selectOptions(within(sheet).getByLabelText("Run"), FAILED.ref.id);
  expect((within(sheet).getByLabelText("Name") as HTMLInputElement).value).toBe("Reschedule keeps one appointment report");
  await user.type(within(sheet).getByLabelText("Notes"), "Seen in QA.");
  await user.click(within(sheet).getByRole("button", { name: "Create" }));
  const saved = facade.oneCall("SaveItem")[0];
  expect(saved).toMatchObject({ kind: "report", draft: { report: { title: "Reschedule keeps one appointment report", notes: "Seen in QA.", run: { kind: "run", id: FAILED.ref.id } } } });
  expect(await page().findByRole("heading", { level: 1, name: "Reschedule keeps one appointment report" })).toBeTruthy();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a comparison the report refuses keeps every entered value and names the field", async () => {
  const user = userEvent.setup();
  const { facade } = await openReports(user, [REGRESSION], {
    SaveItem: (request) => ({ state: "failed", context: request.context, outcome: "invalid", replayed: false, problems: [{ field: "report.comparison", problem: "Choose a different run to compare with" }] }),
  });
  await page().findByRole("table", { name: "Reports" });
  await user.click(page().getByRole("button", { name: "New report" }));
  const sheet = await screen.findByRole("dialog", { name: "New report" });
  await user.selectOptions(within(sheet).getByLabelText("Run"), FAILED.ref.id);
  await user.selectOptions(within(sheet).getByLabelText("Compare with"), PASSED.ref.id);
  await user.clear(within(sheet).getByLabelText("Name"));
  await user.type(within(sheet).getByLabelText("Name"), "Kept name");
  await user.click(within(sheet).getByRole("button", { name: "Create" }));
  expect(await within(sheet).findByText("Choose a different run to compare with")).toBeTruthy();
  expect((within(sheet).getByLabelText("Name") as HTMLInputElement).value).toBe("Kept name");
  expect((within(sheet).getByLabelText("Compare with") as HTMLSelectElement).value).toBe(PASSED.ref.id);
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
});

test("a report reads as a document: result, failed checks first, comparison, messages and notes, with values withheld until shown", async () => {
  const user = userEvent.setup();
  const { facade } = await openReports(user, [REGRESSION], {
    OpenReport: (request) => ({
      state: "completed",
      context: request.context,
      report: request.reveal
        ? view({ revealed: true, checks: view().checks.map((check) => (check.check.id === "ack" ? { ...check, hidden: false, check: { ...check.check, field: { state: "present", text: "AA" } }, observed: { field: { state: "present", text: "AA" } } } : check)) })
        : view(),
    }),
  });
  await user.click(await page().findByRole("row", { name: /Reschedule regression/ }));
  await user.keyboard("{Enter}");
  expect(await page().findByRole("heading", { level: 1, name: "Reschedule regression" })).toBeTruthy();
  const headings = page().getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent);
  expect(headings.slice(0, 5)).toEqual(["Result", "Checks", "Comparison", "Messages", "Notes"]);
  expect(page().getByText("Failed", { selector: ".report-result" })).toBeTruthy();
  expect(rowsOf(page().getByRole("table", { name: "Checks" }))).toEqual([
    ["Record count", "1", "2", "Failed"],
    ["Record count", "0", "Unavailable", "Not evaluated"],
    ["ACK MSA-1 · SIU · S12", "Hidden", "Hidden", "Passed"],
  ]);
  expect(rowsOf(page().getByRole("table", { name: "Compared checks" }))).toEqual([["Record count", "Passed · 1", "Failed · 2"]]);
  expect(rowsOf(page().getByRole("table", { name: "Changed definitions" }))).toEqual([["Exact records", "Added", "Not in this run", "Passed · 1 record"]]);
  expect(page().getByText("Seen in QA.")).toBeTruthy();
  expect(page().getByText("May contain patient data.")).toBeTruthy();
  for (const action of ["Export", "Share"]) expect(page().getByRole("button", { name: action })).toBeTruthy();
  expect(page().queryByText("result-token-1")).toBeNull();

  await user.click(page().getByRole("button", { name: "Show values" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Checks" }))[2]).toEqual(["ACK MSA-1 · SIU · S12", "AA", "AA", "Passed"]));
  expect(facade.callsTo("OpenReport").at(-1)!.args[0]).toMatchObject({ reveal: true });

  await user.click(page().getByRole("button", { name: "More report actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Details" }));
  expect(await page().findByText("result-token-1")).toBeTruthy();
  expect(page().getByText(view().limitations[0]!)).toBeTruthy();
  await user.click(page().getByRole("button", { name: "More report actions" }));
  await user.click(screen.getByRole("menuitem", { name: "History" }));
  const history = await screen.findByRole("dialog", { name: "History" });
  expect(rowsOf(within(history).getByRole("table", { name: "Versions" })).map((row) => [row[0], row[1], row.at(-1)])).toEqual([
    ["2 · Current", "Reschedule regression", "Draft"],
    ["1", "Reschedule keeps one appointment report", "Reviewed"],
  ]);
});

test("editing the title publishes a new version of the same runs, and a failed save keeps the entered text", async () => {
  const user = userEvent.setup();
  let fail = true;
  const { facade } = await openReports(user, [REGRESSION], {
    OpenReport: (request) => ({ state: "completed", context: request.context, report: fail ? view() : view({ title: "Duplicate booking", revision: "3" }) }),
    SaveItem: (request) =>
      fail
        ? { state: "failed", context: request.context, outcome: "conflict", replayed: false, problems: [], reason: "the object changed since this edit began; nothing was saved and the draft is kept", current_revision: "3" }
        : { state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { kind: "report", id: REGRESSION.ref.id, revision: "3" } },
  });
  await user.click(await page().findByRole("row", { name: /Reschedule regression/ }));
  await user.keyboard("{Enter}");
  await page().findByRole("heading", { level: 1, name: "Reschedule regression" });
  await user.click(page().getByRole("button", { name: "More report actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Edit title" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit title" });
  await user.clear(within(sheet).getByLabelText("Title"));
  await user.type(within(sheet).getByLabelText("Title"), "Duplicate booking");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText(/nothing was saved and the draft is kept/)).toBeTruthy();
  expect((within(sheet).getByLabelText("Title") as HTMLInputElement).value).toBe("Duplicate booking");
  expect(facade.oneCall("SaveItem")[0]).toMatchObject({ kind: "report", item: REGRESSION.ref.id, base_revision: "2", draft: { report: { title: "Duplicate booking", run: FAILED.ref, comparison: PASSED.ref } } });
  fail = false;
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await page().findByRole("heading", { level: 1, name: "Duplicate booking" })).toBeTruthy();
});

test("a report whose evidence cannot be verified stays named with its reason and Retry", async () => {
  const user = userEvent.setup();
  let broken = true;
  const { facade } = await openReports(user, [REGRESSION], {
    OpenReport: (request) =>
      broken ? { state: "failed", context: request.context, reason: "invalid, incomplete, changed or unsupported retained packet" } : { state: "completed", context: request.context, report: view() },
  });
  await user.click(await page().findByRole("row", { name: /Reschedule regression/ }));
  await user.keyboard("{Enter}");
  expect(await page().findByRole("heading", { level: 1, name: "Reschedule regression" })).toBeTruthy();
  expect(await page().findByText("invalid, incomplete, changed or unsupported retained packet")).toBeTruthy();
  expect(page().queryByRole("table", { name: "Checks" })).toBeNull();
  expect(page().queryByRole("button", { name: "Export" })).toBeNull();
  broken = false;
  await user.click(page().getByRole("button", { name: "Retry" }));
  expect(await page().findByRole("table", { name: "Checks" })).toBeTruthy();
  expect(facade.callsTo("OpenReport")).toHaveLength(2);
});

test("Mark reviewed records the version shown, and an export alone never makes a report reviewed", async () => {
  const user = userEvent.setup();
  let reviewed = false;
  const { facade } = await openReports(user, [REGRESSION], {
    OpenReport: (request) => ({ state: "completed", context: request.context, report: view({ review: reviewed ? "reviewed" : "draft" }) }),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: { token: "review-token", action: "report.review", consent: "approve", items: [REGRESSION], destination: {}, requirements: [], ready: true, report_review: { report: "Reschedule regression", version: "2" } },
    }),
    ExecuteReviewedAction: (request) => {
      reviewed = true;
      return { state: "completed", context: request.context, outcome: "completed", replayed: false, approved: { kind: "report", id: REGRESSION.ref.id, revision: "2" } };
    },
  });
  await user.click(await page().findByRole("row", { name: /Reschedule regression/ }));
  await user.keyboard("{Enter}");
  await page().findByRole("heading", { level: 1, name: "Reschedule regression" });
  await user.click(page().getByRole("button", { name: "More report actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Mark reviewed" }));
  const sheet = await screen.findByRole("dialog", { name: "Mark reviewed" });
  expect(await within(sheet).findByText("Records that this version was reviewed.")).toBeTruthy();
  expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "report.review", items: [REGRESSION.ref] });
  await user.click(within(sheet).getByRole("button", { name: "Mark reviewed" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Mark reviewed" })).toBeNull());
  await user.click(page().getByRole("button", { name: "More report actions" }));
  expect(screen.queryByRole("menuitem", { name: "Mark reviewed" })).toBeNull();
});
