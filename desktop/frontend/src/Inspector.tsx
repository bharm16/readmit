import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import type { DatasetValue, FHIRCheckPreset, FHIRInspection, FieldState, Inspection, InspectionResult, InspectorNode, RawWindow, Hl7referenceAttribute, Hl7referenceRecord, Hl7referenceOrigin, Hl7referenceSource, HL7ReferenceResult, HL7ReferenceSelection, ProfileevalNode } from "./bindings";
import { chooseInspectionPath, readReferenceCatalog, lookupHL7Reference, readHL7ReferenceSelection } from "./bindings";
import { FIELD_STATES, HIDDEN_VALUE } from "./display";
import { TaskPanel, TaskTabs } from "./TaskTabs";
import { BackLink, FormDialog, Menu, Modal, Reveal, ValueRows, type SubmitFailure } from "./layout";
import { IconButton } from "./IconButton";
import { DIRECTION_NAMES, observedInstant, typeLabel } from "./Messages";
import { useMeasured } from "./measure";
import closeAsset from "./assets/workbench/close.svg";
import downAsset from "./assets/workbench/down.svg";
import moreAsset from "./assets/workbench/more.svg";
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

function readablePath(path:string):string { return path.replace(/\[1\]/g, ""); }

function attributeText(attribute?: Hl7referenceAttribute): string {
  if (!attribute) return "Not available";
  if (attribute.state === "specified") return attribute.value;
  return ({not_specified:"Not specified", not_applicable:"Not applicable", not_available:"Not available"} as Record<string,string>)[attribute.state] || "Not available";
}
function decodeStateText(state: string): string {
  return ({too_large:"Truncated", unsupported_encoding:"Undecodable: unsupported encoding", unsupported_escape:"Undecodable: malformed or unsupported escape", invalid_encoding:"Undecodable: invalid encoding", structural:"Choose a field or value", omitted:"Omitted", empty:"Empty", null:"Null"} as Record<string,string>)[state] || "Not available";
}
function childValueText(child: InspectorNode, revealed: boolean): string {
  if (child.node.state !== "present") return stateText(child.node.state, revealed, "");
  if (!revealed) return HIDDEN_VALUE;
  if (child.decode_state && PROBLEMS.has(child.decode_state)) return decodeStateText(child.decode_state);
  return child.value + (child.truncated ? " · Truncated" : "");
}
function referenceCaption(record:Hl7referenceRecord):string {
  return record.table_id || record.item_id || record.container || record.structure_id || (record.message_code ? record.message_code+(record.event ? "^"+record.event : "") : "") || record.code || record.key;
}

function compactDefinition(definition:string):string {
  const text=definition.trim();
  const marker=/\bDefinition:\s*/.exec(text);
  return marker ? text.slice(marker.index+marker[0].length).trim() : text;
}

function referenceRows(record: Hl7referenceRecord) {
  return [
    {label:"Data type",value:attributeText(record.datatype)}, {label:"Opt",value:attributeText(record.optionality)},
    {label:"Len",value:attributeText(record.length)}, {label:"C-Len",value:attributeText(record.conformance_length)},
    {label:"Rep",value:attributeText(record.repetition)}, {label:"Item#",value:attributeText(record.item)},
    {label:"Tbl",value:attributeText(record.table)}, {label:"Sect",value:attributeText(record.section)},
  ];
}

function referenceOriginText(origin:Hl7referenceOrigin|undefined,sources?:Hl7referenceSource[]) {
  if(!origin?.kind)return "Origin not recorded in this catalog";
  const kind=({normative:"Normative chapter",schema:"Messaging schema",identity:"Identifier",derived:"Derived explanation",not_available:"Not available",not_applicable:"Not applicable"} as Record<string,string>)[origin.kind]||"Not available";
  const source=sources?.find(source=>source.role===origin.source);
  return [kind,source?.file,origin.locator].filter(Boolean).join(" · ");
}
function ReferenceOrigins({record,sources}:{record:Hl7referenceRecord;sources?:Hl7referenceSource[]}) {
 return <details><summary>Attribute origins</summary><ValueRows rows={[
  {label:"Name origin",value:referenceOriginText(record.name_origin,sources)},
  ...([["Type",record.datatype],["Opt",record.optionality],["Len",record.length],["C-Len",record.conformance_length],["Rep",record.repetition],["Item#",record.item],["Tbl",record.table],["Sect",record.section]] as const).map(([label,value])=>({label:label+" origin",value:referenceOriginText(value.origin,sources)})),
  {label:"Definition origin",value:referenceOriginText(record.definition_origin,sources)},
 ]}/><p>Chapter notation and schema fallbacks are separate. An entity citation does not establish every attribute's origin.</p></details>;
}

function StructureOutline({nodes}:{nodes:ProfileevalNode[]}) {
  return <ul className="reader-structure-outline" aria-label="Reference structure">{nodes.map((node,index) => <li key={`${node.name}/${index}`}><strong>{node.segment || node.name}</strong> · {node.min}..{node.max}{node.choice ? " · Choice" : ""}{node.children?.length ? <StructureOutline nodes={node.children} /> : null}</li>)}</ul>;
}

/** The shared reader of one message: Fields, Raw and Hex over the same
 * verified occurrence. Case messages, Timeline, Findings and a standalone file
 * all read a message through this one view. Values stay hidden until Show
 * values; revealing applies to what is open now and never reaches a list,
 * a query or a log. */
