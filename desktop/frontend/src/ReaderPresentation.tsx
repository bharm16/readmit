import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useMeasured } from "./measure";
import type { Inspection, InspectorNode, Hl7referenceAttribute, Hl7referenceRecord } from "./bindings";
import { FIELD_STATES, HIDDEN_VALUE } from "./display";
import { ReaderIcon } from "./ReaderIcon";
import { IconButton } from "./IconButton";

const pathLabel = (path: string) => path;

export function ReferenceBadge({ attribute, onClick, label }: { attribute: Hl7referenceAttribute; onClick?: (() => void) | undefined; label?: string }) {
  if (attribute.state !== "specified") return <span className="reference-unavailable">{({ not_specified: "Not specified", not_applicable: "Not applicable", not_available: "Not available" } as Record<string, string>)[attribute.state] ?? "Not available"}</span>;
  return onClick ? <button type="button" className="reference-badge reference-badge-link" aria-label={label} onClick={onClick}>{attribute.value}</button> : <code className="reference-badge">{attribute.value}</code>;
}

export function ReferenceAttributes({ record, datatype, element }: { record: Hl7referenceRecord; datatype?: (() => void) | undefined; element?: (() => void) | undefined }) {
  const usage = ({ R: "Required", O: "Optional", C: "Conditional", B: "Backward compatibility", W: "Withdrawn" } as Record<string, string>)[record.optionality.value];
  const rows: { label: string; value: ReactNode }[] = [
    { label: "Data type", value: <ReferenceBadge attribute={record.datatype} onClick={datatype} label={`Datatype · ${record.datatype.value}`} /> },
    { label: "Optionality", value: <><ReferenceBadge attribute={record.optionality} />{record.optionality.state === "specified" && usage ? <span>— {usage}</span> : null}</> },
    { label: "Length", value: <ReferenceBadge attribute={record.length} /> },
    { label: "C-Len", value: <ReferenceBadge attribute={record.conformance_length} /> },
    { label: "Repetition", value: <ReferenceBadge attribute={record.repetition} /> },
    { label: "Data element", value: <ReferenceBadge attribute={record.item} onClick={element} label={`Item# · ${record.item.value}`} /> },
  ];
  return <dl className="reference-attributes">{rows.map(row => <div key={row.label}><dt>{row.label}</dt><dd>{row.value}</dd></div>)}</dl>;
}

/** Navigation uses the facade's sibling projection, never a fabricated path. */
export function ReaderSelectionHeader({ inspection, nodes, busy, onSelect, onGoToField }: { inspection: Inspection; nodes: InspectorNode[]; busy: boolean; onSelect: (path: string) => void; onGoToField: () => void }) {
  const selected = inspection.selected;
  const siblings = nodes.filter((row, index) => row.node.kind === selected.kind && row.node.parent === selected.parent && nodes.findIndex(candidate => candidate.node.path === row.node.path) === index);
  const at = siblings.findIndex(row => row.node.path === selected.path);
  const previous = at > 0 ? siblings[at - 1] : undefined;
  const next = at >= 0 ? siblings[at + 1] : undefined;
  const parent = selected.kind === "component" || selected.kind === "subcomponent" || selected.kind === "repetition" ? nodes.find(row => row.node.path === (inspection.grid?.selected_parent || selected.parent)) ?? (selected.kind === "component" ? nodes.find(row => row.node.kind === "field" && selected.parent.startsWith(`${row.node.path}[`)) : undefined) : undefined;
  const title = inspection.reference?.record?.name || inspection.metadata.label || inspection.segment_name || selected.segment || "Message";
  return <div className="reader-selection-context">
    {parent ? <div className="reader-parent-context"><code>{parent.display_path || pathLabel(parent.node.path)}</code><code className="reader-parent-value">{parent.node.state === "present" ? inspection.revealed ? (parent.raw ?? parent.value) + (parent.truncated ? " · Truncated" : "") : HIDDEN_VALUE : FIELD_STATES[parent.node.state as Exclude<keyof typeof FIELD_STATES, "">] ?? parent.node.state}</code><span>{parent.label}</span>{parent.reference ? <ReferenceBadge attribute={parent.reference.datatype} /> : null}</div> : null}
    <div className="reader-location">
      {selected.kind !== "segment" && selected.path ? <div className="reader-location-navigation" role="group" aria-label="Selected position navigation">
        <button type="button" aria-label="Previous position" disabled={busy || !previous} onClick={() => previous && onSelect(previous.node.path)}><ReaderIcon name="previous" /></button>
        {!parent ? <button type="button" aria-label="Go to field" disabled={busy} onClick={onGoToField}><ReaderIcon name="position" /></button> : null}
        <button type="button" aria-label="Next position" disabled={busy || !next} onClick={() => next && onSelect(next.node.path)}><ReaderIcon name="next" /></button>
      </div> : null}
      <code title={selected.path} aria-label={selected.path || "Message"}>{inspection.display_path || pathLabel(selected.path) || "Message"}</code>{selected.path ? <span aria-hidden="true">—</span> : null}{selected.path ? <h4>{title}</h4> : null}
    </div>
  </div>;
}

/** A linked table caption starts a source excerpt, not more introductory prose.
 * Keep that complete excerpt available verbatim; structured codes are read by
 * the existing exact-edition Table endpoint, never parsed in the webview. */
export function ReferenceDefinition({ text, expanded, onToggle, compact, tables = [], onTable }: { text: string; expanded: boolean; onToggle: () => void; compact: string; tables?: string[]; onTable?: (key: string) => void }) {
  const captions = [...compact.matchAll(/(^|[\n.!?]\s+)((?:HL7 |User[- ]defined )?Table (\d{4})[^\n]*?)(?=\s+Values?\s+Description\b|\n|$)/gim)]
    .filter(match => tables.includes(`table/${match[3]}`))
    .map(match => ({index:match.index!+match[1]!.length,table:match[3]!}));
  const first = captions[0];
  const overview = first?.index !== undefined ? compact.slice(0,first.index).trim() : compact;
  const [element,setElement] = useState<HTMLDivElement|null>(null);
  const size = useMeasured(element);
  const [overflow,setOverflow] = useState(false);
  const measuredText = useRef("");
  useLayoutEffect(()=>{
    if(!element || expanded)return;
    measuredText.current=text;
    const measure=()=>setOverflow(element.scrollHeight > element.clientHeight + 1);
    measure();
    const observer=typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(element); if(element.firstElementChild)observer?.observe(element.firstElementChild);
    return ()=>observer?.disconnect();
  },[element,size.width,size.rem,text,overview,expanded]);
  const hasMore=text.trim().replace(/^Definition:\s*/, "")!==overview || overflow && measuredText.current===text;
  return <div className={`reader-definition-card${expanded ? " is-expanded" : ""}`}>
    <div ref={setElement} className={`reader-definition${expanded ? " reader-definition-full" : ""}`}><p>{expanded ? text : overview}</p></div>
    {captions.length && onTable ? <div className="reader-definition-tables">{[...new Set(captions.map(match=>match.table))].map(table=><button type="button" key={table} className="link" onClick={()=>onTable(`table/${table}`)}>View Table {table}</button>)}</div> : null}
    {hasMore ? <IconButton className="reader-action reader-definition-toggle" icon={expanded ? "up" : "down"} label={expanded ? "Collapse definition" : "Expand definition"} expanded={expanded} onClick={onToggle} /> : null}
  </div>;
}
