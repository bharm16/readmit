// Licensing and the commercial portal as a person meets them offline. A
// license the vendor delivered is verified from its two received documents,
// installed into a new private folder for the author and device it names and
// activated, all without hand-written configuration; the same issue offered
// again as a renewal is refused, and the installed document exports byte for
// byte. Released, the activation admits no new work — in the window or on the
// command line — while reading, backing up and exporting what exists goes on,
// and it cannot be activated again. The commercial portal is a destination an
// operator supplies: until then its absence is stated, an invalid file is
// refused, and once selected it is shown as a link the window never requests,
// kept across a restart. None of this contacts anything.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import { goToView, sidebar } from "../testkit/navigation";
import { createProject, pressServed, submitProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The license and activation pane of the privacy region. */
function access() {
  return within(region("License"));
}

/** The pane's status lines, once the window shows the one a pattern matches. */
async function status(pattern: RegExp): Promise<string> {
  return (await access().findByText(byContent(pattern))).textContent ?? "";
}

/** Opens the license pane in Settings. */
async function openLicensing(user: UserEvent) {
  await goToView(user, "Settings", "License");
}

/** Opens the open project's settings sheet from its switcher. */
async function projectSettings(user: UserEvent) {
  await journey.settled();
  await press(user, await sidebar().findByRole("button", { name: /^Project: / }));
  await press(user, screen.getByRole("menuitem", { name: "Project settings" }));
  return within(await screen.findByRole("dialog", { name: "Project settings" }));
}

test("a delivered license is verified, installed and activated without hand-written configuration, and once released admits no new work", async () => {
  const user = userEvent.setup();
  // The vendor's delivery: the signed entitlement and the trust document it
  // is verified against. The folder the fixture writes also holds an
  // operation policy of its own, which this person never selects.
  journey.provisionLicense("vendor-delivery");
  journey.makeFolder("license-activation");
  journey.makeFolder("investigations");
  await journey.launch();
  await openLicensing(user);

  // Verified from the two received documents, with every signed fact shown.
  await journey.chooseFiles([journey.path("vendor-delivery/entitlement.json")], "Choose the received entitlement document");
  await journey.chooseFiles([journey.path("vendor-delivery/trust.json")], "Choose the vendor trust document");
  // The supplied-folder workflow lives in the Administrator setup subview.
  await press(user, access().getByText("Administrator setup"));
  await press(user, access().getByRole("button", { name: "Verify license…" }));
  const received = within(await access().findByRole("heading", { name: "Received license" }).then((heading) => heading.parentElement!));
  const facts = received.getAllByRole("definition").map((item) => item.textContent ?? "");
  expect(facts[0]).toBe("test-entitlement (current format)");
  expect(facts[1]).toBe("test-organization");
  expect(facts[3]).toMatch(/^\S+ to \S+; grace 0 days \(ends \S+\); state active$/);
  expect(facts.slice(4)).toEqual(["1 author seats, 2 devices each; 16 runner instances", "author, execute, hub", "test-key (active)"]);

  // Installed for the author and device it names, into a new private folder,
  // and activated explicitly.
  await user.selectOptions(received.getByLabelText("Author"), "test-author");
  await user.selectOptions(received.getByLabelText("Device"), "test-device");
  await journey.chooseFolder(journey.path("license-activation"), "Choose the private folder for the local license activation");
  await press(user, received.getByRole("button", { name: "Choose activation folder…" }));
  expect(await received.findByText(byContent(/^Activation folder: /))).toBeTruthy();
  await press(user, received.getByRole("button", { name: "Create activation folder" }));
  expect(await access().findByText("the local activation is created; activate it to admit licensed work")).toBeTruthy();
  await press(user, access().getByRole("button", { name: "Activate" }));
  expect(await status(/^License: /)).toMatch(
    /^License: active\. Organization: test-organization\. Named authors: 1; runner instances: 16\. Expires: \S+\. Grace ends: \S+\.$/,
  );

  // Licensed work is admitted: a new project, and a change to its settings
  // that renames it and declares an interface revision, which the command
  // line reads back.
  const project = await createProject(user, journey, "investigations", "licensed-work", "Licensed work");
  const settings = await projectSettings(user);
  await enter(user, settings.getByLabelText("Name", { selector: "#project-name" }), "Licensed handover");
  await press(user, settings.getByRole("button", { name: "Add revision" }));
  await enter(user, settings.getByLabelText("Revision name", { selector: "#revision-0" }), "siu-2.5.1-v2");
  await pressServed(user, journey, settings.getByRole("button", { name: "Save" }), "SaveItem");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Project settings" })).toBeNull());
  expect(await sidebar().findByRole("button", { name: "Project: Licensed handover" })).toBeTruthy();
  const settled = await journey.commandLine(["project", "show", project]);
  expect(settled.stdout).toMatch(/^Project: Licensed handover\n/);
  const revisions = await projectSettings(user);
  expect((revisions.getByLabelText("Revision name", { selector: "#revision-0" }) as HTMLInputElement).value).toBe("siu-2.5.1-v2");
  await press(user, revisions.getByRole("button", { name: "Cancel" }));

  // The same issue offered as a renewal is refused, and the installed
  // document exports byte for byte as it was received.
  await openLicensing(user);
  await journey.chooseFiles([journey.path("vendor-delivery/entitlement.json")], "Choose the later-issue entitlement document");
  await press(user, access().getByRole("button", { name: "Renew activation…" }));
  expect(await access().findByText("installed entitlement is already at this issue sequence or a later one")).toBeTruthy();
  journey.makeFolder("exported");
  await journey.chooseFolder(journey.path("exported"), "Choose the folder to export the entitlement into");
  await press(user, access().getByRole("button", { name: "Export license…" }));
  expect(await status(/^Document test-entitlement written to /)).toBe(
    `Document test-entitlement written to ${journey.path("exported", "test-entitlement.json")}, byte for byte as it was received.`,
  );
  expect(journey.readFile("exported/test-entitlement.json")).toBe(journey.readFile("vendor-delivery/entitlement.json"));

  // Released: the window refuses new work, and so does the command line over
  // the same activation, with the same reason.
  await press(user, access().getByRole("button", { name: "Refresh activation" }));
  await press(user, access().getByRole("button", { name: "Release activation" }));
  expect(await status(/This activation is released\.$/)).toMatch(/No unresolved clock rollback\. This activation is released\.$/);
  const renaming = await projectSettings(user);
  await enter(user, renaming.getByLabelText("Name", { selector: "#project-name" }), "Licensed work, renamed");
  await pressServed(user, journey, renaming.getByRole("button", { name: "Save" }), "SaveItem");
  expect(await renaming.findByText("this device released its entitlement activation")).toBeTruthy();
  // The refused sheet keeps the name typed until it is thrown away.
  expect((renaming.getByLabelText("Name", { selector: "#project-name" }) as HTMLInputElement).value).toBe("Licensed work, renamed");
  await press(user, renaming.getByRole("button", { name: "Cancel" }));
  await press(user, within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  const refused = await journey.commandLine([
    "--operation-policy",
    journey.path("license-activation", "operation-policy.json"),
    "project",
    "settings",
    project,
    "--title",
    "Renamed by hand",
  ]);
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toContain("this device released its entitlement activation");
  expect((await journey.commandLine(["project", "show", project])).stdout).toMatch(/^Project: Licensed handover\n/);
  // The refusal leaves the project, and its maintenance, on the screen.
  expect(sidebar().getByRole("button", { name: "Project: Licensed handover" })).toBeTruthy();
  const evidence = within(region("Evidence"));

  // What exists stays readable and can still be backed up, offline.
  journey.makeFolder("backups");
  await press(user, evidence.getByRole("button", { name: "Maintenance" }));
  const maintenance = within(screen.getByLabelText("Project maintenance"));
  await journey.nameNewFolder(journey.path("backups/after-release"), "New backup folder");
  await press(user, maintenance.getByRole("button", { name: "Choose destination…" }));
  await press(user, maintenance.getByRole("button", { name: "Create backup" }));
  expect(await maintenance.findByText("Backup created.", { selector: "p[role=status]" })).toBeTruthy();
  await press(user, maintenance.getByRole("button", { name: "Close maintenance" }));

  // A released activation is not activated again: the new term needs a new
  // activation folder.
  await press(user, access().getByRole("button", { name: "Activate" }));
  expect(await access().findByText("operation activation is missing or invalid; select and activate an operation policy")).toBeTruthy();
});

test("the commercial portal is an operator-supplied destination: stated as missing, refused when invalid, and once chosen shown as a link that survives a restart", async () => {
  const user = userEvent.setup();
  journey.writeFile(
    "operator/destinations-insecure.json",
    '{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"http://portal.example.test/account"}\n',
  );
  journey.writeFile(
    "operator/destinations.json",
    '{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://portal.example.test/account"}\n',
  );
  await journey.launch();
  await openLicensing(user);
  const commercial = () => within(access().getByRole("heading", { name: "Account" }).parentElement!);
  // The account portal is configured in the Administrator setup subview.
  await press(user, access().getByText("Administrator setup"));
  const portalRow = () => {
    const table = screen.getByRole("table", { name: "Deliberately configured activities and their destinations" });
    const row = within(table).getAllByRole("row").find((candidate) => /commercial|portal/i.test(within(candidate).queryAllByRole("rowheader")[0]?.textContent ?? ""));
    return within(row!);
  };
  expect(await commercial().findByText("the commercial portal destination is not configured; choose the operator-supplied destinations file")).toBeTruthy();
  expect(portalRow().getAllByRole("cell")[3]?.textContent).toBe(
    "Not configuredNo commercial destinations file is selected, so no portal address is shown anywhere.",
  );

  // A destination that is not an https address is refused, and nothing is
  // shown as a portal.
  await journey.chooseFiles([journey.path("operator/destinations-insecure.json")], "Choose the commercial destinations file");
  await press(user, access().getByRole("button", { name: "Configure account portal…" }));
  expect(
    await commercial().findByText("the destinations file cannot be read here; choose a valid commercial destinations file again"),
  ).toBeTruthy();
  expect(commercial().queryByRole("link", { name: "Manage account" })).toBeNull();

  // The operator's file: the destination and its environment, as a link the
  // window itself never requests.
  await journey.chooseFiles([journey.path("operator/destinations.json")], "Choose the commercial destinations file");
  await press(user, access().getByRole("button", { name: "Configure account portal…" }));
  const destination = "Environment: sandbox. Destination: https://portal.example.test/account";
  expect((await commercial().findByText(byContent(/^Environment: /))).textContent).toBe(destination);
  const link = commercial().getByRole("link", { name: "Manage account" });
  expect(link.getAttribute("href")).toBe("https://portal.example.test/account");
  expect(link.getAttribute("target")).toBe("_blank");
  await press(user, screen.getByRole("button", { name: "Refresh privacy status" }));
  await waitFor(() =>
    expect(portalRow().getAllByRole("cell")[3]?.textContent).toBe(
      "Configured (browser only)A portal destination is configured. This window makes no request to it; opening it is a separate deliberate act in your browser.",
    ),
  );

  // Kept across a restart: the selection is read back, still without any
  // request to the portal.
  await journey.close();
  await journey.launch();
  await openLicensing(user);
  expect((await commercial().findByText(byContent(/^Environment: /))).textContent).toBe(destination);
});

/** Selects one supplied activation folder in the license pane and returns
 * the status the pane then reads. */
async function selectActivation(user: UserEvent, folder: string): Promise<string> {
  // A project just opened finishes its reads before the folder is chosen.
  await journey.settled();
  await journey.chooseFolder(journey.path(folder), "Choose the license activation folder");
  await press(user, access().getByRole("button", { name: "Choose activation folder…" }));
  await press(user, access().getByRole("button", { name: "Refresh activation" }));
  return status(/^License: \w+\. Organization: test-organization\./);
}

/** Submits a new project from the New project sheet, in a parent folder
 * chosen with Change, and returns the facade's answer. A refused create
 * stays in the sheet with its reason, which is read and then thrown away. */
async function tryProject(user: UserEvent, parent: string, name: string, title: string) {
  journey.makeFolder(parent);
  const answer = await submitProject(user, journey, parent, name, title, true);
  if (answer.state !== "completed") {
    const sheet = within(screen.getByRole("dialog", { name: "New project" }));
    expect(await sheet.findByText(answer.reason!)).toBeTruthy();
    await press(user, sheet.getByRole("button", { name: "Cancel" }));
    await press(user, within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  }
  return answer;
}

test("an expired term refuses new work until the later issue the vendor signs is installed, and a term in its grace period still admits it", async () => {
  const user = userEvent.setup();
  // What the vendor signed over time, under one key: a term whose grace
  // period has ended, its renewal, and a term that expired yesterday with a
  // week's grace, delivered as a third activation folder.
  journey.provisionLicenseIssues([
    { folder: "vendor-expired", sequence: 1, expires: "-48h", graceDays: 1 },
    { folder: "vendor-renewal", sequence: 2, expires: "720h", graceDays: 14 },
    { folder: "vendor-grace", sequence: 1, expires: "-24h", graceDays: 7 },
  ]);
  await journey.launch();
  await openLicensing(user);

  // Expired: the window says so, and new work is refused with the reason,
  // in the window and on the command line alike; nothing is created.
  // The supplied-folder workflow lives in the Administrator setup subview.
  await press(user, access().getByText("Administrator setup"));
  expect(await selectActivation(user, "vendor-expired")).toMatch(/^License: expired\. /);
  const reason = "entitlement expired and its grace period has ended";
  expect(await tryProject(user, "investigations", "expired-work", "Expired work")).toMatchObject({ state: "permission_denied", reason });
  const byHand = await journey.commandLine([
    "--operation-policy",
    journey.path("vendor-expired", "operation-policy.json"),
    "project",
    "init",
    "--output",
    "investigations/by-hand",
    "--title",
    "By hand",
    "--interface-version",
    "siu-2.5.1-v1",
  ]);
  expect(byHand.code).not.toBe(0);
  expect(byHand.stderr).toContain(reason);
  expect(journey.callsTo("CreateNamedProject").every((call) => !(call.result as { project?: unknown }).project)).toBe(true);

  // The later issue installs over the expired term as its renewal, and the
  // same work is admitted.
  await openLicensing(user);
  await journey.chooseFiles([journey.path("vendor-renewal/entitlement.json")], "Choose the later-issue entitlement document");
  await press(user, access().getByRole("button", { name: "Renew activation…" }));
  expect(await status(/^License: active\. /)).toMatch(/Expires: \S+\. Grace ends: \S+\.$/);
  const renewed = (await tryProject(user, "investigations", "renewed-work", "Renewed work")) as { state: string; project?: { summary: { project?: { folder: string } } } };
  expect(renewed).toMatchObject({ state: "completed" });
  expect((await journey.commandLine(["project", "show", renewed.project!.summary.project!.folder])).stdout).toMatch(/^Project: Renewed work\n/);

  // In its grace period, a term still admits new work, and says it is in
  // grace.
  await openLicensing(user);
  expect(await selectActivation(user, "vendor-grace")).toMatch(/^License: grace\. /);
  expect(await tryProject(user, "investigations", "grace-work", "Grace work")).toMatchObject({ state: "completed" });
});
