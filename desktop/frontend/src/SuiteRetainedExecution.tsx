import {useEffect,useRef,useState} from "react";
import {openRun,type RequestContext,type RunDetail,type SuiteRunRow} from "./bindings";
import {DataTable} from "./DataTable";
import {listDate} from "./Projects";

/** Inspects one selected retained suite and one job at a time. Each field
 * comes from that job's verified run; unopened jobs never borrow its counts. */
export function SuiteRetainedExecution({runs,context}:{runs:SuiteRunRow[];context:()=>RequestContext}) {
 const [chosen,setChosen]=useState("");
 const [loadedRun,setRun]=useState<{scope:()=>RequestContext;owner:string;detail:RunDetail}|null>(null);
 const [loadedJob,setJob]=useState<{scope:()=>RequestContext;owner:string;detail:RunDetail}|null>(null);
 const [selected,setSelected]=useState("");
 const [failure,setFailure]=useState<string|null>(null);
 const serial=useRef(0);
 const active=runs.find(row=>row.run.id===chosen)??runs[0];
 useEffect(()=>{
  const owner=++serial.current;setRun(null);setJob(null);setSelected("");setFailure(null);
  if(!active)return;
  const asked=context();
  void openRun({context:asked,run:active.run,reveal:false}).then(answer=>{
   if(owner!==serial.current)return;
   if(answer.state!=="completed"||!answer.run){setFailure(answer.reason??"The selected execution is unavailable.");return;}
   setRun({scope:context,owner:active.run.id,detail:answer.run});const first=answer.run.jobs[0];if(first)setSelected(first.id);
  });return()=>{serial.current++;};
 },[active?.run.id,context]);
 useEffect(()=>{
  if(!active||!selected)return;
  let current=true;setJob(null);setFailure(null);
  void openRun({context:context(),run:active.run,job:selected,reveal:false}).then(answer=>{
   if(!current)return;
   if(answer.state==="completed"&&answer.run)setJob({scope:context,owner:JSON.stringify([active.run.id,selected]),detail:answer.run});
   else setFailure(answer.reason??"This retained job is unavailable.");
  });return()=>{current=false;};
 },[active?.run.id,selected,context]);
 const run=loadedRun?.scope===context && loadedRun?.owner===active?.run.id?loadedRun?.detail:null;
 const job=loadedJob?.scope===context && loadedJob?.owner===JSON.stringify([active?.run.id,selected])?loadedJob.detail:null;
 const count=job?.checks.find(check=>check.check.operator==="ledger_count");

 const typedCounts=job?.lifecycle?.checks.filter(check=>check.operator==="row-count")??[];
 const typed=typedCounts.length===1 ? typedCounts[0]:undefined;
 const expected=typed ? String(typed.expected_count??"Unavailable projection") : count ? String(count.check.count??"Unavailable projection") : "Unavailable projection";
 const observed=typed ? typed.unavailable ? "Unavailable":String(typed.observed_count??"Unavailable projection") : count ? count.unavailable ? "Unavailable":String(count.observed?.count??count.observed_records??"Unavailable projection") : "Unavailable projection";
 return <section className="workflow-suite-execution" aria-label="Selected suite execution"><h3>Selected execution</h3><label className="workflow-caption">Retained run<select value={active?.run.id??""} onChange={event=>setChosen(event.target.value)}>{runs.map(row=><option key={row.run.id} value={row.run.id}>{listDate(row.started_at)} · {row.outcome??"Not recorded"}</option>)}</select></label>{failure?<p role="alert">{failure}</p>:null}{!runs.length?<p className="workflow-caption">No retained executions</p>:run?<DataTable label="Selected execution results" rows={run.jobs} rowId={row=>row.id} rowLabel={row=>row.test} selected={selected||null} onSelect={setSelected} onOpen={setSelected} columns={[{key:"test",header:"Test case",priority:1,minWidth:15,flex:true,render:row=>row.test},{key:"expected",header:"Expected",priority:1,minWidth:7,render:row=>row.id===selected&&job?expected:"Open evidence"},{key:"observed",header:"Observed",priority:1,minWidth:7,render:row=>row.id===selected&&job?observed:"Open evidence"},{key:"result",header:"Result",priority:1,minWidth:7,render:row=>row.result??row.reason??"Not recorded"}]}/>:<p aria-live="polite">Reading retained execution…</p>}</section>;
}
