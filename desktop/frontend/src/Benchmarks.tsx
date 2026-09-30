// Tools › Benchmarks (view 41): what was measured, newest first. A result is
// only ever what a scan actually read and timed; a stopped scan is Incomplete
// and claims nothing about the part it did not read. Generate input writes a
// synthetic input into the project and measures nothing. Both run as the
// "corpus" operation, which the sidebar's Stop cancels.
import { useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import {
  benchmarkDefaults,
  cancel,
  chooseCorpusPath,
  corpusProgress,
  generateInput,
  listBenchmarkInputs,
  listBenchmarks,
  openBenchmark,
  probeImport,
  RequestScope,
  startBenchmark,
  type BenchmarkDefaults,
  type BenchmarkInput,
  type BenchmarkRecord,
  type BenchmarkResult,
  type CorpusProgress,
  type ImportPlan,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, FrameContext, ValueRows } from "./layout";
import { useLifecycle } from "./lifecycle";
import { listDate } from "./Projects";
import { sizeText } from "./Storage";
import { useVocabulary } from "./vocabulary";

/** How often a running generation or scan is asked what it has reached. */
const PROGRESS_MS = 250;

const FRAMING: Record<string, string> = { mllp: "MLLP", batch: "Batch", raw: "Raw HL7" };
const DIRECTION: Record<string, string> = { inbound: "Inbound", outbound: "Outbound", unknown: "Unknown direction" };
const BOUNDARY: Record<string, string> = { "segment-start": "Each MSH segment", "hl7-batch": "HL7 batch (FHS/BHS)" };
const encodingText = (value: string) => (value === "unknown" ? "Unknown encoding" : value.toUpperCase());

/** A declared format, as one line. */
export function planText(plan: ImportPlan): string {
  return [FRAMING[plan.framing] ?? plan.framing, plan.terminator.toUpperCase(), encodingText(plan.encoding), DIRECTION[plan.direction] ?? plan.direction].join(" · ");
}

const count = (value: number) => value.toLocaleString();
const bytesText = (value: number) => (value < 1024 ? `${count(value)} bytes` : `${sizeText(value)} (${count(value)} bytes)`);

/** How long a complete scan took, in explicit units. */
export function durationText(ms: number | undefined): string {
  if (ms === undefined) return "—";
  return ms < 1000 ? `${count(ms)} ms` : `${(ms / 1000).toFixed(2)} s`;
}

/** Bytes and records per second, only from a complete scan's own bytes and
 * time; anything else has none. */
export function throughputText(record: BenchmarkRecord): string {
  const ms = record.elapsed_milliseconds;
  if (record.completion !== "complete" || !ms || ms <= 0) return "—";
  const seconds = ms / 1000;
  return `${sizeText(Math.round(record.bytes / seconds))}/s · ${count(Math.round(record.records / seconds))} records/s`;
}

function dateText(value: string, compact: boolean): string {
  const day = listDate(value);
  const time = new Date(value).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return compact || day !== "Today" ? day : time;
}

/** Newest first, then by identity. */
export function sortBenchmarks(results: BenchmarkRecord[]): BenchmarkRecord[] {
  return [...results].sort((a, b) => b.created_at.localeCompare(a.created_at) || a.id.localeCompare(b.id));
}

type Activity = { kind: "scan" | "generate"; progress: CorpusProgress | null };

/** The Benchmarks page: its actions, list and the selected result's details.
 * Benchmarks are the open project's, so without a project there are none. */
export function useBenchmarks({ root, shown, busy }: { root: string | null; shown: boolean; busy: boolean }) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const { compact } = useContext(FrameContext);
  const [results, setResults] = useState<BenchmarkRecord[] | null>(null);
  const [inputs, setInputs] = useState<BenchmarkInput[]>([]);
  const [defaults, setDefaults] = useState<BenchmarkDefaults | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [opened, setOpened] = useState<BenchmarkResult | null>(null);
  const [sheet, setSheet] = useState<null | { kind: "start"; input?: string } | { kind: "generate" }>(null);
  const [activity, setActivity] = useState<Activity | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [generateDraft, setGenerateDraft] = useState<GenerateDraft | null>(null);
  const lifecycle = useLifecycle<"scan" | "generate">({ window: true });

  const refresh = useCallback(async () => {
    if (!root) return;
    const [listed, generated] = await Promise.all([listBenchmarks(context()), listBenchmarkInputs(context())]);
    if (listed.state === "completed" || listed.state === "empty") {
      setResults(sortBenchmarks(listed.results));
      setFailure(null);
    } else {
      setFailure(listed.reason ?? "The benchmarks could not be read.");
    }
    if (generated.state === "completed" || generated.state === "empty") setInputs(generated.inputs);
  }, [context, root]);

  useEffect(() => {
    setResults(null);
    setInputs([]);
    setSelected(null);
    setOpened(null);
    setNotice(null);
  }, [root]);
  useEffect(() => {
    if (shown) void refresh();
  }, [shown, refresh]);
  useEffect(() => {
    if (shown && defaults === null) void benchmarkDefaults().then((answer) => answer.state === "completed" && setDefaults(answer.defaults));
  }, [shown, defaults]);
  useEffect(() => {
    if (!selected || !root) {
      setOpened(null);
      return;
    }
    let current = true;
    void openBenchmark({ context: context(), id: selected }).then((answer) => current && setOpened(answer));
    return () => {
      current = false;
    };
  }, [selected, root, context]);

  /** Runs one corpus operation, reading what it has reached until it answers. */
  const operate = async <T,>(kind: Activity["kind"], work: () => Promise<T>): Promise<T | undefined> => {
    setNotice(null);
    setActivity({ kind, progress: null });
    const poll = window.setInterval(() => {
      void corpusProgress().then((read) => {
        if (read.progress) setActivity((held) => (held ? { ...held, progress: read.progress ?? null } : held));
      });
    }, PROGRESS_MS);
    try {
      return await lifecycle.run(kind, () => work());
    } finally {
      window.clearInterval(poll);
      setActivity(null);
    }
  };

  const start = async (request: Parameters<typeof startBenchmark>[0]) => {
    setSheet(null);
    const answer = await operate("scan", () => startBenchmark(request));
    if (!answer) return;
    if (answer.result) {
      await refresh();
      setSelected(answer.result.id);
    } else if (answer.state !== "completed") {
      setNotice(answer.reason ?? "The benchmark did not run.");
    }
  };

  const generate = async (request: Parameters<typeof generateInput>[0]) => {
    setSheet(null);
    const answer = await operate("generate", () => generateInput(request));
    if (!answer) return;
    if (answer.state === "completed" && answer.input) {
      setGenerateDraft(null);
      await refresh();
      setSheet({ kind: "start", input: answer.input.id });
    } else {
      setNotice(answer.reason ?? "The input was not generated.");
    }
  };

  // A new draft captures its base time once, when it is started; a kept
  // draft reopens exactly as it was left.
  const startGenerate = async () => {
    if (!generateDraft) {
      const fresh = await benchmarkDefaults();
      if (fresh.state !== "completed") {
        setNotice(fresh.reason ?? "Generate input could not be opened.");
        return;
      }
      setDefaults(fresh.defaults);
      setGenerateDraft(newGenerateDraft(fresh.defaults));
    }
    setSheet({ kind: "generate" });
  };

  const columns: Column<BenchmarkRecord>[] = [
    {
      key: "input",
      header: "Input",
      priority: 1,
      minWidth: 15,
      flex: true,
      render: (record) => (
        <span className="case-name">
          <span>{record.input.name}</span>
          {record.completion === "incomplete" ? <span className="row-reason">Incomplete</span> : null}
        </span>
      ),
    },
    { key: "records", header: "Records", priority: 2, minWidth: 6, render: (record) => count(record.records) },
    { key: "duration", header: "Duration", priority: 3, minWidth: 7, render: (record) => durationText(record.elapsed_milliseconds) },
    { key: "peak", header: "Peak buffer", priority: 4, minWidth: 8, render: (record) => (record.peak_scan_buffer_bytes === undefined ? "—" : sizeText(record.peak_scan_buffer_bytes)) },
    { key: "date", header: "Date", priority: 5, minWidth: 8, render: (record) => dateText(record.created_at, compact) },
  ];

  let body: ReactNode;
  if (!root) {
    body = null;
  } else if (failure) {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{failure}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (results && results.length === 0 && !activity) {
    body = (
      <EmptyState
        title="No benchmarks"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => setSheet({ kind: "start" })}>
            Start benchmark
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Benchmarks"
        className="page-table"
        rows={results ?? []}
        rowId={(record) => record.id}
        rowLabel={(record) => `${record.input.name} · ${new Date(record.created_at).toLocaleString()}`}
        columns={columns}
        selected={selected}
        onSelect={setSelected}
        onOpen={setSelected}
        loading={results === null}
      />
    );
  }

  const progress = activity?.progress;
  const record = opened?.result && opened.result.id === selected ? opened.result : null;
  return {
    title: "Benchmarks",
    actions: root ? (
      <>
        <button type="button" disabled={busy} onClick={() => void startGenerate()}>
          Generate input
        </button>
        {results && results.length > 0 ? (
          <button type="button" className="primary" disabled={busy} onClick={() => setSheet({ kind: "start" })}>
            Start benchmark
          </button>
        ) : null}
      </>
    ) : null,
    body: (
      <>
        {notice ? (
          <p className="form-status" role="alert">
            {notice}
          </p>
        ) : null}
        {activity ? (
          <p className="benchmark-progress" role="status">
            {activity.kind === "scan"
              ? `Scanning · ${count(progress?.records ?? 0)} records · ${sizeText(progress?.bytes ?? 0)}`
              : `Generating · ${count(progress?.messages ?? 0)} messages · ${sizeText(progress?.bytes ?? 0)}`}
          </p>
        ) : null}
        {body}
        {sheet?.kind === "start" && defaults ? (
          <StartSheet defaults={defaults} inputs={inputs} input={sheet.input} context={context} onClose={() => setSheet(null)} onStart={(request) => void start(request)} />
        ) : null}
        {sheet?.kind === "generate" && defaults && generateDraft ? (
          <GenerateSheet
            defaults={defaults}
            draft={generateDraft}
            onDraft={(update) => setGenerateDraft((held) => (held ? update(held) : held))}
            context={context}
            onClose={() => setSheet(null)}
            onDiscard={() => setGenerateDraft(null)}
            onGenerate={(request) => void generate(request)}
          />
        ) : null}
      </>
    ),
    details: record && opened ? <BenchmarkDetails record={record} available={opened.availability ?? "available"} /> : null,
    closeDetails: () => setSelected(null),
    /** What the sidebar's operation indicator shows while one runs. */
    activity: activity ? { label: activity.kind === "scan" ? "Running benchmark" : "Generating input", stop: () => cancel("corpus") } : null,
    refresh,
  };
}

