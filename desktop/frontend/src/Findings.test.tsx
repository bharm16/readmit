// Findings: the saved analysis of the open case version, one list with the
// selected finding beside it, Analyze with Stop, History, Analysis settings,
// review decisions saved as revisions, the evidence shown in Messages, and
// Similar findings across chosen cases. Fixtures carry positions, rule names
// and states only.
import { afterEach, expect, test, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AnalysisProfile, CatalogItem, FindingDecision, FindingRow, FindingsResult, ItemRequest, SaveItemRequest, SimilarRequest, SimilarResult } from "./bindings";
import { renderApp } from "./testkit/app";
import { windowWidth } from "./testkit/window";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  caseCatalogItem,
  caseResult,
  catalogOfListing,
  diagnosisResult,
  findingPromotion,
  findingRow,
  findingStatus,
  folderChosen,
  folderWithCase,
  GRID_OCCURRENCE,
  inspectionResult,
  messageRow,
  messagesResult,
  NEXT_OCCURRENCE,
  OTHER_CASE_ENTRY,
  REPORT_SHA256,
} from "./testkit/fixtures";
import { details, findCaseRow, findMessageRow, goTo, page } from "./testkit/navigation";
import type { FacadeHandlers } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);
const ANALYSIS_REF = { kind: "analysis" as const, id: "analysis-001" };
const RULES = [
  { id: "ack.msa-outcome", ruleset: "readmit-siu-diagnosis/v1", name: "Unexpected acknowledgement", severity: "error" as const },
  { id: "message.duplicate-control-id", ruleset: "readmit-siu-diagnosis/v1", name: "Repeated control ID", severity: "warning" as const },
];

function findings(ids: string[], overrides: Partial<NonNullable<FindingsResult["analysis"]>> = {}): FindingsResult {
  return findingsOf(
    ids.map((id, index) => findingRow(id, index % 2 === 0 ? "ack.msa-outcome" : "message.duplicate-control-id")),
    overrides,
  );
}

function findingsOf(rows: FindingRow[], overrides: Partial<NonNullable<FindingsResult["analysis"]>> = {}): FindingsResult {
  const diagnosis = { ...diagnosisResult([]).diagnosis!, total: rows.length };
  return {
    state: rows.length > 0 ? "completed" : "empty",
    context: { project: "", generation: 0 },
    analysis: {
      ref: ANALYSIS_REF,
      created_at: "2026-01-02T09:00:00Z",
      profile_name: "SIU",
      report_sha256: REPORT_SHA256,
      config_sha256: "config-siu",
      current: true,
      diagnosis,
      matching: rows.length,
      findings: rows,
      ...overrides,
    },
    rules: RULES,
  };
}

function profile(name: string, compatible = true, builtin = name.toLowerCase()): AnalysisProfile {
  return {
    builtin,
    name,
    profile: `readmit-${builtin}-v1`,
    profile_name: name,
    ruleset: `readmit-${builtin}-diagnosis/v1`,
    config_sha256: `config-${builtin}`,
    compatible,
    refusals: compatible ? [] : [{ code: "unsupported_profile", detail: `The case is not a ${name} case.` }],
  };
}

async function openFindings(user: User, handlers: FacadeHandlers = {}) {
  const rendered = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => caseResult(),
    ReadMessages: () => messagesResult([messageRow(GRID_OCCURRENCE)]),
    FindingReviewHistory: (request) => ({ state: "empty", context: request.context, revisions: [], statuses: [] }),
    ...handlers,
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await goTo(user, "Cases");
  await user.dblClick(await findCaseRow(CASE_ENTRY));
  await user.click(await screen.findByRole("tab", { name: "Findings" }));
  return rendered;
}

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

test("Findings opens the latest analysis of this case version and shows Not analyzed with no setup form when none exists", async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, { OpenCaseFindings: (request) => ({ state: "empty", context: request.context, rules: RULES }) });
  expect(await page().findByText("Not analyzed")).toBeTruthy();
  expect(facade.oneCall("OpenCaseFindings")[0]).toMatchObject({ case: CASE.ref, identity: CASE_IDENTITY, offset: 0 });
  // Opening reads; it never analyzes, and nothing on the page is a form.
  expect(facade.callsTo("AnalyzeCase")).toHaveLength(0);
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  expect(page().queryAllByRole("combobox")).toHaveLength(0);
});

test("a saved analysis lists its findings by rule name with their review state", async () => {
  const user = userEvent.setup();
  await openFindings(user, { OpenCaseFindings: (request) => ({ ...findings(["f000001", "f000002"]), context: request.context }) });
  const table = await page().findByRole("table", { name: "Findings" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  expect(rowsOf(table)[0]!.filter((cell) => cell !== "")).toEqual(["Unexpected acknowledgement", "Error", "New"]);
});

test("a completed analysis with zero findings shows No findings and keeps Analyze", async () => {
  const user = userEvent.setup();
  await openFindings(user, { OpenCaseFindings: (request) => ({ ...findings([]), context: request.context }) });
  expect(await page().findByText("No findings")).toBeTruthy();
  expect(page().getByRole("button", { name: "Analyze" })).toBeTruthy();
});

test("Analyze asks for a profile only when more than one is compatible and refuses one the engine refuses", async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ state: "empty", context: request.context, rules: RULES }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU"), profile("Lifecycle", false)] }),
    AnalyzeCase: (request) => ({ ...findings(["f000001"]), context: request.context }),
  });
  await user.click(await page().findByRole("button", { name: "Analyze" }));
  // One compatible profile: it is used directly, no sheet.
  await waitFor(() => expect(facade.callsTo("AnalyzeCase")).toHaveLength(1));
  expect(screen.queryByRole("dialog", { name: "Analyze" })).toBeNull();
  expect(facade.oneCall("AnalyzeCase")[0]).toMatchObject({ case: CASE.ref, identity: CASE_IDENTITY, profile: { builtin: "siu" } });
  expect(await page().findByRole("table", { name: "Findings" })).toBeTruthy();

  facade.reply({ ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU"), profile("Orders", true, "order"), profile("Lifecycle", false)] }) });
  await user.click(page().getByRole("button", { name: "Analyze" }));
  const sheet = await screen.findByRole("dialog", { name: "Analyze" });
  const choice = within(sheet).getByLabelText("Profile");
  expect(within(choice).getAllByRole("option").map((option) => option.textContent)).toEqual(["SIU", "Orders"]);
  await user.selectOptions(choice, "builtin:order");
  await user.click(within(sheet).getByRole("button", { name: "Analyze" }));
  await waitFor(() => expect(facade.callsTo("AnalyzeCase")).toHaveLength(2));
  expect((facade.callsTo("AnalyzeCase")[1]!.args[0] as { profile: object }).profile).toEqual({ builtin: "order" });

  facade.reply({ ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("Lifecycle", false)] }) });
  await user.click(page().getByRole("button", { name: "Analyze" }));
  const refused = await screen.findByRole("dialog", { name: "Analyze" });
  expect(within(refused).getByText("No supported profile")).toBeTruthy();
  expect(within(refused).getByText("The case is not a Lifecycle case.")).toBeTruthy();
  expect(within(refused).getByRole("button", { name: "Analyze" })).toHaveProperty("disabled", true);
});

