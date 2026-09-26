import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HubPanel } from "./HubPanel";
import { installFacade, uninstallFacade } from "./testkit/wails";
import type { HubResult, HubTransferResult } from "./bindings";

const offline: HubResult = { state: "empty", connected: false, authenticated: false, reason: "no customer hub configured" };
const custody = "Downloaded copies remain under local custody and cannot be revoked.";
const chosen: HubResult = {
  state: "completed",
  connected: false,
  authenticated: false,
  config_path: "/etc/readmit/hub-operator.json",
  hub_url: "https://hub.example.com:8443",
};
const connected: HubResult = { ...chosen, connected: true, custody_warning: custody };

/** The hub panel's operator-only mode, opened from its disclosure. */
async function operatorMode(user: ReturnType<typeof userEvent.setup>) {
  const panel = within(screen.getByRole("region", { name: "Hub" }));
  await user.click(panel.getByRole("button", { name: "Operator-only hub", expanded: false }));
  return within(panel.getByRole("region", { name: "Operator-only hub" }));
}

// An operator-only hub has no identity provider, project or sign-in, so the
// panel's team mode cannot reach it (#312). Its own mode chooses the
// operator's configuration, and a dismissed dialog changes nothing while a
// refused choice says why; connecting is deliberate, a refused connection says
// why, and disconnecting leaves the custody notice.
test("the operator-only hub mode chooses its configuration, connects deliberately and disconnects with the custody notice", async () => {
  const user = userEvent.setup();
  const refusedChoice = "not an operator-only hub configuration this release reads";
  const refusedConnection = "hub connection failed: hub liveness check returned status 503";
  const choices: HubResult[] = [
    { state: "cancelled", connected: false, authenticated: false, reason: "no file was chosen" },
    { state: "failed", connected: false, authenticated: false, reason: refusedChoice },
    chosen,
  ];
  const connections: HubResult[] = [
    { ...chosen, state: "failed", reason: refusedConnection },
    connected,
  ];
  const facade = installFacade({
    HubStatus: async () => offline,
    ChooseOperatorHubConfig: async () => choices.shift()!,
    ConnectOperatorHub: async () => connections.shift()!,
    DisconnectOperatorHub: async () => ({ ...chosen, custody_warning: custody }),
  });

  render(<HubPanel />);
  // The mode is closed until it is opened, from the keyboard here, and
  // closing it again keeps what it showed.
  const panel = within(screen.getByRole("region", { name: "Hub" }));
  const disclosure = panel.getByRole("button", { name: "Operator-only hub", expanded: false });
  const operator = within(panel.getByRole("region", { name: "Operator-only hub" }));
  expect(operator.queryByRole("button", { name: "Choose configuration…" })).toBeNull();
  disclosure.focus();
  await user.keyboard("{Enter}");
  expect(disclosure.getAttribute("aria-expanded")).toBe("true");
  expect(operator.getByText("No operator-only hub configuration chosen.")).toBeTruthy();
  expect(operator.getByText("Not connected")).toBeTruthy();
  const connect = operator.getByRole("button", { name: "Connect" });
  expect(connect.hasAttribute("disabled")).toBe(true);
  const choose = operator.getByRole("button", { name: "Choose configuration…" });

  // A dismissed dialog leaves the mode as it was; Tab reaches the choice.
  await user.tab();
  expect(document.activeElement).toBe(choose);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(facade.callsTo("ChooseOperatorHubConfig")).toHaveLength(1));
  await waitFor(() => expect(choose.hasAttribute("disabled")).toBe(false));
  expect(operator.getByText("No operator-only hub configuration chosen.")).toBeTruthy();
  expect(operator.queryByText("no file was chosen")).toBeNull();
  // A refused choice says why and selects nothing.
  await user.click(choose);
  expect(await operator.findByText(refusedChoice)).toBeTruthy();
  expect(connect.hasAttribute("disabled")).toBe(true);
  // A chosen configuration is shown offline; connecting is its own act.
  await user.click(choose);
  expect(await operator.findByText("Operator-only configuration: /etc/readmit/hub-operator.json")).toBeTruthy();
  expect(operator.queryByText(refusedChoice)).toBeNull();
  expect(operator.getByText("Not connected")).toBeTruthy();
  expect(facade.callsTo("ConnectOperatorHub")).toHaveLength(0);

  await waitFor(() => expect(connect.hasAttribute("disabled")).toBe(false));
  await user.click(connect);
  expect(await operator.findByText(refusedConnection)).toBeTruthy();
  expect(operator.getByText("Not connected")).toBeTruthy();
  expect(operator.queryByRole("button", { name: "Upload file…" })).toBeNull();
  await waitFor(() => expect(connect.hasAttribute("disabled")).toBe(false));
  await user.click(connect);
  expect(await operator.findByText("Connected to operator-only hub (https://hub.example.com:8443)")).toBeTruthy();
  expect(operator.getByRole("note").textContent).toBe(`Copy custody: ${custody}`);
  expect(operator.getByRole("button", { name: "Upload file…" })).toBeTruthy();

  operator.getByRole("button", { name: "Disconnect" }).focus();
  await user.keyboard("[Space]");
  expect(await operator.findByText("Not connected")).toBeTruthy();
  expect(operator.getByRole("note").textContent).toBe(`Copy custody: ${custody}`);
  expect(operator.queryByRole("button", { name: "Upload file…" })).toBeNull();
  expect(operator.getByRole("button", { name: "Connect" })).toBeTruthy();
  await user.click(disclosure);
  expect(disclosure.getAttribute("aria-expanded")).toBe("false");
  expect(panel.queryByRole("button", { name: "Connect" })).toBeNull();
  await user.click(disclosure);
  expect(operator.getByText("Operator-only configuration: /etc/readmit/hub-operator.json")).toBeTruthy();
  expect(facade.calls.map((call) => call.method)).toEqual([
    "HubStatus",
    "ChooseOperatorHubConfig",
    "ChooseOperatorHubConfig",
    "ChooseOperatorHubConfig",
    "ConnectOperatorHub",
    "ConnectOperatorHub",
    "DisconnectOperatorHub",
  ]);

  uninstallFacade();
});