/** One result: what the scan read and measured. Missing metrics are a dash. */
export function BenchmarkDetails({ record, available }: { record: BenchmarkRecord; available: string }) {
  const complete = record.completion === "complete";
  return (
    <section className="benchmark-details" aria-label={record.input.name}>
      <h2>{record.input.name}</h2>
      <ValueRows
        rows={[
          { label: "Completion", value: complete ? "Complete" : "Incomplete" },
          { label: "Records", value: count(record.records) },
          { label: "Bytes read", value: bytesText(record.bytes) },
          { label: "Duration", value: durationText(record.elapsed_milliseconds) },
          { label: "Throughput", value: throughputText(record) },
          { label: "Peak scan buffer", value: record.peak_scan_buffer_bytes === undefined ? "—" : bytesText(record.peak_scan_buffer_bytes) },
          { label: "Batches", value: count(record.batches) },
          { label: "Records per batch", value: count(record.bounds.batch_records) },
          { label: "Bytes per batch", value: count(record.bounds.batch_bytes) },
          { label: "Format", value: planText(record.plan) },
          { label: "Date", value: new Date(record.created_at).toLocaleString() },
          ...(complete && available !== "available" ? [{ label: "Saved result", value: available === "missing" ? "Missing" : "Cannot be read" }] : []),
        ]}
      />
    </section>
  );
}

