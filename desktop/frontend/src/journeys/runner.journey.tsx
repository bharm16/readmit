// The saved regression test run by an enrolled customer runner against the
// real hub. The window writes the runner's configuration and the hub-side
// grant from their structured forms; the hub's operator issues the runner its
// token and installs the grant; a job pinned to the saved test's prepared
// inputs is preflighted offline; the runner is inspected, refused while the
// restarted hub holds new leases, then admitted; the job executes once
// against the independent downstream system, its verdict following the
// system's defect, its recovery read offline, and a second submission of the
// same job refused. The window's own durable run and the command line's
// runner, over the same configuration, reach the same verdict. A grant that
// went stale with an update is refused by the hub until the window's
// revision of the installed policy replaces it; a job cancelled while its
// delivery waits on the downstream system stays uncertain, and its job id,
// occupied from then on, is never run again by the window or the command line.
import { afterEach, beforeEach, expect, test } from "vitest";
import { waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press } from "../testkit/journey";
import { fillRunnerForm, RUNNER_REFUSED, runnerView, runOnce, savedAckTest, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/scheduling-investigation";
const HUB_PROJECT = "scheduling";
const ENVIRONMENT = "scheduling-downstream";

const BOOKED = "20260102100000+0000";
const MOVED = "20260103110000+0000";

/** Waits as a person waits for a hold or a lease to lapse. */
function pause(milliseconds: number) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/** Saves the job document for the job id and spec the form holds. */
async function saveJob(user: UserEvent, view: ReturnType<typeof within>, id: string) {
  await enter(user, view.getByLabelText("Job document destination"), journey.path(`runner/documents/${id}.json`));
  await press(user, view.getByRole("button", { name: "Save job document" }));
  expect(await outcome(view, new RegExp(`^Saved to .*${id}\\.json\\.$`))).toBe(`Saved to ${journey.path(`runner/documents/${id}.json`)}.`);
}

/** Executes the job document the form holds and returns the facade's answer
 * once it has settled. */
async function execute(user: UserEvent, view: ReturnType<typeof within>) {
  const asked = journey.callsTo("ExecuteRunnerJob").length;
  await press(user, view.getByRole("button", { name: "Execute job" }));
  await waitFor(() => expect(journey.callsTo("ExecuteRunnerJob")[asked]?.settled).toBe(true), { timeout: 60_000 });
  return journey.callsTo("ExecuteRunnerJob")[asked]?.result;
}

/** The status or refusal line the runner view shows after an action. */
async function outcome(view: ReturnType<typeof within>, pattern: RegExp) {
  return (await view.findByText(byContent(pattern))).textContent ?? "";
}

test(
  "an enrolled runner executes the saved test once against the downstream system through the real hub, to the verdict the window's own run and the command line's runner reach",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(HUB_PROJECT, [{ subject: "analyst", role: "analyst" }]);
    const { downstream } = await savedAckTest(user, journey, "defective");
    // The window's own durable run of the saved test, on the system's defect:
    // the verdict every runner execution below is held to.
    expect(await runOnce(user, downstream, "run-defective")).toBe("assertion_failed");
    expect(downstream.received()).toHaveLength(2);
    const token = hub.runnerToken("runner");
    const runnerRoot = journey.makePrivateFolder("runner/root");
    journey.makePrivateFolder("runner/documents");

    // The runner's configuration, generated from the form.
    const view = await runnerView(user, "Runner", "Runner");
    await enter(user, view.getByLabelText("Hub URL"), `https://${hub.address}`);
    const [configProject] = view.getAllByLabelText("Project");
    await enter(user, configProject!, HUB_PROJECT);
    const [configEnvironment] = view.getAllByLabelText("Environment");
    await enter(user, configEnvironment!, ENVIRONMENT);
    await enter(user, view.getByLabelText("Runner root"), runnerRoot);
    await enter(user, view.getByLabelText("CA file"), hub.certificateAuthority);
    await enter(user, view.getByLabelText("Client certificate"), hub.clientCertificate);
    await enter(user, view.getByLabelText("Key reader program"), "/bin/cat");
    await enter(user, view.getByLabelText("Key reader arguments (one per line)"), hub.clientKey);
    await enter(user, view.getByLabelText("Token reader program"), "/bin/cat");
    await enter(user, view.getByLabelText("Token reader arguments (one per line)"), token);
    await enter(user, view.getByLabelText("Approved update key (standard base64)"), "A".repeat(43) + "=");
    await enter(user, view.getByLabelText("Approved update engine"), "next-approved-build");
    const [configDestination] = view.getAllByLabelText("Destination");
    await enter(user, configDestination!, journey.path("runner/documents/runner.json"));
    await press(user, view.getByRole("button", { name: "Save configuration" }));
    expect(await outcome(view, /^Saved to .*runner\.json\.$/)).toBe(`Saved to ${journey.path("runner/documents/runner.json")}.`);

    // The hub-side grant for the runner in this environment, generated from
    // its own form; installing it is the hub operator's step.
    await enter(user, view.getAllByLabelText("Project").at(-1)!, HUB_PROJECT);
    await enter(user, view.getByLabelText("Subject"), "runner");
    await enter(user, view.getAllByLabelText("Environment").at(-1)!, ENVIRONMENT);
    await enter(user, view.getAllByLabelText("Destination").at(-1)!, journey.path("runner/documents/runners.json"));
    await press(user, view.getByRole("button", { name: "Save grant revision" }));
    expect(await outcome(view, /^Saved to .*runners\.json\. /)).toBe(
      `Saved to ${journey.path("runner/documents/runners.json")}. Installing it on the hub is the administrator's action.`,
    );

    // A job pinned to the saved test's prepared inputs, preflighted without
    // contacting the hub or sending.
    downstream.reset();
    await enter(user, view.getByLabelText("Configuration path"), journey.path("runner/documents/runner.json"));
    await enter(user, view.getByLabelText("Job id"), "reschedule-001");
    await enter(user, view.getByLabelText("Spec path"), journey.path(PROJECT, "reschedule-ack-test.json"));
    await saveJob(user, view, "reschedule-001");
    await enter(user, view.getByLabelText("Job document"), journey.path("runner/documents/reschedule-001.json"));
    await press(user, view.getByRole("button", { name: "Preflight job" }));
    const prepared = await outcome(view, /^Prepared inputs /);
    expect(prepared).toMatch(new RegExp(`^Prepared inputs [0-9a-f]{64} bind environment ${ENVIRONMENT}\\.$`));
    expect(downstream.received()).toHaveLength(2);

    // The operator installs the grant. The runner is inspected offline, then
    // refused while the restarted hub holds new leases, then admitted.
    await hub.restart(["-runner-policy", journey.path("runner/documents/runners.json")]);
    await press(user, view.getByRole("button", { name: "Inspect runner" }));
    expect(await outcome(view, /^Health: /)).toBe("Health: idle, 0 retained job(s).");
    await press(user, view.getByRole("button", { name: "Enroll (probe admission)" }));
    const leased = /hub refused admission \(Conflict\): environment leased or recovering$/;
    expect(await outcome(view, /^Admission refused: /)).toMatch(leased);
    await pause(10_500);
    await press(user, view.getByRole("button", { name: "Enroll (probe admission)" }));
    expect(await outcome(view, /^Admitted: /)).toMatch(/^Admitted: lease until \S+, at most 300s per job and 100 retained jobs\.$/);

    // While the probe's own lease holds the environment, the job's admission
    // is refused with the hub's reason and nothing is sent; once it lapses,
    // the job runs once and fails on the downstream's defect.
    expect(await execute(user, view)).toMatchObject({ state: "failed", reason: expect.stringMatching(leased) });
    expect(downstream.received()).toHaveLength(2);
    await pause(10_500);
    const defective = await execute(user, view);
    expect(defective).toMatchObject({ state: "completed", summary: { state: "assertion_failed", planned: 2, recorded: 2, delivery_uncertain: false } });
    expect(await outcome(view, /^Job reschedule-001 finished: /)).toBe("Job reschedule-001 finished: assertion_failed.");
    expect(downstream.received()).toHaveLength(4);
    expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });

    // Recovery reads the retained job offline, and the same job is never
    // run again, whatever is asked.
    await enter(user, view.getByLabelText("Retained job id for recovery"), "reschedule-001");
    await press(user, view.getByRole("button", { name: "Read recovery" }));
    expect(await outcome(view, /^reschedule-001: /)).toBe("reschedule-001: 2 acknowledged, 0 uncertain, 0 not attempted. Recovery never sends.");
    expect(await execute(user, view)).toMatchObject({
      state: "failed",
      reason: "runner operation refused; check private configuration, admission, and retained status",
    });
    expect(downstream.received()).toHaveLength(4);

    // The command line's runner executes a job the window prepared, over the
    // same configuration and the same defect. It, the window's runner and the
    // window's own durable run retain one verdict, as the command line reads
    // each of them back.
    downstream.reset();
    await enter(user, view.getByLabelText("Job id"), "cli-001");
    await saveJob(user, view, "cli-001");
    const cli = await journey.commandLine([
      "--operation-policy",
      journey.path("vendor-delivered-license", "operation-policy.json"),
      "runner",
      "execute",
      journey.path("runner/documents/cli-001.json"),
      "--config",
      journey.path("runner/documents/runner.json"),
      "--send",
    ]);
    const verdict = (summary: Record<string, unknown>) => ({
      state: summary.state,
      stop_reason: summary.stop_reason,
      planned: summary.planned,
      recorded: summary.recorded,
      delivery_uncertain: summary.delivery_uncertain,
    });
    const expected = { state: "assertion_failed", stop_reason: "assertion_failed", planned: 2, recorded: 2, delivery_uncertain: false };
    expect(verdict(JSON.parse(cli.stdout) as Record<string, unknown>)).toEqual(expected);
    expect(defective).toMatchObject({ summary: expected });
    for (const run of [journey.path(PROJECT, "run-defective"), journey.path("runner/root/reschedule-001/run"), journey.path("runner/root/cli-001/run")]) {
      const status = await journey.commandLine(["run", "status", run, "--json"]);
      expect(status.code).toBe(1);
      expect(verdict(JSON.parse(status.stdout) as Record<string, unknown>)).toEqual(expected);
    }
    expect(downstream.received()).toHaveLength(6);
    expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });

    // Once the system is fixed, the next job passes.
    downstream.setMode("fixed");
    downstream.reset();
    await enter(user, view.getByLabelText("Job id"), "reschedule-002");
    await saveJob(user, view, "reschedule-002");
    await enter(user, view.getByLabelText("Job document"), journey.path("runner/documents/reschedule-002.json"));
    expect(await execute(user, view)).toMatchObject({ state: "completed", summary: { state: "passed" } });
    expect(await outcome(view, /^Job reschedule-002 finished: /)).toBe("Job reschedule-002 finished: passed.");
    expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
    const status = await journey.commandLine(["run", "status", journey.path("runner/root/reschedule-002/run"), "--json"]);
    expect(JSON.parse(status.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed" });

    // A recurring schedule for the same job, authored as a revision of the
    // hub's schedule policy: its pin is the prepared identity the preflight
    // showed, its occurrences follow the zone's clock through a spring-forward
    // gap, and the hub's operator initializes and serves it.
    const pin = prepared.replace(/^Prepared inputs ([0-9a-f]{64}) .*$/, "$1");
    const schedules = await runnerView(user, "Schedules", "Recurring schedules");
    await press(user, schedules.getByRole("button", { name: "Add schedule" }));
    await enter(user, schedules.getByLabelText("Id"), "nightly-reschedule");
    await enter(user, schedules.getByLabelText("Zone"), "America/Chicago");
    await enter(user, schedules.getByLabelText("At (HH:MM)"), "02:30");
    await enter(user, schedules.getByLabelText("Window seconds"), "600");
    await enter(user, schedules.getByLabelText("Runner configuration"), journey.path("runner/documents/runner.json"));
    await enter(user, schedules.getByLabelText("Spec path"), journey.path(PROJECT, "reschedule-ack-test.json"));
    await enter(user, schedules.getByLabelText("Input pin (SHA-256)"), "0".repeat(64));
    await enter(user, schedules.getByLabelText("Notification route (HTTPS origin)"), "https://alerts.journey.test/");
    await user.click(schedules.getByLabelText("Approved for notifications"));
    await enter(user, schedules.getByLabelText("Preview anchor day (optional)"), "2026-03-07");
    await enter(user, schedules.getByLabelText("Revision destination"), journey.path("runner/documents/schedules.json"));

    // A pin that is not the spec's prepared inputs is shown as a mismatch,
    // and no revision is written with it.
    await press(user, schedules.getByRole("button", { name: "Preview revision" }));
    expect(await schedules.findByText(byContent(/^nightly-reschedule at /))).toBeTruthy();
    expect(schedules.getByText(byContent(/^nightly-reschedule at /)).textContent).toBe(
      `nightly-reschedule at 02:30 in America/Chicago, window 600s — pin mismatch (${pin}).`,
    );
    await press(user, schedules.getByRole("button", { name: "Save revision" }));
    expect(
      await schedules.findByText(
        "Refused: an entry's input pin does not match the prepared inputs of its spec; recompute the pin before enabling the schedule",
      ),
    ).toBeTruthy();

    // With the prepared identity as its pin, the revision previews its
    // occurrences — the spring-forward night has no 02:30 and is marked, not
    // shifted — and is saved.
    await enter(user, schedules.getByLabelText("Input pin (SHA-256)"), pin);
    await press(user, schedules.getByRole("button", { name: "Preview revision" }));
    await waitFor(() =>
      expect(schedules.getByText(byContent(/^nightly-reschedule at /)).textContent).toBe(
        `nightly-reschedule at 02:30 in America/Chicago, window 600s — pin computed (${pin}).`,
      ),
    );
    expect(schedules.getByText("2026-03-08: dst-gap")).toBeTruthy();
    expect(schedules.getByText(byContent(/^Concurrency: serial-skip-missed\. /))).toBeTruthy();
    await press(user, schedules.getByRole("button", { name: "Save revision" }));
    await waitFor(() => expect(journey.callsTo("SaveSchedulePolicy").at(-1)?.result).toMatchObject({ state: "completed" }));
    const identity = (await schedules.findByText(byContent(/^Policy identity [0-9a-f]{64} — /))).textContent?.replace(/^Policy identity ([0-9a-f]{64}) .*$/, "$1");
    expect(JSON.parse(journey.readFile("runner/documents/schedules.json"))).toMatchObject({
      schema: "readmit-hub-schedules/v1",
      concurrency: "serial-skip-missed",
      schedules: [{ id: "nightly-reschedule", zone: "America/Chicago", at: "02:30", window_seconds: 600, input_sha256: pin, approved: true }],
    });
    expect(identity).toMatch(/^[0-9a-f]{64}$/);

    // The hub's operator computes the same pin from the same spec, initializes
    // the revision, and serves it beside the runner grant.
    expect((await hub.operate(["-directory", journey.path(PROJECT, "reschedule-ack-test.json"), "schedule-pin"])).trim()).toBe(pin);
    await hub.operate(["-schedule-policy", journey.path("runner/documents/schedules.json"), "schedule-init"]);
    await hub.restart([
      "-runner-policy",
      journey.path("runner/documents/runners.json"),
      "-schedule-policy",
      journey.path("runner/documents/schedules.json"),
    ]);

    // The installed revision reopens in the window with the identity the hub
    // binds its journal to.
    const installed = await runnerView(user, "Schedules", "Recurring schedules");
    await enter(user, installed.getByLabelText("Installed policy (to read)"), journey.path("runner/documents/schedules.json"));
    const opened = journey.callsTo("OpenSchedulePolicy").length;
    await press(user, installed.getByRole("button", { name: "Open installed policy" }));
    await waitFor(() => expect(journey.callsTo("OpenSchedulePolicy")[opened]?.settled).toBe(true));
    expect(journey.callsTo("OpenSchedulePolicy")[opened]?.result).toMatchObject({ state: "completed", identity });
    expect(await installed.findByText(identity!)).toBeTruthy();
  },
);

