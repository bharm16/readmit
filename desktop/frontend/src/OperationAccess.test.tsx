// Settings › License › Administrator setup, driven as an administrator drives
// it: real user events over the real page, with only the typed facade
// boundary stubbed. The fixtures carry identifiers, dates and counts a signed
// document could declare — never a signature value, a credential, a machine
// path from a real machine, or a network address beyond the operator-supplied
// portal destination the task is about. Each task is its own sheet, and its
// final action is the only thing that changes anything.
import { expect, test, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AdministratorSetup } from "./OperationAccess";
import { licenseDay } from "./ComputerLicense";
import type { LicenseVerifyResult, OperationResult } from "./bindings";
import { installFacade, type FacadeHandlers } from "./testkit/wails";

const ENTITLEMENT_PATH = "/private/received/entitlement.json";
const LATER_PATH = "/private/received/entitlement-2.json";
const TRUST_PATH = "/private/received/trust.json";
const ACTIVATION_FOLDER = "/private/readmit/activation";
const DESTINATIONS = "/private/readmit/commercial-destinations.json";
const PORTAL = "https://sandbox-portal.example.test/checkouts";

function verified(extra: Partial<LicenseVerifyResult> = {}): LicenseVerifyResult {
  return {
    state: "completed",
    entitlement: ENTITLEMENT_PATH,
    trust: TRUST_PATH,
    digest: "c0ffee",
    document: {
      version: "readmit-entitlement/v2", id: "ENT-0002", organization: "example-hospital", plan: "example-plan", sequence: 1,
      issued: "2026-09-18T00:00:00Z", not_before: "2026-09-18T00:00:00Z", expires: "2026-10-18T00:00:00Z",
      grace_days: 14, grace_ends: "2026-11-01T00:00:00Z", state: "active", seats: 2, devices_per_seat: 2,
      assignments: [{ author: "alice", devices: ["desk", "laptop"] }, { author: "bob", devices: ["laptop"] }],
      runner_instances: 3, authorities: [{ id: "ci-pool", instances: 1 }],
      capabilities: ["author", "execute"], key_id: "vendor-2026a", key_status: "active", operation_capable: true,
    },
    ...extra,
  };
}

function selected(extra: Partial<OperationResult> = {}): OperationResult {
  return {
    state: "completed", selected: true, folder: ACTIVATION_FOLDER, author: "alice", device: "laptop", runner_pool: "ci-pool",
    term: "active", expires: "2026-10-18T00:00:00Z", grace_ends: "2026-11-01T00:00:00Z", author_seats: 2, runner_instances: 3,
    clock: { schema: "readmit-operation-clock/v1", organization: "example-hospital", sequence: 1, high_water: "2026-09-20T12:00:00Z", rollback: false, released: false },
    ...extra,
  };
}

const UNSELECTED: OperationResult = { state: "empty", reason: "operation activation is missing or invalid; select and activate an operation policy", selected: false, author_seats: 0, runner_instances: 0 };

function quiet(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    OperationStatus: () => UNSELECTED,
    CommercialStatus: () => ({ state: "empty", reason: "the commercial portal destination is not configured; choose the operator-supplied destinations file" }),
    ...extra,
  };
}

const section = (name: string) => within(screen.getByRole("region", { name }));

function valueOf(label: string, scope: ReturnType<typeof within>): string | null {
  const term = scope.queryAllByRole("term").find((element: HTMLElement) => element.textContent === label);
  return term?.nextElementSibling?.textContent ?? null;
}

async function task(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(section("Activation folder").getByRole("button", { name: "More activation actions" }));
  await user.click(within(screen.getByRole("menu", { name: "More activation actions" })).getByRole("menuitem", { name }));
}

