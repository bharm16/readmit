import "./workflow.css";
import { useEffect, useState } from "react";
import { listWholeCatalog, openReport, type ReportView, type CatalogItem, type ItemRef, type RequestContext } from "./bindings";
import { useRunComparison } from "./RunComparison";
import { useViewState } from "./viewstate";
import { DataTable } from "./DataTable";
import { EmptyState } from "./layout";
import { comparable } from "./Runs";
import { RUN_RESULTS, term } from "./display";
import { listDate } from "./Projects";

export type SelectedTestRun = {
  run: ItemRef;
  revision: string;
  started_at: string | null;
  outcome: string;
  association: string;
  comparable: boolean;
};

/** Retained original pins identify membership. Content-equivalent copies do
 * not become runs of this test merely by sharing a specification. */
export function associatedTestRuns(items: CatalogItem[], test: ItemRef): SelectedTestRun[] {
  return items.flatMap(item => {
    const summary = item.summary.run;
    if (!summary?.test || summary.test.id !== test.id || !["linked", "missing"].includes(summary.test_association ?? "")) return [];
    return [{
      run: item.ref,
      revision: summary.test.revision ?? summary.version ?? "",
      started_at: summary.started_at,
      outcome: summary.result ?? summary.outcome ?? "unknown",
      association: summary.test_association ?? "",
      comparable: comparable(item),
    }];
  }).sort((left, right) => (right.started_at ?? "").localeCompare(left.started_at ?? "") || left.run.id.localeCompare(right.run.id));
}

/** Actual retained runs define the offered roles. Choosing them is read-only;
 * the shared comparison reader decides what the evidence establishes. */
export function SelectedTestBeforeAfter({ root, test, runs, onOpenRun, onPair }: {
  root: string;
  test: ItemRef;
  runs: SelectedTestRun[];
  onOpenRun: (run: ItemRef) => void;
  onPair: (pair: { before: ItemRef; after: ItemRef } | null) => void;
}) {
  const [before, setBefore] = useViewState(`test.${test.id}.before`, "");
  const [after, setAfter] = useViewState(`test.${test.id}.after`, "");
  const [view, setView] = useState("checks");
  const beforeRun = runs.find(row => row.run.id === before && row.comparable);
  const afterRun = runs.find(row => row.run.id === after && row.comparable);
  const valid = before !== after && !!beforeRun && !!afterRun;
  const ids = valid ? [before, after] : [];
  useEffect(() => {
    onPair(valid ? { before: beforeRun!.run, after: afterRun!.run } : null);
  }, [before, after, valid, onPair]);
  const compared = useRunComparison({
    root, runs: ids, view, onView: setView, explicitRoles: true,
    onRuns: next => { setBefore(next[0] ?? ""); setAfter(next[1] ?? ""); },
    onOpen: id => { const row = runs.find(row => row.run.id === id); if (row) onOpenRun(row.run); },
  });
  const label = (row: SelectedTestRun) => `${listDate(row.started_at)} · ${term(RUN_RESULTS, row.outcome).text} · ${row.revision ? `v${row.revision}` : "Original"}`;
  return <section className="workflow-page workflow-content" aria-label="Selected test comparison">
    <div className="workflow-roles">
      <label>Before<select value={before} onChange={event => setBefore(event.target.value)}>
        <option value="">Choose retained run…</option>
        {runs.map(row => <option key={row.run.id} value={row.run.id} disabled={row.run.id === after || !row.comparable}>{label(row)}</option>)}
      </select></label>
      <label>After<select value={after} onChange={event => setAfter(event.target.value)}>
        <option value="">Choose retained run…</option>
        {runs.map(row => <option key={row.run.id} value={row.run.id} disabled={row.run.id === before || !row.comparable}>{label(row)}</option>)}
      </select></label>
    </div>
    {valid ? compared.body : <p>Choose two retained runs of this test to compare.</p>}
  </section>;
}

/** Report/test membership is supplied by the verified retained association,
 * never guessed from a report title or from sharing a source case. */
