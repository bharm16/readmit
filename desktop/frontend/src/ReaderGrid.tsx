import { useId, useRef, useCallback, useState, useEffect, useLayoutEffect, type KeyboardEvent } from "react";
import type { HL7InspectorGrid, InspectorNode } from "./bindings";
import { useMeasured } from "./measure";
import { ReaderIcon } from "./ReaderIcon";
import { ReaderResize } from "./ReaderResize";
import { referenceColumns, useReaderColumns } from "./ReaderColumns";
import { useReferenceHover, type ReferenceHoverScope, type ReferenceHoverTarget } from "./ReferenceHover";

// The host accepts bounded windows up to 1,000 rows, with 100 as its default.
const DEFAULT_WINDOW_ROWS = 100;
const MAX_WINDOW_ROWS = 1000;
const OVERSCAN_ROWS = 10;

/** The host supplies source order, depth and canonical identities. This view
 * navigates that bounded projection; it never reconstructs HL7 structure. */
export function ReaderGrid({ grid, selected, busy, value, onSelect, onExpand, onPage, reference }: {
  grid: HL7InspectorGrid;
  selected: string;
  busy: boolean;
  value: (row: InspectorNode) => string;
  onSelect: (path: string) => void;
  onExpand?: (row: InspectorNode) => void;
  onPage?: (offset: number, edge?: "first" | "last", limit?: number) => void | Promise<void>;
  reference?: ReferenceHoverScope;
}) {
  const element = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  const bindViewport = useCallback((node: HTMLDivElement | null) => { element.current = node; setViewport(node); }, []);
  const [keyboardRead, setKeyboardRead] = useState(false);
  const [scrollTop, setScrollTop] = useState(0);
  const { height, width, rem } = useMeasured(viewport);
  const rowHeight = 2.875 * rem;
  const total = grid.row_count ?? grid.rows.length;
  const requested = useRef<{ offset: number; limit: number } | null>(null);
  const previousSelection = useRef(selected);
  const id = useId();
  const hover = useReferenceHover(reference);
  const [columns, setColumns] = useReaderColumns();
  const metadata = referenceColumns.filter(column => columns.visible.includes(column.name));
  const pathWidth=Math.max(6.5,Math.min(20,Math.max(0,...grid.rows.map(row=>(row.display_path || row.node.path).length*.5+1))));
  const available = Math.max(columns.name + columns.value, width / rem - pathWidth - metadata.reduce((sum, column) => sum + column.width, 0));
  // Spread a wide pane across the descriptive columns. A manually resized
  // column stays at its chosen width; the other absorbs the available space.
  const nameWidth = columns.fixedColumn === "name" ? columns.name
    : columns.fixedColumn === "value" ? available - columns.value
    : Math.max(columns.name, Math.min(available / 2, available - columns.value));
  const valueWidth = available - nameWidth;
  const tracks = `${pathWidth}rem ${nameWidth}rem ${metadata.map(column => `${column.width}rem`).join(" ")} ${valueWidth}rem`;
  const at = grid.rows.findIndex(row => row.node.path === selected);
  const choose = (row: InspectorNode) => {
    if (busy) return;
    element.current?.focus({ preventScroll: true });
    onSelect(row.node.path);
  };
  const page = (offset: number, edge: "first" | "last", limit: number) => {
    setKeyboardRead(true);
    void Promise.resolve(onPage?.(offset, edge, limit)).finally(() => setKeyboardRead(false));
  };
  const keys = (event: KeyboardEvent<HTMLDivElement>) => {
    hover.keyDown(event);
    if (event.defaultPrevented) return;
    if (event.target !== event.currentTarget) return;
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    const row = grid.rows[at];
    if (event.key === "Enter" && row && !busy) {
      const trigger = element.current?.querySelector<HTMLButtonElement>('[aria-selected="true"] .reference-preview-trigger');
      if (trigger) { event.preventDefault(); trigger.focus(); }
      return;
    }
    const windowSize = Math.max(DEFAULT_WINDOW_ROWS, grid.rows.length);
    const previous = Math.max(0, grid.offset - windowSize);
    const more = grid.offset + grid.rows.length < (grid.row_count ?? 0);
    if (!busy && !keyboardRead && (event.key === "PageDown" || event.key === "ArrowDown" && at === grid.rows.length-1) && more) { event.preventDefault(); page(grid.offset+grid.rows.length,"first",windowSize); return; }
    if (!busy && !keyboardRead && (event.key === "PageUp" || event.key === "ArrowUp" && at === 0) && grid.offset > 0) { event.preventDefault(); page(previous,"last",grid.offset-previous); return; }
    if (!busy && !keyboardRead && event.key === "Home" && grid.offset > 0) {event.preventDefault(); page(0,"first",windowSize);return;}
    if (!busy && !keyboardRead && event.key === "End" && more) {event.preventDefault(); page(Math.max(0,total-windowSize),"last",windowSize);return;}
    const index = event.key === "ArrowDown" ? Math.min(at + 1, grid.rows.length - 1)
      : event.key === "ArrowUp" ? Math.max(0, at - 1)
      : event.key === "Home" ? 0 : event.key === "End" ? grid.rows.length - 1 : -1;
    if (index >= 0) { event.preventDefault(); if (grid.rows[index]) choose(grid.rows[index]); return; }
    if (!row || busy || keyboardRead) return;
    if (event.key === "ArrowRight") {
      event.preventDefault();
      if (row.has_children && !row.expanded) onExpand?.(row);
      else if (grid.rows[at + 1]?.grid_parent === row.node.path) choose(grid.rows[at + 1]!);
    } else if (event.key === "ArrowLeft") {
      event.preventDefault();
      if (row.expanded) onExpand?.(row);
      else if (row.grid_parent) onSelect(row.grid_parent);
    }
  };
  useLayoutEffect(() => {
    if (grid.follow_selection || previousSelection.current !== selected) element.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView?.({block:"nearest",inline:"nearest"});
    previousSelection.current = selected;
  }, [selected, grid]);
  // Keep the full scroll range while retaining only the host's bounded window.
  // A later scroll during a pending read is considered again when it finishes.
  useEffect(() => {
    if (!viewport || busy || keyboardRead || !onPage || total <= grid.rows.length) return;
    const first = Math.max(0, Math.floor(viewport.scrollTop / rowHeight));
    const visible = Math.max(1, Math.ceil(viewport.clientHeight / rowHeight));
    const limit = Math.min(MAX_WINDOW_ROWS, Math.max(DEFAULT_WINDOW_ROWS, visible + 2 * OVERSCAN_ROWS));
    const start = Math.max(0, first - OVERSCAN_ROWS);
    const end = Math.min(total, first + visible + OVERSCAN_ROWS);
    if (start >= grid.offset && end <= grid.offset + grid.rows.length) { requested.current = null; return; }
    const offset = Math.min(start, Math.max(0, total - 1));
    if (requested.current?.offset === offset && requested.current.limit === limit) return;
    requested.current = { offset, limit };
    onPage(offset, undefined, limit);
  }, [viewport, height, rowHeight, scrollTop, grid, busy, keyboardRead, onPage, total]);
  return <section className="message-grid-region" aria-label="Segment grid">
    <div ref={bindViewport} className="message-grid" onScroll={event => setScrollTop(event.currentTarget.scrollTop)} style={{["--message-grid-tracks" as string]:tracks, maxHeight:(MAX_WINDOW_ROWS - 2 * OVERSCAN_ROWS) * rowHeight}} role="treegrid" aria-label="Message fields" aria-busy={busy} aria-rowcount={(grid.row_count ?? grid.rows.length) + 1} aria-activedescendant={at < 0 ? undefined : `${id}-${at}`} tabIndex={0} onKeyDown={keys}>
      <div role="row" className="message-grid-head">{["Path", "Name", ...metadata.map(column => column.name), "Value"].map(name => <div role="columnheader" aria-label={name} key={name}>{name}{name === "Name" || name === "Value" ? <ReaderResize label={`${name} column width`} value={name === "Name" ? nameWidth : valueWidth} min={name === "Name" ? 10 : 12} max={Math.max(name === "Name" ? 40 : 80, width / rem, name === "Name" ? nameWidth : valueWidth)} onChange={width => setColumns(current => ({...current, [name === "Name" ? "name" : "value"]:width, fixedColumn:name === "Name" ? "name" : "value"}))} /> : null}</div>)}</div>
      <div aria-hidden="true" style={{ height: grid.offset * rowHeight }} />
      {grid.rows.map((row, index) => <div id={`${id}-${index}`} key={row.node.path} role="row" aria-rowindex={grid.offset + index + 2} aria-level={(row.depth ?? 0) + 1} aria-selected={row.node.path === selected} aria-expanded={row.has_children ? Boolean(row.expanded) : undefined} className={`message-grid-row message-grid-${row.node.kind}`} onClick={() => choose(row)}>
        <div role="gridcell" className="message-grid-path" title={row.node.path}>{row.display_path || row.node.path}</div>
        <div role="gridcell" className="message-grid-name" style={{ paddingInlineStart: `${.5 + (row.depth ?? 0) * 1.5}rem` }}>
          {row.has_children ? <button type="button" tabIndex={-1} className={`message-grid-disclosure${row.expanded ? " is-expanded" : ""}`} aria-label={`${row.expanded ? "Collapse" : "Expand"} ${row.display_path || row.node.path}`} disabled={busy} onClick={event => { event.stopPropagation(); element.current?.focus({ preventScroll: true }); onExpand?.(row); }}><ReaderIcon name="next" /></button> : <span className="message-grid-disclosure" />}
          <button type="button" tabIndex={-1} disabled={busy} aria-current={row.node.path === selected ? "location" : undefined} aria-label={`${row.node.path} ${row.node.kind === "segment" ? row.segment_name || row.node.segment : row.label || row.node.path} ${value(row)}`}>{row.node.kind === "segment" ? row.segment_name || row.node.segment : row.label || row.display_path || row.node.path}</button>
        </div>
        {metadata.map(column => {
          const attribute = row.reference?.[column.attribute];
          const text = attribute?.state === "specified" ? attribute.value : attribute?.state === "not_available" || !attribute ? "?" : "—";
          const target = (anchor: HTMLButtonElement): ReferenceHoverTarget => ({ anchor, path: row.node.path, displayPath: row.display_path || row.node.path, label: row.reference?.name || row.label, recordKey: row.reference?.key || "", attribute: column.attribute, value: text });
          return <div role="gridcell" key={column.name} className={`message-grid-attribute message-grid-${column.attribute}`}>{row.node.kind === "segment" ? "" : reference ? <button type="button" className="reference-preview-trigger" tabIndex={row.node.path === selected ? 0 : -1} disabled={busy} aria-label={`${column.name} reference for ${row.display_path || row.node.path}: ${text}`} aria-haspopup="dialog" aria-controls={hover.activeAnchor?.dataset.referenceTarget === `${row.node.path}/${column.attribute}` ? hover.id : undefined} aria-expanded={hover.activeAnchor?.dataset.referenceTarget === `${row.node.path}/${column.attribute}`} data-reference-target={`${row.node.path}/${column.attribute}`} onPointerEnter={event => hover.show(target(event.currentTarget))} onPointerLeave={hover.leave} onFocus={event => hover.show(target(event.currentTarget), true)} onBlur={hover.blur} onKeyDown={hover.keyDown} onClick={event => { event.stopPropagation(); hover.show(target(event.currentTarget), true); }}>{text}</button> : text}</div>;
        })}
        <div role="gridcell" className={`message-grid-value${row.node.state === "present" ? "" : " message-grid-state"}`}>{row.node.kind === "segment" ? "" : value(row)}</div>
      </div>)}
      <div aria-hidden="true" style={{ height: Math.max(0, total - grid.offset - grid.rows.length) * rowHeight }} />
    </div>
    {hover.card}
  </section>;
}
