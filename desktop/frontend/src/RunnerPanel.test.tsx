import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RunnerPanel } from "./RunnerPanel";
import { IndicatorsContext } from "./lifecycle";
import { installFacade, uninstallFacade, facadeStub, Parked } from "./testkit/wails";
import { indicatorTable } from "./testkit/fixtures";
import type {
  State,
  RunnerEnrollmentResult,
  RunnerInspectResult,
  RunnerExecutionResult,
  RunnerRecoveryResult,
  RunnerJobPreviewResult,
  SchedulePreviewResult,
  CIHandoffResult,
  CIInspectResult,
  CIGateVerifyResult,
  GatePolicyResult,
} from "./bindings";

function enrollment(result: Partial<RunnerEnrollmentResult>): RunnerEnrollmentResult {
  return { state: "failed", ...result };
}

function inspection(result: Partial<RunnerInspectResult>): RunnerInspectResult {
  return { state: "completed", ...result };
}

const CONFIG = "/etc/readmit-runner/config.json";
const JOB = "/srv/jobs/nightly-001.json";
const INPUT_ID = "f".repeat(64);

/** A successful preview of the job file under the configuration. */
function previewed(): RunnerJobPreviewResult {
  return { state: "completed", job_id: "nightly-001", spec: "/srv/readmit/spec.json", input_identity: INPUT_ID, environment: "lab" };
}

/** The job file a preview and a send read, as opposed to the one Save job writes. */
function sendJobFile(): HTMLElement {
  return within(screen.getByRole("group", { name: "Send job" })).getByLabelText("Job file");
}

/** Previews the named job and waits until its send is offered. */
async function previewJob(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Preview job" }));
  await screen.findByText(/^Send job sends/);
}

/** Names the configuration and the job file, and previews them. */
async function preparedJob(user: ReturnType<typeof userEvent.setup>) {
  await user.type(await screen.findByLabelText("Runner configuration file"), CONFIG);
  await user.type(sendJobFile(), JOB);
  await previewJob(user);
}

// A runner action answers one of six states, and only a failure is a refusal.
// Busy, cancelled, nothing to show and permission denied each read as
// themselves, through Status with their own word and shape; a refused
// admission keeps the sentence its section gives it.
test("every state a runner action answers reads as itself, and only a failure as a refusal", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const indicators = indicatorTable();
  render(
    <IndicatorsContext.Provider value={indicators}>
      <RunnerPanel />
    </IndicatorsContext.Provider>,
  );
  const save = await screen.findByRole("button", { name: "Save job" });
  const others: State[] = ["busy", "cancelled", "empty", "permission_denied"];
  for (const state of others) {
    facade.reply({ SaveRunnerJob: () => ({ state, reason: `the job document answered ${state}` }) });
    await user.click(save);
    const reason = await screen.findByText(`the job document answered ${state}`);
    const status = reason.closest("p");
    expect(status?.getAttribute("role")).toBe("status");
    expect(status?.className).toBe(`status status-${state}`);
    expect(within(status as HTMLElement).getByText(indicators.get(state)?.label ?? "")).toBeTruthy();
    expect(screen.queryByText(/Refused:/)).toBeNull();
  }
  facade.reply({ SaveRunnerJob: () => ({ state: "failed", reason: "the job document answered failed" }) });
  await user.click(save);
  expect((await screen.findByText("Refused: the job document answered failed")).getAttribute("role")).toBe("alert");

  // An execution the runner cancelled is not refused, and one it did not
  // admit keeps the section's own sentence. Each send follows its own preview.
  await user.type(screen.getByLabelText("Runner configuration file"), "/etc/readmit-runner/config.json");
  await user.type(sendJobFile(), "/srv/jobs/nightly-001.json");
  facade.reply({
    InspectRunnerJob: () => previewed(),
    ReadRunnerConfig: () => inspection({ state: "failed", reason: "the selected file must be a private regular file" }),
    ExecuteRunnerJob: () => ({ state: "cancelled", job_id: "nightly-001", reason: "the execution was cancelled" }),
  });
  await previewJob(user);
  await user.click(screen.getByRole("button", { name: "Send job" }));
  const cancelled = (await screen.findByText("the execution was cancelled")).closest("p");
  expect(cancelled?.className).toBe("status status-cancelled");
  expect(screen.queryByText(/Refused: the execution was cancelled/)).toBeNull();
  facade.reply({ ExecuteRunnerJob: () => ({ state: "permission_denied", job_id: "nightly-001", reason: "no runner instance is free" }) });
  await previewJob(user);
  await user.click(screen.getByRole("button", { name: "Send job" }));
  expect(await screen.findByText("Not admitted: no runner instance is free")).toBeTruthy();
  uninstallFacade();
});

test("enrollment reports the lease the hub granted", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    EnrollRunner: async () =>
      enrollment({
        state: "completed",
        project: "alpha",
        environment: "lab",
        engine: "engine-v1",
        expires_at: "2026-09-21T12:00:10Z",
        max_seconds: 300,
        max_jobs: 100,
      }),
  });
  render(<RunnerPanel />);
  await user.type(await screen.findByLabelText("Runner configuration file"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: "Check runner admission" }));
  expect(facade.callsTo("EnrollRunner").length).toBe(1);
  expect(
    await screen.findByText(/Admitted: lease until 2026-09-21T12:00:10Z, at most 300s per job/),
  ).toBeTruthy();
  uninstallFacade();
});

