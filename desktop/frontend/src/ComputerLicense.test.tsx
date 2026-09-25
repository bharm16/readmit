// This computer's license as a person meets it: real user events over the
// real component, with only the typed facade boundary stubbed. The fixtures
// carry identifiers, dates and counts a signed license could declare, never
// a signature, a key, a real machine path or an address beyond the
// operator-supplied account destination the renewal is about. What decides
// every fact and refusal is the Go facade, tested against the command line's
// own store readers; these tests hold the window to showing it truthfully, in
// plain words, and to asking before it deactivates anything.
import { expect, test, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ComputerLicense } from "./ComputerLicense";
import type { InstalledLicenseView, LicenseDocumentView } from "./bindings";
import { installFacade, type FacadeHandlers } from "./testkit/wails";

const RECEIVED = "/private/received/license.json";
const KEYS = "/private/received/vendor-keys.json";
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

function installed(extra: Partial<InstalledLicenseView> = {}): InstalledLicenseView {
  return {
    organization: "example-hospital", plan: "annual", sequence: 1, author_seats: 2, runner_slots: 3,
    author: "alice", device: "laptop", runner_pool: "ci-pool",
    starts: "2026-09-18T00:00:00Z", expires: "2027-09-18T00:00:00Z", grace_ends: "2027-10-02T00:00:00Z",
    term: "active", days_left: 360, renew_soon: false, activated: "2026-09-23T10:00:00Z",
    deactivated: false, new_work: true, current_format: true,
    ...extra,
  };
}

function none(extra: FacadeHandlers = {}): FacadeHandlers {
  return { LicenseStatus: () => ({ state: "empty", reason: "no license is activated on this computer" }), ...extra };
}

function licensed(view: InstalledLicenseView, extra: FacadeHandlers = {}): FacadeHandlers {
  return { LicenseStatus: () => ({ state: "completed", license: view }), ...extra };
}

/** The pane speaks of licenses, never of an internal contract. */
function speaksPlainly() {
  const text = document.body.textContent ?? "";
  expect(text).not.toMatch(/readmit-|entitlement|operation policy|trust store/i);
}

const region = () => within(screen.getByRole("region", { name: "Device license" }));

/** The secondary actions live behind the page's More actions disclosure; a
 * person opens it, and the journeys below open it the same way. */
async function moreActions(user: ReturnType<typeof userEvent.setup>) {
  const summary = region().getByText("More actions");
  await user.click(summary);
  return within(summary.closest("details") as HTMLElement);
}

