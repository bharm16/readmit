import { useCallback, useEffect, useRef, useState } from "react";
import {
  observationSupport,
  openObservationSource,
  openObservationWindow,
  saveObservationSource,
  saveObservationWindow,
  validateObservationPair,
  validateObservationSource,
  validateObservationWindow,
  collectObservation,
  explainObservation,
  bindCaptureObservation,
  type CaptureObservationBinding,
  type EditorDraft,
  type ObservationAdapterSupport,
  type ObservationAbsenceSummary,
  type ObservationSource,
  type ObservationWindow,
} from "./bindings";
import { draftFor, useRetainer, RetentionStatus } from "./drafting";
import { Report, type Indicators } from "./shell";
import "./observation.css";

const KINDS = ["file-export", "http-api", "downstream-capture", "database-query"] as const;

function emptyWindow(): ObservationWindow {
  return {
    schema: "readmit-observation-window/v1",
    source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
    watermark: { kind: "none", position: "" },
    pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
    completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 100, max_samples: 16 },
  };
}

function emptyFileSource(): ObservationSource {
  return {
    schema: "readmit-observation-source/v1",
    source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
    enabled: true,
    freshness: { max_age: "1h" },
    extraction: {
      envelope: "csv",
      encoding: "utf-8",
      csv: { delimiter: ",", record_separator: "lf", header: "present", fields: 2 },
      record_key: ["appointment"],
    },
    file: { path: "export.csv", max_bytes: 65536 },
    http: null,
    capture: null,
  };
}

/** The source as its own contract declares it. The facade answers with every
 * transport member, and readmit-observation-source/v1 never had the capture
 * transport: sending that member back would be refused as a v1 document with
 * a member v1 never had, so a v1 source is saved without it. */
function declared(source: ObservationSource): ObservationSource {
  if (source.schema !== "readmit-observation-source/v1") return source;
  const { capture: _, ...v1 } = source;
  return v1 as ObservationSource;
}

/** What the panel says about one named document it read or wrote: the
 * contract and identity it was read with, or why it was refused. */
function documentLine(kind: "Source" | "Window", file: string, outcome: string, schema: string, identity: string): string {
  return `${kind} document ${file}: ${outcome} ${schema}, identity ${identity}.`;
}

function documentRefused(kind: "Source" | "Window", file: string, outcome: string, reason: string | undefined): string {
  return `${kind} document ${file} ${outcome}: ${reason ?? "no reason was given"}`;
}

function applyKind(source: ObservationSource, kind: (typeof KINDS)[number]): ObservationSource {
  const shared = {
    source: { ...source.source, kind },
    enabled: source.enabled,
    freshness: source.freshness,
  };
  if (kind === "file-export") {
    return {
      ...shared,
      schema: "readmit-observation-source/v1",
      extraction: {
        envelope: "csv",
        encoding: "utf-8",
        csv: { delimiter: ",", record_separator: "lf", header: "present", fields: 2 },
        record_key: ["appointment"],
      },
      file: { path: "export.csv", max_bytes: 65536 },
      http: null,
      capture: null,
    };
  }
  if (kind === "http-api") {
    return {
      ...shared,
      schema: "readmit-observation-source/v1",
      extraction: {
        envelope: "json",
        encoding: "utf-8",
        json: { record_path: ["items"] },
        record_key: ["id"],
      },
      file: null,
      http: {
        url: "https://lab.example.invalid:8443/appointments",
        classification: "nonproduction",
        ca_file: "",
        server_name: "lab.example.invalid",
        timeout: "5s",
        max_bytes: 65536,
        retry: { attempts: 0, delay: "0s" },
        credential: null,
      },
      capture: null,
    };
  }
  if (kind === "downstream-capture") {
    return {
      ...shared,
      schema: "readmit-observation-source/v2",
      extraction: null,
      file: null,
      http: null,
      capture: { path: "downstream.case", kinds: ["message"], record_key: "SCH-1.1", max_occurrences: 100 },
    };
  }
  return {
    ...shared,
    schema: "readmit-observation-source/v3",
    extraction: null,
    file: null,
    http: null,
    capture: null,
    database: {
      driver: "postgresql",
      address: "127.0.0.1:5432",
      classification: "nonproduction",
      name: "scheduling",
      username: "observer",
      ca_file: "db-ca.pem",
      server_name: "db.example.invalid",
      credential: {
        store: "os-keychain",
        address: "127.0.0.1:5432",
        purpose: "database-observation",
        command: "/usr/bin/true",
        arguments: [],
      },
      view: ["public", "observed"],
      record_key: "appointment",
      key_type: "text",
      filters: [],
      limits: null,
    },
  };
}

