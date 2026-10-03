import { useEffect, useRef, useState } from "react";
import { listWholeCatalog, openItemDraft, type CaptureSourceDraft, type CatalogItem, type RequestContext } from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { Modal } from "./layout";
import { SourceEditor } from "./Capture";

/** Receiving/acquisition configuration is a source, never a send target.
 * Saving settings does not arm a listener or collect anything. */
export function ReceiveConfigurations({context,onClose}:{context:()=>RequestContext;onClose:()=>void}) {
 const [sources,setSources]=useState<CatalogItem[]>([]);
 const [reason,setReason]=useState<string|null>(null);
 const [loading,setLoading]=useState(true);
 const [editing,setEditing]=useState<CatalogItem|"new"|null>(null);
 const [draft,setDraft]=useState<CaptureSourceDraft|null>(null);
 const turns=useRef(0);
 useEffect(()=>{let live=true;const request=context();void listWholeCatalog({context:request,kind:"source",filter:{}}).then(answer=>{if(!live)return;setLoading(false);if(answer.state==="completed" || answer.state==="empty")setSources(answer.page?.items??[]);else setReason(answer.reason??"Receive configurations could not be read.");});return()=>{live=false;turns.current++;};},[context]);
 const edit=async(item:CatalogItem)=>{const turn=++turns.current;setReason(null);const answer=await openItemDraft({context:context(),ref:item.ref});if(turn!==turns.current)return;if(answer.state!=="completed" || !answer.draft?.source){setReason(answer.reason??"This receive configuration cannot be edited.");return;}setDraft(answer.draft.source);setEditing(item);};
 const columns:Column<CatalogItem>[]=[{key:"name",header:"Receive configuration",priority:1,minWidth:14,flex:true,render:item=>item.name},{key:"type",header:"Source type",priority:2,minWidth:10,render:item=>item.summary.source?.type??"Unknown"},{key:"address",header:"Listen/source scope",priority:2,minWidth:12,render:item=>item.summary.source?.address||"Not configured"},{key:"edit",header:"",priority:1,minWidth:6,render:item=><button type="button" disabled={item.availability!=="available"} aria-label={`Edit ${item.name}`} onClick={()=>void edit(item)}>Edit</button>}];
 return editing ? <SourceEditor context={context} item={editing==="new" ? null:editing} draft={draft} title="Receive setup" onClose={()=>setEditing(null)} onSaved={()=>onClose()} /> : <Modal open title="Receive configurations" onClose={onClose}>
 <p>These sources configure bounded listening or evidence acquisition. They do not grant send authority, and saving them starts nothing.</p>
 {reason ? <p role="alert">{reason}</p>:null}
 <button type="button" disabled={loading} onClick={()=>{setDraft(null);setEditing("new");}}>New receive configuration</button>
 <DataTable label="Receive configurations" rows={sources} rowId={item=>item.ref.id} rowLabel={item=>item.name} columns={columns} selected={null} onSelect={()=>undefined} onOpen={id=>{const item=sources.find(value=>value.ref.id===id);if(item)void edit(item);}} loading={loading}/>
 </Modal>;
}
