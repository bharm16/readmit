// Settings › License as a person meets it: real user events over the real
// page, with only the typed facade boundary stubbed. The fixtures carry
// identifiers, dates and counts a signed license could declare, never a
// signature, a key, a real machine path or an address beyond the
// operator-configured account destination. What decides every fact and
// refusal is the Go facade, tested against the command line's own readers;
// these tests hold the page to showing it truthfully and to installing only
// what the person reviewed, only when they take the final action.
import { expect, test, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ComputerLicense, licenseDay } from "./ComputerLicense";
import type { InstalledLicenseView, LicenseDocumentView, LicenseReviewResult } from "./bindings";
import { installFacade, type FacadeHandlers } from "./testkit/wails";

const RECEIVED = "/private/received/license.json";
const KEYS = "/private/received/vendor-keys.json";
const DIGEST = "5b1f0c3e9a7d2b4c6e8f0a1b3c5d7e9f1a2b3c4d5e6f708192a3b4c5d6e7f809";
const PORTAL = "https://account.example.test/licenses";

function received(extra: Partial<LicenseDocumentView> = {}): LicenseDocumentView {
  return {
    version: "readmit-entitlement/v2", id: "ENT-0002", organization: "example-hospital", plan: "annual", sequence: 1,
    issued: "2026-09-18T00:00:00Z", not_before: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z",
    grace_days: 14, grace_ends: "2027-10-02T00:00:00Z", state: "active", seats: 2, devices_per_seat: 2,
    assignments: [{ author: "alice", devices: ["desk", "laptop"] }, { author: "bob", devices: ["laptop"] }],
    runner_instances: 3, authorities: [{ id: "ci-pool", instances: 1 }, { id: "local-runner", instances: 2 }],
    capabilities: ["author", "execute", "hub"], key_id: "vendor-2026a", key_status: "active", operation_capable: true,
    ...extra,
  };
}

function reviewed(extra: Partial<LicenseReviewResult> = {}): LicenseReviewResult {
  return { state: "completed", entitlement: RECEIVED, trust: KEYS, digest: DIGEST, document: received(), renewal: false, choose_keys: false, ...extra };
}

function installed(extra: Partial<InstalledLicenseView> = {}): InstalledLicenseView {
  return {
    document_id: "ENT-0002", organization: "example-hospital", plan: "annual", sequence: 1, author_seats: 2, runner_slots: 3,
    author: "alice", device: "laptop", runner_pool: "ci-pool",
    starts: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z", grace_ends: "2027-10-02T00:00:00Z",
    term: "active", days_left: 360, renew_soon: false, activated: "2026-09-23T10:00:00Z",
    deactivated: false, new_work: true, current_format: true, clock_rollback: false,
    ...extra,
  };
}

function none(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    LicenseStatus: () => ({ state: "empty", reason: "no license is activated on this computer" }),
    CommercialStatus: () => ({ state: "empty", reason: "the commercial portal destination is not configured" }),
    ...extra,
  };
}

function licensed(view: InstalledLicenseView, extra: FacadeHandlers = {}): FacadeHandlers {
  return none({ LicenseStatus: () => ({ state: "completed", license: view }), ...extra });
}

const page = () => within(screen.getByRole("region", { name: "License" }));

/** The value shown beside a label in the page's read-only values. */
function valueOf(label: string, scope: ReturnType<typeof within> = page()): string | null {
  const term = scope.queryAllByRole("term").find((element: HTMLElement) => element.textContent === label);
  return term?.nextElementSibling?.textContent ?? null;
}

/** The page speaks of licenses, never of an internal contract, and never
 * repeats runner capacity. */
function speaksPlainly() {
  const text = document.body.textContent ?? "";
  expect(text).not.toMatch(/readmit-|entitlement|operation policy|trust store|Renews|slot|instances/i);
}

function renderPage(extra: Partial<Parameters<typeof ComputerLicense>[0]> = {}) {
  return render(<ComputerLicense onAdministratorSetup={() => undefined} {...extra} />);
}

async function more(user: ReturnType<typeof userEvent.setup>) {
  await user.click(page().getByRole("button", { name: "More license actions" }));
  return within(screen.getByRole("menu", { name: "More license actions" }));
}