/** Observation authoring panel: sources, windows, local validation, and
 * explicitly authorized collection. Opening never queries a network source. */
export function ObservationPanel({
  workspace,
  busy,
  indicators,
  drafts,
  captureBinding,
  onBindToTest,
  onClose,
}: {
  workspace: string;
  busy: boolean;
  indicators: Indicators;
  drafts: EditorDraft[];
  captureBinding?: CaptureObservationBinding | null;
  onBindToTest?: (observationFile: string) => void;
  onClose: () => void;
}) {
  const [support, setSupport] = useState<ObservationAdapterSupport[]>([]);
  const [sourceFile, setSourceFile] = useState("observation-source.json");
  const [windowFile, setWindowFile] = useState("observation-window.json");
  const [sourceFileEntry, setSourceFileEntry] = useState(sourceFile);
  const [windowFileEntry, setWindowFileEntry] = useState(windowFile);
  const [outputFile, setOutputFile] = useState("observation-completion.json");
  const [snapshotDir, setSnapshotDir] = useState("observation-snapshot");
  const [source, setSource] = useState<ObservationSource>(emptyFileSource());
  const [windowDoc, setWindowDoc] = useState<ObservationWindow>(emptyWindow());
  const [sourceIdentity, setSourceIdentity] = useState("");
  const [windowIdentity, setWindowIdentity] = useState("");
  const [sourceDirty, setSourceDirty] = useState(false);
  const [windowDirty, setWindowDirty] = useState(false);
  const [confirming, setConfirming] = useState<"source" | "window" | null>(null);
  // What the last open, save or validation of each named document said: the
  // identity it was read with, or why it was refused.
  const [sourceCheck, setSourceCheck] = useState<string | null>(null);
  const [windowCheck, setWindowCheck] = useState<string | null>(null);
  // A named document the reader refused on opening is never replaced by what
  // the editor holds: it may be a later version this release cannot read.
  const [sourceRefused, setSourceRefused] = useState(false);
  const [windowRefused, setWindowRefused] = useState(false);
  const [authorize, setAuthorize] = useState(false);
  const [summary, setSummary] = useState<ObservationAbsenceSummary | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [result, setResult] = useState<{ state: "completed" | "failed" | "cancelled" | "busy" | "empty" | "permission_denied"; reason?: string } | null>(null);
  const report = (state: "completed" | "failed" | "cancelled" | "busy" | "empty" | "permission_denied", reason?: string) => {
    setResult(reason ? { state, reason } : { state });
  };
  const retainer = useRetainer();

  useEffect(() => {
    void observationSupport().then((res) => {
      if (res.state === "completed") setSupport(res.support ?? []);
    });
  }, []);

  // Opening the editor never queries: only load local documents / defaults.
  // Each document is read again only when its own name changes, so naming
  // the other one never replaces what was typed into this one. A document the
  // reader refuses is said to be refused rather than shown as a new one. The
  // editor and its actions stay closed until every read has answered, since a
  // read that landed after a person had started typing would replace what
  // they typed; the file names stay open, because naming a document is what
  // starts a read.
  const sourceReadKey = useRef("");
  const windowReadKey = useRef("");
  const pendingReads = useRef(0);
  const [reading, setReading] = useState(false);
  const blocked = busy || reading || confirming !== null;
  const keepEdits = () => {
    if (confirming === "source") setSourceFileEntry(sourceFile);
    if (confirming === "window") setWindowFileEntry(windowFile);
    setConfirming(null);
  };
  const replaceEdits = () => {
    if (confirming === "source") {
      setSourceIdentity("");
      setSourceFile(sourceFileEntry);
    }
    if (confirming === "window") {
      setWindowIdentity("");
      setWindowFile(windowFileEntry);
    }
    setConfirming(null);
  };
  useEffect(() => {
    let cancelled = false;
    const sourceKey = `${workspace}\n${sourceFile}`;
    const windowKey = `${workspace}\n${windowFile}`;
    pendingReads.current += 1;
    setReading(true);
    void (async () => {
      try {
        if (sourceReadKey.current !== sourceKey) {
          const read = await openObservationSource(workspace, sourceFile);
          if (cancelled) return;
          sourceReadKey.current = sourceKey;
          const refused = read.state !== "completed" || !read.source;
          setSourceRefused(refused);
          if (read.source && !refused) {
            setSource(read.source);
            setSourceIdentity(read.identity ?? "");
            setSourceDirty(false);
            setSourceCheck(null);
          } else {
            setSourceIdentity("");
            setSourceCheck(documentRefused("Source", sourceFile, "not opened", read.reason));
          }
        }
        if (windowReadKey.current !== windowKey) {
          const read = await openObservationWindow(workspace, windowFile);
          if (cancelled) return;
          windowReadKey.current = windowKey;
          const refused = read.state !== "completed" || !read.window;
          setWindowRefused(refused);
          if (read.window && !refused) {
            setWindowDoc(read.window);
            setWindowIdentity(read.identity ?? "");
            setWindowDirty(false);
            setWindowCheck(null);
          } else {
            setWindowIdentity("");
            setWindowCheck(documentRefused("Window", windowFile, "not opened", read.reason));
          }
        }
        if (!captureBinding) {
          setNotice("Editor opened locally. No database or endpoint was queried.");
        }
      } finally {
        pendingReads.current -= 1;
        if (pendingReads.current === 0) setReading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [workspace, sourceFile, windowFile, captureBinding]);

  useEffect(() => {
    if (!captureBinding) return;
    void bindCaptureObservation({
      workspace,
      binding: captureBinding,
      relative_case: captureBinding.case_path.split(/[/\\]/).pop() || "downstream.case",
    }).then((res) => {
      if (res.state === "completed" && res.source) {
        setSource(res.source);
        setSourceIdentity("");
        setSourceDirty(true);
        setWindowDoc((w) => ({ ...w, source: { ...res.source!.source } }));
        setWindowDirty(true);
        setNotice("Bound retained capture into a downstream-capture observation source. Nothing was collected.");
      } else {
        setNotice(res.reason ?? "Capture binding refused.");
      }
    });
  }, [captureBinding, workspace]);

  const retain = useCallback(
    (kind: string, content: unknown) => {
      retainer.save({
        id: retainer.currentId(),
        kind,
        workspace,
        case: "",
        identity: "",
        content_schema: kind === "observation-source" ? source.schema : windowDoc.schema,
        content,
      });
    },
    [retainer, workspace, source.schema, windowDoc.schema],
  );

  const updateSource = (next: ObservationSource) => {
    setSource(next);
    setSourceDirty(true);
    if (next.source.kind !== source.source.kind || next.source.identity !== source.source.identity || next.source.scope !== source.source.scope) {
      setWindowDoc((w) => ({ ...w, source: { ...next.source } }));
      setWindowDirty(true);
    }
    retain("observation-source", next);
  };

  const updateWindow = (next: ObservationWindow) => {
    setWindowDoc(next);
    setWindowDirty(true);
    retain("observation-window", next);
  };

  const currentSupport = support.find((row) => {
    if (row.kind !== source.source.kind) return false;
    if (source.database) return row.adapter === source.database.driver;
    return row.adapter === source.source.kind;
  });

  const restoredSource = draftFor(drafts, "observation-source", workspace);
  const restoredWindow = draftFor(drafts, "observation-window", workspace);

  return (
    <section
      className="observation"
      aria-label="Observation setup"
      onKeyDown={(event) => {
        if (confirming && event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
          event.preventDefault();
          event.stopPropagation();
          keepEdits();
        }
      }}
    >
      <header className="observation-header">
        <h3>Observation sources and windows</h3>
        <button type="button" onClick={onClose} disabled={busy}>
          Close
        </button>
      </header>
      <p className="hint">
        Author declared observation sources and completion windows with structured controls. Local
        validation never queries an endpoint; collection requires an explicit authorize checkbox.
        Database adapters stay unqualified until #75.
      </p>
      <Report indicators={indicators} progress={null} result={result} />
      {notice ? <p role="status">{notice}</p> : null}
      <RetentionStatus retention={retainer.retention} onRetry={() => retainer.retry()} onKeepAsNew={() => retainer.keepAsNew()} onDiscard={() => retainer.clear()} />

      {(restoredSource || restoredWindow) && (
        <p className="hint">A retained observation draft from this workspace is available in the editor draft store.</p>
      )}

      <h4>Adapter support</h4>
      <ul className="observation-support">
        {support.map((row) => (
          <li key={`${row.kind}-${row.adapter}`}>
            <code>{row.adapter}</code> · {row.schema} · {row.qualification}
            {row.production_claim ? "" : " · not a production claim"}
            {row.notes ? ` · ${row.notes}` : ""}
          </li>
        ))}
      </ul>
      {currentSupport ? (
        <p role="status">
          Selected adapter: <code>{currentSupport.adapter}</code> ({currentSupport.qualification}
          {currentSupport.production_claim ? "" : "; not a production claim"})
        </p>
      ) : null}

      <label htmlFor="observation-source-file">Source document</label>
      <input
        id="observation-source-file"
        value={sourceFileEntry}
        disabled={busy || confirming === "window"}
        onChange={(e) => {
          const name = e.target.value;
          setSourceFileEntry(name);
          if (sourceDirty) setConfirming(name === sourceFile ? null : "source");
          else {
            if (name !== sourceFile) setSourceIdentity("");
            setSourceFile(name);
          }
        }}
      />
      <label htmlFor="observation-window-file">Window document</label>
      <input
        id="observation-window-file"
        value={windowFileEntry}
        disabled={busy || confirming === "source"}
        onChange={(e) => {
          const name = e.target.value;
          setWindowFileEntry(name);
          if (windowDirty) setConfirming(name === windowFile ? null : "window");
          else {
            if (name !== windowFile) setWindowIdentity("");
            setWindowFile(name);
          }
        }}
      />
      {confirming ? (
        <div role="group" aria-label={`Replace ${confirming} document?`}>
          <p className="hint">
            The {confirming} document in this editor has unsaved edits. Opening {confirming === "source" ? sourceFileEntry : windowFileEntry} replaces them.
          </p>
          <button type="button" disabled={busy || reading || !(confirming === "source" ? sourceFileEntry : windowFileEntry)} onClick={replaceEdits}>
            Replace {confirming} document
          </button>
          <button type="button" disabled={busy || reading} onClick={keepEdits}>
            Keep {confirming} edits
          </button>
        </div>
      ) : null}

      <h4>Source kind</h4>
      <div className="observation-kinds">
        {KINDS.map((kind) => (
          <button
            key={kind}
            type="button"
            disabled={blocked}
            aria-pressed={source.source.kind === kind}
            onClick={() => updateSource(applyKind(source, kind))}
          >
            {kind}
          </button>
        ))}
      </div>

      <label htmlFor="observation-identity">Source identity</label>
      <input
        disabled={blocked}
        id="observation-identity"
        value={source.source.identity}
        onChange={(e) => updateSource({ ...source, source: { ...source.source, identity: e.target.value } })}
      />
      <label htmlFor="observation-scope">Scope</label>
      <input
        disabled={blocked}
        id="observation-scope"
        value={source.source.scope}
        onChange={(e) => updateSource({ ...source, source: { ...source.source, scope: e.target.value } })}
      />
      <label htmlFor="observation-freshness">Freshness max age</label>
      <input
        disabled={blocked}
        id="observation-freshness"
        value={source.freshness.max_age}
        onChange={(e) => updateSource({ ...source, freshness: { max_age: e.target.value } })}
      />
      <label>
        <input
          disabled={blocked}
          type="checkbox"
          checked={source.enabled}
          onChange={(e) => updateSource({ ...source, enabled: e.target.checked })}
        />{" "}
        Collector enabled
      </label>

      {source.file ? (
        <>
          <h4>File export</h4>
          <label htmlFor="observation-file-path">Export path</label>
          <input
            disabled={blocked}
            id="observation-file-path"
            value={source.file.path}
            onChange={(e) => updateSource({ ...source, file: { ...source.file!, path: e.target.value } })}
          />
          <label htmlFor="observation-file-max">Max bytes</label>
          <input
            disabled={blocked}
            id="observation-file-max"
            type="number"
            value={source.file.max_bytes}
            onChange={(e) => updateSource({ ...source, file: { ...source.file!, max_bytes: Number(e.target.value) } })}
          />
          <label htmlFor="observation-record-key">Record key (explicit locator text)</label>
          <input
            disabled={blocked}
            id="observation-record-key"
            value={(source.extraction?.record_key ?? []).join(".")}
            onChange={(e) =>
              updateSource({
                ...source,
                extraction: {
                  ...(source.extraction ?? { envelope: "csv", encoding: "utf-8", record_key: [] }),
                  record_key: e.target.value.split(".").filter(Boolean),
                },
              })
            }
          />
        </>
      ) : null}

      {source.http ? (
        <>
          <h4>Approved HTTPS API</h4>
          <label htmlFor="observation-http-url">URL</label>
          <input
            disabled={blocked}
            id="observation-http-url"
            value={source.http.url}
            onChange={(e) => updateSource({ ...source, http: { ...source.http!, url: e.target.value } })}
          />
          <label htmlFor="observation-http-class">Classification</label>
          <input
            disabled={blocked}
            id="observation-http-class"
            value={source.http.classification}
            onChange={(e) => updateSource({ ...source, http: { ...source.http!, classification: e.target.value } })}
          />
          <label htmlFor="observation-http-server">Verified server name</label>
          <input
            disabled={blocked}
            id="observation-http-server"
            value={source.http.server_name}
            onChange={(e) => updateSource({ ...source, http: { ...source.http!, server_name: e.target.value } })}
          />
          <label htmlFor="observation-http-ca">CA file reference</label>
          <input
            disabled={blocked}
            id="observation-http-ca"
            value={source.http.ca_file}
            onChange={(e) => updateSource({ ...source, http: { ...source.http!, ca_file: e.target.value } })}
          />
        </>
      ) : null}

      {source.capture ? (
        <>
          <h4>Downstream capture</h4>
          <p className="hint">
            Bind a retained case from capture UI (#250) or name an existing case path. This panel does
            not start a listener.
          </p>
          <label htmlFor="observation-capture-path">Retained case path</label>
          <input
            disabled={blocked}
            id="observation-capture-path"
            value={source.capture.path}
            onChange={(e) => updateSource({ ...source, capture: { ...source.capture!, path: e.target.value } })}
          />
          <label htmlFor="observation-capture-key">Record key selector</label>
          <input
            disabled={blocked}
            id="observation-capture-key"
            value={source.capture.record_key}
            onChange={(e) => updateSource({ ...source, capture: { ...source.capture!, record_key: e.target.value } })}
          />
          <label htmlFor="observation-capture-max">Max occurrences</label>
          <input
            disabled={blocked}
            id="observation-capture-max"
            type="number"
            value={source.capture.max_occurrences}
            onChange={(e) =>
              updateSource({ ...source, capture: { ...source.capture!, max_occurrences: Number(e.target.value) } })
            }
          />
        </>
      ) : null}

      {source.database ? (
        <>
          <h4>Database view (unqualified)</h4>
          <p className="hint">
            Structured view, column mapping and parameter filters only. Live qualification evidence is
            owned by #75; this UI must not claim production support.
          </p>
          <label htmlFor="observation-db-driver">Driver</label>
          <select
            disabled={blocked}
            id="observation-db-driver"
            value={source.database.driver}
            onChange={(e) => updateSource({ ...source, database: { ...source.database!, driver: e.target.value } })}
          >
            <option value="postgresql">postgresql</option>
            <option value="sqlserver">sqlserver</option>
            <option value="oracle">oracle</option>
          </select>
          <label htmlFor="observation-db-view">Approved view (schema.table or table)</label>
          <input
            disabled={blocked}
            id="observation-db-view"
            value={source.database.view.join(".")}
            onChange={(e) =>
              updateSource({
                ...source,
                database: { ...source.database!, view: e.target.value.split(".").filter(Boolean) },
              })
            }
          />
          <label htmlFor="observation-db-key">Record-key column</label>
          <input
            disabled={blocked}
            id="observation-db-key"
            value={source.database.record_key}
            onChange={(e) => updateSource({ ...source, database: { ...source.database!, record_key: e.target.value } })}
          />
          <label htmlFor="observation-db-filter-col">Parameter filter column</label>
          <input
            disabled={blocked}
            id="observation-db-filter-col"
            placeholder="status"
            onBlur={(e) => {
              const column = e.target.value.trim();
              if (!column) return;
              const value = (document.getElementById("observation-db-filter-val") as HTMLInputElement)?.value ?? "";
              updateSource({
                ...source,
                database: {
                  ...source.database!,
                  filters: [...source.database!.filters.filter((f) => f.column !== column), { column, value }],
                },
              });
            }}
          />
          <label htmlFor="observation-db-filter-val">Parameter filter value</label>
          <input id="observation-db-filter-val" placeholder="ready" disabled={blocked} />
          <label htmlFor="observation-db-address">Endpoint address</label>
          <input
            disabled={blocked}
            id="observation-db-address"
            value={source.database.address}
            onChange={(e) => {
              const address = e.target.value;
              updateSource({
                ...source,
                database: {
                  ...source.database!,
                  address,
                  credential: { ...source.database!.credential, address },
                },
              });
            }}
          />
        </>
      ) : null}

      <h4>Completion window</h4>
      <p className="hint">
        Assertions inspect this window&apos;s boundary only. An absence claim is valid only when the
        retained completion is trustworthy and complete — unavailable collectors, timeouts, stale
        responses and caps are not evidence of no output.
      </p>
      <label htmlFor="observation-watermark">Watermark kind</label>
      <select
        disabled={blocked}
        id="observation-watermark"
        value={windowDoc.watermark.kind}
        onChange={(e) =>
          updateWindow({
            ...windowDoc,
            watermark: {
              kind: e.target.value,
              position: e.target.value === "declared-position" ? windowDoc.watermark.position || "0" : "",
            },
          })
        }
      >
        <option value="none">none</option>
        <option value="collection-start">collection-start</option>
        <option value="declared-position">declared-position</option>
      </select>
      {windowDoc.watermark.kind === "declared-position" ? (
        <>
          <label htmlFor="observation-watermark-pos">Watermark position</label>
          <input
            disabled={blocked}
            id="observation-watermark-pos"
            value={windowDoc.watermark.position}
            onChange={(e) => updateWindow({ ...windowDoc, watermark: { ...windowDoc.watermark, position: e.target.value } })}
          />
        </>
      ) : null}
      <label htmlFor="observation-preexisting">Pre-existing state</label>
      <select
        disabled={blocked}
        id="observation-preexisting"
        value={windowDoc.pre_existing_state.declaration}
        onChange={(e) =>
          updateWindow({
            ...windowDoc,
            pre_existing_state: { declaration: e.target.value, baseline_identity: "" },
          })
        }
      >
        <option value="declared-empty">declared-empty</option>
        <option value="recorded-baseline">recorded-baseline</option>
        <option value="unknown">unknown</option>
      </select>
      <label htmlFor="observation-deadline">Deadline</label>
      <input
        disabled={blocked}
        id="observation-deadline"
        value={windowDoc.completion.deadline}
        onChange={(e) => updateWindow({ ...windowDoc, completion: { ...windowDoc.completion, deadline: e.target.value } })}
      />
      <label htmlFor="observation-quiet">Quiet period</label>
      <input
        disabled={blocked}
        id="observation-quiet"
        value={windowDoc.completion.quiet_period}
        onChange={(e) =>
          updateWindow({ ...windowDoc, completion: { ...windowDoc.completion, quiet_period: e.target.value } })
        }
      />
      <label htmlFor="observation-stable">Stable samples</label>
      <input
        disabled={blocked}
        id="observation-stable"
        type="number"
        value={windowDoc.completion.stable_samples}
        onChange={(e) =>
          updateWindow({
            ...windowDoc,
            completion: { ...windowDoc.completion, stable_samples: Number(e.target.value) },
          })
        }
      />

      <div className="observation-actions">
        <button
          type="button"
          disabled={blocked || sourceRefused || windowRefused}
          onClick={() => {
            void (async () => {
              // A collection reads the saved documents, so a save that did not
              // land is said plainly: collecting now would read what was
              // saved before, not what the editor shows. The pair is saved
              // source first, and a refused source leaves the window as saved.
              const savedSource = await saveObservationSource({ workspace, source_file: sourceFile, source: declared(source) });
              if (savedSource.state !== "completed") {
                report(savedSource.state, savedSource.reason);
                setSourceCheck(documentRefused("Source", sourceFile, "not saved", savedSource.reason));
                setNotice(`Not saved: ${savedSource.reason || "the source was refused"}. A collection reads the documents saved before.`);
                return;
              }
              setSourceIdentity(savedSource.identity ?? "");
              setSourceDirty(false);
              if (savedSource.source) setSource(savedSource.source);
              setSourceCheck(documentLine("Source", sourceFile, "saved", savedSource.source?.schema ?? source.schema, savedSource.identity ?? ""));
              const savedWindow = await saveObservationWindow({ workspace, window_file: windowFile, window: windowDoc });
              if (savedWindow.state !== "completed") {
                report(savedWindow.state, savedWindow.reason);
                setWindowCheck(documentRefused("Window", windowFile, "not saved", savedWindow.reason));
                setNotice(`Window not saved: ${savedWindow.reason || "the window was refused"}. The source was saved; a collection reads the window saved before.`);
                return;
              }
              setWindowIdentity(savedWindow.identity ?? "");
              setWindowDirty(false);
              if (savedWindow.window) setWindowDoc(savedWindow.window);
              setWindowCheck(documentLine("Window", windowFile, "saved", savedWindow.window?.schema ?? windowDoc.schema, savedWindow.identity ?? ""));
              report("completed");
              setNotice("Saved through shared Go writers. Identities pinned for test binding.");
            })();
          }}
        >
          Save source and window
        </button>
        <button
          type="button"
          disabled={blocked}
          onClick={() => {
            void validateObservationPair({ workspace, source_file: sourceFile, window_file: windowFile }).then((res) => {
              report(res.state, res.reason);
              setNotice(
                res.state === "completed"
                  ? "Local configuration validation passed. No endpoint was queried."
                  : res.reason ?? "Validation refused.",
              );
            });
          }}
        >
          Validate locally
        </button>
        <button
          type="button"
          disabled={blocked}
          onClick={() => {
            // The saved document, read as `readmit observe collect` reads a
            // source; what the editor holds is validated only once it is saved.
            void validateObservationSource(workspace, sourceFile).then((res) => {
              report(res.state, res.reason);
              setSourceCheck(
                res.state === "completed"
                  ? `${documentLine("Source", sourceFile, "valid", res.source?.schema ?? "", res.identity ?? "")} Nothing was collected.`
                  : documentRefused("Source", sourceFile, "refused", res.reason),
              );
            });
          }}
        >
          Validate source document
        </button>
        <button
          type="button"
          disabled={blocked}
          onClick={() => {
            // The saved document, read as `readmit observe validate` reads it.
            void validateObservationWindow(workspace, windowFile).then((res) => {
              report(res.state, res.reason);
              setWindowCheck(
                res.state === "completed"
                  ? `${documentLine("Window", windowFile, "valid", res.window?.schema ?? "", res.identity ?? "")} Nothing was observed.`
                  : documentRefused("Window", windowFile, "refused", res.reason),
              );
            });
          }}
        >
          Validate window document
        </button>
      </div>

      {sourceCheck ? <p role="status">{sourceCheck}</p> : null}
      {windowCheck ? <p role="status">{windowCheck}</p> : null}
      {sourceRefused || windowRefused ? (
        <p className="hint">
          Saving is closed while a named document is refused: the window never replaces a document it could not
          read. Name another document to save what the editor holds.
        </p>
      ) : null}
      <p className="hint">
        Pinned identities — source: <code>{sourceFileEntry === sourceFile ? sourceIdentity || "not saved" : "not saved"}</code>; window:{" "}
        <code>{windowFileEntry === windowFile ? windowIdentity || "not saved" : "not saved"}</code>
      </p>

      <h4>Authorized collection</h4>
      <label htmlFor="observation-output">Completion output</label>
      <input id="observation-output" value={outputFile} onChange={(e) => setOutputFile(e.target.value)} />
      <label htmlFor="observation-snapshot">Snapshot directory</label>
      <input id="observation-snapshot" value={snapshotDir} onChange={(e) => setSnapshotDir(e.target.value)} />
      <label>
        <input type="checkbox" checked={authorize} onChange={(e) => setAuthorize(e.target.checked)} /> I authorize a
        read-only collection against the declared source
      </label>
      <button
        type="button"
        disabled={busy || !authorize}
        onClick={() => {
          void collectObservation({
            workspace,
            source_file: sourceFile,
            window_file: windowFile,
            output_file: outputFile,
            snapshot_dir: snapshotDir,
            authorize,
          }).then((res) => {
            report(res.state, res.reason);
            setSummary(res.summary ?? null);
            setNotice(
              res.state === "completed"
                ? "Collection retained a completion through shared collectors."
                : res.reason ?? "Collection refused.",
            );
          });
        }}
      >
        Collect once
      </button>
      <button
        type="button"
        disabled={busy}
        onClick={() => {
          void explainObservation({ workspace, completion_file: outputFile, window_file: windowFile }).then((res) => {
            report(res.state, res.reason);
            setSummary(res.summary ?? null);
          });
        }}
      >
        Explain completion
      </button>

      {summary ? (
        <div className="observation-summary" role="status">
          <p>
            Status: <code>{summary.status}</code>
            {summary.trustworthy ? " (trustworthy)" : " (not trustworthy)"}
            {summary.stale ? " · stale" : ""}
            {summary.partial ? " · partial/incomplete" : ""}
          </p>
          <p>
            Records observed: {summary.records_observed}; mapped: {summary.mapped_correlations}; unmapped:{" "}
            {summary.unmapped_correlations}
          </p>
          <p>
            Absence claim:{" "}
            {summary.supported ? "supported by this completion" : `not supported — ${summary.reason ?? "incomplete"}`}
          </p>
          <p className="hint">
            Null, empty, missing and undecodable remain distinct in retained provenance. An unavailable
            collector is never evidence of no output.
          </p>
        </div>
      ) : null}

      {onBindToTest ? (
        <button type="button" disabled={busy || !windowFile} onClick={() => onBindToTest(windowFile)}>
          Bind saved window into test authoring
        </button>
      ) : null}
    </section>
  );
}
