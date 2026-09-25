// The run-explanation panel: a retained run and an assertion set chosen
// through the host's dialogs or named as entries, re-decided by the facade the
// way `readmit explain` re-decides them. The panel draws what the facade
// answered — every outcome in its own word, a refusal as a refusal — and never
// a pass it was not told. A dismissed dialog changes nothing, a changed input
// withdraws the answer on screen, values appear only on purpose, and a cancel
// names the panel's own operation. Every fixture here carries positions,
// states and counts, never an HL7 value, a machine path or an address.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RunExplanation } from "./RunExplanation";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact, ExplainedAssertion, RunExplanation as Explanation, RunExplanationResult } from "./bindings";
import { WORKSPACE_ROOT } from "./testkit/fixtures";

const ENTRIES: Artifact[] = [
  { name: "case", kind: "case", schema: "readmit-case/v3", provenance: "generated" },
  { name: "reschedule-test.json", kind: "spec" },
  { name: "job-001", kind: "job" },
  { name: "post-fix", kind: "result" },
];

const IDENTITY = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

function assertion(id: string, outcome: string | undefined, revealed = false): ExplainedAssertion {
  return {
    id,
    operator: "field_equals",
    ...(outcome ? { outcome } : {}),
    reads: "MSA-1 of observed message s0001-e000001",
    expected: revealed ? 'present, text "expected-value"' : "present, text hidden",
    observed: outcome ? (revealed ? 'present, text "observed-value"' : "present, text hidden") : "not evaluated",
    evidence: ["observed MSA-1 of s0001-e000001: post-fix/run/payloads/o000001-received.bin"],
  };
}

/** One completed explanation, as the facade answers it. */
function explained(overrides: Partial<Explanation> = {}): RunExplanationResult {
  return {
    state: "completed",
    explanation: {
      run: "post-fix",
      bundle: "post-fix/run",
      verdict: "pass",
      declared: 1,
      passed: 1,
      failed: 0,
      undecided: 0,
      skipped: 0,
      set_name: "Reschedule accepted downstream",
      set_schema: "readmit-assertion-set/v1",
      set_identity: IDENTITY,
      run_schema: "readmit-run/v1",
      run_state: "complete",
      run_identity: IDENTITY,
      source_identity: IDENTITY,
      contains_source_values: true,
      export_policy: "customer-local-only",
      target: "",
      transport: "plain",
      target_identity: IDENTITY,
      started_at: "2026-01-01T12:00:00Z",
      completed_at: "2026-01-01T12:00:01Z",
      elapsed: "1s",
      messages: [
        {
          source: "s0001-e000001",
          outbound: "o000001",
          outcome: "application_accepted",
          delivery: "acknowledged",
          ack_code: "AA",
          ack_correlation: "matched",
          elapsed: "10ms",
          input: "payloads/o000001-sent.bin",
          observed: "payloads/o000001-received.bin",
        },
      ],
      observations: [],
      assertions: [assertion("booking-accepted", "passed")],
      revealed: false,
      ...overrides,
    },
  };
}

function renderPanel(handlers: FacadeHandlers = {}) {
  const facade = installFacade({ Cancel: async () => {}, ...handlers });
  render(<RunExplanation workspace={WORKSPACE_ROOT} entries={ENTRIES} busy={false} />);
  return facade;
}

