import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RunnerPanel } from "./RunnerPanel";
import { installFacade, uninstallFacade, facadeStub, Parked } from "./testkit/wails";
import type {
  RunnerEnrollmentResult,
  RunnerInspectResult,
  RunnerExecutionResult,
  RunnerRecoveryResult,
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
  await user.type(await screen.findByLabelText("Configuration path"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: /Enroll \(probe admission\)/ }));
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
  await user.type(await screen.findByLabelText("Configuration path"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: /Enroll \(probe admission\)/ }));
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
    ExecuteRunnerJob: async () => execution,
  });
  render(<RunnerPanel />);
  await user.type(await screen.findByLabelText("Job document"), "/srv/jobs/nightly-001.json");
  await user.click(screen.getByRole("button", { name: "Execute job" }));
  expect(facade.oneCall("ExecuteRunnerJob")[0]).toMatchObject({
    config_path: "",
    job_path: "/srv/jobs/nightly-001.json",
    expected_identity: "",
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
  await user.type(await screen.findByLabelText("Configuration path"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: /Enroll \(probe admission\)/ }));
  expect(await screen.findByText(/environment leased or recovering/)).toBeTruthy();
  expect(facade.callsTo("EnrollRunner").length).toBe(1);
  uninstallFacade();
});

test("cancellation keeps uncertain delivery and recovery reads without sending", async () => {
  const user = userEvent.setup();
  const parked = new Parked();
  const facade = installFacade({
    Cancel: async () => {},
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
  await user.type(await screen.findByLabelText("Job document"), "/srv/jobs/nightly-001.json");
  await user.click(screen.getByRole("button", { name: "Execute job" }));
  // The named cancel control reaches only this panel's operation.
  await user.click(await screen.findByRole("button", { name: "Cancel" }));
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
  await user.type(screen.getByLabelText("Retained job id for recovery"), "nightly-001");
  await user.click(screen.getByRole("button", { name: /Read recovery/ }));
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
    ExecuteRunnerJob: () => parked.arrive() as Promise<RunnerExecutionResult>,
    ReadRunnerConfig: async () => inspection({ state: "failed", reason: "the selected file must be a private regular file" }),
  });
  render(<RunnerPanel />);
  await user.type(await screen.findByLabelText("Job document"), "/srv/jobs/nightly-001.json");
  // From the job document: the pin, then Preflight, then Execute.
  await user.tab();
  await user.tab();
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Execute job" }));
  await user.keyboard("{Enter}");
  const cancel = await screen.findByRole("button", { name: "Cancel" });
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
  await user.click(screen.getByRole("button", { name: /Preview revision/ }));
  expect(facade.callsTo("PreviewSchedulePolicy").length).toBe(1);
  expect(await screen.findByText(/2026-03-08: dst-gap/)).toBeTruthy();
  expect(await screen.findByText(/serial-skip-missed/)).toBeTruthy();
  expect(screen.getByText(/recorded and skipped, never replayed/)).toBeTruthy();
  await user.type(await screen.findByLabelText("Revision destination"), "/tmp/schedules.json");
  await user.click(screen.getByRole("button", { name: /Save revision/ }));
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
  await user.click(screen.getByRole("button", { name: /Preview revision/ }));
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
  await user.click(screen.getByRole("button", { name: /Generate handoff/ }));
  expect(facade.oneCall("SaveCIHandoff")[0]).toMatchObject({ integration: "posix" });
  expect(await screen.findByText(/Install it as the customer administrator\./)).toBeTruthy();
  expect(await screen.findByText(/\$READMIT_BIN.*suite ci/s)).toBeTruthy();

  await user.type(
    screen.getByLabelText("CI output directory"),
    "/var/lib/readmit-ci/run-1",
  );
  await user.click(screen.getByRole("button", { name: /Inspect CI results/ }));
  expect(
    await screen.findByText((_, element) => element?.textContent === "Suite gate: passed (exit 0)."),
  ).toBeTruthy();
  expect(
    await screen.findByText((_, element) => element?.textContent === "Change gate: passed (exit 0)."),
  ).toBeTruthy();
  expect(facade.oneCall("InspectCIResults")[0]).toBe("/var/lib/readmit-ci/run-1");

  await user.type(screen.getByLabelText("Gate policy file"), "/srv/readmit/gate-policy.json");
  await user.click(screen.getByRole("button", { name: /Inspect gate policy/ }));
  expect(await screen.findByText("e".repeat(64))).toBeTruthy();
  expect(await screen.findByText(/approves nothing/i)).toBeTruthy();
  uninstallFacade();
});

