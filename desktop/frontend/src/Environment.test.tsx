import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { EnvironmentPanel, EnvironmentBanner } from "./EnvironmentPanel";
import { IndicatorsContext } from "./lifecycle";
import { installFacade, uninstallFacade } from "./testkit/wails";
import type { SecretChange, SecretSaveRequest, SendPolicySaveRequest, State } from "./bindings";
import {
  WORKSPACE_ROOT,
  indicatorTable,
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
  const diagBtn = screen.getByRole("button", { name: /Test connection/i });
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
  const scanBtn = screen.getByRole("button", { name: /Scan for leaks/i });
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
  const evalBtn = screen.getByRole("button", { name: /Check destination/i });
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
  const executeBtn = screen.getByRole("button", { name: /Reset fixture/i });
  await user.click(executeBtn);

  expect(facade.callsTo("ResetTarget").length).toBe(1);
  expect(await screen.findByText(/Reset Outcome: confirmed/i)).toBeTruthy();
  expect(screen.getByText(/every_action_confirmed/i)).toBeTruthy();
  // A reset that passed reads as passed: its state is a run state, never an
  // operation state.
  expect(screen.getByLabelText("Fixture reset execution outcome").classList.contains("passed")).toBe(true);

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
  await user.click(screen.getByRole("button", { name: "Save target" }));
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

// The identity a save reports: the SHA-256 of the bytes it wrote. The stub
// answers with a stand-in of the same shape.
const WRITTEN = "0123456789abcdef".repeat(4);

function renderPanel(initialTab: "target" | "secrets" | "policy" | "reset") {
  render(
    <EnvironmentPanel
      workspace={WORKSPACE_ROOT}
      targetFile="targets/default.json"
      secretsFile="secrets.json"
      policyFile="send-policy.json"
      planFile="reset-plan.json"
      initialTab={initialTab}
    />,
  );
}

const readsAnswered = {
  ReadTarget: async () => defaultTargetResult(),
  ReadSendPolicy: async () => defaultSendPolicyResult(),
  ReadSecrets: async () => defaultSecretsResult(),
  ReadResetPlan: async () => defaultResetPlanResult(),
};

test("a registered reference is edited through the shared update, and its locator arguments are counted, never shown", async () => {
  const user = userEvent.setup();
  const registered = defaultSecretsResult().document?.references[0];
  if (!registered) throw new Error("fixture secret missing");
  const facade = installFacade({
    ...readsAnswered,
    SaveSecretReference: async (request) => ({
      state: "completed",
      secrets_file: request.secrets_file,
      document: { schema: "readmit-secrets/v1", references: [{ ...request.reference, ...request.change }] },
      identity: WRITTEN,
    }),
  });
  renderPanel("secrets");

  const row = within((await screen.findByText("mllp-basic-auth")).closest("tr") as HTMLElement);
  // The locator arguments are counted, as `readmit secret show` counts them.
  expect(row.getByText(/\(1 locator arguments\)/)).toBeTruthy();
  expect(screen.queryByText(/named-reference/)).toBeNull();

  await user.click(row.getByRole("button", { name: "Edit mllp-basic-auth" }));
  const form = within(screen.getByRole("form", { name: "Edit credential reference mllp-basic-auth" }));
  // One form at a time: the registration's fields are not beside the edit's.
  expect(screen.queryByRole("button", { name: "Add credential reference" })).toBeNull();
  expect(screen.getAllByLabelText("Target Address Constraint")).toHaveLength(1);
  // Focus moves into the edit, and the registered arguments are not shown there either.
  expect(document.activeElement).toBe(form.getByLabelText("Store"));
  expect(screen.queryByText(/named-reference/)).toBeNull();
  expect(form.getByText(/Purpose: mllp-endpoint\./)).toBeTruthy();

  await user.selectOptions(form.getByLabelText("Store"), "customer-managed");
  await user.clear(form.getByLabelText("Target Address Constraint"));
  await user.type(form.getByLabelText("Target Address Constraint"), "second-peer");
  await user.clear(form.getByLabelText("Locator Command (Path)"));
  await user.type(form.getByLabelText("Locator Command (Path)"), "second-locator");
  await user.clear(form.getByLabelText("Maximum Rotation Age"));
  await user.click(form.getByLabelText("Replace the 1 registered locator arguments"));
  await user.type(form.getByLabelText(/Replacement Locator Arguments/), " first reference {Enter}{Enter}second");
  await user.click(form.getByRole("button", { name: "Save changes" }));

  await waitFor(() => expect(facade.callsTo("SaveSecretReference")).toHaveLength(1));
  const request = facade.callsTo("SaveSecretReference")[0]?.args[0] as SecretSaveRequest;
  expect(request.is_update).toBe(true);
  // The update names the reference it was opened on and changes what the
  // person changed: each argument line trimmed, and a blank line no argument.
  expect(request.reference).toEqual(registered);
  const expected: SecretChange = {
    store: "customer-managed",
    address: "second-peer",
    command: "second-locator",
    max_age: "",
    arguments: ["first reference", "second"],
  };
  expect(request.change).toEqual(expected);
  expect(await screen.findByText("Credential reference mllp-basic-auth updated.")).toBeTruthy();
  expect(screen.getByText(`${WRITTEN}`, { selector: "code" })).toBeTruthy();
  expect(screen.queryByRole("form", { name: /Edit credential reference/ })).toBeNull();
  // Focus returns to the control that opened the edit.
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Edit mllp-basic-auth" })));
  uninstallFacade();
});

test("an edit keeps its registered arguments unless they are replaced, stays open with what was typed when refused, and a cancelled edit writes nothing", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...readsAnswered,
    SaveSecretReference: async () => ({ state: "failed", reason: "reference address: must be an explicit host and numeric port" }),
  });
  renderPanel("secrets");
  await user.click(await screen.findByRole("button", { name: "Edit mllp-basic-auth" }));
  const form = within(screen.getByRole("form", { name: "Edit credential reference mllp-basic-auth" }));
  await user.clear(form.getByLabelText("Target Address Constraint"));
  await user.type(form.getByLabelText("Target Address Constraint"), "no-port");
  await user.click(form.getByRole("button", { name: "Save changes" }));

  expect(await screen.findByText("reference address: must be an explicit host and numeric port")).toBeTruthy();
  // Only the member the person changed is sent: the registered arguments and
  // maximum age are kept as recorded, whatever else changed them meanwhile.
  const request = facade.callsTo("SaveSecretReference")[0]?.args[0] as SecretSaveRequest;
  expect(request.change).toEqual({ address: "no-port" });
  // The refusal is recoverable: the edit is still open with what was typed,
  // and no identity is claimed for a write that did not happen.
  expect((form.getByLabelText("Target Address Constraint") as HTMLInputElement).value).toBe("no-port");
  expect(screen.queryByText(/^Written to /)).toBeNull();

  await user.click(form.getByRole("button", { name: "Cancel Editing" }));
  expect(screen.getByText("Edit of mllp-basic-auth cancelled; nothing was written.")).toBeTruthy();
  expect(screen.queryByRole("form", { name: /Edit credential reference/ })).toBeNull();
  expect(facade.callsTo("SaveSecretReference")).toHaveLength(1);
  uninstallFacade();
});

