import { useEffect, useRef, useState } from "react";
import {
  captureProgress,
  chooseCapturePath,
  collectSource,
  diagnoseSource,
  finalizeCaptureImport,
  openCaptureJournal,
  previewCapture,
  readReceiverPolicy,
  readSourceRegistration,
  saveReceiverPolicy,
  saveSourceRegistration,
  startCapture,
  type CaptureJournalResult,
  type CaptureObservationBinding,
  type CapturePreviewResult,
  type CaptureSessionResult,
  type EvidenceSource,
  type EvidenceSourceKind,
  type ImportCommitResult,
  type ImportPlan,
  type ReceiverPolicy,
  type SourceAccessResult,
  type SourceCollectionResult,
} from "./bindings";
import type { Indicators } from "./shell";
import { Status } from "./shell";
import "./capture.css";
import { useLifecycle } from "./lifecycle";

type Mode = "source" | "collect" | "listen";

function defaultSource(): EvidenceSource {
  return {
    schema: "readmit-source/v1",
    name: "exports",
    kind: "directory",
    scope: "appointments",
    quota: { max_entries: 64, max_entry_bytes: 4194304, max_total_bytes: 33554432 },
    retry: { attempts: 1, backoff: "250ms" },
    root: "",
  };
}

function defaultPolicy(): ReceiverPolicy {
  return {
    schema: "readmit-receiver-policy/v1",
    name: "downstream-sink",
    source_label: "downstream-test-endpoint",
    acknowledgement: { operator: "original-mode-fixed-code", code: "AA" },
    accepted_message_types: { operator: "any-message-type", values: [] },
  };
}

/** How often a running capture's bound address is read until it is known. */
const PROGRESS_MS = 200;

type FaultAction = "none" | "delay" | "reject" | "missing-response" | "disconnect" | "malformed-ack";

// What the enhanced and fault controls show for a declared policy. The
// controls express one enhanced rule and one fault step; a reopened document
// keeps whatever else it declares until one of them is changed.
const declaresEnhanced = (p: ReceiverPolicy) => p.enhanced_acknowledgement?.operator === "enhanced-mode-fixed-codes";
const declaredFault = (p: ReceiverPolicy): FaultAction => (p.faults?.steps[0]?.action as FaultAction | undefined) ?? "none";
const declaredDelay = (p: ReceiverPolicy) => p.faults?.steps[0]?.delay_ms ?? 50;
const waitsForFault = (action: FaultAction) => action === "delay" || action === "missing-response";
const nonloopbackRefusal = "accepting connections from beyond this machine is opt-in: pass --approved-bind to bind a nonloopback address";

function captureReason(reason: string, mode: Mode): string {
  if (reason !== nonloopbackRefusal) return reason;
  return mode === "collect"
    ? "To bind beyond this machine, select Approved nonloopback bind in this window."
    : "Choose a loopback listen address for the SIU fixture.";
}

/** The enhanced acknowledgement as `readmit collect` names it on start. */
function enhancedSummary(p: ReceiverPolicy): string {
  const rule = p.enhanced_acknowledgement;
  if (!rule || !declaresEnhanced(p)) return "unsupported";
  return `${rule.operator} ${rule.accept_code} ${rule.application_code} ${rule.application_delivery}`;
}

/** A reopened responder policy, every member as the saved document declares it. */
function PolicyReview({ file, policy }: { file: string; policy: ReceiverPolicy }) {
  const types = policy.accepted_message_types;
  return (
    <dl className="capture-preview" aria-label="Opened responder policy">
      <dt>File</dt>
      <dd>{file}</dd>
      <dt>Schema</dt>
      <dd>{policy.schema}</dd>
      <dt>Name</dt>
      <dd>{policy.name}</dd>
      <dt>Source label</dt>
      <dd>{policy.source_label}</dd>
      <dt>Acknowledgement</dt>
      <dd>
        {policy.acknowledgement.operator} {policy.acknowledgement.code}
      </dd>
      <dt>Accepted message types</dt>
      <dd>{types.values.length ? `${types.operator}: ${types.values.join(", ")}` : types.operator}</dd>
      <dt>Enhanced acknowledgement</dt>
      <dd>{enhancedSummary(policy)}</dd>
      {policy.faults ? (
        <>
          <dt>Faults</dt>
          <dd>
            {policy.faults.environment_class} · approved {policy.faults.approved_test_endpoints.join(", ")} ·{" "}
            {policy.faults.steps
              .map((step) => `message ${step.message} ${step.stage} ${step.action}${step.delay_ms ? ` ${step.delay_ms} ms` : ""}`)
              .join("; ")}
          </dd>
        </>
      ) : null}
    </dl>
  );
}

