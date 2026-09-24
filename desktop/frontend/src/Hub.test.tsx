import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HubPanel } from "./HubPanel";
import { installFacade, uninstallFacade } from "./testkit/wails";
import {
  defaultHubResult,
  defaultDiagnosisResult,
  defaultArtifactsResult,
} from "./testkit/fixtures";
import type {
  HubResult,
  HubDiagnosisResult,
  HubAuthUrlResult,
  HubArtifactsResult,
  HubTransferResult,
} from "./bindings";

test("HubPanel displays offline mode initially and allows selecting hub config", async () => {
  const user = userEvent.setup();
  const initialStatus: HubResult = {
    state: "empty",
    connected: false,
    authenticated: false,
    reason: "no customer hub configured",
  };

  const chosenConfig: HubResult = {
    state: "completed",
    connected: false,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.example.com:8443",
  };

  const facade = installFacade({
    HubStatus: async () => initialStatus,
    ChooseHubConfig: async () => chosenConfig,
  });

  render(<HubPanel />);

  // Check initial offline status
  expect(await screen.findByText(/Offline \/ Local Mode/i)).toBeTruthy();
  expect(screen.getByText(/No configuration file selected/i)).toBeTruthy();

  // Click choose config
  const chooseBtn = screen.getByRole("button", { name: /Choose hub configuration…/i });
  await user.click(chooseBtn);

  expect(facade.callsTo("ChooseHubConfig").length).toBe(1);
  expect(await screen.findByText(/Configuration: \/etc\/readmit\/hub-client\.json/i)).toBeTruthy();

  uninstallFacade();
});

test("HubPanel runs prerequisite diagnostics and displays check results", async () => {
  const user = userEvent.setup();
  const configuredStatus: HubResult = {
    state: "completed",
    connected: false,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.example.com:8443",
  };

  const diagResult: HubDiagnosisResult = defaultDiagnosisResult({
    checks: [
      { name: "ca_certificate", passed: true, message: "CA certificate is valid" },
      { name: "key_pair_match", passed: false, message: "certificate and private key mismatch", detail: "key error" },
    ],
    passed: false,
  });

  const facade = installFacade({
    HubStatus: async () => configuredStatus,
    DiagnoseHub: async () => diagResult,
  });

  render(<HubPanel />);

  const diagBtn = await screen.findByRole("button", { name: /Diagnose prerequisites/i });
  await user.click(diagBtn);

  expect(facade.callsTo("DiagnoseHub").length).toBe(1);
  expect(await screen.findByText(/Checks Failed/i)).toBeTruthy();
  expect(screen.getByText(/ca_certificate:/i)).toBeTruthy();
  expect(screen.getByText(/certificate and private key mismatch/i)).toBeTruthy();

  uninstallFacade();
});

test("HubPanel connects to hub deliberately and disconnects with custody warning", async () => {
  const user = userEvent.setup();
  const configuredStatus: HubResult = {
    state: "completed",
    connected: false,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.example.com:8443",
  };

  const connectedStatus: HubResult = {
    state: "completed",
    connected: true,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.example.com:8443",
  };

  const disconnectedStatus: HubResult = {
    state: "completed",
    connected: false,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.example.com:8443",
    custody_warning: "Downloaded copies remain under local custody and cannot be revoked.",
  };

  const facade = installFacade({
    HubStatus: async () => configuredStatus,
    ConnectHub: async () => connectedStatus,
    DisconnectHub: async () => disconnectedStatus,
  });

  render(<HubPanel />);

  const connectBtn = await screen.findByRole("button", { name: /Connect to hub/i });
  await user.click(connectBtn);

  expect(facade.callsTo("ConnectHub").length).toBe(1);
  expect(await screen.findByText(/Connected \(https:\/\/hub\.example\.com:8443\)/i)).toBeTruthy();

  // Disconnect
  const disconnectBtn = screen.getByRole("button", { name: /Disconnect/i });
  await user.click(disconnectBtn);

  expect(facade.callsTo("DisconnectHub").length).toBe(1);
  expect(await screen.findByText(/Downloaded copies remain under local custody and cannot be revoked\./i)).toBeTruthy();

  uninstallFacade();
});