test("a denied runner or environment shows the hub's own reason", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    EnrollRunner: async () =>
      enrollment({
        state: "permission_denied",
        reason:
          "runner operation refused; check private configuration, admission, and retained status: hub refused admission (Forbidden): version or environment refused",
      }),
  });
  render(<RunnerPanel />);
  await user.type(await screen.findByLabelText("Runner configuration file"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: "Check runner admission" }));
  expect(facade.callsTo("EnrollRunner").length).toBe(1);
  expect(await screen.findByText(/Admission refused:.*version or environment refused/)).toBeTruthy();
  uninstallFacade();
});

test("a retained job id is never replayed and no resend is offered", async () => {
  const user = userEvent.setup();
  const execution: RunnerExecutionResult = {
    state: "failed",
    job_id: "nightly-001",
    reason:
      "runner operation refused; check private configuration, admission, and retained status",
  };
  const facade = installFacade({
    InspectRunnerJob: async () => previewed(),
    ExecuteRunnerJob: async () => execution,
    ReadRunnerConfig: async () => inspection({ state: "failed", reason: "the selected file must be a private regular file" }),
  });
  render(<RunnerPanel />);
  await preparedJob(user);
  expect(facade.oneCall("InspectRunnerJob")).toEqual([CONFIG, JOB]);
  // The prepared input ID is the preview's, shown read-only.
  expect((screen.getByLabelText("Prepared input ID") as HTMLInputElement).readOnly).toBe(true);
  expect((screen.getByLabelText("Prepared input ID") as HTMLInputElement).value).toBe(INPUT_ID);
  await user.click(screen.getByRole("button", { name: "Send job" }));
  expect(facade.oneCall("ExecuteRunnerJob")[0]).toMatchObject({
    config_path: CONFIG,
    job_path: JOB,
    expected_identity: INPUT_ID,
  });
  expect(await screen.findByText(/Refused: runner operation refused/)).toBeTruthy();
  // The panel offers recovery vocabulary, never a resend of retained work.
  expect(screen.queryByRole("button", { name: /retry|resend|run again/i })).toBeNull();
  expect(screen.getByText(/never replayed|never offered/i)).toBeTruthy();
  uninstallFacade();
});

test("resource contention is displayed as the hub's refusal, not retried", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    EnrollRunner: async () =>
      enrollment({
        state: "permission_denied",
        reason:
          "runner operation refused; check private configuration, admission, and retained status: hub refused admission (Conflict): environment leased or recovering",
      }),
  });
  render(<RunnerPanel />);
  await user.type(await screen.findByLabelText("Runner configuration file"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: "Check runner admission" }));
  expect(await screen.findByText(/environment leased or recovering/)).toBeTruthy();
  expect(facade.callsTo("EnrollRunner").length).toBe(1);
  uninstallFacade();
});

test("cancellation keeps uncertain delivery and recovery reads without sending", async () => {
  const user = userEvent.setup();
  const parked = new Parked();
  const facade = installFacade({
    Cancel: async () => {},
    InspectRunnerJob: async () => previewed(),
    ExecuteRunnerJob: () => parked.arrive() as Promise<RunnerExecutionResult>,
    ReadRunnerRecovery: async () =>
      ({
        state: "completed",
        job_id: "nightly-001",
        acknowledged: 2,
        uncertain: 1,
        not_attempted: 0,
      }) as RunnerRecoveryResult,
    ReadRunnerConfig: async () =>
      inspection({
        config: {
          hub: "https://hub.example:8443",
          project: "alpha",
          environment: "lab",
          root: "/var/lib/readmit-runner/runs",
          update_engine: "v-next",
          key: { command: "/bin/reader", arguments: ["k"] },
          token: { command: "/bin/reader", arguments: ["t"] },
        },
        engine: "engine-v1",
        health: { schema: "readmit-runner-status/v1", state: "lease_current", jobs: 1 },
        jobs: [
          {
            id: "nightly-001",
            state: "delivery_uncertain",
            delivery_uncertain: true,
            journal_incomplete: true,
          },
        ],
      }),
  });
  render(<RunnerPanel />);
  await preparedJob(user);
  await user.click(screen.getByRole("button", { name: "Send job" }));
  // The named cancel control reaches only this panel's operation.
  await user.click(await screen.findByRole("button", { name: "Cancel job" }));
  parked.resolve({
    state: "failed",
    job_id: "nightly-001",
    reason: "the operation was cancelled",
  } as RunnerExecutionResult);
  expect(
    await screen.findByText(/Refused: the operation was cancelled/),
  ).toBeTruthy();
  expect(facade.oneCall("ExecuteRunnerJob")[0]).toMatchObject({ job_path: "/srv/jobs/nightly-001.json" });

  // After a disconnection, reading the configuration again is how the window
  // reconnects to current state before any new deliberate action.
  await user.click(screen.getByRole("tab", { name: "Recovery" }));
  await user.type(screen.getByLabelText("Job ID"), "nightly-001");
  await user.click(screen.getByRole("button", { name: "Open recovery" }));
  expect(facade.oneCall("ReadRunnerRecovery")).toEqual([CONFIG, "nightly-001"]);
  expect(
    await screen.findByText(/nightly-001: 2 acknowledged, 1 uncertain, 0 not attempted\. Recovery never sends\./),
  ).toBeTruthy();
  uninstallFacade();
});

