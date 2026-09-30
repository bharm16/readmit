// The saved regression test run by a customer runner against the real hub,
// from Settings › Runners. The window adds the runner and exports its setup,
// and writes the hub-side grant from Access; the hub's operator issues the
// runner its token and installs the grant; the runner's admission is refused
// while the restarted hub holds new leases, then granted; a job pinned to the
// saved test's prepared inputs runs once against the independent downstream
// system, its verdict following the system's defect, its recovery read
// offline, and the same job id never run again. The window's own run and the
// command line's runner, over the same setup, reach the same verdict, and a
// schedule for the job is exported with the pin the hub's operator computes.
// A grant that went stale with an update is refused by the hub until the
// window's revision of the installed policy replaces it, and a job whose
// delivery is never acknowledged stays uncertain and is never run again by
// the window or the command line.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import type { Hub } from "../testkit/hub.js";
import { goTo, goToView, page } from "../testkit/navigation";
import { filesUnder } from "./probes.js";
import {
  activateLicense,
  configureEnvironment,
  createProject,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  openedCase,
  pressServed,
  reviewRun,
  sendReviewed,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const HUB_PROJECT = "scheduling";
/** The environment the saved test's prepared inputs bind: the target name
 * of the window's "Scheduling QA" environment. */
const ENVIRONMENT = "scheduling-qa";
const TEST = "Reschedule is acknowledged";
const BOOKED = "20260102100000+0000";
const MOVED = "20260103110000+0000";
/** The runner's own refusal, the sentence the command line prints after
 * "readmit: ". */
const RUNNER_REFUSED = "runner operation refused; check private configuration, admission, and retained status";
const LEASED = /hub refused admission \(Conflict\): environment leased or recovering$/;

type Scope = ReturnType<typeof within>;

/** Waits as a person waits for a hold or a lease to lapse. */
function pause(milliseconds: number) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/** Chooses one file through the host's dialog from a ChosenPath control. */
async function choose(user: UserEvent, sheet: Scope, control: string, file: string, title: string): Promise<void> {
  await journey.chooseFiles([file], title);
  await pressServed(user, journey, sheet.getByRole("button", { name: `Choose ${control}` }), "ChooseRunnerPath");
}

/** Creates a test from every message of the open case, sent to the
 * environment and decided by the acknowledgement contract: the reschedule is
 * accepted, AA at MSA-1, with the operator's reset declared. One Create test. */
async function createAckTest(user: UserEvent, environment: string): Promise<void> {
  const table = await screen.findByRole("table", { name: "Messages" });
  for (const box of within(table).getAllByRole("checkbox").filter((box) => !(box as HTMLInputElement).checked && box.closest("tr[data-row-id]"))) {
    await press(user, box);
  }
  await press(user, within(screen.getByRole("group", { name: "Selected messages" })).getByRole("button", { name: "Create test" }));
  await enter(user, await page().findByRole("textbox", { name: "Name" }, { timeout: 10_000 }), TEST);
  const environments = page().getByRole("combobox", { name: "Environment" });
  await waitFor(() => expect(within(environments).queryByRole("option", { name: environment })).toBeTruthy(), { timeout: 10_000 });
  await user.selectOptions(environments, environment);
  await press(user, page().getByRole("radio", { name: "Acknowledgements" }));
  await user.selectOptions(page().getByRole("combobox", { name: "Reset" }), "manual");
  const reset = within(await screen.findByRole("dialog", { name: "Reset instructions" }));
  await enter(user, reset.getByRole("textbox"), "Restart the receiver with an empty appointment ledger.");
  await press(user, reset.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Next" }));
  await press(user, await page().findByRole("button", { name: "Add check" }));
  await press(user, await screen.findByRole("menuitem", { name: "ACK field" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add ack field check" }));
  const message = sheet.getByLabelText("Message") as HTMLSelectElement;
  await user.selectOptions(message, message.options[message.options.length - 1]!.value);
  await user.selectOptions(sheet.getByLabelText("Field"), "MSA-1");
  await enter(user, sheet.getByLabelText("Expected value"), "AA");
  await press(user, sheet.getByRole("button", { name: "Apply" }));
  await press(user, page().getByRole("button", { name: "Review" }));
  await page().findByLabelText("Setup", { selector: "dl" });
  await journey.settled();
  await pressServed(user, journey, page().getByRole("button", { name: "Create test" }), "SaveItem");
  expect(journey.callsTo("SaveItem").at(-1)?.result).toMatchObject({ state: "completed", outcome: "saved" });
}

/** A licensed project over the person's own export, the downstream system in
 * the given mode as its environment, and the acknowledgement test saved.
 * Returns the project, the receiver and the saved test's own contract. */
async function savedAckTest(user: UserEvent, mode: "defective" | "fixed") {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", mode, ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule is refused"));
  await openedCase();
  await createAckTest(user, environment);
  const relative = project.slice(journey.path().length + 1);
  const specs = filesUnder(project).filter((file) => file.endsWith(".json") && journey.readFile(`${relative}/${file}`).includes('"readmit-test/v1"'));
  expect(specs).toHaveLength(1);
  return { project, downstream, spec: `${project}/${specs[0]}` };
}

/** Adds the runner for another host from Settings › Runners: the hub's
 * connection with the client identity and the token its operator issued,
 * each read back by a program, and exports its setup. Returns the file. */
async function addRunner(user: UserEvent, hub: Hub, token: string): Promise<string> {
  const root = journey.makePrivateFolder("runner/root");
  await goToView(user, "Settings", "Runners");
  const panel = within(await page().findByRole("region", { name: "Runners" }));
  await press(user, (await panel.findAllByRole("button", { name: "Add runner" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add runner" }));
  await enter(user, sheet.getByLabelText("Name"), "QA runner");
  await enter(user, sheet.getByLabelText("Customer hub"), `https://${hub.address}`);
  await enter(user, sheet.getByLabelText("Hub project"), HUB_PROJECT);
  await choose(user, sheet, "hub certificate authority", hub.certificateAuthority, "Choose a CA certificate");
  await choose(user, sheet, "runner certificate", hub.clientCertificate, "Choose a client certificate");
  await choose(user, sheet, "key reader", "/bin/cat", "Choose the credential reader");
  await enter(user, sheet.getByLabelText("Key name"), hub.clientKey);
  await choose(user, sheet, "token reader", "/bin/cat", "Choose the credential reader");
  await enter(user, sheet.getByLabelText("Token name"), token);
  await enter(user, sheet.getByLabelText("Deployment key"), "A".repeat(43) + "=");
  await enter(user, sheet.getByLabelText("Approved build"), "next-approved-build");
  await press(user, sheet.getByRole("button", { name: "Next" }));
  await enter(user, sheet.getByLabelText("Hub environment"), ENVIRONMENT);
  await press(user, sheet.getByRole("radio", { name: "Another host" }));
  await enter(user, sheet.getByLabelText("Working folder"), root);
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "PreviewRunnerConfig");
  const setup = journey.path("runner/readmit-runner.json");
  await journey.nameNewFolder(setup, "Export setup");
  await pressServed(user, journey, await sheet.findByRole("button", { name: "Export setup" }), "SaveRunnerConfig");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add runner" })).toBeNull(), { timeout: 10_000 });
  await within(await page().findByRole("region", { name: "QA runner" }, { timeout: 10_000 })).findByText("Setup required");
  return setup;
}

/** Opens one of the open runner's tasks from its More menu. */
async function task(user: UserEvent, name: string): Promise<Scope> {
  await press(user, page().getByRole("button", { name: "More runner actions" }));
  await press(user, await screen.findByRole("menuitem", { name }));
  return within(await screen.findByRole("dialog", { name }));
}

/** Writes the hub-side grant for this runner's subject from Access, over
 * the installed policy when one is named, into a new file. */
async function grant(user: UserEvent, file: string, installed?: string): Promise<void> {
  const sheet = await task(user, "Access");
  if (installed) await choose(user, sheet, "current runner policy", installed, "Choose the runner policy");
  await enter(user, sheet.getByLabelText("Runner subject"), "runner");
  await enter(user, sheet.getByLabelText("Max time (seconds)"), "300");
  await enter(user, sheet.getByLabelText("Max jobs"), "100");
  await press(user, sheet.getByRole("button", { name: "Next" }));
  await journey.nameNewFolder(journey.path(file), "Save grant");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Save grant" }), "SaveRunnerGrant");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Access" })).toBeNull(), { timeout: 10_000 });
}

/** Asks the hub to admit the open runner and returns its answer. */
async function admission(user: UserEvent) {
  const asked = journey.callsTo("EnrollRunner").length;
  await press(user, page().getByRole("button", { name: "Request admission" }));
  await waitFor(() => expect(journey.callsTo("EnrollRunner")[asked]?.settled).toBe(true), { timeout: 30_000 });
  return journey.callsTo("EnrollRunner")[asked]!.result as { state: string; reason?: string };
}

/** Writes a new job for the saved test in Run job and previews it against
 * the open runner, and returns the sheet with the facade's preview. */
async function newJob(user: UserEvent, id: string, spec: string) {
  const sheet = await task(user, "Run job");
  await press(user, sheet.getByRole("radio", { name: "New job" }));
  await enter(user, sheet.getByLabelText("Job name"), id);
  await choose(user, sheet, "test", spec, "Choose the test to run");
  await journey.nameNewFolder(journey.path(`runner/${id}.json`), "New job");
  const previewed = journey.callsTo("InspectRunnerJob").length;
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "SaveRunnerJob");
  await waitFor(() => expect(journey.callsTo("InspectRunnerJob")[previewed]?.settled).toBe(true), { timeout: 30_000 });
  return { sheet, preview: journey.callsTo("InspectRunnerJob")[previewed]!.result as { state: string; reason?: string; input_identity?: string; environment?: string } };
}

/** Previews a job file already written against the open runner. */
async function jobFile(user: UserEvent, id: string) {
  const sheet = await task(user, "Run job");
  await journey.chooseFiles([journey.path(`runner/${id}.json`)], "Choose the job");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Choose job file" }), "ChooseRunnerPath");
  const previewed = journey.callsTo("InspectRunnerJob").length;
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "InspectRunnerJob");
  return { sheet, preview: journey.callsTo("InspectRunnerJob")[previewed]!.result as { state: string; reason?: string } };
}