/** The row the assertions table draws for one assertion, cell by cell. */
function row(id: string): string[] {
  const table = screen.getByRole("table", { name: /^Assertions as this run's evidence decided them/ });
  const found = within(table).getByRole("row", { name: new RegExp(`^${id} `) });
  return Array.from(found.children).map((cell) => cell.textContent ?? "");
}

test("a retained run and an assertion set chosen through the host's dialogs are explained assertion by assertion, and values appear only on purpose", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({
    ChooseExplanationInput: (_workspace, kind) => ({ state: "completed", kind, entry: kind === "run" ? "post-fix" : "reschedule-assertions.json" }),
    ExplainRun: (request) =>
      explained(
        request.reveal
          ? { revealed: true, assertions: [assertion("booking-accepted", "passed", true)] }
          : {},
      ),
  });
  // The retained executions of the workspace are offered as entries.
  const offered = Array.from(document.querySelectorAll("#explain-run-options option")).map((option) => option.getAttribute("value"));
  expect(offered).toEqual(["job-001", "post-fix"]);
  expect((screen.getByRole("button", { name: "Explain" }) as HTMLButtonElement).disabled).toBe(true);

  await user.click(screen.getByRole("button", { name: "Choose run folder…" }));
  await waitFor(() => expect((screen.getByLabelText("Retained run") as HTMLInputElement).value).toBe("post-fix"));
  await user.click(screen.getByRole("button", { name: "Choose assertion set…" }));
  await waitFor(() => expect((screen.getByLabelText("Assertion set") as HTMLInputElement).value).toBe("reschedule-assertions.json"));
  expect(facade.callsTo("ChooseExplanationInput").map((call) => call.args)).toEqual([
    [WORKSPACE_ROOT, "run"],
    [WORKSPACE_ROOT, "assertions"],
  ]);

  await user.click(screen.getByRole("button", { name: "Explain" }));
  expect(await screen.findByText("Verdict: pass")).toBeTruthy();
  expect(facade.oneCall("ExplainRun")).toEqual([
    { workspace: WORKSPACE_ROOT, run: "post-fix", assertions: "reschedule-assertions.json", reveal: false },
  ]);
  expect(screen.getByText("Assertions: 1 declared; 1 passed, 0 failed, 0 undecided, 0 skipped")).toBeTruthy();
  expect(screen.getByText(`Assertion set: Reschedule accepted downstream · readmit-assertion-set/v1 · identity ${IDENTITY}`)).toBeTruthy();
  expect(screen.getByText(`Run: post-fix/run · readmit-run/v1 · complete · identity ${IDENTITY}`)).toBeTruthy();
  expect(row("booking-accepted")).toEqual([
    "booking-accepted",
    "field_equals",
    "passed",
    "MSA-1 of observed message s0001-e000001",
    "present, text hidden",
    "present, text hidden",
    "observed MSA-1 of s0001-e000001: post-fix/run/payloads/o000001-received.bin",
  ]);
  expect(screen.getByText(/It opened nothing else, sent nothing and wrote nothing\./)).toBeTruthy();

  // Revealing is a second, deliberate reading; hiding is a third.
  await user.click(screen.getByRole("button", { name: "Show values" }));
  await screen.findByRole("table", { name: /\(values revealed\)$/ });
  expect(row("booking-accepted")[4]).toBe('present, text "expected-value"');
  await user.click(screen.getByRole("button", { name: "Hide values" }));
  await screen.findByRole("table", { name: /\(values hidden until revealed\)$/ });
  expect(facade.callsTo("ExplainRun").map((call) => (call.args[0] as { reveal: boolean }).reveal)).toEqual([false, true, false]);
});

test("undecided, skipped, unevaluated and refused answers are never shown as passes", async () => {
  const user = userEvent.setup();
  const facade = renderPanel();
  await user.type(screen.getByLabelText("Retained run"), "post-fix");
  await user.type(screen.getByLabelText("Assertion set"), "mixed.json");

  facade.reply({
    ExplainRun: () =>
      explained({
        verdict: "fail",
        declared: 4,
        passed: 1,
        failed: 1,
        undecided: 1,
        skipped: 1,
        assertions: [
          assertion("booking-accepted", "passed"),
          assertion("reschedule-rejected", "failed"),
          assertion("name-in-range", "undecided"),
          {
            ...assertion("only-when-rejected", "skipped"),
            condition: "MSA-1 of observed message s0001-e000001 equals present, text hidden",
            observed: "not read; the condition did not hold, so this assertion asserted nothing",
          },
        ],
      }),
  });
  await user.click(screen.getByRole("button", { name: "Explain" }));
  expect(await screen.findByText("Verdict: fail")).toBeTruthy();
  expect(["booking-accepted", "reschedule-rejected", "name-in-range", "only-when-rejected"].map((id) => row(id)[2])).toEqual([
    "passed",
    "failed",
    "undecided",
    "skipped",
  ]);
  expect(row("only-when-rejected")[3]).toBe(
    "MSA-1 of observed message s0001-e000001, when MSA-1 of observed message s0001-e000001 equals present, text hidden",
  );
  expect(screen.getByText(/Unknown is a third answer and it is not a pass/)).toBeTruthy();

  // An occurrence the run retained nothing for decides nothing at all.
  const unobserved = explained({
    error_class: "unknown_message",
    error_assertion: "never-sent",
    declared: 2,
    passed: 0,
    assertions: [
      assertion("booking-accepted", undefined),
      {
        ...assertion("never-sent", undefined),
        evidence: ["observed MSA-1 of s0001-e000404: this run retained no readable payload for that occurrence"],
      },
    ],
  });
  delete unobserved.explanation?.verdict;
  facade.reply({ ExplainRun: () => unobserved });
  await user.click(screen.getByRole("button", { name: "Explain" }));
  expect(await screen.findByText("Verdict: none, execution error unknown_message at assertion never-sent")).toBeTruthy();
  expect(screen.queryByText("Verdict: pass")).toBeNull();
  expect(row("booking-accepted")[2]).toBe("not evaluated");
  expect(row("never-sent")[6]).toMatch(/this run retained no readable payload for that occurrence$/);

  // A refusal is the command's sentence, and no explanation stands beside it.
  facade.reply({ ExplainRun: () => ({ state: "failed", reason: "an assertion set must declare readmit-assertion-set/v1" }) });
  await user.click(screen.getByRole("button", { name: "Explain" }));
  expect(await screen.findByText("Not explained: an assertion set must declare readmit-assertion-set/v1")).toBeTruthy();
  expect(screen.getByText("Nothing was decided. A refusal is neither a pass nor a failure.")).toBeTruthy();
  expect(screen.queryByText(/^Verdict:/)).toBeNull();
  expect(screen.queryByRole("table")).toBeNull();

  // So is the window's own refusal of a slot another operation holds.
  facade.reply({ ExplainRun: () => ({ state: "busy", reason: "another operation is already running" }) });
  await user.click(screen.getByRole("button", { name: "Explain" }));
  expect(await screen.findByText("another operation is already running")).toBeTruthy();
  expect(screen.queryByText(/^Verdict:/)).toBeNull();
});

test("a dismissed dialog and a refused choice leave the inputs as they were, and a changed input withdraws the explanation", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({ ExplainRun: () => explained() });
  await user.type(screen.getByLabelText("Retained run"), "post-fix");
  await user.type(screen.getByLabelText("Assertion set"), "reschedule-assertions.json");
  await user.click(screen.getByRole("button", { name: "Explain" }));
  await screen.findByText("Verdict: pass");

  facade.reply({ ChooseExplanationInput: () => ({ state: "cancelled", reason: "no folder was chosen" }) });
  await user.click(screen.getByRole("button", { name: "Choose run folder…" }));
  expect(await screen.findByText("no folder was chosen")).toBeTruthy();
  expect((screen.getByLabelText("Retained run") as HTMLInputElement).value).toBe("post-fix");

  facade.reply({
    ChooseExplanationInput: () => ({
      state: "failed",
      reason: "an explanation reads entries of the open workspace; advanced selection cannot reach outside it",
    }),
  });
  await user.click(screen.getByRole("button", { name: "Choose assertion set…" }));
  expect(await screen.findByText(/advanced selection cannot reach outside it/)).toBeTruthy();
  expect((screen.getByLabelText("Assertion set") as HTMLInputElement).value).toBe("reschedule-assertions.json");

  // The answer on screen was about the entries it was asked for; editing one
  // withdraws it, and nothing is explained until asked again.
  await user.click(screen.getByRole("button", { name: "Explain" }));
  await screen.findByText("Verdict: pass");
  await user.type(screen.getByLabelText("Retained run"), "-copy");
  expect(screen.queryByText("Verdict: pass")).toBeNull();
  expect(facade.callsTo("ExplainRun")).toHaveLength(2);
});

