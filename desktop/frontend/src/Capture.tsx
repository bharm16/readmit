import "./workflow.css";
// Capture: a bounded session that records messages from one saved source and
// finishes into one case. Setup names the case, the source and its limits and
// starts deliberately; the running session shows what actually arrived; Stop
// finishes and opens the case, Cancel capture keeps what was recorded without
// publishing a case. History lists every session read-only.
import { useCallback, useEffect, useRef, useState } from "react";
import {
  cancel,
  captureProgress,
  chooseCapturePath,
  finishCapture,
  inspectOccurrence,
  listCaptureSessions,
  listCatalog,
  listCredentials,
  newIntentId,
  openItemDraft,
  openRetainedCapture,
  readMessages,
  retryCaptureFinalization,
  saveItem,
  startCapture,
  type AckCode,
  type CapturePathKind,
  type CaptureProgress,
  type CaptureSourceType,
  type ListenerTransport,
  type CaptureSessionResult,
  type CaptureSessionRow,
  type CaptureSourceDraft,
  type CatalogItem,
  type CredentialRow,
  type HL7Terminator,
  type ImportFraming,
  type InspectionResult,
  type MessageRow,
  type RetainedCaptureResult,
  type ItemRef,
  type RequestContext,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { listDate } from "./Projects";
import { MessageReader } from "./Inspector";
import { DIRECTION_NAMES, NO_QUERY, rowType, sourceLabel, timeOfDay } from "./Messages";
import { useVocabulary } from "./vocabulary";
import type { DisplayMap } from "./display";
import { wholeNumber } from "./Environments";
import "./capture.css";

const SOURCE_TYPES: DisplayMap<CaptureSourceType> = {
  "local-folder": "Local folder",
  transfer: "Transfer program",
  "mllp-listener": "MLLP listener",
  api: "API",
};

const TRANSPORTS: DisplayMap<ListenerTransport> = { plain: "Plain MLLP", tls: "TLS", "mutual-tls": "Mutual TLS" };

const OUTCOMES: Record<string, string> = {
  running: "Recording",
  finished: "Finished",
  cancelled: "Cancelled",
  interrupted: "Interrupted",
  "finalize-failed": "Not finalized",
};

const FAULT_WORDS: Record<string, string> = {
  delay: "Delay",
  reject: "Reject",
  "malformed-ack": "Malformed ACK",
  "missing-response": "No response",
  disconnect: "Disconnect",
};

const APPLICATION_DELIVERY: Record<string, string> = { "same-connection": "Same connection", "separate-endpoint": "Separate endpoint" };
const FAULT_STAGES: Record<string, string> = { application: "Application ACK", commit: "Commit ACK", accept: "Commit ACK", connection: "Connection" };

/** How often a running capture's progress is read. */
const PROGRESS_MS = 500;

function elapsedText(from: string | undefined, now: number): string {
  if (!from) return "—";
  const seconds = Math.max(0, Math.floor((now - Date.parse(from)) / 1000));
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return `${h > 0 ? `${h}:` : ""}${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

function loopback(address: string): boolean {
  return address === "127.0.0.1" || address === "::1" || address === "localhost" || address.startsWith("127.");
}

/** Where a source reads from, as its settings say. */
function sourceWhere(source: CaptureSourceDraft | undefined): string {
  if (!source) return "—";
  if (source.listener) return `${source.listener.bind_address}:${source.listener.port || "any port"}`;
  if (source.evidence?.root) return source.evidence.root;
  if (source.evidence?.command) return source.evidence.command;
  return "—";
}

export type CaptureProps = {
  root: string | null;
  context: () => RequestContext;
  busy: boolean;
  /** Setup was asked for from elsewhere (Cases, the header). */
  setupRequest: number;
  /** A finished capture's case, to open. */
  onOpenCase: (ref: ItemRef) => void;
};

/** The Capture page's actions and body, and whether a capture is recording. */
export function useCapture({ root, context, busy, setupRequest, onOpenCase }: CaptureProps) {
  const [progress, setProgress] = useState<CaptureProgress | null>(null);
  const [sessions, setSessions] = useState<CaptureSessionRow[] | null>(null);
  const [historyFailure, setHistoryFailure] = useState<string | null>(null);
  const [setup, setSetup] = useState(false);
  const [receiverSettings,setReceiverSettings]=useState<CaptureSourceDraft|null>(null);
  const [receiverDetails,setReceiverDetails]=useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [failed, setFailed] = useState<{ session: string; reason: string } | null>(null);
  const [now, setNow] = useState(Date.now());
  // A cancelled or unfinalized session whose kept messages are being read.
  const [retained, setRetained] = useState<CaptureSessionRow | null>(null);
  const running = useRef(false);

  const historyReads = useRef(0);
  const readHistory = useCallback(async () => {
    if (!root) return;
    const asked = ++historyReads.current;
    const answer = await listCaptureSessions(context());
    // An answer for another project, or an earlier read, describes nothing shown.
    if (asked !== historyReads.current) return;
    if (answer.state === "completed" || answer.state === "empty") {
      setSessions(answer.sessions);
      setHistoryFailure(null);
    } else if (answer.state !== "busy") setHistoryFailure(answer.reason ?? "Capture history could not be read.");
  }, [root, context]);
  useEffect(() => {
    setSessions(null);
    void readHistory();
  }, [readHistory]);

  // A capture started from anywhere — this window, or one that kept running
  // while the person was elsewhere — is followed by its progress.
  useEffect(() => {
    let live = true;
    const poll = async () => {
      const answer = await captureProgress();
      if (!live) return;
      setProgress(answer.state === "completed" && answer.progress ? answer.progress : null);
      setNow(Date.now());
    };
    void poll();
    const timer = setInterval(() => void poll(), PROGRESS_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    if (setupRequest > 0) setSetup(true);
  }, [setupRequest]);

  /** What a capture that ended answers: a finished one opens its case. */
  const ended = (answer: CaptureSessionResult) => {
    running.current = false;
    void readHistory();
    if (answer.outcome === "finished" && answer.case_ref) {
      onOpenCase(answer.case_ref);
      return;
    }
    if (answer.outcome === "finalize-failed" && answer.session) {
      setFailed({ session: answer.session, reason: answer.reason ?? "The case was not finalized." });
      return;
    }
    if (answer.outcome !== "cancelled" && answer.state !== "completed") setNotice(answer.reason ?? "The capture did not finish.");
  };

  const retry = async (session: string) => {
    const answer = await retryCaptureFinalization({ context: context(), session });
    if (answer.state === "completed" && answer.case) {
      setFailed(null);
      void readHistory();
      onOpenCase(answer.case);
    } else setFailed({ session, reason: answer.reason ?? "The case was not finalized." });
  };

  const recording = progress !== null;
  // Only a listener is finished by Stop; a folder or program capture finishes
  // when it has read its source.
  const listening = progress?.source_type === "mllp-listener";
  const columns: Column<CaptureSessionRow>[] = [
    { key: "name", header: "Name", priority: 1, minWidth: 12, flex: true, render: (row) => <span title={row.name}>{row.name}</span> },
    { key: "source", header: "Source", priority: 2, minWidth: 10, render: (row) => row.source_name || SOURCE_TYPES[row.source_type] || "—" },
    { key: "started", header: "Started", priority: 3, minWidth: 9, render: (row) => (row.started_at ? listDate(row.started_at) : "—") },
    {
      key: "state",
      header: "Result",
      priority: 1,
      minWidth: 9,
      render: (row) => (row.received > 0 && row.state !== "running" ? `${OUTCOMES[row.state] ?? "—"} · ${row.received}` : (OUTCOMES[row.state] ?? "—")),
    },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (row) => {
        const items = [
          ...(row.case ? [{ label: "Open case", onSelect: () => onOpenCase(row.case!) }] : []),
          ...(row.retained && !row.case ? [{ label: "Open retained data", onSelect: () => setRetained(row) }] : []),
          ...(row.state === "finalize-failed" ? [{ label: "Retry finalization", onSelect: () => void retry(row.id), disabled: busy }] : []),
        ];
        return items.length > 0 ? (
          <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
            <Menu label={`More actions for ${row.name}`} items={items} />
          </span>
        ) : null;
      },
    },
  ];

  useEffect(()=>{
   let current=true;setReceiverSettings(null);setReceiverDetails(false);
   if(progress?.source)void openItemDraft({context:context(),ref:progress.source}).then(answer=>{if(current&&answer.state==="completed"&&answer.draft?.source)setReceiverSettings(answer.draft.source);});
   return()=>{current=false;};
  },[progress?.source?.id,progress?.source?.revision,context]);
  const active = progress ? (
    <section className={listening?"workflow-page workflow-receiving":"value-group"} aria-labelledby="capture-active">
      {listening?<aside className="workflow-rail"><h2>Messages</h2><div className="workflow-step-summary"><strong>Receiver</strong><span>{progress.finishing?"Finishing":`Listening · ${progress.received??0} messages`}</span></div></aside>:null}
      <div className="workflow-receiving-main">
      <header className="value-group-header">
        <h2 id="capture-active">{listening?"Receiving · MLLP":progress.name || "Capture"}</h2>
      </header>
      {!listening ? (<ValueRows
        rows={[
          { label: "Source", value: progress.source_name || (progress.source_type ? SOURCE_TYPES[progress.source_type] : "—") },
          ...(listening ? [{ label: "Address", value: progress.bound_address || "Starting…" }] : []),
          { label: "Elapsed", value: elapsedText(progress.started_at, now) },
          ...(progress.received ? [{ label: "Received", value: String(progress.received) }] : []),
        ]}
      />) : null}
      {(progress.messages ?? []).length > 0 ? (
        <table className="plain-table" aria-label="Incoming messages">
          <thead>
            <tr>
              <th scope="col">Time</th>
              <th scope="col">Type</th>
              <th scope="col">Connection</th>
            </tr>
          </thead>
          <tbody>
            {[...(progress.messages ?? [])].reverse().map((message, index) => (
              <tr key={`${message.at}-${index}`}>
                <td>{new Date(message.at).toLocaleTimeString()}</td>
                <td>{message.type ? message.type.replace("^", " · ") : "Message"}</td>
                <td>{message.connection}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <section className="workflow-receive-waiting"><h2 aria-live="polite">{progress.finishing ? "Finishing…" : "Waiting for messages"}</h2>{listening?<><p className="workflow-receive-address">{progress.bound_address||"Starting…"}</p><p className="workflow-caption">Messages received by this listener will appear here.</p></>:null}</section>
      )}
      </div>
      {listening?<aside className="workflow-receiver-pane" aria-label="Receiver"><h3>Receiver</h3><section className="workflow-receiver-content"><h3>{progress.source_name||progress.name||"Receiver"}</h3><ValueRows rows={[{label:"Address",value:progress.bound_address||"Starting…"},{label:"Acknowledgment",value:receiverSettings?.listener?.ack_code??receiverSettings?.responder?.acknowledgement.code??"Not recorded"},{label:"Message limit",value:receiverSettings?.listener?String(receiverSettings.listener.message_limit):"Not recorded"},{label:"Time limit",value:receiverSettings?.listener?.idle_timeout??"Not recorded"},{label:"Elapsed",value:elapsedText(progress.started_at,now)},...(progress.received?[{label:"Received",value:String(progress.received)}]:[])]}/><p className="workflow-caption">Receive and send destination are configured separately.</p><button type="button" className="quiet" disabled={!receiverSettings} onClick={()=>setReceiverDetails(true)}>Receiver settings</button></section></aside>:null}
      {receiverDetails&&receiverSettings?<CaptureSourceDetails source={receiverSettings} onClose={()=>setReceiverDetails(false)}/>:null}
    </section>
  ) : null;

  const body = retained ? (
    <RetainedData context={context} session={retained} onClose={() => setRetained(null)} />
  ) : (
    <>
      {notice ? <p role="alert">{notice}</p> : null}
      {failed ? (
        <div className="notice danger" role="alert">
          <p>{failed.reason}</p>
          <button type="button" disabled={busy} onClick={() => void retry(failed.session)}>
            Retry finalization
          </button>
        </div>
      ) : null}
      {active}
      <section className="value-group" aria-labelledby="capture-history">
        <header className="value-group-header">
          <h2 id="capture-history">Capture history</h2>
        </header>
        {historyFailure ? (
          <p role="alert">{historyFailure}</p>
        ) : sessions && sessions.length === 0 && !recording ? (
          <EmptyState
            title="No captures"
            action={
              <button type="button" className="primary" disabled={busy} onClick={() => setSetup(true)}>
                New capture
              </button>
            }
          />
        ) : (
          <DataTable label="Capture history" rows={sessions ?? []} rowId={(row) => row.id} rowLabel={(row) => row.name} columns={columns} selected={null} onSelect={() => undefined} onOpen={() => undefined} loading={sessions === null} />
        )}
      </section>
      <CaptureSetup
        open={setup}
        context={context}
        onClose={() => setSetup(false)}
        onStart={(request) =>
          // The capture runs until it is finished or cancelled; the page
          // follows its progress meanwhile, and the person may leave it. The
          // sheet stays open until it is recording, so a refusal to start is
          // said there.
          new Promise<SubmitFailure | null>((resolve) => {
            setNotice(null);
            setFailed(null);
            running.current = true;
            let started = false;
            const begun = () => {
              if (started) return;
              started = true;
              setSetup(false);
              resolve(null);
            };
            const watch = setInterval(() => {
              void captureProgress().then((answer) => {
                if (answer.state === "completed" && answer.progress) begun();
              });
            }, PROGRESS_MS);
            void startCapture(request).then((answer) => {
              clearInterval(watch);
              if (!started && !answer.session && answer.outcome !== "finished") {
                started = true;
                running.current = false;
                resolve({ reason: answer.reason ?? "The capture did not start.", field: "capture-source" });
                return;
              }
              begun();
              ended(answer);
            });
          })
        }
      />
    </>
  );

  return {
    title: listening ? "Messages":"Capture",
    recording,
    /** Finishes the recording capture: it stops taking messages, and its case
     * is published and opened. */
    finish: () => void finishCapture(),
    actions: recording ? (
      <>
        <span className="status-text" aria-live="polite">
          {progress?.finishing ? "Finishing" : "Recording"}
        </span>
        {listening ? (
          <button type="button" className="primary" disabled={progress?.finishing} onClick={() => void finishCapture()}>
            Stop
          </button>
        ) : null}
        <Menu label="More capture actions" items={[{ label: "Cancel capture", tone: "danger" as const, onSelect: () => cancel("capture") }]} />
      </>
    ) : (
      <button type="button" className="primary" disabled={busy || !root} onClick={() => setSetup(true)}>
        New capture
      </button>
    ),
    body,
  };
}

// ---------- Retained data ----------

/** What a cancelled, interrupted or unfinalized capture kept, read-only. It
 * is not a case of the project and nothing here makes it one. */
function RetainedData({ context, session, onClose }: { context: () => RequestContext; session: CaptureSessionRow; onClose: () => void }) {
  const load = useCallback(() => openRetainedCapture({ context: context(), session: session.id }), [context, session.id]);
  return <RetainedEvidence name={session.name} resultLabel={OUTCOMES[session.state] ?? "—"} reason={session.reason} load={load} onClose={onClose} />;
}

/** One read-only owner for retained capture and exchange evidence. */
export function RetainedEvidence({ name, resultLabel, reason, load, onClose }: {
  name: string;
  resultLabel: string;
  reason?: string | undefined;
  load: () => Promise<RetainedCaptureResult>;
  onClose: () => void;
}) {
  const [opened, setOpened] = useState<RetainedCaptureResult | null>(null);
  const [rows, setRows] = useState<MessageRow[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [reading, setReading] = useState<InspectionResult | null>(null);
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    let live = true;
    reads.current++;
    setOpened(null);
    setRows(null);
    setSelected(null);
    setReading(null);
    void load().then(async (answer) => {
      if (!live) return;
      setOpened(answer);
      if (answer.state !== "completed" || !answer.case || !answer.workspace) return;
      const listed = await readMessages({ workspace: answer.workspace, case: answer.case.name, identity: answer.case.identity, query: NO_QUERY, sort: "", offset: 0, limit: 0 });
      if (live) setRows(listed.state === "completed" || listed.state === "empty" ? listed.rows : []);
    });
    return () => {
      live = false;
      reads.current++;
    };
  }, [load]);
  const reads = useRef(0);
  const inspect = async (occurrence: string, path = "", nodeOffset = 0, byteOffset = -1, reveal = opened?.case?.protocol === "fhir-r4" ? reading?.inspection?.revealed ?? false : !(reading?.inspection?.phi_masked ?? false)) => {
    if (!opened?.case || !opened.workspace) return null;
    const asked = ++reads.current;
    setLoading(true);
    const answer = await inspectOccurrence({ workspace: opened.workspace, case: opened.case.name, identity: opened.case.identity, occurrence, path, node_offset: nodeOffset, byte_offset: byteOffset, raw_offset: 0, reveal:opened.case.protocol === "fhir-r4" ? reveal : true,...(!reveal && opened.case.protocol !== "fhir-r4" ? {mask_phi:true} : {}) });
    if (asked !== reads.current) return null;
    setLoading(false);
    setReading(answer);
    return answer;
  };
  const columns: Column<MessageRow>[] = [
    { key: "time", header: "Time", priority: 1, minWidth: 6, render: (row) => timeOfDay(row.observed_at) },
    { key: "type", header: "Type", priority: 1, minWidth: 7, render: rowType },
    { key: "source", header: "Source", priority: 2, minWidth: 8, render: (row) => sourceLabel(row) },
    { key: "direction", header: "Direction", priority: 3, minWidth: 7, render: (row) => (row.direction === "unknown" ? "—" : DIRECTION_NAMES[row.direction]) },
  ];
  return (
    <section className="value-group" aria-labelledby="capture-retained">
      <header className="value-group-header">
        <h2 id="capture-retained">{name}</h2>
        <button type="button" onClick={onClose}>
          Close
        </button>
      </header>
      <ValueRows rows={[{ label: "Result", value: resultLabel }, ...(reason ? [{ label: "Reason", value: reason }] : [])]} />
      {opened && opened.state !== "completed" ? <p role="alert">{opened.reason ?? "The kept messages could not be read."}</p> : null}
      {rows ? (
        rows.length === 0 ? (
          <p>No messages</p>
        ) : (
          <DataTable label="Kept messages" rows={rows} rowId={(row) => row.id} rowLabel={rowType} columns={columns} selected={selected} onSelect={(id) => { setSelected(id); void inspect(id); }} onOpen={(id) => { setSelected(id); void inspect(id); }} />
        )
      ) : opened === null ? (
        <p aria-live="polite">Reading…</p>
      ) : null}
      {selected ? (
        <MessageReader result={reading} loading={loading} busy={false} onInspect={(path, nodeOffset, byteOffset) => inspect(selected, path, nodeOffset, byteOffset)} onReveal={(reveal) => void inspect(selected, reading?.inspection?.selected.path ?? "", 0, -1, reveal)} />
      ) : null}
    </section>
  );
}

// ---------- Setup ----------

type StartRequest = Parameters<typeof startCapture>[0];

/** Name, source, environment and limits; Start capture is the last and only
 * action that records. */
function CaptureSetup({ open, context, onClose, onStart }: { open: boolean; context: () => RequestContext; onClose: () => void; onStart: (request: StartRequest) => Promise<SubmitFailure | null> }) {
  const [name, setName] = useState("");
  const [sources, setSources] = useState<CatalogItem[]>([]);
  const [environments, setEnvironments] = useState<CatalogItem[]>([]);
  const [source, setSource] = useState<CatalogItem | null>(null);
  const [draft, setDraft] = useState<CaptureSourceDraft | null>(null);
  const [environment, setEnvironment] = useState("");
  const [messages, setMessages] = useState("");
  const [editing, setEditing] = useState<null | "new" | "edit">(null);
  const [details, setDetails] = useState(false);
  const [dirty, setDirty] = useState(false);

  const readSources = useCallback(async (select?: string) => {
    const [listed, envs] = await Promise.all([listCatalog({ context: context(), kind: "source", filter: {} }), listCatalog({ context: context(), kind: "environment", filter: {} })]);
    const items = (listed.page?.items ?? []).filter((item) => item.availability === "available");
    setSources(items);
    setEnvironments((envs.page?.items ?? []).filter((item) => item.availability === "available"));
    if (select) {
      const chosen = items.find((item) => item.ref.id === select) ?? null;
      setSource(chosen);
    }
  }, [context]);
  useEffect(() => {
    if (!open) return;
    setName("");
    setSource(null);
    setDraft(null);
    setEnvironment("");
    setMessages("");
    setDirty(false);
    void readSources();
  }, [open, readSources]);
  // The chosen source's saved settings, shown read-only.
  useEffect(() => {
    if (!source) {
      setDraft(null);
      return;
    }
    let live = true;
    void openItemDraft({ context: context(), ref: source.ref }).then((answer) => live && setDraft(answer.draft?.source ?? null));
    return () => {
      live = false;
    };
  }, [source?.ref.id, source?.ref.revision]); // eslint-disable-line react-hooks/exhaustive-deps

  const unavailable = draft?.type === "api";
  return (
    <>
      <FormDialog
        open={open && editing === null}
        title="New capture"
        submitLabel="Start capture"
        submitDisabled={name.trim() === "" || !source || !draft || unavailable}
        dirty={dirty}
        onClose={onClose}
        onSubmit={(): SubmitFailure | null | Promise<SubmitFailure | null> => {
          if (!source) return { reason: "Choose a source.", field: "capture-source" };
          const limit = wholeNumber(messages);
          if (limit === null) return { reason: "Enter a whole number of messages.", field: "capture-message-limit" };
          const chosenEnvironment = environments.find((item) => item.ref.id === environment);
          return onStart({
            context: context(),
            source: source.ref,
            name: name.trim(),
            ...(chosenEnvironment ? { environment: chosenEnvironment.ref } : {}),
            ...(limit > 0 ? { limits: { max_messages: limit } } : {}),
            intent_id: newIntentId(),
          });
        }}
      >
        <label htmlFor="capture-name">Name</label>
        <input id="capture-name" type="text" maxLength={200} autoFocus value={name} onChange={(event) => { setName(event.target.value); setDirty(true); }} />
        <label htmlFor="capture-source">Source</label>
        <select
          id="capture-source"
          value={source?.ref.id ?? ""}
          onChange={(event) => {
            if (event.target.value === "new") {
              setEditing("new");
              return;
            }
            setSource(sources.find((item) => item.ref.id === event.target.value) ?? null);
            setDirty(true);
          }}
        >
          {source ? null : <option value="">Choose a source</option>}
          {sources.map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.name}
            </option>
          ))}
          <option value="new">New source…</option>
        </select>
        {draft ? (
          <>
            <div className="section-header">
              <h3>{SOURCE_TYPES[draft.type] ?? "Source"}</h3>
              {draft.evidence || draft.responder || draft.listener ? <button type="button" onClick={() => setDetails(true)}>Source details</button> : null}
              <button type="button" onClick={() => setEditing("edit")}>
                Edit
              </button>
            </div>
            {unavailable ? (
              <ValueRows rows={[{ label: "Status", value: "Unavailable" }]} />
            ) : (
              <ValueRows
                rows={[
                  { label: draft.listener ? "Address" : draft.evidence?.command ? "Program" : "Folder", value: sourceWhere(draft) },
                  ...(draft.listener ? [{ label: "Transport", value: TRANSPORTS[draft.listener.transport] ?? "—" }, { label: "ACK code", value: draft.listener.ack_code }] : []),
                  ...(draft.listener?.allow_remote ? [{ label: "Remote connections", value: "Allowed" }] : []),
                ]}
              />
            )}
          </>
        ) : null}
        {environments.length > 0 ? (
          <>
            <label htmlFor="capture-environment">Environment</label>
            <select id="capture-environment" value={environment} onChange={(event) => { setEnvironment(event.target.value); setDirty(true); }}>
              <option value="">None</option>
              {environments.map((item) => (
                <option key={item.ref.id} value={item.ref.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </>
        ) : null}
        <label htmlFor="capture-message-limit">Message limit</label>
        <input id="capture-message-limit" type="text" inputMode="numeric" value={messages} onChange={(event) => { setMessages(event.target.value); setDirty(true); }} />
      </FormDialog>
      {details && draft ? <CaptureSourceDetails source={draft} onClose={() => setDetails(false)} /> : null}
      {editing !== null ? (
        <SourceEditor
          context={context}
          item={editing === "edit" ? source : null}
          draft={editing === "edit" ? draft : null}
          onClose={() => setEditing(null)}
          onSaved={async (saved) => {
            setEditing(null);
            await readSources(saved.id);
          }}
        />
      ) : null}
    </>
  );
}

// ---------- Source editor ----------

/** One saved capture source: its type and only that type's settings, and for
 * an MLLP listener its responder. One Save publishes them together. */
export function SourceEditor({
  context,
  item,
  draft: saved,
  title,
  onClose,
  onSaved,
}: {
  context: () => RequestContext;
  item: CatalogItem | null;
  draft: CaptureSourceDraft | null;
 title?:string;
  onClose: () => void;
  onSaved: (ref: ItemRef) => void | Promise<void>;
}) {
  const vocabulary = useVocabulary();
  const starts = vocabulary?.capture_source_starts ?? [];
  const faults = vocabulary?.receiver_faults;
  const plans = vocabulary?.import_plan;
  const [name, setName] = useState(item?.name ?? "");
  const [source, setSource] = useState<CaptureSourceDraft | null>(saved ? structuredClone(saved) : (structuredClone(starts.find((start) => start.type === "mllp-listener")) ?? null));
  const [numbers, setNumbers] = useState({ port: "", messages: "", connections: "" });
  const [credentials, setCredentials] = useState<CredentialRow[]>([]);
  const [responder, setResponder] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [chooseFailure, setChooseFailure] = useState<string | null>(null);
  useEffect(() => {
    const listener = source?.listener;
    setNumbers({ port: listener ? String(listener.port) : "", messages: listener ? String(listener.message_limit) : "", connections: listener ? String(listener.connection_limit) : "" });
    void listCredentials({ context: context(), ref: { kind: "environment", id: "" } }).then((answer) => setCredentials(answer.credentials.filter((row) => row.purpose === "mllp-endpoint")));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!source) return null;
  const change = (next: (value: CaptureSourceDraft) => void) => {
    setSource((held) => {
      if (!held) return held;
      const copy = structuredClone(held);
      next(copy);
      return copy;
    });
    setDirty(true);
  };
  const setType = (type: string) => {
    const start = starts.find((entry) => entry.type === type);
    if (!start) return;
    setSource(structuredClone(start));
    setNumbers({ port: start.listener ? String(start.listener.port) : "", messages: start.listener ? String(start.listener.message_limit) : "", connections: start.listener ? String(start.listener.connection_limit) : "" });
    setDirty(true);
  };
  const choose = async (kind: CapturePathKind, set: (path: string) => void) => {
    setChooseFailure(null);
    const answer = await chooseCapturePath(kind);
    if (answer.state === "completed" && answer.paths?.[0]) set(answer.paths[0]);
    else if (answer.state !== "cancelled") setChooseFailure(answer.reason ?? "Nothing was chosen.");
  };
  const listener = source.listener;
  const evidence = source.evidence;
  const fields: Record<string, string> = {
    name: "source-name",
    "source.listener.bind_address": "source-bind-address",
    "source.listener.allow_remote": "source-allow-remote",
    "source.listener.port": "source-port",
    "source.listener.transport": "source-transport",
    "source.listener.message_limit": "source-message-limit",
    "source.listener.connection_limit": "source-connection-limit",
    "source.listener.idle_timeout": "source-idle-timeout",
    "source.listener.ack_code": "source-ack-code",
    "source.listener.tls_key_reference": "source-tls-key",
    "source.listener.tls_certificate": "source-certificate",
    "source.listener.client_ca": "source-client-ca",
    "source.responder": "source-responder",
    "source.responder.faults": "source-responder",
    "source.responder.acknowledgement": "source-ack-code",
    "source.plan": "source-suffixes",
    "source.evidence": "source-scope",
    "source.evidence.kind": "source-type",
    "source.type": "source-type",
  };

  return (
    <>
      <FormDialog
        open={!responder}
        title={title ?? (item ? "Edit source" : "New source")}
        submitLabel="Save"
        submitDisabled={name.trim() === "" || source.type === "api"}
        dirty={dirty}
        onClose={onClose}
        onSubmit={async (): Promise<SubmitFailure | null> => {
          const next = structuredClone(source);
          if (next.listener) {
            const port = wholeNumber(numbers.port);
            const messages = wholeNumber(numbers.messages);
            const connections = wholeNumber(numbers.connections);
            if (port === null) return { reason: "Enter a port number.", field: "source-port" };
            if (messages === null) return { reason: "Enter a whole number of messages.", field: "source-message-limit" };
            if (connections === null) return { reason: "Enter a whole number of connections.", field: "source-connection-limit" };
            next.listener = { ...next.listener, port, message_limit: messages, connection_limit: connections };
          }
          const answer = await saveItem({
            context: context(),
            kind: "source",
            ...(item ? { item: item.ref.id, ...(item.ref.revision ? { base_revision: item.ref.revision } : {}) } : {}),
            draft: { name: name.trim(), source: next },
            intent_id: newIntentId(),
          });
          if (answer.outcome !== "saved" || !answer.saved) {
            if (answer.outcome === "conflict") return { reason: "This changed since you opened it. Close and open it again." };
            const problem = answer.problems.find((entry) => fields[entry.field]) ?? answer.problems[0];
            const field = problem ? fields[problem.field] : undefined;
            return { reason: answer.problems.map((entry) => entry.problem).join(" ") || answer.reason || "Not saved.", ...(field ? { field } : {}) };
          }
          setDirty(false);
          await onSaved(answer.saved);
          return null;
        }}
      >
        <label htmlFor="source-name">Name</label>
        <input id="source-name" type="text" maxLength={200} value={name} onChange={(event) => { setName(event.target.value); setDirty(true); }} />
        <label htmlFor="source-type">Type</label>
        <select id="source-type" value={source.type} disabled={item !== null} onChange={(event) => setType(event.target.value)}>
          {(vocabulary?.capture_source_types ?? []).map((entry) => (
            <option key={entry.type} value={entry.type} disabled={!entry.available}>
              {entry.available ? SOURCE_TYPES[entry.type] : `${SOURCE_TYPES[entry.type]} (Unavailable)`}
            </option>
          ))}
        </select>

        {evidence ? (
          <>
            {source.type === "local-folder" ? (
              <div className="file-picker">
                <span className="field-label">Folder</span>
                <div className="value-with-action">
                  <span className="location-value" title={evidence.root}>
                    {evidence.root || "None"}
                  </span>
                  <button type="button" id="source-root" onClick={() => void choose("source-root", (path) => change((value) => { value.evidence!.root = path; }))}>
                    Choose…
                  </button>
                </div>
              </div>
            ) : (
              <div className="file-picker">
                <span className="field-label">Program</span>
                <div className="value-with-action">
                  <span className="location-value" title={evidence.command}>
                    {evidence.command || "None"}
                  </span>
                  <button type="button" id="source-program" onClick={() => void choose("transfer-program", (path) => change((value) => { value.evidence!.command = path; }))}>
                    Choose…
                  </button>
                </div>
              </div>
            )}
            <label htmlFor="source-scope">Scope</label>
            <input id="source-scope" type="text" value={evidence.scope} onChange={(event) => change((value) => { value.evidence!.scope = event.target.value; })} />
            {source.plan ? (
              <>
                <label htmlFor="source-suffixes">Accepted suffixes</label>
                <input
                  id="source-suffixes"
                  type="text"
                  spellCheck={false}
                  value={source.plan.members.join(", ")}
                  onChange={(event) => change((value) => { value.plan!.members = event.target.value.split(",").map((part) => part.trim()).filter(Boolean); })}
                />
                {plans ? (
                  <div className="field-pair">
                    <div>
                      <label htmlFor="source-framing">Framing</label>
                      <select id="source-framing" value={source.plan.framing} onChange={(event) => change((value) => { value.plan!.framing = event.target.value as ImportFraming; })}>
                        {plans.framings.map((entry) => (
                          <option key={entry} value={entry}>
                            {entry === "mllp" ? "MLLP" : entry === "batch" ? "Batch" : "Raw"}
                          </option>
                        ))}
                      </select>
                    </div>
                    <div>
                      <label htmlFor="source-terminator">Segment terminator</label>
                      <select id="source-terminator" value={source.plan.terminator} onChange={(event) => change((value) => { value.plan!.terminator = event.target.value as HL7Terminator; })}>
                        {plans.terminators.map((entry) => (
                          <option key={entry} value={entry}>
                            {entry.toUpperCase()}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>
                ) : null}
              </>
            ) : null}
          </>
        ) : null}

        {listener ? (
          <>
            <div className="field-pair">
              <div>
                <label htmlFor="source-bind-address">Bind address</label>
                <input id="source-bind-address" type="text" spellCheck={false} value={listener.bind_address} onChange={(event) => change((value) => { value.listener!.bind_address = event.target.value; if (loopback(event.target.value)) value.listener!.allow_remote = false; })} />
              </div>
              <div className="field-narrow">
                <label htmlFor="source-port">Port</label>
                <input id="source-port" type="text" inputMode="numeric" value={numbers.port} onChange={(event) => { setNumbers({ ...numbers, port: event.target.value }); setDirty(true); }} />
              </div>
            </div>
            {!loopback(listener.bind_address) ? (
              <label className="check">
                <input id="source-allow-remote" type="checkbox" checked={listener.allow_remote} onChange={(event) => change((value) => { value.listener!.allow_remote = event.target.checked; })} />
                {`Allow remote connections to ${listener.bind_address}`}
              </label>
            ) : null}
            <label htmlFor="source-transport">Transport</label>
            <select id="source-transport" value={listener.transport} onChange={(event) => change((value) => { value.listener!.transport = event.target.value as ListenerTransport; })}>
              {(vocabulary?.listener_transports ?? [listener.transport]).map((value) => (
                <option key={value} value={value}>
                  {TRANSPORTS[value]}
                </option>
              ))}
            </select>
            {listener.transport !== "plain" ? (
              <>
                <div className="file-picker">
                  <span className="field-label">Certificate</span>
                  <div className="value-with-action">
                    <span className="location-value" title={listener.tls_certificate}>
                      {listener.tls_certificate || "None"}
                    </span>
                    <button type="button" id="source-certificate" onClick={() => void choose("certificate", (path) => change((value) => { value.listener!.tls_certificate = path; }))}>
                      Choose…
                    </button>
                  </div>
                </div>
                <label htmlFor="source-tls-key">Key credential</label>
                <select id="source-tls-key" value={listener.tls_key_reference ?? ""} onChange={(event) => change((value) => { value.listener!.tls_key_reference = event.target.value; })}>
                  {!listener.tls_key_reference ? <option value="">Choose a credential</option> : null}
                  {credentials.map((row) => (
                    <option key={row.name} value={row.name}>
                      {row.name}
                    </option>
                  ))}
                  {listener.tls_key_reference && !credentials.some((row) => row.name === listener.tls_key_reference) ? <option value={listener.tls_key_reference}>{listener.tls_key_reference}</option> : null}
                </select>
                {listener.transport === "mutual-tls" ? (
                  <div className="file-picker">
                    <span className="field-label">Client CA</span>
                    <div className="value-with-action">
                      <span className="location-value" title={listener.client_ca}>
                        {listener.client_ca || "None"}
                      </span>
                      <button type="button" id="source-client-ca" onClick={() => void choose("client-ca", (path) => change((value) => { value.listener!.client_ca = path; }))}>
                        Choose…
                      </button>
                    </div>
                  </div>
                ) : null}
              </>
            ) : null}
            <div className="field-pair">
              <div>
                <label htmlFor="source-message-limit">Message limit</label>
                <input id="source-message-limit" type="text" inputMode="numeric" value={numbers.messages} onChange={(event) => { setNumbers({ ...numbers, messages: event.target.value }); setDirty(true); }} />
              </div>
              <div>
                <label htmlFor="source-connection-limit">Connection limit</label>
                <input id="source-connection-limit" type="text" inputMode="numeric" value={numbers.connections} onChange={(event) => { setNumbers({ ...numbers, connections: event.target.value }); setDirty(true); }} />
              </div>
            </div>
            <label htmlFor="source-idle-timeout">Idle timeout</label>
            <input id="source-idle-timeout" type="text" value={listener.idle_timeout} onChange={(event) => change((value) => { value.listener!.idle_timeout = event.target.value; })} />
            <label htmlFor="source-ack-code">ACK code</label>
            <select id="source-ack-code" value={listener.ack_code} onChange={(event) => change((value) => { value.listener!.ack_code = event.target.value as AckCode; })}>
              {(vocabulary?.ack_codes ?? [listener.ack_code]).map((code) => (
                <option key={code} value={code}>
                  {code}
                </option>
              ))}
            </select>
            <div>
              <button type="button" id="source-responder" className="quiet" onClick={() => setResponder(true)}>
                Responder…
              </button>
            </div>
          </>
        ) : null}
        {chooseFailure ? <p className="field-error" role="alert">{chooseFailure}</p> : null}
      </FormDialog>
      {responder && listener ? (
        <ResponderSheet
          source={source}
          faults={faults?.actions ?? []}
          defaultDelay={faults?.default_delay_ms ?? 0}
          onClose={() => setResponder(false)}
          onDone={(choices) => {
            change((value) => { value.responder_choices = choices; });
            setResponder(false);
          }}
        />
      ) : null}
    </>
  );
}

type Choices = NonNullable<CaptureSourceDraft["responder_choices"]>;

/** The responder's acknowledgement enhancements and one simulated fault, for
 * a nonproduction test receiver only. */
function ResponderSheet({
  source,
  faults,
  defaultDelay,
  onClose,
  onDone,
}: {
  source: CaptureSourceDraft;
  faults: { action: string; waits: boolean }[];
  defaultDelay: number;
  onClose: () => void;
  onDone: (choices: Choices) => void;
}) {
  const held = source.responder_choices;
  const [enhanced, setEnhanced] = useState(held?.enhanced ?? false);
  // "none" is how the facade declares no fault.
  const [fault, setFault] = useState(held?.fault && held.fault !== "none" ? held.fault : "");
  const [delay, setDelay] = useState(String(held?.fault_delay_ms ?? defaultDelay));
  const waits = faults.some((entry) => entry.action === fault && entry.waits);
  return (
    <FormDialog
      open
      title="Responder"
      submitLabel="Done"
      onClose={onClose}
      onSubmit={() => {
        const ms = wholeNumber(delay);
        if (waits && (ms === null || ms <= 0)) return { reason: "Enter a delay in milliseconds.", field: "responder-delay" };
        onDone({ ...(held ?? {}), enhanced, fault: fault || "none", fault_delay_ms: waits ? (ms ?? 0) : (held?.fault_delay_ms ?? defaultDelay) });
        return null;
      }}
    >
      <label className="check">
        <input type="checkbox" checked={enhanced} onChange={(event) => setEnhanced(event.target.checked)} />
        Enhanced acknowledgement (commit, then application ACK)
      </label>
      <label htmlFor="responder-fault">Simulated fault</label>
      <select id="responder-fault" value={fault} onChange={(event) => setFault(event.target.value)}>
        <option value="">None</option>
        {faults.filter((entry) => entry.action !== "none").map((entry) => (
          <option key={entry.action} value={entry.action}>
            {FAULT_WORDS[entry.action] ?? entry.action}
          </option>
        ))}
      </select>
      {waits ? (
        <>
          <label htmlFor="responder-delay">Delay (ms)</label>
          <input id="responder-delay" type="text" inputMode="numeric" value={delay} onChange={(event) => setDelay(event.target.value)} />
        </>
      ) : null}
    </FormDialog>
  );
}


/** The exact saved source and responder limits, inspected without starting it. */
function CaptureSourceDetails({ source, onClose }: { source: CaptureSourceDraft; onClose: () => void }) {
  const evidence = source.evidence;
  const policy = source.responder;
  const enhanced = policy?.enhanced_acknowledgement;
  return <Modal open title="Source details" size="wide" onClose={onClose} footer={<div className="dialog-footer"><button type="button" onClick={onClose}>Close</button></div>}>
    {evidence ? <ValueRows label="Source limits" rows={[
      { label: "Name", value: evidence.name }, { label: "Scope", value: evidence.scope },
      { label: "Maximum entries", value: String(evidence.quota.max_entries) },
      { label: "Maximum entry bytes", value: String(evidence.quota.max_entry_bytes) },
      { label: "Maximum total bytes", value: String(evidence.quota.max_total_bytes) },
      { label: "Attempts", value: String(evidence.retry.attempts) },
      { label: "Backoff", value: evidence.retry.backoff },
    ]} /> : null}
    {policy ? <>
      <ValueRows label="Responder policy" rows={[
        { label: "Name", value: policy.name }, { label: "Source label", value: policy.source_label },
        { label: "Acknowledgement rule", value: policy.acknowledgement.operator === "original-mode-fixed-code" ? "Fixed response" : "Unsupported rule" }, { label: "ACK code", value: policy.acknowledgement.code },
        { label: "Message type rule", value: policy.accepted_message_types.operator === "any-message-type" ? "Any message type" : policy.accepted_message_types.operator === "message-type-in" ? "Allowed types" : "Unsupported rule" },
        { label: "Accepted message types", value: policy.accepted_message_types.values.join(", ") || "None listed" },
        { label: "Enhanced acknowledgement", value: enhanced?.operator === "unsupported" ? "Not supported" : enhanced ? "Configured" : "Not configured" },
        ...(enhanced ? [{ label: "Commit ACK", value: enhanced.accept_code }, { label: "Application ACK", value: enhanced.application_code }, { label: "Application delivery", value: APPLICATION_DELIVERY[enhanced.application_delivery] ?? "Unsupported delivery" }, { label: "Application endpoint", value: enhanced.application_endpoint || "None" }, { label: "Transport approved", value: enhanced.approved_transport ? "Yes" : "No" }] : []),
      ]} />
      {policy.faults ? <>
        <ValueRows label="Fault scope" rows={[{ label: "Environment", value: policy.faults.environment_class === "nonproduction" ? "Nonproduction" : "Unknown classification" }, { label: "Approved test endpoints", value: policy.faults.approved_test_endpoints.join(", ") || "None listed" }]} />
        <ol aria-label="Fault program">{policy.faults.steps.map((step, at) => <li key={at}>{`Message ${step.message} · ${FAULT_STAGES[step.stage] ?? "Unsupported stage"} · ${FAULT_WORDS[step.action] ?? "Unsupported action"} · ${step.delay_ms} ms`}</li>)}</ol>
      </> : <p>No fault program</p>}
    </> : source.listener ? <p>No separate responder policy</p> : null}
  </Modal>;
}