/** Runs the previewed job once and returns the facade's answer. */
async function runJob(user: UserEvent, sheet: Scope) {
  const asked = journey.callsTo("ExecuteRunnerJob").length;
  await press(user, sheet.getByRole("button", { name: "Run job" }));
  await waitFor(() => expect(journey.callsTo("ExecuteRunnerJob")[asked]?.settled).toBe(true), { timeout: 120_000 });
  return journey.callsTo("ExecuteRunnerJob")[asked]!.result as { state: string; reason?: string; summary?: Record<string, unknown> };
}

/** The retained jobs Recovery lists, and one job's deliveries once chosen. */
async function recovery(user: UserEvent, id: string) {
  const sheet = await task(user, "Recovery");
  await press(user, await sheet.findByRole("button", { name: id }, { timeout: 10_000 }));
  const read = within(await sheet.findByLabelText(`Job ${id}`, { selector: "dl" }));
  const value = (label: string) => read.getByText(label).nextElementSibling?.textContent;
  const counts = { acknowledged: value("Acknowledged"), uncertain: value("Uncertain"), notAttempted: value("Not attempted") };
  expect(sheet.queryByRole("button", { name: /Retry|Resend|Send/ })).toBeNull();
  await press(user, sheet.getByRole("button", { name: "Close" }));
  return counts;
}

