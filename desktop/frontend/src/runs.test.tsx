// The run journeys: a run is selected from what the workspace actually holds,
// preflighted locally, executed once under the identity the preflight fixed,
// watched through typed progress, and its retained evidence reopens read-only
// with values revealed only on purpose. A cancel names its own operation, a
// duplicate click starts nothing, and recovery never sends.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Recovery } from "./Recovery";
import { RunPanel } from "./RunPanel";
import { RunComparison } from "./RunComparison";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact } from "./bindings";
import {
  durableRunResult,
  recoveryResult,
  retainedDraft,
  runComparisonResult,
  runEvidenceResult,
  runPreflightResult,
  runProgressResult,
  runSpecChoice,
  WORKSPACE_ROOT,
} from "./testkit/fixtures";

const SPEC_ENTRY = "reschedule-test.json";
const SUITE_ENTRY = "nightly.json";
const RUN_ENTRY = "job-001";

const ENTRIES: Artifact[] = [
  { name: "case", kind: "case", schema: "readmit-case/v3", provenance: "generated" },
  { name: SPEC_ENTRY, kind: "spec" },
  { name: SUITE_ENTRY, kind: "suite" },
  { name: RUN_ENTRY, kind: "job" },
];

function renderPanel(
  handlers: FacadeHandlers = {},
  entries: Artifact[] = ENTRIES,
  initialSpec?: string,
) {
  const events: string[] = [];
  const facade = installFacade({
    Cancel: async () => {
      events.push("cancel");
    },
    ...handlers,
  });
  const mounted = render(
    <RunPanel
      workspace={WORKSPACE_ROOT}
      entries={entries}
      onWatch={async (folder) => {
        events.push(`watch:${folder}`);
      }}
      onRefresh={() => events.push("refresh")}
      onOpenCase={(name) => events.push(`case:${name}`)}
      {...(initialSpec ? { initialSpec } : {})}
    />,
  );
  return { facade, events, ...mounted };
}

test("a saved test is selected from the workspace, preflighted locally, and the preflight names what would run", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreflightRun: (request) => {
      expect(request.workspace).toBe(WORKSPACE_ROOT);
      expect(request.spec).toBe(SPEC_ENTRY);
      return runPreflightResult();
    },
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  expect(await screen.findByText("Preflight — Rescheduling updates the original appointment (test)")).toBeTruthy();
  // Local validation only: nothing was sent and no verdict exists.
  expect(screen.getByText(/No message was sent, nothing was reset, and no result exists yet/)).toBeTruthy();
  expect(screen.getAllByText(/unclassified/).length).toBeGreaterThan(0);
  expect(screen.getByText(/^Admission: admitted/)).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});

test("execution is offered only under a fresh preflight, and a changed selection withdraws it", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({ PreflightRun: () => runPreflightResult() });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  await screen.findByText("Preflight — Rescheduling updates the original appointment (test)");
  // Changing the selection invalidates the preflight: nothing can execute.
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SUITE_ENTRY);
  expect(screen.queryByRole("button", { name: "Send test" })).toBeNull();
  expect(screen.queryByText(/Preflight —/)).toBeNull();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});

test("a denied admission shows the backend's reason and executes nothing", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreflightRun: () =>
      runPreflightResult({
        admission: { admitted: false, reason: "the operation term is not active" },
      }),
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  await screen.findByText(/the operation term is not active/);
  expect(screen.queryByRole("button", { name: "Send test" })).toBeNull();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
});

