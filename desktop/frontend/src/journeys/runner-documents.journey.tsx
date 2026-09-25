// The documents a customer runner reads, generated in the window's runner
// panel with no hub: the runner's configuration, refused while it is not
// one the runner accepts, previewed and saved once; a staged update checked
// against the deployment key that configuration pins, and never run; a job
// document preflighted to the prepared inputs and environment it pins; and a
// schedule revision reopened as the hub would read it. The command line's
// runner reads what the window wrote and answers each check the same way.
import { afterEach, beforeEach, expect, test } from "vitest";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { waitFor, within } from "@testing-library/react";
import { byContent, enter, Journey, press } from "../testkit/journey";
import { activateLicense, fillRunnerForm, RUNNER_REFUSED, runnerView, savedAckTest, tabTo } from "./steps";
import type { RunnerForm } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/scheduling-investigation";
const ENVIRONMENT = "scheduling-downstream";
/** The build the customer approved as this runner's next update. */
const APPROVED = "next-approved-build";

type View = ReturnType<typeof within>;

/** A configuration whose files all sit in the journey's root: the private
 * runner root the administrator created, the certificate paths and the
 * program that reads the key and token back. Nothing here resolves them. */
function runnerForm(environment: string, destination: string, updateKey = "A".repeat(43) + "="): RunnerForm {
  return {
    hub: "https://hub.example:8443",
    project: "scheduling",
    environment,
    root: journey.path("runner/root"),
    ca: journey.path("runner/tls/ca.pem"),
    certificate: journey.path("runner/tls/client.pem"),
    key: { program: journey.path("runner/customer-secret-reader"), arguments: "runner-key" },
    token: { program: journey.path("runner/customer-secret-reader"), arguments: "runner-token" },
    updateKey,
    updateEngine: APPROVED,
    destination,
  };
}

