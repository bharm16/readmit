import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type FocusEvent, type KeyboardEvent, type MouseEvent, type RefObject } from "react";
import { createPortal } from "react-dom";
import { lookupHL7Reference, type HL7ReferenceResult, type Hl7referenceRecord, type Hl7referencePreviewAttribute } from "./bindings";
import { ReaderIcon } from "./ReaderIcon";
import "./reference-hover.css";

export type ReferenceAttribute = Hl7referencePreviewAttribute;
export type ReferenceHoverScope = {
  owner: string;
  catalog: string;
  identity: string;
  edition: string;
  messageEdition: string;
  onOpen: (key: string, query: string) => void;
  onChoose: () => void;
};
export type ReferenceHoverTarget = {
  anchor: HTMLButtonElement;
  path: string;
  displayPath: string;
  label: string;
  recordKey: string;
  attribute: ReferenceAttribute;
  value: string;
};
type OwnedTarget = ReferenceHoverTarget & { owner: string };

/** One delayed card per grid, including grids with thousands of virtual rows.
 * Pointer movement does not select a row or read a reference until it settles. */
export function useReferenceHover(scope?: ReferenceHoverScope) {
  const [target, setTarget] = useState<OwnedTarget | null>(null);
  const current = useRef<OwnedTarget | null>(null);
  const pending = useRef<OwnedTarget | null>(null);
  const owner = useRef(scope?.owner);
  owner.current = scope?.owner;
  const opening = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const closing = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const panel = useRef<HTMLDivElement>(null);
  const id = useId();
  const cancelClose = useCallback(() => clearTimeout(closing.current), []);
  const close = useCallback((restoreFocus = false) => {
    clearTimeout(opening.current); clearTimeout(closing.current);
    const anchor = current.current?.anchor;
    if (restoreFocus && (panel.current?.contains(document.activeElement) || anchor === document.activeElement)) {
      (anchor?.closest<HTMLElement>('[role="treegrid"]') || anchor)?.focus({ preventScroll: true });
    }
    current.current = null; pending.current = null; setTarget(null);
  }, []);
  const show = useCallback((next: ReferenceHoverTarget, immediate = false) => {
    if (!owner.current) return;
    clearTimeout(opening.current); clearTimeout(closing.current);
    if (current.current?.anchor === next.anchor && current.current.owner === owner.current) return;
    const owned = { ...next, owner: owner.current };
    pending.current = owned;
    current.current = null; setTarget(null);
    const open = () => {
      if (owner.current !== owned.owner || !owned.anchor.isConnected) return;
      pending.current = null; current.current = owned; setTarget(owned);
    };
    if (immediate) open(); else opening.current = setTimeout(open, 350);
  }, []);
  const leave = useCallback(() => {
    clearTimeout(opening.current);
    pending.current = null;
    if (panel.current?.contains(document.activeElement) || current.current?.anchor === document.activeElement) return;
    closing.current = setTimeout(() => close(), 180);
  }, [close]);
  const blur = useCallback((event: FocusEvent) => {
    const next = event.relatedTarget;
    if (next instanceof Node && (panel.current?.contains(next) || current.current?.anchor === next)) return;
    close();
  }, [close]);
  const keyDown = useCallback((event: KeyboardEvent) => {
    if (event.key === "Escape" && current.current) { event.preventDefault(); event.stopPropagation(); close(true); }
    if (event.key === "Tab" && !event.shiftKey && current.current?.anchor === event.target) {
      const first = panel.current?.querySelector<HTMLElement>('input, button:not(:disabled), a[href]');
      if (first) { event.preventDefault(); first.focus(); }
    }
  }, [close]);
  useEffect(() => { close(); return () => { clearTimeout(opening.current); clearTimeout(closing.current); }; }, [scope?.owner, close]);
  const visible = target && target.owner === scope?.owner ? target : null;
  useEffect(() => {
    if (!scope?.owner) return;
    const outside = (event: PointerEvent) => {
      const anchor = current.current?.anchor || pending.current?.anchor;
      if (anchor && event.target instanceof Node && !panel.current?.contains(event.target) && !anchor.contains(event.target)) close();
    };
    const scrolled = (event: Event) => { if ((current.current || pending.current) && (!(event.target instanceof Node) || !panel.current?.contains(event.target))) close(); };
    const escaped = (event: globalThis.KeyboardEvent) => {
      if ((current.current || pending.current) && event.key === "Escape" && !event.defaultPrevented) { event.preventDefault(); close(true); }
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("scroll", scrolled, true);
    document.addEventListener("keydown", escaped);
    return () => { document.removeEventListener("pointerdown", outside); document.removeEventListener("scroll", scrolled, true); document.removeEventListener("keydown", escaped); };
  }, [scope?.owner, close]);
  return {
    show, leave, blur, keyDown, close,
    activeAnchor: visible?.anchor,
    id,
    card: visible && scope ? createPortal(<ReferenceHoverCard key={`${visible.owner}/${visible.path}/${visible.attribute}`} id={id} scope={scope} target={visible} panel={panel} onClose={close} onEnter={cancelClose} onLeave={leave} onBlur={blur} />, document.body) : null,
  };
}

const attributeNames: Record<ReferenceAttribute, string> = { datatype: "Data type", optionality: "Optionality", length: "Length", conformance_length: "Conformance length", repetition: "Repetition", item: "Data element", table: "Table", section: "Section" };
// THO utg-concept-properties defines v2-binding as a realm, not a validation strength.
const bindingNames: Record<string, string> = { "0": "No binding / not applicable", "1": "Example", "2": "Representative", "3": "Universal", "4": "US Realm" };
const sourceURLPattern = /^https?:\/\/terminology\.hl7\.org\/CodeSystem\/v2-\d{4}$/;

function ReferenceHoverCard({ id, scope, target, panel, onClose, onEnter, onLeave, onBlur }: {
  id: string; scope: ReferenceHoverScope; target: OwnedTarget; panel: RefObject<HTMLDivElement | null>;
  onClose: (restoreFocus?: boolean) => void; onEnter: () => void; onLeave: () => void; onBlur: (event: FocusEvent) => void;
}) {
  const [answer, setAnswer] = useState<HL7ReferenceResult | null>(null);
  const [problem, setProblem] = useState("");
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [offset, setOffset] = useState(0);
  const [answered, setAnswered] = useState({ query: "", offset: -1 });
  const [copied, setCopied] = useState("");
  const [position, setPosition] = useState<{ left: number; top: number; maxHeight: number } | null>(null);
  const body = useRef<HTMLDivElement>(null);
  const copyAlive = useRef(true);
  useEffect(() => { copyAlive.current = true; return () => { copyAlive.current = false; }; }, []);
  useEffect(() => { const timer = setTimeout(() => setSearch(query), 200); return () => clearTimeout(timer); }, [query]);
  useEffect(() => {
    let active = true;
    setLoading(true); setProblem(""); setCopied("");
    if (!scope.catalog || !scope.identity || !target.recordKey) {
      setAnswer(null); setLoading(false); setProblem(!scope.catalog ? "The app could not read definitions for this HL7 version." : "No reference record is available for this position.");
      return;
    }
    void lookupHL7Reference({ catalog: scope.catalog, identity: scope.identity, edition: scope.edition, key: target.recordKey, attribute: target.attribute, offset, limit: target.attribute === "datatype" ? 5 : 100, ...(search ? { query: search } : {}) })
      .then(result => {
        if (!active) return;
        if (result.state !== "completed" || result.reference?.identity !== scope.identity || result.reference.edition !== scope.edition) {
          setAnswer(null); setProblem(result.reason || "The selected reference is not available.");
        } else { setAnswer(result); setAnswered({ query: search, offset }); }
        setLoading(false);
      }).catch(error => { if (active) { setAnswer(null); setProblem(error instanceof Error ? error.message : "The reference could not be read."); setLoading(false); } });
    return () => { active = false; };
  }, [scope.catalog, scope.identity, scope.edition, target.recordKey, target.attribute, offset, search]);
  useLayoutEffect(() => {
    const node = panel.current;
    if (!node) return;
    const place = () => {
      if (!target.anchor.isConnected) { onClose(); return; }
      const anchor = target.anchor.getBoundingClientRect(), card = node.getBoundingClientRect();
      const margin = 12, gap = 8;
      const above = Math.max(0, anchor.top - margin - gap), below = Math.max(0, window.innerHeight - anchor.bottom - margin - gap);
      const topSide = card.height <= above || card.height > below && above >= below;
      const maxHeight = Math.max(96, topSide ? above : below);
      const next = { left: Math.max(margin, Math.min(anchor.left - 4, window.innerWidth - margin - card.width)), top: Math.max(margin, topSide ? anchor.top - gap - Math.min(card.height, maxHeight) : anchor.bottom + gap), maxHeight };
      setPosition(current => current?.left === next.left && current.top === next.top && current.maxHeight === next.maxHeight ? current : next);
    };
    place();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(place);
    observer?.observe(node);
    window.addEventListener("resize", place);
    return () => { observer?.disconnect(); window.removeEventListener("resize", place); };
  }, [panel, target, answer, problem, loading, onClose]);
  const preview = answer?.preview, reference = answer?.reference, record = reference?.record;
  const ready = Boolean(record && preview);
  const type = preview?.kind || target.attribute;
  const table = type === "table" && ready;
  const busy = loading || query !== search || answered.query !== query || answered.offset !== offset;
  const kind = table ? record?.table_kind === "user" ? "User-defined" : record?.table_kind === "hl7" ? "HL7 table" : "Table"
    : type === "datatype" ? (answer?.total_count ?? 0) > 0 ? "Composite" : "Data type"
      : type === "element" ? "Data element" : type === "section" ? "Section" : "Reference";
  const code = preview?.code || target.value;
  const title = ready ? preview!.title : loading ? "Loading reference…" : "Reference unavailable";
  const citation = record?.section.state === "specified" ? `§${record.section.value} · HL7 reference` : `HL7 ${scope.edition || "edition not selected"} · HL7 reference`;
  const copy = async (text: string, label: string) => {
    try {
      if (window.runtime?.ClipboardSetText) {
        if (!await window.runtime.ClipboardSetText(text)) throw new Error("Clipboard is unavailable.");
      } else {
        if (!navigator.clipboard?.writeText) throw new Error("Clipboard is unavailable.");
        await navigator.clipboard.writeText(text);
      }
      if (copyAlive.current) setCopied(`Copied ${label}.`);
    } catch { if (copyAlive.current) setCopied("Copy failed. Try again."); }
  };
  const open = (key: string) => { onClose(); scope.onOpen(key, search); };
  const keys = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(true); return; }
    if (event.key !== "Tab") return;
    const items = [...(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled), input, a[href], summary') ?? [])];
    if (event.shiftKey && event.target === items[0]) { event.preventDefault(); target.anchor.focus(); }
    else if (!event.shiftKey && event.target === items.at(-1)) { event.preventDefault(); target.anchor.focus(); onClose(); }
  };
  const changePage = (next: number) => { setOffset(next); body.current?.scrollTo?.({ top: 0 }); };
  const focusAction = (event: MouseEvent<HTMLDivElement>) => {
    if (event.button !== 0 || !(event.target instanceof Element)) return;
    const action = event.target.closest<HTMLElement>('button, a[href], summary');
    if (!action || !event.currentTarget.contains(action)) return;
    // WebKit does not focus buttons on mouse-down by default. Preserve the
    // trigger-to-card focus handoff before blur can remove the clicked action.
    event.preventDefault();
    action.focus({ preventScroll: true });
  };
  return <div ref={panel} id={id} role="dialog" aria-label={`${attributeNames[target.attribute]} reference for ${target.displayPath}`} className={`reference-hover reference-hover-${type}`} style={position ? position : { visibility: "hidden" }} onPointerEnter={onEnter} onPointerLeave={onLeave} onBlur={onBlur} onKeyDown={keys} onMouseDownCapture={focusAction}>
    <header className="reference-hover-identity"><code>{code}</code><span>{kind}</span><span className="reference-hover-edition">{scope.edition ? `HL7 ${scope.edition}` : "No reference"}</span></header>
    <h3>{title}</h3>
    <p className="reference-hover-context">{target.displayPath}{target.label ? ` · ${target.label}` : ""}</p>
    {scope.edition && scope.messageEdition && scope.edition !== scope.messageEdition ? <p className="reference-hover-context">Reference HL7 {scope.edition} · Message HL7 {scope.messageEdition}</p> : null}
    <div ref={body} className="reference-hover-body" aria-busy={busy}>
      {!ready ? <p role="status">{loading ? "Reading the selected local catalog…" : problem || reference?.reason || "No definition is available in this catalog."}</p> : <>
        {table ? <TableContext record={record!} onCopy={copy} /> : <>
          {preview!.definition ? <p className="reference-hover-definition">{preview!.definition}</p> : <p className="reference-hover-muted">Definition not available in this catalog.</p>}
          {type === "datatype" && (answer!.total_count === 0 || preview!.maximum_length) ? <div className="reference-hover-inset">{answer!.total_count === 0 ? <strong>No component records in this catalog</strong> : null}{preview!.maximum_length ? <p>Maximum Length: {preview!.maximum_length}</p> : null}{preview!.context?.length.state === "specified" ? <small>At {target.displayPath} · field length {preview!.context.length.value}</small> : null}</div> : null}
          {type === "datatype" && answer!.total_count > 0 ? <section className="reference-hover-components"><h4>Components · {answer!.total_count}</h4>{answer!.children.map(child => <div className="reference-hover-component" key={child.key}><span>{child.position}</span><span>{child.name}</span><code>{child.datatype.state === "specified" ? child.datatype.value : "—"}</code></div>)}<p className="reference-hover-muted">Showing {answer!.children.length} of {answer!.total_count} components</p></section> : null}
          {(type === "datatype" || type === "tables") && preview!.tables?.length ? <section className="reference-hover-inset"><h4>{preview!.tables.length === 1 ? "Table for this field" : "Tables for this field"}</h4>{preview!.tables.map(item => <div key={item.key}><button type="button" className="reference-hover-related" onClick={() => open(item.key)}><code>{item.code}</code> {item.name}</button><small>{item.kind === "user" ? "User-defined table" : item.kind === "hl7" ? "HL7 table" : "Table"}</small></div>)}</section> : null}
          {type === "element" ? <><p className="reference-hover-datatype"><code>{record!.datatype.state === "specified" ? record!.datatype.value : "—"}</code>{preview!.datatype_name ? ` ${preview!.datatype_name}` : ""}</p><section className="reference-hover-inset"><h4>Used in {record!.uses?.length ?? 0} field{record!.uses?.length === 1 ? "" : "s"}</h4>{preview!.uses?.map((use,index) => use.key ? <button type="button" className="reference-hover-related" key={use.key} onClick={() => open(use.key)}>{use.code}</button> : <small key={index}>{use.name}</small>)}{preview!.context?.length.state === "specified" ? <small>At {target.displayPath} · field length {preview!.context.length.value}</small> : null}</section></> : null}
        </>}
        {table && record!.content_state === "not_specified" ? <section><h4>No suggested values</h4><p>HL7 does not supply suggested values for this table. Values are defined by the site.</p></section>
          : table && record!.content_state && record!.content_state !== "available" ? <p role="status">{reference?.reason || "Table content is not available."}</p>
          : table ? <section className="reference-hover-values"><label className="visually-hidden" htmlFor={`${id}-search`}>Search table values</label><div className="reference-hover-search"><input id={`${id}-search`} value={query} maxLength={128} placeholder="Search codes or descriptions…" onChange={event => { setQuery(event.target.value); setOffset(0); }} />{query ? <button type="button" aria-label="Clear search" onClick={() => { setQuery(""); setOffset(0); }}><ReaderIcon name="close" /></button> : null}</div><h4>{search ? `${answer!.child_count} of ${answer!.total_count} values` : `Values · ${answer!.total_count} total`}</h4>{busy ? <p role="status">Searching…</p> : <><div className="reference-hover-codes">{answer!.children.map(child => <div className="reference-hover-code" key={child.key}><button type="button" className="reference-hover-code-copy" aria-label={`Copy code ${child.code}`} title={`Copy code ${child.code}`} onClick={() => void copy(child.code || "", `code ${child.code}`)}>{child.code}</button><span>{child.definition || (child.name !== child.code ? child.name : "Meaning not available")}</span><button type="button" className="reference-hover-copy" aria-label={`Copy description for ${child.code}`} title="Copy description" onClick={() => void copy(child.definition || child.name, "description")}><ReaderIcon name="copy" /></button></div>)}</div>{answer!.child_count === 0 ? <p>No codes or descriptions match “{search}”.</p> : null}{answer!.child_count > 100 ? <nav className="reference-hover-pages" aria-label="Hover code pages"><span>{offset + 1}–{offset + answer!.children.length} of {answer!.child_count}</span><button type="button" disabled={offset === 0} onClick={() => changePage(Math.max(0, offset - 100))}>Previous</button><button type="button" disabled={offset + answer!.children.length >= answer!.child_count} onClick={() => changePage(offset + 100)}>Next</button></nav> : null}</>}</section> : null}
      </>}
    </div>
    <footer><p className="reference-hover-muted">{citation}</p>{preview?.target_key && ready ? <button type="button" className="reference-hover-open" onClick={() => open(preview.target_key!)}>{type === "section" ? "Open section" : "Open reference"}<span aria-hidden="true">→</span></button> : !scope.catalog || problem ? <button type="button" className="reference-hover-open" onClick={() => { onClose(); scope.onChoose(); }}>Choose HL7 version<span aria-hidden="true">→</span></button> : null}<span className="reference-hover-feedback" role="status">{copied}</span></footer>
  </div>;
}