test("each state of this computer's license shows its actual values and the one action it calls for", async () => {
  const cases: { view: InstalledLicenseView; status: string; action: string | null; shown: [string, string][]; absent: string[] }[] = [
    { view: installed(), status: "Active", action: null, shown: [["Plan", "annual"], ["Licensed user", "alice"], ["Device", "laptop"], ["Runner pool", "ci-pool"], ["Expires", licenseDay("2027-09-18T00:00:00Z")]], absent: ["Grace ends", "Starts"] },
    { view: installed({ renew_soon: true, days_left: 10 }), status: "Active", action: "Renew", shown: [], absent: [] },
    { view: installed({ term: "grace", new_work: true }), status: "Grace period", action: "Renew", shown: [["Grace ends", licenseDay("2027-10-02T00:00:00Z")]], absent: [] },
    { view: installed({ term: "expired", new_work: false }), status: "Expired", action: "Renew", shown: [["Grace ends", licenseDay("2027-10-02T00:00:00Z")]], absent: [] },
    { view: installed({ term: "not-yet-valid", new_work: false }), status: "Not yet valid", action: null, shown: [["Starts", licenseDay("2026-09-18T00:00:00Z")]], absent: [] },
    { view: installed({ current_format: false, author: "", runner_pool: "", new_work: false }), status: "Legacy format", action: null, shown: [["Device", "laptop"]], absent: ["Licensed user", "Runner pool"] },
    { view: installed({ deactivated: true, deactivated_at: "2026-09-25T09:00:00Z", new_work: false }), status: "Deactivated", action: "Activate", shown: [["Deactivated", licenseDay("2026-09-25T09:00:00Z")]], absent: [] },
    { view: installed({ clock_rollback: true, new_work: false }), status: "Clock changed", action: "Resolve clock", shown: [], absent: [] },
    { view: installed({ new_work: false }), status: "Activation incomplete", action: "Activate", shown: [], absent: [] },
  ];
  for (const c of cases) {
    installFacade(licensed(c.view));
    const view = renderPage();
    await waitFor(() => expect(valueOf("Status")).toBe(c.status));
    for (const [label, value] of c.shown) expect(valueOf(label), `${c.status}: ${label}`).toBe(value);
    for (const label of c.absent) expect(valueOf(label), `${c.status}: ${label}`).toBeNull();
    const header = page().getByRole("heading", { name: "License" }).parentElement as HTMLElement;
    const primaries = within(header).queryAllByRole("button").filter((button) => button.className.includes("primary"));
    expect(primaries.map((button) => button.textContent), c.status).toEqual(c.action ? [c.action] : []);
    speaksPlainly();
    view.unmount();
  }

  // No license: its status and Activate, and no account link without a
  // configured account.
  installFacade(none());
  const view = renderPage();
  expect(await page().findByText("No license")).toBeTruthy();
  expect(page().getByRole("button", { name: "Activate" })).toBeTruthy();
  expect(page().queryByRole("link")).toBeNull();
  view.unmount();

  // A license that cannot be read says why, and offers nothing it cannot do.
  installFacade(none({ LicenseStatus: () => ({ state: "failed", reason: "this computer's license cannot be read; move its folder aside before activating again" }) }));
  renderPage();
  expect(await page().findByRole("alert")).toHaveProperty("textContent", "this computer's license cannot be read; move its folder aside before activating again");
});

