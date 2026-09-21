// The operation-access pane's own journeys, driven as a person drives them:
// real user events over the real component, with only the typed facade
// boundary stubbed. The fixtures carry identifiers, dates and counts a signed
// document could declare — never a signature value, a credential, a machine
// path from a real machine, or a network address beyond the operator-supplied
// portal destination the journey is about.
import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { OperationAccess } from "./OperationAccess";
import { installFacade, type FacadeHandlers } from "./testkit/wails";

const ENTITLEMENT_PATH = "/private/received/entitlement.json";
const TRUST_PATH = "/private/received/trust.json";
const ACTIVATION_FOLDER = "/private/readmit/activation";
const PORTAL = "https://sandbox-portal.example.test/checkouts";

function receivedLicense() {
  return {
    state: "completed" as const,
    entitlement: ENTITLEMENT_PATH,
    trust: TRUST_PATH,
    document: {
      version: "readmit-entitlement/v2",
      id: "ENT-0002",
      organization: "example-hospital",
      plan: "example-plan",
      sequence: 1,
      issued: "2026-09-18T00:00:00Z",
      not_before: "2026-09-18T00:00:00Z",
      expires: "2026-10-18T00:00:00Z",
      grace_days: 14,
      grace_ends: "2026-11-01T00:00:00Z",
      state: "active",
      seats: 2,
      devices_per_seat: 2,
      assignments: [{ author: "alice", devices: ["desk", "laptop"] }, { author: "bob", devices: ["laptop"] }],
      runner_instances: 3,
      authorities: [{ id: "ci-pool", instances: 1 }, { id: "local-runner", instances: 2 }],
      capabilities: ["author", "execute", "hub"],
      key_id: "vendor-2026a",
      key_status: "active",
      operation_capable: true,
    },
  };
}

function activeStatus() {
  return {
    state: "completed" as const,
    selected: true,
    term: "active",
    expires: "2026-10-18T00:00:00Z",
    grace_ends: "2026-11-01T00:00:00Z",
    author_seats: 2,
    runner_instances: 3,
    clock: {
      schema: "readmit-operation-clock/v1",
      organization: "example-hospital",
      sequence: 1,
      high_water: "2026-09-20T12:00:00Z",
      rollback: false,
      released: false,
    },
  };
}

function quiet(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    OperationStatus: () => ({ state: "failed", reason: "operation activation is missing or invalid; select and activate an operation policy", selected: false }),
    CommercialStatus: () => ({ state: "empty", reason: "the commercial portal destination is not configured; choose the operator-supplied destinations file" }),
    ...extra,
  };
}