test("HubPanel performs first-party PKCE sign-in and shows authorized projects and capabilities", async () => {
  const user = userEvent.setup();
  const connectedStatus: HubResult = {
    state: "completed",
    connected: true,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.example.com:8443",
  };

  const authUrlResult: HubAuthUrlResult = {
    state: "completed",
    auth_url: "http://127.0.0.1:4567/auth?client_id=test",
    port: 4567,
  };

  const authenticatedResult: HubResult = defaultHubResult();

  const facade = installFacade({
    HubStatus: async () => connectedStatus,
    StartHubAuth: async () => authUrlResult,
    CompleteHubAuth: async () => authenticatedResult,
  });

  render(<HubPanel />);

  const signInBtn = await screen.findByRole("button", { name: /Sign in with Customer IdP/i });
  await user.click(signInBtn);

  expect(facade.callsTo("StartHubAuth").length).toBe(1);
  expect(facade.callsTo("CompleteHubAuth").length).toBe(1);

  // Authenticated state displayed
  expect(await screen.findByText(/Subject:/i)).toBeTruthy();
  expect(screen.getByText(/analyst@customer\.example/i)).toBeTruthy();

  // Authorized project & effective capabilities
  expect(screen.getByText("cardio-icu")).toBeTruthy();
  expect(screen.getByText("Authorized")).toBeTruthy();
  expect(screen.getByText("[evidence.read]")).toBeTruthy();
  expect(screen.getByText("[evidence.write]")).toBeTruthy();

  // Denied project
  expect(screen.getByText("restricted-study")).toBeTruthy();
  expect(screen.getByText(/Denied: access refused; role or grant denied/i)).toBeTruthy();

  uninstallFacade();
});

test("HubPanel displays project artifacts, handles download with custody notice, and uploads artifacts", async () => {
  const user = userEvent.setup();
  const authenticatedStatus: HubResult = defaultHubResult();
  const artifactsData: HubArtifactsResult = defaultArtifactsResult();

  const downloadResult: HubTransferResult = {
    state: "completed",
    transfer_state: "completed",
    digest: artifactsData.artifacts?.[0]?.digest ?? "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    size: 2048,
    path: "/tmp/downloaded.bin",
    warning: "Downloaded copies remain under local custody and cannot be revoked.",
  };

  const uploadResult: HubTransferResult = {
    state: "completed",
    transfer_state: "completed",
    digest: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
    size: 1024,
    path: "/tmp/local-evidence.bin",
  };

  const facade = installFacade({
    HubStatus: async () => authenticatedStatus,
    ListHubProjectArtifacts: async () => artifactsData,
    DownloadHubArtifact: async () => downloadResult,
    UploadHubArtifact: async () => uploadResult,
  });

  render(<HubPanel />);

  // Click View Project Artifacts
  const viewBtn = await screen.findByRole("button", { name: /View Project Artifacts/i });
  await user.click(viewBtn);

  expect(facade.callsTo("ListHubProjectArtifacts").length).toBe(1);
  expect(await screen.findByText(/Artifacts for Project: cardio-icu/i)).toBeTruthy();
  expect(screen.getByText("lead@hospital.org")).toBeTruthy();

  // Download
  const destInput = screen.getByLabelText(/Download destination path:/i);
  await user.type(destInput, "/tmp/downloaded.bin");

  const dlBtn = screen.getByRole("button", { name: /^Download$/i });
  await user.click(dlBtn);

  expect(facade.callsTo("DownloadHubArtifact").length).toBe(1);
  expect(await screen.findByText(/Transfer state:/i)).toBeTruthy();

  // Upload
  const srcInput = screen.getByLabelText(/Upload source path:/i);
  await user.type(srcInput, "/tmp/local-evidence.bin");

  const uploadBtn = screen.getByRole("button", { name: /Publish Artifact/i });
  await user.click(uploadBtn);

  expect(facade.callsTo("UploadHubArtifact").length).toBe(1);
  // The published artifact's digest is shown whole: it is what a review of
  // that evidence names.
  expect(await screen.findByText(uploadResult.digest!)).toBeTruthy();

  uninstallFacade();
});

