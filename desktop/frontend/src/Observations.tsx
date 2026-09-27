// One named observation: where a test reads its downstream result and when
// that result is complete. The page shows the saved source and completion
// rule and the actual collections; Edit is one editor with one Save, and
// Collect is a reviewed, read-only collection. Nothing collects on opening.
import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  chooseEnvironmentFile,
  inspectCompletion,
  listWholeCatalog,
  newIntentId,
  observationHistory,
  openItemDraft,
  removeItem,
  saveItem,
  type CatalogItem,
  type CollectionRow,
  type CompletionInspection,
  type ItemDraft,
  type ObservationDraft,
  type ObservationSource,
  type ObservationSourceDatabaseFilter,
  type RequestContext,
  type SecretStore,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { IconButton } from "./IconButton";
import { listDate } from "./Projects";
import { FilePicker, fileName, saveProblem } from "./Environments";
import { ReviewSheet } from "./ReviewSheet";

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

const DRIVERS: Record<string, string> = { postgres: "PostgreSQL", sqlserver: "SQL Server", oracle: "Oracle" };

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

function sourceRows(source: ObservationSource | undefined): { label: string; value: ReactNode }[] {
  if (!source) return [];
  const kind = source.source.kind as SourceKind;
  const rows: { label: string; value: ReactNode }[] = [
    { label: "Type", value: SOURCE_KINDS[kind] ?? "Unsupported" },
    { label: "Scope", value: source.source.scope || "—" },
    { label: "Maximum age", value: source.freshness.max_age || "—" },
    { label: "Collection", value: source.enabled ? "Enabled" : "Off" },
  ];
  if (kind === "file-export" && source.file) {
    rows.push({ label: "Input file", value: fileName(source.file.path) }, { label: "Record key", value: source.extraction?.record_key.join(".") || "—" });
  } else if (kind === "http-api" && source.http) {
    rows.push({ label: "URL", value: source.http.url }, { label: "Record key", value: source.extraction?.record_key.join(".") || "—" });
  } else if (kind === "downstream-capture" && source.capture) {
    rows.push({ label: "Case", value: fileName(source.capture.path) }, { label: "Record key", value: source.capture.record_key });
  } else if (kind === "database-query" && source.database) {
    rows.push(
      { label: "Database", value: DRIVERS[source.database.driver] ?? "Unsupported" },
      { label: "View", value: source.database.view.join(".") },
      { label: "Record key", value: source.database.record_key },
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
  const [failure, setFailure] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "edit" | "collect" | "remove">(null);
  const [inspected, setInspected] = useState<CompletionInspection | null>(null);
  const [inspectFailure, setInspectFailure] = useState<string | null>(null);

  const read = useCallback(async () => {
    if (!id) return;
    const listed = await listWholeCatalog({ context: context(), kind: "observation", filter: {} });
    const found = listed.page?.items.find((candidate) => candidate.ref.id === id) ?? null;
    setItem(found);
    if (!found) {
      setFailure(listed.reason ?? "This observation is not in the project.");
      return;
    }
    const [opened, collected] = await Promise.all([openItemDraft({ context: context(), ref: found.ref }), observationHistory({ context: context(), ref: found.ref })]);
    if (opened.draft) {
      setDraft(opened.draft);
      setFailure(null);
    } else setFailure(opened.reason ?? "This observation could not be read.");
    setHistory(collected.state === "completed" || collected.state === "empty" ? collected.collections : []);
  }, [context, id]);

  useEffect(() => {
    setItem(null);
    setDraft(null);
    setHistory(null);
    void read();
  }, [read]);

  const observation = draft?.observation;
  const rule = observation?.window;
  const columns: Column<CollectionRow>[] = [
    { key: "closed", header: "Collected", priority: 1, minWidth: 10, render: (row) => (row.closed_at ? `${listDate(row.closed_at)} ${new Date(row.closed_at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}` : "—") },
    { key: "result", header: "Result", priority: 1, minWidth: 14, render: statusText },
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
        <ValueRows rows={sourceRows(observation?.source)} />
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
        {history && history.length === 0 ? (
          <EmptyState
            title="No completed observation"
            action={
              <button type="button" disabled={busy || !item} onClick={() => setSheet("collect")}>
                Collect
              </button>
            }
          />
        ) : (
          <DataTable
            label="Collections"
            rows={history ?? []}
            rowId={(row) => row.entry}
            rowLabel={(row) => statusText(row)}
            columns={columns}
            selected={inspected?.entry ?? null}
            onSelect={(entry) => {
              if (!item) return;
              setInspectFailure(null);
              void inspectCompletion({ context: context(), ref: item.ref, entry }).then((answer) => {
                if (answer.completion) setInspected(answer.completion);
                else setInspectFailure(answer.reason ?? "This collection could not be read.");
              });
            }}
            onOpen={() => undefined}
            loading={history === null}
          />
        )}
        {inspectFailure ? <p role="alert">{inspectFailure}</p> : null}
      </section>

      {item && draft ? (
        <ObservationEditor
          open={sheet === "edit"}
          context={context}
          item={item}
          draft={draft}
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
          consequence={`Reads ${item.name} once; source records are not changed.`}
          render={(review) => (
            <ValueRows
              rows={[
                { label: "Source", value: review.collect?.source ?? item.name },
                { label: "Type", value: SOURCE_KINDS[(review.collect?.source_type ?? "") as SourceKind] ?? "—" },
                { label: "Scope", value: review.collect?.scope || "—" },
                ...(review.collect?.destination ? [{ label: "Destination", value: review.collect.destination }] : []),
                { label: "Deadline", value: review.collect?.bounds.deadline ?? "—" },
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
      <Modal open={inspected !== null} title="Collection" onClose={() => setInspected(null)}>
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

// ---------- The editor ----------

type Filter = ObservationSourceDatabaseFilter;

/** The one observation editor: Source, then Completion, one Save. Only the
 * fields of the chosen source type appear. */
function ObservationEditor({
  open,
  context,
  item,
  draft,
  onClose,
  onSaved,
}: {
  open: boolean;
  context: () => RequestContext;
  item: CatalogItem;
  draft: ItemDraft;
  onClose: () => void;
  onSaved: () => void | Promise<void>;
}) {
  const [step, setStep] = useState<"source" | "completion">("source");
  const [held, setHeld] = useState<ObservationDraft | null>(null);
  const [name, setName] = useState("");
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [dirty, setDirty] = useState(false);
  const [chooseFailure, setChooseFailure] = useState<string | null>(null);
  const [filterDraft, setFilterDraft] = useState<Filter>({ column: "", value: "" });
  // How an export or API response is read, kept while another type is chosen
  // so choosing File export or HTTPS again restores it.
  const [extraction, setExtraction] = useState<ObservationSource["extraction"]>(null);

  useEffect(() => {
    if (!open || !draft.observation) return;
    setHeld(structuredClone(draft.observation));
    setExtraction(structuredClone(draft.observation.source.extraction));
    setName(draft.name ?? item.name);
    setStep("source");
    setDirty(false);
    setChooseFailure(null);
    setFilterDraft({ column: "", value: "" });
    void listWholeCatalog({ context: context(), kind: "case", filter: {} }).then((answer) => setCases(answer.page?.items ?? []));
  }, [open, draft, item, context]);

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
  const setKind = (next: SourceKind) =>
    change((value) => {
      value.source.source.kind = next;
      value.window.source.kind = next;
      value.source.file = next === "file-export" ? (value.source.file ?? { path: "", max_bytes: 65536 }) : null;
      value.source.http =
        next === "http-api"
          ? (value.source.http ?? { url: "", classification: "unclassified", ca_file: "", server_name: "", timeout: "10s", max_bytes: 1048576, retry: { attempts: 1, delay: "1s" }, credential: null })
          : null;
      value.source.capture = next === "downstream-capture" ? (value.source.capture ?? { path: "", kinds: ["message"], record_key: "", max_occurrences: 1000 }) : null;
      if (next === "database-query") {
        value.source.database = value.source.database ?? {
          driver: "postgres",
          address: "",
          classification: "unclassified",
          name: "",
          username: "",
          ca_file: "",
          server_name: "",
          credential: { store: "os-keychain", address: "", purpose: "source-endpoint", command: "", arguments: [] },
          view: [],
          record_key: "",
          key_type: "text",
          filters: [],
          limits: null,
        };
      } else delete value.source.database;
      if (next === "downstream-capture" || next === "database-query") value.source.extraction = null;
      else value.source.extraction = value.source.extraction ?? structuredClone(extraction);
    });

  const chooseFile = async (set: (path: string) => void, kind: "observation-input" | "ca-certificate" | "locator-program") => {
    setChooseFailure(null);
    const answer = await chooseEnvironmentFile(kind);
    if (answer.state === "completed" && answer.paths?.[0]) set(answer.paths[0]);
    else if (answer.state !== "cancelled") setChooseFailure(answer.reason ?? "The file was not chosen.");
  };

  const rule = held.window;
  const fields: Record<string, string> = { name: "observation-edit-name" };

  const sourceStep = (
    <>
      <label htmlFor="observation-edit-name">Name</label>
      <input id="observation-edit-name" type="text" value={name} onChange={(event) => { setName(event.target.value); setDirty(true); }} />
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
          <FilePicker label="Input file" path={source.file.path} onChoose={() => void chooseFile((path) => change((value) => { value.source.file!.path = path; }), "observation-input")} />
          <label htmlFor="observation-record-key">Record key field</label>
          <input id="observation-record-key" type="text" value={source.extraction?.record_key.join(".") ?? ""} onChange={(event) => change((value) => { if (value.source.extraction) value.source.extraction.record_key = event.target.value.split(".").filter(Boolean); })} />
          <label htmlFor="observation-max-bytes">Maximum size (bytes)</label>
          <input id="observation-max-bytes" type="text" inputMode="numeric" value={String(source.file.max_bytes)} onChange={(event) => change((value) => { value.source.file!.max_bytes = Number(event.target.value) || 0; })} />
        </>
      ) : null}

      {kind === "http-api" && source.http ? (
        <>
          <label htmlFor="observation-url">URL</label>
          <input id="observation-url" type="url" spellCheck={false} value={source.http.url} onChange={(event) => change((value) => { value.source.http!.url = event.target.value; })} />
          <label htmlFor="observation-http-classification">Classification</label>
          <select id="observation-http-classification" value={source.http.classification} onChange={(event) => change((value) => { value.source.http!.classification = event.target.value; })}>
            <option value="unclassified">Not classified</option>
            <option value="nonproduction">Nonproduction</option>
            <option value="production">Production</option>
          </select>
          <label htmlFor="observation-server-name">Server name</label>
          <input id="observation-server-name" type="text" spellCheck={false} value={source.http.server_name} onChange={(event) => change((value) => { value.source.http!.server_name = event.target.value; })} />
          <FilePicker label="CA certificate" path={source.http.ca_file} onChoose={() => void chooseFile((path) => change((value) => { value.source.http!.ca_file = path; }), "ca-certificate")} onClear={() => change((value) => { value.source.http!.ca_file = ""; })} />
          <label htmlFor="observation-http-store">Credential</label>
          <select
            id="observation-http-store"
            value={source.http.credential?.store ?? ""}
            onChange={(event) =>
              change((value) => {
                const store = event.target.value as SecretStore | "";
                value.source.http!.credential = store ? { store, address: value.source.http!.credential?.address ?? "", header: value.source.http!.credential?.header ?? "", command: value.source.http!.credential?.command ?? "", arguments: value.source.http!.credential?.arguments ?? [] } : null;
              })
            }
          >
            <option value="">None</option>
            <option value="os-keychain">OS keychain</option>
            <option value="customer-managed">Customer-managed vault</option>
          </select>
          {source.http.credential ? (
            <>
              <label htmlFor="observation-http-header">Header</label>
              <input id="observation-http-header" type="text" spellCheck={false} value={source.http.credential.header} onChange={(event) => change((value) => { value.source.http!.credential!.header = event.target.value; })} />
              <FilePicker label="Credential locator program" path={source.http.credential.command} onChoose={() => void chooseFile((path) => change((value) => { value.source.http!.credential!.command = path; }), "locator-program")} />
            </>
          ) : null}
          <label htmlFor="observation-http-record-key">Record key path</label>
          <input id="observation-http-record-key" type="text" value={source.extraction?.record_key.join(".") ?? ""} onChange={(event) => change((value) => { if (value.source.extraction) value.source.extraction.record_key = event.target.value.split(".").filter(Boolean); })} />
        </>
      ) : null}

      {kind === "downstream-capture" && source.capture ? (
        <>
          <label htmlFor="observation-case">Case</label>
          <select id="observation-case" value={source.capture.path} onChange={(event) => change((value) => { value.source.capture!.path = event.target.value; })}>
            <option value="">Choose a case</option>
            {cases.map((entry) => (
              <option key={entry.ref.id} value={entry.summary.case?.entry ?? entry.name}>
                {entry.name}
              </option>
            ))}
          </select>
          <label htmlFor="observation-capture-key">Record key HL7 field</label>
          <input id="observation-capture-key" type="text" spellCheck={false} value={source.capture.record_key} onChange={(event) => change((value) => { value.source.capture!.record_key = event.target.value; })} />
          <label htmlFor="observation-max-occurrences">Maximum occurrences</label>
          <input id="observation-max-occurrences" type="text" inputMode="numeric" value={String(source.capture.max_occurrences)} onChange={(event) => change((value) => { value.source.capture!.max_occurrences = Number(event.target.value) || 0; })} />
        </>
      ) : null}

      {kind === "database-query" && source.database ? (
        <>
          <label htmlFor="observation-driver">Database</label>
          <select id="observation-driver" value={source.database.driver} onChange={(event) => change((value) => { value.source.database!.driver = event.target.value; })}>
            {Object.entries(DRIVERS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
          <label htmlFor="observation-db-address">Address</label>
          <input id="observation-db-address" type="text" spellCheck={false} value={source.database.address} onChange={(event) => change((value) => { value.source.database!.address = event.target.value; })} />
          <label htmlFor="observation-view">View</label>
          <input id="observation-view" type="text" spellCheck={false} value={source.database.view.join(".")} onChange={(event) => change((value) => { value.source.database!.view = event.target.value.split(".").filter(Boolean); })} />
          <label htmlFor="observation-db-key">Record key column</label>
          <input id="observation-db-key" type="text" spellCheck={false} value={source.database.record_key} onChange={(event) => change((value) => { value.source.database!.record_key = event.target.value; })} />
          <label htmlFor="observation-db-store">Credential store</label>
          <select id="observation-db-store" value={source.database.credential.store} onChange={(event) => change((value) => { value.source.database!.credential.store = event.target.value as SecretStore; })}>
            <option value="os-keychain">OS keychain</option>
            <option value="customer-managed">Customer-managed vault</option>
          </select>
          <FilePicker label="Credential locator program" path={source.database.credential.command} onChoose={() => void chooseFile((path) => change((value) => { value.source.database!.credential.command = path; }), "locator-program")} />
          <fieldset>
            <legend>Filters</legend>
            {source.database.filters.map((filter, index) => (
              <div key={index} className="filter-rule-row">
                <span>
                  {filter.column} equals {filter.value}
                </span>
                <IconButton icon="close" label={`Remove filter ${index + 1}`} onClick={() => change((value) => { value.source.database!.filters.splice(index, 1); })} />
              </div>
            ))}
            <div className="filter-rule-row">
              <input type="text" aria-label="Filter column" spellCheck={false} value={filterDraft.column} onChange={(event) => setFilterDraft({ ...filterDraft, column: event.target.value })} />
              <span>equals</span>
              <input type="text" aria-label="Filter value" value={filterDraft.value} onChange={(event) => setFilterDraft({ ...filterDraft, value: event.target.value })} />
              <button
                type="button"
                disabled={filterDraft.column.trim() === "" || filterDraft.value === ""}
                onClick={() => {
                  change((value) => { value.source.database!.filters.push({ column: filterDraft.column.trim(), value: filterDraft.value }); });
                  setFilterDraft({ column: "", value: "" });
                }}
              >
                Add filter
              </button>
            </div>
          </fieldset>
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
          <input id="observation-baseline" type="text" spellCheck={false} value={rule.pre_existing_state.baseline_identity} onChange={(event) => change((value) => { value.window.pre_existing_state.baseline_identity = event.target.value; })} />
        </>
      ) : null}
      <label htmlFor="observation-deadline">Deadline</label>
      <input id="observation-deadline" type="text" value={rule.completion.deadline} onChange={(event) => change((value) => { value.window.completion.deadline = event.target.value; })} />
      <label htmlFor="observation-quiet">Quiet period</label>
      <input id="observation-quiet" type="text" value={rule.completion.quiet_period} onChange={(event) => change((value) => { value.window.completion.quiet_period = event.target.value; })} />
      <label htmlFor="observation-stable">Stable samples</label>
      <input id="observation-stable" type="text" inputMode="numeric" value={String(rule.completion.stable_samples)} onChange={(event) => change((value) => { value.window.completion.stable_samples = Number(event.target.value) || 0; })} />
    </>
  );

  return (
    <FormDialog
      open={open}
      title="Edit observation"
      size="wide"
      submitLabel="Save"
      dirty={dirty}
      onClose={onClose}
      onSubmit={async (): Promise<SubmitFailure | null> => {
        const answer = await saveItem({
          context: context(),
          kind: "observation",
          item: item.ref.id,
          ...(item.ref.revision ? { base_revision: item.ref.revision } : {}),
          draft: { ...draft, name: name.trim(), observation: held },
          intent_id: newIntentId(),
        });
        if (answer.outcome !== "saved") {
          const problem = saveProblem(answer, fields);
          if (answer.problems.some((entry) => entry.field.includes("window"))) setStep("completion");
          else setStep("source");
          return problem;
        }
        setDirty(false);
        await onSaved();
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