test("the free paths stay free and unactivated licensed work is refused by name", async () => {
  const user = userEvent.setup();
  const handlers = quiet();
  const facade = installFacade(handlers);
  render(<OperationAccess />);
  expect(await screen.findByText(/Try the guided synthetic sample without a license/)).toBeTruthy();
  // Nothing is activated: the reason says so and activation is offered, but
  // the pane made only the two quiet reads it owns.
  expect(screen.getByText(/operation activation is missing or invalid/)).toBeTruthy();
  expect(facade.callsTo("OperationStatus").length).toBe(1);
  expect(facade.callsTo("CommercialStatus").length).toBe(1);
  // The commercial prerequisite is visible and offers no navigation.
  expect(screen.getByText(/the commercial portal destination is not configured/)).toBeTruthy();
  expect(screen.queryByRole("link", { name: /Open the commercial portal/ })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Refresh local status" }));
  expect(facade.callsTo("OperationStatus").length).toBe(2);
});

test("a received license is verified, configured, created and activated without hand-authored JSON", async () => {
  const user = userEvent.setup();
  const created: unknown[] = [];
  const handlers = quiet({
    VerifyLicenseDocument: () => receivedLicense(),
    ChooseLicenseFolder: () => ({ state: "completed" as const, folder: ACTIVATION_FOLDER }),
    CreateLicenseActivation: async (request) => {
      created.push(request);
      return { state: "completed" as const, selected: true, reason: "the local activation is created; activate it to admit licensed work" };
    },
    ActivateOperations: () => activeStatus(),
  });
  const facade = installFacade(handlers);
  render(<OperationAccess />);

  await user.click(screen.getByRole("button", { name: "Verify a received license…" }));
  // The document's own declarations are shown, including the term state and
  // the offline-irrelevant facts the verifier decided from the local clock.
  expect(await screen.findByText("example-hospital")).toBeTruthy();
  expect(screen.getByText(/state active/)).toBeTruthy();

  // The role selections come from what the document assigns, not free text.
  await user.selectOptions(screen.getByLabelText("Author this device works as"), "alice");
  await user.selectOptions(screen.getByLabelText("Device to activate"), "laptop");
  await user.selectOptions(screen.getByLabelText("Runner authority"), "ci-pool");
  await user.click(screen.getByRole("button", { name: "Choose the private activation folder…" }));
  expect(await screen.findByText(new RegExp(ACTIVATION_FOLDER))).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Create the local activation" }));
  expect(created).toEqual([{
    entitlement: ENTITLEMENT_PATH, trust: TRUST_PATH,
    author: "alice", device: "laptop", authority: "ci-pool", folder: ACTIVATION_FOLDER,
  }]);
  expect(await screen.findByText(/the local activation is created; activate it/)).toBeTruthy();

  // Activation is the separate explicit action; after it the pane reports the
  // licensed term, and expiry/grace/renewal all run through the same status.
  await user.click(screen.getByRole("button", { name: "Activate license" }));
  expect(await screen.findByText(/License: active\./)).toBeTruthy();

  facade.reply({ OperationStatus: () => ({ ...activeStatus(), term: "grace" }) });
  await user.click(screen.getByRole("button", { name: "Refresh local status" }));
  expect(await screen.findByText(/License: grace\./)).toBeTruthy();

  facade.reply({ RenewLicenseDocument: () => activeStatus() });
  await user.click(screen.getByRole("button", { name: "Renew or extend with a later issue…" }));
  expect(await screen.findByText(/A renewal or an approved extension installs here/)).toBeTruthy();

  facade.reply({ ReleaseOperations: () => ({ ...activeStatus(), clock: { ...activeStatus().clock!, released: true } }) });
  await user.click(screen.getByRole("button", { name: "Release this activation" }));
  expect(await screen.findByText(/This activation is released\./)).toBeTruthy();
});

test("an unusable received license is refused truthfully and configures nothing", async () => {
  const user = userEvent.setup();
  const facade = installFacade(quiet({
    VerifyLicenseDocument: () => ({ state: "failed" as const, reason: "entitlement signature does not match its claims" }),
    ChooseLicenseFolder: () => ({ state: "completed" as const, folder: ACTIVATION_FOLDER }),
  }));
  render(<OperationAccess />);
  await user.click(screen.getByRole("button", { name: "Verify a received license…" }));
  expect(await screen.findByText("entitlement signature does not match its claims")).toBeTruthy();
  // A failed verification offers no configuration: there is no document to
  // select a role from and no create action to press.
  expect(screen.queryByLabelText("Author this device works as")).toBeNull();
  expect(screen.queryByRole("button", { name: "Create the local activation" })).toBeNull();
  expect(facade.callsTo("ChooseLicenseFolder").length).toBe(0);
  // Payment never being proof of an entitlement is stated, and a cancelled or
  // pending checkout changes nothing here.
  expect(screen.getByText(/Completing a payment does not activate anything here/)).toBeTruthy();
  expect(screen.getByText(/a cancelled payment, or a pending issuance leaves everything here unchanged/)).toBeTruthy();
  expect(screen.getByText(/nothing is deleted, and existing work stays readable, verifiable and exportable/)).toBeTruthy();
});

test("a v1 document verifies, is reported as such, and cannot configure operation admission", async () => {
  const user = userEvent.setup();
  installFacade(quiet({
    VerifyLicenseDocument: () => ({
      state: "completed" as const,
      entitlement: ENTITLEMENT_PATH,
      trust: TRUST_PATH,
      document: {
        version: "readmit-entitlement/v1",
        id: "ENT-0001", organization: "example-hospital", plan: "example-plan", sequence: 1,
        issued: "2026-09-18T00:00:00Z", not_before: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z",
        grace_days: 0, grace_ends: "2027-09-18T00:00:00Z", state: "active",
        seats: 1, devices: ["workstation-a"], runner_instances: 1,
        capabilities: ["replay"], key_id: "vendor-2026a", key_status: "active", operation_capable: false,
      },
    }),
  }));
  render(<OperationAccess />);
  await user.click(screen.getByRole("button", { name: "Verify a received license…" }));
  expect(await screen.findByText(/does not grant named-author operation admission/)).toBeTruthy();
  expect(screen.queryByLabelText("Author this device works as")).toBeNull();
});

test("the clock correction journey resolves a latched rollback explicitly", async () => {
  const user = userEvent.setup();
  const resolved: boolean[] = [];
  installFacade(quiet({
    OperationStatus: () => ({
      state: "completed" as const, selected: true, term: "active",
      expires: "2026-10-18T00:00:00Z", grace_ends: "2026-11-01T00:00:00Z",
      author_seats: 2, runner_instances: 3,
      clock: { schema: "readmit-operation-clock/v1", organization: "example-hospital", sequence: 1, high_water: "2026-09-20T12:00:00Z", rollback: true, released: false },
    }),
    ResolveOperationClock: async () => { resolved.push(true); return activeStatus(); },
  }));
  render(<OperationAccess />);
  expect(await screen.findByText(/Clock correction requires explicit resolution/)).toBeTruthy();
  const resolve = screen.getByRole("button", { name: "Resolve corrected clock" });
  expect(resolve.getAttribute("disabled")).toBeNull();
  await user.click(resolve);
  expect(resolved).toEqual([true]);
  expect(await screen.findByText(/No unresolved clock rollback/)).toBeTruthy();
});

test("runner capacity is shown and settled explicitly, never silently", async () => {
  const user = userEvent.setup();
  const settled: { instance: string; reconcile: boolean }[] = [];
  const held = () => ({
    state: "completed" as const,
    organization: "example-hospital",
    authority: "local-runner",
    instances: 2, active: 1, stale: 1, free: 0,
    admissions: [
      { instance: "build-4821", admitted: "2026-09-20T09:00:00Z", lease_until: "2026-09-20T10:00:00Z", state: "active" },
      { instance: "build-4822", admitted: "2026-09-20T09:30:00Z", lease_until: "2026-09-20T09:40:00Z", state: "stale" },
    ],
  });
  installFacade(quiet({
    ShowRunnerAdmissions: () => held(),
    SettleRunnerAdmission: async (request) => { settled.push(request); return held(); },
  }));
  render(<OperationAccess />);
  await user.click(screen.getByRole("button", { name: "Show runner capacity" }));
  expect(await screen.findByText(/1 active, 1 stale, 0 free of 2 granted instances/)).toBeTruthy();
  expect(screen.getByText(/Stale capacity is held until an operator reconciles it/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Reconcile build-4822" }));
  expect(settled).toEqual([{ instance: "build-4822", reconcile: true }]);
  await user.click(screen.getByRole("button", { name: "Release build-4821" }));
  expect(settled).toEqual([{ instance: "build-4822", reconcile: true }, { instance: "build-4821", reconcile: false }]);
});

test("the configured portal destination is shown exactly and navigation is deliberate", async () => {
  const user = userEvent.setup();
  const facade = installFacade(quiet({
    ChooseCommercialDestinations: () => ({ state: "completed" as const, environment: "sandbox", portal: PORTAL, config_path: "/private/readmit/commercial-destinations.json" }),
  }));
  render(<OperationAccess />);
  // Before configuration there is no destination and no link to click.
  expect(screen.queryByRole("link", { name: /Open the commercial portal/ })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Choose the commercial destinations file…" }));
  const portal = await screen.findByRole("link", { name: "Open the commercial portal in your browser" });
  // The destination is shown before navigation and the link carries exactly
  // the operator-supplied URL: no case, workspace or evidence data is added.
  expect(portal.getAttribute("href")).toBe(PORTAL);
  expect(screen.getByText(/Environment: sandbox\./)).toBeTruthy();
  const commercial = screen.getByText(/Purchases, invoices, renewals and cancellations/).closest("div");
  expect(commercial).toBeTruthy();
  const links = within(commercial as HTMLElement).queryAllByRole("link");
  expect(links.length).toBe(1);
  expect(links[0]?.getAttribute("href")).toBe(PORTAL);
  // The commercial section contacted nothing: only the local reads and the
  // one deliberate selection call were made.
  expect(facade.callsTo("ChooseCommercialDestinations").length).toBe(1);
  expect(facade.callsTo("CommercialStatus").length).toBe(1);
  // The boundary sentence is stated where the destination is.
  expect(screen.getByText(/This application makes no request to it/)).toBeTruthy();
  expect(screen.getByText(/a revoked document is learned only when files arrive|Offline limit:/, { exact: false })).toBeTruthy();
});
