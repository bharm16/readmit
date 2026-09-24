import { useEffect, useRef, useState } from "react";
import "./raw.css";
import {
  chooseInspectionPath,
  inspectRawFile,
  writeRoundTrip,
  type InspectFormat,
  type InspectTerminator,
  type InspectionRow,
  type RawInspectionResult,
  type RoundTripResult,
} from "./bindings";
import { Report, type Indicators } from "./shell";
import { useLifecycle } from "./lifecycle";


/** One row as a line: the same facts `readmit inspect` prints, in its order. */
function RowLine({ row, selection }: { row: InspectionRow; selection: string }) {
  switch (row.kind) {
    case "message":
      return (
        <>
          Message {row.message} · terminator {row.terminator} ({selection}) · bytes [{row.start},{row.end}) ·{" "}
          {row.profile}
        </>
      );
    case "segment":
      return (
        <>
          {row.segment} · bytes [{row.start},{row.end})
        </>
      );
    case "field":
      return (
        <>
          {row.segment}-{row.field}
          {row.label ? ` ${row.label}` : ""} · {row.state}
          {row.state !== "omitted" ? ` · ${row.end - row.start} bytes` : ""}
          {row.value !== undefined ? (
            <>
              {" "}
              · <code className="raw-value">{row.value}</code>
              {row.value_truncated ? ` · shown in part: the first ${row.value_shown_bytes} of ${row.end - row.start} bytes` : ""}
            </>
          ) : null}
        </>
      );
    case "repetition":
      return (
        <>
          repetition {row.repetition} · {row.state} · {row.end - row.start} bytes
        </>
      );
  }
}

/** Raw inspection: `readmit inspect` in the window. A person chooses one file
 * through the host's dialog, declares its framing and terminator, and sees
 * every message, segment and field the command reports, a bounded window of
 * rows at a time. The file is read, never imported or changed; values are
 * shown only on request and arrive escaped by Go. A byte-identical copy is
 * written only to a new file in a folder the person chose. */
