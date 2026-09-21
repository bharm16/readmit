import { expect, test } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EnvironmentPanel, EnvironmentBanner } from "./EnvironmentPanel";
import { installFacade, uninstallFacade } from "./testkit/wails";
import {
  WORKSPACE_ROOT,
  defaultTargetResult,
  defaultTargetCheckResult,
  defaultSecretsResult,
  defaultSecretTestResult,
  defaultSecretScanResult,
  defaultSendPolicyResult,
  defaultSendPolicyEvalResult,
  defaultResetPlanResult,
  defaultTargetResetResult,
} from "./testkit/fixtures";

test("EnvironmentPanel displays target configuration and runs deliberate diagnostics", async () => {
  const user = userEvent.setup();
  const targetData = defaultTargetResult();
  const checkData = defaultTargetCheckResult();

  const facade = installFacade({
    ReadTarget: async () => targetData,
    SaveTarget: async () => targetData,
    CheckTarget: async () => checkData,
    ReadSendPolicy: async () => defaultSendPolicyResult(),
    ReadSecrets: async () => defaultSecretsResult(),
    ReadResetPlan: async () => defaultResetPlanResult(),
  });

  render(
    <EnvironmentPanel
      workspace={WORKSPACE_ROOT}
      targetFile="targets/default.json"
      secretsFile="secrets.json"
      policyFile="send-policy.json"
      planFile="reset-plan.json"
      initialTab="target"
    />,
  );

  // Initial read calls
  expect(facade.callsTo("ReadTarget").length).toBe(1);
  expect(await screen.findByDisplayValue("staging-mllp")).toBeTruthy();
  expect(screen.getByDisplayValue("127.0.0.1:2575")).toBeTruthy();

  // Run deliberate diagnostics
  const diagBtn = screen.getByRole("button", { name: /Check Target Reachability & TLS/i });
  await user.click(diagBtn);

  expect(facade.callsTo("CheckTarget").length).toBe(1);
  expect(await screen.findByText(/Diagnostic Report/i)).toBeTruthy();
  expect(screen.getByText(/established/i)).toBeTruthy();

  uninstallFacade();
});

test("EnvironmentPanel manages credential references, tests, rotates and scans", async () => {
  const user = userEvent.setup();
  const secretsData = defaultSecretsResult();
  const testData = defaultSecretTestResult();
  const scanData = defaultSecretScanResult();

  const facade = installFacade({
    ReadTarget: async () => defaultTargetResult(),
    ReadSendPolicy: async () => defaultSendPolicyResult(),
    ReadSecrets: async () => secretsData,
    TestSecretReference: async () => testData,
    RotateSecretReference: async () => secretsData,
    RemoveSecretReference: async () => secretsData,
    ScanSecrets: async () => scanData,
    ReadResetPlan: async () => defaultResetPlanResult(),
  });

  render(
    <EnvironmentPanel
      workspace={WORKSPACE_ROOT}
      targetFile="targets/default.json"
      secretsFile="secrets.json"
      policyFile="send-policy.json"
      planFile="reset-plan.json"
      initialTab="secrets"
    />,
  );

  expect(facade.callsTo("ReadSecrets").length).toBe(1);
  expect(await screen.findByText("mllp-basic-auth")).toBeTruthy();
  // Masking of secret reference details
  expect(screen.getByText("••••••••")).toBeTruthy();

  // Test secret resolution
  const testBtn = screen.getByRole("button", { name: /^Test$/i });
  await user.click(testBtn);
  expect(facade.callsTo("TestSecretReference").length).toBe(1);
  expect(await screen.findByText(/Success \(Resolved in memory\)/i)).toBeTruthy();

  // Rotate secret reference
  const rotateBtn = screen.getByRole("button", { name: /^Rotate$/i });
  await user.click(rotateBtn);
  expect(facade.callsTo("RotateSecretReference").length).toBe(1);

  // Scan workspace for residual secrets
  const scanBtn = screen.getByRole("button", { name: /Scan Workspace for Residual Leaks/i });
  await user.click(scanBtn);
  expect(facade.callsTo("ScanSecrets").length).toBe(1);
  expect(await screen.findByText(/Files Checked: 5/i)).toBeTruthy();

  // Remove secret reference
  const removeBtn = screen.getByRole("button", { name: /^Remove$/i });
  await user.click(removeBtn);
  expect(facade.callsTo("RemoveSecretReference").length).toBe(1);

  uninstallFacade();
});

