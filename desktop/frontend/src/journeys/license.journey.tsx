// This computer's license, handled the way any software purchase is handled,
// over the real facade and checked against the checkout's own command line on
// the same account. A license file delivered at purchase is activated in the
// window, and `readmit license show` reports it and admits new work by it with
// nothing named; the renewed file replaces it in place; a copy saves byte for
// byte; this computer is deactivated only after it is asked, and new work
// stops on the command line too; and what `readmit license import` installs,
// the window reports. None of it contacts anything: the account link is an
// address an operator supplied, opened only by a click.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { Journey, press, region } from "../testkit/journey";
import { goToView, page } from "../testkit/navigation";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** Settings › License, this computer's license. */
function license() {
  return within(region("License"));
}

async function openLicensing(user: UserEvent) {
  await goToView(user, "Settings", "License");
  await license().findByRole("button", { name: "Refresh license" });
}

/** One value of a read-only list, by its label. */
function value(rows: HTMLElement, label: string): string | null | undefined {
  return within(rows).getByText(label, { selector: "dt" }).nextElementSibling?.textContent;
}

/** One item of the License page's More menu. */
async function more(user: UserEvent, item: string): Promise<void> {
  await press(user, license().getByRole("button", { name: "More license actions" }));
  await press(user, await screen.findByRole("menuitem", { name: item }));
}

/** An open sheet, by its title. */
async function sheet(title: string) {
  return within(await screen.findByRole("dialog", { name: title }));
}

/** A new project the command line creates with no license named, admitted
 * or refused by this computer's license alone. */
function newProjectByHand(name: string) {
  return journey.commandLine(["project", "init", "--output", `investigations/${name}`, "--title", "By hand", "--interface-version", "siu-2.5.1-v1"]);
}