test("a running job takes the focus to Cancel, and Enter there cancels only the runner's operation", async () => {
  const user = userEvent.setup();
  const parked = new Parked();
  const facade = installFacade({
    Cancel: async () => {},
    InspectRunnerJob: async () => previewed(),
    ExecuteRunnerJob: () => parked.arrive() as Promise<RunnerExecutionResult>,
    ReadRunnerConfig: async () => inspection({ state: "failed", reason: "the selected file must be a private regular file" }),
  });
  render(<RunnerPanel />);
  await preparedJob(user);
  // From the job file: the prepared input ID, then Preview job, then Send job.
  await user.click(sendJobFile());
  await user.tab();
  await user.tab();
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Send job" }));
  await user.keyboard("{Enter}");
  const cancel = await screen.findByRole("button", { name: "Cancel job" });
  expect(document.activeElement).toBe(cancel);
  await user.keyboard("{Enter}");
  expect(facade.oneCall("Cancel")).toEqual(["runner"]);
  parked.resolve({
    state: "completed",
    job_id: "nightly-001",
    summary: { schema: "readmit-job/v1", state: "delivery_uncertain", stop_reason: "cancelled", delivery_uncertain: true },
  } as RunnerExecutionResult);
  const finished = "Job nightly-001 finished: delivery_uncertain. Delivery stayed uncertain; nothing will be resent from here.";
  expect(await screen.findByText((_, element) => element?.tagName === "P" && element.textContent === finished)).toBeTruthy();
  expect(screen.queryByRole("button", { name: /retry|resend|run again/i })).toBeNull();
  uninstallFacade();
});

test("schedule revisions show effective timing, missed and overlap semantics", async () => {
  const user = userEvent.setup();
  const preview: SchedulePreviewResult = {
    state: "completed",
    identity: "a".repeat(64),
    concurrency: "serial-skip-missed",
    entries: [
      {
        entry: {
          id: "nightly",
          zone: "America/New_York",
          at: "02:30",
          window_seconds: 600,
          runner_config: "/etc/readmit-runner/config.json",
          spec: "/srv/readmit/spec.json",
          input_sha256: "b".repeat(64),
          route: "https://alerts.example/",
          approved: true,
        },
        identity: "c".repeat(64),
        pin_state: "computed",
        occurrences: [
          { day: "2026-03-07", utc: "2026-03-07T07:30:00Z", state: "scheduled" },
          { day: "2026-03-08", state: "dst-gap" },
          { day: "2026-03-09", utc: "2026-03-09T06:30:00Z", state: "missed" },
        ],
        notification: "approved: the hub emits only the fixed alert body shown below",
      },
    ],
    alert: '{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}',
    alert_states: ["passed", "failed"],
  };
  const saved: SchedulePreviewResult = { ...preview, identity: "d".repeat(64) };
  const facade = installFacade({
    PreviewSchedulePolicy: async () => preview,
    SaveSchedulePolicy: async () => saved,
  });
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "Schedules" }));
  await user.click(screen.getByRole("button", { name: "Add schedule" }));
  await user.click(screen.getByRole("button", { name: "Preview schedule policy" }));
  expect(facade.callsTo("PreviewSchedulePolicy").length).toBe(1);
  expect(await screen.findByText(/2026-03-08: dst-gap/)).toBeTruthy();
  expect(await screen.findByText(/serial-skip-missed/)).toBeTruthy();
  expect(screen.getByText(/recorded and skipped, never replayed/)).toBeTruthy();
  await user.type(await screen.findByLabelText("Policy revision file"), "/tmp/schedules.json");
  await user.click(screen.getByRole("button", { name: "Save schedule policy" }));
  expect(facade.callsTo("SaveSchedulePolicy").length).toBe(1);
  expect(await screen.findByText("d".repeat(64))).toBeTruthy();
  uninstallFacade();
});

test("notifications show exactly the fixed approved body and nothing else", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    PreviewSchedulePolicy: async () => ({
      state: "completed",
      identity: "a".repeat(64),
      concurrency: "serial-skip-missed",
      entries: [
        {
          entry: {
            id: "nightly",
            zone: "UTC",
            at: "02:30",
            window_seconds: 600,
            runner_config: "/etc/readmit-runner/config.json",
            spec: "/srv/readmit/spec.json",
            input_sha256: "b".repeat(64),
            route: "https://alerts.example/",
            approved: true,
          },
          pin_state: "unreadable",
          occurrences: [{ day: "2026-09-21", utc: "2026-09-21T02:30:00Z", state: "scheduled" }],
          notification: "approved: the hub emits only the fixed alert body shown below",
        },
      ],
      alert: '{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}',
      alert_states: ["passed", "failed"],
    }),
  });
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "Schedules" }));
  await user.click(screen.getByRole("button", { name: "Add schedule" }));
  await user.click(screen.getByRole("button", { name: "Preview schedule policy" }));
  expect(facade.callsTo("PreviewSchedulePolicy").length).toBe(1);
  expect(
    await screen.findByText(/no names, paths, values or errors/),
  ).toBeTruthy();
  expect(
    await screen.findByText('{"schema":"readmit-hub-alert/v1","state":"failed","coverage":"not-assessed"}'),
  ).toBeTruthy();
  uninstallFacade();
});

