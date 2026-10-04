// The documents a customer runner reads, prepared from Settings › Runners
// with no hub: a runner's setup, refused while it is not one the runner
// accepts, reviewed and exported once for the runner host's administrator; a
// staged update checked against the deployment key that setup pins, and
// never run; a job for a saved test, previewed against the environment its
// prepared inputs bind and refused under a runner for another; and a
// schedule policy for the hub's operator, exported and opened again as the
// hub would read it. The command line's runner reads what the window wrote
// and answers each check the same way.
import { afterEach, beforeEach, expect, test } from "vitest";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { screen, waitFor, within } from "@testing-library/react";
import { enter, Journey, press } from "../testkit/journey";
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
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The build the customer approved as this runner's next update. */
const APPROVED = "next-approved-build";
/** The runner's own refusal, the sentence the command line prints after
 * "readmit: ". */
const RUNNER_REFUSED = "runner operation refused; check private configuration, admission, and retained status";
const TEST = "Reschedule is acknowledged";

type Scope = ReturnType<typeof within>;

/** What an administrator enters for a runner: its connection and where it
 * runs. The files sit in the journey's root; nothing here resolves them. */
interface Setup {
  name: string;
  hub: string;
  environment: string;
  updateKey: string;
}

/** The host's files the connection names, written where an administrator
 * keeps them. */
function stageConnectionFiles(): void {
  journey.writeFile("runner/tls/ca.pem", "synthetic CA\n");
  journey.writeFile("runner/tls/client.pem", "synthetic certificate\n");
  journey.writeFile("runner/customer-secret-reader", "#!/bin/sh\nexit 1\n", 0o700);
  journey.makePrivateFolder("runner/root");
}

/** Chooses one file through the host's dialog from a ChosenPath control. */
async function choose(user: UserEvent, sheet: Scope, control: string, file: string, title: string): Promise<void> {
  await journey.chooseFiles([file], title);
  await pressServed(user, journey, sheet.getByRole("button", { name: `Choose ${control}` }), "ChooseRunnerPath");
}

/** Fills Add runner's Connection step. */
async function connection(user: UserEvent, sheet: Scope, setup: Setup): Promise<void> {
  await enter(user, sheet.getByLabelText("Name"), setup.name);
  await enter(user, sheet.getByLabelText("Customer hub"), setup.hub);
  await enter(user, sheet.getByLabelText("Hub project"), "scheduling");
  await choose(user, sheet, "hub certificate authority", journey.path("runner/tls/ca.pem"), "Choose a CA certificate");
  await choose(user, sheet, "runner certificate", journey.path("runner/tls/client.pem"), "Choose a client certificate");
  await choose(user, sheet, "key reader", journey.path("runner/customer-secret-reader"), "Choose the credential reader");
  await enter(user, sheet.getByLabelText("Key name"), "runner-key");
  await choose(user, sheet, "token reader", journey.path("runner/customer-secret-reader"), "Choose the credential reader");
  await enter(user, sheet.getByLabelText("Token name"), "runner-token");
  await enter(user, sheet.getByLabelText("Deployment key"), setup.updateKey);
  await enter(user, sheet.getByLabelText("Approved build"), APPROVED);
}

/** Fills Add runner's Assignment step for a runner on another host. */
async function assignment(user: UserEvent, sheet: Scope, setup: Setup): Promise<void> {
  await enter(user, sheet.getByLabelText("Hub environment"), setup.environment);
  await press(user, sheet.getByRole("radio", { name: "Another host" }));
  await enter(user, sheet.getByLabelText("Working folder"), journey.path("runner/root"));
}

/** Adds a runner for another host from Settings › Runners and exports its
 * setup into a new file named in the host's save dialog. Returns the file. */
async function addRunner(user: UserEvent, setup: Setup, file: string): Promise<string> {
  await goTo(user,"Tests");
  await press(user,await page().findByRole("button",{name:"Reusable work"}));
  await press(user,await screen.findByRole("menuitem",{name:"Runners"}));
  const panel = within(await page().findByRole("region", { name: "Runners" }));
  // A runner left open is closed back to the list first.
  const back = panel.queryByRole("button", { name: "Back to runners" });
  if (back) await press(user, back);
  await press(user, (await panel.findAllByRole("button", { name: "Add runner" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add runner" }));
  await connection(user, sheet, setup);
  await press(user, sheet.getByRole("button", { name: "Next" }));
  await assignment(user, sheet, setup);
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "PreviewRunnerConfig");
  await sheet.findByText("Writes the setup for the runner host's administrator; nothing is installed.");
  const exported = journey.path(file);
  await journey.nameNewFolder(exported, "Export setup");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Export setup" }), "SaveRunnerConfig");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add runner" })).toBeNull(), { timeout: 10_000 });
  await within(await page().findByRole("region", { name: setup.name }, { timeout: 10_000 })).findByText("Setup required");
  return exported;
}

