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
  type SecretChange,
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
  type State,
} from "./bindings";
import { draftFor, useRetainer, RetentionStatus } from "./drafting";
import { Outcome, useLifecycle, type Answer } from "./lifecycle";


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

/** Locator arguments typed one per line. An argument holds no control
 * character, so a line break never falls inside one and an argument may hold a
 * space. Each line is trimmed, because the arguments are never shown again to
 * reveal a stray space, and a blank line is no argument. */
function argumentLines(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");
}

/** One document a save replaced atomically, named by the SHA-256 of the exact
 * bytes the save wrote. */
interface Written {
  file: string;
  identity: string;
}

function WrittenIdentity({ written }: { written: Written | null }) {
  if (!written) return null;
  return (
    <p className="hint">
      Written to <code>{written.file}</code> · identity <code>{written.identity}</code>
    </p>
  );
}

/** An edit of one registered reference, opened on the reference as it was
 * read: the members `readmit secret update` can change. The name and purpose
 * are what the stored credential is for and are not edited, and the
 * registered locator arguments are counted, never shown; they are replaced
 * only when the person chooses to. */
interface SecretEdit {
  opened: SecretReference;
  store: SecretStore;
  address: string;
  command: string;
  maxAge: string;
  replaceArguments: boolean;
  replacementArguments: string;
}

/** The credential choice that keeps a binding the loaded document does not
 * offer. It is not a reference name: a name never holds a space. */
const KEEP_BINDING = "(keep the recorded binding)";

const newPolicy = (): SendPolicy => ({ schema: "readmit-send-policy/v1", approved_destinations: [] });
const newPlan = (): ResetPlan => ({ schema: "readmit-reset-plan/v1", environment: "local-dev", actions: [] });

/** A relative reference would be anchored to the target's physical directory,
 * which can differ from the path the window names through a folder symlink.
 * Use the exact workspace path ReadSecrets used, independent of that target. */
function secretsReference(workspace: string, secretsFile: string): string {
  const windowsPath = /^[A-Za-z]:[\\/]/.test(workspace) || workspace.startsWith("\\\\");
  if (secretsFile.startsWith("/") || (windowsPath && (secretsFile.startsWith("\\\\") || /^[A-Za-z]:[\\/]/.test(secretsFile)))) return secretsFile;
  // resolveWorkspacePath cleans a relative name before ReadSecrets opens it.
  // Clean it here too, before a symlink/.. sequence can change which file the
  // absolute target credential reference reaches.
  const parts = (windowsPath ? secretsFile.replaceAll("\\", "/") : secretsFile).split("/");
  const clean: string[] = [];
  for (const part of parts) {
    if (part === "" || part === ".") continue;
    if (part === "..") clean.pop();
    else clean.push(part);
  }
  return workspace.replace(/[\\/]$/, "") + "/" + clean.join("/");
}

/** A document whose reference declares no locator arguments may carry none. */
function argumentCount(ref: SecretReference): number {
  return (ref.arguments ?? []).length;
}

/** What an edit changes: each member the person changed since the edit
 * opened, and nothing else, as `readmit secret update` changes only what its
 * flags name. A member someone else changed meanwhile is not written back. */
