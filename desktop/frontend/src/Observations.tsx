// One named observation: where a test reads its downstream result and when
// that result is complete. The page shows the saved source and completion
// rule and the actual collections; Edit is one editor with one Save, and
// Collect is a reviewed, read-only collection. Nothing collects on opening.
import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  chooseEnvironmentFile,
  collectionProgress,
  inspectCompletion,
  listCredentials,
  listWholeCatalog,
  newIntentId,
  observationFields,
  observationHistory,
  observationSupport,
  openItemDraft,
  removeItem,
  saveItem,
  type CaptureObservationBinding,
  type CatalogItem,
  type CollectionProgress,
  type CollectionRow,
  type CompletionInspection,
  type CredentialRow,
  type ItemDraft,
  type ItemRef,
  type ObservationAdapterSupport,
  type ObservationDraft,
  type ObservationSource,
  type ObservationSourceDatabaseFilter,
  type ObservationSourceExtraction,
  type RequestContext,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { IconButton } from "./IconButton";
import { listDate } from "./Projects";
import { FilePicker, fileName, wholeNumber } from "./Environments";
import { ReviewSheet } from "./ReviewSheet";
import { useVocabulary } from "./vocabulary";

type SourceKind = "file-export" | "http-api" | "downstream-capture" | "database-query";

const SOURCE_KINDS: Record<SourceKind, string> = {
  "file-export": "File export",
  "http-api": "HTTPS API",
  "downstream-capture": "Downstream capture",
  "database-query": "Database view",
};

const WATERMARKS: Record<string, string> = {
  none: "None",
  "collection-start": "Collection start",
  "declared-position": "Declared position",
};

const INITIAL_STATES: Record<string, string> = {
  "declared-empty": "Declared empty",
  "recorded-baseline": "Recorded baseline",
  unknown: "Unknown",
};

/** The database adapters by the reader's code. Which of them this release
 * has, and how far each is qualified, is the facade's support answer. */
const DRIVERS: Record<string, string> = { postgresql: "PostgreSQL", sqlserver: "SQL Server", oracle: "Oracle" };

const CLASSIFICATIONS: Record<string, string> = { unclassified: "Not classified", nonproduction: "Nonproduction", production: "Production" };

const KEY_TYPES: Record<string, string> = { text: "Text", integer: "Whole number" };

const FORMATS: Record<string, string> = { csv: "CSV", json: "JSON" };

/** The reader refuses more filters than this. */
const MAX_FILTERS = 16;

/** How a collection's status reads. Only a completed one has a record count. */
const STATUSES: Record<string, string> = {
  complete: "Complete",
  incomplete: "Incomplete",
  missing: "Source missing",
  ambiguous: "Ambiguous",
  stale: "Stale",
  truncated: "Truncated",
  unsupported: "Unsupported",
  failed: "Failed",
  cancelled: "Stopped",
  timed_out: "Timed out",
  interrupted: "Interrupted",
};

function statusText(row: { status: string; records: number | null; reason?: string | undefined }): string {
  const status = STATUSES[row.status] ?? "Incomplete";
  if (row.records === null) return row.reason ? `${status} · ${row.reason}` : status;
  return `${row.records} ${row.records === 1 ? "record" : "records"} · ${status}`;
}

function collectedAt(at: string | null): string {
  return at ? `${listDate(at)} ${new Date(at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}` : "—";
}

function progressText(progress: CollectionProgress): string {
  const parts = [`Sample ${progress.samples}`, `${progress.stable_samples} of ${progress.required_stable} stable`];
  if (progress.deadline) parts.push(`until ${new Date(progress.deadline).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" })}`);
  return parts.join(" · ");
}

function formatText(extraction: ObservationSourceExtraction | null | undefined): string {
  return extraction ? (FORMATS[extraction.envelope] ?? "Unsupported") : "—";
}

/** The name of the project case a downstream capture reads. */
function caseName(path: string, cases: CatalogItem[]): string {
  return cases.find((entry) => (entry.summary.case?.entry ?? entry.name) === path)?.name ?? fileName(path);
}

function sourceRows(draft: ObservationDraft | undefined, cases: CatalogItem[]): { label: string; value: ReactNode }[] {
  if (!draft) return [];
  const source = draft.source;
  const kind = source.source.kind as SourceKind;
  const rows: { label: string; value: ReactNode }[] = [
    { label: "Type", value: SOURCE_KINDS[kind] ?? "Unsupported" },
    { label: "Scope", value: source.source.scope || "—" },
    { label: "Maximum age", value: source.freshness.max_age || "—" },
    { label: "Collection", value: source.enabled ? "Enabled" : "Off" },
  ];
  if (kind === "file-export" && source.file) {
    rows.push(
      { label: "Input file", value: fileName(source.file.path) || "—" },
      { label: "Format", value: formatText(source.extraction) },
      { label: "Record key", value: source.extraction?.record_key.join(".") || "—" },
    );
  } else if (kind === "http-api" && source.http) {
    rows.push(
      { label: "URL", value: source.http.url || "—" },
      { label: "Classification", value: CLASSIFICATIONS[source.http.classification] ?? "Not classified" },
      ...(draft.credential ? [{ label: "Credential", value: draft.credential }] : []),
      { label: "Format", value: formatText(source.extraction) },
      { label: "Record key", value: source.extraction?.record_key.join(".") || "—" },
    );
  } else if (kind === "downstream-capture" && source.capture) {
    rows.push({ label: "Case", value: source.capture.path ? caseName(source.capture.path, cases) : "—" }, { label: "Record key", value: source.capture.record_key || "—" });
  } else if (kind === "database-query" && source.database) {
    const database = source.database;
    rows.push(
      { label: "Database", value: DRIVERS[database.driver] ?? "Unsupported" },
      { label: "Address", value: database.address || "—" },
      { label: "View", value: database.view.join(".") || "—" },
      { label: "Record key", value: database.record_key || "—" },
      ...(draft.credential ? [{ label: "Credential", value: draft.credential }] : []),
      ...(database.filters.length > 0 ? [{ label: "Filters", value: database.filters.map((filter) => `${filter.column} equals ${filter.value}`).join(", ") }] : []),
    );
  }
  return rows;
}