type Limits = { records: string; bytes: string; show: string; offset: string };

/** A whole number within bounds, or null. */
function whole(text: string, min: number, max: number): number | null {
  if (!/^\d+$/.test(text.trim())) return null;
  const value = Number(text.trim());
  return Number.isSafeInteger(value) && value >= min && value <= max ? value : null;
}

/** Start benchmark: one generated input or one chosen file, its format, and
 * the scan's limits at their documented defaults. */
function StartSheet({
  defaults,
  inputs,
  input,
  context,
  onClose,
  onStart,
}: {
  defaults: BenchmarkDefaults;
  inputs: BenchmarkInput[];
  input?: string | undefined;
  context: () => import("./bindings").RequestContext;
  onClose: () => void;
  onStart: (request: Parameters<typeof startBenchmark>[0]) => void;
}) {
  const available = inputs.filter((entry) => entry.availability === "available");
  const [source, setSource] = useState<string>(input ?? available[0]?.id ?? "");
  const [file, setFile] = useState<{ path: string; name: string; plan: ImportPlan | null; detected: boolean } | null>(null);
  const [formatOpen, setFormatOpen] = useState(false);
  const initial: Limits = { records: String(defaults.batch_records), bytes: String(defaults.batch_bytes), show: String(defaults.window_limit), offset: "0" };
  const [limits, setLimits] = useState<Limits>(initial);
  const [chooseFailure, setChooseFailure] = useState<string | null>(null);
  const generated = available.find((entry) => entry.id === source) ?? null;

  const choose = async () => {
    setChooseFailure(null);
    const chosen = await chooseCorpusPath("scan-file");
    if (chosen.state !== "completed" || !chosen.path) {
      if (chosen.state !== "cancelled" && chosen.state !== "empty") setChooseFailure(chosen.reason ?? "The file could not be chosen.");
      return;
    }
    const path = chosen.path;
    const name = path.split(/[\\/]/).pop() || path;
    // Only the import probe's one unambiguous, fully declared reading fills
    // the format; otherwise it stays for the person to declare.
    const probe = await probeImport({ files: [path] });
    const proposed = probe.state === "completed" && probe.selected !== null && probe.selected !== undefined ? probe.formats[probe.selected]?.plan : undefined;
    setFile({ path, name, plan: proposed ? { ...proposed, members: [] } : null, detected: proposed !== undefined });
    setSource("file");
  };

  const records = whole(limits.records, 1, defaults.max_batch_records);
  const bytes = whole(limits.bytes, 1, defaults.max_batch_bytes);
  const show = whole(limits.show, 0, defaults.max_window_limit);
  const offset = whole(limits.offset, 0, Number.MAX_SAFE_INTEGER);
  const limitProblem =
    records === null
      ? `Records per batch is a whole number from 1 to ${count(defaults.max_batch_records)}.`
      : bytes === null
        ? `Bytes per batch is a whole number from 1 to ${count(defaults.max_batch_bytes)}.`
        : show === null
          ? `Records to show is a whole number from 0 to ${defaults.max_window_limit}.`
          : offset === null
            ? "Offset is a whole number, 0 or more."
            : null;
  const ready = (source === "file" ? file !== null && file.plan !== null : generated !== null) && limitProblem === null;

  return (
    <>
      <FormDialog
        open={!formatOpen}
        title="Start benchmark"
        submitLabel="Start benchmark"
        submitDisabled={!ready}
        dirty={JSON.stringify(limits) !== JSON.stringify(initial) || file !== null}
        onClose={onClose}
        status={limitProblem ?? chooseFailure ?? undefined}
        onSubmit={() => {
          if (!ready || records === null || bytes === null || show === null || offset === null) return { reason: limitProblem ?? "Choose an input." };
          onStart({
            context: context(),
            ...(source === "file" && file ? { file: file.path, plan: file.plan! } : { input_id: source }),
            batch_records: records,
            batch_bytes: bytes,
            window_limit: show,
            window_offset: offset,
          });
          return null;
        }}
      >
        <fieldset className="checks picker">
          <legend>Input</legend>
          {available.map((entry) => (
            <label key={entry.id} className="check">
              <input type="radio" name="benchmark-input" checked={source === entry.id} onChange={() => setSource(entry.id)} />
              {entry.name}
            </label>
          ))}
          {file ? (
            <label className="check">
              <input type="radio" name="benchmark-input" checked={source === "file"} onChange={() => setSource("file")} />
              {file.name}
            </label>
          ) : null}
          <button type="button" className="quiet" onClick={() => void choose()}>
            Choose file…
          </button>
        </fieldset>
        {generated && source !== "file" ? (
          <ValueRows
            label="Input"
            rows={[
              { label: "Messages", value: count(generated.manifest.messages) },
              { label: "Size", value: bytesText(generated.manifest.bytes) },
              { label: "Format", value: planText(generated.manifest.plan) },
              { label: "Seed", value: generated.manifest.seed },
              { label: "Base time", value: generated.manifest.base_time },
              { label: "Generator", value: `${generated.manifest.generator_version} · ${generated.manifest.profile_version}` },
              { label: "Origin", value: "Synthetic" },
            ]}
          />
        ) : null}
        {source === "file" && file ? (
          <div className="format-line">
            <ValueRows rows={[{ label: "Format", value: file.plan ? `${planText(file.plan)}${file.detected ? " (detected)" : ""}` : "Not detected" }]} />
            <button type="button" onClick={() => setFormatOpen(true)}>
              {file.plan ? "Change format" : "Choose format"}
            </button>
          </div>
        ) : null}
        <fieldset>
          <legend>Limits</legend>
          <label htmlFor="benchmark-records">Records per batch</label>
          <input id="benchmark-records" inputMode="numeric" value={limits.records} onChange={(event) => setLimits({ ...limits, records: event.target.value })} />
          <label htmlFor="benchmark-bytes">Bytes per batch</label>
          <input id="benchmark-bytes" inputMode="numeric" value={limits.bytes} onChange={(event) => setLimits({ ...limits, bytes: event.target.value })} />
          <label htmlFor="benchmark-show">Records to show</label>
          <input id="benchmark-show" inputMode="numeric" value={limits.show} onChange={(event) => setLimits({ ...limits, show: event.target.value })} />
          <label htmlFor="benchmark-offset">Offset</label>
          <input id="benchmark-offset" inputMode="numeric" value={limits.offset} onChange={(event) => setLimits({ ...limits, offset: event.target.value })} />
        </fieldset>
      </FormDialog>
      {formatOpen && file ? (
        <FormatSheet
          plan={file.plan ?? { schema: "", framing: "", terminator: "", encoding: "", direction: "", members: [] } as unknown as ImportPlan}
          onClose={() => setFormatOpen(false)}
          onDone={(plan) => {
            setFile({ ...file, plan, detected: false });
            setFormatOpen(false);
          }}
        />
      ) : null}
    </>
  );
}

