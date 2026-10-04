import type { ReportView } from "./bindings";
import { Reveal,ValueRows } from "./layout";

export function ConnectedReportEvidence({view,onReveal}:{view:ReportView;onReveal:(value:boolean)=>void}) {
 const doc=view.connected!;
 return <section className="report-reader" aria-label="Connected report evidence">
 <ValueRows rows={[{label:"Packet identity",value:doc.packet_identity},{label:"Evidence class",value:doc.evidence_class},{label:"Regression equivalence",value:doc.equivalence.state}]} />
 {view.notes ? <section aria-label="Local report notes"><h2>Notes</h2><p className="report-notes">{view.notes}</p><p className="muted">Authored titles and notes stay with this saved report. Connected exports contain the retained evidence document.</p></section> : null}
 <Reveal revealed={view.revealed} onToggle={onReveal}/>
 {doc.runs.map(run=><section key={run.section} aria-label={`${run.section} execution`}><h2>{run.section} execution</h2><ValueRows rows={[{label:"Retained run",value:run.run.identity},{label:"Result",value:`${run.run.verdict} · ${run.run.state}`},{label:"Preparation",value:run.run.setup},{label:"Cleanup",value:run.run.cleanup},{label:"Engine at execution",value:run.run.engine}]} />{run.phases.map(phase=><section key={phase.id}><h3>{phase.id}</h3><table className="plain-table" aria-label={`${run.section} ${phase.id} report checks`}><thead><tr><th>Check</th><th>Claim</th><th>Outcome</th></tr></thead><tbody>{phase.checks.map(check=><tr key={check.id}><td>{check.id}</td><td>{check.claim}</td><td>{check.outcome}</td></tr>)}</tbody></table>{phase.observations.map(observation=><section key={observation.dataset}><h4>{observation.dataset}</h4><p>{observation.boundary} · {observation.usable?`${observation.records.length} retained records`:"Unavailable observation; no absence established"}</p><table className="plain-table" aria-label={`${observation.dataset} report fields`}><thead><tr><th>Record</th><th>Field</th><th>State</th><th>Value</th></tr></thead><tbody>{observation.records.flatMap(record=>record.fields.map(field=><tr key={`${record.row}:${field.column}`}><td>{record.row}</td><td>{field.column}</td><td>{field.state}</td><td>{view.revealed?field.text:"Hidden"}</td></tr>))}</tbody></table></section>)}</section>)}</section>)}
 {doc.comparison?<section aria-label="Reported comparison"><h2>Supplied Before / After</h2><p>{doc.comparison.attribution.reason}</p><table className="plain-table"><thead><tr><th>Dimension</th><th>Declared change</th></tr></thead><tbody>{doc.comparison.dimensions.map(row=><tr key={row.dimension}><td>{row.dimension}</td><td>{row.state}</td></tr>)}</tbody></table></section>:null}
 <p>{doc.equivalence.reason}</p><ul>{doc.limitations.map(line=><li key={line}>{line}</li>)}</ul>
 </section>;
}