export function RawInspection({
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
  const [file, setFile] = useState("");
  const [format, setFormat] = useState<InspectFormat>("auto");
  const [terminator, setTerminator] = useState<InspectTerminator>("auto");
  const [showValues, setShowValues] = useState(false);
  const [result, setResult] = useState<RawInspectionResult | null>(null);
  // A dialog that chose nothing is answered beside the control that opened it.
  const [chosen, setChosen] = useState<{ state: RawInspectionResult["state"]; reason?: string | undefined } | null>(null);
  const [copyChosen, setCopyChosen] = useState<{ state: RawInspectionResult["state"]; reason?: string | undefined } | null>(
    null,
  );
  const [folder, setFolder] = useState("");
  const [name, setName] = useState("");
  const [copied, setCopied] = useState<RoundTripResult | null>(null);
  // This screen's calls hold the window's one slot, so the rest of the window
  // is unavailable meanwhile rather than answered busy.
  const { running: working, run } = useLifecycle<"choosing" | "inspecting" | "copying">({ window: true });
  const disabled = busy || working !== null;

  useEffect(() => {
    if (request > 0) {
      setOpen(true);
      toggle.current?.focus();
    }
  }, [request]);

  // Whatever was shown belongs to the file and declarations it was read under.
  const invalidate = () => {
    setResult(null);
    setCopied(null);
  };

  async function choose(kind: "file" | "round-trip-folder") {
    const report = kind === "file" ? setChosen : setCopyChosen;
    await run("choosing", async () => {
      report(null);
      const answer = await chooseInspectionPath(kind);
      if (answer.state === "completed" && answer.path) {
        if (kind === "file") {
          setFile(answer.path);
          invalidate();
        } else {
          setFolder(answer.path);
          setCopied(null);
        }
      } else {
        report({ state: answer.state, reason: answer.reason });
      }
    });
  }

  /** Reads one page. A later page names the digest the shown rows were read
   * from, so a file that changed between pages is refused, not mixed. */
  async function inspect(offset: number, expect?: string) {
    await run("inspecting", async () => {
      setChosen(null);
      setResult(
        await inspectRawFile({
          file,
          format,
          terminator,
          show_values: showValues,
          offset,
          // Zero asks for the facade's own window bound, which the answer names.
          limit: 0,
          ...(expect ? { expect } : {}),
        }),
      );
    });
  }

  async function copy() {
    await run("copying", async () => {
      setCopyChosen(null);
      setCopied(await writeRoundTrip({ file, format, terminator, folder, name: name.trim() }));
    });
  }

  const view = result?.inspection;
  const last = view ? view.offset + view.rows.length : 0;
  return (
    <section className="raw-panel" aria-labelledby="raw-inspection-title">
      <h3 id="raw-inspection-title">
        <button
          ref={toggle}
          type="button"
          aria-expanded={open}
          aria-controls="raw-inspection-body"
          onClick={() => setOpen(!open)}
        >
          Inspect a raw HL7 file
        </button>
      </h3>
      {open ? (
        <div id="raw-inspection-body">
          <p className="hint">
            Reads one file the way <code>readmit inspect</code> does and shows its syntax. Nothing is imported,
            no case is written, and the file is never changed.
          </p>
          <div className="raw-choice">
            <button type="button" disabled={disabled} onClick={() => void choose("file")}>
              Choose a file to inspect…
            </button>
            <span className="raw-path">{file || "No file chosen."}</span>
          </div>
          <fieldset disabled={disabled}>
            <legend>Declarations</legend>
            <label>
              Framing
              <select
                value={format}
                onChange={(event) => {
                  setFormat(event.target.value as InspectFormat);
                  invalidate();
                }}
              >
                <option value="auto">Detect (auto)</option>
                <option value="raw">raw</option>
                <option value="mllp">mllp</option>
              </select>
            </label>
            <label>
              Segment terminator
              <select
                value={terminator}
                onChange={(event) => {
                  setTerminator(event.target.value as InspectTerminator);
                  invalidate();
                }}
              >
                <option value="auto">Detect (auto)</option>
                <option value="cr">cr</option>
                <option value="lf">lf</option>
                <option value="crlf">crlf</option>
              </select>
            </label>
            <label className="raw-check">
              <input
                type="checkbox"
                checked={showValues}
                onChange={(event) => {
                  setShowValues(event.target.checked);
                  invalidate();
                }}
              />
              Show field values as escaped byte strings (may contain patient data)
            </label>
          </fieldset>
          <button type="button" disabled={disabled || !file} onClick={() => void inspect(0)}>
            Inspect
          </button>
          <Report
            indicators={indicators}
            progress={working === "inspecting" ? "Reading and parsing the file." : null}
            result={chosen ?? (result && result.state !== "completed" ? result : null)}
          />
          {!result && !chosen && working !== "inspecting" ? (
            <p className="hint">Nothing has been read yet.</p>
          ) : null}
          {view ? (
            <>
              <p className="raw-summary" role="status">
                Format: {view.format} ({view.format_selection}) · Messages: {view.messages} · {view.bytes} bytes ·
                SHA-256 {view.sha256}
              </p>
              {!view.show_values ? (
                <p className="hint">Values are hidden. Show them and inspect again to see each field's bytes.</p>
              ) : null}
              <ol className="raw-rows" aria-label="Inspection rows" start={view.offset + 1}>
                {view.rows.map((row, index) => (
                  <li key={view.offset + index} className={`raw-row raw-row-${row.kind}`}>
                    <RowLine row={row} selection={view.terminator_selection} />
                  </li>
                ))}
              </ol>
              <div className="raw-paging">
                <button type="button" disabled={disabled || view.offset === 0} onClick={() => void inspect(Math.max(0, view.offset - view.limit), view.sha256)}>
                  Previous rows
                </button>
                <span>
                  Rows {view.rows.length === 0 ? 0 : view.offset + 1}–{last} of {view.total}
                </span>
                <button type="button" disabled={disabled || last >= view.total} onClick={() => void inspect(last, view.sha256)}>
                  Next rows
                </button>
              </div>
            </>
          ) : null}
          <fieldset disabled={disabled} className="raw-roundtrip">
            <legend>Byte-identical copy</legend>
            <p className="hint">
              Writes the file's exact bytes to a new file, as <code>readmit inspect --roundtrip</code> does, once
              it parses under the declarations above. An existing file is never overwritten.
            </p>
            <div className="raw-choice">
              <button type="button" onClick={() => void choose("round-trip-folder")}>
                Choose a folder for the copy…
              </button>
              <span className="raw-path">{folder || "No folder chosen."}</span>
            </div>
            <label>
              New file name
              <input
                value={name}
                onChange={(event) => {
                  setName(event.target.value);
                  setCopied(null);
                }}
              />
            </label>
            <button type="button" disabled={!file || !folder || !name.trim()} onClick={() => void copy()}>
              Write the copy
            </button>
          </fieldset>
          <Report
            indicators={indicators}
            progress={working === "copying" ? "Writing the copy." : null}
            result={copyChosen ?? (copied && copied.state !== "completed" ? copied : null)}
          />
          {copied?.state === "completed" ? (
            <p className="raw-summary" role="status">
              Wrote {copied.bytes} bytes to {copied.path} · SHA-256 {copied.sha256}
              {view && view.sha256 === copied.sha256 ? " · the same bytes the inspection read" : ""}
            </p>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