test("Stop during Analyze keeps the previous findings read-only and records no report", async () => {
  const user = userEvent.setup();
  let finish: (value: FindingsResult) => void = () => {};
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU")] }),
    AnalyzeCase: (request) => new Promise<FindingsResult>((resolve) => (finish = (value) => resolve({ ...value, context: request.context }))),
    Cancel: async () => finish({ state: "cancelled", context: { project: "", generation: 0 }, rules: RULES }),
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await user.click(within(table).getAllByRole("row")[1]!);
  await user.click(page().getByRole("button", { name: "Analyze" }));
  const stop = await page().findByRole("button", { name: "Stop" });
  // While it runs the previous list is historical and cannot be reviewed.
  expect(table.closest("[aria-busy]")).toBeTruthy();
  expect(details().getByRole("button", { name: "Review" })).toHaveProperty("disabled", true);
  await user.click(stop);
  expect(facade.callsTo("Cancel").at(-1)?.args[0]).toBe("analysis");
  expect(await page().findByText("Analysis stopped. Nothing was saved.")).toBeTruthy();
  expect(rowsOf(table)).toHaveLength(1);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("History lists date, profile and findings newest first and opens an older analysis as not current", async () => {
  const user = userEvent.setup();
  const older: CatalogItem = {
    ref: { kind: "analysis", id: "analysis-000" },
    name: "analysis-000",
    created_at: "2026-01-01T09:00:00Z",
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { analysis: { form: "diagnosis", related_case: CASE.ref, cases: [], findings: 3, profile: "readmit-siu-v1", profile_name: "SIU", unsupported: 0 } },
  };
  const newer: CatalogItem = { ...older, ref: ANALYSIS_REF, name: "analysis-001", created_at: "2026-01-02T09:00:00Z", summary: { analysis: { ...older.summary.analysis!, findings: 1 } } };
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) =>
      request.analysis?.id === "analysis-000"
        ? { ...findings(["f000001", "f000002", "f000003"], { ref: older.ref, created_at: older.created_at, current: false }), context: request.context }
        : { ...findings(["f000001"]), context: request.context },
  });
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "analysis" ? { state: "completed", context: query.context, page: { items: [older, newer], total: 2, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, facade),
  });
  await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "History" }));
  const history = await screen.findByRole("dialog", { name: "History" });
  const table = within(history).getByRole("table", { name: "Analyses" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  expect(rowsOf(table).map((row) => row.slice(1))).toEqual([
    ["SIU", "1"],
    ["SIU", "3"],
  ]);
  expect((facade.callsTo("ListCatalog").find((call) => (call.args[0] as { kind: string }).kind === "analysis")!.args[0] as { filter: object }).filter).toEqual({ related_case: CASE.ref });
  await user.dblClick(within(table).getByText("3"));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Findings" }))).toHaveLength(3));
  expect(page().getByRole("button", { name: "Show current" })).toBeTruthy();
  // An older analysis is read, never reviewed as if current.
  await user.click(within(page().getByRole("table", { name: "Findings" })).getAllByRole("row")[1]!);
  expect(details().getByRole("button", { name: "Review" })).toHaveProperty("disabled", true);
});

