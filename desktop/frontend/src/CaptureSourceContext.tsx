import { useEffect, useRef, useState } from "react";
import { listWholeCatalog, readCaptureContext, saveCaptureContext, readContextEditorDraft, newIntentId, type CaptureContextResult, type CaptureAssociation, type CatalogItem, type ItemRef, type RequestContext, type CaptureSourceEditorDraft } from "./bindings";
import { FormDialog, Modal, ValueRows } from "./layout";
import { useLifecycle } from "./lifecycle";
import { useRetainer, RetentionStatus } from "./drafting";
import "./context-editors.css";
const SCHEMA="readmit-capture-source-context-editor/v1";
export function CaptureSourceContext(props:{projectID?:string;source:ItemRef;context:()=>RequestContext;onClose:()=>void;onChanged:()=>void}) {
 return <CaptureSourceBody key={JSON.stringify([props.context().project,props.source])} {...props}/>;
}
/** Authored corrections live in the existing private store. The original
 * evidence and observed provenance are never altered by this editor. */
function CaptureSourceBody({source,context,onClose,onChanged,projectID:suppliedProjectID=""}:{projectID?:string;source:ItemRef;context:()=>RequestContext;onClose:()=>void;onChanged:()=>void}) {
 const [result,setResult]=useState<CaptureContextResult|null>(null);
 const [targets,setTargets]=useState<CatalogItem[]>([]);
 const [draft,setDraft]=useState<CaptureAssociation[]|null>(null);
 const [base,setBase]=useState<ItemRef|undefined>(undefined);
 const [editing,setEditing]=useState(false);
 const [problem,setProblem]=useState("");
 const [projectID,setProjectID]=useState(suppliedProjectID||context().project_id||"");
 const owner=JSON.stringify([context().project,source]);
 const retainer=useRetainer(owner);
 const retained=useRef("");
 const published=useRef<{content:string}|null>(null);
 const current=useRef(true);
 const editSerial=useRef(0);
 const saving=useLifecycle<"save">({window:true});
 useEffect(()=>{let live=true;current.current=true;const mine=editSerial.current;const request=context();void Promise.all([readCaptureContext({context:request,ref:source}),listWholeCatalog({context:request,kind:"environment",filter:{}}),listWholeCatalog({context:request,kind:"source",filter:{}})]).then(async([answer,environments,sources])=>{
  if(!live)return;setResult(answer);setTargets([...(environments.page?.items??[]),...(sources.page?.items??[])]);
  if(!answer.capture)return;
  const restore=await readContextEditorDraft({context:request,schema:SCHEMA,source,identity:answer.capture.identity,occurrence:"",selector:""});
  if(!live)return;setProjectID(held=>restore.context.project_id||suppliedProjectID||held);if(editSerial.current!==mine)return;
  if(restore.state==="completed" && restore.correction && restore.retained){
   if(JSON.stringify(restore.correction.sources.map(value=>value.source_id))!==JSON.stringify(answer.capture.sources.map(value=>value.source_id))){setProblem("Retained correction names another source scope. It is kept without rebinding.");return;}
   retainer.keepId(restore.retained.id);retained.current=JSON.stringify(restore.correction);setBase(restore.correction.base);setDraft(restore.correction.sources);setEditing(true);
  }else if(restore.state!=="empty")setProblem(restore.reason??"Private source corrections could not be read.");
 });return()=>{live=false;current.current=false;};},[source.id,source.revision,context]);
 useEffect(()=>{
  if(!draft || !result?.capture || !projectID)return;
  const content:CaptureSourceEditorDraft={schema:SCHEMA,project_id:projectID,source,identity:result.capture.identity,...(base ? {base}:{}),sources:draft};
  const text=JSON.stringify(content);if(text===retained.current)return;retained.current=text;
  retainer.save({id:retainer.currentId(),kind:"capture-source-context",workspace:context().project,case:"",identity:result.capture.identity,content_schema:SCHEMA,content});
 },[draft,base,projectID,result?.capture?.identity]);
 const keep=async(action:()=>void)=>{if(draft){const answer=await retainer.flush();if(answer.state!=="saved"){setProblem("Source corrections could not be kept. Retry the private draft save before leaving.");return;}}if(current.current)action();};
 const discard=async()=>{if(!await retainer.dropCurrent()){setProblem("The retained corrections could not be discarded. Your work is kept.");return;}retained.current="";setDraft(null);setEditing(false);setProblem("");published.current=null;};
 const targetKey=(ref:ItemRef|undefined)=>ref ? `${ref.kind}:${ref.id}@${ref.revision??""}`:"";
 const nameOf=(ref:ItemRef|undefined)=>ref ? targets.find(target=>target.ref.id===ref.id && target.ref.revision===ref.revision)?.name??"Recorded target unavailable":"Not recorded";
 const update=(index:number,change:Partial<CaptureAssociation>)=>setDraft(held=>held?.map((value,i)=>{if(i!==index)return value;const next={...value,...change};const {received_at,...rest}=next;return received_at ? {...rest,received_at}:rest;})??null);
 return <><Modal className="capture-context-sheet" open={!editing} title="Capture source context" size="wide" onClose={()=>void keep(onClose)}>
 {problem || result?.reason ? <p role="alert">{problem||result?.reason}</p>:null}
 {result?.capture ? <><div className="context-source-grid">{result.capture.sources.map(association=><section className="context-source-card" key={association.source_id}><ValueRows rows={[{label:"Source",value:association.source||"Unknown"},{label:"Channel",value:association.channel||"Unknown"},{label:"Received at",value:association.received_at_name||nameOf(association.received_at)},{label:"Basis",value:association.basis}]} /></section>)}</div><p className="muted">Received at records source provenance. It grants no send or connection authority.</p><button type="button" onClick={()=>{++editSerial.current;if(!draft){setBase(result.ref);setDraft(result.capture!.sources.map(value=>({source_id:value.source_id,source:value.source,channel:value.channel,...(value.received_at ? {received_at:value.received_at}:{}),basis:"manual",remember:false})));}setEditing(true);}}>Correct source context</button></>:<p>{result ? "Source context is unavailable. Original evidence remains intact.":"Reading source context…"}</p>}
 </Modal>
 <FormDialog className="capture-context-sheet" open={editing} title="Correct source context" size="wide" onClose={()=>void keep(()=>setEditing(false))} submitLabel="Save" busy={saving.running!==null} secondary={<button type="button" onClick={()=>void discard()}>Discard draft</button>} onSubmit={async()=>{
 if(!result?.capture || !draft)return {reason:"Read source context before correcting it."};
 if(published.current?.content===JSON.stringify(draft)){if(!await retainer.dropCurrent())return {reason:"Published context is retained; private cleanup still failed."};retained.current="";setDraft(null);setEditing(false);published.current=null;return null;}
 if((await retainer.flush()).state!=="saved")return {reason:"The latest source corrections are not retained. Retry their private save."};
 const answer=await saving.run("save",()=>saveCaptureContext({context:context(),source,identity:result.capture!.identity,...(base ? {base}:{}),intent_id:newIntentId(),sources:draft.map(value=>({...value,basis:value.received_at||value.source||value.channel ? "manual":"unknown"}))}));
 if(!answer || answer.state!=="completed")return {reason:answer?.reason??"The context was not saved."};
 setResult(answer);published.current={content:JSON.stringify(draft)};onChanged();
 if(!await retainer.dropCurrent())return {reason:"Source context was saved, but its private draft could not be discarded. Retry cleanup; authored corrections remain retained."};
 retained.current="";setDraft(null);setEditing(false);setProblem("");return null;
 }}>
 {problem ? <p role="alert">{problem}</p>:null}<RetentionStatus retention={retainer.retention} onRetry={retainer.retry}/>
 <p className="muted">These manually authored corrections stay beside the original evidence.</p>
 <div className="context-source-grid">{draft?.map((value,index)=><fieldset className="context-source-card" key={value.source_id}><legend>Source {index+1}</legend>
 <label>Source<input value={value.source} maxLength={200} onChange={event=>update(index,{source:event.target.value})}/></label>
 <label>Channel<input value={value.channel} maxLength={200} onChange={event=>update(index,{channel:event.target.value})}/></label>
 <label>Received at<select value={targetKey(value.received_at)} onChange={event=>{const chosen=targets.find(target=>targetKey(target.ref)===event.target.value);if(!chosen && event.target.value!=="")return;const {received_at,...rest}=value;setDraft(held=>held?.map((entry,i)=>i!==index?entry:{...rest,...(chosen ? {received_at:chosen.ref}:{}),basis:"manual",remember:false})??null);}}><option value="">Not recorded</option>{value.received_at && !targets.some(target=>targetKey(target.ref)===targetKey(value.received_at)) ? <option value={targetKey(value.received_at)}>Recorded revision {value.received_at.revision} (not in current choices)</option>:null}{targets.filter(target=>target.availability==="available" && Boolean(target.ref.revision)).map(target=><option key={targetKey(target.ref)} value={targetKey(target.ref)}>{target.ref.kind==="source" ? `Receive configuration: ${target.name}`:target.name} · revision {target.ref.revision}</option>)}</select></label>
 <label className="check"><input type="checkbox" checked={value.remember??false} disabled={!value.received_at || !value.source || !value.channel} onChange={event=>update(index,{remember:event.target.checked})}/>Remember this source/channel association</label>
 </fieldset>)}</div>
 </FormDialog></>;
}