test("a license file is activated for the person and computer it names and reported in plain words", async () => {
  const user = userEvent.setup();
  const changed = vi.fn();
  const facade = installFacade(none({
    ReviewLicense: () => ({ state: "completed", entitlement: RECEIVED, trust: KEYS, document: received(), renewal: false }),
    ActivateLicense: () => ({ state: "completed", outcome: "activated", license: installed() }),
  }));
  render(<ComputerLicense portal={undefined} onChanged={changed} />);
  expect(await region().findByText(/^No license is activated on this computer\./)).toBeTruthy();

  // One primary action names the task; the file and paste methods are choices
  // inside the flow it opens.
  await user.click(region().getByRole("button", { name: "Activate license…" }));
  const methods = region().getByRole("group", { name: "Activation method" });
  await user.click(within(methods).getByRole("button", { name: "Choose file…" }));
  expect(facade.oneCall("ReviewLicense")).toEqual([{ contents: "", choose_keys: false }]);
  expect(await region().findByText(
    "License for example-hospital on the annual plan: 2 author seats and 3 runner slots, valid from 2026-09-18 until 2027-09-18, with grace until 2027-10-02. It was checked on this computer with your vendor's verification keys.",
  )).toBeTruthy();
  // Nothing is activated until a person and one of their computers are chosen
  // from what the license itself assigns.
  const activate = region().getByRole("button", { name: "Activate" });
  expect((activate as HTMLButtonElement).disabled).toBe(true);
  expect(within(region().getByLabelText("This computer")).queryAllByRole("option").map((o) => o.textContent)).toEqual(["Choose a computer"]);
  await user.selectOptions(region().getByLabelText("Who uses this computer"), "alice");
  expect(within(region().getByLabelText("This computer")).getAllByRole("option").map((o) => o.textContent)).toEqual(["Choose a computer", "desk", "laptop"]);
  await user.selectOptions(region().getByLabelText("This computer"), "laptop");
  await user.selectOptions(region().getByLabelText("Tests run from this computer count against"), "ci-pool");
  await user.click(activate);
  expect(facade.oneCall("ActivateLicense")).toEqual([{ entitlement: RECEIVED, contents: "", trust: KEYS, author: "alice", device: "laptop", authority: "ci-pool" }]);

  expect(await region().findByText("This computer's license is activated.")).toBeTruthy();
  expect(region().getByText("Licensed to example-hospital on the annual plan: 2 author seats and 3 runner slots.")).toBeTruthy();
  expect(region().getByText("Activated on 2026-09-23 for alice on the computer laptop; tests run from here count against the ci-pool runner pool.")).toBeTruthy();
  expect(region().getByText("Valid until 2027-09-18.")).toBeTruthy();
  expect(region().queryByRole("group", { name: "License to activate" })).toBeNull();
  expect(changed).toHaveBeenCalledTimes(1);
  // An active license that is not expiring offers no primary task beside its
  // status; renewal, the exact-byte copy and deactivation wait in More
  // actions, and the account link waits until renewal is due.
  expect(region().queryByRole("button", { name: "Activate license…" })).toBeNull();
  const renewals = region().queryAllByRole("button", { name: "Renew license…" });
  expect(renewals).toHaveLength(1);
  expect((renewals[0] as HTMLElement).closest("details")?.querySelector("summary")?.textContent).toBe("More actions");
  const more = await moreActions(user);
  expect(more.getByRole("button", { name: "Export license…" })).toBeTruthy();
  expect(region().queryByRole("link")).toBeNull();
  speaksPlainly();
});

test("pasted contents are checked and activated, and cancelling the paste or the review changes nothing", async () => {
  const user = userEvent.setup();
  const pasted = '{"signed":"synthetic license text"}';
  const facade = installFacade(none({
    ReviewLicense: () => ({
      state: "completed", trust: KEYS, renewal: false,
      document: received({ assignments: [{ author: "bob", devices: ["laptop"] }], authorities: [{ id: "local-runner", instances: 2 }] }),
    }),
    ActivateLicense: () => ({ state: "completed", outcome: "activated", license: installed({ author: "bob", runner_pool: "local-runner" }) }),
  }));
  render(<ComputerLicense portal={undefined} onChanged={() => {}} />);
  await region().findByText(/^No license is activated/);

  // Cancelling the method choice, then the paste, leaves nothing behind.
  await user.click(region().getByRole("button", { name: "Activate license…" }));
  await user.click(within(region().getByRole("group", { name: "Activation method" })).getByRole("button", { name: "Cancel" }));
  expect(region().queryByRole("group", { name: "Activation method" })).toBeNull();

  const openPaste = async () => {
    await user.click(region().getByRole("button", { name: "Activate license…" }));
    await user.click(within(region().getByRole("group", { name: "Activation method" })).getByRole("button", { name: "Paste license…" }));
  };
  await openPaste();
  await user.type(region().getByLabelText("License file contents"), "partial");
  await user.click(region().getByRole("button", { name: "Cancel pasting" }));
  expect(region().queryByLabelText("License file contents")).toBeNull();
  expect(facade.callsTo("ReviewLicense")).toHaveLength(0);

  // Pasted and verified; a license assigning one person, one computer and one
  // runner pool shows them chosen, and Escape discards the review unasked.
  await openPaste();
  const contents = region().getByLabelText("License file contents");
  await user.click(contents);
  await user.paste(pasted);
  await user.click(region().getByRole("button", { name: "Verify license" }));
  const review = await region().findByRole("group", { name: "License to activate" });
  expect((within(review).getByLabelText("Who uses this computer") as HTMLSelectElement).value).toBe("bob");
  expect((within(review).getByLabelText("This computer") as HTMLSelectElement).value).toBe("laptop");
  expect((within(review).getByLabelText("Tests run from this computer count against") as HTMLSelectElement).value).toBe("local-runner");
  within(review).getByLabelText("Who uses this computer").focus();
  await user.keyboard("{Escape}");
  expect(region().queryByRole("group", { name: "License to activate" })).toBeNull();
  expect(facade.callsTo("ActivateLicense")).toHaveLength(0);

  // Verified again from the still-open paste, and activated from the keyboard.
  await user.click(region().getByRole("button", { name: "Verify license" }));
  const again = await region().findByRole("group", { name: "License to activate" });
  within(again).getByRole("button", { name: "Activate" }).focus();
  await user.keyboard("{Enter}");
  expect(await region().findByText("This computer's license is activated.")).toBeTruthy();
  expect(facade.callsTo("ReviewLicense").map((call) => call.args)).toEqual([[{ contents: pasted, choose_keys: false }], [{ contents: pasted, choose_keys: false }]]);
  expect(facade.oneCall("ActivateLicense")).toEqual([{ entitlement: "", contents: pasted, trust: KEYS, author: "bob", device: "laptop", authority: "local-runner" }]);
  expect(region().queryByLabelText("License file contents")).toBeNull();
});