test("Review requires a reason, shows what a suppression covers before Save, and Undo keeps the earlier decision in history", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const confirmed: FindingDecision = { finding: "f000001", verdict: "confirmed", rationale: "scheduler must keep rejecting" };
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001", "f000002"]), context: request.context }),
    PreviewFindingReview: (request) => ({ state: "completed", context: request.context, problems: [], effects: [{ finding: "f000001", verdict: "suppressed", scope: "case", findings: ["f000001", "f000002"] }], statuses: [] }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "finding-review", id: "review-1", revision: "1" }, replayed: false, problems: [] }),
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await user.click(within(table).getAllByRole("row")[1]!);
  await user.click(details().getByRole("button", { name: "Review" }));
  const sheet = await screen.findByRole("dialog", { name: "Review Unexpected acknowledgement" });
  expect(within(sheet).getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);
  await user.click(within(sheet).getByRole("radio", { name: "Suppress" }));
  await user.selectOptions(within(sheet).getByLabelText("Scope"), "case");
  expect(await within(sheet).findByText("Covers 2 findings.")).toBeTruthy();
  await user.click(within(sheet).getByRole("radio", { name: "Confirm" }));
  await user.type(within(sheet).getByLabelText("Reason"), "scheduler must keep rejecting");

  facade.reply({
    FindingReviewHistory: (request) => ({
      state: "completed",
      context: request.context,
      review: { kind: "finding-review", id: "review-1", revision: "1" },
      analysis: ANALYSIS_REF,
      revisions: [{ revision: "1", published_at: "2026-01-02T10:00:00Z", author: "Avery QA", decisions: [confirmed] }],
      statuses: [findingStatus("f000001", "confirmed"), findingStatus("f000002", "not_reviewed")],
    }),
  });
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const saved = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(saved).toMatchObject({ kind: "finding-review", draft: { finding_review: { analysis: ANALYSIS_REF, report_sha256: REPORT_SHA256, decisions: [confirmed] } } });
  await waitFor(() => expect(rowsOf(table)[0]).toContain("Confirmed"));
  expect(details().getByText(/Confirmed · Avery QA/)).toBeTruthy();

  facade.reply({
    FindingReviewHistory: (request) => ({
      state: "completed",
      context: request.context,
      review: { kind: "finding-review", id: "review-1", revision: "2" },
      analysis: ANALYSIS_REF,
      revisions: [
        { revision: "2", published_at: "2026-01-02T11:00:00Z", author: "Avery QA", decisions: [] },
        { revision: "1", published_at: "2026-01-02T10:00:00Z", author: "Avery QA", decisions: [confirmed] },
      ],
      statuses: [findingStatus("f000001", "not_reviewed"), findingStatus("f000002", "not_reviewed")],
    }),
  });
  await user.click(details().getByRole("button", { name: "Undo decision" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  // Undo is a new revision on the current one, without the decision; the earlier one stays in history.
  expect(facade.callsTo("SaveItem")[1]!.args[0]).toMatchObject({ item: "review-1", base_revision: "1", draft: { finding_review: { decisions: [] } } });
  await waitFor(() => expect(rowsOf(table)[0]).toContain("New"));
  expect(details().getByText(/Undone · Avery QA/)).toBeTruthy();
  expect(details().getByText(/Confirmed · Avery QA/)).toBeTruthy();
});

test("Analysis settings saves one version and an imported unsupported pair stays as imported", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const imported: CatalogItem = {
    ref: { kind: "analysis-settings", id: "imported", revision: "1" },
    name: "Vendor settings",
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { analysis_settings: { profile: "vendor-x", profile_name: "", ruleset: "vendor-rules", rules: 0, namespaces: 0, supported: false } },
  };
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    OpenItemDraft: (request: ItemRequest) =>
      request.ref.id === "imported"
        ? { state: "completed", context: request.context, new: false, ref: imported.ref, draft: { analysis_settings: { schema: "readmit-diagnose-config/v1", profile: "vendor-x", ruleset: "vendor-rules", rules: [], namespaces: [] } } }
        : { state: "completed", context: request.context, new: true, ref: { kind: "analysis-settings", id: "" }, draft: { analysis_settings: { schema: "readmit-diagnose-config/v1", profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1", rules: [], namespaces: [] } } },
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "analysis-settings", id: "new-settings", revision: "1" }, replayed: false, problems: [] }),
  });
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "analysis-settings" ? { state: "completed", context: query.context, page: { items: [imported], total: 1, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, facade),
  });
  await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Analysis settings" }));
  const sheet = await screen.findByRole("dialog", { name: "Analysis settings" });
  await waitFor(() => expect(within(within(sheet).getByLabelText("Settings")).getByRole("option", { name: "Vendor settings" })).toBeTruthy());
  await user.selectOptions(within(sheet).getByLabelText("Settings"), "imported");
  expect(await within(sheet).findByText("vendor-x (unsupported)")).toBeTruthy();
  // The pair stays read-only; only its name can change, and Save keeps the pair as imported.
  expect(within(sheet).queryByLabelText("Profile")).toBeNull();
  await user.clear(within(sheet).getByLabelText("Name"));
  await user.type(within(sheet).getByLabelText("Name"), "Vendor rules");
  expect(within(sheet).getByRole("button", { name: "Save" })).toHaveProperty("disabled", false);
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.callsTo("SaveItem")[0]!.args[0]).toMatchObject({
    kind: "analysis-settings",
    item: "imported",
    base_revision: "1",
    draft: { name: "Vendor rules", analysis_settings: { profile: "vendor-x", ruleset: "vendor-rules", rules: [], namespaces: [] } },
  });

  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Analysis settings" }));
  const again = await screen.findByRole("dialog", { name: "Analysis settings" });
  await waitFor(() => expect(within(within(again).getByLabelText("Settings")).getByRole("option", { name: "Vendor settings" })).toBeTruthy());
  await user.selectOptions(within(again).getByLabelText("Settings"), "new");
  await user.type(await within(again).findByLabelText("Name"), "Scheduling rules");
  await user.click(within(again).getByRole("checkbox", { name: "Repeated control ID" }));
  await user.click(within(again).getByRole("button", { name: "Add namespace" }));
  await user.type(within(again).getByLabelText("Name 1"), "mrn");
  await user.type(within(again).getByLabelText("Namespace 1"), "HOSP");
  await user.click(within(again).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(2));
  const request = facade.callsTo("SaveItem")[1]!.args[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "analysis-settings", draft: { name: "Scheduling rules", analysis_settings: { profile: "readmit-siu-v1", rules: ["ack.msa-outcome"], namespaces: [{ key: "mrn", namespace: "HOSP", universal_id: "", universal_id_type: "" }] } } });
  expect(request.item).toBeUndefined();
});

test("View messages selects the evidence field and Back to findings restores the selection, filter and scroll", { timeout: 15_000 }, async () => {
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
  const ids = Array.from({ length: 60 }, (_, i) => `f${String(i + 1).padStart(6, "0")}`);
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(ids), context: request.context }),
    InspectOccurrence: (request) => {
      const selected = { segment: "MSH", field: 10, path: request.path, parent: "MSH", kind: "field", state: "present" as const, start: 0, end: 8 };
      return inspectionResult(request.occurrence, { selected, grid: { segment: "MSH", offset: 0, field_count: 1, rows: [{ node: selected, label: "Message Control ID", selector: request.path, segment_name: "Message Header", value: "", truncated: false }] } });
    },
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "Filter findings" }));
  const filter = await screen.findByRole("dialog", { name: "Filter findings" });
  await user.click(within(filter).getByRole("checkbox", { name: "New" }));
  await user.click(within(filter).getByRole("button", { name: "Apply" }));
  // Scrolled so the 21st finding is the first on screen, and one below it chosen.
  const scroller = page().getByRole("table", { name: "Findings" }).closest<HTMLElement>(".table-view")!;
  scroller.scrollTop = 20 * ROW;
  fireEvent.scroll(scroller);
  const chosen = await waitFor(() => {
    const row = table.querySelector<HTMLElement>('[data-row-id="f000024"]');
    if (!row) throw new Error("the 24th finding is not drawn");
    return row;
  });
  await user.click(chosen);
  expect(scroller.scrollTop).toBe(20 * ROW);
  await user.click(details().getByRole("button", { name: "View messages" }));
  await waitFor(() => expect(facade.callsTo("ReadMessages").at(-1)?.args[0]).toMatchObject({ occurrences: [GRID_OCCURRENCE] }));
  expect(await page().findByText("Finding evidence")).toBeTruthy();
  // The evidence field opens selected in the inspector, not the message root.
  await waitFor(() => expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: GRID_OCCURRENCE, path: "MSH-10" }));
  expect((await details().findByRole("button", { name: /^MSH-10 / })).getAttribute("aria-current")).toBe("location");
  await user.click(page().getByRole("button", { name: "Back to findings" }));
  // The finding chosen before is still selected, with the list where it was left.
  expect(await screen.findByRole("region", { name: "Details" })).toBeTruthy();
  expect(details().getByRole("heading", { name: "Repeated control ID" })).toBeTruthy();
  const back = page().getByRole("table", { name: "Findings" }).closest<HTMLElement>(".table-view")!;
  expect(back).not.toBe(scroller);
  await waitFor(() => expect(back.scrollTop).toBe(20 * ROW));
  expect(back.querySelector('[data-row-id="f000024"]')?.getAttribute("aria-selected")).toBe("true");
  await user.click(page().getByRole("button", { name: "Filter findings" }));
  expect(within(await screen.findByRole("dialog", { name: "Filter findings" })).getByRole("checkbox", { name: "New" })).toHaveProperty("checked", true);
});

