// A suite of a saved test, scheduled on a real hub's managed scheduler
// (#564): the checkout's readmit-hub serving its schedule service over mutual
// TLS on loopback, its store in a disposable PostgreSQL cluster. A person
// builds the suite, adds the runner that will run it and the hub grant for
// it, signs in to the team, and schedules the suite from its own page. The
// schedule is enabled only by the scheduler's acknowledgement, which the
// window shows with its next occurrences; a repeated command is recorded
// once and a different one under the same intent is refused; a change the
// hub did not acknowledge stays Pending — across a restart of the window,
// which sends nothing on its own — until the person sends it again; and
// Delete removes it. At the slot itself the scheduler dispatches nothing for
// a paused schedule, refuses and pauses one whose runner has no grant or
// whose pinned suite changed, and never catches up a slot it missed while it
// was stopped.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { enter, Journey, press, region } from "../testkit/journey";
import type { Hub } from "../testkit/hub.js";
import { goTo, openView, page } from "../testkit/navigation";
import {
  activateLicense,
  configureEnvironment,
  createProject,
  createRecordTest,
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

const HUB_PROJECT = "scheduling";
const SUITE = "Reschedule regression";
const TEST = "Reschedule keeps one appointment";

/** One category of Settings. Settings returns to the page of it last
 * shown, so a subpage is left first. */
async function settings(user: UserEvent, category: string) {
  await goTo(user, "Settings");
  const back = page().queryByRole("button", { name: "Back to settings" });
  if (back) await press(user, back);
  await openView(user, category);
}

/** Settings › Team: the team the hub's operator configured, connected and
 * signed in to as subject through the identity provider. */
async function signInToTeam(user: UserEvent, hub: Hub, subject: string) {
  await settings(user, "General");
  await settings(user, "Team");
  await journey.settled();
  const team = within(await screen.findByRole("region", { name: "Team" }));
  const connect = team.queryByRole("button", { name: "Connect team" });
  if (connect) {
    await press(user, connect);
    const sheet = within(await screen.findByRole("dialog", { name: "Connect team" }));
    await journey.chooseFiles([`${hub.clientConfigFolder}/hub-client.json`], "Choose your team's configuration");
    await press(user, sheet.getByRole("button", { name: "Choose file…" }));
    await sheet.findByText(HUB_PROJECT);
    await press(user, sheet.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Connect team" })).toBeNull());
  }
  await press(user, await within(region("Team")).findByRole("button", { name: "Sign in" }));
  const flow = within(await screen.findByRole("dialog", { name: "Sign in" }));
  const login = await flow.findByRole("link", { name: "Open sign-in page" }, { timeout: 20_000 });
  expect(await hub.signIn(login.getAttribute("href") ?? "", subject)).toBe(200);
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Sign in" })).toBeNull(), { timeout: 20_000 });
}

/** A colleague's own window, licensed, connected to the hub and signed in
 * as subject. */
async function colleagueSignedIn(hub: Hub, name: string, subject: string) {
  const colleague = journey.colleague(name);
  await colleague.call("SelectOperationPolicy", `${journey.provisionLicense(`colleagues/${name}-license`)}/operation-policy.json`);
  await colleague.call("SelectHubConfig", `${hub.clientConfigFolder}/hub-client.json`);
  await colleague.call("ConnectHub");
  const flow = await colleague.call("StartHubAuth");
  expect(await colleague.call("CompleteHubAuth", hub.issueCode(subject), new URL(flow.auth_url ?? "").searchParams.get("state") ?? "")).toMatchObject({ authenticated: true, subject });
  return colleague;
}

/** Settings › Runners › Add runner for another host: the hub, its
 * certificates, the readers of its key and token, the project environment it
 * runs and its hub environment. Export setup writes its configuration where
 * the person names. */
async function addRunner(user: UserEvent, hub: Hub, name: string, hubEnvironment: string, token: string) {
  await settings(user, "Runners");
  const runners = within(await screen.findByRole("region", { name: "Runners" }));
  // Runners returns to the runner last open.
  const back = runners.queryByRole("button", { name: "Back to runners" });
  if (back) await press(user, back);
  await press(user, (await runners.findAllByRole("button", { name: "Add runner" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add runner" }));
  await enter(user, sheet.getByLabelText("Name"), name);
  await enter(user, sheet.getByLabelText("Customer hub"), `https://${hub.address}`);
  await enter(user, sheet.getByLabelText("Hub project"), HUB_PROJECT);
  const choose = async (label: string, path: string, title: string) => {
    await journey.chooseFiles([path], title);
    await pressServed(user, journey, sheet.getByRole("button", { name: `Choose ${label}` }), "ChooseRunnerPath");
    await sheet.findByText(path);
  };
  await choose("hub certificate authority", hub.certificateAuthority, "Choose a CA certificate");
  await choose("runner certificate", hub.clientCertificate, "Choose a client certificate");
  await journey.chooseFiles(["/bin/cat"], "Choose the credential reader");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Choose key reader" }), "ChooseRunnerPath");
  await enter(user, sheet.getByLabelText("Key name"), hub.clientKey);
  await journey.chooseFiles(["/bin/cat"], "Choose the credential reader");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Choose token reader" }), "ChooseRunnerPath");
  await enter(user, sheet.getByLabelText("Token name"), token);
  await enter(user, sheet.getByLabelText("Deployment key"), "A".repeat(43) + "=");
  await enter(user, sheet.getByLabelText("Approved build"), "next-approved-build");
  await press(user, sheet.getByRole("button", { name: "Next" }));
  const retryEnvironments = sheet.queryByRole("button", { name: "Retry environments" });
  if (retryEnvironments) await press(user, retryEnvironments);
  const environment = sheet.getByLabelText("Environment") as HTMLSelectElement;
  await waitFor(() => expect(within(environment).getAllByRole("option").length).toBeGreaterThan(1), { timeout: 10_000 });
  await user.selectOptions(environment, within(environment).getAllByRole("option")[1]!);
  await enter(user, sheet.getByLabelText("Hub environment"), hubEnvironment);
  await press(user, sheet.getByLabelText("Another host"));
  // The runner host is this machine: its private jobs folder exists here.
  await enter(user, sheet.getByLabelText("Working folder"), journey.makePrivateFolder(`runner-hosts/${hubEnvironment}-jobs`));
  await press(user, sheet.getByRole("button", { name: "Next" }));
  await sheet.findByText("Writes the setup for the runner host's administrator; nothing is installed.", undefined, { timeout: 10_000 });
  await journey.nameNewFolder(journey.path(`runner-hosts/${hubEnvironment}.json`), "Export setup");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Export setup" }), "SaveItem");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add runner" })).toBeNull(), { timeout: 20_000 });
}

/** The runner's Access task: the grant for its hub environment, written as a
 * new runner policy the hub's operator installs. */
async function grantRunner(user: UserEvent, policy: string) {
  await press(user, await page().findByRole("button", { name: "More runner actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Access" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Access" }));
  await enter(user, sheet.getByLabelText("Runner subject"), "runner");
  await press(user, sheet.getByRole("button", { name: "Next" }));
  await journey.nameNewFolder(policy, "Save grant");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Save grant" }), "SaveRunnerGrant");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Access" })).toBeNull());
}

/** A licensed project with a saved record test of the person's own export,
 * a suite of it, a runner granted in hub environment "scheduling-qa" and one in
 * "other" that no grant names, and the hub serving its managed scheduler
 * with that grant installed, signed in to as the team's administrator.
 * Returns the project folder. */
async function scheduledSuite(user: UserEvent, hub: Hub, onReceiver?: (receiver: Awaited<ReturnType<Journey["startDownstream"]>>) => void): Promise<string> {
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", "duplicating", ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule duplicates");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  if (onReceiver) {
    onReceiver(downstream);
    await goTo(user, "Environments");
    if (!page().queryByRole("heading", { level: 1, name: "Environments" })) await goTo(user, "Environments");
    await press(user, await page().findByText(environment));
    await page().findByRole("region", { name: "Connection" });
    await press(user, within(page().getByRole("region", { name: "Connection" })).getByRole("button", { name: "Edit" }));
    const connection = within(await screen.findByRole("dialog", { name: "Edit connection" }));
    await press(user, connection.getByRole("button", { name: "More connection settings" }));
    await enter(user, connection.getByLabelText("Message timeout"), "60s");
    await pressServed(user, journey, connection.getByRole("button", { name: "Save" }), "SaveItem");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit connection" })).toBeNull());
  }

  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule duplicates"));
  await openedCase();
  await createRecordTest(user, journey, environment, TEST);

  // Tests › Suites › New suite of the saved test.
  await goTo(user, "Tests");
  await press(user, await page().findByRole("tab", { name: "Suites" }));
  // New suite offers the saved tests once the page has read them.
  await page().findByText("No suites yet", undefined, { timeout: 10_000 });
  await journey.settled();
  await press(user, (await page().findAllByRole("button", { name: "New suite" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New suite" }));
  await enter(user, sheet.getByLabelText("Name"), SUITE);
  await press(user, await sheet.findByRole("checkbox", { name: TEST }));
  await pressServed(user, journey, sheet.getByRole("button", { name: "Create" }), "SaveItem");
  expect(journey.callsTo("SaveItem").at(-1)?.result).toMatchObject({ state: "completed", outcome: "saved" });
  // Created, the suite opens on its own page.
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New suite" })).toBeNull(), { timeout: 20_000 });
  await page().findByRole("heading", { level: 1, name: new RegExp(`^${SUITE}`) }, { timeout: 20_000 });

  // Two runners, and the hub grant for the first alone.
  journey.makePrivateFolder("runner-hosts");
  const token = hub.runnerToken("runner");
  await addRunner(user, hub, "Lab runner", "scheduling-qa", token);
  const policy = journey.path("runner-hosts/runners.json");
  await grantRunner(user, policy);
  await addRunner(user, hub, "Other runner", "other", token);

  // The hub's operator serves the managed scheduler with that grant.
  await hub.restart(["-schedules", "-runner-policy", policy]);
  await signInToTeam(user, hub, "analyst");
  return project;
}

/** Tests › Suites › the suite › Schedule: the suite's own schedules. */
async function suiteSchedules(user: UserEvent) {
  // Tests returns to the suite it was left on.
  await goTo(user, "Tests");
  await page().findByRole("heading", { level: 1, name: new RegExp(`^${SUITE}`) }, { timeout: 10_000 });
  await press(user, await page().findByRole("button", { name: "Schedule" }));
  await page().findByRole("heading", { level: 1, name: "Schedules" });
  await journey.settled();
}

/** Runs › Schedules, read again from the hub. */
async function schedules(user: UserEvent) {
  await goTo(user, "Cases");
  await goTo(user, "Runs");
  if (!page().queryByRole("heading", { level: 1, name: "Runs" })) await goTo(user, "Runs");
  await press(user, await page().findByRole("button", { name: "Schedules" }));
  await page().findByRole("heading", { level: 1, name: "Schedules" });
  await journey.settled();
}

/** New schedule, as far as its review, for runner at at in UTC. */
async function reviewSchedule(user: UserEvent, name: string, runner: string, at: string, window = 30) {
  await press(user, (await page().findAllByRole("button", { name: "New schedule" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "New schedule" }));
  await waitFor(() => expect((sheet.getByLabelText("Suite") as HTMLSelectElement).value).not.toBe(""));
  await waitFor(() => expect((sheet.getByLabelText("Environment") as HTMLSelectElement).value).not.toBe(""));
  await enter(user, sheet.getByLabelText("Name"), name);
  await user.selectOptions(sheet.getByLabelText("Runner"), runner);
  await user.selectOptions(sheet.getByLabelText("Time zone"), "UTC");
  await enter(user, sheet.getByLabelText("Time"), at);
  await enter(user, sheet.getByLabelText("Run window (minutes)"), String(window));
  await pressServed(user, journey, sheet.getByRole("button", { name: "Review" }), "PrepareSchedule");
  await sheet.findByLabelText("Schedule", { selector: "dl" }, { timeout: 30_000 });
  return sheet;
}

/** Enables a reviewed schedule, as the scheduler acknowledges it. */
async function enable(user: UserEvent, sheet: ReturnType<typeof within>) {
  await pressServed(user, journey, sheet.getByRole("button", { name: "Enable schedule" }), "CommandSchedule");
  expect(journey.callsTo("CommandSchedule").at(-1)?.result).toMatchObject({ state: "completed", status: "enabled" });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "New schedule" })).toBeNull());
  return (journey.callsTo("CommandSchedule").at(-1)?.result as { schedule: string }).schedule;
}

/** One value of a read-only list, by its label. */
function value(rows: HTMLElement, label: string): string | null | undefined {
  return within(rows).getByText(label, { selector: "dt" }).nextElementSibling?.textContent;
}

/** One schedule's detail, opened from its row. */
async function detail(user: UserEvent, name: string) {
  await press(user, await within(await page().findByRole("table", { name: "Schedules" })).findByRole("row", { name }));
  return within(await screen.findByRole("dialog", { name }));
}

/** Pause, Enable or Delete from a schedule's detail, confirmed. */
async function confirm(user: UserEvent, name: string, verb: "Pause" | "Enable" | "Delete") {
  const shown = await detail(user, name);
  await press(user, shown.getByRole("button", { name: verb }));
  const sheet = within(await screen.findByRole("dialog", { name: `${verb} schedule` }));
  await pressServed(user, journey, sheet.getByRole("button", { name: `${verb} ${name}` }), "CommandSchedule");
  return { sheet, result: journey.callsTo("CommandSchedule").at(-1)?.result as { state: string; reason?: string; revision?: number; status?: string; replayed?: boolean } };
}

/** A schedule's row cells: suite, time, environment, state, next run. */
async function row(name: string): Promise<string[]> {
  const found = await within(await page().findByRole("table", { name: "Schedules" })).findByRole("row", { name });
  return Array.from(found.querySelectorAll("td,th")).map((cell) => cell.textContent ?? "");
}

test(
  "a suite's schedule is enabled by the hub scheduler's acknowledgement, a repeated command is recorded once, an unacknowledged change stays Pending across a window restart until it is sent again, and Delete removes it",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(HUB_PROJECT, [
      { subject: "analyst", role: "admin" },
      { subject: "reviewer", role: "reviewer" },
    ]);
    const project = await scheduledSuite(user, hub);

    // Created from the suite's own page: the review names the exact version,
    // the pinned test, the runner and the next three occurrences, and only
    // the scheduler's acknowledgement enables it.
    await suiteSchedules(user);
    const review = await reviewSchedule(user, "Nightly regression", "Lab runner", "03:17");
    const reviewed = review.getByLabelText("Schedule", { selector: "dl" });
    expect(value(reviewed, "Suite")).toMatch(new RegExp(`^${SUITE} · Version `));
    expect(value(reviewed, "Tests")).toMatch(new RegExp(`^${TEST} · Version `));
    expect(value(reviewed, "Runner")).toBe("Lab runner");
    expect(value(reviewed, "Time")).toBe("03:17 UTC");
    expect(value(reviewed, "Next runs")!.split("; ")).toHaveLength(3);
    expect(journey.callsTo("CommandSchedule")).toHaveLength(0);
    const id = await enable(user, review);
    expect(await row("Nightly regression")).toEqual([SUITE, "03:17 UTC · Daily", "Scheduling QA", "Enabled", expect.stringMatching(/03:17/)]);

    // The same command under the same intent, sent again from a colleague's
    // window on the same project, is answered from the first acknowledgement
    // and recorded once; the same intent with another command is refused; a
    // role that may not change schedules is refused by the hub.
    const admin = await colleagueSignedIn(hub, "admin-window", "analyst");
    const listed = await admin.call("ListSchedules", { context: { project, generation: 0 } });
    const revision = listed.schedules.find((entry) => entry.id === id)!.revision;
    const intent = crypto.randomUUID();
    const pause = { context: { project, generation: 0 }, intent, kind: "pause", schedule: id, expected_revision: revision, enable: false };
    expect(await admin.call("CommandSchedule", pause)).toMatchObject({ state: "completed", status: "paused", revision: revision + 1, replayed: false });
    expect(await admin.call("CommandSchedule", pause)).toMatchObject({ state: "completed", status: "paused", revision: revision + 1, replayed: true });
    expect(await admin.call("CommandSchedule", { ...pause, kind: "enable", expected_revision: revision + 1 })).toMatchObject({ state: "failed" });
    expect((await admin.call("ListSchedules", { context: { project, generation: 0 } })).schedules.find((entry) => entry.id === id)).toMatchObject({ state: "paused", revision: revision + 1 });
    const reviewer = await colleagueSignedIn(hub, "reviewer-window", "reviewer");
    expect(await reviewer.call("CommandSchedule", { ...pause, intent: crypto.randomUUID(), kind: "enable", expected_revision: revision + 1 })).toMatchObject({
      state: "permission_denied",
      reason: "Your hub role does not allow this.",
    });
    await schedules(user);
    expect((await row("Nightly regression"))[3]).toBe("Paused");

    // Enabled again from the window: a double click sends one command.
    const before = journey.callsTo("CommandSchedule").length;
    const shown = await detail(user, "Nightly regression");
    await press(user, shown.getByRole("button", { name: "Enable" }));
    const enabling = within(await screen.findByRole("dialog", { name: "Enable schedule" }));
    await user.dblClick(enabling.getByRole("button", { name: "Enable Nightly regression" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Enable schedule" })).toBeNull());
    expect(journey.callsTo("CommandSchedule").slice(before)).toHaveLength(1);
    await waitFor(async () => expect((await row("Nightly regression"))[3]).toBe("Enabled"));

    // The hub stops: Pause is not acknowledged, so it stays pending and is
    // never shown as done.
    await hub.operate(["check"]);
    const unanswered = await confirm(user, "Nightly regression", "Pause");
    expect(unanswered.result).toMatchObject({ state: "failed", pending: true });
    expect(await unanswered.sheet.findByText("The hub did not acknowledge the change. It stays pending until it is sent again.")).toBeTruthy();
    await press(user, unanswered.sheet.getByRole("button", { name: "Cancel" }));
    const pausing = (journey.callsTo("CommandSchedule").at(-1)?.args[0] as { intent: string }).intent;

    // The window restarts while the hub is back: it shows what the project
    // recorded and what the hub holds, Pending, and sends nothing on its own.
    await journey.close();
    const sent = journey.callsTo("CommandSchedule").length;
    await hub.restart(["-schedules", "-runner-policy", journey.path("runner-hosts/runners.json")]);
    await journey.launch();
    await goTo(user, "Projects");
    await journey.settled();
    await press(user, await within(await page().findByRole("table", { name: "Projects" })).findByRole("row", { name: "Scheduling interface" }));
    await page().findByRole("heading", { level: 1, name: "Cases" }, { timeout: 10_000 });
    await signInToTeam(user, hub, "analyst");
    await schedules(user);
    expect((await row("Nightly regression"))[3]).toBe("Pending");
    expect(journey.callsTo("CommandSchedule")).toHaveLength(sent);
    let pending = await detail(user, "Nightly regression");
    expect(value(pending.getByLabelText("Schedule", { selector: "dl" }), "Pending")).toBe("The hub did not acknowledge this change");
    expect((await admin.call("ListSchedules", { context: { project, generation: 0 } })).schedules.find((entry) => entry.id === id)).toMatchObject({ state: "enabled" });

    // Sent again, the same change under its own intent is acknowledged once.
    await press(user, pending.getByRole("button", { name: "Send again" }));
    await waitFor(() => expect(journey.callsTo("CommandSchedule").at(-1)?.settled).toBe(true));
    expect(journey.callsTo("CommandSchedule").at(-1)?.result).toMatchObject({ state: "completed", status: "paused" });
    await waitFor(async () => expect((await row("Nightly regression"))[3]).toBe("Paused"));
    expect(await admin.call("CommandSchedule", { context: { project, generation: 0 }, intent: pausing, kind: "resend", schedule: id, expected_revision: 0, enable: false })).toMatchObject({
      state: "failed",
      reason: "This schedule has no change waiting to be sent.",
    });
    pending = await detail(user, "Nightly regression");
    expect(pending.queryByRole("button", { name: "Send again" })).toBeNull();
    await press(user, pending.getByRole("button", { name: `Close ${"Nightly regression".toLowerCase()}` }));

    // Delete removes it, at the scheduler and in the window.
    const deleted = await confirm(user, "Nightly regression", "Delete");
    expect(deleted.result).toMatchObject({ state: "completed", status: "deleted" });
    expect(await page().findByText("No schedules")).toBeTruthy();
    expect((await admin.call("ListSchedules", { context: { project, generation: 0 } })).schedules).toEqual([]);
  },
  900_000,
);

/** The UTC clock time a whole number of minutes after the next minute. */
function utcMinute(from: Date, minutesAhead: number): { at: string; due: Date } {
  const due = new Date(from.getTime());
  due.setUTCSeconds(0, 0);
  due.setUTCMinutes(due.getUTCMinutes() + 1 + minutesAhead);
  return { at: `${String(due.getUTCHours()).padStart(2, "0")}:${String(due.getUTCMinutes()).padStart(2, "0")}`, due };
}

/** Waits until an instant has passed, by this much more. */
async function until(instant: Date, after: number) {
  await new Promise((resolve) => setTimeout(resolve, Math.max(0, instant.getTime() - Date.now()) + after));
}

test(
  "at its slot the hub scheduler dispatches nothing for a paused schedule, refuses and pauses one whose runner has no grant or whose pinned suite changed, and never catches up a slot it missed while stopped",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(HUB_PROJECT, [{ subject: "analyst", role: "admin" }]);
    const project = await scheduledSuite(user, hub);
    await suiteSchedules(user);

    // Four schedules of the suite at the next slots: one paused once the
    // scheduler acknowledged it, one whose runner no grant names, one whose
    // pinned suite will change, and one due while the hub is stopped.
    const first = utcMinute(new Date(), 3);
    const second = utcMinute(first.due, 1);
    await enable(user, await reviewSchedule(user, "Paused nightly", "Lab runner", first.at, 5));
    await enable(user, await reviewSchedule(user, "Ungranted nightly", "Other runner", first.at, 5));
    await enable(user, await reviewSchedule(user, "Changed nightly", "Lab runner", first.at, 5));
    await enable(user, await reviewSchedule(user, "Missed nightly", "Lab runner", second.at, 1));
    expect(Date.now()).toBeLessThan(first.due.getTime() - 20_000);
    const paused = await confirm(user, "Paused nightly", "Pause");
    expect(paused.result).toMatchObject({ state: "completed", status: "paused" });

    // What the scheduled suite runs changes on disk after it was pinned.
    const scheduled = journey.path(project.slice(journey.path().length + 1), ".readmit", "scheduled");
    const folders = readdirSync(scheduled);
    expect(folders).toHaveLength(1);
    const queuePath = `${project.slice(journey.path().length + 1)}/.readmit/scheduled/${folders[0]}/queue.json`;
    const queue = JSON.parse(journey.readFile(queuePath)) as { jobs: { id: string; after?: string[] }[] };
    const renamed = `${queue.jobs[0]!.id}-changed`;
    queue.jobs = queue.jobs.map((job, index) => (index === 0 ? { ...job, id: renamed } : job));
    journey.changeFile(queuePath, JSON.stringify(queue));

    // The first slot passes with the scheduler running.
    await until(first.due, 8_000);
    await schedules(user);
    const read = async (name: string) => {
      const shown = await detail(user, name);
      const values = shown.getByLabelText("Schedule", { selector: "dl" });
      const recent = shown.queryByRole("table", { name: "Recent runs" });
      const results = recent ? Array.from(recent.querySelectorAll("tbody tr")).map((entry) => entry.lastElementChild?.textContent) : [];
      const state = { state: value(values, "State"), reason: within(values).queryByText("Reason", { selector: "dt" })?.nextElementSibling?.textContent ?? null, results };
      await press(user, shown.getByRole("button", { name: `Close ${name.toLowerCase()}` }));
      return state;
    };
    expect(await read("Paused nightly")).toEqual({ state: "Paused", reason: null, results: [] });
    expect(await read("Ungranted nightly")).toEqual({ state: "Paused", reason: "The hub's runner authority was not available.", results: ["Refused"] });
    expect(await read("Changed nightly")).toEqual({ state: "Paused", reason: "What it runs changed after it was scheduled. Edit it to review the new version.", results: ["Refused"] });

    // The hub is stopped over the second slot and past its window; started
    // again, it records the slot missed and dispatches nothing for it.
    await hub.operate(["check"]);
    await until(second.due, 75_000);
    await hub.restart(["-schedules", "-runner-policy", journey.path("runner-hosts/runners.json")]);
    // The restarted hub holds its runner leases for ten seconds before its
    // scheduler ticks.
    await new Promise((resolve) => setTimeout(resolve, 15_000));
    // The identity provider's five-minute session has ended by now: the
    // person signs in again to read the schedules.
    await signInToTeam(user, hub, "analyst");
    await schedules(user);
    expect(await read("Missed nightly")).toEqual({ state: "Enabled", reason: null, results: ["Missed"] });
    expect(journey.callsTo("CommandSchedule").filter((call) => (call.args[0] as { kind: string }).kind !== "create" && (call.args[0] as { kind: string }).kind !== "pause")).toEqual([]);
  },
  900_000,
);


test("Pause acknowledged during a real active scheduled send lets that run finish and prevents the queued occurrence from dispatching", async context => {
  if (!Journey.hubAvailable) context.skip();
  const user = userEvent.setup();
  const hub = await journey.startHub(HUB_PROJECT, [{ subject: "analyst", role: "admin" }]);
  let receiver!: Awaited<ReturnType<Journey["startDownstream"]>>;
  await scheduledSuite(user, hub, peer => { receiver = peer; });
  await suiteSchedules(user);
  const slot = utcMinute(new Date(), 1);
  await enable(user, await reviewSchedule(user, "First held run", "Lab runner", slot.at, 5));
  await enable(user, await reviewSchedule(user, "Second queued run", "Lab runner", slot.at, 5));
  receiver.holdAcknowledgements();
  expect(receiver.received()).toHaveLength(0);
  await waitFor(() => expect(receiver.received()).toHaveLength(1), { timeout: 150_000, interval: 100 }).catch(async error => {
    await schedules(user);
    const observed = journey.callsTo("ListSchedules").at(-1)?.result;
    const files = readdirSync(journey.path("runner-hosts/scheduling-qa-jobs"), { recursive: true, withFileTypes: true }).filter(entry => entry.isFile() && /(?:state|result|job)\.json$/.test(entry.name)).map(entry => ({ name: entry.name, text: readFileSync(join(entry.parentPath, entry.name), "utf8") }));
    throw new Error(`${String(error)}; scheduler: ${JSON.stringify(observed)}; jobs: ${JSON.stringify(files)}`);
  });
  for (const name of ["First held run", "Second queued run"]) {
    await schedules(user);
    expect((await confirm(user, name, "Pause")).result).toMatchObject({ state: "completed", status: "paused" });
  }
  // Receiving a payload, while its ACK is held, independently proves an
  // active send. A claimed schedule alone is not evidence that it started.
  expect(receiver.received()).toHaveLength(1);
  receiver.releaseAcknowledgement();
  await waitFor(() => expect(receiver.received()).toHaveLength(2), { timeout: 30_000 });
  const states = async () => {
    await schedules(user);
    const outcomes: string[] = [];
    for (const name of ["First held run", "Second queued run"]) {
      const shown = await detail(user, name);
      expect(value(shown.getByLabelText("Schedule", { selector: "dl" }), "State")).toBe("Paused");
      const recent = shown.queryByRole("table", { name: "Recent runs" });
      if (recent) outcomes.push(...Array.from(recent.querySelectorAll("tbody tr")).map(row => row.lastElementChild?.textContent ?? ""));
      await press(user, shown.getByRole("button", { name: `Close ${name.toLowerCase()}` }));
    }
    return outcomes.sort();
  };
  await waitFor(async () => expect(await states()).toEqual(["Cancelled", "Failed"]), { timeout: 30_000, interval: 1000 });
  expect(receiver.received()).toHaveLength(2);
}, 360_000);


test("an expired installed hub license prevents an enabled schedule from dispatching and keeps its history readable", async context => {
  if (!Journey.hubAvailable) context.skip();
  const user = userEvent.setup();
  const [active] = journey.provisionLicenseIssues([
    { folder: "hub-license-active", sequence: 1, expires: "8760h", graceDays: 0 },
    { folder: "hub-license-expired", sequence: 2, expires: "-48h", graceDays: 0 },
  ]);
  const hub = await journey.startHub(HUB_PROJECT, [{ subject: "analyst", role: "admin" }], "team", `${active}/operation-policy.json`);
  let receiver!: Awaited<ReturnType<Journey["startDownstream"]>>;
  await scheduledSuite(user, hub, peer => { receiver = peer; });
  await suiteSchedules(user);
  const slot = utcMinute(new Date(), 1);
  await enable(user, await reviewSchedule(user, "License-bound nightly", "Lab runner", slot.at, 5));
  expect(receiver.received()).toHaveLength(0);
  // A valid later vendor issue under the same trust replaces the installed
  // entitlement. The real guard must re-read its expired term at dispatch.
  journey.changeFile("hub-license-active/entitlement.json", journey.readFile("hub-license-expired/entitlement.json"));
  await until(slot.due, 5000);
  await schedules(user);
  const shown = await detail(user, "License-bound nightly");
  const recent = await shown.findByRole("table", { name: "Recent runs" });
  expect(within(recent).getByText("Error")).toBeTruthy();
  expect(receiver.received()).toHaveLength(0);
  expect(readdirSync(journey.path("runner-hosts/scheduling-qa-jobs"))).toEqual([]);
}, 300_000);
