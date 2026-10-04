import { useCallback, useEffect, useRef, useState } from "react";
import { chooseShareDestination, listSourceExports, openSourceExport, type ItemRef, type RequestContext, type ReviewedActionResult } from "./bindings";
import type { SourceExportEntry } from "./bindings.gen";
import { useReviewedAction } from "./reviewedAction";
import { DataTable, type Column } from "./DataTable";
import { Modal, Reveal, ValueRows } from "./layout";
import { TaskTabs } from "./TaskTabs";
import { ShareOutputPreview } from "./ShareReport";
import "./selected-export.css";

type Step="contents"|"redaction"|"preview";
const STEPS:{key:Step;label:string}[]=[{key:"contents",label:"Contents"},{key:"redaction",label:"Redaction"},{key:"preview",label:"Preview"}];

export function SelectedMessagesExportPanel({open,context,source,identity,messages,sourceName,onClose}:{open:boolean;context:()=>RequestContext;source:ItemRef|null;identity:string;messages:string[];sourceName:string;onClose:()=>void}) {
 const owner=JSON.stringify([context().project,context().project_id??"",source?.id,source?.revision,identity,messages]);
 const action=useReviewedAction(open,owner);
 const [step,setStep]=useState<Step>("contents");
 const [reveal,setReveal]=useState(false);
 const [destination,setDestination]=useState("");
 const [result,setResult]=useState<ReviewedActionResult|null>(null);
 const [working,setWorking]=useState(false);
 const submitted=useRef(false);
 const prepare=useCallback(async()=>{
  if(!open||!source||messages.length===0)return;
  await action.prepare({context:context(),action:"capture.export-selected",items:[source],selected_export:{messages,identity,transformation:"original",destination,reveal}});
 },[open,owner,destination,reveal,action.prepare]);
 useEffect(()=>{setStep("contents");setReveal(false);setDestination("");setResult(null);submitted.current=false;},[open,owner]);
 useEffect(()=>{void prepare();},[prepare]);
 const review=action.review?.selected_export;
 const previewShown=Boolean(reveal&&review?.output.files.every(file=>file.size===0||Boolean(file.text?.length||file.data?.length)));
 const choose=async()=>{
  if(!review)return;
  action.invalidate();setResult(null);
  const answer=await chooseShareDestination({context:context(),name:review.output.name});
  if(answer.state==="completed"&&answer.destination)setDestination(answer.destination);
  else void prepare();
 };
 const save=async()=>{
  if(submitted.current||!reveal||step!=="preview")return;
  const started=action.begin(context(),{});
  if(!started)return;
  submitted.current=true;setWorking(true);
  const answer=await started.execution;
  if(started.current()) {
   setResult(answer);
   if(answer.outcome==="stale"&&answer.refreshed){action.adopt(answer.refreshed);submitted.current=false;}
  }
  setWorking(false);
 };
 const columns:Column<NonNullable<typeof review>["occurrences"][number]>[]=[
  {key:"order",header:"Order",priority:1,minWidth:4,render:occurrence=>String((review?.occurrences.indexOf(occurrence)??0)+1)},
  {key:"occurrence",header:"Occurrence",priority:1,minWidth:12,render:occurrence=>occurrence.occurrence},
  {key:"kind",header:"Kind",priority:1,minWidth:8,render:occurrence=>occurrence.kind},
  {key:"bytes",header:"Bytes",priority:1,minWidth:6,render:occurrence=>String(occurrence.bytes)},
 ];
 return <Modal open={open} title="Export capture" className="capture-export-sheet" size="wide" onClose={working?()=>undefined:onClose} footer={<button type="button" disabled={working} onClick={onClose}>Cancel</button>}>
  <button type="button" className="capture-export-back" disabled={working} onClick={onClose}>Back to capture</button>
  <p className="selected-export-scope">{sourceName} · {messages.length} selected occurrences</p>
  <TaskTabs label="Export steps" id="selected-export-steps" tabs={STEPS} selected={step} onSelect={setStep}>{null}</TaskTabs>
  {action.failure?<p role="alert">{action.failure}</p>:null}
  {review?<div className="selected-export-layout">
   <section className="selected-export-choices" aria-label="Export choices"><ValueRows label="Output" rows={[{label:"Output",value:review.destination.name||review.output.name},{label:"Format",value:"Original message bytes"},{label:"Bytes",value:String(review.output.size)},{label:"Location",value:review.destination.name?`${review.destination.location||"Local file"} · ${review.destination.name}`:"Choose location"}]}/><button type="button" disabled={working} onClick={()=>void choose()}>Choose location</button>
    <h3>Contains original values</h3><p>Not redacted</p><p>Selected original occurrence bytes only; stored framing is retained.</p>
    {step==="preview"?<Reveal revealed={reveal} disabled={working} onToggle={next=>{action.invalidate();setReveal(next);}}/>:null}
    <p className="selected-export-consequence">{review.consequence}</p>{action.review?.refusal?<p role="status">{action.review.refusal}</p>:null}
    <button type="button" className="primary" disabled={working||step!=="preview"||!previewShown||!action.review?.ready||submitted.current} onClick={()=>void save()}>Save local file</button>
   </section>
   <section className="selected-export-preview" aria-label="Exact content preview"><h2>Exact content preview</h2>
    {step==="contents"?<><ValueRows rows={[{label:"Source",value:review.source_name},{label:"Source identity",value:review.source_identity}]}/><DataTable label="Contents" selected={null} rows={review.occurrences} rowId={row=>row.occurrence} rowLabel={row=>row.occurrence} columns={columns} onSelect={()=>undefined} onOpen={()=>undefined}/></>:null}
    {step==="redaction"?<p>Original bytes are preserved. This selection is an ordinary extract; it is not minimized or proven replay-equivalent. Original attachments, reports, tests and unselected source records are excluded.</p>:null}
    {step==="preview"?(reveal?<ShareOutputPreview files={review.output.files}/>:<p>Original values are hidden until you reveal the exact bytes.</p>):null}
   </section>
  </div>:<p role="status">Preparing the selected scope…</p>}
  {result?.outcome==="completed"&&result.selected_export?<div role="status">Exported {result.selected_export.name}{result.reason?<p role="alert">{result.reason}</p>:null}</div>:result&&result.outcome!=="stale"?<p role="alert">{result.reason??"Nothing was exported."}</p>:null}
 </Modal>;
}

