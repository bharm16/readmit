import { useState } from "react";
import type { GridRow, InspectionResult } from "./bindings";
import { DIRECTIONS, KIND_CAPTIONS, observedTime, Report, type Indicators } from "./shell";
import { TaskPanel, TaskTabs } from "./TaskTabs";
import { FormDialog, MoreMenu, humanize } from "./layout";
import { Field } from "./ui";
import { IconButton } from "./IconButton";
import "./inspector.css";

type InspectorView = "fields" | "raw" | "hex";

const VIEWS: { key: InspectorView; label: string }[] = [
  { key: "fields", label: "Fields" },
  { key: "raw", label: "Raw" },
  { key: "hex", label: "Hex" },
];

/** The details of one message of an open case: where it sits, its parts, and
 * its original bytes. This view never parses a message, computes a byte span
 * or decodes text: every value and path came from the same verified read, and
 * selecting a part replaces its details, value and bytes at once. Nothing
 * shown here is kept. */
export function Inspector({
  result,
  row = null,
  busy,
  progress,
  indicators,
  onInspect,
}: {
  result: InspectionResult | null;
  /** The message's row in the open list, when it is there: what it is, which
   * way it went and when. */
  row?: GridRow | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onInspect: (path: string, nodeOffset: number, byteOffset: number) => void;
}) {
  const [view, setView] = useState<InspectorView>("fields");
  const [goingTo, setGoingTo] = useState(false);
  const [selector, setSelector] = useState("");
  const inspection = result?.inspection;
  const selected = inspection?.selected;
  const atRoot = !selected || selected.path === "";
  const summary = inspection
    ? [
        row && row.direction !== "unknown" ? DIRECTIONS[row.direction] : null,
        row?.observed_at ? observedTime(row.observed_at) : null,
        inspection.source_id,
        `${inspection.size} bytes`,
      ].filter((fact): fact is string => Boolean(fact))
    : [];

  return (
    <section className="message-inspector" aria-label="Message inspector">
      {inspection && selected ? (
        <>
          <header className="inspector-header">
            <div className="inspector-heading">
              <h3>{row ? (row.kind === "unparsed" ? "Unparsed message" : KIND_CAPTIONS[row.kind]) : "Message"}</h3>
              <p className="inspector-meta">{summary.join(" · ")}</p>
            </div>
            <MoreMenu
              label="More message actions"
              items={[{ label: "Go to field…", onSelect: () => setGoingTo(true), disabled: busy || inspection.decode_state === "unparsed" }]}
            />
          </header>

          {!atRoot ? (
            <nav aria-label="Segments" className="crumbs">
              <button type="button" disabled={busy} onClick={() => onInspect("", 0, -1)}>
                Message
              </button>
              {selected.parent !== "" && selected.parent !== selected.path ? (
                <>
                  <span aria-hidden="true">›</span>
                  <button type="button" disabled={busy} onClick={() => onInspect(selected.parent, 0, -1)}>
                    {selected.parent}
                  </button>
                </>
              ) : null}
              <span aria-hidden="true">›</span>
              <span aria-current="location">{selected.path}</span>
            </nav>
          ) : null}

          <Report indicators={indicators} progress={progress} result={result && result.state !== "completed" ? result : null} />

          {inspection.notice ? <p className="field-hint" role="status">{inspection.notice}</p> : null}
          {inspection.decode_state !== "parsed" ? <p className="hint">Display: {humanize(inspection.decode_state)}</p> : null}
          <TaskTabs label="Message views" id="inspector-views" tabs={VIEWS} selected={view} onSelect={setView} keepMounted>
            <TaskPanel tabs="inspector-views" tab="fields" shown={view === "fields"}>
              {!atRoot ? (
                <div className="selected-part">
                  <p className="part-title">
                    <strong>{inspection.metadata.label || selected.path}</strong>
                    {selected.state !== "present" ? <span className="badge warn">{humanize(selected.state)}</span> : null}
                  </p>
                  {inspection.decoded ? <pre className="value">{inspection.decoded}</pre> : null}
                </div>
              ) : null}
              {inspection.child_count > 0 ? (
                <>
                  <ul className="parts" aria-label="Children of selected message part">
                    {inspection.children.map((child) => (
                      <li key={child.path}>
                        <button type="button" className="row-link" disabled={busy} onClick={() => onInspect(child.path, 0, -1)}>
                          {child.path}
                        </button>
                        {child.state !== "present" ? <span className="part-state">{humanize(child.state)}</span> : null}
                      </li>
                    ))}
                  </ul>
                  {inspection.child_count > inspection.children.length ? (
                    <nav aria-label="Tree pages" className="pager">
                      <IconButton
                        icon="previous"
                        label="Previous parts"
                        disabled={busy || inspection.node_offset === 0}
                        onClick={() => onInspect(selected.path, Math.max(0, inspection.node_offset - 100), inspection.byte_offset)}
                      />
                      <span>
                        {inspection.node_offset + 1}–{inspection.node_offset + inspection.children.length} of {inspection.child_count}
                      </span>
                      <IconButton
                        icon="next"
                        label="Next parts"
                        disabled={busy || inspection.node_offset + inspection.children.length >= inspection.child_count}
                        onClick={() => onInspect(selected.path, inspection.node_offset + inspection.children.length, inspection.byte_offset)}
                      />
                    </nav>
                  ) : null}
                </>
              ) : atRoot ? (
                <p className="hint">No parts.</p>
              ) : null}
              <dl className="facts part-facts">
                <div className="fact">
                  <dt>Bytes</dt>
                  <dd>{selected.state === "omitted" ? "None" : `${selected.start}–${selected.end}`}</dd>
                </div>
                {selected.state !== "omitted" && inspection.source_offset !== 0 ? (
                  <div className="fact">
                    <dt>In the file</dt>
                    <dd>
                      {inspection.source_offset + selected.start}–{inspection.source_offset + selected.end}
                    </dd>
                  </div>
                ) : null}
                {inspection.encoding ? (
                  <div className="fact">
                    <dt>Encoding</dt>
                    <dd>{inspection.encoding}</dd>
                  </div>
                ) : null}
                {inspection.metadata.hl7_version ? (
                  <div className="fact">
                    <dt>HL7 version</dt>
                    <dd>{inspection.metadata.hl7_version}</dd>
                  </div>
                ) : null}
                {inspection.metadata.provenance ? (
                  <div className="fact">
                    <dt>Field labels</dt>
                    <dd>{inspection.metadata.provenance}</dd>
                  </div>
                ) : null}
              </dl>
            </TaskPanel>
            <TaskPanel tabs="inspector-views" tab="raw" shown={view === "raw"}>
              <pre className="value raw">{inspection.raw || (selected.state === "omitted" || selected.start === selected.end ? "No bytes" : "Raw text unavailable; original bytes remain in Hex.")}</pre>
            </TaskPanel>
            <TaskPanel tabs="inspector-views" tab="hex" shown={view === "hex"}>
              <nav aria-label="Byte pages" className="pager">
                <IconButton
                  icon="previous"
                  label="Previous bytes"
                  disabled={busy || inspection.byte_offset === 0}
                  onClick={() => onInspect(selected.path, inspection.node_offset, Math.max(0, inspection.byte_offset - 256))}
                />
                <span>
                  {inspection.byte_offset}–{inspection.byte_offset + inspection.bytes.length} of {inspection.size}
                </span>
                <IconButton
                  icon="next"
                  label="Next bytes"
                  disabled={busy || inspection.byte_offset + inspection.bytes.length >= inspection.size}
                  onClick={() => onInspect(selected.path, inspection.node_offset, inspection.byte_offset + inspection.bytes.length)}
                />
              </nav>
              <table className="data-table inspector-bytes">
                <caption className="visually-hidden">Original bytes, escaped, beside their hexadecimal</caption>
                <thead>
                  <tr>
                    <th scope="col" className="number">
                      Offset
                    </th>
                    <th scope="col">Hex</th>
                    <th scope="col">Text</th>
                  </tr>
                </thead>
                <tbody>
                  {inspection.bytes.map((byte) => (
                    <tr key={byte.offset} className={byte.selected ? "selected-byte" : ""} aria-selected={byte.selected}>
                      <th scope="row" className="number">
                        {byte.offset}
                      </th>
                      <td>
                        <code>{byte.hex}</code>
                      </td>
                      <td>
                        <code>{byte.text}</code>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TaskPanel>
          </TaskTabs>
        </>
      ) : (
        <>
          <h3 className="visually-hidden">Message details</h3>
          <Report indicators={indicators} progress={progress} result={result && result.state !== "completed" ? result : null} />
        </>
      )}

      <FormDialog
        open={goingTo}
        title="Go to field"
        submitLabel="Go"
        submitDisabled={selector.trim() === ""}
        busy={busy}
        onClose={() => setGoingTo(false)}
        onSubmit={() => {
          setGoingTo(false);
          setView("fields");
          onInspect(selector.trim(), 0, -1);
        }}
      >
        <Field label="Field path" htmlFor="inspect-selector">
          <input
            id="inspect-selector"
            type="text"
            autoFocus
            placeholder="PID[1]-3[2].4.1"
            value={selector}
            onChange={(event) => setSelector(event.target.value)}
          />
        </Field>
      </FormDialog>
    </section>
  );
}