/** Connected to the hub the fixtures name, and not signed in. */
const signedOutStatus: HubResult = {
  state: "completed",
  connected: true,
  authenticated: false,
  config_path: "/etc/readmit/hub-client.json",
  hub_url: "https://hub.customer.example:8443",
};

// A sign-in that does not complete (#326) is a recoverable error: the panel
// keeps the connection the person made, says why, and offers the next action,
// which reaches the application instead of being refused as busy. While the
// sign-in waits for the browser it holds the application's one operation
// slot, so the panel offers only its cancel.
test("HubPanel shows a failed sign-in as a recoverable error and the next action goes through", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: async () => signedOutStatus,
    StartHubAuth: async () => ({ state: "completed", auth_url: "https://idp.customer.example/authorize", port: 1 }),
  });
  const signIn = facade.park("CompleteHubAuth");

  render(<HubPanel />);
  await user.click(await screen.findByRole("button", { name: /Sign in with Customer IdP/i }));
  expect(await screen.findByRole("button", { name: "Cancel sign-in" })).toBeTruthy();
  expect(screen.getByRole("button", { name: /Sign in with Customer IdP/i }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("button", { name: /Refresh status/i }).hasAttribute("disabled")).toBe(true);

  signIn.resolve({
    state: "failed",
    connected: false,
    authenticated: false,
    reason: "authentication callback failed: IdP returned error: access_denied",
  });
  expect(await screen.findByText(/IdP returned error: access_denied/)).toBeTruthy();
  expect(screen.getByText(/Connected \(https:\/\/hub\.customer\.example:8443\)/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Cancel sign-in" })).toBeNull();
  // Nothing was retried or refreshed on the person's behalf.
  expect(facade.callsTo("StartHubAuth").length).toBe(1);
  expect(facade.callsTo("CompleteHubAuth").length).toBe(1);
  expect(facade.callsTo("HubStatus").length).toBe(1);

  // The next action is the person's own, and it reaches the application.
  await user.click(screen.getByRole("button", { name: /Sign in with Customer IdP/i }));
  expect(facade.callsTo("StartHubAuth").length).toBe(2);
  await screen.findByRole("button", { name: "Cancel sign-in" });
  signIn.resolve(defaultHubResult());
  expect(await screen.findByText(/analyst@customer\.example/)).toBeTruthy();
  expect(screen.queryByText(/access_denied/)).toBeNull();

  uninstallFacade();
});

// Closing the browser leaves nothing to return to the loopback listener, so
// the person cancels the sign-in from the panel. The cancel names the sign-in
// and nothing else, and the panel is ready to sign in again.
test("HubPanel cancels a sign-in whose browser never returns", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: async () => signedOutStatus,
    StartHubAuth: async () => ({ state: "completed", auth_url: "https://idp.customer.example/authorize", port: 1 }),
    Cancel: async () => undefined,
  });
  const signIn = facade.park("CompleteHubAuth");

  render(<HubPanel />);
  await user.click(await screen.findByRole("button", { name: /Sign in with Customer IdP/i }));
  await user.click(await screen.findByRole("button", { name: "Cancel sign-in" }));
  expect(facade.oneCall("Cancel")).toEqual(["hub-sign-in"]);

  signIn.resolve({ state: "cancelled", connected: false, authenticated: false, reason: "sign-in was cancelled" });
  expect(await screen.findByText("sign-in was cancelled")).toBeTruthy();
  expect(screen.getByText(/Connected \(https:\/\/hub\.customer\.example:8443\)/)).toBeTruthy();
  expect(screen.getByRole("button", { name: /Sign in with Customer IdP/i }).hasAttribute("disabled")).toBe(false);
  expect(facade.callsTo("StartHubAuth").length).toBe(1);
  expect(facade.callsTo("CompleteHubAuth").length).toBe(1);
  expect(facade.callsTo("HubStatus").length).toBe(1);

  uninstallFacade();
});