/** Opens one of the open runner's tasks from its More menu. */
async function task(user: UserEvent, name: string): Promise<Scope> {
  await press(user, page().getByRole("button", { name: "More runner actions" }));
  await press(user, await screen.findByRole("menuitem", { name }));
  return within(await screen.findByRole("dialog", { name }));
}

test("a runner's setup is refused while the runner would refuse it, reviewed, exported once for another host and read unchanged by the command line's runner", async () => {
  const user = userEvent.setup();
  stageConnectionFiles();
  const setup: Setup = { name: "Lab runner", hub: "https://hub.example:8443", environment: "lab", updateKey: "A".repeat(43) + "=" };
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");

  // A hub named over plain HTTP is not one the runner connects to: the
  // setup is refused before its review, and nothing is saved or written.
  await goToView(user, "Settings", "Runners");
  const panel = within(await page().findByRole("region", { name: "Runners" }));
  await press(user, (await panel.findAllByRole("button", { name: "Add runner" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add runner" }));
  await connection(user, sheet, { ...setup, hub: "http://hub.example:8443" });
  await press(user, sheet.getByRole("button", { name: "Next" }));
  await assignment(user, sheet, setup);
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "PreviewRunnerConfig");
  expect((await sheet.findByRole("alert")).textContent).toBe(
    "the runner configuration is not complete; every member is required, the hub must be an HTTPS origin, and both credential references name an absolute program",
  );
  expect(sheet.queryByRole("button", { name: "Export setup" })).toBeNull();
  expect(journey.callsTo("SaveItem")).toHaveLength(0);

  // Corrected, the review shows what the runner is, and Export setup writes
  // it once into a new file the administrator names.
  await press(user, sheet.getByRole("button", { name: "Back" }));
  await enter(user, sheet.getByLabelText("Customer hub"), setup.hub);
  await press(user, sheet.getByRole("button", { name: "Next" }));
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "PreviewRunnerConfig");
  const reviewed = within(await sheet.findByLabelText("Runner", { selector: "dl" }));
  expect(reviewed.getByText("Customer hub").nextElementSibling?.textContent).toBe(setup.hub);
  expect(reviewed.getByText("Hub environment").nextElementSibling?.textContent).toBe("lab");
  expect(reviewed.getByText("Approved build").nextElementSibling?.textContent).toBe(APPROVED);
  const exported = journey.path("runner/readmit-runner.json");
  await journey.nameNewFolder(exported, "Export setup");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Export setup" }), "SaveRunnerConfig");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add runner" })).toBeNull(), { timeout: 10_000 });
  // Exporting installed nothing: the runner stays Setup required.
  const detail = within(await page().findByRole("region", { name: setup.name }, { timeout: 10_000 }));
  expect(await detail.findByText("Setup required")).toBeTruthy();
  expect(JSON.parse(journey.readFile("runner/readmit-runner.json"))).toEqual({
    schema: "readmit-runner/v1",
    hub: setup.hub,
    project: "scheduling",
    environment: "lab",
    root: journey.path("runner/root"),
    ca: journey.path("runner/tls/ca.pem"),
    certificate: journey.path("runner/tls/client.pem"),
    key: { command: journey.path("runner/customer-secret-reader"), arguments: ["runner-key"] },
    token: { command: journey.path("runner/customer-secret-reader"), arguments: ["runner-token"] },
    update_key: setup.updateKey,
    update_engine: APPROVED,
  });

  // The command line's runner reads it as its own configuration.
  const status = await journey.commandLine(["runner", "status", "--config", exported]);
  expect(status).toMatchObject({ code: 0, stderr: "" });
  expect(JSON.parse(status.stdout)).toEqual({ schema: "readmit-runner-status/v1", state: "idle", jobs: 0 });

  // A second export never replaces the file the first one wrote.
  const written = journey.readFile("runner/readmit-runner.json");
  await press(user, page().getByRole("button", { name: "More runner actions" }));
  await journey.nameNewFolder(exported, "Export setup");
  const again = journey.callsTo("SaveRunnerConfig").length;
  await press(user, await screen.findByRole("menuitem", { name: "Export setup" }));
  await waitFor(() => expect(journey.callsTo("SaveRunnerConfig")[again]?.settled).toBe(true), { timeout: 10_000 });
  expect(journey.callsTo("SaveRunnerConfig")[again]?.result).toMatchObject({ state: "failed", reason: "a file is already there; name a new file" });
  expect(journey.readFile("runner/readmit-runner.json")).toBe(written);
});

