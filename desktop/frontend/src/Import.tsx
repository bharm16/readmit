// Import: one started flow — Source, Format, Preview — that turns chosen
// messages into one case of the open project. Choosing reads nothing but the
// inputs' heads; Preview reads them under the chosen declaration; Import
// writes the case and its project association as one intent and opens it.
// Nothing is guessed: a format is chosen for the person only when exactly one
// reading fits, and an engine export is always the person's choice.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  cancel,
  chooseImportSources,
  classifyDroppedSources,
  importCase,
  inspectImportPreview,
  newIntentId,
  previewImport,
  probeImport,
  saveItem,
  stagePastedContent,
  type BundleDirection,
  type EditorDraft,
  type EngineExportEngine,
  type EngineExportFormat,
  type EnginePlan,
  type FhirevidenceDeclaration,
  type HL7Terminator,
  type ImportEnvelope,
  type ImportLocation,
  type ImportPlan,
  type ImportPreviewResult,
  type ImportPreviewRow,
  type ImportProbeResult,
  type ImportRequest,
  type ImportTimeOperator,
  type InspectionResult,
  type ItemRef,
  type MappingRecipe,
  type RequestContext,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { draftFor, RetentionStatus, useRetainer } from "./drafting";
import { IconButton } from "./IconButton";
import { MessageReader } from "./Inspector";
import { FormDialog, Menu, StepDialog, ValueRows, type FlowStep, type SubmitFailure } from "./layout";
import { DIRECTION_NAMES } from "./Messages";
import { useVocabulary } from "./vocabulary";
import "./import.css";

/** The draft kind the flow keeps its choices under, and the content it keeps. */
const DRAFT_KIND = "import";
const DRAFT_SCHEMA = "readmit-import-draft/v2";

/** Where native file drops go while the Import flow is open. The window
 * registers one drop listener as it starts and hands paths here. */
export const importDrop: { deliver: ((paths: string[]) => void) | null } = { deliver: null };

type Staged = { id: string; name: string; size: number };
type Sources = { files: string[]; folders: string[]; archives: string[]; staged: Staged[] };
const NO_SOURCES: Sources = { files: [], folders: [], archives: [], staged: [] };

/** What a mapping field reads: nothing, a field of each record, or one value
 * declared for every record. */
type Pick = { from: "none" | "field" | "fixed"; field: string; value: string };
const NONE: Pick = { from: "none", field: "", value: "" };

/** The mapping a person declares for an envelope of records, as the sheet
 * holds it; it becomes a mapping recipe only when the flow previews. */
type Mapping = {
  recordPath: string;
  message: string;
  base64: boolean;
  time: Pick;
  timeFormat: ImportTimeOperator;
  source: Pick;
  direction: Pick;
  directions: { value: string; direction: BundleDirection }[];
  channel: Pick;
  separator: string;
  fields: string;
};

function newMapping(): Mapping {
  return {
    recordPath: "",
    message: "",
    base64: false,
    time: NONE,
    timeFormat: "rfc3339",
    source: NONE,
    direction: NONE,
    directions: [],
    channel: NONE,
    separator: "|",
    fields: "",
  };
}

/** The format the import reads its inputs as. */
type Format = { kind: "plan"; plan: ImportPlan; label: string } | { kind: "recipe"; envelope: ImportEnvelope; label: string } | { kind: "engine"; engine: EnginePlan; label: string } | { kind: "fhir"; declaration: FhirevidenceDeclaration; label: string };

type DraftContent = { investigation?: import("./bindings").ImportInvestigation; step: string; sources: Sources; caseName: string; named: boolean; format: Format | null; mapping: Mapping };

const TIME_FORMATS: Record<Exclude<ImportTimeOperator, "unknown">, string> = {
  rfc3339: "ISO 8601 with offset",
  "unix-seconds": "Unix seconds",
  "unix-milliseconds": "Unix milliseconds",
  "hl7-dtm": "HL7 date/time",
};

const KINDS: Record<string, string> = { file: "File", folder: "Folder", archive: "ZIP" };
const ROW_KINDS: Record<string, string> = { unparsed: "Unparsed", unmapped: "Not mapped" };