test("EnvironmentPanel authors send policy and evaluates local destinations", async () => {
  const user = userEvent.setup();
  const policyData = defaultSendPolicyResult();
  const evalData = defaultSendPolicyEvalResult();

  const facade = installFacade({
    ReadTarget: async () => defaultTargetResult(),
    ReadSendPolicy: async () => policyData,
    SaveSendPolicy: async () => policyData,
    EvaluateSendPolicy: async () => evalData,
    ReadSecrets: async () => defaultSecretsResult(),
    ReadResetPlan: async () => defaultResetPlanResult(),
  });

  render(
    <EnvironmentPanel
      workspace={WORKSPACE_ROOT}
      targetFile="targets/default.json"
      secretsFile="secrets.json"
      policyFile="send-policy.json"
      planFile="reset-plan.json"
      initialTab="policy"
    />,
  );

  expect(facade.callsTo("ReadSendPolicy").length).toBe(1);
  expect(await screen.findByText("127.0.0.1:2575")).toBeTruthy();
  expect(screen.getByText("10.0.0.5:2575")).toBeTruthy();

  // Evaluate destination locally
  const evalBtn = screen.getByRole("button", { name: /Evaluate Destination Locally/i });
  await user.click(evalBtn);

  expect(facade.callsTo("EvaluateSendPolicy").length).toBe(1);
  expect(await screen.findByText(/Local Policy Decision/i)).toBeTruthy();
  expect(screen.getByText("ALLOWED")).toBeTruthy();

  uninstallFacade();
});

test("EnvironmentPanel authors fixture reset plan and executes with deliberate confirmation", async () => {
  const user = userEvent.setup();
  const planData = defaultResetPlanResult();
  const resetData = defaultTargetResetResult();

  const facade = installFacade({
    ReadTarget: async () => defaultTargetResult(),
    ReadSendPolicy: async () => defaultSendPolicyResult(),
    ReadSecrets: async () => defaultSecretsResult(),
    ReadResetPlan: async () => planData,
    SaveResetPlan: async () => planData,
    ResetTarget: async () => resetData,
  });

  render(
    <EnvironmentPanel
      workspace={WORKSPACE_ROOT}
      targetFile="targets/default.json"
      secretsFile="secrets.json"
      policyFile="send-policy.json"
      planFile="reset-plan.json"
      initialTab="reset"
    />,
  );

  expect(facade.callsTo("ReadResetPlan").length).toBe(1);
  expect(await screen.findByText("Confirm patient database is wiped.")).toBeTruthy();

  // Confirm step 1 checkbox
  const confirmBox = screen.getByRole("checkbox");
  await user.click(confirmBox);

  // Click deliberate reset execution
  const executeBtn = screen.getByRole("button", { name: /Execute Fixture Reset/i });
  await user.click(executeBtn);

  expect(facade.callsTo("ResetTarget").length).toBe(1);
  expect(await screen.findByText(/Reset Outcome: succeeded/i)).toBeTruthy();
  expect(screen.getByText(/all reset actions executed successfully/i)).toBeTruthy();

  uninstallFacade();
});

test("EnvironmentBanner displays target details and warns on dangerous classification", () => {
  const { rerender } = render(
    <EnvironmentBanner
      name="staging-mllp"
      classification="nonproduction"
      address="127.0.0.1:2575"
      transport="mllp"
    />,
  );

  expect(screen.getByText(/Environment: staging-mllp/i)).toBeTruthy();
  expect(screen.getAllByText(/nonproduction/i).length).toBeGreaterThan(0);
  expect(screen.getByText(/127\.0\.0\.1:2575/i)).toBeTruthy();

  // Rerender with production classification to verify warning alert
  rerender(
    <EnvironmentBanner
      name="prod-endpoint"
      classification="production"
      address="prod.hospital.internal:2575"
      transport="mllp"
    />,
  );

  expect(screen.getByText(/Environment: prod-endpoint/i)).toBeTruthy();
  expect(screen.getAllByText(/production/i).length).toBeGreaterThan(0);
  expect(screen.getByText(/Refusal: Production targets reject all sends and resets/i)).toBeTruthy();
});