test("Similar findings compares only the chosen cases and lists an unreadable case as a row", async () => {
  const user = userEvent.setup();
  const other = caseCatalogItem("other-case");
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU")] }),
    FindSimilarFindings: (request: SimilarRequest) => ({
      state: "completed",
      context: request.context,
      members: [
        { case: CASE.ref, name: CASE_ENTRY, state: "analyzed" as const },
        { case: other.ref, name: "other-case", state: "unavailable" as const, reason: "The case's evidence cannot be read." },
      ],
      groups: [{ signature: "sig-1", rule_id: "ack.msa-outcome", rule_name: "Unexpected acknowledgement", classification: "observed_fact" as const, cases: [CASE.ref], members: [{ case: CASE.ref, member: 0, finding: "f000001", occurrences: [GRID_OCCURRENCE] }] }],
    }),
  });
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "case" ? { state: "completed", context: query.context, page: { items: [CASE, other, caseCatalogItem("third-case")], total: 3, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, facade),
  });
  await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Similar findings" }));
  expect(await page().findByRole("heading", { level: 1, name: "Similar findings" })).toBeTruthy();
  await user.click(page().getAllByRole("button", { name: "Choose cases" })[0]!);
  const sheet = await screen.findByRole("dialog", { name: "Choose cases" });
  await waitFor(() => expect(within(sheet).getByRole("checkbox", { name: CASE_ENTRY })).toHaveProperty("checked", true));
  await user.click(within(sheet).getByRole("checkbox", { name: "other-case" }));
  await user.click(within(sheet).getByRole("button", { name: "Compare" }));
  await waitFor(() => expect(facade.callsTo("FindSimilarFindings")).toHaveLength(1));
  expect((facade.oneCall("FindSimilarFindings")[0] as SimilarRequest).cases).toEqual([CASE.ref, other.ref]);
  expect(rowsOf(await page().findByRole("table", { name: "Similar findings" }))[0]).toEqual(["Unexpected acknowledgement", "1", "Fact"]);
  expect(rowsOf(page().getByRole("table", { name: "Cases not compared" }))[0]).toEqual(["other-case", "Unavailable", "The case's evidence cannot be read."]);
});

test("Create test opens the test editor from a confirmed finding with its expectations undecided", async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001", "f000002"]), context: request.context }),
    FindingReviewHistory: (request) => ({
      state: "completed",
      context: request.context,
      review: { kind: "finding-review", id: "review-1", revision: "1" },
      analysis: ANALYSIS_REF,
      revisions: [{ revision: "1", published_at: "2026-01-02T10:00:00Z", author: "Avery QA", decisions: [{ finding: "f000001", verdict: "confirmed" as const, rationale: "must keep rejecting" }] }],
      statuses: [findingStatus("f000001", "confirmed", { promotion: findingPromotion([GRID_OCCURRENCE]) }), findingStatus("f000002", "not_reviewed")],
    }),
    OpenItemDraft: (request) => ({
      state: "completed",
      context: request.context,
      new: true,
      ref: { kind: "test", id: "" },
      draft: { name: "Unexpected acknowledgement", test: { schema: "readmit-test-draft/v1", case: { entry: CASE_ENTRY, identity: CASE_IDENTITY }, name: "Unexpected acknowledgement", messages: [GRID_OCCURRENCE], target: "", boundary: "", observation: "", reset: "", expectations: [] } },
      test: { case: CASE.ref, case_name: CASE_ENTRY, messages: [], observations: [], unsupported: [], proposals: request.from?.proposals ?? [], read_only: false },
    }),
  });
  const table = await page().findByRole("table", { name: "Findings" });
  // An unreviewed finding offers no Create test.
  await user.click(within(table).getAllByRole("row")[2]!);
  expect(details().queryByRole("button", { name: "Create test" })).toBeNull();
  await user.click(within(table).getAllByRole("row")[1]!);
  await user.click(details().getByRole("button", { name: "Create test" }));
  await waitFor(() => expect(facade.callsTo("OpenItemDraft")).toHaveLength(1));
  const origin = (facade.oneCall("OpenItemDraft")[0] as ItemRequest).from;
  expect(origin).toMatchObject({ case: CASE.ref, messages: [GRID_OCCURRENCE], title: "Unexpected acknowledgement", source: { kind: "finding", finding: "f000001", report_sha256: REPORT_SHA256, review: "review-1" } });
  expect(origin?.proposals?.length).toBeGreaterThan(0);
  expect(origin?.proposals?.every((proposal) => proposal.source === "finding")).toBe(true);
  expect(await screen.findByRole("heading", { level: 1, name: "New test" })).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

afterEach(() => vi.restoreAllMocks());