test("a license file is chosen, verified on Continue, assigned and activated only by the final action", async () => {
  const user = userEvent.setup();
  const ended = vi.fn();
  const facade = installFacade(none({
    ChooseLicenseFile: () => ({ state: "completed", path: RECEIVED, name: "license.json" }),
    ReviewLicense: () => reviewed(),
    ActivateLicense: () => ({ state: "completed", outcome: "activated", license: installed({ author: "bob", runner_pool: "" }) }),
  }));
  renderPage({ onActivationEnded: ended });
  await user.click(await page().findByRole("button", { name: "Activate" }));
  const dialog = within(screen.getByRole("dialog", { name: "Activate license" }));
  expect(dialog.getAllByRole("listitem").map((item) => item.textContent)).toEqual(["License", "Assignment", "Review"]);

  // Choosing the file reads nothing; Continue verifies exactly that file.
  const next = dialog.getByRole("button", { name: "Continue" }) as HTMLButtonElement;
  expect(next.disabled).toBe(true);
  await user.click(dialog.getByRole("button", { name: "Choose file" }));
  expect(await dialog.findByText("license.json")).toBeTruthy();
  expect(dialog.getByRole("button", { name: "Replace" })).toBeTruthy();
  expect(facade.callsTo("ReviewLicense")).toHaveLength(0);
  await user.click(next);
  expect(facade.oneCall("ReviewLicense")).toEqual([{ entitlement: RECEIVED, choose_keys: false }]);

  // Only what the license assigns is offered; two people means a choice, and
  // the runner pool is an explicit choice with its consequence.
  expect(await dialog.findByLabelText("Licensed user")).toBeTruthy();
  const cont = dialog.getByRole("button", { name: "Continue" }) as HTMLButtonElement;
  expect(cont.disabled).toBe(true);
  await user.selectOptions(dialog.getByLabelText("Licensed user"), "bob");
  // bob has one device, shown chosen.
  expect((dialog.getByLabelText("Device") as HTMLSelectElement).value).toBe("laptop");
  expect(cont.disabled).toBe(true);
  await user.selectOptions(dialog.getByLabelText("Runner pool"), "none");
  expect(dialog.getByText("This device will not run tests.")).toBeTruthy();
  expect(facade.callsTo("ActivateLicense")).toHaveLength(0);
  await user.click(cont);

  // The review shows exactly what will be installed; nothing is installed
  // until Activate.
  const review = within(dialog.getByLabelText("Review"));
  expect(valueOf("Plan", review)).toBe("annual");
  expect(valueOf("Organization", review)).toBe("example-hospital");
  expect(valueOf("Licensed user", review)).toBe("bob");
  expect(valueOf("Device", review)).toBe("laptop");
  expect(valueOf("Runner pool", review)).toBe("None");
  expect(valueOf("Expires", review)).toBe(licenseDay("2027-09-18T00:00:00Z"));
  expect(valueOf("New work", review)).toBe("Available");
  expect(dialog.queryAllByRole("textbox")).toHaveLength(0);
  expect(facade.callsTo("ActivateLicense")).toHaveLength(0);
  await user.click(dialog.getByRole("button", { name: "Activate" }));
  expect(facade.oneCall("ActivateLicense")).toEqual([{ entitlement: RECEIVED, trust: KEYS, digest: DIGEST, author: "bob", device: "laptop", authority: "" }]);

  // Success closes the flow and the page shows the installed license.
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Activate license" })).toBeNull());
  expect(valueOf("Status")).toBe("Active");
  expect(valueOf("Licensed user")).toBe("bob");
  expect(ended).toHaveBeenCalledWith(true);
  speaksPlainly();
});

test("pasted contents are bound to their review, and a refused or repeated activation installs nothing twice", async () => {
  const user = userEvent.setup();
  const facade = installFacade(none({
    ReviewLicense: (request) => reviewed({ entitlement: "", digest: request.contents === "second" ? "d2" : "d1", document: received({ assignments: [{ author: "alice", devices: ["laptop"] }], authorities: [] }) }),
  }));
  const activation = facade.park("ActivateLicense");
  renderPage();
  await user.click(await page().findByRole("button", { name: "Activate" }));
  const dialog = within(screen.getByRole("dialog", { name: "Activate license" }));
  await user.click(dialog.getByRole("radio", { name: "Paste" }));
  await user.type(dialog.getByLabelText("License contents"), "first");
  await user.click(dialog.getByRole("button", { name: "Continue" }));
  // A sole person and device are shown chosen; with no runner pools there is
  // no pool to choose.
  expect(((await dialog.findByLabelText("Licensed user")) as HTMLSelectElement).value).toBe("alice");
  expect(dialog.queryByLabelText("Runner pool")).toBeNull();
  await user.click(dialog.getByRole("button", { name: "Continue" }));
  expect(await dialog.findByRole("button", { name: "Activate" })).toBeTruthy();

  // Going back and editing the text withdraws the review: Continue verifies
  // the new text, and activation sends it with its own review's digest.
  await user.click(dialog.getByRole("button", { name: "Back" }));
  await user.click(dialog.getByRole("button", { name: "Back" }));
  const text = dialog.getByLabelText("License contents");
  await user.clear(text);
  await user.type(text, "second");
  await user.click(dialog.getByRole("button", { name: "Continue" }));
  expect(facade.callsTo("ReviewLicense").map((call) => call.args[0])).toEqual([
    { contents: "first", choose_keys: false },
    { contents: "second", choose_keys: false },
  ]);
  await user.click(await dialog.findByRole("button", { name: "Continue" }));
  const activate = await dialog.findByRole("button", { name: "Activate" });

  // A second press while the first is answered sends nothing more.
  await user.click(activate);
  await user.click(activate);
  expect(facade.callsTo("ActivateLicense")).toHaveLength(1);
  expect(facade.oneCall("ActivateLicense")).toEqual([{ contents: "second", trust: KEYS, digest: "d2", author: "alice", device: "laptop", authority: "" }]);
  // A refusal stays in the flow with its reason and every choice.
  await act(async () => activation.resolve({ state: "failed", reason: "this license does not assign this computer to that person; a license moved to another computer is activated there" }));
  expect(await dialog.findByRole("alert")).toHaveProperty("textContent", "this license does not assign this computer to that person; a license moved to another computer is activated there");
  expect(valueOf("Device", within(dialog.getByLabelText("Review")))).toBe("laptop");
  expect(screen.getByRole("dialog", { name: "Activate license" })).toBeTruthy();
  expect(page().getByText("No license")).toBeTruthy();
});

