import { expect, test } from "vitest";
import { render, screen } from "@testing-library/react";
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

  uninstallFacade();
});