// Storing is authoring: a store the license does not admit, one the hub's
// operation policy refuses and one whose answer never arrived are each shown
// with why, the last as uncertain rather than stored, and a dismissed file
// dialog leaves the last result. A read names the artifact by its digest,
// from the keyboard, and shows a refusal, a digest mismatch and the saved
// file with the custody notice; a dismissed save dialog changes nothing.
test("the operator-only hub mode stores a chosen file and reads an artifact by digest from the keyboard, showing each refusal", async () => {
  const user = userEvent.setup();
  const admission = "operation activation is missing or invalid; select and activate an operation policy";
  const hubRefusal =
    "the hub refused to store the artifact: its operation policy binds no author to this client certificate, or it has served team mode and stores evidence only through a signed-in project";
  const uncertain =
    "the hub did not answer, so whether it stored the artifact is unknown; storing the same file again is safe, or read its digest to check";
  const teamMode = "the hub refused operator-only access; a hub that has served team mode reads and stores evidence only through a signed-in project";
  const mismatch = "artifact integrity verification failed; payload digest mismatch";
  const digest = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2";
  const stores: HubTransferResult[] = [
    { state: "permission_denied", reason: admission },
    { state: "permission_denied", transfer_state: "permission_denied", reason: hubRefusal },
    { state: "failed", transfer_state: "uncertain", reason: uncertain },
    { state: "cancelled", reason: "no file was chosen" },
    { state: "completed", transfer_state: "completed", digest, size: 43, path: "/workspace-under-test/evidence.txt" },
  ];
  const reads: HubTransferResult[] = [
    { state: "permission_denied", transfer_state: "permission_denied", reason: teamMode },
    { state: "failed", transfer_state: "failed", reason: mismatch },
    { state: "cancelled", reason: "no file was named" },
    { state: "completed", transfer_state: "completed", digest, size: 43, path: "/workspace-under-test/received.txt", warning: custody },
  ];
  const facade = installFacade({
    HubStatus: async () => offline,
    ChooseOperatorHubConfig: async () => chosen,
    ConnectOperatorHub: async () => connected,
    StoreOperatorHubArtifact: async () => stores.shift()!,
    ReadOperatorHubArtifact: async () => reads.shift()!,
  });

  render(<HubPanel />);
  const operator = await operatorMode(user);
  await user.click(operator.getByRole("button", { name: "Choose configuration…" }));
  await user.click(await operator.findByRole("button", { name: "Connect" }));
  const store = await operator.findByRole("button", { name: "Upload file…" });
  const outcome = () => operator.getByText(/^(Store|Read):/).textContent;

  await user.click(store);
  expect(await operator.findByText(admission)).toBeTruthy();
  expect(outcome()).toBe("Store: permission_denied");
  await waitFor(() => expect(store.hasAttribute("disabled")).toBe(false));
  await user.click(store);
  expect(await operator.findByText(hubRefusal)).toBeTruthy();
  expect(outcome()).toBe("Store: permission_denied");
  await waitFor(() => expect(store.hasAttribute("disabled")).toBe(false));
  store.focus();
  await user.keyboard("{Enter}");
  expect(await operator.findByText(uncertain)).toBeTruthy();
  expect(outcome()).toBe("Store: uncertain");
  expect(operator.queryByText(digest)).toBeNull();
  // A dismissed file dialog leaves the last result as it was.
  await waitFor(() => expect(store.hasAttribute("disabled")).toBe(false));
  await user.keyboard("[Space]");
  await waitFor(() => expect(facade.callsTo("StoreOperatorHubArtifact")).toHaveLength(4));
  await waitFor(() => expect(store.hasAttribute("disabled")).toBe(false));
  expect(operator.getByText(uncertain)).toBeTruthy();
  await user.click(store);
  expect(await operator.findByText(digest)).toBeTruthy();
  expect(outcome()).toBe("Store: completed (43 bytes)");
  expect(operator.queryByText(/^Saved as:/)).toBeNull();

  // A read needs a digest; Enter in the field reads it.
  const read = operator.getByRole("button", { name: "Download…" });
  expect(read.hasAttribute("disabled")).toBe(true);
  const field = operator.getByLabelText("Artifact SHA-256");
  await user.type(field, `${digest}{Enter}`);
  expect(await operator.findByText(teamMode)).toBeTruthy();
  expect(outcome()).toBe("Read: permission_denied");
  // Tab reaches the button, which reads the same digest.
  await waitFor(() => expect(read.hasAttribute("disabled")).toBe(false));
  await user.click(field);
  await user.tab();
  expect(document.activeElement).toBe(read);
  await user.keyboard("[Space]");
  expect(await operator.findByText(mismatch)).toBeTruthy();
  expect(outcome()).toBe("Read: failed");
  // A dismissed save dialog leaves the last result as it was.
  await waitFor(() => expect(read.hasAttribute("disabled")).toBe(false));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(facade.callsTo("ReadOperatorHubArtifact")).toHaveLength(3));
  await waitFor(() => expect(read.hasAttribute("disabled")).toBe(false));
  expect(operator.getByText(mismatch)).toBeTruthy();
  await user.click(read);
  expect(await operator.findByText("Saved as: /workspace-under-test/received.txt")).toBeTruthy();
  expect(outcome()).toBe("Read: completed (43 bytes)");
  expect(operator.getAllByText(custody).length).toBeGreaterThan(0);
  expect(operator.queryByText(mismatch)).toBeNull();
  expect(facade.callsTo("ReadOperatorHubArtifact").map((call) => call.args)).toEqual([[digest], [digest], [digest], [digest]]);
  expect(facade.callsTo("StoreOperatorHubArtifact").map((call) => call.args)).toEqual([[], [], [], [], []]);

  uninstallFacade();
});