test("a license near its end asks for renewal, opens the account only on a click, and the renewed license replaces it", async () => {
  const user = userEvent.setup();
  const facade = installFacade(licensed(installed({ expires: "2026-10-03T00:00:00Z", days_left: 10, renew_soon: true }), {
    ReviewLicense: () => ({ state: "completed", entitlement: RECEIVED, document: received({ sequence: 2 }), renewal: true }),
    ActivateLicense: () => ({ state: "completed", outcome: "renewed", license: installed({ sequence: 2 }) }),
  }));
  render(<ComputerLicense portal={PORTAL} onChanged={() => {}} />);
  expect(await region().findByText(
    "This license expires on 2026-10-03, in 10 days. Renew it to keep creating and running new work: get the renewed license from your account and activate it here, where it replaces this one.",
  )).toBeTruthy();
  const link = region().getByRole("link", { name: "Get renewed license" });
  expect(link.getAttribute("href")).toBe(PORTAL);
  expect(link.getAttribute("target")).toBe("_blank");
  // Showing the link asked nothing of anyone: only the status was read.
  expect(facade.calls.map((call) => call.method)).toEqual(["LicenseStatus"]);

  // Renewal is the primary action of the expiring state, and its file method
  // sits inside the flow.
  await user.click(region().getByRole("button", { name: "Renew license…" }));
  await user.click(within(region().getByRole("group", { name: "Renewal method" })).getByRole("button", { name: "Choose file…" }));
  const review = await region().findByRole("group", { name: "Renewed license" });
  expect(within(review).getByText("Activating it replaces this computer's license in place, for the same person and computer.")).toBeTruthy();
  expect(within(review).queryByLabelText("Who uses this computer")).toBeNull();
  await user.click(within(review).getByRole("button", { name: "Install renewal" }));
  expect(await region().findByText("The renewed license replaced the previous one.")).toBeTruthy();
  expect(facade.oneCall("ActivateLicense")).toEqual([{ entitlement: RECEIVED, contents: "", trust: "", author: "", device: "", authority: "" }]);
  expect(region().getByText("Valid until 2027-09-18.")).toBeTruthy();
  expect(region().queryByRole("link")).toBeNull();
  speaksPlainly();
});