test("the CI handoff is generated in-app and results are inspected in-app", async () => {
  const user = userEvent.setup();
  const handoff: CIHandoffResult = {
    state: "completed",
    output: "/srv/readmit/handoff.sh",
    document:
      "# Provision these six non-secret path/selection variables on the trusted customer-owned agent:\n" +
      "# --- reviewed workflow (install as-is) ---\n" +
      '#!/bin/sh\n"$READMIT_BIN" --operation-policy "$OPERATION_POLICY" suite ci "$SUITE_FILE" --environment "$SUITE_ENVIRONMENT" --output "$RUN_DIRECTORY" --requirements "$COVERAGE_FILE" --send --deadline 5m\nexit $?\n',
  };
  const results: CIInspectResult = {
    state: "completed",
    ci: { schema: "readmit-suite-ci/v1", state: "passed", exit_code: 0 },
    gate: { schema: "readmit-ci-gate/v1", state: "passed", exit_code: 0 },
  };
  const policy: GatePolicyResult = {
    state: "completed",
    identity: "e".repeat(64),
    environment: "lab",
    engine: "engine-v1",
    specifications: 3,
    retain_until: "2036-01-01T00:00:00Z",
  };
  const facade = installFacade({
    SaveCIHandoff: async () => handoff,
    InspectCIResults: async () => results,
    InspectGatePolicy: async () => policy,
  });
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "CI handoff" }));
  await user.click(screen.getByRole("button", { name: /Generate configuration/ }));
  expect(facade.oneCall("SaveCIHandoff")[0]).toMatchObject({ integration: "posix" });
  expect(await screen.findByText(/Install it as the customer administrator\./)).toBeTruthy();
  expect(await screen.findByText(/\$READMIT_BIN.*suite ci/s)).toBeTruthy();
  // The full workflow text is kept in its own labelled details.
  expect(screen.getByText("Generated workflow").tagName).toBe("SUMMARY");

  await user.click(screen.getByRole("tab", { name: "Inspect results" }));
  await user.type(
    screen.getByLabelText("CI results folder"),
    "/var/lib/readmit-ci/run-1",
  );
  await user.click(screen.getByRole("button", { name: /Open CI results/ }));
  expect(
    await screen.findByText((_, element) => element?.textContent === "Suite gate: passed (exit 0)."),
  ).toBeTruthy();
  expect(
    await screen.findByText((_, element) => element?.textContent === "Change gate: passed (exit 0)."),
  ).toBeTruthy();
  expect(facade.oneCall("InspectCIResults")[0]).toBe("/var/lib/readmit-ci/run-1");

  await user.type(screen.getByLabelText("Gate policy file"), "/srv/readmit/gate-policy.json");
  await user.click(screen.getByRole("button", { name: /Open gate policy/ }));
  expect(await screen.findByText("e".repeat(64))).toBeTruthy();
  expect(await screen.findByText(/approves nothing/i)).toBeTruthy();
  uninstallFacade();
});

/** One task of the CI tab of a freshly rendered panel. */
async function ciTab(user: ReturnType<typeof userEvent.setup>, task = "Generate workflow") {
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "CI handoff" }));
  await user.click(screen.getByRole("tab", { name: task }));
}

const GATE_STEP = {
  releases: "/srv/readmit/releases.json",
  promotion: "/srv/readmit/promotion.json",
  promotion_identity: "a".repeat(64),
  revision: "fixture-build-7",
  baseline: "/var/lib/readmit-ci/baseline",
  policy: "/srv/readmit/gate-policy.json",
  policy_identity: "b".repeat(64),
  snapshot_directory: "/var/lib/readmit-ci/gate-1",
};