test("an edit is opened, cancelled with Escape and saved with Enter from the keyboard alone", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...readsAnswered,
    SaveSecretReference: async (request) => ({
      state: "completed",
      document: { schema: "readmit-secrets/v1", references: [request.reference] },
      identity: WRITTEN,
    }),
  });
  renderPanel("secrets");
  const edit = await screen.findByRole("button", { name: "Edit mllp-basic-auth" });
  await waitFor(() => expect((edit as HTMLButtonElement).disabled).toBe(false));
  // Tab reaches the control; Enter opens the edit with focus inside it.
  for (let step = 0; step < 60 && document.activeElement !== edit; step++) await user.tab();
  expect(document.activeElement).toBe(edit);
  await user.keyboard("{Enter}");
  expect(document.activeElement).toBe(screen.getByLabelText("Store", { selector: "#edit-secret-store" }));
  // The window's own Escape cancels a running operation; the Escape that
  // cancels an edit never reaches it.
  const reachedWindow: string[] = [];
  const listener = (event: KeyboardEvent) => reachedWindow.push(event.key);
  window.addEventListener("keydown", listener);
  await user.keyboard("{Escape}");
  window.removeEventListener("keydown", listener);
  expect(reachedWindow).toEqual([]);
  expect(screen.queryByRole("form", { name: /Edit credential reference/ })).toBeNull();
  await waitFor(() => expect(document.activeElement).toBe(edit));
  expect(facade.callsTo("SaveSecretReference")).toHaveLength(0);

  await user.keyboard("{Enter}");
  await user.tab();
  expect(document.activeElement).toBe(screen.getByLabelText("Target Address Constraint", { selector: "#edit-secret-address" }));
  await user.keyboard("{Control>}a{/Control}second-peer{Enter}");
  await waitFor(() => expect(facade.callsTo("SaveSecretReference")).toHaveLength(1));
  expect((facade.callsTo("SaveSecretReference")[0]?.args[0] as SecretSaveRequest).change).toEqual({ address: "second-peer" });
  expect(await screen.findByText("Credential reference mllp-basic-auth updated.")).toBeTruthy();
  uninstallFacade();
});