export function useObservation({
  id,
  environment,
  context,
  busy,
  onRemoved,
}: {
  id: string | null;
  environment: CatalogItem | null;
  context: () => RequestContext;
  busy: boolean;
  onRemoved: () => void;
}) {
  const [item, setItem] = useState<CatalogItem | null>(null);
  const [draft, setDraft] = useState<ItemDraft | null>(null);
  const [history, setHistory] = useState<CollectionRow[] | null>(null);
  const [historyFailure, setHistoryFailure] = useState<string | null>(null);
  // The project's cases, which name a downstream capture's case.
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "edit" | "collect" | "remove">(null);
  const [inspected, setInspected] = useState<CompletionInspection | null>(null);
  const [inspectFailure, setInspectFailure] = useState<string | null>(null);
  const [collecting, setCollecting] = useState(false);
  const [progress, setProgress] = useState<CollectionProgress | null>(null);

  const read = useCallback(async () => {
    if (!id) return;
    const listed = await listWholeCatalog({ context: context(), kind: "observation", filter: {} });
    const found = listed.page?.items.find((candidate) => candidate.ref.id === id) ?? null;
    setItem(found);
    if (!found) {
      setFailure(listed.reason ?? "This observation is not in the project.");
      return;
    }
    const [opened, collected, caseList] = await Promise.all([
      openItemDraft({ context: context(), ref: found.ref }),
      observationHistory({ context: context(), ref: found.ref }),
      listWholeCatalog({ context: context(), kind: "case", filter: {} }),
    ]);
    setCases(caseList.page?.items ?? []);
    if (opened.draft) {
      setDraft(opened.draft);
      setFailure(null);
    } else setFailure(opened.reason ?? "This observation could not be read.");
    const readable = collected.state === "completed" || collected.state === "empty";
    setHistory(readable ? collected.collections : []);
    setHistoryFailure(readable ? null : (collected.reason ?? "The collections could not be read."));
  }, [context, id]);

  useEffect(() => {
    setItem(null);
    setDraft(null);
    setHistory(null);
    void read();
  }, [read]);

  // While Collect runs, what it has measured so far. The read never takes the
  // operation, so it answers beside the collection.
  useEffect(() => {
    if (!collecting) {
      setProgress(null);
      return;
    }
    let stopped = false;
    const poll = async () => {
      const answer = await collectionProgress();
      if (!stopped) setProgress(answer.state === "completed" && answer.progress ? answer.progress : null);
    };
    void poll();
    const timer = setInterval(() => void poll(), 500);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [collecting]);

  const inspect = (entry: string) => {
    if (!item) return;
    setInspectFailure(null);
    void inspectCompletion({ context: context(), ref: item.ref, entry }).then((answer) => {
      if (answer.completion) setInspected(answer.completion);
      else setInspectFailure(answer.reason ?? "This collection could not be read.");
    });
  };

  const observation = draft?.observation;
  const rule = observation?.window;
  const latest = history?.[0];
  const baselineRow = rule?.pre_existing_state.baseline_identity ? history?.find((row) => row.baseline === rule.pre_existing_state.baseline_identity) : undefined;
  const columns: Column<CollectionRow>[] = [
    { key: "closed", header: "Collected", priority: 1, minWidth: 10, render: (row) => collectedAt(row.closed_at) },
    { key: "result", header: "Result", priority: 1, minWidth: 14, flex: true, render: statusText },
    {
      key: "actions",
      header: "",
      priority: 1,
      minWidth: 3,
      render: (row) => (
        <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
          <Menu label={`More actions for the collection of ${collectedAt(row.closed_at)}`} items={[{ label: "Inspect completion", onSelect: () => inspect(row.entry) }]} />
        </span>
      ),
    },
  ];

  const body = (
    <div className="object-page">
      {failure ? (
        <p role="alert" className="object-problem">
          {failure}
        </p>
      ) : null}
      <section className="value-group" aria-labelledby="observation-source">
        <header className="value-group-header">
          <h2 id="observation-source">Source</h2>
        </header>
        <ValueRows rows={sourceRows(observation, cases)} />
      </section>
      <section className="value-group" aria-labelledby="observation-completion">
        <header className="value-group-header">
          <h2 id="observation-completion">Completion</h2>
        </header>
        {rule ? (
          <ValueRows
            rows={[
              { label: "Watermark", value: WATERMARKS[rule.watermark.kind] ?? "Unsupported" },
              ...(rule.watermark.kind === "declared-position" ? [{ label: "Position", value: rule.watermark.position }] : []),
              { label: "Initial state", value: INITIAL_STATES[rule.pre_existing_state.declaration] ?? "Unsupported" },
              ...(rule.pre_existing_state.declaration === "recorded-baseline"
                ? [{ label: "Baseline", value: baselineRow ? `Collection of ${collectedAt(baselineRow.closed_at)}` : "Recorded earlier" }]
                : []),
              { label: "Deadline", value: rule.completion.deadline },
              { label: "Quiet period", value: rule.completion.quiet_period },
              { label: "Stable samples", value: String(rule.completion.stable_samples) },
            ]}
          />
        ) : null}
      </section>
      <section className="value-group" aria-labelledby="observation-history">
        <header className="value-group-header">
          <h2 id="observation-history">Collections</h2>
        </header>
        {historyFailure ? (
          <p role="alert">{historyFailure}</p>
        ) : history && history.length === 0 ? (
          <EmptyState
            title="No collections"
            action={
              <button type="button" disabled={busy || !item} onClick={() => setSheet("collect")}>
                Collect
              </button>
            }
          />
        ) : (
          <>
            {latest ? <ValueRows rows={[{ label: "Latest result", value: statusText(latest) }]} /> : null}
            <DataTable
              label="Collections"
              rows={history ?? []}
              rowId={(row) => row.entry}
              rowLabel={(row) => `${collectedAt(row.closed_at)} · ${statusText(row)}`}
              columns={columns}
              selected={inspected?.entry ?? null}
              onSelect={inspect}
              onOpen={inspect}
              loading={history === null}
            />
          </>
        )}
        {inspectFailure ? <p role="alert">{inspectFailure}</p> : null}
      </section>

      {item && draft ? (
        <ObservationEditor
          open={sheet === "edit"}
          context={context}
          item={item}
          draft={draft}
          history={history ?? []}
          onClose={() => setSheet(null)}
          onSaved={async () => {
            setSheet(null);
            await read();
          }}
        />
      ) : null}
      {item ? (
        <ReviewSheet
          open={sheet === "collect"}
          title="Collect"
          action="observation.collect"
          finalLabel="Collect"
          context={context}
          items={[item.ref]}
          destination={environment?.ref}
          onClose={() => setSheet(null)}
          onDone={() => void read()}
          onRunning={setCollecting}
          whileRunning={progress ? <span role="status">{progressText(progress)}</span> : null}
          consequence={`Reads ${item.name} once; source records are not changed.`}
          render={(review) => (
            <ValueRows
              rows={[
                { label: "Source", value: item.name },
                ...(review.collect?.revision ? [{ label: "Version", value: review.collect.revision }] : []),
                { label: "Type", value: SOURCE_KINDS[(review.collect?.source_type ?? "") as SourceKind] ?? "—" },
                { label: "Scope", value: review.collect?.scope || "—" },
                ...(review.collect?.destination
                  ? [{ label: "Destination", value: review.collect.source_type === "file-export" || review.collect.source_type === "downstream-capture" ? fileName(review.collect.destination) : review.collect.destination }]
                  : []),
                { label: "Deadline", value: review.collect?.bounds.deadline ?? "—" },
                { label: "Quiet period", value: review.collect?.bounds.quiet_period ?? "—" },
                { label: "Stable samples", value: String(review.collect?.bounds.stable_samples ?? "—") },
              ]}
            />
          )}
          outcome={(result) => (result.collected ? <p role="status">{statusText(result.collected)}</p> : null)}
        />
      ) : null}
      <FormDialog
        open={sheet === "remove"}
        title="Remove observation"
        size="small"
        submitLabel="Remove"
        tone="danger"
        onClose={() => setSheet(null)}
        onSubmit={async () => {
          if (!item) return null;
          const answer = await removeItem({ context: context(), ref: item.ref });
          if (answer.state !== "completed") {
            return { reason: answer.referring.length > 0 ? `Used by ${answer.referring.map((entry) => entry.name).join(", ")}.` : (answer.reason ?? "Not removed.") };
          }
          setSheet(null);
          onRemoved();
          return null;
        }}
      >
        <p>{item?.name}</p>
        <p className="consequence">Removes it from this project; its collections stay readable.</p>
      </FormDialog>
      <Modal open={inspected !== null} title="Completion" onClose={() => setInspected(null)}>
        {inspected ? (
          <ValueRows
            rows={[
              { label: "Result", value: statusText(inspected) },
              { label: "Opened", value: inspected.opened_at ? new Date(inspected.opened_at).toLocaleString() : "—" },
              { label: "Closed", value: inspected.closed_at ? new Date(inspected.closed_at).toLocaleString() : "—" },
              { label: "Samples", value: String(inspected.samples) },
              { label: "Stable samples", value: String(inspected.stable_samples) },
              { label: "Quiet period", value: inspected.quiet_period },
              ...(inspected.supported ? [] : [{ label: "Current rule", value: inspected.unsupported ?? "No longer supported" }]),
            ]}
          />
        ) : null}
      </Modal>
    </div>
  );

  return {
    title: item?.name ?? "Observation",
    backLabel: environment?.name ?? "Environments",
    actions: item ? (
      <>
        <button type="button" disabled={busy || !draft} onClick={() => setSheet("edit")}>
          Edit
        </button>
        <button type="button" className="primary" disabled={busy || !draft} onClick={() => setSheet("collect")}>
          Collect
        </button>
        <Menu label="More observation actions" items={[{ label: "Remove…", tone: "danger" as const, onSelect: () => setSheet("remove") }]} />
      </>
    ) : null,
    body,
  };
}

// ---------- A new observation ----------

/** Add observation: the same editor, started from the facade's validated
 * defaults, or from a retained capture when one starts it. */
export function NewObservationEditor({
  open,
  context,
  capture,
  onClose,
  onSaved,
}: {
  open: boolean;
  context: () => RequestContext;
  capture?: CaptureObservationBinding | null | undefined;
  onClose: () => void;
  onSaved: (saved: ItemRef) => void | Promise<void>;
}) {
  const [draft, setDraft] = useState<ItemDraft | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    if (!open) {
      setDraft(null);
      setFailure(null);
      return;
    }
    let cancelled = false;
    void openItemDraft({ context: context(), ref: { kind: "observation", id: "" }, ...(capture ? { capture } : {}) }).then((answer) => {
      if (cancelled) return;
      if (answer.draft) setDraft({ ...answer.draft, name: "" });
      else setFailure(answer.reason ?? "The observation could not be started.");
    });
    return () => {
      cancelled = true;
    };
  }, [open, capture, context]);
  if (failure) {
    return (
      <Modal open={open} title="Add observation" onClose={onClose}>
        <p role="alert">{failure}</p>
      </Modal>
    );
  }
  return draft ? <ObservationEditor open={open} context={context} item={null} draft={draft} history={[]} onClose={onClose} onSaved={onSaved} /> : null;
}