export function SelectedTestExports({ test, context, runs, onOpenReport, onCreateReport, onExportDefinition, onOpenCapture, onOpenRun }: {
  test: ItemRef;
  context: () => RequestContext;
  runs: SelectedTestRun[];
  onOpenReport: (report: ItemRef) => void;
  onCreateReport: (run: ItemRef) => void;
  onExportDefinition: () => void;
  onOpenCapture?: ((capture:ItemRef)=>void)|undefined;
  onOpenRun?: ((run:ItemRef)=>void)|undefined;
}) {
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [selected, setSelected] = useState("");
  const [focusedReport,setFocusedReport]=useState("");
  const [reportView,setReportView]=useState<ReportView|null>(null);
  useEffect(() => {
    let current = true;
    setItems(null);
    setFailure(null);
    const request = context();
    void listWholeCatalog({ context: request, kind: "report", filter: {} }).then(answer => {
      if (!current) return;
      if (answer.state !== "completed" && answer.state !== "empty") {
        setFailure(answer.reason ?? "The retained exports could not be read.");
        return;
      }
      setItems((answer.page?.items ?? []).filter(item => item.summary.report?.tests?.some(ref => ref.id === test.id)));
    });
    return () => { current = false; };
  }, [test.id, context]);
  const focused=items?.find(item=>item.ref.id===focusedReport)??items?.[0];
  useEffect(()=>{
   let current=true;setReportView(null);
   if(focused)void openReport({context:context(),ref:focused.ref,reveal:false}).then(answer=>{if(current && answer.state==="completed" && answer.report)setReportView(answer.report);});
   return()=>{current=false;};
  },[focused?.ref.id,focused?.ref.revision,context]);
  const chosen = runs.find(row => row.run.id === selected && row.comparable);
  return <section className="workflow-page workflow-content" aria-label="Selected test exports">
    {failure ? <p role="alert">{failure}</p> : items?.length === 0 ? <EmptyState title="No retained reports for this test" /> : <DataTable
      label="Test reports" rows={items ?? []} rowId={item => item.ref.id} rowLabel={item => item.name} selected={focused?.ref.id??null}
      onSelect={setFocusedReport} onOpen={id => { const item = items?.find(item => item.ref.id === id); if (item) onOpenReport(item.ref); }} loading={items === null}
      columns={[
        { key: "name", header: "Output", priority: 1, minWidth: 12, flex: true, render: item => <span className="workflow-link">{item.name}</span> },
        {key:"source",header:"Source",priority:1,minWidth:10,render:item=>(item.summary.report?.source_runs?.length??0)>1?"Before + After":"Retained run"},
        {key:"format",header:"Format",priority:1,minWidth:8,render:item=>item.ref.id===focused?.ref.id&&reportView?(reportView.shares[0]?.format?.toUpperCase()??"Not exported"):"Open preview"},

        { key: "open", header: "", priority: 1, minWidth: 5, render: item => <button type="button" onClick={event => { event.stopPropagation(); onOpenReport(item.ref); }}>Open report</button> },
      ]}
    />}
    {focused ? <section className="workflow-export-source-links"><h3>Source evidence remains available</h3><div className="toolbar-group">{focused.summary.report?.source_runs?.map((run,index)=><button type="button" key={run.id} onClick={()=>{const held=runs.find(row=>row.run.id===run.id);if(held)onOpenRun?.(held.run);}} disabled={!runs.some(row=>row.run.id===run.id)}>{index===0&&focused.summary.report?.source_runs?.length===2?"After run":index===1?"Before run":"Retained run"}</button>)}{focused.summary.report?.related_case&&onOpenCapture?<button type="button" onClick={()=>onOpenCapture(focused.summary.report!.related_case!)}>Input capture</button>:null}</div></section>:null}
    <div className="toolbar-group">
      <button type="button" onClick={onExportDefinition}>Export test definition</button>
      <label>Retained run<select value={selected} onChange={event => setSelected(event.target.value)}>
        <option value="">Choose…</option>
        {runs.map(row => <option key={row.run.id} value={row.run.id} disabled={!row.comparable}>{listDate(row.started_at)} · {term(RUN_RESULTS, row.outcome).text}</option>)}
      </select></label>
      <button type="button" disabled={!chosen} onClick={() => { if (chosen) onCreateReport(chosen.run); }}>Create report</button>
    </div>
    <p>Reports shown here have a verified association with this test. Open a report to review its original or sealed sharing history.</p>
  </section>;
}
