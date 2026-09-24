import { useEffect, useRef, useState } from "react";
import "./raw.css";
import {
  chooseCorpusPath,
  corpusProgress,
  generateCorpus,
  scanCorpus,
  type BundleDirection,
  type CorpusGenerateResult,
  type CorpusPathKind,
  type CorpusProgress,
  type CorpusScanResult,
  type CorpusScanView,
  type HL7Terminator,
  type ImportBoundary,
  type ImportEncoding,
  type ImportFraming,
  type ImportPlan,
  type State,
} from "./bindings";
import { Report, type Indicators } from "./shell";
import { useLifecycle } from "./lifecycle";

/** How often a running generation or scan is asked what it has reached. */
const PROGRESS_MS = 250;

/** The one generator and fixture profile this release implements. */
const GENERATOR_VERSION = "readmit-corpus-v1";
const PROFILE_VERSION = "readmit-siu-v1";

/** The declarations a plan is made of, as the structured controls hold them.
 * Nothing is preselected: like the command, the screen has no likely value. */
interface Declared {
  framing: string;
  boundary: string;
  terminator: string;
  encoding: string;
  direction: string;
}

const undeclared: Declared = { framing: "", boundary: "", terminator: "", encoding: "", direction: "" };

function complete(declared: Declared): boolean {
  return (
    declared.framing !== "" &&
    declared.terminator !== "" &&
    declared.encoding !== "" &&
    declared.direction !== "" &&
    (declared.framing !== "batch" || declared.boundary !== "")
  );
}

// A complete declaration holds only the values its choices offer, which are
// the import plan's own.
function planOf(declared: Declared): ImportPlan {
  const plan: ImportPlan = {
    schema: "readmit-import-plan/v1",
    framing: declared.framing as ImportFraming,
    terminator: declared.terminator as HL7Terminator,
    encoding: declared.encoding as ImportEncoding,
    direction: declared.direction as BundleDirection,
    members: [],
  };
  if (declared.framing === "batch") {
    plan.batch_boundary = declared.boundary as ImportBoundary;
  }
  return plan;
}

/** The choices one plan control offers. Generation offers what a corpus can be
 * written with; a scan offers everything an import plan declares. */
interface Choices {
  framing: string[];
  boundary: string[];
  terminator: string[];
  encoding: string[];
  direction: string[];
}

const generationChoices: Choices = {
  framing: ["mllp", "batch"],
  boundary: ["segment-start"],
  terminator: ["cr"],
  encoding: ["us-ascii", "utf-8"],
  direction: ["inbound", "outbound", "unknown"],
};

const scanChoices: Choices = {
  framing: ["raw", "mllp", "batch"],
  boundary: ["segment-start", "hl7-batch"],
  terminator: ["cr", "lf", "crlf"],
  encoding: ["utf-8", "us-ascii", "iso-8859-1", "unknown"],
  direction: ["inbound", "outbound", "unknown"],
};

function Choice({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: string[];
  onChange: (value: string) => void;
}) {
  return (
    <label>
      {label}
      <select value={value} onChange={(event) => onChange(event.target.value)}>
        <option value="">Choose…</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  );
}

function PlanControls({
  legend,
  declared,
  choices,
  disabled,
  onChange,
}: {
  legend: string;
  declared: Declared;
  choices: Choices;
  disabled: boolean;
  onChange: (declared: Declared) => void;
}) {
  const set = (member: keyof Declared) => (value: string) => onChange({ ...declared, [member]: value });
  return (
    <fieldset disabled={disabled}>
      <legend>{legend}</legend>
      <Choice label="Framing" value={declared.framing} options={choices.framing} onChange={set("framing")} />
      {declared.framing === "batch" ? (
        <Choice label="Batch boundary" value={declared.boundary} options={choices.boundary} onChange={set("boundary")} />
      ) : null}
      <Choice label="Segment terminator" value={declared.terminator} options={choices.terminator} onChange={set("terminator")} />
      <Choice label="Encoding" value={declared.encoding} options={choices.encoding} onChange={set("encoding")} />
      <Choice label="Direction" value={declared.direction} options={choices.direction} onChange={set("direction")} />
    </fieldset>
  );
}

