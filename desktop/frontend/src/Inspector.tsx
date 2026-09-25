import { useState } from "react";
import type { InspectionResult } from "./bindings";
import { Report, type Indicators } from "./shell";
import "./inspector.css";

/** This view never parses a message, computes a byte span or decodes text.
 * Every displayed value and selectable path came from the same verified read.
 * Selecting a node atomically replaces its metadata, decoded value and byte
 * window. No evidence is persisted in browser state or sent elsewhere. */
export function Inspector({
  result,
  busy,
  progress,
  indicators,
  onInspect,
}: {
  result: InspectionResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onInspect: (path: string, nodeOffset: number, byteOffset: number) => void;
}) {
  const [selector, setSelector] = useState("");
  const view = result?.inspection;
  return (
    <section className="message-inspector" aria-label="Message inspector">
      <h3>Message inspector</h3>
      <p className="hint">
        Selecting an occurrence reveals local evidence. Values and byte controls are escaped;
        nothing here is saved.
      </p>
      <Report indicators={indicators} progress={progress} result={result} />
      {view ? (
        <>
          <p>
            Occurrence {view.occurrence} · Source {view.source_id} · Source offset{" "}
            {view.source_offset} · {view.size} original bytes
          </p>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              onInspect(selector, 0, -1);
            }}
          >
            <label htmlFor="inspect-selector">Field path</label>
            <input
              id="inspect-selector"
              placeholder="PID[1]-3[2].4.1"
              value={selector}
              onChange={(event) => setSelector(event.target.value)}
              disabled={busy || view.decode_state === "unparsed"}
            />
            <p className="hint">
              Names an exact field, repetition, component or subcomponent, e.g. PID[1]-3[2].4.1
            </p>
            <button type="submit" disabled={busy || !selector || view.decode_state === "unparsed"}>
              Inspect selector
            </button>
          </form>
          <nav aria-label="Segments">
            <button
              type="button"
              disabled={busy || view.selected.path === ""}
              onClick={() => onInspect("", 0, -1)}
            >
              Message root
            </button>
            <button
              type="button"
              disabled={busy || view.selected.path === ""}
              onClick={() => onInspect(view.selected.parent, 0, -1)}
            >
              Parent
            </button>
          </nav>
          <h4>{view.selected.path || "Original message"}</h4>
          <dl>
            <dt>Part</dt>
            <dd>{view.selected.kind}</dd>
            <dt>Field label</dt>
            <dd>{view.metadata.label || "No bundled label for this selection"}</dd>
            <dt>Label status</dt>
            <dd>{view.metadata.status || "not available"}</dd>
            <dt>HL7 version</dt>
            <dd>{view.metadata.hl7_version || "not available"}</dd>
            <dt>Label dictionary</dt>
            <dd>{view.metadata.contract || "not applicable"}</dd>
            <dt>Label provenance</dt>
            <dd>{view.metadata.provenance || "not applicable"}</dd>
            <dt>Value state</dt>
            <dd>{view.selected.state}</dd>
            <dt>Original byte range</dt>
            <dd>
              {view.selected.state === "omitted"
                ? "No original bytes: this position is omitted"
                : `[${view.selected.start}, ${view.selected.end}) within occurrence; [${view.source_offset + view.selected.start}, ${view.source_offset + view.selected.end}) in source`}
            </dd>
            <dt>Declared encoding</dt>
            <dd>{view.encoding || "not available"}</dd>
            <dt>Decode status</dt>
            <dd>{view.decode_state}</dd>
            <dt>Raw value (escaped bytes)</dt>
            <dd>
              <code>{view.raw || "(no displayed bytes)"}</code>
            </dd>
            <dt>Decoded value (escaped text)</dt>
            <dd>
              <code>{view.decoded || "(no decoded text)"}</code>
            </dd>
          </dl>
          {view.notice ? <p className="unsupported">{view.notice}</p> : null}
          <h4>Segment tree · {view.child_count} children</h4>
          <ul aria-label="Children of selected message part">
            {view.children.map((child) => (
              <li key={child.path}>
                <button type="button" disabled={busy} onClick={() => onInspect(child.path, 0, -1)}>
                  {child.path} · {child.kind} · {child.state} · [{child.start}, {child.end})
                </button>
              </li>
            ))}
          </ul>
          {view.child_count > 0 ? (
            <nav aria-label="Tree pages">
              <button
                type="button"
                disabled={busy || view.node_offset === 0}
                onClick={() =>
                  onInspect(
                    view.selected.path,
                    Math.max(0, view.node_offset - 100),
                    view.byte_offset,
                  )
                }
              >
                Previous children
              </button>
              <span>
                {view.node_offset + 1}–{view.node_offset + view.children.length} of{" "}
                {view.child_count}
              </span>
              <button
                type="button"
                disabled={busy || view.node_offset + view.children.length >= view.child_count}
                onClick={() =>
                  onInspect(
                    view.selected.path,
                    view.node_offset + view.children.length,
                    view.byte_offset,
                  )
                }
              >
                Next children
              </button>
            </nav>
          ) : null}
          <h4>Original raw and hex bytes</h4>
          <p className="hint">
            Marked bytes belong to the selected part. Framing and terminators remain available.
            Offsets below are within the occurrence.
          </p>
          <nav aria-label="Byte pages">
            <button
              type="button"
              disabled={busy || view.byte_offset === 0}
              onClick={() =>
                onInspect(view.selected.path, view.node_offset, Math.max(0, view.byte_offset - 256))
              }
            >
              Previous bytes
            </button>
            <span>
              [{view.byte_offset}, {view.byte_offset + view.bytes.length}) of {view.size}
            </span>
            <button
              type="button"
              disabled={busy || view.byte_offset + view.bytes.length >= view.size}
              onClick={() =>
                onInspect(
                  view.selected.path,
                  view.node_offset,
                  view.byte_offset + view.bytes.length,
                )
              }
            >
              Next bytes
            </button>
            <button
              type="button"
              disabled={busy || view.selected.state === "omitted"}
              onClick={() => onInspect(view.selected.path, view.node_offset, -1)}
            >
              Go to bytes
            </button>
          </nav>
          <table className="inspector-bytes">
            <caption>Exact original bytes · escaped raw beside hexadecimal</caption>
            <thead>
              <tr>
                <th scope="col">Offset</th>
                <th scope="col">Hex</th>
                <th scope="col">Raw (escaped)</th>
                <th scope="col">Selection</th>
              </tr>
            </thead>
            <tbody>
              {view.bytes.map((byte) => (
                <tr key={byte.offset} className={byte.selected ? "selected-byte" : ""}>
                  <th scope="row">{byte.offset}</th>
                  <td>
                    <code>{byte.hex}</code>
                  </td>
                  <td>
                    <code>{byte.text}</code>
                  </td>
                  <td>{byte.selected ? "selected" : ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      ) : !progress ? (
        <p>Select an occurrence in the grid to inspect its original bytes.</p>
      ) : null}
    </section>
  );
}