test("in a narrow window a finding is shown alone, with the way back to the findings", async () => {
  const user = userEvent.setup();
  await openFindings(user, { OpenCaseFindings: (request) => ({ ...findings(["f000001", "f000002"]), context: request.context }) });
  const table = await page().findByRole("table", { name: "Findings" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  windowWidth(820);
  await user.click(within(table).getAllByRole("row")[1]!);
  expect(await screen.findByRole("region", { name: "Finding" })).toBeTruthy();
  expect(screen.queryByRole("region", { name: "Main content" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Back to findings" }));
  expect(screen.getByRole("region", { name: "Main content" })).toBeTruthy();
  expect(screen.queryByRole("region", { name: "Finding" })).toBeNull();
});

test("findings are listed by severity with a Severity column and a severity filter that reads again", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const error = findingRow("f000002", "ack.msa-outcome");
  const warning = findingRow("f000001", "message.duplicate-control-id");
  // The facade sorts by severity across every finding and filters by it; the list shows its order.
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => {
      const wanted = request.severities ?? [];
      const rows = [error, warning].filter((row) => wanted.length === 0 || wanted.includes(row.severity!));
      return { ...findingsOf(rows, { diagnosis: { ...diagnosisResult([]).diagnosis!, total: 2 } }), ...(rows.length === 0 ? { state: "empty" as const } : {}), context: request.context };
    },
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Finding", "Severity", "Review"]);
  expect(rowsOf(table).map((row) => row.filter((cell) => cell !== ""))).toEqual([
    ["Unexpected acknowledgement", "Error", "New"],
    ["Repeated control ID", "Warning", "New"],
  ]);

  await user.click(page().getByRole("button", { name: "Filter findings" }));
  let filter = await screen.findByRole("dialog", { name: "Filter findings" });
  // The Rule filter offers every rule of the analysis's ruleset.
  expect(within(filter).getByRole("checkbox", { name: "Repeated control ID" })).toBeTruthy();
  await user.click(within(filter).getByRole("checkbox", { name: "Warning" }));
  await user.click(within(filter).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(facade.callsTo("OpenCaseFindings").at(-1)?.args[0]).toMatchObject({ severities: ["warning"], offset: 0 }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Findings" }))).toEqual([["Repeated control ID", "Warning", "New"]]));

  await user.click(page().getByRole("button", { name: "Filter findings" }));
  filter = await screen.findByRole("dialog", { name: "Filter findings" });
  await user.click(within(filter).getByRole("checkbox", { name: "Warning" }));
  await user.click(within(filter).getByRole("checkbox", { name: "Info" }));
  await user.click(within(filter).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(facade.callsTo("OpenCaseFindings").at(-1)?.args[0]).toMatchObject({ severities: ["info"] }));
  // The analysis has findings; none has this severity.
  expect(await page().findByText("No matching findings")).toBeTruthy();
  expect(page().queryByText("No findings")).toBeNull();
  await user.click(page().getByRole("button", { name: "Clear filters" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Findings" }))).toHaveLength(2));
  expect(facade.callsTo("OpenCaseFindings").at(-1)?.args[0]).not.toHaveProperty("severities");
});

test("a failed read shows the reason and Retry reads again", async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ state: "failed", reason: "The project's analyses could not be read.", context: request.context, rules: [] }),
  });
  expect(await page().findByText("The project's analyses could not be read.")).toBeTruthy();
  expect(page().queryByText("Not analyzed")).toBeNull();
  facade.reply({ OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }) });
  await user.click(page().getByRole("button", { name: "Retry" }));
  await waitFor(() => expect(rowsOf(page().getByRole("table", { name: "Findings" }))).toHaveLength(1));
  expect(facade.callsTo("OpenCaseFindings")).toHaveLength(2);
  expect(page().queryByText("The project's analyses could not be read.")).toBeNull();
});

test("an analysis in flight when the case changes never appears under the new case", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, {
    SelectWorkspace: () =>
      folderChosen(undefined, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
      ]),
    OpenCase: (_workspace, name) => caseResult(name, name === CASE_ENTRY ? CASE_IDENTITY : "other-identity-fixed-for-tests"),
    OpenCaseFindings: (request) => ({ state: "empty", context: request.context, rules: RULES }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU")] }),
  });
  const analysis = facade.park("AnalyzeCase");
  await user.click(await page().findByRole("button", { name: "Analyze" }));
  await waitFor(() => expect(analysis.size).toBe(1));

  await goTo(user, "Cases");
  for (let step = 0; step < 3 && !screen.queryByRole("table", { name: "Cases" }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) break;
    await user.click(back);
  }
  await user.dblClick(await findCaseRow(OTHER_CASE_ENTRY));
  await user.click(await screen.findByRole("tab", { name: "Findings" }));
  await waitFor(() => expect(facade.callsTo("OpenCaseFindings").at(-1)?.args[0]).toMatchObject({ identity: "other-identity-fixed-for-tests" }));
  expect(await page().findByText("Not analyzed")).toBeTruthy();

  // The first case's analysis finishes after the change: its findings are not shown here.
  analysis.resolve({ ...findings(["f000001", "f000002"]), context: (facade.oneCall("AnalyzeCase")[0] as { context: FindingsResult["context"] }).context });
  await new Promise((settle) => setTimeout(settle, 50));
  expect(page().queryByRole("table", { name: "Findings" })).toBeNull();
  expect(page().getByText("Not analyzed")).toBeTruthy();
});

test("nonzero unevaluated evidence is a result filter and is never hidden behind No findings", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const unsupported = [
    { code: "unsupported_field", occurrence: GRID_OCCURRENCE, field: "SCH-2", detail: "SCH-2 is not evaluated by this profile." },
    { code: "unsupported_field", occurrence: GRID_OCCURRENCE, field: "", detail: "An acknowledgement stage is not evaluated." },
  ];
  const diagnosis = (total: number) => ({ ...diagnosisResult([]).diagnosis!, total, unsupported });
  await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findingsOf([], { diagnosis: diagnosis(0) }), context: request.context }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU")] }),
    AnalyzeCase: (request) => ({ ...findingsOf([findingRow("f000001")], { diagnosis: diagnosis(1) }), context: request.context }),
  });
  // Zero findings with unevaluated evidence shows what was not evaluated, not a pass.
  const unevaluated = await page().findByRole("table", { name: "Unevaluated evidence" });
  expect(rowsOf(unevaluated).map((row) => row.filter((cell) => cell !== ""))).toEqual([
    ["SCH-2 is not evaluated by this profile.", "SCH-2"],
    ["An acknowledgement stage is not evaluated.", "—"],
  ]);
  expect(page().queryByText("No findings")).toBeNull();
  expect(page().getByRole("button", { name: "Unevaluated 2" })).toBeTruthy();

  // With findings, the same count is a filter that switches between the two results.
  await user.click(page().getByRole("button", { name: "Analyze" }));
  const table = await page().findByRole("table", { name: "Findings" });
  await waitFor(() => expect(rowsOf(table)).toHaveLength(1));
  const chip = page().getByRole("button", { name: "Unevaluated 2" });
  expect(chip.getAttribute("aria-pressed")).toBe("false");
  await user.click(chip);
  expect(await page().findByRole("table", { name: "Unevaluated evidence" })).toBeTruthy();
  expect(page().queryByRole("table", { name: "Findings" })).toBeNull();
  expect(page().getByRole("button", { name: "Unevaluated 2" }).getAttribute("aria-pressed")).toBe("true");
  await user.click(page().getByRole("button", { name: "Unevaluated 2" }));
  expect(await page().findByRole("table", { name: "Findings" })).toBeTruthy();
});

test("with no supported profile Analyze shows No supported profile and Choose profile lists the refusals", async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ state: "empty", context: request.context, rules: RULES }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("Lifecycle", false), profile("Orders", false, "order")] }),
  });
  await user.click(await page().findByRole("button", { name: "Analyze" }));
  expect(await page().findByText("No supported profile")).toBeTruthy();
  expect(page().queryByText("Not analyzed")).toBeNull();
  expect(screen.queryByRole("dialog", { name: "Analyze" })).toBeNull();
  await user.click(page().getByRole("button", { name: "Choose profile" }));
  const sheet = await screen.findByRole("dialog", { name: "Analyze" });
  expect(within(sheet).getByText("The case is not a Lifecycle case.")).toBeTruthy();
  expect(within(sheet).getByText("The case is not a Orders case.")).toBeTruthy();
  // Nothing refused can be chosen.
  expect(within(sheet).queryByLabelText("Profile")).toBeNull();
  expect(within(sheet).getByRole("button", { name: "Analyze" })).toHaveProperty("disabled", true);
  expect(facade.callsTo("AnalyzeCase")).toHaveLength(0);
});

