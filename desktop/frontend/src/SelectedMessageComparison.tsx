import { useEffect, useState } from "react";
import { compareCases,type CaseComparisonResult,type ItemRef,type RequestContext,type CaseComparisonRow } from "./bindings";
import { DataTable,type Column } from "./DataTable";
import { Modal,Reveal,ValueRows } from "./layout";
import { FIELD_STATES,HIDDEN_VALUE,term } from "./display";
import { ShareOutputPreview } from "./ShareReport";
import {typeLabel} from "./Messages";
import "./selected-comparison.css";

/** The two roles are the exact ordered checked occurrences. No key guesses a
 * match, and no timestamp/control-ID normalization is selected implicitly. */
export function SelectedMessageComparison({source,identity,messages,context,onClose,onCreateTest}:{source:ItemRef;identity:string;messages:string[];context:()=>RequestContext;onClose:()=>void;onCreateTest:()=>void}) {
 const [reveal,setReveal]=useState(false);
 const [result,setResult]=useState<CaseComparisonResult|null>(null);
 const [offset,setOffset]=useState(0);
 useEffect(()=>{let current=true;setResult(null);const request=context();void compareCases({context:request,current:source,other:source,pair:{left:messages[0]!,right:messages[1]!},keys:[],fields:[],original:true,reveal,offset,limit:100}).then(answer=>{if(!current)return;if(answer.comparison?.current.identity && answer.comparison.current.identity!==identity){setResult({state:"failed",context:request,reason:"The source changed. The initiating selection is kept; reopen its evidence before comparing."});return;}setResult(answer);});return()=>{current=false;};},[source.id,source.revision,identity,JSON.stringify(messages),reveal,offset,context]);
 const comparison=result?.comparison;
 const value=(state:string|undefined,text:string|undefined)=>text || (state ? term(FIELD_STATES,state).text:HIDDEN_VALUE);
 const columns:Column<CaseComparisonRow>[]=[{key:"field",header:"Field",priority:1,minWidth:12,flex:true,render:row=>row.field||row.kind},{key:"left",header:"Left",priority:1,minWidth:10,render:row=>value(row.earlier_state,row.earlier_value)},{key:"right",header:"Right",priority:1,minWidth:10,render:row=>value(row.later_state,row.later_value)},{key:"change",header:"Result",priority:1,minWidth:8,render:row=>row.change}];
 return <Modal className="reader-comparison-workspace" open title="Compare messages" size="wide" onClose={onClose} footer={<><button type="button" onClick={onClose}>Back to reader</button><button type="button" disabled={!comparison} onClick={onCreateTest} className="primary">Create test case</button></>}>
 {result?.reason ? <p role="alert">{result.reason}</p>:null}
 {comparison ? <><div className="selected-message-pair">{[comparison.current,comparison.other].map((side,index)=><section key={index} aria-label={index===0 ? "Left selected message":"Right selected message"}><h2>{index===0 ? "Left":"Right"} · {typeLabel({kind:"message",code:side.message_code??"",trigger:side.trigger_event??""})} · {side.occurrence || messages[index]}</h2>{side.name ? <p>{side.name}</p> : null}<details className="reader-pair-provenance"><summary>Original evidence provenance</summary><ValueRows rows={[{label:"Occurrence",value:side.occurrence??"Unavailable"},{label:"Source identity",value:<code>{side.identity}</code>},{label:"Original bytes",value:String(side.raw_bytes??0)},{label:"Original SHA-256",value:<code>{side.raw_sha256}</code>}]} /></details>{comparison.pair_output?.files[index] ? <ShareOutputPreview files={[comparison.pair_output.files[index]!]}/>:null}</section>)}</div><p>Explicit occurrence pair · {comparison.raw_equal ? "Original bytes are identical":"Original bytes differ"}. No replay equivalence is established.</p><Reveal revealed={reveal} onToggle={setReveal}/>{reveal && !comparison.pair_output ? <p>Exact original bytes exceed the preview bound. Their byte counts and SHA-256 identities remain available.</p>:null}<DataTable label="Selected message differences" rows={comparison.rows} rowId={row=>String(row.position)} rowLabel={row=>row.field||row.kind} columns={columns} selected={null} onSelect={()=>undefined} onOpen={()=>undefined}/>{offset+comparison.rows.length<comparison.total ? <button onClick={()=>setOffset(offset+comparison.rows.length)}>Next differences</button>:null}{offset>0 ? <button onClick={()=>setOffset(Math.max(0,offset-100))}>Previous differences</button>:null}</>:<p>{result ? "The comparison is unavailable; original evidence is unchanged.":"Reading the selected pair…"}</p>}
 </Modal>;
}