test("a staged runner update is verified against the pinned deployment key without being run, and an unsigned or a foreign candidate is refused as the command line refuses it", async () => {
  const user = userEvent.setup();
  stageConnectionFiles();
  // The customer's deployment authority approves the next build; another
  // authority's key signs too, but no runner here pins it.
  const authority = journey.deploymentAuthority();
  const foreign = journey.deploymentAuthority();
  // The candidate the administrator staged is a program that, if anything
  // ran it, would leave a mark beside itself.
  const candidate = journey.writeFile("staged/readmit", `#!/bin/sh\n: > '${journey.path("staged", "ran")}'\n`, 0o700);
  const sha256 = journey.digest("staged/readmit");
  const approved = journey.writeFile("staged/update.json", authority.manifest({ engine: APPROVED, sha256 }));
  const unsigned = journey.writeFile("staged/unsigned.json", authority.manifest({ engine: APPROVED, sha256, signed: false }));
  const foreignSigned = journey.writeFile("staged/foreign.json", foreign.manifest({ engine: APPROVED, sha256 }));

  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const exported = await addRunner(user, { name: "Lab runner", hub: "https://hub.example:8443", environment: "lab", updateKey: authority.publicKey }, "runner/readmit-runner.json");

  // Nothing is checked until the manifest and the candidate are both named.
  let update = await task(user, "Update");
  expect((update.getByRole("button", { name: "Verify update" }) as HTMLButtonElement).disabled).toBe(true);
  await choose(user, update, "update manifest", approved, "Choose the update manifest");
  expect((update.getByRole("button", { name: "Verify update" }) as HTMLButtonElement).disabled).toBe(true);
  await choose(user, update, "staged program", candidate, "Choose the staged program");

  // Verified, as the command line verifies it.
  await pressServed(user, journey, update.getByRole("button", { name: "Verify update" }), "VerifyRunnerUpdate");
  expect(await update.findByText(`Verified for build ${APPROVED}.`, { exact: false })).toBeTruthy();
  expect(await journey.commandLine(["runner", "verify-update", approved, candidate, "--config", exported])).toEqual({
    code: 0,
    stdout: '{"schema":"readmit-runner-update-check/v1","verified":true}\n',
    stderr: "",
  });

  // Naming another manifest withdraws that answer. A manifest nobody signed,
  // and one signed by an authority the runner does not pin, are each
  // refused, as the command line refuses them.
  for (const manifest of [unsigned, foreignSigned]) {
    await choose(user, update, "update manifest", manifest, "Choose the update manifest");
    expect(update.queryByText(/^Verified for build /)).toBeNull();
    await pressServed(user, journey, update.getByRole("button", { name: "Verify update" }), "VerifyRunnerUpdate");
    expect((await update.findByRole("alert")).textContent).toBe(`the staged candidate is not the approved update; ${RUNNER_REFUSED}`);
    expect(await journey.commandLine(["runner", "verify-update", manifest, candidate, "--config", exported])).toEqual({
      code: 1,
      stdout: "",
      stderr: `readmit: ${RUNNER_REFUSED}\n`,
    });
    await press(user, update.getByRole("button", { name: "Cancel" }));
    update = await task(user, "Update");
    await choose(user, update, "staged program", candidate, "Choose the staged program");
  }

  // Nothing ran the candidate, and its bytes are the ones staged.
  expect(journey.digest("staged/readmit")).toBe(sha256);
  expect(() => journey.readFile("staged/ran")).toThrow();
});

/** Creates a test from every message of the open case, sent to the
 * environment and decided by the acknowledgement contract: the reschedule is
 * accepted, AA at MSA-1, with the operator's reset declared. One Create test. */
