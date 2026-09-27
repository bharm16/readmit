// Findings: the saved analysis of the open case version, one list with the
// selected finding beside it, Analyze with Stop, History, Analysis settings,
// review decisions saved as revisions, the evidence shown in Messages, and
// Similar findings across chosen cases. Fixtures carry positions, rule names
// and states only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AnalysisProfile, CatalogItem, FindingDecision, FindingsResult, ItemRequest, SaveItemRequest, SimilarRequest } from "./bindings";
import { renderApp } from "./testkit/app";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  caseCatalogItem,
  caseResult,
  catalogOfListing,
  diagnosisFinding,
  diagnosisResult,
  findingPromotion,
  findingStatus,
  folderWithCase,
  GRID_OCCURRENCE,
  messageRow,
  messagesResult,
  REPORT_SHA256,
} from "./testkit/fixtures";
import { details, findCaseRow, goTo, page } from "./testkit/navigation";
import type { FacadeHandlers } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);
const ANALYSIS_REF = { kind: "analysis" as const, id: "analysis-001" };
const RULES = [
  { id: "ack.msa-outcome", ruleset: "readmit-siu-diagnosis/v1", name: "Unexpected acknowledgement" },
  { id: "message.duplicate-control-id", ruleset: "readmit-siu-diagnosis/v1", name: "Repeated control ID" },
];

function findings(ids: string[], overrides: Partial<NonNullable<FindingsResult["analysis"]>> = {}): FindingsResult {
  const diagnosis = diagnosisResult(ids.map((id, index) => diagnosisFinding(id, index % 2 === 0 ? "ack.msa-outcome" : "message.duplicate-control-id"))).diagnosis!;
  return {
    state: ids.length > 0 ? "completed" : "empty",
    context: { project: "", generation: 0 },
    analysis: {
      ref: ANALYSIS_REF,
      created_at: "2026-01-02T09:00:00Z",
      profile_name: "SIU",
      report_sha256: REPORT_SHA256,
      config_sha256: "config-siu",
      current: true,
      diagnosis,
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
  expect(rowsOf(table)[0]!.filter((cell) => cell !== "")).toEqual(["Unexpected acknowledgement", "Fact", "New"]);
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

test("Analysis settings saves one version and an imported unsupported pair stays as imported", async () => {
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
  expect(within(sheet).getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);

  await user.selectOptions(within(sheet).getByLabelText("Settings"), "new");
  await user.type(await within(sheet).findByLabelText("Name"), "Scheduling rules");
  await user.click(within(sheet).getByRole("checkbox", { name: "Repeated control ID" }));
  await user.click(within(sheet).getByRole("button", { name: "Add namespace" }));
  await user.type(within(sheet).getByLabelText("Name 1"), "mrn");
  await user.type(within(sheet).getByLabelText("Namespace 1"), "HOSP");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const request = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(request).toMatchObject({ kind: "analysis-settings", draft: { name: "Scheduling rules", analysis_settings: { profile: "readmit-siu-v1", rules: ["ack.msa-outcome"], namespaces: [{ key: "mrn", namespace: "HOSP", universal_id: "", universal_id_type: "" }] } } });
  expect(request.item).toBeUndefined();
});

test("View messages selects exactly the referenced messages, including off-page ones, and Back restores the selection and filter", async () => {
  const user = userEvent.setup();
  const { facade } = await openFindings(user, { OpenCaseFindings: (request) => ({ ...findings(["f000001", "f000002"]), context: request.context }) });
  const table = await page().findByRole("table", { name: "Findings" });
  await user.click(page().getByRole("button", { name: "Filter findings" }));
  const filter = await screen.findByRole("dialog", { name: "Filter findings" });
  await user.click(within(filter).getByRole("checkbox", { name: "New" }));
  await user.click(within(filter).getByRole("button", { name: "Apply" }));
  await user.click(within(table).getAllByRole("row")[2]!);
  await user.click(details().getByRole("button", { name: "View messages" }));
  await waitFor(() => expect(facade.callsTo("ReadMessages").at(-1)?.args[0]).toMatchObject({ occurrences: [GRID_OCCURRENCE] }));
  expect(await page().findByText("Finding evidence")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Back to findings" }));
  // The finding chosen before is still selected.
  expect(await screen.findByRole("region", { name: "Details" })).toBeTruthy();
  expect(details().getByRole("heading", { name: "Repeated control ID" })).toBeTruthy();
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
      groups: [{ signature: "sig-1", rule_id: "ack.msa-outcome", rule_name: "Unexpected acknowledgement", classification: "observed_fact" as const, cases: [CASE.ref], members: [{ case: CASE.ref, finding: "f000001" }] }],
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
      test: { case: CASE.ref, case_name: CASE_ENTRY, messages: [], unsupported: [], proposals: request.from?.proposals ?? [], read_only: false },
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
