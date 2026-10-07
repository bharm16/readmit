import { useId, useRef, useLayoutEffect, type KeyboardEvent } from "react";
import type { HL7InspectorGrid, InspectorNode } from "./bindings";
import { ReaderIcon } from "./ReaderIcon";
import { ReaderResize } from "./ReaderResize";
import { referenceColumns, useReaderColumns } from "./ReaderColumns";

/** The host supplies source order, depth and canonical identities. This view
 * navigates that bounded projection; it never reconstructs HL7 structure. */
export function ReaderGrid({ grid, selected, busy, value, onSelect, onExpand, onPage }: {
  grid: HL7InspectorGrid;
  selected: string;
  busy: boolean;
  value: (row: InspectorNode) => string;
  onSelect: (path: string) => void;
  onExpand?: (row: InspectorNode) => void;
  onPage?: (offset: number, edge?: "first" | "last") => void;
}) {
  const element = useRef<HTMLDivElement>(null);
  const id = useId();
  const [columns, setColumns] = useReaderColumns();
  const metadata = referenceColumns.filter(column => columns.visible.includes(column.name));
  const pathWidth=Math.max(6.5,Math.min(20,Math.max(0,...grid.rows.map(row=>(row.display_path || row.node.path).length*.5+1))));
  const tracks = `${pathWidth}rem ${columns.name}rem ${metadata.map(column => `${column.width}rem`).join(" ")} minmax(${columns.value}rem,1fr)`;
  const at = grid.rows.findIndex(row => row.node.path === selected);
  const choose = (row: InspectorNode) => {
    if (busy) return;
    element.current?.focus({ preventScroll: true });
    onSelect(row.node.path);
  };
  const keys = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget) return;
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    const row = grid.rows[at];
    const more = grid.offset + grid.rows.length < (grid.row_count ?? 0);
    if (!busy && (event.key === "PageDown" || event.key === "ArrowDown" && at === grid.rows.length-1) && more) { event.preventDefault(); onPage?.(grid.offset+100,"first"); return; }
    if (!busy && (event.key === "PageUp" || event.key === "ArrowUp" && at === 0) && grid.offset > 0) { event.preventDefault(); onPage?.(Math.max(0,grid.offset-100),"last"); return; }
    if (!busy && event.key === "Home" && grid.offset > 0) {event.preventDefault(); onPage?.(0,"first");return;}
    if (!busy && event.key === "End" && more) {event.preventDefault(); onPage?.(Math.floor(((grid.row_count ?? 1)-1)/100)*100,"last");return;}
    const index = event.key === "ArrowDown" ? Math.min(at + 1, grid.rows.length - 1)
      : event.key === "ArrowUp" ? Math.max(0, at - 1)
      : event.key === "Home" ? 0 : event.key === "End" ? grid.rows.length - 1 : -1;
    if (index >= 0) { event.preventDefault(); if (grid.rows[index]) choose(grid.rows[index]); return; }
    if (!row || busy) return;
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
  useLayoutEffect(() => { if (grid.follow_selection || document.activeElement === element.current) element.current?.querySelector<HTMLElement>('[aria-selected="true"]')?.scrollIntoView?.({block:"nearest",inline:"nearest"}); }, [selected,grid.follow_selection]);
  return <section className="message-grid-region" aria-label="Segment grid">
    <div ref={element} className="message-grid" style={{["--message-grid-tracks" as string]:tracks}} role="treegrid" aria-label="Message fields" aria-busy={busy} aria-rowcount={(grid.row_count ?? grid.rows.length) + 1} aria-activedescendant={at < 0 ? undefined : `${id}-${at}`} tabIndex={0} onKeyDown={keys}>
      <div role="row" className="message-grid-head">{["Path", "Name", ...metadata.map(column => column.name), "Value"].map(name => <div role="columnheader" aria-label={name} key={name}>{name}{name === "Name" || name === "Value" ? <ReaderResize label={`${name} column width`} value={name === "Name" ? columns.name : columns.value} min={name === "Name" ? 10 : 12} max={name === "Name" ? 40 : 80} onChange={width => setColumns(current => ({...current, [name === "Name" ? "name" : "value"]:width}))} /> : null}</div>)}</div>
      {grid.rows.map((row, index) => <div id={`${id}-${index}`} key={row.node.path} role="row" aria-rowindex={grid.offset + index + 2} aria-level={(row.depth ?? 0) + 1} aria-selected={row.node.path === selected} aria-expanded={row.has_children ? Boolean(row.expanded) : undefined} className={`message-grid-row message-grid-${row.node.kind}`} onClick={() => choose(row)}>
        <div role="gridcell" className="message-grid-path" title={row.node.path}>{row.display_path || row.node.path}</div>
        <div role="gridcell" className="message-grid-name" style={{ paddingInlineStart: `${.5 + (row.depth ?? 0) * 1.5}rem` }}>
          {row.has_children ? <button type="button" tabIndex={-1} className={`message-grid-disclosure${row.expanded ? " is-expanded" : ""}`} aria-label={`${row.expanded ? "Collapse" : "Expand"} ${row.display_path || row.node.path}`} disabled={busy} onClick={event => { event.stopPropagation(); element.current?.focus({ preventScroll: true }); onExpand?.(row); }}><ReaderIcon name="next" /></button> : <span className="message-grid-disclosure" />}
          <button type="button" tabIndex={-1} disabled={busy} aria-current={row.node.path === selected ? "location" : undefined} aria-label={`${row.node.path} ${row.node.kind === "segment" ? row.segment_name || row.node.segment : row.label || row.node.path} ${value(row)}`}>{row.node.kind === "segment" ? row.segment_name || row.node.segment : row.label || row.display_path || row.node.path}</button>
        </div>
        {metadata.map(column => { const attribute = row.reference?.[column.attribute]; return <div role="gridcell" key={column.name} className={`message-grid-attribute message-grid-${column.attribute}`} title={attribute?.state === "specified" ? attribute.value : attribute?.state?.replaceAll("_", " ") || "Not available"}>{row.node.kind === "segment" ? "" : attribute?.state === "specified" ? attribute.value : attribute?.state === "not_available" || !attribute ? "?" : "—"}</div>; })}
        <div role="gridcell" className={`message-grid-value${row.node.state === "present" ? "" : " message-grid-state"}`}>{row.node.kind === "segment" ? "" : value(row)}</div>
      </div>)}
    </div>
    {(grid.row_count ?? 0) > grid.rows.length ? <nav className="pager" aria-label="Message field pages"><button type="button" disabled={busy || grid.offset === 0} onClick={() => onPage?.(Math.max(0, grid.offset - 100))}>Previous rows</button><span>{grid.offset + 1}–{grid.offset + grid.rows.length} of {grid.row_count}</span><button type="button" disabled={busy || grid.offset + grid.rows.length >= (grid.row_count ?? 0)} onClick={() => onPage?.(grid.offset + 100)}>Next rows</button></nav> : null}
  </section>;
}
