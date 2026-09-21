import { useEffect, useState, useCallback, useRef } from "react";
import "./environment.css";
import {
  saveTarget,
  readTarget,
  checkTarget,
  resetTarget,
  readSecrets,
  saveSecretReference,
  removeSecretReference,
  testSecretReference,
  rotateSecretReference,
  scanSecrets,
  readSendPolicy,
  saveSendPolicy,
  evaluateSendPolicy,
  readResetPlan,
  saveResetPlan,
  type Target,
  type TargetClassification,
  type EnvironmentReport,
  type SendPolicyDecision,
  type SecretDocument,
  type SecretReference,
  type SecretStore,
  type SecretPurpose,
  type SecretTestResult,
  type SecretScanResult,
  type SendPolicy,
  type ResetPlan,
  type ResetAction,
  type ResetOperator,
  type ResetAuthority,
  type ResetResult,
  type EditorDraft,
} from "./bindings";
import { draftFor, useRetainer, RetentionStatus } from "./drafting";


function draftRecord(content: unknown): Record<string, unknown> | null {
  if (typeof content === "string") {
    try {
      content = JSON.parse(content);
    } catch {
      return null;
    }
  }
  if (content && typeof content === "object" && !Array.isArray(content)) {
    return content as Record<string, unknown>;
  }
  return null;
}

function goDurationMs(value: string): number | null {
  const match = /^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)$/.exec(value.trim());
  if (!match) return null;
  const amount = Number(match[1]);
  const unit = match[2];
  const scale: Record<string, number> = {
    ns: 1e-6,
    us: 0.001,
    "µs": 0.001,
    ms: 1,
    s: 1000,
    m: 60_000,
    h: 3_600_000,
  };
  const factor = unit ? scale[unit] : undefined;
  if (factor == null || Number.isNaN(amount)) return null;
  return amount * factor;
}

function rotationStatus(ref: SecretReference, now = Date.now()): { label: string; tone: "current" | "overdue" | "not-declared" } {
  if (!ref.max_age) return { label: "Rotation age not declared", tone: "not-declared" };
  const windowMs = goDurationMs(ref.max_age);
  const rotated = Date.parse(ref.rotated_at);
  if (windowMs == null || Number.isNaN(rotated)) {
    return { label: "Rotation unreadable — declare a duration such as 720h", tone: "not-declared" };
  }
  if (now > rotated + windowMs) return { label: "Overdue — rotate before use", tone: "overdue" };
  return { label: `Current (generation ${ref.generation})`, tone: "current" };
}

export function EnvironmentBanner({
  name,
  classification = "unclassified",
  address,
  transport,
  disclaimer,
}: {
  name?: string | undefined;
  classification?: TargetClassification | string | undefined;
  address?: string | undefined;
  transport?: string | undefined;
  disclaimer?: string | undefined;
}) {
  const normClass = (classification || "unclassified").toLowerCase();
  const isRefused = normClass === "production" || normClass === "unclassified";
  const defaultDisclaimer = isRefused
    ? `Refusal: ${normClass.charAt(0).toUpperCase() + normClass.slice(1)} targets reject all sends and resets.`
    : "Nonproduction environment: Synthetic test execution only.";

  return (
    <div className="environment-banner" role="status" aria-label="Target environment identity">
      <div className="environment-banner-header">
        <span className="environment-banner-title">
          {name ? `Environment: ${name}` : "Environment: Not configured"}
        </span>
        <span className={`environment-classification-badge ${normClass}`}>{normClass}</span>
        {address ? (
          <span className="environment-peer">
            {transport ? `${transport}://` : ""}
            {address}
          </span>
        ) : null}
      </div>
      <p className={`environment-disclaimer ${isRefused ? "warning" : ""}`}>
        {disclaimer || defaultDisclaimer}
      </p>
    </div>
  );
}