test("a registration sends one locator argument per line and its maximum age, and a duplicate is refused with what was typed kept", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...readsAnswered,
    SaveSecretReference: async () => ({ state: "failed", reason: "that name is already registered in this store" }),
  });
  renderPanel("secrets");
  await screen.findByText("mllp-basic-auth");
  await user.type(screen.getByLabelText("Reference Name"), "mllp-basic-auth");
  await user.type(screen.getByLabelText("Target Address Constraint", { selector: "#secret-address" }), "peer-under-test");
  await user.type(screen.getByLabelText("Locator Command (Path)", { selector: "#secret-command" }), "locator");
  await user.type(screen.getByLabelText("Locator Arguments (one per line)"), "named reference{Enter}-w");
  await user.type(screen.getByLabelText("Maximum Rotation Age", { selector: "#secret-max-age" }), "720h");
  await user.click(screen.getByRole("button", { name: "Add credential reference" }));

  expect(await screen.findByText("that name is already registered in this store")).toBeTruthy();
  const request = facade.callsTo("SaveSecretReference")[0]?.args[0] as SecretSaveRequest;
  expect(request.is_update).toBe(false);
  expect(request.reference.arguments).toEqual(["named reference", "-w"]);
  expect(request.reference.max_age).toBe("720h");
  expect((screen.getByLabelText("Reference Name") as HTMLInputElement).value).toBe("mllp-basic-auth");
  expect(screen.queryByText(/^Written to /)).toBeNull();
  uninstallFacade();
});

test("a destination prefix is added with Enter, a refused policy claims no identity, and a saved one shows the identity it was written under", async () => {
  const user = userEvent.setup();
  let answer: "refuse" | "save" = "refuse";
  const facade = installFacade({
    ...readsAnswered,
    SaveSendPolicy: async (request) =>
      answer === "refuse"
        ? { state: "failed", reason: "every approved destination is one CIDR prefix in canonical masked form, such as 127.0.0.0/8 or 10.1.0.0/16" }
        : { state: "completed", policy: request.policy, policy_file: request.policy_file, identity: WRITTEN },
  });
  renderPanel("policy");
  await screen.findByText("approved-peer");
  await user.type(screen.getByLabelText("Approved destination prefix"), "third-peer{Enter}");
  expect(await screen.findByText("third-peer")).toBeTruthy();
  expect((screen.getByLabelText("Approved destination prefix") as HTMLInputElement).value).toBe("");

  await user.click(screen.getByRole("button", { name: "Save policy" }));
  expect(await screen.findByText(/canonical masked form/)).toBeTruthy();
  expect(screen.queryByText(/^Written to /)).toBeNull();
  // The refused draft is kept for the person to correct.
  expect(screen.getByText("third-peer")).toBeTruthy();

  answer = "save";
  await user.click(screen.getByRole("button", { name: "Save policy" }));
  expect(await screen.findByText("Approved-destination policy saved.")).toBeTruthy();
  expect(screen.getByText(WRITTEN, { selector: "code" })).toBeTruthy();
  const saved = facade.callsTo("SaveSendPolicy")[1]?.args[0] as SendPolicySaveRequest;
  expect(saved.policy.approved_destinations).toEqual(["approved-peer", "second-peer", "third-peer"]);
  uninstallFacade();
});