test("a renewed license pasted from the keyboard replaces this one in place", async () => {
  const user = userEvent.setup();
  const renewal = '{"signed":"synthetic renewed license text"}';
  const facade = installFacade(licensed(installed({ term: "grace", expires: "2026-09-20T00:00:00Z", grace_ends: "2026-10-04T00:00:00Z", days_left: 0 }), {
    ReviewLicense: () => ({ state: "completed", document: received({ sequence: 2 }), renewal: true }),
    ActivateLicense: () => ({ state: "completed", outcome: "renewed", license: installed({ sequence: 2 }) }),
  }));
  render(<ComputerLicense portal={PORTAL} onChanged={() => {}} />);
  expect(await region().findByRole("link", { name: "Get renewed license" })).toBeTruthy();
  region().getByRole("button", { name: "Renew license…" }).focus();
  await user.keyboard("{Enter}");
  await user.click(within(region().getByRole("group", { name: "Renewal method" })).getByRole("button", { name: "Paste license…" }));
  await user.click(region().getByLabelText("License file contents"));
  await user.paste(renewal);
  region().getByRole("button", { name: "Verify license" }).focus();
  await user.keyboard("{Enter}");
  const review = await region().findByRole("group", { name: "Renewed license" });
  within(review).getByRole("button", { name: "Install renewal" }).focus();
  await user.keyboard("{Enter}");
  expect(await region().findByText("The renewed license replaced the previous one.")).toBeTruthy();
  expect(facade.oneCall("ReviewLicense")).toEqual([{ contents: renewal, choose_keys: false }]);
  expect(facade.oneCall("ActivateLicense")).toEqual([{ entitlement: "", contents: renewal, trust: "", author: "", device: "", authority: "" }]);
  expect(region().queryByLabelText("License file contents")).toBeNull();
  expect(region().queryByRole("link", { name: "Get renewed license" })).toBeNull();
});