export function SourceExportHistory({context,source,open,onClose}:{context:()=>RequestContext;source:ItemRef;open:boolean;onClose:()=>void}) {
 const [entries,setEntries]=useState<SourceExportEntry[]|null>(null);
 const [failure,setFailure]=useState("");
 const [preview,setPreview]=useState<import("./bindings.gen").ShareOutput|null>(null);
 const [opening,setOpening]=useState(false);
 const showPreview=async(entry:SourceExportEntry)=>{
  const request=context();setOpening(true);setPreview(null);
  const answer=await openSourceExport({context:request,ref:entry.ref});
  if(answer.context.project===request.project&&(answer.context.project_id??"")===(request.project_id??"")) {
   if(answer.state==="completed"&&answer.output){setPreview(answer.output);setFailure("");}
   else if(answer.state!=="cancelled")setFailure(answer.reason??"The exported file could not be verified.");
  }
  setOpening(false);
 };
 useEffect(()=>{if(!open)return;let live=true;const request=context();setEntries(null);void listSourceExports({context:request,ref:source}).then(answer=>{if(!live||answer.context.project!==request.project||(answer.context.project_id??"")!==(request.project_id??""))return;if(answer.state==="completed"){setEntries(answer.entries);setFailure("");}else setFailure(answer.reason??"History could not be read.");});return()=>{live=false;};},[open,source.id,source.revision,context]);
 const columns:Column<SourceExportEntry>[]=[{key:"output",header:"Output",priority:1,minWidth:12,render:entry=>entry.source.name},{key:"source",header:"Source",priority:1,minWidth:12,render:entry=>`${entry.source.occurrences.length} selected occurrences`},{key:"format",header:"Format",priority:1,minWidth:12,render:()=>"Original message bytes"},{key:"at",header:"Written",priority:2,minWidth:14,render:entry=>entry.at},{key:"preview",header:"Preview",priority:1,minWidth:10,render:entry=><button type="button" disabled={opening} onClick={()=>void showPreview(entry)}>Open preview</button>}];
 return <Modal open={open} title="Capture exports" onClose={onClose} size="wide">{failure?<p role="alert">{failure}</p>:entries===null?<p role="status">Reading…</p>:entries.length===0?<p>No exports yet</p>:<DataTable label="Exports" selected={null} rows={entries} rowId={entry=>entry.ref.id} rowLabel={entry=>entry.source.name} columns={columns} onSelect={()=>undefined} onOpen={()=>undefined}/>}{preview?<section aria-label="Exact exported content"><h2>Contains original values</h2><ShareOutputPreview files={preview.files}/></section>:null}<p>Source evidence remains available. These records identify selected original bytes; they do not establish redaction or replay equivalence.</p></Modal>;
}
