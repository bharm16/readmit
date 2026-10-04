import "./workflow.css";
import type { RunCompareFlowComparison, CatalogItem, RunComparisonView } from "./bindings";
import { ValueRows } from "./layout";

const DIMENSIONS: Record<string,string>={"check-definition":"Expected behavior",input:"Inputs",environment:"Environment","protocol-boundary":"Protocol boundary",target:"Target declaration","profile-validator":"Profile and validator","completion-policy":"Completion policy",collector:"Collector",engine:"Engine"};

/** An adapter for the verified offline comparison; no window-side evaluator. */
export function ConnectedRunComparison({comparison,candidates,onRuns,onOpen}:{comparison:RunComparisonView;candidates:CatalogItem[];onRuns:(ids:string[])=>void;onOpen:(id:string)=>void}) {
 const flow=comparison.connected!;
 const role=(name:"Before"|"After",id:string,index:number)=><div><label htmlFor={`connected-${name.toLowerCase()}`}>{name}</label><select id={`connected-${name.toLowerCase()}`} value={id} onChange={event=>onRuns(index===0?[event.target.value,comparison.later.run.id]:[comparison.earlier.run.id,event.target.value])}>{candidates.map(item=><option key={item.ref.id} value={item.ref.id} disabled={item.ref.id===(index===0?comparison.later.run.id:comparison.earlier.run.id)}>{item.name} · {item.summary.run?.result??"Unknown"} · {item.summary.run?.started_at??"Time not recorded"}</option>)}</select><button type="button" className="link" onClick={()=>onOpen(id)}>Open {name.toLowerCase()} evidence</button></div>;
 return <section aria-label="Connected Before and After" className="object-page workflow-page workflow-content">
 <div className="workflow-roles">{role("Before",comparison.earlier.run.id,0)}{role("After",comparison.later.run.id,1)}</div><button type="button" onClick={()=>onRuns([comparison.later.run.id,comparison.earlier.run.id])}>Swap before and after</button>
 <table className="plain-table" aria-label="Connected comparison checks"><thead><tr><th>Phase</th><th>Expectation</th><th>Before</th><th>After</th><th>Definition</th><th>Behavior</th></tr></thead><tbody>{flow.checks.map(row=><tr key={`${row.phase}:${row.check}`}><td>{row.phase}</td><td>{row.check}</td><td>{row.baseline}</td><td>{row.current}</td><td>{row.definition}</td><td>{row.behavior}</td></tr>)}</tbody></table>
 <table className="plain-table" aria-label="Recorded execution conditions"><thead><tr><th>Condition</th><th>Recorded change</th><th>Declarations</th></tr></thead><tbody>{flow.dimensions.map(row=><tr key={row.dimension}><td>{DIMENSIONS[row.dimension]??row.dimension}</td><td>{row.state}</td><td>{row.changed.join(" · ")||"No declared change"}</td></tr>)}</tbody></table>
 <section className="workflow-surface" aria-label="Retained input comparison"><h3>Recorded input relationship</h3><p>{flow.dimensions.find(row=>row.dimension==="input")?.state??"Unavailable"}</p><p className="workflow-caption">{flow.attribution.reason}</p></section>
 <ConnectedComparedRecords flow={flow}/>
 <ValueRows label="Retained identities" rows={[{label:"Before",value:flow.baseline.identity},{label:"After",value:flow.current.identity},{label:"Original engine",value:`${flow.baseline.engine} → ${flow.current.engine}`}]} />
 <p className="support">{flow.attribution.reason}</p><p className="muted">{flow.scope}</p>
 <p className="muted">Original build and target availability are not established by an offline comparison. Run again requires a separate reviewed execution.</p>
 </section>;
}
function ConnectedComparedRecords({flow}:{flow:RunCompareFlowComparison}) {return <table className="plain-table" aria-label="Compared retained observations"><thead><tr><th>Phase / dataset</th><th>Coverage</th><th>Before records</th><th>After records</th><th>Changed fields</th></tr></thead><tbody>{flow.records.map(row=><tr key={`${row.phase}:${row.dataset}`}><td>{row.phase} / {row.dataset}</td><td>{row.reason||row.state}</td><td>{row.state==="compared"?row.keys.reduce((total,key)=>total+key.baseline,0):"Unavailable"}</td><td>{row.state==="compared"?row.keys.reduce((total,key)=>total+key.current,0):"Unavailable"}</td><td>{[...new Set(row.keys.flatMap(key=>key.values))].join(" · ")||"None compared"}</td></tr>)}</tbody></table>}