export function EnvironmentPanel({
  workspace,
  targetFile = "targets/default.json",
  secretsFile = "secrets.json",
  policyFile = "send-policy.json",
  planFile = "reset-plan.json",
  initialTab = "target",
  drafts = null,
  onTargetChange,
}: {
  workspace: string;
  targetFile?: string;
  secretsFile?: string;
  policyFile?: string;
  planFile?: string;
  initialTab?: "target" | "secrets" | "policy" | "reset";
  drafts?: EditorDraft[] | null;
  onTargetChange?: (target: Target | null) => void;
}) {
  const [activeTab, setActiveTab] = useState<"target" | "secrets" | "policy" | "reset">(initialTab);
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);

  // Target State
  const [currentTargetFile, setCurrentTargetFile] = useState(targetFile);
  const [target, setTarget] = useState<Target>({
    schema: "readmit-target/v3",
    test_endpoint: true,
    address: "",
    transport: "plain",
    approved_transport: false,
    connect_timeout: "2s",
    message_timeout: "5s",
    max_ack_bytes: 65536,
    classification: "unclassified",
  });
  const [checkReport, setCheckReport] = useState<EnvironmentReport | null>(null);
  const [checkDecision, setCheckDecision] = useState<SendPolicyDecision | null>(null);

  // Secrets State
  const [currentSecretsFile, setCurrentSecretsFile] = useState(secretsFile);
  const [secretsDoc, setSecretsDoc] = useState<SecretDocument | null>(null);
  const [newSecretName, setNewSecretName] = useState("");
  const [newSecretStore, setNewSecretStore] = useState<SecretStore>("os-keychain");
  const [newSecretPurpose, setNewSecretPurpose] = useState<SecretPurpose>("mllp-endpoint");
  const [newSecretAddress, setNewSecretAddress] = useState("");
  const [newSecretCommand, setNewSecretCommand] = useState("");
  const [newSecretArgs, setNewSecretArgs] = useState("");
  const [newSecretMaxAge, setNewSecretMaxAge] = useState("");
  const [testResult, setTestResult] = useState<SecretTestResult | null>(null);
  const [scanResult, setScanResult] = useState<SecretScanResult | null>(null);

  // Send Policy State
  const [currentPolicyFile, setCurrentPolicyFile] = useState(policyFile);
  const [policy, setPolicy] = useState<SendPolicy>({
    schema: "readmit-send-policy/v1",
    approved_destinations: [],
  });
  const [newDestination, setNewDestination] = useState("");
  const [evalAddress, setEvalAddress] = useState("");
  const [evalClassification, setEvalClassification] = useState<TargetClassification>("nonproduction");
  const [evalExplicit, setEvalExplicit] = useState(true);
  const [evalDecision, setEvalDecision] = useState<SendPolicyDecision | null>(null);

  // Fixture Reset Plan State
  const [currentPlanFile, setCurrentPlanFile] = useState(planFile);
  const [resetPlan, setResetPlan] = useState<ResetPlan>({
    schema: "readmit-reset-plan/v1",
    environment: "local-dev",
    actions: [],
  });
  const [newActionId, setNewActionId] = useState("");
  const [newActionOperator, setNewActionOperator] = useState<ResetOperator>("operator_confirms");
  const [newActionInstructions, setNewActionInstructions] = useState("");
  const [newActionObservation, setNewActionObservation] = useState("");
  const [confirmedActions, setConfirmedActions] = useState<string[]>([]);
  const [resetResult, setResetResult] = useState<ResetResult | null>(null);

  // Draft Retainer for active tab
  const retainer = useRetainer();

  const retainDraft = useCallback(
    (kind: string, schema: string, content: unknown) => {
      retainer.save({
        id: retainer.currentId(),
        kind,
        workspace,
        case: "",
        identity: "",
        content_schema: schema,
        content,
      });
    },
    [retainer, workspace],
  );

  const updateTarget = useCallback(
    (patch: Partial<Target>) => {
      setTarget((prev) => {
        const updated: Target = { ...prev, ...patch };
        retainDraft("environment/target", "readmit-target-draft/v1", updated);
        return updated;
      });
    },
    [retainDraft],
  );

  // Load initial data
  const targetLoad = useRef(0);
  const policyLoad = useRef(0);
  const planLoad = useRef(0);
  const adoptedDrafts = useRef(false);
  const holdInitialLoad = useRef({ target: false, policy: false, plan: false });

  const loadTarget = useCallback(async (file: string) => {
    const token = ++targetLoad.current;
    setBusy(true);
    try {
      const res = await readTarget(workspace, file);
      if (token !== targetLoad.current) return;
      if (res.state === "completed" && res.target) {
        setTarget(res.target);
        if (onTargetChange) onTargetChange(res.target);
      }
    } finally {
      if (token === targetLoad.current) setBusy(false);
    }
  }, [workspace, onTargetChange]);

  const loadSecrets = useCallback(async (file: string) => {
    setBusy(true);
    try {
      const res = await readSecrets(workspace, file);
      if (res.state === "completed" && res.document) {
        setSecretsDoc(res.document);
      }
    } finally {
      setBusy(false);
    }
  }, [workspace]);

  const loadPolicy = useCallback(async (file: string) => {
    const token = ++policyLoad.current;
    setBusy(true);
    try {
      const res = await readSendPolicy(workspace, file);
      if (token !== policyLoad.current) return;
      if (res.state === "completed" && res.policy) {
        setPolicy(res.policy);
      }
    } finally {
      if (token === policyLoad.current) setBusy(false);
    }
  }, [workspace]);

  const loadPlan = useCallback(async (file: string) => {
    const token = ++planLoad.current;
    setBusy(true);
    try {
      const res = await readResetPlan(workspace, file);
      if (token !== planLoad.current) return;
      if (res.state === "completed" && res.plan) {
        setResetPlan(res.plan);
      }
    } finally {
      if (token === planLoad.current) setBusy(false);
    }
  }, [workspace]);

  useEffect(() => {
    if (adoptedDrafts.current || !drafts) return;
    adoptedDrafts.current = true;
    const targetDraft = draftFor(drafts, "environment/target", workspace);
    const targetRecord = draftRecord(targetDraft?.content);
    if (targetRecord?.schema === "readmit-target/v3") {
      targetLoad.current += 1;
      holdInitialLoad.current.target = true;
      setTarget(targetRecord as unknown as Target);
      if (targetDraft) retainer.keepId(targetDraft.id);
    }
    const policyDraft = draftFor(drafts, "environment/policy", workspace);
    const policyRecord = draftRecord(policyDraft?.content);
    if (policyRecord?.schema === "readmit-send-policy/v1") {
      policyLoad.current += 1;
      holdInitialLoad.current.policy = true;
      setPolicy(policyRecord as unknown as SendPolicy);
      if (policyDraft) retainer.keepId(policyDraft.id);
    }
    const planDraft = draftFor(drafts, "environment/reset", workspace);
    const planRecord = draftRecord(planDraft?.content);
    if (planRecord?.schema === "readmit-reset-plan/v1") {
      planLoad.current += 1;
      holdInitialLoad.current.plan = true;
      setResetPlan(planRecord as unknown as ResetPlan);
      if (planDraft) retainer.keepId(planDraft.id);
    }
  }, [drafts, retainer, workspace]);

  useEffect(() => {
    if (holdInitialLoad.current.target) holdInitialLoad.current.target = false;
    else void loadTarget(currentTargetFile);
    void loadSecrets(currentSecretsFile);
    if (holdInitialLoad.current.policy) holdInitialLoad.current.policy = false;
    else void loadPolicy(currentPolicyFile);
    if (holdInitialLoad.current.plan) holdInitialLoad.current.plan = false;
    else void loadPlan(currentPlanFile);
  }, [currentTargetFile, currentSecretsFile, currentPolicyFile, currentPlanFile, loadTarget, loadSecrets, loadPolicy, loadPlan]);

  // Handle Target Save
  async function handleSaveTarget() {
    setBusy(true);
    setFeedback(null);
    try {
      const res = await saveTarget({
        workspace,
        target_file: currentTargetFile,
        target,
      });
      if (res.state === "completed" && res.target) {
        setTarget(res.target);
        if (onTargetChange) onTargetChange(res.target);
        setFeedback("Target configuration saved successfully.");
        retainer.clear();
      } else {
        setFeedback(res.reason || "Failed to save target.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Check Target (deliberate action)
  async function handleCheckTarget() {
    setBusy(true);
    setFeedback(null);
    setCheckReport(null);
    setCheckDecision(null);
    try {
      const res = await checkTarget({
        workspace,
        target_file: currentTargetFile,
        policy_file: currentPolicyFile,
      });
      if (res.report) setCheckReport(res.report);
      if (res.decision) setCheckDecision(res.decision);
      if (res.state !== "completed") {
        setFeedback(res.reason || "Target check reported issues.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Secret Reference Add
  async function handleAddSecret() {
    if (!newSecretName || !newSecretCommand) {
      setFeedback("Secret name and locator command are required.");
      return;
    }
    setBusy(true);
    setFeedback(null);
    try {
      const args = newSecretArgs
        .split(" ")
        .map((s) => s.trim())
        .filter(Boolean);
      const ref: SecretReference = {
        name: newSecretName,
        store: newSecretStore,
        purpose: newSecretPurpose,
        address: newSecretAddress,
        command: newSecretCommand,
        arguments: args,
        generation: 1,
        rotated_at: new Date().toISOString(),
      };
      if (newSecretMaxAge) {
        ref.max_age = newSecretMaxAge;
      }
      const res = await saveSecretReference({
        workspace,
        secrets_file: currentSecretsFile,
        reference: ref,
        is_update: false,
      });
      if (res.state === "completed" && res.document) {
        setSecretsDoc(res.document);
        setNewSecretName("");
        setNewSecretCommand("");
        setNewSecretArgs("");
        setNewSecretAddress("");
        setNewSecretMaxAge("");
        setFeedback("Secret reference registered successfully.");
        retainer.clear();
      } else {
        setFeedback(res.reason || "Failed to add secret reference.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Test Secret Reference
  async function handleTestSecret(name: string) {
    setBusy(true);
    setTestResult(null);
    try {
      const res = await testSecretReference(workspace, currentSecretsFile, name);
      setTestResult(res);
      if (res.state !== "completed") {
        setFeedback(res.reason || "Secret test failed.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Rotate Secret Reference
  async function handleRotateSecret(name: string) {
    setBusy(true);
    try {
      const res = await rotateSecretReference(workspace, currentSecretsFile, name);
      if (res.state === "completed" && res.document) {
        setSecretsDoc(res.document);
        setFeedback(`Secret ${name} rotated successfully.`);
      } else {
        setFeedback(res.reason || "Rotation failed.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Remove Secret Reference
  async function handleRemoveSecret(name: string) {
    setBusy(true);
    try {
      const res = await removeSecretReference(workspace, currentSecretsFile, name);
      if (res.state === "completed" && res.document) {
        setSecretsDoc(res.document);
        setFeedback(`Secret ${name} removed.`);
      } else {
        setFeedback(res.reason || "Failed to remove secret reference.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Scan Secrets
  async function handleScanSecrets() {
    setBusy(true);
    setScanResult(null);
    try {
      const res = await scanSecrets({
        workspace,
        secrets_file: currentSecretsFile,
        paths: [currentTargetFile, currentPolicyFile, currentPlanFile],
      });
      setScanResult(res);
    } finally {
      setBusy(false);
    }
  }

  // Handle Add Policy Destination
  function handleAddDestination() {
    if (!newDestination.trim()) return;
    const dest = newDestination.trim();
    if (policy.approved_destinations.includes(dest)) {
      setFeedback("Destination is already approved.");
      return;
    }
    const updated = {
      ...policy,
      approved_destinations: [...policy.approved_destinations, dest],
    };
    setPolicy(updated);
    setNewDestination("");
    retainDraft("environment/policy", "readmit-send-policy-draft/v1", updated);
  }

  // Handle Remove Policy Destination
  function handleRemoveDestination(dest: string) {
    const updated = {
      ...policy,
      approved_destinations: policy.approved_destinations.filter((d) => d !== dest),
    };
    setPolicy(updated);
    retainDraft("environment/policy", "readmit-send-policy-draft/v1", updated);
  }

  // Handle Save Send Policy
  async function handleSavePolicy() {
    setBusy(true);
    setFeedback(null);
    try {
      const res = await saveSendPolicy({
        workspace,
        policy_file: currentPolicyFile,
        policy,
      });
      if (res.state === "completed" && res.policy) {
        setPolicy(res.policy);
        setFeedback("Approved-destination policy saved.");
        retainer.clear();
      } else {
        setFeedback(res.reason || "Failed to save policy.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Local Policy Evaluation
  async function handleEvaluatePolicy() {
    setBusy(true);
    setEvalDecision(null);
    try {
      const res = await evaluateSendPolicy({
        workspace,
        policy_file: currentPolicyFile,
        address: evalAddress,
        classification: evalClassification,
        explicit: evalExplicit,
      });
      if (res.decision) {
        setEvalDecision(res.decision);
      }
      if (res.state !== "completed") {
        setFeedback(res.reason || "Evaluation failed.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Add Reset Action
  function handleAddResetAction() {
    if (!newActionId.trim() || !newActionInstructions.trim()) {
      setFeedback("Action ID and instructions are required.");
      return;
    }
    let authority: ResetAuthority = "none";
    if (newActionOperator === "observation_empty") authority = "read_declared_file";
    if (newActionOperator === "endpoint_quiet") authority = "connect_approved_target";

    const action: ResetAction = {
      id: newActionId.trim(),
      operator: newActionOperator,
      authority,
      instructions: newActionInstructions.trim(),
    };
    if (newActionOperator === "observation_empty" && newActionObservation.trim()) {
      action.observation = newActionObservation.trim();
    }

    const updated = {
      ...resetPlan,
      actions: [...resetPlan.actions, action],
    };
    setResetPlan(updated);
    setNewActionId("");
    setNewActionInstructions("");
    setNewActionObservation("");
    retainDraft("environment/reset", "readmit-reset-plan-draft/v1", updated);
  }

  // Handle Remove Reset Action
  function handleRemoveResetAction(id: string) {
    const updated = {
      ...resetPlan,
      actions: resetPlan.actions.filter((a) => a.id !== id),
    };
    setResetPlan(updated);
    setConfirmedActions((prev) => prev.filter((c) => c !== id));
    retainDraft("environment/reset", "readmit-reset-plan-draft/v1", updated);
  }

  // Handle Save Reset Plan
  async function handleSavePlan() {
    setBusy(true);
    setFeedback(null);
    try {
      const res = await saveResetPlan({
        workspace,
        plan_file: currentPlanFile,
        plan: resetPlan,
      });
      if (res.state === "completed" && res.plan) {
        setResetPlan(res.plan);
        setFeedback("Fixture reset plan saved.");
        retainer.clear();
      } else {
        setFeedback(res.reason || "Failed to save reset plan.");
      }
    } finally {
      setBusy(false);
    }
  }

  // Handle Execute Reset
  async function handleExecuteReset() {
    setBusy(true);
    setFeedback(null);
    setResetResult(null);
    try {
      const res = await resetTarget({
        workspace,
        target_file: currentTargetFile,
        plan_file: currentPlanFile,
        outcome_file: "outcomes/last-reset.json",
        policy_file: currentPolicyFile,
        confirmed: confirmedActions,
      });
      if (res.result) {
        setResetResult(res.result);
      }
      if (res.state !== "completed") {
        setFeedback(res.reason || "Reset finished with errors or refusals.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="environment-panel" aria-labelledby="environment-panel-title">
      <h3 id="environment-panel-title">Environment & Credential Configuration</h3>

      {/* Persistent Environment Banner */}
      <EnvironmentBanner
        name={target.name}
        classification={target.classification}
        address={target.address}
        transport={target.transport}
      />

      {feedback ? (
        <div role="status" aria-live="polite" className="report-box">
          <p>{feedback}</p>
        </div>
      ) : null}

      <RetentionStatus
        retention={retainer.retention}
        onRetry={retainer.retry}
        onKeepAsNew={retainer.keepAsNew}
        onDiscard={retainer.clear}
      />

      {/* Tab Navigation */}
      <nav className="environment-tabs" aria-label="Environment views">
        <button
          type="button"
          className={`environment-tab ${activeTab === "target" ? "active" : ""}`}
          onClick={() => setActiveTab("target")}
        >
          Target & Diagnostics
        </button>
        <button
          type="button"
          className={`environment-tab ${activeTab === "secrets" ? "active" : ""}`}
          onClick={() => setActiveTab("secrets")}
        >
          Credential References
        </button>
        <button
          type="button"
          className={`environment-tab ${activeTab === "policy" ? "active" : ""}`}
          onClick={() => setActiveTab("policy")}
        >
          Approved Send Policy
        </button>
        <button
          type="button"
          className={`environment-tab ${activeTab === "reset" ? "active" : ""}`}
          onClick={() => setActiveTab("reset")}
        >
          Fixture Reset Plan
        </button>
      </nav>

      {/* TAB 1: Target & Diagnostics */}
      {activeTab === "target" ? (
        <div className="environment-section" aria-labelledby="target-section-title">
          <h4 id="target-section-title">Named Target Configuration (readmit-target/v3)</h4>
          <div className="environment-field full-width">
            <label htmlFor="target-file-input">Target Config File</label>
            <input
              id="target-file-input"
              value={currentTargetFile}
              disabled={busy}
              onChange={(e) => setCurrentTargetFile(e.target.value)}
            />
          </div>

          <div className="environment-form-grid">
            <div className="environment-field">
              <label htmlFor="target-name">Environment Name</label>
              <input
                id="target-name"
                value={target.name || ""}
                disabled={busy}
                onChange={(e) => updateTarget({ name: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-classification">Classification</label>
              <select
                id="target-classification"
                value={target.classification || "unclassified"}
                disabled={busy}
                onChange={(e) => updateTarget({ classification: e.target.value as TargetClassification })}
              >
                <option value="nonproduction">Nonproduction</option>
                <option value="production">Production</option>
                <option value="unclassified">Unclassified</option>
              </select>
            </div>

            <div className="environment-field">
              <label htmlFor="target-address">Destination Address</label>
              <input
                id="target-address"
                value={target.address}
                disabled={busy}
                onChange={(e) => updateTarget({ address: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-transport">Transport</label>
              <select
                id="target-transport"
                value={target.transport}
                disabled={busy}
                onChange={(e) => updateTarget({ transport: e.target.value })}
              >
                <option value="plain">plain (unencrypted TCP/MLLP)</option>
                <option value="tls">tls (verified TLS/MLLP)</option>
              </select>
            </div>

            <div className="environment-field">
              <label htmlFor="target-server-name">TLS Server Name (SNI)</label>
              <input
                id="target-server-name"
                value={target.server_name || ""}
                disabled={busy}
                onChange={(e) => updateTarget({ server_name: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-ca-file">CA Certificate File</label>
              <input
                id="target-ca-file"
                value={target.ca_file || ""}
                disabled={busy}
                onChange={(e) => updateTarget({ ca_file: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-client-cert">Client Certificate File</label>
              <input
                id="target-client-cert"
                value={target.client_certificate || ""}
                disabled={busy}
                onChange={(e) => updateTarget({ client_certificate: e.target.value })}
              />
            </div>


            <div className="environment-field">
              <label htmlFor="target-connect-timeout">Connect timeout</label>
              <input
                id="target-connect-timeout"
                value={target.connect_timeout}
                disabled={busy}
                onChange={(e) => updateTarget({ connect_timeout: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-message-timeout">Message timeout</label>
              <input
                id="target-message-timeout"
                value={target.message_timeout}
                disabled={busy}
                onChange={(e) => updateTarget({ message_timeout: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-max-ack">Maximum acknowledgement size (bytes)</label>
              <input
                id="target-max-ack"
                type="number"
                min={1}
                value={target.max_ack_bytes}
                disabled={busy}
                onChange={(e) => updateTarget({ max_ack_bytes: Number(e.target.value) })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-test-endpoint">
                <input
                  id="target-test-endpoint"
                  type="checkbox"
                  checked={target.test_endpoint}
                  disabled={busy}
                  onChange={(e) => updateTarget({ test_endpoint: e.target.checked })}
                />
                Test endpoint
              </label>
            </div>

            <div className="environment-field">
              <label htmlFor="target-approved-transport">
                <input
                  id="target-approved-transport"
                  type="checkbox"
                  checked={target.approved_transport}
                  disabled={busy}
                  onChange={(e) => updateTarget({ approved_transport: e.target.checked })}
                />
                Approved transport
              </label>
              <p className="hint">Off until this transport is explicitly approved. Choosing the target does not grant send or reset authority.</p>
            </div>

            <div className="environment-field">
              <label htmlFor="target-secret-ref">Credential Reference</label>
              <select
                id="target-secret-ref"
                value={target.credential?.reference || ""}
                disabled={busy}
                onChange={(e) => {
                  const refName = e.target.value;
                  const updated: Target = { ...target };
                  if (refName) {
                    updated.credential = { secrets_file: currentSecretsFile, reference: refName };
                  } else {
                    delete updated.credential;
                  }
                  setTarget(updated);
                  retainDraft("environment/target", "readmit-target-draft/v1", updated);
                }}
              >
                <option value="">None (no client authentication secret)</option>
                {(secretsDoc?.references ?? []).map((r) => (
                  <option key={r.name} value={r.name}>
                    {r.name} ({r.purpose} · {r.store})
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="environment-actions">
            <button type="button" disabled={busy} onClick={() => void handleSaveTarget()}>
              Save Target Configuration
            </button>
            <button type="button" disabled={busy} onClick={() => void handleCheckTarget()}>
              Check Target Reachability & TLS
            </button>
          </div>

          {checkReport ? (
            <div className="report-box" aria-label="Reachability diagnostic report">
              <h5>Diagnostic Report (no HL7 payloads sent)</h5>
              <div className="report-grid">
                <span className="report-label">Target Name:</span>
                <span className="report-value">{checkReport.name}</span>
                <span className="report-label">Classification:</span>
                <span className="report-value">{checkReport.classification}</span>
                <span className="report-label">Peer Address:</span>
                <span className="report-value">{checkReport.peer}</span>
                <span className="report-label">Outcome:</span>
                <span className="report-value">{checkReport.outcome}</span>
                <span className="report-label">Phase:</span>
                <span className="report-value">{checkReport.phase}</span>
                {checkReport.tls_version ? (
                  <>
                    <span className="report-label">TLS Version:</span>
                    <span className="report-value">{checkReport.tls_version}</span>
                    <span className="report-label">Cipher Suite:</span>
                    <span className="report-value">{checkReport.cipher_suite}</span>
                  </>
                ) : null}
              </div>
              {checkDecision ? (
                <p style={{ marginTop: "0.5rem" }}>
                  Send Policy Decision:{" "}
                  <strong>{checkDecision.allowed ? "Allowed" : "Refused"}</strong> (Reason:{" "}
                  <code>{checkDecision.reason}</code>)
                </p>
              ) : null}
            </div>
          ) : null}
        </div>
      ) : null}

      {/* TAB 2: Credential References */}
      {activeTab === "secrets" ? (
        <div className="environment-section" aria-labelledby="secrets-section-title">
          <h4 id="secrets-section-title">Credential References (readmit-secrets/v1)</h4>
          <p className="environment-disclaimer">
            References name storage locators without holding credentials. Secret values are never
            written to disk, logs, React state or webview storage.
          </p>

          <div className="environment-field full-width">
            <label htmlFor="secrets-file-input">Secrets Document File</label>
            <input
              id="secrets-file-input"
              value={currentSecretsFile}
              disabled={busy}
              onChange={(e) => setCurrentSecretsFile(e.target.value)}
            />
          </div>

          <div className="provisioning-box">
            <h5>Native Store & Customer-Vault Provisioning Handoff</h5>
            <ol>
              <li>Store your credential in your OS keychain (e.g. macOS Keychain, Linux Secret Service) or customer vault.</li>
              <li>Provide the locator command and argument vector that prints the secret to stdout (e.g. <code>security find-generic-password -s readmit -w</code>).</li>
              <li>Readmit invokes the locator strictly on-demand in memory and clears memory immediately after use.</li>
            </ol>
          </div>

          <table className="environment-table" aria-label="Registered credential references">
            <thead>
              <tr>
                <th>Name</th>
                <th>Purpose</th>
                <th>Store</th>
                <th>Command Locator</th>
                <th>Secret Value</th>
                <th>Rotation Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {(secretsDoc?.references ?? []).length === 0 ? (
                <tr>
                  <td colSpan={7}>No credential references registered yet.</td>
                </tr>
              ) : (
                (secretsDoc?.references ?? []).map((r) => (
                  <tr key={r.name}>
                    <td><strong>{r.name}</strong></td>
                    <td>{r.purpose}</td>
                    <td>{r.store}</td>
                    <td><code>{r.command} {r.arguments.join(" ")}</code></td>
                    <td><span className="report-value">••••••••</span></td>
                    <td>
                      {(() => {
                        const rotation = rotationStatus(r);
                        const inaccessible = testResult?.name === r.name && testResult.success === false;
                        return (
                          <>
                            <span className={`rotation-badge ${rotation.tone}`}>{rotation.label}</span>
                            {inaccessible ? <p className="hint">Inaccessible — the locator did not resolve. Provision the reference in its store, then test again.</p> : null}
                          </>
                        );
                      })()}
                    </td>
                    <td>
                      <div style={{ display: "flex", gap: "0.3rem" }}>
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => void handleTestSecret(r.name)}
                          title="Verify resolution without capturing secret"
                        >
                          Test
                        </button>
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => void handleRotateSecret(r.name)}
                        >
                          Rotate
                        </button>
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => void handleRemoveSecret(r.name)}
                        >
                          Remove
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>

          {testResult ? (
            <div className="report-box" aria-label="Secret test outcome">
              <h5>Secret Resolution Test: {testResult.name}</h5>
              <p>Status: {testResult.success ? "Success (Resolved in memory)" : "Failed"}</p>
              {testResult.reason ? <p>Reason: {testResult.reason}</p> : null}
            </div>
          ) : null}

          <h5>Register New Credential Reference</h5>
          <div className="environment-form-grid">
            <div className="environment-field">
              <label htmlFor="secret-name">Reference Name</label>
              <input
                id="secret-name"
                value={newSecretName}
                disabled={busy}
                onChange={(e) => setNewSecretName(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="secret-store">Store</label>
              <select
                id="secret-store"
                value={newSecretStore}
                disabled={busy}
                onChange={(e) => setNewSecretStore(e.target.value as SecretStore)}
              >
                <option value="os-keychain">OS Keychain</option>
                <option value="customer-managed">Customer Managed</option>
              </select>
            </div>
            <div className="environment-field">
              <label htmlFor="secret-purpose">Purpose</label>
              <select
                id="secret-purpose"
                value={newSecretPurpose}
                disabled={busy}
                onChange={(e) => setNewSecretPurpose(e.target.value as SecretPurpose)}
              >
                <option value="mllp-endpoint">MLLP Endpoint</option>
                <option value="source-endpoint">Evidence Source Endpoint</option>
              </select>
            </div>
            <div className="environment-field">
              <label htmlFor="secret-address">Target Address Constraint</label>
              <input
                id="secret-address"
                value={newSecretAddress}
                placeholder="host:port"
                disabled={busy}
                onChange={(e) => setNewSecretAddress(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="secret-command">Locator Command (Path)</label>
              <input
                id="secret-command"
                value={newSecretCommand}
                placeholder="/usr/bin/security"
                disabled={busy}
                onChange={(e) => setNewSecretCommand(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="secret-args">Command Arguments (space-separated)</label>
              <input
                id="secret-args"
                value={newSecretArgs}
                placeholder="find-generic-password -s svc -w"
                disabled={busy}
                onChange={(e) => setNewSecretArgs(e.target.value)}
              />
            </div>
          </div>
          <div className="environment-actions">
            <button type="button" disabled={busy} onClick={() => void handleAddSecret()}>
              Register Secret Reference
            </button>
            <button type="button" disabled={busy} onClick={() => void handleScanSecrets()}>
              Scan Workspace for Residual Leaks
            </button>
          </div>

          {scanResult?.scan ? (
            <div className="report-box" aria-label="Credential leak scan report">
              <h5>Credential Leak Scan Results</h5>
              <p>Status: <strong>{scanResult.scan.status}</strong></p>
              <p>Files Checked: {scanResult.scan.files_checked} · Known Values Checked: {scanResult.scan.known_values_checked}</p>
              {scanResult.scan.unresolved_locations.length > 0 ? (
                <div>
                  <p style={{ color: "#c62828" }}>Residual leaks detected in files:</p>
                  <ul>
                    {scanResult.scan.unresolved_locations.map((loc) => (
                      <li key={loc}>{loc}</li>
                    ))}
                  </ul>
                </div>
              ) : (
                <p style={{ color: "#2e7d32" }}>No residual credential leaks detected in examined files.</p>
              )}
              <p className="environment-disclaimer">{scanResult.scan.limitations}</p>
            </div>
          ) : null}
        </div>
      ) : null}

      {/* TAB 3: Send Policy */}
      {activeTab === "policy" ? (
        <div className="environment-section" aria-labelledby="policy-section-title">
          <h4 id="policy-section-title">Approved Send Policy (readmit-send-policy/v1)</h4>
          <p className="environment-disclaimer">
            All sends require an explicit approved-destinations policy. Destinations outside declared CIDR ranges,
            unclassified environments, and production targets are strictly refused.
          </p>

          <div className="environment-field full-width">
            <label htmlFor="policy-file-input">Policy File</label>
            <input
              id="policy-file-input"
              value={currentPolicyFile}
              disabled={busy}
              onChange={(e) => setCurrentPolicyFile(e.target.value)}
            />
          </div>

          <h5>Approved CIDR Prefixes</h5>
          <ul style={{ listStyle: "none", paddingLeft: 0 }}>
            {policy.approved_destinations.map((dest) => (
              <li key={dest} style={{ display: "flex", gap: "0.5rem", alignItems: "center", marginBottom: "0.4rem" }}>
                <code>{dest}</code>
                <button
                  type="button"
                  disabled={busy || policy.approved_destinations.length <= 1}
                  onClick={() => handleRemoveDestination(dest)}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>

          <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.5rem" }}>
            <input
              placeholder="network/prefix"
              value={newDestination}
              disabled={busy}
              onChange={(e) => setNewDestination(e.target.value)}
            />
            <button type="button" disabled={busy || !newDestination} onClick={handleAddDestination}>
              Add CIDR Prefix
            </button>
          </div>

          <div className="environment-actions" style={{ marginTop: "1rem" }}>
            <button type="button" disabled={busy} onClick={() => void handleSavePolicy()}>
              Save Approved Send Policy
            </button>
          </div>

          <hr style={{ margin: "1.5rem 0", borderColor: "var(--line, #ccc)" }} />

          <h5>Local Destination Evaluation (No Network Connection)</h5>
          <div className="environment-form-grid">
            <div className="environment-field">
              <label htmlFor="eval-address">Destination Address</label>
              <input
                id="eval-address"
                value={evalAddress}
                disabled={busy}
                onChange={(e) => setEvalAddress(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="eval-class">Target Classification</label>
              <select
                id="eval-class"
                value={evalClassification}
                disabled={busy}
                onChange={(e) => setEvalClassification(e.target.value as TargetClassification)}
              >
                <option value="nonproduction">Nonproduction</option>
                <option value="production">Production</option>
                <option value="unclassified">Unclassified</option>
              </select>
            </div>
          </div>
          <div style={{ margin: "0.5rem 0" }}>
            <label style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
              <input
                type="checkbox"
                checked={evalExplicit}
                disabled={busy}
                onChange={(e) => setEvalExplicit(e.target.checked)}
              />
              Explicit send intention declared
            </label>
          </div>
          <button type="button" disabled={busy} onClick={() => void handleEvaluatePolicy()}>
            Evaluate Destination Locally
          </button>

          {evalDecision ? (
            <div className="report-box" aria-label="Local evaluation decision">
              <h5>Local Policy Decision</h5>
              <p>
                Decision: <strong>{evalDecision.allowed ? "ALLOWED" : "DENIED"}</strong>
              </p>
              <p>Reason: <code>{evalDecision.reason}</code></p>
              <p>Target Class: {evalDecision.classification} · Explicit: {evalDecision.explicit_send ? "yes" : "no"}</p>
            </div>
          ) : null}
        </div>
      ) : null}

      {/* TAB 4: Fixture Reset */}
      {activeTab === "reset" ? (
        <div className="environment-section" aria-labelledby="reset-section-title">
          <h4 id="reset-section-title">Fixture Reset Plan & Execution (readmit-reset-plan/v1)</h4>
          <p className="environment-disclaimer">
            Fixture reset plans return nonproduction test fixtures to a declared starting state.
            Only reviewed operators are permitted; arbitrary shell commands or scripts are strictly rejected.
          </p>

          <div className="environment-form-grid">
            <div className="environment-field">
              <label htmlFor="plan-file-input">Plan File</label>
              <input
                id="plan-file-input"
                value={currentPlanFile}
                disabled={busy}
                onChange={(e) => setCurrentPlanFile(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="plan-env-input">Environment Name Match</label>
              <input
                id="plan-env-input"
                value={resetPlan.environment}
                disabled={busy}
                onChange={(e) => {
                  const updated = { ...resetPlan, environment: e.target.value };
                  setResetPlan(updated);
                  retainDraft("environment/reset", "readmit-reset-plan-draft/v1", updated);
                }}
              />
            </div>
          </div>

          <h5>Reviewed Reset Actions</h5>
          {resetPlan.actions.length === 0 ? (
            <p>No actions declared in this plan yet.</p>
          ) : (
            resetPlan.actions.map((act) => (
              <div key={act.id} className="action-item">
                <div className="action-header">
                  <span>{act.id}</span>
                  <span className="authority-badge">Authority: {act.authority}</span>
                  <span className="badge">{act.operator}</span>
                  <button
                    type="button"
                    style={{ marginLeft: "auto" }}
                    disabled={busy}
                    onClick={() => handleRemoveResetAction(act.id)}
                  >
                    Remove
                  </button>
                </div>
                <p className="action-instructions">{act.instructions}</p>
                {act.observation ? (
                  <p style={{ fontSize: "0.82rem" }}>Observation File: <code>{act.observation}</code></p>
                ) : null}
                {act.operator === "operator_confirms" ? (
                  <label style={{ display: "flex", alignItems: "center", gap: "0.5rem", marginTop: "0.4rem" }}>
                    <input
                      type="checkbox"
                      checked={confirmedActions.includes(act.id)}
                      disabled={busy}
                      onChange={(e) => {
                        if (e.target.checked) {
                          setConfirmedActions((prev) => [...prev, act.id]);
                        } else {
                          setConfirmedActions((prev) => prev.filter((id) => id !== act.id));
                        }
                      }}
                    />
                    Confirmed by human operator (--confirm parity)
                  </label>
                ) : null}
              </div>
            ))
          )}

          <h5>Add Reviewed Reset Action</h5>
          <div className="environment-form-grid">
            <div className="environment-field">
              <label htmlFor="action-id">Action ID</label>
              <input
                id="action-id"
                value={newActionId}
                placeholder="e.g. purge-inbox"
                disabled={busy}
                onChange={(e) => setNewActionId(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="action-operator">Reviewed Operator</label>
              <select
                id="action-operator"
                value={newActionOperator}
                disabled={busy}
                onChange={(e) => setNewActionOperator(e.target.value as ResetOperator)}
              >
                <option value="operator_confirms">operator_confirms (Human confirmation required; authority: none)</option>
                <option value="observation_empty">observation_empty (Verifies receiver ledger is empty; authority: read_declared_file)</option>
                <option value="endpoint_quiet">endpoint_quiet (Verifies endpoint connectivity without sending HL7; authority: connect_approved_target)</option>
              </select>
            </div>
            <div className="environment-field full-width">
              <label htmlFor="action-instructions">Side-Effect & Reset Instructions</label>
              <textarea
                id="action-instructions"
                rows={2}
                value={newActionInstructions}
                placeholder="Describe exact manual action or side effect for the operator..."
                disabled={busy}
                onChange={(e) => setNewActionInstructions(e.target.value)}
              />
            </div>
            {newActionOperator === "observation_empty" ? (
              <div className="environment-field full-width">
                <label htmlFor="action-obs">Receiver Observation File Path</label>
                <input
                  id="action-obs"
                  value={newActionObservation}
                  placeholder="observation.json"
                  disabled={busy}
                  onChange={(e) => setNewActionObservation(e.target.value)}
                />
              </div>
            ) : null}
          </div>

          <div className="environment-actions">
            <button type="button" disabled={busy} onClick={handleAddResetAction}>
              Add Action to Plan
            </button>
            <button type="button" disabled={busy} onClick={() => void handleSavePlan()}>
              Save Reset Plan
            </button>
            <button
              type="button"
              disabled={busy || resetPlan.actions.length === 0}
              onClick={() => void handleExecuteReset()}
              style={{ fontWeight: "bold" }}
            >
              Execute Fixture Reset Deliberately
            </button>
          </div>

          {resetResult ? (
            <div
              className={`outcome-card ${resetResult.state === "completed" ? "passed" : "failed"}`}
              aria-label="Fixture reset execution outcome"
            >
              <h5>Reset Outcome: {resetResult.outcome}</h5>
              <p>State: <strong>{resetResult.state}</strong> · Reason: {resetResult.reason || "None"}</p>
              <p>Environment: {resetResult.environment} ({resetResult.classification})</p>
              <h6>Action Results:</h6>
              <ul>
                {(resetResult.actions ?? []).map((ao) => (
                  <li key={ao.id}>
                    <strong>{ao.id}</strong> ({ao.operator}): {ao.outcome} (Authority: {ao.authority})
                    {ao.reason ? ` — ${ao.reason}` : ""}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