test("only a refusal an updated keys file can address offers Choose verification keys", async () => {
  const user = userEvent.setup();
  let keysChosen = false;
  const facade = installFacade(none({
    ChooseLicenseFile: () => ({ state: "completed", path: RECEIVED, name: "license.json" }),
    ReviewLicense: (request) => {
      if (request.choose_keys) keysChosen = true;
      return keysChosen
        ? reviewed({ trust: "/private/received/updated-keys.json" })
        : { state: "failed", reason: "this license was not signed with your vendor's verification keys; if your vendor changed keys, choose their updated keys file", renewal: false, choose_keys: true };
    },
  }));
  renderPage();
  await user.click(await page().findByRole("button", { name: "Activate" }));
  const dialog = within(screen.getByRole("dialog", { name: "Activate license" }));
  await user.click(dialog.getByRole("button", { name: "Choose file" }));
  await user.click(await dialog.findByRole("button", { name: "Continue" }));
  expect(await dialog.findByRole("alert")).toHaveProperty("textContent", "this license was not signed with your vendor's verification keys; if your vendor changed keys, choose their updated keys file");
  // Still on the license step.
  expect(dialog.queryByLabelText("Licensed user")).toBeNull();
  await user.click(dialog.getByRole("button", { name: "Choose verification keys" }));
  expect(facade.callsTo("ReviewLicense").map((call) => call.args[0])).toEqual([
    { entitlement: RECEIVED, choose_keys: false },
    { entitlement: RECEIVED, choose_keys: true },
  ]);
  // The review with the chosen keys stands: Continue goes on without asking
  // for keys again.
  await user.click(dialog.getByRole("button", { name: "Continue" }));
  expect(await dialog.findByLabelText("Licensed user")).toBeTruthy();
  expect(facade.callsTo("ReviewLicense")).toHaveLength(2);

  // A changed file is refused with no keys offered.
  installFacade(none({
    ChooseLicenseFile: () => ({ state: "completed", path: RECEIVED, name: "license.json" }),
    ReviewLicense: () => ({ state: "failed", reason: "this license file was changed after your vendor signed it, or does not match your vendor's keys", renewal: false, choose_keys: false }),
  }));
  await user.click(dialog.getByRole("button", { name: "Back" }));
  await user.click(dialog.getByRole("button", { name: "Replace" }));
  await user.click(dialog.getByRole("button", { name: "Continue" }));
  expect(await dialog.findByText("this license file was changed after your vendor signed it, or does not match your vendor's keys")).toBeTruthy();
  expect(dialog.queryByRole("button", { name: "Choose verification keys" })).toBeNull();
});