function secretChange(edit: SecretEdit): SecretChange {
  const change: SecretChange = {};
  if (edit.store !== edit.opened.store) change.store = edit.store;
  if (edit.address !== edit.opened.address) change.address = edit.address;
  if (edit.command !== edit.opened.command) change.command = edit.command;
  if (edit.maxAge !== (edit.opened.max_age ?? "")) change.max_age = edit.maxAge;
  if (edit.replaceArguments) change.arguments = argumentLines(edit.replacementArguments);
  return change;
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

/** An answer the facade did not complete, as the panel shows it: its state and
 * its reason, or the panel's own sentence when the facade gave none. An answer
 * that says completed without what completion carries is a failure. */
function refused(answer: { state: State; reason?: string }, otherwise: string): Answer {
  return { state: answer.state === "completed" ? "failed" : answer.state, reason: answer.reason || otherwise };
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
  onPlanSaved,
}: {
  workspace: string;
  targetFile?: string;
  secretsFile?: string;
  policyFile?: string;
  planFile?: string;
  initialTab?: "target" | "secrets" | "policy" | "reset";
  drafts?: EditorDraft[] | null;
  onTargetChange?: (target: Target | null) => void;
  /** A saved reset plan is a new or replaced entry of the folder, so the
   * window reads its listing again and the controlled reduction's picker
   * offers it. */
  onPlanSaved?: () => void;
}) {
  const [activeTab, setActiveTab] = useState<"target" | "secrets" | "policy" | "reset">(initialTab);
  // Every control is disabled while an action runs, which takes focus from the
  // one that started it. Once the action has answered, focus returns there when
  // nothing else has taken it, so a keyboard user keeps their place after a
  // refusal as after a success.
  const actions = useLifecycle<"working">();
  const busy = actions.running !== null;
  // The panel's own sentence, or an answer the facade refused, drawn through
  // Status with its state as well as its reason.
  const [feedback, setFeedback] = useState<string | Answer | null>(null);

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
  const [secretsRefusal, setSecretsRefusal] = useState<string | null>(null);
  const [newSecretName, setNewSecretName] = useState("");
  const [newSecretStore, setNewSecretStore] = useState<SecretStore>("os-keychain");
  const [newSecretPurpose, setNewSecretPurpose] = useState<SecretPurpose>("mllp-endpoint");
  const [newSecretAddress, setNewSecretAddress] = useState("");
  const [newSecretCommand, setNewSecretCommand] = useState("");
  const [newSecretArgs, setNewSecretArgs] = useState("");
  const [newSecretMaxAge, setNewSecretMaxAge] = useState("");
  const [testResult, setTestResult] = useState<SecretTestResult | null>(null);
  const [scanResult, setScanResult] = useState<SecretScanResult | null>(null);
  const [secretEdit, setSecretEdit] = useState<SecretEdit | null>(null);
  const [secretsWritten, setSecretsWritten] = useState<Written | null>(null);
  // Focus moves into an edit when it opens and back to its reference's Edit
  // control when it is saved or cancelled, or to the table when that reference
  // is gone, so a keyboard user never loses their place.
  const editFirstField = useRef<HTMLSelectElement | null>(null);
  const secretsTable = useRef<HTMLTableElement | null>(null);
  const returnFocusTo = useRef<string | null>(null);

  // Send Policy State
  const [currentPolicyFile, setCurrentPolicyFile] = useState(policyFile);
  const [policy, setPolicy] = useState<SendPolicy>(newPolicy);
  const [policyReady, setPolicyReady] = useState(false);
  const [policyRefusal, setPolicyRefusal] = useState<string | null>(null);
  const [newDestination, setNewDestination] = useState("");
  const [evalAddress, setEvalAddress] = useState("");
  const [evalClassification, setEvalClassification] = useState<TargetClassification>("nonproduction");
  const [evalExplicit, setEvalExplicit] = useState(true);
  const [evalDecision, setEvalDecision] = useState<SendPolicyDecision | null>(null);
  const [policyWritten, setPolicyWritten] = useState<Written | null>(null);

  // Fixture Reset Plan State
  const [currentPlanFile, setCurrentPlanFile] = useState(planFile);
  const [resetPlan, setResetPlan] = useState<ResetPlan>(newPlan);
  const [planReady, setPlanReady] = useState(false);
  const [planRefusal, setPlanRefusal] = useState<string | null>(null);
  const [newActionId, setNewActionId] = useState("");
  const [newActionOperator, setNewActionOperator] = useState<ResetOperator>("operator_confirms");
  const [newActionInstructions, setNewActionInstructions] = useState("");
  const [newActionObservation, setNewActionObservation] = useState("");
  const [confirmedActions, setConfirmedActions] = useState<string[]>([]);
  const [resetResult, setResetResult] = useState<ResetResult | null>(null);
  const [planWritten, setPlanWritten] = useState<Written | null>(null);

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

  // Load initial data. The four documents are read together, and every form
  // stays disabled until the last read has answered: a read that landed after
  // a person had started typing would replace what they typed. Only the
  // latest read of each document commits what it read.
  const documents = useLifecycle<"target" | "secrets" | "policy" | "plan">({ background: true });
  const loadingDocuments = documents.running !== null;
  const readDocument = documents.run;
  const withdrawDocument = documents.withdraw;
  // A form is closed while an action runs or a document it shows is being
  // read. The file names stay open while documents are read, because naming
  // another document is what starts a read: closing them would drop the
  // keystrokes of the name being typed.
  const blocked = busy || loadingDocuments;
  const policyRead = useRef<{ key: string; succeeded: boolean } | null>(null);
  const planRead = useRef<{ key: string; succeeded: boolean } | null>(null);
  const policyDraftPending = useRef(false);
  const planDraftPending = useRef(false);
  const adoptedDrafts = useRef(false);
  const holdInitialLoad = useRef({ target: false });

  const loadTarget = useCallback(async (file: string) => {
    await readDocument("target", async (current) => {
      const res = await readTarget(workspace, file);
      if (!current()) return;
      if (res.state === "completed" && res.target) {
        setTarget(res.target);
        if (onTargetChange) onTargetChange(res.target);
      }
    });
  }, [workspace, onTargetChange, readDocument]);

  const showSecrets = useCallback((document: SecretDocument) => {
    setSecretsDoc(document);
    setSecretsRefusal(null);
  }, []);

  // A document that cannot be read shows why instead of the references of the
  // one read before it, so an edit never starts from another file.
  const loadSecrets = useCallback(async (file: string) => {
    await readDocument("secrets", async (current) => {
      const res = await readSecrets(workspace, file);
      if (!current()) return;
      if (res.state === "completed" && res.document) {
        showSecrets(res.document);
      } else {
        setSecretsDoc(null);
        setSecretsRefusal(res.reason || "This secret reference document cannot be read.");
      }
    });
  }, [workspace, readDocument, showSecrets]);

  const loadPolicy = useCallback(async (file: string) => {
    await readDocument("policy", async (current) => {
      const res = await readSendPolicy(workspace, file);
      if (!current()) return;
      if (res.state === "completed" && res.policy) {
        if (!policyDraftPending.current) setPolicy(res.policy);
        setPolicyReady(true);
        setPolicyRefusal(null);
        policyRead.current = { key: `${workspace}\0${file}`, succeeded: true };
      } else {
        setPolicy(newPolicy());
        setPolicyReady(false);
        setPolicyRefusal(res.reason || "This send policy cannot be read.");
        policyDraftPending.current = false;
        policyRead.current = { key: `${workspace}\0${file}`, succeeded: false };
      }
    });
  }, [workspace, readDocument]);

  const loadPlan = useCallback(async (file: string) => {
    await readDocument("plan", async (current) => {
      const res = await readResetPlan(workspace, file);
      if (!current()) return;
      if (res.state === "completed" && res.plan) {
        if (!planDraftPending.current) setResetPlan(res.plan);
        setPlanReady(true);
        setPlanRefusal(null);
        planRead.current = { key: `${workspace}\0${file}`, succeeded: true };
      } else {
        setResetPlan(newPlan());
        setPlanReady(false);
        setPlanRefusal(res.reason || "This reset plan cannot be read.");
        planDraftPending.current = false;
        planRead.current = { key: `${workspace}\0${file}`, succeeded: false };
      }
    });
  }, [workspace, readDocument]);

  useEffect(() => {
    if (adoptedDrafts.current || !drafts) return;
    adoptedDrafts.current = true;
    const targetDraft = draftFor(drafts, "environment/target", workspace);
    const targetRecord = draftRecord(targetDraft?.content);
    if (targetRecord?.schema === "readmit-target/v3") {
      withdrawDocument("target");
      holdInitialLoad.current.target = true;
      setTarget(targetRecord as unknown as Target);
      if (targetDraft) retainer.keepId(targetDraft.id);
    }
    const policyDraft = draftFor(drafts, "environment/policy", workspace);
    const policyRecord = draftRecord(policyDraft?.content);
    const policyReadState = policyRead.current;
    const policyKey = `${workspace}\0${currentPolicyFile}`;
    if (policyRecord?.schema === "readmit-send-policy/v1" && (policyReadState?.key !== policyKey || policyReadState.succeeded)) {
      policyDraftPending.current = true;
      setPolicy(policyRecord as unknown as SendPolicy);
      setPolicyReady(policyReadState?.key === policyKey && policyReadState.succeeded);
      if (policyDraft) retainer.keepId(policyDraft.id);
    }
    const planDraft = draftFor(drafts, "environment/reset", workspace);
    const planRecord = draftRecord(planDraft?.content);
    const planReadState = planRead.current;
    const planKey = `${workspace}\0${currentPlanFile}`;
    if (planRecord?.schema === "readmit-reset-plan/v1" && (planReadState?.key !== planKey || planReadState.succeeded)) {
      planDraftPending.current = true;
      setResetPlan(planRecord as unknown as ResetPlan);
      setPlanReady(planReadState?.key === planKey && planReadState.succeeded);
      if (planDraft) retainer.keepId(planDraft.id);
    }
  }, [drafts, retainer, workspace, currentPolicyFile, currentPlanFile, withdrawDocument]);

  // Each document is read when its own file is named, so typing one name
  // re-reads that document and leaves the other three as they are.
  useEffect(() => {
    if (holdInitialLoad.current.target) holdInitialLoad.current.target = false;
    else void loadTarget(currentTargetFile);
  }, [currentTargetFile, loadTarget]);
  useEffect(() => {
    void loadSecrets(currentSecretsFile);
  }, [currentSecretsFile, loadSecrets]);
  useEffect(() => {
    void loadPolicy(currentPolicyFile);
  }, [currentPolicyFile, loadPolicy]);
  useEffect(() => {
    void loadPlan(currentPlanFile);
  }, [currentPlanFile, loadPlan]);

  // Another workspace is other documents: an open edit and the identities of
  // earlier saves belong to the one before it.
  useEffect(() => {
    returnFocusTo.current = null;
    setSecretEdit(null);
    setSecretsWritten(null);
    setPolicyWritten(null);
    setPlanWritten(null);
  }, [workspace]);

  const editingName = secretEdit?.opened.name ?? null;
  useEffect(() => {
    if (editingName !== null) {
      editFirstField.current?.focus();
      return;
    }
    const name = returnFocusTo.current;
    returnFocusTo.current = null;
    if (name === null) return;
    const table = secretsTable.current;
    const edit = Array.from(table?.querySelectorAll<HTMLButtonElement>("button[data-edit]") ?? []).find((b) => b.dataset.edit === name);
    (edit ?? table)?.focus();
  }, [editingName]);

  // Handle Target Save
  async function handleSaveTarget() {
    await actions.run("working", async () => {
      setFeedback(null);
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
        setFeedback(refused(res, "Failed to save target."));
      }
    });
  }

  // Handle Check Target (deliberate action)
  async function handleCheckTarget() {
    await actions.run("working", async () => {
      setFeedback(null);
      setCheckReport(null);
      setCheckDecision(null);
      const res = await checkTarget({
        workspace,
        target_file: currentTargetFile,
        policy_file: currentPolicyFile,
      });
      if (res.report) setCheckReport(res.report);
      if (res.decision) setCheckDecision(res.decision);
      if (res.state !== "completed") {
        setFeedback(refused(res, "Target check reported issues."));
      }
    });
  }

  // Handle Secret Reference Add
  async function handleAddSecret() {
    if (!newSecretName || !newSecretCommand) {
      setFeedback("Secret name and locator command are required.");
      return;
    }
    await actions.run("working", async () => {
      setFeedback(null);
      setSecretsWritten(null);
      const file = currentSecretsFile;
      const ref: SecretReference = {
        name: newSecretName,
        store: newSecretStore,
        purpose: newSecretPurpose,
        address: newSecretAddress,
        command: newSecretCommand,
        arguments: argumentLines(newSecretArgs),
        generation: 1,
        rotated_at: new Date().toISOString(),
      };
      if (newSecretMaxAge) {
        ref.max_age = newSecretMaxAge;
      }
      const res = await saveSecretReference({
        workspace,
        secrets_file: file,
        reference: ref,
        is_update: false,
      });
      if (res.state === "completed" && res.document) {
        showSecrets(res.document);
        if (res.identity) setSecretsWritten({ file, identity: res.identity });
        setNewSecretName("");
        setNewSecretCommand("");
        setNewSecretArgs("");
        setNewSecretAddress("");
        setNewSecretMaxAge("");
        setFeedback("Secret reference registered successfully.");
        retainer.clear();
      } else {
        setFeedback(refused(res, "Failed to add secret reference."));
      }
    });
  }

  function openSecretEdit(ref: SecretReference) {
    setFeedback(null);
    setSecretEdit({
      opened: ref,
      store: ref.store,
      address: ref.address,
      command: ref.command,
      maxAge: ref.max_age ?? "",
      replaceArguments: false,
      replacementArguments: "",
    });
  }

  // Closes the edit. Focus returns to its reference only when the person
  // finished with it; naming another document closes it where they are typing.
  function closeSecretEdit(returnFocus: boolean) {
    returnFocusTo.current = returnFocus && secretEdit ? secretEdit.opened.name : null;
    setSecretEdit(null);
  }

  function updateSecretEdit(patch: Partial<SecretEdit>) {
    setSecretEdit((prev) => (prev ? { ...prev, ...patch } : prev));
  }

  // Cancelling an edit discards what was typed; nothing is written.
  function cancelSecretEdit() {
    if (!secretEdit) return;
    closeSecretEdit(true);
    setFeedback(`Edit of ${secretEdit.opened.name} cancelled; nothing was written.`);
  }

  // Handle Secret Reference Edit, through the same shared update the command
  // line uses, with only what the person changed. A refusal, one for an edit
  // that changed nothing included, keeps the edit open with what was typed.
  async function handleSaveSecretEdit() {
    if (!secretEdit) return;
    const name = secretEdit.opened.name;
    const file = currentSecretsFile;
    await actions.run("working", async () => {
      setFeedback(null);
      setSecretsWritten(null);
      const res = await saveSecretReference({
        workspace,
        secrets_file: file,
        reference: secretEdit.opened,
        is_update: true,
        change: secretChange(secretEdit),
      });
      if (res.state === "completed" && res.document) {
        showSecrets(res.document);
        if (res.identity) setSecretsWritten({ file, identity: res.identity });
        closeSecretEdit(true);
        // A test of the locator the edit replaced says nothing about this one.
        if (testResult?.name === name) setTestResult(null);
        setFeedback(`Credential reference ${name} updated.`);
      } else {
        setFeedback(refused(res, "Failed to update credential reference."));
      }
    });
  }

  // Handle Test Secret Reference
  async function handleTestSecret(name: string) {
    await actions.run("working", async () => {
      setTestResult(null);
      const res = await testSecretReference(workspace, currentSecretsFile, name);
      setTestResult(res);
      if (res.state !== "completed") {
        setFeedback(refused(res, "Secret test failed."));
      }
    });
  }

  // Handle Rotate Secret Reference
  async function handleRotateSecret(name: string) {
    await actions.run("working", async () => {
      const res = await rotateSecretReference(workspace, currentSecretsFile, name);
      if (res.state === "completed" && res.document) {
        showSecrets(res.document);
        // The document changed after the save the identity line named.
        setSecretsWritten(null);
        setFeedback(`Secret ${name} rotated successfully.`);
      } else {
        setFeedback(refused(res, "Rotation failed."));
      }
    });
  }

  // Handle Remove Secret Reference
  async function handleRemoveSecret(name: string) {
    await actions.run("working", async () => {
      const res = await removeSecretReference(workspace, currentSecretsFile, name);
      if (res.state === "completed" && res.document) {
        showSecrets(res.document);
        setSecretsWritten(null);
        if (secretEdit?.opened.name === name) closeSecretEdit(true);
        setFeedback(`Secret ${name} removed.`);
      } else {
        setFeedback(refused(res, "Failed to remove secret reference."));
      }
    });
  }

  // Handle Scan Secrets
  async function handleScanSecrets() {
    await actions.run("working", async () => {
      setScanResult(null);
      const res = await scanSecrets({
        workspace,
        secrets_file: currentSecretsFile,
        paths: [currentTargetFile, currentPolicyFile, currentPlanFile],
      });
      setScanResult(res);
    });
  }

  // Handle Add Policy Destination
  function handleAddDestination() {
    if (!policyReady) return;
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
    if (!policyReady) return;
    const updated = {
      ...policy,
      approved_destinations: policy.approved_destinations.filter((d) => d !== dest),
    };
    setPolicy(updated);
    retainDraft("environment/policy", "readmit-send-policy-draft/v1", updated);
  }

  // Handle Save Send Policy
  async function handleSavePolicy() {
    if (!policyReady) return;
    await actions.run("working", async () => {
      setFeedback(null);
      setPolicyWritten(null);
      const file = currentPolicyFile;
      const res = await saveSendPolicy({
        workspace,
        policy_file: file,
        policy,
      });
      if (res.state === "completed" && res.policy) {
        setPolicy(res.policy);
        if (res.identity) setPolicyWritten({ file, identity: res.identity });
        setFeedback("Approved-destination policy saved.");
        retainer.clear();
      } else {
        setFeedback(refused(res, "Failed to save policy."));
      }
    });
  }

  // Handle Local Policy Evaluation
  async function handleEvaluatePolicy() {
    await actions.run("working", async () => {
      setEvalDecision(null);
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
        setFeedback(refused(res, "Evaluation failed."));
      }
    });
  }

  // Handle Add Reset Action
  function handleAddResetAction() {
    if (!planReady) return;
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
    if (!planReady) return;
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
    if (!planReady) return;
    await actions.run("working", async () => {
      setFeedback(null);
      setPlanWritten(null);
      const file = currentPlanFile;
      const res = await saveResetPlan({
        workspace,
        plan_file: file,
        plan: resetPlan,
      });
      if (res.state === "completed" && res.plan) {
        setResetPlan(res.plan);
        if (res.identity) setPlanWritten({ file, identity: res.identity });
        setFeedback("Fixture reset plan saved.");
        retainer.clear();
        onPlanSaved?.();
      } else {
        setFeedback(refused(res, "Failed to save reset plan."));
      }
    });
  }

  // Handle Execute Reset
  async function handleExecuteReset() {
    await actions.run("working", async () => {
      setFeedback(null);
      setResetResult(null);
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
        setFeedback(refused(res, "Reset finished with errors or refusals."));
      }
    });
  }

  // A credential the target binds in a document other than the one loaded here,
  // or under a name that document does not list, is kept as its own choice.
  const bound = target.credential;
  const boundInOther = bound !== undefined && bound.secrets_file !== secretsReference(workspace, currentSecretsFile);
  const boundUnlisted =
    bound !== undefined && !boundInOther && !(secretsDoc?.references ?? []).some((r) => r.name === bound.reference);

  return (
    <section className="environment-panel" aria-labelledby="environment-panel-title">
      <h3 id="environment-panel-title">Environments</h3>

      {/* Persistent Environment Banner */}
      <EnvironmentBanner
        name={target.name}
        classification={target.classification}
        address={target.address}
        transport={target.transport}
      />

      {typeof feedback === "string" ? (
        <div role="status" aria-live="polite" className="report-box">
          <p>{feedback}</p>
        </div>
      ) : feedback ? (
        <div className="report-box">
          <Outcome result={feedback} />
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
          Target
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
          Send policy
        </button>
        <button
          type="button"
          className={`environment-tab ${activeTab === "reset" ? "active" : ""}`}
          onClick={() => setActiveTab("reset")}
        >
          Reset plan
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
                disabled={blocked}
                onChange={(e) => updateTarget({ name: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-classification">Classification</label>
              <select
                id="target-classification"
                value={target.classification || "unclassified"}
                disabled={blocked}
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
                disabled={blocked}
                onChange={(e) => updateTarget({ address: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-transport">Transport</label>
              <select
                id="target-transport"
                value={target.transport}
                disabled={blocked}
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
                disabled={blocked}
                onChange={(e) => updateTarget({ server_name: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-ca-file">CA Certificate File</label>
              <input
                id="target-ca-file"
                value={target.ca_file || ""}
                disabled={blocked}
                onChange={(e) => updateTarget({ ca_file: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-client-cert">Client Certificate File</label>
              <input
                id="target-client-cert"
                value={target.client_certificate || ""}
                disabled={blocked}
                onChange={(e) => updateTarget({ client_certificate: e.target.value })}
              />
            </div>


            <div className="environment-field">
              <label htmlFor="target-connect-timeout">Connect timeout</label>
              <input
                id="target-connect-timeout"
                value={target.connect_timeout}
                disabled={blocked}
                onChange={(e) => updateTarget({ connect_timeout: e.target.value })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-message-timeout">Message timeout</label>
              <input
                id="target-message-timeout"
                value={target.message_timeout}
                disabled={blocked}
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
                disabled={blocked}
                onChange={(e) => updateTarget({ max_ack_bytes: Number(e.target.value) })}
              />
            </div>

            <div className="environment-field">
              <label htmlFor="target-test-endpoint">
                <input
                  id="target-test-endpoint"
                  type="checkbox"
                  checked={target.test_endpoint}
                  disabled={blocked}
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
                  disabled={blocked}
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
                value={boundInOther || boundUnlisted ? KEEP_BINDING : target.credential?.reference || ""}
                disabled={blocked}
                onChange={(e) => {
                  const refName = e.target.value;
                  if (refName === KEEP_BINDING) return;
                  const updated: Target = { ...target };
                  if (refName) {
                    updated.credential = { secrets_file: secretsReference(workspace, currentSecretsFile), reference: refName };
                  } else {
                    delete updated.credential;
                  }
                  setTarget(updated);
                  retainDraft("environment/target", "readmit-target-draft/v1", updated);
                }}
              >
                <option value="">None (no client authentication secret)</option>
                {/* A binding the loaded document does not offer is shown as it is recorded,
                    so what the screen says is what a save records. */}
                {bound && (boundInOther || boundUnlisted) ? (
                  <option value={KEEP_BINDING}>
                    {bound.reference}
                    {boundInOther ? ` (bound in ${bound.secrets_file})` : secretsDoc ? ` (not listed in ${currentSecretsFile})` : ""}
                  </option>
                ) : null}
                {(secretsDoc?.references ?? []).map((r) => (
                  <option key={r.name} value={r.name}>
                    {r.name} ({r.purpose} · {r.store})
                  </option>
                ))}
              </select>
              {target.credential ? <p className="hint">Credential document saved with this target: <code>{target.credential.secrets_file}</code></p> : null}
            </div>
          </div>

          <div className="environment-actions">
            <button type="button" disabled={blocked} onClick={() => void handleSaveTarget()}>
              Save target
            </button>
            <button type="button" disabled={blocked} onClick={() => void handleCheckTarget()}>
              Test connection
            </button>
            <p className="hint">Checks the named target's reachability and TLS by making a connection; this is not a local preview.</p>
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
              onChange={(e) => {
                setCurrentSecretsFile(e.target.value);
                setSecretsWritten(null);
                closeSecretEdit(false);
              }}
            />
          </div>

          <div className="provisioning-box">
            <h5>Native Store & Customer-Vault Provisioning Handoff</h5>
            <ol>
              <li>Store your credential in your OS keychain (e.g. macOS Keychain, Linux Secret Service) or customer vault.</li>
              <li>Provide the absolute path of the locator program that prints the secret to stdout and its arguments, one per line (e.g. <code>/usr/bin/security</code> with <code>find-generic-password</code>, <code>-s</code>, <code>readmit</code> and <code>-w</code>).</li>
              <li>Readmit invokes the locator strictly on-demand in memory and clears memory immediately after use.</li>
            </ol>
          </div>

          <table className="environment-table" aria-label="Registered credential references" ref={secretsTable} tabIndex={-1}>
            <thead>
              <tr>
                <th>Name</th>
                <th>Purpose</th>
                <th>Store</th>
                <th>Address</th>
                <th>Command Locator</th>
                <th>Secret Value</th>
                <th>Rotation Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {(secretsDoc?.references ?? []).length === 0 ? (
                <tr>
                  <td colSpan={8}>{secretsRefusal ?? "No credential references registered yet."}</td>
                </tr>
              ) : (
                (secretsDoc?.references ?? []).map((r) => (
                  <tr key={r.name}>
                    <td><strong>{r.name}</strong></td>
                    <td>{r.purpose}</td>
                    <td>{r.store}</td>
                    <td><code>{r.address}</code></td>
                    {/* Arguments are counted, never echoed, as `readmit secret show` counts them. */}
                    <td><code>{r.command}</code> ({argumentCount(r)} locator arguments)</td>
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
                          disabled={blocked}
                          onClick={() => void handleTestSecret(r.name)}
                          title="Check credential reference"
                        >
                          Test
                        </button>
                        <button
                          type="button"
                          disabled={blocked}
                          onClick={() => void handleRotateSecret(r.name)}
                        >
                          Rotate
                        </button>
                        <button
                          type="button"
                          aria-label={`Edit ${r.name}`}
                          data-edit={r.name}
                          disabled={blocked || secretEdit !== null}
                          onClick={() => openSecretEdit(r)}
                        >
                          Edit
                        </button>
                        <button
                          type="button"
                          disabled={blocked}
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

          {secretEdit ? (
            <form
              aria-label={`Edit credential reference ${secretEdit.opened.name}`}
              onSubmit={(e) => {
                e.preventDefault();
                void handleSaveSecretEdit();
              }}
              onKeyDown={(e) => {
                // Escape that dismisses an input method's composition is not a
                // cancellation. Escape that cancels the edit goes no further:
                // the window's own Escape cancels a running operation.
                if (e.key === "Escape" && !e.nativeEvent.isComposing && !busy) {
                  e.preventDefault();
                  e.stopPropagation();
                  cancelSecretEdit();
                }
              }}
            >
              <h5>Edit Credential Reference {secretEdit.opened.name}</h5>
              <p className="hint">
                Purpose: {secretEdit.opened.purpose}. The name and purpose are what the stored credential is for, so an edit
                keeps both: a credential for another purpose is a different reference, registered under its own name.
                The recorded generation and rotation time stay as they are, because re-pointing a reference is not a
                rotation.
              </p>
              <div className="environment-form-grid">
                <div className="environment-field">
                  <label htmlFor="edit-secret-store">Store</label>
                  <select
                    id="edit-secret-store"
                    ref={editFirstField}
                    value={secretEdit.store}
                    disabled={blocked}
                    onChange={(e) => updateSecretEdit({ store: e.target.value as SecretStore })}
                  >
                    <option value="os-keychain">OS Keychain</option>
                    <option value="customer-managed">Customer Managed</option>
                  </select>
                </div>
                <div className="environment-field">
                  <label htmlFor="edit-secret-address">Target Address Constraint</label>
                  <input
                    id="edit-secret-address"
                    value={secretEdit.address}
                    placeholder="host:port"
                    disabled={blocked}
                    onChange={(e) => updateSecretEdit({ address: e.target.value })}
                  />
                </div>
                <div className="environment-field">
                  <label htmlFor="edit-secret-command">Locator Command (Path)</label>
                  <input
                    id="edit-secret-command"
                    value={secretEdit.command}
                    disabled={blocked}
                    onChange={(e) => updateSecretEdit({ command: e.target.value })}
                  />
                </div>
                <div className="environment-field">
                  <label htmlFor="edit-secret-max-age">Maximum Rotation Age</label>
                  <input
                    id="edit-secret-max-age"
                    value={secretEdit.maxAge}
                    placeholder="720h, or empty for not declared"
                    disabled={blocked}
                    onChange={(e) => updateSecretEdit({ maxAge: e.target.value })}
                  />
                </div>
                <div className="environment-field full-width">
                  <label htmlFor="edit-secret-replace-args">
                    <input
                      id="edit-secret-replace-args"
                      type="checkbox"
                      checked={secretEdit.replaceArguments}
                      disabled={blocked}
                      onChange={(e) => updateSecretEdit({ replaceArguments: e.target.checked })}
                    />
                    Replace the {argumentCount(secretEdit.opened)} registered locator arguments
                  </label>
                  <p className="hint">
                    The registered arguments are counted, never shown: an argument is the one place a credential could
                    have been put.
                  </p>
                </div>
                {secretEdit.replaceArguments ? (
                  <div className="environment-field full-width">
                    <label htmlFor="edit-secret-args">Replacement Locator Arguments (one per line; empty clears them)</label>
                    <textarea
                      id="edit-secret-args"
                      rows={3}
                      value={secretEdit.replacementArguments}
                      disabled={blocked}
                      onChange={(e) => updateSecretEdit({ replacementArguments: e.target.value })}
                    />
                  </div>
                ) : null}
              </div>
              <div className="environment-actions">
                <button type="submit" disabled={blocked}>
                  Save changes
                </button>
                <button type="button" disabled={busy} onClick={cancelSecretEdit}>
                  Cancel Editing
                </button>
                <p className="hint">Saves the credential reference <code>{secretEdit.opened.name}</code></p>
              </div>
            </form>
          ) : null}

          {/* One form at a time: an open edit has the registration's fields. */}
          {secretEdit ? null : (
            <>
              <h5>Register New Credential Reference</h5>
              <div className="environment-form-grid">
                <div className="environment-field">
                  <label htmlFor="secret-name">Reference Name</label>
                  <input
                    id="secret-name"
                    value={newSecretName}
                    disabled={blocked}
                    onChange={(e) => setNewSecretName(e.target.value)}
                  />
                </div>
                <div className="environment-field">
                  <label htmlFor="secret-store">Store</label>
                  <select
                    id="secret-store"
                    value={newSecretStore}
                    disabled={blocked}
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
                    disabled={blocked}
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
                    disabled={blocked}
                    onChange={(e) => setNewSecretAddress(e.target.value)}
                  />
                </div>
                <div className="environment-field">
                  <label htmlFor="secret-command">Locator Command (Path)</label>
                  <input
                    id="secret-command"
                    value={newSecretCommand}
                    placeholder="/usr/bin/security"
                    disabled={blocked}
                    onChange={(e) => setNewSecretCommand(e.target.value)}
                  />
                </div>
                <div className="environment-field">
                  <label htmlFor="secret-args">Locator Arguments (one per line)</label>
                  <textarea
                    id="secret-args"
                    rows={3}
                    value={newSecretArgs}
                    placeholder={"find-generic-password\n-s\nsvc\n-w"}
                    disabled={blocked}
                    onChange={(e) => setNewSecretArgs(e.target.value)}
                  />
                </div>
                <div className="environment-field">
                  <label htmlFor="secret-max-age">Maximum Rotation Age</label>
                  <input
                    id="secret-max-age"
                    value={newSecretMaxAge}
                    placeholder="720h, or empty for not declared"
                    disabled={blocked}
                    onChange={(e) => setNewSecretMaxAge(e.target.value)}
                  />
                </div>
              </div>
            </>
          )}
          <div className="environment-actions">
            {secretEdit ? null : (
              <button type="button" disabled={blocked} onClick={() => void handleAddSecret()}>
                Add credential reference
              </button>
            )}
            <button type="button" disabled={blocked} onClick={() => void handleScanSecrets()}>
              Scan for leaks
            </button>
            <p className="hint">Scans this workspace's target, policy and plan files for residual credential values; see the report for the scan's limits.</p>
          </div>
          <WrittenIdentity written={secretsWritten} />

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
              onChange={(e) => {
                setCurrentPolicyFile(e.target.value);
                setPolicyWritten(null);
                setPolicyReady(false);
                setPolicyRefusal(null);
                policyDraftPending.current = false;
              }}
            />
          </div>

          {policyRefusal ? (
            <div className="report-box" role="status">
              <p>{policyRefusal}</p>
              <button type="button" disabled={blocked} onClick={() => {
                setPolicy(newPolicy());
                setPolicyReady(true);
                setPolicyRefusal(null);
                }}>New send policy</button>
            </div>
          ) : null}

          <h5>Approved CIDR Prefixes</h5>
          <ul style={{ listStyle: "none", paddingLeft: 0 }}>
            {policy.approved_destinations.map((dest) => (
              <li key={dest} style={{ display: "flex", gap: "0.5rem", alignItems: "center", marginBottom: "0.4rem" }}>
                <code>{dest}</code>
                <button
                  type="button"
                  disabled={blocked || !policyReady || policy.approved_destinations.length <= 1}
                  onClick={() => handleRemoveDestination(dest)}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>

          {/* A form, so Enter in the prefix adds it. */}
          <form
            style={{ display: "flex", gap: "0.5rem", marginTop: "0.5rem" }}
            onSubmit={(e) => {
              e.preventDefault();
              handleAddDestination();
            }}
          >
            <input
              aria-label="Approved destination prefix"
              placeholder="network/prefix"
              value={newDestination}
              disabled={blocked || !policyReady}
              onChange={(e) => setNewDestination(e.target.value)}
            />
            <button type="submit" disabled={blocked || !policyReady || !newDestination}>
              Add CIDR range
            </button>
          </form>
          <p className="hint">Accepted syntax: an IPv4 or IPv6 CIDR range, such as 127.0.0.0/8 or 10.1.0.0/16.</p>

          <div className="environment-actions" style={{ marginTop: "1rem" }}>
            <button type="button" disabled={blocked || !policyReady} onClick={() => void handleSavePolicy()}>
              Save policy
            </button>
            <p className="hint">Saving records the approved destinations and their network scope; it is not send authorization.</p>
          </div>
          <WrittenIdentity written={policyWritten} />

          <hr style={{ margin: "1.5rem 0", borderColor: "var(--line, #ccc)" }} />

          <h5>Local Destination Evaluation (No Network Connection)</h5>
          <div className="environment-form-grid">
            <div className="environment-field">
              <label htmlFor="eval-address">Destination Address</label>
              <input
                id="eval-address"
                value={evalAddress}
                disabled={blocked}
                onChange={(e) => setEvalAddress(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="eval-class">Target Classification</label>
              <select
                id="eval-class"
                value={evalClassification}
                disabled={blocked}
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
                disabled={blocked}
                onChange={(e) => setEvalExplicit(e.target.checked)}
              />
              Explicit send intention declared
            </label>
          </div>
          <button type="button" disabled={blocked} onClick={() => void handleEvaluatePolicy()}>
            Check destination
          </button>
          <p className="hint">Checked locally; no connection is opened.</p>

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
          <h4 id="reset-section-title">Fixture reset (readmit-reset-plan/v1)</h4>
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
                onChange={(e) => {
                  setCurrentPlanFile(e.target.value);
                  setPlanWritten(null);
                  setPlanReady(false);
                  setPlanRefusal(null);
                  planDraftPending.current = false;
                }}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="plan-env-input">Environment Name Match</label>
              <input
                id="plan-env-input"
                value={resetPlan.environment}
                disabled={blocked || !planReady}
                onChange={(e) => {
                  const updated = { ...resetPlan, environment: e.target.value };
                  setResetPlan(updated);
                  retainDraft("environment/reset", "readmit-reset-plan-draft/v1", updated);
                }}
              />
            </div>
          </div>

          {planRefusal ? (
            <div className="report-box" role="status">
              <p>{planRefusal}</p>
              <button type="button" disabled={blocked} onClick={() => {
                setResetPlan(newPlan());
                setPlanReady(true);
                setPlanRefusal(null);
                }}>New reset plan</button>
            </div>
          ) : null}

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
                    disabled={blocked || !planReady}
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
                      disabled={blocked || !planReady}
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
                disabled={blocked || !planReady}
                onChange={(e) => setNewActionId(e.target.value)}
              />
            </div>
            <div className="environment-field">
              <label htmlFor="action-operator">Reviewed Operator</label>
              <select
                id="action-operator"
                value={newActionOperator}
                disabled={blocked || !planReady}
                onChange={(e) => setNewActionOperator(e.target.value as ResetOperator)}
              >
                <option value="operator_confirms">operator_confirms (Human confirmation required; authority: none)</option>
                <option value="observation_empty">observation_empty (Verifies receiver ledger is empty; authority: read_declared_file)</option>
                <option value="endpoint_quiet">endpoint_quiet (Verifies endpoint connectivity without sending HL7; authority: connect_approved_target)</option>
              </select>
            </div>
            <div className="environment-field full-width">
              <label htmlFor="action-instructions">Reset instructions</label>
              <textarea
                id="action-instructions"
                rows={2}
                value={newActionInstructions}
                placeholder="Describe exact manual action or side effect for the operator..."
                disabled={blocked || !planReady}
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
                  disabled={blocked || !planReady}
                  onChange={(e) => setNewActionObservation(e.target.value)}
                />
              </div>
            ) : null}
          </div>

          <div className="environment-actions">
            <button type="button" disabled={blocked || !planReady} onClick={handleAddResetAction}>
              Add action
            </button>
            <button type="button" disabled={blocked || !planReady} onClick={() => void handleSavePlan()}>
              Save plan
            </button>
            <button
              type="button"
              disabled={blocked || !planReady || resetPlan.actions.length === 0}
              onClick={() => void handleExecuteReset()}
              style={{ fontWeight: "bold" }}
            >
              Reset fixture
            </button>
            <p className="hint">Resets the fixture at target <code>{target.name || target.address || currentTargetFile}</code> by running the named actions and side effects listed above, each still requiring its explicit confirmation. Reset is not undo.</p>
          </div>
          <WrittenIdentity written={planWritten} />

          {resetResult ? (
            <div
              className={`outcome-card ${resetResult.state === "passed" ? "passed" : "failed"}`}
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