/** Change format: the actual declaration a chosen file is read under. Nothing
 * is normalized; the bytes are read as declared. */
function FormatSheet({ plan, onClose, onDone }: { plan: ImportPlan; onClose: () => void; onDone: (plan: ImportPlan) => void }) {
  const plans = useVocabulary()?.import_plan;
  const [draft, setDraft] = useState<ImportPlan>(plan);
  const set = (patch: Partial<ImportPlan>) => setDraft((held) => ({ ...held, ...patch }));
  const complete = !!draft.framing && !!draft.terminator && !!draft.encoding && !!draft.direction && (draft.framing !== "batch" || !!draft.batch_boundary);
  const select = (id: string, label: string, value: string, options: string[], text: (value: string) => string, change: (value: string) => void) => (
    <>
      <label htmlFor={id}>{label}</label>
      <select id={id} value={value} onChange={(event) => change(event.target.value)}>
        {value ? null : <option value="">Choose…</option>}
        {options.map((option) => (
          <option key={option} value={option}>
            {text(option)}
          </option>
        ))}
      </select>
    </>
  );
  return (
    <FormDialog
      open
      title="Change format"
      submitLabel="Done"
      submitDisabled={!complete}
      dirty={JSON.stringify(draft) !== JSON.stringify(plan)}
      onClose={onClose}
      onSubmit={() => {
        const { batch_boundary: boundary, ...rest } = draft;
        onDone({ ...rest, schema: draft.schema, members: [], ...(draft.framing === "batch" && boundary ? { batch_boundary: boundary } : {}) });
        return null;
      }}
    >
      {plans ? (
        <>
          {select("benchmark-framing", "Framing", draft.framing, plans.framings, (value) => FRAMING[value] ?? value, (value) => set({ framing: value as ImportPlan["framing"] }))}
          {select("benchmark-terminator", "Segment terminator", draft.terminator, plans.terminators, (value) => value.toUpperCase(), (value) => set({ terminator: value as ImportPlan["terminator"] }))}
          {select("benchmark-encoding", "Encoding", draft.encoding, plans.encodings, encodingText, (value) => set({ encoding: value as ImportPlan["encoding"] }))}
          {select("benchmark-direction", "Direction", draft.direction, plans.directions, (value) => (value === "unknown" ? "Unknown" : (DIRECTION[value] ?? value)), (value) => set({ direction: value as ImportPlan["direction"] }))}
          {draft.framing === "batch"
            ? select("benchmark-boundary", "Batch boundary", draft.batch_boundary ?? "", plans.boundaries, (value) => BOUNDARY[value] ?? value, (value) => set({ batch_boundary: value as ImportPlan["batch_boundary"] & string }))
            : null}
        </>
      ) : null}
    </FormDialog>
  );
}