test("a delivered license file is activated here, used by the command line with nothing named, renewed in place, saved, and deactivated only when asked", async () => {
  const user = userEvent.setup();
  // What the vendor delivered over time, under one key: the license bought
  // now, ending in ten days, and its renewal. Each delivery folder holds the
  // license file and the vendor's verification keys file.
  journey.provisionLicenseIssues([
    { folder: "delivery", sequence: 1, expires: "240h", graceDays: 14 },
    { folder: "renewal", sequence: 2, expires: "8760h", graceDays: 14 },
  ]);
  journey.writeFile(
    "operator/destinations.json",
    '{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://account.example.test/licenses"}\n',
  );
  journey.makeFolder("investigations");
  await journey.launch();
  await openLicensing(user);

  // Nothing is activated, in the window or on the command line.
  expect(await license().findByText("No license")).toBeTruthy();
  const unlicensed = await journey.commandLine(["license", "show"]);
  expect(unlicensed.code).not.toBe(0);
  expect(unlicensed.stderr).toContain("no license is installed on this computer");

  // Dismissing the file dialog changes nothing.
  await press(user, license().getByRole("button", { name: "Activate" }));
  let flow = await sheet("Activate license");
  await journey.dismissDialog("files", "Choose your license file");
  await press(user, flow.getByRole("button", { name: "Choose file" }));
  await waitFor(() => expect(journey.callsTo("ChooseLicenseFile").at(-1)?.settled).toBe(true));
  expect(flow.getByRole("button", { name: "Choose file" })).toBeTruthy();
  expect((flow.getByRole("button", { name: "Continue" }) as HTMLButtonElement).disabled).toBe(true);

  // The license file and the vendor's keys file, checked here; the one
  // person, computer and runner pool the license assigns are offered, and
  // the review shows exactly what will be installed.
  await journey.chooseFiles([journey.path("delivery", "entitlement.json")], "Choose your license file");
  await press(user, flow.getByRole("button", { name: "Choose file" }));
  expect(await flow.findByText("entitlement.json")).toBeTruthy();
  await journey.chooseFiles([journey.path("delivery", "trust.json")], "Choose your vendor's verification keys file");
  await press(user, flow.getByRole("button", { name: "Continue" }));
  expect(((await flow.findByLabelText("Licensed user")) as HTMLSelectElement).value).toBe("test-author");
  expect((flow.getByLabelText("Device") as HTMLSelectElement).value).toBe("test-device");
  await user.selectOptions(flow.getByLabelText("Runner pool"), "test-runner");
  await press(user, flow.getByRole("button", { name: "Continue" }));
  const review = flow.getByLabelText("Review", { selector: "dl" });
  expect(value(review, "Organization")).toBe("test-organization");
  expect(value(review, "Licensed user")).toBe("test-author");
  expect(value(review, "Device")).toBe("test-device");
  expect(value(review, "Runner pool")).toBe("test-runner");
  expect(value(review, "New work")).toBe("Available");
  await press(user, flow.getByRole("button", { name: "Activate" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Activate license" })).toBeNull());
  const rows = () => license().getByLabelText("License", { selector: "dl" });
  await waitFor(() => expect(value(rows(), "Status")).toBe("Active"));
  expect(value(rows(), "Licensed user")).toBe("test-author");
  expect(value(rows(), "Device")).toBe("test-device");
  expect(value(rows(), "Runner pool")).toBe("test-runner");
  // Ten days before its end the one action it calls for is renewal.
  expect(license().getByRole("button", { name: "Renew" })).toBeTruthy();
  expect(license().queryByRole("link", { name: "Manage account" })).toBeNull();

  // The command line reports what the window activated, and new work is
  // admitted by it without naming a license.
  const shown = await journey.commandLine(["license", "show"]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toMatch(/^Entitlement installed: test-entitlement\n/);
  expect(shown.stdout).toContain("Issue sequence: 1\n");
  expect(shown.stdout).toContain("Author: test-author on test-device (assigned)\n");
  expect((await newProjectByHand("first")).code).toBe(0);

  // The operator's account address, configured in Administrator setup: the
  // account link appears, and following it is the person's own click, never
  // a request the window makes.
  await more(user, "Administrator setup");
  const portal = within(await screen.findByRole("region", { name: "Account portal" }));
  await press(user, await portal.findByRole("button", { name: "Set up" }));
  const portalSheet = await sheet("Account portal");
  await journey.chooseFiles([journey.path("operator", "destinations.json")], "Choose the commercial destinations file");
  await press(user, portalSheet.getByRole("button", { name: "Choose file" }));
  expect(await portalSheet.findByText("https://account.example.test/licenses")).toBeTruthy();
  await press(user, portalSheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Account portal" })).toBeNull());
  await press(user, page().getByRole("button", { name: "Back to settings" }));
  const account = await license().findByRole("link", { name: "Manage account" });
  expect(account.getAttribute("href")).toBe("https://account.example.test/licenses");
  expect(account.getAttribute("target")).toBe("_blank");

  // The renewed file replaces the license in place, for the same person and
  // computer; the command line reports the renewal.
  await press(user, license().getByRole("button", { name: "Renew" }));
  flow = await sheet("Renew license");
  await journey.chooseFiles([journey.path("renewal", "entitlement.json")], "Choose your license file");
  await press(user, flow.getByRole("button", { name: "Choose file" }));
  await flow.findByText("entitlement.json");
  await press(user, flow.getByRole("button", { name: "Continue" }));
  const renewal = await flow.findByLabelText("Review", { selector: "dl" });
  expect(value(renewal, "Licensed user")).toBe("test-author");
  expect(value(renewal, "Device")).toBe("test-device");
  await press(user, flow.getByRole("button", { name: "Install renewal" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Renew license" })).toBeNull());
  await waitFor(() => expect(license().queryByRole("button", { name: "Renew" })).toBeNull());
  expect(value(rows(), "Status")).toBe("Active");
  expect((await journey.commandLine(["license", "show"])).stdout).toContain("Issue sequence: 2\n");
  await more(user, "Details");
  expect(value((await screen.findByRole("dialog", { name: "License details" })).querySelector("dl")!, "Issue")).toBe("2");
  await press(user, within(screen.getByRole("dialog", { name: "License details" })).getByRole("button", { name: "Close" }));

  // The earlier file offered again is refused, and nothing changes.
  await more(user, "Renew");
  flow = await sheet("Renew license");
  await journey.chooseFiles([journey.path("delivery", "entitlement.json")], "Choose your license file");
  await press(user, flow.getByRole("button", { name: "Choose file" }));
  await flow.findByText("entitlement.json");
  await press(user, flow.getByRole("button", { name: "Continue" }));
  await flow.findByLabelText("Review", { selector: "dl" });
  await press(user, flow.getByRole("button", { name: "Install renewal" }));
  expect(await flow.findByText("this license is already activated here, or is older than the one activated on this computer")).toBeTruthy();
  await press(user, flow.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Renew license" })).toBeNull());
  expect((await journey.commandLine(["license", "show"])).stdout).toContain("Issue sequence: 2\n");

  // A copy saves byte for byte, as `readmit license export` writes it.
  journey.makeFolder("copies");
  await journey.chooseFolder(journey.path("copies"), "Choose the folder to save a copy of this computer's license in");
  await more(user, "Export license");
  await waitFor(() => expect(journey.callsTo("ExportInstalledLicense").at(-1)?.settled).toBe(true));
  expect(journey.readFile("copies/test-entitlement.json")).toBe(journey.readFile("renewal/entitlement.json"));

  // Deactivating asks first; Escape keeps the license.
  await more(user, "Deactivate");
  await sheet("Deactivate this device?");
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Deactivate this device?" })).toBeNull());
  expect(journey.callsTo("DeactivateLicense")).toHaveLength(0);
  expect(value(rows(), "Status")).toBe("Active");
  await more(user, "Deactivate");
  await press(user, (await sheet("Deactivate this device?")).getByRole("button", { name: "Deactivate" }));
  await waitFor(() => expect(value(rows(), "Status")).toBe("Deactivated"));
  expect(value(rows(), "Deactivated")).not.toBe("—");
  expect(license().getByRole("button", { name: "Activate" })).toBeTruthy();
  expect(license().getByRole("link", { name: "Manage account" }).getAttribute("href")).toBe("https://account.example.test/licenses");

  // New work stops on the command line too, with the store's own reason;
  // what exists stays readable, and the license still saves a copy.
  const refused = await newProjectByHand("after-deactivation");
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toContain("this device released its entitlement activation");
  expect((await journey.commandLine(["project", "show", "investigations/first"])).stdout).toMatch(/^Project: By hand\n/);
  journey.makeFolder("copies-after");
  await journey.chooseFolder(journey.path("copies-after"), "Choose the folder to save a copy of this computer's license in");
  const exports = journey.callsTo("ExportInstalledLicense").length;
  await more(user, "Export license");
  await waitFor(() => expect(journey.callsTo("ExportInstalledLicense")[exports]?.settled).toBe(true));
  expect(journey.readFile("copies-after/test-entitlement.json")).toBe(journey.readFile("renewal/entitlement.json"));

  // What `readmit license import` installs for this computer, the window
  // reports, and new work is admitted again.
  const imported = await journey.commandLine([
    "license", "import", journey.path("renewal", "entitlement.json"), "--trust", journey.path("renewal", "trust.json"),
    "--author", "test-author", "--device", "test-device",
  ]);
  expect(imported.code).toBe(0);
  await press(user, license().getByRole("button", { name: "Refresh license" }));
  await waitFor(() => expect(value(rows(), "Status")).toBe("Active"));
  expect(value(rows(), "Licensed user")).toBe("test-author");
  expect(value(rows(), "Device")).toBe("test-device");
  expect(license().queryByText("Deactivated", { selector: "dt" })).toBeNull();
  expect((await newProjectByHand("after-import")).code).toBe(0);
});

test("administrator activation choices, export and clock correction agree with the command line", async () => {
  const user = userEvent.setup();
  const supplied = journey.provisionLicense("supplied-activation");
  const policy = `${supplied}/operation-policy.json`;
  journey.makePrivateFolder("new-activation");
  const link = journey.makeLink("activation-shortcut", journey.path("new-activation"));
  journey.makeFolder("copies");
  journey.makeFolder("other-copies");
  journey.writeFile("copies/test-entitlement.json", "existing export must survive");
  await journey.launch();
  await openLicensing(user);
  await more(user, "Administrator setup");
  const folder = () => within(region("Activation folder"));
  const moreActivation = async (item: string) => {
    await press(user, folder().getByRole("button", { name: "More activation actions" }));
    await press(user, await screen.findByRole("menuitem", { name: item }));
  };

  // Creating a folder from a verified license asks for a new private
  // folder: a symbolic link is refused, and a dismissed dialog chooses none.
  await moreActivation("Create activation folder");
  const create = await sheet("Create activation folder");
  await journey.chooseFiles([`${supplied}/entitlement.json`], "Choose the received entitlement document");
  await journey.chooseFiles([`${supplied}/trust.json`], "Choose the vendor trust document");
  await press(user, create.getByRole("button", { name: "Choose files" }));
  expect(await create.findByText("test-organization")).toBeTruthy();
  await press(user, create.getByRole("button", { name: "Next" }));
  await press(user, await create.findByRole("button", { name: "Next" }));
  await journey.chooseFolder(link, "Choose the private folder for the local license activation");
  await press(user, await create.findByRole("button", { name: "Choose folder" }));
  expect(await create.findByText("choose an existing folder that is not a symbolic link")).toBeTruthy();
  await journey.dismissDialog("folder", "Choose the private folder for the local license activation");
  await press(user, create.getByRole("button", { name: "Choose folder" }));
  await waitFor(() => expect(journey.callsTo("ChooseLicenseFolder").at(-1)?.settled).toBe(true));
  expect((create.getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(true);
  await press(user, create.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Create activation folder" })).toBeNull());

  // Choosing the supplied folder reads it without activating it; Activate
  // does.
  await press(user, folder().getByRole("button", { name: "Choose folder" }));
  const choose = await sheet("Activation folder");
  await journey.dismissDialog("folder", "Choose the license activation folder");
  await press(user, choose.getByRole("button", { name: "Choose folder" }));
  await waitFor(() => expect(journey.callsTo("ReviewActivationFolder").at(-1)?.settled).toBe(true));
  expect((choose.getByRole("button", { name: "Activate" }) as HTMLButtonElement).disabled).toBe(true);
  await journey.chooseFolder(supplied, "Choose the license activation folder");
  await press(user, choose.getByRole("button", { name: "Choose folder" }));
  expect(await choose.findByText("test-organization")).toBeTruthy();
  await press(user, choose.getByRole("button", { name: "Activate" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Activation folder" })).toBeNull());
  const rows = () => folder().getByLabelText("Activation folder", { selector: "dl" });
  await waitFor(() => expect(value(rows(), "Status")).toBe("Active"));

  // Export never overwrites a file, a dismissed dialog writes nothing, and a
  // copy is written byte for byte.
  await journey.chooseFolder(journey.path("copies"), "Choose the folder to export the entitlement into");
  await moreActivation("Export activation license");
  expect(await folder().findByText("the destination folder already holds a file with this name; choose a different folder")).toBeTruthy();
  expect(journey.readFile("copies/test-entitlement.json")).toBe("existing export must survive");
  await journey.dismissDialog("folder", "Choose the folder to export the entitlement into");
  await moreActivation("Export activation license");
  await waitFor(() => expect(folder().queryByRole("alert")).toBeNull());
  await journey.chooseFolder(journey.path("other-copies"), "Choose the folder to export the entitlement into");
  const exports = journey.callsTo("ExportLicenseDocument").length;
  await moreActivation("Export activation license");
  await waitFor(() => expect(journey.callsTo("ExportLicenseDocument")[exports]?.settled).toBe(true));
  expect(journey.readFile("other-copies/test-entitlement.json")).toBe(journey.readFile("supplied-activation/entitlement.json"));

  // A clock rollback is read on refresh and resolved only once the clock is
  // past the latest recorded time, as the command line decides.
  const clockFile = "supplied-activation/clock.json";
  const clock = JSON.parse(journey.readFile(clockFile)) as Record<string, unknown>;
  const highWater = new Date(Date.now() + 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
  journey.changeFile(clockFile, JSON.stringify({ ...clock, high_water: highWater, rollback: true }) + "\n");
  await press(user, folder().getByRole("button", { name: "Refresh activation" }));
  await waitFor(() => expect(value(rows(), "Status")).toBe("Clock changed"));
  const cliRefusal = await journey.commandLine(["--operation-policy", policy, "license", "operation", "resolve"]);
  expect(cliRefusal.code).not.toBe(0);
  await moreActivation("Clock recovery");
  const recovery = await sheet("Clock recovery");
  await press(user, recovery.getByRole("button", { name: "Resolve" }));
  expect(await recovery.findByText(/local UTC moved backwards more than five minutes/)).toBeTruthy();
  expect((JSON.parse(journey.readFile(clockFile)) as { rollback: boolean }).rollback).toBe(true);

  const corrected = new Date(Date.now() - 60 * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
  journey.changeFile(clockFile, JSON.stringify({ ...clock, high_water: corrected, rollback: true }) + "\n");
  await press(user, recovery.getByRole("button", { name: "Resolve" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Clock recovery" })).toBeNull());
  await waitFor(() => expect(value(rows(), "Status")).toBe("Active"));
  const state = JSON.parse(journey.readFile(clockFile)) as { rollback: boolean; high_water: string };
  expect(state.rollback).toBe(false);
  expect(Date.parse(state.high_water)).toBeGreaterThanOrEqual(Date.parse(corrected));
  const cliStatus = await journey.commandLine(["--operation-policy", policy, "license", "operation", "status"]);
  expect(cliStatus.code).toBe(0);
  expect(cliStatus.stdout).toContain(state.high_water);
});