test("both observations' documents are chosen natively for a set that asks about records, handed over together, and typed over", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({
    ChooseExplanationInput: (_workspace, kind) => ({ state: "completed", kind, entry: `${kind}.json` }),
    ExplainRun: () =>
      explained({
        declared: 1,
        observations: [
          {
            scope: "after",
            status: "complete",
            schema: "readmit-observation-completion/v1",
            source_schema: "readmit-observation-source/v2",
            window: IDENTITY,
            source_kind: "downstream-capture",
            source_identity: "integration-sink",
            source_scope: "appointments",
            records: 1,
            correlations: "none recorded, so nothing in this record binds the observation to this run",
            capture: "downstream.case",
            keys: "1, hidden",
          },
        ],
        assertions: [{ ...assertion("one-appointment", "passed"), operator: "record_count", reads: "the records of the after observation" }],
      }),
  });
  await user.type(screen.getByLabelText("Retained run"), "post-fix");
  await user.type(screen.getByLabelText("Assertion set"), "records.json");
  await user.click(screen.getByText("Observed records"));
  await user.click(screen.getByRole("button", { name: "Choose before completion…" }));
  await waitFor(() => expect((screen.getByLabelText("Completion record before the run") as HTMLInputElement).value).toBe("before.json"));
  await user.click(screen.getByRole("button", { name: "Choose before source…" }));
  await waitFor(() => expect((screen.getByLabelText("Observation source before the run") as HTMLInputElement).value).toBe("before-source.json"));
  await user.click(screen.getByRole("button", { name: "Choose after completion…" }));
  await waitFor(() => expect((screen.getByLabelText("Completion record after the run") as HTMLInputElement).value).toBe("after.json"));
  await user.click(screen.getByRole("button", { name: "Choose after source…" }));
  await waitFor(() => expect((screen.getByLabelText("Observation source after the run") as HTMLInputElement).value).toBe("after-source.json"));
  await user.click(screen.getByRole("button", { name: "Explain" }));
  expect(await screen.findByText("Records derived again from: downstream.case")).toBeTruthy();
  expect(screen.getByText("Keys: 1, hidden")).toBeTruthy();
  expect(facade.oneCall("ExplainRun")).toEqual([
    {
      workspace: WORKSPACE_ROOT,
      run: "post-fix",
      assertions: "records.json",
      before: "before.json",
      before_source: "before-source.json",
      after: "after.json",
      after_source: "after-source.json",
      reveal: false,
    },
  ]);
  expect(facade.callsTo("ChooseExplanationInput").map((call) => call.args[1])).toEqual(["before", "before-source", "after", "after-source"]);

  // A document typed over a chosen one is the one handed over, and clearing
  // one hands over nothing for it.
  await user.clear(screen.getByLabelText("Completion record before the run"));
  await user.clear(screen.getByLabelText("Observation source before the run"));
  await user.type(screen.getByLabelText("Completion record after the run"), "{Backspace}{Backspace}{Backspace}{Backspace}{Backspace}-stale.json");
  await user.click(screen.getByRole("button", { name: "Explain" }));
  await waitFor(() => expect(facade.callsTo("ExplainRun")).toHaveLength(2));
  expect(facade.callsTo("ExplainRun")[1]?.args[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    run: "post-fix",
    assertions: "records.json",
    after: "after-stale.json",
    after_source: "after-source.json",
    reveal: false,
  });
});