/** The verdict a runner summary or run status carries. */
function verdict(summary: Record<string, unknown>) {
  return { state: summary.state, stop_reason: summary.stop_reason, planned: summary.planned, recorded: summary.recorded, delivery_uncertain: summary.delivery_uncertain };
}

test(
  "a customer runner executes the saved test once against the downstream system through the real hub, to the verdict the window's own run and the command line's runner reach",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(HUB_PROJECT, [{ subject: "analyst", role: "analyst" }]);
    const { project, downstream, spec } = await savedAckTest(user, "defective");
    // The window's own run of the saved test, on the system's defect: the
    // verdict every runner execution below is held to.
    await sendReviewed(user, journey, await reviewRun(user, journey, new RegExp(`^${TEST}`)));
    expect(downstream.received()).toHaveLength(2);
    const windowRun = (journey.callsTo("OpenRun").at(-1)?.result as { run?: { item: { summary: { run?: { entry?: string } } } } }).run?.item.summary.run?.entry;
    expect(windowRun).toBeTruthy();

    // The runner's setup, and the grant for it, which installing is the hub
    // operator's step.
    const setup = await addRunner(user, hub, hub.runnerToken("runner"));
    await grant(user, "runner/runners.json");
    expect(JSON.parse(journey.readFile("runner/runners.json"))).toMatchObject({
      schema: "readmit-runner-policy/v1",
      runners: [{ project: HUB_PROJECT, subject: "runner", environment: ENVIRONMENT, max_seconds: 300, max_jobs: 100 }],
    });

    // A job pinned to the saved test's prepared inputs, previewed without
    // contacting the hub or sending.
    downstream.reset();
    let job = await newJob(user, "reschedule-001", spec);
    expect(job.preview).toMatchObject({ state: "completed", environment: ENVIRONMENT, input_identity: expect.stringMatching(/^[0-9a-f]{64}$/) });
    expect(job.sheet.getByText(`Sends this job's messages to ${ENVIRONMENT} once.`)).toBeTruthy();
    const pin = job.preview.input_identity!;
    await press(user, job.sheet.getByRole("button", { name: "Cancel" }));
    expect(downstream.received()).toHaveLength(2);

    // The operator installs the grant. The restarted hub refuses admission
    // while it holds new leases, then admits the runner.
    await hub.restart(["-runner-policy", journey.path("runner/runners.json")]);
    expect((await admission(user)).reason).toMatch(LEASED);
    expect(await page().findByRole("alert")).toBeTruthy();
    await pause(10_500);
    expect(await admission(user)).toMatchObject({ state: "completed", max_seconds: 300, max_jobs: 100 });
    const detail = within(page().getByRole("region", { name: "QA runner" }));
    expect(await detail.findByText("Available")).toBeTruthy();
    expect(detail.getByText("Max jobs").nextElementSibling?.textContent).toBe("100");

    // While the admission's own lease holds the environment, the job is
    // refused with the hub's reason and nothing is sent; once it lapses, the
    // job runs once and fails on the downstream's defect.
    job = await jobFile(user, "reschedule-001");
    expect(await runJob(user, job.sheet)).toMatchObject({ state: "failed", reason: expect.stringMatching(LEASED) });
    expect(downstream.received()).toHaveLength(2);
    // A refused run is previewed again before it is run.
    await press(user, job.sheet.getByRole("button", { name: "Back" }));
    await pressServed(user, journey, job.sheet.getByRole("button", { name: "Next" }), "InspectRunnerJob");
    await pause(10_500);
    const defective = await runJob(user, job.sheet);
    expect(defective).toMatchObject({ state: "completed", summary: { state: "assertion_failed", planned: 2, recorded: 2, delivery_uncertain: false } });
    expect(await job.sheet.findByText("assertion_failed")).toBeTruthy();
    await press(user, job.sheet.getByRole("button", { name: "Cancel" }));
    expect(downstream.received()).toHaveLength(4);
    expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });

    // Recovery reads the retained job offline, and the same job is never run
    // again: its preview names the retained id and offers no run.
    expect(await recovery(user, "reschedule-001")).toEqual({ acknowledged: "2", uncertain: "0", notAttempted: "0" });
    job = await jobFile(user, "reschedule-001");
    expect(job.preview.reason).toBe(
      "job id reschedule-001 is already retained in this runner's root and never runs again; read its recovery, and save a new job document with a new job id once receiver state is established",
    );
    expect(job.sheet.queryByRole("button", { name: "Run job" })).toBeNull();
    await press(user, job.sheet.getByRole("button", { name: "Cancel" }));
    expect(downstream.received()).toHaveLength(4);

    // The command line's runner executes a job the window wrote, over the
    // setup the window exported and the same defect. It, the window's runner
    // and the window's own run retain one verdict, as the command line reads
    // each of them back.
    downstream.reset();
    job = await newJob(user, "cli-001", spec);
    await press(user, job.sheet.getByRole("button", { name: "Cancel" }));
    const cli = await journey.commandLine([
      "--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"),
      "runner", "execute", journey.path("runner/cli-001.json"), "--config", setup, "--send",
    ]);
    const expected = { state: "assertion_failed", stop_reason: "assertion_failed", planned: 2, recorded: 2, delivery_uncertain: false };
    expect(verdict(JSON.parse(cli.stdout) as Record<string, unknown>)).toEqual(expected);
    expect(verdict(defective.summary!)).toEqual(expected);
    for (const run of [`${project}/${windowRun}`, journey.path("runner/root/reschedule-001/run"), journey.path("runner/root/cli-001/run")]) {
      const status = await journey.commandLine(["run", "status", run, "--json"]);
      expect(status.code).toBe(1);
      expect(verdict(JSON.parse(status.stdout) as Record<string, unknown>)).toEqual(expected);
    }
    expect(downstream.received()).toHaveLength(6);
    expect(downstream.ledger()).toEqual({ "PLACER-101": BOOKED });

    // Once the system is fixed, the next job passes.
    downstream.setMode("fixed");
    downstream.reset();
    job = await newJob(user, "reschedule-002", spec);
    expect(await runJob(user, job.sheet)).toMatchObject({ state: "completed", summary: { state: "passed" } });
    await press(user, job.sheet.getByRole("button", { name: "Cancel" }));
    expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
    const passed = await journey.commandLine(["run", "status", journey.path("runner/root/reschedule-002/run"), "--json"]);
    expect(JSON.parse(passed.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed" });

    // A recurring schedule for the same job, exported as a policy for the
    // hub's operator: its pin is the prepared identity the preview showed,
    // which the operator computes from the same test, initializes and serves.
    await press(user, page().getByRole("button", { name: "Schedules" }));
    await page().findByRole("heading", { level: 1, name: "Schedules" }, { timeout: 10_000 });
    await press(user, page().getByRole("button", { name: "More schedule actions" }));
    await press(user, await screen.findByRole("menuitem", { name: "Export policy" }));
    const policy = within(await screen.findByRole("dialog", { name: "Export policy" }));
    await enter(user, policy.getByLabelText("Entry name"), "nightly-reschedule");
    await enter(user, policy.getByLabelText("Time"), "02:30");
    await user.selectOptions(policy.getByLabelText("Time zone"), "America/Chicago");
    await user.selectOptions(policy.getByLabelText("Runner"), "QA runner");
    await choose(user, policy, "test", spec, "Choose the test to run");
    await press(user, policy.getByRole("button", { name: "Add entry" }));
    await pressServed(user, journey, policy.getByRole("button", { name: "Next" }), "PreviewSchedulePolicy");
    const entry = within(await policy.findByLabelText("nightly-reschedule", { selector: "dl" }));
    expect(entry.getByText("Pin").nextElementSibling?.textContent).toBe("Matches the test");
    await journey.nameNewFolder(journey.path("runner/schedules.json"), "Export policy");
    await pressServed(user, journey, policy.getByRole("button", { name: "Export policy" }), "SaveSchedulePolicy");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Export policy" })).toBeNull());
    expect(JSON.parse(journey.readFile("runner/schedules.json"))).toMatchObject({
      schema: "readmit-hub-schedules/v1",
      schedules: [{ id: "nightly-reschedule", zone: "America/Chicago", at: "02:30", input_sha256: pin }],
    });
    expect((await hub.operate(["-directory", spec, "schedule-pin"])).trim()).toBe(pin);
    await hub.operate(["-schedule-policy", journey.path("runner/schedules.json"), "schedule-init"]);
    await hub.restart(["-runner-policy", journey.path("runner/runners.json"), "-schedule-policy", journey.path("runner/schedules.json")]);
  },
);