/** The document the runner view shows for a generated configuration. */
function shownConfiguration(view: View): string {
  return view.getByText(byContent(/^\{\n {2}"schema": "readmit-runner\/v1"/)).textContent ?? "";
}

/** Saves the configuration the form holds and waits for the window to say
 * where it went. */
async function saveConfiguration(user: UserEvent, view: View, destination: string): Promise<void> {
  await press(user, view.getByRole("button", { name: "Save configuration" }));
  expect(await view.findByText(`Saved to ${destination}.`)).toBeTruthy();
}

test("a runner configuration is refused while the runner would refuse it, previewed, saved once from the keyboard and read unchanged by the command line's runner", async () => {
  const user = userEvent.setup();
  journey.makePrivateFolder("runner/root");
  journey.makePrivateFolder("runner/documents");
  const destination = journey.path("runner/documents/runner.json");
  const form = runnerForm("lab", destination);
  await journey.launch();
  const view = await runnerView(user, "Runner", "Runner");

  // A hub named over plain HTTP is not one the runner connects to: the
  // preview is refused, and nothing is shown or written.
  await fillRunnerForm(user, view, { ...form, hub: "http://hub.example:8443" });
  await press(user, view.getByRole("button", { name: "Preview configuration" }));
  expect(
    await view.findByText(
      "Refused: the runner configuration is not complete; every member is required, the hub must be an HTTPS origin, and both credential references name an absolute program",
    ),
  ).toBeTruthy();
  expect(view.queryByText(byContent(/"schema": "readmit-runner\/v1"/))).toBeNull();

  // Corrected and previewed from the keyboard, the canonical document is
  // exactly what the form holds, with each credential a program and its
  // arguments, and still nothing is written.
  await enter(user, view.getByLabelText("Hub URL"), form.hub);
  await tabTo(user, view.getByRole("button", { name: "Preview configuration" }));
  await user.keyboard("{Enter}");
  await view.findByText(byContent(/^\{\n {2}"schema": "readmit-runner\/v1"/));
  const previewed = shownConfiguration(view);
  expect(JSON.parse(previewed)).toEqual({
    schema: "readmit-runner/v1",
    hub: form.hub,
    project: form.project,
    environment: "lab",
    root: form.root,
    ca: form.ca,
    certificate: form.certificate,
    key: { command: form.key.program, arguments: ["runner-key"] },
    token: { command: form.token.program, arguments: ["runner-token"] },
    update_key: form.updateKey,
    update_engine: APPROVED,
  });
  expect(() => journey.readFile("runner/documents/runner.json")).toThrow();

  // Saving is new operator work, not permitted on a machine whose license is
  // not activated, and said as that state rather than as a refusal; it writes
  // nothing.
  const asked = journey.callsTo("SaveRunnerConfig").length;
  await press(user, view.getByRole("button", { name: "Save configuration" }));
  const unactivated = "operation activation is missing or invalid; select and activate an operation policy";
  const denied = (await view.findByText(unactivated)).closest("p");
  expect(denied?.className).toBe("status status-permission_denied");
  expect(within(denied as HTMLElement).getByText("Permission denied")).toBeTruthy();
  expect(view.queryByText(`Refused: ${unactivated}`)).toBeNull();
  expect(journey.callsTo("SaveRunnerConfig")[asked]?.result).toEqual({ state: "permission_denied", reason: unactivated });
  expect(() => journey.readFile("runner/documents/runner.json")).toThrow();

  // Activated, it is saved from the keyboard, as the previewed bytes.
  await activateLicense(user, journey);
  const save = view.getByRole("button", { name: "Save configuration" });
  await tabTo(user, save);
  await user.keyboard("{Enter}");
  expect(await view.findByText(`Saved to ${destination}.`)).toBeTruthy();
  expect(journey.readFile("runner/documents/runner.json")).toBe(previewed);

  // The command line's runner reads it as its own configuration, and so does
  // the window when it inspects the runner.
  const status = await journey.commandLine(["runner", "status", "--config", destination]);
  expect(status).toMatchObject({ code: 0, stderr: "" });
  expect(JSON.parse(status.stdout)).toEqual({ schema: "readmit-runner-status/v1", state: "idle", jobs: 0 });
  await enter(user, view.getByLabelText("Configuration path"), destination);
  await press(user, view.getByRole("button", { name: "Inspect runner" }));
  expect(await view.findByText(byContent(/^Health: /))).toBeTruthy();
  expect(view.getByText(byContent(/^Health: /)).textContent).toBe("Health: idle, 0 retained job(s).");

  // A second save never replaces the configuration the first one wrote.
  await press(user, view.getByRole("button", { name: "Save configuration" }));
  expect(await view.findByText("Refused: an existing document is never replaced; choose a new destination")).toBeTruthy();
  expect(journey.readFile("runner/documents/runner.json")).toBe(previewed);
});

test("a staged runner update is verified against the pinned deployment key without being run, and an unsigned or a foreign candidate is refused as the command line refuses it", async () => {
  const user = userEvent.setup();
  journey.makePrivateFolder("runner/root");
  journey.makePrivateFolder("runner/documents");
  const destination = journey.path("runner/documents/runner.json");
  // The customer's deployment authority approves the next build; another
  // authority's key signs too, but no configuration here pins it.
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
  const view = await runnerView(user, "Runner", "Runner");
  await fillRunnerForm(user, view, runnerForm("lab", destination, authority.publicKey));
  await saveConfiguration(user, view, destination);

  // Nothing is checked until the configuration, the manifest and the
  // candidate are all named.
  const verify = view.getByRole("button", { name: "Verify staged update" }) as HTMLButtonElement;
  await enter(user, view.getByLabelText("Configuration path"), destination);
  expect(verify.disabled).toBe(true);
  await enter(user, view.getByLabelText("Update manifest"), approved);
  await enter(user, view.getByLabelText("Staged candidate"), candidate);

  // Verified from the keyboard, as the command line verifies it.
  await tabTo(user, verify);
  await user.keyboard("{Enter}");
  expect(
    await view.findByText(
      `Verified: the staged candidate is the approved build ${APPROVED} for this platform, signed by the pinned deployment key. It was not run; installing it is the administrator's action.`,
    ),
  ).toBeTruthy();
  expect(await journey.commandLine(["runner", "verify-update", approved, candidate, "--config", destination])).toEqual({
    code: 0,
    stdout: '{"schema":"readmit-runner-update-check/v1","verified":true}\n',
    stderr: "",
  });

  // Naming another manifest withdraws that answer. A manifest nobody signed,
  // and one signed by an authority the configuration does not pin, are each
  // refused, as the command line refuses them.
  for (const manifest of [unsigned, foreignSigned]) {
    await enter(user, view.getByLabelText("Update manifest"), manifest);
    expect(view.queryByText(/^Verified: /)).toBeNull();
    await press(user, verify);
    expect(await view.findByText(`Refused: the staged candidate is not the approved update; ${RUNNER_REFUSED}`)).toBeTruthy();
    expect(await journey.commandLine(["runner", "verify-update", manifest, candidate, "--config", destination])).toEqual({
      code: 1,
      stdout: "",
      stderr: `readmit: ${RUNNER_REFUSED}\n`,
    });
  }

  // Nothing ran the candidate, and its bytes are the ones staged.
  expect(journey.digest("staged/readmit")).toBe(sha256);
  expect(() => journey.readFile("staged/ran")).toThrow();
});

test("a job document is saved once, preflighted to the prepared inputs and environment it pins and refused under another runner's environment, and a saved schedule policy reopens with its entries and identity", async () => {
  const user = userEvent.setup();
  const { downstream } = await savedAckTest(user, journey, "fixed");
  journey.makePrivateFolder("runner/root");
  journey.makePrivateFolder("runner/documents");
  const spec = journey.path(PROJECT, "reschedule-ack-test.json");
  const job = journey.path("runner/documents/nightly-001.json");
  const view = await runnerView(user, "Runner", "Runner");

  // The runner for the downstream system's environment, and a runner for
  // another environment.
  await fillRunnerForm(user, view, runnerForm(ENVIRONMENT, journey.path("runner/documents/runner.json")));
  await saveConfiguration(user, view, journey.path("runner/documents/runner.json"));
  await enter(user, view.getAllByLabelText("Environment")[0]!, "scheduling-training");
  await enter(user, view.getAllByLabelText("Destination")[0]!, journey.path("runner/documents/training.json"));
  await saveConfiguration(user, view, journey.path("runner/documents/training.json"));

  // A job id the runner would not accept is refused, and nothing is written.
  await enter(user, view.getByLabelText("Job id"), "Nightly 1");
  await enter(user, view.getByLabelText("Spec path"), spec);
  await enter(user, view.getByLabelText("Job document destination"), job);
  await press(user, view.getByRole("button", { name: "Save job document" }));
  expect(
    await view.findByText(
      "Refused: a job id is 1-64 lowercase letters, digits or hyphens starting with a letter or digit, and the spec is one absolute path",
    ),
  ).toBeTruthy();
  expect(() => journey.readFile("runner/documents/nightly-001.json")).toThrow();

  // Corrected, it is saved from the keyboard, once.
  await enter(user, view.getByLabelText("Job id"), "nightly-001");
  await tabTo(user, view.getByRole("button", { name: "Save job document" }));
  await user.keyboard("{Enter}");
  expect(await view.findByText(`Saved to ${job}.`)).toBeTruthy();
  const saved = journey.readFile("runner/documents/nightly-001.json");
  expect(JSON.parse(saved)).toEqual({ schema: "readmit-runner-job/v1", id: "nightly-001", spec });
  await press(user, view.getByRole("button", { name: "Save job document" }));
  expect(await view.findByText("Refused: an existing document is never replaced; choose a new destination")).toBeTruthy();
  expect(journey.readFile("runner/documents/nightly-001.json")).toBe(saved);

  // The preflight, from the keyboard, prepares the spec as execution would,
  // offline: the prepared inputs' identity and the environment they bind.
  await enter(user, view.getByLabelText("Configuration path"), journey.path("runner/documents/runner.json"));
  await enter(user, view.getByLabelText("Job document"), job);
  await tabTo(user, view.getByRole("button", { name: "Preflight job" }));
  await user.keyboard("{Enter}");
  const prepared = (await view.findByText(byContent(/^Prepared inputs /))).textContent ?? "";
  expect(prepared).toMatch(new RegExp(`^Prepared inputs [0-9a-f]{64} bind environment ${ENVIRONMENT}\\.$`));
  const pin = prepared.replace(/^Prepared inputs ([0-9a-f]{64}) .*$/, "$1");

  // Under the other runner the same job is refused, naming both
  // environments, before anything is asked of a hub or sent.
  await enter(user, view.getByLabelText("Configuration path"), journey.path("runner/documents/training.json"));
  await press(user, view.getByRole("button", { name: "Preflight job" }));
  expect(
    await view.findByText(
      `Preflight refused: the prepared inputs bind environment ${ENVIRONMENT}; this runner is configured for scheduling-training`,
    ),
  ).toBeTruthy();
  expect(downstream.received()).toHaveLength(0);

  // A schedule revision for the job, pinned to the prepared identity.
  const schedules = await runnerView(user, "Schedules", "Recurring schedules");
  await press(user, schedules.getByRole("button", { name: "Add schedule" }));
  await enter(user, schedules.getByLabelText("Id"), "nightly-reschedule");
  await enter(user, schedules.getByLabelText("Zone"), "UTC");
  await enter(user, schedules.getByLabelText("At (HH:MM)"), "02:30");
  await enter(user, schedules.getByLabelText("Window seconds"), "600");
  await enter(user, schedules.getByLabelText("Runner configuration"), journey.path("runner/documents/runner.json"));
  await enter(user, schedules.getByLabelText("Spec path"), spec);
  await enter(user, schedules.getByLabelText("Input pin (SHA-256)"), pin);
  await enter(user, schedules.getByLabelText("Notification route (HTTPS origin)"), "https://alerts.journey.test/");
  await user.click(schedules.getByLabelText("Approved for notifications"));
  await enter(user, schedules.getByLabelText("Revision destination"), journey.path("runner/documents/schedules.json"));
  const savedAt = journey.callsTo("SaveSchedulePolicy").length;
  await press(user, schedules.getByRole("button", { name: "Save revision" }));
  await schedules.findByText(byContent(/^Policy identity [0-9a-f]{64} — /));
  expect(journey.callsTo("SaveSchedulePolicy")[savedAt]?.result).toMatchObject({ state: "completed" });
  const identity = (schedules.getByText(byContent(/^Policy identity [0-9a-f]{64} — /)).textContent ?? "").replace(
    /^Policy identity ([0-9a-f]{64}) .*$/,
    "$1",
  );

  // Reopened from the keyboard as the installed policy, it shows the entry
  // it holds, its pin recomputed from the spec, and the identity the hub
  // binds its journal to.
  await enter(user, schedules.getByLabelText("Installed policy (to read)"), journey.path("runner/documents/schedules.json"));
  const opened = journey.callsTo("OpenSchedulePolicy").length;
  await tabTo(user, schedules.getByRole("button", { name: "Open installed policy" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(journey.callsTo("OpenSchedulePolicy")[opened]?.settled).toBe(true));
  expect(journey.callsTo("OpenSchedulePolicy")[opened]?.result).toMatchObject({ state: "completed", identity });
  expect(schedules.getByText(byContent(/^nightly-reschedule at /)).textContent).toBe(
    `nightly-reschedule at 02:30 in UTC, window 600s — pin computed (${pin}).`,
  );
  expect(schedules.getByText(identity)).toBeTruthy();
  expect(schedules.getByText("approved: the hub emits only the fixed alert body shown below")).toBeTruthy();

  // A policy a later release wrote is refused by the reader the hub runs,
  // and nothing of the policy shown before stays in its place.
  journey.writeFile(
    "runner/documents/schedules-v2.json",
    journey.readFile("runner/documents/schedules.json").replace("readmit-hub-schedules/v1", "readmit-hub-schedules/v2"),
  );
  await enter(user, schedules.getByLabelText("Installed policy (to read)"), journey.path("runner/documents/schedules-v2.json"));
  await press(user, schedules.getByRole("button", { name: "Open installed policy" }));
  expect(await schedules.findByText("Refused: the schedule policy could not be read through its own strict reader")).toBeTruthy();
  expect(schedules.queryByText(identity)).toBeNull();
  expect(downstream.received()).toHaveLength(0);
});