// A remembered configuration that no longer validates (#335) is shown when the
// panel opens: which configuration it was and why it cannot be used, with
// choosing one again as the way back. It is neither diagnosed nor connected
// to, and the panel asks nothing of the hub on the person's behalf.
test("HubPanel shows a remembered configuration that no longer validates and recovers when one is chosen again", async () => {
  const user = userEvent.setup();
  const remembered: HubResult = {
    state: "failed",
    connected: false,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    reason:
      "the remembered hub configuration no longer validates (hub endpoint must be a valid https URL with host and optional port); choose a hub configuration again",
  };
  const chosen: HubResult = {
    state: "completed",
    connected: false,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.customer.example:8443",
  };
  const facade = installFacade({
    HubStatus: async () => remembered,
    ChooseHubConfig: async () => chosen,
  });

  render(<HubPanel />);
  expect(await screen.findByText(/^the remembered hub configuration no longer validates \(hub endpoint must be/)).toBeTruthy();
  expect(screen.getByText("Configuration: /etc/readmit/hub-client.json")).toBeTruthy();
  expect(screen.getByText(/Offline \/ Local Mode/)).toBeTruthy();
  expect(screen.getByRole("button", { name: /Diagnose prerequisites/i }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("button", { name: /Connect to hub/i }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("button", { name: /Choose hub configuration…/i }).hasAttribute("disabled")).toBe(false);

  await user.click(screen.getByRole("button", { name: /Choose hub configuration…/i }));
  expect(facade.callsTo("ChooseHubConfig").length).toBe(1);
  await waitFor(() => expect(screen.queryByText(/no longer validates/)).toBeNull());
  expect(screen.getByText("Configuration: /etc/readmit/hub-client.json")).toBeTruthy();
  expect(screen.getByRole("button", { name: /Diagnose prerequisites/i }).hasAttribute("disabled")).toBe(false);
  expect(screen.getByRole("button", { name: /Connect to hub/i }).hasAttribute("disabled")).toBe(false);
  // Only the person's own choice was acted on.
  expect(facade.calls.map((call) => call.method).filter((method) => method !== "HubStatus")).toEqual(["ChooseHubConfig"]);

  uninstallFacade();
});

// A chosen configuration the application cannot remember (#364) is refused,
// like any other refused choice, and the panel says why beside the selection
// the application kept: the configuration already selected stays shown and
// usable, and choosing again is the way back. Nothing is refreshed on the
// person's behalf.
test.each([
  [
    "a chosen one cannot be remembered",
    "cannot retain the hub configuration selection, so the selection is unchanged; choose a hub configuration again",
  ],
  ["a chosen folder holds no configuration", "the chosen folder does not contain hub-client.json"],
])("HubPanel keeps the selected configuration and says why when %s", async (_, refused) => {
  const user = userEvent.setup();
  const selected: HubResult = {
    state: "completed",
    connected: false,
    authenticated: false,
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.customer.example:8443",
  };
  const facade = installFacade({
    HubStatus: async () => selected,
    ChooseHubConfig: async () => ({ state: "failed", connected: false, authenticated: false, reason: refused }),
  });

  render(<HubPanel />);
  expect(await screen.findByText("Configuration: /etc/readmit/hub-client.json")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: /Choose hub configuration…/i }));
  expect(await screen.findByText(refused)).toBeTruthy();
  expect(screen.getByText("Configuration: /etc/readmit/hub-client.json")).toBeTruthy();
  expect(screen.queryByText(/No configuration file selected/)).toBeNull();
  expect(screen.getByRole("button", { name: /Diagnose prerequisites/i }).hasAttribute("disabled")).toBe(false);
  expect(screen.getByRole("button", { name: /Connect to hub/i }).hasAttribute("disabled")).toBe(false);
  expect(screen.getByRole("button", { name: /Choose hub configuration…/i }).hasAttribute("disabled")).toBe(false);

  // Choosing again reaches the application, and a choice it remembers
  // replaces what was selected and the refusal with it.
  facade.reply({
    ChooseHubConfig: async () => ({ ...selected, config_path: "/etc/readmit/replacement/hub-client.json" }),
  });
  await user.click(screen.getByRole("button", { name: /Choose hub configuration…/i }));
  expect(await screen.findByText("Configuration: /etc/readmit/replacement/hub-client.json")).toBeTruthy();
  expect(screen.queryByText(refused)).toBeNull();
  expect(facade.calls.map((call) => call.method)).toEqual(["HubStatus", "ChooseHubConfig", "ChooseHubConfig"]);

  uninstallFacade();
});

// Cancelling a new choice while a remembered configuration no longer
// validates (#364) leaves the panel as it was: the configuration and why it
// cannot be used stay shown, without refreshing, and choosing again is still
// the way back.
test("HubPanel keeps a remembered configuration's reason when choosing another is cancelled", async () => {
  const user = userEvent.setup();
  const stale =
    "the remembered hub configuration is no longer there; choose a hub configuration again";
  const facade = installFacade({
    HubStatus: async () => ({
      state: "failed",
      connected: false,
      authenticated: false,
      config_path: "/etc/readmit/hub-client.json",
      reason: stale,
    }),
    ChooseHubConfig: async () => ({
      state: "cancelled",
      connected: false,
      authenticated: false,
      reason: "no configuration file was chosen",
    }),
  });

  render(<HubPanel />);
  expect(await screen.findByText(stale)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: /Choose hub configuration…/i }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: /Choose hub configuration…/i }).hasAttribute("disabled")).toBe(false),
  );
  expect(facade.callsTo("ChooseHubConfig").length).toBe(1);
  expect(screen.getByText(stale)).toBeTruthy();
  expect(screen.getByText("Configuration: /etc/readmit/hub-client.json")).toBeTruthy();
  expect(screen.queryByText("no configuration file was chosen")).toBeNull();
  expect(screen.getByRole("button", { name: /Diagnose prerequisites/i }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("button", { name: /Connect to hub/i }).hasAttribute("disabled")).toBe(true);
  expect(facade.callsTo("HubStatus").length).toBe(1);

  uninstallFacade();
});