test("the reviewed change-gate step is added to the handoff only when asked for, with every reviewed pin, and a refusal is shown", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SaveCIHandoff: async () => ({ state: "failed", reason: "the reviewed gate policy identity is its full 64-character lowercase SHA-256 identity" }),
  });
  await ciTab(user);
  // Without the step, the handoff asks for no gate and no gate field is offered.
  expect(screen.queryByLabelText("Gate policy ID")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Generate configuration" }));
  expect(facade.callsTo("SaveCIHandoff")[0]?.args[0]).not.toHaveProperty("gate");

  // The step is chosen from the keyboard: the checkbox follows the handoff
  // destination, and Space checks it.
  await user.click(screen.getByLabelText("Workflow output file"));
  await user.tab();
  const step = screen.getByRole("checkbox", { name: /Include change gate/ });
  expect(document.activeElement).toBe(step);
  await user.keyboard(" ");
  expect((step as HTMLInputElement).checked).toBe(true);
  expect(screen.getByText(/never replaces the suite's exit status/)).toBeTruthy();
  const fields: [string, string][] = [
    ["Release pins file", GATE_STEP.releases],
    ["Promotion approval file", GATE_STEP.promotion],
    ["Promotion approval ID", GATE_STEP.promotion_identity],
    ["Target revision (operator-declared)", GATE_STEP.revision],
    ["Baseline run folder", GATE_STEP.baseline],
    ["Gate policy file", GATE_STEP.policy],
    ["Gate policy ID", "B".repeat(64)],
    ["Gate snapshot folder", GATE_STEP.snapshot_directory],
  ];
  const setup = within(screen.getByRole("group", { name: "CI setup" }));
  for (const [label, value] of fields) {
    await user.type(setup.getByLabelText(label), value);
  }
  await user.click(screen.getByRole("button", { name: "Generate configuration" }));
  expect(facade.callsTo("SaveCIHandoff")[1]?.args[0]).toMatchObject({ gate: { ...GATE_STEP, policy_identity: "B".repeat(64) } });
  expect((await screen.findByRole("alert")).textContent).toBe(
    "Refused: the reviewed gate policy identity is its full 64-character lowercase SHA-256 identity",
  );

  // Corrected, the handoff is saved and shown as written.
  facade.reply({
    SaveCIHandoff: async () => ({
      state: "completed",
      output: "/srv/readmit/handoff.sh",
      document: '#!/bin/sh\n"$READMIT_BIN" suite gate "$RUN_DIRECTORY" --baseline "$BASELINE_DIRECTORY"\n',
    }),
  });
  await user.clear(setup.getByLabelText("Gate policy ID"));
  await user.type(setup.getByLabelText("Gate policy ID"), GATE_STEP.policy_identity);
  await user.click(screen.getByRole("button", { name: "Generate configuration" }));
  expect(facade.callsTo("SaveCIHandoff")[2]?.args[0]).toMatchObject({ gate: GATE_STEP });
  expect(await screen.findByText("Saved to /srv/readmit/handoff.sh. Install it as the customer administrator.")).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();

  // Unchecked, the next handoff asks for no gate again, whatever was typed.
  await user.click(step);
  expect(screen.queryByLabelText("Gate policy ID")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Generate configuration" }));
  expect(facade.callsTo("SaveCIHandoff")[3]?.args[0]).not.toHaveProperty("gate");
  uninstallFacade();
});

test("a retained change gate is verified against its pinned identity, and what could not be verified is named", async () => {
  const user = userEvent.setup();
  const passed: CIGateVerifyResult = {
    state: "completed",
    gate: {
      schema: "readmit-ci-gate/v1",
      state: "passed",
      exit_code: 0,
      approval: "passed",
      pins: "passed",
      coverage: "passed",
      baseline: "passed",
      retention: "retained",
      target_revision: "operator_asserted",
    },
  };
  const tampered: CIGateVerifyResult = {
    state: "failed",
    reason: "the retained change gate could not be verified against this identity; an unknown gate is never a pass",
    gate: {
      schema: "readmit-ci-gate/v1",
      state: "unknown",
      exit_code: 2,
      approval: "unknown",
      pins: "unknown",
      coverage: "unknown",
      baseline: "unknown",
      retention: "unknown",
      target_revision: "unknown",
    },
    unverified: ["approval", "pins", "coverage", "baseline", "retention", "target_revision"],
  };
  const facade = installFacade({ VerifyCIGate: async () => passed });
  await ciTab(user, "Verify gate");
  const verify = screen.getByRole("button", { name: "Verify gate" }) as HTMLButtonElement;
  expect(verify.disabled).toBe(true);
  await user.type(screen.getByLabelText("Gate snapshot folder"), "/var/lib/readmit-ci/gate-1");
  expect(verify.disabled).toBe(true);
  await user.type(screen.getByLabelText("Pinned gate policy ID"), GATE_STEP.policy_identity);
  await user.click(verify);
  expect(facade.oneCall("VerifyCIGate")).toEqual(["/var/lib/readmit-ci/gate-1", GATE_STEP.policy_identity]);
  const verified = await screen.findByRole("status");
  expect(within(verified).getByText((_, element) => element?.textContent === "Retained change gate: passed (exit 0).")).toBeTruthy();
  expect(
    within(verified).getByText(
      "Approval passed · Pins passed · Coverage passed · Baseline passed · Retention retained · Target revision operator_asserted",
    ),
  ).toBeTruthy();
  expect(within(verified).queryByText(/Not verified/)).toBeNull();
  expect(within(verified).getByText(/nothing was sent or rerun/)).toBeTruthy();

  // Another snapshot is another question: the reading of the first is withdrawn.
  await user.clear(screen.getByLabelText("Gate snapshot folder"));
  expect(screen.queryByText(/Retained change gate:/)).toBeNull();
  await user.type(screen.getByLabelText("Gate snapshot folder"), "/var/lib/readmit-ci/gate-2");
  facade.reply({ VerifyCIGate: async () => tampered });
  await user.click(verify);
  const refused = await screen.findByRole("alert");
  expect(within(refused).getByText((_, element) => element?.textContent === "Retained change gate: unknown (exit 2).")).toBeTruthy();
  expect(within(refused).getByText("Not verified: approval, pins, coverage, baseline, retention, target revision.")).toBeTruthy();
  expect(within(refused).getByText(/an unknown gate is never a pass/)).toBeTruthy();
  expect(screen.queryByText(/nothing was sent or rerun/)).toBeNull();

  // A pin the facade refuses before reading anything is shown as refused.
  facade.reply({
    VerifyCIGate: async () => ({ state: "failed", reason: "the pinned gate policy identity is its full 64-character lowercase SHA-256 identity" }),
  });
  await user.type(screen.getByLabelText("Pinned gate policy ID"), "0");
  await user.click(verify);
  expect((await screen.findByRole("alert")).textContent).toBe(
    "Refused: the pinned gate policy identity is its full 64-character lowercase SHA-256 identity",
  );
  expect(facade.callsTo("VerifyCIGate")).toHaveLength(3);
  uninstallFacade();
});

test("a running verification takes the focus to its Cancel, and Enter there cancels it without a verdict", async () => {
  const user = userEvent.setup();
  const parked = new Parked();
  const facade = installFacade({
    Cancel: async () => {},
    VerifyCIGate: () => parked.arrive() as Promise<CIGateVerifyResult>,
  });
  await ciTab(user, "Verify gate");
  await user.type(screen.getByLabelText("Gate snapshot folder"), "/var/lib/readmit-ci/gate-1");
  await user.type(screen.getByLabelText("Pinned gate policy ID"), GATE_STEP.policy_identity);
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Verify gate" }));
  await user.keyboard("{Enter}");
  const cancelControl = await screen.findByRole("button", { name: "Cancel verification" });
  expect(document.activeElement).toBe(cancelControl);
  expect(screen.getByText("Verifying the retained snapshot…")).toBeTruthy();
  // The other CI tasks' controls are held too, though another task is shown.
  expect((screen.getByRole("button", { name: "Generate configuration", hidden: true }) as HTMLButtonElement).disabled).toBe(true);
  // The snapshot and its pin hold still while they are verified.
  expect((screen.getByLabelText("Gate snapshot folder") as HTMLInputElement).disabled).toBe(true);
  expect((screen.getByLabelText("Pinned gate policy ID") as HTMLInputElement).disabled).toBe(true);
  await user.keyboard("{Enter}");
  // The cancel names the verification, so it can stop nothing another panel started.
  expect(facade.oneCall("Cancel")).toEqual(["ci-gate-verify"]);
  parked.resolve({ state: "cancelled", reason: "the verification was cancelled before it reached a verdict; nothing was changed" });
  // Cancelled is its own state: neither a refusal nor a verdict.
  expect(await screen.findByText("Cancelled: the verification was cancelled before it reached a verdict; nothing was changed")).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("button", { name: "Cancel verification" })).toBeNull();
  expect(screen.queryByText(/Retained change gate:/)).toBeNull();
  expect(facade.callsTo("VerifyCIGate")).toHaveLength(1);
  uninstallFacade();
});

test("a gate policy's identity and a directory's results are withdrawn once another path is typed", async () => {
  const user = userEvent.setup();
  installFacade({
    InspectCIResults: async () => ({ state: "completed", ci: { schema: "readmit-suite-ci/v1", state: "passed", exit_code: 0 } }),
    InspectGatePolicy: async () => ({ state: "completed", identity: "e".repeat(64), environment: "lab", engine: "engine-v1", specifications: 1, retain_until: "2036-01-01T00:00:00Z" }),
  });
  await ciTab(user, "Inspect results");
  await user.type(screen.getByLabelText("Gate policy file"), "/srv/readmit/gate-policy.json");
  await user.click(screen.getByRole("button", { name: "Open gate policy" }));
  expect(await screen.findByText("e".repeat(64))).toBeTruthy();
  // The identity shown beside the path it was read from is one a person pins;
  // beside another path it would be pinned by mistake.
  await user.type(screen.getByLabelText("Gate policy file"), ".next");
  expect(screen.queryByText("e".repeat(64))).toBeNull();

  await user.type(screen.getByLabelText("CI results folder"), "/var/lib/readmit-ci/run-1");
  await user.click(screen.getByRole("button", { name: "Open CI results" }));
  expect(await screen.findByText((_, element) => element?.tagName === "P" && element.textContent === "Suite gate: passed (exit 0).")).toBeTruthy();
  await user.type(screen.getByLabelText("CI results folder"), "0");
  expect(screen.queryByText(/Suite gate:/)).toBeNull();
  uninstallFacade();
});

test("the panel stays inert without a configuration and says local use needs no runner", async () => {
  const facade = installFacade({});
  render(<RunnerPanel />);
  expect(await screen.findByText(/Local use needs no hub and no runner/)).toBeTruthy();
  const inspect = screen.getByRole("button", { name: /Inspect runner/ }) as HTMLButtonElement;
  expect(inspect.disabled).toBe(true);
  expect(facade.calls.length).toBe(0);
  uninstallFacade();
});

test("an interrupted facade reports the fixed unreachable sentence as a refusal", async () => {
  const user = userEvent.setup();
  installFacade({
    EnrollRunner: async () => {
      throw new Error("the test stub has no answer for EnrollRunner");
    },
  });
  render(<RunnerPanel />);
  await user.type(await screen.findByLabelText("Runner configuration file"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: "Check runner admission" }));
  expect(await screen.findByText(/Refused: the application did not answer/)).toBeTruthy();
  expect(facadeStub().callsTo("EnrollRunner").length).toBe(1);
  uninstallFacade();
});

