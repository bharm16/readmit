import { readCaseIdentity } from "./testkit/navigation";
// The diagnosis panel, pinned first over its typed callbacks — which request a
// person's act produces and what the panel does with the engine's answer —
// and then driven through the whole window over the stubbed facade, so every
// act below also reaches GroupDiagnoses, OpenDiagnosisReport, ReviewFindings,
// OpenFindingDecisions or SaveFindingDecisions the way the production bindings
// call them. Domain meaning — what a finding is, what a verdict covers, what a
// promotion may express, which document a reader accepts — stays with the Go
// engine and its own tests; nothing here re-decides any of it.
import { expect, test } from "vitest";
import type { ReactElement } from "react";
import { render as renderAlone, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Diagnosis } from "./Diagnosis";
import type {
  DiagnosisFindingGroup,
  DiagnosisRequest,
  DiagnosisResult,
  DiagnosisGroupsResult,
  FindingDecision,
  FindingDecisionsResult,
  FindingReviewRequest,
  FindingReviewResult,
  FindingStatus,
  GroupDiagnosesRequest,
} from "./bindings";
import { renderApp, vocabularyWrapper } from "./testkit/app";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  OTHER_CASE_ENTRY,
  REPORT_ENTRY,
  REPORT_SHA256,
  WORKSPACE_ROOT,
  caseResult,
  diagnosisFinding,
  diagnosisGroupsResult,
  diagnosisResult,
  findingPromotion,
  findingReviewResult,
  findingStatus,
  folderChosen,
  indicatorTable,
  refused,
  vocabularyFixture,
} from "./testkit/fixtures";

/** A panel on its own, inside the vocabulary the window provides it. */
const render = (ui: ReactElement) => renderAlone(ui, { wrapper: vocabularyWrapper() });

type Callbacks = {
  onRun?: (request: DiagnosisRequest) => void;
  onOpen?: (entry: string, offset: number) => void;
  onGroup?: (request: GroupDiagnosesRequest) => void;
  onOpenGroups?: (entry: string, offset: number) => void;
  onReview?: (request: FindingReviewRequest, write: boolean) => void;
  onSelect?: (occurrence: string) => void;
  onPromote?: (status: FindingStatus, reviewEntry: string, reportSHA256: string) => void;
  onManageProfiles?: () => void;
};

function renderPanel(
  result: DiagnosisResult | null,
  reviewResult: FindingReviewResult | null,
  callbacks: Callbacks = {},
  groupsResult: DiagnosisGroupsResult | null = null,
) {
  return render(
    <Diagnosis
      workspace={WORKSPACE_ROOT}
      caseName={CASE_ENTRY}
      identity={CASE_IDENTITY}
      configEntries={["diagnose-config-1"]}
      reportEntries={[REPORT_ENTRY]}
      groupsReportEntries={["weekly-groups"]}
      caseEntries={[CASE_ENTRY, OTHER_CASE_ENTRY]}
      result={result}
      groupsResult={groupsResult}
      reviewResult={reviewResult}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onRun={callbacks.onRun ?? (() => undefined)}
      onOpen={callbacks.onOpen ?? (() => undefined)}
      onGroup={callbacks.onGroup ?? (() => undefined)}
      onOpenGroups={callbacks.onOpenGroups ?? (() => undefined)}
      onReview={callbacks.onReview ?? (() => undefined)}
      onSelect={callbacks.onSelect ?? (() => undefined)}
      onPromote={callbacks.onPromote ?? (() => undefined)}
      {...(callbacks.onManageProfiles ? { onManageProfiles: callbacks.onManageProfiles } : {})}
    />,
  );
}

test("a diagnosis runs only under an explicitly chosen configuration and a named report entry", async () => {
  const user = userEvent.setup();
  const runs: DiagnosisRequest[] = [];
  renderPanel(null, null, { onRun: (request) => runs.push(request) });
  const run = () => screen.getByRole("button", { name: "Diagnose" }) as HTMLButtonElement;
  // Nothing is chosen implicitly: no configuration and no output means no run.
  expect(run().disabled).toBe(true);
  await user.selectOptions(screen.getByLabelText("Configuration"), "builtin:siu");
  expect(run().disabled).toBe(true);
  await user.type(screen.getByLabelText("New report directory in this workspace"), REPORT_ENTRY);
  await user.click(run());
  expect(runs).toEqual([
    {
      workspace: WORKSPACE_ROOT,
      case: CASE_ENTRY,
      identity: CASE_IDENTITY,
      builtin: "siu",
      output: REPORT_ENTRY,
      offset: 0,
    },
  ]);
});

test("a workspace configuration is named as the entry it is, never as a builtin", async () => {
  const user = userEvent.setup();
  const runs: DiagnosisRequest[] = [];
  renderPanel(null, null, { onRun: (request) => runs.push(request) });
  await user.selectOptions(screen.getByLabelText("Configuration"), "config:diagnose-config-1");
  await user.type(screen.getByLabelText("New report directory in this workspace"), REPORT_ENTRY);
  await user.click(screen.getByRole("button", { name: "Diagnose" }));
  expect(runs[0]).toMatchObject({ config: "diagnose-config-1" });
  expect(runs[0]?.builtin).toBeUndefined();
});