/** A reopened source registration, every member its kind declares. */
function SourceReview({ file, source }: { file: string; source: EvidenceSource }) {
  return (
    <dl className="capture-preview" aria-label="Opened source registration">
      <dt>File</dt>
      <dd>{file}</dd>
      <dt>Schema</dt>
      <dd>{source.schema}</dd>
      <dt>Name</dt>
      <dd>{source.name}</dd>
      <dt>Kind</dt>
      <dd>{source.kind}</dd>
      <dt>Scope</dt>
      <dd>{source.scope}</dd>
      <dt>Quota</dt>
      <dd>
        {source.quota.max_entries} entries, {source.quota.max_entry_bytes} bytes per entry, {source.quota.max_total_bytes}{" "}
        bytes in total
      </dd>
      <dt>Retry</dt>
      <dd>
        {source.retry.attempts} attempts, backoff {source.retry.backoff}
      </dd>
      {source.root ? (
        <>
          <dt>Export folder</dt>
          <dd>{source.root}</dd>
        </>
      ) : null}
      {source.address ? (
        <>
          <dt>Address</dt>
          <dd>
            {source.address} ({source.classification})
          </dd>
        </>
      ) : null}
      {source.command ? (
        <>
          <dt>Transfer program</dt>
          <dd>{[source.command, ...(source.arguments ?? [])].join(" ")}</dd>
        </>
      ) : null}
      {source.credential ? (
        <>
          <dt>Credential reference</dt>
          <dd>
            {source.credential} in {source.secrets_file}
          </dd>
        </>
      ) : null}
    </dl>
  );
}

/** The panel's operations; each one disables every control while it runs. */
type CaptureOperation =
  | "choosing"
  | "saving-source"
  | "opening-source"
  | "opening-policy"
  | "diagnosing"
  | "collecting-source"
  | "previewing"
  | "listening"
  | "collecting"
  | "recovering"
  | "finalizing";