// ---------- The editor ----------

type Filter = ObservationSourceDatabaseFilter;

/** Numeric fields hold what was typed until Save reads it. */
type Numbers = { maxBytes: string; maxOccurrences: string; stableSamples: string };

/** Where each refusal the facade names is corrected: the field's id. The
 * longest name that starts the refusal's field wins. */
const FIELDS: Record<string, string> = {
  name: "observation-edit-name",
  "observation.credential": "observation-credential",
  "observation.source": "observation-type",
  "observation.source.source": "observation-scope",
  "observation.source.freshness": "observation-max-age",
  "observation.source.extraction": "observation-format",
  "observation.source.extraction.record_key": "observation-record-key",
  "observation.source.file.path": "observation-input",
  "observation.source.file.max_bytes": "observation-max-bytes",
  "observation.source.http.url": "observation-url",
  "observation.source.http.classification": "observation-http-classification",
  "observation.source.http.server_name": "observation-server-name",
  "observation.source.http.ca_file": "observation-ca",
  "observation.source.http.credential": "observation-credential",
  "observation.source.http.credential.header": "observation-http-header",
  "observation.source.capture.path": "observation-case",
  "observation.source.capture.record_key": "observation-capture-key",
  "observation.source.capture.max_occurrences": "observation-max-occurrences",
  "observation.source.database.driver": "observation-driver",
  "observation.source.database.address": "observation-db-address",
  "observation.source.database.classification": "observation-db-classification",
  "observation.source.database.name": "observation-db-name",
  "observation.source.database.username": "observation-db-username",
  "observation.source.database.server_name": "observation-db-server-name",
  "observation.source.database.ca_file": "observation-db-ca",
  "observation.source.database.credential": "observation-credential",
  "observation.source.database.view": "observation-view",
  "observation.source.database.record_key": "observation-db-key",
  "observation.source.database.key_type": "observation-db-key-type",
  "observation.source.database.filters": "observation-filter-column",
  "observation.window": "observation-watermark",
  "observation.window.watermark": "observation-watermark",
  "observation.window.pre_existing_state": "observation-initial",
  "observation.window.completion.deadline": "observation-deadline",
  "observation.window.completion.quiet_period": "observation-quiet",
  "observation.window.completion.stable_samples": "observation-stable",
};