// A digest that is not whole is refused with why and kept in the field to be
// corrected; while a read runs every control of the mode waits for it, and a
// read the application answers busy, because another operation holds it, is
// shown as busy with why rather than as a transfer.
test("the operator-only hub mode shows a refused digest and a busy answer, and holds its controls while a read runs", async () => {
  const user = userEvent.setup();
  const invalid = "an artifact is named by its whole SHA-256 digest: 64 lowercase hexadecimal characters";
  const busy = "another operation is already running";
  const digest = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2";
  const facade = installFacade({
    HubStatus: async () => offline,
    ChooseOperatorHubConfig: async () => chosen,
    ConnectOperatorHub: async () => connected,
    ReadOperatorHubArtifact: async () => ({ state: "failed", reason: invalid }),
  });

  render(<HubPanel />);
  const operator = await operatorMode(user);
  await user.click(operator.getByRole("button", { name: "Choose configuration…" }));
  await user.click(await operator.findByRole("button", { name: "Connect" }));
  const field = await operator.findByLabelText("Artifact SHA-256");
  const read = operator.getByRole("button", { name: "Download…" });

  await user.type(field, "A1B2{Enter}");
  expect(await operator.findByText(invalid)).toBeTruthy();
  expect(operator.getByText(/^Read:/).textContent).toBe("Read: failed");
  expect((field as HTMLInputElement).value).toBe("A1B2");

  facade.reply({ ReadOperatorHubArtifact: async () => ({ state: "busy", reason: busy }) });
  await user.clear(field);
  await user.type(field, `${digest}{Enter}`);
  expect(await operator.findByText(busy)).toBeTruthy();
  expect(operator.getByText(/^Read:/).textContent).toBe("Read: busy");

  const parked = facade.park("ReadOperatorHubArtifact");
  await waitFor(() => expect(read.hasAttribute("disabled")).toBe(false));
  await user.click(read);
  await waitFor(() => expect(parked.size).toBe(1));
  for (const name of ["Download…", "Upload file…", "Choose configuration…", "Disconnect"]) {
    expect(operator.getByRole("button", { name }).hasAttribute("disabled")).toBe(true);
  }
  parked.resolve({ state: "completed", transfer_state: "completed", digest, size: 43, path: "/workspace-under-test/received.txt", warning: custody });
  expect(await operator.findByText("Saved as: /workspace-under-test/received.txt")).toBeTruthy();
  await waitFor(() => expect(read.hasAttribute("disabled")).toBe(false));
  expect(operator.getByRole("button", { name: "Upload file…" }).hasAttribute("disabled")).toBe(false);
  expect(operator.queryByText(busy)).toBeNull();
  expect(facade.callsTo("ReadOperatorHubArtifact").map((call) => call.args)).toEqual([["A1B2"], [digest], [digest]]);

  uninstallFacade();
});
