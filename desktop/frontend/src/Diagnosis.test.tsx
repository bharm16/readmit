// The diagnosis panel, pinned over its typed callbacks: which request a
// person's act produces and what the panel does with the engine's answer.
// Domain meaning — what a finding is, what a verdict covers, what a promotion
// may express — stays with the Go engine and its own tests; nothing here
// re-decides any of it.
import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Diagnosis } from "./Diagnosis";
import type {
  DiagnosisRequest,
  DiagnosisResult,
  DiagnosisGroupsResult,
  FindingReviewRequest,
  FindingReviewResult,
  FindingStatus,
  GroupDiagnosesRequest,
} from "./bindings";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  OTHER_CASE_ENTRY,
  REPORT_ENTRY,
  REPORT_SHA256,
  WORKSPACE_ROOT,
  diagnosisFinding,
  diagnosisGroupsResult,
  diagnosisResult,
  findingPromotion,
  findingReviewResult,
  findingStatus,
  indicatorTable,
} from "./testkit/fixtures";

type Callbacks = {
  onRun?: (request: DiagnosisRequest) => void;
  onOpen?: (entry: string, offset: number) => void;
  onGroup?: (request: GroupDiagnosesRequest) => void;
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
  const run = () => screen.getByRole("button", { name: "Run this diagnosis" }) as HTMLButtonElement;
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
  await user.click(screen.getByRole("button", { name: "Run this diagnosis" }));
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
  expect(screen.getByText(new RegExp(REPORT_SHA256))).toBeTruthy();
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
  await user.click(screen.getByRole("button", { name: "Preview the review (writes nothing)" }));
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
    screen.getByRole("button", { name: "Record these finding decisions" }) as HTMLButtonElement;
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
  await user.click(screen.getByRole("button", { name: "Group findings across these cases" }));
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
      caseEntries={[CASE_ENTRY, OTHER_CASE_ENTRY]}
      result={null}
      groupsResult={diagnosisGroupsResult([
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
      ])}
      reviewResult={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onRun={() => undefined}
      onOpen={() => undefined}
      onGroup={() => undefined}
      onReview={() => undefined}
      onSelect={() => undefined}
      onPromote={() => undefined}
    />,
  );
  expect(
    screen.getByText("Equal signatures mean the same diagnostic shape, never the same root cause."),
  ).toBeTruthy();
  expect(screen.getByText(/signature signature-fixed-for-tests · 2 findings across cases/)).toBeTruthy();
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
  await user.click(screen.getByRole("button", { name: "Open this report" }));
  expect(opened).toEqual([[REPORT_ENTRY, 0]]);
  await user.click(screen.getByRole("button", { name: "Manage interface profiles…" }));
  expect(managed).toBe(1);
});