function fieldOf(field: string): string | undefined {
  const key = Object.keys(FIELDS)
    .filter((name) => field === name || field.startsWith(`${name}.`))
    .sort((a, b) => b.length - a.length)[0];
  return key ? FIELDS[key] : undefined;
}

/** The one observation editor: Source, then Completion, one Save. Only the
 * fields of the chosen source type appear. */
function ObservationEditor({
  open,
  context,
  item,
  draft,
  history,
  onClose,
  onSaved,
}: {
  open: boolean;
  context: () => RequestContext;
  /** The saved observation, or null for a new one. */
  item: CatalogItem | null;
  draft: ItemDraft;
  /** Its collections, which a recorded baseline is chosen from. */
  history: CollectionRow[];
  onClose: () => void;
  onSaved: (saved: ItemRef) => void | Promise<void>;
}) {
  const [step, setStep] = useState<"source" | "completion">("source");
  const [held, setHeld] = useState<ObservationDraft | null>(null);
  const starts = useVocabulary()?.observation_starts ?? [];
  const [name, setName] = useState("");
  const [numbers, setNumbers] = useState<Numbers>({ maxBytes: "", maxOccurrences: "", stableSamples: "" });
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [credentials, setCredentials] = useState<CredentialRow[]>([]);
  const [support, setSupport] = useState<ObservationAdapterSupport[]>([]);
  const [fields, setFields] = useState<{ fields: string[]; reason?: string } | null>(null);
  const [dirty, setDirty] = useState(false);
  const [chooseFailure, setChooseFailure] = useState<string | null>(null);
  const [filterDraft, setFilterDraft] = useState<Filter>({ column: "", value: "" });
  // How an export or API response is read, kept while another type is chosen
  // so choosing File export or HTTPS again restores it.
  const [extraction, setExtraction] = useState<ObservationSource["extraction"]>(null);

  // The fields the chosen export offers a record key: a deliberate local
  // read of that one file, never of a network source.
  const readFields = useCallback(
    (source: ObservationSource) => {
      if (source.source.kind !== "file-export" || !source.file?.path) {
        setFields(null);
        return;
      }
      void observationFields({ context: context(), source }).then((answer) =>
        setFields(
          answer.state === "completed"
            ? { fields: answer.fields }
            : { fields: [], reason: answer.reason ?? (answer.state === "empty" ? "The file holds no records." : "The file's fields could not be read.") },
        ),
      );
    },
    [context],
  );

  useEffect(() => {
    if (!open || !draft.observation) return;
    const start = structuredClone(draft.observation);
    setHeld(start);
    setExtraction(structuredClone(start.source.extraction));
    setName(draft.name ?? item?.name ?? "");
    setNumbers({
      maxBytes: String(start.source.file?.max_bytes ?? ""),
      maxOccurrences: String(start.source.capture?.max_occurrences ?? ""),
      stableSamples: String(start.window.completion.stable_samples),
    });
    setStep("source");
    setDirty(false);
    setChooseFailure(null);
    setFilterDraft({ column: "", value: "" });
    setFields(null);
    readFields(start.source);
    void listWholeCatalog({ context: context(), kind: "case", filter: {} }).then((answer) => setCases(answer.page?.items ?? []));
    void listCredentials({ context: context(), ref: { kind: "environment", id: "" } }).then((answer) => setCredentials(answer.credentials.filter((row) => row.purpose === "source-endpoint")));
    void observationSupport().then((answer) => setSupport(answer.support ?? []));
  }, [open, draft, item, context, readFields]);

  if (!held) return null;
  const source = held.source;
  const kind = source.source.kind as SourceKind;
  const change = (next: (value: ObservationDraft) => void) => {
    setHeld((current) => {
      if (!current) return current;
      const copy = structuredClone(current);
      next(copy);
      return copy;
    });
    setDirty(true);
  };
  const typed = (key: keyof Numbers, value: string) => {
    setNumbers((current) => ({ ...current, [key]: value }));
    setDirty(true);
  };
  // Where each type starts is the facade's: its reader's own default bounds,
  // with what has no default left empty.
  const startOf = (next: SourceKind) => starts.find((entry) => entry.source.kind === next);
  const setKind = (next: SourceKind) => {
    const start = startOf(next);
    change((value) => {
      value.source.source.kind = next;
      value.window.source.kind = next;
      if (start) value.source.schema = start.schema;
      value.source.file = next === "file-export" ? (value.source.file ?? structuredClone(start?.file ?? null)) : null;
      value.source.http = next === "http-api" ? (value.source.http ?? structuredClone(start?.http ?? null)) : null;
      value.source.capture = next === "downstream-capture" ? (value.source.capture ?? structuredClone(start?.capture ?? null)) : null;
      if (next === "database-query" && (value.source.database ?? start?.database)) value.source.database = value.source.database ?? structuredClone(start!.database!);
      else delete value.source.database;
      if (next === "downstream-capture" || next === "database-query") value.source.extraction = null;
      else value.source.extraction = value.source.extraction ?? structuredClone(extraction) ?? structuredClone(start?.extraction ?? null);
      // A named credential belongs to an HTTPS or database source only.
      if (next !== "http-api" && next !== "database-query") delete value.credential;
    });
    // The numeric fields show the values the chosen type now holds.
    setNumbers((current) => ({
      ...current,
      maxBytes: next === "file-export" ? String(source.file?.max_bytes ?? start?.file?.max_bytes ?? "") : current.maxBytes,
      maxOccurrences: next === "downstream-capture" ? String(source.capture?.max_occurrences ?? start?.capture?.max_occurrences ?? "") : current.maxOccurrences,
    }));
  };
  const setFormat = (envelope: string) =>
    change((value) => {
      const key = value.source.extraction?.record_key ?? [];
      const start = starts.find((entry) => entry.extraction?.envelope === envelope)?.extraction;
      if (!start) return;
      value.source.extraction = { ...structuredClone(start), record_key: key };
      setExtraction(structuredClone(value.source.extraction));
    });

  const chooseFile = async (set: (path: string) => void, fileKind: "observation-input" | "ca-certificate") => {
    setChooseFailure(null);
    const answer = await chooseEnvironmentFile(fileKind);
    if (answer.state === "completed" && answer.paths?.[0]) set(answer.paths[0]);
    else if (answer.state !== "cancelled") setChooseFailure(answer.reason ?? "The file was not chosen.");
  };

  const rule = held.window;
  const database = source.database;
  const databaseSupport = support.filter((row) => row.kind === "database-query");
  const driverSupport = database ? databaseSupport.find((row) => row.adapter === database.driver) : undefined;
  const recordKey = source.extraction?.record_key.join(".") ?? "";
  const baselines = history.filter((row) => row.baseline);

  const credentialPicker = (optional: boolean) => (
    <>
      <label htmlFor="observation-credential">Credential</label>
      <select
        id="observation-credential"
        value={held.credential ?? ""}
        onChange={(event) =>
          change((value) => {
            const chosen = event.target.value;
            if (chosen) value.credential = chosen;
            else delete value.credential;
            if (value.source.http) {
              value.source.http.credential = chosen
                ? { store: "os-keychain", address: "", command: "", arguments: [], ...value.source.http.credential, header: value.source.http.credential?.header ?? "" }
                : null;
            }
          })
        }
      >
        {optional ? <option value="">None</option> : !held.credential ? <option value="">Choose a credential</option> : null}
        {credentials.map((row) => (
          <option key={row.name} value={row.name}>
            {row.name}
          </option>
        ))}
        {held.credential && !credentials.some((row) => row.name === held.credential) ? <option value={held.credential}>{held.credential}</option> : null}
      </select>
    </>
  );

  const formatFields = (
    <>
      <label htmlFor="observation-format">Format</label>
      <select id="observation-format" value={source.extraction?.envelope ?? "csv"} onChange={(event) => setFormat(event.target.value)}>
        {Object.entries(FORMATS).map(([value, label]) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </select>
      {source.extraction?.envelope === "csv" && source.extraction.csv ? (
        <>
          <label className="check">
            <input
              type="checkbox"
              checked={source.extraction.csv.header === "present"}
              onChange={(event) => change((value) => { value.source.extraction!.csv!.header = event.target.checked ? "present" : "absent"; })}
            />
            First row names the fields
          </label>
          <label htmlFor="observation-csv-fields">Fields per record</label>
          <input
            id="observation-csv-fields"
            type="text"
            inputMode="numeric"
            value={String(source.extraction.csv.fields)}
            onChange={(event) => change((value) => { value.source.extraction!.csv!.fields = wholeNumber(event.target.value) ?? value.source.extraction!.csv!.fields; })}
          />
        </>
      ) : null}
      {source.extraction?.envelope === "json" && source.extraction.json ? (
        <>
          <label htmlFor="observation-json-path">Records at</label>
          <input
            id="observation-json-path"
            type="text"
            spellCheck={false}
            value={source.extraction.json.record_path.join(".")}
            onChange={(event) => change((value) => { value.source.extraction!.json!.record_path = event.target.value.split("."); })}
          />
        </>
      ) : null}
    </>
  );

  const sourceStep = (
    <>
      <label htmlFor="observation-edit-name">Name</label>
      <input id="observation-edit-name" type="text" autoFocus={item === null} value={name} onChange={(event) => { setName(event.target.value); setDirty(true); }} />
      <label htmlFor="observation-type">Type</label>
      <select id="observation-type" value={kind} onChange={(event) => setKind(event.target.value as SourceKind)}>
        {(Object.keys(SOURCE_KINDS) as SourceKind[]).map((value) => (
          <option key={value} value={value}>
            {SOURCE_KINDS[value]}
          </option>
        ))}
      </select>
      <label htmlFor="observation-scope">Scope</label>
      <input id="observation-scope" type="text" value={source.source.scope} onChange={(event) => change((value) => { value.source.source.scope = event.target.value; value.window.source.scope = event.target.value; })} />
      <label htmlFor="observation-max-age">Maximum age</label>
      <input id="observation-max-age" type="text" value={source.freshness.max_age} onChange={(event) => change((value) => { value.source.freshness.max_age = event.target.value; })} />
      <label className="check">
        <input type="checkbox" checked={source.enabled} onChange={(event) => change((value) => { value.source.enabled = event.target.checked; })} />
        Enable collection
      </label>

      {kind === "file-export" && source.file ? (
        <>
          <FilePicker
            id="observation-input"
            label="Input file"
            path={source.file.path}
            onChoose={() =>
              void chooseFile((path) => {
                const next = structuredClone(held);
                next.source.file!.path = path;
                change((value) => { value.source.file!.path = path; });
                readFields(next.source);
              }, "observation-input")
            }
          />
          {formatFields}
          <label htmlFor="observation-record-key">Record key field</label>
          <select
            id="observation-record-key"
            disabled={!fields || fields.fields.length === 0}
            value={recordKey}
            onChange={(event) => change((value) => { if (value.source.extraction) value.source.extraction.record_key = event.target.value ? event.target.value.split(".") : []; })}
          >
            {recordKey === "" ? <option value="">Choose a field</option> : null}
            {(fields?.fields ?? []).map((field) => (
              <option key={field} value={field}>
                {field}
              </option>
            ))}
            {recordKey && !(fields?.fields ?? []).includes(recordKey) ? <option value={recordKey}>{recordKey}</option> : null}
          </select>
          {fields?.reason ? <p className="field-error">{fields.reason}</p> : null}
          <label htmlFor="observation-max-bytes">Maximum size (bytes)</label>
          <input id="observation-max-bytes" type="text" inputMode="numeric" value={numbers.maxBytes} onChange={(event) => typed("maxBytes", event.target.value)} />
        </>
      ) : null}

      {kind === "http-api" && source.http ? (
        <>
          <label htmlFor="observation-url">URL</label>
          <input id="observation-url" type="url" spellCheck={false} value={source.http.url} onChange={(event) => change((value) => { value.source.http!.url = event.target.value; })} />
          <label htmlFor="observation-http-classification">Classification</label>
          <select id="observation-http-classification" value={source.http.classification} onChange={(event) => change((value) => { value.source.http!.classification = event.target.value; })}>
            {Object.entries(CLASSIFICATIONS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
          <label htmlFor="observation-server-name">Server name</label>
          <input id="observation-server-name" type="text" spellCheck={false} value={source.http.server_name} onChange={(event) => change((value) => { value.source.http!.server_name = event.target.value; })} />
          <FilePicker
            id="observation-ca"
            label="CA certificate"
            path={source.http.ca_file}
            onChoose={() => void chooseFile((path) => change((value) => { value.source.http!.ca_file = path; }), "ca-certificate")}
            onClear={() => change((value) => { value.source.http!.ca_file = ""; })}
          />
          {credentialPicker(true)}
          {held.credential && source.http.credential ? (
            <>
              <label htmlFor="observation-http-header">Header</label>
              <input id="observation-http-header" type="text" spellCheck={false} value={source.http.credential.header} onChange={(event) => change((value) => { value.source.http!.credential!.header = event.target.value; })} />
            </>
          ) : null}
          {formatFields}
          <label htmlFor="observation-record-key">Record key path</label>
          <input id="observation-record-key" type="text" spellCheck={false} value={recordKey} onChange={(event) => change((value) => { if (value.source.extraction) value.source.extraction.record_key = event.target.value.split("."); })} />
        </>
      ) : null}

      {kind === "downstream-capture" && source.capture ? (
        <>
          <label htmlFor="observation-case">Case</label>
          <select id="observation-case" value={source.capture.path} onChange={(event) => change((value) => { value.source.capture!.path = event.target.value; })}>
            {source.capture.path === "" ? <option value="">Choose a case</option> : null}
            {cases.map((entry) => (
              <option key={entry.ref.id} value={entry.summary.case?.entry ?? entry.name}>
                {entry.name}
              </option>
            ))}
            {source.capture.path && !cases.some((entry) => (entry.summary.case?.entry ?? entry.name) === source.capture!.path) ? (
              <option value={source.capture.path}>{fileName(source.capture.path)}</option>
            ) : null}
          </select>
          <label htmlFor="observation-capture-key">Record key HL7 field</label>
          <input id="observation-capture-key" type="text" spellCheck={false} value={source.capture.record_key} onChange={(event) => change((value) => { value.source.capture!.record_key = event.target.value; })} />
          <label htmlFor="observation-max-occurrences">Maximum occurrences</label>
          <input id="observation-max-occurrences" type="text" inputMode="numeric" value={numbers.maxOccurrences} onChange={(event) => typed("maxOccurrences", event.target.value)} />
        </>
      ) : null}

      {kind === "database-query" && database ? (
        <>
          <label htmlFor="observation-driver">Database</label>
          <select id="observation-driver" value={database.driver} onChange={(event) => change((value) => { value.source.database!.driver = event.target.value; })}>
            {databaseSupport.map((row) => (
              <option key={row.adapter} value={row.adapter}>
                {DRIVERS[row.adapter] ?? row.adapter}
              </option>
            ))}
            {!driverSupport ? <option value={database.driver}>{DRIVERS[database.driver] ?? database.driver}</option> : null}
          </select>
          {!driverSupport && support.length > 0 ? (
            <p className="field-error" role="alert">
              {DRIVERS[database.driver] ?? database.driver} is not available in this release.
            </p>
          ) : null}
          <label htmlFor="observation-db-address">Address</label>
          <input id="observation-db-address" type="text" spellCheck={false} value={database.address} onChange={(event) => change((value) => { value.source.database!.address = event.target.value; })} />
          <label htmlFor="observation-db-name">Database name</label>
          <input id="observation-db-name" type="text" spellCheck={false} value={database.name} onChange={(event) => change((value) => { value.source.database!.name = event.target.value; })} />
          <label htmlFor="observation-db-username">Username</label>
          <input id="observation-db-username" type="text" spellCheck={false} value={database.username} onChange={(event) => change((value) => { value.source.database!.username = event.target.value; })} />
          <label htmlFor="observation-db-classification">Classification</label>
          <select id="observation-db-classification" value={database.classification} onChange={(event) => change((value) => { value.source.database!.classification = event.target.value; })}>
            {Object.entries(CLASSIFICATIONS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
          <label htmlFor="observation-db-server-name">Server name</label>
          <input id="observation-db-server-name" type="text" spellCheck={false} value={database.server_name} onChange={(event) => change((value) => { value.source.database!.server_name = event.target.value; })} />
          <FilePicker
            id="observation-db-ca"
            label="CA certificate"
            path={database.ca_file}
            onChoose={() => void chooseFile((path) => change((value) => { value.source.database!.ca_file = path; }), "ca-certificate")}
            onClear={() => change((value) => { value.source.database!.ca_file = ""; })}
          />
          {credentialPicker(false)}
          <label htmlFor="observation-view">View</label>
          <input id="observation-view" type="text" spellCheck={false} value={database.view.join(".")} onChange={(event) => change((value) => { value.source.database!.view = event.target.value.split("."); })} />
          <div className="field-pair">
            <div>
              <label htmlFor="observation-db-key">Record key column</label>
              <input id="observation-db-key" type="text" spellCheck={false} value={database.record_key} onChange={(event) => change((value) => { value.source.database!.record_key = event.target.value; })} />
            </div>
            <div>
              <label htmlFor="observation-db-key-type">Key type</label>
              <select id="observation-db-key-type" value={database.key_type} onChange={(event) => change((value) => { value.source.database!.key_type = event.target.value; })}>
                {Object.entries(KEY_TYPES).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
                {!KEY_TYPES[database.key_type] ? <option value={database.key_type}>{database.key_type}</option> : null}
              </select>
            </div>
          </div>
          <table className="plain-table edit-table" aria-label="Filters">
            <thead>
              <tr>
                <th scope="col">Column</th>
                <th scope="col">Operator</th>
                <th scope="col">Value</th>
                <th scope="col">
                  <span className="visually-hidden">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {database.filters.map((filter, index) => (
                <tr key={index}>
                  <td>{filter.column}</td>
                  <td>equals</td>
                  <td>{filter.value}</td>
                  <td>
                    <IconButton icon="close" label={`Remove filter ${index + 1}`} onClick={() => change((value) => { value.source.database!.filters.splice(index, 1); })} />
                  </td>
                </tr>
              ))}
              <tr>
                <td>
                  <input id="observation-filter-column" type="text" aria-label="Filter column" spellCheck={false} value={filterDraft.column} onChange={(event) => setFilterDraft({ ...filterDraft, column: event.target.value })} />
                </td>
                <td>
                  <select aria-label="Filter operator" value="equals" disabled>
                    <option value="equals">equals</option>
                  </select>
                </td>
                <td>
                  <input type="text" aria-label="Filter value" value={filterDraft.value} onChange={(event) => setFilterDraft({ ...filterDraft, value: event.target.value })} />
                </td>
                <td>
                  <button
                    type="button"
                    disabled={filterDraft.column.trim() === "" || filterDraft.value === "" || database.filters.length >= MAX_FILTERS}
                    onClick={() => {
                      change((value) => { value.source.database!.filters.push({ column: filterDraft.column.trim(), value: filterDraft.value }); });
                      setFilterDraft({ column: "", value: "" });
                    }}
                  >
                    Add filter
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </>
      ) : null}
      {chooseFailure ? <p className="field-error" role="alert">{chooseFailure}</p> : null}
    </>
  );

  const completionStep = (
    <>
      <label htmlFor="observation-watermark">Watermark</label>
      <select id="observation-watermark" value={rule.watermark.kind} onChange={(event) => change((value) => { value.window.watermark.kind = event.target.value; if (event.target.value !== "declared-position") value.window.watermark.position = ""; })}>
        {Object.entries(WATERMARKS).map(([value, label]) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </select>
      {rule.watermark.kind === "declared-position" ? (
        <>
          <label htmlFor="observation-position">Position</label>
          <input id="observation-position" type="text" value={rule.watermark.position} onChange={(event) => change((value) => { value.window.watermark.position = event.target.value; })} />
        </>
      ) : null}
      <label htmlFor="observation-initial">Initial state</label>
      <select id="observation-initial" value={rule.pre_existing_state.declaration} onChange={(event) => change((value) => { value.window.pre_existing_state.declaration = event.target.value; if (event.target.value !== "recorded-baseline") value.window.pre_existing_state.baseline_identity = ""; })}>
        {Object.entries(INITIAL_STATES).map(([value, label]) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </select>
      {rule.pre_existing_state.declaration === "recorded-baseline" ? (
        <>
          <label htmlFor="observation-baseline">Baseline</label>
          <select
            id="observation-baseline"
            disabled={baselines.length === 0 && !rule.pre_existing_state.baseline_identity}
            value={rule.pre_existing_state.baseline_identity}
            onChange={(event) => change((value) => { value.window.pre_existing_state.baseline_identity = event.target.value; })}
          >
            {rule.pre_existing_state.baseline_identity === "" ? <option value="">{baselines.length > 0 ? "Choose a collection" : "No completed collection"}</option> : null}
            {baselines.map((row) => (
              <option key={row.entry} value={row.baseline}>
                {`${collectedAt(row.closed_at)} · ${row.records ?? 0} ${row.records === 1 ? "record" : "records"}`}
              </option>
            ))}
            {rule.pre_existing_state.baseline_identity && !baselines.some((row) => row.baseline === rule.pre_existing_state.baseline_identity) ? (
              <option value={rule.pre_existing_state.baseline_identity}>Recorded earlier</option>
            ) : null}
          </select>
        </>
      ) : null}
      <label htmlFor="observation-deadline">Deadline</label>
      <input id="observation-deadline" type="text" value={rule.completion.deadline} onChange={(event) => change((value) => { value.window.completion.deadline = event.target.value; })} />
      <label htmlFor="observation-quiet">Quiet period</label>
      <input id="observation-quiet" type="text" value={rule.completion.quiet_period} onChange={(event) => change((value) => { value.window.completion.quiet_period = event.target.value; })} />
      <label htmlFor="observation-stable">Stable samples</label>
      <input id="observation-stable" type="text" inputMode="numeric" value={numbers.stableSamples} onChange={(event) => typed("stableSamples", event.target.value)} />
    </>
  );

  /** A refusal at a field on the step that holds it. */
  const refuse = (reason: string, field: string | undefined, where: "source" | "completion"): SubmitFailure => {
    setStep(where);
    if (field) setTimeout(() => document.getElementById(field)?.focus());
    return { reason, ...(field ? { field } : {}) };
  };

  return (
    <FormDialog
      open={open}
      title={item ? "Edit observation" : "Add observation"}
      size="wide"
      submitLabel="Save"
      dirty={dirty}
      onClose={onClose}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        if (filterDraft.column.trim() !== "" || filterDraft.value !== "") return refuse("Add this filter or clear it.", "observation-filter-column", "source");
        const next = structuredClone(held);
        // Dotted paths keep what was typed while editing; the saved path has
        // no empty parts.
        if (next.source.extraction) {
          next.source.extraction.record_key = next.source.extraction.record_key.filter(Boolean);
          if (next.source.extraction.json) next.source.extraction.json.record_path = next.source.extraction.json.record_path.filter(Boolean);
        }
        if (next.source.database) next.source.database.view = next.source.database.view.filter(Boolean);
        if (next.source.file) {
          const bytes = wholeNumber(numbers.maxBytes);
          if (bytes === null) return refuse("Enter a whole number of bytes.", "observation-max-bytes", "source");
          next.source.file.max_bytes = bytes;
        }
        if (next.source.capture) {
          const occurrences = wholeNumber(numbers.maxOccurrences);
          if (occurrences === null) return refuse("Enter a whole number.", "observation-max-occurrences", "source");
          next.source.capture.max_occurrences = occurrences;
        }
        const stable = wholeNumber(numbers.stableSamples);
        if (stable === null) return refuse("Enter a whole number.", "observation-stable", "completion");
        next.window.completion.stable_samples = stable;
        const answer = await saveItem({
          context: context(),
          kind: "observation",
          ...(item ? { item: item.ref.id } : {}),
          ...(item?.ref.revision ? { base_revision: item.ref.revision } : {}),
          draft: { ...draft, name: name.trim(), observation: next },
          intent_id: newIntentId(),
        });
        if (answer.outcome !== "saved" || !answer.saved) {
          if (answer.outcome === "conflict") return { reason: "This changed since you opened it. Close and open it again." };
          const problem = answer.problems.find((entry) => fieldOf(entry.field)) ?? answer.problems[0];
          const reason = answer.problems.map((entry) => entry.problem).join(" ") || answer.reason || "Not saved.";
          return refuse(reason, problem ? fieldOf(problem.field) : undefined, problem?.field.startsWith("observation.window") ? "completion" : "source");
        }
        setDirty(false);
        await onSaved(answer.saved);
        return null;
      }}
    >
      <div className="task-tabs" role="tablist" aria-label="Observation steps">
        <button type="button" role="tab" aria-selected={step === "source"} onClick={() => setStep("source")}>
          Source
        </button>
        <button type="button" role="tab" aria-selected={step === "completion"} onClick={() => setStep("completion")}>
          Completion
        </button>
      </div>
      {step === "source" ? sourceStep : completionStep}
    </FormDialog>
  );
}