test("a renewal keeps this device's assignment, says Install renewal, and a failed one keeps the installed license", async () => {
  const user = userEvent.setup();
  const facade = installFacade(licensed(installed({ term: "expired", new_work: false }), {
    ReviewLicense: (request) => request.contents === "other"
      ? reviewed({ entitlement: "", renewal: false })
      : reviewed({ entitlement: "", renewal: true, trust: "", document: received({ sequence: 2, expires: "2028-09-18T00:00:00Z" }) }),
    ActivateLicense: () => ({ state: "failed", reason: "this license is already activated here, or is older than the one activated on this computer" }),
  }));
  renderPage();
  await user.click(await page().findByRole("button", { name: "Renew" }));
  const dialog = within(screen.getByRole("dialog", { name: "Renew license" }));
  expect(dialog.getAllByRole("listitem").map((item) => item.textContent)).toEqual(["License", "Review"]);
  await user.click(dialog.getByRole("radio", { name: "Paste" }));

  // A license that is not a renewal of this one goes no further.
  await user.type(dialog.getByLabelText("License contents"), "other");
  await user.click(dialog.getByRole("button", { name: "Continue" }));
  expect(await dialog.findByRole("alert")).toHaveProperty("textContent", "This license is not a renewal of this computer's license.");

  await user.clear(dialog.getByLabelText("License contents"));
  await user.type(dialog.getByLabelText("License contents"), "renewal");
  await user.click(dialog.getByRole("button", { name: "Continue" }));
  const install = await dialog.findByRole("button", { name: "Install renewal" });
  expect(dialog.queryByRole("button", { name: "Activate" })).toBeNull();
  const review = within(dialog.getByLabelText("Review"));
  expect(valueOf("Licensed user", review)).toBe("alice");
  expect(valueOf("Device", review)).toBe("laptop");
  expect(valueOf("Runner pool", review)).toBe("ci-pool");
  expect(valueOf("Expires", review)).toBe(licenseDay("2028-09-18T00:00:00Z"));
  await user.click(install);
  // The renewal keeps this computer's own assignment.
  expect(facade.oneCall("ActivateLicense")).toEqual([{ contents: "renewal", trust: "", digest: DIGEST, author: "", device: "", authority: "" }]);
  expect(await dialog.findByRole("alert")).toHaveProperty("textContent", "this license is already activated here, or is older than the one activated on this computer");
  await user.click(dialog.getByRole("button", { name: "Cancel" }));
  expect(valueOf("Status")).toBe("Expired");
  expect(facade.callsTo("LicenseStatus")).toHaveLength(1);
});

test("deactivating names this device and its one consequence, a refusal keeps the license, and export and setup live in More", async () => {
  const user = userEvent.setup();
  const setup = vi.fn();
  let refuse = true;
  const facade = installFacade(licensed(installed(), {
    DeactivateLicense: () => refuse
      ? { state: "failed", reason: "another change to this computer's license is in progress or was interrupted; try again" }
      : { state: "completed", outcome: "deactivated", license: installed({ deactivated: true, deactivated_at: "2026-09-29T08:00:00Z", new_work: false }) },
    ExportInstalledLicense: () => ({ state: "failed", reason: "the chosen folder already holds a file with this name; choose a different folder" }),
  }));
  renderPage({ onAdministratorSetup: setup });
  await waitFor(() => expect(valueOf("Status")).toBe("Active"));
  let menu = await more(user);
  expect(menu.getAllByRole("menuitem").map((item) => item.textContent)).toEqual(["Renew", "Export license", "Deactivate", "Administrator setup", "Details"]);

  // Export is local and says why it was refused.
  await user.click(menu.getByRole("menuitem", { name: "Export license" }));
  expect(await page().findByRole("alert")).toHaveProperty("textContent", "the chosen folder already holds a file with this name; choose a different folder");
  expect(facade.callsTo("ExportInstalledLicense")).toHaveLength(1);

  menu = await more(user);
  await user.click(menu.getByRole("menuitem", { name: "Deactivate" }));
  const sheet = within(screen.getByRole("dialog", { name: "Deactivate this device?" }));
  expect(valueOf("Device", sheet)).toBe("laptop");
  expect(sheet.getByText("Stops new licensed work on this device; existing evidence stays readable.")).toBeTruthy();
  expect(sheet.getAllByRole("button").slice(-2).map((button) => button.textContent)).toEqual(["Cancel", "Deactivate"]);
  // Escape answers the question and deactivates nothing.
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog", { name: "Deactivate this device?" })).toBeNull();
  expect(facade.callsTo("DeactivateLicense")).toHaveLength(0);

  menu = await more(user);
  await user.click(menu.getByRole("menuitem", { name: "Deactivate" }));
  const again = within(screen.getByRole("dialog", { name: "Deactivate this device?" }));
  await user.click(again.getByRole("button", { name: "Deactivate" }));
  expect(await again.findByRole("alert")).toHaveProperty("textContent", "another change to this computer's license is in progress or was interrupted; try again");
  expect(valueOf("Status")).toBe("Active");
  refuse = false;
  await user.click(again.getByRole("button", { name: "Deactivate" }));
  await waitFor(() => expect(valueOf("Status")).toBe("Deactivated"));
  expect(screen.queryByRole("dialog", { name: "Deactivate this device?" })).toBeNull();
  expect(page().getByRole("button", { name: "Activate" })).toBeTruthy();

  menu = await more(user);
  expect(menu.queryByRole("menuitem", { name: "Deactivate" })).toBeNull();
  await user.click(menu.getByRole("menuitem", { name: "Administrator setup" }));
  expect(setup).toHaveBeenCalledTimes(1);
});