function TableContext({ record, onCopy }: { record: Hl7referenceRecord; onCopy: (value: string, label: string) => Promise<void> }) {
  const metadata = record.table_metadata;
  const sourceURL = metadata?.code_system_url;
  const openSource = () => {
    if (!sourceURL || !sourceURLPattern.test(sourceURL)) return;
    if (window.runtime?.BrowserOpenURL) window.runtime.BrowserOpenURL(sourceURL);
    else window.open(sourceURL, "_blank", "noopener,noreferrer");
  };
  return <>
    {metadata?.description || metadata?.binding ? <section className="reference-hover-inset reference-hover-terminology"><details><summary>HL7 Terminology</summary>{metadata.description ? <p>{metadata.description}</p> : <p>Terminology description is not included in this catalog.</p>}{metadata.code_system_version ? <small>Code system version {metadata.code_system_version}</small> : null}</details>{sourceURL && sourceURLPattern.test(sourceURL) ? <button type="button" className="reference-hover-source" aria-label="Open terminology source" onClick={openSource}>↗</button> : null}{metadata.binding ? <small>Binding · {bindingNames[metadata.binding] || `Code ${metadata.binding}`}</small> : <small>Binding scope is not included in this catalog.</small>}</section>
      : <section className="reference-hover-inset"><strong>Terminology context unavailable</strong><p className="reference-hover-muted">{metadata ? "The catalog includes identifiers, but no terminology description or binding scope." : "Terminology metadata is not included in this catalog."}</p></section>}
    {metadata ? <div className="reference-hover-identifiers">{([["OID", "table OID", metadata.table_oid], ["CS", "code system OID", metadata.code_system_oid], ["VS", "value set OID", metadata.value_set_oid]] as const).filter(([, , value]) => Boolean(value)).map(([label, full, value]) => <div key={label} className={`reference-hover-identifier reference-hover-${label.toLowerCase()}`}><span>{label}</span><code>{value}</code><button type="button" className="reference-hover-copy" aria-label={`Copy ${full}`} title={`Copy ${full}`} onClick={() => void onCopy(value!, full)}><ReaderIcon name="copy" /></button></div>)}</div> : null}
  </>;
}