test("the Analyze sheet shows the case version it analyzes", async () => {
  const user = userEvent.setup();
  const versioned: CatalogItem = { ...CASE, ref: { ...CASE.ref, revision: "3" } };
  const { facade } = await openFindings(user, {
    ListCatalog: (query) =>
      query.kind === "case"
        ? { state: "completed", context: query.context, page: { items: [versioned], total: 1, snapshot: "s", recorded: true, incomplete: [] } }
        : { state: "empty", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } },
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU"), profile("Orders", true, "order")] }),
  });
  await page().findByRole("table", { name: "Findings" });
  expect(facade.callsTo("OpenCaseFindings")[0]!.args[0]).toMatchObject({ case: versioned.ref });
  await user.click(page().getByRole("button", { name: "Analyze" }));
  const sheet = await screen.findByRole("dialog", { name: "Analyze" });
  expect(within(sheet).getByText("Case")).toBeTruthy();
  expect(within(sheet).getByText(`${CASE_ENTRY} · v3`)).toBeTruthy();
});

test("a selected finding shows its field labels and the messages its evidence is in, including off-page ones", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const finding = findingRow("f000001", "ack.msa-outcome", {
    evidence: [
      { occurrence: GRID_OCCURRENCE, field: "MSH-10", state: "present", offset: null, length: null },
      { occurrence: NEXT_OCCURRENCE, field: "ZXX-4", state: "empty", offset: null, length: null },
    ],
  });
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findingsOf([finding]), context: request.context }),
    // The grid's page holds only the first message; the second is read by its occurrence.
    ReadMessages: (request) =>
      request.occurrences?.length
        ? messagesResult(request.occurrences.map((occurrence) => messageRow(occurrence)))
        : messagesResult([messageRow(GRID_OCCURRENCE)]),
    InspectOccurrence: (request) => inspectionResult(request.occurrence),
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await user.click(within(table).getAllByRole("row")[1]!);
  expect(details().getByText("Error")).toBeTruthy();
  expect(details().getByText("Message Control ID (MSH-10) · Present")).toBeTruthy();
  // A path with no label for every message it is in is shown as the path.
  expect(details().getByText("ZXX-4 · Empty")).toBeTruthy();
  const messages = await details().findByRole("list", { name: "Messages" });
  await waitFor(() => expect(within(messages).getAllByRole("listitem")).toHaveLength(2));
  expect(facade.callsTo("ReadMessages").at(-1)?.args[0]).toMatchObject({ case: CASE_ENTRY, identity: CASE_IDENTITY, occurrences: [GRID_OCCURRENCE, NEXT_OCCURRENCE] });
  // Each message is a link to that message, at its evidence field.
  await user.click(within(within(messages).getAllByRole("listitem")[1]!).getByRole("button"));
  await waitFor(() => expect(facade.callsTo("ReadMessages").at(-1)?.args[0]).toMatchObject({ occurrences: [NEXT_OCCURRENCE] }));
  await waitFor(() => expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: NEXT_OCCURRENCE, path: "ZXX-4" }));
  expect(await page().findByText("Finding evidence")).toBeTruthy();
});

test("Review asks for a reviewer name only when none is set and saves it on this computer", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    SavePreferences: (preferences) => ({ state: "completed", preferences }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "finding-review", id: "review-1", revision: "1" }, replayed: false, problems: [] }),
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await user.click(within(table).getAllByRole("row")[1]!);
  await user.click(details().getByRole("button", { name: "Review" }));
  let sheet = await screen.findByRole("dialog", { name: "Review Unexpected acknowledgement" });
  await user.type(await within(sheet).findByLabelText("Reviewer on this computer"), "Avery QA");
  await user.type(within(sheet).getByLabelText("Reason"), "scheduler must keep rejecting");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.oneCall("SavePreferences")[0]).toMatchObject({ theme: "system", text_scale: 100, reviewer: "Avery QA" });
  // The name is saved on this computer before the decision is recorded under it.
  const order = facade.calls.map((call) => call.method).filter((method) => method === "SavePreferences" || method === "SaveItem");
  expect(order).toEqual(["SavePreferences", "SaveItem"]);

  // With a name set, it is shown as this computer's reviewer and not asked again.
  facade.reply({ ReadPreferences: () => ({ state: "completed", preferences: { theme: "system", text_scale: 100, reviewer: "Avery QA" } }) });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Review Unexpected acknowledgement" })).toBeNull());
  await user.click(details().getByRole("button", { name: "Review" }));
  sheet = await screen.findByRole("dialog", { name: "Review Unexpected acknowledgement" });
  expect(await within(sheet).findByText("Avery QA · This computer")).toBeTruthy();
  expect(within(sheet).queryByLabelText("Reviewer on this computer")).toBeNull();
});

test("a refused review save shows the reason and keeps the sheet open", async () => {
  const user = userEvent.setup();
  await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    ReadPreferences: () => ({ state: "completed", preferences: { theme: "system", text_scale: 100, reviewer: "Avery QA" } }),
    SaveItem: (request) => ({ state: "permission_denied", reason: "This computer is not admitted to author in this project.", context: request.context, outcome: "failed", replayed: false, problems: [] }),
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await user.click(within(table).getAllByRole("row")[1]!);
  await user.click(details().getByRole("button", { name: "Review" }));
  const sheet = await screen.findByRole("dialog", { name: "Review Unexpected acknowledgement" });
  await user.type(within(sheet).getByLabelText("Reason"), "scheduler must keep rejecting");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("This computer is not admitted to author in this project.")).toBeTruthy();
  expect(screen.getByRole("dialog", { name: "Review Unexpected acknowledgement" })).toBe(sheet);
  expect(within(sheet).getByLabelText("Reason")).toHaveProperty("value", "scheduler must keep rejecting");
  expect(rowsOf(table)[0]).toContain("New");
});

