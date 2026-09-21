import { useState } from "react";
import {
  cancel,
  chooseCapturePath,
  collectSource,
  diagnoseSource,
  finalizeCaptureImport,
  openCaptureJournal,
  previewCapture,
  saveReceiverPolicy,
  saveSourceRegistration,
  startCapture,
  type CaptureJournalResult,
  type CapturePreviewResult,
  type CaptureSessionResult,
  type EvidenceSource,
  type ImportCommitResult,
  type ReceiverPolicy,
  type SourceAccessResult,
  type SourceCollectionResult,
} from "./bindings";
import type { Indicators } from "./shell";
import { Status } from "./shell";
import "./capture.css";

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

export function CapturePanel({
  workspace,
  project,
  busy,
  indicators,
  onOpenCase,
  onSetupIndex,
  onClose,
}: {
  workspace: string;
  project: string | null;
  busy: boolean;
  indicators: Indicators;
  onOpenCase: (caseName: string) => void;
  onSetupIndex?: (caseName: string) => void;
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
  const [faultAction, setFaultAction] = useState<
    "none" | "delay" | "reject" | "missing-response" | "disconnect" | "malformed-ack"
  >("none");
  const [faultDelayMs, setFaultDelayMs] = useState(50);
  const [address, setAddress] = useState("127.0.0.1:0");
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
  const [operation, setOperation] = useState<string | null>(null);
  const [access, setAccess] = useState<SourceAccessResult | null>(null);
  const [collection, setCollection] = useState<SourceCollectionResult | null>(null);
  const [preview, setPreview] = useState<CapturePreviewResult | null>(null);
  const [session, setSession] = useState<CaptureSessionResult | null>(null);
  const [journal, setJournal] = useState<CaptureJournalResult | null>(null);
  const [finalized, setFinalized] = useState<ImportCommitResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const locked = busy || operation !== null;

  async function pick(kind: string, apply: (path: string) => void) {
    setError(null);
    setOperation("choosing");
    try {
      const result = await chooseCapturePath(kind);
      if (result.state === "completed" && result.paths?.[0]) apply(result.paths[0]);
      else if (result.state !== "cancelled") setError(result.reason ?? result.state);
    } finally {
      setOperation(null);
    }
  }


  function importPlan() {
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
    setOperation("saving-source");
    try {
      const result = await saveSourceRegistration({ workspace, source_file: sourceFile, source });
      if (result.state !== "completed") setError(result.reason ?? result.state);
    } finally {
      setOperation(null);
    }
  }

  async function runDiagnose() {
    setError(null);
    setAccess(null);
    setOperation("diagnosing");
    try {
      const result = await diagnoseSource({ workspace, source_file: sourceFile, plan: importPlan() });
      setAccess(result);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    } finally {
      setOperation(null);
    }
  }

  async function runCollectSource() {
    setError(null);
    setCollection(null);
    setOperation("collecting-source");
    try {
      const result = await collectSource({
        workspace,
        source_file: sourceFile,
        plan: importPlan(),
        output_name: "collected",
        receipt_name: "collection.json",
      });
      setCollection(result);
      if (result.output_path) setStagedFolder(result.output_path);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    } finally {
      setOperation(null);
    }
  }

  function captureRequest() {
    const req: Parameters<typeof previewCapture>[0] = {
      workspace,
      kind: mode === "listen" ? "listen" : "collect",
      address,
      approved_bind: approvedBind,
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
    setOperation("previewing");
    try {
      if (mode === "collect") {
        const schema = enhanced ? "readmit-receiver-policy/v2" : "readmit-receiver-policy/v1";
        const toSave: ReceiverPolicy = {
          schema,
          name: policy.name,
          source_label: policy.source_label,
          acknowledgement: policy.acknowledgement,
          accepted_message_types: policy.accepted_message_types,
        };
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
            steps: [{ message: 1, stage: "application", action: faultAction, delay_ms: faultDelayMs }],
          };
        }
        const saved = await saveReceiverPolicy({ workspace, policy_file: policyFile, policy: toSave });
        if (saved.state !== "completed") {
          setError(saved.reason ?? saved.state);
          return;
        }
      }
      const result = await previewCapture(captureRequest());
      setPreview(result);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    } finally {
      setOperation(null);
    }
  }

  async function runStart() {
    setError(null);
    setSession(null);
    setOperation(mode === "listen" ? "listening" : "collecting");
    try {
      const result = await startCapture(captureRequest());
      setSession(result);
      if (result.state !== "completed" && result.state !== "cancelled") {
        setError(result.reason ?? result.state);
      }
      if (result.case_path && mode === "collect") {
        // Collected case is already a verified case destination from the collector.
      }
    } finally {
      setOperation(null);
    }
  }

  async function runJournal() {
    setError(null);
    setJournal(null);
    setOperation("recovering");
    try {
      const result = await openCaptureJournal(workspace, journalName);
      setJournal(result);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    } finally {
      setOperation(null);
    }
  }

  async function runFinalize() {
    setError(null);
    setFinalized(null);
    setOperation("finalizing");
    try {
      const folder = stagedFolder || "collected";
      const request: Parameters<typeof finalizeCaptureImport>[0] = {
        workspace,
        folder,
        output_name: "imported-from-capture.case",
        register_in_project: Boolean(project),
      };
      if (project) request.project = project;
      const result = await finalizeCaptureImport(request);
      setFinalized(result);
      if (result.state !== "completed") setError(result.reason ?? result.state);
    } finally {
      setOperation(null);
    }
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
        <h2 id="capture-title">Capture and collect evidence</h2>
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
        {session?.received != null ? ` · Received: ${session.received}` : null}
        {session?.connections != null ? ` · Connections: ${session.connections}` : null}
        {journal?.journal ? ` · Journal received: ${journal.journal.received}` : null}
      </p>
      {error ? <Status indicator={indicators.get("failed")} state="failed" reason={error} /> : null}

      {mode === "source" ? (
        <div className="capture-section">
          <h3>Approved source</h3>
          <label>
            Registration file
            <input value={sourceFile} disabled={locked} onChange={(e) => setSourceFile(e.target.value)} />
          </label>
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
              onChange={(e) => setSource({ ...source, kind: e.target.value })}
            >
              <option value="directory">directory (local export)</option>
              <option value="transfer">transfer (customer program)</option>
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
              Collect into workspace
            </button>
          </div>
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
                Finalize into a verified case…
              </button>
            </div>
          ) : null}
        </div>
      ) : null}

      {mode === "collect" ? (
        <div className="capture-section">
          <h3>MLLP collector</h3>
          <label>
            Policy file
            <input value={policyFile} disabled={locked} onChange={(e) => setPolicyFile(e.target.value)} />
          </label>
          <label>
            Policy name
            <input
              value={policy.name}
              disabled={locked}
              onChange={(e) => setPolicy({ ...policy, name: e.target.value })}
            />
          </label>
          <label>
            Source label
            <input
              value={policy.source_label}
              disabled={locked}
              onChange={(e) => setPolicy({ ...policy, source_label: e.target.value })}
            />
          </label>
          <label>
            Acknowledgement code
            <select
              value={policy.acknowledgement.code}
              disabled={locked}
              onChange={(e) =>
                setPolicy({
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
            <input type="checkbox" checked={enhanced} disabled={locked} onChange={(e) => setEnhanced(e.target.checked)} />
            Enhanced acknowledgement (v2)
          </label>
          <label>
            Controlled fault (v3 synthetic)
            <select value={faultAction} disabled={locked} onChange={(e) => setFaultAction(e.target.value as typeof faultAction)}>
              <option value="none">none</option>
              <option value="delay">delay</option>
              <option value="reject">reject</option>
              <option value="malformed-ack">malformed response</option>
              <option value="missing-response">missing response</option>
              <option value="disconnect">disconnect</option>
            </select>
          </label>
          {faultAction === "delay" ? (
            <label>
              Fault delay (ms)
              <input type="number" value={faultDelayMs} disabled={locked} onChange={(e) => setFaultDelayMs(Number(e.target.value))} />
            </label>
          ) : null}
          <label>
            Listen address
            <input value={address} disabled={locked} onChange={(e) => setAddress(e.target.value)} />
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
            <button type="button" disabled={operation !== "collecting"} onClick={() => cancel()}>
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
          <h3>Built-in synthetic SIU fixture</h3>
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
            <input value={address} disabled={locked} onChange={(e) => setAddress(e.target.value)} />
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
              Start fixture listener
            </button>
            <button type="button" disabled={operation !== "listening"} onClick={() => cancel()}>
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
              Open this case
            </button>
            {onSetupIndex ? (
              <button type="button" onClick={() => onSetupIndex(finalized.case!.name)}>
                Open this case to build an index
              </button>
            ) : null}
          </div>
        </div>
      ) : null}

      {session?.case && mode !== "source" ? (
        <div className="actions">
          <button type="button" disabled={locked} onClick={() => onOpenCase(session.case!.name)}>
            Open this case
          </button>
          {onSetupIndex ? (
            <button type="button" disabled={locked} onClick={() => onSetupIndex(session.case!.name)}>
              Open this case to build an index
            </button>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