test(
  "a runner whose hub grant went stale is refused until the window's grant revision is installed, a job cancelled while its delivery is unacknowledged stays uncertain, and its occupied job id is never run again by the window or the command line",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(HUB_PROJECT, [{ subject: "analyst", role: "analyst" }]);
    const { downstream } = await savedAckTest(user, journey, "fixed");
    const token = hub.runnerToken("runner");
    const runnerRoot = journey.makePrivateFolder("runner/root");
    journey.makePrivateFolder("runner/documents");
    const configuration = journey.path("runner/documents/runner.json");
    /** The command line's runner executing a job document the window saved,
     * over the configuration the window saved. */
    const runnerExecute = (id: string) =>
      journey.commandLine([
        "--operation-policy",
        journey.path("vendor-delivered-license", "operation-policy.json"),
        "runner",
        "execute",
        journey.path(`runner/documents/${id}.json`),
        "--config",
        configuration,
        "--send",
      ]);
    // The build this machine's runner is, as its installed executable says.
    const build = (await journey.commandLine(["--version"])).stdout.replace(/^readmit version (\S+)\n$/, "$1");

    const view = await runnerView(user, "Runner", "Runner");
    await fillRunnerForm(user, view, {
      hub: `https://${hub.address}`,
      project: HUB_PROJECT,
      environment: ENVIRONMENT,
      root: runnerRoot,
      ca: hub.certificateAuthority,
      certificate: hub.clientCertificate,
      key: { program: "/bin/cat", arguments: hub.clientKey },
      token: { program: "/bin/cat", arguments: token },
      updateKey: "A".repeat(43) + "=",
      updateEngine: "next-approved-build",
      destination: configuration,
    });
    await press(user, view.getByRole("button", { name: "Save configuration" }));
    expect(await outcome(view, /^Saved to .*runner\.json\.$/)).toBe(`Saved to ${configuration}.`);

    // The grant the hub's operator installed before this runner's last update
    // pins the build it ran then.
    await enter(user, view.getAllByLabelText("Project").at(-1)!, HUB_PROJECT);
    await enter(user, view.getByLabelText("Subject"), "runner");
    await enter(user, view.getAllByLabelText("Environment").at(-1)!, ENVIRONMENT);
    await enter(user, view.getByLabelText("Engine (empty names this build)"), "retired-build");
    await enter(user, view.getAllByLabelText("Destination").at(-1)!, journey.path("runner/documents/runners.json"));
    await press(user, view.getByRole("button", { name: "Save grant revision" }));
    expect(await outcome(view, /^Saved to .*runners\.json\. /)).toBe(
      `Saved to ${journey.path("runner/documents/runners.json")}. Installing it on the hub is the administrator's action.`,
    );
    await hub.restart(["-runner-policy", journey.path("runner/documents/runners.json")]);

    // Under the stale grant the hub refuses this build's admission, with its
    // own reason.
    await enter(user, view.getByLabelText("Configuration path"), configuration);
    await press(user, view.getByRole("button", { name: "Inspect runner" }));
    expect(await outcome(view, /^Health: /)).toBe("Health: idle, 0 retained job(s).");
    await press(user, view.getByRole("button", { name: "Enroll (probe admission)" }));
    expect(await outcome(view, /^Admission refused: /)).toMatch(/hub refused admission \(Forbidden\): version or environment refused$/);

    // The window revises the installed policy. A policy a later release
    // wrote is refused by the admission protocol's reader, and nothing is
    // written.
    journey.writeFile(
      "runner/documents/runners-later.json",
      journey.readFile("runner/documents/runners.json").replace("readmit-runner-policy/v1", "readmit-runner-policy/v2"),
    );
    await enter(user, view.getByLabelText("Existing policy (optional)"), journey.path("runner/documents/runners-later.json"));
    await enter(user, view.getByLabelText("Engine (empty names this build)"), "");
    await enter(user, view.getAllByLabelText("Destination").at(-1)!, journey.path("runner/documents/runners-revision.json"));
    await press(user, view.getByRole("button", { name: "Save grant revision" }));
    expect(await outcome(view, /^Refused: /)).toBe("Refused: the existing runner policy could not be read through its own strict reader");
    expect(() => journey.readFile("runner/documents/runners-revision.json")).toThrow();

    // Over the installed policy, saved from the keyboard, this build replaces
    // the stale grant for the pair rather than joining it. The revision is a
    // new file, shown as it was written.
    await enter(user, view.getByLabelText("Existing policy (optional)"), journey.path("runner/documents/runners.json"));
    await tabTo(user, view.getByRole("button", { name: "Save grant revision" }));
    await user.keyboard("{Enter}");
    expect(await outcome(view, /^Saved to .*runners-revision\.json\. /)).toBe(
      `Saved to ${journey.path("runner/documents/runners-revision.json")}. Installing it on the hub is the administrator's action.`,
    );
    const revision = journey.readFile("runner/documents/runners-revision.json");
    expect(JSON.parse(revision)).toEqual({
      schema: "readmit-runner-policy/v1",
      runners: [
        { project: HUB_PROJECT, subject: "runner", environment: ENVIRONMENT, engine: build, spec: "readmit-test/v1", profile: "readmit-siu-v1", max_seconds: 300, max_jobs: 100 },
      ],
    });
    expect(view.getByText(byContent(/^\{\n {2}"schema": "readmit-runner-policy\/v1"/)).textContent).toBe(revision);
    expect(JSON.parse(journey.readFile("runner/documents/runners.json")).runners[0].engine).toBe("retired-build");

    // The operator installs the revision and restarts the hub with it. Once
    // the restarted hub issues leases, the same probe is admitted.
    await hub.restart(["-runner-policy", journey.path("runner/documents/runners-revision.json")]);
    await pause(10_500);
    await press(user, view.getByRole("button", { name: "Enroll (probe admission)" }));
    expect(await outcome(view, /^Admitted: /)).toMatch(/^Admitted: lease until \S+, at most 300s per job and 100 retained jobs\.$/);
    await pause(10_500);

    // A job is preflighted and executed from the keyboard while the
    // downstream system holds its acknowledgement, and cancelled from the
    // keyboard once the first message has reached it.
    downstream.reset();
    downstream.holdAcknowledgements();
    await enter(user, view.getByLabelText("Job id"), "reschedule-001");
    await enter(user, view.getByLabelText("Spec path"), journey.path(PROJECT, "reschedule-ack-test.json"));
    await saveJob(user, view, "reschedule-001");
    await enter(user, view.getByLabelText("Job document"), journey.path("runner/documents/reschedule-001.json"));
    await press(user, view.getByRole("button", { name: "Preflight job" }));
    expect(await outcome(view, /^Prepared inputs /)).toMatch(new RegExp(`^Prepared inputs [0-9a-f]{64} bind environment ${ENVIRONMENT}\\.$`));
    // Execute is disabled while the job runs, and the focus moves to Cancel,
    // the one action the running job offers.
    const asked = journey.callsTo("ExecuteRunnerJob").length;
    await tabTo(user, view.getByRole("button", { name: "Execute job" }));
    await user.keyboard("{Enter}");
    const cancel = await view.findByRole("button", { name: "Cancel" });
    await waitFor(() => expect(document.activeElement).toBe(cancel));
    await waitFor(() => expect(downstream.received()).toHaveLength(1), { timeout: 60_000 });
    await user.keyboard("{Enter}");
    await waitFor(() => expect(journey.callsTo("ExecuteRunnerJob")[asked]?.settled).toBe(true), { timeout: 60_000 });
    expect(journey.callsTo("Cancel").at(-1)?.args).toEqual(["runner"]);
    expect(journey.callsTo("ExecuteRunnerJob")[asked]?.result).toMatchObject({
      state: "completed",
      summary: { state: "delivery_uncertain", stop_reason: "cancelled", delivery_uncertain: true },
    });
    expect(await outcome(view, /^Job reschedule-001 finished: /)).toBe(
      "Job reschedule-001 finished: delivery_uncertain. Delivery stayed uncertain; nothing will be resent from here.",
    );
    downstream.releaseAcknowledgement();
    expect(downstream.received()).toHaveLength(1);

    // Recovery reads the cancelled job as uncertain, in the window and on the
    // command line, and offers nothing to resume.
    await enter(user, view.getByLabelText("Retained job id for recovery"), "reschedule-001");
    await press(user, view.getByRole("button", { name: "Read recovery" }));
    expect(await outcome(view, /^reschedule-001: /)).toBe("reschedule-001: 0 acknowledged, 1 uncertain, 1 not attempted. Recovery never sends.");
    const recovery = await journey.commandLine(["run", "status", `${runnerRoot}/reschedule-001/run`, "--recovery", "--json"]);
    expect(recovery.code).toBe(2);
    expect(JSON.parse(recovery.stdout)).toMatchObject({
      schema: "readmit-run-recovery/v1",
      run: { delivery_uncertain: true },
      uncertain: 1,
      not_attempted: 1,
      safe_to_repeat: false,
    });

    // Its job id is occupied from now on. The preflight says so, the window's
    // execution and the command line's runner are each refused with the
    // runner's reason, and the downstream system receives nothing more.
    await press(user, view.getByRole("button", { name: "Preflight job" }));
    expect(await outcome(view, /^Preflight refused: /)).toBe(
      "Preflight refused: job id reschedule-001 is already retained in this runner's root and never runs again; read its recovery, and save a new job document with a new job id once receiver state is established",
    );
    expect(await execute(user, view)).toMatchObject({ state: "failed", job_id: "reschedule-001", reason: RUNNER_REFUSED });
    expect(await runnerExecute("reschedule-001")).toMatchObject({ code: 1, stdout: "", stderr: `readmit: ${RUNNER_REFUSED}\n` });
    expect(downstream.received()).toHaveLength(1);

    // A new job id the window writes runs unchanged through the command
    // line's runner, over the configuration the window wrote.
    downstream.reset();
    await enter(user, view.getByLabelText("Job id"), "reschedule-002");
    await saveJob(user, view, "reschedule-002");
    const next = await runnerExecute("reschedule-002");
    expect(next.code).toBe(0);
    expect(JSON.parse(next.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed", delivery_uncertain: false });
    expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
  },
);