test("a confirmed finding without proposed checks still opens a draft with explicit expectations left to the author", async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    FindingReviewHistory: (request) => ({
      state: "completed",
      context: request.context,
      review: { kind: "finding-review", id: "review-1", revision: "1" },
      analysis: ANALYSIS_REF,
      revisions: [{ revision: "1", published_at: "2026-01-02T10:00:00Z", author: "Avery QA", decisions: [{ finding: "f000001", verdict: "confirmed" as const, rationale: "must keep rejecting" }] }],
      statuses: [findingStatus("f000001", "confirmed", { next_evidence: "Capture the acknowledgement the case does not hold." })],
    }),
    OpenItemDraft: (request) => ({
      state: "completed", context: request.context, new: true, ref: {kind: "test", id: ""},
      draft: {name: "Unexpected acknowledgement", test: {schema: "readmit-test-draft/v1", case: {entry: CASE_ENTRY, identity: CASE_IDENTITY}, name: "Unexpected acknowledgement", messages: [GRID_OCCURRENCE], target: "", boundary: "", observation: "", reset: "", expectations: []}, test_links: {source: request.from!.source!}},
      test: {case: CASE.ref, case_name: CASE_ENTRY, messages: [], observations: [], unsupported: [], proposals: [], read_only: false},
    }),
  });
  const table = await page().findByRole("table", { name: "Findings" });
  await waitFor(() => expect(rowsOf(table)[0]).toContain("Confirmed"));
  await user.click(within(table).getAllByRole("row")[1]!);
  expect(details().getByRole("button", { name: "Create test" })).toHaveProperty("disabled", false);
  expect(details().getByText("Capture the acknowledgement the case does not hold.")).toBeTruthy();
  await user.click(details().getByRole("button", {name: "Create test"}));
  await screen.findByRole("dialog", {name: "Create test case"});
  expect((facade.oneCall("OpenItemDraft")[0] as ItemRequest).from).toMatchObject({case: CASE.ref, source: {kind: "finding", finding: "f000001", report_sha256: REPORT_SHA256, review: "review-1"}});
  expect(page().getByText(/Choose the expected behavior/)).toBeTruthy();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("Import settings opens the file as an unsaved draft, an unsupported pair is saved unchanged, and Export writes the saved settings", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const saved: CatalogItem = {
    ref: { kind: "analysis-settings", id: "scheduling", revision: "2" },
    name: "Scheduling rules",
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { analysis_settings: { profile: "readmit-siu-v1", profile_name: "SIU", ruleset: "readmit-siu-diagnosis/v1", rules: 0, namespaces: 0, supported: true } },
  };
  const vendor = { schema: "readmit-diagnose-config/v1" as const, profile: "vendor-x", ruleset: "vendor-rules", rules: ["vendor.rule"], namespaces: [{ key: "mrn", namespace: "HOSP", universal_id: "", universal_id_type: "" }] };
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    OpenItemDraft: (request: ItemRequest) => ({
      state: "completed",
      context: request.context,
      new: false,
      ref: saved.ref,
      draft: { analysis_settings: { schema: "readmit-diagnose-config/v1", profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1", rules: [], namespaces: [] } },
    }),
    ExportAnalysisSettings: (request) => ({ state: "completed", context: request.context, path: "/exports/Scheduling rules.json" }),
    ImportAnalysisSettings: (context) => ({ state: "completed", context, new: true, ref: { kind: "analysis-settings", id: "" }, draft: { name: "vendor", analysis_settings: vendor }, problems: [] }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "analysis-settings", id: "vendor", revision: "1" }, replayed: false, problems: [] }),
  });
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "analysis-settings" ? { state: "completed", context: query.context, page: { items: [saved], total: 1, snapshot: "s", recorded: true, incomplete: [] } } : catalogOfListing(query, facade),
  });
  await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Analysis settings" }));
  const sheet = await screen.findByRole("dialog", { name: "Analysis settings" });
  // Only saved settings can be exported.
  await user.click(within(sheet).getByRole("button", { name: "More settings actions" }));
  expect((await screen.findByRole("menuitem", { name: "Export settings…" })).hasAttribute("disabled")).toBe(true);
  await user.keyboard("{Escape}");
  await waitFor(() => expect(within(within(sheet).getByLabelText("Settings")).getByRole("option", { name: "Scheduling rules" })).toBeTruthy());
  await user.selectOptions(within(sheet).getByLabelText("Settings"), "scheduling");
  await within(sheet).findByLabelText("Name");
  await user.click(within(sheet).getByRole("button", { name: "More settings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Export settings…" }));
  await waitFor(() => expect(facade.callsTo("ExportAnalysisSettings")).toHaveLength(1));
  expect(facade.oneCall("ExportAnalysisSettings")[0]).toMatchObject({ ref: saved.ref });
  expect(await within(sheet).findByText("Exported Scheduling rules.json")).toBeTruthy();

  // Import reads the file into the sheet as new settings, saving nothing.
  await user.click(within(sheet).getByRole("button", { name: "More settings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Import settings…" }));
  expect(await within(sheet).findByText("vendor-x (unsupported)")).toBeTruthy();
  expect(within(sheet).getByLabelText("Name")).toHaveProperty("value", "vendor");
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await user.clear(within(sheet).getByLabelText("Name"));
  await user.type(within(sheet).getByLabelText("Name"), "Vendor rules");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "analysis-settings", draft: { name: "Vendor rules", analysis_settings: vendor } });
  expect(request.draft.analysis_settings).toEqual(vendor);
  expect(request.item).toBeUndefined();
});

function similarAnswer(request: SimilarRequest, other: CatalogItem, extra: Partial<SimilarResult> = {}): SimilarResult {
  return {
    state: "completed",
    context: request.context,
    members: [
      { case: CASE.ref, name: CASE_ENTRY, state: "analyzed" as const },
      { case: other.ref, name: OTHER_CASE_ENTRY, state: "analyzed" as const },
      { case: caseCatalogItem("third-case").ref, name: "third-case", state: "unavailable" as const, reason: "The case's evidence cannot be read." },
    ],
    groups: [
      {
        signature: "sig-1",
        rule_id: "ack.msa-outcome",
        rule_name: "Unexpected acknowledgement",
        classification: "observed_fact" as const,
        cases: [CASE.ref, other.ref],
        members: [
          { case: CASE.ref, member: 0, finding: "f000001", occurrences: [GRID_OCCURRENCE] },
          { case: other.ref, member: 1, finding: "f000004", occurrences: [NEXT_OCCURRENCE, "occ-000009"] },
        ],
      },
    ],
    ...extra,
  };
}