test("send once: the folder is named in the session before the send, the pinned identity is executed, and the retained evidence opens read-only", async () => {
  const user = userEvent.setup();
  const { facade, events } = renderPanel({
    PreflightRun: () => runPreflightResult(),
    StartDurableRun: (request) => {
      expect(request).toEqual({
        workspace: WORKSPACE_ROOT,
        spec: SPEC_ENTRY,
        output: "job-001",
        expected_identity: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      });
      return durableRunResult("passed");
    },
    DurableRunProgress: () => runProgressResult(),
    OpenRunEvidence: (request) => {
      expect(request.entry).toBe("job-001");
      return runEvidenceResult();
    },
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  await screen.findByText(/Destination: job-001 \(generated\) · fresh/);
  const parked = facade.park("StartDurableRun");
  await user.click(screen.getByRole("button", { name: "Send test" }));
  await screen.findByText(/Running\. Cancellation stops future sends/);
  // The watch is recorded before the send starts.
  expect(events[0]).toBe("watch:job-001");
  expect(facade.callsTo("StartDurableRun")).toHaveLength(1);
  parked.resolve(durableRunResult("passed"));
  await screen.findByText(/Stop reason: passed/);
  expect(events).toContain("refresh");
  // The retained evidence opened beside the result, values hidden.
  expect(screen.getByText(/hidden until revealed/)).toBeTruthy();
  expect(facade.callsTo("OpenRunEvidence")).toHaveLength(1);
});

test("a duplicate click while a send is running starts nothing", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreflightRun: () => runPreflightResult(),
    StartDurableRun: () => durableRunResult("passed"),
    DurableRunProgress: () => runProgressResult({ executing: true, phase: "executing" }),
    OpenRunEvidence: () => runEvidenceResult(),
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  const parked = facade.park("StartDurableRun");
  await user.click(screen.getByRole("button", { name: "Send test" }));
  // The button is disabled while the send holds the operation, so a second
  // click can press nothing.
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Send test" }) as HTMLButtonElement).disabled,
    ).toBe(true),
  );
  await user.click(screen.getByRole("button", { name: "Send test" }));
  expect(facade.callsTo("StartDurableRun")).toHaveLength(1);
  parked.resolve(durableRunResult("passed"));
  await screen.findByText(/Stop reason: passed/);
});