test("a saved reset plan shows the identity it was written under", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...readsAnswered,
    SaveResetPlan: async (request) => ({ state: "completed", plan: request.plan, plan_file: request.plan_file, identity: WRITTEN }),
  });
  renderPanel("reset");
  await screen.findByText("Confirm patient database is wiped.");
  await user.type(screen.getByLabelText("Action ID"), "step-3");
  await user.type(screen.getByLabelText("Reset instructions"), "Confirm the receiver is stopped.");
  await user.click(screen.getByRole("button", { name: "Add action" }));
  await user.click(screen.getByRole("button", { name: "Save plan" }));
  expect(await screen.findByText("Fixture reset plan saved.")).toBeTruthy();
  expect(screen.getByText(WRITTEN, { selector: "code" })).toBeTruthy();
  expect(screen.getByText("reset-plan.json", { selector: "code" })).toBeTruthy();
  expect(facade.callsTo("SaveResetPlan")).toHaveLength(1);
  uninstallFacade();
});

// An action the facade did not complete says which of the six states it
// answered, with its word and shape, and not only its reason: a busy window, a
// cancelled check and an account without permission are not failures.
test("an action that did not complete says which state it answered as well as why", async () => {
  const user = userEvent.setup();
  const facade = installFacade(readsAnswered);
  const indicators = indicatorTable();
  render(
    <IndicatorsContext.Provider value={indicators}>
      <EnvironmentPanel
        workspace={WORKSPACE_ROOT}
        targetFile="targets/default.json"
        secretsFile="secrets.json"
        policyFile="send-policy.json"
        planFile="reset-plan.json"
        initialTab="target"
      />
    </IndicatorsContext.Provider>,
  );
  const check = await screen.findByRole("button", { name: /Test connection/i });
  await waitFor(() => expect((check as HTMLButtonElement).disabled).toBe(false));
  const states: State[] = ["empty", "busy", "cancelled", "permission_denied", "failed"];
  for (const state of states) {
    facade.reply({ CheckTarget: async () => ({ state, reason: `the check answered ${state}` }) });
    await user.click(check);
    const status = (await screen.findByText(`the check answered ${state}`)).closest("p");
    expect(status?.className).toBe(`status status-${state}`);
    expect(within(status as HTMLElement).getByText(indicators.get(state)?.label ?? "")).toBeTruthy();
  }
  uninstallFacade();
});

test("focus returns to the control that started a save once its refusal answers, after the disabled form took it", async () => {
  const user = userEvent.setup();
  const facade = installFacade(readsAnswered);
  const saving = facade.park("SaveSecretReference");
  renderPanel("secrets");
  await user.click(await screen.findByRole("button", { name: "Edit mllp-basic-auth" }));
  const form = within(screen.getByRole("form", { name: "Edit credential reference mllp-basic-auth" }));
  const address = form.getByLabelText("Target Address Constraint") as HTMLInputElement;
  await user.click(address);
  await user.keyboard("{Control>}a{/Control}no-port{Enter}");
  await waitFor(() => expect(address.disabled).toBe(true));
  // A browser moves focus off a control it disables; jsdom neither does that
  // nor blurs a disabled control, so the test does what the browser does.
  address.disabled = false;
  address.blur();
  address.disabled = true;
  expect(document.activeElement).toBe(document.body);
  saving.resolve({ state: "failed", reason: "reference address: must be an explicit host and numeric port" });
  expect(await screen.findByText("reference address: must be an explicit host and numeric port")).toBeTruthy();
  await waitFor(() => expect(document.activeElement).toBe(address));
  // The person keeps typing where they were.
  await user.keyboard("{Control>}a{/Control}peer-under-test");
  expect(address.value).toBe("peer-under-test");
  uninstallFacade();
});

