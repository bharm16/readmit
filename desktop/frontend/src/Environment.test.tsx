import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
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
  expect(screen.getByDisplayValue("peer-under-test")).toBeTruthy();

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
  expect(await screen.findByText("approved-peer")).toBeTruthy();
  expect(screen.getByText("second-peer")).toBeTruthy();

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
      address="peer-under-test"
      transport="mllp"
    />,
  );

  expect(screen.getByText(/Environment: staging-mllp/i)).toBeTruthy();
  expect(screen.getAllByText(/nonproduction/i).length).toBeGreaterThan(0);
  expect(screen.getByText(/peer-under-test/i)).toBeTruthy();

  // Rerender with production classification to verify warning alert
  rerender(
    <EnvironmentBanner
      name="prod-endpoint"
      classification="production"
      address="production-peer"
      transport="mllp"
    />,
  );

  expect(screen.getByText(/Environment: prod-endpoint/i)).toBeTruthy();
  expect(screen.getAllByText(/production/i).length).toBeGreaterThan(0);
  expect(screen.getByText(/Refusal: Production targets reject all sends and resets/i)).toBeTruthy();
});

test("an unfinished target draft is kept and transport approval stays off until chosen", async () => {
  const user = userEvent.setup();
  const base = defaultTargetResult().target;
  if (!base) throw new Error("fixture target missing");
  const saved = defaultTargetResult({
    target: { ...base, name: "from-file", approved_transport: true },
  });
  let savedRequest: unknown;
  installFacade({
    ReadTarget: async () => saved,
    SaveTarget: async (request) => {
      savedRequest = request;
      return saved;
    },
    ReadSendPolicy: async () => defaultSendPolicyResult(),
    ReadSecrets: async () => defaultSecretsResult(),
    ReadResetPlan: async () => defaultResetPlanResult(),
  });

  const draftTarget = { ...base, name: "from-draft", approved_transport: false, connect_timeout: "3s" };
  render(
    <EnvironmentPanel
      workspace={WORKSPACE_ROOT}
      targetFile="targets/default.json"
      secretsFile="secrets.json"
      policyFile="send-policy.json"
      planFile="reset-plan.json"
      drafts={[
        {
          id: "draft-target",
          kind: "environment/target",
          workspace: WORKSPACE_ROOT,
          case: "",
          identity: "",
          content_schema: "readmit-target-draft/v1",
          content: draftTarget,
        },
      ]}
    />,
  );

  expect(await screen.findByDisplayValue("from-draft")).toBeTruthy();
  expect(screen.queryByDisplayValue("from-file")).toBeNull();
  const approval = screen.getByLabelText("Approved transport") as HTMLInputElement;
  expect(approval.checked).toBe(false);
  expect(screen.getByDisplayValue("3s")).toBeTruthy();

  await user.click(approval);
  await user.click(screen.getByRole("button", { name: "Save Target Configuration" }));
  expect(savedRequest).toMatchObject({ target: { approved_transport: true, name: "from-draft" } });
  uninstallFacade();
});

test("an overdue credential reference says so instead of claiming it is current", async () => {
  const secrets = defaultSecretsResult();
  const document = secrets.document;
  const reference = document?.references[0];
  if (!document || !reference) throw new Error("fixture secret missing");
  document.references[0] = {
    ...reference,
    max_age: "1s",
    rotated_at: "2020-01-01T00:00:00Z",
  };
  installFacade({
    ReadTarget: async () => defaultTargetResult(),
    ReadSendPolicy: async () => defaultSendPolicyResult(),
    ReadSecrets: async () => secrets,
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
  expect(await screen.findByText("Overdue — rotate before use")).toBeTruthy();
  uninstallFacade();
});

test("the forms stay closed until every document read has answered, so a late read never replaces what was typed", async () => {
  const facade = installFacade({
    ReadSendPolicy: async () => defaultSendPolicyResult(),
    ReadSecrets: async () => defaultSecretsResult(),
    ReadResetPlan: async () => defaultResetPlanResult(),
  });
  // The target read is still in flight after the other three answered.
  const reading = facade.park("ReadTarget");
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
  await waitFor(() => expect(facade.callsTo("ReadResetPlan").length).toBe(1));
  await waitFor(() => expect(facade.callsTo("ReadSendPolicy").length).toBe(1));
  const name = screen.getByLabelText("Environment Name") as HTMLInputElement;
  // Nothing can be typed while the document it would replace is being read.
  expect(name.disabled).toBe(true);
  reading.resolve(defaultTargetResult());
  expect(await screen.findByDisplayValue("staging-mllp")).toBeTruthy();
  expect(name.disabled).toBe(false);
});

test("a target file name is typed in full while documents are read, and naming it re-reads only the target", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ReadTarget: async () => defaultTargetResult(),
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
  await screen.findByDisplayValue("staging-mllp");
  const file = screen.getByLabelText("Target Config File");
  await user.clear(file);
  await user.type(file, "downstream-target.json");
  // Every keystroke landed: the field is not closed by the read it starts.
  expect((file as HTMLInputElement).value).toBe("downstream-target.json");
  await waitFor(() => expect(facade.callsTo("ReadTarget").at(-1)?.args).toEqual([WORKSPACE_ROOT, "downstream-target.json"]));
  // Naming a target does not read the other three documents again.
  expect(facade.callsTo("ReadSecrets")).toHaveLength(1);
  expect(facade.callsTo("ReadSendPolicy")).toHaveLength(1);
  expect(facade.callsTo("ReadResetPlan")).toHaveLength(1);
});