// The license's runner capacity lives here, with the runner work it governs:
// nothing is read until asked, and an instance is settled only by the
// explicit release or reconcile a person chose.
test("runner capacity is shown and settled explicitly, never silently", async () => {
  const user = userEvent.setup();
  const settled: { instance: string; reconcile: boolean }[] = [];
  const held = () => ({
    state: "completed" as const,
    organization: "example-hospital",
    authority: "local-runner",
    instances: 2, active: 1, stale: 1, free: 0,
    admissions: [
      { instance: "build-4821", admitted: "2026-09-20T09:00:00Z", lease_until: "2026-09-20T10:00:00Z", state: "active" },
      { instance: "build-4822", admitted: "2026-09-20T09:30:00Z", lease_until: "2026-09-20T09:40:00Z", state: "stale" },
    ],
  });
  const facade = installFacade({
    ShowRunnerAdmissions: () => held(),
    SettleRunnerAdmission: async (request) => { settled.push(request); return held(); },
  });
  render(
    <IndicatorsContext.Provider value={indicatorTable()}>
      <RunnerPanel />
    </IndicatorsContext.Provider>,
  );
  await user.click(screen.getByRole("button", { name: "Runner capacity" }));
  expect(await screen.findByText(/1 active, 1 stale, 0 free of 2 granted instances/)).toBeTruthy();
  expect(screen.getByText(/Stale capacity is held until an operator reconciles it/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Reconcile build-4822" }));
  expect(settled).toEqual([{ instance: "build-4822", reconcile: true }]);
  await user.click(screen.getByRole("button", { name: "Release build-4821" }));
  expect(settled).toEqual([{ instance: "build-4822", reconcile: true }, { instance: "build-4821", reconcile: false }]);
  expect(facade.calls.length).toBeGreaterThan(0);
  uninstallFacade();
});

// A preview is bound to the configuration and job file it read: changing
// either withdraws the prepared input ID and the send until a fresh preview.
test("a job preview is bound to its configuration and job file, and a late answer cannot restore it", async () => {
  const user = userEvent.setup();
  const facade = installFacade({ InspectRunnerJob: async () => previewed() });
  render(<RunnerPanel />);
  const send = () => screen.getByRole("button", { name: "Send job" }) as HTMLButtonElement;
  expect(send().disabled).toBe(true);
  await preparedJob(user);
  expect(send().disabled).toBe(false);
  expect(screen.getByText(`Send job sends ${JOB} (job nightly-001) to environment lab under prepared inputs ${INPUT_ID}, after the runner's own admission and your explicit approval.`)).toBeTruthy();

  await user.type(sendJobFile(), "x");
  expect(send().disabled).toBe(true);
  expect((screen.getByLabelText("Prepared input ID") as HTMLInputElement).value).toBe("");
  await user.clear(sendJobFile());
  await user.type(sendJobFile(), JOB);
  expect(send().disabled).toBe(true);
  await previewJob(user);
  await user.type(screen.getByLabelText("Runner configuration file"), "x");
  expect(send().disabled).toBe(true);

  // While a preview runs, the inputs it reads hold still, and no job is
  // running for Cancel job to name.
  const parked = facade.park("InspectRunnerJob");
  await user.click(screen.getByRole("button", { name: "Preview job" }));
  expect((sendJobFile() as HTMLInputElement).disabled).toBe(true);
  expect((screen.getByLabelText("Runner configuration file") as HTMLInputElement).disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Cancel job" })).toBeNull();
  parked.resolve(previewed());
  await screen.findByText(/^Send job sends/);
  expect(facade.callsTo("ExecuteRunnerJob")).toHaveLength(0);
  uninstallFacade();
});

test("the runner's work is split into status and jobs and four administrator views", async () => {
  const user = userEvent.setup();
  installFacade({});
  render(<RunnerPanel />);
  const views = screen.getByRole("tablist", { name: "Runner views" });
  expect(within(views).getAllByRole("tab").map((tab) => tab.textContent)).toEqual([
    "Status and jobs", "Configuration", "Access grant", "Recovery", "Update verification",
  ]);
  expect(within(views).getByRole("tab", { selected: true }).textContent).toBe("Status and jobs");
  expect(screen.queryByLabelText("Key lookup program")).toBeNull();
  within(views).getByRole("tab", { name: "Status and jobs" }).focus();
  await user.keyboard("{ArrowRight}");
  expect(within(views).getByRole("tab", { selected: true }).textContent).toBe("Configuration");
  expect(screen.getByLabelText("Key lookup program")).toBeTruthy();
  expect(screen.getByText(/Never the key itself/)).toBeTruthy();
  expect(screen.getByLabelText("Key lookup arguments")).toBeTruthy();
  expect(screen.getByLabelText("Update verification key")).toBeTruthy();
  expect(screen.getByText(/standard base64\. Not the client private key/)).toBeTruthy();
  await user.keyboard("{End}");
  expect(within(views).getByRole("tab", { selected: true }).textContent).toBe("Update verification");
  expect(screen.getByRole("button", { name: "Verify update" })).toBeTruthy();
  await user.click(within(views).getByRole("tab", { name: "Access grant" }));
  expect(screen.getByLabelText("Engine version (optional)")).toBeTruthy();
  expect(screen.getByText("Leave empty to name this build's engine.")).toBeTruthy();
  uninstallFacade();
});

const OPENED: SchedulePreviewResult = {
  state: "completed",
  identity: "9".repeat(64),
  concurrency: "serial-skip-missed",
  entries: [
    {
      entry: {
        id: "nightly",
        zone: "America/New_York",
        at: "02:30",
        window_seconds: 900,
        runner_config: "/etc/readmit-runner/config.json",
        spec: "/srv/readmit/spec.json",
        input_sha256: "b".repeat(64),
        route: "https://alerts.example/",
        approved: true,
      },
      pin_state: "computed",
      occurrences: [{ day: "2026-03-08", state: "dst-gap" }],
    },
  ],
};

// Opening a schedule policy fills the editable rows with what it declares,
// asks before replacing unsaved edits, and each row edits and saves back.
test("an opened schedule policy becomes the editable draft and round-trips through save", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    OpenSchedulePolicy: async () => OPENED,
    PreviewSchedulePolicy: async () => OPENED,
    SaveSchedulePolicy: async () => ({ ...OPENED, identity: "8".repeat(64) }),
  });
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "Schedules" }));
  await user.type(screen.getByLabelText("Schedule policy file"), "/srv/readmit/schedules.json");
  await user.click(screen.getByRole("button", { name: "Open schedule policy" }));
  const row = within(await screen.findByRole("group", { name: "Schedule 1" }));
  expect((row.getByLabelText("Schedule ID") as HTMLInputElement).value).toBe("nightly");
  expect((row.getByLabelText("Time zone") as HTMLInputElement).value).toBe("America/New_York");
  expect((row.getByLabelText("Start window (seconds)") as HTMLInputElement).value).toBe("900");
  expect((row.getByRole("checkbox", { name: /Approve notifications/ }) as HTMLInputElement).checked).toBe(true);

  // A preview describes the rows it was asked for; editing withdraws it.
  expect(within(screen.getByRole("group", { name: "Opened policy" })).getByText("9".repeat(64))).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Preview schedule policy" }));
  expect(await within(await screen.findByRole("group", { name: "Policy preview" })).findByText(/2026-03-08: dst-gap/)).toBeTruthy();
  await user.clear(row.getByLabelText("Daily time"));
  await user.type(row.getByLabelText("Daily time"), "03:15");
  expect(screen.queryByRole("group", { name: "Policy preview" })).toBeNull();
  // A changed notification URL is not the approved one.
  await user.type(row.getByLabelText("Notification URL"), "x");
  expect((row.getByRole("checkbox", { name: /Approve notifications/ }) as HTMLInputElement).checked).toBe(false);

  await user.type(screen.getByLabelText("Policy revision file"), "/srv/readmit/schedules-2.json");
  await user.click(screen.getByRole("button", { name: "Save schedule policy" }));
  const saved = facade.oneCall("SaveSchedulePolicy")[0] as { entries: { id: string; at: string; approved: boolean }[] };
  expect(saved.entries).toHaveLength(1);
  expect(saved.entries[0]).toMatchObject({ id: "nightly", at: "03:15", approved: false });
  expect(await screen.findByText("8".repeat(64))).toBeTruthy();
  expect(screen.getByText(/Nothing was installed on the hub/)).toBeTruthy();

  // Opening again over unsaved edits asks first; keeping the draft keeps it.
  await user.type(row.getByLabelText("Schedule ID"), "-edited");
  await user.click(screen.getByRole("button", { name: "Open schedule policy" }));
  await user.click(await screen.findByRole("button", { name: "Keep draft" }));
  expect((row.getByLabelText("Schedule ID") as HTMLInputElement).value).toBe("nightly-edited");
  await user.click(screen.getByRole("button", { name: "Open schedule policy" }));
  await user.click(await screen.findByRole("button", { name: "Replace draft" }));
  expect((within(screen.getByRole("group", { name: "Schedule 1" })).getByLabelText("Schedule ID") as HTMLInputElement).value).toBe("nightly");
  uninstallFacade();
});