test("a changed clock is resolved only by the explicit action, and a refusal says why", async () => {
  const user = userEvent.setup();
  let behind = true;
  const facade = installFacade(licensed(installed({ clock_rollback: true, new_work: false }), {
    ResolveLicenseClock: () => behind
      ? { state: "failed", reason: "this computer's clock is still earlier than the latest time recorded here; correct the clock first" }
      : { state: "completed", outcome: "resolved", license: installed() },
  }));
  renderPage();
  await user.click(await page().findByRole("button", { name: "Resolve clock" }));
  const sheet = within(screen.getByRole("dialog", { name: "Resolve clock change" }));
  expect(facade.callsTo("ResolveLicenseClock")).toHaveLength(0);
  await user.click(sheet.getByRole("button", { name: "Resolve" }));
  expect(await sheet.findByRole("alert")).toHaveProperty("textContent", "this computer's clock is still earlier than the latest time recorded here; correct the clock first");
  expect(valueOf("Status")).toBe("Clock changed");
  behind = false;
  await user.click(sheet.getByRole("button", { name: "Resolve" }));
  await waitFor(() => expect(valueOf("Status")).toBe("Active"));
  expect(facade.callsTo("ResolveLicenseClock")).toHaveLength(2);
});

test("Manage account is the configured destination only, opened by a click that asks nothing of the facade, and Refresh repeats the local read", async () => {
  const user = userEvent.setup();
  const runners = vi.fn();
  const facade = installFacade(licensed(installed(), { CommercialStatus: () => ({ state: "completed", environment: "production", portal: PORTAL }) }));
  renderPage({ onOpenRunners: runners });
  const link = await page().findByRole("link", { name: "Manage account" });
  expect(link.getAttribute("href")).toBe(PORTAL);
  expect(link.getAttribute("target")).toBe("_blank");
  const before = facade.calls.length;
  link.addEventListener("click", (event) => event.preventDefault());
  await user.click(link);
  expect(facade.calls.length).toBe(before);

  // The runner pool is a way to its settings, not a capacity report here.
  await user.click(page().getByRole("button", { name: "ci-pool" }));
  expect(runners).toHaveBeenCalledTimes(1);

  await user.click(page().getByRole("button", { name: "Refresh license" }));
  await waitFor(() => expect(facade.callsTo("LicenseStatus")).toHaveLength(2));
  expect(facade.callsTo("CommercialStatus")).toHaveLength(1);

  // Identifiers wait in Details, as values.
  const menu = await more(user);
  await user.click(menu.getByRole("menuitem", { name: "Details" }));
  const details = within(screen.getByRole("dialog", { name: "License details" }));
  expect(valueOf("License ID", details)).toBe("ENT-0002");
  expect(valueOf("Organization", details)).toBe("example-hospital");
  speaksPlainly();
});

test("a task refused while the license needs no activation opens nothing, and a changed clock opens its own action", async () => {
  const handled = vi.fn();
  const ended = vi.fn();
  installFacade(licensed(installed()));
  const view = render(<ComputerLicense activateRequest={1} onActivateHandled={handled} onActivationEnded={ended} onAdministratorSetup={() => undefined} />);
  await waitFor(() => expect(ended).toHaveBeenCalledWith(false));
  expect(handled).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("dialog")).toBeNull();
  view.unmount();

  installFacade(licensed(installed({ clock_rollback: true, new_work: false })));
  render(<ComputerLicense activateRequest={2} onActivateHandled={handled} onActivationEnded={ended} onAdministratorSetup={() => undefined} />);
  expect(await screen.findByRole("dialog", { name: "Resolve clock change" })).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "Activate license" })).toBeNull();
});