test("a supplied activation folder is chosen and shown, and only Activate activates and selects it", async () => {
  const user = userEvent.setup();
  let activated = false;
  const facade = installFacade(quiet({
    OperationStatus: () => activated ? selected() : UNSELECTED,
    ReviewActivationFolder: () => ({ state: "failed", reason: "operation activation is missing or invalid; select and activate an operation policy", selected: false, folder: ACTIVATION_FOLDER, author: "alice", device: "laptop", author_seats: 0, runner_instances: 0 }),
    ActivateActivationFolder: () => { activated = true; return selected(); },
  }));
  render(<AdministratorSetup />);
  const folder = section("Activation folder");
  expect(await folder.findByText("No activation folder")).toBeTruthy();
  // The page reads only its own scope: the folder's activation and the
  // configured portal, never this computer's license.
  expect(facade.callsTo("LicenseStatus")).toHaveLength(0);
  await user.click(folder.getByRole("button", { name: "Choose folder" }));
  const sheet = within(screen.getByRole("dialog", { name: "Activation folder" }));
  expect((sheet.getByRole("button", { name: "Activate" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(sheet.getByRole("button", { name: "Choose folder" }));
  expect(await sheet.findByText("activation")).toBeTruthy();
  expect(valueOf("Status", sheet)).toBe("Not activated");
  expect(valueOf("Licensed user", sheet)).toBe("alice");
  expect(valueOf("Device", sheet)).toBe("laptop");
  // Choosing the folder activated and selected nothing.
  expect(facade.callsTo("ActivateActivationFolder")).toHaveLength(0);
  expect(section("Activation folder").getByText("No activation folder")).toBeTruthy();
  await user.click(sheet.getByRole("button", { name: "Activate" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Activation folder" })).toBeNull());
  expect(facade.oneCall("ActivateActivationFolder")).toEqual([{ folder: ACTIVATION_FOLDER }]);
  expect(valueOf("Status", folder)).toBe("Active");
  expect(valueOf("Organization", folder)).toBe("example-hospital");
  expect(valueOf("Runner pool", folder)).toBe("ci-pool");
  expect(valueOf("Expires", folder)).toBe(licenseDay("2026-10-18T00:00:00Z"));
  const reads = facade.callsTo("OperationStatus").length;
  await user.click(folder.getByRole("button", { name: "Refresh activation" }));
  await waitFor(() => expect(facade.callsTo("OperationStatus")).toHaveLength(reads + 1));
  expect(facade.callsTo("LicenseStatus")).toHaveLength(0);
});

test("a received license creates an activation folder in three steps and activates nothing", async () => {
  const user = userEvent.setup();
  const facade = installFacade(quiet({
    VerifyLicenseDocument: () => verified(),
    ChooseLicenseFolder: () => ({ state: "completed", folder: ACTIVATION_FOLDER }),
    CreateLicenseActivation: () => ({ state: "completed", selected: true, author_seats: 0, runner_instances: 0, reason: "the local activation is created; activate it to admit licensed work" }),
  }));
  render(<AdministratorSetup />);
  await section("Activation folder").findByText("No activation folder");
  await task(user, "Create activation folder");
  const sheet = within(screen.getByRole("dialog", { name: "Create activation folder" }));
  expect(sheet.getAllByRole("listitem").map((item) => item.textContent)).toEqual(["License", "Assignment", "Folder"]);
  await user.click(sheet.getByRole("button", { name: "Choose files" }));
  expect(valueOf("Plan", sheet)).toBe("example-plan");
  await user.click(sheet.getByRole("button", { name: "Next" }));
  await user.selectOptions(sheet.getByLabelText("Licensed user"), "alice");
  // A user without a device is not an assignment.
  expect(sheet.getByRole("alert").textContent).toBe("Choose a device for the licensed user.");
  expect((sheet.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
  await user.selectOptions(sheet.getByLabelText("Device"), "desk");
  await user.selectOptions(sheet.getByLabelText("Runner pool"), "ci-pool");
  await user.click(sheet.getByRole("button", { name: "Next" }));
  expect((sheet.getByRole("button", { name: "Create" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(sheet.getByRole("button", { name: "Choose folder" }));
  expect(await sheet.findByText(ACTIVATION_FOLDER)).toBeTruthy();
  expect(valueOf("Device", sheet)).toBe("desk");
  await user.click(sheet.getByRole("button", { name: "Create" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Create activation folder" })).toBeNull());
  expect(facade.oneCall("CreateLicenseActivation")).toEqual([{
    entitlement: ENTITLEMENT_PATH, trust: TRUST_PATH, author: "alice", device: "desk", authority: "ci-pool", folder: ACTIVATION_FOLDER,
  }]);
  // Creation is configuration only: activation stays its own action, taken
  // on the created folder the window now selects.
  expect(facade.callsTo("ActivateOperations")).toHaveLength(0);
  facade.reply({ OperationStatus: () => selected({ state: "failed", reason: "operation activation is missing or invalid; select and activate an operation policy", clock: undefined as never, term: undefined as never }), ActivateOperations: () => selected() });
  await user.click(section("Activation folder").getByRole("button", { name: "Refresh activation" }));
  await waitFor(() => expect(valueOf("Status", section("Activation folder"))).toBe("Not activated"));
  await task(user, "Activation folder");
  const folder = within(screen.getByRole("dialog", { name: "Activation folder" }));
  expect(valueOf("Licensed user", folder)).toBe("alice");
  await user.click(folder.getByRole("button", { name: "Activate" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Activation folder" })).toBeNull());
  expect(facade.callsTo("ActivateOperations")).toHaveLength(1);
  expect(facade.callsTo("ActivateActivationFolder")).toHaveLength(0);
});

test("a license that cannot configure activation, or cannot be verified, goes no further", async () => {
  const user = userEvent.setup();
  let answer: LicenseVerifyResult = { state: "failed", reason: "entitlement signature verification failed" };
  installFacade(quiet({ VerifyLicenseDocument: () => answer }));
  render(<AdministratorSetup />);
  await section("Activation folder").findByText("No activation folder");
  await task(user, "Create activation folder");
  const sheet = within(screen.getByRole("dialog", { name: "Create activation folder" }));
  await user.click(sheet.getByRole("button", { name: "Choose files" }));
  expect(await sheet.findByText("entitlement signature verification failed")).toBeTruthy();
  expect((sheet.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
  const earlier = verified();
  answer = { ...earlier, document: { ...earlier.document!, version: "readmit-entitlement/v1", operation_capable: false } };
  await user.click(sheet.getByRole("button", { name: "Choose files" }));
  expect(await sheet.findByText("This license cannot create an activation folder.")).toBeTruthy();
  expect((sheet.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
});

test("a later issue is verified before Install renewal installs exactly those bytes, and a refusal keeps the activation", async () => {
  const user = userEvent.setup();
  let refuse = true;
  const facade = installFacade(quiet({
    OperationStatus: () => selected(),
    ReviewActivationRenewal: () => verified({ entitlement: LATER_PATH, trust: "", digest: "d2", document: { ...verified().document!, sequence: 2, expires: "2027-10-18T00:00:00Z" } }),
    RenewLicenseDocument: () => refuse
      ? { state: "failed", reason: "entitlement renewal is not newer", selected: true, author_seats: 0, runner_instances: 0 }
      : selected({ expires: "2027-10-18T00:00:00Z" }),
  }));
  render(<AdministratorSetup />);
  await waitFor(() => expect(valueOf("Status", section("Activation folder"))).toBe("Active"));
  await task(user, "Renew activation");
  const sheet = within(screen.getByRole("dialog", { name: "Renew activation" }));
  expect(valueOf("Folder", sheet)).toBe("activation");
  expect((sheet.getByRole("button", { name: "Install renewal" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(sheet.getByRole("button", { name: "Choose file" }));
  expect(await sheet.findByText("entitlement-2.json")).toBeTruthy();
  expect(valueOf("Issue", sheet)).toBe("2");
  expect(facade.callsTo("RenewLicenseDocument")).toHaveLength(0);
  await user.click(sheet.getByRole("button", { name: "Install renewal" }));
  expect(await sheet.findByRole("alert")).toHaveProperty("textContent", "entitlement renewal is not newer");
  expect(valueOf("Expires", section("Activation folder"))).toBe(licenseDay("2026-10-18T00:00:00Z"));
  refuse = false;
  await user.click(sheet.getByRole("button", { name: "Install renewal" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Renew activation" })).toBeNull());
  expect(facade.callsTo("RenewLicenseDocument").map((call) => call.args[0])).toEqual([
    { entitlement: LATER_PATH, digest: "d2" },
    { entitlement: LATER_PATH, digest: "d2" },
  ]);
});

test("release names the folder and device, clock recovery appears only for a detected rollback, and export writes the selected activation", async () => {
  const user = userEvent.setup();
  let rollback = false;
  const facade = installFacade(quiet({
    OperationStatus: () => selected({ clock: { ...selected().clock!, rollback } }),
    ResolveOperationClock: () => { rollback = false; return selected(); },
    ReleaseOperations: () => selected({ clock: { ...selected().clock!, released: true } }),
    ExportLicenseDocument: () => ({ state: "failed", reason: "the destination folder already holds a file with this name; choose a different folder" }),
  }));
  const view = render(<AdministratorSetup />);
  await waitFor(() => expect(valueOf("Status", section("Activation folder"))).toBe("Active"));
  await user.click(section("Activation folder").getByRole("button", { name: "More activation actions" }));
  const items = within(screen.getByRole("menu", { name: "More activation actions" })).getAllByRole("menuitem").map((item) => item.textContent);
  expect(items).toEqual(["Activation folder", "Create activation folder", "Renew activation", "Export activation license", "Release activation", "Details"]);
  await user.keyboard("{Escape}");

  await task(user, "Export activation license");
  expect(await section("Activation folder").findByRole("alert")).toHaveProperty("textContent", "the destination folder already holds a file with this name; choose a different folder");
  expect(facade.callsTo("ExportLicenseDocument")).toHaveLength(1);
  expect(facade.callsTo("ExportInstalledLicense")).toHaveLength(0);

  // A detected rollback offers recovery, which resolves only when asked.
  view.unmount();
  rollback = true;
  render(<AdministratorSetup />);
  await waitFor(() => expect(valueOf("Status", section("Activation folder"))).toBe("Clock changed"));
  await task(user, "Clock recovery");
  const clock = within(screen.getByRole("dialog", { name: "Clock recovery" }));
  expect(clock.getByText("Resumes new licensed work admitted through this folder.")).toBeTruthy();
  expect(facade.callsTo("ResolveOperationClock")).toHaveLength(0);
  await user.click(clock.getByRole("button", { name: "Resolve" }));
  await waitFor(() => expect(valueOf("Status", section("Activation folder"))).toBe("Active"));

  await task(user, "Release activation");
  const release = within(screen.getByRole("dialog", { name: "Release activation?" }));
  expect(valueOf("Folder", release)).toBe("activation");
  expect(valueOf("Device", release)).toBe("laptop");
  expect(release.getByText("Stops new licensed work admitted through this folder.")).toBeTruthy();
  expect(facade.callsTo("ReleaseOperations")).toHaveLength(0);
  await user.click(release.getByRole("button", { name: "Release" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Release activation?" })).toBeNull());
  expect(facade.callsTo("ReleaseOperations")).toHaveLength(1);
  expect(facade.callsTo("DeactivateLicense")).toHaveLength(0);
});

test("the account portal is read from the operator's file, shown as its destination, and kept only by Save", async () => {
  const user = userEvent.setup();
  const configured = vi.fn();
  const handled = vi.fn();
  let saved = false;
  const facade = installFacade(quiet({
    CommercialStatus: () => saved ? { state: "completed", environment: "sandbox", portal: PORTAL, config_path: DESTINATIONS } : { state: "empty", reason: "the commercial portal destination is not configured; choose the operator-supplied destinations file" },
    ReviewCommercialDestinations: () => ({ state: "completed", environment: "sandbox", portal: PORTAL, config_path: DESTINATIONS }),
    SaveCommercialDestinations: () => { saved = true; return { state: "completed", environment: "sandbox", portal: PORTAL, config_path: DESTINATIONS }; },
  }));
  // Security's Edit of the customer portal asks for this sheet once.
  const view = render(<AdministratorSetup portalRequest={1} onPortalHandled={handled} onPortalConfigured={configured} />);
  const sheet = within(await screen.findByRole("dialog", { name: "Account portal" }));
  expect(handled).toHaveBeenCalledTimes(1);
  expect((sheet.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(sheet.getByRole("button", { name: "Choose file" }));
  expect(valueOf("Destination", sheet)).toBe(PORTAL);
  expect(valueOf("Environment", sheet)).toBe("Sandbox");
  // Reading the file kept nothing and opened nothing.
  expect(facade.callsTo("SaveCommercialDestinations")).toHaveLength(0);
  expect(sheet.queryByRole("link")).toBeNull();
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Account portal" })).toBeNull());
  expect(facade.oneCall("SaveCommercialDestinations")).toEqual([{ path: DESTINATIONS, portal: PORTAL }]);
  expect(configured).toHaveBeenCalledWith(true);
  expect(valueOf("Destination", section("Account portal"))).toBe(PORTAL);
  view.rerender(<AdministratorSetup portalRequest={1} onPortalHandled={handled} onPortalConfigured={configured} />);
  expect(screen.queryByRole("dialog", { name: "Account portal" })).toBeNull();

  // Closing without saving keeps what was saved.
  await user.click(section("Account portal").getByRole("button", { name: "Edit" }));
  const again = within(screen.getByRole("dialog", { name: "Account portal" }));
  expect(valueOf("Destination", again)).toBe(PORTAL);
  await user.click(again.getByRole("button", { name: "Cancel" }));
  expect(configured).toHaveBeenLastCalledWith(false);
  expect(facade.callsTo("SaveCommercialDestinations")).toHaveLength(1);
});

test("an unreadable remembered selection stays visible until the folder is chosen again", async () => {
  const refusal = "the remembered operation selection cannot be read; choose an activation folder again";
  installFacade(quiet({ OperationStatus: () => ({ state: "failed", selected: false, author_seats: 0, runner_instances: 0, reason: refusal }) }));
  render(<AdministratorSetup />);
  expect(await section("Activation folder").findByRole("alert")).toHaveProperty("textContent", refusal);
  expect(section("Activation folder").getByText("No activation folder")).toBeTruthy();
});