test(
  "a runner whose hub grant went stale is refused until the window's grant revision is installed, a job whose delivery is never acknowledged stays uncertain, and its job id is never run again by the window or the command line",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(HUB_PROJECT, [{ subject: "analyst", role: "analyst" }]);
    const { downstream, spec } = await savedAckTest(user, "fixed");
    const setup = await addRunner(user, hub, hub.runnerToken("runner"));
    /** The command line's runner executing a job document the window wrote,
     * over the setup the window exported. */
    const runnerExecute = (id: string) =>
      journey.commandLine([
        "--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"),
        "runner", "execute", journey.path(`runner/${id}.json`), "--config", setup, "--send",
      ]);
    // The build this machine's runner is, as its installed executable says.
    const build = (await journey.commandLine(["--version"])).stdout.replace(/^readmit version (\S+)\n$/, "$1");

    // The grant the hub's operator installed before this runner's last
    // update pins the build it ran then. Under it the hub refuses this
    // build's admission, with its own reason.
    const stale = {
      schema: "readmit-runner-policy/v1",
      runners: [{ project: HUB_PROJECT, subject: "runner", environment: ENVIRONMENT, engine: "retired-build", spec: "readmit-test/v1", profile: "readmit-siu-v1", max_seconds: 300, max_jobs: 100 }],
    };
    journey.writeFile("runner/runners.json", `${JSON.stringify(stale)}\n`);
    await hub.restart(["-runner-policy", journey.path("runner/runners.json")]);
    expect((await admission(user)).reason).toMatch(/hub refused admission \(Forbidden\): version or environment refused$/);

    // Access revises the installed policy. A policy a later release wrote is
    // refused by the admission protocol's reader, and nothing is written.
    journey.writeFile("runner/runners-later.json", journey.readFile("runner/runners.json").replace("readmit-runner-policy/v1", "readmit-runner-policy/v2"));
    const later = await task(user, "Access");
    await choose(user, later, "current runner policy", journey.path("runner/runners-later.json"), "Choose the runner policy");
    expect((await later.findByRole("alert")).textContent).toBe("the runner policy could not be read through its own strict reader");
    await enter(user, later.getByLabelText("Runner subject"), "runner");
    await press(user, later.getByRole("button", { name: "Next" }));
    await pressServed(user, journey, later.getByRole("button", { name: "Save grant" }), "SaveRunnerGrant");
    expect(await later.findByText("the existing runner policy could not be read through its own strict reader")).toBeTruthy();
    await press(user, later.getByRole("button", { name: "Cancel" }));
    expect(() => journey.readFile("runner/runners-revision.json")).toThrow();

    // Over the installed policy, the stale grant is shown, and this build
    // replaces it for the pair rather than joining it, in a new file.
    const revising = await task(user, "Access");
    await choose(user, revising, "current runner policy", journey.path("runner/runners.json"), "Choose the runner policy");
    const grants = within(await revising.findByRole("table", { name: "Grants" }));
    expect(grants.getByText("retired-build")).toBeTruthy();
    expect((revising.getByLabelText("Runner subject") as HTMLInputElement).value).toBe("runner");
    await press(user, revising.getByRole("button", { name: "Cancel" }));
    await grant(user, "runner/runners-revision.json", journey.path("runner/runners.json"));
    expect(JSON.parse(journey.readFile("runner/runners-revision.json"))).toEqual({
      schema: "readmit-runner-policy/v1",
      runners: [{ project: HUB_PROJECT, subject: "runner", environment: ENVIRONMENT, engine: build, spec: "readmit-test/v1", profile: "readmit-siu-v1", max_seconds: 300, max_jobs: 100 }],
    });
    expect(JSON.parse(journey.readFile("runner/runners.json")).runners[0].engine).toBe("retired-build");

    // The operator installs the revision. Once the restarted hub issues
    // leases, the runner is admitted.
    await hub.restart(["-runner-policy", journey.path("runner/runners-revision.json")]);
    await pause(10_500);
    expect(await admission(user)).toMatchObject({ state: "completed" });
    await pause(10_500);

    // A job runs while the downstream system holds its acknowledgement: the
    // first message reaches it, is never acknowledged, and the job stays
    // uncertain; nothing is resent.
    downstream.reset();
    downstream.holdAcknowledgements();
    const job = await newJob(user, "reschedule-001", spec);
    const held = await runJob(user, job.sheet);
    expect(held).toMatchObject({ summary: { delivery_uncertain: true } });
    await press(user, job.sheet.getByRole("button", { name: "Cancel" }));
    downstream.releaseAcknowledgement();
    expect(downstream.received()).toHaveLength(1);

    // Recovery reads the job as uncertain, in the window and on the command
    // line, and offers nothing to resume.
    expect(await recovery(user, "reschedule-001")).toEqual({ acknowledged: "0", uncertain: "1", notAttempted: "1" });
    const recovered = await journey.commandLine(["run", "status", journey.path("runner/root/reschedule-001/run"), "--recovery", "--json"]);
    expect(recovered.code).toBe(2);
    expect(JSON.parse(recovered.stdout)).toMatchObject({
      schema: "readmit-run-recovery/v1",
      run: { delivery_uncertain: true },
      uncertain: 1,
      not_attempted: 1,
      safe_to_repeat: false,
    });

    // Its job id is occupied from now on: a fresh preview in the window says
    // the id is retained and offers no run, the command line's runner is
    // refused, and the downstream system receives nothing more.
    const again = await jobFile(user, "reschedule-001");
    expect(again.preview.reason).toMatch(/^job id reschedule-001 is already retained in this runner's root and never runs again; /);
    expect(again.sheet.queryByRole("button", { name: "Run job" })).toBeNull();
    await press(user, again.sheet.getByRole("button", { name: "Cancel" }));
    expect(await runnerExecute("reschedule-001")).toMatchObject({ code: 1, stdout: "", stderr: `readmit: ${RUNNER_REFUSED}\n` });
    expect(downstream.received()).toHaveLength(1);

    // A new job id the window writes runs unchanged through the command
    // line's runner, over the setup the window exported.
    downstream.reset();
    const next = await newJob(user, "reschedule-002", spec);
    await press(user, next.sheet.getByRole("button", { name: "Cancel" }));
    await pause(10_500);
    const ran = await runnerExecute("reschedule-002");
    expect(ran.code, ran.stderr).toBe(0);
    expect(JSON.parse(ran.stdout)).toMatchObject({ schema: "readmit-job/v1", state: "passed", delivery_uncertain: false });
    expect(downstream.ledger()).toEqual({ "PLACER-101": MOVED });
  },
);
