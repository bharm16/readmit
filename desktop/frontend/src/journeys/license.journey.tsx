// This computer's license, handled the way any software purchase is handled,
// over the real facade and checked against the checkout's own command line on
// the same account. A license file delivered at purchase is activated in the
// window, and `readmit license show` reports it and admits new work by it with
// nothing named; the renewed file replaces it in place; a copy saves byte for
// byte; this computer is deactivated only after it is asked, and new work
// stops on the command line too; and what `readmit license import` installs,
// the window reports. None of it contacts anything: the renewal link is an
// address an operator supplied, opened only by a click.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, Journey, press, region } from "../testkit/journey";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** This computer's license, inside the license and activation pane. */
function license() {
  return within(region("This computer's license"));
}

async function openLicensing(user: UserEvent) {
  await press(user, screen.getByRole("button", { name: "License and activation…" }));
  await license().findByRole("button", { name: "Refresh this computer's license" });
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
  expect(await license().findByText(/^No license is activated on this computer\./)).toBeTruthy();
  const unlicensed = await journey.commandLine(["license", "show"]);
  expect(unlicensed.code).not.toBe(0);
  expect(unlicensed.stderr).toContain("no license is installed on this computer");

  // Dismissing the file dialog changes nothing.
  await journey.dismissDialog("files", "Choose your license file");
  await press(user, license().getByRole("button", { name: "Activate a license file…" }));
  expect(await license().findByText("no file was chosen")).toBeTruthy();

  // The license file and the vendor's keys file, checked here and shown in
  // plain words; the one person, computer and runner pool the license
  // assigns are shown chosen.
  await journey.chooseFiles([journey.path("delivery", "entitlement.json")], "Choose your license file");
  await journey.chooseFiles([journey.path("delivery", "trust.json")], "Choose your vendor's verification keys file");
  await press(user, license().getByRole("button", { name: "Activate a license file…" }));
  const review = within(await license().findByRole("group", { name: "License to activate" }));
  expect(review.getByText(/^License for test-organization on the test-only plan: 1 author seat and 16 runner slots, valid from \S+ until \S+, with grace until \S+\. It was checked on this computer with your vendor's verification keys\.$/)).toBeTruthy();
  expect((review.getByLabelText("Who uses this computer") as HTMLSelectElement).value).toBe("test-author");
  expect((review.getByLabelText("This computer") as HTMLSelectElement).value).toBe("test-device");
  expect((review.getByLabelText("Tests run from this computer count against") as HTMLSelectElement).value).toBe("test-runner");
  await press(user, review.getByRole("button", { name: "Activate on this computer" }));
  expect(await license().findByText("This computer's license is activated.")).toBeTruthy();
  expect(license().getByText("Licensed to test-organization on the test-only plan: 1 author seat and 16 runner slots.")).toBeTruthy();
  expect(license().getByText(/^Activated on \S+ for test-author on the computer test-device; tests run from here count against the test-runner runner pool\.$/)).toBeTruthy();
  // Ten days before its end it asks for the renewal, and says where to get it
  // once an operator has configured the account address.
  expect(license().getByText(/^This license expires on \S+, in 10 days\. Renew it/)).toBeTruthy();
  expect(license().getByText("Your account's address is not configured here: ask your vendor for the renewed license file.")).toBeTruthy();

  // The command line reports what the window activated, and new work is
  // admitted by it without naming a license.
  const shown = await journey.commandLine(["license", "show"]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toMatch(/^Entitlement installed: test-entitlement\n/);
  expect(shown.stdout).toContain("Issue sequence: 1\n");
  expect(shown.stdout).toContain("Author: test-author on test-device (assigned)\n");
  expect((await newProjectByHand("first")).code).toBe(0);

  // The operator's account address: the renewal link appears, and following
  // it is the person's own click, never a request the window makes.
  const commercial = within(region("License and trial activation").querySelector(".commercial-access") as HTMLElement);
  await journey.chooseFiles([journey.path("operator", "destinations.json")], "Choose the commercial destinations file");
  await press(user, commercial.getByRole("button", { name: "Choose the commercial destinations file…" }));
  const renewalLink = await license().findByRole("link", { name: "Get renewed license" });
  expect(renewalLink.getAttribute("href")).toBe("https://account.example.test/licenses");
  expect(renewalLink.getAttribute("target")).toBe("_blank");

  // The renewed file replaces the license in place, for the same person and
  // computer; the command line reports the renewal.
  await journey.chooseFiles([journey.path("renewal", "entitlement.json")], "Choose your license file");
  await press(user, license().getByRole("button", { name: "Renew with a license file…" }));
  const renewal = within(await license().findByRole("group", { name: "Renewed license" }));
  await press(user, renewal.getByRole("button", { name: "Install the renewed license" }));
  expect(await license().findByText("The renewed license replaced the previous one.")).toBeTruthy();
  expect(license().getByText(/^Valid until \S+\.$/)).toBeTruthy();
  expect(license().queryByRole("link", { name: "Get renewed license" })).toBeNull();
  expect((await journey.commandLine(["license", "show"])).stdout).toContain("Issue sequence: 2\n");

  // The earlier file offered again is refused, and nothing changes.
  await journey.chooseFiles([journey.path("delivery", "entitlement.json")], "Choose your license file");
  await press(user, license().getByRole("button", { name: "Renew with a license file…" }));
  await press(user, within(await license().findByRole("group", { name: "Renewed license" })).getByRole("button", { name: "Install the renewed license" }));
  expect(await license().findByText("this license is already activated here, or is older than the one activated on this computer")).toBeTruthy();
  await press(user, within(license().getByRole("group", { name: "Renewed license" })).getByRole("button", { name: "Cancel" }));
  expect((await journey.commandLine(["license", "show"])).stdout).toContain("Issue sequence: 2\n");

  // A copy saves byte for byte, as `readmit license export` writes it.
  journey.makeFolder("copies");
  await journey.chooseFolder(journey.path("copies"), "Choose the folder to save a copy of this computer's license in");
  await press(user, license().getByRole("button", { name: "Save a copy of this license…" }));
  expect(await license().findByText(byContent(/^A copy of this license was saved to .*test-entitlement\.json, exactly as it was received\.$/))).toBeTruthy();
  expect(journey.readFile("copies/test-entitlement.json")).toBe(journey.readFile("renewal/entitlement.json"));

  // Deactivating asks first; Escape keeps the license.
  await press(user, license().getByRole("button", { name: "Deactivate this computer…" }));
  expect(license().getByRole("group", { name: "Deactivate this computer?" })).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(license().queryByRole("group", { name: "Deactivate this computer?" })).toBeNull();
  expect(journey.callsTo("DeactivateLicense")).toHaveLength(0);
  await press(user, license().getByRole("button", { name: "Deactivate this computer…" }));
  await press(user, within(license().getByRole("group", { name: "Deactivate this computer?" })).getByRole("button", { name: "Deactivate" }));
  expect(await license().findByText("This computer is deactivated.")).toBeTruthy();
  expect(license().getByText(/^This computer was deactivated on \S+\. Your vendor can reissue its seat/)).toBeTruthy();
  expect(license().getByRole("link", { name: "Open your account" }).getAttribute("href")).toBe("https://account.example.test/licenses");

  // New work stops on the command line too, with the store's own reason;
  // what exists stays readable, and the license still saves a copy.
  const refused = await newProjectByHand("after-deactivation");
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toContain("this device released its entitlement activation");
  expect((await journey.commandLine(["project", "show", "investigations/first"])).stdout).toMatch(/^Project: By hand\n/);
  journey.makeFolder("copies-after");
  await journey.chooseFolder(journey.path("copies-after"), "Choose the folder to save a copy of this computer's license in");
  await press(user, license().getByRole("button", { name: "Save a copy of this license…" }));
  expect(await license().findByText(byContent(/^A copy of this license was saved to /))).toBeTruthy();
  expect(journey.readFile("copies-after/test-entitlement.json")).toBe(journey.readFile("renewal/entitlement.json"));

  // What `readmit license import` installs for this computer, the window
  // reports, and new work is admitted again.
  const imported = await journey.commandLine([
    "license", "import", journey.path("renewal", "entitlement.json"), "--trust", journey.path("renewal", "trust.json"),
    "--author", "test-author", "--device", "test-device",
  ]);
  expect(imported.code).toBe(0);
  await press(user, license().getByRole("button", { name: "Refresh this computer's license" }));
  expect(await license().findByText(/^Activated on \S+ for test-author on the computer test-device\.$/)).toBeTruthy();
  expect(license().getByText(/^Valid until \S+\.$/)).toBeTruthy();
  expect((await newProjectByHand("after-import")).code).toBe(0);
});