export function CapturePanel({
  workspace,
  project,
  busy,
  indicators,
  onOpenCase,
  onSetupIndex,
  onBindObservation,
  onClose,
}: {
  workspace: string;
  project: string | null;
  busy: boolean;
  indicators: Indicators;
  onOpenCase: (caseName: string) => void;
  onSetupIndex?: (caseName: string) => void;
  onBindObservation?: (binding: CaptureObservationBinding) => void;
  onClose: () => void;
}) {
  const [mode, setMode] = useState<Mode>("source");
  const [source, setSource] = useState<EvidenceSource>(defaultSource);
  const [sourceFile, setSourceFile] = useState("source.json");
  const [planFraming, setPlanFraming] = useState<"raw" | "mllp">("raw");
  const [planTerminator, setPlanTerminator] = useState<"cr" | "lf" | "crlf">("cr");
  const [planMembers, setPlanMembers] = useState(".hl7");
  const [policyFile, setPolicyFile] = useState("receiver-policy.json");
  const [policy, setPolicy] = useState<ReceiverPolicy>(defaultPolicy);
  const [enhanced, setEnhanced] = useState(false);
  const [faultAction, setFaultAction] = useState<FaultAction>("none");
  const [faultDelayMs, setFaultDelayMs] = useState(50);
  // The documents reopened from disk, as they were read or last saved, and
  // whether a policy control changed since.
  const [openedSource, setOpenedSource] = useState<{ file: string; source: EvidenceSource } | null>(null);
  const [openedPolicy, setOpenedPolicy] = useState<{ file: string; policy: ReceiverPolicy } | null>(null);
  const [policyEdited, setPolicyEdited] = useState(false);
  const [listeningOn, setListeningOn] = useState<string | null>(null);
  const [address, setAddress] = useState("127.0.0.1:0");
  const [addressEdited, setAddressEdited] = useState(false);
  const [approvedBind, setApprovedBind] = useState(false);
  const [outputName, setOutputName] = useState("capture.case");
  const [journalName, setJournalName] = useState("capture.journal");
  const [observationName, setObservationName] = useState("observation.json");
  const [fixtureMode, setFixtureMode] = useState<"fixed" | "defective">("fixed");
  const [maxMessages, setMaxMessages] = useState(0);
  const [maxConnections, setMaxConnections] = useState(1);
  const [tlsCert, setTlsCert] = useState("");
  const [tlsKeyRef, setTlsKeyRef] = useState("");
  const [secretsFile, setSecretsFile] = useState("");
  const [clientCA, setClientCA] = useState("");
  const [stagedFolder, setStagedFolder] = useState("");
  const [stagedReceipt, setStagedReceipt] = useState("");
  // A browser takes focus from a control it disables, so a running capture
  // hands it to its Cancel control and every action returns it afterwards to
  // the control that started it. A capture, collecting or listening, runs
  // under the facade's capture operation, which that Cancel stops.
  const cancelControl = useRef<HTMLButtonElement>(null);
  const lifecycle = useLifecycle<CaptureOperation>({
    names: { collecting: "capture", listening: "capture" },
    stops: { collecting: cancelControl, listening: cancelControl },
  });
  const operation = lifecycle.running;
  const [access, setAccess] = useState<SourceAccessResult | null>(null);
  const [collection, setCollection] = useState<SourceCollectionResult | null>(null);
  const [preview, setPreview] = useState<CapturePreviewResult | null>(null);
  const [session, setSession] = useState<CaptureSessionResult | null>(null);
  const [journal, setJournal] = useState<CaptureJournalResult | null>(null);
  const [finalized, setFinalized] = useState<ImportCommitResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const locked = busy || operation !== null;
  const serving = operation === "listening" || operation === "collecting";
  // While a capture runs, the screen reads where it listens: with a port of 0
  // that is the only place the port a sender needs is known. The read never
  // waits for the capture and stops once it answers.
  useEffect(() => {
    if (!serving) return;
    let stopped = false;
    const poll = async () => {
      const answer = await captureProgress();
      if (!stopped && answer.state === "completed" && answer.progress) {
        stopped = true;
        clearInterval(timer);
        setListeningOn(answer.progress.bound_address);
      }
    };
    const timer = setInterval(() => void poll(), PROGRESS_MS);
    void poll();
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [serving]);

  function observationBinding(caseName: string): CaptureObservationBinding {
    return {
      case_path: caseName,
      identity: "downstream-capture",
      scope: "appointments",
      kinds: ["message"],
      record_key: "SCH-1.1",
      max_occurrences: 100,
      freshness: "1h",
    };
  }


  async function pick(kind: string, apply: (path: string) => void) {
    setError(null);
    await lifecycle.run("choosing", async () => {
      const result = await chooseCapturePath(kind);
      if (result.state === "completed" && result.paths?.[0]) apply(result.paths[0]);
      else if (result.state !== "cancelled") setError(result.reason ?? result.state);
    });
  }


  function importPlan(): ImportPlan {
    return {
      schema: "readmit-import-plan/v1",
      framing: planFraming,
      terminator: planTerminator,
      encoding: "utf-8",
      direction: "inbound",
      members: planMembers.split(/\s+/).filter(Boolean),
    };
  }

  async function saveSource() {
    setError(null);
    await lifecycle.run("saving-source", async () => {
      const result = await saveSourceRegistration({ workspace, source_file: sourceFile, source });
      if (result.state !== "completed") setError(result.reason ?? result.state);
      else if (openedSource) setOpenedSource({ file: sourceFile, source: result.source ?? source });
    });
  }

  // Opening a declared document reads it through the command line's own
  // reader and fills the form with it; what the form does not show is kept as
  // declared. A dismissed dialog opens nothing, and a refused document leaves
  // the form as it was.
  async function chosenFile(kind: "source" | "policy"): Promise<string | null> {
    const chosen = await chooseCapturePath(kind);
    const file = chosen.paths?.[0];
    if (chosen.state === "completed" && file) return file;
    if (chosen.state !== "cancelled") setError(chosen.reason ?? chosen.state);
    return null;
  }

  async function openRegistration() {
    setError(null);
    await lifecycle.run("opening-source", async () => {
      const file = await chosenFile("source");
      if (!file) return;
      const opened = await readSourceRegistration(workspace, file);
      if (opened.state !== "completed" || !opened.source) {
        setError(opened.reason ?? opened.state);
        return;
      }
      setSource(opened.source);
      setSourceFile(file);
      setOpenedSource({ file, source: opened.source });
      setAccess(null);
      setCollection(null);
    });
  }

  async function openPolicy() {
    setError(null);
    await lifecycle.run("opening-policy", async () => {
      const file = await chosenFile("policy");
      if (!file) return;
      const opened = await readReceiverPolicy(workspace, file);
      if (opened.state !== "completed" || !opened.policy) {
        setError(opened.reason ?? opened.state);
        return;
      }
      const declared = opened.policy;
      setPolicy(declared);
      setEnhanced(declaresEnhanced(declared));
      setFaultAction(declaredFault(declared));
      setFaultDelayMs(declaredDelay(declared));
      // A saved fault policy names its exact test endpoints. Fill an untouched
      // ephemeral-port default from that declaration so its first preview can
      // use the policy the person just chose; preserve any address they set.
      if (!addressEdited && address === "127.0.0.1:0" && declared.faults?.approved_test_endpoints[0]) {
        setAddress(declared.faults.approved_test_endpoints[0]);
      }
      setPolicyFile(file);
      setOpenedPolicy({ file, policy: declared });
      setPolicyEdited(false);
      setPreview(null);
    });
  }

  function editPolicy<T>(set: (value: T) => void) {
    return (value: T) => {
      set(value);
      setPolicyEdited(true);
    };
  }

  function editAddress(value: string) {
    setAddress(value);
    setAddressEdited(true);
  }

  function keepsDeclaredPolicyControls(): boolean {
    const declared = openedPolicy?.policy;
    return !!declared && enhanced === declaresEnhanced(declared) && faultAction === declaredFault(declared) &&
      (faultAction === "none" || faultDelayMs === declaredDelay(declared));
  }

  /** The policy the form describes. A reopened document keeps the enhanced
   * rule and faults it declares while those controls still show what it
   * declared, including its approved test endpoints; changing one of them
   * replaces them with what the controls express. */
  function policyToSave(): ReceiverPolicy {
    const base = {
      name: policy.name,
      source_label: policy.source_label,
      acknowledgement: policy.acknowledgement,
      accepted_message_types: policy.accepted_message_types,
    };
    const declared = openedPolicy?.policy;
    if (declared && keepsDeclaredPolicyControls()) {
      const kept: ReceiverPolicy = { schema: declared.schema, ...base };
      if (declared.enhanced_acknowledgement) kept.enhanced_acknowledgement = declared.enhanced_acknowledgement;
      if (declared.faults) kept.faults = declared.faults;
      return kept;
    }
    const schema = enhanced ? "readmit-receiver-policy/v2" : "readmit-receiver-policy/v1";
    const toSave: ReceiverPolicy = { schema, ...base };
    if (enhanced) {
      toSave.enhanced_acknowledgement = {
        operator: "enhanced-mode-fixed-codes",
        accept_code: "CA",
        application_code: "AA",
        application_delivery: "same-connection",
        application_endpoint: "",
        approved_transport: false,
      };
    }
    if (faultAction !== "none") {
      toSave.schema = "readmit-receiver-policy/v3";
      if (!toSave.enhanced_acknowledgement) {
        toSave.enhanced_acknowledgement = {
          operator: "unsupported",
          accept_code: "",
          application_code: "",
          application_delivery: "",
          application_endpoint: "",
          approved_transport: false,
        };
      }
      toSave.faults = {
        environment_class: "nonproduction",
        approved_test_endpoints: [address],
        steps: [{ message: 1, stage: "application", action: faultAction, delay_ms: waitsForFault(faultAction) ? faultDelayMs : 0 }],
      };
    }
    return toSave;
  }

  async function runDiagnose() {
    setError(null);
    setAccess(null);
    await lifecycle.run("diagnosing", async () => {
      const result = await diagnoseSource({ workspace, source_file: sourceFile, plan: importPlan() });
      setAccess(result);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    });
  }

  async function runCollectSource() {
    setError(null);
    setCollection(null);
    await lifecycle.run("collecting-source", async () => {
      const result = await collectSource({
        workspace,
        source_file: sourceFile,
        plan: importPlan(),
        output_name: "collected",
        receipt_name: "collection.json",
      });
      setCollection(result);
      // Only a completed collection is finalized: what an incomplete one
      // staged is not the whole of its scope.
      if (result.state === "completed" && result.output_path) {
        setStagedFolder(result.output_path);
        setStagedReceipt(result.receipt_path ?? "");
      }
      if (result.state !== "completed") setError(result.reason ?? result.state);
    });
  }

  function captureRequest() {
    const req: Parameters<typeof previewCapture>[0] = {
      workspace,
      kind: mode === "listen" ? "listen" : "collect",
      address,
      // The fixture listens on loopback only: the collector's approval of a
      // nonloopback bind never reaches it.
      approved_bind: mode === "collect" && approvedBind,
      output_name: outputName,
      max_messages: maxMessages,
      max_connections: maxConnections,
      idle_timeout: "30s",
    };
    if (mode === "collect") req.policy_file = policyFile;
    if (mode === "listen") {
      req.fixture_mode = fixtureMode;
      req.observation_name = observationName;
    }
    if (mode === "collect" && journalName) req.journal_name = journalName;
    if (tlsCert) req.tls_certificate_file = tlsCert;
    if (tlsKeyRef) req.tls_key_reference = tlsKeyRef;
    if (secretsFile) req.secrets_file = secretsFile;
    if (clientCA) req.client_ca_file = clientCA;
    return req;
  }

  async function runPreview() {
    setError(null);
    setPreview(null);
    await lifecycle.run("previewing", async () => {
      // Previewing saves what the form describes, except a reopened policy
      // nothing has changed since: that stays the document on disk, byte for
      // byte, and is previewed as it is.
      if (mode === "collect" && !(openedPolicy && openedPolicy.file === policyFile && !policyEdited)) {
        const toSave = policyToSave();
        const saved = await saveReceiverPolicy({ workspace, policy_file: policyFile, policy: toSave });
        if (saved.state !== "completed") {
          setError(saved.reason ?? saved.state);
          return;
        }
        if (openedPolicy) {
          setOpenedPolicy({ file: policyFile, policy: saved.policy ?? toSave });
          setPolicyEdited(false);
        }
      }
      const result = await previewCapture(captureRequest());
      setPreview(result);
      if (result.state !== "completed") setError(captureReason(result.reason ?? result.state, mode));
    });
  }

  async function runStart() {
    setError(null);
    setSession(null);
    setListeningOn(null);
    await lifecycle.run(mode === "listen" ? "listening" : "collecting", async () => {
      const result = await startCapture(captureRequest());
      setSession(result);
      if (result.state !== "completed" && result.state !== "cancelled") {
        setError(captureReason(result.reason ?? result.state, mode));
      }
      if (result.case_path && mode === "collect") {
        // Collected case is already a verified case destination from the collector.
      }
    });
  }

  async function runJournal() {
    setError(null);
    setJournal(null);
    await lifecycle.run("recovering", async () => {
      const result = await openCaptureJournal(workspace, journalName);
      setJournal(result);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    });
  }

  async function runFinalize() {
    setError(null);
    setFinalized(null);
    await lifecycle.run("finalizing", async () => {
      const folder = stagedFolder || "collected";
      const request: Parameters<typeof finalizeCaptureImport>[0] = {
        workspace,
        folder,
        collection_receipt: stagedReceipt || "collection.json",
        output_name: "imported-from-capture.case",
        register_in_project: Boolean(project),
      };
      if (project) request.project = project;
      const result = await finalizeCaptureImport(request);
      setFinalized(result);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    });
  }

  const phase =
    operation === "listening"
      ? "listening"
      : operation === "collecting"
        ? "collecting"
        : operation === "previewing"
          ? "previewing"
          : session?.phase ?? preview?.phase ?? journal?.phase ?? "idle";

  return (
    <section className="capture-panel" aria-labelledby="capture-title">
      <header className="capture-header">
        <h2 id="capture-title">Capture</h2>
        <button type="button" disabled={locked} onClick={onClose}>
          Close
        </button>
      </header>
      <p>
        Register approved sources, run the MLLP collector, or use the built-in synthetic SIU fixture.
        Start only after preview. Cancel uses the shared engine.
      </p>
      <div className="capture-modes" role="tablist" aria-label="Capture mode">
        {(["source", "collect", "listen"] as Mode[]).map((value) => (
          <button
            key={value}
            type="button"
            role="tab"
            aria-selected={mode === value}
            disabled={locked}
            onClick={() => setMode(value)}
          >
            {value === "source" ? "Source" : value === "collect" ? "MLLP collect" : "SIU fixture"}
          </button>
        ))}
      </div>
      <p role="status" aria-live="polite" className="capture-phase">
        Phase: {phase}
        {serving && listeningOn ? ` · Listening on ${listeningOn}` : null}
        {session?.received != null ? ` · Received: ${session.received}` : null}
        {session?.connections != null ? ` · Connections: ${session.connections}` : null}
        {journal?.journal ? ` · Journal received: ${journal.journal.received}` : null}
      </p>
      {error ? <Status indicator={indicators.get("failed")} state="failed" reason={error} /> : null}
      {session?.state === "cancelled" && !serving ? (
        <Status indicator={indicators.get("cancelled")} state="cancelled" reason={session.reason} />
      ) : null}

      {mode === "source" ? (
        <div className="capture-section">
          <h3>Approved source</h3>
          <div className="capture-row">
            <label>
              Registration file
              <input value={sourceFile} disabled={locked} onChange={(e) => setSourceFile(e.target.value)} />
            </label>
            <button type="button" disabled={locked} onClick={() => void openRegistration()}>
              Open registration…
            </button>
          </div>
          {openedSource ? <SourceReview file={openedSource.file} source={openedSource.source} /> : null}
          <label>
            Name
            <input
              value={source.name}
              disabled={locked}
              onChange={(e) => setSource({ ...source, name: e.target.value })}
            />
          </label>
          <label>
            Kind
            <select
              value={source.kind}
              disabled={locked}
              onChange={(e) => setSource({ ...source, kind: e.target.value as EvidenceSourceKind })}
            >
              <option value="directory">directory (local export)</option>
              <option value="transfer">transfer (customer program)</option>
              {source.kind === "api" ? <option value="api">api (declared; not collected in this release)</option> : null}
            </select>
          </label>
          <label>
            Scope
            <input
              value={source.scope}
              disabled={locked}
              onChange={(e) => setSource({ ...source, scope: e.target.value })}
            />
          </label>
          <label>
            Collection plan framing
            <select value={planFraming} disabled={locked} onChange={(e) => setPlanFraming(e.target.value as "raw" | "mllp")}>
              <option value="raw">raw</option>
              <option value="mllp">mllp</option>
            </select>
          </label>
          <label>
            Terminator
            <select value={planTerminator} disabled={locked} onChange={(e) => setPlanTerminator(e.target.value as "cr" | "lf" | "crlf")}>
              <option value="cr">cr</option>
              <option value="lf">lf</option>
              <option value="crlf">crlf</option>
            </select>
          </label>
          <label>
            Member suffixes
            <input value={planMembers} disabled={locked} onChange={(e) => setPlanMembers(e.target.value)} />
          </label>
          {source.kind === "directory" ? (
            <div className="capture-row">
              <label>
                Export folder
                <input
                  value={source.root ?? ""}
                  disabled={locked}
                  onChange={(e) => setSource({ ...source, root: e.target.value })}
                />
              </label>
              <button type="button" disabled={locked} onClick={() => void pick("source-root", (p) => setSource({ ...source, root: p }))}>
                Choose folder…
              </button>
            </div>
          ) : (
            <>
              <label>
                Address
                <input
                  value={source.address ?? ""}
                  disabled={locked}
                  onChange={(e) => setSource({ ...source, address: e.target.value, classification: "nonproduction" })}
                />
              </label>
              <div className="capture-row">
                <label>
                  Transfer program
                  <input
                    value={source.command ?? ""}
                    disabled={locked}
                    onChange={(e) => setSource({ ...source, command: e.target.value })}
                  />
                </label>
                <button
                  type="button"
                  disabled={locked}
                  onClick={() => void pick("transfer-program", (p) => setSource({ ...source, command: p }))}
                >
                  Choose program…
                </button>
              </div>
            </>
          )}
          <div className="actions">
            <button type="button" disabled={locked} onClick={() => void saveSource()}>
              Save registration
            </button>
            <button type="button" disabled={locked} onClick={() => void runDiagnose()}>
              Diagnose access
            </button>
            <button type="button" disabled={locked} onClick={() => void runCollectSource()}>
              Collect
            </button>
          </div>
          <p className="hint">
            Read-only collection from the source declared above into this workspace; it starts only when you collect.
          </p>
          {access?.access ? (
            <p>
              Access: {access.access.status}
              {access.access.declared != null ? ` · Declared: ${access.access.declared}` : null}
              {access.access.readable != null ? ` · Readable: ${access.access.readable}` : null}
            </p>
          ) : null}
          {collection?.collection ? (
            <p>
              Collected: {collection.collection.collected} / {collection.collection.declared}
              {collection.output_path ? ` · Staged as ${collection.output_path}` : null}
            </p>
          ) : null}
          {collection?.state === "completed" ? (
            <div className="actions">
              <button type="button" disabled={locked} onClick={() => void runFinalize()}>
                Create case…
              </button>
              <p className="hint">
                Finalizes the staged collection into imported-from-capture.case in this workspace; the new case is
                verified before it opens.
              </p>
            </div>
          ) : null}
        </div>
      ) : null}

      {mode === "collect" ? (
        <div className="capture-section">
          <h3>MLLP collector</h3>
          <div className="capture-row">
            <label>
              Policy file
              <input value={policyFile} disabled={locked} onChange={(e) => setPolicyFile(e.target.value)} />
            </label>
            <button type="button" disabled={locked} onClick={() => void openPolicy()}>
              Open policy…
            </button>
          </div>
          {openedPolicy ? <PolicyReview file={openedPolicy.file} policy={openedPolicy.policy} /> : null}
          <label>
            Policy name
            <input
              value={policy.name}
              disabled={locked}
              onChange={(e) => editPolicy(setPolicy)({ ...policy, name: e.target.value })}
            />
          </label>
          <label>
            Source label
            <input
              value={policy.source_label}
              disabled={locked}
              onChange={(e) => editPolicy(setPolicy)({ ...policy, source_label: e.target.value })}
            />
          </label>
          <label>
            Acknowledgement code
            <select
              value={policy.acknowledgement.code}
              disabled={locked}
              onChange={(e) =>
                editPolicy(setPolicy)({
                  ...policy,
                  acknowledgement: { ...policy.acknowledgement, code: e.target.value },
                })
              }
            >
              <option value="AA">AA</option>
              <option value="AE">AE</option>
              <option value="AR">AR</option>
            </select>
          </label>
          <label>
            <input type="checkbox" checked={enhanced} disabled={locked} onChange={(e) => editPolicy(setEnhanced)(e.target.checked)} />
            Enhanced acknowledgement (v2)
          </label>
          <label>
            Controlled fault (v3 synthetic)
            <select value={faultAction} disabled={locked} onChange={(e) => {
              const action = e.target.value as FaultAction;
              editPolicy(setFaultAction)(action);
              if (action !== "none" && !addressEdited && address === "127.0.0.1:0") setAddress("127.0.0.1:2575");
              if (waitsForFault(action) && faultDelayMs === 0) setFaultDelayMs(50);
            }}>
              <option value="none">none</option>
              <option value="delay">delay</option>
              <option value="reject">reject</option>
              <option value="malformed-ack">malformed response</option>
              <option value="missing-response">missing response</option>
              <option value="disconnect">disconnect</option>
            </select>
          </label>
          {waitsForFault(faultAction) ? (
            <label>
              Fault delay (ms)
              <input type="number" min="1" max="30000" value={faultDelayMs} disabled={locked} onChange={(e) => editPolicy(setFaultDelayMs)(Number(e.target.value))} />
            </label>
          ) : null}
          <label>
            Listen address
            <input value={address} disabled={locked} onChange={(e) => editAddress(e.target.value)} />
          </label>
          <label>
            <input
              type="checkbox"
              checked={approvedBind}
              disabled={locked}
              onChange={(e) => setApprovedBind(e.target.checked)}
            />
            Approved nonloopback bind
          </label>
          <label>
            Case output name
            <input value={outputName} disabled={locked} onChange={(e) => setOutputName(e.target.value)} />
          </label>
          <label>
            Journal name
            <input value={journalName} disabled={locked} onChange={(e) => setJournalName(e.target.value)} />
          </label>
          <label>
            Max messages (0 = until cancel)
            <input
              type="number"
              value={maxMessages}
              disabled={locked}
              onChange={(e) => setMaxMessages(Number(e.target.value))}
            />
          </label>
          <label>
            Max connections
            <input
              type="number"
              value={maxConnections}
              disabled={locked}
              onChange={(e) => setMaxConnections(Number(e.target.value))}
            />
          </label>
          <div className="capture-row">
            <label>
              TLS certificate
              <input value={tlsCert} disabled={locked} onChange={(e) => setTlsCert(e.target.value)} />
            </label>
            <button type="button" disabled={locked} onClick={() => void pick("certificate", setTlsCert)}>
              Choose…
            </button>
          </div>
          <label>
            TLS key reference
            <input value={tlsKeyRef} disabled={locked} onChange={(e) => setTlsKeyRef(e.target.value)} />
          </label>
          <div className="capture-row">
            <label>
              Secrets store
              <input value={secretsFile} disabled={locked} onChange={(e) => setSecretsFile(e.target.value)} />
            </label>
            <button type="button" disabled={locked} onClick={() => void pick("secrets", setSecretsFile)}>
              Choose…
            </button>
          </div>
          <div className="capture-row">
            <label>
              Client CA
              <input value={clientCA} disabled={locked} onChange={(e) => setClientCA(e.target.value)} />
            </label>
            <button type="button" disabled={locked} onClick={() => void pick("client-ca", setClientCA)}>
              Choose…
            </button>
          </div>
          <div className="actions">
            <button type="button" disabled={locked} onClick={() => void runPreview()}>
              Preview collector
            </button>
            <button type="button" disabled={locked || preview?.state !== "completed"} onClick={() => void runStart()}>
              Start collecting
            </button>
            <button ref={cancelControl} type="button" disabled={operation !== "collecting"} onClick={lifecycle.cancel}>
              Cancel
            </button>
            <button type="button" disabled={locked} onClick={() => void runJournal()}>
              Reopen journal
            </button>
          </div>
        </div>
      ) : null}

      {mode === "listen" ? (
        <div className="capture-section">
          <h3>Synthetic SIU fixture</h3>
          <p className="fixture-label">Separately labelled test fixture (readmit-siu-v1). Not a production receiver.</p>
          <label>
            Mode
            <select
              value={fixtureMode}
              disabled={locked}
              onChange={(e) => setFixtureMode(e.target.value as "fixed" | "defective")}
            >
              <option value="fixed">fixed</option>
              <option value="defective">defective</option>
            </select>
          </label>
          <label>
            Listen address
            <input value={address} disabled={locked} onChange={(e) => editAddress(e.target.value)} />
          </label>
          <label>
            Case output name
            <input value={outputName} disabled={locked} onChange={(e) => setOutputName(e.target.value)} />
          </label>
          <label>
            Observation file
            <input value={observationName} disabled={locked} onChange={(e) => setObservationName(e.target.value)} />
          </label>
          <label>
            Max messages (0 = until cancel)
            <input
              type="number"
              value={maxMessages}
              disabled={locked}
              onChange={(e) => setMaxMessages(Number(e.target.value))}
            />
          </label>
          <div className="actions">
            <button type="button" disabled={locked} onClick={() => void runPreview()}>
              Preview fixture
            </button>
            <button type="button" disabled={locked || preview?.state !== "completed"} onClick={() => void runStart()}>
              Start listener
            </button>
            <button ref={cancelControl} type="button" disabled={operation !== "listening"} onClick={lifecycle.cancel}>
              Cancel
            </button>
          </div>
        </div>
      ) : null}

      {preview?.preview ? (
        <dl className="capture-preview">
          <dt>Kind</dt>
          <dd>{preview.preview.kind}</dd>
          {preview.preview.fixture_label ? (
            <>
              <dt>Fixture</dt>
              <dd>{preview.preview.fixture_label}</dd>
            </>
          ) : null}
          {preview.preview.policy_name ? (
            <>
              <dt>Policy</dt>
              <dd>{preview.preview.policy_name}</dd>
            </>
          ) : null}
          {preview.preview.source_label ? (
            <>
              <dt>Source label</dt>
              <dd>{preview.preview.source_label}</dd>
            </>
          ) : null}
          <dt>Transport</dt>
          <dd>{preview.preview.transport}</dd>
          {preview.preview.key_reference ? (
            <>
              <dt>Key reference</dt>
              <dd>{preview.preview.key_reference}</dd>
            </>
          ) : null}
          <dt>Journal</dt>
          <dd>{preview.preview.journal_enabled ? "enabled" : "unavailable"}</dd>
          <dt>Limits</dt>
          <dd>
            messages {preview.preview.max_messages ?? 0}, connections {preview.preview.max_connections ?? 1}, frame{" "}
            {preview.preview.max_frame_bytes}
          </dd>
        </dl>
      ) : null}

      {session?.case ? (
        <p>
          Case sealed: {session.case.name} · {session.case.messages} messages · {session.case.sources} sources
        </p>
      ) : null}
      {session?.ledger ? (
        <p>
          {`Appointment ledger ${session.observation_path ?? ""}: ${session.ledger.schema} · ` +
            `Receiver mode: ${session.ledger.mode} · Processed occurrences: ${session.ledger.processed} · ` +
            `Ledger records: ${session.ledger.records} · Consistent: ${String(session.ledger.consistent)}`}
        </p>
      ) : null}
      {journal?.journal ? (
        <p>
          Journal {journal.journal.state}: received {journal.journal.received}, acknowledged {journal.journal.acknowledged}
          {journal.journal.recovered ? " · recovery never resumes or resends" : null}
        </p>
      ) : null}

      {finalized?.case ? (
        <div className="capture-complete">
          <h3>Import completed</h3>
          <p>
            {finalized.case.name}: {finalized.case.messages} messages
            {finalized.registered ? " · registered on the project" : null}
          </p>
          <div className="actions">
            <button type="button" onClick={() => onOpenCase(finalized.case!.name)}>
              Open case
            </button>
            {onSetupIndex ? (
              <button type="button" onClick={() => onSetupIndex(finalized.case!.name)}>
                Set up index
              </button>
            ) : null}
            {onBindObservation ? (
              <button
                type="button"
                onClick={() => onBindObservation(observationBinding(finalized.case!.name))}
              >
                Set up observation
              </button>
            ) : null}
          </div>
        </div>
      ) : null}

      {session?.case && mode !== "source" ? (
        <div className="actions">
          <button type="button" disabled={locked} onClick={() => onOpenCase(session.case!.name)}>
            Open case
          </button>
          {onSetupIndex ? (
            <button type="button" disabled={locked} onClick={() => onSetupIndex(session.case!.name)}>
              Set up index
            </button>
          ) : null}
          {onBindObservation ? (
            <button
              type="button"
              disabled={locked}
              onClick={() => onBindObservation(observationBinding(session.case!.name))}
            >
              Set up observation
            </button>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