/** The CI tab of a freshly rendered panel. */
async function ciTab(user: ReturnType<typeof userEvent.setup>) {
  render(<RunnerPanel />);
  await user.click(await screen.findByRole("tab", { name: "CI handoff" }));
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
  expect(screen.queryByLabelText("Reviewed gate policy identity")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Generate handoff" }));
  expect(facade.callsTo("SaveCIHandoff")[0]?.args[0]).not.toHaveProperty("gate");

  // The step is chosen from the keyboard: the checkbox follows the handoff
  // destination, and Space checks it.
  await user.click(screen.getByLabelText("Handoff destination"));
  await user.tab();
  const step = screen.getByRole("checkbox", { name: /Add the reviewed change-gate step after the suite/ });
  expect(document.activeElement).toBe(step);
  await user.keyboard(" ");
  expect((step as HTMLInputElement).checked).toBe(true);
  expect(screen.getByText(/never replaces the suite's exit status/)).toBeTruthy();
  const fields: [string, string][] = [
    ["Release references", GATE_STEP.releases],
    ["Promotion approval", GATE_STEP.promotion],
    ["Promotion approval identity", GATE_STEP.promotion_identity],
    ["Target revision (operator-declared)", GATE_STEP.revision],
    ["Reviewed baseline run directory", GATE_STEP.baseline],
    ["Reviewed gate policy", GATE_STEP.policy],
    ["Reviewed gate policy identity", "B".repeat(64)],
    ["Gate snapshot directory (fresh per invocation)", GATE_STEP.snapshot_directory],
  ];
  for (const [label, value] of fields) {
    await user.type(screen.getByLabelText(label), value);
  }
  await user.click(screen.getByRole("button", { name: "Generate handoff" }));
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
  await user.clear(screen.getByLabelText("Reviewed gate policy identity"));
  await user.type(screen.getByLabelText("Reviewed gate policy identity"), GATE_STEP.policy_identity);
  await user.click(screen.getByRole("button", { name: "Generate handoff" }));
  expect(facade.callsTo("SaveCIHandoff")[2]?.args[0]).toMatchObject({ gate: GATE_STEP });
  expect(await screen.findByText("Saved to /srv/readmit/handoff.sh. Install it as the customer administrator.")).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();

  // Unchecked, the next handoff asks for no gate again, whatever was typed.
  await user.click(step);
  expect(screen.queryByLabelText("Reviewed gate policy identity")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Generate handoff" }));
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
  await ciTab(user);
  const verify = screen.getByRole("button", { name: "Verify retained gate" }) as HTMLButtonElement;
  expect(verify.disabled).toBe(true);
  await user.type(screen.getByLabelText("Retained gate snapshot"), "/var/lib/readmit-ci/gate-1");
  expect(verify.disabled).toBe(true);
  await user.type(screen.getByLabelText("Pinned gate policy identity"), GATE_STEP.policy_identity);
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
  await user.clear(screen.getByLabelText("Retained gate snapshot"));
  expect(screen.queryByText(/Retained change gate:/)).toBeNull();
  await user.type(screen.getByLabelText("Retained gate snapshot"), "/var/lib/readmit-ci/gate-2");
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
  await user.type(screen.getByLabelText("Pinned gate policy identity"), "0");
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
  await ciTab(user);
  await user.type(screen.getByLabelText("Retained gate snapshot"), "/var/lib/readmit-ci/gate-1");
  await user.type(screen.getByLabelText("Pinned gate policy identity"), GATE_STEP.policy_identity);
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Verify retained gate" }));
  await user.keyboard("{Enter}");
  const cancelControl = await screen.findByRole("button", { name: "Cancel verification" });
  expect(document.activeElement).toBe(cancelControl);
  expect(screen.getByText("Verifying the retained snapshot…")).toBeTruthy();
  expect((screen.getByRole("button", { name: "Generate handoff" }) as HTMLButtonElement).disabled).toBe(true);
  // The snapshot and its pin hold still while they are verified.
  expect((screen.getByLabelText("Retained gate snapshot") as HTMLInputElement).disabled).toBe(true);
  expect((screen.getByLabelText("Pinned gate policy identity") as HTMLInputElement).disabled).toBe(true);
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
  await ciTab(user);
  await user.type(screen.getByLabelText("Gate policy file"), "/srv/readmit/gate-policy.json");
  await user.click(screen.getByRole("button", { name: "Inspect gate policy" }));
  expect(await screen.findByText("e".repeat(64))).toBeTruthy();
  // The identity shown beside the path it was read from is one a person pins;
  // beside another path it would be pinned by mistake.
  await user.type(screen.getByLabelText("Gate policy file"), ".next");
  expect(screen.queryByText("e".repeat(64))).toBeNull();

  await user.type(screen.getByLabelText("CI output directory"), "/var/lib/readmit-ci/run-1");
  await user.click(screen.getByRole("button", { name: "Inspect CI results" }));
  expect(await screen.findByText((_, element) => element?.tagName === "P" && element.textContent === "Suite gate: passed (exit 0).")).toBeTruthy();
  await user.type(screen.getByLabelText("CI output directory"), "0");
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
  await user.type(await screen.findByLabelText("Configuration path"), "/etc/readmit-runner/config.json");
  await user.click(screen.getByRole("button", { name: /Enroll \(probe admission\)/ }));
  expect(await screen.findByText(/Refused: the application did not answer/)).toBeTruthy();
  expect(facadeStub().callsTo("EnrollRunner").length).toBe(1);
  uninstallFacade();
});
