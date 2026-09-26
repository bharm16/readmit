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
  type ObservationSourceChoices,
  type ObservationSourceDatabase,
  type ObservationWindow,
} from "./bindings";
import { draftFor, useRetainer, RetentionStatus } from "./drafting";
import { Report, type Indicators } from "./shell";
import "./observation.css";
import { useLifecycle } from "./lifecycle";
import { TaskTabs } from "./TaskTabs";

const KINDS = ["file-export", "http-api", "downstream-capture", "database-query"] as const;

/** The window the editor holds until the read of the named document answers.
 * Its pre-existing state is left unchosen: that declaration decides what an
 * absence claim may say, so only the person makes it. */
function emptyWindow(): ObservationWindow {
  return {
    schema: "readmit-observation-window/v1",
    source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
    watermark: { kind: "none", position: "" },
    pre_existing_state: { declaration: "", baseline_identity: "" },
    completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 100, max_samples: 16 },
  };
}

/** The source the editor holds until the read of the named document answers,
 * which replaces it. Its contract version is the facade's to declare. */
function emptyFileSource(): ObservationSource {
  return {
    schema: "",
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

/** What the person declared for a source: every member but its contract
 * version, which the facade picks when it saves the document. */
function choicesOf(source: ObservationSource): ObservationSourceChoices {
  const { schema: _, ...choices } = source;
  return choices;
}

/** What the panel says about one named document it read or wrote: the
 * contract and identity it was read with, or why it was refused. */
function documentLine(kind: "Source" | "Window", file: string, outcome: string, schema: string, identity: string): string {
  return `${kind} document ${file}: ${outcome} ${schema}, identity ${identity}.`;
}

function documentRefused(kind: "Source" | "Window", file: string, outcome: string, reason: string | undefined): string {
  return `${kind} document ${file} ${outcome}: ${reason ?? "no reason was given"}`;
}

/** A source of another kind, starting from that kind's example transport.
 * `schema` is the contract version the facade publishes for the kind, so a
 * retained draft names the contract its content is read under. */
function applyKind(source: ObservationSource, kind: (typeof KINDS)[number], schema: string): ObservationSource {
  const shared = {
    source: { ...source.source, kind },
    enabled: source.enabled,
    freshness: source.freshness,
  };
  if (kind === "file-export") {
    return {
      ...shared,
      schema,
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
      schema,
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
      schema,
      extraction: null,
      file: null,
      http: null,
      capture: { path: "downstream.case", kinds: ["message"], record_key: "SCH-1.1", max_occurrences: 100 },
    };
  }
  return {
    ...shared,
    schema,
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


/** What a kind of source is called on screen. The serialized kind stays the
 * value the documents carry. */
const KIND_CAPTIONS: Record<(typeof KINDS)[number], string> = {
  "file-export": "File export",
  "http-api": "HTTPS API",
  "downstream-capture": "Downstream capture",
  "database-query": "Database view",
};

const VIEWS = ["source", "rules", "collect"] as const;
type View = (typeof VIEWS)[number];
const VIEW_CAPTIONS: Record<View, string> = {
  source: "Source",
  rules: "Completion rules",
  collect: "Collect and results",
};

type DocumentKind = "source" | "window";

/** One saved document as the panel accepted it: the file it was read from or
 * written to, and the identity the shared reader computed for its bytes. A
 * document is saved only when it exists on disk with an identity. */
interface SavedDocument {
  file: string;
  identity: string;
}

function sameDocument(a: SavedDocument | null, b: SavedDocument | null): boolean {
  return a !== null && b !== null && a.file === b.file && a.identity === b.identity;
}

type CollectState = "completed" | "failed" | "cancelled" | "busy" | "empty" | "permission_denied";

/** A collection or explanation result, kept with the exact saved pair and
 * output that produced it so it is never read as evidence for another. */
interface ScopedResult {
  action: "collection" | "explanation";
  summary: ObservationAbsenceSummary;
  source: SavedDocument | null;
  window: SavedDocument | null;
  windowFile: string;
  output: string;
}

/** Observation authoring panel: sources, completion rules, validation of the
 * saved configuration, and explicitly authorized collection of the reviewed
 * saved pair. Opening never queries a network source. */
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
  const [view, setView] = useState<View>("source");
  // The file each editor holds (the one last opened or saved), and the names
  // typed into the file fields, which are only staged until Open reads them.
  const [sourceFile, setSourceFile] = useState("observation-source.json");
  const [windowFile, setWindowFile] = useState("observation-window.json");
  const [sourceFileEntry, setSourceFileEntry] = useState(sourceFile);
  const [windowFileEntry, setWindowFileEntry] = useState(windowFile);
  const [outputFile, setOutputFile] = useState("observation-completion.json");
  const [snapshotDir, setSnapshotDir] = useState("observation-snapshot");
  const [source, setSource] = useState<ObservationSource>(emptyFileSource());
  const [windowDoc, setWindowDoc] = useState<ObservationWindow>(emptyWindow());
  // The saved configuration: what is on disk as the panel last read or wrote
  // it. Edits never change it; only a completed open or save does.
  const [savedSource, setSavedSource] = useState<SavedDocument | null>(null);
  const [savedWindow, setSavedWindow] = useState<SavedDocument | null>(null);
  const [sourceDirty, setSourceDirty] = useState(false);
  const [windowDirty, setWindowDirty] = useState(false);
  // A save that wrote the source but not the window leaves no current pair
  // until both documents are saved or opened again.
  const [pairIncomplete, setPairIncomplete] = useState(false);
  const [confirming, setConfirming] = useState<DocumentKind | null>(null);
  // What the last open, save or validation of each named document said: the
  // identity it was read with, or why it was refused.
  const [sourceCheck, setSourceCheck] = useState<string | null>(null);
  const [windowCheck, setWindowCheck] = useState<string | null>(null);
  // A named document the reader refused on opening is never replaced by what
  // the editor holds: it may be a later version this release cannot read.
  const [sourceRefused, setSourceRefused] = useState(false);
  const [windowRefused, setWindowRefused] = useState(false);
  // The saved pair a person authorized collecting, captured when they gave
  // it. It is never restored from a draft, and any change to the pair or the
  // editors withdraws it.
  const [authorizedPair, setAuthorizedPair] = useState<{ source: SavedDocument; window: SavedDocument } | null>(null);
  const [scoped, setScoped] = useState<ScopedResult | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [filterDraft, setFilterDraft] = useState<{ column: string; value: string; editing: number | null }>({
    column: "",
    value: "",
    editing: null,
  });
  const [result, setResult] = useState<{ state: CollectState; reason?: string } | null>(null);
  const report = (state: CollectState, reason?: string) => {
    setResult(reason ? { state, reason } : { state });
  };
  const retainer = useRetainer();

  useEffect(() => {
    void observationSupport().then((res) => {
      if (res.state === "completed") setSupport(res.support ?? []);
    });
  }, []);

  // Opening a document is an explicit act: the file fields only stage a name,
  // and Open reads it. Each read commits only into its own editor, and only
  // while it is the latest read of that document for this workspace, so a
  // read of one document never resets the other and a late answer never
  // lands in a form that has moved on. The editor and its actions stay
  // closed while a read is pending, since a read that landed after a person
  // had started typing would replace what they typed.
  const reads = useLifecycle<DocumentKind>({ background: true });
  const reading = reads.running !== null;
  const saves = useLifecycle<"saving">();
  const saving = saves.running !== null;
  const savingNow = useRef(false);
  const blocked = busy || reading || saving || confirming !== null;
  const captureRef = useRef(captureBinding);
  captureRef.current = captureBinding;
  // What the form holds now, for answers that arrive later: a validation
  // commits only while its workspace and files are still the ones open.
  const current = useRef({ workspace, sourceFile, windowFile });
  current.current = { workspace, sourceFile, windowFile };
  const stillOpen = (asked: { workspace: string; sourceFile?: string; windowFile?: string }) =>
    asked.workspace === current.current.workspace &&
    (asked.sourceFile === undefined || asked.sourceFile === current.current.sourceFile) &&
    (asked.windowFile === undefined || asked.windowFile === current.current.windowFile);

  const openDocument = useCallback(
    (kind: DocumentKind, file: string) => {
      setAuthorizedPair(null);
      setPairIncomplete(false);
      if (kind === "source") {
        setSourceFile(file);
        setSourceFileEntry(file);
        setSavedSource(null);
        setFilterDraft({ column: "", value: "", editing: null });
        void reads.run("source", async (current) => {
          const read = await openObservationSource(workspace, file);
          if (!current()) return;
          const refused = read.state !== "completed" || !read.source;
          setSourceRefused(refused);
          if (read.source && !refused) {
            setSource(read.source);
            setSavedSource(read.identity ? { file, identity: read.identity } : null);
            setSourceDirty(false);
            setSourceCheck(null);
          } else {
            setSourceCheck(documentRefused("Source", file, "not opened", read.reason));
          }
          if (!captureRef.current) setNotice("Editor opened locally. No database or endpoint was queried.");
        });
      } else {
        setWindowFile(file);
        setWindowFileEntry(file);
        setSavedWindow(null);
        void reads.run("window", async (current) => {
          const read = await openObservationWindow(workspace, file);
          if (!current()) return;
          const refused = read.state !== "completed" || !read.window;
          setWindowRefused(refused);
          if (read.window && !refused) {
            // A window not yet on disk (no identity) is the facade's new
            // window, whose pre-existing state the person has not chosen: it
            // is offered unchosen rather than as the default's declaration.
            setWindowDoc(
              read.identity ? read.window : { ...read.window, pre_existing_state: { declaration: "", baseline_identity: "" } },
            );
            setSavedWindow(read.identity ? { file, identity: read.identity } : null);
            setWindowDirty(false);
            setWindowCheck(null);
          } else {
            setWindowCheck(documentRefused("Window", file, "not opened", read.reason));
          }
          if (!captureRef.current) setNotice("Editor opened locally. No database or endpoint was queried.");
        });
      }
    },
    [reads, workspace],
  );

  // The panel opens the documents it names when it opens and for each new
  // workspace; after that only Open reads a document.
  useEffect(() => {
    reads.withdraw();
    openDocument("source", sourceFile);
    openDocument("window", windowFile);
    // Only a new workspace opens the named documents again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace]);

  // The confirmation takes focus when it appears, so Escape and the keyboard
  // reach its choices from the Open action that asked.
  const confirmOpen = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (confirming) confirmOpen.current?.focus();
  }, [confirming]);

  const requestOpen = (kind: DocumentKind) => {
    const dirty = kind === "source" ? sourceDirty : windowDirty;
    if (dirty) {
      setConfirming(kind);
      return;
    }
    openDocument(kind, kind === "source" ? sourceFileEntry : windowFileEntry);
  };
  const keepEdits = () => {
    if (confirming === "source") setSourceFileEntry(sourceFile);
    if (confirming === "window") setWindowFileEntry(windowFile);
    setConfirming(null);
  };
  const replaceEdits = () => {
    const kind = confirming;
    setConfirming(null);
    if (kind) openDocument(kind, kind === "source" ? sourceFileEntry : windowFileEntry);
  };

  useEffect(() => {
    if (!captureBinding) return;
    void bindCaptureObservation({
      workspace,
      binding: captureBinding,
      relative_case: captureBinding.case_path.split(/[/\\]/).pop() || "downstream.case",
    }).then((res) => {
      if (res.state === "completed" && res.source) {
        setSource(res.source);
        setSourceDirty(true);
        setWindowDoc((w) => ({ ...w, source: { ...res.source!.source } }));
        setWindowDirty(true);
        setAuthorizedPair(null);
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
    setAuthorizedPair(null);
    if (next.source.kind !== source.source.kind || next.source.identity !== source.source.identity || next.source.scope !== source.source.scope) {
      setWindowDoc((w) => ({ ...w, source: { ...next.source } }));
      setWindowDirty(true);
    }
    retain("observation-source", next);
  };

  const updateWindow = (next: ObservationWindow) => {
    setWindowDoc(next);
    setWindowDirty(true);
    setAuthorizedPair(null);
    retain("observation-window", next);
  };

  const updateFilters = (filters: ObservationSourceDatabase["filters"]) => {
    updateSource({ ...source, database: { ...source.database!, filters } });
  };

  const commitFilter = () => {
    const column = filterDraft.column.trim();
    if (!column || !source.database) return;
    const row = { column, value: filterDraft.value };
    const filters = source.database.filters;
    updateFilters(
      filterDraft.editing === null ? [...filters, row] : filters.map((filter, index) => (index === filterDraft.editing ? row : filter)),
    );
    setFilterDraft({ column: "", value: "", editing: null });
  };

  const supportFor = (kind: string, adapter?: string) =>
    support.find((row) => row.kind === kind && row.adapter === (adapter ?? kind)) ?? support.find((row) => row.kind === kind);
  const currentSupport = support.find((row) => {
    if (row.kind !== source.source.kind) return false;
    if (source.database) return row.adapter === source.database.driver;
    return row.adapter === source.source.kind;
  });
  const qualificationOf = (kind: (typeof KINDS)[number]) => {
    const row = kind === source.source.kind && source.database ? supportFor(kind, source.database.driver) : supportFor(kind);
    if (!row) return "support not reported";
    return row.production_claim ? row.qualification : `${row.qualification}; not a production claim`;
  };

  const restoredSource = draftFor(drafts, "observation-source", workspace);
  const restoredWindow = draftFor(drafts, "observation-window", workspace);

  // The saved pair Collect reads, and why it cannot be collected now. Collect
  // reads only saved documents, so it waits for a clean, current pair the
  // person has reviewed; it never saves on its own.
  const selectionMoved = sourceFileEntry !== sourceFile || windowFileEntry !== windowFile;
  const dirty = sourceDirty || windowDirty;
  const pairBlocked: string | null = reading
    ? "A document is being read."
    : saving
      ? "The observation is being saved."
      : sourceRefused || windowRefused
        ? "A named document was refused. Open a document the reader accepts before collecting."
        : selectionMoved
          ? "The file fields name documents that are not open. Open them, or restore the open names, before collecting."
          : dirty
            ? "The editor has unsaved edits. Save observation first: Collect reads only the saved documents, never the editor."
            : pairIncomplete
              ? "The last save did not write both documents. Save observation again before collecting."
              : !savedSource || !savedWindow
                ? "Save observation first: Collect reads only saved documents, and this pair is not saved."
                : null;
  const reviewedPair = pairBlocked === null && savedSource && savedWindow ? { source: savedSource, window: savedWindow } : null;
  const authorized =
    reviewedPair !== null &&
    authorizedPair !== null &&
    sameDocument(authorizedPair.source, reviewedPair.source) &&
    sameDocument(authorizedPair.window, reviewedPair.window);

  // A test binds only the saved window, and only while it is what the editor
  // shows: an edit or another document withdraws the handoff.
  const windowBindable =
    !blocked && !windowRefused && !windowDirty && savedWindow !== null && windowFileEntry === savedWindow.file && windowFile === savedWindow.file;

  const scopeCurrent =
    scoped !== null &&
    !dirty &&
    scoped.windowFile === windowFile &&
    (scoped.window === null ? savedWindow === null : sameDocument(scoped.window, savedWindow)) &&
    (scoped.action === "explanation" || sameDocument(scoped.source, savedSource)) &&
    scoped.output === outputFile;

  // The pre-existing state is never chosen for the person, so a window
  // without one waits for their choice before it can be saved.
  const undeclared = windowDoc.pre_existing_state.declaration === "";

  const saveObservation = async () => {
    // One save at a time: a second press while the pair is being written
    // does nothing.
    if (savingNow.current) return;
    savingNow.current = true;
    setAuthorizedPair(null);
    try {
      await saves.run("saving", async () => {
        // A collection reads the saved documents, so a save that did not
        // land is said plainly: collecting now would read what was saved
        // before, not what the editor shows. The pair is saved source first,
        // and a refused source leaves the window as saved.
        const writtenSource = await saveObservationSource({ workspace, source_file: sourceFile, choices: choicesOf(source) });
        if (writtenSource.state !== "completed") {
          report(writtenSource.state, writtenSource.reason);
          setSourceCheck(documentRefused("Source", sourceFile, "not saved", writtenSource.reason));
          setNotice(`Not saved: ${writtenSource.reason || "the source was refused"}. A collection reads the documents saved before.`);
          return;
        }
        setSavedSource(writtenSource.identity ? { file: sourceFile, identity: writtenSource.identity } : null);
        setSourceDirty(false);
        if (writtenSource.source) setSource(writtenSource.source);
        setSourceCheck(documentLine("Source", sourceFile, "saved", writtenSource.source?.schema ?? source.schema, writtenSource.identity ?? ""));
        const writtenWindow = await saveObservationWindow({ workspace, window_file: windowFile, window: windowDoc });
        if (writtenWindow.state !== "completed") {
          setPairIncomplete(true);
          report(writtenWindow.state, writtenWindow.reason);
          setWindowCheck(documentRefused("Window", windowFile, "not saved", writtenWindow.reason));
          setNotice(`Window not saved: ${writtenWindow.reason || "the window was refused"}. The source was saved; a collection reads the window saved before.`);
          return;
        }
        setSavedWindow(writtenWindow.identity ? { file: windowFile, identity: writtenWindow.identity } : null);
        setWindowDirty(false);
        setPairIncomplete(false);
        if (writtenWindow.window) setWindowDoc(writtenWindow.window);
        setWindowCheck(documentLine("Window", windowFile, "saved", writtenWindow.window?.schema ?? windowDoc.schema, writtenWindow.identity ?? ""));
        report("completed");
        setNotice("Saved through shared Go writers. Identities pinned for test binding.");
      });
    } finally {
      savingNow.current = false;
    }
  };

  const savedLine = (saved: SavedDocument | null) =>
    saved ? (
      <>
        <code>{saved.identity}</code> ({saved.file})
      </>
    ) : (
      <code>not saved</code>
    );

  const adapterLine = currentSupport ? (
    <p role="status">
      Selected adapter: <code>{currentSupport.adapter}</code> ({currentSupport.qualification}
      {currentSupport.production_claim ? "" : "; not a production claim"})
    </p>
  ) : null;

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
        <h3>Observations</h3>
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
      <RetentionStatus retention={retainer.retention} onRetry={() => retainer.retry()} onKeepAsNew={() => retainer.keepAsNew()} onDiscard={() => void retainer.dropCurrent()} />

      {(restoredSource || restoredWindow) && (
        <p className="hint">A retained observation draft from this workspace is available in the editor draft store.</p>
      )}

      <div className="observation-files" role="group" aria-label="Observation files">
        <div className="observation-file">
          <label htmlFor="observation-source-file">Source file</label>
          <div className="observation-file-row">
            <input
              id="observation-source-file"
              value={sourceFileEntry}
              disabled={busy || confirming !== null || saving}
              onChange={(e) => setSourceFileEntry(e.target.value)}
            />
            <button type="button" disabled={busy || confirming !== null || saving || !sourceFileEntry} onClick={() => requestOpen("source")}>
              Open source
            </button>
          </div>
          <p className="hint">
            {sourceRefused
              ? `${sourceFile}: refused`
              : reads.running === "source"
                ? `${sourceFile}: opening`
                : sourceDirty
                  ? `${sourceFile}: unsaved edits`
                  : savedSource
                    ? `${sourceFile}: saved`
                    : `${sourceFile}: not saved yet`}
            {sourceFileEntry !== sourceFile ? ` · ${sourceFileEntry || "no file"} is named but not open` : ""}
          </p>
        </div>
        <div className="observation-file">
          <label htmlFor="observation-window-file">Window file</label>
          <div className="observation-file-row">
            <input
              id="observation-window-file"
              value={windowFileEntry}
              disabled={busy || confirming !== null || saving}
              onChange={(e) => setWindowFileEntry(e.target.value)}
            />
            <button type="button" disabled={busy || confirming !== null || saving || !windowFileEntry} onClick={() => requestOpen("window")}>
              Open window
            </button>
          </div>
          <p className="hint">
            {windowRefused
              ? `${windowFile}: refused`
              : reads.running === "window"
                ? `${windowFile}: opening`
                : windowDirty
                  ? `${windowFile}: unsaved edits`
                  : savedWindow
                    ? `${windowFile}: saved`
                    : `${windowFile}: not saved yet`}
            {windowFileEntry !== windowFile ? ` · ${windowFileEntry || "no file"} is named but not open` : ""}
          </p>
        </div>
      </div>
      {confirming ? (
        <div role="group" aria-label={confirming === "source" ? "Replace unsaved source edits?" : "Replace unsaved window edits?"}>
          <p className="hint">
            The {confirming} in this editor has unsaved edits. Opening {confirming === "source" ? sourceFileEntry : windowFileEntry} will
            replace those unsaved edits.
          </p>
          <button type="button" ref={confirmOpen} disabled={busy || reading} onClick={replaceEdits}>
            {confirming === "source" ? "Open selected source" : "Open selected window"}
          </button>
          <button type="button" disabled={busy || reading} onClick={keepEdits}>
            {confirming === "source" ? "Keep source edits" : "Keep window edits"}
          </button>
        </div>
      ) : null}

      <p className="hint">Saving writes both documents: the source first, then the completion window.</p>
      <div className="observation-actions">
        <button
          type="button"
          disabled={blocked || sourceRefused || windowRefused || selectionMoved || undeclared}
          onClick={() => void saveObservation()}
        >
          Save observation
        </button>
        <button
          type="button"
          disabled={blocked}
          aria-describedby="observation-validate-scope"
          onClick={() => {
            const files = { source: sourceFile, window: windowFile };
            const unsaved = dirty;
            void validateObservationPair({ workspace, source_file: files.source, window_file: files.window }).then((res) => {
              if (!stillOpen({ workspace, sourceFile: files.source, windowFile: files.window })) return;
              report(res.state, res.reason);
              setNotice(
                res.state === "completed"
                  ? `Saved configuration valid: ${files.source} and ${files.window} as saved. No endpoint was queried.${unsaved ? " Unsaved edits in the editor were not validated." : ""}`
                  : `Saved configuration ${files.source} and ${files.window} refused: ${res.reason ?? "no reason was given"}`,
              );
            });
          }}
        >
          Validate saved configuration
        </button>
        <button
          type="button"
          disabled={blocked}
          onClick={() => {
            // The saved document, read as `readmit observe collect` reads a
            // source; what the editor holds is validated only once it is saved.
            const file = sourceFile;
            void validateObservationSource(workspace, file).then((res) => {
              if (!stillOpen({ workspace, sourceFile: file })) return;
              report(res.state, res.reason);
              setSourceCheck(
                res.state === "completed"
                  ? `${documentLine("Source", file, "valid", res.source?.schema ?? "", res.identity ?? "")} Nothing was collected.`
                  : documentRefused("Source", file, "refused", res.reason),
              );
            });
          }}
        >
          Validate saved source
        </button>
        <button
          type="button"
          disabled={blocked}
          onClick={() => {
            // The saved document, read as `readmit observe validate` reads it.
            const file = windowFile;
            void validateObservationWindow(workspace, file).then((res) => {
              if (!stillOpen({ workspace, windowFile: file })) return;
              report(res.state, res.reason);
              setWindowCheck(
                res.state === "completed"
                  ? `${documentLine("Window", file, "valid", res.window?.schema ?? "", res.identity ?? "")} Nothing was observed.`
                  : documentRefused("Window", file, "refused", res.reason),
              );
            });
          }}
        >
          Validate saved window
        </button>
      </div>
      <p className="hint" id="observation-validate-scope">
        Validation reads the saved files, not the editor. No endpoint is queried.
      </p>

      {sourceCheck ? <p role="status">{sourceCheck}</p> : null}
      {windowCheck ? <p role="status">{windowCheck}</p> : null}
      {sourceRefused || windowRefused ? (
        <p className="hint">
          Saving is closed while a named document is refused: the window never replaces a document it could not
          read. Open another document to save what the editor holds.
        </p>
      ) : selectionMoved ? (
        <p className="hint">Saving is closed while a file field names a document that is not open. Open it first.</p>
      ) : undeclared ? (
        <p className="hint">Choose the pre-existing state under Completion rules before saving: readmit does not choose it for you.</p>
      ) : null}
      <div className="observation-saved" role="group" aria-label="Saved configuration">
        <p>Saved source ID: {savedLine(savedSource)}</p>
        <p>Saved window ID: {savedLine(savedWindow)}</p>
        {dirty ? (
          <p className="hint">The editor has unsaved edits; these saved IDs do not represent them until the observation is saved.</p>
        ) : null}
      </div>

      <TaskTabs
        label="Observation views"
        id="observation"
        tabs={VIEWS.map((key) => ({ key, label: VIEW_CAPTIONS[key] }))}
        selected={view}
        onSelect={setView}
      >
        {view === "source" ? (
          <>
            <h4 id="observation-source-type">Source type</h4>
            <div className="observation-kinds" role="group" aria-labelledby="observation-source-type">
              {KINDS.map((kind) => (
                <div key={kind} className="observation-kind">
                  <button
                    type="button"
                    disabled={blocked}
                    aria-pressed={source.source.kind === kind}
                    aria-describedby={`observation-kind-${kind}`}
                    onClick={() => updateSource(applyKind(source, kind, support.find((row) => row.kind === kind)?.schema ?? ""))}
                  >
                    {KIND_CAPTIONS[kind]}
                  </button>
                  <span className="hint" id={`observation-kind-${kind}`}>
                    {qualificationOf(kind)}
                  </span>
                </div>
              ))}
            </div>
            {adapterLine}
            <details className="observation-support-details">
              <summary>Adapter support</summary>
              <ul className="observation-support">
                {support.map((row) => (
                  <li key={`${row.kind}-${row.adapter}`}>
                    <code>{row.adapter}</code> · {row.schema} · {row.qualification}
                    {row.production_claim ? "" : " · not a production claim"}
                    {row.notes ? ` · ${row.notes}` : ""}
                  </li>
                ))}
              </ul>
            </details>

            <label htmlFor="observation-identity">Source ID</label>
            <input
              disabled={blocked}
              id="observation-identity"
              aria-describedby="observation-identity-help"
              value={source.source.identity}
              onChange={(e) => updateSource({ ...source, source: { ...source.source, identity: e.target.value } })}
            />
            <p className="hint" id="observation-identity-help">
              The name you give the system observed. It is not the saved source ID computed from the document.
            </p>
            <label htmlFor="observation-scope">Scope</label>
            <input
              disabled={blocked}
              id="observation-scope"
              aria-describedby="observation-scope-help"
              value={source.source.scope}
              onChange={(e) => updateSource({ ...source, source: { ...source.source, scope: e.target.value } })}
            />
            <p className="hint" id="observation-scope-help">
              The subset of the source this observation covers, as you declare it. An absence claim is only ever about this scope.
            </p>
            <label htmlFor="observation-freshness">Maximum data age</label>
            <input
              disabled={blocked}
              id="observation-freshness"
              aria-describedby="observation-freshness-help"
              value={source.freshness.max_age}
              onChange={(e) => updateSource({ ...source, freshness: { max_age: e.target.value } })}
            />
            <p className="hint" id="observation-freshness-help">
              A duration such as 1h or 30m. Data older than this is reported stale, never as current evidence.
            </p>
            <label>
              <input
                disabled={blocked}
                type="checkbox"
                aria-describedby="observation-enabled-help"
                checked={source.enabled}
                onChange={(e) => updateSource({ ...source, enabled: e.target.checked })}
              />{" "}
              Enable source
            </label>
            <p className="hint" id="observation-enabled-help">
              Saved with the configuration only: it does not connect or collect. Collection needs its own authorization.
            </p>

            {source.file ? (
              <fieldset className="observation-transport">
                <legend>File export</legend>
                <label htmlFor="observation-file-path">Export file</label>
                <input
                  disabled={blocked}
                  id="observation-file-path"
                  aria-describedby="observation-file-path-help"
                  value={source.file.path}
                  onChange={(e) => updateSource({ ...source, file: { ...source.file!, path: e.target.value } })}
                />
                <p className="hint" id="observation-file-path-help">
                  The export file read, relative to the source file&apos;s folder. Nothing is written to it.
                </p>
                <label htmlFor="observation-file-max">Maximum input bytes</label>
                <input
                  disabled={blocked}
                  id="observation-file-max"
                  type="number"
                  aria-describedby="observation-file-max-help"
                  value={source.file.max_bytes}
                  onChange={(e) => updateSource({ ...source, file: { ...source.file!, max_bytes: Number(e.target.value) } })}
                />
                <p className="hint" id="observation-file-max-help">
                  An export larger than this is recorded as truncated, never read as complete data.
                </p>
                <label htmlFor="observation-record-key">Record key</label>
                <input
                  disabled={blocked}
                  id="observation-record-key"
                  aria-describedby="observation-record-key-help"
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
                <p className="hint" id="observation-record-key-help">
                  Explicit locator text: the field that identifies one record, with each step of its path separated by a dot.
                </p>
              </fieldset>
            ) : null}

            {source.http ? (
              <fieldset className="observation-transport">
                <legend>HTTPS API</legend>
                <p className="hint">Approved source, HTTPS only: the URL must be an https address of an approved endpoint.</p>
                <label htmlFor="observation-http-url">HTTPS URL</label>
                <input
                  disabled={blocked}
                  id="observation-http-url"
                  value={source.http.url}
                  onChange={(e) => updateSource({ ...source, http: { ...source.http!, url: e.target.value } })}
                />
                <label htmlFor="observation-http-class">Environment classification</label>
                <input
                  disabled={blocked}
                  id="observation-http-class"
                  aria-describedby="observation-http-class-help"
                  value={source.http.classification}
                  onChange={(e) => updateSource({ ...source, http: { ...source.http!, classification: e.target.value } })}
                />
                <p className="hint" id="observation-http-class-help">
                  Declared by you. A nonproduction label is not an authorization by itself.
                </p>
                <label htmlFor="observation-http-server">TLS server name</label>
                <input
                  disabled={blocked}
                  id="observation-http-server"
                  aria-describedby="observation-http-server-help"
                  value={source.http.server_name}
                  onChange={(e) => updateSource({ ...source, http: { ...source.http!, server_name: e.target.value } })}
                />
                <p className="hint" id="observation-http-server-help">
                  The name the server certificate must match when a collection connects. Nothing has been verified yet.
                </p>
                <label htmlFor="observation-http-ca">CA certificate file</label>
                <input
                  disabled={blocked}
                  id="observation-http-ca"
                  aria-describedby="observation-http-ca-help"
                  value={source.http.ca_file}
                  onChange={(e) => updateSource({ ...source, http: { ...source.http!, ca_file: e.target.value } })}
                />
                <p className="hint" id="observation-http-ca-help">
                  A reference to the trusted CA certificate, never a private key. Certificate verification is never turned off.
                </p>
              </fieldset>
            ) : null}

            {source.capture ? (
              <fieldset className="observation-transport">
                <legend>Downstream capture</legend>
                <p className="hint">
                  Bind a retained case from capture UI (#250) or name an existing case path. This panel does
                  not start a listener.
                </p>
                <label htmlFor="observation-capture-path">Captured case</label>
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
                <label htmlFor="observation-capture-max">Maximum occurrences</label>
                <input
                  disabled={blocked}
                  id="observation-capture-max"
                  type="number"
                  aria-describedby="observation-capture-max-help"
                  value={source.capture.max_occurrences}
                  onChange={(e) =>
                    updateSource({ ...source, capture: { ...source.capture!, max_occurrences: Number(e.target.value) } })
                  }
                />
                <p className="hint" id="observation-capture-max-help">
                  How many captured occurrences are read: not file bytes and not completion samples.
                </p>
              </fieldset>
            ) : null}

            {source.database ? (
              <fieldset className="observation-transport">
                <legend>Database view</legend>
                <p className="hint">
                  Structured view, column mapping and parameter filters only. Live qualification evidence is
                  owned by #75; this UI must not claim production support.
                </p>
                <label htmlFor="observation-db-driver">Database engine</label>
                <select
                  disabled={blocked}
                  id="observation-db-driver"
                  value={source.database.driver}
                  onChange={(e) => updateSource({ ...source, database: { ...source.database!, driver: e.target.value } })}
                >
                  <option value="postgresql">PostgreSQL</option>
                  <option value="sqlserver">SQL Server</option>
                  <option value="oracle">Oracle</option>
                </select>
                <label htmlFor="observation-db-view">Approved view</label>
                <input
                  disabled={blocked}
                  id="observation-db-view"
                  aria-describedby="observation-db-view-help"
                  value={source.database.view.join(".")}
                  onChange={(e) =>
                    updateSource({
                      ...source,
                      database: { ...source.database!, view: e.target.value.split(".").filter(Boolean) },
                    })
                  }
                />
                <p className="hint" id="observation-db-view-help">
                  schema.table or table. Naming a view here grants no database privileges.
                </p>
                <label htmlFor="observation-db-key">Record key column</label>
                <input
                  disabled={blocked}
                  id="observation-db-key"
                  aria-describedby="observation-db-key-help"
                  value={source.database.record_key}
                  onChange={(e) => updateSource({ ...source, database: { ...source.database!, record_key: e.target.value } })}
                />
                <p className="hint" id="observation-db-key-help">A column of the view, not an SQL expression.</p>

                <div className="observation-filters" role="group" aria-label="Filters">
                  {source.database.filters.length === 0 ? (
                    <p className="hint">No filters: every row of the view is in scope.</p>
                  ) : (
                    <ul>
                      {source.database.filters.map((filter, index) => (
                        <li key={index}>
                          <span id={`observation-filter-${index}`}>
                            <code>{filter.column}</code> = <code>{filter.value}</code>
                          </span>{" "}
                          <button
                            type="button"
                            disabled={blocked}
                            aria-label={`Edit filter ${filter.column}`}
                            aria-describedby={`observation-filter-${index}`}
                            onClick={() => setFilterDraft({ column: filter.column, value: filter.value, editing: index })}
                          >
                            Edit filter
                          </button>{" "}
                          <button
                            type="button"
                            disabled={blocked}
                            aria-label={`Remove filter ${filter.column}`}
                            aria-describedby={`observation-filter-${index}`}
                            onClick={() => {
                              updateFilters(source.database!.filters.filter((_, at) => at !== index));
                              setFilterDraft({ column: "", value: "", editing: null });
                            }}
                          >
                            Remove filter
                          </button>
                        </li>
                      ))}
                    </ul>
                  )}
                  <div className="observation-filter-row">
                    <label htmlFor="observation-db-filter-col">Filter column</label>
                    <input
                      disabled={blocked}
                      id="observation-db-filter-col"
                      placeholder="status"
                      value={filterDraft.column}
                      onChange={(e) => setFilterDraft({ ...filterDraft, column: e.target.value })}
                    />
                    <label htmlFor="observation-db-filter-val">Filter value</label>
                    <input
                      disabled={blocked}
                      id="observation-db-filter-val"
                      placeholder="ready"
                      value={filterDraft.value}
                      onChange={(e) => setFilterDraft({ ...filterDraft, value: e.target.value })}
                    />
                    <button type="button" disabled={blocked || !filterDraft.column.trim()} onClick={commitFilter}>
                      {filterDraft.editing === null ? "Add filter" : "Update filter"}
                    </button>
                    {filterDraft.editing !== null ? (
                      <button type="button" disabled={blocked} onClick={() => setFilterDraft({ column: "", value: "", editing: null })}>
                        Cancel edit
                      </button>
                    ) : null}
                  </div>
                  <p className="hint">
                    Each filter compares one view column with one bound value. No SQL, expression or credential value is accepted.
                  </p>
                </div>

                <label htmlFor="observation-db-address">Database address</label>
                <input
                  disabled={blocked}
                  id="observation-db-address"
                  aria-describedby="observation-db-address-help"
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
                <p className="hint" id="observation-db-address-help">
                  Host and port of the database, not an HTTPS URL. The credential reference stays scoped to this address.
                </p>
              </fieldset>
            ) : null}
          </>
        ) : view === "rules" ? (
          <>
            <h4>Completion rules</h4>
            <p className="hint">
              Assertions inspect this window&apos;s boundary only. An absence claim is valid only when the
              retained completion is trustworthy and complete — unavailable collectors, timeouts, stale
              responses and caps are not evidence of no output.
            </p>
            <label htmlFor="observation-watermark">Watermark</label>
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
              <option value="none">None</option>
              <option value="collection-start">Collection start</option>
              <option value="declared-position">Declared position</option>
            </select>
            <details>
              <summary>Watermark details</summary>
              <p className="hint">
                <code>none</code>: the source offers no ordering, so no sample can be told apart as predating the
                window except by a recorded baseline. <code>collection-start</code>: the window&apos;s own opening
                instant is the watermark. <code>declared-position</code>: a position in the source&apos;s own
                ordering, recorded before the window opened. No boundary is inferred.
              </p>
            </details>
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
              aria-describedby="observation-preexisting-help"
              value={windowDoc.pre_existing_state.declaration}
              onChange={(e) =>
                updateWindow({
                  ...windowDoc,
                  pre_existing_state: { declaration: e.target.value, baseline_identity: "" },
                })
              }
            >
              <option value="" disabled>
                Not chosen
              </option>
              <option value="declared-empty">Declared empty</option>
              <option value="recorded-baseline">Recorded baseline</option>
              <option value="unknown">Unknown</option>
            </select>
            <p className="hint" id="observation-preexisting-help">
              {windowDoc.pre_existing_state.declaration === ""
                ? "Not chosen yet. Say what was in scope before collection: each choice makes a different absence claim."
                : windowDoc.pre_existing_state.declaration === "declared-empty"
                  ? "Declared empty is your claim that nothing was in scope before collection; readmit does not verify it."
                  : windowDoc.pre_existing_state.declaration === "recorded-baseline"
                    ? "Recorded baseline counts only a baseline observation recorded before the window opened."
                    : "Unknown: what already existed is not known, so no record can be attributed to this run."}
            </p>
            {windowDoc.pre_existing_state.declaration === "recorded-baseline" ? (
              <>
                <label htmlFor="observation-baseline">Baseline ID</label>
                <input
                  disabled={blocked}
                  id="observation-baseline"
                  aria-describedby="observation-baseline-help"
                  value={windowDoc.pre_existing_state.baseline_identity}
                  onChange={(e) =>
                    updateWindow({
                      ...windowDoc,
                      pre_existing_state: { ...windowDoc.pre_existing_state, baseline_identity: e.target.value },
                    })
                  }
                />
                <p className="hint" id="observation-baseline-help">
                  The full identity of the recorded baseline observation. A file name or the current sample is not a baseline.
                </p>
              </>
            ) : null}
            <label htmlFor="observation-deadline">Collection deadline</label>
            <input
              disabled={blocked}
              id="observation-deadline"
              aria-describedby="observation-deadline-help"
              value={windowDoc.completion.deadline}
              onChange={(e) => updateWindow({ ...windowDoc, completion: { ...windowDoc.completion, deadline: e.target.value } })}
            />
            <p className="hint" id="observation-deadline-help">
              A duration such as 30s bounding the whole window. Passing it without settling is an error, not completion; it is
              separate from the quiet period.
            </p>
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
            <p className="hint">
              The observed state must hold still for the quiet period across this many samples; both conditions apply.
            </p>
            <fieldset className="observation-limits">
              <legend>Collection limits</legend>
              <label htmlFor="observation-max-records">Maximum records</label>
              <input
                disabled={blocked}
                id="observation-max-records"
                type="number"
                value={windowDoc.completion.max_records}
                onChange={(e) =>
                  updateWindow({ ...windowDoc, completion: { ...windowDoc.completion, max_records: Number(e.target.value) } })
                }
              />
              <label htmlFor="observation-max-samples">Maximum samples</label>
              <input
                disabled={blocked}
                id="observation-max-samples"
                type="number"
                value={windowDoc.completion.max_samples}
                onChange={(e) =>
                  updateWindow({ ...windowDoc, completion: { ...windowDoc.completion, max_samples: Number(e.target.value) } })
                }
              />
              <p className="hint">Reaching a limit leaves the completion partial and incomplete, never proof of absence.</p>
            </fieldset>
          </>
        ) : (
          <>
            <h4>Collect</h4>
            <label htmlFor="observation-output">Completion file</label>
            <input id="observation-output" value={outputFile} onChange={(e) => setOutputFile(e.target.value)} />
            <label htmlFor="observation-snapshot">Snapshot folder</label>
            <input id="observation-snapshot" value={snapshotDir} onChange={(e) => setSnapshotDir(e.target.value)} />
            <p className="hint">
              Each collection writes a new completion file and a new snapshot folder; an existing one is never replaced.
            </p>
            {adapterLine}
            <label>
              <input
                type="checkbox"
                disabled={busy || reviewedPair === null}
                checked={authorized}
                onChange={(e) => setAuthorizedPair(e.target.checked ? reviewedPair : null)}
              />{" "}
              I authorize a read-only collection against the declared source
            </label>
            {reviewedPair ? (
              <p className="hint">
                This authorizes one collection of the saved source {reviewedPair.source.file} (ID{" "}
                <code>{reviewedPair.source.identity}</code>) with the saved window {reviewedPair.window.file} (ID{" "}
                <code>{reviewedPair.window.identity}</code>). A document changed on disk since is refused.
              </p>
            ) : null}
            <button
              type="button"
              disabled={busy || !authorized}
              aria-describedby={pairBlocked ? "observation-collect-blocked" : undefined}
              onClick={() => {
                const pair = authorizedPair;
                if (!authorized || !pair) return;
                const output = outputFile;
                // One authorization is one collection.
                setAuthorizedPair(null);
                void collectObservation({
                  workspace,
                  source_file: pair.source.file,
                  window_file: pair.window.file,
                  output_file: output,
                  snapshot_dir: snapshotDir,
                  authorize: true,
                  expected_source_identity: pair.source.identity,
                  expected_window_identity: pair.window.identity,
                }).then((res) => {
                  report(res.state, res.reason);
                  setScoped(
                    res.summary
                      ? { action: "collection", summary: res.summary, source: pair.source, window: pair.window, windowFile: pair.window.file, output }
                      : null,
                  );
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
            {pairBlocked ? (
              <p className="hint" id="observation-collect-blocked">
                {pairBlocked}
              </p>
            ) : null}
            <button
              type="button"
              disabled={busy || reading || saving}
              aria-describedby="observation-explain-help"
              onClick={() => {
                const output = outputFile;
                const file = windowFile;
                const window = savedWindow;
                void explainObservation({ workspace, completion_file: output, window_file: file }).then((res) => {
                  report(res.state, res.reason);
                  setScoped(
                    res.summary ? { action: "explanation", summary: res.summary, source: null, window, windowFile: file, output } : null,
                  );
                });
              }}
            >
              Explain completion
            </button>
            <p className="hint" id="observation-explain-help">
              Reads the retained completion file against the saved window; it collects nothing.
            </p>

            {scoped ? (
              <div className="observation-summary" role="status">
                <p>
                  {scopeCurrent ? "" : "Historical result, not evidence for the configuration shown now. "}
                  {scoped.action === "collection"
                    ? `Collection of saved source ${scoped.source?.file ?? ""} and saved window ${scoped.windowFile}, into ${scoped.output}.`
                    : `Explanation of ${scoped.output} against saved window ${scoped.windowFile}.`}
                </p>
                <p>
                  Status: <code>{scoped.summary.status}</code>
                  {scoped.summary.trustworthy ? " (trustworthy)" : " (not trustworthy)"}
                  {scoped.summary.stale ? " · stale" : ""}
                  {scoped.summary.partial ? " · partial/incomplete" : ""}
                </p>
                <p>
                  Records observed: {scoped.summary.records_observed}; mapped: {scoped.summary.mapped_correlations}; unmapped:{" "}
                  {scoped.summary.unmapped_correlations}
                </p>
                <p>
                  Absence claim:{" "}
                  {scoped.summary.supported ? "supported by this completion" : `not supported — ${scoped.summary.reason ?? "incomplete"}`}
                </p>
                <p className="hint">
                  Null, empty, missing and undecodable remain distinct in retained provenance. An unavailable
                  collector is never evidence of no output.
                </p>
              </div>
            ) : null}

            {onBindToTest ? (
              <>
                <button type="button" disabled={busy || !windowBindable} onClick={() => savedWindow && onBindToTest(savedWindow.file)}>
                  Use saved window in test
                </button>
                <p className="hint">
                  {windowBindable
                    ? `Binds the saved window ${savedWindow?.file ?? ""} into the test draft. No test is run.`
                    : "Save the window first: only a saved window without unsaved edits binds into a test. No test is run."}
                </p>
              </>
            ) : null}
          </>
        )}
      </TaskTabs>
    </section>
  );
}