test("findings are grouped by signature and every evidence reference opens the original occurrence", async () => {
  const user = userEvent.setup();
  const selected: string[] = [];
  renderPanel(
    diagnosisResult([
      diagnosisFinding("f000001", "ack.msa-outcome"),
      diagnosisFinding("f000002", "ack.msa-outcome"),
      diagnosisFinding("f000003", "message.duplicate-control-id"),
    ]),
    null,
    { onSelect: (occurrence) => selected.push(occurrence) },
  );
  // Grouping hides nothing: both signatures appear with every member inside.
  const recurring = screen.getByRole("region", {
    name: "Findings ack.msa-outcome · protocol",
  });
  expect(within(recurring).getByText("f000001")).toBeTruthy();
  expect(within(recurring).getByText("f000002")).toBeTruthy();
  expect(
    screen.getByRole("region", { name: "Findings message.duplicate-control-id · protocol" }),
  ).toBeTruthy();
  // The report identity a review must name is shown, not summarized away.
  expect(screen.getByText(new RegExp(`report identity ${REPORT_SHA256}`))).toBeTruthy();
  // Evidence is a link to the original occurrence, read in the inspector.
  const [evidence] = within(recurring).getAllByRole("button", { name: GRID_OCCURRENCE });
  await user.click(evidence as HTMLElement);
  expect(selected).toEqual([GRID_OCCURRENCE]);
});

test("a review sends only what was explicitly decided, scoped suppressions included", async () => {
  const user = userEvent.setup();
  const reviews: { request: FindingReviewRequest; write: boolean }[] = [];
  renderPanel(
    diagnosisResult([
      diagnosisFinding("f000001", "ack.msa-outcome"),
      diagnosisFinding("f000002", "message.duplicate-control-id"),
    ]),
    null,
    { onReview: (request, write) => reviews.push({ request, write }) },
  );
  // One finding is suppressed with a scope and a rationale; the other is not
  // decided about, so nothing is recorded for it.
  const [firstDecision] = screen.getAllByLabelText("Decision");
  await user.selectOptions(firstDecision as HTMLElement, "suppressed");
  await user.selectOptions(screen.getByLabelText("Suppression scope"), "occurrence");
  await user.type(screen.getByLabelText("Rationale"), "known capture artifact");
  await user.click(screen.getByRole("button", { name: "Preview" }));
  expect(reviews).toHaveLength(1);
  expect(reviews[0]?.write).toBe(false);
  expect(reviews[0]?.request).toMatchObject({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    report: "",
    report_sha256: REPORT_SHA256,
    decisions: [
      {
        finding: "f000001",
        verdict: "suppressed",
        scope: "occurrence",
        rationale: "known capture artifact",
      },
    ],
  });
  // A preview names no output entries; nothing can be written by it.
  expect(reviews[0]?.request.output).toBeUndefined();
  expect(reviews[0]?.request.decisions_output).toBeUndefined();
});

test("recording decisions names the new review directory and decisions document explicitly", async () => {
  const user = userEvent.setup();
  const reviews: { request: FindingReviewRequest; write: boolean }[] = [];
  renderPanel(diagnosisResult([diagnosisFinding("f000001")]), null, {
    onReview: (request, write) => reviews.push({ request, write }),
  });
  await user.selectOptions(screen.getByLabelText("Decision"), "confirmed");
  await user.type(screen.getByLabelText("Rationale"), "the acceptance must keep holding");
  const record = () =>
    screen.getByRole("button", { name: "Save decisions" }) as HTMLButtonElement;
  expect(record().disabled).toBe(true);
  await user.type(screen.getByLabelText("New finding-review directory"), "review-1");
  await user.type(screen.getByLabelText("New decisions document"), "decisions-1.json");
  await user.click(record());
  expect(reviews[0]?.write).toBe(true);
  expect(reviews[0]?.request).toMatchObject({
    output: "review-1",
    decisions_output: "decisions-1.json",
    decisions: [
      { finding: "f000001", verdict: "confirmed", rationale: "the acceptance must keep holding" },
    ],
  });
});

test("only an explicitly confirmed finding with expressible expectations can become a draft", async () => {
  const user = userEvent.setup();
  const promoted: { status: FindingStatus; review: string; sha: string }[] = [];
  renderPanel(
    null,
    findingReviewResult(
      [
        findingStatus("f000001", "confirmed", {
          rationale: "keep it",
          promotion: findingPromotion(),
        }),
        findingStatus("f000002", "not_reviewed"),
        findingStatus("f000003", "confirmed", {
          rationale: "confirmed but not expressible",
          promotion: {
            messages: [],
            expectations: [],
            unsupported: [
              { code: "unsupported_boundary", detail: "no acknowledgement is retained for it" },
            ],
          },
        }),
      ],
      {},
      { output: "review-1" },
    ),
    { onPromote: (status, review, sha) => promoted.push({ status, review, sha }) },
  );
  // The statement and both identities are the record's own, rendered verbatim.
  expect(
    screen.getByText(
      "The machine's findings and one person's judgment of them, joined but distinguishable.",
    ),
  ).toBeTruthy();
  // The unreviewed finding is visible and promotes nothing.
  expect(screen.getByText(/Not reviewed/)).toBeTruthy();
  expect(screen.getByText("Only a confirmed finding promotes anything.")).toBeTruthy();
  // The unsupported promotion is visible with its reasons, and not draftable.
  expect(
    screen.getByText(/Not expressible: unsupported_boundary · no acknowledgement is retained/),
  ).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Draft a test from f000003" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Draft a test from f000002" })).toBeNull();
  // The confirmed, expressible finding is the one that can be drafted.
  await user.click(screen.getByRole("button", { name: "Draft a test from f000001" }));
  expect(promoted).toHaveLength(1);
  expect(promoted[0]?.status.finding).toBe("f000001");
  expect(promoted[0]?.review).toBe("review-1");
  expect(promoted[0]?.sha).toBe(REPORT_SHA256);
});