test("Cancel names the durable-run operation, so it cannot stop another panel's work", async () => {
  const user = userEvent.setup();
  const facade = renderPanel({
    PreflightRun: () => runPreflightResult(),
    StartDurableRun: () => durableRunResult("passed"),
    DurableRunProgress: () => runProgressResult(),
    OpenRunEvidence: () => runEvidenceResult(),
  }).facade;
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  expect(
    (screen.getByRole("button", { name: "Cancel run" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  const parked = facade.park("StartDurableRun");
  await user.click(screen.getByRole("button", { name: "Send test" }));
  await screen.findByText(/Cancellation stops future sends/);
  await user.click(screen.getByRole("button", { name: "Cancel run" }));
  expect(facade.oneCall("Cancel")).toEqual(["durable-run"]);
  parked.resolve(durableRunResult("cancelled"));
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Cancel run" }) as HTMLButtonElement).disabled,
    ).toBe(true),
  );
});

test("run history opens retained evidence read-only and reveals values only on purpose", async () => {
  const user = userEvent.setup();
  const { facade, events } = renderPanel({
    DurableRunProgress: () =>
      runProgressResult({ phase: "interrupted", acknowledged: 0, uncertain: 1, lease: "held" }),
    OpenRunEvidence: (request) =>
      runEvidenceResult({
        revealed: request.reveal,
        run_state: "interrupted",
        stop_reason: "interrupted",
        recovered: true,
        delivery_uncertain: true,
        ...(request.reveal
          ? {
              assertions: [
                { id: "ack", operator: "ack_field_equals", message: "s0001-e000001", selector: "MSA-1", status: "not_evaluated", expected: '{"field":{"state":"present","text":"REVEALED-EXPECTED"}}', evidence: "unobserved" },
              ],
            }
          : {}),
      }),
  });
  await user.selectOptions(screen.getByLabelText("Select run"), RUN_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  expect(await screen.findByText(/Recovery never resumes, resets or resends/)).toBeTruthy();
  // Completion was not recorded and the delivery is uncertain: both facts are
  // shown and neither became a verdict.
  expect(screen.getByText(/completion was not recorded/)).toBeTruthy();
  expect(screen.getAllByText(/uncertain/).length).toBeGreaterThan(0);
  expect(screen.getAllByText("hidden").length).toBeGreaterThan(0);
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  // The source case is a link into the inspector, not a value.
  await user.click(screen.getByRole("button", { name: "regression" }));
  expect(events).toEqual(["case:regression"]);
  // The deliberate reveal asks again and only then sees values.
  await user.click(screen.getByRole("button", { name: "Show values" }));
  await screen.findByText(/values revealed/);
  const evidenceCalls = facade.callsTo("OpenRunEvidence");
  expect(evidenceCalls).toHaveLength(2);
  expect(evidenceCalls[1]?.args[0]).toMatchObject({ entry: RUN_ENTRY, reveal: true });
  expect(screen.getByText(/REVEALED-EXPECTED/)).toBeTruthy();
});

test("a suite is selected, its environment is chosen from what it declares, and it executes through the queue", async () => {
  const user = userEvent.setup();
  const { facade, events } = renderPanel({
    PreflightRun: (request) => {
      expect(request.spec).toBe(SUITE_ENTRY);
      if (!request.environment) {
        return runPreflightResult({
          kind: "suite",
          spec: SUITE_ENTRY,
          schema: "readmit-suite/v1",
          name: "nightly",
          selected: [],
          suite: {
            id: "nightly",
            environments: ["east", "west"],
            parallelism: 1,
            jobs: [{ id: "booking", spec: SPEC_ENTRY, parameter: "scheduling", isolation: "shared", after: [], rows: 1, sequence: 1 }],
            targets: [],
          },
          admission: { admitted: false, reason: "select one of the environments the suite declares" },
        });
      }
      expect(request.environment).toBe("east");
      return runPreflightResult({
        kind: "suite",
        spec: SUITE_ENTRY,
        schema: "readmit-suite/v1",
        name: "nightly",
        selected: [],
        suite: {
          id: "nightly",
          environments: ["east", "west"],
          environment: "east",
          site: "hospital-a",
          parallelism: 1,
          jobs: [{ id: "booking", spec: SPEC_ENTRY, parameter: "scheduling", isolation: "shared", after: [], rows: 1, sequence: 1 }],
          targets: [],
        },
      });
    },
    StartSuiteRun: (request) => {
      expect(request).toEqual({
        workspace: WORKSPACE_ROOT,
        suite: SUITE_ENTRY,
        environment: "east",
        output: "job-001",
        expected_identity: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      });
      const summary = durableRunResult("passed").run;
      return {
        state: "completed",
        output: "suite-run",
        report: {
          schema: "readmit-run-queue-report/v1",
          parallelism: 1,
          executed: 1,
          start_failed: 0,
          refused: 0,
          skipped: 0,
          jobs: [{ id: "booking-one", admission: "executed", isolation: "shared", ...(summary ? { run: summary } : {}) }],
        },
      };
    },
    OpenRunEvidence: () => runEvidenceResult(),
    DurableRunProgress: () => runProgressResult(),
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SUITE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  await screen.findByText(/select one of the environments the suite declares/);
  expect(screen.queryByRole("button", { name: "Send suite" })).toBeNull();
  await user.selectOptions(screen.getByLabelText("Suite environment"), "east");
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  await screen.findByText(/Environment east · site hospital-a/);
  await user.click(screen.getByRole("button", { name: "Send suite" }));
  await screen.findByText(/Stop reason: passed/);
  // The queue's own report shows each job's admission and its own run.
  expect(screen.getByText("Suite queue report — each job's own admission and run")).toBeTruthy();
  expect(screen.getByText("booking-one")).toBeTruthy();
  expect(events[0]).toBe("watch:job-001");
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  expect(facade.callsTo("StartSuiteRun")).toHaveLength(1);
});

test("the native file dialog can still select a saved test, kept inside the workspace", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    ChooseRunSpec: () => Promise.resolve(runSpecChoice(SPEC_ENTRY)),
    PreflightRun: () => runPreflightResult(),
  });
  await user.click(screen.getByRole("button", { name: "Browse files…" }));
  await waitFor(() =>
    expect((screen.getByLabelText("Saved test or suite") as HTMLSelectElement).value).toBe(SPEC_ENTRY),
  );
  expect(facade.callsTo("ChooseRunSpec")).toHaveLength(1);
  expect(facade.oneCall("ChooseRunSpec")).toEqual([WORKSPACE_ROOT]);
  // The advanced path hands the same entry to the same preflight.
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  expect(facade.oneCall("PreflightRun")[0]).toMatchObject({ spec: SPEC_ENTRY });
});

test("resume repeats a retained never-attempted run only after explicit keyboard action into a fresh folder", async () => {
  const user = userEvent.setup();
  const order: string[] = [];
  const { facade, events } = renderPanel({
    OpenRunEvidence: () => runEvidenceResult({ acknowledged: 0, not_attempted: 1, run_state: "timed_out", stop_reason: "timed_out" }),
    DurableRunProgress: () => runProgressResult(),
    ResumeDurableRun: () => { order.push("resume"); return { state: "completed", resume: {
      schema: "readmit-run-resume/v1", resumed_from: "timed_out", repeated: 1,
      run: durableRunResult("passed").run!,
    } }; },
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.selectOptions(screen.getByLabelText("Select run"), RUN_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  await screen.findByText(/Run: timed_out/);
  await user.type(screen.getByLabelText("Resume folder"), "job-002");
  screen.getByRole("button", { name: "Resume send" }).focus();
  await user.keyboard("{Enter}");
  await screen.findByText(/Resumed 1 never-attempted occurrence/);
  expect(facade.oneCall("ResumeDurableRun")).toEqual([{ workspace: WORKSPACE_ROOT, job: RUN_ENTRY, spec: SPEC_ENTRY, output: "job-002" }]);
  expect(events).toContain("watch:job-002");
  expect(order).toEqual(["resume"]);
});

test("resume shows a delivery-uncertain refusal without creating an output", async () => {
  const user = userEvent.setup();
  const { facade, events } = renderPanel({
    OpenRunEvidence: () => runEvidenceResult({ run_state: "delivery_uncertain", delivery_uncertain: true, uncertain: 1, acknowledged: 0 }),
    DurableRunProgress: () => runProgressResult(),
    ResumeDurableRun: () => ({ state: "failed", reason: "resume refused: an intent was synced without an acknowledged outcome; that send is never repeated" }),
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.selectOptions(screen.getByLabelText("Select run"), RUN_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  await user.type(screen.getByLabelText("Resume folder"), "job-002");
  await user.click(screen.getByRole("button", { name: "Resume send" }));
  await screen.findByText(/resume refused: an intent was synced without an acknowledged outcome/);
  expect(facade.callsTo("ResumeDurableRun")).toHaveLength(1);
  expect(events).toContain("watch:job-001");
});

test("an invalid resume output is refused without recording an outside folder in the session", async () => {
  const user = userEvent.setup();
  const { facade, events } = renderPanel({
    OpenRunEvidence: () => runEvidenceResult({ acknowledged: 0, not_attempted: 1 }),
    DurableRunProgress: () => runProgressResult(),
    ResumeDurableRun: () => ({ state: "failed", reason: "the new run folder must be one entry of the open workspace" }),
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.selectOptions(screen.getByLabelText("Select run"), RUN_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  await user.type(screen.getByLabelText("Resume folder"), "../outside");
  await user.click(screen.getByRole("button", { name: "Resume send" }));
  await screen.findByText(/new run folder must be one entry/);
  expect(facade.callsTo("ResumeDurableRun")).toHaveLength(1);
  expect(events).not.toContain("watch:../outside");
});

test("cancelling a resumed run retains its new folder and later recovery only reads it", async () => {
  const user = userEvent.setup();
  const mounted = renderPanel({
    OpenRunEvidence: () => runEvidenceResult({ acknowledged: 0, not_attempted: 1, run_state: "timed_out" }),
    DurableRunProgress: () => runProgressResult(),
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.selectOptions(screen.getByLabelText("Select run"), RUN_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  await user.type(screen.getByLabelText("Resume folder"), "job-002");
  const pending = mounted.facade.park("ResumeDurableRun");
  await user.click(screen.getByRole("button", { name: "Resume send" }));
  await waitFor(() => expect(mounted.facade.callsTo("ResumeDurableRun")).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "Cancel run" }));
  expect(mounted.facade.oneCall("Cancel")).toEqual(["durable-run"]);
  pending.resolve({ state: "completed", resume: {
    schema: "readmit-run-resume/v1", resumed_from: "timed_out", repeated: 1,
    run: durableRunResult("cancelled").run!,
  } });
  await screen.findByText(/Resumed 1 never-attempted occurrence\(s\) into job-002. Run: cancelled/);
  expect(mounted.events).toContain("watch:job-002");
  expect(mounted.facade.callsTo("DurableRunProgress").some((call) => call.args[1] === "job-002")).toBe(true);
  mounted.unmount();

  const reopened = renderPanel({
    OpenRunEvidence: () => runEvidenceResult({ entry: "job-002", run_state: "cancelled", stop_reason: "cancelled", acknowledged: 0, not_attempted: 1 }),
    DurableRunProgress: () => runProgressResult({ phase: "cancelled", acknowledged: 0, not_attempted: 1 }),
  }, [...ENTRIES, { name: "job-002", kind: "job" }]);
  await user.selectOptions(screen.getByLabelText("Select run"), "job-002");
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  await screen.findByText(/Run: cancelled · stopped cancelled/);
  expect(reopened.facade.callsTo("ResumeDurableRun")).toHaveLength(0);
  expect(reopened.facade.callsTo("OpenRunEvidence")).toHaveLength(1);
});

test("cleanup is offered after completion, refuses a changed live run, then removes only its stale lease", async () => {
  const user = userEvent.setup();
  let listedTerminal = false;
  let actuallyTerminal = false;
  const { facade } = renderPanel({
    OpenRunEvidence: () => runEvidenceResult({ terminal: listedTerminal, lease: listedTerminal ? "stale" : "held" }),
    DurableRunProgress: () => runProgressResult(),
    CleanDurableRun: () => actuallyTerminal
      ? { state: "completed", cleanup: { schema: "readmit-run-cleanup/v1", run: durableRunResult("passed").run!, removed: ["lease.json"], retained: ["journal.jsonl"] } }
      : { state: "failed", reason: "completion was not recorded; the writer may still hold its lease and nothing was removed" },
  });
  await user.selectOptions(screen.getByLabelText("Select run"), RUN_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  expect(screen.queryByRole("button", { name: "Clear stale lease" })).toBeNull();
  listedTerminal = true;
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  await user.click(screen.getByRole("button", { name: "Clear stale lease" }));
  await screen.findByText(/Cleanup refused: completion was not recorded/);
  actuallyTerminal = true;
  await user.click(screen.getByRole("button", { name: "Open evidence" }));
  screen.getByRole("button", { name: "Clear stale lease" }).focus();
  await user.keyboard("{Enter}");
  await screen.findByText(/Cleanup removed lease.json; retained 1 evidence entries/);
  expect(facade.callsTo("CleanDurableRun")).toHaveLength(2);
  expect(facade.callsTo("CleanDurableRun").map((call) => call.args)).toEqual([
    [WORKSPACE_ROOT, RUN_ENTRY], [WORKSPACE_ROOT, RUN_ENTRY],
  ]);
});

test("an empty workspace offers nothing to select and explains the empty history", () => {
  renderPanel({}, []);
  const select = screen.getByLabelText("Saved test or suite") as HTMLSelectElement;
  expect(select.value).toBe("");
  const preflightButton = screen.getByRole("button", { name: "Preview run" }) as HTMLButtonElement;
  expect(preflightButton.disabled).toBe(true);
});

test("recovery after an interruption shows the run and never resumes it", async () => {
  const user = userEvent.setup();
  const events: string[] = [];
  const facade = installFacade({
    Cancel: async () => {
      events.push("cancel");
    },
    DiscardDraft: async () => ({ state: "completed" }),
  });
  render(
    <Recovery
      restored={recoveryResult(
        {
          schema: "readmit-desktop-session/v1",
          view: { workspace: WORKSPACE_ROOT, region: "evidence", case: "sample-case", run: "baseline-run" },
          drafts: [retainedDraft(WORKSPACE_ROOT, "triage")],
        },
        {
          schema: "readmit-run/v1",
          state: "delivery_uncertain",
          stop_reason: "interrupted",
          delivery_uncertain: true,
          planned: 2,
          recorded: 1,
          recovered: true,
          journal_incomplete: false,
        },
      )}
      onChanged={() => undefined}
      onReopen={() => undefined}
    />,
  );
  expect(screen.getByText("Restored after an interruption")).toBeTruthy();
  expect(screen.getByText("delivery_uncertain")).toBeTruthy();
  expect(screen.getByText(/inspect the receiver before any new execution/)).toBeTruthy();
  expect(screen.getByText("Nothing was resumed or resent. Recovery only read the retained evidence.")).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  // An unstored note is offered back, and dropping it is the person's act.
  expect(screen.getByText("still writing this")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Discard draft" }));
  await waitFor(() => expect(facade.callsTo("DiscardDraft")).toHaveLength(1));
  expect(facade.oneCall("DiscardDraft")).toEqual([WORKSPACE_ROOT, "triage"]);
});

test("recovery of an empty session draws nothing", () => {
  installFacade({});
  render(<Recovery restored={null} onChanged={() => undefined} onReopen={() => undefined} />);
  expect(screen.queryByText("Restored after an interruption")).toBeNull();
});

test("a comparison selects actual retained executions of the workspace", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    Cancel: async () => {},
    CompareRuns: () => runComparisonResult(),
  });
  render(<RunComparison workspace={WORKSPACE_ROOT} busy={false} entries={ENTRIES} />);
  await user.selectOptions(screen.getByLabelText("Baseline execution"), RUN_ENTRY);
  await user.selectOptions(screen.getByLabelText("Current execution"), RUN_ENTRY);
  // The case entry is not a retained execution and is not offered.
  const options = Array.from(screen.getByLabelText("Baseline execution").querySelectorAll("option")).map((o) => o.value);
  expect(options).toEqual(["", RUN_ENTRY]);
  const parked = facade.park("CompareRuns");
  await user.click(screen.getByRole("button", { name: "Compare executions" }));
  await screen.findByText("Verifying retained executions…");
  await user.click(screen.getByRole("button", { name: "Cancel comparison" }));
  parked.resolve(runComparisonResult({
    assertions: [{ id: "ledger", baseline: "failed", current: "failed", definition: "unchanged", behavior: "same_failure" }],
    scope: "STALE ANSWER",
  }));
  expect(await screen.findByText(/Comparison cancelled/)).toBeTruthy();
  expect(screen.queryByText("STALE ANSWER")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Compare executions" }));
  expect(parked.size).toBe(1);
  parked.resolve(runComparisonResult({
    assertions: [{ id: "ledger", baseline: "passed", current: "passed", definition: "unchanged", behavior: "same_pass" }],
    scope: "THE NEW ANSWER",
  }));
  await screen.findByText("THE NEW ANSWER");
  expect(screen.getByText("same_pass")).toBeTruthy();
});

test("a disconnected application reports the fixed failure sentence and never a silent success", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreflightRun: () => runPreflightResult(),
  });
  await user.selectOptions(screen.getByLabelText("Saved test or suite"), SPEC_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  await screen.findByText(/Preflight —/);
  const parked = facade.park("StartDurableRun");
  // The preflight stays; the send is attempted only under its identity.
  await user.click(await screen.findByRole("button", { name: "Send test" }));
  parked.reject();
  // The bindings turn an unreachable application into one fixed failure
  // sentence — never a silent success and never a made-up verdict.
  await screen.findByText(/the application did not answer/);
  expect(screen.getByText(/Operation: failed/)).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(1);
});

// A suite handed over from the suite panel arrives with the environment it
// was prepared against; the run view still preflights it itself and sends
// nothing until its own explicit decision.
test("a handed-over suite preflights with the environment it was prepared against", async () => {
  const user = userEvent.setup();
  const facade = installFacade({ PreflightRun: () => runPreflightResult() });
  render(
    <RunPanel
      workspace={WORKSPACE_ROOT}
      entries={ENTRIES}
      onWatch={async () => {}}
      onRefresh={() => {}}
      onOpenCase={() => {}}
      initialSpec={SUITE_ENTRY}
      initialEnvironment="east"
    />,
  );
  expect((screen.getByLabelText("Saved test or suite") as HTMLSelectElement).value).toBe(SUITE_ENTRY);
  expect((screen.getByLabelText("Suite environment") as HTMLSelectElement).value).toBe("east");
  await user.click(screen.getByRole("button", { name: "Preview run" }));
  expect(facade.oneCall("PreflightRun")[0]).toEqual({ workspace: WORKSPACE_ROOT, spec: SUITE_ENTRY, environment: "east" });
  expect(facade.callsTo("StartSuiteRun")).toHaveLength(0);
});
