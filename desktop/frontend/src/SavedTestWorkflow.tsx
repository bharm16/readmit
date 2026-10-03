import { useState, type ReactNode } from "react";
import { ValueRows } from "./layout";
import { TestInputEvidence } from "./TestInputEvidence";
import type { TestMessage } from "./bindings";
import { messageLabel } from "./TestEditor";

/** The saved test's workspace reads its exact recorded inputs. Selecting a
 * step changes only inspection; edits still enter the existing draft owner. */
export function SavedTestWorkflow({workspace,inputs,source,entry,identity,target,observation,preparation,checksShown,onChecks,onInputs,children}:{
 workspace:string; inputs:TestMessage[];source:string;entry:string;identity:string;target:string;observation:string;preparation:string;checksShown:boolean;onChecks:()=>void;onInputs:()=>void;children:ReactNode;
}) {
 const [selected,setSelected]=useState<string|null>(null);
 const input=inputs.find(row=>row.id===selected);
 return <div className="workflow-workspace"><aside className="workflow-rail" aria-label="Saved test steps"><h2>Steps</h2><button type="button" className="workflow-step" aria-current={!checksShown&&!input?"step":undefined} onClick={()=>{setSelected(null);onInputs();}}><strong>Preparation</strong><span>{preparation||"Starting state not recorded"}</span></button>{inputs.map((row,index)=><button className="workflow-step" key={row.id} type="button" aria-current={!checksShown&&row.id===selected?"step":undefined} onClick={()=>{setSelected(row.id);onInputs();}}><strong>{index+1} · Send {messageLabel(row,row.id).replace(" · ","^")}</strong><span>{row.id}</span></button>)}<button className="workflow-step" type="button" aria-current={checksShown?"step":undefined} onClick={onChecks}><strong>{inputs.length+1} · {observation||"Expected behavior"}</strong><span>{observation?"Read retained receiver observation":"Authored expectations"}</span></button></aside><div className="workflow-content">{!checksShown&&input?<><h2>Send {messageLabel(input,input.id).replace(" · ","^")}</h2><TestInputEvidence key={`${entry}:${identity}:${input.id}`} workspace={workspace} entry={entry} identity={identity} occurrence={input.id} source={source||"Unavailable original source"}/><div className="workflow-connection-fields workflow-selected-input-fields"><ValueRows rows={[{label:"Target",value:target||"Not configured"},{label:"Observation after this step",value:observation||"Not configured"}]}/></div><p className="workflow-caption">Original input retained. Changes create a separate version.</p></>:children}</div></div>;
}
