import { useEffect, useRef, useState } from "react";
import { cancel, readFieldValues, readFieldValueOccurrences } from "./bindings";
import type { FieldValueScope, FieldValuesResult, FieldValueBucket, FieldValueOccurrencesResult, MessageRow } from "./bindings";
import { DataTable, Pager, type Column } from "./DataTable";
import "./field-values.css";

function bucketLabel(row: FieldValueBucket): string {
  if (row.hidden) return "Hidden present values";
  if (row.state === "present") return row.value || "Present · empty decoded text";
  return ({ empty:"Empty", null:"Null", omitted:"Omitted", undecodable:"Undecodable", undecided:"Undecided / outside grouping bound" } as Record<string,string>)[row.state] || "Not available";
}

/** A local, explicit whole-capture/filter count. Neither bucket values nor
 * handles are persisted in route/session state. Exact source IDs back every
 * drilldown, and an ownership change immediately hides previous read results. */
export function FieldValues({ scope, selector, label, onBack, onInspectOccurrence, busy=false }: {
  scope: FieldValueScope;
  selector: string;
  label?: string;
  onBack: () => void;
  onInspectOccurrence: (occurrence: string, selector: string) => void;
  busy?: boolean;
}) {
  const [field,setField]=useState(selector);
  const [input,setInput]=useState(selector);
  const [revealConsent,setRevealConsent]=useState<{owner:string;shown:boolean}|null>(null);
  const consentOwner=JSON.stringify([scope,field]);
  const revealed=revealConsent?.owner===consentOwner && revealConsent.shown;
  const [answer,setAnswer]=useState<{owner:string;result:FieldValuesResult}|null>(null);
  const [reading,setReading]=useState(false);
  const [problem,setProblem]=useState("");
  const [selectedBucket,setSelectedBucket]=useState<string|null>(null);
  const [members,setMembers]=useState<{owner:string;result:FieldValueOccurrencesResult}|null>(null);
  const [membersReading,setMembersReading]=useState(false);
  const [selectedMessage,setSelectedMessage]=useState<string|null>(null);
  const countSerial=useRef(0);const memberSerial=useRef(0);
  const owner=JSON.stringify([scope,field,revealed]);const currentOwner=useRef(owner);currentOwner.current=owner;
  const data=answer?.owner===owner ? answer.result : null;
  const rows=members?.owner===owner ? members.result : null;
  const initialSource=JSON.stringify([scope.workspace,scope.case,scope.identity,scope.query,selector]);
  useEffect(()=>{setField(selector);setInput(selector);setRevealConsent(null);},[initialSource,selector]);

  const read=async(offset=0,snapshot="")=>{
    const held=owner;const serial=++countSerial.current;
    setReading(true);setProblem("");setMembers(null);setSelectedBucket(null);setSelectedMessage(null);++memberSerial.current;
    const result=await readFieldValues({scope,selector:field,reveal:revealed,offset,limit:100,...(snapshot ? {snapshot}: {})});
    if(currentOwner.current!==held||countSerial.current!==serial)return;
    if(result.state==="completed"&&(result.identity!==scope.identity||result.revealed!==revealed)){setAnswer(null);setProblem("The returned counts do not match this source and reveal scope.");setReading(false);return;}
    setAnswer({owner:held,result});setReading(false);
  };
  useEffect(()=>{void read();return()=>{++countSerial.current;++memberSerial.current;};},[owner]);
  const show=async(bucket:string,offset=0)=>{
    if(data?.state!=="completed")return;
    const held=owner;const snapshot=data.snapshot;const serial=++memberSerial.current;
    setSelectedBucket(bucket);setMembers(null);setMembersReading(true);
    const result=await readFieldValueOccurrences({scope,selector:data.selector,reveal:revealed,snapshot,bucket,offset,limit:200});
    if(currentOwner.current!==held||memberSerial.current!==serial)return;
    if(result.state==="completed"&&(result.identity!==scope.identity||result.scope_identity!==data.scope_identity||result.snapshot!==snapshot||result.bucket!==bucket)){setMembers(null);setMembersReading(false);setProblem("The returned messages do not match this counted scope.");return;}
    setMembers({owner:held,result});setMembersReading(false);
  };
  const labelFor=(row:FieldValueBucket)=>!revealed&&row.state==="present" ? "Hidden present values":bucketLabel(row);
  const bucketColumns:Column<FieldValueBucket>[]=[
    {key:"value",header:"Value",priority:1,minWidth:16,flex:true,render:row=><span className={row.state==="present"&&!row.hidden&&revealed ? "value":""}>{labelFor(row)}</span>},
    {key:"messages",header:"Messages",priority:1,minWidth:7,render:row=>row.messages},
    {key:"open",header:"",priority:1,minWidth:10,render:row=><button type="button" className="quiet" disabled={reading||membersReading||busy} onClick={event=>{event.stopPropagation();void show(row.id);}}>Show messages</button>},
  ];
  const messageColumns:Column<MessageRow>[]=[
    {key:"message",header:"Message",priority:1,minWidth:10,flex:true,render:row=><>{row.message_code}{row.trigger_event ? "^"+row.trigger_event:""}</>},
    {key:"source",header:"Source",priority:1,minWidth:8,render:row=>row.source_name||row.source_id},
    {key:"occurrence",header:"Occurrence",priority:1,minWidth:12,render:row=><span className="selector">{row.id}</span>},
  ];
  return <section className="field-values" aria-label="Field values">
    <header className="field-values-header"><h2>Field values</h2><button type="button" onClick={onBack}>Back to reader</button></header>
    <h3>{field}{label&&field===selector ? " · "+label:""}</h3>
    <p className="field-values-summary">{scope.case} · {data?.state === "completed" ? `${data.matched} messages · ${data.group_count} value groups · ${data.counts.empty} empty` : "Current capture and filters"}</p>
    <form className="field-values-field" onSubmit={event=>{event.preventDefault();setField(input.trim());}}><label htmlFor="aggregate-field">Field</label><div><input id="aggregate-field" value={input} maxLength={256} onChange={event=>setInput(event.target.value)}/><button type="submit" disabled={reading||busy||!input.trim()}>Count</button></div></form>
    <div className="field-values-actions"><button type="button" disabled={busy} onClick={()=>setRevealConsent({owner:consentOwner,shown:!revealed})}>{revealed ? "Hide values":"Show values for this scope"}</button><button type="button" disabled={reading||busy} onClick={()=>void read()}>Recount</button>{reading ? <button type="button" onClick={()=>cancel("field-values-read")}>Stop counting</button>:null}</div>
    <p>{revealed ? "Decoded values across this explicit scope are shown. They are never saved with this view.":"Values are hidden. Present values share one bucket; no distinct-value fingerprint is exposed."}</p>
    {problem ? <p role="alert">{problem}</p>:null}
    {reading ? <p role="status">Counting the complete selected scope…</p>:null}
    {data&&data.state!=="completed" ? <p role="alert">{data.reason||"The scope could not be counted."}</p>:null}
    {data?.state==="completed" ? <>
      <details className="field-values-coverage"><summary>Scope and coverage</summary><p>{data.matched} matched of {data.total} capture occurrences · {data.scanned} examined · {data.scan_complete ? "Complete scope scan":"Incomplete scope scan"}</p>
      <p>{data.complete ? "Complete field-value coverage":"Some field values or query membership remain unavailable or undecided."}</p>
      {data.scope_undecided>0||data.scope_undecodable>0 ? <p role="status">Query uncertainty: {data.scope_undecided} undecided; {data.scope_undecodable} eligible occurrences could not be parsed.</p>:null}
      <dl className="field-values-counts"><div><dt>Readable present</dt><dd>{data.counts.present}</dd></div><div><dt>Empty</dt><dd>{data.counts.empty}</dd></div><div><dt>Null</dt><dd>{data.counts.null}</dd></div><div><dt>Omitted</dt><dd>{data.counts.omitted}</dd></div><div><dt>Undecodable</dt><dd>{data.counts.undecodable}</dd></div><div><dt>Undecided</dt><dd>{data.counts.undecided}</dd></div></dl>
      </details>
      {!data.complete || !data.scan_complete ? <p role="status">Counts are incomplete. Scope and coverage identifies unavailable or undecided values.</p> : null}
      <DataTable label="Field value counts" rows={data.rows} rowId={row=>row.id} rowLabel={labelFor} columns={bucketColumns} selected={selectedBucket} onSelect={id=>void show(id)} onOpen={id=>void show(id)} loading={reading}/>
      <Pager first={data.offset} count={data.rows.length} total={data.group_count} noun="values" disabled={reading||busy} onPrevious={()=>void read(Math.max(0,data.offset-100),data.snapshot)} onNext={()=>void read(data.offset+100,data.snapshot)}/>
    </>:null}
    {selectedBucket ? <section aria-label="Counted messages"><header className="field-values-header"><h3>Counted messages</h3><button type="button" onClick={()=>{++memberSerial.current;setSelectedBucket(null);setMembers(null);}}>Back to aggregate</button></header>{membersReading ? <p role="status">Reading counted occurrences…</p>:null}{rows&&rows.state!=="completed" ? <p role="alert">{rows.reason||"The counted occurrences are unavailable."}</p>:null}{rows?.state==="completed" ? <><p>{rows.total} exact counted occurrences</p><DataTable label="Counted messages" rows={rows.rows} rowId={row=>row.id} rowLabel={row=>row.id} columns={messageColumns} selected={selectedMessage} onSelect={setSelectedMessage} onOpen={id=>onInspectOccurrence(id,data?.selector||field)} loading={membersReading}/><Pager first={rows.offset} count={rows.rows.length} total={rows.total} noun="counted messages" disabled={membersReading||busy} onPrevious={()=>void show(rows.bucket,Math.max(0,rows.offset-200))} onNext={()=>void show(rows.bucket,rows.offset+200)}/></>:null}</section>:null}
  </section>;
}
