import { useEffect, useState } from "react";
import type { FieldState, InspectionResult, InspectorNode } from "./bindings";
import { FIELD_STATES, HIDDEN_VALUE } from "./display";
import { TaskPanel, TaskTabs } from "./TaskTabs";
import { BackLink, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { IconButton } from "./IconButton";
import { observedInstant, typeLabel } from "./Messages";
import "./inspector.css";

type ReaderView = "fields" | "raw" | "hex";

const VIEWS: { key: ReaderView; label: string }[] = [
  { key: "fields", label: "Fields" },
  { key: "raw", label: "Raw" },
  { key: "hex", label: "Hex" },
];

/** Decode states that are an actual failure to show, rather than routine. */
const PROBLEMS = new Set(["unparsed", "too_large", "unsupported_encoding", "unsupported_escape", "invalid_encoding"]);

/** How a field's state reads when it holds no value to show. */
function stateText(state: FieldState, revealed: boolean, value: string): string {
  if (state === "present") return revealed ? value : HIDDEN_VALUE;
  return FIELD_STATES[state as Exclude<FieldState, "">] ?? state;
}

/** What a node is called: a segment by its code and readable name, a field by
 * its label, anything else by its position. */
function nodeName(node: InspectorNode): string {
  if (node.node.kind === "segment") return node.node.segment;
  return node.label || node.node.path;
}

/** The shared reader of one message: Fields, Raw and Hex over the same
 * verified occurrence. Case messages, Timeline, Findings and a standalone file
 * all read a message through this one view. Values stay hidden until Show
 * values; revealing applies to what is open now and never reaches a list,
 * a query or a log. */
export function MessageReader({
  result,
  kind,
  source,
  loading,
  busy,
  onInspect,
  onReveal,
  onFilterByField,
  onClose,
  backLabel,
  onBack,
}: {
  result: InspectionResult | null;
  /** The occurrence kind the list names; the header names ACK and Unparsed. */
  kind?: "message" | "ack" | "unparsed";
  /** The source the list names the message by. */
  source?: string;
  loading: boolean;
  busy: boolean;
  onInspect: (path: string, nodeOffset: number, byteOffset: number) => Promise<InspectionResult | null>;
  onReveal: (revealed: boolean) => void;
  /** Opens Filter with a rule for the selected field; absent where there is no list to filter. */
  onFilterByField?: ((selector: string, value: string | null, state: FieldState) => void) | undefined;
  onClose?: (() => void) | undefined;
  backLabel?: string | undefined;
  onBack?: (() => void) | undefined;
}) {
  const [view, setView] = useState<ReaderView>("fields");
  const [goingTo, setGoingTo] = useState(false);
  const [info, setInfo] = useState(false);
  const [path, setPath] = useState("");
  const inspection = result?.inspection;
  const selected = inspection?.selected;
  const revealed = inspection?.revealed ?? false;
  useEffect(() => {
    if (goingTo) setPath("");
  }, [goingTo]);

  const title = inspection
    ? typeLabel({ kind: kind ?? (inspection.decode_state === "unparsed" ? "unparsed" : "message"), code: inspection.message_code, trigger: inspection.trigger_event })
    : "Message";
  const facts = inspection ? [inspection.observed_at ? observedInstant(inspection.observed_at) : null, source ?? inspection.source_id].filter(Boolean) : [];
  const atRoot = !selected || selected.path === "";
  const field = selected && selected.kind !== "segment" && selected.kind !== "message" && selected.kind !== "occurrence";

  const go = (target: string) => void onInspect(target, 0, -1);

  return (
    <section className="message-reader" aria-label="Message details">
      <header className="reader-header">
        {backLabel && onBack ? <BackLink label={backLabel} onBack={onBack} /> : null}
        <div className="reader-heading">
          <h2>{title}</h2>
          {facts.length > 0 ? <p className="reader-facts">{facts.join(" · ")}</p> : null}
        </div>
        {inspection ? (
          <Menu
            label="More message actions"
            items={[
              { label: "Go to field…", onSelect: () => setGoingTo(true), disabled: busy || inspection.decode_state === "unparsed" },
              { label: "Info", onSelect: () => setInfo(true) },
            ]}
          />
        ) : null}
        {onClose ? <IconButton icon="close" label="Close message details" onClick={onClose} /> : null}
      </header>

      {result && result.state !== "completed" ? (
        <p className="reader-problem" role="alert">
          {result.reason ?? "This message could not be read."}
        </p>
      ) : null}
      {!inspection && loading ? <p className="reader-loading" aria-live="polite">Reading…</p> : null}

      {inspection && selected ? (
        <>
          {PROBLEMS.has(inspection.decode_state) && inspection.notice ? (
            <p className="reader-problem" role="status">
              {inspection.notice}
            </p>
          ) : null}
          <TaskTabs label="Message views" id="reader-views" tabs={VIEWS} selected={view} onSelect={setView} keepMounted>
            <TaskPanel tabs="reader-views" tab="fields" shown={view === "fields"}>
              {inspection.decode_state === "unparsed" ? (
                <div className="reader-empty">
                  <button type="button" onClick={() => setView("raw")}>
                    View raw
                  </button>
                </div>
              ) : (
                <>
                  {!atRoot ? (
                    <nav aria-label="Path" className="crumbs">
                      <button type="button" disabled={busy} onClick={() => go("")}>
                        Segments
                      </button>
                      {selected.parent !== "" && selected.parent !== selected.path ? (
                        <>
                          <span aria-hidden="true">›</span>
                          <button type="button" disabled={busy} onClick={() => go(selected.parent)}>
                            {selected.parent}
                          </button>
                        </>
                      ) : null}
                      <span aria-hidden="true">›</span>
                      <span aria-current="location">{selected.path}</span>
                    </nav>
                  ) : null}
                  {field ? (
                    <div className="selected-field">
                      <p className="selected-field-name">
                        <strong>{inspection.metadata.label || selected.path}</strong>
                        <code className="selector">{inspection.selector || selected.path}</code>
                      </p>
                      <p className="selected-field-value">
                        {selected.state === "present" ? (revealed ? inspection.decoded || inspection.raw : HIDDEN_VALUE) : stateText(selected.state, revealed, "")}
                      </p>
                      <div className="selected-field-actions">
                        {revealed && selected.state === "present" && (inspection.decoded || inspection.raw) ? (
                          <button type="button" onClick={() => void navigator.clipboard?.writeText(inspection.decoded || inspection.raw)}>
                            Copy value
                          </button>
                        ) : null}
                        {onFilterByField && inspection.selector ? (
                          <button
                            type="button"
                            onClick={() =>
                              onFilterByField(
                                inspection.selector,
                                revealed && selected.state === "present" ? inspection.decoded || null : null,
                                selected.state,
                              )
                            }
                          >
                            Filter by this field
                          </button>
                        ) : null}
                      </div>
                    </div>
                  ) : null}
                  {inspection.children.length > 0 ? (
                    <ul className="outline" aria-label={atRoot ? "Segments" : `Parts of ${selected.path}`}>
                      {inspection.children.map((child) => (
                        <li key={child.node.path}>
                          <button type="button" className="outline-row" disabled={busy} onClick={() => go(child.node.path)}>
                            {child.node.kind === "segment" ? (
                              <>
                                <span className="outline-code">{child.node.segment}</span>
                                <span className="outline-name">{child.segment_name}</span>
                              </>
                            ) : (
                              <>
                                <span className="outline-name">
                                  <span>{nodeName(child)}</span>
                                  {child.label ? <span className="selector">{child.selector || child.node.path}</span> : null}
                                </span>
                                <span className={revealed && child.node.state === "present" ? "outline-value" : "outline-value outline-state"}>
                                  {stateText(child.node.state, revealed, child.value)}
                                </span>
                              </>
                            )}
                          </button>
                        </li>
                      ))}
                    </ul>
                  ) : null}
                  {inspection.child_count > inspection.children.length ? (
                    <nav aria-label="Parts pages" className="pager">
                      <IconButton
                        icon="previous"
                        label="Previous parts"
                        disabled={busy || inspection.node_offset === 0}
                        onClick={() => void onInspect(selected.path, Math.max(0, inspection.node_offset - 100), inspection.byte_offset)}
                      />
                      <span>
                        {inspection.node_offset + 1}–{inspection.node_offset + inspection.children.length} of {inspection.child_count}
                      </span>
                      <IconButton
                        icon="next"
                        label="Next parts"
                        disabled={busy || inspection.node_offset + inspection.children.length >= inspection.child_count}
                        onClick={() => void onInspect(selected.path, inspection.node_offset + inspection.children.length, inspection.byte_offset)}
                      />
                    </nav>
                  ) : null}
                </>
              )}
            </TaskPanel>
            <TaskPanel tabs="reader-views" tab="raw" shown={view === "raw"}>
              {revealed ? (
                <pre className="value raw">{inspection.raw || (selected.state === "omitted" || selected.start === selected.end ? "No bytes" : "—")}</pre>
              ) : (
                <p className="reader-hidden">{HIDDEN_VALUE}</p>
              )}
            </TaskPanel>
            <TaskPanel tabs="reader-views" tab="hex" shown={view === "hex"}>
              {revealed ? (
                <HexTable
                  rows={inspection.bytes}
                  selection={selected.path === "" ? null : { start: selected.start, end: selected.end }}
                  total={inspection.size}
                  offset={inspection.byte_offset}
                  busy={busy}
                  onPage={(offset) => void onInspect(selected.path, inspection.node_offset, offset)}
                />
              ) : (
                <p className="reader-hidden">{HIDDEN_VALUE}</p>
              )}
            </TaskPanel>
          </TaskTabs>
          <div className="reveal">
            <button type="button" disabled={busy} onClick={() => onReveal(!revealed)}>
              {revealed ? "Hide values" : "Show values"}
            </button>
            {revealed ? null : <span className="consequence">May contain patient data.</span>}
          </div>
        </>
      ) : null}

      <FormDialog
        open={goingTo}
        title="Go to field"
        size="small"
        submitLabel="Go"
        submitDisabled={path.trim() === ""}
        busy={busy}
        onClose={() => setGoingTo(false)}
        onSubmit={async (): Promise<SubmitFailure | null> => {
          const answer = await onInspect(path.trim(), 0, -1);
          if (!answer || answer.state !== "completed") return { reason: answer?.reason ?? "This field is not in the message.", field: "go-to-field" };
          setView("fields");
          setGoingTo(false);
          return null;
        }}
      >
        <label htmlFor="go-to-field">Field path</label>
        <input id="go-to-field" type="text" autoFocus spellCheck={false} value={path} onChange={(event) => setPath(event.target.value)} />
      </FormDialog>

      <Modal open={info && Boolean(inspection)} title="Info" onClose={() => setInfo(false)}>
        {inspection ? (
          <ValueRows
            rows={[
              { label: "Bytes", value: String(inspection.size) },
              { label: "Encoding", value: inspection.encoding || "—" },
              { label: "HL7 version", value: inspection.metadata.hl7_version || "—" },
              { label: "Field labels", value: inspection.metadata.provenance || "—" },
              { label: "Observed", value: observedInstant(inspection.observed_at) },
              { label: "Source", value: inspection.source_id || "—" },
              { label: "Occurrence", value: <code>{inspection.occurrence || `Message ${inspection.message + 1}`}</code> },
              ...(inspection.identity ? [{ label: "Evidence", value: <code>{inspection.identity}</code> }] : []),
              ...(inspection.source_offset ? [{ label: "In the source", value: `From byte ${inspection.source_offset}` }] : []),
            ]}
          />
        ) : null}
      </Modal>
    </section>
  );
}

/** Original bytes, 16 to a row: the offset, grouped hex and the printable
 * text, which stays empty until values are shown. The rows holding the
 * selected part are marked. Pages appear only when there is more. */
export function HexTable({
  rows,
  selection,
  total,
  offset,
  busy,
  onPage,
}: {
  rows: { offset: number; hex: string; text: string }[];
  selection: { start: number; end: number } | null;
  total: number;
  offset: number;
  busy: boolean;
  onPage: (offset: number) => void;
}) {
  const shown = rows.reduce((sum, row) => sum + row.hex.split(" ").filter(Boolean).length, 0);
  const paged = offset > 0 || offset + shown < total;
  return (
    <>
      {paged ? (
        <nav aria-label="Byte pages" className="pager">
          <IconButton icon="previous" label="Previous bytes" disabled={busy || offset === 0} onClick={() => onPage(Math.max(0, offset - 256))} />
          <span>
            {offset}–{offset + shown} of {total}
          </span>
          <IconButton icon="next" label="Next bytes" disabled={busy || offset + shown >= total} onClick={() => onPage(offset + shown)} />
        </nav>
      ) : null}
      <div className="hex-scroll">
      <table className="hex-table">
        <caption className="visually-hidden">Original bytes</caption>
        <thead>
          <tr>
            <th scope="col">Offset</th>
            <th scope="col">Hex</th>
            <th scope="col">Text</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const marked = selection !== null && selection.end > row.offset && selection.start < row.offset + 16;
            return (
              <tr key={row.offset} className={marked ? "marked" : undefined}>
                <th scope="row">{row.offset.toString(16).padStart(total > 0xffff ? 8 : 4, "0")}</th>
                <td>
                  <code>{row.hex}</code>
                </td>
                <td>
                  <code>{row.text}</code>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      </div>
    </>
  );
}
