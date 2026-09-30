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
import { enter, Journey, press, region } from "../testkit/journey";
import { goTo, goToView, openView, page, sidebar } from "../testkit/navigation";
import { createProject, pressServed, submitProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** One value of a read-only list, by its label. */
function value(rows: HTMLElement, label: string): string | null | undefined {
  return within(rows).getByText(label, { selector: "dt" }).nextElementSibling?.textContent;
}

/** An open sheet, by its title. */
async function sheet(title: string) {
  return within(await screen.findByRole("dialog", { name: title }));
}

async function closed(title: string) {
  await waitFor(() => expect(screen.queryByRole("dialog", { name: title })).toBeNull());
}

/** Settings › License › Administrator setup. */
async function administratorSetup(user: UserEvent) {
  // Settings returns to the page of it last shown.
  await goTo(user, "Settings");
  if (!screen.queryByRole("region", { name: "Activation folder" })) {
    await openView(user, "License");
    await press(user, await within(region("License")).findByRole("button", { name: "More license actions" }));
    await press(user, await screen.findByRole("menuitem", { name: "Administrator setup" }));
  }
  await screen.findByRole("region", { name: "Activation folder" });
  // The page reads the selected folder on arrival; a person acts once it has.
  await journey.settled();
}

/** The selected activation folder's section. */
function activation() {
  return within(region("Activation folder"));
}

/** The selected folder's values, once they have been read. */
function activationRows() {
  return activation().getByLabelText("Activation folder", { selector: "dl" });
}

/** One item of the activation folder's More menu. */
async function activationAction(user: UserEvent, item: string) {
  await press(user, activation().getByRole("button", { name: "More activation actions" }));
  await press(user, await screen.findByRole("menuitem", { name: item }));
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
  await administratorSetup(user);

  // Verified from the two received documents, and installed for the author
  // and device it names into a new private folder; creating it activates
  // nothing.
  await activationAction(user, "Create activation folder");
  const create = await sheet("Create activation folder");
  await journey.chooseFiles([journey.path("vendor-delivery/entitlement.json")], "Choose the received entitlement document");
  await journey.chooseFiles([journey.path("vendor-delivery/trust.json")], "Choose the vendor trust document");
  await press(user, create.getByRole("button", { name: "Choose files" }));
  expect(await create.findByText("test-organization")).toBeTruthy();
  expect(create.getByText("test-only")).toBeTruthy();
  await press(user, create.getByRole("button", { name: "Next" }));
  await user.selectOptions(await create.findByLabelText("Licensed user"), "test-author");
  await user.selectOptions(create.getByLabelText("Device"), "test-device");
  await press(user, create.getByRole("button", { name: "Next" }));
  await journey.chooseFolder(journey.path("license-activation"), "Choose the private folder for the local license activation");
  await press(user, await create.findByRole("button", { name: "Choose folder" }));
  expect(await create.findByText(journey.path("license-activation"))).toBeTruthy();
  await press(user, create.getByRole("button", { name: "Create" }));
  await closed("Create activation folder");
  await waitFor(() => expect(value(activationRows(), "Status")).toBe("Not activated"));
  expect(value(activationRows(), "Folder")).toBe("license-activation");

  // Activated explicitly.
  await activationAction(user, "Activation folder");
  await press(user, (await sheet("Activation folder")).getByRole("button", { name: "Activate" }));
  await closed("Activation folder");
  await waitFor(() => expect(value(activationRows(), "Status")).toBe("Active"));
  expect(value(activationRows(), "Organization")).toBe("test-organization");
  expect(value(activationRows(), "Licensed user")).toBe("test-author");
  expect(value(activationRows(), "Device")).toBe("test-device");

  // Licensed work is admitted: a new project, and a change to its settings
  // that renames it and declares an interface revision, which the command
  // line reads back.
  const project = await createProject(user, journey, "investigations", "licensed-work", "Licensed work");
  const settings = await projectSettings(user);
  await enter(user, settings.getByLabelText("Name", { selector: "#project-name" }), "Licensed handover");
  await press(user, settings.getByRole("button", { name: "Add revision" }));
  await enter(user, settings.getByLabelText("Revision name", { selector: "#revision-0" }), "siu-2.5.1-v2");
  await pressServed(user, journey, settings.getByRole("button", { name: "Save" }), "SaveItem");
  await closed("Project settings");
  expect(await sidebar().findByRole("button", { name: "Project: Licensed handover" })).toBeTruthy();
  const settled = await journey.commandLine(["project", "show", project]);
  expect(settled.stdout).toMatch(/^Project: Licensed handover\n/);
  const revisions = await projectSettings(user);
  expect((revisions.getByLabelText("Revision name", { selector: "#revision-0" }) as HTMLInputElement).value).toBe("siu-2.5.1-v2");
  await press(user, revisions.getByRole("button", { name: "Cancel" }));

  // The same issue offered as a renewal is refused, and the installed
  // document exports byte for byte as it was received.
  await administratorSetup(user);
  await activationAction(user, "Renew activation");
  const renew = await sheet("Renew activation");
  await journey.chooseFiles([journey.path("vendor-delivery/entitlement.json")], "Choose the later-issue entitlement document");
  await press(user, renew.getByRole("button", { name: "Choose file" }));
  await renew.findByRole("button", { name: "Replace" });
  await press(user, renew.getByRole("button", { name: "Install renewal" }));
  expect(await renew.findByText("installed entitlement is already at this issue sequence or a later one")).toBeTruthy();
  await press(user, renew.getByRole("button", { name: "Cancel" }));
  await closed("Renew activation");
  journey.makeFolder("exported");
  await journey.chooseFolder(journey.path("exported"), "Choose the folder to export the entitlement into");
  await activationAction(user, "Export activation license");
  await waitFor(() => expect(journey.callsTo("ExportLicenseDocument").at(-1)?.settled).toBe(true));
  expect(activation().queryByRole("alert")).toBeNull();
  expect(journey.readFile("exported/test-entitlement.json")).toBe(journey.readFile("vendor-delivery/entitlement.json"));

  // Released: the window refuses new work, and so does the command line over
  // the same activation, with the same reason.
  await activationAction(user, "Release activation");
  await press(user, (await sheet("Release activation?")).getByRole("button", { name: "Release" }));
  await closed("Release activation?");
  await waitFor(() => expect(value(activationRows(), "Status")).toBe("Released"));
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
  // The refusal leaves the project open.
  expect(sidebar().getByRole("button", { name: "Project: Licensed handover" })).toBeTruthy();

  // What exists stays readable and can still be backed up, offline.
  journey.makeFolder("backups");
  await goToView(user, "Settings", "Storage");
  const storage = within(await page().findByRole("region", { name: "Storage" }));
  await press(user, (await storage.findAllByRole("button", { name: "Create backup" }))[0]!);
  const backup = await sheet("Create backup");
  await journey.chooseFolder(journey.path("backups"), "Choose where backups are kept");
  await press(user, backup.getByRole("button", { name: /^(Choose…|Change)$/ }));
  await backup.findByText(journey.path("backups"));
  await press(user, backup.getByRole("button", { name: "Create backup" }));
  await closed("Create backup");
  expect(await storage.findByText("Backup created")).toBeTruthy();

  // A released activation is not activated again: the new term needs a new
  // activation folder.
  await administratorSetup(user);
  await activationAction(user, "Activation folder");
  const again = await sheet("Activation folder");
  await press(user, again.getByRole("button", { name: "Activate" }));
  expect(await again.findByText("operation activation is missing or invalid; select and activate an operation policy")).toBeTruthy();
  await press(user, again.getByRole("button", { name: "Cancel" }));
  await closed("Activation folder");
  expect(value(activationRows(), "Status")).toBe("Released");
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
  await goToView(user, "Settings", "License");
  expect(within(region("License")).queryByRole("link", { name: "Manage account" })).toBeNull();
  await administratorSetup(user);
  const portal = () => within(region("Account portal"));
  expect(await portal().findByText("No account portal")).toBeTruthy();

  // A destination that is not an https address is refused, and nothing is
  // saved as a portal.
  await press(user, portal().getByRole("button", { name: "Set up" }));
  const setup = await sheet("Account portal");
  await journey.chooseFiles([journey.path("operator/destinations-insecure.json")], "Choose the commercial destinations file");
  await press(user, setup.getByRole("button", { name: "Choose file" }));
  expect(await setup.findByText("the destinations file cannot be read here; choose a valid commercial destinations file again")).toBeTruthy();
  expect((setup.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);

  // The operator's file: the destination and its environment.
  await journey.chooseFiles([journey.path("operator/destinations.json")], "Choose the commercial destinations file");
  await press(user, setup.getByRole("button", { name: /^(Choose file|Replace)$/ }));
  expect(await setup.findByText("https://portal.example.test/account")).toBeTruthy();
  await press(user, setup.getByRole("button", { name: "Save" }));
  await closed("Account portal");
  const rows = () => portal().getByLabelText("Account portal", { selector: "dl" });
  await waitFor(() => expect(value(rows(), "Destination")).toBe("https://portal.example.test/account"));
  expect(value(rows(), "Environment")).toBe("Sandbox");

  // A link the window itself never requests.
  const checkLink = async () => {
    await goToView(user, "Settings", "License");
    const link = await within(region("License")).findByRole("link", { name: "Manage account" });
    expect(link.getAttribute("href")).toBe("https://portal.example.test/account");
    expect(link.getAttribute("target")).toBe("_blank");
  };
  await checkLink();
  await goToView(user, "Settings", "Security");
  const connections = within(await page().findByRole("table", { name: "Connections" }));
  expect(await connections.findByText("Customer portal", { selector: "td *, td, th *, th" })).toBeTruthy();

  // Kept across a restart: the selection is read back, still without any
  // request to the portal.
  await journey.close();
  await journey.launch();
  await checkLink();
  await administratorSetup(user);
  await waitFor(() => expect(value(rows(), "Destination")).toBe("https://portal.example.test/account"));
});

/** Chooses one supplied activation folder in Administrator setup, reads its
 * status in the sheet and activates it, and returns the status the section
 * then reads. */
async function selectActivation(user: UserEvent, folder: string): Promise<string> {
  await administratorSetup(user);
  const choose = activation().queryByRole("button", { name: "Choose folder" });
  if (choose) await press(user, choose);
  else await activationAction(user, "Activation folder");
  const selecting = await sheet("Activation folder");
  await journey.chooseFolder(journey.path(folder), "Choose the license activation folder");
  const reviews = journey.callsTo("ReviewActivationFolder").length;
  await press(user, selecting.getByRole("button", { name: /^(Choose folder|Replace)$/ }));
  await waitFor(() => expect(journey.callsTo("ReviewActivationFolder")[reviews]?.settled).toBe(true));
  await selecting.findByText(folder);
  await press(user, selecting.getByRole("button", { name: "Activate" }));
  await closed("Activation folder");
  await waitFor(() => expect(value(activationRows(), "Folder")).toBe(folder));
  expect(value(activationRows(), "Organization")).toBe("test-organization");
  return value(activationRows(), "Status") ?? "";
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

  // Expired: the window says so, and new work is refused with the reason,
  // in the window and on the command line alike; nothing is created.
  expect(await selectActivation(user, "vendor-expired")).toBe("Expired");
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
  await administratorSetup(user);
  await activationAction(user, "Renew activation");
  const renew = await sheet("Renew activation");
  await journey.chooseFiles([journey.path("vendor-renewal/entitlement.json")], "Choose the later-issue entitlement document");
  await press(user, renew.getByRole("button", { name: "Choose file" }));
  expect(await renew.findByText("Issue", { selector: "dt" }).then((label) => label.nextElementSibling?.textContent)).toBe("2");
  await press(user, renew.getByRole("button", { name: "Install renewal" }));
  await closed("Renew activation");
  await waitFor(() => expect(value(activationRows(), "Status")).toBe("Active"));
  const renewed = (await tryProject(user, "investigations", "renewed-work", "Renewed work")) as { state: string; project?: { summary: { project?: { folder: string } } } };
  expect(renewed).toMatchObject({ state: "completed" });
  expect((await journey.commandLine(["project", "show", renewed.project!.summary.project!.folder])).stdout).toMatch(/^Project: Renewed work\n/);

  // In its grace period, a term still admits new work, and says it is in
  // grace.
  expect(await selectActivation(user, "vendor-grace")).toBe("Grace period");
  expect(await tryProject(user, "investigations", "grace-work", "Grace work")).toMatchObject({ state: "completed" });
});