function baseName(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

function sizeText(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** The case name a new selection proposes: a single source's name without
 * its extension, or Imported messages for several. */
function proposedName(sources: Sources): string {
  const names = [...sources.files, ...sources.folders, ...sources.archives].map(baseName).concat(sources.staged.map((entry) => entry.name));
  if (names.length !== 1) return "Imported messages";
  return names[0]!.replace(/\.[^.]+$/, "") || names[0]!;
}

/** A case name the facade accepts: 1–200 printable characters. */
function validName(name: string): boolean {
  const trimmed = name.trim();
  return trimmed.length > 0 && trimmed.length <= 200 && !/[\u0000-\u001f\u007f]/.test(trimmed);
}

function locator(path: string): string[] {
  return path === "" ? [] : path.split(".");
}

/** The recipe a mapping declares over an envelope. Unknown time, source,
 * direction and channel stay unknown. */
function recipeOf(envelope: ImportEnvelope, mapping: Mapping, probe: ImportProbeResult | null): MappingRecipe {
  const sample = probe?.sample;
  const relative = (field: string) => locator(field);
  const label = (pick: Pick) =>
    pick.from === "field" ? { operator: "field" as const, locator: relative(pick.field) } : pick.from === "fixed" ? { operator: "declared" as const, declared: pick.value } : { operator: "unknown" as const };
  const recipe: MappingRecipe = {
    schema: "",
    name: "import-mapping",
    revision: 1,
    envelope,
    encoding: "utf-8",
    members: [],
    payload: { operator: mapping.base64 ? "base64" : "verbatim", locator: relative(mapping.message), framing: "raw", terminator: "cr" },
    observed_at: mapping.time.from === "field" ? { operator: mapping.timeFormat, locator: relative(mapping.time.field) } : { operator: "unknown" },
    source: label(mapping.source),
    direction:
      mapping.direction.from === "field"
        ? { operator: "field", locator: relative(mapping.direction.field), values: mapping.directions.map((row) => ({ envelope: row.value, mapped: row.direction })) }
        : mapping.direction.from === "fixed"
          ? { operator: "declared", declared: (mapping.direction.value || "unknown") as BundleDirection }
          : { operator: "declared", declared: "unknown" },
    channel: label(mapping.channel),
  };
  if (envelope === "csv") {
    const columns = sample?.columns ?? [];
    recipe.csv = { delimiter: ",", record_separator: "lf", header: columns.length > 0 ? "present" : "absent", fields: columns.length || sample?.fields || Number(mapping.fields) || 0 };
  } else if (envelope === "text") {
    recipe.text = { field_separator: mapping.separator, record_separator: "lf", fields: Number(mapping.fields) || 0 };
  } else if (envelope === "json") {
    recipe.json = { record_path: locator(mapping.recordPath) };
  } else {
    recipe.xml = { record_path: locator(mapping.recordPath) };
  }
  return recipe;
}

/** Whether a mapping names everything a preview needs; the preview itself
 * says what the reader refuses. */
function mappingComplete(envelope: ImportEnvelope, mapping: Mapping): boolean {
  if (mapping.message === "") return false;
  if (envelope === "xml" && mapping.recordPath === "") return false;
  if (envelope === "text" && (mapping.separator.length !== 1 || !/^\d+$/.test(mapping.fields))) return false;
  const pickOk = (pick: Pick) => pick.from === "none" || (pick.from === "field" ? pick.field !== "" : pick.value.trim() !== "");
  return pickOk(mapping.time) && pickOk(mapping.source) && pickOk(mapping.direction) && pickOk(mapping.channel);
}

export type ImportFlowProps = {
  open: boolean;
  /** The open project's folder. */
  root: string;
  context: () => RequestContext;
  drafts: EditorDraft[] | null;
  busy: boolean;
  onClose: () => void;
  /** The case the import published, by reference and entry. */
  onImported: (ref: ItemRef,selection?: import("./bindings").RetainedImportSelection) => void;
 seed?: import("./bindings").ImportInvestigation | null;
};

export function ImportFlow({ open, root, context, drafts, busy: windowBusy, onClose, onImported, seed }: ImportFlowProps) {
  const vocabulary = useVocabulary();
  const retainer = useRetainer();
  const [investigation,setInvestigation]=useState<import("./bindings").ImportInvestigation | undefined>(undefined);
  const [step, setStep] = useState("source");
  const [sources, setSources] = useState<Sources>(NO_SOURCES);
  const [caseName, setCaseName] = useState("");
  const [named, setNamed] = useState(false);
  const [probe, setProbe] = useState<ImportProbeResult | null>(null);
  const [format, setFormat] = useState<Format | null>(null);
  const [mapping, setMapping] = useState<Mapping>(newMapping());
  const [preview, setPreview] = useState<ImportPreviewResult | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  // The import writing its case, which Stop cancels.
  const [importing, setImporting] = useState(false);
  const busy = windowBusy || importing;
  const [sheet, setSheet] = useState<null | "paste" | "format" | "mapping" | "save-mapping">(null);
  const [selectedRow, setSelectedRow] = useState<number | null>(null);
  const [reading, setReading] = useState<InspectionResult | null>(null);
  const [readingBusy, setReadingBusy] = useState(false);
  const probes = useRef(0);
  const previews = useRef(0);
  const reads = useRef(0);
  const intents = useRef(new Map<string, string>());
  // The selection as it is now, for answers that arrive after it changed.
  const sourcesNow = useRef(sources);
  sourcesNow.current = sources;
  const restored = useRef(false);

  // A draft left by an earlier visit comes back with its choices.
  useEffect(() => {
    if (!open) {
      restored.current = false;
      intents.current = new Map();
      return;
    }
    if (restored.current) return;
    restored.current = true;
    const held = seed ? undefined : draftFor(drafts, DRAFT_KIND, root);
    if(seed) {
      const next={...NO_SOURCES,files:[seed.file]};
      setInvestigation(seed);setSources(next);setCaseName(proposedName(next));setNamed(false);setMapping(newMapping());setFormat(null);setStep("source");setProbe(null);void runProbe(next);
    } else
    if (held && held.content_schema === DRAFT_SCHEMA && held.content && typeof held.content === "object") {
      const content = held.content as DraftContent;
      setInvestigation(content.investigation);
      setSources(content.sources ?? NO_SOURCES);
      setCaseName(content.caseName ?? "");
      setNamed(content.named ?? false);
      setFormat(content.format ?? null);
      setMapping({ ...newMapping(), ...(content.mapping ?? {}) });
      setStep(content.step === "preview" ? "format" : (content.step ?? "source"));
      retainer.keepId(held.id);
      void runProbe(content.sources ?? NO_SOURCES, false);
    } else {
      setInvestigation(undefined);
      setSources(NO_SOURCES);
      setCaseName("");
      setNamed(false);
      setFormat(null);
      setMapping(newMapping());
      setStep("source");
      setProbe(null);
    }
    setPreview(null);
    setNotice(null);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  // Every choice is kept as this import's draft until the import lands.
  const keep = useCallback(
    (content: DraftContent) => {
      retainer.save({ id: retainer.currentId(), kind: DRAFT_KIND, workspace: root, case: "", identity: "", content_schema: DRAFT_SCHEMA, content });
    },
    [retainer, root],
  );
  useEffect(() => {
    if (!open || !restored.current) return;
    if (sources === NO_SOURCES && caseName === "" && format === null) return;
    keep({ step, sources, caseName, named, format, mapping, ...(investigation ? {investigation} : {}) });
  }, [step, sources, caseName, named, format, mapping, investigation]); // eslint-disable-line react-hooks/exhaustive-deps

  /** Reads the heads of the chosen inputs: what each is, and which formats
   * fit them. A format is chosen for the person only when exactly one fits. */
  async function runProbe(next: Sources, choose = true) {
    const asked = ++probes.current;
    if (next.files.length + next.folders.length + next.archives.length + next.staged.length === 0) {
      setProbe(null);
      if (choose) setFormat(null);
      return;
    }
    const answer = await probeImport({ context: context(), files: next.files, folders: next.folders, archives: next.archives, staged: next.staged.map((entry) => entry.id) });
    if (asked !== probes.current) return;
    if (answer.state !== "completed" && answer.state !== "empty") {
      setNotice(answer.reason ?? "The inputs could not be read.");
      setProbe(null);
      return;
    }
    setNotice(null);
    setProbe(answer);
    if (!choose) return;
    const selected = answer.selected !== null ? answer.formats[answer.selected] : undefined;
    if (selected?.plan) setFormat({ kind: "plan", plan: selected.plan, label: selected.label });
    else if (selected?.envelope) setFormat({ kind: "recipe", envelope: selected.envelope, label: selected.label });
    else setFormat(null);
  }

  /** A change of inputs withdraws any preview and re-reads what they are. */
  const changeSources = (next: Sources) => {
    setInvestigation(undefined);
    setSources(next);
    setPreview(null);
    if (!named) setCaseName(proposedName(next));
    return runProbe(next);
  };
  const add = (kind: "files" | "folders" | "archives", paths: string[]) => {
    const merged = { ...sources, [kind]: [...sources[kind], ...paths.filter((path) => !sources[kind].includes(path))] };
    changeSources(merged);
  };
  const choose = async (kind: "files" | "folder" | "archive") => {
    const answer = await chooseImportSources(kind);
    // A cancelled picker leaves the selection as it was.
    if (answer.state === "cancelled") return;
    if (answer.state !== "completed" || !answer.paths) {
      setNotice(answer.reason ?? "Nothing was chosen.");
      return;
    }
    add(kind === "files" ? "files" : kind === "folder" ? "folders" : "archives", answer.paths);
  };

  // Files dropped on the window join the selection the pickers make.
  useEffect(() => {
    if (!open) return;
    importDrop.deliver = (paths) =>
      void classifyDroppedSources(paths).then((answer) => {
        if (answer.state !== "completed") {
          if (answer.state !== "empty") setNotice(answer.reason ?? "The dropped items could not be read.");
          return;
        }
        const now = sourcesNow.current;
        const merged = {
          ...now,
          files: [...now.files, ...answer.files.filter((path) => !now.files.includes(path))],
          folders: [...now.folders, ...answer.folders.filter((path) => !now.folders.includes(path))],
          archives: [...now.archives, ...answer.archives.filter((path) => !now.archives.includes(path))],
        };
        // What the drop refused is said once the inputs have been read.
        const refused = answer.refused.length > 0 ? answer.refused.map((entry) => `${entry.name}: ${entry.reason}`).join(" ") : null;
        void changeSources(merged).then(() => refused && setNotice(refused));
      });
    return () => {
      importDrop.deliver = null;
    };
  });

  const inputs = probe?.inputs ?? [];
  const accepted = inputs.some((input) => input.accepted);
  // One row per chosen location: a folder or ZIP is one input however many
  // files it holds, named as it was chosen.
  const chosen = (() => {
    const rows: { key: string; source: ImportLocation; index: number; name: string; kind: string; size: number; accepted: boolean; reason?: string }[] = [];
    for (const input of inputs) {
      const key = `${input.source}-${input.index}`;
      const held = rows.find((row) => row.key === key);
      if (held) {
        held.size += input.size;
        held.accepted ||= input.accepted;
        continue;
      }
      const path = input.source === "folder" ? sources.folders[input.index] : input.source === "archive" ? sources.archives[input.index] : undefined;
      rows.push({ key, source: input.source, index: input.index, name: path ? baseName(path) : input.name, kind: input.kind, size: input.size, accepted: input.accepted, ...(input.reason ? { reason: input.reason } : {}) });
    }
    return rows;
  })();
  const remove = (source: ImportLocation, index: number) => {
    const without = <T,>(list: T[], from: ImportLocation) => (source === from ? list.filter((_, at) => at !== index) : list);
    void changeSources({ files: without(sources.files, "file"), folders: without(sources.folders, "folder"), archives: without(sources.archives, "archive"), staged: without(sources.staged, "staged") });
  };

  /** The request the preview and the import both read. */
  const request = (): ImportRequest | null => {
    if (!format) return null;
    const base = { context: context(), workspace: root, files: sources.files, folders: sources.folders, archives: sources.archives, staged: sources.staged.map((entry) => entry.id) };
    if (format.kind === "plan") return { ...base, mode: "plan", plan: format.plan };
    if (format.kind === "engine") return { ...base, mode: "engine", engine_plan: format.engine };
    if (format.kind === "fhir") return { ...base, mode: "fhir-r4", fhir: format.declaration };
    return { ...base, mode: "recipe", recipe: recipeOf(format.envelope, mapping, probe) };
  };

  async function runPreview() {
    const asked = ++previews.current;
    const req = request();
    setPreview(null);
    setSelectedRow(null);
    setReading(null);
    if (!req) return;
    setPreviewing(true);
    const answer = await previewImport(req);
    if (asked !== previews.current) return;
    setPreviewing(false);
    setPreview(answer);
  }
  useEffect(() => {
    if (open && step === "preview") void runPreview();
  }, [step]); // eslint-disable-line react-hooks/exhaustive-deps

  const readRow = async (row: number, path = "", nodeOffset = 0, byteOffset = -1, reveal = reading?.inspection?.revealed ?? false) => {
    const req = request();
    if (!req || !preview?.preview_token) return null;
    const asked = ++reads.current;
    setReadingBusy(true);
    const answer = await inspectImportPreview({ context: context(), source: req, preview_token: preview.preview_token, row, path, node_offset: nodeOffset, byte_offset: byteOffset, reveal });
    // Only the latest read describes the row on screen.
    if (asked !== reads.current) return null;
    setReadingBusy(false);
    setReading(answer);
    return answer;
  };

  const formatReady = format !== null && (format.kind !== "recipe" || mappingComplete(format.envelope, mapping)) && (format.kind !== "fhir" || format.declaration.source_kind !== "");
  const previewReady = preview?.state === "completed" && Boolean(preview.preview_token);

  const sourceStep = (
    <div className="import-drop" style={{ ["--wails-drop-target" as string]: "drop" }}>
      <div className="row-actions">
        <span className="split-button">
          <button type="button" disabled={busy} onClick={() => void choose("files")}>
            Choose files
          </button>
          <Menu
            label="More ways to choose"
            items={[
              { label: "Choose folder", onSelect: () => void choose("folder") },
              { label: "Choose ZIP", onSelect: () => void choose("archive") },
            ]}
          />
        </span>
        <button type="button" disabled={busy} onClick={() => setSheet("paste")}>
          Paste
        </button>
      </div>
      {inputs.length > 0 ? (
        <table className="plain-table" aria-label="Selected inputs">
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">Type</th>
              <th scope="col">Size</th>
              <th scope="col">
                <span className="visually-hidden">Remove</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {chosen.map((input) => (
              <tr key={input.key}>
                <th scope="row" title={input.name}>
                  {input.name}
                  {!input.accepted && input.reason ? <span className="row-reason">{input.reason}</span> : null}
                </th>
                <td>{KINDS[input.kind] ?? "File"}</td>
                <td>{sizeText(input.size)}</td>
                <td>
                  <IconButton icon="close" label={`Remove ${input.name} from import`} onClick={() => remove(input.source, input.index)} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {inputs.length > 0 ? (
        <div>
          <button type="button" className="quiet" disabled={busy} onClick={() => void choose("files")}>
            Add files
          </button>
        </div>
      ) : null}
      <label htmlFor="import-case-name">Case</label>
      <input
        id="import-case-name"
        type="text"
        maxLength={200}
        value={caseName}
        onChange={(event) => {
          setCaseName(event.target.value);
          setNamed(true);
        }}
      />
    </div>
  );

  const formatRows = (): { label: string; value: ReactNode }[] => {
    if (!format) return [];
    if (format.kind === "plan") {
      const plan = format.plan;
      return [
        { label: "Format", value: format.label },
        { label: "Framing", value: plan.framing.toUpperCase() === "MLLP" ? "MLLP" : plan.framing === "batch" ? "Batch" : "Raw" },
        { label: "Segment terminator", value: plan.terminator.toUpperCase() },
        { label: "Encoding", value: plan.encoding.toUpperCase() },
      ];
    }
    if (format.kind === "engine") return [{ label: "Format", value: format.label }];
    if (format.kind === "fhir") return [{ label: "Format", value: format.label }, { label: "Source type", value: format.declaration.source_kind }, { label: "FHIR version", value: format.declaration.context.version }, { label: "Media type", value: format.declaration.context.media_type }, ...(format.declaration.context.base ? [{ label: "Reference base", value: format.declaration.context.base }] : [])];
    return [{ label: "Format", value: format.label }];
  };

  const formatStep = (
    <>
      {format ? (
        <>
          <div className="section-header">
            <h3>Format</h3>
            <button type="button" onClick={() => setSheet("format")}>
              Edit
            </button>
          </div>
          <ValueRows rows={formatRows()} />
          {format.kind === "recipe" ? (
            <>
              <div className="section-header">
                <h3>Mapping</h3>
                <span className="row-actions">
                  <button type="button" onClick={() => setSheet("mapping")}>
                    Edit
                  </button>
                  <Menu label="More mapping actions" items={[{ label: "Save mapping…", onSelect: () => setSheet("save-mapping"), disabled: !mappingComplete(format.envelope, mapping) }]} />
                </span>
              </div>
              {mapping.message ? (
                <ValueRows
                  rows={[
                    { label: "Message", value: mapping.message },
                    { label: "Timestamp", value: mapping.time.from === "field" ? `${mapping.time.field} · ${TIME_FORMATS[mapping.timeFormat as keyof typeof TIME_FORMATS]}` : "Unknown" },
                    { label: "Source", value: pickText(mapping.source) },
                    { label: "Direction", value: mapping.direction.from === "fixed" ? DIRECTION_NAMES[(mapping.direction.value || "unknown") as BundleDirection] : pickText(mapping.direction) },
                    { label: "Channel", value: pickText(mapping.channel) },
                  ]}
                />
              ) : (
                <p role="alert">Map the message field.</p>
              )}
            </>
          ) : null}
        </>
      ) : (
        <>
          <label htmlFor="import-format-choice">Choose format</label>
          <select
            id="import-format-choice"
            value=""
            onChange={(event) => {
              const chosen = formatChoices(probe, vocabulary?.import_engines ?? []).find((entry) => entry.key === event.target.value);
              if (!chosen) return;
              setFormat(chosen.format);
              setPreview(null);
              if (chosen.format.kind === "recipe") setSheet("mapping");
              if (chosen.format.kind === "fhir") setSheet("format");
            }}
          >
            <option value="">Choose a format</option>
            {formatChoices(probe, vocabulary?.import_engines ?? []).map((entry) => (
              <option key={entry.key} value={entry.key}>
                {entry.format.label}
              </option>
            ))}
          </select>
        </>
      )}
    </>
  );

  const rows = preview?.rows ?? [];
  const rowColumns: Column<ImportPreviewRow>[] = [
    { key: "time", header: "Time", priority: 1, minWidth: 9, render: (row) => (row.time ? new Date(row.time).toLocaleString() : "—") },
    { key: "type", header: "Type", priority: 1, minWidth: 7, render: (row) => ROW_KINDS[row.kind] ?? (row.type ? row.type.replace("^", " · ") : "Message") },
    { key: "source", header: "Source", priority: 2, minWidth: 9, render: (row) => row.source || "—" },
    { key: "direction", header: "Direction", priority: 3, minWidth: 7, render: (row) => (row.direction && row.direction !== "unknown" ? (DIRECTION_NAMES[row.direction as BundleDirection] ?? "—") : "—") },
  ];
  const problems = preview?.problems;
  const showFirst = (kind: string) => {
    const row = rows.find((entry) => entry.kind === kind);
    if (!row) return;
    setSelectedRow(row.index);
    void readRow(row.index);
  };
  const previewStep = previewing ? (
    <p aria-live="polite">Reading…</p>
  ) : preview && preview.state !== "completed" ? (
    <div role="alert">
      <p>{preview.reason ?? "These inputs could not be read under this format."}</p>
      <button type="button" onClick={() => setStep("format")}>
        Edit
      </button>
    </div>
  ) : preview ? (
    <section className="import-preview" aria-label="Import summary">
      <ValueRows
        rows={[
          { label: "Case", value: <span title={caseName}>{caseName}</span> },
          { label: "Source", value: chosen.filter((input) => input.accepted).map((input) => input.name).join(", ") || "—" },
          { label: "Format", value: format?.label ?? "—" },
        ]}
      />
      <table className="plain-table import-summary-table" aria-label="Import totals"><thead><tr><th scope="col">Preview</th><th scope="col">Result</th></tr></thead><tbody><tr><th scope="row">Occurrences</th><td>{preview.row_total ?? preview.plan_preview?.totals.occurrences ?? "Not recorded"}</td></tr><tr><th scope="row">Message types in preview</th><td>{[...new Set(rows.map(row=>row.type).filter(Boolean))].join(" · ") || "Not recorded"}</td></tr></tbody></table>
      {problems && (problems.excluded || problems.unparsed || problems.unmapped) ? (
        <div className="chips" aria-label="Problems">
          {/* Each chip opens the first row it counts. */}
          {problems.unparsed ? (
            <button type="button" className="chip" onClick={() => showFirst("unparsed")}>
              Unparsed {problems.unparsed}
            </button>
          ) : null}
          {problems.unmapped ? (
            <button type="button" className="chip" onClick={() => showFirst("unmapped")}>
              Not mapped {problems.unmapped}
            </button>
          ) : null}
          {problems.excluded ? <span className="chip">Excluded {problems.excluded}</span> : null}
        </div>
      ) : null}
      <DataTable
        label="Preview"
        rows={rows}
        rowId={(row) => String(row.index)}
        rowLabel={(row) => `${row.type || "Message"} ${row.index + 1}`}
        columns={rowColumns}
        selected={selectedRow === null ? null : String(selectedRow)}
        onSelect={(id) => {
          setSelectedRow(Number(id));
          void readRow(Number(id));
        }}
        onOpen={(id) => {
          setSelectedRow(Number(id));
          void readRow(Number(id));
        }}
      />
      {preview.row_total && preview.row_total > rows.length ? <p>{`${rows.length} of ${preview.row_total} messages shown`}</p> : null}
      {selectedRow !== null ? (
        <MessageReader
          result={reading}
          loading={readingBusy}
          busy={busy}
          onInspect={(path, nodeOffset, byteOffset) => readRow(selectedRow, path, nodeOffset, byteOffset)}
          onReveal={(reveal) => void readRow(selectedRow, reading?.inspection?.selected.path ?? "", 0, -1, reveal)}
        />
      ) : null}
    </section>
  ) : null;

  const steps: FlowStep[] = [
    { key: "source", label: "Source", valid: accepted && validName(caseName), render: () => sourceStep },
    { key: "format", label: "Format", valid: formatReady, render: () => formatStep },
    { key: "preview", label: "Preview", valid: previewReady, render: () => previewStep },
  ];

  return (
    <>
      <StepDialog
        open={open && sheet === null}
        title="Import"
        size="wide"
        steps={steps}
        step={step}
        onStep={(next) => {
          // An unambiguous HL7 reading goes straight to its preview.
          if (step === "source" && next === "format" && format?.kind === "plan") {
            setStep("preview");
            return;
          }
          setStep(next);
        }}
        submitLabel="Import"
        submitDisabled={!previewReady}
        finalSecondary={
          importing ? (
            <button type="button" onClick={() => cancel("import")}>
              Stop
            </button>
          ) : null
        }
        busy={busy}
        onClose={onClose}
        status={
          <>
            {notice ? <p role="alert">{notice}</p> : null}
            <RetentionStatus
              retention={retainer.retention}
              onRetry={retainer.retry}
              onKeepAsNew={retainer.keepAsNew}
              onDiscard={() => void retainer.dropCurrent()}
            />
          </>
        }
        onSubmit={async (): Promise<SubmitFailure | null> => {
          const req = request();
          if (!req || !preview?.preview_token) return { reason: "Preview the import first." };
          setImporting(true);
          const answer = await importCase({ context: context(), name: caseName.trim(), source: req, preview_token: preview.preview_token, intent_id: intentFor(intents.current, req, caseName), ...(investigation ? {investigation} : {}) }).finally(() => setImporting(false));
          if (answer.stale) {
            void runPreview();
            return { reason: "The inputs or their format changed since this preview; it was read again." };
          }
          if (answer.state !== "completed" || !answer.case) return { reason: answer.reason ?? "The case was not imported." };
          // The import is stored: its draft goes before the case opens, so
          // nothing is offered back as unstored work.
          await retainer.dropCurrent();
          onImported(answer.case,answer.selection);
          return null;
        }}
      />
      {sheet === "paste" ? (
        <PasteSheet
          context={context}
          onClose={() => setSheet(null)}
          onAdded={(staged) => {
            setSheet(null);
            changeSources({ ...sources, staged: [...sources.staged, staged] });
          }}
        />
      ) : null}
      {sheet === "format" && format ? (
        <FormatSheet
          probe={probe}
          format={format}
          onClose={() => setSheet(null)}
          onDone={(next) => {
            setFormat(next);
            setPreview(null);
            setSheet(next.kind === "recipe" && !mapping.message ? "mapping" : null);
          }}
        />
      ) : null}
      {sheet === "save-mapping" && format?.kind === "recipe" ? (
        <SaveMappingSheet context={context} recipe={recipeOf(format.envelope, mapping, probe)} onClose={() => setSheet(null)} />
      ) : null}
      {sheet === "mapping" && format?.kind === "recipe" ? (
        <MappingSheet
          envelope={format.envelope}
          probe={probe}
          mapping={mapping}
          onClose={() => setSheet(null)}
          onDone={(next) => {
            setMapping(next);
            setPreview(null);
            setSheet(null);
          }}
        />
      ) : null}
    </>
  );
}

/** One click's intent within one opening of the flow: the same inputs,
 * format and name again are the same click, so a second Import publishes
 * nothing new; a later opening is a new import. */
function intentFor(intents: Map<string, string>, request: ImportRequest, name: string): string {
  const { context: _context, ...rest } = request;
  const key = JSON.stringify([rest, name.trim()]);
  let intent = intents.get(key);
  if (!intent) {
    intent = newIntentId();
    intents.set(key, intent);
  }
  return intent;
}

function pickText(pick: Pick): string {
  return pick.from === "field" ? pick.field : pick.from === "fixed" ? pick.value : "Unknown";
}

/** Every format a person may choose for these inputs: those the probe found,
 * and each supported engine export by name and exact version. */
function formatChoices(probe: ImportProbeResult | null, engines: EngineExportEngine[]): { key: string; format: Format }[] {
  const found = (probe?.formats ?? []).flatMap((entry, index): { key: string; format: Format }[] => {
    if (entry.plan) return [{ key: `probe-${index}`, format: { kind: "plan", plan: entry.plan, label: entry.label } }];
    if (entry.envelope) return [{ key: `probe-${index}`, format: { kind: "recipe", envelope: entry.envelope, label: entry.label } }];
    return [];
  });
  const exports = engines.map((engine) => ({
    key: `engine-${engine.engine}`,
    format: {
      kind: "engine" as const,
      label: `${engine.name} ${engine.version}`,
      engine: { schema: "", engine: engine.engine, version: engine.version, format: engine.formats[0] ?? "raw", terminator: engine.terminators[0] ?? "cr" },
    },
  }));
  return [...found, { key: "fhir-r4", format: { kind: "fhir", label: "FHIR R4 JSON", declaration: { source_kind: "", context: { version: "4.0.1", media_type: "application/fhir+json", base: "" } } } }, ...exports];
}

/** Paste: messages typed or pasted in become their own source. */
function PasteSheet({ context, onClose, onAdded }: { context: () => RequestContext; onClose: () => void; onAdded: (staged: Staged) => void }) {
  const [content, setContent] = useState("");
  const [name, setName] = useState("Pasted messages");
  return (
    <FormDialog
      open
      title="Paste"
      submitLabel="Add"
      submitDisabled={content.trim() === "" || !validName(name)}
      dirty={content !== ""}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await stagePastedContent({ context: context(), name: name.trim(), content });
        if (answer.state !== "completed" || !answer.staged_id) return { reason: answer.reason ?? "The messages were not added." };
        onAdded({ id: answer.staged_id, name: answer.name ?? name.trim(), size: answer.size ?? content.length });
        return null;
      }}
    >
      <label htmlFor="paste-messages">Messages</label>
      <textarea id="paste-messages" rows={10} spellCheck={false} value={content} onChange={(event) => setContent(event.target.value)} />
      <label htmlFor="paste-name">Name</label>
      <input id="paste-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
    </FormDialog>
  );
}

/** The explicit format: which reading, and for HL7 its framing, segment
 * terminator, encoding and batch boundary. Choosing is not repairing: the
 * bytes are read as declared. */
function FormatSheet({ probe, format, onClose, onDone }: { probe: ImportProbeResult | null; format: Format; onClose: () => void; onDone: (format: Format) => void }) {
  const vocabulary = useVocabulary();
  const plans = vocabulary?.import_plan;
  const choices = formatChoices(probe, vocabulary?.import_engines ?? []);
  const [draft, setDraft] = useState<Format>(format);
  const keyOf = (entry: Format) => choices.find((choice) => choice.format.label === entry.label && choice.format.kind === entry.kind)?.key ?? "";
  const engineOf = (engine: string) => (vocabulary?.import_engines ?? []).find((entry) => entry.engine === engine);
  const plan = draft.kind === "plan" ? draft.plan : null;
  const setPlan = (patch: Partial<ImportPlan>) => plan && draft.kind === "plan" && setDraft({ ...draft, plan: { ...plan, ...patch } });
  return (
    <FormDialog open title="Format" submitLabel="Done" submitDisabled={draft.kind === "fhir" && (draft.declaration.source_kind === "" || draft.declaration.source_kind === "request" && (!draft.declaration.context.base || !draft.declaration.request?.url))} dirty={JSON.stringify(draft) !== JSON.stringify(format)} onClose={onClose} onSubmit={() => { onDone(draft); return null; }}>
      <label htmlFor="format-choice">Format</label>
      <select
        id="format-choice"
        value={keyOf(draft)}
        onChange={(event) => {
          const chosen = choices.find((choice) => choice.key === event.target.value);
          if (chosen) setDraft(chosen.format);
        }}
      >
        {keyOf(draft) === "" ? <option value="">{draft.label}</option> : null}
        {choices.map((choice) => (
          <option key={choice.key} value={choice.key}>
            {choice.format.label}
          </option>
        ))}
      </select>
      {plan && plans ? (
        <>
          <label htmlFor="format-framing">Framing</label>
          <select id="format-framing" value={plan.framing} onChange={(event) => setPlan({ framing: event.target.value as ImportPlan["framing"] })}>
            {plans.framings.map((value) => (
              <option key={value} value={value}>
                {value === "mllp" ? "MLLP" : value === "batch" ? "Batch" : "Raw"}
              </option>
            ))}
          </select>
          <label htmlFor="format-terminator">Segment terminator</label>
          <select id="format-terminator" value={plan.terminator} onChange={(event) => setPlan({ terminator: event.target.value as ImportPlan["terminator"] })}>
            {plans.terminators.map((value) => (
              <option key={value} value={value}>
                {value.toUpperCase()}
              </option>
            ))}
          </select>
          <label htmlFor="format-encoding">Encoding</label>
          <select id="format-encoding" value={plan.encoding} onChange={(event) => setPlan({ encoding: event.target.value as ImportPlan["encoding"] })}>
            {plans.encodings.map((value) => (
              <option key={value} value={value}>
                {value === "unknown" ? "Unknown" : value.toUpperCase()}
              </option>
            ))}
          </select>
          {plan.framing === "batch" ? (
            <>
              <label htmlFor="format-boundary">Batch boundary</label>
              <select id="format-boundary" value={plan.batch_boundary ?? ""} onChange={(event) => setPlan({ batch_boundary: event.target.value as ImportPlan["batch_boundary"] & string })}>
                {plan.batch_boundary ? null : <option value="">Choose a boundary</option>}
                {plans.boundaries.map((value) => (
                  <option key={value} value={value}>
                    {value === "hl7-batch" ? "HL7 batch (FHS/BHS)" : "Each MSH segment"}
                  </option>
                ))}
              </select>
            </>
          ) : null}
        </>
      ) : null}
      {draft.kind === "fhir" ? <>
        <label htmlFor="format-fhir-source">Source type</label>
        <select id="format-fhir-source" value={draft.declaration.source_kind} onChange={(event) => {
          const source_kind = event.target.value;
          const { request: _request, ...declaration } = draft.declaration;
          setDraft({ ...draft, declaration: { ...declaration, source_kind, ...(source_kind === "request" ? { request: { method: "GET", url: "", headers: {} } } : {}) } });
        }}><option value="">Choose a source type</option><option value="resource">Resource</option><option value="bundle">Bundle</option><option value="request">Request evidence</option></select>
        <label htmlFor="format-fhir-version">FHIR version</label><select id="format-fhir-version" value={draft.declaration.context.version} onChange={(event) => setDraft({ ...draft, declaration: { ...draft.declaration, context: { ...draft.declaration.context, version: event.target.value } } })}><option value="4.0.1">R4 · 4.0.1</option></select>
        <label htmlFor="format-fhir-media">Media type</label><select id="format-fhir-media" value={draft.declaration.context.media_type} onChange={(event) => setDraft({ ...draft, declaration: { ...draft.declaration, context: { ...draft.declaration.context, media_type: event.target.value } } })}><option value="application/fhir+json">application/fhir+json</option></select>
        <label htmlFor="format-fhir-base">Reference base</label><input id="format-fhir-base" type="text" value={draft.declaration.context.base} onChange={(event) => setDraft({ ...draft, declaration: { ...draft.declaration, context: { ...draft.declaration.context, base: event.target.value } } })} />
        {draft.declaration.request ? <>
          <label htmlFor="format-fhir-method">Request method</label><select id="format-fhir-method" value={draft.declaration.request.method} onChange={(event) => setDraft({ ...draft, declaration: { ...draft.declaration, request: { ...draft.declaration.request!, method: event.target.value } } })}>{["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"].map((method) => <option key={method}>{method}</option>)}</select>
          <label htmlFor="format-fhir-url">Request URL</label><input id="format-fhir-url" type="text" value={draft.declaration.request.url} onChange={(event) => setDraft({ ...draft, declaration: { ...draft.declaration, request: { ...draft.declaration.request!, url: event.target.value } } })} />
          {(["if_match", "if_none_match", "if_modified_since", "if_none_exist", "prefer"] as const).map((key) => <label key={key}>{key.replaceAll("_", "-")}<input type="text" value={draft.declaration.request?.headers[key] ?? ""} onChange={(event) => setDraft({ ...draft, declaration: { ...draft.declaration, request: { ...draft.declaration.request!, headers: { ...draft.declaration.request!.headers, [key]: event.target.value } } } })} /></label>)}
        </> : null}
      </> : null}
      {draft.kind === "engine" ? (
        <>
          <ValueRows rows={[{ label: "Version", value: draft.engine.version }]} />
          <label htmlFor="format-engine-format">Messages as</label>
          <select id="format-engine-format" value={draft.engine.format} onChange={(event) => setDraft({ ...draft, engine: { ...draft.engine, format: event.target.value as EngineExportFormat } })}>
            {(engineOf(draft.engine.engine)?.formats ?? [draft.engine.format]).map((value) => (
              <option key={value} value={value}>
                {value === "message-xml" ? "Message XML" : "Raw"}
              </option>
            ))}
          </select>
          <label htmlFor="format-engine-terminator">Segment terminator</label>
          <select id="format-engine-terminator" value={draft.engine.terminator} onChange={(event) => setDraft({ ...draft, engine: { ...draft.engine, terminator: event.target.value as HL7Terminator } })}>
            {(engineOf(draft.engine.engine)?.terminators ?? [draft.engine.terminator]).map((value) => (
              <option key={value} value={value}>
                {value.toUpperCase()}
              </option>
            ))}
          </select>
        </>
      ) : null}
    </FormDialog>
  );
}

/** The mapping of an envelope of records to messages. Each field is picked
 * from the actual input's columns or paths, or declared as one fixed value;
 * what is not mapped stays unknown. */
function MappingSheet({ envelope, probe, mapping, onClose, onDone }: { envelope: ImportEnvelope; probe: ImportProbeResult | null; mapping: Mapping; onClose: () => void; onDone: (mapping: Mapping) => void }) {
  const vocabulary = useVocabulary();
  const [draft, setDraft] = useState<Mapping>(mapping);
  const [encodingShown, setEncodingShown] = useState(false);
  const sample = probe?.sample;
  const set = (patch: Partial<Mapping>) => setDraft((held) => ({ ...held, ...patch }));
  // The fields a record offers: CSV columns (or column numbers without a
  // header), text field numbers, or JSON/XML paths below the record path.
  const fields = (() => {
    if (envelope === "csv") {
      if ((sample?.columns ?? []).length > 0) return sample!.columns;
      return Array.from({ length: sample?.fields ?? 0 }, (_, index) => String(index + 1));
    }
    if (envelope === "text") return Array.from({ length: Number(draft.fields) || 0 }, (_, index) => String(index + 1));
    const record = locator(draft.recordPath);
    return (sample?.paths ?? [])
      .filter((path) => path.length > record.length && record.every((part, at) => path[at] === part))
      .map((path) => path.slice(record.length).join("."));
  })();
  const records = envelope === "json" || envelope === "xml" ? (sample?.paths ?? []).map((path) => path.join(".")) : [];
  const fieldSelect = (id: string, value: string, onChange: (value: string) => void, placeholder = "Choose a field") => (
    <select id={id} value={value} onChange={(event) => onChange(event.target.value)}>
      {value === "" ? <option value="">{placeholder}</option> : null}
      {fields.map((field) => (
        <option key={field} value={field}>
          {field}
        </option>
      ))}
      {value && !fields.includes(value) ? <option value={value}>{value}</option> : null}
    </select>
  );
  const pick = (key: "source" | "channel" | "direction", label: string) => {
    const held = draft[key];
    return (
      <fieldset>
        <legend>{label}</legend>
        <select aria-label={`${label} from`} value={held.from} onChange={(event) => set({ [key]: { ...held, from: event.target.value as Pick["from"] } })}>
          <option value="none">Unknown</option>
          <option value="field">From a field</option>
          <option value="fixed">One value</option>
        </select>
        {held.from === "field" ? fieldSelect(`mapping-${key}-field`, held.field, (field) => set({ [key]: { ...held, field } })) : null}
        {held.from === "fixed" && key === "direction" ? (
          <select aria-label={`${label} value`} value={held.value || "unknown"} onChange={(event) => set({ [key]: { ...held, value: event.target.value } })}>
            {(Object.keys(DIRECTION_NAMES) as BundleDirection[]).map((direction) => (
              <option key={direction} value={direction}>
                {DIRECTION_NAMES[direction]}
              </option>
            ))}
          </select>
        ) : held.from === "fixed" ? (
          <input aria-label={`${label} value`} type="text" value={held.value} onChange={(event) => set({ [key]: { ...held, value: event.target.value } })} />
        ) : null}
      </fieldset>
    );
  };
  return (
    <FormDialog
      open
      title="Mapping"
      size="wide"
      submitLabel="Done"
      dirty={JSON.stringify(draft) !== JSON.stringify(mapping)}
      onClose={onClose}
      onSubmit={() => {
        if (draft.message === "") return { reason: "Choose the field that holds the message.", field: "mapping-message" };
        if (envelope === "text" && draft.separator.length !== 1) return { reason: "Enter one separator character.", field: "mapping-separator" };
        if (envelope === "text" && !/^\d+$/.test(draft.fields)) return { reason: "Enter a whole number of fields.", field: "mapping-fields" };
        const partial = draft.direction.from === "field" ? draft.directions.findIndex((row) => row.value.trim() === "") : -1;
        if (partial >= 0) return { reason: "Enter the source value, or remove the row.", field: `mapping-direction-value-${partial}` };
        onDone(draft);
        return null;
      }}
    >
      {envelope === "json" || envelope === "xml" ? (
        <>
          <label htmlFor="mapping-records">Records at</label>
          <select id="mapping-records" value={draft.recordPath} onChange={(event) => set({ recordPath: event.target.value })}>
            {envelope === "json" ? <option value="">The whole document</option> : draft.recordPath === "" ? <option value="">Choose an element</option> : null}
            {records.map((path) => (
              <option key={path} value={path}>
                {path}
              </option>
            ))}
          </select>
        </>
      ) : null}
      {envelope === "text" ? (
        <div className="inline-fields">
          <div>
            <label htmlFor="mapping-separator">Field separator</label>
            <input id="mapping-separator" type="text" maxLength={1} value={draft.separator} onChange={(event) => set({ separator: event.target.value })} />
          </div>
          <div>
            <label htmlFor="mapping-fields">Fields per record</label>
            <input id="mapping-fields" type="text" inputMode="numeric" value={draft.fields} onChange={(event) => set({ fields: event.target.value })} />
          </div>
        </div>
      ) : null}
      <label htmlFor="mapping-message">Message</label>
      {fieldSelect("mapping-message", draft.message, (message) => set({ message }))}
      {/* A message stored encoded is declared so only when asked for. */}
      {draft.base64 || encodingShown ? (
        <label className="check">
          <input type="checkbox" checked={draft.base64} onChange={(event) => set({ base64: event.target.checked })} />
          Message is Base64-encoded
        </label>
      ) : (
        <div>
          <button type="button" className="quiet" onClick={() => setEncodingShown(true)}>
            Message encoding…
          </button>
        </div>
      )}
      <fieldset>
        <legend>Timestamp</legend>
        <select aria-label="Timestamp from" value={draft.time.from === "field" ? "field" : "none"} onChange={(event) => set({ time: { ...draft.time, from: event.target.value as Pick["from"] } })}>
          <option value="none">Unknown</option>
          <option value="field">From a field</option>
        </select>
        {draft.time.from === "field" ? (
          <>
            {fieldSelect("mapping-time-field", draft.time.field, (field) => set({ time: { ...draft.time, field } }))}
            <select aria-label="Timestamp format" value={draft.timeFormat} onChange={(event) => set({ timeFormat: event.target.value as ImportTimeOperator })}>
              {(vocabulary?.import_time_operators ?? []).filter((operator) => operator !== "unknown").map((operator) => (
                <option key={operator} value={operator}>
                  {TIME_FORMATS[operator as keyof typeof TIME_FORMATS]}
                </option>
              ))}
            </select>
          </>
        ) : null}
      </fieldset>
      {pick("source", "Source")}
      {pick("direction", "Direction")}
      {draft.direction.from === "field" ? (
        <table className="plain-table edit-table" aria-label="Direction values">
          <thead>
            <tr>
              <th scope="col">Source value</th>
              <th scope="col">Direction</th>
              <th scope="col">
                <span className="visually-hidden">Remove</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {draft.directions.map((row, index) => (
              <tr key={index}>
                <td>
                  <input
                    id={`mapping-direction-value-${index}`}
                    aria-label={`Source value ${index + 1}`}
                    type="text"
                    value={row.value}
                    onChange={(event) => set({ directions: draft.directions.map((entry, at) => (at === index ? { ...entry, value: event.target.value } : entry)) })}
                  />
                </td>
                <td>
                  <select
                    aria-label={`Direction ${index + 1}`}
                    value={row.direction}
                    onChange={(event) => set({ directions: draft.directions.map((entry, at) => (at === index ? { ...entry, direction: event.target.value as BundleDirection } : entry)) })}
                  >
                    {(Object.keys(DIRECTION_NAMES) as BundleDirection[]).map((direction) => (
                      <option key={direction} value={direction}>
                        {DIRECTION_NAMES[direction]}
                      </option>
                    ))}
                  </select>
                </td>
                <td>
                  <IconButton icon="close" label={`Remove mapping ${index + 1}`} onClick={() => set({ directions: draft.directions.filter((_, at) => at !== index) })} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
      {draft.direction.from === "field" ? (
        <div>
          <button type="button" className="quiet" onClick={() => set({ directions: [...draft.directions, { value: "", direction: "inbound" }] })}>
            Add mapping
          </button>
        </div>
      ) : null}
      {pick("channel", "Channel")}
    </FormDialog>
  );
}

/** Save mapping: this mapping as a named preset, published only here. */
function SaveMappingSheet({ context, recipe, onClose }: { context: () => RequestContext; recipe: MappingRecipe; onClose: () => void }) {
  const [name, setName] = useState("");
  return (
    <FormDialog
      open
      title="Save mapping"
      size="small"
      submitLabel="Save"
      submitDisabled={!validName(name)}
      dirty={name !== ""}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await saveItem({ context: context(), kind: "mapping", draft: { name: name.trim(), mapping: recipe }, intent_id: newIntentId() });
        if (answer.outcome !== "saved") return { reason: answer.problems.map((problem) => problem.problem).join(" ") || answer.reason || "The mapping was not saved.", field: "mapping-name" };
        onClose();
        return null;
      }}
    >
      <label htmlFor="mapping-name">Name</label>
      <input id="mapping-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
    </FormDialog>
  );
}