/** Generate input: a named synthetic input from the generator's own choices.
 * The seed and base time are captured once and kept while the draft is
 * edited; nothing is imported, sent or measured. */
/** A Generate input draft: kept while the sheet is closed, so its seed and
 * base time are never taken again until it is generated or discarded. */
export type GenerateDraft = { name: string; messages: string; seed: string; baseTime: string; plan: ImportPlan };

/** A new draft from the defaults read when it is started. */
export function newGenerateDraft(defaults: BenchmarkDefaults): GenerateDraft {
  return { name: "", messages: "", seed: defaults.seed, baseTime: defaults.base_time, plan: defaults.plan };
}

function GenerateSheet({
  defaults,
  draft,
  onDraft,
  context,
  onClose,
  onDiscard,
  onGenerate,
}: {
  defaults: BenchmarkDefaults;
  draft: GenerateDraft;
  onDraft: (update: (held: GenerateDraft) => GenerateDraft) => void;
  context: () => import("./bindings").RequestContext;
  onClose: () => void;
  onDiscard: () => void;
  onGenerate: (request: Parameters<typeof generateInput>[0]) => void;
}) {
  const initial = newGenerateDraft(defaults);
  const set = (patch: Partial<GenerateDraft>) => onDraft((held) => ({ ...held, ...patch }));
  const setPlan = (patch: Partial<ImportPlan>) => onDraft((held) => ({ ...held, plan: { ...held.plan, ...patch } }));
  const messages = whole(draft.messages, 1, defaults.max_messages);
  const problem = !draft.name.trim()
    ? null
    : messages === null && draft.messages.trim() !== ""
      ? `Message count is a whole number from 1 to ${count(defaults.max_messages)}.`
      : !/^\d+$/.test(draft.seed.trim())
        ? "Seed is a whole number."
        : null;
  const ready = draft.name.trim() !== "" && messages !== null && problem === null;
  const choice = (id: string, label: string, value: string, options: string[], text: (value: string) => string, change: (value: string) => void) => (
    <>
      <label htmlFor={id}>{label}</label>
      <select id={id} value={value} onChange={(event) => change(event.target.value)}>
        {options.includes(value) ? null : <option value="">Choose…</option>}
        {options.map((option) => (
          <option key={option} value={option}>
            {text(option)}
          </option>
        ))}
      </select>
    </>
  );
  return (
    <FormDialog
      open
      title="Generate input"
      submitLabel="Generate input"
      submitDisabled={!ready}
      dirty={JSON.stringify({ ...draft, baseTime: "" }) !== JSON.stringify({ ...initial, baseTime: "" })}
      onClose={onClose}
      onDiscard={onDiscard}
      status={problem ?? undefined}
      onSubmit={() => {
        if (!ready || messages === null) return { reason: problem ?? "Name the input and give its message count." };
        const { batch_boundary: boundary, ...plan } = draft.plan;
        onGenerate({
          context: context(),
          name: draft.name.trim(),
          seed: draft.seed.trim(),
          base_time: draft.baseTime,
          generator_version: defaults.generator_version,
          profile_version: defaults.profile_version,
          messages,
          plan: { ...plan, members: [], ...(plan.framing === "batch" && boundary ? { batch_boundary: boundary } : {}) },
        });
        return null;
      }}
    >
      <label htmlFor="generate-name">Name</label>
      <input id="generate-name" type="text" maxLength={200} data-autofocus value={draft.name} onChange={(event) => set({ name: event.target.value })} />
      <label htmlFor="generate-messages">Message count</label>
      <input id="generate-messages" inputMode="numeric" value={draft.messages} onChange={(event) => set({ messages: event.target.value })} />
      <ValueRows rows={[{ label: "Profile", value: defaults.profile_version }]} />
      {choice("generate-framing", "Framing", draft.plan.framing, defaults.framings, (value) => FRAMING[value] ?? value, (value) => setPlan({ framing: value as ImportPlan["framing"] }))}
      {draft.plan.framing === "batch"
        ? choice("generate-boundary", "Batch boundary", draft.plan.batch_boundary ?? "", defaults.batch_boundaries, (value) => BOUNDARY[value] ?? value, (value) => setPlan({ batch_boundary: value as ImportPlan["batch_boundary"] & string }))
        : null}
      <fieldset>
        <legend>Generation settings</legend>
        <label htmlFor="generate-seed">Seed</label>
        <input id="generate-seed" inputMode="numeric" value={draft.seed} onChange={(event) => set({ seed: event.target.value })} />
        <label htmlFor="generate-base-time">Base time</label>
        <input id="generate-base-time" type="text" value={draft.baseTime} onChange={(event) => set({ baseTime: event.target.value })} />
        <ValueRows rows={[{ label: "Generator", value: defaults.generator_version }]} />
        {choice("generate-terminator", "Terminator", draft.plan.terminator, defaults.terminators, (value) => value.toUpperCase(), (value) => setPlan({ terminator: value as ImportPlan["terminator"] }))}
        {choice("generate-encoding", "Encoding", draft.plan.encoding, defaults.encodings, encodingText, (value) => setPlan({ encoding: value as ImportPlan["encoding"] }))}
        {choice("generate-direction", "Direction", draft.plan.direction, defaults.directions, (value) => (value === "unknown" ? "Unknown" : (DIRECTION[value] ?? value)), (value) => setPlan({ direction: value as ImportPlan["direction"] }))}
      </fieldset>
    </FormDialog>
  );
}