// The opened-policy note says the draft holds the opened file only while it
// does: once the rows differ from that file, the note says so instead, and
// the file's own read stays shown beside it.
test("the opened-policy note is qualified once the draft differs from the opened file", async () => {
  const user = userEvent.setup();
  installFacade({ OpenSchedulePolicy: async () => OPENED });
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "Schedules" }));
  await user.type(screen.getByLabelText("Schedule policy file"), "/srv/readmit/schedules.json");
  await user.click(screen.getByRole("button", { name: "Open schedule policy" }));
  const row = within(await screen.findByRole("group", { name: "Schedule 1" }));
  const opened = () => within(screen.getByRole("group", { name: "Opened policy" }));
  expect(opened().getByText(/^Opened this policy file into the draft below\./)).toBeTruthy();

  await user.clear(row.getByLabelText("Daily time"));
  await user.type(row.getByLabelText("Daily time"), "03:15");
  expect(opened().queryByText(/Opened this policy file into the draft below/)).toBeNull();
  expect(opened().getByText(/^The draft below differs from this policy file\./)).toBeTruthy();
  expect(opened().getByText("9".repeat(64))).toBeTruthy();

  // Typing the opened value back makes the draft the opened file again.
  await user.clear(row.getByLabelText("Daily time"));
  await user.type(row.getByLabelText("Daily time"), "02:30");
  expect(opened().getByText(/^Opened this policy file into the draft below\./)).toBeTruthy();

  // Keeping an edited draft over a new read never claims the file was opened
  // into it.
  await user.type(row.getByLabelText("Schedule ID"), "-edited");
  await user.click(screen.getByRole("button", { name: "Open schedule policy" }));
  await user.click(await screen.findByRole("button", { name: "Keep draft" }));
  expect(opened().queryByText(/Opened this policy file into the draft below/)).toBeNull();
  expect(opened().getByText(/^The draft below differs from this policy file\./)).toBeTruthy();
  uninstallFacade();
});