test("naming another secrets document while an edit is open closes the edit where the person is typing, and every keystroke lands", async () => {
  const user = userEvent.setup();
  const facade = installFacade(readsAnswered);
  renderPanel("secrets");
  await user.click(await screen.findByRole("button", { name: "Edit mllp-basic-auth" }));
  expect(screen.getByRole("form", { name: "Edit credential reference mllp-basic-auth" })).toBeTruthy();
  const file = screen.getByLabelText("Secrets Document File") as HTMLInputElement;
  await user.click(file);
  await user.type(file, ".bak");
  expect(file.value).toBe("secrets.json.bak");
  expect(document.activeElement).toBe(file);
  expect(screen.queryByRole("form", { name: /Edit credential reference/ })).toBeNull();
  await waitFor(() => expect(facade.callsTo("ReadSecrets").at(-1)?.args).toEqual([WORKSPACE_ROOT, "secrets.json.bak"]));
  uninstallFacade();
});

test("a secrets document that cannot be read says why instead of showing the references of the one read before it", async () => {
  const user = userEvent.setup();
  installFacade({
    ...readsAnswered,
    ReadSecrets: async (_workspace, file) =>
      file === "secrets.json" ? defaultSecretsResult() : { state: "failed", reason: "invalid secret reference document" },
  });
  renderPanel("secrets");
  expect(await screen.findByText("mllp-basic-auth")).toBeTruthy();
  const file = screen.getByLabelText("Secrets Document File");
  await user.clear(file);
  await user.type(file, "notes.json");
  expect(await screen.findByText("invalid secret reference document")).toBeTruthy();
  expect(screen.queryByText("mllp-basic-auth")).toBeNull();
  expect(screen.queryByRole("button", { name: "Edit mllp-basic-auth" })).toBeNull();
  uninstallFacade();
});

test("a reference that declares no locator arguments is shown and edited, and a bound reference the document does not list stays visible", async () => {
  const user = userEvent.setup();
  const secrets = defaultSecretsResult();
  const reference = secrets.document?.references[0];
  if (!secrets.document || !reference) throw new Error("fixture secret missing");
  // A document may omit the member; the facade then carries no list at all.
  secrets.document.references[0] = { ...reference, arguments: null as unknown as string[] };
  const target = defaultTargetResult();
  if (!target.target) throw new Error("fixture target missing");
  target.target.credential = { secrets_file: `${WORKSPACE_ROOT}/secrets.json`, reference: "unlisted-reference" };
  installFacade({ ...readsAnswered, ReadSecrets: async () => secrets, ReadTarget: async () => target });
  renderPanel("target");
  const bound = (await screen.findByLabelText("Credential Reference")) as HTMLSelectElement;
  await waitFor(() => expect(bound.selectedOptions[0]?.textContent).toBe("unlisted-reference (not listed in secrets.json)"));

  await user.click(screen.getByRole("button", { name: "Credential References" }));
  const row = within((await screen.findByText("mllp-basic-auth")).closest("tr") as HTMLElement);
  expect(row.getByText(/\(0 locator arguments\)/)).toBeTruthy();
  await user.click(row.getByRole("button", { name: "Edit mllp-basic-auth" }));
  expect(screen.getByLabelText("Replace the 0 registered locator arguments")).toBeTruthy();
  uninstallFacade();
});