test("a saved comparison is named, listed in History and reopens as Similar findings", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const other = caseCatalogItem(OTHER_CASE_ENTRY);
  const savedRef = { kind: "analysis" as const, id: "grouping-001" };
  const grouping: CatalogItem = {
    ref: savedRef,
    name: "Rejected bookings",
    created_at: "2026-01-03T09:00:00Z",
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { analysis: { form: "grouping", related_case: null, cases: [CASE.ref, other.ref], findings: 1, profile: "readmit-siu-v1", profile_name: "SIU", unsupported: 0 } },
  };
  const { facade } = await openFindings(user, {
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU")] }),
    FindSimilarFindings: (request: SimilarRequest) => similarAnswer(request, other, request.save ? { saved: savedRef, name: request.name ?? "" } : {}),
    OpenSimilarFindings: (request) => similarAnswer({ context: request.context, cases: [], profile: { builtin: "siu" }, save: false }, other, { saved: savedRef, name: "Rejected bookings" }),
  });
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "case"
        ? { state: "completed", context: query.context, page: { items: [CASE, other, caseCatalogItem("third-case")], total: 3, snapshot: "s", recorded: true, incomplete: [] } }
        : query.kind === "analysis"
          ? { state: "completed", context: query.context, page: { items: [grouping], total: 1, snapshot: "s", recorded: true, incomplete: [] } }
          : catalogOfListing(query, facade),
  });
  await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Similar findings" }));
  await user.click((await page().findAllByRole("button", { name: "Choose cases" }))[0]!);
  const choose = await screen.findByRole("dialog", { name: "Choose cases" });
  await user.click(within(choose).getByRole("checkbox", { name: OTHER_CASE_ENTRY }));
  await user.click(within(choose).getByRole("checkbox", { name: "third-case" }));
  await user.click(within(choose).getByRole("button", { name: "Compare" }));
  await page().findByRole("table", { name: "Similar findings" });

  await user.click(page().getByRole("button", { name: "Save comparison" }));
  const naming = await screen.findByRole("dialog", { name: "Save comparison" });
  // The case that was not compared is said to stay out of the saved comparison.
  expect(within(naming).getByText("The case not compared is not saved with it.")).toBeTruthy();
  expect(within(naming).getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);
  await user.type(within(naming).getByLabelText("Name"), "Rejected bookings");
  await user.click(within(naming).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("FindSimilarFindings")).toHaveLength(2));
  expect(facade.callsTo("FindSimilarFindings")[1]!.args[0]).toMatchObject({ cases: [CASE.ref, other.ref, caseCatalogItem("third-case").ref], save: true, name: "Rejected bookings" });
  expect(await page().findByRole("heading", { level: 1, name: "Similar findings · Rejected bookings" })).toBeTruthy();
  expect(page().queryByRole("button", { name: "Save comparison" })).toBeNull();

  await user.click(page().getByRole("button", { name: "Back to findings" }));
  await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "History" }));
  const history = await screen.findByRole("dialog", { name: "History" });
  const listed = within(history).getByRole("table", { name: "Analyses" });
  await waitFor(() => expect(rowsOf(listed)).toHaveLength(1));
  expect(rowsOf(listed)[0]!.slice(1)).toEqual(["Similar findings · Rejected bookings", "1"]);
  await user.dblClick(within(listed).getByText("Similar findings · Rejected bookings"));
  await waitFor(() => expect(facade.callsTo("OpenSimilarFindings")).toHaveLength(1));
  expect(facade.oneCall("OpenSimilarFindings")[0]).toMatchObject({ ref: savedRef });
  expect(await page().findByRole("heading", { level: 1, name: "Similar findings · Rejected bookings" })).toBeTruthy();
  expect(rowsOf(await page().findByRole("table", { name: "Similar findings" }))[0]).toEqual(["Unexpected acknowledgement", "2", "Fact"]);
  // Reopening reads what was saved; nothing is compared again.
  expect(facade.callsTo("FindSimilarFindings")).toHaveLength(2);
});

test("a similar group lists its member cases and opens one on exactly its evidence", { timeout: 15_000 }, async () => {
  const user = userEvent.setup();
  const other = caseCatalogItem(OTHER_CASE_ENTRY);
  const { facade } = await openFindings(user, {
    SelectWorkspace: () =>
      folderChosen(undefined, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
      ]),
    OpenCase: (_workspace, name) => caseResult(name, name === CASE_ENTRY ? CASE_IDENTITY : "other-identity-fixed-for-tests"),
    OpenCaseFindings: (request) => ({ ...findings(["f000001"]), context: request.context }),
    ListAnalysisProfiles: (request) => ({ state: "completed", context: request.context, profiles: [profile("SIU")] }),
    FindSimilarFindings: (request: SimilarRequest) => similarAnswer(request, other),
    ReadMessages: (request) => messagesResult((request.occurrences?.length ? request.occurrences : [GRID_OCCURRENCE]).map((occurrence) => messageRow(occurrence))),
  });
  await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "More findings actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Similar findings" }));
  await user.click((await page().findAllByRole("button", { name: "Choose cases" }))[0]!);
  const choose = await screen.findByRole("dialog", { name: "Choose cases" });
  await waitFor(() => expect(within(choose).getByRole("checkbox", { name: OTHER_CASE_ENTRY })).toBeTruthy());
  await user.click(within(choose).getByRole("checkbox", { name: OTHER_CASE_ENTRY }));
  await user.click(within(choose).getByRole("button", { name: "Compare" }));
  const groups = await page().findByRole("table", { name: "Similar findings" });
  await user.dblClick(within(groups).getByText("Unexpected acknowledgement"));

  const group = await screen.findByRole("dialog", { name: "Unexpected acknowledgement" });
  const first = within(group).getByRole("region", { name: CASE_ENTRY });
  const second = within(group).getByRole("region", { name: OTHER_CASE_ENTRY });
  expect(within(first).getByText("1 finding")).toBeTruthy();
  expect(within(second).getByText("1 finding")).toBeTruthy();
  const opened = facade.callsTo("OpenCase").length;
  await user.click(within(second).getByRole("button", { name: "View messages" }));
  await waitFor(() => expect(facade.callsTo("OpenCase").length).toBe(opened + 1));
  expect(facade.callsTo("OpenCase").at(-1)?.args[1]).toBe(OTHER_CASE_ENTRY);
  await waitFor(() =>
    expect(facade.callsTo("ReadMessages").at(-1)?.args[0]).toMatchObject({ case: OTHER_CASE_ENTRY, identity: "other-identity-fixed-for-tests", occurrences: [NEXT_OCCURRENCE, "occ-000009"] }),
  );
  expect(await page().findByText("Finding evidence")).toBeTruthy();
  await findMessageRow("occ-000009");
  // The evidence's own Back returns to the comparison, not this case's findings.
  expect(page().queryByRole("button", { name: "Back to findings" })).toBeNull();
  await user.click(page().getByRole("button", { name: "Back to similar findings" }));
  expect(await page().findByRole("heading", { level: 1, name: "Similar findings" })).toBeTruthy();
});

test("a finding without code or occurrence opens whole-interface requirements and returns to the same finding",async()=>{
 const user=userEvent.setup();
 const row=findingRow("pathless-owned","message.duplicate-control-id",{evidence:[],labels:{},summary:"Owned diagnostic shape without field evidence"});
 const {facade}=await openFindings(user,{OpenCaseFindings:request=>({...findingsOf([row]),context:request.context}),ListInterfaceSpecs:context=>({state:"completed",context,items:[]})});
 const table=await page().findByRole("table",{name:"Findings"});
 await user.click(table.querySelector<HTMLElement>('[data-row-id="pathless-owned"]')!);
 await user.click(details().getByRole("button",{name:"Interface requirements"}));
 expect(await screen.findByText(/Whole retained interface case; no field occurrence/)).toBeTruthy();
 expect(facade.callsTo("InspectOccurrence")).toHaveLength(0);
 await user.click(screen.getByRole("button",{name:"Return to selected message"}));
 await waitFor(()=>expect(details().getByText("Owned diagnostic shape without field evidence")).toBeTruthy());
});