// The format needs at least one schedule. Removing the last row leaves an
// editable empty draft that says so; nothing reports scheduling stopped and
// nothing is saved in its place.
test("removing the last schedule leaves an empty draft that cannot be saved and says the installed policy is unchanged", async () => {
  const user = userEvent.setup();
  const facade = installFacade({ OpenSchedulePolicy: async () => OPENED });
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "Schedules" }));
  await user.type(screen.getByLabelText("Schedule policy file"), "/srv/readmit/schedules.json");
  await user.click(screen.getByRole("button", { name: "Open schedule policy" }));
  await screen.findByRole("group", { name: "Schedule 1" });
  await user.type(screen.getByLabelText("Policy revision file"), "/srv/readmit/schedules-2.json");
  await user.click(screen.getByRole("button", { name: "Remove from draft" }));
  expect(screen.queryByRole("group", { name: "Schedule 1" })).toBeNull();
  expect(screen.getByText(/A schedule policy must declare at least one.*the installed policy is unchanged and scheduling has not stopped/)).toBeTruthy();
  expect((screen.getByRole("button", { name: "Preview schedule policy" }) as HTMLButtonElement).disabled).toBe(true);
  expect((screen.getByRole("button", { name: "Save schedule policy" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo("SaveSchedulePolicy")).toHaveLength(0);
  // The draft stays editable.
  await user.click(screen.getByRole("button", { name: "Add schedule" }));
  expect((screen.getByRole("button", { name: "Preview schedule policy" }) as HTMLButtonElement).disabled).toBe(false);
  uninstallFacade();
});

// Paths the workflow names on the CI agent are grouped apart from the one
// file written on this computer.
test("CI agent paths and the local workflow output file are grouped apart", async () => {
  const user = userEvent.setup();
  installFacade({});
  await ciTab(user);
  const agent = within(screen.getByRole("group", { name: "Paths on the CI agent" }));
  for (const label of [
    "Executable path on CI agent",
    "Operation policy on CI agent",
    "Suite file on CI agent",
    "Environment ID",
    "Run folder on CI agent",
    "Coverage file on CI agent",
  ]) {
    expect(agent.getByLabelText(label)).toBeTruthy();
  }
  expect(agent.getByText("Fresh per invocation; never a resume path.")).toBeTruthy();
  const local = within(screen.getByRole("group", { name: "On this computer" }));
  expect(local.getByLabelText("Workflow output file")).toBeTruthy();
  expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toContain("Verify gate");
  uninstallFacade();
});