/** Moves focus with Tab until the control has it, as a keyboard user does,
 * and fails when the control is not reachable that way at all. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 60; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? control.id} is not reachable with Tab`);
}

test("the explanation is driven from the keyboard, and its cancel names its own operation and discards the late answer", async () => {
  const user = userEvent.setup();
  const cancels: string[] = [];
  const facade = renderPanel({ Cancel: async (operation) => void cancels.push(operation) });
  const parked = facade.park("ExplainRun");
  await tabTo(user, screen.getByLabelText("Retained run"));
  await user.keyboard("post-fix");
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Choose run folder…" }));
  await user.tab();
  expect(document.activeElement).toBe(screen.getByLabelText("Assertion set"));
  await user.keyboard("reschedule-assertions.json{Enter}");
  expect(await screen.findByText("Re-deciding the set against the run's retained evidence…")).toBeTruthy();
  expect(parked.size).toBe(1);

  // Every other control is disabled while the set is re-decided, so the
  // keyboard lands on the cancel, which names this panel's operation.
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel explanation" })));
  await user.keyboard("{Enter}");
  expect(cancels).toEqual(["run-explanation"]);
  parked.resolve(explained({ set_name: "A LATE ANSWER" }));
  expect(await screen.findByText("The explanation was cancelled. It retained nothing; explain again to decide it.")).toBeTruthy();
  await waitFor(() => expect((screen.getByRole("button", { name: "Cancel explanation" }) as HTMLButtonElement).disabled).toBe(true));
  expect(screen.queryByText(/A LATE ANSWER/)).toBeNull();

  // Explaining again from the keyboard decides it, and the reveal is reached
  // and pressed the same way.
  facade.reply({ ExplainRun: (request) => explained({ revealed: request.reveal }) });
  await tabTo(user, screen.getByRole("button", { name: "Explain" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Verdict: pass")).toBeTruthy();
  await tabTo(user, screen.getByRole("button", { name: "Show values" }));
  await user.keyboard(" ");
  expect(await screen.findByRole("button", { name: "Hide values" })).toBeTruthy();
  expect(facade.callsTo("ExplainRun").map((call) => (call.args[0] as { reveal: boolean }).reveal)).toEqual([false, false, true]);
});