test("grouping recurring findings re-evaluates the selected cases under the chosen configuration", async () => {
  const user = userEvent.setup();
  const grouped: GroupDiagnosesRequest[] = [];
  const { rerender } = renderPanel(null, null, {
    onGroup: (request) => grouped.push(request),
  });
  await user.selectOptions(screen.getByLabelText("Configuration"), "builtin:lifecycle");
  await user.click(screen.getByRole("checkbox", { name: CASE_ENTRY }));
  await user.click(screen.getByRole("checkbox", { name: OTHER_CASE_ENTRY }));
  await user.click(screen.getByRole("button", { name: "Group findings" }));
  expect(grouped).toEqual([
    {
      workspace: WORKSPACE_ROOT,
      cases: [CASE_ENTRY, OTHER_CASE_ENTRY],
      builtin: "lifecycle",
      offset: 0,
    },
  ]);
  rerender(
    <Diagnosis
      workspace={WORKSPACE_ROOT}
      caseName={CASE_ENTRY}
      identity={CASE_IDENTITY}
      configEntries={[]}
      reportEntries={[]}
      groupsReportEntries={[]}
      caseEntries={[CASE_ENTRY, OTHER_CASE_ENTRY]}
      result={null}
      groupsResult={{ ...diagnosisGroupsResult([
        {
          signature: "signature-fixed-for-tests",
          rule_id: "ack.msa-outcome",
          members: [
            { case_identity: CASE_IDENTITY, finding_id: "f000001" },
            { case_identity: "other-case-identity-fixed-for-tests", finding_id: "f000001" },
          ],
          representatives: [{ case_identity: CASE_IDENTITY, finding_id: "f000001" }],
          occurrences: [],
        },
      ]), case_entries: { [CASE_IDENTITY]: CASE_ENTRY } }}
      reviewResult={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onRun={() => undefined}
      onOpen={() => undefined}
      onGroup={() => undefined}
      onOpenGroups={() => undefined}
      onReview={() => undefined}
      onSelect={() => undefined}
      onPromote={() => undefined}
    />,
  );
  expect(
    screen.getByText("Equal signatures mean the same diagnostic shape, never the same root cause."),
  ).toBeTruthy();
  expect(screen.getByText(/signature signature-fixed-for-tests · 2 findings across cases/)).toBeTruthy();
  expect(screen.getByText(`${CASE_ENTRY} (${CASE_IDENTITY})`, { exact: false })).toBeTruthy();
});

test("a retained report is reopened by name, and the manage-profiles handoff is offered", async () => {
  const user = userEvent.setup();
  const opened: [string, number][] = [];
  let managed = 0;
  renderPanel(null, null, {
    onOpen: (entry, offset) => opened.push([entry, offset]),
    onManageProfiles: () => {
      managed += 1;
    },
  });
  await user.selectOptions(screen.getByLabelText("Retained diagnosis report"), REPORT_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open report" }));
  expect(opened).toEqual([[REPORT_ENTRY, 0]]);
  await user.click(screen.getByRole("button", { name: "Profiles" }));
  expect(managed).toBe(1);
});


// ---------------------------------------------------------------------------
// The panel driven through the window over the stubbed facade.

const THIRD_CASE = "third-case";
const MISSING_REPORT = "missing-report";
const GROUPS_REPORT = "weekly-groups";
const OTHER_REPORT = "other-case-report";
const OTHER_IDENTITY = "other-case-identity-fixed-for-tests";
const DECISIONS = "decisions-1.json";
const STALE_DECISIONS = "stale-decisions.json";
const OUTSIDE_DECISIONS = "outside-decisions.json";
const REFUSED_DECISIONS = "refused-decisions.json";
const DECISIONS_SHA256 = "decisions-sha256-fixed-for-tests";
const STALE_REPORT_SHA256 = "stale-report-sha256-fixed-for-tests";

const NO_REPORT = "a diagnosis report directory holds report.json as one regular file";
const NOT_A_REPORT = "diagnosis report declares a contract version this release does not read";
const OTHER_EVIDENCE = "this diagnosis was run over different evidence; open the case the report names";
const CHANGED = "the displayed diagnosis changed; reopen the report before reviewing";
const UNDECIDED =
  "a decision confirms, dismisses or suppresses a finding; leaving it undecided is recording no decision at all";
const DUPLICATE = "diagnosis grouping refuses duplicate case identities";
const CANCELLED = "the operation was cancelled";

type Stub = Awaited<ReturnType<typeof renderApp>>["facade"];
type User = ReturnType<typeof userEvent.setup>;

/** Moves focus with Tab from where it is until the control has it, as a
 * keyboard user does, failing when the control cannot be reached that way. */
async function tabTo(user: User, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 200; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? ""} is not reachable with Tab`);
}

/** A workspace holding three cases, three retained single reports, a
 * grouping report of its own kind, and four
 * decisions documents. */
function workspace() {
  return folderChosen(WORKSPACE_ROOT, [
    { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
    { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
    { name: THIRD_CASE, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
    { name: REPORT_ENTRY, kind: "diagnosis" },
    { name: MISSING_REPORT, kind: "diagnosis" },
    { name: GROUPS_REPORT, kind: "diagnosis-groups" },
    { name: OTHER_REPORT, kind: "diagnosis" },
    { name: DECISIONS, kind: "finding-decisions" },
    { name: STALE_DECISIONS, kind: "finding-decisions" },
    { name: OUTSIDE_DECISIONS, kind: "finding-decisions" },
    { name: REFUSED_DECISIONS, kind: "finding-decisions" },
  ]);
}

/** Opens the workspace and verifies its first case, as a person does before
 * the diagnosis panel is offered at all. */
async function openCase(facade: Stub, user: User) {
  facade.reply({ SelectWorkspace: () => workspace(), OpenWorkspace: () => workspace(), OpenCase: () => caseResult() });
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByText(WORKSPACE_ROOT);
  await user.click(screen.getByRole("button", { name: `Open case ${CASE_ENTRY}` }));
  await readCaseIdentity(user, CASE_IDENTITY);
  await user.click(screen.getByRole("tab", { name: "Findings" }));
  return within(await screen.findByRole("region", { name: "Diagnosis" }));
}

type Panel = Awaited<ReturnType<typeof openCase>>;

/** Two findings of the open case's retained report. */
const FINDINGS = [
  diagnosisFinding("f000001", "ack.msa-outcome"),
  diagnosisFinding("f000002", "ack.err-outcome"),
];

/** Opens the retained report of the open case, as the panel's own form does. */
async function openReport(facade: Stub, user: User, panel: Panel, entry = REPORT_ENTRY) {
  facade.reply({
    OpenDiagnosisReport: (_workspace, name) =>
      name === REPORT_ENTRY
        ? diagnosisResult(FINDINGS)
        : name === OTHER_REPORT
          ? diagnosisResult(FINDINGS, { case_identity: OTHER_IDENTITY, report_sha256: STALE_REPORT_SHA256 })
          : refused(name === MISSING_REPORT ? NO_REPORT : NOT_A_REPORT),
  });
  await user.selectOptions(panel.getByLabelText("Retained diagnosis report"), entry);
  const asked = facade.callsTo("OpenDiagnosisReport").length;
  await user.click(panel.getByRole("button", { name: "Open report" }));
  await waitFor(() => expect(facade.callsTo("OpenDiagnosisReport")).toHaveLength(asked + 1));
}

/** One group of the recurring findings, numbered so a page reads in order. */
function group(index: number): DiagnosisFindingGroup {
  return {
    signature: `signature-${String(index).padStart(3, "0")}-fixed-for-tests`,
    rule_id: "siu.booking-not-observed",
    members: [
      { case_identity: CASE_IDENTITY, finding_id: "f000001" },
      { case_identity: OTHER_IDENTITY, finding_id: "f000001" },
    ],
    representatives: [{ case_identity: CASE_IDENTITY, finding_id: "f000001" }],
    occurrences: [],
  };
}

/** A window of a grouping of two cases with 201 signatures. */
function groupsWindow(offset: number): DiagnosisGroupsResult {
  const window = Array.from({ length: offset === 0 ? 200 : 1 }, (_, index) => group(offset + index + 1));
  const result = diagnosisGroupsResult(window);
  result.offset = offset;
  result.total = 201;
  result.case_entries = { [CASE_IDENTITY]: CASE_ENTRY, [OTHER_IDENTITY]: OTHER_CASE_ENTRY };
  result.groups!.cases = [CASE_IDENTITY, OTHER_IDENTITY].map((identity) => ({
    schema: "readmit-diagnosis/v1",
    config_sha256: "config-sha256-fixed-for-tests",
    case_identity: identity,
    profile: "readmit-siu-v1",
    ruleset: "readmit-siu-diagnosis/v1",
    rules: [],
    window: diagnosisResult([]).diagnosis!.window,
    findings: [],
    unsupported: [],
    scope: "A diagnosis describes the capture window, never a complete lifecycle.",
  }));
  return result;
}

test("a grouping has its own picker and reader, and its member labels use the facade's case entries", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const singles = panel.getByLabelText("Retained diagnosis report") as HTMLSelectElement;
  const groupings = panel.getByLabelText("Retained grouping report") as HTMLSelectElement;
  expect(within(singles).queryByRole("option", { name: GROUPS_REPORT })).toBeNull();
  expect(within(groupings).getByRole("option", { name: GROUPS_REPORT })).toBeTruthy();

  facade.reply({ OpenDiagnosisGroupsReport: (_workspace, _entry, offset) => groupsWindow(offset) });
  await user.selectOptions(groupings, GROUPS_REPORT);
  await user.click(panel.getByRole("button", { name: "Open this grouping" }));
  expect(facade.oneCall("OpenDiagnosisGroupsReport")).toEqual([WORKSPACE_ROOT, GROUPS_REPORT, 0]);
  const listed = within(await panel.findByRole("region", { name: "Recurring finding groups" }));
  expect(listed.getAllByText(`${CASE_ENTRY} (${CASE_IDENTITY})`, { exact: false })).toHaveLength(200);
  expect(listed.getAllByText(`${OTHER_CASE_ENTRY} (${OTHER_IDENTITY})`, { exact: false })).toHaveLength(200);
  expect(facade.callsTo("OpenDiagnosisReport")).toHaveLength(0);

  await user.click(listed.getByRole("button", { name: "Next 200 groups" }));
  expect(facade.callsTo("OpenDiagnosisGroupsReport")[1]?.args).toEqual([WORKSPACE_ROOT, GROUPS_REPORT, 200]);
  expect(await panel.findByText("Groups 201–201 of 201 across 2 cases")).toBeTruthy();
});

test("recurring findings are grouped through the facade, paged over the grouping on screen from the keyboard, refused in the engine's words, and a grouping cancelled from the window's Cancel or Escape is shown as cancelled with no groups", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const groups = () => panel.queryByRole("region", { name: "Recurring finding groups" });

  // Grouping reads the configuration chosen above it and the cases checked.
  facade.reply({ GroupDiagnoses: (request) => groupsWindow(request.offset) });
  await user.selectOptions(panel.getByLabelText("Configuration"), "builtin:siu");
  await user.click(panel.getByRole("checkbox", { name: CASE_ENTRY }));
  await user.click(panel.getByRole("checkbox", { name: OTHER_CASE_ENTRY }));
  await user.click(panel.getByRole("button", { name: "Group findings" }));
  expect(await panel.findByText("Groups 1–200 of 201 across 2 cases")).toBeTruthy();
  expect(facade.oneCall("GroupDiagnoses")).toEqual([
    { workspace: WORKSPACE_ROOT, cases: [CASE_ENTRY, OTHER_CASE_ENTRY], builtin: "siu", offset: 0 },
  ]);
  const listed = within(groups()!);
  expect(listed.getByText(/signature signature-001-fixed-for-tests · 2 findings across cases/)).toBeTruthy();
  expect(listed.getByText(/signature signature-200-fixed-for-tests · 2 findings across cases/)).toBeTruthy();
  expect(listed.queryByText(/signature signature-201-fixed-for-tests/)).toBeNull();

  // The next window is of the grouping on screen, whatever the form holds.
  await user.selectOptions(panel.getByLabelText("Configuration"), "builtin:order");
  await user.click(panel.getByRole("checkbox", { name: THIRD_CASE }));
  await tabTo(user, panel.getByRole("button", { name: "Next 200 groups" }));
  await user.keyboard("{Enter}");
  expect(await panel.findByText("Groups 201–201 of 201 across 2 cases")).toBeTruthy();
  expect(within(groups()!).getByText(`f000001 of ${CASE_ENTRY} (${CASE_IDENTITY}), f000001 of ${OTHER_CASE_ENTRY} (${OTHER_IDENTITY})`)).toBeTruthy();
  expect(facade.callsTo("GroupDiagnoses")[1]?.args).toEqual([
    { workspace: WORKSPACE_ROOT, cases: [CASE_ENTRY, OTHER_CASE_ENTRY], builtin: "siu", offset: 200 },
  ]);
  expect((panel.getByRole("button", { name: "Next 200 groups" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(panel.getByRole("button", { name: "Previous 200 groups" }));
  expect(await panel.findByText("Groups 1–200 of 201 across 2 cases")).toBeTruthy();
  expect(facade.callsTo("GroupDiagnoses")[2]?.args[0]).toMatchObject({ builtin: "siu", offset: 0 });

  // A grouping the engine refuses is refused in its words, and no group of
  // the grouping before it stays on screen.
  facade.reply({ GroupDiagnoses: () => ({ ...refused(DUPLICATE), offset: 0, total: 0 }) });
  await user.click(panel.getByRole("button", { name: "Group findings" }));
  expect(await panel.findByText(DUPLICATE)).toBeTruthy();
  expect(groups()).toBeNull();
  expect(facade.callsTo("GroupDiagnoses")[3]?.args[0]).toMatchObject({
    cases: [CASE_ENTRY, OTHER_CASE_ENTRY, THIRD_CASE],
    builtin: "order",
  });

  // A grouping is interruptible: while it runs the window says so, holds the
  // panel, and offers its own Cancel, which asks the facade to cancel.
  const running = facade.park("GroupDiagnoses");
  await user.click(panel.getByRole("button", { name: "Group findings" }));
  expect(await panel.findByText("Grouping findings across these cases.")).toBeTruthy();
  expect((panel.getByRole("button", { name: "Group findings" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Open report" }) as HTMLButtonElement).disabled).toBe(true);
  const cancels = facade.callsTo("Cancel").length;
  const cancel = within(screen.getByRole("region", { name: "Status" })).getByRole("button", { name: /^Cancel$/ }) as HTMLButtonElement;
  expect(cancel.disabled).toBe(false);
  await user.click(cancel);
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""]]);
  running.resolve({ state: "cancelled", reason: CANCELLED, offset: 0, total: 0 });
  expect(await panel.findByText(CANCELLED)).toBeTruthy();
  expect(panel.queryByText("Grouping findings across these cases.")).toBeNull();
  expect(groups()).toBeNull();
  expect(within(screen.getByRole("region", { name: "Status" })).queryByRole("button", { name: /^Cancel$/ })).toBeNull();

  // Escape reaches the same Cancel while a grouping asked for from the
  // keyboard runs.
  panel.getByRole("checkbox", { name: THIRD_CASE }).focus();
  await tabTo(user, panel.getByRole("button", { name: "Group findings" }));
  await user.keyboard("{Enter}");
  expect(await panel.findByText("Grouping findings across these cases.")).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""], [""]]);
  running.resolve({ state: "cancelled", reason: CANCELLED, offset: 0, total: 0 });
  expect(await panel.findByText(CANCELLED)).toBeTruthy();
  expect(groups()).toBeNull();
});

test("a retained report is reopened through the facade with the identity a review names, a missing report and a grouping's report are refused leaving no findings, and another case's report opens none of its evidence in this case's inspector", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ InspectOccurrence: () => refused("not asked for") });
  const panel = await openCase(facade, user);

  // Reopening is a read the window says it is doing, and it holds the panel.
  const opening = facade.park("OpenDiagnosisReport");
  await user.selectOptions(panel.getByLabelText("Retained diagnosis report"), REPORT_ENTRY);
  await user.click(panel.getByRole("button", { name: "Open report" }));
  expect(await panel.findByText("Opening this report.")).toBeTruthy();
  expect((panel.getByRole("button", { name: "Open report" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.oneCall("OpenDiagnosisReport")).toEqual([WORKSPACE_ROOT, REPORT_ENTRY, 0]);
  opening.resolve(diagnosisResult(FINDINGS, { total: 201 }));
  expect(await panel.findByText(`rules message.duplicate-control-id, ack.msa-outcome · report identity ${REPORT_SHA256}`)).toBeTruthy();
  expect(panel.getByText("Findings 1–2 of 201")).toBeTruthy();
  expect(panel.queryByText(/was run over other evidence/)).toBeNull();

  // The next window is of the report opened, from the keyboard.
  const next = facade.park("OpenDiagnosisReport");
  await tabTo(user, panel.getByRole("button", { name: "Next 200" }));
  await user.keyboard("{Enter}");
  expect(facade.callsTo("OpenDiagnosisReport")[1]?.args).toEqual([WORKSPACE_ROOT, REPORT_ENTRY, 200]);
  next.resolve(diagnosisResult([diagnosisFinding("f000201")], { offset: 200, total: 201 }));
  expect(await panel.findByText("Findings 201–201 of 201")).toBeTruthy();

  // Its evidence opens in the inspector of the case it was run over.
  const [evidence] = panel.getAllByRole("button", { name: GRID_OCCURRENCE });
  await user.click(evidence!);
  expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: GRID_OCCURRENCE });
  const inspected = facade.callsTo("InspectOccurrence").length;

  // A report directory whose report.json is gone is refused in the reader's
  // words, and no finding of the report before it stays on screen.
  for (const [entry, sentence] of [
    [MISSING_REPORT, NO_REPORT],
  ] as const) {
    await openReport(facade, user, panel, entry);
    expect(await panel.findByText(sentence)).toBeTruthy();
    expect(panel.queryByText(/report identity /)).toBeNull();
    expect(panel.queryAllByRole("button", { name: GRID_OCCURRENCE })).toHaveLength(0);
  }

  // A report of another case is shown for what it is: its findings are
  // listed, and none of its evidence opens in this case's inspector.
  await openReport(facade, user, panel, OTHER_REPORT);
  expect(
    await panel.findByText(
      `This report was run over other evidence (case identity ${OTHER_IDENTITY}), not the case open here. Its evidence names that case's occurrences, so none of it opens in this case's inspector.`,
    ),
  ).toBeTruthy();
  for (const button of panel.getAllByRole("button", { name: GRID_OCCURRENCE })) {
    expect((button as HTMLButtonElement).disabled).toBe(true);
    await user.click(button);
  }
  expect(facade.callsTo("InspectOccurrence")).toHaveLength(inspected);

  // Reviewing it is the engine's to refuse, and it does, naming the remedy.
  facade.reply({ ReviewFindings: () => refused(OTHER_EVIDENCE) });
  await user.click(panel.getByRole("button", { name: "Preview" }));
  expect(await panel.findByText(OTHER_EVIDENCE)).toBeTruthy();
  expect(facade.oneCall("ReviewFindings")[0]).toMatchObject({ report: OTHER_REPORT, report_sha256: STALE_REPORT_SHA256 });
});

test("a review is previewed through the facade from the keyboard and writes nothing, a preview offers no draft, a change to the decisions withdraws it, and a report changed since it was shown is refused", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await openReport(facade, user, panel);
  await panel.findByText(/report identity /);

  await user.selectOptions(panel.getByLabelText("Decision", { selector: "#diagnosis-verdict-f000001" }), "confirmed");
  await user.type(panel.getByLabelText("Rationale"), "the scheduler must keep rejecting");
  const previewed = [
    findingStatus("f000001", "confirmed", { rationale: "the scheduler must keep rejecting", promotion: findingPromotion() }),
    findingStatus("f000002", "not_reviewed"),
  ];
  const previewing = facade.park("ReviewFindings");
  await tabTo(user, panel.getByRole("button", { name: "Preview" }));
  await user.keyboard("{Enter}");
  expect(await panel.findByText("Reviewing these findings.")).toBeTruthy();
  expect(facade.oneCall("ReviewFindings")).toEqual([
    {
      workspace: WORKSPACE_ROOT,
      case: CASE_ENTRY,
      identity: CASE_IDENTITY,
      report: REPORT_ENTRY,
      report_sha256: REPORT_SHA256,
      decisions: [{ finding: "f000001", verdict: "confirmed", rationale: "the scheduler must keep rejecting" }],
      offset: 0,
    },
  ]);
  previewing.resolve(findingReviewResult(previewed));
  const review = within(await panel.findByRole("region", { name: "Finding review" }));
  expect(review.getByText("The review, previewed (nothing written)")).toBeTruthy();
  expect(review.getByText(/Confirmed · by an explicit decision/)).toBeTruthy();
  expect(review.getByText(/Not reviewed · nobody has decided about it/)).toBeTruthy();
  // Nothing was written, so nothing can name this review as a draft's source.
  expect(review.queryByRole("button", { name: /^Draft a test from / })).toBeNull();
  expect(review.getByText(/Record these decisions to draft a test from it/)).toBeTruthy();
  expect(facade.callsTo("DecideFindings")).toHaveLength(0);
  expect(facade.callsTo("SaveFindingDecisions")).toHaveLength(0);

  // Changing a decision withdraws a preview of the decisions before it.
  await user.type(panel.getByLabelText("Rationale"), " again");
  expect(panel.queryByRole("region", { name: "Finding review" })).toBeNull();
  expect(panel.getByText("The decisions above changed since this review was previewed. Preview again to see what they mean.")).toBeTruthy();

  // The report changed on disk after it was shown: the preview is refused in
  // the facade's words, and nothing of the earlier preview returns.
  facade.reply({ ReviewFindings: () => refused(CHANGED) });
  await user.click(panel.getByRole("button", { name: "Preview" }));
  expect(await panel.findByText(CHANGED)).toBeTruthy();
  expect(panel.queryByRole("region", { name: "Finding review" })).toBeNull();

  // Recording names the review, and only then can a confirmed finding be
  // drafted from it.
  facade.reply({
    DecideFindings: () => findingReviewResult(previewed, {}, { output: "review-1", decisions_output: "decisions-2.json" }),
  });
  await user.type(panel.getByLabelText("New finding-review directory"), "review-1");
  await user.type(panel.getByLabelText("New decisions document"), "decisions-2.json");
  await user.click(panel.getByRole("button", { name: "Save decisions" }));
  const recorded = within(await panel.findByRole("region", { name: "Finding review" }));
  expect(recorded.getByText("The review")).toBeTruthy();
  expect(recorded.getByRole("button", { name: "Draft a test from f000001" })).toBeTruthy();
});

/** A retained decisions document as its reader decoded it. */
function decisionsDocument(report: string, decisions: FindingDecision[], sha256 = DECISIONS_SHA256): FindingDecisionsResult {
  return {
    state: "completed",
    sha256,
    document: JSON.stringify({ schema: "readmit-finding-decisions/v1", report_sha256: report, decisions }, null, 2),
    decisions: { schema: "readmit-finding-decisions/v1", report_sha256: report, decisions },
  };
}

const RETAINED: FindingDecision[] = [
  { finding: "f000001", verdict: "confirmed", rationale: "the scheduler must keep rejecting" },
  { finding: "f000002", verdict: "suppressed", scope: "case", rationale: "the vendor's own wording" },
];

test("a standalone decisions document is opened into the findings and saved through the facade, one recorded against another report is applied to nothing, an open over unsaved decisions asks first and Escape keeps them, and a refused document or save leaves the decisions", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await openReport(facade, user, panel);
  await panel.findByText(/report identity /);
  const section = within(panel.getByRole("region", { name: "Finding decisions document" }));
  const verdict = (finding: string) => (panel.getByLabelText("Decision", { selector: `#diagnosis-verdict-${finding}` }) as HTMLSelectElement).value;

  const retained = (_workspace: string, entry: string) =>
    entry === DECISIONS
      ? decisionsDocument(REPORT_SHA256, RETAINED)
      : entry === STALE_DECISIONS
        ? decisionsDocument(STALE_REPORT_SHA256, [{ finding: "f000001", verdict: "dismissed", rationale: "about another report" }])
        : entry === OUTSIDE_DECISIONS
          ? decisionsDocument(REPORT_SHA256, [...RETAINED, { finding: "f000009", verdict: "dismissed", rationale: "not in this window" }])
          : refused(UNDECIDED);

  // Opening a document about this report puts its decisions on the findings,
  // and names the entry and the identity of the bytes read.
  const opening = facade.park("OpenFindingDecisions");
  await user.selectOptions(section.getByLabelText("Retained decisions document"), DECISIONS);
  await user.click(section.getByRole("button", { name: "Open decisions" }));
  expect(await section.findByText(`Opening ${DECISIONS}.`)).toBeTruthy();
  expect((section.getByRole("button", { name: "Open decisions" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Preview" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByLabelText("Decision", { selector: "#diagnosis-verdict-f000001" }) as HTMLSelectElement).disabled).toBe(true);
  // Escape while it reads reaches the window's Cancel; what the facade then
  // answers is what is shown.
  const cancels = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""]]);
  opening.resolve(retained(WORKSPACE_ROOT, DECISIONS));
  facade.reply({ OpenFindingDecisions: retained });
  expect(await section.findByText(`Opened ${DECISIONS} · exact bytes hash to ${DECISIONS_SHA256}`)).toBeTruthy();
  expect(facade.callsTo("OpenFindingDecisions")[0]?.args).toEqual([WORKSPACE_ROOT, DECISIONS]);
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["confirmed", "suppressed"]);
  expect((panel.getByLabelText("Suppression scope") as HTMLSelectElement).value).toBe("case");
  expect(panel.getAllByLabelText("Rationale").map((field) => (field as HTMLInputElement).value)).toEqual([
    "the scheduler must keep rejecting",
    "the vendor's own wording",
  ]);

  // Opened and unchanged, another document opens without asking. One
  // recorded against another report is shown for what it is and applied to
  // nothing.
  await user.selectOptions(section.getByLabelText("Retained decisions document"), STALE_DECISIONS);
  await user.click(section.getByRole("button", { name: "Open decisions" }));
  expect(
    await section.findByText(
      `The decisions in ${STALE_DECISIONS} were recorded against a different diagnosis report (${STALE_REPORT_SHA256}); finding identifiers name other findings there, so none of them is applied to this report.`,
    ),
  ).toBeTruthy();
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["confirmed", "suppressed"]);

  // A changed decision is unsaved: opening another document asks first, and
  // Escape keeps the decisions, reads nothing and cancels nothing.
  await user.selectOptions(panel.getByLabelText("Decision", { selector: "#diagnosis-verdict-f000002" }), "dismissed");
  await user.selectOptions(section.getByLabelText("Retained decisions document"), REFUSED_DECISIONS);
  await user.click(section.getByRole("button", { name: "Open decisions" }));
  const question = within(section.getByRole("group", { name: `Open ${REFUSED_DECISIONS} in place of these decisions?` }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep these decisions" }));
  const opens = facade.callsTo("OpenFindingDecisions").length;
  const quiet = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(section.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(section.getByRole("button", { name: "Open decisions" }));
  expect(facade.callsTo("OpenFindingDecisions")).toHaveLength(opens);
  expect(facade.callsTo("Cancel")).toHaveLength(quiet);
  expect(verdict("f000002")).toBe("dismissed");

  // Keep these decisions, pressed, answers the same way.
  await user.keyboard("{Enter}");
  await user.click(section.getByRole("button", { name: "Keep these decisions" }));
  expect(section.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(facade.callsTo("OpenFindingDecisions")).toHaveLength(opens);

  // Answered the other way, the reader refuses the document in its own words
  // and the decisions on screen stay.
  await user.keyboard("{Enter}");
  await user.click(section.getByRole("button", { name: `Replace them with ${REFUSED_DECISIONS}` }));
  expect(await section.findByText(UNDECIDED)).toBeTruthy();
  expect(facade.callsTo("OpenFindingDecisions")).toHaveLength(opens + 1);
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["confirmed", "dismissed"]);

  // A save the facade refuses writes nothing and leaves them unsaved.
  facade.reply({ SaveFindingDecisions: () => ({ state: "permission_denied", reason: "no active license admits authoring" }) });
  await user.type(section.getByLabelText("New finding-decisions entry"), "decisions-3.json{Enter}");
  expect(await section.findByText("no active license admits authoring")).toBeTruthy();
  expect((section.getByLabelText("New finding-decisions entry") as HTMLInputElement).value).toBe("decisions-3.json");

  // Saved while the question is open, the decisions are no longer unsaved:
  // the question is withdrawn. The document is the decisions on screen bound
  // to this report, the entry and its identity are named, and the listing is
  // read again.
  await user.click(section.getByRole("button", { name: "Open decisions" }));
  expect(section.getByRole("group", { name: `Open ${REFUSED_DECISIONS} in place of these decisions?` })).toBeTruthy();
  const saving = facade.park("SaveFindingDecisions");
  const listings = facade.callsTo("OpenWorkspace").length;
  await user.click(section.getByRole("button", { name: "Save as new" }));
  expect(await section.findByText("Saving decisions-3.json.")).toBeTruthy();
  expect((section.getByRole("button", { name: "Save as new" }) as HTMLButtonElement).disabled).toBe(true);
  const [request] = facade.callsTo("SaveFindingDecisions").at(-1)!.args as [{ workspace: string; document: string; output: string }];
  expect(request.workspace).toBe(WORKSPACE_ROOT);
  expect(request.output).toBe("decisions-3.json");
  const composed = {
    schema: "readmit-finding-decisions/v1",
    report_sha256: REPORT_SHA256,
    decisions: [
      { finding: "f000001", verdict: "confirmed", rationale: "the scheduler must keep rejecting" },
      { finding: "f000002", verdict: "dismissed", rationale: "the vendor's own wording" },
    ],
  };
  expect(JSON.parse(request.document)).toEqual(composed);
  saving.resolve({ state: "completed", output: "decisions-3.json", sha256: "saved-sha256-fixed-for-tests", document: request.document, decisions: composed });
  facade.reply({ SaveFindingDecisions: () => refused("not asked for") });
  expect(await section.findByText("Saved to decisions-3.json · exact bytes hash to saved-sha256-fixed-for-tests")).toBeTruthy();
  expect(section.queryByRole("group", { name: /^Open / })).toBeNull();
  await waitFor(() => expect(facade.callsTo("OpenWorkspace").length).toBeGreaterThan(listings));
  expect((section.getByLabelText("New finding-decisions entry") as HTMLInputElement).value).toBe("");
  await user.selectOptions(section.getByLabelText("Retained decisions document"), OUTSIDE_DECISIONS);
  await user.click(section.getByRole("button", { name: "Open decisions" }));
  expect(section.queryByRole("group", { name: /^Open / })).toBeNull();

  // A decision about a finding this window does not list is shown, part of
  // the review, until it is forgotten.
  const outside = within(await panel.findByRole("region", { name: "Unlisted decisions" }));
  expect(outside.getByText("f000009")).toBeTruthy();
  facade.reply({ ReviewFindings: () => refused("a decision names a finding this diagnosis report does not hold") });
  await user.click(panel.getByRole("button", { name: "Preview" }));
  expect(facade.callsTo("ReviewFindings").at(-1)?.args[0]).toMatchObject({
    decisions: [...RETAINED, { finding: "f000009", verdict: "dismissed", rationale: "not in this window" }],
  });
  await user.click(outside.getByRole("button", { name: "Remove decision" }));
  expect(panel.queryByRole("region", { name: "Unlisted decisions" })).toBeNull();
  expect(section.getByText(/"finding": "f000002"/)).toBeTruthy();
  expect(section.queryByText(/"finding": "f000009"/)).toBeNull();
});

// The built-in configurations offered and the size of one window of findings
// are what the facade publishes, not a copy the panel keeps: a vocabulary with
// one built-in and a window of 50 findings is exactly what the panel offers.
test("the built-in configurations and the findings window are the ones the facade publishes", async () => {
  const user = userEvent.setup();
  const opened: [string, number][] = [];
  const vocabulary = {
    ...vocabularyFixture({ diagnosis: 50 }),
    diagnosis_builtins: [{ id: "order", profile: "readmit-order-v1", ruleset: "readmit-order-diagnosis/v1" }],
  };
  const findings = Array.from({ length: 50 }, (_, index) => diagnosisFinding(`f${String(index + 51).padStart(6, "0")}`));
  renderAlone(
    <Diagnosis
      workspace={WORKSPACE_ROOT}
      caseName={CASE_ENTRY}
      identity={CASE_IDENTITY}
      configEntries={[]}
      reportEntries={[REPORT_ENTRY]}
      groupsReportEntries={[]}
      caseEntries={[CASE_ENTRY]}
      result={diagnosisResult(findings, { offset: 50, total: 150 })}
      groupsResult={null}
      reviewResult={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onRun={() => undefined}
      onOpen={(entry, offset) => opened.push([entry, offset])}
      onGroup={() => undefined}
      onOpenGroups={() => undefined}
      onReview={() => undefined}
      onSelect={() => undefined}
      onPromote={() => undefined}
    />,
    { wrapper: vocabularyWrapper(vocabulary) },
  );
  const configuration = screen.getByLabelText("Configuration") as HTMLSelectElement;
  expect(Array.from(configuration.options).map((option) => option.value).filter((value) => value.startsWith("builtin:"))).toEqual([
    "builtin:order",
  ]);
  await user.selectOptions(screen.getByLabelText("Retained diagnosis report"), REPORT_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open report" }));
  await user.click(screen.getByRole("button", { name: "Next 50" }));
  await user.click(screen.getByRole("button", { name: "Previous 50" }));
  expect(opened.map(([, offset]) => offset)).toEqual([0, 100, 0]);
});