test("a target bound to a reference in another secrets document says so rather than showing the loaded document's reference of that name", async () => {
  const target = defaultTargetResult();
  if (!target.target) throw new Error("fixture target missing");
  target.target.credential = { secrets_file: "other-secrets.json", reference: "mllp-basic-auth" };
  const user = userEvent.setup();
  let saved: unknown;
  installFacade({ ...readsAnswered, ReadTarget: async () => target, SaveTarget: async (request) => ((saved = request), target) });
  renderPanel("target");
  const bound = (await screen.findByLabelText("Credential Reference")) as HTMLSelectElement;
  await waitFor(() => expect(bound.selectedOptions[0]?.textContent).toBe("mllp-basic-auth (bound in other-secrets.json)"));
  // The loaded document's reference of the same name is still a choice of its own.
  await user.selectOptions(bound, "mllp-basic-auth (mllp-endpoint · os-keychain)");
  await user.click(screen.getByRole("button", { name: "Save target" }));
  expect(saved).toMatchObject({ target: { credential: { secrets_file: `${WORKSPACE_ROOT}/secrets.json`, reference: "mllp-basic-auth" } } });
  uninstallFacade();
});

test.each([
  {
    tab: "policy" as const,
    fileLabel: "Policy File",
    oldContent: "approved-peer",
    refusal: "invalid send policy document",
    save: "Save policy",
    method: "SaveSendPolicy" as const,
    fresh: "New send policy",
  },
  {
    tab: "reset" as const,
    fileLabel: "Plan File",
    oldContent: "Confirm patient database is wiped.",
    refusal: "invalid fixture reset plan",
    save: "Save plan",
    method: "SaveResetPlan" as const,
    fresh: "New reset plan",
  },
])("a failed $tab read clears the prior document and cannot save it to the new file", async ({ tab, fileLabel, oldContent, refusal, save, method, fresh }) => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...readsAnswered,
    ReadSendPolicy: async (_workspace, file) => file === "send-policy.json" ? defaultSendPolicyResult() : { state: "failed", reason: "invalid send policy document" },
    ReadResetPlan: async (_workspace, file) => file === "reset-plan.json" ? defaultResetPlanResult() : { state: "failed", reason: "invalid fixture reset plan" },
  });
  renderPanel(tab);
  expect(await screen.findByText(oldContent)).toBeTruthy();
  await user.clear(screen.getByLabelText(fileLabel));
  await user.type(screen.getByLabelText(fileLabel), "unreadable.json");
  expect(await screen.findByText(refusal)).toBeTruthy();
  expect(screen.queryByText(oldContent)).toBeNull();
  expect((screen.getByRole("button", { name: save }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo(method)).toHaveLength(0);
  expect(screen.getByRole("button", { name: fresh })).toBeTruthy();
  uninstallFacade();
});

test.each(["policy", "reset"] as const)("a retained %s draft cannot bypass a failed read of the named file", async (tab) => {
  const policy = defaultSendPolicyResult().policy;
  const plan = defaultResetPlanResult().plan;
  if (!policy || !plan) throw new Error("fixture document missing");
  const facade = installFacade({
    ...readsAnswered,
    ReadSendPolicy: async () => ({ state: "failed", reason: "policy file is unreadable" }),
    ReadResetPlan: async () => ({ state: "failed", reason: "plan file is unreadable" }),
  });
  render(<EnvironmentPanel
    workspace={WORKSPACE_ROOT}
    initialTab={tab}
    drafts={[{
      id: "retained-draft",
      kind: tab === "policy" ? "environment/policy" : "environment/reset",
      workspace: WORKSPACE_ROOT,
      case: "",
      identity: "",
      content_schema: tab === "policy" ? "readmit-send-policy-draft/v1" : "readmit-reset-plan-draft/v1",
      content: tab === "policy" ? policy : plan,
    }]}
  />);
  expect(await screen.findByText(tab === "policy" ? "policy file is unreadable" : "plan file is unreadable")).toBeTruthy();
  expect(screen.queryByText(tab === "policy" ? "approved-peer" : "Confirm patient database is wiped.")).toBeNull();
  const save = screen.getByRole("button", { name: tab === "policy" ? "Save policy" : "Save plan" }) as HTMLButtonElement;
  expect(save.disabled).toBe(true);
  expect(facade.callsTo(tab === "policy" ? "SaveSendPolicy" : "SaveResetPlan")).toHaveLength(0);
  uninstallFacade();
});
