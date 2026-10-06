import type { ReactNode } from "react";
import type { Inspection, InspectorNode, Hl7referenceAttribute, Hl7referenceRecord } from "./bindings";
import { FIELD_STATES, HIDDEN_VALUE } from "./display";
import { ReaderIcon } from "./ReaderIcon";

const pathLabel = (path: string) => path.replace(/\[1\]/g, "");

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
  const parent = selected.kind === "component" || selected.kind === "subcomponent" || selected.kind === "repetition" ? nodes.find(row => row.node.path === selected.parent) : undefined;
  const title = inspection.reference?.record?.name || inspection.metadata.label || inspection.segment_name || selected.segment || "Message";
  return <div className="reader-selection-context">
    {parent ? <div className="reader-parent-context"><code>{pathLabel(parent.node.path)}</code><code className="reader-parent-value">{parent.node.state === "present" ? inspection.revealed ? parent.raw ?? parent.value : HIDDEN_VALUE : FIELD_STATES[parent.node.state as Exclude<keyof typeof FIELD_STATES, "">] ?? parent.node.state}</code><span>{parent.label}</span>{parent.reference ? <ReferenceBadge attribute={parent.reference.datatype} /> : null}</div> : null}
    <div className="reader-location">
      {selected.kind !== "segment" && selected.path ? <div className="reader-location-navigation" role="group" aria-label="Selected position navigation">
        <button type="button" aria-label="Previous position" disabled={busy || !previous} onClick={() => previous && onSelect(previous.node.path)}><ReaderIcon name="previous" /></button>
        {!parent ? <button type="button" aria-label="Go to field" disabled={busy} onClick={onGoToField}><ReaderIcon name="position" /></button> : null}
        <button type="button" aria-label="Next position" disabled={busy || !next} onClick={() => next && onSelect(next.node.path)}><ReaderIcon name="next" /></button>
      </div> : null}
      <code title={selected.path} aria-label={selected.path || "Message"}>{pathLabel(selected.path) || "Message"}</code>{selected.path ? <span aria-hidden="true">—</span> : null}{selected.path ? <h4>{title}</h4> : null}
    </div>
  </div>;
}

export function ReferenceDefinition({ text, expanded, onToggle, compact }: { text: string; expanded: boolean; onToggle: () => void; compact: string }) {
  return <div className={`reader-definition-card${expanded ? " is-expanded" : ""}`}>
    <div className={`reader-definition${expanded ? " reader-definition-full" : ""}`}><p>{expanded ? text : compact}</p></div>
    <button type="button" className="link reader-definition-toggle" aria-label={expanded ? "Collapse definition" : "Expand definition"} aria-expanded={expanded} onClick={onToggle}><span className={expanded ? "reader-icon-up" : ""}><ReaderIcon name="showMore" /></span>{expanded ? "Show less" : "Show more"}</button>
  </div>;
}
