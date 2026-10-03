import { useEffect, useRef, useState } from "react";
import { listValueMaps, readValueMap, readValueMapDraft, saveValueMap, importValueMapCSV, exportValueMapCSV, inspectValueMap, listWholeCatalog, newIntentId } from "./bindings";
import type { RequestContext, ItemRef, CatalogItem, ValuemapDocument, ValuemapEntry, ValueMapInspectionResult, ValueMapEditorDraft } from "./bindings";
import { useRetainer, RetentionStatus } from "./drafting";
import { DataTable, type Column } from "./DataTable";
import { FormDialog } from "./layout";
import "./value-maps.css";
type Props = {
    context: () => RequestContext;
    source: ItemRef;
    identity: string;
    occurrence: string;
    selector: string;
    edition: string;
    busy?: boolean;
    onBack: () => void;
};
const emptyEntry: ValuemapEntry = { source: "", destination: "", source_meaning: "", destination_meaning: "", provenance: "" };
export function ValueMaps(props: Props) { return <ValueMapBody key={JSON.stringify([props.source, props.identity, props.occurrence, props.selector, props.edition])} {...props}/>; }
function ValueMapBody({ context, source, identity, occurrence, selector, edition, busy = false, onBack }: Props) {
    const owner = JSON.stringify([source, identity, occurrence, selector, edition]);
    const active = useRef(owner);
    active.current = owner;
    const contextOwner = useRef(context);
    contextOwner.current = context;
    const serial = useRef(0);
    const formSerial = useRef(0);
    const live = (held: string, heldContext: () => RequestContext) => active.current === held && contextOwner.current === heldContext;
    const [items, setItems] = useState<CatalogItem[]>([]);
    const [selected, setSelected] = useState<ItemRef | null>(null);
    const [saved, setSaved] = useState<ValuemapDocument | null>(null);
    const [draft, setDraftState] = useState<ValuemapDocument | null>(null);
    const [problem, setProblem] = useState("");
    const [inspection, setInspection] = useState<ValueMapInspectionResult | null>(null);
    const [consent, setConsent] = useState({ owner, reveal: false });
    const consentOwner = JSON.stringify([owner, selected]);
    const reveal = consent.owner === consentOwner && consent.reveal;
    const setDraft = (next: ValuemapDocument | null) => { ++formSerial.current; setDraftState(next); };
    const [discardAction, setDiscardAction] = useState<(() => void) | null>(null);
    const [publishing, setPublishing] = useState(false);
    const publishingRef = useRef(false);
    const requestAction = (action: () => void) => draft ? setDiscardAction(() => action) : action();
    const retainer = useRetainer(owner);
    const retainedContent = useRef("");
    const [entry, setEntry] = useState<ValuemapEntry | null>(null);
    const [entryIndex, setEntryIndex] = useState<number | null>(null);
    const [configurations, setConfigurations] = useState<CatalogItem[]>([]);
    const [declaration, setDeclaration] = useState("");
    const refresh = async () => { const held = owner; const result = await listValueMaps(context()); if (!live(held, context))
        return; if (result.state === "completed") {
        setItems(result.items);
        setSelected(old => old || result.items.find(item=>item.availability==="available"&&item.ref.revision)?.ref || null);
    }
    else
        setProblem(result.reason || "Value maps could not be read."); };
    useEffect(() => { ++serial.current; ++formSerial.current; setItems([]); setSelected(null); setSaved(null); setDraft(null); setEntry(null); setInspection(null); setProblem(""); setConsent({ owner, reveal: false }); void refresh(); const mine = formSerial.current; void readValueMapDraft({ context: context(), source, identity, occurrence, selector, edition }).then(result => { if (!live(owner, context) || formSerial.current !== mine)
        return; if (result.state === "completed" && result.retained && result.draft) {
        retainer.keepId(result.retained.id);
        retainedContent.current = JSON.stringify(result.draft);
        setSelected(result.draft.base || null);
        setDraftState(result.draft.draft);
        setEntry(result.draft.working_entry || null);
        setEntryIndex(result.draft.working_index ?? null);
        setProblem("Restored authored map draft for these exact source IDs. Contextual reveal stays off.");
    } }); return () => { ++serial.current; ++formSerial.current; }; }, [owner, context]);
    useEffect(() => { if (!selected)
        return; const held = owner; const mine = ++serial.current; setSaved(null); setInspection(null); void readValueMap({ context: context(), ref: selected }).then(result => { if (!live(held, context) || serial.current !== mine)
        return; if (result.state === "completed" && result.map) {
        setSaved(result.map);
        setProblem("");
    }
    else
        setProblem(result.reason || "The map revision cannot be read."); }); }, [owner, context, selected?.id, selected?.revision]);
    useEffect(() => { if (!saved || !selected)
        return; const held = owner; let disposed = false; setInspection(null); void inspectValueMap({ context: context(), ref: selected, source, identity, occurrence, selector, reveal }).then(result => { if (!disposed && live(held, context)) {
        setInspection(result);
        if (result.state !== "completed")
            setProblem(result.reason || "Contextual mapping is unavailable.");
    } }); return () => { disposed = true; }; }, [owner, context, saved, selected?.id, selected?.revision, reveal]);
    const content = JSON.stringify(draft ? { schema: "readmit-field-value-map-editor/v1", source, identity, occurrence, selector, edition, ...(selected ? { base: selected } : {}), draft, ...(entry ? { working_entry: entry } : {}), ...(entryIndex !== null ? { working_index: entryIndex } : {}) } : null);
    useEffect(() => { if (!draft || retainedContent.current === content)
        return; retainedContent.current = content; const value = JSON.parse(content) as ValueMapEditorDraft; retainer.save({ id: "", kind: "field-value-map", workspace: context().project, case: "", identity: "", content_schema: "readmit-field-value-map-editor/v1", content: value, ...(selected && draft.project ? { item: { project_id: draft.project, ref: selected } } : {}) }); }, [content, retainer.save]);
    const create = () => { ++serial.current; ++formSerial.current; setSelected(null); setSaved(null); setInspection(null); setDraft({ schema: "readmit-field-value-map/v1", project: "", name: "", edition, source_selector: selector, destination_selector: selector, source_meaning: "", destination_meaning: "", provenance: "", entries: [], associations: [] }); setProblem(""); };
    const save = async () => { if (!draft || publishingRef.current)
        return; publishingRef.current = true; setPublishing(true); const held = owner; const mine = formSerial.current; const result = await saveValueMap({ context: context(), ...(selected ? { base: selected } : {}), intent_id: newIntentId(), draft }); publishingRef.current = false; setPublishing(false); if (!live(held, context) || formSerial.current !== mine)
        return; if (result.state !== "completed" || !result.ref) {
        setProblem(result.reason || "The map was not saved; the draft is kept.");
        return;
    } const dropped = await retainer.dropCurrent(); if (!dropped)
        setProblem("Map revision saved; private draft could not be discarded. Retry draft discard explicitly."); setDraft(null); setSelected(result.ref); await refresh(); };
    const importCSV = async () => { const held = owner; const mine = formSerial.current; const result = await importValueMapCSV({ context: context() }); if (!live(held, context) || formSerial.current !== mine)
        return; if (result.state === "completed" && result.map) {
        setDraft(result.map);
        setProblem("CSV validated into an unsaved draft. Save explicitly to publish one whole revision.");
    }
    else if (result.state !== "cancelled")
        setProblem(result.reason || "No entries were imported."); };
    const exportCSV = async () => { if (!selected)
        return; const held = owner; const mine = formSerial.current; const result = await exportValueMapCSV({ context: context(), ref: selected }); if (!live(held, context) || formSerial.current !== mine)
        return; setProblem(result.state === "completed" ? "Authored mapping CSV exported: " + result.path : result.reason || "The CSV was not exported."); };
    const loadConfigurations = async () => { const held = owner; const result = await listWholeCatalog({ context: context(), kind: "environment", filter: {} }); if (live(held, context))
        setConfigurations(result.page?.items.filter(item => Boolean(item.ref.revision)) || []); };
    const chooseMap=(id:string)=>{
        const chosen=items.find(candidate=>candidate.ref.id===id);
        if(chosen?.availability==="available"&&chosen.ref.id===selected?.id&&chosen.ref.revision===selected?.revision)return;
        requestAction(()=>{
        ++formSerial.current;++serial.current;setSaved(null);setDraft(null);setInspection(null);
        const item=chosen;
        if(!item||item.availability!=="available"||!item.ref.revision){setSelected(null);setProblem(item?.reason||"This file has no retained catalog revision. Import CSV and save a map explicitly.");return;}
        setProblem("");setSelected(item.ref);
    });
    };
    const maps: Column<CatalogItem>[] = [{key:"name",header:"Value maps",priority:1,minWidth:11,flex:true,render:item=><div className="requirements-rail-item"><strong>{item.name}</strong><span>{item.ref.revision ? `Revision ${item.ref.revision}`:"Unretained"}</span><span>{item.availability==="available"?"Retained":item.availability==="unsupported"?"Unsupported schema":"Unavailable"}</span>{item.reason ? <small>{item.reason}</small>:null}</div>}];
    const entries: Column<ValuemapEntry>[] = [{ key: "source", header: "Source value", priority: 1, minWidth: 10, flex: true, render: e => e.source }, { key: "destination", header: "Recorded destination", priority: 1, minWidth: 10, flex: true, render: e => e.destination }, { key: "source_meaning", header: "Source meaning", priority: 2, minWidth: 10, render: e => e.source_meaning }, { key: "destination_meaning", header: "Destination meaning", priority: 2, minWidth: 10, render: e => e.destination_meaning }, { key: "provenance", header: "Reference", priority: 1, minWidth: 12, render: e => e.provenance }];
    const shown = draft || saved;
    return <section className="value-maps" aria-label="Field value maps"><header className="value-map-header"><h2>Value maps</h2><button type="button" onClick={() => requestAction(onBack)}>Return to selected message</button></header><p>Exact selected field {selector} · source edition {edition || "Not declared"}. Recorded mappings deploy nothing and preserve source evidence.</p>{problem ? <p role="status">{problem}</p> : null}
 <RetentionStatus retention={retainer.retention} onRetry={retainer.retry} onKeepAsNew={retainer.keepAsNew}/>
 <div className="value-map-actions"><button type="button" disabled={busy} onClick={() => requestAction(create)}>Create value map</button><button type="button" disabled={busy} onClick={() => requestAction(() => void importCSV())}>Import CSV</button><button type="button" disabled={busy || !selected || Boolean(draft)} onClick={() => void exportCSV()}>Export CSV</button></div>
 <div className="value-map-workspace"><aside className="value-map-rail"><DataTable label="Interface value maps" rows={items} rowId={item => item.ref.id} rowLabel={item => item.name} columns={maps} selected={selected?.id || null} onSelect={chooseMap} onOpen={chooseMap}/></aside>
 <div className="value-map-detail">{shown ? <><h3>{shown.name || "New field value map"} · {shown.source_selector}</h3><p>{draft ? "Unsaved draft" : "Saved revision " + selected?.revision} · {shown.edition} · destination {shown.destination_selector}</p><p>{shown.source_meaning} → {shown.destination_meaning}</p><p>{shown.provenance}{shown.table ? " · Table " + shown.table.id + " · " + shown.table.edition + " · " + shown.table.provenance : " · No table bound"}</p>
 {draft ? <><label htmlFor="map-name">Name</label><input id="map-name" value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })}/>{(["edition", "source_selector", "destination_selector", "source_meaning", "destination_meaning", "provenance"] as const).map(key => <label key={key}>{({ edition: "HL7 edition", source_selector: "Exact source selector", destination_selector: "Exact destination selector", source_meaning: "Source field meaning", destination_meaning: "Destination field meaning", provenance: "Map provenance" })[key]}<input value={draft[key]} onChange={e => setDraft({ ...draft, [key]: e.target.value })}/></label>)}<label>Optional table identity<input value={draft.table?.id || ""} onChange={e => { if (!e.target.value) {
                const { table, ...without } = draft;
                setDraft(without);
            }
            else
                setDraft({ ...draft, table: { id: e.target.value, edition: draft.edition, provenance: draft.table?.provenance || "" } }); }}/></label>{draft.table ? <label>Table provenance<input value={draft.table.provenance} onChange={e => setDraft({ ...draft, table: { ...draft.table!, provenance: e.target.value } })}/></label> : null}
 <div className="value-map-actions"><button type="button" disabled={busy} onClick={() => { setEntryIndex(null); setEntry({ ...emptyEntry }); }}>Add entry</button><button type="button" disabled={busy || publishing} onClick={() => void save()}>Save map revision</button><button type="button" onClick={() => requestAction(() => { ++formSerial.current; setDraft(null); setEntry(null); })}>Discard draft</button><button type="button" onClick={() => void loadConfigurations()}>Choose configuration declaration</button></div><label>Configuration revision<select value={declaration} onChange={e => setDeclaration(e.target.value)}><option value="">No new association</option>{configurations.map(item => <option key={item.ref.id} value={item.ref.id + "@" + item.ref.revision}>{item.name} · {item.ref.revision}</option>)}</select></label><button type="button" disabled={!declaration} onClick={() => { const item = configurations.find(i => i.ref.id + "@" + i.ref.revision === declaration); if (item && item.ref.revision)
                setDraft({ ...draft, associations: [...draft.associations, { kind: "environment", item: item.ref.id, revision: item.ref.revision }] }); }}>Record configuration association</button></> : <button type="button" disabled={busy} onClick={() => { ++formSerial.current; setDraft(saved); setProblem(""); }}>Edit map</button>}
 {shown.associations.map(a => <p key={a.kind + a.item + a.revision}>Recorded declaration: {a.kind} {a.item} · revision {a.revision}; not external deployment proof.</p>)}
 <DataTable label="Recorded mapping entries" rows={shown.entries} rowId={e => e.source} rowLabel={e => e.source} columns={entries} selected={null} onSelect={() => undefined} onOpen={id => { if (!draft)
            return; const i = draft.entries.findIndex(e => e.source === id); setEntryIndex(i); setEntry({ ...draft.entries[i]! }); }}/>
 {!draft && inspection && inspection.ref.id === selected?.id && inspection.ref.revision === selected?.revision && inspection.revealed === reveal ? <><h3>Selected source field</h3><p>{inspection.state === "completed" ? inspection.mapping : inspection.reason} · {inspection.field_state || "State unavailable"}</p>{inspection.entry ? <p>Recorded destination: {inspection.entry.destination} · {inspection.entry.destination_meaning} · {inspection.entry.provenance}</p> : null}<button type="button" onClick={() => setConsent({ owner: consentOwner, reveal: !reveal })}>{reveal ? "Hide contextual mapping" : "Show contextual mapping for this source"}</button><p>An unmapped value has no recorded destination; it is not a predicted receiver error.</p></> : null}</> : <p>Choose or create a field-scoped interface value map.</p>} {entry && draft ? <form className="value-map-entry-card" aria-label="Value map entry" onSubmit={event=>{event.preventDefault();if(draft.entries.some((value,index)=>value.source===entry.source && index!==entryIndex)){setProblem("A source value already has an entry. Edit it instead.");return;}setDraft({...draft,entries:entryIndex===null?[...draft.entries,entry]:draft.entries.map((value,index)=>index===entryIndex?entry:value)});setEntry(null);}}><h3>Value map entry</h3>{(["source","destination","source_meaning","destination_meaning","provenance"] as const).map(key=><label key={key}>{({source:"Source value",destination:"Destination value",source_meaning:"Source value meaning",destination_meaning:"Destination value meaning",provenance:"Entry provenance"})[key]}<input value={entry[key]} onChange={event=>setEntry({...entry,[key]:event.target.value})}/></label>)}<div className="value-map-actions"><button type="button" onClick={()=>setEntry(null)}>Cancel</button><button type="submit">Save entry</button></div></form>:null}</div></div>
 <FormDialog open={Boolean(discardAction)} title="Discard unsaved value map?" submitLabel="Discard draft" onClose={() => setDiscardAction(null)} onSubmit={async () => { if (!await retainer.dropCurrent())
        return { reason: "The retained draft could not be discarded; keep editing or retry." }; const action = discardAction; setDiscardAction(null); setDraft(null); setEntry(null); action?.(); return null; }}>The authored draft has not been saved. Discard it explicitly to leave this map.</FormDialog>
</section>;
}