async function createAckTest(user: UserEvent, environment: string): Promise<void> {
  const table = await screen.findByRole("table", { name: "Messages" });
  await waitFor(()=>expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
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

/** The saved test's own contract: the one test specification in the
 * project, as a runner's job names it. */
function savedSpec(project: string): string {
  const relative = project.slice(journey.path().length + 1);
  const found = filesUnder(project).filter((file) => file.endsWith(".json") && journey.readFile(`${relative}/${file}`).includes('"readmit-test/v1"'));
  expect(found).toHaveLength(1);
  return `${project}/${found[0]}`;
}

test("a job is written once for a saved test, previewed against the environment its prepared inputs bind and refused under a runner for another, and a schedule policy for it is exported and opened again", async () => {
  const user = userEvent.setup();
  stageConnectionFiles();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed", ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule is refused");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule is refused"));
  await openedCase();
  await createAckTest(user, environment);
  const spec = savedSpec(project);

  // A runner for another environment: a job id it would not accept is
  // refused and nothing is written; a valid one is written once, and its
  // preview names the environment the prepared inputs bind and refuses to
  // run it here. Nothing is asked of a hub or sent.
  await addRunner(user, { name: "Training runner", hub: "https://hub.example:8443", environment: "scheduling-training", updateKey: "A".repeat(43) + "=" }, "runner/training.json");
  let sheet = await task(user, "Run job");
  await press(user, sheet.getByRole("radio", { name: "New job" }));
  await enter(user, sheet.getByLabelText("Job name"), "Nightly 001");
  await choose(user, sheet, "test", spec, "Choose the test to run");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "SaveRunnerJob");
  expect((await sheet.findByRole("alert")).textContent).toBe(
    "a job id is 1-64 lowercase letters, digits or hyphens starting with a letter or digit, and the spec is one absolute path",
  );
  expect(() => journey.readFile("runner/nightly-001.json")).toThrow();
  await enter(user, sheet.getByLabelText("Job name"), "nightly-001");
  await journey.nameNewFolder(journey.path("runner/nightly-001.json"), "New job");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "SaveRunnerJob");
  await waitFor(() => expect(journey.callsTo("InspectRunnerJob").at(-1)?.settled).toBe(true), { timeout: 30_000 });
  const refused = journey.callsTo("InspectRunnerJob").at(-1)!.result as { reason: string; input_identity: string };
  const bound = /^the prepared inputs bind environment (\S+); this runner is configured for scheduling-training$/.exec(refused.reason)?.[1];
  expect(bound, refused.reason).toBeTruthy();
  expect((await sheet.findByRole("alert")).textContent).toBe(refused.reason);
  expect(JSON.parse(journey.readFile("runner/nightly-001.json"))).toEqual({ schema: "readmit-runner-job/v1", id: "nightly-001", spec });
  await press(user, sheet.getByRole("button", { name: "Cancel" }));

  // Under a runner for the environment the inputs bind, the same job file
  // previews to its prepared inputs and says what Run job would send.
  await addRunner(user, { name: "QA runner", hub: "https://hub.example:8443", environment: bound!, updateKey: "A".repeat(43) + "=" }, "runner/qa.json");
  sheet = await task(user, "Run job");
  await press(user, sheet.getByRole("radio", { name: "Job file" }));
  await journey.chooseFiles([journey.path("runner/nightly-001.json")], "Choose the job");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Choose job file" }), "ChooseRunnerPath");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Next" }), "InspectRunnerJob");
  const job = within(await sheet.findByLabelText("Job", { selector: "dl" }));
  expect(job.getByText("Job").nextElementSibling?.textContent).toBe("nightly-001");
  expect(job.getByText("Hub environment").nextElementSibling?.textContent).toBe(bound);
  expect(sheet.getByText(`Sends this job's messages to ${bound} once.`)).toBeTruthy();
  expect((journey.callsTo("InspectRunnerJob").at(-1)!.result as { input_identity: string }).input_identity).toBe(refused.input_identity);
  await press(user, sheet.getByRole("button", { name: "Cancel" }));
  expect(journey.callsTo("ExecuteRunnerJob")).toHaveLength(0);
  expect(downstream.received()).toHaveLength(0);

  // A schedule for the job, exported as a policy for the hub's operator:
  // its pin is computed from the test, and the spring-forward night has no
  // 02:30 and is marked skipped, not shifted.
  await press(user, page().getByRole("button", { name: "Schedules" }));
  await page().findByRole("heading", { level: 1, name: "Schedules" }, { timeout: 10_000 });
  await press(user, page().getByRole("button", { name: "More schedule actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Export policy" }));
  const policy = within(await screen.findByRole("dialog", { name: "Export policy" }));
  await enter(user, policy.getByLabelText("Entry name"), "nightly-reschedule");
  await enter(user, policy.getByLabelText("Time"), "02:30");
  await user.selectOptions(policy.getByLabelText("Time zone"), "America/Chicago");
  const runnerChoice = policy.getByLabelText("Runner");
  await user.selectOptions(runnerChoice, await within(runnerChoice).findByRole("option", { name: "QA runner" }, { timeout: 10_000 }));
  await journey.chooseFiles([spec], "Choose the test to run");
  await pressServed(user, journey, policy.getByRole("button", { name: "Choose test" }), "ChooseRunnerPath");
  await press(user, policy.getByRole("button", { name: "Add entry" }));
  await pressServed(user, journey, policy.getByRole("button", { name: "Next" }), "PreviewSchedulePolicy");
  const entry = within(await policy.findByLabelText("nightly-reschedule", { selector: "dl" }));
  expect(entry.getByText("Pin").nextElementSibling?.textContent).toBe("Matches the test");
  expect(entry.getByText("Time").nextElementSibling?.textContent).toBe("02:30 America/Chicago");
  const exported = journey.path("runner/schedules.json");
  await journey.nameNewFolder(exported, "Export policy");
  await pressServed(user, journey, policy.getByRole("button", { name: "Export policy" }), "SaveSchedulePolicy");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Export policy" })).toBeNull());
  const saved = journey.callsTo("SaveSchedulePolicy").at(-1)!.result as { identity: string };
  expect(saved.identity).toMatch(/^[0-9a-f]{64}$/);
  const written = JSON.parse(journey.readFile("runner/schedules.json")) as { schema: string; schedules: { id: string; zone: string; at: string; spec: string; input_sha256: string }[] };
  expect(written.schema).toBe("readmit-hub-schedules/v1");
  expect(written.schedules).toMatchObject([{ id: "nightly-reschedule", zone: "America/Chicago", at: "02:30", spec, input_sha256: refused.input_identity }]);

  // Opened again as the installed policy, it holds the entry it was saved
  // with and the identity the hub binds its journal to. A policy a later
  // release wrote is refused by the reader the hub runs.
  await press(user, page().getByRole("button", { name: "More schedule actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Export policy" }));
  const reopened = within(await screen.findByRole("dialog", { name: "Export policy" }));
  await journey.chooseFiles([exported], "Open policy");
  const opening = journey.callsTo("OpenSchedulePolicy").length;
  await pressServed(user, journey, reopened.getByRole("button", { name: "Open policy…" }), "ChooseRunnerPath");
  await waitFor(() => expect(journey.callsTo("OpenSchedulePolicy")[opening]?.settled).toBe(true));
  expect(journey.callsTo("OpenSchedulePolicy")[opening]?.result).toMatchObject({ state: "completed", identity: saved.identity });
  const entries = within(await reopened.findByRole("table", { name: "Entries" }));
  expect(entries.getAllByRole("row").slice(1).map((row) => row.querySelector("td")?.textContent)).toEqual(["nightly-reschedule"]);
  journey.writeFile("runner/schedules-later.json", journey.readFile("runner/schedules.json").replace("readmit-hub-schedules/v1", "readmit-hub-schedules/v2"));
  await journey.chooseFiles([journey.path("runner/schedules-later.json")], "Open policy");
  await pressServed(user, journey, reopened.getByRole("button", { name: "Open policy…" }), "ChooseRunnerPath");
  await waitFor(() => expect(journey.callsTo("OpenSchedulePolicy")[opening + 1]?.settled).toBe(true));
  expect(journey.callsTo("OpenSchedulePolicy")[opening + 1]?.result).toMatchObject({
    state: "failed",
    reason: "the schedule policy could not be read through its own strict reader",
  });
  expect(await reopened.findByText("the schedule policy could not be read through its own strict reader")).toBeTruthy();
});