export function MessageReader({
  result,
  referenceCatalog: suppliedCatalog = "",
 referenceIdentity: suppliedIdentity = "",
  referenceSelection: suppliedSelection,
  referenceRequest = 0,
  toolbarReference = false,
  compactReader,
  onCompactReader,
  contextDetails,
  contextDetailsTitle = "Details",
  kind,
  source,
  loading,
  busy,
  onInspect,
  onReveal,
  onFilterByField,
  onFieldValues,
  onRequirements,
  onValidationFindings,
  onCreateVariant,
  onValueMaps,
  onInspectResource,
  onCreateFHIRCheck,
  onClose,
  backLabel,
  onBack,
}: {
  result: InspectionResult | null;
  referenceCatalog?: string;
 referenceIdentity?: string;
  referenceSelection?: HL7ReferenceSelection;
  referenceRequest?: number;
  toolbarReference?: boolean;
  compactReader?: boolean;
  onCompactReader?: () => void;
  /** Explicit, source-fenced contextual content published by the workflow owner. */
  contextDetails?: ReactNode;
  contextDetailsTitle?: string;
  /** The occurrence kind the list names; the header names ACK and Unparsed. */
  kind?: "message" | "ack" | "unparsed";
  /** The source the list names the message by. */
  source?: string;
  loading: boolean;
  busy: boolean;
  onInspect: (path: string, nodeOffset: number, byteOffset: number, rawOffset?: number, referenceCatalog?: string, selection?: HL7ReferenceSelection,referenceIdentity?: string) => Promise<InspectionResult | null>;
  onReveal: (revealed: boolean) => void;
  /** Opens Filter with a rule for the selected field; absent where there is no list to filter. */
  onFilterByField?: ((selector: string, value: string | null, state: FieldState) => void) | undefined;
  onFieldValues?: (selector: string) => void;
  onRequirements?: (selector: string) => void;
  onValidationFindings?: () => void;
  onCreateVariant?: () => void;
  onValueMaps?: (selector:string) => void;
  /** Navigation to an occurrence Go resolved inside this retained source. */
  onInspectResource?: ((occurrence: string) => void) | undefined;
  /** A typed dataset check over the exact Go projection and source binding. */
  onCreateFHIRCheck?: ((preset: FHIRCheckPreset) => void) | undefined;
  onClose?: (() => void) | undefined;
  backLabel?: string | undefined;
  onBack?: (() => void) | undefined;
}) {
  const [view, setView] = useState<ReaderView>("fields");
  const [knownSegments,setKnownSegments]=useState<{owner:string;nodes:InspectorNode[]}|null>(null);
  const [knownControlID,setKnownControlID]=useState<{owner:string;value:string}|null>(null);
  const returnGridFocus=useRef(false);
  const selectView=(next:ReaderView)=>{returnGridFocus.current=next==="fields" && view!=="fields";setView(next);};
  const gridElement=useRef<HTMLElement|null>(null);
  useEffect(()=>{if(view==="fields" && returnGridFocus.current){gridElement.current?.focus();returnGridFocus.current=false;}},[view]);
  const [workbenchElement, setWorkbenchElement] = useState<HTMLDivElement | null>(null);
  const workbenchSize = useMeasured(workbenchElement);
  const compactWorkbench = workbenchSize.width > 0 && workbenchSize.width / workbenchSize.rem < 42;
  const [goingTo, setGoingTo] = useState(false);
  const [info, setInfo] = useState(false);
  const [path, setPath] = useState("");
  const [catalogPath, setCatalogPath] = useState("");
 const [catalogIdentity,setCatalogIdentity]=useState("");
  const [catalogProblem, setCatalogProblem] = useState("");
  const [catalogReading, setCatalogReading] = useState(false);
  const [fullDefinition, setFullDefinition] = useState(false);
  const referenceSerial = useRef(0);
  const referenceContext = useRef("");
  const referenceOwner = useRef("");
  const lookupSerial = useRef(0);
  const lookupOwner = useRef("");
  const [referenceView, setReferenceView] = useState<HL7ReferenceResult | null>(null);
  const [referenceLoading, setReferenceLoading] = useState(false);
  const [lookupDefinitionExpanded,setLookupDefinitionExpanded]=useState(false);
  const [referenceKey, setReferenceKey] = useState("");
  const [referenceTrail, setReferenceTrail] = useState<{key:string;offset:number;query:string}[]>([]);
  const [codeQuery, setCodeQuery] = useState("");
  const [activeQuery, setActiveQuery] = useState("");
  const [columnGuide, setColumnGuide] = useState(false);
  const [validationShown,setValidationShown]=useState(false);
  const referenceTabFocus=useRef(false);
  const [overlaySelection, setOverlaySelection] = useState<HL7ReferenceSelection | undefined>();
  const [overlayProblem, setOverlayProblem] = useState("");
  const overlaySerial=useRef(0);
  const inspection = result?.inspection;
  const selected = inspection?.selected;
  const referencePin=catalogIdentity || suppliedIdentity || inspection?.reference?.identity || undefined;
  const contextKey = `${inspection?.identity ?? ""}/${inspection?.occurrence ?? inspection?.message ?? ""}/${selected?.path ?? ""}/${inspection?.revealed ?? false}/${inspection?.reference_overlay?.selection.profile_identity ?? ""}/${inspection?.reference_overlay?.selection.pack_identity ?? ""}/${inspection?.reference_overlay?.selection.documentation_identity ?? ""}`;
  referenceContext.current = contextKey;
  useEffect(()=>setValidationShown(false),[inspection?.identity,inspection?.occurrence,inspection?.message]);
  const lookupContext = `${contextKey}/${referencePin ?? ""}/${catalogPath || suppliedCatalog}/${inspection?.reference?.status ?? ""}`;
  lookupOwner.current = lookupContext;
  useEffect(() => { ++lookupSerial.current; setReferenceView(null); setReferenceLoading(false); setReferenceKey(""); setReferenceTrail([]); setCodeQuery(""); setActiveQuery(""); setColumnGuide(false); }, [lookupContext]);
  useEffect(() => { setCatalogProblem(""); setFullDefinition(false); }, [contextKey]);
  useEffect(() => {
    const identity = inspection?.identity;
    if (!identity) return; // A transient read clears its result, not its owner.
    if (referenceOwner.current && referenceOwner.current !== identity) {setCatalogPath("");setCatalogIdentity("");setOverlaySelection(undefined);setOverlayProblem("");}
    referenceOwner.current = identity;
  }, [inspection?.identity]);
  const revealed = inspection?.revealed ?? false;
  useEffect(() => {
    if (goingTo) setPath("");
  }, [goingTo]);

  const title = inspection
    ? typeLabel({ kind: kind ?? (inspection.decode_state === "unparsed" ? "unparsed" : "message"), code: inspection.message_code, trigger: inspection.trigger_event })
    : "Message";
  const facts = inspection
    ? [
        inspection.observed_at ? observedInstant(inspection.observed_at) : null,
        source ?? (inspection.source_name || inspection.source_id),
        inspection.direction && inspection.direction !== "unknown" ? DIRECTION_NAMES[inspection.direction] : null,
      ].filter(Boolean)
    : [];
  const controlIDNode=revealed ? inspection?.grid?.rows.find(row=>row.node.segment==="MSH" && row.node.kind==="field" && row.node.field===10 && row.node.state==="present" && !row.truncated && !PROBLEMS.has(row.decode_state || "")) : undefined;
  const captionOwner=`${inspection?.identity || ""}/${inspection?.occurrence || inspection?.message || 0}`;
  useEffect(()=>{if(!revealed){setKnownControlID(null);return;}if(controlIDNode?.value)setKnownControlID({owner:captionOwner,value:controlIDNode.value});},[captionOwner,revealed,controlIDNode?.value]);
  useEffect(()=>{if(selected?.path === "" && inspection?.children){setKnownSegments({owner:captionOwner,nodes:inspection.children.filter(row=>row.node.kind === "segment")});}},[captionOwner,selected?.path,inspection?.children]);
  const segmentShortcuts=selected?.path && selected.segment && knownSegments?.owner===captionOwner ? knownSegments.nodes.filter(row=>row.node.path !== selected?.path && row.node.segment !== selected?.segment) : [];
  const controlID=revealed ? controlIDNode?.value || (knownControlID?.owner===captionOwner ? knownControlID.value : "") : "";
  const editorCaption=controlID ? `${inspection?.message_code || title}${inspection?.trigger_event ? "^"+inspection.trigger_event : ""} · ${controlID}` : title;
  const atRoot = !selected || selected.path === "";
  const field = selected && selected.kind !== "segment" && selected.kind !== "message" && selected.kind !== "occurrence";

  const gridNodes: InspectorNode[] = inspection?.grid?.rows ?? (inspection?.reference && selected && selected.path !== "" ? [{
    node:selected, label:inspection.metadata.label, selector:inspection.selector, segment_name:inspection.segment_name,
    value:inspection.decoded, truncated:inspection.decode_state === "too_large", decode_state:inspection.decode_state,
    ...(inspection.reference?.record ? {reference:inspection.reference.record} : {}),
  }, ...inspection.children] : inspection?.children ?? []);
  useEffect(()=>{gridElement.current?.querySelector<HTMLElement>('[aria-current="location"]')?.scrollIntoView?.({block:"nearest",inline:"nearest"});},[selected?.path,view]);
  const inspectSelection = (target: string) => {
    // Fence the user's new selection intent before the asynchronous host reply
    // changes props. An older catalog/profile read must not navigate back.
    ++referenceSerial.current; ++overlaySerial.current; ++lookupSerial.current;
    setCatalogReading(false); setReferenceLoading(false); setReferenceView(null);
    setReferenceKey(""); setReferenceTrail([]);
    return onInspect(target,0,-1,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin);
  };
  const go = (target: string) => void inspectSelection(target);
  const chooseReference = async (expectedEdition?: string) => {
    const owner = contextKey;
    const serial = ++referenceSerial.current;
    setCatalogReading(true);
    setCatalogProblem("");
    try {
      const chosen = await chooseInspectionPath("reference-catalog");
      if (referenceContext.current !== owner || referenceSerial.current !== serial) return;
      if (chosen.state !== "completed" || !chosen.path) { if (chosen.state !== "cancelled") setCatalogProblem(chosen.reason || "The catalog could not be selected."); return; }
      const checked = await readReferenceCatalog(chosen.path);
      if (referenceContext.current !== owner || referenceSerial.current !== serial) return;
      if (checked.state !== "completed" || !checked.reference?.identity) { setCatalogProblem(checked.reason || "The catalog could not be read."); return; }
      if(expectedEdition && checked.reference?.edition !== expectedEdition) {setCatalogProblem(`Choose a catalog for the declared edition ${expectedEdition}.`);return;}
      setCatalogPath(chosen.path);
      setCatalogIdentity(checked.reference!.identity);
      await onInspect(selected?.path ?? "", inspection?.node_offset ?? 0, -1, undefined, chosen.path,overlaySelection || suppliedSelection,checked.reference!.identity);
    } catch { if (referenceContext.current === owner && referenceSerial.current === serial) setCatalogProblem("The catalog could not be read."); }
    finally { if (referenceSerial.current === serial) setCatalogReading(false); }
  };

  const handledReferenceRequest = useRef(referenceRequest);
  useEffect(()=>{if(referenceRequest !== handledReferenceRequest.current){handledReferenceRequest.current=referenceRequest;void chooseReference();}},[referenceRequest]);

  const referenceKeys=(event:KeyboardEvent<HTMLDivElement>)=>{
    const tabs=[...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]:not([disabled])')];
    const active=document.activeElement;
    const at=active instanceof HTMLButtonElement ? tabs.indexOf(active) : -1;
    if(at<0 || !tabs.length)return;
    const next=event.key==="ArrowRight" ? (at+1)%tabs.length : event.key==="ArrowLeft" ? (at+tabs.length-1)%tabs.length : event.key==="Home" ? 0 : event.key==="End" ? tabs.length-1 : -1;
    if(next<0)return;event.preventDefault();
    referenceTabFocus.current=event.currentTarget.getAttribute("aria-label")==="Reference views";
    tabs[next]?.click();tabs[next]?.focus();
  };
  useEffect(()=>{if(!referenceTabFocus.current)return;workbenchElement?.querySelector<HTMLButtonElement>('[aria-label="Reference views"] [aria-selected="true"]')?.focus();referenceTabFocus.current=false;},[referenceKey,referenceLoading,workbenchElement]);

  const chooseOverlay = async (kind: "reference-profile" | "reference-pack" | "reference-documentation") => {
    const owner=contextKey;const serial=++overlaySerial.current;
    setOverlayProblem("");
    const chosen=await chooseInspectionPath(kind);
    if (referenceContext.current!==owner||overlaySerial.current!==serial) return;
    if(chosen.state!=="completed"||!chosen.path) {if(chosen.state!=="cancelled")setOverlayProblem(chosen.reason||"The reference could not be selected.");return;}
    const next={...(overlaySelection || suppliedSelection || {})};
    if(kind==="reference-profile") {next.profile=chosen.path;delete next.profile_identity;}
    if(kind==="reference-pack") {next.pack=chosen.path;delete next.pack_identity;}
    if(kind==="reference-documentation") {next.documentation=chosen.path;delete next.documentation_identity;}
    const verified=await readHL7ReferenceSelection(next);
    if(referenceContext.current!==owner||overlaySerial.current!==serial)return;
    if(verified.state!=="completed"||!verified.overlay) {setOverlayProblem(verified.reason||"The reference selection could not be verified.");return;}
    setOverlaySelection(verified.overlay.selection);
    await onInspect(selected?.path ?? "",inspection?.node_offset ?? 0,-1,undefined,catalogPath || suppliedCatalog || undefined,verified.overlay.selection,referencePin);
  };

  const openReference = async (key: string, offset = 0, remember = true, query = "") => {
    if (!inspection?.reference) return;
    const owner = lookupContext;
    const serial = ++lookupSerial.current;
    if (remember && referenceKey && referenceKey !== key) setReferenceTrail((held) => [...held, {key:referenceKey,offset:referenceView?.offset ?? 0,query:activeQuery}]);
    if(referenceKey!==key)setLookupDefinitionExpanded(false);
    setReferenceKey(key);
    setCodeQuery(query);
    setActiveQuery(query);
    setReferenceView(null);
    setReferenceLoading(true);
    const answer = await lookupHL7Reference({catalog:catalogPath || suppliedCatalog, identity:inspection.reference.identity, edition:inspection.reference.edition, key, offset, limit:100, ...(query ? {query} : {})});
    if (lookupOwner.current !== owner || lookupSerial.current !== serial) return;
    setReferenceView(answer);
    setReferenceLoading(false);
  };
  const backReference = () => {
    const previous = referenceTrail.at(-1);
    if (previous) {
      setReferenceTrail((held) => held.slice(0,-1));
      void openReference(previous.key,previous.offset,false,previous.query);
    } else {
      ++lookupSerial.current;
      setReferenceView(null);
      setReferenceLoading(false);
      setReferenceKey("");
    }
  };

  if (inspection?.fhir) return <FHIRReader inspection={inspection} fhir={inspection.fhir} busy={busy} onInspect={onInspect} onReveal={onReveal} onInspectResource={onInspectResource} onCreateCheck={onCreateFHIRCheck} onClose={onClose} backLabel={backLabel} onBack={onBack} />;

  return (
    <section className="message-reader hl7-reader" aria-label="Message details">


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
          {catalogProblem ? <p role="alert">{catalogProblem}</p> : null}
          {overlayProblem ? <p role="alert">{overlayProblem}</p> : null}
          <div ref={setWorkbenchElement} className={`reader-workbench${compactWorkbench ? " compact-workbench" : ""}`}>
          <div className={`reader-center reader-view-${view}`}>
      <header className="reader-header reader-context">
        {backLabel && onBack ? <BackLink label={backLabel} onBack={onBack} /> : null}
        <div className="reader-heading">
          <h2>{editorCaption}</h2>
          {onClose ? <button className="reader-tab-close" type="button" aria-label="Close message details" onClick={onClose}><img className="workbench-icon" src={closeAsset} alt="" /></button> : null}
          {facts.length > 0 ? <p className="reader-facts">{facts.join(" · ")}</p> : null}
        </div>
        {onCompactReader ? <button type="button" className="link" onClick={onCompactReader}>{compactReader ? "Wide view" : "Compact view"}</button> : null}
        {inspection && !toolbarReference ? <button type="button" disabled={busy || catalogReading} onClick={() => void chooseReference()}>Reference</button> : null}
        {inspection ? (
          <Menu
            className="reader-menu" trigger={<img className="workbench-icon" src={moreAsset} alt="" />}
            label="More message actions"
            items={[
              {label:"Fields",onSelect:()=>selectView("fields")},
              {label:"Raw",onSelect:()=>selectView("raw")},
              {label:"Hex",onSelect:()=>selectView("hex")},
              ...(onRequirements ? [{label:"Validation",onSelect:()=>setValidationShown(true)}] : []),

          {label:"Choose local profile…",onSelect:() => void chooseOverlay("reference-profile"),disabled:busy},
          {label:"Choose exact profile pack…",onSelect:() => void chooseOverlay("reference-pack"),disabled:busy},
          {label:"Choose local documentation…",onSelect:() => void chooseOverlay("reference-documentation"),disabled:busy},
          {label:"Clear profile and documentation",onSelect:() => {++overlaySerial.current;setOverlaySelection({});void onInspect(selected?.path ?? "",inspection.node_offset,-1,undefined,catalogPath || suppliedCatalog || undefined,{},referencePin);},disabled:busy},

              ...(onRequirements ? [{label:"Interface requirements",onSelect:()=>onRequirements(inspection.selector||selected?.path||""),disabled:busy}] : []),
              ...(onValueMaps ? [{label:"Value maps",onSelect:()=>onValueMaps(inspection.selector),disabled:busy||!field||!inspection.selector}] : []),
              ...(onFieldValues ? [{label:"Field values",onSelect:()=>onFieldValues(inspection.selector),disabled:busy||!field||!inspection.selector}] : []),
              { label: "Go to field…", onSelect: () => setGoingTo(true), disabled: busy || inspection.decode_state === "unparsed" },
              { label: "Info", onSelect: () => setInfo(true) },
            ]}
          />
        ) : null}
      </header>
          {view === "fields" ? <section className="reader-original" aria-label="Original message"><h3>Original message <span>Read only</span></h3>{revealed && (inspection.readable_window || inspection.raw_window) ? <RawText window={inspection.readable_window || inspection.raw_window!} busy={busy} onPage={(offset) => void onInspect(selected.path, inspection.node_offset, inspection.byte_offset, offset, catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)} /> : <p className="reader-hidden">{HIDDEN_VALUE}</p>}</section> : null}
          <div className="reader-grid-controls"><h3>Segment grid</h3><button type="button" className="link" onClick={()=>setColumnGuide(!columnGuide)}>Column guide</button><span>Base HL7 {inspection.reference?.edition || "not selected"}</span></div>
          {view !== "fields" ? <TaskTabs tablistClass="reader-view-tabs" label="Message views" id="reader-views" tabs={VIEWS} selected={view} onSelect={selectView} panels>{null}</TaskTabs> : <span id="reader-views-tab-fields" hidden>Fields</span>}
          <>
            <TaskPanel tabs="reader-views" tab="fields" shown={view === "fields"}>
              {inspection.decode_state === "unparsed" ? (
                <div className="reader-empty">
                  <button type="button" onClick={() => selectView("raw")}>
                    View raw
                  </button>
                </div>
              ) : (
                <>
                  {gridNodes.length > 0 ? (
                    <section ref={gridElement} className="reader-grid-scroll" aria-label="Segment grid" tabIndex={0}>
                      <div className="reader-grid-columns" aria-hidden="true">{["Path", "Name", "Type", "Opt", "Len", "C-Len", "Rep", "Item#", "Tbl", "Sect", "Value"].map((column) => <span key={column}>{column}</span>)}</div>
                      <ul className="outline reader-grid" aria-label={atRoot ? "Segments" : `Parts of ${selected.path}`}>
                        {gridNodes.map((child) => <li key={child.node.path}>
                          <button type="button" className={`outline-row reader-grid-row reader-kind-${child.node.kind} ${child.node.path === selected.path ? "reader-grid-selected" : ""}`} aria-current={child.node.path === selected.path ? "location" : undefined} aria-label={`${child.node.path} ${child.node.kind === "segment" ? child.segment_name || child.node.segment : nodeName(child)} ${childValueText(child, revealed)}`} disabled={busy} onClick={() => go(child.node.path)}>
                            <span className="selector" title={child.node.path}><span aria-hidden="true">{child.node.kind === "segment" ? <img className="workbench-icon" src={downAsset} alt="" /> : null}</span>{readablePath(child.node.path)}</span>
                            <span className="outline-name"><span>{child.node.kind === "segment" ? child.segment_name || child.node.segment : nodeName(child)}</span></span>
                            {[child.reference?.datatype, child.reference?.optionality, child.reference?.length, child.reference?.conformance_length, child.reference?.repetition, child.reference?.item, child.reference?.table, child.reference?.section].map((attribute, index) => <span className="reader-attribute" key={index} title={attributeText(attribute)}>{attribute?.state === "specified" ? attribute.value : attribute?.state === "not_specified" || attribute?.state === "not_applicable" ? "—" : "?"}</span>)}
                            <span className={revealed && child.node.state === "present" ? "outline-value" : "outline-value outline-state"}>{childValueText(child, revealed)}</span>
                          </button>
                        </li>)}
                      </ul>
                    </section>
                  ) : null}
                  {inspection.grid && inspection.grid.field_count > 100 ? <nav className="pager" aria-label="Segment fields pages"><button type="button" disabled={busy || inspection.grid.offset === 0} onClick={() => void onInspect(inspection.grid!.segment,Math.max(0,inspection.grid!.offset-100),-1,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)}>Previous segment fields</button><span>{inspection.grid.offset+1}–{Math.min(inspection.grid.field_count,inspection.grid.offset+100)} of {inspection.grid.field_count}</span><button type="button" disabled={busy || inspection.grid.offset+100 >= inspection.grid.field_count} onClick={() => void onInspect(inspection.grid!.segment,inspection.grid!.offset+100,-1,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)}>Next segment fields</button></nav> : null}
                  {inspection.child_count > inspection.children.length ? (
                    <nav aria-label="Parts pages" className="pager">
                      <IconButton
                        icon="previous"
                        label="Previous parts"
                        disabled={busy || inspection.node_offset === 0}
                        onClick={() => void onInspect(selected.path, Math.max(0, inspection.node_offset - 100), inspection.byte_offset,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)}
                      />
                      <span>
                        {inspection.node_offset + 1}–{inspection.node_offset + inspection.children.length} of {inspection.child_count}
                      </span>
                      <IconButton
                        icon="next"
                        label="Next parts"
                        disabled={busy || inspection.node_offset + inspection.children.length >= inspection.child_count}
                        onClick={() => void onInspect(selected.path, inspection.node_offset + inspection.children.length, inspection.byte_offset,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)}
                      />
                    </nav>
                  ) : null}
                </>
              )}
            </TaskPanel>
            <TaskPanel tabs="reader-views" tab="raw" shown={view === "raw"}>
              {revealed && inspection.raw_window ? (
                <RawText window={inspection.raw_window} busy={busy} onPage={(offset) => void onInspect(selected.path, inspection.node_offset, inspection.byte_offset, offset,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)} />
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
                  onPage={(offset) => void onInspect(selected.path, inspection.node_offset, offset,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)}
                />
              ) : (
                <p className="reader-hidden">{HIDDEN_VALUE}</p>
              )}
            </TaskPanel>
          </>
          <footer className="reader-grid-footer">{segmentShortcuts.length ? <nav aria-label="Other segments" className="crumbs">{segmentShortcuts.map(row=><button key={row.node.path} type="button" title={row.node.path} aria-label={`Inspect ${row.node.path}`} disabled={busy} onClick={()=>go(row.node.path)}>{row.node.segment}</button>)}</nav> : null}<nav aria-label="Path" className="crumbs"><button type="button" disabled={busy} onClick={()=>go("")}>Segments</button>{selected.parent && selected.parent !== selected.path ? <button type="button" disabled={busy} onClick={()=>go(selected.parent)}>{selected.parent}</button> : null}</nav><span>— Not specified / not applicable · ? Not available · Empty ≠ Omitted</span></footer>
          </div>
          {contextDetails !== undefined && contextDetails !== null && !validationShown ? <section className="reader-reference" aria-label="Context details"><h3 className="reader-pane-heading">{contextDetailsTitle}</h3><div className="reader-reference-scroll">{contextDetails}</div></section> : inspection.reference ? <section className="reader-reference" aria-label="Reference details">{validationShown ? <div className="reader-pane-tabs" onKeyDown={referenceKeys} role="tablist" aria-label="Inspector views"><button type="button" role="tab" aria-selected={!validationShown} onClick={()=>setValidationShown(false)}>Details</button><button type="button" role="tab" aria-selected={validationShown} onClick={()=>setValidationShown(true)}>Validation</button></div> : <h3 className="reader-pane-heading">Details</h3>}<div className="reader-reference-scroll">
            {validationShown ? <section className="reader-validation" aria-label="Validation"><h4>Validation</h4><h5>No validation result available in this inspection</h5><p>Choose the specification used to check these messages. Reading reference metadata does not run validation.</p><button type="button" className="primary" disabled={busy} onClick={()=>onRequirements?.(inspection.selector || selected.path)}>Select specification</button><ValueRows rows={[{label:"Message",value:title},{label:"Field",value:inspection.selector || selected.path || "Message"}]} /><label>Original value</label><pre className="value">{selected.state === "present" ? revealed ? inspection.raw || decodeStateText(inspection.decode_state) : HIDDEN_VALUE : stateText(selected.state,revealed,"")}</pre>{onValidationFindings ? <button type="button" className="link" onClick={onValidationFindings}>View findings</button> : null}</section> : null}
            {!validationShown && !columnGuide && <>{referenceKey ? <>
              <h3 className="reader-reference-kind visually-hidden">{referenceKey.startsWith("table/") ? "Table reference" : referenceKey.startsWith("element/") ? "Data element" : referenceKey.startsWith("datatype/") ? "Datatype reference" : "Source definition"}</h3>
              <button type="button" className="link reader-reference-back" onClick={backReference}>{referenceTrail.length ? "Back to reference" : "Back to selected field"}</button>
              <details className="reader-extra-metadata reader-selected-evidence"><summary>Selected evidence</summary><p className="selector">Selected field: {selected.path || "Message"}</p></details>
              {referenceView?.reference?.record ? <><p className="selector reader-selected-path">{referenceCaption(referenceView.reference.record)}</p><h4 className="reader-selected-name">{referenceView.reference.record.name}</h4></> : null}
              <div className="reader-reference-tabs" onKeyDown={referenceKeys} role="tablist" aria-label="Reference views"><button type="button" role="tab" aria-selected="false" onClick={()=>{++lookupSerial.current;setReferenceTrail([]);setReferenceView(null);setReferenceKey("");setReferenceLoading(false);}}>Overview</button><button type="button" role="tab" aria-selected={referenceKey.startsWith("datatype/")} disabled={!inspection.reference.datatype_key && !inspection.reference.parent_datatype_key} onClick={()=>void openReference(inspection.reference!.datatype_key || inspection.reference!.parent_datatype_key!)}>Components</button><button type="button" role="tab" aria-selected={referenceKey.startsWith("element/") || referenceKey.startsWith("table/")} disabled={!inspection.reference.element_key && !inspection.reference.table_keys?.length} onClick={()=>void openReference(inspection.reference!.element_key || inspection.reference!.table_keys![0]!)}>{inspection.reference.element_key ? "Data element" : "Table"}</button></div>
              {referenceLoading ? <p role="status">Reading reference…</p> : null}
              {referenceView && referenceView.state !== "completed" ? <p role="alert">{referenceView.reason || "This reference is unavailable."}</p> : null}
              {referenceView?.reference?.reason ? <p role="status">{referenceView.reference.reason}</p> : null}
              {referenceView?.reference?.record ? <>

                {referenceView.reference.record.kind === "table" ? <><p>{referenceView.reference.record.table_kind || "Unknown"} table · {referenceView.total_count} extracted source codes</p>{referenceView.reference.record.unparsed_rows ? <p role="status">Partial table extraction: {referenceView.reference.record.unparsed_rows} source rows could not be interpreted. The receipt identifies each gap.</p> : null}</> : referenceView.reference.record.kind === "datatype" ? null : <ValueRows rows={referenceRows(referenceView.reference.record)} />}

                {referenceView.reference.record.kind === "element" ? <><p>Item number: {referenceView.reference.record.item_id}</p><p>Used by: {referenceView.reference.record.uses?.join(", ") || "Not available"}</p><p>Original message value: {selected.state === "present" ? revealed ? inspection.raw : HIDDEN_VALUE : stateText(selected.state,revealed,"")}</p></> : null}

                {referenceView.reference.record.sequence?.length ? <StructureOutline nodes={referenceView.reference.record.sequence} /> : null}
                <div className={`reader-definition${lookupDefinitionExpanded ? " reader-definition-full" : ""}${referenceView.reference.record.kind === "datatype" ? " reader-datatype-summary" : ""}`}><p>{referenceView.reference.record.definition ? lookupDefinitionExpanded ? referenceView.reference.record.definition : compactDefinition(referenceView.reference.record.definition) : "Definition not available."}</p></div>
                {referenceView.reference.record.definition ? <button type="button" className="link" onClick={()=>setLookupDefinitionExpanded(!lookupDefinitionExpanded)}>{lookupDefinitionExpanded ? "Collapse definition" : "Expand definition"}</button> : null}
              </> : null}
              {referenceKey.startsWith("table/") ? <>
                <form className="reader-code-search" onSubmit={(event) => {event.preventDefault();void openReference(referenceKey,0,false,codeQuery);}}><label htmlFor="reference-code-query">Search codes</label><div><input id="reference-code-query" value={codeQuery} maxLength={128} onChange={(event) => setCodeQuery(event.target.value)} /><button type="submit" disabled={referenceLoading || busy}>Search</button></div></form>
                {referenceView?.reference?.record?.content_state === "available" ? <><p>{referenceView.child_count} matches · {referenceView.total_count} source codes</p><div className="reader-code-scroll"><table className="data-table" aria-label="Reference codes"><thead><tr><th>Code</th><th>Meaning</th></tr></thead><tbody>{referenceView.children.map((code) => <tr key={code.key} className={revealed && inspection.reference?.table_keys?.includes(referenceKey) && inspection.decode_state === "decoded" && code.code === inspection.decoded ? "reader-code-selected" : ""}><td><button type="button" onClick={() => void openReference(code.key)}>{code.code}</button></td><td>{code.definition || "Meaning not available"}</td></tr>)}</tbody></table></div>{referenceView.child_count === 0 ? <p>No codes match this search.</p> : null}<nav className="pager" aria-label="Code pages"><button type="button" disabled={referenceLoading || referenceView.offset === 0} onClick={() => void openReference(referenceKey,Math.max(0,referenceView.offset-100),false,activeQuery)}>Previous codes</button><span>{referenceView.child_count ? referenceView.offset+1 : 0}–{referenceView.offset+referenceView.children.length} of {referenceView.child_count}</span><button type="button" disabled={referenceLoading || referenceView.offset+referenceView.children.length >= referenceView.child_count} onClick={() => void openReference(referenceKey,referenceView.offset+100,false,activeQuery)}>Next codes</button></nav></> : null}
              </> : null}
              {inspection.reference_values_notice ? <p role="status">{inspection.reference_values_notice}</p> : null}
              {referenceView?.reference?.record?.kind === "datatype" && referenceView.children?.length ? <ul className="reader-reference-components" aria-label="Datatype components">{referenceView.children.map((component) => {
                const value=inspection.reference_values?.find((value) => value.key===component.key);
                const original=value ? value.node.state === "omitted" ? "Omitted" : value.node.state === "present" ? revealed ? value.encoded + (value.truncated ? " · Truncated" : "") : HIDDEN_VALUE : stateText(value.node.state,revealed,"") : "Not available";
                return <li key={component.key}><button type="button" onClick={() => void openReference(component.key)}>{component.position} · {component.name}</button><p>{attributeText(component.datatype)} · Opt {attributeText(component.optionality)} · Len {attributeText(component.length)}</p><p>Original value: {original}</p>{value ? <code className="selector">{value.node.path}</code> : null}{value && revealed && value.node.state === "present" && (value.decoded !== value.encoded || PROBLEMS.has(value.decode_state)) ? <p>Decoded value: {PROBLEMS.has(value.decode_state) ? decodeStateText(value.decode_state) : value.decoded}</p> : null}</li>;
              })}</ul> : null}
              {referenceView?.reference?.record ? <><div className="reader-source-reference"><p>{[...new Set(referenceView.reference.sources?.map(source=>source.publisher) || [])].join(" · ")} · {referenceView.reference.edition}</p><p className="reader-reference-citation">{referenceView.reference.record.source} · §{attributeText(referenceView.reference.record.section)}</p></div><ReferenceOrigins record={referenceView.reference.record} {...(referenceView.reference.sources ? {sources:referenceView.reference.sources} : {})}/></> : null}
              {referenceView?.reference?.record?.kind === "datatype" && referenceView.child_count > 100 ? <nav aria-label="Datatype component pages"><button type="button" disabled={referenceLoading || referenceView.offset === 0} onClick={() => void openReference(referenceKey, Math.max(0, referenceView.offset-100),false)}>Previous components</button><button type="button" disabled={referenceLoading || referenceView.offset + referenceView.children.length >= referenceView.child_count} onClick={() => void openReference(referenceKey,referenceView.offset+100,false)}>Next components</button></nav> : null}
            </> : <>
            <p className="selector reader-selected-path" title={selected.path || "Message"} aria-label={selected.path || "Message"}>{readablePath(selected.path) || "Message"}</p>
            <h4 className="reader-selected-name">{inspection.reference.record?.name || inspection.metadata.label || (selected.kind === "segment" ? inspection.segment_name : selected.path) || "Reference"}</h4>
            {field ? <><label>Original value</label><pre className="value reader-original-value">{selected.state === "present" ? revealed ? inspection.raw || (inspection.decode_state === "too_large" ? "Truncated" : "Not available") : HIDDEN_VALUE : stateText(selected.state, revealed, "")}</pre>
              {revealed && selected.state === "present" && inspection.decode_state === "decoded" && inspection.decoded !== inspection.raw ? <><label>Decoded value</label><pre className="value">{inspection.decoded}</pre></> : null}
            </> : null}
            {inspection.message_context ? <><section className="reader-message-context" aria-label="Message context"><p>Structure: {inspection.message_context.resolved_structure || "Not available"}</p><p>{inspection.message_context.status === "inferred" ? "Resolved from message code + trigger event" : inspection.message_context.status === "declared" ? "Declared in the original message" : `Reference status: ${inspection.message_context.status}`}</p></section><details className="reader-context-provenance reader-extra-metadata reader-structure-provenance"><summary>Message structure provenance</summary><p>Transmitted MSH-9.3: {inspection.message_context.declared_structure_state === "present" ? inspection.message_context.declared_structure || "Not available" : inspection.message_context.declared_structure_state === "omitted" ? "Omitted" : inspection.message_context.declared_structure_state}</p><p>Reference structure: {inspection.message_context.resolved_structure || "Not available"} · {inspection.message_context.status}</p>{inspection.message_context.reason ? <p role="status">{inspection.message_context.reason}</p> : null}{inspection.message_context.message_key ? <button type="button" disabled={busy} onClick={() => void openReference(inspection.message_context!.message_key)}>Message overview</button> : null}{inspection.message_context.structure_key ? <button type="button" disabled={busy} onClick={() => void openReference(inspection.message_context!.structure_key)}>Structure overview</button> : null}{selected.segment ? <details><summary>Selected occurrence placement</summary><p>{inspection.message_context.placement.state} · {inspection.message_context.placement.reason}</p>{inspection.message_context.placement.state === "known" ? <><p>{inspection.message_context.placement.path}</p><p>Segment placement: {inspection.message_context.placement.segment_min}..{inspection.message_context.placement.segment_max}; occurrence {inspection.message_context.placement.segment_repetition}</p><ul>{inspection.message_context.placement.groups.map((group) => <li key={`${group.name}/${group.occurrence}`}>{group.name}[{group.occurrence}] · {group.min}..{group.max}</li>)}</ul><p>Placement cardinality is separate from field repetition and reusable segment definitions.</p></> : null}</details> : null}</details></> : null}

            <div className="reader-reference-tabs" onKeyDown={referenceKeys} role="tablist" aria-label="Reference views">
              <button type="button" role="tab" aria-selected="true" onClick={()=>setColumnGuide(false)}>Overview</button>
              <button type="button" role="tab" aria-selected="false" disabled={busy || !(inspection.reference.datatype_key || inspection.reference.parent_datatype_key)} onClick={()=>void openReference(inspection.reference!.datatype_key || inspection.reference!.parent_datatype_key!)}>{inspection.reference.parent_datatype_key ? "Parent type" : "Components"}</button>
              <button type="button" role="tab" aria-selected="false" disabled={busy || !(inspection.reference.element_key || inspection.reference.table_keys?.length)} onClick={()=>void openReference(inspection.reference!.element_key || inspection.reference!.table_keys![0]!)}>{inspection.reference.element_key ? "Data element" : "Table"}</button>
            </div>
            {inspection.reference.edition && inspection.reference.edition !== inspection.metadata.hl7_version ? <aside className="notice warning" role="status"><p>Selected reference edition differs from the message declaration. Attributes describe the selected reference; source values and evaluation pins are unchanged.</p><button type="button" disabled={busy || catalogReading} onClick={() => void chooseReference(inspection.metadata.hl7_version)}>Return to message edition</button></aside> : null}
            {inspection.reference.reason ? <p role="status">{inspection.reference.reason}</p> : null}
            {inspection.reference.type_resolution === "unresolved" ? <p>Reference declared datatype: {inspection.reference.declared_datatype || "Not available"}. Contextual resolution: Not available.</p> : null}
            {inspection.reference.record ? <>
              <p className="reader-reference-citation">§{attributeText(inspection.reference.record.section)} · {readablePath(selected.path) || "Message"} · {attributeText(inspection.reference.record.datatype)} · {attributeText(inspection.reference.record.item)}</p>
              {inspection.reference.record.definition ? <div className={`reader-definition ${fullDefinition ? "reader-definition-full" : ""}`}><p>{fullDefinition ? inspection.reference.record.definition : compactDefinition(inspection.reference.record.definition)}</p></div> : <p>Definition not available.</p>}
              {inspection.reference.record.definition ? <button type="button" className="link" onClick={() => setFullDefinition(!fullDefinition)}>{fullDefinition ? "Collapse definition" : "Expand definition"}</button> : null}
              {field ? <ValueRows rows={[
                {label:"Data type",value:inspection.reference.datatype_key ? <button type="button" className="reader-attribute-link" aria-label={`Datatype · ${inspection.reference.record.datatype.value}`} disabled={busy} onClick={()=>void openReference(inspection.reference!.datatype_key!)}>{attributeText(inspection.reference.record.datatype)}</button> : attributeText(inspection.reference.record.datatype)},
                {label:"Opt / Len",value:<span className="reader-opt-length"><span>{attributeText(inspection.reference.record.optionality)}</span><span aria-hidden="true"> / </span><span>{attributeText(inspection.reference.record.length)}</span></span>},
                {label:"C-Len",value:attributeText(inspection.reference.record.conformance_length)},
                {label:"Repetition",value:attributeText(inspection.reference.record.repetition)},
              ]} /> : <section className="reader-segment-evidence" aria-label="Selected segment evidence"><h5>In this message</h5><ValueRows rows={[{label:"Occurrence",value:selected.path},{label:"Original byte span",value:`${selected.start}–${selected.end}`}]} /><label>Actual segment</label><pre className="value">{revealed ? inspection.raw_window?.selected || inspection.raw || decodeStateText(inspection.decode_state) : HIDDEN_VALUE}</pre>{inspection.grid?.rows.find(row=>row.node.kind==="field" && row.node.field===9 && row.node.segment===selected.segment) ? <button type="button" onClick={()=>go(`${selected.path}-9`)}>Inspect Message Type</button> : null}</section>}
              <div className="reader-reference-links">
                {onRequirements ? <button type="button" className="link" disabled={busy} onClick={()=>onRequirements(inspection.selector || selected.path)}>Open specification</button> : null}
                {onFieldValues && field && inspection.selector ? <button type="button" className="link" disabled={busy} onClick={()=>onFieldValues(inspection.selector)}>Field values</button> : null}
                {inspection.reference.table_keys?.map((key) => <button key={key} type="button" className="link" disabled={busy} onClick={() => void openReference(key)}>Table · {key.slice("table/".length)}</button>)}
                {inspection.reference.element_key ? <button type="button" className="link" disabled={busy} onClick={() => void openReference(inspection.reference!.element_key!)}>Item# · {inspection.reference.record.item.value}</button> : null}
                {inspection.reference.section_key ? <button type="button" className="link" disabled={busy} onClick={() => void openReference(inspection.reference!.section_key!)}>Sect · §{inspection.reference.record.section.value}</button> : null}
                {inspection.reference.parent_datatype_key ? <button type="button" className="link" disabled={busy} onClick={() => void openReference(inspection.reference!.parent_datatype_key!)}>Parent datatype · {inspection.reference.record.container}</button> : null}
              </div>
              <div className="reader-source-reference"><p>{[...new Set(inspection.reference.sources?.map((source) => source.publisher) ?? [])].join(" · ")} · {inspection.reference.edition}</p><p className="reader-reference-citation">{inspection.reference.record.source} · §{attributeText(inspection.reference.record.section)}</p><p>{referenceOriginText(inspection.reference.record.definition_origin,inspection.reference.sources)}</p></div>
              <details className="reader-extra-metadata"><summary>Source attributes and provenance</summary><ValueRows rows={referenceRows(inspection.reference.record).slice(5)} /><ReferenceOrigins record={inspection.reference.record} {...(inspection.reference.sources?{sources:inspection.reference.sources}:{})}/>{inspection.reference.record.conformance_length.state === "specified" ? <p>Conformance length describes the receiving application’s storage capacity, not a sending maximum. The source’s permitted lengths and truncation markers remain as printed.</p> : null}</details>
            </> : null}
            {inspection.reference.parent_field ? <aside className="reader-parent-field"><h5>Parent field notes</h5><p>{inspection.reference.parent_field.segment}-{inspection.reference.parent_field.field} · {inspection.reference.parent_field.name}</p><p>Parent Item#: {attributeText(inspection.reference.parent_field.item)} · Parent repetition: {attributeText(inspection.reference.parent_field.repetition)}</p></aside> : null}
            <details className="reader-extra-metadata"><summary>Message and reference edition</summary><p>Message edition: {inspection.metadata.hl7_version || "Not available"}</p><p>Reference edition: {inspection.reference.edition || (inspection.reference.status === "not_selected" ? "Not selected" : "Not available")}</p></details>
            {field ? <div className="selected-field-actions">
              {revealed && selected.state === "present" && (inspection.decoded || inspection.raw) ? <button type="button" className="link" onClick={()=>void navigator.clipboard?.writeText(inspection.decoded || inspection.raw)}>Copy value</button> : null}
              {onFilterByField && inspection.selector ? <button type="button" className="link" onClick={()=>onFilterByField(inspection.selector,revealed && selected.state === "present" ? inspection.decoded || null : null,selected.state)}>Filter by this field</button> : null}
            </div> : null}
            </>}
            </>}
            {!validationShown && inspection.reference_overlay ? <section className="reader-profile-overlay" aria-label="Selected profile context"><h4>Selected profile context</h4><p>{inspection.reference_overlay.status} · {inspection.reference_overlay.reason} · Applicability: {inspection.reference_overlay.applicability||"Not established"}</p>{inspection.reference_overlay.profile ? <p>Profile: {inspection.reference_overlay.profile.id} · {inspection.reference_overlay.profile.version} · {inspection.reference_overlay.profile.sha256}</p> : null}{inspection.reference_overlay.pack ? <p>Exact pack: {inspection.reference_overlay.pack.id} · {inspection.reference_overlay.pack.version} · {inspection.reference_overlay.pack.sha256}</p> : null}{inspection.reference_overlay.base ? <p>Authored base: {inspection.reference_overlay.base.hl7_version}/{inspection.reference_overlay.base.family} · {inspection.reference_overlay.base.pack.id}/{inspection.reference_overlay.base.pack.version}</p> : null}{inspection.reference_overlay.field ? <><h5>Local field constraints, separate from base reference</h5><p>Parent field: {selected.segment}-{inspection.reference_overlay.field.position} · Usage {inspection.reference_overlay.field.usage} · Datatype {inspection.reference_overlay.field.type || "Not specified"}</p>{inspection.reference_overlay.field.condition ? <p>Conditional predicate: {inspection.reference_overlay.field.condition.segment}-{inspection.reference_overlay.field.condition.position} · {inspection.reference_overlay.field.condition.operator}. Displayed as authored; not evaluated here.</p> : null}{inspection.reference_overlay.field.cardinality ? <p>Local field cardinality: {inspection.reference_overlay.field.cardinality.min}..{inspection.reference_overlay.field.cardinality.max}</p> : null}</> : null}{inspection.reference_overlay.segment_cardinality ? <p>Local segment cardinality: {inspection.reference_overlay.segment_cardinality.min}..{inspection.reference_overlay.segment_cardinality.max}</p> : null}{inspection.reference_overlay.segment_description ? <p>{inspection.reference_overlay.segment_description}</p> : null}{inspection.reference_overlay.documentation ? <details><summary>{inspection.reference_overlay.documentation.title}</summary><p>{inspection.reference_overlay.documentation.identity}</p><pre className="value">{inspection.reference_overlay.documentation.text}</pre></details> : null}<p>No inspection selection changes historical evaluator pins or establishes a validation/ACK verdict.</p></section> : null}
            {!validationShown && columnGuide ? <section className="reader-column-guide" aria-label="Reference attributes"><h4>Reference attributes</h4><p>Metadata comes from the selected reference edition. Values come from the original message.</p><ValueRows rows={[
              {label:"Type",value:"Declared datatype; components use their own datatype. An unresolved variable type stays unresolved."},
              {label:"Opt",value:"Source usage code. Required, optional, conditional and backward-compatibility codes retain their original notation; profile overrides are separate."},
              {label:"Len",value:"Edition-specific permitted-length notation. Preserve source lists and ranges; storage capacity is not a guessed sender maximum."},
              {label:"C-Len",value:"Source conformance-length and truncation notation where available. Not specified or unavailable is not zero."},
              {label:"Rep",value:"Field repetition notation from the source, separate from observed repetitions and segment-group cardinality."},
              {label:"Item#",value:"Data-element identifier. Leading zeros are retained; components have no independent field item number."},
              {label:"Tbl",value:"Bound edition-specific table. Missing bindings and unavailable table content are different states."},
              {label:"Sect",value:"Source definition section for this entity and edition."},
              {label:"Value",value:"Original evidence. Empty, Null, Omitted, Hidden, Truncated and undecodable remain distinct."},
            ]} /><button type="button" onClick={() => setColumnGuide(false)}>Back to reader</button></section> : null}
            <p className="reader-reference-coverage">Catalog coverage: {inspection.reference.coverage.segments} segments · {inspection.reference.coverage.fields} fields{inspection.reference.coverage.datatypes ? ` · ${inspection.reference.coverage.datatypes} datatypes · ${inspection.reference.coverage.components ?? 0} components` : ""} · {inspection.reference.coverage.definitions} definitions{inspection.reference.coverage.tables ? ` · ${inspection.reference.coverage.tables} tables · ${inspection.reference.coverage.codes ?? 0} codes · ${inspection.reference.coverage.elements ?? 0} data elements` : ""}{inspection.reference.missing_count ? ` · ${inspection.reference.missing_count} gaps or ambiguities` : ""}</p>
            {inspection.reference.missing_count ? <details><summary>Coverage gaps</summary><ul>{inspection.reference.coverage.missing.map((gap) => <li key={gap}>{gap}</li>)}</ul>{inspection.reference.missing_count > inspection.reference.coverage.missing.length ? <p>Additional gaps are listed in the extraction receipt.</p> : null}</details> : null}
            <p className="reader-reference-coverage">Reference metadata does not establish validation or ACK success. Existing findings remain available in their finding detail.</p>{onCreateVariant ? <button type="button" disabled={busy} onClick={onCreateVariant}>Create variant</button> : null}
          </div></section> : <section className="reader-reference" aria-label="Selected details"><h3 className="reader-pane-heading">Details</h3><div className="reader-reference-scroll"><p className="reader-selected-path">{inspection.selector || selected.path || "Message"}</p><h4 className="reader-selected-name">{inspection.metadata.label || selected.path || "Message"}</h4>{field ? <><label>Original encoded value</label><pre className="value">{selected.state === "present" ? revealed ? inspection.raw || inspection.decoded : HIDDEN_VALUE : stateText(selected.state,revealed,"")}</pre><div className="selected-field-actions">{revealed && selected.state === "present" && (inspection.decoded || inspection.raw) ? <button type="button" onClick={()=>void navigator.clipboard?.writeText(inspection.decoded || inspection.raw)}>Copy value</button> : null}{onFilterByField && inspection.selector ? <button type="button" onClick={()=>onFilterByField(inspection.selector,revealed && selected.state === "present" ? inspection.decoded || null : null,selected.state)}>Filter by this field</button> : null}</div></> : null}</div></section>}
          </div>
          <div className="reader-status-bar"><span>HL7 {inspection.metadata.hl7_version || "not declared"}</span><span>{title}</span><span>Original bytes</span><span>Reference {inspection.reference?.edition || "not selected"}</span><Reveal revealed={revealed} disabled={busy} onToggle={onReveal} /></div>
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
          const answer = await inspectSelection(path.trim());
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
              ...(inspection.source_name ? [{ label: "Source", value: inspection.source_name }] : []),
              { label: inspection.source_name ? "Source ID" : "Source", value: inspection.source_id || "—" },
              ...(inspection.direction ? [{ label: "Direction", value: DIRECTION_NAMES[inspection.direction] }] : []),
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

/** The same Fields, Raw and Hex views for typed R4 evidence. Narratives,
 * attachment URLs and extensions appear only as escaped original JSON. */
function FHIRReader({ inspection, fhir, busy, onInspect, onReveal, onInspectResource, onCreateCheck, onClose, backLabel, onBack }: {
  inspection: Inspection; fhir: FHIRInspection; busy: boolean;
  onInspect: (path: string, nodeOffset: number, byteOffset: number, rawOffset?: number, referenceCatalog?: string) => Promise<InspectionResult | null>;
  onReveal: (revealed: boolean) => void;
  onInspectResource?: ((occurrence: string) => void) | undefined;
  onCreateCheck?: ((preset: FHIRCheckPreset) => void) | undefined;
  onClose?: (() => void) | undefined; backLabel?: string | undefined; onBack?: (() => void) | undefined;
}) {
  const [view, setView] = useState<ReaderView>("fields");
  const [goingTo, setGoingTo] = useState(false);
  const [goPath, setGoPath] = useState("");
  const [info, setInfo] = useState(false);
  const current = fhir.resources.find((resource) => resource.occurrence === inspection.occurrence);
  const selected = fhir.selected;
  const path = selected?.field.id ?? "";
  const revealed = inspection.revealed;
  const value = (text: string) => revealed ? text || "Unavailable" : HIDDEN_VALUE;
  return <section className="message-reader" aria-label="Message details">
    <header className="reader-header">
      {backLabel && onBack ? <BackLink label={backLabel} onBack={onBack} /> : null}
      <div className="reader-heading"><h2>{current?.type ?? inspection.message_code ?? "FHIR request"}</h2><p className="reader-facts">FHIR R4 · {fhir.declaration.context.version} · {fhir.declaration.source_kind}</p></div>
      <Menu label="More message actions" items={[{ label: "Go to field…", disabled: busy || fhir.fields.length === 0, onSelect: () => { setGoPath(""); setGoingTo(true); } }, { label: "Info", onSelect: () => setInfo(true) }]} />
      {onClose ? <IconButton icon="close" label="Close message details" onClick={onClose} /> : null}
    </header>
    {inspection.notice ? <p className="row-reason" role="note">{inspection.notice}</p> : null}
    <TaskTabs label="Message views" id="reader-views" tabs={VIEWS} selected={view} onSelect={setView} panels>
      <TaskPanel tabs="reader-views" tab="fields" shown={view === "fields"}>
        {current ? <ValueRows label="Resource identity" rows={[
          { label: "Occurrence", value: current.occurrence }, { label: "Interpretation", value: current.state },
          { label: "Logical ID", value: value(current.logical_id) }, { label: "Resource version", value: value(current.version_id) },
          { label: "Full URL", value: value(current.full_url) }, { label: "Canonical URL", value: value(current.canonical_url) }, { label: "Business version", value: value(current.canonical_version) },
          ...current.identifiers.map((identifier, at) => ({ label: `Identifier ${at + 1}`, value: revealed ? `${identifier.system || "System unavailable"} · ${identifier.value || "Value unavailable"}` : HIDDEN_VALUE })),
        ]} /> : null}
        {selected ? <div className="selected-field">
          <button type="button" className="quiet" disabled={busy} onClick={() => void onInspect("", inspection.node_offset, -1)}>Back to fields</button>
          <p className="selected-field-name"><strong>{selected.field.id}</strong><span className="selector">{selected.field.type}{selected.field.repeated ? " · Repeated" : ""}</span></p>
          <p>{selected.selection.state}</p>
          <ul aria-label="Field readings">{selected.selection.readings.map((reading, at) => <li key={`${reading.pointer}-${at}`}><code>{reading.pointer}</code> · {reading.datatype} · <span>{reading.value.state}</span>{reading.value.state === "present" ? ` · ${datasetText(reading.value, revealed)}` : ""}{revealed && reading.canonical ? ` · ${reading.canonical.url} · ${reading.canonical.version}` : ""}</li>)}</ul>
          {selected.preset && onCreateCheck ? <button type="button" disabled={busy} onClick={() => onCreateCheck(selected.preset!)}>Create check from this field</button> : null}
        </div> : <ul className="outline" aria-label="FHIR fields">{fhir.fields.map((field) => <li key={field.field.id}><button type="button" className="outline-row" disabled={busy} onClick={() => void onInspect(field.field.id, inspection.node_offset, -1)}><span className="outline-name"><span>{field.field.id}</span><span className="selector">{field.field.type}{field.field.repeated ? " · Repeated" : ""}</span></span><span className="outline-state">{field.selection.state} · {field.selection.readings.length} readings</span></button></li>)}</ul>}
        {inspection.child_count > fhir.fields.length ? <nav aria-label="Parts pages" className="pager"><IconButton icon="previous" label="Previous fields" disabled={busy || inspection.node_offset === 0} onClick={() => void onInspect(path, Math.max(0, inspection.node_offset - 100), -1)} /><span>{inspection.node_offset + 1}–{inspection.node_offset + fhir.fields.length} of {inspection.child_count}</span><IconButton icon="next" label="Next fields" disabled={busy || inspection.node_offset + fhir.fields.length >= inspection.child_count} onClick={() => void onInspect(path, inspection.node_offset + fhir.fields.length, -1)} /></nav> : null}
        {fhir.references.length > 0 ? <section aria-label="FHIR references"><h3>References</h3><ul>{fhir.references.map((reference, at) => <li key={at}><code>{reference.pointer}</code> · {revealed ? reference.reference || "Absent reference" : HIDDEN_VALUE} · {reference.resolution.state}{reference.resolution.occurrences.map((occurrence) => onInspectResource ? <button key={occurrence} type="button" className="quiet" disabled={busy} onClick={() => onInspectResource(occurrence)}>Open local resource {occurrence}</button> : <span key={occurrence}> · {occurrence}</span>)}</li>)}</ul></section> : null}
        {fhir.findings.length > 0 ? <ValueRows label="FHIR findings" rows={fhir.findings.map((finding, at) => ({ label: finding.pointer || `Finding ${at + 1}`, value: `${finding.state} · ${finding.code}` }))} /> : null}
      </TaskPanel>
      <TaskPanel tabs="reader-views" tab="raw" shown={view === "raw"}>{revealed && inspection.raw_window ? <RawText window={inspection.raw_window} busy={busy} onPage={(offset) => void onInspect(path, inspection.node_offset, inspection.byte_offset, offset)} /> : <p className="reader-hidden">{HIDDEN_VALUE}</p>}</TaskPanel>
      <TaskPanel tabs="reader-views" tab="hex" shown={view === "hex"}>{revealed ? <HexTable rows={inspection.bytes} selection={null} total={inspection.size} offset={inspection.byte_offset} busy={busy} onPage={(offset) => void onInspect(path, inspection.node_offset, offset)} /> : <p className="reader-hidden">{HIDDEN_VALUE}</p>}</TaskPanel>
    </TaskTabs>
    <Reveal revealed={revealed} disabled={busy} onToggle={onReveal} />
    <FormDialog open={goingTo} title="Go to field" size="small" submitLabel="Go" submitDisabled={goPath.trim() === ""} busy={busy} onClose={() => setGoingTo(false)} onSubmit={async () => {
      const answer = await onInspect(goPath.trim(), inspection.node_offset, -1);
      if (answer?.state !== "completed") return { reason: answer?.reason ?? "This is not a supported typed R4 field.", field: "go-to-fhir-field" };
      setView("fields"); setGoingTo(false); return null;
    }}><label htmlFor="go-to-fhir-field">Field path</label><input id="go-to-fhir-field" type="text" autoFocus spellCheck={false} value={goPath} onChange={(event) => setGoPath(event.target.value)} /></FormDialog>
    <Modal open={info} title="Info" onClose={() => setInfo(false)}><ValueRows rows={[{ label: "Protocol", value: `FHIR R4 · ${fhir.declaration.context.version}` }, { label: "Media type", value: fhir.declaration.context.media_type }, { label: "Source type", value: fhir.declaration.source_kind }, { label: "Bytes", value: String(inspection.size) }, { label: "In the source", value: `From byte ${inspection.source_offset}` }, { label: "Encoding", value: inspection.encoding }, { label: "Occurrence", value: inspection.occurrence }, { label: "Evidence", value: inspection.identity }]} /></Modal>
  </section>;
}

function datasetText(value: DatasetValue, revealed: boolean): string {
  if (value.state !== "present") return value.state;
  if (!revealed) return HIDDEN_VALUE;
  if (value.items) return value.items.map((item) => datasetText(item, true)).join(" · ");
  return [value.text ?? "", value.code_system, value.precision, value.timezone].filter((held) => held !== undefined).join(" · ");
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

/** The whole message as escaped original text, a window at a time, with the
 * selected part marked. It pages only when the message is longer than one
 * window. */
export function RawText({ window: raw, busy, onPage }: { window: RawWindow; busy: boolean; onPage: (offset: number) => void }) {
  const paged = raw.message_end - raw.message_start > RAW_WINDOW;
  const lineStart=raw.before.lastIndexOf("\n")+1;
  const nextLine=raw.after.indexOf("\n");
  const lineEnd=nextLine<0 ? raw.after.length : nextLine;
  return (
    <>
      <pre className="value raw">
        {raw.selected ? <>{raw.before.slice(0,lineStart)}<span className="selected-line">{raw.before.slice(lineStart)}<mark>{raw.selected}</mark>{raw.after.slice(0,lineEnd)}</span>{raw.after.slice(lineEnd)}</> : <>{raw.before}{raw.after}</>}
      </pre>
      {paged ? (
        <nav aria-label="Raw pages" className="pager">
          <IconButton icon="previous" label="Previous raw text" disabled={busy || raw.offset <= raw.message_start} onClick={() => onPage(Math.max(raw.message_start, raw.offset - RAW_WINDOW))} />
          <span>
            {raw.offset - raw.message_start + 1}–{raw.end - raw.message_start} of {raw.message_end - raw.message_start} bytes
          </span>
          <IconButton icon="next" label="Next raw text" disabled={busy || raw.end >= raw.message_end} onClick={() => onPage(raw.end)} />
        </nav>
      ) : null}
    </>
  );
}

/** The bytes one Raw window holds. */
const RAW_WINDOW = 4096;
