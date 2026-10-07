import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import type { InspectionGridRequest, DatasetValue, FHIRCheckPreset, FHIRInspection, FieldState, Inspection, InspectionResult, InspectorNode, RawWindow, Hl7referenceAttribute, Hl7referenceRecord, Hl7referenceOrigin, Hl7referenceSource, HL7ReferenceResult, HL7ReferenceSelection, ProfileevalNode } from "./bindings";
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
import { ReaderIcon } from "./ReaderIcon";
import { ReaderGrid } from "./ReaderGrid";
import { ReaderColumns } from "./ReaderColumns";
import { ReaderResize } from "./ReaderResize";
import { useViewState } from "./viewstate";
import { ReaderSelectionHeader, ReferenceAttributes, ReferenceBadge, ReferenceDefinition } from "./ReaderPresentation";
import { ReferenceLibrary } from "./ReferenceLibrary";
import { ReferenceMenu } from "./ReferenceMenu";
import { MessageText } from "./MessageText";
import "./inspector.css";
import "./reader-grid.css";

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

function readablePath(path:string):string { return path; }

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
 * all read a message through this one view. PHI masking applies to this reader
 * while list, report and FHIR reveal policies remain independently owned,
 * a query or a log. */
export function MessageReader({
  result,
  referenceCatalog: suppliedCatalog = "",
 referenceIdentity: suppliedIdentity = "",
  referenceSelection: suppliedSelection,
  referenceRequest = 0,
  referenceLibraryRequest = 0,
  referenceResetRequest = 0,
  toolbarReference = false,
  compactReader,
  onCompactReader,
  contextDetails,
  contextDetailsTitle = "Details",
  kind,
  source,
  loading,
  valuesHidden = false,
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
  referenceLibraryRequest?: number;
  referenceResetRequest?: number;
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
  valuesHidden?: boolean;
  busy: boolean;
  onInspect: (path: string, nodeOffset: number, byteOffset: number, rawOffset?: number, referenceCatalog?: string, selection?: HL7ReferenceSelection,referenceIdentity?: string, grid?: InspectionGridRequest) => Promise<InspectionResult | null>;
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
  const [layout, setLayout] = useViewState("reader-layout", () => ({wrap:false, fit:false, sourceHeight:12.25, detailsWidth:24}));
  const [view, setView] = useState<ReaderView>("fields");
  const [collapsedSegment, setCollapsedSegment] = useState<{owner:string;path:string}|null>(null);
  const returnGridFocus=useRef(false);
  const selectView=(next:ReaderView)=>{returnGridFocus.current=next==="fields" && view!=="fields";setView(next);};
  const gridElement=useRef<HTMLElement|null>(null);
  useEffect(()=>{if(view==="fields" && returnGridFocus.current){(gridElement.current ?? workbenchElement?.querySelector<HTMLElement>('[role="treegrid"]'))?.focus();returnGridFocus.current=false;}},[view]);
  const [workbenchElement, setWorkbenchElement] = useState<HTMLDivElement | null>(null);
  const workbenchSize = useMeasured(workbenchElement);
  const [sourceContent,setSourceContent]=useState<HTMLElement|null>(null);
  const sourceSize=useMeasured(sourceContent);
  const compactWorkbench = workbenchSize.width > 0 && workbenchSize.width / workbenchSize.rem < 42;
  const maxSourceHeight=workbenchSize.height ? Math.max(8,workbenchSize.height/workbenchSize.rem-14) : 24;
  const sourceHeight=layout.fit ? Math.max(6,Math.min(24,sourceSize.height/sourceSize.rem+3,maxSourceHeight)) : Math.min(layout.sourceHeight,maxSourceHeight);
  const maxDetailsWidth=workbenchSize.width ? Math.max(20,workbenchSize.width/workbenchSize.rem-24.375) : 48;
  const detailsWidth=Math.min(layout.detailsWidth,maxDetailsWidth);
  const [goingTo, setGoingTo] = useState(false);
  const [info, setInfo] = useState(false);
  const [libraryOpen,setLibraryOpen]=useState(false);
  const [referenceInfo, setReferenceInfo] = useState(false);
  const [detailsVisible, setDetailsVisible] = useState(true);
  const [path, setPath] = useState("");
  const [catalogPath, setCatalogPath] = useState("");
 const [catalogIdentity,setCatalogIdentity]=useState("");
  const [catalogProblem, setCatalogProblem] = useState("");
  const [catalogReading, setCatalogReading] = useState(false);
  const [fullDefinition, setFullDefinition] = useState(false);
  const [definitionVisible,setDefinitionVisible]=useState(true);
  const referencePositions = useRef(new Map<string,{path:string;pin:string;key:string;offset:number;query:string;components:boolean;fields:boolean;overview:boolean}>());
  const scrollPositions = useRef(new Map<string,{grid?:{top:number;left:number};source?:{top:number;left:number};details?:{top:number;left:number}}> ());
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
  const [gridProblem,setGridProblem]=useState("");
  const [columnGuide, setColumnGuide] = useState(false);
  const [segmentFields,setSegmentFields] = useState(false);
  const [entityOverview,setEntityOverview] = useState(false);
  const [fieldComponents,setFieldComponents] = useState(false);
  const [validationShown,setValidationShown]=useState(false);
  const referenceTabFocus=useRef(false);
  const [overlaySelection, setOverlaySelection] = useState<HL7ReferenceSelection | undefined>();
  const [overlayProblem, setOverlayProblem] = useState("");
  const overlaySerial=useRef(0);
  const inspection = valuesHidden && result?.inspection ? {...result.inspection,revealed:false} : result?.inspection;
  const selected = inspection?.selected;
  const manualCatalog=catalogPath || suppliedCatalog;
  const referencePath=manualCatalog || inspection?.reference_catalog || "";
  const referencePin=manualCatalog ? catalogIdentity || suppliedIdentity || inspection?.reference?.identity || undefined : inspection?.reference?.identity || undefined;
  const contextKey = `${inspection?.identity ?? ""}/${inspection?.occurrence || inspection?.message || ""}/${selected?.path ?? ""}/${result?.inspection?.revealed ?? false}/${inspection?.reference_overlay?.selection.profile_identity ?? ""}/${inspection?.reference_overlay?.selection.pack_identity ?? ""}/${inspection?.reference_overlay?.selection.documentation_identity ?? ""}`;
  referenceContext.current = contextKey;
  useEffect(()=>setValidationShown(false),[inspection?.identity,inspection?.occurrence,inspection?.message]);
  const lookupContext = `${contextKey}/${referencePin ?? ""}/${referencePath}/${inspection?.reference?.status ?? ""}`;
  lookupOwner.current = lookupContext;
  useEffect(() => {
    ++lookupSerial.current; setReferenceView(null); setReferenceLoading(false); setReferenceKey(""); setReferenceTrail([]); setCodeQuery(""); setActiveQuery(""); setColumnGuide(false);
    const saved=referencePositions.current.get(captionOwner);
    if(saved && saved.path===selected?.path && saved.pin===referencePin && inspection?.reference?.status==="available") {
      if(saved.key) void openReference(saved.key,saved.offset,false,saved.query,saved.components,saved.overview);
      setSegmentFields(saved.fields);
    } else setSegmentFields(false);
  }, [lookupContext]);
  useEffect(() => { setCatalogProblem(""); setFullDefinition(false); setDefinitionVisible(true); }, [contextKey]);
  useEffect(() => {
    const identity = inspection?.identity;
    if (!identity) return; // A transient read clears its result, not its owner.
    if (referenceOwner.current && referenceOwner.current !== identity) {setCatalogPath("");setCatalogIdentity("");setOverlaySelection(undefined);setOverlayProblem("");referencePositions.current.clear();scrollPositions.current.clear();}
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
  const captionOwner=`${inspection?.identity || ""}/${inspection?.occurrence || inspection?.message || 0}`;
  const segmentShortcuts=selected?.path && selected.segment ? (inspection?.segments ?? []).filter(row=>row.node.path !== (inspection?.grid?.segment || selected.path)) : [];
  const controlID=revealed ? inspection?.control_id || "" : "";
  const editorCaption=controlID ? `${inspection?.message_code || title}${inspection?.trigger_event ? "^"+inspection.trigger_event : ""} · ${controlID}` : title;
  const atRoot = !selected || selected.path === "";
  const field = selected && selected.kind !== "segment" && selected.kind !== "message" && selected.kind !== "occurrence";

  const gridNodes: InspectorNode[] = inspection?.grid?.rows ?? (inspection?.reference && selected && selected.path !== "" ? [{
    node:selected, label:inspection.metadata.label, selector:inspection.selector, segment_name:inspection.segment_name,
    value:inspection.decoded, truncated:inspection.decode_state === "too_large", decode_state:inspection.decode_state,
    ...(inspection.reference?.record ? {reference:inspection.reference.record} : {}),
  }, ...inspection.children] : inspection?.children ?? []);
  useEffect(()=>{gridElement.current?.querySelector<HTMLElement>('[aria-current="location"]')?.scrollIntoView?.({block:"nearest",inline:"nearest"});},[selected?.path,view]);
  const segmentCollapsed = collapsedSegment?.owner === captionOwner && collapsedSegment.path === inspection?.grid?.segment;
  const visibleGridNodes = segmentCollapsed ? gridNodes.filter(row => row.node.kind === "segment") : gridNodes;
  const gridWindowLimit = useRef<number | undefined>(undefined);
  const gridRequest = (changes: Partial<InspectionGridRequest> = {}): InspectionGridRequest => ({expanded:inspection?.grid?.expanded ?? [],show_omitted:inspection?.grid?.show_omitted ?? false,offset:inspection?.grid?.offset ?? 0,follow_selection:true,...(gridWindowLimit.current ? {limit:gridWindowLimit.current} : {}),...changes});
  const rememberReference = (key="",offset=0,query="",components=false,fields=false,overview=false) => {
    referencePositions.current.delete(captionOwner);
    referencePositions.current.set(captionOwner,{path:selected?.path??"",pin:referencePin??"",key,offset,query,components,fields,overview});
    if(referencePositions.current.size>64)referencePositions.current.delete(referencePositions.current.keys().next().value!);
  };
  const inspectSelection = async (target: string, rawOffset?: number, grid?: InspectionGridRequest) => {
    setCollapsedSegment(null);
    referencePositions.current.delete(captionOwner);
    // Fence the user's new selection intent before the asynchronous host reply
    // changes props. An older catalog/profile read must not navigate back.
    ++referenceSerial.current; ++overlaySerial.current; ++lookupSerial.current;
    setCatalogReading(false); setReferenceLoading(false); setReferenceView(null);
    setReferenceKey(""); setReferenceTrail([]);
    setGridProblem("");
    const args = [target,0,-1,rawOffset,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin] as const;
    const answer = inspection?.grid?.mode === "message" ? await onInspect(...args,grid ?? gridRequest()) : await onInspect(...args);
    if (answer && answer.state !== "completed") setGridProblem(answer.reason || "This position could not be read.");
    return answer;
  };
  const go = (target: string) => void inspectSelection(target);
  const toggleBranch = (row: InspectorNode) => {
    const expanded = new Set(inspection?.grid?.expanded ?? []);
    if (row.expanded) expanded.delete(row.node.path); else expanded.add(row.node.path);
    const target = row.expanded && inspection?.grid?.ancestors?.includes(row.node.path) ? row.node.path : selected?.path ?? "";
    void inspectSelection(target,undefined,gridRequest({expanded:[...expanded],follow_selection:false}));
  };
  const pageGrid = async (offset:number,edge?:"first"|"last",limit?:number) => {
    if(limit!==undefined)gridWindowLimit.current=limit;
    const owner=contextKey;
    // Scrolling reads another grid window without resetting the selected source
    // or the open reference details.
    const answer=await onInspect(selected?.path ?? "",0,-1,inspection?.readable_window?.offset ?? inspection?.raw_window?.offset,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin,gridRequest({offset,follow_selection:false}));
    if(answer && answer.state!=="completed")setGridProblem(answer.reason || "This position could not be read.");
    if(!edge || referenceContext.current!==owner || answer?.state!=="completed")return;
    const rows=answer.inspection?.grid?.rows;
    const row=edge==="first" ? rows?.[0] : rows?.at(-1);
    if(row)await inspectSelection(row.node.path,undefined,gridRequest({offset}));
  };
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

  const handledLibraryRequest=useRef(referenceLibraryRequest);
  useEffect(()=>{if(referenceLibraryRequest!==handledLibraryRequest.current){handledLibraryRequest.current=referenceLibraryRequest;setLibraryOpen(true);}},[referenceLibraryRequest]);
  const useMessageReference = () => {
    ++referenceSerial.current; ++lookupSerial.current;
    setCatalogReading(false); setCatalogProblem(""); setCatalogPath(""); setCatalogIdentity("");
    void onInspect(selected?.path ?? "",inspection?.node_offset ?? 0,-1,undefined,"",overlaySelection || suppliedSelection,"");
  };
  const handledReferenceReset = useRef(referenceResetRequest);
  useEffect(()=>{if(referenceResetRequest!==handledReferenceReset.current){handledReferenceReset.current=referenceResetRequest;useMessageReference();}},[referenceResetRequest]);

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

  const openReference = async (key: string, offset = 0, remember = true, query = "", asComponents = false, overview = false) => {
    if (!inspection?.reference) return;
    rememberReference(key,offset,query,asComponents,false,overview);
    const owner = lookupContext;
    const serial = ++lookupSerial.current;
    if (remember && referenceKey && referenceKey !== key) setReferenceTrail((held) => [...held, {key:referenceKey,offset:referenceView?.offset ?? 0,query:activeQuery}]);
    if(referenceKey!==key)setLookupDefinitionExpanded(false);
    setFieldComponents(asComponents);
    setEntityOverview(overview);
    setReferenceKey(key);
    setCodeQuery(query);
    setActiveQuery(query);
    setReferenceView(null);
    setReferenceLoading(true);
    const answer = await lookupHL7Reference({catalog:referencePath, identity:inspection.reference.identity, edition:inspection.reference.edition, key, offset, limit:100, ...(query ? {query} : {})});
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
      rememberReference();
      ++lookupSerial.current;
      setReferenceView(null);
      setReferenceLoading(false);
      setReferenceKey("");
    }
  };

  const referenceTabs = () => {
    const reference=inspection?.reference;
    if(!reference || !selected)return null;
    const record=referenceView?.reference?.record;
    const messageEntity=record?.kind==="message" || record?.kind==="structure";
    const datatypeEntity=record?.kind==="datatype";
    const reset=()=>{rememberReference();++lookupSerial.current;setReferenceTrail([]);setReferenceView(null);setReferenceKey("");setReferenceLoading(false);setSegmentFields(false);};
    const tabs:{name:string;icon:"overview"|"componentTab"|"components"|"dataElement";selected:boolean;action:()=>void}[]=[];
    if(messageEntity){
      tabs.push({name:"Overview",icon:"overview",selected:record.kind==="message",action:()=>{if(inspection.message_context?.message_key)void openReference(inspection.message_context.message_key);}});
      if(inspection.message_context?.structure_key)tabs.push({name:"Structure",icon:"componentTab",selected:record.kind==="structure",action:()=>void openReference(inspection.message_context!.structure_key)});
    } else if(datatypeEntity && !fieldComponents){
      tabs.push({name:"Overview",icon:"overview",selected:entityOverview,action:()=>{rememberReference(referenceKey,referenceView?.offset??0,activeQuery,fieldComponents,false,true);setEntityOverview(true);}},{name:"Components",icon:"componentTab",selected:!entityOverview,action:()=>{rememberReference(referenceKey,referenceView?.offset??0,activeQuery,fieldComponents,false,false);setEntityOverview(false);}});
    } else {
      tabs.push({name:"Overview",icon:"overview",selected:!referenceKey&&!segmentFields,action:reset});
      if(selected.kind==="segment")tabs.push({name:"Fields",icon:"componentTab",selected:segmentFields,action:()=>{reset();rememberReference("",0,"",false,true);setSegmentFields(true);}});
      else if(reference.datatype_key && inspection.reference_values?.some(value=>value.node.kind === (selected.kind === "component" ? "subcomponent" : "component")) && (selected.kind==="field" || selected.kind==="repetition" || selected.kind==="component"))tabs.push({name:selected.kind === "component" ? "Subcomponents" : "Components",icon:"componentTab",selected:fieldComponents && Boolean(referenceKey),action:()=>void openReference(reference.datatype_key!,0,true,"",true)});
      for(const key of reference.table_keys??[])tabs.push({name:reference.table_keys!.length>1 ? `Table ${key.slice(6)}`:"Table",icon:"components",selected:referenceKey===key,action:()=>void openReference(key)});
      if(reference.element_key)tabs.push({name:"Data element",icon:"dataElement",selected:referenceKey===reference.element_key,action:()=>void openReference(reference.element_key!)});
    }
    return <div className="reader-reference-tabs" onKeyDown={referenceKeys} role="tablist" aria-label="Reference views">{tabs.map(tab=><button key={tab.name} type="button" role="tab" aria-selected={tab.selected} disabled={busy} onClick={tab.action}><ReaderIcon name={tab.icon}/>{tab.name}</button>)}</div>;
  };

  useLayoutEffect(()=>setSourceContent(workbenchElement?.querySelector<HTMLElement>(".reader-original .raw-numbered") ?? null),[workbenchElement,inspection,view,layout.wrap]);
  useLayoutEffect(()=>{
    const saved=scrollPositions.current.get(captionOwner);
    if(!workbenchElement)return;
    for(const [part,selector] of [["grid",".message-grid"],["source",".raw-source"],["details",".reader-reference-scroll"]] as const){
      const node=workbenchElement.querySelector<HTMLElement>(selector), position=saved?.[part];
      if(node){node.scrollTop=position?.top ?? 0;node.scrollLeft=position?.left ?? 0;}
    }
  },[captionOwner,workbenchElement,Boolean(inspection),referenceLoading]);
  const keepScroll = (target:EventTarget) => {
    if(!(target instanceof HTMLElement)||!inspection)return;
    const part=target.classList.contains("message-grid") ? "grid" : target.classList.contains("raw-source") ? "source" : target.classList.contains("reader-reference-scroll") ? "details" : null;
    if(!part)return;
    const positions=scrollPositions.current;
    positions.set(captionOwner,{...positions.get(captionOwner),[part]:{top:target.scrollTop,left:target.scrollLeft}});
    if(positions.size>64)positions.delete(positions.keys().next().value!);
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
          {gridProblem ? <p role="alert">{gridProblem}</p> : null}
          <div ref={setWorkbenchElement} onScrollCapture={event=>keepScroll(event.target)} style={{["--reader-reference-width" as string]:`${detailsWidth}rem`,["--reader-source-height" as string]:`${sourceHeight}rem`}} className={`reader-workbench reader-adjustable${compactWorkbench ? " compact-workbench" : ""}${detailsVisible ? "" : " reader-details-hidden"}`}>
          <div className={`reader-center reader-view-${view}`}>
      <header className="reader-header reader-context">
        {backLabel && onBack ? <BackLink label={backLabel} onBack={onBack} /> : null}
        <div className="reader-heading">
          <h2>{editorCaption}</h2>
          {onClose ? <button className="reader-tab-close" type="button" aria-label="Close message details" onClick={onClose}><img className="workbench-icon" src={closeAsset} alt="" /></button> : null}
          {facts.length > 0 ? <p className="reader-facts">{facts.join(" · ")}</p> : null}
        </div>
        <PHIToggle masked={inspection.phi_masked ?? false} disabled={busy} onToggle={()=>onReveal(Boolean(inspection.phi_masked))} />
        {!toolbarReference ? <ReferenceMenu edition={inspection.reference?.edition || inspection.metadata.hl7_version} disabled={busy || catalogReading} onAutomatic={useMessageReference} onChoose={()=>void chooseReference()} onLibrary={()=>setLibraryOpen(true)} /> : null}
        {inspection ? (
          <Menu
            className="reader-menu" trigger={<img className="workbench-icon" src={moreAsset} alt="" />}
            label="More message actions"
            items={[
              {label:"Fields",onSelect:()=>selectView("fields")},
              {label:"Raw",onSelect:()=>selectView("raw")},
              {label:"Hex",onSelect:()=>selectView("hex")},
              ...(onCompactReader && !toolbarReference ? [{label:compactReader ? "Wide view" : "Compact view",onSelect:onCompactReader}] : []),
              ...(onRequirements ? [{label:"Validation",onSelect:()=>{setDetailsVisible(true);setValidationShown(true);}}] : []),

          {label:"Choose local profile…",onSelect:() => void chooseOverlay("reference-profile"),disabled:busy},
          {label:"Choose exact profile pack…",onSelect:() => void chooseOverlay("reference-pack"),disabled:busy},
          {label:"Choose local documentation…",onSelect:() => void chooseOverlay("reference-documentation"),disabled:busy},
          {label:"Clear profile and documentation",onSelect:() => {++overlaySerial.current;setOverlaySelection({});void onInspect(selected?.path ?? "",inspection.node_offset,-1,undefined,catalogPath || suppliedCatalog || undefined,{},referencePin);},disabled:busy},

              ...(onRequirements ? [{label:"Interface requirements",onSelect:()=>onRequirements(inspection.selector||selected?.path||""),disabled:busy}] : []),
              ...(onValueMaps ? [{label:"Value maps",onSelect:()=>onValueMaps(inspection.selector),disabled:busy||!field||!inspection.selector}] : []),
              ...(onFieldValues ? [{label:"Field values",onSelect:()=>onFieldValues(inspection.selector),disabled:busy||!field||!inspection.selector}] : []),
              { label: "Go to field…", onSelect: () => setGoingTo(true), disabled: busy || inspection.decode_state === "unparsed" },
              { label: "Info", onSelect: () => setInfo(true) },
              { label: detailsVisible ? "Hide Details panel" : "Show Details panel", onSelect: () => setDetailsVisible(shown => !shown) },
              ...(onCreateVariant ? [{label:"Create variant",onSelect:onCreateVariant,disabled:busy}] : []),
            ]}
          />
        ) : null}
      </header>
          {view === "fields" ? <section className={`reader-original${layout.wrap ? " reader-source-wrap" : ""}`} aria-label="Original message"><div className="reader-source-toolbar"><span>Original message</span>{view === "fields" ? <div className="reader-source-controls" role="group" aria-label="Source presentation"><button type="button" aria-pressed={layout.fit} onClick={()=>setLayout(current=>({...current,fit:!current.fit}))}>Fit</button><button type="button" aria-pressed={layout.wrap} onClick={()=>setLayout(current=>({...current,wrap:!current.wrap}))}>Wrap</button></div> : null}</div>{revealed && (inspection.readable_window || inspection.raw_window) ? <RawText followSelection={inspection.grid?.mode !== "message" || Boolean(inspection.grid.follow_selection)} window={inspection.readable_window || inspection.raw_window!} busy={busy} onSelect={path=>void inspectSelection(path,inspection.readable_window?.offset)} onPage={(offset) => void onInspect(selected.path, inspection.node_offset, inspection.byte_offset, offset, catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)} /> : <p className="reader-hidden">{HIDDEN_VALUE}</p>}</section> : null}
          <div className="reader-grid-controls">{view === "fields" ? <ReaderResize label="Source height" horizontal className="reader-source-resize" value={sourceHeight} min={6} max={maxSourceHeight} onChange={height=>setLayout(current=>({...current,sourceHeight:height,fit:false}))} /> : null}<h3>Segment grid</h3>{inspection.grid?.mode === "message" ? <label className="reader-omitted-toggle"><input type="checkbox" checked={Boolean(inspection.grid.show_omitted)} disabled={busy} onChange={event=>void inspectSelection(selected.path,undefined,gridRequest({show_omitted:event.target.checked,offset:0}))}/>Show fields not present</label> : null}{inspection.grid?.mode === "message" ? <><ReaderColumns /><IconButton icon="up" label="Collapse all segments" disabled={busy || !inspection.grid.expanded?.length} onClick={()=>void inspectSelection("",undefined,gridRequest({expanded:[],offset:0,follow_selection:false}))}/></> : null}<IconButton icon="help" label="Column guide" className="reader-action" onClick={()=>{setDetailsVisible(true);setValidationShown(false);setColumnGuide(!columnGuide);}} /></div>
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
                  {inspection.grid?.mode === "message" ? <ReaderGrid key={captionOwner} grid={inspection.grid} selected={selected.path} busy={busy} value={row=>childValueText(row,revealed)} onSelect={go} onExpand={toggleBranch} onPage={pageGrid} /> : gridNodes.length > 0 ? (
                    <section ref={gridElement} className="reader-grid-scroll" aria-label="Segment grid" tabIndex={0}>
                      <div className="reader-grid-columns" aria-hidden="true">{["Path", "Name", "Type", "Opt", "Len", "C-Len", "Rep", "Item#", "Tbl", "Sect", "Value"].map((column) => <span key={column}>{column}</span>)}</div>
                      <ul className="outline reader-grid" aria-label={atRoot ? "Segments" : `Parts of ${selected.path}`}>
                        {visibleGridNodes.map((child) => <li key={child.node.path}>
                          <button type="button" className={`outline-row reader-grid-row reader-kind-${child.node.kind} ${child.node.path === selected.path ? "reader-grid-selected" : ""}`} aria-current={child.node.path === selected.path ? "location" : undefined} aria-label={`${child.node.path} ${child.node.kind === "segment" ? child.segment_name || child.node.segment : nodeName(child)} ${childValueText(child, revealed)}`} aria-expanded={child.node.kind === "segment" ? inspection.grid?.segment === child.node.path && !segmentCollapsed : undefined} disabled={busy} onClick={() => {
                            if (child.node.kind === "segment" && inspection.grid?.segment === child.node.path) setCollapsedSegment(segmentCollapsed ? null : {owner:captionOwner,path:child.node.path});
                            else go(child.node.path);
                          }}>
                            <span className={`selector${child.node.kind === "segment" ? " syntax-type" : ""}`} title={child.node.path}>{readablePath(child.node.path)}</span>
                            <span className={`outline-name reader-depth-${child.depth ?? 1}`}><span>{child.node.kind === "segment" ? <img className="workbench-icon" src={downAsset} alt="" /> : null}{child.node.kind === "segment" ? child.segment_name || child.node.segment : nodeName(child)}</span></span>
                            {[child.reference?.datatype, child.reference?.optionality, child.reference?.length, child.reference?.conformance_length, child.reference?.repetition, child.reference?.item, child.reference?.table, child.reference?.section].map((attribute, index) => <span className={`reader-attribute${attribute?.state === "specified" ? index === 0 ? " syntax-type" : index === 1 && attribute.value === "R" ? " syntax-date" : index === 5 ? " syntax-number" : index === 6 ? " syntax-code" : "" : ""}`} key={index} title={attributeText(attribute)}>{child.node.kind === "segment" ? "" : attribute?.state === "specified" ? attribute.value : attribute?.state === "not_specified" || attribute?.state === "not_applicable" ? "—" : "?"}</span>)}
                            <span className={`${revealed && child.node.state === "present" ? "outline-value" : "outline-value outline-state"}${child.reference?.datatype.state === "specified" && /^(DT|DTM|TM|TS)$/.test(child.reference.datatype.value) ? " syntax-date" : ""}`}>{child.node.kind === "segment" ? "" : childValueText(child, revealed)}</span>
                          </button>
                        </li>)}
                      </ul>
                    </section>
                  ) : null}
                  {!segmentCollapsed && inspection.grid && inspection.grid.field_count > 100 ? <nav className="pager" aria-label="Segment fields pages"><button type="button" disabled={busy || inspection.grid.offset === 0} onClick={() => void onInspect(inspection.grid!.segment,Math.max(0,inspection.grid!.offset-100),-1,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)}>Previous segment fields</button><span>{inspection.grid.offset+1}–{Math.min(inspection.grid.field_count,inspection.grid.offset+100)} of {inspection.grid.field_count}</span><button type="button" disabled={busy || inspection.grid.offset+100 >= inspection.grid.field_count} onClick={() => void onInspect(inspection.grid!.segment,inspection.grid!.offset+100,-1,undefined,catalogPath || suppliedCatalog || undefined,overlaySelection || suppliedSelection,referencePin)}>Next segment fields</button></nav> : null}
                  {inspection.grid?.mode !== "message" && !segmentCollapsed && inspection.child_count > inspection.children.length ? (
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
          <footer className="reader-grid-footer">{inspection.grid?.mode === "message" ? <><nav aria-label="Path" className="crumbs"><button type="button" disabled={busy} onClick={()=>go("")}>Message</button>{(inspection.parents ?? []).filter(parent=>parent.node.kind!=="repetition").map(parent=><button type="button" key={parent.node.path} title={parent.node.path} disabled={busy} onClick={()=>go(parent.node.path)}>{parent.display_path || parent.node.path}</button>)}{selected.path ? <code title={selected.path}>{inspection.display_path || selected.path}</code> : null}</nav></> : <>{segmentShortcuts.length ? <nav aria-label="Other segments" className="crumbs">{segmentShortcuts.map(row=><button key={row.node.path} type="button" title={row.node.path} aria-label={`Inspect ${row.node.path}`} disabled={busy} onClick={()=>go(row.node.path)}>{row.node.segment}</button>)}</nav> : null}<nav aria-label="Path" className="crumbs"><button type="button" disabled={busy} onClick={()=>go("")}>Segments</button>{selected.parent && selected.parent !== selected.path ? <button type="button" disabled={busy} onClick={()=>go(selected.parent)}>{selected.parent}</button> : null}</nav></>}</footer>
          </div>
          {detailsVisible && !compactWorkbench ? <ReaderResize label="Details width" reverse className="reader-details-resize" value={detailsWidth} min={20} max={maxDetailsWidth} onChange={width=>setLayout(current=>({...current,detailsWidth:width}))} /> : null}
          {contextDetails !== undefined && contextDetails !== null && !validationShown ? <section className="reader-reference" aria-label="Context details" hidden={!detailsVisible}><h3 className="reader-pane-heading">{contextDetailsTitle}</h3><div className="reader-reference-scroll">{contextDetails}</div></section> : inspection.reference ? <section className="reader-reference" aria-label="Reference details" hidden={!detailsVisible}>{validationShown ? <div className="reader-pane-tabs" onKeyDown={referenceKeys} role="tablist" aria-label="Inspector views"><button type="button" role="tab" aria-selected={!validationShown} onClick={()=>setValidationShown(false)}>Details</button><button type="button" role="tab" aria-selected={validationShown} onClick={()=>setValidationShown(true)}>Validation</button></div> : <div className="reader-pane-heading"><h3>Details</h3><button type="button" className="reader-plain-icon" aria-label="Hide Details panel" onClick={()=>setDetailsVisible(false)}><ReaderIcon name="close" /></button><button type="button" className="reader-plain-icon reader-pane-options" aria-label="Reference information" onClick={()=>setReferenceInfo(true)}><ReaderIcon name="settings" /></button></div>}{!validationShown && !columnGuide ? <ReaderSelectionHeader inspection={inspection} nodes={[...(inspection.parents ?? []),...gridNodes]} busy={busy} onSelect={go} onGoToField={()=>setGoingTo(true)} /> : null}<div className="reader-reference-scroll">
            {!validationShown && !columnGuide && field ? <div className="reader-value-control"><pre className="value" aria-label="Original value">{selected.state === "present" ? revealed ? inspection.raw || decodeStateText(inspection.decode_state) : HIDDEN_VALUE : stateText(selected.state,revealed,"")}</pre>{inspection.reference?.datatype_key && inspection.reference_values?.some(value=>value.node.kind === (selected.kind === "component" ? "subcomponent" : "component")) ? <button type="button" className="reader-plain-icon" aria-label={selected.kind === "component" ? "Inspect subcomponents" : "Inspect components"} disabled={busy} onClick={()=>void openReference(inspection.reference!.datatype_key!,0,true,"",true)}><ReaderIcon name="components" /></button> : null}<button type="button" className="reader-plain-icon" aria-label="Copy value" disabled={!revealed || selected.state!=="present" || !inspection.raw || inspection.decode_state==="too_large"} onClick={()=>void navigator.clipboard?.writeText(inspection.raw)}><ReaderIcon name="copy" /></button></div> : null}
            {!validationShown && !columnGuide && inspection.message_context && (atRoot || selected.segment==="MSH" && selected.field===9) ? <div className="reader-structure-strip"><span>Message structure</span><button type="button" className="link" disabled={busy || !inspection.message_context.structure_key} onClick={()=>inspection.message_context?.structure_key && void openReference(inspection.message_context.structure_key)}>{inspection.message_context.resolved_structure || "Not available"}</button></div> : null}
            {validationShown ? <section className="reader-validation" aria-label="Validation"><h4>Validation</h4><h5>No validation result available in this inspection</h5><p>Choose the specification used to check these messages. Reading reference metadata does not run validation.</p><button type="button" className="primary" disabled={busy} onClick={()=>onRequirements?.(inspection.selector || selected.path)}>Select specification</button><ValueRows rows={[{label:"Message",value:title},{label:"Field",value:inspection.selector || selected.path || "Message"}]} /><label>Original value</label><pre className="value">{selected.state === "present" ? revealed ? inspection.raw || decodeStateText(inspection.decode_state) : HIDDEN_VALUE : stateText(selected.state,revealed,"")}</pre>{onValidationFindings ? <button type="button" className="link" onClick={onValidationFindings}>View findings</button> : null}</section> : null}
            {!validationShown && !columnGuide && <>{referenceKey ? <>
              <h3 className="reader-reference-kind visually-hidden">{referenceKey.startsWith("table/") ? "Table reference" : referenceKey.startsWith("element/") ? "Data element" : referenceKey.startsWith("datatype/") ? "Datatype reference" : "Source definition"}</h3>
              {!fieldComponents ? <button type="button" className="link reader-reference-back" onClick={backReference}>{referenceTrail.length ? "Back to reference" : "Back to selected field"}</button> : null}
              <span className="visually-hidden">Selected field: {selected.path || "Message"}</span>

              {referenceTabs()}
              {referenceView?.reference?.record && !fieldComponents ? <div className="reader-entity-heading"><code className="reference-badge">{referenceCaption(referenceView.reference.record)}</code><h4>{referenceView.reference.record.name}</h4></div> : null}
              {referenceLoading ? <p role="status">Reading reference…</p> : null}
              {referenceView && referenceView.state !== "completed" ? <p role="alert">{referenceView.reason || "This reference is unavailable."}</p> : null}
              {referenceView?.reference?.reason ? <p role="status">{referenceView.reference.reason}</p> : null}
              {referenceView?.reference?.record ? <>

                {referenceView.reference.record.kind === "table" ? <>{referenceView.reference.record.unparsed_rows ? <p role="status">Partial table extraction: {referenceView.reference.record.unparsed_rows} source rows could not be interpreted. The receipt identifies each gap.</p> : null}</> : referenceView.reference.record.kind === "datatype" || referenceView.reference.record.kind === "element" ? null : <ReferenceAttributes record={referenceView.reference.record} />}



                {referenceView.reference.record.sequence?.length ? <StructureOutline nodes={referenceView.reference.record.sequence} /> : null}
                {fieldComponents ? null : referenceView.reference.record.definition ? <ReferenceDefinition text={referenceView.reference.record.definition} compact={compactDefinition(referenceView.reference.record.definition)} expanded={lookupDefinitionExpanded} onToggle={()=>setLookupDefinitionExpanded(shown=>!shown)} /> : <p>Definition not available.</p>}
                {referenceView.reference.record.kind === "element" ? <><dl className="reference-attributes"><div><dt>Data type</dt><dd><ReferenceBadge attribute={referenceView.reference.record.datatype}/></dd></div><div><dt>Length</dt><dd><ReferenceBadge attribute={referenceView.reference.record.length}/></dd></div></dl><section className="reader-element-uses"><h4>Used in ({referenceView.reference.record.uses?.length ?? 0})</h4>{referenceView.reference.record.uses?.map(key=><button type="button" key={key} className="link" onClick={()=>void openReference(key)}>{key}</button>)}</section></> : null}
              </> : null}
              {referenceView?.reference?.record?.kind === "table" ? <details className="reader-table-metadata"><summary>Metadata</summary><ValueRows rows={[{label:"Edition",value:referenceView.reference.edition},{label:"Table",value:referenceView.reference.record.table_id||"Not available"},{label:"Kind",value:referenceView.reference.record.table_kind||"Not available"},...(referenceView.reference.record.table_metadata ? [
                    {label:"Table OID",value:referenceView.reference.record.table_metadata.table_oid || "Not available"},
                    {label:"Code system OID",value:referenceView.reference.record.table_metadata.code_system_oid || "Not available"},
                    {label:"Value set OID",value:referenceView.reference.record.table_metadata.value_set_oid || "Not available"},
                    {label:"Code system URL",value:referenceView.reference.record.table_metadata.code_system_url || "Not available"},
                    {label:"Code system version",value:referenceView.reference.record.table_metadata.code_system_version || "Not available"},
                    {label:"Identifier source",value:(referenceView.reference.sources?.find(source=>source.role===referenceView?.reference?.record?.table_metadata?.origin.source)?.file || "Not available")+" · "+referenceView.reference.record.table_metadata.origin.locator},
                  ] : []),{label:"Source",value:referenceView.reference.record.source||"Not available"}]} /></details> : null}
              {referenceKey.startsWith("table/") ? <>
                {(referenceView?.total_count ?? 0)>10 || codeQuery || activeQuery ? <form className="reader-code-search" onSubmit={(event) => {event.preventDefault();void openReference(referenceKey,0,false,codeQuery);}}><label htmlFor="reference-code-query">Search codes</label><div><input id="reference-code-query" value={codeQuery} maxLength={128} onChange={(event) => setCodeQuery(event.target.value)} /><button type="submit" disabled={referenceLoading || busy}>Search</button></div></form> : null}
                {referenceView?.reference?.record?.content_state === "available" ? <>{activeQuery ? <p className="reader-code-count">{referenceView.child_count} of {referenceView.total_count} values</p> : null}<div className="reader-code-scroll"><table className="data-table" aria-label="Reference codes"><thead><tr><th>Code</th><th>Description</th></tr></thead><tbody>{referenceView.children.map((code) => <tr key={code.key} className={revealed && inspection.reference?.table_keys?.includes(referenceKey) && inspection.decode_state === "decoded" && code.code === inspection.decoded ? "reader-code-selected" : ""}><td><button type="button" onClick={() => void openReference(code.key)}>{code.code}</button></td><td><strong>{code.name || code.code}</strong>{code.definition && code.definition!==code.name ? <p>{code.definition}</p> : null}{!code.name && !code.definition ? <span>Meaning not available</span> : null}</td></tr>)}</tbody></table></div>{referenceView.child_count === 0 ? <p>No codes match this search.</p> : null}{referenceView.child_count>100 ? <nav className="pager" aria-label="Code pages"><button type="button" disabled={referenceLoading || referenceView.offset === 0} onClick={() => void openReference(referenceKey,Math.max(0,referenceView.offset-100),false,activeQuery)}>Previous codes</button><span>{referenceView.child_count ? referenceView.offset+1 : 0}–{referenceView.offset+referenceView.children.length} of {referenceView.child_count}</span><button type="button" disabled={referenceLoading || referenceView.offset+referenceView.children.length >= referenceView.child_count} onClick={() => void openReference(referenceKey,referenceView.offset+100,false,activeQuery)}>Next codes</button></nav> : null}</> : null}
              </> : null}
              {inspection.reference_values_notice ? <p role="status">{inspection.reference_values_notice}</p> : null}
              {referenceView?.reference?.record?.kind === "datatype" && !entityOverview && referenceView.children?.length ? <div className="reader-components"><div className="reader-components-heading"><h4>Components</h4><span>{referenceView.child_count} items</span></div><div className="reader-components-scroll"><table className="data-table" aria-label="Datatype components"><thead><tr><th>#</th><th>Name</th><th>DT</th><th>Opt</th><th>Len</th><th>Original</th></tr></thead><tbody>{referenceView.children.map(component=>{
                const value=inspection.reference_values?.find(value=>value.key===component.key);
                const original=value ? value.node.state==="omitted" ? "Omitted" : value.node.state==="present" ? revealed ? value.encoded+(value.truncated ? " · Truncated" : "") : HIDDEN_VALUE : stateText(value.node.state,revealed,"") : "Not available";
                return <tr key={component.key}><td>{component.position}</td><td><button type="button" className="link" onClick={()=>void openReference(component.key)}>{component.name}</button></td><td><ReferenceBadge attribute={component.datatype}/></td><td><ReferenceBadge attribute={component.optionality}/></td><td>{attributeText(component.length)}</td><td><span title={original}>{original}</span>{value && revealed && value.node.state==="present" && (value.decoded!==value.encoded || PROBLEMS.has(value.decode_state)) ? <details><summary>Decoded value</summary>{PROBLEMS.has(value.decode_state) ? decodeStateText(value.decode_state) : value.decoded}</details> : null}</td></tr>;
              })}</tbody></table></div></div> : null}
              {referenceView?.reference?.record?.kind === "datatype" && referenceView.child_count > 100 ? <nav aria-label="Datatype component pages"><button type="button" disabled={referenceLoading || referenceView.offset === 0} onClick={() => void openReference(referenceKey, Math.max(0, referenceView.offset-100),false)}>Previous components</button><button type="button" disabled={referenceLoading || referenceView.offset + referenceView.children.length >= referenceView.child_count} onClick={() => void openReference(referenceKey,referenceView.offset+100,false)}>Next components</button></nav> : null}
            </> : <>
            {referenceTabs()}
            {inspection.reference.edition && inspection.reference.edition !== inspection.metadata.hl7_version ? <aside className="notice warning" role="status"><p>Reference: HL7 {inspection.reference.edition}<br/>Message declares: HL7 {inspection.metadata.hl7_version || "Not available"}</p><button type="button" disabled={busy || catalogReading} onClick={() => void chooseReference(inspection.metadata.hl7_version)}>Return to message edition</button></aside> : null}
            {inspection.reference.reason ? <p role="status">{inspection.reference.reason}</p> : null}
            {inspection.reference.type_resolution === "unresolved" ? <p>Reference declared datatype: {inspection.reference.declared_datatype || "Not available"}. Contextual resolution: Not available.</p> : null}
            {segmentFields ? <div className="reader-components-scroll"><table className="data-table reader-segment-fields" aria-label="Segment fields"><thead><tr><th>Path</th><th>Name</th><th>Type</th><th>Opt</th></tr></thead><tbody>{gridNodes.filter(row=>row.node.kind==="field"&&row.node.segment===selected.segment).map(row=><tr key={row.node.path}><td><button type="button" className="link" onClick={()=>go(row.node.path)}>{row.display_path || readablePath(row.node.path)}</button></td><td>{row.label}</td><td>{attributeText(row.reference?.datatype)}</td><td>{attributeText(row.reference?.optionality)}</td></tr>)}</tbody></table></div> : null}
            {inspection.reference.record && !segmentFields ? <>
              <div className="reader-definition-heading"><button type="button" className={`reader-plain-icon${definitionVisible ? "" : " reader-definition-closed"}`} aria-label={definitionVisible ? "Hide definition" : "Show definition"} aria-expanded={definitionVisible} onClick={()=>setDefinitionVisible(shown=>!shown)}><ReaderIcon name="disclosure" /></button><ReaderIcon name="book" /><button type="button" className="link" disabled={busy || !inspection.reference.section_key} onClick={()=>inspection.reference?.section_key && void openReference(inspection.reference.section_key)}>§{attributeText(inspection.reference.record.section)}</button><span>{inspection.display_path || readablePath(selected.path) || inspection.reference.record.name} {inspection.reference.record.name}</span></div>
              {inspection.reference.record.definition ? definitionVisible ? <ReferenceDefinition tables={inspection.reference.table_keys ?? []} onTable={key=>void openReference(key)} text={inspection.reference.record.definition} compact={compactDefinition(inspection.reference.record.definition)} expanded={fullDefinition} onToggle={()=>setFullDefinition(shown=>!shown)} /> : null : <p>Definition not available.</p>}
              {field ? <ReferenceAttributes record={inspection.reference.record} datatype={inspection.reference.datatype_key ? ()=>void openReference(inspection.reference!.datatype_key!) : undefined} element={inspection.reference.element_key ? ()=>void openReference(inspection.reference!.element_key!) : undefined} /> : <section className="reader-segment-evidence" aria-label="Selected segment evidence"><ValueRows rows={[{label:"Occurrence",value:selected.path},{label:"Original byte span",value:`${selected.start}–${selected.end}`}]} /><label>Actual segment</label><pre className="value">{revealed ? inspection.raw_window?.selected || inspection.raw || decodeStateText(inspection.decode_state) : HIDDEN_VALUE}</pre>{inspection.grid?.rows.find(row=>row.node.kind==="field" && row.node.field===9 && row.node.segment===selected.segment) ? <IconButton icon="book" label="Inspect Message Type" className="reader-action" disabled={busy} onClick={()=>go(`${selected.path}-9`)} /> : null}</section>}
              <div className="reader-reference-links">
                {onRequirements ? <IconButton icon="book" label="Open specification" className="reader-action" disabled={busy} onClick={()=>onRequirements(inspection.selector || selected.path)} /> : null}
                {onFieldValues && field && inspection.selector ? <IconButton icon="search" label="Field values" className="reader-action" disabled={busy} onClick={()=>onFieldValues(inspection.selector)} /> : null}
                {inspection.reference.table_keys?.map((key) => <button key={key} type="button" className="link" disabled={busy} onClick={() => void openReference(key)}>Table · {key.slice("table/".length)}</button>)}
                {inspection.reference.parent_datatype_key ? <button type="button" className="link" disabled={busy} onClick={() => void openReference(inspection.reference!.parent_datatype_key!)}>Parent datatype · {inspection.reference.record.container}</button> : null}
              </div>
            </> : null}
            {field ? <div className="selected-field-actions">

              {onFilterByField && inspection.selector ? <IconButton icon="filter" label="Filter by this field" className="reader-action" onClick={()=>onFilterByField(inspection.selector,revealed && selected.state === "present" ? inspection.decoded || null : null,selected.state)} /> : null}
            </div> : null}
            </>}
            </>}
            {!validationShown && inspection.reference_overlay ? <section className="reader-profile-overlay" aria-label="Selected profile context"><h4>Selected profile context</h4><p>{inspection.reference_overlay.status} · {inspection.reference_overlay.reason} · Applicability: {inspection.reference_overlay.applicability||"Not established"}</p>{inspection.reference_overlay.profile ? <p>Profile: {inspection.reference_overlay.profile.id} · {inspection.reference_overlay.profile.version} · {inspection.reference_overlay.profile.sha256}</p> : null}{inspection.reference_overlay.pack ? <p>Exact pack: {inspection.reference_overlay.pack.id} · {inspection.reference_overlay.pack.version} · {inspection.reference_overlay.pack.sha256}</p> : null}{inspection.reference_overlay.base ? <p>Authored base: {inspection.reference_overlay.base.hl7_version}/{inspection.reference_overlay.base.family} · {inspection.reference_overlay.base.pack.id}/{inspection.reference_overlay.base.pack.version}</p> : null}{inspection.reference_overlay.field ? <><h5>Local field constraints, separate from base reference</h5><p>Parent field: {selected.segment}-{inspection.reference_overlay.field.position} · Usage {inspection.reference_overlay.field.usage} · Datatype {inspection.reference_overlay.field.type || "Not specified"}</p>{inspection.reference_overlay.field.condition ? <p>Conditional predicate: {inspection.reference_overlay.field.condition.segment}-{inspection.reference_overlay.field.condition.position} · {inspection.reference_overlay.field.condition.operator}. Displayed as authored; not evaluated here.</p> : null}{inspection.reference_overlay.field.cardinality ? <p>Local field cardinality: {inspection.reference_overlay.field.cardinality.min}..{inspection.reference_overlay.field.cardinality.max}</p> : null}</> : null}{inspection.reference_overlay.segment_cardinality ? <p>Local segment cardinality: {inspection.reference_overlay.segment_cardinality.min}..{inspection.reference_overlay.segment_cardinality.max}</p> : null}{inspection.reference_overlay.segment_description ? <p>{inspection.reference_overlay.segment_description}</p> : null}{inspection.reference_overlay.documentation ? <details><summary>{inspection.reference_overlay.documentation.title}</summary><p>{inspection.reference_overlay.documentation.identity}</p><pre className="value">{inspection.reference_overlay.documentation.text}</pre></details> : null}<p>No inspection selection changes historical evaluator pins or establishes a validation/ACK verdict.</p></section> : null}
            {!validationShown && columnGuide ? <section className="reader-column-guide" aria-label="Reference attributes"><h4>Column guide</h4><ValueRows rows={[
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
          </div></section> : <section className="reader-reference" aria-label="Selected details" hidden={!detailsVisible}><div className="reader-pane-heading"><h3>Details</h3><button type="button" className="reader-plain-icon" aria-label="Hide Details panel" onClick={()=>setDetailsVisible(false)}><ReaderIcon name="close" /></button><button type="button" className="reader-plain-icon reader-pane-options" aria-label="Reference information" onClick={()=>setReferenceInfo(true)}><ReaderIcon name="settings" /></button></div><ReaderSelectionHeader inspection={inspection} nodes={[...(inspection.parents ?? []),...gridNodes]} busy={busy} onSelect={go} onGoToField={()=>setGoingTo(true)} /><div className="reader-reference-scroll">{field ? <><label>Original encoded value</label><pre className="value">{selected.state === "present" ? revealed ? inspection.raw || decodeStateText(inspection.decode_state) : HIDDEN_VALUE : stateText(selected.state,revealed,"")}</pre><div className="selected-field-actions">{revealed && selected.state === "present" && (inspection.decoded || inspection.raw) ? <button type="button" onClick={()=>void navigator.clipboard?.writeText(inspection.raw)}>Copy value</button> : null}{onFilterByField && inspection.selector ? <button type="button" onClick={()=>onFilterByField(inspection.selector,revealed && selected.state === "present" ? inspection.decoded || null : null,selected.state)}>Filter by this field</button> : null}</div></> : null}</div></section>}
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


      <ReferenceLibrary open={libraryOpen} messageEdition={inspection?.metadata.hl7_version || ""} onClose={()=>setLibraryOpen(false)} onSelect={async entry=>{
        if(!inspection || !selected)return;
        setCatalogPath(entry?.path || "");setCatalogIdentity(entry?.identity || "");
        await onInspect(selected.path,inspection.node_offset,-1,undefined,entry?.path || "",overlaySelection || suppliedSelection,entry?.identity || "");
      }}/>
      <Modal open={referenceInfo && Boolean(inspection)} title="Reference information" onClose={()=>setReferenceInfo(false)}>
        {inspection && !inspection.reference ? <p>No offline reference is selected. Use Reference to select a catalog.</p> : null}
        {referenceKey && referenceView?.reference?.record ? <section aria-label="Current reference provenance"><h3>{referenceView.reference.record.name}</h3><p>{referenceView.reference.record.source} · {referenceView.reference.edition}</p><ReferenceOrigins record={referenceView.reference.record} {...(referenceView.reference.sources ? {sources:referenceView.reference.sources}: {})}/></section> : null}
        {inspection?.reference && selected ? <>
          {field && revealed && inspection.decode_state==="decoded" && inspection.decoded!==inspection.raw ? <><h3>Decoded value</h3><pre className="value">{inspection.decoded}</pre></> : null}
          {inspection.message_context ? <details className="reader-context-provenance reader-extra-metadata reader-structure-provenance"><summary>Message structure provenance</summary><p>Transmitted MSH-9.3: {inspection.message_context.declared_structure_state === "present" ? inspection.message_context.declared_structure || "Not available" : inspection.message_context.declared_structure_state === "omitted" ? "Omitted" : inspection.message_context.declared_structure_state}</p><p>Reference structure: {inspection.message_context.resolved_structure || "Not available"} · {inspection.message_context.status}</p>{inspection.message_context.reason ? <p role="status">{inspection.message_context.reason}</p> : null}{inspection.message_context.message_key ? <button type="button" disabled={busy} onClick={() => {setReferenceInfo(false);void openReference(inspection.message_context!.message_key);}}>Message overview</button> : null}{inspection.message_context.structure_key ? <button type="button" disabled={busy} onClick={() => {setReferenceInfo(false);void openReference(inspection.message_context!.structure_key);}}>Structure overview</button> : null}{selected.segment ? <details><summary>Selected occurrence placement</summary><p>{inspection.message_context.placement.state} · {inspection.message_context.placement.reason}</p>{inspection.message_context.placement.state === "known" ? <><p>{inspection.message_context.placement.path}</p><p>Segment placement: {inspection.message_context.placement.segment_min}..{inspection.message_context.placement.segment_max}; occurrence {inspection.message_context.placement.segment_repetition}</p><ul>{inspection.message_context.placement.groups.map((group) => <li key={`${group.name}/${group.occurrence}`}>{group.name}[{group.occurrence}] · {group.min}..{group.max}</li>)}</ul><p>Placement cardinality is separate from field repetition and reusable segment definitions.</p></> : null}</details> : null}</details> : null}
          {inspection.reference.record ? <>              <div className="reader-source-reference"><p>{[...new Set(inspection.reference.sources?.map((source) => source.publisher) ?? [])].join(" · ")} · {inspection.reference.edition}</p><p className="reader-reference-citation">{inspection.reference.record.source} · §{attributeText(inspection.reference.record.section)}</p><p>{referenceOriginText(inspection.reference.record.definition_origin,inspection.reference.sources)}</p></div>
              <details className="reader-extra-metadata"><summary>Source attributes and provenance</summary><ValueRows rows={referenceRows(inspection.reference.record).slice(5)} /><ReferenceOrigins record={inspection.reference.record} {...(inspection.reference.sources?{sources:inspection.reference.sources}:{})}/>{inspection.reference.record.conformance_length.state === "specified" ? <p>Conformance length describes the receiving application’s storage capacity, not a sending maximum. The source’s permitted lengths and truncation markers remain as printed.</p> : null}</details>
</> : null}
                      {inspection.reference.parent_field ? <aside className="reader-parent-field"><h5>Parent field notes</h5><p>{inspection.reference.parent_field.segment}-{inspection.reference.parent_field.field} · {inspection.reference.parent_field.name}</p><p>Parent Item#: {attributeText(inspection.reference.parent_field.item)} · Parent repetition: {attributeText(inspection.reference.parent_field.repetition)}</p></aside> : null}
            <details className="reader-extra-metadata"><summary>Message and reference edition</summary><p>Message edition: {inspection.metadata.hl7_version || "Not available"}</p><p>Reference edition: {inspection.reference.edition || (inspection.reference.status === "not_selected" ? "Not selected" : "Not available")}</p></details>
            <p className="reader-reference-coverage">Catalog coverage: {inspection.reference.coverage.segments} segments · {inspection.reference.coverage.fields} fields{inspection.reference.coverage.datatypes ? ` · ${inspection.reference.coverage.datatypes} datatypes · ${inspection.reference.coverage.components ?? 0} components` : ""} · {inspection.reference.coverage.definitions} definitions{inspection.reference.coverage.tables ? ` · ${inspection.reference.coverage.tables} tables · ${inspection.reference.coverage.codes ?? 0} codes · ${inspection.reference.coverage.elements ?? 0} data elements` : ""}{inspection.reference.missing_count ? ` · ${inspection.reference.missing_count} gaps or ambiguities` : ""}</p>
            {inspection.reference.missing_count ? <details><summary>Coverage gaps</summary><ul>{inspection.reference.coverage.missing.map((gap) => <li key={gap}>{gap}</li>)}</ul>{inspection.reference.missing_count > inspection.reference.coverage.missing.length ? <p>Additional gaps are listed in the extraction receipt.</p> : null}</details> : null}
            <p className="reader-reference-coverage">Reference metadata does not establish validation or ACK success. Existing findings remain available in their finding detail.</p>

        </> : null}
      </Modal>

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
export function RawText({ window: raw, busy, onPage, onSelect, followSelection = true }: { followSelection?: boolean; window: RawWindow; busy: boolean; onPage: (offset: number) => void; onSelect?: (path: string) => void }) {
  const paged = raw.message_end - raw.message_start > RAW_WINDOW;
  const lineStart=raw.before.lastIndexOf("\n")+1;
  const nextLine=raw.after.indexOf("\n");
  const lineEnd=nextLine<0 ? raw.after.length : nextLine;
  return (
    <>
      {raw.lines ? <MessageText lines={raw.lines} busy={busy} followSelection={followSelection} onSelect={onSelect} /> : <pre className="value raw">
        {raw.selected ? <>{raw.before.slice(0,lineStart)}<span className="selected-line">{raw.before.slice(lineStart)}<mark>{raw.selected}</mark>{raw.after.slice(0,lineEnd)}</span>{raw.after.slice(lineEnd)}</> : <>{raw.before}{raw.after}</>}
      </pre>}
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

function PHIToggle({masked,disabled,onToggle}:{masked:boolean;disabled:boolean;onToggle:()=>void}) {
 return <IconButton icon={masked ? "phi-on" : "phi-off"} label={masked ? "Disable PHI masking" : "Enable PHI masking"} pressed={masked} disabled={disabled} onClick={onToggle} className="reader-action reader-phi-toggle" />;
}