/** A whole number a person typed, or null when the field holds anything else.
 * Only plain decimal digits without a leading zero are read — the command
 * reads 010 as octal — and only up to the largest number a JavaScript number
 * holds exactly, so what is sent is what was typed. */
function whole(text: string): number | null {
  const typed = text.trim();
  if (!/^(0|[1-9][0-9]*)$/.test(typed)) {
    return null;
  }
  const value = Number(typed);
  return Number.isSafeInteger(value) ? value : null;
}

function ProgressLine({ progress }: { progress: CorpusProgress | null }) {
  if (!progress) {
    return null;
  }
  return (
    <p className="raw-progress" role="status" aria-live="polite">
      {progress.operation === "generate"
        ? `Generated so far: ${progress.messages} messages, ${progress.bytes} bytes`
        : `Scanned so far: ${progress.bytes} bytes, ${progress.records} records, ${progress.occurrences} occurrences, ${progress.batches} parsing batches`}
    </p>
  );
}

function ScanReport({ scan, state }: { scan: CorpusScanView; state: State }) {
  return (
    <div className="raw-scan" aria-label="Scan report">
      <dl className="raw-facts">
        <dt>State</dt>
        <dd>{state === "cancelled" ? "cancelled" : "completed"}</dd>
        <dt>Plan</dt>
        <dd>
          {scan.plan.schema} · framing {scan.plan.framing} · batch boundary {scan.plan.batch_boundary || "not declared"} ·
          terminator {scan.plan.terminator} · encoding {scan.plan.encoding} · direction {scan.plan.direction}
        </dd>
        <dt>Bytes read</dt>
        <dd>{scan.bytes}</dd>
        <dt>Digest</dt>
        <dd>{scan.sha256 ?? "none"}</dd>
        <dt>Records</dt>
        <dd>{scan.records}</dd>
        <dt>Occurrences</dt>
        <dd>{scan.occurrences}</dd>
        <dt>Decoded</dt>
        <dd>{scan.decoded}</dd>
        <dt>Undecodable</dt>
        <dd>{scan.undecodable}</dd>
        <dt>Parsing batches</dt>
        <dd>{scan.batches}</dd>
        <dt>Batch bounds</dt>
        <dd>
          {scan.bounds.batch_records} records, {scan.bounds.batch_bytes} bytes
        </dd>
        <dt>Peak resident bytes</dt>
        <dd>
          {scan.peak_resident_bytes} of a resident bound of {scan.bounds.resident_bound}
        </dd>
        <dt>Case bounds</dt>
        <dd>
          {scan.case_bounds === "not-evaluated"
            ? "not evaluated; the scan was cancelled"
            : scan.case_bounds === "within"
              ? "within"
              : `exceeded ${(scan.exceeded ?? []).join(", ")}`}
        </dd>
        <dt>Elapsed</dt>
        <dd>{scan.elapsed_milliseconds} ms</dd>
        <dt>Targets</dt>
        <dd>{scan.targets}</dd>
      </dl>
      {scan.window_limit === 0 ? (
        <p className="hint">No window of records was requested.</p>
      ) : (
        <table className="raw-window">
          <caption>
            {scan.rows.length} records from offset {scan.window_offset} of {scan.records}
          </caption>
          <thead>
            <tr>
              <th scope="col">Record</th>
              <th scope="col">Offset</th>
              <th scope="col">Size</th>
              <th scope="col">Occurrences</th>
              <th scope="col">Decoded</th>
              <th scope="col">Undecodable</th>
            </tr>
          </thead>
          <tbody>
            {scan.rows.map((row) => (
              <tr key={row.ordinal}>
                <th scope="row">{row.ordinal}</th>
                <td>{row.offset}</td>
                <td>{row.size}</td>
                <td>{row.occurrences}</td>
                <td>{row.decoded}</td>
                <td>{row.undecodable}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

/** The performance corpus: `readmit corpus generate` and `readmit corpus scan`
 * in the window. Generation writes a declared corpus and its manifest to two
 * new files of a natively chosen folder; a scan streams one declared file in
 * bounded batches and reports what it holds, and writes the benchmark the
 * command writes only when it was asked for and the scan completed. Both run
 * in Go through the operations the command line uses; the window sends
 * declarations and shows counts, and can cancel either while it runs. */
export function PerformanceCorpus({
  busy,
  indicators,
  request,
}: {
  busy: boolean;
  indicators: Indicators;
  /** Counts the palette's requests to open this screen. */
  request: number;
}) {
  const [open, setOpen] = useState(false);
  const toggle = useRef<HTMLButtonElement>(null);
  // This screen's calls hold the window's one slot, so the rest of the window
  // is unavailable meanwhile rather than answered busy; generation and a scan
  // both run under the facade's corpus operation, which its cancel stops.
  const { running, run, cancel: stop } = useLifecycle<"choosing" | "generate" | "scan">({
    window: true,
    names: { generate: "corpus", scan: "corpus" },
  });
  const [progress, setProgress] = useState<CorpusProgress | null>(null);
  const [feedback, setFeedback] = useState<{ state: State; reason?: string | undefined } | null>(null);

  const [seed, setSeed] = useState("");
  const [baseTime, setBaseTime] = useState("");
  const [generator, setGenerator] = useState("");
  const [profile, setProfile] = useState("");
  const [messages, setMessages] = useState("");
  const [generation, setGeneration] = useState<Declared>(undeclared);
  const [folder, setFolder] = useState("");
  const [corpusName, setCorpusName] = useState("corpus.mllp");
  const [manifestName, setManifestName] = useState("corpus.json");
  const [generated, setGenerated] = useState<CorpusGenerateResult | null>(null);

  const [stream, setStream] = useState("");
  const [scanning, setScanning] = useState<Declared>(undeclared);
  const [batchRecords, setBatchRecords] = useState("");
  const [batchBytes, setBatchBytes] = useState("");
  const [windowOffset, setWindowOffset] = useState("0");
  const [windowLimit, setWindowLimit] = useState("20");
  const [benchmark, setBenchmark] = useState(false);
  const [reportFolder, setReportFolder] = useState("");
  const [reportName, setReportName] = useState("benchmark.json");
  const [scanned, setScanned] = useState<CorpusScanResult | null>(null);

  const disabled = busy || running !== null;

  // What was shown belongs to the declarations and files it was produced
  // from, so changing any of them clears it rather than leaving it beside
  // choices it does not describe.
  const forGeneration =
    <T,>(set: (value: T) => void) =>
    (value: T) => {
      set(value);
      setGenerated(null);
    };
  const forScan =
    <T,>(set: (value: T) => void) =>
    (value: T) => {
      set(value);
      setScanned(null);
    };

  useEffect(() => {
    if (request > 0) {
      setOpen(true);
      toggle.current?.focus();
    }
  }, [request]);

  // While a generation or scan runs, the screen reads what it has reached. The
  // read never waits for the operation, and it stops when the operation ends.
  useEffect(() => {
    if (running !== "generate" && running !== "scan") {
      return;
    }
    let stopped = false;
    const poll = async () => {
      const answer = await corpusProgress();
      if (!stopped && answer.state === "completed" && answer.progress) {
        setProgress(answer.progress);
      }
    };
    void poll();
    const timer = setInterval(() => void poll(), PROGRESS_MS);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [running]);

  async function choose(kind: CorpusPathKind, set: (path: string) => void) {
    await run("choosing", async () => {
      setFeedback(null);
      const answer = await chooseCorpusPath(kind);
      if (answer.state === "completed" && answer.path) {
        set(answer.path);
      } else {
        setFeedback({ state: answer.state, reason: answer.reason });
      }
    });
  }

  async function generate() {
    await run("generate", async () => {
      setFeedback(null);
      setGenerated(null);
      setProgress(null);
      try {
        setGenerated(
          await generateCorpus({
            seed: seed.trim(),
            base_time: baseTime.trim(),
            generator_version: generator,
            profile_version: profile,
            messages: whole(messages) ?? 0,
            plan: planOf(generation),
            folder,
            corpus_name: corpusName.trim(),
            manifest_name: manifestName.trim(),
          }),
        );
      } finally {
        // The progress read stops with the operation it reads.
        setProgress(null);
      }
    });
  }

  async function scan() {
    await run("scan", async () => {
      setFeedback(null);
      setScanned(null);
      setProgress(null);
      try {
        setScanned(
          await scanCorpus({
            file: stream,
            plan: planOf(scanning),
            batch_records: whole(batchRecords) ?? 0,
            batch_bytes: whole(batchBytes) ?? 0,
            window_offset: whole(windowOffset) ?? 0,
            window_limit: whole(windowLimit) ?? 0,
            ...(benchmark ? { report_folder: reportFolder, report_name: reportName.trim() } : {}),
          }),
        );
      } finally {
        // The progress read stops with the operation it reads.
        setProgress(null);
      }
    });
  }

  const numbersValid =
    [batchRecords, batchBytes].every((text) => text.trim() === "" || whole(text) !== null) &&
    whole(windowOffset) !== null &&
    whole(windowLimit) !== null;
  const canGenerate =
    // A seed is sent as typed and read as the command reads it, so only plain
    // decimal digits are offered: 0123 would be octal there.
    /^(0|[1-9][0-9]*)$/.test(seed.trim()) &&
    baseTime.trim() !== "" &&
    generator !== "" &&
    profile !== "" &&
    whole(messages) !== null &&
    complete(generation) &&
    folder !== "" &&
    corpusName.trim() !== "" &&
    manifestName.trim() !== "";
  const canScan =
    stream !== "" && complete(scanning) && numbersValid && (!benchmark || (reportFolder !== "" && reportName.trim() !== ""));
  const manifest = generated?.manifest;

  return (
    <section className="raw-panel" aria-labelledby="performance-corpus-title">
      <h3 id="performance-corpus-title">
        <button
          ref={toggle}
          type="button"
          aria-expanded={open}
          aria-controls="performance-corpus-body"
          onClick={() => setOpen(!open)}
        >
          Generate or scan a performance corpus
        </button>
      </h3>
      {open ? (
        <div id="performance-corpus-body">
          <p className="hint">
            Streams files far larger than a case may hold without ever holding one: what a scan keeps is one read
            window, one record and one parsing batch, whatever the file's length. Nothing is imported and no case
            is written.
          </p>
          <Report indicators={indicators} progress={running === "choosing" ? "Waiting for the dialog." : null} result={feedback} />

          <section aria-labelledby="corpus-generate-title" className="raw-part">
            <h4 id="corpus-generate-title">Generate a corpus</h4>
            <fieldset disabled={disabled}>
              <legend>Generator inputs</legend>
              <label>
                Seed
                <input inputMode="numeric" value={seed} onChange={(event) => forGeneration(setSeed)(event.target.value)} />
              </label>
              <label>
                Base time (whole-second RFC 3339 with a time zone)
                <input
                  value={baseTime}
                  placeholder="2026-01-02T03:04:05Z"
                  onChange={(event) => forGeneration(setBaseTime)(event.target.value)}
                />
              </label>
              <Choice label="Generator version" value={generator} options={[GENERATOR_VERSION]} onChange={forGeneration(setGenerator)} />
              <Choice label="Profile version" value={profile} options={[PROFILE_VERSION]} onChange={forGeneration(setProfile)} />
              <label>
                Messages
                <input inputMode="numeric" value={messages} onChange={(event) => forGeneration(setMessages)(event.target.value)} />
              </label>
            </fieldset>
            <PlanControls
              legend="How the corpus is framed"
              declared={generation}
              choices={generationChoices}
              disabled={disabled}
              onChange={forGeneration(setGeneration)}
            />
            <fieldset disabled={disabled}>
              <legend>New files</legend>
              <div className="raw-choice">
                <button type="button" onClick={() => void choose("corpus-folder", forGeneration(setFolder))}>
                  Choose a folder for the corpus…
                </button>
                <span className="raw-path">{folder || "No folder chosen."}</span>
              </div>
              <label>
                Corpus file name
                <input value={corpusName} onChange={(event) => forGeneration(setCorpusName)(event.target.value)} />
              </label>
              <label>
                Manifest file name
                <input value={manifestName} onChange={(event) => forGeneration(setManifestName)(event.target.value)} />
              </label>
            </fieldset>
            <div className="raw-actions">
              <button type="button" disabled={disabled || !canGenerate} onClick={() => void generate()}>
                Generate corpus
              </button>
              {running === "generate" ? (
                <button type="button" onClick={stop}>
                  Cancel generation
                </button>
              ) : null}
            </div>
            {running === "generate" ? <ProgressLine progress={progress} /> : null}
            <Report
              indicators={indicators}
              progress={running === "generate" ? "Generating the corpus." : null}
              result={generated && generated.state !== "completed" ? generated : null}
            />
            {generated?.state === "completed" && manifest ? (
              <dl className="raw-facts" aria-label="Written corpus">
                <dt>Corpus</dt>
                <dd>{generated.corpus}</dd>
                <dt>Manifest</dt>
                <dd>
                  {generated.manifest_path} ({manifest.schema})
                </dd>
                <dt>Generator</dt>
                <dd>{manifest.generator_version}</dd>
                <dt>Profile</dt>
                <dd>{manifest.profile_version}</dd>
                <dt>Seed</dt>
                <dd>{manifest.seed}</dd>
                <dt>Base time</dt>
                <dd>{manifest.base_time}</dd>
                <dt>Plan</dt>
                <dd>
                  framing {manifest.plan.framing} · terminator {manifest.plan.terminator} · encoding{" "}
                  {manifest.plan.encoding} · direction {manifest.plan.direction}
                </dd>
                <dt>Messages</dt>
                <dd>{manifest.messages}</dd>
                <dt>Bytes</dt>
                <dd>{manifest.bytes}</dd>
                <dt>Digest</dt>
                <dd>{manifest.sha256}</dd>
              </dl>
            ) : null}
          </section>

          <section aria-labelledby="corpus-scan-title" className="raw-part">
            <h4 id="corpus-scan-title">Scan a stream</h4>
            <div className="raw-choice">
              <button type="button" disabled={disabled} onClick={() => void choose("scan-file", forScan(setStream))}>
                Choose a stream to scan…
              </button>
              <span className="raw-path">{stream || "No stream chosen."}</span>
            </div>
            <PlanControls
              legend="How the stream divides"
              declared={scanning}
              choices={scanChoices}
              disabled={disabled}
              onChange={forScan(setScanning)}
            />
            <fieldset disabled={disabled}>
              <legend>Bounds and window</legend>
              <label>
                Records per parsing batch (empty for 256)
                <input inputMode="numeric" value={batchRecords} onChange={(event) => forScan(setBatchRecords)(event.target.value)} />
              </label>
              <label>
                Bytes per parsing batch (empty for 8388608)
                <input inputMode="numeric" value={batchBytes} onChange={(event) => forScan(setBatchBytes)(event.target.value)} />
              </label>
              <label>
                Window offset
                <input inputMode="numeric" value={windowOffset} onChange={(event) => forScan(setWindowOffset)(event.target.value)} />
              </label>
              <label>
                Window records (0 for counts only, at most 200)
                <input inputMode="numeric" value={windowLimit} onChange={(event) => forScan(setWindowLimit)(event.target.value)} />
              </label>
            </fieldset>
            <fieldset disabled={disabled}>
              <legend>Benchmark</legend>
              <label className="raw-check">
                <input type="checkbox" checked={benchmark} onChange={(event) => forScan(setBenchmark)(event.target.checked)} />
                Write a readmit-benchmark/v1 document when the scan completes
              </label>
              {benchmark ? (
                <>
                  <div className="raw-choice">
                    <button type="button" onClick={() => void choose("benchmark-folder", forScan(setReportFolder))}>
                      Choose a folder for the benchmark…
                    </button>
                    <span className="raw-path">{reportFolder || "No folder chosen."}</span>
                  </div>
                  <label>
                    Benchmark file name
                    <input value={reportName} onChange={(event) => forScan(setReportName)(event.target.value)} />
                  </label>
                </>
              ) : null}
            </fieldset>
            <div className="raw-actions">
              <button type="button" disabled={disabled || !canScan} onClick={() => void scan()}>
                Scan stream
              </button>
              {running === "scan" ? (
                <button type="button" onClick={stop}>
                  Cancel scan
                </button>
              ) : null}
            </div>
            {running === "scan" ? <ProgressLine progress={progress} /> : null}
            <Report
              indicators={indicators}
              progress={running === "scan" ? "Scanning the stream." : null}
              result={scanned && scanned.state !== "completed" ? scanned : null}
            />
            {scanned?.scan ? <ScanReport scan={scanned.scan} state={scanned.state} /> : null}
            {scanned?.benchmark ? (
              <p className="raw-summary" role="status">
                Benchmark written to {scanned.benchmark}
              </p>
            ) : null}
          </section>
        </div>
      ) : null}
    </section>
  );
}