// Publishing is authoring, so the application admits the author against the
// activated license before anything is sent (#311). An upload it does not
// admit is shown refused with the reason, and nothing is refreshed on the
// person's behalf; once admitted, the published digest is shown and the
// project's artifacts are read again. Publishing is reachable from the
// keyboard: the source field, then the button.
test("HubPanel shows an upload the license does not admit as refused with why, and publishes from the keyboard once admitted", async () => {
  const user = userEvent.setup();
  const admission = "operation activation is missing or invalid; select and activate an operation policy";
  const published = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2";
  const answers: HubTransferResult[] = [
    { state: "permission_denied", reason: admission },
    { state: "completed", transfer_state: "completed", digest: published, size: 43, path: "/workspace-under-test/evidence.txt" },
  ];
  const facade = installFacade({
    HubStatus: async () => defaultHubResult(),
    ListHubProjectArtifacts: async () => defaultArtifactsResult(),
    UploadHubArtifact: async () => answers.shift()!,
  });

  render(<HubPanel />);
  await user.click(await screen.findByRole("button", { name: /View Project Artifacts/i }));
  const publish = await screen.findByRole("button", { name: "Publish Artifact" });
  expect(publish.hasAttribute("disabled")).toBe(true);

  await user.type(screen.getByLabelText("Upload source path:"), "/workspace-under-test/evidence.txt");
  await user.tab();
  expect(document.activeElement).toBe(publish);
  await user.keyboard("{Enter}");
  expect(await screen.findByText(admission)).toBeTruthy();
  expect(screen.getByText(/Transfer state:/).textContent).toBe("Transfer state: permission_denied");
  expect(screen.queryByText(published)).toBeNull();
  expect(facade.callsTo("ListHubProjectArtifacts").length).toBe(1);

  await waitFor(() => expect(publish.hasAttribute("disabled")).toBe(false));
  await user.click(screen.getByLabelText("Upload source path:"));
  await user.tab();
  expect(document.activeElement).toBe(publish);
  await user.keyboard("[Space]");
  expect(await screen.findByText(published)).toBeTruthy();
  expect(screen.queryByText(admission)).toBeNull();
  expect(facade.callsTo("UploadHubArtifact").map((call) => call.args[0])).toEqual([
    { project: "cardio-icu", source_path: "/workspace-under-test/evidence.txt" },
    { project: "cardio-icu", source_path: "/workspace-under-test/evidence.txt" },
  ]);
  await waitFor(() => expect(facade.callsTo("ListHubProjectArtifacts").length).toBe(2));

  uninstallFacade();
});