test("refusals are said plainly and change nothing: a dismissed dialog, a changed file and a refused activation", async () => {
  const user = userEvent.setup();
  const facade = installFacade(licensed(installed(), {
    ReviewLicense: () => ({ state: "cancelled", reason: "no file was chosen", renewal: false }),
  }));
  render(<ComputerLicense portal={undefined} onChanged={() => {}} />);
  await region().findByText(/^Licensed to example-hospital/);
  // The active, unexpired license keeps renewal reachable in More actions.
  const more = await moreActions(user);
  await user.click(more.getByRole("button", { name: "Renew license…" }));
  await user.click(within(region().getByRole("group", { name: "Renewal method" })).getByRole("button", { name: "Choose file…" }));
  expect(await region().findByText("no file was chosen")).toBeTruthy();
  expect(region().queryByRole("group", { name: "Renewal method" })).toBeNull();
  expect(region().queryByRole("group", { name: "License to activate" })).toBeNull();

  // A license that does not verify is shown refused, with a way to verify it
  // again against the vendor's updated keys.
  facade.reply({ ReviewLicense: () => ({ state: "failed", reason: "this license was not signed with your vendor's verification keys; if your vendor changed keys, choose their updated keys file", renewal: false }) });
  await user.click(more.getByRole("button", { name: "Renew license…" }));
  await user.click(within(region().getByRole("group", { name: "Renewal method" })).getByRole("button", { name: "Choose file…" }));
  const refused = await region().findByRole("group", { name: "License to activate" });
  expect(within(refused).getByText(/was not signed with your vendor's verification keys/)).toBeTruthy();
  expect(within(refused).queryByRole("button", { name: /Activate|Install/ })).toBeNull();
  facade.reply({ ReviewLicense: () => ({ state: "completed", entitlement: RECEIVED, trust: KEYS, document: received({ sequence: 1 }), renewal: true }) });
  await user.click(within(refused).getByRole("button", { name: "Verify with keys…" }));
  expect(facade.callsTo("ReviewLicense").at(-1)?.args).toEqual([{ contents: "", choose_keys: true }]);

  // The renewal the facade refuses leaves this computer's license as it was.
  facade.reply({ ActivateLicense: () => ({ state: "failed", reason: "this license is already activated here, or is older than the one activated on this computer" }) });
  await user.click(await region().findByRole("button", { name: "Install renewal" }));
  expect(await region().findByText("this license is already activated here, or is older than the one activated on this computer")).toBeTruthy();
  expect(region().getByText("Valid until 2027-09-18.")).toBeTruthy();
  expect(region().getByRole("group", { name: "Renewed license" })).toBeTruthy();
});

test("deactivating asks first, Escape keeps the license, and a deactivated computer still saves a copy", async () => {
  const user = userEvent.setup();
  const changed = vi.fn();
  const facade = installFacade(licensed(installed(), {
    DeactivateLicense: () => ({ state: "completed", outcome: "deactivated", license: installed({ deactivated: true, deactivated_at: "2026-09-24T09:00:00Z", new_work: false }) }),
    ExportInstalledLicense: () => ({ state: "completed", document: "ENT-0002", path: "/private/copies/ENT-0002.json" }),
  }));
  render(<ComputerLicense portal={PORTAL} onChanged={changed} />);
  await region().findByText(/^Licensed to example-hospital/);

  const more = await moreActions(user);
  await user.click(more.getByRole("button", { name: "Deactivate device…" }));
  const question = region().getByRole("group", { name: "Deactivate this device?" });
  expect(document.activeElement).toBe(within(question).getByRole("button", { name: "Cancel" }));
  await user.keyboard("{Escape}");
  expect(region().queryByRole("group", { name: "Deactivate this device?" })).toBeNull();
  expect(facade.callsTo("DeactivateLicense")).toHaveLength(0);
  await vi.waitFor(() => expect(document.activeElement).toBe(more.getByRole("button", { name: "Deactivate device…" })));

  await user.keyboard("{Enter}");
  await user.click(within(region().getByRole("group", { name: "Deactivate this device?" })).getByRole("button", { name: "Deactivate device" }));
  expect(await region().findByText("This computer is deactivated.")).toBeTruthy();
  expect(region().getByText("This computer was deactivated on 2026-09-24. Your vendor can reissue its seat for another computer from your account. Existing work stays readable, verifiable and exportable.")).toBeTruthy();
  expect(changed).toHaveBeenCalledTimes(1);
  // The seat is reissued in the account, opened only by a click; this
  // computer can activate the license issued for it; and a copy still saves.
  expect(region().getByRole("link", { name: "Open your account" }).getAttribute("href")).toBe(PORTAL);
  expect(region().getByRole("button", { name: "Activate license…" })).toBeTruthy();
  expect(more.queryByRole("button", { name: "Deactivate device…" })).toBeNull();
  await user.click(more.getByRole("button", { name: "Export license…" }));
  expect(await region().findByText("A copy of this license was saved to /private/copies/ENT-0002.json, exactly as it was received.")).toBeTruthy();
  facade.reply({ ExportInstalledLicense: () => ({ state: "failed", reason: "the chosen folder already holds a file with this name; choose a different folder" }) });
  await user.click(more.getByRole("button", { name: "Export license…" }));
  expect(await region().findByText("the chosen folder already holds a file with this name; choose a different folder")).toBeTruthy();
  speaksPlainly();
});

test("expired, grace, not-yet-started and earlier-format licenses say what still works", async () => {
  for (const [view, text, link] of [
    [installed({ term: "grace", expires: "2026-09-20T00:00:00Z", grace_ends: "2026-10-04T00:00:00Z", days_left: 0 }),
      "This license expired on 2026-09-20. New work is still allowed during its grace period, until 2026-10-04. Activate the renewed license to keep working after that.", true],
    [installed({ term: "expired", expires: "2026-09-01T00:00:00Z", days_left: 0, new_work: false }),
      "This license expired on 2026-09-01. Existing work stays readable, verifiable and exportable; creating or running new work needs the renewed license.", true],
    [installed({ term: "not-yet-valid", starts: "2026-10-01T00:00:00Z", new_work: false }), "This license starts on 2026-10-01.", false],
  ] as const) {
    installFacade(licensed(view));
    const rendered = render(<ComputerLicense portal={undefined} onChanged={() => {}} />);
    expect(await region().findByText(text)).toBeTruthy();
    expect(region().queryByText(/Your account's address is not configured here/) !== null).toBe(link);
    rendered.unmount();
  }
  const { author: _author, runner_pool: _pool, ...earlier } = installed({ current_format: false, device: "ws-0413", new_work: false });
  installFacade(licensed(earlier));
  render(<ComputerLicense portal={undefined} onChanged={() => {}} />);
  expect(await region().findByText(/^Activated on 2026-09-23 for the computer ws-0413\. This license is in an earlier format/)).toBeTruthy();
  speaksPlainly();
});
